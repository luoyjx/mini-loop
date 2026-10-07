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
SQLite adapters, remaining HTTP surfaces, session restoration
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

### Durable state contracts and archival decoding

`agent.SessionRecord` models the Python schema-v7 session projection: immutable
creation time, mutable run/status/todo state, tenant owner, pending steering and
bound-workspace flag. The event cursor is derived from event rows. Model, mode,
lineage and human execution authority are not fields in the Python session row.
`Clone` detaches pointer/slice data before a persistence consumer takes ownership.

The consumer-owned `SessionStore`, `TranscriptStore`, `EventStore`, `LeaseStore`
and `ApprovalReader` interfaces use concrete records and named IDs/epochs.
`LeaseOwner` is a process identity, separate from tenant `OwnerID`. Deleting
operational session/messages/events must retain action and approval audit rows.
An empty message append returns the requested epoch's count; message ordinals
continue across rewritten epochs. A nil epoch selects the highest stored epoch.
`StateStore` aggregates these with action/approval storage for explicit managed
session composition. The standalone app still has no state-store activation.

`agent.DecodeStoredEvent` reads the current flat writer projections into closed
event variants, bounded to 16 MiB per row. It requires a positive sequence and
epoch, a session ID and nonnegative depth. Tool inputs are decoded through the
existing closed protocol boundary, including its smaller input limits. Unknown
event/compaction variants fail explicitly; extra unused properties are ignored.
The rendered error string is retained; its original in-memory cause category is
not available in the flat row. System-prompt strings and the current single
cached text-block shape are supported. The existing writer determines ephemeral
progress markers and canonical approval-refusal reasons on re-encoding.

Stored message lineage remains informational: the decoder assigns untrusted
authority, no actor and no approved capabilities. Reading approval-grant events
does not install grants into a broker. This decoder does not execute a turn,
restore a session, enable durable SSE or implement a SQLite backend.

Snapshot 44 executes Python SQLite directly: two connections, epoch history,
provider-object serialization, transaction rollback, leases at exact expiry,
upsert preservation, explicit unknown transitions, audit-preserving deletion,
concurrent append, v1 migration and future/corrupt-file refusal. Go checks those
record/message shapes and all current writer variants, including a real tool
loop and malformed/authority-bearing archival rows. SQL outcomes currently
remain Python evidence. Live injection is now available through
`RuntimeConfig.StateStore` and `ManagerServices.StateStore`; optional
`StateLeaseOwner`/`StateLeaseTTL` and manager-generated process identity preserve
the tenant/lease distinction. A manager defaults to a stored journal and approval
rows, while preserving explicit overrides. The caller owns backend close.

Transcript growth is flushed before provider calls, versioned after prefix
replacement and projected through the registry without mutating live content.
Session-row explicit system and Todo data follow the source projection; masking
is applied to transcripts, events and queued steering rather than every metadata
field. The current epoch count is checked before a request. Events write before the
subsequent flush; a rewrite event retains its old epoch stamp, while the next
event uses the new epoch. Ephemeral events consume live sequence IDs only.
`PersistenceStatus()` reports configured state, masked sticky write faults and
lease confirmation history; confirmation alone is not a current holder query.
Ordinary write errors/panics degrade; invariant/query failures stop a request.
Confirmed renewal loss cancels the turn before later model/tool admission.

Claims occur after serialized turn admission. Queued steering is saved before
acknowledgment. Delete disables later writes before deleting rows/cancelling work;
stop releases leases after drain. Cleanup errors are reported without preventing
remaining cleanup. Renewal and session row refresh require transcript growth,
so stored status can remain `running` after final text has already been flushed.
The default manager journal explicitly marks all started actions unknown, as in
Python; this policy is not proof that a foreign process is dead or external fencing.

Snapshot 45 runs actual Python AgentSession over SQLite to pin guard/capture,
masking, epochs, metadata timing, ephemeral sequence/ordinal differences and
confirmed/unconfirmed renewal loss. Go runs the same recipe and failure outcomes
against a test-only backing. Driver approval, backend execution
and durable SSE/launcher activation remain open; passing injection tests does
not establish physical durability.

`manager.RestoreSessions(ctx)` is an explicit library operation over the supplied
backend. It restores recorded tenant/workspace/system/run/status, the highest
transcript epoch and immutable prefix references, Todo and steering. Missing saved
workspaces are recreated; the new-session workspace factory is not invoked.
Handles start interactive with no active turn, including recorded `running`
status. Restore neither installs grants nor activates historical work. The next
sequence exceeds both physical cursor and stored payload sequences.

Claims precede expiry of pending approvals and crash-tail repair. Expired parked
calls receive the canonical not-run result; other unanswered calls receive unknown
results. Bare user tails receive the interruption marker. Repairs append in the
same epoch; Todo/steering are installed first so the write preserves metadata.
A foreign-held handle reports `RestorePending` and refuses parked steering; turn
admission can claim, reload the latest facts, then finish repair. Failed reloads
release the acquired lease and remain pending. `PersistenceStatus` also reports
`Restored` and a detached list of `RepairedToolUses`.

Restore read/repair errors prevent publication. Existing historical rows and
workspaces are retained, including any partial writes; no fleet transaction or
repair rollback is claimed. Stop waits for restore and refuses late publication.
Already live IDs are skipped, while reserved/retired IDs fail. Source snapshot 46
captures seven actual Python/SQLite cases and its repair-time Todo/queue overwrite,
physical sequence reset and repair-before-claim behavior; Go explicitly fixes
those three boundaries. Tests make the first resumed model request and check
pairing and persistence-before-request. The test backing does not prove SQL,
restarts, concurrent connections or cross-process fencing. Plan/goal event
variants remain unsupported. `RestoreScheduledSession(ctx, id)` is the privileged
stable-ID resolver used by cron. It returns live handles unchanged. Saved bound
rows retain the recorded workspace; saved scratch or missing rows use the current
factory (the default is WorkspaceRoot/id). Scheduled construction uses the current
system builder and does not reinstall saved explicit system, matching Python.
Saved rows restore owner/history/run/status/Todo/steering. Missing rows produce
an anonymous handle with no history; a missing-row claim refusal leaves it pending
and prevents any turn/upsert. No-store fallback can run ephemerally.
A successful growth upsert advances the expected stored projection, so retry after
partial repair can accept its own selected workspace/system while refusing a
foreign identity. Restore requests share a lifetime cancellation context; manager
Stop cancels cooperative reads and joins before lease release/return. Snapshot 47
executes actual Python manager/SQLite-or-Null restoration and the next model call.

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

### Typed decision operator library

`go/decisions` evaluates explicit state into `choice`, `score` and `noul`
judgments. `Request`, `Question`, `Answer`, `Result`, `TokenUsage` and `Provider`
are named types. The JSON Value domain contains only null, string, number,
boolean, array and object variants; it stores no `any` or raw JSON. Containers,
questions, answers and optional usage are detached on construction and access.
JSON is decoded at a strict boundary with duplicate-key, UTF-8, nesting and byte
checks. Empty state is allowed; instructions must be nonempty. Question IDs and
answers must match, probabilities must cover the requested criteria and sum to
one, choice must select a maximum, and score/legend must match the exact rubric.
Jev confidence is retained; noul gets no synthesized confidence.

```go
q, err := decisions.NewNoul(decisions.StringValue("Is a workaround explicit?"))
if err != nil { return err }
request, err := decisions.NewRequest(
    decisions.StringValue("CSV export failed; JSON export still works."),
    map[string]decisions.Question{"workaround": q},
)
if err != nil { return err }
backend, err := decisions.NewJev(decisions.DefaultJevConfig(apiKey))
if err != nil { return err }
result, err := backend.Evaluate(ctx, request)
```

Jev is an explicit operator library; the calling application selects the data
and authorization. Construction is offline. Evaluation uses the source fixed
`https://api.typesafe.ai/v1/systemone`, never follows redirects, and never falls
back to another backend. Defaults are model `jev-latest`, total timeout 20 seconds
and two retries; explicit configuration allows (0,120] seconds and 0..3 retries.
Only HTTP 429/529 retry; numeric Retry-After values cap at two seconds and invalid
values use bounded exponential fallback. Response bodies cap at 512 KiB while
reading. Transport/status/malformed-JSON feedback omits response/credential data.
The actual returned model, provider `typesafe`, probability source `jev` and
complete token usage remain explicit. Owned clients ignore environment proxies
and close idle connections. Borrowed clients/transports remain caller-owned;
redirect policy is applied on a private client copy.

Snapshot 53 compares 28 actual Python request cases, 22 result cases, four UTF-8
budget boundaries and 21 isolated HTTP recipes. Native tests add detached
constructor/accessor snapshots, surrogate/duplicate/nonfinite refusal, exact
integer/float legend equality, total deadline/cancellation, early read bounds,
real local HTTPS redirects and concurrent independent calls. No paid Jev request
is made. Go usage uses signed-64 counts; Python accepts unbounded integers. Go
refuses duplicate local request keys, nonstandard nonfinite JSON and excessive
raw/ignored response nesting before domain decoding. Map ordering is canonical,
so byte-for-byte object key order is not a compatibility promise.

The runtime can explicitly install this tool with `RuntimeConfig.DecisionTools`
or `ManagerServices.DecisionTools`. `DecisionProvider` selects a custom/Jev
backend; when omitted, a fresh isolated current-LLM query is used. Supplying a
backend alone does not install the tool. The ten default tools remain unchanged.

```go
config := agent.RuntimeConfig{
    DecisionTools: true,
    DecisionProvider: backend, // an existing decisions.Provider; nil selects LLM
    DecisionLLM: agent.DecisionLLMConfig{MaxOutputTokens: 4096},
}
// Pass config to NewRuntimeSession. Managers use the same fields in ManagerServices.
```

The external-risk gate preserves before/guard/permission/execute/after/observer
order, readonly refusal and interactive approval. Explicit state and description
strings/member names are masked and revalidated before either backend. Both
backends share the session model limiter; custom providers receive detached
requests and must honor cancellation. Total tool timeout is 60 seconds.

The LLM query has one explicit user JSON message, the source system prompt and
no tools, parent transcript, cache policy, stream or parent meter. Recovery is
shared, with isolated model fallback and actual served model/usage. Only complete
end-turn text/thinking/opaque-thinking replies are accepted; text alone feeds
bounded strict estimation. Response key order determines ties and numeric folds.
Confidence is normalized entropy, not calibrated correctness. Zero LLM settings
select 60 seconds and 4096 output tokens; negative values fail construction.
`NewLLMDecisionProvider(parent, run, config)` pins explicit run provenance.

Typed decision events carry metadata; full masked inputs remain private trajectory
fields. Recording-only masked tool projections cannot execute or acquire action
identity. Native tests retain and replay a full result larger than 4 KiB without
another provider call; replay still crosses current guards. Snapshot 54 compares
31 actual Python LLM recipes and eight common-gate outcomes, including masking,
provider faults, small custom model events and parent isolation.

Native reply contracts require complete usage/model; Python permits missing usage.
Go refuses lone surrogate JSON earlier, and unknown backend failures use sanitized
`RuntimeError`. Derived floats compare within 1e-12; raw probability spelling is
retained. The standalone launcher now accepts `MINILOOP_DECISIONS=off|llm|jev`.
Jev requires `TYPESAFE_API_KEY` and uses `MINILOOP_DECISION_MODEL` (jev-latest by
default). `launcher.Options.DecisionProvider` overrides a selected backend; alone
it leaves off-mode disabled. With environment off, `DecisionTools` explicitly
selects LLM when no backend is supplied. `--dump-config` reports off/llm/jev/custom without I/O or
credentials. FakeLLM does not substitute a fake Jev backend.
Snapshot 55 compares actual source memory/SQLite-reopen large-result replay and
aggregate shedding. Native gate tests retain exact maximum-size bytes, replay
from a fresh session, retain three of five maximum results under the shared
budget, and preserve shed action identities without reexecution. Unicode bounds
and recorded-tool reconciliation match source. Recreated stored adapters use a
test backing. Snapshot 56 compares cooperative cancellation, cancelled action
settlement and permit release. Go also masks the closed backend result before
JSON escaping, covering backend model strings with quote/Unicode secrets. Managed
tests inspect nested decoded results across live/SSE, EventSink, journal, stored
messages/events and private trajectories. This closes a source escaped-output
masking gap recorded by the negative recipe. Native SQLite/restart and live
provider verification remain pending.

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
comprehensive feature/workflow/guardian/token-efficiency/AST integrations.
Implemented decision providers and memory roots have independent selections. Inactive optional settings remain typed and validated.
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

### Operator background commands

`background.New(background.Config{Shell: shell.Config{Workspace: root}})` creates
an explicit library service. `Run(ctx, Request{Command: command})` returns a typed
Started value immediately; `Started.Render()` matches source text. The caller
context controls admission only. Tasks have independent contexts and Unix
process groups; use `Wait`, `Get`, `Check`, `LiveCount`, `Drain` or `DrainBatch` to
observe results. `CancelAll` returns joinable handles; `Close(ctx)` cancels and
joins the current handles, with the caller responsible for admission quiescence.
As in Python, a later Run remains allowed. Foreground Interrupt excludes them.

Commands pass the same case-sensitive typo blocklist, sandbox argv, scrubbed
and selectively injected environment, whole-stream masking and group cleanup
as foreground commands. Background merges stdout/stderr into one native pipe
and bounds **bytes**, retaining raw newlines and Python maximal-subpart UTF-8
replacement. Foreground counts decoded characters and normalizes newlines.
Default capture is 5,000,000 bytes, default timeout is 300 seconds. `Request.Timeout`
is optional; explicit zero uses the default and negative is an immediate timeout.
Finished nonzero exits remain Completed with visible exit text; timeout/spawn
faults are Error. Rendering preserves the 50,000-character tail recipe.

`Config.MaxResultsRetained` nil means 100, explicit zero sheds every full result,
and negative is refused. Shed task records still answer with the source marker;
completion notifications keep independent text until drained. No-argument Check
lists the newest 50 tasks. DrainBatch keeps the latest 50 notifications and names
the omitted count. Metadata and undrained notification queues remain unbounded,
as in source; this is not a globally bounded task store.

The original workspace `.background` ledger records masked 200-character
command previews, PID and start time before/during execution. Writes are atomic
best-effort private leaves without fsync or a host ACL; a failed initial write
adds the source unrecorded warning while execution proceeds. Terminal completion
and cancellation remove the record. On construction, matching records become
Orphaned, the counter reserves numeric IDs and the ledger is removed. PID
liveness is informational, with no ownership/PID-reuse proof; Close never signals
adopted PIDs. No restart replay, process adoption or session restoration occurs.

Rebind prepares a fresh executor before publishing future cwd/sandbox. Admitted
commands keep the prior executor; the ledger stays at its original root, matching
source's pinned `_ledger_dir`. Go additionally publishes deterministic cancelled
metadata and cleans the ledger even if cancellation precedes process start;
actual Python cancellation before coroutine entry leaves Running and a record.
Go also bounds orphan reads to one MiB, rejects malformed field types, catches
start observer/worker panics without secret-bearing panic values, and refuses
negative retention. Numeric ledger filenames reserve IDs using Python Unicode
14 decimal digits, including mixed scripts and arbitrarily large counters. Other
numeric characters such as superscripts remain reportable orphans; Python can
raise ValueError while adopting them. No lease is inferred.

`RuntimeConfig.BackgroundTools: true` enables two additional native-session
model tools through the common gate: background_run (exec) and check_background
(read). Named inputs preserve absent/null/zero values, detached optional pointers,
canonical action/step identity and masked recording copies. Timeout is optional
integer seconds; Go rejects values outside its native duration range instead of
overflowing. Both shell tool names use the same immutable command deny list,
destructive-command approval and validated remembered prefix logic.

Activation requires a real shell.Executor bound to the session root. Per-session
state constructs its manager lazily, including when an existing nonempty ledger
must surface without a tool call. No caller-supplied shared manager is accepted.
Enabled Bash uses explicit true or the source slow-operation heuristic to enqueue;
only the explicit true flag changes scheduling to parallel. Other Bash calls remain
exclusive and preserve typed foreground metadata. Disabled activation keeps both
foreground behavior and the default ten-tool catalogue.

Before model requests, completed work becomes a bounded user task_notification
batch and typed background_result count/dropped event. The injector consumes the
queue once, keeps the newest 50 and leaves omitted IDs queryable. Operator
interruption markers name tasks still running, except when repaired tool results
must remain last. Turn cancellation never closes background task ownership.

Workspace entry prepares the native shell once and publishes it to the background
state alongside the other execution bindings; in-flight work and original ledger
stay pinned. Default child roles omit the two capability-empty tools and retain
native foreground Bash with fresh state. Explicit role selection of background
tools now binds independent child state, including a check-only catalogue without
foreground Bash. Source readonly rules still deny Explore execution.

Each selected child uses a `bg_<scope>_<counter>` identity: scope is the 64-character
SHA-256 of its newly derived peer message identity. Root IDs remain bg_0001 etc.
The parent adopts preexisting root ledger evidence before child admission;
`background.NewScopedWithExecutor` accepts only a bound executor and a validated
lowercase hexadecimal Scope, and never adopts the shared root. This preserves
both live parent and child records and independent queues/counters. The source
counter format differs deliberately for children to avoid same-root collisions.
Root startup adoption reports qualified records as ordinary orphans without PID
control; source root numeric counters are unchanged.

Child tasks can outlive the returned summary, as in Python. Parent ownership
retains their scopes and descendants for CloseBackground / manager cleanup;
cancellation is requested for the entire tree before any handle is awaited.
Checks, completion queues and interruption live counts remain local to each
scope; the parent model does not automatically receive a returned child's queue.
Retained child states/queues are not globally bounded. This is process-local
lifetime ownership, not host ACLs, a lease or arbitration between separately
bound sessions/processes in one checkout.

Snapshot 40 proves an actual source gap with inherited harness injectors: a
child, including a default role without background tools, adopts the live parent
as an orphan and unlinks its ledger while that parent keeps running. Selected
child work also survives return without an automatic source close. Go preserves
continuing child work while retaining a reachable cleanup owner and avoiding
live-parent adoption. Default children never construct background state.

Bare library callers quiesce admission then call Session.CloseBackground or
ManagedSession.CloseBackground to cancel/join ownership. ManagerServices can
select BackgroundTools for the fleet; every session and completed-boundary fork
gets independent lazy state and a fresh executor/root. Delete/stop first revoke
admission and drain the active turn, then close/join any created background
service before reclaiming workspace/recordings. This includes a service first
created during the grace window. Idle background owners use asynchronous tracked
cleanup, and Stop also waits for retiring sessions. Bound and preserved roots
remain intact; stopping retains scratch as source does.

The standalone executable accepts `--background-tools`; embedding callers use
`launcher.NewWithOptions(..., launcher.Options{BackgroundTools: true})`.
`--dump-config` / InspectWithOptions report that explicit choice without starting
services. This is an individual Go service option: MINILOOP_FEATURES continues to
refuse the unimplemented comprehensive bundle. Defaults remain ten tools.

Snapshot 38 captures codecs and 15 real gated source calls; snapshot 39 adds five
actual source manager deletion/stop/fork ownership scenarios. Native Go checks
cover a still-draining turn, lazy creation after admission closes and shutdown
through real HTTP serving with a local scripted provider. No production model
endpoint is called. Snapshot 40 adds six actual source child/catalogue/readonly
scenarios and native Go scope/queue/ledger/lifetime comparisons, including nested
ownership and orphan evidence from qualified IDs.

### Operator cron scheduler

`cron.New(cron.Config{Resolver: resolver, DurablePath: path, Secrets: registry})`
binds the operator library explicitly. Job, SessionID, ID, Request, Invocation,
Problem and Expression are concrete types. The Resolver resolves/restores a
session through `ResolveScheduled`; the Runner consumes `RunScheduled` with
read-only prompt/session accessors and an always-untrusted authority accessor.
The embedding adapter must create a fresh untrusted runtime context. The manager
now supplies that adapter and owns the service by default, matching Python even
with the comprehensive feature bundle disabled. Durable session/lease restoration
remains pending; the default tool catalogue still contains ten tools.

Parse matches Python's five-field ranges/lists/steps, Sunday 0 and restricted
day-of-month/day-of-week OR rule. Numeric parsing pins Unicode 14 decimal digits,
signs/underscores and Python's 4,300-digit limit; whitespace includes the ASCII
record separators that Go Fields omits. The same decimal table now supplies
background orphan counters, with their earlier 74 source cases unchanged.
The supplied local civil minute is the deduplication key; no catch-up or timezone
conversion is added, and repeated local minutes share the source marker.

Schedule caps prompts at 8,000 Unicode characters and jobs at 200, preserves live
raw prompts and masks only their stored copies. Per-session cancel/arm answers
foreign IDs exactly like unknown IDs. ArmAll is an operator act and activation
is never stored. Loaded valid jobs remain disarmed. CancelForSession removes
future occurrences; it does not interrupt a run already dispatched.

Durable occurrences create private O_EXCL claim files; losers consume the minute
locally and stay quiet. Winners mark the occurrence and remove one-shot jobs
before the atomic temp-file/fsync/rename/directory-sync save, then resolve/dispatch.
An error never dispatches the occurrence; the claim/consumed memory state remains
as source does. Only successful saves prune the preceding claim. Cancel removes
the last claim. Source-compatible stale whole-file state remains between live
writers: this is not a transactional job database, external exactly-once effect,
host lease, stale-claim reclamation or replay protocol. Store reads/writes are
additionally capped at eight MiB and files are private; no host symlink sandbox
or multi-file commit is claimed. Typed decoding rejects malformed field types;
Python's dataclass can retain some malformed scalar/null fields. Valid rows and
dataclass defaults match, duplicate IDs replace in place, unknown fields and bad
cron rows are diagnosed individually. Source non-array JSON yields an empty store.

Go has no ambient asyncio loop: `Start()` explicitly owns the immediate tick and
20-second ticker. Schedule alone does not start it. `Tick(time)` is the operator
and deterministic test path. Stop revokes the current admission generation,
cancels ticker/runs and joins them; expired observers can resume joining, and
later explicit Start is supported. A delayed resolver cannot dispatch an old
generation after Stop. Resolver callbacks may inspect scheduler state; masking
callbacks run inside persistence and must not reenter the scheduler. Runner
errors/panics and resolver/persistence panics are reported, a Go addition to
source's asynchronous task failures. Problems retain 50 distinct entries with
occurrence/eviction counts; state snapshots are detached. Active runs and source
armed metadata are not globally bounded. Callers quiesce admission before Wait
or final lifetime cleanup.

Snapshot 41 compares 27 actual source expressions, 14 operator states, one-shot
removal, two stale claimers, three claim/save loss cases, masked restoration,
prompt/job limits and actual source untrusted dispatch/missing-session diagnostics.
Go tests additionally run two independent processes sharing the store and cover
native filesystem failures, ticker cancellation/resumed joins and delayed
resolution. Operator evidence alone does not complete managed runtime validation,
restoration or G7.

### Managed cron lifecycle

`SessionManager.CronScheduler()` is a privileged operator surface without owner
checks. User-facing callers instead use `ScheduleCron(owner, session, request)`,
`CronJobs`, `CancelCron` and `ArmCron`: session ownership is checked first and
foreign job IDs read like missing ones. Inputs and list rows are concrete types;
callers cannot choose an authoritative session through ScheduleCronRequest.

The default service stores masked durable jobs at `<WorkspaceRoot>/.cron.json`.
Construction loads jobs without starting goroutines. `SessionManager.Start()`
starts existing jobs while retaining their disarmed state; standalone Serve calls
it after listener admission. An admitted owner schedule starts the ticker,
including a job retained in memory after a save failure. Invalid requests never
start it. These managed rules are distinct from raw operator Schedule, whose
Start is explicit. Arm remains an operator action.

Resolution reuses a live session or lazily calls RestoreScheduledSession inside
the scheduler-owned cancellation context, then calls Run with a fresh default
untrusted context: no scheduling actor, approved capabilities or parent message
identity is reused. The ordinary session queue, common gate, shared pools and
trajectory capture remain in force. A fork shares the service but has no copied
jobs. Missing-row lease refusal and stopped/faulted restoration produce bounded
diagnostics; native Go SQLite backend/restart proof remains pending.

Owner mutations and manager deletion/stop share an admission lock. Delete removes
future jobs before drain and scratch reclamation; save failure is recorded in
CleanupErrors and cleanup continues, a Go addition to Python's propagating error.
Stop revokes admission, cancels pending restore reads and drains managed turns
before cancelling/joining ticker and runs. Queued jobs cannot reenter, native foreground shells are reaped, and an
expired Stop observer can resume the join. Preserved/bound workspaces and normal
stop retain files. Cron masking callbacks must not reenter either scheduler or
manager cron methods while persistence holds their locks.

Snapshot 42 compares actual source authority, delete/preserve/bound/fork/stop
states and a live cancelled scheduled turn. Additional Go tests exercise foreign
owner refusal, detached lists, closed admission, a queued scheduled turn, delete
save failure, real native-shell cleanup and restored-disarmed launcher startup.

### Cron tools and operator HTTP

`RuntimeConfig.CronTools` installs three concrete protocol inputs and source
schemas: schedule_cron (write), list_crons (read/readonly), cancel_cron (write).
All are exclusive, without default child capabilities. A typed CronControl seam
binds established owner/session identities; model arguments cannot select either
identity and Arm is absent. ManagerServices.CronTools binds the manager service.
Standalone `--cron-tools` / launcher.Options.CronTools selects these tools alone;
MINILOOP_FEATURES remains unsupported. Construction/inspection never starts a
listener or activates restored jobs. The default tool catalogue remains ten.

Calls cross the existing rewrite/guard/permission/journal/masking path. Optional
booleans distinguish absent defaults from explicit false and detach on copy.
The model decoder rejects null/nonboolean values against the advertised boolean
schema; Python's kwargs handler is more permissive for schema-invalid values.
Forks preserve activation with new job scope. Selected child handlers have no
CronControl and report source unavailability instead of borrowing parent state.
Raw library callers can bind their own typed implementation; they own admission.

Four operator method/path operations are always registered, independently of
CronTools: GET/POST `/sessions/{session_id}/cron`, DELETE
`/sessions/{session_id}/cron/{job_id}`, and POST
`/sessions/{session_id}/cron/{job_id}/arm`. Authentication precedes route work;
all manager reads/mutations admit session ownership and hide foreign jobs. HTTP
schedule validates its required nonempty fields and 100-character cron limit
before owner lookup like FastAPI. It preserves Pydantic boolean strings/0/1 and
rejects null/other values. Error status parity is tested; full structured 422
validation detail remains an existing open HTTP boundary. Domain refusals are
400, hidden jobs 404, closed manager 503 and persistence faults a masked 500.

Operator Arm changes process-local authorization only. Save/reload cannot retain
activation, and future runs still use the fresh untrusted managed context.
Snapshot 43 compares three actual tool contexts (24 gate calls), six canonical/
grant variants, three schemas/traits and 29 real Python HTTP requests. Go tests
also cover before rewrites, retained raw prompts/masked stores, replay without a
second schedule, foreign authority, a real model tool round, fresh fork/child
scope, restored HTTP authorization with no stored activation and local TCP
launcher-to-provider-to-HTTP composition. No paid provider is contacted.

### Plan mode tools and logged guidance

Select `RuntimeConfig.PlanModeTools`, `ManagerServices.PlanModeTools`,
`launcher.Options.PlanModeTools` or `cmd/miniloop --plan-mode-tools`. This adds
`enter_plan_mode` and `exit_plan_mode` with named `ExitPlanModeInput{Plan}`.
Both stay in the fitted catalog while off; both are exclusive read-risk tools.
The default tool set stays ten. Full feature activation remains unsupported.

Entry sets the active boolean and emits `plan_mode` even when already active.
Exit requires active planning and full Markdown beginning with `#` after Python
whitespace trimming. Optional `PlanApprover` reviews `PlanReviewRequest` with
bound `ToolAuthority` and returns `PlanReview{Approved, Feedback}`. Rejection is
source error text with feedback and failed=false/completed journal status.
Inactive/invalid-plan refusals use the same outcome. Callback/hook errors remain
failed=true; review errors/panics/cancellation keep active state. Nil
uses source headless auto-approval. The CLI uses this headless path; the normal
approval broker is not implicitly a plan reviewer.

`DefaultSystemBuilder` appends the exact source `PlanSection` during planning.
Custom builders receive `SystemContext.PlanMode`; a fixed system remains fixed.
Plan state never changes the permission/sandbox policy. Auto can still mutate;
Readonly still denies writes. The envelope changes with the default prompt,
while schemas stay stable. `Session.PlanModeActive()` is safe during callbacks;
shared reviewers must synchronize state, honor cancellation and avoid reentry.

A concrete boolean event survives archival decoding, masked capture, trajectory
recording and stored-event SSE. Restore folds the last logged value, including
pending claim reload; a new fork starts inactive. Selected child handlers have
fresh state and bound child authority; default role profiles omit these tools.
Like Python, restoration folds all logged plan events including child scopes;
that restored value is guidance, with no recovered reviewer or human authority.
Go rejects absent/null/nonboolean archival `active` values; Python's general fold
uses truthiness, while the actual source tools always emit booleans.

Snapshot 50 compares 35 actual source gate calls across headless, readonly,
approved, rejected and fixed-prompt installations, plus two actual Python SQLite
restores and their next requests. Go tests actual model-round prompt changes,
event-before-request persistence, failure/cancellation, masked feedback, owner
binding, fresh forks/children and local TCP launcher composition. Runtime storage
tests use injected memory backings; native SQLite and SQL restart remain pending.

Snapshot 50 also pins all 35 source failed/denied flags. Snapshot 52 runs five
actual Python manager/Agent loops with real JSONL recording, custom result
observers and memory journals: reject, approve, reviewer fault, before denial and
after fault. Native Go matches journal settlement before observers, live/stored
result flags, stuck ledger flags, model result blocks without is_error, aggregate
tool-error metrics and same-action replay. Refused plans do not count as tool
errors; callback faults and denials do. Replay of a stored failed call returns
its text with fresh failed=false unless a current post hook itself faults;
current permission/guard denials still win before replay. No new permission or
reviewer authority is introduced by text outcomes.

### Goal tools and bounded continuation

Select `RuntimeConfig.GoalTools`, `ManagerServices.GoalTools`,
`launcher.Options.GoalTools` or `--goal-tools`. The five tools are `goal_create`,
`goal_status`, `goal_complete`, `goal_block` and `goal_resume`; the default catalog
remains ten. Inputs, revisions, phases, reasons and logged snapshots are concrete
types. Signed 64-bit revisions fail on overflow; provider JSON is strict, unlike
Python's coercions and unbounded integers. Omitted/null/zero max_rounds use ten;
valid caps are 1–100. Goal codes preserve actual Python acceptance of repeated
or trailing hyphens and one trailing newline, despite the lower-kebab-case label.

Create/resume require explicit human authority stamped by a trusted caller via
`ExplicitHumanRunContext`. Normal HTTP authentication, cron and child delegation
remain untrusted/peer. Other mutations use revision CAS without arming. Refusals
are textual results with source `failed=false`; permission denials remain denied.
A completed goal can be blocked again, matching the shipped source behavior.
The source ships no edit/clear/pause tools; clear remains a restore tombstone.
Cap exhaustion text still refers to the source's nonexistent `goal_edit` tool.

Nil StopHooks installs the stateless default `GoalContinuation`, inert until
an active goal is armed. Any nonnil list replaces it, including an empty list.
Custom lists may include `GoalContinuation{}`. Only stop-sourced continuation
consumes budget; tool rounds do not. Counts increment before stuck/global-loop
limit decisions, so they count requested continuations rather than guaranteed
model requests. Continuations retain the current source turn's RunContext.

`Session.GoalSnapshot` and `ManagedSession.GoalSnapshot` detach state for readers.
Owned GET `/sessions/{session_id}/goal` returns goal, goal_armed and plan_mode;
it never arms or starts a turn. Registered secrets mask the HTTP/event projections
while live state retains original text. Whole `goal_change` snapshots reach
trajectories and configured capture/SSE. Restore folds all logged scopes and
clear tombstones; it always disarms, including pending claim reload. Forks and
selected children own fresh goals; default role policy excludes capability-free
goal tools. An explicitly selected child still has peer authority.

Snapshot 51 compares 46 actual gate calls and six direct stop calls across Auto
and Readonly, the default/custom actual Agent loop, real Python SQLite restore
and resumed requests, and four owner/auth HTTP views. Go tests CAS concurrency,
stop replacement, bounded model requests, event-before-request capture, disarmed
restore/reload, detached masking, fresh child/fork state and real TCP launcher
selection/refusal. Memory test backings do not prove Go SQLite durability.

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
That source-compatible fallback provides no branch isolation. To use the source
managed factory, construct `agent.NewWorktreeWorkspaceFactory(service)` and set
`ManagerConfig.WorkspaceFactory` to that concrete adapter. Provisioning is explicit
and does not install model tools or activate standalone feature flags.

Factory paths are managed scratch (`WorkspaceBound=false`). Matching actual
Python, delete drains current work and waits for shared scratch holders, then
removes the directory even if dirty. Git registration and the branch remain;
manager cleanup does not call the guarded worktree Remove API or append its audit.
`DeleteSessionOptions.PreserveWorkspace` and manager Stop retain the directory.
For operator-owned retention, provision explicitly, configure bindable roots and
pass `CreateSessionRequest.Workspace`; bound cleanup preserves it. Source has no
Git-aware manager cleanup, so that behavior is a recorded gap rather than a
missing port. The existing Go manager additionally reclaims unpublished scratch
on construction failure; Python create leaves it allocated. This Go failure
cleanup also leaves Git registration and branch. Standalone feature activation
remains unsupported.

For model tools, explicitly set `RuntimeConfig.WorktreeTools` or
`ManagerServices.WorktreeTools` and supply `Worktrees: service`. This installs
`create_worktree`, `remove_worktree`, `keep_worktree`, `list_worktrees` and
`enter_worktree` through the common gate; without a service their source response
is `Error: worktree repository is not configured`. Defaults remain ten tools.
Named create/remove/name input variants preserve omitted versus explicit-null
task/discard fields. Only explicit true authorizes discard.

`enter_worktree` is an exclusive tool barrier, including when a custom classifier
requests parallel execution. It prepares replacement files, shell/sandbox,
catalogue and broker approval/question surfaces before publishing execution
scope. Later tools in the same batch, model context and fresh children see the
new directory. Preparation failure keeps the prior execution bindings. Manager
Info, task HTTP reads, trajectory attribution and scratch reclamation keep the
original lifecycle workspace; deletion does not erase entered worktrees. An
initialized task store stays pinned; first lazy creation after entry uses the
new root. Forks keep manager tool activation but start in fresh scratch.

Built-in `shell.Executor.WithWorkspace` retains deadline, capture, credentials,
spill and process interrupt ownership while re-binding cwd and sandbox together.
Custom Bash executors require an explicit `RuntimeConfig.WorkspaceBashFactory`;
managed construction reuses `Services.BashFactory`. The embedding factory owns
policy preservation and must return a `WorkspaceBashExecutor` reporting the
requested workspace. Failed/nil/unbound/wrong-root
results refuse entry. Source capability sets are empty, so the default child role
policy omits these optional tools; trusted custom policies can select them.
Default in-process children still have fresh state without the parent's worktree
service/task board: even a selected worktree tool reports unconfigured. Child
files and shell use the parent's current execution directory. Explicit fixed
prompts, including the child role prompt, remain fixed; the default system
builder regenerates its workspace from current SystemContext.
There is no shipped OS sandbox backend. The explicitly enabled background state
receives the same prepared native executor during entry; its existing runs and
ledger remain pinned to their admitted bindings.

Git calls inherit the source process environment, take concrete argv, have a
30-second context-owned deadline, bound each output channel to five MiB and bound
pipe cleanup waits. Go replaces malformed UTF-8 diagnostics and requires an
explicit repository; source open/coercion behavior is not a domain contract.
This operator capability is not a host path/symlink sandbox, owner ACL or lease.
Native Git must be available; no Go dependency was added. Snapshot 34 compares
nine real Python/Git scenarios, outputs, file/branch effects, task bindings and
audit events. Additional Go tests cover stale prechecks, partial failures,
duplicate creation, cancellation and a real owned session's tool write/delete/stop.
Snapshot 35 compares three actual source tool flows, 25 steps, all five schemas
and risk/capability traits, plus eight canonical input identities. Go tests also
compare a real Python child loop's three results: worktree service unavailability,
a write in the inherited directory and Bash cwd. They also prove
atomic preparation refusal, parallel barriers, journal-before-observer
settlement, broker rebinding, fresh child state, fresh forks, retained task
roots and scratch deletion that preserves entered work.

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
The Cron pane uses the implemented list/schedule/cancel/arm APIs; the Goal view
uses the owned read endpoint without arming. Owned skill catalogue, preview/commit
and memory reads are also implemented. Optional
Team/Workflows/Improve/Benchmark/Self-audit
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
Twenty-eight method/path combinations cover create/list/detail/delete, completed message,
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

### User-authored skill contracts

`userresources.NewCanonicalSkill(SkillFields)` validates lowercase kebab names,
raw character/line limits, one-line descriptions, NUL and skill-wrapper refusal.
Description/body snapshots are normalized like Python: universal CRLF/CR
newlines and Python whitespace trimming. Content and its SHA-256 are immutable
strings; returned fields are detached value copies. Digest is the canonical UTF-8
content hash, not a live catalogue entry digest.

`OwnerDirectoryKey(owner)` derives u- plus SHA-256 from the exact UTF-8 identifier;
case, whitespace and Unicode normalization forms remain separate. Empty/invalid
UTF-8 identifiers fail. Only trusted composition may choose this owner. The
function creates no directory and binds no authority.

Snapshot 57 compares 42 actual source canonicalization cases and ten exact owner
keys, including Unicode character limits, splitlines boundaries, wrapper aliases
and newline normalization. Go additionally refuses invalid UTF-8 skill fields.
Owner directory binding is now available as an explicit operator library.
Memory, trusted layered session binding, create-only publication,
next-session activation and runtime/HTTP/configuration integration remain pending.

### Owner resource directories

`userresources.NewDirectoryResolver(ctx, root)` resolves trusted configured links
before creating/tightening the private root. `ForOwner(ctx, OwnerID)` creates
0700 digest, skills and memory directories and caches immutable DirectoryBinding
values. Exact owner IDs remain separate. Paths are operator accessors, not JSON
receipts; the model never chooses an owner. Cache waiting and initial resolution
honor cancellation. Cancelled work may leave partial directories, never a cached
partial binding. Root symlink resolution is bounded at 128 links.

Pre-planted owner/skills/memory links and directory files fail with sanitized
errors. Checks match source path policy; they are not atomic filesystem fencing
against an external process replacing directories during checks. Cached paths
retain source snapshot semantics and are not re-resolved on every call.

Snapshot 58 compares ten actual Python resolver directory recipes, including
trusted root links/dangling targets, link/.. ordering, lax modes and planted links.
Native tests add concurrent cache reuse, owner separation and cancellation. The
source also constructs stores; the native resolver binds directories only. No
skill/memory store, publication, runtime binding, route or config activation yet.

### Explicit layered skill catalogues

`skills.NewLayeredCatalog(agent, user)` binds two concrete `*skills.Catalog`
construction snapshots. It implements Descriptions/Load while retaining separate
source authority: unique names resolve automatically; collisions require
`agent:name`, `user:name` or explicit scope. No user skill shadows agent policy.
Rendered instructions include source and body digest. Single-layer and layered
loads share source-file verification; changed or missing files refuse, and
diagnostics stay with the source catalogue. The layer copies construction
problems once and owns combined-budget diagnostics; bounded native logs aggregate
repeated reports and return detached values.

Both provenance headings and an omission receipt share the 8,000-character
budget. Agent entries are considered first; user flooding cannot erase the
agent heading. Explicit loads can still select entries omitted from the prompt.
Snapshot 59 compares 16 actual Python cases for selection, diagnostics, file
mutation/newline equivalence and combined/unknown-name bounds. Compiled builtin
entries need no file re-read. Catalogues are immutable after construction;
problem logs support concurrent calls.

This is an explicit operator seam. Owner resource bundles, manager/child/restore
binding, memory stores, publication/next-session activation and configuration
remain pending. The normal runtime continues to use its agent catalogue.

### Typed owner memory storage

`memory.NewStore(ctx, root, masker)` pins a physical operator root and creates
a serialized process-local Markdown store. `Write`, `List`, `Index`, `Search`,
`ReplaceAll`, `Flush` use explicit contexts and typed Input/Record/OwnerID/Origin.
Writes normalize headers, clamp bodies at 32,000 characters, cap filename slugs,
mask the whole document, fsync a same-directory temporary file and rename it;
directory fsync is best effort. New temporary files and roots use private modes,
which is stronger than standalone Python's default modes. Existing owner roots
retain DirectoryResolver's permission policy. Invalid UTF-8 fields refuse.

Authenticated filenames digest byte-length-prefixed exact owner/name pairs;
anonymous filenames preserve compatibility. Legacy same-name/same-owner files
migrate lazily. Parsed owner keys take precedence over display-owner lines.
`memory.Bind(store, owner)` returns ScopedStore with no owner override and no
catch-all delegation; replacement always keeps other owners' files. Passing nil
to raw Store owner filters is an explicit operator view/replacement of all owners.

Parse caches use mtime/size, including unreadable-file failures; external edits
reparse and returned records are values. Invalid UTF-8/unreadable memories are
skipped with bounded diagnostics. Imported type text is retained; writes use the
four supported types with project fallback. Origins normalize to the four
supported values. The source's full imported body reads are retained, so write
caps do not bound operator-imported files. Indices render from records rather
than trusting MEMORY.md, have an 8,000-character prefix with omission notice, and
flush deferred disk state on index/search. Lexical search uses unique lowercase
word terms, occurrence scores, stable filename tie order and Python slice limits.

Snapshot 60 compares 13 actual Python scenarios and 118 operations, exact file
bytes, body hashes, metadata, index timing and diagnostics. Native tests add
concurrent scoped replacement, owner protection, cancellation and detached values.
Cache semantics can retain same-mtime/same-size external edits, matching source;
multi-file replacement is not transactional or cross-process fenced. Subsequent
slices deliver owner resource bundles, manager/restore/child binding, explicit
remember/recall, automatic selection/extraction/consolidation and contained capture.

The standalone launcher constructs shared storage at MINILOOP_MEMORY_ROOT or
<workspace root>/.memory even with MINILOOP_USER_RESOURCES_ROOT selected. The
optional owner resolver takes precedence; its root is eagerly private (0700),
while owner children remain lazy. Either root construction failure refuses
startup. Binding refusal precedes all these effects. --memory-tools installs
remember/recall; default startup carries storage without those tools.
--memory-auto=false disables automatic selection/capture with the tool pair;
changed context indices still follow recall independently. Embedding uses
launcher.Options.MemoryTools/MemoryAuto or explicit agent.ManagerServices.Memory
and UserResources. Inspect/dump reports memory_backend, memory_tools and memory_auto
without creating roots or making model calls; these are configuration choices,
not runtime health evidence. Snapshot 70 compares seven actual source constructor
cases. Native HTTP model tests verify selected roots, exact owners and the copied
auto override. Admitted skill capture is described below; native-session preview/HTTP routes remain pending.

### Immutable owner resource snapshots

`userresources.NewResolver(ctx, root, agentCatalog, masker)` composes the existing
private directory policy, concrete agent/user skill catalogues and typed memory
store. `ForOwner(ctx, OwnerID)` validates the exact identifier and caches one
complete Resources value, with private binding fields and operator-only path
accessors. Memory() returns a ScopedStore directly, a stronger boundary than
Python's raw store inside its frozen bundle. It has no owner override.

Serialized construction and context checks admit the cache only after all
services exist. Failed/cancelled construction can leave uncached directories;
retry rechecks every directory rather than reusing a partial directory cache.
Cached successful bindings preserve their original catalogue and memory service.
A new resolver reads later user skill edits; live old snapshots remain pinned,
and changed source files still refuse through the shared skill verifier.

Problems(ctx) is a detached bounded operator projection in owner-resolution
order. It summarizes repeated local reports with source-compatible counts,
prefixes opaque owner directory keys and omits agent diagnostics already audited
through the deployment catalogue. It does not make one owner's model catalogue
share another owner's mutable log. Snapshot 61 compares four actual Python
resource/cache/log/memory scenarios; native tests add concurrent reuse, failure/
retry rechecks, cancellation and invalid-construction side-effect refusal.

This explicit composition library creates full resource bundles; create-only
skill publication, future-session refresh, manager/restore/child inheritance,
configuration, scoped serving and memory lifecycle/tool activation remain open.
Directory checks remain process-local checks, not filesystem fencing.

### Anchored create-only files

`durable.CreateText(ctx, path, text)` and `ReadBytesNoFollow(ctx, path, maxBytes)`
implement the Python publication file boundary on Darwin/Linux without new
dependencies or cgo. Paths are opened from `/` component by component using
O_DIRECTORY/O_NOFOLLOW and owned descriptors; symlink/.. ordering is preserved.
Relative inputs use the current working directory spelling, so linked components
still refuse. Publication callers should use their pinned physical owner paths.

Create uses a random same-directory O_EXCL/O_NOFOLLOW scratch file, exact 0600
mode, file fsync and a no-replace linkat. It returns typed device/inode identity.
Existing files/directories/links never get replaced. Link success is the commit
point; cancellation is checked before it, and scratch cleanup/directory fsync/
descriptor close are best effort afterward. An external rename can relocate the
anchored directory; the operation stays on that inode rather than following a
replacement link. This is a path boundary, not host tenancy or multi-file fencing.

Reads verify regular-file type and retain at most maxBytes+1 bytes to detect
overflow. NONBLOCK is a native strengthening: hostile FIFOs refuse instead of
hanging before the regular-file check. Syscall pointers are confined to the
boundary. Linux uses toolchain syscall constants; Darwin numbers are pinned to
Apple XNU f6217f891ac0bb64f3d375211650a4c1ff8ca1ea, syscalls.master
(openat 463, linkat 471, unlinkat 472), with Go 1.23's kernel syscall entry.

Snapshot 62 compares 12 actual Python anchored create/read cases, file bytes,
mode, identity, overflow/errors and scratch cleanup. Native tests add concurrent
no-replace winners, cancellation, descriptor-preserving parent rename and FIFO
refusal. The create-only skill publisher, strict secret screening, safe receipts
and next-session resource replacement are the next composition slice.


### Owner/session-bound personal-skill drafts

userresources.NewDraftStore(DefaultDraftStoreConfig()) supplies an explicit
process-local operator store: 15-minute TTL, 64 total drafts, 16 per owner and
four per session. The canonical skill validator normalizes fields and hashes
exact canonical UTF-8 bytes. Evidence integers and four named coverage variants
stay typed. Add takes trusted owner/session identifiers separately from content.
Draft handles are immutable identities; Preview detaches public values and
never emits owner/session authority. Get/Peek use exact owner/session and optional
digest; wrong authority is indistinguishable from absence. Access preserves FIFO,
and full capacity can replace only the requester's own oldest draft. Atomic
Consume has one winner; post-publication DiscardCommitted uses exact identity
and succeeds after TTL when the object remains stored. It never publishes a file.

Snapshot 71 compares 40 actual Python operations, including quota ordering,
non-consuming failures, TTL boundary and object-identity cleanup. Native tests
add evidence detachment, private JSON projection, copied limits and concurrent
consumption. Typed duration/configuration errors and initial entropy failure use
safe native errors; source config ValueError and malformed dynamic types have
no direct domain counterpart. Store Add permits signed/duplicate integer evidence,
as source does; the candidate parser validates evidence against its
projection. There is no manager/launcher injection, model preview, authenticated
turn projection or HTTP skill route yet. Publication should retain a Peek handle
until successful durable commit, then discard it; Consume is not a pre-commit step.


### Typed skill preview business flow

userresources.SkillPreviewer implements the source preview function through
an explicit SkillPreviewModel seam and typed DraftStore. It preserves name/owner/
sensitive-name ordering, strict ledger selection, empty-transcript refusal,
2,000-character masked focus, source system text and 2,500 output tokens. Two
generation attempts rebuild sanitized payloads; repair contains a safe reason
without quoting prior output. Successful parse is followed by another health
check, skip refusal or owner/session-bound draft retention. Failures retain no
draft; cancellation propagates and native host faults use safe typed errors.
Snapshot 75 compares 29 actual Python flows, every request hash and accepted
draft fields. Explicit standalone Session.PreviewPersonalSkill now binds the
normal model cache/recovery/shared limiter/telemetry with preview purpose and
empty tools, joining only text blocks. Snapshot 76 compares six actual Agent
request/repair/refusal/provider flows and model events. Core turn admission
serializes it; history and token meter remain unchanged. Manager admission/lease
binding and HTTP routes remain pending; no publication default is activated.
Manager construction now owns one typed DraftStore and shares it through the
common create/fork/restore factory. Snapshot 77 compares seven actual Python
manager injection frames and process-local draft loss at restart. Global 64-item
capacity cannot be bypassed through another session; foreign owners cannot be
evicted. Standalone previewers retain independent stores.

### Typed skill candidate parsing

userresources.ParseSkillCandidate returns an immutable SkillCandidate or a named
CandidateError. Exact five-field schema checks precede recursive secret refusal,
then decision, field types, strict integer evidence, empty skip and canonical
create validation. Original strings and evidence order are retained; evidence
accessors detach. It preserves source duplicate-key last-wins and nonfinite-type
outcomes using transient JSON boundaries, with no dynamic retained payload or
nonfinite domain value. Snapshot 74 compares 63 actual Python parser outcomes,
accepted-field hashes and budget recipes. Native lone-surrogate/invalid-UTF-8
refusals prevent lossy decoding; native masking panics have a safe named failure.
This library adds no model request, route, durable effect or publication grant.

### Admitted-turn skill evidence capture

ManagedSession records successful input/final pairs after terminal flush when
the trusted caller granted personal_skill.capture_source. HTTP normal/streaming
admission already supplies it; ordinary/peer/cron calls, errors and cancellation
do not grant capture. Its process-local CaptureLedger is fresh per agent, trims
source Unicode whitespace, masks fixed fields/keys before 64-message/40k compact
Unicode JSON bounds and evicts single oldest messages with omission counts.
Only plain history gap strings set excluded-compaction flags. Unavailable secret
screening refuses append and latches errors even after a healthy registry later
returns. Detached snapshots retain established-empty versus absent evidence.
Projection checks the latch and masked canonical fields without granting roles.
Snapshot 73 compares 20 actual Python recipes; native tests cover concurrent
snapshots, contained faults, terminal order and real HTTP idempotent admission.
Native-session model binding, manager draft injection and publication routes
remain pending. Capture is not persisted or reconstructed from history.

### Pure skill evidence projection

userresources.ProjectSessionText preserves the source legacy projection over
protocol.Message: only cleaned ordinary plain user text and assistant text
blocks survive. User arrays are dropped completely; recalled memory is stripped
greedily through the final closing wrapper, malformed wrappers are excluded,
whole user-interjection wrappers unwrap, and known internal markers are excluded.
Compaction/snipping flags remain visible without their text. Python whitespace,
IGNORECASE dotted/dotless i and Unicode 14 word boundaries are explicit; the
shared Unicode exporter now generates the word table along with casing.

ProjectAuthenticatedText accepts already-admitted typed text and preserves its
nonempty content verbatim. Callers retain provenance responsibility. Both mask
before fitting a whole-message suffix into compact Unicode Python JSON, with a
40,000-character cap and source coverage/omission flags. Display role labels and
the two fixed JSON keys can be masked, including key collisions; these are data
labels, not provider roles. A transient map is used only by MarshalJSON; domain
values retain no open payload. Positive integer limits and UTF-8 are checked;
malformed dynamic protocol data is rejected earlier at native typed boundaries.
Snapshot 72 compares 38 actual source projections, with deterministic repeats
for large budget cases, exact JSON hashes, omissions, coverage and exclusion flags.
The pure helpers do not grant admission. Managed capture is described above;
native-session preview binding and authenticated skill publication routes remain pending.

### Owned manager skill preview

SessionManager.PreviewPersonalSkill checks owner/configuration, serializes through
managed admission, rechecks identity/accepting state and requires the process
lease. It always supplies the admitted-turn ledger and uses the shared draft pool.
Snapshot 78 compares eight actual source manager policy/preview outcomes, including
readonly previews and refusal of legacy history when the ledger is empty. The
private preview lifetime retains idle turn status/count and turn-cancel behavior.
Go explicitly cancels and joins it on deletion/stop, adding to source turn-only
cleanup. Lease loss maps to a safe 409 before and during requests. Reviewed commit
and HTTP routes are described below; no publication model tool is added.

### Reviewed manager skill commit

CommitPersonalSkill shares owned admission/lease/lifetime with preview, checks
readonly, peeks exact authority/digest, publishes create-only fields and discards
only after success. The typed receipt activates in next_session; existing live
resources remain pinned. Failure retains the draft, and durable success survives
TTL/cancellation after the file commit. Snapshot 79 compares six actual source
commit/retention outcomes. No model tool is added; HTTP routes are described below.

### Typed personal-skill HTTP requests

PersonalSkillPreviewRequest and PersonalSkillCommitRequest decode into concrete
name/focus and DraftDigest fields. Unknown fields are rejected with exact,
case-sensitive keys; name and digest use the source lowercase patterns. Focus
counts Unicode code points rather than UTF-8 bytes, defaults only when absent,
and rejects explicit null. Duplicate JSON keys follow source last-value-wins
behavior. Failed decoding preserves the existing receiver. Snapshot 80 compares
43 actual Pydantic acceptance and normalized-value cases. These request types
now feed the preview/commit routes. Valid-JSON HTTP validation lists are described
below; malformed Unicode handling still requires separate route-boundary evidence.

### Owned personal-skill HTTP routes

The two POST routes reuse the one admitted principal, require its live session,
then call Manager preview/commit with the request context. They add no model
publication authority and no rate accounting, matching the source route flow.
Typed policy/draft errors keep source status, code and message through masked
JSON projection. Unknown host faults return a safe native failure without host
text. Snapshot 81 compares 11 actual HTTP responses, with authentic message
capture, public preview and receipt fields, readonly/wrong-digest/cross-session
refusals, successful publication and exact draft consumption. Dynamic IDs and
timestamps are checked independently, then excluded from source comparison.
Failure cases retain the same draft for eventual commit. Request validation is
described below; malformed request boundary fidelity remains open.

### Personal-skill request validation lists

Valid-JSON validation failures now return typed ordered detail lists, preserving
source type/location/message/input/context fields. Schema fields are validated
in declaration order; extra fields follow input order. Duplicate keys retain
their last value and original position. Absent or null bodies use body-level
missing errors; non-object roots use model_attributes_type. A closed six-variant
ValidationInput exists only in HTTP diagnostics. No open payload is retained in
session/service state. Responses use the existing masked recording projection,
including escaped input values and extra-field keys. Snapshot 82 compares 41
actual FastAPI responses. UTF-16/32, lone-surrogate, extreme numeric input and
nesting-boundary parity remain open. Syntax/media/UTF-8 behavior is described
below. The native diagnostic tree caps
nesting at 256; complete FastAPI parity is not claimed for these boundaries.

### Personal-skill JSON syntax and media admission

Only application/json or application subtypes ending in +json parse JSON bodies,
matching FastAPI 0.136.3's strict_content_type default. Missing or other media
types echo a byte-string diagnostic rather than granting object semantics. Empty
bodies still use the required-body error. A bounded syntax scanner follows the
CPython prefix grammar and reports code-point positions through a closed
field/position location variant. UTF-8 BOM is removed before parsing; illegal
UTF-8 JSON returns source 400 details, while illegal non-JSON input preserves the
source safe 500 plain response. Snapshot 83 compares 63 actual HTTP results,
including every supported syntax failure category, multibyte positions, media
variants and encoding admission. JSON convenience tests now send Content-Type
explicitly; dedicated missing-header cases use actual untyped requests. Alternate
UTF-16/32 scalar decoding is described below. Surrogatepass, nonfinite/extreme
numeric values and depth boundary parity remain open.

### Personal-skill JSON byte decoding

The byte decoder follows Python json.detect_encoding: UTF-32 BOM takes precedence
over UTF-16, then UTF-8 BOM; absent BOMs use the first four bytes' NUL pattern or
the two-byte special case. Named encodings and explicit endian reads lower scalar
UTF-16/32 data into UTF-8 before existing syntax/schema validation. Valid surrogate
pairs in UTF-16 become one scalar; truncated units and out-of-range UTF-32 refuse.
Declared charset does not override detection, matching the source. Snapshot 84
compares 88 actual HTTP results across ten byte forms and both routes. Its six
surrogate counterexamples are historical and now covered by the next slice.

### Personal-skill surrogatepass boundary

Byte decoding now preserves Python surrogatepass code points without substitution.
A closed value reader lowers valid syntax directly into the diagnostic tree,
combining escaped UTF-16 pairs but preserving raw UTF-32/UTF-8 pairs as separate
source code points. Duplicate keys retain their final value and original position
before Unicode screening; discarded malformed values do not poison requests.
Syntax positions count raw non-scalars as one source code point. Active non-scalars
in retained strings or keys produce the safe source plain 500 response, before
owner lookup or model execution. Malformed documents still produce their source
syntax errors. Only scalar, normalized tree data is encoded into concrete requests,
preventing the old decoder from replacing discarded malformed Unicode. Diagnostic
encoding independently refuses non-scalar strings/keys. Snapshot 85 compares 90
actual HTTP outcomes, including the previous six counterexamples; native tests
also check decoded pair, overwrite and control-escape values. Numeric semantics
and pinned live HTTP depth are covered by the following slices.

### Personal-skill numeric request boundary

Finite floating-point JSON input now normalizes through IEEE-754 double parsing
and source-compatible shortest decimal formatting, including signed zero,
underflow and precision loss. Integer spelling remains arbitrary precision up to
the pinned CPython default 4300-digit limit; negative integer zero becomes zero.
The syntax scanner enforces that limit at the point the number is parsed, including
values later overwritten and numbers followed by malformed syntax. Nonfinite
literals and floating overflow enter an explicit closed diagnostic variant; after
last-key retention, any remaining nonfinite value produces safe source plain 500
before ownership/model work. Discarded nonfinite values do not poison requests.
Snapshot 86 compares 288 actual HTTP outcomes across both routes, six placements
and 24 numeric forms, comparing complete diagnostic number spelling without float
conversion in the test oracle. Source interpreter changes to the integer limit
fail the fixture pin. The pinned live HTTP depth profile is covered below;
alternate runtime profiles remain unverified. This does not introduce nonfinite
values into service/session structs or add dependencies.

### Personal-skill live HTTP depth boundary

Snapshot 87 uses real localhost Uvicorn HTTP with default loop/transport selection
and CPython recursion limit 1000. It records 144 source outcomes across 12 depths,
six shapes and both routes. In this pinned profile, parsing permits 985 containers;
validation echo permits 978. Above the latter, source safe plain 500 is preserved;
above the former, parsing returns source 400 even for discarded values. Syntax
errors above the echo budget still return 422 with their small safe diagnostic.
Array/object/empty-container counting and duplicate retention are explicit.
TestClient's parse/echo budgets are 980/973 because its stack differs; these are
not the standalone HTTP target. Alternate server/interpreter stacks are unverified.

Go compares the entire source bodies/statuses in unmasked and registered-secret
modes (288 comparisons), including a credential at the deepest echoed leaf. A
separate request-diagnostic projection allows the validated deep tree and keeps
its own depth-1000 and existing byte bounds, key/string masking, and fail-closed
panic handling. Ordinary recording retains depth 256. Only concrete validation
responses use the new projection; no open payload enters runtime state.

### Owned skill catalogue HTTP view

`GET /sessions/{session_id}/skills` authenticates once and requires the admitted
owner before reading descriptions. `ManagedSession.SkillCatalogue` reads the same
fixed source used by model requests, and `SkillCatalogueResponse` contains only
session ID and catalogue text. It never loads skill bodies or resolves a fresh
owner snapshot on behalf of an existing session. Future independent sessions and
forks see newly published owner skills; existing sessions keep their bindings.
Open deployments retain one anonymous principal. Shared custom skill sources must
obey ManagerServices' existing concurrency contract.

Snapshot 88 compares 48 actual Python HTTP outcomes across empty/shared/layered/
anonymous configurations, including denied query credentials and foreign reads.
Additional native tests pin admission before source access, registered-secret
masking of descriptions, no body loads and safe plain 500 on custom source panic.
The registered-secret projection is the existing native HTTP safety extension;
source catalogue comparisons are otherwise exact. No tool/default/owner authority
is activated by this read. Memory/team/workflow and other remaining routes still
require their planned slices; this does not close G3 or the full migration.


### Owned memory HTTP views

The manager constructs WorkspaceRoot/.memory when Services.Memory is nil, even
with owner-local resources selected. Owner-local storage takes precedence at
runtime binding. GET /sessions/{session_id}/memory returns typed metadata;
GET /sessions/{session_id}/memory/{name} returns the exact stored body. Both
require the admitted owner and use the existing masked HTTP projection. Existing
sessions see subsequent writes through their fixed scoped store; tools remain
explicitly selected. Snapshot 89 compares 45 actual source responses across shared,
local and anonymous deployments, including auth, quoting and encoded slashes.
Native tests additionally cover detached views, wrong binding/cancellation,
registered-secret masking and safe filesystem failure. A removed root produces
native safe plain 500 whereas Python's glob yields an empty list.


### Typed benchmark statistics

`benchmark.AggregateRuns`, `Compare` and `BehavioralMetrics` are operator functions
on concrete task results and typed transcripts. Six explicit optional measurement
fields use an immutable closed `Number`: exact integers, finite doubles or booleans
(the source accepts bool as a number). BigInteger construction/access detach values;
JSON integer digits are pinned at the current Python 4300-digit limit. Nonfinite
arithmetic returns a typed error instead of retaining NaN/Infinity.

Aggregation preserves first-seen tasks/arms, strict-majority votes, rounded pass
rates, first nonempty error and numeric medians over supplied rows. Comparison
pairs the last row per task but totals every row, refuses unequal task sets, and
lets any regression override wins. Performance warnings follow six-dimension
order, use Python decimal rounding and require a rounded delta strictly above 25%.
Their presence does not change the effect verdict. Motion counts identical path/
offset/limit windows, with absent/null distinct from explicit zero, and rendered
errors including Unicode decimal exit notes; is_error alone is not the judge.

Snapshot 90 compares 51 actual Python statistical/behavior/rounding outcomes.
Native tests additionally cover finite guards, detached large integers/results and
non-mutating decode failures. Model arms, timing/cost measurement and the HTTP
endpoint are still pending. No
model was called by these statistical probes. Earlier package status notes are
chronological; this is the current component checkpoint.

### Admitted benchmark tasks

`benchmark.DefaultTasks()` and `HeldoutTasks()` return five visible and three
heldout tasks with the source's exact prompts, trusted typed Judge/Setup callbacks
and detached optional tool names. `NewTask(TaskConfig)` requires a judge; the zero
Task refuses execution. Empty names/prompts remain representable. ToolNames returns
names plus explicit presence: absent means the installed profile, present empty
means no tools. Custom callbacks own their state and must honor context cancellation.

Prepare creates the exact 6000-line, 324000-byte log for both paging tasks. Judge
preserves Python substring checks, case lowering, strict UTF-8/universal newlines,
Unicode splitlines and existence-only nested-file behavior, including directories.
Missing/broken/looping paths fail the effect check; directory reads and invalid
UTF-8 become native errors. Native OS error text is not Python exception spelling.
Snapshot 91 compares eight specifications, 50 real Python judge outcomes and the
seed digest. Native tests also cover callback faults, cancellation, immutable
lists/whitelists and explicit empty selection. Model-generated drafts are not
admitted judges. POST /benchmark now composes these admitted tasks through four independent fake arms.

### Owned benchmark arms

`benchmark.RunArm` takes an explicit workload and ManagerConfig, captures task
storage before callbacks, creates fresh anonymous interactive sessions and applies
the selected catalogue. A caller-supplied workspace factory must isolate tasks.
Workspaces remain available for inspection; injected shared services stay caller-owned.
The owned manager is stopped/joined on success, setup faults and cancellation.
Setup/create faults abort the arm, ordinary run/judge faults score failed rows,
and cancellation propagates, including cancellation returned by a provider whose
parent context remains live. Native stage errors unwrap their original cause.

Time measures session.Run only, rounded to one decimal millisecond. Cost/motion
measure final actual history; empty typed history costs zero. Source recovered
provider-error text still reaches the effect judge, rather than automatically
failing the task. Snapshot 92 contains eight actual Python arm recipes: visible,
heldout, empty, setup/judge/entry-run faults, provider recovery and cancellation.
All eight default-fake task effects/final texts/deterministic metrics match.
Native fault labels remain Go diagnostics; the recovered fixture costs 38 tokens
versus Python's 32 because that class spelling is longer.

`FakeProvider.ObjectView()` represents Python's non-SDK fake-object transcript
projection, omitting tool caller metadata. Raw FakeProvider continues to model
the client wire reply with caller=null; dictionary/SDK simulations and actual
provider absent/null semantics retain that view. The launcher now selects the
object view. This distinction fixes four excess transcript-estimate tokens per
default fake task without subtracting fabricated cost. No paid HTTP arm is exposed.


### Fake-only benchmark HTTP

`POST /benchmark` requires the normal admitted principal and shares the expensive
route rate budget. Body/query options cannot select a paid provider, model or
workload. `benchmark.RunFakeComparison` constructs four fresh object-view fake
clients, two visible arms and two heldout arms. Only visible rows are returned;
the heldout verdict supplies a separate comparison. Each arm joins its manager
before the temporary a/b roots are removed, including cancellation/setup failure.
The real main manager's provider and owner resources are not injected.

The handler reads the supported environment profile per request, forcing fake
transport and no spill. `httpapi.Config.BenchmarkSkillsDir` captures the deployment
path; empty selects compiled builtin data. External catalogue files are read
fresh for every arm. Launcher passes its actual configured skills path. Explicit
external memory/trajectory paths retain their configured effects. Unsupported
comprehensive features, workflows, guardian, token-efficiency and AST activation
fail privately; native profile validation limits remain explicit differences.
Snapshot 93 compares five actual source HTTP profiles with only nondeterministic
durations/derived warnings omitted. This is instrument validation, not model
quality evidence or G7 completion. Paid runs remain operator initiated.


### Typed self-audit observations

`selfaudit.BuildReport(Observations, Scope)` renders activity, problems, trajectory
trends, skill-load correlation and optionally fleet cron authorization. Explicit
owner selection narrows session/recording data; authenticated callers must pair
that owner with IncludeGlobal=false. The adapter must authorize before storage IO.
The pure library reads no files and launches no model/cron/benchmark work.
Failures retain a class, without private exception content. Input snapshots remain
unchanged. Source session/trajectory/event bounds and 8000-character report cap
apply, including owner totals capped at 100 and the explicit truncation marker.

`SuggestObjectives` and `SuggestBenchTasks` return typed human-review data over
last-three ledger entries, with Python whitespace/300-character deduplication and
source SHA-256 draft names. Pass MaxSuggestions for the default eight; nonpositive
limits clamp to one as source. A draft holds NoExpectation, which encodes/decodes
only null and contains no callable. Admitting a task still requires an explicit
benchmark judge. Snapshot 94 pins 26 actual Python processing profiles.

Live manager collection is implemented below. Model-tool installation and the
three self-audit HTTP routes are bound below. This package accepts concrete scalar/finite observations;
it is not an arbitrary Python object interpreter or an owner-authentication layer.


### Exact problem diagnostics

`problems.Log` keeps 50 distinct messages by default, first-seen FIFO order and
exact occurrence/total/eviction counters. Repeats retain their position; evicted
entries restart at one when reintroduced. Clear resets total/dropped and retained
messages while retaining capacity. Snapshot slices and Counter.BigInt views detach
from the live log. Native locking makes append/clear/snapshot atomic; an Extend
sequence performs its source append transitions individually. New(limit) preserves
nonpositive source limits; Append then returns ErrEmptyEviction after incrementing
total. The zero Log defaults to 50. Messages are already rendered strings.

`selfaudit.ProblemSource` is an optional narrow holder seam. ApprovalBroker and
InMemoryActionJournal now supply SelfAuditProblems with exact lifetime counts and
source 50-entry retention. Their existing Problems() methods preserve their prior
100-entry/deduplication contracts. Ledger.Total uses problems.Counter, so supplied
counts beyond machine integers retain exact decimal output. It does not use floats
or retain mutable integer aliases. Snapshot 95 pins nine actual source sequences.
Native diagnostic holders and live collection are implemented below; stored
journals without a diagnostic seam remain pending; HTTP and optional model
binding are implemented below.


### Live self-audit collection

`SessionManager.ObserveSelfAudit(ctx, scope)` returns concrete observations for the
existing pure report/suggestion/draft processors. Embedding callers establish
authority; authenticated frontends must pair Owner with IncludeGlobal=false.
It admits owner handles before IO, bounds actual session inspection to 100, keeps
fleet totals, and never holds manager/session locks during injected callbacks.
Native holders implement optional ProblemSource; cron snapshots read jobs/arming
together. Task observation uses the runtime's existing atomic store reference.

Recordings are admitted by session and recorded owner before event reads. Source
50-recording, recent-20-owned-session, 10-per-session and 200-tool-use limits apply;
other tools consume the event budget too. Transient wire decoding additionally
has a total 64 MiB budget. Faults retain class only, and event faults preserve
trajectory trends. Snapshot 96 compares six actual Python manager constructions,
while native tests exercise real JSONL, bounded reads, cancellation and concurrency.

A memory binding reports its explicitly attributed write/replacement diagnostics.
Unreadable shared filenames stay fleet-only; a new binding starts its own ledger.
The privileged fallback Store exposes its full raw diagnostic snapshot for fleet
views. Owner-resource fleet observation currently retains binding diagnostics,
leaving unattributed per-resource errors as a remaining collection gap. This is a
privacy correction to source shared-memory diagnostic delegation. No model tool
or HTTP route is installed by this observer itself; the HTTP binding below
and optional model binding below use it. Suggestions launch no work and task drafts still contain only null judges.


### Self-audit HTTP routes

GET `/self-audit` returns the live bounded plain-text report. GET
`/self-audit/suggestions` returns SelfAuditSuggestionsResponse and GET
`/self-audit/bench-task-drafts` returns SelfAuditDraftsResponse with fixed null
expectations. Configured auth always uses the admitted principal with fleet
inclusion disabled; open deployments use the operator view. No query/body fields
override those choices or the fixed default limit. These source GETs do not consume
rate budget, and no suggestion/draft launches a turn or becomes an admitted task.

`ObserveSelfAuditProblems` is the ledger-only embedding seam. It skips activity/
Info, cron jobs and trajectory list/event IO; it never constructs another task
board. Source-snapshot failure becomes a private 500 for curation while reports
preserve class-only section failures. Plain report projection precedes writes,
fails closed, and reapplies the source Unicode cap/marker after masking expansion.
JSON responses retain the existing concrete projection.

Snapshot 97 compares 72 actual Python HTTP responses with only generated session
IDs replaced by labels. Native tests check no recording IO/launches, owner-scoped
shared-memory omission, global auth/body caps, no-rate behavior, private failed
projection and expanded mask caps. The handler now exposes 37 operations across
34 patterns; seven improvement/team/workflow operations remain open, along with the broader port and native persistence gaps.

### Self-audit model input

protocol.SelfAuditSchema and SelfAuditToolInput describe the optional source tool.
SelfAuditInput is an empty concrete payload; model arguments cannot request another
owner or fleet visibility. Unknown fields and non-object values are rejected at
the provider boundary. Canonical replay JSON, recording projection and typed
provider tool-use blocks preserve the self_audit discriminator. Snapshot 98 pins
the source schema and keyword contract. The protocol declarations alone do not
activate the optional runtime binding below.

### Bound self-audit model tool

RuntimeConfig.SelfAuditTools installs the source read-risk, readonly, exclusive
self_audit tool; ManagerServices.SelfAuditTools selects the same installation.
RuntimeConfig.SelfAuditObserver is the collector seam. Manager construction binds
its actual observer for new, forked and restored runtimes. The finite trusted
SelfAuditView defaults to SelfAuditOwnerView; SelfAuditOperatorView requests an
unscoped fleet report. Invalid view values fail construction. No model fields
can alter scope. Selected children inherit the binding; default role policies
still omit its empty source capability set. A bare activated runtime with no
observer returns the exact source no-manager notice.

`--self-audit-tools` selects the individual tool at startup. Configured auth picks
the fixed admitted owner view; open startup retains the source operator report.
This explicitly corrects Python's unscoped model report in authenticated deployments.
MINILOOP_FEATURES remains unsupported until its complete bundle is implemented.
The collector never reenters the running session turn lock. Existing gate guards,
permissions, journal, result hooks and registered-secret projection remain in
order. Injected observer panics return a fixed private failure; cancellation
aborts before/after collection. The source character bound applies before normal
result hooks/projection; hooks can deliberately replace output, as for other tools.

Snapshot 98 now also captures the actual source handler's no-manager notice;
snapshot 96 supplies the actual empty manager report. Native tests execute actual
model tool loops, owned/operator active manager reports, readonly execution,
selected/default children, guard refusal before observation, masked results and
local HTTP provider startup under both authentication modes. Dump-config reports
selection without starting runtime. No additional HTTP operations are introduced.

### Improvement acceptance instruments

improvement.VerifierTouches classifies changed paths by the source substrings,
retaining case, order and duplicates. VerifierFingerprint(workspace) computes a
comparable InstrumentFingerprint [16]byte with a fixed 32-hex JSON/text projection.
The four source globs are tools/verify_*, .github/workflows/*, conftest.py and
tests/conftest.py, sorted independently in that order. Only regular files count;
file symlinks are followed. Relative paths and full bytes enter SHA-256 without
separators; the first 16 bytes are retained. Failed reads hash <unreadable>, while
stat permission errors and non-permission scan IO failures abort. Missing roots
produce the empty digest. Literal workspace metacharacters are supported.

Snapshot 99 pins three source classifier profiles and ten source filesystem
fingerprints. Native tests verify actual filesystem recipes, Unicode/binary and
symlink behavior, typed projection, changed/restored instrument contents, detached
classification and explicit IO faults. Malformed path encoding is exercised with
a detached entry on APFS. This is an operator library. Native archive, verified
loop, proposal worktree/commit composition and improvement HTTP routes are the
next steps. These four globs cover their declared locations; acceptance callers
must sample at each judgment window to detect intermediate tampering.
