"""The capability plan: what could EXECUTE, not only what tools existed.

Codex compiles a capability plan per turn and asks "what did this turn
compile?" (OPENAI_CODEX_HARNESS_RESEARCH.md section 7). mini-loop already
compiled the catalog per round and, since rounds 197/198, logged it -- but
the catalog fingerprint cannot tell a readonly request from an auto one:
permission_mode flips mid-session while the catalog stays identical, and
two requests with different effective capabilities carried the same
recorded identity. The plan fingerprint closes that: catalog fingerprint
x permission mode x sandbox posture, one durable event per distinct plan,
referenced from every model_start and joined by reconstruct_request.
"""

import asyncio
import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.storage import SQLiteStateStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path, responder=None):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(responder=responder) if responder else FakeAsyncAnthropic(),
        state_store=SQLiteStateStore(tmp_path / "state.db"),
    )


def test_every_agent_turn_references_one_deduped_plan(tmp_path):
    responder = scripted([
        ([text("a"), tool("bash", _id="t1", command="echo 1")], "tool_use"),
        ([text("b"), tool("bash", _id="t2", command="echo 2")], "tool_use"),
        ([text("done")], "end_turn"),
    ])
    manager = _manager(tmp_path, responder)
    session = manager.create()
    asyncio.run(session.run("three rounds"))

    events = manager.state_store.load_events(session.id)
    starts = [e for e in events if e.get("type") == "model_start"]
    plans = [e for e in events if e.get("type") == "capability_plan"]
    assert len(starts) == 3
    assert len(plans) == 1, "an unchanged plan re-logged per round"
    assert all(
        s.get("capability_fingerprint") == plans[0]["fingerprint"]
        for s in starts
    )
    assert plans[0]["permission_mode"] == "interactive"


def test_a_permission_flip_changes_the_recorded_identity(tmp_path):
    """The Identity gap this round closes: same catalog, different powers,
    distinguishable requests."""
    manager = _manager(tmp_path)
    session = manager.create()
    asyncio.run(session.run("first"))
    session.permission_mode = "auto"
    asyncio.run(session.run("second"))

    events = manager.state_store.load_events(session.id)
    plans = [e for e in events if e.get("type") == "capability_plan"]
    assert len(plans) == 2
    assert plans[0]["permission_mode"] == "interactive"
    assert plans[1]["permission_mode"] == "auto"
    assert plans[0]["fingerprint"] != plans[1]["fingerprint"]


def test_reconstruction_names_the_powers_in_force(tmp_path):
    from mini_loop.session_query import reconstruct_request

    manager = _manager(tmp_path)
    session = manager.create()
    asyncio.run(session.run("before the flip"))
    session.permission_mode = "auto"
    asyncio.run(session.run("after the flip"))

    events = manager.state_store.load_events(session.id)
    starts = [e for e in events if e.get("type") == "model_start"]
    before = reconstruct_request(manager.state_store, session.id, starts[0]["seq"])
    after = reconstruct_request(manager.state_store, session.id, starts[-1]["seq"])
    assert before["capability"]["permission_mode"] == "interactive"
    assert after["capability"]["permission_mode"] == "auto"


def test_the_viewer_renders_the_plan_compactly(tmp_path):
    from mini_loop.trace_view import build_ledger, render_html

    ledger = build_ledger({
        "trajectory_id": "traj_x", "input": "go", "status": "completed",
        "started_at": 1000.0, "ended_at": 1010.0, "duration_ms": 10_000.0,
        "output": "done", "error": None, "metrics": {}, "partial": False,
        "events": [
            {"type": "capability_plan", "seq": 2, "ts": 1000.2,
             "fingerprint": "cafe1234", "permission_mode": "readonly",
             "sandbox": "SeatbeltSandbox", "sandbox_confined": True,
             "catalog_fingerprint": "abc"},
        ],
    })
    row = next(r for r in ledger["rows"] if r["label"] == "capability")
    assert row["content"] == "readonly · confined · cafe1234"
    assert "readonly · confined" in render_html([ledger])
