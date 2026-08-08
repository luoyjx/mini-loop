"""A cron occurrence is persisted-as-fired before it is dispatched.

`_tick_once` marks `last_fired`, dispatches the run, and persists. The old
order dispatched *then* persisted, so a crash in that window left the run
already fired but the mark only in memory -- and a restart within the same
minute re-fired it. That is OpenWorker's cross-store crash-consistency hazard
(research doc 9.2.10) and it contradicts the scheduler's own stated intent:
the exception path already prefers a lost occurrence over a double one.

The fix persists the mark (and any one-shot removal) *before* dispatching, so
the durable state gates re-firing and the dispatch is strictly after it. A
crash before the save means nothing ran, so re-firing on restart is correct;
a crash after means the mark is on disk, so it will not re-fire. At-most-once
across a crash, matching the in-process semantics.
"""

import json
from datetime import datetime
from pathlib import Path

from mini_loop.cron import CronScheduler


class _RecordingScheduler(CronScheduler):
    """Captures the durable file's contents at the instant `_fire` runs."""

    fires: list

    def _fire(self, job):
        self.fires.append({
            "id": job.id,
            "on_disk_last_fired": self._on_disk_mark(job.id),
        })

    def _on_disk_mark(self, job_id):
        if not self.durable_path.exists():
            return None
        records = json.loads(self.durable_path.read_text())
        for record in records:
            if record["id"] == job_id:
                return record.get("last_fired")
        return None


def _scheduler(tmp_path):
    scheduler = _RecordingScheduler(manager=None,
                                    durable_path=tmp_path / ".cron.json")
    scheduler.fires = []
    return scheduler


NOW = datetime(2026, 8, 5, 10, 0)
MARKER = NOW.strftime("%Y-%m-%d %H:%M")


def test_the_mark_is_on_disk_before_the_run_dispatches(tmp_path):
    scheduler = _scheduler(tmp_path)
    scheduler.schedule("sess", "* * * * *", "do the thing", durable=True)

    scheduler._tick_once(NOW)

    assert len(scheduler.fires) == 1
    assert scheduler.fires[0]["on_disk_last_fired"] == MARKER, (
        "the occurrence dispatched before its mark reached disk; a crash here "
        "re-fires it on restart"
    )


def test_a_same_minute_restart_does_not_double_fire(tmp_path):
    durable = tmp_path / ".cron.json"
    first = _RecordingScheduler(manager=None, durable_path=durable)
    first.fires = []
    first.schedule("sess", "* * * * *", "do the thing", durable=True)
    first._tick_once(NOW)
    assert len(first.fires) == 1

    # A fresh process reloads the same durable file and ticks the same minute.
    restarted = _RecordingScheduler(manager=None, durable_path=durable)
    restarted.fires = []
    restarted._tick_once(NOW)

    assert restarted.fires == [], (
        "a restart within the same minute re-fired an already-fired occurrence"
    )


def test_a_later_minute_still_fires_after_restart(tmp_path):
    """Not a wall: the persisted mark only blocks the *same* minute. The next
    matching minute must still fire, or the schedule is dead after one tick."""

    durable = tmp_path / ".cron.json"
    first = _RecordingScheduler(manager=None, durable_path=durable)
    first.fires = []
    first.schedule("sess", "* * * * *", "do the thing", durable=True)
    first._tick_once(NOW)

    restarted = _RecordingScheduler(manager=None, durable_path=durable)
    restarted.fires = []
    restarted._tick_once(datetime(2026, 8, 5, 10, 1))  # the next minute

    assert len(restarted.fires) == 1


def test_a_nondurable_job_still_fires(tmp_path):
    """The persist-before-fire path is gated on `durable`; a non-durable job
    has no save step and must still dispatch."""

    scheduler = _scheduler(tmp_path)
    scheduler.schedule("sess", "* * * * *", "ephemeral", durable=False)

    scheduler._tick_once(NOW)

    assert len(scheduler.fires) == 1
