"""Real production Worker connection, with the existing deterministic model seam."""
from __future__ import annotations

import argparse

from .task_rollout_worker_fixture import make_fixture_provider
from mini_loop_deepagents.task_server.connection import TemporalConnectionSettings
from mini_loop_deepagents.task_server.worker import run_process


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--address", required=True)
    parser.add_argument("--task-queue", required=True)
    parser.add_argument("--identity", required=True)
    parser.add_argument("--barrier-directory", required=True)
    args = parser.parse_args()
    settings = TemporalConnectionSettings.from_env()
    run_process(args.address, args.task_queue, args.identity, connection=settings,
                model_provider=make_fixture_provider(args.barrier_directory), activity_slots=1,
                workflow_slots=2, shutdown_grace_seconds=1, shutdown_timeout_seconds=5)


if __name__ == "__main__":
    main()
