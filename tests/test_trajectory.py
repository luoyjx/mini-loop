"""Durable trajectory recording, correlation, redaction, and recovery tests."""

import asyncio
import json
from pathlib import Path

import pytest

from mini_loop import SessionManager, Settings, TrajectoryStore, default_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, tool

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def test_trajectory_store_round_trip_and_partial_recovery(tmp_path):
    store = TrajectoryStore(tmp_path / "trajectories")
    trajectory_id = store.start(
        session_id="session-a",
        run_index=1,
        input_text="inspect the repository",
        metadata={"model": "test-model"},
    )
    store.append(trajectory_id, {
        "seq": 1,
        "ts": 10.0,
        "session": "session-a",
        "type": "tool_use",
        "name": "read_file",
    })

    partial = store.get(trajectory_id)
    assert partial["status"] == "running" and partial["partial"] is True
    recovered = TrajectoryStore(tmp_path / "trajectories").get(trajectory_id)
    assert recovered["status"] == "interrupted" and recovered["partial"] is True

    store.finish(
        trajectory_id,
        status="completed",
        output="done",
        duration_ms=25.5,
    )
    recorded = store.get(trajectory_id)
    assert recorded["schema_version"] == "mini-loop.trajectory.v1"
    assert recorded["trace_id"] == trajectory_id
    assert recorded["group_id"] == "session-a"
    assert recorded["status"] == "completed" and recorded["partial"] is False
    assert recorded["metrics"]["tool_calls"] == 1
    assert store.list(session_id="session-a")[0]["id"] == trajectory_id
    assert [json.loads(line)["record_type"] for line in store.raw(trajectory_id).splitlines()] == [
        "trajectory_start", "event", "trajectory_end",
    ]


def test_trajectory_store_can_redact_sensitive_content(tmp_path):
    store = TrajectoryStore(tmp_path / "trajectories", capture_content=False)
    trajectory_id = store.start(
        session_id="session-a",
        run_index=1,
        input_text="secret user request",
        metadata={"system": "secret system prompt", "model": "test-model"},
    )
    store.append(trajectory_id, {
        "seq": 1,
        "type": "tool_result",
        "input": {"token": "secret"},
        "model_input": {"messages": [{"role": "user", "content": "secret"}]},
        "output": "secret tool output",
    })
    store.finish(trajectory_id, status="completed", output="secret final")

    recorded = store.get(trajectory_id)
    assert recorded["input"].startswith("[redacted:")
    assert recorded["metadata"]["system"].startswith("[redacted:")
    assert recorded["events"][0]["input"].startswith("[redacted:")
    assert recorded["events"][0]["model_input"].startswith("[redacted:")
    assert recorded["events"][0]["output"].startswith("[redacted:")
    assert recorded["output"].startswith("[redacted:")
    assert "secret" not in store.raw(trajectory_id)


def test_session_records_correlated_model_and_tool_steps(tmp_path):
    settings = Settings(
        model="test-model",
        workspace_root=tmp_path / "workspaces",
        skills_dir=SKILLS_DIR,
        trajectory_root=tmp_path / "trajectories",
        trajectory_enabled=True,
    )

    async def main():
        manager = SessionManager(settings, FakeAsyncAnthropic())
        session = manager.create()
        final = await session.run("trace this run")
        return manager, session, final

    manager, session, final = asyncio.run(main())
    summaries = manager.trajectories.list(session_id=session.id)
    assert final.startswith("Done.") and len(summaries) == 1
    trajectory = manager.trajectories.get(summaries[0]["id"])
    types = [event["type"] for event in trajectory["events"]]
    assert types[0:2] == ["trajectory_start", "status"]
    assert "trajectory_end" in types and types[-1] == "done"
    assert types.count("model_start") == types.count("model_end") == 2
    assert trajectory["status"] == "completed"
    assert trajectory["metrics"]["tool_calls"] == 1

    tool_use = next(event for event in trajectory["events"] if event["type"] == "tool_use")
    tool_result = next(event for event in trajectory["events"] if event["type"] == "tool_result")
    assert tool_use["id"] == tool_result["id"]
    assert tool_use["span_id"] == tool_result["span_id"]
    assert tool_use["parent_span_id"].startswith("model_")
    assert all(
        event["trajectory_id"] == trajectory["id"]
        for event in trajectory["events"]
    )
    assert session.info()["trajectory_count"] == 1
    assert session.info()["active_trajectory_id"] is None


def test_trajectory_keeps_full_details_while_live_events_stay_bounded(tmp_path):
    settings = Settings(
        model="test-model",
        workspace_root=tmp_path / "workspaces",
        skills_dir=SKILLS_DIR,
        trajectory_root=tmp_path / "trajectories",
    )
    registry = default_registry()
    full_output = "observation-" * 500

    @registry.add("long_observation", "Return a long observation.", {
        "type": "object", "properties": {},
    })
    async def long_observation(_ctx):
        return full_output

    async def main():
        client = FakeAsyncAnthropic(responder=scripted([
            ([tool("long_observation", _id="toolu_long")], "tool_use"),
        ]))
        manager = SessionManager(settings, client, tool_registry=registry)
        session = manager.create()
        await session.run("capture the complete observation")
        return manager, session

    manager, session = asyncio.run(main())
    trajectory_id = manager.trajectories.list(session_id=session.id)[0]["id"]
    recorded = manager.trajectories.get(trajectory_id)["events"]
    live_tool_result = next(
        event for event in session._backlog if event["type"] == "tool_result"
    )
    recorded_tool_result = next(
        event for event in recorded if event["type"] == "tool_result"
    )
    assert len(live_tool_result["output"]) == 2000
    assert recorded_tool_result["output"] == full_output

    live_model_start = next(
        event for event in session._backlog if event["type"] == "model_start"
    )
    recorded_model_starts = [
        event for event in recorded if event["type"] == "model_start"
    ]
    recorded_model_start = recorded_model_starts[0]
    assert "model_input" not in live_model_start
    assert recorded_model_start["model_input"]["messages"]
    second_input = recorded_model_starts[1]["model_input"]["messages"]
    assert any(
        part.get("type") == "tool_result" and part.get("content") == full_output
        for message in second_input if isinstance(message.get("content"), list)
        for part in message["content"]
    )


def test_unexpected_session_failure_closes_the_trajectory(tmp_path):
    settings = Settings(
        model="test-model",
        workspace_root=tmp_path / "workspaces",
        skills_dir=SKILLS_DIR,
        trajectory_root=tmp_path / "trajectories",
    )

    async def main():
        manager = SessionManager(settings, FakeAsyncAnthropic())
        session = manager.create()

        async def explode(_message):
            raise RuntimeError("boom")

        session.agent.run = explode
        with pytest.raises(RuntimeError, match="boom"):
            await session.run("fail safely")
        return manager, session

    manager, session = asyncio.run(main())
    summary = manager.trajectories.list(session_id=session.id)[0]
    trajectory = manager.trajectories.get(summary["id"])
    assert trajectory["status"] == "error" and trajectory["partial"] is False
    assert trajectory["error"] == "RuntimeError: boom"
    assert trajectory["events"][-1]["type"] == "error"
    assert trajectory["metrics"]["errors"] == 1
