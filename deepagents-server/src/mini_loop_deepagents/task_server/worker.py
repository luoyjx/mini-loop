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
from mini_loop_deepagents.task_server.runtime import MODEL_ALIAS, TaskWorkflow


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
) -> None:
    """Run the production Workflow with an optional trusted test-process provider.

    The Python injection seam is for tests; the production CLI has no fixture or
    provider flag. Slots bound one worker, not cluster or upstream-provider quota.
    """
    if not 1 <= activity_slots <= 32 or not 1 <= workflow_slots <= 32:
        raise ValueError("worker slots must be 1–32")
    if model_provider is None:
        model_provider = make_model_provider(AgentSettings.from_env())
    client = await Client.connect(address, namespace=namespace, identity=identity)
    worker = Worker(
        client,
        task_queue=task_queue,
        workflows=[TaskWorkflow],
        plugins=[DeepAgentsPlugin(model_provider=model_provider)],
        identity=identity,
        max_cached_workflows=0,
        max_concurrent_workflow_tasks=workflow_slots,
        max_concurrent_activities=activity_slots,
        disable_eager_activity_execution=True,
        workflow_task_poller_behavior=PollerBehaviorSimpleMaximum(maximum=workflow_slots),
        activity_task_poller_behavior=PollerBehaviorSimpleMaximum(maximum=activity_slots),
        max_heartbeat_throttle_interval=timedelta(seconds=0.5),
        graceful_shutdown_timeout=timedelta(seconds=2),
    )
    print(f"TASK_WORKER_READY identity={identity}", flush=True)
    await worker.run()


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--address", required=True)
    parser.add_argument("--task-queue", required=True)
    parser.add_argument("--identity", required=True)
    parser.add_argument("--namespace", default="default")
    parser.add_argument("--activity-slots", type=int, default=1)
    parser.add_argument("--workflow-slots", type=int, default=2)
    args = parser.parse_args()
    asyncio.run(serve(args.address, args.task_queue, args.identity,
                      namespace=args.namespace, activity_slots=args.activity_slots, workflow_slots=args.workflow_slots))


if __name__ == "__main__":
    main()
