"""A truncated answer was continued, then thrown away.

`stop_reason == "max_tokens"` means the model ran out of output budget. The
recovery policy handles it: append what came back, ask it to continue, repeat.
Three round-trips were spent finishing a long answer -- and `run()` returned the
final chunk. The earlier parts went into the request history and never reached
the caller. The work was done and discarded.

Underneath it was the representation split this codebase keeps paying for. The
agent normalizes a response's provider *objects* into dicts on the way into the
transcript, and four other places went on reading blocks by attribute. So the
continuation path had to choose which half to break: append raw objects (and put
un-maskable, un-compactable blocks in the transcript) or append dicts (and have
the text extractor read `.text` off a dict and return nothing). It had been
doing the first. Both are now moot -- blocks are read through `_block`.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.agent import _block, _content_payload
from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
from mini_loop.recovery import ContinuedResponse, DefaultRecovery
from mini_loop.storage import SQLiteStateStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _agent(tmp_path, turns, **kwargs):
    state = {"i": 0}

    def responder(request):
        index = state["i"]
        state["i"] += 1
        return turns[index] if index < len(turns) else ([text("done")], "end_turn")

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(responder=responder),
        recovery=DefaultRecovery(escalate=False),
        **kwargs,
    )
    return manager.create()


THREE_PART = [
    ([text("PART-ONE. ")], "max_tokens"),
    ([text("PART-TWO. ")], "max_tokens"),
    ([text("PART-THREE.")], "end_turn"),
]


def test_the_caller_gets_the_whole_answer(tmp_path):
    answer = asyncio.run(_agent(tmp_path, THREE_PART).agent.run("write something long"))
    assert answer == "PART-ONE. PART-TWO. PART-THREE."


def test_the_continued_answer_is_one_assistant_turn(tmp_path):
    """Not three. It was one question and one reply that took three requests."""
    agent = _agent(tmp_path, THREE_PART).agent
    asyncio.run(agent.run("write something long"))
    assistant = [m for m in agent.messages if m["role"] == "assistant"]
    assert len(assistant) == 1


def test_continuation_leaves_no_provider_objects_in_the_transcript(tmp_path):
    """The recurring trap, in the one path that appended around the normalizer."""
    agent = _agent(tmp_path, THREE_PART).agent
    asyncio.run(agent.run("write something long"))
    for message in agent.messages:
        content = message.get("content")
        if isinstance(content, list):
            assert all(isinstance(block, dict) for block in content), (
                f"raw provider block in the transcript: {content}"
            )


def test_a_tool_call_in_a_truncated_chunk_still_runs(tmp_path):
    """The chunk that got cut off may already have asked for a tool.

    Dropping the earlier chunks dropped its tool calls with them.
    """
    session = _agent(tmp_path, [
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


def test_a_continued_turn_persists_and_restores(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    session = _agent(tmp_path, THREE_PART, state_store=store)
    asyncio.run(session.agent.run("write something long"))
    session._flush_messages()
    restored = store.load_messages(session.id, epoch=store.transcript_epoch(session.id))
    joined = str(restored)
    assert all(part in joined for part in ("PART-ONE", "PART-TWO", "PART-THREE"))
    store.close()


def test_healthy_calls_are_untouched(tmp_path):
    """The module promises to change nothing when no error occurs."""
    session = _agent(tmp_path, [([text("just one answer")], "end_turn")])
    assert asyncio.run(session.agent.run("hi")) == "just one answer"


# --- the accessor that removes the split ----------------------------------

@pytest.mark.parametrize("shape", ["object", "dict"])
def test_blocks_read_the_same_in_either_shape(shape):
    block = tool("run_bash", _id="t1", command="echo hi")
    if shape == "dict":
        block = _content_payload([block])[0]
    assert _block(block, "type") == "tool_use"
    assert _block(block, "name") == "run_bash"
    assert _block(block, "id") == "t1"
    assert _block(block, "input") == {"command": "echo hi"}
    assert _block(block, "missing", "fallback") == "fallback"


def test_the_continued_response_stands_in_for_a_real_one(tmp_path):
    """It is handed to the agent as a response, so it must carry what one does.

    `tests/test_provider_fidelity.py` pins the same property for the offline
    model; this is the other object the agent reads a response off.
    """
    from mini_loop.fake_llm import FakeMessage, FakeUsage

    final = FakeMessage([text("end")], "end_turn", usage=FakeUsage(1234))
    combined = ContinuedResponse([[{"type": "text", "text": "start"}]], final)
    assert [b["text"] for b in combined.content] == ["start"]
    assert combined.stop_reason == "end_turn"
    assert combined.usage.input_tokens == 1234
    for field in ("id", "model", "role", "type", "stop_sequence"):
        assert hasattr(combined, field), field


def test_the_meter_still_sees_usage_after_a_continuation(tmp_path):
    """Metering reads `response.usage`; a wrapper that dropped it would make
    the context budget silently stop updating on exactly the longest turns."""
    agent = _agent(tmp_path, THREE_PART).agent
    asyncio.run(agent.run("write something long"))
    assert agent.token_meter.observations > 0
    assert agent.token_meter.calibrated
