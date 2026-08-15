"""Spill storage: an oversized tool output is preserved, not destroyed.

Rounds 140-169 put a cap on every path that produces tool output, and every
cap was implemented the same way: keep a head/tail preview, drop the middle.
The context budget was protected at the price of the data -- the model was
told "output truncated" with no way to recover what the command actually
printed, and a command's output cannot be re-produced by running it again
(it already ran; it had side effects).

DeepSeek Harness separates the two concerns into a seam (`ctx.spillStore`):
the *cap* stays where it is, but the full text is persisted verbatim first,
and the preview carries a locator plus retrieval guidance. Three details of
their contract are load-bearing and kept here:

* the backend chooses a **private** location (0700 directory, owner-only
  files) and a **collision-free random name** derived from -- never equal
  to -- the caller's suggestion, opened with `O_EXCL` so a planted symlink
  at a guessable path cannot redirect the write;
* the locator is **opaque to the consumer** and rendered together with the
  backend's own `retrieval_hint`, because only the backend knows whether
  the artifact is a local file, a URI, or a database key;
* preservation is **best-effort at the policy layer**: a failed save keeps
  the plain truncated preview and never turns a successful tool call into
  an error.

The store accepts already-masked content only, by the same contract as
`MaskedRawArtifactStore` (token_efficiency.py): callers mask before saving,
so a spill file can never out-live a secret rotation as a plaintext copy.
"""

from __future__ import annotations

import hashlib
import os
import re
import secrets as _secrets
from dataclasses import dataclass
from pathlib import Path
from typing import Protocol, runtime_checkable

__all__ = ["SpillRef", "SpillStore", "LocalSpillStore"]

#: Largest single artifact a local store accepts, so one runaway output
#: cannot fill the disk through the preservation path that exists to be
#: safe. Matches the bash capture ceiling: nothing bigger can reach us.
MAX_SPILL_BYTES = 8_000_000

_SAFE_NAME = re.compile(r"[^A-Za-z0-9._-]+")


@dataclass(frozen=True, slots=True)
class SpillRef:
    """One preserved artifact: locator, exact size, and how to read it back.

    `locator` is opaque -- the local backend renders a filesystem path, but a
    remote backend may render a URI or key, so consumers show it together
    with `retrieval_hint` instead of assuming any one retrieval mechanism.
    """

    locator: str
    bytes: int
    retrieval_hint: str


@runtime_checkable
class SpillStore(Protocol):
    """Persist one oversized text verbatim; return where it went."""

    def save_text(
        self,
        *,
        session_id: str,
        tool_name: str,
        label: str,
        suggested_name: str,
        content: str,
    ) -> SpillRef:
        """Persist `content` and return its `SpillRef`.

        Raises on a real storage failure (permissions, disk full, artifact
        over the backend's limit); the caller decides how to degrade.
        `session_id` is a storage namespace, not access control.
        `suggested_name` is a naming hint, never a path.
        """
        ...


class LocalSpillStore:
    """Session-scoped spill files on the host filesystem.

    Layout: `<root>/session-<sha256(session_id)[:16]>/<random>-<safe-name>`.
    The root and each session directory are private (0700); every artifact
    is created exclusively (`O_EXCL`) with owner-only permissions (0600), so
    a predictable-path symlink race cannot redirect the write and another
    local user cannot read the content.
    """

    def __init__(self, root: Path) -> None:
        self.root = Path(root).resolve()
        self.root.mkdir(parents=True, exist_ok=True)
        # An inherited pre-existing root may be world-readable; tighten it
        # rather than trusting whoever created it.
        os.chmod(self.root, 0o700)

    @staticmethod
    def _safe_name(suggested: str) -> str:
        # A hint, never a path: collapse everything path-like, refuse hidden
        # files and `..`, and bound the length so a hostile suggestion cannot
        # exceed filename limits.
        name = _SAFE_NAME.sub("_", suggested)
        # `..` never survives, even embedded: the name is one path segment by
        # construction, but a filename containing ".." invites misreading in
        # logs and by review tools that scan for traversal.
        name = re.sub(r"\.{2,}", ".", name).lstrip(".")[:80]
        return name or "spill.txt"

    def save_text(
        self,
        *,
        session_id: str,
        tool_name: str,
        label: str,
        suggested_name: str,
        content: str,
    ) -> SpillRef:
        del tool_name, label  # descriptive; the filename hint carries enough
        data = content.encode("utf-8")
        if len(data) > MAX_SPILL_BYTES:
            raise ValueError(
                f"spill artifact is {len(data):,} bytes; limit {MAX_SPILL_BYTES:,}"
            )
        digest = hashlib.sha256(session_id.encode("utf-8")).hexdigest()[:16]
        session_dir = self.root / f"session-{digest}"
        session_dir.mkdir(mode=0o700, exist_ok=True)
        path = session_dir / f"{_secrets.token_hex(8)}-{self._safe_name(suggested_name)}"
        # Exclusive create: fails on anything already at the path, including
        # a symlink planted there, instead of following it.
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        try:
            os.write(fd, data)
            os.fsync(fd)
        finally:
            os.close(fd)
        return SpillRef(
            locator=str(path),
            bytes=len(data),
            retrieval_hint=(
                "outside the workspace; read it with bash, e.g. "
                f"sed -n '1,200p' {path} or grep <pattern> {path}"
            ),
        )

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: storage privacy is established at create time (0700/0600/O_EXCL) and pinned by permission-bit tests; later observation could not undo a wrong create."
)
