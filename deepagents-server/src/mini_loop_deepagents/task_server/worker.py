"""Independent Task worker: python -m mini_loop_deepagents.task_server.worker.

The trusted deployment environment selects the OpenAI model and credentials.
HTTP task input cannot select providers, supply keys, or enable test fixtures.
"""

from __future__ import annotations

import argparse
import asyncio
from collections.abc import Callable
from datetime import timedelta
from typing import Any

from langchain_openai import ChatOpenAI
from temporalio.client import Client
from temporalio.contrib.deepagents import DeepAgentsPlugin
from temporalio.worker import PollerBehaviorSimpleMaximum, Worker

from mini_loop_deepagents.config import AgentSettings
from mini_loop_deepagents.task_server.lifecycle import (
    ProcessLifecycle, lifecycle_log, validate_shutdown_settings,
)
from mini_loop_deepagents.task_server.runtime import MODEL_ALIAS, TaskWorkflow
from mini_loop_deepagents.task_server.runtime_v2 import TaskWorkflowV2


def make_model_provider(settings: AgentSettings) -> Callable[[str], Any]:
    """Capture deployment settings outside Workflow execution, without API keys in History."""
    model = ChatOpenAI(
        model=settings.model.removeprefix("openai:"),
        max_tokens=settings.max_tokens,
        timeout=settings.timeout_seconds,
        # Temporal owns retries; do not stack OpenAI SDK retries underneath.
        max_retries=0,
    )

    def model_provider(name: str) -> Any:
        if name != MODEL_ALIAS:
            raise ValueError("Task worker only accepts its deployment-owned model alias")
        return model
    return model_provider


async def serve(
    address: str,
    task_queue: str,
    identity: str,
    *,
    namespace: str = "default",
    activity_slots: int = 1,
    workflow_slots: int = 2,
    model_provider: Callable[[str], Any] | None = None,
    shutdown_grace_seconds: float = 2.0,
    stop_event: asyncio.Event | None = None,
) -> None:
    """Run the production Workflow with an optional trusted test-process provider.

    The Python injection seam is for tests; the production CLI has no fixture or
    provider flag. Slots bound one worker, not cluster or upstream-provider quota.
    The caller owns signals and hard exits. A stop event requests SDK shutdown:
    stop new polling and drain already fetched work, including buffered Starts.
    """
    if not 1 <= activity_slots <= 32 or not 1 <= workflow_slots <= 32:
        raise ValueError("worker slots must be 1–32")
    # Reuse the same strict grace validation without imposing a library deadline.
    validate_shutdown_settings(shutdown_grace_seconds, 360.0)
    stop = stop_event if stop_event is not None else asyncio.Event()
    if stop.is_set():
        lifecycle_log("TASK_WORKER_STOPPED phase=startup")
        return

    async def connect() -> Any:
        async with asyncio.timeout(10):
            return await Client.connect(address, namespace=namespace, identity=identity)

    connecting = asyncio.create_task(connect())
    stopping = asyncio.create_task(stop.wait())
    run_task: asyncio.Task | None = None
    worker: Worker | None = None

    async def drain() -> None:
        assert worker is not None and run_task is not None
        lifecycle_log("TASK_WORKER_DRAINING")
        shutdown = asyncio.create_task(worker.shutdown())
        try:
            # A startup/fatal error must win over shutdown's completion event,
            # which the SDK may never set if validation failed before run began.
            await asyncio.shield(run_task)
        finally:
            shutdown.cancel()
            await asyncio.gather(shutdown, return_exceptions=True)

    try:
        await asyncio.wait((connecting, stopping), return_when=asyncio.FIRST_COMPLETED)
        if not connecting.done():
            connecting.cancel()
            await asyncio.gather(connecting, return_exceptions=True)
            lifecycle_log("TASK_WORKER_STOPPED phase=startup")
            return
        client = connecting.result()
        if stop.is_set():
            lifecycle_log("TASK_WORKER_STOPPED phase=startup")
            return
        if model_provider is None:
            model_provider = make_model_provider(AgentSettings.from_env())
        if stop.is_set():
            lifecycle_log("TASK_WORKER_STOPPED phase=startup")
            return
        worker = Worker(
            client,
            task_queue=task_queue,
            workflows=[TaskWorkflow, TaskWorkflowV2],
            plugins=[DeepAgentsPlugin(model_provider=model_provider)],
            identity=identity,
            max_cached_workflows=0,
            max_concurrent_workflow_tasks=workflow_slots,
            max_concurrent_activities=activity_slots,
            disable_eager_activity_execution=True,
            workflow_task_poller_behavior=PollerBehaviorSimpleMaximum(maximum=workflow_slots),
            activity_task_poller_behavior=PollerBehaviorSimpleMaximum(maximum=activity_slots),
            max_heartbeat_throttle_interval=timedelta(seconds=0.5),
            graceful_shutdown_timeout=timedelta(seconds=shutdown_grace_seconds),
        )
        run_task = asyncio.create_task(worker.run())
        # is_running becomes true after SDK namespace validation. Observe stop
        # and fatal startup completion before publishing readiness.
        while not worker.is_running and not run_task.done() and not stop.is_set():
            await asyncio.wait((run_task, stopping), timeout=0.01,
                               return_when=asyncio.FIRST_COMPLETED)
        if run_task.done():
            await run_task
            return
        if not stop.is_set():
            print(f"TASK_WORKER_READY identity={identity}", flush=True)
            await asyncio.wait((run_task, stopping), return_when=asyncio.FIRST_COMPLETED)
        if run_task.done():
            await run_task
        else:
            await drain()
        if stop.is_set():
            lifecycle_log("TASK_WORKER_STOPPED phase=drained")
    except asyncio.CancelledError:
        # Library cancellation must retain its semantics; it is not a business
        # Workflow cancellation and must not issue a Workflow cancel request.
        if run_task is not None and not run_task.done():
            await asyncio.shield(drain())
        raise
    finally:
        stopping.cancel()
        if not connecting.done():
            connecting.cancel()
        await asyncio.gather(stopping, connecting, return_exceptions=True)


def run_process(
    address: str,
    task_queue: str,
    identity: str,
    *,
    namespace: str = "default",
    activity_slots: int = 1,
    workflow_slots: int = 2,
    model_provider: Callable[[str], Any] | None = None,
    shutdown_grace_seconds: float = 2.0,
    shutdown_timeout_seconds: float = 15.0,
) -> None:
    """Trusted process entry point shared by the production CLI and fixtures.

    A first signal requests SDK shutdown. A second signal or total stop deadline
    exits this dedicated process with code 70, bypassing potentially unbounded
    asyncio Runner cleanup. Production CLI cannot select a fixture/provider.
    """
    grace, timeout = validate_shutdown_settings(shutdown_grace_seconds, shutdown_timeout_seconds)

    async def run(lifecycle: ProcessLifecycle) -> None:
        stop = asyncio.Event()
        lifecycle.bind(asyncio.get_running_loop(), stop)
        await serve(address, task_queue, identity, namespace=namespace,
                    activity_slots=activity_slots, workflow_slots=workflow_slots,
                    model_provider=model_provider, shutdown_grace_seconds=grace, stop_event=stop)

    with ProcessLifecycle(timeout) as lifecycle:
        asyncio.run(run(lifecycle))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--address", required=True)
    parser.add_argument("--task-queue", required=True)
    parser.add_argument("--identity", required=True)
    parser.add_argument("--namespace", default="default")
    parser.add_argument("--activity-slots", type=int, default=1)
    parser.add_argument("--workflow-slots", type=int, default=2)
    parser.add_argument("--shutdown-grace-seconds", type=float, default=2.0)
    parser.add_argument("--shutdown-timeout-seconds", type=float, default=15.0)
    args = parser.parse_args()
    try:
        validate_shutdown_settings(args.shutdown_grace_seconds, args.shutdown_timeout_seconds)
    except ValueError as error:
        parser.error(str(error))
    run_process(args.address, args.task_queue, args.identity,
                namespace=args.namespace, activity_slots=args.activity_slots, workflow_slots=args.workflow_slots,
                shutdown_grace_seconds=args.shutdown_grace_seconds,
                shutdown_timeout_seconds=args.shutdown_timeout_seconds)


if __name__ == "__main__":
    main()
