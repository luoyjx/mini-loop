"""Model-visible means logged -- the transcript invariant.

DeepSeek Harness states this as an architecture rule backed by a runtime
assertion: anything that reaches a model request must be reconstructable
from the durable log. mini-loop's injectors and steering extend
`agent.messages` *between* event beats, so before this invariant the durable
transcript lagged the request by exactly the injected input: a crash at the
wrong moment left a log whose next assistant message answers content the
record never held.

Two halves, both pinned here:
* the guard **flushes** the injected tail before the model sees it (the fix);
* the guard **asserts** the durable epoch covers the request (the alarm),
  and the alarm actually fires when a path bypasses the log.
"""

import asyncio

import pytest

from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.invariants import InvariantError
from mini_loop.manager import SessionManager
from mini_loop.storage import SQLiteStateStore


def _manager(tmp_path, store, **over):
    settings = Settings(
        fake_llm=True,
        workspace_root=tmp_path / "ws",
        spill_dir=None,
        **over,
    )
    return SessionManager(settings, FakeAsyncAnthropic(), state_store=store)


def test_an_injected_message_is_durable_before_the_model_sees_it(tmp_path):
    """The flush half: injectors extend messages between event beats."""

    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store)
    session = manager.create()

    injected = {"count": 0}
    seen_at_request = {}

    async def inject(agent):
        if injected["count"]:
            return None
        injected["count"] += 1
        return [{
            "role": "user",
            "content": [{"type": "text", "text": "background result: build finished"}],
        }]

    real_create = session.agent._create

    async def spying_create(messages, **kwargs):
        # At request time the durable epoch must already hold every message
        # the request carries -- including the one injected microseconds ago.
        seen_at_request["sent"] = len(messages)
        seen_at_request["durable"] = store.message_count(session.id)
        return await real_create(messages, **kwargs)

    session.agent.injectors.insert(0, inject)
    session.agent._create = spying_create
    asyncio.run(session.run("hello"))

    assert injected["count"] == 1
    assert seen_at_request["sent"] >= 2  # user turn + injected input
    assert seen_at_request["durable"] == seen_at_request["sent"]
    store.close()


def test_a_model_visible_input_that_bypasses_the_log_fails_loud(tmp_path):
    """The alarm half: a lying flush is detected, not absorbed."""

    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store)
    session = manager.create()

    # Sabotage: make the flush a no-op, as a future code path that appends to
    # `agent.messages` without ever reaching persistence would.
    session._flush_messages = lambda: None

    with pytest.raises(InvariantError) as failure:
        asyncio.run(session.run("hello"))
    assert "mini_loop.session" in str(failure.value)
    assert "bypassed the log" in str(failure.value)
    store.close()


def test_without_a_durable_store_nothing_is_asserted(tmp_path):
    """A NullStateStore session claims no durability, so none is enforced."""

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", spill_dir=None),
        FakeAsyncAnthropic(),
    )
    session = manager.create()
    result = asyncio.run(session.run("hello"))
    assert result  # ran to completion; the guard stayed out of the way


def test_every_attach_site_inherits_the_guard(tmp_path):
    """The guard rides the property setter, not any one attach site."""

    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store)
    session = manager.create()
    assert session.agent.transcript_guard is not None

    # Reattaching a fresh agent -- as restore and teammate spawn do -- must
    # install the guard again without the call site knowing it exists.
    from mini_loop.session import AgentSession

    other = AgentSession(
        "s-other", tmp_path / "ws" / "s-other", state_store=store
    )
    other.agent = session.agent
    assert session.agent.transcript_guard is not None
    store.close()


# -- the content half (round 190) -------------------------------------------
# Coverage by count was necessary, not sufficient. The flush-time rewrite
# check compares POINTERS, and its docstring states the limit plainly: a
# flushed dict mutated in place is invisible to it. Measured before the fix:
# a hook rewriting messages[0]["content"] mid-turn left memory saying
# "REWRITTEN-BY-HOOK" and disk saying the original -- the guard passed, the
# run completed, and the two durable records (messages table, trajectory
# model_input) contradicted each other. dsh prevents this structurally by
# deep-freezing every message at creation; the mini-loop equivalent is
# detection at the request boundary, where "model-visible means logged"
# already stands guard.

def test_a_flushed_row_mutated_in_place_fails_loud(tmp_path):
    from mini_loop.fake_llm import scripted, text, tool
    from mini_loop.registry import Hook

    store = SQLiteStateStore(tmp_path / "state.db")
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        spill_dir=None)
    client = FakeAsyncAnthropic(responder=scripted([
        ([text("step"), tool("bash", _id="t1", command="echo hi")], "tool_use"),
        ([text("done")], "end_turn"),
    ]))
    manager = SessionManager(settings, client, state_store=store)
    session = manager.create()

    class Vandal(Hook):
        async def before_tool(self, ctx, call):
            messages = ctx.agent.messages
            if messages and messages[0].get("role") == "user":
                messages[0]["content"] = "REWRITTEN-BY-HOOK"
            return None

    session.agent.hooks._hooks.append(Vandal())
    with pytest.raises(InvariantError, match="mutated in place"):
        asyncio.run(session.run("the original question"))


def test_a_sanctioned_replacement_rewrite_still_passes(tmp_path):
    """`messages[:] = [...]` is the legitimate rewrite path: the pointer
    check turns it into a new mirrored epoch, and the digests rebuild."""

    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store)
    session = manager.create()
    asyncio.run(session.run("first question"))

    agent = session.agent
    agent.messages[:] = [
        {"role": "user", "content": "summary of everything so far"},
    ]
    answer = asyncio.run(session.run("second question"))
    assert "[Error]" not in answer
    assert not isinstance(answer, Exception)


def test_restore_seeds_the_digests(tmp_path):
    """Mutation after a restart is caught too, not only in the first life."""

    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, store)
    session = manager.create()
    asyncio.run(session.run("remember this"))
    session_id = session.id
    # Hand the lease back the way a clean shutdown does, so the second
    # process may advance the session rather than waiting out the TTL.
    store.release_lease(session_id, session.lease_owner)
    session.lease_owner = None
    manager._sessions.clear()

    second = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", spill_dir=None),
        FakeAsyncAnthropic(), state_store=store,
    )
    restored = second.restore_scheduled_session(session_id)
    # Half one: an INNOCENT restored session runs clean. Without seeding,
    # the first flush appends digests starting at position 0 while the rows
    # they describe sit at the tail -- misaligned digests false-positive on
    # every restored session, which is how the unseeded mutant announces
    # itself here.
    answer = asyncio.run(restored.run("a clean follow-up"))
    assert "[Error]" not in answer
    # Half two: with digests genuinely seeded, mutation after restart is
    # caught like mutation in the first life.
    restored.agent.messages[0]["content"] = "HISTORY REWRITTEN AFTER RESTART"
    with pytest.raises(InvariantError, match="mutated in place"):
        asyncio.run(restored.run("next question"))


def test_the_content_check_stays_cheap_at_threshold_size():
    """Bounded output is not bounded work -- applied to our own instrument.

    The digest check recomputes over the whole flushed prefix at every model
    request; a 50-round turn re-verifies the same frozen rows 50 times.
    Measured (round 195): 2.2 ms per request on a compaction-threshold-sized
    transcript (300 rows, ~230 KB) -- under 0.1% of a model call. The full
    recompute is the point: in-place mutation preserves pointer identity, so
    nothing cheaper detects it. This bound has ~100x headroom and exists to
    catch an accidental O(N^2) or a pathological serializer regression, not
    to police jitter.
    """
    import time as _time

    from mini_loop.session import _row_digest

    rows = []
    for i in range(300):
        if i % 3 == 0:
            rows.append({"role": "assistant", "content": [
                {"type": "tool_use", "id": f"t{i}", "name": "bash",
                 "input": {"command": "make test 2>&1 | tail -40"}}]})
        elif i % 3 == 1:
            rows.append({"role": "user", "content": [
                {"type": "tool_result", "tool_use_id": f"t{i-1}",
                 "content": "line of build output\n" * 60}]})
        else:
            rows.append({"role": "assistant", "content": [
                {"type": "text", "text": "analysis paragraph " * 40}]})

    start = _time.perf_counter()
    for row in rows:
        _row_digest(row)
    elapsed_ms = (_time.perf_counter() - start) * 1000
    assert elapsed_ms < 200, (
        f"digesting a threshold-size transcript took {elapsed_ms:.0f} ms; "
        "the content check has regressed far past its measured budget"
    )
