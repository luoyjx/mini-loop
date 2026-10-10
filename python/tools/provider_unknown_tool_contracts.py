"""Actual missing-tool dispatch, hook and permission behavior in Source Agent."""
import asyncio
import copy
import hashlib
import inspect
import json
from pathlib import Path

from mini_loop.agent import Agent
from mini_loop.builtins import default_registry
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.permissions import PermissionHook
from mini_loop.registry import Hook, Hooks, ToolCall


def provider_unknown_tool_contracts(scratch: Path | str) -> dict:
    scratch = Path(scratch)
    values = [("null", None), ("boolean", True), ("integer", 17), ("float", 13.75),
              ("text", "inert"), ("array", ["inert"]), ("empty-object", {}),
              ("object", {"command": "rm -rf /", "secret-key": "secret-value"})]
    recipes = [(name + "/" + permission, "missing-tool", value, "keep", permission)
               for permission in ("interactive", "auto", "readonly") for name, value in values]
    recipes += [("name/" + name, name, {}, "keep", "interactive")
                for name in ("", "bash ", "MISSING", "缺失🌟")]
    recipes += [("hook/" + mode, "missing-tool", None, mode, "interactive")
                for mode in ("rewrite", "deny", "guard-deny", "after-replace")]

    async def scenario(index, name, tool_name, value, mode, permission):
        trace, events, outcomes = [], [], []

        class Probe(Hook):
            async def before_tool(self, ctx, call):
                trace.append("before")
                if mode == "rewrite":
                    call.input = {"rewritten": True}
                if mode == "deny":
                    return "denied by source probe"

            async def guard_tool(self, ctx, call):
                trace.append("guard")
                if mode == "guard-deny":
                    return "denied by source guard"

            async def after_tool(self, ctx, call, output):
                trace.append("after")
                if mode == "after-replace":
                    return "after:" + output

            async def on_result(self, ctx, call, output, *, denied=False, failed=False):
                trace.append("result")
                outcomes.append({"output": output, "denied": denied, "failed": failed})

        async def emit(event):
            if event["type"] in {"tool_use", "tool_result", "permission"}:
                events.append({key: copy.deepcopy(event[key]) for key in
                               ("type", "name", "input", "output", "denied", "error", "display",
                                "decision", "rule", "tool", "reason") if key in event})

        agent = Agent(client=FakeAsyncAnthropic(),
                      settings=Settings(fake_llm=True, workspace_root=scratch,
                                        skills_dir=scratch / "empty-skills"),
                      workspace=scratch / f"unknown-tool-{index}", tools=default_registry(),
                      hooks=Hooks([Probe(), PermissionHook()]), emit=emit)
        agent.state["permission_mode"] = permission
        call = ToolCall(name=tool_name, input=copy.deepcopy(value), id="unknown-tool-call")
        output = await agent._exec_tool(call)
        return {"name": name, "tool_name": tool_name, "mode": mode,
                "permission_mode": permission, "input_json": json.dumps(value, ensure_ascii=True),
                "final_input_json": json.dumps(call.input, ensure_ascii=True),
                "output": output, "trace": trace, "events": events, "outcomes": outcomes}

    async def capture():
        return [await scenario(index, *recipe) for index, recipe in enumerate(recipes)]

    sources = {"Agent._exec_tool": Agent._exec_tool, "PermissionHook.before_tool": PermissionHook.before_tool}
    return {"source_sha256": {name: hashlib.sha256(inspect.getsource(fn).encode()).hexdigest()
                              for name, fn in sources.items()}, "cases": asyncio.run(capture())}
