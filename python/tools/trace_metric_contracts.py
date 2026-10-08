"""Run actual ledger integer conversion/fold/render phases over historical files."""
import copy
import hashlib
import json
import os
import tempfile
from pathlib import Path
from unittest.mock import patch


def trace_metric_contracts() -> dict:
    from fastapi.testclient import TestClient
    from mini_loop import trace_view
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    from mini_loop.trajectory import TrajectoryStore

    trace = "traj_" + "f" * 24
    base = {"trajectory_id": trace, "session": "metric-session", "run_index": 1, "status": "completed",
            "started_at": 1000.0, "ended_at": 1002.0, "duration_ms": 2000.0, "input": "input",
            "output": "done", "metrics": {}, "events": []}
    variants = [("null", None), ("false", False), ("true", True), ("zero", 0), ("negative", -7),
                ("float", -12.9), ("negative-zero", -0.0), ("large-float", 1e30), ("max-float", float.fromhex('0x1.fffffffffffffp+1023')),
                ("wide-integer", 2**80), ("numeric-text", "  +12_345  "), ("unicode-digits", "\u0085-１２_٣\u3000"),
                ("leading-zeros", "0_0_7"), ("empty-text", ""), ("space-only", "   "),
                ("separator-space", "\x1c12"), ("invalid-underscore", "1__2"), ("decimal-text", "1.5"),
                ("float-text", "1e3"), ("empty-list", []), ("list", [1]), ("empty-object", {}), ("object", {"x": 1}),
                ("nan", float("nan")), ("infinity", float("inf")), ("negative-infinity", -float("inf")),
                ("surrogate", "\ud800"), ("digit-limit", "9"*4300), ("over-digit-limit", "9"*4301)]
    cases = []
    for role in ("counter", "usage"):
        for name, value in variants:
            document = copy.deepcopy(base)
            if role == "counter":
                document["metrics"] = {"model_calls": value, "input_tokens": "overwritten", "output_tokens": float("nan")}
            else:
                document["events"] = [{"type": "model_start", "span_id": "m", "seq": 1, "ts": 1000.5},
                                      {"type": "model_end", "span_id": "m", "seq": 2, "ts": 1001.0,
                                       "usage": {"input_tokens": value, "output_tokens": 2}, "duration_ms": 500.0}]
            cases.append((role + "/" + name, document))
    for name, metrics in [("pairs", [["model_calls", "12"], ["tool_calls", 1.5], ["model_calls", "13"]]),
                          ("string-pair", ["ab"]), ("object-pair", [{"model_calls": 1, "17": 2}]),
                          ("integer-key", [[1, "ignored"], ["errors", True]]),
                          ("scalar-key-equality", [[True, "first"], [1, "second"], [1.0, "last"], ["model_calls", 2]]),
                          ("legacy-keys", [[None, "none"], [float("inf"), "positive"], [-float("inf"), "negative"],
                                           [float("nan"), "first"], [float("nan"), "last"], ["model_calls", "12"]]),
                          ("unicode-pair", ["甲🎈"]), ("unhashable-object-key", [[{}, "value"]]),
                          ("unhashable-key", [[[1], 2]]), ("bad-pair", [["model_calls"]]),
                          ("bad-item", [1])]:
        document = copy.deepcopy(base)
        document["metrics"] = metrics
        cases.append(("container/" + name, document))
    for name, value in [("wide-sum", 2**63-1), ("format-limit", int("9"*4300))]:
        document = copy.deepcopy(base)
        document["events"] = []
        for i in range(2):
            document["events"].extend([{"type": "model_start", "span_id": str(i), "seq": i*2+1},
                                       {"type": "model_end", "span_id": str(i), "seq": i*2+2,
                                        "usage": {"input_tokens": value, "output_tokens": value}}])
        cases.append(("fold/" + name, document))

    rows = []
    with patch.dict(os.environ, {"MINILOOP_API_TOKENS": "alice:token-a,bob:token-b", "MINILOOP_API_TOKEN": ""}), \
         patch.object(trace_view.time, "strftime", return_value="2000-01-02 03:04:05"):
        for name, document in cases:
            # Both implementations receive the same decoded wire values,
            # including the Source decoder's shared nonfinite constants.
            document = json.loads(json.dumps(document))
            build_error = render_error = metrics_error = metrics_wire = page_hash = multi_hash = multi_error = None
            try:
                ledger = trace_view.build_ledger(document)
            except (AttributeError, TypeError, ValueError, OverflowError) as error:
                build_error = type(error).__name__
            if build_error is None:
                try:
                    metrics_wire = json.dumps(ledger["metrics"])
                except ValueError as error:
                    metrics_error = type(error).__name__
                for copies in (1, 2):
                    try:
                        page = trace_view.render_html([ledger]*copies, title="mini-loop trace · fixture")
                        digest = hashlib.sha256(page.encode()).hexdigest()
                        if copies == 1: page_hash = digest
                        else: multi_hash = digest
                    except (TypeError, ValueError, OverflowError) as error:
                        if copies == 1: render_error = type(error).__name__
                        else: multi_error = type(error).__name__
            start = {"record_type": "trajectory_start", "trajectory_id": trace, "session": "metric-session",
                     "owner": "alice", "run_index": 1, "started_at": 1000.0, "input": "input", "metadata": {}}
            end = {"record_type": "trajectory_end", "status": "completed", "ended_at": 1002.0,
                   "duration_ms": 2000.0, "output": "done", "metrics": document["metrics"]}
            raw = "\n".join(json.dumps(record) for record in [start, *[{"record_type": "event", **e} for e in document["events"]], end]) + "\n"
            with tempfile.TemporaryDirectory(prefix="mini-loop-metrics-") as scratch:
                store = TrajectoryStore(Path(scratch)/"traces")
                (store.root/(trace+".jsonl")).write_text(raw, encoding="utf-8")
                cfg = Settings(fake_llm=True, workspace_root=Path(scratch)/"ws", trajectory_enabled=False, ast_outline_enabled=False)
                manager = SessionManager(cfg, FakeAsyncAnthropic(), trajectory_store=store)
                routes = []
                with TestClient(create_app(settings=cfg, manager=manager), raise_server_exceptions=False) as client:
                    for suffix, token in [("/view", "token-a"), ("/view", "token-b"), ("", "token-a"), ("/export?format=json", "token-a")]:
                        response = client.get(f"/trajectories/{trace}{suffix}", headers={"Authorization": "Bearer "+token})
                        routes.append({"suffix": suffix, "token": token, "status": response.status_code,
                                       "body_sha256": hashlib.sha256(response.content).hexdigest()
                                       if suffix == "/view" and response.status_code == 200 else None})
            rows.append({"name": name, "document_wire": json.dumps(document), "raw": raw,
                         "build_error": build_error, "render_error": render_error,
                         "metrics_wire": metrics_wire, "metrics_error": metrics_error, "page_sha256": page_hash,
                         "multi_sha256": multi_hash, "multi_error": multi_error, "http": routes})
    return {"rows": rows}
