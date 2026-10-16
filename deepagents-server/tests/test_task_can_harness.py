"""Reject false-positive continuation/replay evidence before expensive faults."""

from __future__ import annotations

from copy import deepcopy
import json
from pathlib import Path
import sys

from langchain_core.messages import AIMessage, HumanMessage, ToolMessage
import pytest


pytest.importorskip("temporalio")


from tooling.acceptance.scenarios import continuation as scenario
from tooling.acceptance.support import continuation as harness
fixture = harness.FIXTURE


def chain():
    return [{"run_id": f"r{index}", "first_execution_run_id": "r0",
             "previous_run_id": f"r{index-1}" if index else None,
             "next_run_id": f"r{index+1}" if index < 3 else None,
             "status": "continued_as_new" if index < 3 else "completed",
             "start_input_sha256": f"payload{index}",
             "continued_input_sha256": f"payload{index+1}" if index < 3 else None,
             "workflow_task_failures": 0} for index in range(4)]


def test_native_chain_requires_three_real_linked_transitions_without_workflow_task_failure():
    assert harness.check_chain(chain(), "r0") == 3
    for field, value in (("first_execution_run_id", "foreign"), ("previous_run_id", "r0"),
                         ("next_run_id", "missing"), ("workflow_task_failures", 1),
                         ("start_input_sha256", "changed-checkpoint"),
                         ("status", "completed"), ("run_id", "r0")):
        altered = chain()
        altered[2][field] = value
        with pytest.raises(harness.HarnessFailure):
            harness.check_chain(altered, "r0")
    with pytest.raises(harness.HarnessFailure):
        harness.check_chain(chain()[:3], "r0")


def test_replay_requires_identical_request_and_private_summary_state_on_new_attempt():
    before = {"state": {"_summarization_event": {"summary_message": {"kwargs": {"id": "summary-stable"}}},
                        "files": {"/conversation_history/session.md": {"content": "exact timestamp"}}}}
    marker = {"workflow_id": "task", "run_id": "run", "activity_id": "4", "call_id": "can-main-024",
              "request_sha256": "same", "pid": 100, "attempt": 1}
    retried = dict(marker, pid=200, attempt=2)
    harness.check_replay(before, deepcopy(before), marker, retried)
    for altered in (dict(retried, request_sha256="different"), dict(retried, attempt=1), dict(retried, pid=100)):
        with pytest.raises(harness.HarnessFailure):
            harness.check_replay(before, before, marker, altered)
    changed = deepcopy(before)
    changed["state"]["_summarization_event"]["summary_message"]["kwargs"]["id"] = "new random id"
    with pytest.raises(harness.HarnessFailure, match="native checkpoint state"):
        harness.check_replay(before, changed, marker, retried)


def test_model_summary_preserves_completed_steps_after_original_human_is_evicted():
    model = fixture.TaskCANFixtureModel(barrier_directory="unused")
    prompt = HumanMessage(content='[can-main]\n<tool_call id="can-main-000">plan</tool_call>\n<tool_call id="can-main-001">write</tool_call>')
    summary, role, step, is_summary = model.reply([prompt])
    assert is_summary and role == "main" and step == -1
    compacted = HumanMessage(content="Earlier conversation:\n" + summary.content,
                             additional_kwargs={"lc_source": "summarization"})
    bound = model.bind_tools([{"name": "read_file"}])
    reply, role, step, is_summary = bound.reply([compacted])
    assert (role, step, is_summary) == ("main", 2, False)
    assert reply.tool_calls[0]["name"] == "read_file"
    assert reply.tool_calls[0]["args"]["file_path"] == "/phase-001.md"


def test_normal_model_never_treats_an_uncompleted_ai_tool_call_as_done():
    model = fixture.TaskCANFixtureModel(barrier_directory="unused", has_tools=True)
    messages = [HumanMessage(content="[can-main]"),
                AIMessage(content="", tool_calls=[{"id": "can-main-000", "name": "write_todos", "args": {}}])]
    reply, _, step, _ = model.reply(messages)
    assert step == 0 and reply.tool_calls[0]["id"] == "can-main-000"
    messages.append(ToolMessage(content="error", tool_call_id="can-main-000", status="error"))
    with pytest.raises(ValueError, match="real CAN fixture tool"):
        model.reply(messages)


def test_native_subagent_fixture_uses_separate_role_and_real_file_tools():
    model = fixture.TaskCANFixtureModel(barrier_directory="unused", has_tools=True)
    messages = [HumanMessage(content="[can-child:003] real delegated work")]
    reply, role, step, _ = model.reply(messages)
    assert (role, step) == ("child003", 0)
    assert reply.tool_calls[0]["name"] == "write_file"
    messages.extend([reply, ToolMessage(content="written", tool_call_id="can-child003-000")])
    reply, _, step, _ = model.reply(messages)
    assert step == 1 and reply.tool_calls[0]["name"] == "read_file"
    messages.extend([reply, ToolMessage(content="verified:child003", tool_call_id="can-child003-001")])
    reply, _, step, _ = model.reply(messages)
    assert step == 2 and reply.content == "CAN_COMPLETE:child003" and not reply.tool_calls


def test_private_gates_are_task_scoped_hashed_and_owner_readable_only(tmp_path):
    marker, release = fixture.gate_paths(tmp_path, "private-task-id", "can-main-008")
    other_marker, other_release = fixture.gate_paths(tmp_path, "another-task", "can-main-008")
    assert marker != other_marker and release != other_release
    assert "private-task-id" not in marker.name and "can-main" not in release.name
    fixture.private_json(marker, {"workflow_id": "private-task-id", "summary": False, "call_id": "can-main-008"})
    assert marker.stat().st_mode & 0o777 == 0o600
    observed = harness.read_marker(tmp_path, "private-task-id")
    harness.release(tmp_path, "private-task-id", observed)
    assert release.stat().st_mode & 0o777 == 0o600


def test_missing_binary_yields_private_terminal_receipt_without_child_process(tmp_path):
    assert scenario.main(["--temporal-binary", str(tmp_path / "absent"), "--output-dir", str(tmp_path / "runs")]) == 1
    receipts = list((tmp_path / "runs").glob("*/receipt.json"))
    assert len(receipts) == 1
    result = json.loads(receipts[0].read_text())
    assert result["status"] == "failed" and result["cleanup_confirmed"] is True
    assert result["passed"] == [] and "processes" not in result
    assert receipts[0].stat().st_mode & 0o777 == 0o600
