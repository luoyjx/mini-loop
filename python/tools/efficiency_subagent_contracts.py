"""Actual in-process child efficiency inheritance, role selection and recovery."""
import asyncio
import copy
from pathlib import Path


def efficiency_subagent_contracts(scratch: Path | str) -> dict:
    from mini_loop.agent import Agent
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.registry import Hook, Hooks
    from mini_loop.run_context import RunContext
    from mini_loop.subagents import InProcessSubagents
    from mini_loop.token_efficiency import (
        ComponentDescriptor, ConciseResponsePolicy, ConciseResponsePolicySettings,
        ObservationReduction, RequestOptimization, TokenEfficiencyRegistry,
    )

    async def scenario(name, role="worker", mode="enforce", selected=None,
                       require_opt_in=True, persist=True):
        root = Path(scratch) / name
        root.mkdir(parents=True, exist_ok=True)
        (root / "proof").write_text("masked authority " * 200)
        children, observations, requests, responses, results = [], [], [], [], []

        class Reducer:
            descriptor = ComponentDescriptor(id="child-observation", version="1", stage="observation")
            async def reduce(self, observation, **kwargs):
                observations.append(observation.content)
                return ObservationReduction("short")

        class Optimizer:
            descriptor = ComponentDescriptor(id="child-request", version="1", stage="request_context")
            async def optimize(self, context, **kwargs):
                requests.append(context.frozen_prefix_messages)
                return RequestOptimization(copy.deepcopy(context.request))

        class Response(ConciseResponsePolicy):
            async def plan(self, context):
                responses.append(context.concise_requested)
                return await super().plan(context)

        class Capture(Hook):
            async def before_tool(self, ctx, call):
                if ctx.agent.depth and not children:
                    children.append(ctx.agent)
            async def on_result(self, ctx, call, output, **kwargs):
                if ctx.agent.depth:
                    results.append(output)

        class Policy:
            def select(self, role, parent):
                names = ["read_file"] + (["read_token_artifact"] if selected else [])
                return parent.subset(name for name in names if name in parent)

        registry = TokenEfficiencyRegistry()
        registry.register_observation(Reducer())
        registry.register_request_optimizer(Optimizer())
        registry.register_response_policy(Response(ConciseResponsePolicySettings(require_opt_in=require_opt_in)))
        parent = None
        round_number = 0
        seed_ref = None
        def responder(request):
            nonlocal round_number
            round_number += 1
            if round_number == 1:
                return [tool("read_file", _id="read", path="proof")], "tool_use"
            if round_number == 2 and seed_ref and "read_token_artifact" in children[0].tools:
                return [tool("read_token_artifact", _id="recover", raw_ref=seed_ref)], "tool_use"
            return [text("done")], "end_turn"
        parent = Agent(client=FakeAsyncAnthropic(responder=responder, thinking=False),
            settings=Settings(fake_llm=True, skills_dir=root / "empty-skills", workspace_root=root,
                subagent_max_rounds=4, token_efficiency_persist_raw=persist,
                token_efficiency_raw_min_bytes=1), workspace=root,
            token_efficiency=registry.runtime(default_mode=mode), hooks=Hooks([Capture()]),
            role_tool_policy=Policy() if selected is not None else None,
            state={"permission_mode": "auto", "concise_response": True})
        store = parent.token_efficiency.raw_store
        if store:
            seed_ref = store.put_masked("seed masked authority").ref
        output = await InProcessSubagents().run(parent, prompt="inspect", agent_type=role,
                                               run_context=RunContext())
        child = children[0]
        result = dict(name=name, role=role, mode=mode, selected=selected,
            require_opt_in=require_opt_in, persist=persist, output=output,
            permission=child.state.get("permission_mode", "interactive"),
            shared_root=child.token_efficiency._lifecycle_state is parent.token_efficiency._lifecycle_state,
            store_present=child.token_efficiency.raw_store is not None,
            shared_store=store is not None and child.token_efficiency.raw_store is store,
            recovery_tool="read_token_artifact" in child.tools,
            concise_requested=bool(child.state.get("concise_response", False)),
            fresh_ledger=child._request_projection_ledger is not parent._request_projection_ledger,
            observations=len(observations), requests=len(requests), responses=responses,
            recovered=any("seed masked authority" in value for value in results),
            store_open=store is not None and not store.closed,
            parent_history_empty=parent.messages == [])
        if store:
            store.close()
        await parent.token_efficiency.close()
        return result

    async def collect():
        return [await scenario(*args, **kwargs) for args, kwargs in [
            (("worker",), {}), (("explore", "Explore"), {}),
            (("filtered",), dict(selected=False)),
            (("readonly-recovery", "Explore"), dict(selected=True)),
            (("shadow",), dict(mode="shadow")), (("off",), dict(mode="off")),
            (("concise-default",), dict(require_opt_in=False)),
            (("no-store",), dict(persist=False)),
        ]]
    return dict(rows=asyncio.run(collect()))
