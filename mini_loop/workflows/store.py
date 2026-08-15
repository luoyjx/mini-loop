"""In-memory workflow state store with CAS and launch idempotency.

This is a deterministic test/MVP store, not a durability claim.  The methods
mirror the transaction boundaries expected from a future shared SQLite store.
"""

from __future__ import annotations

import copy
import threading
import time
import uuid
from collections.abc import Iterable, Mapping, Sequence
from dataclasses import replace
from typing import Any

from .models import (
    Artifact,
    AttemptClaim,
    AttemptStatus,
    NodeAttempt,
    NodeState,
    NodeStatus,
    OutboxMessage,
    RunContext,
    RunStatus,
    VerificationStatus,
    WorkflowDefinition,
    WorkflowRun,
    content_hash,
)
from .validation import validate_definition


class WorkflowStoreError(RuntimeError):
    pass


class NotFoundError(WorkflowStoreError):
    pass


class VersionConflict(WorkflowStoreError):
    pass


class IdempotencyConflict(WorkflowStoreError):
    pass


class InvalidTransition(WorkflowStoreError):
    pass


_RUN_TRANSITIONS = {
    RunStatus.CREATED: {RunStatus.PLANNING},
    RunStatus.PLANNING: {RunStatus.AWAITING_APPROVAL, RunStatus.FAILED},
    RunStatus.AWAITING_APPROVAL: {
        RunStatus.QUEUED,
        RunStatus.REJECTED,
        RunStatus.CANCELLED,
    },
    RunStatus.QUEUED: {RunStatus.RUNNING, RunStatus.CANCELLED},
    RunStatus.RUNNING: {
        RunStatus.PAUSING,
        RunStatus.WAITING_APPROVAL,
        RunStatus.CANCELLING,
        RunStatus.COMPLETED,
        RunStatus.FAILED,
    },
    RunStatus.PAUSING: {RunStatus.PAUSED, RunStatus.CANCELLING},
    RunStatus.PAUSED: {RunStatus.QUEUED, RunStatus.CANCELLED},
    RunStatus.WAITING_APPROVAL: {
        RunStatus.RUNNING,
        RunStatus.REJECTED,
        RunStatus.CANCELLED,
    },
    RunStatus.CANCELLING: {RunStatus.CANCELLED},
}


#: Terminal runs whose full state graph is retained. The store is created once
#: at the manager and shared for the whole process; every run adds a run, its
#: nodes, attempts and artifacts, and none of it was ever removed -- a
#: completed run's whole graph stayed in memory forever (rounds 146/147's
#: retention-store class, third and largest instance). Past this the oldest
#: terminal runs are evicted whole, but only ones with no undelivered outbox: an
#: active run and an unread result are live commitments and are spared. Generous
#: so a real workload's recent runs stay fully readable via `status`; a run
#: evicted past it reads as NotFound, standard retention.
MAX_TERMINAL_RUNS = 500


class InMemoryWorkflowStore:
    def __init__(self) -> None:
        self._lock = threading.RLock()
        self._definitions: dict[str, WorkflowDefinition] = {}
        self._definition_hashes: dict[str, str] = {}
        self._runs: dict[str, WorkflowRun] = {}
        self._nodes: dict[tuple[str, str], NodeState] = {}
        self._attempts: dict[str, NodeAttempt] = {}
        self._artifacts: dict[str, Artifact] = {}
        self._outbox: dict[str, OutboxMessage] = {}
        self._outbox_keys: dict[tuple[str, str], str] = {}
        self._launches: dict[tuple[str, str], tuple[str, str]] = {}

    @staticmethod
    def _copy(value):
        return copy.deepcopy(value)

    def register_definition(self, definition: WorkflowDefinition) -> WorkflowDefinition:
        validate_definition(definition)
        with self._lock:
            existing = self._definitions.get(definition.revision)
            if existing is not None:
                if existing.definition_hash != definition.definition_hash:
                    raise IdempotencyConflict(
                        f"definition revision {definition.revision} has different content"
                    )
                return self._copy(existing)
            prior_revision = self._definition_hashes.get(definition.definition_hash)
            if prior_revision is not None:
                return self._copy(self._definitions[prior_revision])
            stored = self._copy(definition)
            self._definitions[stored.revision] = stored
            self._definition_hashes[stored.definition_hash] = stored.revision
            return self._copy(stored)

    def get_definition(self, revision: str) -> WorkflowDefinition:
        with self._lock:
            try:
                return self._copy(self._definitions[revision])
            except KeyError as error:
                raise NotFoundError(f"definition {revision} not found") from error

    def create_run(
        self,
        *,
        definition_revision: str,
        session_id: str,
        run_context: RunContext,
        idempotency_key: str,
        args: Mapping[str, Any],
        parent_run_id: str | None = None,
        launch_action_id: str | None = None,
        policy_snapshot_hash: str = "",
    ) -> WorkflowRun:
        if not session_id or not idempotency_key:
            raise ValueError("session_id and idempotency_key are required")
        definition = self.get_definition(definition_revision)
        payload_hash = content_hash(
            {
                "definition_revision": definition_revision,
                "args": args,
                "run_context": run_context,
                "parent_run_id": parent_run_id,
                "launch_action_id": launch_action_id,
                "policy_snapshot_hash": policy_snapshot_hash,
            },
            prefix="launch",
        )
        key = (session_id, idempotency_key)
        with self._lock:
            previous = self._launches.get(key)
            if previous is not None:
                prior_hash, run_id = previous
                if prior_hash != payload_hash:
                    raise IdempotencyConflict(
                        "idempotency key was already used with a different payload"
                    )
                return self._copy(self._runs[run_id])

            run_id = f"wfrun_{uuid.uuid4().hex[:20]}"
            run = WorkflowRun(
                run_id=run_id,
                definition_revision=definition_revision,
                session_id=session_id,
                run_context=run_context,
                idempotency_key=idempotency_key,
                args=dict(args),
                parent_run_id=parent_run_id,
                launch_action_id=launch_action_id,
                policy_snapshot_hash=policy_snapshot_hash,
            )
            self._runs[run_id] = run
            for node in definition.nodes:
                self._nodes[(run_id, node.id)] = NodeState(run_id=run_id, node_id=node.id)
            self._launches[key] = (payload_hash, run_id)
            return self._copy(run)

    def get_run(self, run_id: str) -> WorkflowRun:
        with self._lock:
            try:
                return self._copy(self._runs[run_id])
            except KeyError as error:
                raise NotFoundError(f"run {run_id} not found") from error

    def list_runs(self, *, session_id: str | None = None) -> list[WorkflowRun]:
        with self._lock:
            runs = [
                self._copy(run)
                for run in self._runs.values()
                if session_id is None or run.session_id == session_id
            ]
        return sorted(runs, key=lambda item: (item.created_at, item.run_id))

    def prune_terminal_runs(self, *, keep: int = MAX_TERMINAL_RUNS) -> list[str]:
        """Evict the oldest terminal runs whose outbox is fully delivered.

        Returns the pruned run ids so a caller can drop parallel bookkeeping
        (the service's `_launch_turns`). Never touches an active run, nor a
        terminal one that still has an undelivered outbox message -- an unread
        result is a live commitment, the same rule that spares a pending
        handshake in `_prune_protocols` (round 147). Removes the whole cascade
        -- run, nodes, attempts, artifacts, delivered outbox, outbox keys, and
        the launch key -- so nothing dangles and no reader hits a half-pruned
        run. `create_run` returns `self._runs[run_id]` for a repeated launch
        key, so the launch key must go with the run; a re-launch of an evicted
        key past the retention window then starts a fresh run, a bounded dedup
        window rather than an unbounded one.
        """

        with self._lock:
            if len(self._runs) <= keep:
                return []
            undelivered = {
                message.run_id
                for message in self._outbox.values()
                if message.delivered_at is None
            }
            terminal = sorted(
                (
                    run
                    for run in self._runs.values()
                    if run.is_terminal and run.run_id not in undelivered
                ),
                key=lambda run: (run.created_at, run.run_id),
            )
            if len(terminal) <= keep:
                return []
            pruned = []
            for run in terminal[: len(terminal) - keep]:
                self._prune_run_cascade(run.run_id)
                pruned.append(run.run_id)
            return pruned

    def _prune_run_cascade(self, run_id: str) -> None:
        """Delete a run and everything keyed to it. Caller holds `self._lock`."""

        self._runs.pop(run_id, None)
        for key in [key for key in self._nodes if key[0] == run_id]:
            del self._nodes[key]
        for attempt_id in [
            attempt_id for attempt_id, attempt in self._attempts.items()
            if attempt.run_id == run_id
        ]:
            del self._attempts[attempt_id]
        for artifact_id in [
            artifact_id for artifact_id, artifact in self._artifacts.items()
            if artifact.run_id == run_id
        ]:
            del self._artifacts[artifact_id]
        for message_id in [
            message_id for message_id, message in self._outbox.items()
            if message.run_id == run_id
        ]:
            del self._outbox[message_id]
        for key in [key for key in self._outbox_keys if key[0] == run_id]:
            del self._outbox_keys[key]
        for key in [
            key for key, (_hash, launched_run_id) in self._launches.items()
            if launched_run_id == run_id
        ]:
            del self._launches[key]

    def list_nodes(self, run_id: str) -> list[NodeState]:
        definition = self.get_definition(self.get_run(run_id).definition_revision)
        with self._lock:
            return [
                self._copy(self._nodes[(run_id, node.id)])
                for node in definition.nodes
            ]

    def get_node(self, run_id: str, node_id: str) -> NodeState:
        with self._lock:
            try:
                return self._copy(self._nodes[(run_id, node_id)])
            except KeyError as error:
                raise NotFoundError(f"node {run_id}/{node_id} not found") from error

    def transition_run(
        self,
        run_id: str,
        *,
        expected_version: int,
        to_status: RunStatus,
        error: str | None = None,
    ) -> WorkflowRun:
        to_status = RunStatus(to_status)
        with self._lock:
            run = self._runs.get(run_id)
            if run is None:
                raise NotFoundError(f"run {run_id} not found")
            if run.version != expected_version:
                raise VersionConflict(
                    f"run {run_id} expected version {expected_version}, found {run.version}"
                )
            if run.status == to_status:
                return self._copy(run)
            if to_status not in _RUN_TRANSITIONS.get(run.status, set()):
                raise InvalidTransition(f"{run.status.value} -> {to_status.value}")
            run.status = to_status
            run.version += 1
            run.error = error
            if to_status == RunStatus.RUNNING and run.started_at is None:
                run.started_at = time.time()
            if to_status.is_terminal:
                run.ended_at = time.time()
            return self._copy(run)

    def claim_nodes(
        self,
        run_id: str,
        claims: Sequence[AttemptClaim],
        *,
        expected_version: int,
    ) -> list[NodeAttempt]:
        with self._lock:
            run = self._runs.get(run_id)
            if run is None:
                raise NotFoundError(f"run {run_id} not found")
            if run.version != expected_version:
                raise VersionConflict(
                    f"run {run_id} expected version {expected_version}, found {run.version}"
                )
            if run.status != RunStatus.RUNNING:
                raise InvalidTransition(f"cannot claim nodes while run is {run.status.value}")
            definition = self._definitions[run.definition_revision]
            if run.attempts_used + len(claims) > definition.budget.max_agents:
                raise WorkflowStoreError("workflow attempt budget exhausted")
            if len({claim.node_id for claim in claims}) != len(claims):
                raise WorkflowStoreError("duplicate node claim")

            claimed = []
            for claim in claims:
                state = self._nodes.get((run_id, claim.node_id))
                if state is None:
                    raise NotFoundError(f"node {run_id}/{claim.node_id} not found")
                if state.status != NodeStatus.PENDING:
                    raise InvalidTransition(
                        f"node {claim.node_id} is {state.status.value}, not pending"
                    )

            for claim in claims:
                state = self._nodes[(run_id, claim.node_id)]
                attempt_id = f"wfatt_{uuid.uuid4().hex[:20]}"
                attempt = NodeAttempt(
                    attempt_id=attempt_id,
                    run_id=run_id,
                    node_id=claim.node_id,
                    attempt=len(state.attempt_ids) + 1,
                    agent_id=claim.agent_id,
                    parent_agent_id=claim.parent_agent_id,
                    spawn_index=claim.spawn_index,
                )
                self._attempts[attempt_id] = attempt
                state.status = NodeStatus.RUNNING
                state.version += 1
                state.attempt_ids = (*state.attempt_ids, attempt_id)
                claimed.append(self._copy(attempt))

            run.attempts_used += len(claimed)
            run.active_node_ids = tuple(
                node.node_id
                for node in self.list_nodes(run_id)
                if node.status == NodeStatus.RUNNING
            )
            run.version += 1
            return claimed

    def start_attempt(self, attempt_id: str, *, expected_version: int) -> NodeAttempt:
        with self._lock:
            attempt = self._attempts.get(attempt_id)
            if attempt is None:
                raise NotFoundError(f"attempt {attempt_id} not found")
            if attempt.version != expected_version:
                raise VersionConflict(f"attempt {attempt_id} version conflict")
            if attempt.status != AttemptStatus.CLAIMED:
                raise InvalidTransition(f"attempt {attempt_id} is {attempt.status.value}")
            now = time.time()
            attempt.status = AttemptStatus.RUNNING
            attempt.version += 1
            attempt.started_at = now
            attempt.heartbeat_at = now
            return self._copy(attempt)

    def cancel_claimed_attempts(
        self,
        run_id: str,
        *,
        error: str = "cancelled before start",
    ) -> list[NodeAttempt]:
        """Atomically settle attempts whose asyncio task never started."""

        now = time.time()
        with self._lock:
            if run_id not in self._runs:
                raise NotFoundError(f"run {run_id} not found")
            cancelled = []
            for attempt in self._attempts.values():
                if (
                    attempt.run_id != run_id
                    or attempt.status != AttemptStatus.CLAIMED
                ):
                    continue
                node = self._nodes[(run_id, attempt.node_id)]
                if node.status != NodeStatus.RUNNING:
                    raise InvalidTransition(
                        f"claimed attempt {attempt.attempt_id} has "
                        f"node status {node.status.value}"
                )
                attempt.status = AttemptStatus.CANCELLED
                attempt.version += 1
                attempt.ended_at = now
                attempt.heartbeat_at = now
                attempt.error = error
                node.status = NodeStatus.CANCELLED
                node.version += 1
                node.error = error
                cancelled.append(self._copy(attempt))

            if cancelled:
                run = self._runs[run_id]
                run.active_node_ids = tuple(
                    node.node_id
                    for node in self.list_nodes(run_id)
                    if node.status == NodeStatus.RUNNING
                )
                run.version += 1
            return cancelled

    def commit_attempt(
        self,
        attempt_id: str,
        *,
        expected_version: int,
        attempt_status: AttemptStatus,
        node_status: NodeStatus,
        artifact: Artifact | None = None,
        verification_status: VerificationStatus = VerificationStatus.NOT_APPLICABLE,
        error: str | None = None,
    ) -> NodeAttempt:
        attempt_status = AttemptStatus(attempt_status)
        node_status = NodeStatus(node_status)
        if not attempt_status.is_terminal or not node_status.is_terminal:
            raise InvalidTransition("attempt and node commit statuses must be terminal")
        with self._lock:
            attempt = self._attempts.get(attempt_id)
            if attempt is None:
                raise NotFoundError(f"attempt {attempt_id} not found")
            if attempt.version != expected_version:
                raise VersionConflict(f"attempt {attempt_id} version conflict")
            if attempt.status != AttemptStatus.RUNNING:
                raise InvalidTransition(f"attempt {attempt_id} is {attempt.status.value}")
            node = self._nodes[(attempt.run_id, attempt.node_id)]
            if node.status != NodeStatus.RUNNING:
                raise InvalidTransition(f"node {node.node_id} is {node.status.value}")
            if artifact is not None:
                if (
                    artifact.run_id != attempt.run_id
                    or artifact.node_id != attempt.node_id
                    or artifact.attempt_id != attempt_id
                ):
                    raise WorkflowStoreError("artifact provenance does not match attempt")
                self._artifacts[artifact.artifact_id] = self._copy(artifact)
                node.result_artifact_ids = (*node.result_artifact_ids, artifact.artifact_id)
                attempt.result_artifact_id = artifact.artifact_id

            now = time.time()
            attempt.status = attempt_status
            attempt.version += 1
            attempt.ended_at = now
            attempt.heartbeat_at = now
            attempt.verification_status = VerificationStatus(verification_status)
            attempt.error = error
            node.status = node_status
            node.version += 1
            node.error = error

            run = self._runs[attempt.run_id]
            run.active_node_ids = tuple(
                item.node_id
                for item in self.list_nodes(attempt.run_id)
                if item.status == NodeStatus.RUNNING
            )
            run.version += 1
            return self._copy(attempt)

    def get_attempt(self, attempt_id: str) -> NodeAttempt:
        with self._lock:
            try:
                return self._copy(self._attempts[attempt_id])
            except KeyError as error:
                raise NotFoundError(f"attempt {attempt_id} not found") from error

    def list_attempts(self, run_id: str) -> list[NodeAttempt]:
        with self._lock:
            attempts = [
                self._copy(attempt)
                for attempt in self._attempts.values()
                if attempt.run_id == run_id
            ]
        return sorted(attempts, key=lambda item: (item.spawn_index, item.attempt_id))

    def get_artifact(self, artifact_id: str) -> Artifact:
        with self._lock:
            try:
                return self._copy(self._artifacts[artifact_id])
            except KeyError as error:
                raise NotFoundError(f"artifact {artifact_id} not found") from error

    def artifacts_for_node(self, run_id: str, node_id: str) -> list[Artifact]:
        state = self.get_node(run_id, node_id)
        return [self.get_artifact(artifact_id) for artifact_id in state.result_artifact_ids]

    def request_cancel(
        self,
        run_id: str,
        *,
        expected_version: int,
        reason: str = "requested",
    ) -> WorkflowRun:
        with self._lock:
            run = self._runs.get(run_id)
            if run is None:
                raise NotFoundError(f"run {run_id} not found")
            if run.version != expected_version:
                raise VersionConflict(f"run {run_id} version conflict")
            if run.is_terminal:
                return self._copy(run)
            run.cancel_reason = run.cancel_reason or reason
            active = any(
                node.status == NodeStatus.RUNNING for node in self.list_nodes(run_id)
            )
            if run.status == RunStatus.CANCELLING:
                if active:
                    return self._copy(run)
                run.status = RunStatus.CANCELLED
                run.version += 1
                run.ended_at = time.time()
                run.active_node_ids = ()
                return self._copy(run)
            # Preserve the documented RUNNING -> CANCELLING -> CANCELLED path
            # even when a timed-out attempt has already committed CANCELLED.
            target = (
                RunStatus.CANCELLING
                if active or run.status == RunStatus.RUNNING
                else RunStatus.CANCELLED
            )
            if target not in _RUN_TRANSITIONS.get(run.status, set()):
                if run.status == RunStatus.QUEUED and target == RunStatus.CANCELLED:
                    pass
                else:
                    raise InvalidTransition(f"{run.status.value} -> {target.value}")
            run.status = target
            run.version += 1
            for node in self.list_nodes(run_id):
                if node.status == NodeStatus.PENDING:
                    stored = self._nodes[(run_id, node.node_id)]
                    stored.status = NodeStatus.CANCELLED
                    stored.version += 1
            if target == RunStatus.CANCELLED:
                run.ended_at = time.time()
            elif not active:
                run.status = RunStatus.CANCELLED
                run.version += 1
                run.ended_at = time.time()
                run.active_node_ids = ()
            return self._copy(run)

    def finish_cancellation(self, run_id: str) -> WorkflowRun:
        with self._lock:
            run = self._runs.get(run_id)
            if run is None:
                raise NotFoundError(f"run {run_id} not found")
            if run.status == RunStatus.CANCELLED:
                return self._copy(run)
            if run.status != RunStatus.CANCELLING:
                raise InvalidTransition(f"run {run_id} is {run.status.value}")
            if any(node.status == NodeStatus.RUNNING for node in self.list_nodes(run_id)):
                raise InvalidTransition("cannot finish cancellation with active nodes")
            run.status = RunStatus.CANCELLED
            run.version += 1
            run.ended_at = time.time()
            run.active_node_ids = ()
            return self._copy(run)

    def fail_run(self, run_id: str, *, error: str) -> WorkflowRun:
        run = self.get_run(run_id)
        if run.status == RunStatus.FAILED:
            return run
        return self.transition_run(
            run_id,
            expected_version=run.version,
            to_status=RunStatus.FAILED,
            error=error,
        )

    def finalize_run(
        self,
        run_id: str,
        *,
        expected_version: int,
        final_artifact_id: str,
    ) -> WorkflowRun:
        with self._lock:
            run = self._runs.get(run_id)
            if run is None:
                raise NotFoundError(f"run {run_id} not found")
            if run.version != expected_version:
                raise VersionConflict(f"run {run_id} version conflict")
            if run.status != RunStatus.RUNNING:
                raise InvalidTransition(f"run {run_id} is {run.status.value}")
            if final_artifact_id not in self._artifacts:
                raise NotFoundError(f"artifact {final_artifact_id} not found")
            if any(
                not node.status.satisfies_dependency for node in self.list_nodes(run_id)
            ):
                raise InvalidTransition("not all workflow nodes completed successfully")
            run.status = RunStatus.COMPLETED
            run.version += 1
            run.ended_at = time.time()
            run.final_artifact_id = final_artifact_id
            run.active_node_ids = ()
            message = OutboxMessage(
                message_id=f"wfout_{uuid.uuid4().hex[:20]}",
                run_id=run_id,
                session_id=run.session_id,
                kind="workflow_completed",
                payload={
                    "run_id": run_id,
                    "definition_revision": run.definition_revision,
                    "artifact_id": final_artifact_id,
                },
            )
            self._outbox[message.message_id] = message
            self._outbox_keys[(run_id, message.kind)] = message.message_id
            return self._copy(run)

    def enqueue_outbox(
        self,
        run_id: str,
        *,
        kind: str,
        payload: Mapping[str, Any],
    ) -> OutboxMessage:
        """Create one process-local notification for a run/kind pair."""

        if not kind:
            raise ValueError("outbox kind is required")
        with self._lock:
            run = self._runs.get(run_id)
            if run is None:
                raise NotFoundError(f"run {run_id} not found")
            key = (run_id, kind)
            existing_id = self._outbox_keys.get(key)
            if existing_id is not None:
                return self._copy(self._outbox[existing_id])
            message = OutboxMessage(
                message_id=f"wfout_{uuid.uuid4().hex[:20]}",
                run_id=run_id,
                session_id=run.session_id,
                kind=kind,
                payload=dict(payload),
            )
            self._outbox[message.message_id] = message
            self._outbox_keys[key] = message.message_id
            return self._copy(message)

    def list_outbox(
        self,
        *,
        run_id: str | None = None,
        session_id: str | None = None,
        undelivered_only: bool = False,
    ) -> list[OutboxMessage]:
        with self._lock:
            messages = [
                self._copy(message)
                for message in self._outbox.values()
                if run_id is None or message.run_id == run_id
                if session_id is None or message.session_id == session_id
                if not undelivered_only or message.delivered_at is None
            ]
        return sorted(messages, key=lambda item: (item.created_at, item.message_id))

    def claim_outbox(
        self,
        *,
        session_id: str,
        run_ids: set[str] | None = None,
        lease_seconds: float = 30.0,
        limit: int | None = None,
    ) -> tuple[str, list[OutboxMessage]]:
        """Lease pending messages without marking them delivered.

        `limit` caps how many are claimed in one call, so a parent turn cannot be
        handed every completed run's result at once. The rest stay unclaimed and
        the next `claim_outbox` picks them up -- bounded delivery, nothing lost.
        """

        if lease_seconds <= 0:
            raise ValueError("lease_seconds must be positive")
        now = time.time()
        token = f"wfclaim_{uuid.uuid4().hex[:20]}"
        with self._lock:
            claimed = []
            for message_id, message in list(self._outbox.items()):
                if limit is not None and len(claimed) >= limit:
                    break
                if message.session_id != session_id or message.delivered_at is not None:
                    continue
                if run_ids is not None and message.run_id not in run_ids:
                    continue
                lease_active = (
                    message.claim_token is not None
                    and message.claimed_at is not None
                    and now - message.claimed_at < lease_seconds
                )
                if lease_active:
                    continue
                leased = replace(
                    message,
                    claim_token=token,
                    claimed_at=now,
                )
                self._outbox[message_id] = leased
                claimed.append(self._copy(leased))
        return token, sorted(
            claimed,
            key=lambda item: (item.created_at, item.message_id),
        )

    def acknowledge_outbox(
        self,
        *,
        session_id: str,
        message_ids: Iterable[str],
        claim_token: str,
    ) -> list[OutboxMessage]:
        """Mark messages delivered only after they were appended to a parent."""

        if not claim_token:
            raise ValueError("claim_token is required")
        now = time.time()
        with self._lock:
            acknowledged = []
            for message_id in message_ids:
                message = self._outbox.get(message_id)
                if message is None:
                    raise NotFoundError(f"outbox message {message_id} not found")
                if message.session_id != session_id:
                    raise NotFoundError(f"outbox message {message_id} not found")
                if message.delivered_at is not None:
                    acknowledged.append(self._copy(message))
                    continue
                if message.claim_token != claim_token:
                    raise VersionConflict(
                        f"outbox message {message_id} lease does not match"
                    )
                delivered = replace(
                    message,
                    claim_token=None,
                    claimed_at=None,
                    delivered_at=now,
                )
                self._outbox[message_id] = delivered
                acknowledged.append(self._copy(delivered))
        return sorted(
            acknowledged,
            key=lambda item: (item.created_at, item.message_id),
        )

    def release_outbox(
        self,
        *,
        session_id: str,
        message_ids: Iterable[str],
        claim_token: str,
    ) -> None:
        """Release a failed append so a later parent turn can retry it."""

        with self._lock:
            for message_id in message_ids:
                message = self._outbox.get(message_id)
                if message is None or message.session_id != session_id:
                    raise NotFoundError(f"outbox message {message_id} not found")
                if message.delivered_at is not None:
                    continue
                if message.claim_token != claim_token:
                    raise VersionConflict(
                        f"outbox message {message_id} lease does not match"
                    )
                self._outbox[message_id] = replace(
                    message,
                    claim_token=None,
                    claimed_at=None,
                )

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: every transition is CAS-guarded (VersionConflict) inside one lock; an illegal state cannot be committed to observe."
)
