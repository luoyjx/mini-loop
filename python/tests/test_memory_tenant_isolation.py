"""One owner's memories are not another's, through the tools themselves.

The manager builds one `MemoryStore` for the whole process, so isolation
between callers is by `owner`. Round 26 added `ScopedMemory` to bind every
operation to the session's owner and wired it into the runtime-facts index --
the memory list auto-injected into an agent's context. But the `remember` and
`recall` *tools* kept calling `_store(ctx)`, the raw unscoped store. So
`remember` wrote every memory as "anonymous" and `recall` returned every
owner's memories: the index was scoped, the tools were not. Measured -- Bob's
`recall` returned Alice's private memory verbatim. Round 80's applied-to-some
trap, in memory instead of tool identity.

The tools now go through `memory_store_for`, the same scoped seam the index
uses. Distinct HTTP owners are isolated; process-local callers are all
"anonymous" and still share, which is the intended one-user continuity.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.registry import ToolCall, ToolContext

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 memory_root=tmp_path / "mem", skills_dir=SKILLS),
        FakeAsyncAnthropic(), tool_registry=full_registry(),
    )


def _ctx(session):
    agent = session.agent
    return ToolContext(agent, agent.workspace, agent.state,
                       ToolCall("x", {}, "x"))


async def _remember(session, **kw):
    handler = session.agent.tools.get("remember").handler
    return await handler(_ctx(session), **kw)


async def _recall(session, query=None):
    handler = session.agent.tools.get("recall").handler
    return await handler(_ctx(session), query=query)


@pytest.mark.asyncio
async def test_one_owner_cannot_recall_anothers_memory(tmp_path):
    manager = _manager(tmp_path)
    alice = manager.create()
    alice.owner = "alice"
    bob = manager.create()
    bob.owner = "bob"

    await _remember(alice, name="alice-plan", content="alice's private plan is X",
                    type="project", description="plan")

    bob_view = await _recall(bob, query=None)
    assert "alice's private plan" not in bob_view, (
        "a caller recalled another owner's memory through the tool"
    )


@pytest.mark.asyncio
async def test_an_owner_still_recalls_their_own(tmp_path):
    """Not a wall: scoping isolates across owners, it does not blind an owner
    to their own memories."""

    manager = _manager(tmp_path)
    alice = manager.create()
    alice.owner = "alice"

    await _remember(alice, name="alice-plan", content="alice's own note",
                    type="project", description="note")

    assert "alice's own note" in await _recall(alice, query=None)


@pytest.mark.asyncio
async def test_anonymous_sessions_share_by_design(tmp_path):
    """Process-local callers with no HTTP owner are all "anonymous" and share,
    which is the intended single-user continuity across sessions."""

    manager = _manager(tmp_path)
    first, second = manager.create(), manager.create()  # both anonymous

    await _remember(first, name="team-note", content="shared team note",
                    type="project", description="note")

    assert "shared team note" in await _recall(second, query=None)


@pytest.mark.asyncio
async def test_remember_writes_under_the_session_owner(tmp_path):
    """The write is attributed to the owner, not the "anonymous" default of the
    raw store -- pinned at the store level so the fix cannot silently regress
    to writing everything as anonymous."""

    from mini_loop.memory import memory_store_for

    manager = _manager(tmp_path)
    alice = manager.create()
    alice.owner = "alice"

    await _remember(alice, name="attributed", content="body", type="project",
                    description="d")

    raw = alice.agent.state["memory"]
    owners = {m.get("owner") for m in raw.list()}
    assert owners == {"alice"}, f"memory not attributed to its owner: {owners}"


@pytest.mark.asyncio
async def test_one_owners_consolidation_does_not_wipe_anothers(tmp_path):
    """`consolidate_memories` calls `store.replace_all`, which deletes files and
    rewrites. `ScopedMemory` overrides `write`/`list`/`index`/`search` for owner
    scoping but left `replace_all` to `__getattr__`, which sent it to the raw
    store unscoped -- so it deleted *every* owner's memory files and rewrote the
    survivors as "anonymous". The round-117 read leak as a *destructive* op: one
    tenant's turn-end consolidation wiped every tenant's memories.
    """
    from mini_loop.memory import memory_store_for

    manager = _manager(tmp_path)
    alice = manager.create()
    alice.owner = "alice"
    bob = manager.create()
    bob.owner = "bob"

    await _remember(alice, name="a-plan", content="alice body", type="project", description="a")
    await _remember(bob, name="b-plan", content="bob body", type="project", description="b")

    # Alice's turn-end consolidation replaces her memories with a consolidated set.
    memory_store_for(alice.agent).replace_all(
        [{"name": "a-plan", "type": "project", "description": "a v2", "body": "consolidated"}]
    )

    assert [m["name"] for m in memory_store_for(bob.agent).list()] == ["b-plan"], (
        "another owner's memories were wiped by a consolidation"
    )
    alice_after = memory_store_for(alice.agent).list()
    assert {m["name"] for m in alice_after} == {"a-plan"}, (
        "alice lost her own memory to an anonymous-attributed rewrite"
    )
    assert all(m["owner"] == "alice" for m in alice_after)


def test_scopedmemory_scopes_every_owner_sensitive_method():
    """`ScopedMemory` scopes by overriding the owner-sensitive methods and
    delegating the rest to the raw store via `__getattr__`. That is safe only
    while the delegated rest is harmless unscoped -- and round 136 found it was
    not: `replace_all` rode `__getattr__` and deleted every tenant's memories.

    This pins the classification so the next method decides itself. Every public
    `MemoryStore` method must be either owner-sensitive (and overridden on
    `ScopedMemory`) or on the reviewed harmless allowlist; a new one lands in
    neither and fails here, before it silently rides `__getattr__` unscoped.
    """
    import inspect

    from mini_loop.memory import MemoryStore, ScopedMemory

    # Reads or writes per-owner records -> ScopedMemory MUST override it.
    owner_sensitive = {"write", "list", "index", "search", "replace_all"}
    # Rebuilds the durable (un-injected) index file; touches no per-owner record.
    harmless = {"flush"}

    public = {
        name for name, _ in inspect.getmembers(MemoryStore, callable)
        if not name.startswith("_")
    }

    unclassified = public - owner_sensitive - harmless
    assert not unclassified, (
        f"unclassified public MemoryStore method(s) {sorted(unclassified)}: decide "
        "owner-sensitive (override on ScopedMemory) or harmless (add to the allowlist) "
        "-- delegation is where an un-reviewed method goes unscoped"
    )
    delegated = owner_sensitive - set(vars(ScopedMemory))
    assert not delegated, (
        f"ScopedMemory delegates owner-sensitive method(s) {sorted(delegated)} to the "
        "raw store unscoped -- the replace_all leak class"
    )
