"""Pure contracts cover admission, uncertain dispatch and stale worker rejection."""

from dataclasses import FrozenInstanceError, replace
from datetime import datetime, timedelta, timezone

import pytest

from mini_loop_deepagents.task_contracts import (
    AdmissionKey, Attempt, BackendCapabilities, EventCursor, ExecutionRef,
    LANGGRAPH_AGENT_SERVER_PROFILE, Reconciliation, StreamSequence, Task, TaskStatus,
    TERMINAL_TASK_STATUSES, admission_decision, fence_allows_write, transition_task,
)


DIGEST = "a" * 64
NOW = datetime(2026, 10, 16, tzinfo=timezone.utc)


def task(status=TaskStatus.ACCEPTED):
    return Task("task-1", "owner-1", "operation-1", DIGEST, status)


def attempt(**updates):
    fields = dict(attempt_id="attempt-1", task_id="task-1", logical_operation_id="operation-1", ordinal=1, epoch=1, worker_id="worker-1")
    return Attempt(**(fields | updates))


def test_retry_identity_is_distinct_from_task_and_logical_operation():
    original = attempt()
    retry = replace(original, attempt_id="attempt-2", ordinal=2, epoch=2, worker_id="worker-2")
    assert retry.task_id == original.task_id == task().task_id
    assert retry.logical_operation_id == original.logical_operation_id == task().logical_operation_id
    assert retry.attempt_id != original.attempt_id
    with pytest.raises(ValueError):
        replace(original, attempt_id=original.logical_operation_id)
    with pytest.raises(ValueError):
        replace(task(), task_id=task().logical_operation_id)


@pytest.mark.parametrize("record,field,value", [
    (task(), "owner", "other"),
    (attempt(), "epoch", 2),
    (ExecutionRef("runtime", "run-1", "thread-1"), "execution_id", "run-2"),
    (AdmissionKey("owner-1", "request-1", DIGEST), "payload_sha256", "b" * 64),
    (LANGGRAPH_AGENT_SERVER_PROFILE, "idempotent_submit", True),
    (EventCursor("task-1", 1), "ordinal", 2),
])
def test_contract_records_are_immutable(record, field, value):
    with pytest.raises(FrozenInstanceError):
        setattr(record, field, value)


def test_admission_is_scoped_and_payload_conflicts_are_explicit():
    incoming = AdmissionKey("owner-1", "request-1", DIGEST)
    assert admission_decision(None, incoming) == "create"
    assert admission_decision(incoming, incoming) == "reuse"
    assert admission_decision(incoming, replace(incoming, payload_sha256="b" * 64)) == "conflict"
    assert admission_decision(incoming, replace(incoming, owner="owner-2")) == "create"
    assert admission_decision(incoming, replace(incoming, idempotency_key="request-2")) == "create"


def test_known_failure_can_retry_before_task_is_terminal():
    current = task()
    for status in (
        TaskStatus.DISPATCHING, TaskStatus.QUEUED, TaskStatus.RUNNING,
        TaskStatus.RETRY_WAIT, TaskStatus.DISPATCHING, TaskStatus.RUNNING, TaskStatus.SUCCEEDED,
    ):
        current = transition_task(current, status)
    assert current.task_id == task().task_id and current.status == TaskStatus.SUCCEEDED


@pytest.mark.parametrize("terminal", sorted(TERMINAL_TASK_STATUSES))
@pytest.mark.parametrize("target", list(TaskStatus))
def test_terminal_tasks_cannot_be_revived(terminal, target):
    current = task(terminal)
    if terminal == target:
        assert transition_task(current, target) is current
    else:
        with pytest.raises(ValueError, match="terminal"):
            transition_task(current, target)


@pytest.mark.parametrize("target", [TaskStatus.DISPATCHING, TaskStatus.RETRY_WAIT, TaskStatus.ACCEPTED, TaskStatus.FAILED, TaskStatus.CANCELLED])
def test_uncertain_submission_cannot_be_blindly_retried_or_declared_done(target):
    with pytest.raises(ValueError):
        transition_task(task(TaskStatus.DISPATCH_UNKNOWN), target, capabilities=LANGGRAPH_AGENT_SERVER_PROFILE)


def test_best_effort_absence_does_not_authorize_nonidempotent_redispatch():
    with pytest.raises(ValueError, match="Unknown dispatch"):
        transition_task(
            task(TaskStatus.DISPATCH_UNKNOWN), TaskStatus.DISPATCHING,
            capabilities=LANGGRAPH_AGENT_SERVER_PROFILE, reconciliation=Reconciliation("absent"),
        )


def test_authoritative_absence_or_idempotent_backend_can_authorize_redispatch():
    current = task(TaskStatus.DISPATCH_UNKNOWN)
    authoritative = replace(LANGGRAPH_AGENT_SERVER_PROFILE, authoritative_absence=True)
    idempotent = replace(LANGGRAPH_AGENT_SERVER_PROFILE, idempotent_submit=True)
    assert transition_task(current, TaskStatus.DISPATCHING, capabilities=idempotent).status == TaskStatus.DISPATCHING
    assert transition_task(
        current, TaskStatus.DISPATCHING, capabilities=authoritative, reconciliation=Reconciliation("absent"),
    ).status == TaskStatus.DISPATCHING
    with pytest.raises(ValueError):
        transition_task(current, TaskStatus.DISPATCHING, capabilities=authoritative, reconciliation=Reconciliation("unknown"))


def test_reconciliation_can_bind_known_execution_without_resubmission():
    reference = ExecutionRef(LANGGRAPH_AGENT_SERVER_PROFILE.backend, "run-1", "thread-1")
    current = transition_task(
        task(TaskStatus.DISPATCH_UNKNOWN), TaskStatus.RUNNING,
        capabilities=LANGGRAPH_AGENT_SERVER_PROFILE, reconciliation=Reconciliation("found", reference),
    )
    assert current.status == TaskStatus.RUNNING
    with pytest.raises(ValueError):
        transition_task(
            task(TaskStatus.DISPATCH_UNKNOWN), TaskStatus.RUNNING,
            capabilities=LANGGRAPH_AGENT_SERVER_PROFILE,
            reconciliation=Reconciliation("found", replace(reference, backend="other")),
        )


@pytest.mark.parametrize("outcome,reference", [
    ("found", None), ("absent", ExecutionRef("runtime", "run-1")),
    ("unknown", ExecutionRef("runtime", "run-1")), ("invalid", None),
])
def test_reconciliation_cannot_confuse_absence_unknown_and_found(outcome, reference):
    with pytest.raises(ValueError):
        Reconciliation(outcome, reference)


def test_fence_requires_current_epoch_worker_attempt_and_unexpired_server_lease():
    running = task(TaskStatus.RUNNING)
    claim = attempt()
    expiry = NOW + timedelta(seconds=30)
    assert fence_allows_write(running, claim, claim, lease_expires_at=expiry, server_now=NOW)
    # Persisting the backend reference does not itself invalidate the worker claim.
    current = replace(claim, execution=ExecutionRef("runtime", "run-1"))
    assert fence_allows_write(running, claim, current, lease_expires_at=expiry, server_now=NOW)
    for stale in (
        replace(claim, epoch=2), replace(claim, worker_id="worker-2"),
        replace(claim, attempt_id="attempt-2"), replace(claim, task_id="task-2"),
        replace(claim, logical_operation_id="operation-2"), replace(claim, ordinal=2),
    ):
        assert not fence_allows_write(running, claim, stale, lease_expires_at=expiry, server_now=NOW)
    for time in (expiry, expiry + timedelta(seconds=1)):
        assert not fence_allows_write(running, claim, claim, lease_expires_at=expiry, server_now=time)
    assert not fence_allows_write(task(TaskStatus.SUCCEEDED), claim, claim, lease_expires_at=expiry, server_now=NOW)
    assert not fence_allows_write(replace(running, task_id="other-task"), claim, claim, lease_expires_at=expiry, server_now=NOW)
    with pytest.raises(ValueError, match="timezone-aware"):
        fence_allows_write(running, claim, claim, lease_expires_at=expiry, server_now=NOW.replace(tzinfo=None))


def test_durable_event_cursor_and_ephemeral_stream_sequence_are_distinct():
    assert EventCursor("task-1", 1) != StreamSequence(1)
    assert EventCursor("task-1", 1) != EventCursor("task-2", 1)
    for invalid in (0, -1, True, 1.5, "1"):
        with pytest.raises(ValueError):
            EventCursor("task-1", invalid)
        with pytest.raises(ValueError):
            StreamSequence(invalid)


def test_current_backend_profile_does_not_claim_unverified_production_guarantees():
    profile = LANGGRAPH_AGENT_SERVER_PROFILE
    assert not profile.idempotent_submit and not profile.authoritative_absence
    assert profile.checkpoint_owner == "runtime"
    assert profile.checkpoint_durability == "architecture_available_unverified"
    assert profile.supports_cancel and profile.supports_resume


@pytest.mark.parametrize("construct", [
    lambda: AdmissionKey("owner-1", "request-1", "not-a-hash"),
    lambda: AdmissionKey("", "request-1", DIGEST),
    lambda: replace(task(), status="running"),
    lambda: replace(attempt(), epoch=True),
    lambda: replace(attempt(), execution={"run_id": "mutable"}),
    lambda: replace(LANGGRAPH_AGENT_SERVER_PROFILE, checkpoint_owner="invented"),
    lambda: replace(LANGGRAPH_AGENT_SERVER_PROFILE, idempotent_submit="true"),
])
def test_invalid_record_values_are_rejected(construct):
    with pytest.raises(ValueError):
        construct()
