"""Stop finding disk sinks one at a time.

Sinks one to four were enumerated when secret masking was built. Five and six
turned up in round 32 (compaction's workspace files), seven in round 46
(memory), eight in round 47 (the cron file). Each was found by looking at one
module for an unrelated reason, and `secrets.py` now says outright that
enumerating them by hand is how every one of them was missed.

So this does not enumerate. An AST scan finds every write in the package, and
each site must be **classified**: either it *records* something (and must be
masked, and is covered by the sweep below) or it *executes* something the user
asked for (and must stay raw, or the operation would not work). A new write site
fails this test until somebody says which it is.

The sweep is the other half: a session that exercises the persistence paths with
a registered secret, then every directory the harness writes to is walked and no
file may contain it. That is one assertion covering sinks five through eight and
whatever nine turns out to be.
"""

import ast
import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.memory import memory_store_for
from mini_loop.secrets import SecretRegistry
from mini_loop.storage import SQLiteStateStore

PACKAGE = pathlib.Path(__file__).resolve().parent.parent / "mini_loop"
SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
SECRET = "sk-SWEEP-CANARY-0123456789abcdef"

#: Writes that *record* what happened. These must be masked, and the sweep
#: below is what checks it.
RECORDED = {
    "compaction.py": "spilled tool results and transcript dumps",
    "cron.py": "scheduled prompts that survive a restart",
    "memory.py": "memories and their index, re-read into every later request",
    "tasks.py": "task subjects and descriptions, model-authored",
    "teams.py": "messages delivered to another agent's inbox",
    "trajectory.py": "the recorded event log",
    "worktrees.py": "worktree lifecycle events",
    "approvals.py": (
        "the durable approval trail: input_preview is masked in ask() before "
        "the row is built, so the write persists already-redacted text"
    ),
    "session.py": (
        "expiring parked approval rows on restore: rewrites rows the broker "
        "already masked, adding only a status and timestamp"
    ),
    "spill.py": (
        "preserved oversized tool output: callers save the already-masked "
        "projection (run_bash masks each stream before any truncation), so "
        "the spill file records what the model saw, never raw bytes"
    ),
    "trace_view.py": (
        "the rendered trace page: a read-side projection of trajectory rows "
        "that were masked once at _capture_event before they reached the "
        "store, written 0600 like the recording it renders"
    ),
}

#: Writes that *perform* the thing the caller asked for. Masking these would
#: break the operation: a `write_file` that redacted its own content, or an MCP
#: request the server cannot act on. The same rule the transcript follows --
#: what is recorded is masked, what is executed is not.
EXECUTED = {
    "ast_context.py": (
        "ephemeral, private source snapshots passed byte-for-byte to the "
        "operator-pinned ast-outline process and deleted after each invocation"
    ),
    "tools.py": "write_file / edit_file put the agent's work product on disk",
    "mcp.py": "requests to an MCP server, which must arrive intact to work",
    "durable.py": (
        "the write primitive itself: it puts down exactly the bytes it was "
        "handed, so masking is the caller's decision and every caller is "
        "classified separately above"
    ),
}

#: `atomic_write_text` is a write primitive like the others. Round 82 routed
#: four durable writers through it, and that silently removed them from this
#: scan -- cron.py and tasks.py stopped containing a syntactic write and so
#: stopped being classified at all. A refactor must not shrink the inventory.
WRITE_CALLS = {"write_text", "write_bytes", "writelines", "write",
               "atomic_write_text", "atomic_write_bytes"}


def _write_sites() -> dict[str, list[int]]:
    found: dict[str, list[int]] = {}
    for path in sorted(PACKAGE.rglob("*.py")):
        for node in ast.walk(ast.parse(path.read_text())):
            if not isinstance(node, ast.Call):
                continue
            name = getattr(node.func, "attr", None) or getattr(node.func, "id", None)
            writes = name in WRITE_CALLS or (
                name == "open"
                and len(node.args) > 1
                and isinstance(node.args[1], ast.Constant)
                and "w" in str(node.args[1].value)
            )
            if writes:
                found.setdefault(path.name, []).append(node.lineno)
    return found


def test_every_write_site_is_classified():
    """A new one fails here until someone decides which kind it is."""
    sites = _write_sites()
    assert sites, "no write sites found -- the scan broke, not the package"

    unclassified = sorted(set(sites) - set(RECORDED) - set(EXECUTED))
    assert not unclassified, (
        "these modules write to disk and nobody has said whether that is a "
        f"record (mask it) or an execution (leave it raw): {unclassified}"
    )


def test_the_classification_has_no_dead_entries():
    """A module that stopped writing should leave the list, or the list rots
    into a description of an older package."""
    sites = _write_sites()
    stale = sorted((set(RECORDED) | set(EXECUTED)) - set(sites))
    assert not stale, f"classified but no longer writes anything: {stale}"


def test_no_module_is_in_both_lists():
    assert not (set(RECORDED) & set(EXECUTED))


# --- the sweep ------------------------------------------------------------

def _exercise(tmp_path) -> list[pathlib.Path]:
    """Run the persistence paths with a canary, return every root written to."""
    workspace = tmp_path / "ws"
    memory = tmp_path / "mem"
    store = SQLiteStateStore(tmp_path / "state.db")

    manager = SessionManager(
        Settings(
            fake_llm=True, workspace_root=workspace, skills_dir=SKILLS,
            memory_root=memory, trajectory_enabled=True,
        ),
        FakeAsyncAnthropic(
            responder=lambda request: (
                [__import__("mini_loop.fake_llm", fromlist=["text"]).text(
                    f"the key is {SECRET}")],
                "end_turn",
            )
        ),
        secrets=SecretRegistry.from_environ(environ={"P_API_KEY": SECRET}),
        state_store=store,
    )
    session = manager.create()

    asyncio.run(session.agent.run(f"remember that the key is {SECRET}"))
    memory_store_for(session.agent).write(
        "creds", "project", f"api key {SECRET}", f"body {SECRET}"
    )
    manager.cron.schedule("s1", "0 3 * * *", f"deploy using {SECRET}")
    session._flush_messages()
    store.close()
    return [tmp_path]


def test_no_file_the_harness_writes_contains_a_registered_secret(tmp_path):
    """One assertion over everything, rather than a list of known paths.

    A list of paths is what missed sinks five, six, seven and eight.
    """
    roots = _exercise(tmp_path)
    written = [
        path for root in roots for path in root.rglob("*")
        if path.is_file() and path.suffix != ".db-wal"
    ]
    assert written, "nothing was written, so nothing is proven"

    leaked = [
        str(path.relative_to(tmp_path))
        for path in written
        if SECRET.encode() in path.read_bytes()
    ]
    assert not leaked, f"the canary reached: {leaked}"


def test_the_sweep_would_notice(tmp_path):
    """The sweep must be able to fail; a canary nothing writes proves nothing."""
    _exercise(tmp_path)
    planted = tmp_path / "ws" / "planted.txt"
    planted.parent.mkdir(parents=True, exist_ok=True)
    planted.write_text(f"leak {SECRET}")

    found = [
        path for path in tmp_path.rglob("*")
        if path.is_file() and SECRET.encode() in path.read_bytes()
    ]
    assert found, "the sweep cannot detect a planted canary; it proves nothing"


def test_the_masked_form_is_present_so_masking_ran(tmp_path):
    """Absence of the secret could also mean nothing was recorded at all."""
    _exercise(tmp_path)
    masked = [
        path for path in tmp_path.rglob("*")
        if path.is_file() and b"<secret-hidden>" in path.read_bytes()
    ]
    assert masked, "no file shows a mask, so the canary may never have been recorded"


# --- layers must be pinned separately -------------------------------------

def test_the_agent_masks_a_tool_argument_without_a_session(tmp_path):
    """Two layers now mask a tool argument: the agent boundary (round 21) and
    the event path (this round). The second made the first's test pass for the
    wrong reason -- removing the agent-level mask stopped leaking, because the
    session masked the event anyway.

    Defence in depth is worth keeping, but a layer nothing pins is a layer that
    can be deleted by accident. This exercises the agent with no session
    attached, which is the case the outer layer does not cover.
    """
    import mini_loop.fake_llm as fake
    from mini_loop.agent import Agent

    captured: list[dict] = []

    async def emit(event):
        captured.append(event)

    agent = Agent(
        client=FakeAsyncAnthropic(
            responder=lambda request: (
                [fake.tool("run_bash", _id="t1", command=f"echo {SECRET}")],
                "tool_use",
            )
        ),
        settings=Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                          skills_dir=SKILLS),
        workspace=tmp_path / "ws",
        secrets=SecretRegistry.from_environ(environ={"P_API_KEY": SECRET}),
        emit=emit,
        max_rounds=1,
    )
    asyncio.run(agent.run("run it"))

    tool_events = [e for e in captured if e.get("type") == "tool_use"]
    assert tool_events, "no tool_use event was emitted"
    assert SECRET not in str(tool_events), (
        "the agent boundary let a credential into the event stream; with no "
        "session there is no second layer to catch it"
    )
