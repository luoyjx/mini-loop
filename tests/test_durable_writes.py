"""A durable store must not lose what it already had.

Round 81 made the stores tolerate a file they cannot read. This is the other
half: not producing one. `Path.write_text` opens with `"w"`, which truncates
before writing, so a process that dies in that window has not corrupted the old
content -- it has destroyed it. For `MEMORY.md` that turns "the agent knows
twelve things" into "the agent has never learned anything", and the two are
indistinguishable afterwards.

The four durable writers disagreed four ways -- uuid temp with no fsync, fixed
temp with no fsync, and two with no temp at all -- so this pins the helper they
now share rather than each site, and the AST guard below is what keeps the fifth
writer from inventing a fifth way.
"""

import ast
import os
import pathlib
import threading

import pytest

from mini_loop.durable import atomic_write_text

PACKAGE = pathlib.Path(__file__).resolve().parent.parent / "mini_loop"

#: Modules that keep state meant to outlive the turn that wrote it.
DURABLE_MODULES = ("memory.py", "tasks.py", "cron.py", "compaction.py")


def _scratch(directory):
    return sorted(p.name for p in directory.iterdir() if ".tmp" in p.name)


# -- the property ---------------------------------------------------------

def test_a_failed_write_leaves_the_old_content(tmp_path, monkeypatch):
    target = tmp_path / "MEMORY.md"
    target.write_text("- [important](a.md) — hard-won knowledge\n")

    def explode(*args, **kwargs):
        raise OSError("disk full")

    monkeypatch.setattr(os, "replace", explode)
    with pytest.raises(OSError):
        atomic_write_text(target, "# Memory index\n")

    assert "hard-won knowledge" in target.read_text()
    assert not _scratch(tmp_path)


def test_an_interrupt_leaves_no_scratch_file(tmp_path, monkeypatch):
    """`except Exception` would let Ctrl-C strand a `.tmp` for a later glob."""

    target = tmp_path / "cron.json"
    target.write_text("[]")

    def interrupt(*args, **kwargs):
        raise KeyboardInterrupt

    monkeypatch.setattr(os, "replace", interrupt)
    with pytest.raises(KeyboardInterrupt):
        atomic_write_text(target, "[{}]")

    assert target.read_text() == "[]"
    assert not _scratch(tmp_path)


def test_concurrent_writers_do_not_share_a_scratch_path(tmp_path):
    """A fixed `.tmp` name means one writer renames another's half-written bytes."""

    target = tmp_path / "shared.json"
    payloads = [f'{{"writer": {i}, "pad": "{"x" * 4000}"}}' for i in range(8)]
    seen, errors = [], []

    def hammer(text):
        for _ in range(40):
            try:
                atomic_write_text(target, text)
                seen.append(target.read_text())
            except Exception as exc:                     # pragma: no cover
                errors.append(f"{type(exc).__name__}: {exc}")

    threads = [threading.Thread(target=hammer, args=(p,)) for p in payloads]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    assert not errors
    # Every observed state is one writer's complete payload, never a splice.
    assert set(seen) <= set(payloads)
    assert not _scratch(tmp_path)


def test_the_write_is_actually_durable_not_just_renamed(tmp_path, monkeypatch):
    """Without an fsync of the temp file, the rename can outlive the data.

    Asserting only that `os.fsync` ran is vacuous: this helper also fsyncs the
    *directory*, so dropping the file flush leaves the call count non-zero and
    the test green. The mutation runner caught exactly that. What matters is
    the ordering -- data flushed *before* the rename that publishes it.
    """

    order = []
    real_fsync, real_replace = os.fsync, os.replace
    monkeypatch.setattr(os, "fsync", lambda fd: (order.append("fsync"), real_fsync(fd))[1])
    monkeypatch.setattr(
        os, "replace", lambda *a: (order.append("replace"), real_replace(*a))[1]
    )

    atomic_write_text(tmp_path / "x.json", "{}")

    assert "replace" in order
    assert order.index("fsync") < order.index("replace"), (
        f"the rename published data that was never flushed: {order}"
    )


def test_a_missing_parent_is_created(tmp_path):
    atomic_write_text(tmp_path / "deep" / "nested" / "f.txt", "ok")
    assert (tmp_path / "deep" / "nested" / "f.txt").read_text() == "ok"


# -- the guard ------------------------------------------------------------

def _direct_writes(path):
    """`write_text` / `write_bytes` calls in `path`, by line."""

    tree = ast.parse(path.read_text())
    for node in ast.walk(tree):
        if (
            isinstance(node, ast.Call)
            and isinstance(node.func, ast.Attribute)
            and node.func.attr in ("write_text", "write_bytes")
        ):
            yield node.lineno, ast.unparse(node)[:70]


def _helper_writes(path):
    """`atomic_write_text` / `atomic_write_bytes` calls in `path`, by line."""

    tree = ast.parse(path.read_text())
    for node in ast.walk(tree):
        if (
            isinstance(node, ast.Call)
            and isinstance(node.func, ast.Name)
            and node.func.id in ("atomic_write_text", "atomic_write_bytes")
        ):
            yield node.lineno, ast.unparse(node)[:70]


def test_the_direct_write_matcher_still_matches(tmp_path):
    """The negative guard below (`no durable module uses a raw write`) is only
    as good as `_direct_writes`. Round 163 converted the last raw writers in the
    package, so there is deliberately nothing left in the source for it to find
    -- the old "it finds one somewhere" anchor now asserts the opposite of the
    goal. Anchor the matcher on a synthetic sample instead, so a matcher that
    silently stopped recognising writes still gets caught."""

    sample = tmp_path / "sample.py"
    sample.write_text(
        "from pathlib import Path\n"
        "Path('x').write_text('y')\n"
        "Path('z').write_bytes(b'w')\n"
    )
    found = [source for _lineno, source in _direct_writes(sample)]
    assert len(found) == 2, found
    assert any("write_text" in f for f in found)
    assert any("write_bytes" in f for f in found)


def test_the_package_writes_through_the_helper_somewhere():
    """The positive anchor, and the one `verify_scans` empties: a package that
    had quietly regressed to raw writes everywhere would leave this at zero.
    Emptying `PACKAGE` drives it to zero and this must fail -- that is what keeps
    the module a load-bearing scan now that the raw-write count is, by design,
    zero."""

    total = sum(len(list(_helper_writes(p))) for p in PACKAGE.rglob("*.py"))
    assert total, "no atomic_write_text call anywhere -- the helper went unused"


def test_durable_modules_write_through_the_helper():
    offenders = []
    for name in DURABLE_MODULES:
        for lineno, source in _direct_writes(PACKAGE / name):
            offenders.append(f"{name}:{lineno}  {source}")
    assert not offenders, (
        "these write durable state without atomic_write_text, so a crash "
        "mid-write destroys what was there:\n  " + "\n  ".join(offenders)
    )


def test_the_helper_is_the_only_thing_that_renames_into_place():
    """A store doing its own temp-and-rename is a fifth way to get it wrong."""

    offenders = []
    for name in DURABLE_MODULES:
        source = (PACKAGE / name).read_text()
        for marker in (".replace(", "os.rename", "os.replace"):
            if marker in source and "atomic_write_text" not in marker:
                for lineno, line in enumerate(source.splitlines(), 1):
                    if marker in line and "str" not in line and "text.replace" not in line:
                        offenders.append(f"{name}:{lineno}  {line.strip()[:60]}")
    assert not offenders, "hand-rolled rename outside the helper:\n  " + "\n  ".join(offenders)
