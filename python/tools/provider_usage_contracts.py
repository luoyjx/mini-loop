"""Pinned real SDK usage normalization at the repository's event boundary."""
from __future__ import annotations

import asyncio
import hashlib
import inspect
import json
import sys

import pydantic

import anthropic
import httpx
from anthropic.lib.streaming._messages import accumulate_event
from anthropic.types import Usage
from mini_loop.agent import _usage_payload


def provider_usage_contracts() -> dict:
    if anthropic.__version__ != "0.107.1":
        raise RuntimeError("usage contracts require anthropic 0.107.1")
    core = {"input_tokens": 13, "output_tokens": 5}
    profiles = [
        {"name": "minimal", "usage": core, "stream": True},
        {"name": "explicit-null", "usage": {**core, **{key: None for key in Usage.model_fields if key not in core}}},
        {"name": "nested", "stream": True, "usage": {**core,
            "cache_creation_input_tokens": 17, "cache_read_input_tokens": 19,
            "cache_creation": {"ephemeral_1h_input_tokens": 7, "ephemeral_5m_input_tokens": 10},
            "inference_geo": "us", "output_tokens_details": {"thinking_tokens": 3},
            "server_tool_use": {"web_fetch_requests": 2, "web_search_requests": 4},
            "service_tier": "priority"}},
        {"name": "future-extension", "usage": {**core, "future_counter": {"generation": 8}}},
        {"name": "scalar-strings", "usage": {"input_tokens": "13", "output_tokens": "5", "cache_creation_input_tokens": "17"}},
    ]

    async def capture():
        rows = []
        for profile in profiles:
            for mode in (["direct", "stream"] if profile.get("stream") else ["direct"]):
                requests = []
                body = {"id": "fixture", "type": "message", "role": "assistant", "model": "fixture-model",
                    "content": [{"type": "text", "text": "ok"}], "stop_reason": "end_turn",
                    "stop_sequence": None, "usage": profile["usage"]}

                async def transport(request):
                    requests.append(json.loads(request.content))
                    if mode == "direct":
                        return httpx.Response(200, json=body)
                    start = {**body, "content": [], "stop_reason": None,
                        "usage": {**profile["usage"], "output_tokens": 0}}
                    events = [
                        ("message_start", {"type": "message_start", "message": start}),
                        ("content_block_start", {"type": "content_block_start", "index": 0, "content_block": {"type": "text", "text": ""}}),
                        ("content_block_delta", {"type": "content_block_delta", "index": 0, "delta": {"type": "text_delta", "text": "ok"}}),
                        ("content_block_stop", {"type": "content_block_stop", "index": 0}),
                        ("message_delta", {"type": "message_delta", "delta": {"stop_reason": "end_turn", "stop_sequence": None}, "usage": {"output_tokens": 5}}),
                        ("message_stop", {"type": "message_stop"}),
                    ]
                    text = "".join(f"event: {name}\ndata: {json.dumps(value)}\n\n" for name, value in events)
                    return httpx.Response(200, headers={"content-type": "text/event-stream"}, text=text)

                async with anthropic.AsyncAnthropic(api_key="fixture-key", base_url="https://fixture.invalid", max_retries=0,
                        http_client=httpx.AsyncClient(transport=httpx.MockTransport(transport))) as client:
                    kwargs = {"model": "fixture-model", "max_tokens": 8, "messages": [{"role": "user", "content": "hello"}]}
                    if mode == "direct":
                        response = await client.messages.create(**kwargs)
                    else:
                        async with client.messages.stream(**kwargs) as stream:
                            async for _ in stream:
                                pass
                            response = await stream.get_final_message()
                    rows.append({"name": profile["name"], "mode": mode, "wire_usage": profile["usage"],
                        "request": requests[0], "event_usage": _usage_payload(response)})
        return rows

    return {"sdk_version": anthropic.__version__, "pydantic_version": pydantic.__version__,
        "python_version": sys.version.split()[0], "usage_fields": list(Usage.model_fields),
        "source_hashes": {symbol.__qualname__: hashlib.sha256(inspect.getsource(symbol).encode()).hexdigest()
            for symbol in [_usage_payload, accumulate_event, Usage]}, "cases": asyncio.run(capture())}
