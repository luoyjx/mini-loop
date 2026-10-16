"""Single Task Workflow contracts; native checkpoint transport is tested apart."""

from __future__ import annotations

import asyncio
from datetime import timedelta
from types import SimpleNamespace

import pytest

pytest.importorskip("temporalio.contrib.deepagents")

from langchain_core.messages import AIMessage, HumanMessage
from langgraph.errors import GraphRecursionError
from temporalio.exceptions import ApplicationError

from mini_loop_deepagents.task_server import runtime
from mini_loop_deepagents.temporal_support.determinism import DeterministicStateBackend, StableToolMessageIDs


PAYLOAD = {"task_id": "task-stable", "owner": "alice", "message": "complete work", "version": "task-v2"}


class Continued(BaseException):
    def __init__(self, continuation):
        self.continuation = continuation


class FakeAgent:
    def __init__(self, saver, target=25):
        self.saver, self.target = saver, target
        saved = saver.get_tuple({"configurable": {"thread_id": "task-stable"}})
        self.position = saved.checkpoint["channel_values"]["position"] if saved else 0

    async def ainvoke(self, initial, config, interrupt_before):
        assert interrupt_before == ["model"]
        assert config["recursion_limit"] == 128
        if initial is None:
            self.position += 1
        else:
            assert len(initial["messages"]) == 1
            assert initial["messages"][0].content == PAYLOAD["message"]
        self.saver.put({"configurable": {"thread_id": "task-stable", "checkpoint_ns": ""}},
                       {"v": 4, "id": f"checkpoint-{self.position:04d}", "ts": "fixed",
                        "channel_values": {"position": self.position}, "channel_versions": {"position": self.position},
                        "versions_seen": {}, "updated_channels": ["position"]},
                       {"source": "loop", "step": self.position, "parents": {}}, {"position": self.position})
        return self.get_state(config).values

    def get_state(self, config):
        pending = ("model",) if self.position < self.target else ()
        task = SimpleNamespace(name="model", error=None, interrupts=(), state=None)
        values = {"messages": [HumanMessage(content="original", id="input"), AIMessage(content=str(self.position), id="reply")],
                  "files": {"/plan": {"content": "kept", "modified_at": "fixed"}},
                  "todos": [{"content": "work", "status": "in_progress" if pending else "completed"}],
                  "_summarization_event": {"cutoff_index": 1, "summary_message": HumanMessage(content="summary", id="summary")},
                  "_summarization_session_id": "stable-summary"}
        return SimpleNamespace(values=values, next=pending, tasks=(task,) if pending else (), interrupts=())


@pytest.fixture
def execution(monkeypatch):
    info = SimpleNamespace(workflow_id="task-stable", first_execution_run_id="first-run", task_queue="queue",
                           execution_timeout=timedelta(hours=24))
    memo = {key: PAYLOAD[key] for key in ("task_id", "owner", "version")}
    memo["request_sha256"] = runtime._request_digest(PAYLOAD, info.task_queue)
    monkeypatch.setattr(runtime.workflow, "info", lambda: info)
    monkeypatch.setattr(runtime.workflow, "memo", lambda: dict(memo))
    monkeypatch.setattr(runtime, "build_agent", lambda workflow_id, saver: FakeAgent(saver))

    def continue_as_new(*, args, **options):
        assert args[0] == PAYLOAD
        assert options == {"memo": memo}
        raise Continued(args[1])

    monkeypatch.setattr(runtime.workflow, "continue_as_new", continue_as_new)
    return info, memo


@pytest.mark.asyncio
async def test_three_continuations_preserve_identity_budget_native_state_and_terminal_result(execution):
    continuation = None
    identities = []
    for index in range(4):
        instance = runtime.TaskWorkflow()
        try:
            result = await instance.run(PAYLOAD, continuation)
        except Continued as carried:
            assert index < 3
            continuation = carried.continuation
            snapshot = instance.snapshot()
            identities.append(snapshot["identity"])
            assert continuation["can_count"] == index + 1
            assert continuation["total_model_boundaries"] == 8 * (index + 1)
            assert snapshot["next"] == ["model"]
            assert snapshot["state"]["_summarization_session_id"] == "stable-summary"
            assert "_summarization_event" in snapshot["state"]
            assert "checkpoint" not in snapshot
        else:
            assert index == 3
            assert result["status"] == "completed"
            assert result["can_count"] == 3
            assert result["total_model_boundaries"] == 25
            assert result["segment_model_boundaries"] == 1
            assert result["next"] == []
            assert result["identity"] == identities[0] == identities[1] == identities[2]
            assert result["identity"]["first_execution_run_id"] == "first-run"


@pytest.mark.asyncio
async def test_total_boundary_budget_cannot_be_reset_by_another_segment(execution, monkeypatch):
    monkeypatch.setattr(runtime, "MAX_MODEL_BOUNDARIES", 3)
    with pytest.raises(ApplicationError) as failure:
        await runtime.TaskWorkflow().run(PAYLOAD)
    assert failure.value.type == "task_step_limit" and failure.value.non_retryable


@pytest.mark.asyncio
@pytest.mark.parametrize("failure, expected", [
    (GraphRecursionError("invocation budget"), "task_step_limit"),
    (runtime.CheckpointLimitError("raw budget"), "task_checkpoint_limit"),
])
async def test_native_step_and_checkpoint_limits_are_terminal(execution, monkeypatch, failure, expected):
    class Agent(FakeAgent):
        async def ainvoke(self, *args, **kwargs):
            raise failure

    monkeypatch.setattr(runtime, "build_agent", lambda workflow_id, saver: Agent(saver))
    with pytest.raises(ApplicationError) as error:
        await runtime.TaskWorkflow().run(PAYLOAD)
    assert error.value.type == expected and error.value.non_retryable


@pytest.mark.asyncio
@pytest.mark.parametrize("failure", [ValueError("framework failure"), asyncio.CancelledError("cancelled")])
async def test_other_failures_and_cancellation_preserve_their_identity(execution, monkeypatch, failure):
    class Agent(FakeAgent):
        async def ainvoke(self, *args, **kwargs):
            raise failure

    monkeypatch.setattr(runtime, "build_agent", lambda workflow_id, saver: Agent(saver))
    with pytest.raises(type(failure)) as error:
        await runtime.TaskWorkflow().run(PAYLOAD)
    assert error.value is failure


@pytest.mark.asyncio
async def test_foreign_continuation_or_memo_never_builds_a_graph(execution, monkeypatch):
    monkeypatch.setattr(runtime, "build_agent", lambda *_: pytest.fail("untrusted graph allocation"))
    with pytest.raises(ApplicationError) as error:
        await runtime.TaskWorkflow().run(PAYLOAD, {"identity": {"owner": "bob"}})
    assert error.value.type == "task_checkpoint_invalid"
    execution[1]["owner"] = "bob"
    with pytest.raises(ApplicationError) as error:
        await runtime.TaskWorkflow().run(PAYLOAD)
    assert error.value.type == "task_input_invalid"


@pytest.mark.asyncio
@pytest.mark.parametrize("duration", [None, timedelta(hours=25)])
async def test_native_total_execution_timeout_is_required_and_never_extended(execution, duration):
    execution[0].execution_timeout = duration
    with pytest.raises(ApplicationError) as error:
        await runtime.TaskWorkflow().run(PAYLOAD)
    assert error.value.type == "task_input_invalid"


def test_only_a_single_pending_model_is_a_safe_continuation_boundary():
    instance = runtime.TaskWorkflow()
    for pending in ((), ("tools",), ("model", "tools")):
        with pytest.raises(ApplicationError) as error:
            instance._require_model_boundary(SimpleNamespace(next=pending, interrupts=(), tasks=()))
        assert error.value.type == "task_checkpoint_boundary"


def test_task_input_rejects_provider_owner_version_and_message_ambiguity():
    assert runtime.validate_payload(PAYLOAD) == PAYLOAD
    for payload in (None, {**PAYLOAD, "model": "other:model"}, {**PAYLOAD, "owner": " "},
                    {**PAYLOAD, "version": "task-v1"}, {**PAYLOAD, "message": 1},
                    {**PAYLOAD, "message": "x" * (16 * 1024 + 1)},
                    {**PAYLOAD, "message": "\ud800"},
                    {key: value for key, value in PAYLOAD.items() if key != "task_id"}):
        with pytest.raises(ApplicationError) as error:
            runtime.validate_payload(payload)
        assert error.value.type == "task_input_invalid" and error.value.non_retryable


def test_real_task_factory_has_native_planning_files_and_no_prototype_memory(monkeypatch):
    actual_factory = runtime.create_temporal_deep_agent
    captured = {}

    def recording_factory(**kwargs):
        captured.update(kwargs)
        return actual_factory(**kwargs)

    monkeypatch.setattr(runtime, "create_temporal_deep_agent", recording_factory)
    saver = runtime.PortableSaver("stable-task")
    agent = runtime.build_agent("stable-task", saver)
    tools = agent.get_graph().nodes["tools"].data.tools_by_name
    assert {"write_todos", "write_file", "read_file", "edit_file"} <= tools.keys()
    assert not {"memory_put", "memory_get"} & tools.keys()
    assert tools["write_todos"].coroutine.__module__ == "langchain.agents.middleware.todo"
    assert captured["model"].model == runtime.MODEL_ALIAS
    assert isinstance(captured["backend"], DeterministicStateBackend)
    assert isinstance(captured["middleware"][0], StableToolMessageIDs)
    assert captured["checkpointer"] is saver
    options = captured["model"].activity_options
    assert options["heartbeat_timeout"].total_seconds() == 5
    assert options["retry_policy"].maximum_attempts == 3
