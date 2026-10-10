"""Actual installed background_run argument consumers through the Source gate."""
import asyncio
import copy
import hashlib
import inspect
import json
from pathlib import Path

from mini_loop.agent import Agent
from mini_loop.background import BackgroundManager, install_background, _mgr
from mini_loop.builtins import default_registry
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.registry import Hook, Hooks, Tool, ToolCall
from mini_loop.tools import looks_dangerous


def provider_background_input_contracts(scratch: Path | str) -> dict:
    scratch = Path(scratch)
    values = [("null", None), ("boolean", True), ("integer", 17),
              ("float", 13.75), ("empty-array", []), ("array", ["alpha"]),
              ("empty-object", {}), ("object", {"command": "inert"})]
    recipes = [(name, {"command": value}, "keep") for name, value in values]
    recipes += [("missing", {}, "keep"), ("extra", {"command": None, "extra": 1}, "keep"),
                ("ordered-extra", {"command": None, "z_extra": 1, "a_extra": 2}, "keep"),
                ("extra-and-missing", {"extra": 1}, "keep"),
                ("root-null", None, "keep"), ("root-array", [], "keep"),
                ("root-integer", 17, "keep")]
    recipes += [("null-" + mode, {"command": None}, mode)
                for mode in ("rewrite", "deny", "guard-deny")]

    async def scenario(index, name, value, mode):
        trace, events, outcomes = [], [], []

        class Probe(Hook):
            async def before_tool(self, ctx, call):
                trace.append("before")
                if mode == "rewrite":
                    call.input = {"command": "printf background-input-rewritten"}
                if mode == "deny":
                    return "denied by source probe"

            async def guard_tool(self, ctx, call):
                trace.append("guard")
                if mode == "guard-deny":
                    return "denied by source guard"

            async def after_tool(self, ctx, call, output):
                trace.append("after")

            async def on_result(self, ctx, call, output, *, denied=False, failed=False):
                trace.append("result")
                outcomes.append({"output": output, "denied": denied, "failed": failed})

        async def emit(event):
            if event["type"] in {"tool_use", "tool_result"}:
                events.append({key: copy.deepcopy(event[key]) for key in
                               ("type", "name", "input", "output", "denied", "error", "display")
                               if key in event})

        tools = install_background(default_registry())
        agent = Agent(client=FakeAsyncAnthropic(),
                      settings=Settings(fake_llm=True, workspace_root=scratch,
                                        skills_dir=scratch / "empty-skills"),
                      workspace=scratch / f"background-input-{index}", tools=tools,
                      hooks=Hooks([Probe()]), emit=emit)
        agent.state["permission_mode"] = "auto"
        call = ToolCall(name="background_run", input=copy.deepcopy(value), id="background-input-call")
        row = {"name": name, "mode": mode, "input_json": json.dumps(value, ensure_ascii=True),
               "trace": trace, "events": events, "outcomes": outcomes}
        try:
            row["output"] = await agent._exec_tool(call)
        except Exception as error:
            row["error"] = {"class": type(error).__name__, "message": str(error)}
        finally:
            manager = agent.state.get("background")
            row["background_initialized"] = manager is not None
            row["background_live"] = manager.live_count() if manager else 0
            row["background_counter"] = manager._counter if manager else 0
            if manager:
                await manager.close()
        row["final_input_json"] = json.dumps(call.input, ensure_ascii=True)
        return row

    async def capture():
        return [await scenario(index, *recipe) for index, recipe in enumerate(recipes)]

    sources = {"Agent._exec_tool": Agent._exec_tool, "Tool.run": Tool.run,
               "install_background": install_background, "background._mgr": _mgr,
               "BackgroundManager.run": BackgroundManager.run, "looks_dangerous": looks_dangerous}
    return {"source_sha256": {name: hashlib.sha256(inspect.getsource(fn).encode()).hexdigest()
                              for name, fn in sources.items()}, "cases": asyncio.run(capture())}
