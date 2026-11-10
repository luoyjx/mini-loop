import copy
import json
from pathlib import Path

import pytest

from mini_loop_deepagents.task_server import checkpoint, http, runtime
from tooling.acceptance import production_profile


PROFILE = Path(__file__).parents[1] / "docs" / "PRODUCTION_ACCEPTANCE_PROFILE.json"


def test_committed_profile_is_strict_valid_and_not_execution_authority():
    profile = production_profile.load_profile(PROFILE)

    assert profile["status"] == "fixed-p0-targets-not-yet-proven"
    assert profile["provider_budget"]["execution_authorized"] is False
    assert profile["environment"]["provisioning_status"] == "not-provisioned"
    assert profile["open_p0_conditions"]


def test_profile_matches_current_runtime_and_deployment_limits():
    profile = production_profile.load_profile(PROFILE)
    limits = profile["state_limits"]

    assert limits == {
        "request_body_bytes": http.MAX_BODY_BYTES,
        "raw_checkpoint_bytes": checkpoint.MAX_RAW_BYTES,
        "compressed_checkpoint_bytes": checkpoint.MAX_COMPRESSED_BYTES,
        "continuation_envelope_bytes": checkpoint.MAX_ENVELOPE_BYTES,
        "workflow_execution_seconds": 24 * 60 * 60,
        "parent_model_boundaries": runtime.MAX_MODEL_BOUNDARIES,
        "segment_model_boundaries": runtime.SEGMENT_MODEL_BOUNDARIES,
        "graph_recursion_limit": runtime.RECURSION_LIMIT,
        "dimensions": ["messages", "summaries", "general-purpose-subgraphs", "virtual-text-files", "todos"],
        "target_ratios_milli": [500, 900, 1050],
        "over_limit_outcome": "explicit-terminal-failure-without-unbounded-replay",
    }


def test_profile_fixes_the_load_duration_retention_and_resource_contracts():
    profile = production_profile.load_profile(PROFILE)

    assert [(row["name"], row["submission_count"]) for row in profile["load_cases"]] == [
        ("idle", 0), ("full", 2), ("queued", 6), ("burst", 12),
    ]
    assert [row["target_seconds"] for row in profile["duration_cases"]] == [300, 1800, 7200, 86400]
    assert {profile["retention_contract"][key] for key in (
        "workflow_history_days", "result_days", "event_days", "virtual_artifact_days",
    )} == {30}
    assert profile["resource_budget"]["owned_process_tree_rss_mib"] == 6144
    assert profile["resource_budget"]["minimum_host_available_percent"] == 25


@pytest.mark.parametrize("mutation", [
    lambda value: value.update(extra="field"),
    lambda value: value["topology"].update(worker_replicas=3),
    lambda value: value["provider_budget"].update(execution_authorized=True),
    lambda value: value["duration_cases"].pop(),
    lambda value: value["state_limits"].update(target_ratios_milli=[500, 900]),
    lambda value: value["resource_budget"].update(heavy_runs_serial=False),
    lambda value: value["retention_contract"].update(workflow_history_days=True),
    lambda value: value.update(decision_date="2026-99-99"),
])
def test_profile_rejects_drift_that_would_weaken_or_misstate_p0(mutation):
    value = copy.deepcopy(production_profile.load_profile(PROFILE))
    mutation(value)

    with pytest.raises(production_profile.ProfileError):
        production_profile.validate_profile(value)


@pytest.mark.parametrize("content", [
    '{"schema_version": 1, "schema_version": 1}',
    '{"schema_version": NaN}',
    '[]',
])
def test_profile_loader_rejects_ambiguous_or_non_object_json(tmp_path, content):
    path = tmp_path / "profile.json"
    path.write_text(content, encoding="utf-8")

    with pytest.raises(production_profile.ProfileError):
        production_profile.load_profile(path)


def test_profile_cli_emits_bounded_machine_readable_summary(capsys):
    assert production_profile.main([str(PROFILE)]) == 0

    output = json.loads(capsys.readouterr().out)
    assert output == {
        "decision_date": "2026-11-10",
        "duration_seconds": [300, 1800, 7200, 86400],
        "load_cases": ["idle", "full", "queued", "burst"],
        "open_p0_conditions": 4,
        "profile_status": "fixed-p0-targets-not-yet-proven",
        "provider_execution_authorized": False,
        "status": "valid",
    }
