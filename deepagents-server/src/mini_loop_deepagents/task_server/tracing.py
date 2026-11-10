"""CLI-owned, content-free LangSmith tracing for Temporal Task execution."""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass, field
import math
import os
import re
from typing import Any
from urllib.parse import urlsplit

import langsmith
from temporalio.contrib.langsmith import LangSmithPlugin

from .observability import DeploymentIdentity

TRACING_ENV = "LANGSMITH_TRACING"
PROJECT_ENV = "LANGSMITH_PROJECT"
API_KEY_ENV = "LANGSMITH_API_KEY"
ENDPOINT_ENV = "LANGSMITH_ENDPOINT"
RETENTION_ENV = "MINILOOP_TASK_TRACE_RETENTION_DAYS"
DEFAULT_ENDPOINT = "https://api.smith.langchain.com"
TRACE_RETENTION_DAYS = 30

_PROJECT = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.:/+-]{0,127}\Z")
_SAFE_TEXT = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.:@+/-]{0,255}\Z")
_RUN_NAME = re.compile(r"(?:[a-z][a-z0-9_-]{0,63}|[A-Z][A-Za-z0-9]{0,63})\Z")
_TEMPORAL_RUN_NAME = re.compile(
    r"(?:StartWorkflow|RunWorkflow|QueryWorkflow|StartActivity|RunActivity):"
    r"(?:TaskWorkflowV2|snapshot|deepagents\.invoke_model(?:_streaming)?)\Z"
)
_UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\Z")
_TASK_ID = re.compile(r"task-[0-9a-f]{64}\Z")
_DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")

_TEXT_METADATA = frozenset({
    "mini_loop_service_role", "mini_loop_protocol_version", "mini_loop_image_digest",
    "task_id", "first_execution_run_id", "current_run_id", "thread_id",
    "temporalWorkflowID", "temporalRunID", "temporalActivityID",
    "langgraph_node", "langgraph_checkpoint_ns", "ls_provider", "ls_model_name", "ls_model_type",
})
_INTEGER_METADATA = frozenset({
    "mini_loop_snapshot_schema", "mini_loop_trace_retention_days", "langgraph_step",
})
_USAGE_SCALARS = frozenset({
    "input_tokens", "output_tokens", "total_tokens", "input_cost", "output_cost", "total_cost",
})
_USAGE_MAPS = frozenset({"input_token_details", "output_token_details"})
_RUN_FIELDS = frozenset({
    "id", "name", "run_type", "start_time", "end_time", "parent_run_id", "trace_id",
    "dotted_order", "project_name", "session_id", "session_name", "reference_example_id",
})
_RUN_TAGS = frozenset({"mini-loop-task", "role:api", "role:worker"})


class TaskTracingConfigurationError(ValueError):
    """Fixed startup code that never contains credentials or raw SDK errors."""


def _valid_endpoint(value: Any) -> bool:
    try:
        if not isinstance(value, str) or len(value) > 512 or not value.isascii():
            return False
        parsed = urlsplit(value)
        return (
            parsed.scheme == "https" and parsed.hostname is not None
            and parsed.username is None and parsed.password is None
            and parsed.path in {"", "/"} and not parsed.query and not parsed.fragment
            and parsed.port in {None, 443}
        )
    except (TypeError, ValueError):
        return False


@dataclass(frozen=True)
class TaskTracingSettings:
    """Explicit trace settings; disabled library defaults never inspect ambient config."""

    enabled: bool = False
    project_name: str | None = None
    api_key: str | None = field(default=None, repr=False)
    endpoint: str = DEFAULT_ENDPOINT
    retention_days: int | None = None

    def __post_init__(self) -> None:
        if type(self.enabled) is not bool:
            raise TaskTracingConfigurationError("task_trace_enabled_invalid") from None
        if not self.enabled:
            if self.project_name is not None or self.api_key is not None or self.retention_days is not None:
                raise TaskTracingConfigurationError("task_trace_disabled_settings") from None
            if self.endpoint != DEFAULT_ENDPOINT:
                raise TaskTracingConfigurationError("task_trace_disabled_settings") from None
            return
        if not isinstance(self.project_name, str) or not _PROJECT.fullmatch(self.project_name):
            raise TaskTracingConfigurationError("task_trace_project_invalid") from None
        if (not isinstance(self.api_key, str) or not 1 <= len(self.api_key) <= 16 * 1024
                or any(not 33 <= ord(char) <= 126 for char in self.api_key)):
            raise TaskTracingConfigurationError("task_trace_api_key_invalid") from None
        if not _valid_endpoint(self.endpoint):
            raise TaskTracingConfigurationError("task_trace_endpoint_invalid") from None
        if self.retention_days != TRACE_RETENTION_DAYS:
            raise TaskTracingConfigurationError("task_trace_retention_invalid") from None

    @classmethod
    def from_env(cls, environ: Mapping[str, str] | None = None) -> TaskTracingSettings:
        env = os.environ if environ is None else environ
        enabled = env.get(TRACING_ENV, "false")
        if enabled not in {"true", "false"}:
            raise TaskTracingConfigurationError("task_trace_enabled_invalid") from None
        if enabled == "false":
            return cls()
        try:
            retention = int(env.get(RETENTION_ENV, ""))
        except (TypeError, ValueError):
            raise TaskTracingConfigurationError("task_trace_retention_invalid") from None
        return cls(
            enabled=True,
            project_name=env.get(PROJECT_ENV),
            api_key=env.get(API_KEY_ENV),
            endpoint=env.get(ENDPOINT_ENV, DEFAULT_ENDPOINT),
            retention_days=retention,
        )


def _safe_trace_text(key: str, value: Any) -> str | None:
    if not isinstance(value, str):
        return None
    if key in {"task_id", "thread_id", "temporalWorkflowID"}:
        return value if _TASK_ID.fullmatch(value) else None
    if key in {"first_execution_run_id", "current_run_id", "temporalRunID"}:
        return value if _UUID.fullmatch(value) else None
    if key == "mini_loop_image_digest":
        return value if value == "unavailable" or _DIGEST.fullmatch(value) else None
    return value if _SAFE_TEXT.fullmatch(value) else None


def _safe_number(value: Any, *, integer: bool = False) -> int | float | None:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    if value < 0 or not math.isfinite(value):
        return None
    if integer and not isinstance(value, int):
        return None
    return value


def _sanitize_usage(value: Any) -> dict[str, Any] | None:
    if not isinstance(value, Mapping):
        return None
    safe: dict[str, Any] = {}
    for key in _USAGE_SCALARS:
        number = _safe_number(value.get(key), integer=key.endswith("_tokens"))
        if number is not None:
            safe[key] = number
    for key in _USAGE_MAPS:
        details = value.get(key)
        if not isinstance(details, Mapping):
            continue
        safe_details = {
            detail: number
            for detail, raw in details.items()
            if isinstance(detail, str) and _SAFE_TEXT.fullmatch(detail)
            and (number := _safe_number(raw, integer=True)) is not None
        }
        if safe_details:
            safe[key] = safe_details
    return safe or None


def sanitize_trace_metadata(metadata: Any) -> dict[str, Any]:
    """Allow only bounded correlation, topology and provider usage metadata."""
    if not isinstance(metadata, Mapping):
        return {}
    safe: dict[str, Any] = {}
    for key in _TEXT_METADATA:
        value = _safe_trace_text(key, metadata.get(key))
        if value is not None:
            safe[key] = value
    for key in _INTEGER_METADATA:
        value = _safe_number(metadata.get(key), integer=True)
        if value is not None:
            safe[key] = value
    usage = _sanitize_usage(metadata.get("usage_metadata"))
    if usage is not None:
        safe["usage_metadata"] = usage
    return safe


def trace_anonymizer(payload: dict[str, Any]) -> dict[str, Any]:
    """Redact errors and treat every other anonymizer call as run metadata."""
    if set(payload) == {"error"}:
        return {"error": "redacted"}
    return sanitize_trace_metadata(payload)


def _safe_run_name(value: Any) -> str:
    if isinstance(value, str) and (_RUN_NAME.fullmatch(value) or _TEMPORAL_RUN_NAME.fullmatch(value)):
        return value
    return "redacted-run"


def _sanitize_run_payload(payload: Mapping[str, Any]) -> dict[str, Any]:
    """Fail closed over whole SDK run operations, not only input/output fields."""
    safe = {key: payload[key] for key in _RUN_FIELDS if key in payload}
    if "name" in safe:
        safe["name"] = _safe_run_name(safe["name"])
    safe["inputs"] = {}
    if "outputs" in payload:
        safe["outputs"] = {}
    if payload.get("error") is not None:
        safe["error"] = "redacted"
    metadata = sanitize_trace_metadata(
        payload.get("extra", {}).get("metadata")
        if isinstance(payload.get("extra"), Mapping) else None
    )
    safe["extra"] = {"metadata": metadata} if metadata else {}
    tags = payload.get("tags")
    safe["tags"] = [tag for tag in tags if tag in _RUN_TAGS] if isinstance(tags, list) else []
    return safe


class _ContentFreeLangSmithClient(langsmith.Client):
    """Public SDK calls with a fail-closed whole-payload scrub before enqueueing."""

    def create_run(self, name: str, inputs: dict[str, Any], run_type: Any, **kwargs: Any) -> None:
        safe = _sanitize_run_payload({"name": name, "inputs": inputs, "run_type": run_type, **kwargs})
        super().create_run(
            name=safe.pop("name"), inputs=safe.pop("inputs"), run_type=safe.pop("run_type"),
            dangerously_allow_filesystem=False, **safe,
        )

    def update_run(self, run_id: Any, **kwargs: Any) -> None:
        safe = _sanitize_run_payload(kwargs)
        super().update_run(run_id, dangerously_allow_filesystem=False, **safe)


def create_trace_plugins(
    role: str, deployment: DeploymentIdentity, settings: TaskTracingSettings,
) -> tuple[LangSmithPlugin, ...]:
    """Create the explicit Client plugin; the Client carries it into its Worker."""
    if role not in {"api", "worker"}:
        raise TaskTracingConfigurationError("task_trace_role_invalid") from None
    if not isinstance(settings, TaskTracingSettings):
        raise TaskTracingConfigurationError("task_trace_settings_invalid") from None
    if not settings.enabled:
        return ()
    try:
        client = _ContentFreeLangSmithClient(
            api_url=settings.endpoint,
            api_key=settings.api_key,
            hide_inputs=True,
            hide_outputs=True,
            hide_metadata=sanitize_trace_metadata,
            anonymizer=trace_anonymizer,
            omit_traced_runtime_info=True,
        )
        plugin = LangSmithPlugin(
            client=client,
            project_name=settings.project_name,
            add_temporal_runs=True,
            default_metadata={
                "mini_loop_service_role": role,
                "mini_loop_protocol_version": deployment.protocol_version,
                "mini_loop_snapshot_schema": deployment.snapshot_schema,
                "mini_loop_image_digest": deployment.image_digest,
                "mini_loop_trace_retention_days": settings.retention_days,
            },
            default_tags=["mini-loop-task", f"role:{role}"],
        )
    except Exception:
        raise TaskTracingConfigurationError("task_trace_start_failed") from None
    return (plugin,)
