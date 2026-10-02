"""A context-overflow retry must be preceded by an actually smaller surface.

DeepSeek Harness's compaction/recovery contract: `agent/request-error`
returns a retry action only when the surface replacement generation advanced
-- pruning or summarization really replaced content -- otherwise the
original request error remains authoritative. mini-loop's reactive path
retried after *attempting* compaction, which is a different claim:

* a transcript already at/below `keep` messages makes `reactive_compact`
  the identity, so the retry re-sent the identical prompt -- a guaranteed
  second overflow billed as recovery;
* the tool-pairing adjustment can walk `start` back to 0, making the
  "compacted" prompt the whole history PLUS a marker -- strictly larger
  than the prompt that just overflowed.

Both now raise instead of retrying, with a `recovery: failed` event naming
the reason.
"""

import asyncio

import pytest

from mini_loop import recovery
from mini_loop.recovery import DefaultRecovery


class _Stub:
    def __init__(self):
        self.events = []
        self.transport = None

    async def _send(self, event_type, **fields):
        self.events.append({"type": event_type, **fields})


def _overflow():
    return RuntimeError("prompt is too long: too many tokens")


def test_a_shrinkable_surface_is_compacted_and_retried(monkeypatch):
    monkeypatch.setattr(recovery, "backoff_delay", lambda *a, **k: 0)
    calls = {"n": 0}

    async def call(kw):
        calls["n"] += 1
        if len(kw["messages"]) > 10:
            raise _overflow()
        return type("R", (), {"stop_reason": "end_turn", "content": []})()

    kw = {
        "model": "m",
        "messages": [{"role": "user", "content": f"turn {i}" * 50} for i in range(20)],
        "max_tokens": 8000,
    }
    stub = _Stub()
    asyncio.run(DefaultRecovery().run(stub, kw, call))
    assert calls["n"] == 2  # one overflow, one successful retry
    assert any(e["type"] == "recovery" and e.get("action") == "reactive_compact"
               for e in stub.events)


def test_an_unshrinkable_surface_fails_without_a_retry(monkeypatch):
    """<= keep messages: compaction is the identity; the retry would be the
    byte-identical request. The original error stays authoritative."""

    monkeypatch.setattr(recovery, "backoff_delay", lambda *a, **k: 0)
    calls = {"n": 0}

    async def call(kw):
        calls["n"] += 1
        raise _overflow()

    kw = {
        "model": "m",
        "messages": [{"role": "user", "content": "one enormous message" * 1000}],
        "max_tokens": 8000,
    }
    stub = _Stub()
    with pytest.raises(RuntimeError):
        asyncio.run(DefaultRecovery().run(stub, kw, call))
    assert calls["n"] == 1, "no retry may follow a compaction that changed nothing"
    failed = [e for e in stub.events if e["type"] == "recovery" and e.get("action") == "failed"]
    assert failed and "could not shrink" in failed[0]["reason"]


def test_a_pairing_walkback_that_inflates_the_prompt_does_not_retry(monkeypatch):
    """`start` walked to 0 by the tool-pairing rule: the marker makes the
    result *larger* than what already overflowed. Fail, do not retry."""

    monkeypatch.setattr(recovery, "backoff_delay", lambda *a, **k: 0)
    calls = {"n": 0}

    async def call(kw):
        calls["n"] += 1
        raise _overflow()

    # 7 messages (> keep=6); messages[1] is the tool_result answering
    # messages[0]'s tool_use, so start 1 -> 0 and nothing is dropped.
    messages = [
        {"role": "assistant", "content": [
            {"type": "tool_use", "id": "tu1", "name": "bash", "input": {}}]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "tu1", "content": "x" * 2000}]},
    ] + [{"role": "user", "content": f"filler {i}" * 100} for i in range(5)]
    kw = {"model": "m", "messages": messages, "max_tokens": 8000}
    stub = _Stub()
    with pytest.raises(RuntimeError):
        asyncio.run(DefaultRecovery().run(stub, kw, call))
    assert calls["n"] == 1
