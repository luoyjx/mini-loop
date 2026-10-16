"""Shared tls support for bounded acceptance scenarios."""
from __future__ import annotations

from dataclasses import replace
from datetime import timedelta
import hashlib
import json
import secrets
import ssl
import time

from tooling.acceptance.support import process as SUPPORT
from tooling.acceptance.support.process import HarnessFailure


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
