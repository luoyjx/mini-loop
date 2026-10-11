"""Actual Agent response policy adoption and event projection, offline."""

import asyncio
import copy
from dataclasses import asdict
from pathlib import Path

from mini_loop.agent import Agent
from mini_loop.caching import NullCachePolicy
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, text
from mini_loop.token_efficiency import (
    ComponentDescriptor, ConciseResponsePolicy, ConciseResponsePolicySettings,
    ResponsePolicyPlan, StableRequestSettings, TokenEfficiencyRegistry,
)


def efficiency_agent_response_contracts(scratch: Path | str) -> dict:
    scratch = Path(scratch)
    async def scenario(name, *, purpose="agent_turn", system="stable", action="concise",
                       mode="enforce", opt_in=False, require_opt_in=False, maximum=None,
                       cap=1200, messages=None):
        calls, receipts, inputs = [], [], []
        class Cache(NullCachePolicy):
            def annotate(self, *, system, tools, messages):
                calls.append(dict(phase="cache", system=copy.deepcopy(system)))
                return super().annotate(system=system, tools=tools, messages=messages)
        class Policy(ConciseResponsePolicy):
            async def plan(self, context):
                inputs.append(asdict(context))
                calls.append(dict(phase="policy"))
                if action == "failure": raise RuntimeError("private response fault")
                if action == "cancel": raise asyncio.CancelledError()
                if action == "instructions":
                    return ResponsePolicyPlan(StableRequestSettings(instructions=("first", "second"), max_output_tokens=900), ("bounded_warning",))
                return await super().plan(context)
        policy = Policy(ConciseResponsePolicySettings(require_opt_in=require_opt_in, max_output_tokens=cap))
        if action != "concise":
            policy.descriptor = ComponentDescriptor(id="agent-response-probe", version="1", stage="response_policy")
        registry = TokenEfficiencyRegistry()
        registry.register_response_policy(policy, mode=mode)
        async def emit(event):
            if event["type"] == "optimization_receipt":
                calls.append(dict(phase="receipt"))
                receipts.append({key: value for key, value in event.items()
                                 if key not in {"type", "agent", "depth", "message_id", "parent_message_id"}})
                receipts[-1]["elapsed_ms"] = 0
        def responder(kwargs):
            calls.append(dict(phase="provider", system=copy.deepcopy(kwargs.get("system")),
                              max_tokens=kwargs["max_tokens"], messages=copy.deepcopy(kwargs["messages"])))
            return [text("done")], "end_turn"
        selected = messages if messages is not None else [{"role": "user", "content": "Explain the result"}]
        original = copy.deepcopy(selected)
        agent = Agent(client=FakeAsyncAnthropic(responder=responder),
                      settings=Settings(fake_llm=True, workspace_root=scratch, skills_dir=scratch / "empty-skills"),
                      workspace=scratch / name, token_efficiency=registry.runtime(),
                      cache_policy=Cache(), emit=emit, state={"concise_response": opt_in})
        error = None
        try:
            await agent._create(selected, system=system, purpose=purpose, max_tokens=maximum)
        except asyncio.CancelledError:
            error = "CancelledError"
        assert selected == original
        return dict(name=name, purpose=purpose, system=system, action=action, mode=mode,
                    opt_in=opt_in, require_opt_in=require_opt_in, maximum=maximum, cap=cap,
                    messages=original, inputs=inputs, calls=calls, receipts=receipts, error=error)

    async def capture():
        return [
            await scenario("concise"),
            await scenario("side-compaction", purpose="compaction"),
            await scenario("side-memory", purpose="memory_selection"),
            await scenario("opt-in-required", require_opt_in=True),
            await scenario("opt-in-enabled", require_opt_in=True, opt_in=True),
            await scenario("shadow", mode="shadow"),
            await scenario("off", mode="off"),
            await scenario("existing-tight-cap", maximum=300),
            await scenario("zero-uses-default", maximum=0),
            await scenario("nil-system", system=None, action="instructions"),
            await scenario("empty-system", system="", action="instructions"),
            await scenario("failure", action="failure"),
            await scenario("cancel", action="cancel"),
            await scenario("text-task", messages=[{"role": "user", "content": "older"}, {"role": "assistant", "content": "answer"}, {"role": "user", "content": [{"type": "text", "text": "first"}, {"type": "text", "text": "second"}]}]),
        ]
    return dict(rows=asyncio.run(capture()))
