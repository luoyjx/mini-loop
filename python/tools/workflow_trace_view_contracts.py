"""Source ledgers, final UTF-8 pages, file assembly, CLI and owned view routes."""
import contextlib
import copy
import hashlib
import io
import json
import os
import tempfile
from pathlib import Path
from unittest.mock import patch


def workflow_trace_view_contracts() -> dict:
    from fastapi.testclient import TestClient
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    from mini_loop.trajectory import TrajectoryStore
    from mini_loop import trace_view
    from tools.workflow_trajectory_contracts import workflow_trajectory_contracts

    profiles = workflow_trajectory_contracts()["rows"]
    cases = [(row["name"]+"/"+str(row["capture"]), json.loads(row["document_wire"]), row["records"]) for row in profiles]
    for name, payload in [
        ("escaped-surrogate", {"text": "\ud800"}),
        ("escaped-surrogate-key", {"\udfff": "value"}),
        ("capped-surrogate", {"text": "x"*21000+"\ud800"}),
        ("unicode-and-html", {"n": float("nan"), "values": [float("inf"), -float("inf"), 1, 1.0],
                              "历史": "<script>& 🎈", "null": None}),
    ]:
        document = copy.deepcopy(cases[0][1])
        event = document["events"][0]
        event["payload"] = payload
        records = [json.loads(raw) for raw in cases[0][2]]
        records[1]["payload"] = payload
        cases.append((name, document, [json.dumps(record) for record in records]))

    rows = []
    with patch.dict(os.environ, {"MINILOOP_API_TOKENS": "alice:token-a,bob:token-b"}), \
         patch.object(trace_view.time, "strftime", return_value="2000-01-02 03:04:05"):
        for name, document, records in cases:
            ledger = trace_view.build_ledger(document)
            page = trace_view.render_html([ledger], title="mini-loop trace · fixture")
            page_hash, page_error = None, None
            try:
                page_hash = hashlib.sha256(page.encode("utf-8")).hexdigest()
            except UnicodeEncodeError as error:
                page_error = type(error).__name__
            with tempfile.TemporaryDirectory(prefix="mini-loop-workflow-view-") as scratch:
                trace = "traj_"+"c"*24
                root = Path(scratch)/"traces"
                store = TrajectoryStore(root)
                path = root/(trace+".jsonl")
                raw = "\n".join(records)+"\n"
                path.write_text(raw, encoding="utf-8")
                file_ledger = trace_view.build_ledger(trace_view.assemble_file(path))
                file_page = trace_view.render_html([file_ledger], title="mini-loop trace · fixture")
                output = Path(scratch)/"page.html"
                cli_error = None
                with contextlib.redirect_stdout(io.StringIO()):
                    try:
                        cli_exit = trace_view.main([str(path), "--output", str(output)])
                    except UnicodeEncodeError as error:
                        cli_error, cli_exit = type(error).__name__, 1
                cfg = Settings(fake_llm=True, workspace_root=Path(scratch)/"ws", trajectory_enabled=False, ast_outline_enabled=False)
                manager = SessionManager(cfg, FakeAsyncAnthropic(), trajectory_store=store)
                with TestClient(create_app(settings=cfg, manager=manager), raise_server_exceptions=False) as http:
                    routes = []
                    for suffix, token in [("/view", "token-a"), ("/view", "token-b"), ("", "token-a"),
                                          ("/export?format=json", "token-a"), ("/export?format=jsonl", "token-a")]:
                        response = http.get(f"/trajectories/{trace}{suffix}", headers={"Authorization": f"Bearer {token}"})
                        routes.append({"suffix": suffix, "token": token, "status": response.status_code,
                                       "body_sha256": hashlib.sha256(response.content).hexdigest()
                                       if suffix == "/view" and response.status_code == 200 else None})
                if file_page != page:
                    raise AssertionError("file ledger differs")
                rows.append({"name": name, "document_wire": json.dumps(document), "raw": raw,
                             "contents_wire": json.dumps([row.get("content", "") for row in ledger["rows"]]),
                             "page_sha256": page_hash, "page_error": page_error,
                             "cli_exit": cli_exit, "cli_error": cli_error,
                             "cli_empty": output.stat().st_size == 0, "cli_mode": output.stat().st_mode & 0o777,
                             "http": routes})
    return {"rows": rows}
