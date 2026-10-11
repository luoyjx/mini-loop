"""Pinned SDK per-delta input accumulation, including EOF and invalid syntax."""
import asyncio
import hashlib
import inspect
import json
from importlib.metadata import version

import anthropic
import httpx
from anthropic.lib.streaming._messages import accumulate_event
from mini_loop.agent import _content_payload


def provider_partial_input_contracts() -> dict:
    if anthropic.__version__ != "0.107.1":
        raise RuntimeError("partial input contracts require anthropic 0.107.1")
    if version("jiter") != "0.15.0":
        raise RuntimeError("partial input contracts require jiter 0.15.0")
    profiles = [
        ("no-deltas", []), ("empty-delta", [""]), ("empty-twice", ["", ""]),
        ("complete", ['{"command":"true","keep":1}']),
        ("open-object", ["{"]), ("open-object-space", ["{ "]),
        ("complete-member-open-object", ['{"command":"true","keep":1']),
        ("trailing-comma-eof", ['{"command":"true","keep":1,']),
        ("partial-key", ['{"command":"true","keep":1,"dro']),
        ("missing-colon", ['{"command":"true","keep":1,"drop"']),
        ("missing-value", ['{"command":"true","keep":1,"drop":']),
        ("partial-string", ['{"command":"true","keep":1,"drop":"unfinished']),
        ("partial-escape", ['{"command":"true","keep":1,"drop":"unfinished\\']),
        ("partial-unicode", ['{"command":"true","keep":1,"drop":"\\u12']),
        ("partial-array", ['{"command":"true","values":[1,true,"unfinished']),
        ("partial-array-literal", ['{"command":"true","values":[1,tru']),
        ("partial-nested-object", ['{"command":"true","nested":{"keep":1,"drop":"unfinished']),
        ("partial-true", ['{"command":"true","keep":1,"drop":tru']),
        ("partial-false", ['{"command":"true","keep":1,"drop":fals']),
        ("partial-null", ['{"command":"true","keep":1,"drop":nul']),
        ("partial-negative", ['{"command":"true","keep":1,"drop":-']),
        ("partial-fraction", ['{"command":"true","keep":1,"drop":1.']),
        ("partial-exponent", ['{"command":"true","keep":1,"drop":1e+']),
        ("complete-exponent-eof", ['{"command":"true","value":1e2']),
        ("large-integer-eof", ['{"command":"true","value":9223372036854775808123']),
        ("duplicate-partial", ['{"command":"true","keep":1,"keep":"unfinished']),
        ("duplicate-complete", ['{"command":"true","keep":1,"keep":2']),
        ("completed-two-chunks", ['{"command":"true","keep":"un', 'finished"}']),
        ("completed-many-chunks", list('{"command":"true","keep":1}')),
        ("empty-first", ["", '{"command":"true"}']),
        ("empty-middle", ['{', "", '"command":"true"}']),
        ("empty-last", ['{"command":"true"}', ""]),
        ("invalid-repaired-later", ['{"command":"true",?', '"keep":1}']),
        ("invalid-literal", ['{"command":"true","keep":truth}']),
        ("invalid-number", ['{"command":"true","keep":01}']),
        ("invalid-trailing-comma", ['{"command":"true",}']),
        ("invalid-unicode", ['{"command":"true","keep":"\\u12xz"}']),
        ("invalid-control", ['{"command":"true","keep":"a\nb"}']),
        ("invalid-delimiter", ['{"command":"true"]']),
        ("trailing-garbage", ['{"command":"true"}garbage']),
        ("whitespace-only", [" "]),
        ("root-array", ['[1,true]']), ("root-null", ['null']),
        ("root-string", ['"hello"']), ("root-integer", ['17']),
        ("root-boolean", ['true']), ("root-partial-string", ['"unfinished']),
        ("nonfinite-nan", ['{"command":"true","keep":NaN}']),
        ("nonfinite-infinity", ['{"command":"true","keep":Infinity}']),
    ]

    async def capture():
        rows = []
        for kind, tool_name in [("tool_use", "bash"), ("server_tool_use", "web_search")]:
            for name, chunks in profiles:
                block = {"type": kind, "id": "tool", "name": tool_name, "input": {"command": "true"}}
                message = {"id": "fixture", "type": "message", "role": "assistant", "model": "fixture-model", "content": [], "stop_reason": None, "stop_sequence": None, "usage": {"input_tokens": 1, "output_tokens": 0}}
                events = [
                    ("message_start", {"type": "message_start", "message": message}),
                    ("content_block_start", {"type": "content_block_start", "index": 0, "content_block": block}),
                    *(("content_block_delta", {"type": "content_block_delta", "index": 0, "delta": {"type": "input_json_delta", "partial_json": chunk}}) for chunk in chunks),
                    ("content_block_stop", {"type": "content_block_stop", "index": 0}),
                    ("message_delta", {"type": "message_delta", "delta": {"stop_reason": "tool_use", "stop_sequence": None}, "usage": {"output_tokens": 2}}),
                    ("message_stop", {"type": "message_stop"}),
                ]
                sse = "".join(f"event: {event}\ndata: {json.dumps(payload)}\n\n" for event, payload in events)
                row = {"kind": kind, "name": name, "chunks": chunks, "sse": sse, "snapshots_json": []}

                def transport(request):
                    return httpx.Response(200, text=sse, headers={"content-type": "text/event-stream"}, request=request)

                async with httpx.AsyncClient(transport=httpx.MockTransport(transport)) as http:
                    async with anthropic.AsyncAnthropic(api_key="fixture-key", http_client=http, max_retries=0) as client:
                        try:
                            async with client.messages.stream(model="fixture-model", max_tokens=8, messages=[{"role": "user", "content": "hello"}]) as stream:
                                async for event in stream:
                                    if event.type == "content_block_delta":
                                        row["snapshots_json"].append(json.dumps(_content_payload(stream.current_message_snapshot.content), ensure_ascii=True, sort_keys=True))
                                result = await stream.get_final_message()
                            row["expected_json"] = json.dumps(_content_payload(result.content), ensure_ascii=True, sort_keys=True)
                        except ValueError as error:
                            row["error"] = {"class": type(error).__name__, "message": str(error)}
                rows.append(row)
        return rows

    return {"sdk_version": anthropic.__version__, "jiter_version": version("jiter"), "accumulate_event_sha256": hashlib.sha256(inspect.getsource(accumulate_event).encode()).hexdigest(), "content_payload_sha256": hashlib.sha256(inspect.getsource(_content_payload).encode()).hexdigest(), "cases": asyncio.run(capture())}
