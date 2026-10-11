"""The server routes the web UI grew in R2-R5, scoped like everything else.

Every new route follows the house rules: session routes go through
_require (a stranger's probe answers "not found", never "forbidden"),
the self-audit is owner-scoped under authentication (cross-tenant
operational metadata must not leak through an observability endpoint),
and the improvement route surfaces self_improve.py's refusals as 400s.
"""

import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"

ALICE = {"Authorization": "Bearer tok-alice"}
BOB = {"Authorization": "Bearer tok-bob"}


def _client(tmp_path, *, authed=True):
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.server import create_app

    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS, spill_dir=None)
    manager = SessionManager(settings, FakeAsyncAnthropic())
    app = create_app(manager=manager, settings=settings)
    client = TestClient(app)
    client.__enter__()
    if authed:
        app.state.auth = TokenAuth({"tok-alice": "alice", "tok-bob": "bob"})
    return client


def _sid(client, headers=None):
    return client.post("/sessions", json={}, headers=headers or {}).json()["id"]


def test_cron_schedule_list_cancel_roundtrip(tmp_path):
    client = _client(tmp_path)
    try:
        sid = _sid(client, ALICE)
        created = client.post(f"/sessions/{sid}/cron",
                              json={"cron": "*/5 * * * *", "prompt": "check in"},
                              headers=ALICE)
        assert created.status_code == 200

        jobs = client.get(f"/sessions/{sid}/cron", headers=ALICE).json()["jobs"]
        assert len(jobs) == 1
        job = jobs[0]
        assert job["cron"] == "*/5 * * * *"
        # Scheduling over authenticated HTTP is the human edge: armed.
        assert job["armed"] is True

        gone = client.delete(f"/sessions/{sid}/cron/{job['id']}", headers=ALICE)
        assert gone.status_code == 200
        assert client.get(f"/sessions/{sid}/cron", headers=ALICE).json()["jobs"] == []
    finally:
        client.__exit__(None, None, None)


def test_a_strangers_cron_probe_answers_not_found(tmp_path):
    client = _client(tmp_path)
    try:
        sid = _sid(client, ALICE)
        client.post(f"/sessions/{sid}/cron",
                    json={"cron": "*/5 * * * *", "prompt": "x"}, headers=ALICE)
        assert client.get(f"/sessions/{sid}/cron", headers=BOB).status_code == 404
        job_id = client.get(f"/sessions/{sid}/cron",
                            headers=ALICE).json()["jobs"][0]["id"]
        assert client.delete(f"/sessions/{sid}/cron/{job_id}",
                             headers=BOB).status_code == 404
    finally:
        client.__exit__(None, None, None)


def test_a_bad_cron_expression_is_400(tmp_path):
    client = _client(tmp_path)
    try:
        sid = _sid(client, ALICE)
        bad = client.post(f"/sessions/{sid}/cron",
                          json={"cron": "not cron", "prompt": "x"}, headers=ALICE)
        assert bad.status_code == 400
    finally:
        client.__exit__(None, None, None)


def test_self_audit_is_owner_scoped_under_auth(tmp_path):
    client = _client(tmp_path)
    try:
        _sid(client, ALICE)
        _sid(client, BOB)
        _sid(client, BOB)
        report = client.get("/self-audit", headers=ALICE).text
        assert "sessions (1)" in report, "alice must see only her own session"
        # Manager-wide subsystem ledgers stay out of tenant responses.
        assert "### cron:" not in report
        bob_report = client.get("/self-audit", headers=BOB).text
        assert "sessions (2)" in bob_report
    finally:
        client.__exit__(None, None, None)


def test_self_audit_is_global_on_an_open_deployment(tmp_path):
    client = _client(tmp_path, authed=False)
    try:
        _sid(client)
        report = client.get("/self-audit").text
        assert "# self-audit" in report and "sessions (1)" in report
        assert "## cron" in report  # the operator view keeps the global tail
    finally:
        client.__exit__(None, None, None)


def test_skills_and_memory_routes_answer_for_the_owner(tmp_path):
    client = _client(tmp_path)
    try:
        sid = _sid(client, ALICE)
        skills = client.get(f"/sessions/{sid}/skills", headers=ALICE)
        assert skills.status_code == 200
        assert "catalogue" in skills.json()
        memory = client.get(f"/sessions/{sid}/memory", headers=ALICE)
        assert memory.status_code == 200
        assert memory.json()["memories"] == []
        assert client.get(f"/sessions/{sid}/skills", headers=BOB).status_code == 404
        assert client.get(f"/sessions/{sid}/memory", headers=BOB).status_code == 404
    finally:
        client.__exit__(None, None, None)


def test_propose_improvement_surfaces_the_git_refusal_as_400(tmp_path):
    client = _client(tmp_path)
    try:
        sid = _sid(client, ALICE)
        response = client.post(f"/sessions/{sid}/propose-improvement",
                               json={"objective": "improve things",
                                     "acceptance_command": "true"},
                               headers=ALICE)
        assert response.status_code == 400
        assert "git checkout" in response.json()["detail"]
    finally:
        client.__exit__(None, None, None)


def test_memory_body_serves_the_owners_stored_masked_text(tmp_path):
    client = _client(tmp_path)
    try:
        sid = _sid(client, ALICE)
        # Write a memory through the session's own scoped store.
        from mini_loop.memory import memory_store_for

        session = client.app.state.manager._sessions[sid]
        memory_store_for(session.agent).write(
            "deploy-ritual", "reference", "how we deploy", "step one: breathe")

        listing = client.get(f"/sessions/{sid}/memory", headers=ALICE).json()
        assert listing["memories"][0]["name"] == "deploy-ritual"
        body = client.get(f"/sessions/{sid}/memory/deploy-ritual",
                          headers=ALICE).json()
        assert body["body"] == "step one: breathe"
        missing = client.get(f"/sessions/{sid}/memory/nope", headers=ALICE)
        assert missing.status_code == 404
        assert client.get(f"/sessions/{sid}/memory/deploy-ritual",
                          headers=BOB).status_code == 404
    finally:
        client.__exit__(None, None, None)


def test_the_ui_benchmark_is_fake_only_and_reports_parity(tmp_path):
    client = _client(tmp_path)
    try:
        report = client.post("/benchmark", headers=ALICE).json()
        assert report["real"] is False
        assert "fake transport" in report["note"]
        assert report["comparison"]["verdict"] == "not_worse"
        assert report["comparison"]["tasks"] == 5
        # The promotion gate's second opinion rides the same report: tasks
        # outside the visible loop, compared with the same instrument.
        assert report["heldout_comparison"]["verdict"] == "not_worse"
        assert report["heldout_comparison"]["tasks"] == 3
    finally:
        client.__exit__(None, None, None)


def test_the_task_board_route_reflects_claims(tmp_path):
    from mini_loop.tasks import TaskStore

    client = _client(tmp_path)
    try:
        sid = _sid(client, ALICE)
        session = client.app.state.manager._sessions[sid]
        board = TaskStore(session.workspace)
        first = board.create("write the report")
        second = board.create("review it", blocked_by=[first.id])
        board.claim(first.id, "alice")

        payload = client.get(f"/sessions/{sid}/tasks", headers=ALICE).json()
        by_id = {t["id"]: t for t in payload["tasks"]}
        assert by_id[first.id]["status"] == "in_progress"
        assert by_id[first.id]["owner"] == "alice"
        assert by_id[second.id]["blockedBy"] == [first.id]
        assert client.get(f"/sessions/{sid}/tasks", headers=BOB).status_code == 404
    finally:
        client.__exit__(None, None, None)


def test_the_goal_route_reports_objective_and_flags(tmp_path):
    client = _client(tmp_path)
    try:
        sid = _sid(client, ALICE)
        empty = client.get(f"/sessions/{sid}/goal", headers=ALICE).json()
        assert empty["goal"] is None and empty["plan_mode"] is False

        session = client.app.state.manager._sessions[sid]
        session.agent.state["goal"] = {
            "id": "goal_x", "revision": 1, "objective": "ship the UI",
            "phase": "active", "rounds_started": 2, "max_rounds": 5,
            "blocked": None,
        }
        session.agent.state["goal_armed"] = True
        session.agent.state["plan_mode"] = True

        payload = client.get(f"/sessions/{sid}/goal", headers=ALICE).json()
        assert payload["goal"]["objective"] == "ship the UI"
        assert payload["goal_armed"] is True and payload["plan_mode"] is True
        assert client.get(f"/sessions/{sid}/goal", headers=BOB).status_code == 404
    finally:
        client.__exit__(None, None, None)


def test_the_team_pane_peeks_without_consuming(tmp_path):
    """The round-250 blocker, resolved: a UI view of an inbox must never
    deliver the agent's messages to nobody. After a peek, read() still
    hands the injector every message."""

    from mini_loop.teams import team_key

    client = _client(tmp_path)
    try:
        sid = _sid(client, ALICE)
        session = client.app.state.manager._sessions[sid]
        session.agent.state["team_id"] = "team1"
        session.agent.state["agent_name"] = "lead"
        bus = client.app.state.manager.bus
        bus.send(team_key("team1", "worker"), team_key("team1", "lead"),
                 "the build is green")

        pane = client.get(f"/sessions/{sid}/team", headers=ALICE).json()
        assert pane["team"] == "team1"
        assert len(pane["inbox"]) == 1
        assert "build is green" in pane["inbox"][0]["content"]

        # Peek again: still there. Then the delivery path consumes it.
        again = client.get(f"/sessions/{sid}/team", headers=ALICE).json()
        assert len(again["inbox"]) == 1
        delivered = bus.read(team_key("team1", "lead"))
        assert len(delivered) == 1, "the peek consumed the agent's message"

        after = client.get(f"/sessions/{sid}/team", headers=ALICE).json()
        assert after["inbox"] == []
    finally:
        client.__exit__(None, None, None)


def test_a_solo_session_is_its_own_team_with_an_empty_inbox(tmp_path):
    """Every session is created as a one-member team (team_id = its own id,
    name 'lead'), so the pane shows that identity and an empty inbox rather
    than a 'no team' state."""

    client = _client(tmp_path)
    try:
        sid = _sid(client, ALICE)
        payload = client.get(f"/sessions/{sid}/team", headers=ALICE).json()
        assert payload["team"] == sid and payload["name"] == "lead"
        assert payload["inbox"] == []
        assert client.get(f"/sessions/{sid}/team", headers=BOB).status_code == 404
    finally:
        client.__exit__(None, None, None)
