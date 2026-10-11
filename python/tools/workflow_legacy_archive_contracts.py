"""Actual Source scalar capture, SQLite degradation and observe-route frames."""
import asyncio
import json
import os
import tempfile
from pathlib import Path
from unittest.mock import patch


def workflow_legacy_archive_contracts() -> dict:
    from starlette.requests import Request
    from fastapi import HTTPException
    from mini_loop.config import Settings
    from mini_loop.events import WorkflowEvent
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.secrets import SecretRegistry
    from mini_loop.server import create_app
    from mini_loop.storage import SQLiteStateStore

    token = "legacy-workflow-secret"
    recipes = [
        ("nonfinite", {"values": [float("nan"), float("inf"), -float("inf")]}),
        ("surrogate-text", {"text": "\ud800"}),
        ("surrogate-key", {"\udfff": [True, None, 1, 1.0]}),
        ("nested-mixed", {"nested": [{"text": "\ud800", "number": float("nan")}, token]}),
        ("masked-key-collision", {token: "first", "<secret-hidden>": "last", "value": token}),
    ]

    async def collect():
        with tempfile.TemporaryDirectory(prefix="mini-loop-workflow-legacy-") as scratch:
            cfg = Settings(fake_llm=True, workspace_root=Path(scratch)/"ws",
                           trajectory_enabled=False, ast_outline_enabled=False)
            store = SQLiteStateStore(Path(scratch)/"state.sqlite")
            secrets = SecretRegistry()
            secrets.register("WORKFLOW_TOKEN", token)
            manager = SessionManager(cfg, FakeAsyncAnthropic(), state_store=store, secrets=secrets)
            app = create_app(settings=cfg, manager=manager)
            try:
                async with app.router.lifespan_context(app):
                    observe = next(route.endpoint for route in app.routes
                                   if getattr(route, "path", None) == "/sessions/{session_id}/events")
                    rows = []
                    for index, (name, payload) in enumerate(recipes, 1):
                        parent = manager.create(owner="alice")
                        await parent.emit({"type": "status", "status": "idle"})
                        event = WorkflowEvent(kind="workflow_paused", session_id=parent.id,
                                              run_id="run", workflow_name="wf", definition_revision="rev",
                                              event_id=f"event-{index}", occurred_at=1.5, payload=payload)
                        queue = parent.subscribe(replay=False)
                        raw = event.as_session_event()
                        await parent.emit(raw)
                        captured = queue.get_nowait()
                        saved = store.load_events(parent.id)

                        def request(token):
                            return Request({"type": "http", "app": app, "method": "GET",
                                            "path": f"/sessions/{parent.id}/events",
                                            "headers": [(b"authorization", f"Bearer {token}".encode()),
                                                        (b"last-event-id", b"1")]})

                        response = await observe(request("token-a"), parent.id, envelope=False)
                        iterator = response.body_iterator
                        try:
                            frame = await anext(iterator)
                        finally:
                            await iterator.aclose()
                        try:
                            await observe(request("token-b"), parent.id, envelope=False)
                        except HTTPException as failure:
                            foreign_status = failure.status_code
                        else:
                            raise AssertionError("foreign owner admitted")

                        def normalized(row):
                            return {**row, "session_id": "s", "session": "s", "ts": 0}

                        utf8_error = None
                        try:
                            json.dumps(captured, ensure_ascii=False).encode("utf-8")
                        except UnicodeEncodeError as failure:
                            utf8_error = type(failure).__name__
                        raw["session_id"] = "s"
                        rows.append({"name": name, "raw_wire": json.dumps(raw),
                                     "recorded_wire": json.dumps(normalized(captured)),
                                     "frame_wire": json.dumps(normalized(json.loads(frame["data"]))),
                                     "frame_event": frame["event"], "frame_id": frame["id"],
                                     "stored_wire": json.dumps(normalized(saved[-1])) if len(saved) > 1 else None,
                                     "persist_error": parent.persist_error.split(":", 1)[0] if parent.persist_error else None,
                                     "utf8_error": utf8_error, "foreign_status": foreign_status})
                        parent.unsubscribe(queue)
                    return {"rows": rows}
            finally:
                store.close()

    with patch.dict(os.environ, {"MINILOOP_API_TOKENS": "alice:token-a,bob:token-b"}):
        return asyncio.run(collect())
