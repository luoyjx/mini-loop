"""Actual FastAPI workflow routes with owned execution and stable action replay."""
from __future__ import annotations

import asyncio
import json
import os
import tempfile
from pathlib import Path


def workflow_http_contracts() -> dict:
    from fastapi.testclient import TestClient
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    import mini_loop.workflows.service as module

    saved = {key: os.environ.get(key) for key in ("MINILOOP_API_TOKENS", "MINILOOP_API_TOKEN")}
    original = module.FreshAgentRunner
    rows = []
    try:
        for mode in ("disabled", "anonymous", "authenticated"):
            os.environ.pop("MINILOOP_API_TOKEN", None)
            os.environ.pop("MINILOOP_API_TOKENS", None)
            if mode != "anonymous":
                os.environ["MINILOOP_API_TOKENS"] = "alice:token-a,bob:token-b"
            with tempfile.TemporaryDirectory(prefix="mini-loop-workflow-http-") as scratch:
                entered = asyncio.Event()
                class Worker:
                    def __init__(self, **kwargs):
                        pass
                    async def __call__(self, attempt, node, inputs):
                        entered.set()
                        await asyncio.Event().wait()
                module.FreshAgentRunner = Worker
                cfg = Settings(fake_llm=True, trajectory_enabled=False, ast_outline_enabled=False,
                               workspace_root=Path(scratch), skills_dir=Path(scratch) / "empty-skills")
                manager = SessionManager(cfg, FakeAsyncAnthropic(), enable_workflows=mode != "disabled")
                cases, ids = [], {}
                def normalize(value):
                    if isinstance(value, dict):
                        return {key: normalize(item) for key, item in value.items()}
                    if isinstance(value, list):
                        return [normalize(item) for item in value]
                    if isinstance(value, str):
                        for raw, symbol in ids.items():
                            value = value.replace(raw, symbol)
                    return value
                with TestClient(create_app(settings=cfg, manager=manager)) as http:
                    token = "" if mode == "anonymous" else "token-a"
                    headers = {"Authorization": "Bearer " + token} if token else {}
                    parent = http.post("/sessions", json={}, headers=headers).json()["id"]
                    other = http.post("/sessions", json={}, headers=headers).json()["id"]
                    ids[parent], ids[other] = "s", "other"
                    path = f"/sessions/{parent}/workflows"
                    definition = {"name": "wf", "return_from": "a", "nodes": [{"id": "a", "kind": "agent"}]}
                    body = {"definition": definition, "action_id": "action"}
                    def call(name, method, target=path, payload=None, auth=token, raw=None, worker=False):
                        text = raw if raw is not None else (json.dumps(payload, ensure_ascii=False) if payload is not None else "")
                        h = {"Content-Type": "application/json"}
                        if auth:
                            h["Authorization"] = "Bearer " + auth
                        response = http.request(method, target, content=text.encode(), headers=h)
                        result = response.json()
                        if response.status_code == 200 and isinstance(result, dict) and "run_id" in result:
                            ids.setdefault(result["run_id"], f"run-{sum(v.startswith('run-') for v in ids.values()) + 1}")
                        if worker:
                            http.portal.call(entered.wait)
                        cases.append(dict(name=name, method=method, path=normalize(target), body=text,
                                          token=auth, status=response.status_code, response=normalize(result), await_worker=worker))
                        return result
                    call("empty-list", "GET")
                    call("missing-body", "POST")
                    call("missing-definition", "POST", payload={})
                    call("multiple-model-errors", "POST", payload={"definition": None, "args": [], "action_id": 7})
                    call("wrong-body", "POST", payload=[])
                    call("null-body", "POST", raw="null")
                    call("validation-before-owner", "POST", target="/sessions/missing/workflows", payload={})
                    if token:
                        call("auth-before-validation", "POST", payload={}, auth="wrong")
                        call("foreign-parent", "POST", payload=body, auth="token-b")
                    if mode != "authenticated":
                        call("launch-refused", "POST", payload=body)
                        call("legacy-ignored-extra", "POST", raw=json.dumps(body)[:-1] + ', "extra": NaN}')
                        rows.append(dict(mode=mode, cases=cases, context=None))
                        continue
                    call("invalid-definition", "POST", payload={"definition": {}, "action_id": "bad"})
                    call("missing-name", "POST", payload={"definition": {"return_from": "a"}, "action_id": "bad"})
                    call("missing-return", "POST", payload={"definition": {"name": "wf"}, "action_id": "bad"})
                    call("unknown-definition-field", "POST", payload={"definition": {**definition, "unrecognized": True}, "action_id": "bad"})
                    launched = call("launch", "POST", payload=body, worker=True)
                    call("same-action-replay", "POST", payload=body)
                    call("ignored-nonfinite-extra-replay", "POST", raw=json.dumps(body)[:-1] + ', "extra": NaN}')
                    call("ignored-surrogate-extra-replay", "POST", raw=json.dumps(body)[:-1] + ', "extra": "\\ud800"}')
                    call("changed-action-payload", "POST", payload={**body, "args": {"changed": True}})
                    call("different-parent-action", "POST", target=f"/sessions/{other}/workflows", payload=body)
                    call("owned-list", "GET")
                    run_path = path + "/" + launched["run_id"]
                    call("foreign-run", "GET", target=f"/sessions/{other}/workflows/{launched['run_id']}")
                    call("cancel", "POST", target=run_path + "/cancel", payload={"reason": "operator stop"})
                    call("terminal-replay", "POST", payload=body)
                    call("terminal-list", "GET")
                    context = manager.workflows.store.get_run(launched["run_id"]).run_context.as_dict()
                    rows.append(dict(mode=mode, cases=cases, context=context))
    finally:
        module.FreshAgentRunner = original
        for key, value in saved.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value
    return dict(rows=rows)
