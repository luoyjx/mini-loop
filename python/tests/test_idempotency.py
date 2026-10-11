"""POST /messages honors an Idempotency-Key (round 231, roadmap G4).

A double-submit -- a network retry, a double-click -- must not run a
possibly non-idempotent turn twice. The key returns the first result;
without it, each POST is a fresh turn (unchanged behavior). The key is
scoped to (owner, session) so one caller's key can never read another's.
"""

import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"

ALICE = {"Authorization": "Bearer tok-alice"}
BOB = {"Authorization": "Bearer tok-bob"}


@pytest.fixture
def client(tmp_path):
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.server import create_app

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
    )
    app = create_app(manager=manager)
    with TestClient(app) as c:
        app.state.auth = TokenAuth({"tok-alice": "alice", "tok-bob": "bob"})
        c._manager = manager
        yield c


def _sid(client, headers):
    return client.post("/sessions", json={}, headers=headers).json()["id"]


def test_a_repeated_key_runs_the_turn_once(client):
    sid = _sid(client, ALICE)
    h = {**ALICE, "Idempotency-Key": "abc-123"}
    first = client.post(f"/sessions/{sid}/messages", json={"message": "do it"}, headers=h)
    runs_after_first = client._manager._sessions[sid].run_count
    second = client.post(f"/sessions/{sid}/messages", json={"message": "do it"}, headers=h)
    runs_after_second = client._manager._sessions[sid].run_count

    assert first.status_code == second.status_code == 200
    assert second.json()["final"] == first.json()["final"]
    assert runs_after_second == runs_after_first, "the turn ran twice for one key"


def test_no_key_is_the_unchanged_fresh_turn(client):
    sid = _sid(client, ALICE)
    client.post(f"/sessions/{sid}/messages", json={"message": "one"}, headers=ALICE)
    client.post(f"/sessions/{sid}/messages", json={"message": "two"}, headers=ALICE)
    assert client._manager._sessions[sid].run_count == 2


def test_a_different_key_runs_again(client):
    sid = _sid(client, ALICE)
    client.post(f"/sessions/{sid}/messages", json={"message": "x"},
                headers={**ALICE, "Idempotency-Key": "k1"})
    client.post(f"/sessions/{sid}/messages", json={"message": "x"},
                headers={**ALICE, "Idempotency-Key": "k2"})
    assert client._manager._sessions[sid].run_count == 2


def test_a_key_is_scoped_to_its_owner_and_session(client):
    """Bob reusing Alice's key on his own session gets his own turn, never
    Alice's cached result. Defense in depth: the key carries the owner AND
    a caller can only POST to their own sessions (session ids are unique
    per owner), so a single mutation dropping the owner component SURVIVES
    -- documented, not guarded (the round-223/226 lesson)."""
    alice_sid = _sid(client, ALICE)
    client.post(f"/sessions/{alice_sid}/messages", json={"message": "alice secret"},
                headers={**ALICE, "Idempotency-Key": "shared"})
    bob_sid = _sid(client, BOB)
    reply = client.post(f"/sessions/{bob_sid}/messages", json={"message": "bob work"},
                        headers={**BOB, "Idempotency-Key": "shared"})
    assert reply.status_code == 200
    assert client._manager._sessions[bob_sid].run_count == 1
