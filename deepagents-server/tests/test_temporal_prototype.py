"""Narrow contract checks; full worker/server faults live in the opt-in harness."""

from __future__ import annotations

from datetime import datetime, timezone
from dataclasses import replace
import json
import sqlite3
from types import SimpleNamespace

import pytest

pytest.importorskip("temporalio")

from langchain_core.messages import AIMessage, HumanMessage, ToolMessage
from temporalio.exceptions import ApplicationError
from temporalio.common import WorkflowIDReusePolicy
from langgraph.types import Command

from mini_loop_deepagents.temporal_prototype import CODE_VERSION, SNAPSHOT_SCHEMA
from mini_loop_deepagents.temporal_prototype import activities
from mini_loop_deepagents.temporal_prototype.client import PrototypeClient
from mini_loop_deepagents.temporal_prototype.model_fixture import TranscriptModel
from mini_loop_deepagents.temporal_prototype.workflow import (
    DeterministicStateBackend,
    DurableAgentWorkflow,
    StableToolMessageIDs,
    build_agent,
    rehydrate_snapshot,
    restore_agent_state,
)


def test_state_file_timestamps_follow_workflow_clock_across_write_and_edit(monkeypatch):
    files = {}
    backend = DeterministicStateBackend()
    monkeypatch.setattr(backend, "_read_files", lambda: files)
    monkeypatch.setattr(backend, "_send_files_update", lambda update: files.update(update))
    created = datetime(2026, 1, 1, tzinfo=timezone.utc)
    edited = datetime(2026, 1, 2, tzinfo=timezone.utc)
    monkeypatch.setattr("mini_loop_deepagents.temporal_prototype.workflow.workflow.now", lambda: created)
    assert backend.write("/plan.md", "before").error is None
    assert files["/plan.md"]["created_at"] == files["/plan.md"]["modified_at"] == created.isoformat()
    monkeypatch.setattr("mini_loop_deepagents.temporal_prototype.workflow.workflow.now", lambda: edited)
    assert backend.edit("/plan.md", "before", "after").error is None
    assert files["/plan.md"]["content"] == "after"
    assert files["/plan.md"]["created_at"] == created.isoformat()
    assert files["/plan.md"]["modified_at"] == edited.isoformat()
    assert backend.edit("/missing", "before", "after").error
    before_error = dict(files["/plan.md"])
    assert backend.edit("/plan.md", "not present", "after").error
    assert files["/plan.md"] == before_error
    assert backend.upload_files([("/bytes.bin", b"\xff\x00")])[0].error is None
    assert files["/bytes.bin"]["encoding"] == "base64"
    assert files["/bytes.bin"]["created_at"] == files["/bytes.bin"]["modified_at"] == edited.isoformat()


def test_full_state_projection_roundtrips_tool_ids_files_todos_and_identity():
    instance = DurableAgentWorkflow()
    state = {
        "messages": [HumanMessage(content="hello"), AIMessage(content="", tool_calls=[
            {"id": "call-plan", "name": "read_file", "args": {"file_path": "/plan.md"}},
        ]), ToolMessage(content="plan", tool_call_id="call-plan", name="read_file")],
        "files": {"/plan.md": {"content": "plan", "encoding": "utf-8"}},
        "todos": [{"content": "recover", "status": "in_progress"}],
    }
    instance._agent = SimpleNamespace(get_state=lambda config: SimpleNamespace(values=state))
    instance._identity = {"workflow_id": "task", "owner": "alice", "memory_key": "note"}
    instance._phase = instance._can_count = 1
    projected = json.loads(json.dumps(instance.snapshot()))
    restored = rehydrate_snapshot(projected, instance._identity)
    assert projected["version"] == CODE_VERSION and projected["schema_version"] == SNAPSHOT_SCHEMA
    assert restored["files"] == state["files"] and restored["todos"] == state["todos"]
    assert restored["messages"][-1].tool_call_id == "call-plan"
    assert restored["messages"][-2].tool_calls[0]["args"] == {"file_path": "/plan.md"}
    with pytest.raises(ApplicationError, match="Invalid full-state"):
        rehydrate_snapshot(projected, {**instance._identity, "owner": "bob"})
    with pytest.raises(ApplicationError, match="Invalid full-state"):
        rehydrate_snapshot({**projected, "version": "other-code"}, instance._identity)


def test_effect_retry_reuses_committed_receipt_and_rejects_payload_change(tmp_path):
    database = tmp_path / "memory.sqlite"
    activities.initialize_database(database)
    activities.configure_database(database, "worker-a")
    activities.bind_workflow(database, "task", "alice")
    info = SimpleNamespace(workflow_id="task", workflow_run_id="run", activity_id="activity", attempt=1)
    request = {"key": "note", "value": "saved", "operation_id": "logical-op"}
    first, _ = activities._put(database, info, request)
    activities.configure_database(database, "worker-c")
    info.attempt = 2
    second, _ = activities._put(database, info, request)
    ledger = activities.inspect_database(database)
    assert first == second
    assert len(ledger["effects"]) == 1 and ledger["effects"][0]["owner"] == "alice"
    assert ledger["effects"][0]["committed_by"] == "worker-a"
    assert [row["worker"] for row in ledger["activity_calls"]] == ["worker-a", "worker-c"]
    assert ledger["activity_calls"][-1]["status"] == "effect_reused"
    with pytest.raises(ApplicationError, match="payload conflict"):
        activities._put(database, info, {**request, "value": "different"})
    with pytest.raises(ApplicationError, match="Unexpected memory"):
        activities._put(database, info, {**request, "owner": "bob"})
    assert activities.inspect_database(database)["memories"][0]["value"] == "saved"


@pytest.mark.asyncio
async def test_foreign_client_rejected_before_any_temporal_request(tmp_path):
    database = tmp_path / "memory.sqlite"
    activities.initialize_database(database)
    activities.bind_workflow(database, "task", "alice")
    client = PrototypeClient(object(), database=database, owner="bob", task_queue="queue")
    for operation in (client.snapshot, client.result, client.cancel):
        with pytest.raises(PermissionError):
            await operation("task")
    with pytest.raises(PermissionError):
        client.task_handle("task")
    with pytest.raises(PermissionError):
        client.release("task")
    with pytest.raises(PermissionError):
        activities.bind_workflow(database, "task", "bob")


@pytest.mark.asyncio
async def test_start_rejects_workflow_id_reuse_and_conflicting_request_locally(tmp_path):
    database = tmp_path / "memory.sqlite"
    activities.initialize_database(database)
    calls = []

    class RecordingClient:
        async def start_workflow(self, *args, **kwargs):
            calls.append(kwargs)
            return SimpleNamespace(first_execution_run_id="first-run", result_run_id="first-run")

    client = PrototypeClient(RecordingClient(), database=database, owner="alice", task_queue="queue")
    await client.start("task", "same", block_after_effect=False)
    await client.start("task", "same", block_after_effect=False)
    assert len(calls) == 2
    assert all(call["id_reuse_policy"] == WorkflowIDReusePolicy.REJECT_DUPLICATE for call in calls)
    with pytest.raises(ValueError, match="different request"):
        await client.start("task", "different", block_after_effect=False)
    assert len(calls) == 2
    foreign = PrototypeClient(RecordingClient(), database=database, owner="bob", task_queue="queue")
    with pytest.raises(PermissionError):
        await foreign.start("task", "same", block_after_effect=False)
    assert len(calls) == 2


def test_new_fixture_instances_resume_from_transcript_without_shared_cursor():
    control = {"prototype_phase": 1, "message": "task", "memory_key": "note", "block_after_effect": True}
    transcript = [HumanMessage(content=json.dumps(control))]
    first = TranscriptModel()._reply(transcript)
    assert first.tool_calls[0]["name"] == "write_todos"
    transcript.extend([first, ToolMessage(content="planned", tool_call_id=first.tool_calls[0]["id"], name="write_todos")])
    resumed = TranscriptModel()._reply(transcript)
    assert resumed.tool_calls[0]["name"] == "write_file"
    assert resumed.tool_calls[0]["args"]["content"] == "plan:task"


@pytest.mark.asyncio
async def test_real_factory_installs_native_async_planning_and_restores_omitted_state(monkeypatch):
    agent = build_agent()
    tools = agent.get_graph().nodes["tools"].data.tools_by_name
    assert {"write_todos", "read_file", "write_file", "memory_put", "memory_get"} <= tools.keys()
    planning = tools["write_todos"]
    assert planning.coroutine is not None
    assert planning.coroutine.__module__ == "langchain.agents.middleware.todo"
    todos = [{"content": "survive continuation", "status": "in_progress"}]

    async def forbidden_thread(*args, **kwargs):
        raise AssertionError("Native planning must not require a thread hop")

    with monkeypatch.context() as local:
        local.setattr("asyncio.to_thread", forbidden_thread)
        command = await planning.coroutine(runtime=SimpleNamespace(tool_call_id="stable-plan"), todos=todos)
    assert command.update["todos"] == todos
    assert command.update["messages"][0].tool_call_id == "stable-plan"

    # PlanningState.todos is OmitFromInput: use the actual checkpoint update API,
    # not an input projection that silently drops the carried planning state.
    config = {"configurable": {"thread_id": "native-planning-restore"}}
    files = {"/plan.md": {"content": "carried file", "encoding": "utf-8"}}
    await restore_agent_state(agent, config, {"messages": [AIMessage(content="carried", id="carried")],
                                              "files": files, "todos": todos})
    snapshot = await agent.aget_state(config)
    restored = snapshot.values
    assert restored["files"] == files and restored["todos"] == todos
    assert restored["messages"][0].id == "carried"
    assert snapshot.next == ()


def test_fixture_retains_controlled_tool_error_diagnostics():
    control = {"prototype_phase": 1, "message": "task", "memory_key": "note", "block_after_effect": True}
    transcript = [HumanMessage(content=json.dumps(control)),
                  ToolMessage(content="write_todos is not a valid tool", status="error",
                              tool_call_id="phase-1-plan", name="write_todos")]
    with pytest.raises(ValueError, match="write_todos is not a valid tool"):
        TranscriptModel()._reply(transcript)


@pytest.mark.asyncio
async def test_native_planning_command_keeps_all_fields_and_stable_result_id():
    native = build_agent("task").get_graph().nodes["tools"].data.tools_by_name["write_todos"]
    todos = [{"content": "native plan", "status": "in_progress"}]
    command = await native.coroutine(runtime=SimpleNamespace(tool_call_id="plan-call"), todos=todos)
    command = replace(command, graph=Command.PARENT, resume={"approved": True}, goto="next")
    command.update["extra"] = {"preserve": [1, 2]}
    original_message = command.update["messages"][0]
    assert original_message.id is None

    async def handler(request):
        return command

    request = SimpleNamespace(tool_call={"id": "plan-call"})
    first = await StableToolMessageIDs("task").awrap_tool_call(request, handler)
    second = await StableToolMessageIDs("task").awrap_tool_call(request, handler)
    assert first == second
    assert first.graph == command.graph and first.resume == command.resume and first.goto == command.goto
    assert first.update["todos"] == todos and first.update["extra"] == command.update["extra"]
    assert first.update["messages"][0].id == "task:tool:plan-call:0"
    assert first.update["messages"][0].tool_call_id == original_message.tool_call_id
    assert first.update["messages"][0].content == original_message.content
    assert command.update["messages"][0].id is None  # native result was not mutated


@pytest.mark.asyncio
async def test_tool_result_stable_id_preserves_content_status_artifact_and_explicit_id():
    original = ToolMessage(content=[{"type": "text", "text": "result"}], tool_call_id="read-call",
                           status="error", name="read_file", artifact={"data": [1, 2]})

    async def handler(request):
        return original

    request = SimpleNamespace(tool_call={"id": "read-call"})
    first = await StableToolMessageIDs("task").awrap_tool_call(request, handler)
    second = await StableToolMessageIDs("task").awrap_tool_call(request, handler)
    assert first == second and first.id == "task:tool:read-call:0"
    assert first.model_dump(exclude={"id"}) == original.model_dump(exclude={"id"})
    assert first.id != (await StableToolMessageIDs("other-task").awrap_tool_call(request, handler)).id
    explicit = original.model_copy(update={"id": "provider-result-id"})
    assert StableToolMessageIDs("task")._result(explicit, "read-call") is explicit


def test_sqlite_context_closes_on_success_and_rolls_back_then_closes_on_error(tmp_path):
    database = tmp_path / "lifecycle.sqlite"
    activities.initialize_database(database)
    with activities._connect(database) as successful:
        successful.execute("INSERT INTO bindings VALUES ('kept', 'alice', NULL)")
    with pytest.raises(sqlite3.ProgrammingError, match="closed"):
        successful.execute("SELECT 1")
    with pytest.raises(RuntimeError, match="rollback"):
        with activities._connect(database) as failed:
            failed.execute("INSERT INTO bindings VALUES ('discarded', 'alice', NULL)")
            raise RuntimeError("rollback")
    with pytest.raises(sqlite3.ProgrammingError, match="closed"):
        failed.execute("SELECT 1")
    assert activities.lookup_owner(database, "kept") == "alice"
    assert activities.lookup_owner(database, "discarded") is None
