"""Validate the fixed P0 production-acceptance profile without runtime authority."""

from __future__ import annotations

import argparse
import json
from datetime import date
from pathlib import Path
from typing import Any, Sequence

DEFAULT_PROFILE = Path(__file__).resolve().parents[2] / "docs" / "PRODUCTION_ACCEPTANCE_PROFILE.json"
KIND = "mini-loop-agent-server-production-acceptance"
STATUS = "fixed-p0-targets-not-yet-proven"


class ProfileError(ValueError):
    """A stable, content-free acceptance-profile validation failure."""


def _reject_constant(_value: str) -> None:
    raise ProfileError("profile contains a nonfinite number")


def _unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ProfileError("profile contains a duplicate object key")
        result[key] = value
    return result


def _object(value: Any, path: str, keys: set[str]) -> dict[str, Any]:
    if type(value) is not dict or set(value) != keys:
        raise ProfileError(f"{path} must contain exactly the documented fields")
    return value


def _string(value: Any, path: str) -> str:
    if type(value) is not str or not value or value != value.strip():
        raise ProfileError(f"{path} must be a nonempty trimmed string")
    return value


def _integer(value: Any, path: str, minimum: int = 0, maximum: int | None = None) -> int:
    if type(value) is not int or value < minimum or (maximum is not None and value > maximum):
        raise ProfileError(f"{path} must be an integer in the documented range")
    return value


def _boolean(value: Any, path: str) -> bool:
    if type(value) is not bool:
        raise ProfileError(f"{path} must be a boolean")
    return value


def _strings(value: Any, path: str, *, minimum: int = 1) -> list[str]:
    if type(value) is not list or len(value) < minimum:
        raise ProfileError(f"{path} must be a nonempty string list")
    result = [_string(item, f"{path}[]") for item in value]
    if len(set(result)) != len(result):
        raise ProfileError(f"{path} must not contain duplicates")
    return result


def _named_cases(value: Any, path: str, fields: set[str]) -> dict[str, dict[str, Any]]:
    if type(value) is not list or not value:
        raise ProfileError(f"{path} must be a nonempty case list")
    cases: dict[str, dict[str, Any]] = {}
    for index, item in enumerate(value):
        case = _object(item, f"{path}[{index}]", fields)
        name = _string(case["name"], f"{path}[{index}].name")
        if name in cases:
            raise ProfileError(f"{path} contains a duplicate case name")
        cases[name] = case
    return cases


def validate_profile(value: Any) -> dict[str, Any]:
    """Validate structure, fixed P0 targets and non-authorization boundaries."""
    profile = _object(value, "profile", {
        "schema_version", "kind", "status", "decision_date", "scope_ref", "environment",
        "topology", "load_cases", "duration_cases", "state_limits", "latency_targets_ms",
        "recovery_targets_seconds", "retention_contract", "provider_budget", "resource_budget",
        "evidence_requirements", "open_p0_conditions",
    })
    if _integer(profile["schema_version"], "schema_version", 1, 1) != 1 or profile["kind"] != KIND or profile["status"] != STATUS:
        raise ProfileError("profile identity is unsupported")
    if type(profile["decision_date"]) is not str:
        raise ProfileError("decision_date must be an ISO calendar date")
    try:
        decision_date = date.fromisoformat(profile["decision_date"])
    except ValueError:
        raise ProfileError("decision_date must be an ISO calendar date") from None
    if not 2000 <= decision_date.year <= 2099:
        raise ProfileError("decision_date must be an ISO calendar date")
    if profile["scope_ref"] != "docs/PRODUCTION_PILOT_SCOPE.md":
        raise ProfileError("scope_ref must bind the fixed P0.1 decision")

    environment = _object(profile["environment"], "environment", {
        "class", "provisioning_status", "data_classification", "data_residency", "temporal_storage",
        "transport", "authorization", "license_gate",
    })
    for key, item in environment.items():
        _string(item, f"environment.{key}")
    if environment["provisioning_status"] != "not-provisioned":
        raise ProfileError("P0 profile must not claim an unverified provisioned environment")

    topology = _object(profile["topology"], "topology", {
        "owners", "api_replicas", "worker_replicas", "activity_slots_per_worker",
        "workflow_slots_per_worker", "api_max_inflight_per_replica",
    })
    expected_topology = {
        "owners": 3, "api_replicas": 2, "worker_replicas": 2, "activity_slots_per_worker": 1,
        "workflow_slots_per_worker": 2, "api_max_inflight_per_replica": 32,
    }
    for key, item in topology.items():
        _integer(item, f"topology.{key}", 1)
    if topology != expected_topology:
        raise ProfileError("topology must retain the fixed P0.2 acceptance shape")

    load_cases = _named_cases(profile["load_cases"], "load_cases", {
        "name", "submission_count", "submission_window_seconds", "expected",
    })
    if set(load_cases) != {"idle", "full", "queued", "burst"}:
        raise ProfileError("load_cases must cover idle, full, queued and burst")
    for name, case in load_cases.items():
        _integer(case["submission_count"], f"load_cases.{name}.submission_count", 0, 10000)
        _integer(case["submission_window_seconds"], f"load_cases.{name}.submission_window_seconds", 1, 86400)
        _string(case["expected"], f"load_cases.{name}.expected")
    if [load_cases[name]["submission_count"] for name in ("idle", "full", "queued", "burst")] != [0, 2, 6, 12]:
        raise ProfileError("load case counts must retain the fixed P0.2 ladder")

    duration_cases = _named_cases(profile["duration_cases"], "duration_cases", {
        "name", "target_seconds", "task_count", "provider_mode",
    })
    expected_durations = {
        "five-minute-live": (300, 2, "live"),
        "thirty-minute-live": (1800, 1, "live"),
        "two-hour-live": (7200, 1, "live"),
        "twenty-four-hour-service-observation": (86400, 0, "deterministic-fixture"),
    }
    if set(duration_cases) != set(expected_durations):
        raise ProfileError("duration_cases must retain the fixed duration matrix")
    for name, case in duration_cases.items():
        _integer(case["target_seconds"], f"duration_cases.{name}.target_seconds", 1, 86400)
        _integer(case["task_count"], f"duration_cases.{name}.task_count", 0, 100)
        _string(case["provider_mode"], f"duration_cases.{name}.provider_mode")
        if (case["target_seconds"], case["task_count"], case["provider_mode"]) != expected_durations[name]:
            raise ProfileError("duration case targets must retain the fixed P0.2 matrix")

    state = _object(profile["state_limits"], "state_limits", {
        "request_body_bytes", "raw_checkpoint_bytes", "compressed_checkpoint_bytes",
        "continuation_envelope_bytes", "workflow_execution_seconds", "parent_model_boundaries",
        "segment_model_boundaries", "graph_recursion_limit", "dimensions", "target_ratios_milli",
        "over_limit_outcome",
    })
    for key in (
        "request_body_bytes", "raw_checkpoint_bytes", "compressed_checkpoint_bytes",
        "continuation_envelope_bytes", "workflow_execution_seconds", "parent_model_boundaries",
        "segment_model_boundaries", "graph_recursion_limit",
    ):
        _integer(state[key], f"state_limits.{key}", 1)
    if _strings(state["dimensions"], "state_limits.dimensions") != [
        "messages", "summaries", "general-purpose-subgraphs", "virtual-text-files", "todos",
    ]:
        raise ProfileError("state dimensions must retain all fixed P0.2 growth axes")
    if (type(state["target_ratios_milli"]) is not list
            or any(type(item) is not int for item in state["target_ratios_milli"])
            or state["target_ratios_milli"] != [500, 900, 1050]):
        raise ProfileError("state target ratios must cover baseline, near-limit and over-limit")
    _string(state["over_limit_outcome"], "state_limits.over_limit_outcome")

    latency = _object(profile["latency_targets_ms"], "latency_targets_ms", {
        "percentile", "submit_confirmation", "status_query", "cancel_confirmation",
        "queued_visibility", "must_report_separately",
    })
    if _integer(latency["percentile"], "latency_targets_ms.percentile", 1, 100) != 95:
        raise ProfileError("latency target must use p95")
    for key in ("submit_confirmation", "status_query", "cancel_confirmation", "queued_visibility"):
        _integer(latency[key], f"latency_targets_ms.{key}", 1, 60000)
    if set(_strings(latency["must_report_separately"], "latency_targets_ms.must_report_separately")) != {
        "queue-time", "execution-time", "completion-time",
    }:
        raise ProfileError("latency evidence must separate queue, execution and completion time")

    recovery = _object(profile["recovery_targets_seconds"], "recovery_targets_seconds", {
        "worker_loss_task_progress_rto", "backend_reconnect_service_rto",
        "backend_reconnect_task_progress_rto", "backup_restore_service_rto",
        "backup_restore_task_progress_rto", "acknowledged_task_rpo", "workflow_history_rpo",
        "terminal_result_rpo", "virtual_artifacts_at_safe_boundary_rpo", "backup_disaster_rpo",
        "inflight_activity_progress",
    })
    for key, item in recovery.items():
        if key == "inflight_activity_progress":
            _string(item, f"recovery_targets_seconds.{key}")
        else:
            _integer(item, f"recovery_targets_seconds.{key}", 0, 86400)
    for key in ("acknowledged_task_rpo", "workflow_history_rpo", "terminal_result_rpo", "virtual_artifacts_at_safe_boundary_rpo"):
        if recovery[key] != 0:
            raise ProfileError("durably acknowledged state targets must retain zero RPO")

    retention = _object(profile["retention_contract"], "retention_contract", {
        "workflow_history_days", "result_days", "event_days", "virtual_artifact_days", "trace_days",
        "idempotency_scope", "same_key_after_expiry", "unknown_start_result",
        "cancel_acknowledgement", "deletion_completion_hours",
    })
    for key in ("workflow_history_days", "result_days", "event_days", "virtual_artifact_days", "trace_days"):
        if _integer(retention[key], f"retention_contract.{key}", 1, 3650) != 30:
            raise ProfileError("history, result, event, artifact and trace retention must remain aligned at 30 days")
    _integer(retention["deletion_completion_hours"], "retention_contract.deletion_completion_hours", 1, 168)
    for key in ("idempotency_scope", "same_key_after_expiry", "unknown_start_result", "cancel_acknowledgement"):
        _string(retention[key], f"retention_contract.{key}")

    budget = _object(profile["provider_budget"], "provider_budget", {
        "execution_authorized", "operator_approval_required", "allowed_models",
        "max_output_tokens_per_call", "call_timeout_seconds", "max_provider_calls_per_live_run",
        "max_total_tokens_per_live_run", "max_cost_cents_per_live_run",
        "max_cost_cents_per_calendar_month", "consecutive_provider_failures_stop", "unknown_usage_action",
    })
    if _boolean(budget["execution_authorized"], "provider_budget.execution_authorized") is not False:
        raise ProfileError("the committed profile must not authorize provider spend")
    if _boolean(budget["operator_approval_required"], "provider_budget.operator_approval_required") is not True:
        raise ProfileError("live provider execution must require operator approval")
    if _strings(budget["allowed_models"], "provider_budget.allowed_models") != ["openai:gpt-4.1-mini"]:
        raise ProfileError("the P0.2 profile must retain one fixed allowed model")
    for key in (
        "max_output_tokens_per_call", "call_timeout_seconds", "max_provider_calls_per_live_run",
        "max_total_tokens_per_live_run", "max_cost_cents_per_live_run",
        "max_cost_cents_per_calendar_month", "consecutive_provider_failures_stop",
    ):
        _integer(budget[key], f"provider_budget.{key}", 1)
    _string(budget["unknown_usage_action"], "provider_budget.unknown_usage_action")

    resources = _object(profile["resource_budget"], "resource_budget", {
        "host_minimum_memory_mib", "owned_process_tree_rss_mib", "minimum_host_available_percent",
        "maximum_cpu_cores", "api_container_memory_mib", "worker_container_memory_mib",
        "maximum_evidence_bytes", "heavy_runs_serial", "stop_on_budget_or_pressure",
    })
    for key in (
        "host_minimum_memory_mib", "owned_process_tree_rss_mib", "minimum_host_available_percent",
        "maximum_cpu_cores", "api_container_memory_mib", "worker_container_memory_mib",
        "maximum_evidence_bytes",
    ):
        _integer(resources[key], f"resource_budget.{key}", 1)
    if resources["owned_process_tree_rss_mib"] >= resources["host_minimum_memory_mib"]:
        raise ProfileError("owned RSS budget must remain below minimum host memory")
    if not _boolean(resources["heavy_runs_serial"], "resource_budget.heavy_runs_serial"):
        raise ProfileError("heavy acceptance runs must remain serial")
    if not _boolean(resources["stop_on_budget_or_pressure"], "resource_budget.stop_on_budget_or_pressure"):
        raise ProfileError("resource pressure must remain a stop condition")

    _strings(profile["evidence_requirements"], "evidence_requirements", minimum=8)
    open_conditions = _strings(profile["open_p0_conditions"], "open_p0_conditions", minimum=4)
    if "approve-live-provider-spend-and-credential-use" not in open_conditions:
        raise ProfileError("live provider approval must remain an explicit open P0 condition")
    return profile


def load_profile(path: Path = DEFAULT_PROFILE) -> dict[str, Any]:
    """Load strict JSON and validate it; no profile value grants execution authority."""
    try:
        value = json.loads(
            path.read_text(encoding="utf-8"),
            object_pairs_hook=_unique_object,
            parse_constant=_reject_constant,
        )
    except OSError as exc:
        raise ProfileError("profile file is unavailable") from exc
    except json.JSONDecodeError as exc:
        raise ProfileError("profile is not valid JSON") from exc
    return validate_profile(value)


def summary(profile: dict[str, Any]) -> dict[str, Any]:
    """Return a small machine-readable projection without implying a passing run."""
    return {
        "status": "valid",
        "profile_status": profile["status"],
        "decision_date": profile["decision_date"],
        "load_cases": [case["name"] for case in profile["load_cases"]],
        "duration_seconds": [case["target_seconds"] for case in profile["duration_cases"]],
        "provider_execution_authorized": profile["provider_budget"]["execution_authorized"],
        "open_p0_conditions": len(profile["open_p0_conditions"]),
    }


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("profile", nargs="?", type=Path, default=DEFAULT_PROFILE)
    args = parser.parse_args(argv)
    try:
        result = summary(load_profile(args.profile))
    except ProfileError as exc:
        print(json.dumps({"status": "invalid", "error": str(exc)}, sort_keys=True))
        return 2
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
