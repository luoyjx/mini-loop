"""Immutable provenance and authority carried through one agent run."""

from __future__ import annotations

import uuid
from dataclasses import dataclass, field, replace


EXPLICIT_HUMAN = "explicit_human"
PEER_AGENT = "peer_agent"
UNTRUSTED = "untrusted"
WORKFLOW_LAUNCH = "workflow.launch"
WORKFLOW_MANAGE = "workflow.manage"


def _message_id() -> str:
    return f"msg_{uuid.uuid4().hex}"


@dataclass(frozen=True, slots=True)
class RunContext:
    """Trusted metadata attached by the caller at a run boundary.

    Omitted contexts are deliberately untrusted. Authenticated entry points
    must opt in to ``explicit_human`` authority rather than inheriting it from
    model-controlled text or mutable agent state.
    """

    message_id: str = field(default_factory=_message_id)
    origin: str = "api"
    actor_id: str | None = None
    channel: str = "internal"
    authority: str = UNTRUSTED
    stamped_by: str = "mini_loop"
    delegated_by: str | None = None
    parent_message_id: str | None = None
    approved_capabilities: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(
            self,
            "approved_capabilities",
            tuple(sorted(set(self.approved_capabilities))),
        )

    @classmethod
    def default(cls) -> "RunContext":
        """Return a safe context for legacy and unauthenticated callers."""

        return cls()

    @classmethod
    def explicit_human(
        cls,
        *,
        actor_id: str | None = None,
        channel: str = "local",
        stamped_by: str = "trusted_local",
        approved_capabilities: tuple[str, ...] = (),
    ) -> "RunContext":
        """Create a context only for a caller that authenticated the human."""

        return cls(
            origin=EXPLICIT_HUMAN,
            actor_id=actor_id,
            channel=channel,
            authority=EXPLICIT_HUMAN,
            stamped_by=stamped_by,
            approved_capabilities=approved_capabilities,
        )

    @classmethod
    def peer_agent(
        cls,
        *,
        delegated_by: str,
        actor_id: str | None = None,
        stamped_by: str = "mini_loop",
        parent_message_id: str | None = None,
    ) -> "RunContext":
        """Create a non-human context for autonomous agent work."""

        return cls(
            origin=PEER_AGENT,
            actor_id=actor_id,
            channel="agent",
            authority=PEER_AGENT,
            stamped_by=stamped_by,
            delegated_by=delegated_by,
            parent_message_id=parent_message_id,
        )

    def derive_peer_agent(
        self,
        *,
        delegated_by: str,
        actor_id: str | None = None,
    ) -> "RunContext":
        """Delegate without allowing explicit-human authority to propagate."""

        return type(self).peer_agent(
            delegated_by=delegated_by,
            actor_id=actor_id,
            stamped_by=self.stamped_by,
            parent_message_id=self.message_id,
        )

    def with_new_message(
        self,
        *,
        approved_capabilities: tuple[str, ...] = (),
    ) -> "RunContext":
        """Reuse provenance for a new input without carrying old approvals."""

        return replace(
            self,
            message_id=_message_id(),
            parent_message_id=self.message_id,
            approved_capabilities=approved_capabilities,
        )

    def allows(self, capability: str) -> bool:
        return capability in self.approved_capabilities

    def as_dict(self) -> dict[str, object]:
        return {
            "message_id": self.message_id,
            "origin": self.origin,
            "actor_id": self.actor_id,
            "channel": self.channel,
            "authority": self.authority,
            "stamped_by": self.stamped_by,
            "delegated_by": self.delegated_by,
            "parent_message_id": self.parent_message_id,
            "approved_capabilities": list(self.approved_capabilities),
        }


__all__ = [
    "EXPLICIT_HUMAN",
    "PEER_AGENT",
    "UNTRUSTED",
    "WORKFLOW_LAUNCH",
    "WORKFLOW_MANAGE",
    "RunContext",
]
