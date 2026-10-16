"""Task wire vocabulary and HTTP-facing application contract.

This module has no Temporal, HTTP, model, or process dependency. Versioned
identities and canonical bytes are persistent protocol, not refactoring details.
"""

from __future__ import annotations

import hashlib
import json
import re
from typing import Any, Protocol
from uuid import UUID

TASK_VERSION = "task-v2"
WORKFLOW_TYPE = "TaskWorkflowV2"
TASK_ID_PATTERN = re.compile(r"task-[a-f0-9]{64}\Z")
SHA256_PATTERN = re.compile(r"[a-f0-9]{64}\Z")
STATUSES = {
    "RUNNING": "running", "COMPLETED": "succeeded", "FAILED": "failed",
    "CANCELED": "cancelled", "TIMED_OUT": "timed_out", "TERMINATED": "terminated",
    "CONTINUED_AS_NEW": "continued_as_new",
}
TERMINAL_STATUSES = frozenset({"succeeded", "failed", "cancelled", "timed_out", "terminated"})


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


def canonical_json(value: Any) -> bytes:
    return json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode()


def task_id_for(owner: str, key: str) -> str:
    """Domain separation plus a JSON tuple avoids ambiguous owner/key joins."""
    return "task-" + hashlib.sha256(b"mini-loop:task-id:v1\0" + canonical_json([owner, key])).hexdigest()


def request_digest(task_id: str, owner: str, message: str, version: str, task_queue: str) -> str:
    # Digest the immutable submission; continuation state is not a submission.
    payload = {"task_id": task_id, "owner": owner, "message": message, "version": version}
    return hashlib.sha256(canonical_json({"payload": payload, "task_queue": task_queue})).hexdigest()


def valid_run_id(value: Any) -> bool:
    try:
        return isinstance(value, str) and str(UUID(value)) == value
    except (ValueError, TypeError, AttributeError):
        return False


class TaskOperations(Protocol):
    """Trusted service injected into the HTTP adapter; owners are authenticated."""

    async def readiness(self) -> bool: ...

    async def submit(self, owner: str, key: str, message: str) -> dict[str, Any]: ...

    async def get(self, owner: str, task_id: str) -> dict[str, Any]: ...

    async def result(self, owner: str, task_id: str) -> dict[str, Any]: ...

    async def cancel(self, owner: str, task_id: str) -> dict[str, Any]: ...

    async def events(
        self, owner: str, task_id: str, cursor: str | None = None,
        limit: int = 50, *, run_id: str | None = None,
    ) -> dict[str, Any]: ...
