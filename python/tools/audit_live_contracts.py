"""Capture full Source deployment audit from actual manager components and logs."""

import asyncio
import os
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from mini_loop.audit import audit, audit_settings, render
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.manager import SessionManager
from mini_loop.problems import ProblemLog
from mini_loop.registry import Tool, ToolRegistry
from mini_loop.sandbox import SeatbeltSandbox, UnavailableSandbox
from mini_loop.secrets import SecretRegistry, NullSecretRegistry
from mini_loop.storage import NullStateStore, SQLiteStateStore


def _ledger(log):
    summary = getattr(log, "summary", None)
    return dict(entries=list(log), summary=summary() if callable(summary) else None,
                total=log.total() if callable(getattr(log, "total", None)) else None,
                churning=log.churning() if callable(getattr(log, "churning", None)) else False)


def _observation(manager):
    catalog = manager.tool_registry
    tools = None if catalog is None else dict(tools=[dict(name=t.name,
        risk=t.risk or "unclassified", parallel_safe=t.parallel_safe)
        for t in (catalog.get(n) for n in catalog.names()) if t is not None])
    sandbox = manager.sandbox
    secrets = manager.secrets
    mcp = ProblemLog()
    for session in manager._sessions.values():
        log = session.agent.state.get("mcp_problems", [])
        for message in log:
            for _ in range(getattr(log, "counts", {}).get(message, 1)):
                mcp.append(message)
    channels = []
    for name in sorted(dir(manager)):
        if name.startswith("_") or name in {"cron", "skills"}:
            continue
        log = getattr(getattr(manager, name, None), "problems", None)
        if log and isinstance(log, (list, tuple)):
            channels.append(dict(name=name, ledger=_ledger(log)))
    return dict(catalog=tools,
        sandbox_null=sandbox is None or type(sandbox).__name__ == "NullSandbox",
        sandbox_reason=getattr(sandbox, "reason", None),
        secrets_null=secrets is None or type(secrets).__name__ == "NullSecretRegistry",
        secret_names=list(getattr(secrets, "names", lambda: ())()),
        unresolved=list(getattr(secrets, "unresolved", lambda: ())()),
        short_values=list(getattr(secrets, "short_values", lambda: ())()),
        persistence_error=manager.persistence_error(),
        mcp=_ledger(mcp), cron=_ledger(manager.cron.problems),
        skills=_ledger(manager.skills.problems), channels=channels,
        state_null=type(manager.state_store).__name__ == "NullStateStore",
        journal_memory=type(manager.actions).__name__ == "InMemoryActionJournal",
        workflows=manager.enable_workflows)


def audit_live_contracts(scratch: Path) -> dict:
    class BrokenStore(NullStateStore):
        def append_event(self, session_id, event):
            raise RuntimeError("fixture disk unavailable")

    async def handler(**_):
        return "fixture"

    names = ("default", "no-shell", "parallel-risk", "exposed", "masked",
             "partial", "case-sensitive", "cached-secret-faults", "unavailable",
             "empty-reason", "unavailable-no-shell", "hardened", "broken-store",
             "workflows", "problem-ledgers", "plain-problems")
    rows = []
    for name in names:
        root = scratch / name
        env = {}
        if name in {"exposed", "masked", "partial", "case-sensitive"}:
            env = {"Z_TOKEN": "fixture-long-z", "a_api_key": "fixture-long-a",
                   "B_SECRET": "fixture-long-b", "C_PASSWORD": "fixture-long-c",
                   "EMPTY_SECRET": "", "ORDINARY": "fixture-long-value"}
        if name == "hardened":
            env = {"MINILOOP_API_TOKEN": "fixture-auth"}
        with patch.dict(os.environ, env, clear=True):
            cfg = Settings(fake_llm=True, workspace_root=root / "workspaces",
                           skills_dir=root / "skills", trajectory_enabled=False)
            options = {}
            if name in {"no-shell", "unavailable-no-shell"}:
                options["tool_registry"] = ToolRegistry()
            elif name == "parallel-risk":
                registry = ToolRegistry()
                for n, risk, readonly in [("bash", "exec", True), ("writer", "write", True),
                                         ("reader", "read", False), ("unknown", None, True),
                                         ("external", "external", True)]:
                    registry.register(Tool(n, "fixture", {}, handler, risk=risk,
                                           readonly=readonly, parallel_safe=True))
                options["tool_registry"] = registry
            if name == "exposed":
                options["secrets"] = NullSecretRegistry()
            elif name in {"masked", "hardened"}:
                options["secrets"] = SecretRegistry.from_environ(environ=env)
            elif name in {"partial", "case-sensitive", "cached-secret-faults"}:
                registry = SecretRegistry()
                if name == "partial":
                    registry.register("B_SECRET", "fixture-long-b")
                elif name == "case-sensitive":
                    registry.register("A_API_KEY", "fixture-long-a")
                else:
                    registry.register("FAILED_TOKEN", lambda: None)
                    registry.register("SHORT_TOKEN", "tiny")
                    registry.mask("fixture")
                options["secrets"] = registry
            if name in {"unavailable", "unavailable-no-shell", "empty-reason"}:
                options["sandbox"] = UnavailableSandbox("" if name == "empty-reason" else "fixture unavailable")
            if name == "hardened":
                options["sandbox"] = SeatbeltSandbox(writable_roots=[cfg.workspace_root])
                options["state_store"] = SQLiteStateStore(root / "state.db")
            elif name == "broken-store":
                options["state_store"] = BrokenStore()
            elif name == "workflows":
                options["enable_workflows"] = True
            manager = SessionManager(cfg, FakeAsyncAnthropic(), **options)
            try:
                if name == "broken-store":
                    session = manager.create()
                    asyncio.run(session.run("hello"))
                if name == "problem-ledgers":
                    for _ in range(2):
                        session = manager.create()
                        log = ProblemLog()
                        log.extend(["MCP collision", "MCP collision", "MCP truncated"])
                        session.agent.state["mcp_problems"] = log
                    manager.cron.problems.extend(["cron lost", "cron lost", "cron refused"])
                    manager.skills.problems.extend(["skill shadow", "skill shadow"])
                    manager.actions.problems.append("unknown action")
                    manager.memory.problems.append("memory refused")
                    log = ProblemLog(limit=1)
                    log.extend(["one", "two", "one", "two"])
                    manager.telemetry = SimpleNamespace(problems=log)
                if name == "plain-problems":
                    manager.telemetry = SimpleNamespace(problems=["first", "second"])
                normalize = lambda text: text.replace(str(scratch), "ROOT")
                configuration = [dict(severity=f.severity, check=f.check,
                    detail=normalize(f.detail), remedy=normalize(f.remedy))
                    for f in audit_settings(cfg, environ=env)]
                findings = audit(manager, environ=env)
                rows.append(dict(name=name, environment=env, configuration=configuration,
                    observation=_observation(manager), findings=[dict(severity=f.severity,
                    check=f.check, detail=normalize(f.detail), remedy=normalize(f.remedy)) for f in findings],
                    rendered=normalize(render(findings))))
            finally:
                asyncio.run(manager.stop())
                store = options.get("state_store")
                if isinstance(store, SQLiteStateStore):
                    store.close()
    return dict(cases=rows)
