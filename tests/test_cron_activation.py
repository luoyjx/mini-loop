"""Cron activation: a restored schedule is a fact, not an authorization.

The model can schedule a *durable* cron job (`schedule_cron`, default
`durable=True`). Before this round, `_load()` restored such jobs at boot and
the ticker fired them unattended -- one authorized turn became standing
authority surviving every restart, with no human in the loop ever again.
This is the held-vs-once-held defect class (rounds 157/158/161), this time
for authorization itself.

DeepSeek Harness's goal domain draws the line adopted here: durable state
answers "what was scheduled"; process-local activation answers "may THIS
process fire it unattended". Activation is deliberately never persisted:

* scheduling arms the job (the authorization edge just happened);
* a job restored from disk is disarmed and does not fire;
* a skipped occurrence is NOT consumed -- once armed, the next matching
  minute fires normally;
* `arm()` is an operator act, session-scoped like `cancel`, and is not a
  model-facing tool.
"""

from datetime import datetime
from pathlib import Path

from mini_loop.cron import CronJob, CronScheduler


class _Manager:
    """The minimal surface `_fire` needs; records fired sessions."""

    def __init__(self):
        self.fired = []

    def get(self, session_id):
        self.fired.append(session_id)
        return None  # _fire reports "session does not exist"; firing counts


def _scheduler(tmp_path: Path) -> CronScheduler:
    return CronScheduler(_Manager(), durable_path=tmp_path / "cron.json")


def _minute() -> datetime:
    return datetime(2026, 1, 1, 12, 0)


def test_scheduling_arms_the_job(tmp_path):
    scheduler = _scheduler(tmp_path)
    out = scheduler.schedule("sess-1", "* * * * *", "do the thing")
    job_id = out.split()[2].rstrip(":")
    assert scheduler.armed(job_id)
    scheduler._tick_once(_minute())
    [job] = scheduler.jobs.values()
    assert job.last_fired  # the occurrence was consumed: it fired


def test_a_restored_job_is_disarmed_and_does_not_fire(tmp_path):
    first = _scheduler(tmp_path)
    first.schedule("sess-1", "* * * * *", "do the thing")

    # A new process: same durable file, fresh activation.
    restored = _scheduler(tmp_path)
    [job] = restored.jobs.values()
    assert not restored.armed(job.id)
    restored._tick_once(_minute())
    assert job.last_fired == ""  # the occurrence was not consumed
    assert any("not re-armed" in p for p in restored.problems)


def test_arming_a_restored_job_restores_firing(tmp_path):
    first = _scheduler(tmp_path)
    first.schedule("sess-1", "* * * * *", "do the thing")
    restored = _scheduler(tmp_path)
    [job] = restored.jobs.values()
    assert restored.arm(job.id) == f"Armed {job.id}"
    restored._tick_once(_minute())
    assert job.last_fired  # armed again -> the next occurrence fires


def test_arm_is_session_scoped_like_cancel(tmp_path):
    first = _scheduler(tmp_path)
    first.schedule("sess-1", "* * * * *", "do the thing")
    restored = _scheduler(tmp_path)
    [job] = restored.jobs.values()
    # A stranger's arm answers exactly like a nonexistent job.
    assert restored.arm(job.id, "sess-other").startswith("Error")
    assert not restored.armed(job.id)
    assert restored.arm(job.id, "sess-1") == f"Armed {job.id}"


def test_activation_is_never_persisted(tmp_path):
    """The durable file must not carry an armed bit a restart would trust."""

    import json

    scheduler = _scheduler(tmp_path)
    scheduler.schedule("sess-1", "* * * * *", "do the thing")
    stored = json.loads((tmp_path / "cron.json").read_text())
    assert stored, "the durable file should hold the job"
    for record in stored:
        assert "armed" not in record and "activation" not in record


def test_arm_all_reports_how_many_it_armed(tmp_path):
    first = _scheduler(tmp_path)
    first.schedule("sess-1", "* * * * *", "a")
    first.schedule("sess-2", "* * * * *", "b")
    restored = _scheduler(tmp_path)
    assert restored.arm_all() == 2
    assert all(restored.armed(job_id) for job_id in restored.jobs)


def test_the_model_facing_cron_tools_do_not_include_arm(tmp_path):
    """Re-arming is a human act; a model must not re-authorize itself."""

    from mini_loop.builtins import full_registry

    registry = full_registry(cron=True)
    names = list(registry._tools)
    cron_tools = [n for n in names if "cron" in n.lower()]
    assert cron_tools, "cron tools should exist"
    assert not any("arm" in n.lower() for n in cron_tools)
