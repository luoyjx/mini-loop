"""The listing gated instead of filtering, and two probes missed it.

Round 74 found the trajectory *fetch* unscoped and checked the listing at the
same time: `GET /trajectories` did not show Bob another caller's trajectories,
so it looked right. Round 76 checked the query-parameter form
(`?session_id=<someone else's>`), which correctly 404s, and it still looked
right.

Both probes had Bob owning **no session of his own**, and the handler read:

    if session_id is not None:
        _require(request, session_id)
    elif not _owned_session_ids(request, caller):
        return []
    return store.list(session_id=session_id)      # everything

That `elif` is a "do you own anything at all" gate, not a filter. A caller who
owned nothing got `[]` -- which reads exactly like scoping. **Creating one
session of your own was enough to read every trajectory on the box:**

    bob owns NO session:   GET /trajectories -> sees alice's: False
    bob owns one session:  GET /trajectories -> 2 items, sees alice's: True

Twice in three rounds a fixture that gave the stranger nothing hid an
authorization hole, after round 74's delete bug was hidden by deleting as the
owner first. The fixtures here give every caller their own data precisely so an
"empty" result cannot be mistaken for a scoped one.

The fetch and the listing now share one predicate. They had two rules for the
same question, which is how they came to disagree.
"""

import importlib
import pathlib

import pytest

ALICE = {"Authorization": "Bearer tok-alice"}
BOB = {"Authorization": "Bearer tok-bob"}


@pytest.fixture
def client(monkeypatch, tmp_path):
    monkeypatch.setenv("MINILOOP_FAKE_LLM", "1")
    monkeypatch.setenv("MINILOOP_WORKSPACE_ROOT", str(tmp_path / "ws"))
    monkeypatch.setenv("MINILOOP_SKILLS_DIR",
                       str(pathlib.Path(__file__).resolve().parent.parent / "skills"))
    monkeypatch.setenv("MINILOOP_API_TOKENS", "alice:tok-alice,bob:tok-bob")
    monkeypatch.setenv("MINILOOP_TRAJECTORY_ENABLED", "1")
    monkeypatch.setenv("MINILOOP_TRAJECTORY_CAPTURE_CONTENT", "1")
    monkeypatch.setenv("MINILOOP_TRAJECTORY_DIR", str(tmp_path / "traj"))

    import mini_loop.server as server

    importlib.reload(server)
    from fastapi.testclient import TestClient

    with TestClient(server.app) as test_client:
        yield test_client


def _record(client, headers, message):
    session_id = client.post("/sessions", json={}, headers=headers).json()["id"]
    client.post(f"/sessions/{session_id}/messages",
                json={"message": message}, headers=headers)
    listing = client.get(f"/sessions/{session_id}/trajectories", headers=headers).json()
    items = listing if isinstance(listing, list) else listing.get("trajectories", [])
    assert items, "nothing recorded"
    return session_id, items[0]["trajectory_id"]


@pytest.fixture
def both(client):
    """**Both** callers own data.

    The bug survived two probes because the stranger owned nothing, and an empty
    result is indistinguishable from a filtered one.
    """
    alice = _record(client, ALICE, "ALICE-CONFIDENTIAL")
    bob = _record(client, BOB, "BOB-OWN-WORK")
    return {"alice": alice, "bob": bob}


def _listed(client, headers, **params):
    response = client.get("/trajectories", headers=headers, params=params)
    assert response.status_code == 200
    body = response.json()
    items = body if isinstance(body, list) else body.get("trajectories", [])
    return [item["trajectory_id"] for item in items]


# --- the listing filters ---------------------------------------------------

def test_each_caller_sees_only_their_own(client, both):
    _, alice_trajectory = both["alice"]
    _, bob_trajectory = both["bob"]

    assert _listed(client, ALICE) == [alice_trajectory]
    assert _listed(client, BOB) == [bob_trajectory]


def test_owning_a_session_does_not_unlock_everyone_elses(client, both):
    """The exact escalation: one session of your own was the whole exploit."""
    _, alice_trajectory = both["alice"]
    assert alice_trajectory not in _listed(client, BOB)


def test_a_caller_with_no_data_gets_an_empty_listing(client):
    """The case that used to pass for the wrong reason must still pass -- but on
    its own, with nobody else's data present to be leaked."""
    assert _listed(client, BOB) == []


def test_content_does_not_leak_through_the_listing(client, both):
    response = client.get("/trajectories", headers=BOB)
    assert "ALICE-CONFIDENTIAL" not in response.text


# --- the filtered form stays scoped ---------------------------------------

def test_filtering_by_another_callers_session_is_refused(client, both):
    alice_session, _ = both["alice"]
    response = client.get("/trajectories", headers=BOB,
                          params={"session_id": alice_session})
    assert response.status_code == 404


def test_filtering_by_your_own_session_works(client, both):
    bob_session, bob_trajectory = both["bob"]
    assert _listed(client, BOB, session_id=bob_session) == [bob_trajectory]


# --- one rule, not two -----------------------------------------------------

def test_the_listing_and_the_fetch_agree(client, both):
    """They had separate rules for the same question, which is how they came to
    disagree. Whatever the listing shows must be fetchable, and whatever it
    hides must not be."""
    for label, headers in (("alice", ALICE), ("bob", BOB)):
        visible = set(_listed(client, headers))
        for _, trajectory_id in both.values():
            fetched = client.get(f"/trajectories/{trajectory_id}", headers=headers)
            expected = 200 if trajectory_id in visible else 404
            assert fetched.status_code == expected, (
                f"{label}: listing and fetch disagree about {trajectory_id}"
            )


def test_export_agrees_too(client, both):
    _, alice_trajectory = both["alice"]
    assert alice_trajectory not in _listed(client, BOB)
    assert client.get(f"/trajectories/{alice_trajectory}/export",
                      headers=BOB).status_code == 404
