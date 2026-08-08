"""A problem channel nobody reads is not reporting.

Rounds 45 to 50 gave six subsystems a `ProblemLog` on the reasoning that a
surface with nowhere to say "that did not work" eventually fails silently.
Rounds since added more -- the action journal in 86, the tool registry in 91 --
and every one of them was written up as "reported, not hidden".

That was half true. The value was recorded; nothing surfaced it. The audit
enumerated subsystems by hand, so it checked `cron` and `skills` and nothing
else, while `actions`, `memory`, `tool_registry` and the bus accumulated reports
with no reader:

    manager attributes carrying a problems log   6
    surfaced by audit()                          2

The audit sweeps now instead of enumerating, which is the same move that stopped
disk sinks and cross-tenant leaks being found one at a time. The test below is
the part that matters: it discovers the channels the same way the audit does and
requires each to reach the report, so a channel added in a later round is
covered by existing code rather than by somebody remembering.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.audit import _SPECIFICALLY_CHECKED, audit, render
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 memory_root=tmp_path / "mem", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
        tool_registry=full_registry(),
    )


def _channels(manager) -> dict:
    """Every attribute carrying a problems log, found not listed."""

    return {
        name: getattr(manager, name).problems
        for name in dir(manager)
        if not name.startswith("_")
        and hasattr(getattr(manager, name, None), "problems")
    }


def test_the_sweep_finds_the_channels(tmp_path):
    """A discovery that matches nothing would pass the case below forever."""

    found = _channels(_manager(tmp_path))
    assert len(found) >= 5, f"channel discovery sees {sorted(found)}"
    assert {"memory", "cron", "skills"} <= set(found)


def test_every_problem_channel_reaches_the_report(tmp_path):
    manager = _manager(tmp_path)
    channels = _channels(manager)
    for name, log in channels.items():
        log.append(f"CANARY{name.upper()}XYZ")

    report = render(audit(manager))

    missing = sorted(
        name for name in channels if f"CANARY{name.upper()}XYZ" not in report
    )
    assert not missing, (
        "these subsystems have somewhere to report and nowhere it is read: "
        f"{missing}"
    )


def test_a_clean_manager_reports_no_problem_findings(tmp_path):
    """Not vacuous: the sweep must not invent findings for empty channels."""

    manager = _manager(tmp_path)
    for log in _channels(manager).values():
        log.clear()

    findings = [f for f in audit(manager) if f.check.endswith("-problems")]
    assert not findings, [f.check for f in findings]


def test_one_fault_is_reported_once(tmp_path):
    """The hand-written checks and the sweep must not both claim the same log."""

    manager = _manager(tmp_path)
    for name in _SPECIFICALLY_CHECKED:
        channel = getattr(manager, name, None)
        if channel is None:                                  # pragma: no cover
            continue
        channel.problems.append("DOUBLECOUNTXYZ")

    report = render(audit(manager))
    assert report.count("DOUBLECOUNTXYZ") == len(
        [n for n in _SPECIFICALLY_CHECKED if getattr(manager, n, None) is not None]
    )


def test_the_specific_checks_still_exist(tmp_path):
    """An exemption for a check that was deleted would silently drop a channel.

    The sweep skips `cron` and `skills` because each has a hand-written finding
    with a better remedy. If one of those is removed, the exemption turns into a
    hole rather than a duplicate.
    """

    manager = _manager(tmp_path)
    for name in _SPECIFICALLY_CHECKED:
        getattr(manager, name).problems.append(f"SPECIFIC{name.upper()}XYZ")

    report = render(audit(manager))
    for name in _SPECIFICALLY_CHECKED:
        assert f"SPECIFIC{name.upper()}XYZ" in report, (
            f"{name} is exempt from the sweep and has no check of its own"
        )


@pytest.mark.asyncio
async def test_a_real_fault_reaches_the_report(tmp_path):
    """End to end, rather than by appending a canary to a log."""

    manager = _manager(tmp_path)
    session = manager.create()
    await session.run("hi")
    manager.memory.write(
        name="oversized", mem_type="project", description="d", body="B" * 40_000
    )

    report = render(audit(manager))
    assert "truncated" in report and "memory" in report
