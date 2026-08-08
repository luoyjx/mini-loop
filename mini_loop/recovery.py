"""Error recovery for the LLM call (s11), as a swappable seam.

A bare `messages.create` dies on the first 429/529/overflow/truncation. A
`RecoveryPolicy` wraps the call so each error class routes to a recovery path:

  * transient (429 rate-limit / 529 overloaded) -> exponential backoff + jitter,
    honoring Retry-After; after N consecutive 529s, switch to a fallback model;
  * prompt too long -> reactive compaction of the history, then retry once;
  * output truncated (stop_reason == "max_tokens") -> escalate the token budget
    (8k -> 64k) once, then continue from the truncated output with a bounded
    continuation prompt.

Inject via `Agent(recovery=...)` / `SessionManager(recovery=...)`. The default
is transparent when no errors occur (so it changes nothing for healthy calls).
"""

from __future__ import annotations

import asyncio
import math
import os
import random

from .blocks import block_field

DEFAULT_MAX_TOKENS = 8000
ESCALATED_MAX_TOKENS = 64000
MAX_RETRIES = 10
BASE_DELAY_MS = 500
MAX_DELAY_MS = 32000
#: A server's Retry-After is honored, but the wait it produces is still bounded.
#: The computed backoff caps at MAX_DELAY_MS; a header must not be able to escape
#: that into an unbounded -- or, with `Retry-After: inf`, an *infinite* -- sleep
#: that hangs the turn on a session that never makes progress. Generous enough
#: to honor any real rate-limit window (which are seconds, rarely a minute);
#: finite by construction.
MAX_RETRY_AFTER_MS = 300_000
MAX_CONSECUTIVE_529 = 3
MAX_CONTINUATIONS = 3
#: Escalation discards a partial answer and regenerates. Only worth it when the
#: new budget is meaningfully larger than the one that truncated.
MIN_ESCALATION_RATIO = 1.5
CONTINUATION_PROMPT = (
    "Continue exactly where the truncated response stopped. Do not repeat completed content."
)


def _name(e) -> str:
    return type(e).__name__.lower()


def _msg(e) -> str:
    return str(e).lower()


def _status(e):
    return getattr(e, "status_code", None) or getattr(getattr(e, "response", None), "status_code", None)


def is_overloaded(e) -> bool:  # 529
    return _status(e) == 529 or "overloaded" in _name(e) or "overloaded" in _msg(e) or "529" in _msg(e)


def is_rate_limit(e) -> bool:  # 429
    return _status(e) == 429 or "ratelimit" in _name(e) or "429" in _msg(e)


#: Failures of the connection rather than of the request. Retrying is safe:
#: a model call has no side effects, so a repeat costs tokens and nothing else.
_CONNECTION_ERRORS = (
    "apiconnectionerror", "connectionerror", "connectionreset", "connectionaborted",
    "readerror", "writeerror", "remoteprotocolerror", "incompleteread",
    "timeout", "timeouterror", "apitimeouterror", "readtimeout",
)


def is_connection_error(e) -> bool:
    """A transport-level failure, which streaming makes far more likely.

    A non-streaming call is one request the SDK can retry internally. A stream
    is held open for the whole generation, and a drop *after the first byte*
    cannot be retried down there -- it surfaces here. Streaming is also used
    precisely for the longest requests, which are the ones most likely to drop,
    so leaving this unclassified meant the longer the answer, the more likely a
    turn was lost outright.
    """

    name = _name(e)
    return any(marker in name for marker in _CONNECTION_ERRORS)


def is_transient(e) -> bool:
    return is_overloaded(e) or is_rate_limit(e) or is_connection_error(e)


def is_streaming_required(e) -> bool:
    """The SDK refused a non-streaming call for being too large to finish.

    Raised *before* any request goes out, so it is neither transient nor
    something the prompt can be shrunk to fix. It has to be its own class or it
    falls through to `raise`, which is what happened: escalating `max_tokens`
    turned a recoverable truncation into a failed turn.
    """

    return "streaming is required" in _msg(e) or "streaming is strongly recommended" in _msg(e)


def nonstreaming_ceiling(model: str) -> int | None:
    """The largest `max_tokens` this SDK will send without streaming.

    Read from the SDK rather than hardcoded: the limit is per-model and moves
    between versions, so any constant here would be wrong on some model or some
    upgrade. `None` means the SDK has no listed limit for this model, in which
    case it decides from an estimated duration and the only way to find out is
    to try -- which `is_streaming_required` handles.
    """

    try:
        from anthropic._constants import MODEL_NONSTREAMING_TOKENS
    except Exception:
        return None
    return MODEL_NONSTREAMING_TOKENS.get(model)


def is_prompt_too_long(e) -> bool:
    m = _msg(e)
    return any(k in m for k in (
        "prompt is too long", "prompt_too_long", "prompt_is_too_long",
        "context_length_exceeded", "max_context", "too many tokens",
    ))


def retry_after_seconds(e) -> float | None:
    resp = getattr(e, "response", None)
    headers = getattr(resp, "headers", None)
    if headers:
        val = headers.get("retry-after") or headers.get("Retry-After")
        try:
            seconds = float(val) if val is not None else None
        except (TypeError, ValueError):
            return None
        # A malformed header (`inf`, `nan`, negative) is not a delay. Reject it
        # rather than pass it on: `float("inf")` sleeps forever, and a negative
        # skips the backoff entirely and hammers the server. Fall back to the
        # computed backoff, which is bounded.
        if seconds is None or not math.isfinite(seconds) or seconds < 0:
            return None
        return seconds
    return None


def backoff_delay(attempt: int, retry_after: float | None = None) -> float:
    if retry_after is not None:
        # Honored, but bounded and finite: the whole point of MAX_DELAY_MS is a
        # ceiling on a single wait, and a server-supplied value must not escape
        # it into an hours-long -- or infinite -- sleep. `min` also collapses a
        # non-finite value that reached here to the ceiling rather than passing
        # `inf` straight to `asyncio.sleep`.
        return max(0.0, min(retry_after, MAX_RETRY_AFTER_MS / 1000.0))
    base = min(BASE_DELAY_MS * (2 ** attempt), MAX_DELAY_MS) / 1000.0
    return base * (1 + random.random() * 0.25)  # +0-25% jitter


def reactive_compact(messages: list, keep: int = 6) -> list:
    """Teaching-simple shrink that keeps tool-use/result pairs intact."""
    if len(messages) <= keep:
        return messages
    start = len(messages) - keep
    if start > 0:
        current = messages[start].get("content")
        previous = messages[start - 1].get("content")
        current_is_result = isinstance(current, list) and any(
            block_field(block, "type", "") == "tool_result" for block in current
        )
        previous_has_use = isinstance(previous, list) and any(
            block_field(block, "type", "") == "tool_use"
            for block in previous
        )
        if current_is_result and previous_has_use:
            start -= 1
    return [{"role": "user", "content": "[Reactive compact: older turns dropped to fit context.]"},
            *messages[start:]]


class DirectRecovery:
    """No recovery -- call straight through (matches the bare loop)."""

    async def run(self, agent, kwargs: dict, call, *, live_history: list | None = None):
        return await call(kwargs)


class ContinuedResponse:
    """One logical answer reassembled from several truncated ones.

    Continuation used to leave the caller with the *last* chunk only: three
    round-trips were spent finishing a long answer, the earlier parts went into
    the request history, and `run()` returned the final third. The work was done
    and then thrown away.

    Carries the fields the agent reads off a response -- content, stop_reason,
    usage and identity -- because it is standing in for one.
    """

    def __init__(self, chunks: list[list], final) -> None:
        self.content = [block for chunk in chunks for block in chunk]
        self.stop_reason = getattr(final, "stop_reason", None)
        # The *final* request's usage, not a sum: the meter is asking how full
        # the context is now, and that is what the last call measured.
        self.usage = getattr(final, "usage", None)
        self.id = getattr(final, "id", None)
        self.model = getattr(final, "model", None)
        self.stop_sequence = getattr(final, "stop_sequence", None)
        self.role = "assistant"
        self.type = "message"


class DefaultRecovery:
    """Backoff + token escalation + reactive compaction + fallback model."""

    def __init__(self, *, fallback_model: str | None = None, max_retries: int = MAX_RETRIES,
                 escalate: bool = True, max_continuations: int = MAX_CONTINUATIONS) -> None:
        self.fallback_model = fallback_model or os.getenv("FALLBACK_MODEL_ID") or None
        self.max_retries = max_retries
        self.escalate = escalate
        self.max_continuations = max_continuations

    async def run(self, agent, kwargs: dict, call, *, live_history: list | None = None):
        from .agent import _content_payload

        attempt = consecutive_529 = continuations = 0
        escalated = reactive = False
        # Retry bookkeeping edits this list; whether it aliased the live
        # transcript depended on whether a CachePolicy had copied it, which is
        # not a thing recovery should be sensitive to. The transcript is now
        # only ever touched through `live_history`, explicitly.
        kwargs["messages"] = list(kwargs["messages"])
        chunks: list[list] = []
        escalated_from: int | None = None
        escalation_partial = None
        while True:
            try:
                resp = await call(kwargs)
            except Exception as e:
                if is_transient(e) and attempt < self.max_retries:
                    if is_overloaded(e):
                        consecutive_529 += 1
                        if consecutive_529 >= MAX_CONSECUTIVE_529 and self.fallback_model:
                            kwargs["model"] = self.fallback_model
                            if hasattr(agent, "state"):
                                agent.state["recovery_model"] = self.fallback_model
                            consecutive_529 = 0
                            await agent._send("recovery", action="fallback_model", model=self.fallback_model)
                    await agent._send("recovery", action="retry", attempt=attempt + 1, error=type(e).__name__)
                    await asyncio.sleep(backoff_delay(attempt, retry_after_seconds(e)))
                    attempt += 1
                    continue
                if is_streaming_required(e) and escalated_from is not None:
                    # Give back the budget that provoked it and let continuation
                    # finish the answer. Refusing here would lose a whole turn to
                    # an optimization.
                    kwargs["max_tokens"] = escalated_from
                    escalated_from = None
                    if escalation_partial is not None:
                        content = _content_payload(escalation_partial.content)
                        chunks.append(content)
                        kwargs["messages"].append(
                            {"role": "assistant", "content": content}
                        )
                        kwargs["messages"].append(
                            {"role": "user", "content": CONTINUATION_PROMPT}
                        )
                        escalation_partial = None
                    await agent._send(
                        "recovery", action="unescalate_tokens",
                        max_tokens=kwargs["max_tokens"],
                        reason="the SDK requires streaming above this budget",
                    )
                    continue
                if is_prompt_too_long(e) and not reactive:
                    # `kwargs["messages"]` used to alias the live history, but a
                    # CachePolicy annotates onto a copy, so the aliasing silently
                    # stopped holding. `live_history` is now passed explicitly:
                    # shrink the retry *and* the conversation, or the next turn
                    # rebuilds the same oversized prompt.
                    compacted = reactive_compact(kwargs["messages"])
                    kwargs["messages"][:] = compacted
                    if live_history is not None:
                        live_history[:] = reactive_compact(live_history)
                    reactive = True
                    await agent._send("recovery", action="reactive_compact")
                    continue
                await agent._send("recovery", action="failed", error=f"{type(e).__name__}: {e}")
                raise
            consecutive_529 = 0
            if (self.escalate and getattr(resp, "stop_reason", None) == "max_tokens"
                    and not escalated and kwargs.get("max_tokens", 0) < ESCALATED_MAX_TOKENS):
                # Capped at what a non-streaming call can carry. Asking for more
                # does not produce a longer answer, it produces a `ValueError`
                # from the SDK before the request is sent -- so the path meant to
                # rescue a truncated response was failing the turn instead.
                # A streaming transport is not subject to it at all -- that is
                # the whole reason the cap exists.
                streaming = getattr(getattr(agent, "transport", None), "streaming", False)
                ceiling = None if streaming else nonstreaming_ceiling(kwargs.get("model", ""))
                target = min(ESCALATED_MAX_TOKENS, ceiling) if ceiling else ESCALATED_MAX_TOKENS
                # Escalation *regenerates* the answer from scratch, so the
                # partial already paid for is discarded. Worth it for a real
                # increase; absurd for a small one -- with a ceiling of 8192 and
                # a budget of 8000 it buys 192 tokens for a whole second
                # generation. Below the threshold, go straight to continuation,
                # which keeps what was already produced.
                if target < kwargs.get("max_tokens", 0) * MIN_ESCALATION_RATIO:
                    # No headroom. Continuation handles the rest, and it is the
                    # general answer anyway; escalation was only ever a shortcut.
                    escalated = True
                else:
                    escalated_from = kwargs.get("max_tokens")
                    # Held, not discarded. Escalation regenerates, so on success
                    # this is correctly dropped -- but if the SDK refuses the
                    # bigger budget, it is the only copy of work already paid
                    # for, and losing it costs the front of the answer.
                    escalation_partial = resp
                    kwargs["max_tokens"] = target
                    escalated = True
                    await agent._send(
                        "recovery", action="escalate_tokens", max_tokens=target,
                        capped=bool(ceiling and target < ESCALATED_MAX_TOKENS),
                    )
                    continue
            # A truncated chunk that already contains a `tool_use` must not be
            # continued. Appending it plus a "carry on" prompt leaves the tool
            # call unanswered, and the API rejects that outright:
            #   400 `tool_use` ids were found without `tool_result` blocks
            #       immediately after
            # The right move is to hand it back so the agent executes the tools;
            # continuation is for truncated *text*. The offline model used to
            # accept the malformed transcript, so a test asserting the tool ran
            # passed while the real endpoint would have refused the request.
            truncated_with_tools = any(
                block_field(block, "type") == "tool_use"
                for block in (getattr(resp, "content", None) or [])
            )
            if (getattr(resp, "stop_reason", None) == "max_tokens"
                    and truncated_with_tools):
                if chunks:
                    return ContinuedResponse(
                        [*chunks, _content_payload(resp.content)], resp
                    )
                return resp
            if (getattr(resp, "stop_reason", None) == "max_tokens"
                    and continuations < self.max_continuations):
                # Normalized here too. Appending raw provider blocks was the
                # side door that put objects rather than dicts into a transcript
                # the agent believed it had normalized on the way in.
                content = _content_payload(resp.content)
                chunks.append(content)
                kwargs["messages"].append({"role": "assistant", "content": content})
                kwargs["messages"].append({"role": "user", "content": CONTINUATION_PROMPT})
                continuations += 1
                await agent._send("recovery", action="continue_truncated", attempt=continuations)
                continue
            if chunks:
                # Hand back the whole answer, not the tail of it.
                return ContinuedResponse([*chunks, _content_payload(resp.content)], resp)
            return resp
