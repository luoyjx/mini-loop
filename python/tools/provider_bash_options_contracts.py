"""Actual Bash optional-field consumers; effects use explicit foreground/background probes."""
import asyncio
import copy
import hashlib
import inspect
import json
from pathlib import Path
from types import SimpleNamespace

from mini_loop.agent import Agent
from mini_loop.approvals import grant_candidate, proposed_candidate
from mini_loop.background import install_background, should_run_background
from mini_loop.builtins import _bash, default_registry
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.registry import Hook, Hooks, ToolCall


def provider_bash_options_contracts(scratch: Path | str) -> dict:
    scratch = Path(scratch)
    backgrounds = [("absent",), ("null", None), ("false", False), ("true", True),
                   ("zero", 0), ("integer", 7), ("negative", -2), ("float-zero", 0.0),
                   ("float", 0.5), ("empty-text", ""), ("text-false", "false"),
                   ("empty-array", []), ("array", [False]), ("empty-object", {}),
                   ("object", {"secret-key": "secret-value"})]
    prefixes = [("absent",), ("null", None), ("false", False), ("integer", 7),
                ("float", 0.5), ("text", "printf field-fixture"), ("empty-array", []),
                ("one-token", ["printf"]), ("valid", ["printf", "field-fixture"]),
                ("mismatch", ["printf", "other"]), ("mixed", ["printf", 7]),
                ("null-element", ["printf", None]), ("object", {"secret-key": "secret-value"})]

    async def scenario(index, field, profile, enabled):
        trace, events, outcomes = [], [], []
        value = {"command": "printf field-fixture"}
        if len(profile) == 2:
            value[field] = copy.deepcopy(profile[1])

        class Probe(Hook):
            async def before_tool(self, ctx, call):
                trace.append("before")

            async def guard_tool(self, ctx, call):
                trace.append("guard")

            async def after_tool(self, ctx, call, output):
                trace.append("after")

            async def on_result(self, ctx, call, output, *, denied=False, failed=False):
                trace.append("result")
                outcomes.append({"output": output, "denied": denied, "failed": failed})

        def foreground(command):
            trace.append("foreground")
            return "foreground:" + command

        class BackgroundProbe:
            def run(self, command):
                trace.append("background")
                return "background:" + command

        async def emit(event):
            if event["type"] in {"tool_use", "tool_result"}:
                events.append({key: copy.deepcopy(event[key]) for key in
                               ("type", "name", "input", "output", "denied", "error") if key in event})

        tools = default_registry()
        if enabled:
            install_background(tools)
        agent = Agent(client=FakeAsyncAnthropic(),
                      settings=Settings(fake_llm=True, workspace_root=scratch, skills_dir=scratch / "empty-skills"),
                      workspace=scratch / f"options-{index}", tools=tools, hooks=Hooks([Probe()]), emit=emit)
        agent.toolset = SimpleNamespace(run_bash_result=foreground)
        agent.state["background"] = BackgroundProbe()
        call = ToolCall(name="bash", input=copy.deepcopy(value), id="options-call")
        output = await agent._exec_tool(call)
        return {"field": field, "name": profile[0], "background_enabled": enabled,
                "input_json": json.dumps(value, ensure_ascii=True), "final_input_json": json.dumps(call.input, ensure_ascii=True),
                "output": output, "trace": trace, "events": events, "outcomes": outcomes,
                "default_candidate": grant_candidate("bash", value),
                "proposed_candidate": proposed_candidate("bash", value)}

    async def capture():
        rows = []
        for enabled in (False, True):
            for field, profiles in (("run_in_background", backgrounds), ("approval_prefix", prefixes)):
                for profile in profiles:
                    rows.append(await scenario(len(rows), field, profile, enabled))
        return rows

    sources = {"Agent._exec_tool": Agent._exec_tool, "builtins._bash": _bash,
               "should_run_background": should_run_background, "grant_candidate": grant_candidate,
               "proposed_candidate": proposed_candidate}
    return {"source_sha256": {name: hashlib.sha256(inspect.getsource(fn).encode()).hexdigest()
                              for name, fn in sources.items()}, "cases": asyncio.run(capture())}
