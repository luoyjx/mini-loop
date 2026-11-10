"""Content-free request/Activity correlation and deployment identity contracts."""

from __future__ import annotations

from types import SimpleNamespace

import pytest

pytest.importorskip("temporalio")

from mini_loop_deepagents.task_server import observability
from mini_loop_deepagents.task_server.protocol import correlation_metadata

TASK_ID = "task-" + "a" * 64
FIRST_RUN = "11111111-1111-4111-8111-111111111111"
CURRENT_RUN = "22222222-2222-4222-8222-222222222222"
IMAGE = "registry.example/mini-loop@sha256:" + "d" * 64


class Model:
    def __init__(self):
        self.calls = []

    def bind_tools(self, *args, **kwargs):
        self.calls.append(("bind_tools", args, kwargs))
        return self

    def bind(self, *args, **kwargs):
        self.calls.append(("bind", args, kwargs))
        return self

    async def ainvoke(self, messages, *, config=None):
        self.calls.append(("ainvoke", messages, config))
        return "provider-result"


def test_deployment_identity_accepts_only_an_immutable_image_and_current_wire_contract():
    identity = observability.DeploymentIdentity.from_env({observability.IMAGE_REF_ENV: IMAGE})
    assert identity.image_digest == "sha256:" + "d" * 64
    assert identity.protocol_version == "task-v2" and identity.snapshot_schema == 2
    assert observability.DeploymentIdentity.from_env({}).image_digest == "unavailable"
    for value in ("latest", "registry.example/image:tag", "registry.example/image@sha256:ABC"):
        with pytest.raises(ValueError, match="immutable"):
            observability.DeploymentIdentity.from_env({observability.IMAGE_REF_ENV: value})


@pytest.mark.asyncio
async def test_model_activity_attempts_join_task_runs_owner_image_and_protocol_without_content(monkeypatch):
    records = []
    info = SimpleNamespace(
        workflow_id=TASK_ID, workflow_run_id=CURRENT_RUN, activity_id="7",
        activity_type="deepagents.invoke_model", attempt=2,
    )
    monkeypatch.setattr(observability, "_current_activity_info", lambda: info)
    model = Model()
    provider = observability.observe_model_provider(
        lambda name: model, deployment=observability.DeploymentIdentity.from_env(
            {observability.IMAGE_REF_ENV: IMAGE}), worker_identity="worker-host:123",
        sink=records.append,
    )
    wrapped = provider("openai:task-server").bind_tools([{"name": "safe"}]).bind(temperature=0)
    config = {"metadata": correlation_metadata(
        owner="alice", task_id=TASK_ID, first_execution_run_id=FIRST_RUN,
        current_run_id=CURRENT_RUN,
    )}
    result = await wrapped.ainvoke(["private prompt"], config=config)
    assert result == "provider-result"
    assert [record["outcome"] for record in records] == ["started", "completed"]
    for record in records:
        assert record["event"] == "task.model_activity"
        assert record["correlation_status"] == "complete"
        assert record["owner"] == "alice" and record["task_id"] == TASK_ID
        assert record["first_execution_run_id"] == FIRST_RUN
        assert record["current_run_id"] == CURRENT_RUN and record["attempt"] == 2
        assert record["image_digest"] == "sha256:" + "d" * 64
        assert record["protocol_version"] == "task-v2" and record["snapshot_schema"] == 2
        assert len(record["worker_identity_sha256"]) == 64
        assert "private prompt" not in repr(record)


@pytest.mark.asyncio
async def test_missing_or_failing_observation_sink_never_changes_provider_outcome(monkeypatch):
    info = SimpleNamespace(
        workflow_id=TASK_ID, workflow_run_id=CURRENT_RUN, activity_id="8",
        activity_type="deepagents.invoke_model", attempt=1,
    )
    monkeypatch.setattr(observability, "_current_activity_info", lambda: info)

    def broken(_record):
        raise RuntimeError("sink unavailable")

    wrapped = observability.observe_model_provider(
        lambda name: Model(), deployment=observability.DeploymentIdentity.unavailable(),
        worker_identity="worker", sink=broken,
    )("openai:task-server")
    assert await wrapped.ainvoke(["content"], config={}) == "provider-result"


@pytest.mark.asyncio
async def test_new_worker_attempt_after_restart_keeps_the_same_task_and_run_join(monkeypatch):
    records = []
    info = SimpleNamespace(
        workflow_id=TASK_ID, workflow_run_id=CURRENT_RUN, activity_id="9",
        activity_type="deepagents.invoke_model", attempt=1,
    )
    monkeypatch.setattr(observability, "_current_activity_info", lambda: info)
    config = {"metadata": correlation_metadata(
        owner="alice", task_id=TASK_ID, first_execution_run_id=FIRST_RUN,
        current_run_id=CURRENT_RUN,
    )}
    for worker_identity, attempt in (("worker-before", 1), ("worker-after", 2)):
        info.attempt = attempt
        wrapped = observability.observe_model_provider(
            lambda name: Model(), deployment=observability.DeploymentIdentity.unavailable(),
            worker_identity=worker_identity, sink=records.append,
        )("openai:task-server")
        assert await wrapped.ainvoke(["content"], config=config) == "provider-result"
    completed = [row for row in records if row["outcome"] == "completed"]
    assert [row["attempt"] for row in completed] == [1, 2]
    assert len({row["worker_identity_sha256"] for row in completed}) == 2
    assert {(row["task_id"], row["first_execution_run_id"], row["current_run_id"])
            for row in completed} == {(TASK_ID, FIRST_RUN, CURRENT_RUN)}


def test_default_sink_writes_one_bounded_machine_readable_line(capfd):
    observability.default_observation_sink({"schema_version": 1, "event": "task.test", "ready": True})
    assert capfd.readouterr().err == '{"event":"task.test","ready":true,"schema_version":1}\n'
