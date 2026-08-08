"""A cancel from outside the harness must not kill the session.

Round 87 serialized concurrent turns. Cancellation is its sibling: a server that
gets two requests for one session also gets requests that go away -- an HTTP
client disconnects, a `wait_for` fires, a supervisor tears down a task.

Cancelling between dispatching a tool and recording its result leaves a
`tool_use` nothing answers, which the provider refuses outright. The session
then carries that shape forward:

    cancel@0.0005  INVALID  next turn -> '[Error] InvalidTranscript...'
    cancel@0.002   INVALID  next turn -> '[Error] InvalidTranscript...'

The repair already existed and was already documented -- `validate_transcript`
names `_close_unanswered_tools` as what runs "after a cancel or a crash". It ran
on `Session.cancel()` and on restore, and a cancellation arriving from *outside*
reached neither. The rule was written down; nothing executed it on this path.

It lives on the agent now, because the invariant belongs to whoever owns the
transcript, and `Session` delegates rather than keeping a second copy.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.actions import UNKNOWN_RESULT
from mini_loop.agent import unanswered_tool_uses
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, validate_transcript

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"

#: Chosen to straddle the dangerous window: the first two land between the
#: tool_use and its result, the last two after the turn has closed it.
CANCEL_POINTS = (0.0005, 0.002, 0.01, 0.05)


def _session(tmp_path, name="ws"):
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / name,
                 memory_root=tmp_path / f"{name}-mem", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
        tool_registry=full_registry(),
    )
    return manager.create()


async def _cancel_after(session, delay) -> bool:
    """Cancel a turn `delay` in. Returns whether the cancel actually landed.

    The later points here are past the end of a fake-LLM turn, so `cancel()` on
    a finished task is a no-op. Asserting CancelledError unconditionally made
    the fixture claim more than it exercised -- the property under test is that
    the session survives, which has to hold whether or not the cancel landed.
    """

    task = asyncio.create_task(session.agent.run("do some work"))
    await asyncio.sleep(delay)
    if task.done():
        await task
        return False
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task
    return True


@pytest.mark.parametrize("delay", CANCEL_POINTS)
@pytest.mark.asyncio
async def test_the_session_still_works_after_an_external_cancel(tmp_path, delay):
    session = _session(tmp_path, f"ws{delay}")
    await _cancel_after(session, delay)

    validate_transcript(session.agent.messages)
    answer = await session.agent.run("carry on")
    assert "[Error]" not in answer
    assert "carry on" in answer


@pytest.mark.asyncio
async def test_a_dangling_tool_is_answered_as_unknown_not_failed(tmp_path):
    """"Failed" would invite a retry of a side effect that may have happened."""

    session = _session(tmp_path)
    landed = [await _cancel_after(session, delay) for delay in (0.0005, 0.002)]
    assert any(landed), "no cancel landed mid-turn; the case is not exercised"

    # `unanswered_tool_uses` inspects only the *tail*, so once any later turn
    # appends a user message it reports clean while the transcript is still
    # malformed in the middle -- an assertion that passes for the wrong reason.
    # The provider checks every position, so ask it instead.
    validate_transcript(session.agent.messages)
    assert UNKNOWN_RESULT in str(session.agent.messages)
    assert "error" not in UNKNOWN_RESULT.lower(), (
        "reporting it as failed invites a retry of a side effect that may "
        "already have happened"
    )


@pytest.mark.asyncio
async def test_the_repair_is_idempotent(tmp_path):
    """`Session.cancel` repairs too; running twice must not append twice."""

    session = _session(tmp_path)
    assert await _cancel_after(session, 0.0005), "the cancel did not land"
    before = len(session.agent.messages)
    assert session.agent.close_unanswered_tools() == []
    assert len(session.agent.messages) == before


@pytest.mark.asyncio
async def test_an_uncancelled_turn_is_not_repaired(tmp_path):
    """Not vacuous: the repair must not fire on an ordinary turn."""

    session = _session(tmp_path)
    await session.agent.run("ordinary")
    assert session.agent.close_unanswered_tools() == []


def test_the_detector_lives_in_one_place():
    """Two copies of a transcript invariant is how one of them goes stale."""

    from mini_loop.session import AgentSession

    assert AgentSession._unanswered_tool_uses(
        [{"role": "assistant", "content": [{"type": "tool_use", "id": "t9"}]}]
    ) == ["t9"]
    source = (pathlib.Path(__file__).resolve().parent.parent
              / "mini_loop" / "session.py").read_text()
    assert source.count("last.get(\"role\")") == 0, "session.py kept its own copy"
