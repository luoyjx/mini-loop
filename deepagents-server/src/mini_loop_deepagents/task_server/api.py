"""Independent, bounded HTTP admission for Temporal-owned long-running Tasks.

Stable CLI and compatibility imports. HTTP composition lives in http.py; this
module alone owns environment loading, Temporal connection and server startup.
"""

from __future__ import annotations

import argparse
from contextlib import asynccontextmanager
import os

from starlette.applications import Starlette

from mini_loop_deepagents.config import load_tokens
from .connection import TemporalConfigurationError, TemporalConnectionSettings, connect_temporal
from .http import AccessBoundary, MAX_BODY_BYTES, create_app, error_response

__all__ = ["AccessBoundary", "MAX_BODY_BYTES", "create_app", "error_response", "main"]

def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--address", default="127.0.0.1:8123")
    parser.add_argument("--temporal-address", default="127.0.0.1:7233")
    parser.add_argument("--namespace", default="default")
    parser.add_argument("--task-queue", default="mini-loop-task-v1")
    parser.add_argument("--max-inflight", type=int, default=32)
    args = parser.parse_args()
    import uvicorn
    from .service import TaskService

    tokens = load_tokens()
    try:
        connection = TemporalConnectionSettings.from_env()
    except TemporalConfigurationError as error:
        parser.error(str(error))
    secret = os.environ.get("MINILOOP_TASK_SERVER_CURSOR_SECRET", "")
    if len(secret) < 32 or not secret.isascii() or any(c.isspace() for c in secret):
        parser.error("MINILOOP_TASK_SERVER_CURSOR_SECRET requires 32 or more non-whitespace ASCII characters")
    try:
        host, port_text = args.address.rsplit(":", 1)
        port = int(port_text)
        if not host or not 1 <= port <= 65535:
            raise ValueError()
    except ValueError:
        parser.error("--address must be HOST:PORT")

    @asynccontextmanager
    async def lifespan(app: Starlette):
        client = await connect_temporal(args.temporal_address, namespace=args.namespace, connection=connection)
        app.state.task_service = TaskService(client, task_queue=args.task_queue, cursor_secret=secret)
        yield

    app = create_app(None, tokens=tokens, max_inflight=args.max_inflight, lifespan=lifespan)
    uvicorn.run(app, host=host.strip("[]"), port=port, access_log=False, log_level="warning",
                limit_concurrency=args.max_inflight + 8, timeout_keep_alive=5, timeout_graceful_shutdown=10)


if __name__ == "__main__":
    main()
