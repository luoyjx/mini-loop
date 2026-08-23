"""Memory is captured on every healthy turn endpoint, not just the happy
path (round 228, from TENCENTDB_AGENT_MEMORY_RESEARCH.md's gap: capture
was a hook on the normal final-answer path only, so max-rounds and
stuck-halt endpoints -- the turns that did the MOST work -- lost their
memory).

The line: capture where the provider is healthy and the turn did real
work (happy, stuck-halt, round-exhausted). NOT the error exit (provider
may be down) or a cancel (the caller wants an immediate stop).
"""

import asyncio
import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _session(tmp_path, responder, **over):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None, **over),
        FakeAsyncAnthropic(responder=responder),
    ).create()


def _capture_calls(session):
    """Count memory_on_stop invocations by patching it."""
    calls = {"n": 0}
    import mini_loop.memory as memory

    original = memory.memory_on_stop

    async def counting(agent):
        calls["n"] += 1
        return await original(agent)

    memory.memory_on_stop = counting
    return calls, lambda: setattr(memory, "memory_on_stop", original)


def test_the_happy_path_still_captures(tmp_path):
    session = _session(tmp_path, scripted([([text("done")], "end_turn")]))
    calls, restore = _capture_calls(session)
    try:
        asyncio.run(session.run("hi"))
    finally:
        restore()
    assert calls["n"] == 1


def test_round_exhaustion_captures(tmp_path):
    def never_stops(kwargs):
        return [tool("bash", _id="t", command="echo loop")], "tool_use"

    session = _session(tmp_path, never_stops, max_turns=3)
    calls, restore = _capture_calls(session)
    try:
        final = asyncio.run(session.run("go"))
    finally:
        restore()
    assert "stopped after 3 rounds" in final
    assert calls["n"] == 1, "the hardest-working turn captured no memory"


def test_capture_is_contained(tmp_path):
    """A memory failure at an endpoint must not fail the finished turn."""
    session = _session(tmp_path, scripted([([text("done")], "end_turn")]))
    import mini_loop.memory as memory

    original = memory.memory_on_stop

    async def boom(agent):
        raise RuntimeError("extraction model unreachable")

    memory.memory_on_stop = boom
    try:
        result = asyncio.run(session.run("hi"))
    finally:
        memory.memory_on_stop = original
    assert result == "done", "a memory failure broke a completed turn"


def test_exactly_one_capture_per_turn(tmp_path):
    """No endpoint double-captures: each turn reaches exactly one."""
    session = _session(tmp_path, scripted([
        ([text("step"), tool("bash", _id="t", command="echo hi")], "tool_use"),
        ([text("done")], "end_turn"),
    ]))
    calls, restore = _capture_calls(session)
    try:
        asyncio.run(session.run("go"))
    finally:
        restore()
    assert calls["n"] == 1
