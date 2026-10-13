"""Public threshold boundaries captured through real Source constructors/loops."""

import json
import os
import socket
from pathlib import Path

from tools.stuck_threshold_contracts import INT64_MAX, INT64_MIN, stuck_threshold_contracts


def test_actual_stuck_threshold_boundaries_and_agent_loops(monkeypatch):
    def deny_network(*args, **kwargs):
        raise AssertionError("stuck contracts must use only the offline fake client")

    monkeypatch.setattr(socket.socket, "connect", deny_network)
    monkeypatch.setattr(socket.socket, "connect_ex", deny_network)
    root = Path(__file__).resolve().parents[2]
    old_fixture = root / "go/testdata/python-loops.json"
    old_bytes, environment = old_fixture.read_bytes(), dict(os.environ)
    observed = stuck_threshold_contracts()
    assert dict(os.environ) == environment
    assert old_fixture.read_bytes() == old_bytes
    assert observed == json.loads((root / "go/testdata/python-stuck-thresholds.json").read_text())
    assert len(observed["source_sha256"]) == 12
    cases = {row["name"]: row for row in observed["cases"]}
    assert len(cases) == len(observed["cases"]) == 61
    assert all(row["constructor_error"] is None for row in cases.values())
    defaults = {"repeat_action_result": 4, "repeat_action_error": 3, "alternating": 6,
                "monologue": 3, "unproductive_tool": 5, "max_nudges": 1}
    for construction in ("omitted", "none", "configured"):
        row = cases[f"default-{construction}"]
        assert row["thresholds"] == defaults
        assert row["signal"]["pattern"] == "repeat_action_result"
        assert row["inspection_error"] is None
    assert cases["configured-all-zero"]["thresholds"] == dict.fromkeys(defaults, 0)
    assert cases["configured-all-zero"]["signal"]["pattern"] == "monologue"
    assert cases["configured-all-zero"]["max_nudges"] == 0

    for field in ("repeat_action_result", "repeat_action_error"):
        for suffix in ("zero-empty", "negative-empty", "negative-exhausted", "int64-min"):
            row = cases[f"{field}-{suffix}"]
            assert row["inspection_error"] == {
                "exception_class": "IndexError", "message": "tuple index out of range", "stage": "inspect"}
            assert row["signal"] is row["reminder"] is None
        for suffix in ("positive-at", "positive-last-window", "zero-single", "zero-identical", "negative-suffix"):
            row = cases[f"{field}-{suffix}"]
            assert row["inspection_error"] is None
            assert row["signal"]["pattern"] == field
            assert row["reminder"].startswith("<stuck")
        for suffix in ("positive-before", "zero-mixed", "negative-mixed-suffix", "int64-max"):
            row = cases[f"{field}-{suffix}"]
            assert row["signal"] is row["inspection_error"] is None
    assert cases["repeat-error-zero-success"]["signal"] is None
    assert "denied" in cases["repeat-error-negative-denied"]["signal"]["detail"]
    for prefix in ("unproductive", "alternating"):
        for suffix in ("negative-disabled", "zero-disabled", "int64-min-disabled", "int64-max"):
            assert cases[f"{prefix}-{suffix}"]["signal"] is None
        assert cases[f"{prefix}-positive-at"]["signal"]["pattern"] == (
            "unproductive_tool" if prefix == "unproductive" else prefix)
        assert cases[f"{prefix}-positive-before"]["signal"] is None
    assert cases["alternating-three-disabled"]["signal"] is None
    assert cases["alternating-positive-odd"]["signal"]["pattern"] == "alternating"
    for suffix in ("negative", "zero", "positive-at", "int64-min"):
        assert cases[f"monologue-{suffix}"]["signal"]["pattern"] == "monologue"
    for suffix in ("positive-before", "int64-max"):
        assert cases[f"monologue-{suffix}"]["signal"] is None
    for suffix, threshold in (("negative", -1), ("zero", 0), ("positive", 1),
                              ("int64-min", INT64_MIN), ("int64-max", INT64_MAX)):
        assert cases[f"max-nudges-{suffix}"]["max_nudges"] == threshold
    for name, pattern in (("precedence-monologue-before-empty-repeat", "monologue"),
                          ("precedence-unproductive-before-empty-result", "unproductive_tool"),
                          ("precedence-error-before-result", "repeat_action_error")):
        assert cases[name]["signal"]["pattern"] == pattern
        assert cases[name]["inspection_error"] is None

    loops = {row["name"]: row for row in observed["loops"]}
    assert len(loops) == len(observed["loops"]) == 12
    for name in ("unproductive-negative", "alternating-negative"):
        turn = loops[name]["turns"][0]
        assert turn["output"] == "done"
        assert turn["model_calls"] == len(turn["requests"]) == 7
        assert not any(event["type"] == "stuck" for event in turn["events"])
    for name, calls in (("default-constructor", 8), ("unproductive-positive", 5),
                        ("alternating-positive", 6), ("max-nudges-negative", 2),
                        ("max-nudges-zero", 2), ("max-nudges-positive", 4),
                        ("monologue-zero", 1), ("configured-all-zero", 1)):
        turn = loops[name]["turns"][0]
        assert turn["model_calls"] == len(turn["requests"]) == calls
        assert turn["output"].startswith("[stopped: ")
        assert "Partial output before the stop:\nloop commentary" in turn["output"]
        assert turn["error"] is turn["transcript_error"] is None
        stuck = [event for event in turn["events"] if event["type"] == "stuck"]
        assert stuck[-1]["halted"] is True
        assert len(stuck) == (2 if name in ("default-constructor", "max-nudges-positive") else 1)
    for name in ("unproductive-negative", "unproductive-positive"):
        assert loops[name]["turns"][0]["handler_commands"] == []
    for name in ("repeat-result-negative-fault", "repeat-error-negative-fault"):
        first, second = loops[name]["turns"]
        assert first["error"] == {
            "exception_class": "IndexError", "message": "tuple index out of range", "stage": "agent_run"}
        assert first["output"] is None
        assert first["model_calls"] == len(first["requests"]) == 1
        assert first["handler_commands"] == ["same"]
        assert len(first["recent_steps"]) == 1
        assert [event["type"] for event in first["events"]] == ["tool_use", "tool_result"]
        assert first["transcript_error"]["exception_class"] == "InvalidTranscript"
        assert first["messages"][-1]["role"] == "assistant"
        # Observe the real validator on the next request, not a synthetic failure
        # or a capture-side history repair. The actual loop decides its output.
        assert len(second["requests"]) == 1
        assert second["requests"][0]["error"]["exception_class"] == "InvalidTranscript"
        assert second["requests"][0]["error"]["stage"] == "provider_create"
        assert second["model_calls"] == 0
        assert second["handler_commands"] == second["recent_steps"] == []
        assert second["transcript_error"]["exception_class"] == "InvalidTranscript"

    managed = {row["name"]: row for row in observed["managed_loops"]}
    assert len(managed) == len(observed["managed_loops"]) == 2
    for name, row in managed.items():
        first, second = row["turns"]
        assert first["error"] == {
            "exception_class": "IndexError", "message": "tuple index out of range", "stage": "session_run"}
        assert first["session_status"] == "error"
        assert first["busy"] is False and first["run_count"] == 1
        assert first["terminal_events"] == [
            {"type": "status", "status": "running"},
            {"type": "error", "error": "IndexError: tuple index out of range"},
        ]
        assert first["handler_commands"] == ["same"]
        assert first["messages"] == loops[name]["turns"][0]["messages"]
        assert first["recent_steps"] == loops[name]["turns"][0]["recent_steps"]
        assert first["transcript_error"] == loops[name]["turns"][0]["transcript_error"]
        assert second["busy"] is False and second["run_count"] == 2
        assert second["requests"] == loops[name]["turns"][1]["requests"]
        assert second["messages"] == loops[name]["turns"][1]["messages"]
        assert second["model_calls"] == 0
