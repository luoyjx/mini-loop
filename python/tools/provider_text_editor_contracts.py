"""Pinned SDK text-editor-result response and repository transcript projections."""
import asyncio
import hashlib
import inspect
import json

import anthropic
import httpx

from mini_loop.agent import _content_payload


def provider_text_editor_contracts() -> dict:
    if anthropic.__version__ != "0.107.1":
        raise RuntimeError("text editor contracts require anthropic 0.107.1")

    async def capture():
        rows = []
        view = {"type": "text_editor_code_execution_view_result", "file_type": "text", "content": "line 名称"}
        replace = {"type": "text_editor_code_execution_str_replace_result"}
        profiles = [
            ("view-text", {"content": view}),
            ("view-image", {"content": {**view, "file_type": "image", "content": "image payload"}}),
            ("view-pdf", {"content": {**view, "file_type": "pdf", "content": "pdf payload"}}),
            ("view-numbers", {"content": {**view, "num_lines": 3, "start_line": 0, "total_lines": 12}}),
            ("view-large", {"content": {**view, "num_lines": 2**80+17, "start_line": -(2**80+17), "total_lines": 2**80+19}}),
            ("view-nulls", {"content": {**view, "num_lines": None, "start_line": None, "total_lines": None}}),
            ("create", {"content": {"type": "text_editor_code_execution_create_result", "is_file_update": False}}),
            ("update", {"content": {"type": "text_editor_code_execution_create_result", "is_file_update": True}}),
            ("replace-default", {"content": replace}),
            ("replace-lines", {"content": {**replace, "lines": ["before 名称", "after"], "new_lines": 2, "new_start": 0, "old_lines": 1, "old_start": -1}}),
            ("replace-empty-lines", {"content": {**replace, "lines": []}}),
            ("replace-nulls", {"content": {**replace, "lines": None, "new_lines": None, "new_start": None, "old_lines": None, "old_start": None}}),
            ("replace-large", {"content": {**replace, "new_lines": 2**80+17, "new_start": -(2**80+17), "old_lines": 2**80+19, "old_start": -(2**80+19)}}),
            ("error-message", {"content": {"type": "text_editor_code_execution_tool_result_error", "error_code": "file_not_found", "error_message": "missing 名称"}}),
            ("future-error", {"content": {"type": "text_editor_code_execution_tool_result_error", "error_code": "future_code", "future": 17}}),
            ("view-extensions", {"content": {**view, "future": {"view": "metadata"}}, "future": {"root": "metadata"}}),
            ("replace-extensions", {"content": {**replace, "future": 19}}),
            ("create-extensions", {"content": {"type": "text_editor_code_execution_create_result", "is_file_update": False, "future": 20}}),
            ("caller-extension", {"content": view, "caller": {"type": "future_metadata", "value": "metadata"}}),
        ]
        profiles += [(code, {"content": {"type": "text_editor_code_execution_tool_result_error", "error_code": code}}) for code in (
            "invalid_tool_input", "unavailable", "too_many_requests", "execution_time_exceeded", "file_not_found")]
        for name, fields in profiles:
            for mode in ("direct", "stream"):
                content = [{"type": "text_editor_code_execution_tool_result", "tool_use_id": "server_editor", **fields}, {"type": "text", "text": "ready"}]
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
