# mini-loop

A **minimal but complete-capability** coding agent, served as a **concurrent
multi-agent FastAPI server**.

The agent is the `s01` loop from
[`learn-claude-code`](./learn-claude-code/) with the essential harness
mechanisms layered on top — and nothing more:

```
Agent  = one async loop                         (s01)
       + tools: bash / read / write / edit      (s02, sandboxed per session)
       + TodoWrite plan-then-execute            (s05)
       + subagent delegation (`task`)           (s06, fresh context)
       + on-demand skill loading                (s07)
       + micro + auto context compaction        (s08)

Server = FastAPI + SessionManager
       + one isolated Agent per session
       + SSE live event stream
       + true concurrency across sessions
```

The one thing this project deliberately **does not** copy from the reference
`s_full.py` is its module-global state (`WORKDIR`, `TODO`, `TASK_MGR`, …). A
concurrent server needs every session fully isolated, so here **everything is
instance-based and async** — thousands of agents can run in one process without
touching each other's history, workspace, or todos.

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

---

## Layout

```
mini_loop/
  config.py     env + LLM-client factory (anthropic import is lazy)
  tools.py      per-workspace bash/read/write/edit + async dispatch + safe_path
  skills.py     SkillLoader — index descriptions, load bodies on demand
  agent.py      the async agent loop: tools + todo + subagent + skills + compaction
  session.py    AgentSession — history, status, event pub/sub, per-session lock
  manager.py    SessionManager — registry, shared client + semaphore, isolation
  server.py     FastAPI app: REST + SSE, plus a tiny browser console at /
  fake_llm.py   deterministic offline stand-in for AsyncAnthropic
  __main__.py   `python -m mini_loop` → uvicorn
skills/code_review/SKILL.md   sample skill (loadable via load_skill)
tests/          offline tests (no API key): loop, sandbox, subagent, compaction, server, concurrency
```

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

Open <http://127.0.0.1:8000> in **two browser tabs** and run both — you'll
watch two agents work in parallel.

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
| GET    | `/sessions/{id}/events`          | — | observe a session's events (SSE) |

```sh
# create a session, then send it a task
SID=$(curl -s -XPOST localhost:8000/sessions -d '{}' -H content-type:application/json | jq -r .id)
curl -s -XPOST localhost:8000/sessions/$SID/messages \
     -H content-type:application/json -d '{"message":"write fib.py and run it"}' | jq

# watch it work live
curl -sN -XPOST localhost:8000/sessions/$SID/messages/stream \
     -H content-type:application/json -d '{"message":"now add tests"}'
```

SSE event types: `status`, `assistant_text`, `tool_use`, `tool_result`,
`subagent_start`, `subagent_end`, `todo`, `compact`, `done`, `error`. Every
event carries `agent` + `depth`, so subagent activity is visibly nested.

---

## Tests

All offline (injected fake model — no key, no network):

```sh
.venv/bin/python -m pytest -q
# 37 passed
```

Covers the loop, the max-turns guard, workspace sandbox escape, async tool
dispatch, TodoWrite validation, micro/auto compaction, subagent delegation +
context isolation, skill loading, every server endpoint + SSE, and the headline
**concurrency** and **per-session isolation** guarantees.

---

## Configuration

All via env (see `.env.example`): `ANTHROPIC_API_KEY`, `MODEL_ID`,
`ANTHROPIC_BASE_URL` (for Anthropic-compatible providers — GLM / MiniMax /
Kimi / DeepSeek), plus `MINILOOP_*` knobs for concurrency cap, turn limits,
token budget, compaction threshold, bash timeout, and the workspace/skills
directories.
```
