"""A scheduled prompt fires with untrusted authority, never the human's.

Cron jobs are durable and fire unattended -- they survive a restart and run with
no human present. Workflow launch and manage require EXPLICIT_HUMAN authority
(`workflows/service.py`, `workflows/tools.py`), so a cron-fired turn carrying
human authority would let a model schedule one turn that escalates to launching
workflows on every later firing, forever, after the human who might have stopped
it is gone.

`_fire` runs the prompt through `session.run` with `RunContext.default()`
(untrusted). The property held on an *implicit* default before -- `session.run`
falls back to `RunContext.default()` when given no context -- so nothing at the
firing site stated it and nothing tested it, and a well-meaning edit handing cron
"the scheduler's authority" would have silently enabled the escalation. This pins
the authority a cron-fired turn actually runs under, and the non-vacuity case
shows the recorder can read human authority when it is genuinely present, so
"untrusted" is a real downgrade rather than the only value it ever sees.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.cron import CronJob, CronScheduler
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.registry import Tool
from mini_loop.run_context import EXPLICIT_HUMAN, RunContext

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager_recording(tmp_path):
    """A manager whose one custom tool records the authority of its run."""
    seen: list = []

    async def record_authority(ctx, **_kw):
        rc = ctx.run_context
        seen.append(rc.authority if rc else None)
        return f"authority={seen[-1]}"

    registry = full_registry()
    registry.register(
        Tool("record_authority", "records the run authority",
             {"type": "object"}, record_authority, risk="read", readonly=True)
    )
    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("record_authority", _id="r1")], "tool_use"),
        ([text("done")], "end_turn"),
    ]))
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        client, tool_registry=registry,
    )
    return manager, seen


@pytest.mark.asyncio
async def test_a_cron_fired_turn_runs_untrusted_not_as_the_human(tmp_path):
    manager, seen = _manager_recording(tmp_path)
    session = manager.create()

    scheduler = CronScheduler(manager)
    job = CronJob(id="j1", cron="* * * * *", prompt="call record_authority",
                  session_id=session.id, recurring=True, durable=False)
    scheduler._fire(job)
    await asyncio.gather(*scheduler._running, return_exceptions=True)

    assert seen, "the cron-fired turn never reached the tool"
    assert seen[0] != EXPLICIT_HUMAN, (
        "a cron-fired turn ran with human authority -- it could launch a workflow"
    )


@pytest.mark.asyncio
async def test_the_recorder_sees_human_authority_when_it_is_really_present(tmp_path):
    """Non-vacuity: the same recorder reads EXPLICIT_HUMAN from a genuinely human
    turn, so "untrusted" above is a downgrade, not the only value it can see."""
    manager, seen = _manager_recording(tmp_path)
    session = manager.create()

    await session.run(
        "call record_authority",
        run_context=RunContext.explicit_human(actor_id="alice"),
    )

    assert seen and seen[0] == EXPLICIT_HUMAN
