"""Owner-bound local skill and memory resources.

Raw principal identifiers are authority, not filesystem names.  This module
maps an exact owner string to one digest-only directory and one immutable
resource bundle that can be shared by that owner's sessions.
"""

from __future__ import annotations

import hashlib
import threading
from dataclasses import dataclass
from pathlib import Path
from typing import Literal

from .memory import MemoryStore
from .problems import ProblemLog
from .skills import LayeredSkillLoader, SkillLoader

__all__ = ["UserResourceResolver", "UserResources"]


@dataclass(frozen=True, slots=True)
class UserResources:
    """One owner's immutable resource bindings.

    The stores themselves retain their normal internal mutability; freezing the
    bundle prevents a live session from being rebound to another catalogue,
    memory store, or root after its owner has been resolved.
    """

    scope: Literal["user"]
    skills: LayeredSkillLoader
    memory: MemoryStore
    root: Path


class UserResourceResolver:
    """Resolve and cache digest-rooted resources for exact owner identifiers."""

    def __init__(
        self,
        root: Path,
        agent_skills: SkillLoader,
        secrets=None,
    ) -> None:
        self.root = Path(root).resolve()
        self.root.mkdir(parents=True, exist_ok=True)
        self.agent_skills = agent_skills
        self.secrets = secrets
        self._resources: dict[str, UserResources] = {}
        self._lock = threading.RLock()

    @property
    def problems(self) -> ProblemLog:
        """Operator view over current owner-local skill problem logs.

        The live layered loader keeps only its owner's problems, while this
        derived view lets the manager audit report all resolved owners without
        sharing one mutable log back into their model-facing sessions.
        """

        combined = ProblemLog()
        with self._lock:
            resources = tuple(self._resources.values())
        for resource in resources:
            log = resource.skills.problems
            rendered = log.summary() if hasattr(log, "summary") else list(log)
            for problem in rendered:
                if str(problem).startswith("agent:"):
                    # The manager's deployment SkillLoader reports this shared
                    # source once through its own audit channel.
                    continue
                combined.append(f"{resource.root.name}: {problem}")
        return combined

    @staticmethod
    def _owner_key(owner: str) -> str:
        digest = hashlib.sha256(owner.encode("utf-8")).hexdigest()
        return f"u-{digest}"

    def _directory(self, path: Path) -> Path:
        # A pre-planted link at the otherwise-safe digest name would undo the
        # namespace guarantee by redirecting skills or memory outside the
        # configured root.  Refuse it rather than following it.
        if path.is_symlink():
            raise RuntimeError("user resource directory must not be a symlink")
        path.mkdir(parents=True, exist_ok=True)
        resolved = path.resolve()
        try:
            resolved.relative_to(self.root)
        except ValueError as error:
            raise RuntimeError(
                "user resource directory resolves outside the configured root"
            ) from error
        return resolved

    def for_owner(self, owner: str) -> UserResources:
        """Return the one cached resource bundle for ``owner``.

        The model never supplies this value.  Callers bind it from the trusted
        session owner before agent construction, then reuse the returned
        snapshot for every session belonging to that exact identifier.
        """

        if not isinstance(owner, str) or not owner:
            raise ValueError("owner must be a non-empty string")
        with self._lock:
            existing = self._resources.get(owner)
            if existing is not None:
                return existing

            owner_root = self._directory(self.root / self._owner_key(owner))
            skills_root = self._directory(owner_root / "skills")
            memory_root = self._directory(owner_root / "memory")
            user_skills = SkillLoader(skills_root)
            # Keep the session-visible problem view local to this owner.
            # Handing one mutable log to every layered loader would let Alice's
            # live view acquire Bob's rejected paths after Bob was resolved.
            skills = LayeredSkillLoader(self.agent_skills, user_skills)
            resources = UserResources(
                scope="user",
                skills=skills,
                memory=MemoryStore(memory_root, secrets=self.secrets),
                root=owner_root,
            )
            self._resources[owner] = resources
            return resources


RUNTIME_INVARIANT = "enforced by for_owner: each exact owner identifier maps to one cached immutable resource bundle rooted in a digest-only directory"
