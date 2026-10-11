"""Pinned SDK inputs and actual Bash consumers for legacy numeric observations."""
from __future__ import annotations
import asyncio
import copy
import hashlib
import inspect
import json
from pathlib import Path

import anthropic
import httpx

from mini_loop.agent import Agent, _content_payload
from mini_loop.approvals import grant_candidate, proposed_candidate
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.manager import SessionManager
from mini_loop.registry import Hook, Hooks, ToolCall
from mini_loop.builtins import default_registry
from mini_loop.background import install_background, BackgroundManager, _mgr


def provider_nonfinite_bash_contracts(scratch: Path | str) -> dict:
    if anthropic.__version__ != "0.107.1":
        raise RuntimeError("Bash observation contracts require anthropic 0.107.1")
    scratch = Path(scratch)
    inputs = []
    for number_name, number in (("nan", float("nan")), ("infinity", float("inf")), ("negative-infinity", -float("inf"))):
        for shape, value in (("root", number), ("root-array", [number]), ("command", {"command": number}),
                             ("command-array", {"command": [number]}), ("command-object", {"command": {"keep": number}}),
                             ("background", {"command": "true", "run_in_background": number}),
                             ("prefix", {"command": "true", "approval_prefix": number}),
                             ("prefix-array", {"command": "true", "approval_prefix": [number]})):
            inputs.append((shape + "-" + number_name, value))

    return _provider_nonfinite_shell_contracts(scratch, inputs, tool_name="bash",
        rewrite_names={"root-nan", "command-nan", "background-nan", "prefix-nan"},
        rewrite_command="printf command-rewritten", background=False)


def _provider_nonfinite_shell_contracts(scratch, inputs, *, tool_name, rewrite_names, rewrite_command, background):
    """Capture actual SDK, standalone gate and managed consumers for one shell tool."""
    def registry():
        tools = default_registry()
        return install_background(tools) if background else tools

    async def capture():
        rows = []
        managed_rows = []
        for name, original in inputs:
            body = {"id": "message", "type": "message", "role": "assistant", "model": "model",
                    "content": [{"type": "tool_use", "id": "call", "name": tool_name, "input": original}],
                    "stop_reason": "tool_use", "usage": {"input_tokens": 1, "output_tokens": 0}}
            stream_events = [("message_start", {"type": "message_start", "message": {**body, "content": [], "stop_reason": None}}),
                             ("content_block_start", {"type": "content_block_start", "index": 0, "content_block": {**body["content"][0], "input": {}}}),
                             ("content_block_delta", {"type": "content_block_delta", "index": 0, "delta": {"type": "input_json_delta", "partial_json": json.dumps(original)}}),
                             ("content_block_stop", {"type": "content_block_stop", "index": 0}),
                             ("message_delta", {"type": "message_delta", "delta": {"stop_reason": "tool_use", "stop_sequence": None}, "usage": {"output_tokens": 0}}),
                             ("message_stop", {"type": "message_stop"})]
            sse = "".join(f"event: {event}\ndata: {json.dumps(payload)}\n\n" for event, payload in stream_events)
            requests = []
            async def transport(request):
                requests.append(json.loads(request.content))
                if json.loads(request.content).get("stream"):
                    return httpx.Response(200, content=sse.encode(), headers={"content-type": "text/event-stream"})
                return httpx.Response(200, content=json.dumps(body).encode(), headers={"content-type": "application/json"})
            async with anthropic.AsyncAnthropic(api_key="fixture-key", base_url="https://fixture.invalid", max_retries=0,
                    http_client=httpx.AsyncClient(transport=httpx.MockTransport(transport))) as sdk:
                response = await sdk.messages.create(model="model", max_tokens=8, messages=[{"role": "user", "content": "hello"}])
                async with sdk.messages.stream(model="model", max_tokens=8, messages=[{"role": "user", "content": "hello"}]) as stream:
                    streamed = await stream.get_final_message()
            observed = _content_payload(response.content)
            profiles = ["keep"]
            if name in rewrite_names:
                profiles += ["rewrite", "deny", "guard-deny"]
            for mode in profiles:
                trace, events, outcomes = [], [], []
                class Probe(Hook):
                    async def before_tool(self, ctx, call):
                        trace.append("before")
                        if mode == "rewrite":
                            call.input = {"command": rewrite_command}
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
                        outcomes.append({"output": output, "failed": failed, "denied": denied})
                async def emit(event):
                    if event["type"] in {"tool_use", "tool_result"}:
                        events.append(json.dumps({key: event[key] for key in ("type", "input", "display") if key in event}, sort_keys=True))
                agent = Agent(client=FakeAsyncAnthropic(), settings=Settings(fake_llm=True, workspace_root=scratch,
                              skills_dir=scratch / "skills"), workspace=scratch / f"case-{len(rows)}",
                              tools=registry(), hooks=Hooks([Probe()]), emit=emit)
                agent.state["permission_mode"] = "auto"
                call = ToolCall(name=tool_name, input=copy.deepcopy(observed[0]["input"]), id="call")
                output = await agent._exec_tool(call)
                background_fields = {}
                if background:
                    owned = agent.state.get("background")
                    background_fields = {"background_initialized": owned is not None,
                        "background_live": owned.live_count() if owned else 0,
                        "background_counter": owned._counter if owned else 0,
                        "events": [{"display": json.loads(event)["display"]} for event in events if "display" in json.loads(event)]}
                    if owned:
                        await owned.close()
                rows.append({"kind": "tool_use", "name": name, "mode": mode, "input_json": json.dumps(original),
                             "canonical_json": json.dumps(original, sort_keys=True, separators=(",", ":"), ensure_ascii=False),
                             "response_json": json.dumps(body), "content_json": json.dumps(observed, sort_keys=True),
                             "sse": sse, "stream_content_json": json.dumps(_content_payload(streamed.content), sort_keys=True),
                             **background_fields, "output": output, "trace": trace, "outcomes": outcomes, "events_json": events,
                             "final_input_json": json.dumps(call.input),
                             "default_candidate": grant_candidate(tool_name, original if isinstance(original, dict) else {}),
                             "proposed_candidate": proposed_candidate(tool_name, original if isinstance(original, dict) else {})})
            if name == "root-nan":
                body = {**body, "content": [{"type": "tool_use", "id": "valid", "name": tool_name, "input": {"command": "printf should-not-run"}}, *body["content"]]}
            managed_trace, managed_events = [], []
            class ManagedProbe(Hook):
                async def before_tool(self, ctx, call): managed_trace.append("before")
                async def guard_tool(self, ctx, call): managed_trace.append("guard")
                async def after_tool(self, ctx, call, output): managed_trace.append("after")
                async def on_result(self, ctx, call, output, *, denied=False, failed=False): managed_trace.append("result")
            async def collect(event):
                if event["type"] in {"tool_use", "tool_result"}:
                    managed_events.append(copy.deepcopy(event))
            sdk = anthropic.AsyncAnthropic(api_key="fixture-key", base_url="https://fixture.invalid", max_retries=0,
                    http_client=httpx.AsyncClient(transport=httpx.MockTransport(transport)))
            manager = SessionManager(Settings(fake_llm=True, workspace_root=scratch / f"managed-{name}", skills_dir=scratch / "skills"),
                                     sdk, tool_registry=registry(), hooks=Hooks([ManagedProbe()]), event_sink=collect)
            try:
                session = manager.create(owner="alice", permission_mode="auto")
                before = len(requests)
                try:
                    final = await session.run("hello")
                    error = None
                except Exception as failure:
                    final = None
                    error = {"class": type(failure).__name__, "message": str(failure)}
                uses = [event for event in managed_events if event["type"] == "tool_use"]
                results = [event for event in managed_events if event["type"] == "tool_result"]
                managed_rows.append({"name": name, "input_json": json.dumps(original), "response_json": json.dumps(body), "trace": managed_trace,
                                     "tool_input_json": json.dumps(uses[0]["input"]) if uses else None,
                                     "tool_output": results[0]["output"] if results else None,
                                     "action_recorded": manager.actions.get(uses[0]["action_id"]) is not None if uses else False,
                                     "transport_requests": len(requests) - before, "final": final, "error": error,
                                     "messages": len(session.agent.messages), "tool_uses": len(uses),
                                     **({"background_initialized": session.agent.state.get("background") is not None} if background else {})})
            finally:
                await manager.stop()
                await sdk.close()
        return rows, managed_rows
    rows, managed = asyncio.run(capture())
    sources = {"Agent._exec_tool": Agent._exec_tool, "Agent._exec_tool_batch": Agent._exec_tool_batch,
               "grant_candidate": grant_candidate, "proposed_candidate": proposed_candidate}
    if background:
        sources.update({"BackgroundManager.run": BackgroundManager.run, "background._mgr": _mgr})
    return {"sdk_version": anthropic.__version__, "cases": rows, "managed": managed,
            "source_sha256": {name: hashlib.sha256(inspect.getsource(fn).encode()).hexdigest()
                              for name, fn in sources.items()}}
