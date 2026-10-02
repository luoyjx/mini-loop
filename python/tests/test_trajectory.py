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
    }, risk="read")
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


def test_a_listing_does_not_read_the_event_bodies_it_discards(tmp_path):
    """`summary()` needs the header and the terminal metrics, never the event
    stream -- yet it built the full `get()` representation (every event body in
    a list) and threw the events away. So `list()`, and `count()` on every
    session construction, cost O(recorded content), not O(trajectory count):
    the server reads *every* trajectory on the box to build the listing before
    filtering by caller, so one tenant's oversized recording loaded its whole
    body into memory to summarise -- amplifying one caller's data into
    everyone's listing. The summary now streams: one record resident at a time.
    """
    import tracemalloc

    store = TrajectoryStore(tmp_path / "trajectories")
    trajectory_id = store.start(session_id="s", run_index=1, input_text="hi")
    big = "X" * 20_000
    for _ in range(1500):  # ~30 MB of event bodies a summary must not load
        store.append(trajectory_id, {"type": "tool_result", "content": big})
    store.finish(trajectory_id, status="completed", output="done", duration_ms=1.0)
    body_bytes = (tmp_path / "trajectories" / f"{trajectory_id}.jsonl").stat().st_size

    tracemalloc.start()
    summaries = store.list()
    _current, peak = tracemalloc.get_traced_memory()
    tracemalloc.stop()

    assert peak < body_bytes // 10, (
        f"list() held {peak:,} bytes to summarise a {body_bytes:,}-byte body"
    )
    # The summary is still correct: metrics come from the stored end record.
    assert summaries[0]["metrics"]["event_count"] == 1500
    assert summaries[0]["status"] == "completed"


def test_the_streamed_summary_matches_the_full_representation(tmp_path):
    """Streaming must not change *what* a summary says -- only how it is read.

    Pins the equivalence for both a finished trajectory (metrics from the end
    record) and a still-open one (metrics counted while streaming), so a future
    change to either path cannot drift the two apart unnoticed.
    """
    store = TrajectoryStore(tmp_path / "trajectories")

    done = store.start(session_id="a", run_index=1, input_text="q" * 300,
                       metadata={"model": "m1"})
    for index in range(10):
        store.append(done, {"type": "tool_use"})
        store.append(done, {"type": "tool_result", "error": bool(index % 2)})
        store.append(done, {"type": "model_start"})
        store.append(done, {"type": "error"})
    store.finish(done, status="completed", output="ok", duration_ms=9.0)

    running = store.start(session_id="b", run_index=1, input_text="short")
    store.append(running, {"type": "tool_use"})
    store.append(running, {"type": "model_start"})

    def from_full(trajectory_id):
        full = store.get(trajectory_id)
        preview = full.get("input")
        if isinstance(preview, str) and len(preview) > 160:
            preview = preview[:159] + "…"
        row = {
            key: full[key]
            for key in (
                "id", "trajectory_id", "trace_id", "group_id", "session", "owner",
                "run_index", "status", "started_at", "ended_at", "duration_ms",
                "metrics", "partial",
            )
        }
        row["input_preview"] = preview
        row["model"] = (full.get("metadata") or {}).get("model")
        row["workspace"] = (full.get("metadata") or {}).get("workspace")
        row["build"] = (full.get("metadata") or {}).get("build")
        return row

    for trajectory_id in (done, running):
        assert store.summary(trajectory_id) == from_full(trajectory_id)


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
