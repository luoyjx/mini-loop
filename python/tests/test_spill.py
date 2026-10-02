"""Truncation preserves data instead of destroying it -- the spill seam.

Rounds 140-169 capped every output path by *dropping* the overflow: the model
was told "output truncated" with no way to recover what a command actually
printed, and a command cannot be re-run to reproduce it (it already ran; it
had side effects). The spill store (mini_loop/spill.py, after DeepSeek
Harness's `ctx.spillStore`) persists the full masked text first, so the
preview carries a locator plus the backend's retrieval hint.

The contract pinned here:
* the full text survives, byte-exact, at the locator;
* storage is private -- 0700 directories, 0600 exclusive-create files,
  random names a hostile suggestion cannot control;
* preservation is best-effort -- a failing store keeps the plain preview
  and never turns a successful tool call into an error;
* an output within the cap saves nothing at all.
"""

import os
import stat
from pathlib import Path

import pytest

from mini_loop.spill import LocalSpillStore, MAX_SPILL_BYTES, SpillRef
from mini_loop.tools import OUTPUT_CAP, Toolset


def _store(tmp_path: Path) -> LocalSpillStore:
    return LocalSpillStore(tmp_path / "spill")


def test_the_full_text_survives_byte_exact(tmp_path):
    store = _store(tmp_path)
    content = "x" * (OUTPUT_CAP + 5_000) + "\nthe very end"
    ref = store.save_text(
        session_id="s1", tool_name="bash", label="output",
        suggested_name="bash.txt", content=content,
    )
    assert Path(ref.locator).read_text() == content
    assert ref.bytes == len(content.encode("utf-8"))
    assert ref.retrieval_hint


def test_storage_is_private(tmp_path):
    store = _store(tmp_path)
    ref = store.save_text(
        session_id="s1", tool_name="bash", label="output",
        suggested_name="bash.txt", content="secret-ish output",
    )
    path = Path(ref.locator)
    assert stat.S_IMODE(os.stat(store.root).st_mode) == 0o700
    assert stat.S_IMODE(os.stat(path.parent).st_mode) == 0o700
    assert stat.S_IMODE(os.stat(path).st_mode) == 0o600


def test_a_hostile_suggested_name_cannot_choose_the_path(tmp_path):
    store = _store(tmp_path)
    ref = store.save_text(
        session_id="s1", tool_name="bash", label="output",
        suggested_name="../../../../etc/passwd", content="body",
    )
    path = Path(ref.locator)
    # Still inside the session directory, no traversal, not a hidden file.
    assert path.parent.parent == store.root
    assert not path.name.startswith(".")
    assert "/" not in path.name and ".." not in path.name


def test_two_saves_never_collide(tmp_path):
    store = _store(tmp_path)
    refs = {
        store.save_text(
            session_id="s1", tool_name="bash", label="output",
            suggested_name="bash.txt", content=f"body {i}",
        ).locator
        for i in range(5)
    }
    assert len(refs) == 5


def test_a_planted_symlink_cannot_redirect_the_write(tmp_path, monkeypatch):
    """`O_EXCL` refuses anything already at the path -- including a symlink.

    Random names make the path unguessable in practice; this pins the second,
    independent layer: even a correctly *guessed* path cannot redirect the
    write, because exclusive create fails on an existing entry instead of
    following it into its target.
    """

    import hashlib

    store = _store(tmp_path)
    victim = tmp_path / "victim.txt"
    victim.write_text("original")
    monkeypatch.setattr(
        "mini_loop.spill._secrets.token_hex", lambda n=8: "feedfacedeadbeef"
    )
    digest = hashlib.sha256(b"s1").hexdigest()[:16]
    session_dir = store.root / f"session-{digest}"
    session_dir.mkdir(mode=0o700, exist_ok=True)
    (session_dir / "feedfacedeadbeef-bash.txt").symlink_to(victim)
    with pytest.raises(FileExistsError):
        store.save_text(
            session_id="s1", tool_name="bash", label="output",
            suggested_name="bash.txt", content="attacker text",
        )
    assert victim.read_text() == "original"


def test_an_artifact_over_the_backend_limit_is_refused(tmp_path):
    store = _store(tmp_path)
    with pytest.raises(ValueError):
        store.save_text(
            session_id="s1", tool_name="bash", label="output",
            suggested_name="bash.txt", content="y" * (MAX_SPILL_BYTES + 1),
        )


# --- the Toolset policy layer ---------------------------------------------


def test_oversized_bash_output_carries_a_locator(tmp_path):
    toolset = Toolset(tmp_path / "ws", spill=_store(tmp_path))
    out = toolset.run_bash(
        f"python3 -c \"print('A' * {OUTPUT_CAP + 10_000})\""
    )
    assert "[truncated:" in out
    assert "full output preserved:" in out
    locator = out.split("full output preserved: ")[1].split(" (")[0]
    preserved = Path(locator).read_text()
    assert preserved == "A" * (OUTPUT_CAP + 10_000)


def test_output_within_the_cap_spills_nothing(tmp_path):
    store = _store(tmp_path)
    toolset = Toolset(tmp_path / "ws", spill=store)
    out = toolset.run_bash("echo small")
    assert "full output preserved" not in out
    assert not any(store.root.iterdir())


def test_without_a_store_truncation_behaves_as_before(tmp_path):
    toolset = Toolset(tmp_path / "ws")
    out = toolset.run_bash(
        f"python3 -c \"print('A' * {OUTPUT_CAP + 10_000})\""
    )
    assert "[truncated:" in out
    assert "full output preserved" not in out


def test_a_failing_store_keeps_the_preview(tmp_path):
    class ExplodingStore:
        def save_text(self, **kwargs):
            raise OSError("disk full")

    command = f"python3 -c \"print('A' * {OUTPUT_CAP + 10_000})\""
    out = Toolset(tmp_path / "ws", spill=ExplodingStore()).run_bash(command)
    # Best-effort means byte-identical to a toolset with no store at all:
    # the preview intact, no error text, no note of any kind appended.
    assert out == Toolset(tmp_path / "ws2").run_bash(command)
    assert "[truncated:" in out


def test_the_spilled_copy_is_the_masked_projection(tmp_path):
    """What reaches disk must be what the model saw: masked, never raw."""

    from mini_loop.secrets import SecretRegistry

    registry = SecretRegistry()
    registry.register("API_KEY", "hunter2-hunter2-hunter2")
    store = _store(tmp_path)
    toolset = Toolset(tmp_path / "ws", spill=store, secrets=registry)
    filler = "B" * (OUTPUT_CAP + 10_000)
    out = toolset.run_bash(
        f"echo 'key=hunter2-hunter2-hunter2'; python3 -c \"print('{filler[:0]}' + 'B' * {OUTPUT_CAP + 10_000})\""
    )
    assert "hunter2" not in out
    files = [p for p in store.root.rglob("*") if p.is_file()]
    assert files, "an over-cap output should have been preserved"
    for f in files:
        assert "hunter2" not in f.read_text()


def test_the_harness_carries_the_spill_seam():
    from mini_loop.harness import Harness

    marker = object()
    harness = Harness(spill=marker)
    assert harness.derive().spill is marker
    assert harness.resolve("spill", None) is marker
