"""Temporal Run access with ownership and immutable execution-chain checks.

All Task reads go through this boundary before using native History or results.
A native missing Run stays distinct from rejected authority metadata.
"""

from __future__ import annotations

import asyncio
from datetime import timedelta
from typing import Any

from temporalio.client import Client, WorkflowContinuedAsNewError
from temporalio.service import RPCError, RPCStatusCode

from ..protocol import (
    SHA256_PATTERN, STATUSES, TASK_ID_PATTERN, TRUSTED_WORKFLOWS,
    TaskNotFound, TaskUnavailable, valid_run_id,
)


class RunMissing(TaskNotFound):
    """Native NOT_FOUND, distinct internally from rejected authority metadata."""


class OwnedRuns:
    def __init__(self, client: Client, timeout_seconds: float):
        self.client = client
        self._seconds = timeout_seconds
        self._timeout = timedelta(seconds=timeout_seconds)

    @staticmethod
    def _validate_identity(owner: str, task_id: str) -> None:
        if not isinstance(owner, str) or not owner or not isinstance(task_id, str) or not TASK_ID_PATTERN.fullmatch(task_id):
            raise TaskNotFound()

    async def rpc(self, call: Any) -> Any:
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

    async def get(self, owner: str, task_id: str, *, run_id: str | None = None) -> tuple[Any, dict[str, Any]]:
        self._validate_identity(owner, task_id)
        handle = self.client.get_workflow_handle(task_id, run_id=run_id)
        try:
            description = await self.rpc(handle.describe(rpc_timeout=self._timeout))
        except TaskNotFound:
            raise RunMissing() from None
        try:
            async with asyncio.timeout(self._seconds):
                memo = dict(await description.memo())
            valid = (
                description.id == task_id and valid_run_id(description.run_id)
                and TRUSTED_WORKFLOWS.get(description.workflow_type) == memo.get("version")
                and memo.get("version") in TRUSTED_WORKFLOWS.values()
                and (run_id is None or description.run_id == run_id)
                and memo.get("owner") == owner and memo.get("task_id") == task_id
                and isinstance(memo.get("request_sha256"), str) and SHA256_PATTERN.fullmatch(memo["request_sha256"])
            )
        except Exception:
            raise TaskNotFound() from None
        if not valid:
            raise TaskNotFound()
        return description, memo

    @staticmethod
    def first_run_id(description: Any) -> str:
        first = getattr(description.raw_info, "first_run_id", "")
        if not valid_run_id(first):
            # Guessing the current Run would lose the native chain guard at CAN.
            raise TaskUnavailable()
        return first

    @classmethod
    def require_same_chain(cls, original: tuple[Any, dict[str, Any]], current: tuple[Any, dict[str, Any]]) -> None:
        left, left_memo = original
        right, right_memo = current
        if (cls.first_run_id(left) != cls.first_run_id(right)
                or left.workflow_type != right.workflow_type
                or any(left_memo[key] != right_memo[key] for key in ("owner", "task_id", "version", "request_sha256"))):
            raise TaskNotFound()

    async def latest(self, owner: str, task_id: str) -> tuple[Any, dict[str, Any]]:
        try:
            async with asyncio.timeout(self._seconds):
                original = await self.get(owner, task_id)
                current = original
                # Continue-as-New is atomic, but a read can still observe an old
                # closed Run. Refresh at most twice and never cross a new chain.
                for _ in range(2):
                    if getattr(current[0].status, "name", None) != "CONTINUED_AS_NEW":
                        break
                    current = await self.get(owner, task_id)
                    self.require_same_chain(original, current)
                return current
        except TimeoutError:
            raise TaskUnavailable() from None

    @classmethod
    def summary(cls, description: Any, memo: dict[str, Any]) -> dict[str, Any]:
        status = STATUSES.get(getattr(description.status, "name", None))
        if status is None:
            raise TaskUnavailable()
        return {"task_id": description.id, "run_id": description.run_id,
                "status": status, "version": memo["version"],
                "first_execution_run_id": cls.first_run_id(description)}
