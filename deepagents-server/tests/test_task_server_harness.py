"""Lightweight safety/observation checks; these never start a server."""

import copy
import json
from pathlib import Path
import sys

import httpx
import pytest


from tooling.acceptance.scenarios import task_server as scenario
from tooling.acceptance.support import http as harness


def page():
    return {"task_id": "task-a", "run_id": "run-a", "order": "descending",
            "events": [{"event_id": 2, "type": "workflow_task_scheduled", "time": "now"},
                       {"event_id": 1, "type": "workflow_execution_started", "time": "now"}],
            "next_cursor": None}


def test_history_observation_requires_native_identity_and_strict_event_order():
    assert harness.check_page(page(), "task-a", "run-a") == [2, 1]
    for field, value in (("task_id", "foreign"), ("run_id", "different-run"), ("order", "ascending")):
        invalid = page()
        invalid[field] = value
        with pytest.raises(harness.HarnessFailure):
            harness.check_page(invalid, "task-a", "run-a")
    for ids in ((1, 2), (2, 2), (True, 1), (0, 1)):
        invalid = page()
        for event, event_id in zip(invalid["events"], ids):
            event["event_id"] = event_id
        with pytest.raises(harness.HarnessFailure):
            harness.check_page(invalid, "task-a", "run-a")


def test_history_observation_rejects_payload_or_incomplete_summaries():
    for mutation in ({"payloads": "private"}, {"token": "stream"}, {"time": None}, {"type": None}):
        invalid = page()
        invalid["events"][0].update(mutation)
        with pytest.raises(harness.HarnessFailure):
            harness.check_page(invalid, "task-a", "run-a")


def test_response_validation_does_not_print_private_body():
    with pytest.raises(harness.HarnessFailure) as failure:
        harness.decode(httpx.Response(503, json={"token": "private-token"}), 202)
    assert "private-token" not in str(failure.value)
    with pytest.raises(harness.HarnessFailure):
        harness.decode(httpx.Response(202, json=[]), 202)


def test_recovered_result_preserves_complete_messages_file_metadata_and_plan():
    before = {"identity": {"task_id": "task-a", "owner": "alice", "workflow_id": "task-a",
                           "first_execution_run_id": "run-a"},
              "schema_version": 2, "version": "task-v2", "status": "running", "state": {},
              "messages": [{"type": "tool", "data": {"id": "stable-id", "content": "original"}}],
              "files": {"/plan.md": {"content": "original", "created_at": "time-1", "modified_at": "time-1"}},
              "todos": [{"content": "original task", "status": "in_progress"}]}
    after = copy.deepcopy(before)
    after["messages"].append({"type": "ai", "data": {"id": "final"}})
    after["todos"][0]["status"] = "completed"
    after["status"] = "completed"
    harness.check_preserved_state(before, after)
    for field, key, changed in (("messages", "id", "random-id"), ("files", "created_at", "changed-time"),
                                ("todos", "content", "different task")):
        damaged = copy.deepcopy(after)
        target = {"messages": damaged["messages"][0]["data"], "files": damaged["files"]["/plan.md"],
                  "todos": damaged["todos"][0]}[field]
        target[key] = changed
        with pytest.raises(harness.HarnessFailure) as failure:
            harness.check_preserved_state(before, damaged)
        assert field in str(failure.value) and changed not in str(failure.value)


def test_current_snapshot_contract_requires_first_execution_identity_and_native_state():
    snapshot = {"schema_version": 2, "version": "task-v2", "status": "running", "state": {},
                "identity": {"task_id": "task-a", "workflow_id": "task-a", "owner": "alice",
                             "first_execution_run_id": "run-a"}}
    harness.check_snapshot_contract(snapshot, "task-a", "run-a", "task-v2", "running")
    for damaged in (dict(snapshot, schema_version=1), dict(snapshot, version="unsupported-version"),
                    dict(snapshot, state=None), dict(snapshot, identity={**snapshot["identity"], "first_execution_run_id": "foreign"})):
        with pytest.raises(harness.HarnessFailure):
            harness.check_snapshot_contract(damaged, "task-a", "run-a", "task-v2", "running")


def test_preservation_rejects_changed_private_native_state_without_normalization():
    before = {"identity": {}, "schema_version": 2, "version": "task-v2", "files": {},
              "messages": [{"type": "human", "data": {"id": "original"}}],
              "todos": [{"content": "same plan", "status": "in_progress"}],
              "state": {"_summarization_event": {"summary_message": {"kwargs": {"id": "stable"}}}}}
    after = copy.deepcopy(before)
    after["messages"].append({"type": "ai", "data": {"id": "final"}})
    after["todos"][0]["status"] = "completed"
    after["status"] = "completed"
    harness.check_preserved_state(before, after)
    for initial, completed in (({k: v for k, v in before.items() if k != "state"}, after),
                               (before, dict(after, state=None))):
        with pytest.raises(harness.HarnessFailure, match="full native state"):
            harness.check_preserved_state(initial, completed)
    after["state"]["_summarization_event"]["summary_message"]["kwargs"]["id"] = "random"
    with pytest.raises(harness.HarnessFailure, match="native state"):
        harness.check_preserved_state(before, after)


def test_control_files_are_hashed_and_private(tmp_path):
    task_id = "../../outside"
    path = harness.marker_path(tmp_path, task_id)
    assert path.parent == tmp_path and len(path.stem) == 72
    assert harness.read_marker(tmp_path, task_id) is None
    path.write_text(json.dumps({"workflow_id": "other"}))
    with pytest.raises(harness.HarnessFailure):
        harness.read_marker(tmp_path, task_id)
    harness.release(tmp_path, task_id)
    release = next(tmp_path.glob("*.release"))
    assert release.parent == tmp_path and release.stat().st_mode & 0o777 == 0o600


def test_preflight_leaves_unique_terminal_private_receipts_without_children(tmp_path, monkeypatch):
    def refuse_children(*args, **kwargs):
        pytest.fail("Preflight must not start the process manager")
    monkeypatch.setattr(harness.SUPPORT, "Processes", refuse_children)
    for _ in range(2):
        assert scenario.main(["--temporal-binary", str(tmp_path / "missing"),
                             "--output-dir", str(tmp_path / "receipts")]) == 1
    receipts = list((tmp_path / "receipts").glob("*/receipt.json"))
    assert len(receipts) == 2
    values = [json.loads(path.read_text()) for path in receipts]
    assert len({value["run_id"] for value in values}) == 2
    assert all(value["status"] == "failed" and value["cleanup_confirmed"] for value in values)
    assert all(path.stat().st_mode & 0o777 == 0o600 for path in receipts)


def test_checksum_failure_occurs_before_process_manager(tmp_path, monkeypatch):
    binary = tmp_path / "temporal"
    binary.write_text("a deliberately unused executable")
    binary.chmod(0o700)
    monkeypatch.setattr(harness.SUPPORT, "Processes", lambda *args, **kwargs: pytest.fail("No children on checksum mismatch"))
    assert scenario.main(["--temporal-binary", str(binary), "--binary-sha256", "0" * 64,
                         "--output-dir", str(tmp_path / "receipts")]) == 1
    receipt = json.loads(next((tmp_path / "receipts").glob("*/receipt.json")).read_text())
    assert receipt["reason"] == "The Temporal executable checksum did not match"
    assert receipt["cleanup_confirmed"] and "processes" not in receipt
