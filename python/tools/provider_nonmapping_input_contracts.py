"""Actual SDK direct response and forwarding of finite non-mapping tool inputs."""
from __future__ import annotations

import asyncio
import hashlib
import inspect
import json

import anthropic
import httpx
from anthropic.types import ToolUseBlock, ServerToolUseBlock


def provider_nonmapping_input_contracts() -> dict:
    if anthropic.__version__ != "0.107.1":
        raise RuntimeError("non-mapping contracts require anthropic 0.107.1")

    async def capture():
        rows = []
        for kind in ("tool_use", "server_tool_use"):
            for name, value in [("array", [1, True]), ("null", None),
                    ("text", "hello"), ("integer", 17), ("float", 13.75), ("boolean", True)]:
                block = {"type": kind, "id": "call", "name": "bash" if kind == "tool_use" else "web_search", "input": value}
                body = {"id": "message", "type": "message", "role": "assistant", "model": "fixture-model",
                    "content": [block], "stop_reason": "end_turn", "stop_sequence": None,
                    "usage": {"input_tokens": 1, "output_tokens": 0}}
                requests = []

                async def transport(request):
                    requests.append(json.loads(request.content))
                    return httpx.Response(200, json=body)

                async with anthropic.AsyncAnthropic(api_key="fixture-key", base_url="https://fixture.invalid", max_retries=0,
                        http_client=httpx.AsyncClient(transport=httpx.MockTransport(transport))) as client:
                    response = await client.messages.create(model="fixture-model", max_tokens=8,
                        messages=[{"role": "user", "content": "hello"}])
                    content = [item.model_dump() for item in response.content]
                    await client.messages.create(model="fixture-model", max_tokens=8,
                        messages=[{"role": "assistant", "content": content}])
                    rows.append({"kind": kind, "name": name, "response_json": json.dumps(body),
                        "expected": content, "forwarded": requests[1]["messages"][0]})
        return rows

    return {"sdk_version": anthropic.__version__, "source_hashes": {
        cls.__name__: hashlib.sha256(inspect.getsource(cls).encode()).hexdigest()
        for cls in (ToolUseBlock, ServerToolUseBlock)}, "cases": asyncio.run(capture())}
