"""One session, one process at a time.

Making state durable made a second process possible, and nothing stopped both
from advancing the same session. Measured before the lease existed: two managers
on one database restored the same session, ran concurrently, and produced a
single transcript containing two consecutive user turns and orphaned tool
results -- the exact shape a provider rejects with `tool_use ids were found
without tool_result blocks`.

Failing the second run is the lesser outcome.
"""

import asyncio
import time
from pathlib import Path

import pytest

from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.manager import SessionManager
from mini_loop.session import LeaseLost
from mini_loop.storage import SQLiteStateStore, SessionRecord

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path):
    return Settings(
        fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR
    )


def _manager(tmp_path, db):
    return SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), state_store=SQLiteStateStore(db)
    )


def _record(session_id="s1"):
    return SessionRecord(session_id, "/w", None, 1.0, 0, "idle", 0)


# --- the primitive ----------------------------------------------------------

def test_only_one_owner_can_hold_a_lease(tmp_path):
    store = SQLiteStateStore(tmp_path / "s.db")
    store.upsert_session(_record())
    assert store.acquire_lease("s1", "A", ttl=60) is True
    assert store.acquire_lease("s1", "B", ttl=60) is False
    assert store.lease_holder("s1") == "A"
    store.close()


def test_reacquiring_your_own_lease_is_not_a_conflict(tmp_path):
    store = SQLiteStateStore(tmp_path / "s.db")
    store.upsert_session(_record())
    assert store.acquire_lease("s1", "A", ttl=60) is True
    assert store.acquire_lease("s1", "A", ttl=60) is True
    store.close()


def test_an_expired_lease_can_be_taken_over(tmp_path):
    """A crashed process leaves its lease behind; the TTL is what frees it."""
    store = SQLiteStateStore(tmp_path / "s.db")
    store.upsert_session(_record())
    assert store.acquire_lease("s1", "dead", ttl=-1) is True  # already expired
    assert store.lease_holder("s1") is None
    assert store.acquire_lease("s1", "live", ttl=60) is True
    store.close()


def test_a_lost_lease_cannot_be_renewed(tmp_path):
    """Renewal must not silently re-take what someone else now holds."""
    store = SQLiteStateStore(tmp_path / "s.db")
    store.upsert_session(_record())
    store.acquire_lease("s1", "A", ttl=-1)
    store.acquire_lease("s1", "B", ttl=60)
    assert store.renew_lease("s1", "A", ttl=60) is False
    assert store.lease_holder("s1") == "B"
    store.close()


def test_releasing_someone_elses_lease_does_nothing(tmp_path):
    store = SQLiteStateStore(tmp_path / "s.db")
    store.upsert_session(_record())
    store.acquire_lease("s1", "A", ttl=60)
    store.release_lease("s1", "B")
    assert store.lease_holder("s1") == "A"
    store.close()


def test_acquisition_is_atomic_under_contention(tmp_path):
    """Read-then-write would let both callers see it free."""
    import threading

    store = SQLiteStateStore(tmp_path / "s.db")
    store.upsert_session(_record())
    winners: list[str] = []
    barrier = threading.Barrier(8)

    def contend(name: str):
        barrier.wait()
        if store.acquire_lease("s1", name, ttl=60):
            winners.append(name)

    threads = [threading.Thread(target=contend, args=(f"p{i}",)) for i in range(8)]
    for t in threads:
        t.start()
    for t in threads:
        t.join()

    assert len(winners) == 1, f"{len(winners)} processes each believed they won"
    store.close()


# --- through the manager ----------------------------------------------------

def test_a_second_process_refuses_to_advance_the_same_session(tmp_path):
    db = tmp_path / "shared.db"
    first = _manager(tmp_path, db)
    session = first.create()
    session_id = session.id
    asyncio.run(session.run("first"))

    second = _manager(tmp_path, db)
    theirs = second.restore_scheduled_session(session_id)
    with pytest.raises(LeaseLost) as denial:
        asyncio.run(theirs.run("from the other process"))
    assert first.instance_id in str(denial.value)

    asyncio.run(first.stop())
    asyncio.run(second.stop())


def test_a_clean_shutdown_hands_the_session_over(tmp_path):
    """A crash waits out the TTL; an orderly stop should not."""
    db = tmp_path / "shared.db"
    first = _manager(tmp_path, db)
    session_id = first.create().id
    asyncio.run(first.stop())

    second = _manager(tmp_path, db)
    revived = second.restore_scheduled_session(session_id)
    asyncio.run(revived.run("continues here"))  # no LeaseLost
    asyncio.run(second.stop())


def test_the_holder_keeps_working_across_turns(tmp_path):
    """Renewal rides the flush, so a long session cannot lapse under itself."""
    db = tmp_path / "shared.db"
    manager = _manager(tmp_path, db)
    session = manager.create()
    session.lease_ttl = 30.0
    for turn in range(3):
        asyncio.run(session.run(f"turn {turn}"))
    assert manager.state_store.lease_holder(session.id) == manager.instance_id
    asyncio.run(manager.stop())


def test_no_store_means_no_lease(tmp_path):
    """A NullStateStore has no second process to race."""
    manager = SessionManager(_settings(tmp_path), FakeAsyncAnthropic())
    session = manager.create()
    assert session.lease_owner is None
    asyncio.run(session.run("go"))  # must not raise
    asyncio.run(manager.stop())


# --- a held lease lost mid-turn, vs a claim that never won -------------------

def test_a_held_lease_lost_mid_turn_stops_the_session(tmp_path):
    """`_require_lease` guards the turn's start; renewal per persistence beat
    guards its length -- but the renewal result was discarded, so a lease that
    lapsed under a long, event-quiet operation and was taken by another process
    went undetected, and this process kept appending to a transcript it no
    longer owned. A lease this process actually held and then lost now raises."""
    manager = _manager(tmp_path, tmp_path / "s.db")
    store = manager.state_store
    session = manager.create()
    # `create` already confirms via `_claim` (round 158); reset so this pins the
    # *other* confirmation site -- `_require_lease` acquiring at the turn's start,
    # the path a session whose claim failed then relies on.
    session.lease_confirmed = False
    session._require_lease()               # we hold it now
    assert session.lease_confirmed is True

    # Another process takes it: expire ours, then a different owner acquires.
    store.acquire_lease(session.id, session.lease_owner, ttl=-1)
    store.acquire_lease(session.id, "process-2", ttl=60)

    with pytest.raises(LeaseLost):
        session._renew_lease()
    asyncio.run(manager.stop())


def test_a_claim_that_never_won_does_not_raise_on_renewal(tmp_path):
    """A restored session whose claim lost to a still-held lease never held one,
    so a failing renewal is not a loss -- it must not raise, or a legitimate
    restart would fail on a lease it was never going to win."""
    manager = _manager(tmp_path, tmp_path / "s.db")
    store = manager.state_store
    session = manager.create()
    session.lease_confirmed = False        # assigned an owner, never acquired

    store.acquire_lease(session.id, session.lease_owner, ttl=-1)
    store.acquire_lease(session.id, "process-2", ttl=60)

    session._renew_lease()                 # must not raise
    asyncio.run(manager.stop())


def test_a_successful_claim_confirms_the_lease_a_lost_one_does_not(tmp_path):
    """`_claim` discarded its acquire result, so a session it successfully took
    the lease for stayed unconfirmed until its first `_require_lease` -- which a
    session driven straight through `agent.run` never reaches, leaving a mid-turn
    loss undetectable. A successful claim now records the hold; a claim that lost
    stays unconfirmed rather than pretending to hold a lease another owns."""
    manager = _manager(tmp_path, tmp_path / "s.db")
    session = manager.create()
    assert manager.state_store.lease_holder(session.id) == session.lease_owner
    assert session.lease_confirmed is True

    # A second process on the same database cannot claim the still-held lease.
    other = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(),
        state_store=SQLiteStateStore(tmp_path / "s.db"),
    )
    session.lease_confirmed = False       # reset to observe the claim's outcome
    other._claim(session)
    assert session.lease_confirmed is False
    assert other.state_store.lease_holder(session.id) == manager.instance_id

    asyncio.run(manager.stop())
    asyncio.run(other.stop())
