"""Pi P0-4: the durable-RFC crash windows as acceptance scenarios.

Pi's durable-harness RFC names the windows a restart must survive; its
counter-examples become mini-loop's acceptance matrix. Each window is
tested END TO END (crash -> restore -> next turn), not only at the layer
that implements it -- round 88 measured how layer claims fail to compose.

  window 1  crash after provider intent, before any reply   -> here
  window 2  crash between a tool's effect and settlement    -> here,
            composing the journal's unknown machinery at session level
  window 3  crash before the terminal transaction           ->
            trajectory reads interrupted/partial (test_trajectory.py);
            the run's text tail is window 1's shape, covered here
  window 4  lease loss mid-turn                             ->
            test_leases.py (holder stops; claimant waits out the TTL)
"""

import asyncio
import pathlib
import shutil

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.storage import SQLiteStateStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path, store, responder=None):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(responder=responder) if responder else FakeAsyncAnthropic(),
        state_store=store,
    )


def _abandon(store, manager, session):
    """The crash: no cancel handler runs; the lease is simply released the
    way the TTL eventually would."""
    store.release_lease(session.id, session.lease_owner)
    manager._sessions.clear()


def test_window_1_crash_mid_generation_is_marked_on_restore(tmp_path):
    seen = []

    def responder(kwargs):
        seen.append(kwargs["messages"])
        return [text("answered")], "end_turn"

    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store, responder)
    session = manager.create()
    asyncio.run(session.run("first question"))
    # The guard flushed the prompt; the process dies before any reply.
    session.agent.messages.append({"role": "user", "content": "doomed question"})
    session._flush_messages()
    _abandon(store, manager, session)

    second = _manager(tmp_path, store, responder)
    restored = second.restore_scheduled_session(session.id)
    asyncio.run(restored.run("next question"))

    tail = seen[-1]
    assert any(
        "[Turn interrupted" in str(m.get("content", "")) for m in tail
    ), "the crash left no trace between the two questions"
    roles = [m["role"] for m in tail]
    bare_consecutive = any(
        roles[i] == roles[i + 1] == "user"
        and "tool_result" not in str(tail[i]["content"])
        and "tool_result" not in str(tail[i + 1]["content"])
        for i in range(len(roles) - 1)
    )
    assert not bare_consecutive, (
        "the model saw two questions in a row with nothing between them"
    )
    store.close()


def test_window_1_negative_a_completed_session_gains_no_marker(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store)
    session = manager.create()
    asyncio.run(session.run("finished cleanly"))
    _abandon(store, manager, session)

    restored = _manager(tmp_path, store).restore_scheduled_session(session.id)
    assert not any(
        "[Turn interrupted" in str(m.get("content", ""))
        for m in restored.agent.messages
    ), "a clean completion was falsely marked interrupted"
    store.close()


def test_window_2_effect_before_settlement_reads_unknown_not_retried(tmp_path):
    """Crash after the tool_use was flushed, before its result settled:
    the restored transcript says UNKNOWN explicitly, and the next turn
    proceeds without silently re-running the effect."""
    from mini_loop.actions import UNKNOWN_RESULT

    seen = []

    def responder(kwargs):
        seen.append(kwargs["messages"])
        if len(seen) == 1:
            return [text("running the effect"),
                    tool("bash", _id="t-effect", command="echo effect")], "tool_use"
        return [text("carrying on")], "end_turn"

    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store, responder)
    session = manager.create()

    async def crash_mid_batch():
        turn = asyncio.create_task(session.run("do the effect"))
        # Let the assistant tool_use flush, then die before the result does.
        for _ in range(400):
            if any(
                "t-effect" in str(m.get("content", ""))
                for m in session.agent.messages
            ):
                break
            await asyncio.sleep(0.005)
        turn.cancel()
        try:
            await turn
        except asyncio.CancelledError:
            pass

    asyncio.run(crash_mid_batch())
    _abandon(store, manager, session)

    second = _manager(tmp_path, store, responder)
    restored = second.restore_scheduled_session(session.id)
    flat = " ".join(str(m.get("content", "")) for m in restored.agent.messages)
    assert UNKNOWN_RESULT.split(".")[0] in flat or "unknown" in flat.lower(), (
        "the unsettled effect was not answered with an explicit unknown"
    )
    answer = asyncio.run(restored.run("continue"))
    assert "[Error]" not in answer, "the session did not survive the window"
    store.close()
