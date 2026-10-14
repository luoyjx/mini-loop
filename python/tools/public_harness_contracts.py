"""Observe the public sixteen-field Harness value and real Agent consumers."""
from __future__ import annotations

import asyncio
import dataclasses
import hashlib
import json
from pathlib import Path


def public_harness_contracts(scratch: Path) -> dict:
    from mini_loop import Harness, default_registry
    from mini_loop.agent import Agent
    from mini_loop.caching import DefaultCachePolicy
    from mini_loop.compaction import DefaultCompactor
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text
    from mini_loop.recovery import DefaultRecovery
    from mini_loop.registry import Hooks
    from mini_loop.sandbox import NullSandbox
    from mini_loop.secrets import NullSecretRegistry
    from mini_loop.skills import SkillLoader
    from mini_loop.spill import LocalSpillStore
    from mini_loop.stuck import DefaultStuckDetector
    from mini_loop.subagents import InProcessSubagents
    from mini_loop.token_efficiency import TokenEfficiencyRegistry
    from mini_loop.tool_policy import CapabilityRoleToolPolicy
    from mini_loop.transport import DirectTransport

    fields = [field.name for field in dataclasses.fields(Harness)]
    sentinels = {name: object() for name in fields}
    template = Harness(**sentinels)
    value_rows = []
    for name in fields:
        replacement = object()
        derived = template.derive(**{name: replacement})
        cleared = template.derive(**{name: None})
        value_rows.append(dict(
            field=name,
            omitted_inherits=template.resolve(name, None) is sentinels[name],
            explicit_none_inherits=template.resolve(name, None) is sentinels[name],
            explicit_value_wins=template.resolve(name, replacement) is replacement,
            explicit_false_wins=template.resolve(name, False) is False,
            derive_replaces=getattr(derived, name) is replacement,
            derive_none_clears=getattr(cleared, name) is None,
            untouched_fields_shared=all(
                getattr(derived, other) is sentinels[other]
                for other in fields if other != name
            ),
            original_unchanged=getattr(template, name) is sentinels[name],
        ))

    def failure(operation):
        try:
            operation()
        except Exception as error:
            return dict(exception_class=type(error).__name__, message=str(error))
        raise AssertionError("expected the actual public operation to fail")

    defaults = Harness()
    errors = dict(
        unknown_derive=failure(lambda: defaults.derive(z_unknown=1, a_unknown=2)),
        unknown_resolve_none=failure(lambda: defaults.resolve("unknown", None)),
        frozen_assignment=failure(lambda: setattr(defaults, "tools", object())),
    )

    scratch = Path(scratch)
    workspace = scratch / "workspace"
    workspace.mkdir(parents=True, exist_ok=True)
    skills_dir = scratch / "skills"
    skills_dir.mkdir(parents=True, exist_ok=True)
    settings = Settings(fake_llm=True, workspace_root=workspace,
                        skills_dir=skills_dir, spill_dir=None,
                        token_efficiency_persist_raw=False)
    observed_agents = []
    injected_agents = []

    class ObservedDirectTransport(DirectTransport):
        async def send(self, agent, kwargs):
            observed_agents.append(agent)
            return await super().send(agent, kwargs)

    def profile(label):
        async def injector(agent):
            injected_agents.append((label, agent))
            return []

        return dict(
            tools=default_registry().subset(["bash"]), transport=ObservedDirectTransport(),
            hooks=Hooks(), skills=SkillLoader(skills_dir),
            system_builder=lambda agent: "public-harness " + label,
            compactor=DefaultCompactor(), recovery=DefaultRecovery(),
            stuck_detector=DefaultStuckDetector(), cache_policy=DefaultCachePolicy(),
            secrets=NullSecretRegistry(), sandbox=NullSandbox(),
            spill=LocalSpillStore(scratch / ("spill-" + label)),
            subagents=InProcessSubagents(), token_efficiency=TokenEfficiencyRegistry().runtime(),
            role_tool_policy=CapabilityRoleToolPolicy(), injectors=(injector,),
        )

    a, b = profile("a"), profile("b")
    base = Harness(**a)

    def client():
        return FakeAsyncAnthropic(responder=lambda _: ([text("harness-ok")], "end_turn"),
                                  thinking=False, delay=0)

    def construct(harness, **overrides):
        return Agent(client=client(), settings=settings, workspace=workspace,
                     harness=harness, **overrides)

    def bindings(agent, expected):
        result = {}
        for name in fields:
            if name == "injectors":
                # Runtime facts are always appended by Agent; inherited user
                # injectors must retain identity without aliasing the tuple.
                result[name] = all(any(value is item for item in agent.injectors)
                                   for value in expected[name])
            else:
                result[name] = getattr(agent, name) is expected[name]
        return result

    inheritance = [
        dict(name="harness-only", bindings=bindings(construct(base), a)),
        dict(name="explicit-none-inherits", bindings=bindings(
            construct(base, **{name: None for name in fields}), a)),
        dict(name="explicit-values-win", bindings=bindings(construct(base, **b), b)),
        dict(name="derived-values-win", bindings=bindings(construct(base.derive(**b)), b)),
    ]
    cleared_agent = construct(base.derive(**{name: None for name in fields}))
    cleared = {name: type(getattr(cleared_agent, name)).__name__ for name in fields}
    empty_agent = construct(base, injectors=[])
    empty_injectors = all(a["injectors"][0] is not item for item in empty_agent.injectors)

    async def exercise():
        parent = construct(base)
        output = await parent.run("parent")
        summary = await parent._run_subagent("child", "general-purpose")
        assert len(observed_agents) == 2
        child = observed_agents[-1]
        child_fields = {}
        for name in fields:
            if name == "tools":
                selected = parent.role_tool_policy.select("general-purpose", parent.tools)
                child_fields[name] = dict(same_identity=child.tools is parent.tools,
                                         selected_names=list(child.tools.names()),
                                         expected_names=list(selected.names()))
            elif name == "injectors":
                child_fields[name] = dict(same_identity=child.injectors is parent.injectors,
                                         entries_inherited=all(
                                             any(item is value for value in child.injectors)
                                             for item in a[name]))
            else:
                child_fields[name] = dict(same_identity=getattr(child, name) is getattr(parent, name))
        # The supplied builder handle survives, but a child has its own fixed
        # system string. Comparing a callback identity alone would miss that.
        fixed = child.system.replace(str(workspace), "$WORKSPACE")
        return dict(parent_output=output, child_output=summary,
                    parent_model_calls=parent.client.calls,
                    child_depth=child.depth, parent_depth=parent.depth,
                    child_system=fixed, child_dynamic_system=child._dynamic_system,
                    parent_history_unchanged_by_child=len(parent.messages) == 2,
                    injector_calls=[label for label, _ in injected_agents],
                    injector_parent_seen=any(agent is parent for _, agent in injected_agents),
                    injector_child_seen=any(agent is child for _, agent in injected_agents),
                    child_fields=child_fields)

    runtime = asyncio.run(exercise())
    package = Path(__file__).resolve().parents[1] / "mini_loop"
    source_paths = ["harness.py", "agent.py", "subagents.py", "__init__.py"]
    result = dict(fields=fields, value_rows=value_rows,
                  default_none_fields=[name for name in fields if getattr(defaults, name) is None],
                  default_injectors_empty_tuple=defaults.injectors == (),
                  empty_derive_new_value=defaults.derive() is not defaults,
                  errors=errors, construction=inheritance, cleared_agent_types=cleared,
                  explicit_empty_injectors_replace=empty_injectors, runtime=runtime,
                  scope=[
                      "Actual root-public Harness; all sixteen dataclass fields, shallow derive, None fallback and explicit values including False.",
                      "Four real Agent construction profiles bind every field. Cleared derive selects Agent defaults; None is not an instruction to disable an inherited seam.",
                      "Actual parent run and default in-process child run use the observed direct transport and injected fake SDK, with no remote model or shell execution.",
                      "Child tools are capability-selected; system is fixed despite retaining the Source builder handle; injector containers are copied and runtime facts remain appended.",
                      "Go uses typed RuntimeConfig and ManagerServices, with Provider/stream policy and BashFactory/sandbox/spill adapters. It has no stringly derive/resolve method or arbitrary object attribute contract.",
                      "This is one sixteen-field finite composition contract, not proof of every tool/backend, workflow worker, Launcher default or future field inheritance.",
                  ],
                  source_sha256={"mini_loop/" + name: hashlib.sha256((package / name).read_bytes()).hexdigest()
                                 for name in source_paths},
                  producer_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest())
    json.dumps(result, allow_nan=False)
    return result
