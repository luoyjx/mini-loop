"""Worker composition and trusted fixture contracts; no service is started."""

from __future__ import annotations

import pytest

pytest.importorskip("temporalio.contrib.deepagents")

from langchain_core.messages import HumanMessage, ToolMessage

from mini_loop_deepagents.config import AgentSettings
from mini_loop_deepagents.task_server import runtime, worker
from tooling.acceptance.fixtures.task_worker_fixture import TaskFixtureModel, barrier_paths, make_fixture_provider


def test_model_alias_resolves_only_deployment_settings_and_disables_nested_retry(monkeypatch):
    captures = []
    monkeypatch.setattr(worker, "ChatOpenAI", lambda **kwargs: captures.append(kwargs) or "real-model")
    provider = worker.make_model_provider(AgentSettings(model="openai:deployment-owned", max_tokens=1234,
                                                       timeout_seconds=19))
    assert provider("openai:task-server") == "real-model"
    assert captures == [{"model": "deployment-owned", "max_tokens": 1234, "timeout": 19, "max_retries": 0}]
    with pytest.raises(ValueError, match="deployment-owned"):
        provider("openai:user-selected")


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
