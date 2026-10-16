#!/usr/bin/env python3
"""Bounded acceptance of the production Task image against owned host start-dev.

No images are pulled or built. Fixture code is a trusted readonly mount, never
a production selector. Local containers do not prove cross-host production HA.
"""
from __future__ import annotations

import argparse
import asyncio
from datetime import timedelta
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

MODULE = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(MODULE / "scripts"))
import task_tls_fault_smoke as TLS

DOCKER = TLS.load_module("task_container_support", MODULE / "tests/task_container_support.py")
HTTP, SUPPORT, ROLLOUT, FIXTURE = TLS.HTTP, TLS.SUPPORT, TLS.ROLLOUT, TLS.FIXTURE
HarnessFailure, ResourceBlocked = SUPPORT.HarnessFailure, SUPPORT.ResourceBlocked
PYTHON = "/opt/venv/bin/python"


def markers(blob, kind):
    prefix = "TASK_CONTAINER_" + kind + " "
    result = []
    for line in blob.decode("utf-8", errors="replace").splitlines():
        if line.startswith(prefix):
            value = json.loads(line[len(prefix):])
            if not isinstance(value, dict):
                raise HarnessFailure("Container fixture marker is not an object")
            result.append(value)
    return result


def check_takeover(before, after):
    for field in ("workflow_id", "run_id", "activity_id", "step", "request_sha256"):
        if before.get(field) != after.get(field):
            raise HarnessFailure("Container takeover changed native Activity identity or request at " + field)
    if (after.get("hostname") == before.get("hostname") or after.get("worker_identity") == before.get("worker_identity")
            or not before["attempt"] < after.get("attempt", 0) <= 3):
        raise HarnessFailure("Container takeover lacks a distinct Worker and bounded new attempt")


def check_container_contract(value, role, secret_files):
    config, host = value["Config"], value["HostConfig"]
    ports = host.get("PortBindings") or {}
    env_names = {entry.split("=", 1)[0] for entry in config.get("Env", [])}
    mounted = {item["Destination"] for item in value.get("Mounts", []) if item.get("Type") == "bind"}
    forbidden = ("OPENAI_API_KEY", "OPENAI_API_KEY_FILE") if role == "api" else (
        "MINILOOP_DEEPAGENTS_TOKENS", "MINILOOP_DEEPAGENTS_TOKENS_FILE",
        "MINILOOP_TASK_SERVER_CURSOR_SECRET", "MINILOOP_TASK_SERVER_CURSOR_SECRET_FILE")
    if (config.get("User") != "10001:10001" or not host.get("ReadonlyRootfs") or host.get("CapDrop") != ["ALL"]
            or not {"no-new-privileges", "no-new-privileges:true"}.intersection(host.get("SecurityOpt") or []) or host.get("Memory", 0) <= 0
            or host.get("MemorySwap") != host.get("Memory") or host.get("NanoCpus", 0) <= 0
            or not 1 <= host.get("PidsLimit", 0) <= 128
            or any(binding.get("HostIp") != "127.0.0.1" for bindings in ports.values() for binding in bindings)
            or any(name in env_names for name in forbidden)
            or any(item.get("RW") for item in value.get("Mounts", []) if item.get("Type") == "bind")
            or {path for path in mounted if path.startswith("/run/secrets/")} != set(secret_files)):
        raise HarnessFailure("A Task container violated non-root, readonly, resource or role-secret isolation")
    return {"container_id": value["Id"], "hostname": config["Hostname"], "role": role,
            "vm_pid_at_inspection": value.get("State", {}).get("Pid"), "restart_count": value.get("RestartCount", 0),
            "user": config["User"], "readonly_rootfs": host["ReadonlyRootfs"], "cap_drop": host["CapDrop"],
            "security_options": host["SecurityOpt"], "memory_limit_bytes": host["Memory"], "memory_swap_limit_bytes": host["MemorySwap"],
            "nano_cpus": host["NanoCpus"], "pids_limit": host["PidsLimit"], "tmpfs": host.get("Tmpfs"),
            "published_ports": ports, "secret_mount_destinations": sorted(secret_files),
            "all_bind_mounts_readonly": True, "opposite_role_secret_environment_absent": True}


def check_runtime_probe(value, contract):
    if (value.get("uid") != 10001 or value.get("gid") != 10001 or value.get("root_write_errno") != 30
            or value.get("tmp_writable") is not True or value.get("tmp_capacity_bytes") != 64 * DOCKER.MIB):
        raise HarnessFailure("A Task container failed actual non-root/readonly/tmp write probes")
    limits = value.get("cgroup_v2", {})
    try:
        quota, period = (int(part) for part in limits["cpu.max"].split())
        valid = (int(limits["memory.max"]) == contract["memory_limit_bytes"]
                 and int(limits["memory.swap.max"]) == 0
                 and int(limits["pids.max"]) == contract["pids_limit"]
                 and quota > 0 and period > 0 and quota * 1_000_000_000 == period * contract["nano_cpus"])
    except (KeyError, TypeError, ValueError):
        valid = False
    if not valid:
        raise HarnessFailure("A Task container did not observe its configured hard cgroup v2 limits")
    contract["runtime_probe"] = value


def check_idle_shutdown(blob, hostname, state):
    lines = blob.decode("utf-8", errors="strict").splitlines()
    expected = (f"TASK_WORKER_READY identity={hostname}:1", "TASK_WORKER_STOP_REQUESTED signal=SIGTERM",
                "TASK_WORKER_DRAINING", "TASK_WORKER_STOPPED phase=drained")
    if state.get("ExitCode") != 0 or state.get("OOMKilled") or any(line not in lines for line in expected):
        raise HarnessFailure("The production idle Worker launcher did not become ready and drain successfully on SIGTERM")
    positions = [lines.index(line) for line in expected]
    if positions != sorted(positions) or any(line.startswith("TASK_WORKER_HARD_EXIT") for line in lines):
        raise HarnessFailure("The production idle Worker did not preserve the ready-to-drain lifecycle")
    return list(expected)


SECURITY_PROBE = """import errno,json,os,pathlib
root_errno=None
try:
 f=os.open('/var/tmp/task-image-root-write',os.O_CREAT|os.O_WRONLY,0o600);os.close(f);os.unlink('/var/tmp/task-image-root-write')
except OSError as e:root_errno=e.errno
p=pathlib.Path('/tmp/task-image-write');p.write_text('probe');p.unlink()
s=os.statvfs('/tmp')
limits={name:pathlib.Path('/sys/fs/cgroup',name).read_text().strip() for name in ('memory.max','memory.swap.max','cpu.max','pids.max')}
print(json.dumps({'uid':os.getuid(),'gid':os.getgid(),'root_write_errno':root_errno,'tmp_writable':True,'tmp_capacity_bytes':s.f_blocks*s.f_frsize,'cgroup_v2':limits}))
"""


async def probe_loopback(containers, processes):
    nonce = secrets.token_hex(16).encode()
    received = asyncio.Event()
    async def accept(reader, writer):
        try:
            value = await asyncio.wait_for(reader.readexactly(len(nonce)), timeout=3)
            if value == nonce:
                received.set()
                writer.write(nonce)
                await writer.drain()
        finally:
            writer.close()
            await writer.wait_closed()
    server = await asyncio.start_server(accept, "127.0.0.1", 0)
    port = server.sockets[0].getsockname()[1]
    code = f"import socket;s=socket.create_connection(('host.docker.internal',{port}),3);s.sendall(bytes.fromhex('{nonce.hex()}'));assert s.recv(64)==bytes.fromhex('{nonce.hex()}');s.close();print('LOOPBACK_PROBE_OK')"
    try:
        probe = containers.create("tc-" + containers.run_id[:12] + "-probe", ["-c", code], {}, entrypoint=[PYTHON], transient=True)
        async def exited():
            return containers.owned(probe)["State"]
        state = await SUPPORT.eventually(processes, exited, lambda value: not value["Running"], timeout=7)
        if state["ExitCode"] != 0 or not received.is_set() or b"LOOPBACK_PROBE_OK" not in containers.logs(probe):
            raise HarnessFailure("host.docker.internal could not reach the owned 127.0.0.1 listener; public rebinding was not attempted")
        return {"container_id": probe["id"], "host_bind": "127.0.0.1", "container_target": "host.docker.internal",
                "nonce_roundtrip": True, "exit_code": state["ExitCode"]}
    finally:
        server.close()
        await server.wait_closed()


async def exercise(processes, containers, binary, directory, run_id, receipt, persist, credentials):
    from mini_loop_deepagents.task_server.service import task_id_for
    from temporalio.api.history.v1 import History
    receipt["host_loopback_probe"] = await probe_loopback(containers, processes)
    persist()
    temporal_port, *api_ports = HTTP.free_ports(3)
    host_address = f"127.0.0.1:{temporal_port}"
    container_address = f"host.docker.internal:{temporal_port}"
    queue, database = "container-" + run_id, directory / "temporal.sqlite"
    def temporal(name):
        return processes.spawn(name, [binary, "--disable-config-env", "--disable-config-file", "--log-level", "warn",
            "server", "start-dev", "--ip", "127.0.0.1", "--port", temporal_port, "--headless", "--db-filename", database],
            extra_environment={"GOMAXPROCS": "2", "GOMEMLIMIT": "512MiB"})
    temporal_process = temporal("temporal-1")
    client = await SUPPORT.connect(processes, host_address)
    secret_dir, gates = directory / "secrets", directory / "gates"
    secret_dir.mkdir(mode=0o700)
    gates.mkdir(mode=0o755)
    credentials.update(http_token=secrets.token_urlsafe(32), cursor_secret=secrets.token_urlsafe(32),
                       model_key="synthetic-container-model-" + secrets.token_urlsafe(32))
    for name, content in (("tokens.json", json.dumps({credentials["http_token"]: "alice"})),
                          ("cursor.txt", credentials["cursor_secret"]), ("model.txt", credentials["model_key"])):
        path = secret_dir / name
        SUPPORT.private_text(path, content + "\n")
        # The host parent remains 0700. A single file readonly mount must be
        # readable by the image's unrelated, non-root UID 10001.
        path.chmod(0o444)
    idle_name = f"tc-{run_id[:12]}-production-worker"
    idle = containers.create(idle_name, ["worker", "--address", container_address, "--task-queue", queue + "-empty"],
        {"OPENAI_API_KEY_FILE": "/run/secrets/model.txt", "LANGSMITH_TRACING": "false"},
        [(secret_dir / "model.txt", "/run/secrets/model.txt")], worker=True)
    idle_inspect = containers.owned(idle)
    if idle_inspect["Config"].get("Entrypoint") != [PYTHON, "-m", "mini_loop_deepagents.task_server.launch"]:
        raise HarnessFailure("The idle Worker did not use the image's default production entrypoint")
    async def idle_ready():
        return containers.logs(idle)
    await SUPPORT.eventually(processes, idle_ready, lambda data: f"TASK_WORKER_READY identity={idle_name}:1".encode() in data)
    idle_contract = check_container_contract(idle_inspect, "worker", ["/run/secrets/model.txt"])
    check_runtime_probe(json.loads(containers.engine.exec(idle["id"], [PYTHON, "-c", SECURITY_PROBE])), idle_contract)
    receipt["production_idle_worker"] = {"container_id": idle["id"], "default_entrypoint": idle_inspect["Config"]["Entrypoint"],
        "default_identity": idle_name + ":1", "isolated_empty_queue": queue + "-empty", "tasks_submitted": 0,
        "provider_invoked": False, "docker_stop_grace_seconds": 20, "security_contract": idle_contract}
    persist()
    stop_started = time.monotonic()
    stopped = containers.stop(idle, grace_seconds=20)
    receipt["production_idle_worker"].update(exit_code=stopped["State"]["ExitCode"],
        oom_killed=stopped["State"].get("OOMKilled", False), stop_seconds=round(time.monotonic() - stop_started, 3))
    persist()
    lifecycle = check_idle_shutdown(containers.logs(idle), idle_name, stopped["State"])
    receipt["production_idle_worker"]["lifecycle_markers"] = lifecycle
    receipt["passed"].append("the default production Worker entrypoint becomes ready with hostname:PID identity on an empty queue, then docker stop delivers SIGTERM and exits 0 without invoking a model provider")
    persist()
    counter = {"api": 0, "worker": 0}
    contracts = []
    def create_api(index):
        counter["api"] += 1
        name = f"tc-{run_id[:12]}-api-{counter['api']}"
        env = {"MINILOOP_DEEPAGENTS_TOKENS_FILE": "/run/secrets/tokens.json", "MINILOOP_TASK_SERVER_CURSOR_SECRET_FILE": "/run/secrets/cursor.txt",
               "LANGSMITH_TRACING": "false"}
        mounts = [(secret_dir / name, "/run/secrets/" + name) for name in ("tokens.json", "cursor.txt")]
        row = containers.create(name, ["api", "--address", "0.0.0.0:8123", "--temporal-address", container_address,
            "--task-queue", queue], env, mounts, port=api_ports[index])
        contracts.append(check_container_contract(containers.owned(row), "api", ["/run/secrets/tokens.json", "/run/secrets/cursor.txt"]))
        return row
    def create_worker():
        counter["worker"] += 1
        name = f"tc-{run_id[:12]}-worker-{counter['worker']}"
        mounts = [(secret_dir / "model.txt", "/run/secrets/model.txt"), (gates, "/opt/task-gates"),
                  (MODULE / "tests/task_container_worker_fixture.py", "/opt/task-fixture/task_container_worker_fixture.py"),
                  (MODULE / "tests/task_rollout_worker_fixture.py", "/opt/task-fixture/task_rollout_worker_fixture.py")]
        row = containers.create(name, ["/opt/task-fixture/task_container_worker_fixture.py", "--address", container_address,
            "--task-queue", queue, "--gate-directory", "/opt/task-gates"],
            {"OPENAI_API_KEY_FILE": "/run/secrets/model.txt", "LANGSMITH_TRACING": "false"}, mounts, worker=True, entrypoint=[PYTHON])
        contracts.append(check_container_contract(containers.owned(row), "worker", ["/run/secrets/model.txt"]))
        return row
    apis = [create_api(index) for index in range(2)]
    workers = [create_worker() for _ in range(2)]
    urls = [f"http://127.0.0.1:{port}" for port in api_ports]
    headers = {"Authorization": "Bearer " + credentials["http_token"]}
    records = []
    async with httpx.AsyncClient(timeout=8, trust_env=False, follow_redirects=False) as http:
        async def ready(url, code):
            async def get():
                try:
                    return await http.get(url + "/readyz")
                except httpx.TransportError:
                    return None
            response = await SUPPORT.eventually(processes, get, lambda value: value is not None and value.status_code == code, timeout=25)
            body = HTTP.decode(response, code)
            if body != {"status": "ready" if code == 200 else "not_ready"} or response.headers.get("cache-control") != "no-store":
                raise HarnessFailure("Container readiness returned the wrong dependency state or cache policy")
        for url in urls:
            await HTTP.ready(processes, http, url)
            await ready(url, 200)
        for api in apis:
            value = json.loads(containers.engine.exec(api["id"], [PYTHON, "-c", SECURITY_PROBE]))
            check_runtime_probe(value, next(item for item in contracts if item["container_id"] == api["id"]))
        async def worker_gates():
            return [item for row in workers for item in markers(containers.logs(row), "MODEL_GATE")]
        for index, url in enumerate(urls):
            key = f"container-{run_id}-{index}"
            message = f"[rollout] container acceptance {run_id} {index}"
            value = HTTP.decode(await http.post(url + "/v1/tasks", headers={**headers, "Idempotency-Key": key}, json={"message": message}), 202)
            task_id = task_id_for("alice", key)
            if value.get("task_id") != task_id or value.get("reused") is not False or value.get("version") != "task-v2":
                raise HarnessFailure("A container API did not create a new real V2 Task")
            records.append({"task_id": task_id, "owner": "alice", "first_execution_run_id": value["first_execution_run_id"], "key": key, "message": message})
        initial = await SUPPORT.eventually(processes, worker_gates, lambda values: len({row["hostname"] for row in values}) == 2)
        if {row["hostname"] for row in initial} != {row["name"] for row in workers}:
            raise HarnessFailure("Two simultaneous gates did not run on the two independent container Workers")
        before = [await client.get_workflow_handle(row["task_id"]).query("snapshot", rpc_timeout=timedelta(seconds=5)) for row in records]
        for worker in workers:
            security = markers(containers.logs(worker), "SECURITY")
            if len(security) != 1 or security[0]["hostname"] != worker["name"]:
                raise HarnessFailure("Worker secret loading and filesystem security were not actually observed")
            check_runtime_probe(security[0], next(item for item in contracts if item["container_id"] == worker["id"]))
        receipt.update(task_queue=queue, simultaneous_gates=initial, replacements=[], container_contracts=contracts)
        receipt["passed"].append("the image reaches host loopback; two isolated production APIs and two non-root readonly Worker containers execute simultaneous real Activity gates with disjoint secret mounts")
        persist()
        for index, old in enumerate(list(apis)):
            stopped = containers.stop(old, kill=True)
            if stopped["State"]["ExitCode"] != 137 or stopped["State"].get("OOMKilled"):
                raise HarnessFailure("The intended API container SIGKILL was not observed")
            replacement = create_api(index)
            apis[index] = replacement
            await ready(urls[index], 200)
            check_runtime_probe(json.loads(containers.engine.exec(replacement["id"], [PYTHON, "-c", SECURITY_PROBE])),
                                next(item for item in contracts if item["container_id"] == replacement["id"]))
            record = records[index]
            duplicate = HTTP.decode(await http.post(urls[index] + "/v1/tasks", headers={**headers, "Idempotency-Key": record["key"]},
                                                    json={"message": record["message"]}), 202)
            if duplicate.get("reused") is not True or duplicate.get("first_execution_run_id") != record["first_execution_run_id"]:
                raise HarnessFailure("An API container replacement duplicated or replaced an existing Task")
            receipt["replacements"].append({"role": "api", "before_container_id": old["id"], "after_container_id": replacement["id"],
                                            "signal": "SIGKILL", "exit_code": 137, "same_key_reused": True})
        for index, old in enumerate(list(workers)):
            original = next(row for row in initial if row["hostname"] == old["name"])
            stopped = containers.stop(old, kill=True)
            if stopped["State"]["ExitCode"] != 137 or stopped["State"].get("OOMKilled"):
                raise HarnessFailure("The intended Worker container SIGKILL was not observed")
            replacement = create_worker()
            workers[index] = replacement
            async def taken():
                return next((row for row in markers(containers.logs(replacement), "MODEL_GATE") if row["workflow_id"] == original["workflow_id"]), None)
            after = await SUPPORT.eventually(processes, taken, lambda row: row is not None, timeout=25)
            check_takeover(original, after)
            security = markers(containers.logs(replacement), "SECURITY")
            if len(security) != 1 or security[0]["hostname"] != replacement["name"]:
                raise HarnessFailure("Replacement Worker security was not actually observed")
            check_runtime_probe(security[0], next(item for item in contracts if item["container_id"] == replacement["id"]))
            receipt["replacements"].append({"role": "worker", "before_container_id": old["id"], "after_container_id": replacement["id"],
                                            "signal": "SIGKILL", "exit_code": 137, "before_activity": original, "after_activity": after})
            persist()
        snapshots = []
        for record, original in zip(records, before, strict=True):
            recovered = await client.get_workflow_handle(record["task_id"]).query("snapshot", rpc_timeout=timedelta(seconds=5))
            TLS.check_snapshot_preserved(original, recovered)
            path = directory / (record["task_id"] + ".snapshots.json")
            SUPPORT.private_json(path, {"before": original, "after": recovered}, run_id)
            snapshots.append({"task_id": record["task_id"], "file": str(path), "file_sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
                              "full_native_state_equal": True})
            (gates / (FIXTURE.task_stem(record["task_id"]) + ".release")).touch(mode=0o444)
        receipt["snapshots"] = snapshots
        receipt["passed"].append("both APIs and both Workers are recreated with distinct container IDs; original Tasks and full native state survive, same-key submissions reuse existing Runs, and model Activity retries retain request SHA")
        completed, results = [], []
        for index, record in enumerate(records):
            async def result():
                return await http.get(urls[index] + f"/v1/tasks/{record['task_id']}/result", headers=headers)
            response = await SUPPORT.eventually(processes, result, lambda value: value.status_code == 200, timeout=30)
            value = HTTP.decode(response, 200)
            completed.append(ROLLOUT.check_completed(value, record))
            results.append(value)
            SUPPORT.private_json(directory / (record["task_id"] + ".result.json"), value, run_id)
        # A dependency-only outage after completion avoids consuming another
        # retry on the already faulted model Activities.
        processes.kill(temporal_process)
        for url in urls:
            await ready(url, 503)
            HTTP.decode(await http.get(url + "/healthz"), 200)
        if any(not containers.owned(row)["State"]["Running"] for row in apis):
            raise HarnessFailure("Dependency failure terminated an API container")
        second_temporal = temporal("temporal-2")
        await SUPPORT.connect(processes, host_address)
        for url in urls:
            await ready(url, 200)
        for index, record in enumerate(records):
            recovered = HTTP.decode(await http.get(urls[index] + f"/v1/tasks/{record['task_id']}/result", headers=headers), 200)
            TLS.check_snapshot_preserved(results[index], recovered)
        receipt["readiness_dependency_outage"] = {"first_temporal_pid": temporal_process.process.pid, "first_temporal_exit": temporal_process.process.returncode,
            "replacement_temporal_pid": second_temporal.process.pid, "same_sqlite_file": True, "during_outage_readyz": 503,
            "during_outage_healthz": 200, "after_reconnect_readyz": 200, "api_container_ids_preserved": [row["id"] for row in apis],
            "completed_results_unchanged": True}
        needles, chains, history_scan = TLS.secret_needles(credentials), [], []
        for record, result in zip(records, results, strict=True):
            first = await client.get_workflow_handle(record["task_id"], run_id=record["first_execution_run_id"]).fetch_history()
            continued = [event.workflow_execution_continued_as_new_event_attributes for event in first.events if event.HasField("workflow_execution_continued_as_new_event_attributes")]
            if len(continued) != 1 or continued[0].new_execution_run_id != result["run_id"]:
                raise HarnessFailure("A container Task lacks its real native continuation chain")
            final = await client.get_workflow_handle(record["task_id"], run_id=result["run_id"]).fetch_history()
            if any(event.HasField("workflow_task_failed_event_attributes") for history in (first, final) for event in history.events):
                raise HarnessFailure("A container Task recorded WorkflowTaskFailed")
            for index, history in enumerate((first, final)):
                blob = History(events=history.events).SerializeToString()
                TLS.check_no_secrets(blob, needles, "native-history")
                TLS.check_no_secrets(history.to_json().encode(), needles, "native-history-json")
                history_scan.append({"task_id": record["task_id"], "run_id": (record["first_execution_run_id"], result["run_id"])[index],
                                     "sha256": hashlib.sha256(blob).hexdigest(), "event_count": len(history.events), "credential_matches": 0})
            chains.append({"task_id": record["task_id"], "first_execution_run_id": record["first_execution_run_id"], "final_run_id": result["run_id"],
                           "workflow_task_failures": 0, "continuation_payload_sha256": hashlib.sha256(continued[0].input.SerializeToString()).hexdigest()})
        receipt.update(completed_tasks=completed, native_run_chains=chains, history_secret_scan=history_scan,
                       container_contracts=contracts)
        receipt["passed"].append("both Tasks finish ten native tools and one CAN; real readiness changes 200→503→200 around Temporal restart while liveness remains 200, original API containers survive, and completed native results remain identical")
        persist()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", required=True, help="Existing locally built Task image; never pulled")
    parser.add_argument("--temporal-binary", type=Path, required=True)
    parser.add_argument("--binary-sha256", required=True)
    parser.add_argument("--output-dir", type=Path, default=MODULE.parent / "var/task-container-prototype")
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
               "native_temporal_tls_acceptance": False, "scope": "production Task image with readonly trusted test model against owned host CLI start-dev"}
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
        engine = DOCKER.Engine()
        image = engine.request("GET", "/images/" + DOCKER.quote(args.image, safe="") + "/json")
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
