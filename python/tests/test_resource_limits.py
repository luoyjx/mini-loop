"""Nothing bounds how much a shell command consumes, and nothing said so.

Round 70 fixed `bash_timeout` so a timed-out command's children are reaped, and
recorded what was still missing: memory and disk are unbounded. This round tried
to close that and **could not**, which is the finding.

Measured on this platform rather than assumed:

    ulimit -H -f 20480  then write 50 MB  -> 52.4 MB on disk, not capped
    ulimit -H -t 2      then infinite loop -> still running after 25s
    ulimit -H -v 204800                    -> "cannot modify limit: Invalid argument"
    a command allocating 700 MB            -> allocated, unopposed

`preexec_fn` with `resource.setrlimit` is the usual answer and is not available
here: `run_bash` executes in a thread, where `preexec_fn` is documented as
unsafe. Limiting consumption needs a container.

So what shipped is not a limit but the *absence* of one, made visible. A
sandboxed deployment draws no `shell-confinement` finding at all -- correctly,
since confinement is active -- and an operator reading that clean result has
nothing telling them a runaway command can still take the host down. The sandbox
answers "where may it write", not "how much may it consume", and those read as
the same reassurance.

This is the round-37 discipline applied to an absence rather than to a weak
control: a gap nobody is told about gets counted as covered.
"""

import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.audit import audit
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.registry import ToolRegistry
from mini_loop.sandbox import SeatbeltSandbox, default_sandbox

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
CLEAN_ENV = {"PATH": "/usr/bin"}


def _findings(tmp_path, *, sandboxed=True, registry=None):
    workspace = tmp_path / "ws"
    kwargs = {"tool_registry": registry} if registry is not None else {}
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=workspace, skills_dir=SKILLS),
        FakeAsyncAnthropic(),
        sandbox=(default_sandbox(workspace)
                 if sandboxed and SeatbeltSandbox.available() else None),
        **kwargs,
    )
    return {f.check: f for f in audit(manager, environ=CLEAN_ENV)}


def test_a_shell_deployment_is_told_consumption_is_unbounded(tmp_path):
    assert "resource-limits" in _findings(tmp_path)


def test_a_sandbox_does_not_suppress_it(tmp_path):
    """The whole point: confinement and consumption are different questions,
    and a clean confinement result must not read as covering both."""
    sandboxed = _findings(tmp_path, sandboxed=True)
    assert "shell-confinement" not in sandboxed, "fixture is not actually sandboxed"
    assert "resource-limits" in sandboxed


def test_it_still_fires_without_a_sandbox(tmp_path):
    unsandboxed = _findings(tmp_path, sandboxed=False)
    assert "shell-confinement" in unsandboxed
    assert "resource-limits" in unsandboxed


def test_a_deployment_with_no_shell_is_not_warned(tmp_path):
    """A finding that fires when there is no shell is noise."""
    assert "resource-limits" not in _findings(tmp_path, registry=ToolRegistry())


def test_the_finding_names_what_is_unbounded(tmp_path):
    detail = _findings(tmp_path)["resource-limits"].detail.lower()
    for resource in ("memory", "disk", "cpu"):
        assert resource in detail, resource


def test_the_finding_says_what_is_bounded(tmp_path):
    """`bash_timeout` is real and reaps its process group since round 70;
    implying nothing is bounded would be its own overstatement."""
    detail = _findings(tmp_path)["resource-limits"].detail.lower()
    assert "wall time" in detail


def test_the_remedy_is_the_one_that_works(tmp_path):
    """`ulimit` was measured and does not enforce these here, so pointing an
    operator at it would send them to spend an afternoon on nothing."""
    finding = _findings(tmp_path)["resource-limits"]
    assert "container" in finding.remedy.lower()
    assert "ulimit" in finding.remedy.lower(), (
        "the remedy should say the obvious alternative was tried"
    )


def test_it_is_not_graded_as_an_escape(tmp_path):
    """A runaway command exhausts the host; it does not escape or leak. Grading
    by consequence keeps the high findings meaning what they mean."""
    assert _findings(tmp_path)["resource-limits"].severity == "medium"


@pytest.mark.skipif(not SeatbeltSandbox.available(), reason="macOS Seatbelt only")
def test_the_sandbox_does_not_claim_to_limit_consumption(tmp_path):
    workspace = tmp_path / "ws"
    workspace.mkdir()
    describe = default_sandbox(workspace).describe.lower()
    for word in ("memory", "cpu", "quota", "limit"):
        assert word not in describe, (
            f"`describe` says {word!r}, which reads as a consumption bound"
        )
