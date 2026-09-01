"""Edge-input census for the built-in tools: pin the status quo first.

The long-horizon experiment plan (docs/RSI_RESEARCH_AND_PLAN.md §5) works
in micro-experiments -- reword a truncation notice, change a default limit
-- and each one needs a before-picture, or "the experiment changed
behavior" and "the experiment broke an edge case" are indistinguishable.
This file is the before-picture: named tests for what `read_file` does
TODAY on inputs nobody sends on the happy path. Nothing here asserts the
behavior is *good*; findings are candidates for deliberate experiments,
and when an experiment lands, its pin flips here in the same change:

* RESOLVED (micro-experiment A, 2026-08-31): an offset past EOF used to
  read exactly like an empty file; it now names the end and the file's
  actual line count.
* RESOLVED (micro-experiment B, 2026-08-31): the offset guidance on a
  READ_CHAR_CAP-exceeded read used to sit on the LAST line, exactly where
  the head-only output cap cuts; it now leads the output and survives.

Changing a finding is a deliberate experiment measured by the behavioral
benchmark dimensions -- not a drive-by fix, which is why current behavior
is pinned rather than patched.
"""

import os

import pytest

from mini_loop.tools import OUTPUT_CAP, READ_CHAR_CAP, Toolset


@pytest.fixture
def toolset(tmp_path):
    workspace = tmp_path / "ws"
    workspace.mkdir()
    return Toolset(workspace)


def test_an_empty_file_reads_as_empty_not_error(toolset):
    (toolset.workspace / "empty.txt").write_text("")
    assert toolset.run_read("empty.txt") == ""


def test_an_offset_past_eof_names_the_end_not_an_empty_file(toolset):
    """Micro-experiment A: paging past EOF used to answer the same empty
    string as an empty file. Now it names the end and the real line
    count, so "I paged too far" and "nothing there" read differently."""

    (toolset.workspace / "empty.txt").write_text("")
    (toolset.workspace / "two.txt").write_text("a\nb\n")

    past_eof = toolset.run_read("two.txt", offset=10)
    assert past_eof == "... (nothing at offset 10: the file ends after 2 lines)"
    assert past_eof != toolset.run_read("empty.txt")

    # The boundary reads the same way: offset == line count is also "past".
    assert "ends after 2 lines" in toolset.run_read("two.txt", offset=2)
    # No trailing newline still counts the partial last line.
    (toolset.workspace / "tail.txt").write_text("a\nb")
    assert "ends after 2 lines" in toolset.run_read("tail.txt", offset=9)
    # An empty file with an offset says so too, instead of a bare "".
    assert "ends after 0 lines" in toolset.run_read("empty.txt", offset=3)
    # A valid offset is untouched by the experiment.
    assert toolset.run_read("two.txt", offset=1) == "b"


def test_negative_offset_and_limit_are_clamped_not_errors(toolset):
    """Negative values clamp to zero; with limit clamped to 0 the read
    answers only the more-lines marker, no content."""

    (toolset.workspace / "two.txt").write_text("a\nb\n")
    assert toolset.run_read("two.txt", offset=-5, limit=-3) == \
        "... (2 more lines)"
    assert toolset.run_read("two.txt", limit=0) == "... (2 more lines)"


def test_binary_bytes_are_replaced_never_a_crash(toolset):
    (toolset.workspace / "blob.dat").write_bytes(b"PNG\x00\xff\xfe\xfaend")
    result = toolset.run_read("blob.dat")
    assert not result.startswith("Error")
    assert "�" in result, "undecodable bytes surface as replacements"
    assert "PNG" in result and "end" in result


def test_a_directory_path_answers_error(toolset):
    (toolset.workspace / "sub").mkdir()
    assert toolset.run_read("sub").startswith("Error")


@pytest.mark.skipif(os.geteuid() == 0, reason="root ignores file modes")
def test_an_unreadable_file_answers_error_and_stays_unread(toolset):
    locked = toolset.workspace / "locked.txt"
    locked.write_text("secret")
    locked.chmod(0)
    try:
        result = toolset.run_read("locked.txt")
        assert result.startswith("Error")
        assert "secret" not in result
    finally:
        locked.chmod(0o644)


def test_a_huge_single_line_keeps_the_offset_guidance(toolset):
    """Micro-experiment B: the guidance used to be the LAST line, exactly
    where the head-only cap cuts -- the pathological input that most
    needed it never saw it. Leading the output, it survives any head
    truncation, and the generic cap marker still names the cut."""

    (toolset.workspace / "one-line.txt").write_text(
        "A" * (READ_CHAR_CAP + READ_CHAR_CAP // 2)
    )
    result = toolset.run_read("one-line.txt")
    assert len(result) <= OUTPUT_CAP
    assert "[truncated:" in result, "the generic cap marker must survive"
    assert result.splitlines()[0].startswith("... (file exceeds"), (
        "the guidance must lead the output, where head truncation "
        "cannot reach it"
    )
    assert "read further with a larger `offset`" in result

    # A small file (or a small window under `limit`) carries no guidance.
    (toolset.workspace / "small.txt").write_text("one\ntwo\n")
    assert "file exceeds" not in toolset.run_read("small.txt")
