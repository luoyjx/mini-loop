"""Actual workspace entry followed by fresh in-process child artifact scope."""
import asyncio
from pathlib import Path


def efficiency_workspace_child_contracts(scratch: Path | str) -> dict:
    from mini_loop.agent import Agent
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.registry import Hook, Hooks
    from mini_loop.run_context import RunContext
    from mini_loop.subagents import InProcessSubagents
    from mini_loop.token_efficiency import (
        ComponentDescriptor, MaskedRawArtifactStore, ObservationReduction,
        TokenEfficiencyRegistry,
    )

    async def scenario(name, role="worker", mode="enforce", selected=None, persist=True):
        home, entered = Path(scratch) / name / "home", Path(scratch) / name / "entered"
        home.mkdir(parents=True, exist_ok=True)
        entered.mkdir()
        (home / "proof").write_text("home workspace authority")
        (entered / "proof").write_text("new workspace authority")
        store = MaskedRawArtifactStore(home, ttl_seconds=99, max_artifact_bytes=100, max_total_bytes=400)
        parent_ref = store.put_masked("home-only masked seed").ref
        children, observations, results = [], [], []
        child_ref = None

        class Reducer:
            descriptor = ComponentDescriptor(id="workspace-child", version="1", stage="observation")
            async def reduce(self, observation, **kwargs):
                observations.append(observation.content)
                return ObservationReduction("short")

        class Capture(Hook):
            async def before_tool(self, ctx, call):
                nonlocal child_ref
                if ctx.agent.depth and not children:
                    children.append(ctx.agent)
                    if ctx.agent.token_efficiency.raw_store:
                        child_ref = ctx.agent.token_efficiency.raw_store.put_masked("child-only masked seed").ref
            async def on_result(self, ctx, call, output, **kwargs):
                if ctx.agent.depth:
                    results.append(output)

        class Policy:
            def select(self, role, parent):
                names = ["read_file"] + (["read_token_artifact"] if selected else [])
                return parent.subset(name for name in names if name in parent)

        rounds = 0
        def responder(request):
            nonlocal rounds
            rounds += 1
            if rounds == 1:
                return [tool("read_file", _id="read", path="proof")], "tool_use"
            if rounds == 2 and "read_token_artifact" in children[0].tools:
                calls = [tool("read_token_artifact", _id="foreign", raw_ref=parent_ref)]
                if child_ref:
                    calls.append(tool("read_token_artifact", _id="own", raw_ref=child_ref))
                return calls, "tool_use"
            return [text("done")], "end_turn"
        registry = TokenEfficiencyRegistry()
        registry.register_observation(Reducer())
        parent = Agent(client=FakeAsyncAnthropic(responder=responder, thinking=False),
            settings=Settings(fake_llm=True, skills_dir=home / "empty-skills", workspace_root=home,
                subagent_max_rounds=4, token_efficiency_persist_raw=persist,
                token_efficiency_raw_min_bytes=1, token_efficiency_artifact_ttl_seconds=7,
                token_efficiency_max_artifact_bytes=200, token_efficiency_max_total_bytes=800),
            workspace=home, token_efficiency=registry.runtime(default_mode=mode, raw_store=store),
            hooks=Hooks([Capture()]), role_tool_policy=Policy() if selected is not None else None,
            state={"permission_mode": "auto"})
        parent.enter_workspace(entered)
        output = await InProcessSubagents().run(parent, prompt="inspect", agent_type=role,
                                               run_context=RunContext())
        child = children[0]
        raw = child.token_efficiency.raw_store
        limits = [raw.ttl_seconds, raw.max_artifact_bytes, raw.max_total_bytes] if raw else None
        result = dict(name=name, role=role, mode=mode, selected=selected, persist=persist,
            output=output, permission=child.state.get("permission_mode", "interactive"),
            entered_workspace=child.workspace == entered.resolve(), fresh_store=raw is not None and raw is not store,
            store_workspace_matches=raw is not None and raw.workspace == entered.resolve(),
            shared_root=child.token_efficiency._lifecycle_state is parent.token_efficiency._lifecycle_state,
            recovery_tool="read_token_artifact" in child.tools, limits=limits,
            observations=observations, foreign_recovered=any("home-only masked seed" in value for value in results),
            own_recovered=any("child-only masked seed" in value for value in results),
            parent_seed_retained=store.get_masked(parent_ref) == "home-only masked seed",
            parent_store_open=not store.closed, child_store_open=raw is not None and not raw.closed)
        if raw:
            raw.close()
        store.close()
        await parent.token_efficiency.close()
        return result

    async def collect():
        return [await scenario(*args, **kwargs) for args, kwargs in [
            (("worker",), {}), (("explore", "Explore"), {}),
            (("filtered",), dict(selected=False)),
            (("readonly-recovery", "Explore"), dict(selected=True)),
            (("shadow",), dict(mode="shadow")), (("off",), dict(mode="off")),
            (("no-persistence",), dict(persist=False)),
        ]]
    return dict(rows=asyncio.run(collect()))
