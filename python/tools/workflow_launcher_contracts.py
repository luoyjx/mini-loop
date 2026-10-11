"""Exercise the default Source FastAPI lifespan, without an injected manager."""
from __future__ import annotations

import os
import tempfile
from pathlib import Path


def workflow_launcher_contracts() -> dict:
    from fastapi.testclient import TestClient
    from mini_loop.config import Settings
    from mini_loop.server import create_app

    names = ("default", "enabled-open", "enabled-auth", "disabled-auth", "caps-open", "caps-auth")
    keys = ("MINILOOP_API_TOKEN", "MINILOOP_API_TOKENS", "MINILOOP_EXPERIMENTAL_WORKFLOWS",
            "MINILOOP_WORKFLOW_MAX_CONCURRENT_AGENTS", "MINILOOP_WORKFLOW_MAX_AGENTS",
            "MINILOOP_WORKFLOW_MAX_ROUNDS", "MINILOOP_WORKFLOW_WALL_TIME_SECONDS")
    saved = {key: os.environ.get(key) for key in keys}
    rows = []
    try:
        for name in names:
            env = {key: str(value) for key, value in zip(keys[3:], (4, 32, 4, 900.0))}
            if name != "default":
                env["MINILOOP_EXPERIMENTAL_WORKFLOWS"] = "0" if name == "disabled-auth" else "1"
            if name.startswith("caps-"):
                env.update({key: str(value) for key, value in zip(keys[3:], (1, 2, 2, 3.5))})
            authenticated = name.endswith("auth")
            for key in keys:
                os.environ.pop(key, None)
            os.environ.update(env)
            if authenticated:
                os.environ["MINILOOP_API_TOKENS"] = "alice:token-a,bob:token-b"
            with tempfile.TemporaryDirectory(prefix="mini-loop-workflow-launcher-") as scratch:
                settings = Settings(fake_llm=True, trajectory_enabled=False, ast_outline_enabled=False,
                                    enable_features=False, guardian_enabled=False, spill_dir=None,
                                    workspace_root=Path(scratch), skills_dir=Path(scratch) / "empty-skills")
                app = create_app(settings=settings)
                with TestClient(app) as http:
                    manager = app.state.manager
                    headers = {"Authorization": "Bearer token-a"} if authenticated else {}
                    parent_id = http.post("/sessions", json={}, headers=headers).json()["id"]
                    parent = manager.get(parent_id)
                    service = manager.workflows
                    rows.append(dict(
                        name=name, env=env, authenticated=authenticated,
                        enabled=service is not None,
                        health=http.get("/healthz").json()["experimental_workflows"],
                        listing=http.get(f"/sessions/{parent_id}/workflows", headers=headers).json(),
                        tools=sorted(tool for tool in parent.agent.tools.names() if tool.startswith("Workflow")),
                        pool=service.attempt_semaphore._value if service else 0,
                        caps=dict(max_concurrent_agents=settings.workflow_max_concurrent_agents,
                                  max_agents=settings.workflow_max_agents, max_rounds=settings.workflow_max_rounds,
                                  wall_time_seconds=settings.workflow_wall_time_seconds),
                        journal_shared=service.action_journal is manager.actions if service else False,
                    ))
    finally:
        for key, value in saved.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value
    return dict(rows=rows)
