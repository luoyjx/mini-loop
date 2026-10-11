"""Actual SDK whole-usage construction, stream faults and repository consumers."""
from __future__ import annotations

import asyncio
import hashlib
import inspect
import json
import sys

import anthropic
import httpx
import pydantic
from anthropic.lib.streaming._messages import accumulate_event
from anthropic.types import Usage
from mini_loop.agent import _usage_payload
from mini_loop.metering import TokenMeter, prompt_tokens


def provider_whole_usage_contracts() -> dict:
    if anthropic.__version__ != "0.107.1":
        raise RuntimeError("whole usage contracts require anthropic 0.107.1")
    profiles = [
        ("absent", False, None), ("null", True, None),
        ("integer", True, 13), ("fraction", True, 13.5),
        ("boolean", True, True), ("text", True, "secret-marker"),
        ("empty-array", True, []), ("array-model", True, [{"input_tokens": 13}]),
        ("nested-array", True, [[{"input_tokens": 13}]]),
        ("empty-object", True, {}), ("input-only", True, {"input_tokens": 13}),
        ("output-only", True, {"output_tokens": 5}),
        ("extension-only", True, {"secret-marker": [13, "secret-marker"]}),
        ("complete", True, {"input_tokens": 13, "output_tokens": 5}),
        ("array-text-counts", True, [{"input_tokens": "13", "output_tokens": "5", "cache_read_input_tokens": "7"}]),
        ("array-nested-models", True, [{"cache_creation": {"ephemeral_1h_input_tokens": "7"}, "output_tokens_details": {"thinking_tokens": "3"}, "server_tool_use": {"web_fetch_requests": "2", "web_search_requests": "4"}}]),
        ("array-metadata-list", True, [{"cache_creation": [{"ephemeral_1h_input_tokens": "7"}]}]),
        ("array-extensions", True, [{"secret-marker": {"quote": "a'\\b", "flag": True}, "inference_geo": "secret-marker"}]),
        ("mixed-array", True, [None, 13, "secret-marker", {"input_tokens": 13}, [True, {}]]),
    ]

    def observation(response):
        usage = getattr(response, "usage", None)
        meter = TokenMeter()
        messages = [{"role": "user", "content": "hello"}]
        observed = meter.observe(usage, messages, envelope="fixture")
        return {"event_usage": _usage_payload(response),
                "usage_present": hasattr(response, "usage"),
                "usage_type": type(usage).__name__,
                "consumer": {"prompt_tokens": prompt_tokens(usage), "observed": observed,
                             "anchor": meter.anchor, "observations": meter.observations}}

    async def capture():
        rows = []
        for name, present, usage in profiles:
            for mode in ("direct", "stream"):
                body = {"id": "fixture", "type": "message", "role": "assistant",
                        "model": "fixture-model", "content": [],
                        "stop_reason": "end_turn", "stop_sequence": None}
                if present:
                    body["usage"] = usage
                start = {**body, "stop_reason": None}
                events = [("message_start", {"type": "message_start", "message": start}),
                          ("message_delta", {"type": "message_delta", "delta": {
                              "stop_reason": "end_turn", "stop_sequence": None},
                              "usage": {"output_tokens": 5}}),
                          ("message_stop", {"type": "message_stop"})]
                sse = "".join(f"event: {event}\ndata: {json.dumps(value)}\n\n"
                              for event, value in events)
                row = {"name": name, "mode": mode, "wire_usage_present": present,
                       "response_json": json.dumps(body, separators=(",", ":")),
                       "usage_stages": [], "phase": "request"}
                if mode == "stream":
                    row["sse"] = sse

                async def transport(request):
                    row["request"] = json.loads(request.content)
                    return (httpx.Response(200, json=body) if mode == "direct" else
                            httpx.Response(200, headers={"content-type": "text/event-stream"}, text=sse))

                async with anthropic.AsyncAnthropic(api_key="fixture-key", max_retries=0,
                        base_url="https://fixture.invalid", http_client=httpx.AsyncClient(
                            transport=httpx.MockTransport(transport))) as client:
                    kwargs = {"model": "fixture-model", "max_tokens": 8,
                              "messages": [{"role": "user", "content": "hello"}]}
                    try:
                        if mode == "direct":
                            response = await client.messages.create(**kwargs)
                        else:
                            async with client.messages.stream(**kwargs) as stream:
                                row["phase"] = "message_start"
                                async for event in stream:
                                    if event.type in {"message_start", "message_delta"}:
                                        row["usage_stages"].append({"event": event.type,
                                            **observation(stream.current_message_snapshot)})
                                        row["phase"] = ("message_delta" if event.type == "message_start"
                                                        else "message_stop")
                                response = await stream.get_final_message()
                        row["phase"] = "final_projection"
                        row.update(observation(response))
                        row["outcome"] = "completed"
                    except (AttributeError, TypeError, ValueError, OverflowError) as exc:
                        row["outcome"] = "error"
                        row["error"] = {"class": type(exc).__name__, "message": str(exc)}
                rows.append(row)
        return rows

    symbols = (Usage, accumulate_event, _usage_payload, prompt_tokens, TokenMeter.observe)
    return {"sdk_version": anthropic.__version__, "pydantic_version": pydantic.__version__,
            "python_version": sys.version.split()[0],
            "source_hashes": {symbol.__qualname__: hashlib.sha256(
                inspect.getsource(symbol).encode()).hexdigest() for symbol in symbols},
            "cases": asyncio.run(capture())}
