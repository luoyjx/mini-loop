"""`info()['activity']` refines status with waiting-for-confirmation.

Roadmap G5: status is idle/running/error, so a turn blocked on a human
approval looks identical to one actively working. `activity` names the
difference, derived from the broker's live pending list (not a stored
field that could drift). Backward compatible: `status` is unchanged.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path, responder=None, **kw):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None, **kw),
        FakeAsyncAnthropic(responder=responder) if responder else FakeAsyncAnthropic(),
    )


def test_idle_and_running_pass_through(tmp_path):
    session = _manager(tmp_path).create()
    assert session.info()["activity"] == "idle"
    session.status = "running"
    assert session.info()["activity"] == "running"
    session.status = "error"
    assert session.info()["activity"] == "error"


def test_a_pending_approval_shows_awaiting_approval(tmp_path):
    session = _manager(tmp_path).create()
    session.status = "running"
    # Inject a pending approval for this session into the broker.
    broker = session.agent.state["manager"].approvals
    from mini_loop.approvals import PendingApproval

    async def _inject():
        broker._pending["apr_x"] = PendingApproval(
            approval_id="apr_x", session_id=session.id, tool="bash",
            rule="destructive", message="confirm", input_preview="{}",
            created_at=0.0, future=asyncio.get_event_loop().create_future(),
            tool_use_id="t1",
        )
    asyncio.run(_inject())
    assert session.info()["activity"] == "awaiting_approval"
    # Another session is unaffected (scoped by id).
    other = _manager(tmp_path).create()
    other.status = "running"
    assert other.info()["activity"] == "running"


def test_live_stuck_evidence_shows_stuck(tmp_path):
    """A spinning turn reads `stuck`, and recovery reads `running` again.

    Derived from the same detector that will nudge or halt (round 244,
    the second half of the G5 refinement): four identical call/outcome
    pairs are the repeat_action_result pattern, and a nudge clears the
    evidence window -- so the surface tracks the loop's own view, live.
    """

    from mini_loop.stuck import ToolStep

    session = _manager(tmp_path).create()
    session.status = "running"
    step = ToolStep(name="bash", input_hash="i", output_hash="o",
                    failed=False, denied=False)
    session.agent._recent_steps.extend([step] * 4)

    assert session.info()["activity"] == "stuck"
    assert session.info()["status"] == "running"  # coarse field unchanged

    # The nudge path clears the evidence; the surface follows immediately.
    session.agent._recent_steps.clear()
    assert session.info()["activity"] == "running"


def test_awaiting_approval_outranks_stuck(tmp_path):
    """Blocked on a human beats spinning: the human unblocks both."""

    from mini_loop.approvals import PendingApproval
    from mini_loop.stuck import ToolStep

    session = _manager(tmp_path).create()
    session.status = "running"
    step = ToolStep(name="bash", input_hash="i", output_hash="o",
                    failed=False, denied=False)
    session.agent._recent_steps.extend([step] * 4)
    broker = session.agent.state["manager"].approvals

    async def _inject():
        broker._pending["apr_z"] = PendingApproval(
            approval_id="apr_z", session_id=session.id, tool="bash", rule="r",
            message="m", input_preview="{}", created_at=0.0,
            future=asyncio.get_event_loop().create_future(), tool_use_id="t")
    asyncio.run(_inject())

    assert session.info()["activity"] == "awaiting_approval"


def test_status_field_is_unchanged(tmp_path):
    """The refinement must not alter the coarse status any client already reads."""
    session = _manager(tmp_path).create()
    session.status = "running"
    broker = session.agent.state["manager"].approvals
    from mini_loop.approvals import PendingApproval

    async def _inject():
        broker._pending["apr_y"] = PendingApproval(
            approval_id="apr_y", session_id=session.id, tool="bash", rule="r",
            message="m", input_preview="{}", created_at=0.0,
            future=asyncio.get_event_loop().create_future(), tool_use_id="t")
    asyncio.run(_inject())
    info = session.info()
    assert info["status"] == "running"  # coarse field unchanged
    assert info["activity"] == "awaiting_approval"
