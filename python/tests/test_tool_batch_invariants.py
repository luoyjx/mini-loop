"""Pi P0-2: the tool-batch invariants, pinned instead of commented.

`_exec_tool_batch` claims its properties in comments -- "gather preserves
input order, which is required by provider tool-result protocols";
barriers flush the group. A claim only a comment makes is documentation
(round 99), and these are exactly the properties a refactor of the
dispatcher would silently break. Pinned here:

- transcript result order == call order, proven under ADVERSE completion
  order (the slow tool is called first and finishes last; a vacuous test
  would pass with lucky timing);
- a non-parallel call is an ordering barrier: it starts only after the
  group before it completes, and later calls start only after it ends --
  while parallel calls AFTER the barrier still overlap each other, which
  is mini-loop's deliberate divergence from Pi's whole-batch degradation
  (the safety property is no-reordering-across-the-barrier, and full
  serialization would give up measured concurrency for no added safety);
- a steer landing mid-batch is delivered between rounds, never spliced
  into the batch's result message.
"""

import asyncio
import pathlib
import time

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.registry import Tool, ToolRegistry
from mini_loop.builtins import default_registry

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _instrumented_registry(log):
    registry = default_registry()

    def probe(name, delay):
        async def handler(ctx) -> str:
            log.append((name, "start", time.monotonic()))
            await asyncio.sleep(delay)
            log.append((name, "end", time.monotonic()))
            return f"{name} done"
        return handler

    for name, delay, parallel in (
        ("slow_read", 0.15, True),
        ("fast_read", 0.01, True),
        ("quick_read", 0.01, True),
        ("exclusive_op", 0.05, False),
    ):
        registry.register(Tool(
            name, f"probe {name}", {"type": "object", "properties": {}},
            probe(name, delay), readonly=True, parallel_safe=parallel,
            risk="read",
        ))
    return registry


def _manager(tmp_path, responder, log):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(responder=responder),
        tool_registry=_instrumented_registry(log),
    )


def _result_ids(request_messages):
    for message in request_messages:
        content = message.get("content")
        if isinstance(content, list) and any(
            isinstance(p, dict) and p.get("type") == "tool_result"
            for p in content
        ):
            yield [p["tool_use_id"] for p in content
                   if isinstance(p, dict) and p.get("type") == "tool_result"]


def test_results_keep_call_order_under_adverse_completion(tmp_path):
    log, seen = [], []

    def responder(kwargs):
        seen.append(kwargs["messages"])
        if len(seen) == 1:
            return [text("batch"),
                    tool("slow_read", _id="slow1"),
                    tool("fast_read", _id="fast1")], "tool_use"
        return [text("done")], "end_turn"

    session = _manager(tmp_path, responder, log).create()
    asyncio.run(session.run("go"))

    ends = [name for name, phase, _ in log if phase == "end"]
    assert ends[0] == "fast_read", (
        "the fast tool must finish first or the ordering claim is untested"
    )
    [batch_results] = list(_result_ids(seen[1]))
    assert batch_results == ["slow1", "fast1"], (
        "transcript result order must be call order, not completion order"
    )


def test_a_barrier_sequences_but_does_not_serialize_the_tail(tmp_path):
    log, seen = [], []

    def responder(kwargs):
        seen.append(kwargs["messages"])
        if len(seen) == 1:
            return [text("batch"),
                    tool("slow_read", _id="p1"),
                    tool("exclusive_op", _id="ex"),
                    tool("fast_read", _id="p2"),
                    tool("quick_read", _id="p3")], "tool_use"
        return [text("done")], "end_turn"

    session = _manager(tmp_path, responder, log).create()
    asyncio.run(session.run("go"))

    stamps = {(name, phase): t for name, phase, t in log}
    assert stamps[("exclusive_op", "start")] >= stamps[("slow_read", "end")], (
        "the barrier started before the group ahead of it completed"
    )
    assert stamps[("fast_read", "start")] >= stamps[("exclusive_op", "end")], (
        "a call after the barrier started before the barrier ended"
    )
    # The tail pair still overlaps: deliberate divergence from Pi's
    # whole-batch degradation.
    assert stamps[("quick_read", "start")] < stamps[("fast_read", "end")], (
        "parallel calls after the barrier serialized; concurrency was "
        "given up for no added safety"
    )
    [batch_results] = list(_result_ids(seen[1]))
    assert batch_results == ["p1", "ex", "p2", "p3"]


def test_a_mid_batch_steer_lands_between_rounds(tmp_path):
    log, seen = [], []
    box = {}

    def responder(kwargs):
        seen.append(kwargs["messages"])
        if len(seen) == 1:
            box["session"].steer("change of plan")
            return [text("batch"),
                    tool("slow_read", _id="a"),
                    tool("fast_read", _id="b")], "tool_use"
        return [text("done")], "end_turn"

    session = box["session"] = _manager(tmp_path, responder, log).create()
    asyncio.run(session.run("go"))

    # The batch's result message is pure tool_results...
    [batch_results] = list(_result_ids(seen[1]))
    assert batch_results == ["a", "b"]
    # ...and the interjection arrives as its own later message.
    interjections = [
        m for m in seen[1]
        if isinstance(m.get("content"), str)
        and "<user_interjection>" in m["content"]
    ]
    assert interjections, "the steer never reached the next round"
