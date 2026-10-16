"""Bounded namespace readiness, coalesced independently of business requests."""

from __future__ import annotations

import asyncio
from datetime import timedelta

from temporalio.api.enums.v1 import NamespaceState
from temporalio.api.workflowservice.v1 import DescribeNamespaceRequest
from temporalio.client import Client

READINESS_TIMEOUT_SECONDS = 1.0
READINESS_TTL_SECONDS = 1.0


class NamespaceReadiness:
    def __init__(self, client: Client):
        self.client = client
        self._readiness_task: asyncio.Task[bool] | None = None
        self._readiness_expires_at = 0.0
        self._readiness_result = False

    async def _check_readiness(self) -> bool:
        """Read namespace metadata without creating or changing any task."""
        ready = False
        try:
            async with asyncio.timeout(READINESS_TIMEOUT_SECONDS):
                response = await self.client.workflow_service.describe_namespace(
                    DescribeNamespaceRequest(namespace=self.client.namespace), retry=False,
                    timeout=timedelta(seconds=READINESS_TIMEOUT_SECONDS),
                )
                ready = (response.namespace_info.state == NamespaceState.NAMESPACE_STATE_REGISTERED
                         and response.namespace_info.name == self.client.namespace)
        except asyncio.CancelledError:
            raise
        except Exception:
            # Public probes intentionally expose no endpoint, namespace, status
            # detail, or raw service error. Failure is readiness, not liveness.
            pass
        self._readiness_result = ready
        self._readiness_expires_at = asyncio.get_running_loop().time() + READINESS_TTL_SECONDS
        return ready

    async def readiness(self) -> bool:
        """Singleflight plus a short success/failure cache bounds probe RPCs.

        Cancellation of one HTTP caller does not cancel the shared, independently
        bounded namespace read. This check does not prove Workflow Start rights.
        """
        if asyncio.get_running_loop().time() < self._readiness_expires_at:
            return self._readiness_result
        if self._readiness_task is None or self._readiness_task.done():
            self._readiness_task = asyncio.create_task(self._check_readiness())
        return await asyncio.shield(self._readiness_task)
