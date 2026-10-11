"""Owner-bound local skill and memory resources.

Raw principal identifiers are authority, not filesystem names.  This module
maps an exact owner string to one digest-only directory and one immutable
resource bundle that can be shared by that owner's sessions.
"""

from __future__ import annotations

import errno
import hashlib
import re
import threading
from collections.abc import Mapping
from dataclasses import dataclass, field
from pathlib import Path
from typing import Literal

from .durable import atomic_create_text, read_bytes_no_follow
from .memory import MemoryStore
from .problems import ProblemLog
from .skills import (
    MAX_SKILL_BODY,
    MAX_SKILL_DESCRIPTION,
    LayeredSkillLoader,
    SkillLoader,
)

__all__ = [
    "UserResourceResolver",
    "UserResources",
    "UserSkillConflict",
    "UserSkillPublication",
    "UserSkillPublicationError",
    "UserSkillValidationError",
    "canonical_user_skill",
]

_USER_SKILL_NAME = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
_SKILL_WRAPPER = re.compile(r"<\s*/?\s*skill(?:\s|/?>)", re.IGNORECASE)
_USER_SKILL_FIELDS = frozenset(("name", "description", "body"))
_MAX_USER_SKILL_LINES = 500


class UserSkillPublicationError(Exception):
    """Safe publication failure whose text never includes host paths/content."""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code

    def as_dict(self) -> dict[str, str]:
        return {"code": self.code, "error": str(self)}


class UserSkillValidationError(UserSkillPublicationError, ValueError):
    """Caller-supplied skill fields are not safe canonical input."""


class UserSkillConflict(UserSkillPublicationError):
    """A create-only publication found an existing user skill name."""


def _validation(code: str, message: str) -> UserSkillValidationError:
    return UserSkillValidationError(code, message)


def _canonical_user_skill_parts(
    name: str,
    description: str,
    body: str,
) -> tuple[str, str, str]:
    if not isinstance(name, str) or not _USER_SKILL_NAME.fullmatch(name):
        raise _validation(
            "invalid_name",
            "Skill name must be lowercase kebab-case",
        )
    if len(name) > 64:
        raise _validation("invalid_name", "Skill name must be at most 64 characters")
    if not isinstance(description, str) or not description:
        raise _validation("invalid_description", "Skill description must be non-empty")
    if "\n" in description or "\r" in description:
        raise _validation("invalid_description", "Skill description must be one line")
    if len(description) > MAX_SKILL_DESCRIPTION:
        raise _validation(
            "invalid_description",
            f"Skill description must be at most {MAX_SKILL_DESCRIPTION} characters",
        )
    normalized_description = description.strip()
    if not normalized_description:
        raise _validation("invalid_description", "Skill description must be non-empty")
    if not isinstance(body, str) or not body.strip():
        raise _validation("invalid_body", "Skill body must be non-empty")
    if len(body) > MAX_SKILL_BODY:
        raise _validation(
            "invalid_body",
            f"Skill body must be at most {MAX_SKILL_BODY} characters",
        )
    if len(body.splitlines()) > _MAX_USER_SKILL_LINES:
        raise _validation(
            "invalid_body",
            f"Skill body must be at most {_MAX_USER_SKILL_LINES} lines",
        )
    if any("\x00" in value for value in (name, description, body)):
        raise _validation("unsafe_content", "Skill fields must not contain NUL")
    if _SKILL_WRAPPER.search(description) or _SKILL_WRAPPER.search(body):
        raise _validation(
            "unsafe_content",
            "Skill content must not contain a skill wrapper",
        )
    # Match Path.read_text's universal-newline behaviour and SkillLoader's
    # frontmatter/body stripping before constructing the live snapshot. This
    # keeps first publication, idempotent retry, and restart byte-equivalent.
    normalized_body = body.replace("\r\n", "\n").replace("\r", "\n").strip()
    canonical = (
        f"---\nname: {name}\ndescription: {normalized_description}\n---\n"
        f"{normalized_body}\n"
    )
    return canonical, normalized_description, normalized_body


def canonical_user_skill(name: str, description: str, body: str) -> str:
    """Validate user-authored fields and render one canonical ``SKILL.md``."""

    canonical, _description, _body = _canonical_user_skill_parts(
        name,
        description,
        body,
    )
    return canonical


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


@dataclass(frozen=True, slots=True)
class UserSkillPublication:
    """Safe receipt plus the new internal owner snapshot."""

    name: str
    digest: str
    content_digest: str
    resources: UserResources = field(repr=False, compare=False)
    activation: Literal["next_session"] = "next_session"
    collision_warning: str | None = None
    idempotent: bool = False

    @property
    def source(self) -> Literal["user"]:
        return "user"

    @property
    def warning(self) -> str | None:
        """Compatibility name used by the manager-facing publication API."""

        return self.collision_warning

    def as_dict(self) -> dict[str, str | bool | None]:
        return {
            "name": self.name,
            "digest": self.digest,
            "content_digest": self.content_digest,
            "activation": self.activation,
            "collision_warning": self.collision_warning,
            "idempotent": self.idempotent,
        }


class UserResourceResolver:
    """Resolve and cache digest-rooted resources for exact owner identifiers."""

    def __init__(
        self,
        root: Path,
        agent_skills: SkillLoader,
        secrets=None,
    ) -> None:
        self.root = Path(root).resolve()
        # Private like the spill root (round 171): these trees hold one
        # owner's memories and skills, and a default-mode directory reads as
        # world-readable to every other local account. chmod on reuse, so a
        # root created before this line existed is tightened, not trusted.
        self.root.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.root.chmod(0o700)
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
        path.mkdir(parents=True, exist_ok=True, mode=0o700)
        path.chmod(0o700)
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

    def publish_skill(
        self,
        owner: str,
        fields: Mapping[str, str],
    ) -> UserSkillPublication:
        """Create one user skill and activate it only for future resolutions."""

        if not isinstance(owner, str) or not owner:
            raise _validation("invalid_owner", "Owner must be a non-empty string")
        if not isinstance(fields, Mapping) or set(fields) != _USER_SKILL_FIELDS:
            raise _validation(
                "invalid_fields",
                "Skill fields must contain exactly name, description, and body",
            )
        name, description, body = (
            fields.get("name"),
            fields.get("description"),
            fields.get("body"),
        )
        canonical, normalized_description, normalized_body = (
            _canonical_user_skill_parts(name, description, body)
        )
        canonical_bytes = canonical.encode("utf-8")
        mask = getattr(self.secrets, "mask", None)
        if callable(mask):
            try:
                masked_name = mask(name)
                masked_description = mask(description)
                masked_body = mask(body)
            except Exception as error:
                raise UserSkillPublicationError(
                    "secret_check_failed",
                    "Skill secret screening failed",
                ) from error
            if (
                masked_name != name
                or masked_description != description
                or masked_body != body
            ):
                raise _validation(
                    "secret_detected",
                    "Skill content contains a registered secret",
                )

            # Masking deliberately ignores short values, and a lazy source may
            # fail to resolve. That is reasonable for general logs but not for
            # a new durable instruction file: if every registered value cannot
            # be screened, publication fails closed without naming the secret.
            try:
                names = tuple(getattr(self.secrets, "names", lambda: ())())
                unresolved_reader = getattr(self.secrets, "unresolved", None)
                short_reader = getattr(self.secrets, "short_values", None)
                if names and (
                    not callable(unresolved_reader) or not callable(short_reader)
                ):
                    raise RuntimeError("secret registry has no health surface")
                unresolved = tuple(unresolved_reader()) if names else ()
                short_values = tuple(short_reader()) if names else ()
            except Exception as error:
                raise UserSkillPublicationError(
                    "secret_check_failed",
                    "Skill secret screening is unavailable",
                ) from error
            if unresolved or short_values:
                raise UserSkillPublicationError(
                    "secret_check_failed",
                    "Skill secret screening is unavailable",
                )

        with self._lock:
            try:
                previous = self.for_owner(owner)
                skills_root = self._directory(previous.root / "skills")
                current = SkillLoader(skills_root)
                skill_root_path = skills_root / name
                target = skill_root_path / "SKILL.md"
                expected_digest = hashlib.sha256(
                    normalized_body.encode("utf-8")
                ).hexdigest()
                canonical_digest = hashlib.sha256(canonical_bytes).hexdigest()
                warning = None
                if name in self.agent_skills.skills:
                    warning = (
                        "An agent-provided skill has the same name; use an explicit "
                        "agent: or user: source"
                    )

                def publication_for(
                    loader: SkillLoader,
                    *,
                    idempotent: bool,
                ) -> UserSkillPublication:
                    refreshed = UserResources(
                        scope="user",
                        skills=LayeredSkillLoader(self.agent_skills, loader),
                        memory=previous.memory,
                        root=previous.root,
                    )
                    return UserSkillPublication(
                        name=name,
                        digest=canonical_digest,
                        content_digest=expected_digest,
                        resources=refreshed,
                        collision_warning=warning,
                        idempotent=idempotent,
                    )

                if name in current.skills:
                    if not self._active_skill_matches(
                        current,
                        name=name,
                        target=target,
                        expected_digest=expected_digest,
                        canonical_digest=canonical_digest,
                        max_bytes=len(canonical_bytes),
                    ):
                        raise UserSkillConflict(
                            "user_skill_exists",
                            "A user skill with this name already exists",
                        )
                    publication = publication_for(current, idempotent=True)
                    self._resources[owner] = publication.resources
                    return publication

                skill_root = self._directory(skill_root_path)
                target = skill_root / "SKILL.md"
                current.skills[name] = {
                    "meta": {
                        "name": name,
                        "description": normalized_description,
                    },
                    "body": normalized_body,
                    "path": str(target),
                    "digest": expected_digest,
                }
                current.skills = dict(
                    sorted(
                        current.skills.items(),
                        key=lambda item: Path(item[1]["path"]),
                    )
                )
                # Everything that may allocate or validate is complete before
                # the hard link. Once that link succeeds it is the irreversible
                # commit point; no rollback may revoke another resolver's
                # already-confirmed idempotent success.
                publication = publication_for(current, idempotent=False)
            except UserSkillPublicationError:
                raise
            except RuntimeError as error:
                raise UserSkillPublicationError(
                    "unsafe_path",
                    "User skill path is unsafe",
                ) from error
            except OSError as error:
                code = (
                    "unsafe_path"
                    if error.errno in (errno.ELOOP, errno.ENOTDIR)
                    else "publish_failed"
                )
                message = (
                    "User skill path is unsafe"
                    if code == "unsafe_path"
                    else "User skill could not be published"
                )
                raise UserSkillPublicationError(code, message) from error
            except Exception as error:
                raise UserSkillPublicationError(
                    "publish_failed",
                    "User skill could not be published",
                ) from error

            try:
                atomic_create_text(target, canonical)
            except FileExistsError:
                try:
                    raced = SkillLoader(skills_root)
                    if not self._active_skill_matches(
                        raced,
                        name=name,
                        target=target,
                        expected_digest=expected_digest,
                        canonical_digest=canonical_digest,
                        max_bytes=len(canonical_bytes),
                    ):
                        raise UserSkillConflict(
                            "user_skill_exists",
                            "A user skill with this name already exists",
                        )
                    publication = publication_for(raced, idempotent=True)
                except UserSkillPublicationError:
                    raise
                except Exception as error:
                    raise UserSkillPublicationError(
                        "publish_failed",
                        "User skill could not be published",
                    ) from error
            except OSError as error:
                code = (
                    "unsafe_path"
                    if error.errno in (errno.ELOOP, errno.ENOTDIR)
                    else "publish_failed"
                )
                message = (
                    "User skill path is unsafe"
                    if code == "unsafe_path"
                    else "User skill could not be published"
                )
                raise UserSkillPublicationError(code, message) from error

            # No fallible operation follows a successful create. A crash before
            # the response is retried through the idempotent branch above.
            self._resources[owner] = publication.resources
            return publication

    @staticmethod
    def _active_skill_matches(
        loader: SkillLoader,
        *,
        name: str,
        target: Path,
        expected_digest: str,
        canonical_digest: str,
        max_bytes: int,
    ) -> bool:
        """Whether the active entry is the exact canonical retry target."""

        active = loader.skills.get(name)
        try:
            payload = read_bytes_no_follow(target, max_bytes=max_bytes)
            existing_digest = hashlib.sha256(payload).hexdigest()
        except OverflowError:
            return False
        except OSError as error:
            if error.errno in (errno.ELOOP, errno.ENOTDIR):
                raise UserSkillPublicationError(
                    "unsafe_path",
                    "User skill path is unsafe",
                ) from error
            if error.errno == errno.ENOENT:
                return False
            raise UserSkillPublicationError(
                "publish_failed",
                "Existing user skill could not be verified",
            ) from error
        if active is None or Path(active["path"]) != target:
            return False
        return (
            existing_digest == canonical_digest
            and active.get("digest") == expected_digest
        )


RUNTIME_INVARIANT = (
    "enforced by for_owner and publish_skill: each exact owner maps to one "
    "current cached immutable bundle, while create-only publication atomically "
    "replaces only the resolver cache and never an existing skill file"
)
