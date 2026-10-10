"""Actual Source gate and post-spawn background optional-field observations."""
from __future__ import annotations

import asyncio
import copy
import hashlib
import inspect
import json
import warnings
from pathlib import Path
from unittest.mock import patch

from mini_loop.agent import Agent
from mini_loop.approvals import grant_candidate, proposed_candidate
from mini_loop.background import BackgroundManager, install_background
import mini_loop.background as background
from mini_loop.builtins import default_registry
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.registry import Hook, Hooks, ToolCall


def provider_background_options_contracts(scratch: Path | str) -> dict:
    """Keep task acceptance separate from eventual timeout/consumer failure."""
    values = [("null", None), ("false", False), ("true", True), ("zero", 0),
              ("float-zero", 0.0), ("negative-zero", -0.0), ("integer", 2),
              ("fraction", 0.005), ("negative", -1), ("negative-fraction", -0.5),
              ("empty-text", ""), ("numeric-text", "2"), ("text", "bad"),
              ("empty-array", []), ("array", [1]), ("empty-object", {}),
              ("object", {"keep": 1}), ("nan", float("nan")),
              ("infinity", float("inf")), ("negative-infinity", -float("inf")),
              ("huge-integer", 10 ** 400)]
    profiles = [("timeout-missing", {"command": "sleep 0.03; printf options-done"})]
    profiles += [("timeout-" + name, {"command": "sleep 0.03; printf options-done", "timeout": value})
                 for name, value in values]
    profiles.append(("timeout-true-expired", {"command": "sleep 1.1; printf options-done", "timeout": True}))
    profiles.append(("prefix-missing", {"command": "printf options-done"}))
    profiles += [("prefix-" + name, {"command": "printf options-done", "approval_prefix": value})
                 for name, value in values]
    profiles += [("prefix-valid", {"command": "printf options-done", "approval_prefix": ["printf", "options-done"]}),
                 ("prefix-mixed", {"command": "printf options-done", "approval_prefix": ["printf", 1]}),
                 ("prefix-mismatch", {"command": "printf options-done", "approval_prefix": ["printf", "other"]})]

    async def capture():
        rows = []
        original_spawn, original_kill = asyncio.create_subprocess_exec, background._kill_group
        for name, arguments in profiles:
            trace, outcomes, processes, kills = [], [], [], []
            class Probe(Hook):
                async def before_tool(self, ctx, call): trace.append("before")
                async def guard_tool(self, ctx, call): trace.append("guard")
                async def after_tool(self, ctx, call, output): trace.append("after")
                async def on_result(self, ctx, call, output, *, denied=False, failed=False):
                    trace.append("result")
                    outcomes.append({"output": output, "denied": denied, "failed": failed})
            async def spawn(*argv, **kwargs):
                process = await original_spawn(*argv, **kwargs)
                processes.append(process)
                return process
            def kill(process):
                kills.append("kill-group")
                original_kill(process)
            agent = Agent(client=FakeAsyncAnthropic(),
                settings=Settings(fake_llm=True, workspace_root=Path(scratch), skills_dir=Path(scratch) / "skills"),
                workspace=Path(scratch) / name, tools=install_background(default_registry()), hooks=Hooks([Probe()]))
            agent.state["permission_mode"] = "auto"
            owned = None
            with patch.object(asyncio, "create_subprocess_exec", spawn), patch.object(background, "_kill_group", kill), \
                    warnings.catch_warnings(record=True) as recorded:
                warnings.simplefilter("always", RuntimeWarning)
                try:
                    output = await agent._exec_tool(ToolCall(name="background_run", input=copy.deepcopy(arguments), id="call"))
                    starts_at_return = len(processes)
                    owned = agent.state.get("background")
                    if owned is None or owned._counter != 1:
                        raise AssertionError(f"{name}: optional input did not allocate a task")
                    task = owned._tasks["bg_0001"]
                    await asyncio.wait_for(asyncio.shield(task["handle"]), timeout=3)
                    rows.append({"name": name, "input_json": json.dumps(arguments), "output": output,
                        "trace": trace, "outcomes": outcomes, "starts_at_return": starts_at_return,
                        "starts_at_settlement": len(processes), "kills_at_settlement": len(kills),
                        "status": task["status"], "result": task["result"],
                        "check": owned.check("bg_0001"), "notifications": owned.drain(),
                        "live_at_settlement": owned.live_count(), "counter": owned._counter,
                        "default_candidate": grant_candidate("background_run", arguments),
                        "proposed_candidate": proposed_candidate("background_run", arguments),
                        "warnings": [{"class": warning.category.__name__, "message": str(warning.message)}
                                     for warning in recorded]})
                finally:
                    # Source type/overflow faults do not kill the spawned command.
                    # Harmless commands finish naturally; join and drain all of them.
                    for process in processes:
                        await asyncio.wait_for(process.communicate(), timeout=3)
                    if owned:
                        await owned.close()
        return rows
    sources = {"Agent._exec_tool": Agent._exec_tool, "BackgroundManager.run": BackgroundManager.run,
               "BackgroundManager._exec": BackgroundManager._exec, "BackgroundManager._settle": BackgroundManager._settle,
               "grant_candidate": grant_candidate, "proposed_candidate": proposed_candidate,
               "asyncio.wait_for": asyncio.wait_for}
    return {"cases": asyncio.run(capture()),
            "source_sha256": {name: hashlib.sha256(inspect.getsource(fn).encode()).hexdigest() for name, fn in sources.items()},
            "limits": ["Standalone actual gate/task execution; no managed journal or SDK transport acceptance claim",
                       "Source type/overflow faults do not kill groups; capture joins harmless commands after settlement",
                       "No wall-clock durations or process identifiers are fixture authority"]}
