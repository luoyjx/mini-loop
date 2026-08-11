"""Composable, fail-open token-efficiency service-provider interfaces.

This module deliberately does not wire itself into :mod:`mini_loop.agent`.
It defines the contracts and a small runtime that a harness can compose at the
three places where token optimization has materially different semantics:

* observations returned by tools;
* a copy of the provider request; and
* stable response-policy settings.

The raw artifact store accepts *masked* content only by contract.  It neither
knows the application's secrets nor attempts to redact them.  Callers must run
their secret masker before calling :meth:`MaskedRawArtifactStore.put_masked`.
"""

from __future__ import annotations

import asyncio
import copy
import hashlib
import inspect
import json
import math
import re
import secrets
import threading
import time
from dataclasses import dataclass, field, replace
from enum import Enum
from pathlib import Path
from typing import Any, Callable, Mapping, Protocol, runtime_checkable


class ComponentStage(str, Enum):
    OBSERVATION = "observation"
    REQUEST_CONTEXT = "request_context"
    RESPONSE_POLICY = "response_policy"


class OptimizationMode(str, Enum):
    OFF = "off"
    SHADOW = "shadow"
    ENFORCE = "enforce"


class OptimizationStatus(str, Enum):
    APPLIED = "applied"
    PASSTHROUGH = "passthrough"
    SHADOWED = "shadowed"
    DEGRADED = "degraded"
    ERROR = "error"


class Lossiness(str, Enum):
    """The semantic fidelity declared by a component.

    ``LOSSLESS`` means the component preserves meaningful source content; it
    need not preserve presentation bytes such as ANSI colour escapes.
    ``RECOVERABLE`` means omitted content can be fetched through a scoped raw
    reference.  ``LOSSY`` has no such completeness guarantee.
    """

    LOSSLESS = "lossless"
    RECOVERABLE = "recoverable"
    LOSSY = "lossy"


class LifecyclePhase(str, Enum):
    INITIALIZE = "initialize"
    HEALTH = "health"
    CLOSE = "close"


class LifecycleStatus(str, Enum):
    OK = "ok"
    SKIPPED = "skipped"
    DEGRADED = "degraded"
    ERROR = "error"


_COMPONENT_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}\Z")
_COMPONENT_VERSION = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.+-]{0,63}\Z")
_WARNING_CODE = re.compile(r"[a-z][a-z0-9_.-]{0,63}(?::[0-9]{1,12})?\Z")
_RECEIPT_REASON = re.compile(
    r"[A-Za-z][A-Za-z0-9_.-]{0,127}(?::[A-Za-z0-9_.-]{1,128})?\Z"
)
_SHA256_DIGEST = re.compile(r"sha256:[0-9a-f]{64}\Z")
_MAX_WARNINGS = 32
_MAX_WARNING_CHARS = 256
_MAX_INSTRUCTIONS = 16
_MAX_INSTRUCTION_CHARS = 2_048
_MAX_INSTRUCTIONS_CHARS = 8_192
_MAX_METADATA_ITEMS = 32
_MAX_METADATA_KEY_CHARS = 128
_MAX_METADATA_VALUE_CHARS = 512


def _validated_warnings(value: object) -> tuple[str, ...]:
    """Validate bounded, inert warning codes returned across the plugin SPI."""

    if not isinstance(value, tuple):
        raise TypeError("warnings must be a tuple of strings")
    if len(value) > _MAX_WARNINGS:
        raise ValueError(f"warnings must contain at most {_MAX_WARNINGS} entries")
    if any(not isinstance(item, str) for item in value):
        raise TypeError("warnings must contain only strings")
    if any(len(item) > _MAX_WARNING_CHARS for item in value):
        raise ValueError(
            f"warning entries must be at most {_MAX_WARNING_CHARS} characters"
        )
    if any(not _WARNING_CODE.fullmatch(item) for item in value):
        raise ValueError("warnings must contain bounded machine-readable codes")
    return value


def _merged_warnings(*groups: tuple[str, ...]) -> tuple[str, ...]:
    """Combine already-validated warning codes without expanding receipt bounds."""

    merged: list[str] = []
    for group in groups:
        for warning in group:
            if warning not in merged:
                merged.append(warning)
            if len(merged) == _MAX_WARNINGS:
                return tuple(merged)
    return tuple(merged)


def _component_error_reason(error: BaseException) -> str:
    name = type(error).__name__
    if re.fullmatch(r"[A-Za-z][A-Za-z0-9_]{0,63}", name):
        return f"component_error:{name}"
    return "component_error"


@dataclass(frozen=True, slots=True)
class ComponentDescriptor:
    id: str
    version: str
    stage: ComponentStage
    content_types: tuple[str, ...] = ("*/*",)
    deterministic: bool = True
    lossiness: Lossiness = Lossiness.LOSSLESS
    recoverable: bool = False
    cost_tier: str = "fast"
    network_access: bool = False
    timeout_ms: int = 1_000
    max_input_bytes: int | None = None
    capabilities: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        if not _COMPONENT_ID.fullmatch(self.id):
            raise ValueError("component id must be 1-128 safe identifier characters")
        if not isinstance(self.version, str) or not _COMPONENT_VERSION.fullmatch(
            self.version
        ):
            raise ValueError("component version must be a 1-64 character safe identifier")
        object.__setattr__(self, "stage", ComponentStage(self.stage))
        object.__setattr__(self, "lossiness", Lossiness(self.lossiness))
        content_types = tuple(self.content_types)
        if not content_types or any(not value or "/" not in value for value in content_types):
            raise ValueError("content_types must contain MIME-like values")
        object.__setattr__(self, "content_types", content_types)
        capabilities = tuple(dict.fromkeys(self.capabilities))
        if any(not value for value in capabilities):
            raise ValueError("capabilities must be non-empty strings")
        object.__setattr__(self, "capabilities", capabilities)
        if self.timeout_ms <= 0:
            raise ValueError("timeout_ms must be positive")
        if self.max_input_bytes is not None and self.max_input_bytes <= 0:
            raise ValueError("max_input_bytes must be positive when set")
        if self.cost_tier not in {"fast", "ml", "remote"}:
            raise ValueError("cost_tier must be fast, ml, or remote")
        if self.lossiness is Lossiness.RECOVERABLE and not self.recoverable:
            raise ValueError("recoverable lossiness requires recoverable=True")


def _stable_bytes(value: Any) -> bytes:
    try:
        rendered = json.dumps(
            value,
            ensure_ascii=False,
            sort_keys=True,
            separators=(",", ":"),
            allow_nan=False,
        )
    except (TypeError, ValueError):
        rendered = repr(value)
    return rendered.encode("utf-8", errors="replace")


def _digest_bytes(value: bytes) -> str:
    return f"sha256:{hashlib.sha256(value).hexdigest()}"


def _default_token_counter(value: str) -> int:
    """Cheap routing estimate; provider usage remains the reporting authority."""

    if not value:
        return 0
    return max(1, (len(value.encode("utf-8")) + 3) // 4)


@dataclass(frozen=True, slots=True)
class OptimizationReceipt:
    component_id: str
    component_version: str
    stage: ComponentStage
    mode: OptimizationMode
    status: OptimizationStatus
    reason: str
    raw_bytes: int
    projected_bytes: int
    tokens_before_estimate: int
    tokens_after_estimate: int
    input_digest: str
    output_digest: str
    lossiness: Lossiness
    deterministic: bool
    raw_ref: str | None = None
    raw_digest: str | None = None
    warnings: tuple[str, ...] = ()
    elapsed_ms: float = 0.0

    def __post_init__(self) -> None:
        if not _COMPONENT_ID.fullmatch(self.component_id):
            raise ValueError("receipt component id is invalid")
        if not _COMPONENT_VERSION.fullmatch(self.component_version):
            raise ValueError("receipt component version is invalid")
        if not isinstance(self.reason, str) or not _RECEIPT_REASON.fullmatch(
            self.reason
        ):
            raise ValueError("receipt reason must be a bounded machine-readable code")
        object.__setattr__(self, "stage", ComponentStage(self.stage))
        object.__setattr__(self, "mode", OptimizationMode(self.mode))
        object.__setattr__(self, "status", OptimizationStatus(self.status))
        object.__setattr__(self, "lossiness", Lossiness(self.lossiness))
        object.__setattr__(self, "warnings", _validated_warnings(self.warnings))
        for field_name in (
            "raw_bytes",
            "projected_bytes",
            "tokens_before_estimate",
            "tokens_after_estimate",
        ):
            value = getattr(self, field_name)
            if not isinstance(value, int) or isinstance(value, bool):
                raise TypeError(f"{field_name} must be an integer")
            if value < 0:
                raise ValueError(f"{field_name} cannot be negative")
        if not isinstance(self.deterministic, bool):
            raise TypeError("deterministic must be boolean")
        if not isinstance(self.elapsed_ms, (int, float)) or isinstance(
            self.elapsed_ms, bool
        ):
            raise TypeError("elapsed_ms must be numeric")
        if self.elapsed_ms < 0 or not math.isfinite(self.elapsed_ms):
            raise ValueError("elapsed_ms must be finite and non-negative")
        for field_name in ("input_digest", "output_digest"):
            if not _SHA256_DIGEST.fullmatch(getattr(self, field_name)):
                raise ValueError(f"{field_name} must be a sha256 digest")
        if (self.raw_ref is None) != (self.raw_digest is None):
            raise ValueError("raw_ref and raw_digest must be set together")
        if self.raw_ref is not None and not _RAW_REF.fullmatch(self.raw_ref):
            raise ValueError("raw_ref is invalid")
        if self.raw_digest is not None and not _SHA256_DIGEST.fullmatch(
            self.raw_digest
        ):
            raise ValueError("raw_digest must be a sha256 digest")

    @property
    def changed(self) -> bool:
        return self.input_digest != self.output_digest

    def as_dict(self) -> dict[str, object]:
        """Return event-safe metrics and references, never observation content."""

        return {
            "component_id": self.component_id,
            "component_version": self.component_version,
            "stage": self.stage.value,
            "mode": self.mode.value,
            "status": self.status.value,
            "reason": self.reason,
            "raw_bytes": self.raw_bytes,
            "projected_bytes": self.projected_bytes,
            "tokens_before_estimate": self.tokens_before_estimate,
            "tokens_after_estimate": self.tokens_after_estimate,
            "input_digest": self.input_digest,
            "output_digest": self.output_digest,
            "lossiness": self.lossiness.value,
            "deterministic": self.deterministic,
            "raw_ref": self.raw_ref,
            "raw_digest": self.raw_digest,
            "warnings": list(self.warnings),
            "elapsed_ms": self.elapsed_ms,
            "changed": self.changed,
        }


@dataclass(frozen=True, slots=True)
class MaskedArtifactPointer:
    ref: str
    digest: str
    size_bytes: int
    expires_at: float

    def __post_init__(self) -> None:
        if not _RAW_REF.fullmatch(self.ref):
            raise ValueError("artifact pointer ref is invalid")
        if not _SHA256_DIGEST.fullmatch(self.digest):
            raise ValueError("artifact pointer digest must be sha256")
        if not isinstance(self.size_bytes, int) or isinstance(self.size_bytes, bool):
            raise TypeError("artifact pointer size must be an integer")
        if self.size_bytes < 0:
            raise ValueError("artifact pointer size cannot be negative")
        if not math.isfinite(self.expires_at):
            raise ValueError("artifact pointer expiry must be finite")


class RawArtifactStoreError(RuntimeError):
    pass


class RawArtifactNotFound(RawArtifactStoreError):
    pass


class RawArtifactExpired(RawArtifactNotFound):
    pass


class RawArtifactTooLarge(RawArtifactStoreError):
    pass


class RawArtifactCapacityExceeded(RawArtifactStoreError):
    pass


_RAW_REF = re.compile(r"raw_[A-Za-z0-9_-]{43}\Z")


@dataclass(frozen=True, slots=True)
class _ArtifactRecord:
    payload: bytes
    digest: str
    size_bytes: int
    expires_at: float


class MaskedRawArtifactStore:
    """Session-scoped in-memory storage for already-masked text.

    The class intentionally exposes no generic ``put`` method.  ``put_masked``
    is an assertion by the caller that application-level redaction already ran.
    References contain 256 random bits, reveal no filesystem path, expire by a
    trusted clock, and are only resolvable by the store object that created them.
    ``workspace`` remains provenance metadata; artifact bytes are never written
    into it.
    """

    def __init__(
        self,
        workspace: Path | str,
        *,
        directory: str = ".token-efficiency/raw",
        ttl_seconds: float = 3_600,
        max_artifact_bytes: int = 2_000_000,
        max_total_bytes: int = 20_000_000,
        clock: Callable[[], float] = time.monotonic,
    ) -> None:
        if isinstance(ttl_seconds, bool) or not isinstance(ttl_seconds, (int, float)):
            raise TypeError("ttl_seconds must be numeric")
        if not math.isfinite(ttl_seconds) or ttl_seconds <= 0:
            raise ValueError("ttl_seconds must be positive")
        if any(
            not isinstance(value, int) or isinstance(value, bool)
            for value in (max_artifact_bytes, max_total_bytes)
        ):
            raise TypeError("artifact size limits must be integers")
        if max_artifact_bytes <= 0 or max_total_bytes <= 0:
            raise ValueError("artifact size limits must be positive")
        if max_artifact_bytes > max_total_bytes:
            raise ValueError("max_artifact_bytes cannot exceed max_total_bytes")
        if not callable(clock):
            raise TypeError("clock must be callable")
        relative = Path(directory)
        if relative.is_absolute() or ".." in relative.parts or not relative.parts:
            raise ValueError("artifact directory must be a contained relative path")

        workspace_path = Path(workspace).expanduser().resolve()
        if not workspace_path.is_dir():
            raise ValueError("workspace must be an existing directory")

        self.workspace = workspace_path
        self.ttl_seconds = float(ttl_seconds)
        self.max_artifact_bytes = int(max_artifact_bytes)
        self.max_total_bytes = int(max_total_bytes)
        self._clock = clock
        self._lock = threading.RLock()
        self._records: dict[str, _ArtifactRecord] = {}
        self._closed = False

    @property
    def closed(self) -> bool:
        with self._lock:
            return self._closed

    @staticmethod
    def _validate_ref(ref: str) -> None:
        if not isinstance(ref, str) or not _RAW_REF.fullmatch(ref):
            raise RawArtifactNotFound("invalid raw artifact reference")

    def _ensure_open(self) -> None:
        if self._closed:
            raise RawArtifactStoreError("raw artifact store is closed")

    def _now(self) -> float:
        try:
            now = float(self._clock())
        except Exception as error:
            raise RawArtifactStoreError("raw artifact clock failed") from error
        if not math.isfinite(now):
            raise RawArtifactStoreError("raw artifact clock must be finite")
        return now

    def sweep(self) -> int:
        """Forget refs expired by the trusted clock."""

        now = self._now()
        removed = 0
        with self._lock:
            self._ensure_open()
            expired = [
                ref
                for ref, record in self._records.items()
                if now >= record.expires_at
            ]
            for ref in expired:
                self._records.pop(ref, None)
                removed += 1
        return removed

    def clear(self) -> int:
        """Thread-safely forget every artifact and return the removed count."""

        with self._lock:
            removed = len(self._records)
            self._records.clear()
            return removed

    def discard(self, ref: str) -> bool:
        """Forget one scoped reference, returning whether it was present."""

        self._validate_ref(ref)
        with self._lock:
            self._ensure_open()
            return self._records.pop(ref, None) is not None

    def close(self) -> int:
        """Clear all content and permanently reject future put/get operations."""

        with self._lock:
            removed = len(self._records)
            self._records.clear()
            self._closed = True
            return removed

    def put_masked(self, masked_content: str) -> MaskedArtifactPointer:
        """Store text that the caller has already secret-masked.

        This method cannot verify redaction.  Passing unmasked content violates
        the SPI contract and may persist credentials until TTL expiry.
        """

        if not isinstance(masked_content, str):
            raise TypeError("masked_content must be text")
        payload = masked_content.encode("utf-8")
        size = len(payload)
        if size > self.max_artifact_bytes:
            raise RawArtifactTooLarge(
                f"masked artifact is {size} bytes; limit is {self.max_artifact_bytes}"
            )

        with self._lock:
            self._ensure_open()
            now = self._now()
            expired = [
                ref
                for ref, record in self._records.items()
                if now >= record.expires_at
            ]
            for ref in expired:
                self._records.pop(ref, None)
            total = sum(record.size_bytes for record in self._records.values())
            if total + size > self.max_total_bytes:
                raise RawArtifactCapacityExceeded("masked artifact store is at capacity")

            for _ in range(8):
                ref = f"raw_{secrets.token_urlsafe(32)}"
                if ref in self._records:
                    continue
                digest = _digest_bytes(payload)
                pointer = MaskedArtifactPointer(
                    ref=ref,
                    digest=digest,
                    size_bytes=size,
                    expires_at=now + self.ttl_seconds,
                )
                self._records[ref] = _ArtifactRecord(
                    payload=payload,
                    digest=digest,
                    size_bytes=size,
                    expires_at=pointer.expires_at,
                )
                return pointer
            raise RawArtifactStoreError("could not allocate an unpredictable artifact reference")

    def get_masked(self, ref: str) -> str:
        """Resolve a reference inside this store object's session scope."""

        self._validate_ref(ref)
        with self._lock:
            self._ensure_open()
            record = self._records.get(ref)
            if record is None:
                raise RawArtifactNotFound(
                    "raw artifact does not belong to this session"
                )
            if self._now() >= record.expires_at:
                self._records.pop(ref, None)
                raise RawArtifactExpired("raw artifact has expired")
            payload = record.payload
            if len(payload) > self.max_artifact_bytes:
                raise RawArtifactTooLarge("raw artifact exceeds this store's read limit")
            if len(payload) != record.size_bytes:
                raise RawArtifactNotFound(
                    "raw artifact failed its content integrity check"
                )
            if not secrets.compare_digest(_digest_bytes(payload), record.digest):
                raise RawArtifactNotFound(
                    "raw artifact failed its content integrity check"
                )
            return payload.decode("utf-8")


@dataclass(frozen=True, slots=True)
class MaskedObservation:
    """A tool observation after the harness has applied its secret masker."""

    content: str
    content_type: str = "text/plain"
    reduced_by: tuple[str, ...] = ()
    raw_ref: str | None = None
    raw_digest: str | None = None
    metadata: tuple[tuple[str, object], ...] = ()

    def __post_init__(self) -> None:
        if not isinstance(self.content, str):
            raise TypeError("observation content must be text")
        if not self.content_type or "/" not in self.content_type:
            raise ValueError("content_type must be MIME-like")
        object.__setattr__(self, "reduced_by", tuple(dict.fromkeys(self.reduced_by)))
        object.__setattr__(
            self,
            "metadata",
            tuple((str(key), value) for key, value in self.metadata),
        )
        if (self.raw_ref is None) != (self.raw_digest is None):
            raise ValueError("raw_ref and raw_digest must be set together")


@dataclass(frozen=True, slots=True)
class ObservationReduction:
    content: str
    warnings: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        if not isinstance(self.content, str):
            raise TypeError("reduced observation content must be text")
        object.__setattr__(self, "warnings", _validated_warnings(self.warnings))


@runtime_checkable
class ObservationReducer(Protocol):
    descriptor: ComponentDescriptor

    async def reduce(
        self,
        observation: MaskedObservation,
        *,
        query: str | None = None,
        budget_tokens: int | None = None,
    ) -> ObservationReduction:
        ...


@dataclass(frozen=True, slots=True)
class RequestContext:
    """A logical provider-request copy and its cache-stable prefix boundary."""

    request: Mapping[str, Any]
    frozen_prefix_messages: int = 0
    optimized_by: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        if not isinstance(self.request, Mapping):
            raise TypeError("request must be a mapping")
        if self.frozen_prefix_messages < 0:
            raise ValueError("frozen_prefix_messages cannot be negative")
        object.__setattr__(self, "optimized_by", tuple(dict.fromkeys(self.optimized_by)))


@dataclass(frozen=True, slots=True)
class RequestOptimization:
    request: Mapping[str, Any]
    warnings: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        if not isinstance(self.request, Mapping):
            raise TypeError("optimized request must be a mapping")
        object.__setattr__(self, "warnings", _validated_warnings(self.warnings))


@runtime_checkable
class RequestContextOptimizer(Protocol):
    descriptor: ComponentDescriptor

    async def optimize(
        self,
        context: RequestContext,
        *,
        budget_tokens: int | None = None,
    ) -> RequestOptimization:
        ...


@dataclass(frozen=True, slots=True)
class StableRequestSettings:
    """Provider-neutral settings produced by response policies."""

    instructions: tuple[str, ...] = ()
    max_output_tokens: int | None = None
    verbosity: str | None = None
    metadata: tuple[tuple[str, str], ...] = ()

    def __post_init__(self) -> None:
        if not isinstance(self.instructions, tuple):
            raise TypeError("instructions must be a tuple of strings")
        if len(self.instructions) > _MAX_INSTRUCTIONS:
            raise ValueError(
                f"instructions must contain at most {_MAX_INSTRUCTIONS} entries"
            )
        if any(not isinstance(item, str) for item in self.instructions):
            raise TypeError("instructions must contain only strings")
        if any(len(item) > _MAX_INSTRUCTION_CHARS for item in self.instructions):
            raise ValueError(
                f"instruction entries must be at most {_MAX_INSTRUCTION_CHARS} characters"
            )
        if sum(len(item) for item in self.instructions) > _MAX_INSTRUCTIONS_CHARS:
            raise ValueError(
                f"instructions must total at most {_MAX_INSTRUCTIONS_CHARS} characters"
            )
        if not isinstance(self.metadata, tuple):
            raise TypeError("metadata must be a tuple of string pairs")
        if len(self.metadata) > _MAX_METADATA_ITEMS:
            raise ValueError(f"metadata must contain at most {_MAX_METADATA_ITEMS} entries")
        seen_keys: set[str] = set()
        for item in self.metadata:
            if not isinstance(item, tuple) or len(item) != 2:
                raise TypeError("metadata entries must be two-item tuples")
            key, value = item
            if not isinstance(key, str) or not isinstance(value, str):
                raise TypeError("metadata keys and values must be strings")
            if not key or len(key) > _MAX_METADATA_KEY_CHARS:
                raise ValueError(
                    f"metadata keys must be 1-{_MAX_METADATA_KEY_CHARS} characters"
                )
            if len(value) > _MAX_METADATA_VALUE_CHARS:
                raise ValueError(
                    f"metadata values must be at most {_MAX_METADATA_VALUE_CHARS} characters"
                )
            if key in seen_keys:
                raise ValueError("metadata keys must be unique")
            seen_keys.add(key)
        if self.max_output_tokens is not None:
            if not isinstance(self.max_output_tokens, int) or isinstance(
                self.max_output_tokens, bool
            ):
                raise TypeError("max_output_tokens must be an integer when set")
            if self.max_output_tokens <= 0:
                raise ValueError("max_output_tokens must be positive when set")
        if self.verbosity is not None:
            if not isinstance(self.verbosity, str):
                raise TypeError("verbosity must be text when set")
            if not self.verbosity or len(self.verbosity) > 64:
                raise ValueError("verbosity must be 1-64 characters when set")


@dataclass(frozen=True, slots=True)
class ResponsePolicyContext:
    task: str
    settings: StableRequestSettings = field(default_factory=StableRequestSettings)
    provider_capabilities: tuple[str, ...] = ()
    budget_tokens: int | None = None
    concise_requested: bool = False
    applied_by: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        if not isinstance(self.task, str):
            raise TypeError("task must be text")
        if not isinstance(self.settings, StableRequestSettings):
            raise TypeError("settings must be StableRequestSettings")
        object.__setattr__(self, "provider_capabilities", tuple(self.provider_capabilities))
        object.__setattr__(self, "applied_by", tuple(dict.fromkeys(self.applied_by)))
        if self.budget_tokens is not None and self.budget_tokens <= 0:
            raise ValueError("budget_tokens must be positive when set")


@dataclass(frozen=True, slots=True)
class ResponsePolicyPlan:
    settings: StableRequestSettings
    warnings: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        if not isinstance(self.settings, StableRequestSettings):
            raise TypeError("response policy settings must be StableRequestSettings")
        object.__setattr__(self, "warnings", _validated_warnings(self.warnings))


@runtime_checkable
class ResponsePolicy(Protocol):
    descriptor: ComponentDescriptor

    async def plan(self, context: ResponsePolicyContext) -> ResponsePolicyPlan:
        ...


@dataclass(frozen=True, slots=True)
class LifecycleHealth:
    status: LifecycleStatus = LifecycleStatus.OK
    detail: str = ""

    def __post_init__(self) -> None:
        object.__setattr__(self, "status", LifecycleStatus(self.status))
        if not isinstance(self.detail, str):
            raise TypeError("health detail must be text")
        if self.detail and not _RECEIPT_REASON.fullmatch(self.detail):
            raise ValueError("health detail must be a bounded machine-readable code")


@runtime_checkable
class ComponentLifecycle(Protocol):
    """Optional lifecycle hooks understood by :class:`TokenEfficiencyRuntime`."""

    async def initialize(self, services: object | None = None) -> None:
        ...

    def health(self) -> LifecycleHealth:
        ...

    async def close(self, deadline_seconds: float | None = None) -> None:
        ...


@dataclass(frozen=True, slots=True)
class ComponentLifecycleRecord:
    component_id: str
    component_version: str
    stage: ComponentStage
    phase: LifecyclePhase
    status: LifecycleStatus
    detail: str = ""
    elapsed_ms: float = 0.0

    def __post_init__(self) -> None:
        if not _COMPONENT_ID.fullmatch(self.component_id):
            raise ValueError("lifecycle component id is invalid")
        if not _COMPONENT_VERSION.fullmatch(self.component_version):
            raise ValueError("lifecycle component version is invalid")
        object.__setattr__(self, "stage", ComponentStage(self.stage))
        object.__setattr__(self, "phase", LifecyclePhase(self.phase))
        object.__setattr__(self, "status", LifecycleStatus(self.status))
        if not isinstance(self.detail, str):
            raise TypeError("lifecycle detail must be text")
        if self.detail and not _RECEIPT_REASON.fullmatch(self.detail):
            raise ValueError("lifecycle detail must be a bounded machine-readable code")
        if self.elapsed_ms < 0 or not math.isfinite(self.elapsed_ms):
            raise ValueError("elapsed_ms must be finite and non-negative")

    def as_dict(self) -> dict[str, object]:
        return {
            "component_id": self.component_id,
            "component_version": self.component_version,
            "stage": self.stage.value,
            "phase": self.phase.value,
            "status": self.status.value,
            "detail": self.detail,
            "elapsed_ms": self.elapsed_ms,
        }


@dataclass(frozen=True, slots=True)
class LifecycleReport:
    phase: LifecyclePhase
    status: LifecycleStatus
    components: tuple[ComponentLifecycleRecord, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, "phase", LifecyclePhase(self.phase))
        object.__setattr__(self, "status", LifecycleStatus(self.status))
        object.__setattr__(self, "components", tuple(self.components))

    @property
    def healthy(self) -> bool:
        return self.status in {LifecycleStatus.OK, LifecycleStatus.SKIPPED}

    def as_dict(self) -> dict[str, object]:
        return {
            "phase": self.phase.value,
            "status": self.status.value,
            "healthy": self.healthy,
            "components": [record.as_dict() for record in self.components],
        }


@dataclass(frozen=True, slots=True)
class ObservationOutcome:
    observation: MaskedObservation
    receipts: tuple[OptimizationReceipt, ...] = ()
    warnings: tuple[str, ...] = ()


@dataclass(frozen=True, slots=True)
class RequestOutcome:
    context: RequestContext
    receipts: tuple[OptimizationReceipt, ...] = ()


@dataclass(frozen=True, slots=True)
class ResponsePolicyOutcome:
    context: ResponsePolicyContext
    receipts: tuple[OptimizationReceipt, ...] = ()


@dataclass(frozen=True, slots=True)
class ComponentBinding:
    component: object
    mode: OptimizationMode | None = None

    def __post_init__(self) -> None:
        descriptor = getattr(self.component, "descriptor", None)
        if not isinstance(descriptor, ComponentDescriptor):
            raise TypeError("component must expose a ComponentDescriptor")
        if self.mode is not None:
            object.__setattr__(self, "mode", OptimizationMode(self.mode))

    @property
    def descriptor(self) -> ComponentDescriptor:
        return self.component.descriptor  # type: ignore[no-any-return,union-attr]


@dataclass(frozen=True, slots=True)
class StageComponents:
    observation_reducers: tuple[ComponentBinding, ...] = ()
    request_optimizers: tuple[ComponentBinding, ...] = ()
    response_policies: tuple[ComponentBinding, ...] = ()


class TokenEfficiencyRegistry:
    """Explicit, stage-specific registry; it performs no package discovery."""

    def __init__(self) -> None:
        self._observation: list[ComponentBinding] = []
        self._request: list[ComponentBinding] = []
        self._response: list[ComponentBinding] = []
        self._ids: set[str] = set()

    def _register(
        self,
        component: object,
        stage: ComponentStage,
        target: list[ComponentBinding],
        mode: OptimizationMode | None,
    ) -> object:
        binding = ComponentBinding(component, mode)
        if binding.descriptor.stage is not stage:
            raise ValueError(
                f"component {binding.descriptor.id} belongs to "
                f"{binding.descriptor.stage.value}, not {stage.value}"
            )
        if binding.descriptor.id in self._ids:
            raise ValueError(f"duplicate token-efficiency component id: {binding.descriptor.id}")
        target.append(binding)
        self._ids.add(binding.descriptor.id)
        return component

    def register_observation(
        self,
        component: ObservationReducer,
        *,
        mode: OptimizationMode | None = None,
    ) -> ObservationReducer:
        return self._register(
            component, ComponentStage.OBSERVATION, self._observation, mode
        )  # type: ignore[return-value]

    def register_request_optimizer(
        self,
        component: RequestContextOptimizer,
        *,
        mode: OptimizationMode | None = None,
    ) -> RequestContextOptimizer:
        return self._register(
            component, ComponentStage.REQUEST_CONTEXT, self._request, mode
        )  # type: ignore[return-value]

    def register_response_policy(
        self,
        component: ResponsePolicy,
        *,
        mode: OptimizationMode | None = None,
    ) -> ResponsePolicy:
        return self._register(
            component, ComponentStage.RESPONSE_POLICY, self._response, mode
        )  # type: ignore[return-value]

    def snapshot(self) -> StageComponents:
        return StageComponents(
            observation_reducers=tuple(self._observation),
            request_optimizers=tuple(self._request),
            response_policies=tuple(self._response),
        )

    def runtime(
        self,
        *,
        default_mode: OptimizationMode = OptimizationMode.OFF,
        raw_store: MaskedRawArtifactStore | None = None,
        inflation_guard: bool = True,
        prevent_double_reduction: bool = True,
        token_counter: Callable[[str], int] = _default_token_counter,
    ) -> "TokenEfficiencyRuntime":
        return TokenEfficiencyRuntime(
            components=self.snapshot(),
            default_mode=default_mode,
            raw_store=raw_store,
            inflation_guard=inflation_guard,
            prevent_double_reduction=prevent_double_reduction,
            token_counter=token_counter,
        )


def _supports_content_type(descriptor: ComponentDescriptor, content_type: str) -> bool:
    for supported in descriptor.content_types:
        if supported == "*/*" or supported == content_type:
            return True
        if supported.endswith("/*") and content_type.startswith(supported[:-1]):
            return True
    return False


async def _await_component(value: Any, timeout_ms: int) -> Any:
    if not inspect.isawaitable(value):
        raise TypeError("component method must return an awaitable")
    return await asyncio.wait_for(value, timeout=timeout_ms / 1_000)


@dataclass(slots=True)
class _RuntimeLifecycleState:
    """Shared component lifecycle/admission coordinator for runtime clones."""

    lifecycle_lock: asyncio.Lock = field(default_factory=asyncio.Lock)
    drained: asyncio.Event = field(default_factory=asyncio.Event)
    state_lock: threading.RLock = field(default_factory=threading.RLock)
    initialize_report: LifecycleReport | None = None
    close_report: LifecycleReport | None = None
    initializing: bool = False
    closing: bool = False
    closed: bool = False
    active_calls: int = 0
    health_active: bool = False
    component_locks: dict[int, asyncio.Lock] = field(default_factory=dict)

    def __post_init__(self) -> None:
        self.drained.set()

    def begin_close(self) -> None:
        with self.state_lock:
            self.closing = True

    def enter_component_call(self) -> bool:
        with self.state_lock:
            if self.initializing or self.closing or self.closed or self.health_active:
                return False
            if self.active_calls == 0:
                self.drained.clear()
            self.active_calls += 1
            return True

    def leave_component_call(self) -> None:
        with self.state_lock:
            if self.active_calls <= 0:
                raise RuntimeError("unbalanced token-efficiency runtime call gate")
            self.active_calls -= 1
            if self.active_calls == 0:
                self.drained.set()

    def admission_denial_reason(self) -> str:
        with self.state_lock:
            if self.closing or self.closed:
                return "runtime_closed"
            return "runtime_busy"

    def enter_health(self) -> bool:
        with self.state_lock:
            if self.initializing or self.closing or self.closed or self.active_calls:
                return False
            self.health_active = True
            self.active_calls = 1
            self.drained.clear()
            return True

    def leave_health(self) -> None:
        with self.state_lock:
            if not self.health_active or self.active_calls != 1:
                raise RuntimeError("unbalanced token-efficiency health gate")
            self.health_active = False
            self.active_calls = 0
            self.drained.set()

    def component_lock(self, component: object) -> asyncio.Lock:
        with self.state_lock:
            identity = id(component)
            lock = self.component_locks.get(identity)
            if lock is None:
                lock = asyncio.Lock()
                self.component_locks[identity] = lock
            return lock


@dataclass(frozen=True, slots=True)
class TokenEfficiencyRuntime:
    """Immutable stage container with fail-open component orchestration.

    A runtime and all clones created by :meth:`with_raw_store` share one
    lifecycle/admission coordinator and must be driven by one asyncio event
    loop at a time. Components execute in-process and are trusted: async
    timeouts are cooperative and cannot preempt CPU-bound plugin code. The raw
    artifact store itself is thread-safe. A component object must belong to one
    root runtime tree; derive session variants with :meth:`with_raw_store`
    instead of constructing independent runtimes around the same object.
    """

    components: StageComponents = field(default_factory=StageComponents)
    default_mode: OptimizationMode = OptimizationMode.OFF
    raw_store: MaskedRawArtifactStore | None = None
    inflation_guard: bool = True
    prevent_double_reduction: bool = True
    token_counter: Callable[[str], int] = _default_token_counter
    _lifecycle_state: _RuntimeLifecycleState = field(
        default_factory=_RuntimeLifecycleState,
        init=False,
        repr=False,
        compare=False,
    )

    def __post_init__(self) -> None:
        object.__setattr__(self, "default_mode", OptimizationMode(self.default_mode))
        if not callable(self.token_counter):
            raise TypeError("token_counter must be callable")

    @property
    def observation_enforced(self) -> bool:
        """True only when an observation component can change the live result."""

        return any(
            self._mode(binding) is OptimizationMode.ENFORCE
            for binding in self.components.observation_reducers
        )

    def with_raw_store(
        self, store: MaskedRawArtifactStore | None
    ) -> "TokenEfficiencyRuntime":
        """Bind session storage while sharing component lifecycle/admission state."""

        clone = replace(self, raw_store=store)
        object.__setattr__(clone, "_lifecycle_state", self._lifecycle_state)
        return clone

    def _lifecycle_bindings(self) -> tuple[ComponentBinding, ...]:
        """Return enabled snapshot order with each component object only once."""

        ordered = (
            *self.components.observation_reducers,
            *self.components.request_optimizers,
            *self.components.response_policies,
        )
        unique: list[ComponentBinding] = []
        seen: set[int] = set()
        for binding in ordered:
            if self._mode(binding) is OptimizationMode.OFF:
                continue
            identity = id(binding.component)
            if identity not in seen:
                seen.add(identity)
                unique.append(binding)
        return tuple(unique)

    @staticmethod
    def _lifecycle_skipped(phase: LifecyclePhase) -> LifecycleReport:
        return LifecycleReport(phase, LifecycleStatus.SKIPPED, ())

    def _close_deadline_exceeded(self) -> LifecycleReport:
        records = tuple(
            ComponentLifecycleRecord(
                binding.descriptor.id,
                binding.descriptor.version,
                binding.descriptor.stage,
                LifecyclePhase.CLOSE,
                LifecycleStatus.ERROR,
                "deadline_exceeded",
            )
            for binding in reversed(self._lifecycle_bindings())
        )
        return LifecycleReport(LifecyclePhase.CLOSE, LifecycleStatus.ERROR, records)

    @staticmethod
    def _lifecycle_status(
        records: tuple[ComponentLifecycleRecord, ...],
    ) -> LifecycleStatus:
        statuses = {record.status for record in records}
        if LifecycleStatus.ERROR in statuses:
            return LifecycleStatus.ERROR
        if LifecycleStatus.DEGRADED in statuses:
            return LifecycleStatus.DEGRADED
        if records and statuses == {LifecycleStatus.SKIPPED}:
            return LifecycleStatus.SKIPPED
        return LifecycleStatus.OK

    @staticmethod
    async def _call_lifecycle_hook(
        hook: Callable[..., Any],
        *args: Any,
        timeout_seconds: float,
    ) -> Any:
        result = hook(*args)
        if inspect.isawaitable(result):
            return await asyncio.wait_for(result, timeout=timeout_seconds)
        return result

    async def initialize(self, services: object | None = None) -> LifecycleReport:
        """Initialize enabled components at most once for this runtime."""

        state = self._lifecycle_state
        async with state.lifecycle_lock:
            with state.state_lock:
                if state.initialize_report is not None:
                    return state.initialize_report
                if state.closing or state.closed:
                    return self._lifecycle_skipped(LifecyclePhase.INITIALIZE)
                state.initializing = True
            try:
                report = await self._initialize_components(services)
                with state.state_lock:
                    state.initialize_report = report
                return report
            finally:
                with state.state_lock:
                    state.initializing = False

    async def _initialize_components(
        self, services: object | None = None
    ) -> LifecycleReport:
        """Initialize optional component hooks in immutable snapshot order."""

        records: list[ComponentLifecycleRecord] = []
        for binding in self._lifecycle_bindings():
            descriptor = binding.descriptor
            hook = getattr(binding.component, "initialize", None)
            if not callable(hook):
                records.append(
                    ComponentLifecycleRecord(
                        descriptor.id,
                        descriptor.version,
                        descriptor.stage,
                        LifecyclePhase.INITIALIZE,
                        LifecycleStatus.SKIPPED,
                        "hook_not_implemented",
                    )
                )
                continue
            started = time.perf_counter()
            try:
                async with self._lifecycle_state.component_lock(binding.component):
                    await self._call_lifecycle_hook(
                        hook,
                        services,
                        timeout_seconds=descriptor.timeout_ms / 1_000,
                    )
                status = LifecycleStatus.OK
                detail = "initialized"
            except Exception as error:
                status = LifecycleStatus.ERROR
                detail = _component_error_reason(error)
            records.append(
                ComponentLifecycleRecord(
                    descriptor.id,
                    descriptor.version,
                    descriptor.stage,
                    LifecyclePhase.INITIALIZE,
                    status,
                    detail,
                    (time.perf_counter() - started) * 1_000,
                )
            )
        frozen = tuple(records)
        return LifecycleReport(
            LifecyclePhase.INITIALIZE, self._lifecycle_status(frozen), frozen
        )

    def health(self) -> LifecycleReport:
        """Run synchronous health hooks without racing component calls or close."""

        state = self._lifecycle_state
        if not state.enter_health():
            return self._lifecycle_skipped(LifecyclePhase.HEALTH)
        try:
            return self._health_components()
        finally:
            state.leave_health()

    def _health_components(self) -> LifecycleReport:
        """Collect synchronous component health without trusting free-form data."""

        records: list[ComponentLifecycleRecord] = []
        for binding in self._lifecycle_bindings():
            descriptor = binding.descriptor
            hook = getattr(binding.component, "health", None)
            if not callable(hook):
                records.append(
                    ComponentLifecycleRecord(
                        descriptor.id,
                        descriptor.version,
                        descriptor.stage,
                        LifecyclePhase.HEALTH,
                        LifecycleStatus.SKIPPED,
                        "hook_not_implemented",
                    )
                )
                continue
            started = time.perf_counter()
            try:
                result = hook()
                if inspect.isawaitable(result):
                    close_coroutine = getattr(result, "close", None)
                    if callable(close_coroutine):
                        close_coroutine()
                    raise TypeError("health hook must be synchronous")
                if result is None or result is True:
                    health = LifecycleHealth()
                elif result is False:
                    health = LifecycleHealth(
                        LifecycleStatus.DEGRADED, "component_reported_unhealthy"
                    )
                elif isinstance(result, LifecycleHealth):
                    health = LifecycleHealth(result.status, result.detail)
                else:
                    raise TypeError("health hook returned the wrong result type")
                status = health.status
                detail = health.detail or "healthy"
            except Exception as error:
                status = LifecycleStatus.ERROR
                detail = _component_error_reason(error)
            records.append(
                ComponentLifecycleRecord(
                    descriptor.id,
                    descriptor.version,
                    descriptor.stage,
                    LifecyclePhase.HEALTH,
                    status,
                    detail,
                    (time.perf_counter() - started) * 1_000,
                )
            )
        frozen = tuple(records)
        return LifecycleReport(LifecyclePhase.HEALTH, self._lifecycle_status(frozen), frozen)

    async def close(self, deadline_seconds: float | None = None) -> LifecycleReport:
        """Stop admission and close enabled components at most once."""

        if deadline_seconds is not None and deadline_seconds <= 0:
            raise ValueError("deadline_seconds must be positive when set")
        state = self._lifecycle_state
        state.begin_close()
        deadline = (
            time.monotonic() + deadline_seconds
            if deadline_seconds is not None
            else None
        )
        try:
            if deadline is None:
                await state.drained.wait()
            else:
                await asyncio.wait_for(
                    state.drained.wait(),
                    timeout=max(0.0, deadline - time.monotonic()),
                )
        except TimeoutError:
            return self._close_deadline_exceeded()

        lock_acquired = False
        try:
            if deadline is None:
                await state.lifecycle_lock.acquire()
            else:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    return self._close_deadline_exceeded()
                await asyncio.wait_for(
                    state.lifecycle_lock.acquire(), timeout=remaining
                )
            lock_acquired = True
            with state.state_lock:
                if state.close_report is not None:
                    return state.close_report
            remaining = None if deadline is None else deadline - time.monotonic()
            if remaining is not None and remaining <= 0:
                return self._close_deadline_exceeded()
            report = await self._close_components(remaining)
            with state.state_lock:
                state.close_report = report
                state.closed = True
            return report
        except TimeoutError:
            return self._close_deadline_exceeded()
        finally:
            if lock_acquired:
                state.lifecycle_lock.release()

    async def _close_components(
        self, deadline_seconds: float | None = None
    ) -> LifecycleReport:
        """Close optional hooks in reverse initialization order within a deadline."""

        if deadline_seconds is not None and deadline_seconds <= 0:
            raise ValueError("deadline_seconds must be positive when set")
        deadline = (
            time.monotonic() + deadline_seconds
            if deadline_seconds is not None
            else None
        )
        records: list[ComponentLifecycleRecord] = []
        for binding in reversed(self._lifecycle_bindings()):
            descriptor = binding.descriptor
            hook = getattr(binding.component, "close", None)
            if not callable(hook):
                records.append(
                    ComponentLifecycleRecord(
                        descriptor.id,
                        descriptor.version,
                        descriptor.stage,
                        LifecyclePhase.CLOSE,
                        LifecycleStatus.SKIPPED,
                        "hook_not_implemented",
                    )
                )
                continue
            remaining = None if deadline is None else deadline - time.monotonic()
            if remaining is not None and remaining <= 0:
                records.append(
                    ComponentLifecycleRecord(
                        descriptor.id,
                        descriptor.version,
                        descriptor.stage,
                        LifecyclePhase.CLOSE,
                        LifecycleStatus.ERROR,
                        "deadline_exceeded",
                    )
                )
                continue
            timeout = descriptor.timeout_ms / 1_000
            if remaining is not None:
                timeout = min(timeout, remaining)
            started = time.perf_counter()
            try:
                await self._call_lifecycle_hook(
                    hook,
                    remaining,
                    timeout_seconds=timeout,
                )
                status = LifecycleStatus.OK
                detail = "closed"
            except Exception as error:
                status = LifecycleStatus.ERROR
                detail = _component_error_reason(error)
            records.append(
                ComponentLifecycleRecord(
                    descriptor.id,
                    descriptor.version,
                    descriptor.stage,
                    LifecyclePhase.CLOSE,
                    status,
                    detail,
                    (time.perf_counter() - started) * 1_000,
                )
            )
        frozen = tuple(records)
        return LifecycleReport(LifecyclePhase.CLOSE, self._lifecycle_status(frozen), frozen)

    def _mode(self, binding: ComponentBinding) -> OptimizationMode:
        return binding.mode or self.default_mode

    def _receipt(
        self,
        descriptor: ComponentDescriptor,
        mode: OptimizationMode,
        status: OptimizationStatus,
        reason: str,
        before: bytes,
        after: bytes,
        *,
        raw_ref: str | None = None,
        raw_digest: str | None = None,
        warnings: tuple[str, ...] = (),
        elapsed_ms: float = 0.0,
    ) -> OptimizationReceipt:
        before_text = before.decode("utf-8", errors="replace")
        after_text = after.decode("utf-8", errors="replace")
        return OptimizationReceipt(
            component_id=descriptor.id,
            component_version=descriptor.version,
            stage=descriptor.stage,
            mode=mode,
            status=status,
            reason=reason,
            raw_bytes=len(before),
            projected_bytes=len(after),
            tokens_before_estimate=self._estimate_tokens(before_text),
            tokens_after_estimate=self._estimate_tokens(after_text),
            input_digest=_digest_bytes(before),
            output_digest=_digest_bytes(after),
            lossiness=descriptor.lossiness,
            deterministic=descriptor.deterministic,
            raw_ref=raw_ref,
            raw_digest=raw_digest,
            warnings=warnings,
            elapsed_ms=elapsed_ms,
        )

    def _skipped_receipt(
        self,
        binding: ComponentBinding,
        reason: str,
        payload: bytes,
        *,
        raw_ref: str | None = None,
        raw_digest: str | None = None,
        warnings: tuple[str, ...] = (),
    ) -> OptimizationReceipt:
        return self._receipt(
            binding.descriptor,
            self._mode(binding),
            OptimizationStatus.PASSTHROUGH,
            reason,
            payload,
            payload,
            raw_ref=raw_ref,
            raw_digest=raw_digest,
            warnings=warnings,
        )

    def _estimate_tokens(self, value: str) -> int:
        try:
            estimate = int(self.token_counter(value))
            if estimate < 0:
                raise ValueError("token estimate cannot be negative")
            return estimate
        except Exception:
            # Metrics cannot be allowed to break the value-preserving fallback.
            return _default_token_counter(value)

    def _inflates(self, before: bytes, after: bytes) -> bool:
        if not self.inflation_guard:
            return False
        before_tokens = self._estimate_tokens(before.decode("utf-8", errors="replace"))
        after_tokens = self._estimate_tokens(after.decode("utf-8", errors="replace"))
        return len(after) > len(before) or after_tokens > before_tokens

    async def reduce_observation(
        self,
        observation: MaskedObservation,
        *,
        query: str | None = None,
        budget_tokens: int | None = None,
        persist_masked_raw: bool = False,
    ) -> ObservationOutcome:
        state = self._lifecycle_state
        if not state.enter_component_call():
            reason = state.admission_denial_reason()
            payload = observation.content.encode("utf-8")
            receipts = tuple(
                self._skipped_receipt(
                    binding,
                    reason,
                    payload,
                    raw_ref=observation.raw_ref,
                    raw_digest=observation.raw_digest,
                    warnings=(reason,),
                )
                for binding in self.components.observation_reducers
            )
            return ObservationOutcome(observation, receipts, (reason,))
        try:
            return await self._reduce_observation_components(
                observation,
                query=query,
                budget_tokens=budget_tokens,
                persist_masked_raw=persist_masked_raw,
            )
        finally:
            state.leave_component_call()

    async def _reduce_observation_components(
        self,
        observation: MaskedObservation,
        *,
        query: str | None = None,
        budget_tokens: int | None = None,
        persist_masked_raw: bool = False,
    ) -> ObservationOutcome:
        """Run reducers over an already-masked observation.

        In shadow mode candidates are measured but never returned.  In enforce
        mode the first reducer that changes the observation owns it; later
        reducers receive a double-reduction receipt without being called.
        """

        current = observation
        runtime_warnings: list[str] = []

        receipts: list[OptimizationReceipt] = []
        for binding in self.components.observation_reducers:
            descriptor = binding.descriptor
            mode = self._mode(binding)
            before = current.content.encode("utf-8")
            shared_warnings = _merged_warnings(tuple(runtime_warnings))
            if mode is OptimizationMode.OFF:
                receipts.append(
                    self._skipped_receipt(
                        binding,
                        "mode_off",
                        before,
                        raw_ref=current.raw_ref,
                        raw_digest=current.raw_digest,
                        warnings=shared_warnings,
                    )
                )
                continue
            if self.prevent_double_reduction and current.reduced_by:
                receipts.append(
                    self._skipped_receipt(
                        binding,
                        f"double_reduction_guard:{current.reduced_by[-1]}",
                        before,
                        raw_ref=current.raw_ref,
                        raw_digest=current.raw_digest,
                        warnings=shared_warnings,
                    )
                )
                continue
            if not _supports_content_type(descriptor, current.content_type):
                receipts.append(
                    self._skipped_receipt(
                        binding,
                        "unsupported_content_type",
                        before,
                        raw_ref=current.raw_ref,
                        raw_digest=current.raw_digest,
                        warnings=shared_warnings,
                    )
                )
                continue
            if descriptor.max_input_bytes is not None and len(before) > descriptor.max_input_bytes:
                receipts.append(
                    self._skipped_receipt(
                        binding,
                        "max_input_bytes_exceeded",
                        before,
                        raw_ref=current.raw_ref,
                        raw_digest=current.raw_digest,
                        warnings=shared_warnings,
                    )
                )
                continue

            started = time.perf_counter()
            try:
                async with self._lifecycle_state.component_lock(binding.component):
                    result = await _await_component(
                        binding.component.reduce(  # type: ignore[attr-defined]
                            current, query=query, budget_tokens=budget_tokens
                        ),
                        descriptor.timeout_ms,
                    )
                if not isinstance(result, ObservationReduction):
                    raise TypeError("observation reducer returned the wrong result type")
                # Re-run dataclass validation at the trust boundary. Frozen
                # instances can still be forged with object.__setattr__.
                result = ObservationReduction(result.content, result.warnings)
                after = result.content.encode("utf-8")
                elapsed = (time.perf_counter() - started) * 1_000
                warnings = _merged_warnings(shared_warnings, result.warnings)
                if result.content == current.content:
                    receipts.append(
                        self._receipt(
                            descriptor,
                            mode,
                            OptimizationStatus.PASSTHROUGH,
                            "component_passthrough",
                            before,
                            after,
                            raw_ref=current.raw_ref,
                            raw_digest=current.raw_digest,
                            warnings=warnings,
                            elapsed_ms=elapsed,
                        )
                    )
                elif mode is OptimizationMode.SHADOW:
                    receipts.append(
                        self._receipt(
                            descriptor,
                            mode,
                            OptimizationStatus.SHADOWED,
                            "shadow_candidate",
                            before,
                            after,
                            raw_ref=current.raw_ref,
                            raw_digest=current.raw_digest,
                            warnings=warnings,
                            elapsed_ms=elapsed,
                        )
                    )
                elif self._inflates(before, after):
                    receipts.append(
                        self._receipt(
                            descriptor,
                            mode,
                            OptimizationStatus.DEGRADED,
                            "inflation_guard",
                            before,
                            after,
                            raw_ref=current.raw_ref,
                            raw_digest=current.raw_digest,
                            warnings=warnings,
                            elapsed_ms=elapsed,
                        )
                    )
                else:
                    raw_ref = current.raw_ref
                    raw_digest = current.raw_digest
                    if descriptor.lossiness is Lossiness.RECOVERABLE and raw_ref is None:
                        storage_warning: str | None = None
                        if not persist_masked_raw or self.raw_store is None:
                            storage_warning = "raw_store_unavailable"
                        else:
                            try:
                                pointer = self.raw_store.put_masked(current.content)
                                raw_ref = pointer.ref
                                raw_digest = pointer.digest
                            except Exception as error:  # fail-open at the storage seam
                                storage_warning = "raw_store_error"
                        if storage_warning is not None:
                            runtime_warnings.append(storage_warning)
                            receipt_warnings = _merged_warnings(
                                (storage_warning,), warnings
                            )
                            receipts.append(
                                self._receipt(
                                    descriptor,
                                    mode,
                                    OptimizationStatus.DEGRADED,
                                    "recovery_unavailable",
                                    before,
                                    after,
                                    warnings=receipt_warnings,
                                    elapsed_ms=elapsed,
                                )
                            )
                            continue
                    receipts.append(
                        self._receipt(
                            descriptor,
                            mode,
                            OptimizationStatus.APPLIED,
                            "reduced",
                            before,
                            after,
                            raw_ref=raw_ref,
                            raw_digest=raw_digest,
                            warnings=warnings,
                            elapsed_ms=elapsed,
                        )
                    )
                    current = MaskedObservation(
                        content=result.content,
                        content_type=current.content_type,
                        reduced_by=(*current.reduced_by, descriptor.id),
                        raw_ref=raw_ref,
                        raw_digest=raw_digest,
                        metadata=current.metadata,
                    )
            except Exception as error:  # component failures never break the tool result
                elapsed = (time.perf_counter() - started) * 1_000
                receipts.append(
                    self._receipt(
                        descriptor,
                        mode,
                        OptimizationStatus.ERROR,
                        _component_error_reason(error),
                        before,
                        before,
                        raw_ref=current.raw_ref,
                        raw_digest=current.raw_digest,
                        warnings=shared_warnings,
                        elapsed_ms=elapsed,
                    )
                )
        return ObservationOutcome(current, tuple(receipts), tuple(runtime_warnings))

    async def optimize_request(
        self,
        context: RequestContext,
        *,
        budget_tokens: int | None = None,
    ) -> RequestOutcome:
        state = self._lifecycle_state
        if not state.enter_component_call():
            reason = state.admission_denial_reason()
            current = RequestContext(
                request=copy.deepcopy(dict(context.request)),
                frozen_prefix_messages=context.frozen_prefix_messages,
                optimized_by=context.optimized_by,
            )
            payload = _stable_bytes(current.request)
            receipts = tuple(
                self._skipped_receipt(binding, reason, payload)
                for binding in self.components.request_optimizers
            )
            return RequestOutcome(current, receipts)
        try:
            return await self._optimize_request_components(
                context, budget_tokens=budget_tokens
            )
        finally:
            state.leave_component_call()

    async def _optimize_request_components(
        self,
        context: RequestContext,
        *,
        budget_tokens: int | None = None,
    ) -> RequestOutcome:
        """Optimize request copies while preserving the declared frozen prefix."""

        current = RequestContext(
            request=copy.deepcopy(dict(context.request)),
            frozen_prefix_messages=context.frozen_prefix_messages,
            optimized_by=context.optimized_by,
        )
        receipts: list[OptimizationReceipt] = []
        for binding in self.components.request_optimizers:
            descriptor = binding.descriptor
            mode = self._mode(binding)
            before = _stable_bytes(current.request)
            if mode is OptimizationMode.OFF:
                receipts.append(self._skipped_receipt(binding, "mode_off", before))
                continue
            if self.prevent_double_reduction and current.optimized_by:
                receipts.append(
                    self._skipped_receipt(
                        binding,
                        f"double_reduction_guard:{current.optimized_by[-1]}",
                        before,
                    )
                )
                continue
            if descriptor.max_input_bytes is not None and len(before) > descriptor.max_input_bytes:
                receipts.append(
                    self._skipped_receipt(binding, "max_input_bytes_exceeded", before)
                )
                continue

            component_context = RequestContext(
                request=copy.deepcopy(dict(current.request)),
                frozen_prefix_messages=current.frozen_prefix_messages,
                optimized_by=current.optimized_by,
            )
            started = time.perf_counter()
            try:
                async with self._lifecycle_state.component_lock(binding.component):
                    result = await _await_component(
                        binding.component.optimize(  # type: ignore[attr-defined]
                            component_context, budget_tokens=budget_tokens
                        ),
                        descriptor.timeout_ms,
                    )
                if not isinstance(result, RequestOptimization):
                    raise TypeError("request optimizer returned the wrong result type")
                result = RequestOptimization(result.request, result.warnings)
                candidate = copy.deepcopy(dict(result.request))
                after = _stable_bytes(candidate)
                elapsed = (time.perf_counter() - started) * 1_000
                if not self._frozen_prefix_unchanged(
                    current.request, candidate, current.frozen_prefix_messages
                ):
                    receipts.append(
                        self._receipt(
                            descriptor,
                            mode,
                            OptimizationStatus.DEGRADED,
                            "frozen_prefix_guard",
                            before,
                            after,
                            warnings=result.warnings,
                            elapsed_ms=elapsed,
                        )
                    )
                elif after == before:
                    receipts.append(
                        self._receipt(
                            descriptor,
                            mode,
                            OptimizationStatus.PASSTHROUGH,
                            "component_passthrough",
                            before,
                            after,
                            warnings=result.warnings,
                            elapsed_ms=elapsed,
                        )
                    )
                elif mode is OptimizationMode.SHADOW:
                    receipts.append(
                        self._receipt(
                            descriptor,
                            mode,
                            OptimizationStatus.SHADOWED,
                            "shadow_candidate",
                            before,
                            after,
                            warnings=result.warnings,
                            elapsed_ms=elapsed,
                        )
                    )
                elif self._inflates(before, after):
                    receipts.append(
                        self._receipt(
                            descriptor,
                            mode,
                            OptimizationStatus.DEGRADED,
                            "inflation_guard",
                            before,
                            after,
                            warnings=result.warnings,
                            elapsed_ms=elapsed,
                        )
                    )
                else:
                    receipts.append(
                        self._receipt(
                            descriptor,
                            mode,
                            OptimizationStatus.APPLIED,
                            "optimized",
                            before,
                            after,
                            warnings=result.warnings,
                            elapsed_ms=elapsed,
                        )
                    )
                    current = RequestContext(
                        request=candidate,
                        frozen_prefix_messages=current.frozen_prefix_messages,
                        optimized_by=(*current.optimized_by, descriptor.id),
                    )
            except Exception as error:  # fail-open and keep the last good copy
                elapsed = (time.perf_counter() - started) * 1_000
                receipts.append(
                    self._receipt(
                        descriptor,
                        mode,
                        OptimizationStatus.ERROR,
                        _component_error_reason(error),
                        before,
                        before,
                        elapsed_ms=elapsed,
                    )
                )
        return RequestOutcome(current, tuple(receipts))

    @staticmethod
    def _frozen_prefix_unchanged(
        before: Mapping[str, Any], after: Mapping[str, Any], count: int
    ) -> bool:
        if count == 0:
            return True
        before_messages = before.get("messages")
        after_messages = after.get("messages")
        if not isinstance(before_messages, list) or not isinstance(after_messages, list):
            return False
        if len(before_messages) < count or len(after_messages) < count:
            return False
        return _stable_bytes(before_messages[:count]) == _stable_bytes(after_messages[:count])

    async def plan_response(self, context: ResponsePolicyContext) -> ResponsePolicyOutcome:
        state = self._lifecycle_state
        if not state.enter_component_call():
            reason = state.admission_denial_reason()
            payload = _stable_bytes(context.settings)
            receipts = tuple(
                self._skipped_receipt(binding, reason, payload)
                for binding in self.components.response_policies
            )
            return ResponsePolicyOutcome(context, receipts)
        try:
            return await self._plan_response_components(context)
        finally:
            state.leave_component_call()

    async def _plan_response_components(
        self, context: ResponsePolicyContext
    ) -> ResponsePolicyOutcome:
        """Compose response settings without allowing policy failures to escape."""

        current = context
        receipts: list[OptimizationReceipt] = []
        for binding in self.components.response_policies:
            descriptor = binding.descriptor
            mode = self._mode(binding)
            before = _stable_bytes(current.settings)
            if mode is OptimizationMode.OFF:
                receipts.append(self._skipped_receipt(binding, "mode_off", before))
                continue
            if self.prevent_double_reduction and current.applied_by:
                receipts.append(
                    self._skipped_receipt(
                        binding,
                        f"double_reduction_guard:{current.applied_by[-1]}",
                        before,
                    )
                )
                continue
            started = time.perf_counter()
            try:
                async with self._lifecycle_state.component_lock(binding.component):
                    result = await _await_component(
                        binding.component.plan(current),  # type: ignore[attr-defined]
                        descriptor.timeout_ms,
                    )
                if not isinstance(result, ResponsePolicyPlan):
                    raise TypeError("response policy returned the wrong result type")
                validated_settings = StableRequestSettings(
                    instructions=result.settings.instructions,
                    max_output_tokens=result.settings.max_output_tokens,
                    verbosity=result.settings.verbosity,
                    metadata=result.settings.metadata,
                )
                result = ResponsePolicyPlan(validated_settings, result.warnings)
                after = _stable_bytes(result.settings)
                elapsed = (time.perf_counter() - started) * 1_000
                output_limits = (
                    current.settings.max_output_tokens,
                    current.budget_tokens,
                )
                output_ceiling = min(
                    (limit for limit in output_limits if limit is not None),
                    default=None,
                )
                if result.settings == current.settings:
                    receipts.append(
                        self._receipt(
                            descriptor,
                            mode,
                            OptimizationStatus.PASSTHROUGH,
                            "component_passthrough",
                            before,
                            after,
                            warnings=result.warnings,
                            elapsed_ms=elapsed,
                        )
                    )
                elif mode is OptimizationMode.SHADOW:
                    receipts.append(
                        self._receipt(
                            descriptor,
                            mode,
                            OptimizationStatus.SHADOWED,
                            "shadow_candidate",
                            before,
                            after,
                            warnings=result.warnings,
                            elapsed_ms=elapsed,
                        )
                    )
                elif (
                    output_ceiling is not None
                    and (
                        result.settings.max_output_tokens is None
                        or result.settings.max_output_tokens > output_ceiling
                    )
                ):
                    receipts.append(
                        self._receipt(
                            descriptor,
                            mode,
                            OptimizationStatus.DEGRADED,
                            "max_output_tokens_inflation",
                            before,
                            after,
                            warnings=result.warnings,
                            elapsed_ms=elapsed,
                        )
                    )
                else:
                    receipts.append(
                        self._receipt(
                            descriptor,
                            mode,
                            OptimizationStatus.APPLIED,
                            "policy_applied",
                            before,
                            after,
                            warnings=result.warnings,
                            elapsed_ms=elapsed,
                        )
                    )
                    current = ResponsePolicyContext(
                        task=current.task,
                        settings=result.settings,
                        provider_capabilities=current.provider_capabilities,
                        budget_tokens=current.budget_tokens,
                        concise_requested=current.concise_requested,
                        applied_by=(*current.applied_by, descriptor.id),
                    )
            except Exception as error:  # fail-open and retain stable settings
                elapsed = (time.perf_counter() - started) * 1_000
                receipts.append(
                    self._receipt(
                        descriptor,
                        mode,
                        OptimizationStatus.ERROR,
                        _component_error_reason(error),
                        before,
                        before,
                        elapsed_ms=elapsed,
                    )
                )
        return ResponsePolicyOutcome(current, tuple(receipts))


class PassthroughObservationReducer:
    descriptor = ComponentDescriptor(
        id="passthrough-observation",
        version="1",
        stage=ComponentStage.OBSERVATION,
        content_types=("*/*",),
        deterministic=True,
        lossiness=Lossiness.LOSSLESS,
    )

    async def reduce(
        self,
        observation: MaskedObservation,
        *,
        query: str | None = None,
        budget_tokens: int | None = None,
    ) -> ObservationReduction:
        del query, budget_tokens
        return ObservationReduction(observation.content)


_ANSI_ESCAPE = re.compile(
    r"(?:\x1b\][^\x07]*(?:\x07|\x1b\\))|(?:\x1b\[[0-?]*[ -/]*[@-~])"
)
_CRITICAL_LINE = re.compile(
    r"(?:\b(?:error|failed?|failure|exception|traceback|panic|fatal|warn(?:ing)?|"
    r"denied|timeout|exit)\b|\d|(?:^|\s)(?:[/\\]|[A-Za-z]:[/\\])|"
    r"\.[A-Za-z0-9_+-]{1,12}(?::\d+)?\b)",
    re.IGNORECASE,
)


class DeterministicLosslessReducer:
    """Fold display noise while retaining a scoped path to the masked source."""

    descriptor = ComponentDescriptor(
        id="deterministic-lossless",
        version="1",
        stage=ComponentStage.OBSERVATION,
        content_types=("text/*", "application/json"),
        deterministic=True,
        lossiness=Lossiness.RECOVERABLE,
        recoverable=True,
        cost_tier="fast",
        network_access=False,
    )

    async def reduce(
        self,
        observation: MaskedObservation,
        *,
        query: str | None = None,
        budget_tokens: int | None = None,
    ) -> ObservationReduction:
        del query, budget_tokens
        without_ansi, ansi_count = _ANSI_ESCAPE.subn("", observation.content)
        lines = without_ansi.split("\n")
        folded: list[str] = []
        duplicate_runs = 0
        blank_lines_removed = 0
        index = 0
        while index < len(lines):
            line = lines[index]
            if not line.strip("\r"):
                end = index + 1
                while end < len(lines) and not lines[end].strip("\r"):
                    end += 1
                run = end - index
                folded.append(line)
                blank_lines_removed += max(0, run - 1)
                index = end
                continue

            end = index + 1
            while end < len(lines) and lines[end] == line:
                end += 1
            run = end - index
            # Error text, numbers, and path-like lines are never folded.  This
            # favors diagnostic fidelity over compression on failure paths.
            if run > 1 and not _CRITICAL_LINE.search(line):
                marker = f"[repeated {run - 1} additional time{'s' if run != 2 else ''}]"
                folded.extend((line, marker))
                duplicate_runs += 1
            else:
                folded.extend(lines[index:end])
            index = end

        content = "\n".join(folded)
        warnings: list[str] = []
        if ansi_count:
            warnings.append(f"removed_ansi:{ansi_count}")
        if duplicate_runs:
            warnings.append(f"folded_duplicate_runs:{duplicate_runs}")
        if blank_lines_removed:
            warnings.append(f"folded_blank_lines:{blank_lines_removed}")
        return ObservationReduction(content, tuple(warnings))


class PassthroughRequestContextOptimizer:
    descriptor = ComponentDescriptor(
        id="passthrough-request",
        version="1",
        stage=ComponentStage.REQUEST_CONTEXT,
    )

    async def optimize(
        self,
        context: RequestContext,
        *,
        budget_tokens: int | None = None,
    ) -> RequestOptimization:
        del budget_tokens
        return RequestOptimization(copy.deepcopy(dict(context.request)))


class PassthroughResponsePolicy:
    descriptor = ComponentDescriptor(
        id="passthrough-response",
        version="1",
        stage=ComponentStage.RESPONSE_POLICY,
    )

    async def plan(self, context: ResponsePolicyContext) -> ResponsePolicyPlan:
        return ResponsePolicyPlan(context.settings)


@dataclass(frozen=True, slots=True)
class ConciseResponsePolicySettings:
    """Opt-in settings for a Caveman-inspired concise response policy."""

    require_opt_in: bool = True
    max_output_tokens: int | None = 1_200
    instruction: str = (
        "Answer concisely. Remove filler, hedging, repetition, and tool-call narration. "
        "Preserve negation, code, commands, errors, numbers, units, paths, technical "
        "terms, and security or destructive-action warnings exactly enough to act on."
    )

    def __post_init__(self) -> None:
        if self.max_output_tokens is not None and self.max_output_tokens <= 0:
            raise ValueError("max_output_tokens must be positive when set")
        if not self.instruction.strip():
            raise ValueError("concise response instruction cannot be empty")


class ConciseResponsePolicy:
    descriptor = ComponentDescriptor(
        id="concise-response",
        version="1",
        stage=ComponentStage.RESPONSE_POLICY,
        deterministic=True,
        lossiness=Lossiness.LOSSY,
        cost_tier="fast",
        network_access=False,
    )

    def __init__(self, settings: ConciseResponsePolicySettings | None = None) -> None:
        self.settings = settings or ConciseResponsePolicySettings()

    async def plan(self, context: ResponsePolicyContext) -> ResponsePolicyPlan:
        if self.settings.require_opt_in and not context.concise_requested:
            return ResponsePolicyPlan(context.settings)
        instructions = context.settings.instructions
        if self.settings.instruction not in instructions:
            instructions = (*instructions, self.settings.instruction)
        candidates = [
            value
            for value in (
                context.settings.max_output_tokens,
                self.settings.max_output_tokens,
                context.budget_tokens,
            )
            if value is not None
        ]
        max_output_tokens = min(candidates) if candidates else None
        metadata = tuple(
            (key, value)
            for key, value in context.settings.metadata
            if key != "response_policy"
        )
        return ResponsePolicyPlan(
            StableRequestSettings(
                instructions=instructions,
                max_output_tokens=max_output_tokens,
                verbosity="concise",
                metadata=(*metadata, ("response_policy", self.descriptor.id)),
            )
        )


__all__ = [
    "ComponentBinding",
    "ComponentDescriptor",
    "ComponentLifecycle",
    "ComponentLifecycleRecord",
    "ComponentStage",
    "ConciseResponsePolicy",
    "ConciseResponsePolicySettings",
    "DeterministicLosslessReducer",
    "LifecycleHealth",
    "LifecyclePhase",
    "LifecycleReport",
    "LifecycleStatus",
    "Lossiness",
    "MaskedArtifactPointer",
    "MaskedObservation",
    "MaskedRawArtifactStore",
    "ObservationOutcome",
    "ObservationReducer",
    "ObservationReduction",
    "OptimizationMode",
    "OptimizationReceipt",
    "OptimizationStatus",
    "PassthroughObservationReducer",
    "PassthroughRequestContextOptimizer",
    "PassthroughResponsePolicy",
    "RawArtifactCapacityExceeded",
    "RawArtifactExpired",
    "RawArtifactNotFound",
    "RawArtifactStoreError",
    "RawArtifactTooLarge",
    "RequestContext",
    "RequestContextOptimizer",
    "RequestOptimization",
    "RequestOutcome",
    "ResponsePolicy",
    "ResponsePolicyContext",
    "ResponsePolicyOutcome",
    "ResponsePolicyPlan",
    "StableRequestSettings",
    "StageComponents",
    "TokenEfficiencyRegistry",
    "TokenEfficiencyRuntime",
]
