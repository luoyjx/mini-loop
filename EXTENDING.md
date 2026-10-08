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
[`examples/custom_agent.py`](./python/examples/custom_agent.py).

---

## The extension map

| Module | Seam | Inject via | Replace to change… |
|---|---|---|---|
| `registry.py` / `builtins.py` | `ToolRegistry` | `tools=` / `tool_registry=` | what the agent can *do* |
| `decisions.py` | `DecisionProvider` | `install_decisions(registry, provider=...)` | the backend for typed choice, score, and noul judgments |
| `registry.py` | immutable `ToolCatalogSnapshot` | automatic per request | the exact schema/prompt prefix and its fingerprint |
| `tool_policy.py` | `RoleToolPolicy` | `role_tool_policy=` | which declared capabilities Explore/Worker children inherit |
| `registry.py` | `Hooks` (`Hook`) | `hooks=` | permissions, audit, arg/output rewriting |
| `prompts.py` | `system_builder(agent)->str` | `system_builder=` / `system=` | the system prompt |
| `compaction.py` | `Compactor` | `compactor=` | how context is trimmed/summarized |
| `caching.py` | `CachePolicy` | `cache_policy=` | where prompt-cache breakpoints go |
| `storage.py` | `StateStore` | `state_store=` | whether a session survives a restart |
| `secrets.py` | `SecretRegistry` | `secrets=` | which credentials a tool can see or print |
| `spill.py` | `SpillStore` | `spill=` / `Harness.spill` | where already-masked oversized string-Bash output is preserved |
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

`app.state.auth` is resolved once at request admission, so rotating a token
takes effect on the next request without letting one in-flight request change
identity between its handler, ownership check, and capture stamp. `NullAuth`
(the default) makes every caller one anonymous principal, which is why
`refuse_open_bind` turns a non-loopback bind without tokens into a startup
failure rather than a warning.

Shape from the OpenHands agent server: authentication as a dependency on the
router rather than a check repeated per handler, config read at request time,
and any credential channel opened for one surface staying scoped to it. Two
deliberate differences — constant-time comparison, and refusing to serve rather
than defaulting open.

The Go handler accepts `httpapi.Authenticator` with named `Principal` results.
Its `Authenticate` and `Configured` methods must be safe for concurrent requests.
Authentication runs once at admission; handlers reuse that principal for ownership
and the untrusted HTTP stamp. `TokenAuth` supports detached owner bindings;
`AuthFromEnvironment` resolves only token configuration. The embedding application
must invoke `RefuseOpenBind` before listening. The standalone Go launcher owns
that check, additionally checks the actual listener, and refuses blank hosts.

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

## 1a. Typed judgments — `DecisionProvider`

`decision` evaluates an explicit `state` against a batch of named questions.
It returns typed `choice`, `score`, and `noul` answers without executing an
action. `DecisionRequest`, `DecisionResult`, and `DecisionProvider` are public
composition types. The question IDs connect results to inputs; they are not
instructions. Keep the actual question in `instructions` and its alternatives
or ordered levels in `criteria`.

The tool is off by default, even in `full_registry()` and
`MINILOOP_FEATURES=all`. Select a backend explicitly:

```python
import os
from mini_loop import default_registry, install_decisions, JevDecisionProvider

def configured_decision_registry():
    registry = default_registry()
    install_decisions(
        registry,
        provider=JevDecisionProvider(
            api_key=os.environ["TYPESAFE_API_KEY"],
            model="jev-latest",
        ),
    )
    return registry

# registry = configured_decision_registry()  # requires TYPESAFE_API_KEY
# SessionManager(settings, client, tool_registry=registry)
```

`install_decisions(registry)` uses the current LLM explicitly; so does
`full_registry(decisions=True)`. To select Jev through the comprehensive
registry, pass both `decisions=True` and `decision_provider=provider`.
For the default server, use `MINILOOP_DECISIONS=llm` or
`MINILOOP_DECISIONS=jev`; Jev requires `TYPESAFE_API_KEY` and accepts
`MINILOOP_DECISION_MODEL` (default `jev-latest`). An explicitly installed
`decision` tool takes precedence over environment-backed manager composition.

Both backends carry `risk="external"`: the existing interactive approval path
applies, read-only sessions refuse the tool, and auto mode records its normal
permission decision. A returned classification never authorizes a write,
shell command, workflow, or approval. Subsequent actions still cross their
own gates.

Jev responses retain the actual served model and provider statistics. The
LLM backend marks `probability_source="llm_estimate"`; its confidence uses a
local normalized-entropy calculation, not Jev's undisclosed computation or a
calibrated correctness probability. A failed Jev call does not silently switch
to the LLM. The existing tool-result trace contains the typed result;
`decision_completed` adds bounded metadata without a raw state copy. There
is no new HTTP endpoint or decision store.

See [Typed decisions](docs/DECISIONS.md) for a complete batch example,
source-pinned upstream semantics, and validation boundaries.

The Go `decisions.Provider` seam evaluates a detached `decisions.Request` and
returns a validated `decisions.Result`. Question/Answer expose closed choice,
score and noul variants; state/criteria use the six supported JSON Value kinds.
`NewJev(DefaultJevConfig(apiKey))` selects the fixed source endpoint without
performing I/O during construction. An optional HTTP client is borrowed through
a private shallow copy that disables redirects; its transport must support
concurrent evaluations. Owned transports ignore environment proxies and close
idle connections after each call.

Native `RuntimeConfig` and `ManagerServices` explicitly select `DecisionTools`;
`DecisionProvider` chooses the backend, while nil selects an isolated current-LLM
query. Provider injection alone never activates a tool. `DecisionLLMConfig` zero
values select the source defaults. All calls cross the common external-risk gate,
mask/revalidate explicit state, and share the model limiter. Providers must honor
contexts; arbitrary backend error text is omitted. The LLM child shares recovery
and explicit peer provenance, with fresh history/tools/cache/meter and no stream.
Typed decision metadata reaches existing sinks; full masked requests are private
trajectory fields. Native tool results mask closed structures before JSON escaping,
including backend model/provenance strings; typed provider results remain intact. Selected custom roles may inherit the tool; default child roles
omit it. Results and replay confer no approval or execution authority. The native
launcher now accepts MINILOOP_DECISIONS=llm or jev; Jev uses TYPESAFE_API_KEY and
MINILOOP_DECISION_MODEL. Launcher Options can explicitly install/override the
backend and tune LLM settings; provider injection alone leaves off-mode disabled.
Full source replay/SQL audit remains pending. See [Go decisions](go/README.md#typed-decision-operator-library).

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

The default chain contains `GoalContinuation` (inert without an armed goal) and
`PermissionHook`: an immutable command deny-list,
ordered `PermissionRule`s, and an optional async approval callback. With no UI
callback, an `ask` decision fails closed. Passing an explicit `Hooks(...)`
chain replaces the default, so include `PermissionHook` when a custom fleet
still needs the standard policy and `GoalContinuation` when it needs goal
continuation.

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
and the database stores the configured secret projection of transcripts; the
Null-secrets path stores them as supplied.

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

`DurableActionJournal` stores this in the same SQLite database. The Python
manager explicitly calls `mark_inflight_unknown()` when composing that journal;
opening a store or constructing a journal alone does not mark actions.
Recovery marks still-`started` actions `unknown` — never `failed`, which would
invite exactly the retry the rule forbids. A replayed tool result carries
`replayed: True` on its event so an operator can see a resumed turn reused a
recorded outcome.

This shape comes from durable-execution engines (Temporal, Restate, Azure
Durable Task) rather than from any agent harness: harnesses commonly journal
actions for audit and re-run them regardless.

An optional tool verifier can reconcile an unknown action: proven landing
returns a recorded marker, proven non-landing permits execution, and an
unavailable or inconclusive verifier preserves unknown. This is not a
transaction with an external side effect.

**Agent-side state.** The transcript mentions the plan but does not rebuild it,
so the store also carries the TodoWrite board; without it a restored session has
an empty board while its own transcript shows otherwise, silently disabling the
s05 nag and the runtime-state reminder. The session row is refreshed on every
flush rather than only at creation — otherwise `run_count` and `status` stay
frozen at their initial values for the life of the session.

**Lease boundary.** A conditional SQLite UPDATE claims an existing session row;
renewal cannot reclaim a lease that expired or was stolen. A confirmed lease
lost during persistence stops the turn. This is separate from a transaction
with external tool effects, and per-event renewal is not a periodic heartbeat.
A process killed mid-run is restored with its recorded status (`running`) and
no run attached; restoring state does not automatically resume that work.

**Go contracts.** `agent.SessionRecord` carries the v7 fields with separate
tenant and process-lease identities. Small consumer-owned session, transcript,
event, lease and approval-read interfaces complement the existing action and
approval-write seams. `agent.DecodeStoredEvent` is a bounded archival decoder
for current known event variants. It preserves informational message lineage
while stamping it untrusted, with no actor or approved capabilities. Grant
events are data and do not restore broker grants. Unknown event/compaction
variants fail; additive unused fields are ignored. These explicit library
contracts are now composed by `RuntimeConfig.StateStore` and
`ManagerServices.StateStore` for live managed sessions. The caller owns backend
close. A manager generates a separate process lease identity; bare managed
sessions may explicitly omit leases. Default journal/approval services use the
store; caller-supplied services are preserved. The default stored journal is
explicitly marked unknown during manager construction, matching Python policy;
that global operation does not prove another process is dead.

The guard flushes before provider admission, checks the current epoch count and
uses immutable message content for prefix identity. Event capture writes first,
then flushes; event epochs are stamped before a rewrite flush advances the epoch.
Ephemeral events consume live sequence numbers without adding stored ordinals.
Go names these separately as EventSequence and EventOrdinal. Embedding backends
must allow context-owned reads concurrently with live writes.
ManagedSession.CatchUpEvents(ctx, sequence) selects up to 2,000 recent physical
rows, validates their scope/order/closed variants and returns detached historical
records without live authority. Subscribe before calling, then de-duplicate
queued records against the greatest sequence delivered. Fresh cursor zero skips
the backend. The owned HTTP events route composes this handoff; read faults
return an opaque 503 before SSE headers and leave writer status unchanged.
This bounded window/queue does not guarantee complete historical delivery or
cross-process live tailing, and an injected implementation is not SQL proof.
`ManagedSession.ReadTranscript(ctx, TranscriptSelection)` reads the stored
current or selected epoch into a detached TranscriptSnapshot. Exact epoch
constructors accept a concrete TranscriptEpoch or owned big.Int, preserving
out-of-range request identity. Empty storage reports epoch zero; historical
reads do not flush live messages, acquire leases, repair crash tails or install
authority. Unanswered tool calls are valid archival data. Embedding callers
own authorization; the HTTP route authenticates, validates epoch, admits the
owner and then reads. Concurrent read/write safety belongs to the backend.
Ordinary backend write faults/panics are reported and degrade; count-query or
coverage failures stop the request. Confirmed renewal loss cancels the active
turn and suppresses later publication/model/tool admission. Renewal and session
metadata refresh happen on transcript growth, not a timer or every terminal
event; stored status can therefore remain `running` after an idle completion.
Queued steering gets its own metadata write before acknowledgment.

Go claims after acquiring turn admission, closing the source queued-claim gap.
Deletion disables future writes before removing rows and cancelling parked work,
preventing late resurrection; backend cleanup faults are reported and remaining
cleanup continues. Stop releases leases only after turns drain. Snapshot 45 runs
actual Python AgentSession/SQLite guard, capture, masking, epoch and lease probes;
Go compares that recipe and additionally tests its admission/teardown rules.
`SessionManager.RestoreSessions(ctx)` now restores recorded owner, binding,
explicit system, run/status, immutable transcript references, Todo and steering.
Missing saved workspaces are recreated without the new-session factory. The
handle starts interactive and idle even with recorded `running` status; restored
facts install no human grants, activation or trusted actor. A conditional claim
precedes approval expiry and repair. A foreign-held row is exposed with
`PersistenceStatus().RestorePending`; steering acknowledgment and turn admission
fail until a claim succeeds and the latest rows are reloaded. Read/repair faults
prevent publication, release owned leases and preserve history; earlier fleet
handles can remain published when a later row fails. Stop joins the restoration
operation and prevents late publication. There is no fleet transaction claim.

Snapshot 46 uses actual Python manager/SQLite restoration. It records source
metadata loss when repair flushes before Todo/steering installation, and source
sequence reset to physical ordinal. Go installs metadata before repair and starts
its next sequence above both ordinal and stored payload sequence. It also waits
for a lease before repair. These are documented Go additions. Crash-tail calls
get unknown results, while expired parked approvals get not-run results. Tests
send the next model request, exercise retry/reload and join shutdown races.
The Go backing is test-only. Native SQLite/reopen and goal variant folding
and backend launcher activation remain pending; a lease
is neither an external-effect transaction nor fencing.

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

The Python default loads `python/skills/<name>/SKILL.md` with frontmatter; it is
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

Point the loader at another deployment directory (`MINILOOP_SKILLS_DIR` or
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

An owner's resolved skill catalogue is an immutable Agent-lifetime snapshot.
External deployment edits still require a manager restart or replacement
resolver. The personal-skill publication API described below is the supported
in-process refresh seam: it validates a complete new catalogue and atomically
swaps only the resolver's snapshot for future sessions. Live sessions are
never silently rebound.

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

An authenticated user can deliberately capture a reusable procedure from the
current live session through two separate REST calls:

1. `POST /sessions/{id}/personal-skills/preview` accepts a lower-kebab `name`
   and optional `focus`. Its authoritative source is a provenance ledger of the
   raw authenticated HTTP user input and the final answer returned for each
   successfully completed turn. Admission happens only after terminal
   trajectory/event persistence succeeds. It therefore excludes tool traffic,
   injected runtime/skill/memory text, background work, internal continuations, and
   restored history by construction. The ledger is capped at 64 messages and
   40,000 serialized characters, masked before storage, and re-masked before
   synthesis. A preview draft is process-local, owner/session-bound, expires
   after 15 minutes, and writes no skill file. Draft storage is capped at 64
   globally, 16 per owner, and 4 per session; a full global store never evicts
   another owner's reviewed draft.
   The synthesis call sets an immutable-message boundary: request optimizers,
   cache annotation, and recovery retries cannot replace the admitted ledger
   payload before provider dispatch.
2. After inspecting the returned description, body, coverage receipt, and
   digest, the user calls
   `POST /sessions/{id}/personal-skills/{draft_id}/commit` with only that
   digest. The authenticated route publishes the exact server-held draft into
   the already-bound owner root. It never accepts an owner, path, or Markdown
   body. The canonical-document `digest` is identical in preview and commit
   receipts; `content_digest` separately identifies the loaded body.

Publication is create-only. Repeating identical canonical content is
idempotent; different content under an existing user name conflicts. A name
shared with an Agent skill is allowed but must be loaded as `user:<name>`.
Commit is refused in `readonly`, and successful receipts say
`activation=next_session`: the authoring session and every other live session
keep their original catalogue, while subsequently created sessions see the new
skill. A teammate spawned from an older live parent explicitly inherits that
parent's pinned snapshot; it does not silently refresh. Editing, replacement,
rollback, cross-process cache refresh, and a model-callable publish tool are
intentionally outside this version.

Publication builds and validates the complete future-session snapshot before
the no-replace hard link. That link is the irreversible commit point: no later
validation or rollback may turn another resolver's already-confirmed
idempotent success into a missing file. Short or unavailable registered secret
values make preview and publication fail closed rather than enter a durable
instruction file.
Canonical publication normalizes frontmatter whitespace and newlines exactly as
`SkillLoader` will read them, then orders the in-memory entries by source path;
the future-session snapshot, an idempotent retry, and a restart therefore share
the same descriptions, bodies, digests, and catalogue order.

This is application-level scoping, not host tenancy. A user skill is still
instruction-like model input and a Markdown memory is still a host file. They
cannot grant tools or bypass hooks, but an unconfined shell can read host paths;
use a real sandbox/container for an untrusted multi-user deployment. The full
contract and inheritance matrix are in
[`docs/USER_SCOPED_SKILLS_MEMORY_DESIGN.md`](docs/USER_SCOPED_SKILLS_MEMORY_DESIGN.md).

---

### Go layered skill composition

`skills.NewLayeredCatalog(agent, user)` takes two concrete construction snapshots
and provides Descriptions/Load with source-qualified names, collision refusal
and one total prompt budget. The source catalogue owns serve-time digest checks
and diagnostics. Layered construction copies existing source problems once;
later source failures stay in the source log. Return values and bounded problem
reports retain the native catalogue conventions. Callers must bind the user
catalogue from trusted owner resources; the model cannot supply an owner.
The owner Resolver composes full bundles; explicit managed runtime layering is
selected through ManagerServices.UserResources.

### Go owner memory storage

`memory.NewStore(ctx, root, masker)` is an explicit typed file-store seam. Bind
one trusted owner with `memory.Bind`; ScopedStore exposes only owner-sensitive
methods without an override parameter or automatic delegation. Optional Masker
has one typed MaskText operation and can use the existing secrets.Registry. Raw
Store all-owner views/replacement remain operator operations. The owner Resolver supplies resource bundles;
managed runtimes bind that owner's store ahead of the shared fallback. Nil
ManagerServices.Memory creates WorkspaceRoot/.memory during construction, even
with a resolver selected; failed shared-root construction is fatal. Memory tools
remain explicitly selected. Do not inject the raw store into an owner-facing handler.

### Go owner resource snapshots

`userresources.NewResolver` takes a concrete deployment catalogue and optional
typed memory masker. ForOwner resolves a trusted exact owner into immutable
Resources; Skills() is a concrete LayeredCatalog and Memory() a bound ScopedStore.
Keep this snapshot for the live session and its selected descendants. Successful
cache entries never silently rebind; cancelled/failed builds are not published.
Problems(ctx) is operator-only and derives a fresh bounded view from owner-local
logs. PublishSkill replaces the cache only for future resolutions; managed
launcher/configuration activation remains pending.

### Go anchored instruction publication files

`durable.CreateText` and `ReadBytesNoFollow` take operator-selected paths and
explicit contexts. They anchor each directory component without symlink following;
the typed identity names the committed hard-link object. No fallible return follows
the link commit point. Keep canonical validation, secret screening, collision
checks and future-session bundle preparation above this seam before creating a
file. PublishSkill composes this seam. Darwin/Linux require no added
dependency; Darwin syscall constants are pinned in the platform file.

### Go pre-commit skill catalogue preparation

`Catalog.WithSourceDocument(ctx, absolutePath, document)` returns a new concrete
Catalog without reading or writing files. It parses through the same bounded
Markdown reader as NewCatalog, refuses malformed/truncated entries and existing
name/path collisions, sorts by source path and copies bounded diagnostic history
under the source lock. The original catalogue and later diagnostic mutations stay
independent. Full source digest verification remains mandatory at load time, so
a prepared skill cannot serve until its file exists with the expected content.

This operator seam prepares the future snapshot before the hard-link commit. It
does not establish owner authority, validate the stricter canonical publication
fields, screen secrets, commit a file or replace a resolver cache. The complete
create-only publisher now composes these seams while preserving live snapshots.

### Go create-only owner skill publication

`Resolver.PublishSkill(ctx, trustedOwner, SkillFields)` is an operator seam. It
screens all raw fields, requires a typed registration surface for custom maskers
and requires healthy registered values. Nil and secrets.Null preserve no-secret
configuration. A custom memory-only masker without Names cannot publish. Masking
or health panics become safe errors; no secret names, values or host faults enter
receipts. Canonical field failures preserve the source code/text; static Go fields
remove the mapping-shaped invalid_fields case from this library API.

Publication prepares the new source catalogue, layered bundle and receipt before
the no-replace hard link. Same canonical document/body/active target retries are
idempotent; changed descriptions/bodies and alternate active paths conflict. A
competing link winner is freshly scanned and verified without changing its file.
The successful-link path performs no fallible validation or cancellation check.
Only the future resolver cache changes; old Resources and their memory binding
remain pinned. Publication.Receipt and PublicationError.Receipt are safe typed
JSON projections; Publication.Resources is internal owner composition.

Native prepared entries retain source digests, so later file tampering is refused
without requiring a restart. Capture/preview, authenticated commit routes, trusted
launcher/configuration activation and cross-process cache refresh remain pending. This library
operation does not authorize model-driven publication.

### Go explicit owner memory tools

`RuntimeConfig.MemoryTools` is default-off. An explicit matching `Memory`
ScopedStore supplies the legacy shared-store binding, or UserResources supplies
the owner-local store with precedence. Construction rejects missing/foreign
bindings before workspace writes. `ManagerServices.MemoryTools` selects the pair
with an explicit UserResources resolver. The launcher still rejects resource/
memory settings until root configuration and default composition are implemented.

`remember` is a write-risk exclusive tool; `recall` is a readonly read-risk
exclusive tool. Both pass through the shared rewrite/permission/guard/masking
gate. Their closed typed inputs have no owner or root override; optional values
are detached and preserve absent/null values for replay identity. Source
string/null unknown memory types normalize to project, and empty/null
descriptions fall back to the name. Malformed non-string fields are refused
by the typed Go boundary. Store masking must be configured separately for its
durable sink; gate masking protects recorded inputs and returned observations.

ScopedStore.WithLifecycle serializes callbacks across all bindings of the same
Store, matching the source lifecycle_lock. Explicit remember holds it through
Write; recall remains outside it. Waiting observes cancellation and callback
errors/panics release the lock. Callbacks can perform ordinary scoped operations
but cannot recursively acquire the lifecycle. The lock is process-local, has no
rollback, and is independent across Store instances. Automatic end-of-turn capture now holds this lock across the model extraction
and consolidation stages. The ordinary operation permit remains separate.

Recall renders the source provenance wrapper and lexical search's five-record
limit. A selected in-process child keeps the parent's scoped store and is
subject to its own role and permission mode; default role capabilities still
omit these tools. With both present, automatic selection defaults on; explicit
RuntimeConfig/ManagerServices.MemoryAuto=false disables it, with copied values.
The selection request has purpose memory_selection, budget 200, no system/tools
and the source 4,000-character query tail. It follows normal provider/recovery/
cache/limiter/event behavior and never anchors the live conversation meter.
Valid indices retain order/duplicates, including source boolean-as-integer
behavior; invalid or empty selections and provider faults fall back to lexical
search. Cancellation and native persistence authority failures propagate.
The native JSON boundary rejects nonfinite/malformed JSON and uses fallback.
Selected reference blocks follow rewritten input; a typed memory/load event is
serialized and decoded for archival reads without granting authority. The dynamic
index is sent in changed runtime facts only with recall, even when automatic
selection is disabled. The extraction stage projects supported typed message
variants, excluding recalled contexts, runtime facts and all tool-result bodies.
Its source agent_turn request uses a separate side-call entry point so purpose
alone cannot grant live history/meter ownership. It uses the source 1,500-token
budget, 40,000-character ASCII JSON tail, first/last bracket decoding, text-block
concatenation and first-five incremental writes. Missing optional fields use
source defaults; additional owner/root/origin fields are ignored and the bound
store fixes attribution. Non-string/null fields are rejected by the native typed
boundary; source can coerce some malformed scalars. Ordinary faults return zero,
including after partial writes; cancellation and native state authority failures
propagate. Capture now runs after final text, a stop-hook stuck halt, and round
exhaustion when both tools, MemoryAuto and writable posture permit it. Provider
fault and cancellation exits do not initiate capture. The source tool-batch stuck
halt lacks capture and is preserved as a measured source omission.

Consolidation starts at ten scoped records and uses a 2,500-token
memory_consolidation side request. Source lowercase ordered record fields are
serialized with non-ASCII JSON. Nonobjects/missing names are skipped; no usable
entries leaves the store untouched. Exact name/type/description/body identities
retain original origins; changed/new entries receive consolidated. Replacement
always uses the scoped owner. The source's multi-file replacement has no rollback.
List failures outside the consolidation best-effort block reach contained capture;
normal model/parse/write faults return zero. memory/extract carries count and an
explicit consolidated count; memory/load omits it. Accessors/event clones detach
pointers, flat serializers and archival readers use known event variants, and
memory_capture_error carries bounded detail through normal registry masking.
Native error class labels differ from Python class names. Ordinary capture faults
preserve the completed turn; cancellation and native state authority loss remain
errors. Managed-session skill preview/routes remain pending; launcher root configuration
and individual memory selection are implemented as described below.

### Go owner-bound draft storage

Construct userresources.DraftStore with DefaultDraftStoreConfig or an explicit
positive TTL and bounded limits. Defaults are 15 minutes, 64 total, 16 per owner
and four per session. MaxPerOwner nil selects min(16, MaxItems). Clock is an
optional trusted concurrency-safe test clock. Configuration is copied; Go uses
time.Duration/integers and stable typed errors rather than Python ValueError.

Add validates canonical fields and binds separate OwnerID/DraftSessionID values.
The returned Draft is an immutable private identity handle; Preview returns a
detached DraftPreview with no owner/session fields. Get/Peek bind owner, session
and optional constant-time digest comparison; wrong authority is 404 before TTL
or digest evaluation. Successful Consume is atomic and one-shot. Capacity
replacement is FIFO within the requester; foreign reviewed drafts cannot be
removed to make room. A cryptographic UUID4 hex identifier is generated; initial
entropy failure returns a stable 500 before quota mutation.

A future publication flow must Peek, publish the exact reviewed fields, then
DiscardCommitted with that exact handle. Cleanup checks pointer identity and
ignores elapsed TTL; it cannot remove a different object with the same ID.
Do not Consume before publication, because a publication fault must retain the
reviewed draft. This is a library only: admitted-turn projection, model preview,
manager binding and authenticated routes remain subsequent work.

### Go skill evidence projections

ProjectSessionText accepts typed protocol messages and implements the legacy
current-epoch fallback: strip greedy lowercase memory wrappers, unwrap whole
interjections, exclude internal markers and discard user block arrays as a unit.
Assistant blocks contribute cleaned text only; thinking/tool data are excluded.
Compaction markers set the exclusion flag even when their text is filtered out.
Python whitespace/IGNORECASE and Unicode 14 word boundaries are preserved.

ProjectAuthenticatedText takes already-admitted AuthenticatedText rows. It keeps
nonempty user/assistant text verbatim, including internal-looking strings, and
carries prior omissions/excluded-history flags. Neither function establishes
provenance. ManagedSession now enforces the trusted capture capability before
recording a successful completed pair; legacy blacklist filtering does not grant
provenance.

Both mask the two fixed display fields and their keys before budgeting, then keep
a suffix of whole messages by compact non-ASCII sorted-key Python JSON character
cost, including commas/brackets, capped at 40,000. ProjectedText holds only fixed
content/label/key values; MarshalJSON uses a temporary string map at the boundary
for source key masking/collisions. A masked ProjectionLabel is data, never a
provider role. Returned arrays are detached. Native positive-limit and UTF-8
refusals are typed errors rather than source ValueError/dynamic malformed rows.
The pure helpers have no model/file effect. CaptureLedger.Project uses the
authenticated helper after interpreting the two canonical source fields from
masked key labels; masked role keys or role values do not become authority.

### Go admitted-turn skill capture

ManagedSession owns one zero-value CaptureLedger. Successful terminal flush and
the caller-stamped personal_skill.capture_source capability precede Record.
HTTP admission supplies that capability; ordinary/peer/cron calls do not.
Record trims source Unicode whitespace, masks both display fields and keys,
checks secret names/unresolved/short reports and retains at most 64 messages and
40k compact Unicode JSON characters. Each oldest message evicted increments the
omitted count; pairs can split at the bound as in Python. Only plain string
history markers set sticky compaction exclusion. Block arrays do not.

Screening failure prevents append and latches secret_screening_unavailable;
recovered registries can append later pairs but do not clear that agent's error.
Snapshots detach rows; established distinguishes an absent source ledger from a
valid empty bounded ledger. Project refuses any capture error with source
capture_source_unavailable/503. Typed native storage cannot represent a corrupt
dynamic Python state/list or omitted field. Masking faults/invalid UTF-8 use a
safe capture_failed latch, never private host exceptions. Nothing is persisted
or restored, and optional capture cannot fail a durably completed turn. Candidate
parsing, preview business flow and standalone native model binding are described
below; manager draft injection and preview/commit routes remain.

### Go skill candidate parsing

ParseSkillCandidate accepts raw model text, the caller's requested name, projected
message count and a trusted masker. It returns an immutable SkillCandidate with
named create/skip decisions, original description/body and detached integer
evidence, or a safe enumerated CandidateError. It grants no owner/publication
authority. The five-field schema must match before recursive secret checks;
masked fixed/nested keys or strings refuse sensitive_output before subsequent
schema/decision/type validation. Description/body must be strings; evidence must
be a list of integers, excluding booleans and float/exponent lexemes. Skip requires
empty fields/evidence and does not validate the requested name. Create requires
nonempty unique in-range evidence and reuses NewCanonicalSkill validation without
normalizing its returned original fields.

Transient RawMessage maps/arrays exist only during JSON boundary validation;
they never enter domain/service state. Duplicate object keys retain the last
value, including discarding overridden sensitive values as Python does. Bare
NaN/Infinity/-Infinity lower to null only for invalid-type outcomes; quoted values
are preserved and no nonfinite native value is stored. The source default
4,300-digit integer ceiling is pinned by fixtures. Invalid UTF-8 and escaped lone
surrogates refuse malformed_json before lossy decoding; Python can represent
lone surrogate strings. Native masker panics return masking_unavailable without
private error text. Registry health checks belong to the preview caller, matching
the source parser's responsibility. Preview business flow is described below;
runtime and routes remain unbound.

### Go skill preview business flow

SkillPreviewer owns a trusted SkillPreviewModel, masker and process-local DraftStore
(default source quotas/lifetime when no store is supplied). Preview validates name,
owner and sensitive name before projection. A supplied CaptureLedger selects
authenticated evidence, even when empty; nil selects standalone legacy history.
Empty evidence refuses before a model request. Masked focus is bounded to 2,000
Unicode characters; secret health is checked before generation. Fixed system text,
2,500 output tokens and an immutable prompt are supplied to the model seam.

At most two attempts run. Candidate errors add only the safe repair reason to a
fresh recursively masked, sorted compact Unicode JSON payload; prior model output
is never quoted. Provider faults refuse provider_failure/502; cancellation passes
through. Valid candidates trigger another secret-health check before skip refusal
or bound draft retention. Failed/cancelled operations retain no draft. Native
masker/provider panics have safe 503/502 errors; explicit context deadlines also
propagate. Closed JSON Value maps are transient boundary projections, not retained
service state. Model adapters must extract text blocks and preserve source normal
model recovery/telemetry with non-live history/meter ownership, empty tools and
personal_skill_preview purpose. Managed callers must hold admission/lease and
supply a ledger; this library enforces no session/HTTP authority itself. The
manager preview binding is described below; commit and HTTP routes remain pending.
Standalone Session.PreviewPersonalSkill now serializes with the core turn gate
and binds owner/session from runtime configuration. Its native adapter calls
completeSideModel with empty tools and preview purpose, concatenates only text
blocks and preserves configured cache/recovery/shared model limiter and telemetry.
It grants recovery no live history and does not observe the live token meter.
The per-session previewer retains only process-local drafts. A manager now owns
one default DraftStore and injects it through its common runtime factory for
create/fork/ordinary and scheduled restoration. That internal typed binding keeps
64/16/4 quotas at fleet/owner/session scope; standalone callers keep their own
store. Manager restarts start fresh pools and no publication flow is installed.
SessionManager.PreviewPersonalSkill now checks owner/configuration before
admission, rechecks the same accepting handle while holding the manager/session
locks, then requires the lease and supplies the ledger even when empty. Policy
errors expose closed codes/statuses; typed ledger storage cannot be corrupted
like the source dynamic list. Lease loss before or during model work maps to
session_lease_lost/409. A separate preview lifetime does not change run status,
count or operator turn-cancel behavior. StopAccepting cancels it; deletion/stop
join it before closing services, releasing leases or reclaiming workspace.
That join extends the source turn-only cleanup. Context cancellation discards
any exact retained draft before returning failure. CommitPersonalSkill now shares the same admission/lease/lifetime seam. Readonly
refusal precedes Peek; source reviewed fields publish through Resolver.PublishSkill.
A typed receipt binds session/draft/canonical digest, user source, body digest,
optional warning, idempotency and next_session activation. Reviewed publication
errors retain code/message with source 409/422/500 status; unknown faults/panics
use publication_failed/500 without host text. Failures retain drafts. Durable
success discards only the exact Peek identity and bypasses cancellation/TTL
checks after the commit point. Existing snapshots remain immutable; future
sessions resolve the new catalogue. HTTP routes remain open.

### Go trusted session resource binding

`ManagerServices.UserResources` accepts a concrete Resolver and defaults to nil.
`ManagerServices.Memory` accepts a concrete shared Store as the fallback when no
resolver is configured. The common create/fork/restore composition binds it to
the admitted owner through `memory.Bind`; it never passes a raw store to tools.
Resolver failures refuse admission instead of falling back to shared storage.
Shared bindings retain the Store lifecycle lock, and files survive fleet stop.
Neither field alone installs memory tools; select MemoryTools explicitly.
Create, fork and recorded/scheduled restoration resolve the admitted owner before
the managed runtime is built. Fork uses the current generation, while existing
sessions retain their earlier catalogue. Delete and stop preserve durable owner
files. Failed resource admission cannot publish a handle; native scratch cleanup
still follows the manager's unpublished-allocation rules.

`RuntimeConfig.UserResources` accepts one concrete Resources value through an
optional pointer. The constructor copies it, verifies matching owner and complete
skills/memory, then uses its layered source. The session's UserResources getter
returns that fixed bundle; changing the caller's config value cannot rebind it.
SubagentParent and native in-process children inherit the parent's binding. Python
in-process subagents explicitly inherit skills; native complete bundle retention
also pins owner memory for future integrations. Source teammates inherit both,
and Go teammate scheduling inherits that fixed parent bundle. Memory tools/hooks are not activated
by carrying a bundle. The launcher always constructs shared storage at the
configured MemoryRoot or WorkspaceRoot/.memory, then optionally constructs a
UserResourcesRoot resolver with the same agent catalogue. Both roots are mandatory
when selected; root faults close the owned transport and refuse startup. Binding
validation precedes root effects. Options.MemoryTools (default false) and the
copied Options.MemoryAuto override independently select tools and automatic
lifecycle behavior. --memory-tools and --memory-auto expose these choices; pure
Inspect reports selection without root creation. User-skill capture/routes remain
pending.

Builtin Catalog/LayeredCatalog Load returns named skills.RefusalError for domain
refusals. The runtime renders those as source-compatible `Error: ...` completed
text outcomes; custom backend faults and cancellation retain error semantics.
This distinction is verified against actual Python tool events, not inferred
from an output prefix.

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

The Go `agent.Provider` consumer accepts concrete `protocol.ModelRequest` and
`ModelReply`. `go/provider.New` supplies an explicit direct Anthropic-compatible
HTTP adapter with SDK-level bounded retries, cache wire annotations and served-model
identity. It reads no environment/profile credentials and retains no dynamic
payload in the service layer. `provider.NewStreaming` adds the optional consumer-owned
`agent.StreamingProvider` seam: synchronous, context-owned callbacks carry only
`protocol.StreamDelta` text/thinking progress, followed by a validated final reply.
Implementations must stop callbacks before returning, including on failure. Session
coalescing, stream IDs, masking and managed cancellation repair own progress.
`RuntimeConfig.Recovery` and `ManagerServices.Recovery` select a typed policy.
Nil selects DefaultRecovery; DirectRecovery disables it. RecoveryInput is detached,
and RecoveryServices supplies named call/event/history/model callbacks. Policies
must honor contexts, stop callbacks before returning and be safe when shared.
DefaultRecovery releases model permits during backoff and keeps all attempt state
local. `RuntimeConfig.StreamProgress` and `ManagerServices.StreamProgress` capture
optional character/duration thresholds and a named StreamClock. Nil thresholds
select 200 Unicode characters/200 ms; explicit zero or negative values flush each
nonempty fragment. The clock runs synchronously on arrival/flush and must be safe
when shared. Children/forks retain captured settings with fresh pending buffers.
`NewFakeProvider(FakeProviderConfig)` creates one stateful fake client; use pointers,
including `&FakeProvider{}` for defaults, and do not copy a used client. Typed
FakeResponder callbacks return FakeGeneration; identity/usage/thinking remain
client-owned. Responders must honor contexts and synchronize shared state.
`fake.Streaming()` selects an explicit streaming view over that same sequence.
See `go/README.md` for configuration and boundary differences.

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

The Go port exposes `RuntimeConfig.EventSink` over closed `SessionEventRecord`
variants. Records are masked and detached before publication. The optional sink
runs synchronously in event order; faults/panics become bounded diagnostics and
never rewrite tool outcomes. Slow sinks delay the emitter. A sink may inspect
`ManagedSession.Info`, event snapshots or subscriptions, but must not recursively
emit or call the blocking `Messages` accessor while a turn owns the loop lock.
`Subscribe(replay)` returns a closeable bounded live queue; it sheds oldest at
2,000 events. Replay is only the 200-event non-ephemeral memory backlog;
`EventsAfter` does not restore a durable gap. `NewManagedSession` adds outer
status/done/cancelled events; raw child `Session` loops emit only core telemetry.
The Go HTTP handler projects the same bounded events into SSE. A trajectory/state
store and durable cursor gap recovery remain pending. Manager conversation forks
copy completed history and expose lineage/source events through this seam; their
first-turn persistence still awaits a Go session store.

---

### Go trajectory recording seam

`agent.TrajectoryWriter` owns typed Start/Append/Finish/Count operations;
`TrajectoryReader` owns List/Summary/JSON/ByteSize/Stream/DeleteForSession and
VisitRecords. The visitor receives detached encoded bytes and a typed type/limit
query; nil types includes headers/ends, an empty type slice yields none. Its limit
counts yielded records, not scanned lines; no append lock spans callbacks.
`TrajectoryStore` combines them for `ManagerServices.Trajectories`.
`RuntimeConfig.Trajectories` supplies only the writer to ManagedSession; bare core
sessions and children do not open independent outer recordings. `Build` is explicit
metadata. The launcher constructs the standard file store by default.

Records are masked before the writer receives their encoded full fields. A private
closed recording-detail variant carries model request/reply or full tool/child text
synchronously; live records retain their existing display limits. Append precedes
backlog/subscriber/sink publication. Finish records terminal status and duration
before publishing the live persistence receipt. Start/append/finish/count faults and
panics degrade to bounded masked Info/terminal diagnostics. Disabled recording keeps
the source Null-state persistence fields. Custom writers must not recursively emit
or call blocking Messages; inspection through Info and detached event snapshots is
safe. Reader ownership enforcement belongs to the HTTP/manager caller; raw library
store access is an operator capability. Default deletion retains files, and explicit
purge runs after manager drain. File recording has no session-restoration authority.
`traceview.Build` decodes wire JSON into named ledger rows and plain inspector strings;
`Render` applies one HTML escaping boundary. The HTTP view uses recorded-owner and
file-size checks before Build. The standalone traceview CLI is an operator reader,
with no synthetic HTTP principal. Original CSS and filter JS are embedded.

### Go background command service

`background.New(Config{Shell: shell.Config{Workspace: root, Secrets: registry,
Sandbox: sandbox}})` binds the operator library explicitly. Request timeout and
retention are typed; each admitted command receives its own cancellable context.
Run caller cancellation controls admission only. CancelAll returns cancelled
joinable handles; Close cancels and joins current handles, with admission
quiescence supplied by the embedding caller.
Foreground Interrupt does not reach them. Close permits later Run like source.

The foreground/background paths share argv, environment, mask and native group
control. Background merges native streams and counts raw bytes, unlike foreground
character capture/newline decoding. A Started observer must return promptly;
panic containment ends and joins the process group. Service Rebind prepares a new
executor, pins existing executions and keeps the original ledger root. Sandbox
construction must preserve that binding; no OS backend is supplied here.

Ledger writes mask the command preview and tolerate recording failure. Orphan
records report unknown outcomes and current PID liveness; they never replay work,
adopt authority or signal the PID. Atomic private files are not fsync, a lease or
host ownership. Full-result retention is 100, listing/projection counts are 50;
metadata and undrained notifications remain unbounded. Go validates ledger field
types/read sizes, refuses negative retention and cleans pre-start cancellation,
where Python can leave Running metadata.

RuntimeConfig.BackgroundTools explicitly selects two closed native-session tool
variants, conditional Bash dispatch/classification, bounded next-round completion
injection and typed background_result events. The native executor supplies the
exact admitted credentials, sandbox and capture policy through NewWithExecutor;
no shared manager is injected. State is lazy and also boots for ledger evidence.
Source-compatible shell prefix approvals and immutable denial cover both names.
Workspace entry publishes one prepared shell to foreground/background bindings.
Library callers quiesce admission before CloseBackground; cancelling a turn leaves
background work running and names live survivors in its interruption marker.
ManagerServices.BackgroundTools enables fresh per-session/fork ownership.
Manager delete/stop revoke admission, drain the active turn and then close/join
background work before reclaiming scratch. Checking ownership after the drain
covers a service lazily created during that grace window. Idle background owners
use tracked asynchronous cleanup; Stop also awaits retiring sessions.
launcher.NewWithOptions and InspectWithOptions accept Options.BackgroundTools;
the Go executable exposes --background-tools and reports its explicit selection
under --dump-config. MINILOOP_FEATURES still refuses the incomplete comprehensive
bundle. Explicitly selected child background tools bind a fresh scoped state to
the admitted native executor; default capability roles omit these tools. The
parent initializes existing root evidence before registering children and keeps
their states reachable for recursive lifetime cleanup. Qualified IDs come from
the child's peer message identity, while records stay in the admitted workspace's
ledger; root startup adoption can report them after a crash. Queues and checks
stay local to each child. Returned child tasks keep running until their own
completion/deadline or owner cleanup; CloseBackground cancels the entire owned
tree before waiting, so an expired observer can resume joining. Metadata/queues
remain unbounded and no cross-session/process ledger authority is implied.
Actual source with harness-bound inherited injectors can adopt/unlink the live
parent's record; Go intentionally avoids that gap rather than sharing queues.
Default ten tools are unchanged.

### Go operator cron service

`cron.New(Config{Resolver, DurablePath, Secrets})` explicitly binds a typed
operator scheduler. Resolver/Runner adapt session lookup/restoration and an
always-untrusted Invocation into the embedding runtime; the adapter must create
a fresh untrusted RunContext. SessionManager supplies this adapter for live
managed sessions and owns a scheduler by default. Its ScheduleCron/CronJobs/
CancelCron/ArmCron APIs require owner identity; CronScheduler exposes privileged
operator access without owner checks. RuntimeConfig.CronTools installs three
closed model variants through the common gate; RuntimeConfig.Cron binds a typed
CronControl with schedule/list/cancel only. ManagerServices.CronTools supplies the
manager binding, and --cron-tools / launcher.Options.CronTools selects it alone.
Default tools remain ten and MINILOOP_FEATURES remains unsupported.
Owned HTTP list/schedule/cancel/arm routes work independently of tool activation.
Missing cron handles now call `RestoreScheduledSession(ctx, id)` inside the
scheduler-owned context. The privileged method returns live handles unchanged,
uses the saved path for bound rows and the current workspace factory for saved
scratch or missing rows. Saved owner/history/run/status/Todo/steering are restored
before publication. Scheduled system is the current builder rather than saved
explicit text, matching actual source. Missing rows are anonymous: without a
backend they run ephemerally; an injected backend refusing its missing-row claim
produces lease loss without an unconditional upsert. Snapshot 47 executes these
seven actual Python/SQLite-or-Null recipes and the next offline request.
Manager Stop cancels restore reads through a lifetime context and joins cleanup.
The backend must cooperate with cancellation; ignored cancellation can still
hold shutdown. Native SQLite restart evidence remains pending.
Schedule arms only current-process jobs. Restored jobs stay disarmed until the
operator calls Arm/ArmAll, and no model arm tool is supplied. Start is explicit;
Stop cancels/joins ticker and admitted runs, retaining armed in-memory jobs for
later Start. Delayed lookup cannot admit a stopped generation. Callers quiesce
admission before relying on Wait or reclaiming session resources.

Atomic masked storage persists marks/one-shot removal before dispatch; O_EXCL
minute claims suppress competing stale readers. Failure reports lost occurrences
without dispatch. This does not arbitrate stale whole-file writers or transact
with external effects. The eight-MiB store bound and strict scalar decoding are
Go additions. Resolver callbacks run outside the state lock; Mask must not
reenter scheduler or manager cron methods. Manager owner mutations serialize
with deletion/stop; deletion removes future jobs and records save failures while
continuing cleanup. Stop revokes turn admission, drains sessions and then joins
cron. Standalone Serve calls manager.Start without arming restored jobs. A fresh
untrusted ManagedSession.Run retains the normal gate/pools/recording boundaries.
Forks keep activation with fresh job scope; selected children have no cron
service. Model booleans are strict against the schema, while HTTP booleans retain
source coercions. Calls retain permission, replay, masking and observer boundaries.
Loaded and live prompts differ when masking changes
the stored copy, and that change is diagnosed. See the operator library contract
in [go/README.md](go/README.md#operator-cron-scheduler).

### Go operator worktree service

`worktrees.New(worktrees.Config{Repository: repo})` explicitly selects a repository.
`Base` and `BranchPrefix` are optional string pointers: nil keeps `.worktrees`
and `wt/`, while an explicit empty value retains the Python override contract.
Names, task IDs, changes and audit events use named types. `TaskBoard` accepts
typed `Load` and `BindWorktree` operations; `*tasks.Store` implements it directly.

Create checks the task before invoking Git, then binds it and appends the audit.
These are separate effects: later binding/audit failure preserves created work.
Remove fails closed on unknown status or changed/unmerged work unless discard
was explicit. Git receives `--force`/`-D` only in that case; its ordinary checks
remain a second guard. Branch cleanup failure is ignored like Python, so a
successful clean directory removal can leave an unmerged branch for review.

`WorkspaceFor(ctx, sessionName)` ports Python sanitization/existing-directory and
plain-directory fallback. `agent.NewWorktreeWorkspaceFactory(service)` provides
an explicit concrete `ManagerConfig.WorkspaceFactory` adapter. Factory admission
marks the path as scratch; it does not install model tools. Actual Python/Go
manager delete removes even dirty scratch after draining/shared-holder checks,
without Git registration/branch cleanup or a worktree removal audit.
`PreserveWorkspace` and Stop retain it. To retain an operator checkout across
delete, provision explicitly and use `CreateSessionRequest.Workspace` with the
manager's configured bindable roots. Bound workspaces survive session cleanup.
Source create leaves allocated work after construction failure; Go's existing
unpublished-scratch cleanup removes only the directory, retaining Git metadata.
Set `RuntimeConfig.WorktreeTools` or `ManagerServices.WorktreeTools` to install
create/remove/keep/list/enter through the same common gate. Supply `Worktrees` to
bind the explicit repository; a nil service retains the source unconfigured
error. The default catalogue remains ten tools. These five tools retain source
risk/readonly metadata and empty capability sets, so default child roles exclude
them; a trusted RoleToolPolicy may explicitly select them.
The default in-process child starts without the parent worktree service/task
board, matching Python's fresh state. Selected child worktree tools therefore
report unconfigured; ordinary child files/shell use the parent's current execution
directory. Manager forks retain service activation with fresh scratch.

Entry always creates an exclusive barrier, prepares files, executor/sandbox,
catalogue and broker surfaces, then publishes execution scope. Existing catalogue
snapshots and child bindings remain pinned. Initialized task stores do not move;
first lazy admission after entry uses the new root. Managed Info, task HTTP reads,
trajectory attribution and cleanup retain the original lifecycle workspace.
Built-in `shell.Executor.WithWorkspace` preserves secrets, spill, capture/deadline
settings and foreground interrupt ownership. A custom Bash executor requires
`RuntimeConfig.WorkspaceBashFactory`; managed sessions reuse `Services.BashFactory`.
That factory must return a `WorkspaceBashExecutor` for the supplied SessionBinding, preserve
its policy and prepare without destroying the old binding. A nil, failed or
unbound or wrong-root result refuses entry before publication. Custom hooks/handlers,
approvers and system builders retain their explicit contracts; authority/context
arguments carry the new execution root. Do not retain an obsolete root in them.
Explicit fixed prompts, including the source-compatible child role prompt, remain
fixed; the default system builder consumes the current SystemContext workspace.
No full feature flag is implied. Source manager cleanup has no Git-aware
reclaimer; the guarded worktree Remove API is a separate explicit boundary.

### Go persistent task graph seam

`RuntimeConfig.TaskTools` and `ManagerServices.TaskTools` explicitly install
create_task/list_tasks/get_task/claim_task/complete_task, preserving the ten
base tools and the single bound execution gate. The task store is initialized
lazily after permission/context checks, with the runtime's credential projection.
Task owners are agent labels; HTTP principals control access to the workspace,
which is a separate ownership boundary. Manager forks retain activation and
start with a new scratch board. Custom role policies can explicitly select the
otherwise unclassified optional tools; fresh child handlers bind their own state.

`tasks.New(Config)` is also an operator library capability. It accepts a concrete
workspace and text masker; shared stores coordinate in process through bounded
striped locks and arbitrate claims across processes through exclusive markers.
It does not confine a host-edited `.tasks` symlink or provide a process lease.
Structured HTTP reads use a fresh store after session ownership admission; an
empty board may create its directory, matching Python. Readers never complete
or consume tasks. Diagnostics retain 50 distinct messages and occurrence counts.

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

For the Go slice, pass `agent.NewSessionManager` to `httpapi.New(Config)` and use the
result as an `http.Server.Handler`. The application owns listening, server shutdown
and `manager.Stop`. Alternatively, `config.Load` and `launcher.New` compose the
supported process-local services; `App.Serve(ctx, listener)` owns the listener and
joins HTTP/manager shutdown on cancellation. The application still calls `App.Stop`
when listener acquisition or a pre-serve guard fails. `cmd/miniloop` supplies this
lifecycle and signal handling. Twenty-three method/path operations, mode/steering,
process-local SSE and
embedded public console/UI shells are present. The browser keeps authenticated
data requests; no host static directory is mounted. Full health posture, optional
fleet routes and their UI panels, and durable event catch-up remain pending.
`Config.Now` and shared auth/services must synchronize their state. The
manager's optional Secrets supply a typed output projection for HTTP JSON/SSE data;
encoding faults fail closed and live model history remains unchanged. See
[the Go README](go/README.md) for the explicit composition pattern.

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

### Go plan mode seam

`agent.RuntimeConfig.PlanModeTools`, `ManagerServices.PlanModeTools` and
`launcher.Options.PlanModeTools` explicitly install both closed tool variants.
`--plan-mode-tools` uses headless approval. The ten default tools stay unchanged.
`PlanApprover.ApprovePlan(context.Context, PlanReviewRequest)` returns concrete
`PlanReview{Approved, Feedback}`. The request carries trimmed full Markdown and
bound `ToolAuthority`; the gate has already applied rewrites, guards and current
permissions. Rejection returns source error text carrying feedback with
failed=false and completed journal status. Inactive/invalid plan refusals do the
same. Callback/hook errors remain failed=true; review errors/panics and cancelled
reviews retain active state. Callback implementations must honor
cancellation, synchronize fleet-shared state and avoid reentering an active turn.
Nil approval auto-approves, matching the source installation; the existing broker
is not implicitly adapted to plan review.

`SystemContext.PlanMode` is a detached boolean. `DefaultSystemBuilder` adds the
source planning section; fixed/custom builders can omit or adapt it. This is
soft guidance with an unchanged catalog and independent permission policy.
`Session.PlanModeActive` is safe during callbacks. The typed `plan_mode` event
carries only `active`; successful entry (including no-op) and approved exit emit
it through existing persistence/trajectory/SSE sinks. Restore folds the last
logged value; no reviewer/authority/activation is deserialized. Forks start off;
explicitly selected children bind independent state and the supplied callback.
The default role policy excludes these tools, whose source capabilities are empty.
Like source, the restore fold consumes all logged plan events, including child
scopes; this guidance state does not change effect permissions.

### Go goal seam

Select `RuntimeConfig.GoalTools`, `ManagerServices.GoalTools`,
`launcher.Options.GoalTools` or `--goal-tools` to add the five named goal inputs.
All mutations cross the existing gate and use session-bound typed state;
`goal_status` is readonly. Create/resume require `ExplicitHumanRunContext` from a
trusted embedding caller. HTTP authentication alone does not stamp this authority;
model input never supplies it. A goal continuation keeps the source turn's
provenance; a child derives peer provenance and cannot arm itself.

Nil `StopHooks` selects the default stateless `GoalContinuation`; a nonnil list
replaces it, including an empty list. Put `GoalContinuation{}` in a custom list
when continuation is wanted. Shared hooks receive a detached public StopContext;
the built-in consumer uses a private live session binding. Hook order is meaningful:
the first continuation wins. The goal counter counts requested continuation before
stuck/global round-limit decisions, as source does. Read `GoalSnapshot` during
callbacks without reentering a turn. Returned blocked pointers are detached.
`goal_change` whole snapshots fold on restore, including clear tombstones and
pending lease reload; activation is always false. There is no edit/clear tool or
HTTP arming route. Source cap text mentions `goal_edit`, which is not installed.

## Concurrency & safety

### Go benchmark statistics seam

`benchmark.AggregateRuns` and `Compare` accept concrete task result rows;
`BehavioralMetrics` accepts typed protocol messages. Six measurement fields are
explicit optional Number pointers, never a free-form dimension payload. Number
has exact-integer, finite-double and source-compatible boolean variants; constructor
and BigInt access detach state. JSON integers obey the pinned 4300-digit limit,
and nonfinite arithmetic returns ErrMetric. Duplicate task rows preserve source
last-row pairing with all-row totals. Effect regressions always override wins;
dimension warnings inform an operator without changing that verdict.

`agent.SelectTools(names...)` creates a detached immutable ToolSelection for
`RuntimeConfig.ToolSelection` or `CreateSessionRequest.ToolSelection`. Its zero
value retains installed tools; calling it with no names removes every tool.
Unknown names and duplicates are ignored, retaining registry order. Reduction
happens before the gate is built, so advertised schemas and executable handlers
agree; permissions and optional feature flags retain their independent meaning.
Children inherit the reduced parent catalogue. It is a transient construction
profile, not persisted authorization: independent creates/forks/restores use their
current configuration. Trusted embeddings must supply durable policy separately.

`benchmark.DefaultTasks` and `HeldoutTasks` now return fresh admitted task lists.
`NewTask(TaskConfig)` accepts explicit trusted Judge/Setup functions; nil judges
are rejected, while empty names/prompts remain representable. Task specification
and optional whitelist storage are immutable/detached; callback authors own and
synchronize their closed-over state. Prepare/Judge propagate setup/read/decode
faults and cancellation, without turning them into passing effects. The source's
permissive substring/existence/line-count judges and exact long-log bytes remain
intentional. Model output and improvement drafts cannot become these callbacks.

`benchmark.RunArm(ctx, label, ManagerConfig, tasks)` now explicitly launches one
owned manager and fresh anonymous interactive sessions, captures workload storage
before callbacks, applies real catalogue reduction and stops/joins on every exit.
Nil/empty tasks mean no tasks; callers explicitly select DefaultTasks or HeldoutTasks.
Injected services remain caller-owned, and custom workspace factories must isolate
task paths. Setup/create failures abort; ordinary run/judge failures score rows;
cancellation aborts. Native stage errors support errors.Is/As. Duration excludes
setup/judge; transcript cost and motion are actual measurements, not fake usage.
Workspaces are retained. Runtime error diagnostic spelling remains native.

`FakeProvider.ObjectView()` adapts non-SDK fake objects to source transcript
semantics: tool caller metadata is absent. Raw FakeProvider retains client-wire
caller=null and supports explicit dictionary/SDK simulations; real providers keep
their existing absent/null contract. The launcher selects the object view. Model
task drafts remain inadmissible. `benchmark.RunFakeComparison` accepts a concrete
Settings profile and delay, always owns four new fake clients, and removes its
transient roots after all arms join. No main provider dependency is accepted.
HTTP admission captures the app deployment skills path and reads fresh supported
environment configuration per request; unsupported activations remain errors.

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

The Go runtime exposes these implemented loop seams through `agent.RuntimeConfig`:
`UserPromptHooks`, `Injectors`, `StopHooks`, `CachePolicy`, `StuckDetector`,
`EventSink`, `ModelLimiter` and `ToolLimiter`. Prompt/injector views detach history,
todos and caller authority; callbacks cannot reenter a running session. Shared
hooks and parallel handlers must synchronize state and honor context cancellation.
`ToolDefinition.WithExecutionClassifier` is evaluated before gate rewrites and
fails to an exclusive barrier. Exclusive tools bypass the parallel-tool pool;
custom nested delegation must avoid holding a permit while waiting for that same
pool. The Go model pool is explicit (nil is unbounded); its default tool pool has
eight slots per session, and children inherit the exact pools.
`agent.NewSessionManager` supplies fleet-wide eight-slot pools, a shared broker
and journal through typed `ManagerServices`. `WorkspaceFactory` selects scratch
paths and `BashFactory` receives a typed `SessionBinding`; factories must not
recursively create/delete/stop the manager. Services and callbacks are shared
and must synchronize mutable state. Explicit owner identities are required.
The httpapi handler supplies admitted HTTP ownership; `config` resolves all 49
source settings and `launcher` passes the supported subset into owned services,
rejecting unavailable activations. Inspection is settings/availability only, with
full effective posture and `.env` discovery pending. Default per-run trajectory
recording and owned read/export routes are implemented. Typed
`RuntimeConfig.Spill` / `ManagerServices.Spill` accept `spill.Store`; the launcher
best-effort constructs `spill.LocalStore`. Runtime binds a real shell executor
independently, retaining its credential scope and process tracker. A custom store
accepts already-masked `spill.Request` and returns an opaque `spill.Ref`; it must
synchronize shared state. Failures/panics leave the command preview intact. Namespace
is storage grouping, not authorization. Python `run_bash` and Go `ExecuteBash`
preserve oversized string output, while the actual default structured Bash adapters
bypass this policy in both languages. Do not infer preservation from store presence.
Injected non-shell executors own their policy; read/glob do not use this seam.
See `go/README.md` for concrete semantics and the parity matrix for evidence.

Managed Go controls are separate from the transcript/model lock. A before/guard
hook may call ChangePermissionMode; the gate loads current mode at permission
selection after those hooks. UserPromptHooks and Injectors receive a detached
mode snapshot at callback entry. Builtin steering/posture injection follows custom
injectors and precedes compaction; child sessions inherit custom seams but cannot
consume parent control queues. Steer is synchronous and parks even when idle;
SubmitSteering additionally wakes an idle owned turn. Manager shutdown/deletion
joins that active holder. Neither control text nor posture wrappers grant human
or workflow authority. See `go/README.md` for queue and persistence boundaries.


### Go self-audit observation seam

The separate selfaudit package consumes concrete Observations for sessions, fixed
global/session ledger slots, prefiltered trajectory tool-use observations and
cron snapshots. Collection remains adapter-owned. Summaries/total counts and
failures are optional explicit fields; missing and empty remain distinct.
Failures expose only their class. Global and per-session recordings retain the
store ordering; owner report selection uses only BySession. Authorize before
collecting files, mask output at the recording/HTTP boundary and capture an
immutable snapshot before rendering. Global inclusion is separate from owner
selection as in source; authenticated routes must select owner and false.

The library neither launches suggested objectives nor installs benchmark drafts.
NoExpectation carries only null, and benchmark task construction still requires
a reviewed judge. Live manager, HTTP and optional model binding are implemented.
RuntimeConfig accepts SelfAuditObserver and a finite SelfAuditView; owner view is
the zero value. The observer must avoid acquiring the running session turn lock.
ManagerServices.SelfAuditTools binds the manager automatically. Operator visibility
is explicit embedding authority; authenticated frontends select owner view.
The model has no scope arguments. Selected children retain the trusted binding.


### Exact diagnostic ledger seam

The optional selfaudit.ProblemSource interface returns a detached Ledger through
SelfAuditProblems. It changes no existing action/approval interface requirements.
The broker and in-memory journal count every occurrence under their holder mutex
before legacy deduplication; reads do not mutate live state. Shared problems.Log
uses FIFO distinct retention and immutable Counter values. Ledger totals are exact
JSON integers with decimal report formatting; Counter.BigInt gives a detached copy.

Live collectors must establish owner scope before IO. The source ScopedMemory
proxy delegates problems to its backing store through __getattr__; shared stores
can therefore mix tenants' diagnostic filenames. That accessor alone is not
owner attribution. Native ScopedStore exposes only explicitly attributed
write/replacement diagnostics from that binding. Unattributed filenames stay in
privileged raw-store diagnostics; binding ledgers do not aggregate one another.
ObserveSelfAudit admits owner handles before IO, validates recording metadata
before event visits and releases manager/session locks before injected callbacks.
Existing native holders expose optional ProblemSource; stored journals still need
an adapter. Authenticated frontends must set Owner and IncludeGlobal=false.
The observer itself installs no route or model tool. HTTP now binds its full
report mode and ObserveSelfAuditProblems ledger-only mode. The latter does not
project Info, enumerate cron jobs or read recordings; a failed ledger snapshot
is an error instead of an empty proposal set. Authenticated HTTP always selects
the admitted principal and false, ignores scope query overrides and spends no
rate budget. Plain text is projected before writing and capped after masking
expansion; JSON uses the existing typed projection. The separate explicit
SelfAuditTools binding installs the model tool through the same execution gate.

## Go improvement acceptance-instrument seam

The improvement package supplies the source path classifier and a fixed-size,
comparable InstrumentFingerprint. A future verified loop must capture its baseline
before execution and sample again immediately before each acceptance judgment.
It hashes only the four source root-relative glob selections, in source order;
substring-based touch flags have a broader scope. Read failures have a stable
marker; stat permission and other enumeration IO failures remain errors. The
private filesystem seam exists for fault verification, not as an injected runtime
service. NewArchive performs no startup IO; Record accepts concrete ProposalFields
and ArchiveRecordOptions. ArchiveMasker masks string keys and values before the
append lock, preserving source last-key collision behavior. Projection failures
abort without raw fallback; filesystem failures return the allocated ID. This
JSONL review index is best-effort, with a per-instance lock and no cross-process
lease. List accepts ArchiveQuery with a trusted owner and optional limit, returning
closed ArchiveValue variants that preserve arbitrary legacy fields without raw
JSON service state. The decision parser is deliberately stricter (duplicate keys,
finite/scalar-only values and byte/depth caps) and cannot decode this legacy index.
Nil owner is an operator query; nil limit defaults to 200,
while explicit zero/negative selects one. Accepted rows count after owner filtering.
Reads take no append lock, read the whole source file, and do not remask old rows.
IO failure yields an empty index; conversion/encoding/scoped shape failures abort.
SessionManager owns the fixed workspace_root/.improvements archive without startup
archive IO. ListImprovements accepts a trusted optional OwnerID; default GET binds
the authenticated principal or an open operator view, ignoring query overrides.
It returns historical records without remasking and spends no rate budget. Full
JSON serialization finishes before headers; lone-surrogate/nonfinite values and
read faults produce source private plain 500s. Verified-loop receipts and proposal
POST composition remain pending. Archive APIs grant no model tool or merge authority.

## Go verified-loop contract seam

verifiedloop supplies TaskSpec/CheckpointSpec/ReceiptSpec/RoundPlanSpec constructors
that retain detached private snapshots, named IDs/revisions/statuses and five
closed Operation variants. ApplyPatch checks CAS, contract revision and every
receipt hash before applying operations atomically. Verified status needs a clean,
complete receipt covering that specific state requirement. Empty patches still
increment revision; duplicate checkpoint rows collapse like source dictionaries.
NewPatch detaches artifact evidence, operations and receipt slices. Accessors
return copies; text canonicalization and contract hashing are deterministic.
Hash excludes source surfaces/persistence/contamination metadata, so a receipt is
not an authorization signature. The eventual service must generate receipts from
actual acceptance effects and sample integrity before every judgment, then bind
the pure fold to the ordinary session/subagent/execution pipeline. This library
activates no runtime tool, verifier execution, persistence or completion policy.

## Go verified-loop coordination seam

NewService requires a trusted Worker and AcceptanceRunner, with optional integrity
probe and typed event sink. RunTask keeps state/receipts/feedback local, samples
integrity before execution and immediately before each acceptance command, and
folds verified only from its resulting clean complete receipt. Nil baseline
disables subsequent probes as in source. Missing MaxRounds means three; explicit
zero/negative emits an unverified checkpoint without executing a worker/command.
Failures and cancellation abort; callback panics become private errors, not false
receipts. ShellAcceptance uses the existing configured workspace, credentials,
spill/process/sandbox policy; it does not establish OS confinement.
WorkspaceIntegrity supplies the same four source globs, not expanded coverage.
ManagedSession.RunVerifiedWithContext binds worker role selection, execution
workspace and round/receipt/checkpoint telemetry to the ordinary session pipeline.
The trusted caller owns external owner admission; operator acceptance uses the
structured executor directly as in Python, while worker calls retain tool gates.
Proposal Git composition and POST remain the next task. No model can
choose these effect implementations or turn its summary into a verified result.

## Go managed verified task seam

RunVerifiedWithContext takes a trusted RunContext and VerifiedRunOptions. A single
managed operation owns admission, active cancellation, trajectory and terminal
publication across all rounds. CheckInstruments enables execution-workspace
fingerprints; false preserves the source optional probe. A string-only executor
cannot supply exit authority and is refused. Workspace-aware executors must match
the session execution root. Round/receipt/checkpoint events have concrete payloads,
detached accessors and archival decoders; reading them grants no live authority.
The parent conversation is unchanged; real worker transcripts remain child-owned.
Configured lease renewal checks precede worker/acceptance/event effects and the
return boundary, without reacquiring a missing lease. These checks are boundary
checks, not a heartbeat or fencing of an already running external process. There
is no registered model tool, HTTP route or default task policy in this API.

## Go improvement proposal seam

selfimprove.Service composes a trusted worker, structured command runner, repository
checker and optional typed verified/proposal event sinks and archive recorder.
GitRepository uses the source 30-second git rev-parse exit check; ShellCommands
uses the caller-configured executor. Acceptance instruments are always sampled
from the configured workspace. Empty acceptance/non-Git workspaces are refused.
Git command strings are fixed; objectives never enter a command string. Failed
add or commit retains an explicit working-tree diff fallback. Unverified attempts
are still committed for review. Proposal.Lineage distinguishes absent archive
metadata from an attached archive with a null parent in the flat JSON projection.
Recorder fields, lineage options and proposed-event slices are detached.

ManagedSession.ProposeImprovementWithContext binds the actual session worker,
execution root, command executor and owner under one admitted cancellable turn.
Git, archive and event boundaries use the existing verified lease checks. The
typed improvement_proposed event follows ordinary masking, recording and archival
decode; historical rows grant no authority. The trusted operator supplies an
isolated checkout: source git add -A includes every changed path. This API never
creates or merges a branch and adds no HTTP/model tool or default policy. The
manager now supplies the owned proposal POST as described below.

## Go owned improvement proposal HTTP seam

SessionManager.ProposeImprovement resolves an already admitted owner, supplies
the manager's fixed archive and uses atomic try admission for the entire managed
proposal. POST /sessions/{session_id}/propose-improvement validates the source
objective/acceptance lengths and max-round coercion/bounds, ignores unknown fields,
then performs owner lookup and busy admission. Missing/foreign sessions stay 404,
busy stays 409, source operator-correctable admission errors stay 400 and other
failures use a private plain 500. The body cannot choose owner, workspace or archive.
The route spends no rate budget and does not create an isolated worktree; it runs
in the existing session execution workspace, matching Python. The operator must
prepare the isolated checkout before creating/binding the session. Success uses
the source direct response projection, while events/index use their existing masks.


## Go team mailbox and owned-view seam

`teams.New(teams.Config{Root, Masker})` selects a concrete in-memory bus when
Root is nil or persisted JSONL when it is established. Send takes SendRequest
with typed mailbox identities, content, optional MessageType (nil means message,
explicit empty stays empty), object-only Metadata and closed Field extensions.
Reserved Python parameter names are not extension fields. SendResult separates
sent/refused text from filesystem/callback faults. Read consumes and owns malformed
row/overflow reports; Peek does not consume. Problems returns a detached bounded
ledger. Returned Message data is immutable, preserves historical fields and grants
no authority. Disk masking traverses structure before escaping; in-memory storage
retains source unmasked semantics. Applications own the trusted root and callback
synchronization; masking callbacks must not reenter the same bus. Native
directories/files use private 0700/0600 modes. Instance serialization is not a
cross-process transaction.

SessionManager binds `.teams` and team=id/name=lead independently of model-tool
activation, including restoration and forks. PeekTeam(ctx, owner, id) performs
owner lookup before IO, supplies the fixed identity and returns newest 50 messages.
HTTP GET team uses that view and the direct historical source projection. No HTTP
body can choose another mailbox/root/identity; historical encoding failures remain
private and leave the mailbox intact. Bare/custom runtime sessions may have no
identity. `protocol.TeamSchemas()` and the concrete ToolInput constructors now
cover all ten team/protocol input variants. They encode/decode through the existing
provider/block/storage boundary. Inputs carry member names and request IDs, never
owner, session, team identity or mailbox root. Metadata is object-only immutable
closed JSON; its zero value is an empty object. Optional nil metadata retains
absent versus explicit null in recording identity, while optional text retains
absent versus empty and rejects null. Copies/accessors detach pointer state.

CanonicalJSON/SortedPythonJSON recursively sort metadata keys without changing
live insertion order. MapToolInputStrings masks names, values and nested keys
before escaping, with source last-value key-collision semantics. Historical
nonfinite/surrogate values remain supported by the mailbox, but do not enter this
standard model-input boundary. The source object-only schema remains exactly
{type:object}, with no invented properties/additionalProperties. Python handlers
bind keywords without enforcing their advertised scalar types; Go additionally
rejects wrong scalar/container types as part of its declared typed boundary.
ManagerServices.TeamTools now binds ten team variants through the
common gate (see below). Round injection and owned idle/task scheduling are described
below; self-audit bus aggregation remains a future slice. These codecs
themselves install no model tools or activation defaults.

## Go team protocol coordination seam

`teams.NewCoordinator(CoordinatorConfig{Bus, Members})` requires an established
bus and takes a trusted MemberDirectory. Member returns the explicit missing,
without-agent or ready variant; nil directory means no teammate. Calls to the
directory occur outside the protocol lock and the callback must be concurrency
safe. Bus masking callbacks must not reenter either bus or coordinator operations.
This operator seam grants no HTTP/model identity authority. SessionManager owns its
coordinator, deriving team/member from the registered owned session for model tools.

RequestShutdown checks member existence, inserts a pending shutdown and delivers
its reason/default notice. RequestPlan sends a full instruction directly and refuses
oversize rather than truncating it. SubmitPlan rejects lead and retains the original
plan in a pending approval record. ReviewPlan checks ID/type/status/team in source
order, resolves it before sending the response and retains original feedback.
Deliver preserves a code-point preview of long reports, then records bus refusals.
The table is process-local and globally capped at 200: resolved oldest-first, then
oldest pending only when required. Snapshot/TeamProtocols return detached records.

Consume drains before processing, correlates responses and automatically sends
shutdown acknowledgments for ready teammates. It returns a named ConsumedInbox
with Messages and a ShutdownRequested assignment flag. The caller owns the sticky
session shutdown state and must preserve a true flag even with a later error.
Malformed historical metadata/request IDs may fail after earlier transitions/acks;
failed batches are not replayed or put back. Rendering uses source ASCII indentation
and historical closed values; standard HTTP encoding retains its existing refusal
of nonfinite/surrogate data. Response correlation deliberately retains source
request-ID/type/status matching; this library does not add sender verification.

ManagerServices.TeamTools (or standalone --team-tools / launcher.Options.TeamTools)
installs ten tools independently, including owned-lifetime spawn_teammate. A typed private manager
binding executes closed ToolInput variants only after runtime owner/session/workspace
checks. A guard before journal replay prevents stale or foreign authority from reading
settled private projections. Members are read from immutable registered identities in
manager order; only trusted construction can establish a teammate. Shutdown/plan/review
are lead-only; send validates the roster plus lead, and broadcast preserves source
recipient order/refusal summaries. The directory callback runs outside coordinator
locks. Standard metadata becomes immutable bus fields without reordering.

ReadInbox drains/renders, applies a sticky atomic shutdown assignment even on partial
failure, and preserves the source read-risk/read-only trait despite automatic acks.
Tool results, masking, hook order, permission checks and action settlement use the
existing gate. Selection can only reduce installed tools; forks bind a fresh lead
team. Delegated subagents inherit selected tools but have a fresh guard and no parent
manager/bus binding. Bare TeamTools sessions return source unavailable notices.
Source handler fixtures establish trusted rosters before execution; they do not prove
spawn/inheritance or lifecycle. Native SessionManager.SpawnTeammate requires an admitted
owner/parent and reserves names before construction. It inherits the original workspace,
bound-workspace retention flag and fixed skills/memory/owner resources, while rebuilding
the manager-default model/system and interactive permission mode. The teammate prefix
wraps the base system builder; named peer context drops human capabilities. Children
share the original task store and omit recursive spawn. The manager owns the initial
run context and cancel/join handle, delivers normal results to lead and drains it before
reclaiming shared scratch on delete/stop. Parent deletion does not cascade to members.
Construction faults release reservations and remove unpublished persistence.
Explicit TeamTools also binds round injection after background notifications and
before custom injectors. Consumption requires the live registered core identity;
shutdown assignment survives partial protocol failure. Emit team_inbox count before
rendering the cleaned message batch, including when malformed sender data then fails.
The named event has an archival decoder; it grants no authority. Delegated children
have no parent manager/bus binding and therefore no automatic inbox delivery.

After initial delivery, the owned worker polls, consumes, clears sticky shutdown and
runs raw historical inbox prompts or the first successfully claimed runnable task.
Fresh idle contexts are peer_agent / agent, stamped session_manager, delegated by lead
and named for the teammate, with no parent message or human capabilities. Deadline
resets after result delivery; source polling may cross the deadline before processing.
Tasks remain in the original board, and result metadata contains task_id. Existing
worktree paths select the same atomic dependency rebind used by enter_worktree; invalid
or missing paths fall back to the lifecycle root. Managed turn admission protects the
rebind and run from competing turns. Cancellation joins the worker; timeout/shutdown
leaves the handle registered. ManagerConfig.TeamIdlePoll / TeamIdleTimeout default to
1s / 60s; negative durations are refused before filesystem effects. The launcher binds
the already validated MINILOOP_TEAM_IDLE_POLL / MINILOOP_TEAM_IDLE_TIMEOUT settings.
Task/worktree tool installation remains independently selected.

Snapshot 112 uses actual Python spawn, model turns, injector and idle scheduling for
8 injection and 11 idle recipes, retaining real task/mailbox effects. Prepared directories
exercise worktree path selection; they do not claim new Git branch creation. Clock sleeps
are real and short. IDs/paths are normalized. Native tests add owned idle-run delete joins,
shutdown registration retention, stale injection admission and delegated-child isolation.
Fleet diagnostics, restart teammate restoration and full default feature activation
remain pending; the worker/team identity is process-local.

Managed root composition supplies the source label `main` for create/fork/restore;
named teammates override it. Low-level runtime callers can still select a label.
Persisted records carry no team role/identity/runner. Restoring a teammate builds an
ordinary lead in its own team, retaining recorded owner/workspace/transcript/status,
and leaves old-group mail untouched. Snapshot 112 includes real source SQLite
close/reopen evidence; the native injected-store comparison is not SQLite evidence.

## Go workflow model seam

The workflows package exposes finite named states, DecodeDefinition, CanonicalJSON,
ContentHash and NewArtifact. Value is the existing immutable closed JSON sum; schema
objects are admitted before explicit ValidateSchema/ValidateValue/ValidateDefinition.
Definition keeps private canonical projections; detached views cannot change its hash.
Saved definition_hash never supplies identity or authority. Numeric type identity,
Unicode and Python enum/status spelling are preserved. Artifact snapshots contain
explicit IDs/hashes/verification/media/timestamp fields; creation does not validate
a schema or establish verification truth. No service/tool/storage adapter is installed.
Validation preserves source error order and its limited schema subset, including
Python enum/const equality versus strict numeric type admission. Validation does not
grant launch authority; origin checks remain service-owned. ArtifactFromSubmission
requires a structured return_artifact result, then validates against the controller's
schema and binds the controller's run/node/attempt IDs. VerificationFromValue falls
back to unverified for malformed/missing status. It does not invent verification.
runmeta.Snapshot is a shared inert provenance record with detached Clone projections.
agent retains private trusted RunContext construction, validation and capability checks;
its public provenance types alias runmeta without a wire-format change. Consumers
may store snapshots but must not use decoded fields to mint execution authority.
WorkflowRun, NodeState, AttemptClaim, NodeAttempt and OutboxSnapshot now expose
named record projections and Clone methods. DecodeWorkflowRun/DecodeNodeState/
DecodeAttemptClaim/DecodeNodeAttempt/DecodeOutboxSnapshot lower JSON through
concrete variants, refuse unknown fields and validate status enums before return.
Store/service adapters must clone write/read records and use these boundary
functions for external JSON instead of decoding directly into mutable projections.
Args and outbox payload must be closed object values; decoding does not validate
a workflow input schema, authorize launch or establish delivery/verification.
Nested provenance is an already materialized snapshot; decoding never stamps missing
caller fields, normalizes historical grants or reconstructs a trusted live context.
Native counters are bounded machine integers (versions/cursors use int64), and
wall timestamps are float64; Python arbitrary live objects are not admitted.
NewInMemoryStore supplies the process-local registration/admission/transition/claim
core. CreateRunInput uses named identities, inert provenance and closed object args.
RegisterDefinition validates before deduplicating revision/content; semantic aliases
return the existing revision without registering the supplied alias. CreateRun hashes
definition/args/provenance/parent/action/policy for a session-key launch identity and
returns the latest detached run on replay. New launch contexts materialize omitted
capabilities as an empty list; archival Snapshot.Clone still retains nulls. TransitionRun and ClaimNodes require the
expected run version. Claims validate the entire batch before mutation and retain
source budget/refusal order; dependency/concurrency scheduling is engine-owned.
StoreError exposes finite source kinds/details; NativeRecordOverflow refuses int64
version wrap before mutation. This store is not durable and does not authorize launch,
check argument schemas, execute workers or append external outbox messages.
StartAttempt gates CLAIMED -> RUNNING by attempt version. CommitAttemptInput binds
the attempt ID/version, two terminal enums, optional immutable Artifact, optional
VerificationStatus and error. Nil verification selects not_applicable; an explicit
invalid value retains the source late failure after artifact/attempt changes but
before node/run settlement. Callers must supply validated enum variants; the store
is not a transactional repair boundary for arbitrary invalid operator input.
Artifact provenance must match run/node/attempt. Store does not recompute hashes,
check artifact schemas/schema_valid or correlate the two terminal statuses.
GetArtifact and ArtifactsForNode expose immutable models in recorded reference order.
CancelClaimedAttempts settles unstarted attempts in insertion order, retaining
earlier settlements on a later source node-status refusal. RequestCancel uses run
CAS and a sticky first nonempty reason; invalid source transitions retain that reason.
FinishCancellation refuses running nodes. FailRun uses source read-then-CAS ordering.
FinalizeRun checks run CAS/RUNNING, artifact existence and successful node states,
then publishes completion and one WorkflowCompleted OutboxSnapshot together.
It does not bind final artifact ownership/return-node provenance; callers own that
check. ListOutbox takes a named OutboxFilter and returns detached records ordered by
creation time/ID. EnqueueOutboxInput supplies named run/kind and immutable object
payload; an existing run/kind returns the current message before checking a new
payload. ClaimOutboxInput supplies session, optional run IDs (nil means any; an
explicit empty list means none), lease seconds (nil selects 30) and optional limit.
OutboxLease returns a fresh named token even with no selected messages. Selection
uses insertion order before sorting the response, and checks lease age using the
current caller's duration. As in source, nonpositive durations fail; operator NaN
and positive infinity retain Python comparison behavior. No saved expiry is minted.
SettleOutboxInput identifies session, message IDs and token. AcknowledgeOutbox
requires a nonempty token; ReleaseOutbox retains source empty-token matching.
Both use session isolation and preserve earlier sequential effects on a later
missing/foreign/lease-mismatch refusal. Duplicate acknowledgments retain duplicate
return records; repeated release can conflict after the first clears the lease.
Delivered acknowledgment/release is a no-op after session admission. Only caller
append success justifies acknowledgment; this store does not append externally.
Native counter/ID allocation admission precedes publication; overflow does not
publish partial cancellation/finalization. PruneTerminalRuns accepts optional named
TerminalRunLimit; nil selects MaxTerminalRuns (500). It retains newest terminal
runs whose outbox is fully delivered; pending/claimed/expired-but-unacknowledged
notifications spare the run. Negative source limits remove all eligible runs.
Pruning atomically removes owned maps, launch/outbox keys and both insertion indexes,
then returns run IDs for service bookkeeping. Ownership is by run_id, not parent
references; surviving child metadata and reusable definition/hash indexes remain.
Replaying an evicted session/key starts fresh; replaying a retained one preserves
its payload conflict boundary. Service installation and trusted live origin adaptation
remain open; this callable library does not schedule pruning automatically.

WorkflowRunner now receives detached AttemptExecution records (attempt/node/closed
inputs) and returns an optional structured ArtifactSubmission plus error. Its
implementation must respect context cancellation; arbitrary panics and hard process
termination are outside this contract. RunnerError supplies finite Python-style
error kinds; unspecified native errors render NativeRunnerError. WorkflowEngine
validates definitions, serializes Execute per run, folds dependency artifacts and
claims definition-ordered batches bounded by concurrency and remaining attempts.
AttemptPool is explicitly shareable across engines; default capacity is four,
negative capacity is refused and zero blocks until cancellation. AttemptPermit.Release
is idempotent; no arbitrary semaphore growth is exposed. Permits cover runner calls,
not artifact validation/settlement. Cancel publishes run cancellation and cancels
in-flight contexts without waiting for workers; Execute joins the batch. Callers must
join Execute before reclaiming worker resources. Parent context expiration settles
attempts and returns the context error; run cancellation/wall-time policy remains
service-owned. Verifier failures retain schema-invalid unverified fallback artifacts,
including source nil-submission AttributeError. Idle run locks are reclaimed rather
than accumulating Source's lock table. agent.NewFreshWorkflowRunner now supplies
the isolated native worker adapter through WorkflowRunnerConfig. Provider, owner,
workspace and WorkflowContextResolver are required; the resolver receives a detached
attempt and returns a valid private RunContext. It must resolve trusted live authority,
never reconstruct it from saved snapshots. Each call creates a fresh readonly session,
Explore-selected catalogue, owned exact-schema return_artifact handler and in-memory
compactor. Defaults select read_file/glob; configured catalogues retain handler scope
and the independent readonly policy denies mutation. Human capabilities are dropped
in the named peer context. Configured secrets mask read results before the model;
shared limiters, recovery/cache/stuck/skill/event seams are explicit. No parent history,
injectors, hooks, stop hooks, approval broker or persistence are inherited. Custom
handlers remain responsible for their workspace/confinement contract. Capture validates
before accepting once; duplicate submission is a textual refusal. Synthetic tool inputs
are a named closed variant, including explicit null; exact immutable schema projections
preserve numeric enum/const across model requests and archival ToolSchema reads.
Normal exit without a structured result raises source RuntimeError. Parent cancellation
must be joined by the engine/service before resource reclamation. LastWorker is a
synchronized detached diagnostic and does not grant authority. The owned
WorkflowService below composes this runner. Optional runtime tools are described
below; HTTP and manager installation remain pending.

workflows.NewServiceViews owns the status/summary and notification projection layer.
Status reads run/node/artifact state under one store lock and detaches optional fields.
Session filtering hides foreign run IDs as NotFound but is not owner authentication.
RecordLaunchTurn is first-wins; PruneTerminalRuns removes matching bookkeeping.
PrepareNotifications takes named SessionID/ParentTurn, selects strictly later-turn
runs and caps each lease at MaxWorkflowNotifications (50). NotificationBatch keeps
its recipient/turn/token/message identities private; public notifications/IDs detach.
ContextMessage emits the source untrusted artifact-data wrapper. Results over 8,000
UTF-8 bytes become null with a 2,000-code-point preview; status retains full retrieval.
Nonempty run error/reason strings override closed outbox payload diagnostics.
DeliverNotifications requires a NotificationAppender with explicit recipient/turn/
content. Append must succeed before ack; prepare/construction/append failure releases
claims, and acknowledgment failure retains the append effect. Return count plus error
reports that partial boundary. No durable or exactly-once delivery is implied. Caller
owner admission and live session mutation belong to the future owned-service adapter;
this package does not append to an agent or install an automatic injector.

DefinitionAdmission captures named DefinitionCaps by value at construction. Admit
accepts an immutable object Value, discards definition_hash/definition_id/revision/
parent_revision/source/source_version and forces dynamic source before decoding.
Definition and graph validation precede concurrency/agent/round/wall-time process
caps in source order. AdmittedDefinition exposes immutable content and the canonical
wfpolicy digest, without granting live authority or writing to the store/journal.
Callers must retain original tool input separately for action identity. Unknown
wire fields retain the existing native decoder refusal rather than Python constructor
TypeError text; invalid operator caps are refused before admission.

protocol.WorkflowInput preserves original immutable object Definition/Args rather
than prematurely lowering DefinitionAdmission's normalized view into the journal.
WorkflowReferenceInput uses WorkflowRunID shared with workflows.RunID and the
agent action record alias. The closed ToolInput variants support exact canonical
and spaced Python JSON, archival round trips and recursive recording masks.
WorkflowToolSchemas returns detached source schemas; bound runtime handlers below
consume them. Manager installation remains separate composition. Optional bound runtime tools below
perform trusted origin/capability checks.
Optional tools are absent from DefaultToolNames. The decoder enforces the advertised
outer object shape; it does not validate the definition DAG or argument schema.
ActionJournal.Begin hashes the original input and AttachWorkflow binds one run
without changing action status; duplicate binding is idempotent, conflicting run
binding/replay is refused. A started record is not a dispatch claim. Missing-action
errors retain existing native diagnostics instead of Python KeyError formatting.


### Go owned workflow service seam

agent.NewWorkflowService captures DefinitionCaps, an ActionJournal, a live
WorkflowParentResolver and WorkflowRunnerConfig. The parent resolver binds a
trusted OwnerID/workspace and a concurrent-safe WorkflowEventSink. WorkerFactory
is an optional operator injection; the default creates FreshWorkflowRunner workers.
Service-supplied parent, live context, rounds and progress sink replace caller worker
configuration. A shared AttemptPool limits aggregate worker concurrency. Custom
workers and observers must cooperate with cancellation; callbacks are not sandboxed.

Launch requires a private validated ExplicitHuman RunContext with per-message
workflow.launch capability, nonempty ActionID and a live owned parent. Definition
admission, authority policy and argument schema checks precede journal/store effects.
WorkflowLaunchRequest.ActionInput must carry the original typed model input when
composition already began its action; nil uses the source direct-service normalized
fallback. Replays bind one run. Queued replay accepts only a matching original
trusted live context, never reconstructs authority from a saved runmeta snapshot.

Wait cancellation only ends the caller's wait. Cancel, CancelSession and Close join
owned tasks; Close continues draining when its caller times out and rejects new
launches. Live context is reclaimed at terminal task completion. PruneTerminalRuns
pins registered tasks through terminal event/outbox publication and clears service
bookkeeping after eviction. Use the service pruning entry point when tasks are active;
direct Store/Views mutations are operator seams and bypass those lifecycle pins.

WorkflowEvent is a closed payload union. Progress exports only type/name/id/error/
duration_ms, without tool inputs or outputs. Observer errors retain at most 100
500-code-point diagnostics and cannot fail execution. Terminal event publication
and result enqueue events are once per live service. Outbox enqueue retains source
run/kind deduplication, including the store's three-field completion payload.
Native wall times must fit a positive Go duration. These process-local mechanisms
are not durable recovery or exactly-once delivery. Manager/HTTP installation,
SSE/archive projection and automatic parent append are separate pending work.


### Go bound workflow model tools

RuntimeConfig.WorkflowService explicitly enables Workflow/WorkflowStatus/
WorkflowCancel and supplies that service's journal to the ToolGate. A conflicting
runtime ActionJournal is superseded, matching source injected-service composition.
WorkflowTools alone advertises tools whose missing service is a runtime refusal;
all defaults stay off. ToolSelection can narrow this surface but cannot enable it.
NewManagedSession binds the actual parent handle so launch records its runCount.
Bare Session launch is refused. Owner/session/workspace guards and trusted per-
message launch/manage capability checks run before journal replay and again at the
handler boundary; this explicit native replay fence protects private cached results.

Launch is exec risk, cancel write risk, status readonly read risk, and all three
are ordering barriers. Permission mode still applies. A launch retains original
WorkflowInput for Begin/AttachWorkflow and requires a journaled ToolCall. Status
and cancel use the bound session filter; cancel joins with reason "cancelled by
trusted parent". Responses preserve source sorted, spaced UTF-8 JSON. Custom
service resolution/event sinks remain operator seams. Runtime construction does
not own the injected service's shutdown; SessionManager does as described below.
Automatic notification injection and HTTP/SSE/archive integration remain pending.


### Go manager-owned workflow lifetime

ManagerServices.WorkflowTools enables one owned WorkflowService with captured
WorkflowCaps (nil selects defaults). Injected WorkflowService implies activation
and supplies the manager journal and attempt pool. A conflicting explicitly supplied
WorkflowAttemptPool is refused by identity. Worker construction inherits provider,
model budgets, skills, secrets, role policy, recovery/cache/stuck seams and shared
model/tool permits. The default live parent resolver accepts only current active
manager sessions. New, forked and restored sessions bind the same service; teammates
remove the three tools. Workflows() is an operator seam, not owner authentication.

Native bound guards reject deleted/stopping manager handles before journal replay.
The private managed-parent admission check occurs under the service admission lock;
CancelSession takes the same barrier before recording its deletion snapshot. Launches
already in admission register tasks before deletion joins, and closed parents cannot
admit later tool launches even with an injected custom resolver. Cancellation joins
terminal publication tasks too. Injected resolvers/callbacks remain trusted operator
seams and must cooperate; direct operator requests have their own lifetime policy.

Delete owns asynchronous workflow cancellation alongside existing turn/background
cleanup. Scratch reclamation waits for both; preserve/bound policies remain in force.
Cancellation errors or remaining work prevent removal and pin the workspace against
other shared-session cleanups. Shutdown retries eligible pins after service closure
and cleanup joins, retaining paths still used by surviving sessions. Stop callers
may cancel their wait while background shutdown still owns drainage. No durable
worker restart, cross-process fencing or exactly-once effects are implied. Session
workflow event/SSE/archive projection, summaries, notifications and HTTP remain
separate pending integrations.


### Go workflow session observation

The default manager constructs an internal event resolver distinct from launch
admission. It may observe terminal publication during Stop, but never resurrects a
deleted session or grants launch authority. Injected services keep their operator
resolver/sink policy. The managed sink validates service/session identity and emits a
closed SessionEvent through the existing masked persistence/trajectory/subscription
path. Workflow() returns a detached known variant; stored-event decoding carries
only historical untrusted provenance. The sequence field aliases the live event seq.

SessionInfo.Workflows is a concrete ordered RunSummary slice, empty when disabled
or freshly forked; the existing HTTP session wrapper preserves it and health reflects
activation. Unsupported reserved event kinds remain refused. Current emitted payloads
have concrete fields; additional arbitrary archival payload members are not retained
by this projection and full open-payload archival parity remains pending. Automatic result injection and dedicated
workflow routes remain pending. Stores and callbacks retain their existing cooperative
and ownership requirements; an injected store is not proof of native SQLite support.


## Owned workflow result injection

Manager-bound workflow parents append later-turn results automatically after user
injectors and before steering/posture. Launch-turn results remain pending. The core
turn lock owns append; the concrete parent appender checks live session/owner/turn
binding and cancellation. ServiceViews releases failed construction/append claims
and acknowledges only after the message is in history. A failed acknowledgment keeps
that append observable. The Source untrusted-artifact-data wrapper conveys data and
grants no capability. Standalone runtimes, Fresh workers and delegated/autonomous
children have no automatic manager delivery. This path is process-local and is not
a durable append/ack transaction. Snapshot 130 compares actual Python managed turns.

## Go workflow HTTP admission

POST session workflows validates the closed request object before owner lookup,
then requires the optional service and authenticated deployment. WorkflowHTTPRunContext
is a trusted caller constructor: the adapter establishes deployment authentication
and ownership before calling it. Only workflow.launch is approved. Actor, channel,
stamp and stable msg_ action identity are captured; body fields and saved provenance
cannot reconstruct or widen live authority. General HTTP messages remain untrusted.

The service retains definition admission, policy checks, typed journal identity,
worker ownership and terminal publication. HTTP uses Source launch_turn zero, not
a parent-model turn counter. Unknown request fields are ignored. Retained JSON data
lowers into immutable closed Value; request diagnostics never enter service authority.
Known permission/conflict/lookup/validation failures retain HTTP categories; nested
malformed-definition and retained legacy-scalar profiles still need broader G7 proof.
