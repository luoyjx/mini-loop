"""Edge-input census for the built-in tools: pin the status quo first.

The long-horizon experiment plan (docs/RSI_RESEARCH_AND_PLAN.md §5) works
in micro-experiments -- reword a truncation notice, change a default limit
-- and each one needs a before-picture, or "the experiment changed
behavior" and "the experiment broke an edge case" are indistinguishable.
This file is the before-picture: named tests for what `read_file` does
TODAY on inputs nobody sends on the happy path. Nothing here asserts the
behavior is *good*; two findings are explicitly candidates for future
experiments, marked FINDING below:

* an offset past EOF reads exactly like an empty file -- the model cannot
  tell "no such content" from "nothing there at all";
* on a single overlong line, the head-only output cap swallows the very
  notice that says how to read further (`offset` guidance), leaving only
  the generic truncation marker.

Changing either is a deliberate experiment measured by the behavioral
benchmark dimensions -- not a drive-by fix, which is why the current
behavior is pinned rather than patched.
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


def test_an_offset_past_eof_reads_the_same_as_an_empty_file(toolset):
    """FINDING: both answer the empty string, so the model cannot
    distinguish "I paged past the end" from "the file is empty"."""

    (toolset.workspace / "empty.txt").write_text("")
    (toolset.workspace / "two.txt").write_text("a\nb\n")
    past_eof = toolset.run_read("two.txt", offset=10)
    assert past_eof == ""
    assert past_eof == toolset.run_read("empty.txt")


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


def test_a_huge_single_line_loses_the_offset_guidance(toolset):
    """FINDING: the read-level notice ("read further with a larger
    `offset`") is appended as the LAST line, and the head-only output cap
    then cuts it off -- so on the pathological input that most needs the
    guidance, only the generic truncation marker survives."""

    (toolset.workspace / "one-line.txt").write_text(
        "A" * (READ_CHAR_CAP + READ_CHAR_CAP // 2)
    )
    result = toolset.run_read("one-line.txt")
    assert len(result) <= OUTPUT_CAP
    assert "[truncated:" in result, "the generic cap marker must survive"
    assert "read further with a larger `offset`" not in result, (
        "if the guidance now survives, an experiment changed this "
        "deliberately -- update this pin and the §5 finding together"
    )
