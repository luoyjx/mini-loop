"""One session could cancel another's scheduled work.

Round 78 ended on a component being made wrong by features added around it, and
the manager shares five objects across every session it creates. `CronScheduler`
was the next one: `list_for(session_id)` filters, and `cancel(job_id)` took a
bare id.

    alice list_for : 94813e59 -> alice's nightly report
    bob   list_for : fa9587c5 -> bob's nightly report
    bob cancels alice's job 94813e59 -> 'Cancelled cron 94813e59'
    alice's job gone: True

That is the same "filtered index, unprotected direct reference" as the
trajectory fetch (round 74) and the trajectory listing (round 76) -- the third
occurrence, on a third object.

Not equally exploitable: a job id is a random 8 hex characters and the listing
that shows it is filtered, so an attacker needs the id from somewhere else --
an event, a log, an error. That is a reason to grade it lower, not a reason to
leave it: ids leak, and a scheduled job silently not running is exactly the
failure round 47 spent a round making visible.

`session_id=None` keeps the unscoped call for whoever owns the scheduler
outright. The *tool* always passes its own session, so an agent can only cancel
its own.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


class Context:
    def __init__(self, session, manager):
        self.state = {"session_id": session.id, "manager": manager,
                      "cron": manager.cron}
        self.agent = session.agent
        self.workspace = session.agent.workspace


@pytest.fixture
def tenants(tmp_path):
    """Both sessions own a job. With only one scheduled, "no such job" and
    "not yours" are the same observation -- round 77."""
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
        tool_registry=full_registry(),
    )
    alice, bob = manager.create(), manager.create()
    manager.cron.schedule(alice.id, "0 3 * * *", "alice's nightly report")
    manager.cron.schedule(bob.id, "0 4 * * *", "bob's nightly report")
    jobs = {
        "alice": next(j for j in manager.cron.jobs.values() if j.session_id == alice.id),
        "bob": next(j for j in manager.cron.jobs.values() if j.session_id == bob.id),
    }
    return manager, {"alice": alice, "bob": bob}, jobs


def _cancel(manager, session, job_id):
    tool = session.agent.tools.get("cancel_cron")
    return asyncio.run(tool.handler(Context(session, manager), job_id=job_id))


def test_the_fixture_scheduled_both(tenants):
    manager, _, jobs = tenants
    assert jobs["alice"].id != jobs["bob"].id
    assert len(manager.cron.jobs) == 2


def test_a_session_cannot_cancel_another_sessions_job(tenants):
    manager, sessions, jobs = tenants
    result = _cancel(manager, sessions["bob"], jobs["alice"].id)

    assert "Cancelled" not in result
    assert jobs["alice"].id in manager.cron.jobs, "alice's job was cancelled by bob"


def test_a_session_can_cancel_its_own(tenants):
    """Scoping that stops the owner is not scoping."""
    manager, sessions, jobs = tenants
    assert "Cancelled" in _cancel(manager, sessions["alice"], jobs["alice"].id)
    assert jobs["alice"].id not in manager.cron.jobs


def test_a_refusal_is_indistinguishable_from_a_missing_job(tenants):
    """Round 24's rule: a different answer confirms the id exists."""
    manager, sessions, jobs = tenants
    someone_elses = _cancel(manager, sessions["bob"], jobs["alice"].id)
    never_existed = _cancel(manager, sessions["bob"], "deadbeef")
    assert someone_elses.replace(jobs["alice"].id, "X") == \
        never_existed.replace("deadbeef", "X")


def test_the_listing_was_already_filtered(tenants):
    """It always was; the risk is scoping the cancel and breaking this."""
    manager, sessions, jobs = tenants
    listing = manager.cron.list_for(sessions["bob"].id)
    assert jobs["bob"].id in listing
    assert jobs["alice"].id not in listing


def test_an_operator_holding_the_scheduler_can_still_cancel(tenants):
    """`session_id=None` is the unscoped call, for whoever owns the scheduler."""
    manager, _, jobs = tenants
    assert "Cancelled" in manager.cron.cancel(jobs["alice"].id)


def test_cancelling_removes_it_from_the_durable_file(tenants, tmp_path):
    manager, sessions, jobs = tenants
    _cancel(manager, sessions["alice"], jobs["alice"].id)
    reloaded = type(manager.cron)(manager, durable_path=manager.cron.durable_path)
    assert jobs["alice"].id not in reloaded.jobs
    assert jobs["bob"].id in reloaded.jobs


# --- what the same scan said about the other shared objects --------------

def test_the_other_shared_objects_are_scoped_where_they_are_reachable():
    """Recorded as a negative rather than left implicit.

    A scan of every manager-shared object for "takes an id, takes no owner"
    also flagged `TrajectoryStore.get/raw/summary`, `TaskStore.load/can_start`
    and `MessageBus.read`. Each is scoped at the boundary where a caller can
    actually reach it -- the HTTP layer for trajectories (rounds 74-76), the
    workspace for tasks, and `_self_key(ctx)` for mailboxes -- so the store
    being unscoped is the design rather than a gap.
    """
    import inspect

    from mini_loop.teams import install_teams
    from mini_loop.registry import ToolRegistry

    registry = install_teams(ToolRegistry())
    source = inspect.getsource(registry.get("read_inbox").handler)
    assert "_self_key(ctx)" in source, (
        "read_inbox derives its mailbox from the caller's own identity; if that "
        "changes, an agent could name any mailbox"
    )
