"""One cron occurrence, one dispatch, however many processes share the store.

The persisted `last_fired` mark defends a *restart*: the fresh process loads
it before ticking (test_cron_crash_consistency.py). Two live processes
sharing one durable file never reload -- each holds the stale mark in memory,
both pass the same-minute check, both dispatch. Roadmap G7 calls this the
duplicate-claim risk, and it is exactly how a second worker or a fat-fingered
second `mini-loop serve` behaves.

The protocol is one O_EXCL file per (job, minute): the first creator owns
the occurrence. Pinned here:

* two schedulers on one store fire a shared occurrence exactly once;
* the loser stays quiet (the occurrence ran elsewhere -- nothing is lost)
  and stays alive (it can win the next minute);
* claim infrastructure failure is loud and falls toward a lost occurrence,
  the direction the save path already chose, never toward two;
* claim files do not accumulate: the winner unclaims the previous mark,
  and cancellation unclaims the last.
"""

from datetime import datetime

from mini_loop.cron import CronScheduler


class _RecordingScheduler(CronScheduler):
    fires: list

    def _fire(self, job):
        self.fires.append(job.id)


def _scheduler(durable):
    scheduler = _RecordingScheduler(manager=None, durable_path=durable)
    scheduler.fires = []
    return scheduler


NOW = datetime(2026, 8, 23, 10, 0)
NEXT = datetime(2026, 8, 23, 10, 1)


def test_two_live_processes_fire_a_shared_occurrence_once(tmp_path):
    durable = tmp_path / ".cron.json"
    first = _scheduler(durable)
    first.schedule("sess", "* * * * *", "shared job", durable=True)
    # The second process loads the same store BEFORE the minute fires -- the
    # concurrent-worker shape, not the restart shape -- and its operator
    # re-arms the restored job.
    second = _scheduler(durable)
    second.arm_all()

    first._tick_once(NOW)
    second._tick_once(NOW)

    assert len(first.fires) + len(second.fires) == 1, (
        "both processes dispatched the same occurrence"
    )


def test_the_loser_stays_quiet_and_wins_the_next_minute(tmp_path):
    durable = tmp_path / ".cron.json"
    first = _scheduler(durable)
    first.schedule("sess", "* * * * *", "shared job", durable=True)
    second = _scheduler(durable)
    second.arm_all()

    first._tick_once(NOW)
    second._tick_once(NOW)

    # Losing the race is normal multi-process operation: the occurrence ran
    # elsewhere. It is not a problem to report.
    assert len(second.problems) == 0
    # And it is not a death: the loser competes for -- and here wins -- the
    # next occurrence.
    second._tick_once(NEXT)
    assert len(first.fires) + len(second.fires) == 2


def test_a_broken_claims_store_is_loud_and_loses_the_occurrence(tmp_path):
    durable = tmp_path / ".cron.json"
    scheduler = _scheduler(durable)
    scheduler.schedule("sess", "* * * * *", "job", durable=True)
    # A plain file where the claims directory must be: mkdir raises, the
    # per-job handler reports, and nothing dispatches -- the same direction
    # the save path falls (a lost occurrence, never a double one).
    scheduler._claims_dir.write_text("not a directory")

    scheduler._tick_once(NOW)

    assert scheduler.fires == []
    assert any("firing failed" in p for p in scheduler.problems)


def test_claim_files_do_not_accumulate_across_minutes(tmp_path):
    durable = tmp_path / ".cron.json"
    scheduler = _scheduler(durable)
    scheduler.schedule("sess", "* * * * *", "job", durable=True)

    for minute in range(6):
        scheduler._tick_once(datetime(2026, 8, 23, 10, minute))

    assert len(scheduler.fires) == 6
    claims = list(scheduler._claims_dir.iterdir())
    assert len(claims) == 1, (
        f"spent claims were not pruned: {[c.name for c in claims]}"
    )


def test_cancellation_unclaims_the_last_occurrence(tmp_path):
    durable = tmp_path / ".cron.json"
    scheduler = _scheduler(durable)
    result = scheduler.schedule("sess", "* * * * *", "job", durable=True)
    job_id = result.split()[2].rstrip(":")
    scheduler._tick_once(NOW)
    assert len(list(scheduler._claims_dir.iterdir())) == 1

    scheduler.cancel(job_id)

    assert list(scheduler._claims_dir.iterdir()) == []


def test_a_scheduler_without_a_durable_path_needs_no_claims(tmp_path):
    scheduler = _RecordingScheduler(manager=None, durable_path=None)
    scheduler.fires = []
    scheduler.schedule("sess", "* * * * *", "ephemeral", durable=False)

    scheduler._tick_once(NOW)

    assert len(scheduler.fires) == 1
    assert scheduler._claims_dir is None
