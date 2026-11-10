"""P1.5/P1.6 controlled provider faults and HTTP uncertainty acceptance."""

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

from mini_loop_deepagents.task_server.protocol import task_id_for
from tooling.paths import MODULE
from tooling.acceptance.support import process as SUPPORT
from tooling.acceptance.support.http import decode, free_ports, read_marker, read_until, ready, release
from tooling.acceptance.support.process import HarnessFailure, ResourceBlocked
from tooling.acceptance.support.response_loss import post_and_drop_response


EXPECTED_INITIAL_ATTEMPTS = {
    "rate-limit": [(1, "failed"), (2, "failed"), (3, "succeeded")],
    "timeout": [(1, "failed"), (2, "failed"), (3, "failed")],
    "disconnect": [(1, "failed"), (2, "succeeded")],
}


def summarize_fault_attempts(directory: Path, task_ids: dict[str, str]) -> dict[str, dict]:
    rows = [json.loads(path.read_text()) for path in sorted(directory.glob("*.attempt.json"))]
    summary = {}
    for mode, task_id in task_ids.items():
        selected = [row for row in rows if row["workflow_id"] == task_id and row["phase"] == "initial"]
        selected.sort(key=lambda row: row["attempt"])
        observed = [(row["attempt"], row["outcome"]) for row in selected]
        if observed != EXPECTED_INITIAL_ATTEMPTS[mode]:
            raise HarnessFailure(f"The controlled {mode} Activity attempts did not match their finite budget")
        if len({(row["run_id"], row["activity_id"]) for row in selected}) != 1:
            raise HarnessFailure(f"The controlled {mode} retries changed native Activity identity")
        elapsed_ms = round((selected[-1]["observed_monotonic_ns"]
                            - selected[0]["observed_monotonic_ns"]) / 1_000_000, 1)
        summary[mode] = {
            "attempts": [{"attempt": row["attempt"], "outcome": row["outcome"], "pid": row["pid"]}
                         for row in selected],
            "attempt_count": len(selected),
            "elapsed_first_to_last_ms": elapsed_ms,
            "single_native_activity": True,
            "fixture_provider_calls_per_activity_attempt": 1,
        }
    return summary


async def exercise(processes, binary: Path, directory: Path, run_id: str, receipt: dict, persist) -> None:
    temporal_port, first_api_port, second_api_port = free_ports(3)
    address, queue = f"127.0.0.1:{temporal_port}", "protocol-fault-" + run_id
    barriers = directory / "fault-records"
    barriers.mkdir(mode=0o700)
    urls = [f"http://127.0.0.1:{port}" for port in (first_api_port, second_api_port)]
    server = processes.spawn("temporal", [
        binary, "--disable-config-env", "--disable-config-file", "--log-level", "warn",
        "server", "start-dev", "--ip", "127.0.0.1", "--port", str(temporal_port),
        "--headless", "--db-filename", str(directory / "temporal.sqlite"),
    ], extra_environment={"GOMAXPROCS": "2", "GOMEMLIMIT": "512MiB"})
    temporal_client = await SUPPORT.connect(processes, address)
    worker = processes.spawn("worker", [
        sys.executable, "-m", "tooling.acceptance.fixtures.task_worker_fixture",
        "--address", address, "--task-queue", queue, "--identity", "protocol-fault-worker",
        "--barrier-directory", str(barriers),
    ])
    alice, bob = secrets.token_urlsafe(32), secrets.token_urlsafe(32)
    environment = {
        "MINILOOP_DEEPAGENTS_TOKENS": json.dumps({alice: "alice", bob: "bob"}),
        "MINILOOP_TASK_SERVER_CURSOR_SECRET": secrets.token_urlsafe(32),
    }
    apis = []
    for name, port in (("api-a", first_api_port), ("api-b", second_api_port)):
        apis.append(processes.spawn(name, [
            sys.executable, "-m", "mini_loop_deepagents.task_server.api",
            "--address", f"127.0.0.1:{port}", "--temporal-address", address,
            "--namespace", "default", "--task-queue", queue,
        ], extra_environment=environment))
    receipt.update(task_queue=queue, authority="Temporal Workflow History only",
                   process_pids={"temporal": server.process.pid, "worker": worker.process.pid,
                                 "apis": [record.process.pid for record in apis]})
    persist()

    async with httpx.AsyncClient(timeout=8, follow_redirects=False, trust_env=False) as client:
        for url in urls:
            await ready(processes, client, url)

        async def submit(url: str, key: str, message: str) -> dict:
            response = await client.post(url + "/v1/tasks", headers={
                "Authorization": "Bearer " + alice, "Idempotency-Key": key,
            }, json={"message": message})
            return decode(response, 202)

        async def terminal(task_id: str, allowed: set[str], timeout: float = 35) -> dict:
            response = await read_until(
                processes, client, urls[1] + "/v1/tasks/" + task_id, alice,
                lambda value: value.status_code == 200 and value.json().get("status") in allowed,
                timeout=timeout,
            )
            return decode(response, 200)

        fault_tasks = {}
        terminal_states = {}
        for mode, expected in (("rate-limit", "succeeded"), ("timeout", "failed"),
                               ("disconnect", "succeeded")):
            admitted = await submit(urls[0], f"{mode}-{run_id}", f"fault:{mode}:{run_id}")
            fault_tasks[mode] = admitted["task_id"]
            state = await terminal(admitted["task_id"], {expected})
            terminal_states[mode] = state["status"]
        attempts = summarize_fault_attempts(barriers, fault_tasks)
        receipt["controlled_provider_faults"] = {
            "task_terminal_states": terminal_states,
            "activity_attempts": attempts,
            "sdk_nested_retry": "disabled in production worker (max_retries=0)",
            "duplicate_billing_boundary": "one fixture provider call per recorded Activity attempt",
            "real_provider_claim": False,
        }
        receipt["passed"].append(
            "controlled 429/timeout/disconnect faults terminate at exact Temporal attempt budgets; "
            "429 and disconnect recover, timeout fails, and no inner SDK retry is present")
        persist()

        lost_key = "response-lost-" + run_id
        lost_message = "response loss reconciliation " + run_id
        expected_task = task_id_for("alice", lost_key)
        lost = await post_and_drop_response(
            "127.0.0.1", first_api_port, "/v1/tasks",
            headers={"Authorization": "Bearer " + alice, "Idempotency-Key": lost_key},
            json_body={"message": lost_message},
        )
        if lost["upstream_status"] != 202:
            raise HarnessFailure("The dropped response did not follow a successful upstream Start")
        running = decode(await read_until(
            processes, client, urls[1] + "/v1/tasks/" + expected_task, alice,
            lambda value: value.status_code == 200,
        ), 200)
        retry = await submit(urls[1], lost_key, lost_message)
        if retry.get("task_id") != expected_task or retry.get("reused") is not True:
            raise HarnessFailure("Retry after the lost Start response did not reconcile the retained Task")
        count = (await temporal_client.count_workflows(query=f'WorkflowId = "{expected_task}"')).count
        if count != 1:
            raise HarnessFailure("Retry after the lost Start response created another native Workflow")
        conflict = await client.post(urls[0] + "/v1/tasks", headers={
            "Authorization": "Bearer " + alice, "Idempotency-Key": lost_key,
        }, json={"message": lost_message + " changed"})
        decode(conflict, 409)
        unauthorized = {}
        for suffix, method in (("", "GET"), ("/result", "GET"), ("/events", "GET"), ("/cancel", "POST")):
            response = await client.request(method, urls[1] + "/v1/tasks/" + expected_task + suffix,
                                            headers={"Authorization": "Bearer " + bob})
            decode(response, 404)
            unauthorized[method + " " + (suffix or "/")] = response.status_code
        release(barriers, expected_task)
        await terminal(expected_task, {"succeeded"})
        receipt["lost_start_response"] = {
            **lost,
            "expected_task_id_matched": running.get("task_id") == expected_task,
            "retry_reused": retry["reused"],
            "native_temporal_workflow_count": count,
            "conflicting_content_status": conflict.status_code,
            "foreign_owner_statuses": unauthorized,
        }
        receipt["passed"].append(
            "Start completed with upstream HTTP 202 while its client response was dropped; deterministic "
            "Task ID query and same-key retry reconcile one Workflow, conflict is 409, foreign owner is 404")
        persist()

        race = await submit(urls[0], "cancel-race-" + run_id, "fault:race:" + run_id)
        race_task = race["task_id"]
        await SUPPORT.eventually(processes, lambda: asyncio.sleep(0, result=read_marker(barriers, race_task)),
                                 lambda value: value is not None, timeout=30)

        async def release_gate() -> None:
            await asyncio.sleep(0)
            release(barriers, race_task)

        cancel_response, _ = await asyncio.gather(
            client.post(urls[1] + "/v1/tasks/" + race_task + "/cancel",
                        headers={"Authorization": "Bearer " + alice}),
            release_gate(),
        )
        cancel_value = decode(cancel_response, 202)
        final = await terminal(race_task, {"succeeded", "cancelled"})
        if cancel_value.get("cancel_requested") is False and cancel_value.get("status") not in {"succeeded", "cancelled"}:
            raise HarnessFailure("The cancel/complete race returned an unexplained cancellation no-op")
        race_count = (await temporal_client.count_workflows(query=f'WorkflowId = "{race_task}"')).count
        replicas = [decode(await client.get(url + "/v1/tasks/" + race_task,
                                            headers={"Authorization": "Bearer " + alice}), 200)
                    for url in urls]
        if race_count != 1 or {value.get("status") for value in replicas} != {final["status"]}:
            raise HarnessFailure("The cancel/complete race did not converge on one stable native Task")
        receipt["cancel_complete_race"] = {
            "cancel_acknowledgement": {
                "status": cancel_value.get("status"),
                "cancel_requested": cancel_value.get("cancel_requested"),
            },
            "terminal_status": final["status"],
            "native_temporal_workflow_count": race_count,
            "replica_terminal_statuses": [value["status"] for value in replicas],
        }
        receipt["passed"].append(
            "barrier release and cancel race converges to one explainable succeeded/cancelled terminal state "
            "on both replicas without a second Workflow execution")
        persist()


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--temporal-binary", type=Path, required=True)
    parser.add_argument("--binary-sha256")
    parser.add_argument("--output-dir", type=Path, default=MODULE.parent / "var/task-protocol-failures")
    parser.add_argument("--timeout-seconds", type=float, default=180)
    args = parser.parse_args(argv)
    started, run_id = time.monotonic(), uuid4().hex
    directory = args.output_dir.resolve() / run_id
    directory.mkdir(parents=True, mode=0o700)
    receipt_path = directory / "receipt.json"
    receipt = {
        "run_id": run_id, "driver_pid": os.getpid(), "status": "running",
        "started_at": SUPPORT.timestamp(), "passed": [], "cleanup_confirmed": False,
        "scope": "private local Temporal, controlled provider fixture, and real HTTP Task APIs",
        "production_provider_acceptance": False,
        "excluded_claims": ["real provider behavior", "production cluster HA", "permanent idempotency",
                            "external-tool exactly-once", "provider billing totals"],
        "rss_budget_mib": 2048, "minimum_memory_free_percent": 15,
    }
    persist = lambda: SUPPORT.private_json(receipt_path, receipt, run_id)
    persist()
    processes = watcher = None
    exit_code = 1
    previous_sigterm = signal.getsignal(signal.SIGTERM)

    def interrupted(_signum, _frame):
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        raise HarnessFailure("The protocol failure acceptance driver received SIGTERM")

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
        receipt.update(temporal_binary=str(binary), temporal_binary_sha256=checksum,
                       supplied_checksum_verified=args.binary_sha256 is not None)
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
        receipt.update(status="failed", reason=str(error) if isinstance(error, HarnessFailure)
                       else "Acceptance stopped: " + type(error).__name__)
        SUPPORT.private_text(directory / "driver-error.log", traceback.format_exc())
        receipt["failure_detail_file"] = str(directory / "driver-error.log")
    finally:
        if processes is not None:
            receipt["cleanup_confirmed"] = processes.stop_all(force=processes.failure is not None)
            receipt["processes"] = [{"name": item.name, "pid": item.process.pid,
                                     "returncode": item.process.poll(), "log": str(item.log_path)}
                                    for item in processes.records]
            if processes.failure is not None:
                receipt.update(status="resource_blocked" if isinstance(processes.failure, ResourceBlocked)
                               else "failed", reason=str(processes.failure))
                exit_code = 3 if isinstance(processes.failure, ResourceBlocked) else 1
            if not receipt["cleanup_confirmed"]:
                receipt.update(status="failed", reason="Cleanup of owned acceptance processes was not confirmed")
                exit_code = 1
        else:
            receipt["cleanup_confirmed"] = True
        if watcher is not None:
            watcher.stop()
            receipt.update(peak_owned_tree_rss_mib=round(watcher.peak_kib / 1024, 1),
                           minimum_observed_memory_free_percent=watcher.minimum_free,
                           resource_samples=watcher.samples)
        receipt.update(finished_at=SUPPORT.timestamp(), seconds=round(time.monotonic() - started, 2))
        persist()
        signal.signal(signal.SIGTERM, previous_sigterm)
        print(json.dumps({"run_id": run_id, "status": receipt["status"], "receipt": str(receipt_path),
                          "seconds": receipt["seconds"], "passed": receipt["passed"]}, indent=2))
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
