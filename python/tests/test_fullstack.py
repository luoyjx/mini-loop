"""One session, every protection on, through the HTTP surface.

Eight of the defects found while building this harness lived at the boundary
between two modules, and every one was found by hand, one pair at a time --
because no test ran the whole stack together. This one does: authentication,
per-caller scope, durable state, the durable action journal, secret masking,
shell confinement and prompt caching, all enabled on a single session.

It is deliberately assertion-dense rather than split into a dozen cases: the
point is that these invariants hold *simultaneously*, which is exactly what
per-module tests cannot show.
"""

import asyncio
import os
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from mini_loop import DefaultCachePolicy, DefaultStuckDetector, SecretRegistry
from mini_loop.auth import TokenAuth
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
from mini_loop.identity import build_id
from mini_loop.manager import SessionManager
from mini_loop.sandbox import SeatbeltSandbox
from mini_loop.server import create_app
from mini_loop.storage import SQLiteStateStore

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"

TOKEN = "tok-fullstack-00000000"
OTHER = "tok-intruder-00000000"
CANARY = "sk-fullstack-0123456789abcdef"
ENV_NAME = "MINILOOP_FULLSTACK_CANARY"


def _responder(kwargs: dict):
    if not kwargs.get("tools"):
        return [text("[summary]")], "end_turn"
    last = kwargs["messages"][-1]
    if isinstance(last.get("content"), str):
        # The model writes the credential into an *argument*, as it would when
        # constructing an authenticated request.
        return (
            [tool("bash", _id="t1", command=f'echo "auth {CANARY}" > hello.txt')],
            "tool_use",
        )
    return [text("done")], "end_turn"


@pytest.fixture
def stack(tmp_path):
    workspaces = tmp_path / "ws"
    workspaces.mkdir()
    os.environ[ENV_NAME] = CANARY
    store = SQLiteStateStore(tmp_path / "state.db")
    sandbox = (
        SeatbeltSandbox(writable_roots=[workspaces])
        if SeatbeltSandbox.available()
        else None
    )
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=workspaces, skills_dir=SKILLS_DIR),
        FakeAsyncAnthropic(responder=_responder),
        state_store=store,
        secrets=SecretRegistry.from_environ(extra_names=[ENV_NAME]),
        sandbox=sandbox,
        cache_policy=DefaultCachePolicy(),
        stuck_detector=DefaultStuckDetector(),
    )
    app = create_app(manager=manager)
    try:
        with TestClient(app) as client:
            app.state.auth = TokenAuth({TOKEN: "owner", OTHER: "intruder"})
            yield client, manager, store
    finally:
        os.environ.pop(ENV_NAME, None)
        store.close()


def _h(token=TOKEN):
    return {"Authorization": f"Bearer {token}"}


def test_the_whole_stack_holds_together(stack):
    client, manager, store = stack

    # --- identity and posture, before anything else ------------------------
    health = client.get("/healthz").json()
    assert health["build"] == build_id(), "not the build under test"
    posture = health["posture"]
    assert posture["state_store"] == "SQLiteStateStore"
    assert posture["action_journal"] == "DurableActionJournal"
    assert posture["secrets"] == "SecretRegistry"
    assert health["authenticated"] is True
    if SeatbeltSandbox.available():
        assert posture["sandbox"] == "SeatbeltSandbox"

    # --- authentication -----------------------------------------------------
    assert client.post("/sessions", json={}).status_code == 401

    session_id = client.post("/sessions", json={}, headers=_h()).json()["id"]
    reply = client.post(
        f"/sessions/{session_id}/messages", json={"message": "go"}, headers=_h()
    )
    assert reply.status_code == 200

    # --- per-caller scope ---------------------------------------------------
    assert client.get(f"/sessions/{session_id}", headers=_h(OTHER)).status_code == 404
    assert client.get("/sessions", headers=_h(OTHER)).json() == []
    assert [s["id"] for s in client.get("/sessions", headers=_h()).json()] == [session_id]

    # --- durability ---------------------------------------------------------
    stored = store.load_messages(session_id)
    assert len(stored) > 2, "the transcript did not reach the store"
    assert store.event_cursor(session_id) > 0

    # --- masking, in both directions and every sink -------------------------
    assert CANARY not in reply.text, "credential returned over HTTP"
    assert CANARY not in str(stored), "credential written to disk"
    assert CANARY not in str(store.load_events(session_id)), "credential in the event log"

    # --- the tool really ran ------------------------------------------------
    workspace = manager.get(session_id).workspace
    assert (workspace / "hello.txt").exists(), "the shell command did not run"

    # --- the action journal recorded it, durably ---------------------------
    rows = store._db.execute(
        "SELECT status, tool_name FROM actions WHERE session_id = ?", (session_id,)
    ).fetchall()
    assert rows, "no action was journalled"
    assert {row["status"] for row in rows} <= {"completed", "failed", "denied"}


def test_a_restart_resumes_the_same_session(stack, tmp_path):
    """The store is the only thing that survives; prove it is enough."""
    client, manager, store = stack
    session_id = client.post("/sessions", json={}, headers=_h()).json()["id"]
    client.post(f"/sessions/{session_id}/messages", json={"message": "go"}, headers=_h())
    persisted = len(store.load_messages(session_id))
    assert persisted > 0

    # A different process: new manager, same database, nothing in memory.
    revived_store = SQLiteStateStore(tmp_path / "state.db")
    revived = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR),
        FakeAsyncAnthropic(responder=_responder),
        state_store=revived_store,
        secrets=SecretRegistry.from_environ(extra_names=[ENV_NAME]),
    )
    restored = revived.restore_sessions()
    assert session_id in {s.id for s in restored}
    session = next(s for s in restored if s.id == session_id)
    assert len(session.agent.messages) == persisted
    asyncio.run(revived.stop())
    revived_store.close()


def test_the_audit_agrees_with_the_running_process(stack):
    """The audit must describe the deployment it is pointed at."""
    from mini_loop.audit import audit_posture

    client, _, _ = stack
    report = client.get("/healthz").json()
    blocking = [f for f in audit_posture(report, source="x") if f.severity == "high"]
    assert not blocking, [f.check for f in blocking]
