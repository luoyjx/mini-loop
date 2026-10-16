"""Library-only contracts, not a durable queue/admission API.

A future TaskControlPlane must persist these records, enforce admission uniqueness
and perform fenced updates transactionally. Execution adapters own runtime work;
these types provide no scheduler, event log, database, lease service or exactly-once
side effects. Server time and the active lease must come from authoritative storage.
"""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass, replace
from datetime import datetime
from enum import StrEnum
from typing import Any, Literal, Protocol


def _text(value: str) -> None:
    if not isinstance(value, str) or not value.strip():
        raise ValueError("Identifiers must be nonempty strings")


def _positive(value: int) -> None:
    if type(value) is not int or value <= 0:
        raise ValueError("Ordinal and epoch values must be positive integers")


def _digest(value: str) -> None:
    if not isinstance(value, str) or len(value) != 64 or any(c not in "0123456789abcdef" for c in value):
        raise ValueError("payload_sha256 must be a lowercase SHA-256 hexadecimal digest")


class TaskStatus(StrEnum):
    ACCEPTED = "accepted"
    DISPATCHING = "dispatching"
    DISPATCH_UNKNOWN = "dispatch_unknown"
    QUEUED = "queued"
    RUNNING = "running"
    CANCEL_REQUESTED = "cancel_requested"
    RETRY_WAIT = "retry_wait"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELLED = "cancelled"
    TIMED_OUT = "timed_out"


TERMINAL_TASK_STATUSES = frozenset({TaskStatus.SUCCEEDED, TaskStatus.FAILED, TaskStatus.CANCELLED, TaskStatus.TIMED_OUT})


@dataclass(frozen=True)
class Task:
    task_id: str
    owner: str
    logical_operation_id: str
    payload_sha256: str
    status: TaskStatus = TaskStatus.ACCEPTED

    def __post_init__(self) -> None:
        for value in (self.task_id, self.owner, self.logical_operation_id):
            _text(value)
        _digest(self.payload_sha256)
        if self.task_id == self.logical_operation_id or not isinstance(self.status, TaskStatus):
            raise ValueError("Task requires distinct task/operation IDs and a TaskStatus")


@dataclass(frozen=True)
class ExecutionRef:
    backend: str
    execution_id: str
    scope_id: str | None = None  # e.g. Agent Server thread_id; execution_id is run_id.

    def __post_init__(self) -> None:
        for value in (self.backend, self.execution_id):
            _text(value)
        if self.scope_id is not None:
            _text(self.scope_id)


@dataclass(frozen=True)
class Attempt:
    attempt_id: str
    task_id: str
    logical_operation_id: str  # Stable across retries; never replace it with attempt_id.
    ordinal: int
    epoch: int
    worker_id: str
    execution: ExecutionRef | None = None

    def __post_init__(self) -> None:
        for value in (self.attempt_id, self.task_id, self.logical_operation_id, self.worker_id):
            _text(value)
        _positive(self.ordinal)
        _positive(self.epoch)
        if len({self.attempt_id, self.task_id, self.logical_operation_id}) != 3:
            raise ValueError("Task, attempt and logical operation IDs must be distinct")
        if self.execution is not None and not isinstance(self.execution, ExecutionRef):
            raise ValueError("Attempt execution must be an immutable ExecutionRef")


@dataclass(frozen=True)
class BackendCapabilities:
    backend: str
    idempotent_submit: bool
    authoritative_absence: bool
    checkpoint_owner: Literal["runtime", "workflow_history", "none"]
    checkpoint_durability: Literal["none", "architecture_available_unverified", "verified"]
    supports_cancel: bool
    supports_resume: bool

    def __post_init__(self) -> None:
        _text(self.backend)
        if any(type(value) is not bool for value in (
            self.idempotent_submit, self.authoritative_absence, self.supports_cancel, self.supports_resume,
        )):
            raise ValueError("Capability flags must be booleans")
        if self.checkpoint_owner not in {"runtime", "workflow_history", "none"} or self.checkpoint_durability not in {
            "none", "architecture_available_unverified", "verified",
        }:
            raise ValueError("Invalid checkpoint capability")


LANGGRAPH_AGENT_SERVER_PROFILE = BackendCapabilities(
    backend="langgraph-agent-server", idempotent_submit=False, authoritative_absence=False,
    checkpoint_owner="runtime", checkpoint_durability="architecture_available_unverified",
    supports_cancel=True, supports_resume=True,
)


@dataclass(frozen=True)
class AdmissionKey:
    owner: str
    idempotency_key: str
    payload_sha256: str

    def __post_init__(self) -> None:
        _text(self.owner)
        _text(self.idempotency_key)
        _digest(self.payload_sha256)


def admission_decision(existing: AdmissionKey | None, incoming: AdmissionKey) -> Literal["create", "reuse", "conflict"]:
    """Decision only: future storage needs UNIQUE(owner, idempotency_key).

    Hash equality must use a specified canonical payload encoding. A read followed
    by an insert is insufficient; uniqueness and the stored digest decide the race.
    """
    if existing is None or (existing.owner, existing.idempotency_key) != (incoming.owner, incoming.idempotency_key):
        return "create"
    return "reuse" if existing.payload_sha256 == incoming.payload_sha256 else "conflict"


@dataclass(frozen=True)
class Reconciliation:
    outcome: Literal["found", "absent", "unknown"]
    execution: ExecutionRef | None = None

    def __post_init__(self) -> None:
        if self.outcome not in {"found", "absent", "unknown"} or (
            (self.outcome == "found") != isinstance(self.execution, ExecutionRef)
        ) or (self.outcome != "found" and self.execution is not None):
            raise ValueError("Only a found reconciliation carries an ExecutionRef")


_NEXT = {
    TaskStatus.ACCEPTED: {TaskStatus.DISPATCHING, TaskStatus.CANCELLED},
    TaskStatus.DISPATCHING: {TaskStatus.DISPATCH_UNKNOWN, TaskStatus.QUEUED, TaskStatus.RUNNING, TaskStatus.RETRY_WAIT, *TERMINAL_TASK_STATUSES},
    TaskStatus.DISPATCH_UNKNOWN: {TaskStatus.DISPATCHING, TaskStatus.QUEUED, TaskStatus.RUNNING, TaskStatus.CANCEL_REQUESTED, *TERMINAL_TASK_STATUSES},
    TaskStatus.QUEUED: {TaskStatus.RUNNING, TaskStatus.CANCEL_REQUESTED, *TERMINAL_TASK_STATUSES},
    TaskStatus.RUNNING: {TaskStatus.CANCEL_REQUESTED, TaskStatus.RETRY_WAIT, *TERMINAL_TASK_STATUSES},
    TaskStatus.CANCEL_REQUESTED: {*TERMINAL_TASK_STATUSES},
    TaskStatus.RETRY_WAIT: {TaskStatus.DISPATCHING, TaskStatus.CANCELLED, TaskStatus.FAILED},
}


def transition_task(
    task: Task, target: TaskStatus, *, capabilities: BackendCapabilities | None = None,
    reconciliation: Reconciliation | None = None,
) -> Task:
    """Validate a proposed transition; this does not commit it or serialize races."""
    if not isinstance(target, TaskStatus):
        raise ValueError("A TaskStatus is required")
    if target == task.status:
        return task
    if target not in _NEXT.get(task.status, set()):
        raise ValueError("Invalid Task transition; terminal Tasks cannot be revived")
    if task.status == TaskStatus.DISPATCH_UNKNOWN:
        can_redispatch = capabilities is not None and (
            capabilities.idempotent_submit or (
                capabilities.authoritative_absence and reconciliation is not None and reconciliation.outcome == "absent"
            )
        )
        found = capabilities is not None and reconciliation is not None and reconciliation.outcome == "found" and (
            reconciliation.execution.backend == capabilities.backend
        )
        if not (can_redispatch if target == TaskStatus.DISPATCHING else found):
            raise ValueError("Unknown dispatch requires reconciliation or safe idempotent submission")
    return replace(task, status=target)


def fence_allows_write(
    task: Task, claim: Attempt, current: Attempt, *, lease_expires_at: datetime, server_now: datetime,
) -> bool:
    """Pure predicate; current active lease/time must be read inside the DB write.

    It does not fence database writes, LangGraph checkpoints or external tools.
    A future SQL mutation must atomically check epoch, worker and unexpired lease.
    """
    if any(not isinstance(value, datetime) or value.utcoffset() is None for value in (lease_expires_at, server_now)):
        raise ValueError("Lease expiry and server time must be timezone-aware")
    identity = lambda a: (a.task_id, a.attempt_id, a.logical_operation_id, a.ordinal, a.epoch, a.worker_id)
    return (
        task.status not in TERMINAL_TASK_STATUSES and identity(claim) == identity(current)
        and claim.task_id == task.task_id and claim.logical_operation_id == task.logical_operation_id
        and server_now < lease_expires_at
    )


@dataclass(frozen=True)
class EventCursor:
    """A future durable event ordinal within one Task, not a token stream offset."""
    task_id: str
    ordinal: int

    def __post_init__(self) -> None:
        _text(self.task_id)
        _positive(self.ordinal)


@dataclass(frozen=True)
class StreamSequence:
    """An ephemeral stream sequence, deliberately distinct from EventCursor."""
    sequence: int

    def __post_init__(self) -> None:
        _positive(self.sequence)


class ExecutionAdapter(Protocol):
    """Proposal seam; no concrete adapter or durable control plane is installed."""
    capabilities: BackendCapabilities

    async def submit(self, task: Task, attempt: Attempt, payload: Mapping[str, Any]) -> ExecutionRef: ...
    async def reconcile(self, task: Task, attempt: Attempt) -> Reconciliation: ...
    async def get(self, execution: ExecutionRef) -> Literal["queued", "running", "succeeded", "failed", "cancelled", "timed_out", "unknown"]: ...
    async def cancel(self, execution: ExecutionRef) -> None: ...
    async def resume(self, task: Task, attempt: Attempt, previous: ExecutionRef) -> ExecutionRef: ...
