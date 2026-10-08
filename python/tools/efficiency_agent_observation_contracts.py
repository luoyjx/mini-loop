"""Capture the actual Agent post-mask observation and journal boundary."""

from __future__ import annotations

import asyncio
from dataclasses import asdict
from pathlib import Path
from unittest.mock import patch

from mini_loop.actions import InMemoryActionJournal
from mini_loop.agent import Agent
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, text
from mini_loop.registry import Hook, Hooks, Tool, ToolCall, ToolRegistry
from mini_loop.secrets import SecretRegistry
from mini_loop.run_context import RunContext
from mini_loop.token_efficiency import (
    ComponentDescriptor, ObservationReduction, TokenEfficiencyRegistry,
)
from mini_loop.token_tools import install_token_efficiency_tools
from mini_loop.tools import CommandResult


def efficiency_agent_observation_contracts(scratch: Path | str) -> dict:
    scratch = Path(scratch)
    secret = "source-observation-secret-123456"

    async def scenario(name, *, mode="enforce", recoverable=False,
                       content=None, candidate="short", readonly=False,
                       persist=True, install=True, replay=False, action="reduce",
                       command=False, denied=False, artifact=False):
        content = content if content is not None else f"prefix {secret} " + "detail 中文 😀 " * 80
        inputs, receipts, results, observers, calls, journal_rows, outputs = [], [], [], [], [], [], []
        secrets = SecretRegistry()
        secrets.register("TOKEN", secret)

        class Reducer:
            descriptor = ComponentDescriptor(
                id="agent-observation-probe", version="1", stage="observation",
                content_types=("text/*", "application/json"), lossiness="recoverable" if recoverable else "lossless",
                recoverable=recoverable,
            )

            async def reduce(self, observation, *, query=None, budget_tokens=None):
                calls.append("reduce")
                inputs.append(dict(observation=asdict(observation), query=query, budget_tokens=budget_tokens))
                if action == "cancel":
                    raise asyncio.CancelledError()
                if action == "fault":
                    raise ValueError(secret)
                return ObservationReduction(candidate, warnings=(secret,) if action == "secret" else ())

        class Observer(Hook):
            async def before_tool(self, ctx, call):
                if denied:
                    return content

            async def on_result(self, ctx, call, output, *, denied=False, failed=False):
                calls.append("observer")
                observers.append(dict(output=output, denied=denied, failed=failed))

        def handler(_ctx, **_kwargs):
            calls.append("handler")
            if command:
                return CommandResult(content, "", 7, False, False, 12)
            return content

        tools = ToolRegistry([Tool(
            "probe", "observation probe", {"type": "object", "properties": {}},
            handler, readonly=True, risk="read",
        )])
        if install:
            install_token_efficiency_tools(tools)
        registry = TokenEfficiencyRegistry()
        registry.register_observation(Reducer())

        async def emit(event):
            kind = event["type"]
            if kind == "optimization_receipt":
                calls.append("receipt")
                receipt = {k: v for k, v in event.items() if k not in {
                    "type", "agent", "depth", "message_id", "parent_message_id",
                }}
                receipt["elapsed_ms"] = 0
                receipt["parent_span_id"] = "tool-span"
                receipts.append(receipt)
            elif kind == "tool_result":
                calls.append("result")
                results.append({k: event[k] for k in (
                    "name", "output", "error", "denied", "replayed", "command_result",
                ) if k in event})

        journal = InMemoryActionJournal()
        agent = Agent(
            client=FakeAsyncAnthropic(responder=lambda _kwargs: ([text("done")], "end_turn")),
            settings=Settings(fake_llm=True, workspace_root=scratch, skills_dir=scratch / "empty-skills",
                              token_efficiency_raw_min_bytes=1, token_efficiency_persist_raw=persist),
            workspace=scratch / name, tools=tools, hooks=Hooks([Observer()]), secrets=secrets,
            token_efficiency=registry.runtime(default_mode=mode), emit=emit,
            state={"session_id": "observation-session", "permission_mode": "readonly" if readonly else "default",
                   "action_journal": journal},
        )
        error = None
        tool_call = ToolCall("read_token_artifact" if artifact else "probe",
                             {"raw_ref": "raw_missing"} if artifact else {"value": secret, "nested": {"n": 1.0}}, "tool-1")
        # Only artifact entropy is fixed; the real RAM store, runtime and Agent run.
        with patch("mini_loop.token_efficiency.secrets.token_urlsafe", return_value="A" * 43):
            try:
                for _ in range(2 if replay else 1):
                    outputs.append(await agent._exec_tool(tool_call, parent_span_id="model-span",
                                                              run_context=RunContext(message_id="message-1")))
            except asyncio.CancelledError:
                error = "CancelledError"
        action_id = next((e.action_id for e in journal._records.values()), None)
        if action_id is not None:
            row = journal.get(action_id)
            journal_rows.append(dict(status=row.status, result=row.result))
        store = agent.token_efficiency.raw_store
        raw_rows = [] if store is None else [dict(ref=ref, content=store.get_masked(ref)) for ref in sorted(store._records)]
        return dict(name=name, mode=mode, recoverable=recoverable, content=content, candidate=candidate,
                    readonly=readonly, persist=persist, install=install, replay=replay, action=action,
                    command=command, denied=denied, artifact=artifact, inputs=inputs, receipts=receipts,
                    outputs=outputs, results=results, observers=observers, calls=calls,
                    journal=journal_rows, raw=raw_rows, error=error)

    async def capture():
        return [
            await scenario("lossless"),
            await scenario("recoverable", recoverable=True),
            await scenario("recovery-envelope-inflation", recoverable=True, content="short authoritative output"),
            await scenario("readonly", recoverable=True, readonly=True),
            await scenario("no-persistence", recoverable=True, persist=False),
            await scenario("no-recovery-tool", recoverable=True, install=False),
            await scenario("shadow", mode="shadow", recoverable=True),
            await scenario("off", mode="off"),
            await scenario("secret-candidate", candidate=secret, action="secret"),
            await scenario("replay", recoverable=True, replay=True),
            await scenario("denial", denied=True),
            await scenario("command", command=True),
            await scenario("json", content='  {"rows": [1, 2, 3], "padding": "long json content"}'),
            await scenario("malformed-json", content="{ invalid long diagnostic result"),
            await scenario("component-fault", action="fault"),
            await scenario("cancel", action="cancel"),
            await scenario("raw-artifact-bypass", artifact=True),
        ]

    return dict(rows=asyncio.run(capture()))
