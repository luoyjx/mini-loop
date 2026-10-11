"""Source HTTP composition with explicitly selected MCP and operator servers."""
import tempfile
from pathlib import Path


def mcp_launcher_contracts() -> dict:
    from fastapi.testclient import TestClient
    from mini_loop.builtins import full_registry
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.manager import SessionManager
    from mini_loop.mcp import MCPClient
    from mini_loop.server import create_app

    rows = []
    for enabled, configured in [(False, True), (True, False), (True, True)]:
        names, results = [], []
        counters = dict(listed=0, called=0, closed=0)
        class Peer(MCPClient):
            name = "raw.name"
            async def list_tools(self):
                counters["listed"] += 1
                return [dict(name="echo", description="echo", input_schema={"type": "object"})]
            async def call_tool(self, name, args):
                counters["called"] += 1
                return "echo"
            async def close(self):
                counters["closed"] += 1
        peer = Peer()
        def responder(request):
            names.append([entry["name"] for entry in request["tools"]
                          if entry["name"] == "connect_mcp" or entry["name"].startswith("mcp__")])
            if enabled and len(names) == 1:
                blocks = [tool("connect_mcp", _id="connect", name="friendly")]
                if configured:
                    blocks.append(tool("mcp__raw_name__echo", _id="echo"))
                return blocks, "tool_use"
            results.extend(block["content"] for message in request["messages"]
                           if isinstance(message.get("content"), list) for block in message["content"]
                           if isinstance(block, dict) and block.get("type") == "tool_result")
            return [text("done")], "end_turn"
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            settings = Settings(fake_llm=True, workspace_root=root, skills_dir=root / "skills", trajectory_enabled=False)
            servers = {"friendly": peer} if configured else {}
            # Individual Source full_registry toggles give the same default-ten
            # plus MCP boundary that the native launcher selects independently.
            registry = full_registry(tasks=False, background=False, memory=False, cron=False,
                plan=False, goals=False, diagnostics=False, session_query=False, teams=False,
                worktrees=False, mcp=enabled, mcp_servers=servers, self_audit=False)
            manager = None
            def manager_factory(cfg):
                nonlocal manager
                manager = SessionManager(cfg, FakeAsyncAnthropic(responder=responder, thinking=False),
                    tool_registry=registry, mcp_servers=servers)
                return manager
            with TestClient(create_app(settings=settings, manager_factory=manager_factory)) as http:
                created = http.post("/sessions", json={"mode": "auto"})
                assert created.status_code == 200, created.text
                session_id = created.json()["id"]
                response = http.post(f"/sessions/{session_id}/messages", json={"message": "connect"})
                assert response.status_code == 200, response.text
                output = response.json()["final"]
            rows.append(dict(enabled=enabled, configured=configured, output=output,
                             request_mcp_names=names, results=results, counters=counters))
    return dict(rows=rows)
