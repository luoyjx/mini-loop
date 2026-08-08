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
directory fsync that makes the rename itself durable is best-effort -- it is not
available on every platform, and failing to harden the rename is not a reason to
fail the write.
"""

from __future__ import annotations

import os
import uuid
from pathlib import Path

__all__ = ["atomic_write_text", "atomic_write_bytes"]


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
