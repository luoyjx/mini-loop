"""Actual Source workflow envelope, open payload and recording projections."""
from __future__ import annotations

import asyncio
import dataclasses
import tempfile
from pathlib import Path


def workflow_archive_contracts() -> dict:
    from mini_loop.config import Settings
    from mini_loop.events import WorkflowEvent, WORKFLOW_EVENT_KINDS
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.secrets import SecretRegistry
    from mini_loop.storage import SQLiteStateStore

    token = "workflow-archive-secret"
    def event(kind, payload, index, **patch):
        fields = dict(kind=kind, session_id="s", run_id="run", workflow_name="wf",
                      definition_revision="rev", payload=payload, event_id=f"event-{index}-{token}", occurred_at=1.5)
        if kind in {"workflow_node_claimed", "workflow_agent_started", "workflow_agent_progress", "workflow_agent_completed", "workflow_verdict_recorded"}:
            fields["node_id"] = token
        if kind in {"workflow_agent_started", "workflow_agent_progress", "workflow_agent_completed", "workflow_verdict_recorded"}:
            fields["attempt_id"] = token
        if kind in {"workflow_agent_started", "workflow_agent_progress", "workflow_agent_completed"}:
            fields.update(agent_id=token, parent_agent_id=token)
        if kind == "workflow_phase_started":
            fields["phase_id"] = token
        fields.update(patch)
        return WorkflowEvent(**fields)

    recipes = []
    for index, kind in enumerate(sorted(WORKFLOW_EVENT_KINDS), 1):
        payload = {"future": {token: [None, True, 1, 1.0, 123456789012345678901234567890, {"text": token}]},
                   "error": {"detail": token}, "decision": "rejected", "mode": "historical",
                   "authority": "explicit_human", "approved_capabilities": ["workflow.launch"],
                   "unknown": "历史数据"}
        recipes.append((kind, event(kind, payload, index)))
    recipes.extend([
        ("empty-progress", event("workflow_agent_progress", {}, 19)),
        ("empty-event-id", event("workflow_paused", {}, 20, event_id="")),
        ("optional-empty-fields", event("workflow_resumed", {}, 21, phase_id="", node_id="", attempt_id="", agent_id="", parent_agent_id="")),
        ("optional-null-fields", event("workflow_checkpointed", {}, 22, phase_id=None, node_id=None)),
    ])
    refusals = []
    for name, kind, patch in [
        ("unknown", "workflow_unknown", {}),
        ("empty-session", "workflow_paused", {"session_id": ""}),
        ("empty-run", "workflow_paused", {"run_id": ""}),
        ("empty-name", "workflow_paused", {"workflow_name": ""}),
        ("empty-revision", "workflow_paused", {"definition_revision": ""}),
        ("version", "workflow_paused", {"payload_version": 2}),
        ("phase", "workflow_phase_started", {"phase_id": None}),
        ("node", "workflow_agent_progress", {"node_id": None}),
        ("attempt", "workflow_agent_progress", {"attempt_id": None}),
        ("agent", "workflow_agent_progress", {"agent_id": None}),
    ]:
        base = event("workflow_agent_progress" if "agent" in kind else "workflow_paused", {}, 23).as_session_event()
        base.update(type=kind, kind=kind, **patch)
        if kind == "workflow_phase_started":
            base["phase_id"] = None
        if "run_id" in patch:
            base["workflow_run_id"] = patch["run_id"]
        try:
            event(kind, {}, 23, **patch)
        except Exception as failure:
            refusals.append(dict(name=name, input=base, error=type(failure).__name__, detail=str(failure)))
        else:
            raise AssertionError(name)

    async def collect():
        with tempfile.TemporaryDirectory(prefix="mini-loop-workflow-archive-") as scratch:
            store = SQLiteStateStore(Path(scratch) / "state.sqlite")
            secrets = SecretRegistry()
            secrets.register("WORKFLOW_TOKEN", token)
            manager = SessionManager(Settings(fake_llm=True, workspace_root=Path(scratch)/"ws", trajectory_enabled=False, ast_outline_enabled=False), FakeAsyncAnthropic(), state_store=store, secrets=secrets)
            try:
                parent = manager.create(owner="owner")
                queue = parent.subscribe(replay=False)
                rows = []
                for name, source in recipes:
                    bound = dataclasses.replace(source, session_id=parent.id)
                    raw = bound.as_session_event()
                    await parent.emit(raw)
                    captured = queue.get_nowait()
                    stored = store.load_events(parent.id)[-1]
                    def normalize(value):
                        return {**value, "session": "s", "session_id": "s", "ts": 0}
                    live, saved = normalize(captured), normalize(stored)
                    if live != saved:
                        raise AssertionError("recording mismatch")
                    rows.append(dict(name=name, raw={**raw, "session_id": "s"}, recorded=live))
                return dict(rows=rows, refusals=refusals, kinds=sorted(WORKFLOW_EVENT_KINDS), stored_matches_live=True)
            finally:
                await manager.stop()
                store.close()
    return asyncio.run(collect())
