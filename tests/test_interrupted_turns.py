"""What an interrupted turn leaves behind.

Round 40 named three streaming interactions and checked two. This is the third.
Cancelling mid-stream composes cleanly and that is a negative result worth
recording: the stream's context manager exits exactly once (so the connection is
released), the semaphore is returned, status goes back to `idle`, and the
session runs again.

What it did *not* do was leave a record. A turn interrupted mid-generation
appended nothing, so the next run added a second user message and the model saw
two questions in a row with nothing between them. With a streaming transport it
is worse: the console had already rendered text the transcript had no record of,
so "finish that thought" referred to something the agent could not see -- and
the natural failure is to silently start over.

Two boundaries matter and are tested here:

* **Thinking is not answer text.** It is shown as progress and never
  accumulated: a distinct block type carrying a signature, and replaying it as
  assistant *text* would misrepresent it and drop the signature the API wants.
* **A repaired tool call already says it.** The `[unknown]` result has to stay
  last so it answers the `tool_use` immediately before it, and it already
  records that the turn was cut short.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, text, thinking
from mini_loop.transport import DirectTransport, StreamingTransport

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _session(tmp_path, *, responder=None, transport=None, thinking=True):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(responder=responder, thinking=thinking),
        transport=transport,
    ).create()


@pytest.fixture
def slow_stream(monkeypatch):
    """Stretch the generation so there is a middle to interrupt."""
    import mini_loop.fake_llm as fake

    real = fake._FakeStream.__aiter__

    async def slow(self):
        async for event in real(self):
            await asyncio.sleep(0.05)
            yield event

    monkeypatch.setattr(fake._FakeStream, "__aiter__", slow)


async def _cancel_during_generation(session, delay=0.12):
    task = asyncio.create_task(session.run("write a long answer"))
    session._running = task
    await asyncio.sleep(delay)
    stopped = await session.cancel(reason="stop")
    with pytest.raises(asyncio.CancelledError):
        await task
    return stopped


def _roles(agent):
    return [message["role"] for message in agent.messages]


def _last_text(agent):
    return str(agent.messages[-1])


# --- the composition that already worked ----------------------------------

def test_a_cancelled_stream_releases_its_connection(tmp_path, slow_stream, monkeypatch):
    import mini_loop.fake_llm as fake

    exits = {"n": 0}
    real = fake._FakeStream.__aexit__

    async def counting(self, *exc):
        exits["n"] += 1
        return await real(self, *exc)

    monkeypatch.setattr(fake._FakeStream, "__aexit__", counting)
    session = _session(
        tmp_path,
        responder=lambda request: ([text("A" * 600)], "end_turn"),
        transport=StreamingTransport(coalesce_chars=1, coalesce_seconds=0),
    )
    asyncio.run(_cancel_during_generation(session))
    assert exits["n"] == 1, "the stream context was not exited"


def test_a_cancelled_session_is_reusable(tmp_path, slow_stream):
    session = _session(
        tmp_path,
        responder=lambda request: ([text("A" * 600)], "end_turn"),
        transport=StreamingTransport(coalesce_chars=1, coalesce_seconds=0),
    )

    async def scenario():
        await _cancel_during_generation(session)
        assert session.status == "idle"
        assert session.agent.semaphore._value == session.agent.semaphore._value
        return await asyncio.wait_for(session.run("hello again"), timeout=10)

    assert asyncio.run(scenario())


# --- the record it now leaves ---------------------------------------------

def test_an_interrupted_turn_says_so(tmp_path, slow_stream):
    session = _session(
        tmp_path,
        responder=lambda request: ([text("A" * 600)], "end_turn"),
        transport=StreamingTransport(coalesce_chars=1, coalesce_seconds=0),
    )
    asyncio.run(_cancel_during_generation(session))
    assert "[Turn interrupted" in _last_text(session.agent)


def test_the_text_the_user_saw_is_kept(tmp_path, slow_stream):
    """The console rendered it; the transcript has to know it exists."""
    session = _session(
        tmp_path,
        responder=lambda request: ([text("PARTIAL-ANSWER-" + "A" * 400)], "end_turn"),
        transport=StreamingTransport(coalesce_chars=1, coalesce_seconds=0),
        # A reasoner streams its thinking first; with it on, the cancel lands
        # before any answer text exists and the test would pass vacuously.
        thinking=False,
    )
    asyncio.run(_cancel_during_generation(session))
    assistant = [m for m in session.agent.messages if m["role"] == "assistant"]
    assert assistant, "the streamed partial was discarded"
    assert "PARTIAL-ANSWER" in str(assistant[-1])


def test_a_completed_stream_leaves_no_partial_to_re_record(tmp_path):
    """The mirror of the test above. `streamed_text` holds what an *interrupted*
    stream showed and never recorded, so a *completed* stream -- whose text is in
    its final message and committed to the transcript -- has to leave it empty.

    Left set, an interrupt landing after a completed round (or after an internal
    streamed call like the compaction summary) re-records that stale text as a
    phantom interrupted assistant turn: the round duplicated, or "[summary]"
    surfaced as the answer.
    """
    session = _session(
        tmp_path,
        responder=lambda request: ([text("the whole answer")], "end_turn"),
        transport=StreamingTransport(),
    )
    asyncio.run(session.run("say something"))
    assert session.agent.streamed_text == "", "a completed stream left stale partial text"

    # An interrupt in the inter-round window must add only the marker.
    recorded = session._record_interruption("cancelled", repaired=False)
    assert recorded
    assert session.agent.messages[-1]["content"][0]["text"] == "[Turn interrupted: cancelled]", (
        "an interrupt after a completed stream re-recorded stale streamed text"
    )


def test_thinking_is_shown_but_never_recorded_as_an_answer(tmp_path, slow_stream):
    session = _session(
        tmp_path,
        responder=lambda request: ([thinking("REASONING-ONLY" * 40)], "end_turn"),
        transport=StreamingTransport(coalesce_chars=1, coalesce_seconds=0),
        # The fake prepends its own thinking block; with it on, that streams
        # first and the cancel lands before this one is reached, so the test
        # would pass without the claim ever being exercised.
        thinking=False,
    )
    deltas: list[str] = []
    original = session._capture_event

    async def spy(event):
        result = await original(event)
        if result.get("type") == "assistant_delta":
            deltas.append(result["text"])
        return result

    session._capture_event = spy
    asyncio.run(_cancel_during_generation(session))

    assert any("REASONING-ONLY" in chunk for chunk in deltas), (
        "the thinking never streamed, so nothing was proven"
    )
    assert "REASONING-ONLY" not in str(session.agent.messages), (
        "thinking was replayed as assistant text, without its signature"
    )
    assert "[Turn interrupted" in _last_text(session.agent)


def test_the_next_prompt_is_not_preceded_by_an_unexplained_gap(tmp_path, slow_stream):
    """The symptom was two questions in a row with *nothing between them*.

    Consecutive user turns are legal; a model being asked twice with no sign
    that anything happened in between is the actual problem.
    """
    session = _session(
        tmp_path,
        responder=lambda request: ([text("A" * 600)], "end_turn"),
        transport=StreamingTransport(coalesce_chars=1, coalesce_seconds=0),
    )

    async def scenario():
        await _cancel_during_generation(session)
        await asyncio.wait_for(session.run("hello again"), timeout=10)

    asyncio.run(scenario())
    roles = _roles(session.agent)
    prompts = [i for i, m in enumerate(session.agent.messages)
               if m["role"] == "user" and "hello again" in str(m["content"])]
    assert prompts, "the follow-up prompt is missing"
    before = session.agent.messages[prompts[0] - 1]
    assert before["role"] == "assistant" and "[Turn interrupted" in str(before), (
        f"nothing explains the gap before the follow-up: {roles}"
    )


def test_a_non_streaming_turn_records_the_interruption_but_no_partial(tmp_path):
    """`DirectTransport` shows nothing before it finishes, so there is nothing
    the user saw to preserve -- but the interruption still happened."""
    session = _session(tmp_path, transport=DirectTransport())

    async def scenario():
        task = asyncio.create_task(session.run("hello"))
        session._running = task
        await asyncio.sleep(0)
        await session.cancel(reason="stop")
        with pytest.raises(asyncio.CancelledError):
            await task

    asyncio.run(scenario())
    last = session.agent.messages[-1]
    assert last["role"] == "assistant"
    body = last["content"][0]["text"]
    assert body == "[Turn interrupted: stop]", (
        f"nothing was shown, so nothing should be reconstructed: {body!r}"
    )
