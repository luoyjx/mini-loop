"""P1.4 live-provider gate is deterministic offline and fail-closed online."""

from __future__ import annotations

import copy
from datetime import datetime, timezone
import json
from pathlib import Path
import stat
from uuid import UUID

import pytest

from tooling.acceptance import live_provider
from tooling.acceptance.production_profile import load_profile


CASES = Path(__file__).parents[1] / "docs" / "PRODUCTION_PROVIDER_CASES.json"
PROFILE = Path(__file__).parents[1] / "docs" / "PRODUCTION_ACCEPTANCE_PROFILE.json"
COMMIT = "a" * 40
DIGEST = "sha256:" + "b" * 64
TASK = "task-" + "c" * 64
RUNS = ["11111111-1111-4111-8111-111111111111",
        "22222222-2222-4222-8222-222222222222",
        "33333333-3333-4333-8333-333333333333"]


def approval_value(**updates):
    value = {
        "schema_version": 1,
        "kind": live_provider.APPROVAL_KIND,
        "approved": True,
        "approved_by": "operator-a",
        "approved_at": "2026-11-10T00:00:00Z",
        "expires_at": "2026-11-10T01:00:00Z",
        "source_commit": COMMIT,
        "image_digest": DIGEST,
        "case_ids": ["planning-files-subagents-summary-can-v1"],
        "model": "openai:gpt-4.1-mini",
        "environment": {
            "provider": "openai",
            "region": "region-a",
            "temporal_namespace": "namespace-a",
            "data_residency": "region-a",
            "license": "confirmed",
        },
        "budget": {
            "max_provider_calls": 64,
            "max_total_tokens": 200000,
            "max_cost_cents": 1000,
            "calendar_month_spend_before_cents": 0,
        },
    }
    value.update(updates)
    return value


def private_json(tmp_path, value, name="approval.json"):
    path = tmp_path / name
    path.write_text(json.dumps(value), encoding="utf-8")
    path.chmod(0o600)
    return path


def test_committed_cases_are_fixed_and_cover_every_p1_4_axis():
    cases = live_provider.load_cases(CASES)
    case = cases["planning-files-subagents-summary-can-v1"]
    expected = case["expected"]

    assert case["model"] == "openai:gpt-4.1-mini"
    assert expected["minimum_can_count"] >= 2
    assert expected["minimum_model_boundaries"] >= 20
    assert expected["require_summary"] is True
    assert expected["expected_trace_tool_runs"]["task"] == 2
    assert expected["expected_trace_tool_runs"]["write_file"] == 19
    assert {"write_file", "read_file", "write_todos"} <= expected["expected_trace_tool_runs"].keys()
    assert {"/acceptance/child-a.txt", "/acceptance/child-b.txt",
            "/acceptance/parent.txt"} <= expected["required_files"].keys()


def test_check_mode_is_ready_but_never_claims_live_authority(capsys):
    assert live_provider.main(["check", "--profile", str(PROFILE), "--cases", str(CASES)]) == 0
    value = json.loads(capsys.readouterr().out)

    assert value["status"] == "ready"
    assert value["provider_execution_authorized"] is False
    assert value["operator_approval_required"] is True
    assert value["live_execution"] == "not-authorized-by-committed-profile"
    assert value["unknown_usage_action"] == "stop-and-mark-inconclusive-never-count-as-zero"


def test_live_run_requires_explicit_environment_acknowledgement_before_files(monkeypatch, capsys):
    monkeypatch.delenv("MINILOOP_TASK_ACCEPTANCE_REAL", raising=False)
    code = live_provider.main([
        "run", "--approval-file", "/does/not/exist", "--api-base-url", "https://task.invalid",
        "--task-token-file", "/does/not/exist", "--langsmith-project", "project",
        "--langsmith-api-key-file", "/does/not/exist", "--output-dir", "/does/not/exist",
    ])
    assert code == 2
    assert json.loads(capsys.readouterr().out) == {
        "status": "blocked", "code": "execution_acknowledgement_required",
    }


def test_live_run_rejects_replacement_case_manifest_before_credentials(tmp_path, monkeypatch, capsys):
    replacement = tmp_path / "cases.json"
    replacement.write_bytes(CASES.read_bytes())
    monkeypatch.setenv("MINILOOP_TASK_ACCEPTANCE_REAL", live_provider.ACKNOWLEDGEMENT)
    code = live_provider.main([
        "run", "--cases", str(replacement), "--approval-file", "/does/not/exist",
        "--api-base-url", "https://task.invalid", "--task-token-file", "/does/not/exist",
        "--langsmith-project", "project", "--langsmith-api-key-file", "/does/not/exist",
        "--output-dir", "/does/not/exist",
    ])
    assert code == 2
    assert json.loads(capsys.readouterr().out) == {
        "status": "blocked", "code": "committed_contract_required",
    }


def test_short_lived_approval_binds_clean_commit_cases_model_and_budget(tmp_path, monkeypatch):
    cases = live_provider.load_cases(CASES)
    profile = load_profile(PROFILE)
    monkeypatch.setattr(live_provider, "_git_state", lambda: (COMMIT, False))
    path = private_json(tmp_path, approval_value())

    approval = live_provider.load_approval(
        path, cases, profile, now=datetime(2026, 11, 10, 0, 30, tzinfo=timezone.utc),
    )
    assert approval["source_commit"] == COMMIT
    assert approval["budget"]["max_provider_calls"] == 64


@pytest.mark.parametrize("mutation,code", [
    (lambda value: value.update(approved=False), "operator_approval_required"),
    (lambda value: value["budget"].update(max_cost_cents=1001), "approval_budget"),
    (lambda value: value["budget"].update(calendar_month_spend_before_cents=4500),
     "approval_monthly_budget"),
    (lambda value: value.update(source_commit="d" * 40), "approval_source_mismatch"),
    (lambda value: value["environment"].update(provider="other"), "approval_environment"),
])
def test_approval_rejects_missing_authority_or_budget_drift(tmp_path, monkeypatch, mutation, code):
    cases = live_provider.load_cases(CASES)
    profile = load_profile(PROFILE)
    monkeypatch.setattr(live_provider, "_git_state", lambda: (COMMIT, False))
    value = approval_value()
    mutation(value)
    path = private_json(tmp_path, value)

    with pytest.raises(live_provider.LiveProviderError, match=code):
        live_provider.load_approval(
            path, cases, profile, now=datetime(2026, 11, 10, 0, 30, tzinfo=timezone.utc),
        )


def test_private_credentials_require_regular_owner_only_single_line_files(tmp_path):
    secret = tmp_path / "secret"
    secret.write_text("synthetic-key\n", encoding="ascii")
    secret.chmod(0o600)
    assert live_provider._read_secret(secret) == "synthetic-key"

    secret.chmod(0o644)
    with pytest.raises(live_provider.LiveProviderError, match="secret_file_invalid"):
        live_provider._read_secret(secret)


def test_evidence_files_are_owner_only_and_never_overwritten(tmp_path):
    path = tmp_path / "receipt.json"
    digest = live_provider._write_json(path, {"status": "synthetic"})
    assert len(digest) == 64
    assert stat.S_IMODE(path.stat().st_mode) == 0o600
    with pytest.raises(live_provider.LiveProviderError, match="evidence_write_failed"):
        live_provider._write_json(path, {"status": "replacement"})


def trace_run(run_id, *, tokens=15, cost="0.0012", inputs=None, error=None, name="ChatOpenAI"):
    return {
        "id": run_id,
        "name": name,
        "run_type": "llm",
        "end_time": "done",
        "inputs": {} if inputs is None else inputs,
        "outputs": {},
        "serialized": None,
        "attachments": None,
        "events": [],
        "error": error,
        "parent_run_id": RUNS[0],
        "total_tokens": tokens,
        "total_cost": cost,
        "extra": {"metadata": {
            "task_id": TASK, "ls_provider": "openai", "ls_model_name": "gpt-4.1-mini",
            "usage_metadata": {"total_tokens": tokens},
        }},
    }


def tool_run(run_id, name):
    return {
        "id": run_id, "name": name, "run_type": "tool", "end_time": "done",
        "inputs": {}, "outputs": {}, "serialized": None, "attachments": None,
        "events": [], "error": None, "parent_run_id": RUNS[0],
        "extra": {"metadata": {"task_id": TASK}},
    }


def test_trace_summary_counts_provider_usage_rounds_cost_up_and_detects_content():
    safe = trace_run(RUNS[1])
    safe["id"] = UUID(RUNS[1])  # LangSmith Run objects expose UUID instances.
    foreign = copy.deepcopy(safe)
    foreign["id"] = RUNS[2]
    foreign["extra"]["metadata"]["task_id"] = "task-" + "d" * 64
    tool = tool_run(RUNS[2], "write_file")
    value = live_provider.summarize_traces([safe, foreign, tool], TASK, "openai:gpt-4.1-mini")

    assert value["provider_call_count"] == 1
    assert value["total_tokens"] == 15
    assert value["total_cost_cents"] == 1
    assert value["unknown_provider_run_ids"] == []
    assert value["unsafe_run_ids"] == []
    assert value["completed_tool_runs"] == {"write_file": 1}
    assert value["unexpected_tool_run_count"] == 0

    unexpected = tool_run(RUNS[2], "shell")
    value = live_provider.summarize_traces([unexpected], TASK, "openai:gpt-4.1-mini")
    assert value["completed_tool_runs"] == {}
    assert value["unexpected_tool_run_count"] == 1

    unsafe = trace_run(RUNS[2], inputs={"prompt": "private"})
    value = live_provider.summarize_traces([unsafe], TASK, "openai:gpt-4.1-mini")
    assert value["unsafe_run_ids"] == [RUNS[2]]


def test_trace_unknown_usage_is_named_and_never_counted_as_zero():
    value = live_provider.summarize_traces(
        [trace_run(RUNS[1], tokens=None, cost=None)], TASK, "openai:gpt-4.1-mini",
    )
    assert value["provider_call_count"] == 1
    assert value["unknown_provider_run_ids"] == [RUNS[1]]
    assert value["total_tokens"] == 0 and value["total_cost_cents"] == 0


def valid_result():
    case = live_provider.load_cases(CASES)["planning-files-subagents-summary-can-v1"]
    files = {path: {"content": content, "created_at": "now", "modified_at": "now"}
             for path, content in case["expected"]["required_files"].items()}
    files["/conversation_history/summary.md"] = {
        "content": "synthetic summary", "created_at": "now", "modified_at": "now",
    }
    snapshot = {
        "schema_version": 2,
        "version": "task-v2",
        "status": "completed",
        "next": [],
        "identity": {"task_id": TASK, "workflow_id": TASK, "owner": "owner-a",
                     "first_execution_run_id": RUNS[0]},
        "can_count": 2,
        "total_model_boundaries": 20,
        "files": files,
        "todos": [{"content": item, "status": "completed"}
                  for item in case["expected"]["required_completed_todos"]],
        "messages": [{"type": "ai", "data": {"content": "completion"}}],
        "state": {"_summarization_session_id": "session-a", "_summarization_event": {
            "summary_message": {"content": "summary"},
            "file_path": "/conversation_history/summary.md",
        }},
    }
    return {"status": "succeeded", "ready": True, "run_id": RUNS[2], "result": snapshot}, case


def test_result_validation_proves_files_plan_subagents_summary_and_cross_segment_state():
    result, case = valid_result()
    value = live_provider.validate_result(result, case, TASK, RUNS[0])

    assert value["status"] == "passed"
    assert value["can_count"] == 2 and value["total_model_boundaries"] == 20
    assert value["required_file_count"] == 19


def test_result_validation_rejects_a_plausible_message_when_artifact_is_missing():
    result, case = valid_result()
    del result["result"]["files"]["/acceptance/child-a.txt"]
    with pytest.raises(live_provider.LiveProviderError, match="result_files"):
        live_provider.validate_result(result, case, TASK, RUNS[0])


def test_result_validation_rejects_unrequested_artifacts():
    result, case = valid_result()
    result["result"]["files"]["/acceptance/extra.txt"] = {
        "content": "extra", "created_at": "now", "modified_at": "now",
    }
    with pytest.raises(live_provider.LiveProviderError, match="result_files"):
        live_provider.validate_result(result, case, TASK, RUNS[0])


class HistoryAPI:
    def __init__(self):
        self.rows = {
            RUNS[0]: (None, RUNS[1]),
            RUNS[1]: (RUNS[0], RUNS[2]),
            RUNS[2]: (RUNS[1], None),
        }

    def request(self, method, path, **kwargs):
        run_id = next(run for run in RUNS if run in path)
        previous, following = self.rows[run_id]
        return 200, {
            "run_id": run_id,
            "previous_run_id": previous,
            "next_run_id": following,
            "history_navigation_incomplete": False,
            "events": [{"event_id": 2, "type": "workflow_execution_completed"},
                       {"event_id": 1, "type": "workflow_execution_started"}],
            "next_cursor": None,
        }


def test_history_validation_requires_the_complete_continue_as_new_chain():
    value = live_provider.read_history(HistoryAPI(), TASK, RUNS[2], RUNS[0], 3)
    assert value == {
        "run_count": 3,
        "event_count": 6,
        "run_ids": RUNS,
        "workflow_task_failed": 0,
    }


def test_live_endpoints_require_https_and_never_accept_embedded_credentials(monkeypatch):
    for value in ("http://task.invalid", "https://user:secret@task.invalid", "https://task.invalid/path"):
        with pytest.raises(live_provider.LiveProviderError, match="endpoint_invalid"):
            live_provider.TaskAPI(value, "synthetic")

    client = live_provider.TaskAPI("https://task.invalid", "synthetic")
    assert any(isinstance(handler, live_provider._NoRedirect) for handler in client._opener.handlers)
    assert all(handler.__class__.__name__ != "ProxyHandler" for handler in client._opener.handlers)

    session = live_provider._DirectSession()
    assert session.trust_env is False
    monkeypatch.setattr(
        live_provider.requests.Session, "request",
        lambda self, method, url, **kwargs: kwargs,
    )
    assert session.request("GET", "https://trace.invalid", allow_redirects=True)["allow_redirects"] is False
