"""Persistent memory (s09).

Compaction (s08) is lossy and dies with the session. Memory is a filesystem
layer that survives both: each memory is a Markdown file with frontmatter under
a memory dir, indexed by `MEMORY.md`. The index is cheap, so it's injected into
the system prompt; full bodies are pulled in on demand via `recall`.

Unlike the per-session workspace, a memory dir can be shared across sessions
(per user/tenant) to give an agent long-term recall. Enable at the manager
level with a shared dir, or per-session with `install_memory(registry)`.
"""

from __future__ import annotations

import asyncio
import json
import re
import threading
from pathlib import Path

from .registry import Tool, ToolContext, ToolRegistry

MEMORY_TYPES = ("user", "feedback", "project", "reference")
_FRONTMATTER = re.compile(r"^---\n(.*?)\n---\n(.*)", re.DOTALL)


def _slug(name: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-") or "memory"


def _header(value: object) -> str:
    return " ".join(str(value).splitlines()).strip()


class MemoryStore:
    def __init__(self, root: Path) -> None:
        self.dir = Path(root)
        self.dir.mkdir(parents=True, exist_ok=True)
        self.index_path = self.dir / "MEMORY.md"
        self._lock = threading.RLock()
        self.lifecycle_lock = asyncio.Lock()

    def write(self, name: str, mem_type: str, description: str, body: str) -> str:
        with self._lock:
            if mem_type not in MEMORY_TYPES:
                mem_type = "project"
            name, description = _header(name) or "memory", _header(description)
            slug = _slug(name)
            text = (
                f"---\nname: {name}\ndescription: {description}\n"
                f"type: {mem_type}\n---\n\n{body}\n"
            )
            (self.dir / f"{slug}.md").write_text(text)
            self._rebuild_index()
            return f"Remembered '{name}' ({mem_type})"

    def _parse(self, path: Path) -> dict:
        text = path.read_text()
        meta, body = {}, text
        m = _FRONTMATTER.match(text)
        if m:
            for line in m.group(1).splitlines():
                if ":" in line:
                    k, v = line.split(":", 1)
                    meta[k.strip()] = v.strip()
            body = m.group(2).strip()
        return {"file": path.name, "name": meta.get("name", path.stem),
                "description": meta.get("description", ""), "type": meta.get("type", "project"),
                "body": body}

    def list(self) -> list[dict]:
        with self._lock:
            return [self._parse(p) for p in sorted(self.dir.glob("*.md"))
                    if p.name != "MEMORY.md"]

    def _rebuild_index(self) -> None:
        lines = ["# Memory index\n"]
        for m in self.list():
            lines.append(f"- [{m['name']}]({m['file']}) — {m['description']}")
        self.index_path.write_text("\n".join(lines) + "\n")

    def index(self) -> str:
        with self._lock:
            items = self.list()
            if not items:
                return "(no memories yet)"
            return "\n".join(
                f"  - {m['name']} [{m['type']}]: {m['description']}" for m in items
            )

    def search(self, query: str | None = None, limit: int = 5) -> list[dict]:
        with self._lock:
            items = self.list()
            if not query:
                return items[:limit]
            terms = set(re.findall(r"[\w-]{2,}", query.lower()))
            scored = []
            for memory in items:
                haystack = " ".join(
                    str(memory[f] or "") for f in ("name", "description", "body")
                ).lower()
                score = sum(haystack.count(term) for term in terms)
                scored.append((score, memory))
            return [m for score, m in sorted(scored, key=lambda x: -x[0]) if score][:limit]

    def replace_all(self, memories: list[dict]) -> None:
        with self._lock:
            for path in self.dir.glob("*.md"):
                if path.name != "MEMORY.md":
                    path.unlink()
            for memory in memories:
                self.write(memory["name"], memory.get("type", "project"),
                           memory.get("description", ""), memory.get("body", ""))
            self._rebuild_index()


def memory_system_builder(base_builder, store: MemoryStore):
    """Wrap a system_builder so the memory index rides along in the prompt."""
    def build(agent) -> str:
        base = base_builder(agent)
        return f"{base}\n\nKnown memories (use `recall` to load full text):\n{store.index()}"
    return build


def _store(ctx: ToolContext) -> MemoryStore:
    store = ctx.state.get("memory")
    if store is None:
        store = ctx.state["memory"] = MemoryStore(ctx.workspace / ".memory")
    return store


def memory_store_for(agent) -> MemoryStore:
    store = agent.state.get("memory")
    if store is None:
        root = agent.state.get("memory_root") or (agent.workspace / ".memory")
        store = agent.state["memory"] = MemoryStore(root)
    return store


def memory_enabled(agent) -> bool:
    return "remember" in agent.tools and "recall" in agent.tools and agent.state.get("memory_auto", True)


def _response_text(response) -> str:
    return "".join(getattr(block, "text", "") for block in response.content
                   if getattr(block, "type", "") == "text")


def _json_array(text: str) -> list:
    start, end = text.find("["), text.rfind("]")
    if start < 0 or end < start:
        return []
    value = json.loads(text[start:end + 1])
    return value if isinstance(value, list) else []


async def select_relevant_memories(agent, query: str, max_items: int = 5) -> list[dict]:
    """Use a small side-query for selection, with lexical fallback."""
    store = memory_store_for(agent)
    memories = store.list()
    if not memories:
        return []
    catalog = "\n".join(
        f"{index}: {memory['name']} — {memory['description']}"
        for index, memory in enumerate(memories)
    )
    try:
        response = await agent._create(
            [{"role": "user", "content": (
                "Select relevant memory indices for the request. Return ONLY a JSON array of integers.\n\n"
                f"Request:\n{query[-4000:]}\n\nCatalog:\n{catalog}"
            )}],
            max_tokens=200,
            purpose="memory_selection",
        )
        indices = _json_array(_response_text(response))
        selected = [memories[index] for index in indices
                    if isinstance(index, int) and 0 <= index < len(memories)]
        if selected:
            return selected[:max_items]
    except Exception:
        pass
    return store.search(query, max_items)


async def prepare_memory_context(agent, user_text: str) -> str:
    if not memory_enabled(agent):
        return user_text
    selected = await select_relevant_memories(agent, user_text)
    if not selected:
        # Still create the store so the dynamic prompt can expose its index.
        memory_store_for(agent)
        return user_text
    bodies = "\n\n".join(
        f'<memory name="{memory["name"]}" type="{memory["type"]}">\n'
        f'{memory["body"]}\n</memory>'
        for memory in selected
    )
    await agent._send("memory", action="load", count=len(selected))
    return f"<memory_context>\n{bodies}\n</memory_context>\n\n{user_text}"


async def extract_memories(store: MemoryStore, messages: list, client, model: str,
                           max_items: int = 5, create=None) -> int:
    """Side LLM query: pull durable facts out of a conversation and store them.

    Call at session end (or from a tool). Returns count written. Best-effort:
    any failure is swallowed so it never breaks a session.
    """
    try:
        convo = json.dumps(messages, default=str)[-40_000:]
        existing = "\n".join(f"- {item['name']}: {item['description']}" for item in store.list())
        prompt = (
            "From this conversation, extract durable facts worth remembering across sessions "
            f"(types: {', '.join(MEMORY_TYPES)}). Return ONLY a JSON array of "
            '{"name","type","description","body"}. Empty array if nothing durable or already covered.\n\n'
            f"Existing memories:\n{existing}\n\nConversation:\n{convo}"
        )
        request = [{"role": "user", "content": prompt}]
        resp = (await create(request, max_tokens=1500) if create is not None
                else await client.messages.create(model=model, max_tokens=1500, messages=request))
        items = _json_array(_response_text(resp))
        for m in items[:max_items]:
            store.write(m["name"], m.get("type", "project"), m.get("description", ""), m.get("body", ""))
        return len(items[:max_items])
    except Exception:
        return 0


async def consolidate_memories(store: MemoryStore, agent, threshold: int = 10) -> int:
    memories = store.list()
    if len(memories) < threshold:
        return 0
    try:
        response = await agent._create(
            [{"role": "user", "content": (
                "Deduplicate and consolidate these memories. Preserve current facts and remove obsolete or "
                "contradictory duplicates. Return ONLY JSON array entries with name,type,description,body.\n\n"
                + json.dumps(memories, ensure_ascii=False)
            )}],
            max_tokens=2500,
            purpose="memory_consolidation",
        )
        consolidated = _json_array(_response_text(response))
        if not consolidated:
            return 0
        store.replace_all(consolidated)
        return len(consolidated)
    except Exception:
        return 0


async def memory_on_stop(agent) -> None:
    if not memory_enabled(agent):
        return
    store = memory_store_for(agent)
    async with store.lifecycle_lock:
        count = await extract_memories(
            store, list(agent.messages), agent.client,
            agent.state.get("recovery_model", agent.settings.model),
            create=agent._create,
        )
        consolidated = await consolidate_memories(store, agent)
    if count or consolidated:
        await agent._send("memory", action="extract", count=count, consolidated=consolidated)


_REMEMBER = {
    "type": "object",
    "properties": {
        "name": {"type": "string"},
        "type": {"type": "string", "enum": list(MEMORY_TYPES)},
        "description": {"type": "string"},
        "content": {"type": "string"},
    },
    "required": ["name", "content"],
}
_RECALL = {"type": "object", "properties": {"query": {"type": "string"}}}


def install_memory(registry: ToolRegistry) -> ToolRegistry:
    async def remember(ctx, name, content, type="project", description=""):
        store = _store(ctx)
        async with store.lifecycle_lock:
            return await asyncio.to_thread(store.write, name, type, description or name, content)

    async def recall(ctx, query=None):
        hits = await asyncio.to_thread(_store(ctx).search, query)
        if not hits:
            return "(no matching memories)"
        return "\n\n".join(f"<memory name=\"{m['name']}\" type=\"{m['type']}\">\n{m['body']}\n</memory>" for m in hits)

    registry.register(Tool("remember", "Save a durable fact to long-term memory (survives across sessions).", _REMEMBER, remember))
    registry.register(Tool("recall", "Recall memories matching a query (or list all if no query).", _RECALL, recall, readonly=True))
    return registry
