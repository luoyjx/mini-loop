"""Authentication and per-caller scope on the HTTP surface.

Before this, an anonymous caller could create a session, run shell commands,
enumerate *other* callers' sessions and read every recorded trajectory --
demonstrated against a running server. These tests pin the boundary in both
directions: a valid token gets its own sessions, and never anyone else's.
"""

from contextlib import contextmanager
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from mini_loop.auth import (
    ANONYMOUS,
    NullAuth,
    Principal,
    TokenAuth,
    load_auth,
    refuse_open_bind,
)
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.manager import SessionManager
from mini_loop.server import create_app

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"

ALICE = "tok-alice-000000000000"
BOB = "tok-bob-1111111111111"


@contextmanager
def _client(tmp_path, auth=None):
    """A live app (lifespan run) whose authenticator we control."""
    settings = Settings(
        fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR
    )
    manager = SessionManager(settings, FakeAsyncAnthropic())
    app = create_app(manager=manager)
    with TestClient(app) as client:
        # After the lifespan, so it is not overwritten by the env-built default.
        app.state.auth = auth or TokenAuth({ALICE: "alice", BOB: "bob"})
        yield client


def _h(token):
    return {"Authorization": f"Bearer {token}"}


# --- the authenticator -----------------------------------------------------

def test_a_valid_bearer_token_identifies_its_principal():
    auth = TokenAuth({ALICE: "alice", BOB: "bob"})
    assert auth.authenticate(f"Bearer {ALICE}") == Principal(id="alice")
    assert auth.authenticate(f"Bearer {BOB}") == Principal(id="bob")


@pytest.mark.parametrize(
    "header",
    [None, "", "Bearer", "Bearer ", ALICE, f"Basic {ALICE}", f"Bearer {ALICE}x", "Bearer wrong"],
)
def test_anything_but_a_valid_bearer_token_is_rejected(header):
    assert TokenAuth({ALICE: "alice"}).authenticate(header) is None


def test_the_scheme_is_matched_case_insensitively():
    assert TokenAuth({ALICE: "alice"}).authenticate(f"bearer {ALICE}") is not None


def test_an_empty_token_map_is_rejected():
    with pytest.raises(ValueError):
        TokenAuth({})


def test_null_auth_makes_everyone_the_same_anonymous_caller():
    assert NullAuth().authenticate(None) == ANONYMOUS
    assert NullAuth().configured is False


def test_tokens_are_loaded_from_the_environment():
    single = load_auth({"MINILOOP_API_TOKEN": ALICE})
    assert single.authenticate(f"Bearer {ALICE}") == Principal(id="default")

    multi = load_auth({"MINILOOP_API_TOKENS": f"alice:{ALICE},bob:{BOB}"})
    assert multi.principals() == ("alice", "bob")

    assert isinstance(load_auth({}), NullAuth)


def test_a_malformed_token_list_is_an_error_not_a_silent_skip():
    with pytest.raises(ValueError):
        load_auth({"MINILOOP_API_TOKENS": "no-colon-here"})


def test_a_token_shared_by_two_principals_is_refused():
    """A token assigned to two principals is a shared credential: one caller
    authenticates as the other, and last-wins would drop the first without a
    word. Refused, like a malformed entry -- fail loud on insecure config."""
    with pytest.raises(ValueError):
        load_auth({"MINILOOP_API_TOKENS": f"alice:{ALICE},bob:{ALICE}"})

    # Legitimate shapes still load: distinct tokens, one principal with several
    # tokens, and a redundant identical entry (same principal *and* token).
    distinct = load_auth({"MINILOOP_API_TOKENS": f"alice:{ALICE},bob:{BOB}"})
    assert distinct.authenticate(f"Bearer {ALICE}") == Principal(id="alice")
    assert distinct.authenticate(f"Bearer {BOB}") == Principal(id="bob")

    two_tokens = load_auth({"MINILOOP_API_TOKENS": f"alice:{ALICE},alice:{BOB}"})
    assert two_tokens.authenticate(f"Bearer {ALICE}") == Principal(id="alice")
    assert two_tokens.authenticate(f"Bearer {BOB}") == Principal(id="alice")

    redundant = load_auth({"MINILOOP_API_TOKENS": f"alice:{ALICE},alice:{ALICE}"})
    assert redundant.authenticate(f"Bearer {ALICE}") == Principal(id="alice")


# --- refusing to serve an open bind ---------------------------------------

def test_an_open_bind_without_auth_is_refused():
    reason = refuse_open_bind("0.0.0.0", NullAuth())
    assert reason and "refusing to bind" in reason


def test_an_open_bind_with_auth_is_allowed():
    assert refuse_open_bind("0.0.0.0", TokenAuth({ALICE: "alice"})) is None


def test_loopback_without_auth_is_allowed():
    for host in ("127.0.0.1", "localhost", "::1"):
        assert refuse_open_bind(host, NullAuth()) is None


# --- the HTTP surface ---

def test_an_unauthenticated_request_is_401(tmp_path):
    with _client(tmp_path) as client:
        for response in (
            client.post("/sessions", json={}),
            client.get("/sessions"),
            client.get("/sessions/whatever"),
        ):
            path = response.request.url.path
            assert response.status_code == 401, path
            assert "bearer" in response.headers.get("www-authenticate", "").lower()


def test_a_caller_only_sees_its_own_sessions(tmp_path):
    with _client(tmp_path) as client:
        alice = client.post("/sessions", json={}, headers=_h(ALICE)).json()["id"]
        bob = client.post("/sessions", json={}, headers=_h(BOB)).json()["id"]

        assert [s["id"] for s in client.get("/sessions", headers=_h(ALICE)).json()] == [alice]
        assert [s["id"] for s in client.get("/sessions", headers=_h(BOB)).json()] == [bob]


def test_another_callers_session_is_404_not_403(tmp_path):
    """403 would confirm the id exists, which is itself a disclosure."""
    with _client(tmp_path) as client:
        alice = client.post("/sessions", json={}, headers=_h(ALICE)).json()["id"]

        mine = client.get(f"/sessions/{alice}", headers=_h(BOB))
        missing = client.get("/sessions/nope", headers=_h(BOB))
        assert mine.status_code == 404
        assert mine.status_code == missing.status_code


def test_another_callers_session_cannot_be_driven(tmp_path):
    with _client(tmp_path) as client:
        alice = client.post("/sessions", json={}, headers=_h(ALICE)).json()["id"]
        response = client.post(
            f"/sessions/{alice}/messages",
            json={"message": "run something"},
            headers=_h(BOB),
        )
        assert response.status_code == 404


def test_trajectories_do_not_leak_across_callers(tmp_path):
    with _client(tmp_path) as client:
        alice = client.post("/sessions", json={}, headers=_h(ALICE)).json()["id"]
        client.post(f"/sessions/{alice}/messages", json={"message": "go"}, headers=_h(ALICE))

        assert client.get("/trajectories", headers=_h(ALICE)).status_code == 200
        assert client.get("/trajectories", headers=_h(BOB)).json() == []
        assert client.get(f"/trajectories?session_id={alice}", headers=_h(BOB)).status_code == 404


def test_healthz_reports_whether_auth_is_configured(tmp_path):
    with _client(tmp_path) as client:
        assert client.get("/healthz", headers=_h(ALICE)).json()["authenticated"] is True
    with _client(tmp_path, auth=NullAuth()) as client:
        assert client.get("/healthz").json()["authenticated"] is False


def test_without_auth_the_surface_behaves_as_before(tmp_path):
    """Existing local deployments must not break."""
    with _client(tmp_path, auth=NullAuth()) as client:
        created = client.post("/sessions", json={})
        assert created.status_code == 200
        session_id = created.json()["id"]
        assert client.get(f"/sessions/{session_id}").status_code == 200
        assert len(client.get("/sessions").json()) == 1


# --- route coverage: the guard that replaces "remember to add the check" ---

def _routes(app):
    from fastapi.routing import APIRoute

    return [
        (sorted(r.methods - {"HEAD", "OPTIONS"})[0], r.path)
        for r in app.routes
        if isinstance(r, APIRoute) and r.methods - {"HEAD", "OPTIONS"}
    ]


def test_the_routes_that_were_open_are_closed(tmp_path):
    """Three endpoints answered without a credential.

    Two of them return a whole recorded conversation. They were open because
    authentication was a call each handler had to remember, and these did not.
    Authentication is now middleware, so the property holds by construction --
    this pins the specific regression that motivated the change.
    """
    with _client(tmp_path) as client:
        session_id = client.post("/sessions", json={}, headers=_h(ALICE)).json()["id"]
        for method, url in (
            ("GET", f"/sessions/{session_id}/trajectories"),
            ("GET", "/trajectories/tid"),
            ("GET", "/trajectories/tid/export"),
            ("GET", "/trajectories"),
            ("DELETE", f"/sessions/{session_id}"),
        ):
            response = client.request(method, url)
            assert response.status_code == 401, f"{method} {url} -> {response.status_code}"


def test_authentication_is_middleware_not_a_per_handler_call(tmp_path):
    """The structural property: a route added later inherits the check."""
    with _client(tmp_path) as client:
        from mini_loop.server import PUBLIC_PATHS

        # An endpoint that exists on the app but was never given a check.
        assert client.get("/does-not-exist").status_code == 401
        assert "/healthz" in PUBLIC_PATHS


def test_public_paths_stay_reachable(tmp_path):
    with _client(tmp_path) as client:
        assert client.get("/healthz").status_code == 200
        assert client.get("/").status_code == 200


def test_the_sse_query_credential_is_scoped_to_streaming_routes():
    """`EventSource` cannot set headers; nothing else gets that exemption.

    Tested at the resolver rather than over HTTP: an SSE response never
    completes, so a live request would hang the suite rather than assert
    anything.
    """
    from types import SimpleNamespace

    from mini_loop.server import _credential

    def request(path, *, header=None, query=None):
        return SimpleNamespace(
            headers={"authorization": header} if header else {},
            url=SimpleNamespace(path=path),
            query_params=query or {},
        )

    # The streaming route accepts it.
    assert _credential(
        request("/sessions/s1/events", query={"access_token": ALICE})
    ) == f"Bearer {ALICE}"

    # Nothing else does.
    for path in ("/sessions", "/trajectories", "/sessions/s1/trajectories"):
        assert _credential(request(path, query={"access_token": ALICE})) is None

    # A header always wins and works everywhere.
    assert _credential(
        request("/sessions", header=f"Bearer {ALICE}")
    ) == f"Bearer {ALICE}"
