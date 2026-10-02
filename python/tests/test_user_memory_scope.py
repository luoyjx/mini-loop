"""User memory has a physical namespace, provenance, and a clean write path."""

import asyncio
import hashlib
import json
from types import SimpleNamespace

import pytest

from mini_loop.fake_llm import FakeMessage, text
from mini_loop.memory import (
    MemoryStore,
    ScopedMemory,
    consolidate_memories,
    extract_memories,
    install_memory,
    memory_store_for,
    memory_on_stop,
    prepare_memory_context,
)
from mini_loop.registry import ToolCall, ToolContext, ToolRegistry


def _files(store):
    return sorted(
        path for path in store.dir.glob("*.md") if path.name != "MEMORY.md"
    )


def test_physical_keys_cover_owner_and_exact_normalized_name(tmp_path):
    store = MemoryStore(tmp_path / "memory")
    alice = ScopedMemory(store, "alice")
    bob = ScopedMemory(store, "bob")

    alice.write("preferences", "user", "alice", "alice-v1")
    bob.write("preferences", "user", "bob", "bob-v1")
    alice.write("preferences", "user", "alice", "alice-v2")
    alice.write("a b", "project", "space", "space-body")
    alice.write("a-b", "project", "hyphen", "hyphen-body")

    assert {item["body"] for item in alice.list()} == {
        "alice-v2", "space-body", "hyphen-body"
    }
    assert {item["body"] for item in bob.list()} == {"bob-v1"}
    assert len(_files(store)) == 4
    assert all(path.name.startswith("u-") for path in _files(store))
    assert all("alice" not in path.name and "bob" not in path.name
               for path in _files(store))


def test_new_records_use_safe_owner_metadata_and_legacy_records_still_load(tmp_path):
    store = MemoryStore(tmp_path / "memory")
    malicious = "../../alice\nowner: bob/用户"
    scoped = ScopedMemory(store, malicious)
    scoped.write("profile", "user", "display", "private")

    [path] = _files(store)
    frontmatter = path.read_text().split("---", 2)[1]
    assert "../" not in path.name and "alice" not in path.name
    assert frontmatter.count("\nowner: ") == 1
    assert "owner: ../../alice owner: bob/用户" in frontmatter
    assert f"owner_key: {hashlib.sha256(malicious.encode()).hexdigest()}" in frontmatter
    assert "scope: user" in frontmatter
    assert "origin: explicit" in frontmatter
    assert [item["body"] for item in scoped.list()] == ["private"]
    assert ScopedMemory(store, "bob").list() == []

    # Pre-scope records used the slug directly and either carried a display
    # owner or had no frontmatter at all. Both remain readable.
    (store.dir / "legacy-alice.md").write_text(
        "---\nname: legacy alice\ndescription: old\n"
        "type: project\nowner: alice\n---\n\nlegacy owner body\n"
    )
    (store.dir / "legacy-anonymous.md").write_text("legacy anonymous body\n")
    alice_legacy = ScopedMemory(store, "alice").list()
    anonymous_legacy = ScopedMemory(store, "anonymous").list()
    assert alice_legacy[0]["origin"] == "imported"
    assert alice_legacy[0]["owner_key"] == ""
    assert any(item["body"].strip() == "legacy anonymous body"
               for item in anonymous_legacy)


def test_anonymous_keeps_legacy_filename_and_scoped_owner_cannot_switch(tmp_path):
    store = MemoryStore(tmp_path / "memory")
    anonymous = ScopedMemory(store, "anonymous")
    anonymous.write("plain note", "project", "plain", "body")
    assert (store.dir / "plain-note.md").exists()

    alice = ScopedMemory(store, "alice")
    alice.write("own", "project", "own", "body", owner="alice")
    with pytest.raises(ValueError, match="cannot be overridden"):
        alice.write("foreign", "project", "foreign", "body", owner="bob")
    with pytest.raises(ValueError, match="cannot be overridden"):
        alice.list(owner="bob")
    with pytest.raises(ValueError, match="cannot be overridden"):
        alice.index(owner="bob")
    with pytest.raises(ValueError, match="cannot be overridden"):
        alice.search(owner="bob")
    with pytest.raises(ValueError, match="cannot be overridden"):
        alice.replace_all([], owner="bob")


def test_construction_time_resource_owner_precedes_mutable_session_owner(tmp_path):
    store = MemoryStore(tmp_path / "memory")
    agent = SimpleNamespace(
        workspace=tmp_path,
        secrets=None,
        state={
            "memory": store,
            "resource_owner": "alice",
            "session": SimpleNamespace(owner="bob"),
        },
    )

    assert memory_store_for(agent).owner == "alice"


def test_prebound_memory_must_match_the_resource_owner(tmp_path):
    store = MemoryStore(tmp_path / "memory")
    agent = SimpleNamespace(
        workspace=tmp_path,
        secrets=None,
        state={
            "memory": ScopedMemory(store, "bob"),
            "resource_owner": "alice",
        },
    )

    with pytest.raises(ValueError, match="does not match"):
        memory_store_for(agent)


class _MemoryAgent:
    def __init__(self, store, owner="alice", *, readonly=False):
        self.workspace = store.dir.parent
        mode = "readonly" if readonly else "interactive"
        self.state = {
            "memory": store,
            "session": SimpleNamespace(owner=owner, permission_mode=mode),
            "memory_auto": True,
            "permission_mode": mode,
        }
        self.tools = ToolRegistry()
        install_memory(self.tools)
        self.messages = []
        self.client = None
        self.settings = SimpleNamespace(model="fake")
        self.sent = []
        self.create_calls = 0

    async def _create(self, *_args, **_kwargs):
        self.create_calls += 1
        return FakeMessage([text("[]")], "end_turn")

    async def _send(self, kind, **payload):
        self.sent.append((kind, payload))


@pytest.mark.asyncio
async def test_context_and_recall_expose_scope_and_origin(tmp_path):
    store = MemoryStore(tmp_path / "memory")
    agent = _MemoryAgent(store)
    scoped = ScopedMemory(store, "alice")
    scoped.write("preference", "user", "marker", "distinct marker")

    context = await prepare_memory_context(agent, "how does the marker work?")
    assert '<memory scope="user" origin="explicit"' in context
    assert "distinct marker" in context

    handler = agent.tools.get("recall").handler
    ctx = ToolContext(
        agent, agent.workspace, agent.state, ToolCall("recall", {}, "call-1")
    )
    recalled = await handler(ctx, query="marker")
    assert '<memory scope="user" origin="explicit"' in recalled
    assert "distinct marker" in recalled


@pytest.mark.asyncio
async def test_auto_extraction_uses_clean_messages_and_marks_origin(tmp_path):
    store = MemoryStore(tmp_path / "memory")
    scoped = ScopedMemory(store, "alice")
    captured = []

    async def create(request, **_kwargs):
        captured.append(request[0]["content"])
        item = [{
            "name": "clean-fact",
            "type": "user",
            "description": "clean",
            "body": "remembered from the clean turn",
        }]
        return FakeMessage([text(json.dumps(item))], "end_turn")

    messages = [
        {
            "role": "user",
            "content": (
                "<memory_context>\n"
                '<memory scope="user" origin="explicit">RECALLED-SECRET</memory>\n'
                "</memory_context>\n\nactual user request"
            ),
        },
        {
            "role": "assistant",
            "content": [{
                "type": "tool_use", "id": "tool-1", "name": "load_skill",
                "input": {"name": "private"},
            }],
        },
        {
            "role": "user",
            "content": [{
                "type": "tool_result", "tool_use_id": "tool-1",
                "content": "<skill>SKILL-SECRET</skill> RAW-TOOL-SECRET",
            }],
        },
        {
            "role": "user",
            "content": "<runtime-state>\nINDEX-SECRET\n</runtime-state>",
        },
        {"role": "assistant", "content": "final answer"},
    ]

    count = await extract_memories(
        scoped, messages, client=None, model="fake", create=create
    )
    assert count == 1
    [prompt] = captured
    assert "actual user request" in prompt and "final answer" in prompt
    assert "RECALLED-SECRET" not in prompt
    assert "SKILL-SECRET" not in prompt
    assert "RAW-TOOL-SECRET" not in prompt
    assert "INDEX-SECRET" not in prompt
    assert scoped.list()[0]["origin"] == "auto_extracted"


@pytest.mark.asyncio
async def test_consolidation_preserves_unchanged_origin_and_marks_changes(tmp_path):
    store = MemoryStore(tmp_path / "memory")
    scoped = ScopedMemory(store, "alice")
    scoped.write("keep", "project", "same", "same body", origin="explicit")
    scoped.write(
        "change", "project", "old", "old body", origin="auto_extracted"
    )

    class Agent:
        async def _create(self, *_args, **_kwargs):
            result = [
                {"name": "keep", "type": "project",
                 "description": "same", "body": "same body"},
                {"name": "change", "type": "project",
                 "description": "new", "body": "new body"},
            ]
            return FakeMessage([text(json.dumps(result))], "end_turn")

    assert await consolidate_memories(scoped, Agent(), threshold=2) == 2
    origins = {item["name"]: item["origin"] for item in scoped.list()}
    assert origins == {"keep": "explicit", "change": "consolidated"}


@pytest.mark.asyncio
async def test_readonly_skips_automatic_memory_writes(tmp_path):
    store = MemoryStore(tmp_path / "memory")
    agent = _MemoryAgent(store, readonly=True)
    agent.messages = [
        {"role": "user", "content": "remember this"},
        {"role": "assistant", "content": "done"},
    ]

    await memory_on_stop(agent)

    assert agent.create_calls == 0
    assert _files(store) == []


@pytest.mark.asyncio
async def test_runtime_mode_change_to_readonly_skips_automatic_write(tmp_path):
    store = MemoryStore(tmp_path / "memory")
    agent = _MemoryAgent(store)
    # The API mutates the live session; the construction-time state snapshot
    # deliberately remains interactive.
    agent.state["session"].permission_mode = "readonly"
    assert agent.state["permission_mode"] == "interactive"
    agent.messages = [
        {"role": "user", "content": "remember this"},
        {"role": "assistant", "content": "done"},
    ]

    await memory_on_stop(agent)

    assert agent.create_calls == 0
    assert _files(store) == []
