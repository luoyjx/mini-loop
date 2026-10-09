"""Run Source's actual local audit entry point and observe constructor effects."""

import asyncio
import contextlib
import io
import os
from pathlib import Path
from unittest.mock import patch

from mini_loop.audit import main
from mini_loop.manager import SessionManager


def audit_local_cli_contracts(scratch: Path) -> dict:
    scratch = scratch.resolve()
    profiles = [
        ("default", {}, []),
        ("public", {"HOST": "0.0.0.0"}, []),
        ("authenticated", {"HOST": "0.0.0.0", "MINILOOP_API_TOKEN": "fixture-auth"}, []),
        ("exposed", {"Z_TOKEN": "fixture-long-z", "A_SECRET": "fixture-long-a"}, []),
        ("content", {"MINILOOP_TRAJECTORY_CAPTURE_CONTENT": "1"}, []),
        ("world-writable", {}, []),
        ("full-features", {"MINILOOP_FEATURES": "1"}, []),
        ("workflow-setting", {"MINILOOP_EXPERIMENTAL_WORKFLOWS": "1"}, []),
        ("unknown-args", {}, ["--unknown", "unused"]),
        ("ignored-port", {"PORT": "not-a-port"}, []),
        ("real-client", {"MINILOOP_FAKE_LLM": "", "ANTHROPIC_API_KEY": "fixture-provider-key"}, []),
        ("missing-key", {"MINILOOP_FAKE_LLM": ""}, []),
        ("invalid-settings", {"MINILOOP_MAX_TURNS": "0"}, []),
        ("invalid-auth", {"MINILOOP_API_TOKENS": "alice:shared,bob:shared"}, []),
    ]
    rows = []
    for name, overrides, args in profiles:
        root = scratch / name
        env = dict(MINILOOP_FAKE_LLM="1", MINILOOP_WORKSPACE_ROOT=str(root / "workspaces"),
                   MINILOOP_SKILLS_DIR=str(root / "skills"))
        env.update(overrides)
        if name == "world-writable":
            (root / "workspaces").mkdir(parents=True)
            (root / "workspaces").chmod(0o777)
        managers = []

        def construct(*a, **kw):
            manager = SessionManager(*a, **kw)
            managers.append(manager)
            return manager

        output = io.StringIO()
        code, fault = None, None
        try:
            with patch.dict(os.environ, env, clear=True), \
                    patch("mini_loop.manager.SessionManager", construct), \
                    contextlib.redirect_stdout(output):
                try:
                    code = main(args)
                except Exception as exc:
                    fault = dict(type=type(exc).__name__, message=str(exc).replace(str(scratch), "ROOT"))
            rows.append(dict(name=name, overrides=overrides, args=args, exit_code=code,
                             stdout=output.getvalue().replace(str(scratch), "ROOT"),
                             fault=fault, workspace_exists=(root / "workspaces").exists(),
                             managers=len(managers), sessions=sum(len(m._sessions) for m in managers),
                             workflows=[m.enable_workflows for m in managers],
                             workflow_setting=[m.settings.enable_workflows for m in managers]))
        finally:
            for manager in managers:
                asyncio.run(manager.stop())
                close = getattr(manager.client, "close", None)
                if close is not None:
                    asyncio.run(close())
    return dict(cases=rows)
