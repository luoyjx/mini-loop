"""What streaming changed about failing, checked after the fact.

Round 39 added the transport and verified the happy path end to end. It did not
check what streaming does to the paths that *handle failure*, and it changed two
of them.

**A dropped stream was not retried.** `is_transient` knew 429 and 529 and
nothing about the connection. A non-streaming call is one request the SDK can
retry internally; a stream is held open for the whole generation, and a drop
after the first byte surfaces here instead. Streaming is also used for the
longest requests -- the ones most likely to drop -- so the longer the answer,
the more likely the turn was simply lost:

    answer         : '[Error] ConnectionError: stream dropped'
    model attempts : 1

**Ephemeral deltas were still replayed.** They were kept out of the store and
the trajectory, and left in the backlog a late SSE subscriber replays. Catching
up on a finished turn meant being handed stale fragments the final
`assistant_text` had already superseded. Two thirds of a decision.

And retrying a stream needs a signal, because the retry regenerates: a console
holding the first attempt's text would render it spliced onto the second.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, text
from mini_loop.recovery import (
    DefaultRecovery,
    is_connection_error,
    is_prompt_too_long,
    is_streaming_required,
    is_transient,
)
from mini_loop.storage import SQLiteStateStore
from mini_loop.transport import StreamingTransport

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
ANSWER = "HELLO-WORLD-THIS-IS-THE-ANSWER"


def _session(tmp_path, *, responder=None, **kwargs):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(responder=responder) if responder else FakeAsyncAnthropic(),
        transport=StreamingTransport(coalesce_chars=1, coalesce_seconds=0),
        recovery=DefaultRecovery(),
        **kwargs,
    ).create()


def _events(session, kind):
    seen: list[dict] = []
    original = session._capture_event

    async def spy(event):
        # The *result*, not the argument: enrichment (seq, ts, and the
        # `ephemeral` flag) happens inside, so reading the input observes the
        # event before the thing under test has been decided.
        result = await original(event)
        if result.get("type") == kind:
            seen.append(result)
        return result

    session._capture_event = spy
    return seen


@pytest.fixture
def dropping_stream(monkeypatch):
    """Kill the first stream partway, as a lost connection would."""
    import mini_loop.fake_llm as fake

    real = fake._FakeStream.__aiter__
    armed = {"value": True}

    async def flaky(self):
        async for event in real(self):
            yield event
            if armed["value"]:
                armed["value"] = False
                raise ConnectionError("stream dropped")

    monkeypatch.setattr(fake._FakeStream, "__aiter__", flaky)
    return armed


# --- classification -------------------------------------------------------

@pytest.mark.parametrize("error", [
    ConnectionError("dropped"),
    TimeoutError("read timed out"),
    type("APIConnectionError", (Exception,), {})("peer reset"),
    type("RemoteProtocolError", (Exception,), {})("incomplete chunked read"),
])
def test_connection_failures_are_transient(error):
    assert is_connection_error(error)
    assert is_transient(error)


@pytest.mark.parametrize("error", [
    ValueError("Streaming is required for operations..."),
    ValueError("prompt is too long"),
])
def test_request_level_errors_are_not_swept_up_as_transient(error):
    """Retrying these burns ten attempts to fail identically."""
    assert not is_connection_error(error)
    assert not is_transient(error)
    assert is_streaming_required(error) or is_prompt_too_long(error)


# --- behaviour ------------------------------------------------------------

def test_a_dropped_stream_is_retried_and_the_answer_survives(tmp_path, dropping_stream):
    attempts = {"n": 0}

    def responder(request):
        attempts["n"] += 1
        return ([text(ANSWER)], "end_turn")

    session = _session(tmp_path, responder=responder)
    assert asyncio.run(session.agent.run("say hello")) == ANSWER
    assert attempts["n"] == 2, "the drop was not retried"


def test_a_retry_announces_that_it_is_starting_over(tmp_path, dropping_stream):
    """The retry regenerates; a console must discard the first attempt's text."""
    session = _session(tmp_path, responder=lambda r: ([text(ANSWER)], "end_turn"))
    starts = _events(session, "stream_start")
    asyncio.run(session.agent.run("say hello"))
    assert len(starts) == 2, f"expected one stream_start per attempt, got {len(starts)}"


def test_the_answer_is_not_spliced_from_two_attempts(tmp_path, dropping_stream):
    session = _session(tmp_path, responder=lambda r: ([text(ANSWER)], "end_turn"))
    asyncio.run(session.agent.run("say hello"))
    assert session.agent.last_text == ANSWER
    assert session.agent.last_text.count("HELLO") == 1


# --- ephemeral means ephemeral -------------------------------------------

def test_deltas_are_not_replayed_to_a_late_subscriber(tmp_path):
    session = _session(tmp_path)
    deltas = _events(session, "assistant_delta")
    asyncio.run(session.agent.run("hello"))
    assert deltas, "nothing was emitted, so nothing is proven"

    replayed = [event.get("type") for event in session._backlog]
    assert "assistant_delta" not in replayed, f"stale progress replayed: {replayed}"
    assert "stream_start" not in replayed
    assert "assistant_text" in replayed, "the record must still be replayable"


def test_the_flag_reaches_a_client(tmp_path):
    """A console has to tell live progress from the record it can rely on."""
    session = _session(tmp_path)
    deltas = _events(session, "assistant_delta")
    texts = _events(session, "assistant_text")
    asyncio.run(session.agent.run("hello"))
    assert all(event.get("ephemeral") for event in deltas)
    assert not any(event.get("ephemeral") for event in texts)


def test_deltas_still_stay_out_of_the_durable_log(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    session = _session(tmp_path, state_store=store)
    deltas = _events(session, "assistant_delta")
    asyncio.run(session.agent.run("hello"))
    assert deltas
    kinds = [event.get("type") for event in store.load_events(session.id)]
    assert "assistant_delta" not in kinds and "stream_start" not in kinds
    store.close()
