"""Streaming, and the ceiling it removes.

Round 38 found recovery escalating `max_tokens` to 64,000 on a non-streaming
call, which the SDK refuses before sending -- turning a recoverable truncation
into a failed turn. The cap added there is a workaround for a missing
capability. Against the real endpoint:

    non-streaming, max_tokens=64,000 -> ValueError: Streaming is required...
    streaming,     max_tokens=64,000 -> accepted, 10 deltas, first at 0.66s

`get_final_message()` returns the same `Message` shape as `create()`, so
normalization, the token meter and recovery's continuation are untouched. A real
session confirms it: transcript intact, meter calibrated, and the answer whole.

The part that needed design is the deltas. This harness persists every event and
fans it to every SSE subscriber, so one event per token would mean thousands of
fragment rows per turn in the durable log. They are coalesced and marked
`_ephemeral`: live consoles see progress, the record keeps what happened.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
from mini_loop.recovery import DefaultRecovery, ESCALATED_MAX_TOKENS
from mini_loop.storage import SQLiteStateStore
from mini_loop.transport import (
    DELTA_COALESCE_CHARS,
    DirectTransport,
    StreamingTransport,
)

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _session(tmp_path, *, transport=None, responder=None, **kwargs):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(responder=responder) if responder else FakeAsyncAnthropic(),
        transport=transport,
        **kwargs,
    ).create()


def _deltas(session):
    seen: list[str] = []
    original = session._capture_event

    async def spy(event):
        if event.get("type") == "assistant_delta":
            seen.append(event["text"])
        return await original(event)

    session._capture_event = spy
    return seen


# --- the transport is a seam, and the default is unchanged ----------------

def test_the_default_transport_is_still_one_shot(tmp_path):
    assert isinstance(_session(tmp_path).agent.transport, DirectTransport)
    assert _session(tmp_path).agent.transport.streaming is False


def test_a_streamed_turn_produces_the_same_answer(tmp_path):
    direct = asyncio.run(_session(tmp_path).agent.run("hello"))
    streamed = asyncio.run(
        _session(tmp_path, transport=StreamingTransport()).agent.run("hello")
    )
    assert streamed == direct


def test_a_streamed_turn_still_executes_tools(tmp_path):
    session = _session(tmp_path, transport=StreamingTransport())
    asyncio.run(session.agent.run("do the thing"))
    results = [
        block for message in session.agent.messages
        if isinstance(message.get("content"), list)
        for block in message["content"]
        if isinstance(block, dict) and block.get("type") == "tool_result"
    ]
    assert results, "tool_use in a streamed response never ran"


def test_the_token_meter_still_sees_usage(tmp_path):
    """`get_final_message()` carries usage; a transport that dropped it would
    make the context budget silently stop updating."""
    session = _session(tmp_path, transport=StreamingTransport())
    asyncio.run(session.agent.run("hello"))
    assert session.agent.token_meter.calibrated


def test_a_subagent_inherits_the_transport(tmp_path):
    parent = _session(tmp_path, transport=StreamingTransport()).agent
    captured = {}
    import mini_loop.agent as module

    real = module.Agent

    class Spy(real):
        def __init__(self, **kwargs):
            super().__init__(**kwargs)
            captured["child"] = self

    module.Agent = Spy
    try:
        asyncio.run(parent._run_subagent("look", "Explore"))
    finally:
        module.Agent = real
    assert captured["child"].transport is parent.transport


# --- progress is shown, not stored ----------------------------------------

def test_streaming_emits_progress(tmp_path):
    session = _session(tmp_path, transport=StreamingTransport())
    seen = _deltas(session)
    asyncio.run(session.agent.run("hello"))
    assert seen, "a streamed turn emitted no progress at all"


def test_streaming_progress_is_correlated_with_authoritative_phases(tmp_path):
    session = _session(tmp_path, transport=StreamingTransport())
    seen = []
    original = session._capture_event

    async def spy(event):
        if event.get("type") in {"assistant_delta", "assistant_text"}:
            seen.append(dict(event))
        return await original(event)

    session._capture_event = spy
    asyncio.run(session.agent.run("hello"))

    deltas = [event for event in seen if event["type"] == "assistant_delta"]
    complete = [event for event in seen if event["type"] == "assistant_text"]
    assert deltas and complete
    assert [event["phase"] for event in complete] == [
        "commentary",
        "final_answer",
    ]
    assert all(
        event["phase"] == "commentary" and event["provisional"] is True
        for event in deltas
    )
    authoritative_streams = {event["stream_id"] for event in complete}
    assert all(event["stream_id"] in authoritative_streams for event in deltas)


def test_deltas_are_coalesced_not_one_per_token(tmp_path):
    long_text = "word " * 400
    session = _session(
        tmp_path,
        transport=StreamingTransport(coalesce_seconds=99.0),
        responder=lambda request: ([text(long_text)], "end_turn"),
    )
    seen = _deltas(session)
    asyncio.run(session.agent.run("write a lot"))
    assert seen, "no deltas"
    assert len(seen) < len(long_text.split()), "one event per token"
    assert all(len(chunk) >= DELTA_COALESCE_CHARS or chunk is seen[-1] for chunk in seen)


def test_deltas_never_reach_the_durable_log(tmp_path):
    """The record keeps what happened, not every fragment of how it arrived."""
    store = SQLiteStateStore(tmp_path / "state.db")
    session = _session(tmp_path, transport=StreamingTransport(), state_store=store)
    seen = _deltas(session)
    asyncio.run(session.agent.run("hello"))
    assert seen, "nothing was emitted, so nothing was proven"

    kinds = [event.get("type") for event in store.load_events(session.id)]
    assert "assistant_delta" not in kinds, f"deltas were persisted: {kinds}"
    assert "model_end" in kinds, "the meaningful events must still be there"
    store.close()


def test_a_delta_carrying_a_secret_is_masked(tmp_path):
    """A stream is a sink, and it was not on the list when sinks were counted."""
    from mini_loop.secrets import SecretRegistry

    secret = "sk-STREAMED-SECRET-0123456789"
    session = _session(
        tmp_path,
        transport=StreamingTransport(),
        responder=lambda request: ([text(f"the key is {secret}")], "end_turn"),
        secrets=SecretRegistry.from_environ(environ={"P_API_KEY": secret}),
    )
    seen = _deltas(session)
    asyncio.run(session.agent.run("tell me"))
    assert seen and all(secret not in chunk for chunk in seen), seen


# --- the ceiling this exists to remove ------------------------------------

#: A model the SDK *does* list a non-streaming ceiling for, so the two paths
#: can actually differ. Naming a model with no listed limit makes the test
#: vacuous -- the first version of this did, and the mutation survived it.
CAPPED_MODEL, CAPPED_CEILING = "claude-opus-4-0", 8192


def _budgets_requested(tmp_path, *, transport, ceiling):
    seen: list[int] = []

    def responder(request):
        seen.append(int(request.get("max_tokens") or 0))
        return ([text("x. ")], "max_tokens") if len(seen) < 2 else ([text("y.")], "end_turn")

    session = SessionManager(
        Settings(fake_llm=True, model=CAPPED_MODEL,
                 workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(responder=responder, nonstreaming_ceiling=ceiling),
        transport=transport,
        recovery=DefaultRecovery(),
    ).create()
    asyncio.run(session.agent.run("write something long"))
    return seen


def test_the_reference_model_really_has_a_ceiling():
    """Otherwise both tests below pass for the same, wrong reason."""
    from mini_loop.recovery import nonstreaming_ceiling

    assert nonstreaming_ceiling(CAPPED_MODEL) == CAPPED_CEILING


def test_streaming_lifts_the_non_streaming_cap(tmp_path):
    """With a stream there is no ten-minute refusal, so no cap is applied."""
    seen = _budgets_requested(
        tmp_path, transport=StreamingTransport(), ceiling=None
    )
    assert max(seen) == ESCALATED_MAX_TOKENS, (
        f"streaming was capped to {max(seen)} for no reason"
    )


def test_the_cap_still_applies_without_streaming(tmp_path):
    seen = _budgets_requested(
        tmp_path, transport=None, ceiling=CAPPED_CEILING
    )
    assert max(seen) <= CAPPED_CEILING
