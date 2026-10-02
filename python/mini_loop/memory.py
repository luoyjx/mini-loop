"""Persistent memory (s09).

Compaction (s08) is lossy and dies with the session. Memory is a filesystem
layer that survives both: each memory is a Markdown file with frontmatter under
a memory dir, indexed by `MEMORY.md`. The bounded index is delivered through
the runtime-facts message stream; full bodies are pulled in on demand via
`recall`.

Unlike the per-session workspace, a memory dir can be shared across sessions
(per user/tenant) to give an agent long-term recall. Enable at the manager
level with a shared dir, or per-session with `install_memory(registry)`.
"""

from __future__ import annotations

from .durable import atomic_write_text
from .problems import ProblemLog
from .blocks import block_field, block_text
import asyncio
import hashlib
import html
import json
import re
import threading
from pathlib import Path

from .registry import Tool, ToolContext, ToolRegistry

MEMORY_TYPES = ("user", "feedback", "project", "reference")
MEMORY_ORIGINS = ("explicit", "auto_extracted", "consolidated", "imported")
_FRONTMATTER = re.compile(r"^---\n(.*?)\n---\n(.*)", re.DOTALL)
_OWNER_KEY = re.compile(r"^[0-9a-f]{64}$")
_MEMORY_CONTEXT_PREFIX = re.compile(
    r"\A<memory_context>\n.*\n</memory_context>\n\n",
    re.DOTALL,
)
_RUNTIME_FACTS_MESSAGE = re.compile(
    r"\A<runtime-state>\n.*\n</runtime-state>\Z",
    re.DOTALL,
)


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
    normalized = re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-")[:MAX_SLUG]
    if not normalized or normalized == "memory":
        # Memory census (2026-09-01): every name that normalized to
        # nothing -- Chinese, emoji -- shared the one bare fallback file,
        # so unrelated CJK-named memories silently destroyed each other;
        # and on a case-insensitive filesystem that file folds onto the
        # MEMORY.md index, so the next flush destroyed the memory
        # outright. The fallback is stable per exact name, and the
        # reserved index name can never become a memory's filename.
        digest = hashlib.sha256(name.encode("utf-8")).hexdigest()[:8]
        normalized = f"memory-{digest}"
    return normalized


def _header(value: object) -> str:
    return " ".join(str(value).splitlines()).strip()


def _owner_key(owner: object) -> str:
    """Opaque identity key; the raw principal never becomes a path segment."""

    return hashlib.sha256(str(owner).encode("utf-8")).hexdigest()


def _memory_key(owner: object, normalized_name: str) -> str:
    """Collision-safe physical key over the exact owner and normalized name."""

    digest = hashlib.sha256()
    for value in (str(owner), normalized_name):
        encoded = value.encode("utf-8")
        digest.update(len(encoded).to_bytes(8, "big"))
        digest.update(encoded)
    return digest.hexdigest()


def _origin(value: object, *, default: str = "imported") -> str:
    normalized = _header(value)
    return normalized if normalized in MEMORY_ORIGINS else default


def _belongs_to_owner(memory: dict, owner: object) -> bool:
    """Match new records by opaque key and legacy records by their owner line."""

    stored_key = memory.get("owner_key", "")
    if stored_key:
        return stored_key == _owner_key(owner)
    return memory.get("owner", "anonymous") == str(owner)


def _memory_block(memory: dict) -> str:
    """Model-visible reference block with explicit access/provenance labels."""

    name = html.escape(str(memory.get("name", "memory")), quote=True)
    mem_type = html.escape(str(memory.get("type", "project")), quote=True)
    origin = html.escape(_origin(memory.get("origin")), quote=True)
    body = str(memory.get("body", ""))
    return (
        f'<memory scope="user" origin="{origin}" name="{name}" '
        f'type="{mem_type}">\n{body}\n</memory>'
    )


def _strip_injected_context(text: str) -> str:
    """Remove model-facing context that must never feed memory extraction."""

    if _RUNTIME_FACTS_MESSAGE.fullmatch(text):
        return ""
    return _MEMORY_CONTEXT_PREFIX.sub("", text, count=1)


def _clean_memory_messages(messages: list) -> list[dict]:
    """A transcript projection without recalled memory or tool-result bodies."""

    cleaned: list[dict] = []
    for message in messages:
        if not isinstance(message, dict):
            continue
        content = message.get("content")
        if isinstance(content, str):
            content = _strip_injected_context(content)
            if not content:
                continue
        elif isinstance(content, list):
            parts = []
            for part in content:
                if block_field(part, "type", "") == "tool_result":
                    continue
                if isinstance(part, dict) and part.get("type") == "text":
                    text = _strip_injected_context(str(part.get("text", "")))
                    if not text:
                        continue
                    part = {**part, "text": text}
                parts.append(part)
            if not parts:
                continue
            content = parts
        elif isinstance(content, dict):
            if block_field(content, "type", "") == "tool_result":
                continue
        cleaned.append({**message, "content": content})
    return cleaned


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
              owner: str = "anonymous", *, origin: str = "explicit") -> str:
        with self._lock:
            if mem_type not in MEMORY_TYPES:
                mem_type = "project"
            name, description = _header(name) or "memory", _header(description)
            owner = str(owner)
            owner_key = _owner_key(owner)
            owner_display = _header(owner) or "anonymous"
            origin = _origin(origin, default="explicit")
            slug = _slug(name)
            if len(body) > MAX_BODY:
                self.problems.append(
                    f"{slug}: body truncated from {len(body):,} to {MAX_BODY:,}"
                )
                body = body[:MAX_BODY] + "\n[memory truncated]"
            text = (
                f"---\nname: {name}\ndescription: {description}\n"
                f"type: {mem_type}\nscope: user\nowner_key: {owner_key}\n"
                f"owner: {owner_display}\norigin: {origin}\n---\n\n{body}\n"
            )
            if self.secrets is not None:
                text = self.secrets.mask(text)
            # Anonymous is the pre-scope single-user namespace. Preserve its
            # exact filenames; authenticated owners use a digest over both the
            # exact owner and exact normalized name, so neither cross-owner
            # equality nor same-slug names such as `a b` / `a-b` can collide.
            filename = (
                f"{slug}.md"
                if owner == "anonymous"
                else f"u-{_memory_key(owner, name)}-{slug}.md"
            )
            target = self.dir / filename
            self._parsed.pop(target.name, None)
            atomic_write_text(target, text)

            # Lazily migrate the exact legacy record this write supersedes.
            # A different normalized name may share its slug and must survive.
            # The pre-census fallback file (bare memory.md) is a candidate
            # too: a CJK-named record stored there before hashed fallbacks
            # would otherwise reappear beside its own rewrite.
            legacy_candidates = [self.dir / f"{slug}.md"]
            if slug.startswith("memory-"):
                legacy_candidates.append(self.dir / "memory.md")
            for legacy in legacy_candidates:
                if target != legacy and legacy.exists():
                    prior = self._parse(legacy)
                    if (
                        prior is not None
                        and _belongs_to_owner(prior, owner)
                        and prior.get("name") == name
                    ):
                        legacy.unlink(missing_ok=True)
                        self._parsed.pop(legacy.name, None)
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
        stored_owner_key = meta.get("owner_key", "").lower()
        if not _OWNER_KEY.fullmatch(stored_owner_key):
            stored_owner_key = ""
        return {
            "file": path.name,
            "name": meta.get("name", path.stem),
            "owner": meta.get("owner", "anonymous"),
            "owner_key": stored_owner_key,
            "scope": "user",
            "origin": _origin(meta.get("origin"), default="imported"),
            "description": meta.get("description", ""),
            "type": meta.get("type", "project"),
            "body": body,
        }

    def list(self, owner: str | None = None) -> list[dict]:
        """Memories, optionally only one owner's.

        The compatibility store may serve every session on a manager, so its
        owner filter is a required isolation boundary.  With
        `UserResourceResolver`, each owner also has a separate physical store;
        keeping this logical filter in place makes accidental rebinding fail
        closed. Records written before owner metadata are `anonymous`, so an
        unauthenticated single-user deployment still sees its own.
        """

        with self._lock:
            found = [m for m in (self._parse(p) for p in sorted(self.dir.glob("*.md"))
                                 if p.name != "MEMORY.md") if m is not None]
            # A legacy index that landed in `memory.md` (case-insensitive
            # filesystems fold that name onto MEMORY.md) parses as a memory
            # named "memory" whose body IS the index. Serve no index text as
            # a memory; a real legacy record living in memory.md still parses
            # by its frontmatter and stays.
            found = [m for m in found
                     if not (m["file"].lower() == "memory.md"
                             and m["body"].startswith("# Memory index"))]
            if owner is not None:
                expected_key = _owner_key(owner)
                # Normalize keyed records onto the exact requested identity
                # only after their digest matches. Non-matches get a sentinel,
                # so they cannot fall through the legacy display-owner check.
                found = [
                    ({**memory, "owner": str(owner)}
                     if memory.get("owner_key") == expected_key
                     else {**memory, "owner": None}
                     if memory.get("owner_key")
                     else memory)
                    for memory in found
                ]
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

    def replace_all(self, memories: list[dict], owner: str | None = None, *,
                    origin: str = "imported") -> None:
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
                parsed = self._parse(path)
                if owner is None or (
                    parsed is not None and _belongs_to_owner(parsed, owner)
                ):
                    path.unlink(missing_ok=True)
                    self._parsed.pop(path.name, None)
            default_origin = _origin(origin, default="imported")
            replacement_owner = "anonymous" if owner is None else owner
            for memory in memories:
                self.write(memory["name"], memory.get("type", "project"),
                           memory.get("description", ""), memory.get("body", ""),
                           owner=replacement_owner,
                           origin=_origin(
                               memory.get("origin"), default=default_origin
                           ))
            self._rebuild_index()
            self._index_dirty = False


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
        return (
            f"{base}\n\nKnown user memories "
            f"(use `recall` to load full text):\n{store.index()}"
        )
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
        self.owner = str(owner)

    def _bound_owner(self, owner):
        if owner is not None and str(owner) != self.owner:
            raise ValueError("scoped memory owner cannot be overridden")
        return self.owner

    def write(self, name, mem_type, description, body, owner=None, *,
              origin="explicit"):
        return self._store.write(name, mem_type, description, body,
                                 owner=self._bound_owner(owner), origin=origin)

    def list(self, owner=None):
        return self._store.list(self._bound_owner(owner))

    def index(self, owner=None):
        return self._store.index(self._bound_owner(owner))

    def search(self, query=None, limit=5, owner=None):
        return self._store.search(
            query, limit, owner=self._bound_owner(owner)
        )

    def replace_all(self, memories, owner=None, *, origin="imported"):
        # Scoped, or consolidation deletes every tenant's memories, not this
        # owner's: `replace_all` is destructive, and `__getattr__` would send it
        # to the raw store unscoped. The one operation `ScopedMemory` cannot
        # afford to leave to delegation.
        return self._store.replace_all(
            memories, owner=self._bound_owner(owner), origin=origin
        )

    def __getattr__(self, name):
        return getattr(self._store, name)


def _owner_of(agent) -> str:
    state = getattr(agent, "state", None) or {}
    # SessionManager binds user resources before Agent construction. Prefer
    # that immutable construction-time authority; the session fallback keeps
    # bare/legacy agents compatible. A default anonymous binding may still be
    # followed by the historical direct `session.owner = ...` pattern; honour
    # that only for the compatibility sentinel, never for a real bound owner.
    resource_owner = state.get("resource_owner")
    session = state.get("session")
    session_owner = getattr(session, "owner", None)
    if resource_owner not in (None, "", "anonymous"):
        return str(resource_owner)
    if session_owner not in (None, "", "anonymous"):
        return str(session_owner)
    return str(resource_owner or session_owner or "anonymous")


def memory_store_for(agent) -> ScopedMemory:
    store = agent.state.get("memory")
    if store is None:
        root = agent.state.get("memory_root") or (agent.workspace / ".memory")
        store = agent.state["memory"] = MemoryStore(
            root, secrets=getattr(agent, "secrets", None)
        )
    # Bind the logical view even when UserResourceResolver has already selected
    # a per-owner physical store.  The same seam also protects the legacy
    # manager-wide store, so every call site gets one owner and cannot override
    # it through a method argument.
    if isinstance(store, ScopedMemory):
        state = getattr(agent, "state", None) or {}
        resource_owner = state.get("resource_owner")
        session_owner = getattr(state.get("session"), "owner", None)
        expected_owner = _owner_of(agent)
        if (
            resource_owner not in (None, "")
            or session_owner not in (None, "")
        ) and store.owner != expected_owner:
            raise ValueError(
                "agent memory scope does not match its bound resource owner"
            )
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
    bodies = "\n\n".join(_memory_block(memory) for memory in selected)
    await agent._send("memory", action="load", count=len(selected))
    return f"<memory_context>\n{bodies}\n</memory_context>\n\n{user_text}"


async def extract_memories(store: MemoryStore, messages: list, client, model: str,
                           max_items: int = 5, create=None) -> int:
    """Side LLM query: pull durable facts out of a conversation and store them.

    Called after a normal final turn (or from an explicit lifecycle adapter),
    not at true session close. Returns count written. Best-effort: any failure
    is swallowed so it never breaks a session.
    """
    try:
        convo = json.dumps(
            _clean_memory_messages(messages), default=str
        )[-40_000:]
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
            store.write(
                m["name"], m.get("type", "project"),
                m.get("description", ""), m.get("body", ""),
                origin="auto_extracted",
            )
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
        unchanged_origins = {
            (
                memory.get("name"),
                memory.get("type", "project"),
                memory.get("description", ""),
                memory.get("body", ""),
            ): _origin(memory.get("origin"), default="imported")
            for memory in memories
        }
        normalized = []
        for memory in consolidated:
            if not isinstance(memory, dict) or "name" not in memory:
                continue
            identity = (
                memory.get("name"),
                memory.get("type", "project"),
                memory.get("description", ""),
                memory.get("body", ""),
            )
            normalized.append({
                **memory,
                "origin": unchanged_origins.get(identity, "consolidated"),
            })
        if not normalized:
            return 0
        store.replace_all(normalized, origin="consolidated")
        return len(normalized)
    except Exception:
        return 0


async def memory_on_stop(agent) -> None:
    if not memory_enabled(agent):
        return
    # Readonly agents may use `recall`, but an automatic extraction is a
    # durable write that must obey the same posture as every explicit tool.
    session = agent.state.get("session")
    permission_mode = (
        getattr(session, "permission_mode", None)
        or agent.state.get("permission_mode")
    )
    if permission_mode == "readonly":
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
            return await asyncio.to_thread(
                store.write, name, type, description or name, content,
                origin="explicit",
            )

    async def recall(ctx, query=None):
        hits = await asyncio.to_thread(memory_store_for(ctx.agent).search, query)
        if not hits:
            return "(no matching memories)"
        return "\n\n".join(_memory_block(memory) for memory in hits)

    registry.register(Tool(
        "remember",
        "Save a durable fact to the current user's memory (survives across sessions).",
        _REMEMBER,
        remember,
        risk="write",
    ))
    registry.register(Tool(
        "recall",
        "Recall the current user's memories matching a query (or list all).",
        _RECALL,
        recall,
        readonly=True,
        risk="read",
    ))
    return registry

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: directory entries remain authority; cached parses are validated by file stat and the bounded index is delivered through the runtime-facts message stream."
)
