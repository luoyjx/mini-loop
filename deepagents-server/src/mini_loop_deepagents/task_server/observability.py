"""Bounded, content-free correlation records for Task API and model Activities.

Observations contain deployment and execution identifiers only. Task messages,
idempotency keys, model inputs/outputs, credentials, exception text and Temporal
task tokens never enter this module's records.
"""

from __future__ import annotations

from collections.abc import Callable, Mapping
from dataclasses import dataclass
import hashlib
import json
import os
import re
from typing import Any

from .protocol import SNAPSHOT_SCHEMA, TASK_ID_PATTERN, TASK_VERSION, valid_run_id

OBSERVATION_SCHEMA = 1
IMAGE_REF_ENV = "MINILOOP_TASK_IMAGE_REF"

Observation = Mapping[str, str | int | bool]
ObservationSink = Callable[[Observation], None]

_IMAGE_REF = re.compile(r"[A-Za-z0-9][A-Za-z0-9._/:@+-]{0,439}@sha256:[0-9a-f]{64}\Z")
_IMAGE_DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")
_OWNER = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.@+~:-]{0,127}\Z")
_SAFE_LABEL = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.:@+/-]{0,255}\Z")
_CORRELATION_FIELDS = frozenset({
    "task_owner", "task_id", "first_execution_run_id", "current_run_id",
    "protocol_version", "snapshot_schema",
})


@dataclass(frozen=True)
class DeploymentIdentity:
    """Deployment-owned image and wire identity attached to every record."""

    image_digest: str
    protocol_version: str = TASK_VERSION
    snapshot_schema: int = SNAPSHOT_SCHEMA

    def __post_init__(self) -> None:
        if (self.image_digest != "unavailable" and not _IMAGE_DIGEST.fullmatch(self.image_digest)):
            raise ValueError("image_digest must be unavailable or sha256:<64 lowercase hex>")
        if self.protocol_version != TASK_VERSION or self.snapshot_schema != SNAPSHOT_SCHEMA:
            raise ValueError("deployment wire identity must match the current Task protocol")

    @classmethod
    def unavailable(cls) -> DeploymentIdentity:
        return cls(image_digest="unavailable")

    @classmethod
    def from_env(cls, environ: Mapping[str, str] | None = None) -> DeploymentIdentity:
        env = os.environ if environ is None else environ
        reference = env.get(IMAGE_REF_ENV)
        if reference is None:
            return cls.unavailable()
        if not _IMAGE_REF.fullmatch(reference):
            raise ValueError(f"{IMAGE_REF_ENV} must be an immutable repository@sha256 image reference")
        return cls(image_digest=reference.rsplit("@", 1)[1])


def default_observation_sink(observation: Observation) -> None:
    """Write one bounded JSON line to stderr without depending on log levels."""
    encoded = (json.dumps(dict(observation), ensure_ascii=True, sort_keys=True,
                          separators=(",", ":"), allow_nan=False) + "\n").encode("ascii")
    if len(encoded) > 4096:
        raise ValueError("Task observation exceeds its fixed line budget")
    os.write(2, encoded)


def emit_observation(sink: ObservationSink, observation: Observation) -> None:
    """Observability is best-effort and cannot rewrite API or provider outcomes."""
    try:
        sink(dict(observation))
    except Exception:
        return


def base_observation(event: str, deployment: DeploymentIdentity) -> dict[str, str | int | bool]:
    return {
        "schema_version": OBSERVATION_SCHEMA,
        "event": event,
        "image_digest": deployment.image_digest,
        "protocol_version": deployment.protocol_version,
        "snapshot_schema": deployment.snapshot_schema,
    }


def _activity_correlation(config: Any, info: Any) -> dict[str, str | int] | None:
    metadata = config.get("metadata") if isinstance(config, Mapping) else None
    if not isinstance(metadata, Mapping) or not _CORRELATION_FIELDS.issubset(metadata):
        return None
    owner = metadata.get("task_owner")
    task_id = metadata.get("task_id")
    first_run_id = metadata.get("first_execution_run_id")
    current_run_id = metadata.get("current_run_id")
    if (not isinstance(owner, str) or not _OWNER.fullmatch(owner) or owner in {".", ".."}
            or not isinstance(task_id, str) or not TASK_ID_PATTERN.fullmatch(task_id)
            or not isinstance(first_run_id, str) or not valid_run_id(first_run_id)
            or not isinstance(current_run_id, str) or not valid_run_id(current_run_id)
            or metadata.get("protocol_version") != TASK_VERSION
            or metadata.get("snapshot_schema") != SNAPSHOT_SCHEMA
            or task_id != info.workflow_id or current_run_id != info.workflow_run_id):
        return None
    return {
        "owner": owner,
        "task_id": task_id,
        "first_execution_run_id": first_run_id,
        "current_run_id": current_run_id,
    }


def _safe_label(value: Any) -> str:
    return value if isinstance(value, str) and _SAFE_LABEL.fullmatch(value) else "unavailable"


def _current_activity_info() -> Any:
    # Keep the HTTP adapter importable without the optional Temporal dependency.
    from temporalio import activity
    return activity.info()


class ObservedModel:
    """Delegate a provider model while recording content-free Activity attempts."""

    def __init__(
        self, delegate: Any, *, deployment: DeploymentIdentity, worker_identity: str,
        sink: ObservationSink,
    ) -> None:
        self._delegate = delegate
        self._deployment = deployment
        self._worker_identity = worker_identity
        self._worker_identity_sha256 = hashlib.sha256(worker_identity.encode("utf-8", errors="replace")).hexdigest()
        self._sink = sink

    def _wrap(self, delegate: Any) -> ObservedModel:
        return ObservedModel(delegate, deployment=self._deployment,
                             worker_identity=self._worker_identity, sink=self._sink)

    def bind_tools(self, *args: Any, **kwargs: Any) -> ObservedModel:
        return self._wrap(self._delegate.bind_tools(*args, **kwargs))

    def bind(self, *args: Any, **kwargs: Any) -> ObservedModel:
        return self._wrap(self._delegate.bind(*args, **kwargs))

    async def ainvoke(self, *args: Any, config: Any = None, **kwargs: Any) -> Any:
        info = _current_activity_info()
        correlation = _activity_correlation(config, info)
        record = base_observation("task.model_activity", self._deployment)
        record.update({
            "worker_identity_sha256": self._worker_identity_sha256,
            "task_id": info.workflow_id if isinstance(info.workflow_id, str) and TASK_ID_PATTERN.fullmatch(info.workflow_id) else "unavailable",
            "current_run_id": info.workflow_run_id if isinstance(info.workflow_run_id, str) and valid_run_id(info.workflow_run_id) else "unavailable",
            "activity_id": _safe_label(info.activity_id),
            "activity_type": _safe_label(info.activity_type),
            "attempt": info.attempt,
            "correlation_status": "complete" if correlation is not None else "unavailable",
        })
        if correlation is not None:
            record.update(correlation)
        emit_observation(self._sink, {**record, "outcome": "started"})
        try:
            result = await self._delegate.ainvoke(*args, config=config, **kwargs)
        except BaseException:
            emit_observation(self._sink, {**record, "outcome": "failed"})
            raise
        emit_observation(self._sink, {**record, "outcome": "completed"})
        return result


def observe_model_provider(
    provider: Callable[[str], Any], *, deployment: DeploymentIdentity,
    worker_identity: str, sink: ObservationSink = default_observation_sink,
) -> Callable[[str], Any]:
    """Wrap the trusted worker-side provider selected by DeepAgentsPlugin."""
    def observed(name: str) -> Any:
        return ObservedModel(provider(name), deployment=deployment,
                             worker_identity=worker_identity, sink=sink)
    return observed
