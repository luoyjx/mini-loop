"""Every loop counter must be in the scope its behaviour needs, and say which.

Round 84 added `_resumptions` to bound how many times a paused turn may be sent
back. It was initialized in `__init__` beside two counters that *are* reset per
turn, and never reset -- which silently made it a **session lifetime** budget:

    turn  7  resumptions=8   -> 'ANSWER 7'
    turn  8  resumptions=9   -> 'thinking about 8'   <- fragment
    turn 10  resumptions=10  -> 'thinking about 9'   <- fragment

After eight paused turns spread over an afternoon, every later pause was handed
to the caller as a finished answer: exactly the bug the counter was added to
prevent, appearing only in long sessions nobody reproduces. A one-turn test
could not have seen it, and did not.

Not every counter should reset -- `_rounds_without_todo` tracks a plan that
outlives a turn on purpose. The defect was not the scope, it was that nothing
stated the scope, so the wrong one was invisible. This makes each choice
explicit and checks it, so the next counter cannot get session lifetime by
being declared next to the others.
"""

import ast
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.agent import MAX_RESUMPTIONS
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text

PACKAGE = pathlib.Path(__file__).resolve().parent.parent / "mini_loop"
SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"

#: Counters that must be cleared at the start of each user turn, and why.
PER_TURN = {
    "_rounds_without_tools": "a new user turn is a new intent",
    "_stuck_nudges": "the nudge budget is spent per user turn",
    "_resumptions": "a pause budget that accrues across turns stops working",
}

#: Counters that deliberately outlive a turn, and why.
PER_SESSION = {
    "_rounds_without_todo": "a plan spans turns; drifting off it does too",
    "_pending_compact": "a flag for the current loop pass, cleared where it is read",
}


def _counters() -> dict[str, str]:
    """`self._x = 0 / False` assignments in `Agent.__init__`."""

    tree = ast.parse((PACKAGE / "agent.py").read_text())
    agent = next(n for n in ast.walk(tree)
                 if isinstance(n, ast.ClassDef) and n.name == "Agent")
    init = next(n for n in agent.body
                if isinstance(n, ast.FunctionDef) and n.name == "__init__")
    found = {}
    for node in ast.walk(init):
        if not (isinstance(node, ast.Assign) and len(node.targets) == 1):
            continue
        target = node.targets[0]
        if (isinstance(target, ast.Attribute)
                and isinstance(target.value, ast.Name)
                and target.value.id == "self"
                and target.attr.startswith("_")):
            value = ast.unparse(node.value)
            if value in ("0", "False") or value.isdigit():
                found.setdefault(target.attr, value)
    return found


def _turn_body() -> list:
    """`run` plus whatever it delegates the turn to.

    Round 87 split `run()` into a lock wrapper and `_run_one_turn()`, which
    moved every reset out of the method this guard inspected -- and the guard
    failed, which is the point. Following the delegation keeps it pointed at
    the turn body wherever that body lives.
    """

    tree = ast.parse((PACKAGE / "agent.py").read_text())
    agent = next(n for n in ast.walk(tree)
                 if isinstance(n, ast.ClassDef) and n.name == "Agent")
    methods = {n.name: n for n in agent.body
               if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef))}
    bodies, pending, seen = [], ["run"], set()
    while pending:
        name = pending.pop()
        if name in seen or name not in methods:
            continue
        seen.add(name)
        bodies.append(methods[name])
        # Only the *prologue* delegates. Walking every call transitively reaches
        # the whole loop, where `_rounds_without_todo` and `_pending_compact`
        # are legitimately assigned mid-turn -- and then "reset at the start of
        # a turn" and "written to at some point during one" stop being
        # different claims, which is the entire distinction being checked.
        for sub in ast.walk(methods[name]):
            if not isinstance(sub, (ast.For, ast.While, ast.AsyncFor)):
                continue
            for inner in ast.walk(sub):
                inner._inside_loop = True
        for sub in ast.walk(methods[name]):
            called = getattr(getattr(sub, "func", None), "attr", None)
            if (isinstance(sub, ast.Call) and called in methods
                    and not getattr(sub, "_inside_loop", False)):
                pending.append(called)
    return bodies


def _prologue_assignments(body) -> set:
    """Attribute assignments outside any loop -- i.e. once per turn."""

    inside_loop = set()
    for node in ast.walk(body):
        if isinstance(node, (ast.For, ast.While, ast.AsyncFor)):
            inside_loop.update(id(inner) for inner in ast.walk(node))
    return {
        node.targets[0].attr
        for node in ast.walk(body)
        if isinstance(node, ast.Assign) and len(node.targets) == 1
        and id(node) not in inside_loop
        and isinstance(node.targets[0], ast.Attribute)
        and isinstance(node.targets[0].value, ast.Name)
        and node.targets[0].value.id == "self"
    }


def _reset_in_run() -> set[str]:
    reset = set()
    for body in _turn_body():
        reset |= _prologue_assignments(body)
    return reset


def test_the_scan_finds_the_counters():
    """A scan matching nothing would pass every case below forever."""

    found = _counters()
    assert len(found) >= 4, f"the counter scan sees {found}"
    assert "_resumptions" in found


def test_every_counter_declares_its_scope():
    classified = set(PER_TURN) | set(PER_SESSION)
    unclassified = sorted(set(_counters()) - classified)
    assert not unclassified, (
        "these loop counters do not say whether they are per turn or per "
        f"session, which is how `_resumptions` got the wrong one: {unclassified}"
    )


def test_per_turn_counters_are_actually_reset():
    reset = _reset_in_run()
    missing = sorted(name for name in PER_TURN if name not in reset)
    assert not missing, f"declared per-turn but never cleared in the turn body: {missing}"


def test_per_session_counters_are_not_reset():
    """Otherwise the classification is decoration rather than a description."""

    reset = _reset_in_run()
    contradicted = sorted(name for name in PER_SESSION if name in reset)
    assert not contradicted, f"declared per-session but cleared per turn: {contradicted}"


def test_the_classification_has_no_dead_entries():
    live = set(_counters())
    stale = sorted((set(PER_TURN) | set(PER_SESSION)) - live)
    assert not stale, f"classified but no longer a counter: {stale}"


@pytest.mark.asyncio
async def test_the_pause_budget_survives_a_long_session(tmp_path):
    """The behaviour, over more turns than the budget.

    The unit checks above would all pass on a counter that resets in the wrong
    place; this is the one that failed before the fix.
    """

    script = []
    for i in range(MAX_RESUMPTIONS + 4):
        script += [([text(f"thinking {i}")], "pause_turn"),
                   ([text(f"ANSWER {i}")], "end_turn")]
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(responder=scripted(script)),
        tool_registry=full_registry(),
    )
    session = manager.create()

    for i in range(MAX_RESUMPTIONS + 4):
        answer = await session.agent.run(f"question {i}")
        assert answer == f"ANSWER {i}", (
            f"turn {i} returned the paused fragment: {answer!r}"
        )
