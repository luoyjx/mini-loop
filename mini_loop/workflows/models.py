"""Versioned declarative models for the standalone workflow core.

The module intentionally has no dependency on ``Agent`` or ``SessionManager``.
It is safe to import while the workflow feature remains disabled.
"""

from __future__ import annotations

import hashlib
import json
import time
import uuid
from dataclasses import asdict, dataclass, field, is_dataclass
from enum import Enum
from typing import Any, Mapping

from ..run_context import RunContext


SCHEMA_VERSION = 1
HARD_MAX_CONCURRENT_AGENTS = 4
HARD_MAX_AGENTS_PER_RUN = 32
READ_ONLY_WORKFLOW_TOOLS = frozenset({"read_file", "glob"})


class NodeKind(str, Enum):
    AGENT = "agent"
    MAP = "map"
    REDUCE = "reduce"
    VERIFY = "verify"
    SEQUENCE = "sequence"
    BRANCH = "branch"
    REPEAT_UNTIL = "repeat_until"
    BARRIER = "barrier"
    RETURN = "return"


class RunStatus(str, Enum):
    CREATED = "CREATED"
    PLANNING = "PLANNING"
    AWAITING_APPROVAL = "AWAITING_APPROVAL"
    QUEUED = "QUEUED"
    RUNNING = "RUNNING"
    PAUSING = "PAUSING"
    PAUSED = "PAUSED"
    WAITING_APPROVAL = "WAITING_APPROVAL"
    CANCELLING = "CANCELLING"
    CANCELLED = "CANCELLED"
    REJECTED = "REJECTED"
    COMPLETED = "COMPLETED"
    FAILED = "FAILED"

    @property
    def is_terminal(self) -> bool:
        return self in {
            RunStatus.CANCELLED,
            RunStatus.REJECTED,
            RunStatus.COMPLETED,
            RunStatus.FAILED,
        }


class NodeStatus(str, Enum):
    PENDING = "PENDING"
    RUNNING = "RUNNING"
    SUCCEEDED = "SUCCEEDED"
    UNVERIFIED = "UNVERIFIED"
    FAILED = "FAILED"
    CANCELLED = "CANCELLED"

    @property
    def is_terminal(self) -> bool:
        return self in {
            NodeStatus.SUCCEEDED,
            NodeStatus.UNVERIFIED,
            NodeStatus.FAILED,
            NodeStatus.CANCELLED,
        }

    @property
    def satisfies_dependency(self) -> bool:
        return self in {NodeStatus.SUCCEEDED, NodeStatus.UNVERIFIED}


class AttemptStatus(str, Enum):
    PENDING = "PENDING"
    CLAIMED = "CLAIMED"
    RUNNING = "RUNNING"
    SUCCEEDED = "SUCCEEDED"
    FAILED = "FAILED"
    UNKNOWN = "UNKNOWN"
    CANCELLED = "CANCELLED"

    @property
    def is_terminal(self) -> bool:
        return self in {
            AttemptStatus.SUCCEEDED,
            AttemptStatus.FAILED,
            AttemptStatus.UNKNOWN,
            AttemptStatus.CANCELLED,
        }


class VerificationStatus(str, Enum):
    NOT_APPLICABLE = "not_applicable"
    VERIFIED = "verified"
    REFUTED = "refuted"
    UNVERIFIED = "unverified"


class DefinitionSource(str, Enum):
    DYNAMIC = "dynamic"
    PROJECT = "project"
    PERSONAL = "personal"
    PLUGIN = "plugin"


def _json_value(value: Any) -> Any:
    if isinstance(value, Enum):
        return value.value
    if is_dataclass(value):
        return _json_value(asdict(value))
    if isinstance(value, Mapping):
        return {str(key): _json_value(item) for key, item in value.items()}
    if isinstance(value, (list, tuple)):
        return [_json_value(item) for item in value]
    return value


def canonical_json(value: Any) -> str:
    """Serialize JSON-compatible data deterministically."""

    return json.dumps(
        _json_value(value),
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
        allow_nan=False,
    )


def content_hash(value: Any, *, prefix: str = "sha256") -> str:
    digest = hashlib.sha256(canonical_json(value).encode("utf-8")).hexdigest()
    return f"{prefix}:{digest}"


@dataclass(frozen=True)
class BudgetPolicy:
    size_guideline: str = "small"
    max_concurrent_agents: int = HARD_MAX_CONCURRENT_AGENTS
    max_agents: int = HARD_MAX_AGENTS_PER_RUN
    max_rounds: int = 4
    wall_time_seconds: float = 900.0
    token_budget: int | None = None

    @classmethod
    def from_dict(cls, data: Mapping[str, Any] | None) -> "BudgetPolicy":
        return cls(**dict(data or {}))


@dataclass(frozen=True)
class ToolPolicy:
    origin_authority_required: str = "explicit_human"
    agent_profile: str = "workflow-readonly"
    allowed_tools: tuple[str, ...] = ("read_file", "glob")

    def __post_init__(self) -> None:
        object.__setattr__(self, "allowed_tools", tuple(self.allowed_tools))

    @classmethod
    def from_dict(cls, data: Mapping[str, Any] | None) -> "ToolPolicy":
        payload = dict(data or {})
        if "allowed_tools" in payload:
            payload["allowed_tools"] = tuple(payload["allowed_tools"])
        return cls(**payload)


@dataclass(frozen=True)
class WorkflowNode:
    id: str
    kind: NodeKind
    needs: tuple[str, ...] = ()
    prompt_template: str = ""
    output_schema: Mapping[str, Any] = field(default_factory=lambda: {"type": "object"})
    items_from: str | None = None
    max_rounds: int | None = None

    def __post_init__(self) -> None:
        object.__setattr__(self, "kind", NodeKind(self.kind))
        object.__setattr__(self, "needs", tuple(self.needs))
        object.__setattr__(self, "output_schema", dict(self.output_schema))

    @classmethod
    def from_dict(cls, data: Mapping[str, Any]) -> "WorkflowNode":
        payload = dict(data)
        payload["kind"] = NodeKind(payload["kind"])
        payload["needs"] = tuple(payload.get("needs", ()))
        return cls(**payload)

    def to_dict(self) -> dict[str, Any]:
        return {
            "id": self.id,
            "kind": self.kind.value,
            "needs": list(self.needs),
            "prompt_template": self.prompt_template,
            "output_schema": _json_value(self.output_schema),
            "items_from": self.items_from,
            "max_rounds": self.max_rounds,
        }


@dataclass(frozen=True)
class WorkflowDefinition:
    name: str
    nodes: tuple[WorkflowNode, ...]
    return_from: str
    schema_version: int = SCHEMA_VERSION
    description: str = ""
    revision: str = ""
    definition_id: str = ""
    parent_revision: str | None = None
    source: DefinitionSource = DefinitionSource.DYNAMIC
    source_version: str | None = None
    input_schema: Mapping[str, Any] = field(default_factory=lambda: {"type": "object"})
    output_schema: Mapping[str, Any] = field(default_factory=lambda: {"type": "object"})
    budget: BudgetPolicy = field(default_factory=BudgetPolicy)
    policy: ToolPolicy = field(default_factory=ToolPolicy)
    definition_hash: str = field(init=False)

    def __post_init__(self) -> None:
        object.__setattr__(self, "nodes", tuple(self.nodes))
        object.__setattr__(self, "source", DefinitionSource(self.source))
        object.__setattr__(self, "input_schema", dict(self.input_schema))
        object.__setattr__(self, "output_schema", dict(self.output_schema))
        digest = content_hash(self.semantic_dict(), prefix="wfdef")
        object.__setattr__(self, "definition_hash", digest)
        suffix = digest.split(":", 1)[1][:16]
        if not self.revision:
            object.__setattr__(self, "revision", f"wfdef_{suffix}")
        if not self.definition_id:
            object.__setattr__(self, "definition_id", f"wf_{suffix}")

    @classmethod
    def from_dict(cls, data: Mapping[str, Any]) -> "WorkflowDefinition":
        payload = dict(data)
        # ``definition_hash`` is derived from the semantic definition.  Ignore a
        # serialized copy so deserialization always recomputes and verifies the
        # canonical value instead of treating it as constructor input.
        payload.pop("definition_hash", None)
        payload["nodes"] = tuple(
            node if isinstance(node, WorkflowNode) else WorkflowNode.from_dict(node)
            for node in payload.get("nodes", ())
        )
        payload["budget"] = (
            payload["budget"]
            if isinstance(payload.get("budget"), BudgetPolicy)
            else BudgetPolicy.from_dict(payload.get("budget"))
        )
        payload["policy"] = (
            payload["policy"]
            if isinstance(payload.get("policy"), ToolPolicy)
            else ToolPolicy.from_dict(payload.get("policy"))
        )
        if "source" in payload:
            payload["source"] = DefinitionSource(payload["source"])
        return cls(**payload)

    def semantic_dict(self) -> dict[str, Any]:
        """Return the immutable definition content covered by its hash."""

        return {
            "schema_version": self.schema_version,
            "name": self.name,
            "description": self.description,
            "source": self.source.value,
            "source_version": self.source_version,
            "input_schema": _json_value(self.input_schema),
            "output_schema": _json_value(self.output_schema),
            "budget": _json_value(self.budget),
            "policy": _json_value(self.policy),
            "nodes": [node.to_dict() for node in self.nodes],
            "return_from": self.return_from,
        }

    def to_dict(self) -> dict[str, Any]:
        return {
            **self.semantic_dict(),
            "definition_id": self.definition_id,
            "revision": self.revision,
            "parent_revision": self.parent_revision,
            "definition_hash": self.definition_hash,
        }


@dataclass
class WorkflowRun:
    run_id: str
    definition_revision: str
    session_id: str
    run_context: RunContext
    idempotency_key: str
    args: dict[str, Any]
    status: RunStatus = RunStatus.QUEUED
    version: int = 0
    parent_run_id: str | None = None
    launch_action_id: str | None = None
    created_at: float = field(default_factory=time.time)
    started_at: float | None = None
    ended_at: float | None = None
    active_node_ids: tuple[str, ...] = ()
    event_cursor: int = 0
    attempts_used: int = 0
    policy_snapshot_hash: str = ""
    workspace_baseline: str | None = None
    final_artifact_id: str | None = None
    error: str | None = None
    cancel_reason: str | None = None

    def __post_init__(self) -> None:
        self.status = RunStatus(self.status)
        self.active_node_ids = tuple(self.active_node_ids)
        self.args = dict(self.args)

    @property
    def is_terminal(self) -> bool:
        return self.status.is_terminal


@dataclass
class NodeState:
    run_id: str
    node_id: str
    status: NodeStatus = NodeStatus.PENDING
    version: int = 0
    attempt_ids: tuple[str, ...] = ()
    result_artifact_ids: tuple[str, ...] = ()
    error: str | None = None

    def __post_init__(self) -> None:
        self.status = NodeStatus(self.status)
        self.attempt_ids = tuple(self.attempt_ids)
        self.result_artifact_ids = tuple(self.result_artifact_ids)


@dataclass(frozen=True)
class AttemptClaim:
    node_id: str
    agent_id: str
    spawn_index: int
    parent_agent_id: str | None = None


@dataclass
class NodeAttempt:
    attempt_id: str
    run_id: str
    node_id: str
    attempt: int
    agent_id: str
    spawn_index: int
    status: AttemptStatus = AttemptStatus.CLAIMED
    version: int = 0
    parent_agent_id: str | None = None
    started_at: float | None = None
    heartbeat_at: float | None = None
    ended_at: float | None = None
    result_artifact_id: str | None = None
    verification_status: VerificationStatus = VerificationStatus.NOT_APPLICABLE
    error: str | None = None

    def __post_init__(self) -> None:
        self.status = AttemptStatus(self.status)
        self.verification_status = VerificationStatus(self.verification_status)


@dataclass(frozen=True)
class Artifact:
    artifact_id: str
    run_id: str
    node_id: str
    attempt_id: str
    value: Any
    content_hash: str
    schema_hash: str
    verification_status: VerificationStatus = VerificationStatus.NOT_APPLICABLE
    schema_valid: bool = True
    media_type: str = "application/json"
    created_at: float = field(default_factory=time.time)

    @classmethod
    def create(
        cls,
        *,
        run_id: str,
        node_id: str,
        attempt_id: str,
        value: Any,
        schema: Mapping[str, Any],
        verification_status: VerificationStatus = VerificationStatus.NOT_APPLICABLE,
        schema_valid: bool = True,
    ) -> "Artifact":
        return cls(
            artifact_id=f"artifact_{uuid.uuid4().hex[:20]}",
            run_id=run_id,
            node_id=node_id,
            attempt_id=attempt_id,
            value=_json_value(value),
            content_hash=content_hash(value, prefix="artifact"),
            schema_hash=content_hash(schema, prefix="schema"),
            verification_status=verification_status,
            schema_valid=schema_valid,
        )


@dataclass(frozen=True)
class OutboxMessage:
    message_id: str
    run_id: str
    session_id: str
    kind: str
    payload: Mapping[str, Any]
    created_at: float = field(default_factory=time.time)
    claim_token: str | None = None
    claimed_at: float | None = None
    delivered_at: float | None = None
