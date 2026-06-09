"""End-to-end server tests via FastAPI's TestClient, fake LLM, no API key."""

from pathlib import Path

from fastapi.testclient import TestClient

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _client(tmp_path, monkeypatch) -> TestClient:
    monkeypatch.setenv("MINILOOP_FAKE_LLM", "1")
    monkeypatch.setenv("MINILOOP_WORKSPACE_ROOT", str(tmp_path / "ws"))
    monkeypatch.setenv("MINILOOP_SKILLS_DIR", str(SKILLS_DIR))
    from mini_loop.server import app
    return TestClient(app)


def test_health_crud_and_message(tmp_path, monkeypatch):
    with _client(tmp_path, monkeypatch) as c:
        health = c.get("/healthz").json()
        assert health["status"] == "ok" and health["fake_llm"] is True

        sid = c.post("/sessions", json={"system": "be terse"}).json()["id"]
        assert c.get(f"/sessions/{sid}").json()["status"] == "idle"
        assert any(s["id"] == sid for s in c.get("/sessions").json())

        run = c.post(f"/sessions/{sid}/messages", json={"message": "hi"}).json()
        assert run["final"].startswith("Done.")
        assert run["info"]["run_count"] == 1
        assert run["info"]["message_count"] >= 3  # user, assistant(tool), user(result), assistant

        assert c.delete(f"/sessions/{sid}").json()["deleted"] == sid
        assert c.get(f"/sessions/{sid}").status_code == 404


def test_unknown_session_404(tmp_path, monkeypatch):
    with _client(tmp_path, monkeypatch) as c:
        assert c.get("/sessions/deadbeef").status_code == 404
        assert c.post("/sessions/deadbeef/messages", json={"message": "x"}).status_code == 404


def test_sse_stream_emits_events(tmp_path, monkeypatch):
    with _client(tmp_path, monkeypatch) as c:
        sid = c.post("/sessions", json={}).json()["id"]
        seen = []
        with c.stream("POST", f"/sessions/{sid}/messages/stream", json={"message": "run it"}) as s:
            cur = None
            for line in s.iter_lines():
                if line.startswith("event:"):
                    cur = line.split(":", 1)[1].strip()
                elif line.startswith("data:") and cur:
                    seen.append(cur)
        assert "status" in seen
        assert "tool_use" in seen
        assert "tool_result" in seen
        assert seen[-1] == "done"


def test_console_served(tmp_path, monkeypatch):
    with _client(tmp_path, monkeypatch) as c:
        body = c.get("/").text
        assert "mini-loop" in body and "new session" in body
