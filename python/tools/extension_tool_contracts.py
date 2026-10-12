"""Actual registered custom tools through Source's ordinary execution gate."""

import asyncio
import copy
import json
import tempfile
from pathlib import Path


def extension_tool_contracts() -> dict:
    from mini_loop.agent import Agent
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.permissions import PermissionHook
    from mini_loop.registry import Hook, Hooks, Tool, ToolCall, ToolRegistry

    recipes = [
        dict(name="text", value={"name": "World"}),
        dict(name="unicode", value={"name": "用户 café"}),
        dict(name="default", value={}),
        dict(name="ignored-extra", value={"name": "World", "extra": {"x": [1]}}),
        dict(name="empty-text", value={"name": ""}),
        dict(name="null", value={"name": None}),
        dict(name="integer", value={"name": 7}),
        dict(name="array-value", value={"name": [1]}),
        dict(name="rewrite", value={"name": "old"}, rewrite=True),
        dict(name="before-deny", value={"name": "World"}, before_deny=True),
        dict(name="guard-deny", value={"name": "World"}, guard_deny=True),
        dict(name="readonly-read", value={"name": "World"}, mode="readonly"),
        dict(name="readonly-write", value={"name": "World"}, mode="readonly", risk="write"),
        dict(name="handler-error", value={"name": "boom"}),
    ]

    async def scenario(row, root):
        trace, results, received = [], [], []

        class Probe(Hook):
            async def before_tool(self, ctx, call):
                trace.append("before")
                if row.get("before_deny"):
                    return "before blocked"
                if row.get("rewrite"):
                    call.input["name"] = "rewritten"

            async def guard_tool(self, ctx, call):
                trace.append("guard")
                if row.get("guard_deny"):
                    return "guard blocked"

            async def after_tool(self, ctx, call, output):
                trace.append("after")
                return output + "|after"

            async def on_result(self, ctx, call, output, *, denied=False, failed=False):
                trace.append("result")
                results.append(dict(output=output, denied=denied, failed=failed))

        async def greet(ctx, name="default", **extra):
            trace.append("decode")
            if not isinstance(name, str):
                raise ValueError("name must be text")
            trace.append("handle")
            received.append(name)
            if name == "boom":
                raise RuntimeError("extension failed")
            return f"Hello, {name}!"

        registry = ToolRegistry()
        registry.register(Tool("greet", "Typed extension test", {"type": "object", "properties": {"name": {"type": "string"}}},
                               greet, risk=row.get("risk", "read")))
        agent = Agent(client=FakeAsyncAnthropic(), workspace=root, tools=registry,
                      hooks=Hooks([Probe(), PermissionHook()]), state={"permission_mode": row.get("mode", "auto")},
                      settings=Settings(fake_llm=True, spill_dir=None, skills_dir=root / "skills"))
        call = ToolCall(name="greet", id="custom", input=copy.deepcopy(row["value"]))
        output = await agent._exec_tool(call)
        return dict(name=row["name"], input_json=json.dumps(row["value"]),
                    rewrite=row.get("rewrite", False), before_deny=row.get("before_deny", False),
                    guard_deny=row.get("guard_deny", False), mode=row.get("mode", "auto"),
                    risk=row.get("risk", "read"), output=output, trace=trace, received=received,
                    results=results)

    with tempfile.TemporaryDirectory(prefix="mini-loop-extension-tool-") as scratch:
        async def capture():
            return [await scenario(row, Path(scratch) / row["name"]) for row in recipes]
        return dict(cases=asyncio.run(capture()), limits=["Explicit registered named-input handler, not arbitrary Go value decoding.",
                                                        "Codec/handler remain trusted construction-time seams; schema alone grants no execution."])
