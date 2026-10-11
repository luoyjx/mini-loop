"""Capture completed reply metadata through installed SDK direct and stream APIs."""
from __future__ import annotations

import asyncio
import copy
import hashlib
import inspect
import json
import unicodedata

import anthropic
import httpx
import pydantic
import pydantic_core
from anthropic._models import construct_type
from anthropic._utils._datetime_parse import parse_datetime, _from_unix_seconds, _parse_timezone
from anthropic.lib.streaming._messages import accumulate_event
from anthropic.types import Container, Message, RefusalStopDetails


def provider_reply_metadata_contracts() -> dict:
    if anthropic.__version__ != "0.107.1":
        raise RuntimeError("reply metadata contracts require anthropic 0.107.1")
    container = {"id": "container_α", "expires_at": "2030-01-02T03:04:05Z"}
    refusal = {"type": "refusal", "category": "cyber", "explanation": "说明"}
    profiles = [
        ("absent", {}, {}),
        ("null", {"container": None, "stop_details": None}, {}),
        ("container-utc", {"container": container}, {}),
        ("container-naive", {"container": {**container, "expires_at": "2030-01-02T03:04:05"}}, {}),
        ("container-fractional", {"container": {**container, "expires_at": "2030-01-02T03:04:05.1234Z"}}, {}),
        ("container-offset", {"container": {**container, "expires_at": "2030-01-02T03:04:05+08:00"}}, {}),
        ("container-empty", {"container": {}}, {}),
        ("container-epoch", {"container": {**container, "expires_at": 1893553445}}, {}),
        ("container-extra", {"container": {**container, "future": {"values": [0, None]}}}, {}),
        ("container-missing-expiry", {"container": {"id": "container_fixture"}}, {}),
        ("container-invalid-expiry", {"container": {**container, "expires_at": "not-a-date"}}, {}),
        ("container-scalar", {"container": "opaque"}, {}),
        ("refusal", {"stop_details": refusal}, {}),
        ("refusal-defaults", {"stop_details": {"type": "refusal"}}, {}),
        ("refusal-extra", {"stop_details": {**refusal, "future": [False, "x"]}}, {}),
        ("refusal-future-kind", {"stop_details": {"type": "future", "category": "future"}}, {}),
        ("refusal-empty", {"stop_details": {}}, {}),
        ("refusal-field-types", {"stop_details": {"type": 0, "category": ["future"], "explanation": False}}, {}),
        ("refusal-scalar", {"stop_details": ["opaque"]}, {}),
        ("delta-container", {"container": container}, {"container": {**container, "id": "replacement"}}),
        ("delta-refusal", {"stop_details": None}, {"stop_details": refusal}),
        ("delta-refusal-null", {"stop_details": refusal}, {"stop_details": None}),
    ]
    # These values pass through actual SDK construction and public serialization;
    # archive readers must not independently reinterpret their stored observations.
    expiry_profiles = [
        ("milliseconds", 1893553445123),
        ("fractional-epoch", 0.1234565),
        ("negative-fractional-epoch", -0.1234565),
        ("true", True), ("false", False),
        ("numeric-text", " 1.893553445e9 "),
        ("underscored-text", "1_893_553_445"),
        ("unicode-numeric-text", "١٨٩٣٥٥٣٤٤٥"),
        ("positive-infinity-text", "Infinity"),
        ("negative-infinity-text", "-inf"),
        ("nan-text", "NaN"),
        ("overflow", 3.1e20), ("underflow", -3.1e20),
        ("watershed", 20000000000),
        ("above-watershed", 20000000001),
        ("short-fields", "2030-1-2T3:4:5Z"),
        ("minute-only", "2030-01-02T03:04Z"),
        ("offset-minute-carry", "2030-01-02T03:04:05+01:60"),
        ("offset-compact", "2030-01-02T03:04:05-0130"),
        ("long-fraction", "2030-01-02T03:04:05.123456789012Z"),
        ("too-long-fraction", "2030-01-02T03:04:05.1234567890123Z"),
        ("unicode-date", "٢٠٣٠-٠١-٠٢T٠٣:٠٤:٠٥Z"),
        ("trailing-newline", "2030-01-02T03:04:05Z\n"),
        ("lowercase-zone", "2030-01-02T03:04:05z"),
        ("invalid-calendar", "2030-02-30T03:04:05Z"),
        ("invalid-offset", "2030-01-02T03:04:05+24:00"),
        ("fallback-list", [1]), ("fallback-object", {"future": 1}),
        ("date-only", "2030-01-02"),
        ("lowercase-separator", "2030-01-02t03:04:05z"),
        ("comma-fraction", "2030-01-02T03:04:05,1234567Z"),
        ("text-negative-fraction", "-0.1234565"),
        ("float-second-carry", -0.9999999),
        ("float-watershed-fraction", 20000000000.5),
        ("float-negative-watershed-fraction", -20000000000.5),
        ("text-overflow", "1e999"),
        ("hexadecimal-text", "0x1p2"),
        ("two-newlines", "2030-01-02T03:04:05Z\n\n"),
    ]
    profiles.extend(("expiry-" + name, {"container": {**container, "expires_at": value}}, {})
                    for name, value in expiry_profiles)
    profiles.extend(("expiry-fallback-" + name,
                     {"container": {"id": 0, "expires_at": value}}, {})
                    for name, value in expiry_profiles)

    async def capture():
        rows = []
        for name, initial, delta in profiles:
            for mode in ("direct", "stream"):
                message = {"id": "fixture", "type": "message", "role": "assistant", "model": "fixture-model", "content": [{"type": "text", "text": "ready"}], "stop_reason": "end_turn", "stop_sequence": None, "usage": {"input_tokens": 1, "output_tokens": 2}, **copy.deepcopy(initial), **copy.deepcopy(delta)}
                start = {**message, **copy.deepcopy(initial), "content": [], "stop_reason": None, "usage": {"input_tokens": 1, "output_tokens": 0}}
                # Delta-only metadata must not appear in the start snapshot.
                for field in delta:
                    if field not in initial:
                        start.pop(field, None)
                events = [
                    ("message_start", {"type": "message_start", "message": start}),
                    ("content_block_start", {"type": "content_block_start", "index": 0, "content_block": message["content"][0]}),
                    ("content_block_stop", {"type": "content_block_stop", "index": 0}),
                    ("message_delta", {"type": "message_delta", "delta": {"stop_reason": "end_turn", "stop_sequence": None, **copy.deepcopy(delta)}, "usage": {"output_tokens": 2}}),
                    ("message_stop", {"type": "message_stop"}),
                ]
                sse = "".join(f"event: {kind}\ndata: {json.dumps(value, ensure_ascii=False)}\n\n" for kind, value in events)

                def transport(request):
                    if mode == "direct":
                        return httpx.Response(200, json=message, request=request)
                    return httpx.Response(200, text=sse, headers={"content-type": "text/event-stream"}, request=request)

                async with httpx.AsyncClient(transport=httpx.MockTransport(transport)) as http:
                    async with anthropic.AsyncAnthropic(api_key="fixture-key", http_client=http, max_retries=0) as client:
                        request = {"model": "fixture-model", "max_tokens": 8, "messages": [{"role": "user", "content": "hello"}]}
                        if mode == "direct":
                            response = await client.messages.create(**request)
                        else:
                            async with client.messages.stream(**request) as stream:
                                response = await stream.get_final_message()
                payload = response.model_dump(mode="json", warnings=False)
                metadata = {field: payload[field] for field in ("container", "stop_details") if field in payload}
                json.dumps(metadata, allow_nan=False)
                rows.append({"name": name, "mode": mode, "message": message, "sse": sse, "expected_metadata": metadata})
        return rows

    return {
        "sdk_version": anthropic.__version__,
        "pydantic_version": pydantic.__version__,
        "pydantic_core_version": pydantic_core.__version__,
        "unicode_version": unicodedata.unidata_version,
        "source_sha256": {value.__name__: hashlib.sha256(inspect.getsource(value).encode()).hexdigest() for value in (Message, Container, RefusalStopDetails, accumulate_event, construct_type, parse_datetime, _from_unix_seconds, _parse_timezone)},
        "cases": asyncio.run(capture()),
    }
