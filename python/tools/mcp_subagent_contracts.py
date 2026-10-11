"""Real task children: default exclusion and explicit MCP connector rebinding."""
import asyncio
import tempfile
from pathlib import Path


def mcp_subagent_contracts() -> dict:
    from mini_loop.agent import Agent
    from mini_loop.builtins import default_registry
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool, system_text
    from mini_loop.mcp import MCPClient, install_mcp
    from mini_loop.permissions import PermissionHook
    from mini_loop.registry import Hook, Hooks

    async def scenario(custom, preconnected, role):
        created, listed, called, child_results, child_names = [], [], [], [], []
        fresh_states = []
        class Peer(MCPClient):
            name = "raw.name"
            def __init__(self):
                self.identity = len(created)
                created.append(self)
            async def list_tools(self):
                listed.append(self.identity)
                return [dict(name="echo", description="echo", input_schema={"type": "object"})]
            async def call_tool(self, name, args):
                called.append(self.identity)
                return f"client{self.identity}"
        class Policy:
            def select(self, role, parent):
                return parent.subset(name for name in parent.names()
                    if name == "connect_mcp" or name.startswith("mcp__"))
        class Capture(Hook):
            async def before_tool(self, ctx, call):
                if ctx.agent.depth and call.name == "connect_mcp":
                    fresh_states.append(dict(ctx.state.get("mcp_server_names", {})))
            async def on_result(self, ctx, call, output, **kwargs):
                if ctx.agent.depth:
                    child_results.append(output)
        parent_requests, child_requests = 0, 0
        def responder(request):
            nonlocal parent_requests, child_requests
            if " subagent in " in system_text(request):
                child_requests += 1
                child_names.append([entry["name"] for entry in request["tools"]
                    if entry["name"] == "connect_mcp" or entry["name"].startswith("mcp__")])
                if custom and child_requests == 1:
                    blocks = []
                    if preconnected and role != "Explore":
                        blocks.append(tool("mcp__raw_name__echo", _id="borrowed"))
                    blocks += [tool("connect_mcp", _id="child-connect", name="friendly"),
                               tool("mcp__raw_name__echo", _id="child-echo")]
                    return blocks, "tool_use"
                return [text("child done")], "end_turn"
            parent_requests += 1
            if parent_requests == 1:
                blocks = [tool("connect_mcp", _id="parent-connect", name="friendly")] if preconnected else []
                blocks.append(tool("task", _id="task", prompt="delegated", agent_type=role))
                if preconnected:
                    blocks.append(tool("mcp__raw_name__echo", _id="parent-echo"))
                return blocks, "tool_use"
            return [text("parent done")], "end_turn"
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            parent = Agent(client=FakeAsyncAnthropic(responder=responder, thinking=False),
                settings=Settings(fake_llm=True, skills_dir=root / "skills", workspace_root=root,
                                  subagent_max_rounds=3), workspace=root,
                tools=install_mcp(default_registry(), {"friendly": Peer}),
                role_tool_policy=Policy() if custom else None,
                # Explicit permissive operator policy probes connector binding;
                # readonly remains monotonic. Production default rules are unchanged.
                hooks=Hooks([PermissionHook(rules=[]), Capture()]), state={"permission_mode": "auto"})
            output = await parent.run("delegate")
            parent_results = [block["content"] for message in parent.messages
                if isinstance(message.get("content"), list) for block in message["content"]
                if isinstance(block, dict) and block.get("type") == "tool_result"]
            return dict(custom=custom, preconnected=preconnected, role=role, output=output,
                created=len(created), listed=listed, called=called, child_names=child_names,
                child_results=child_results, fresh_states=fresh_states, parent_results=parent_results,
                parent_connected=parent.state.get("mcp_server_names", {}),
                parent_names=[name for name in parent.tools.names() if name == "connect_mcp" or name.startswith("mcp__")])
    async def collect():
        return [await scenario(*recipe) for recipe in [(False, True, "worker"),
            (True, False, "worker"), (True, True, "worker"), (True, True, "Explore")]]
    return dict(rows=asyncio.run(collect()))
