"""Pinned SDK server-tool-use response and repository transcript projections."""
import asyncio
import hashlib
import inspect
import json

import anthropic
import httpx

from mini_loop.agent import _content_payload


def provider_server_tool_contracts() -> dict:
    if anthropic.__version__ != "0.107.1":
        raise RuntimeError("server tool contracts require anthropic 0.107.1")

    async def capture():
        rows = []
        profiles = [(name, {"name": name, "input": {}}) for name in (
            "web_search", "web_fetch", "code_execution", "bash_code_execution",
            "text_editor_code_execution", "tool_search_tool_regex", "tool_search_tool_bm25")]
        profiles += [
            ("nested", {"name": "web_search", "input": {"query": "名称 query", "nested": [None, True, 7, 1.5, {"key": "value"}]}}),
            ("caller-null", {"name": "web_fetch", "input": {}, "caller": None}),
            ("caller-direct", {"name": "web_search", "input": {}, "caller": {"type": "direct"}}),
            ("caller-code-2025", {"name": "web_search", "input": {}, "caller": {"type": "code_execution_20250825", "tool_id": "parent"}}),
            ("caller-code-2026", {"name": "web_search", "input": {}, "caller": {"type": "code_execution_20260120", "tool_id": "parent"}}),
            ("future-name", {"name": "future_server_tool", "input": {"future": 17}}),
            ("extensions", {"name": "web_search", "input": {"future": {"secret-key": "secret-value"}}, "future": {"root": "metadata"}}),
            ("initial-replaced", {"name": "web_search", "input": {"query": "final query"}}),
            ("empty-delta", {"name": "web_search", "input": {"query": "retained initial"}}),
        ]
        for name, fields in profiles:
            for mode in ("direct", "stream"):
                content = [{"type": "server_tool_use", "id": "server_search", **fields}, {"type": "text", "text": "ready"}]
                message = {"id": "fixture", "type": "message", "role": "assistant", "model": "fixture-model", "content": content, "stop_reason": "end_turn", "stop_sequence": None, "usage": {"input_tokens": 1, "output_tokens": 2}}
                streamed = content[0].copy()
                streamed["input"] = {"ignored": "initial"} if name == "initial-replaced" else {}
                chunks = []
                if fields["input"]:
                    raw = json.dumps(fields["input"], ensure_ascii=False)
                    chunks = [("content_block_delta", {"type": "content_block_delta", "index": 0, "delta": {"type": "input_json_delta", "partial_json": raw[:len(raw)//2]}}),
                              ("content_block_delta", {"type": "content_block_delta", "index": 0, "delta": {"type": "input_json_delta", "partial_json": raw[len(raw)//2:]}})]
                if name == "empty-delta":
                    streamed["input"] = fields["input"]
                    chunks = [("content_block_delta", {"type": "content_block_delta", "index": 0, "delta": {"type": "input_json_delta", "partial_json": ""}})]
                events = [
                    ("message_start", {"type": "message_start", "message": {**message, "content": [], "stop_reason": None, "usage": {"input_tokens": 1, "output_tokens": 0}}}),
                    ("content_block_start", {"type": "content_block_start", "index": 0, "content_block": streamed}),
                    *chunks,
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
