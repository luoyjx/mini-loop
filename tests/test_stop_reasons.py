"""The loop must not read every stop reason it does not know as "done".

The loop decides by *content*: run the `tool_use` blocks, stop when there are
none. That is deliberately robust to a provider disagreeing with itself about
`end_turn` versus `tool_use`, and it was written that way for good reason.

But "no tool blocks" and "the turn is over" are different claims, and collapsing
them gave the harness an implicit allowlist where everything outside it silently
meant completion:

    pause_turn  -> "Let me search for that"   returned as the final answer
    refusal     -> ""                          returned with no explanation
    unknown     -> "partial"                   returned as if complete

`pause_turn` is the sharp one. It means the model was interrupted mid-work and
is asking to be sent back; it arrives with no tool blocks, so a paused turn was
handed to the caller as a finished one. The fragment even reads like an answer.
"""

import pathlib
import tempfile

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.agent import (
    KNOWN_STOP_REASONS,
    MAX_RESUMPTIONS,
    REFUSAL_NOTICE,
    RESUMABLE_STOP_REASONS,
)
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


async def _run(tmp_path, turns, prompt="go"):
    """Run one turn against a scripted provider; return (answer, event kinds)."""

    seen: list[str] = []
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(responder=scripted(turns)),
        tool_registry=full_registry(),
    )
    session = manager.create()
    original = session.agent._send

    async def spy(kind, **fields):
        seen.append(kind)
        return await original(kind, **fields)

    session.agent._send = spy
    return await session.agent.run(prompt), seen


@pytest.mark.asyncio
async def test_a_paused_turn_is_resumed_not_returned(tmp_path):
    answer, events = await _run(tmp_path, [
        ([text("Let me search for that")], "pause_turn"),
        ([text("Found it: 42")], "end_turn"),
    ])
    assert answer == "Found it: 42", "the paused fragment was returned as the answer"
    assert "turn_paused" in events


@pytest.mark.asyncio
async def test_resumption_is_bounded(tmp_path):
    """Each resumption is a real request, so an always-pausing provider is spend."""

    answer, events = await _run(tmp_path, [([text("p")], "pause_turn")] * 40)
    assert events.count("turn_paused") == MAX_RESUMPTIONS
    assert "provider_stop_unhandled" in events
    assert answer == "p", "the caller still gets what was produced"


@pytest.mark.asyncio
async def test_a_refusal_is_not_an_empty_answer(tmp_path):
    """A refusal carries no content, so the caller got `""` and no reason."""

    answer, events = await _run(tmp_path, [([], "refusal")])
    assert "provider_refusal" in events
    assert answer == REFUSAL_NOTICE


@pytest.mark.asyncio
async def test_a_refusal_does_not_overwrite_text_the_model_did_send(tmp_path):
    answer, _ = await _run(tmp_path, [([text("I can help with the first part")], "refusal")])
    assert answer == "I can help with the first part"


@pytest.mark.asyncio
async def test_an_unrecognized_reason_is_reported_but_still_answers(tmp_path):
    """Refusing to return a usable answer would be worse than returning it."""

    answer, events = await _run(
        tmp_path, [([text("partial")], "model_context_window_exceeded")]
    )
    assert answer == "partial"
    assert "provider_stop_unhandled" in events


@pytest.mark.asyncio
async def test_an_ordinary_turn_reports_nothing(tmp_path):
    """Not vacuous: the events above must not fire on every turn."""

    answer, events = await _run(tmp_path, [([text("plain answer")], "end_turn")])
    assert answer == "plain answer"
    assert not [e for e in events
                if e in ("turn_paused", "provider_refusal", "provider_stop_unhandled")]


def test_every_resumable_reason_is_also_a_known_one():
    """A reason the loop resumes on but does not recognize would double-report."""

    assert RESUMABLE_STOP_REASONS <= KNOWN_STOP_REASONS


def test_every_known_reason_is_exercised_here():
    """An entry nothing acts on is a vacuous entry.

    `refusal` was named in the set first and handled second, and in between it
    was "known" only in the sense that it no longer triggered the unknown-reason
    report -- naming it had made the silence *quieter*. So each name must point
    at behaviour.

    Checked against this file rather than the source, because "acted on" is not
    a syntactic property: `end_turn` and `stop_sequence` are handled correctly
    by the content rule (no tool blocks, so stop) and have no named branch at
    all. What can be required is that somebody wrote down what each one does.
    """

    here = pathlib.Path(__file__)
    tests = here.parent
    # `max_tokens` is acted on in recovery.py (escalate the budget and continue),
    # not in the loop, so it is pinned in its own file. A pointer like that is a
    # claim, so the file has to exist and has to mention the reason.
    elsewhere = {"max_tokens": tests / "test_token_escalation.py"}
    missing = []
    for reason in KNOWN_STOP_REASONS:
        target = elsewhere.get(reason, here)
        if not target.exists() or reason not in target.read_text():
            missing.append(f"{reason} (looked in {target.name})")
    assert not missing, f"named as known but never exercised: {sorted(missing)}"


@pytest.mark.asyncio
async def test_a_stop_sequence_ends_the_turn_quietly(tmp_path):
    """One of the two reasons handled by the content rule rather than a branch."""

    answer, events = await _run(tmp_path, [([text("cut here")], "stop_sequence")])
    assert answer == "cut here"
    assert "provider_stop_unhandled" not in events
