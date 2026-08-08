"""A request body the server reads must be bounded, or the ingress is a memory bomb.

`message` and `system` are bounded downstream -- `steer` truncates, the model
rejects an over-long prompt -- but nothing bounded the *body* on the way in.
Starlette reads the whole body to parse it, so an authenticated caller POSTing a
multi-gigabyte body OOMs the shared process before any handler runs, taking every
tenant on it down. `RequestSizeLimit` caps the body at `MAX_REQUEST_BYTES`: a
declared `Content-Length` over the cap is refused before the body is read at all,
and the streamed body is counted too so a chunked request cannot slip past.
"""

import asyncio
import tempfile
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from mini_loop.auth import NullAuth
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.manager import SessionManager
from mini_loop.server import MAX_REQUEST_BYTES, RequestSizeLimit, create_app

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


# --- the middleware directly: a declared oversize is refused unread -----------

def test_content_length_over_cap_is_refused_without_reading_the_body():
    """The header alone is enough to refuse: the app is never invoked and the
    body is never read, so a caller cannot make the server buffer gigabytes just
    to reject them. Reading the body first would be the memory bomb this exists
    to stop."""

    async def app(scope, receive, send):
        raise AssertionError("the app must not run for an oversized request")

    async def receive():
        raise AssertionError("the body must not be read for an oversized request")

    sent = []

    async def send(message):
        sent.append(message)

    scope = {
        "type": "http",
        "headers": [(b"content-length", str(MAX_REQUEST_BYTES + 1).encode())],
    }
    middleware = RequestSizeLimit(app, max_bytes=MAX_REQUEST_BYTES)
    asyncio.run(middleware(scope, receive, send))

    assert sent and sent[0]["status"] == 413


def test_a_body_within_the_cap_reaches_the_app():
    """The common case is untouched: a small body is passed through, receive and
    all."""

    seen = {}

    async def app(scope, receive, send):
        message = await receive()
        seen["body"] = message.get("body", b"")

    async def receive():
        return {"type": "http.request", "body": b'{"message":"hi"}', "more_body": False}

    async def send(message):
        pass

    scope = {"type": "http", "headers": [(b"content-length", b"16")]}
    asyncio.run(RequestSizeLimit(app, max_bytes=MAX_REQUEST_BYTES)(scope, receive, send))
    assert seen["body"] == b'{"message":"hi"}'


# --- over HTTP: chunked bypass, permitted large body, every route -------------

@pytest.fixture
def client(tmp_path):
    settings = Settings(
        fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR
    )
    manager = SessionManager(settings, FakeAsyncAnthropic())
    app = create_app(manager=manager)
    with TestClient(app) as http:
        app.state.auth = NullAuth()
        yield http


def _session(client) -> str:
    return client.post("/sessions", json={}).json()["id"]


def test_a_normal_request_is_unaffected(client):
    session_id = _session(client)
    response = client.post(f"/sessions/{session_id}/steer", json={"message": "hi"})
    assert response.status_code == 200


def test_a_chunked_body_over_the_cap_is_still_refused(client):
    """A request without Content-Length (chunked, or a lying header) cannot slip
    a large body past the header check: the streamed bytes are counted too, so
    the handler never parses a body larger than the cap."""
    session_id = _session(client)

    def stream():
        yield b'{"message":"'
        chunk = b"A" * (1024 * 1024)
        for _ in range(MAX_REQUEST_BYTES // len(chunk) + 2):
            yield chunk
        yield b'"}'

    response = client.post(
        f"/sessions/{session_id}/steer",
        content=stream(),
        headers={"content-type": "application/json"},
    )
    assert response.status_code == 413


def test_a_large_but_permitted_body_still_works(client):
    """A full-context prompt is a few MB; the cap is generous enough for one."""
    session_id = _session(client)
    body = "x" * (MAX_REQUEST_BYTES // 2)
    response = client.post(f"/sessions/{session_id}/steer", json={"message": body})
    assert response.status_code == 200


def test_the_limit_guards_every_route(client):
    """The cap is middleware, not a per-handler check, so a route added later
    inherits it -- the property that made session creation itself protected."""
    session_id = _session(client)
    oversized = b'{"message":"' + b"A" * (MAX_REQUEST_BYTES + 4096) + b'"}'
    headers = {"content-type": "application/json"}
    for method, url in (
        ("POST", "/sessions"),
        ("POST", f"/sessions/{session_id}/messages"),
        ("POST", f"/sessions/{session_id}/steer"),
    ):
        response = client.request(method, url, content=oversized, headers=headers)
        assert response.status_code == 413, f"{method} {url} -> {response.status_code}"
