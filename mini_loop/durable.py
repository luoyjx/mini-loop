"""One way to put bytes on disk so that a crash cannot lose what was there.

Round 81 taught the stores to *tolerate* a file they cannot read. This is the
other half: not producing one. `Path.write_text` opens with `"w"`, which
truncates before writing anything, so the window between truncate and write is
one where the file is empty -- and a process that dies in it has not corrupted
the old content, it has destroyed it:

    MEMORY.md before : "# Memory index\\n- [important](a.md) - hard-won knowledge"
    crash mid-write
    MEMORY.md after  : "# Memory index\\n"

For a store whose entire purpose is to outlive the session that is the worst
available outcome. A memory that reads as garbage is at least *evidence* that
something was there; an empty index is indistinguishable from never having
learned anything.

Two of the four durable writers already did temp-and-rename, and they disagreed
in ways worth collapsing into one place rather than fixing four times:

    tasks.py   unique temp name (uuid), no fsync
    cron.py    fixed ".tmp" name,        no fsync
    memory.py  no temp at all       (x2)
    compaction no temp at all

A fixed temp name means two writers -- two threads, or two harness processes
sharing a cron file -- use the same scratch path and one renames the other's
half-written bytes into place. And `replace` is only atomic with respect to the
*rename*: without flushing the temp file first, a power loss can order the
rename ahead of the data and leave a valid-looking empty file at the target,
which is the failure this module exists to prevent.

So: unique temp beside the target, flush and fsync it, then rename. The
create-only variant publishes that flushed inode with a hard link: linking a
new name is atomic, and unlike ``replace`` it fails when the target already
exists. The directory fsync that makes either publication durable is
best-effort -- it is not available on every platform, and failing to harden the
directory entry is not a reason to report a committed write as failed.
"""

from __future__ import annotations

import os
import stat
import uuid
from pathlib import Path

__all__ = [
    "atomic_create_bytes",
    "atomic_create_text",
    "atomic_write_bytes",
    "atomic_write_text",
    "read_bytes_no_follow",
]


def atomic_create_text(
    path: Path,
    text: str,
    *,
    encoding: str = "utf-8",
) -> tuple[int, int]:
    """Create ``path`` atomically and return its ``(device, inode)`` identity.

    ``FileExistsError`` is raised without replacing the existing entry.
    """

    return atomic_create_bytes(Path(path), text.encode(encoding))


def atomic_create_bytes(path: Path, payload: bytes) -> tuple[int, int]:
    """Publish new bytes without ever replacing an existing directory entry.

    The parent must already exist.  It is opened component by component with
    ``O_NOFOLLOW`` so a symlink swap cannot redirect the final create.  Once
    the hard link succeeds the target is committed; scratch cleanup is then
    best-effort so a harmless leftover alias cannot turn success into an
    ambiguous reported failure. The returned ``(device, inode)`` pair names
    the committed object for callers that need an audit receipt.
    """

    path = Path(path)
    parent_fd = _open_directory_no_follow(path.parent)
    temporary = f".{path.name}.{uuid.uuid4().hex}.tmp"
    file_fd: int | None = None
    published = False
    identity: tuple[int, int] | None = None
    try:
        flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL
        if hasattr(os, "O_NOFOLLOW"):
            flags |= os.O_NOFOLLOW
        file_fd = os.open(temporary, flags, 0o600, dir_fd=parent_fd)
        os.fchmod(file_fd, 0o600)
        with os.fdopen(file_fd, "wb") as handle:
            file_fd = None
            handle.write(payload)
            handle.flush()
            os.fsync(handle.fileno())
            metadata = os.fstat(handle.fileno())
            identity = (metadata.st_dev, metadata.st_ino)
        os.link(
            temporary,
            path.name,
            src_dir_fd=parent_fd,
            dst_dir_fd=parent_fd,
            follow_symlinks=False,
        )
        published = True
    finally:
        if file_fd is not None:
            os.close(file_fd)
        try:
            os.unlink(temporary, dir_fd=parent_fd)
        except FileNotFoundError:
            pass
        except OSError:
            pass
        if published:
            _fsync_fd(parent_fd)
        # The hard link is the commit point. A close failure must never turn a
        # visible, durable file into an error that invites a second publish.
        try:
            os.close(parent_fd)
        except OSError:
            pass
    assert identity is not None
    return identity


def read_bytes_no_follow(path: Path, *, max_bytes: int) -> bytes:
    """Read one regular file through an anchored, no-symlink path.

    ``max_bytes`` bounds both the returned value and the work performed. One
    extra byte distinguishes an exact-bound file from an oversized one.
    """

    if (
        isinstance(max_bytes, bool)
        or not isinstance(max_bytes, int)
        or max_bytes < 0
    ):
        raise ValueError("max_bytes must be a non-negative integer")
    path = Path(path)
    parent_fd = _open_directory_no_follow(path.parent)
    file_fd: int | None = None
    try:
        flags = os.O_RDONLY
        if hasattr(os, "O_NOFOLLOW"):
            flags |= os.O_NOFOLLOW
        file_fd = os.open(path.name, flags, dir_fd=parent_fd)
        metadata = os.fstat(file_fd)
        if not stat.S_ISREG(metadata.st_mode):
            raise OSError("secure read requires a regular file")
        with os.fdopen(file_fd, "rb") as handle:
            file_fd = None
            payload = handle.read(max_bytes + 1)
        if len(payload) > max_bytes:
            raise OverflowError("secure read exceeds the configured bound")
        return payload
    finally:
        if file_fd is not None:
            try:
                os.close(file_fd)
            except OSError:
                pass
        try:
            os.close(parent_fd)
        except OSError:
            pass


def atomic_write_text(path: Path, text: str, *, encoding: str = "utf-8") -> None:
    """Replace `path` with `text`, or leave it exactly as it was."""

    atomic_write_bytes(Path(path), text.encode(encoding))


def atomic_write_bytes(path: Path, payload: bytes) -> None:
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    # Beside the target, so the rename stays within one filesystem -- across a
    # mount boundary `replace` is a copy and stops being atomic. Unique, so
    # concurrent writers do not share a scratch file.
    temporary = path.with_name(f".{path.name}.{uuid.uuid4().hex}.tmp")
    try:
        with open(temporary, "wb") as handle:
            handle.write(payload)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
    except BaseException:
        # Including KeyboardInterrupt and SystemExit: an interrupted write must
        # not leave scratch files behind for a later glob to pick up. The target
        # is untouched either way -- that is the point of writing beside it.
        temporary.unlink(missing_ok=True)
        raise
    _fsync_directory(path.parent)


def _fsync_directory(directory: Path) -> None:
    """Make the rename itself durable, where the platform allows it."""

    try:
        fd = os.open(directory, os.O_RDONLY)
    except OSError:
        return
    try:
        os.fsync(fd)
    except OSError:
        pass
    finally:
        os.close(fd)


def _fsync_fd(fd: int) -> None:
    """Best-effort directory-entry durability for an already anchored fd."""

    try:
        os.fsync(fd)
    except OSError:
        pass


def _open_directory_no_follow(directory: Path) -> int:
    """Open an absolute directory path without following any component link."""

    required_dir_fd = (os.open, os.link, os.unlink)
    if (
        not hasattr(os, "O_NOFOLLOW")
        or not hasattr(os, "O_DIRECTORY")
        or any(function not in os.supports_dir_fd for function in required_dir_fd)
    ):
        raise OSError("secure create requires dir_fd and O_NOFOLLOW support")
    absolute = directory if directory.is_absolute() else Path.cwd() / directory
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    current = os.open(absolute.anchor or os.sep, flags)
    try:
        for component in absolute.parts[1:]:
            following = os.open(component, flags, dir_fd=current)
            os.close(current)
            current = following
        return current
    except BaseException:
        os.close(current)
        raise

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: atomic writes publish with replace, atomic creates "
    "publish with a no-overwrite hard link after flushing a private temporary "
    "inode, and secure reads anchor every path component with O_NOFOLLOW."
)
