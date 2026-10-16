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
from temporalio.api.workflowservice.v1 import GetWorkflowExecutionHistoryReverseRequest
from temporalio.client import Client
from temporalio.common import WorkflowIDConflictPolicy, WorkflowIDReusePolicy
from temporalio.exceptions import WorkflowAlreadyStartedError
from temporalio.service import RPCError, RPCStatusCode


TASK_VERSION = "task-v1"
WORKFLOW_TYPE = "TaskWorkflow"
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
        except RPCError as error:
            if error.status == RPCStatusCode.NOT_FOUND:
                raise TaskNotFound() from None
            raise TaskUnavailable() from None
        except Exception:
            raise TaskUnavailable() from None

    async def _owned(self, owner: str, task_id: str, *, run_id: str | None = None) -> tuple[Any, dict[str, Any]]:
        self._validate_identity(owner, task_id)
        handle = self.client.get_workflow_handle(task_id, run_id=run_id)
        description = await self._rpc(handle.describe(rpc_timeout=self._timeout))
        try:
            async with asyncio.timeout(self._seconds):
                memo = dict(await description.memo())
            valid = (
                description.id == task_id and description.workflow_type == WORKFLOW_TYPE
                and (run_id is None or description.run_id == run_id)
                and memo.get("owner") == owner and memo.get("task_id") == task_id
                and memo.get("version") == TASK_VERSION
                and isinstance(memo.get("request_sha256"), str) and _SHA256.fullmatch(memo["request_sha256"])
            )
        except Exception:
            raise TaskNotFound() from None
        if not valid:
            raise TaskNotFound()
        return description, memo

    @staticmethod
    def _summary(description: Any) -> dict[str, Any]:
        status = _STATUSES.get(getattr(description.status, "name", None))
        if status is None:
            raise TaskUnavailable()
        return {"task_id": description.id, "run_id": description.run_id,
                "status": status, "version": TASK_VERSION}

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
        digest = hashlib.sha256(_canonical({"payload": payload, "task_queue": self.task_queue})).hexdigest()
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
            description, existing = await self._owned(owner, task_id)
            if existing["request_sha256"] != digest:
                raise TaskConflict(task_id)
            return {**self._summary(description), "reused": True, "write_confirmed": True}
        except (Exception, asyncio.CancelledError):
            # The wrapper does not create another business submission. The SDK
            # may retry this same Start RPC/request ID; retain the stable Task ID.
            raise TaskSubmissionUnknown(task_id) from None
        run_id = handle.result_run_id or handle.first_execution_run_id
        if not isinstance(run_id, str) or not run_id:
            raise TaskSubmissionUnknown(task_id)
        return {"task_id": task_id, "run_id": run_id, "status": "running", "version": TASK_VERSION,
                "reused": False, "write_confirmed": True}

    async def get(self, owner: str, task_id: str) -> dict[str, Any]:
        description, _ = await self._owned(owner, task_id)
        return self._summary(description)

    async def result(self, owner: str, task_id: str) -> dict[str, Any]:
        description, _ = await self._owned(owner, task_id)
        summary = self._summary(description)
        if summary["status"] not in _TERMINAL:
            return {**summary, "ready": False}
        if summary["status"] != "succeeded":
            return {**summary, "ready": True, "result": None, "error": {"code": "workflow_" + summary["status"]}}
        handle = self.client.get_workflow_handle(task_id, run_id=description.run_id)
        value = await self._rpc(handle.result(follow_runs=False, rpc_timeout=self._timeout))
        try:
            value = json.loads(_canonical(value))
        except (ValueError, TypeError):
            raise TaskUnavailable() from None
        return {**summary, "ready": True, "result": value}

    async def cancel(self, owner: str, task_id: str) -> dict[str, Any]:
        description, _ = await self._owned(owner, task_id)
        summary = self._summary(description)
        if summary["status"] in _TERMINAL:
            return {**summary, "cancel_requested": False}
        first_run_id = getattr(description.raw_info, "first_run_id", "") or description.run_id
        handle = self.client.get_workflow_handle(task_id, first_execution_run_id=first_run_id)
        try:
            async with asyncio.timeout(self._seconds):
                await handle.cancel(rpc_timeout=self._timeout)
        except RPCError as error:
            if error.status in {RPCStatusCode.NOT_FOUND, RPCStatusCode.FAILED_PRECONDITION}:
                current = await self.get(owner, task_id)
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

    async def events(self, owner: str, task_id: str, cursor: str | None = None, limit: int = 50) -> dict[str, Any]:
        """Read native retained History, newest first; no token-stream guarantee.

        The signed exclusive event-ID boundary survives appended History and API
        restarts. Old pages scan from the head under one deadline and a 64-page
        cap; this prototype has no persistent event projection/index of its own.
        """
        if type(limit) is not int or not 1 <= limit <= 100:
            raise TaskInvalidRequest("limit")
        try:
            async with asyncio.timeout(self._seconds):
                description, _ = await self._owned(owner, task_id)
                before = None
                if cursor is not None:
                    parsed = self._parse_cursor(cursor, owner, task_id)
                    before = parsed["before_event_id"]
                    if parsed["run_id"] != description.run_id:
                        description, _ = await self._owned(owner, task_id, run_id=parsed["run_id"])
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
                                    "events": events, "next_cursor": next_cursor}
                    token = page.next_page_token
                    if not token:
                        return {"task_id": task_id, "run_id": description.run_id, "order": "descending",
                                "events": events, "next_cursor": None}
                raise TaskUnavailable({"operation": "history_scan_limit"})
        except TaskServiceError:
            raise
        except RPCError as error:
            if error.status == RPCStatusCode.NOT_FOUND:
                raise TaskNotFound() from None
            raise TaskUnavailable() from None
        except Exception:
            raise TaskUnavailable() from None
