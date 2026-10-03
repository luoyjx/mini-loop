# Go implementation

This directory is the independent Go port of the Python runtime in `../python/`.
It is under construction. The httpapi package provides an embeddable HTTP/SSE
handler; a standalone agent CLI/listener remains pending. The
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
against Python. Future sink masking, team/plan/memory prompt sections,
and provider recovery remain pending. Compaction files persist, but cannot
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
inheritance, owner resources and remote provider transport remain open.
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
SQLite adapters, remaining HTTP surfaces, provider transport and session persistence
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
real provider transport and additional sink masking remain pending.

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
same saturated tool pool. Manager composition is implemented; environment settings
remain pending.
An open todo board receives `<reminder>Update your todos.</reminder>` after three
tool batches without an attempted TodoWrite. The counter persists across user
turns; child counters are fresh and a TodoWrite attempt resets it even if denied.

The nineteenth Python snapshot compares actual classified batch overlap/barriers,
ordered outputs and stuck hashes, six mode decisions, nil/empty prompt rewrites
and four cross-turn Todo counters. Deterministic Go synchronization tests cover
shared pool caps and waiting cancellation, exclusive bypass, completed-sibling
repair, worker joins, panic capacity release, atomic injector validation and
child seam/pool inheritance. Bounded steering and live modes are implemented below; provider transport/recovery
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
joined shutdown can finish. Durable restore, optional fleet services, fork,
durable steering and trajectories remain pending.

The twentieth Python snapshot compares initial status/defaults, shared services,
creation order, ten workspace outcomes, bound retention, scratch deletion and
stop. Go synchronization/race tests cover queued turns, deletion/shutdown joins,
construction faults, shared retiring paths, symlink reclamation and bounded
owner/diagnostic copies.

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

There is no Go startup command or listener policy enforcement outside this helper.
Custom Authenticators and Config.Now must be concurrency-safe. Config.Build defaults
to development; Config.FakeLLM is explicit rather than inferred from a provider.
The basic health response omits full effective posture and source build hashing.
Fourteen method/path combinations cover create/list/detail/delete, completed message,
streamed message, cancel, approval list/resolve, event subscription and Null-store
transcript, plus health, mode and steer. Foreign and missing sessions both return 404; non-streaming
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

This slice uses process-local manager/backlog/cache/rate state and disabled trajectory/
workflow metadata. Mode and steer routes are active. Null-store transcript returns 404, current epoch zero, matching
Python; it does not expose a synthetic durable transcript. Full FastAPI validation
error arrays/coercions, CLI/UI, forks, trajectories,
optional fleet routes, durable SSE gap recovery, real model transport and SQLite are
pending. No dependencies were added.

```sh
cd go
go test ./...
go vet ./...
go test -race ./...
```

See [the plan](../GO_PORT_PLAN.md) and [parity matrix](../GO_PARITY_MATRIX.md)
for remaining work. Do not treat a compiling package as runtime parity.
