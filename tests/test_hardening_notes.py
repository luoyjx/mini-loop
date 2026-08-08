"""Guards for the claims in `docs/HARDENING_NOTES.md`.

Documentation rots. Every trap recorded there describes a fix that produced no
error when it was missing, so removing one would be just as quiet as the
original defect. These assert the structural facts the document claims, against
the code rather than against anyone's memory -- writing them found that one
claim I had stated for several rounds was checked wrongly, not that the code had
drifted.

Deliberately structural: a behaviour test only covers the path someone thought
of, which is how each of these got in.
"""

import inspect
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parent.parent
NOTES = ROOT / "docs" / "HARDENING_NOTES.md"


def _src(relative: str) -> str:
    return (ROOT / relative).read_text()


def test_the_notes_exist_and_name_their_open_items():
    """A hardening document that omits what is still broken is marketing."""
    text = NOTES.read_text()
    assert "## Still open" in text

    # These terms track gaps that are genuinely still open. A term is removed
    # only when the gap actually closes -- when one narrows instead, the term is
    # replaced by one naming what remains. `cross-process` became `fairness`
    # when leases landed: a second process is now refused, but there is still no
    # queueing, hand-off, or notion of a run within a session.
    for unfinished in ("reconciliation", "resource limits", "fairness", "macOS-only"):
        assert unfinished.lower() in text.lower(), f"{unfinished} dropped from the open list"


# --- trap 1: volatile state out of the cached prefix -----------------------

def test_the_system_prompt_does_not_read_the_todo_board():
    """The board changes per turn; the prefix must not.

    The check is on the board *state*, not the word "TodoWrite" -- the prompt
    legitimately contains a static instruction mentioning it, and a naive
    substring check reports a drift that is not there.
    """
    from mini_loop.prompts import default_system_builder, runtime_facts

    builder = inspect.getsource(default_system_builder)
    assert "todo.render" not in builder and "todo.items" not in builder
    assert "todo.render" in inspect.getsource(runtime_facts)


# --- trap 3/4: the store's contract with the transcript --------------------

def test_the_store_delegates_block_conversion():
    """One converter. A second one fell back to `str(value)` and lost tool calls."""
    from mini_loop.storage import _json_safe
    from mini_loop.fake_llm import ToolUseBlock

    converted = _json_safe(
        {"role": "assistant", "content": [ToolUseBlock("bash", {"command": "x"}, "t1")]}
    )
    assert converted["content"][0]["type"] == "tool_use"
    assert converted["content"][0]["input"] == {"command": "x"}


def test_transcripts_are_epoched():
    from mini_loop.storage import SCHEMA_VERSION

    assert SCHEMA_VERSION >= 2
    assert "epoch" in _src("mini_loop/storage.py")


# --- trap 6: the journal is a replay guard ---------------------------------

def test_the_replay_guard_is_wired_into_the_loop():
    """The pieces existed before; the wire was what was missing."""
    agent = _src("mini_loop/agent.py")
    assert "_REPLAYABLE_STATUSES" in agent
    assert "UNKNOWN_RESULT" in agent
    assert "replayed_action" in agent


# --- trap 7: masking runs both ways ----------------------------------------

def test_arguments_are_masked_as_well_as_output():
    from mini_loop.secrets import SecretRegistry

    assert hasattr(SecretRegistry, "mask_payload")
    assert "mask_payload(call.input)" in _src("mini_loop/agent.py")


def test_what_is_executed_is_never_masked():
    """The boundary that keeps commands working."""
    from mini_loop.secrets import SecretRegistry

    doc = SecretRegistry.mask_payload.__doc__ or ""
    assert "recorded and emitted" in doc and "never what is executed" in doc


# --- trap 8: auth cannot be per-handler ------------------------------------

def test_authentication_is_middleware_with_an_explicit_allowlist():
    server = _src("mini_loop/server.py")
    assert '@app.middleware("http")' in server
    assert "PUBLIC_PATHS" in server


def test_the_sse_exemption_is_scoped_to_streaming():
    server = _src("mini_loop/server.py")
    assert 'path.endswith("/events")' in server, "the query credential lost its scope"


# --- trap 9: the cost guard exists -----------------------------------------

def test_rewrite_detection_does_not_hash_the_prefix():
    from mini_loop.session import AgentSession

    detector = inspect.getsource(AgentSession._transcript_was_rewritten)
    assert "is not reference" in detector, "back to hashing every event?"
    assert "_digest" not in detector


# --- trap 10: identity -----------------------------------------------------

def test_healthz_carries_a_build_fingerprint():
    from mini_loop.identity import build_id

    assert len(build_id()) == 12
    assert "runtime_identity(" in _src("mini_loop/server.py")


# --- the guards themselves --------------------------------------------------

@pytest.mark.parametrize(
    "path",
    [
        "tests/test_harness.py",       # construction
        "tests/test_composition.py",   # cost + composition
        "tests/test_fullstack.py",     # everything at once
        "tests/test_auth.py",          # coverage
        "tests/test_block_normalization.py",  # the recurring root cause
    ],
)
def test_each_guard_still_exists(path):
    """The table in the notes names these; a missing one is a silent gap."""
    assert (ROOT / path).exists(), f"{path} is referenced by the hardening notes"
