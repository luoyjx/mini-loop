# Extending mini-loop

mini-loop is built so you add **your** business on top without editing core
files. The agent loop never changes; every capability around it is a swappable
seam you inject at construction time.

```
                         the loop  (agent.py, do not touch)
                              │
   ┌──────────┬──────────┬───┴────┬───────────┬───────────┬────────────┐
 tools      hooks      system   compaction  skills      LLM        workspace
ToolRegistry Hooks   builder    Compactor  SkillLoader  client     factory
   │          │          │         │           │           │            │
 add your   permission  prompt   context     domain     provider/    docker /
 own tools  / audit /   assembly strategy    knowledge  fake/base_url worktree
            transform                                                + event_sink
```

Everything is injected through **two constructors**:

* `Agent(...)` — one agent (used directly, and for subagents)
* `SessionManager(...)` — the fleet; whatever you pass here is applied to
  *every* session it creates, and then served over HTTP by `create_app`.

A complete, runnable example combining all of the below:
[`examples/custom_agent.py`](./examples/custom_agent.py).

---

## The extension map

| Module | Seam | Inject via | Replace to change… |
|---|---|---|---|
| `registry.py` / `builtins.py` | `ToolRegistry` | `tools=` / `tool_registry=` | what the agent can *do* |
| `registry.py` | `Hooks` (`Hook`) | `hooks=` | permissions, audit, arg/output rewriting |
| `prompts.py` | `system_builder(agent)->str` | `system_builder=` / `system=` | the system prompt |
| `compaction.py` | `Compactor` | `compactor=` | how context is trimmed/summarized |
| `recovery.py` | `RecoveryPolicy` | `recovery=` | retry/backoff/token-escalation/fallback on LLM errors |
| `agent.py` | `injectors` (`async (agent)->msgs`) | `injectors=` | splice messages into each turn (background, cron) |
| `skills.py` | `SkillLoader` | `skills=` + `skills/` dir | on-demand domain knowledge |
| `config.py` | LLM client | `build_client` / `client=` | model / provider / base_url |
| `manager.py` | `workspace_factory(id)->Path` | `workspace_factory=` | where/how the sandbox is provisioned |
| `session.py` | `event_sink(event)` | `event_sink=` | global metrics / logging / persistence |
| `server.py` | `create_app(manager=...)` | app factory | serving a customized fleet |

---

## 1. Tools — `ToolRegistry`

A tool is `(name, description, JSON schema, handler)`. The handler receives a
`ToolContext` first, then the model-supplied arguments.

```python
from mini_loop import default_registry

registry = default_registry()      # bash, read_file, write_file, edit_file,
                                   # TodoWrite, task, load_skill, compress

@registry.add(
    "web_search",
    "Search the web and return the top hit.",
    {"type": "object", "properties": {"query": {"type": "string"}}, "required": ["query"]},
)
async def web_search(ctx, query):          # ctx + your schema properties
    ctx.state.setdefault("searches", []).append(query)   # per-session state
    return await my_search_api(query)      # return a string
```

Hand it to an agent or the whole fleet:

```python
Agent(..., tools=registry)
SessionManager(settings, client, tool_registry=registry)   # cloned per session
```

**`ToolContext`** (the handler's first arg) gives you:

* `ctx.workspace` — this session's sandboxed `Path`
* `ctx.state` — a per-session `dict` for your business state (survives turns)
* `ctx.agent` — the running agent (advanced: `messages`, `todo`, `skills`)
* `await ctx.emit_event("my_event", ...)` — push a custom event to the stream

Handlers may be **sync or async**, and may return anything (`str()`-ified).
Raised exceptions are caught and returned to the model as `Error: ...`, so a
buggy tool degrades into feedback instead of a crash.

**Remove or replace built-ins:**

```python
registry.unregister("bash")                      # no shell for this product
registry.register(my_bash_tool, replace=True)    # swap the implementation
readonly = registry.subset(["read_file", "web_search"])
```

Subagents get their own restricted registries (`explore_registry`,
`worker_registry` in `builtins.py`) — override `Agent._run_subagent` or pass a
different `task` tool if you need custom delegation.

---

## 2. Hooks — permissions, audit, rewriting

A `Hook` wraps every tool call. Both methods are async; override either.

```python
from mini_loop import Hook, Hooks

class Policy(Hook):
    async def before_tool(self, ctx, call):
        # return a string to DENY (it becomes the tool result)
        if call.name == "bash" and "rm " in call.input.get("command", ""):
            return "DENIED: destructive command"
        # mutate call.input in place to REWRITE arguments
        if call.name == "write_file":
            call.input["path"] = f"sandbox/{call.input['path']}"
        return None                       # None = allow

    async def after_tool(self, ctx, call, output):
        await ctx.emit_event("audit", tool=call.name)
        return output.replace(SECRET, "***")   # return to REPLACE, None to keep

Agent(..., hooks=Hooks([Policy(), AnotherHook()]))     # ordered chain
SessionManager(settings, client, hooks=Hooks([Policy()]))
```

`before_tool` runs in order; the **first** hook to return a string wins and
short-circuits the call. `after_tool` runs in order; each may transform the
output. Hooks apply to subagents too. Keep hooks stateless (or guard their
state) since one `Hooks` instance is shared across concurrent sessions.

> Permissions (s03) and pre/post-tool extension points (s04) are *both* just
> hooks here — there's no separate subsystem to learn.

---

## 3. System prompt — `system_builder`

The prompt is produced from the agent at construction, so it can reflect the
*actual* tools and skills wired up.

```python
from mini_loop import sections_builder

build = sections_builder(
    "You are AcmeBot. Always cite sources.",          # static section
    lambda a: f"Workspace: {a.workspace}. Tools: {', '.join(a.tools.names())}.",
)

Agent(..., system_builder=build)
SessionManager(settings, client, system_builder=build)
```

Or skip building entirely with a fixed string: `Agent(..., system="...")`
(also what the API's `POST /sessions {"system": "..."}` does per session).

---

## 4. Context compaction — `Compactor`

Implement two async methods; swap in your strategy (rolling summary, S3
transcripts, never auto-compact, semantic dedup, …).

```python
from mini_loop import Compactor   # Protocol: maybe_compact + compact

class KeepLastN:
    def __init__(self, n=40): self.n = n
    async def maybe_compact(self, agent):
        if len(agent.messages) > self.n:
            agent.messages[:] = agent.messages[-self.n:]
    async def compact(self, agent):           # explicit `compress` tool
        await self.maybe_compact(agent)

Agent(..., compactor=KeepLastN())
SessionManager(settings, client, compactor=KeepLastN())
```

`maybe_compact` runs at the top of every loop pass; `compact` is forced by the
`compress` tool. The default (`DefaultCompactor`) micro-compacts old tool
results every pass and writes a transcript + LLM summary past the token
threshold.

---

## 5. Skills — on-demand knowledge

Drop a `skills/<name>/SKILL.md` with frontmatter; it's indexed by description
and injected only when the model calls `load_skill`.

```markdown
---
name: refunds
description: Company refund policy and the steps to issue one.
---
# Refunds
...full instructions the model loads on demand...
```

Point the loader at your directory (`MINILOOP_SKILLS_DIR` or
`SkillLoader(path)`), or subclass `SkillLoader` to source skills from a DB/CMS
— it only needs `descriptions()` and `load(name)`.

---

## 6. LLM / provider — the client

Any object exposing `await client.messages.create(model=, messages=, tools=,
system=, max_tokens=)` returning `.content` (blocks) + `.stop_reason` works.

* **Anthropic / compatible providers:** set `ANTHROPIC_API_KEY`, `MODEL_ID`,
  and `ANTHROPIC_BASE_URL` (GLM / MiniMax / Kimi / DeepSeek) — `build_client`
  handles it.
* **Custom client:** `Agent(..., client=MyClient())` or
  `SessionManager(settings, MyClient())`.
* **Offline:** `MINILOOP_FAKE_LLM=1` uses `FakeAsyncAnthropic`; in tests inject
  a `scripted([...])` responder for exact tool sequences.

---

## 7. Workspace provisioning — `workspace_factory`

Every session is sandboxed to a directory; all file/bash tools are confined to
it (`Toolset.safe_path` blocks escapes). Control *where/how* it's provisioned:

```python
def factory(session_id: str) -> Path:
    # per-tenant, git worktree, ephemeral tmpfs, a mounted docker volume, ...
    return Path("/srv/tenants") / current_tenant() / session_id

SessionManager(settings, client, workspace_factory=factory)
```

For stronger isolation than `safe_path`, provision a container/jail in the
factory and have a custom `bash` tool exec inside it.

---

## 8. Events & observability — `event_sink`

Each session is an event bus. Built-in event types: `status`, `assistant_text`,
`tool_use`, `tool_result`, `subagent_start`, `subagent_end`, `todo`, `compact`,
`done`, `error` — plus any you emit from tools/hooks via `ctx.emit_event(...)`.
Every event carries `session`, `agent`, `depth`, `seq`, `ts`.

```python
def sink(event):                 # sync or async; called for every event
    statsd.incr(f"agent.event.{event['type']}")
    audit_log.write(event)

SessionManager(settings, client, event_sink=sink)
```

In-process consumers can also `session.subscribe()` (used by the SSE
endpoints).

---

## 9. Serving a customized fleet

Build your `SessionManager` with all the seams above, then hand it to the app
factory — you keep the same REST + SSE endpoints and console:

```python
# myapp.py
from mini_loop.server import create_app
from mini_loop import SessionManager, build_client, load_settings, Hooks
# ... your registry / hooks / builders ...

def app():
    s = load_settings()
    mgr = SessionManager(s, build_client(s),
                         tool_registry=registry, hooks=Hooks([Policy()]),
                         system_builder=build, workspace_factory=factory,
                         event_sink=sink)
    return create_app(manager=mgr)
```

```sh
uvicorn myapp:app --factory --port 8000
```

---

## 10. Built-in feature modules (s09, s11–s19)

Beyond the core loop, mini-loop ships optional modules covering the rest of the
learn-claude-code curriculum. Turn them all on at once:

```python
SessionManager(settings, client, enable_features=True)   # or env MINILOOP_FEATURES=all
```

`enable_features` swaps the per-session registry for `full_registry()` and adds
the background injector. Or compose exactly what you want:

```python
from mini_loop import full_registry, default_injectors
reg = full_registry(tasks=True, background=True, memory=True, cron=True, teams=True,
                    mcp_servers={"docs": my_mcp_client})
SessionManager(settings, client, tool_registry=reg, injectors=default_injectors())
```

Each module is also usable à la carte via its `install_*(registry)` helper.

| Chapter | Module | Enable | Adds |
|---|---|---|---|
| **s11** Error Recovery | `recovery.py` | `recovery=DefaultRecovery(...)` (on by default) | backoff on 429/529, 8k→64k token escalation, reactive compaction, fallback model (`FALLBACK_MODEL_ID`) |
| **s12** Task System | `tasks.py` | `install_tasks(reg)` | `create_task / list_tasks / get_task / claim_task / complete_task` — a file-backed `blockedBy` graph under `<ws>/.tasks/` |
| **s13** Background Tasks | `background.py` | `install_background(reg)` + `background_injector` | `background_run / check_background`; results arrive as `<task_notification>` via the injector (asyncio, not threads) |
| **s09** Memory | `memory.py` | `install_memory(reg)` (+ `memory_system_builder`) | `remember / recall`; Markdown memories + index; pass a shared dir for cross-session recall |
| **s14** Cron | `cron.py` | manager `enable_features` | `schedule_cron / list_crons / cancel_cron`; an asyncio ticker wakes the session on schedule; durable to `.cron.json` |
| **s15–17** Teams | `teams.py` | manager `enable_features` | `spawn_teammate` (a concurrent session sharing the workspace) + `send_message / read_inbox / broadcast / list_teammates` over a `MessageBus` |
| **s18** Worktrees | `worktrees.py` | `workspace_factory=worktree_workspace_factory(repo)` | each session runs in its own git worktree + branch (`wt/<id>`); falls back to a plain dir if not a git repo |
| **s19** MCP | `mcp.py` | `full_registry(mcp_servers=...)` or `install_mcp(reg, servers)` | `connect_mcp` discovers a server's tools and registers them as `mcp__<server>__<tool>`; transports: `InProcessMCP`, `StdioMCP` |

Notes:
* **Teams reframed.** Since every session already runs concurrently, teammates
  are *sub-sessions* (not threads) that share the spawner's workspace — so they
  share the `.tasks` board and `.memory`. The thread/idle-poll machinery from
  the tutorial is intentionally dropped; the coordination layer (mailbox +
  shared board) is what's kept. Teammates can't spawn teammates (fork-bomb
  guard).
* **Custom tools** can stash per-session services on `ctx.state` and emit custom
  events with `ctx.emit_event(...)` — that's exactly how these modules are
  built. Read any of them as a template.

---

## Concurrency & safety

* **Per session (isolated):** workspace, conversation history, `TodoManager`,
  `ctx.state`, the cloned `ToolRegistry`, the run `Lock`.
* **Shared across the fleet:** the LLM client, the `LLM semaphore` (caps
  simultaneous requests — `MINILOOP_MAX_CONCURRENT_LLM`), the `SkillLoader`
  (read-only), and your `Hooks` / `event_sink`. Keep those last two stateless
  or concurrency-safe.
* A session's runs are serialized by its `Lock` (one conversation = one
  history); different sessions run truly in parallel on the event loop. Make
  custom tools **non-blocking** — `await` real I/O, or wrap blocking calls in
  `asyncio.to_thread` (the built-in file/bash tools already do).
