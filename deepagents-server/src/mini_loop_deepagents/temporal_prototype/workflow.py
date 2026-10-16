"""Real Deep Agents loop in a Temporal Workflow, with explicit full-state CAN.

The small memory tools only schedule named Activities. SDK 1.34.0 warns that
unwrapped tools must be pure/deterministic; these wrappers meet that condition.
They do no DB, filesystem, network or wall-clock I/O in Workflow code. We use
explicit scheduling because activity_as_tool does not expose heartbeat_timeout.
"""

from __future__ import annotations

from datetime import timedelta
import hashlib
import json
from typing import Any

from temporalio import workflow
from temporalio.common import RetryPolicy
from temporalio.exceptions import ApplicationError

with workflow.unsafe.imports_passed_through():
    from langchain.agents.middleware import TodoListMiddleware
    from langchain_core.messages import HumanMessage, messages_from_dict, messages_to_dict
    from langchain_core.tools import StructuredTool
    from langgraph.checkpoint.memory import InMemorySaver
    from langgraph.graph import END
    from temporalio.contrib.deepagents import create_temporal_deep_agent
    from mini_loop_deepagents.temporal_prototype import CODE_VERSION, SNAPSHOT_SCHEMA
    from mini_loop_deepagents.temporal_prototype.activities import memory_get, memory_put
    # Compatibility exports for clients of the original prototype module.
    from mini_loop_deepagents.temporal_support.determinism import (
        DeterministicStateBackend,
        StableToolMessageIDs,
    )


def _memory_tool(name: str, function: Any) -> StructuredTool:
    async def dispatch(request: dict[str, Any]) -> Any:
        return await workflow.execute_activity(
            function, request, start_to_close_timeout=timedelta(seconds=120),
            heartbeat_timeout=timedelta(seconds=3),
            retry_policy=RetryPolicy(initial_interval=timedelta(seconds=1),
                                     maximum_interval=timedelta(seconds=2), maximum_attempts=20),
            summary=f"prototype:{name}",
        )

    return StructuredTool.from_function(coroutine=dispatch, name=name,
                                       description="Perform an owner-scoped durable memory operation through an Activity.")


def rehydrate_snapshot(snapshot: Any, identity: dict[str, str]) -> dict[str, Any]:
    """Validate the one explicit prototype CAN boundary before loading state."""
    if (not isinstance(snapshot, dict) or snapshot.get("schema_version") != SNAPSHOT_SCHEMA
            or snapshot.get("version") != CODE_VERSION or snapshot.get("identity") != identity
            or type(snapshot.get("phase")) is not int or snapshot["phase"] != 1
            or type(snapshot.get("can_count")) is not int or snapshot["can_count"] != 1
            or not isinstance(snapshot.get("messages"), list)
            or not isinstance(snapshot.get("files"), dict) or not isinstance(snapshot.get("todos"), list)):
        raise ApplicationError("Invalid full-state continuation snapshot", non_retryable=True)
    try:
        messages = messages_from_dict(snapshot["messages"])
    except (KeyError, TypeError, ValueError):
        raise ApplicationError("Invalid continuation transcript", non_retryable=True) from None
    return {"messages": messages, "files": dict(snapshot["files"]), "todos": list(snapshot["todos"])}


def build_agent(workflow_id: str = "prototype-local-check") -> Any:
    """Build the real factory with explicit native planning for this model profile.

    Deep Agents 0.7.25 does not install TodoListMiddleware in its default stack
    for this fixture model. The public middleware supplies its own async tool.
    """
    return create_temporal_deep_agent(
        model="fixture:transcript", backend=DeterministicStateBackend(),
        tools=[_memory_tool("memory_put", memory_put), _memory_tool("memory_get", memory_get)],
        middleware=[StableToolMessageIDs(workflow_id), TodoListMiddleware()], checkpointer=InMemorySaver(),
        system_prompt="Follow the controlled local fault fixture; do not delegate or use shell.",
        activity_options={"start_to_close_timeout": timedelta(seconds=20),
                          "retry_policy": RetryPolicy(maximum_attempts=3)},
    )


async def restore_agent_state(agent: Any, config: dict[str, Any], state: dict[str, Any]) -> None:
    """Restore all native state, then mark the carried completed turn finished.

    On a fresh graph, update_state without as_node chooses the input writer and
    drops PlanningState.todos (OmitFromInput). The pinned factory's model writer
    accepts full state. Clearing pending routes with the public END operation
    prevents a carried after_model task from racing the next user turn.
    """
    await agent.aupdate_state(config, state, as_node="model")
    await agent.aupdate_state(config, None, as_node=END)


@workflow.defn
class DurableAgentWorkflow:
    def __init__(self) -> None:
        self._agent: Any = None
        self._config: dict[str, Any] = {}
        self._last_state: dict[str, Any] = {}
        self._identity: dict[str, str] = {}
        self._phase = 0
        self._can_count = 0
        self._status = "starting"

    def _snapshot(self) -> dict[str, Any]:
        state = self._last_state
        if self._agent is not None:
            state = dict(self._agent.get_state(self._config).values) or state
        return {
            "schema_version": SNAPSHOT_SCHEMA, "version": CODE_VERSION,
            "identity": dict(self._identity), "phase": self._phase,
            "can_count": self._can_count, "status": self._status,
            "messages": messages_to_dict(state.get("messages", [])),
            "files": dict(state.get("files", {})), "todos": list(state.get("todos", [])),
        }

    @workflow.query(name="snapshot")
    def snapshot(self) -> dict[str, Any]:
        return self._snapshot()

    @workflow.run
    async def run(self, payload: dict[str, Any]) -> dict[str, Any]:
        owner, message = payload.get("owner"), payload.get("message")
        if not isinstance(owner, str) or not owner or not isinstance(message, str) or not message:
            raise ApplicationError("Invalid trusted prototype input", non_retryable=True)
        if payload.get("version", CODE_VERSION) != CODE_VERSION:
            raise ApplicationError("Prototype code version mismatch", non_retryable=True)
        for key in ("continue_as_new", "block_after_effect"):
            if type(payload.get(key, True)) is not bool:
                raise ApplicationError("Invalid prototype option", non_retryable=True)
        workflow_id = workflow.info().workflow_id
        memory_key = "note-" + hashlib.sha256(workflow_id.encode()).hexdigest()[:24]
        self._identity = {"workflow_id": workflow_id, "owner": owner, "memory_key": memory_key}
        carried = payload.get("snapshot")
        if carried is not None:
            self._last_state = rehydrate_snapshot(carried, self._identity)
            self._can_count = carried["can_count"]
            self._phase = 2
        else:
            self._phase = 1
        self._config = {"configurable": {"thread_id": workflow_id}}
        self._agent = build_agent(workflow_id)
        control = {"prototype_phase": self._phase, "message": message, "memory_key": memory_key,
                   "block_after_effect": payload.get("block_after_effect", True)}
        # Native PlanningState marks todos OmitFromInput: a new graph's ainvoke
        # input cannot rehydrate it. Update the full checkpoint before invoking.
        if carried is not None:
            await restore_agent_state(self._agent, self._config, self._last_state)
        initial = {"messages": [HumanMessage(content=json.dumps(control, sort_keys=True),
                                            id=f"{workflow_id}:phase-{self._phase}:input")]}
        self._status = "running"
        self._last_state = dict(await self._agent.ainvoke(initial, config=self._config))
        if self._phase == 1 and payload.get("continue_as_new", True):
            self._can_count += 1
            self._status = "continuing"
            snapshot = self._snapshot()
            # Unlike the SDK helper, copy files/todos and identity/version as well
            # as the message transcript. This is a single explicit test boundary.
            workflow.continue_as_new({**payload, "snapshot": snapshot})
        self._status = "completed"
        return self._snapshot()
