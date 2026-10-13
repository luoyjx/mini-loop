"""Actual Source fallback selection at Settings, Recovery and Agent boundaries."""

import asyncio
import copy
import hashlib
import os
import tempfile
from dataclasses import replace
from pathlib import Path
from unittest.mock import patch


def legacy_recovery_contracts() -> dict:
    # Import-time configuration and every finite profile stay inside the saved
    # environment. This also protects an otherwise fresh exporter process.
    with patch.dict(os.environ, {}, clear=True):
        return _capture_legacy_recovery_contracts()


def _capture_legacy_recovery_contracts() -> dict:
    from mini_loop import Agent, DefaultRecovery, DirectRecovery, RunContext, SessionManager, Settings
    from mini_loop import recovery as recovery_module
    from mini_loop.caching import NullCachePolicy
    from mini_loop.fake_llm import FakeAsyncAnthropic, text
    from mini_loop.registry import ToolRegistry

    legacy = {"FALLBACK_MODEL_ID": "legacy"}
    both = {"MINILOOP_FALLBACK_MODEL": "modern", **legacy}
    long_legacy = {"FALLBACK_MODEL_ID": "m" * 513}

    def settings(root):
        return Settings(fake_llm=True, model="primary", max_tokens=64, max_turns=4,
                        workspace_root=root, skills_dir=root / "skills", spill_dir=None,
                        trajectory_enabled=False)

    def selected(recovery):
        return getattr(recovery, "fallback_model", None)

    def injected(kind, fallback):
        if kind == "direct":
            return DirectRecovery()
        if kind == "explicit":
            return DefaultRecovery(fallback_model=fallback)
        assert kind == "default"
        return None

    def make_agent(root, configured, recovery, client=None, emit=None):
        return Agent(client=client if client is not None else FakeAsyncAnthropic(thinking=False, delay=0),
                     settings=configured, workspace=root, tools=ToolRegistry(),
                     system="Legacy recovery contract.", recovery=recovery, emit=emit,
                     cache_policy=NullCachePolicy())

    constructors = []
    for name, environment, fallback, after in [
        ("absent", {}, None, {}),
        ("empty", {"FALLBACK_MODEL_ID": ""}, None, {}),
        ("whitespace", {"FALLBACK_MODEL_ID": " \t "}, None, {}),
        ("explicit-priority", legacy, "explicit", legacy),
        ("explicit-empty", legacy, "", legacy),
        ("modern-ignored", {"MINILOOP_FALLBACK_MODEL": "modern"}, None, {}),
        ("capture-stable", {"FALLBACK_MODEL_ID": "before"}, None,
         {"FALLBACK_MODEL_ID": "after"}),
        ("explicit-512", legacy, "m" * 512, {}),
        ("legacy-513", long_legacy, None, {}),
    ]:
        with patch.dict(os.environ, environment, clear=True):
            policy = DefaultRecovery(fallback_model=fallback)
            before = selected(policy)
            with patch.dict(os.environ, after, clear=True):
                after_selected = selected(policy)
        constructors.append(dict(name=name, environment=dict(environment), explicit_fallback=fallback,
                                 after_environment=dict(after), selected=before, after_selected=after_selected))

    def agent_recipe(name, settings_environment, *, agent_environment=None,
                     after_environment=None, recovery_kind="default", explicit_fallback=None):
        creation = settings_environment if agent_environment is None else agent_environment
        after = creation if after_environment is None else after_environment
        return dict(name=name, settings_environment=dict(settings_environment),
                    agent_environment=dict(creation), after_environment=dict(after),
                    recovery_kind=recovery_kind, explicit_fallback=explicit_fallback)

    profiles = [
        agent_recipe("default", {}),
        agent_recipe("legacy-only", legacy),
        agent_recipe("modern-priority", both),
        agent_recipe("modern-empty", {"MINILOOP_FALLBACK_MODEL": "", **legacy}),
        agent_recipe("modern-whitespace", {"MINILOOP_FALLBACK_MODEL": " \t ", **legacy}),
        agent_recipe("legacy-between-settings-agent", {"FALLBACK_MODEL_ID": "before"},
                     agent_environment={"FALLBACK_MODEL_ID": "after"}),
        agent_recipe("modern-between-settings-agent", {"MINILOOP_FALLBACK_MODEL": "before"},
                     agent_environment={"MINILOOP_FALLBACK_MODEL": "after", **legacy}),
        agent_recipe("legacy-after-agent", {"FALLBACK_MODEL_ID": "before"},
                     after_environment={"FALLBACK_MODEL_ID": "after"}),
        agent_recipe("explicit-direct", both, recovery_kind="direct"),
        agent_recipe("explicit-recovery", both, recovery_kind="explicit", explicit_fallback="explicit"),
        agent_recipe("explicit-recovery-empty", both, recovery_kind="explicit", explicit_fallback=""),
    ]

    def manager_recipe(name, settings_environment, manager_environment,
                       first_environment, second_environment, *, recovery_kind="default"):
        return dict(name=name, settings_environment=dict(settings_environment),
                    manager_environment=dict(manager_environment),
                    first_environment=dict(first_environment), second_environment=dict(second_environment),
                    recovery_kind=recovery_kind, explicit_fallback=None)

    manager_specs = [
        manager_recipe("default-changing", {"FALLBACK_MODEL_ID": "at-settings"},
                       {"FALLBACK_MODEL_ID": "at-manager"},
                       {"FALLBACK_MODEL_ID": "first"}, {"FALLBACK_MODEL_ID": "second"}),
        manager_recipe("modern-fixed", {"MINILOOP_FALLBACK_MODEL": "fixed-modern"},
                       {"MINILOOP_FALLBACK_MODEL": "manager-modern", "FALLBACK_MODEL_ID": "at-manager"},
                       {"MINILOOP_FALLBACK_MODEL": "first-modern", "FALLBACK_MODEL_ID": "first"},
                       {"MINILOOP_FALLBACK_MODEL": "second-modern", "FALLBACK_MODEL_ID": "second"}),
        manager_recipe("explicit-shared", {}, {"FALLBACK_MODEL_ID": "captured-policy"},
                       {"FALLBACK_MODEL_ID": "first"}, {"FALLBACK_MODEL_ID": "second"},
                       recovery_kind="explicit"),
        manager_recipe("direct-shared", both, {"FALLBACK_MODEL_ID": "at-manager"},
                       {"FALLBACK_MODEL_ID": "first"}, {"FALLBACK_MODEL_ID": "second"},
                       recovery_kind="direct"),
        manager_recipe("empty-then-value", {"FALLBACK_MODEL_ID": ""}, {"FALLBACK_MODEL_ID": ""},
                       {"FALLBACK_MODEL_ID": ""}, {"FALLBACK_MODEL_ID": "later"}),
    ]

    async def manager_profile(spec, root):
        def observation(session):
            assert session.agent is not None
            policy = session.agent.recovery
            return dict(selected=selected(policy), recovery_class=type(policy).__name__)

        with patch.dict(os.environ, spec["settings_environment"], clear=True):
            configured = settings(root)
        with patch.dict(os.environ, spec["manager_environment"], clear=True):
            policy = injected(spec["recovery_kind"], spec["explicit_fallback"])
            manager = SessionManager(configured, FakeAsyncAnthropic(thinking=False, delay=0),
                                     recovery=policy, tool_registry=ToolRegistry())
        try:
            with patch.dict(os.environ, spec["first_environment"], clear=True):
                first = manager.create(owner="fixture-owner")
                first_observation = observation(first)
            with patch.dict(os.environ, spec["second_environment"], clear=True):
                second = manager.create(owner="fixture-owner")
                second_observation = observation(second)
                first_after_second = observation(first)
            return dict(**spec, settings_fallback=configured.fallback_model,
                        manager_recovery_class=(type(manager.recovery).__name__
                                                if manager.recovery is not None else None),
                        first=first_observation, second=second_observation,
                        first_after_second=first_after_second,
                        shared_recovery=first.agent.recovery is second.agent.recovery,
                        first_uses_injected=first.agent.recovery is policy,
                        second_uses_injected=second.agent.recovery is policy)
        finally:
            await manager.stop()

    class Overloaded(Exception):
        status_code = 529

    async def run_profile(spec, root):
        requests, events, error_events, waits, outputs, errors, models = [], [], [], [], [], [], []
        turn = 0

        def respond(request):
            requests.append(dict(turn=turn, model=request["model"]))
            if len(requests) <= 3:
                raise Overloaded("fixture overloaded")
            return [text("done")], "end_turn"

        async def emit(event):
            if event["type"] == "recovery":
                events.append(copy.deepcopy(event))
            elif event["type"] == "error":
                error_events.append(copy.deepcopy(event))

        async def skip_wait(delay, *args, **kwargs):
            waits.append(delay)

        with patch.dict(os.environ, spec["environment"], clear=True):
            configured = settings(root)
            agent = make_agent(root, configured, injected(spec["recovery_kind"], None),
                               FakeAsyncAnthropic(responder=respond, thinking=False, delay=0), emit)
            chosen = selected(agent.recovery)
            with patch.dict(os.environ, spec["after_environment"], clear=True), \
                    patch.object(recovery_module.random, "random", lambda: 0.0), \
                    patch.object(recovery_module.asyncio, "sleep", skip_wait):
                for turn in range(1, spec["turn_count"] + 1):
                    context = replace(RunContext.explicit_human(actor_id="fixture-human"),
                                      message_id=f"turn-{turn}")
                    output, error = None, None
                    try:
                        output = await agent.run(f"turn {turn}", run_context=context)
                    except Exception as exc:
                        error = dict(type=type(exc).__name__, text=str(exc))
                    outputs.append(output)
                    errors.append(error)
                    models.append(agent.state.get("recovery_model"))
        return dict(**spec, settings_fallback=configured.fallback_model, selected=chosen,
                    requests=requests, events=events, error_events=error_events,
                    waits=waits, outputs=outputs, errors=errors,
                    model_after_turns=models)

    with tempfile.TemporaryDirectory(prefix="mini-loop-legacy-recovery-") as temporary:
        root = Path(temporary).resolve()
        agents = []
        for index, spec in enumerate(profiles):
            workspace = root / f"agent-{index}"
            with patch.dict(os.environ, spec["settings_environment"], clear=True):
                configured = settings(workspace)
                with patch.dict(os.environ, spec["agent_environment"], clear=True):
                    policy = injected(spec["recovery_kind"], spec["explicit_fallback"])
                    agent = make_agent(workspace, configured, policy)
                    chosen = selected(agent.recovery)
                    with patch.dict(os.environ, spec["after_environment"], clear=True):
                        after_selected = selected(agent.recovery)
            agents.append(dict(**spec, settings_fallback=configured.fallback_model, selected=chosen,
                               after_selected=after_selected, recovery_class=type(agent.recovery).__name__))

        run_specs = [
            dict(name="legacy-only", environment=legacy, after_environment=legacy,
                 recovery_kind="default", turn_count=2),
            dict(name="modern-priority", environment=both, after_environment=both,
                 recovery_kind="default", turn_count=2),
            dict(name="no-fallback", environment={}, after_environment={},
                 recovery_kind="default", turn_count=2),
            dict(name="captured-before-turn", environment={"FALLBACK_MODEL_ID": "before"},
                 after_environment={"FALLBACK_MODEL_ID": "after"}, recovery_kind="default", turn_count=2),
            dict(name="legacy-long-513", environment=long_legacy, after_environment={},
                 recovery_kind="default", turn_count=2),
            dict(name="explicit-direct", environment=legacy, after_environment=legacy,
                 recovery_kind="direct", turn_count=1),
        ]

        async def collect():
            runs = [await run_profile(spec, root / f"run-{index}")
                    for index, spec in enumerate(run_specs)]
            managers = [await manager_profile(spec, root / f"manager-{index}")
                        for index, spec in enumerate(manager_specs)]
            return runs, managers

        runs, managers = asyncio.run(collect())

    package = Path(__file__).resolve().parents[1] / "mini_loop"
    sources = {"mini_loop/" + name: package / name for name in
               ("__init__.py", "config.py", "recovery.py", "agent.py", "fake_llm.py",
                "transport.py", "caching.py", "run_context.py", "manager.py", "session.py", "harness.py")}
    sources["tools/legacy_recovery_contracts.py"] = Path(__file__)
    return dict(
        source_sha256={name: hashlib.sha256(path.read_bytes()).hexdigest()
                       for name, path in sources.items()},
        scope=[
            "Public actual DefaultRecovery, Settings and Agent constructors; environment is isolated and restored for each construction phase.",
            "Settings captures MINILOOP_FALLBACK_MODEL at Settings construction; actual DefaultRecovery selection occurs at Recovery construction, including the default Agent path.",
            "Actual Agent.run and DefaultRecovery.run use FakeAsyncAnthropic with three status-529 failures then success; a second real turn observes persistent model selection. DirectRecovery is the explicit no-retry control.",
            "Only asyncio.sleep is replaced with a recorded no-wait awaitable and random.random returns zero jitter. Real backoff_delay, selection, retry, state updates and event emission are unchanged; no live network or shell execution.",
            "Run message IDs are explicitly supplied as turn-1/turn-2; actual recovery event fields and order are retained without error or model normalization.",
            "Explicit 512-character and legacy 513-character models are passed unchanged; the latter also traverses the actual retry/fallback request path. Agent error events are captured separately from exceptions escaping Agent.run.",
            "Actual SessionManager.create constructs two real Agents in distinct environment phases. Default policies capture legacy per Agent, modern Settings remain fixed, and explicitly injected Recovery instances remain shared. Only object identity is reduced to boolean observations; no manager or Agent construction is replaced.",
        ], constructors=constructors, agents=agents, runs=runs, managers=managers,
    )
