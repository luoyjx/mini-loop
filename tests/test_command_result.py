"""Structured foreground command results keep execution facts out of prose."""

from dataclasses import FrozenInstanceError
import shlex
import sys

import pytest

import mini_loop.tools as tools
from mini_loop.tools import CommandResult, Toolset


def test_success_is_structured_immutable_and_has_a_stable_projection(tmp_path):
    result = Toolset(tmp_path).run_bash_result("printf 'hello\\n'")

    assert isinstance(result, CommandResult)
    assert result.stdout == "hello\n"
    assert result.stderr == ""
    assert result.exit_code == 0
    assert result.timed_out is False
    assert result.overflowed is False
    assert result.duration_ms >= 0
    assert result.render() == str(result) == "hello"
    with pytest.raises(FrozenInstanceError):
        result.exit_code = 9


def test_stderr_and_nonzero_exit_are_separate_from_stdout(tmp_path):
    result = Toolset(tmp_path).run_bash_result(
        "printf 'ordinary output\\n'; printf 'failure detail\\n' >&2; exit 7"
    )

    assert result.stdout == "ordinary output\n"
    assert result.stderr == "failure detail\n"
    assert result.exit_code == 7
    assert result.timed_out is False
    assert result.render() == "ordinary output\nfailure detail\n(exit 7)"


def test_timeout_retains_metadata_and_masked_partial_streams(tmp_path):
    toolset = Toolset(tmp_path, bash_timeout=1)
    result = toolset.run_bash_result(
        "printf 'partial out\\n'; printf 'partial err\\n' >&2; sleep 30"
    )

    assert result.stdout == "partial out\n"
    assert result.stderr == "partial err\n"
    assert result.exit_code is not None
    assert result.timed_out is True
    assert result.overflowed is False
    assert result.duration_ms >= 900
    # Orthogonal outcomes, independently reported: the partial output IS the
    # diagnostic for a hang (the last line before it stopped), so the render
    # carries both facts -- never the error alone while output exists.
    assert result.render() == "partial out\npartial err\nError: Timeout (1s)"


def test_stdout_and_stderr_share_one_aggregate_capture_cap(tmp_path, monkeypatch):
    monkeypatch.setattr(tools, "MAX_BASH_CAPTURE", 1_024)
    script = (
        "import sys; "
        "sys.stdout.write('o' * 800); sys.stdout.flush(); "
        "sys.stderr.write('e' * 800); sys.stderr.flush()"
    )
    command = f"{shlex.quote(sys.executable)} -c {shlex.quote(script)}"

    result = Toolset(tmp_path).run_bash_result(command)

    assert result.overflowed is True
    assert len(result.stdout) + len(result.stderr) == 1_024
    assert "output exceeded 1,024 bytes" in result.render()


def test_run_bash_remains_the_string_projection(tmp_path):
    toolset = Toolset(tmp_path)

    assert toolset.run_bash("printf 'out\\n'; printf 'err\\n' >&2; exit 3") == (
        "out\nerr\n(exit 3)"
    )
    assert toolset.run_bash("true") == "(no output)"
    assert toolset.run_bash("sudo true") == "Error: Dangerous command blocked"


def test_projection_cannot_reassemble_a_secret_split_across_streams(
    tmp_path, monkeypatch
):
    from mini_loop.secrets import SecretRegistry

    name = "COMMAND_RESULT_SPLIT_TOKEN"
    value = "split-secret-0123456789"
    monkeypatch.setenv(name, value)
    toolset = Toolset(
        tmp_path,
        secrets=SecretRegistry.from_environ(extra_names=[name]),
    )
    script = (
        f"import os,sys; value=os.environ[{name!r}]; "
        "sys.stdout.write(value[:12]); sys.stdout.flush(); "
        "sys.stderr.write(value[12:]); sys.stderr.flush()"
    )
    command = f"{shlex.quote(sys.executable)} -c {shlex.quote(script)}"

    result = toolset.run_bash_result(command)

    assert value not in result.stdout + result.stderr
    assert value not in result.render()
    assert result.render() == "<secret-hidden>"
