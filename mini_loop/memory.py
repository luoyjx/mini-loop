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

from .durable import atomic_write_text
from .problems import ProblemLog
from .blocks import block_text
import asyncio
import json
import re
import threading
from pathlib import Path

from .registry import Tool, ToolContext, ToolRegistry

MEMORY_TYPES = ("user", "feedback", "project", "reference")
_FRONTMATTER = re.compile(r"^---\n(.*?)\n---\n(.*)", re.DOTALL)


#: A slug becomes a filename. Uncapped, a long name raised `OSError: File name
#: too long` out of a tool the model can call with any string.
MAX_SLUG = 80

#: The index rides in the runtime facts of *every* request. Two hundred
#: memories measured 84,089 characters -- about 21,000 tokens per call, growing
#: without bound as the agent remembers more.
MAX_INDEX = 8_000

#: Round 46 capped the index and left the *body* alone -- a 2,000,000 character
#: memory still landed on disk and came back whole through `recall`. The
#: checklist found it the moment it was run against every store at once.
MAX_BODY = 32_000


def _slug(name: str) -> str:
    return (re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-") or "memory")[:MAX_SLUG]


def _header(value: object) -> str:
    return " ".join(str(value).splitlines()).strip()


class MemoryStore:
    def __init__(self, root: Path, *, secrets=None) -> None:
        # A memory is a disk sink, and the most durable one here: memories
        # outlive the session by design and their index is fed back into every
        # later request. A credential captured into one is not merely written
        # down, it is re-read into context indefinitely.
        self.secrets = secrets
        #: Writes that were bounded or refused. Every other content store in
        #: the package grew one of these; a surface with nowhere to say "that
        #: did not work" eventually fails silently.
        self.problems = ProblemLog()
        #: `MEMORY.md` is regenerated on the next read rather than on every
        #: write, so it is eventually consistent for anything outside this
        #: process. Any read through `index()` or `search()` flushes it first.
        self._index_dirty = False
        #: file name -> ((mtime_ns, size), parsed). See `_parse`.
        self._parsed: dict[str, tuple[tuple[int, int], dict]] = {}
        self.dir = Path(root)
        self.dir.mkdir(parents=True, exist_ok=True)
        self.index_path = self.dir / "MEMORY.md"
        self._lock = threading.RLock()
        self.lifecycle_lock = asyncio.Lock()

    def write(self, name: str, mem_type: str, description: str, body: str,
              owner: str = "anonymous") -> str:
        with self._lock:
            if mem_type not in MEMORY_TYPES:
                mem_type = "project"
            name, description = _header(name) or "memory", _header(description)
            slug = _slug(name)
            if len(body) > MAX_BODY:
                self.problems.append(
                    f"{slug}: body truncated from {len(body):,} to {MAX_BODY:,}"
                )
                body = body[:MAX_BODY] + "\n[memory truncated]"
            text = (
                f"---\nname: {name}\ndescription: {description}\n"
                f"type: {mem_type}\nowner: {owner}\n---\n\n{body}\n"
            )
            if self.secrets is not None:
                text = self.secrets.mask(text)
            atomic_write_text(self.dir / f"{slug}.md", text)
            # Deferred. Caching the parses made the *reads* linear and left the
            # time quadratic, because every write still materialised the whole
            # `MEMORY.md`. An agent that remembers ten things and then looks at
            # the index once should rebuild once, not ten times.
            self._index_dirty = True
            return f"Remembered '{name}' ({mem_type})"

    def _parse(self, path: Path) -> dict:
        """Parse one memory, reusing the last result while the file is unchanged.

        `write` rebuilds the index, and the index is built from `list()`, which
        parsed *every* file. So storing N memories read N^2/2 files:

            memories   total s  per write ms  file reads
                  50      0.03          0.65       1,275
                 100      0.11          1.14       5,050
                 200      0.43          2.13      20,100
                 400      1.68          4.20      80,200

        Doubling the memories nearly quadrupled the time, on a path an agent
        touches every time it remembers something. Keyed on (mtime, size) rather
        than held forever, because `replace_all` and an operator with an editor
        both change these files underneath the process.
        """

        try:
            stat = path.stat()
            key = (stat.st_mtime_ns, stat.st_size)
        except OSError:
            key = None
        if key is not None:
            cached = self._parsed.get(path.name)
            if cached is not None and cached[0] == key:
                return cached[1]

        parsed = self._parse_uncached(path)
        if key is not None:
            self._parsed[path.name] = (key, parsed)
        return parsed

    def _parse_uncached(self, path: Path) -> dict | None:
        """Parse one memory, or report it and return None if it cannot be read.

        This read was unguarded, and `list()` parses *every* file in the
        directory, so one undecodable byte took out `list`, `index` and
        `search` together -- and `index()` is called by `runtime_facts` while
        building every request. Three bytes written into the memory directory
        ended every turn of every session on the manager:

            (memory/poison.md) = b"\\xff\\xfe\\x00"
            agent.run("say hi") -> UnicodeDecodeError: invalid start byte

        The directory is not a trusted input. The agent writes to it with its
        own tools, an operator edits it by hand, and a half-finished write from
        a killed process leaves exactly this. A store whose whole purpose is to
        outlive the session has to survive what the last session left behind.

        So a file that will not read is skipped and *reported* rather than
        raising: the rest of memory keeps working, and the failure has
        somewhere to be seen. `list()` drops the Nones.
        """

        try:
            text = path.read_text()
        except (OSError, UnicodeDecodeError) as exc:
            self.problems.append(f"unreadable memory {path.name}: {type(exc).__name__}")
            return None
        meta, body = {}, text
        m = _FRONTMATTER.match(text)
        if m:
            for line in m.group(1).splitlines():
                if ":" in line:
                    k, v = line.split(":", 1)
                    meta[k.strip()] = v.strip()
            body = m.group(2).strip()
        return {"file": path.name, "name": meta.get("name", path.stem),
                "owner": meta.get("owner", "anonymous"),
                "description": meta.get("description", ""), "type": meta.get("type", "project"),
                "body": body}

    def list(self, owner: str | None = None) -> list[dict]:
        """Memories, optionally only one owner's.

        One `MemoryStore` serves every session on a manager, which is right for
        a single user carrying knowledge between sessions and wrong the moment
        two callers share a process: every memory was injected into every
        agent's context. Records written before this field are `anonymous`, so
        an unauthenticated single-user deployment still sees its own.
        """

        with self._lock:
            found = [m for m in (self._parse(p) for p in sorted(self.dir.glob("*.md"))
                                 if p.name != "MEMORY.md") if m is not None]
            if owner is None:
                return found
            return [m for m in found if m.get("owner", "anonymous") == owner]

    def _rebuild_index(self) -> None:
        lines = ["# Memory index\n"]
        for m in self.list():
            lines.append(f"- [{m['name']}]({m['file']}) — {m['description']}")
        rendered = "\n".join(lines) + "\n"
        if self.secrets is not None:
            rendered = self.secrets.mask(rendered)
        atomic_write_text(self.index_path, rendered)

    def flush(self) -> None:
        """Write `MEMORY.md` if a write has made it stale."""

        with self._lock:
            if self._index_dirty:
                self._rebuild_index()
                self._index_dirty = False

    def index(self, owner: str | None = None) -> str:
        self.flush()
        with self._lock:
            items = self.list(owner)
            if not items:
                return "(no memories yet)"
            rendered = "\n".join(
                f"  - {m['name']} [{m['type']}]: {m['description']}" for m in items
            )
            if len(rendered) > MAX_INDEX:
                kept = rendered[:MAX_INDEX].rsplit("\n", 1)[0]
                rendered = (
                    f"{kept}\n  ... {len(items)} memories total; index truncated. "
                    "Use `recall` with a query to search the rest."
                )
            return rendered

    def search(self, query: str | None = None, limit: int = 5,
               owner: str | None = None) -> list[dict]:
        self.flush()
        with self._lock:
            items = self.list(owner)
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

    def replace_all(self, memories: list[dict], owner: str | None = None) -> None:
        """Replace stored memories.

        Scoped by `owner`: only that owner's files are removed and the new ones
        are attributed to them, so consolidating one tenant's memories cannot
        wipe another's. This method is reached from `ScopedMemory.replace_all`,
        which -- before the override beside it -- delegated here unscoped via
        `__getattr__`, so one tenant's turn-end consolidation deleted every
        tenant's memories: the round-117 leak class as a *destructive* op.
        Without `owner` -- an operator, a single-tenant store -- every memory is
        replaced, the original behaviour.
        """

        with self._lock:
            for path in self.dir.glob("*.md"):
                if path.name == "MEMORY.md":
                    continue
                if owner is None or (self._parse(path) or {}).get(
                    "owner", "anonymous"
                ) == owner:
                    path.unlink(missing_ok=True)
            for memory in memories:
                self.write(memory["name"], memory.get("type", "project"),
                           memory.get("description", ""), memory.get("body", ""),
                           owner=owner or "anonymous")
            self._rebuild_index()


def memory_system_builder(base_builder, store: MemoryStore):
    """Wrap a system_builder so the memory index rides along in the prompt.

    **Not wired by default, and it should stay that way.** The memory index
    changes whenever the agent remembers something, and `prompts.py` is explicit
    that turn-to-turn state belongs in the message stream rather than the system
    prompt -- otherwise every write invalidates the whole cached prefix. The
    index is delivered through `runtime_facts` for exactly that reason; this
    helper remains for callers who want the older shape and know the cost.
    """
    def build(agent) -> str:
        base = base_builder(agent)
        return f"{base}\n\nKnown memories (use `recall` to load full text):\n{store.index()}"
    return build


# Removed in round 117/118: the raw, owner-blind store accessor the memory
# tools used to call. It returned `state["memory"]` unscoped, which is how
# `recall` read every owner's memories. Every path to memory now goes through
# `memory_store_for` (below), which binds to the caller's owner. The accessor
# is gone rather than merely unused so a future tool cannot reach for it; the
# guard in test_memory_hygiene.py holds new tools to the scoped seam.


class ScopedMemory:
    """A `MemoryStore` bound to one owner.

    Threading `owner=` through every call site is the arrangement round 26
    showed does not hold: one site forgets and the isolation is gone with no
    signal. Binding it once at the seam means `remember`, `recall` and the
    runtime-facts index are scoped without knowing they are.
    """

    def __init__(self, store: MemoryStore, owner: str) -> None:
        self._store = store
        self.owner = owner

    def write(self, name, mem_type, description, body, owner=None):
        return self._store.write(name, mem_type, description, body,
                                 owner=owner or self.owner)

    def list(self, owner=None):
        return self._store.list(owner or self.owner)

    def index(self, owner=None):
        return self._store.index(owner or self.owner)

    def search(self, query=None, limit=5, owner=None):
        return self._store.search(query, limit, owner=owner or self.owner)

    def replace_all(self, memories, owner=None):
        # Scoped, or consolidation deletes every tenant's memories, not this
        # owner's: `replace_all` is destructive, and `__getattr__` would send it
        # to the raw store unscoped. The one operation `ScopedMemory` cannot
        # afford to leave to delegation.
        return self._store.replace_all(memories, owner=owner or self.owner)

    def __getattr__(self, name):
        return getattr(self._store, name)


def _owner_of(agent) -> str:
    session = (getattr(agent, "state", None) or {}).get("session")
    return getattr(session, "owner", None) or "anonymous"


def memory_store_for(agent) -> MemoryStore:
    store = agent.state.get("memory")
    if store is None:
        root = agent.state.get("memory_root") or (agent.workspace / ".memory")
        store = agent.state["memory"] = MemoryStore(
            root, secrets=getattr(agent, "secrets", None)
        )
    # Bound to this agent's owner. The manager builds one store for every
    # session, which is right for one user carrying knowledge between their own
    # sessions and wrong the moment two callers share the process: every memory
    # was going into every agent's context, automatically, each turn.
    if isinstance(store, ScopedMemory):
        return store
    return ScopedMemory(store, _owner_of(agent))


def memory_enabled(agent) -> bool:
    return "remember" in agent.tools and "recall" in agent.tools and agent.state.get("memory_auto", True)


def _response_text(response) -> str:
    return block_text(response.content)


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
        # `memory_store_for`, not `_store`: the scoped store binds this write to
        # the session's owner. `_store` returns the raw shared MemoryStore, so
        # every remember was written as "anonymous" and every recall read every
        # owner's memories -- the scoping added in round 26 reached the
        # runtime-facts index but never these tools (round 80's applied-to-some
        # trap). Process-local callers are all "anonymous" and still share, which
        # is the intended one-user behaviour; distinct HTTP owners are isolated.
        store = memory_store_for(ctx.agent)
        async with store.lifecycle_lock:
            return await asyncio.to_thread(store.write, name, type, description or name, content)

    async def recall(ctx, query=None):
        hits = await asyncio.to_thread(memory_store_for(ctx.agent).search, query)
        if not hits:
            return "(no matching memories)"
        return "\n\n".join(f"<memory name=\"{m['name']}\" type=\"{m['type']}\">\n{m['body']}\n</memory>" for m in hits)

    registry.register(Tool("remember", "Save a durable fact to long-term memory (survives across sessions).", _REMEMBER, remember, risk="write"))
    registry.register(Tool("recall", "Recall memories matching a query (or list all if no query).", _RECALL, recall, readonly=True, risk="read"))
    return registry

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: memory files are re-read from disk each selection, so there is no in-memory mirror to diverge from the store."
)
