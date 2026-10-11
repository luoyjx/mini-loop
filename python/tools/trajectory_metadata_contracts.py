"""Actual historical metadata reads; file seeds are explicit, not runtime writes."""
import copy
import hashlib
import json
import os
import tempfile
from pathlib import Path
from unittest.mock import patch


def trajectory_metadata_contracts() -> dict:
    from fastapi.testclient import TestClient
    from mini_loop import trace_view
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    from mini_loop.trajectory import TrajectoryStore

    trace = "traj_" + "d" * 24
    header = {"record_type": "trajectory_start", "trajectory_id": trace,
              "session": "metadata-session", "owner": "alice", "run_index": 1,
              "started_at": 1000.0, "input": "historical input", "metadata": {}}
    event = {"record_type": "event", "type": "workflow_started", "seq": 1,
             "ts": 1001.0, "workflow_id": "wf_fixture"}
    end = {"record_type": "trajectory_end", "status": "completed", "ended_at": 1002.0,
           "duration_ms": 2000.0, "output": "done", "metrics": {"event_count": 1}}
    cases = []
    for name, value in [("null", None), ("false", False), ("zero", 0),
                        ("empty-text", ""), ("empty-list", []), ("object", {}),
                        ("unknown-nonfinite", {"model": "model", "workspace": "/ws", "build": "build",
                                               "extra": [float("nan"), float("inf"), -float("inf"), 1, 1.0]}),
                        ("unknown-surrogate", {"model": "model", "extra": {"\ud800": "\udfff"}})]:
        start = copy.deepcopy(header)
        start["metadata"] = value
        cases.append((name, [start, copy.deepcopy(event), copy.deepcopy(end)]))
    for name, index in [("unknown-header", 0), ("unknown-terminal", 2)]:
        records = copy.deepcopy([header, event, end])
        records[index]["unused"] = {"n": float("nan"), "text": "\ud800"}
        cases.append((name, records))
    records = copy.deepcopy([header, event, end])
    records[-1]["metrics"]["unused"] = {"n": float("nan"), "text": "\udfff"}
    cases.append(("unknown-metrics", records))

    rows = []
    with patch.dict(os.environ, {"MINILOOP_API_TOKENS": "alice:token-a,bob:token-b", "MINILOOP_API_TOKEN": ""}), \
         patch.object(trace_view.time, "strftime", return_value="2000-01-02 03:04:05"):
        for name, records in cases:
            with tempfile.TemporaryDirectory(prefix="mini-loop-metadata-") as scratch:
                root = Path(scratch) / "traces"
                store = TrajectoryStore(root)
                path = root / (trace + ".jsonl")
                raw = "\n".join(json.dumps(record) for record in records) + "\n"
                path.write_text(raw, encoding="utf-8")
                document = store.get(trace)
                summary = store.summary(trace)
                # Root fields unused by the ledger must not affect its decoder.
                direct = {**document, "unused": {"n": float("nan"), "text": "\ud800"}}
                page = trace_view.render_html([trace_view.build_ledger(direct)], title="mini-loop trace · fixture")
                file_page = trace_view.render_html([trace_view.build_ledger(trace_view.assemble_file(path))],
                                                   title="mini-loop trace · fixture")
                if page != file_page:
                    raise AssertionError("metadata changed file page")
                cfg = Settings(fake_llm=True, workspace_root=Path(scratch) / "ws", trajectory_enabled=False,
                               ast_outline_enabled=False)
                manager = SessionManager(cfg, FakeAsyncAnthropic(), trajectory_store=store)
                routes = []
                with TestClient(create_app(settings=cfg, manager=manager), raise_server_exceptions=False) as client:
                    for suffix, token in [("/view", "token-a"), ("/view", "token-b"), ("", "token-a"),
                                          ("/export?format=json", "token-a"), ("/export?format=jsonl", "token-a")]:
                        response = client.get(f"/trajectories/{trace}{suffix}",
                                              headers={"Authorization": "Bearer " + token})
                        routes.append({"suffix": suffix, "token": token, "status": response.status_code,
                                       "body_sha256": hashlib.sha256(response.content).hexdigest()
                                       if suffix == "/view" and response.status_code == 200 else None})
                rows.append({"name": name, "raw": raw, "document_wire": json.dumps(document),
                             "direct_wire": json.dumps(direct), "summary_wire": json.dumps(summary),
                             "page_sha256": hashlib.sha256(page.encode()).hexdigest(), "http": routes})
    return {"rows": rows}
