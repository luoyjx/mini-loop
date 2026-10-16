"""Lightweight checks of rollout observation, gates and failure accounting."""

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
from langchain_core.messages import AIMessage, HumanMessage, ToolMessage


SCRIPT = Path(__file__).resolve().parents[1] / "scripts/task_worker_rollout_smoke.py"
SPEC = importlib.util.spec_from_file_location("_rollout_acceptance_tests", SCRIPT)
harness = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = harness
SPEC.loader.exec_module(harness)
fixture = harness.FIXTURE


def transcript(steps):
    result = [HumanMessage(content="[rollout] trusted test", id="original")]
    for index in range(steps):
        result.extend([AIMessage(content="", id=f"ai-{index}"),
                       ToolMessage(content="native success", id=f"tool-{index}", tool_call_id=f"rollout-{index:03d}")])
    return result


def test_fixture_resumes_unchanged_real_tool_sequence_after_native_continuation():
    model = fixture.RolloutModel(barrier_directory="unused")
    reply, step = model.reply(transcript(8))
    assert step == 8 and reply.tool_calls[0]["id"] == "rollout-008"
    assert reply.tool_calls[0]["name"] == "read_file"
    assert reply.tool_calls[0]["args"] == {"file_path": "/rollout-7.md"}
    reply, step = model.reply(transcript(10))
    assert step == 10 and reply.content == "ROLLOUT_COMPLETE" and not reply.tool_calls
    broken = transcript(2)
    broken[-1].tool_call_id = "rollout-000"
    with pytest.raises(ValueError, match="duplicated, lost or reordered"):
        model.reply(broken)


def test_takeover_requires_same_request_activity_run_and_bounded_new_attempt():
    before = {"workflow_id": "task", "run_id": "run", "activity_id": "4", "step": 3,
              "request_sha256": "same", "pid": 1, "attempt": 1}
    after = dict(before, pid=2, attempt=2)
    harness.check_takeover(before, after)
    for altered in (dict(after, run_id="new"), dict(after, request_sha256="new"),
                    dict(after, pid=1), dict(after, attempt=1), dict(after, attempt=4)):
        with pytest.raises(harness.HarnessFailure):
            harness.check_takeover(before, altered)


def write_interval(directory, name, pid, start, end=None):
    row = {"pid": pid, "workflow_id": name, "started_monotonic_ns": start}
    fixture.private_json(directory / f"{name}.started.json", row)
    if end is not None:
        fixture.private_json(directory / f"{name}.finished.json", {**row, "finished_monotonic_ns": end, "outcome": "completed"})


def test_concurrency_accounting_requires_real_finish_or_owned_exit_and_detects_fourth_slot(tmp_path):
    write_interval(tmp_path, "one", 1, 1, 10)
    write_interval(tmp_path, "two", 2, 2, 9)
    write_interval(tmp_path, "hard", 3, 3)
    with pytest.raises(harness.HarnessFailure, match="finish or authoritative process exit"):
        harness.concurrency_evidence(tmp_path, {})
    evidence = harness.concurrency_evidence(tmp_path, {3: 8})
    assert evidence["maximum_active_model_activities"] == 3
    assert evidence["outcomes"] == {"completed": 2, "process_exited": 1}
    write_interval(tmp_path, "fourth", 4, 4, 7)
    with pytest.raises(harness.HarnessFailure, match="exceeded three"):
        harness.concurrency_evidence(tmp_path, {3: 8})


def completed_result():
    snapshot = {"schema_version": 2, "status": "completed", "can_count": 1,
        "identity": {"task_id": "task", "workflow_id": "task", "owner": "alice", "first_execution_run_id": "run1"},
        "messages": [{"type": "human", "data": {"id": "original"}}] + [
            {"type": "tool", "data": {"tool_call_id": f"rollout-{index:03d}"}} for index in range(10)],
        "files": {f"/rollout-{index}.md": {"content": f"rollout:{index}", "created_at": "one", "modified_at": "one"} for index in (1,3,5,7)},
        "todos": [{"content": "finish durable rollout task", "status": "completed"}]}
    return {"task_id": "task", "run_id": "run2", "version": "task-v2", "status": "succeeded", "ready": True, "result": snapshot}


def test_completion_evidence_requires_can_owner_and_native_results():
    record = {"task_id": "task", "owner": "alice", "first_execution_run_id": "run1"}
    result = completed_result()
    assert harness.check_completed(result, record)["native_tool_steps"] == 10
    for changed in ("owner", "can", "file", "tool"):
        value = deepcopy(result)
        if changed == "owner": value["result"]["identity"]["owner"] = "bob"
        elif changed == "can": value["result"]["can_count"] = 0
        elif changed == "file": value["result"]["files"]["/rollout-1.md"]["content"] = "changed"
        else: value["result"]["messages"].append(value["result"]["messages"][-1])
        with pytest.raises(harness.HarnessFailure):
            harness.check_completed(value, record)


def test_fixture_cooperative_cancel_and_deliberate_stuck_path_are_activity_only(tmp_path, monkeypatch):
    info = SimpleNamespace(workflow_id="task", workflow_run_id="run", activity_id="4", attempt=1)
    monkeypatch.setattr(fixture.activity, "info", lambda: info)
    monkeypatch.setattr(fixture.activity, "heartbeat", lambda *args: None)
    async def exercise(ignore):
        directory = tmp_path / ("ignored" if ignore else "cooperative")
        directory.mkdir()
        model = fixture.RolloutModel(barrier_directory=str(directory))
        gates = fixture.paths(directory, "task")
        if ignore:
            harness.touch_gate(directory, "task", "ignore-cancel")
        task = asyncio.create_task(model._agenerate(transcript(3)))
        for _ in range(100):
            if gates["blocked.json"].exists(): break
            await asyncio.sleep(0)
        assert gates["blocked.json"].exists()
        task.cancel()
        if ignore:
            for _ in range(100):
                if list(directory.glob("*.ignored-cancellation.json")): break
                await asyncio.sleep(0)
            assert not task.done() and list(directory.glob("*.ignored-cancellation.json"))
            harness.touch_gate(directory, "task", "release")
            result = await asyncio.wait_for(task, 1)
            assert result.generations[0].message.tool_calls[0]["id"] == "rollout-003"
        else:
            with pytest.raises(asyncio.CancelledError):
                await task
        rows = [json.loads(path.read_text()) for path in directory.glob("*.finished.json")]
        assert len(rows) == 1 and rows[0]["outcome"] == ("completed" if ignore else "cancelled")
    asyncio.run(exercise(False))
    asyncio.run(exercise(True))


def test_missing_binary_is_terminal_private_evidence_without_children(tmp_path):
    assert harness.main(["--temporal-binary", str(tmp_path / "absent"), "--output-dir", str(tmp_path / "runs")]) == 1
    receipts = list((tmp_path / "runs").glob("*/receipt.json"))
    result = json.loads(receipts[0].read_text())
    assert result["status"] == "failed" and result["cleanup_confirmed"] is True
    assert "processes" not in result and result["passed"] == []
    assert receipts[0].stat().st_mode & 0o777 == 0o600
