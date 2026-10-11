"""Actual Agent/default-Bash consumer outcomes for pinned streamed SDK inputs."""
import asyncio
import copy
import hashlib
import inspect
import json
from pathlib import Path
from types import SimpleNamespace

from mini_loop.agent import Agent
from mini_loop.builtins import _bash, default_registry
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.registry import Hook, Hooks, ToolCall, Tool
from tools.provider_partial_input_contracts import provider_partial_input_contracts


def provider_tool_input_consumer_contracts(scratch: Path | str) -> dict:
    scratch = Path(scratch)
    sdk = provider_partial_input_contracts()
    inputs = [(row["name"], json.loads(row["expected_json"])[0]["input"], "keep")
              for row in sdk["cases"] if row["kind"] == "tool_use" and "expected_json" in row]
    inputs += [("ordered-extra", {"command": "true", "z_extra": 1, "a_extra": 2}, "keep"),
               ("empty-extra", {"command": "true", "": 1}, "keep"),
               ("extra-and-missing", {"keep": 1}, "keep"),
               ("extra-rewritten", {"command": "true", "keep": 1}, "rewrite"),
               ("missing-rewritten", {}, "rewrite"),
               ("extra-denied-before", {"command": "true", "keep": 1}, "deny"),
               ("missing-denied-before", {}, "deny"),
               ("root-float", 13.75, "keep"),
               ("root-rewritten", ["original"], "rewrite"),
               ("root-denied-before", None, "deny")]

    async def scenario(index, name, value, mode):
        trace, events, outcomes = [], [], []

        class Probe(Hook):
            async def before_tool(self, ctx, call):
                trace.append("before")
                if mode == "rewrite":
                    call.input = {"command": "rewritten"}
                if mode == "deny":
                    return "denied by source probe"
                return None

            async def guard_tool(self, ctx, call):
                trace.append("guard")
                return None

            async def after_tool(self, ctx, call, output):
                trace.append("after")
                return None

            async def on_result(self, ctx, call, output, *, denied=False, failed=False):
                trace.append("result")
                outcomes.append({"output": output, "denied": denied, "failed": failed})

        def execute(command):
            trace.append("execute")
            return "executed:" + command

        async def emit(event):
            if event["type"] in {"tool_use", "tool_result"}:
                fields = {key: copy.deepcopy(event[key]) for key in ("type", "name", "input", "output", "denied", "failed") if key in event}
                events.append(json.dumps(fields, sort_keys=True, ensure_ascii=True))

        workspace = scratch / f"consumer-{index}"
        agent = Agent(client=FakeAsyncAnthropic(),
                      settings=Settings(fake_llm=True, workspace_root=scratch, skills_dir=scratch / "empty-skills"),
                      workspace=workspace, tools=default_registry(), hooks=Hooks([Probe()]), emit=emit)
        agent.toolset = SimpleNamespace(run_bash_result=execute)
        call = ToolCall(name="bash", input=copy.deepcopy(value), id="fixture-call")
        row = {"name": name, "mode": mode, "input_json": json.dumps(value, ensure_ascii=True), "trace": trace, "events_json": events, "outcomes": outcomes}
        try:
            row["output"] = await agent._exec_tool(call)
        except Exception as error:
            row["error"] = {"class": type(error).__name__, "message": str(error)}
        row["final_input_json"] = json.dumps(call.input, ensure_ascii=True)
        return row

    async def capture():
        return [await scenario(index, name, value, mode) for index, (name, value, mode) in enumerate(inputs)]

    sources = {"Agent._exec_tool": Agent._exec_tool, "Tool.run": Tool.run, "builtins._bash": _bash}
    return {"sdk_version": sdk["sdk_version"], "jiter_version": sdk["jiter_version"], "source_sha256": {name: hashlib.sha256(inspect.getsource(fn).encode()).hexdigest() for name, fn in sources.items()}, "cases": asyncio.run(capture())}
