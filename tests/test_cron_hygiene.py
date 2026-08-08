"""Scheduled work that stops running looks exactly like work never scheduled.

Third round on agent-authored state, after skills (45) and memory (46). A cron
job is the strongest case in the family: it persists across restarts and fires a
*prompt* into a session, unattended, with nobody watching the result.

**A dead job consumed its schedule in silence.** `_tick_once` sets
`last_fired` and then calls `_fire`, which returns without a word when the
session is gone. The occurrence is spent, nothing runs, and the next tick sees a
job that has already fired -- for every occurrence, forever, with no signal that
the schedule is dead.

**Sink eight.** The durable JSON stored prompts verbatim. Sinks one to four were
the transcript, the event stream, the trajectory and the state store; five and
six were compaction's workspace files (round 32); seven was memory (round 46).

**No bounds.** A 2,000,000-character prompt was accepted and stored -- half a
million tokens fired unattended -- and 500 `schedule` calls produced 503 jobs.

**Silent loss on load.** An unreadable durable file dropped every job and
returned zero, so a lost schedule was indistinguishable from an empty one.
"""

import asyncio
import json
import pathlib
from datetime import datetime

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.audit import audit
from mini_loop.cron import CronJob, CronScheduler
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.secrets import SecretRegistry

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
SECRET = "sk-CRON-LEAK-0123456789abcdef"


class NoSessions:
    def get(self, session_id):
        return None


def _scheduler(tmp_path, *, secrets=None, name="cron.json"):
    return CronScheduler(NoSessions(), durable_path=tmp_path / name, secrets=secrets)


def _registry():
    return SecretRegistry.from_environ(environ={"P_API_KEY": SECRET})


# --- a dead schedule must say so -----------------------------------------

def test_firing_into_a_missing_session_is_reported(tmp_path):
    scheduler = _scheduler(tmp_path)
    scheduler.schedule("gone", "* * * * *", "run the nightly report")
    scheduler._tick_once(datetime(2026, 8, 2, 3, 0))

    assert scheduler.problems, "the occurrence vanished without a word"
    assert "does not exist" in scheduler.problems[0]
    assert "gone" in scheduler.problems[0]


def test_the_job_is_not_quietly_removed(tmp_path):
    """It stays scheduled, so cancelling it remains the operator's decision."""
    scheduler = _scheduler(tmp_path)
    scheduler.schedule("gone", "* * * * *", "report")
    scheduler._tick_once(datetime(2026, 8, 2, 3, 0))
    assert scheduler.jobs


def test_a_healthy_fire_reports_nothing(tmp_path):
    """A signal that always fires is a signal nobody reads."""
    fired = []

    class Live:
        def get(self, session_id):
            class Session:
                # `run_context` matches the real AgentSession.run signature --
                # `_fire` passes it explicitly (round 127) to pin the cron turn's
                # untrusted authority, so a fake that omits it fails the call.
                async def run(self, prompt, run_context=None):
                    fired.append(prompt)
            return Session()

    scheduler = CronScheduler(Live(), durable_path=tmp_path / "cron.json")
    scheduler.schedule("s1", "* * * * *", "report")

    async def tick():
        # Inside a loop: `_fire` dispatches with `create_task`, so ticking
        # synchronously fails for a reason that has nothing to do with the job.
        scheduler._tick_once(datetime(2026, 8, 2, 3, 0))
        await asyncio.sleep(0)

    asyncio.run(tick())
    assert scheduler.problems == []
    assert fired and fired[0].endswith("report")


# --- sink eight -----------------------------------------------------------

def test_a_prompt_holding_a_secret_is_not_stored_raw(tmp_path):
    scheduler = _scheduler(tmp_path, secrets=_registry())
    scheduler.schedule("s1", "0 3 * * *", f"deploy using {SECRET}")
    assert SECRET not in (tmp_path / "cron.json").read_text()


def test_masking_a_stored_prompt_is_reported(tmp_path):
    """The restored job will fire masked, which changes what it does."""
    scheduler = _scheduler(tmp_path, secrets=_registry())
    scheduler.schedule("s1", "0 3 * * *", f"deploy using {SECRET}")
    assert any("masked" in problem for problem in scheduler.problems)


def test_an_ordinary_prompt_is_stored_intact(tmp_path):
    scheduler = _scheduler(tmp_path, secrets=_registry())
    scheduler.schedule("s1", "0 3 * * *", "run the nightly report")
    stored = json.loads((tmp_path / "cron.json").read_text())
    assert stored[0]["prompt"] == "run the nightly report"
    assert scheduler.problems == []


def test_the_manager_passes_the_registry(tmp_path):
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
        secrets=_registry(),
    )
    assert manager.cron.secrets is not None


# --- bounded --------------------------------------------------------------

def test_an_oversized_prompt_is_refused_not_truncated(tmp_path):
    """A truncated instruction that still fires is worse than none."""
    scheduler = _scheduler(tmp_path)
    result = scheduler.schedule("s1", "0 3 * * *", "X" * 2_000_000)
    assert result.startswith("Error:")
    assert scheduler.jobs == {}


def test_a_prompt_at_the_limit_is_accepted(tmp_path):
    scheduler = _scheduler(tmp_path)
    assert not scheduler.schedule(
        "s1", "0 3 * * *", "X" * CronScheduler.MAX_PROMPT
    ).startswith("Error:")


def test_jobs_cannot_accumulate_without_limit(tmp_path):
    scheduler = _scheduler(tmp_path)
    for index in range(CronScheduler.MAX_JOBS + 50):
        scheduler.schedule("s1", "0 3 * * *", f"job {index}")
    assert len(scheduler.jobs) == CronScheduler.MAX_JOBS


# --- loading --------------------------------------------------------------

def test_an_unreadable_durable_file_is_reported(tmp_path):
    (tmp_path / "cron.json").write_text("{ this is not json")
    scheduler = _scheduler(tmp_path)
    assert scheduler.jobs == {}
    assert any("unreadable" in problem for problem in scheduler.problems)


def test_a_job_with_a_bad_cron_expression_is_reported(tmp_path):
    (tmp_path / "cron.json").write_text(json.dumps([{
        "id": "x", "cron": "not a cron", "prompt": "p", "session_id": "s",
        "recurring": True, "durable": True, "last_fired": "",
    }]))
    scheduler = _scheduler(tmp_path)
    assert scheduler.jobs == {}
    assert any("not a valid cron" in problem for problem in scheduler.problems)


def test_good_jobs_round_trip_without_complaint(tmp_path):
    scheduler = _scheduler(tmp_path)
    scheduler.schedule("s1", "0 3 * * *", "nightly")
    reloaded = _scheduler(tmp_path)
    assert len(reloaded.jobs) == 1
    assert reloaded.problems == []


# --- reported where an operator looks -------------------------------------

def test_the_audit_reports_broken_schedules(tmp_path):
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
    )
    manager.cron.jobs["j1"] = CronJob(
        id="j1", cron="* * * * *", prompt="p", session_id="gone",
        recurring=True, durable=False, last_fired="",
    )
    manager.cron._tick_once(datetime(2026, 8, 2, 3, 0))

    findings = {f.check for f in audit(manager, environ={"PATH": "/usr/bin"})}
    assert "cron-problems" in findings


def test_a_healthy_scheduler_draws_no_finding(tmp_path):
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
    )
    checks = {f.check for f in audit(manager, environ={"PATH": "/usr/bin"})}
    assert "cron-problems" not in checks
