"""Light checks for the optional real Task harness; no Temporal service starts."""

from __future__ import annotations

import asyncio
import json
from types import SimpleNamespace

import pytest

pytest.importorskip("temporalio.contrib.deepagents")

from langchain_core.messages import AIMessage, HumanMessage, ToolMessage
from langgraph.errors import GraphRecursionError
from temporalio.exceptions import ApplicationError

from mini_loop_deepagents.config import AgentSettings
from mini_loop_deepagents.task_server import runtime, worker
from mini_loop_deepagents.temporal_prototype.workflow import DeterministicStateBackend, StableToolMessageIDs
from task_worker_fixture import TaskFixtureModel, barrier_paths, make_fixture_provider


def test_task_input_rejects_provider_or_owner_ambiguity():
    trusted = {"task_id": "task-1", "owner": "alice", "message": "make a plan", "version": "task-v1"}
    assert runtime.validate_payload(trusted) == trusted
    for payload in (None, {**trusted, "model": "other:model"}, {**trusted, "owner": " "},
                    {**trusted, "version": "future"}, {**trusted, "message": 1},
                    {key: value for key, value in trusted.items() if key != "task_id"}):
        with pytest.raises(ApplicationError):
            runtime.validate_payload(payload)


def test_real_task_factory_has_native_planning_files_and_no_prototype_memory(monkeypatch):
    actual_factory = runtime.create_temporal_deep_agent
    captured = {}

    def recording_factory(**kwargs):
        captured.update(kwargs)
        return actual_factory(**kwargs)

    monkeypatch.setattr(runtime, "create_temporal_deep_agent", recording_factory)
    agent = runtime.build_agent("stable-task")
    tools = agent.get_graph().nodes["tools"].data.tools_by_name
    assert {"write_todos", "write_file", "read_file", "edit_file"} <= tools.keys()
    assert not {"memory_put", "memory_get"} & tools.keys()
    assert tools["write_todos"].coroutine.__module__ == "langchain.agents.middleware.todo"
    assert captured["model"] == runtime.MODEL_ALIAS
    assert isinstance(captured["backend"], DeterministicStateBackend)
    assert isinstance(captured["middleware"][0], StableToolMessageIDs)
    options = captured["activity_options"]
    assert options["heartbeat_timeout"].total_seconds() == 5
    assert options["retry_policy"].maximum_attempts == 3


def test_model_alias_resolves_only_deployment_settings_and_disables_nested_retry(monkeypatch):
    captures = []
    monkeypatch.setattr(worker, "ChatOpenAI", lambda **kwargs: captures.append(kwargs) or "real-model")
    provider = worker.make_model_provider(AgentSettings(model="openai:deployment-owned", max_tokens=1234,
                                                       timeout_seconds=19))
    assert provider("openai:task-server") == "real-model"
    assert captures == [{"model": "deployment-owned", "max_tokens": 1234, "timeout": 19, "max_retries": 0}]
    with pytest.raises(ValueError, match="deployment-owned"):
        provider("openai:user-selected")


@pytest.mark.asyncio
async def test_workflow_passes_plain_human_and_bounded_config_then_projects_real_state(monkeypatch):
    captured = {}
    state = {
        "messages": [HumanMessage(content="original", id="stable:user-input"),
                     AIMessage(content="done", id="stable:final")],
        "files": {"/plan.md": {"content": "result", "encoding": "utf-8"}},
        "todos": [{"content": "task", "status": "completed"}],
    }

    class Agent:
        async def ainvoke(self, initial, config):
            captured.update(initial=initial, config=config)
            return state

        def get_state(self, config):
            return SimpleNamespace(values=state)

    monkeypatch.setattr(runtime.workflow, "info", lambda: SimpleNamespace(workflow_id="stable"))
    monkeypatch.setattr(runtime, "build_agent", lambda workflow_id: Agent())
    instance = runtime.TaskWorkflow()
    result = await instance.run({"task_id": "stable", "owner": "alice", "message": "original", "version": "task-v1"})
    message = captured["initial"]["messages"][0]
    assert message.content == "original" and message.id == "stable:user-input"
    assert captured["config"] == {"configurable": {"thread_id": "stable"}, "recursion_limit": 128}
    assert result == instance.snapshot()
    assert result["identity"] == {"task_id": "stable", "owner": "alice", "workflow_id": "stable"}
    assert result["status"] == "completed"
    assert result["files"] == state["files"] and result["todos"] == state["todos"]
    assert json.loads(json.dumps(result))["messages"][-1]["data"]["id"] == "stable:final"


@pytest.mark.asyncio
async def test_workflow_refuses_mismatched_identity_before_building_agent(monkeypatch):
    monkeypatch.setattr(runtime.workflow, "info", lambda: SimpleNamespace(workflow_id="actual"))
    monkeypatch.setattr(runtime, "build_agent", lambda _: pytest.fail("agent allocated before identity check"))
    with pytest.raises(ApplicationError, match="identity"):
        await runtime.TaskWorkflow().run({"task_id": "other", "owner": "alice", "message": "x", "version": "task-v1"})


@pytest.mark.asyncio
async def test_graph_step_limit_becomes_nonretryable_task_failure(monkeypatch):
    class Agent:
        async def ainvoke(self, initial, config):
            raise GraphRecursionError("framework step budget exhausted")

    monkeypatch.setattr(runtime.workflow, "info", lambda: SimpleNamespace(workflow_id="stable"))
    monkeypatch.setattr(runtime, "build_agent", lambda _: Agent())
    with pytest.raises(ApplicationError) as failure:
        await runtime.TaskWorkflow().run({"task_id": "stable", "owner": "alice", "message": "x", "version": "task-v1"})
    assert failure.value.type == "task_step_limit"
    assert failure.value.non_retryable is True


@pytest.mark.asyncio
@pytest.mark.parametrize("failure", [ValueError("other framework failure"), asyncio.CancelledError("cancelled")])
async def test_step_limit_adapter_preserves_other_exceptions_and_cancellation(monkeypatch, failure):
    class Agent:
        async def ainvoke(self, initial, config):
            raise failure

    monkeypatch.setattr(runtime.workflow, "info", lambda: SimpleNamespace(workflow_id="stable"))
    monkeypatch.setattr(runtime, "build_agent", lambda _: Agent())
    with pytest.raises(type(failure)) as raised:
        await runtime.TaskWorkflow().run({"task_id": "stable", "owner": "alice", "message": "x", "version": "task-v1"})
    assert raised.value is failure


def test_production_model_configuration_is_validated_when_worker_is_composed(monkeypatch):
    from openai import OpenAIError
    monkeypatch.delenv("OPENAI_API_KEY", raising=False)
    with pytest.raises((ValueError, OpenAIError)):
        worker.make_model_provider(AgentSettings(model="openai:deployment-owned"))


def test_test_only_fixture_progress_is_transcript_based_and_barrier_names_are_not_prompt_paths(tmp_path):
    model = TaskFixtureModel(barrier_directory=str(tmp_path))
    messages = [HumanMessage(content="../../host-secret", id="input")]
    for call_id, name in (("task-plan", "write_todos"), ("task-write", "write_file"), ("task-read", "read_file")):
        reply, blocked = model._reply(messages)
        assert reply.tool_calls[0]["id"] == call_id and reply.tool_calls[0]["name"] == name
        assert blocked is False
        messages.extend([reply, ToolMessage(content="ok", tool_call_id=call_id, name=name)])
    # A new worker/model starts from the same durable transcript, with no cursor.
    resumed = make_fixture_provider(str(tmp_path))(runtime.MODEL_ALIAS)
    reply, blocked = resumed._reply(messages)
    assert reply.tool_calls[0]["name"] == "write_todos" and blocked is True
    marker, release = barrier_paths(tmp_path, "../../workflow")
    assert marker.parent == release.parent == tmp_path
    assert len(marker.name.removesuffix(".blocked.json")) == 64


@pytest.mark.asyncio
async def test_worker_rejects_resource_slots_before_connecting():
    with pytest.raises(ValueError, match="slots"):
        await worker.serve("unused", "unused", "unused", activity_slots=0)
