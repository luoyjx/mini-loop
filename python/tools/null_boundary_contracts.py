"""Actual omitted/None secret and state defaults at three public constructors."""
from __future__ import annotations

import argparse
import asyncio
import hashlib
import json
import sqlite3
import tempfile
from pathlib import Path
from unittest.mock import patch


def null_boundary_contracts() -> dict:
    from mini_loop.agent import Agent
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text
    from mini_loop.manager import SessionManager
    from mini_loop.session import AgentSession

    sample = "visible fixture-secret-7"
    payload = {"token": "fixture-secret-7", "nested": ["visible fixture-secret-7"]}
    sqlite_calls = 0

    def refuse_sqlite(*args, **kwargs):
        nonlocal sqlite_calls
        sqlite_calls += 1
        raise AssertionError("default Null construction attempted SQLite")

    def sqlite_files(root):
        matches = []
        for path in root.rglob("*"):
            if path.is_file():
                with path.open("rb") as stream:
                    if stream.read(16) == b"SQLite format 3\x00":
                        matches.append(str(path.relative_to(root)))
        return sorted(matches)

    async def capture(root):
        rows = []
        for layer in ("agent", "session", "manager"):
            for construction in ("omitted", "none"):
                workspace = root / f"{layer}-{construction}"
                workspace.mkdir()
                settings = Settings(fake_llm=True, model="fixture-model", max_tokens=64,
                                    max_turns=1, workspace_root=workspace, skills_dir=workspace / "empty-skills",
                                    spill_dir=None, trajectory_enabled=False, ast_outline_enabled=False)
                client = FakeAsyncAnthropic(responder=lambda request: ([text(sample)], "end_turn"), thinking=False, delay=0)
                manager = session = store = None
                options = {} if construction == "omitted" else {"secrets": None}
                if layer == "manager":
                    if construction == "none":
                        options["state_store"] = None
                    manager = SessionManager(settings, client, **options)
                    session = manager.create(owner="fixture-owner")
                    agent = session.agent
                    store = manager.state_store
                else:
                    agent = Agent(client=client, settings=settings, workspace=workspace, **options)
                    if layer == "session":
                        state_options = {} if construction == "omitted" else {"state_store": None}
                        session = AgentSession("fixture-session", workspace, **state_options)
                        session.agent = agent
                        store = session.state_store
                output = await (session or agent).run("show the fixture text")
                live_count = len(agent.messages)
                status = session.status if session else None
                busy = session.busy if session else None
                runs = session.run_count if session else None
                rows.append(dict(
                    layer=layer, construction=construction, secret_class=type(agent.secrets).__name__,
                    state_class=type(store).__name__ if store is not None else None,
                    mask_text=agent.secrets.mask(sample), mask_payload=agent.secrets.mask_payload(payload),
                    output=output, model_calls=client.calls, live_messages=live_count,
                    session_status=status, busy=busy, run_count=runs,
                    stored_sessions=len(store.load_sessions()) if store is not None else None,
                    stored_messages=store.message_count(session.id) if store is not None else None,
                    stored_events=len(store.load_events(session.id)) if store is not None else None,
                    persist_error=session.persist_error if session else None,
                ))
                if manager is not None:
                    await manager.stop()
                elif session is not None:
                    session.stop_accepting("fixture stopped")
                    await session.cancel("fixture stopped")
                    store.close()
                rows[-1]["sqlite_files_after_stop"] = sqlite_files(workspace)
                rows[-1]["sqlite_connect_calls"] = sqlite_calls
        return rows

    with tempfile.TemporaryDirectory(prefix="mini-loop-null-boundaries-") as temporary, patch.object(sqlite3, "connect", refuse_sqlite):
        rows = asyncio.run(capture(Path(temporary)))
    python_root = Path(__file__).resolve().parents[1]
    names = ("mini_loop/agent.py", "mini_loop/session.py", "mini_loop/manager.py", "mini_loop/secrets.py", "mini_loop/storage.py", "tools/null_boundary_contracts.py")
    return dict(rows=rows, sample=sample, payload=payload,
                source_sha256={name: hashlib.sha256((python_root / name).read_bytes()).hexdigest() for name in names},
                scope=["Six actual public omitted/explicit-None constructors followed by an offline run; session and manager rows also stop their live owner.",
                       "Default Null secret masking and Null state readback are actual observations, not assumptions about named Go classes.",
                       "sqlite3.connect is a rejecting tripwire; temporary roots are inspected for SQLite file headers after stop.",
                       "No explicit SecretRegistry, sandbox or SQLite backend equivalence is claimed; those have separate existing evidence."])


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    args.output.write_text(json.dumps(null_boundary_contracts(), ensure_ascii=False, sort_keys=True, indent=2) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
