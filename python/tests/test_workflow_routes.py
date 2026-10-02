"""The workflow HTTP surface: observe and cancel by ownership, launch by stamp.

The routes follow the house rules -- session routes go through _require, a
foreign run reads as missing -- plus one rule of their own: launch mints an
`explicit_human` RunContext with the single `workflow.launch` capability,
and ONLY on an authenticated deployment. Unlike /messages (untrusted on
purpose: message text flows through the model), the launch payload IS the
single action the human invokes, so the stamp is honest -- but an anonymous
bind cannot claim to be the human, so an open deployment refuses with 403
and the disabled feature reads as an explicit `enabled: false`, never as a
vacuously empty list.
"""

import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.workflows.models import (
    NodeKind,
    WorkflowDefinition,
    WorkflowNode,
)

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"

ALICE = {"Authorization": "Bearer tok-alice"}
BOB = {"Authorization": "Bearer tok-bob"}

VALUE_SCHEMA = {
    "type": "object",
    "required": ["value"],
    "properties": {"value": {"type": "string"}},
    "additionalProperties": False,
}


def _definition_dict() -> dict:
    return WorkflowDefinition(
        name="route-audit",
        description="One-node workflow for route tests.",
        nodes=(WorkflowNode("discover", NodeKind.AGENT,
                            output_schema=VALUE_SCHEMA),),
        return_from="discover",
        input_schema={
            "type": "object",
            "required": ["question"],
            "properties": {"question": {"type": "string"}},
            "additionalProperties": False,
        },
        output_schema=VALUE_SCHEMA,
    ).to_dict()


def _client(tmp_path, *, workflows=True, authed=True):
    from fastapi.testclient import TestClient

    from mini_loop.auth import TokenAuth
    from mini_loop.server import create_app

    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS, spill_dir=None)
    manager = SessionManager(settings, FakeAsyncAnthropic(),
                             enable_workflows=workflows)
    app = create_app(manager=manager, settings=settings)
    client = TestClient(app)
    client.__enter__()
    if authed:
        app.state.auth = TokenAuth({"tok-alice": "alice", "tok-bob": "bob"})
    return client, manager


def _sid(client, headers=None):
    return client.post("/sessions", json={}, headers=headers or {}).json()["id"]


def test_a_disabled_deployment_says_so(tmp_path):
    client, _ = _client(tmp_path, workflows=False)
    try:
        sid = _sid(client, ALICE)
        listed = client.get(f"/sessions/{sid}/workflows", headers=ALICE).json()
        assert listed == {"enabled": False, "runs": []}
        assert client.post(f"/sessions/{sid}/workflows",
                           json={"definition": _definition_dict(),
                                 "args": {"question": "q"}},
                           headers=ALICE).status_code == 404
        assert client.get(f"/sessions/{sid}/workflows/wfr-1",
                          headers=ALICE).status_code == 404
    finally:
        client.__exit__(None, None, None)


def test_an_open_deployment_observes_but_cannot_launch(tmp_path):
    client, _ = _client(tmp_path, authed=False)
    try:
        sid = _sid(client)
        assert client.get(f"/sessions/{sid}/workflows").json()["enabled"] is True
        refused = client.post(f"/sessions/{sid}/workflows",
                              json={"definition": _definition_dict(),
                                    "args": {"question": "q"}})
        assert refused.status_code == 403
        assert "anonymous" in refused.json()["detail"]
    finally:
        client.__exit__(None, None, None)


def test_launch_observe_cancel_roundtrip(tmp_path):
    client, manager = _client(tmp_path)
    try:
        sid = _sid(client, ALICE)
        launched = client.post(f"/sessions/{sid}/workflows",
                               json={"definition": _definition_dict(),
                                     "args": {"question": "audit the repo"}},
                               headers=ALICE)
        assert launched.status_code == 200, launched.text
        body = launched.json()
        run_id = body["run_id"]
        assert body["workflow_name"] == "route-audit"
        assert body["action_id"].startswith("wfhttp_")

        runs = client.get(f"/sessions/{sid}/workflows",
                          headers=ALICE).json()["runs"]
        assert [r["run_id"] for r in runs] == [run_id]

        detail = client.get(f"/sessions/{sid}/workflows/{run_id}",
                            headers=ALICE).json()
        assert detail["workflow_name"] == "route-audit"
        assert [n["node_id"] for n in detail["nodes"]] == ["discover"]

        cancelled = client.post(f"/sessions/{sid}/workflows/{run_id}/cancel",
                                json={}, headers=ALICE)
        assert cancelled.status_code == 200
        assert client.get(f"/sessions/{sid}/workflows/{run_id}",
                          headers=ALICE).json()["status"] in (
            "CANCELLED", "FAILED", "COMPLETED")
    finally:
        client.__exit__(None, None, None)


def test_the_same_action_id_returns_the_same_run(tmp_path):
    """The idempotency the tool path already has, reachable over HTTP: a
    network retry of the launch must not start a second workflow."""

    client, _ = _client(tmp_path)
    try:
        sid = _sid(client, ALICE)
        payload = {"definition": _definition_dict(),
                   "args": {"question": "q"}, "action_id": "act-once"}
        first = client.post(f"/sessions/{sid}/workflows", json=payload,
                            headers=ALICE).json()
        second = client.post(f"/sessions/{sid}/workflows", json=payload,
                             headers=ALICE).json()
        assert first["run_id"] == second["run_id"]
        assert second["reused"] is True
        runs = client.get(f"/sessions/{sid}/workflows",
                          headers=ALICE).json()["runs"]
        assert len(runs) == 1
    finally:
        client.__exit__(None, None, None)


def test_strangers_and_foreign_runs_read_as_missing(tmp_path):
    client, _ = _client(tmp_path)
    try:
        sid = _sid(client, ALICE)
        run_id = client.post(f"/sessions/{sid}/workflows",
                             json={"definition": _definition_dict(),
                                   "args": {"question": "q"}},
                             headers=ALICE).json()["run_id"]

        assert client.get(f"/sessions/{sid}/workflows",
                          headers=BOB).status_code == 404
        assert client.get(f"/sessions/{sid}/workflows/{run_id}",
                          headers=BOB).status_code == 404

        # Bob's own session cannot see Alice's run either: scoping is by
        # session inside the service, not only by the URL path.
        bob_sid = _sid(client, BOB)
        assert client.get(f"/sessions/{bob_sid}/workflows/{run_id}",
                          headers=BOB).status_code == 404
        assert client.post(f"/sessions/{bob_sid}/workflows/{run_id}/cancel",
                           json={}, headers=BOB).status_code == 404
    finally:
        client.__exit__(None, None, None)


def test_the_env_flag_reaches_the_default_manager(tmp_path):
    """MINILOOP_EXPERIMENTAL_WORKFLOWS sets Settings.enable_workflows; the
    server's own manager construction must forward it, or the flag is dead
    on every real deployment and the panel always says disabled."""

    from fastapi.testclient import TestClient

    from mini_loop.server import create_app

    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS, spill_dir=None,
                        enable_workflows=True)
    app = create_app(settings=settings)
    with TestClient(app) as client:
        sid = client.post("/sessions", json={}).json()["id"]
        assert client.get(f"/sessions/{sid}/workflows").json()["enabled"] is True


def test_a_malformed_definition_is_400_not_500(tmp_path):
    client, _ = _client(tmp_path)
    try:
        sid = _sid(client, ALICE)
        bad = client.post(f"/sessions/{sid}/workflows",
                          json={"definition": {"name": "x"}, "args": {}},
                          headers=ALICE)
        assert bad.status_code == 400
    finally:
        client.__exit__(None, None, None)
