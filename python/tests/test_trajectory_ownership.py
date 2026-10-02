"""Any authenticated caller could read any other caller's recorded conversation.

Round 24 built ownership scoping and stated the rule: someone else's session is
**404, not 403**, because 403 confirms the id exists. Fifty rounds later the
trajectory routes had been added and did not inherit it. Against a running
server with two tokens:

    alice  GET /sessions/{id}              -> 200
    bob    GET /sessions/{id}              -> 404   correct
    bob    GET /sessions/{id}/trajectories -> 200   alice's trajectory ids
    bob    GET /trajectories/{tid}         -> 200   alice's message content
    bob    GET /trajectories/{tid}/export  -> 200   alice's message content

`GET /trajectories` -- the *listing* -- was filtered by caller. The direct fetch
was not: a filtered index over unprotected direct object references, which is
the classic shape of this bug.

The sharpest part is that the check already existed. `_require_owned_trajectory`
was written, documented "a trajectory is readable only by the owner of its
session", and **called from nowhere**. Round 26's construction-drift pattern in
its worst form: not a site that passes less than its siblings, but a protection
wired to nothing at all.

A trajectory holds the full recorded conversation including tool inputs and
outputs, so this was the most exposed thing in the package.
"""

import os
import pathlib
import tempfile

import pytest

CONFIDENTIAL = "ALICE-CONFIDENTIAL-PLAN"
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

    import importlib

    import mini_loop.server as server

    importlib.reload(server)
    from fastapi.testclient import TestClient

    with TestClient(server.app) as test_client:
        yield test_client


@pytest.fixture
def alices_trajectory(client):
    session_id = client.post("/sessions", json={}, headers=ALICE).json()["id"]
    client.post(f"/sessions/{session_id}/messages",
                json={"message": CONFIDENTIAL}, headers=ALICE)
    listing = client.get(f"/sessions/{session_id}/trajectories", headers=ALICE).json()
    items = listing if isinstance(listing, list) else listing.get("trajectories", [])
    assert items, "the fixture recorded no trajectory"
    return session_id, items[0]["trajectory_id"]


# --- the owner keeps working ---------------------------------------------

def test_the_owner_can_read_their_own(client, alices_trajectory):
    """Scoping that locks out the owner is not scoping."""
    session_id, trajectory_id = alices_trajectory
    assert client.get(f"/sessions/{session_id}/trajectories",
                      headers=ALICE).status_code == 200
    body = client.get(f"/trajectories/{trajectory_id}", headers=ALICE)
    assert body.status_code == 200
    assert CONFIDENTIAL in body.text


def test_the_owner_can_export(client, alices_trajectory):
    _, trajectory_id = alices_trajectory
    exported = client.get(f"/trajectories/{trajectory_id}/export", headers=ALICE)
    assert exported.status_code == 200
    assert CONFIDENTIAL in exported.text


# --- a stranger gets nothing ---------------------------------------------

@pytest.mark.parametrize("route", [
    "/sessions/{session_id}/trajectories",
    "/trajectories/{trajectory_id}",
    "/trajectories/{trajectory_id}/export",
])
def test_another_caller_is_refused(client, alices_trajectory, route):
    session_id, trajectory_id = alices_trajectory
    path = route.format(session_id=session_id, trajectory_id=trajectory_id)
    response = client.get(path, headers=BOB)

    assert response.status_code == 404, (
        f"{path} answered {response.status_code} to a caller who does not own it"
    )
    assert CONFIDENTIAL not in response.text


@pytest.mark.parametrize("fmt", ["json", "jsonl"])
def test_no_export_format_is_a_way_round(client, alices_trajectory, fmt):
    """Two code paths inside one handler; both need the check."""
    _, trajectory_id = alices_trajectory
    response = client.get(f"/trajectories/{trajectory_id}/export?format={fmt}",
                          headers=BOB)
    assert response.status_code == 404
    assert CONFIDENTIAL not in response.text


def test_refusal_is_404_not_403(client, alices_trajectory):
    """Round 24's rule: 403 confirms the id exists, which is the disclosure."""
    _, trajectory_id = alices_trajectory
    assert client.get(f"/trajectories/{trajectory_id}",
                      headers=BOB).status_code == 404


def test_a_stranger_cannot_tell_it_from_a_missing_one(client, alices_trajectory):
    _, trajectory_id = alices_trajectory
    real = client.get(f"/trajectories/{trajectory_id}", headers=BOB)
    absent = client.get("/trajectories/traj_does_not_exist_at_all", headers=BOB)
    assert real.status_code == absent.status_code == 404


def test_the_listing_stays_filtered(client, alices_trajectory):
    """It always was; the regression risk is fixing the fetch and breaking this."""
    _, trajectory_id = alices_trajectory
    assert trajectory_id not in client.get("/trajectories", headers=BOB).text
    assert trajectory_id in client.get("/trajectories", headers=ALICE).text


# --- the helper is wired, not merely present -----------------------------

def test_every_trajectory_route_checks_ownership():
    """It existed, was documented, and was called from nowhere.

    An AST check rather than a behavioural one, because the behavioural tests
    above can only cover the routes that exist today.
    """
    import ast

    source = (pathlib.Path(__file__).resolve().parent.parent
              / "mini_loop" / "server.py").read_text()
    tree = ast.parse(source)

    unchecked = []
    for node in ast.walk(tree):
        if not isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            continue
        paths = [
            decorator.args[0].value
            for decorator in node.decorator_list
            if isinstance(decorator, ast.Call) and decorator.args
            and isinstance(decorator.args[0], ast.Constant)
            and isinstance(decorator.args[0].value, str)
        ]
        if not any("{trajectory_id}" in p or "{session_id}" in p for p in paths):
            continue
        called = {
            ast.unparse(call.func) for call in ast.walk(node)
            if isinstance(call, ast.Call)
        }
        # `_owned_trajectory_summary` enforces ownership before any bulk read
        # (it calls `_require_owned_trajectory` on the cheap header summary), so
        # a route that resolves the trajectory through it is checked -- and
        # checked *before* loading the file, which the direct `get()`+check was
        # not (round 151).
        ownership_checks = {
            "_require", "_require_owned_trajectory", "_owned_trajectory_summary"
        }
        if not (ownership_checks & called):
            unchecked.append(f"{node.name} {paths}")

    assert not unchecked, (
        "these routes take an id from the caller and check no ownership:\n  "
        + "\n  ".join(unchecked)
    )


def test_only_the_owner_may_delete_a_session(client, alices_trajectory):
    """Found by the AST check, not by the probe.

    A live probe deleted as the owner first, so the stranger's 404 meant
    "already gone" rather than "not yours" and the missing check was invisible.
    Order-dependence is exactly what a behavioural probe hides and a scan does
    not.
    """
    session_id, _ = alices_trajectory
    assert client.delete(f"/sessions/{session_id}", headers=BOB).status_code == 404
    assert client.get(f"/sessions/{session_id}", headers=ALICE).status_code == 200
    assert client.delete(f"/sessions/{session_id}", headers=ALICE).status_code == 200


def test_a_trajectory_outlives_its_session_for_its_owner(client, alices_trajectory):
    """Trajectories are meant to survive deletion, and the first version of the
    ownership check resolved owners from *live* sessions -- so deleting a
    session made its recordings unreadable by the person who made them. An
    existing test caught that regression."""
    session_id, trajectory_id = alices_trajectory
    assert client.delete(f"/sessions/{session_id}", headers=ALICE).status_code == 200

    still_readable = client.get(f"/trajectories/{trajectory_id}", headers=ALICE)
    assert still_readable.status_code == 200
    assert CONFIDENTIAL in still_readable.text
    assert client.get(f"/trajectories/{trajectory_id}",
                      headers=BOB).status_code == 404


# --- round 151: the export is bounded, and ownership is checked first --------

def _inflate(client, trajectory_id, target_bytes):
    """Grow a trajectory on disk past `target_bytes` (a long run records the
    full model input at every model call, so real ones reach tens of MB)."""
    store = client.app.state.manager.trajectories
    body = "X" * 40_000
    while store.byte_size(trajectory_id) < target_bytes:
        store.append(trajectory_id, {
            "type": "model_start",
            "model_input": {"messages": [{"role": "user", "content": body}]},
        })
    return store.byte_size(trajectory_id)


def test_a_large_trajectory_streams_as_jsonl(client, alices_trajectory):
    """The export must work for a trajectory far larger than memory: JSONL is
    streamed off disk, not read whole."""
    from mini_loop.server import MAX_TRAJECTORY_JSON_BYTES

    _session, trajectory_id = alices_trajectory
    size = _inflate(client, trajectory_id, MAX_TRAJECTORY_JSON_BYTES + 1_000_000)

    response = client.get(
        f"/trajectories/{trajectory_id}/export?format=jsonl", headers=ALICE
    )
    assert response.status_code == 200
    assert response.headers["content-type"].startswith("application/x-ndjson")
    assert len(response.content) == size  # the whole file was delivered


def test_a_trajectory_too_large_for_one_json_document_is_refused(client, alices_trajectory):
    """The JSON views build the whole thing in memory, so an oversized one is
    refused with a pointer to the streaming export -- edit_file's rule (round
    141) for a read that cannot be truncated."""
    from mini_loop.server import MAX_TRAJECTORY_JSON_BYTES

    _session, trajectory_id = alices_trajectory
    _inflate(client, trajectory_id, MAX_TRAJECTORY_JSON_BYTES + 1_000_000)

    export = client.get(
        f"/trajectories/{trajectory_id}/export?format=json", headers=ALICE
    )
    assert export.status_code == 413 and "jsonl" in export.json()["detail"]

    inspect = client.get(f"/trajectories/{trajectory_id}", headers=ALICE)
    assert inspect.status_code == 413 and "jsonl" in inspect.json()["detail"]


def test_a_stranger_is_refused_a_large_trajectory_without_loading_it(client, alices_trajectory):
    """The ownership check reads only the header, so a stranger's request for
    another tenant's id is 404 without the file being pulled into memory -- both
    the disclosure and the cross-tenant OOM lever are closed."""
    from mini_loop.server import MAX_TRAJECTORY_JSON_BYTES

    _session, trajectory_id = alices_trajectory
    _inflate(client, trajectory_id, MAX_TRAJECTORY_JSON_BYTES + 1_000_000)

    for url in (
        f"/trajectories/{trajectory_id}",
        f"/trajectories/{trajectory_id}/export?format=jsonl",
        f"/trajectories/{trajectory_id}/export?format=json",
    ):
        assert client.get(url, headers=BOB).status_code == 404, url


def test_a_normal_trajectory_still_exports_both_ways(client, alices_trajectory):
    """The common case is untouched: JSON and JSONL both work for a small one."""
    _session, trajectory_id = alices_trajectory
    assert client.get(f"/trajectories/{trajectory_id}", headers=ALICE).status_code == 200
    for fmt in ("json", "jsonl"):
        response = client.get(
            f"/trajectories/{trajectory_id}/export?format={fmt}", headers=ALICE
        )
        assert response.status_code == 200
        assert CONFIDENTIAL in response.text
