"""Two turns on one session must not interleave into one transcript.

Nearly every test here drives one session, one turn at a time, so nothing ever
asked what a *server* does: an ordinary double-submit or an SSE reconnect gives
one session two concurrent `run()` calls.

`self.messages` is a single mutable list. Two turns appending to it produce a
shape the provider refuses -- a `tool_use` block with somebody else's user
message where its `tool_result` belongs. Measured on four concurrent calls:

    provider requests            5
    rejected: InvalidTranscript  4
    distinct answers             1 of 4   (every caller got the same error)
    final transcript             permanently malformed

The last line is the one that makes it more than a failed request: the session
carries the broken shape forward, so later turns degrade too.

Turns are serialized now. Queued rather than refused -- a second request is
almost always something the user meant to ask -- and the wait is reported, so a
caller blocked behind a long turn is not left guessing.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, InvalidTranscript, validate_transcript

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path, client=None):
    return SessionManager(
        Settings(
            fake_llm=True,
            workspace_root=tmp_path / "ws",
            memory_root=tmp_path / "mem",
            skills_dir=SKILLS,
        ),
        client or FakeAsyncAnthropic(),
        tool_registry=full_registry(),
    )


@pytest.mark.asyncio
async def test_concurrent_turns_each_get_their_own_answer(tmp_path):
    session = _manager(tmp_path).create()

    answers = await asyncio.gather(
        *(session.agent.run(f"UNIQUE-{i}") for i in range(4))
    )

    assert len(set(answers)) == 4, f"callers got each other's replies: {answers}"
    for i, answer in enumerate(answers):
        assert f"UNIQUE-{i}" in answer
        assert "[Error]" not in answer


@pytest.mark.asyncio
async def test_the_transcript_survives_concurrent_turns(tmp_path):
    """The shape the provider requires, after the burst rather than during it."""

    session = _manager(tmp_path).create()
    await asyncio.gather(*(session.agent.run(f"req {i}") for i in range(4)))

    validate_transcript(session.agent.messages)


@pytest.mark.asyncio
async def test_no_request_is_sent_mid_mutation(tmp_path):
    """Validated synchronously inside the call, so nothing can interleave.

    Checking after the fact races: the message dicts are shared and keep
    mutating, so both a shallow copy and a deep copy measure a different moment
    than the one the provider saw. asyncio is single-threaded, so a synchronous
    check with no await before it is the one that cannot lie.
    """

    client = FakeAsyncAnthropic()
    verdicts: list[bool] = []
    original = client.messages.create

    async def spy(**kwargs):
        try:
            validate_transcript(kwargs.get("messages"))
            verdicts.append(True)
        except InvalidTranscript:
            verdicts.append(False)
        return await original(**kwargs)

    client.messages.create = spy
    session = _manager(tmp_path, client).create()
    await asyncio.gather(*(session.agent.run(f"req {i}") for i in range(4)))

    assert verdicts, "no request was observed"
    assert all(verdicts), f"{verdicts.count(False)}/{len(verdicts)} sent malformed"


@pytest.mark.asyncio
async def test_a_queued_turn_is_reported(tmp_path):
    """Serializing silently would leave a blocked caller guessing."""

    session = _manager(tmp_path).create()
    seen: list[str] = []
    original = session.agent._send

    async def spy(kind, **fields):
        seen.append(kind)
        return await original(kind, **fields)

    session.agent._send = spy
    await asyncio.gather(*(session.agent.run(f"req {i}") for i in range(3)))

    assert "turn_queued" in seen


@pytest.mark.asyncio
async def test_a_lone_turn_is_not_reported_as_queued(tmp_path):
    """Not vacuous: the event must mean something."""

    session = _manager(tmp_path).create()
    seen: list[str] = []
    original = session.agent._send

    async def spy(kind, **fields):
        seen.append(kind)
        return await original(kind, **fields)

    session.agent._send = spy
    await session.agent.run("just one")

    assert "turn_queued" not in seen


@pytest.mark.asyncio
async def test_separate_sessions_still_run_in_parallel(tmp_path):
    """The lock is per agent. Serializing a whole server would be a bad trade."""

    manager = _manager(tmp_path)
    sessions = [manager.create() for _ in range(6)]

    answers = await asyncio.gather(
        *(s.agent.run(f"session-{i}") for i, s in enumerate(sessions))
    )
    assert len(set(answers)) == 6
    for i, session in enumerate(sessions):
        blob = str(session.agent.messages)
        assert all(f"session-{j}" not in blob for j in range(6) if j != i)
