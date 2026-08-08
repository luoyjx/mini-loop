"""The count stopped meaning anything again, at the eviction boundary.

Round 51 replaced six unbounded `problems` lists with `ProblemLog`, because a
single recurring fault flooding thousands of identical entries makes a count
stop meaning "how many things are wrong". Fifty-three rounds of shared mutable
state had never been exercised under concurrency, so this round did that -- and
the concurrency was fine. What the probe exposed instead was arithmetic:

    400 appends of 4 distinct problems, limit 3
        total() reports : 3        (should be 400)
        summary         : ['b', 'c', 'd']
        dropped         : 397

`total()` summed `counts`, which only holds *retained* messages, and an evicted
message coming back restarts at one. So a subsystem reporting more distinct
problems than the log retains reported every one of them as having happened
once -- the exact failure the class was written to prevent, reappearing where
the bound is enforced.

`dropped` was misleading in the same way: 397 evictions of four distinct
messages, not 397 problems lost.

An honest negative on the way: a concurrent reader/writer probe showed 421
memories against 352 index lines, which looked like a race and was the
documented `MAX_INDEX` truncation. Checked before claiming.
"""

import threading

import pytest

from mini_loop.problems import ProblemLog


def test_every_occurrence_is_counted_even_after_eviction():
    log = ProblemLog(limit=3)
    for _ in range(100):
        for name in "abcd":
            log.append(name)
    assert log.total() == 400


def test_a_repeated_problem_still_reads_with_its_count():
    log = ProblemLog(limit=50)
    for _ in range(1_440):
        log.append("one dead job")
    assert log.total() == 1_440
    assert log.summary() == ["one dead job (x1440)"]
    assert not log.churning()


def test_churn_is_visible():
    """A log too small for what a subsystem reports must say so, or its
    per-message counts read as facts when they are lower bounds."""
    log = ProblemLog(limit=3)
    for _ in range(100):
        for name in "abcd":
            log.append(name)
    assert log.churning()


def test_a_settled_log_is_not_reported_as_churning():
    log = ProblemLog(limit=50)
    for name in "abc":
        log.append(name)
    assert not log.churning()


def test_clearing_resets_the_occurrence_count():
    log = ProblemLog()
    log.append("x")
    log.clear()
    assert log.total() == 0


# --- the audit tells an operator the counts are lower bounds --------------

def test_the_audit_flags_a_churning_log(tmp_path):
    import pathlib
    from datetime import datetime

    from mini_loop import SessionManager, Settings
    from mini_loop.audit import audit
    from mini_loop.cron import CronJob
    from mini_loop.fake_llm import FakeAsyncAnthropic

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=pathlib.Path(__file__).resolve().parent.parent / "skills"),
        FakeAsyncAnthropic(),
    )
    manager.cron.problems.limit = 2
    for index in range(200):
        manager.cron.problems.append(f"job {index % 5} failed")

    findings = {f.check: f for f in audit(manager, environ={"PATH": "/usr/bin"})}
    detail = findings["cron-problems"].detail
    assert "churning" in detail and "200" in detail, detail


def test_a_healthy_log_is_reported_plainly(tmp_path):
    import pathlib
    from datetime import datetime

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
    for minute in range(5):
        manager.cron.jobs["j1"].last_fired = ""
        manager.cron._tick_once(datetime(2026, 8, 3, 3, minute))

    detail = {f.check: f for f in audit(
        manager, environ={"PATH": "/usr/bin"})}["cron-problems"].detail
    assert "churning" not in detail
    assert "(x5)" in detail


# --- the concurrency that prompted the round -----------------------------

def test_concurrent_appends_leave_a_consistent_log():
    """Six subsystems share one of these across sessions."""
    log = ProblemLog(limit=50)

    def worker():
        for index in range(2_000):
            log.append(f"problem {index % 80}")

    threads = [threading.Thread(target=worker) for _ in range(8)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    assert len(log) == len(log.counts) <= log.limit
    assert log.total() == 8 * 2_000
    assert all(text in log.counts for text in log)


def test_concurrent_memory_writes_and_reads_agree(tmp_path):
    """The other shared object rounds 45-52 added state to."""
    from mini_loop.memory import MemoryStore

    store = MemoryStore(tmp_path / "mem")
    errors: list[str] = []

    def writer(worker):
        try:
            for index in range(60):
                store.write(f"w{worker}-{index}", "project", "d", "b")
        except Exception as error:
            errors.append(f"{type(error).__name__}: {error}")

    def reader():
        try:
            for _ in range(200):
                store.index()
        except Exception as error:
            errors.append(f"{type(error).__name__}: {error}")

    threads = [threading.Thread(target=writer, args=(w,)) for w in range(4)]
    threads += [threading.Thread(target=reader) for _ in range(4)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    assert not errors, errors
    # `MEMORY.md` lives in the same directory and is also a `.md` file.
    written = [p for p in store.dir.glob("*.md") if p.name != "MEMORY.md"]
    assert len(written) == 240
