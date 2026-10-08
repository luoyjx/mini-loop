"""Execute Source summary metadata/type boundaries over explicit historical files."""
import copy
import hashlib
import json
import os
import tempfile
from pathlib import Path
from unittest.mock import patch


def trajectory_summary_contracts() -> dict:
    from fastapi.testclient import TestClient
    from mini_loop import trace_view
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    from mini_loop.trajectory import TrajectoryStore

    trace = "traj_" + "e" * 24
    seed = [{"record_type": "trajectory_start", "trajectory_id": trace, "session": "summary-session",
             "owner": "alice", "run_index": 1, "started_at": 1000.0, "input": "input", "metadata": {}},
            {"record_type": "event", "type": "workflow_started", "seq": 1, "ts": 1001.0},
            {"record_type": "trajectory_end", "status": "completed", "ended_at": 1002.0,
             "duration_ms": 2000.0, "output": "done", "metrics": {"event_count": 1}}]
    cases = []
    values = [("null", None), ("boolean", False), ("integer", 7), ("float", 1.5), ("text", "历史<&"),
              ("list", ["x", 2]), ("object", {"nested": [1, 1.0]}),
              ("nonfinite", float("nan")), ("surrogate", "\ud800")]
    for field in ("model", "workspace", "build"):
        for name, value in values:
            records = copy.deepcopy(seed)
            records[0]["metadata"][field] = value
            cases.append((field + "/" + name, records))
    for name, value in [("list", [1]), ("true", True), ("integer", 1), ("text", "metadata")]:
        records = copy.deepcopy(seed)
        records[0]["metadata"] = value
        cases.append(("invalid-metadata/" + name, records))
    for name, value in [("null", None), ("false", False), ("zero", 0), ("empty-text", ""),
                        ("empty-list", []), ("object", {}), ("true", True), ("text", "metrics"),
                        ("unknown", {"model_calls": 2, "extra": {"n": float("nan"), "text": "\udfff"}})]:
        records = copy.deepcopy(seed)
        records[-1]["metrics"] = value
        cases.append(("metrics/" + name, records))

    def capture(call):
        try:
            return json.dumps(call()), None
        except (AttributeError, TypeError, ValueError) as error:
            return None, type(error).__name__

    rows = []
    with patch.dict(os.environ, {"MINILOOP_API_TOKENS": "alice:token-a,bob:token-b", "MINILOOP_API_TOKEN": ""}), \
         patch.object(trace_view.time, "strftime", return_value="2000-01-02 03:04:05"):
        for name, records in cases:
            with tempfile.TemporaryDirectory(prefix="mini-loop-summary-") as scratch:
                store = TrajectoryStore(Path(scratch) / "traces")
                raw = "\n".join(json.dumps(record) for record in records) + "\n"
                path = store.root / (trace + ".jsonl")
                path.write_text(raw, encoding="utf-8")
                document = store.get(trace)
                summary, summary_error = capture(lambda: store.summary(trace))
                listing, listing_error = capture(store.list)
                def page_hash():
                    page = trace_view.render_html([trace_view.build_ledger(document)], title="mini-loop trace · fixture")
                    return hashlib.sha256(page.encode()).hexdigest()
                page, page_error = capture(page_hash)
                cfg = Settings(fake_llm=True, workspace_root=Path(scratch) / "ws", trajectory_enabled=False,
                               ast_outline_enabled=False)
                manager = SessionManager(cfg, FakeAsyncAnthropic(), trajectory_store=store)
                routes = []
                with TestClient(create_app(settings=cfg, manager=manager), raise_server_exceptions=False) as client:
                    for suffix, token in [("/view", "token-a"), ("/view", "token-b"), ("", "token-a"),
                                          ("/export?format=json", "token-a"), ("/export?format=jsonl", "token-a"),
                                          ("LIST", "token-a"), ("LIST", "token-b")]:
                        target = "/trajectories" if suffix == "LIST" else f"/trajectories/{trace}{suffix}"
                        response = client.get(target, headers={"Authorization": "Bearer " + token})
                        routes.append({"suffix": suffix, "token": token, "status": response.status_code,
                                       "body_wire": json.dumps(response.json()) if suffix == "LIST" and response.status_code == 200 else None,
                                       "body_sha256": hashlib.sha256(response.content).hexdigest()
                                       if suffix == "/view" and response.status_code == 200 else None})
                rows.append({"name": name, "raw": raw, "document_wire": json.dumps(document),
                             "summary_wire": summary, "summary_error": summary_error,
                             "listing_wire": listing, "listing_error": listing_error,
                             "page_sha256": json.loads(page) if page else None, "page_error": page_error,
                             "http": routes})
    return {"rows": rows}
