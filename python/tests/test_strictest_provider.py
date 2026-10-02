"""The double must match the strictest provider supported, not the one plugged in.

Round 43 made the offline model refuse the conversations the live endpoint
refuses. Probing further showed the configured endpoint is *more lenient* than
the provider this harness targets:

    case                          THIS ENDPOINT   ANTHROPIC (documented)
    thinking with no signature    accepted        rejected
    6 cache_control breakpoints   accepted        max 4
    role: "system" in messages    accepted        user/assistant only
    assistant with empty content  rejected        rejected
    max_tokens = 0                rejected        rejected

Encoding what happens to be configured would leave the harness green here and
broken against the provider it is written for. The double enforces the stricter
set, and the two categories are labelled: **observed** rules were reproduced from
a live 400, **documented** rules come from Anthropic's published limits and are
not verifiable against this endpoint.

That distinction is a correction as much as a design note. Rounds 30 and 41
stated that "the API rejects a thinking block whose signature did not survive"
as though it had been observed. It had not -- this endpoint accepts one. The
requirement is real and documented, and the harness satisfies it, but the
evidence for it was Anthropic's docs, not a probe. Saying otherwise overstated
what had been checked.

Everything below passed the moment the double started checking, so the harness
was already correct against the stricter provider; nothing was enforcing it.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.caching import DefaultCachePolicy
from mini_loop.fake_llm import (
    MAX_CACHE_BREAKPOINTS,
    FakeAsyncAnthropic,
    InvalidTranscript,
    validate_request,
    validate_transcript,
)

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
USER = {"role": "user", "content": "hi"}
EPHEMERAL = {"type": "ephemeral"}


def _request(**overrides):
    return {"model": "m", "max_tokens": 32, "messages": [USER], **overrides}


# --- observed: reproduced from a live 400 --------------------------------

def test_an_empty_content_list_is_refused():
    with pytest.raises(InvalidTranscript, match="non-empty content"):
        validate_transcript([USER, {"role": "assistant", "content": []}, USER])


@pytest.mark.parametrize("value", [0, -1, None, "8000", 1.5])
def test_an_invalid_max_tokens_is_refused(value):
    with pytest.raises(InvalidTranscript, match="max_tokens"):
        validate_request(_request(max_tokens=value))


def test_a_valid_request_goes_through():
    validate_request(_request())


# --- documented: Anthropic's published limits, not observable here -------

def test_a_thinking_block_without_a_signature_is_refused():
    """Documented, not observed. This endpoint accepts it; Anthropic does not,
    and the whole round-41 design depends on the signature surviving."""
    with pytest.raises(InvalidTranscript, match="signature"):
        validate_transcript([
            USER,
            {"role": "assistant", "content": [
                {"type": "thinking", "thinking": "reasoning"}]},
            USER,
        ])


def test_a_thinking_block_with_a_signature_is_accepted():
    validate_transcript([
        USER,
        {"role": "assistant", "content": [
            {"type": "thinking", "thinking": "r", "signature": "sig-abc"}]},
        USER,
    ])


def test_too_many_cache_breakpoints_are_refused():
    over = MAX_CACHE_BREAKPOINTS + 1
    with pytest.raises(InvalidTranscript, match="cache_control"):
        validate_transcript([{
            "role": "user",
            "content": [
                {"type": "text", "text": f"b{i}", "cache_control": EPHEMERAL}
                for i in range(over)
            ],
        }])


def test_exactly_the_limit_is_accepted():
    validate_transcript([{
        "role": "user",
        "content": [
            {"type": "text", "text": f"b{i}", "cache_control": EPHEMERAL}
            for i in range(MAX_CACHE_BREAKPOINTS)
        ],
    }])


def test_the_cache_policy_budget_matches_the_documented_limit():
    """The policy's budget and the provider's ceiling are the same number kept
    in two places on purpose -- see `MAX_CACHE_BREAKPOINTS`. This assertion is
    what makes the duplication safe; if they drift, the policy starts building
    requests the provider will reject."""
    assert DefaultCachePolicy().max_breakpoints == MAX_CACHE_BREAKPOINTS


# --- and the harness must actually satisfy them ---------------------------

@pytest.mark.parametrize("prompt", ["do the thing", "hello", "run a command"])
def test_a_real_turn_builds_a_request_the_strict_provider_accepts(tmp_path, prompt):
    """Not just the transcript -- the annotated request, breakpoints included."""
    sent: list[dict] = []

    class Recording(FakeAsyncAnthropic):
        pass

    session = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        Recording(),
    ).create()
    original = session.agent.client.messages.create

    async def spy(**kwargs):
        sent.append(kwargs)
        return await original(**kwargs)

    session.agent.client.messages.create = spy
    asyncio.run(session.agent.run(prompt))

    assert sent, "no request was made"
    for request in sent:
        validate_request(request)


def test_a_long_conversation_stays_within_the_breakpoint_budget(tmp_path):
    """The policy places breakpoints per block; a long transcript is where an
    off-by-one would show up."""
    messages = []
    for index in range(30):
        messages.append({"role": "assistant", "content": [
            {"type": "tool_use", "id": f"t{index}", "name": "b", "input": {}}]})
        messages.append({"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": f"t{index}", "content": "out"}]})

    _, _, annotated = DefaultCachePolicy().annotate(
        system="sys", tools=[], messages=messages
    )
    validate_transcript(annotated)
