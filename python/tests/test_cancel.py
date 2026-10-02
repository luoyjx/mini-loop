"""Stopping a turn that is already running.

There was no way to. A runaway agent could only be ended by killing the process,
and a second request to the same session queued on its lock with no timeout and
no visibility -- the connection simply hung.

Cancelling mid-turn also has to leave the session *usable*: an interrupted turn
can end between dispatching a tool and recording its result, which is the
unanswered-`tool_use` shape a provider rejects with `tool_use ids were found
without tool_result blocks`.
"""

import asyncio
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from mini_loop.auth import NullAuth
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
from mini_loop.manager import SessionManager
from mini_loop.server import create_app

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _long_running(kwargs: dict):
    """Keeps going. Commands vary so the loop detector does not end it first --
    that is a different mechanism and would mask what is under test."""
    if not kwargs.get("tools"):
        return [text("[summary]")], "end_turn"
    n = len(kwargs["messages"])
    return [tool("bash", _id=f"t{n}", command=f"echo step-{n}")], "tool_use"


def _manager(tmp_path, responder=_long_running):
    return SessionManager(
        Settings(
            fake_llm=True,
            workspace_root=tmp_path / "ws",
            skills_dir=SKILLS_DIR,
            max_turns=500,
        ),
        FakeAsyncAnthropic(responder=responder, delay=0.02),
    )


def _unanswered(messages) -> set[str]:
    open_calls: set[str] = set()
    for message in messages:
        content = message.get("content")
        for block in content if isinstance(content, list) else []:
            kind = block.get("type") if isinstance(block, dict) else getattr(block, "type", None)
            if kind == "tool_use":
                open_calls.add(block["id"] if isinstance(block, dict) else block.id)
            elif kind == "tool_result":
                open_calls.discard(
                    block["tool_use_id"] if isinstance(block, dict) else block.tool_use_id
                )
    return open_calls


async def _cancel_mid_turn(session, reason="stop", *, after_tool_call=False):
    """Cancel a live turn, optionally once a tool call is actually in flight.

    Cancelling before any tool has been dispatched leaves nothing to repair, so
    a test of the repair has to wait for one.
    """

    task = asyncio.create_task(session.run("go"))
    for _ in range(300):
        await asyncio.sleep(0.01)
        if not session.busy:
            continue
        if not after_tool_call or _unanswered(session.agent.messages):
            break
    assert session.busy, "the turn never started"
    if after_tool_call:
        assert _unanswered(session.agent.messages), "no tool call was in flight"
    stopped = await session.cancel(reason)
    return task, stopped


def test_a_running_turn_can_be_stopped(tmp_path):
    async def scenario():
        manager = _manager(tmp_path)
        session = manager.create()
        _, stopped = await _cancel_mid_turn(session)
        assert stopped is True
        assert session.busy is False
        assert session.status == "idle"
        await manager.stop()

    asyncio.run(scenario())


def test_cancelling_leaves_a_transcript_a_provider_would_accept(tmp_path):
    """The repair that makes cancellation safe rather than merely possible."""

    async def scenario():
        manager = _manager(tmp_path)
        session = manager.create()
        await _cancel_mid_turn(session, after_tool_call=True)
        assert _unanswered(session.agent.messages) == set()
        assert "[unknown]" in str(session.agent.messages[-1])
        assert "Do not retry" in str(session.agent.messages[-1])
        await manager.stop()

    asyncio.run(scenario())


def test_the_session_still_works_afterwards(tmp_path):
    async def scenario():
        manager = _manager(tmp_path)
        session = manager.create()
        await _cancel_mid_turn(session)

        def finish(kwargs):
            if not kwargs.get("tools"):
                return [text("[summary]")], "end_turn"
            return [text("recovered")], "end_turn"

        session.agent.client = FakeAsyncAnthropic(responder=finish)
        assert "recovered" in await session.run("still there?")
        await manager.stop()

    asyncio.run(scenario())


def test_cancelling_an_idle_session_is_a_no_op(tmp_path):
    async def scenario():
        manager = _manager(tmp_path)
        session = manager.create()
        assert await session.cancel() is False
        await manager.stop()

    asyncio.run(scenario())


def test_the_reason_is_reported(tmp_path):
    async def scenario():
        manager = _manager(tmp_path)
        session = manager.create()
        events: list[dict] = []
        queue = session.subscribe(replay=False)

        async def drain():
            while True:
                events.append(await queue.get())

        pump = asyncio.create_task(drain())
        await _cancel_mid_turn(session, reason="operator pressed stop")
        await asyncio.sleep(0.05)
        pump.cancel()

        cancelled = [e for e in events if e["type"] == "cancelled"]
        assert cancelled and cancelled[-1]["reason"] == "operator pressed stop"
        assert session.info()["cancel_reason"] == "operator pressed stop"
        await manager.stop()

    asyncio.run(scenario())


# --- over HTTP -------------------------------------------------------------

def test_a_second_request_is_refused_rather_than_hung(tmp_path):
    """Queueing on the lock held the connection open with no timeout."""
    manager = _manager(tmp_path)
    app = create_app(manager=manager)
    with TestClient(app) as client:
        app.state.auth = NullAuth()
        session_id = client.post("/sessions", json={}).json()["id"]
        session = manager.get(session_id)

        # Pretend a turn is in flight without actually racing the test client.
        class _Busy:
            def done(self):
                return False

        session._running = _Busy()
        response = client.post(
            f"/sessions/{session_id}/messages", json={"message": "second"}
        )
        assert response.status_code == 409
        assert "cancel" in response.json()["detail"]
        session._running = None


def test_cancel_is_reachable_over_http(tmp_path):
    manager = _manager(tmp_path)
    app = create_app(manager=manager)
    with TestClient(app) as client:
        app.state.auth = NullAuth()
        session_id = client.post("/sessions", json={}).json()["id"]
        body = client.post(f"/sessions/{session_id}/cancel").json()
        assert body["cancelled"] is False  # nothing was running
        assert body["info"]["busy"] is False


def test_unanswered_tool_uses_reads_both_block_shapes():
    """A live transcript holds provider objects; a restored one holds dicts.

    Written for dicts only, this found nothing on a live session -- so
    cancelling repaired nothing, and only a real provider would have said so.
    """
    from mini_loop.fake_llm import ToolUseBlock
    from mini_loop.session import AgentSession

    live = [{"role": "assistant", "content": [ToolUseBlock("bash", {}, "tu_1")]}]
    restored = [{"role": "assistant", "content": [
        {"type": "tool_use", "id": "tu_1", "name": "bash", "input": {}}
    ]}]
    assert AgentSession._unanswered_tool_uses(live) == ["tu_1"]
    assert AgentSession._unanswered_tool_uses(restored) == ["tu_1"]
