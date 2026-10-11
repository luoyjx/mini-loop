"""Pinned SDK bash-execution-result response and repository transcript projections."""
import asyncio
import hashlib
import inspect
import json

import anthropic
import httpx

from mini_loop.agent import _content_payload


def provider_bash_execution_contracts() -> dict:
    if anthropic.__version__ != "0.107.1":
        raise RuntimeError("bash execution contracts require anthropic 0.107.1")

    async def capture():
        rows = []
        output = {"type": "bash_code_execution_output", "file_id": "file-名称"}
        result = {"type": "bash_code_execution_result", "content": [], "return_code": 0, "stdout": "output 名称", "stderr": ""}
        profiles = [
            ("empty-files", {"content": result}),
            ("files", {"content": {**result, "content": [output, {**output, "file_id": "second"}]}}),
            ("negative-code", {"content": {**result, "return_code": -9, "stderr": "terminated"}}),
            ("large-code", {"content": {**result, "return_code": 2**80+17}}),
            ("negative-large-code", {"content": {**result, "return_code": -(2**80+17)}}),
            ("empty-text", {"content": {**result, "stdout": "", "stderr": ""}}),
            ("extensions", {"content": {**result, "content": [{**output, "future": {"file": "metadata"}}], "future": 8}, "future": {"root": "metadata"}}),
            ("future-error", {"content": {"type": "bash_code_execution_tool_result_error", "error_code": "future_code", "future": 17}}),
        ]
        profiles += [(code, {"content": {"type": "bash_code_execution_tool_result_error", "error_code": code}}) for code in (
            "invalid_tool_input", "unavailable", "too_many_requests", "execution_time_exceeded", "output_file_too_large")]
        for name, fields in profiles:
            for mode in ("direct", "stream"):
                content = [{"type": "bash_code_execution_tool_result", "tool_use_id": "server_bash", **fields}, {"type": "text", "text": "ready"}]
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
