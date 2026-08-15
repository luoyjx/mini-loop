"""Which entry point a caller uses is a durability decision, not a style one.

Round 88 corrected a claim I had made from the wrong layer: a defect measured
through `Agent.run()` did not exist through `session.run()`, because
`AgentSession.lock` had always serialized runs. The mistake was cheap to make
and would be cheap to repeat -- the suite calls `agent.run()` 54 times and
`session.run()` 42 -- so this pins the distinction instead of relying on
remembering it.

The two paths are not equivalent, measured rather than assumed:

    observable        agent.run   session.run
    run_count                 0             1
    trajectory                0             1
    backlog                  10            14

So a caller on the inner path gets no trajectory, no run count and no
session-level events. That is right for the two production callers that use it
-- a subagent and a workflow node are agents that have no session -- and it
would be wrong for anything serving a user, which is what this guards.

Four things checked here came back negative and are recorded so a later round
does not re-walk them: the session's bookkeeping survives an external cancel and
a provider exception (`status`, `run_count` and the trajectory all close
cleanly); the round-88 cancellation repair reaches the *persisted* transcript,
so memory and disk agree; and no HTTP route reaches an agent directly.
"""

import ast
import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, validate_transcript
from mini_loop.storage import SQLiteStateStore

PACKAGE = pathlib.Path(__file__).resolve().parent.parent / "mini_loop"
SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"

#: Modules allowed to call `agent.run()` directly, and why. Everything serving a
#: user goes through a session, or it silently loses persistence and recording.
MAY_BYPASS_THE_SESSION = {
    "agent.py": "spawning a subagent, which is an agent with no session",
    "runner.py": "a workflow node runs on a worker agent with no session",
    "subagents.py": (
        "the in-process provider extracted from agent.py (round 183); the "
        "child is deliberately sessionless, same rationale as agent.py"
    ),
}


def _direct_agent_runs() -> dict[str, list[int]]:
    found: dict[str, list[int]] = {}
    for path in sorted(PACKAGE.rglob("*.py")):
        try:
            tree = ast.parse(path.read_text())
        except SyntaxError:                                  # pragma: no cover
            continue
        for node in ast.walk(tree):
            if not (isinstance(node, ast.Call)
                    and isinstance(node.func, ast.Attribute)
                    and node.func.attr == "run"):
                continue
            target = ast.unparse(node.func)
            if any(k in target for k in ("asyncio", "subprocess", "uvicorn", "loop.")):
                continue
            if target.endswith("session.run") or ".recovery." in target:
                continue
            if target.endswith("tool.run") or "_mgr" in target or "manager_for" in target:
                continue
            found.setdefault(path.name, []).append(node.lineno)
    return found


def _manager(tmp_path, client=None, store=None):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 memory_root=tmp_path / "mem", skills_dir=SKILLS),
        client or FakeAsyncAnthropic(),
        tool_registry=full_registry(),
        **({"state_store": store} if store is not None else {}),
    )


# -- the guard ------------------------------------------------------------

def test_the_scan_finds_the_direct_callers():
    """A scan matching nothing would pass the case below forever."""

    found = _direct_agent_runs()
    assert found, "the entry-point scan sees no direct agent.run() anywhere"


def test_only_sessionless_callers_bypass_the_session():
    offenders = sorted(
        f"{name}:{lines}" for name, lines in _direct_agent_runs().items()
        if name not in MAY_BYPASS_THE_SESSION
    )
    assert not offenders, (
        "these call agent.run() directly, so their turns are not counted, not "
        f"recorded to a trajectory, and not serialized by the session: {offenders}"
    )


def test_the_exemptions_are_still_callers():
    """An exemption for a caller that no longer exists is a stale claim."""

    live = set(_direct_agent_runs())
    stale = sorted(set(MAY_BYPASS_THE_SESSION) - live)
    assert not stale, f"exempted but no longer calls agent.run(): {stale}"


# -- the negatives, kept executable ---------------------------------------

@pytest.mark.asyncio
async def test_the_session_path_records_what_the_agent_path_does_not(tmp_path):
    """The measured delta, so "equivalent" cannot be assumed again."""

    inner = _manager(tmp_path / "a").create()
    await inner.agent.run("do the thing")
    outer = _manager(tmp_path / "b").create()
    await outer.run("do the thing")

    assert (inner.run_count, inner._trajectory_count) == (0, 0)
    assert (outer.run_count, outer._trajectory_count) == (1, 1)


@pytest.mark.asyncio
async def test_session_bookkeeping_survives_a_provider_exception(tmp_path):
    def boom(kwargs):
        raise RuntimeError("provider exploded")

    session = _manager(tmp_path, FakeAsyncAnthropic(responder=boom)).create()
    answer = await session.run("work")

    assert "[Error]" in answer
    assert session.status == "idle"
    assert session._active_trajectory_id is None
    assert session.run_count == 1


@pytest.mark.asyncio
async def test_the_cancellation_repair_reaches_disk(tmp_path):
    """Round 88 repaired memory; a restart reads the store."""

    store = SQLiteStateStore(tmp_path / "state.db")
    # A slow model call keeps the turn in flight so the cancel lands *inside*
    # it. `run` does several awaits before the agent writes its first message
    # (trajectory start, status emits), and a fixed short sleep raced them: a
    # cancel during that setup leaves an empty, legitimately unrepairable
    # transcript, which then fails `validate_transcript`. Poll for the first
    # message instead, so the mid-turn point is deterministic, not a timing bet.
    manager = _manager(tmp_path, FakeAsyncAnthropic(delay=0.2), store=store)
    session = manager.create()

    task = asyncio.create_task(session.run("do work"))
    for _ in range(400):
        if session.agent.messages or task.done():
            break
        await asyncio.sleep(0.005)
    assert not task.done(), "the turn finished before it could be cancelled mid-flight"
    assert session.agent.messages, "the turn never reached a mid-turn state to repair"

    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task

    validate_transcript(session.agent.messages)
    validate_transcript(store.load_messages(session.id))
    assert len(store.load_messages(session.id)) == len(session.agent.messages)
