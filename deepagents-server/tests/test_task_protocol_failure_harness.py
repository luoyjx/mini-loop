"""Offline contracts for the P1.5/P1.6 acceptance fixtures."""

from __future__ import annotations

import asyncio
import json

import pytest

from tooling.acceptance.fixtures.task_worker_fixture import ControlledHTTPError, fault_mode
from tooling.acceptance.scenarios import task_protocol_failures as scenario
from tooling.acceptance.scenarios.task_protocol_failures import summarize_fault_attempts
from tooling.acceptance.support.process import HarnessFailure
from tooling.acceptance.support.response_loss import post_and_drop_response


def test_fault_labels_are_fixed_fixture_inputs_and_rate_limit_carries_retry_delay():
    assert fault_mode("fault:rate-limit:run") == "rate-limit"
    assert fault_mode("fault:timeout:run") == "timeout"
    assert fault_mode("fault:disconnect:run") == "disconnect"
    assert fault_mode("fault:race:run") == "race"
    assert fault_mode("ordinary production input") is None
    error = ControlledHTTPError()
    assert error.status_code == 429
    assert error.response.headers == {"retry-after-ms": "100"}


def test_attempt_summary_requires_exact_finite_sequences_and_one_activity(tmp_path):
    task_ids = {"rate-limit": "task-rate", "timeout": "task-timeout", "disconnect": "task-disconnect"}
    expected = {
        "rate-limit": [(1, "failed"), (2, "failed"), (3, "succeeded")],
        "timeout": [(1, "failed"), (2, "failed"), (3, "failed")],
        "disconnect": [(1, "failed"), (2, "succeeded")],
    }
    index = 0
    for mode, attempts in expected.items():
        for attempt, outcome in attempts:
            row = {"workflow_id": task_ids[mode], "run_id": "run-" + mode,
                   "activity_id": "activity-" + mode, "attempt": attempt,
                   "mode": mode, "phase": "initial", "outcome": outcome,
                   "pid": 1, "observed_monotonic_ns": attempt * 1_000_000}
            (tmp_path / f"{index}.attempt.json").write_text(json.dumps(row))
            index += 1
    summary = summarize_fault_attempts(tmp_path, task_ids)
    assert {mode: row["attempt_count"] for mode, row in summary.items()} == {
        "rate-limit": 3, "timeout": 3, "disconnect": 2}
    damaged = json.loads((tmp_path / "0.attempt.json").read_text())
    damaged["activity_id"] = "different"
    (tmp_path / "0.attempt.json").write_text(json.dumps(damaged))
    with pytest.raises(HarnessFailure, match="Activity identity"):
        summarize_fault_attempts(tmp_path, task_ids)


@pytest.mark.asyncio
async def test_response_loss_proxy_consumes_upstream_success_before_dropping_client_response():
    received = asyncio.Event()

    async def upstream(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        await reader.readuntil(b"\r\n\r\n")
        received.set()
        body = b'{"accepted":true}'
        writer.write(b"HTTP/1.1 202 Accepted\r\nContent-Type: application/json\r\nContent-Length: "
                     + str(len(body)).encode() + b"\r\nConnection: close\r\n\r\n" + body)
        await writer.drain()
        writer.close()
        await writer.wait_closed()

    server = await asyncio.start_server(upstream, "127.0.0.1", 0)
    port = server.sockets[0].getsockname()[1]
    try:
        evidence = await post_and_drop_response(
            "127.0.0.1", port, "/v1/tasks",
            headers={"Authorization": "Bearer private", "Idempotency-Key": "stable"},
            json_body={"message": "private"},
        )
    finally:
        server.close()
        await server.wait_closed()
    assert received.is_set()
    assert evidence["upstream_status"] == 202
    assert evidence["upstream_response_bytes"] > 0
    assert len(evidence["upstream_response_sha256"]) == 64
    assert evidence["client_error"] in {"RemoteProtocolError", "ReadError"}
    assert "private" not in repr(evidence)


def test_scenario_preflight_fails_closed_without_starting_children(tmp_path, monkeypatch):
    monkeypatch.setattr(scenario.SUPPORT, "Processes",
                        lambda *args, **kwargs: pytest.fail("preflight must not start children"))
    assert scenario.main(["--temporal-binary", str(tmp_path / "missing"),
                          "--output-dir", str(tmp_path / "receipts")]) == 1
    receipt_path = next((tmp_path / "receipts").glob("*/receipt.json"))
    receipt = json.loads(receipt_path.read_text())
    assert receipt["status"] == "failed" and receipt["cleanup_confirmed"] is True
    assert receipt_path.stat().st_mode & 0o777 == 0o600


def test_scenario_checksum_mismatch_precedes_process_manager(tmp_path, monkeypatch):
    binary = tmp_path / "temporal"
    binary.write_text("unused")
    binary.chmod(0o700)
    monkeypatch.setattr(scenario.SUPPORT, "Processes",
                        lambda *args, **kwargs: pytest.fail("checksum mismatch must not start children"))
    assert scenario.main(["--temporal-binary", str(binary), "--binary-sha256", "0" * 64,
                          "--output-dir", str(tmp_path / "receipts")]) == 1
    receipt = json.loads(next((tmp_path / "receipts").glob("*/receipt.json")).read_text())
    assert receipt["reason"] == "The Temporal executable checksum did not match"
