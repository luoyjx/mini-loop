"""Actual fixed-SDK caller defaults, forwarding, and safe Agent observations."""

import asyncio
import copy
import hashlib
import inspect
import json
import os
import tempfile
import warnings
from dataclasses import replace
from pathlib import Path
from unittest.mock import patch

import anthropic
import httpx
import pydantic
from anthropic._models import BaseModel, _construct_field, construct_type
from anthropic.lib.streaming._messages import accumulate_event
from anthropic.types.direct_caller import DirectCaller
from anthropic.types.parsed_message import ParsedMessage
from anthropic.types.server_tool_caller import ServerToolCaller
from anthropic.types.server_tool_caller_20260120 import ServerToolCaller20260120
from anthropic.types.server_tool_use_block import ServerToolUseBlock
from anthropic.types.tool_use_block import ToolUseBlock
from anthropic.types.web_fetch_tool_result_block import WebFetchToolResultBlock
from anthropic.types.web_search_tool_result_block import WebSearchToolResultBlock
from httpx._content import encode_json

from mini_loop.agent import Agent, _content_payload, _tool_action_id
from mini_loop.config import Settings
from mini_loop.permissions import PermissionHook
from mini_loop.registry import Hook, Hooks, Tool, ToolRegistry
from mini_loop.run_context import RunContext


def _legacy_json(value):
    return json.dumps(value, ensure_ascii=True, separators=(",", ":"), allow_nan=True)


def _caller_profiles():
    profiles = [
        ("unknown-type", {"type": "future_caller", "tool_id": "parent", "future": {"secret-key": "secret-value"}}),
        ("missing-type", {"tool_id": "parent"}),
        ("null-type", {"type": None, "tool_id": "parent"}),
        ("empty-type", {"type": "", "tool_id": "parent"}),
        ("number-type", {"type": 513, "tool_id": "parent"}),
        ("bool-type", {"type": True, "tool_id": "parent"}),
        ("list-type", {"type": ["direct"], "tool_id": "parent"}),
        ("object-type", {"type": {"name": "direct"}, "tool_id": "parent"}),
        ("root-text", "caller text"),
        ("root-number", 513),
        ("root-bool", True),
        ("root-empty-list", []),
        ("root-empty-object", {}),
        ("root-mixed-list", [{}, {"tool_id": 7}, {"type": "code_execution_20250825"},
                             {"type": "code_execution_20260120", "tool_id": None},
                             ["nested"], True, [{}, {"type": "code_execution_20250825"}]]),
    ]
    for label, number in (("nan", float("nan")), ("infinity", float("inf")),
                          ("negative-infinity", -float("inf"))):
        profiles.extend([
            ("code-2025-id-" + label, {"type": "code_execution_20250825", "tool_id": number}),
            ("code-2026-id-" + label, {"type": "code_execution_20260120", "tool_id": number}),
            ("nonfinite-type-" + label, {"type": number, "tool_id": "parent"}),
            ("nonfinite-root-" + label, number),
        ])
    profiles.extend([
        ("unknown-extension-nonfinite", {"type": "future_caller", "tool_id": "parent",
                                        "future": {"secret-key": [float("nan"), float("inf"), -float("inf"), "secret-value"]}}),
        ("code-id-nested-nonfinite", {"type": "code_execution_20250825",
                                      "tool_id": {"secret-key": [float("nan"), float("inf"), -float("inf"), "secret-value"]}}),
    ])
    return profiles


def _blocks():
    return [
        ("tool_use", {"id": "client", "name": "bash", "input": {"command": "true"}}),
        ("server_tool_use", {"id": "server", "name": "web_search", "input": {}}),
        ("web_search_tool_result", {"tool_use_id": "search", "content": {"type": "web_search_tool_result_error", "error_code": "unavailable"}}),
        ("web_fetch_tool_result", {"tool_use_id": "fetch", "content": {"type": "web_fetch_tool_result_error", "error_code": "unavailable"}}),
    ]


def _message(content, *, stop="end_turn"):
    return {"id": "fixture", "type": "message", "role": "assistant", "model": "fixture-model",
            "content": content, "stop_reason": stop, "stop_sequence": None,
            "usage": {"input_tokens": 1, "output_tokens": 2}}


def _stream(message):
    events = [("message_start", {"type": "message_start", "message": {
        **message, "content": [], "stop_reason": None, "usage": {"input_tokens": 1, "output_tokens": 0}}})]
    for index, block in enumerate(message["content"]):
        initial = copy.deepcopy(block)
        if "input" in initial:
            initial["input"] = {}
        events.append(("content_block_start", {"type": "content_block_start", "index": index, "content_block": initial}))
        if block.get("input"):
            raw = _legacy_json(block["input"])
            for fragment in (raw[:len(raw) // 2], raw[len(raw) // 2:]):
                events.append(("content_block_delta", {"type": "content_block_delta", "index": index,
                    "delta": {"type": "input_json_delta", "partial_json": fragment}}))
        events.append(("content_block_stop", {"type": "content_block_stop", "index": index}))
    events.extend([
        ("message_delta", {"type": "message_delta", "delta": {"stop_reason": message["stop_reason"], "stop_sequence": None}, "usage": {"output_tokens": 2}}),
        ("message_stop", {"type": "message_stop"}),
    ])
    return "".join(f"event: {kind}\ndata: {_legacy_json(value)}\n\n" for kind, value in events)


async def _capture_sdk(name, mode, message, *, forward):
    response_json, sse = _legacy_json(message), _stream(message)
    requests = []

    def transport(request):
        requests.append(json.loads(request.content))
        if len(requests) > 1:
            return httpx.Response(200, content=_legacy_json(_message([{"type": "text", "text": "done"}])).encode(),
                                  headers={"content-type": "application/json"}, request=request)
        return httpx.Response(200, content=(response_json if mode == "direct" else sse).encode(),
                              headers={"content-type": "application/json" if mode == "direct" else "text/event-stream"},
                              request=request)

    with warnings.catch_warnings(record=True) as diagnostics:
        warnings.simplefilter("always")
        async with httpx.AsyncClient(transport=httpx.MockTransport(transport)) as http:
            async with anthropic.AsyncAnthropic(api_key="fixture-key", base_url="https://fixture.invalid",
                                               http_client=http, max_retries=0) as client:
                request = {"model": "fixture-model", "max_tokens": 8,
                           "messages": [{"role": "user", "content": "hello"}]}
                if mode == "direct":
                    response = await client.messages.create(**request)
                else:
                    async with client.messages.stream(**request) as stream:
                        response = await stream.get_final_message()
                content = _content_payload(response.content)
                row = {"name": name, "mode": mode, "response_json": response_json, "sse": sse,
                       "expected_content_json": _legacy_json(content),
                       "sdk_block_class": type(response.content[0]).__name__,
                       "sdk_caller_class": type(response.content[0].caller).__name__,
                       "warning_count": len(diagnostics)}
                forwarded = None
                if forward:
                    succeeded, error_class, error_message = False, None, None
                    forward_messages = [{"role": "assistant", "content": content},
                                        {"role": "user", "content": [{"type": "tool_result",
                                          "tool_use_id": content[0]["id"], "content": "safe fixture result"}]}]
                    try:
                        await client.messages.create(model="fixture-model", max_tokens=8,
                                                     messages=forward_messages)
                        succeeded = True
                    except Exception as error:
                        error_class, error_message = type(error).__name__, str(error)
                    forwarded = {"name": name, "mode": mode, "content_json": _legacy_json(content),
                                 "request_messages_json": _legacy_json(forward_messages),
                                 "succeeded": succeeded, "error_class": error_class, "error_message": error_message,
                                 "transport_requests": len(requests),
                                 "forwarded_request_json": _legacy_json(requests[1]) if len(requests) > 1 else None}
    return row, forwarded


def _normalise_paths(value, root):
    if isinstance(value, str):
        return value.replace(str(root), "<scratch>")
    if isinstance(value, list):
        return [_normalise_paths(item, root) for item in value]
    if isinstance(value, dict):
        return {key: _normalise_paths(item, root) for key, item in value.items()}
    return value


async def _capture_agent(name, caller, root):
    trace, requests, handled, results, events = [], [], [], [], []
    first = _message([{"type": "tool_use", "id": "actual-use", "name": "safe_probe",
                       "input": {"value": "safe-value"}, "caller": copy.deepcopy(caller)}], stop="tool_use")

    def transport(request):
        trace.append("transport")
        requests.append(json.loads(request.content))
        body = first if len(requests) == 1 else _message([{"type": "text", "text": "done"}])
        return httpx.Response(200, content=_legacy_json(body).encode(),
                              headers={"content-type": "application/json"}, request=request)

    class Probe(Hook):
        async def before_tool(self, ctx, call):
            trace.append("before")

        async def guard_tool(self, ctx, call):
            trace.append("guard")

        async def after_tool(self, ctx, call, output):
            trace.append("after")

        async def on_result(self, ctx, call, output, *, denied=False, failed=False):
            trace.append("result")
            results.append({"id": call.id, "name": call.name, "output": output,
                            "denied": denied, "failed": failed})

    async def safe_handler(ctx, value):
        trace.append("handler")
        handled.append({"id": ctx.call.id, "name": ctx.call.name, "value": value,
                        "action_id": ctx.action_id, "run_context": ctx.run_context.as_dict(),
                        "permission_mode": ctx.state.get("permission_mode")})
        return "safe output"

    async def emit(event):
        if event["type"] in {"tool_use", "tool_result", "error"}:
            trace.append(event["type"])
        fields = ("type", "name", "id", "input", "output", "error", "denied", "failed", "replayed",
                  "action_id", "agent", "depth", "message_id", "purpose", "status", "model")
        events.append({key: copy.deepcopy(event[key]) for key in fields if key in event})

    registry = ToolRegistry([Tool("safe_probe", "Safe observation fixture",
        {"type": "object", "properties": {"value": {"type": "string"}}, "required": ["value"]},
        safe_handler, readonly=True, risk="read")])
    configured = Settings(fake_llm=True, model="fixture-model", max_tokens=8, max_turns=2,
                          workspace_root=root, skills_dir=root / "skills", spill_dir=None)
    run = replace(RunContext.peer_agent(delegated_by="fixture-parent", actor_id="fixture-peer"),
                  message_id="fixture-message")
    with warnings.catch_warnings(record=True):
        warnings.simplefilter("always")
        async with httpx.AsyncClient(transport=httpx.MockTransport(transport)) as http:
            async with anthropic.AsyncAnthropic(api_key="fixture-key", base_url="https://fixture.invalid",
                                               http_client=http, max_retries=0) as client:
                agent = Agent(client=client, settings=configured, workspace=root / name, tools=registry,
                              hooks=Hooks([Probe(), PermissionHook()]), emit=emit, system="Caller observation fixture.",
                              state={"session_id": "caller-fixture", "permission_mode": "auto"})
                final, error_class, error_message = None, None, None
                try:
                    final = await agent.run("run the safe fixture", run_context=run)
                except Exception as error:
                    error_class, error_message = type(error).__name__, str(error)
                history = copy.deepcopy(agent.messages)
    return {"name": name, "caller_json": _legacy_json(caller), "response_json": _legacy_json(first),
            "trace": trace, "handler_calls": handled, "result_json": _legacy_json(results),
            "history_json": _legacy_json(_normalise_paths(history, root)),
            "events_json": _legacy_json(_normalise_paths(events, root)), "final": final,
            "error_class": error_class, "error_message": error_message,
            "transport_requests": len(requests), "requests_json": _legacy_json(_normalise_paths(requests, root))}


def provider_caller_observation_contracts() -> dict:
    if anthropic.__version__ != "0.107.1" or pydantic.__version__ != "2.13.4":
        raise RuntimeError("caller observation contracts require anthropic 0.107.1 and pydantic 2.13.4")
    profiles = _caller_profiles()
    forwarding = {"unknown-type", "root-mixed-list", "code-2025-id-nan", "unknown-extension-nonfinite"}
    agent_profiles = {"root-mixed-list", "code-2025-id-nan", "unknown-extension-nonfinite"}

    async def capture(root):
        rows, forwards = [], []
        for kind, fields in _blocks():
            for label, caller in profiles:
                message = _message([{"type": kind, **copy.deepcopy(fields), "caller": copy.deepcopy(caller)},
                                    {"type": "text", "text": "ready"}])
                for mode in ("direct", "stream"):
                    row, forwarded = await _capture_sdk(kind + "/" + label, mode, message,
                        forward=kind == "tool_use" and label in forwarding)
                    rows.append(row)
                    if forwarded is not None:
                        forwards.append(forwarded)
        runs = [await _capture_agent(name, copy.deepcopy(caller), root)
                for name, caller in profiles if name in agent_profiles]
        return rows, forwards, runs

    with patch.dict(os.environ, {}, clear=True), tempfile.TemporaryDirectory(prefix="mini-loop-caller-observation-") as scratch:
        rows, forwards, runs = asyncio.run(capture(Path(scratch).resolve()))
    sources = {"SDK.construct_type": construct_type, "SDK.BaseModel.construct": BaseModel.construct,
               "SDK._construct_field": _construct_field, "SDK.accumulate_event": accumulate_event,
               "_content_payload": _content_payload, "Agent.run": Agent.run, "Agent._loop": Agent._loop,
               "Agent._exec_tool_batch": Agent._exec_tool_batch, "Agent._exec_tool": Agent._exec_tool,
               "_tool_action_id": _tool_action_id, "Tool.run": Tool.run, "HTTPX.encode_json": encode_json}
    schemas = {cls.__name__: cls for cls in (DirectCaller, ServerToolCaller, ServerToolCaller20260120,
        ToolUseBlock, ServerToolUseBlock, WebSearchToolResultBlock, WebFetchToolResultBlock, ParsedMessage)}
    fixture = {"sdk_version": anthropic.__version__, "pydantic_version": pydantic.__version__,
               "httpx_version": httpx.__version__,
               "source_sha256": {name: hashlib.sha256(inspect.getsource(fn).encode()).hexdigest()
                                 for name, fn in sources.items()},
               "sdk_schema_sha256": {name: hashlib.sha256(Path(inspect.getfile(cls)).read_bytes()).hexdigest()
                                     for name, cls in schemas.items()},
               "scope": ["Actual default SDK HTTP construction and get_final_message; no strict-validation override or parser replacement.",
                         "Four declared outer block kinds; caller JSON roots/discriminators and nonfinite observations remain inert metadata.",
                         "Raw response, normalized content, requests and histories use legacy JSON strings inside a finite fixture document.",
                         "Forwarding calls the actual SDK again with a paired synthetic tool result; transport counts distinguish request encoding failure from an HTTP exchange.",
                         "Three actual Agent.run loops use a safe custom handler and the ordinary permission/recovery/transport paths; no shell or network.",
                         "Environment is temporarily cleared and restored; scratch paths normalize to <scratch>, run message identity is fixed after public peer construction.",
                         "Event selection omits nondeterministic spans/timing, preserving actual order, tool IDs, action IDs and error/output fields."],
               "profile_names": [name for name, _ in profiles], "block_kinds": [kind for kind, _ in _blocks()],
               "modes": ["direct", "stream"], "cases": rows, "forward_cases": forwards, "agent_runs": runs}
    json.dumps(fixture, allow_nan=False)
    return fixture
