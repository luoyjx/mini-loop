"""Two rounds, two defects, one shape: a caller threw away an error.

Round 55: `teams.broadcast` discarded `bus.send`'s refusal string and reported
"Broadcast to 3 teammate(s)" while delivering none. Round 56: `worktrees.remove`
discarded git's own check by passing `--force` unconditionally. Both were found
by reading a module that coverage flagged, which does not scale and did not
generalise.

It is mechanically detectable. A call whose result can carry an error, used as a
bare statement, is a discarded error. Scanning for that found **eight more in
`manager.py`** -- every `bus.send` on the real team-coordination path, which is
exactly the code round 55 could not reach because it needs a live manager.

The consequence is the worst of the three:

    a teammate's finished result: 26,032 chars
    bus.send returned           : 'Error: message is 26,032 characters...'
    lead's inbox                : 0 messages

The teammate did the work, the manager dropped the refusal, and the lead is
never told the task completed.

Results are now truncated rather than refused -- the opposite of the call made
for a cron prompt in round 47, and for the same reason: a truncated report still
carries most of the work, while a truncated instruction that still executes is
worse than one that never ran. `request_plan` sends an instruction, so it keeps
refusing and returns that refusal to its caller.
"""

import ast
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.teams import MessageBus, team_key

PACKAGE = pathlib.Path(__file__).resolve().parent.parent / "mini_loop"
SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"

#: Discards that are deliberate, with the reason. A new one fails the scan until
#: it is either fixed or explained here.
ALLOWED_DISCARDS = {
    ("builtins.py", "install_tasks"): "returns the registry; the Error strings are in nested handlers",
    ("builtins.py", "install_cron"): "returns the registry",
    ("builtins.py", "install_teams"): "returns the registry",
    ("builtins.py", "install_worktrees"): "returns the registry",
    ("builtins.py", "install_mcp"): "returns the registry",
    ("builtins.py", "install_plan_mode"): "registers tools; the Error strings are in nested handlers",
    ("builtins.py", "install_goals"): "registers tools; the Error strings are in nested handlers",
    ("builtins.py", "install_diagnostics"): "registers tools; the Error strings are in nested handlers",
    ("builtins.py", "install_session_query"): "registers tools; the Error strings are in nested handlers",
    ("__main__.py", "run"): "uvicorn.run, unrelated to this package",
    ("runner.py", "run"): "workflow runner records the outcome through its own store",
    ("worktrees.py", "bind_worktree"): "binding is advisory; creation already succeeded",
    ("server.py", "send"): "the ASGI send callable returns None; it is awaited for its side effect (emitting the 413), not for a result -- a different `send` than MessageBus.send",
    ("verified_loop.py", "remove"): "list.remove returns None (clearing a blocker in the patch fold); a different `remove` than the error-returning worktree one",
}


def _error_returning_functions() -> dict[str, set[str]]:
    found: dict[str, set[str]] = {}
    markers = ('"Error:', "'Error:", '"Refusing:', "'Refusing:", '"No ')
    for path in sorted(PACKAGE.rglob("*.py")):
        tree = ast.parse(path.read_text())
        for node in ast.walk(tree):
            if not isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
                continue
            for sub in ast.walk(node):
                if isinstance(sub, ast.Return) and sub.value is not None:
                    if any(m in ast.unparse(sub.value) for m in markers):
                        found.setdefault(node.name, set()).add(path.name)
    return found


def _discarded_calls():
    error_returning = _error_returning_functions()
    discarded = []
    for path in sorted(PACKAGE.rglob("*.py")):
        tree = ast.parse(path.read_text())
        for node in ast.walk(tree):
            if not isinstance(node, ast.Expr):
                continue
            call = node.value
            if isinstance(call, ast.Await):
                call = call.value
            if not isinstance(call, ast.Call):
                continue
            name = getattr(call.func, "attr", None) or getattr(call.func, "id", None)
            if name in error_returning:
                discarded.append((path.name, name, node.lineno))
    return discarded


def test_no_error_carrying_result_is_silently_discarded():
    """The scan that found eight of these, kept as a guard."""
    offenders = [
        f"{path}:{line} discards {name}()"
        for path, name, line in _discarded_calls()
        if (path, name) not in ALLOWED_DISCARDS
    ]
    assert not offenders, (
        "these calls can return an error and the result is thrown away:\n  "
        + "\n  ".join(offenders)
        + "\nFix it, or add it to ALLOWED_DISCARDS with the reason."
    )


def test_the_scan_finds_something():
    """A scan matching nothing would pass the case above forever."""
    assert _error_returning_functions(), "no error-returning functions found"
    assert _discarded_calls(), "no discards found at all -- the scan broke"


def test_the_allow_list_has_no_dead_entries():
    live = {(path, name) for path, name, _ in _discarded_calls()}
    stale = sorted(entry for entry in ALLOWED_DISCARDS if entry not in live)
    assert not stale, f"allowed but no longer discarded anywhere: {stale}"


# --- the behaviour ---------------------------------------------------------

@pytest.fixture
def manager(tmp_path):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
    )


def _long_result():
    return "Here is the completed analysis.\n" + ("finding line\n" * 2000)


def test_a_teammates_finished_work_reaches_the_lead(manager):
    result = _long_result()
    assert len(result) > MessageBus.MAX_CONTENT

    manager._deliver(team_key("t", "alice"), team_key("t", "lead"), result, "result")
    inbox = manager.bus.read(team_key("t", "lead"))

    assert len(inbox) == 1, "the completed work was dropped"
    assert inbox[0]["content"].startswith("Here is the completed analysis.")


def test_a_truncated_delivery_says_so(manager):
    manager._deliver(team_key("t", "alice"), team_key("t", "lead"),
                     _long_result(), "result")
    body = manager.bus.read(team_key("t", "lead"))[0]["content"]
    assert "truncated" in body
    assert f"{len(_long_result()):,}" in body


def test_an_ordinary_result_is_untouched(manager):
    manager._deliver(team_key("t", "alice"), team_key("t", "lead"),
                     "task done, all green", "result")
    body = manager.bus.read(team_key("t", "lead"))[0]["content"]
    assert body == "task done, all green"
    assert manager.bus.problems == []


def test_a_delivery_the_bus_still_refuses_is_recorded(manager):
    """Truncation handles size; a bad mailbox key is still a refusal."""
    manager._deliver("alice", "../../escape", "anything", "result")
    assert any("failed" in problem for problem in manager.bus.problems)


def test_an_instruction_is_still_refused_rather_than_truncated(manager):
    """`request_plan` carries an instruction, so round 47's call stands."""
    manager._sessions.clear()
    refused = manager.request_plan("t", "nobody", "task")
    assert refused.startswith("Error:")
