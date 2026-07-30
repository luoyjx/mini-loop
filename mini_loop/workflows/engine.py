"""Bounded, deterministic async executor for validated workflow DAGs."""

from __future__ import annotations

import asyncio
import uuid
from collections.abc import Awaitable, Callable, Mapping, Sequence
from typing import Any, Protocol

from .artifacts import (
    ArtifactSubmission,
    artifact_from_submission,
    verification_status_from_value,
)
from .models import (
    HARD_MAX_CONCURRENT_AGENTS,
    Artifact,
    AttemptClaim,
    AttemptStatus,
    NodeAttempt,
    NodeKind,
    NodeState,
    NodeStatus,
    RunStatus,
    VerificationStatus,
    WorkflowNode,
    WorkflowRun,
)
from .store import InMemoryWorkflowStore, InvalidTransition, VersionConflict
from .validation import ArtifactValidationError, validate_definition


class WorkflowRunner(Protocol):
    async def __call__(
        self,
        attempt: NodeAttempt,
        node: WorkflowNode,
        inputs: Mapping[str, Any],
    ) -> ArtifactSubmission: ...


class WorkflowExecutionError(RuntimeError):
    pass


class WorkflowEngine:
    def __init__(
        self,
        store: InMemoryWorkflowStore,
        runner: WorkflowRunner,
        *,
        max_concurrent_agents: int = HARD_MAX_CONCURRENT_AGENTS,
        attempt_semaphore: asyncio.Semaphore | None = None,
    ) -> None:
        if not 0 < max_concurrent_agents <= HARD_MAX_CONCURRENT_AGENTS:
            raise ValueError(
                f"max_concurrent_agents must be between 1 and "
                f"{HARD_MAX_CONCURRENT_AGENTS}"
            )
        self.store = store
        self.runner = runner
        self.max_concurrent_agents = max_concurrent_agents
        self._attempt_semaphore = (
            attempt_semaphore
            if attempt_semaphore is not None
            else asyncio.Semaphore(max_concurrent_agents)
        )
        self._inflight: dict[str, set[asyncio.Task]] = {}
        self._execute_locks: dict[str, asyncio.Lock] = {}

    async def cancel(
        self,
        run_id: str,
        *,
        reason: str = "requested",
    ) -> WorkflowRun:
        while True:
            run = self.store.get_run(run_id)
            try:
                self.store.request_cancel(
                    run_id,
                    expected_version=run.version,
                    reason=reason,
                )
                break
            except VersionConflict:
                continue
        for task in tuple(self._inflight.get(run_id, ())):
            task.cancel()
        self.store.cancel_claimed_attempts(run_id)
        current = self.store.get_run(run_id)
        if (
            current.status == RunStatus.CANCELLING
            and not current.active_node_ids
        ):
            return self.store.finish_cancellation(run_id)
        return current

    async def execute(self, run_id: str) -> WorkflowRun:
        lock = self._execute_locks.setdefault(run_id, asyncio.Lock())
        async with lock:
            return await self._execute_locked(run_id)

    async def _execute_locked(self, run_id: str) -> WorkflowRun:
        run = self.store.get_run(run_id)
        definition = validate_definition(
            self.store.get_definition(run.definition_revision)
        )
        if run.status == RunStatus.QUEUED:
            run = self.store.transition_run(
                run_id,
                expected_version=run.version,
                to_status=RunStatus.RUNNING,
            )
        if run.is_terminal:
            return run
        if run.status not in {RunStatus.RUNNING, RunStatus.CANCELLING}:
            raise WorkflowExecutionError(f"run cannot execute from {run.status.value}")

        while True:
            run = self.store.get_run(run_id)
            states = {state.node_id: state for state in self.store.list_nodes(run_id)}

            if run.status == RunStatus.CANCELLING:
                if not any(state.status == NodeStatus.RUNNING for state in states.values()):
                    return self.store.finish_cancellation(run_id)
                await asyncio.sleep(0)
                continue
            if run.is_terminal:
                return run
            failed_nodes = [
                state for state in states.values()
                if state.status == NodeStatus.FAILED
            ]
            if failed_nodes:
                detail = "; ".join(
                    f"{state.node_id}: {state.error or 'failed'}"
                    for state in failed_nodes
                )
                return self.store.fail_run(
                    run_id,
                    error=f"workflow node failed: {detail}",
                )

            if all(state.status.satisfies_dependency for state in states.values()):
                final_state = states[definition.return_from]
                if not final_state.result_artifact_ids:
                    return self.store.fail_run(
                        run_id, error="return node produced no artifact"
                    )
                return self.store.finalize_run(
                    run_id,
                    expected_version=run.version,
                    final_artifact_id=final_state.result_artifact_ids[-1],
                )

            runnable = [
                node
                for node in definition.nodes
                if states[node.id].status == NodeStatus.PENDING
                and all(states[dependency].status.satisfies_dependency for dependency in node.needs)
            ]
            if not runnable:
                return self.store.fail_run(run_id, error="workflow graph is deadlocked")

            remaining_budget = definition.budget.max_agents - run.attempts_used
            limit = min(
                self.max_concurrent_agents,
                definition.budget.max_concurrent_agents,
                remaining_budget,
                len(runnable),
            )
            if limit <= 0:
                return self.store.fail_run(run_id, error="workflow attempt budget exhausted")
            selected = runnable[:limit]
            attempts_by_node = {
                attempt.node_id: attempt for attempt in self.store.list_attempts(run_id)
            }
            claims = []
            for offset, node in enumerate(selected):
                parent_agent_id = None
                if node.kind == NodeKind.VERIFY and node.needs:
                    parent = attempts_by_node.get(node.needs[0])
                    parent_agent_id = parent.agent_id if parent else None
                claims.append(
                    AttemptClaim(
                        node_id=node.id,
                        agent_id=f"wfagent_{node.id}_{uuid.uuid4().hex[:10]}",
                        parent_agent_id=parent_agent_id,
                        spawn_index=run.attempts_used + offset,
                    )
                )
            try:
                attempts = self.store.claim_nodes(
                    run_id, claims, expected_version=run.version
                )
            except VersionConflict:
                continue

            tasks = [
                asyncio.create_task(
                    self._execute_attempt(
                        attempt,
                        next(node for node in selected if node.id == attempt.node_id),
                    )
                )
                for attempt in attempts
            ]
            self._inflight[run_id] = set(tasks)
            try:
                await asyncio.gather(*tasks, return_exceptions=True)
            finally:
                self._inflight.pop(run_id, None)

    def _inputs_for(self, attempt: NodeAttempt, node: WorkflowNode) -> dict[str, Any]:
        inputs: dict[str, Any] = {
            "args": dict(self.store.get_run(attempt.run_id).args)
        }
        for dependency in node.needs:
            artifacts = self.store.artifacts_for_node(attempt.run_id, dependency)
            inputs[dependency] = (
                artifacts[-1].value if len(artifacts) == 1
                else [artifact.value for artifact in artifacts]
            )
        return inputs

    async def _execute_attempt(
        self,
        claimed: NodeAttempt,
        node: WorkflowNode,
    ) -> None:
        attempt = self.store.start_attempt(
            claimed.attempt_id, expected_version=claimed.version
        )
        try:
            # This semaphore is shared by all runs in a service. Integrations
            # can inject the same semaphore into multiple services/managers
            # when they need an application-wide workflow-agent ceiling.
            async with self._attempt_semaphore:
                submission = await self.runner(
                    attempt,
                    node,
                    self._inputs_for(attempt, node),
                )
            verification = (
                verification_status_from_value(submission.value)
                if node.kind == NodeKind.VERIFY
                else VerificationStatus.NOT_APPLICABLE
            )
            artifact = artifact_from_submission(
                submission,
                attempt=attempt,
                node=node,
                verification_status=verification,
            )
            node_status = (
                NodeStatus.UNVERIFIED
                if verification == VerificationStatus.UNVERIFIED
                else NodeStatus.SUCCEEDED
            )
            self.store.commit_attempt(
                attempt.attempt_id,
                expected_version=attempt.version,
                attempt_status=AttemptStatus.SUCCEEDED,
                node_status=node_status,
                artifact=artifact,
                verification_status=verification,
            )
        except asyncio.CancelledError:
            self.store.commit_attempt(
                attempt.attempt_id,
                expected_version=attempt.version,
                attempt_status=AttemptStatus.CANCELLED,
                node_status=NodeStatus.CANCELLED,
                error="cancelled",
            )
            raise
        except Exception as error:
            if node.kind == NodeKind.VERIFY:
                reason = f"{type(error).__name__}: {error}"
                fallback = Artifact.create(
                    run_id=attempt.run_id,
                    node_id=attempt.node_id,
                    attempt_id=attempt.attempt_id,
                    value={
                        "status": "unverified",
                        "claim_id": node.needs[0] if node.needs else node.id,
                        "evidence": [],
                        "reason": reason,
                    },
                    schema=node.output_schema,
                    verification_status=VerificationStatus.UNVERIFIED,
                    schema_valid=False,
                )
                self.store.commit_attempt(
                    attempt.attempt_id,
                    expected_version=attempt.version,
                    attempt_status=AttemptStatus.FAILED,
                    node_status=NodeStatus.UNVERIFIED,
                    artifact=fallback,
                    verification_status=VerificationStatus.UNVERIFIED,
                    error=reason,
                )
                return
            self.store.commit_attempt(
                attempt.attempt_id,
                expected_version=attempt.version,
                attempt_status=AttemptStatus.FAILED,
                node_status=NodeStatus.FAILED,
                error=f"{type(error).__name__}: {error}",
            )
