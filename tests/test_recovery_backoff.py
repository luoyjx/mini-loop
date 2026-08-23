"""The retry delay is bounded and finite, even when a server says otherwise.

`DefaultRecovery` retries a transient error after `backoff_delay(...)` seconds,
and honors a `Retry-After` header when the server sends one. The computed
backoff is deliberately capped at `MAX_DELAY_MS` -- the point of that ceiling is
that a single retry cannot wait forever. But the server-supplied path escaped it
entirely: `backoff_delay` returned the header value verbatim, so `Retry-After:
100000` slept the turn for 27 hours and `Retry-After: inf` -- a value
`float()` accepts -- slept it *forever*, on a session that would never make
progress again. A response header must not be able to hang a turn.
"""

import math

import pytest

from mini_loop.recovery import (
    MAX_DELAY_MS,
    MAX_RETRY_AFTER_MS,
    backoff_delay,
    retry_after_seconds,
)


class _Resp:
    def __init__(self, headers):
        self.headers = headers


class _Err(Exception):
    def __init__(self, headers):
        self.response = _Resp(headers)


CEILING = MAX_RETRY_AFTER_MS / 1000.0


def test_a_reasonable_retry_after_is_honored_exactly():
    """The whole reason to read the header is to wait as long as it asks -- for
    any value a real rate limiter sends, which is seconds."""
    for value in ("1", "15", "60", "120"):
        error = _Err({"retry-after": value})
        assert backoff_delay(0, retry_after_seconds(error)) == float(value)


def test_an_absurd_retry_after_is_clamped_not_honored():
    """A value past every real rate-limit window is clamped to the ceiling, not
    slept through -- the header is respected, the hang is not."""
    delay = backoff_delay(0, retry_after_seconds(_Err({"retry-after": "100000"})))
    assert delay == CEILING
    assert delay < 100000


@pytest.mark.parametrize("value", ["inf", "nan", "-5", "not-a-number", ""])
def test_a_malformed_retry_after_falls_back_to_bounded_backoff(value):
    """`inf` sleeps forever; `nan` and a negative are not delays either. None of
    them is a number to sleep for, so the parse rejects them and the caller uses
    the computed backoff, which is finite and capped."""
    assert retry_after_seconds(_Err({"retry-after": value})) is None
    # And even if such a value reached backoff_delay directly, the sleep it
    # produces is finite and within the ceiling.
    for raw in (float("inf"), float("nan"), -5.0):
        delay = backoff_delay(0, raw)
        assert math.isfinite(delay)
        assert 0.0 <= delay <= CEILING


def test_every_backoff_delay_is_finite_and_bounded():
    """The invariant the recovery loop relies on: whatever it passes to
    `asyncio.sleep` is a finite number no larger than the ceiling."""
    for attempt in range(0, 40):
        computed = backoff_delay(attempt)
        assert math.isfinite(computed)
        # No header: exponential backoff, capped at MAX_DELAY_MS plus jitter.
        assert computed <= (MAX_DELAY_MS / 1000.0) * 1.25
    for header in (0.0, 5.0, CEILING, CEILING * 10, float("inf")):
        honored = backoff_delay(0, header)
        assert math.isfinite(honored)
        assert 0.0 <= honored <= CEILING


def test_no_retry_after_header_uses_the_computed_backoff():
    """A missing or headerless error carries no delay to honor."""
    assert retry_after_seconds(_Err({})) is None
    assert retry_after_seconds(Exception("no response at all")) is None


# -- shape-agnostic classification (round 225) ------------------------------
# A compatible endpoint (Pi P0-1's provider variation; measured against
# api.deepseek.com/anthropic) need not surface errors as the anthropic
# SDK's typed exceptions. Recovery classifies by status_code read from
# several locations AND by keyword fallback in the name/message, so a
# retryable error is still retried whether it arrives as a status code or
# only as text. Pinned so a status-code-only refactor cannot silently stop
# retrying against a compatible endpoint.

from mini_loop.recovery import is_overloaded, is_rate_limit, is_transient


class _StatusErr(Exception):
    def __init__(self, status_code):
        super().__init__("boom")
        self.status_code = status_code


def test_overload_is_classified_from_status_code_alone():
    assert is_overloaded(_StatusErr(529))
    assert is_transient(_StatusErr(529))


def test_overload_is_classified_from_a_message_only():
    assert is_overloaded(Exception("the model is overloaded, try again"))
    assert is_overloaded(Exception("upstream returned 529"))
    assert is_transient(Exception("529 overloaded"))


def test_rate_limit_is_classified_from_status_or_message():
    assert is_rate_limit(_StatusErr(429))
    assert is_rate_limit(Exception("RateLimit exceeded"))
    assert is_rate_limit(Exception("HTTP 429 too many requests"))


def test_a_plain_error_is_not_spuriously_retryable():
    assert not is_transient(Exception("invalid request: bad schema"))
    assert not is_overloaded(_StatusErr(400))
