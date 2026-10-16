"""Small harness safety checks; no Temporal service or Worker is started."""

import importlib.util
import json
import os
from pathlib import Path
import signal
import sys
import time
from types import SimpleNamespace

import pytest


SCRIPT = Path(__file__).resolve().parents[1] / "scripts/temporal_fault_smoke.py"
SPEC = importlib.util.spec_from_file_location("mini_loop_temporal_fault_harness", SCRIPT)
harness = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = harness
SPEC.loader.exec_module(harness)


def test_rss_budget_counts_driver_descendants_and_owned_reparented_children_only():
    rows = {
        100: (1, 100, 10),
        101: (100, 101, 20),
        102: (101, 101, 30),
        103: (1, 101, 40),
        200: (1, 200, 9000),
    }
    assert harness.owned_rss_kib(rows, 100, {101}) == 100


def test_fault_signal_refuses_unregistered_or_replaced_process_group(tmp_path, monkeypatch):
    manager = harness.Processes(tmp_path, time.monotonic() + 10)
    record = harness.OwnedProcess("worker", SimpleNamespace(pid=111, poll=lambda: None), tmp_path / "worker.log")
    signals = []
    monkeypatch.setattr(os, "killpg", lambda *args: signals.append(args))
    with pytest.raises(harness.HarnessFailure, match="unregistered"):
        manager.signal(record, signal.SIGKILL)
    manager.records.append(record)
    monkeypatch.setattr(os, "getpgid", lambda _pid: 999)
    with pytest.raises(harness.HarnessFailure, match="original process group"):
        manager.signal(record, signal.SIGKILL)
    assert signals == []


@pytest.mark.parametrize("rss,free", [(harness.RSS_BUDGET_KIB + 1, 50), (10, 14)])
def test_resource_watchdog_stops_only_its_owned_manager_on_threshold_crossing(monkeypatch, rss, free):
    manager = SimpleNamespace(deadline=time.monotonic() + 20, failure=None,
                              groups=lambda: {123}, stop_all=lambda **kwargs: stopped.append(kwargs))
    stopped = []
    monkeypatch.setattr(harness, "process_rows", lambda: {os.getpid(): (1, 9, rss)})
    monkeypatch.setattr(harness, "free_percent", lambda: free)
    watchdog = harness.Watchdog(manager)
    watchdog.observe()
    assert isinstance(manager.failure, harness.ResourceBlocked)
    assert stopped == [{"force": True}]
    assert watchdog.samples == 1


def test_children_do_not_inherit_credentials_or_existing_temporal_deployment(monkeypatch):
    for key in ("TEMPORAL_ADDRESS", "TEMPORAL_API_KEY", "LANGSMITH_API_KEY", "OPENAI_API_KEY", "PYTHONPATH"):
        monkeypatch.setenv(key, "sensitive-untrusted-override")
    result = harness.subprocess_environment()
    assert all("sensitive-untrusted-override" != value for value in result.values())
    assert result["LANGSMITH_TRACING"] == "false"
    assert result["PYTHONPATH"] == str(harness.MODULE / "src")


def test_missing_binary_has_unique_terminal_receipts_without_starting_children(tmp_path, monkeypatch):
    monkeypatch.setattr(harness.Processes, "spawn", lambda *args, **kwargs: pytest.fail("A child must not start"))
    for _ in range(2):
        assert harness.main(["--temporal-binary", str(tmp_path / "absent"), "--output-dir", str(tmp_path)]) == 1
    receipts = [json.loads(path.read_text()) for path in tmp_path.glob("*/receipt.json")]
    assert len(receipts) == 2
    assert len({receipt["run_id"] for receipt in receipts}) == 2
    assert all(receipt["status"] == "failed" and receipt["cleanup_confirmed"] for receipt in receipts)
    assert not list(tmp_path.rglob("*.tmp"))


def test_checksum_mismatch_stops_before_any_child_process(tmp_path, monkeypatch):
    binary = tmp_path / "temporal"
    binary.write_text("not a Temporal executable")
    binary.chmod(0o700)
    monkeypatch.setattr(harness.Processes, "spawn", lambda *args, **kwargs: pytest.fail("An unverified child must not start"))
    assert harness.main(["--temporal-binary", str(binary), "--binary-sha256", "0" * 64,
                         "--output-dir", str(tmp_path / "receipts")]) == 1
    receipt = json.loads(next((tmp_path / "receipts").glob("*/receipt.json")).read_text())
    assert receipt["status"] == "failed"
    assert "checksum" in receipt["reason"]


def test_receipt_replaces_running_state_privately_and_atomically(tmp_path):
    receipt = tmp_path / "receipt.json"
    harness.private_json(receipt, {"status": "running"}, "run-id")
    harness.private_json(receipt, {"status": "passed"}, "run-id")
    assert json.loads(receipt.read_text()) == {"status": "passed"}
    assert receipt.stat().st_mode & 0o777 == 0o600
    assert list(tmp_path.iterdir()) == [receipt]


def test_snapshot_audit_reports_only_the_changed_field_path():
    before = [{"data": {"id": "original", "content": "private full message"}}]
    changed = [{"data": {"id": "replacement", "content": "private full message"}}]
    difference = harness.first_difference(before, changed, "messages")
    assert difference == "messages[0].data.id"
    assert "private full message" not in difference
    assert harness.first_difference(before, before) is None
