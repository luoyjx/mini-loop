"""A recovery path that could only ever make things worse.

`stop_reason == "max_tokens"` means the answer was cut off. Recovery escalated
`max_tokens` from 8,000 to 64,000 and retried. Against the real SDK that call
never leaves the process:

    max_tokens=8,000   -> OK in 0.8s
    max_tokens=64,000  -> ValueError: Streaming is required for operations that
                          may take longer than 10 minutes.

The error is raised before any request is sent, so it is neither transient nor
fixable by shrinking the prompt. It fell through to `raise`. The path built to
*rescue* a truncated answer was converting a recoverable truncation into a
failed turn — and every test passed, because the offline model accepted any
budget. Round 30's finding, in a new place: what the stand-in does not
reproduce, the suite cannot check.

The ceiling is per-model and moves between SDK versions
(`anthropic._constants.MODEL_NONSTREAMING_TOKENS`), so it is read rather than
hardcoded, and the refusal is also handled as its own error class — when the SDK
has no listed limit it decides from an estimated duration, and the only way to
learn that is to be told.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, text
from mini_loop.recovery import (
    DEFAULT_MAX_TOKENS,
    ESCALATED_MAX_TOKENS,
    DefaultRecovery,
    is_prompt_too_long,
    is_streaming_required,
    is_transient,
    nonstreaming_ceiling,
)

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
STREAMING_REQUIRED = ValueError(
    "Streaming is required for operations that may take longer than 10 minutes."
)


def _session(tmp_path, turns, *, ceiling=8192, **kwargs):
    state = {"i": 0}

    def responder(request):
        index = state["i"]
        state["i"] += 1
        return turns[index] if index < len(turns) else ([text("done")], "end_turn")

    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(responder=responder, nonstreaming_ceiling=ceiling),
        recovery=DefaultRecovery(**kwargs),
    ).create()


TRUNCATED = [
    ([text("PART-ONE. ")], "max_tokens"),
    ([text("PART-TWO.")], "end_turn"),
]


# --- classification -------------------------------------------------------

def test_the_refusal_is_its_own_error_class():
    """It is not transient and not a prompt-size problem, so it needs a class."""
    assert is_streaming_required(STREAMING_REQUIRED)
    assert not is_transient(STREAMING_REQUIRED)
    assert not is_prompt_too_long(STREAMING_REQUIRED)


def test_the_ceiling_is_read_from_the_sdk_not_guessed():
    from anthropic._constants import MODEL_NONSTREAMING_TOKENS

    for model, limit in list(MODEL_NONSTREAMING_TOKENS.items())[:3]:
        assert nonstreaming_ceiling(model) == limit
    assert nonstreaming_ceiling("a-model-with-no-listed-limit") is None


def test_an_unlisted_model_is_not_given_a_made_up_limit():
    """`None` means "the SDK decides"; inventing a number here would be a guess
    that is wrong on some model or some upgrade."""
    assert nonstreaming_ceiling("") is None


# --- the behaviour that was broken ----------------------------------------

def test_a_truncated_answer_completes_instead_of_failing(tmp_path):
    """With no useful headroom, continuation keeps what was already produced.

    Escalation regenerates from scratch and throws the partial away, which only
    pays when the new budget is meaningfully bigger. A ceiling of 8192 against a
    budget of 8000 is not, so this must not escalate.
    """
    session = _session(tmp_path, TRUNCATED)
    answer = asyncio.run(session.agent.run("write something long"))
    assert answer == "PART-ONE. PART-TWO."


def test_escalation_never_asks_for_more_than_the_call_can_carry(tmp_path):
    """The direct regression: 64,000 on a non-streaming call is a hard error."""
    seen: list[int] = []

    def responder(request):
        seen.append(int(request.get("max_tokens") or 0))
        return ([text("x. ")], "max_tokens") if len(seen) < 2 else ([text("y.")], "end_turn")

    session = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(responder=responder, nonstreaming_ceiling=8192),
        recovery=DefaultRecovery(),
    ).create()
    asyncio.run(session.agent.run("write something long"))
    assert seen, "no request was made"
    assert max(seen) <= 8192, f"asked for {max(seen)}, above the non-streaming ceiling"


def test_the_refusal_is_survivable_when_the_ceiling_is_unknown(tmp_path):
    """No listed limit, so the budget is only discovered by being refused."""
    attempts: list[int] = []

    class Refusing(FakeAsyncAnthropic):
        pass

    session = _session(tmp_path, TRUNCATED, ceiling=None)
    client = session.agent.client
    original = client.messages.create

    async def create(**kwargs):
        attempts.append(int(kwargs.get("max_tokens") or 0))
        if int(kwargs.get("max_tokens") or 0) > 8192:
            raise STREAMING_REQUIRED
        return await original(**kwargs)

    client.messages.create = create
    answer = asyncio.run(session.agent.run("write something long"))
    assert answer, "the turn failed on a recoverable refusal"
    assert any(a > 8192 for a in attempts), "the escalation was never attempted"
    assert attempts[-1] <= 8192, "it never came back down"


def test_escalation_is_still_possible_when_there_is_headroom(tmp_path):
    """The check must not disable the feature wherever it is legitimate."""
    seen: list[int] = []

    def responder(request):
        seen.append(int(request.get("max_tokens") or 0))
        return ([text("x. ")], "max_tokens") if len(seen) < 2 else ([text("y.")], "end_turn")

    session = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(responder=responder, nonstreaming_ceiling=ESCALATED_MAX_TOKENS),
        recovery=DefaultRecovery(),
    ).create()
    asyncio.run(session.agent.run("write something long"))
    assert max(seen) > DEFAULT_MAX_TOKENS, "escalation stopped happening entirely"


def test_the_offline_model_enforces_the_ceiling(tmp_path):
    """The stand-in reproduces the constraint, or this hides again."""
    client = FakeAsyncAnthropic(nonstreaming_ceiling=8192)
    with pytest.raises(ValueError, match="[Ss]treaming is required"):
        asyncio.run(client.messages.create(
            model="m", max_tokens=64_000,
            messages=[{"role": "user", "content": "hi"}],
        ))
