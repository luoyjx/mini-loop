"""Bounded real TaskWorkflowV2 Continue-As-New and cold-replay acceptance.

Uses the production API/Worker/Workflow with only a trusted model fixture. Every
gate is inside a model Activity; no Workflow test branches or replacement graph.
Local Temporal SQLite proves restart recovery, not production cluster HA."""
from __future__ import annotations

import argparse
import asyncio
import hashlib
import json
import os
from pathlib import Path
import secrets
import signal
import sys
import time
import traceback
from uuid import uuid4
import httpx

from tooling.paths import MODULE
from tooling.acceptance.support import process as SUPPORT
from tooling.acceptance.support.process import HarnessFailure, ResourceBlocked
from tooling.acceptance.fixtures import task_can_worker_fixture as FIXTURE
from tooling.acceptance.support import http as HTTP
from tooling.acceptance.support.continuation import (
    read_marker,
    release,
    check_replay,
    check_chain,
    check_final,
)


async def exercise(processes, binary, directory, run_id, receipt, persist):
    temporal_port, port_a, port_b = HTTP.free_ports(3)
    address, queue = f"127.0.0.1:{temporal_port}", "task-can-fault-" + run_id
    database, barriers = directory / "temporal.sqlite", directory / "barriers"
    barriers.mkdir(mode=0o700)
    urls = [f"http://127.0.0.1:{port}" for port in (port_a, port_b)]
    token, foreign_token = secrets.token_urlsafe(32), secrets.token_urlsafe(32)
    environment = {"MINILOOP_DEEPAGENTS_TOKENS": json.dumps({token: "alice", foreign_token: "bob"}),
                   "MINILOOP_TASK_SERVER_CURSOR_SECRET": secrets.token_urlsafe(32)}
    serial = {"worker": 0, "api": 0, "server": 0}

    def server():
        serial["server"] += 1
        return processes.spawn(f"temporal-{serial['server']}", [binary, "--disable-config-env", "--disable-config-file",
            "--log-level", "warn", "server", "start-dev", "--ip", "127.0.0.1", "--port", str(temporal_port),
            "--headless", "--db-filename", database], extra_environment={"GOMAXPROCS": "2", "GOMEMLIMIT": "512MiB"})

    def worker():
        serial["worker"] += 1
        name = f"worker-{serial['worker']}"
        return processes.spawn(name, [sys.executable, "-m", "tooling.acceptance.fixtures.task_can_worker_fixture", "--address", address,
            "--task-queue", queue, "--identity", name, "--barrier-directory", barriers])

    def apis():
        serial["api"] += 1
        return [processes.spawn(f"api-{serial['api']}-{index}", [sys.executable, "-m", "mini_loop_deepagents.task_server.api",
            "--address", f"127.0.0.1:{port}", "--temporal-address", address, "--namespace", "default", "--task-queue", queue],
            extra_environment=environment) for index, port in enumerate((port_a, port_b))]

    temporal = server()
    client = await SUPPORT.connect(processes, address)
    workers, api_processes = [worker(), worker()], apis()
    receipt.update(task_queue=queue, authority="Temporal Workflow History", temporal_database=str(database))
    headers = {"Authorization": "Bearer " + token}
    async with httpx.AsyncClient(timeout=8, trust_env=False, follow_redirects=False) as observer:
        for url in urls:
            await HTTP.ready(processes, observer, url)
        message, key = "[can-main] acceptance " + run_id, "can-a-" + run_id
        results = await asyncio.gather(*(HTTP.detached_submit(processes, url, token, key, message) for url in urls))
        accepted = [HTTP.decode(result, 202) for result in results]
        task_a = accepted[0]["task_id"]
        if accepted[1]["task_id"] != task_a or sorted(item["reused"] for item in accepted) != [False, True]:
            raise HarnessFailure("Two APIs did not converge on one retained Task")
        task_b = HTTP.decode(await HTTP.detached_submit(processes, urls[0], token, "can-b-" + run_id, message + " B"), 202)["task_id"]
        initial = {}
        for task_id in (task_a, task_b):
            async def marker(t=task_id):
                return read_marker(barriers, t)
            initial[task_id] = await SUPPORT.eventually(processes, marker, lambda item: item is not None)
        if len({item["pid"] for item in initial.values()}) != 2:
            raise HarnessFailure("Two Tasks did not execute concurrently on independent Worker processes")
        first_run = initial[task_a]["run_id"]
        receipt.update(task_a=task_a, task_b=task_b, first_execution_run_id=first_run, faults=[], snapshots=[])
        receipt["passed"].append("two independent API replicas converge one key; two Tasks reach real model Activities on independent Workers")
        first_page = HTTP.decode(await observer.get(urls[0] + f"/v1/tasks/{task_a}/events", params={"limit": 2}, headers=headers), 200)
        old_cursor = first_page.get("next_cursor")
        if not old_cursor:
            raise HarnessFailure("The first Run did not expose a durable cursor")
        persist()

        async def next_marker(task_id, previous=None):
            async def read():
                return read_marker(barriers, task_id)
            return await SUPPORT.eventually(processes, read, lambda value: value is not None and
                (previous is None or (value["call_id"], value["run_id"]) != (previous["call_id"], previous["run_id"])), timeout=35)

        async def replace_at(marker, *, replace_apis=False, replace_server=False):
            nonlocal workers, api_processes, temporal, client
            task_id = marker["workflow_id"]
            before = await client.get_workflow_handle(task_id).query("snapshot")
            old_pids = [record.process.pid for record in workers]
            targets = list(workers) if replace_server else [record for record in workers if record.process.pid == marker["pid"]]
            if len(targets) != (2 if replace_server else 1):
                raise HarnessFailure("The intended active Worker is not an owned live process")
            for record in targets:
                await asyncio.to_thread(processes.kill, record)
                workers.remove(record)
            if replace_apis:
                for record in api_processes:
                    await asyncio.to_thread(processes.kill, record)
                api_processes = apis()
                for url in urls:
                    await HTTP.ready(processes, observer, url)
            if replace_server:
                await asyncio.to_thread(processes.kill, temporal)
                temporal = server()
                client = await SUPPORT.connect(processes, address)
            workers.extend(worker() for _ in targets)
            async def retry_marker():
                return read_marker(barriers, task_id)
            retried = await SUPPORT.eventually(processes, retry_marker, lambda value: value is not None and
                value["call_id"] == marker["call_id"] and value["pid"] not in old_pids and value["attempt"] > marker["attempt"], timeout=35)
            after = await client.get_workflow_handle(task_id).query("snapshot")
            check_replay(before, after, marker, retried)
            path = directory / f"replay-{len(receipt['faults'])}.json"
            SUPPORT.private_json(path, {"before": before, "after": after}, run_id)
            receipt["faults"].append({"before": marker, "after": retried, "replaced_api": replace_apis,
                                       "replaced_temporal": replace_server, "snapshot_file": str(path)})
            persist()
            return retried

        # Let B independently cross one CAN, then retain a live cross-Run handle
        # while A exercises child replay and additional continuation boundaries.
        marker_b = initial[task_b]
        while not (marker_b["role"] == "main" and marker_b["step"] == 8):
            release(barriers, task_b, marker_b)
            marker_b = await next_marker(task_b, marker_b)
        if marker_b["run_id"] == initial[task_b]["run_id"]:
            raise HarnessFailure("Task B did not independently cross a continuation boundary")

        marker_a = initial[task_a]
        observed_runs = []
        while True:
            if marker_a["run_id"] not in observed_runs:
                observed_runs.append(marker_a["run_id"])
            snapshot = await client.get_workflow_handle(task_a).query("snapshot")
            if snapshot.get("identity", {}).get("first_execution_run_id") != first_run:
                raise HarnessFailure("A running checkpoint lost its first execution identity")
            if marker_a["role"] == "child003" and marker_a["step"] == 1:
                marker_a = await replace_at(marker_a)
            elif marker_a["role"] == "main" and marker_a["step"] in (7, 8, 24):
                marker_a = await replace_at(marker_a, replace_apis=marker_a["step"] in (8, 24), replace_server=marker_a["step"] == 24)
            if marker_a["role"] == "main" and marker_a["step"] in (7, 8, 16, 24, 32):
                path = directory / f"boundary-{marker_a['step']:03d}.json"
                SUPPORT.private_json(path, snapshot, run_id)
                receipt["snapshots"].append({"step": marker_a["step"], "run_id": marker_a["run_id"], "file": str(path),
                    "can_count": snapshot.get("can_count"), "has_summary": bool(snapshot.get("state", {}).get("_summarization_event"))})
                persist()
            release(barriers, task_a, marker_a)
            if marker_a["role"] == "main" and marker_a["step"] == FIXTURE.MAIN_STEPS:
                break
            marker_a = await next_marker(task_a, marker_a)

        result_response = await HTTP.read_until(processes, observer, urls[1] + f"/v1/tasks/{task_a}/result", token,
            lambda response: response.status_code == 200 and response.json().get("ready") is True, timeout=30)
        final = HTTP.decode(result_response, 200)
        if final.get("status") != "succeeded":
            raise HarnessFailure("The repeatedly continued Task did not succeed")
        receipt["final_state_checks"] = check_final(final.get("result", {}), task_a, first_run)
        receipt["completed_result"] = final
        receipt["observed_run_ids"] = observed_runs
        receipt["passed"].append("child Activity replay, pre-CAN Worker kill, post-CAN Worker/API kill, and third-CAN Temporal/Worker/API kill preserve byte-exact native state and model request")

        histories = []
        for index, native_run in enumerate(observed_runs):
            history = await client.get_workflow_handle(task_a, run_id=native_run).fetch_history()
            started = history.events[0].workflow_execution_started_event_attributes
            continued = [event.workflow_execution_continued_as_new_event_attributes for event in history.events
                         if event.HasField("workflow_execution_continued_as_new_event_attributes")]
            next_run = continued[0].new_execution_run_id if continued else None
            rows = {"run_id": native_run, "first_execution_run_id": started.first_execution_run_id,
                    "previous_run_id": started.continued_execution_run_id or None, "next_run_id": next_run,
                    "status": "continued_as_new" if continued else "completed",
                    "start_input_sha256": hashlib.sha256(started.input.SerializeToString(deterministic=True)).hexdigest(),
                    "continued_input_sha256": hashlib.sha256(continued[0].input.SerializeToString(deterministic=True)).hexdigest() if continued else None,
                    "workflow_task_failures": sum(event.HasField("workflow_task_failed_event_attributes") for event in history.events),
                    "event_count": len(history.events)}
            page = HTTP.decode(await observer.get(urls[index % 2] + f"/v1/tasks/{task_a}/events", params={"run_id": native_run, "limit": 2}, headers=headers), 200)
            for field in ("run_id", "first_execution_run_id", "previous_run_id", "next_run_id"):
                if page.get(field) != rows[field]:
                    raise HarnessFailure("HTTP navigation differs from native continuation History at " + field)
            histories.append(rows)
        receipt["continuation_count"] = check_chain(histories, first_run)
        receipt["run_chain"] = histories
        old_page = HTTP.decode(await observer.get(urls[1] + f"/v1/tasks/{task_a}/events", params={"cursor": old_cursor, "limit": 2}, headers=headers), 200)
        if old_page.get("run_id") != first_run or old_page.get("next_run_id") != observed_runs[1]:
            raise HarnessFailure("The original cursor did not remain bound to its retained original Run")
        duplicate = HTTP.decode(await HTTP.detached_submit(processes, urls[0], token, key, message), 202)
        if duplicate.get("task_id") != task_a or duplicate.get("reused") is not True:
            raise HarnessFailure("Completed cross-Run Task was not reused by its retained idempotency key")
        for suffix, method in (("", "GET"), ("/result", "GET"), ("/events", "GET"), ("/cancel", "POST")):
            HTTP.decode(await observer.request(method, urls[1] + f"/v1/tasks/{task_a}" + suffix,
                headers={"Authorization": "Bearer " + foreign_token}), 404)
        receipt["passed"].append("at least three native Continue-As-New links retain Task identity, latest result, owner isolation, same-key reuse, and original Run cursor navigation")

        HTTP.decode(await observer.post(urls[0] + f"/v1/tasks/{task_b}/cancel", headers=headers), 202)
        cancelled = await HTTP.read_until(processes, observer, urls[1] + f"/v1/tasks/{task_b}", token,
            lambda response: response.status_code == 200 and response.json().get("status") == "cancelled", timeout=30)
        receipt["cancelled_task"] = HTTP.decode(cancelled, 200)
        if receipt["cancelled_task"]["run_id"] != marker_b["run_id"]:
            raise HarnessFailure("Cross-Run cancellation did not terminate the current Run")
        audits = [json.loads(path.read_text(encoding="utf-8")) for path in barriers.glob("*.activity.json")]
        summary_audits = [item for item in audits if item["workflow_id"] == task_a and item["summary"]]
        if not summary_audits or len({item["run_id"] for item in summary_audits}) < 2:
            raise HarnessFailure("Real summary model Activities did not execute across multiple Runs")
        receipt["summary_model_activities"] = summary_audits
        receipt["passed"].append("real summary Activities run across segments; private summary/offload state and 37 real parent tools plus two subagents survive; cancelling a second continued Task is isolated")
        persist()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--temporal-binary", type=Path, required=True)
    parser.add_argument("--binary-sha256")
    parser.add_argument("--output-dir", type=Path, default=MODULE.parent / "var/task-can-prototype")
    parser.add_argument("--timeout-seconds", type=float, default=180)
    args = parser.parse_args(argv)
    started, run_id = time.monotonic(), uuid4().hex
    directory = args.output_dir.resolve() / run_id
    directory.mkdir(parents=True, mode=0o700)
    receipt_path = directory / "receipt.json"
    receipt = {"run_id": run_id, "driver_pid": os.getpid(), "status": "running", "started_at": SUPPORT.timestamp(),
        "passed": [], "cleanup_confirmed": False, "production_ha_acceptance": False,
        "rss_budget_mib": 2048, "minimum_memory_free_percent": 15,
        "scope": "production TaskWorkflowV2 with real native tools/subagents/summarization and deterministic model Activities",
        "excluded_claims": ["production cluster HA", "real provider inference", "permanent idempotency", "external-tool exactly-once", "unlimited task duration"]}
    persist = lambda: SUPPORT.private_json(receipt_path, receipt, run_id)
    persist()
    processes = watcher = None
    exit_code = 1
    previous_sigterm = signal.getsignal(signal.SIGTERM)

    def interrupted(_signum, _frame):
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        raise HarnessFailure("The CAN acceptance driver received SIGTERM")

    signal.signal(signal.SIGTERM, interrupted)
    try:
        if not 10 <= args.timeout_seconds <= 180:
            raise HarnessFailure("The overall run budget must be between 10 and 180 seconds")
        binary = args.temporal_binary.resolve()
        if not binary.is_file() or not os.access(binary, os.X_OK):
            raise HarnessFailure("An existing Temporal CLI executable is required; no download was attempted")
        with binary.open("rb") as stream:
            checksum = hashlib.file_digest(stream, "sha256").hexdigest()
        if args.binary_sha256 is not None and args.binary_sha256 != checksum:
            raise HarnessFailure("The Temporal executable checksum did not match")
        receipt.update(temporal_binary=str(binary), temporal_binary_sha256=checksum, supplied_checksum_verified=args.binary_sha256 is not None)
        processes = SUPPORT.Processes(directory, started + args.timeout_seconds)
        watcher = SUPPORT.Watchdog(processes)
        watcher.start()

        async def bounded():
            async with asyncio.timeout(max(0.1, processes.deadline - 5 - time.monotonic())):
                await exercise(processes, binary, directory, run_id, receipt, persist)

        asyncio.run(bounded())
        processes.check()
        receipt["status"] = "passed"
        exit_code = 0
    except ResourceBlocked as error:
        receipt.update(status="resource_blocked", reason=str(error))
        exit_code = 3
    except BaseException as error:
        receipt.update(status="failed", reason=str(error) if isinstance(error, HarnessFailure) else "Acceptance stopped: " + type(error).__name__)
        SUPPORT.private_text(directory / "driver-error.log", traceback.format_exc())
        receipt["failure_detail_file"] = str(directory / "driver-error.log")
    finally:
        if processes is not None:
            receipt["cleanup_confirmed"] = processes.stop_all(force=processes.failure is not None)
            if processes.failure is not None:
                receipt.update(status="resource_blocked" if isinstance(processes.failure, ResourceBlocked) else "failed", reason=str(processes.failure))
                exit_code = 3 if isinstance(processes.failure, ResourceBlocked) else 1
            receipt["processes"] = [{"name": item.name, "pid": item.process.pid, "returncode": item.process.poll(), "log": str(item.log_path)} for item in processes.records]
            if not receipt["cleanup_confirmed"]:
                receipt.update(status="failed", reason="Cleanup of owned CAN acceptance processes could not be confirmed")
                exit_code = 1
        else:
            receipt["cleanup_confirmed"] = True
        if watcher is not None:
            watcher.stop()
            receipt.update(peak_owned_tree_rss_mib=round(watcher.peak_kib / 1024, 1),
                           minimum_observed_memory_free_percent=watcher.minimum_free, resource_samples=watcher.samples)
        receipt.update(finished_at=SUPPORT.timestamp(), seconds=round(time.monotonic() - started, 2))
        persist()
        signal.signal(signal.SIGTERM, previous_sigterm)
        print(json.dumps({"run_id": run_id, "status": receipt["status"], "receipt": str(receipt_path),
                          "seconds": receipt["seconds"], "passed": receipt["passed"]}, indent=2))
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
