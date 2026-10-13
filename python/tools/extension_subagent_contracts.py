"""Actual capability-selected custom handlers in Source fresh subagents."""

import asyncio
import copy
import json
import tempfile
from pathlib import Path


def extension_subagent_contracts() -> dict:
    from mini_loop.agent import Agent
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.permissions import PermissionHook
    from mini_loop.registry import Hook, Hooks, Tool, ToolRegistry
    from mini_loop.run_context import RunContext

    profiles = [
        ("read", ["repo.read"], "read"),
        ("write", ["workspace.write"], "write"),
        ("mixed", ["repo.read", "workspace.write"], "read"),
        ("unclassified", [], "read"),
        ("foreign", ["custom.unknown"], "read"),
        ("read-cap-write-risk", ["repo.read"], "write"),
    ]
    schema = {"type": "object", "properties": {"name": {"type": "string", "pattern": "^[A-Z]"}}, "required": ["name"]}

    async def capture(role, profile, capabilities, risk, root):
        requests, observations, outputs, received = [], [], [], []

        def responder(kwargs):
            requests.append(copy.deepcopy(kwargs))
            if len(requests) == 1:
                return [tool("greet", name="World")], "tool_use"
            return [text("child done")], "end_turn"

        def observation(ctx):
            snapshot = ctx.run_context.as_dict()
            snapshot["message_id"] = "child"
            snapshot["parent_message_id"] = "parent"
            return dict(label=ctx.agent.label, depth=ctx.agent.depth,
                        mode=ctx.state.get("permission_mode", "interactive"),
                        same_workspace=ctx.workspace == root, context=snapshot,
                        parent_marker_present="private_marker" in ctx.state)

        class Probe(Hook):
            async def before_tool(self, ctx, call):
                observations.append(observation(ctx))

            async def on_result(self, ctx, call, output, *, denied=False, failed=False):
                outputs.append(dict(output=output, denied=denied, failed=failed))

        async def greet(ctx, name):
            received.append(observation(ctx))
            return f"{ctx.agent.label}/{ctx.agent.depth}/{ctx.state.get('permission_mode', 'interactive')}:Hello, {name}!"

        registry = ToolRegistry([Tool("greet", "Typed child tool", copy.deepcopy(schema), greet,
                                     risk=risk, capabilities=frozenset(capabilities))])
        parent = Agent(client=FakeAsyncAnthropic(responder=responder, thinking=False),
                       settings=Settings(fake_llm=True, subagent_max_rounds=2, skills_dir=root / "skills", spill_dir=None),
                       workspace=root, tools=registry, hooks=Hooks([Probe(), PermissionHook()]),
                       state={"private_marker": "parent only"}, label="main")
        parent.messages.append({"role": "user", "content": "private parent history"})
        before = copy.deepcopy(parent.messages)
        summary = await parent._run_subagent("child", role,
                                            RunContext.explicit_human(actor_id="human", approved_capabilities=("workflow.launch",)))
        tools = requests[0].get("tools", [])
        return dict(name=role + "/" + profile, role=role, capabilities=capabilities, risk=risk,
                    tools_json=json.dumps(tools, ensure_ascii=False, sort_keys=True, separators=(",", ":")),
                    first_messages=requests[0]["messages"], summary=summary, outputs=outputs,
                    observations=observations, received=received, parent_history_unchanged=parent.messages == before,
                    parent_marker_unchanged=parent.state["private_marker"] == "parent only")

    with tempfile.TemporaryDirectory(prefix="mini-loop-extension-child-") as scratch:
        async def collect():
            rows=[]
            for role in ["Explore", "worker", "general-purpose"]:
                for profile, capabilities, risk in profiles:
                    root=Path(scratch) / (role + "-" + profile);root.mkdir()
                    rows.append(await capture(role, profile, capabilities, risk, root))
            return rows
        return dict(cases=asyncio.run(collect()), schema_json=json.dumps(schema), limits=[
            "Actual Source fresh child loops and default capability policy, not name-only inheritance checks.",
            "Captured fixed execution label/depth/workspace and peer provenance; arbitrary mutable Agent state API is separate.",
            "Source child state omits parent private_marker; Go execution scope adds no business-state pointer or authority.",
        ])
