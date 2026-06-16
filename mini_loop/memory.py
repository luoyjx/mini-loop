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
from pathlib import Path

from .registry import Tool, ToolContext, ToolRegistry

MEMORY_TYPES = ("user", "feedback", "project", "reference")
_FRONTMATTER = re.compile(r"^---\n(.*?)\n---\n(.*)", re.DOTALL)


def _slug(name: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-") or "memory"


class MemoryStore:
    def __init__(self, root: Path) -> None:
        self.dir = Path(root)
        self.dir.mkdir(parents=True, exist_ok=True)
        self.index_path = self.dir / "MEMORY.md"

    def write(self, name: str, mem_type: str, description: str, body: str) -> str:
        if mem_type not in MEMORY_TYPES:
            mem_type = "project"
        slug = _slug(name)
        text = f"---\nname: {name}\ndescription: {description}\ntype: {mem_type}\n---\n\n{body}\n"
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
        return [self._parse(p) for p in sorted(self.dir.glob("*.md")) if p.name != "MEMORY.md"]

    def _rebuild_index(self) -> None:
        lines = ["# Memory index\n"]
        for m in self.list():
            lines.append(f"- [{m['name']}]({m['file']}) — {m['description']}")
        self.index_path.write_text("\n".join(lines) + "\n")

    def index(self) -> str:
        items = self.list()
        if not items:
            return "(no memories yet)"
        return "\n".join(f"  - {m['name']} [{m['type']}]: {m['description']}" for m in items)

    def search(self, query: str | None = None, limit: int = 5) -> list[dict]:
        items = self.list()
        if not query:
            return items[:limit]
        q = query.lower()
        scored = [(sum(q in (m[f] or "").lower() for f in ("name", "description", "body")), m) for m in items]
        return [m for score, m in sorted(scored, key=lambda x: -x[0]) if score][:limit]


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


async def extract_memories(store: MemoryStore, messages: list, client, model: str, max_items: int = 5) -> int:
    """Side LLM query: pull durable facts out of a conversation and store them.

    Call at session end (or from a tool). Returns count written. Best-effort:
    any failure is swallowed so it never breaks a session.
    """
    try:
        convo = json.dumps(messages, default=str)[-40_000:]
        prompt = (
            "From this conversation, extract durable facts worth remembering across sessions "
            f"(types: {', '.join(MEMORY_TYPES)}). Return ONLY a JSON array of "
            '{"name","type","description","body"}. Empty array if nothing durable.\n\n' + convo
        )
        resp = await client.messages.create(
            model=model, max_tokens=1500, messages=[{"role": "user", "content": prompt}])
        text = "".join(getattr(b, "text", "") for b in resp.content if getattr(b, "type", "") == "text")
        start, end = text.find("["), text.rfind("]")
        items = json.loads(text[start:end + 1]) if start >= 0 else []
        for m in items[:max_items]:
            store.write(m["name"], m.get("type", "project"), m.get("description", ""), m.get("body", ""))
        return len(items[:max_items])
    except Exception:
        return 0


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
        return await asyncio.to_thread(_store(ctx).write, name, type, description or name, content)

    async def recall(ctx, query=None):
        hits = await asyncio.to_thread(_store(ctx).search, query)
        if not hits:
            return "(no matching memories)"
        return "\n\n".join(f"<memory name=\"{m['name']}\" type=\"{m['type']}\">\n{m['body']}\n</memory>" for m in hits)

    registry.register(Tool("remember", "Save a durable fact to long-term memory (survives across sessions).", _REMEMBER, remember))
    registry.register(Tool("recall", "Recall memories matching a query (or list all if no query).", _RECALL, recall, readonly=True))
    return registry
