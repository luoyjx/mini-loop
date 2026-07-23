# mini-loop

A **minimal but complete-capability** coding agent, served as a **concurrent
multi-agent FastAPI server**.

The agent is the `s01` loop from
[`learn-claude-code`](./learn-claude-code/) with the essential harness
mechanisms layered on top — and nothing more:

```
Agent  = one async loop                              (s01)
       + tools: bash / read / write / edit / glob    (s02, workspace-scoped)
       + TodoWrite plan-then-execute            (s05)
       + subagent delegation (`task`)           (s06, fresh context)
       + on-demand skill loading                (s07)
       + four-layer context compaction          (s08)

Server = FastAPI + SessionManager
       + one isolated Agent per session
       + SSE live event stream
       + true concurrency across sessions
```

The one thing this project deliberately **does not** copy from the reference
`s_full.py` is its module-global state (`WORKDIR`, `TODO`, `TASK_MGR`, …). A
concurrent server needs every session fully isolated, so here **everything is
instance-based and async** — thousands of agents can run in one process without
sharing history or todos. File tools enforce workspace boundaries; shell commands
run with that workspace as their `cwd` and still require normal OS-level isolation
if untrusted users can submit prompts.

---

## Why it's actually concurrent

The agent loop is `async`. LLM calls go through `AsyncAnthropic`
(non-blocking network I/O); blocking tool calls (`subprocess`, file I/O) are
offloaded with `asyncio.to_thread`. So while agent A waits on the model, agent
B's loop keeps running on the same event loop.

Measured with the bundled fake model (0.3s/call, 2 calls per run, 5 sessions):

| Mode                         | Wall time |
|------------------------------|-----------|
| 5 runs **sequential**        | 3.22 s    |
| 5 runs **concurrent**        | 0.67 s    |

A single global semaphore (`MINILOOP_MAX_CONCURRENT_LLM`, default 8) caps how
many LLM requests are in flight at once, so concurrency never blows the
provider's rate limit. A per-session `asyncio.Lock` serializes a *single*
session's runs (one conversation = one history) while different sessions stay
parallel.

Within one model turn, consecutive tools registered with
`parallel_safe=True` run concurrently. A second global semaphore
(`MINILOOP_MAX_CONCURRENT_TOOLS`, default 8) bounds those calls across all
sessions. Results are returned to the model in its original call order even
when completion order differs. Any tool not marked parallel-safe is an
ordering barrier; built-in `read_file` and `glob` opt in, while `bash` and
mutation tools remain sequential.

---

## Layout

```
mini_loop/
  config.py      env + LLM-client factory (anthropic import is lazy)
  tools.py       per-workspace bash/read/write/edit/glob + async dispatch + safe_path
  registry.py    Tool / ToolRegistry / ToolContext + Hook / Hooks   ← extension seam
  permissions.py deny-list + PermissionRule + async approval callback
  builtins.py    the built-in tools as Tools; default/explore/worker registries
  skills.py      SkillLoader — index descriptions, load bodies on demand
  prompts.py     system_builder (default + sections_builder)
  compaction.py  Compactor protocol + budget / snip / micro / auto layers
  agent.py       the async loop: dispatch via registry + hooks + compactor
  session.py     AgentSession — history, status, event pub/sub, per-session lock
  manager.py     SessionManager — injects every seam, shared client + semaphore
  server.py      create_app() factory: REST + SSE + browser console at /
  fake_llm.py    deterministic offline stand-in for AsyncAnthropic
  __main__.py    `python -m mini_loop` → uvicorn
skills/code_review/SKILL.md   sample skill (loadable via load_skill)
examples/custom_agent.py      all seams composed into a domain agent + custom server
tests/           offline tests (no key): loop, sandbox, subagent, compaction,
                 server, concurrency, and every extension seam
```

---

## Extending it (build your own business)

Every capability around the loop is a **swappable seam** you inject at
construction — no core edits. Add a tool, gate it with a permission hook, swap
the prompt/compaction/provider, provision per-tenant sandboxes, tap the event
stream:

```python
from mini_loop import SessionManager, build_client, load_settings, Hooks, default_registry
from mini_loop.server import create_app

registry = default_registry()

@registry.add("web_search", "Search the web.",
              {"type":"object","properties":{"query":{"type":"string"}},"required":["query"]})
async def web_search(ctx, query):
    return await my_search_api(query)        # ctx.workspace / ctx.state are yours

s = load_settings()
manager = SessionManager(s, build_client(s),
                         tool_registry=registry,          # your tools
                         hooks=Hooks([MyPolicy()]),       # permissions / audit
                         system_builder=my_prompt,        # prompt assembly
                         workspace_factory=per_tenant_dir,# sandbox provisioning
                         event_sink=my_metrics)           # observability
app = create_app(manager=manager)            # same REST + SSE + console
```

| Seam | Inject via | Changes |
|---|---|---|
| `ToolRegistry` | `tool_registry=` | what the agent can do |
| `Hooks` | `hooks=` | permissions, audit, arg/output rewriting |
| `system_builder` | `system_builder=` | the system prompt |
| `Compactor` | `compactor=` | context trimming/summarization |
| `SkillLoader` + `skills/` | `skills=` | on-demand domain knowledge |
| LLM client | `client=` / env | model / provider / base_url |
| `workspace_factory` | `workspace_factory=` | where/how the sandbox is provisioned |
| `event_sink` | `event_sink=` | global metrics / logging / persistence |

**Full guide with interfaces + runnable examples for each module:
[EXTENDING.md](./EXTENDING.md).**

### Feature coverage (learn-claude-code s01–s20)

The core loop is always on. The rest ship as optional modules — flip them all on
with `MINILOOP_FEATURES=all` (or `SessionManager(enable_features=True)` /
`full_registry()`):

| | Mechanism | | Mechanism |
|---|---|---|---|
| s01 | agent loop ✅ | s11 | error recovery ✅ (`recovery=`) |
| s02 | bash/read/write/edit/glob ✅ | s12 | task system ✅ (`install_tasks`) |
| s03 | permissions ✅ (`PermissionHook`) | s13 | background tasks ✅ (auto + explicit) |
| s04 | lifecycle hooks ✅ | s14 | durable cron ✅ (asyncio scheduler) |
| s05 | TodoWrite ✅ | s15–17 | teams + protocols + autonomy ✅ |
| s06 | subagent ✅ | s18 | task-bound worktrees ✅ |
| s07 | skills ✅ | s19 | MCP ✅ (`connect_mcp`, in-process + stdio) |
| s08 | four-layer compaction ✅ | s20 | comprehensive ✅ (`full_registry`) |
| s09 | auto memory lifecycle ✅ | s10 | per-call runtime prompt ✅ |

```sh
MINILOOP_FAKE_LLM=1 MINILOOP_FEATURES=all python -m mini_loop   # all built-in modules, no key
```

`MINILOOP_REPO_ROOT=/path/to/repo` gives the worktree tools a target repository.
MCP servers are application dependencies, supplied through `mcp_servers=` when
constructing `SessionManager`; `connect_mcp` remains present and reports the
configured server names.

---

## Quick start

```sh
python -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
cp .env.example .env            # set ANTHROPIC_API_KEY + MODEL_ID
python -m mini_loop             # http://127.0.0.1:8000  (open it: live console)
```

### Run with no API key

A deterministic fake model lets you exercise the whole stack offline:

```sh
MINILOOP_FAKE_LLM=1 MINILOOP_FAKE_DELAY=0.3 python -m mini_loop
```

Open <http://127.0.0.1:8000> in **two browser tabs** and run both — the pushed
event panels show both agents working in parallel, with expandable SSE payloads.
Every run is also recorded locally; the console can reopen it and export JSON
or JSONL. See [Agent trajectories](docs/TRAJECTORIES.md).

---

## API

| Method | Path | Body | Purpose |
|--------|------|------|---------|
| GET    | `/`                              | — | embedded console |
| GET    | `/healthz`                       | — | liveness + config |
| POST   | `/sessions`                      | `{system?, model?}` | create an isolated agent |
| GET    | `/sessions`                      | — | list sessions |
| GET    | `/sessions/{id}`                 | — | status, todos, message count |
| DELETE | `/sessions/{id}`                 | — | drop session + workspace |
| POST   | `/sessions/{id}/messages`        | `{message}` | run to completion → final text |
| POST   | `/sessions/{id}/messages/stream` | `{message}` | run, stream live events (SSE) |
| GET    | `/sessions/{id}/events`          | — | persistent session event feed (SSE; `?envelope=true` emits one generic event name) |
| GET    | `/sessions/{id}/trajectories`    | — | list durable runs for a session |
| GET    | `/trajectories`                  | — | list all durable runs; filter with `?session_id=` |
| GET    | `/trajectories/{trajectory_id}`  | — | inspect an assembled trajectory |
| GET    | `/trajectories/{trajectory_id}/export` | — | download `?format=json` or `jsonl` |

```sh
# create a session, then send it a task
SID=$(curl -s -XPOST localhost:8000/sessions -d '{}' -H content-type:application/json | jq -r .id)
curl -s -XPOST localhost:8000/sessions/$SID/messages \
     -H content-type:application/json -d '{"message":"write fib.py and run it"}' | jq

# watch it work live
curl -sN -XPOST localhost:8000/sessions/$SID/messages/stream \
     -H content-type:application/json -d '{"message":"now add tests"}'
```

SSE event types include `status`, `model_start`, `model_end`, `assistant_text`,
`tool_use`, `tool_result`, `trajectory_start`, `trajectory_end`,
`permission`, `recovery`, `memory`, `background_result`, `team_inbox`,
`subagent_start`, `subagent_end`, `todo`, `compact`, `done`, `error`, plus
custom hook events. Every event carries `seq`, `ts`, `session`, and `type`;
agent-originated events also carry `agent` + `depth`. Event IDs support safe
reconnection with `Last-Event-ID`.

---

## Tests

All offline (injected fake model — no key, no network):

```sh
.venv/bin/python -m pytest -q
# 96 passed, 24 subtests passed
```

Covers the loop, sandbox and concurrency guarantees, permissions and all hook
phases, four-layer compaction, automatic cross-session memory, recovery paths,
atomic task claims, background notifications, strict/durable cron, team
protocols and autonomous claiming, task-bound worktrees, MCP stdio, REST and SSE.

---

## Configuration

All via env (see `.env.example`): `ANTHROPIC_API_KEY`, `MODEL_ID`,
`ANTHROPIC_BASE_URL` (for Anthropic-compatible providers — GLM / MiniMax /
Kimi / DeepSeek), plus `MINILOOP_*` knobs for concurrency cap, turn limits,
token budget, compaction threshold, bash timeout, and the workspace/skills
directories. Comprehensive-mode settings also include `MINILOOP_MEMORY_ROOT`,
`MINILOOP_REPO_ROOT`, `MINILOOP_TEAM_IDLE_POLL`, and
`MINILOOP_TEAM_IDLE_TIMEOUT`. Trajectory settings are
`MINILOOP_TRAJECTORIES`, `MINILOOP_TRAJECTORY_ROOT`, and
`MINILOOP_TRAJECTORY_CAPTURE_CONTENT`; recording is enabled locally by default.
```
