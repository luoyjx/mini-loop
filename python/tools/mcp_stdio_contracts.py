"""Capture the real source StdioMCP against an explicit local JSON-RPC peer."""
from __future__ import annotations

import asyncio
import hashlib
import json
import sys
import tempfile
from pathlib import Path

SERVER = r'''
import json, sys
recipe=json.load(open(sys.argv[1]))
for line in sys.stdin:
    msg=json.loads(line)
    if "id" not in msg: continue
    method=msg["method"]
    result={} if method=="initialize" else recipe["tools"] if method=="tools/list" else recipe["result"]
    print(json.dumps({"jsonrpc":"2.0","method":"probe/notice"}),flush=True)
    print(json.dumps({"jsonrpc":"2.0","id":-1,"result":{}}),flush=True)
    print(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":result}),flush=True)
'''


def mcp_stdio_contracts() -> dict:
    from mini_loop.mcp import StdioMCP

    recipes = [
        ("text", {"content": [{"type": "text", "text": "hello"}]}),
        ("mixed", {"content": [None, 7, {"text": "a"}, {}, {"text": "b"}]}),
        ("is-error", {"isError": True, "content": [{"text": "failure"}]}),
        ("empty", {}),
        ("fallback-unicode", {"content": [], "value": "中文😀"}),
        ("blank-fallback", {"content": [{"text": ""}], "ok": True}),
        ("blank-lines", {"content": [{}, {}]}),
        ("limit", {"content": [{"text": "x" * 50000}]}),
        ("over-limit", {"content": [{"text": "x" * 50001}]}),
        ("unicode-cap", {"content": [{"text": "😀中" * 30000}]}),
        ("large-fallback", {"value": "x" * 60000}),
        ("bad-text", {"content": [{"text": 1}]}),
        ("object-content", {"content": {"text": "ignored"}}),
        ("string-content", {"content": "ignored"}),
        ("null-content", {"content": None}),
        ("null-result", None),
    ]
    tools = {"tools": [{"name": "echo"}, {"name": "other", "description": 7,
              "inputSchema": None, "annotations": {"readOnlyHint": True}}]}

    async def collect():
        rows = []
        for name, result in recipes:
            recipe = dict(tools=tools, result=result)
            # Large content is written to a file: exec argv has a host size cap.
            with tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / "recipe.json"
                path.write_text(json.dumps(recipe))
                client = StdioMCP("fixture", [sys.executable, "-c", SERVER, str(path)])
                try:
                    discovered = await client.list_tools()
                    error = ""
                    output = ""
                    try:
                        output = await client.call_tool("echo", {"value": "中文"})
                    except Exception as exc:
                        error = type(exc).__name__
                    compact = result
                    repeat_text, repeat_count, repeat_location = "", 0, ""
                    if isinstance(result, dict):
                        content = result.get("content")
                        single_text = (isinstance(content, list) and len(content) == 1
                                       and isinstance(content[0], dict))
                        text = content[0].get("text") if single_text else result.get("value")
                        if isinstance(text, str) and len(text) > 1000:
                            repeat_text = "😀中" if text.startswith("😀中") else "x"
                            repeat_count = len(text) // len(repeat_text)
                            assert text == repeat_text * repeat_count, "repeat recipe must preserve every character"
                            repeat_location = "content" if content is not None else "value"
                            compact = {"content": [{"text": ""}]} if repeat_location == "content" else {"value": ""}
                    rows.append(dict(name=name, tools_json=json.dumps(tools),
                                     result_json=json.dumps(compact),
                                     repeat_text=repeat_text, repeat_count=repeat_count,
                                     repeat_location=repeat_location,
                                     discovered=discovered, output=output if len(output) < 1024 else "",
                                     output_sha256=hashlib.sha256(output.encode()).hexdigest(),
                                     output_characters=len(output), error=error))
                finally:
                    await client.close()
        return rows

    return dict(rows=asyncio.run(collect()))
