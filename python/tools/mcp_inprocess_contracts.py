"""Actual Source InProcessMCP dispatch, text conversion and duplicate definitions."""
import asyncio


def mcp_inprocess_contracts() -> dict:
    from mini_loop.mcp import InProcessMCP
    definitions = [dict(name="value", description="value", input_schema={"type": "object"}),
                   dict(name="async", description="async", input_schema={"type": "object"}),
                   dict(name="error", description="error", input_schema={"type": "object"}),
                   dict(name="duplicate", description="first", input_schema={"type": "object"}),
                   dict(name="duplicate", description="second", input_schema={"type": "object"}),
                   dict(name="nil", description="nil", input_schema={"type": "object"})]
    async def asynchronous(**args):
        return args
    def fail(**args):
        raise ValueError("bad 中文")
    handlers = [lambda **args: args["value"], asynchronous, fail,
                lambda **args: "first", lambda **args: "second", None]
    client = InProcessMCP("raw.name", [dict(definition, handler=handler)
                        for definition, handler in zip(definitions, handlers)])
    recipes = [dict(name="value", arguments=dict(value=value)) for value in
               [None, True, 123, 1.5, "中文", [True, None, "中文"], {"a": 1, "b": False}]]
    recipes += [dict(name="async", arguments={"value": "中文"}), dict(name="error", arguments={}),
                dict(name="duplicate", arguments={}), dict(name="nil", arguments={}),
                dict(name="missing", arguments={})]
    async def scenario():
        rows = [dict(recipe=recipe, output=await client.call_tool(recipe["name"], recipe["arguments"]))
                for recipe in recipes]
        await client.close()
        return dict(name=client.name, definitions=await client.list_tools(), rows=rows,
                    after_close=await client.call_tool("duplicate", {}))
    return asyncio.run(scenario())
