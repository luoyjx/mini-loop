"""Real Source Agent batches connecting and invoking newly discovered MCP tools."""
from __future__ import annotations

import asyncio
import json
import tempfile
from pathlib import Path


def mcp_connect_contracts() -> dict:
    from mini_loop.agent import Agent
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.mcp import MCPClient, install_mcp
    from mini_loop.registry import ToolRegistry

    recipes = [
        dict(servers=[dict(alias="friendly", name="raw.name", factory=True, withheld=["A", "B", "C", "D"]),
                      dict(alias="second", name="raw.name", factory=False, withheld=[])],
             operations=[dict(connect="missing"), dict(connect="friendly"),
                         dict(call="mcp__raw_name__echo", arguments={"value": "中文"}),
                         dict(connect="friendly"), dict(connect="second"),
                         dict(call="mcp__raw_name__echo", arguments={"value": "new"})]),
        dict(servers=[dict(alias="first", name="my.server", factory=False, withheld=[]),
                      dict(alias="collision", name="my_server", factory=True, withheld=[])],
             operations=[dict(connect="first"), dict(connect="collision"), dict(connect="collision"),
                         dict(call="mcp__my_server__echo", arguments={"value": "kept"})]),
        dict(servers=[], operations=[dict(connect="missing"), dict(connect="")]),
        dict(servers=[dict(alias="empty", name="empty", factory=True, withheld=[], no_tools=True)],
             operations=[dict(connect="empty"), dict(connect="empty")]),
        dict(servers=[dict(alias="", name="empty-alias", factory=False, withheld=[])],
             operations=[dict(connect=""), dict(call="mcp__empty-alias__echo", arguments={})]),
    ]

    async def scenario(recipe):
        factory_calls = []
        tool_calls = []
        list_calls = []
        class Peer(MCPClient):
            def __init__(self, spec):
                self.name = spec["name"]
                self.spec = spec
                self.withheld = tuple(spec["withheld"])
            async def list_tools(self):
                list_calls.append(self.spec["alias"])
                return [] if self.spec.get("no_tools") else [dict(name="echo", description="echo",
                        input_schema={"type": "object", "properties": {}}, annotations={"readOnlyHint": True})]
            async def call_tool(self, name, args):
                tool_calls.append(dict(alias=self.spec["alias"], name=name, arguments=args))
                return json.dumps(dict(alias=self.spec["alias"], name=name, arguments=args),
                                  ensure_ascii=False, sort_keys=True)
            async def close(self):
                pass
        servers = {}
        for spec in recipe["servers"]:
            if spec["factory"]:
                def factory(spec=spec):
                    factory_calls.append(spec["alias"])
                    return Peer(spec)
                servers[spec["alias"]] = factory
            else:
                servers[spec["alias"]] = Peer(spec)
        registry = install_mcp(ToolRegistry(), servers)
        schemas = []
        def responder(request):
            schemas.append([entry["name"] for entry in request["tools"]])
            if len(schemas) > 1:
                return [text("done")], "end_turn"
            blocks = []
            for index, operation in enumerate(recipe["operations"]):
                if "connect" in operation:
                    blocks.append(tool("connect_mcp", _id=f"call{index}", name=operation["connect"]))
                else:
                    blocks.append(tool(operation["call"], _id=f"call{index}", **operation["arguments"]))
            return blocks, "tool_use"
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            agent = Agent(client=FakeAsyncAnthropic(responder=responder, thinking=False),
                          settings=Settings(fake_llm=True, skills_dir=root / "skills", workspace_root=root),
                          workspace=root, tools=registry, state={"permission_mode": "auto"})
            output = await agent.run("connect")
            results = [block["content"] for message in agent.messages
                       if isinstance(message.get("content"), list) for block in message["content"]
                       if isinstance(block, dict) and block.get("type") == "tool_result"]
            return dict(recipe=recipe, output=output, results=results, request_names=schemas,
                        factory_calls=factory_calls, list_calls=list_calls, tool_calls=tool_calls,
                        connected=agent.state.get("mcp_server_names", {}),
                        problems=list(agent.state.get("mcp_problems", [])),
                        connect_schema=registry.get("connect_mcp").schema)

    async def collect():
        return [await scenario(recipe) for recipe in recipes]
    return dict(rows=asyncio.run(collect()))
