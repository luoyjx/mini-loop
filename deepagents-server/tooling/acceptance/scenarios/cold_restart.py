"""Six real Tasks across an observed all-service cold restart, bounded locally.

The driver never creates an HTTP or Temporal client. An owned subprocess is
both submitter and observer; it is killed with the old application and replaced.
Only the approved existing arm64 image and host CLI are used. No images pulled."""
from __future__ import annotations

import argparse
import asyncio
import collections
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

from tooling.paths import MODULE, FIXTURES
from tooling.acceptance.support import process as SUPPORT
from tooling.acceptance.support.process import HarnessFailure, ResourceBlocked
from tooling.acceptance.support import http as HTTP
from tooling.acceptance.support import tls as TLS
from tooling.acceptance.support import containers as DOCKER
from tooling.acceptance.support import container_checks as CONTAINER
from tooling.acceptance.fixtures import task_can_worker_fixture as CAN
from tooling.acceptance.support.container_checks import PYTHON
from tooling.acceptance.support.image_contract import validate_image_id, validate_manifest, verify_image_manifest



def check_queued(descriptions, expected_ids, starts):
    if {row["task_id"] for row in descriptions} != set(expected_ids):
        raise HarnessFailure("Native queue observation lost an accepted Task")
    for row in descriptions:
        if row["status"] != "RUNNING" or len(row["pending_activities"]) != 1 or row["pending_activities"][0]["state"] != "PENDING_ACTIVITY_STATE_SCHEDULED":
            raise HarnessFailure("An intended queued Task was not a native SCHEDULED Activity")
    if any(row["workflow_id"] in expected_ids for row in starts):
        raise HarnessFailure("A queued Task executed a model before the cold outage")


def check_zero_window(samples):
    if len(samples) < 2 or samples[-1]["monotonic_ns"] - samples[0]["monotonic_ns"] < 3_000_000_000:
        raise HarnessFailure("The zero-service cold window did not last three seconds")
    ids, pids = samples[0]["container_ids"], samples[0]["host_pids"]
    if len(ids) != 4 or len(set(ids)) != 4 or len(pids) != 2 or len(set(pids)) != 2:
        raise HarnessFailure("Cold-window observation did not include four business containers and both host roles")
    if any(row["container_ids"] != ids or row["host_pids"] != pids or row["running_container_count"] != 0 or row["live_host_process_count"] != 0 for row in samples):
        raise HarnessFailure("An old API, Worker, Temporal or observer remained alive during cold downtime")


def check_final(value, record):
    snapshot = value.get("result", {})
    expected = {"task_id": record["task_id"], "workflow_id": record["task_id"], "owner": record["owner"],
        "first_execution_run_id": record["first_execution_run_id"]}
    if (value.get("status") != "succeeded" or value.get("ready") is not True
            or snapshot.get("identity") != expected or snapshot.get("status") != "completed"
            or snapshot.get("version") != "task-v2" or snapshot.get("schema_version") != 2 or snapshot.get("can_count") != 4):
        raise HarnessFailure("A cold-recovered Task did not retain identity and complete four real CAN transitions")
    messages = snapshot["messages"]
    ids = [x["data"].get("tool_call_id") for x in messages if x["type"] == "tool"]
    if ids != [f"can-main-{step:03d}" for step in range(37)] or sum(x["type"] == "human" for x in messages) != 1:
        raise HarnessFailure("A cold-recovered Task duplicated or lost parent tools or original input")
    tools = {x["data"].get("tool_call_id"): x["data"] for x in messages if x["type"] == "tool"}
    for step in (3, 18):
        if tools[f"can-main-{step:03d}"]["content"] != f"CAN_COMPLETE:child{step:03d}":
            raise HarnessFailure("A real general-purpose subagent result was not preserved")
    for path, content in CAN.expected_files().items():
        file = snapshot["files"].get(path, {})
        if file.get("content") != content or not file.get("created_at") or not file.get("modified_at"):
            raise HarnessFailure("Native parent or subagent files did not preserve content and metadata")
    if snapshot["todos"] != [{"content": "finish 37 native tool steps with two subagents", "status": "completed"}]:
        raise HarnessFailure("The native plan was not preserved and completed")
    state = snapshot["state"]
    event = state.get("_summarization_event", {})
    if not state.get("_summarization_session_id") or event.get("file_path") not in snapshot["files"] or not event.get("summary_message"):
        raise HarnessFailure("Real private summarization and history offload state was lost")
    return {**expected, "final_run_id": value["run_id"], "status": "succeeded", "can_count": 4,
        "parent_tools": 37, "general_purpose_subagents": 2,
        "snapshot_sha256": hashlib.sha256(json.dumps(snapshot, sort_keys=True, separators=(",", ":")).encode()).hexdigest()}


def check_chain(rows, record, final_run):
    if len(rows) != 5 or rows[0]["run_id"] != record["first_execution_run_id"] or rows[-1]["run_id"] != final_run:
        raise HarnessFailure("Native history did not contain the complete five-Run chain")
    for index, row in enumerate(rows):
        if row["first_execution_run_id"] != record["first_execution_run_id"] or row["workflow_task_failures"]:
            raise HarnessFailure("A native Run lost its first identity or recorded WorkflowTaskFailed")
        previous = rows[index - 1]["run_id"] if index else None
        following = rows[index + 1]["run_id"] if index < 4 else None
        if row["previous_run_id"] != previous or row["next_run_id"] != following:
            raise HarnessFailure("Native previous/next continuation links are broken")
        if index and row["start_input_sha256"] != rows[index - 1]["continued_input_sha256"]:
            raise HarnessFailure("The complete continuation payload changed between native Runs")


def check_attempts(starts, finishes, task_ids):
    groups = collections.defaultdict(list)
    for row in starts: groups[(row["workflow_id"], row["run_id"], row["activity_id"])].append(row)
    maximum = 0
    for rows in groups.values():
        attempts = sorted(row["attempt"] for row in rows)
        if attempts != list(range(1, len(rows) + 1)) or len(rows) > 3 or len({row["request_sha256"] for row in rows}) != 1:
            raise HarnessFailure("Model retries exceeded their budget, lost an attempt or changed request bytes")
        maximum = max(maximum, max(attempts))
    if {row["workflow_id"] for row in starts} != set(task_ids):
        raise HarnessFailure("Model attempt audit did not cover exactly the six accepted Tasks")
    summary_runs = {}
    for task_id in task_ids:
        rows = [row for row in starts if row["workflow_id"] == task_id]
        summary_runs[task_id] = sorted({row["run_id"] for row in rows if row["summary"]})
        if len(summary_runs[task_id]) < 2 or not {"child003", "child018"}.issubset({row["role"] for row in rows}):
            raise HarnessFailure("A Task lacks real subagent model Activities or summaries across multiple Runs")
    return {"native_activity_count": len(groups), "model_attempt_count": len(starts), "maximum_attempt": maximum,
        "retried_activity_count": sum(len(rows) > 1 for rows in groups.values()),
        "finish_outcomes": dict(collections.Counter(row["outcome"] for row in finishes)),
        "summary_run_ids": summary_runs, "request_sha_preserved_for_every_retry": True}


class Observer:
    def __init__(self, processes, directory, phase, address, urls, secret_file):
        self.processes = processes
        self.directory = directory / ("observer-" + phase)
        self.directory.mkdir(mode=0o700)
        self.serial = 0
        SUPPORT.private_json(self.directory / "configuration.json", {"temporal_address": address,
            "api_urls": urls, "secrets_file": str(secret_file)}, phase)
        self.process = processes.spawn("observer-" + phase, [sys.executable, "-m", "tooling.acceptance.fixtures.task_cold_observer", "--directory", self.directory])

    async def ready(self):
        async def read():
            if self.process.process.poll() is not None:
                raise HarnessFailure("The owned observer exited before readiness")
            path = self.directory / "ready.json"
            return json.loads(path.read_text()) if path.exists() else None
        return await SUPPORT.eventually(self.processes, read, lambda x: x is not None, timeout=20)

    async def call(self, operation, **arguments):
        if self.serial >= 1024:
            raise HarnessFailure("The bounded observer mailbox command limit was reached")
        self.serial += 1
        request = self.directory / f"request-{self.serial:04d}.json"
        response = self.directory / f"response-{self.serial:04d}.json"
        SUPPORT.private_json(request, {"operation": operation, **arguments}, str(self.serial))
        async def read():
            if self.process.process.poll() is not None:
                raise HarnessFailure("The owned observer exited while processing a command")
            return json.loads(response.read_text()) if response.exists() else None
        result = await SUPPORT.eventually(self.processes, read, lambda x: x is not None, timeout=12)
        # Replies needed for evidence are copied into the receipt or dedicated
        # snapshot artifacts; keep at most one pending command in this mailbox.
        request.unlink(); response.unlink()
        if not result["ok"]:
            raise HarnessFailure("Cold observer operation failed: " + result["error_type"])
        return result["value"]


async def exercise(processes, containers, binary, directory, run_id, receipt, persist, credentials):
    from mini_loop_deepagents.task_server.service import task_id_for
    receipt["host_loopback_probe"] = await CONTAINER.probe_loopback(containers, processes)
    port, *api_ports = HTTP.free_ports(3)
    host_address, container_address = f"127.0.0.1:{port}", f"host.docker.internal:{port}"
    queue, database = "cold-" + run_id, directory / "temporal.sqlite"
    urls = [f"http://127.0.0.1:{p}" for p in api_ports]
    timeline = []
    def event(kind, **data):
        timeline.append({"kind": kind, "monotonic_ns": time.monotonic_ns(), "wall_time_ns": time.time_ns(), **data})
        receipt["timeline"] = timeline
        persist()
    def temporal(name):
        result = processes.spawn(name, [binary, "--disable-config-env", "--disable-config-file", "--log-level", "warn",
            "server", "start-dev", "--ip", "127.0.0.1", "--port", port, "--headless", "--db-filename", database],
            extra_environment={"GOMAXPROCS": "2", "GOMEMLIMIT": "512MiB"})
        event("temporal_spawned", name=name, pid=result.process.pid)
        return result
    secret_dir, gates = directory / "secrets", directory / "gates"
    secret_dir.mkdir(mode=0o700); gates.mkdir(mode=0o755)
    owner_tokens = {owner: secrets.token_urlsafe(32) for owner in ("alice", "bob", "carol")}
    credentials.update({"token_" + k: v for k, v in owner_tokens.items()})
    credentials.update(cursor=secrets.token_urlsafe(32), model="synthetic-cold-model-" + secrets.token_urlsafe(32))
    for name, value in (("tokens.json", json.dumps({v: k for k, v in owner_tokens.items()})), ("cursor.txt", credentials["cursor"]), ("model.txt", credentials["model"])):
        SUPPORT.private_text(secret_dir / name, value + "\n"); (secret_dir / name).chmod(0o444)
    secret_file = directory / "observer-secrets.json"
    SUPPORT.private_json(secret_file, {"tokens": owner_tokens, "cursor": credentials["cursor"], "model": credentials["model"]}, run_id)
    records = []
    for index, owner in enumerate(("alice", "bob", "carol", "alice", "bob", "carol")):
        key = f"cold-key-{index % 2}-{run_id}"
        records.append({"owner": owner, "key": key, "message": f"[can-main] cold acceptance {run_id} {index}",
            "task_id": task_id_for(owner, key)})
    assert len({x["task_id"] for x in records}) == 6
    lead_ids = [x["task_id"] for x in records[:2]]
    counters = {"api": 0, "worker": 0}
    contracts = []
    def create_api(index):
        counters["api"] += 1
        row = containers.create(f"cold-{run_id[:10]}-api-{counters['api']}",
            ["api", "--address", "0.0.0.0:8123", "--temporal-address", container_address, "--task-queue", queue],
            {"MINILOOP_DEEPAGENTS_TOKENS_FILE": "/run/secrets/tokens.json", "MINILOOP_TASK_SERVER_CURSOR_SECRET_FILE": "/run/secrets/cursor.txt", "LANGSMITH_TRACING": "false"},
            [(secret_dir / n, "/run/secrets/" + n) for n in ("tokens.json", "cursor.txt")], port=api_ports[index])
        contracts.append(CONTAINER.check_container_contract(containers.owned(row), "api", ["/run/secrets/tokens.json", "/run/secrets/cursor.txt"]))
        return row
    def create_worker():
        counters["worker"] += 1
        files = ("__init__.py", "task_cold_worker_fixture.py", "task_can_worker_fixture.py", "task_container_worker_fixture.py", "task_rollout_worker_fixture.py")
        row = containers.create(f"cold-{run_id[:10]}-worker-{counters['worker']}",
            ["-m", "fixtures.task_cold_worker_fixture", "--address", container_address, "--task-queue", queue,
                "--gate-directory", "/opt/task-gates", "--gate-task-ids", ",".join(lead_ids)],
            {"OPENAI_API_KEY_FILE": "/run/secrets/model.txt", "LANGSMITH_TRACING": "false", "PYTHONPATH": "/opt/task-fixture"},
            [(secret_dir / "model.txt", "/run/secrets/model.txt"), (gates, "/opt/task-gates"),
                *[(FIXTURES / n, "/opt/task-fixture/fixtures/" + n) for n in files]], worker=True, entrypoint=[PYTHON])
        contracts.append(CONTAINER.check_container_contract(containers.owned(row), "worker", ["/run/secrets/model.txt"]))
        return row
    async def ready(observer, code):
        for index in range(2):
            async def get():
                try:
                    return await observer.call("http", path="/readyz", replica=index)
                except HarnessFailure:
                    return None
            value = await SUPPORT.eventually(processes, get, lambda x: x is not None and x["status_code"] == code)
            if value["body"] != {"status": "ready" if code == 200 else "not_ready"} or value["cache_control"] != "no-store":
                raise HarnessFailure("Readiness did not report the real dependency state")
    async def post(observer, record, replica, reused):
        value = await observer.call("http", method="POST", path="/v1/tasks", owner=record["owner"], key=record["key"], replica=replica, json={"message": record["message"]})
        body = value["body"]
        if value["status_code"] != 202 or body.get("task_id") != record["task_id"] or body.get("reused") is not reused or body.get("version") != "task-v2":
            raise HarnessFailure("An accepted owner-scoped Task failed create/reuse identity reconciliation")
        if "first_execution_run_id" in record and body["first_execution_run_id"] != record["first_execution_run_id"]:
            raise HarnessFailure("A duplicate submission changed the original execution chain")
        record.setdefault("first_execution_run_id", body["first_execution_run_id"])
        return body
    def audits(workers, kind):
        return [x for row in workers for x in CONTAINER.markers(containers.logs(row), kind)]
    async def wait_gates(workers):
        async def read(): return audits(workers, "MODEL_GATE")
        result = await SUPPORT.eventually(processes, read, lambda rows: {x["workflow_id"] for x in rows} == set(lead_ids), timeout=40)
        if len(result) != 2 or len({x["hostname"] for x in result}) != 2:
            raise HarnessFailure("Two parent-24 model gates did not occupy separate Worker slots")
        return result
    async def probes(apis, workers):
        for row in apis:
            value = json.loads(containers.engine.exec(row["id"], [PYTHON, "-c", CONTAINER.SECURITY_PROBE]))
            CONTAINER.check_runtime_probe(value, next(x for x in contracts if x["container_id"] == row["id"]))
        for row in workers:
            values = CONTAINER.markers(containers.logs(row), "SECURITY")
            if len(values) != 1 or values[0]["hostname"] != row["name"]:
                raise HarnessFailure("A Worker did not publish its actual security contract")
            CONTAINER.check_runtime_probe(values[0], next(x for x in contracts if x["container_id"] == row["id"]))

    old_temporal = temporal("temporal-before")
    before_observer = Observer(processes, directory, "before", host_address, urls, secret_file)
    receipt["observer_before"] = await before_observer.ready()
    apis = [create_api(x) for x in range(2)]
    await ready(before_observer, 200)
    for index, record in enumerate(records[:2]):
        await post(before_observer, record, index, False)
        record["accepted_monotonic_ns"] = time.monotonic_ns()
    workers = [create_worker(), create_worker()]
    initial_gates = await wait_gates(workers)
    await probes(apis, workers)
    snapshots = {}
    for record in records[:2]:
        value = await before_observer.call("snapshot", task_id=record["task_id"])
        if value.get("can_count") != 3 or not value["state"].get("_summarization_event"):
            raise HarnessFailure("A lead Task did not reach three native CAN segments with real summary state")
        snapshots[record["task_id"]] = value
    for index, record in enumerate(records[2:]):
        await post(before_observer, record, index % 2, False)
        record["accepted_monotonic_ns"] = time.monotonic_ns()
    async def queued():
        return [await before_observer.call("describe", task_id=x["task_id"]) for x in records[2:]]
    def scheduled(rows):
        return all(len(x["pending_activities"]) == 1 and x["pending_activities"][0]["state"] == "PENDING_ACTIVITY_STATE_SCHEDULED" for x in rows)
    queue_before = await SUPPORT.eventually(processes, queued, scheduled)
    check_queued(queue_before, {x["task_id"] for x in records[2:]}, audits(workers, "MODEL_START"))
    receipt.update(initial_gates=initial_gates, queued_before=queue_before, accepted_task_count=6, owner_count=3,
        activity_slots_per_worker=1, api_count=2, worker_count=2, task_queue=queue, container_contracts=contracts)
    receipt["passed"].append("two real Tasks reach parent 24 after three CANs with child/summary state; four other owner-scoped Tasks are accepted and remain native SCHEDULED without model execution")
    event("six_tasks_accepted_and_two_slots_occupied")
    first_event_page = await before_observer.call("http", path=f"/v1/tasks/{records[0]['task_id']}/events", owner=records[0]["owner"],
        params={"run_id": records[0]["first_execution_run_id"], "limit": 2})
    cursor = first_event_page["body"].get("next_cursor")
    if first_event_page["status_code"] != 200 or not cursor:
        raise HarnessFailure("The retained first Run did not provide a resumable event cursor")
    age_target = max(x["accepted_monotonic_ns"] for x in records[:2]) + 45_000_000_000
    while time.monotonic_ns() < age_target:
        processes.check(); await asyncio.sleep(0.1)
    receipt["lead_ages_before_fault_seconds"] = [(time.monotonic_ns() - x["accepted_monotonic_ns"]) / 1e9 for x in records[:2]]
    SUPPORT.private_json(directory / "accepted.json", records, run_id)
    processes.kill(old_temporal)
    event("temporal_sigkill_during_parent_24", pid=old_temporal.process.pid, exit_code=old_temporal.process.returncode)
    await ready(before_observer, 503)
    liveness = [await before_observer.call("http", path="/healthz", replica=x) for x in range(2)]
    if any(x["status_code"] != 200 for x in liveness):
        raise HarnessFailure("An API lost liveness when Temporal stopped")
    receipt["dependency_outage"] = {"readyz": 503, "healthz": 200, "tasks_still_inflight": True}
    old_business = [*apis, *workers]
    for row in old_business:
        value = containers.stop(row, kill=True)
        if value["State"]["ExitCode"] != 137 or value["State"].get("OOMKilled"):
            raise HarnessFailure("The intended application SIGKILL was not observed")
    processes.kill(before_observer.process)
    event("all_old_services_and_observer_stopped", observer_pid=before_observer.process.process.pid)
    zeros = []
    zero_deadline = time.monotonic() + 3.05
    while True:
        zeros.append({"monotonic_ns": time.monotonic_ns(), "container_ids": [x["id"] for x in old_business],
            "host_pids": [old_temporal.process.pid, before_observer.process.process.pid],
            "running_container_count": sum(containers.owned(x)["State"]["Running"] for x in old_business),
            "live_host_process_count": sum(x.process.poll() is None for x in (old_temporal, before_observer.process))})
        if time.monotonic() >= zero_deadline: break
        processes.check(); await asyncio.sleep(0.2)
    check_zero_window(zeros)
    receipt["zero_service_window"] = zeros
    if not database.is_file() or not database.stat().st_size:
        raise HarnessFailure("The original Temporal SQLite directory was not retained")
    receipt["cold_sqlite_files"] = [{"name": p.name, "bytes": p.stat().st_size, "sha256": hashlib.sha256(p.read_bytes()).hexdigest()}
        for p in directory.glob("temporal.sqlite*") if p.is_file()]
    event("zero_service_window_confirmed", seconds=(zeros[-1]["monotonic_ns"] - zeros[0]["monotonic_ns"]) / 1e9)

    new_temporal = temporal("temporal-after")
    observer = Observer(processes, directory, "after", host_address, urls, secret_file)
    receipt["observer_after"] = await observer.ready()
    if receipt["observer_after"]["pid"] == receipt["observer_before"]["pid"]:
        raise HarnessFailure("The cold observer reused the old process")
    apis = [create_api(x) for x in range(2)]
    await ready(observer, 200)
    receipt["queued_after_temporal_before_workers"] = [await observer.call("describe", task_id=x["task_id"]) for x in records[2:]]
    check_queued(receipt["queued_after_temporal_before_workers"], {x["task_id"] for x in records[2:]}, [])
    for index, record in enumerate(records): await post(observer, record, index % 2, True)
    event("fresh_apis_observe_all_six_tasks_before_any_new_worker")
    event("new_workers_starting")
    workers = [create_worker(), create_worker()]
    retried = await wait_gates(workers)
    await probes(apis, workers)
    replay = []
    for record in records[:2]:
        task_id = record["task_id"]
        old = next(x for x in initial_gates if x["workflow_id"] == task_id)
        new = next(x for x in retried if x["workflow_id"] == task_id)
        CONTAINER.check_takeover(old, new)
        current = await observer.call("snapshot", task_id=task_id)
        TLS.check_snapshot_preserved(snapshots[task_id], current)
        path = directory / (task_id + ".replay.json")
        SUPPORT.private_json(path, {"before": snapshots[task_id], "after": current}, run_id)
        replay.append({"task_id": task_id, "before_activity": old, "after_activity": new,
            "full_native_state_equal": True, "snapshot_file": str(path), "snapshot_file_sha256": hashlib.sha256(path.read_bytes()).hexdigest()})
        (gates / (CAN.task_stem(task_id) + ".release")).touch(mode=0o444)
    receipt["replay"] = replay
    receipt["passed"].append("all Temporal/API/Worker/observer processes are absent for at least three seconds; fresh processes and connections recover all accepted tasks, complete snapshots and model request identity")
    event("lead_tasks_replayed_and_released")
    completed = {}
    while len(completed) < 6:
        processes.check()
        for index, record in enumerate(records):
            if record["task_id"] in completed: continue
            value = await observer.call("http", path=f"/v1/tasks/{record['task_id']}/result", owner=record["owner"], replica=index % 2)
            if value["status_code"] == 200:
                result = check_final(value["body"], record)
                result["accepted_to_observed_terminal_seconds"] = round((time.monotonic_ns() - record["accepted_monotonic_ns"]) / 1e9, 3)
                completed[record["task_id"]] = result
                SUPPORT.private_json(directory / (record["task_id"] + ".result.json"), value["body"], run_id)
            elif value["status_code"] != 202:
                raise HarnessFailure("An accepted Task disappeared or became unreadable after cold restart")
        if len(completed) < 6: await asyncio.sleep(0.1)
    chains = []
    for record in records:
        rows = []; native_run = record["first_execution_run_id"]
        while native_run:
            if len(rows) >= 5: raise HarnessFailure("A Task exceeded its expected native Run chain")
            value = await observer.call("history", task_id=record["task_id"], run_id=native_run)
            rows.append(value); native_run = value["next_run_id"]
        check_chain(rows, record, completed[record["task_id"]]["final_run_id"])
        chains.append({"task_id": record["task_id"], "runs": rows})
        await post(observer, record, 1, True)
        foreign = next(x for x in owner_tokens if x != record["owner"])
        denied = await observer.call("http", path=f"/v1/tasks/{record['task_id']}/result", owner=foreign)
        if denied["status_code"] != 404: raise HarnessFailure("Cold restart lost owner isolation")
    new_worker_wall_ns = next(x["wall_time_ns"] for x in timeline if x["kind"] == "new_workers_starting")
    queue_history = []
    for record in records[2:]:
        first = next(x for x in chains if x["task_id"] == record["task_id"])["runs"][0]
        activity_id = next(x for x in queue_before if x["task_id"] == record["task_id"])["pending_activities"][0]["activity_id"]
        scheduled = next(x for x in first["activity_events"] if x["kind"] == "scheduled" and x["activity_id"] == activity_id)
        started = next(x for x in first["activity_events"] if x["kind"] == "started" and x["scheduled_event_id"] == scheduled["event_id"])
        if started["time_ns"] < new_worker_wall_ns or started["attempt"] != 1 or started["identity"] not in {row["name"] + ":1" for row in workers}:
            raise HarnessFailure("Native History did not confirm that queued work first started on a cold replacement Worker")
        queue_history.append({"task_id": record["task_id"], "scheduled": scheduled, "started": started,
            "clock": "host Temporal UTC event time compared with host driver UTC time"})
    old_page = await observer.call("http", path=f"/v1/tasks/{records[0]['task_id']}/events", owner=records[0]["owner"], params={"cursor": cursor, "limit": 2})
    if old_page["status_code"] != 200 or old_page["body"].get("run_id") != records[0]["first_execution_run_id"]:
        raise HarnessFailure("A cursor from the dead observer did not remain bound to the retained original Run")
    all_workers = [x for x in containers.records if x["role"] == "worker"]
    starts, finishes = audits(all_workers, "MODEL_START"), audits(all_workers, "MODEL_FINISH")
    attempt_checks = check_attempts(starts, finishes, {x["task_id"] for x in records})
    summary_counts = {r["task_id"]: sum(x["workflow_id"] == r["task_id"] and x["summary"] for x in starts) for r in records}
    if any(n < 2 for n in summary_counts.values()): raise HarnessFailure("A Task did not execute real summary model Activities across segments")
    receipt.update(completed_tasks=list(completed.values()), native_run_chains=chains, container_contracts=contracts,
        summary_activity_counts=summary_counts, model_attempts=starts, model_finishes=finishes,
        model_attempt_checks=attempt_checks, observer_command_counts={"before": before_observer.serial, "after": observer.serial},
        queued_native_start_evidence=queue_history,
        old_cursor_retained=True, queued_tasks_all_completed=True, same_key_reused_after_cold_start=True,
        new_business_container_ids=[x["id"] for x in [*apis, *workers]], new_temporal_pid=new_temporal.process.pid)
    receipt["passed"].append("all six multi-owner Tasks complete 37 native parent tools, two general-purpose subagents, real summaries and four CANs; 30 native Runs have zero WorkflowTaskFailed, queued tasks drain and original identities/cursors remain valid")
    event("six_tasks_verified_terminal")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", required=True, help="Immutable local Task image ID sha256:<digest>; never pulled")
    parser.add_argument("--manifest", type=Path, required=True, help="Audited build manifest for this exact image")
    parser.add_argument("--temporal-binary", type=Path, required=True)
    parser.add_argument("--binary-sha256", required=True)
    parser.add_argument("--output-dir", type=Path, default=MODULE.parent / "var/task-cold-prototype")
    parser.add_argument("--timeout-seconds", type=float, default=180)
    args = parser.parse_args(argv)
    started, run_id = time.monotonic(), uuid4().hex
    directory = args.output_dir.resolve() / run_id
    directory.mkdir(parents=True, mode=0o700)
    receipt_path = directory / "receipt.json"
    receipt = {"run_id": run_id, "driver_pid": os.getpid(), "status": "running", "started_at": SUPPORT.timestamp(), "passed": [],
               "cleanup_confirmed": False, "rss_budget_mib": 2048, "minimum_memory_free_percent": 15,
               "production_ha_acceptance": False, "cross_failure_domain_acceptance": False,
               "real_model_provider_acceptance": False, "long_task_worker_entrypoint": "trusted readonly fixture calling production run_process",
               "native_temporal_tls_acceptance": False, "scope": "six multi-owner multi-segment Tasks across full cold restart; production image and trusted readonly model fixture"}
    persist = lambda: SUPPORT.private_json(receipt_path, receipt, run_id)
    persist()
    processes = containers = budget = None
    credentials = {}
    exit_code = 1
    old_signal = signal.getsignal(signal.SIGTERM)
    def interrupted(_signum, _frame):
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        raise HarnessFailure("Container acceptance driver received SIGTERM")
    signal.signal(signal.SIGTERM, interrupted)
    try:
        if not 10 <= args.timeout_seconds <= 180:
            raise HarnessFailure("The overall run budget must be between 10 and 180 seconds")
        binary = args.temporal_binary.resolve()
        if not binary.is_file() or not os.access(binary, os.X_OK):
            raise HarnessFailure("An existing Temporal CLI is required; no download was attempted")
        with binary.open("rb") as stream:
            checksum = hashlib.file_digest(stream, "sha256").hexdigest()
        if checksum != args.binary_sha256:
            raise HarnessFailure("The fixed Temporal CLI checksum did not match")
        validate_image_id(args.image)
        receipt["image_source_boundary"] = validate_manifest(args.manifest.resolve())
        engine = DOCKER.Engine()
        image = engine.request("GET", "/images/" + DOCKER.quote(args.image, safe="") + "/json")
        if image.get("Id") != args.image:
            raise HarnessFailure("The selected immutable local image ID did not match; no rebuild or pull was attempted")
        if image.get("Architecture") != "arm64" or image.get("Config", {}).get("User") != "10001:10001":
            raise HarnessFailure("The existing Task image must be arm64 and non-root UID/GID 10001")
        if image["Config"].get("Entrypoint") != [PYTHON, "-m", "mini_loop_deepagents.task_server.launch"]:
            raise HarnessFailure("The Task image does not use the production role entrypoint")
        info = engine.request("GET", "/info")
        receipt.update(image_id=image["Id"], image_architecture=image["Architecture"], image_entrypoint=image["Config"]["Entrypoint"],
                       docker_platform={key: info.get(key) for key in ("OperatingSystem", "Architecture", "NCPU", "MemTotal", "ServerVersion", "CgroupVersion", "CgroupDriver")},
                       temporal_binary_sha256=checksum)
        processes = SUPPORT.Processes(directory, started + args.timeout_seconds - 5)
        containers = DOCKER.Containers(engine, image["Id"], run_id, directory)
        budget = DOCKER.Budget(SUPPORT, processes, containers)
        budget.start()
        async def bounded():
            async with asyncio.timeout(max(0.1, processes.deadline - 5 - time.monotonic())):
                receipt["image_manifest_probe"] = await verify_image_manifest(
                    containers, processes, receipt["image_source_boundary"]["manifest_sha256"])
                persist()
                await exercise(processes, containers, binary, directory, run_id, receipt, persist, credentials)
        asyncio.run(bounded())
        processes.check()
        receipt["status"], exit_code = "passed", 0
    except ResourceBlocked as error:
        receipt.update(status="resource_blocked", reason=str(error)); exit_code = 3
    except BaseException as error:
        receipt.update(status="failed", reason=str(error) if isinstance(error, HarnessFailure) else "Acceptance stopped: " + type(error).__name__)
        SUPPORT.private_text(directory / "driver-error.log", traceback.format_exc())
    finally:
        if budget is not None:
            budget.stop()
        container_cleanup = containers.cleanup() if containers is not None else True
        host_cleanup = processes.stop_all(force=processes.failure is not None) if processes is not None else True
        receipt["cleanup_confirmed"] = container_cleanup and host_cleanup
        if not receipt["cleanup_confirmed"]:
            receipt.update(status="failed", reason="Owned container or host process cleanup was not confirmed"); exit_code = 1
        if processes is not None:
            receipt["host_processes"] = [{"name": row.name, "pid": row.process.pid, "returncode": row.process.poll(), "log": str(row.log_path)} for row in processes.records]
            if processes.failure is not None:
                error = processes.failure
                receipt.update(status="resource_blocked" if isinstance(error, ResourceBlocked) else "failed", reason=str(error))
                exit_code = 3 if isinstance(error, ResourceBlocked) else 1
        if containers is not None:
            receipt["containers"] = containers.records
            if any(row.get("terminal_state", {}).get("OOMKilled") for row in containers.records):
                receipt.update(status="resource_blocked", reason="An owned container was OOM-killed")
                exit_code = 3
        if budget is not None:
            receipt["resource_samples"] = budget.samples
            if budget.samples:
                receipt["peak_aggregate_mib"] = round(max(row["aggregate_bytes"] for row in budget.samples) / DOCKER.MIB, 1)
                host_free = [row["host_free_percent"] for row in budget.samples if row["host_free_percent"] is not None]
                receipt["minimum_host_free_percent"] = min(host_free) if host_free else None
                available = [row["vm"]["available"] / row["vm"]["total"] * 100 for row in budget.samples if row["vm"] is not None]
                receipt["minimum_vm_available_percent"] = round(min(available), 2) if available else None
        try:
            needles = TLS.secret_needles(credentials)
            logs = sorted(directory.glob("*.log"))
            for path in logs:
                TLS.check_no_secrets(path.read_bytes(), needles, path.name)
            receipt["process_log_secret_scan"] = {"credential_matches": 0, "needle_count": len(needles),
                "logs": [{"name": path.name, "sha256": hashlib.sha256(path.read_bytes()).hexdigest()} for path in logs]}
        except HarnessFailure as error:
            receipt.update(status="failed", reason=str(error)); exit_code = 1
        receipt.update(finished_at=SUPPORT.timestamp(), seconds=round(time.monotonic() - started, 2))
        persist()
        signal.signal(signal.SIGTERM, old_signal)
        print(json.dumps({"run_id": run_id, "status": receipt["status"], "seconds": receipt["seconds"], "receipt": str(receipt_path), "passed": receipt["passed"]}, indent=2))
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
