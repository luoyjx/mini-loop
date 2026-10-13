"""Public role selection and actual fresh child execution observations."""

import asyncio
import copy
import tempfile
from pathlib import Path


def role_policy_contracts() -> dict:
    from mini_loop import CapabilityRoleToolPolicy, RunContext, Tool, ToolRegistry
    from mini_loop.agent import Agent
    from mini_loop.caching import NullCachePolicy
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.permissions import PermissionHook
    from mini_loop.registry import Hook, Hooks
    from mini_loop.subagents import InProcessSubagents

    tools = [
        ("read_probe", ["repo.read"], "read", True, True),
        ("write_probe", ["workspace.write"], "write", False, False),
        ("exec_probe", ["process.exec"], "exec", False, False),
        ("mixed_probe", ["repo.read", "workspace.write"], "read", True, False),
        ("unclassified_probe", [], "read", True, False),
        ("duplicate_read", ["repo.read", "repo.read"], "read", True, False),
        ("dotted_cap", ["İ"], "read", True, False),
        ("ascii_cap", ["i"], "read", True, False),
        ("sigma_cap", ["ΟΣ"], "read", True, False),
        ("final_sigma_cap", ["ος"], "read", True, False),
        ("spaced_cap", [" repo.read "], "read", True, False),
        ("read_cap_write_risk", ["repo.read"], "write", False, False),
    ]
    definitions = [dict(name=name, description="Role fixture " + name,
                        capabilities=capabilities, risk=risk, readonly=readonly,
                        parallel_safe=parallel_safe,
                        schema={"type": "object", "properties": {}})
                   for name, capabilities, risk, readonly, parallel_safe in tools]

    def policy(default, profiles):
        if default:
            return CapabilityRoleToolPolicy()
        # The public constructor receives a Mapping. Raw duplicate keys retain
        # their first dict position before normalization can create collisions.
        raw = dict((row["role"], row["capabilities"]) for row in profiles)
        return CapabilityRoleToolPolicy(raw)

    def registry(handler):
        return ToolRegistry([
            Tool(row["name"], row["description"], copy.deepcopy(row["schema"]),
                 handler(row["name"]), risk=row["risk"], readonly=row["readonly"],
                 parallel_safe=row["parallel_safe"], capabilities=row["capabilities"])
            for row in definitions
        ])

    def entries(pairs):
        return [dict(role=role, capabilities=capabilities) for role, capabilities in pairs]

    async def unused_handler(_ctx):
        raise AssertionError("selection must not execute a handler")

    parent_registry = registry(lambda _name: unused_handler)
    invalid_tools = []
    for name, capabilities in [
        ("invalid_empty", [""]),
        ("invalid_space", [" "]),
        ("invalid_control_space", ["\u0085\u001f"]),
        ("invalid_tabs", ["\t\n\r"]),
        ("invalid_mixed_empty", ["repo.read", ""]),
        ("invalid_mixed_unicode_space", ["repo.read", "repo.read", " \u3000"]),
    ]:
        error_text = ""
        try:
            ToolRegistry([Tool(name, "Invalid capability probe", {"type": "object"},
                               unused_handler, risk="read", capabilities=capabilities)])
        except Exception as error:
            error_text = f"{type(error).__name__}: {error}"
        invalid_tools.append(dict(name=name, capabilities=capabilities, error=error_text))
    cases = []

    def capture(name, role, pairs=(), *, default=False):
        profiles = entries(pairs)
        row = dict(name=name, default=default, profiles=profiles, role=role,
                   names=[], selected=[], failed=False, error="")
        try:
            selected = policy(default, profiles).select(role, parent_registry)
            row["names"] = selected.names()
            for name in selected.names():
                selected_tool = selected.get(name)
                row["selected"].append(dict(
                    name=name, description=selected_tool.description,
                    capabilities=sorted(selected_tool.capabilities), risk=selected_tool.risk,
                    readonly=selected_tool.readonly, parallel_safe=selected_tool.parallel_safe,
                    schema=copy.deepcopy(selected_tool.input_schema)))
        except Exception as error:
            row.update(failed=True, error=f"{type(error).__name__}: {error}")
        cases.append(row)

    for name, role in [("default-explore", "Explore"), ("default-explore-padded", " \tExPlOrE "),
                       ("default-worker", "worker"), ("default-general", "general-purpose"),
                       ("default-unknown", "missing"), ("default-empty", "")]:
        capture(name, role, default=True)
    dotted = [("İ", ["repo.read"]), ("i", ["workspace.write"])]
    for order, pairs in [("forward", dotted), ("reverse", list(reversed(dotted)))]:
        for suffix, role in [("dotted", "İ"), ("ascii", "i"), ("expanded", "i\u0307")]:
            capture("dotted-" + order + "-" + suffix, role, pairs)
    sigma = [("ΟΣ", ["repo.read"]), ("οσ", ["workspace.write"])]
    for order, pairs in [("forward", sigma), ("reverse", list(reversed(sigma)))]:
        for suffix, role in [("upper", "ΟΣ"), ("final", "ος"), ("medial", "οσ")]:
            capture("sigma-" + order + "-" + suffix, role, pairs)
    capture("normalized-collision-last-write", "role",
            [("Role", ["repo.read"]), (" role ", ["workspace.write"])])
    capture("normalized-collision-last-read", "ROLE",
            [(" role ", ["workspace.write"]), ("Role", ["repo.read"])])
    raw_duplicate = [("Role", ["repo.read"]), ("role", ["workspace.write"]),
                     ("Role", ["process.exec"])]
    capture("raw-duplicate-before-normalization", "role", raw_duplicate)
    capture("raw-duplicate-reversed-position", "role",
            [("role", ["workspace.write"]), ("Role", ["repo.read"]),
             ("role", ["process.exec"])])
    capture("empty-role", "", [("", ["repo.read"])])
    capture("whitespace-role", " \n", [(" \t", ["workspace.write"])])
    capture("empty-capability-set", "custom", [("custom", [])])
    capture("empty-capability-text", "custom", [("custom", [""])])
    capture("duplicate-capabilities", "custom", [("custom", ["repo.read", "repo.read"])])
    capture("unicode-capability-dotted", "custom", [("custom", ["İ"])])
    capture("unicode-capability-ascii", "custom", [("custom", ["i"])])
    capture("unicode-capability-sigma", "custom", [("custom", ["ΟΣ"])])
    capture("capability-whitespace-retained", "custom", [("custom", [" repo.read "])])
    capture("complete-capability-subset", "custom",
            [("custom", ["repo.read", "workspace.write", "process.exec"])])
    capture("empty-profile-mapping", "Explore")
    capture("default-unicode-whitespace", "\u00a0worker\u3000", default=True)

    attempts = [dict(name="read_probe", id="read_attempt"),
                dict(name="write_probe", id="write_attempt")]

    async def child_capture(name, role, pairs, default, root):
        root.mkdir()
        requests, observations, received, outputs, effects = [], [], [], [], []
        parent_context = RunContext.explicit_human(
            actor_id="human", approved_capabilities=("workflow.launch", "workflow.manage"))

        def context_snapshot(context):
            value = context.as_dict()
            if value["message_id"] == parent_context.message_id:
                value["message_id"] = "parent"
            elif value["message_id"]:
                value["message_id"] = "child"
            if value["parent_message_id"] == parent_context.message_id:
                value["parent_message_id"] = "parent"
            return value

        def observation(ctx):
            return dict(label=ctx.agent.label, depth=ctx.agent.depth,
                        mode=ctx.state.get("permission_mode", "interactive"),
                        same_workspace=ctx.workspace == root,
                        parent_marker_present="private_marker" in ctx.state,
                        context=context_snapshot(ctx.run_context))

        class Probe(Hook):
            async def before_tool(self, ctx, call):
                observations.append(observation(ctx))

            async def on_result(self, ctx, call, output, *, denied=False, failed=False):
                outputs.append(dict(id=call.id, name=call.name, output=output,
                                    denied=denied, failed=failed))

        def handler(name):
            async def execute(ctx):
                effects.append(name)
                received.append(observation(ctx))
                return "effect:" + name
            return execute

        def responder(kwargs):
            requests.append(copy.deepcopy(kwargs))
            if len(requests) == 1:
                return [tool(attempt["name"], _id=attempt["id"]) for attempt in attempts], "tool_use"
            return [text("child done")], "end_turn"

        provider = InProcessSubagents()
        profiles = entries(pairs)
        parent = Agent(client=FakeAsyncAnthropic(responder=responder, thinking=False),
                       settings=Settings(fake_llm=True, subagent_max_rounds=2,
                                         skills_dir=root / "skills", spill_dir=None),
                       workspace=root, tools=registry(handler),
                       role_tool_policy=policy(default, profiles), subagents=provider,
                       cache_policy=NullCachePolicy(), hooks=Hooks([Probe(), PermissionHook()]),
                       state={"private_marker": "parent only", "permission_mode": "auto"}, label="main")
        before = copy.deepcopy(parent.messages)
        result = await parent._run_subagent("child", role, parent_context)
        first_observation = observations[0]
        tool_results = [
            copy.deepcopy(part)
            for message in requests[1]["messages"]
            if isinstance(message.get("content"), list)
            for part in message["content"]
            if isinstance(part, dict) and part.get("type") == "tool_result"
        ]
        return dict(name=name, default=default, profiles=profiles, role=role,
                    attempts=copy.deepcopy(attempts), child_tools=requests[0].get("tools", []),
                    first_messages=requests[0]["messages"], request_count=len(requests),
                    tool_results=tool_results,
                    summary=result, outputs=outputs, observations=observations,
                    received=received, effects=effects,
                    parent_context=context_snapshot(parent_context),
                    peer_context=first_observation["context"], child_label=first_observation["label"],
                    parent_mode=parent.state["permission_mode"],
                    lineage=provider.last_lineage, parent_history_unchanged=parent.messages == before,
                    parent_marker_unchanged=parent.state["private_marker"] == "parent only")

    child_specs = [
        ("dotted-read-isolation", "İ", dotted, False),
        ("ascii-write-isolation", "i", dotted, False),
        ("sigma-read-isolation", "ΟΣ", sigma, False),
        ("explore-alias-readonly", " \tExPlOrE ",
         [("explore", ["repo.read", "workspace.write"])], False),
        ("worker-whitespace-label", " Worker ", [], True),
        ("raw-duplicate-child", "Role", raw_duplicate, False),
        ("empty-role-child", " ", [("", ["repo.read"])], False),
    ]
    with tempfile.TemporaryDirectory(prefix="mini-loop-role-policy-") as scratch:
        async def collect():
            return [await child_capture(name, role, pairs, default, Path(scratch) / name)
                    for name, role, pairs, default in child_specs]
        children = asyncio.run(collect())
    return dict(tools=definitions, invalid_tools=invalid_tools, cases=cases, children=children, limits=[
        "Actual public CapabilityRoleToolPolicy constructor and select; raw duplicate entries become dict before role normalization.",
        "Roles use Python strip/lower; capability text is exact and not normalized. Empty tool capability sets are not inherited.",
        "Policy profiles may contain empty capability text, but actual ToolRegistry registration refuses empty or all-Python-whitespace capability names.",
        "Real _run_subagent and InProcessSubagents with selected schemas, handler effects, readonly refusal and derived peer authority.",
        "tool_results are copied from the actual second model request, preserving is_error absence independently of observer flags.",
        "Only generated parent/child message IDs are normalized; child labels retain raw role.lower() whitespace.",
        "Declared text/list profiles only; arbitrary Python objects, custom mappings, concurrent mutation and all host/resource profiles remain separate.",
    ])
