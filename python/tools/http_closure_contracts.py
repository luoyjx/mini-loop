"""Finite actual FastAPI cron diagnostics and owned workflow detail reads."""
from __future__ import annotations

import argparse
import asyncio
import hashlib
import json
import os
import tempfile
from pathlib import Path
from unittest.mock import patch


def http_closure_contracts() -> dict:
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    import mini_loop.workflows.service as workflow_module

    future = "0 0 31 2 *"
    valid = {"cron": future, "prompt": "future only"}
    recipes = [
        ("missing-body", ""), ("null-body", "null"), ("array-body", "[]"),
        ("missing-both", {}), ("missing-prompt", {"cron": future}),
        ("empty-cron", {**valid, "cron": ""}),
        ("long-cron", {**valid, "cron": "界" * 101}),
        ("empty-prompt", {**valid, "prompt": ""}),
        ("null-cron", {**valid, "cron": None}),
        ("numeric-prompt", {**valid, "prompt": 7}),
        ("null-recurring", {**valid, "recurring": None}),
        ("bad-text-durable", {**valid, "durable": "maybe"}),
        ("bad-integer-recurring", {**valid, "recurring": 2}),
        ("bad-fraction-durable", {**valid, "durable": 0.5}),
        ("array-recurring", {**valid, "recurring": []}),
        ("object-durable", {**valid, "durable": {}}),
        ("multiple-errors", {"cron": "", "prompt": 7, "recurring": None, "durable": "maybe"}),
        ("foreign-invalid", {**valid, "prompt": ""}),
        ("missing-session-invalid", {**valid, "prompt": ""}),
        ("bad-auth-invalid", {**valid, "prompt": ""}),
        ("valid-string-bools", {**valid, "recurring": "no", "durable": "yes"}),
        ("valid-numeric-bools", {**valid, "recurring": 0.0, "durable": 1}),
    ]
    entered = asyncio.Event()

    class Worker:
        def __init__(self, **kwargs):
            pass

        async def __call__(self, attempt, node, inputs):
            entered.set()
            await asyncio.Event().wait()

    environment = {key: value for key, value in os.environ.items()
                   if key not in {"MINILOOP_API_TOKEN", "MINILOOP_API_TOKENS"}}
    environment["MINILOOP_API_TOKENS"] = "alice:token-a,bob:token-b"
    with patch.dict(os.environ, environment, clear=True), patch.object(workflow_module, "FreshAgentRunner", Worker):
        with tempfile.TemporaryDirectory(prefix="mini-loop-http-closure-") as temporary:
            root = Path(temporary)
            settings = Settings(fake_llm=True, trajectory_enabled=False, ast_outline_enabled=False,
                                workspace_root=root, skills_dir=root / "empty-skills", spill_dir=None)
            model = FakeAsyncAnthropic()
            manager = SessionManager(settings, model, enable_workflows=True)
            app = create_app(settings=settings, manager=manager)
            rows, aliases = [], {}

            def normalize(value):
                if isinstance(value, str):
                    for actual, symbolic in aliases.items():
                        value = value.replace(actual, symbolic)
                    return value
                if isinstance(value, list):
                    return [normalize(item) for item in value]
                if isinstance(value, dict):
                    return {key: normalize(item) for key, item in value.items()}
                return value

            with TestClient(app) as http:
                app.state.auth = TokenAuth({"token-a": "alice", "token-b": "bob"})
                sid = http.post("/sessions", json={}, headers={"Authorization": "Bearer token-a"}).json()["id"]
                aliases[sid] = "session-fixture"
                base = f"/sessions/{sid}"

                def call(name, method, path, body="", token="token-a", group="cron"):
                    headers = {"Content-Type": "application/json", "Authorization": "Bearer " + token}
                    response = http.request(method, path, content=body.encode("utf-8"), headers=headers)
                    value = response.json()
                    for job_id in manager.cron.jobs:
                        aliases.setdefault(job_id, f"job-{sum(v.startswith('job-') for v in aliases.values()) + 1}")
                    if isinstance(value, dict):
                        for node in value.get("nodes", []):
                            for attempt_id in node["attempt_ids"]:
                                aliases.setdefault(attempt_id, f"attempt-{sum(v.startswith('attempt-') for v in aliases.values()) + 1}")
                    rows.append(dict(name=name, group=group, method=method, path=normalize(path),
                                     body=body, token=token, status=response.status_code,
                                     response=normalize(value),
                                     headers={key: response.headers[key] for key in
                                              ("content-type", "x-content-type-options", "x-frame-options", "referrer-policy")
                                              if key in response.headers},
                                     cron_jobs=len(manager.cron.jobs), model_calls=model.calls))
                    return value

                for name, body in recipes:
                    text = body if isinstance(body, str) else json.dumps(body, ensure_ascii=False)
                    token = "token-b" if name == "foreign-invalid" else "wrong" if name == "bad-auth-invalid" else "token-a"
                    path = "/sessions/missing/cron" if name == "missing-session-invalid" else base + "/cron"
                    call(name, "POST", path, text, token)

                definition = {"name": "wf", "return_from": "a", "nodes": [{"id": "a", "kind": "agent"}]}
                launch = http.post(base + "/workflows", json={"definition": definition, "action_id": "closure-action"},
                                   headers={"Authorization": "Bearer token-a"})
                launch.raise_for_status()
                run_id = launch.json()["run_id"]
                aliases[run_id] = "run-fixture"
                http.portal.call(entered.wait)
                path = base + "/workflows/" + run_id
                call("workflow-running-detail", "GET", path, group="workflow")
                cancelled = http.post(path + "/cancel", json={"reason": "operator stop"}, headers={"Authorization": "Bearer token-a"})
                cancelled.raise_for_status()
                call("workflow-cancelled-detail", "GET", path, group="workflow")
            sources = {"mini_loop/server.py", "mini_loop/manager.py", "mini_loop/workflows/service.py", "mini_loop/workflows/store.py"}
            python_root = Path(__file__).resolve().parents[1]
            sources.add("tools/http_closure_contracts.py")
            return dict(cases=rows, workflow_definition=definition, workflow_action_id="closure-action",
                        workflow_cancel_reason="operator stop",
                        source_sha256={name: hashlib.sha256((python_root / name).read_bytes()).hexdigest() for name in sorted(sources)},
                        scope=["Actual FastAPI requests; complete finite Pydantic diagnostic payloads and owned detail responses.",
                               "Only random session/job/run/attempt identifiers are normalized; no detail field is dropped.",
                               "Future cron expressions never execute; workflow worker is a controlled blocking public runner seam.",
                               "Teamless HTTP remains outside this fixture: the current native manager lacks a public no-team binding seam."])


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    args.output.write_text(json.dumps(http_closure_contracts(), ensure_ascii=False, sort_keys=True, indent=2) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
