"""The workflow service's failure path, which had no test at all.

Coverage-guided defect hunting found real bugs in rounds 55 (`teams.broadcast`
discarding refusals) and 56 (`worktrees.remove` forcing unconditionally), so
this round pointed it at the largest remaining gap: the workflows package, 165
uncovered statements and never examined in sixty-eight rounds. The biggest
single block was `WorkflowService`'s `except Exception` -- 38 lines deciding
what happens when the engine raises mid-run.

**It is correct**, which is the honest result. With the engine made to raise:

    engine.execute calls : 1
    final status         : RunStatus.FAILED
    is_terminal          : True
    error recorded       : 'RuntimeError: engine exploded'
    notifications queued : 3

The run reaches a terminal state, the error is recorded verbatim rather than
flattened, and the launching session is told. That is three of the four
questions from round 49's checklist answered correctly by code nobody had run.

Uncovered is not the same as defective. What it is, is unpinned: an error path
this size with no test can regress into silence and nothing would notice, which
is what the rest of this file is for.
"""

import asyncio
import pathlib
import sys

import pytest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

from mini_loop import SessionManager, Settings          # noqa: E402
from mini_loop.fake_llm import FakeAsyncAnthropic       # noqa: E402
from mini_loop.run_context import RunContext            # noqa: E402
from mini_loop.workflows.models import RunStatus        # noqa: E402
from test_workflow_integration import _single_node_definition  # noqa: E402

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
        enable_workflows=True,
    )


def _context():
    return RunContext.explicit_human(
        actor_id="local-user", approved_capabilities=("workflow.launch",)
    )


async def _launch_with_failing_engine(manager, session, error):
    service = manager.workflows
    original = service.engine.execute
    calls = {"n": 0}

    async def exploding(run_id):
        calls["n"] += 1
        if calls["n"] == 1:
            raise error
        return await original(run_id)

    service.engine.execute = exploding
    launched = await service.launch(
        session_id=session.id,
        definition=_single_node_definition(wall_time_seconds=10),
        args={}, run_context=_context(), action_id="failure-probe",
        action_input={}, tool_use_id="t1",
    )
    run = getattr(launched, "run", launched)
    await asyncio.sleep(0.2)
    return service, service.store.get_run(run.run_id), calls


def _run_scenario(tmp_path, error=None):
    manager = _manager(tmp_path)
    session = manager.create()

    async def scenario():
        service, run, calls = await _launch_with_failing_engine(
            manager, session, error or RuntimeError("engine exploded")
        )
        notifications = service.prepare_notifications(
            session_id=session.id, parent_turn=1
        )
        await manager.stop()
        return run, notifications, calls

    return asyncio.run(scenario())


# --- the path reaches a terminal state -----------------------------------

def test_an_engine_failure_ends_the_run(tmp_path):
    run, _, _ = _run_scenario(tmp_path)
    assert run.status == RunStatus.FAILED
    assert run.is_terminal, "a failed run that is not terminal is a run nobody reaps"


def test_the_error_is_recorded_verbatim(tmp_path):
    """Not flattened to "workflow failed" -- the type and message are what an
    operator has to work from."""
    run, _, _ = _run_scenario(tmp_path, RuntimeError("disk went away"))
    assert "RuntimeError" in (run.error or "")
    assert "disk went away" in (run.error or "")


def test_the_launching_session_is_told(tmp_path):
    """A workflow that fails silently is indistinguishable from one still
    running -- the failure mode this whole document is about."""
    _, notifications, _ = _run_scenario(tmp_path)
    assert notifications, "the session was never notified the run failed"
    assert "engine exploded" in str(notifications)


def test_the_notification_carries_the_status(tmp_path):
    _, notifications, _ = _run_scenario(tmp_path)
    assert "FAILED" in str(notifications)


@pytest.mark.parametrize("error", [
    RuntimeError("engine exploded"),
    ValueError("bad node definition"),
    OSError("disk went away"),
    KeyError("missing"),
])
def test_any_exception_type_ends_the_run(tmp_path, error):
    """The handler catches `Exception`; every subclass must reach a terminal
    state, not only the one that happened to be tried."""
    run, notifications, _ = _run_scenario(tmp_path, error)
    assert run.is_terminal
    assert run.status == RunStatus.FAILED
    assert notifications


def test_the_error_distinguishes_a_node_failure_from_an_engine_failure(tmp_path):
    """The control, and a better one than intended.

    This was written as "a healthy run is not marked failed" and the run failed
    anyway -- with `workflow node slow did not call return_artifact`, because the
    offline model does not produce the artifact the node requires. That is a
    fixture limitation rather than a harness defect, and it makes the sharper
    point: the `error` field carries *which* thing went wrong, not a generic
    label. An engine-level explosion and a node-level contract violation are
    both `FAILED` and are told apart by their message.
    """
    manager = _manager(tmp_path)
    session = manager.create()

    async def scenario():
        launched = await manager.workflows.launch(
            session_id=session.id,
            definition=_single_node_definition(wall_time_seconds=10),
            args={}, run_context=_context(), action_id="healthy",
            action_input={}, tool_use_id="t1",
        )
        run = getattr(launched, "run", launched)
        await asyncio.sleep(0.2)
        final = manager.workflows.store.get_run(run.run_id)
        await manager.stop()
        return final

    final = asyncio.run(scenario())
    node_failure = final.error or ""
    assert "workflow node" in node_failure, node_failure
    assert "engine exploded" not in node_failure

    injected, _, _ = _run_scenario(tmp_path, RuntimeError("engine exploded"))
    assert "engine exploded" in (injected.error or "")
    assert "workflow node" not in (injected.error or "")
