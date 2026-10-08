"""Workflow composition and cleanup through the actual Python SessionManager."""
from __future__ import annotations

import asyncio
import dataclasses
from pathlib import Path
import tempfile


def manager_workflow_contracts() -> dict:
    from mini_loop.actions import InMemoryActionJournal
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.run_context import RunContext, WORKFLOW_LAUNCH
    from mini_loop.workflows.artifacts import ArtifactSubmission
    import mini_loop.workflows.service as module

    names = [
        "disabled", "complete", "custom-caps", "injected", "same-pool",
        "conflicting-pool", "delete-running", "delete-preserve", "stop-running",
        "delete-idle", "fork", "teammate",
    ]
    async def scenario(name: str) -> dict:
        with tempfile.TemporaryDirectory(prefix="mini-loop-manager-workflow-") as scratch:
            settings = Settings(
                fake_llm=True, workspace_root=Path(scratch), model="manager-model",
                trajectory_enabled=False, ast_outline_enabled=False,
            )
            if name == "custom-caps":
                settings = dataclasses.replace(
                    settings, workflow_max_concurrent_agents=1, workflow_max_agents=2,
                    workflow_max_rounds=3, workflow_wall_time_seconds=10.0,
                )
            client = FakeAsyncAnthropic()
            entered, cancelled, release = asyncio.Event(), asyncio.Event(), asyncio.Event()
            calls = []
            manager = None
            class Worker:
                def __init__(self, **kwargs):
                    self.config = kwargs
                async def __call__(self, attempt, node, inputs):
                    parent = manager.get(session.id)
                    calls.append({
                        "rounds": self.config["max_rounds"],
                        "workspace_bound": self.config["workspace"] == parent.workspace,
                        "model_pool_shared": self.config["llm_semaphore"] is manager.llm_semaphore,
                        "tool_pool_shared": self.config["tool_semaphore"] is manager.tool_semaphore,
                    })
                    entered.set()
                    if name in ("delete-running", "delete-preserve", "stop-running"):
                        try:
                            await asyncio.Event().wait()
                        except asyncio.CancelledError:
                            cancelled.set()
                            await release.wait()
                            raise
                    return ArtifactSubmission(value={"ok": True})
            original = module.FreshAgentRunner
            module.FreshAgentRunner = Worker
            injected = None
            pool = None
            if name in ("injected", "same-pool", "conflicting-pool"):
                pool = asyncio.Semaphore(2)
                injected = module.WorkflowService(
                    settings=settings, client=client, action_journal=InMemoryActionJournal(),
                    session_resolver=lambda identity: manager.get(identity), attempt_semaphore=pool,
                )
                if name == "conflicting-pool":
                    pool = asyncio.Semaphore(2)
            try:
                try:
                    manager = SessionManager(
                        settings, client, enable_workflows=name != "disabled",
                        workflow_service=injected,
                        workflow_attempt_semaphore=pool if name != "injected" else None,
                    )
                except Exception as failure:
                    return {"name": name, "error": type(failure).__name__, "detail": str(failure)}
                session = manager.create(owner="owner", permission_mode="auto")
                service = manager.workflows
                workflow_names = [tool for tool in session.agent.tools.names() if tool.startswith("Workflow")]
                before_exists, delete_returned, after_exists = True, False, True
                child_tools, fork_shared = [], False
                run = None
                if name == "fork":
                    child = await manager.fork_session(session.id)
                    fork_shared = child.agent.state["workflow_service"] is service
                    child_tools = [tool for tool in child.agent.tools.names() if tool.startswith("Workflow")]
                elif name == "teammate":
                    await manager.spawn_teammate(session.id, "peer", "research", "hello")
                    child = manager.teammate_session(session.id, "peer")
                    child_tools = [tool for tool in child.agent.tools.names() if tool.startswith("Workflow")]
                elif name not in ("disabled", "delete-idle"):
                    context = dataclasses.replace(
                        RunContext.explicit_human(actor_id="human", approved_capabilities=(WORKFLOW_LAUNCH,)),
                        message_id="m",
                    )
                    launch = await service.launch(
                        session_id=session.id,
                        definition={"name": "wf", "return_from": "a", "nodes": [{"id": "a", "kind": "agent"}],
                                    "budget": {"max_concurrent_agents": 1, "max_agents": 1, "max_rounds": 2, "wall_time_seconds": 1}},
                        args={}, run_context=context, action_id="action", launch_turn=1,
                    )
                    if name in ("delete-running", "delete-preserve", "stop-running"):
                        await entered.wait()
                        if name == "stop-running":
                            cleanup = asyncio.create_task(manager.stop())
                        else:
                            delete_returned = manager.delete(session.id, remove_workspace=name != "delete-preserve")
                            cleanup = None
                        await cancelled.wait()
                        before_exists = session.workspace.exists()
                        release.set()
                        if cleanup:
                            await cleanup
                        elif manager._cleanup_tasks:
                            await asyncio.gather(*tuple(manager._cleanup_tasks))
                        run = await service.wait(launch.run_id)
                        after_exists = session.workspace.exists()
                    else:
                        run = await service.wait(launch.run_id)
                if name == "delete-idle":
                    delete_returned = manager.delete(session.id)
                    if manager._cleanup_tasks:
                        await asyncio.gather(*tuple(manager._cleanup_tasks))
                    after_exists = session.workspace.exists()
                await manager.stop()
                return {
                    "name": name, "error": "", "detail": "",
                    "enabled": service is not None, "tools": workflow_names,
                    "journal_shared": service.action_journal is manager.actions if service else False,
                    "pool_shared": service.attempt_semaphore is manager.workflow_attempt_semaphore if service else False,
                    "pool_capacity": service.attempt_semaphore._value if service else 0,
                    "status": run.status.value if run else "",
                    "reason": run.cancel_reason if run else None,
                    "worker_calls": calls, "delete_returned": delete_returned,
                    "exists_while_draining": before_exists, "exists_after": after_exists,
                    "child_tools": child_tools, "fork_shared": fork_shared,
                }
            finally:
                release.set()
                if manager:
                    await manager.stop()
                elif injected:
                    await injected.close()
                module.FreshAgentRunner = original
    async def collect():
        return [await scenario(name) for name in names]
    return {"rows": asyncio.run(collect())}
