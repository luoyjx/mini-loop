"""Storage file-fault census: what a broken state.db does at startup.

The suite covers the store's *logic* thoroughly -- kill-points, idempotent
restore, schema upgrades, degraded mode behind a failing store object.
This census covers the *file*: garbage bytes, a truncated database, a
read-only volume, and the lease contract's missing-session edge. One fix
landed with it: a corrupt state file used to surface as sqlite's bare
"file is not a database" traceback; it now refuses with the file named,
the likely cause, and the remedy -- the same refuse-and-say-how-to-fix
shape as the newer-schema refusal beside it.
"""

import os
import sqlite3
import time

import pytest

from mini_loop.storage import SessionRecord, SQLiteStateStore, StorageSchemaError


def _record(session_id="sess1"):
    return SessionRecord(
        session_id=session_id, workspace="/tmp/ws", system=None,
        created_at=time.time(), run_count=0, status="idle", event_cursor=0,
    )


def test_a_corrupt_database_refuses_with_advice(tmp_path):
    garbage = tmp_path / "garbage.db"
    garbage.write_bytes(b"\xff\xfe not a database \x00" * 8)
    with pytest.raises(StorageSchemaError) as caught:
        SQLiteStateStore(garbage)
    message = str(caught.value)
    assert str(garbage) in message
    assert "Move it aside" in message, "the refusal must name the remedy"


def test_a_truncated_database_refuses_the_same_way(tmp_path):
    path = tmp_path / "half.db"
    store = SQLiteStateStore(path)
    for index in range(50):
        store.append_event("s", {"type": "e", "i": index})
    store.close()
    data = path.read_bytes()
    path.write_bytes(data[: len(data) // 2])
    with pytest.raises(StorageSchemaError):
        SQLiteStateStore(path)


@pytest.mark.skipif(os.geteuid() == 0, reason="root ignores file modes")
def test_a_readonly_database_opens_and_fails_loudly_on_write(tmp_path):
    """Environmental failures stay OperationalError -- the corrupt-file
    advice would be the wrong remedy -- and the degraded-store seam
    (test_persistence_failure_does_not_stall_the_agent) owns the
    session-level behavior."""

    path = tmp_path / "ro.db"
    SQLiteStateStore(path).close()
    os.chmod(path, 0o444)
    try:
        store = SQLiteStateStore(path)
        with pytest.raises(sqlite3.OperationalError):
            store.append_event("sess", {"type": "x"})
        store.close()
    finally:
        os.chmod(path, 0o644)


def test_a_lease_on_a_missing_session_fails_closed(tmp_path):
    """The lease is an UPDATE on the sessions row: acquiring against a
    session that does not exist answers False -- indistinguishable from
    'held by someone else', and deliberately so: missing must never read
    as free."""

    store = SQLiteStateStore(tmp_path / "state.db")
    assert store.acquire_lease("ghost", "proc-a", ttl=60) is False
    assert store.lease_holder("ghost") is None

    store.upsert_session(_record("real"))
    assert store.acquire_lease("real", "proc-a", ttl=60) is True
    assert store.acquire_lease("real", "proc-b", ttl=60) is False
    assert store.lease_holder("real") == "proc-a"
    assert store.renew_lease("real", "proc-a", ttl=60) is True
    assert store.renew_lease("real", "proc-b", ttl=60) is False
    store.release_lease("real", "proc-a")
    assert store.acquire_lease("real", "proc-b", ttl=60) is True
    store.close()
