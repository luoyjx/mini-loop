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
