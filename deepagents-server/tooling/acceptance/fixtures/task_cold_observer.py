"""Owned HTTP submitter/observer with fresh SDK connections after cold restart.

Commands and results are private atomic mailbox files. No credentials or task
contents enter stdout. The parent has no Temporal Client or HTTP connection.
"""
from __future__ import annotations

import argparse
import asyncio
from datetime import timedelta
import hashlib
import json
import os
from pathlib import Path
import time

import httpx
from temporalio.client import Client
from temporalio.api.enums.v1 import PendingActivityState
from temporalio.api.history.v1 import History


def private_json(path, value):
    encoded = json.dumps(value)
    if len(encoded.encode()) > 4 * 1024 * 1024:
        raise ValueError("observer_result_limit")
    temporary = path.with_suffix(".tmp")
    with os.fdopen(os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "w") as stream:
        stream.write(encoded)
    temporary.replace(path)


async def execute(command, client, http, configuration, secrets):
    operation = command["operation"]
    if operation == "http":
        owner = command.get("owner")
        headers = {"Authorization": "Bearer " + secrets["tokens"][owner]} if owner else {}
        if "key" in command:
            headers["Idempotency-Key"] = command["key"]
        response = await http.request(command.get("method", "GET"),
            configuration["api_urls"][command.get("replica", 0)] + command["path"],
            headers=headers, json=command.get("json"), params=command.get("params"))
        return {"status_code": response.status_code, "body": response.json(),
            "cache_control": response.headers.get("cache-control")}
    task_id = command["task_id"]
    handle = client.get_workflow_handle(task_id, run_id=command.get("run_id"))
    if operation == "snapshot":
        return await handle.query("snapshot", rpc_timeout=timedelta(seconds=5))
    if operation == "describe":
        value = await handle.describe(rpc_timeout=timedelta(seconds=5))
        return {"task_id": task_id, "run_id": value.run_id, "status": value.status.name,
            "pending_activities": [{"activity_id": x.activity_id, "attempt": x.attempt,
                "state": PendingActivityState.Name(x.state)} for x in value.raw_description.pending_activities]}
    if operation == "history":
        history = await handle.fetch_history(rpc_timeout=timedelta(seconds=5))
        proto = History(events=history.events).SerializeToString(deterministic=True)
        text = history.to_json().encode()
        for value in [*secrets["tokens"].values(), secrets["cursor"], secrets["model"]]:
            for needle in (value.encode(), json.dumps(value)[1:-1].encode()):
                if needle in proto or needle in text:
                    raise ValueError("history_credential_match")
        started = history.events[0].workflow_execution_started_event_attributes
        continued = [event.workflow_execution_continued_as_new_event_attributes for event in history.events
            if event.HasField("workflow_execution_continued_as_new_event_attributes")]
        activity_events = []
        for event in history.events:
            for kind in ("scheduled", "started", "completed", "failed", "timed_out", "canceled"):
                field = "activity_task_" + kind + "_event_attributes"
                if not event.HasField(field): continue
                attributes = getattr(event, field)
                row = {"event_id": event.event_id, "time_ns": event.event_time.ToNanoseconds(), "kind": kind}
                if kind == "scheduled": row.update(activity_id=attributes.activity_id, activity_type=attributes.activity_type.name)
                else: row["scheduled_event_id"] = attributes.scheduled_event_id
                if kind == "started": row.update(attempt=attributes.attempt, identity=attributes.identity)
                activity_events.append(row)
        return {"task_id": task_id, "run_id": command["run_id"], "event_count": len(history.events),
            "first_execution_run_id": started.first_execution_run_id,
            "previous_run_id": started.continued_execution_run_id or None,
            "next_run_id": continued[0].new_execution_run_id if continued else None,
            "continued_input_sha256": hashlib.sha256(continued[0].input.SerializeToString(deterministic=True)).hexdigest() if continued else None,
            "start_input_sha256": hashlib.sha256(started.input.SerializeToString(deterministic=True)).hexdigest(),
            "workflow_task_failures": sum(event.HasField("workflow_task_failed_event_attributes") for event in history.events),
            "history_sha256": hashlib.sha256(proto).hexdigest(), "credential_matches": 0, "activity_events": activity_events}
    raise ValueError("unknown_observer_operation")


async def serve(directory):
    configuration = json.loads((directory / "configuration.json").read_text())
    secrets = json.loads(Path(configuration["secrets_file"]).read_text())
    deadline = time.monotonic() + 15
    while True:
        try:
            client = await asyncio.wait_for(Client.connect(configuration["temporal_address"], lazy=False,
                identity="cold-observer-" + str(os.getpid())), timeout=3)
            break
        except (Exception, asyncio.TimeoutError):
            if time.monotonic() >= deadline:
                raise RuntimeError("cold_observer_connect_failed") from None
            await asyncio.sleep(0.1)
    async with httpx.AsyncClient(timeout=8, trust_env=False, follow_redirects=False) as http:
        private_json(directory / "ready.json", {"pid": os.getpid(), "connected_at_monotonic_ns": time.monotonic_ns()})
        print("TASK_COLD_OBSERVER_READY", flush=True)
        while True:
            for request in sorted(directory.glob("request-*.json")):
                result = directory / request.name.replace("request-", "response-")
                if result.exists():
                    continue
                try:
                    if request.stat().st_size > 32 * 1024:
                        raise ValueError("observer_request_limit")
                    value = await execute(json.loads(request.read_text()), client, http, configuration, secrets)
                    response = {"ok": True, "value": value}
                except Exception as error:
                    response = {"ok": False, "error_type": type(error).__name__}
                private_json(result, response)
            await asyncio.sleep(0.02)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", type=Path, required=True)
    args = parser.parse_args()
    asyncio.run(serve(args.directory))


if __name__ == "__main__":
    main()
