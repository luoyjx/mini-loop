"""Pinned SDK web-fetch-result response and repository transcript projections."""
import asyncio
import hashlib
import inspect
import json

import anthropic
import httpx

from mini_loop.agent import _content_payload


def provider_web_fetch_contracts() -> dict:
    if anthropic.__version__ != "0.107.1":
        raise RuntimeError("web fetch contracts require anthropic 0.107.1")

    async def capture():
        rows = []
        source = {"type": "text", "media_type": "text/plain", "data": "文档 text"}
        document = {"type": "document", "source": source}
        result = {"type": "web_fetch_result", "url": "https://fixture.invalid", "content": document}
        profiles = [
            ("text", {"content": result}),
            ("pdf", {"content": {**result, "content": {**document, "source": {"type": "base64", "media_type": "application/pdf", "data": "JVBERi0="}}}}),
            ("empty-text", {"content": {**result, "content": {**document, "source": {**source, "data": ""}}}}),
            ("title-time", {"content": {**result, "retrieved_at": "2026-10-10T00:00:00Z", "content": {**document, "title": "标题"}}}),
            ("nulls", {"content": {**result, "retrieved_at": None, "content": {**document, "title": None, "citations": None}}, "caller": None}),
            ("citations-on", {"content": {**result, "content": {**document, "citations": {"enabled": True}}}}),
            ("citations-off", {"content": {**result, "content": {**document, "citations": {"enabled": False}}}}),
            ("direct-caller", {"content": result, "caller": {"type": "direct"}}),
            ("code-caller-2025", {"content": result, "caller": {"type": "code_execution_20250825", "tool_id": "parent"}}),
            ("code-caller-2026", {"content": result, "caller": {"type": "code_execution_20260120", "tool_id": "parent"}}),
            ("extensions", {"content": {**result, "future": 8, "content": {**document, "future": 9, "source": {**source, "future": {"source": "metadata"}}, "citations": {"enabled": True, "future": 10}}}, "future": {"root": "metadata"}}),
            ("future-error", {"content": {"type": "web_fetch_tool_result_error", "error_code": "future_code", "future": 7}}),
        ]
        profiles += [(code, {"content": {"type": "web_fetch_tool_result_error", "error_code": code}}) for code in (
            "invalid_tool_input", "url_too_long", "url_not_allowed", "url_not_in_prior_context",
            "url_not_accessible", "unsupported_content_type", "too_many_requests", "max_uses_exceeded", "unavailable")]
        for name, fields in profiles:
            for mode in ("direct", "stream"):
                content = [{"type": "web_fetch_tool_result", "tool_use_id": "server_fetch", **fields}, {"type": "text", "text": "ready"}]
                message = {"id": "fixture", "type": "message", "role": "assistant", "model": "fixture-model", "content": content, "stop_reason": "end_turn", "stop_sequence": None, "usage": {"input_tokens": 1, "output_tokens": 2}}
                events = [
                    ("message_start", {"type": "message_start", "message": {**message, "content": [], "stop_reason": None, "usage": {"input_tokens": 1, "output_tokens": 0}}}),
                    ("content_block_start", {"type": "content_block_start", "index": 0, "content_block": content[0]}),
                    ("content_block_stop", {"type": "content_block_stop", "index": 0}),
                    ("content_block_start", {"type": "content_block_start", "index": 1, "content_block": content[1]}),
                    ("content_block_stop", {"type": "content_block_stop", "index": 1}),
                    ("message_delta", {"type": "message_delta", "delta": {"stop_reason": "end_turn", "stop_sequence": None}, "usage": {"output_tokens": 2}}),
                    ("message_stop", {"type": "message_stop"}),
                ]
                sse = "".join(f"event: {kind}\ndata: {json.dumps(value)}\n\n" for kind, value in events)

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
                rows.append({"mode": mode, "name": name, "message": message, "sse": sse, "expected_content": _content_payload(response.content), "sdk_block_class": type(response.content[0]).__name__})
        return rows

    return {"sdk_version": anthropic.__version__, "content_payload_sha256": hashlib.sha256(inspect.getsource(_content_payload).encode()).hexdigest(), "cases": asyncio.run(capture())}
