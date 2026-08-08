"""Silent truncation threw away exactly the part worth reading.

Round 62 asked what an agent does not know that would change what it does.
Confinement was one answer; this is the next. `run_bash` and `read_file` cut
output at exactly 50,000 characters and said nothing, so the agent received
something ending mid-stream with no way to tell it was cut -- it reasons about
"the end" of a file it never saw, or concludes a search found no more matches.

Two of the three sites had already thought about truncation (`glob` says
"matches truncated", `read_file` says "N more lines") and then applied a blanket
`[:OUTPUT_CAP]` underneath that silently truncated again. Round 60 gave MCP
results a truncation notice; the built-in tools, used far more, never got one.

The sharper half is *which* part survived. For command output the important
content is at the end -- a test summary, a stack trace, an exit status -- and
keeping the head discarded precisely that:

    $ ...40,000 lines...; echo 'FAILED: 3 tests'; exit 1
    before: 50,000 chars of LINE, no notice, no summary
    after :  the head, the tail, a notice, and 'FAILED: 3 tests' intact
"""

import pathlib

import pytest

from mini_loop.tools import OUTPUT_CAP, Toolset, capped


@pytest.fixture
def toolset(tmp_path):
    workspace = tmp_path / "ws"
    workspace.mkdir()
    return Toolset(workspace)


# --- the helper -----------------------------------------------------------

def test_short_output_is_untouched():
    assert capped("hello") == "hello"
    assert capped("hello", keep_tail=True) == "hello"


def test_truncation_says_the_original_size():
    result = capped("X" * 200_000)
    assert "truncated" in result
    assert "200,000" in result
    assert len(result) <= OUTPUT_CAP


def test_keeping_the_tail_keeps_both_ends():
    text = "HEAD" + "x" * 200_000 + "TAIL"
    result = capped(text, keep_tail=True)
    assert result.startswith("HEAD")
    assert "TAIL" in result
    assert "omitted from the middle" in result
    assert len(result) <= OUTPUT_CAP


def test_the_head_only_form_keeps_the_head():
    """A file is read from the top and `read_file` takes an offset for the rest."""
    text = "HEAD" + "x" * 200_000 + "TAIL"
    result = capped(text)
    assert result.startswith("HEAD")
    assert "TAIL" not in result


@pytest.mark.parametrize("keep_tail", [False, True])
def test_the_cap_is_never_exceeded(keep_tail):
    for size in (OUTPUT_CAP - 1, OUTPUT_CAP, OUTPUT_CAP + 1, 5_000_000):
        assert len(capped("y" * size, keep_tail=keep_tail)) <= OUTPUT_CAP


# --- the tools ------------------------------------------------------------

def test_a_large_file_read_says_it_was_cut(toolset):
    (toolset.workspace / "big.txt").write_text("LINE\n" * 40_000)
    result = toolset.run_read("big.txt")
    assert "truncated" in result
    assert len(result) <= OUTPUT_CAP


def test_a_read_does_not_load_a_huge_file_into_memory(toolset):
    """The output was capped, but `read_text()` loaded the whole file first --
    so a model that wrote a huge file (shell output is capped, a file it writes
    is not) and read it would OOM the process, every tenant on it with it. The
    read is bounded to READ_CHAR_CAP, so peak memory tracks the cap, not the file.
    """
    import tracemalloc

    from mini_loop.tools import READ_CHAR_CAP

    file_chars = READ_CHAR_CAP * 15  # 15x the cap, as one line (defeats line reads)
    (toolset.workspace / "huge.txt").write_text("A" * file_chars)

    tracemalloc.start()
    result = toolset.run_read("huge.txt")
    _current, peak = tracemalloc.get_traced_memory()
    tracemalloc.stop()

    assert len(result) <= OUTPUT_CAP + 200
    assert peak < file_chars // 2, (
        f"the read loaded {peak:,} bytes of a {file_chars:,}-char file into memory"
    )
    assert peak < READ_CHAR_CAP * 6, "the read scaled with file size, not the cap"


def test_an_edit_refuses_a_huge_file_instead_of_loading_it(toolset):
    """`read_file` bounds its read (round 140), but `edit_file` needs the whole
    file to replace within it -- it cannot truncate, so reading a huge one whole
    would OOM the process, the same agent-reachable danger a different way. It
    refuses instead, leaving the file untouched; a normal edit is unaffected.
    """
    import tracemalloc

    from mini_loop.tools import READ_CHAR_CAP

    file_chars = READ_CHAR_CAP * 15
    (toolset.workspace / "huge.txt").write_text("A" * file_chars)

    tracemalloc.start()
    result = toolset.run_edit("huge.txt", "A", "B")
    _current, peak = tracemalloc.get_traced_memory()
    tracemalloc.stop()

    assert "too large to edit" in result
    assert peak < file_chars // 4, "the edit loaded the file it refused to edit"
    assert (toolset.workspace / "huge.txt").stat().st_size == file_chars, (
        "the refused file was modified"
    )

    # A normal-sized edit is unaffected.
    (toolset.workspace / "small.txt").write_text("hello world")
    assert toolset.run_edit("small.txt", "world", "there") == "Edited small.txt"
    assert (toolset.workspace / "small.txt").read_text() == "hello there"


def test_an_ambiguous_edit_is_refused_not_applied_to_the_first_match(toolset):
    """When `old_text` matches more than one place the intended location is
    unknowable, and replacing the first silently edits a spot the model may not
    have meant while reporting success -- so it never learns. The edit is refused
    with the count and the file is left untouched; a unique anchor still works.
    """
    path = toolset.workspace / "cfg.py"
    path.write_text("a_value = 0\nb_value = 0\n")

    result = toolset.run_edit("cfg.py", "value = 0", "value = 1")
    assert "ambiguous" in result and "2" in result, result
    assert path.read_text() == "a_value = 0\nb_value = 0\n", "the file was changed"

    # A uniquely-anchored edit on the very same file still lands.
    assert toolset.run_edit("cfg.py", "b_value = 0", "b_value = 1") == "Edited cfg.py"
    assert path.read_text() == "a_value = 0\nb_value = 1\n"


def test_a_write_is_atomic_so_a_failure_leaves_the_original(toolset, monkeypatch):
    """`write_file` / `edit_file` write through a temp file and an atomic rename,
    like the durable store. A bare `write_text` truncates the target in place, so
    a crash mid-write leaves a half-written file and a teammate sharing the
    workspace can read one. Proof: force the rename to fail and the original must
    survive intact with no scratch temp left behind."""
    import mini_loop.durable as durable

    path = toolset.workspace / "cfg.txt"
    path.write_text("ORIGINAL")

    def boom(src, dst):
        raise OSError("disk full at rename")

    monkeypatch.setattr(durable.os, "replace", boom)
    result = toolset.run_write("cfg.txt", "NEW-CONTENT-THAT-MUST-NOT-PARTIALLY-LAND")

    assert result.startswith("Error"), result
    assert path.read_text() == "ORIGINAL", "a failed write corrupted the original"
    leftovers = [p.name for p in toolset.workspace.iterdir() if p.name != "cfg.txt"]
    assert not leftovers, f"a scratch temp survived the failure: {leftovers}"


def test_a_commands_failure_summary_survives(toolset):
    """The reason `run_bash` keeps the tail: the answer is at the end."""
    script = toolset.workspace / "run.sh"
    script.write_text(
        "for i in $(seq 1 40000); do echo LINE; done\n"
        "echo 'FAILED: 3 tests'\nexit 1\n"
    )
    result = toolset.run_bash("sh run.sh")
    assert "FAILED: 3 tests" in result, "the part worth running the command for"
    assert "truncated" in result


def test_a_large_glob_says_it_was_cut(toolset):
    for index in range(20_000):
        (toolset.workspace / f"f{index:06d}.txt").write_text("x")
    result = toolset.run_glob("*.txt")
    lines = result.splitlines()
    assert len(result) <= OUTPUT_CAP
    assert "matches truncated" in result
    # The notice is a *trailing* signal: sorted in with the matches it went by
    # its leading "." to the top, reading as if nothing matched. It belongs at
    # the end, and must survive the output cap rather than being trimmed off it.
    assert "matches truncated" in lines[-1], f"notice misplaced: {lines[-1]!r}"
    assert "truncated" not in lines[0], f"notice sorted to the top: {lines[0]!r}"
    assert lines[0].endswith(".txt"), "the first line should be a real match"


def test_read_offset_pages_past_the_char_cap(toolset):
    """`offset` skips lines *from the file*, so it reaches content past the
    READ_CHAR_CAP-char window. The old read capped at READ_CHAR_CAP chars first
    and applied the line offset *within* that window, so any line beyond the cap
    was unreachable -- while the truncation notice told the model to seek it with
    `offset`. The skip stays memory-bounded even across an overlong line."""
    import tracemalloc

    from mini_loop.tools import READ_CHAR_CAP

    row = "row-{:07d}-" + "z" * 40
    per = len(row.format(0)) + 1
    count = (READ_CHAR_CAP // per) * 2  # twice the cap's worth of lines
    (toolset.workspace / "big.log").write_text(
        "\n".join(row.format(i) for i in range(count))
    )
    # A line that lives well past the first READ_CHAR_CAP chars of the file.
    target = int(count * 0.8)
    out = toolset.run_read("big.log", limit=2, offset=target)
    assert f"row-{target:07d}" in out, "offset could not reach a line past the cap"
    assert len(out.splitlines()) <= 3, "limit not respected paging deep in the file"

    # Skipping past an overlong line must not pull the whole line into memory --
    # the read's hard bound is a property of the resource, offset or not.
    giant = "A" * (READ_CHAR_CAP * 5) + "\nsecond\nthird"
    (toolset.workspace / "giant.log").write_text(giant)
    tracemalloc.start()
    result = toolset.run_read("giant.log", offset=1)
    _current, peak = tracemalloc.get_traced_memory()
    tracemalloc.stop()
    assert "second" in result, "offset=1 did not reach the line after the giant one"
    assert peak < READ_CHAR_CAP * 4, "the skip loaded the overlong line whole"


def test_bash_output_is_memory_bounded_not_just_capped(toolset):
    """`run_bash` capped what the model *saw*, but `communicate()` read all of
    stdout into memory first -- so a high-output command (`yes`, `cat /dev/zero`)
    fills memory within the timeout window and OOMs the host, the round-140
    'bounded output is not bounded work' hazard for bash. The capture is now
    byte-bounded: memory tracks MAX_BASH_CAPTURE, not the command's output size,
    and the overflow is flagged. A normal command is untouched."""
    import sys
    import tracemalloc

    from mini_loop.tools import MAX_BASH_CAPTURE

    big = MAX_BASH_CAPTURE * 8  # far more than the capture bound, produced fast
    cmd = f"{sys.executable} -c \"import sys; sys.stdout.write('a'*{big})\""
    tracemalloc.start()
    out = toolset.run_bash(cmd)
    _current, peak = tracemalloc.get_traced_memory()
    tracemalloc.stop()

    assert peak < MAX_BASH_CAPTURE * 4, f"held {peak:,} bytes for a {big:,}-byte output"
    assert "exceeded" in out, "the truncated capture was not flagged to the model"

    # A command whose output fits is untouched, tail and all, with no notice.
    (toolset.workspace / "s.sh").write_text(
        "for i in $(seq 1 2000); do echo LINE; done\necho THE_ANSWER\n"
    )
    normal = toolset.run_bash("sh s.sh")
    assert "THE_ANSWER" in normal, "the tail of a normal command was lost"
    assert "exceeded" not in normal, "a fitting command was wrongly flagged"


def test_ordinary_output_carries_no_notice(toolset):
    """A notice on every result would be noise, and noise gets ignored."""
    (toolset.workspace / "small.txt").write_text("just a little text\n")
    assert toolset.run_read("small.txt").strip() == "just a little text"
    assert "truncated" not in toolset.run_bash("echo hello")


def test_the_built_in_cap_matches_the_mcp_one():
    """Both land in the same context; different ceilings would be arbitrary."""
    from mini_loop.mcp import MAX_TOOL_RESULT

    assert OUTPUT_CAP == MAX_TOOL_RESULT
