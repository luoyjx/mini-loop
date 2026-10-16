"""Resource-bounded entry point for the opt-in local Temporal fault prototype."""

from __future__ import annotations

import argparse
import asyncio
from datetime import timedelta

from temporalio.client import Client
from temporalio.contrib.deepagents import DeepAgentsPlugin
from temporalio.worker import PollerBehaviorSimpleMaximum, Worker

from mini_loop_deepagents.temporal_prototype.activities import configure_database, memory_get, memory_put
from mini_loop_deepagents.temporal_prototype.model_fixture import model_provider
from mini_loop_deepagents.temporal_prototype.workflow import DurableAgentWorkflow


async def serve(address: str, task_queue: str, database: str, identity: str) -> None:
    configure_database(database, identity)
    client = await Client.connect(address, identity=identity)
    plugin = DeepAgentsPlugin(model_provider=model_provider)
    worker = Worker(
        client, task_queue=task_queue, workflows=[DurableAgentWorkflow],
        activities=[memory_put, memory_get], plugins=[plugin], identity=identity,
        max_cached_workflows=0, max_concurrent_workflow_tasks=2,
        max_concurrent_activities=1, disable_eager_activity_execution=True,
        workflow_task_poller_behavior=PollerBehaviorSimpleMaximum(maximum=2),
        activity_task_poller_behavior=PollerBehaviorSimpleMaximum(maximum=1),
        max_heartbeat_throttle_interval=timedelta(seconds=0.5),
        graceful_shutdown_timeout=timedelta(seconds=2),
    )
    print(f"TEMPORAL_PROTOTYPE_READY identity={identity}", flush=True)
    await worker.run()


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--address", required=True)
    parser.add_argument("--task-queue", required=True)
    parser.add_argument("--database", required=True)
    parser.add_argument("--identity", required=True)
    args = parser.parse_args()
    asyncio.run(serve(args.address, args.task_queue, args.database, args.identity))


if __name__ == "__main__":
    main()
