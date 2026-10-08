"""Actual manager/model and HTTP observations for optional transcript search."""
from __future__ import annotations

import asyncio
import tempfile
from pathlib import Path


def transcript_search_runtime_contracts() -> dict:
    from fastapi.testclient import TestClient
    from mini_loop.builtins import full_registry
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    from mini_loop.storage import SQLiteStateStore

    def registry(enabled):
        return full_registry(tasks=False, background=False, memory=False, cron=False,
            plan=False, goals=False, diagnostics=False, session_query=enabled,
            teams=False, worktrees=False, mcp=False, self_audit=False)

    def responder_for(enabled, observations):
        def respond(request):
            observations["names"].append([entry["name"] for entry in request["tools"]])
            results = [block["content"] for message in request["messages"]
                if isinstance(message.get("content"), list) for block in message["content"]
                if isinstance(block, dict) and block.get("type") == "tool_result"
                and block.get("tool_use_id") == "search"]
            if results:
                observations["results"].extend(results)
                return [text("done")], "end_turn"
            last = request["messages"][-1].get("content")
            if enabled and last == "search history":
                return [tool("transcript_search", _id="search", query="needle")], "tool_use"
            return [text("done")], "end_turn"
        return respond

    http_rows = []
    for enabled in (False, True):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            settings = Settings(fake_llm=True, workspace_root=root,
                skills_dir=root / "skills", trajectory_enabled=False)
            observations = dict(names=[], results=[])
            def factory(cfg):
                return SessionManager(cfg, FakeAsyncAnthropic(
                    responder=responder_for(enabled, observations), thinking=False),
                    tool_registry=registry(enabled))
            with TestClient(create_app(settings=settings, manager_factory=factory)) as http:
                created = http.post("/sessions", json={"mode": "readonly"})
                assert created.status_code == 200, created.text
                response = http.post(f"/sessions/{created.json()['id']}/messages",
                    json={"message": "search history"})
                assert response.status_code == 200, response.text
                final = response.json()["final"]
            http_rows.append(dict(enabled=enabled, output=final, **observations))

    async def manager_recipe(rewrite):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            settings = Settings(fake_llm=True, workspace_root=root,
                skills_dir=root / "skills", trajectory_enabled=False)
            store = SQLiteStateStore(root / "state.db")
            observations = dict(names=[], results=[])
            manager = SessionManager(settings, FakeAsyncAnthropic(
                responder=responder_for(True, observations), thinking=False),
                tool_registry=registry(True), state_store=store)
            try:
                await manager.start()
                own = manager.create(owner="alice", permission_mode="readonly")
                foreign = manager.create(owner="bob", permission_mode="readonly")
                await foreign.run("foreign needle")
                if rewrite:
                    await own.run("own old needle")
                    # The actual Source sanctioned compaction rewrite path.
                    own.agent.messages[:] = [{"role": "user", "content": "summary"}]
                observations["names"].clear()
                final = await own.run("search history")
                assert len(observations["results"]) == 1
                assert "foreign needle" not in observations["results"][0]
                current = store.transcript_epoch(own.id)
                return dict(rewrite=rewrite, output=final, current_epoch=current, **observations)
            finally:
                await manager.stop()
                store.close()

    async def managers():
        return [await manager_recipe(rewrite) for rewrite in (False, True)]

    return dict(http=http_rows, managers=asyncio.run(managers()))
