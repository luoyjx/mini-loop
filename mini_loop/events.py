"""Typed event helpers shared by the experimental workflow runtime.

``AgentSession.emit`` remains the single envelope/sequence owner.  This module
only validates workflow payload correlation and converts it into the existing
session event shape, so workflows do not create a second event bus.
"""

from __future__ import annotations

import time
import uuid
from dataclasses import dataclass, field
from typing import Any


WORKFLOW_EVENT_KINDS = frozenset({
    "workflow_planned",
    "workflow_approval_required",
    "workflow_decision_recorded",
    "workflow_rejected",
    "workflow_started",
    "workflow_phase_started",
    "workflow_node_claimed",
    "workflow_agent_started",
    "workflow_agent_progress",
    "workflow_agent_completed",
    "workflow_verdict_recorded",
    "workflow_checkpointed",
    "workflow_paused",
    "workflow_resumed",
    "workflow_cancelled",
    "workflow_failed",
    "workflow_completed",
    "workflow_result_enqueued",
})

_NODE_KINDS = frozenset({
    "workflow_node_claimed",
    "workflow_agent_started",
    "workflow_agent_progress",
    "workflow_agent_completed",
    "workflow_verdict_recorded",
})
_ATTEMPT_KINDS = frozenset({
    "workflow_agent_started",
    "workflow_agent_progress",
    "workflow_agent_completed",
    "workflow_verdict_recorded",
})
_AGENT_KINDS = frozenset({
    "workflow_agent_started",
    "workflow_agent_progress",
    "workflow_agent_completed",
})


@dataclass(frozen=True)
class WorkflowEvent:
    """A validated workflow payload for the existing session event stream."""

    kind: str
    session_id: str
    run_id: str
    workflow_name: str
    definition_revision: str
    payload: dict[str, Any] = field(default_factory=dict)
    phase_id: str | None = None
    node_id: str | None = None
    attempt_id: str | None = None
    agent_id: str | None = None
    parent_agent_id: str | None = None
    payload_version: int = 1
    event_id: str = field(default_factory=lambda: f"wfevt_{uuid.uuid4().hex}")
    occurred_at: float = field(default_factory=time.time)

    def __post_init__(self) -> None:
        if self.kind not in WORKFLOW_EVENT_KINDS:
            raise ValueError(f"unsupported workflow event kind: {self.kind}")
        for name in ("session_id", "run_id", "workflow_name", "definition_revision"):
            if not getattr(self, name):
                raise ValueError(f"{name} is required")
        if self.kind in _NODE_KINDS and not self.node_id:
            raise ValueError(f"{self.kind} requires node_id")
        if self.kind in _ATTEMPT_KINDS and not self.attempt_id:
            raise ValueError(f"{self.kind} requires attempt_id")
        if self.kind in _AGENT_KINDS and not self.agent_id:
            raise ValueError(f"{self.kind} requires agent_id")
        if self.kind == "workflow_phase_started" and not self.phase_id:
            raise ValueError("workflow_phase_started requires phase_id")
        if self.payload_version != 1:
            raise ValueError("unsupported workflow event payload_version")

    def as_session_event(self) -> dict[str, Any]:
        event: dict[str, Any] = {
            "type": self.kind,
            "kind": self.kind,
            "event_id": self.event_id,
            "occurred_at": self.occurred_at,
            "session_id": self.session_id,
            "run_id": self.run_id,
            "workflow_run_id": self.run_id,
            "workflow_name": self.workflow_name,
            "definition_revision": self.definition_revision,
            "payload_version": self.payload_version,
            "payload": dict(self.payload),
        }
        for name in (
            "phase_id",
            "node_id",
            "attempt_id",
            "agent_id",
            "parent_agent_id",
        ):
            value = getattr(self, name)
            if value is not None:
                event[name] = value
        return event

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: workflow payloads are validated at construction and rejected before entering the stream; invalid ones never exist to detect."
)
