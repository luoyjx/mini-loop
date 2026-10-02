"""Compaction fired on a guess while the provider was reporting the answer.

`estimate_tokens` is `len(json.dumps(messages)) // 4`. Measured against the real
tokenizer it is off by 0.36x-2.64x depending on content -- Chinese prose 2.64x
high, a base64 payload 0.36x low -- and it never saw the system prompt or tool
schemas, which measured 3,395 tokens reported as 8.

Over a live five-turn session mixing those content types, mean error was 54%
for the estimate and 21% for the meter, and the estimate's error was almost all
in the *under*-counting direction: compacting late, which does not degrade, it
fails the request outright.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.compaction import DefaultCompactor, context_used, estimate_tokens
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.metering import (
    MAX_CALIBRATION,
    MIN_CALIBRATION,
    TokenMeter,
    prompt_tokens,
)

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _agent(tmp_path, **kwargs):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
        **kwargs,
    ).create().agent


def _messages(count, size=400):
    return [{"role": "user", "content": "x" * size} for _ in range(count)]


# --- the caching interaction, which is the trap ---------------------------

def test_cached_tokens_count_toward_the_prompt():
    """A cache hit is cheaper, not smaller. It occupies the window either way.

    Reading `input_tokens` alone would call a 190k-token request 4k, and would
    get *worse* the better caching works -- under-counting, which is the
    direction that ends in a hard context-length error rather than a
    degradation.
    """
    usage = {
        "input_tokens": 4_000,
        "cache_read_input_tokens": 180_000,
        "cache_creation_input_tokens": 6_000,
        "output_tokens": 500,
    }
    assert prompt_tokens(usage) == 190_000


def test_usage_may_be_an_object_or_absent():
    class Usage:
        input_tokens = 120
        cache_read_input_tokens = 30

    assert prompt_tokens(Usage()) == 150
    assert prompt_tokens(None) is None
    assert prompt_tokens({"output_tokens": 10}) is None


# --- anchoring and calibration --------------------------------------------

def test_before_any_response_it_falls_back_to_the_estimate():
    """Which is what the harness did for *every* request, not just the first.

    The first prompt is also the smallest, so the one request that cannot be
    measured is the one where being wrong costs least.
    """
    meter = TokenMeter()
    messages = _messages(3)
    assert not meter.calibrated
    assert meter.used(messages) == estimate_tokens(messages)


def test_an_observation_anchors_exactly():
    meter = TokenMeter()
    messages = _messages(3)
    assert meter.observe({"input_tokens": 9_999}, messages) == 9_999
    assert meter.used(messages) == 9_999, "the anchor must be used verbatim"


def test_calibration_is_learned_from_growth_not_from_absolutes(tmp_path):
    """The design point: a fixed overhead must not become a multiplier.

    System prompt and tools are a constant ~3,000 tokens the estimate never
    sees. Calibrating on absolute readings would fold that into a ratio and then
    re-inflate it as the transcript grows, over-counting more and more. Taking
    the ratio between *consecutive* readings cancels it.
    """
    OVERHEAD, PER_TOKEN = 3_000, 1.0
    meter = TokenMeter()
    messages = []
    for _ in range(5):
        messages.extend(_messages(2))
        actual = OVERHEAD + int(estimate_tokens(messages) * PER_TOKEN)
        meter.observe({"input_tokens": actual}, messages)

    assert meter.calibration == pytest.approx(PER_TOKEN, abs=0.15), (
        f"calibration {meter.calibration} absorbed the fixed overhead"
    )
    messages.extend(_messages(2))
    truth = OVERHEAD + int(estimate_tokens(messages) * PER_TOKEN)
    assert meter.used(messages) == pytest.approx(truth, rel=0.1)


def test_calibration_is_clamped_against_a_single_strange_reading():
    meter = TokenMeter(smoothing=1.0)
    messages = _messages(2)
    meter.observe({"input_tokens": 100}, messages)
    messages.append({"role": "user", "content": "x"})
    meter.observe({"input_tokens": 10_000_000}, messages)
    assert MIN_CALIBRATION <= meter.calibration <= MAX_CALIBRATION


def test_a_shrinking_transcript_is_seen_not_pinned_to_the_anchor():
    """Compaction shrinks `messages` between provider readings, and the meter
    re-anchors only on the next response. Until then `used()` has to reflect the
    shrink -- otherwise the `context_used() > threshold` gate runs the expensive
    LLM-summary layer against a transcript the cheap layers (snip, micro)
    already cut below threshold.

    The rule here used to be "growth must not go negative", which pinned a
    shrunk transcript at the full anchor and made every in-process compaction
    invisible to the gate -- the anchor models a *fixed* overhead plus linear
    scaling, so a shrink is just a negative delta, not something to clamp away.
    The delta is signed now; only the final result is floored at zero.
    """
    meter = TokenMeter()
    big = _messages(20)
    meter.observe({"input_tokens": 40_000}, big)
    assert meter.used(big) == 40_000, "the anchor is still used verbatim at rest"

    small = _messages(2)
    assert meter.used(small) < meter.used(big), (
        "a shrunk transcript still read as the pre-compaction size: the gate "
        "cannot see what snip/micro removed"
    )
    # Monotonic and never negative across an arbitrary shrink to empty.
    assert 0 <= meter.used([]) <= meter.used(small) <= meter.used(big)


# --- composition: the decision must actually read it ----------------------

def test_compaction_reads_the_meter_not_the_estimate(tmp_path):
    """The payoff. The estimate under-counted by ~50% in the live measurement,
    so a transcript well over the threshold read as comfortably under it.
    """
    agent = _agent(tmp_path)
    agent.messages.extend(_messages(4))
    estimate = estimate_tokens(agent.messages)

    agent.token_meter.observe({"input_tokens": estimate * 4}, agent.messages)
    assert context_used(agent) == estimate * 4
    assert context_used(agent) > estimate, "the meter was ignored"


def test_cheap_compaction_below_threshold_skips_the_llm_summary(tmp_path):
    """The payoff. snip and micro cut the transcript below the threshold; the
    meter must reflect that so the expensive LLM-summary layer -- an extra model
    call plus the whole transcript spilled to disk -- is not run on an already
    small transcript. With a growth-clamped meter `context_used` reported the
    pre-compaction anchor, so the summary fired on every turn after a snip.
    """
    agent = _agent(tmp_path)
    agent.messages.extend(_messages(60, size=1_000))
    estimate = estimate_tokens(agent.messages)
    agent.token_meter.observe({"input_tokens": estimate}, agent.messages)

    compactor = DefaultCompactor(token_threshold=estimate // 2, max_messages=6)
    assert context_used(agent) > compactor.token_threshold, "anchored above threshold"

    fired = []

    async def _fake_summary(_agent):
        fired.append(True)

    compactor.compact = _fake_summary
    asyncio.run(compactor.maybe_compact(agent))

    assert context_used(agent) < compactor.token_threshold, (
        "the meter still did not reflect what snip/micro removed from the transcript"
    )
    assert not fired, (
        "the LLM-summary layer fired even though the cheap layers had already "
        "cut the transcript below the threshold"
    )


def test_an_agent_without_a_meter_still_works(tmp_path):
    class Bare:
        messages = _messages(3)

    assert context_used(Bare()) == estimate_tokens(Bare.messages)


def test_the_threshold_now_fires_on_real_tokens(tmp_path):
    """Under threshold by estimate, over it in reality -> must compact."""
    agent = _agent(tmp_path)
    agent.messages.extend(_messages(4))
    estimate = estimate_tokens(agent.messages)
    agent.token_meter.observe({"input_tokens": estimate * 5}, agent.messages)

    compactor = DefaultCompactor(token_threshold=estimate * 2)
    assert estimate < compactor.token_threshold, "estimate should look safe"
    assert context_used(agent) > compactor.token_threshold, "reality should not"

    fired = []
    compactor.compact = lambda agent: fired.append(True) or asyncio.sleep(0)
    asyncio.run(compactor.maybe_compact(agent))
    assert fired, "compaction did not fire on the real token count"


def test_the_agent_feeds_the_meter_from_live_conversation_responses(tmp_path):
    """Wiring check: live usage reaches the conversation meter."""
    agent = _agent(tmp_path)
    asyncio.run(agent.run("hello"))
    assert agent.token_meter.observations > 0, "no response was ever observed"


def test_side_queries_report_usage_without_reanchoring_the_live_meter(tmp_path):
    agent = _agent(tmp_path)
    agent.messages.append({"role": "user", "content": "live conversation"})
    session = agent.state["session"]

    async def exercise():
        await agent._create(
            agent.messages, tools=[], system="stable", purpose="agent_turn"
        )
        live_snapshot = agent.token_meter.snapshot()
        assert live_snapshot["observations"] == 1

        side_messages = [{"role": "user", "content": "side query" * 100}]
        for purpose, messages in (
            ("memory_selection", side_messages),
            ("memory_consolidation", side_messages),
            ("compaction", side_messages),
            # Memory extraction currently calls `_create` without overriding
            # the default purpose. Object identity must still keep it separate.
            ("agent_turn", side_messages),
            # Purpose is also load-bearing: even the live list must not anchor
            # a request explicitly classified as a side operation.
            ("compaction", agent.messages),
        ):
            await agent._create(messages, purpose=purpose)
            assert agent.token_meter.snapshot() == live_snapshot

        await agent._create(
            agent.messages, tools=[], system="stable", purpose="agent_turn"
        )
        assert agent.token_meter.observations == 2

    asyncio.run(exercise())

    side_events = [
        event
        for event in session._backlog
        if event.get("type") == "model_end"
        and event.get("purpose") != "agent_turn"
    ]
    assert side_events
    assert all(event.get("prompt_tokens") for event in side_events)


# --- the envelope, which is the anchor's hidden assumption -----------------

def test_an_envelope_change_sets_the_anchor_aside():
    """The anchor prices `overhead + transcript` with the overhead FIXED.

    Connecting an MCP server, spawning a teammate (which unregisters tools),
    or rebuilding the system prompt changes that overhead mid-session; an
    anchor read under the old envelope then under-counts the new request --
    the direction that ends in a hard overflow. On mismatch, `used_for`
    answers with the raw estimate until the next response re-anchors.
    """
    meter = TokenMeter()
    messages = _messages(10)
    meter.observe({"input_tokens": 5_000}, messages, envelope="env-a")
    anchored = meter.used_for(messages, envelope="env-a")
    assert anchored == 5_000
    # Same transcript, different envelope: the anchor no longer applies.
    repriced = meter.used_for(messages, envelope="env-b")
    assert repriced == estimate_tokens(messages)


def test_a_matching_or_unnamed_envelope_keeps_the_anchor():
    meter = TokenMeter()
    messages = _messages(10)
    meter.observe({"input_tokens": 5_000}, messages, envelope="env-a")
    assert meter.used_for(messages, envelope="env-a") == 5_000
    # A caller that cannot name its envelope gets the anchored answer.
    assert meter.used_for(messages) == 5_000


def test_calibration_is_not_learned_across_an_envelope_change():
    """The delta between readings under different envelopes contains the
    envelope change itself; feeding it to the calibration would poison the
    ratio with schema bytes that are not transcript growth."""
    meter = TokenMeter()
    meter.observe({"input_tokens": 1_000}, _messages(5), envelope="env-a")
    before = meter.calibration
    # A big jump in actual tokens caused by a fatter envelope, not by growth.
    meter.observe({"input_tokens": 40_000}, _messages(6), envelope="env-b")
    assert meter.calibration == before  # re-anchored, not re-calibrated
    assert meter.anchor == 40_000
