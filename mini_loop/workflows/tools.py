"""Agent-facing tools for the explicitly enabled local workflow service."""

from __future__ import annotations

import json

from ..registry import Tool, ToolContext, ToolRegistry
from ..run_context import (
    EXPLICIT_HUMAN,
    WORKFLOW_LAUNCH,
    WORKFLOW_MANAGE,
)
from .service import WorkflowService


WORKFLOW_INPUT_SCHEMA = {
    "type": "object",
    "properties": {
        "definition": {
            "type": "object",
            "description": "Versioned declarative WorkflowDefinition.",
        },
        "args": {
            "type": "object",
            "description": "Arguments validated against definition.input_schema.",
        },
    },
    "required": ["definition", "args"],
    "additionalProperties": False,
}

RUN_ID_SCHEMA = {
    "type": "object",
    "properties": {"run_id": {"type": "string"}},
    "required": ["run_id"],
    "additionalProperties": False,
}


def _service(ctx: ToolContext) -> WorkflowService:
    service = ctx.state.get("workflow_service")
    if not isinstance(service, WorkflowService):
        raise RuntimeError("workflow service is not available")
    return service


def _require_trusted_context(ctx: ToolContext, capability: str) -> None:
    if ctx.run_context is None or ctx.run_context.authority != EXPLICIT_HUMAN:
        raise PermissionError(
            "workflow operations require an explicit_human trusted local context"
        )
    if not ctx.run_context.allows(capability):
        raise PermissionError(
            f"workflow operation requires per-message {capability} approval"
        )


async def _launch_workflow(
    ctx: ToolContext,
    definition: dict,
    args: dict,
) -> str:
    _require_trusted_context(ctx, WORKFLOW_LAUNCH)
    if not ctx.action_id or ctx.call is None:
        raise RuntimeError("workflow launch requires a journaled tool action")
    session = ctx.state.get("session")
    if session is None:
        raise RuntimeError("workflow launch requires a managed AgentSession")
    result = await _service(ctx).launch(
        session_id=session.id,
        definition=definition,
        args=args,
        run_context=ctx.run_context,
        action_id=ctx.action_id,
        launch_turn=session.run_count,
        action_input={"definition": definition, "args": args},
        tool_use_id=ctx.call.id,
    )
    return json.dumps(result.as_dict(), ensure_ascii=False, sort_keys=True)


async def _workflow_status(ctx: ToolContext, run_id: str) -> str:
    _require_trusted_context(ctx, WORKFLOW_MANAGE)
    session_id = str(ctx.state.get("session_id", ""))
    status = _service(ctx).status(run_id, session_id=session_id)
    return json.dumps(status, ensure_ascii=False, sort_keys=True)


async def _cancel_workflow(ctx: ToolContext, run_id: str) -> str:
    _require_trusted_context(ctx, WORKFLOW_MANAGE)
    session_id = str(ctx.state.get("session_id", ""))
    run = await _service(ctx).cancel(
        run_id,
        session_id=session_id,
        reason="cancelled by trusted parent",
    )
    return json.dumps(
        {"run_id": run.run_id, "status": run.status.value},
        ensure_ascii=False,
        sort_keys=True,
    )


def install_workflows(registry: ToolRegistry) -> ToolRegistry:
    """Install the local-only workflow surface; all calls are ordering barriers."""

    registry.register(
        Tool(
            "Workflow",
            (
                "Launch a bounded, read-only declarative workflow. This "
                "experimental tool is local-only and requires trusted human origin."
            ),
            WORKFLOW_INPUT_SCHEMA,
            _launch_workflow,
        )
    )
    registry.register(
        Tool(
            "WorkflowStatus",
            "Inspect a workflow owned by this session.",
            RUN_ID_SCHEMA,
            _workflow_status,
            readonly=True,
        )
    )
    registry.register(
        Tool(
            "WorkflowCancel",
            "Cooperatively cancel a workflow owned by this session.",
            RUN_ID_SCHEMA,
            _cancel_workflow,
        )
    )
    return registry
