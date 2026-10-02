"""The double reproduced the provider's shape but not its rules.

Round 42 ended on a stated gap: an attribute scan finds a missing field and
cannot find a missing *behaviour*. Checked against the live endpoint, three
transcript-shape violations are refused there and were accepted here in silence:

    case                          REAL                          FAKE
    unanswered tool_use           400 BadRequestError           accepted
    tool_result with no tool_use  400 BadRequestError           accepted
    tool_result id mismatch       400 BadRequestError           accepted

Those are not incidental rules. Several subsystems exist *only* to keep this
shape legal -- `_close_unanswered_tools` after a cancel or a crash, the
pair-preserving logic in `snip_compact` and `reactive_compact`, the tool-batch
ordering -- and against a double that accepts anything, every one of them could
have been broken with the suite green.

Enforcing it immediately found a real defect. Recovery's continuation appended
a truncated chunk and a "carry on" prompt; when that chunk already contained a
`tool_use`, the result was an unanswered tool call, which the API rejects. The
round-31 test asserting the tool still ran had been passing on a transcript the
real endpoint would have refused.
"""

import asyncio
import contextlib
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import (
    FakeAsyncAnthropic,
    InvalidTranscript,
    text,
    tool,
    validate_transcript,
)
from mini_loop.recovery import DefaultRecovery

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
USER = {"role": "user", "content": "hi"}
CALL = {"role": "assistant", "content": [
    {"type": "tool_use", "id": "t1", "name": "run_bash", "input": {"command": "x"}}]}
RESULT = {"role": "user", "content": [
    {"type": "tool_result", "tool_use_id": "t1", "content": "out"}]}


# --- the rules, as the live endpoint enforces them ------------------------

def test_an_unanswered_tool_use_is_refused():
    with pytest.raises(InvalidTranscript, match="without `tool_result`"):
        validate_transcript([USER, CALL, {"role": "user", "content": "what next?"}])


def test_a_tool_use_at_the_end_is_refused():
    with pytest.raises(InvalidTranscript, match="without `tool_result`"):
        validate_transcript([USER, CALL])


def test_a_tool_result_with_no_tool_use_is_refused():
    with pytest.raises(InvalidTranscript, match="unexpected `tool_result`"):
        validate_transcript([USER, RESULT])


def test_a_mismatched_tool_result_id_is_refused():
    mismatched = {"role": "user", "content": [
        {"type": "tool_result", "tool_use_id": "WRONG", "content": "out"}]}
    # This violates both rules at once -- the call is unanswered *and* the
    # result is unmatched -- so the message depends on which is reported first.
    # The refusal is the contract; the wording is not.
    with pytest.raises(InvalidTranscript):
        validate_transcript([USER, CALL, mismatched])


def test_an_empty_conversation_is_refused():
    with pytest.raises(InvalidTranscript, match="at least one message"):
        validate_transcript([])


def test_a_matched_pair_is_accepted():
    validate_transcript([USER, CALL, RESULT])


def test_an_empty_content_string_is_accepted():
    """The live endpoint accepts it, so the double must not be stricter --
    a double that refuses more than the provider invents failures."""
    validate_transcript([{"role": "user", "content": ""}])


def test_the_client_applies_the_rules_not_just_exposes_them():
    """Through `messages.create`, not by calling the validator.

    The first version of these tests only exercised `validate_transcript`
    directly, so deleting the call from the client left them all passing -- a
    contract nothing was actually held to.
    """
    client = FakeAsyncAnthropic()
    with pytest.raises(InvalidTranscript):
        asyncio.run(client.messages.create(
            model="m", max_tokens=32, messages=[USER, CALL],
        ))


def test_the_streaming_path_applies_them_too():
    client = FakeAsyncAnthropic()

    async def attempt():
        async with client.messages.stream(
            model="m", max_tokens=32, messages=[USER, RESULT]
        ):
            pass

    with pytest.raises(InvalidTranscript):
        asyncio.run(attempt())


def test_a_legal_conversation_still_goes_through():
    client = FakeAsyncAnthropic()
    assert asyncio.run(client.messages.create(
        model="m", max_tokens=32, messages=[USER, CALL, RESULT],
    ))


# --- the defect enforcing it exposed --------------------------------------

def _session(tmp_path, turns):
    state = {"i": 0}

    def responder(request):
        index = state["i"]
        state["i"] += 1
        return turns[index] if index < len(turns) else ([text("done")], "end_turn")

    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(responder=responder),
        recovery=DefaultRecovery(escalate=False),
    ).create()


def test_a_truncated_chunk_holding_a_tool_call_is_not_continued(tmp_path):
    """Continuing it produces an unanswered `tool_use`, which the API refuses.

    The tools are executed instead; continuation is for truncated *text*.
    """
    session = _session(tmp_path, [
        ([tool("run_bash", _id="t1", command="echo one")], "max_tokens"),
        ([text("and done.")], "end_turn"),
    ])
    asyncio.run(session.agent.run("do the thing"))

    results = [
        block for message in session.agent.messages
        if isinstance(message.get("content"), list)
        for block in message["content"]
        if isinstance(block, dict) and block.get("type") == "tool_result"
    ]
    assert results, "the truncated chunk's tool_use never executed"
    validate_transcript(session.agent.messages)


def test_truncated_text_is_still_continued(tmp_path):
    """The fix must not disable continuation wherever it is legitimate."""
    session = _session(tmp_path, [
        ([text("PART-ONE. ")], "max_tokens"),
        ([text("PART-TWO.")], "end_turn"),
    ])
    assert asyncio.run(session.agent.run("write")) == "PART-ONE. PART-TWO."


# --- every transcript the harness builds must survive the rules -----------

@pytest.mark.parametrize("prompt", ["do the thing", "run two commands", "hello"])
def test_a_completed_turn_leaves_a_legal_transcript(tmp_path, prompt):
    session = _session(tmp_path, [])
    asyncio.run(session.agent.run(prompt))
    validate_transcript(session.agent.messages)


def test_a_cancelled_turn_leaves_a_legal_transcript(tmp_path):
    """What `_close_unanswered_tools` exists for, now actually checked."""
    session = _session(tmp_path, [
        ([tool("run_bash", _id="t1", command="sleep")], "tool_use"),
    ])

    async def scenario():
        task = asyncio.create_task(session.run("do the thing"))
        session._running = task
        # Yield once so the turn is genuinely in flight; the offline model is
        # fast enough to finish first otherwise, and the test would validate a
        # completed transcript while claiming to validate a cancelled one.
        await asyncio.sleep(0)
        await session.cancel(reason="stop")
        with contextlib.suppress(asyncio.CancelledError):
            await task

    asyncio.run(scenario())
    assert "[Turn interrupted" in str(session.agent.messages) or session.agent.messages
    validate_transcript(session.agent.messages)


def test_a_compacted_transcript_stays_legal(tmp_path):
    """`snip_compact` keeps pairs together; this is the reason it has to."""
    from mini_loop.compaction import microcompact, snip_compact

    messages = [USER]
    for index in range(10):
        messages.append({"role": "assistant", "content": [
            {"type": "tool_use", "id": f"t{index}", "name": "run_bash",
             "input": {"command": "x"}}]})
        messages.append({"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": f"t{index}", "content": "X" * 400}]})
    validate_transcript(messages)

    snip_compact(messages, max_messages=8)
    validate_transcript(messages)
    microcompact(messages)
    validate_transcript(messages)
