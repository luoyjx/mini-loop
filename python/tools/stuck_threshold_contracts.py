"""Finite public StuckThresholds boundaries and real Agent failure continuations.

Run this module with --output to capture only this fixture. The exporter imports
the same public function; it does not replace any detector or runtime method.
"""

import argparse
import asyncio
import copy
import hashlib
import json
import os
import platform
import tempfile
from dataclasses import asdict, replace
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch


INT64_MIN = -(1 << 63)
INT64_MAX = (1 << 63) - 1


def _failure(error: Exception, stage: str) -> dict:
    return {"exception_class": type(error).__name__, "message": str(error), "stage": stage}


def stuck_threshold_contracts() -> dict:
    # Defaults must not depend on the exporter's credentials or recovery model.
    with patch.dict(os.environ, {}, clear=True):
        return _capture_stuck_threshold_contracts()


def _capture_stuck_threshold_contracts() -> dict:
    from mini_loop import Agent, DefaultStuckDetector, RunContext, Settings, StuckThresholds
    from mini_loop.agent import _messages_payload
    from mini_loop.caching import NullCachePolicy
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool, validate_transcript
    from mini_loop.registry import Hooks, Tool, ToolRegistry
    from mini_loop.session import AgentSession
    from mini_loop.stuck import ToolStep, step_hash

    defaults = asdict(StuckThresholds())
    quiet = dict(repeat_action_result=99, repeat_action_error=99, alternating=0,
                 monologue=99, unproductive_tool=0, max_nudges=0)

    def step(command="a", *, failed=False, denied=False):
        return ToolStep("bash", step_hash({"command": command}), step_hash("same"),
                        failed=failed, denied=denied)

    cases = []

    def inspect(name, steps=(), rounds=0, *, construction="configured", base=None, **overrides):
        requested = None if construction != "configured" else {**(quiet if base is None else base), **overrides}
        row = dict(name=name, construction=construction, requested=requested,
                   thresholds=None, max_nudges=None, steps=[asdict(value) for value in steps],
                   rounds_without_tools=rounds, constructor_error=None,
                   inspection_error=None, signal=None, reminder=None)
        try:
            if construction == "omitted":
                detector = DefaultStuckDetector()
            elif construction == "none":
                detector = DefaultStuckDetector(None)
            else:
                detector = DefaultStuckDetector(StuckThresholds(**requested))
            row["thresholds"] = asdict(detector.thresholds)
            row["max_nudges"] = detector.max_nudges
        except Exception as error:
            row["constructor_error"] = _failure(error, "constructor")
        else:
            try:
                signal = detector.inspect(SimpleNamespace(recent_steps=steps,
                                                          rounds_without_tools=rounds))
                if signal is not None:
                    row["signal"], row["reminder"] = asdict(signal), signal.reminder()
            except Exception as error:
                row["inspection_error"] = _failure(error, "inspect")
        cases.append(row)

    for construction in ("omitted", "none", "configured"):
        inspect(f"default-{construction}", [step()] * 4, construction=construction, base=defaults)
    inspect("configured-all-zero", base={field: 0 for field in defaults})

    for field in ("repeat_action_result", "repeat_action_error"):
        a, b = step(failed=field == "repeat_action_error"), step("b", failed=field == "repeat_action_error")
        for suffix, threshold, steps in (
            ("positive-before", 2, [a]),
            ("positive-at", 2, [a, a]),
            ("positive-last-window", 2, [b, a, a]),
            ("zero-empty", 0, []),
            ("zero-single", 0, [a]),
            ("zero-identical", 0, [a, a]),
            ("zero-mixed", 0, [a, b]),
            ("negative-empty", -1, []),
            ("negative-exhausted", -1, [a]),
            ("negative-suffix", -1, [b, a, a]),
            ("negative-mixed-suffix", -1, [a, a, b]),
            ("int64-min", INT64_MIN, [a]),
            ("int64-max", INT64_MAX, [a, a]),
        ):
            inspect(f"{field}-{suffix}", steps, **{field: threshold})
    inspect("repeat-error-zero-success", [step()], repeat_action_error=0)
    inspect("repeat-error-negative-denied", [step("b"), step(denied=True)], repeat_action_error=-1)

    bad = [step(str(index), failed=True) for index in range(3)]
    for suffix, threshold, steps in (
        ("negative-disabled", -1, bad), ("zero-disabled", 0, bad),
        ("positive-one", 1, bad[:1]), ("positive-before", 3, bad[:2]),
        ("positive-at", 3, bad), ("int64-min-disabled", INT64_MIN, bad),
        ("int64-max", INT64_MAX, bad),
    ):
        inspect(f"unproductive-{suffix}", steps, unproductive_tool=threshold)

    alternating = [step("a"), step("b")] * 3
    for suffix, threshold, steps in (
        ("negative-disabled", -1, alternating), ("zero-disabled", 0, alternating),
        ("three-disabled", 3, alternating), ("positive-before", 4, alternating[:3]),
        ("positive-at", 4, alternating[:4]), ("positive-odd", 5, alternating[:5]),
        ("int64-min-disabled", INT64_MIN, alternating), ("int64-max", INT64_MAX, alternating),
    ):
        inspect(f"alternating-{suffix}", steps, alternating=threshold)

    for suffix, threshold, rounds in (
        ("negative", -1, 0), ("zero", 0, 0), ("positive-before", 1, 0),
        ("positive-at", 1, 1), ("int64-min", INT64_MIN, 0), ("int64-max", INT64_MAX, 0),
    ):
        inspect(f"monologue-{suffix}", rounds=rounds, monologue=threshold)
    for suffix, threshold in (("negative", -1), ("zero", 0), ("positive", 1),
                              ("int64-min", INT64_MIN), ("int64-max", INT64_MAX)):
        inspect(f"max-nudges-{suffix}", max_nudges=threshold)
    inspect("precedence-monologue-before-empty-repeat", monologue=0, repeat_action_error=0)
    inspect("precedence-unproductive-before-empty-result", [step(failed=True)],
            unproductive_tool=1, repeat_action_result=-1)
    inspect("precedence-error-before-result", [step(failed=True)] * 2,
            repeat_action_error=2, repeat_action_result=2)

    class DenyAll(Hooks):
        async def before_tool(self, ctx, call):
            return "Error: permission denied by policy"

    def loop_spec(name, *, pattern="same", finish_after=0, turn_count=1,
                  construction="configured", base=None, **overrides):
        return dict(name=name, pattern=pattern, finish_after=finish_after,
                    turn_count=turn_count, construction=construction, max_rounds=12,
                    thresholds={**(quiet if base is None else base), **overrides})

    specs = [
        loop_spec("default-constructor", construction="omitted", base=defaults),
        loop_spec("unproductive-negative", pattern="varied-denied", finish_after=6, unproductive_tool=-1),
        loop_spec("unproductive-positive", pattern="varied-denied", finish_after=6, unproductive_tool=5),
        loop_spec("alternating-negative", pattern="alternating", finish_after=6, alternating=-1),
        loop_spec("alternating-positive", pattern="alternating", finish_after=6, alternating=6),
        loop_spec("max-nudges-negative", repeat_action_result=2, max_nudges=-1),
        loop_spec("max-nudges-zero", repeat_action_result=2, max_nudges=0),
        loop_spec("max-nudges-positive", repeat_action_result=2, max_nudges=1),
        loop_spec("monologue-zero", monologue=0),
        loop_spec("configured-all-zero", base={field: 0 for field in defaults}),
        loop_spec("repeat-result-negative-fault", repeat_action_result=-1, turn_count=2),
        loop_spec("repeat-error-negative-fault", repeat_action_error=-1, turn_count=2),
    ]

    event_fields = {
        "tool_use": ("name", "input", "id"),
        "tool_result": ("name", "output", "id", "error", "denied", "replayed"),
        "stuck": ("pattern", "detail", "tool", "halted", "nudges_used"),
        "error": ("error",),
    }
    common_event_fields = ("type", "agent", "depth", "message_id", "parent_message_id")

    def messages_snapshot(messages):
        return copy.deepcopy(_messages_payload(messages))

    async def run_loop(spec, workspace, *, managed=False):
        workspace.mkdir(parents=True)
        settings = Settings(fake_llm=True, model="fixture-model", max_tokens=64,
                            max_turns=spec["max_rounds"], workspace_root=workspace,
                            skills_dir=workspace / "empty-skills", spill_dir=None,
                            trajectory_enabled=False)
        global_call, turn_call = 0, 0
        requests, events, event_types, commands, terminal_events = [], [], [], [], []

        def respond(request):
            nonlocal global_call, turn_call
            global_call += 1
            turn_call += 1
            if spec["finish_after"] and turn_call > spec["finish_after"]:
                return [text("done")], "end_turn"
            command = (f"q{turn_call}" if spec["pattern"] == "varied-denied" else
                       ("a" if turn_call % 2 else "b") if spec["pattern"] == "alternating" else "same")
            return [text("loop commentary"), tool("bash", _id=f"u{global_call}", command=command)], "tool_use"

        async def same(ctx, command):
            commands.append(command)
            return "same"

        async def emit(event):
            event_types.append(event["type"])
            if event["type"] in event_fields:
                fields = common_event_fields + event_fields[event["type"]]
                events.append(copy.deepcopy({key: event[key] for key in fields if key in event}))
            if managed and event["type"] in ("status", "error", "done"):
                fields = ("type", "status", "error", "text", "phase")
                terminal_events.append(copy.deepcopy({key: event[key] for key in fields if key in event}))

        registry = ToolRegistry()
        registry.register(Tool("bash", "Stuck fixture tool.",
                               {"type": "object", "properties": {"command": {"type": "string"}},
                                "required": ["command"]}, same, readonly=True, risk="read"))
        client = FakeAsyncAnthropic(responder=respond, thinking=False, delay=0)
        actual_create = client.messages.create

        async def record_create(**kwargs):
            observation = dict(messages=messages_snapshot(kwargs["messages"]), error=None)
            requests.append(observation)
            try:
                return await actual_create(**kwargs)
            except Exception as error:
                observation["error"] = _failure(error, "provider_create")
                raise

        detector = (DefaultStuckDetector() if spec["construction"] == "omitted" else
                    DefaultStuckDetector(StuckThresholds(**spec["thresholds"])))
        session = (AgentSession(f"stuck-{spec['name']}", workspace, event_sink=emit)
                   if managed else None)
        agent = Agent(client=client, settings=settings, workspace=workspace, tools=registry,
                      hooks=DenyAll() if spec["pattern"] == "varied-denied" else Hooks(),
                      system="Stuck threshold contract.", stuck_detector=detector,
                      cache_policy=NullCachePolicy(), emit=session.emit if managed else emit)
        if session is not None:
            session.agent = agent
        turns = []
        with patch.object(client.messages, "create", record_create):
            for turn in range(1, spec["turn_count"] + 1):
                requests.clear()
                events.clear()
                event_types.clear()
                commands.clear()
                terminal_events.clear()
                turn_call = 0
                before_calls = client.calls
                prompt, output, error = f"turn {turn}", None, None
                context = replace(RunContext.explicit_human(actor_id="fixture-human"),
                                  message_id=f"stuck-turn-{turn}")
                try:
                    output = await (session if managed else agent).run(prompt, run_context=context)
                except Exception as caught:
                    error = _failure(caught, "session_run" if managed else "agent_run")
                transcript_error = None
                try:
                    validate_transcript(agent.messages)
                except Exception as caught:
                    transcript_error = _failure(caught, "transcript_validation")
                turns.append(dict(prompt=prompt, output=output, error=error,
                                  requests=copy.deepcopy(requests), model_calls=client.calls - before_calls,
                                  events=copy.deepcopy(events), event_types=list(event_types),
                                  handler_commands=list(commands), recent_steps=[asdict(value) for value in agent.recent_steps],
                                  messages=messages_snapshot(agent.messages),
                                  rounds_without_tools=agent.rounds_without_tools,
                                  nudges_used=agent._stuck_nudges, last_text=agent.last_text,
                                  transcript_error=transcript_error))
                if session is not None:
                    turns[-1].update(session_status=session.status, busy=session.busy,
                                     run_count=session.run_count,
                                     terminal_events=copy.deepcopy(terminal_events))
        return dict(**spec, turns=turns)

    async def all_loops(root):
        loops = [await run_loop(spec, root / spec["name"]) for spec in specs]
        managed = [await run_loop(spec, root / f"managed-{spec['name']}", managed=True)
                   for spec in specs if spec["name"].endswith("-fault")]
        return loops, managed

    with tempfile.TemporaryDirectory(prefix="mini-loop-stuck-thresholds-") as temporary:
        loops, managed_loops = asyncio.run(all_loops(Path(temporary).resolve()))
    package = Path(__file__).resolve().parents[1] / "mini_loop"
    sources = {f"mini_loop/{name}": package / name for name in
               ("__init__.py", "stuck.py", "agent.py", "fake_llm.py", "registry.py", "config.py",
                "caching.py", "recovery.py", "transport.py", "run_context.py", "session.py")}
    sources["tools/stuck_threshold_contracts.py"] = Path(__file__)
    return dict(
        source_sha256={name: hashlib.sha256(path.read_bytes()).hexdigest() for name, path in sources.items()},
        versions={"python": platform.python_version()},
        scope=[
            "Actual public StuckThresholds dataclass and DefaultStuckDetector constructor/inspect; six finite integer fields include negative, zero, positive, and signed int64 boundaries. No Python runtime validation is replaced.",
            "Each direct case separately captures constructor and inspect exceptions with their actual class, message and stage. Signal fields and reminder are actual public observations.",
            "Actual bare Agent.run with an offline FakeAsyncAnthropic, NullCachePolicy and one readonly tool whose handler only records a command and returns same. No shell or live network is used.",
            "The pass-through create observer records actual messages before FakeAsyncAnthropic validation, including rejected next-turn requests. Two negative-repeat faults are followed by a real second user turn on the same Agent.",
            "Events project actual tool_use/tool_result/stuck/error payloads onto declared identity/outcome fields; dynamic span, action, activity and timing fields are excluded. event_types retains all actual Source event names/order as diagnostic evidence, not cross-runtime equality.",
            "Full actual request and retained message payloads are detached through the existing Agent payload serializer; explicit turn message IDs and scripted tool IDs are stable. No transcript repair or exception synthesis is applied by the capture.",
            "Two additional actual AgentSession wrappers attach the same Agent through the public agent setter and invoke public run twice. Public status/busy/run_count and status/error/done identity/outcome projections are observed; default NullStateStore makes no SQLite or durable recovery claim.",
            "This fixture does not replace the pre-existing sixteen default detector decisions or five actual Agent loop controls in python-loops.json, and does not claim arbitrary Python object/type coercion parity.",
        ], cases=cases, loops=loops, managed_loops=managed_loops,
    )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    payload = json.dumps(stuck_threshold_contracts(), ensure_ascii=False, sort_keys=True, indent=2) + "\n"
    args.output.write_text(payload, encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
