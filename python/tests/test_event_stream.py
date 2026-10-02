"""The console's lifeline: /events streams incrementally and never leaks.

The console is one long-lived `EventSource` on `GET /sessions/{id}/events` --
an *infinite* generator that yields each pushed event. Two properties keep it
working, and neither had a test:

1. **Incremental delivery.** The stream must flush each event as it happens.
   The existing SSE test uses `/messages/stream`, which *terminates*, so it
   passes even if a middleware buffered the whole body -- the fatal failure
   mode for an infinite stream is a hang, and a terminating stream cannot
   exhibit it. Round 106 added a second `BaseHTTPMiddleware` (security
   headers), and `BaseHTTPMiddleware` is exactly the layer that has broken
   streaming in past Starlette versions. This pins that it did not.

2. **Subscriber reclamation.** Every connection registers a queue in
   `session._subscribers` and every event is `put_nowait` onto it. A client
   that disconnects without the generator's `finally` running would leave a
   queue nobody drains, filling forever -- the round-94 resource-leak shape,
   one subsystem over.

Both need a *real* server: the sync TestClient cannot drive an infinite SSE
stream (it waits for the body to finish). So this runs uvicorn in a thread.
"""

import json
import socket
import threading
import time
import urllib.request
from http.client import HTTPConnection

import pytest

pytest.importorskip("uvicorn")

import pathlib

import uvicorn

from mini_loop import SessionManager, Settings
from mini_loop.auth import TokenAuth
from mini_loop.fake_llm import FakeAsyncAnthropic

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
TOKEN = "tok-alice-000000000000"
AUTH = {"Authorization": f"Bearer {TOKEN}", "Content-Type": "application/json"}


class _LiveServer:
    """A real uvicorn server in a background thread, on a free port."""

    def __init__(self, tmp_path, manager=None):
        from mini_loop.server import create_app

        self.manager = manager or SessionManager(
            Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                     skills_dir=SKILLS),
            FakeAsyncAnthropic(),
        )
        self.app = create_app(manager=self.manager)
        sock = socket.socket()
        sock.bind(("127.0.0.1", 0))
        self.port = sock.getsockname()[1]
        sock.close()
        self._server = uvicorn.Server(
            uvicorn.Config(self.app, host="127.0.0.1", port=self.port,
                           log_level="error")
        )
        self._thread = threading.Thread(target=self._server.run, daemon=True)

    def __enter__(self):
        self._thread.start()
        for _ in range(200):
            try:
                urllib.request.urlopen(f"{self.base}/healthz", timeout=1)
                break
            except Exception:
                time.sleep(0.05)
        self.app.state.auth = TokenAuth({TOKEN: "alice"})
        return self

    def __exit__(self, *exc):
        self._server.should_exit = True
        self._thread.join(timeout=5)

    @property
    def base(self):
        return f"http://127.0.0.1:{self.port}"

    def post(self, path, body):
        req = urllib.request.Request(f"{self.base}{path}",
                                     data=json.dumps(body).encode(),
                                     method="POST", headers=AUTH)
        return json.load(urllib.request.urlopen(req, timeout=5))


def _open_event_stream(server, sid):
    conn = HTTPConnection("127.0.0.1", server.port, timeout=8)
    conn.request("GET", f"/sessions/{sid}/events?envelope=true&access_token={TOKEN}")
    return conn, conn.getresponse()


def test_the_observe_stream_delivers_incrementally(tmp_path):
    with _LiveServer(tmp_path) as server:
        sid = server.post("/sessions", {})["id"]
        conn, resp = _open_event_stream(server, sid)
        try:
            assert resp.status == 200
            # Headers already arrived: a fully-buffering middleware could never
            # return them on an infinite body. This is the first half of proof.
            header_names = {k.lower() for k, _ in resp.getheaders()}
            assert "content-security-policy" in header_names

            def fire():
                time.sleep(0.4)
                server.post(f"/sessions/{sid}/messages", {"message": "hello"})

            threading.Thread(target=fire, daemon=True).start()

            conn.sock.settimeout(6)
            deadline = time.time() + 6
            chunks = []
            while time.time() < deadline:
                block = resp.read(256)
                if block:
                    chunks.append(block)
                    if b"agent_event" in b"".join(chunks):
                        break
            body = b"".join(chunks).decode("utf-8", "replace")
            assert "agent_event" in body, (
                "no event arrived while the stream was still open -- the "
                "observe stream is buffered, not incremental"
            )
        finally:
            conn.close()


def test_a_disconnect_reclaims_the_subscriber(tmp_path):
    with _LiveServer(tmp_path) as server:
        sid = server.post("/sessions", {})["id"]
        session = server.manager._sessions[sid]
        assert len(session._subscribers) == 0

        conn, resp = _open_event_stream(server, sid)
        for _ in range(40):
            if len(session._subscribers) == 1:
                break
            time.sleep(0.05)
        assert len(session._subscribers) == 1, "the stream never registered"

        conn.close()  # abrupt disconnect, no graceful close of the generator
        for _ in range(60):
            if len(session._subscribers) == 0:
                break
            time.sleep(0.1)
        assert len(session._subscribers) == 0, (
            "a disconnected client left its queue in _subscribers; every event "
            "now fills a queue nobody drains"
        )


def test_a_resume_catches_up_from_the_store_beyond_the_backlog(tmp_path):
    """The in-memory backlog holds only the last 200 events, so a client that
    missed more than that gapped -- yet the events are durably stored and the
    resume never read them. A Last-Event-ID far behind the backlog now receives
    everything after it, caught up from the store, with no gap and no duplicate."""
    import asyncio

    from mini_loop.session import BACKLOG
    from mini_loop.storage import SQLiteStateStore

    store = SQLiteStateStore(tmp_path / "state.db")
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(), state_store=store,
    )
    session = manager.create()
    session.owner = "alice"  # the token the live server authenticates as
    total = BACKLOG + 50  # more events than the backlog can hold

    async def _emit():
        for i in range(total):
            await session.emit({"type": "status", "n": i})

    asyncio.run(_emit())

    with _LiveServer(tmp_path, manager=manager) as server:
        conn = HTTPConnection("127.0.0.1", server.port, timeout=8)
        conn.request(
            "GET", f"/sessions/{session.id}/events",
            headers={"Authorization": f"Bearer {TOKEN}", "Last-Event-ID": "5"},
        )
        resp = conn.getresponse()
        assert resp.status == 200
        seen = set()
        try:
            conn.sock.settimeout(6)
            while True:
                line = resp.readline()  # one \n-terminated SSE field at a time
                if not line:
                    break
                if line.startswith(b"id:"):
                    value = line[3:].strip()
                    if value.isdigit():
                        seen.add(int(value))
                        if int(value) >= total:
                            break  # got the last emitted; the rest would block
        except (TimeoutError, OSError):
            pass
        finally:
            conn.close()

    seqs = sorted(seen)
    # Everything after last-event-id 5, including the 6..50 the backlog dropped.
    assert seqs == list(range(6, total + 1)), (
        f"gap or duplicate on resume: {seqs[:3]}..{seqs[-3:]}"
    )
