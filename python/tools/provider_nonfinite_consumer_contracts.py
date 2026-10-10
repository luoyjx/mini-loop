"""Actual SDK nonfinite observations, Source consumers, archive and HTTP encoding."""
from __future__ import annotations

import asyncio
import hashlib
import inspect
import json
from pathlib import Path

import anthropic
import httpx

from mini_loop.agent import _content_payload
from mini_loop.storage import SQLiteStateStore, SessionRecord
from tools.provider_tool_input_consumer_contracts import provider_tool_input_consumer_contracts


def provider_nonfinite_consumer_contracts(scratch: Path | str) -> dict:
    if anthropic.__version__ != "0.107.1":
        raise RuntimeError("nonfinite consumer contracts require anthropic 0.107.1")
    scratch = Path(scratch)
    consumers = {row["name"]: row for row in
                 provider_tool_input_consumer_contracts(scratch)["cases"]
                 if row["name"] in {"nonfinite-nan", "nonfinite-infinity"}}

    async def capture():
        rows = []
        for kind in ("tool_use", "server_tool_use"):
            for name, value in (("nonfinite-nan", float("nan")),
                                ("nonfinite-infinity", float("inf"))):
                block = {"type": kind, "id": "call", "name": "bash" if kind == "tool_use" else "web_search",
                         "input": {"command": "true", "keep": value}}
                body = {"id": "message", "type": "message", "role": "assistant", "model": "fixture-model",
                        "content": [block], "stop_reason": "end_turn", "stop_sequence": None,
                        "usage": {"input_tokens": 1, "output_tokens": 0}}
                requests = []

                async def transport(request):
                    requests.append(json.loads(request.content))
                    # This intentionally preserves the actual legacy response bytes;
                    # httpx.Response(json=...) would refuse them before SDK parsing.
                    return httpx.Response(200, content=json.dumps(body).encode(),
                                          headers={"content-type": "application/json"})

                async with anthropic.AsyncAnthropic(api_key="fixture-key", base_url="https://fixture.invalid", max_retries=0,
                        http_client=httpx.AsyncClient(transport=httpx.MockTransport(transport))) as client:
                    response = await client.messages.create(model="fixture-model", max_tokens=8,
                        messages=[{"role": "user", "content": "hello"}])
                    content = _content_payload(response.content)
                    message = {"role": "assistant", "content": content}
                    path = scratch / f"{kind}-{name}.sqlite3"
                    store = SQLiteStateStore(path)
                    try:
                        store.upsert_session(SessionRecord("session", str(scratch), None, 1.0, 0, "idle", 0))
                        store.append_messages("session", [message])
                        store.append_event("session", {"type": "assistant", "content": content})
                    finally:
                        store.close()
                    reopened = SQLiteStateStore(path)
                    try:
                        archived = reopened.load_messages("session")
                        events = reopened.load_events("session")
                    finally:
                        reopened.close()
                    row = {"kind": kind, "name": name, "response_json": json.dumps(body),
                           "content_json": json.dumps(content, sort_keys=True),
                           "reopened_messages_json": json.dumps(archived, sort_keys=True),
                           "reopened_events_json": json.dumps(events, sort_keys=True)}
                    if kind == "tool_use":
                        row["tool_consumer"] = consumers[name]
                    try:
                        await client.messages.create(model="fixture-model", max_tokens=8, messages=[message])
                        row["forward_succeeded"] = True
                    except Exception as error:
                        row["forward_error"] = {"class": type(error).__name__, "message": str(error)}
                    row["transport_requests"] = len(requests)
                    rows.append(row)
        return rows

    sources = {"_content_payload": _content_payload, "SQLiteStateStore.append_messages": SQLiteStateStore.append_messages,
               "SQLiteStateStore.append_event": SQLiteStateStore.append_event}
    return {"sdk_version": anthropic.__version__, "httpx_version": httpx.__version__,
            "source_sha256": {name: hashlib.sha256(inspect.getsource(fn).encode()).hexdigest()
                              for name, fn in sources.items()}, "cases": asyncio.run(capture())}
