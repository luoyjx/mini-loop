"""The dsh-style trace viewer: a recorded run rendered as a turn ledger.

The viewer is the first place recorded transcript content is turned back into
markup, which makes it the first place a tool result could smuggle live HTML
into an operator's browser. So the properties under test are the harness's
usual ones, applied to a renderer: everything escaped, no fabricated duration
for spans that never closed (copied deliberately from dsh's viewer, which
renders partial rows without inventing a bar), the row cap stated rather than
silent with totals folded before it, unknown event kinds visible rather than
dropped, and the HTTP route scoped exactly like the JSON routes beside it.
"""

import json
import os
import pathlib

import pytest

from mini_loop.trace_view import (
    MAX_ROWS,
    assemble_file,
    build_ledger,
    main,
    render_html,
)

INJECTION = "<script>alert('owned')</script>"


def _trajectory(events, **over):
    base = {
        "trajectory_id": "traj_test", "session": "s1", "run_index": 0,
        "status": "completed", "started_at": 1000.0, "ended_at": 1010.0,
        "duration_ms": 10_000.0, "input": "do the thing",
        "output": "done", "error": None, "metrics": {}, "events": events,
        "partial": False,
    }
    base.update(over)
    return base


def _tool_span(span="tool_a", *, output="ok", result=True, seq=10, usage=None):
    events = [{
        "type": "tool_use", "seq": seq, "ts": 1001.0, "span_id": span,
        "name": "bash", "input": {"command": "echo hi"}, "id": "call_1",
    }]
    if result:
        events.append({
            "type": "tool_result", "seq": seq + 1, "ts": 1002.0,
            "span_id": span, "name": "bash", "output": output,
            "id": "call_1", "error": False, "duration_ms": 320.0,
        })
    return events


def _model_span(span="model_a", *, end=True, seq=1, usage=None):
    events = [{
        "type": "model_start", "seq": seq, "ts": 1000.5, "span_id": span,
        "purpose": "agent_turn", "model": "fake-1", "message_count": 3,
    }]
    if end:
        events.append({
            "type": "model_end", "seq": seq + 1, "ts": 1003.0, "span_id": span,
            "purpose": "agent_turn", "status": "completed",
            "duration_ms": 2500.0, "stop_reason": "end_turn",
            "usage": usage or {"input_tokens": 100, "output_tokens": 20},
        })
    return events


# -- escaping ---------------------------------------------------------------

def test_tool_output_script_is_escaped():
    """Transcript content is untrusted; the page must render it as text."""
    events = _tool_span(output=f"result {INJECTION}")
    events[0]["input"] = {"command": INJECTION}
    page = render_html([build_ledger(_trajectory(
        events, input=f"please {INJECTION}", output=INJECTION,
    ))])
    assert INJECTION not in page
    assert "&lt;script&gt;" in page


# -- span correlation -------------------------------------------------------

def test_tool_call_and_result_fold_into_one_row():
    ledger = build_ledger(_trajectory(_tool_span()))
    tools = [r for r in ledger["rows"] if r["kind"] == "tool"]
    assert len(tools) == 1
    assert tools[0]["status"] == "completed"
    assert tools[0]["duration_ms"] == 320.0
    assert tools[0]["detail"]["Output"] == "ok"


def test_a_denied_result_marks_the_row():
    events = _tool_span()
    events[1]["denied"] = True
    ledger = build_ledger(_trajectory(events))
    row = next(r for r in ledger["rows"] if r["kind"] == "tool")
    assert row["status"] == "denied" and row["error"]


# -- no fabricated duration -------------------------------------------------

def test_an_unfinished_span_reports_in_flight_not_a_duration():
    """A crash leaves open spans; the viewer must not invent their end."""
    events = _model_span(end=False) + _tool_span(result=False, seq=5)
    ledger = build_ledger(_trajectory(
        events, status="interrupted", ended_at=None, duration_ms=None,
        output=None, partial=True,
    ))
    for row in ledger["rows"]:
        if row["kind"] in ("model", "tool"):
            assert row["duration_ms"] is None
            assert row["status"] == "in flight"
    page = render_html([ledger])
    assert "in flight" in page
    # The overview draws a start marker for an open span, never a bar.
    assert "sp model open" in page and "sp tool open" in page


# -- the row cap ------------------------------------------------------------

def test_long_ledger_keeps_tail_and_names_the_omission():
    # Two model spans with usage land FIRST, so the cap cuts their rows.
    events = list(_model_span(span="m1", seq=1))
    events += _model_span(span="m2", seq=3,
                          usage={"input_tokens": 7, "output_tokens": 5})
    events += [
        {"type": "assistant_text", "seq": 100 + i, "ts": 1004.0,
         "text": f"line {i}"}
        for i in range(MAX_ROWS + 50)
    ]
    ledger = build_ledger(_trajectory(events))
    assert len(ledger["rows"]) == MAX_ROWS
    assert ledger["omitted"] > 0
    # The tail survives; the head is what was dropped.
    assert ledger["rows"][-2]["content"] == f"line {MAX_ROWS + 49}"
    # Totals fold over every row BEFORE the cap: the cut spans still count.
    assert ledger["metrics"]["input_tokens"] == 107
    assert ledger["metrics"]["output_tokens"] == 25
    page = render_html([ledger])
    assert "earlier records omitted" in page


# -- unknown kinds stay visible ---------------------------------------------

def test_unknown_event_kinds_still_render():
    """A new event type inherits the need to be seen, not a silent drop."""
    ledger = build_ledger(_trajectory([
        {"type": "quantum_flux", "seq": 4, "ts": 1001.0, "level": 11},
    ]))
    row = next(r for r in ledger["rows"] if r["kind"] == "event")
    assert row["label"] == "quantum_flux"
    assert "quantum_flux" in render_html([ledger])


def test_session_bookkeeping_events_collapse_without_losing_the_answer():
    """`done` mirrors the final row -- unless there is no final row."""
    bookkeeping = [
        {"type": "trajectory_start", "seq": 1, "ts": 1000.1},
        {"type": "done", "seq": 2, "ts": 1009.0, "text": "the answer"},
        {"type": "trajectory_end", "seq": 3, "ts": 1010.0},
    ]
    mirrored = build_ledger(_trajectory(bookkeeping, output="the answer"))
    assert not [r for r in mirrored["rows"] if r["kind"] == "event"]
    assert any(r["kind"] == "final" for r in mirrored["rows"])
    # No assembled output to mirror it -> the done row must survive.
    orphaned = build_ledger(_trajectory(bookkeeping, output=None))
    assert [r["label"] for r in orphaned["rows"] if r["kind"] == "event"] == ["done"]


def test_turn_boundaries_and_steps_are_marked():
    ledgers = [
        build_ledger(_trajectory(_model_span())),
        build_ledger(_trajectory(_model_span(span="m2"), run_index=1)),
    ]
    page = render_html(ledgers)
    assert page.count('<section class="turn">') == 2
    assert "turn 2" in page
    assert "step 1" in page


# -- file assembly and the CLI ----------------------------------------------

def _write_jsonl(path, records):
    path.write_text("".join(json.dumps(r) + "\n" for r in records))


def test_assemble_file_reads_an_export(tmp_path):
    path = tmp_path / "run.jsonl"
    _write_jsonl(path, [
        {"record_type": "trajectory_start", "trajectory_id": "traj_x",
         "session": "s1", "run_index": 0, "started_at": 1000.0,
         "input": "hello"},
        {"record_type": "event", "type": "assistant_text", "seq": 1,
         "ts": 1001.0, "text": "hi"},
    ])
    trajectory = assemble_file(path)
    # No end record: liveness is unknowable from a bare file, so it reads
    # exactly the way the store reports a dead process.
    assert trajectory["status"] == "interrupted"
    assert trajectory["partial"] is True
    assert trajectory["input"] == "hello"
    assert len(trajectory["events"]) == 1


def test_assemble_file_rejects_a_non_trajectory(tmp_path):
    path = tmp_path / "junk.jsonl"
    _write_jsonl(path, [{"record_type": "event", "type": "x"}])
    with pytest.raises(ValueError):
        assemble_file(path)


def test_cli_writes_a_private_html_file(tmp_path, monkeypatch, capsys):
    source = tmp_path / "run.jsonl"
    _write_jsonl(source, [
        {"record_type": "trajectory_start", "trajectory_id": "traj_x",
         "session": "s1", "run_index": 0, "started_at": 1000.0,
         "input": "hello"},
        {"record_type": "event", "type": "assistant_text", "seq": 1,
         "ts": 1001.0, "text": "world"},
    ])
    out = tmp_path / "run.trace.html"
    assert main([str(source), "-o", str(out)]) == 0
    page = out.read_text()
    assert "<!doctype html>" in page and "world" in page
    # The page carries the same content the recording does -> same mode.
    assert (out.stat().st_mode & 0o777) == 0o600


# -- the HTTP route ---------------------------------------------------------

ALICE = {"Authorization": "Bearer tok-alice"}
BOB = {"Authorization": "Bearer tok-bob"}


@pytest.fixture
def client(monkeypatch, tmp_path):
    monkeypatch.setenv("MINILOOP_FAKE_LLM", "1")
    monkeypatch.setenv("MINILOOP_WORKSPACE_ROOT", str(tmp_path / "ws"))
    monkeypatch.setenv("MINILOOP_SKILLS_DIR",
                       str(pathlib.Path(__file__).resolve().parent.parent / "skills"))
    monkeypatch.setenv("MINILOOP_API_TOKENS", "alice:tok-alice,bob:tok-bob")
    monkeypatch.setenv("MINILOOP_TRAJECTORY_ENABLED", "1")
    monkeypatch.setenv("MINILOOP_TRAJECTORY_CAPTURE_CONTENT", "1")

    import importlib

    import mini_loop.server as server

    importlib.reload(server)
    from fastapi.testclient import TestClient

    with TestClient(server.app) as test_client:
        yield test_client


@pytest.fixture
def alices_trajectory(client):
    session_id = client.post("/sessions", json={}, headers=ALICE).json()["id"]
    client.post(f"/sessions/{session_id}/messages",
                json={"message": "ALICE-PRIVATE-QUESTION"}, headers=ALICE)
    listing = client.get(f"/sessions/{session_id}/trajectories",
                         headers=ALICE).json()
    assert listing, "the fixture recorded no trajectory"
    return listing[0]["trajectory_id"]


def test_view_route_renders_for_the_owner(client, alices_trajectory):
    response = client.get(f"/trajectories/{alices_trajectory}/view",
                          headers=ALICE)
    assert response.status_code == 200
    assert response.headers["content-type"].startswith("text/html")
    assert "ALICE-PRIVATE-QUESTION" in response.text
    assert '<section class="turn">' in response.text


def test_view_route_is_scoped_like_its_json_siblings(client, alices_trajectory):
    """Someone else's trajectory is 404, not 403 -- and never a page."""
    response = client.get(f"/trajectories/{alices_trajectory}/view",
                          headers=BOB)
    assert response.status_code == 404
    assert "ALICE-PRIVATE-QUESTION" not in response.text


# -- subagent nesting and request numbering (round 189) ---------------------
# dsh's trajectory ledger indents nested subtool records and numbers every
# request in one chronological space across purposes. Before this round the
# viewer flattened child-agent events into the parent's rows (a subagent's
# bash was indistinguishable from the main agent's) and the child's model
# calls advanced the parent's step counter.

def _nested_events():
    return [
        {"type": "model_start", "seq": 1, "ts": 1000.5, "span_id": "m1",
         "purpose": "agent_turn", "model": "fake", "agent": "main", "depth": 0},
        {"type": "model_start", "seq": 2, "ts": 1001.0, "span_id": "m2",
         "purpose": "agent_turn", "model": "fake",
         "agent": "main>explore", "depth": 1},
        {"type": "tool_use", "seq": 3, "ts": 1001.5, "span_id": "t1",
         "name": "bash", "input": {"command": "ls"}, "id": "c1",
         "agent": "main>explore", "depth": 1},
        {"type": "model_start", "seq": 4, "ts": 1002.0, "span_id": "m3",
         "purpose": "compaction_summary", "model": "fake",
         "agent": "main", "depth": 0},
    ]


def test_subagent_rows_are_nested_not_flattened():
    ledger = build_ledger(_trajectory(_nested_events()))
    child = [r for r in ledger["rows"] if r.get("depth")]
    assert child, "child-agent rows lost their depth"
    assert all(r["agent"] == "main>explore" for r in child)
    page = render_html([ledger])
    assert "padding-left:1.4rem" in page
    assert "main&gt;explore" in page  # the agent chip, escaped


def test_step_markers_ignore_subagent_model_calls():
    ledger = build_ledger(_trajectory(_nested_events()))
    steps = [r["label"] for r in ledger["rows"] if r["kind"] == "step"]
    assert steps == ["step 1"], (
        "a child's model call must not advance the parent's step count"
    )


def test_requests_share_one_numbering_space():
    """Ordinary turns and compaction summaries number chronologically."""
    ledger = build_ledger(_trajectory(_nested_events()))
    labels = [r["label"] for r in ledger["rows"] if r["kind"] == "model"]
    assert [l.split()[0] for l in labels] == ["#1", "#2", "#3"]


def test_steering_renders_as_a_first_class_row():
    """The reader sees the interjection at the position it entered the turn."""
    ledger = build_ledger(_trajectory([
        {"type": "steering_delivered", "seq": 5, "ts": 1002.0,
         "count": 2, "text": "first redirect\n\nsecond redirect"},
    ]))
    row = next(r for r in ledger["rows"] if r["kind"] == "steer")
    assert row["label"] == "steer x2"
    assert "first redirect" in row["content"]
    page = render_html([ledger])
    assert "steer x2" in page and "second redirect" in page


def test_reference_events_render_compactly():
    """Catalog and system-prompt events are reference data: one line in the
    ledger, the payload behind the inspector -- not a schema dump drowning
    the conversation rows."""
    big_schemas = [{"name": f"tool_{i}", "input_schema": {"type": "object"}}
                   for i in range(40)]
    ledger = build_ledger(_trajectory([
        {"type": "tool_catalog", "seq": 2, "ts": 1000.2,
         "fingerprint": "abc123", "schemas": big_schemas},
        {"type": "system_prompt", "seq": 3, "ts": 1000.3,
         "hash": "def456", "text": "You are an agent. " * 200},
    ]))
    catalog, system = [r for r in ledger["rows"] if r["kind"] == "reference"]
    assert catalog["content"] == "40 tools · fingerprint abc123"
    assert system["content"].endswith("hash def456")
    assert "tool_0" not in catalog["content"]
    # The payload survives, one layer down.
    assert catalog["detail"]["Schemas"] == big_schemas
    page = render_html([ledger])
    assert "40 tools" in page
