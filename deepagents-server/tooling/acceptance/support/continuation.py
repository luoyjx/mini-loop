"""Shared continuation support for bounded acceptance scenarios."""
from __future__ import annotations

import json
import os

from tooling.acceptance.support import process as SUPPORT
from tooling.acceptance.support.process import HarnessFailure
from tooling.acceptance.fixtures import task_can_worker_fixture as FIXTURE


def read_marker(directory, task_id):
    path = directory / (FIXTURE.task_stem(task_id) + ".latest.json")
    if not path.exists():
        return None
    value = json.loads(path.read_text(encoding="utf-8"))
    if value.get("workflow_id") != task_id or value.get("summary") is not False:
        raise HarnessFailure("A model barrier lost its Task identity")
    return value


def release(directory, task_id, marker):
    _, path = FIXTURE.gate_paths(directory, task_id, marker["call_id"])
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    os.close(descriptor)


def check_replay(before, after, original, retried):
    for field in ("workflow_id", "run_id", "activity_id", "call_id", "request_sha256"):
        if original.get(field) != retried.get(field):
            raise HarnessFailure("Cold replay changed the persisted Activity at " + field)
    if retried["pid"] == original["pid"] or retried["attempt"] <= original["attempt"]:
        raise HarnessFailure("Cold replay did not retry the original Activity on a new Worker")
    difference = SUPPORT.first_difference(before, after, "snapshot")
    if difference:
        raise HarnessFailure("Cold replay changed native checkpoint state at " + difference)


def check_chain(histories, first_run_id):
    if len(histories) < 4 or histories[0]["run_id"] != first_run_id:
        raise HarnessFailure("Task did not cross at least three native Continue-As-New boundaries")
    seen = set()
    for index, row in enumerate(histories):
        if row["run_id"] in seen:
            raise HarnessFailure("Native continuation chain repeated a Run ID")
        seen.add(row["run_id"])
        if row["first_execution_run_id"] != first_run_id:
            raise HarnessFailure("Continuation changed its first execution identity")
        expected_previous = histories[index - 1]["run_id"] if index else None
        expected_next = histories[index + 1]["run_id"] if index + 1 < len(histories) else None
        if row["previous_run_id"] != expected_previous or row["next_run_id"] != expected_next:
            raise HarnessFailure("Native continuation navigation is not a contiguous chain")
        if index and row["start_input_sha256"] != histories[index - 1]["continued_input_sha256"]:
            raise HarnessFailure("Native continuation changed the complete checkpoint transport payload")
        if row["workflow_task_failures"]:
            raise HarnessFailure("The continuation chain contains WorkflowTaskFailed events")
        if expected_next is not None and row["status"] != "continued_as_new":
            raise HarnessFailure("An intermediate Run did not close as Continued-As-New")
    return len(histories) - 1


def check_final(snapshot, task_id, first_run_id):
    identity = {"task_id": task_id, "workflow_id": task_id, "owner": "alice",
                "first_execution_run_id": first_run_id}
    if snapshot.get("identity") != identity or snapshot.get("version") != "task-v2" or snapshot.get("schema_version") != 2:
        raise HarnessFailure("The final snapshot lost its stable Task identity or schema")
    if snapshot.get("status") != "completed" or snapshot.get("can_count", 0) < 3:
        raise HarnessFailure("The Task did not complete after at least three continuations")
    for path, content in FIXTURE.expected_files().items():
        file = snapshot.get("files", {}).get(path, {})
        if file.get("content") != content or not file.get("created_at") or not file.get("modified_at"):
            raise HarnessFailure("A native tool/subagent file was lost or changed: " + path)
    todos = snapshot.get("todos", [])
    if len(todos) != 1 or todos[0].get("status") != "completed":
        raise HarnessFailure("The real planning state did not finish")
    messages = snapshot.get("messages", [])
    completed_ids = [item.get("data", {}).get("tool_call_id") for item in messages if item.get("type") == "tool"]
    expected_ids = [f"can-main-{step:03d}" for step in range(FIXTURE.MAIN_STEPS)]
    if completed_ids != expected_ids:
        raise HarnessFailure("Main native tools were duplicated, reordered or lost across continuations")
    if sum(item.get("type") == "human" for item in messages) != 1:
        raise HarnessFailure("Continuation duplicated the original human input")
    for index in (3, 18):
        content = messages[[item.get("data", {}).get("tool_call_id") for item in messages].index(f"can-main-{index:03d}")]["data"]["content"]
        if content != f"CAN_COMPLETE:child{index:03d}":
            raise HarnessFailure("A real subagent result was not retained by its parent task tool")
    state = snapshot.get("state", {})
    event = state.get("_summarization_event")
    session = state.get("_summarization_session_id")
    if not isinstance(event, dict) or not isinstance(session, str) or not session:
        raise HarnessFailure("The completed checkpoint lacks genuine private summarization state")
    path = event.get("file_path")
    if not isinstance(path, str) or path not in snapshot.get("files", {}) or not event.get("summary_message"):
        raise HarnessFailure("Summarization lost its original history offload or summary message")
    return {"native_tool_steps": len(completed_ids), "history_file": path,
            "summary_cutoff_index": event.get("cutoff_index"), "can_count": snapshot["can_count"]}
