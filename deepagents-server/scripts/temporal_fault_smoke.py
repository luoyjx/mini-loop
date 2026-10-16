"""Bounded local Temporal/Deep Agents process-fault acceptance, not production HA.

Requires an explicitly selected, already downloaded Temporal CLI executable.
Never downloads a server, builds an image, or connects to an existing deployment.
Each run owns its loopback server, worker process groups, databases, and receipt.
"""

from __future__ import annotations

import argparse
import asyncio
from dataclasses import dataclass
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import socket
import subprocess
import sys
import threading
import time
import traceback
from uuid import uuid4


MODULE = Path(__file__).resolve().parents[1]
RSS_BUDGET_KIB = 2 * 1024 * 1024
FREE_FLOOR_PERCENT = 15
MAX_SECONDS = 180


class HarnessFailure(RuntimeError):
    """An intentionally safe diagnostic suitable for the receipt."""


class ResourceBlocked(HarnessFailure):
    pass


def timestamp():
    return datetime.now(timezone.utc).isoformat()


def private_json(path, value, run_id):
    """Atomically replace this run's receipt so stale passes cannot survive a run."""
    temporary = path.with_name(path.name + "." + run_id + ".tmp")
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    try:
        with os.fdopen(descriptor, "w") as stream:
            os.fchmod(stream.fileno(), 0o600)
            json.dump(value, stream, indent=2)
            stream.write("\n")
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def private_text(path, text):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(text)


def first_difference(expected, actual, path="state"):
    """Locate a snapshot mismatch without dumping message bodies or schemas."""
    if type(expected) is not type(actual):
        return path + " (type)"
    if isinstance(expected, dict):
        if set(expected) != set(actual):
            return path + " (keys)"
        for key in sorted(expected):
            difference = first_difference(expected[key], actual[key], path + "." + key)
            if difference:
                return difference
    elif isinstance(expected, list):
        if len(expected) != len(actual):
            return path + " (length)"
        for index, (left, right) in enumerate(zip(expected, actual, strict=True)):
            difference = first_difference(left, right, f"{path}[{index}]")
            if difference:
                return difference
    elif expected != actual:
        return path
    return None


def subprocess_environment():
    allowed = {"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL"}
    environment = {key: value for key, value in os.environ.items() if key in allowed}
    environment.update(PYTHONPATH=str(MODULE / "src"), PYTHONUNBUFFERED="1",
                       LANGSMITH_TRACING="false", OMP_NUM_THREADS="1",
                       OPENBLAS_NUM_THREADS="1", VECLIB_MAXIMUM_THREADS="1")
    return environment


def process_rows():
    result = subprocess.run(["/bin/ps", "-axo", "pid=,ppid=,pgid=,rss="],
                            capture_output=True, text=True, timeout=3, check=False)
    if result.returncode:
        raise HarnessFailure("Owned process RSS could not be observed")
    rows = {}
    for line in result.stdout.splitlines():
        fields = line.split()
        if len(fields) == 4 and all(field.isdigit() for field in fields):
            pid, parent, group, rss = map(int, fields)
            rows[pid] = (parent, group, rss)
    if os.getpid() not in rows:
        raise HarnessFailure("The acceptance driver process was absent from the RSS sample")
    return rows


def owned_rss_kib(rows, driver_pid, process_groups):
    owned = {driver_pid}
    while True:
        additions = {pid for pid, (parent, _, _) in rows.items() if parent in owned}
        if additions <= owned:
            break
        owned.update(additions)
    # Process-group membership retains owned descendants after reparenting.
    owned.update(pid for pid, (_, group, _) in rows.items() if group in process_groups)
    return sum(rows[pid][2] for pid in owned if pid in rows)


def free_percent():
    if sys.platform != "darwin":
        return None
    result = subprocess.run(["/usr/bin/memory_pressure", "-Q"], capture_output=True,
                            text=True, timeout=3, check=False)
    match = re.search(r"System-wide memory free percentage:\s*(\d+)%", result.stdout)
    if result.returncode or not match:
        raise HarnessFailure("System memory pressure could not be observed")
    return int(match[1])


@dataclass
class OwnedProcess:
    name: str
    process: subprocess.Popen
    log_path: Path


class Processes:
    def __init__(self, directory, deadline):
        self.directory, self.deadline = directory, deadline
        self.records = []
        self.lock = threading.RLock()
        self.stop_lock = threading.Lock()
        self.failure = None

    def check(self):
        if self.failure is not None:
            raise self.failure
        # Reserve a bounded cleanup window inside the overall 180-second budget.
        if time.monotonic() >= self.deadline - 5:
            raise HarnessFailure("The acceptance execution deadline was reached")

    def spawn(self, name, arguments, *, extra_environment=None):
        self.check()
        environment = subprocess_environment()
        environment.update(extra_environment or {})
        log_path = self.directory / (name + ".log")
        descriptor = os.open(log_path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
        with os.fdopen(descriptor, "ab", buffering=0) as log:
            process = subprocess.Popen(["/usr/bin/nice", "-n", "10", *map(str, arguments)],
                                       cwd=MODULE, env=environment, stdin=subprocess.DEVNULL,
                                       stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        record = OwnedProcess(name, process, log_path)
        with self.lock:
            self.records.append(record)
        return record

    def signal(self, record, signum):
        with self.lock:
            if record not in self.records:
                raise HarnessFailure("Refusing to signal an unregistered process")
        process = record.process
        if process.poll() is not None:
            raise HarnessFailure("The owned process was already stopped before fault injection")
        # start_new_session makes this group unique to this recorded child.
        if os.getpgid(process.pid) != process.pid:
            raise HarnessFailure("The owned child no longer has its original process group")
        os.killpg(process.pid, signum)

    def kill(self, record):
        self.check()
        self.signal(record, signal.SIGKILL)
        record.process.wait(timeout=min(3, max(0.1, self.deadline - time.monotonic())))
        if record.process.returncode != -signal.SIGKILL:
            raise HarnessFailure("The intended SIGKILL fault was not observed")

    def groups(self):
        with self.lock:
            return {record.process.pid for record in self.records if record.process.poll() is None}

    def stop_all(self, *, force=False):
        with self.stop_lock:
            with self.lock:
                records = list(reversed(self.records))
            for record in records:
                if record.process.poll() is None:
                    try:
                        self.signal(record, signal.SIGKILL if force else signal.SIGTERM)
                    except (ProcessLookupError, HarnessFailure):
                        pass
            grace_deadline = min(self.deadline, time.monotonic() + (0.2 if force else 1.5))
            while any(record.process.poll() is None for record in records) and time.monotonic() < grace_deadline:
                time.sleep(0.02)
            for record in records:
                if record.process.poll() is None:
                    try:
                        self.signal(record, signal.SIGKILL)
                    except (ProcessLookupError, HarnessFailure):
                        pass
            for record in records:
                remaining = self.deadline - time.monotonic()
                if record.process.poll() is None and remaining > 0:
                    try:
                        record.process.wait(timeout=min(0.5, remaining))
                    except subprocess.TimeoutExpired:
                        pass
            return all(record.process.poll() is not None for record in records)


class Watchdog:
    def __init__(self, processes):
        self.processes = processes
        self.peak_kib = 0
        self.minimum_free = None
        self.samples = 0
        self.stopped = threading.Event()
        self.thread = threading.Thread(target=self.observe, name="temporal-fault-resource-watch", daemon=True)

    def observe(self):
        failures = 0
        while not self.stopped.is_set():
            try:
                if time.monotonic() >= self.processes.deadline - 5:
                    raise HarnessFailure("The acceptance execution deadline was reached")
                rss = owned_rss_kib(process_rows(), os.getpid(), self.processes.groups())
                self.peak_kib = max(self.peak_kib, rss)
                free = free_percent()
                if free is not None:
                    self.minimum_free = min(self.minimum_free if self.minimum_free is not None else free, free)
                self.samples += 1
                if rss > RSS_BUDGET_KIB:
                    raise ResourceBlocked("Owned aggregate process RSS exceeded 2 GiB")
                if free is not None and free < FREE_FLOOR_PERCENT:
                    raise ResourceBlocked("System memory free percentage fell below 15 percent")
                failures = 0
            except (ResourceBlocked, HarnessFailure) as error:
                if isinstance(error, ResourceBlocked) or time.monotonic() >= self.processes.deadline - 5:
                    self.processes.failure = error
                    self.processes.stop_all(force=True)
                    return
                failures += 1
                if failures >= 3:
                    self.processes.failure = ResourceBlocked("Resource observation repeatedly failed")
                    self.processes.stop_all(force=True)
                    return
            except Exception:
                failures += 1
                if failures >= 3:
                    self.processes.failure = ResourceBlocked("Resource observation repeatedly failed")
                    self.processes.stop_all(force=True)
                    return
            self.stopped.wait(0.5)

    def start(self):
        self.thread.start()

    def stop(self):
        self.stopped.set()
        self.thread.join(timeout=min(3, max(0, self.processes.deadline - time.monotonic())))


async def eventually(processes, operation, predicate, *, timeout=25):
    deadline = min(processes.deadline - 5, time.monotonic() + timeout)
    last_value = None
    while time.monotonic() < deadline:
        processes.check()
        last_value = await operation()
        if predicate(last_value):
            return last_value
        await asyncio.sleep(0.1)
    raise HarnessFailure("A required fault-recovery condition was not observed before its deadline")


async def connect(processes, address):
    from temporalio.client import Client

    async def attempt():
        try:
            return await asyncio.wait_for(Client.connect(address), timeout=2)
        except Exception:
            return None

    return await eventually(processes, attempt, lambda client: client is not None, timeout=30)


async def exercise(processes, binary, directory, run_id, receipt, persist):
    """Protocol coordinated with temporal_prototype; no external Agent Server."""
    from mini_loop_deepagents.temporal_prototype import activities
    from mini_loop_deepagents.temporal_prototype.client import PrototypeClient
    from temporalio.exceptions import WorkflowAlreadyStartedError

    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    address, queue = f"127.0.0.1:{port}", "fault-" + run_id
    temporal_database = directory / "temporal.sqlite"
    application_database = directory / "application.sqlite"
    activities.initialize_database(str(application_database))
    server_arguments = [binary, "--disable-config-env", "--disable-config-file", "--log-level", "warn",
                        "server", "start-dev", "--ip", "127.0.0.1", "--port", str(port),
                        "--headless", "--db-filename", str(temporal_database)]
    server = processes.spawn("server-1", server_arguments,
                             extra_environment={"GOMAXPROCS": "2", "GOMEMLIMIT": "512MiB"})
    temporal_client = await connect(processes, address)

    def worker(identity):
        return processes.spawn(identity, [sys.executable, "-m", "mini_loop_deepagents.temporal_prototype.worker",
                                          "--address", address, "--task-queue", queue,
                                          "--database", str(application_database), "--identity", identity])

    worker_a, worker_b = worker("worker-a"), worker("worker-b")
    alice = PrototypeClient(temporal_client, database=str(application_database), owner="alice", task_queue=queue)
    bob = PrototypeClient(temporal_client, database=str(application_database), owner="bob", task_queue=queue)
    workflow_ids = ["temporal-fault-" + run_id + "-a", "temporal-fault-" + run_id + "-b"]
    handles = []
    for index, workflow_id in enumerate(workflow_ids):
        handles.append(await alice.start(workflow_id, "job-" + str(index), continue_as_new=True, block_after_effect=True))
    receipt.update(workflow_ids=workflow_ids, task_queue=queue, temporal_database=str(temporal_database),
                   application_database=str(application_database), owner_boundary="controlled fixed-owner client adapter; not production HTTP or Temporal ACL")
    persist()

    async def inspect():
        return await asyncio.to_thread(activities.inspect_database, str(application_database))

    def effects_for(database, workflow_id):
        return [row for row in database["effects"] if row["workflow_id"] == workflow_id]

    async def initial_barrier():
        database = await inspect()
        if not all(effects_for(database, workflow_id) for workflow_id in workflow_ids):
            for handle in handles:
                native = temporal_client.get_workflow_handle(handle.workflow_id)
                description = await native.describe()
                if description.status.name != "RUNNING":
                    # Preserve the real SDK failure and its cause in driver-error.log
                    # instead of hiding it behind a generic barrier timeout.
                    await native.result()
                    raise HarnessFailure("A Workflow completed before the required effect fault barrier")
        return database

    initial = await eventually(processes, initial_barrier,
                               lambda database: all(effects_for(database, workflow_id) for workflow_id in workflow_ids))
    snapshots = [await alice.snapshot(workflow_id) for workflow_id in workflow_ids]
    receipt["initial_snapshots"] = snapshots
    persist()
    for snapshot in snapshots:
        if not snapshot.get("files") or not snapshot.get("todos") or not snapshot.get("messages"):
            raise HarnessFailure("A blocked real Deep Agents task did not expose files, todos, and messages")
    receipt["passed"].append("two real Deep Agents workflows have checkpointed plans/files/messages and committed memory effects while concurrently blocked")
    persist()

    # The adapter verifies owner from the trusted database binding before access.
    for operation in (bob.task_handle, bob.snapshot, bob.result, bob.cancel):
        try:
            value = operation(workflow_ids[0])
            if hasattr(value, "__await__"):
                await value
        except PermissionError:
            continue
        raise HarnessFailure("The fixed-owner adapter accepted a foreign task binding")
    receipt["passed"].append("foreign owner handle, snapshot, result, and cancellation rejected by trusted mapping adapter")

    calls = initial["activity_calls"]
    active_pids = {row["pid"] for row in calls if row["workflow_id"] in workflow_ids and row.get("activity") == "memory_put"}
    if active_pids != {worker_a.process.pid, worker_b.process.pid}:
        raise HarnessFailure("The blocked effects did not execute on two independently owned Worker processes")
    killed_workflow = next(row["workflow_id"] for row in calls if row.get("activity") == "memory_put" and row["pid"] == worker_a.process.pid)
    stable_effects = {workflow_id: [row["effect_id"] for row in effects_for(initial, workflow_id)] for workflow_id in workflow_ids}
    await asyncio.to_thread(processes.kill, worker_a)
    worker_c = worker("worker-c")

    def replacement_observed(database):
        return any(row["workflow_id"] == killed_workflow and row["pid"] == worker_c.process.pid
                   and row.get("activity") == "memory_put" and row["attempt"] > 1 for row in database["activity_calls"])

    recovered = await eventually(processes, inspect, replacement_observed, timeout=25)
    for workflow_id in workflow_ids:
        if [row["effect_id"] for row in effects_for(recovered, workflow_id)] != stable_effects[workflow_id]:
            raise HarnessFailure("The committed-before-ack tool effect was duplicated during Worker takeover")
    receipt["passed"].append("SIGKILL after effect commit/before Activity acknowledgement; new Worker PID retries with the same stable effect ID and no duplicate effect")
    persist()

    before_runs = [(await temporal_client.get_workflow_handle(handle.workflow_id).describe()).run_id for handle in handles]
    old_server_pid = server.process.pid
    await asyncio.to_thread(processes.kill, server)
    # Discard every remaining Worker process so successful continuation cannot
    # be explained by an in-process LangGraph checkpoint or Workflow cache.
    await asyncio.to_thread(processes.kill, worker_b)
    await asyncio.to_thread(processes.kill, worker_c)
    if not temporal_database.is_file() or temporal_database.stat().st_size == 0:
        raise HarnessFailure("The Temporal server did not retain an on-disk SQLite database")
    server = processes.spawn("server-2", server_arguments,
                             extra_environment={"GOMAXPROCS": "2", "GOMEMLIMIT": "512MiB"})
    await connect(processes, address)
    if server.process.pid == old_server_pid:
        raise HarnessFailure("The server restart did not create a new process")
    worker_d, worker_e = worker("worker-d"), worker("worker-e")
    # Use the original clients and handles after restart, exercising reconnection.
    after_runs = [(await temporal_client.get_workflow_handle(handle.workflow_id).describe()).run_id for handle in handles]
    if before_runs != after_runs:
        raise HarnessFailure("An active Workflow Run changed identity during the server restart")
    for workflow_id in workflow_ids:
        await alice.snapshot(workflow_id)
        activities.release_gate(str(application_database), workflow_id, "after-effect")
    receipt["passed"].append("Temporal server SIGKILL and all old Workers discarded: new server PID and cold Workers retain SQLite history, active Workflow IDs/Run IDs, and original client handles")
    persist()

    results = []
    for workflow_id in workflow_ids:
        processes.check()
        results.append(await alice.result(workflow_id))
    final_database = await inspect()
    receipt["results"] = results
    receipt["database_observations"] = final_database
    receipt["carried_snapshots"] = []
    persist()
    continuations = []
    activity_types = set()
    for handle, workflow_id, before, result in zip(handles, workflow_ids, snapshots, results, strict=True):
        if result.get("can_count") != 1:
            raise HarnessFailure("The workflow did not complete exactly one Continue-as-New migration")
        if not set(before["files"]).issubset(result.get("files", {})):
            raise HarnessFailure("Continue-as-New lost a previously written virtual file")
        if len(result.get("messages", [])) < len(before["messages"]):
            raise HarnessFailure("Continue-as-New lost existing transcript messages")
        if not result.get("todos") or any(todo["status"] != "completed" for todo in result["todos"]):
            raise HarnessFailure("The migrated Deep Agents task did not complete its plan")
        if [row["effect_id"] for row in effects_for(final_database, workflow_id)] != stable_effects[workflow_id]:
            raise HarnessFailure("Task completion or Continue-as-New duplicated a stable memory effect")
        original = temporal_client.get_workflow_handle(workflow_id, run_id=handle.run_id)
        history = await original.fetch_history()
        continued = [event.workflow_execution_continued_as_new_event_attributes for event in history.events
                     if event.HasField("workflow_execution_continued_as_new_event_attributes")]
        if len(continued) != 1:
            raise HarnessFailure("The original Temporal history does not prove one Continue-as-New")
        continuation = continued[0]
        payloads = await temporal_client.data_converter.decode(continuation.input.payloads)
        carried = payloads[0]["snapshot"]
        receipt["carried_snapshots"].append({"workflow_id": workflow_id, "snapshot": carried})
        persist()
        for key in ("schema_version", "version", "identity", "files", "todos", "messages", "phase", "can_count"):
            if key not in carried:
                raise HarnessFailure("The actual Continue-as-New input omits required full Agent state")
        if carried["identity"] != before["identity"] or carried["phase"] != 1 or carried["can_count"] != 1:
            raise HarnessFailure("The Continue-as-New input changed task/owner identity or migration state")
        if carried["schema_version"] != before["schema_version"] or carried["version"] != before["version"]:
            raise HarnessFailure("The Continue-as-New input changed the Agent snapshot schema or code version")
        if carried["todos"] != before["todos"]:
            difference = first_difference(before["todos"], carried["todos"], "todos")
            raise HarnessFailure("The Continue-as-New input changed the earlier plan at " + str(difference))
        if any(result.get(key) != carried[key] for key in ("identity", "schema_version", "version")):
            raise HarnessFailure("The replacement Workflow Run changed the carried identity, schema, or code version")
        if result.get("phase") != 2 or result.get("can_count") != 1 or result.get("status") != "completed":
            raise HarnessFailure("The replacement Workflow Run did not finish the required migration phase")
        if any(carried["files"].get(path) != data for path, data in before["files"].items()):
            raise HarnessFailure("The Continue-as-New input lost the earlier file data")
        if carried["messages"][:len(before["messages"])] != before["messages"]:
            difference = first_difference(before["messages"], carried["messages"][:len(before["messages"])], "messages")
            raise HarnessFailure("The Continue-as-New input changed the earlier transcript at " + str(difference))
        if result["messages"][:len(carried["messages"])] != carried["messages"]:
            raise HarnessFailure("The new Workflow Run did not retain the carried transcript")
        if any(result["files"].get(path) != data for path, data in carried["files"].items()):
            raise HarnessFailure("The new Workflow Run did not retain the carried file data")
        if {todo["content"] for todo in carried["todos"]} - {todo["content"] for todo in result["todos"]}:
            raise HarnessFailure("The new Workflow Run lost an existing plan item")
        current = temporal_client.get_workflow_handle(workflow_id)
        current_description = await current.describe()
        if current_description.run_id != continuation.new_execution_run_id or current_description.run_id == handle.run_id:
            raise HarnessFailure("The actual Continue-as-New chain does not link the completed replacement Run")
        replacement_history = await current.fetch_history()
        for event in [*history.events, *replacement_history.events]:
            if event.HasField("activity_task_scheduled_event_attributes"):
                activity_types.add(event.activity_task_scheduled_event_attributes.activity_type.name)
        continuations.append({"workflow_id": workflow_id, "old_run_id": handle.run_id,
                              "new_run_id": continuation.new_execution_run_id,
                              "snapshot_sha256": hashlib.sha256(json.dumps(carried, sort_keys=True).encode()).hexdigest(),
                              "snapshot_keys": sorted(carried)})
    if not {"prototype_memory_put", "prototype_memory_get"}.issubset(activity_types) or len(activity_types) < 3:
        raise HarnessFailure("Temporal history does not show separate model/tool Activities inside the agent Workflow loop")
    receipt["passed"].append("cold Workflow replay and Continue-as-New finish both tasks with files/todos/messages migrated and stable effect deduplication")
    for index, workflow_id in enumerate(workflow_ids):
        try:
            await alice.start(workflow_id, "job-" + str(index), continue_as_new=True, block_after_effect=True)
        except WorkflowAlreadyStartedError:
            continue
        raise HarnessFailure("A completed Workflow ID was accepted for a duplicate execution")
    receipt["passed"].append("completed logical Workflow IDs reject repeated identical submissions after the Continue-as-New chain closes")
    receipt["initial_snapshots"] = snapshots
    receipt["results"] = results
    receipt["database_observations"] = final_database
    receipt["continue_as_new"] = continuations
    receipt["observed_activity_types"] = sorted(activity_types)
    receipt["cold_worker_pids"] = [worker_d.process.pid, worker_e.process.pid]
    receipt["server_restart"] = {"old_pid": old_server_pid, "new_pid": server.process.pid, "preserved_run_ids": before_runs}
    persist()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--temporal-binary", type=Path, required=True, help="Existing checksum-verified CLI path; no automatic download")
    parser.add_argument("--binary-sha256", help="Optional expected SHA-256 of the extracted executable")
    parser.add_argument("--output-dir", type=Path, default=MODULE.parent / "var/temporal-prototype")
    parser.add_argument("--timeout-seconds", type=float, default=MAX_SECONDS)
    args = parser.parse_args(argv)
    started = time.monotonic()
    run_id = uuid4().hex
    directory = args.output_dir.resolve() / run_id
    directory.mkdir(parents=True, mode=0o700)
    receipt_path = directory / "receipt.json"
    receipt = {"run_id": run_id, "driver_pid": os.getpid(), "status": "running", "started_at": timestamp(), "passed": [],
               "rss_budget_mib": RSS_BUDGET_KIB // 1024, "minimum_memory_free_percent": FREE_FLOOR_PERCENT,
               "production_ha_acceptance": False,
               "scope": "local SQLite Temporal server plus independent Python Workers; deterministic model and real Deep Agents harness",
               "excluded_claims": ["production cluster HA", "physical power-loss", "arbitrary external-tool exactly-once", "production owner ACL"],
               "cleanup_confirmed": False}
    persist = lambda: private_json(receipt_path, receipt, run_id)
    persist()
    processes = watcher = None
    exit_code = 1
    previous_sigterm = signal.getsignal(signal.SIGTERM)

    def interrupted(_signum, _frame):
        # A normal termination must write a terminal receipt and clean up children.
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        raise HarnessFailure("The acceptance driver received SIGTERM")

    signal.signal(signal.SIGTERM, interrupted)
    try:
        if not 10 <= args.timeout_seconds <= MAX_SECONDS:
            raise HarnessFailure("The run budget must be between 10 and 180 seconds")
        binary = args.temporal_binary.resolve()
        if not binary.is_file() or not os.access(binary, os.X_OK):
            raise HarnessFailure("An existing executable Temporal CLI must be supplied; no download was attempted")
        with binary.open("rb") as binary_stream:
            checksum = hashlib.file_digest(binary_stream, "sha256").hexdigest()
        if args.binary_sha256 is not None and checksum != args.binary_sha256:
            raise HarnessFailure("The supplied Temporal executable does not match its expected checksum")
        receipt.update(temporal_binary=str(binary), temporal_binary_sha256=checksum,
                       supplied_checksum_verified=args.binary_sha256 is not None)
        processes = Processes(directory, started + args.timeout_seconds)
        watcher = Watchdog(processes)
        watcher.start()

        async def bounded():
            async with asyncio.timeout_at(asyncio.get_running_loop().time() + max(0.1, processes.deadline - 5 - time.monotonic())):
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
        # Inputs are harness-generated fixtures, and child credential/environment
        # forwarding is disabled. Keep detailed SDK diagnostics private on disk.
        failure_log = directory / "driver-error.log"
        private_text(failure_log, traceback.format_exc())
        receipt["failure_detail_file"] = str(failure_log)
    finally:
        if processes is not None:
            receipt["cleanup_confirmed"] = processes.stop_all(force=processes.failure is not None)
            if processes.failure is not None:
                receipt.update(status="resource_blocked" if isinstance(processes.failure, ResourceBlocked) else "failed", reason=str(processes.failure))
                exit_code = 3 if isinstance(processes.failure, ResourceBlocked) else 1
            receipt["processes"] = [{"name": record.name, "pid": record.process.pid,
                                     "returncode": record.process.poll(), "log": str(record.log_path)} for record in processes.records]
            if not receipt["cleanup_confirmed"]:
                receipt.update(status="failed", reason="Cleanup of all owned process roots could not be confirmed")
                exit_code = 1
        else:
            receipt["cleanup_confirmed"] = True
        if watcher is not None:
            watcher.stop()
            receipt.update(peak_owned_tree_rss_mib=round(watcher.peak_kib / 1024, 1),
                           minimum_observed_memory_free_percent=watcher.minimum_free, resource_samples=watcher.samples)
        receipt.update(finished_at=timestamp(), seconds=round(time.monotonic() - started, 2))
        persist()
        signal.signal(signal.SIGTERM, previous_sigterm)
        print(json.dumps({"run_id": run_id, "status": receipt["status"], "receipt": str(receipt_path),
                          "seconds": receipt["seconds"], "passed": receipt["passed"]}, indent=2))
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
