"""Actual managed Source trajectory observations from pinned Anthropic replies."""
from __future__ import annotations

import asyncio
import hashlib
import inspect
import json
from pathlib import Path

import anthropic
import httpx

from mini_loop.config import Settings
from mini_loop.manager import SessionManager
from mini_loop.secrets import SecretRegistry
from mini_loop.session import AgentSession
from mini_loop.trajectory import TrajectoryStore


def provider_nonfinite_trajectory_contracts(scratch: Path | str) -> dict:
    if anthropic.__version__ != "0.107.1":
        raise RuntimeError("trajectory contracts require anthropic 0.107.1")

    async def capture(name, number, content):
        root = Path(scratch) / f"nonfinite-trajectory-{name}-{content}"
        registry = SecretRegistry()
        registry.register("DEMO", "fixture-secret")
        store = TrajectoryStore(root / "traces", capture_content=content)
        requests = []
        original = {"command": "true", "keep": number,
                    "fixture-secret": ["fixture-secret", {"n": number}]}

        async def transport(request):
            requests.append(json.loads(request.content))
            reply = {"id": "message", "type": "message", "role": "assistant", "model": "model",
                     "content": [{"type": "tool_use", "id": "call", "name": "bash", "input": original}],
                     "stop_reason": "tool_use", "usage": {"input_tokens": 1, "output_tokens": 0}}
            return httpx.Response(200, content=json.dumps(reply).encode(),
                                  headers={"content-type": "application/json"})

        sdk = anthropic.AsyncAnthropic(api_key="fixture-key", base_url="https://fixture.invalid", max_retries=0,
            http_client=httpx.AsyncClient(transport=httpx.MockTransport(transport)))
        manager = SessionManager(Settings(fake_llm=True, workspace_root=root / "ws", skills_dir=root / "skills"),
                                 sdk, secrets=registry, trajectory_store=store)
        try:
            session = manager.create(owner="alice", permission_mode="auto")
            await session.run("fixture-secret")
            trace = store.list(session_id=session.id)[0]["id"]
            # A fresh reader proves actual JSONL bytes, not the live event sink.
            reader = TrajectoryStore(root / "traces", capture_content=content)
            document = reader.get(trace)
            snapshots = []
            for event in document["events"]:
                if event["type"] == "model_start":
                    value = event["model_input"]
                    snapshots.append({"type": event["type"], "messages": value["messages"] if content else value})
                elif event["type"] == "model_end" and "model_output" in event:
                    snapshots.append({"type": event["type"], "output": event["model_output"]})
                elif event["type"] == "tool_use":
                    snapshots.append({"type": event["type"], "input": event["input"]})
            return {"name": name, "capture_content": content,
                    "input_json": json.dumps(original, sort_keys=True),
                    "snapshots_json": json.dumps(snapshots, sort_keys=True),
                    "transport_requests": len(requests),
                    "raw_contains_secret": "fixture-secret" in reader.raw(trace),
                    "recording_error": session.info()["trajectory_recording_error"],
                    "status": document["status"],
                    "live_input_json": json.dumps(session.agent.messages[1]["content"][0]["input"], sort_keys=True)}
        finally:
            await manager.stop()
            await sdk.close()

    rows = [asyncio.run(capture(name, number, content)) for content in (True, False)
            for name, number in (("nan", float("nan")), ("infinity", float("inf")))]
    return {"sdk_version": anthropic.__version__, "cases": rows,
            "source_sha256": {name: hashlib.sha256(inspect.getsource(fn).encode()).hexdigest()
                              for name, fn in {"AgentSession._capture_event": AgentSession._capture_event,
                                               "TrajectoryStore._write": TrajectoryStore._write}.items()}}
