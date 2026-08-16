"""Input for a busy session is steering, not a 409.

OpenWorker's gateway rule (OPENWORKER_RESEARCH.md 6.4): a message for a busy
session becomes steering delivered into the running turn; only an idle session
starts a fresh turn. Ours answered 409 and dropped the caller's words on the
floor -- correct about the lock, wrong about the words.

`AgentSession.steer()` queues text from any context; `steering_injector`
drains the queue at the agent's next loop round, so a mid-turn steer reaches
the model mid-turn and an idle steer opens the next turn. The injector seam
was built for exactly this shape (background results and team inboxes already
ride it); steering adds the human to the list of things that can interject.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path, responder):
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    return SessionManager(settings, FakeAsyncAnthropic(responder=responder))


def _interjections(request_messages):
    return [
        m["content"] for m in request_messages
        if isinstance(m.get("content"), str)
        and "<user_interjection>" in m["content"]
    ]


@pytest.mark.asyncio
async def test_a_mid_turn_steer_reaches_the_model_mid_turn(tmp_path):
    seen = []
    session_box = {}

    def responder(kwargs):
        seen.append(kwargs["messages"])
        if len(seen) == 1:
            # Steer while the turn is unquestionably still running.
            session_box["session"].steer("actually, use the staging target")
            return [tool("bash", command="echo step", _id="t1")], "tool_use"
        return [text("done")], "end_turn"

    manager = _manager(tmp_path, responder)
    session = session_box["session"] = manager.create()

    await asyncio.wait_for(session.run("deploy"), timeout=10)

    assert not _interjections(seen[0]), "the steer arrived before it was sent"
    later = [m for request in seen[1:] for m in _interjections(request)]
    assert any("staging target" in m for m in later), (
        "the mid-turn steer never reached the model"
    )
    assert session.info()["pending_steering"] == 0


@pytest.mark.asyncio
async def test_an_idle_steer_opens_the_next_turn(tmp_path):
    seen = []

    def responder(kwargs):
        seen.append(kwargs["messages"])
        return [text("ok")], "end_turn"

    manager = _manager(tmp_path, responder)
    session = manager.create()
    session.steer("remember the deadline is friday")
    assert session.info()["pending_steering"] == 1

    await asyncio.wait_for(session.run("plan the week"), timeout=10)

    assert any("deadline is friday" in m for m in _interjections(seen[0]))


@pytest.mark.asyncio
async def test_steers_arrive_once_in_order(tmp_path):
    seen = []

    def responder(kwargs):
        seen.append(kwargs["messages"])
        return [text("ok")], "end_turn"

    manager = _manager(tmp_path, responder)
    session = manager.create()
    session.steer("first")
    session.steer("second")

    await asyncio.wait_for(session.run("go"), timeout=10)
    [interjection] = _interjections(seen[0])
    assert interjection.index("first") < interjection.index("second")

    await asyncio.wait_for(session.run("again"), timeout=10)
    assert not [m for m in _interjections(seen[-1]) if "first" in m] or (
        # The transcript keeps the delivered interjection; what must not
        # happen is a *second* delivery message.
        sum("<user_interjection>" in str(m.get("content", ""))
            for m in seen[-1]) == 1
    )


@pytest.mark.asyncio
async def test_steering_is_bounded_in_size_and_count(tmp_path):
    """Every queued steer is joined into one injected `<user_interjection>`, so
    an oversized steer -- or an unbounded number of them on a busy session --
    floods the context exactly as an unbounded team message did (round 50). The
    per-steer text is truncated and the queue is capped, dropping the oldest so
    the caller's latest redirection is the one that survives.
    """
    from mini_loop.session import MAX_STEER_CHARS, MAX_STEER_QUEUE

    session = _manager(tmp_path, lambda kwargs: ([text("ok")], "end_turn")).create()
    session.steer("X" * (MAX_STEER_CHARS * 3))       # one oversized steer
    for i in range(MAX_STEER_QUEUE + 500):           # then flood the queue
        session.steer(f"m{i}")

    assert len(session._steering) == MAX_STEER_QUEUE, "the queue grew past its bound"
    assert all(len(s) <= MAX_STEER_CHARS + 32 for s in session._steering), (
        "a queued steer exceeded the per-message bound"
    )
    assert session._steering[-1] == f"m{MAX_STEER_QUEUE + 499}", "the newest steer was dropped"


@pytest.mark.asyncio
async def test_an_oversized_steer_is_truncated_with_a_marker(tmp_path):
    """Not silently dropped: it still fires, partially, and the marker tells the
    model (and the user, who is present) that it was cut."""
    from mini_loop.session import MAX_STEER_CHARS

    session = _manager(tmp_path, lambda kwargs: ([text("ok")], "end_turn")).create()
    session.steer("Y" * 100_000)
    [queued] = session._steering
    assert len(queued) <= MAX_STEER_CHARS + 32
    assert queued.endswith("[steer truncated]")


def test_steering_over_http_is_owner_scoped(tmp_path):
    from fastapi.testclient import TestClient

    from mini_loop.auth import TokenAuth
    from mini_loop.server import create_app

    alice, bob = "tok-alice-000000000000", "tok-bob-1111111111111"
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    manager = SessionManager(settings, FakeAsyncAnthropic())
    app = create_app(manager=manager)
    with TestClient(app) as client:
        app.state.auth = TokenAuth({alice: "alice", bob: "bob"})
        headers = {"Authorization": f"Bearer {alice}"}
        session_id = client.post("/sessions", json={}, headers=headers).json()["id"]

        reply = client.post(f"/sessions/{session_id}/steer",
                            json={"message": "go faster"}, headers=headers)
        assert reply.status_code == 200
        # Idle since round 194: the words become a turn, not a parked queue.
        assert reply.json()["delivered"] == "new_turn"

        foreign = {"Authorization": f"Bearer {bob}"}
        assert client.post(f"/sessions/{session_id}/steer",
                           json={"message": "pwn"}, headers=foreign).status_code == 404
        assert manager._sessions[session_id]._steering == []


# -- durability (round 192) --------------------------------------------------
# `steer()` answers "queued" -- a promise -- and the queue was memory-only:
# an idle session's steer waits for the next run, and the process that said
# "queued: 1" need not be the one that runs it. The queue now rides the
# session record (persisted on the same sync beat as every flush), restore
# reseeds it, and delivery clears it through the flush that persists the
# interjection -- so a crash between queue and delivery re-delivers instead
# of silently dropping the caller's words.

def _store_manager(tmp_path, responder):
    from mini_loop.storage import SQLiteStateStore

    store = SQLiteStateStore(tmp_path / "state.db")
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    return store, SessionManager(settings, FakeAsyncAnthropic(responder=responder),
                                 state_store=store)


def _handoff(store, session):
    """Release the lease the way a clean shutdown does."""
    store.release_lease(session.id, session.lease_owner)
    session.lease_owner = None


@pytest.mark.asyncio
async def test_a_queued_steer_survives_a_restart(tmp_path):
    seen = []

    def responder(kwargs):
        seen.append(kwargs["messages"])
        return [text("ok")], "end_turn"

    store, manager = _store_manager(tmp_path, responder)
    session = manager.create()
    await session.run("set up")          # give the session a durable life
    session.steer("the deadline moved to monday")
    _handoff(store, session)

    second = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(responder=responder), state_store=store,
    )
    restored = second.restore_scheduled_session(session.id)
    assert restored.info()["pending_steering"] == 1
    await restored.run("continue")
    assert any("deadline moved to monday" in m for m in _interjections(seen[-1])), (
        "the promised steer vanished across the restart"
    )


@pytest.mark.asyncio
async def test_a_delivered_steer_is_not_redelivered_after_a_restart(tmp_path):
    seen = []

    def responder(kwargs):
        seen.append(kwargs["messages"])
        return [text("ok")], "end_turn"

    store, manager = _store_manager(tmp_path, responder)
    session = manager.create()
    session.steer("only once")
    await session.run("go")              # delivered here; flush clears the record
    _handoff(store, session)

    second = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(responder=responder), state_store=store,
    )
    restored = second.restore_scheduled_session(session.id)
    assert restored.info()["pending_steering"] == 0
    await restored.run("again")
    delivered = sum(
        "only once" in str(m.get("content", "")) for m in seen[-1]
    )
    assert delivered == 1, "a delivered steer must not arrive twice"


@pytest.mark.asyncio
async def test_the_durable_queue_holds_the_masked_form(tmp_path):
    from mini_loop.secrets import SecretRegistry
    from mini_loop.storage import SQLiteStateStore

    canary = "sk-STEER-CANARY-0123456789abcdef"
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(responder=lambda kwargs: ([text("ok")], "end_turn")),
        secrets=SecretRegistry.from_environ(environ={"CANARY_API_KEY": canary}),
        state_store=store,
    )
    session = manager.create()
    session.steer(f"use {canary} for the deploy")

    [record] = [r for r in store.load_sessions() if r.session_id == session.id]
    assert record.pending_steering, "the steer never reached the record"
    assert canary not in record.pending_steering[0], (
        "a pasted secret reached the sessions table raw"
    )
    # The live queue keeps the raw text for same-process delivery.
    assert canary in session._steering[0]


# -- idle delivery over HTTP (round 194) -------------------------------------
# dsh's unified-send note: steer is `next-step` WITH wakeup -- an idle agent
# hears a steer by running. OpenWorker's original rule says the same from
# the other side: only a busy session's message becomes steering; an idle
# session starts a fresh turn. Ours parked idle steers until the next run --
# durable after round 192, so the parked words could wait forever while the
# HTTP caller believed they were on their way. Over HTTP, idle now runs the
# message as an ordinary turn; `session.steer()` keeps parking semantics for
# process-local callers (the sync contract the mid-turn test relies on).

def test_an_idle_http_steer_starts_a_turn(tmp_path):
    import time

    from fastapi.testclient import TestClient

    from mini_loop.server import create_app

    seen = []

    def responder(kwargs):
        seen.append(kwargs["messages"])
        return [text("heard you")], "end_turn"

    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    manager = SessionManager(settings, FakeAsyncAnthropic(responder=responder))
    app = create_app(manager=manager)
    with TestClient(app) as client:
        session_id = client.post("/sessions", json={}).json()["id"]
        reply = client.post(f"/sessions/{session_id}/steer",
                            json={"message": "check the deploy"}).json()
        assert reply["delivered"] == "new_turn"

        session = manager._sessions[session_id]
        for _ in range(400):
            if session.run_count and not session.busy:
                break
            time.sleep(0.005)
        assert session.run_count == 1, "the idle steer never started a turn"
        assert any(
            "check the deploy" in str(m.get("content", "")) for m in seen[0]
        ), "the steered text never reached the model"
        assert session.info()["pending_steering"] == 0


@pytest.mark.asyncio
async def test_the_delivery_event_carries_the_words(tmp_path):
    """Observers see WHAT was steered, not only that steering happened."""
    events = []

    def responder(kwargs):
        return [text("ok")], "end_turn"

    manager = _manager(tmp_path, responder)
    session = manager.create()
    queue = session.subscribe()
    session.steer("switch to the hotfix branch")
    await asyncio.wait_for(session.run("carry on"), timeout=10)
    while not queue.empty():
        events.append(queue.get_nowait())
    [delivered] = [e for e in events if e["type"] == "steering_delivered"]
    assert delivered["count"] == 1
    assert "hotfix branch" in delivered["text"]
