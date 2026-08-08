"""The same shell, reached through a different tool, had none of the guards.

`Toolset.run_bash` builds its argv through the sandbox and scrubs the
environment, putting back only the credentials the command names. `run_in_background`
called `create_subprocess_shell` with no `env` at all. Measured side by side on
one machine, one workspace, one command each:

    run_bash            printenv -> '<secret-hidden>',   escape blocked
    run_in_background   printenv -> 'sk-BACKGROUND-...', escape succeeded

The escaping write landed outside the workspace. Confinement that one tool
honours and its sibling ignores is not confinement, and the background result is
worse than a transcript entry besides: it is stored, injected into the next turn
by `background_injector`, and read back by `check`.

These tests are written as a **comparison** rather than as separate assertions
about each tool. Two paths to the same shell will drift again, and a test that
checks them independently passes while they diverge.
"""

import asyncio
import os
import pathlib

import pytest

from mini_loop.background import BackgroundManager
from mini_loop.sandbox import SeatbeltSandbox, default_sandbox
from mini_loop.secrets import SecretRegistry
from mini_loop.tools import Toolset

SECRET = "sk-BACKGROUND-PARITY-0123456789"


@pytest.fixture
def env(tmp_path, monkeypatch):
    monkeypatch.setenv("PROBE_API_KEY", SECRET)
    workspace = tmp_path / "ws"
    workspace.mkdir()
    registry = SecretRegistry.from_environ()
    sandbox = (
        SeatbeltSandbox(writable_roots=[workspace])
        if SeatbeltSandbox.available() else None
    )
    return workspace, registry, sandbox, tmp_path / "outside.txt"


def _background(workspace, registry, sandbox, command, timeout=3.0):
    manager = BackgroundManager(workspace, secrets=registry, sandbox=sandbox)

    async def run():
        manager.run(command)
        for _ in range(int(timeout * 20)):
            await asyncio.sleep(0.05)
            if manager._completed:
                break
        return manager.drain()

    return asyncio.run(run())


# --- parity, asserted as parity ------------------------------------------

def test_neither_path_reveals_a_credential(env):
    workspace, registry, sandbox, _ = env
    toolset = Toolset(workspace, secrets=registry, sandbox=sandbox)

    foreground = toolset.run_bash("printenv PROBE_API_KEY")
    background = _background(workspace, registry, sandbox, "printenv PROBE_API_KEY")

    assert SECRET not in foreground
    assert background, "the background task never completed"
    assert SECRET not in str(background), (
        "the background path returned the credential the foreground path masked"
    )


@pytest.mark.skipif(not SeatbeltSandbox.available(), reason="macOS Seatbelt only")
def test_neither_path_writes_outside_the_workspace(env):
    workspace, registry, sandbox, outside = env
    toolset = Toolset(workspace, secrets=registry, sandbox=sandbox)

    toolset.run_bash(f"echo pwned > {outside}")
    assert not outside.exists(), "the foreground path escaped"

    _background(workspace, registry, sandbox, f"echo pwned > {outside}")
    assert not outside.exists(), (
        "the background path escaped a workspace the foreground path could not"
    )


def test_both_paths_still_do_the_work(env):
    """Confinement that breaks the tool is not a fix."""
    workspace, registry, sandbox, _ = env
    toolset = Toolset(workspace, secrets=registry, sandbox=sandbox)

    assert "hello" in toolset.run_bash("echo hello")
    done = _background(workspace, registry, sandbox, "echo hello")
    assert done and "hello" in done[0]["result"]
    assert done[0]["status"] == "completed"


def test_a_named_credential_still_reaches_the_command(env):
    """Narrow injection: a command that names a secret gets it, in both paths."""
    workspace, registry, sandbox, _ = env
    done = _background(
        workspace, registry, sandbox,
        'test -n "$PROBE_API_KEY" && echo present || echo absent',
    )
    assert done and "present" in done[0]["result"]


def test_an_unrelated_command_cannot_read_the_environment(env):
    workspace, registry, sandbox, _ = env
    done = _background(workspace, registry, sandbox, "printenv | wc -l")
    assert done
    assert SECRET not in done[0]["result"]


def test_scrubbing_matters_even_though_the_output_is_masked(env):
    """The two layers are not redundant, and a test can only tell them apart by
    transforming the value.

    Masking is a backstop: it searches output for the exact string. A command
    that base64s or slices the credential defeats it entirely. Scrubbing is the
    control that works -- the command never receives the value, so there is
    nothing to transform. Asserting on masked output alone could not distinguish
    the two, and the mutation runner said so.
    """
    workspace, registry, sandbox, _ = env
    # The command must not *name* the secret: narrow injection deliberately
    # hands a credential to a command that asks for it by name, so naming it
    # tests the wrong thing -- as the first version of this did.
    done = _background(
        workspace, registry, sandbox,
        "env | grep -o 'sk-[A-Za-z0-9-]*' | tr 'a-z' 'A-Z'",
    )
    assert done
    leaked = done[0]["result"]
    assert SECRET.upper() not in leaked, (
        f"an unrelated command read the credential and transformed it past the "
        f"mask: {leaked!r}"
    )


# --- the seam is wired from the agent ------------------------------------

def test_the_tool_builds_a_manager_with_the_agents_protections(tmp_path, monkeypatch):
    """`_mgr` constructs it lazily, which is where the drift would return."""
    monkeypatch.setenv("PROBE_API_KEY", SECRET)
    from mini_loop import SessionManager, Settings
    from mini_loop.background import background_manager_for
    from mini_loop.fake_llm import FakeAsyncAnthropic

    workspace = tmp_path / "ws"
    agent = SessionManager(
        Settings(fake_llm=True, workspace_root=workspace,
                 skills_dir=pathlib.Path(__file__).resolve().parent.parent / "skills"),
        FakeAsyncAnthropic(),
        secrets=SecretRegistry.from_environ(),
        sandbox=default_sandbox(workspace) if SeatbeltSandbox.available() else None,
    ).create().agent

    class Context:
        pass

    context = Context()
    context.state = {}
    context.workspace = agent.workspace
    context.agent = agent

    manager = background_manager_for(context)
    assert manager.secrets is agent.secrets
    assert type(manager.sandbox).__name__ == type(agent.sandbox).__name__


def test_a_manager_without_protections_still_runs(tmp_path):
    """Both seams are optional everywhere else in this harness."""
    workspace = tmp_path / "ws"
    workspace.mkdir()
    done = _background(workspace, None, None, "echo plain")
    assert done and "plain" in done[0]["result"]


def test_finished_task_results_do_not_accumulate_without_bound(tmp_path):
    """`_tasks` kept every result for `check_background` and was never trimmed,
    so a session doing repeated background work held every result forever -- the
    action journal's own leak (MAX_RESULTS_RETAINED) in a sibling store that
    never inherited the bound. Past the retention bound the oldest completed
    result text is released; the record stays, so `check_background` still
    answers, and `drain` -- which holds its own copy -- still delivered the full
    text when the task finished.
    """
    from mini_loop.background import SHED_BACKGROUND_RESULT

    workspace = tmp_path / "ws"
    workspace.mkdir()
    manager = BackgroundManager(workspace, max_results_retained=3)
    body = 5_000  # a sizable result, the case the bound exists for

    async def run():
        ids = []
        for _ in range(9):
            reported = manager.run(f"printf 'B%.0s' $(seq 1 {body})")
            bg_id = reported.split()[3].rstrip(":")
            ids.append(bg_id)
            await manager._tasks[bg_id]["handle"]  # finish before launching next
        return ids

    ids = asyncio.run(run())

    full = [b for b in ids
            if manager._tasks[b]["result"] not in (None, SHED_BACKGROUND_RESULT)]
    # Bounded: only the newest-completed handful keeps its full text.
    assert full == ids[-3:], f"retained {full}, expected newest 3 {ids[-3:]}"
    # Every older task was shed to the marker -- record kept, text gone.
    assert all(manager._tasks[b]["result"] == SHED_BACKGROUND_RESULT for b in ids[:6])
    # A shed task still answers -- record kept, marker in place of the text.
    shed_answer = manager.check(ids[0])
    assert "completed" in shed_answer and "released" in shed_answer
    # Peak result text tracks the bound, not the number of tasks ever run.
    held = sum(len(t.get("result") or "") for t in manager._tasks.values())
    assert held < (manager.max_results_retained + 1) * body, (
        "result text is not bounded by the retention limit"
    )


def test_the_task_listing_is_bounded_regardless_of_task_count(tmp_path):
    """`check_background()` with no id rendered one line per task *ever run*.
    The result text is shed past the retention bound, but the listing itself was
    uncapped, so after a long session of background work a single no-arg
    `check_background` floods the model context -- the one growth every other
    tool result is bounded against. It now shows the most recent tasks and
    summarises the rest; a specific id still answers in full.
    """
    from mini_loop.background import MAX_TASK_LISTING

    workspace = tmp_path / "ws"
    workspace.mkdir()
    manager = BackgroundManager(workspace, max_results_retained=5)
    total = MAX_TASK_LISTING + 30

    async def run():
        ids = []
        for _ in range(total):
            reported = manager.run("printf x")
            bg_id = reported.split()[3].rstrip(":")
            ids.append(bg_id)
            await manager._tasks[bg_id]["handle"]
        return ids

    ids = asyncio.run(run())
    listing = manager.check()
    lines = listing.splitlines()

    # Bounded by the display cap, not by how many tasks ever ran.
    assert len(lines) <= MAX_TASK_LISTING + 1, f"listing grew to {len(lines)} lines"
    assert f"{total - MAX_TASK_LISTING} older" in listing, listing
    # The newest is shown; the oldest is summarised away but still answers by id.
    assert ids[-1] in listing
    assert ids[0] not in listing
    assert "released" in manager.check(ids[0]), "an evicted task lost its record"


def test_shedding_never_swallows_an_undelivered_notification(tmp_path):
    """The `_completed` copy is independent of the `_tasks` copy that is shed,
    so a result finished but not yet drained is still delivered in full."""
    workspace = tmp_path / "ws"
    workspace.mkdir()
    manager = BackgroundManager(workspace, max_results_retained=2)

    async def run():
        for _ in range(5):
            reported = manager.run("printf 'HELLO'")
            await manager._tasks[reported.split()[3].rstrip(":")]["handle"]
        return manager.drain()

    delivered = asyncio.run(run())
    assert len(delivered) == 5
    assert all(item["result"] == "HELLO" for item in delivered)


@pytest.mark.skipif(not SeatbeltSandbox.available(), reason="macOS Seatbelt only")
def test_entering_a_worktree_re_confines_background_too(tmp_path):
    """`enter_workspace` rebuilds the Toolset -- re-binding run_bash's sandbox to
    the new workspace -- but for the background manager it updated only the
    workspace (the cwd), not the sandbox. So after a worktree switch a background
    command ran in the new workspace while still confined to the old one: it
    could not write its own worktree, yet could still write the one it left,
    defeating the isolation entering a worktree exists to provide. Both paths
    must move together."""
    from mini_loop.agent import Agent
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.secrets import NullSecretRegistry

    ws_a = tmp_path / "ws_a"
    ws_a.mkdir()
    ws_b = tmp_path / "ws_b"
    ws_b.mkdir()
    settings = Settings(
        fake_llm=True, workspace_root=ws_a,
        skills_dir=pathlib.Path(__file__).resolve().parent.parent / "skills",
    )
    agent = Agent(
        client=FakeAsyncAnthropic(), settings=settings, workspace=ws_a,
        sandbox=default_sandbox(ws_a), secrets=NullSecretRegistry(),
    )
    manager = BackgroundManager(ws_a, sandbox=agent.sandbox, secrets=NullSecretRegistry())
    agent.state["background"] = manager

    agent.enter_workspace(ws_b)

    async def run(command):
        manager.run(command)
        await asyncio.gather(
            *[t["handle"] for t in manager._tasks.values()
              if t.get("handle") and not t["handle"].done()],
            return_exceptions=True,
        )

    asyncio.run(run("echo hi > in_b.txt"))
    asyncio.run(run(f"echo escaped > {ws_a / 'leak.txt'}"))

    # It can write its own (entered) worktree, and cannot write the one it left.
    assert (ws_b / "in_b.txt").exists(), "background could not write the entered worktree"
    assert not (ws_a / "leak.txt").exists(), "background escaped to the workspace it left"
