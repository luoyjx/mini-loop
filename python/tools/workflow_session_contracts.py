"""Actual workflow event capture, masking and SQLite publication contracts."""
import asyncio
import tempfile
from pathlib import Path


def workflow_session_contracts() -> dict:
    from mini_loop.config import Settings
    from mini_loop.events import WorkflowEvent
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.run_context import RunContext, WORKFLOW_LAUNCH
    from mini_loop.manager import SessionManager
    from mini_loop.secrets import SecretRegistry
    from mini_loop.storage import SQLiteStateStore

    token = "workflow-secret-canary"
    recipes = [
        ("workflow_planned", {"definition_hash": token, "node_count": 1, "size_guideline": token}),
        ("workflow_decision_recorded", {"decision": "approved", "actor_id": token, "authority": "explicit_human", "mode": "trusted_local_preapproval"}),
        ("workflow_started", {"node_count": 1, "started_at": 2.0}),
        ("workflow_node_claimed", {"kind": "agent", "spawn_index": 1}),
        ("workflow_agent_started", {"kind": "agent"}),
        ("workflow_agent_progress", {"type": "tool_result", "name": "read_file", "id": token, "error": token, "duration_ms": 3.5}),
        ("workflow_agent_completed", {"success": False, "error": token}),
        ("workflow_verdict_recorded", {"status": "failed", "artifact_id": None, "error": token}),
        ("workflow_completed", {"artifact_id": token, "content_hash": token}),
        ("workflow_failed", {"error": token}),
        ("workflow_cancelled", {"reason": token}),
        ("workflow_result_enqueued", {"message_id": token, "status": "completed", "artifact_id": token}),
    ]

    async def collect():
        with tempfile.TemporaryDirectory(prefix="mini-loop-workflow-events-") as scratch:
            store = SQLiteStateStore(Path(scratch) / "state.sqlite")
            secrets = SecretRegistry()
            secrets.register("WORKFLOW_TOKEN", token)
            seen = []
            manager = SessionManager(Settings(fake_llm=True, workspace_root=Path(scratch) / "ws", trajectory_enabled=False, ast_outline_enabled=False), FakeAsyncAnthropic(), state_store=store, secrets=secrets, event_sink=seen.append)
            try:
                session = manager.create(owner="owner")
                queue = session.subscribe(replay=False)
                raw = []
                for index, (kind, payload) in enumerate(recipes, 1):
                    scoped = kind in {"workflow_node_claimed", "workflow_agent_started", "workflow_agent_progress", "workflow_agent_completed", "workflow_verdict_recorded"}
                    agent = kind in {"workflow_agent_started", "workflow_agent_progress", "workflow_agent_completed"}
                    event = WorkflowEvent(kind, session.id, "run", token, token, payload,
                        node_id=token if scoped else None, attempt_id=token if scoped and kind != "workflow_node_claimed" else None,
                        agent_id=token if agent else None, parent_agent_id=token if agent else None,
                        event_id=f"event-{index}", occurred_at=1.0)
                    raw.append(event.as_session_event())
                    await session.emit(event.as_session_event())
                def normalize(rows):
                    return [{**row, "session": "s", "session_id": "s", "ts": 0.0} for row in rows]
                result = {"raw": [{**row, "session_id": "s"} for row in raw],
                    "live": normalize([queue.get_nowait() for _ in recipes]),
                    "backlog": normalize(list(session._backlog)), "sink": normalize(seen),
                    "stored": normalize(store.load_events(session.id)), "disabled_summaries": session.info()["workflows"]}
                calls = {"count": 0}
                def responder(request):
                    calls["count"] += 1
                    return ([tool("return_artifact", _id="a", value={})], "tool_use") if calls["count"] == 1 else ([text("done")], "end_turn")
                enabled = SessionManager(Settings(fake_llm=True, workspace_root=Path(scratch) / "enabled", trajectory_enabled=False, ast_outline_enabled=False), FakeAsyncAnthropic(responder=responder), enable_workflows=True)
                try:
                    parent = enabled.create(owner="owner")
                    launch = await enabled.workflows.launch(session_id=parent.id,
                        definition={"name": "wf", "return_from": "a", "nodes": [{"id": "a", "kind": "agent"}]}, args={},
                        run_context=RunContext.explicit_human(actor_id="human", approved_capabilities=(WORKFLOW_LAUNCH,)), action_id="action", launch_turn=1)
                    await enabled.workflows.wait(launch.run_id)
                    result["completed_summaries"] = [{**row, "run_id": "run"} for row in parent.info()["workflows"]]
                    child = await enabled.fork_session(parent.id)
                    result["fork_summaries"] = child.info()["workflows"]
                finally:
                    await enabled.stop()
                import mini_loop.workflows.service as module
                entered = asyncio.Event()
                class BlockedWorker:
                    def __init__(self, **kwargs):
                        pass
                    async def __call__(self, attempt, node, inputs):
                        entered.set()
                        await asyncio.Event().wait()
                original = module.FreshAgentRunner
                module.FreshAgentRunner = BlockedWorker
                stopping = None
                try:
                    stopping = SessionManager(Settings(fake_llm=True, workspace_root=Path(scratch) / "stopping", trajectory_enabled=False, ast_outline_enabled=False), FakeAsyncAnthropic(), enable_workflows=True)
                    parent = stopping.create(owner="owner")
                    launch = await stopping.workflows.launch(session_id=parent.id,
                        definition={"name": "wf", "return_from": "a", "nodes": [{"id": "a", "kind": "agent"}]}, args={},
                        run_context=RunContext.explicit_human(actor_id="human", approved_capabilities=(WORKFLOW_LAUNCH,)), action_id="action", launch_turn=1)
                    await entered.wait()
                    await stopping.stop()
                    result["shutdown_terminal_kinds"] = [row["type"] for row in parent._backlog if row["type"] in ("workflow_cancelled", "workflow_result_enqueued")]
                finally:
                    if stopping is not None:
                        await stopping.stop()
                    module.FreshAgentRunner = original
                return result
            finally:
                await manager.stop()
                store.close()
    return asyncio.run(collect())
