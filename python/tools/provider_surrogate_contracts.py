"""Actual fixed-SDK escaped Unicode ingress and next-request observations."""

from __future__ import annotations

import asyncio
import copy
import hashlib
import inspect
import json
from pathlib import Path
import warnings

import anthropic
import httpx
import pydantic
from anthropic._models import construct_type
from anthropic.lib.streaming._messages import accumulate_event
from httpx._content import encode_json

from mini_loop.agent import Agent, _content_payload
from mini_loop.config import Settings
from mini_loop.registry import Hooks, ToolRegistry
from mini_loop.transport import DirectTransport, StreamingTransport


def _ascii(value) -> str:
    return json.dumps(value, ensure_ascii=True, separators=(",", ":"), allow_nan=False)


def _message(content):
    return {"id": "surrogate-reply", "type": "message", "role": "assistant",
            "model": "fixture-model", "content": content, "stop_reason": "end_turn",
            "stop_sequence": None, "usage": {"input_tokens": 1, "output_tokens": 2}}


def _profiles():
    high, low = "\ud800", "\udfff"
    tool = {"type": "tool_use", "id": "client", "name": "bash",
            "input": {"command": "true"}}
    return [
        ("text-high", {"type": "text", "text": "before" + high + "after"}),
        ("text-low", {"type": "text", "text": "before" + low + "after"}),
        ("text-separated", {"type": "text", "text": high + "-" + low}),
        ("text-pair-control", {"type": "text", "text": "\ud83d\ude00"}),
        ("text-replacement-control", {"type": "text", "text": "before\ufffdafter"}),
        ("text-unicode-control", {"type": "text", "text": "中文🌱\n\t\u0000"}),
        ("thinking-high", {"type": "thinking", "thinking": "thought" + high,
                           "signature": "signed"}),
        ("signature-low", {"type": "thinking", "thinking": "thought",
                           "signature": "signed" + low}),
        ("redacted-high", {"type": "redacted_thinking", "data": "opaque" + high}),
        ("upload-low", {"type": "container_upload", "file_id": "file" + low}),
        ("bash-command-high", {**tool, "input": {"command": "echo " + high}}),
        ("bash-extra-value-low", {**tool, "input": {"command": "true", "future": low}}),
        ("bash-extra-key-high", {**tool, "input": {"command": "true", high: "metadata"}}),
        ("server-input-low", {"type": "server_tool_use", "id": "server",
                              "name": "web_search", "input": {"query": low}}),
        ("caller-id-high", {**tool, "caller": {"type": "code_execution_20260120",
                                               "tool_id": "parent" + high}}),
        ("caller-extension-key-low", {**tool, "caller": {"type": "direct", low: high}}),
        ("caller-root-low", {**tool, "caller": low}),
    ]


def _stream(message):
    """Valid SSE frames; JSON escaping is the real response's wire encoding."""
    events = [("message_start", {"type": "message_start", "message": {
        **message, "content": [], "stop_reason": None,
        "usage": {"input_tokens": 1, "output_tokens": 0}}})]
    for index, original in enumerate(message["content"]):
        initial, deltas = copy.deepcopy(original), []
        if original["type"] == "text":
            initial["text"] = ""
            deltas.append({"type": "text_delta", "text": original["text"]})
        elif original["type"] == "thinking":
            initial["thinking"], initial["signature"] = "", ""
            deltas.extend([{"type": "thinking_delta", "thinking": original["thinking"]},
                           {"type": "signature_delta", "signature": original["signature"]}])
        elif "input" in original:
            initial["input"] = {}
            encoded = _ascii(original["input"])
            # Divide in the middle, including inside an escape when the input
            # happens to place one there. The real SDK owns partial parsing.
            midpoint = len(encoded) // 2
            deltas.extend({"type": "input_json_delta", "partial_json": part}
                          for part in (encoded[:midpoint], encoded[midpoint:]))
        events.append(("content_block_start", {"type": "content_block_start", "index": index,
                                               "content_block": initial}))
        events.extend(("content_block_delta", {"type": "content_block_delta", "index": index,
                                               "delta": delta}) for delta in deltas)
        events.append(("content_block_stop", {"type": "content_block_stop", "index": index}))
    events.extend([
        ("message_delta", {"type": "message_delta", "delta": {
            "stop_reason": message["stop_reason"], "stop_sequence": None},
            "usage": {"output_tokens": 2}}),
        ("message_stop", {"type": "message_stop"}),
    ])
    return "".join(f"event: {kind}\ndata: {_ascii(value)}\n\n" for kind, value in events)


async def _capture(root: Path, name: str, mode: str, block: dict) -> dict:
    message = _message([copy.deepcopy(block)])
    response_json, sse = _ascii(message), _stream(message)
    requests, progress = [], []

    def transport(request):
        requests.append({"method": request.method, "path": request.url.raw_path.decode(),
                         "body_json": _ascii(json.loads(request.content))})
        content = response_json if mode == "direct" else sse
        if len(requests) > 1:
            done = _message([{"type": "text", "text": "done"}])
            content = _ascii(done) if mode == "direct" else _stream(done)
        return httpx.Response(200, content=content.encode("ascii"), request=request,
                              headers={"content-type": "application/json" if mode == "direct"
                                       else "text/event-stream"})

    async def emit(event):
        if event["type"] == "assistant_delta":
            progress.append(event["text"].encode("utf-8", "surrogatepass").hex())

    configured = Settings(fake_llm=True, enable_features=False, model="fixture-model",
                          workspace_root=root, skills_dir=root / "empty-skills",
                          trajectory_enabled=False, spill_dir=None)
    row = {"name": name, "mode": mode, "response_json": response_json, "sse": sse,
           "ingress_succeeded": False, "sdk_reply_obtained": False, "ingress_error_class": None,
           "ingress_error_message": None, "content_json": None,
           "forward_messages_json": None, "forward_succeeded": None,
           "forward_error_class": None, "forward_error_message": None,
           "failure_phase": None}
    with warnings.catch_warnings(record=True) as diagnostics:
        warnings.simplefilter("always")
        async with httpx.AsyncClient(transport=httpx.MockTransport(transport)) as http:
            async with anthropic.AsyncAnthropic(api_key="fixture-key", base_url="https://fixture.invalid",
                                               http_client=http, max_retries=0) as client:
                selected = DirectTransport() if mode == "direct" else StreamingTransport(
                    coalesce_chars=1, coalesce_seconds=3600)
                agent = Agent(client=client, settings=configured, workspace=root / name / mode,
                              tools=ToolRegistry([]), hooks=Hooks([]), system="Unicode wire fixture.",
                              transport=selected, emit=emit)
                initial = {"model": "fixture-model", "max_tokens": 8,
                           "messages": [{"role": "user", "content": "capture"}]}
                phase = "reply_ingress"
                try:
                    response = await selected.send(agent, initial)
                    row["sdk_reply_obtained"] = True
                    phase = "reply_snapshot"
                    content = _content_payload(response.content)
                except Exception as error:
                    row.update(ingress_error_class=type(error).__name__,
                               ingress_error_message=str(error), failure_phase=phase)
                else:
                    row.update(ingress_succeeded=True, content_json=_ascii(content),
                               sdk_block_classes=[type(value).__name__ for value in response.content])
                    if block["type"] == "tool_use":
                        continuation = [{"type": "tool_result", "tool_use_id": block["id"],
                                         "content": "safe fixture result"}]
                    else:
                        continuation = "continue"
                    history = [*initial["messages"], {"role": "assistant", "content": content},
                               {"role": "user", "content": continuation}]
                    row["forward_messages_json"] = _ascii(history)
                    before_forward = len(requests)
                    try:
                        await selected.send(agent, {**initial, "messages": history})
                    except Exception as error:
                        row.update(forward_succeeded=False, forward_error_class=type(error).__name__,
                                   forward_error_message=str(error), failure_phase=(
                                       "request_encode" if len(requests) == before_forward else "next_reply_ingress"))
                    else:
                        row["forward_succeeded"] = True
                row["retained_content_json"] = row["content_json"] if not row["ingress_succeeded"] else _ascii(
                    _content_payload(response.content))
    row.update(transport_requests=len(requests), requests=requests,
               progress_hex=progress, warning_count=len(diagnostics))
    return row


def provider_surrogate_contracts(scratch: Path) -> dict:
    if anthropic.__version__ != "0.107.1" or pydantic.__version__ != "2.13.4":
        raise RuntimeError("surrogate contracts require anthropic 0.107.1 / pydantic 2.13.4")
    scratch = Path(scratch).resolve()

    async def capture():
        return [await _capture(scratch, name, mode, block)
                for name, block in _profiles() for mode in ("direct", "stream")]

    result = {"sdk_version": anthropic.__version__, "pydantic_version": pydantic.__version__,
              "httpx_version": httpx.__version__, "profile_names": [name for name, _ in _profiles()],
              "source_sha256": {name: hashlib.sha256(inspect.getsource(value).encode()).hexdigest()
                  for name, value in {"DirectTransport.send": DirectTransport.send,
                      "StreamingTransport.send": StreamingTransport.send,
                      "_content_payload": _content_payload, "SDK.construct_type": construct_type,
                      "SDK.accumulate_event": accumulate_event,
                      "HTTPX.encode_json": encode_json, "HTTPX.Response.json": httpx.Response.json}.items()},
              "scope": ["Actual AsyncAnthropic HTTP ingress via production DirectTransport/StreamingTransport and a real constructed Agent; no custom SDK parser or validation override.",
                        "ASCII JSON response bytes contain escaped high/low surrogates; valid pairs, literal U+FFFD and Unicode controls stay separate.",
                        "The actual subsequent SDK call receives captured content and paired synthetic tool results where required; no shell/tool handler executes.",
                        "Raw response/SSE/content/history use JSON strings within the finite fixture; progress uses surrogatepass hex to preserve emitted code points.",
                        "IDNA/native hostname adoption and arbitrary SDK block/metadata shapes are outside these finite recipes."],
              "cases": asyncio.run(capture())}
    json.dumps(result, allow_nan=False)
    return result
