"""Source MCP registration ownership, schema and handler timeout contracts."""
from __future__ import annotations

import asyncio
import json
from types import SimpleNamespace


def mcp_registration_contracts() -> dict:
    from mini_loop.mcp import MCPClient, normalize_name, register_mcp
    from mini_loop.registry import ToolRegistry

    schema = {"type": "object", "properties": {}, "$defs": {"value": {"enum": [1, None]}},
              "additionalProperties": {"$ref": "#/$defs/value"}}

    def tool(name, description="d", hint=False, input_schema=schema):
        return dict(name=name, description=description, annotations={"readOnlyHint": hint},
                    input_schema=input_schema)

    recipes = [
        [dict(server="alpha__beta", tools=[tool("gamma")]),
         dict(server="alpha", tools=[tool("beta__gamma")])],
        [dict(server="my.server", tools=[tool("read", "first", True)]),
         dict(server="my_server", tools=[tool("read", "takeover", True)])],
        [dict(server="same", tools=[tool("echo", "old")]),
         dict(server="same", tools=[tool("echo", "new", True)])],
        [dict(server="!!!", tools=[tool("___"), tool("-"), tool("中文😀")])],
        [dict(server="caps", tools=[tool("long", "😀中" * 2100), tool("exact", "x" * 4000)])],
        [dict(server="values", tools=[tool("none", None), tool("number", 7),
             tool("list", ["中文", 1, None], "false"), tool("schema-null", input_schema=None),
             tool("schema-bool", input_schema=False)])],
        [dict(server="duplicates", tools=[tool("a.b", "first"), tool("a_b", "last", True)])],
    ]

    class Peer(MCPClient):
        def __init__(self, step):
            self.name = step["server"]
            self.tools = step["tools"]
        async def list_tools(self):
            return self.tools
        async def call_tool(self, name, args):
            if name == "slow":
                await asyncio.sleep(1)
            return json.dumps(dict(original=name, arguments=args), ensure_ascii=False, sort_keys=True)
        async def close(self):
            pass

    async def collect():
        rows = []
        for steps in recipes:
            agent = SimpleNamespace(tools=ToolRegistry(), state={})
            publications = []
            for step in steps:
                added = await register_mcp(agent, Peer(step), timeout=0.01)
                definitions = []
                for name in agent.tools.names():
                    definition = agent.tools.get(name)
                    definitions.append(dict(name=name, description=definition.description,
                                            schema=definition.input_schema, risk=definition.risk,
                                            readonly=definition.readonly, parallel_safe=definition.parallel_safe))
                publications.append(dict(added=added, definitions=definitions,
                                         problems=list(agent.state.get("mcp_problems", [])),
                                         owners=dict(agent.state["mcp_tool_owner"])))
            rows.append(dict(steps=steps, publications=publications))
        agent = SimpleNamespace(tools=ToolRegistry(), state={})
        await register_mcp(agent, Peer(dict(server="calls", tools=[tool("original name"), tool("slow")])), timeout=0.01)
        outputs = []
        for name in agent.tools.names():
            outputs.append(await agent.tools.get(name).handler(None, value="中文", nested={"x": [1, True, None]}))
        return dict(rows=rows, outputs=outputs,
                    normalizations=[dict(input=name, output=normalize_name(name))
                                    for name in ["", "___", "!!!", "-", "alpha__beta", "中a😀-b._c", "_x_"]])

    return asyncio.run(collect())
