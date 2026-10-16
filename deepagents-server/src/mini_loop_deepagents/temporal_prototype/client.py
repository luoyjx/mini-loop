"""Controlled local client; owner binding is not a public network auth service."""

from __future__ import annotations

from dataclasses import dataclass
import hashlib
import json
from pathlib import Path
from typing import Any

from temporalio.client import Client
from temporalio.common import WorkflowIDReusePolicy

from mini_loop_deepagents.temporal_prototype import CODE_VERSION
from mini_loop_deepagents.temporal_prototype.activities import bind_workflow, lookup_owner, release_gate
from mini_loop_deepagents.temporal_prototype.workflow import DurableAgentWorkflow


@dataclass(frozen=True)
class PrototypeHandle:
    workflow_id: str
    run_id: str

    @property
    def id(self) -> str:
        return self.workflow_id


class PrototypeClient:
    """Trusted-process fixture; raw Temporal access must remain private."""

    def __init__(self, client: Client, *, database: str | Path, owner: str, task_queue: str | None = None) -> None:
        if not isinstance(owner, str) or not owner:
            raise ValueError("A trusted owner is required")
        self.client, self.database, self.owner = client, Path(database), owner
        self.task_queue = task_queue

    def _check(self, handle: PrototypeHandle | str) -> PrototypeHandle:
        if isinstance(handle, str):
            handle = PrototypeHandle(handle, "")
        if lookup_owner(self.database, handle.workflow_id) != self.owner:
            raise PermissionError("Workflow is not owned by this client")
        return handle

    def task_handle(self, workflow_id: str) -> PrototypeHandle:
        return self._check(workflow_id)

    async def start(
        self, workflow_id: str, message: str, *, task_queue: str | None = None,
        continue_as_new: bool = True, block_after_effect: bool = True,
    ) -> PrototypeHandle:
        selected_queue = task_queue or self.task_queue
        if not selected_queue:
            raise ValueError("A task queue is required")
        payload = {"owner": self.owner, "message": message, "version": CODE_VERSION,
                   "continue_as_new": continue_as_new, "block_after_effect": block_after_effect}
        digest = hashlib.sha256(json.dumps(payload, sort_keys=True).encode()).hexdigest()
        bind_workflow(self.database, workflow_id, self.owner, digest)
        # The SDK may retry the same Start RPC/request ID. This wrapper does not
        # create another business submission: after an uncertain response, check
        # the stable workflow ID rather than inventing a new execution.
        handle = await self.client.start_workflow(DurableAgentWorkflow.run, payload,
                                                  id=workflow_id, task_queue=selected_queue,
                                                  id_reuse_policy=WorkflowIDReusePolicy.REJECT_DUPLICATE)
        return PrototypeHandle(workflow_id, handle.first_execution_run_id or handle.result_run_id or "")

    async def snapshot(self, handle: PrototypeHandle | str) -> dict[str, Any]:
        handle = self._check(handle)
        return await self.client.get_workflow_handle(handle.workflow_id).query("snapshot")

    async def result(self, handle: PrototypeHandle | str) -> dict[str, Any]:
        handle = self._check(handle)
        return await self.client.get_workflow_handle(handle.workflow_id).result(follow_runs=True)

    async def cancel(self, handle: PrototypeHandle | str) -> None:
        handle = self._check(handle)
        await self.client.get_workflow_handle(handle.workflow_id).cancel()

    def release(self, handle: PrototypeHandle | str) -> None:
        handle = self._check(handle)
        release_gate(self.database, handle.workflow_id)
