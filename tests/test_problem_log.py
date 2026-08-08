"""The reporting channel failed the checklist it was added to satisfy.

Rounds 45 to 50 gave six subsystems a `problems` list, on the reasoning that a
surface with nowhere to say "that did not work" eventually fails silently. Every
one was a plain list, appended to on each occurrence and never trimmed. Asking
the round-49 checklist about them -- *is the content bounded* -- answers no:

    cron   : one dead job for 24h -> 1,440 problem entries, 138,240 chars
    teams  : 10,000 bad reads      -> 10,000 entries
    memory : 2,000 oversized writes-> 2,000 entries
    tasks  : 2,000 oversized tasks -> 2,000 entries

Two failures. A long-running process leaks memory through its own error channel,
which is the obvious one. The subtler one matters more: a single recurring fault
produces thousands of identical entries, so a count stops meaning "how many
things are wrong" and the *rare* problem -- the one nobody has seen yet -- ends
up buried under repeats of the one they already know about.

`ProblemLog` deduplicates, counts, and keeps the newest distinct problems. It
subclasses `list` so the six call sites did not have to change to gain it.
"""

import pathlib
import tempfile
from datetime import datetime

import pytest

from mini_loop.cron import CronScheduler
from mini_loop.memory import MemoryStore
from mini_loop.problems import MAX_DISTINCT_PROBLEMS, ProblemLog
from mini_loop.skills import SkillLoader
from mini_loop.tasks import TaskStore
from mini_loop.teams import MessageBus


# --- the type -------------------------------------------------------------

def test_a_repeated_problem_is_counted_not_accumulated():
    log = ProblemLog()
    for _ in range(1_440):
        log.append("the same thing went wrong")
    assert len(log) == 1
    assert log.total() == 1_440
    assert log.summary() == ["the same thing went wrong (x1440)"]


def test_distinct_problems_are_all_kept_up_to_the_limit():
    log = ProblemLog()
    for index in range(MAX_DISTINCT_PROBLEMS):
        log.append(f"problem {index}")
    assert len(log) == MAX_DISTINCT_PROBLEMS
    assert log.dropped == 0


def test_the_newest_distinct_problems_win():
    """A subsystem with fifty *different* faults has a bigger issue than the
    fifty-first going unrecorded -- but the loss is counted, not hidden."""
    log = ProblemLog(limit=3)
    for index in range(5):
        log.append(f"problem {index}")
    assert list(log) == ["problem 2", "problem 3", "problem 4"]
    assert log.dropped == 2


def test_a_single_occurrence_reads_plainly():
    log = ProblemLog()
    log.append("one off")
    assert log.summary() == ["one off"]


def test_it_behaves_like_the_list_it_replaced():
    """Six call sites append to it and the audit iterates and takes `len`."""
    log = ProblemLog()
    assert not log
    log.append("x")
    assert log and len(log) == 1
    assert list(log) == ["x"]
    assert "x" in log
    log.extend(["y", "x"])
    assert list(log) == ["x", "y"]
    log.clear()
    assert not log and log.total() == 0


# --- the checklist, asked of every reporting channel ----------------------

def _cron(tmp_path):
    class NoSessions:
        def get(self, session_id):
            return None

    scheduler = CronScheduler(NoSessions(), durable_path=tmp_path / "cron.json")
    scheduler.schedule("gone", "* * * * *", "report")

    def provoke():
        for minute in range(600):
            list(scheduler.jobs.values())[0].last_fired = ""
            scheduler._tick_once(datetime(2026, 8, 3, minute // 60 % 24, minute % 60))

    return scheduler, provoke


def _teams(tmp_path):
    bus = MessageBus(tmp_path / "teams")
    return bus, lambda: [bus.read("../../escape") for _ in range(600)]


def _memory(tmp_path):
    store = MemoryStore(tmp_path / "mem")
    return store, lambda: [
        store.write(f"m{i}", "project", "d", "X" * 40_000) for i in range(600)
    ]


def _tasks(tmp_path):
    store = TaskStore(tmp_path / "tasks")
    return store, lambda: [
        store.create(subject="s", description="X" * 20_000) for _ in range(600)
    ]


def _skills(tmp_path):
    root = tmp_path / "skills"
    (root / "a").mkdir(parents=True)
    (root / "b").mkdir(parents=True)
    for directory in ("a", "b"):
        (root / directory / "SKILL.md").write_text(
            "---\nname: same\ndescription: d\n---\nbody"
        )
    loader = SkillLoader(root)
    return loader, lambda: None


CHANNELS = {"cron": _cron, "teams": _teams, "memory": _memory,
            "tasks": _tasks, "skills": _skills}


@pytest.mark.parametrize("name", sorted(CHANNELS))
def test_the_reporting_channel_is_itself_bounded(tmp_path, name):
    subject, provoke = CHANNELS[name](tmp_path)
    provoke()
    assert len(subject.problems) <= MAX_DISTINCT_PROBLEMS, (
        f"{name} accumulated {len(subject.problems)} entries in its own error "
        "channel"
    )


@pytest.mark.parametrize("name", sorted(CHANNELS))
def test_the_reporting_channel_counts_repeats(tmp_path, name):
    subject, provoke = CHANNELS[name](tmp_path)
    provoke()
    assert isinstance(subject.problems, ProblemLog), (
        f"{name} still uses a plain list, so a repeat floods it"
    )


def test_a_recurring_fault_does_not_bury_a_rare_one(tmp_path):
    """The reason deduplication matters more than the memory does."""
    scheduler, provoke = _cron(tmp_path)
    provoke()
    scheduler.problems.append("something nobody has seen before")

    assert "something nobody has seen before" in scheduler.problems
    assert len(scheduler.problems) == 2, (
        f"the rare problem is buried among {len(scheduler.problems)} entries"
    )


# --- and it reaches the operator with the count ---------------------------

def test_the_audit_shows_how_often_a_problem_recurred(tmp_path):
    from mini_loop import SessionManager, Settings
    from mini_loop.audit import audit
    from mini_loop.cron import CronJob
    from mini_loop.fake_llm import FakeAsyncAnthropic

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=pathlib.Path(__file__).resolve().parent.parent / "skills"),
        FakeAsyncAnthropic(),
    )
    manager.cron.jobs["j1"] = CronJob(
        id="j1", cron="* * * * *", prompt="p", session_id="gone",
        recurring=True, durable=False, last_fired="",
    )
    for minute in range(10):
        manager.cron.jobs["j1"].last_fired = ""
        manager.cron._tick_once(datetime(2026, 8, 3, 3, minute))

    findings = {f.check: f for f in audit(manager, environ={"PATH": "/usr/bin"})}
    detail = findings["cron-problems"].detail
    assert "(x10)" in detail, f"the recurrence count is missing: {detail}"
