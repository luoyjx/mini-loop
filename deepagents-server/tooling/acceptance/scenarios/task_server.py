"""Real HTTP Task API fault acceptance on a private local Temporal service.

Two API replicas share only Temporal Workflow History as durable Task authority.
The private test Worker drives real Deep Agents with a deterministic model; no
provider keys, business SQLite ledger, or automatic server download are used."""
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
from tooling.acceptance.support.http import (
    free_ports,
    read_marker,
    release,
    decode,
    check_page,
    check_preserved_state,
    check_snapshot_contract,
    ready,
    read_until,
    detached_submit,
)


async def exercise(processes, binary, directory, run_id, receipt, persist, *, restart_temporal):
    temporal_port, first_api_port, second_api_port = free_ports(3)
    address, queue = f"127.0.0.1:{temporal_port}", "http-fault-" + run_id
    temporal_database = directory / "temporal.sqlite"
    barriers = directory / "barriers"
    barriers.mkdir(mode=0o700)
    urls = [f"http://127.0.0.1:{port}" for port in (first_api_port, second_api_port)]
    server_arguments = [binary, "--disable-config-env", "--disable-config-file", "--log-level", "warn",
                        "server", "start-dev", "--ip", "127.0.0.1", "--port", str(temporal_port),
                        "--headless", "--db-filename", str(temporal_database)]

    def server(name):
        return processes.spawn(name, server_arguments,
                               extra_environment={"GOMAXPROCS": "2", "GOMEMLIMIT": "512MiB"})

    def worker(name):
        return processes.spawn(name, [sys.executable, "-m", "tooling.acceptance.fixtures.task_worker_fixture",
                                      "--address", address, "--task-queue", queue, "--identity", name,
                                      "--barrier-directory", str(barriers)])

    alice, bob = secrets.token_urlsafe(32), secrets.token_urlsafe(32)
    environment = {"MINILOOP_DEEPAGENTS_TOKENS": json.dumps({alice: "alice", bob: "bob"}),
                   "MINILOOP_TASK_SERVER_CURSOR_SECRET": secrets.token_urlsafe(32)}

    def api(name, port):
        return processes.spawn(name, [sys.executable, "-m", "mini_loop_deepagents.task_server.api",
                                    "--address", f"127.0.0.1:{port}", "--temporal-address", address,
                                    "--namespace", "default", "--task-queue", queue], extra_environment=environment)

    temporal = server("temporal-1")
    temporal_client = await SUPPORT.connect(processes, address)
    workers = [worker("worker-a"), worker("worker-b")]
    apis = [api("api-a", first_api_port), api("api-b", second_api_port)]
    receipt.update(temporal_database=str(temporal_database), authority="Temporal Workflow History only",
                   idempotency_scope="retained Temporal Workflow history; no permanent or cross-retention guarantee",
                   task_queue=queue, api_urls=urls)
    persist()

    async with httpx.AsyncClient(timeout=5, follow_redirects=False, trust_env=False) as observer:
        for url in urls:
            await ready(processes, observer, url)
        key_a, key_b = "key-a-" + run_id, "key-b-" + run_id
        message_a, message_b = "HTTP fault task A " + run_id, "HTTP fault task B " + run_id
        responses = await asyncio.gather(*(detached_submit(processes, url, alice, key_a, message_a) for url in urls))
        admitted = [decode(response, 202) for response in responses]
        task_a = admitted[0].get("task_id")
        if not isinstance(task_a, str) or not task_a.startswith("task-") or admitted[1].get("task_id") != task_a:
            raise HarnessFailure("Two API replicas did not converge on the same accepted Task ID")
        if sorted(value.get("reused") for value in admitted) != [False, True]:
            raise HarnessFailure("The competing submissions did not distinguish one create and one reuse")
        if any(value.get("version") != "task-v2" for value in admitted):
            raise HarnessFailure("The production API did not select the current Workflow for new Tasks")

        async def count():
            return (await temporal_client.count_workflows(query=f'WorkflowId = "{task_a}"')).count
        count_value = await SUPPORT.eventually(processes, count, lambda value: value == 1, timeout=10)
        receipt.update(task_a=task_a, cross_replica_race={"response_task_ids": [item["task_id"] for item in admitted],
                                                       "reused": [item["reused"] for item in admitted],
                                                       "native_temporal_workflow_count": count_value})
        receipt["passed"].append("two independent HTTP replicas race one owner/idempotency key: HTTP 202 responses identify exactly one native Temporal Workflow")
        persist()

        conflict = await detached_submit(processes, urls[1], alice, key_a, message_a + " changed")
        decode(conflict, 409)
        for suffix, method in (("", "GET"), ("/result", "GET"), ("/events", "GET"), ("/cancel", "POST")):
            response = await observer.request(method, urls[1] + "/v1/tasks/" + task_a + suffix,
                                              headers={"Authorization": "Bearer " + bob})
            decode(response, 404)
        receipt["passed"].append("changed payload under the same retained key returns 409; foreign owner read/result/events/cancel return 404")

        task_b = decode(await detached_submit(processes, urls[1], alice, key_b, message_b), 202)["task_id"]
        receipt["task_b"] = task_b
        if task_b == task_a:
            raise HarnessFailure("Independent messages/keys were collapsed into one Task")

        async def markers():
            return [read_marker(barriers, task_id) for task_id in (task_a, task_b)]
        blocked = await SUPPORT.eventually(processes, markers, lambda value: all(value), timeout=30)
        if {item["pid"] for item in blocked} != {record.process.pid for record in workers}:
            raise HarnessFailure("Two accepted Tasks did not reach the real Deep Agents barrier on two independent Workers")
        run_ids = {task_id: (await temporal_client.get_workflow_handle(task_id).describe()).run_id
                   for task_id in (task_a, task_b)}
        initial_snapshots = {task_id: await temporal_client.get_workflow_handle(task_id).query("snapshot")
                             for task_id in (task_a, task_b)}
        for task_id, snapshot in initial_snapshots.items():
            check_snapshot_contract(snapshot, task_id, run_ids[task_id], "task-v2", "running")
            if (not snapshot.get("messages")
                    or not snapshot.get("files") or not snapshot.get("todos")):
                raise HarnessFailure("An accepted blocked Task lacks its native running state")
            SUPPORT.private_json(directory / (task_id + "-initial-snapshot.json"), snapshot, run_id)
        receipt["initial_snapshot_files"] = {task_id: str(directory / (task_id + "-initial-snapshot.json"))
                                             for task_id in (task_a, task_b)}
        receipt["blocked_worker_markers"] = blocked
        receipt["accepted_run_ids"] = run_ids
        receipt["passed"].append("detached producers exit after HTTP 202; two different Tasks execute real Deep Agents planning/file/model steps concurrently on independent Worker PIDs")

        page_response = await observer.get(urls[0] + "/v1/tasks/" + task_a + "/events", params={"limit": 2},
                                           headers={"Authorization": "Bearer " + alice})
        first_page = decode(page_response, 200)
        event_ids = check_page(first_page, task_a, run_ids[task_a])
        cursor = first_page["next_cursor"]
        if len(event_ids) != 2 or not cursor:
            raise HarnessFailure("The pre-restart history did not produce a bounded resumable event page")
        receipt["event_pagination"] = {"first_page_ids": event_ids[:], "cursor_sha256": hashlib.sha256(cursor.encode()).hexdigest()}
        persist()

        old_apis = [record.process.pid for record in apis]
        for record in apis:
            await asyncio.to_thread(processes.kill, record)
        apis = [api("api-c", first_api_port), api("api-d", second_api_port)]
        for url in urls:
            await ready(processes, observer, url)
        for task_id in (task_a, task_b):
            response = await read_until(processes, observer, urls[0] + "/v1/tasks/" + task_id, alice,
                                        lambda value: value.status_code == 200)
            state = decode(response, 200)
            if state.get("task_id") != task_id or state.get("run_id") != run_ids[task_id] or state.get("status") != "running":
                raise HarnessFailure("Full API replacement changed or lost an accepted running Task")
        pending = decode(await observer.get(urls[1] + "/v1/tasks/" + task_b + "/result",
                                            headers={"Authorization": "Bearer " + alice}), 202)
        if pending.get("ready") is not False:
            raise HarnessFailure("A blocked Task result did not remain asynchronous after API replacement")
        receipt["api_restart"] = {"old_pids": old_apis, "new_pids": [record.process.pid for record in apis]}
        receipt["passed"].append("all HTTP API processes SIGKILL/restart: accepted Task IDs, active Run IDs, and asynchronous result handles survive without application-local durable state")

        pages = 1
        while cursor is not None:
            processes.check()
            response = await observer.get(urls[1] + "/v1/tasks/" + task_a + "/events",
                                           params={"limit": 2, "cursor": cursor}, headers={"Authorization": "Bearer " + alice})
            page = decode(response, 200)
            ids = check_page(page, task_a, run_ids[task_a])
            if ids[0] >= event_ids[-1]:
                raise HarnessFailure("A resumed event page overlaps or moves ahead of its previous cursor")
            event_ids.extend(ids)
            cursor = page["next_cursor"]
            pages += 1
            if pages > 100:
                raise HarnessFailure("Durable event pagination exceeded its bounded page budget")
        if event_ids != list(range(event_ids[0], 0, -1)):
            raise HarnessFailure("Cross-replica event cursor replay dropped or duplicated native Temporal history events")
        if page["events"][-1]["type"] != "workflow_execution_started":
            raise HarnessFailure("The durable history cursor did not reach WorkflowExecutionStarted")
        receipt["event_pagination"].update(page_count=pages, all_event_ids=event_ids,
                                            oldest_type=page["events"][-1]["type"])
        receipt["passed"].append("signed durable lifecycle-event cursor continues across API replacement and another replica, with contiguous descending Temporal event IDs through event 1")
        persist()

        if restart_temporal:
            old_server_pid = temporal.process.pid
            for record in [temporal, *workers]:
                await asyncio.to_thread(processes.kill, record)
            if not temporal_database.is_file() or temporal_database.stat().st_size == 0:
                raise HarnessFailure("The owned Temporal service did not retain its SQLite history")
            temporal = server("temporal-2")
            await SUPPORT.connect(processes, address)
            workers = [worker("worker-c"), worker("worker-d")]
            for task_id in (task_a, task_b):
                response = await read_until(processes, observer, urls[0] + "/v1/tasks/" + task_id, alice,
                                            lambda value: value.status_code == 200, timeout=30)
                value = decode(response, 200)
                if value.get("run_id") != run_ids[task_id] or value.get("status") != "running":
                    raise HarnessFailure("Temporal/Worker replacement changed an already accepted Task identity")

            def retries_visible(value):
                return all(value) and {item["pid"] for item in value} == {record.process.pid for record in workers} and all(item["attempt"] > 1 for item in value)
            retried = await SUPPORT.eventually(processes, markers, retries_visible, timeout=30)
            for task_id in (task_a, task_b):
                recovered = await temporal_client.get_workflow_handle(task_id).query("snapshot")
                difference = SUPPORT.first_difference(initial_snapshots[task_id], recovered, "snapshot")
                if difference:
                    raise HarnessFailure("Cold replay changed the full native state at " + difference)
            receipt["temporal_restart"] = {"old_server_pid": old_server_pid, "new_server_pid": temporal.process.pid,
                                            "cold_worker_markers": retried, "preserved_run_ids": run_ids}
            receipt["passed"].append("Temporal and both Workers SIGKILL/restart on the same history database: live API replicas reconnect and new Worker PIDs retry both Tasks under their original Run identities")
            persist()
        else:
            receipt["skipped"] = ["optional same-database Temporal/Worker process replacement; separate prototype gate covers this boundary"]

        decode(await observer.post(urls[0] + "/v1/tasks/" + task_a + "/cancel",
                                    headers={"Authorization": "Bearer " + alice}), 202)
        cancelled_response = await read_until(processes, observer, urls[1] + "/v1/tasks/" + task_a, alice,
                                               lambda response: response.status_code == 200 and response.json().get("status") == "cancelled")
        cancelled = decode(cancelled_response, 200)
        if cancelled.get("run_id") != run_ids[task_a]:
            raise HarnessFailure("Cancellation changed the accepted Run identity")
        still_running = decode(await observer.get(urls[1] + "/v1/tasks/" + task_b,
                                                  headers={"Authorization": "Bearer " + alice}), 200)
        if still_running.get("status") != "running":
            raise HarnessFailure("Cancelling one Task stopped a different running Task")
        release(barriers, task_b)
        final_response = await read_until(processes, observer, urls[1] + "/v1/tasks/" + task_b + "/result", alice,
                                           lambda response: response.status_code == 200 and response.json().get("ready") is True, timeout=30)
        final = decode(final_response, 200)
        snapshot = final.get("result")
        if final.get("status") != "succeeded" or final.get("run_id") != run_ids[task_b] or not isinstance(snapshot, dict):
            raise HarnessFailure("The detached Task did not return its completed result under the accepted Run identity")
        check_snapshot_contract(snapshot, task_b, run_ids[task_b], "task-v2", "completed")
        if snapshot.get("files", {}).get("/plan.md", {}).get("content") != "plan:" + message_b:
            raise HarnessFailure("The completed HTTP result lost the real Deep Agents virtual file")
        check_preserved_state(initial_snapshots[task_b], snapshot)
        history = await temporal_client.get_workflow_handle(task_b).fetch_history()
        activity_types = sorted({event.activity_task_scheduled_event_attributes.activity_type.name for event in history.events
                                 if event.HasField("activity_task_scheduled_event_attributes")})
        if "deepagents.invoke_model" not in activity_types:
            raise HarnessFailure("Temporal history lacks independently persisted model Activities")
        duplicate = decode(await detached_submit(processes, urls[0], alice, key_b, message_b), 202)
        if duplicate.get("task_id") != task_b or duplicate.get("reused") is not True:
            raise HarnessFailure("A repeated completed submission was not reconciled with its retained Workflow history")
        receipt.update(cancelled_task=cancelled, completed_result=final, observed_activity_types=activity_types)
        receipt["passed"].append("after API replacement, owner cancellation terminates only its selected Task; another detached Task preserves its full original message prefix/file metadata/plan content, completes its todos, and reuses retained-history duplicate submission")
        persist()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--temporal-binary", type=Path, required=True)
    parser.add_argument("--binary-sha256", help="Expected SHA-256 of the already selected executable")
    parser.add_argument("--output-dir", type=Path, default=MODULE.parent / "var/task-server-prototype")
    parser.add_argument("--timeout-seconds", type=float, default=180)
    parser.add_argument("--restart-temporal", action="store_true", help="Also replace the Temporal service and both Workers while Tasks are active")
    args = parser.parse_args(argv)
    started, run_id = time.monotonic(), uuid4().hex
    directory = args.output_dir.resolve() / run_id
    directory.mkdir(parents=True, mode=0o700)
    receipt_path = directory / "receipt.json"
    receipt = {"run_id": run_id, "driver_pid": os.getpid(), "status": "running", "started_at": SUPPORT.timestamp(),
               "passed": [], "cleanup_confirmed": False, "production_ha_acceptance": False,
               "rss_budget_mib": 2048, "minimum_memory_free_percent": 15,
               "scope": "private local Temporal plus two real HTTP APIs and deterministic-model Deep Agents Workers",
               "excluded_claims": ["production cluster HA", "real provider inference", "permanent idempotency", "external-tool exactly-once", "token streaming replay"]}
    persist = lambda: SUPPORT.private_json(receipt_path, receipt, run_id)
    persist()
    processes = watcher = None
    exit_code = 1
    previous_sigterm = signal.getsignal(signal.SIGTERM)

    def interrupted(_signum, _frame):
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        raise HarnessFailure("The HTTP acceptance driver received SIGTERM")

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
                await exercise(processes, binary, directory, run_id, receipt, persist,
                               restart_temporal=args.restart_temporal)

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
            receipt["processes"] = [{"name": item.name, "pid": item.process.pid, "returncode": item.process.poll(),
                                     "log": str(item.log_path)} for item in processes.records]
            if not receipt["cleanup_confirmed"]:
                receipt.update(status="failed", reason="Cleanup of the owned acceptance processes could not be confirmed")
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
