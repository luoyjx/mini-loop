"""A failed summary leaves the surface unchanged and the turn alive.

The compaction pipeline already runs its stages in DeepSeek Harness's order
-- cheap deterministic pruning first, remeasure, LLM summary only if still
over threshold. What it did not honor was their failure taxonomy: the
summary stage is a model call and can fail like one (429, 529, provider
error), and that failure PROPAGATED -- the turn died on its own
context-management step before the user's request was even attempted.

The contract pinned here:
* a failed summary closes the attempt with the transcript unchanged;
* the turn continues -- the next request either fits anyway or fails as
  itself, and that error stays authoritative;
* a summary that comes back empty is a failure, not a license to replace
  the whole transcript with a file path;
* cancellation still wins over the containment.
"""

import asyncio

import pytest

from mini_loop.compaction import DefaultCompactor


class _Agent:
    """The minimal agent surface DefaultCompactor touches."""

    def __init__(self, tmp_path, *, messages, summary_response):
        self.workspace = tmp_path
        self.messages = messages
        self.events = []
        self.secrets = None
        self.token_meter = None
        self._summary_response = summary_response

        class _Settings:
            token_threshold = 10  # anything triggers the summary stage

        self.settings = _Settings()

    async def _send(self, event_type, **fields):
        self.events.append({"type": event_type, **fields})

    async def _create(self, messages, **kwargs):
        result = self._summary_response
        if isinstance(result, BaseException):  # includes CancelledError
            raise result
        return result


def _big_transcript():
    return [{"role": "user", "content": f"turn {i} " * 200} for i in range(8)]


def test_a_failed_summary_keeps_the_transcript_and_reports(tmp_path):
    agent = _Agent(
        tmp_path,
        messages=_big_transcript(),
        summary_response=RuntimeError("overloaded 529"),
    )
    before = list(agent.messages)
    asyncio.run(DefaultCompactor(token_threshold=10).maybe_compact(agent))
    assert agent.messages == before, "a failed summary must not touch the surface"
    failed = [e for e in agent.events if e["type"] == "compact" and e.get("kind") == "failed"]
    assert failed and "529" in failed[0]["error"]


def test_an_empty_summary_is_a_failure_not_a_replacement(tmp_path):
    class _Resp:
        content = [{"type": "text", "text": "   "}]
        stop_reason = "end_turn"

    agent = _Agent(tmp_path, messages=_big_transcript(), summary_response=_Resp())
    before = list(agent.messages)
    asyncio.run(DefaultCompactor(token_threshold=10).maybe_compact(agent))
    assert agent.messages == before
    failed = [e for e in agent.events if e["type"] == "compact" and e.get("kind") == "failed"]
    assert failed and "empty" in failed[0]["error"]


def test_cancellation_still_wins(tmp_path):
    agent = _Agent(
        tmp_path,
        messages=_big_transcript(),
        summary_response=asyncio.CancelledError(),
    )
    with pytest.raises(asyncio.CancelledError):
        asyncio.run(DefaultCompactor(token_threshold=10).maybe_compact(agent))


def test_a_successful_summary_still_replaces(tmp_path):
    class _Resp:
        content = [{"type": "text", "text": "the session so far, summarized"}]
        stop_reason = "end_turn"

    agent = _Agent(tmp_path, messages=_big_transcript(), summary_response=_Resp())
    asyncio.run(DefaultCompactor(token_threshold=10).maybe_compact(agent))
    assert len(agent.messages) == 1
    assert "summarized" in str(agent.messages[0]["content"])
