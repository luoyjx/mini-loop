"""How the model call is made: one shot, or streamed.

The harness has always called `client.messages.create(...)` and waited. That is
the simplest thing that works, and it has a hard ceiling: the SDK refuses a
non-streaming request whose `max_tokens` implies more than ten minutes of work,
*before sending it*. Round 38 found the recovery path walking straight into that
refusal — escalating to 64,000 tokens to rescue a truncated answer, and turning
a recoverable truncation into a failed turn. The cap added there is a workaround
for the absent capability; this is the capability.

    non-streaming, max_tokens=64,000 -> ValueError: Streaming is required...
    streaming,     max_tokens=64,000 -> accepted, 10 deltas, first at 0.66s

`get_final_message()` returns the same `Message` the non-streaming call returns —
same id, model, content blocks, `stop_reason` and `usage` — so everything
downstream (normalization, the token meter, recovery's continuation) is
unchanged. The transport is a seam, and the default is still `DirectTransport`:
streaming is opt-in, because a stream is a longer-lived resource and a harness
should not quietly change how it holds one.

**Deltas are ephemeral.** A streamed turn produces one event per token, and this
harness persists every event to SQLite and fans it to every SSE subscriber. Left
alone that is thousands of rows per turn and a durable log made mostly of
fragments. Deltas are coalesced before emission and marked `_ephemeral`, which
keeps them out of the store and the trajectory while still reaching a live
console. Progress is worth showing and is not worth keeping.
"""

from __future__ import annotations

import time
import uuid
from typing import Any, Protocol, runtime_checkable

__all__ = [
    "Transport",
    "DirectTransport",
    "StreamingTransport",
    "DELTA_COALESCE_SECONDS",
    "DELTA_COALESCE_CHARS",
]

#: Deltas are flushed when either bound is reached. Small enough to feel live,
#: large enough that a fast stream does not emit per token.
DELTA_COALESCE_SECONDS = 0.20
DELTA_COALESCE_CHARS = 200


@runtime_checkable
class Transport(Protocol):
    #: True when this transport is not subject to the non-streaming ceiling.
    streaming: bool

    async def send(self, agent, kwargs: dict) -> Any:
        """Perform one model call and return the final message."""
        ...


class DirectTransport:
    """One request, one response. The behaviour the harness has always had."""

    streaming = False

    async def send(self, agent, kwargs: dict) -> Any:
        agent._last_stream_id = None
        return await agent.client.messages.create(**kwargs)


class StreamingTransport:
    """Stream the response, emit coalesced progress, return the final message.

    The return value is what `get_final_message()` gives: the same object shape
    a non-streaming call produces. Nothing downstream needs to know which
    transport ran.
    """

    streaming = True

    def __init__(
        self,
        *,
        coalesce_seconds: float = DELTA_COALESCE_SECONDS,
        coalesce_chars: int = DELTA_COALESCE_CHARS,
    ) -> None:
        self.coalesce_seconds = coalesce_seconds
        self.coalesce_chars = coalesce_chars

    async def send(self, agent, kwargs: dict) -> Any:
        # Every send is a fresh generation. When a retry follows a stream that
        # already emitted text, a console holding those deltas must discard them
        # or it renders the first attempt spliced onto the second.
        stream_id = f"stream_{uuid.uuid4().hex[:16]}"
        agent._last_stream_id = stream_id
        await agent._send(
            "stream_start",
            stream_id=stream_id,
            phase="commentary",
            provisional=True,
            _ephemeral=True,
        )
        # Reset per send, because each send is a fresh generation. Held so that
        # an interrupted turn can record what the user was actually shown: the
        # console had rendered it, and a transcript that does not know it exists
        # cannot answer "finish that thought", or worse, starts over.
        agent.streamed_text = ""
        pending: list[tuple[str, str]] = []
        last_flush = time.monotonic()

        async def flush() -> None:
            nonlocal last_flush
            if not pending:
                return
            text = "".join(piece for piece, _ in pending)
            # Only answer text is recoverable into a transcript. Thinking is
            # shown as progress but never accumulated: it is a distinct block
            # type carrying a signature, and replaying it back as assistant
            # *text* would both misrepresent it and lose the signature the API
            # requires. Raw, like the rest of the in-memory transcript;
            # `_flush_messages` masks what reaches disk.
            agent.streamed_text += "".join(
                piece for piece, kind in pending if kind == "text"
            )
            pending.clear()
            last_flush = time.monotonic()
            # Masked like any other emitted text: a stream is a sink too, and
            # it was not on the list when the sinks were enumerated.
            await agent._send(
                "assistant_delta",
                text=agent.secrets.mask(text),
                stream_id=stream_id,
                # The provider does not reveal whether later content blocks
                # contain a tool call. Treat live fragments as provisional
                # commentary; the following assistant_text event carries the
                # authoritative commentary/final_answer classification.
                phase="commentary",
                provisional=True,
                _ephemeral=True,
            )

        async with agent.client.messages.stream(**kwargs) as stream:
            async for event in stream:
                if getattr(event, "type", "") != "content_block_delta":
                    continue
                delta = getattr(event, "delta", None)
                piece = getattr(delta, "text", None)
                kind = "text"
                if not piece:
                    piece, kind = getattr(delta, "thinking", None), "thinking"
                if not piece:
                    continue
                pending.append((piece, kind))
                if (
                    sum(len(p) for p, _ in pending) >= self.coalesce_chars
                    or time.monotonic() - last_flush >= self.coalesce_seconds
                ):
                    await flush()
            await flush()
            final = await stream.get_final_message()
            # The stream completed, so its text is in `final` and the caller
            # commits it to the transcript. Clear the partial: `streamed_text`
            # must hold only what an *interrupted* stream showed and never
            # recorded. Left set, an interrupt landing after a completed round --
            # or after an internal streamed call like the compaction summary --
            # re-records stale text as a phantom interrupted assistant turn
            # (duplicating the round, or surfacing "[summary]" as the answer).
            agent.streamed_text = ""
            return final
