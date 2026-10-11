"""One task, one claimer, however many processes share the board.

`TaskStore.claim` is load -> check -> save under a *thread* lock, and the
module's own docstring invites processes to share a board ("teammates
sharing a workspace share the board"). Two processes interleave those steps
freely: both load pending, both pass every check, last save wins, and two
workers do the same task. Roadmap G7's duplicate-claim risk, the tasks
edition of round 236's cron fix, with the same primitive: an O_EXCL marker
file is the cross-process authority for the claim transition; the record
file stays the human-readable board.

Pinned here:

* a claimer whose view of the board predates another's claim is refused by
  the marker, whatever its stale snapshot says;
* the refusal names the holder, read from the marker;
* a marker whose claim never reached the record (a claimer crashed between
  the two writes) is reported to the operator, never silently seized;
* completion removes the spent marker, and the record's status -- checked
  before the marker is ever consulted -- keeps the removal from reopening
  the task.
"""

from dataclasses import asdict

from mini_loop.tasks import Task, TaskStore


class _StaleReadStore(TaskStore):
    """The read half of a cross-process race, made deterministic: this
    store's view of one task was taken before the other process's claim
    landed, and never refreshed."""

    def __init__(self, root, stale: Task) -> None:
        super().__init__(root)
        self._stale = stale

    def load(self, tid):
        if tid == self._stale.id:
            return Task(**asdict(self._stale))
        return super().load(tid)


def test_a_stale_reader_cannot_claim_a_claimed_task(tmp_path):
    board = TaskStore(tmp_path)
    task = board.create("shared work")
    stale = board.load(task.id)  # the other process's snapshot: pending

    assert "Claimed" in board.claim(task.id, "alice")

    racer = _StaleReadStore(tmp_path, stale)
    verdict = racer.claim(task.id, "bob")

    assert verdict.startswith("Error"), "the stale reader seized the task"
    assert "alice" in verdict, "the refusal should name the holder"
    on_disk = TaskStore(tmp_path).load(task.id)
    assert on_disk.owner == "alice" and on_disk.status == "in_progress"


def test_a_normal_second_claim_is_refused_without_alarm(tmp_path):
    board = TaskStore(tmp_path)
    task = board.create("work")
    board.claim(task.id, "alice")

    other = TaskStore(tmp_path)
    verdict = other.claim(task.id, "bob")

    # The record check answers first here ("in_progress, not claimable");
    # the marker's holder-naming refusal is for stale readers, above.
    assert verdict.startswith("Error")
    # An ordinary already-owned refusal is not an operator problem.
    assert not any("crashed" in p for p in other.problems)


def test_a_crashed_claimers_marker_is_reported_not_seized(tmp_path):
    board = TaskStore(tmp_path)
    task = board.create("work")
    # The crash window: the marker landed, the record update never did.
    board._marker(task.id).write_text("ghost")

    verdict = board.claim(task.id, "bob")

    assert verdict.startswith("Error") and "ghost" in verdict
    assert any("crashed mid-claim" in p for p in board.problems)
    # The record is untouched: the human decides, not the next claimer.
    assert TaskStore(tmp_path).load(task.id).owner is None


def test_completion_removes_the_spent_marker_without_reopening(tmp_path):
    board = TaskStore(tmp_path)
    task = board.create("work")
    board.claim(task.id, "alice")
    assert board._marker(task.id).exists()

    assert "Completed" in board.complete(task.id, "alice")

    assert not board._marker(task.id).exists()
    verdict = TaskStore(tmp_path).claim(task.id, "bob")
    assert "not claimable" in verdict, (
        "removing the marker must not make a completed task claimable"
    )


def test_the_single_process_flow_is_unchanged(tmp_path):
    board = TaskStore(tmp_path)
    first = board.create("first")
    second = board.create("second", blocked_by=[first.id])

    assert "blocked" in board.claim(second.id, "alice")
    assert "Claimed" in board.claim(first.id, "alice")
    done = board.complete(first.id, "alice")
    assert "Completed" in done and second.id in done
    assert "Claimed" in board.claim(second.id, "alice")
