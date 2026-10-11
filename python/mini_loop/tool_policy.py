"""Role-aware tool catalogue selection.

Child agents inherit semantic capabilities from their parent's registry rather
than rebuilding a list of concrete tool names.  Capability names are policy
data: adding a new read-only code-context provider automatically makes it
available to Explore workers, while write and process execution remain absent.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from types import MappingProxyType
from typing import Mapping, Protocol, runtime_checkable

from .registry import ToolRegistry


EXPLORE_CAPABILITIES = frozenset(
    {
        "repo.read",
        "repo.search",
        "repo.semantic_outline",
        "repo.symbol",
        "repo.references",
    }
)

WORKER_CAPABILITIES = EXPLORE_CAPABILITIES | frozenset(
    {"workspace.write", "process.exec", "observation.recover"}
)


@runtime_checkable
class RoleToolPolicy(Protocol):
    """Select a child catalogue from the parent's declared capabilities."""

    def select(self, role: str, parent: ToolRegistry) -> ToolRegistry: ...


@dataclass(frozen=True, slots=True)
class CapabilityRoleToolPolicy:
    """Default least-authority policy for Explore and Worker subagents."""

    capabilities_by_role: Mapping[str, frozenset[str]] = field(
        default_factory=lambda: {
            "explore": EXPLORE_CAPABILITIES,
            "worker": WORKER_CAPABILITIES,
            "general-purpose": WORKER_CAPABILITIES,
        }
    )

    def __post_init__(self) -> None:
        normalized = {
            str(role).strip().lower(): frozenset(capabilities)
            for role, capabilities in self.capabilities_by_role.items()
        }
        object.__setattr__(
            self,
            "capabilities_by_role",
            MappingProxyType(normalized),
        )

    def select(self, role: str, parent: ToolRegistry) -> ToolRegistry:
        key = role.strip().lower()
        try:
            allowed = self.capabilities_by_role[key]
        except KeyError as error:
            raise ValueError(f"unknown agent role: {role!r}") from error
        return parent.with_capabilities(allowed)


DEFAULT_ROLE_TOOL_POLICY = CapabilityRoleToolPolicy()


__all__ = [
    "CapabilityRoleToolPolicy",
    "DEFAULT_ROLE_TOOL_POLICY",
    "EXPLORE_CAPABILITIES",
    "RoleToolPolicy",
    "WORKER_CAPABILITIES",
]

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: a static mapping consulted at composition; a wrong entry is a review problem, invisible to the process running under it."
)
