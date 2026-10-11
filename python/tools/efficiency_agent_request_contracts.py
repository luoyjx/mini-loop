"""Real Agent request protocol guard and cross-round projection ledger."""

import asyncio
import copy
import json
from dataclasses import asdict
from pathlib import Path

from mini_loop.agent import Agent, _message_protocol_shape
from mini_loop.caching import NullCachePolicy
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, text
from mini_loop.token_efficiency import ComponentDescriptor, RequestOptimization, TokenEfficiencyRegistry


def efficiency_agent_request_contracts(scratch: Path | str) -> dict:
    scratch = Path(scratch)

    async def scenario(name, messages, *, action="shorten", mode="enforce", live=False,
                       purpose="agent_turn", immutable=False, ledger=False, tamper=False):
        inputs, requests, receipts, rejected, calls, histories, ledgers = [], [], [], [], [], [], []
        class Optimizer:
            descriptor = ComponentDescriptor(id="agent-request-probe", version="1", stage="request_context")
            async def optimize(self, context, *, budget_tokens=None):
                inputs.append(dict(request=copy.deepcopy(context.request),
                                   frozen_prefix_messages=context.frozen_prefix_messages,
                                   optimized_by=list(context.optimized_by), budget_tokens=budget_tokens))
                calls.append("optimize")
                if action == "cancel": raise asyncio.CancelledError()
                result = copy.deepcopy(context.request)
                tail = result["messages"][-1]
                if action == "drop": result["messages"].pop()
                elif action == "role":
                    tail["role"] = "assistant"
                    tail["content"] = "short"
                elif action == "prefix": result["messages"][0]["content"] = "short"
                elif isinstance(tail["content"], str): tail["content"] = "short"
                else:
                    for block in tail["content"]:
                        if block["type"] == "tool_result":
                            if action == "tool-id": block["tool_use_id"] = "bad"
                            elif action == "error-flag":
                                block["is_error"] = True
                                block["content"] = "short"
                            else: block["content"] = "short"
                        elif block["type"] == "text": block["text"] = "short"
                        elif block["type"] == "thinking": block["thinking"] = "short"
                return RequestOptimization(result)
        class Cache(NullCachePolicy):
            def annotate(self, *, system, tools, messages):
                calls.append("cache")
                return super().annotate(system=system, tools=tools, messages=messages)
        async def emit(event):
            if event["type"] == "optimization_receipt":
                calls.append("receipt")
                receipt = {k: v for k, v in event.items() if k not in {"type", "agent", "depth", "message_id", "parent_message_id"}}
                receipt["elapsed_ms"] = 0
                receipts.append(receipt)
            elif event["type"] == "request_optimization_rejected":
                calls.append("rejected")
                rejected.append(event["reason"])
        def responder(kwargs):
            calls.append("provider")
            requests.append(copy.deepcopy(kwargs["messages"]))
            return [text("done")], "end_turn"
        registry = TokenEfficiencyRegistry()
        registry.register_request_optimizer(Optimizer(), mode=mode)
        agent = Agent(client=FakeAsyncAnthropic(responder=responder),
                      settings=Settings(fake_llm=True, workspace_root=scratch, skills_dir=scratch / "empty-skills"),
                      workspace=scratch / name, token_efficiency=registry.runtime(), cache_policy=Cache(), emit=emit)
        if tamper:
            class Recovery:
                async def run(self, agent, kwargs, call, *, live_history=None):
                    changed = dict(kwargs)
                    changed["messages"] = [{"role": "user", "content": [{"type": "text", "text": "injected", "cache_control": {"type": "ephemeral"}}]}]
                    return await call(changed)
            agent.recovery = Recovery()
        agent.messages = copy.deepcopy(messages)
        error = None
        async def call():
            selected = agent.messages if live else copy.deepcopy(messages)
            await agent._create(selected, purpose=purpose, immutable_messages=immutable)
            histories.append(copy.deepcopy(agent.messages))
            ledgers.append([dict(index=index, digest=digest, projected=copy.deepcopy(value))
                            for (index, digest), value in sorted(agent._request_projection_ledger.items())])
        try:
            await call()
            if ledger:
                agent.messages.extend([{"role": "assistant", "content": "done"}, {"role": "user", "content": "next long user task"}])
                await call()
                agent.messages[0]["content"] = "replacement authoritative task"
                await call()
                agent.messages.pop()
                await call()
        except asyncio.CancelledError:
            error = "CancelledError"
        return dict(name=name, messages=messages, action=action, mode=mode, live=live,
                    purpose=purpose, immutable=immutable, ledger=ledger, tamper=tamper, inputs=inputs,
                    requests=requests, receipts=receipts, rejected=rejected, calls=calls,
                    histories=histories, ledgers=ledgers, error=error)

    async def capture():
        plain = [{"role": "user", "content": "long user task with unicode 中文 😀 <tag>"}]
        prefix = [{"role": "user", "content": "old long user task"}, {"role": "assistant", "content": "done"}, *plain]
        pair = [{"role": "assistant", "content": [{"type": "tool_use", "id": "call", "name": "bash", "input": {"command": "echo okay"}}]}, {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "call", "content": "long diagnostic result"}]}]
        return [
            await scenario("tail", plain, live=True),
            await scenario("side-no-ledger", plain),
            await scenario("compaction", plain, purpose="compaction"),
            await scenario("shadow", plain, mode="shadow", live=True),
            await scenario("off", plain, mode="off", live=True),
            await scenario("frozen-prefix", prefix, action="prefix", live=True),
            await scenario("assistant-frozen", [{"role": "user", "content": "question"}, {"role": "assistant", "content": [{"type": "thinking", "thinking": "long internal reasoning", "signature": "signed"}, {"type": "text", "text": "long answer"}]}], live=True),
            await scenario("role-guard", plain, action="role", live=True),
            await scenario("count-guard", plain, action="drop", live=True),
            await scenario("tool-payload", pair, live=True),
            await scenario("tool-id-guard", pair, action="tool-id", live=True),
            await scenario("error-flag-guard", pair, action="error-flag", live=True),
            await scenario("protected", plain, immutable=True, purpose="personal_skill_preview"),
            await scenario("protected-tamper", plain, immutable=True, purpose="personal_skill_preview", tamper=True),
            await scenario("cancel", plain, action="cancel"),
            await scenario("ledger-growth-prune", plain, live=True, ledger=True),
        ]
    return dict(rows=asyncio.run(capture()))


def efficiency_message_shape_contracts() -> dict:
    base = [{"role": "user", "content": "hello"}]
    values = [base, [], None, {}, [None], [{"role": "system", "content": "x"}],
              [{"role": "user", "content": None}],
              [{"role": "user", "content": [5, "opaque", None]}],
              [{"role": "user", "content": [{"type": "text", "text": "x", "cache_control": {"type": "ephemeral"}}]}],
              [{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "call", "is_error": False, "content": "x"}]}],
              [{"role": "assistant", "content": [{"type": "tool_use", "id": "call", "name": "bash", "input": {"command": "echo", "nested": [1, 1.0]}}]}],
              [{"role": "assistant", "content": [{"type": "thinking", "thinking": "x", "signature": "signed"}]}],
              [{"role": "user", "content": [{"type": "image", "source": {"type": "base64", "data": "abc"}}]}],
              [{"role": "user", "content": [{"type": True, "value": "unknown"}, {"value": float("nan")}]}],
              [{"role": "user", "content": "x", "name": "metadata", "extra": [1, 1.0]}]]
    rows = []
    for value in values:
        shape = _message_protocol_shape(value)
        projected = None if shape is None else [dict(metadata=metadata, blocks=[dict(kind=block[0], stable=block[1] if len(block)>1 else None) for block in blocks]) for metadata, blocks in shape]
        rows.append(dict(input_json=json.dumps(value, ensure_ascii=True), shape=projected))
    return dict(rows=rows)
