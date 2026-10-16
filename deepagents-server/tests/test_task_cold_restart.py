"""No-service acceptance-contract checks for the cold restart harness."""
from __future__ import annotations

import json
from pathlib import Path
import sys

import pytest

ROOT = Path(__file__).resolve().parents[1]
from tooling.acceptance.scenarios import cold_restart as harness


def queued(task="task-a", state="PENDING_ACTIVITY_STATE_SCHEDULED"):
    return {"task_id": task, "status": "RUNNING", "pending_activities": [{"activity_id": "1", "state": state}]}


def test_native_queue_requires_scheduled_and_no_model_start():
    harness.check_queued([queued()], {"task-a"}, [])
    for rows, starts in (([queued(state="PENDING_ACTIVITY_STATE_STARTED")], []),
                         ([queued()], [{"workflow_id": "task-a"}]), ([], [])):
        with pytest.raises(harness.HarnessFailure): harness.check_queued(rows, {"task-a"}, starts)


def zeros(at):
    return {"monotonic_ns": at, "container_ids": ["a", "b", "c", "d"], "host_pids": [1, 2],
            "running_container_count": 0, "live_host_process_count": 0}


@pytest.mark.parametrize("mutation", ["short", "container", "observer", "missing_role", "identity_changed"])
def test_zero_window_requires_every_old_role_absent_for_three_seconds(mutation):
    good = [zeros(0), zeros(3_100_000_000)]
    harness.check_zero_window(good)
    if mutation == "short": good[-1]["monotonic_ns"] = 2_900_000_000
    elif mutation == "container": good[-1]["running_container_count"] = 1
    elif mutation == "observer": good[-1]["live_host_process_count"] = 1
    elif mutation == "missing_role":
        for row in good: row["container_ids"] = ["a", "b", "c"]
    else: good[-1]["host_pids"] = [3, 4]
    with pytest.raises(harness.HarnessFailure): harness.check_zero_window(good)


def test_native_can_chain_requires_payload_hash_and_first_identity():
    record = {"first_execution_run_id": "run-0"}
    rows = [{"run_id": f"run-{i}", "first_execution_run_id": "run-0", "previous_run_id": f"run-{i-1}" if i else None,
        "next_run_id": f"run-{i+1}" if i < 4 else None, "workflow_task_failures": 0,
        "start_input_sha256": str(i), "continued_input_sha256": str(i+1) if i < 4 else None} for i in range(5)]
    harness.check_chain(rows, record, "run-4")
    for key, value in (("start_input_sha256", "bad"), ("first_execution_run_id", "foreign"), ("workflow_task_failures", 1), ("previous_run_id", "foreign")):
        broken = [dict(x) for x in rows]; broken[2][key] = value
        with pytest.raises(harness.HarnessFailure): harness.check_chain(broken, record, "run-4")


def test_final_long_task_rejects_fast_fixture_and_wrong_owner():
    record = {"task_id": "task-a", "owner": "bob", "first_execution_run_id": "run-a"}
    value = {"status": "succeeded", "ready": True, "result": {"status": "completed", "version": "task-v2", "schema_version": 2,
        "identity": {"task_id": "task-a", "workflow_id": "task-a", "owner": "bob", "first_execution_run_id": "run-a"}, "can_count": 1}}
    with pytest.raises(harness.HarnessFailure): harness.check_final(value, record)
    value["result"]["can_count"] = 4; value["result"]["identity"]["owner"] = "alice"
    with pytest.raises(harness.HarnessFailure): harness.check_final(value, record)


def test_missing_cli_writes_failed_receipt_without_docker(tmp_path, monkeypatch):
    def forbidden(*args, **kwargs): raise AssertionError("Docker must not be contacted")
    monkeypatch.setattr(harness.DOCKER, "Engine", forbidden)
    assert harness.main(["--image", "sha256:" + "a" * 64, "--manifest", str(tmp_path / "manifest.json"), "--temporal-binary", str(tmp_path / "absent"), "--binary-sha256", "bad", "--output-dir", str(tmp_path)]) == 1
    receipt = json.loads(next(tmp_path.glob("*/receipt.json")).read_text())
    assert receipt["status"] == "failed" and receipt["cleanup_confirmed"]


def test_driver_does_not_create_sdk_or_http_clients():
    text = Path(harness.__file__).read_text()
    assert "Client.connect(" not in text and "httpx.AsyncClient(" not in text and "SUPPORT.connect(" not in text
    observer = (ROOT / "tooling/acceptance/fixtures/task_cold_observer.py").read_text()
    assert "Client.connect(" in observer and "httpx.AsyncClient(" in observer


def test_attempt_audit_requires_consecutive_stable_retries_and_distinct_summary_runs():
    rows = [{"workflow_id": "task-a", "run_id": "run-" + str(i), "activity_id": str(i), "attempt": 1,
        "request_sha256": str(i), "summary": i < 2, "role": ("main", "main", "child003", "child018")[i]} for i in range(4)]
    rows.append({**rows[-1], "attempt": 2})
    result = harness.check_attempts(rows, [{"outcome": "completed"}], {"task-a"})
    assert result["maximum_attempt"] == 2 and result["retried_activity_count"] == 1
    for field, value in (("attempt", 3), ("request_sha256", "different")):
        changed = [dict(x) for x in rows]; changed[-1][field] = value
        with pytest.raises(harness.HarnessFailure): harness.check_attempts(changed, [], {"task-a"})
    changed = [dict(x) for x in rows]; changed[1]["run_id"] = changed[0]["run_id"]
    with pytest.raises(harness.HarnessFailure): harness.check_attempts(changed, [], {"task-a"})
