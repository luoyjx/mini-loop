"""Actual managed SDK turns through Source's finite message SSE route."""

import hashlib
import inspect
import json
from pathlib import Path

import anthropic
import httpx
from starlette.testclient import TestClient

from mini_loop import SessionManager, Settings
from mini_loop.actions import _payload_hash
from mini_loop.agent import Agent
from mini_loop.auth import TokenAuth
from mini_loop.registry import Hook, Hooks
from mini_loop.server import create_app, _register_routes
from mini_loop.storage import SQLiteStateStore


def provider_nonfinite_stream_contracts(scratch: Path | str) -> dict:
    if anthropic.__version__ != "0.107.1":
        raise RuntimeError("nonfinite stream contracts require anthropic 0.107.1")
    scratch = Path(scratch)
    rows = []
    for store_kind in ("memory", "sqlite"):
        for name, number in (("nan", float("nan")), ("infinity", float("inf"))):
            root = scratch / f"nonfinite-stream-{store_kind}-{name}"
            root.mkdir(parents=True, exist_ok=True)
            trace, requests = [], []

            class Probe(Hook):
                async def before_tool(self, ctx, call):
                    trace.append("before")

                async def guard_tool(self, ctx, call):
                    trace.append("guard")

                async def after_tool(self, ctx, call, output):
                    trace.append("after")

                async def on_result(self, ctx, call, output, *, denied=False, failed=False):
                    trace.append("result")

            async def transport(request):
                requests.append(json.loads(request.content))
                body = {"id": "message", "type": "message", "role": "assistant", "model": "fixture-model",
                        "content": [{"type": "tool_use", "id": "call", "name": "bash",
                                     "input": {"command": "true", "keep": number}}],
                        "stop_reason": "tool_use", "stop_sequence": None,
                        "usage": {"input_tokens": 1, "output_tokens": 0}}
                return httpx.Response(200, content=json.dumps(body).encode(),
                                      headers={"content-type": "application/json"})

            sdk = anthropic.AsyncAnthropic(api_key="fixture-key", base_url="https://fixture.invalid", max_retries=0,
                http_client=httpx.AsyncClient(transport=httpx.MockTransport(transport)))
            store = SQLiteStateStore(root / "state.sqlite3") if store_kind == "sqlite" else None
            manager = SessionManager(Settings(fake_llm=True, workspace_root=root / "ws", skills_dir=root / "skills"),
                                     sdk, hooks=Hooks([Probe()]), state_store=store)
            app = create_app(manager=manager)
            token = "tok-fixture-000000000000"
            try:
                with TestClient(app) as client:
                    app.state.auth = TokenAuth({token: "alice"})
                    headers = {"Authorization": f"Bearer {token}"}
                    created = client.post("/sessions", headers=headers, json={})
                    created.raise_for_status()
                    session_id = created.json()["id"]
                    response = client.post(f"/sessions/{session_id}/messages/stream", headers=headers,
                                           json={"message": "hello"})
                    response.raise_for_status()
                    events = [json.loads(line[6:]) for line in response.text.splitlines() if line.startswith("data: ")]
                    tool_events = [{key: event[key] for key in ("type", "name", "input", "output", "error", "denied", "replayed", "display")
                                    if key in event} for event in events if event["type"] in {"tool_use", "tool_result"}]
                    row = {"store": store_kind, "name": name, "status": response.status_code,
                           "content_type": response.headers["content-type"], "trace": trace,
                           "event_types": [event["type"] for event in events],
                           "tool_events_json": json.dumps(tool_events, sort_keys=True),
                           "transport_requests": len(requests),
                           "action_recorded": manager.actions.get(next(event["action_id"] for event in events if event["type"] == "tool_use")) is not None,
                           "done_text": next(event["text"] for event in events if event["type"] == "done")}
                    if store is not None:
                        persisted = store.load_events(session_id)
                        row["stored_tool_events_json"] = json.dumps(
                            [{key: event[key] for key in tool_events[0] if key in event}
                             for event in persisted if event["type"] == "tool_use"], sort_keys=True)
                    rows.append(row)
                    client.portal.call(sdk.close)
            finally:
                if store is not None:
                    store.close()
    return {"sdk_version": anthropic.__version__, "httpx_version": httpx.__version__,
            "source_sha256": {name: hashlib.sha256(inspect.getsource(fn).encode()).hexdigest()
                              for name, fn in {"Agent._exec_tool": Agent._exec_tool, "actions._payload_hash": _payload_hash,
                                               "server._register_routes": _register_routes}.items()}, "cases": rows}
