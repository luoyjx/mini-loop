"""Ownership that survives the process, which round 74's did not.

Round 74 closed a real disclosure -- any authenticated caller could read anyone
else's recorded conversation -- by resolving the owner from live sessions, and
recorded the gap that left: the mapping is per-process, so after a restart the
check fails closed and a trajectory becomes unreadable **by the person who made
it**. Fail-closed is the right direction for an access check and the wrong
outcome for the owner.

The owner is now written into the trajectory record itself, first-class rather
than tucked into `metadata`, because it is what the access check reads. Verified
against a reloaded server with no session in memory:

    recorded owner in the summary: 'alice'
    after restart:
      alice -> 200  content readable: True
      bob   -> 404  content leaked  : False

Records written before the field existed keep working through the session
lookup, and that path is exercised directly below rather than by stripping files
and hoping the glob matched -- a first attempt reported "stripped owner from 0
files" and would have claimed compatibility it never tested.
"""

import importlib
import pathlib

import pytest

ALICE = {"Authorization": "Bearer tok-alice"}
BOB = {"Authorization": "Bearer tok-bob"}
CONFIDENTIAL = "ALICE-CONFIDENTIAL-PLAN"


@pytest.fixture
def env(monkeypatch, tmp_path):
    monkeypatch.setenv("MINILOOP_FAKE_LLM", "1")
    monkeypatch.setenv("MINILOOP_WORKSPACE_ROOT", str(tmp_path / "ws"))
    monkeypatch.setenv("MINILOOP_SKILLS_DIR",
                       str(pathlib.Path(__file__).resolve().parent.parent / "skills"))
    monkeypatch.setenv("MINILOOP_API_TOKENS", "alice:tok-alice,bob:tok-bob")
    monkeypatch.setenv("MINILOOP_TRAJECTORY_ENABLED", "1")
    monkeypatch.setenv("MINILOOP_TRAJECTORY_CAPTURE_CONTENT", "1")
    monkeypatch.setenv("MINILOOP_TRAJECTORY_DIR", str(tmp_path / "traj"))
    return tmp_path


def _client():
    from fastapi.testclient import TestClient

    import mini_loop.server as server

    importlib.reload(server)
    return TestClient(server.app)


def _record_a_trajectory(client):
    session_id = client.post("/sessions", json={}, headers=ALICE).json()["id"]
    client.post(f"/sessions/{session_id}/messages",
                json={"message": CONFIDENTIAL}, headers=ALICE)
    listing = client.get(f"/sessions/{session_id}/trajectories", headers=ALICE).json()
    items = listing if isinstance(listing, list) else listing.get("trajectories", [])
    assert items, "nothing was recorded"
    return session_id, items[0]


# --- the owner is on the record ------------------------------------------

def test_the_summary_carries_the_owner(env):
    with _client() as client:
        _, summary = _record_a_trajectory(client)
    assert summary.get("owner") == "alice"


def test_the_full_record_carries_the_owner(env):
    with _client() as client:
        _, summary = _record_a_trajectory(client)
        record = client.get(f"/trajectories/{summary['trajectory_id']}",
                            headers=ALICE).json()
    assert record.get("owner") == "alice"


# --- and it survives the process -----------------------------------------

def test_the_owner_can_still_read_after_a_restart(env):
    """The gap round 74 left open, stated in its own notes."""
    with _client() as client:
        _, summary = _record_a_trajectory(client)
        trajectory_id = summary["trajectory_id"]

    with _client() as restarted:      # fresh app, no session in memory
        response = restarted.get(f"/trajectories/{trajectory_id}", headers=ALICE)
    assert response.status_code == 200
    assert CONFIDENTIAL in response.text


def test_a_stranger_is_still_refused_after_a_restart(env):
    with _client() as client:
        _, summary = _record_a_trajectory(client)
        trajectory_id = summary["trajectory_id"]

    with _client() as restarted:
        response = restarted.get(f"/trajectories/{trajectory_id}", headers=BOB)
    assert response.status_code == 404
    assert CONFIDENTIAL not in response.text


def test_export_is_scoped_after_a_restart_too(env):
    with _client() as client:
        _, summary = _record_a_trajectory(client)
        trajectory_id = summary["trajectory_id"]

    with _client() as restarted:
        assert restarted.get(f"/trajectories/{trajectory_id}/export",
                             headers=ALICE).status_code == 200
        refused = restarted.get(f"/trajectories/{trajectory_id}/export", headers=BOB)
    assert refused.status_code == 404
    assert CONFIDENTIAL not in refused.text


# --- records written before the field existed ----------------------------

def _check(record, caller_id, owned_sessions):
    """Call the access check directly, with a stubbed request."""
    import mini_loop.server as server
    from fastapi import HTTPException

    from mini_loop.auth import Principal

    class Request:
        pass

    request = Request()
    original_principal = server._principal
    original_owned = server._owned_session_ids
    server._principal = lambda _r: Principal(id=caller_id)
    server._owned_session_ids = lambda _r, _c: owned_sessions
    try:
        server._require_owned_trajectory(request, record)
        return True
    except HTTPException:
        return False
    finally:
        server._principal = original_principal
        server._owned_session_ids = original_owned


def test_a_record_without_an_owner_falls_back_to_the_session(env):
    """Exercised directly. A first attempt stripped the field from files on
    disk, matched nothing, and would have reported compatibility it never
    tested."""
    import mini_loop.server  # noqa: F401  (import for the module object)

    legacy = {"trajectory_id": "traj_old", "session": "sess-1"}
    assert _check(legacy, "alice", {"sess-1"}) is True
    assert _check(legacy, "bob", {"sess-2"}) is False


def test_the_recorded_owner_wins_over_the_session_lookup(env):
    """Otherwise a stranger who owns *some* session could read another's."""
    record = {"trajectory_id": "traj_new", "session": "sess-1", "owner": "alice"}
    assert _check(record, "alice", set()) is True
    assert _check(record, "bob", {"sess-1"}) is False


def test_a_record_with_neither_is_refused(env):
    assert _check({"trajectory_id": "traj_orphan"}, "alice", {"sess-1"}) is False
