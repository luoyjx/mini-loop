"""Every collection endpoint, checked with both tenants holding data.

Rounds 74 to 76 found three authorization holes on one surface, and **two were
hidden by the fixture rather than the code**: the delete bug because the probe
deleted as the owner first, the listing bug because the stranger owned nothing.
A caller with no data of their own cannot tell a *filtered* result from an
*empty* one, and `GET /trajectories` returned every trajectory on the box the
moment the caller owned a single session.

Those were found one endpoint at a time by hand. This is the shape as a test:
routes are discovered from the app, both tenants create real data, and each is
asserted never to see the other's identifiers. A collection route added later is
covered on arrival, which is the part hand-probing cannot promise.

`GET /sessions` was checked the same way and is correctly filtered -- recorded
as a negative, because the interesting claim is that the *rule* holds, not that
one more bug was found.
"""

import importlib
import pathlib

import pytest

ALICE = {"Authorization": "Bearer tok-alice"}
BOB = {"Authorization": "Bearer tok-bob"}

#: Collections that are deliberately not per-caller.
SHARED_BY_DESIGN: set[str] = set()


@pytest.fixture
def server_module(monkeypatch, tmp_path):
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
    return server


@pytest.fixture
def client(server_module):
    from fastapi.testclient import TestClient

    with TestClient(server_module.app) as test_client:
        yield test_client


def _collection_routes(server_module):
    """GET routes that take no path parameter -- the listings."""
    routes = []
    for route in server_module.app.routes:
        methods = getattr(route, "methods", None) or set()
        if "GET" not in methods or "{" in route.path:
            continue
        if route.path in server_module.PUBLIC_PATHS or route.path.startswith("/docs"):
            continue
        if route.path in ("/openapi.json", "/redoc"):
            continue
        routes.append(route.path)
    return sorted(set(routes) - SHARED_BY_DESIGN)


@pytest.fixture
def tenants(client):
    """Both callers own real, distinguishable data.

    This is the whole point. With the stranger holding nothing, `[]` and "every
    record on the box, filtered" are the same observation.
    """
    identifiers = {}
    for name, headers, marker in (("alice", ALICE, "ALICE-PRIVATE"),
                                  ("bob", BOB, "BOB-PRIVATE")):
        session_id = client.post("/sessions", json={}, headers=headers).json()["id"]
        client.post(f"/sessions/{session_id}/messages",
                    json={"message": marker}, headers=headers)
        listing = client.get(f"/sessions/{session_id}/trajectories",
                             headers=headers).json()
        items = listing if isinstance(listing, list) else listing.get("trajectories", [])
        identifiers[name] = {
            "headers": headers,
            "marker": marker,
            "session": session_id,
            "trajectory": items[0]["trajectory_id"] if items else None,
        }
    return identifiers


def test_the_fixture_gives_both_tenants_data(tenants):
    """Without this the whole file could pass by testing nothing."""
    for name, data in tenants.items():
        assert data["session"], f"{name} has no session"
        assert data["trajectory"], f"{name} has no trajectory"
    assert tenants["alice"]["session"] != tenants["bob"]["session"]


def test_collection_routes_were_found(server_module):
    """A discovery that matches nothing would pass every case below."""
    assert _collection_routes(server_module)


def test_every_collection_is_scoped(client, server_module, tenants):
    """One assertion over every listing, present and future."""
    problems = []
    for path in _collection_routes(server_module):
        for viewer, other in (("alice", "bob"), ("bob", "alice")):
            response = client.get(path, headers=tenants[viewer]["headers"])
            if response.status_code != 200:
                continue
            body = response.text
            for field in ("session", "trajectory", "marker"):
                value = tenants[other][field]
                if value and value in body:
                    problems.append(
                        f"{path}: {viewer} can see {other}'s {field} ({value})"
                    )
    assert not problems, "cross-tenant disclosure:\n  " + "\n  ".join(problems)


def test_each_collection_still_shows_the_caller_their_own(client, server_module,
                                                          tenants):
    """Scoping that hides everything from everyone would pass the test above."""
    seen_any = False
    for path in _collection_routes(server_module):
        response = client.get(path, headers=ALICE)
        if response.status_code != 200:
            continue
        if tenants["alice"]["session"] in response.text or (
                tenants["alice"]["trajectory"] or "") in response.text:
            seen_any = True
    assert seen_any, "no collection showed a caller their own data"


@pytest.mark.parametrize("field", ["session", "trajectory"])
def test_a_stranger_cannot_reach_an_identifier_directly(client, tenants, field):
    """The other half: listings hide it, and the direct route refuses it."""
    value = tenants["alice"][field]
    route = f"/sessions/{value}" if field == "session" else f"/trajectories/{value}"
    assert client.get(route, headers=BOB).status_code == 404
