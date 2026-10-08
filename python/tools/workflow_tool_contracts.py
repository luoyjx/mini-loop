"""Actual workflow tool handlers, isolated from the large general exporter."""
from __future__ import annotations

import asyncio
import dataclasses
import json
from pathlib import Path
from types import SimpleNamespace


def workflow_bound_tool_contracts() -> dict:
    from mini_loop.actions import InMemoryActionJournal
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.registry import ToolCall, ToolContext, ToolRegistry
    from mini_loop.run_context import RunContext, WORKFLOW_LAUNCH, WORKFLOW_MANAGE
    from mini_loop.workflows.artifacts import ArtifactSubmission
    from mini_loop.workflows.tools import install_workflows
    import mini_loop.workflows.service as module

    recipes = [
        ("launch", "Workflow", "both"),
        ("status", "WorkflowStatus", "both"),
        ("cancel", "WorkflowCancel", "both"),
        ("launch-only", "Workflow", "launch"),
        ("manage-only-status", "WorkflowStatus", "manage"),
        ("manage-only-cancel", "WorkflowCancel", "manage"),
        ("launch-missing-cap", "Workflow", "manage"),
        ("status-missing-cap", "WorkflowStatus", "launch"),
        ("cancel-missing-cap", "WorkflowCancel", "launch"),
        ("untrusted-launch", "Workflow", "untrusted"),
        ("untrusted-status", "WorkflowStatus", "untrusted"),
        ("untrusted-cancel", "WorkflowCancel", "untrusted"),
        ("no-context", "WorkflowStatus", "none"),
        ("no-action", "Workflow", "both"),
        ("no-call", "Workflow", "both"),
        ("no-parent", "Workflow", "both"),
        ("no-service-launch", "Workflow", "both"),
        ("no-service-status", "WorkflowStatus", "both"),
        ("no-service-cancel", "WorkflowCancel", "both"),
        ("foreign-status", "WorkflowStatus", "both"),
        ("foreign-cancel", "WorkflowCancel", "both"),
    ]

    async def scenario(name: str, tool: str, context_kind: str) -> dict:
        definition = {
            "name": "wf", "return_from": "a",
            "revision": "forged", "source": "plugin",
            "nodes": [{"id": "a", "kind": "agent"}],
        }
        args = {"x": "中文", "float": 1.0}
        async def emit(_):
            pass
        parent = SimpleNamespace(id="s", run_count=7, workspace="<workspace>", emit=emit)
        class Worker:
            def __init__(self, **_):
                pass
            async def __call__(self, attempt, node, inputs):
                return ArtifactSubmission(value={"answer": "中文"})
        original = module.FreshAgentRunner
        module.FreshAgentRunner = Worker
        journal = InMemoryActionJournal()
        service = module.WorkflowService(
            settings=Settings(fake_llm=True), client=FakeAsyncAnthropic(),
            action_journal=journal, session_resolver=lambda _: parent,
        )
        trusted = dataclasses.replace(
            RunContext.explicit_human(
                actor_id="human", approved_capabilities=(WORKFLOW_LAUNCH, WORKFLOW_MANAGE),
            ), message_id="m",
        )
        context = trusted
        if context_kind == "launch":
            context = dataclasses.replace(trusted, approved_capabilities=(WORKFLOW_LAUNCH,))
        if context_kind == "manage":
            context = dataclasses.replace(trusted, approved_capabilities=(WORKFLOW_MANAGE,))
        if context_kind == "untrusted":
            context = dataclasses.replace(RunContext.default(), message_id="m")
        if context_kind == "none":
            context = None
        state = {"workflow_service": service, "session": parent, "session_id": "s"}
        if name == "no-parent":
            state.pop("session")
        if name.startswith("no-service"):
            state.pop("workflow_service")
        if name.startswith("foreign"):
            state["session_id"] = "foreign"
        output, error, detail, ids = "", "", "", {}
        try:
            run_id = ""
            if tool != "Workflow":
                launch = await service.launch(
                    session_id="s", definition=definition, args=args, run_context=trusted,
                    action_id="seed", tool_use_id="seed", launch_turn=7,
                )
                run = await service.wait(launch.run_id)
                run_id = run.run_id
                ids[run_id] = "<run>"
                ids[run.final_artifact_id] = "<artifact>"
                for attempt in service.store.list_attempts(run_id):
                    ids[attempt.attempt_id] = "<attempt>"
            value = {"definition": definition, "args": args} if tool == "Workflow" else {"run_id": run_id}
            call = ToolCall(tool, value, "u")
            ctx = ToolContext(
                agent=None, workspace=Path("<workspace>"), state=state,
                call=None if name == "no-call" else call, run_context=context,
                action_id=None if name == "no-action" else "action",
            )
            try:
                handler = install_workflows(ToolRegistry()).get(tool).handler
                output = await handler(ctx, **value)
                if tool == "Workflow":
                    launched = json.loads(output)
                    ids[launched["run_id"]] = "<run>"
                    await service.wait(launched["run_id"])
            except Exception as failure:
                error, detail = type(failure).__name__, str(failure)

            def clean(value):
                if isinstance(value, dict):
                    return {
                        key: (0 if key in ("created_at", "started_at", "ended_at", "heartbeat_at", "completed_at") and child is not None else clean(child))
                        for key, child in value.items()
                    }
                if isinstance(value, list):
                    return [clean(child) for child in value]
                if isinstance(value, str):
                    for raw, label in ids.items():
                        if raw:
                            value = value.replace(raw, label)
                return value
            if output:
                output = json.dumps(clean(json.loads(output)), ensure_ascii=False, sort_keys=True)
            record = journal.get("action")
            return {
                "name": name, "tool": tool, "context": context_kind,
                "definition": definition, "args": args, "output": output,
                "error": error, "detail": clean(detail),
                "input_hash": record.input_hash if record else "",
            }
        finally:
            await service.close()
            module.FreshAgentRunner = original

    async def collect():
        return [await scenario(*recipe) for recipe in recipes]
    registry = install_workflows(ToolRegistry())
    traits = [
        {"name": name, "risk": registry.get(name).risk,
         "readonly": registry.get(name).readonly,
         "parallel_safe": registry.get(name).parallel_safe}
        for name in registry.names()
    ]
    return {"rows": asyncio.run(collect()), "traits": traits}
