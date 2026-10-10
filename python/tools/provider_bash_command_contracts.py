"""Actual Agent, Toolset and background consumers of finite non-text Bash commands."""
import asyncio
import copy
import hashlib
import inspect
import json
from pathlib import Path

from mini_loop.agent import Agent
from mini_loop.approvals import grant_candidate, proposed_candidate
from mini_loop.background import BackgroundManager, install_background, should_run_background
from mini_loop.builtins import _bash, default_registry
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.permissions import PermissionHook
from mini_loop.registry import Hook, Hooks, ToolCall, ToolContext
from mini_loop.tools import Toolset, looks_dangerous


def provider_bash_command_contracts(scratch: Path | str) -> dict:
    scratch = Path(scratch)
    commands = [("null", None), ("boolean", True), ("integer", 17), ("float", 13.75),
                ("empty-array", []), ("array", ["alpha", "beta"]), ("empty-object", {}),
                ("object", {"secret-key": "secret-value"})]

    async def scenario(index, name, value, enabled, explicit, mode="keep"):
        trace, events, outcomes = [], [], []
        original = {"command": copy.deepcopy(value)}
        if explicit:
            original["run_in_background"] = "false"
        if mode == "extra":
            original["extra"] = 1

        class Probe(Hook):
            async def before_tool(self, ctx, call):
                trace.append("before")
                if mode == "rewrite":
                    call.input = {"command": "printf command-rewritten"}
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
                               ("type", "name", "input", "output", "denied", "error") if key in event})

        tools = default_registry()
        if enabled:
            install_background(tools)
        agent = Agent(client=FakeAsyncAnthropic(),
                      settings=Settings(fake_llm=True, workspace_root=scratch, skills_dir=scratch / "empty-skills"),
                      workspace=scratch / f"commands-{index}", tools=tools, hooks=Hooks([Probe()]), emit=emit)
        agent.state["permission_mode"] = "auto"
        call = ToolCall(name="bash", input=copy.deepcopy(original), id="command-call")
        output = await agent._exec_tool(call)
        manager = agent.state.get("background")
        live = manager.live_count() if manager else 0
        if manager:
            await manager.close()
        return {"name": name, "mode": mode, "background_enabled": enabled, "explicit": explicit,
                "input_json": json.dumps(original, ensure_ascii=True), "final_input_json": json.dumps(call.input, ensure_ascii=True),
                "output": output, "trace": trace, "events": events, "outcomes": outcomes,
                "background_initialized": manager is not None, "background_live": live,
                "default_candidate": grant_candidate("bash", original),
                "proposed_candidate": proposed_candidate("bash", original)}

    async def capture():
        rows = []
        for enabled, explicit in ((False, False), (False, True), (True, False), (True, True)):
            for name, value in commands:
                rows.append(await scenario(len(rows), name, value, enabled, explicit))
        for mode in ("extra", "rewrite", "deny", "guard-deny"):
            rows.append(await scenario(len(rows), "null-" + mode, None, True, True, mode))
        permission_rows = []
        for name, value in commands + [("blocked-array", ["rm -rf /"]), ("blocked-object", {"command": "rm -rf /"})]:
            for mode in ("auto", "readonly"):
                state = {"permission_mode": mode}
                call = ToolCall(name="bash", input={"command": copy.deepcopy(value)}, id="permission-call")
                agent = type("PermissionAgent", (), {"tools": default_registry()})()
                ctx = ToolContext(agent=agent, workspace=scratch, state=state, call=call)
                emitted = []
                async def send(event, **fields):
                    emitted.append({"type": event, **fields})
                agent.state = state
                agent._send = send
                output = await PermissionHook().before_tool(ctx, call)
                permission_rows.append({"name": name, "mode": mode, "input_json": json.dumps(call.input),
                                        "output": output, "events": emitted})
        return rows, permission_rows

    rows, permissions = asyncio.run(capture())
    sources = {"Agent._exec_tool": Agent._exec_tool, "builtins._bash": _bash,
               "Toolset.run_bash_result": Toolset.run_bash_result, "looks_dangerous": looks_dangerous,
               "should_run_background": should_run_background, "BackgroundManager.run": BackgroundManager.run,
               "PermissionHook.before_tool": PermissionHook.before_tool,
               "grant_candidate": grant_candidate, "proposed_candidate": proposed_candidate}
    return {"source_sha256": {name: hashlib.sha256(inspect.getsource(fn).encode()).hexdigest()
                              for name, fn in sources.items()}, "cases": rows, "permissions": permissions}
