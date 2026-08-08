"""A tool that takes an id must scope it to the caller.

Rounds 74 to 79 found four cross-tenant defects and each was the same shape: an
operation taking a caller-supplied id without checking whose it was. The
trajectory fetch, the trajectory listing, session deletion, cron cancellation --
four objects, four rounds, one mistake.

Round 74 built an AST guard for HTTP *routes*. Agent-facing *tools* are the
other way in, and `cancel_cron` slipped past because nothing looked there. This
is that guard for the tool surface: a handler taking an id must reach for the
caller's identity, so the next one fails a test instead of shipping.

The rest of this file records what the sweep found, as negatives, because after
four findings the interesting claim is where the shape stops:

* `team_id` is per session, so two callers cannot collide on a mailbox --
  checked behaviourally, where round 79 had only read the source.
* `WorkflowService.get` and `wait` take an unscoped `run_id`, and neither is
  reachable from a tool or a route; the agent-facing `status` and `cancel` both
  pass `session_id`.
* `TrajectoryStore` and `TaskStore` are scoped at the HTTP layer and by
  workspace respectively.
"""

import ast
import inspect
import pathlib

import pytest

from mini_loop.builtins import full_registry

PACKAGE = pathlib.Path(__file__).resolve().parent.parent / "mini_loop"

#: Parameter names that name something belonging to somebody.
ID_PARAMS = {"run_id", "job_id", "trajectory_id", "session_id", "task_id", "tid"}

#: How a handler can establish who is asking.
SCOPING = ("session_id", "state.get(\"session\")", "state[\"session\"]",
           "_self_key", "agent_name", "team_id", "owner")

#: Tools whose id names something the caller already owns by construction, with
#: the reason. Anything else must scope.
NOT_A_FOREIGN_ID = {
    # A task id is looked up in the caller's own workspace store.
    "get_task": "TaskStore is per-workspace",
    "claim_task": "TaskStore is per-workspace",
    "complete_task": "TaskStore is per-workspace",
    "create_task": "creates, does not address",
    # Resolves the id against `board(ctx)`, which is the caller's own store
    # (`team_workspace` is a parent session's workspace, so a team shares a
    # board and two callers do not). Verified below rather than asserted.
    "create_worktree": "resolved against the caller's own board",
}


def _handlers():
    registry = full_registry()
    for name in registry.names():
        tool = registry.get(name)
        handler = getattr(tool, "handler", None)
        if handler is None:
            continue
        try:
            parameters = set(inspect.signature(handler).parameters)
        except (TypeError, ValueError):
            continue
        if not (parameters & ID_PARAMS):
            continue
        yield name, handler, sorted(parameters & ID_PARAMS)


def _source(handler):
    try:
        return inspect.getsource(handler)
    except (OSError, TypeError):
        return ""


def test_the_sweep_finds_tools():
    """A sweep matching nothing would pass the case below forever."""
    assert list(_handlers())


def test_every_tool_taking_an_id_scopes_it():
    unscoped = []
    for name, handler, ids in _handlers():
        if name in NOT_A_FOREIGN_ID:
            continue
        source = _source(handler)
        if not any(marker in source for marker in SCOPING):
            unscoped.append(f"{name}({', '.join(ids)})")
    assert not unscoped, (
        "these tools take an id from the model and never ask whose it is:\n  "
        + "\n  ".join(unscoped)
        + "\nScope it, or record why the id cannot be another caller's in "
          "NOT_A_FOREIGN_ID."
    )


def test_the_exemptions_are_still_tools():
    """An exemption for a tool that no longer exists is a stale claim."""
    names = set(full_registry().names())
    stale = sorted(set(NOT_A_FOREIGN_ID) - names)
    assert not stale, f"exempted but no longer registered: {stale}"


def test_a_task_id_does_not_resolve_on_another_board(tmp_path):
    """The evidence for the `create_worktree` exemption above.

    An exemption is a claim, and an unverified claim in a guard is worse than
    no guard: it reads as "checked" forever. So the reason is executable.
    """

    from mini_loop.tasks import TaskStore

    alice, bob = TaskStore(tmp_path / "alice"), TaskStore(tmp_path / "bob")
    task = alice.create(subject="alice private")
    assert bob.load(task.id) is None
    assert "not found" in bob.complete(task.id)
    assert alice.load(task.id) is not None      # not vacuous: it resolves at home


# --- where the shape stops, recorded as negatives ------------------------

def test_two_sessions_get_different_team_namespaces(tmp_path):
    """Round 79 asserted this by reading `_self_key`; this observes it."""
    from mini_loop import SessionManager, Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=PACKAGE.parent / "skills"),
        FakeAsyncAnthropic(),
        tool_registry=full_registry(),
    )
    first, second = manager.create(), manager.create()
    teams = {first.agent.state.get("team_id"), second.agent.state.get("team_id")}
    assert len(teams) == 2 and None not in teams


def test_the_agent_facing_workflow_tools_pass_a_session():
    """`WorkflowService.get` and `wait` take an unscoped run_id and are not
    reachable from a tool; `status` and `cancel` are, and both scope."""
    registry = full_registry()
    for name in ("workflow_status", "cancel_workflow"):
        tool = registry.get(name)
        if tool is None:
            continue
        assert "session_id" in _source(tool.handler), name


def test_unscoped_service_methods_are_not_reachable_from_a_tool():
    reachable = "".join(_source(full_registry().get(n).handler)
                        for n in full_registry().names()
                        if getattr(full_registry().get(n), "handler", None))
    for method in (".wait(", ".get("):
        assert f"service{method}" not in reachable, (
            f"a tool calls service{method}, which takes an unscoped run_id"
        )
