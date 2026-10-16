#!/usr/bin/env python3
"""Bounded real-SDK mTLS reconnect acceptance through a test TLS terminator.

The loopback backend is plaintext start-dev. This does not validate native
Temporal server TLS, Cloud API-key authorization, production HA or capacity.
"""
from __future__ import annotations

import argparse
import asyncio
from dataclasses import replace
from datetime import timedelta
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import secrets
import signal
import ssl
import sys
import time
import traceback
from uuid import uuid4

import httpx

MODULE = Path(__file__).resolve().parents[1]


def load_module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


ROLLOUT = load_module("task_tls_rollout_support", MODULE / "scripts/task_worker_rollout_smoke.py")
HTTP, FIXTURE, SUPPORT = ROLLOUT.HTTP, ROLLOUT.FIXTURE, ROLLOUT.SUPPORT
HarnessFailure, ResourceBlocked = SUPPORT.HarnessFailure, SUPPORT.ResourceBlocked
SERVER_NAME = "temporal.task-server.test"
ROLES = ("api-1", "api-2", "worker-1", "worker-2", "observer")
PREFIX = "MINILOOP_TASK_TEMPORAL_"


def certificate_fingerprint(path):
    return hashlib.sha256(ssl.PEM_cert_to_DER_cert(path.read_text())).hexdigest()


def connection_environment(directory, role):
    if role not in ROLES:
        raise HarnessFailure("Unknown TLS fixture transport role")
    return {PREFIX + "TLS": "true", PREFIX + "CA_CERT_FILE": str(directory / "trusted-ca.crt"),
            PREFIX + "CLIENT_CERT_FILE": str(directory / f"{role}.crt"),
            PREFIX + "CLIENT_KEY_FILE": str(directory / f"{role}.key"),
            PREFIX + "SERVER_NAME": SERVER_NAME, PREFIX + "API_KEY_FILE": str(directory / "api-key.txt")}


def secret_needles(secrets_by_name):
    """Match complete/JSON-escaped values and long PEM body lines, without output."""
    needles = {}
    for label, value in secrets_by_name.items():
        raw = value.encode() if isinstance(value, str) else value
        if not raw:
            raise HarnessFailure("The secret exclusion scan received an empty value")
        needles[label + ":raw"] = raw
        needles[label + ":json"] = json.dumps(raw.decode("ascii"))[1:-1].encode()
        if b"-----BEGIN " in raw:
            for index, line in enumerate(raw.splitlines()):
                if len(line) >= 48 and not line.startswith(b"-----"):
                    needles[f"{label}:pem-line-{index}"] = line
    return needles


def check_no_secrets(blob, needles, artifact):
    for label, value in needles.items():
        if value in blob:
            raise HarnessFailure(f"Credential exclusion failed in {artifact} ({label})")


def audit_connections(path):
    if not path.is_file():
        return []
    rows = json.loads(path.read_text())["connections"]
    if any(row.get("alpn") != "h2" or row.get("tls_version") not in ("TLSv1.2", "TLSv1.3") for row in rows):
        raise HarnessFailure("The TLS tunnel accepted a connection outside verified mTLS and HTTP/2")
    return rows


def check_snapshot_preserved(before, after):
    difference = SUPPORT.first_difference(before, after, "snapshot")
    if difference:
        raise HarnessFailure("Transport restart changed the complete gated native state at " + difference)


def summarize_activity_attempts(directory):
    """Bind retries to the same native identity and serialized model request."""
    groups = {}
    for path in sorted(directory.glob("*.started.json")):
        row = json.loads(path.read_text())
        identity = (row["workflow_id"], row["run_id"], row["activity_id"])
        groups.setdefault(identity, []).append(row)
    if not groups:
        raise HarnessFailure("No real model Activity attempts were recorded")
    evidence = []
    for identity, rows in sorted(groups.items()):
        rows.sort(key=lambda row: row["attempt"])
        attempts = [row["attempt"] for row in rows]
        if attempts != list(range(1, len(rows) + 1)) or attempts[-1] > 3:
            raise HarnessFailure("A model Activity lost attempt evidence or exceeded its retry budget")
        if len({(row["step"], row["request_sha256"]) for row in rows}) != 1:
            raise HarnessFailure("A retried native Activity changed its serialized model request")
        evidence.append({"workflow_id": identity[0], "run_id": identity[1], "activity_id": identity[2],
                         "step": rows[0]["step"], "request_sha256": rows[0]["request_sha256"],
                         "attempts": [{"attempt": row["attempt"], "pid": row["pid"],
                                       "started_monotonic_ns": row["started_monotonic_ns"]} for row in rows]})
    return {"model_activity_attempt_count": sum(len(row["attempts"]) for row in evidence),
            "maximum_activity_attempt": max(row["attempts"][-1]["attempt"] for row in evidence),
            "retried_activity_count": sum(len(row["attempts"]) > 1 for row in evidence), "activities": evidence,
            "source_files": [{"name": path.name, "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
                             for path in sorted(directory.glob("*.started.json"))]}


async def command(processes, name, arguments):
    record = processes.spawn(name, arguments)
    async def finished():
        return record.process.poll()
    result = await SUPPORT.eventually(processes, finished, lambda code: code is not None, timeout=8)
    if result != 0:
        raise HarnessFailure("A local test certificate command failed: " + name)
    return record


async def certificates(processes, openssl, directory):
    directory.mkdir(mode=0o700)
    for authority in ("trusted-ca", "untrusted-ca"):
        await command(processes, authority, [openssl, "req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256",
            "-noenc", "-keyout", directory / f"{authority}.key", "-out", directory / f"{authority}.crt", "-days", "1",
            "-subj", "/CN=task-tls-" + authority, "-addext", "basicConstraints=critical,CA:TRUE",
            "-addext", "keyUsage=critical,keyCertSign,cRLSign"])
    for serial, name in enumerate(("server", *ROLES, "untrusted-client"), start=2):
        extension = "basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage="
        extension += "serverAuth\nsubjectAltName=DNS:" + SERVER_NAME + "\n" if name == "server" else "clientAuth\n"
        SUPPORT.private_text(directory / f"{name}.ext", extension)
        await command(processes, name + "-request", [openssl, "req", "-new", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256",
            "-noenc", "-keyout", directory / f"{name}.key", "-out", directory / f"{name}.csr", "-subj", "/CN=task-tls-" + name])
        authority = "untrusted-ca" if name == "untrusted-client" else "trusted-ca"
        await command(processes, name + "-certificate", [openssl, "x509", "-req", "-in", directory / f"{name}.csr",
            "-CA", directory / f"{authority}.crt", "-CAkey", directory / f"{authority}.key", "-set_serial", str(serial),
            "-days", "1", "-extfile", directory / f"{name}.ext", "-out", directory / f"{name}.crt"])
    for path in directory.iterdir():
        path.chmod(0o600)
    SUPPORT.private_text(directory / "api-key.txt", "synthetic-task-tls-" + secrets.token_urlsafe(32) + "\n")


async def native_health(client):
    from temporalio.api.workflowservice.v1 import GetSystemInfoRequest
    await client.workflow_service.get_system_info(GetSystemInfoRequest(), retry=False, timeout=timedelta(seconds=3))


async def negative_controls(processes, address, settings, certs, audit, receipt, persist):
    from mini_loop_deepagents.task_server.connection import connect_temporal, TemporalConnectionError
    cases = {
        "wrong_server_ca": replace(settings, server_root_ca_cert=(certs / "untrusted-ca.crt").read_bytes()),
        "wrong_server_name": replace(settings, server_name="wrong.task-server.test"),
        "missing_client_identity": replace(settings, client_cert=None, client_private_key=None),
        "untrusted_client_identity": replace(settings, client_cert=(certs / "untrusted-client.crt").read_bytes(),
                                             client_private_key=(certs / "untrusted-client.key").read_bytes()),
    }
    controls = []
    for name, invalid in cases.items():
        processes.check()
        positive = await connect_temporal(address, connection=settings)
        await native_health(positive)
        before = len(audit_connections(audit))
        started = time.monotonic()
        try:
            await connect_temporal(address, connection=invalid)
        except TemporalConnectionError as error:
            if str(error) != "temporal_connection_failed":
                raise HarnessFailure("A negative TLS control timed out instead of proving rejection") from None
            failure_code = str(error)
        else:
            raise HarnessFailure("A negative TLS control unexpectedly connected: " + name)
        denied = len(audit_connections(audit))
        if denied != before:
            raise HarnessFailure("A negative TLS control reached the plaintext backend: " + name)
        positive = await connect_temporal(address, connection=settings)
        await native_health(positive)
        after = len(audit_connections(audit))
        if after <= denied:
            raise HarnessFailure("A healthy control did not establish a fresh verified transport")
        controls.append({"case": name, "result": "rejected", "error_code": failure_code,
                         "healthy_rpc_before": True, "healthy_rpc_after": True,
                         "accepted_connections_before": before, "accepted_connections_after_rejection": denied,
                         "accepted_connections_after_healthy": after, "seconds": round(time.monotonic() - started, 3)})
        receipt["negative_controls"] = controls
        persist()


async def exercise(processes, binary, openssl, directory, run_id, receipt, persist, credential_values):
    from mini_loop_deepagents.task_server.connection import TemporalConnectionSettings, connect_temporal
    from mini_loop_deepagents.task_server.service import task_id_for
    from temporalio.api.history.v1 import History
    ports = HTTP.free_ports(4)
    backend, tls_port, *api_ports = ports
    address, queue = f"127.0.0.1:{tls_port}", "tls-" + run_id
    certs, barriers = directory / "certificates", directory / "barriers"
    barriers.mkdir(mode=0o700)
    await certificates(processes, openssl, certs)
    credential_values.update({path.name: path.read_bytes() for path in certs.iterdir() if path.suffix in (".key", ".crt")})
    credential_values["synthetic_temporal_api_key"] = (certs / "api-key.txt").read_bytes().rstrip(b"\n")
    tokens = {"alice": secrets.token_urlsafe(32)}
    credential_values["http_bearer"] = tokens["alice"]
    credential_values["cursor_secret"] = secrets.token_urlsafe(32)
    environment = {"MINILOOP_DEEPAGENTS_TOKENS": json.dumps({token: owner for owner, token in tokens.items()}),
                   "MINILOOP_TASK_SERVER_CURSOR_SECRET": credential_values["cursor_secret"]}
    processes.spawn("temporal", [binary, "--disable-config-env", "--disable-config-file", "--log-level", "warn",
        "server", "start-dev", "--ip", "127.0.0.1", "--port", str(backend), "--headless", "--db-filename", directory / "temporal.sqlite"],
        extra_environment={"GOMAXPROCS": "2", "GOMEMLIMIT": "512MiB"})
    async def backend_ready():
        try:
            _, writer = await asyncio.open_connection("127.0.0.1", backend)
            writer.close()
            await writer.wait_closed()
            return True
        except OSError:
            return False
    await SUPPORT.eventually(processes, backend_ready, bool)
    def proxy(index):
        audit = directory / f"tls-audit-{index}.json"
        process = processes.spawn(f"tls-proxy-{index}", [sys.executable, MODULE / "tests/task_tls_tunnel.py", "--port", tls_port,
            "--backend-port", backend, "--certificate", certs / "server.crt", "--private-key", certs / "server.key",
            "--client-ca", certs / "trusted-ca.crt", "--audit", audit])
        return process, audit
    async def proxy_ready(process):
        async def log():
            return process.log_path.read_text()
        await SUPPORT.eventually(processes, log, lambda value: "TASK_TLS_TUNNEL_READY" in value)
    original_proxy, original_audit = proxy(1)
    await proxy_ready(original_proxy)
    settings = TemporalConnectionSettings.from_env(connection_environment(certs, "observer"))
    await negative_controls(processes, address, settings, certs, original_audit, receipt, persist)
    receipt["passed"].append("real SDK refuses wrong CA, wrong DNS name, absent client identity and untrusted client identity, each between successful fresh verified RPC controls")
    client = await connect_temporal(address, connection=settings, identity="tls-observer")
    await native_health(client)
    peers = {role: certificate_fingerprint(certs / f"{role}.crt") for role in ROLES}
    if len(set(peers.values())) != 5:
        raise HarnessFailure("The five transport roles do not have independent client certificates")
    workers, apis = [], []
    for index in range(2):
        role = f"worker-{index + 1}"
        workers.append(processes.spawn(role, [sys.executable, MODULE / "tests/task_tls_worker_fixture.py", "--address", address,
            "--task-queue", queue, "--identity", "tls-" + role, "--barrier-directory", barriers],
            extra_environment=connection_environment(certs, role)))
        role = f"api-{index + 1}"
        apis.append(processes.spawn(role, [sys.executable, "-m", "mini_loop_deepagents.task_server.api", "--address",
            f"127.0.0.1:{api_ports[index]}", "--temporal-address", address, "--task-queue", queue],
            extra_environment={**environment, **connection_environment(certs, role)}))
    receipt.update(task_queue=queue, roles=peers, api_replica_count=2, worker_count=2,
                   synthetic_api_key_metadata_only=True, tls_backend="loopback plaintext start-dev")
    records = []
    headers = {"Authorization": "Bearer " + tokens["alice"]}
    urls = [f"http://127.0.0.1:{port}" for port in api_ports]
    async with httpx.AsyncClient(timeout=8, trust_env=False, follow_redirects=False) as http:
        for url in urls:
            await HTTP.ready(processes, http, url)
        for index, url in enumerate(urls):
            key = f"tls-{run_id}-{index}"
            task_id = task_id_for("alice", key)
            result = HTTP.decode(await http.post(url + "/v1/tasks", headers={**headers, "Idempotency-Key": key},
                                                json={"message": f"[rollout] TLS {run_id} {index}"}), 202)
            if result.get("task_id") != task_id or result.get("version") != "task-v2" or result.get("reused") is not False:
                raise HarnessFailure("The TLS APIs did not create two fresh production V2 Tasks")
            records.append({"owner": "alice", "task_id": task_id, "first_execution_run_id": result["first_execution_run_id"]})
        async def blocked():
            return [ROLLOUT.read_marker(barriers, record["task_id"]) for record in records]
        before_markers = await SUPPORT.eventually(processes, blocked, lambda values: all(value is not None for value in values))
        if {row["pid"] for row in before_markers} != {row.process.pid for row in workers}:
            raise HarnessFailure("Two gated native Activities did not occupy both independent TLS Workers")
        if not set(peers.values()).issubset({row["client_certificate_sha256"] for row in audit_connections(original_audit)}):
            raise HarnessFailure("The initial tunnel did not authenticate all five distinct client roles")
        before = [await client.get_workflow_handle(record["task_id"]).query("snapshot", rpc_timeout=timedelta(seconds=5)) for record in records]
        for record, snapshot in zip(records, before, strict=True):
            HTTP.check_snapshot_contract(snapshot, record["task_id"], record["first_execution_run_id"], "task-v2", "running")
        killed_at = time.monotonic_ns()
        processes.kill(original_proxy)
        unavailable = HTTP.decode(await http.get(urls[0] + f"/v1/tasks/{records[0]['task_id']}", headers=headers), 503)
        replacement, replacement_audit = proxy(2)
        await proxy_ready(replacement)
        async def reconnected():
            processes.check()
            for url, record in zip(urls, records, strict=True):
                response = await http.get(url + f"/v1/tasks/{record['task_id']}", headers=headers)
                if response.status_code == 503:
                    return False
                value = HTTP.decode(response, 200)
                if value.get("first_execution_run_id") != record["first_execution_run_id"]:
                    raise HarnessFailure("A reconnected API replaced a Task's original Run chain")
            await native_health(client)
            return set(peers.values()).issubset({row["client_certificate_sha256"] for row in audit_connections(replacement_audit)})
        await SUPPORT.eventually(processes, reconnected, bool, timeout=30)
        if any(row.process.poll() is not None for row in (*apis, *workers)):
            raise HarnessFailure("An original API or Worker process exited during the transport-only fault")
        after = [await client.get_workflow_handle(record["task_id"]).query("snapshot", rpc_timeout=timedelta(seconds=5)) for record in records]
        snapshots = []
        for record, initial, recovered in zip(records, before, after, strict=True):
            check_snapshot_preserved(initial, recovered)
            path = directory / f"{record['task_id']}.snapshots.json"
            SUPPORT.private_json(path, {"before": initial, "after": recovered}, run_id)
            snapshots.append({"task_id": record["task_id"], "file": str(path), "file_sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
                              "snapshot_sha256": hashlib.sha256(FIXTURE.canonical(initial)).hexdigest(), "full_native_state_equal": True})
        receipt["proxy_restart"] = {"signal": "SIGKILL", "before_pid": original_proxy.process.pid, "before_returncode": original_proxy.process.returncode,
            "replacement_pid": replacement.process.pid, "same_listen_port": True, "outage_http_status": 503,
            "outage_error_code": unavailable.get("error"), "api_worker_pids_preserved": [row.process.pid for row in (*apis, *workers)],
            "reconnect_seconds": round((time.monotonic_ns() - killed_at) / 1e9, 3), "before_activities": before_markers,
            "after_activities": await blocked(), "snapshots": snapshots}
        receipt["passed"].append("SIGKILL and same-port restart of the mTLS proxy yields real HTTP 503 then reconnects both original APIs, both original Workers and observer; complete gated native state and Task identity remain equal")
        persist()
        for record in records:
            ROLLOUT.touch_gate(barriers, record["task_id"], "release")
        completed, chains, history_artifacts = [], [], []
        needles = secret_needles(credential_values)
        for index, record in enumerate(records):
            async def result():
                return await http.get(urls[index] + f"/v1/tasks/{record['task_id']}/result", headers=headers)
            response = await SUPPORT.eventually(processes, result, lambda value: value.status_code == 200, timeout=35)
            value = HTTP.decode(response, 200)
            completed.append(ROLLOUT.check_completed(value, record))
            SUPPORT.private_json(directory / f"{record['task_id']}.result.json", value, run_id)
            first = await client.get_workflow_handle(record["task_id"], run_id=record["first_execution_run_id"]).fetch_history()
            can_events = [event.workflow_execution_continued_as_new_event_attributes for event in first.events
                          if event.HasField("workflow_execution_continued_as_new_event_attributes")]
            if len(can_events) != 1 or can_events[0].new_execution_run_id != value["run_id"]:
                raise HarnessFailure("A TLS Task lacks its native Continue-As-New transition")
            final = await client.get_workflow_handle(record["task_id"], run_id=value["run_id"]).fetch_history()
            failures = sum(event.HasField("workflow_task_failed_event_attributes") for history in (first, final) for event in history.events)
            if failures:
                raise HarnessFailure("A TLS Task recorded a WorkflowTaskFailed event")
            chains.append({"task_id": record["task_id"], "first_execution_run_id": record["first_execution_run_id"],
                           "final_run_id": value["run_id"], "workflow_task_failures": failures,
                           "continuation_payload_sha256": hashlib.sha256(can_events[0].input.SerializeToString()).hexdigest()})
            for position, history in enumerate((first, final)):
                raw = History(events=history.events).SerializeToString()
                check_no_secrets(raw, needles, "native-history")
                check_no_secrets(history.to_json().encode(), needles, "native-history-json")
                history_artifacts.append({"task_id": record["task_id"], "run_id": (record["first_execution_run_id"], value["run_id"])[position],
                                          "sha256": hashlib.sha256(raw).hexdigest(), "event_count": len(history.events), "credential_matches": 0})
        attempts = summarize_activity_attempts(barriers)
        receipt.update(completed_tasks=completed, native_run_chains=chains, history_secret_scan=history_artifacts,
                       maximum_activity_attempt=attempts["maximum_activity_attempt"],
                       model_activity_attempt_count=attempts["model_activity_attempt_count"], activity_attempts=attempts,
                       tunnel_audits=[{"file": str(path), "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
                                       "connections": audit_connections(path)} for path in (original_audit, replacement_audit)])
        receipt["passed"].append("both real V2 Tasks complete ten native tools and one native CAN each after reconnect, within three Activity attempts; four native Run Histories contain none of the test credentials")
        persist()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--temporal-binary", type=Path, required=True)
    parser.add_argument("--binary-sha256")
    parser.add_argument("--openssl-binary", type=Path, default=Path("/opt/homebrew/opt/openssl@3/bin/openssl"))
    parser.add_argument("--output-dir", type=Path, default=MODULE.parent / "var/task-tls-prototype")
    parser.add_argument("--timeout-seconds", type=float, default=180)
    args = parser.parse_args(argv)
    started, run_id = time.monotonic(), uuid4().hex
    directory = args.output_dir.resolve() / run_id
    directory.mkdir(parents=True, mode=0o700)
    receipt_path = directory / "receipt.json"
    receipt = {"run_id": run_id, "driver_pid": os.getpid(), "status": "running", "started_at": SUPPORT.timestamp(),
        "passed": [], "cleanup_confirmed": False, "rss_budget_mib": 2048, "minimum_memory_free_percent": 15,
        "production_ha_acceptance": False, "production_capacity_acceptance": False,
        "native_temporal_tls_acceptance": False, "cloud_api_key_acl_acceptance": False,
        "scope": "two real V2 Tasks, two production APIs and two production Workers reconnect through test-only mTLS termination",
        "excluded_claims": ["native Temporal server TLS", "Temporal Cloud API-key ACL", "production ingress", "certificate rotation", "production HA or throughput", "external-effect exactly-once"]}
    persist = lambda: SUPPORT.private_json(receipt_path, receipt, run_id)
    persist()
    processes = watcher = None
    credential_values = {}
    exit_code = 1
    previous_sigterm = signal.getsignal(signal.SIGTERM)
    def interrupted(_signum, _frame):
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        raise HarnessFailure("The TLS acceptance driver received SIGTERM")
    signal.signal(signal.SIGTERM, interrupted)
    try:
        if not 10 <= args.timeout_seconds <= 180:
            raise HarnessFailure("The overall run budget must be between 10 and 180 seconds")
        binary, openssl = args.temporal_binary.resolve(), args.openssl_binary.resolve()
        for name, path in (("Temporal", binary), ("OpenSSL", openssl)):
            if not path.is_file() or not os.access(path, os.X_OK):
                raise HarnessFailure("An existing " + name + " executable is required; no download was attempted")
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
                await exercise(processes, binary, openssl, directory, run_id, receipt, persist, credential_values)
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
                receipt.update(status="failed", reason="Owned TLS process cleanup could not be confirmed")
                exit_code = 1
        else:
            receipt["cleanup_confirmed"] = True
        if watcher is not None:
            watcher.stop()
            receipt.update(peak_owned_tree_rss_mib=round(watcher.peak_kib / 1024, 1), minimum_observed_memory_free_percent=watcher.minimum_free, resource_samples=watcher.samples)
        try:
            needles = secret_needles(credential_values)
            logs = sorted(directory.glob("*.log"))
            for path in logs:
                check_no_secrets(path.read_bytes(), needles, path.name)
            receipt["process_log_secret_scan"] = {"needle_count": len(needles), "credential_labels": sorted(credential_values),
                "log_count": len(logs), "credential_matches": 0,
                "logs": [{"name": path.name, "sha256": hashlib.sha256(path.read_bytes()).hexdigest()} for path in logs]}
        except HarnessFailure as error:
            receipt.update(status="failed", reason=str(error))
            exit_code = 1
        receipt.update(finished_at=SUPPORT.timestamp(), seconds=round(time.monotonic() - started, 2))
        persist()
        signal.signal(signal.SIGTERM, previous_sigterm)
        print(json.dumps({"run_id": run_id, "status": receipt["status"], "receipt": str(receipt_path), "seconds": receipt["seconds"], "passed": receipt["passed"]}, indent=2))
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
