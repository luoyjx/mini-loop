"""An agent that does not know it is confined tries to escape.

Round 61's lesson was that every mechanical instrument here checks the code
against itself, and the one defect it could not have found came from reading
what a real model said about its own situation. So this round asked one
directly.

Its answers on tools, skills and working directory matched reality exactly --
a negative result worth recording. The fourth question did not:

    "the description alone doesn't state any sandboxing, resource limits, or
     network restrictions. So I ... cannot confirm actual confinement without
     testing"

The cost is measurable. Given a write outside its workspace, against the real
endpoint:

    prompt does NOT mention confinement : 7 bash attempts
    prompt states the confinement       : 2 bash attempts

The unaware agent grepped `/proc/self/status` for seccomp, tried invoking
`sandbox-exec` itself, and probed writes in three directories -- it treated the
boundary as a broken tool to diagnose, which is the reasonable move when nothing
told it otherwise.

Confinement is stable for an agent's lifetime, so it goes in the system prompt
and not the runtime facts -- the opposite of the memory index in round 61, and
for the same reason: the cached prefix must hold still.
"""

import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.prompts import default_system_builder, runtime_facts
from mini_loop.sandbox import (
    NullSandbox,
    SeatbeltSandbox,
    UnavailableSandbox,
    default_sandbox,
)

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _agent(tmp_path, sandbox=None):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
        sandbox=sandbox,
    ).create().agent


# --- every backend answers the question ----------------------------------

@pytest.mark.parametrize("factory,expected", [
    (lambda p: NullSandbox(), False),
    (lambda p: UnavailableSandbox("no backend on this platform"), False),
    (lambda p: SeatbeltSandbox(writable_roots=[p]), True),
])
def test_each_backend_declares_whether_it_confines(tmp_path, factory, expected):
    """Declared, not inferred from the class name, so a new backend has to say."""
    assert factory(tmp_path).confined is expected


def test_every_sandbox_class_declares_it():
    """The family guard: a backend added later must answer too."""
    import inspect

    from mini_loop import sandbox as module

    classes = [
        value for _, value in inspect.getmembers(module, inspect.isclass)
        if hasattr(value, "argv") and value.__module__ == module.__name__
        # The Protocol declares `confined: bool` as an annotation and must not
        # assign one -- it is the contract, not an implementation of it.
        and not getattr(value, "_is_protocol", False)
    ]
    assert classes, "no sandbox classes found -- the scan broke"
    missing = [c.__name__ for c in classes if not isinstance(
        getattr(c, "confined", None), bool)]
    assert not missing, f"these do not declare `confined`: {missing}"


# --- the prompt says so, accurately --------------------------------------

def test_a_confined_agent_is_told(tmp_path):
    workspace = tmp_path / "ws"
    sandbox = (
        SeatbeltSandbox(writable_roots=[workspace])
        if SeatbeltSandbox.available() else None
    )
    if sandbox is None:
        pytest.skip("macOS Seatbelt only")

    prompt = default_system_builder(_agent(tmp_path, sandbox))
    assert "confined" in prompt
    assert "seatbelt" in prompt
    assert "not a broken tool" in prompt, (
        "knowing there is a boundary is only useful with what to do about it"
    )


@pytest.mark.parametrize("sandbox", [
    None,
    NullSandbox(),
    UnavailableSandbox("no backend on this platform"),
])
def test_an_unconfined_agent_is_not_told_otherwise(tmp_path, sandbox):
    """Claiming confinement that is not there is worse than saying nothing."""
    assert "Shell commands are confined" not in default_system_builder(
        _agent(tmp_path, sandbox)
    )


def test_the_statement_matches_the_sandbox_actually_in_use(tmp_path):
    """Round 26's rule: report what is running, not what was configured."""
    workspace = tmp_path / "ws"
    if not SeatbeltSandbox.available():
        pytest.skip("macOS Seatbelt only")
    agent = _agent(tmp_path, default_sandbox(workspace))
    assert agent.sandbox.describe in default_system_builder(agent)


# --- and it belongs in the prefix, not the reminder ----------------------

def test_confinement_is_stable_across_turns(tmp_path):
    """It goes in the system prompt precisely because it does not change."""
    if not SeatbeltSandbox.available():
        pytest.skip("macOS Seatbelt only")
    agent = _agent(tmp_path, default_sandbox(tmp_path / "ws"))
    before = agent.refresh_system()
    agent.todo.update([{"content": "t", "status": "pending", "activeForm": "t"}])
    assert agent.refresh_system() == before


def test_confinement_is_not_repeated_in_runtime_facts(tmp_path):
    """The memory index went the other way for the same reason (round 61)."""
    if not SeatbeltSandbox.available():
        pytest.skip("macOS Seatbelt only")
    agent = _agent(tmp_path, default_sandbox(tmp_path / "ws"))
    assert "confined" not in (runtime_facts(agent) or "")
