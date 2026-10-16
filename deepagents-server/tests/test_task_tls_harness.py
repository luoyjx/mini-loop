"""Acceptance checks fail closed without starting Temporal or an HTTP server."""
from __future__ import annotations

import asyncio
from copy import deepcopy
import importlib.util
import json
from pathlib import Path
import sys
from types import SimpleNamespace

import pytest

pytest.importorskip("temporalio")
ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("tls_harness_test", ROOT / "scripts/task_tls_fault_smoke.py")
harness = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = harness
SPEC.loader.exec_module(harness)
tunnel = harness.load_module("tls_tunnel_test", ROOT / "tests/task_tls_tunnel.py")


def test_secret_scan_checks_full_escaped_and_pem_body_without_disclosing_value():
    body = b"0123456789ABCDEF" * 4
    pem = b"-----BEGIN PRIVATE KEY-----\n" + body + b"\n-----END PRIVATE KEY-----\n"
    needles = harness.secret_needles({"test-key": pem, "token": b"private-token-1234567890"})
    for leaked in (pem, json.dumps(pem.decode())[1:-1].encode(), body, b"private-token-1234567890"):
        with pytest.raises(harness.HarnessFailure, match="Credential exclusion failed") as error:
            harness.check_no_secrets(b"prefix " + leaked + b" suffix", needles, "worker.log")
        assert leaked.decode() not in str(error.value)
    harness.check_no_secrets(b"TASK_WORKER_READY and public certificate fingerprint", needles, "safe.log")


def test_full_native_state_equality_includes_private_summary_and_identity():
    before = {"identity": {"first_execution_run_id": "first"}, "state": {
        "_summarization_event": {"summary_message": {"id": "summary"}}, "files": {"/a": {"created_at": "one"}}}}
    harness.check_snapshot_preserved(before, deepcopy(before))
    after = deepcopy(before)
    after["state"]["_summarization_event"]["summary_message"]["id"] = "different"
    with pytest.raises(harness.HarnessFailure, match="_summarization_event"):
        harness.check_snapshot_preserved(before, after)


def test_retry_evidence_requires_same_native_request_and_contiguous_bounded_attempts(tmp_path):
    before = {"workflow_id": "task", "run_id": "run", "activity_id": "4", "step": 3,
              "request_sha256": "stable", "attempt": 1, "pid": 100, "started_monotonic_ns": 1}
    after = {**before, "attempt": 2, "pid": 200, "started_monotonic_ns": 2}
    (tmp_path / "one.started.json").write_text(json.dumps(before))
    path = tmp_path / "two.started.json"
    path.write_text(json.dumps(after))
    result = harness.summarize_activity_attempts(tmp_path)
    assert result["retried_activity_count"] == 1 and result["maximum_activity_attempt"] == 2
    assert result["model_activity_attempt_count"] == 2 and len(result["source_files"]) == 2
    for field, value in (("request_sha256", "different"), ("step", 4), ("attempt", 4), ("attempt", 1)):
        path.write_text(json.dumps({**after, field: value}))
        with pytest.raises(harness.HarnessFailure):
            harness.summarize_activity_attempts(tmp_path)


def test_role_environment_uses_explicit_files_and_no_inline_secret(tmp_path):
    env = harness.connection_environment(tmp_path, "api-1")
    assert env[harness.PREFIX + "TLS"] == "true"
    assert env[harness.PREFIX + "CLIENT_CERT_FILE"].endswith("api-1.crt")
    assert env[harness.PREFIX + "CLIENT_KEY_FILE"].endswith("api-1.key")
    assert harness.PREFIX + "API_KEY" not in env
    with pytest.raises(harness.HarnessFailure, match="Unknown"):
        harness.connection_environment(tmp_path, "unexpected")


@pytest.mark.parametrize("alpn,certificate", [(None, b"certificate"), ("http/1.1", b"certificate"), ("h2", b"")])
def test_tunnel_rejects_missing_peer_or_wrong_application_protocol(alpn, certificate):
    peer = SimpleNamespace(selected_alpn_protocol=lambda: alpn, getpeercert=lambda **_: certificate, version=lambda: "TLSv1.3")
    transport = SimpleNamespace(get_extra_info=lambda _: peer)
    with pytest.raises(ValueError, match="required_mtls_h2"):
        tunnel.peer_metadata(transport)


def test_tunnel_audit_contains_only_public_peer_fingerprint():
    certificate = b"public DER certificate fixture"
    peer = SimpleNamespace(selected_alpn_protocol=lambda: "h2", getpeercert=lambda **_: certificate, version=lambda: "TLSv1.3")
    row = tunnel.peer_metadata(SimpleNamespace(get_extra_info=lambda _: peer))
    assert set(row) == {"client_certificate_sha256", "alpn", "tls_version"}
    assert len(row["client_certificate_sha256"]) == 64
    assert certificate.decode() not in json.dumps(row)


def test_tunnel_copy_applies_backpressure_for_every_bounded_chunk():
    class Reader:
        pieces = iter((b"one", b"two", b""))
        async def read(self, limit):
            assert limit == 65536
            return next(self.pieces)
    class Writer:
        chunks = []
        drains = 0
        def write(self, chunk):
            self.chunks.append(chunk)
        async def drain(self):
            self.drains += 1
    writer = Writer()
    asyncio.run(tunnel.copy_bytes(Reader(), writer))
    assert writer.chunks == [b"one", b"two"] and writer.drains == 2


def test_negative_controls_require_rejection_and_healthy_rpc_on_both_sides(tmp_path, monkeypatch):
    from mini_loop_deepagents.task_server import connection
    for name in ("untrusted-ca.crt", "untrusted-client.crt", "untrusted-client.key"):
        (tmp_path / name).write_bytes(b"untrusted")
    accepted = []
    async def connect(address, *, connection: object):
        if connection.server_root_ca_cert != b"trusted" or connection.server_name != harness.SERVER_NAME or connection.client_cert != b"client":
            raise globals_connection_error("temporal_connection_failed")
        accepted.append({})
        return object()
    globals_connection_error = connection.TemporalConnectionError
    async def health(_):
        return None
    monkeypatch.setattr(connection, "connect_temporal", connect)
    monkeypatch.setattr(harness, "native_health", health)
    monkeypatch.setattr(harness, "audit_connections", lambda _: accepted)
    settings = connection.TemporalConnectionSettings(tls_enabled=True, server_root_ca_cert=b"trusted", client_cert=b"client",
        client_private_key=b"key", server_name=harness.SERVER_NAME)
    receipt = {}
    asyncio.run(harness.negative_controls(SimpleNamespace(check=lambda: None), "127.0.0.1:1234", settings, tmp_path, None, receipt, lambda: None))
    assert len(receipt["negative_controls"]) == 4
    assert all(row["accepted_connections_before"] == row["accepted_connections_after_rejection"] for row in receipt["negative_controls"])
    async def timeout(*args, **kwargs):
        raise globals_connection_error("temporal_connection_timeout")
    # Healthy controls must still run. Only invalid connection returns a timeout.
    async def sometimes_timeout(address, *, connection):
        if connection.server_root_ca_cert != b"trusted":
            return await timeout()
        return await connect(address, connection=connection)
    monkeypatch.setattr(connection, "connect_temporal", sometimes_timeout)
    with pytest.raises(harness.HarnessFailure, match="timed out"):
        asyncio.run(harness.negative_controls(SimpleNamespace(check=lambda: None), "127.0.0.1:1234", settings, tmp_path, None, {}, lambda: None))


def test_missing_binary_is_private_terminal_receipt_and_creates_no_processes(tmp_path):
    assert harness.main(["--temporal-binary", str(tmp_path / "missing"), "--output-dir", str(tmp_path)]) == 1
    receipt_path = next(tmp_path.glob("*/receipt.json"))
    value = json.loads(receipt_path.read_text())
    assert value["status"] == "failed" and value["cleanup_confirmed"] is True
    assert "processes" not in value
    assert receipt_path.stat().st_mode & 0o777 == 0o600
