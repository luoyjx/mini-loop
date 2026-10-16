"""Task application operations backed by retained Temporal Workflow History.

TaskService remains the public composition entry. It owns submission, result and
cancellation semantics; backend adapters own Run authority, pagination, signed
cursors and readiness. No application queue or durable task ledger is introduced.
"""

from __future__ import annotations

import asyncio
from datetime import timedelta
import json
import math
from typing import Any

from temporalio.client import Client, WorkflowContinuedAsNewError
from temporalio.common import WorkflowIDConflictPolicy, WorkflowIDReusePolicy
from temporalio.exceptions import WorkflowAlreadyStartedError
from temporalio.service import RPCError, RPCStatusCode

from .backend.cursors import CursorCodec
from .backend.history import HistoryReader
from .backend.readiness import NamespaceReadiness
from .backend.runs import OwnedRuns
from .protocol import (
    TASK_VERSION, TERMINAL_STATUSES, WORKFLOW_TYPE,
    TaskConflict, TaskInvalidRequest, TaskNotFound, TaskServiceError,
    TaskSubmissionUnknown, TaskUnavailable, canonical_json, request_digest, task_id_for,
)

# Keep existing caller imports; new protocol consumers should import protocol.
_request_digest = request_digest
__all__ = [
    "TaskService", "TaskServiceError", "TaskNotFound", "TaskConflict",
    "TaskInvalidRequest", "TaskSubmissionUnknown", "TaskUnavailable",
    "TASK_VERSION", "WORKFLOW_TYPE", "task_id_for",
]


class TaskService:
    def __init__(
        self, client: Client, *, task_queue: str, cursor_secret: bytes | str,
        rpc_timeout_seconds: float = 5.0, workflow_execution_timeout_seconds: float = 86400,
    ):
        if not isinstance(task_queue, str) or not task_queue:
            raise ValueError("A task queue is required")
        cursors = CursorCodec(cursor_secret)
        if any(not math.isfinite(value) or value <= 0 for value in (rpc_timeout_seconds, workflow_execution_timeout_seconds)):
            raise ValueError("RPC and Workflow timeouts must be positive and finite")
        self.client, self.task_queue = client, task_queue
        self._seconds = rpc_timeout_seconds
        self._timeout = timedelta(seconds=rpc_timeout_seconds)
        self._execution_timeout = timedelta(seconds=workflow_execution_timeout_seconds)
        self._runs = OwnedRuns(client, rpc_timeout_seconds)
        self._history = HistoryReader(self._runs, cursors, rpc_timeout_seconds)
        self._readiness = NamespaceReadiness(client)

    async def readiness(self) -> bool:
        return await self._readiness.readiness()

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
        digest = request_digest(task_id, owner, message, TASK_VERSION, self.task_queue)
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
            description, existing = await self._runs.latest(owner, task_id)
            if existing["request_sha256"] != digest:
                raise TaskConflict(task_id)
            return {**self._runs.summary(description, existing), "reused": True, "write_confirmed": True}
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
        description, memo = await self._runs.latest(owner, task_id)
        return self._runs.summary(description, memo)

    async def result(self, owner: str, task_id: str) -> dict[str, Any]:
        try:
            async with asyncio.timeout(self._seconds):
                original = await self._runs.latest(owner, task_id)
                current = original
                for _ in range(2):
                    description, memo = current
                    self._runs.require_same_chain(original, current)
                    summary = self._runs.summary(description, memo)
                    if summary["status"] not in TERMINAL_STATUSES:
                        return {**summary, "ready": False}
                    if summary["status"] != "succeeded":
                        return {**summary, "ready": True, "result": None, "error": {"code": "workflow_" + summary["status"]}}
                    handle = self.client.get_workflow_handle(task_id, run_id=description.run_id)
                    try:
                        value = await self._runs.rpc(handle.result(follow_runs=False, rpc_timeout=self._timeout))
                    except WorkflowContinuedAsNewError:
                        current = await self._runs.latest(owner, task_id)
                        continue
                    return {**summary, "ready": True, "result": json.loads(canonical_json(value))}
        except (ValueError, TypeError, TimeoutError):
            raise TaskUnavailable() from None
        raise TaskUnavailable()

    async def cancel(self, owner: str, task_id: str) -> dict[str, Any]:
        original = await self._runs.latest(owner, task_id)
        description, memo = original
        summary = self._runs.summary(description, memo)
        if summary["status"] in TERMINAL_STATUSES:
            return {**summary, "cancel_requested": False}
        first_run_id = self._runs.first_run_id(description)
        handle = self.client.get_workflow_handle(task_id, first_execution_run_id=first_run_id)
        try:
            async with asyncio.timeout(self._seconds):
                await handle.cancel(rpc_timeout=self._timeout)
        except RPCError as error:
            if error.status in {RPCStatusCode.NOT_FOUND, RPCStatusCode.FAILED_PRECONDITION}:
                refreshed = await self._runs.latest(owner, task_id)
                self._runs.require_same_chain(original, refreshed)
                current = self._runs.summary(*refreshed)
                if current["status"] in TERMINAL_STATUSES:
                    return {**current, "cancel_requested": False}
            raise TaskUnavailable() from None
        except Exception:
            raise TaskUnavailable() from None
        return {**summary, "cancel_requested": True}

    async def events(
        self, owner: str, task_id: str, cursor: str | None = None, limit: int = 50,
        *, run_id: str | None = None,
    ) -> dict[str, Any]:
        """Read retained History within one Run; cursors never merge Run chains."""
        return await self._history.events(owner, task_id, cursor, limit, run_id=run_id)
