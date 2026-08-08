"""Memory is the most durable sink in the harness, and it was unmasked.

Round 45's lens was "content the model obeys, loaded without checks". Memory is
the same shape with the agent as author: it writes its own future context.

**Sink seven.** Secrets counted four sinks originally, then compaction's
workspace files made five and six (round 32). Memory is worse than either:
compaction spills are debris, memories are durable *by design*, and their index
is read back into every later request. A credential captured in one is not
merely written down -- it is re-injected indefinitely.

    memory files written : ['MEMORY.md', 'creds.md']
    containing the secret: ['MEMORY.md', 'creds.md']

**An index with no ceiling.** It rides in the runtime facts of every request.
Two hundred memories measured 84,089 characters -- about 21,000 tokens per
call -- growing without bound as the agent remembers more.

**A name that crashed the tool.** Slugs become filenames uncapped, so a long
name raised `OSError: File name too long` out of a tool the model calls with
arbitrary strings.

Two negatives, recorded as such: `_slug` already strips path separators, so
`../../escape` cannot write outside the memory directory; and the index is *not*
duplicated into the system prompt -- `memory_system_builder` exists but is not
wired, so round 8's cache-stability fix does hold here.
"""

import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.memory import (
    MAX_INDEX,
    MAX_SLUG,
    MEMORY_TYPES,
    MemoryStore,
    memory_store_for,
)
from mini_loop.prompts import runtime_facts
from mini_loop.secrets import SecretRegistry

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
SECRET = "sk-MEMORY-LEAK-0123456789abcdef"


@pytest.fixture
def agent(tmp_path):
    # `full_registry` so `remember`/`recall` exist: the index is injected only
    # when the agent has a tool to act on it, so an agent without them is not
    # exercising memory at all.
    from mini_loop.builtins import full_registry

    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS,
                 memory_root=tmp_path / "mem"),
        FakeAsyncAnthropic(),
        secrets=SecretRegistry.from_environ(environ={"P_API_KEY": SECRET}),
        tool_registry=full_registry(),
    ).create().agent


# --- sink seven -----------------------------------------------------------

def test_no_memory_file_carries_a_credential(agent, tmp_path):
    store = memory_store_for(agent)
    store.write("creds", "project", f"api key is {SECRET}", f"the key is {SECRET}")

    written = [p for p in (tmp_path / "mem").rglob("*") if p.is_file()]
    assert written, "nothing was written, so nothing is proven"
    leaked = [p.name for p in written if SECRET in p.read_text(errors="ignore")]
    assert not leaked, f"the raw secret reached {leaked}"


def test_the_index_read_back_into_context_is_masked(agent):
    store = memory_store_for(agent)
    store.write("creds", "project", f"api key is {SECRET}", "body")
    assert SECRET not in store.index()
    assert SECRET not in (runtime_facts(agent) or "")


def test_a_masked_memory_is_still_useful(agent):
    """Masking must not shred the memory into uselessness."""
    store = memory_store_for(agent)
    store.write("creds", "project", f"api key is {SECRET}", "body")
    assert "creds" in store.index()
    assert "api key is" in store.index()


def test_a_store_without_a_registry_still_works(tmp_path):
    """The seam is optional, as everywhere else in this harness."""
    store = MemoryStore(tmp_path / "mem")
    store.write("plain", "project", "a description", "a body")
    assert "plain" in store.index()


@pytest.mark.parametrize("construction", ["manager", "agent"])
def test_every_construction_site_passes_the_registry(tmp_path, agent, construction):
    """Round 26's lesson: a site that quietly passes less is how this recurs."""
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws2", skills_dir=SKILLS,
                 memory_root=tmp_path / "mem2"),
        FakeAsyncAnthropic(),
        secrets=SecretRegistry.from_environ(environ={"P_API_KEY": SECRET}),
    )
    store = manager.memory if construction == "manager" else memory_store_for(agent)
    assert store.secrets is not None, f"{construction} built a store with no masking"


# --- bounded ---------------------------------------------------------------

def test_the_index_is_capped(tmp_path):
    store = MemoryStore(tmp_path / "mem")
    for index in range(200):
        store.write(f"m{index}", "project", "D" * 400, "body")
    rendered = store.index()
    assert len(rendered) <= MAX_INDEX + 200, f"{len(rendered)} chars in every request"
    assert "truncated" in rendered


def test_a_capped_index_says_how_to_reach_the_rest(tmp_path):
    store = MemoryStore(tmp_path / "mem")
    for index in range(200):
        store.write(f"m{index}", "project", "D" * 400, "body")
    assert "recall" in store.index(), "truncation must not be a dead end"


def test_a_small_index_is_untouched(tmp_path):
    store = MemoryStore(tmp_path / "mem")
    store.write("one", "project", "a description", "body")
    assert "truncated" not in store.index()


@pytest.mark.parametrize("name", ["x" * 300, "y" * 5000])
def test_a_long_name_does_not_crash_the_tool(tmp_path, name):
    store = MemoryStore(tmp_path / "mem")
    store.write(name, "project", "d", "b")
    written = list((tmp_path / "mem").glob("*.md"))
    assert written
    assert all(len(p.stem) <= MAX_SLUG for p in written)


# --- negatives, kept as negatives ------------------------------------------

@pytest.mark.parametrize("name", ["../../escape", "a/b/c", "....//x", "a\x00b"])
def test_a_name_cannot_write_outside_the_memory_directory(tmp_path, name):
    store = MemoryStore(tmp_path / "mem")
    store.write(name, "project", "d", "b")
    stray = [p for p in tmp_path.rglob("*.md") if p.parent != store.dir]
    assert not stray, f"files written outside the memory directory: {stray}"


def test_the_index_is_not_also_in_the_system_prompt(agent):
    """`memory_system_builder` would put it there; it is deliberately unwired,
    because a memory write would then invalidate the whole cached prefix."""
    memory_store_for(agent).write("alpha", "project", "DISTINCTIVE-MARKER", "body")
    assert "DISTINCTIVE-MARKER" not in agent.refresh_system()
    assert "DISTINCTIVE-MARKER" in (runtime_facts(agent) or "")


def test_a_memory_write_does_not_change_the_cached_prefix(agent):
    before = agent.refresh_system()
    memory_store_for(agent).write("beta", "project", "second", "body")
    assert agent.refresh_system() == before


@pytest.mark.parametrize("bad", ["not-a-type", "", "../x"])
def test_an_unknown_type_is_coerced_rather_than_stored(tmp_path, bad):
    """Not a hole -- it normalizes to `project`. Pinned so it stays that way."""
    store = MemoryStore(tmp_path / "mem")
    store.write("m", bad, "d", "b")
    assert store.list()[0]["type"] in MEMORY_TYPES


# --- the scoped seam is the only door (rounds 117/118) ----------------------

def test_memory_tools_reach_the_store_only_through_the_scoped_seam():
    """A memory tool must reach the store through `memory_store_for`, which
    binds it to the caller's owner -- never the raw shared store directly.

    Round 117 found `remember`/`recall` reaching the raw store via `_store`,
    so `recall` read every owner's memories. `_store` is gone (round 118), but
    a future tool could still subscript `ctx.state["memory"]` or construct a
    `MemoryStore` and reintroduce the leak. This scans the handlers defined
    inside `install_memory` for exactly those raw reaches.
    """

    import ast
    import inspect

    from mini_loop.memory import install_memory

    source = inspect.getsource(install_memory)
    tree = ast.parse(source.lstrip())
    offenders = []
    for node in ast.walk(tree):
        # ctx.state["memory"] / state["memory"] -- a raw, owner-blind read.
        if isinstance(node, ast.Subscript):
            target = node.value
            if (isinstance(target, ast.Attribute) and target.attr == "state"
                    and isinstance(node.slice, ast.Constant)
                    and node.slice.value == "memory"):
                offenders.append("ctx.state['memory'] subscript")
        # MemoryStore(...) -- a fresh unscoped store built inside a handler.
        if (isinstance(node, ast.Call) and isinstance(node.func, ast.Name)
                and node.func.id == "MemoryStore"):
            offenders.append("MemoryStore(...) construction")

    assert not offenders, (
        "a memory tool reaches the raw store instead of `memory_store_for`: "
        f"{offenders}. That is the round-117 cross-tenant leak."
    )


def test_the_scan_would_see_a_raw_reach():
    """Non-vacuity: the scan must actually flag a raw `ctx.state['memory']`."""

    import ast

    sample = 'def install_memory(reg):\n    async def t(ctx):\n        return ctx.state["memory"]\n'
    tree = ast.parse(sample)
    found = any(
        isinstance(n, ast.Subscript)
        and isinstance(n.value, ast.Attribute) and n.value.attr == "state"
        and isinstance(n.slice, ast.Constant) and n.slice.value == "memory"
        for n in ast.walk(tree)
    )
    assert found, "the scan cannot see a raw ctx.state['memory'] reach"
