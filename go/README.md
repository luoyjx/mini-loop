# Go implementation

This directory is the independent Go port of the Python runtime in `../python/`.
It is under construction. The httpapi package provides an embeddable HTTP/SSE
handler; `cmd/miniloop` now provides standalone HTTP startup and signal shutdown.
UI and the remaining Python default services are still pending. The
protocol package fixes the default fake-model transcript shapes as explicit Go
types, completed model replies, usage and stop reasons, and has concrete inputs
for all ten Python default tools. It rejects
unsupported tool names and extra fields at the JSON boundary instead of
admitting an untyped payload into the runtime. The `agent` package adds an
injected, in-memory fake-model turn loop. Every tool call now passes through a
typed gate with an immutable executable catalogue, ordered hooks and permission
modes. `NewSession` registers Bash only and derives a real executor's bound workspace. `NewWorkspaceSession` binds a
workspace and registers Bash, `read_file`, `write_file`, `edit_file` and `glob` through
the same gate. `NewRuntimeSession(RuntimeConfig)` adds `TodoWrite`, `load_skill`
and `ask_user`, plus deferred `compress` and bound `task`: all ten default tools
now execute through the same gate, with explicit dependencies and state bound to
one session and owner. Todo, stop, stuck, compaction and scoped child events share a
typed, sequenced 200-event backlog.
The `workspace` package preserves bounded Unicode reads, line pagination,
unique exact edits and atomic replacements. Every file path is checked at
execution; write paths also undergo permission checks. Thirty-four Python cases
compare the output and resulting file hashes, including failure cases. Glob
preserves Python recursive and filename matching, hidden-file filtering,
workspace result filtering and Unicode budgets before deduplication and sorting.
A separate snapshot covers 38 searches and 182 filename patterns; cancellation,
readonly session execution and deep search under a low descriptor limit are
tested in Go. Enumeration uses native directory order and is not a filesystem
snapshot.
The deployment skill catalogue discovers sorted sources, preserves first-wins
names, bounds descriptions and bodies, reports refusal/omission counts and
rechecks the complete normalized source digest before serving a body. Streaming
reads retain bounded prefixes while hashing the full source. Nineteen Python
catalogue scenarios cover outputs, hashes and diagnostic counts. Ten todo
updates and four question variants match real Python outputs; tests also cover
readonly calls, cancellation, session binding and concurrent skill reads.
`RuntimeConfig.Skills` is explicit; nil supplies an empty catalogue. Descriptions
enter the actual model request with fitted, immutable tool schemas. User
skill layering is pending. A nil question surface reports unavailability;
an injected `Questioner` returns answered text or an unanswered variant, and is
not a durable broker. Invalid-name values in diagnostics retain at most 2,048 characters
and mark truncation, unlike Python's potentially unbounded diagnostic text.
`Provider.Complete` now consumes `protocol.ModelRequest`: model, output budget,
detached messages, optional system text, recursively typed tool schemas and a
local request purpose. The default system builder names only fitted tools and
includes skill descriptions; changed todo/pressure facts enter the message
stream, leaving the stable prefix alone. Schema fitting and fingerprints match
Python's JSON character budgets and canonical serialization. The fake provider
counts messages, system and schemas with Python's ASCII/wide-character model.
The token meter includes cached input, learns same-envelope growth and sees
signed shrinkage. Summary usage belongs to its receipt, not the live anchor.
Workspace-backed sessions run result spill, pair-safe snip, consumed-result
micro compaction and transcript-plus-model summary in that order. `compress`
passes the write-risk gate and runs after the entire batch has results. Failures,
empty summaries and cancellation preserve paired history. Archive/spill paths
use the workspace resolver and atomic replacement. Automatic compaction can
write artifacts under readonly tool mode, like Python's ordinary agent;
read-only workers must inject `InMemoryCompactor`. The Bash-only constructor uses that in-memory strategy for an unbound injected
executor; a real bound executor selects workspace compaction.
The context snapshot compares six catalogues, three wire/token cases, four
cheap-compaction histories, six meter steps, spill output and summary artifacts
against Python. Future sink masking and team/plan/memory prompt sections
remain pending. Compaction files persist, but cannot
restore a Go session on their own.
`task` passes the execution-risk gate and uses an explicit `SubagentProvider`.
The default `InProcessSubagents` creates fresh child history, todos and meter;
it inherits model/budgets and injected context/tool services, then uses a fixed
role-specific system prompt. `CapabilityRoleToolPolicy` selects tools only when
their nonempty capability set is entirely allowed. Explore is readonly;
general-purpose and worker are interactive independently of the parent mode.
The default roles omit unclassified tools, including task. Custom selected
built-in handlers are rebound to the child, with the same depth guard.
`RunWithContext` and `DelegateWithContext` accept caller-stamped `RunContext`;
`Run` and `Delegate` default to untrusted provenance. Default child derivation
creates a peer message linked to its parent and drops human actor/approvals.
Custom providers receive a pinned `SubagentParent` environment and parent run
context. Every delegation checks the depth quota before invoking the provider
or emitting start telemetry (defaults: depth 2, child rounds 30). Child events
forward their original label/depth/provenance into the parent backlog. Exhausted
runs prepend a stop marker to partial output instead of reporting a clean summary.
The twelfth Python snapshot covers four role selections, five context snapshots
and three real child loops (read, write and exhaustion). Go tests also cover
custom-provider refusal/cancellation, nested task rebinding, readonly Explore
with an auto parent and shared-provider session isolation under the race detector.
This library supplies a process-local fleet manager and authenticated HTTP
ownership through the httpapi handler. Core lifecycle events and shared limiters are implemented.
Custom broker/state
inheritance, owner resources remain open.
`RuntimeConfig.ActionJournal` and `NewJournaledToolGate` optionally bind a typed
journal. Stable action IDs hash session/message/tool-use/name; canonical input
hashes bind final rewritten arguments. Terminal replay does not execute again,
but still crosses current permissions and post hooks. An unknown action asks an
optional `ToolVerifier`: already-applied returns a reconciliation marker,
not-applied permits retry, and undetermined/error/panic preserves unknown.
`write_file` compares the full strict UTF-8 text with universal newlines inside
the bound workspace; Bash cannot verify an opaque shell effect.
`InMemoryActionJournal` keeps all identities and sheds old result text by count
and aggregate Unicode character budgets. `StoredActionJournal` ports Python
transitions over a concrete `ActionStore` interface; its durability depends on
that injected store. The Go SQLite adapter remains pending. Finish changes only
started records, reconcile changes only unknown records, and terminal settlement
is immutable. Like Python, a proven non-landing retry does not rewrite unknown
through finish. Journal settlement runs before observers; cancelled calls settle
without the cancelled context, and settlement faults propagate. Default children
have fresh state and do not inherit the parent journal. No cross-process dispatch
claim or exactly-once side-effect guarantee is supplied by these journals.
The thirteenth Python snapshot compares twelve input/hash/identity cases, memory
and SQLite transitions, Unicode bounds and ten real replay/reconciliation paths.
The Go stored-adapter tests use a typed test backing; they do not prove SQLite
persistence. The optional bound approval broker now supplies parked requests,
timeout/cancellation, process-local session grants, reviewer abstention and typed
approval rows; an injected store is still required for durable writes. Registry
masking binds implemented results, recordings, approvals and compaction files.
SQLite adapters, remaining HTTP surfaces, session persistence
remain pending.
Python
contract snapshots and fake reply fixtures are generated into `testdata/` by
`../python/tools/export_go_contracts.py`.


`go/shell.Executor` supplies real foreground host execution on macOS/Linux:
`/bin/sh -c`, an immutable resolved workspace, new process groups, one default
120-second process/pipe deadline, aggregate bounded capture and typed `Result`.
Python's capture bound counts decoded characters despite its "bytes" notice;
Go preserves that behavior, UTF-8 replacement, universal newlines, channel
separation and tail-preserving rendering. Nonzero exits and timeout/error status
reach the gate, journal and observer as failures, with detached command metadata.
Replays have stored text and no fresh process metadata. Cancellation/overflow
kill the process group and retry while inherited pipes remain open, covering
concurrent forks. Explicit interruption enters the same bounded cleanup.
Pipe cleanup is bounded to five further seconds. A
child deliberately detaching into a new session is outside the group.

Create an executor with `shell.New(shell.Config{Workspace: root})` and pass it
as `RuntimeConfig.Bash`. `RuntimeConfig.Secrets` binds an independent credential
configuration to a real executor before it captures/projects output, with the
shared explicit interrupt tracker preserved. Registered ambient names are
scrubbed; only command-mentioned names are injected. Full-stream masks precede
truncation and protect split-stream credentials. The legacy string executor
interface stays supported. The typed `Sandbox` argv seam rebinds to the workspace;
no Go Seatbelt backend ships and cwd is not confinement. Runtime process tests
ran on macOS. Linux code has no Linux-host execution evidence; other platforms
reject construction. The sixteenth Python snapshot covers eight actual commands,
seven rendering recipes and six typo-blocklist decisions. Core tool events are implemented;
additional sink masking remain pending.

Cache and stuck policies are enabled by default. `RuntimeConfig.CachePolicy`
can select `NullCachePolicy`; `NewCachePolicy(CacheConfig)` controls TTL, stride
and breakpoint budget. Annotations belong to one cloned request: system text
becomes one cached text block on the wire and eligible user blocks receive
`cache_control`. Live transcript blocks, assistant thinking and signatures stay
unchanged. Summaries use the same policy. `ModelRequest.System` currently accepts
plain text only; existing system block-list input remains pending. The fake
counts the annotated payload and rejects more than four points; real cache
reuse and savings require an actual provider.

`RuntimeConfig.StuckDetector` can select `NullStuckDetector` or a configured
`NewStuckDetector(StuckThresholds)`. The default keeps 20 tool steps, checks five
Python patterns after paired batches, nudges once, then halts with an explicit
stop marker. Each user intent resets the window and nudge count. Input hashes
use final gate rewrites; output hashes use final masked results. `StopHooks`
request another tool-less round through an optional string (nil finishes, empty
text continues); this is the monologue detection path. Children inherit policies
and hooks with fresh counters. Hooks receive detached snapshots and must not
recursively enter their locked session. Constructors reject invalid thresholds
and cache strides rather than accepting Python's invalid configurations.

The seventeenth Python snapshot compares twelve cache projections/token counts,
sixteen detector signals, six identity hashes and five actual looping agents.
Go tests also cover bounded/reset state, paired error exits, rewritten input
identity, summary and child policy inheritance, and detached/masked events.
Optional input fields preserve absent versus explicit JSON null through wire,
cloning and masking; their execution defaults remain unchanged.

`RuntimeConfig.UserPromptHooks` form a typed sequential rewrite chain before the
submitted user message; nil preserves text and an empty replacement is valid.
Named `Injectors` receive detached messages, todos and caller-stamped authority,
validate an entire typed message batch before append, and run before runtime
facts and compaction each round. Children inherit both seams with fresh history.
Callbacks must not reenter the locked session and shared callbacks must be safe
for concurrent sessions. Injected text never changes authority.

Consecutive parallel-safe calls overlap; exclusive calls wait for the prior group
and remain ordering barriers. `ToolDefinition.WithExecutionClassifier` can choose
per-call `ExecutionParallel` or `ExecutionExclusive` before gate rewrites. Errors,
panics and invalid values fall back to exclusive. Static readonly and parallel
traits remain independent. Every effect still crosses the gate. Result rows and
stuck steps follow model order; tool-use telemetry starts in that order while
result telemetry follows completion. Interrupted groups join started workers,
retain recorded outputs and pair unfinished calls with unknown results. Worker
panics become type-only errors and release capacity; exclusive panics retain the
existing repair/rethrow behavior. Custom handlers must honor cancellation.

`NewConcurrencyLimiter(ConcurrencyLimit(n))` supplies a positive shared pool.
Set `RuntimeConfig.ModelLimiter` to cap provider calls across sessions, children
and summaries. Nil is unbounded, matching a bare Python Agent. Parallel tools
use a fresh eight-slot pool unless `ToolLimiter` is explicitly shared; children
inherit the exact pointers. Exclusive tools bypass this pool, including default
task delegation. A custom parallel classification must cover handler and hook
safety; do not classify nested task execution as parallel while it waits for the
same saturated tool pool. Manager composition and typed environment settings are
implemented; the standalone launcher composes the supported services.
An open todo board receives `<reminder>Update your todos.</reminder>` after three
tool batches without an attempted TodoWrite. The counter persists across user
turns; child counters are fresh and a TodoWrite attempt resets it even if denied.

The nineteenth Python snapshot compares actual classified batch overlap/barriers,
ordered outputs and stuck hashes, six mode decisions, nil/empty prompt rewrites
and four cross-turn Todo counters. Deterministic Go synchronization tests cover
shared pool caps and waiting cancellation, exclusive bypass, completed-sibling
repair, worker joins, panic capacity release, atomic injector validation and
child seam/pool inheritance. Bounded steering and live modes are implemented below; remaining context integrations
remain open.

`NewSessionManager(ManagerConfig)` owns ordered `ManagedSession` handles. It
requires a provider and explicit owner identity on every create/lookup operation;
missing and foreign IDs both return `ErrSessionNotFound`. Unlike the raw Python
manager, an omitted owner is rejected and missing deletion returns a typed error
as well as false. Defaults are interactive mode, 50 rounds, shared model/tool
pools of eight, an in-memory approval broker and action journal, host shell and
`./workspaces` scratch root. Nil skills select an empty catalogue; inject a
deployment catalogue explicitly. No full environment loader is implied;
httpapi supplies authentication when explicitly composed below.

`WorkspaceFactory` selects scratch paths; `BashFactory` receives immutable
`SessionBinding` identity/owner/workspace/mode. Custom services must be safe to
share. Factories may inspect the manager but cannot recursively create/delete/stop
it. `BindableRoots` opts into existing checkout binding. Paths resolve symlinks
and home prefixes, with policy checked before existence: disabled/outside/manager
scratch roots return `WorkspaceBindingError` 403; allowed missing/non-directory
paths return 400. Bound directories are always retained.

Delete closes admission and revokes approvals before unpublishing, allows five
seconds for a holder to finish, then cancels and joins it before scratch reclaim.
Shared live and retiring references prevent early removal. `PreserveWorkspace`
retains scratch explicitly; `WaitCleanup(ctx)` joins asynchronous deleted holders.
Remembered owners and masked cleanup diagnostics are capped at 10,000 and 100.
Go reports removal faults rather than silently discarding them. Stop closes all
admission, gives current holders 250ms, joins pending construction and cleanup,
and preserves surviving scratch. Cancelling a Stop caller only ends that wait;
a later Stop joins the same shutdown. Custom providers/sinks must return so a
joined shutdown can finish. Durable restore, optional fleet services,
durable steering and session restoration remain pending; per-run trajectory files
are implemented below.

The twentieth Python snapshot compares initial status/defaults, shared services,
creation order, ten workspace outcomes, bound retention, scratch deletion and
stop. Go synchronization/race tests cover queued turns, deletion/shutdown joins,
construction faults, shared retiring paths, symlink reclamation and bounded
owner/diagnostic copies.

### Use the direct HTTP model adapter

`provider.New(provider.Config{BaseURL: endpoint, APIKey: key})` returns a concrete
`agent.Provider` usable in `ManagerServices.Provider` or `RuntimeConfig.Provider`.
Construction reads no environment/profile credentials. The default endpoint is
`https://api.anthropic.com`; a compatible base path is retained before appending
`/v1/messages`. The adapter preserves typed request cache annotations and reports
the response's served model and usage through existing model events.

SDK retry behavior is pinned to Python Anthropic SDK 0.107.1: two retries,
408/409/429/5xx and connection/timeout/body-read failures, retry override headers,
0.5..8s exponential backoff with negative jitter and <=60s Retry-After including
ms/date forms. `Failure` exposes named kind/class/status and separate header
metadata. This is independent of the Agent recovery policy documented below. Default budget preflight reflects SDK non-streaming limits; custom
timeouts opt out. A whole-attempt deadline and caller context own calls/waits.

Config permits explicit byte limits (default 8 MiB, maximum 64 MiB), 0..10 HTTP
retries, a client, and concurrency-safe waiter/clock/jitter seams. Reply ingress
validates signed thinking and supported default-tool inputs; unknown content and
nonempty citations fail until typed adapters exist. Nullable SDK text citations
are omitted. API-key diagnostics are scrubbed; redirects are refused and the
supplied client's redirect policy is preserved. Description/debug identity never
prints the key. Advanced request/auth options and
production endpoint/cache conformance remain pending. No dependencies were added.

### Use streaming model calls

`provider.NewStreaming(provider.Config{BaseURL: endpoint, APIKey: key})` selects
SSE explicitly and can replace the Provider in the same runtime/fleet configuration.
The session detects the consumer-owned `StreamingProvider` interface. Callbacks
carry named `protocol.StreamDelta` text/thinking variants and run synchronously;
they must stop before the provider returns. The returned ModelReply remains the
only authoritative model response used by tool dispatch and token accounting.

Every send has a fresh ephemeral stream_start, coalesced masked assistant_delta
progress (200 Unicode chars or 200 ms on incoming fragments), then final
assistant_text events correlated by stream_id and classified commentary/final_answer.
Ephemeral progress does not replay. Only flushed text, never thinking or pending
fragments, is retained above a managed cancellation marker. Successful streams
clear partial text before returning, including compaction summaries.

Wire assembly validates stopped text, signed thinking and complete default-tool
JSON. Configured total wire caps include ignored frames; each frame/line/block and
session progress are bounded by the protocol's 512 KiB limit. SDK retries cover
opening failures/statuses, and streaming lifts the direct token ceiling. An owned
body drop/timeout is surfaced to Agent recovery, which starts a fresh generation. Go requires a final delta/stop instead of accepting unchecked partial EOF
snapshots. Unknown content deltas and nonempty citations remain pending. No external endpoint calls were used to validate this slice.

### Configure stream progress and offline clients

`RuntimeConfig.StreamProgress` and `ManagerServices.StreamProgress` capture typed
character/duration thresholds and an optional `StreamClock`. Nil fields select
200 Unicode characters/200 ms. Explicit zero or negative thresholds publish every
nonempty fragment, matching Python. Duration is checked on fragment arrival; there
is no timer-only flush. Clock callbacks are synchronous and must be safe when
shared. Constructor snapshots reach future forks and fresh children; each stream
still owns fresh buffers, shown-answer state and an independent generation ID.

```go
chars, duration := 400, 100*time.Millisecond
config.StreamProgress = agent.StreamProgressConfig{
    CoalesceChars: &chars, CoalesceDuration: &duration,
}
fake := agent.NewFakeProvider(agent.FakeProviderConfig{})
config.Provider = fake // direct; no streaming or environment discovery implied
config.Provider = fake.Streaming() // explicit stream view over the same client
```

A `*FakeProvider` owns an atomic call sequence (`msg_fake_000001`, ...). The zero
value `&agent.FakeProvider{}` and NewFakeProvider defaults enable signed thinking,
use the one-bash-then-summary responder, zero delay, and an 8192 direct ceiling.
Thinking signatures use the Python direct/stream ordinal convention; final usage
counts all blocks and the full request, and reports the requested model. Streaming
lifts the ceiling, ignores the direct delay, and fragments text/thinking by Unicode
characters as Python does. Bare FakeProvider has no streaming interface.

`FakeProviderConfig` captures an optional Thinking pointer, explicit Delay,
NonStreamingCeiling pointer, DisableNonStreamingCeiling flag and typed
FakeResponder. A responder returns FakeGeneration content/stop reason; the client
stamps message identity, thinking, usage and model. Shared responders must honor
contexts and synchronize state. Use pointers and do not copy clients after use.
No `MINILOOP_FAKE_DELAY` discovery is implemented yet. The fake proves offline
protocol/runtime behavior, not endpoint conformance or cache reuse.

### Configure Agent recovery

Nil `RuntimeConfig.Recovery` or `ManagerServices.Recovery` selects DefaultRecovery.
Use `agent.NewDefaultRecovery(agent.RecoveryConfig{FallbackModel: "backup"})`
for fallback, or `agent.DirectRecovery{}` to disable Agent recovery. SDK transport
retries remain independent. Optional pointers configure zero retries/continuations
or disable escalation; values must be within 0..100. Waiter/Jitter dependencies
must honor contexts and be safe when shared. Attempt state is local to each call.

Defaults match Python: ten transient retries, 0.5..32s exponential base with
0..25% positive jitter, finite Retry-After seconds capped at 300s, and 300s total
outer wait. Shared model permits cover a call (and SDK waits), but not outer
backoff. Three overloads can select the configured fallback; selection persists
for subsequent session requests, while children/forks start with fresh override
state. The default does not configure a fallback.

Truncated replies can regenerate at 64K when the budget increases at least 1.5x;
listed SDK non-streaming ceilings cap that request. A refused unknown-model
escalation restores its budget and keeps the original partial. Up to three text
continuations return the entire answer with the final call's usage/identity. A
truncated reply containing tools returns for dispatch without an orphaning user
continuation. One reactive shrink preserves tool pairing and rebases retained
cache markers; it retries only if the outgoing surface shrinks and mirrors live
agent history explicitly. Internal summaries do not replace the live transcript.

The consumer-owned Recovery seam accepts concrete RecoveryInput/RecoveryServices.
Provider adapters expose typed protocol.ModelFailure evidence; custom Go errors
without that interface retain compatible overload/rate/context/streaming message
classification, but connection errors should expose a typed failure kind. Durable
recovery state, protected token-efficiency projections and advanced provider
variants remain pending.

### Control a managed session

`Steer(text)` parks input for the next round/turn and reports queue length.
The queue holds 100 entries, drops oldest, and caps each at 16,000 Unicode
characters plus a visible truncation marker. `ChangePermissionMode(mode)` updates
readonly/interactive/auto without blocking on the model. Permission evaluation
reads current mode after before/guard hooks; already made decisions and parked
approvals remain governed by their existing lifecycle. After the first run starts,
a real change queues the source meaning gloss in a separate posture_update. Initial
changes and no-ops are silent. Builtin control injections follow custom injectors
and precede context facts/compaction. Fresh children cannot drain parent controls.
TryRunWithSnapshot captures completion Info before releasing admission for a
completed HTTP response/cache. Info exposes live permission_mode and pending_steering.
Registered secrets mask
recorded delivery events; raw input reaches the live model history.

`SubmitSteering` (also `SessionManager.Steer(owner,id,text)`) implements HTTP wakeup.
On an idle handle it atomically publishes a background active holder, starts the
text as an ordinary default-untrusted turn, and returns DeliveryNewTurn. Busy
handles queue and return DeliverySteering. Manager deletion/shutdown owns joining
these holders; request disconnect does not. StopAccepting refuses late control
calls. A steer sent after the final round may wait for the next turn, matching
source. These controls are process-local: durable steering/restoration and
persist_error reporting remain pending. Posture notes retain source's uncapped
queue; the steering count bound does not describe posture state.

The twenty-second snapshot compares six actual Python control scenarios and six
HTTP responses. Targeted Go tests cover before-hook mode changes, Unicode/batch
bounds, concurrent control updates, child isolation, masking, simultaneous wakeups,
request cancellation and manager shutdown. They establish delivery, not obedience
by a live model.

### Fork a completed conversation

`manager.Fork(ctx, owner, sourceID)` returns a fresh scratch session initialized
with an independent copy of an idle paired transcript and named `ForkLineage`.
It inherits owner, explicit system and current permission mode; the manager's
default model, fresh Todo board and empty control queues apply. Workspace files
are not copied. Source admission pins only the detached snapshot; busy/open
boundaries are refused. Later source turns/idle wakeup can progress during child
provisioning without changing that captured history. Construction failure releases admission and cleans unused
scratch; shutdown joins pending creation. Successful forks emit `session_forked`
in the source stream, and child detail/listing exposes `forked_from`.

The HTTP route is `POST /sessions/{id}/fork`: ownership precedes rate budget,
busy returns 409 and success returns child Info. This implementation is
process-local. Python's configured StateStore flush before the first child turn
remains pending in Go.

### Run the standalone HTTP server

The binary embeds the same default `code_review` skill as `python/skills`, and
can run from outside the checkout with no Python interpreter. From this directory:

```sh
MINILOOP_FAKE_LLM=1 go run ./cmd/miniloop
# Or build, then run with the same explicit environment settings:
go build -o /tmp/miniloop ./cmd/miniloop
/tmp/miniloop --dump-config
```

`HOST` defaults to `127.0.0.1`, `PORT` to `8000` (zero selects an ephemeral port).
To use the real HTTP model adapter, supply `ANTHROPIC_API_KEY`, optional
`ANTHROPIC_BASE_URL` and `MODEL_ID` instead of enabling the fake client. No real
provider was contacted during port verification. Token auth uses
`MINILOOP_API_TOKEN` or owner bindings in `MINILOOP_API_TOKENS`; an unauthenticated
non-loopback host, blank host or actual wildcard listener is refused. Sessions
start in interactive permission mode. Shell execution uses the host, with no sandbox.

`config.Load` covers all 49 Python Settings fields with named enums, optional values,
integer limits and checked durations. An explicit environment map is isolated from
process configuration; nil reads process variables. Loading and `--dump-config` do
not create workspace directories. The launcher passes supported model/budget/depth,
concurrency, approval, Bash timeout, fallback, rate and workspace settings into owned
services, and captures `MINILOOP_FAKE_DELAY`. `MINILOOP_SKILLS_DIR` selects a
filesystem catalogue with the existing digest recheck; absent selects the compiled skill.
Configuration errors fail instead of guessing, including invalid inactive feature bounds.

Trajectory recording is enabled by default at `MINILOOP_TRAJECTORY_ROOT` or
`<workspace root>/.trajectories`; `MINILOOP_TRAJECTORIES=0` disables it. A configured
trajectory root failure refuses startup. The launcher best-effort constructs the separate private
spill store at `MINILOOP_SPILL_DIR` (default `./var/spill`; empty disables it).
A root construction failure disables preservation while startup continues, matching
Python. The launcher also refuses enabled
feature/workflow/guardian/decision/token-efficiency/AST integrations and configured
owner-resource/memory roots. Inactive optional settings remain typed and validated.
Ordinary workspace compaction artifacts remain separate from the private store.
The default structured Bash tool currently bypasses string-output preservation in
both Python and Go; see the preservation section below. No dependency was added.

`--dump-config` emits a **settings-and-availability** report with credential presence,
URL credential/query/fragment removal, build revision, process-local state and unavailable
activations. It creates no manager or listener and makes no provider probe; it does
not implement Python's full effective harness/tool posture. There is no `.env`
discovery/override or reload. Go additionally rejects nonfinite/overflowing numbers
and positive durations smaller than one nanosecond; representability limits differ
from Python's arbitrary integers/floats. Fake delay is checked even with a real client.

SIGINT/SIGTERM cancel HTTP request contexts and manager-owned background turns;
shutdown joins the HTTP server and manager under a ten-second independent timeout,
then closes the owned provider transport. Header reads are limited to five seconds,
idle connections to sixty seconds and headers to one MiB; no global write timeout
cuts off model calls or SSE. This is process-local serving, with optional browser data routes,
session restart/SQLite and optional fleet routes still pending.

The 28th source snapshot compares 64 actual Python Settings outcomes and the default
skill bytes/descriptions/load output. Real TCP tests cover authenticated tool execution,
foreign owners, actual bind refusal, active model/queued SSE shutdown, model options,
Bash timeout and side-effect-free inspection. A built binary was also exercised outside
the repository, including SIGTERM during a delayed call.

### Preserve oversized string-Bash output

`spill.Store.SaveText(context.Context, spill.Request)` accepts already-masked text
and returns a typed `spill.Ref` with opaque locator, byte count and retrieval hint.
`spill.LocalStore` matches the Python private root/new namespace permissions (0700),
exclusive leaf creation (0600), SHA-256 namespace grouping, safe filenames, random
16-hex prefix, exact UTF-8 content and 8,000,000-byte limit. Files are synced before
success; existing names or symlinks are never overwritten. A namespace is a storage
group, not an owner ACL; existing namespace permissions are retained as in Python.
Parent-directory races are not confined by leaf exclusivity. There is no TTL or
purge, and manager deletion/shutdown does not remove these artifacts.

Set `shell.Config.Spill` for direct use, or `agent.RuntimeConfig.Spill` /
`ManagerServices.Spill` for scoped composition. Runtime binding clones a real shell
executor without mutating a caller's executor, and preserves credential selection,
masking and process tracking. Forks use fresh workspace names; selected child tools
inherit the bound executor. Injected non-shell executors own their preservation policy.

`ExecuteBash` matches Python `Toolset.run_bash`: after full-stream masking, outputs
over 50,000 Unicode characters save their stripped captured text and append a
retrieval note. Save errors/panics keep the same preview. Capture bounds still apply;
this is the complete captured text, not an unbounded producer transcript. A Unicode
capture may exceed the store's byte limit and retain only its preview. Cancellation,
invalid UTF-8 and short-write checks plus best-effort cleanup of a failed leaf are
additional Go behavior. Cleanup checks inode identity before unlinking; the check
and unlink are not atomic against host tampering. Retrieval hints are text and
are not executed by the store.

**Actual default behavior:** Python's built-in Bash calls `run_bash_result`, and
Go's built-in Bash calls `ExecuteBashResult`. These structured surfaces currently
bypass the string preservation policy. The 29th actual-source snapshot checks both
interfaces and a managed Python tool round (zero preservation artifacts). It also
pins fourteen local-store cases, collision/symlink refusal, eight real shell cases,
three projection cases and three best-effort manager construction cases. This
source behavior is recorded as a gap, not advertised as default full-output storage.

### Record and read per-run trajectories

`trajectory.New(trajectory.Config{Root: path, CaptureContent: true})` creates a
private file store. Pass it through `agent.ManagerServices.Trajectories`; for a
standalone `ManagedSession`, use `agent.RuntimeConfig.Trajectories` (writer only).
Raw child/core Session instances do not own separate outer runs. Named Start,
Finish, Summary, Metrics and Query structures keep the runtime domain typed;
unknown historical JSON fields survive only at the serialization boundary.

Each run records masked input/metadata, full model requests/responses, full tool and
child text, ordered non-ephemeral events, and terminal metrics. These full fields are
private recording details, absent from the bounded live backlog. CaptureContent=false
applies Python's recursive content-key redaction. Recording faults do not fail the
model turn; Info and the terminal event report them. The persistence receipt is
published only after file finish, with terminal status/duration already recorded.

Authenticated GET routes are `/trajectories`, `/sessions/{id}/trajectories`,
`/trajectories/{id}`, and `/trajectories/{id}/export?format=json|jsonl`. Recorded
owners are checked before bulk reads. JSON inspection/export has an eight-MiB source
limit; JSONL streams without a client-held append lock and aborts a failed response.
Summary scans retain one capped line, not an event array. Files remain readable by
the recorded owner after server restart and ordinary session deletion. An explicit
library `DeleteSessionOptions{RemoveTrajectories:true}` drains the writer before purge.

New roots use 0700 and files 0600; reused modes match Python. There are 32 process-local
striped locks, a Go-specific 64 MiB record cap, and no file fsync or process lease.
Evidence persistence does not provide session restore or durable event catch-up.
The store retains at most 50 distinct diagnostic messages with occurrence/eviction
counts. The HTML viewer and filtered record visitor are implemented below. The thirtieth
source snapshot covers eight source files, seven managed cases, sixteen HTTP outcomes
and ten duration rounding boundaries. Tests add concurrent appends, masking, full
versus live text bounds, terminal publication ordering, stream failure, active purge,
JSON refusal, retained owner reads and default launcher activation. No dependency added.

### Inspect a trajectory as an HTML ledger

GET `/trajectories/{id}/view` returns the existing Python ledger design as a
self-contained page. Owner checks precede file size and full JSON reading; the
page shares the eight-MiB source limit and response security headers. Every input,
output, label and inspector field crosses the same escaping boundary. Source CSS
and filter JS are embedded without translating labels or substituting typography.

`traceview.Build` returns named Ledger/Row/Field/Metrics values; encoded JSON remains
transient at decoding. Span pairs fold once; child depth/agent labels remain visible,
request numbers cover normal and compaction calls, and parent step numbering excludes
child calls. Unknown events render generically. Reference payloads live in inspectors.
Unclosed spans show start markers and `in flight`. Totals are computed before keeping
the last 2000 rows; previews use 240 characters and inspector fields 20,000, with
explicit omission markers. Render accepts an explicit generation timestamp.

The offline CLI accepts an export file, trajectory ID or recorded session (up to
500 recent turns rendered chronologically), with flags before or after the target:

```sh
go run ./cmd/traceview /path/to/export.jsonl -o /tmp/run.trace.html
go run ./cmd/traceview SESSION_ID --root /path/to/.trajectories --output /tmp/session.trace.html
```

New output files use 0600; existing output modes remain unchanged like Python.
Missing file end records mean interrupted; malformed JSON marks partial, blank lines
are ignored and the last end wins. Operator-selected files have no whole-file HTTP
size cap, but retain the Go store's 64 MiB per-line limit. Operator reads do not claim
HTTP ownership admission, filesystem confinement or crash-durable writes.

`TrajectoryReader.VisitRecords(ctx,id,TrajectoryEventQuery,visitor)` streams detached
wire bytes. Nil Types means all record types (including headers/ends); an empty slice
means none. Limit counts yielded records, so a filtered query may scan the entire
file. Missing/unreadable files yield no records as in Python; cancellation and visitor
errors propagate. No append lock spans the visitor. Snapshot 31 compares eleven
actual Python complete HTML pages, exported-file assembly, nine iterator outcomes,
four HTTP page/error responses and exact CSS/JS hashes. No dependency was added.

### Persistent workspace task graph

`RuntimeConfig.TaskTools: true` or `ManagerServices.TaskTools: true` explicitly
adds create_task/list_tasks/get_task/claim_task/complete_task to the ten defaults.
The default `task` is child delegation; these five operate persistent task records.
The standalone launcher keeps comprehensive `MINILOOP_FEATURES` unsupported.
Library opt-in does not pretend the full optional bundle is available.

`tasks.New(tasks.Config{Workspace: path, Secrets: masker})` exposes named Task,
ID/Owner/Status, dependency, worktree and diagnostic records. A board is under
`.tasks`, initialized lazily by runtime tools. Only unblocked pending tasks can
be claimed; an exclusive `.owner` file arbitrates between processes. A stale
marker reports its holder/crash window and is retained for operator inspection.
Completion writes completed state before removing the spent marker. There is
no lease, automatic stale-marker takeover or general cross-process write lock.

Writes cap subject/description at 16,000 characters with a notice, mask the
structure before JSON escaping, fsync a fresh same-directory file and rename,
then best-effort sync the directory. Creation modes follow the process umask
like source. Go adds a 64 MiB record-read cap and uses 32 striped process locks.
List/render still scan the whole board; only display is bounded to 50 tail rows
and 200-character subjects. Missing dependencies are reported across all rows.
Diagnostics retain 50 distinct messages with total/eviction counts. Complete
records remain addressable by ID, and task files are not automatically pruned.

GET `/sessions/{id}/tasks` checks owner before fresh store access and returns
structured rows without descriptions or consuming claim markers. It works with
tool opt-in disabled, as Python does. New empty views may create `.tasks`.
Workspace sharing and operator library access are not host ACLs; host-edited
parent/symlink paths are not confined by this file store. Scratch deletion still
removes its board; retained/bound workspaces can reopen files across processes.
Typed decoding rejects unknown status and malformed non-string inputs; Python's
open dataclass coercions are not a Go domain contract. No dependency was added.

Snapshot 33 runs 53 source store steps, six actual installed-tool outputs,
five schemas/traits and five HTTP results. Go tests add real process contention,
readonly refusal before directory creation, argument cloning/masking/null identity,
manager/fork activation and fresh workspace isolation.

### Operator worktree lifecycle

`worktrees.New(worktrees.Config{Repository: repo})` provides named worktree and
audit records, `Create`, `Changes`, `Keep`, `Remove`, `List`, `PathFor` and
`WorkspaceFor`. A supplied `*tasks.Store` satisfies the typed TaskBoard seam:
create checks an existing task before Git, then binds its worktree and logs the
operation. Optional `*string` Base/BranchPrefix distinguish default values from
explicit empty overrides. Default paths/branches are `.worktrees/<name>` and
`wt/<name>`; names match `[A-Za-z0-9._-]{1,64}` excluding `.`/`..`.

Remove checks dirty files and commits ahead of the repository's current HEAD.
Unknown status refuses. Without explicit discard, Git receives ordinary worktree
remove and branch `-d`, retaining its own refusal even if work arrives after the
service check. Explicit discard selects `--force` and `-D`. Like Python, failed
branch deletion does not undo successful directory removal or erase the branch.
Audit events append after landed operations; they are not fsynced or an atomic
Git/task transaction. Later binding/audit failure leaves created work for inspection.

`WorkspaceFor` sanitizes session names, returns existing paths and falls back to
a plain directory on non-repositories, unborn HEAD or Git creation failure.
That source-compatible fallback provides no branch isolation. The embedding
caller owns retention and Git-aware removal. To run a session now, provision
explicitly, configure the manager's bindable roots and pass the resulting path
as `CreateSessionRequest.Workspace`; ordinary bound-workspace cleanup preserves it.
Do not attach this helper to raw scratch directory deletion. There is no shipped
model-facing worktree tool, live executor/workspace/sandbox rebind or managed
worktree cleanup adapter. Standalone feature activation remains unsupported.

Git calls inherit the source process environment, take concrete argv, have a
30-second context-owned deadline, bound each output channel to five MiB and bound
pipe cleanup waits. Go replaces malformed UTF-8 diagnostics and requires an
explicit repository; source open/coercion behavior is not a domain contract.
This operator capability is not a host path/symlink sandbox, owner ACL or lease.
Native Git must be available; no Go dependency was added. Snapshot 34 compares
nine real Python/Git scenarios, outputs, file/branch effects, task bindings and
audit events. Additional Go tests cover stale prechecks, partial failures,
duplicate creation, cancellation and a real owned session's tool write/delete/stop.

### Open the browser console

The standalone binary serves the original Python development console at `/` and
full interface at `/ui`. Both static documents are public; session data remains
authenticated. HTML/CSS/JS are embedded, self-contained source copies, preserving
source typography and interactions. No runtime checkout, Python process, build
step, external asset or filesystem static mount is needed.

Refresh and verify the embedded inputs from the repository root:

```sh
.venv/bin/python python/tools/export_go_webui.py
.venv/bin/python python/tools/export_go_webui.py --check
```

Core create/message/SSE/control/approval/trajectory flows have implemented APIs.
Optional Team/Goal/Cron/Workflows/Skills/Memory/Improve/Benchmark/Self-audit
panels still need Go services and currently display the source error states.
Serving the full interface is not complete UI feature parity. Snapshot 32 compares
16 Python HTTP outcomes and exact source assets; 37 real JS interaction regressions
run against the Go embed inputs. A built-binary browser pass verified core flows.

### Serve the implemented HTTP slice

Compose `httpapi.New` with a manager and `Authenticator`. Resolve token configuration
with `AuthFromEnvironment(nil)` or pass an explicit snapshot/bindings. Authentication
is admitted once; its owner flows into session operations and untrusted HTTP
RunContext. The following is an embedding pattern inside an application that already
owns `manager` and its lifecycle:

```go
auth, err := httpapi.AuthFromEnvironment(nil)
if err != nil { return err }
if err := httpapi.RefuseOpenBind("127.0.0.1", auth); err != nil { return err }
handler, err := httpapi.New(httpapi.Config{Manager: manager, Auth: auth})
if err != nil { return err }
server := &http.Server{Addr: "127.0.0.1:8000", Handler: handler}
// The application owns server.Shutdown(ctx) and manager.Stop(ctx).
return server.ListenAndServe()
```

For standalone startup, use `cmd/miniloop` above: it enforces requested and actual
listener policy and owns shutdown. For custom composition, the embedding application
retains that responsibility.
Custom Authenticators and Config.Now must be concurrency-safe. Config.Build defaults
to development; Config.FakeLLM is explicit rather than inferred from a provider.
The basic health response omits full effective posture and source build hashing.
Twenty-three method/path combinations cover create/list/detail/delete, completed message,
streamed message, cancel, approval list/resolve, event subscription and Null-store
transcript, public `/` and `/ui` shells, owned Tasks view, plus health, mode, steer, fork and five trajectory read/view/export operations. Foreign and missing sessions both return 404; non-streaming
messages reject busy turns atomically. Streamed messages queue; disconnect cancels
that submitted turn or its wait. Observe disconnect only unsubscribes. The event
wire is a named flat JSON union with sequence IDs and CRLF SSE framing.

All requests pass a ten-MiB admission cap before auth. Event endpoints alone permit
query-token fallback; headers win. Completed idempotent snapshots are scoped by
owner/session/key, capped at 1,024, and checked before rate budget. Optional fixed
minute rate windows are owner-scoped, capped at 4,096, and default off. Listing bounds
Info work to the latest 1..500 handles. Optional manager Secrets project typed JSON
and SSE data on output without rewriting live history; projection faults return no
raw fallback. The new twenty-first source snapshot compares actual finite HTTP/SSE
responses, with separate Go concurrency and real disconnect tests. The Python live
disconnect probe was checked separately from that fixture.

This slice uses process-local manager/backlog/cache/rate state, file-backed trajectories
and disabled workflow metadata. Mode and steer routes are active. Null-store transcript returns 404, current epoch zero, matching
Python; it does not expose a synthetic durable transcript. Full FastAPI validation
error arrays/coercions, optional browser data routes,
optional fleet routes, durable SSE gap recovery, SQLite are
pending. No dependencies were added.

```sh
cd go
go test ./...
go vet ./...
go test -race ./...
```

See [the plan](../GO_PORT_PLAN.md) and [parity matrix](../GO_PARITY_MATRIX.md)
for remaining work. Do not treat a compiling package as runtime parity.
