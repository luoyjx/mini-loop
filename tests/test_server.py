"""End-to-end server tests via FastAPI's TestClient, fake LLM, no API key."""

import json
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


def test_comprehensive_mode_is_assembled_end_to_end(tmp_path, monkeypatch):
    monkeypatch.setenv("MINILOOP_FEATURES", "all")
    monkeypatch.setenv("MINILOOP_MEMORY_ROOT", str(tmp_path / "memory"))
    with _client(tmp_path, monkeypatch) as c:
        sid = c.post("/sessions", json={}).json()["id"]
        session = c.app.state.manager.get(sid)
        assert {
            "glob", "remember", "create_task", "background_run", "schedule_cron",
            "spawn_teammate", "request_plan", "create_worktree", "connect_mcp",
        } <= set(session.agent.tools.names())
        run = c.post(f"/sessions/{sid}/messages", json={"message": "exercise the full harness"})
        assert run.status_code == 200
        assert run.json()["final"].startswith("Done.")


def test_unknown_session_404(tmp_path, monkeypatch):
    with _client(tmp_path, monkeypatch) as c:
        assert c.get("/sessions/deadbeef").status_code == 404
        assert c.post("/sessions/deadbeef/messages", json={"message": "x"}).status_code == 404


def test_sse_stream_emits_events(tmp_path, monkeypatch):
    with _client(tmp_path, monkeypatch) as c:
        sid = c.post("/sessions", json={}).json()["id"]
        seen, event_ids, payloads = [], [], []
        with c.stream("POST", f"/sessions/{sid}/messages/stream", json={"message": "run it"}) as s:
            cur = None
            for line in s.iter_lines():
                if line.startswith("event:"):
                    cur = line.split(":", 1)[1].strip()
                elif line.startswith("id:"):
                    event_ids.append(int(line.split(":", 1)[1].strip()))
                elif line.startswith("data:") and cur:
                    seen.append(cur)
                    payloads.append(json.loads(line.split(":", 1)[1].strip()))
        assert "status" in seen
        assert "tool_use" in seen
        assert "tool_result" in seen
        assert "model_start" in seen and "model_end" in seen
        assert "trajectory_start" in seen and "trajectory_end" in seen
        assert seen[-1] == "done"
        assert payloads[-1]["trajectory_id"].startswith("traj_")
        assert payloads[-1]["trajectory_persisted"] is True
        assert event_ids == sorted(event_ids) and len(event_ids) == len(set(event_ids))
        assert all({"seq", "ts", "session", "type"} <= payload.keys() for payload in payloads)


def test_trajectory_api_and_exports_survive_session_deletion(tmp_path, monkeypatch):
    with _client(tmp_path, monkeypatch) as c:
        sid = c.post("/sessions", json={}).json()["id"]
        assert c.post(f"/sessions/{sid}/messages", json={"message": "record me"}).status_code == 200

        summaries = c.get(f"/sessions/{sid}/trajectories").json()
        assert len(summaries) == 1
        trajectory_id = summaries[0]["id"]
        assert summaries[0]["status"] == "completed"
        assert summaries[0]["metrics"]["tool_calls"] == 1
        assert any(item["id"] == trajectory_id for item in c.get("/trajectories").json())

        trajectory = c.get(f"/trajectories/{trajectory_id}").json()
        assert trajectory["input"] == "record me"
        assert trajectory["events"][0]["type"] == "trajectory_start"
        assert trajectory["events"][-1]["type"] == "done"

        exported_json = c.get(f"/trajectories/{trajectory_id}/export?format=json")
        assert exported_json.status_code == 200
        assert "attachment" in exported_json.headers["content-disposition"]
        assert exported_json.json()["id"] == trajectory_id

        exported_jsonl = c.get(f"/trajectories/{trajectory_id}/export?format=jsonl")
        records = [json.loads(line) for line in exported_jsonl.text.splitlines()]
        assert records[0]["record_type"] == "trajectory_start"
        assert records[-1]["record_type"] == "trajectory_end"
        assert c.get(f"/trajectories/{trajectory_id}/export?format=csv").status_code == 400

        assert c.delete(f"/sessions/{sid}").status_code == 200
        assert c.get(f"/trajectories/{trajectory_id}").status_code == 200
        assert c.get("/trajectories/not-a-trajectory").status_code == 404


def test_trajectory_recording_can_be_disabled(tmp_path, monkeypatch):
    monkeypatch.setenv("MINILOOP_TRAJECTORIES", "0")
    with _client(tmp_path, monkeypatch) as c:
        assert c.get("/healthz").json()["trajectories"] is False
        sid = c.post("/sessions", json={}).json()["id"]
        run = c.post(f"/sessions/{sid}/messages", json={"message": "do not record"}).json()
        assert run["info"]["trajectory_count"] == 0
        assert c.get(f"/sessions/{sid}/trajectories").status_code == 503


def test_console_served(tmp_path, monkeypatch):
    with _client(tmp_path, monkeypatch) as c:
        body = c.get("/").text
        assert "mini-loop" in body and "New session" in body
        assert "Pushed events" in body
        assert 'aria-live="polite"' in body
        assert "new EventSource(" in body and "events?envelope=true" in body
        assert "View event payload" in body
        assert "Recorded trajectories" in body
        assert "View recording" in body
        assert "Export JSONL" in body
        assert "/sessions/'+encodeURIComponent(sid)+'/trajectories" in body
        assert "Open to render the complete payload." in body
