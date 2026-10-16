"""Resource-bounded multi-owner Task queue and rolling Worker acceptance.

Twelve real V2 Tasks, three single-Activity Workers and two production APIs test
admission backpressure, pending work, native continuation and three shutdown
paths. Deterministic model Activities keep this a mechanism check, not a
production throughput/latency or real-provider capacity measurement."""
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
from tooling.acceptance.support import http as HTTP
from tooling.acceptance.support.rollout import (
    read_marker,
    touch_gate,
    check_takeover,
    concurrency_evidence,
    check_completed,
    wait_exit,
    admission_backpressure,
)


async def exercise(processes, binary, directory, run_id, receipt, persist):
    from temporalio.api.enums.v1 import PendingActivityState
    from mini_loop_deepagents.task_server.service import task_id_for
    ports = HTTP.free_ports(3)
    address, queue = f"127.0.0.1:{ports[0]}", "rollout-" + run_id
    barriers, database = directory / "barriers", directory / "temporal.sqlite"
    barriers.mkdir(mode=0o700)
    urls = [f"http://127.0.0.1:{port}" for port in ports[1:]]
    tokens = {owner: secrets.token_urlsafe(32) for owner in ("alice", "bob", "carol")}
    environment = {"MINILOOP_DEEPAGENTS_TOKENS": json.dumps({token: owner for owner, token in tokens.items()}),
                   "MINILOOP_TASK_SERVER_CURSOR_SECRET": secrets.token_urlsafe(32)}
    processes.spawn("temporal", [binary, "--disable-config-env", "--disable-config-file", "--log-level", "warn",
        "server", "start-dev", "--ip", "127.0.0.1", "--port", str(ports[0]), "--headless", "--db-filename", database],
        extra_environment={"GOMAXPROCS": "2", "GOMEMLIMIT": "512MiB"})
    client = await SUPPORT.connect(processes, address)
    serial = 0
    def worker():
        nonlocal serial
        serial += 1
        return processes.spawn(f"worker-{serial}", [sys.executable, "-m", "tooling.acceptance.fixtures.task_rollout_worker_fixture",
            "--address", address, "--task-queue", queue, "--identity", f"rollout-{serial}",
            "--barrier-directory", barriers, "--shutdown-grace-seconds", "1", "--shutdown-timeout-seconds", "5"])
    workers = [worker() for _ in range(3)]
    initial_workers = list(workers)
    for index, port in enumerate(ports[1:]):
        processes.spawn(f"api-{index}", [sys.executable, "-m", "mini_loop_deepagents.task_server.api", "--address",
            f"127.0.0.1:{port}", "--temporal-address", address, "--task-queue", queue, "--max-inflight", "2"], extra_environment=environment)
    records = []
    for owner in tokens:
        for index in range(4):
            key = f"same-owner-key-{index}-{run_id}"
            records.append({"owner": owner, "key": key, "message": f"[rollout] {owner} {index} {run_id}", "task_id": task_id_for(owner, key)})
    receipt.update(task_queue=queue, accepted_task_count=0, steady_worker_count=3, activity_slots_per_worker=1,
                   api_replica_count=2, owner_count=3, producer_concurrency_limit=2, worker_replacements=[])
    async with httpx.AsyncClient(timeout=7, trust_env=False, follow_redirects=False) as observer:
        for url in urls:
            await HTTP.ready(processes, observer, url)
        receipt["admission_backpressure"] = await admission_backpressure(processes, observer, urls[0], tokens["alice"], records[0], client)
        persist()
        semaphore = asyncio.Semaphore(2)
        async def submit(record, replica):
            async with semaphore:
                return HTTP.decode(await HTTP.detached_submit(processes, urls[replica], tokens[record["owner"]], record["key"], record["message"]), 202)
        for record in records:
            values = await asyncio.gather(submit(record, 0), submit(record, 1))
            if (any(value.get("task_id") != record["task_id"] or value.get("version") != "task-v2" for value in values)
                    or sorted(value.get("reused") for value in values) != [False, True]):
                raise HarnessFailure("Duplicate submissions did not converge on exactly one new owner-scoped Task")
            record["first_execution_run_id"] = values[0]["first_execution_run_id"]
        if len({record["task_id"] for record in records}) != 12:
            raise HarnessFailure("Same keys from different owners collapsed independent Tasks")
        receipt["accepted_task_count"] = len(records)
        receipt["admission_backpressure"]["same_key_retry_accepted"] = True
        receipt["passed"].append("two authenticated slow-body requests fill the real API cap; next submission is 429 with native NOT_FOUND, then the same key joins twelve accepted owner-scoped Tasks through duplicate requests across two APIs")

        async def queue_snapshot():
            rows = []
            for record in records:
                description = await client.get_workflow_handle(record["task_id"]).describe()
                rows.append({"task_id": record["task_id"], "run_id": description.run_id,
                    "pending_states": [PendingActivityState.Name(item.state) for item in description.raw_description.pending_activities],
                    "marker": read_marker(barriers, record["task_id"])})
            return rows
        def saturated(rows):
            return sum(row["marker"] is not None for row in rows) == 3 and sum(
                "PENDING_ACTIVITY_STATE_SCHEDULED" in row["pending_states"] for row in rows) == 9
        queued = await SUPPORT.eventually(processes, queue_snapshot, saturated, timeout=35)
        initial_markers = [row["marker"] for row in queued if row["marker"] is not None]
        if {row["pid"] for row in initial_markers} != {row.process.pid for row in workers}:
            raise HarnessFailure("The three active model barriers did not occupy three independent Worker processes")
        receipt["initial_queue"] = queued
        receipt["passed"].append("all twelve accepted Tasks persist; three real model Activities occupy the three Worker slots while nine native pending Activities remain scheduled")
        persist()

        async def wait_takeover(original, protected_pids):
            deadline = time.monotonic() + 35
            while time.monotonic() < deadline:
                processes.check()
                marker = read_marker(barriers, original["workflow_id"])
                if marker is not None and marker["pid"] != original["pid"] and marker["attempt"] > original["attempt"]:
                    check_takeover(original, marker)
                    return marker
                # Native queue ordering is unspecified. Release unrelated newer
                # work so a queued retry cannot starve behind another test gate.
                for record in records:
                    candidate = read_marker(barriers, record["task_id"])
                    if candidate is not None and candidate["pid"] not in protected_pids and candidate["workflow_id"] != original["workflow_id"]:
                        touch_gate(barriers, record["task_id"], "release")
                await asyncio.sleep(0.1)
            raise HarnessFailure("A shutdown-interrupted Activity did not transfer to another Worker within its retry budget")

        process_end_times = {}
        for index, original_worker in enumerate(initial_workers):
            original = next(row for row in initial_markers if row["pid"] == original_worker.process.pid)
            kind = ("graceful_completion", "cooperative_cancellation", "hard_deadline")[index]
            if kind == "hard_deadline":
                touch_gate(barriers, original["workflow_id"], "ignore-cancel")
            signalled_at = time.monotonic_ns()
            processes.signal(original_worker, signal.SIGTERM)
            if kind == "graceful_completion":
                async def draining():
                    return original_worker.log_path.read_text(encoding="utf-8", errors="replace")
                await SUPPORT.eventually(processes, draining, lambda text: "TASK_WORKER_DRAINING" in text, timeout=3)
                if original_worker.process.poll() is not None:
                    raise HarnessFailure("SIGTERM terminated a Worker before its grace-bound Activity could finish")
                touch_gate(barriers, original["workflow_id"], "release")
            exit_record = await wait_exit(processes, original_worker, 70 if kind == "hard_deadline" else 0)
            process_end_times[original_worker.process.pid] = exit_record["exited_monotonic_ns"]
            workers.remove(original_worker)
            replacement = worker()
            workers.append(replacement)
            evidence = {"kind": kind, "signal": "SIGTERM", "signalled_monotonic_ns": signalled_at,
                "before": original, "exit": exit_record, "replacement_pid": replacement.process.pid,
                "shutdown_grace_seconds": 1, "shutdown_timeout_seconds": 5}
            if kind != "graceful_completion":
                protected = {record.process.pid for record in initial_workers[index + 1:]}
                evidence["after"] = await wait_takeover(original, protected)
                touch_gate(barriers, original["workflow_id"], "release")
            if kind == "hard_deadline":
                ignored = [json.loads(path.read_text()) for path in barriers.glob("*.ignored-cancellation.json")]
                if not any(row["pid"] == original_worker.process.pid and row["workflow_id"] == original["workflow_id"] for row in ignored):
                    raise HarnessFailure("Hard-exit acceptance never observed a model Activity ignoring cancellation")
                elapsed = (exit_record["exited_monotonic_ns"] - signalled_at) / 1e9
                if not 4.5 <= elapsed <= 8:
                    raise HarnessFailure("The configured Worker hard deadline did not bound a stuck model Activity")
                evidence["hard_exit_seconds"] = round(elapsed, 3)
            receipt["worker_replacements"].append(evidence)
            persist()
        receipt["passed"].append("rolling SIGTERM replacements cover grace-bound completion (exit 0), cooperative Activity cancellation (exit 0), and cancellation-ignoring Activity hard deadline (exit 70); retries keep native Activity/Run/request identity")

        completed = {}
        deadline = time.monotonic() + 45
        while len(completed) < len(records) and time.monotonic() < deadline:
            processes.check()
            for record in records:
                if record["task_id"] in completed:
                    continue
                marker = read_marker(barriers, record["task_id"])
                if marker is not None:
                    touch_gate(barriers, record["task_id"], "release")
                response = await observer.get(urls[0] + f"/v1/tasks/{record['task_id']}/result",
                    headers={"Authorization": "Bearer " + tokens[record["owner"]]})
                if response.status_code == 200:
                    value = HTTP.decode(response, 200)
                    completed[record["task_id"]] = check_completed(value, record)
                    SUPPORT.private_json(directory / f"{record['task_id']}.result.json", value, run_id)
                elif response.status_code != 202:
                    raise HarnessFailure("An accepted Task result handle became unavailable during rolling replacement")
            await asyncio.sleep(0.1)
        if len(completed) != len(records):
            raise HarnessFailure("Not all accepted Tasks reached a verified successful terminal result")

        native_chains = []
        for record in records:
            history = await client.get_workflow_handle(record["task_id"], run_id=record["first_execution_run_id"]).fetch_history()
            can_events = [event.workflow_execution_continued_as_new_event_attributes for event in history.events
                          if event.HasField("workflow_execution_continued_as_new_event_attributes")]
            if len(can_events) != 1 or can_events[0].new_execution_run_id != completed[record["task_id"]]["final_run_id"]:
                raise HarnessFailure("A Task result claimed continuation without its native original Run transition")
            final_history = await client.get_workflow_handle(record["task_id"], run_id=can_events[0].new_execution_run_id).fetch_history()
            failures = sum(event.HasField("workflow_task_failed_event_attributes") for item in (history, final_history) for event in item.events)
            if failures:
                raise HarnessFailure("A rollout Task recorded WorkflowTaskFailed during continuation or worker replacement")
            native_chains.append({"task_id": record["task_id"], "first_execution_run_id": record["first_execution_run_id"],
                "next_run_id": can_events[0].new_execution_run_id, "workflow_task_failures": failures})
        for record in records:
            duplicate = await submit(record, 1)
            if duplicate.get("reused") is not True or duplicate.get("task_id") != record["task_id"]:
                raise HarnessFailure("A completed multi-owner Task lost retained-key idempotency")
        for owner_index, owner in enumerate(tokens):
            record = next(row for row in records if row["owner"] == owner)
            wrong_owner = list(tokens)[(owner_index + 1) % len(tokens)]
            for suffix, method in (("", "GET"), ("/result", "GET"), ("/events", "GET"), ("/cancel", "POST")):
                HTTP.decode(await observer.request(method, urls[1] + f"/v1/tasks/{record['task_id']}" + suffix,
                    headers={"Authorization": "Bearer " + tokens[wrong_owner]}), 404)
        usage = concurrency_evidence(barriers, process_end_times)
        replacement_pids = {item["replacement_pid"] for item in receipt["worker_replacements"]}
        original_tasks = {row["workflow_id"] for row in initial_markers}
        replacement_queued_tasks = {row["workflow_id"] for row in usage["intervals"] if row["pid"] in replacement_pids and row["workflow_id"] not in original_tasks}
        if not replacement_queued_tasks or usage["maximum_active_model_activities"] != 3:
            raise HarnessFailure("Replacement Workers did not drain queued accepted work under the configured slot limit")
        receipt.update(completed_tasks=list(completed.values()), native_run_chains=native_chains,
            activity_concurrency=usage, replacement_queued_task_ids=sorted(replacement_queued_tasks),
            same_key_distinct_owner_task_ids={str(index):[row["task_id"] for row in records if row["key"] == f"same-owner-key-{index}-{run_id}"] for index in range(4)})
        receipt["passed"].append("all twelve accepted Tasks finish ten native tools and one CAN each; replacement Workers drain previously queued tasks, maximum observed model concurrency is three, duplicates retain identity, and cross-owner read/result/events/cancel remain 404")
        persist()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--temporal-binary", type=Path, required=True)
    parser.add_argument("--binary-sha256")
    parser.add_argument("--output-dir", type=Path, default=MODULE.parent / "var/task-rollout-prototype")
    parser.add_argument("--timeout-seconds", type=float, default=180)
    args = parser.parse_args(argv)
    started, run_id = time.monotonic(), uuid4().hex
    directory = args.output_dir.resolve() / run_id
    directory.mkdir(parents=True, mode=0o700)
    receipt_path = directory / "receipt.json"
    receipt = {"run_id": run_id, "driver_pid": os.getpid(), "status": "running", "started_at": SUPPORT.timestamp(),
        "passed": [], "cleanup_confirmed": False, "rss_budget_mib": 2048, "minimum_memory_free_percent": 15,
        "production_ha_acceptance": False, "production_capacity_acceptance": False,
        "scope": "twelve real V2 Tasks, three owners, three single-Activity production Workers and two production HTTP APIs",
        "excluded_claims": ["production throughput or latency", "production cluster HA", "real provider capacity", "external-effect exactly-once", "global tenant quotas"]}
    persist = lambda: SUPPORT.private_json(receipt_path, receipt, run_id)
    persist()
    processes = watcher = None
    exit_code = 1
    previous_sigterm = signal.getsignal(signal.SIGTERM)
    def interrupted(_signum, _frame):
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        raise HarnessFailure("The rollout acceptance driver received SIGTERM")
    signal.signal(signal.SIGTERM, interrupted)
    try:
        if not 10 <= args.timeout_seconds <= 180:
            raise HarnessFailure("The overall run budget must be between 10 and 180 seconds")
        binary = args.temporal_binary.resolve()
        if not binary.is_file() or not os.access(binary, os.X_OK):
            raise HarnessFailure("An existing Temporal executable is required; no download was attempted")
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
            receipt["processes"] = [{"name": row.name, "pid": row.process.pid, "returncode": row.process.poll(), "log": str(row.log_path)} for row in processes.records]
            if not receipt["cleanup_confirmed"]:
                receipt.update(status="failed", reason="Owned rollout process cleanup could not be confirmed")
                exit_code = 1
        else:
            receipt["cleanup_confirmed"] = True
        if watcher is not None:
            watcher.stop()
            receipt.update(peak_owned_tree_rss_mib=round(watcher.peak_kib / 1024, 1), minimum_observed_memory_free_percent=watcher.minimum_free, resource_samples=watcher.samples)
        receipt.update(finished_at=SUPPORT.timestamp(), seconds=round(time.monotonic() - started, 2))
        persist()
        signal.signal(signal.SIGTERM, previous_sigterm)
        print(json.dumps({"run_id": run_id, "status": receipt["status"], "receipt": str(receipt_path), "seconds": receipt["seconds"], "passed": receipt["passed"]}, indent=2))
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
