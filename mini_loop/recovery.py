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
import os
import random

DEFAULT_MAX_TOKENS = 8000
ESCALATED_MAX_TOKENS = 64000
MAX_RETRIES = 10
BASE_DELAY_MS = 500
MAX_DELAY_MS = 32000
MAX_CONSECUTIVE_529 = 3
MAX_CONTINUATIONS = 3
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


def is_transient(e) -> bool:
    return is_overloaded(e) or is_rate_limit(e)


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
            return float(val) if val is not None else None
        except (TypeError, ValueError):
            return None
    return None


def backoff_delay(attempt: int, retry_after: float | None = None) -> float:
    if retry_after is not None:
        return retry_after
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
            isinstance(block, dict) and block.get("type") == "tool_result" for block in current
        )
        previous_has_use = isinstance(previous, list) and any(
            (block.get("type") if isinstance(block, dict) else getattr(block, "type", "")) == "tool_use"
            for block in previous
        )
        if current_is_result and previous_has_use:
            start -= 1
    return [{"role": "user", "content": "[Reactive compact: older turns dropped to fit context.]"},
            *messages[start:]]


class DirectRecovery:
    """No recovery -- call straight through (matches the bare loop)."""

    async def run(self, agent, kwargs: dict, call):
        return await call(kwargs)


class DefaultRecovery:
    """Backoff + token escalation + reactive compaction + fallback model."""

    def __init__(self, *, fallback_model: str | None = None, max_retries: int = MAX_RETRIES,
                 escalate: bool = True, max_continuations: int = MAX_CONTINUATIONS) -> None:
        self.fallback_model = fallback_model or os.getenv("FALLBACK_MODEL_ID") or None
        self.max_retries = max_retries
        self.escalate = escalate
        self.max_continuations = max_continuations

    async def run(self, agent, kwargs: dict, call):
        attempt = consecutive_529 = continuations = 0
        escalated = reactive = False
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
                if is_prompt_too_long(e) and not reactive:
                    # kwargs["messages"] is the live history for the main call.
                    kwargs["messages"][:] = reactive_compact(kwargs["messages"])
                    reactive = True
                    await agent._send("recovery", action="reactive_compact")
                    continue
                await agent._send("recovery", action="failed", error=f"{type(e).__name__}: {e}")
                raise
            consecutive_529 = 0
            if (self.escalate and getattr(resp, "stop_reason", None) == "max_tokens"
                    and not escalated and kwargs.get("max_tokens", 0) < ESCALATED_MAX_TOKENS):
                kwargs["max_tokens"] = ESCALATED_MAX_TOKENS
                escalated = True
                await agent._send("recovery", action="escalate_tokens", max_tokens=ESCALATED_MAX_TOKENS)
                continue
            if (getattr(resp, "stop_reason", None) == "max_tokens"
                    and continuations < self.max_continuations):
                kwargs["messages"].append({"role": "assistant", "content": resp.content})
                kwargs["messages"].append({"role": "user", "content": CONTINUATION_PROMPT})
                continuations += 1
                await agent._send("recovery", action="continue_truncated", attempt=continuations)
                continue
            return resp
