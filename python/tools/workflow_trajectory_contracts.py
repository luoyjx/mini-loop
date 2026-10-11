"""Source file, privacy and HTTP evidence for historical workflow payloads."""
import json
import os
import tempfile
from pathlib import Path
from unittest.mock import patch


def workflow_trajectory_contracts() -> dict:
    from fastapi.testclient import TestClient
    from mini_loop.config import Settings
    from mini_loop.events import WorkflowEvent
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    from mini_loop.trajectory import TrajectoryStore

    recipes = [
        ("nonfinite", {"values": [float("nan"), float("inf"), -float("inf")]}),
        ("surrogate-text", {"text": "\ud800"}),
        ("surrogate-key", {"\udfff": "value"}),
        ("redacted-float", {"error": float("nan")}),
        ("mixed", {"text": "\ud800", "kept": [float("inf"), 1, 1.0]}),
    ]
    rows = []
    with patch.dict(os.environ, {"MINILOOP_API_TOKENS": "alice:token-a,bob:token-b"}):
        for capture in (True, False):
            for name, payload in recipes:
                with tempfile.TemporaryDirectory(prefix="mini-loop-workflow-trajectory-") as scratch:
                    store = TrajectoryStore(Path(scratch)/"traces", capture_content=capture)
                    trace = store.start(session_id="s", owner="alice", run_index=1, input_text="input")
                    event = WorkflowEvent(kind="workflow_paused", session_id="s", run_id="run",
                                          workflow_name="wf", definition_revision="rev", event_id="event",
                                          occurred_at=1.5, payload=payload).as_session_event()
                    record = {**event, "seq": 1, "sequence": 1, "ts": 0, "session": "s",
                              "transcript_epoch": 1, "trajectory_id": trace, "trace_id": trace, "group_id": "s"}
                    failure = None
                    try:
                        store.append(trace, record)
                    except Exception as error:
                        failure = type(error).__name__
                    store.finish(trace, status="completed", output="done", duration_ms=0)

                    def normalize(value):
                        if isinstance(value, dict):
                            return {key: 0 if key in {"started_at", "ended_at", "duration_ms"} else normalize(item)
                                    for key, item in value.items()}
                        if isinstance(value, list):
                            return [normalize(item) for item in value]
                        if isinstance(value, str):
                            return value.replace(trace, "traj_"+"c"*24)
                        return value

                    cfg = Settings(fake_llm=True, workspace_root=Path(scratch)/"ws", trajectory_enabled=False,
                                   ast_outline_enabled=False)
                    manager = SessionManager(cfg, FakeAsyncAnthropic(), trajectory_store=store)
                    with TestClient(create_app(settings=cfg, manager=manager), raise_server_exceptions=False) as http:
                        routes = []
                        for suffix, token in [("", "token-a"), ("/export?format=json", "token-a"),
                                              ("/export?format=jsonl", "token-a"), ("/export?format=json", "token-b")]:
                            response = http.get(f"/trajectories/{trace}{suffix}", headers={"Authorization": f"Bearer {token}"})
                            routes.append({"suffix": suffix, "token": token, "status": response.status_code})
                    rows.append({"name": name, "capture": capture, "input_wire": json.dumps(normalize(record)),
                                 "append_error": failure, "summary": normalize(store.summary(trace)),
                                 "document_wire": json.dumps(normalize(store.get(trace))),
                                 "records": [json.dumps(normalize(json.loads(line))) for line in store.raw(trace).splitlines()],
                                 "iter_events": [json.dumps(normalize(row)) for row in store.iter_events(trace, types={"workflow_paused"})],
                                 "http": routes})
    return {"rows": rows}
