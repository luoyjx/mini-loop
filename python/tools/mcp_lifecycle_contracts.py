"""Actual manager MCP holders, fork isolation and shutdown close contracts."""
import asyncio
import tempfile
from pathlib import Path


def mcp_lifecycle_contracts() -> dict:
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.manager import SessionManager
    from mini_loop.mcp import MCPClient, install_mcp
    from mini_loop.registry import ToolRegistry

    async def scenario(factory, fork, stop_only):
        closed, created, steps = [], [], []
        class Peer(MCPClient):
            name = "shared"
            def __init__(self):
                self.identity = len(created)
                created.append(self)
            async def list_tools(self):
                return [dict(name="echo", description="echo", input_schema={"type": "object"})]
            async def call_tool(self, name, args):
                return "echo"
            async def close(self):
                closed.append(self.identity)
        shared = None if factory else Peer()
        servers = {"friendly": Peer if factory else shared}
        count = 0
        def responder(request):
            nonlocal count
            count += 1
            if count % 2:
                return [tool("connect_mcp", _id=f"connect{count}", name="friendly")], "tool_use"
            return [text("done")], "end_turn"
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manager = SessionManager(Settings(fake_llm=True, workspace_root=root, skills_dir=root / "skills"),
                FakeAsyncAnthropic(responder=responder, thinking=False),
                tool_registry=install_mcp(ToolRegistry(), servers), mcp_servers=servers)
            first = manager.create(owner="owner", permission_mode="auto")
            await first.run("connect")
            second = await manager.fork_session(first.id) if fork else manager.create(owner="owner", permission_mode="auto")
            initial = dict(names=list(second.agent.tools.names()), connected=dict(second.agent.state.get("mcp_server_names", {})))
            await second.run("connect")
            steps.append(dict(point="connected", closed=sorted(closed)))
            if not stop_only:
                manager.delete(first.id)
                if manager._cleanup_tasks:
                    await asyncio.gather(*tuple(manager._cleanup_tasks))
                steps.append(dict(point="first_deleted", closed=sorted(closed)))
                manager.delete(second.id)
                if manager._cleanup_tasks:
                    await asyncio.gather(*tuple(manager._cleanup_tasks))
                steps.append(dict(point="second_deleted", closed=sorted(closed)))
            await manager.stop()
            await manager.stop()
            steps.append(dict(point="stopped", closed=sorted(closed)))
            return dict(factory=factory, fork=fork, stop_only=stop_only, created=len(created), second_initial=initial, steps=steps)
    async def collect():
        return [await scenario(*recipe) for recipe in [(False, False, False), (False, False, True),
            (True, False, False), (True, False, True), (False, True, False), (True, True, True)]]
    return dict(rows=asyncio.run(collect()))
