"""A message to a name nobody consumes is refused, not silently lost.

`send_message(to=...)` builds a team key from `to` and writes to that inbox.
Nothing checked that `to` names a real participant, so a typo or a teammate
that had shut down sent the message into a limbo inbox nobody drains -- and the
sender was told "Sent message to bob". OpenWorker flags exactly this as its
unrouted-message hazard (research doc 6.4), and it is the same shape round 50
fixed for an oversized broadcast that reported success while delivering
nothing: a confirmed delivery that never happened is worse than an error.

`broadcast` already iterates the roster, so only the direct send was exposed.
The manager knows the roster (`teammates_of`), and the lead is always addressed
as "lead" (its agent_name is pinned), so the valid set is exact and the check
has no false positives.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.registry import ToolCall, ToolContext
from mini_loop.teams import team_key

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


async def _team(tmp_path):
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(), tool_registry=full_registry(), enable_features=True,
    )
    lead = manager.create()
    await lead.run("hi")
    await manager.spawn_teammate(lead.id, "alice", "worker", "do work")
    return manager, lead


def _send_handler(agent):
    ctx = ToolContext(agent, agent.workspace, agent.state,
                      ToolCall("send_message", {}, "x"))
    return ctx, agent.tools.get("send_message").handler


@pytest.mark.asyncio
async def test_a_message_to_a_nonexistent_teammate_is_refused(tmp_path):
    manager, lead = await _team(tmp_path)
    ctx, send = _send_handler(lead.agent)

    result = await send(ctx, to="bob_typo", content="hello")

    assert result.startswith("Error:")
    assert "bob_typo" in result
    # And nothing was written to the limbo inbox.
    tid = lead.agent.state["team_id"]
    assert manager.bus.read(team_key(tid, "bob_typo")) == []


@pytest.mark.asyncio
async def test_the_error_lists_the_real_roster(tmp_path):
    """So the agent can correct itself instead of guessing."""

    manager, lead = await _team(tmp_path)
    ctx, send = _send_handler(lead.agent)

    result = await send(ctx, to="ghost", content="hi")
    assert "alice" in result and "lead" in result


@pytest.mark.asyncio
async def test_a_real_teammate_still_receives(tmp_path):
    """Not a wall: the valid path is untouched, and the message lands."""

    manager, lead = await _team(tmp_path)
    ctx, send = _send_handler(lead.agent)

    result = await send(ctx, to="alice", content="ping alice")
    assert result.startswith("Sent")

    tid = lead.agent.state["team_id"]
    inbox = manager.bus.read(team_key(tid, "alice"))
    assert any(m["content"] == "ping alice" for m in inbox)


@pytest.mark.asyncio
async def test_the_lead_is_always_a_valid_recipient(tmp_path):
    """Teammates report to 'lead'; the manager pins that name, so it must
    never be flagged as unrouted."""

    manager, lead = await _team(tmp_path)
    alice = manager.teammate_session(lead.agent.state["team_id"], "alice")
    ctx, send = _send_handler(alice.agent)

    result = await send(ctx, to="lead", content="status update")
    assert result.startswith("Sent")


@pytest.mark.asyncio
async def test_without_a_manager_the_check_is_skipped(tmp_path):
    """A bare bus (no manager in state) has no roster to check against; the
    pre-existing behaviour stands rather than refusing every message."""

    manager, lead = await _team(tmp_path)
    agent = lead.agent
    ctx = ToolContext(agent, agent.workspace, dict(agent.state),
                      ToolCall("send_message", {}, "x"))
    ctx.state.pop("manager", None)
    send = agent.tools.get("send_message").handler

    result = await send(ctx, to="anyone", content="hi")
    assert result.startswith("Sent")
