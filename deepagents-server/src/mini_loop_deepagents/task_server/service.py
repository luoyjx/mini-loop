"""Stateless Task operations backed by retained Temporal Workflow History.

The service does not own a queue, admission ledger or process-local task state.
Temporal's ID reuse protection lasts only while closed executions are retained.
The injected owner has already been authenticated by the HTTP boundary; raw
Temporal access remains trusted and private. SDK retries of one RPC are retained.
"""

from __future__ import annotations

import asyncio
import base64
from datetime import timedelta
import hashlib
import hmac
import json
import math
import re
from typing import Any
from uuid import UUID

from temporalio.api.common.v1 import WorkflowExecution
from temporalio.api.enums.v1 import EventType
from temporalio.api.workflowservice.v1 import GetWorkflowExecutionHistoryRequest, GetWorkflowExecutionHistoryReverseRequest
from temporalio.client import Client, WorkflowContinuedAsNewError
from temporalio.common import WorkflowIDConflictPolicy, WorkflowIDReusePolicy
from temporalio.exceptions import WorkflowAlreadyStartedError
from temporalio.service import RPCError, RPCStatusCode


TASK_VERSION = "task-v2"
WORKFLOW_TYPE = "TaskWorkflowV2"
TRUSTED_WORKFLOWS = {"TaskWorkflow": "task-v1", "TaskWorkflowV2": "task-v2"}
_TASK_ID = re.compile(r"task-[a-f0-9]{64}\Z")
_SHA256 = re.compile(r"[a-f0-9]{64}\Z")
_STATUSES = {
    "RUNNING": "running", "COMPLETED": "succeeded", "FAILED": "failed",
    "CANCELED": "cancelled", "TIMED_OUT": "timed_out", "TERMINATED": "terminated",
    "CONTINUED_AS_NEW": "continued_as_new",
}
_TERMINAL = frozenset({"succeeded", "failed", "cancelled", "timed_out", "terminated"})


class TaskServiceError(Exception):
    status_code = 503
    code = "task_service_unavailable"

    def __init__(self, details: dict[str, Any] | None = None):
        self.details = dict(details or {})
        super().__init__(self.code)


class TaskNotFound(TaskServiceError):
    status_code, code = 404, "task_not_found"


class _RunMissing(TaskNotFound):
    """Native NOT_FOUND, distinct internally from rejected authority metadata."""


class TaskConflict(TaskServiceError):
    status_code, code = 409, "idempotency_conflict"

    def __init__(self, task_id: str):
        super().__init__({"task_id": task_id})


class TaskInvalidRequest(TaskServiceError):
    status_code, code = 400, "invalid_request"

    def __init__(self, field: str):
        super().__init__({"field": field})


class TaskSubmissionUnknown(TaskServiceError):
    code = "submission_unknown"

    def __init__(self, task_id: str):
        self.task_id = task_id
        super().__init__({"task_id": task_id, "outcome": "unknown"})


class TaskUnavailable(TaskServiceError):
    pass


def _canonical(value: Any) -> bytes:
    return json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode()


def task_id_for(owner: str, key: str) -> str:
    """Domain separation plus a JSON tuple avoids ambiguous owner/key joins."""
    return "task-" + hashlib.sha256(b"mini-loop:task-id:v1\0" + _canonical([owner, key])).hexdigest()


def _request_digest(task_id: str, owner: str, message: str, version: str, task_queue: str) -> str:
    # Keep the original v1 digest shape. Continuation state is not a submission.
    payload = {"task_id": task_id, "owner": owner, "message": message, "version": version}
    return hashlib.sha256(_canonical({"payload": payload, "task_queue": task_queue})).hexdigest()


def _valid_run_id(value: Any) -> bool:
    try:
        return isinstance(value, str) and str(UUID(value)) == value
    except (ValueError, TypeError, AttributeError):
        return False


def _encoded(value: bytes) -> str:
    return base64.urlsafe_b64encode(value).decode("ascii").rstrip("=")


def _decoded(value: str) -> bytes:
    return base64.b64decode(value + "=" * (-len(value) % 4), altchars=b"-_", validate=True)


class TaskService:
    def __init__(
        self, client: Client, *, task_queue: str, cursor_secret: bytes | str,
        rpc_timeout_seconds: float = 5.0, workflow_execution_timeout_seconds: float = 86400,
    ):
        if not isinstance(task_queue, str) or not task_queue:
            raise ValueError("A task queue is required")
        secret = cursor_secret.encode() if isinstance(cursor_secret, str) else cursor_secret
        if not isinstance(secret, bytes) or len(secret) < 32:
            raise ValueError("A fixed deployment cursor secret of at least 32 bytes is required")
        if any(not math.isfinite(value) or value <= 0 for value in (rpc_timeout_seconds, workflow_execution_timeout_seconds)):
            raise ValueError("RPC and Workflow timeouts must be positive and finite")
        self.client, self.task_queue, self._secret = client, task_queue, secret
        self._seconds = rpc_timeout_seconds
        self._timeout = timedelta(seconds=rpc_timeout_seconds)
        self._execution_timeout = timedelta(seconds=workflow_execution_timeout_seconds)

    @staticmethod
    def _validate_identity(owner: str, task_id: str) -> None:
        if not isinstance(owner, str) or not owner or not isinstance(task_id, str) or not _TASK_ID.fullmatch(task_id):
            raise TaskNotFound()

    async def _rpc(self, call: Any) -> Any:
        try:
            async with asyncio.timeout(self._seconds):
                return await call
        except WorkflowContinuedAsNewError:
            raise
        except RPCError as error:
            if error.status == RPCStatusCode.NOT_FOUND:
                raise TaskNotFound() from None
            raise TaskUnavailable() from None
        except Exception:
            raise TaskUnavailable() from None

    async def _owned(self, owner: str, task_id: str, *, run_id: str | None = None) -> tuple[Any, dict[str, Any]]:
        self._validate_identity(owner, task_id)
        handle = self.client.get_workflow_handle(task_id, run_id=run_id)
        try:
            description = await self._rpc(handle.describe(rpc_timeout=self._timeout))
        except TaskNotFound:
            raise _RunMissing() from None
        try:
            async with asyncio.timeout(self._seconds):
                memo = dict(await description.memo())
            valid = (
                description.id == task_id and _valid_run_id(description.run_id)
                and TRUSTED_WORKFLOWS.get(description.workflow_type) == memo.get("version")
                and memo.get("version") in TRUSTED_WORKFLOWS.values()
                and (run_id is None or description.run_id == run_id)
                and memo.get("owner") == owner and memo.get("task_id") == task_id
                and isinstance(memo.get("request_sha256"), str) and _SHA256.fullmatch(memo["request_sha256"])
            )
        except Exception:
            raise TaskNotFound() from None
        if not valid:
            raise TaskNotFound()
        return description, memo

    @staticmethod
    def _first_run_id(description: Any) -> str:
        first = getattr(description.raw_info, "first_run_id", "")
        if not _valid_run_id(first):
            # Guessing the current Run would lose the native chain guard at CAN.
            raise TaskUnavailable()
        return first

    @classmethod
    def _same_chain(cls, original: tuple[Any, dict[str, Any]], current: tuple[Any, dict[str, Any]]) -> None:
        left, left_memo = original
        right, right_memo = current
        if (cls._first_run_id(left) != cls._first_run_id(right)
                or left.workflow_type != right.workflow_type
                or any(left_memo[key] != right_memo[key] for key in ("owner", "task_id", "version", "request_sha256"))):
            raise TaskNotFound()

    async def _latest_owned(self, owner: str, task_id: str) -> tuple[Any, dict[str, Any]]:
        try:
            async with asyncio.timeout(self._seconds):
                original = await self._owned(owner, task_id)
                current = original
                # Continue-as-New is atomic, but a read can still observe an old
                # closed Run. Refresh at most twice and never cross a new chain.
                for _ in range(2):
                    if getattr(current[0].status, "name", None) != "CONTINUED_AS_NEW":
                        break
                    current = await self._owned(owner, task_id)
                    self._same_chain(original, current)
                return current
        except TimeoutError:
            raise TaskUnavailable() from None

    @classmethod
    def _summary(cls, description: Any, memo: dict[str, Any]) -> dict[str, Any]:
        status = _STATUSES.get(getattr(description.status, "name", None))
        if status is None:
            raise TaskUnavailable()
        return {"task_id": description.id, "run_id": description.run_id,
                "status": status, "version": memo["version"],
                "first_execution_run_id": cls._first_run_id(description)}

    async def submit(self, owner: str, key: str, message: str) -> dict[str, Any]:
        if not isinstance(owner, str) or not owner:
            raise TaskInvalidRequest("owner")
        if (not isinstance(key, str) or not key or len(key) > 200 or key.strip() != key
                or any(ord(c) < 32 or ord(c) == 127 for c in key)):
            raise TaskInvalidRequest("idempotency_key")
        if not isinstance(message, str) or not message.strip():
            raise TaskInvalidRequest("message")
        try:
            key.encode("utf-8")
            message_bytes = message.encode("utf-8")
        except UnicodeError:
            raise TaskInvalidRequest("text_encoding") from None
        if len(message_bytes) > 65536:
            raise TaskInvalidRequest("message")
        task_id = task_id_for(owner, key)
        payload = {"task_id": task_id, "owner": owner, "message": message, "version": TASK_VERSION}
        digest = _request_digest(task_id, owner, message, TASK_VERSION, self.task_queue)
        memo = {"owner": owner, "request_sha256": digest, "version": TASK_VERSION, "task_id": task_id}
        try:
            async with asyncio.timeout(self._seconds):
                handle = await self.client.start_workflow(
                    WORKFLOW_TYPE, payload, id=task_id, task_queue=self.task_queue, memo=memo,
                    id_reuse_policy=WorkflowIDReusePolicy.REJECT_DUPLICATE,
                    id_conflict_policy=WorkflowIDConflictPolicy.FAIL,
                    execution_timeout=self._execution_timeout, rpc_timeout=self._timeout,
                )
        except WorkflowAlreadyStartedError:
            description, existing = await self._latest_owned(owner, task_id)
            expected = _request_digest(task_id, owner, message, existing["version"], self.task_queue)
            if existing["request_sha256"] != expected:
                raise TaskConflict(task_id)
            return {**self._summary(description, existing), "reused": True, "write_confirmed": True}
        except (Exception, asyncio.CancelledError):
            # The wrapper does not create another business submission. The SDK
            # may retry this same Start RPC/request ID; retain the stable Task ID.
            raise TaskSubmissionUnknown(task_id) from None
        run_id = handle.result_run_id or handle.first_execution_run_id
        if not isinstance(run_id, str) or not run_id:
            raise TaskSubmissionUnknown(task_id)
        return {"task_id": task_id, "run_id": run_id, "status": "running", "version": TASK_VERSION,
                "first_execution_run_id": handle.first_execution_run_id or run_id,
                "reused": False, "write_confirmed": True}

    async def get(self, owner: str, task_id: str) -> dict[str, Any]:
        description, memo = await self._latest_owned(owner, task_id)
        return self._summary(description, memo)

    async def result(self, owner: str, task_id: str) -> dict[str, Any]:
        try:
            async with asyncio.timeout(self._seconds):
                original = await self._latest_owned(owner, task_id)
                current = original
                for _ in range(2):
                    description, memo = current
                    self._same_chain(original, current)
                    summary = self._summary(description, memo)
                    if summary["status"] not in _TERMINAL:
                        return {**summary, "ready": False}
                    if summary["status"] != "succeeded":
                        return {**summary, "ready": True, "result": None, "error": {"code": "workflow_" + summary["status"]}}
                    handle = self.client.get_workflow_handle(task_id, run_id=description.run_id)
                    try:
                        value = await self._rpc(handle.result(follow_runs=False, rpc_timeout=self._timeout))
                    except WorkflowContinuedAsNewError:
                        current = await self._latest_owned(owner, task_id)
                        continue
                    return {**summary, "ready": True, "result": json.loads(_canonical(value))}
        except (ValueError, TypeError, TimeoutError):
            raise TaskUnavailable() from None
        raise TaskUnavailable()

    async def cancel(self, owner: str, task_id: str) -> dict[str, Any]:
        original = await self._latest_owned(owner, task_id)
        description, memo = original
        summary = self._summary(description, memo)
        if summary["status"] in _TERMINAL:
            return {**summary, "cancel_requested": False}
        first_run_id = self._first_run_id(description)
        handle = self.client.get_workflow_handle(task_id, first_execution_run_id=first_run_id)
        try:
            async with asyncio.timeout(self._seconds):
                await handle.cancel(rpc_timeout=self._timeout)
        except RPCError as error:
            if error.status in {RPCStatusCode.NOT_FOUND, RPCStatusCode.FAILED_PRECONDITION}:
                refreshed = await self._latest_owned(owner, task_id)
                self._same_chain(original, refreshed)
                current = self._summary(*refreshed)
                if current["status"] in _TERMINAL:
                    return {**current, "cancel_requested": False}
            raise TaskUnavailable() from None
        except Exception:
            raise TaskUnavailable() from None
        return {**summary, "cancel_requested": True}

    def _cursor(self, owner: str, task_id: str, run_id: str, before: int) -> str:
        value = {"v": 1, "namespace": self.client.namespace, "owner": owner,
                 "task_id": task_id, "run_id": run_id, "before_event_id": before}
        body = _encoded(_canonical(value))
        signature = _encoded(hmac.digest(self._secret, body.encode("ascii"), "sha256"))
        return body + "." + signature

    def _parse_cursor(self, cursor: str, owner: str, task_id: str) -> dict[str, Any]:
        try:
            if not isinstance(cursor, str) or len(cursor) > 4096:
                raise ValueError
            body, signature = cursor.split(".")
            if not hmac.compare_digest(_decoded(signature), hmac.digest(self._secret, body.encode("ascii"), "sha256")):
                raise ValueError
            value = json.loads(_decoded(body))
            if (set(value) != {"v", "namespace", "owner", "task_id", "run_id", "before_event_id"}
                    or type(value["v"]) is not int or value["v"] != 1 or value["owner"] != owner
                    or value["namespace"] != self.client.namespace or value["task_id"] != task_id
                    or type(value["before_event_id"]) is not int or value["before_event_id"] <= 0
                    or str(UUID(value["run_id"])) != value["run_id"]):
                raise ValueError
            return value
        except (ValueError, TypeError, KeyError, UnicodeError, AttributeError):
            raise TaskInvalidRequest("cursor") from None

    async def _navigation(self, owner: str, task_id: str, current: tuple[Any, dict[str, Any]]) -> dict[str, Any]:
        description, _ = current
        execution = WorkflowExecution(workflow_id=task_id, run_id=description.run_id)
        first = await self.client.workflow_service.get_workflow_execution_history(
            GetWorkflowExecutionHistoryRequest(namespace=self.client.namespace, execution=execution,
                                                maximum_page_size=1), retry=True, timeout=self._timeout)
        if (not first.history.events or first.history.events[0].event_id != 1
                or not first.history.events[0].HasField("workflow_execution_started_event_attributes")):
            raise TaskUnavailable()
        started = first.history.events[0].workflow_execution_started_event_attributes
        if (started.first_execution_run_id != self._first_run_id(description)
                or started.workflow_type.name != description.workflow_type):
            raise TaskNotFound()
        last = await self.client.workflow_service.get_workflow_execution_history_reverse(
            GetWorkflowExecutionHistoryReverseRequest(namespace=self.client.namespace, execution=execution,
                                                       maximum_page_size=1), retry=True, timeout=self._timeout)
        next_run_id = None
        if last.history.events and last.history.events[0].HasField("workflow_execution_continued_as_new_event_attributes"):
            next_run_id = last.history.events[0].workflow_execution_continued_as_new_event_attributes.new_execution_run_id
        if getattr(description.status, "name", None) == "CONTINUED_AS_NEW" and not next_run_id:
            raise TaskUnavailable()
        navigation = {"first_execution_run_id": self._first_run_id(description),
                      "previous_run_id": None, "next_run_id": None, "history_navigation_incomplete": False}
        for key, run_id in (("previous_run_id", started.continued_execution_run_id), ("next_run_id", next_run_id)):
            if not run_id:
                continue
            if not _valid_run_id(run_id) or run_id == description.run_id:
                raise TaskNotFound()
            try:
                adjacent = await self._owned(owner, task_id, run_id=run_id)
                self._same_chain(current, adjacent)
            except _RunMissing:
                # An expired adjacent Run grants no navigation capability, but
                # this independently owned Run's retained History remains useful.
                navigation["history_navigation_incomplete"] = True
                continue
            navigation[key] = run_id
        return navigation

    async def events(self, owner: str, task_id: str, cursor: str | None = None, limit: int = 50,
                     *, run_id: str | None = None) -> dict[str, Any]:
        """Read native retained History, newest first; no token-stream guarantee.

        The signed exclusive event-ID boundary survives appended History and API
        restarts. Each page and event ID belongs to one Run; navigation never
        merges them into a task-global sequence. Old pages scan from the head
        under one deadline and a 64-page cap; there is no event projection.
        """
        if type(limit) is not int or not 1 <= limit <= 100:
            raise TaskInvalidRequest("limit")
        if run_id is not None and not _valid_run_id(run_id):
            raise TaskInvalidRequest("run_id")
        try:
            async with asyncio.timeout(self._seconds):
                latest = await self._latest_owned(owner, task_id)
                parsed = self._parse_cursor(cursor, owner, task_id) if cursor is not None else None
                if parsed is not None and run_id is not None and parsed["run_id"] != run_id:
                    raise TaskInvalidRequest("run_id")
                selected_run_id = parsed["run_id"] if parsed is not None else run_id
                current = latest
                if selected_run_id is not None and selected_run_id != latest[0].run_id:
                    current = await self._owned(owner, task_id, run_id=selected_run_id)
                    self._same_chain(latest, current)
                description, _ = current
                before = parsed["before_event_id"] if parsed is not None else None
                navigation = await self._navigation(owner, task_id, current)
                token, events = b"", []
                for _ in range(64):
                    page = await self.client.workflow_service.get_workflow_execution_history_reverse(
                        GetWorkflowExecutionHistoryReverseRequest(
                            namespace=self.client.namespace,
                            execution=WorkflowExecution(workflow_id=task_id, run_id=description.run_id),
                            maximum_page_size=100, next_page_token=token,
                        ), retry=True, timeout=self._timeout,
                    )
                    for event in page.history.events:
                        if before is not None and event.event_id >= before:
                            continue
                        events.append({"event_id": event.event_id,
                                       "type": EventType.Name(event.event_type).removeprefix("EVENT_TYPE_").lower(),
                                       "time": event.event_time.ToDatetime().isoformat() + "+00:00"})
                        if len(events) == limit:
                            next_cursor = self._cursor(owner, task_id, description.run_id, event.event_id) if event.event_id > 1 else None
                            return {"task_id": task_id, "run_id": description.run_id, "order": "descending",
                                    "events": events, "next_cursor": next_cursor, **navigation}
                    token = page.next_page_token
                    if not token:
                        return {"task_id": task_id, "run_id": description.run_id, "order": "descending",
                                "events": events, "next_cursor": None, **navigation}
                raise TaskUnavailable({"operation": "history_scan_limit"})
        except TaskServiceError:
            raise
        except RPCError as error:
            if error.status == RPCStatusCode.NOT_FOUND:
                raise TaskNotFound() from None
            raise TaskUnavailable() from None
        except Exception:
            raise TaskUnavailable() from None
