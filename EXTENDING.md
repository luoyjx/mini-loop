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
| `registry.py` | immutable `ToolCatalogSnapshot` | automatic per request | the exact schema/prompt prefix and its fingerprint |
| `tool_policy.py` | `RoleToolPolicy` | `role_tool_policy=` | which declared capabilities Explore/Worker children inherit |
| `registry.py` | `Hooks` (`Hook`) | `hooks=` | permissions, audit, arg/output rewriting |
| `prompts.py` | `system_builder(agent)->str` | `system_builder=` / `system=` | the system prompt |
| `compaction.py` | `Compactor` | `compactor=` | how context is trimmed/summarized |
| `caching.py` | `CachePolicy` | `cache_policy=` | where prompt-cache breakpoints go |
| `storage.py` | `StateStore` | `state_store=` | whether a session survives a restart |
| `secrets.py` | `SecretRegistry` | `secrets=` | which credentials a tool can see or print |
| `sandbox.py` | `Sandbox` | `sandbox=` | what the shell can reach on the host |
| `stuck.py` | `StuckDetector` | `stuck_detector=` | when a repeating agent is nudged or halted |
| `recovery.py` | `RecoveryPolicy` | `recovery=` | retry/backoff/token-escalation/fallback on LLM errors |
| `token_efficiency.py` | `TokenEfficiencyRuntime` | `token_efficiency=` | post-mask observations, request copies, and response policy |
| `ast_context.py` | `AstOutlineAdapter` | settings or `install_ast_context_tools()` | typed, bounded semantic code reads |
| `agent.py` | `injectors` (`async (agent)->msgs`) | `injectors=` | splice messages into each turn (background, cron) |
| `skills.py` | `SkillLoader` | `skills=` + `skills/` dir | deployment-managed Agent knowledge |
| `user_resources.py` | `UserResourceResolver` | `user_resources=` or `MINILOOP_USER_RESOURCES_ROOT` | owner-scoped user skills and Markdown memory |
| `config.py` | LLM client | `build_client` / `client=` | model / provider / base_url |
| `manager.py` | `workspace_factory(id)->Path` | `workspace_factory=` | where/how the sandbox is provisioned |
| `session.py` | `event_sink(event)` | `event_sink=` | global metrics / logging / persistence |
| `server.py` | `create_app(manager=...)` | app factory | serving a customized fleet |

A `RecoveryPolicy` receives `live_history=` — the agent's own message list when
the failing request *was* the conversation, `None` otherwise. Reactive
compaction needs it: it used to mutate the request list on the assumption that
it aliased `agent.messages`, which stopped being true once a `CachePolicy`
started annotating onto a copy. Shrinking only the retry leaves the next turn to
rebuild the same oversized prompt.

---

## 0a. Who is calling — `Authenticator`

`app.state.auth` is resolved per request, so rotating a token takes effect
without restarting or re-registering routes. `NullAuth` (the default) makes every
caller one anonymous principal, which is why `refuse_open_bind` turns a
non-loopback bind without tokens into a startup failure rather than a warning.

Shape from the OpenHands agent server: authentication as a dependency on the
router rather than a check repeated per handler, config read at request time,
and any credential channel opened for one surface staying scoped to it. Two
deliberate differences — constant-time comparison, and refusing to serve rather
than defaulting open.

---

## 0. The policy set — `Harness`

Every seam below can be passed to `Agent`/`SessionManager` individually. They
are also fields of one value:

```python
from mini_loop import Harness, SessionManager

harness = Harness(
    hooks=my_hooks,
    secrets=registry,
    sandbox=sandbox,
    token_efficiency=my_runtime,
    role_tool_policy=my_role_policy,
)
SessionManager(settings, client, ...)          # assembles one internally
Agent(client=..., settings=..., workspace=..., harness=harness)
```

**Why a value and not a parameter list.** An `Agent` is constructed in three
places — the manager, subagent delegation, and workflow workers — and each used
to keep its own list of what to pass. Over five added seams, two sites were
missed: workflow workers ran without secret masking or sandboxing because they
were built directly rather than through the manager. Nothing failed; the
capability was simply absent on one path.

Deriving a variant copies the whole value and overrides only what differs:

```python
child = parent.harness.derive(tools=narrow_registry, hooks=Hooks())
```

So a seam added to `Harness` reaches every construction site the moment it is
added — you cannot forget a field you never had to type. This is the shape
OpenHands uses for `AgentBase`, where the agent's whole configuration is a
single serializable model rather than a call signature.

`tests/test_harness.py` enforces it structurally: it AST-scans the package for
`Agent(...)` calls and fails if one omits `harness=` or passes a seam alongside
it.

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
    readonly=True,
    parallel_safe=True,
    capabilities={"repo.search"},
)
async def web_search(ctx, query):          # ctx + your schema properties
    return await my_concurrency_safe_search_client(query)  # return a string
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

A tool may also carry `verify=async (ctx, call) -> bool | None`, consulted only
when a crash left its action `unknown`:

```python
async def already_sent(ctx, call):
    return await my_api.message_exists(call.input["idempotency_key"])

registry.register(Tool("send_email", ..., handler, verify=already_sent))
```

`True` records it as done without re-running; `False` is the only verdict that
permits a retry; `None` — including a verifier that raises — leaves it unknown,
because failing to check is not evidence that nothing happened. `write_file`
ships with one; `bash` does not, and cannot: an opaque command string carries no
statement of intent to check against. That asymmetry is the concrete payoff of
promoting a side effect out of the shell into a typed tool.

Set `parallel_safe=True` only when the handler, its hooks, and any external
client it uses can overlap safely. Consecutive parallel-safe calls from one
model response run together under `MINILOOP_MAX_CONCURRENT_TOOLS`; result
blocks still follow the model's original call order. A tool without this flag
is an ordering barrier. `readonly` remains separate because a nominal read may
still drain a queue or mutate external state.

**Remove or replace built-ins:**

```python
registry.unregister("bash")                      # no shell for this product
registry.register(my_bash_tool, replace=True)    # swap the implementation
readonly = registry.subset(["read_file", "web_search"])
```

`registry.snapshot()` fits the catalogue once and returns an immutable
`ToolCatalogSnapshot`: canonical schema JSON, sent/omitted names, registry
revision, and a SHA-256 fingerprint. During one model request both
`default_system_builder` and `tools=` consume that same snapshot, so cache
identity cannot drift because the registry was fitted twice. `schemas()`
returns a detached copy; provider/cache annotation cannot mutate the snapshot.

Subagents no longer rebuild a list of concrete tool names. Every `Tool` may
declare stable `capabilities`; the default `CapabilityRoleToolPolicy` selects a
subset of the **parent** registry. Explore inherits `repo.read`, `repo.search`,
`repo.semantic_outline`, `repo.symbol`, and `repo.references`; Worker adds
`workspace.write`, `process.exec`, and `observation.recover` when those
capabilities exist in the parent registry. A tool with no capability is not
inherited implicitly. Explore also runs in read-only permission mode, so capability
selection and execution-time authority agree.

---

## 2. Hooks — permissions, lifecycle, audit, rewriting

A `Hook` wraps tool calls and the outer turn lifecycle. All methods are async;
override only the phases you need.

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

    async def on_user_prompt(self, agent, text):
        return text                              # may rewrite the prompt

    async def on_stop(self, agent, messages, last_text):
        return None                              # string = force another round

Agent(..., hooks=Hooks([Policy(), AnotherHook()]))     # ordered chain
SessionManager(settings, client, hooks=Hooks([Policy()]))
```

`before_tool` runs in order; the **first** hook to return a string wins and
short-circuits the call. `after_tool` and `on_user_prompt` transform in order;
the first `on_stop` continuation keeps the loop alive. Hooks apply to
subagents too. Keep hooks stateless (or guard their state) since one `Hooks`
instance is shared across concurrent sessions.

The default chain contains `PermissionHook`: an immutable command deny-list,
ordered `PermissionRule`s, and an optional async approval callback. With no UI
callback, an `ask` decision fails closed. Passing an explicit `Hooks(...)`
chain replaces the default, so include `PermissionHook` when a custom fleet
still needs the standard policy.

---

## 3. System prompt — `system_builder`

The prompt is rebuilt before every model call, so it reflects the current
workspace, tools, skills, TodoWrite state, memory index, and team identity.

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
`compress` tool. The default (`DefaultCompactor`) runs four ordered layers:
oversized-result persistence under `.task_outputs/tool-results/`, pair-safe
middle snipping, old-result micro compaction, then transcript + LLM summary
past the token threshold.

---

## 3b. Shell confinement — `Sandbox`

`run_bash` sets `cwd` to the workspace, but a shell can `cd /`. This seam puts
an OS-level boundary around it. Default is `NullSandbox` — host execution,
trusted callers only, exactly as before.

```python
from mini_loop import SeatbeltSandbox, SessionManager, default_sandbox

sandbox = SeatbeltSandbox(
    writable_roots=[workspace],
    unreadable_roots=[Path.home() / ".ssh", Path.home() / ".aws"],
    allow_network=False,
)
SessionManager(settings, client, sandbox=sandbox)
# or: default_sandbox(workspace)  -> Seatbelt on macOS, NullSandbox elsewhere
```

Modelled on the OpenAI Codex CLI sandbox (`codex-rs/sandboxing/`), whose policy
shape encodes decisions worth copying verbatim:

* **Deny by default**, then re-grant what a shell needs to start.
* **Reads broad, writes narrow.** A process that cannot read `/bin/sh` or the
  dynamic linker is not a shell. Confinement lives on the write side, plus an
  explicit read deny-list for paths that matter.
* **Paths are `-D` parameters, never interpolated** into the policy body, so a
  workspace path containing policy syntax cannot rewrite the policy.
* **An excluded root needs two clauses.** `(require-not (subpath X))` alone does
  not cover creating `X` itself; upstream pairs it with `(require-not (literal
  X))` and cites `mkdir .codex`.
* **Network is additive** — its absence is the denial.
* **`/usr/bin/sandbox-exec` is hardcoded**, never resolved through `PATH`.

Verified by execution, not by inspecting the policy string: `tests/test_sandbox.py`
runs real commands and asserts a canary outside the roots is unreadable, writes
outside are denied, the network is unreachable, and a shell still works.

A sandbox rebinds itself when the workspace moves: `Agent.enter_workspace`
switches into a per-task worktree (s18), and a sandbox still holding the
previous workspace as its only writable root would deny every write in the new
one — silently, and only in sandboxed deployments. `Sandbox.for_workspace()` is
part of the protocol for that reason; extra writable roots and the read/network
policy survive the rebind.

**Two real limits.**

*It is macOS-only.* `default_sandbox` elsewhere returns an `UnavailableSandbox`:
commands still run, but the posture carries `sandbox_reason` and the audit
raises a distinct `shell-confinement-unavailable` finding whose remedy is a
container, not a configuration change. Pass `require=True` to refuse at
construction instead.

*It is not a container.* No CPU, memory, PID or wall-clock caps, so it does not
stop a fork bomb — the roadmap's resource-exhaustion criterion needs
`DockerWorkspace`. A test pins this as a known gap.

*A read deny-list only protects what you list.* This bit the first version of
the end-to-end test: the model could not read the protected file, but found the
canary in neighbouring files that were never listed. Copies leak; a container's
filesystem view does not have that failure mode.

---

## 3a. Secrets — `SecretRegistry`

`run_bash` inherits the whole process environment, so an agent that runs
`printenv` puts the host's API keys into the tool result — and from there into
the model's context, the SSE stream the console renders, the trajectory, and the
SQLite state store. Four sinks, one leak. Measured before/after on a canary key:
4 of 4 leaked, then 0 of 4.

```python
from mini_loop import SecretRegistry, SessionManager

secrets = SecretRegistry.from_environ()   # names matching *_API_KEY, *_TOKEN, ...
SessionManager(settings, client, secrets=secrets)
```

The split is taken from the OpenHands SDK `SecretRegistry`:

* **Injection is narrow.** A shell command receives a registered credential only
  when the command *names* it, so a bare `printenv` has nothing to read. Naming
  it does hand it over — that is deliberate, so legitimate use works.
* **Masking is wide, and runs both ways.** Every registered secret's *value* is
  scrubbed from tool output whether or not the command named it — a value can
  arrive without one (upstream's example: a token inside a git remote URL) — and
  from tool *arguments*, which are model-generated and were the leakier side:
  before this, a credential a model wrote into a command reached the event
  stream, the console, the trajectory and the durable tables.

Masking applies to what is recorded and emitted, never to what is executed —
the live `ToolCall` keeps the real value. The in-memory transcript keeps it too,
deliberately: it goes back to the provider that already has it and dies with the
process.

Two properties are load-bearing and easy to get wrong:

* **Cached values are never re-resolved.** After a secret rotates the registry
  keeps masking the value it previously handed out; re-resolving would let the
  old value start appearing again.
* **Short values are reported, not masked.** A floor of 8 characters keeps a
  two-character "secret" from shredding unrelated output;
  `registry.short_values()` names any that were skipped.

Masking is applied where a tool result is produced, which is upstream of all
four sinks at once, and again inside `run_bash` so a direct `Toolset` caller is
covered too.

**What it does not cover:** text the *model* writes. If a model reads a
credential and repeats it in prose, that is its own output, not a tool result.
None of this substitutes for not giving an agent credentials it does not need.

---

## 4. Durable conversation state — `StateStore`

`TrajectoryStore` records what happened, for audit, and redacts content when
asked. `StateStore` is the other half: the state a *different process* needs to
resume a session — the model-facing transcript and the event cursor an SSE
client reconnects against. Off by default; persistence is a deployment choice,
and the database holds unredacted transcripts.

```python
from mini_loop import SessionManager, SQLiteStateStore

store = SQLiteStateStore("var/state.db")          # WAL, schema-versioned
manager = SessionManager(settings, client, state_store=store)

# ... process dies ...

manager = SessionManager(settings, client, state_store=SQLiteStateStore("var/state.db"))
for session in manager.restore_sessions():        # transcript + cursor rebuilt
    await session.run("continue where we left off")
```

The backend is SQLite because the action journal that comes next needs actions
and events ordered inside one transaction, which a file log cannot give. The
*append contract* is taken from the OpenHands SDK `EventLog`, which solves the
same problem over a file store, and whose properties are backend-independent:

| Property | OpenHands (file store) | Here (SQLite) |
|---|---|---|
| Order is data | ordinal encoded in the filename, index rebuilt by `listdir` | explicit `ordinal` column, `UNIQUE(session_id, ordinal)` |
| Re-read the head before appending | take a file lock, then re-scan the directory | read `MAX(ordinal)` inside the writing transaction |
| Never materialize history to read a tail | one event file per read | every read takes `after` / `limit` |

A stale writer therefore hits an integrity error rather than silently
reordering history. Persistence failures are reported on
`session._persist_error` and never stall the agent — the same contract the
trajectory sink already follows.

**Compaction and the append-only table.** `agent.messages` is mutable and
compaction rewrites it; an append-only table cannot mirror that by index. The
store versions each transcript with an `epoch`: when the live transcript stops
extending what was persisted -- shortened, or edited in the middle -- the next
flush opens a new epoch and rewrites it whole, leaving the superseded epoch on
disk. This is the structural difference from OpenHands, whose log is never
rewritten at all: condensation is another event and the conversation is a
projection over the log.

**A crash mid-tool leaves an unanswered call.** Killed between dispatching a
tool and recording its result, the persisted transcript ends with a `tool_use`
and no `tool_result` — which the provider rejects outright (`tool_use ids were
found without tool_result blocks immediately after`), so an unrepaired session
fails on *every* subsequent turn, not subtly. `restore()` closes such calls with
an explicit **unknown** result rather than an error: the tool was dispatched and
whether it completed is genuinely not known, and reporting failure invites a
retry of a side effect that may already have happened. Given the unknown result
a live model verified before acting rather than repeating the call. The repair
is persisted, so a second restart does not rediscover it.

**The journal is a replay guard, not an audit log.** `begin()` always returned
the existing record on replay, and `_exec_tool` discarded it and executed
anyway — so the journal recorded side effects without preventing a second one.
It now decides:

* a **terminal** record means the step already ran, so the recorded result is
  returned and the tool is not called again (the `action_id`, derived from
  session + message + tool_use id + tool name, is the idempotency key);
* an **unknown** record means a dead process dispatched it, so the unknown
  marker is returned rather than a retry.

`DurableActionJournal` stores this in the same SQLite database, and opening the
store marks every still-`started` action `unknown` — never `failed`, which would
invite exactly the retry the rule forbids. A replayed tool result carries
`replayed: True` on its event so an operator can see a resumed turn reused a
recorded outcome.

This shape comes from durable-execution engines (Temporal, Restate, Azure
Durable Task) rather than from any agent harness: harnesses commonly journal
actions for audit and re-run them regardless.

Still missing: reconciliation. Nothing yet asks an external system whether an
`unknown` action landed — the harness only refuses to guess.

**Agent-side state.** The transcript mentions the plan but does not rebuild it,
so the store also carries the TodoWrite board; without it a restored session has
an empty board while its own transcript shows otherwise, silently disabling the
s05 nag and the runtime-state reminder. The session row is refreshed on every
flush rather than only at creation — otherwise `run_count` and `status` stay
frozen at their initial values for the life of the session.

**What this does not do yet.** No action journal, no outbox, no run state
machine, no cross-process claim or lease, no fork/snapshot. Two processes on one
database read consistently, but nothing stops both from advancing the same
session. A process killed mid-run is restored with its recorded status
(`running`) and no run attached — surfacing the truth rather than inventing a
resume that the missing run state machine cannot honour.

---

## 4a. Prompt caching — `CachePolicy`

A provider renders a request as `tools` → `system` → `messages` and caches it by
**prefix match**: one changed byte at position N invalidates every cached token
after it. Two things follow, and the first matters more than the second.

**Keep volatile state out of the prefix.** The system prompt used to carry the
TodoWrite board and the memory index, so every turn the model updated its plan
it also invalidated the whole conversation. `default_system_builder` now emits
only agent-lifetime-stable facts; `prompts.runtime_facts` returns the volatile
half, and `runtime_facts_injector` (installed by default) delivers it through
the *message stream*, re-sending only when it actually changes. Appending to the
end invalidates nothing before it. Measured on a real 4-turn session: 3 distinct
system payloads before, 1 after.

A custom `system_builder` that interpolates changing state is still allowed —
it is correct, just uncacheable. That is a trade, not a bug.

**Then place breakpoints.** `DefaultCachePolicy` spends one on the last system
block (which covers `tools` too — they render first) and walks the rest back
through the conversation:

```python
from mini_loop import DefaultCachePolicy, NullCachePolicy

Agent(..., cache_policy=DefaultCachePolicy(ttl="1h"))
SessionManager(settings, client, cache_policy=NullCachePolicy())  # opt out
```

Placement is **per content block, not per message**. A breakpoint only searches
back a bounded number of blocks for a prior entry, and mini-loop executes a
*batch* of tool calls per round — one assistant turn with N `tool_use` blocks
plus one user turn with N `tool_result` blocks — so marking just the newest turn
leaves the next request's breakpoint out of range. Only user turns are marked:
assistant content is provider objects that round-trip untouched.

Annotation happens on per-request copies, so `agent.messages` never acquires
provider-specific keys.

**Known limit:** a round wider than the lookback window cannot be fully chained
within the 4-breakpoint budget — a batch of N tools contributes N unmarkable
assistant blocks. The newest entry is still written and earlier ones stay
readable, so the cache degrades rather than breaking. `test_caching.py` pins
this so a future change has to acknowledge it.

Verify with `usage.cache_read_input_tokens`. If it is zero across repeated
requests, something upstream is still changing the prefix — or the provider
does not implement Anthropic-style caching (DeepSeek's compat endpoint accepts
the blocks but reports no cache usage).

---

## 4b. Loop detection — `StuckDetector`

`max_rounds` bounds work but cannot tell productive rounds from an agent
retrying one denied call thirty times. A `StuckDetector` inspects the shape of
recent activity and reports unproductive repetition before the budget is spent.

```python
from mini_loop import DefaultStuckDetector, NullStuckDetector, StuckThresholds

# defaults: 4 identical call+result, 3 identical call+error, 5 unproductive
# uses of one tool, 6-step ping-pong, 3 tool-less turns; one corrective nudge
# before halting the turn.
Agent(..., stuck_detector=DefaultStuckDetector(StuckThresholds(max_nudges=0)))
SessionManager(settings, client, stuck_detector=NullStuckDetector())  # opt out
```

The five rules, in the order they are checked:

| Pattern | Fires when | Default |
|---|---|---|
| `repeat_action_error` | N consecutive identical calls, all failed or denied | 3 |
| `unproductive_tool` | N unproductive uses of one tool in the window, **never** successful — order and arguments irrelevant | 5 |
| `repeat_action_result` | N consecutive identical calls with identical output | 4 |
| `alternating` | two calls ping-ponging with stable outputs | 6 |
| `monologue` | consecutive tool-less turns a `stop` hook keeps resuming | 3 |

`unproductive_tool` is the one rule with no upstream counterpart. Every
consecutive-and-identical detector (ours, OpenHands, Cline's
`LoopDetectionTracker`, opencode's doom-loop check) is blind to a model that
varies its arguments or interleaves other calls; Cline pairs its detector with
a reset-on-success `MistakeTracker`, which a measured trace of this harness
showed also misses — the model alternated a denied call with a *succeeding*
workaround call, so the consecutive-failure count never got above 1. Scoping
per tool and dropping the ordering requirement covers it, while a tool that
sometimes succeeds is treated as flaky rather than stuck.

The protocol is one method, and the detector is a **stateless policy** — the
history lives on the agent, so a single instance is safely shared by every
session in a manager:

```python
class NoRepeatedWrites:
    max_nudges = 1                      # nudges before the turn halts
    def inspect(self, agent):           # -> StuckSignal | None
        steps = agent.recent_steps      # tuple[ToolStep], oldest first
        if len(steps) >= 2 and steps[-1].same_call(steps[-2]):
            return StuckSignal("repeat_write", "Same write twice.", steps[-1].name)
        return None
```

`ToolStep` is `(name, input_hash, output_hash, failed, denied)` — ids, spans,
and durations are excluded so two calls compare equal when the model asked for
the same thing and got the same thing back. The ledger is bounded to the last
`STUCK_WINDOW` (20) calls, recorded in *batch order* (not completion order, so
parallel-safe groups stay deterministic), and cleared on each new user turn.

On detection the loop emits a `stuck` event (`pattern`, `detail`, `tool`,
`halted`, `nudges_used`). While nudges remain, the signal's reminder text is
spliced into the tool-result block and the loop continues; once they are spent
the turn ends. `agent.rounds_without_tools` powers the monologue rule, which
only fires when a `stop` hook keeps resuming a model that stopped calling
tools.

Rules and default thresholds are ported from the OpenHands SDK `StuckDetector`
(`OpenHands/software-agent-sdk`), adapted from one-action-per-step events to
mini-loop's batched tool calls.

---

## 4c. Token-efficiency stages

Token efficiency is a staged projection pipeline, not an output-string hook:

```text
tool authority
  -> Hook.after_tool
  -> SecretRegistry.mask
  -> ObservationReducer(s)
  -> SecretRegistry.mask (again, across the plugin boundary)
  -> action journal: bounded masked authority
  -> event / trajectory / model transcript: guarded projection

provider request copy
  -> RequestContextOptimizer(s)
  -> role + tool_use/tool_result protocol guard
  -> CachePolicy.annotate
  -> provider
```

`TokenEfficiencyRegistry` has three typed stages:

* `ObservationReducer` receives only an already-masked `MaskedObservation`;
* `RequestContextOptimizer` receives a deep copy plus the cache-stable
  `frozen_prefix_messages` boundary;
* `ResponsePolicy` returns stable instructions and output-budget settings
  before cache annotation.

Each component exposes a `ComponentDescriptor` (id, version, stage, content
types, determinism, lossiness, network access, timeout and limits). Every
attempt creates an internal `OptimizationReceipt` with digests, before/after
byte and token estimates, status, reason, bounded warning codes, elapsed time
and optional `raw_ref` — never the observation content. Normal events omit
warnings and all content digests, exposing only a `warning_count`. Modes have
strict semantics:

| Mode | Calls component | Changes model view |
|---|---:|---:|
| `off` | no | no |
| `shadow` | yes | no; emits candidate receipt |
| `enforce` | yes | only after frozen-prefix, inflation and double-reduction guards |

Component exceptions and cooperative async timeouts fail open to the last good
projection. Descriptor fields such as `network_access` and `timeout_ms` are
policy metadata, not an in-process security sandbox: registered components must
be trusted and event-loop cooperative; untrusted or remote reducers belong in a
restricted sidecar. The runtime snapshot is immutable; a changed rollout is
constructed as a new runtime rather than mutating live sessions. Optional
`initialize`, synchronous `health`, and `close` hooks initialize in registry
order and close in reverse.
`SessionManager.start()`
initializes them without handing over the manager or its credentials, and
`SessionManager.stop()` closes them only after session/background consumers.

An enforced recoverable observation reducer can retain a sufficiently large
**already masked** result in a per-session in-memory store. References are
opaque, object-scoped, size-bounded and TTL-bound; no artifact bytes are written
under the model-visible workspace. The model gets paged
`read_token_artifact(raw_ref, offset, limit)` only when its catalogue explicitly
contains the `observation.recover` capability. The action journal keeps the
bounded masked-authority prefix with an explicit truncation marker, rather than
an ephemeral recovery reference.

The built-in shell now returns a `CommandResult` to the harness with separate
`stdout`, `stderr`, `exit_code`, `timed_out`, `overflowed`, duration and harness
error fields. Its compatibility string rendering is stdout followed by stderr;
it does not claim to reconstruct cross-stream interleaving. No RTK binary is
invoked and no shell command is transparently rewritten; adapters can classify
this structured result later without moving execution behind the permission
decision.

### Inject a custom reducer and request optimizer

Start custom components in `shadow`, inspect receipts and task-quality checks,
then construct a new `enforce` runtime only for a validated rollout:

```python
from mini_loop import (
    ComponentDescriptor,
    ComponentStage,
    Lossiness,
    OptimizationMode,
    SessionManager,
    TokenEfficiencyRegistry,
)
from mini_loop.token_efficiency import ObservationReduction, RequestOptimization


class DomainLogReducer:
    descriptor = ComponentDescriptor(
        id="domain-heartbeat-fold",
        version="1",
        stage=ComponentStage.OBSERVATION,
        content_types=("text/x-command-output",),
        deterministic=True,
        lossiness=Lossiness.LOSSY,
        network_access=False,
        timeout_ms=100,
    )

    async def reduce(self, observation, *, query=None, budget_tokens=None):
        lines = observation.content.splitlines()
        kept = [line for line in lines if line != "HEARTBEAT OK"]
        removed = len(lines) - len(kept)
        warnings = (f"removed_known_heartbeat:{removed}",) if removed else ()
        return ObservationReduction("\n".join(kept), warnings)


class LatestDeltaOptimizer:
    descriptor = ComponentDescriptor(
        id="latest-delta-blank-fold",
        version="1",
        stage=ComponentStage.REQUEST_CONTEXT,
        deterministic=True,
        lossiness=Lossiness.LOSSY,
        network_access=False,
        timeout_ms=100,
    )

    async def optimize(self, context, *, budget_tokens=None):
        request = dict(context.request)
        messages = [dict(message) for message in request.get("messages", [])]
        if messages and isinstance(messages[-1].get("content"), str):
            messages[-1]["content"] = "\n".join(
                line for line in messages[-1]["content"].splitlines()
                if line.strip()
            )
        request["messages"] = messages
        return RequestOptimization(request)


components = TokenEfficiencyRegistry()
components.register_observation(DomainLogReducer())
components.register_request_optimizer(LatestDeltaOptimizer())
runtime = components.runtime(default_mode=OptimizationMode.SHADOW)

manager = SessionManager(settings, client, token_efficiency=runtime)
```

The request optimizer above can touch only the newest delta: the runtime rejects
a changed frozen prefix, and `Agent._create` separately rejects changes to role
count or `tool_use`/`tool_result` identities. Optimizers operate before
`CachePolicy.annotate`, and neither their mutation nor provider-specific cache
keys enter `agent.messages`.

### Built-in configuration

`SessionManager` performs no package/entry-point discovery. Settings select
only reviewed built-ins; an injected `token_efficiency=` runtime takes
precedence.

| Environment variable | Default | Meaning |
|---|---:|---|
| `MINILOOP_TOKEN_EFFICIENCY_MODE` | `off` | `off`, `shadow`, or `enforce` for built-in components |
| `MINILOOP_TOKEN_EFFICIENCY_RESPONSE_STYLE` | `normal` | `concise` registers the Caveman-inspired local response policy |
| `MINILOOP_TOKEN_EFFICIENCY_PERSIST_RAW` | `true` | retain eligible already-masked raw observations in enforce mode |
| `MINILOOP_TOKEN_EFFICIENCY_RAW_MIN_BYTES` | `16384` | minimum masked observation size eligible for in-memory recovery |
| `MINILOOP_TOKEN_EFFICIENCY_ARTIFACT_TTL_SECONDS` | `3600` | in-memory artifact lifetime |
| `MINILOOP_TOKEN_EFFICIENCY_MAX_ARTIFACT_BYTES` | `2000000` | per-artifact limit |
| `MINILOOP_TOKEN_EFFICIENCY_MAX_TOTAL_BYTES` | `20000000` | per-session store limit |

With a non-`off` mode, the manager registers the local
`DeterministicLosslessReducer`; despite its historical class name, its
descriptor is `recoverable` because it folds display noise. `enforce` therefore
applies it only when the scoped original can be recovered.
`RESPONSE_STYLE=concise` additionally registers
`ConciseResponsePolicy`; `shadow` measures it and `enforce` applies it. This is
a small local policy inspired by Caveman, not the Caveman project packaged as a
runtime dependency.

The optional code-context provider is independent and off by default:

| Environment variable | Default | Meaning |
|---|---:|---|
| `MINILOOP_AST_OUTLINE_ENABLED` | `false` | install four typed semantic-read tools |
| `MINILOOP_AST_OUTLINE_BINARY` | `ast-outline` | absolute operator-pinned path when enabled |
| `MINILOOP_AST_OUTLINE_SHA256` | unset | required 64-hex executable digest when enabled |
| `MINILOOP_AST_OUTLINE_TIMEOUT` | `10` | per-process wall-clock bound in seconds |
| `MINILOOP_AST_OUTLINE_MAX_OUTPUT_BYTES` | `1000000` | independent stdout/stderr capture cap |

The `Settings`/`SessionManager` built-in path probes and accepts
`>=1.9.0,<1.10.0`, rechecks the pinned SHA-256 before every execution, and calls
the executable with direct argv
(`shell=False`), refuses a binary inside the model-visible workspace, confines
paths to the session workspace, and gives every invocation a bounded,
root-anchored/no-follow private source snapshot. Recursive snapshots omit
symlinks, special files and VCS metadata, and apply ast-outline 1.9's supported
source-name set plus bounded root/nested `.gitignore`/`.ignore` frames before
copying. Ignore files also have byte, line, pattern, match-operation and
wall-clock guards; patterns with more than two variable-star groups fail closed
before `pathspec` can compile a backtracking regex. `show_symbol` globs are resolved in the harness rather than
re-expanded by the child. It
returns typed statuses such as `applied`, `no_match`, `partial`, `missing`, and
`incompatible`. The installed tools are `repo_map`, `file_outline`,
`show_symbol`, and `symbol_references`; their semantic capabilities flow to
Explore/Worker children through `RoleToolPolicy`.

Direct construction of `AstOutlineAdapter(AstContextConfig(...))` is a trusted
embedding seam and can intentionally omit the digest or resolve a PATH name;
the embedding caller then owns binary verification. Do not treat that lower-
level API as carrying the manager's supply-chain guarantee.

Headroom is not bundled as an adapter or proxy. A future integration must run
offline with its upload beacon disabled, preserve the frozen prefix, and begin
in `shadow`; merely importing a proxy into the provider path is outside this
contract.

---

## 5. Skills and user-scoped knowledge

Drop a deployment-managed `skills/<name>/SKILL.md` with frontmatter; it is
indexed by description and injected only when the model calls `load_skill`.
This is the `agent` source shared by the manager.

```markdown
---
name: refunds
description: Company refund policy and the steps to issue one.
---
# Refunds
...full instructions the model loads on demand...
```

Point the loader at your directory (`MINILOOP_SKILLS_DIR` or
`SkillLoader(path)`), or subclass `SkillLoader` to source skills from a DB/CMS.
Manager-wide use only calls `descriptions()` and `load(name)`. Layering a user
source additionally requires the construction snapshot exposed by
`SkillLoader.skills` and its `problems` log, because collision detection cannot
be inferred safely from rendered description text.

For per-principal user skills and memory, configure one owner root:

```sh
MINILOOP_USER_RESOURCES_ROOT=/srv/mini-loop/users
```

The authenticated owner is bound before Agent construction and mapped to a
digest-only directory; raw principal IDs never become path components:

```text
/srv/mini-loop/users/u-<owner-digest>/
  skills/<name>/SKILL.md
  memory/*.md
```

An owner's first resolution snapshots that skill directory for the lifetime
of the resolver. Restart the manager or inject a replacement resolver after a
deployment changes those files; live sessions are never silently rebound.

The effective catalogue names both sources. `load_skill` accepts
`scope="agent"|"user"`, and `agent:<name>` / `user:<name>` are equivalent
qualified names. An unqualified name still works when it is unique; a name in
both sources is an error rather than a user skill shadowing Agent policy.

User memory is the only writable memory layer in this version. When the
existing memory tools/lifecycle feature is enabled, `remember`, `recall`,
automatic selection, extraction, and consolidation all use the bound owner
store; configuring the root does not itself add tools. `readonly` may recall
but skips automatic extraction. Without the owner root, user skills are absent
and the existing shared `MemoryStore` remains as a compatibility fallback with
owner-filtered, collision-safe keys.

This is application-level scoping, not host tenancy. A user skill is still
instruction-like model input and a Markdown memory is still a host file. They
cannot grant tools or bypass hooks, but an unconfined shell can read host paths;
use a real sandbox/container for an untrusted multi-user deployment. The full
contract and inheritance matrix are in
[`docs/USER_SCOPED_SKILLS_MEMORY_DESIGN.md`](docs/USER_SCOPED_SKILLS_MEMORY_DESIGN.md).

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

Each session is an event bus. Built-in event types: `status`, `model_start`,
`model_end`, `assistant_text`, `tool_use`, `tool_result`, `trajectory_start`,
`trajectory_end`, `subagent_start`, `subagent_end`, `todo`, `compact`, `done`,
`error` — plus any you emit from tools/hooks via `ctx.emit_event(...)`.
Every event carries `session`, `agent`, `depth`, `seq`, `ts`.

```python
def sink(event):                 # sync or async; called for every event
    statsd.incr(f"agent.event.{event['type']}")
    audit_log.write(event)

SessionManager(settings, client, event_sink=sink)
```

In-process consumers can also `session.subscribe()` (used by the SSE
endpoints).

Runs are also persisted by the injected `TrajectoryStore`. Pass a custom store
to replace the fleet-wide recorder, or set `MINILOOP_TRAJECTORIES=0` to disable
the built-in local JSONL implementation:

```python
from mini_loop import SessionManager, TrajectoryStore

trajectories = TrajectoryStore("./audit", capture_content=False)
manager = SessionManager(settings, client, trajectory_store=trajectories)
```

The store is intentionally independent from `event_sink`: exporter failures do
not stop an agent run, and custom sinks continue to receive live events. See
[Agent trajectories](docs/TRAJECTORIES.md) for the schema and API.

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

## 10. Built-in feature modules (s09–s20)

Beyond the core loop, mini-loop ships optional modules covering the rest of the
learn-claude-code curriculum. Turn them all on at once:

```python
SessionManager(settings, client, enable_features=True)   # or env MINILOOP_FEATURES=all
```

`enable_features` swaps the per-session registry for `full_registry()` and adds
the background/team injectors. Or compose exactly what you want:

```python
from mini_loop import full_registry, default_injectors
reg = full_registry(tasks=True, background=True, memory=True, cron=True, teams=True,
                    worktrees=True, mcp=True, mcp_servers={"docs": my_mcp_client})
SessionManager(settings, client, tool_registry=reg, injectors=default_injectors())
```

Each module is also usable à la carte via its `install_*(registry)` helper.

| Chapter | Module | Enable | Adds |
|---|---|---|---|
| **s09** Memory | `memory.py` | `install_memory(reg)` | shared Markdown/index store; per-turn selection; stop-time extraction and consolidation; explicit `remember / recall` |
| **s10** System Prompt | `prompts.py` | on by default | per-call assembly from live tools, skills, todos, memory index, workspace, and team identity |
| **s11** Error Recovery | `recovery.py` | on by default | 429/529 backoff, sticky fallback, 8k→64k escalation, bounded continuation, reactive compaction |
| **s12** Task System | `tasks.py` | `install_tasks(reg)` | atomically claimed file task graph, strict state transitions, `blockedBy`, optional worktree binding |
| **s13** Background Tasks | `background.py` | registry + injector | explicit/automatic slow commands and `<task_notification>` injection |
| **s14** Cron | `cron.py` | manager `enable_features` | strict five-field matching, durable definitions, stable session restoration and wake-up |
| **s15** Teams | `teams.py` | manager `enable_features` | concurrent sub-sessions, persisted JSONL mailboxes, automatic result delivery |
| **s16** Protocols | `teams.py` | manager `enable_features` | request-correlated shutdown handshake and plan approval state machine |
| **s17** Autonomy | `manager.py` | manager `enable_features` | WORK → IDLE → SHUTDOWN, inbox-first polling, atomic runnable-task claiming |
| **s18** Worktrees | `worktrees.py` | `MINILOOP_REPO_ROOT` / `repo_root` | task binding, automatic teammate cwd switch, safe keep/remove and JSONL audit |
| **s19** MCP | `mcp.py` | `full_registry(mcp_servers=...)` or `install_mcp(reg, servers)` | `connect_mcp` discovers a server's tools and registers them as `mcp__<server>__<tool>`; transports: `InProcessMCP`, `StdioMCP` |
| **s20** Comprehensive | `builtins.py` + `manager.py` | `enable_features=True` | assembles the full registry, lifecycle injectors, shared services, recovery, and one content-driven agent loop |

Notes:
* **Teams are async-native.** Teammates are sub-sessions rather than OS threads,
  but retain idle polling, protocol routing, the shared task board, automatic
  claiming, result delivery, and shutdown lifecycle. Teammates cannot spawn
  teammates (fork-bomb guard).
* **Custom tools** can stash per-session services on `ctx.state` and emit custom
  events with `ctx.emit_event(...)` — that's exactly how these modules are
  built. Read any of them as a template.

---

## Concurrency & safety

* **Per session (isolated):** workspace, conversation history, `TodoManager`,
  `ctx.state`, the cloned `ToolRegistry`, the run `Lock`.
* **Shared across the fleet:** the LLM client, the `LLM semaphore` (caps
  simultaneous requests — `MINILOOP_MAX_CONCURRENT_LLM`), the parallel-tool
  semaphore (`MINILOOP_MAX_CONCURRENT_TOOLS`), the Agent `SkillLoader`
  (read-only), JSONL team mailboxes, and your `Hooks` /
  `event_sink`. Keep custom shared objects stateless or concurrency-safe.
* **Shared only within one owner:** the resolved user skill snapshot,
  `MemoryStore`, and its lifecycle lock when `UserResourceResolver` is active.
* A session's runs are serialized by its `Lock` (one conversation = one
  history); different sessions run truly in parallel on the event loop. Make
  custom tools **non-blocking** — `await` real I/O, or wrap blocking calls in
  `asyncio.to_thread` (the built-in file/bash tools already do).
* Within one session run, `parallel_safe` tool handlers and their before/after
  hooks may overlap. Non-parallel-safe tools remain ordered barriers, and tool
  results are always appended in model-call order.
