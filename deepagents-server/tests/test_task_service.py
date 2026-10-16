"""Stateless service contracts against a fake Temporal RPC boundary, not a queue."""

import asyncio
from copy import deepcopy
from datetime import datetime, timezone
from types import SimpleNamespace

import pytest
from temporalio.api.enums.v1 import EventType
from temporalio.api.history.v1 import History, HistoryEvent
from temporalio.api.workflowservice.v1 import GetWorkflowExecutionHistoryReverseResponse
from temporalio.client import WorkflowExecutionStatus
from temporalio.common import WorkflowIDConflictPolicy, WorkflowIDReusePolicy
from temporalio.exceptions import WorkflowAlreadyStartedError
from temporalio.service import RPCError, RPCStatusCode

from mini_loop_deepagents.task_server.service import (
    TASK_VERSION, TaskConflict, TaskInvalidRequest, TaskNotFound, TaskService,
    TaskSubmissionUnknown, TaskUnavailable, task_id_for,
)


RUN_ID = "10000000-0000-4000-8000-000000000001"
SECRET = b"cursor-secret-must-be-fixed-across-processes"


class FakeDescription:
    def __init__(self, task_id, memo, status=WorkflowExecutionStatus.RUNNING):
        self.id, self.run_id, self.workflow_type = task_id, RUN_ID, "TaskWorkflow"
        self.status, self._memo = status, memo
        self.raw_info = SimpleNamespace(first_run_id=RUN_ID)

    async def memo(self):
        return deepcopy(self._memo)


class FakeTemporal:
    """Only models server responses; no persistence guarantee is inferred here."""
    namespace = "default"

    def __init__(self):
        self.descriptions, self.start_calls, self.calls = {}, [], []
        self.start_error = self.cancel_error = None
        self.finished_result = {"files": {"/plan.md": {"content": "preserved"}}}
        self.history, self.page_size = [], 3
        self.workflow_service = self

    async def start_workflow(self, workflow_type, payload, **options):
        self.start_calls.append((workflow_type, payload, options))
        if self.start_error:
            raise self.start_error
        task_id = options["id"]
        if task_id in self.descriptions:
            raise WorkflowAlreadyStartedError(task_id, workflow_type, run_id=RUN_ID)
        self.descriptions[task_id] = FakeDescription(task_id, options["memo"])
        return SimpleNamespace(result_run_id=RUN_ID, first_execution_run_id=RUN_ID)

    def get_workflow_handle(self, task_id, **options):
        self.calls.append(("handle", task_id, options))
        parent = self

        class Handle:
            async def describe(self, **kwargs):
                parent.calls.append(("describe", task_id, kwargs))
                if task_id not in parent.descriptions:
                    raise RPCError("internal sensitive details", RPCStatusCode.NOT_FOUND, b"")
                return parent.descriptions[task_id]

            async def result(self, **kwargs):
                parent.calls.append(("result", task_id, kwargs))
                return parent.finished_result

            async def cancel(self, **kwargs):
                parent.calls.append(("cancel", task_id, kwargs))
                if parent.cancel_error:
                    parent.descriptions[task_id].status = WorkflowExecutionStatus.COMPLETED
                    raise parent.cancel_error

        return Handle()

    async def get_workflow_execution_history_reverse(self, request, **options):
        self.calls.append(("history", request, options))
        offset = int(request.next_page_token.decode() or 0)
        ordered = list(reversed(self.history))
        end = offset + self.page_size
        return GetWorkflowExecutionHistoryReverseResponse(
            history=History(events=ordered[offset:end]),
            next_page_token=str(end).encode() if end < len(ordered) else b"",
        )


def service(client, **options):
    return TaskService(client, task_queue="task-v1", cursor_secret=SECRET, **options)


def event(ordinal):
    result = HistoryEvent(event_id=ordinal, event_type=EventType.EVENT_TYPE_WORKFLOW_EXECUTION_STARTED)
    result.event_time.FromDatetime(datetime(2026, 10, 16, tzinfo=timezone.utc))
    return result


def test_task_ids_are_stable_owner_scoped_and_unambiguous():
    assert task_id_for("alice", "key") == task_id_for("alice", "key")
    assert task_id_for("alice", "key") != task_id_for("bob", "key")
    assert task_id_for("a|b", "c") != task_id_for("a", "b|c")


async def test_start_acknowledges_a_single_stable_versioned_workflow_with_bounded_execution():
    client = FakeTemporal()
    result = await service(client).submit("alice", "one", "work")
    workflow_type, payload, options = client.start_calls[0]
    assert workflow_type == "TaskWorkflow" and payload["task_id"] == options["id"] == result["task_id"]
    assert payload == {"task_id": result["task_id"], "owner": "alice", "message": "work", "version": TASK_VERSION}
    assert options["id_reuse_policy"] == WorkflowIDReusePolicy.REJECT_DUPLICATE
    assert options["id_conflict_policy"] == WorkflowIDConflictPolicy.FAIL
    assert options["rpc_timeout"].total_seconds() == 5
    assert options["execution_timeout"].total_seconds() == 86400
    assert options["memo"]["owner"] == "alice" and options["memo"]["task_id"] == result["task_id"]
    assert len(options["memo"]["request_sha256"]) == 64
    assert result["write_confirmed"] and not result["reused"] and result["status"] == "running"


async def test_concurrent_identical_submissions_reuse_native_execution_and_conflicts_do_not_reuse():
    client = FakeTemporal()
    connection = service(client)
    first, second = await asyncio.gather(connection.submit("alice", "key", "same"), connection.submit("alice", "key", "same"))
    assert first["task_id"] == second["task_id"] and len(client.descriptions) == 1
    assert {first["reused"], second["reused"]} == {False, True}
    with pytest.raises(TaskConflict):
        await connection.submit("alice", "key", "different")
    changed_queue = TaskService(client, task_queue="new-profile", cursor_secret=SECRET)
    with pytest.raises(TaskConflict):
        await changed_queue.submit("alice", "key", "same")
    assert len(client.descriptions) == 1


@pytest.mark.parametrize("error", [RPCError("credential-secret", RPCStatusCode.UNAVAILABLE, b""), TimeoutError("credential-secret")])
async def test_uncertain_start_exposes_stable_id_and_never_resubmits_in_wrapper(error):
    client = FakeTemporal()
    client.start_error = error
    with pytest.raises(TaskSubmissionUnknown) as caught:
        await service(client).submit("alice", "key", "same")
    assert len(client.start_calls) == 1
    assert caught.value.status_code == 503 and caught.value.details["task_id"] == task_id_for("alice", "key")
    assert "credential-secret" not in str(caught.value) and "credential-secret" not in str(caught.value.details)


async def test_start_deadline_is_bounded_and_reports_unknown_instead_of_cancelled_task():
    client = FakeTemporal()

    async def slow(*args, **kwargs):
        await asyncio.sleep(10)

    client.start_workflow = slow
    with pytest.raises(TaskSubmissionUnknown):
        await service(client, rpc_timeout_seconds=0.01).submit("alice", "key", "same")


@pytest.mark.parametrize("mutation", [
    lambda d: d._memo.update(owner="bob"),
    lambda d: d._memo.update(version="foreign-v2"),
    lambda d: d._memo.update(task_id=task_id_for("bob", "other")),
    lambda d: d._memo.update(request_sha256="not-a-digest"),
    lambda d: setattr(d, "workflow_type", "UntrustedWorkflow"),
])
@pytest.mark.parametrize("operation", ["get", "result", "events", "cancel"])
async def test_every_operation_checks_trusted_memo_and_type_before_access(mutation, operation):
    client = FakeTemporal()
    connection = service(client)
    task = await connection.submit("alice", "key", "same")
    mutation(client.descriptions[task["task_id"]])
    with pytest.raises(TaskNotFound):
        await getattr(connection, operation)("alice", task["task_id"])
    assert not any(call[0] in {"history", "result", "cancel"} for call in client.calls)


async def test_foreign_owner_is_not_found_including_duplicate_start():
    client = FakeTemporal()
    connection = service(client)
    task = await connection.submit("alice", "key", "same")
    for operation in (connection.get, connection.result, connection.events, connection.cancel):
        with pytest.raises(TaskNotFound):
            await operation("bob", task["task_id"])
    client.descriptions[task["task_id"]]._memo["owner"] = "bob"
    with pytest.raises(TaskNotFound):
        await connection.submit("alice", "key", "different")


async def test_status_and_running_result_need_no_worker_query_or_long_poll():
    client = FakeTemporal()
    connection = service(client)
    task = await connection.submit("alice", "key", "same")
    assert (await connection.get("alice", task["task_id"]))["status"] == "running"
    result = await connection.result("alice", task["task_id"])
    assert result["ready"] is False
    assert not any(call[0] == "result" for call in client.calls)
    client.descriptions[task["task_id"]].status = WorkflowExecutionStatus.COMPLETED
    result = await connection.result("alice", task["task_id"])
    assert result["ready"] and result["status"] == "succeeded" and result["result"] == client.finished_result
    assert [call for call in client.calls if call[0] == "result"][-1][2]["follow_runs"] is False


@pytest.mark.parametrize("status, expected", [
    (WorkflowExecutionStatus.FAILED, "failed"), (WorkflowExecutionStatus.CANCELED, "cancelled"),
    (WorkflowExecutionStatus.TIMED_OUT, "timed_out"), (WorkflowExecutionStatus.TERMINATED, "terminated"),
])
async def test_native_failure_terminal_states_are_reported_without_exception_bodies(status, expected):
    client = FakeTemporal()
    connection = service(client)
    task = await connection.submit("alice", "key", "same")
    client.descriptions[task["task_id"]].status = status
    result = await connection.result("alice", task["task_id"])
    assert result["ready"] and result["status"] == expected and result["result"] is None
    assert result["error"] == {"code": "workflow_" + expected}


async def test_cancel_acknowledgement_is_not_a_claim_of_terminal_stop():
    client = FakeTemporal()
    connection = service(client)
    task = await connection.submit("alice", "key", "same")
    result = await connection.cancel("alice", task["task_id"])
    assert result["cancel_requested"] and result["status"] == "running"
    handle_options = [call[2] for call in client.calls if call[0] == "handle"][-1]
    assert handle_options == {"first_execution_run_id": RUN_ID}
    client.descriptions[task["task_id"]].status = WorkflowExecutionStatus.COMPLETED
    assert not (await connection.cancel("alice", task["task_id"]))["cancel_requested"]
    assert len([call for call in client.calls if call[0] == "cancel"]) == 1


async def test_completed_before_cancel_rpc_is_reconciled_without_false_cancellation():
    client = FakeTemporal()
    connection = service(client)
    task = await connection.submit("alice", "key", "same")
    client.cancel_error = RPCError("internal sensitive details", RPCStatusCode.NOT_FOUND, b"")
    result = await connection.cancel("alice", task["task_id"])
    assert result["status"] == "succeeded" and result["cancel_requested"] is False


async def test_history_pagination_has_no_gaps_when_appended_and_survives_new_service_instance():
    client = FakeTemporal()
    connection = service(client)
    task = await connection.submit("alice", "key", "same")
    client.history = [event(i) for i in range(1, 10)]
    first = await connection.events("alice", task["task_id"], limit=2)
    assert [row["event_id"] for row in first["events"]] == [9, 8]
    assert first["order"] == "descending" and first["events"][0]["type"] == "workflow_execution_started"
    client.history += [event(i) for i in range(10, 19)]
    restarted, cursor, seen = service(client), first["next_cursor"], [9, 8]
    while cursor is not None:
        page = await restarted.events("alice", task["task_id"], cursor, limit=2)
        assert len(page["events"]) <= 2
        seen += [row["event_id"] for row in page["events"]]
        cursor = page["next_cursor"]
    assert seen == list(range(9, 0, -1))
    assert [row["event_id"] for row in (await restarted.events("alice", task["task_id"], limit=2))["events"]] == [18, 17]
    assert all(call[2]["retry"] and call[2]["timeout"].total_seconds() == 5 for call in client.calls if call[0] == "history")


async def test_history_cursor_rejects_tampering_cross_task_namespace_and_untrusted_native_token():
    client = FakeTemporal()
    connection = service(client)
    task = await connection.submit("alice", "key", "same")
    other = await connection.submit("alice", "other", "same")
    client.history = [event(i) for i in range(1, 5)]
    cursor = (await connection.events("alice", task["task_id"], limit=1))["next_cursor"]
    count = len([call for call in client.calls if call[0] == "history"])
    for invalid in ("raw-native-page-token", cursor[:-4] + "AAAA", cursor + ".other"):
        with pytest.raises(TaskInvalidRequest):
            await connection.events("alice", task["task_id"], invalid)
    with pytest.raises(TaskInvalidRequest):
        await connection.events("alice", other["task_id"], cursor)
    changed_secret = TaskService(client, task_queue="task-v1", cursor_secret=b"a" * 32)
    with pytest.raises(TaskInvalidRequest):
        await changed_secret.events("alice", task["task_id"], cursor)
    client.namespace = "different-namespace"
    with pytest.raises(TaskInvalidRequest):
        await connection.events("alice", task["task_id"], cursor)
    assert len([call for call in client.calls if call[0] == "history"]) == count


@pytest.mark.parametrize("key,message", [
    ("", "work"), (" trailing ", "work"), ("x" * 201, "work"),
    ("key", ""), ("key", "x" * 65537), ("key", "\ud800"), ("\ud800", "work"),
])
async def test_invalid_input_never_issues_a_start(key, message):
    client = FakeTemporal()
    with pytest.raises(TaskInvalidRequest):
        await service(client).submit("alice", key, message)
    assert client.start_calls == []


async def test_backend_not_found_and_other_rpc_failures_remain_distinct_and_redacted():
    client = FakeTemporal()
    with pytest.raises(TaskNotFound):
        await service(client).get("alice", task_id_for("alice", "missing"))
    task = await service(client).submit("alice", "key", "same")

    async def unavailable(*args, **kwargs):
        raise RPCError("credential-secret", RPCStatusCode.UNAVAILABLE, b"")

    client.descriptions[task["task_id"]].memo = unavailable
    with pytest.raises(TaskNotFound) as caught:
        await service(client).get("alice", task["task_id"])
    assert "credential-secret" not in str(caught.value)


def test_constructor_requires_fixed_strong_cursor_key_and_positive_deadlines():
    for options in ({"cursor_secret": "short"}, {"rpc_timeout_seconds": 0}, {"workflow_execution_timeout_seconds": float("nan")}):
        arguments = {"task_queue": "task-v1", "cursor_secret": SECRET} | options
        with pytest.raises(ValueError):
            TaskService(FakeTemporal(), **arguments)
