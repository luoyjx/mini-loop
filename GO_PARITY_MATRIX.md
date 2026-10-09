# Python to Go parity matrix

## 2026-10-09 native SQLite killed-generation process recovery

Baseline: fd57eb8. Native sqlitestate now has an actual process-kill integration
test for the admitted generation window. A dedicated child of the Go test binary
creates a Manager/session, persists its request and process lease, validates the
request at the real provider boundary and atomically publishes a typed checkpoint
with session ID, lease identity and writer PID. Complete remains blocked. The
parent validates the checkpoint against its exact child handle, kills only that
process and waits for its unsuccessful terminal exit. Manager.Stop and Store.Close
are bypassed; cleanup always kills/joins the owned child if the parent fails.

Another process opens the native SQLite database. The admitted request and
unexpired writer lease are retained. Manager restore exposes a pending owner-bound
handle with unrepaired history; a premature Run is refused and invokes no model.
No test SQL update, injected clock or manual lease release is used. After natural
eight-second lease expiry, Run reloads/repairs the unanswered user turn, installs
a distinct process lease and sends the exact interrupted-turn marker in its next
provider request. The resumed turn persists; after Stop/close/reopen another
Manager retains the repair/history and run_count=2. The helper's PID is verified
to differ from the recovery process, so this is not instance reconstruction alone.

The checkpoint is only a test synchronization file, not runtime persistence.
This proves a killed-generation window on the local OS, not power-loss durability,
external-tool effect reconciliation, in-flight SQL transaction kill recovery or
all crash windows. SQLite remains configured with Source NORMAL synchronization.
The test uses a local typed provider and no external service. Wider archival
profiles, full feature activation and final Go port audit remain open. This slice
adds typed tests/documentation only, without runtime changes or dependencies.

Next activation audit must resolve the remaining MINILOOP_FEATURES refusal rather
than infer completeness from optional flags. Current config.Settings.Unsupported
still rejects the comprehensive bundle. Source Manager selects full_registry and
default_injectors only for enable_features when no explicit overrides are supplied;
workflows, AST and decision selection remain independent. Capture the concrete
Source full catalogue/injectors and compare current Go composition before removing
that refusal. Other native crash windows remain separate proof work.

Focused process-crash test passed (8.465s); full Go ordinary tests (-p=1 -count=1
-timeout=300s) including the final exact repair assertion and go vet ./... passed.
Full Go race (-race -p=1 -count=1 -timeout=300s) passed, including the
process-kill recovery test. Native links emitted nonfatal macOS CGO LC_DYSYMTAB
warnings; all commands succeeded. git diff --check passed. README runtime
baseline/boundary, extension/Go README and architecture specification updated.
Archify delivery passes 9/9 with zero errors/warnings and exact receipt hashes
verified. Visual review remains skipped after the earlier browser denial.
Python source/default-tool types/exported fixtures were unchanged; Python suite,
export and coverage were not refreshed. No push; full port goal remains active.

## 2026-10-09 native cron and scheduled SQLite reconstruction proof

Baseline: ba6dc73. Native sqlitestate integration now replays all seven actual
Python scheduled-restoration recipes from Snapshot 47: bound-clean, scratch-clean,
bound-crash, scratch-crash, missing-sql, missing-null and foreign-bound. Every
stored recipe closes/reopens native SQLite before Manager construction. Bound
workspace paths remain recorded; scratch selects the current explicit factory.
Scheduled requests use the current system builder rather than recorded custom
system. Metadata survives repair as the documented Go addition. Missing SQL rows
remain anonymous/unconfirmed and cannot run or create the missing row. No-store
fallback remains ephemeral. Foreign leases prevent repair/model admission.
Live handles are reused and pre-provider transcript flush uses actual native SQL.
Growth publishes the newly selected scratch workspace/system projection.

A second integration creates a session through Manager, runs a provider round,
persists a durable one-shot cron job, stops/drains and closes SQLite. A reconstructed
Manager loads the JSON job disarmed; explicit Manager Start leaves it disarmed
and no session is restored eagerly. Tick before arm does nothing. Privileged
operator arm permits lazy native restoration, a second provider request with the
persisted prefix and a fresh untrusted context without actor/capabilities. Stop
releases the lease. Another database/Manager reopen proves run_count=2 and that
one-shot removal persisted. Cron definitions remain .cron.json, with independent
claim files; sessions, history and process leases use SQLite. No cron SQL table
or external-effect transaction is claimed.

The tests use explicit Source civil-minute ticks and Manager reconstruction in
one test process. OS-process crash/clock-drift testing and wider archival profiles
remain open, along with full feature activation and final Go port audit. This
slice adds typed integration tests and documentation only; no runtime changes or
new dependencies. Python source/default-tool types/exported fixtures unchanged.

Focused native cron and scheduled tests passed. Full Go ordinary tests (-p=1
-count=1 -timeout=300s) and go vet ./... passed. The final focused run adds explicit
Manager Start to verify restored jobs stay disarmed; full race covers that final
assertion. Full Go race (-race -p=1 -count=1 -timeout=300s) passed.
Native test links emitted nonfatal macOS CGO LC_DYSYMTAB warnings; all commands
succeeded. git diff --check passed.
README baseline/canonical map, extension/Go README and architecture specification
updated. Archify delivery passes 9/9 with zero errors/warnings and exact receipt
hashes verified. Visual review skipped after the earlier browser denial. Python
suite/export/coverage were not refreshed. No push; the full port goal stays active.

## 2026-10-09 Launcher caller-owned state injection and explicit restoration

Baseline: 492a916. launcher.Options.StateStore is now a concrete consumer-owned
agent.StateStore seam forwarded into ManagerServices. Default startup remains
process-local. App.RestoreSessions(ctx) delegates explicit operator restoration;
neither NewWithOptions nor Serve automatically publishes saved session handles.
The Manager's existing default stored-journal construction policy still explicitly
marks started actions unknown. Store open and side-effect-free inspection do not.
Report.StateStore now uses named StateBackend values process-local/injected;
injection selection makes no claim about arbitrary backend physical durability.

Backend ownership stays with the embedding caller: App never opens or closes it,
including on constructor failure. Stop cancels/joins manager work and releases
owned leases before the caller may close the store. Restore preserves the Manager
partial-fleet result/error and cancellation semantics and does not start a turn,
arm cron or reconstruct historical grants. HTTP owner admission stays unchanged.
Python server.create_app supports custom Manager/manager_factory composition,
while its default startup supplies no SQLite and performs no automatic restore.
This slice follows that seam; it introduces no CLI/environment database setting.

Native authenticated HTTP integration writes a model turn, reads its stored
transcript, stops/releases the process lease, closes/reopens SQLite and constructs
a second Launcher. Before explicit restore the session returns 404 even after
Serve starts. Explicit restore exposes the owner-bound handle, a repeated restore
returns no duplicates, foreign GET/transcript/events remain 404, the transcript
is unchanged and a resumed model turn persists without degradation. Tests also
verify inspection leaves started actions unchanged while Manager construction
marks them unknown, cancelled restore propagates, stopped restore is refused,
no-store restore stays empty and failed construction leaves the caller store open.

Focused native Launcher tests, full Go ordinary tests (-p=1 -count=1
-timeout=300s), go vet ./... and full Go race (-race -p=1 -count=1
-timeout=300s) passed. Native test links emitted nonfatal macOS CGO
LC_DYSYMTAB warnings; all commands succeeded. git diff --check passed.
README canonical map/baseline, boundary, extension guide and Go README updated;
Archify delivery passes 9/9 with zero errors/warnings and exact receipt hashes
verified. Visual inspection remains skipped after the earlier browser denial.
Python source/default-tool types/exports were unchanged; their suite/export and
coverage were not refreshed. Native cron restart, OS-process crash tests, wider
archival variants and full Go port audit remain open. No push.

## 2026-10-09 native SQLite Manager reconstruction proof

Baseline: 78ce1a3. Library-injected agent.SessionManager now has real native
SQLite close/reopen integration evidence. TestManagerNativeSQLiteRestartMatchesSourceRecipes
replays all seven actual Python manager restoration recipes from Snapshot 46:
clean, bare-user, bare-blocks, tools, paired, empty and foreign. It closes native
handles before constructing each successor, rather than retaining an in-memory
backend. Restored owner, system, status, creation time, run count, workspace,
Todo/steering and repaired transcript match supported Source facts. Existing Go
additions preserve metadata through repair and advance payload sequence beyond
both physical cursor and archived sequence.

Foreign leases prevent crash-tail repair and model admission until released;
reacquisition reloads before the resumed request. Pending tool approval expires
with a persisted resolution time, and the transcript contains not-run repair.
Second database reopen retains repair/metadata. Stop releases owned leases.
Bounded native CatchUpEvents returns scoped, strictly increasing sequences above
the original payload sequence 9; managed ReadTranscript reads native rows.
TestManagerCreatedSessionSurvivesNativeDatabaseReopen creates a session through
the public manager, runs a real local provider round and saves steering. After
Stop/database close/reopen, a new Manager resumes the persisted request prefix
and sends the queued steering to its provider without degraded persistence.

These are Manager-instance reconstruction tests in one Go test process, not
OS-process termination tests. No Launcher setting, cron restart proof, restored
human authority or external-effect fencing is inferred. Those remaining slices
and wider archival variants remain open. This slice adds typed integration tests
and documentation only; it changes no runtime behavior or dependencies.

Next activation slice follows the actual Source startup seam: server.create_app
accepts a supplied manager/manager_factory; its default constructs a manager
without SQLite and does not call restore_sessions automatically. No Source
STATE_DB environment option was found in server/config/__main__. Go Launcher
should first expose an explicit typed caller-supplied StateStore and operator
restoration path, preserving caller ownership and default process-local behavior;
a new CLI database switch would be a separately labeled Go addition.

Validation: both focused native Manager tests passed, full Go ordinary tests
(-p=1 -count=1 -timeout=300s), go vet ./... and full Go race tests
(-race -p=1 -count=1 -timeout=300s) passed. The final focused live-create
run additionally verifies steering reaches the resumed provider; full race covers
that assertion. macOS CGO emitted a nonfatal LC_DYSYMTAB link warning.
git diff --check passed. README runtime baseline/boundary, extension guide, Go
README and architecture specification updated. Archify delivery passes 9/9 with
zero errors/warnings and exact specification/artifact receipt hashes verified.
Visual inspection remains skipped after the earlier browser denial. Python source
and exported fixtures were unchanged; Python suite/export/coverage were not
rerun for this test-only slice. No push; full Go port goal remains active.

## 2026-10-09 coverage refresh after native SQLite audit delivery

Measured implementation baseline: `547de7d`. Full serialized cross-package test
run passed on local Darwin arm64:
`cd go && go test ./... -p=1 -count=1 -timeout=300s -coverpkg=./... -coverprofile=/tmp/mini-loop-go-cross-coverage.out`.
Repeated profile blocks were deduplicated by source location; execution counts
were combined by maximum, counting each statement once across test packages.

| Scope | Covered / total statements | Coverage |
| --- | --- | --- |
| All Go packages | 26,914 / 29,902 | 90.01% |
| agent | 8,766 / 9,783 | 89.60% |
| sqlitestate | 520 / 625 | 83.20% |
| efficiency | 1,489 / 1,629 | 91.41% |
| httpapi | 1,610 / 1,802 | 89.35% |

Previous comparable snapshot at `6feeba2` was 25,648 / 28,448 (90.16%). This
snapshot adds 1,454 statements and 1,266 covered statements; overall coverage
changes by -0.15 percentage points. This measures executed Go statements,
not Python-to-Go feature completion, mutation strength or production durability.
Python coverage was not refreshed. The successful serialized run followed an
initial parallel AST timeout-recipe failure documented in the implementation
entry; that failed profile is excluded from these totals. No push.

## 2026-10-09 native SQLite action and approval audit rows

Baseline: 1daa299. Native sqlitestate.Store now compiles against the complete
agent.StateStore interface. Action upserts update only status, result, workflow run
and completion time. Approval upserts update only status, resolved time and answer;
original identity, prompt/preview/kind and creation fields remain immutable.
ApprovalRecord.ToolUseIDNull and its concrete JSON codec preserve SQL NULL versus
empty tool-use IDs. Invalid simultaneous NULL/value state is refused. Reads are
detached and approvals remain audit data without remembered grants.

MarkInflightUnknown is explicit, scoped by optional session or global when nil.
It selects started IDs after BEGIN IMMEDIATE, closes the cursor, updates inside
that transaction and rolls back all earlier updates on failure. Open does not
invoke recovery. Unknown does not prove foreign-process death or external-effect
fencing. Native tests replay existing actual Source audit/reopen/scoped/global
recovery snapshots, verify identity preservation, missing/closed reads, nullable
approval order/status filtering, detached results and trigger-induced rollback.
An actual Source SQLite probe confirms empty tool-use ID and SQL NULL stay distinct.

Validation: focused Go sqlitestate/agent tests, full Go race and vet passed;
25 Python storage/crash tests and the 186-file Source export check passed.
The initial parallel ordinary coverage run failed one existing AST timeout recipe:
the version probe reached its one-second timeout rather than succeeding before the
operation timeout. Final serialized full cross-package coverage verification
(-p=1 -count=1 -timeout=300s -coverpkg=./...) passed.
The successful race run emitted a nonfatal macOS CGO LC_DYSYMTAB linker warning.
README baseline/canonical map, extension/hardening and Go README updated.
Archify delivery passes 9/9, zero errors/warnings; exact specification/artifact
hashes verified. Visual review skipped after the earlier browser denial.
Full Python suite/coverage not refreshed. Configured Manager/Launcher storage,
native managed restart/catch-up/cron proofs, wider archival profiles and full port
audit remain open. No push.

## 2026-10-09 native SQLite transcript epochs and event rows

Baseline: 5221144. sqlitestate.Store now satisfies TranscriptStore and EventStore
in addition to session/lease interfaces. Immediate transactions reserve the writer
before MAX(ordinal); message ordinals increase across epochs and entire failed
batches roll back. Nil epoch selects MAX(epoch), empty append returns selected
epoch count and Source epoch-zero reads/counts remain empty. Old epochs stay
readable, including unfinished tool calls; no live transcript pairing is imposed
on archival reads. Known typed payloads render spaced UTF-8 without a whole-row
container cap. Physical event ordinals remain distinct from payload SSE sequences.
Exclusive after and SQL nil/zero/negative-limit behavior are preserved. Signed SQL
ordinal exhaustion returns a named error; no wraparound is allowed.

protocol.DecodeStoredMessage provides a separate archival size mode for known
message/content/block variants. The existing JSON ingress still enforces 512 KiB;
known large plain/text/thinking/result rows and accumulated block arrays no longer
inherit that frame cap. Tool-input and caller decoders retain their separate
contracts. Known event decoding uses the existing untrusted archival adapter and
its explicit 16 MiB/variant boundaries. Wider malformed/scalar/Unicode/tool-input
and caller profiles remain for audit. Historical data cannot install live actors,
capabilities or remembered grants; no full arbitrary Python payload claim.

Tests replay actual Source state-store epoch/count/rollback/reopen results, append
24 messages over two independent connections with contiguous ordinals, round-trip
large plain/result messages and 1,501 blocks, and prove network rejection remains.
Native event tests retain sequence 100/201 in ordinal 1/2, tail limits, failed append
rollback and reopen with untrusted scope. An actual temporary Source SQLite probe
confirms epoch-zero append/load (0/[]) and tail counts (2/1/0/1/2). An early test
inspector incorrectly reused a single-connection pinned pool and blocked; its
confirmed test process was terminated, inspection changed to an independent
connection and subsequent narrow/full tests passed. Production read paths use
the pinned connection directly.

Validation: final Go focused/full tests (-p=1 -count=1 -timeout=300s), vet, 32 focused
Python storage/restart tests and the complete 186-file exporter check passed.
Full Go race (-p=1 -count=1 -timeout=300s) also passed. The macOS CGO link emitted
an LC_DYSYMTAB warning; tests and command succeeded. git diff --check passed.
README baseline/canonical map, extension/hardening and
Go README updated; architecture delivery passes 9/9 checks with zero errors/warnings
and receipt hashes verified. Visual review remains skipped after the earlier browser
denial. Full Python regression/coverage not refreshed. No push. Native action/
approval SQL, complete StateStore conformance, configured Manager/Launcher storage,
durable managed restoration/catch-up/cron restart and full port audit remain open.

## 2026-10-09 native SQLite session metadata, deletion and leases

Baseline: 8066d7b. sqlitestate.Store now satisfies the consumer-owned SessionStore
and LeaseStore interfaces. Session upsert preserves creation time and independent
lease ownership; detached reads order by creation time and derive event cursors
after fetching session rows. Source system NULL/empty identity, owner/steering/
binding fallbacks and Python scalar binding truth are retained. Exact spaced UTF-8
Todo and steering JSON match actual Source bytes. Invalid UTF-8 parameters are
refused before SQL. Current typed Todo/steering shapes are decoded explicitly;
unknown fields/unsupported shapes fail instead of silently discarding metadata.

A real Source capture and native test round-trip 1,501 Todo and steering entries.
Flat typed metadata arrays avoid the generic archival container cap. Each Todo
object uses exact known property names and closed field values; no vague service
fields or payload maps are added. Broader legacy malformed metadata diagnostics
remain part of the remaining profile audit.

Deletion uses BEGIN IMMEDIATE and preserves action/approval audit rows. An actual
Source/native aborting trigger proves that earlier message/event deletion rolls
back with the session-row failure. Conditional lease UPDATEs preserve all thirteen
Source outcomes: foreign equality refusal, owner equality renewal, expired renewal
refusal, stale-owner release refusal, same-owner reacquisition and missing-row
refusal. Twenty double-connection races admit exactly one process owner; upsert
and reopen retain lease data. These database leases do not fence external effects.

Native tests exposed the driver's successful short SQL execution with an already
cancelled context. API admission and pre-commit context checks now fence those
cases; rollback uses a live cleanup context. Tests prove cancelled commit rollback,
released writer reservation, closed-store errors, detached reads and no invalid
text mutations. Metadata/lease methods install no human or tenant authorization.

Validation: 42 focused Python tests, all 19 scan guards, 186-file exporter check,
Go focused SQLite/jsonvalue tests, final full tests (-p=1 -count=1 -timeout=300s)
and vet passed. Final full race (-p=1 -count=1 -timeout=300s) also passed. The
macOS CGO race link emitted an LC_DYSYMTAB warning; tests and command succeeded.
git diff --check passed. README baseline/canonical map,
extension/hardening guides and Go README updated. Generated architecture passes
9/9 checks with zero errors/warnings and matching SHA-256 receipts. Visual review
remains skipped after the earlier browser denial. Full Python regression and
coverage were not refreshed; no push. Transcript/event/action/approval SQL,
complete StateStore conformance, configured Manager/Launcher activation and
native durable restoration/catch-up/cron restart remain required. Goal stays active.

## 2026-10-09 SQLite session metadata and deletion Source contracts

A new actual SQLiteStateStore capture records literal Unicode/HTML/quote/newline/
separator bytes in Todo and steering JSON, empty owner/system identity, immutable
created_at on upsert, reopen, empty metadata and optional-column read fallbacks.
Historical textual workspace_bound follows Python bool(), including nonempty
"false". A real BEFORE DELETE trigger aborts the final session-row deletion after
message/event deletion; Source rolls the entire operation back and a subsequent
successful deletion retains action/approval audit rows. This extends the existing
session snapshots and thirteen lease steps in python-state-store.json.

Validation: 42 focused storage/census/durable-approval/restart tests, all 19 scan
guards and the complete 186-file exporter check passed. git diff --check passed.
No Python package changes, full regression or coverage refresh. No push.

## 2026-10-09 native SQLite opening and schema migration

Baseline: f44e8b6. The standalone sqlitestate library now opens the approved CGO
SQLite driver, creates parent directories and pins a single configured connection.
WAL, synchronous NORMAL, foreign keys and the driver's five-second busy timeout
are verified on the actual connection. Close is idempotent; failed admission closes
native handles. Query/fragment bytes in filenames remain literal path content.

The embedded schema matches the complete Python export. Native creation and
additive upgrades preserve Source step ordering/version gates and leave partial
DDL/version effects when a later operation fails. Initial CREATE statements precede
future-version refusal. Existing v7 files are not silently repaired. Twenty-two
actual Source databases compare columns, indexes, version-row scalar identity,
preserved session values and exact observed migration errors. Successful native
opens are reopened and compared again. The SQL scanner immediately lowers concrete
scalar variants into the existing closed Source int conversion before comparisons;
large positive version identities remain exact in refusal diagnostics, and negative
unbounded versions retain every applicable upgrade branch. No vague domain fields.

Corrupt/foreign database errors retain Source recovery advice; ordinary environment
and context failures propagate. Malformed version-conversion diagnostics, broader
SQLite error categories and byte-specific malformed numeric profiles remain for
further audit. This package has no record methods or agent.StateStore conformance
and does not activate Manager/Launcher persistence. Native session/transcript/event/
action/approval/lease SQL, concurrent connection transactions and durable managed
restart/catch-up/cron restoration remain required. No full migration completion.

Validation: 26 focused Python storage/census tests, the 185-file exporter check,
Go full tests (-p=1 -count=1 -timeout=300s), vet and module verification passed.
Go full race (-p=1 -count=1 -timeout=300s) also passed, including sqlitestate.
The macOS CGO race link emitted an LC_DYSYMTAB warning; the tests and command
completed successfully. git diff --check passed.
README baseline/canonical map/boundary, extension and
hardening guides and Go README are updated; generated architecture delivers 9/9
checks with zero errors/warnings and matching SHA-256 receipts. Visual review stays
skipped after the earlier browser denial. Full Python regression and coverage are
not refreshed. No push.

## 2026-10-09 SQLite schema-version scalar Source contracts

The migration capture now contains 22 actual SQLiteStateStore cases. Seven new
recipes exercise fractional current/old versions, huge negative REAL versions,
Unicode decimal TEXT, ASCII BLOB, underscored future TEXT and an unbounded future
version. Version-row observations now use SQLite quote() so scalar identity is
retained without a vague numeric JSON field. Source int() truncates REAL values
and preserves the exact huge future-version refusal. No Go behavior is claimed
by this fixture extension; native checks are part of the following schema slice.

Validation: 26 focused Python storage/census tests and the complete 185-file
exporter check passed. The 19 scan guards passed in this iteration. git diff
--check passed. No package/runtime change, full Python regression or coverage
refresh. No push.

## 2026-10-09 SQLite schema migration Source contracts

Fifteen actual Python SQLiteStateStore open cases capture fresh creation, versions
0–7, a future version, multiple version rows and partially missing v2/v3/v6/v7
columns. Fixtures contain seed DDL and observed columns, indexes, version rows,
saved values and exact errors. Additive migration preserves existing data. Source
only calls _upgrade below v7, gates epoch/todo/lease additions by version, updates
all version rows and can leave v7 recorded before index creation fails. Future
refusal follows current CREATE IF NOT EXISTS statements. No Go backend claim.

Validation: 26 focused Python storage/census tests, all 19 scan guards and the
complete 185-file contract exporter check passed. git diff --check passed.
No Python package/runtime changes, full regression or coverage refresh. No push.

## 2026-10-09 native component timeout overflow/error order

Baseline: `0300df4`. Shared invocationDuration now converts the exact arbitrary
integer ms/1000 ratio using big.Rat.Float64. Finite ratios still saturate Go
scheduling without narrowing descriptor metadata. Overflow returns a named
ComponentFault/OverflowError with the exact Source division diagnostic before
component bodies run. Observation/request/response contain it in receipts and
Initialize contains it in safe lifecycle records. Close preserves Source ordering:
missing hooks and exhausted budgets skip first; conversion errors then escape
before hook containment, with admission fenced and no cached success report.

Twenty-five actual Source operations compare invocation-body calls, public status/
reason, exact escaped error and post-close admission. The 2**1024 millisecond case
proves ratio conversion rather than premature operand conversion. No dynamic
payload types or dependencies added. README baseline/boundary, EXTENDING, Go README
and hardening notes are updated. No topology or default change; interactive map
regenerated with 9/9 checks, no errors/warnings and verified receipt hashes. Visual
inspection remains skipped after the earlier browser denial.

Python gates: 124 focused tests, 19 scan guards and complete 184-file exporter
check passed. Final Go full tests (-p=1 -count=1 -timeout=180s), vet and
efficiency package race passed. Full race (-p=1 -timeout=180s) hit the HTTP
package time limit during historical metadata views; that specific test reran
green, then the entire HTTP package race passed in 42 seconds with -timeout=300s.
The original race command finished with only the HTTP timeout failure; all
other packages passed. All package race suites are verified across that run and
the HTTP rerun. The original full command remains a recorded nonzero result.
git diff --check passed. No push; full Python
regression and coverage were not refreshed. Wider wire/SPI profiles, native SQLite
and completion of the full Go port remain open.


## 2026-10-09 component timeout conversion Source contracts

Baseline: `1a64ec5`. Twenty-five actual Source operations cover three stage calls
and initialize/close across five positive arbitrary-size millisecond values.
Division converts the integer ratio: 2**1024 milliseconds remains finite after
dividing by 1000. 10**312 and 10**400 overflow before component bodies run.
Stage calls and initialize report component_error:OverflowError; close escapes
OverflowError with its exact division diagnostic. Close keeps admission fenced.
These contracts precede native conversion/error-order fixes.
Validation: 124 focused Python tests, all 19 scan guards, complete 184-file
exporter check and git diff --check passed. No Python runtime modules changed.


## 2026-10-09 native benchmark construction/filter order

Baseline: `bcaeca9`. ManagedSession.RestrictTools is an explicit first-turn-only
monotonic catalogue reduction. Nonblocking turn admission and the core lock
serialize mutation; cancelled, closed, busy, used and restored-history handles
refuse it. A fresh catalogue preserves installed order and immutable snapshots;
unknown/removed names cannot widen capability. Model schemas and tool execution
use the same gate inventory. Services, stores and Manager ownership are untouched.

RunArm now constructs the complete session, runs task setup, restricts tools, then
runs the model, matching Python. Ordinary CreateSessionRequest.ToolSelection still
filters before allocation. Seven Source recipes match native construction/setup
names, store identity across filtering, model schemas and manager store closure.
Seven actual RunArm tests compare model catalogues, setup-before-model and results.
Additional native tests prove removed write effects are denied, old snapshots stay
immutable, repeated restriction cannot restore tools, and live admission/history
guards reject mutation. Source run_arm has no explicit stop; native owned teardown
remains a deliberate lifecycle guarantee after success/failure/cancellation.

README canonical benchmark flow and boundary, EXTENDING, Go README and the
generated architecture are updated. Architecture checks passed 9/9, zero errors/
warnings, both SHA256 values verified. Visual inspection remains skipped after
the earlier browser denial. Full Go tests, vet and race passed:
go test ./... -count=1 -timeout=180s; go vet ./...;
go test -race -p=1 ./... -count=1 -timeout=180s. git diff --check passed.
Source gates passed: 124 focused Python tests, all 19 scan guards and complete
183-file exporter check. No Python runtime modules or default-tool types changed.
No dependencies or push. Full Python regression/coverage were not refreshed.
E6 broader profile audit, native SQLite and complete Go migration remain open.


## 2026-10-09 benchmark post-construction filter Source contracts

Baseline: `76772ef`. Seven actual Python benchmark.run_arm runs observe Manager
construction, task setup, catalogue filtering, model schemas and manager stop.
Enforce allocates before setup/filtering and retains the identical store for
empty, unknown-only and readonly tool selections. Off/shadow allocate none.
Unknown names and duplicates are ignored; original registry order is retained.
These Source contracts precede the native post-construction narrowing seam.
Validation: 124 focused Python tests, all 19 scan guards, complete 183-file
exporter check and git diff --check passed. No Python runtime modules changed.


## 2026-10-09 native efficiency startup settings activation

Baseline: `2f57665`. The pure runtimeconfig.ManagerEfficiency adapter maps validated
mode/style, persistence, raw threshold, TTL and capacities into named Manager
selections with freshly owned integers. Launcher and fake benchmark managers use
the same adapter. Mode/style startup refusal is removed only for these implemented
consumers; comprehensive MINILOOP_FEATURES remains unsupported. Off/normal stays
the default. App.Serve owns initialize-before-listening and ordered shutdown.

Nine actual native HTTP/listener/provider/shell scenarios match Source model
requests and metric-only receipts: all mode/style pairs, readonly, disabled
persistence, below-threshold raw output, and actual raw-reference recovery. Root
initialize/close reports are checked after real listener shutdown. The HTTP test
compares requests/receipts, not the fixture's private store fields; Manager and
child allocation/limits/closure have their separate existing tests. Two actual
benchmark HTTP reports now compare the separate efficiency fixture; the original
five profiles remain covered. These reports do not establish allocation-order
parity: Python filters an already constructed session, native RunArm still filters
before allocation. That difference must be closed without losing store ownership.

README canonical Go startup label/boundary, EXTENDING, Go README and generated
architecture are updated. Structural architecture checks passed 9/9 with zero
errors/warnings and both SHA256 values match the receipt. Visual inspection was
not retried after the earlier browser denial.

Validation: go test ./... -count=1 -timeout=180s, go vet ./..., complete 182-file
Source exporter check and git diff --check passed.
go test -race -p=1 ./... -count=1 -timeout=180s also passed.
No Python package modules/default tools, dependencies or push changed. Full Python
regression and coverage were not refreshed. Benchmark allocation order, SQLite,
broader profile audit and full Go port completion remain open.


## 2026-10-09 benchmark efficiency Source contracts

Baseline: `7f9d13c`. Two actual authenticated Python benchmark HTTP runs capture
request-time shadow and enforce/concise settings, four isolated fake-client arms
and workspace cleanup. A separate fixture preserves the existing five-case
Go benchmark comparison while native settings activation remains pending.
These reports do not establish parity of benchmark raw-store allocation order.
Validation: complete 182-file exporter check, existing benchmark HTTP comparison
and git diff --check passed. The preceding startup-contract commit also passed
124 focused Python tests, 19 scan guards and the full Go test suite.
No runtime code or dependencies changed; coverage was not refreshed.


## 2026-10-09 efficiency startup Source contracts

Baseline: `8456602`. Nine actual Python server-lifespan/HTTP scenarios capture
mode/style combinations, readonly authority, disabled persistence and the raw
threshold. Requests record caps, concise instructions, recovery capability and
actual Bash/result recovery; receipts normalize timing and ephemeral identities.
Store limits and shutdown closure are observed from the actual runtime.
E6.4c Go Launcher/HTTP/benchmark settings activation remains pending.
Validation: 124 focused Python tests, 19 scan guards, complete 181-file exporter
check, go test ./... -count=1 -timeout=180s and git diff --check passed.
Full Python regression, race/vet and coverage were not refreshed for this
Source-contract-only change.



## 2026-10-09 native entered-workspace child artifact allocation

Baseline: `3968369`. Added named efficiency.ArtifactLimits with validation/Clone
and detached RawStore.Limits. The raw-store constructor keeps identical validation
messages/order. ObservationRawConfig snapshots StoreLimits with default
3600/2 MB/20 MB; Manager forwards its selected TTL/capacities. Child snapshots
detach them and use Settings values, not a foreign store's configuration.

After role filtering and foreign-store detachment, writable enforced children
with recovery capability allocate a fresh RAM store in the entered workspace.
Readonly, filtered, off/shadow and disabled-persistence paths allocate none.
Recovery handlers bind child scope. Unpublished construction unwinds allocation.
Successful child-created stores are transient RAM held by that child runtime;
completion does not close reachable stores, matching Source. Shared component
root and managed session/template cleanup retain their existing ownership.

Seven actual native child runs match Source workspace reads, parameter values,
old-reference isolation, own-reference recovery and parent store preservation.
Parent close fences the distinct child clone, proving shared lifecycle identity.
Additional tests verify detached limits, exact TTL, capacity and item bounds.
README/EXTENDING and the generated map reflect the new allocation path.
Python gates passed: 82 focused tests, all 19 scan guards, complete 180-file
exporter check. Passed go test ./... -count=1 -timeout=180s,
go test -race -p=1 ./... -count=1 -timeout=180s, go vet ./... and
git diff --check. Architecture checks passed 9/9 with zero errors/warnings;
source/artifact SHA256 match the receipt. Visual inspection was not repeated
after the earlier browser access denial.

E6.4c next: launcher/HTTP/benchmark mode/style settings activation. SQLite,
broader profile audit and full port completion remain open. No dependencies or
push; full Python regression and coverage are not refreshed in this iteration.


## 2026-10-09 workspace-child artifact scope Source contracts

Baseline: `54c6acf`. Seven actual Agent.enter_workspace + InProcessSubagents runs
compare worker/Explore, filtered/explicit recovery, enforce/shadow/off and disabled
persistence. Real file tools read the entered workspace; real recovery tools
cannot retrieve old-workspace references. A writable enforce child with selected
recovery allocates a fresh store using Settings TTL/limits (7/200/800), rather than
the injected old store's 99/100/400. Readonly, filtered, off/shadow and disabled
persistence allocate none. Component lifecycle root remains shared.

Existing parent artifacts remain readable in their own store; child completion
leaves both reachable stores open. Child-created stores are transient RAM, not
managed session stores; explicit fixture teardown closes them. Only paths are
canonicalized for workspace identity. No timestamps, refs or random IDs exported.

Passed 82 focused Python tests, all 19 scan guards, complete 180-file contract
exporter check and git diff --check. Native allocation and parameter snapshots
are delivered separately. Launcher/HTTP/benchmark activation and full port remain
open. No dependency or push.


## 2026-10-09 native child efficiency inheritance and scoped recovery

Baseline: `65913d3`. SubagentParent now pins the shared three-stage runtime and
detached raw threshold/persistence settings. Same-workspace children retain the
store; native selected recovery handlers bind child identity. Default role policy
remains authoritative and no recovery tool is added after filtering. Explore is
readonly; explicit selected recovery reads existing artifacts without raw writes.
Concise opt-in and request projection ledger start fresh; RequireOptIn=false
policies still act through the shared root. Child completion does not close shared
components/stores. Foreign-workspace stores detach.

Eight native actual-child model runs match Source root/store identity, role
selection, all three stage callback counts, fresh state and recovered content.
A managed actual task test proves active child cancellation/join before Manager
store revocation and component close. README/EXTENDING and the generated map
are updated in this commit. Passed go test ./... -count=1 -timeout=180s,
go test -race -p=1 ./... -count=1 -timeout=180s, go vet ./... and
git diff --check. Architecture validation passed 9/9 with zero errors/warnings;
source and generated artifact SHA256 match the receipt. Visual inspection was
not repeated after the earlier browser access denial. Python Source gates:
82 focused tests, all 19 scan guards and the complete 179-file exporter check
passed. Full Python regression and coverage were not refreshed.

E6.4c remains open for launcher/HTTP/benchmark mode/style activation and
foreign-workspace child allocation/settings ownership. SQLite and the full port
remain incomplete. No dependencies, merge or push.


## 2026-10-09 child efficiency inheritance Source contracts

Baseline: `bc63773`. Added eight actual InProcessSubagents child runs, with real
read_file and selected read_token_artifact calls, capturing shared component root,
same-workspace store identity, role-selected recovery capability, fresh ledger,
empty parent history and response opt-in state. Worker, Explore, custom filtered
and custom readonly recovery roles are exercised across enforce/shadow/off,
required/default concise policy and disabled raw persistence.

Source children share components and matching raw stores; constructor state does
not inherit the parent's concise_response flag. Default Explore excludes recovery;
explicit readonly selection can recover existing masked data without store writes.
The parent's store stays open after the child finishes. Child request/response/
observation callbacks execute through the actual Agent. The default runtime owns
no child-specific initialize/close. No random refs, timestamps or paths are exported.

Validation: 82 focused Python efficiency/subagent tests, all 19 scan guards and
the complete 179-file exporter check passed. git diff --check passed. Native child
inheritance is a separate commit; launcher/HTTP/benchmark activation and foreign
workspace child allocation remain open. No dependency or push.


## 2026-10-09 Manager-owned efficiency composition and lifecycle

Baseline: `50dd0a2`. The library Manager now selects default or injected shared
runtimes, preserves caller catalogue membership, forwards raw threshold 16384 and
allocates stores only after capability filtering. Writable enforce sessions get
private RAM stores; readonly gets no automatic recovery tool. Same-workspace
templates retain identity, including readonly reuse; foreign templates detach.
New, fork, restore and teammate constructors share the managed binding. Failed
publication unwinds store ownership. Delete waits for consumers and releases the
last live holder; workflow cancellation faults retain stores until shutdown.
Stop drains sessions, cron, workflows and cleanup, revokes every distinct store,
then closes the shared component root. Start hooks run once with no service
authority; EfficiencyStatus returns detached lifecycle reports.

Native tests compare session/store selection and lifecycle reports with all 14
Python Manager recipes, including running consumer cancellation before close.
Additional tests cover empty capability filtering, failed state publication and
restored-store deletion. Passed go test ./... -count=1 -timeout=180s, full
go test -race -p=1 ./... -count=1 -timeout=180s, the final Manager tests under race,
go vet ./... and git diff --check. Fresh Go statement coverage across ./... (duplicate blocks merged, including
command packages) is
25363/29145 = 87.02% from /tmp/mini-loop-manager-final-coverage.out.
README architecture and generated map are updated; validation passed 9/9 with
zero errors/warnings and both SHA256 hashes verified. Visual inspection was not
repeated after the earlier browser access denial. Full Python regression was
not rerun; there is no merge or push.

E6.4c remains open: launcher/HTTP/benchmark settings activation and child
inheritance/broader custom profiles. Existing launcher mode/style refusal remains.
The complete Go port and SQLite backend remain unfinished.


## 2026-10-09 typed Manager efficiency selection helpers

Baseline: `33ca813`. Added named ManagerEfficiencyConfig with off/normal defaults,
Settings raw threshold 16384, detached integer limits and finite validated TTL.
The default runtime factory selects the deterministic reducer for non-off modes
and concise response policy with RequireOptIn=false for concise style.
Six mode/style combinations and invalid/aliased limits are tested. These helpers
are a separate prerequisite commit; Manager ownership/lifecycle wiring and its
14 Source comparisons are delivered separately. Launcher flags and full port
completion remain pending. No dependencies or push.

## 2026-10-09 Manager efficiency composition Source contracts

Baseline `098eb57`. Fourteen real SessionManager recipes capture six mode/style
combinations, caller-owned empty catalogue preservation, disabled raw persistence,
injected runtime precedence, foreign/shared template stores, contained initialize/
close faults and a live managed tool consumer during shutdown. Actual start/create/
delete/stop methods, registry/runtime, hooks and masked RAM stores run unchanged;
only lifecycle elapsed metrics are normalized. Complete component descriptors and
binding modes, per-session store/tool/workspace identity, authority text, delete/
sharing/revocation outcomes, lifecycle reports, hook order and stopped admission are
captured. Random refs/session IDs/paths are not part of these observed projections.

The real live turn uses a scripted model and an actual safe printf command. Its
reducer blocks cooperatively; Manager.stop cancels and joins it before store
revocation and shared close. The trace proves initialize -> reduce -> cancellation
-> close with all stores already closed. Repeated start/stop does not rerun hooks.
Lifecycle plugin faults remain report records and do not prevent authoritative
cleanup. Explicit injected runtimes keep their own mode/components. Template stores
matching the same workspace can be shared, including by readonly sessions; default
automatic allocation excludes readonly. Delete retains a shared live store and stop
revokes every distinct template/session store.

Next E6.4 work is split into (a) typed Manager default/injected composition plus
private catalogue preservation, new/fork/restored session raw binding and owned
store cleanup; (b) shared initialize/close reports and drain/order/idempotency; then
(c) launcher/HTTP/benchmark setting translation and removal of mode/style refusal
only after their actual flows are verified. Default off/normal, authority boundaries
and the full Go port remain unchanged by this Source-only capture. Agent fallback
raw threshold is 4096; actual Settings default is 16384 and must be forwarded by
service composition rather than replaced with that fallback.

Validation: 74 targeted Python efficiency tests and all 19 scan guards passed.
The complete exporter check passed after the final live-consumer recipe: 178 files.
git diff --check and README/helper outlines passed. No Python package module or Go
implementation changed; full Python regression, Go gates and coverage were not
rerun for this Source-only capture. No dependencies, merge or push were added.


## 2026-10-09 explicit Agent observation projection adoption

Baseline `0ce795c`. Session dispatch now binds a named private toolOutcomeProjector
between full-output masking and journal settlement/result observers. The concrete
consumer uses the existing shared efficiency.Runtime and closed observation/command
metadata, sorted post-mask query and token-threshold budget. Replay and
read_token_artifact bypass reduction. Denials and ordinary hook/handler errors still
pass through the post-mask stage. The bare ToolGate seam remains unchanged.

Action settlement retains full masked authority while observers, tool_result events,
trajectory and model tool results receive the projection. Candidates and recovery
markers are masked again. Recovery envelope inflation discards the unused ref and
rewrites applied public metrics to full authority; successful metrics include the
final envelope and final mask. Post-mask cancellation preserves Source's started
journal record. Early handler cancellation keeps the existing cancellation settlement.

ObservationRawConfig has named persist/minimum-byte settings, cloned unbounded
thresholds and Source defaults true/4096. readonly and absent recovery tools forbid
writes. Explicit runtime and recovery tool bindings must use the same nonnil store;
foreign-workspace runtime stores are detached without changing the caller's runtime.
Store/tool creation, session-owned cleanup, initialization, environment selection and
Manager/launcher/benchmark/HTTP defaults remain E6.4 work. No default feature is
activated or dependency added. A typed optimization_stage_error variant exposes
only a safe native fault class and tool span, with event masking/archival decoding.
Native runtime fault class wording is explicit and is not claimed as arbitrary
Python exception-class parity.

All 17 actual Source tool scenarios match reducer inputs, full public receipt
metrics, masked raw artifacts, authority journals, observer/provider outputs and
call ordering (only opaque ref and span/elapsed identity normalized). A real
Session.Run test performs bash -> projected result -> read_token_artifact -> model
reply and confirms only the first tool is reduced, recovered output stays complete
and masked, and the journal contains no ephemeral recovery projection. Targeted
race tests cover the differential corpus, live flow, detached/foreign store settings
and typed stage-error archival decoding. Wider malformed/custom runtime profiles
and full port parity remain open; E6.3 is delivered for the supported typed boundary.

Validation: full Go ordinary and race suites passed with `-p=1 -count=1
-timeout=180s`; `go vet ./...` passed. Targeted observation differential/live/binding
race tests passed. The 74 targeted Python efficiency tests, all 19 scan guards and
complete 177-file exporter check passed for the Source contract commit in this
iteration. git diff --check passed. Architecture regeneration passed 9/9 checks,
with zero errors/warnings and file hashes matching its receipt. Visual inspection
remains skipped after prior browser access denial. Full Python regression and
coverage were not refreshed; no merge or push was performed.


## 2026-10-09 Agent observation adoption Source contracts

Baseline `ef26580`. A separate helper captures 17 actual Agent._exec_tool calls
through the real registry, efficiency runtime, hooks, action journal and masked RAM
artifact store. The snapshots retain reducer inputs/query/budget, complete public
receipt metrics, returned/display/observer outputs, call order, journal authority,
remaining raw artifacts and cancellation. Only artifact entropy, receipt elapsed
time and tool parent-span identity are fixed; no dispatcher or reducer runtime is
replaced. Replay uses the same explicit message/tool identity.

The corpus covers lossless/recoverable reduction, recovery-envelope inflation and
discard, readonly/no-persistence/no-recovery-tool refusal, shadow/off, secret-emitting
candidates/warnings, journal replay, denial, structured nonzero-exit command output,
JSON/malformed JSON classification, component failure, cancellation and recovery-tool
bypass. Journal results retain full masked authority; observers receive projections.
The envelope guard rewrites applied receipt metrics to the actual final masked
provider output and discards unused refs when the envelope is not smaller.
Cancellation at this post-mask stage propagates while Source leaves the journal
started; this behavior is captured, not silently changed by the fixture.

Validation: 74 targeted Python efficiency tests, all 19 scan guards and the complete
177-file exporter check pass. git diff --check passes. Python package modules and
mutation anchors were unchanged; full Python regression and coverage were not
refreshed. Native E6.3 observation adoption and E6.4 defaults/lifecycle remain open.


## 2026-10-09 typed Agent request guard and projection ledger

Baseline `b9fe20f`. Session now runs the explicit request optimizer between response
planning and cache annotation. Contexts contain only a detached provider message
copy, frozen prefix count, ownership and configured token-threshold budget. A newest
assistant freezes every row; otherwise the newest delta remains eligible. The full
protocol shape protects message metadata/roles/count, tool IDs/input/error flags,
thinking/signature, media/unknown blocks and cache metadata. Safe text/result payloads
are lowered into concrete protocol variants; no domain map/any/RawMessage is added.

The process-local ledger is keyed by named row index plus a fixed SHA256 digest of
authoritative content. Live agent_turn calls reuse earlier projections before
optimizing, prune obsolete keys on replacement/truncation and store only differing
provider rows. Side calls do not acquire ledger or live-history/meter ownership.
Authoritative messages, envelope/model/tools and persisted history remain unchanged.
Malformed provider candidates retain the previous projection and emit the typed
request_optimization_rejected event; event JSON and archival decoding support it.
Only supported typed text/result payloads are lowered. Annotation-violating payload
types and additional provider block variants are not claimed by this consumer.

Protected side calls bypass request optimizers and cache annotation, remask detached
selected messages and restore both message bytes and their cache annotations before
every recovery dispatch. Personal-skill previews now use this path, matching actual
Source immutable_messages=True. Their old cache-required test was corrected from
source evidence. Shared runtime lifecycle ownership remains caller-controlled.

Sixteen actual Agent recipes match contexts, public receipt metrics, call order,
provider messages, authoritative histories and per-round ledger digests. Fifteen
actual shape recipes match opaque/unknown/media/thinking/cache/nonfinite semantics.
Native tests confirm live Run/default-cache prefix reuse, preserved authority,
protected recovery tampering refusal and rows larger than the unrelated wire decoder
cap. JSON object key order is compared semantically; integer/float identity remains
observable. Broader numeric/surrogate/provider profiles remain separate audit gates.
E6.2 is delivered for the supported provider boundary; observation adoption E6.3,
Manager/launcher/benchmark/HTTP/default composition E6.4 and the full Go port remain
open. No environment mode/style or manager default is enabled.

Validation: full Go ordinary and race suites passed with `-p=1 -count=1
-timeout=180s`; `go vet ./...` passed. The 74 targeted Python efficiency tests,
all 19 scan guards and the complete 176-file Python-to-Go exporter check passed.
`git diff --check` passed. Architecture regeneration passed 9/9 checks with no
errors or warnings, and both file hashes matched the generation receipt. Visual
inspection remains skipped after browser access was denied. Full Python regression
and coverage were not refreshed; no merge or push was performed.

## 2026-10-09 Agent request projection Source contracts

Baseline `01f2549`. Sixteen recipes execute actual Agent._create over the real
request runtime, NullCachePolicy spy and fake-client transport. Inputs include
frozen counts/budgets/ownership; outputs include complete public receipt metrics,
provider message copies, authoritative history and ordered projection ledger digests.
Cases cover live/side/compaction calls, off/shadow, prefix/assistant freezing, role/
count/tool-result ID/error-bit protection, cancellation, protected queries and
recovery message/cache-metadata tampering. The multi-round recipe grows history,
replaces an authoritative row and drops the tail, recording reuse/pruning each time.

Fifteen standalone actual _message_protocol_shape recipes cover string/array/opaque
content, malformed roles, complete tool inputs, thinking/signature, cache metadata,
images, unknown types, integer/float identity and legacy NaN repr fallback. These
recipes capture the guard function separately from native provider lowering.
Only elapsed_ms and event wrapper identity fields are normalized; JSON object key
order is compared semantically, preserving scalar variants and all metric/digest data.

Validation: 74 focused Source efficiency tests, all 19 scan guards and the full
176-file exporter --check pass; git diff --check passes. This capture does not claim
native adoption or full Python regression/coverage/mutation verification.

## 2026-10-09 explicit Agent response policy adoption

Baseline `91d6634`. RuntimeConfig.TokenEfficiency accepts the typed shared runtime;
ConciseResponse is the explicit opt-in state. Nil runtime preserves existing behavior.
On agent_turn purpose, Session builds the response context from the latest user text,
requested output limit, matching budget and opt-in before cache annotation. It emits
public receipt metrics, appends instructions to a detached optional system string
and adopts only a tighter limit. Side requests with agent_turn purpose also plan
responses without acquiring live-history/meter ownership. Other purposes skip it.
Verbosity/metadata are not forwarded to the provider, matching current Source Agent.

OptimizationMetrics is a closed event payload with unbounded numeric metrics and
nullable raw reference/parent span. Digests, warning strings and content have no
fields; warning_count is retained. Event accessors, masking and record clones detach
numeric/pointer data. JSON event encoding and archival decoding support the explicit
optimization_receipt variant; decoding validates enum, finite duration and nonnegative
metric boundaries. Maskable identifier/reason strings remain inert archival data.

Fourteen actual Source Agent recipes match policy contexts, complete public metrics,
cache/provider/receipt order and provider arguments, with elapsed_ms normalized.
Caller request values remain unchanged; cancellation occurs before cache/provider.
Native live Run tests confirm tighter caps and policy guidance in the default cache,
no instruction accumulation across turns and no response/cache fields in history.
Additional tests verify 101-digit metric copies, event masking, stored event round-trip
and malformed metric rejection. No generic domain map, RawMessage or dependency added.

This delivers E6.1 only. Request protocol guard/projection ledger (E6.2), masked tool
observation adoption and authority/recovery handling (E6.3), then Manager/launcher/
benchmark/HTTP/default composition and shared shutdown (E6.4) remain open. Runtime
ownership stays explicit with the caller; this constructor does not initialize or
close shared components. No environment mode/style or manager default is activated.

Validation: targeted response/event/live-run race, full go test ./... -p=1 -count=1
-timeout=180s, full go test -race ./... -p=1 -count=1 -timeout=180s and go vet ./...
pass on the final Go files. Source capture was committed separately after 74 focused
efficiency tests, all 19 scan guards and a full 174-file exporter --check.
README outline and git diff --check pass. Architecture delivery passes 9/9 showcase
checks with zero errors/warnings; specification SHA256 d01da17c968109717ff63b49e4c7ff9b5a9d8c43527f19618440534d063abdad;
HTML SHA256 95f082470689ff4cce02b0f74003f8e181e5f99d5ba3e6ac27311ba5561a7a04.
Visual review remains skipped after prior browser access denial; correction_rounds: 0.
Full Python regression, coverage and mutation sweep are not refreshed. The previous
coverage snapshot is pinned to 6feeba2 and excludes later runtime/adoption changes.
Evidence: Darwin arm64, local execution.

## 2026-10-09 Agent response adoption Source contracts

Baseline `8bb03db`. Fourteen recipes execute actual Agent._create with the real
response runtime and actual concise policy. Captures cover purpose selection,
opt-in, shadow/off, existing output caps, zero selecting the configured default,
nil/empty system prompts, bounded multi-instruction guidance, failure/cancellation
and latest user text-block task labels. A NullCachePolicy spy records annotation;
the real fake-client transport records actual provider arguments. Policy contexts,
cache/provider/receipt order and metrics match real methods; caller messages remain
unchanged. Receipt wrapper identity fields are excluded, with only elapsed_ms
normalized in the public metrics (including nullable parent_span_id).

Validation: 74 focused Source efficiency tests, all 19 scan guards and the full
174-file exporter --check pass. git diff --check passes. This capture does not
claim Go adoption, full Python regression/coverage or a mutation sweep.

## 2026-10-09 typed multi-stage registry and shared runtime

Baseline `38de135`. Registry has explicit observation/request/response registration,
global ID admission and detached membership/mode snapshots. Registration validates
mode before descriptor stage before global duplicates and returns the same instance.
The zero value is ready; registration and snapshots are synchronized. No package
search, generic service payload or dependency is introduced.

StageComponents names three distinct SPI binding lists. The direct construction seam
preserves Source repeated-ID and descriptor-stage behavior rather than applying
registry-only admission rules. Runtime combines those lists in observation/request/
response order with one admission/lifecycle/token-counter root. Shared pointer
instances use one call lock and the first enabled binding for lifecycle reports;
distinct instances with equal descriptor IDs remain distinct. Stateful Go components
must use pointer instances to retain object identity; value components are copies.
WithRawStore clones share all root state while retaining caller-owned storage.
Fresh runtime construction makes a separate root. Existing individual stage libraries
remain valid independent roots.

Ten actual Source recipes match registrations, refusals, snapshots, all stage calls,
normalized full lifecycle reports, clone caches and reverse close order. Native
concurrency tests confirm cross-stage lock identity, queued request cancellation,
health exclusion, global close drain/retry and response fencing during teardown.
Snapshots detach slice/mode fields; caller mutation does not disable installed work.
Descriptors and hook capabilities are pinned on runtime construction as in existing
stage libraries. Mutating trusted plugin descriptors after construction is not claimed.
Agent/Manager adoption, provider capability lowering and default mode/style activation
remain pending E4/E6. This delivery does not complete the Go port.

Validation: targeted efficiency race, full go test ./... -p=1 -count=1 -timeout=180s,
full go test -race ./... -p=1 -count=1 -timeout=180s and go vet ./... pass.
The separately committed Source capture passed 74 focused tests, all 19 scan guards
and the full 173-file exporter check. README outline and git diff --check pass.
Architecture delivery passes 9/9 showcase checks with zero errors/warnings;
specification SHA256 f56ed0e5b74da8844412adbcb454d3bb2a50b9338b431e35d80bc857d262669a;
HTML SHA256 d208dcf8d53570eddc80f45121e298f80646909db87eaab0ab967cee4755de91.
Visual review is skipped after prior browser access denial; correction_rounds: 0.
Full Python regression, coverage and mutation sweep are not refreshed this slice.
The previous coverage snapshot is pinned to 6feeba2 and excludes this implementation.
Evidence: Darwin arm64, local execution.

## 2026-10-09 multi-stage registry Source contracts

Baseline `6cd2260`. Ten recipes execute actual TokenEfficiencyRegistry and
TokenEfficiencyRuntime methods. They capture stage/global-ID admission, mode and
wrong-stage refusal order, immutable membership snapshots, grouped stage order,
clone-cached lifecycle, reverse close order and actual calls in all three stages.
Direct StageComponents recipes separately capture shared-object deduplication,
first-enabled binding selection and distinct objects with identical descriptor IDs.
Only elapsed_ms is normalized. This capture alone does not install Go composition.

Validation: 74 focused Source efficiency tests pass; all 19 scanning guards pass;
full exporter --check confirms 173 files; git diff --check passes. Python full
regression/coverage and mutation sweep are not refreshed.

## 2026-10-09 typed response settings and policy library

Baseline 5e4dffa. StableResponseSettings names bounded instruction strings, ordered
ResponseMetadata pairs, optional unbounded output caps and optional verbosity.
Constructors enforce Source count/character/uniqueness/positive-value limits and
snapshots detach all mutable fields. ResponseContext names task, provider capabilities,
budget, opt-in and deduplicated ownership; ResponsePlan names settings/warnings.
ResponsePlanner is an explicit typed SPI. No generic domain map, any/interface{},
RawMessage payload or new dependency is introduced.

ResponsePipeline pins response bindings/modes, revalidates candidate settings and
warnings, preserves off/double ordering, unchanged-before-output-cap and shadow-before-
output-cap checks and adopts only enforced safe candidates. Existing output limits
and context budget define the tighter ceiling; removing or raising it is degraded.
Ordinary faults retain the previous settings with bounded class-only reasons, while
cancellation escapes. The Source response stage does not enforce descriptor input
byte caps or byte/token inflation on settings; native planning retains that behavior.
Counter inputs, hashes and sizes use exact StableRequestSettings dataclass repr.

PassthroughResponsePlanner and opt-in ConciseResponsePlanner port actual Source
builtins. Concise adds the configured instruction once, chooses the tighter
existing/config/context output limit, sets concise verbosity and replaces policy
metadata at the end while preserving other order. Configuration permits a long
nonblank instruction; producing settings applies normal Source bounds and fails
open through the runtime. Its configured numeric cap is detached from caller state.

Nineteen actual Source constructor recipes match settings, refused boundaries and
representation. Forty-eight actual Source runtime recipes match full contexts,
receipts, policy call order and every token-counter input, normalizing only elapsed_ms.
Native tests verify detached settings/context/policy values, malformed metadata pairs,
Unicode whitespace, positive limits, pinned caller modes/bindings, confirmed queued
clone cancellation, health exclusion, close fencing/drain/retry and missing hooks.
Each independently constructed stage is still a separate lifecycle root. E4 provider
capability lowering, E5 multi-stage identity registration and E6 Agent/Manager/default
composition remain open. No default response style or efficiency mode is activated.

Validation: targeted efficiency race, full go test ./... -p=1 -count=1 -timeout=180s,
full go test -race ./... -p=1 -count=1 -timeout=180s and go vet ./... pass. Source
capture was committed separately after 48 focused efficiency tests, a 172-file exporter
check and all 19 scan guards. README outline and git diff --check pass. Archify
passes 9/9 with zero errors/warnings; specification SHA256 b5ff4d9e674f475cd5541434ee4018ad85159c505b4615320c690e7ef73e1e6c; HTML dbb96daa4b69beda1734aa59928be1143370fd61ca430a1fa0049ff9c49877f0.
Visual inspection remains skipped after prior browser access denial. Full Python
regression, coverage and mutation sweep are not refreshed. Evidence: Darwin arm64.

## 2026-10-09 response policy Source contracts

Baseline 4755905. Nineteen StableRequestSettings constructors capture bounds,
ordered metadata/uniqueness, unbounded output limits, Unicode character counts and
exact dataclass repr. Forty-eight actual response-runtime recipes cover modes,
unchanged-before-cap and shadow-before-cap ordering, existing/budget ceilings,
double ownership, cap removal/raising, safe faults, cancellation/timeout, forged
settings/warnings, counter fallback, closed receipts and actual passthrough/concise
policies. Full context/receipt projections and all counter inputs are retained;
only elapsed_ms is normalized. Descriptor max_input_bytes is not enforced by this
Source response-stage implementation. No native response runtime is added here.

Validation: 48 focused Source efficiency tests, exporter --check (172 files), all
19 source scan guards and git diff --check. Full Python regression, coverage,
mutation sweeps and Go gates are not refreshed for this capture-only commit.

## 2026-10-09 typed request optimization library

Baseline ce898e8. NewRequestDocument represents the provider-neutral request as an
immutable closed JSON Object; OptimizationContext names detached unbounded frozen
prefix counts and stable ownership markers, and RequestOptimizationResult names
bounded warnings. RequestOptimizer uses explicit arguments/results. No any,
interface{}, generic domain map, RawMessage payload or dependency is introduced.
The document is genuine open provider JSON: native consumers must lower projected
values into their concrete protocol types before dispatch. Non-string Python keys,
opaque Python objects and extreme nesting/annotation-violating profiles stay open.

RequestPipeline pins typed bindings/modes/descriptors and implements ordered off,
double-owner/input-cap skips, frozen-prefix-before-passthrough/shadow, byte/token
inflation and enforce adoption. Safe immutable receipts preserve last-good request
copies on ordinary faults; cancellation escapes. Stable text sorts finite JSON keys,
retains unbounded integers/float identity, uses Source object repr for nonfinite
values and replaces surrogate characters with question marks at the byte boundary.
Counter call ordering and fallback match Source. PassthroughRequestOptimizer ports
the actual structured no-op. Source library intentionally does not guard envelope
fields or role/tool identities at zero prefix; those belong to later Agent adoption.

Private componentBinding/componentRuntime now own named hook capabilities, pinned
descriptors, per-component serialization, counters and shared lifecycle state for
both library implementations. Observation payloads and request documents remain
stage-specific. Request clones share initialize/health/close reports and admission;
root construction is still independent per stage, without a multi-stage registry.
Observation WithRawStore continues to share its existing root and caller-owned store.

Forty real Source recipes (39 controlled optimizer cases plus the actual builtin)
match complete contexts/receipts, call arguments/order and every counter input; only
elapsed_ms is normalized. Input preservation, frozen big integers/markers/modes,
clone serialization with confirmed queued cancellation, health exclusion, close
fencing/drain/retry and missing request hooks have native evidence. Existing 38
observation and nine lifecycle differential recipes continue to pass after extracting
the common coordinator. E3 Agent identity/envelope/ledger and cache/transcript adoption,
E4 response policies, E5 multi-stage identities and E6 runtime selection remain open.
Default efficiency/style refusal is intact; this is not full runtime activation.

Validation: targeted efficiency race, full go test ./... -p=1 -count=1 -timeout=180s,
full go test -race ./... -p=1 -count=1 -timeout=180s and go vet ./... pass. Final
Source efficiency suite passes 48 tests, exporter --check confirms 171 files and all
19 source scan guards pass. README outline and git diff --check pass. Archify delivery
passes 9/9 with zero errors/warnings; spec SHA256
fc89fc843763458213ed1ac3188fbec38a423136fce74893ad46030b3b4f83b3; HTML
6ac34c9343fb240164dd531da9b6df1cd61a89ceb75735798669b7953058d910.
Visual inspection remains skipped after prior browser access denial. Full Python
regression, coverage and mutation sweep are not refreshed. Evidence: Darwin arm64.

## 2026-10-09 request optimizer Source contracts

Baseline b45bc58. Thirty-nine actual TokenEfficiencyRuntime request recipes capture
ordered off/shadow/enforce, detached plugin inputs, frozen-prefix-before-shadow,
unbounded prefix/budget/counter integers, byte/token inflation, double ownership,
input caps, safe fault/cancellation/timeout, malformed results/warnings, counter
fallback, closed receipts, nonfinite repr fallback and surrogate UTF-8 replacement.
Full receipt projections, argument order, token-counter inputs and unchanged caller
requests are retained; only elapsed_ms is normalized. Source library protects only
the declared message prefix: envelope changes and protocol-shape changes at zero
prefix remain visible. Agent separately guards role/tool identities and retains its
projection ledger; native Agent adoption is not part of this capture.

Validation: 48 focused Source efficiency tests, exporter --check (171 files), all
19 source scan guards and git diff --check. Full Python regression, coverage,
mutation sweeps and Go gates are not refreshed by this capture-only commit.

## 2026-10-09 shared observation lifecycle admission

Baseline 08ef321. ObservationPipeline clones now share a lifecycle coordinator,
per-component serialization, cached initialize/close reports and a call-drain event.
Initialize excludes new observation calls and runs enabled hooks in order; optional
synchronous health excludes concurrent observations and returns bounded typed
reports. Close fences new admission before draining all admitted calls (including
queued reducer calls and health), then invokes enabled hooks in reverse order.
Drain/lock deadline failure is not cached, so teardown can be retried after drain.
Completed close reports are cached even when hooks fail. Hook cancellation escapes;
initialization cancellation clears its temporary gate and leaves no partial cache.
Ordinary errors/panics retain safe class-only details and do not abort later hooks.
Busy/closed observation receipts cover all bindings, including effectively off ones.

LifecycleServices exposes an explicit caller-owned RawStore reference; close hooks
receive detached positive finite deadline seconds. No generic service object or
new dependency is introduced. Typed health/records/reports validate closed enums,
machine codes and elapsed time and detach record projections. Cooperative hooks
must honor context; synchronous health must return promptly. No detached worker is
used and this component lifecycle does not close caller-owned raw artifacts.

Nine real Source lifecycle recipes match normalized reports, health states,
closed observation receipts and event order. Native tests also verify clone-shared
admission, canceled initialization/retry, concurrent close-once, timed drain/retry,
health exclusion/drain, canceled calls/close, close-before-initialize, reverse hook
budgets/error caching, panic isolation and immutable report projections.

This closes observation-stage lifecycle orchestration only. E5 multi-stage identity
registration/deduplication, E3 request optimizers, E4 response policies and E6 Agent/
Manager/default composition remain open. Cross-root component ownership and extreme
Source timeout/float/Unicode or annotation-violating plugin profiles are not claimed.
MINILOOP_TOKEN_EFFICIENCY_MODE/non-normal response startup refusal remains intact.

Validation: targeted efficiency race, full go test ./... -p=1 -count=1 -timeout=180s,
serialized full go test -race ./... -p=1 -count=1 -timeout=180s and go vet ./... pass.
The initial concurrent-package normal run crossed a real minute boundary in
TestImprovementHTTPAdmissionAndNoRateSpending (200 instead of expected 429).
The unchanged test passed 10 isolated repetitions; the test now pins its existing
Config.Now seam and passes 10 race repetitions (separate test-only commit).
The first serialized full race run then timed out in the unrelated 144-case live
personal-skill depth fixture test: the test normalizer repeatedly decoded/marshaled
every nested subtree. A separate test-only commit changes it to one UseNumber decode,
one traversal and one encode, with large/lexical-number, deep-shape and replacement
regressions. The depth race test passes in 26.508s; the complete HTTP race package
passes in 42.040s. All other packages passed the earlier full race run.
Final serialized full normal/race gates are rerun after both test repairs.
Source lifecycle capture committed separately after 48 focused efficiency tests,
170-file exporter check and all 19 scan guards. README outline and git diff --check
pass. Archify delivery passes 9/9 with zero errors/warnings: specification SHA256
31873f78b8568602080fd634f13cf14441e022cee6c04a96603d9de4385ad6b6; HTML
230eed00f99cfb3bb99f17da3ff995b05f880af2061768a468753ebcd5d8194a.
Visual inspection remains skipped after prior browser access denial. Full Python
regression, coverage and mutation sweep are not refreshed. Evidence: Darwin arm64.

## 2026-10-09 efficiency lifecycle Source contracts

Baseline 54c42b0. Nine real TokenEfficiencyRuntime recipes capture ordered enabled
initialize/health/reverse-close hooks, per-root clone-shared cached reports, off and
missing hooks, contained class-only errors, cooperative timeout, safe/invalid health
results and closed observation receipts. Only elapsed_ms is normalized. Components
are controlled probes; runtime coordination and report validation are Source code.
This capture does not implement native lifecycle or activate Agent efficiency.

Validation: focused Source efficiency tests, exporter --check (170 files), all
19 source scan guards and git diff --check. Full Python regression, coverage,
mutation sweeps and Go gates are not refreshed by this capture-only commit.

## 2026-10-09 typed observation orchestration library

Baseline 91137e3. ObservationPipeline accepts explicit ordered ReducerBindingConfig
and immutable mode/descriptor snapshots, named TokenCounter and optional RawStore.
It rejects duplicate component IDs/non-observation stages and reports whether any
binding is enforced. ReduceObservation preserves off-before-double ordering, MIME/
input byte guards, unchanged passthrough, shadow-before-inflation, byte/token
inflation and first-owner reduction. Source-compatible recoverable admission stores
only the current already-masked text before adoption; unavailable/failing storage
keeps the last good observation and propagates storage warnings to later receipts.
Existing recovery refs are retained. Outcome projections detach metrics/references.

Ordinary component faults produce bounded class-only reasons and unchanged receipt
payloads; unsafe class names project to component_error. Invalid concrete results
fail open as TypeError, invalid candidates/warnings as their named faults, and
cooperative context timeout projects TimeoutError. Cancellation escapes instead of
being converted into a successful fallback. Counters retain unbounded integer metrics
and fall back on error/negative/nil/panic. WithRawStore clones share per-component
serialization and synchronized counter pairs while pinning separate raw stores.

All 38 actual Source runtime recipes match full projections, receipt fields (only
elapsed_ms normalized), call order/arguments, enforced state and recovered masked
text. Native tests confirm frozen caller modes/bindings, clone serialization,
confirmed queued cancellation, stateful shared counters, copied outcome projections,
constructor refusal, counter-panic fallback and argument snapshots before counters
can mutate caller-owned pointers. Source only controls reducer SPI,
raw entropy/store clock; its runtime orchestration and store remain real.

This is an explicit observation-stage library, not the full TokenEfficiencyRuntime
or Agent activation. E5 initialize/health/close admission/drain, cross-root component
identity and extremely large Source timeout/overflow scheduler profiles remain open.
Large Go duration scheduling saturates while descriptor metadata stays unbounded;
no parity is claimed for that scheduler boundary. Agent masking before/after reducers,
privacy-safe event/journal/trajectory projections, automatic session store binding and
E3-E6 remain open. Unsupported efficiency mode/style startup checks remain intact.
No dependency or any/interface{}/generic domain map is introduced.

Validation: full go test ./... -count=1 -timeout=180s, full go test -race ./...
-p=1 -count=1 -timeout=180s and go vet ./... pass; focused pipeline race passes.
The initial concurrent-package race run hit the pre-existing AST Launcher 1-second
probe timing failure under load; its isolated race rerun and serialized full race
pass. No AST behavior/fixture change is included.
Source exporter --check confirms 169 files, all 19 scan guards pass and Source
tools/efficiency suite passes 52 tests. README outline and git diff --check pass.
Archify delivery passes 9/9 with zero errors/warnings; spec SHA256
2ac6791db1520cda303ec1b41351771b45fae15893fed0d4b82b799728a5c28e;
HTML 8741dde1cfb9286705b02ce1fdaa80390cfe3c8d4ec1e4c2b0bf4ae9b756789e.
Visual inspection remains skipped after prior browser access denial. Full Python
regression, coverage and mutation sweep are not refreshed. Evidence is Darwin arm64.

## 2026-10-09 observation pipeline Source contracts

Baseline dbfe672. Export 169 captures 38 real TokenEfficiencyRuntime observation
scenarios through a controlled reducer SPI. Runtime orchestration, receipts,
warning propagation and raw-store operations remain actual Source. Only raw entropy
and the store clock are controlled; elapsed_ms alone is normalized after capture.

Cases pin default/per-component modes, shadow-before-inflation ordering, unchanged
results, byte/token inflation, double-reduction guards and disabled guards, MIME/input
admission, recoverable storage availability/closed/size faults, existing references,
last-good projection fallback, downstream warning propagation, duplicate outcome
warnings, component/unsafe class faults, invalid result/warnings, cooperative timeout,
cancellation escape, broken/negative/unbounded counters and metadata retention.
Captures retain full observation, receipt projections, call arguments/order, enforced
status and recovered masked text. Native orchestration follows; lifecycle initialization,
health/close admission and actual Agent/default selection remain separate E5/E6 gates.

Validation: Source tools/efficiency suite passes 52 tests, exporter --check confirms
169 files and all 19 scan guards pass. README outline and git diff --check pass.
Full Python regression, coverage and mutation sweep are not refreshed.

## 2026-10-09 typed observations and builtin reducer libraries

Baseline 824d130. efficiency adds ObservationConfig/MaskedObservation,
ReductionConfig/ObservationReduction, ReductionOptions and the concrete-result
ObservationReducer SPI. Constructors own detached slices/pointers and snapshots
copy them again; zero objects refuse marshaling. Ordered ObservationMetadata entries
preserve repeated keys and closed immutable JSON values, including real command
status scalars and nested values. Source's observation boundary deduplicates
reduced_by and checks MIME-like text and ref/digest pairing only; invalid-looking
paired legacy refs/digests remain admitted here, with stronger validation retained
at receipts and raw recovery. Reduction warnings keep Source bounds/messages.

PassthroughObservationReducer and DeterministicLosslessReducer are stateless explicit
libraries. The latter removes actual Source CSI/OSC escapes, folds CR-only/blank
runs and noncritical duplicates, and retains singular/plural markers and warning
order. Native critical-line matching uses pinned Python Unicode-14 word/decimal
classes and ASCII IGNORECASE extension letters, rather than Go ASCII boundaries.
Source's faile/failed versus fail behavior, extension boundaries and greedy OSC ST
removal remain exact. Both descriptors match actual Source constructor snapshots.

All twelve observation and eight reduction constructors compare normalized fields,
observation JSON or exact validation messages. Both reducers compare full content
and warnings on 69 actual Source inputs and repeated invocations. Native checks
mutate caller/snapshot slices, pointer metadata and nested array copies, reject
zero/cancelled inputs and run 32 shared concurrent stateless calls under race.
Arbitrary Python metadata objects, annotation-violating values and broader Unicode/
numeric wire profiles remain open; no generic map or dynamic result object enters
the reducer SPI. Metadata uses the existing closed JSON sum, not any/interface{}.

This delivers E2 types/builtin algorithms only. Runtime registry/modes/admission,
component deadlines/health/drain, inflation/double-reduction guards, recoverable
storage and fail-open receipts remain pending. Neither reducer is installed in the
Agent pipeline; efficiency mode/style activation remains unsupported. E3-E6 and
the full migration objective stay open.

Validation: full go test ./... -count=1 -timeout=180s, full go test -race ./...
-count=1 -timeout=180s and go vet ./... pass; focused observation/builtin race passes.
Source exporter --check confirms 168 files, all 19 scan guards pass, and Source
tools/efficiency tests pass 52 tests. README outline and git diff --check pass.
Archify delivery passes 9/9 with zero errors/warnings; spec SHA256
06671546e6ac1d04d5ea1efe2a87eeb95c96331212f602485a338c787cba7357;
HTML 1e8ae6e4f06dba54e7c14a0235548962c4e5f235dfbdde452fd694185726bbf3.
Visual inspection remains skipped after prior browser access denial. Full Python
regression, coverage and mutation sweep are not refreshed. Evidence is Darwin arm64.

## 2026-10-09 observation and builtin-reducer Source contracts

Baseline 7f40ded. Export 168 captures twelve MaskedObservation and eight
ObservationReduction constructors plus both real builtin reducers over 69 inputs.
The constructor corpus pins stable reduced_by deduplication, MIME-like values,
ref/digest pairing without premature pattern validation, ordered/repeated metadata
pairs, warning bounds and successful projections/exact error messages.

Reducer captures preserve full result content/warnings and both descriptors.
Cases cover ANSI CSI and OSC removal (including greedy ST consumption), incomplete
escapes, duplicate/CR-only/blank/space runs, singular/plural markers, diagnostics,
Python Unicode decimal and word boundaries, Unicode-14/15 differences, extension
and drive paths and the four extra re.IGNORECASE ASCII-range letters. The actual
Source regex preserves faile/failed but can fold fail; fixtures retain that fact.
No validation or algorithm is substituted. Inputs follow declared field types;
arbitrary Python metadata objects and annotation-violating values are not claimed.

Validation: Source tools/efficiency suite passes 52 tests, exporter --check confirms
168 files and all 19 scan guards pass. README outline and git diff --check pass.
Native typed observations/reducers follow. E2 mode/admission/inflation/storage/fault
handling and E3-E6 remain pending; runtime optimization is not activated by this
contract capture. Full Python regression, coverage and mutation sweep not refreshed.

## 2026-10-09 typed masked-artifact recovery tool

Baseline 30274e7. efficiency.ReadMaskedPage accepts a scoped reference and named
RecoveryPage with unbounded integer character offsets/limits. It retrieves caller-
masked strict UTF-8 from RawStore, renders Source continuation/header text and
binary-searches the complete page against the 50,000-byte limit. Empty/end pages,
Unicode, huge offsets and clamped limits preserve Source output.

protocol adds a closed ReadTokenArtifactInput, exact schema and typed decoder,
canonical sorted replay, detached integer snapshots and masked recording copies.
Absent pagination defaults and explicit JSON null remain distinct; null/negative
pagination returns Source validation text before store availability checks.
Malformed noninteger JSON is refused at the typed decoding boundary; broader
annotation-violating Source input/fault profiles remain open.

RuntimeConfig.TokenArtifactTools explicitly installs the read-only, parallel-safe
observation.recover tool through the normal gate; TokenArtifactStore remains
caller-owned, with nil returning the Source unavailable notice. Each handler pins
session, owner and canonical workspace and refuses foreign authority/workspace.
InstallTokenArtifactTool extends immutable catalogues and preserves an existing
same-name custom definition and catalogue identity. Explicit ToolSelection continues
to narrow runtime admission. No manager/default/environment activation or implicit
store allocation/close is added; E2-E6 and default clone/lifecycle binding remain open.
The recovery marker will follow typed observation outcomes in E2.

Twenty actual Source handler recipes compare complete UTF-8 output digest, byte
count and header against actual Go tool turns, with exact schemas/traits. Native
checks pin scoped handler authority, cancellation, workspace refusal, custom tool
preservation, closed input/null replay and detached large integer fields. Guard
refusal proves zero store-clock reads. An initial test assumption that textual
refusal sets the provider error flag was corrected to the existing Source-compatible
gate behavior; runtime refusal semantics were unchanged.

Validation: full go test ./... -count=1 -timeout=180s, full go test -race ./...
-count=1 -timeout=180s and go vet ./... pass after correction. Focused recovery/
store race passes. Source exporter --check confirms 167 files, all 19 scan guards
pass and Source token-tools/efficiency tests pass 52 tests. README outline and
 git diff --check pass. Archify delivery passes 9/9 with zero errors/warnings;
spec SHA256 82c866cc9fa8bb9e89607350ec0132f0e764a50693d5c7b9d4a9c2b1860833c5,
HTML 86457c3bfa53ac71fb07fa7af4283f4c036af43e4b8575daea840937684e4162.
Visual inspection remains skipped after prior browser access denial. Full Python
regression, coverage and mutation sweep are not refreshed. Evidence is Darwin arm64.

## 2026-10-09 bounded artifact-recovery Source contracts

Baseline 5f6035b. Export 167 executes the real installed read_token_artifact handler
in twenty store/pagination scenarios. Captures pin the exact schema and read-only,
parallel-safe observation.recover traits, complete rendered-output SHA256/byte count
and header, including UTF-8 byte caps, Python character offsets, continuation hints,
unbounded offsets/limits, explicit null, invalid bounds, no store, foreign refs,
closed stores and expiry. Temporary real stores retain scoped ref authority; no
handler or pagination implementation is substituted. Installation also proves the
registry object and an existing custom same-name tool are retained unchanged.

Validation: Source tools/efficiency tests pass 52 tests; exporter --check confirms
167 files and all 19 scan guards pass. README outline and git diff --check pass.
This commit captures Source only. Native recovery and tool/runtime binding follow;
full Python regression, coverage and mutation sweep are not refreshed.

## 2026-10-09 native scoped masked RAM store

Baseline ad703dc. Package efficiency adds RawStoreConfig/ArtifactClock/RawStore,
ArtifactPointerConfig/ArtifactPointer and named ArtifactFault/ArtifactError.
NewRawStore validates and detaches unbounded limits, resolves workspace provenance
and validates the legacy relative directory without using it for storage. The
production entropy source is crypto/rand with 32 bytes per ref; default clock uses
monotonic elapsed time. The explicit store owns masked UTF-8 strings in RAM only.

PutMasked asserts caller redaction, rejects oversize before closed checks, sweeps
expiry before total capacity, retries collisions eight times and publishes only
validated immutable pointer metadata. GetMasked validates ref before closed state,
checks scoped membership before reading clock, removes expired rows and verifies
size/SHA256 before returning text. Discard does not read the clock. Sweep reads the
clock before checking closed state; Clear and Close remain idempotent after close.
errors.Is preserves Source expired-as-not-found and storage-fault parent categories.

All twelve Source constructor profiles and fifteen operation recipes compare
pointer fields, outcomes, exact error kind/message and closed state. Extra native
checks cover 128 concurrent crypto refs/read/discard, detached limits/pointer
metadata, metadata symlink with no writes, strict UTF-8 refusal, expiry overflow,
missing workspace, nil clock and zero-pointer marshaling. Typed callback errors
are the supported clock-fault boundary; arbitrary Python callback/duck-typed inputs,
nonstandard workspace expansion and malformed Unicode exception text remain open.

E1 store library is delivered; store tools and runtime binding remain pending.
Observation/request/response stages, component lifecycle and E6 runtime composition
remain open. Efficiency mode/style activation is still unsupported. No dependency
or any/interface{}/RawMessage domain structure is added. RawMessage appears only
at the fixture JSON decoding boundary in tests.

Validation: go test ./... -count=1 -timeout=180s, go test -race ./... -count=1
-timeout=180s, go vet ./... and focused efficiency race pass. Source exporter
--check confirms 166 files, all 19 scan guards pass, and Source token-efficiency
suite passes 48 tests. README outline and git diff --check pass. Archify delivery
passes 9/9 with zero errors/warnings; spec SHA256
9d4cc4b92269f9e43cdd6a5ecbcbcd75788eade2138bdf268e46abf9488357a1;
HTML b3313668a4e2f1af9f92370986c46f954fc9dc3fcdad6b024d68c67fa162aeca.
Visual inspection remains skipped after prior browser access denial. Full Python
regression, coverage and mutation sweep are not refreshed. Evidence is Darwin arm64.

## 2026-10-09 masked raw-store Source contracts

Baseline 7484fd5. Export 166 captures 12 constructor profiles and 15 real
MaskedRawArtifactStore operation sequences, controlling only the trusted clock
and token entropy boundary. Covers scoped refs, UTF-8 byte sizes, capacities,
eager expiry, expiry removal, backward/failing/nonfinite clocks, collisions,
private record corruption and close/clear/discard ordering. Each fixture retains
actual pointer fields or Source exception class/message and closed state.
All stores use isolated temporary workspaces; captures assert no files are written.

Validation: Source token-efficiency suite passes 48 tests; exporter --check and
all 19 scan guards pass. git diff --check passes. This is Source evidence only;
native storage and tools/runtime activation remain pending. Full Python regression,
coverage and mutation sweep are not refreshed.

## 2026-10-09 native immutable token-efficiency contracts

Baseline 67d83e4. Package efficiency adds named Stage/Mode/Status/Lossiness/CostTier,
DescriptorConfig/Descriptor and ReceiptConfig/Receipt/ReceiptProjection. Constructors
follow the captured Source validation order and messages; Descriptor retains first-seen
capability deduplication, MIME-like matching and declared recovery/cost metadata.
Receipt validates safe codes, SHA256 digests, raw-ref pairing and nonnegative metrics;
its projection carries no observation/request content and computes changed from hashes.

Integer limits and metrics use math/big.Int rather than truncating Source integers to
host widths. Construction and each public snapshot/projection copy integer pointers,
optional text pointers and slices. Private object state cannot be changed through a
caller-owned config or returned projection; zero descriptors/receipts refuse marshaling.
DefaultDescriptor supplies the Source constructor defaults. Warning length uses the
existing Python surrogatepass character counter before machine-code validation.

All 27 Source descriptor and 41 receipt recipes match successful normalized fields
and JSON projections or exact validation messages, including 101-digit descriptor
limits and a 1001-digit raw-byte counter. Native tests also mutate every returned
pointer/slice category, refuse NaN/Inf timing and zero objects, and pin the 256/257
lone-surrogate warning boundary. Native typed construction excludes Python annotation-
violating objects; exception class names, broader Unicode/numeric wire profiles and
arbitrary duck-typed SPI values are not claimed matched by this foundation.

This is E0 library delivery, not an activated optimizer. No registry, raw artifact
store, reducer, request/response policy, component lifecycle or runtime consumer is
installed yet. Existing unsupported efficiency mode/style checks remain unchanged.
E1-E6 in the explicit migration breakdown and the full port stay open. No dependency,
any/interface{}/RawMessage or generic domain map is introduced.

Validation: full go test ./... -count=1 -timeout=180s, full go test -race ./...
-count=1 -timeout=180s and go vet ./... pass. Focused efficiency race passes.
Source exporter --check confirms 165 files, all 19 scan guards pass and the Source
48-test token-efficiency suite passes in this iteration. README outline and
 git diff --check pass. Archify delivery passes 9/9, zero errors/warnings;
spec SHA256 34f1bbc8b6d7a3ec671ee76593dfe5a0ee18b2664cebd4dcfccb5fba52212b87,
HTML ba83403b60d959507daf429004bc896b3482e1fe9d365c1517a1ac74d9a1ff38.
Visual inspection remains skipped after prior browser access denial. Evidence is
Darwin arm64; full Python regression, coverage and mutation sweep are not refreshed.

## 2026-10-09 token-efficiency descriptor/receipt Source contracts

Baseline 2f742eb. Export 165 captures 27 real ComponentDescriptor constructors and
41 real OptimizationReceipt constructors/as_dict projections. Inputs follow the
Source-declared field types; sequence recipes are converted to tuple warnings at
the Source boundary. Captures retain full normalized output or exact error class/
message without substituting validation or implementing a reducer.

Cases cover closed stages/modes/status/lossiness, identifier/version/reason bounds,
MIME-like values (including the Source-admitted slash-only value), stable capability
deduplication, costs, recoverability declaration, positive descriptor limits, warning
count/code/length boundaries, nonnegative metric validation, digest/ref pairing and
changed detection. Descriptor limits include 101-digit integers; a receipt raw-byte
metric contains 1001 digits, pinning unbounded counts before native type design.
Native field types will prevent arbitrary ill-typed Python objects from entering
the domain; this corpus does not claim dynamic duck-typed SPI input conformance.

Validation: 68 isolated constructor captures, Source exporter --check confirms 165
files, all 19 scan guards pass, and python/tests/test_token_efficiency.py passes
48 tests. README outline and git diff --check pass. This commit adds the E0-E6
migration breakdown and Source evidence only; no Go implementation or runtime
activation is included. Full Python regression, coverage and mutation sweep remain
unrefreshed. Native immutable contracts follow; E1-E6 and the full port stay open.

## 2026-10-09 native Guardian Launcher activation and HTTP effects

Baseline cfb5304. The standalone Launcher now forwards Settings.GuardianEnabled to
ManagerServices.GuardianEnabled, accepts MINILOOP_GUARDIAN independently of the
still-unsupported comprehensive feature bundle, and reports guardian_enabled in
side-effect-free inspection. Unset stays false. Runtime owned-action binding,
readonly construction and the normal broker semantics remain the preceding slice.
The separate fake-benchmark composition also forwards this shared setting rather
than silently accepting an inactive flag after the global unsupported entry is removed.
A real fake-factory manager turn with controlled model responses proves that its
Guardian denies the action, preserves the canary and avoids a human pending question.

Eleven actual Go Launcher/provider-HTTP/session-message/approval runs match the real
Source HTTP captures: parent/review tool catalogues, prompts and tool-result text,
final response, human parking and canary deletion/preservation. Cases cover default
human routing, automatic allow/deny, defer, hostile readonly reviewers, immutable/
readonly pre-review refusal and provider errors both with/without a verdict word.
Native tests use a loopback HTTP model adapter rather than replacing the Guardian,
gate or broker; file effects occur in isolated temporary workspaces. Source responder
RuntimeError and native upstream HTTP 400 are distinct provider faults; the captured
comparison proves the resulting review/approval/effect paths, not exception-text or
provider-status equivalence. Caller-owned model/test services make no remote calls.

MINILOOP_GUARDIAN=yes inspection proves activation without allocating a workspace;
actual go run ./cmd/miniloop --dump-config confirms the same CLI behavior. Guardian
cannot bypass MINILOOP_FEATURES refusal. No dependency or ambiguous domain field
added. Shared concurrency pools retain existing nested admission; broader optional
harness, nested-pool profiles and full G6 are not claimed closed. SQLite, diagnostics
backend choice, token-efficiency policies/bundle selection and G0-G7 remain open.

Validation: full go test ./... -count=1 -timeout=180s passes on rerun. The first
full normal run observed an AST timeout fixture's version probe timing out before
its operation (expected an applied probe); no AST code was changed. A later complete
run passed after the earlier concurrent jobs ended. Full go test -race ./... -count=1
-timeout=180s passes; final focused benchmark/launcher Guardian race passes after
fake-composition forwarding and its new owned-turn test. Final go vet ./... passes.
Source exporter --check confirms 164 files and all 19 scan guards pass for the new
Source contract helper. README outline and git diff --check pass. Archify delivery
passes 9/9, zero errors/warnings; spec SHA256
04a5a3bd6dfaa0b8ee3c0e64d80d101796c2e511d8d01c3e907cc5d7700e072e,
HTML 46a72bbb63c603956eba30405a347a3db83a9686786367f3663d541bdfb5a96c.
Visual inspection remains skipped after earlier browser access denial. Evidence is
Darwin arm64; full Python regression, coverage and mutation sweep are not refreshed.

## 2026-10-09 Guardian HTTP Source contracts

Baseline 770e997. Export 164 adds eleven real FastAPI lifespan/session/message/
approval recipes using the actual Source manager and Guardian. Only the model
responder is controlled; the normal permission stack, reviewer, HTTP resolution
and host shell execute normally in fresh temporary workspaces. A canary records the
actual approved shell effect, and hostile auditors must leave no planted file.

The recipes cover default-off human allow/deny, automatic allow/deny, defer to both
human answers, hostile review writes, readonly/immutable denials before review,
and ordinary provider faults with and without verdict words. Captures retain exact
parent/review catalogues, prompts and tool-result strings, human parking, final
response and canary existence. The ALLOW-word provider fault actually deletes the
canary, pinning the previously observed Source text-parsing behavior end to end.

Validation: eleven isolated HTTP captures; full Source exporter --check confirms
164 files; all 19 scanning guards pass; README outline and git diff --check pass.
No Python runtime or Go implementation included in this contract commit. Native
Launcher forwarding and HTTP comparison follow; full Go/Python, race, coverage and
mutation sweeps are not refreshed for this Source contract slice.

## 2026-10-09 native manager-owned per-action Guardian binding

Baseline 09f66e4. ManagerServices.GuardianEnabled now forwards an explicit boolean
to the runtime. RuntimeConfig.GuardianEnabled requires a bound ApprovalBroker.
Each owned tool dispatch captures a typed SubagentParent with its current run,
owner, execution root and catalogue; a cloned ApprovalSurface selects the readonly
Guardian for this action. A private gate/policy override passes that surface without
mutating a shared broker or policy. The review callback stores no parent Session
handle, never reacquires its mutex, and constructs a fresh auditor only when the
broker actually asks for review. Remembered human grants still answer first.
Direct public gate/broker calls retain their explicit embedding behavior; they do
not fabricate a current parent. Default-off managed turns keep the existing human
path. Children remain readonly peers with fresh state and no recursive auto-binding.

Seventeen Source recipes are exercised: fifteen managed owned-dispatch recipes
match decisions, human parking, normalized approval rows/events, prompts/catalogues,
inspection results and problem counts. The two missing-agent/session Source recipes
map to native refusal of unbound approval authority; this is an authority-boundary
comparison, not an emulation of a Python state dictionary. Native construction fault
messages retain Go error types rather than Python exception class text. Provider
errors already project to child error text in the native loop, so both ordinary
fault deferral and ALLOW-word error-text approval match actual Source outcomes.
Reviewer context.Canceled now propagates through broker and permission dispatch,
including cancellation returned while the outer context is still live. It creates
neither an auto decision nor a human pending row. Other reviewer faults still defer.

Additional complete concurrent managed turns share one provider and read distinct
owner workspaces, producing separate allow/deny receipts without deadlocking the
parent turn or leaking the review prompt into its transcript. Default immutable and
readonly denials reject before Guardian/provider execution. Repeated owned reviews
see changed file data; hostile auditors cannot write even under a widened catalogue.
No dependencies or ambiguous domain fields added. Launcher MINILOOP_GUARDIAN
forwarding is the next slice; configuration still refuses it with a truthful Launcher
selection reason. HTTP/launcher activation and broader optional harness profiles
are not claimed complete. G0-G7 and the full port remain open.

Validation: full go test ./... -count=1 -timeout=180s, full go test -race ./...
-count=1 -timeout=180s, and go vet ./... pass for the implementation. Final focused
Guardian/Approval race also passes after adding the denial checks. Source exporter
--check confirms 163 files. README outline and git diff --check pass. Archify delivery
passes 9/9 with zero errors/warnings; spec SHA256
802912598def5f4d97ca9f1efa518165d7a6f94065f071e41fbef0e81ae214ff,
HTML 0792ad08834eca8e77c48aca2d6c77ced3f6d59b870045ba58df274860a9889c.
Visual inspection remains skipped after earlier browser access denial. Native runtime
evidence is Darwin arm64. Full Python regression, scans/mutation sweep and coverage
are not refreshed; no Python runtime/scanner/target changed in this slice.

## 2026-10-09 manager-selected Guardian Source contracts

Baseline d564fef. Export 163 captures seventeen actual SessionManager-selected
Guardian recipes through the real ApprovalBroker and readonly auditor loop.
No Guardian or broker substitute is installed: only the model responder and a
NullStateStore-derived approval receipt sink are controlled. Each case records
reviewer activation, model prompts/catalogues, human parking, normalized durable
rows/events, decisions, problem reports and absence of pending questions afterward.

Coverage includes default-off human allow/deny, automatic allow/deny, defer to either
human decision, provider error, constructor error, provider cancellation, missing
agent/session refusal, hostile writes with both ordinary and widened catalogues,
masked preview, fresh reviews reading changed workspace data, and human remembered
grants taking precedence before another model review. Parent history remains private
and unchanged; review writes leave no file in either catalogue profile.

The provider-error observation changes the next implementation check: Source's
Agent loop returns ordinary provider faults as error text. Guardian parses that text
like any other answer, so RuntimeError("ALLOW provider fault") actually auto-allows.
An ordinary fault with no verdict word defers without a broker problem. In contrast,
a role-policy constructor ValueError reaches broker containment, adds a problem and
parks a human; CancelledError propagates without approval or human parking. These
are captured current behavior, not a claim that all faults abstain. Native error
projection and automatic selection must be reconciled against these distinct paths;
Manager/Launcher activation remains unimplemented and MINILOOP_GUARDIAN unsupported.

Validation: isolated seventeen-case capture and full Source exporter --check
(163 files), all 19 scan guards, and focused Python Guardian/wiring/readonly-role
suite pass (14 passed, 1 operator-gated real-provider test skipped). README outline
and git diff --check pass. No Python runtime or Go implementation changes; full Go,
full Python, race, coverage and mutation sweep are not refreshed for this Source
contract slice. G0-G7 and the full port remain open.

## 2026-10-09 native readonly Guardian library and explicit broker reviewer

Baseline afa4f2d. GuardianRequest/GuardianDecision/Guardian and AgentGuardian now
provide a named Go review boundary. Session.Guardian snapshots a typed parent
environment outside its held turn; each review constructs a fresh readonly auditor
with the explore catalogue, isolated messages/counters, parent hooks/services/pools,
peer-agent provenance, source system prompt and auditor lineage. It adds no task
wrapper or delegation-depth refusal to the Source readonly-role path. Normal task
delegation and Guardian reuse the extracted fresh-child constructor and parent
snapshot builder, retaining existing task depth checks and selected-handler rebinding.

Verdicts use Source's first Unicode word-boundary ALLOW/DENY occurrence and first
trimmed line capped at 200 Python characters. Other replies abstain. GuardianReviewer
implements the existing ApprovalReviewer seam: mask typed input before ASCII JSON,
cap preview at 2000 characters, drop the reason and forward only the decision.
Explicit ApprovalBrokerConfig.Reviewer selection is tested for actual allow/deny
settlement without pending human requests. No opaque payloads or dependencies added.

Eighteen real Source reviews match verdict/reason, prompt/system/cache fields,
catalogues/results and unchanged parent history. Both default and widened catalogue
hostile writes leave the workspace untouched. Four Source bridge recipes match
masked/escaped/bounded previews and defer. Native tests also check zero/nil parent
refusal, caller cancellation before provider, inherited stop hooks, auditor label/
depth/lineage and absence of inherited human authority.

Manager/Launcher automatic selection is not yet installed; MINILOOP_GUARDIAN still
fails activation with the truthful automatic-binding reason. Pinned library reviewers
must be freshly captured when the execution workspace changes. Session.Guardian must
not be called from an already locked parent tool turn; the manager-bound next slice
must construct from the existing owned turn, compare HTTP approval outcomes and
contain faults/defer/cancellation. Optional-harness state profiles and full G6 remain
open; this slice proves the captured default-core and widened-catalogue recipes.

Validation: full go test ./... -count=1 -timeout=180s and go vet ./... pass;
full go test -race ./... -count=1 -timeout=180s passes after the final lineage
change. Focused Guardian/subagent race also passes.
Source exports 162 and all 19 scan guards pass. Existing Source Guardian/wiring/
readonly-role suite: 14 passed, 1 operator-gated real-provider test skipped.
README outline and git diff --check pass. Archify delivery passes 9/9, zero
errors/warnings after isolating the new review arrow in a separate corridor.
Specification SHA256 1a96135acd0eff68bb1d5e8669a394fc52db9a4a6eea9800a14081075db76f84;
artifact 12563377d1c500c28d8dafdef691342dd934fc8e1457e57a9d5783d3a26c6cf0.
Visual inspection remains skipped after prior browser access denial. Native runtime
evidence is Darwin arm64.
Full Python regression, coverage and mutation sweep are not refreshed. Previous Go
coverage remains the earlier 90.03% snapshot. SQLite, diagnostics parser choice,
token-efficiency/default bundle selection and G0-G7 remain open.


## 2026-10-09 Guardian Source review and broker-adapter contracts

Baseline 917574a. Export 162 captures eighteen actual AgentGuardian reviews through
the real readonly_role_agent constructor and fake model responder, plus four actual
broker_reviewer adapters. Captures compare exact review prompt, cached system text,
model-visible tool catalogues/results, first Unicode-word-boundary ALLOW/DENY match,
first-line 200-character reason, deferred outcomes and unchanged parent history.
Hostile write attempts are blocked both with the default explore catalogue and an
operator-widened catalogue, establishing readonly as the second independent boundary.
Bridge cases mask registered secrets before ASCII JSON preview and cap at 2000
characters, including escaped credentials and a cut through escaped emoji data.

Source capture changes no Python runtime behavior. Validation: eighteen direct
reviews and four bridge captures; exporter --check confirms 162 current files and
all 19 scan guards pass. Existing Guardian/wiring/readonly-role tests pass 14 tests;
one real-provider role-isolation test is skipped by MINILOOP_REAL_PROVIDER_TESTS.
README outline and git diff --check pass. Native Guardian construction/explicit
broker selection follows; Manager/Launcher activation remains a separate slice.


## 2026-10-09 native AST text and final tool-result encoding

Baseline bc51cc7. jsonvalue.LegacyString now renders compact ensure_ascii=False
Python strings through the existing surrogatepass character representation. AST
Result.ToJSON uses that string profile; lone surrogate fields/keys/warnings/hints
remain observable and no longer fail before the tool returns. Result.MarshalJSON,
MarshalUTF8 and MarshalLegacyUTF8 retain their existing strict byte profiles.
Nonfinite values are preserved inside the returned result text, as in Source.

Tool-result block JSON encoding now checks the named wireResultText variant for
valid UTF-8 before encoding. It returns ErrResultUTF8 rather than replacing lone
surrogates with U+FFFD. Normal wire field order, omission and cache-control behavior
are unchanged. Actual direct and streaming provider paths refuse before transport;
Go retains its typed FailureProtocol category, while Source SDK raises UnicodeEncodeError.
This does not claim exact exception text or all transcript/SSE/recording Unicode
profiles. In-memory fake-model tool results remain observable as Source text.

Eight Source public-operation/Tool.run/SDK recipes replay exact surrogatepass text
and ASCII data projections through native CLI calls. Tests check original cwd,
0400 copied sources and cleanup. Actual readonly runtime AST model execution retains
all eight strings; direct provider tests preserve admitted text on the actual request
wire and reject five malformed-Unicode results without HTTP calls. Streaming refusal
shares that tested block serializer. No host interfaces enter domain/service fields.

Validation: full go test ./... -count=1 -timeout=180s, full go test -race ./...
-count=1 -timeout=180s and go vet ./... pass. Additional runtime-model and exact
provider-wire assertions pass focused race; agent vet also passes after new tests.
The first exporter check flagged an existing child-background timing discrepancy;
its isolated capture matches and a repeated full --check confirms all 161 exports.
All 19 scan guards pass. README outline and git diff --check pass. Archify
delivery passes 9/9, zero errors/warnings. Specification SHA256
25c8fc54ebd8ab31d8374d69c9022a3b8fb09113b8de6bb334865d506a53ceaa; artifact
33a3a7611d028e1eb21ac8198de791da35cc037d284d4ce1c4d15b878a73bd40. Visual
inspection remains skipped after prior browser denial. Native runtime evidence
is Darwin arm64. Full Python regression, coverage and mutation
sweep are not refreshed. The last Go coverage remains the earlier 90.03% snapshot.
CPython recursion/stack limits, broader Unicode lifecycle profiles, native SQLite,
diagnostic parser selection and G0-G7 remain open.


## 2026-10-09 AST text and provider encoding Source contracts

Baseline 92b6536. Export 161 adds eight actual Source public AST operations and
installed Tool.run observations. CLI JSON carries escaped lone high/low surrogates
in fields, keys, warnings and error hints; paired emoji, Unicode/control characters
and nonfinite numbers establish the admitted profile. Captures retain exact ASCII
result JSON and the result string as surrogatepass hex, plus original cwd/argv and
post-tool snapshot cleanup. Source to_json succeeds as a Python string in all eight
cases. Strict UTF-8 encoding rejects five lone-surrogate cases.

Each result is submitted through the actual AsyncAnthropic SDK and an in-memory
HTTP transport. Lone surrogates raise UnicodeEncodeError before transport (zero
requests); ordinary Unicode/nonfinite result text sends once. No network credentials
or live model calls are involved. The first full export check flagged the existing
unrelated child-background snapshot; an isolated child-background capture matches,
and the repeated full --check passes all 161 exports. All 19 scan guards, README
outline and git diff --check pass.
Source runtime behavior is unchanged. Go text and byte-boundary alignment follows.


## 2026-10-08 native AST decimal conversion faults

Baseline 60a1ce3. PayloadIntegerError now preserves the Source ValueError text for
oversized decimal JSON integers, including the unsigned digit count and Unicode
scanner offsets. It unwraps the existing typed jsonvalue.ErrInteger cause. AST
metric totals remain exact big.Int values; converting warning totals above 4300
digits fails with the distinct CPython output-conversion message. Error-envelope
precedence and ignored negative/float metrics are retained. Shared JSON decoding
and other callers' profiles are unchanged. Public operations convert these faults
to the Source no-probe error result after private snapshot cleanup.

Twelve export-160 cases compare exact invocation error class/text or ASCII result,
and execute real native public file_outline children with original cwd, immutable
0400 copies and post-fault snapshot removal. Tests retain the admitted maximum,
signed/nested parsing, Unicode prefixes, syntax/error-envelope precedence, both
metric-warning families and large float tokens. Integer handling is kept in its
own file; no host-interface domain or service fields were introduced.

Validation: full Go tests and vet pass; twelve focused conversions pass, including
focused race/vet after the source-only move into integer.go. Full
go test -race ./... -count=1 -timeout=180s passes. Exporter --check confirms
160 files; all 19 scan guards pass.
README outline and git diff --check pass. Runtime topology, ownership, authority,
defaults and public tool declarations are unchanged, so the architecture specification
and generated HTML remain at the previous validated delivery. Python full regression,
coverage and mutation sweep are not refreshed. The previous Go coverage is 90.03%
and predates this slice. Exact CPython recursion/stack limits and lone-surrogate
text projection remain open alongside SQLite, diagnostic parser selection and G0-G7.


## 2026-10-08 AST decimal conversion Source contracts

Baseline 60dbc6e. Export 160 captures twelve real AST CLI-output recipes at the
4300-digit CPython conversion boundary. Each runs both the private invocation and
public file_outline path, recording exact ASCII result JSON or exception class/text,
actual child argv/cwd and public snapshot cleanup. Cases cover signed/nested integer
parsing, Unicode offsets, syntax/fault precedence, maximum admitted metrics,
parse-error and truncation sums, error-envelope precedence, ignored negatives and
large floating-point tokens. Private invocation ValueError becomes a no-probe public
error result after the public OSError/ValueError catch and snapshot cleanup.

This commit establishes Source observations; native integer-fault alignment follows.
Validation: all twelve direct Source cases captured; exporter --check confirms
160 current files and all 19 scan guards pass. README outline and git diff --check
pass. Python runtime modules and defaults are unchanged.
Full Python regression/coverage and mutation sweep are not refreshed.


## 2026-10-08 AST Launcher environment selection

Baseline f921d3a. The Launcher now forwards the five existing Source AST settings
into a detached astcontext.Config and ManagerServices.ASTConfig. Selection defaults
off. Activation requires an operator-owned absolute executable path and SHA-256
pin; timeout/output defaults are the Source settings' 10 seconds / 1,000,000 bytes.
MINILOOP_AST_OUTLINE_ENABLED is no longer an unsupported setting. No extra model
inputs, environment passthrough or alternate CLI activation bypass is introduced.
Inspection reports ast_tools as configured selection without probing the binary;
missing executables are reported at actual invocation, as in Source.

Fourteen actual HTTP/model traces match export 159: catalogues and exact result
text, final text, child argv/cwd and cleanup. Tests use a copied native executable
with no injected production environment; the race helper exits through the native
syscall to avoid the race runtime's artificial one-second exit delay. Readonly
sessions exercise all four operations and executable/path/output/timeout refusals.
CLI dump-config also reports activation without starting runtime or probing.
The existing gate and current session binding remain the execution boundary.

Validation: full go test ./... -count=1 -timeout=180s with cross-package coverage,
go vet ./..., fourteen focused HTTP replays and inspection/config tests pass.
Deduplicated Go statement coverage: 90.03% (24,094 / 26,762), agent 89.57%,
astcontext 89.01%, httpapi 89.51%, protocol 91.68%, launcher 80.79%, config 86.60%.
Coverage is executed statements, not migration completion. Source exports 159 and
all 19 scan guards pass. Full go test -race ./... -count=1 -timeout=180s
passes. README outline and git diff --check pass. Archify delivery passes 9/9
with zero errors/warnings; specification SHA256
84f0fbec73caa3b6b78e245d1bac8ee71fde36c4689f0a15895b8f24572cbb9f, artifact
a1bd1cf38c3432f76f4d87eb64ff6378daf73baec171a82dbb73cec1cdc67c71.
Full Python regression/coverage and mutation sweep are not refreshed. Native
runtime evidence is Darwin arm64; visual review remains skipped after the earlier
browser access denial. Extreme malformed AST profiles, native SQLite, diagnostic
parser selection and G0-G7 remain open.


## 2026-10-08 AST HTTP Source contracts

Baseline 8ecc4c2. Export 159 adds fourteen actual Source HTTP-to-model AST traces:
default-off catalogues, all four semantic operations, path escape, wrong digest,
missing executable, incompatible version, malformed/non-object output, nonzero exit,
output cap and command timeout. The controlled model and feature-disabled manager
retain the real Source AST handler, probe, snapshot and subprocess implementation.
Captured catalogues, result text, child argv/cwd and snapshot cleanup are normalized
only for temporary paths. Timeout uses a one-second budget and a three-second child
sleep so version startup does not decide the expected phase.

This commit records the Source contract; native Launcher replay is the next slice.
No runtime topology/default change. Validation: direct capture of all fourteen rows;
exporter --check confirms 159 current files; all 19 scan guards pass.
README outline and git diff --check pass.
Full Python regression, coverage and mutation sweep are not refreshed by this capture.


## 2026-10-08 explicit runtime AST model tools

Baseline aa18ee6. RuntimeConfig.ASTConfig and ManagerServices.ASTConfig now explicitly install the
four Source-compatible read/parallel AST tools through the existing ToolGate.
Nil keeps them absent; invalid zero config fails construction before runtime setup.
The Manager detaches the operator config and forwards it through its shared session
construction path. The handler captures the current identity/workspace binding
under lock, then executes immutable request state without holding the mutation lock.
Foreign session/owner/workspace authority is refused. Selected task-child definitions
rebind to fresh child handlers while sharing only the stateless configured adapter;
existing read-capability policy selects all four tools for Explore.
Real native CLI tests exercise a read-only four-tool model batch, eight version/command
executions, source freeze modes, values-only path rewriting and post-tool cleanup.
Guard denials start no child, workspace transitions rebind the root, manager config
reassignment cannot affect sessions and an actual task-child performs its own probe
and operation. Installed schemas/traits/capabilities match Source export 158.
Launcher settings/environment/CLI selection and HTTP end-to-end Source traces remain
next; default tool installation is unchanged. Final AST malformed/extreme output
profiles, SQLite, diagnostic parser selection and G0-G7 remain open.

Validation: installed Source traits/capabilities match; four-tool readonly model
batch performs eight real probes/operations and compares rewritten path/content,
original cwd, private mode and cleanup. Manager config detachment/current workspace
rebinding, foreign authority refusal, guard-before-process denial, zero config
refusal and actual task-child applied-result delivery pass under focused AST race.
Full go test ./... -count=1 -timeout=180s, go test -race ./... -count=1 -timeout=180s
and go vet ./... pass; a final focused race refresh includes the added child-result
assertion. Source exports 158 and scan guards 19 passed this iteration. README outline
and git diff --check pass. Archify delivery passes 9/9 with zero errors/warnings;
specification SHA256 f7ef731506812e8419ad6d4ad8ea012a94d7ea293c0082a6b28bc384ebd51fdb,
artifact 12e865b6e8d7f813bb56bcd16cc402791f13ae991379694c192544add4563797.
Native evidence is Darwin arm64. Visual inspection remains skipped after prior browser
access denial. Coverage, full Python regression and full mutation sweep were not
refreshed; G0-G7 remain open.

## 2026-10-08 AST tool declarations and closed input variants

Baseline 2169cd4. Source export 158 captures the four installed tools'
schemas, risk/read-only/parallel claims and capability sets without replacing handlers.
Native protocol now declares four AST ToolInput variants with named density,
view and reference-kind enums, detached required arrays and optional presence
pointers. Canonical sorted UTF-8 identity and recording-only string masking preserve
supplied fields without inserting execution defaults. The model schema projection
matches all four actually installed Source declarations, including default values,
maxLength, array limits, numeric bounds, descriptions and additionalProperties=false.
InputSchema adds named MaxLength/Minimum/Maximum/Default fields with detached clones
and typed validation; defaults use existing immutable closed JSON variants.
The Go boundary refuses non-string arrays, unknown fields, null fields and invalid
enums. These are explicit typed admission refusals, not a claim of parity for every
malformed dynamic Python handler input. Scalar/count semantic errors still belong
to the existing adapter. Runtime registration is the next delivery slice.

Validation: all four schemas equal actual Source declarations; optional supplied-field
identity, array/pointer detachment, recording-only masking and typed shape refusals
pass. Focused protocol race also validates named schema-bound refusals. Full go test
./... -count=1 -timeout=180s, go test -race ./... -count=1 -timeout=180s and go vet
./... pass for the working implementation. Export --check confirms all 158 Source
contracts current; verify_scans confirms all 19 guards anchored. README outline and
git diff --check pass. Archify delivery passes 9/9 with zero errors/warnings;
specification SHA256 fa0425e2efabab9d62a50e757413267c96ce9228f63c2ae5a14d4e50f9358fa6,
artifact 5f11a98def2e1ffa31c1a796abbc7bfc284f3e3c88e58cc8ffe90ffe059f351a.
Visual inspection remains skipped after prior browser access denial. Coverage, full
Python regression and mutation sweep were not refreshed; G0-G7 stay open.

## 2026-10-08 native AST semantic operations

Baseline f77182e. Native astcontext Adapter now implements RepoMap, FileOutline, ShowSymbol and
SymbolReferences using closed density/view/reference-kind options and typed request
records. Every operation canonicalizes/validates paths, builds the existing bounded
no-follow snapshot, probes the operator executable in the original workspace, then
executes direct CLI argv and classifies output before snapshot cleanup. Leading-dash
patterns use attached --expression values; show preserves signature flags and bounded
glob early returns (32 visible matches/16384 UTF-8 bytes), without executing a probe.
All 26 public Source contracts compare exact ordered result JSON, probe/command argv,
original cwd and removal of private snapshots through actual native CLI children.
The child independently verifies external 0400 source copies; a mutation of the live
workspace during child execution still reads original bytes. Workspace executables
are refused before side effects and caller cancellation remains a Go error.
Four model-tool registrations and runtime selection remain next. Final extreme
numeric/recursion wording and lone-surrogate projection profiles remain open.

Validation: all 26 actual public Source recipes replay against real native children;
workspace mutation during child execution, pre-probe workspace executable refusal,
caller cancellation and both visible ambiguity limits pass. The focused AST race
suite passes after adding the final ambiguity-bound cases. Full go test ./...
-count=1 -timeout=180s, go test -race ./... -count=1 -timeout=180s and go vet ./...
pass for the implementation. Source exports 157, AST tests 41 and scan guards 19
passed in the preceding capture step. README outline and git diff --check pass.
Archify delivery passes 9/9 with zero errors/warnings; specification SHA256
1fdf69fe8d70e8483c5e5c1c0a7f11ac8892c5b6ce4f466449817f5f31f15399,
artifact 1ac09390f2ca545e3fc6c018b2f823280d78948df983835b6dcea3c7e881e0d3.
Native evidence is Darwin arm64. Visual inspection remains skipped after prior
browser access denial; full Python regression, mutation sweep and coverage were
not refreshed. Native SQLite, diagnostic parser selection and G0-G7 stay open.

## 2026-10-08 AST public-operation Source contracts

Baseline d051052. Export 157 records 26 actual public Source operations against
real isolated CLI children and real bounded snapshots. Exact JSON, probe/command
argv order, original workspace cwd and post-return snapshot removal are captured.
Cases include four density profiles, all six view/import pairs, leading-dash
symbols/grep patterns, signature-only show, all reference kinds and selected caps,
glob early no-match/ambiguity, regular-file refusal and option/input errors.
No Source operation, probe, subprocess, snapshot or classifier method is replaced.
Native semantic operation replay is the next delivery slice; this capture does
not activate tools or change runtime defaults. G0-G7 remain open.
Validation: all 157 exported Source contracts current; 41 Source AST tests pass;
all 19 scanning guards anchored. README outline and git diff --check pass.
Source runtime/defaults are unchanged. Full Python regression, full mutation
sweep and coverage were not refreshed for this capture commit.

## 2026-10-08 native AST results and captured payload classification

Baseline 8dc34a8. Native astcontext now has closed Operation and immutable Result/Payload records.
The private captured-output classifier preserves envelope numeric equality, values-only
path rewriting, error-before-import handling, iterable warnings/errors, arbitrary integer
metrics and partial/ambiguity/no-match precedence. All 65 actual Source observations
replay: 61 compare exact ASCII/UTF-8 JSON and four preserve ordinary TypeError text.
Detached records/warnings and strict standard-JSON nonfinite refusal are tested.
Actual semantic subprocess execution, four model tools and runtime selection remain
pending. Extreme CPython recursion/digit-fault wording and lone-surrogate string
projection still need final profile work; current ToJSON enforces a UTF-8 boundary.

Validation: focused 65 Source replays, detached publication records and probe/process
short-circuits pass; focused AST race also covers native integer/depth fault refusal.
Full go test ./... -count=1 -timeout=180s, go test -race ./... -count=1 -timeout=180s,
and go vet ./... pass. README outline and git diff --check pass.
Archify delivery passes 9/9, zero errors/warnings. Specification SHA256
135accd1c962af6bba8e08fed0e6b75dc3d15cc9475dcd4b200bd4efdafe9bbf;
artifact 1b5ff7be079bd59d79519e23bd10e330f2425edbf1fa3f4be221acf6a5126361.
Visual inspection skipped after prior browser access denial. Coverage, full Python
regression and full mutation sweep were not refreshed. G0-G7 remain open.

## 2026-10-08 shared CPython JSON syntax diagnostics

Baseline 2279dc1. The HTTP syntax-only scanner now lives in internal/jsonsyntax
with closed Code/Failure/Limits records, named character-offset accessors and a
shared number-prefix helper. HTTP retains its existing private failure projection,
985-container/4300-digit policy and closed value lowering; only duplicated lexical
ownership moved. Integer/depth policy remains caller-selected, not model-controlled.
The shared scanner is a prerequisite for Source-compatible AST invalid-JSON messages.
Eight malformed outputs from 65 actual Source CLI invocations compare exact
messages and Python character positions. No AST payload interpreter, public
semantic operation, tool/default activation or dependency is introduced.

Validation: go test ./... -count=1 -timeout=180s (with coverprofile),
go test -race ./... -count=1 -timeout=180s, go vet ./..., focused HTTP
JSON/syntax/depth/number/surrogate tests, and verify_scans.py (19 guards) pass.
Cross-package go test -coverpkg=./... -coverprofile also passes; deduplicated
statement coverage is 90.00% (23,448 / 26,054), AST 87.35%, HTTP 89.35%,
and shared jsonsyntax 96.33%. This measures executed Go statements, not feature
parity or migration completion. Python full-suite coverage and the full mutation
sweep were not refreshed; G0-G7 remain open.
Architecture delivery passes 9/9 with zero errors/warnings; specification SHA256
85c2ad49545df767726089b8672d942cfd2dbdd4e4adb7f0cc071bce28af62ed,
artifact ebe79e4bcb7ab5507d28b5040b205b57de9ebe9a5729c6550351d1859cb6ea48.
Visual inspection was skipped because prior browser access was denied.
README outline and git diff --check pass.


## 2026-10-08 AST payload invocation Source contracts

Baseline 632f208. Export 156 executes 65 actual Source _invoke calls against a
real isolated CLI child outside the workspace. Each recipe records actual version
probe/operation argv, Source result JSON in ASCII and UTF-8 projections, stdout
byte accounting and four ordinary TypeError escapes. No Source classifier,
JSON decoder, process runner, rewriting or projection method is replaced.

The corpus pins envelope validation (including Python true/1.0 schema equality),
nonzero stderr precedence, malformed JSON diagnostics, non-object output omission,
error-member presence/notes/hint conversion, imports removal ordering, values-only
snapshot rewriting, warning iteration, recursive arbitrary-precision metric sums,
container counts without recursive double-counting, nonfinite observations and
partial-before-ambiguity/no-match precedence for all four commands. Malformed null
collections remain ordinary exceptions where Source iteration raises, rather than
being synthesized as clean/empty results. Non-string iterable behavior is observed.

These are capture contracts, not a Go semantic implementation or tool activation.
Closed native results/payload trees, actual semantic subprocess invocation and
four model tools/runtime wiring remain required. SQLite, diagnostics parser choice
and G7 remain open; the full migration goal stays active.
Validation: 156 Source contract exports current; all 41 AST-context tests pass;
all 19 scanning guards anchored. README outline and git diff --check pass. Source
runtime/defaults did not change; Go gates, full Python regression, full mutation
sweep and coverage were not refreshed for this capture commit.


## 2026-10-08 native AST canonical and bounded glob selection

Baseline af83178. Private helpers now validate named-string arrays with Source
64-item/2048-character limits, return detached slices and canonicalize operator
workspaces. Exact selections implement non-strict realpath with symlink-before-
dot-dot ordering and workspace containment. Missing/non-directory suffixes and
ordinary metadata/readlink errors retain Source resolution behavior; snapshot
access later reopens the canonical selection with NOFOLLOW. Home expansion uses
operator HOME or standard-library account lookup. No dependency was added.

Adapter.safeGlobPaths accepts only final-basename wildcards, rejects recursive/
intermediate patterns and opens every static directory component relative to a
held no-follow root. It counts every enumerated name against 10,000, then applies
Source hidden-name, fnmatchcase and regular-file filtering. The shared fnmatch
rune matcher preserves malformed brackets, descending classes and backslashes.
Matches are sorted; Source deadline checks remain in entry iteration, including
empty-directory success, while caller cancellation propagates.

All 80 Source recipes are exercised: 76 compare native results/error text; four
non-string JSON inputs are refused at the native typed boundary. Raw JSON exists
only in fixture decoding, not service/domain state. A real post-glob symlink swap
is refused by the real snapshot constructor before its callback. Selection is
not a frozen read or new model authority. These helpers are private; typed CLI
payloads, four semantic tools/runtime activation, descending ignore-range wording,
native SQLite, diagnostics parser selection and G7 remain open.

Validation passes: focused AST selection race tests; full go test ./... -count=1
-timeout=180s; go test -race ./... -count=1 -timeout=180s; go vet ./...;
Linux amd64/arm64 go test -c ./astcontext; README outline; git diff --check.
Source preparation af83178 verified 155 exports, 41 AST-context tests, 19 scanning
guards and 77 invariant-posture modules; no Source code changed afterward.
Archify deliver passes 9/9 showcase checks, zero errors/warnings. Visual inspection
remains skipped after prior access denial. Full Python regression, full mutation
sweep and coverage were not refreshed. Linux evidence is compile-only.
Specification SHA256: a02474b68ee0a2c7ac66be876f101164652c73bf72a4e0c5ccfc44fd81186936;
HTML SHA256: bf1fbb8b3ac567f40e0b7c3af394d2992d48f995c7ff1edae2a3ef7f04aa1a34.


## 2026-10-08 AST path-selection Source contracts

Baseline 7802901. Export 155 captures 63 actual Source exact/glob selections,
five workspace resolutions and twelve string-array admissions. The helper builds
real files, directories, local/outside/broken/looping links and a POSIX FIFO, with
operator HOME isolated to a prepared workspace directory. Results include missing
suffixes, file-as-parent paths, symlink-before-dot-dot resolution, literal spaces,
Unicode character lengths, hidden-name rules, Python fnmatch bracket/backslash
semantics and malformed/descending character classes. Errors retain Source text.

Only capacity/deadline configuration changes in three recipes; actual glob
traversal and matching are not replaced. A real directory exceeding a tiny entry
limit refuses even when nothing matches; a real 1ns deadline fails on entries,
while an empty directory returns no matches because Source checks deadline during
iteration. Ordinary paths follow canonical internal links; basename-glob static
parents reject all symlinks. Public model handlers choose these branches; neither
this fixture nor private selectors install semantic tools or execute a CLI.

Native selection is the next implementation slice. Complete migration, AST payloads/
runtime wiring, SQLite, diagnostics parser selection and G7 remain open.
Validation: 155 Source exports current; all 41 AST-context tests pass; all 19
scanning guards anchored; all 77 package modules state invariant posture.
README outline and git diff --check pass. No runtime/module/default change was made;
Go gates, full Python regression, mutation sweep and coverage were not refreshed.


## 2026-10-08 complete private AST source snapshot builder

Baseline 626c759. Adapter.withSnapshot now combines the anchored filesystem layer
with the complete Source snapshot algorithm. Named snapshotLimits/Budget/Control/
Frame/Rewrite records keep request state local and typed. Candidate deduplication
retains return order; shallower targets copy first. Single-component traversal and
no-follow metadata/open checks remain anchored. Source handles close before the
callback; the private trusted temporary directory is removed on return/error/panic.
No Go dependency or public model tool was added.

All eight Source capacities, 56 suffixes, two basenames and 35 default-ignore rules
are pinned. Regular copies check initial and actual aggregate bytes, count controls
as files and finish with mode 0400. Every enumerated source name counts before
filtering. Recursive VCS metadata/symlinks/special files are skipped; explicit
files bypass suffix and ignore filtering. The nearest non-symlink git marker sets
the project frame; directory snapshots get a synthetic marker and cached raw root
controls. Local controls feed reverse three-state frames with compiled-pattern
match counts, raw-line quotas and Source UTF-8-ignore/splitlines decoding. Shape
validation occurs before control quotas; regex compilation occurs afterward.
Native errno causes are retained while projecting Source descriptor-child text.

All 36 actual Python filesystem recipes replay the native algorithm and compare
selected path order, rewrite roots, manifests, byte hashes, 0400 modes, refusal
messages/errno causes and successful cleanup. Native callback faults/cancellation
also prove temporary parent cleanup; caller deadline/cancellation remain distinct
from internal copy timeout. Earlier native handle tests prove parent-replacement
anchoring and metadata-to-open symlink refusal. Unsupported metadata backends now
refuse at root open even for an empty tree, rather than being swallowed as absent
ignore controls. Darwin arm64 is runtime-tested; Linux amd64/arm64 is compile-only.

This completes the private snapshot kernel, not the entire AST feature or migration.
Public canonical selectors/bounded globs, CLI JSON payloads and four model tools/
runtime wiring remain required. Descending-range compiler wording, native SQLite,
diagnostics parser selection and G7 remain open. Copying does not freeze concurrent
regular-file writers or forcibly interrupt blocking filesystem syscalls.

Validation passed: go test ./... -count=1 -timeout=180s;
go test -race ./... -count=1 -timeout=180s; go vet ./...;
go test -race ./astcontext -run 'Snapshot|Ignore' -count=1 -timeout=60s;
Linux amd64/arm64 go test -c ./astcontext; 154 Source contract exports current;
verify_scans.py reports all 19 scanning guards anchored. verify_guards.py -k ast
also caught its five substring-selected legacy mutations; these names are not AST
snapshot mutations and do not establish a Source AST mutation sweep. README outline
and git diff --check pass. Archify deliver passes 9/9 showcase checks with zero
errors/warnings. Visual inspection remains skipped after prior access denial.
Full Python regression, full Source mutation sweep and coverage were not refreshed.
Specification SHA256: 8e0fb3c4da3994400e951add30754b6a06361e03d2fb8add23d8b5c1d10750e3;
HTML SHA256: 7e6ceffe3a52ce526ad831ed4b4b3dae84798cc6027ce8dde31ab05820180e60.


## 2026-10-08 anchored AST snapshot filesystem primitives

Baseline 9716ae5. Private snapshotDirectory owns a directory handle and performs
single-component no-follow openat/fstatat operations. Metadata classifies regular,
directory, symlink and special entries without opening filtered source files.
Regular opens recheck type after open and use NONBLOCK to reject FIFO reads;
descriptors use CLOEXEC. Enumeration reopens dot with an independent offset,
checks cancellation per batch/name and bounds every entry before filtering.
Parent traversal closes intermediate handles. No dependency or public tool added.

Native tests on Darwin arm64 prove anchored metadata/read/enumeration after rename
and replacement by an external symlink, rejection after a metadata-to-open symlink
swap, FIFO/special-file refusal, repeated scans, entry limits, cancellation,
component admission, descriptor inheritance flags and independent lifetimes.
Darwin fstatat64/openat constants come from pinned Apple XNU
f6217f891ac0bb64f3d375211650a4c1ff8ca1ea; Linux uses Go syscall constants.
Darwin amd64 and Linux amd64/arm64 are compile-only evidence. Other metadata
backends fail closed rather than fall back to pathname reads.

This is a prerequisite, not the complete snapshot port. Full bounded copy, ignore
frame/counter integration, temporary lifetime cleanup, all 36 Source snapshot
replays, semantic payloads and public/runtime AST wiring remain required. Native
SQLite, diagnostics parser selection and G7 remain open; full migration stays active.

Validation passed: go test -race ./astcontext -run Snapshot -count=1 -timeout=30s;
go test ./... -count=1 -timeout=180s; go test -race ./... -count=1 -timeout=180s;
go vet ./...; go test -c ./astcontext for Darwin amd64 and Linux amd64/arm64;
README outline and git diff --check. Archify deliver passes 9/9 showcase checks
with zero errors/warnings. Source files/export inventory did not change. Python
regression, Source mutation sweep and coverage are not refreshed. Visual inspection
remains skipped after prior access denial. Specification SHA256:
91b1b45babfef1564771e75d455a815b33d35040f4d84d66ab7e2746068af951;
HTML SHA256: 373146485120cb79e87ea42921206fa2244ca915c155a09ba5dc6ebdd14b8b77.


## 2026-10-08 native AST ignore policy prerequisite

Baseline 8686f88. The internal ignoreSpec now compiles explicit ignore/keep/unmatched
variants with named pattern records and selected indices. Go standard regular
expressions preserve Source root/basename/globstar rules, directory/file precedence,
negation and Python final-newline decisions. Rune sets/ranges avoid interpreting
Source POSIX-looking classes as Go POSIX syntax. Source 4096-character and two-
variable-star-group admission remains enforced before compilation. Empty raw lines
are removed; comments/null patterns retain compiled indices and count for later
match-operation budgets. No dependency was added.

All 35 successfully compiled Source recipes compare pattern counts and every
include/index decision. Six Source compiler/guard refusals remain failures; five
retain exact Source messages. Descending character ranges use native compiler text,
so Python regex-error diagnostic compatibility remains open before full public AST
parity. UTF-8 errors=ignore and Python splitlines control decoding replay Source
snapshot inputs; the eventual copier must preserve the original control bytes.

This is a private prerequisite of the full native snapshotter, not a path authority
or narrowed replacement for it. Anchored no-follow descriptor traversal/copy,
supported-name/default-ignore policy, nested frame/counter integration, shared
snapshot cancellation/deadlines and private lifetime cleanup remain required.
No semantic AST tool, process invocation or feature activation was added here.
Diagnostics parser choice, native SQLite, semantic payloads/runtime wiring and G7
remain open; the complete Python-to-Go objective stays active.

Validation: focused ignore race tests, full go test ./... -count=1 -timeout=180s,
go test -race ./... -count=1 -timeout=180s and go vet ./... pass. Source preparation
in 8686f88 passed 154 exports, 41 AST-context tests, 19 scanning guards and package
invariants; no Source file changed afterward. README outline and git diff --check
pass. Canonical diagram/specification/generated HTML match; Archify passes 9/9
showcase checks with zero errors/warnings. Visual inspection remains skipped after
prior access denial. Full Python regression, Source mutation sweep and coverage
were not refreshed. Specification SHA256:
3d45fe104b89ccc746012e15ce4a71d9062fe59dacf88b7c13c34b4653f9a95d;
HTML SHA256: dcf716a598c6c7b5a59bcc4c939ad10d82634bae20f2602c73dd86b909f3da02.

## 2026-10-08 AST ignore matcher Source contracts

Baseline aff92ec. Source export 154 adds 41 actual ignore-guard/GitIgnoreSpec
recipes against installed pathspec 1.1.1. Include/keep/unmatched decisions, selected
pattern indices and compiled pattern counts are observed independently of Go.
Root anchors, segment/globstar rules, file/directory precedence, negation, blank/
comment filtering, escapes, Unicode runes, bracket classes, invalid ranges, guards,
normalization and Python final-newline matching are captured. The POSIX-looking
nested class emits Python's existing FutureWarning; its current behavior is recorded.

These records support the native snapshotter's prerequisite ignore matcher; they
prove no no-follow snapshot copy or public AST-tool installation. Descriptor/copy
and nested frame/budget/lifetime integration, semantic payloads, tool/runtime wiring,
diagnostics parser choice, native SQLite and G7 remain open. The full port stays active.

Validation: all 154 Source exports pass --check, 41 Source AST-context tests
pass, all 19 scanning guards remain anchored and verify_invariants.py passes.
README outline and git diff --check pass. Source runtime, defaults and topology
are unchanged; full Python regression, mutation sweeps and coverage were not
refreshed. Native matching is delivered in the next independent commit; no
native snapshot claim is made by this Source-contract preparation.

## 2026-10-08 AST source snapshot filesystem contracts

Baseline 6158e2c. Source export 153 records 36 actual private anchored snapshot
recipes without substituting its traversal, descriptor opens, reads, ignore matcher
or copy effects. Manifests retain relative paths, file bytes/hashes, read-only modes,
synthetic Git markers, returned candidate order and private path rewrites. Successful
context exit is observed to remove the temporary snapshot; a source-file mutation
inside the context leaves the frozen bytes unchanged.

The corpus pins all 56 supported source suffixes and both exact basenames, uppercase
suffixes, hidden names, unsupported explicit-file bypass, default exclusions, root/
nested ignore frames, negated vendor paths, nearest Git-marker selection and explicit
nested overrides. Recursive symlinks/FIFOs and symlink ignore controls are skipped;
raw explicit symlink/FIFO targets, symlink parents, absent/empty/outside candidates
retain actual refusal classes/text. These private snapshot candidates are distinct
from public tool paths, which first pass safe-path resolution.

The actual default 64 MiB stat gate is exercised using a sparse oversized source.
Smaller, explicitly recorded capacity overrides exercise file/control-file, entry,
depth, ignore-byte, ignore-pattern and match-operation counters without changing
matching or traversal. Their native replay must use these same declared limits;
these cases do not prove default-capacity throughput or race behavior. Default cap
values are pinned separately. Oversized ignored files bypass the byte gate; invalid
UTF-8 is dropped for ignore matching while raw control bytes are copied unchanged,
and Unicode splitlines behavior is captured. Actual ignore shape/length guards are
also captured.

Next implement the native snapshotter with anchored no-follow opens, complete
supported-name/default-ignore behavior, bounded nested frames and private cleanup.
Public model tool paths must be resolved before forming trusted snapshot targets.
This Source preparation changes no runtime/default/topology and proves no native
snapshot implementation. Structured AST payloads/tool/runtime wiring, diagnostics
parser choice, native SQLite and G7 remain open; the complete port goal is active.

Validation: all 153 Source exports pass --check; 41 Python AST-context tests,
all 19 scanning guards and verify_invariants.py pass. Full Go go test ./...
-count=1 -timeout=180s passes in this batch. README outline and git diff --check
pass. No runtime module, default, scanner target or architecture topology changed;
full Python regression, mutation sweeps, race, vet and coverage were not rerun.
No native snapshot replay or race proof is claimed. Interactive-map regeneration
was not required for this contract-only change.

## 2026-10-08 foreground shell startup registration ordering

Baseline d3157cf. A full race run failed the existing real-child spawn/stop stress
test's two-second deadline. Code inspection found a Start/registration window:
a child can emit effects before its parent publishes its PID, allowing Interrupt
to observe an empty tracker. The foreground executor now holds the same tracker
lock through Start and PID insertion; startup failures unlock before projection.
Callbacks, output drains and waits remain outside the lock. Background ownership
and effect permission policy are unchanged. This is a separate small runtime fix.

The existing TestStopWhileShellSpawnsChild runs both interruption and context
cancellation over twenty starts per case. Five repeated race runs pass after the
fix (200 real starts/stops). Final combined-worktree go test ./... -count=1
-timeout=180s, go test -race ./... -count=1 -timeout=180s and go vet ./... pass;
all 152 Source exports were checked in this batch. No Source runtime module or
mutation anchor changed; full Python regression, Source mutation sweeps and
coverage were not refreshed. README outline and git diff --check pass. Architecture
source/generated map match; Archify passes 9/9 showcase checks with zero errors/
warnings. Visual inspection remains skipped after prior access denial.
Specification SHA256: 38e875be238f24e99e2fbec83aa556c30b437c223dba7184d4f09e5962f601fe;
HTML SHA256: e0ca6fcd24a5d050bbf26ffa89d198fed03eb90fa54eb350dbec7764a58708c3.

AST source snapshots/structured payloads/tool wiring, diagnostics parser choice,
native SQLite and the complete G7 migration audit remain required. The full
Python-to-Go objective is still active.

## 2026-10-08 standalone AST executable probes and bounded processes

Baseline 0477c2b. NewAdapter validates Config and exposes Probe(ctx, workspace).
A probe resolves the configured executable, rejects canonical workspace-local
binaries, recomputes optional SHA-256 before each launch and runs direct argv.
LANG/LC_ALL/SYSTEMROOT plus explicit passthrough names form the child environment;
NO_COLOR=1 and a fixed trusted TMPDIR outside the supplied workspace override
ambient values. Empty workspace is an operator-only probe, not a model binding.
Digest checking precedes exec and does not atomically pin the file against replacement.

Independent stdout/stderr captures retain at most MaxOutputBytes raw bytes each;
overflow kills the launched group. Python maximal-subpart UTF-8 replacement preserves
CR/LF rather than applying file-reader universal newlines. Buffered 64 KiB read-or-
EOF ordering retains Source timeout-before-overflow for a partial over-limit writer
that keeps its pipe open; native regression and a real Source process audit cover this
case. Direct-child wait and both
pipe drains share one deadline. Darwin/Linux retain the launched group ID after child
exit to terminate pipe-holding descendants; fallback builds kill only the direct child.
Cleanup owns/closes readers and joins local child/drains; external cancellation returns
its context error. Internal timeout/overflow/start/read faults remain typed probe
statuses. The adapter keeps no persistent child or per-request mutable state.

All sixteen real Source probe recipes now replay native executable behavior, including
three pre-exec refusals and matched-digest execution followed by an ordinary exit fault.
Native helpers additionally verify exact per-stream caps, malformed UTF-8, environment
isolation, one-argv shell metacharacters, ordinary/closed-pipe hangs, an actually started
exited-child descendant, pre-start and active cancellation, zero configuration and
changed-file digest refusal on the same adapter. The initial test-only three-second
limit on copied helper executables timed out once during parallel full-suite execution;
normal probes now retain Source's default fifteen seconds, while dedicated short
wall-time tests keep their 0.3-second limit and descendant-start evidence.

The public method currently probes only --version. Structured AST payloads, safe source
snapshots/ignores/globs, four tool-input variants and manager/child/startup/HTTP wiring
remain required. No model AST tool, default activation or dependency was added. This
slice does not resolve diagnostics parser choice, native SQLite or the full G7 audit;
the complete port objective remains active.

Validation: focused native astcontext race tests and all 152 Source contract
exports pass. The final batch passes full go test ./... -count=1 -timeout=180s,
go test -race ./... -count=1 -timeout=180s and go vet ./... with the adjacent
foreground-shell registration fix recorded separately. The real Source partial-
block process audit confirms actual execution, timeout status and 1,000 retained
bytes; the native regression matches that ordering. README outline and git diff
--check pass. Canonical map/source/generated HTML are synchronized; Archify passes
9/9 showcase checks with zero errors/warnings. Visual inspection remains skipped
after prior access denial. Full Python regression, package invariants, Source mutation
sweeps and coverage were not refreshed; no Source runtime module changed.
Specification SHA256: cad0e208dc39f47ec85ba8fffaed029bcd2e3c9cb950778bf977b68f1ce8166d;
HTML SHA256: e2aa65224c80d4c7c412ed8bd1db736201c7b804bf779b1c044b6dcbc6533bee.

## 2026-10-08 typed AST configuration and inert version probes

Baseline 1b820e2. The standalone astcontext package now implements Parameters,
private validated Config, seven closed Status variants, immutable Probe and typed
ProbeSnapshot/VersionOutput. DefaultParameters matches the Source adapter defaults;
NewConfig validates bounds, environment names, digest normalization, schema and
version-range ordering. Snapshots detach mutable names/digest storage. Version
components use existing Python Unicode 14 decimal blocks and arbitrary-precision
comparison; suffix range behavior is retained without machine integer overflow.

ParseVersion matches Source banner search, homepage normalization, supported range
and nonzero-exit text. It interprets captured output without launching or trusting an
executable. Seventeen typed configuration recipes and all twelve actual version-
output recipes compare Source results. The numeric environment recipe is separately
refused by concrete JSON admission. All seven valid Source probe/status projections
match; unknown/zero status, zero Config, invalid duration and mutable snapshot cases
are exercised. Native timeouts must additionally fit a finite positive Go duration;
Source dynamic NaN/infinity or sub-nanosecond values are not admitted as equivalent.

The four executable-admission probes still require the future native process adapter.
General AST payload/result projection, bounded argv execution/streams/deadline,
executable checks, anchored snapshots/ignores/globs and tool/runtime/startup wiring
remain required. This library installs no tool, dependency or default, and does not
resolve the diagnostics parser question or native SQLite approval. The full port
objective remains active; no AST execution/G7 completion is claimed.

Validation: focused astcontext tests, full go test ./... -count=1 -timeout=180s,
go test -race ./... -count=1 -timeout=180s and go vet ./... pass. All 152 Source
contract exports pass --check. README outline and git diff --check pass. Canonical
architecture and generated HTML are synchronized; Archify passes 9/9 showcase
checks with zero errors/warnings. Visual inspection remains skipped after prior
access denial. Full Python regression, invariants, source mutation sweep and coverage
were not refreshed for this Go-only library slice. Specification SHA256:
a3112c0e64f73a6f94c1ec55dfd7b5d77fa8067062ac2a6c62b724becc4bd8ed;
HTML SHA256: 26b7a9a238648d29b0b57cec40dfca09c3b17f0b00ad9c80baaa00aa68dacfa5.

## 2026-10-08 AST adapter Source admission and probe contracts

Baseline 940c023. Source export 152 starts the missing AST-context adapter port.
Eighteen actual AstContextConfig recipes capture defaults, invalid bounds, environment
name validation/deduplication, normalized executable digests, pinned schema and version
range admission. Sixteen actual AstOutlineAdapter.probe invocations use temporary
operator-owned executables and the unmodified Source subprocess runner. Recorded
version/homepage/exit behavior includes prereleases, multiline search and Unicode
decimal versions. Missing, workspace-local and digest-mismatched binaries do not
execute; matching digests permit execution without overriding a later process fault.
Eight actual probe/result projections retain statuses, optional fields and compact JSON.

Implementation order for the complete adapter: (1) closed configuration/status/probe
and validated inert result projections; (2) argv subprocess, environment allowlist,
digest recheck, independent byte drains, shared deadline and descendant cleanup;
(3) anchored no-follow snapshots, bounded ignores and safe glob selection;
(4) payload admission/path rewriting and four closed tool-input variants;
(5) managed/child/workspace binding, explicit startup selection and real HTTP replay.
No raw map/any domain design, relaxed path boundary or substitute parser is planned.
This Source preparation changes no runtime/default/architecture topology. It does
not prove native AST support, secure snapshot behavior or full migration completion.
Diagnostics parser choice and native SQLite approval remain independent open items.

Validation: 41 Python AST-context tests pass; all 19 source scanning guards
remain anchored; export_go_contracts.py --check confirms all 152 files. Full
Go go test ./... -count=1 -timeout=180s passes. README outline and git diff
--check pass. No Python runtime package modules, source scanner targets or Go
runtime code changed; full Python regression, invariant/mutation sweep, race,
vet and coverage were not rerun. Architecture topology is unchanged, so no
interactive-map regeneration or visual inspection was required for this slice.

## 2026-10-08 explicit diagnostics launcher forwarding

Baseline 1350998. launcher.Options.DiagnosticsProvider forwards an explicit
caller-owned provider into managed sessions. Nil selection leaves diagnostics absent;
InspectWithOptions reports diagnostics_tools without callbacks, provider disclosure
or workspace allocation. App shutdown drains sessions without closing the provider.
The unsupported MINILOOP_FEATURES bundle guard remains in force.

Six actual Source HTTP/model traces match ordered tool catalogues, exact paired
results, final output and bound workspace/target observations. Disabled and escaping
inputs invoke no provider; ordinary provider failure reaches the later model turn.
This slice uses an injected custom provider and does not select a Python compiler,
CLI/default parser, dependency or full-feature activation. Parser choice, native
SQLite and G7 remain open; the complete Python-to-Go goal is still active.

Validation: go test ./... -count=1 -timeout=180s, go test -race ./...
-count=1 -timeout=180s and go vet ./... pass. All 151 Source contract exports
pass --check. Focused launcher diagnostics tests, README outline and git diff
--check pass. Architecture source/generated HTML remain synchronized; Archify
passes 9/9 showcase checks with zero errors/warnings. Visual inspection remains
skipped after the prior access denial. Full Python regression, coverage, Python
package invariants and mutation sweeps were not rerun for this Go-only slice.
No Python runtime modules or scanner targets changed. Specification SHA256:
c1e656a5f7f10e8764cde321b971ec3bc795bad898a2296d163b6b049f680f14;
HTML SHA256: 2f72ec46df7150daa3b41ec7639f7fb180ee9efa4320f417ab11cae72e474243.

## 2026-10-08 diagnostics HTTP Source contracts

Baseline 5c301f4. Source snapshot 151 adds six actual TestClient HTTP/model turns
with an explicitly installed custom diagnostics provider. Disabled selection,
whole-tree, resolved nested target, explicit null, escaping path and provider
RuntimeError retain real registry/manager/gate/model behavior. Ordered model tool
catalogues, exact paired result text, provider root/target observations and final
output are recorded. Escaping input produces the Source refusal without invoking
the provider; ordinary provider error produces Error text and the later model turn.

The fixture does not invoke or substitute a Python syntax parser and does not
resolve the pending parser-choice question. Next forward explicit provider handles
through launcher.Options and verify the actual native HTTP composition against
these six traces. Inspection must report selection without invoking callbacks or
exposing provider data; ownership remains with the operator.

Validation: all 151 export files pass --check, all 19 scanning guards remain anchored,
eight Python diagnostics/query tests pass, full Go tests and go vet ./... pass.
The exporter also emitted one child-process watcher notification without fixture
byte drift. README outline and git diff --check pass. Runtime/dependencies/topology
are unchanged; full Python/race, invariants, mutation sweeps and coverage were not
rerun. Parser choice, native SQLite, full activation and G7 remain open.
The complete Python-to-Go objective remains active.

## 2026-10-08 native Python diagnostic file scanner

Baseline e04abd1. diagnostics.NewPythonSyntaxDiagnostics now implements native
file/control flow around an explicit PythonSyntaxParser. SyntaxResult is a closed
invalid/clean/problem projection with private fields and named constructors; zero
results are rejected and non-syntax parser errors propagate. No parser or external
compiler implementation/default is selected. The user preference question remains
pending; this scanner works with either eventual parser choice.

The native scanner retains Source selection: explicit true .py suffix targets,
hidden .py tree matches, matching directories, non-traversed directory symlinks,
component-tuple sorting before first-200 selection and stat-before-read byte checks.
Tree symlink files retain Source's follow-at-stat/read behavior. Its text reader
reuses shared Python UTF-8 maximal-subpart replacement/universal newlines. Filesystem
warnings retain errno/quoted-path text. Oversize warnings continue before the report
stop, so 53 warnings can be collected before a later ordinary file checks the stop;
outer rendering still caps at 50. Full enumeration and the stat gate are not whole-
tree work or atomic growth limits. Context checks cover traversal, read and parser
admission/completion; cancellation never returns a successful partial report.

All 34 valid file/tool recipes replay actual CPython input hashes and syntax/fault
observations through a test-only selected parser. Inputs, parser call order/count and
complete output/error match. Three dynamic keyword/list payloads remain separate
native strict-admission refusals, including Source's falsey empty list. Nil/unbound
parsers, invalid result variants, ordinary parser faults and cancellation are tested.
This establishes the native scanner, not a native Python grammar or actual compiler
adapter. The six earlier injected-provider recipes remain green.

Validation: focused diagnostics race tests and final full go test ./... -count=1
-timeout=180s, go test -race ./... -count=1 -timeout=180s and go vet ./... pass.
Source preparation in e04abd1 passed all 150 export files, all 19 anchored scanning
guards and eight Python diagnostics/query tests; no Source files changed afterward.
Full Python regression, package invariants, source mutation sweep and coverage were
not refreshed for this Go-only scanner slice. README outline and git diff --check
pass. Canonical map/boundary and interactive source are synchronized; Archify deliver
passes 9/9 showcase checks with zero errors/warnings. Visual inspection remains
skipped after prior access denial. Specification SHA256: 10eb7082c512abe93d08448552befa8bc5cc7663e338564d2c0715cba2a8dc14;
HTML SHA256: 8a498534da3a8053736add2888d5d4f3535d8bc96c12d667ab5a9be6ae7fd673.

Next implement the selected parser after the user's reply, then standalone startup
selection and complete native diagnostic runtime replay. Native SQLite approval,
wider filesystem/archival profiles, full feature activation and G7 remain open.
The complete Python-to-Go goal remains active.

## 2026-10-08 diagnostics parser-boundary Source observations

Baseline 82c9d05. Snapshot 150's diagnostics corpus now includes 37 real file/tool
recipes and six injected-provider recipes. A temporary ast.parse observer calls the
original CPython parser and records the normalized input SHA256, clean/syntax/fault
variant, line/message and non-syntax exception class. No parser or scan is replaced.
This supplies a replay boundary for native file/control-flow implementation while
the actual parser implementation remains separately provable.

New recipes capture pathlib component sorting (a/inner.py precedes a.py), .py as a
hidden tree match but not an explicit Python suffix, explicit dot-directory selection,
matching .py directories, quoted/Unicode filesystem warnings, UTF-8 maximal-subpart
replacement/universal newlines and oversized-warning continuation. The Source byte
limit is a stat-before-read gate. Its oversized-file branch continues before checking
the 50-report stop: 53 oversized warnings are collected and the later clean file is
still parsed, although tool rendering caps at 50. Whole-tree enumeration precedes
sorted 200-entry selection. Native work must retain these actual behaviors and not
claim stronger work/race bounds than the Source implements.

Validation: all 150 export files pass --check, all 19 scanning guards remain anchored,
8 Python diagnostics/query tests pass, full Go tests and go vet ./... pass, README
outline and git diff --check pass. Runtime code/dependencies/topology are unchanged;
full Python/race, invariants, mutation sweeps and coverage were not rerun.
Next implement the native scanner around a closed parser-result interface. The user
has been asked whether the language parser may invoke an installed Python compiler
or must be fully native; no compiler dependency/default is selected before a reply.
The full Python-to-Go objective remains active.

## 2026-10-08 native diagnostics provider and tool seam

Baseline 03d02d4 (Source snapshot 150). The native diagnostics package now has
named Diagnostic/Severity records, Source-default NewDiagnostic, immutable private
Request selections and the explicit context-aware Provider interface. Tool applies
workspace.Files resolution for explicit nonempty paths before callback admission;
refusals precede provider invocation. Reports preserve scope, order and vocabulary,
with Source's 50-report cap and caveat at exactly 50. Scope is read after Diagnose.
Context/backing errors remain failures, never a fabricated No diagnostics result.

A nonnil RuntimeConfig.DiagnosticsProvider or ManagerServices.DiagnosticsProvider
explicitly installs the typed diagnostics schema with Source read/parallel traits.
Nil configuration leaves it absent. Model input can choose only an optional string
path; root/provider/session selectors and malformed JSON types are rejected.
Omitted versus explicit null survives canonical JSON and masked recording, while
both execute tree selection. Constructors, accessors and masks detach path pointers.
The native strict decoder is distinct from Python's dynamic falsey-list acceptance.

Runtime captures the admitted identity and current session.files under its handler
lock, then releases that lock before provider IO. Parallel same-batch reads overlap.
Manager-created sessions bind their own roots. Explicitly selected task children
share the configured provider with a fresh child binding; the default capability
role catalogue remains unchanged. Worktree entry updates later diagnostics paths.
The caller owns provider lifetime, concurrency safety and returned snapshots. No
manager grants, durable state, new dependencies or default parser are introduced.

All six actual Source injected-provider recipes match output/fault and root/target
selection. Native tests also prove source schema/traits, optional/null/canonical/mask
identity, explicit symlink refusal, cancellation and transcript pairing, parallel
reads, manager roots, workspace rebind, foreign authority refusal and actual child
model invocation. Ordinary provider errors stay in the common tool-result pipeline;
a callback panic keeps the existing type-only parallel run fault and repaired tail.

Validation: focused diagnostics/protocol/agent race tests; full go test ./... -count=1
-timeout=180s, go test -race ./... -count=1 -timeout=180s and go vet ./... pass.
Source export --check passes all 150 files; Python diagnostics/query tests pass
8 tests (0.23s). The exporter logged two child-process watcher notifications while
still matching all fixture bytes. Python runtime/scanner modules are unchanged;
full Python regression, separate invariants/scanner/mutation sweeps and coverage
were not rerun for this Go-only runtime slice. README outline and git diff --check
pass. Canonical map and interactive spec are synchronized; Archify deliver passes
9/9 showcase checks with zero errors/warnings. Visual inspection remains skipped
after prior access denial. Specification SHA256: 8ae346d0920400ce7e83e3d9be9b629a6d5adac7966508e467fcce254637e8c2;
HTML SHA256: 0819b5790b24edac70a90c328deee38e609dadbcfabc392b8d9e99148ff0ac47.

Next implement the default Python syntax backend against the 29 actual AST/file
recipes, then standalone selection and activation. Injected provider replay does
not prove those parser/tree bounds or arbitrary provider tree confinement. Native
SQLite, wider archival profiles, full feature activation and G7 remain open.
The complete Python-to-Go objective stays active.

## 2026-10-08 diagnostics Source contracts

Reviewed baseline 79f9931. Source snapshot 150 adds python-diagnostics.json from
actual PythonSyntaxDiagnostics scans and installed diagnostics tool invocations,
not a replacement formatter. CPython 3.11.3 is recorded because ast.parse exception
classes and diagnostic messages are interpreter behavior. All three actual limits
(200 files, 1,000,000 bytes per file, 50 reports) and the schema/read/parallel traits
are captured. Twenty-nine file/tool recipes and six injected-provider recipes cover
sorted/hidden trees, exact and exceeded bounds, explicit targets, absent/null/empty
paths, UTF-8 replacement, NUL parse failure, filesystem warnings, unknown arguments,
provider failure, exact-cap rendering and refusal before provider invocation.

Source confines explicit safe_path selections, including outside symlink targets;
its root.rglob tree scan does not resolve each symlink file through safe_path and
therefore diagnoses outside file contents. Symlink directories are not traversed.
Both observed behaviors are retained as distinct fixtures; no blanket confinement
claim or silently corrected implementation is made. The file-count bound applies
to the sorted selected files, not the preceding whole-tree enumeration. The byte
warning is present only above the exact limit; exact 50 reports already print the
cap caveat. Missing non-.py targets return No diagnostics without stat. NUL source
raises ValueError in the pinned interpreter instead of becoming a SyntaxError row.

Go runtime/provider/default activation is unchanged. Next implement the named
Diagnostic/Provider and typed optional-path tool seam against these actual outputs,
then the default Python syntax backend with explicit parser fidelity evidence;
a regex or a Go-language parser cannot establish the Python syntax contract.
Native SQLite, diagnostics default backend, wider archival profiles, full activation
and G7 remain open. No dependency was added. The full port objective stays active.

Validation: all 150 Source exports pass --check; all 19 scanning guards remain
anchored; focused Python diagnostics/query tests pass (8 tests). Full Go tests
(go test ./... -count=1 -timeout=180s) and go vet ./... pass; README outline and
git diff --check pass. Python runtime/package modules and Go runtime code did not
change, so full Python/race, invariant/mutation sweeps and coverage were not rerun.
Canonical topology and interactive map are unchanged; this is contract preparation,
not diagnostics runtime activation.

## 2026-10-08 managed historical request reconstruction

Baseline 1e9240a (Source snapshot 149). ManagedSession.ReconstructRequest now
binds its own session ID to the enabled configured StateStore. A private concrete
reader projects typed stored events into the closed RequestEvent union and loads
messages from the model_start's exact integer TranscriptEpoch. Physical ordering
and ignored-event sequence stopping survive. Nil/disabled storage returns the
Source MissingStart result; no live-history fallback is used.

The caller owns authorization and backend lifetime. Reads release persistence
locks before IO and do not take turn admission, flush history, acquire leases,
repair crash tails or reinstall permission/capability observations. Backend errors
retain errors.Is identity through ErrRequestReconstruction; backend panics retain
only their type. Historical message values detach and retain stored masking and
caller omission. The managed typed-store profile supports concrete integer epochs;
the broader Source scalar/malformed archive profile remains the library contract.
No model tool or HTTP route is added by the operator method.

Native tests reconstruct both rounds of a real managed tool/model turn and compare
messages, cached system representation, tool schemas, model and max tokens against
the actual provider boundary. Requests remain identical after an idle rewrite
advances the transcript epoch. Reads also work during actual provider invocation;
masked projections, missing/disabled stores, event/message errors, panics and
cancellation, concurrent event capture and foreign-lease unrepaired crash tails are
verified. The test backend is injected and in memory, not a native SQLite database.

Validation: focused reconstruction race tests; full go test ./... -count=1
-timeout=180s, go test -race ./... -count=1 -timeout=180s and go vet ./... pass.
Source export --check passes 149 files; all 19 scan guards remain anchored. Full
Python regression passes 2155 tests, skips 28 and passes 24 subtests in 187.40s.
Warnings: three dependency deprecations and one subprocess finalizer reporting an
already closed event loop during test_owner_map_bound. Python runtime modules and
scanner targets were unchanged; separate invariants and mutation sweeps were not
rerun. README outline and git diff --check pass. Coverage was not rerun after this
adapter; the preceding 90.15% profile belongs to 1e9240a, not this changed tree.

README canonical map/boundary and interactive specification are synchronized.
Archify deliver passes 9/9 showcase checks with zero errors/warnings. Visual
inspection remains skipped after prior access denial. Specification SHA256:
af82235b359024451f0959cddd58de0c3c49004219c903f401a63d94670225ab;
HTML SHA256: 88c5562f9c000dee39baadc1a1d8661231e130ab692f77b32af77a3df271d812.
Native SQLite driver approval, remaining full-registry services, nonfinite/surrogate
archival profiles, full activation and G7 remain open. Next inspect the remaining
full-registry service contracts and port their runtime composition in small slices.
The complete Python-to-Go objective remains active.

## 2026-10-08 native historical request reconstruction

Baseline c2088cf (Source snapshot 149). sessionquery.Reconstruct exposes a closed
RequestEvent union, immutable integer RequestSequence, ReconstructionReader,
named RequestProjection/CapabilityProjection and Found/MissingStart/LegacyEpoch
result variants. Fields from historical records are immutable closed archive values,
not runtime permission or executable model-request types. Request() detaches message
containers and capability projections; standard JSON preserves strict serialization
failures. Reader ownership, authorization and close remain with the caller.

All 32 actual Source SQLite recipes match output or typed Source exception class,
and the original request from an actual managed Source run reconstructs unchanged
after a later epoch rewrite. First matching model_start, early legacy epoch before
count conversion, physical-order stopping, latest preceding fingerprint projections,
missing/null sequence distinctions and inert scalar output survive. Python structural
fingerprint equality compares exact numeric leaves across integer/float/bool and
ignores object insertion order. Count truth/int conversion retains Unicode decimal
text, truncation, large clipping and negative prefix slices. Count/sequence faults
are named ReconstructionFault variants separate from backing/context errors.

Native tests verify cancellation during event/message reads, backing errors, explicit
session selection, invalid/large sequence parsing and detached result containers.
This is archive-reader replay of Source SQLite evidence, not a managed native event/
transcript adapter or native SQLite durability. A reader must preserve physical order
and stored masking; the library neither repairs state nor issues grants. Managed
backing integration, nonfinite/surrogate/wider malformed profiles, native SQLite
driver approval, remaining full-registry services, full activation and G7 remain open.

Validation: focused sessionquery/jsonvalue race tests; full go test ./... -count=1
-timeout=180s, full go test -race ./... -count=1 -timeout=180s and go vet ./...
passed. Source export --check passes all 149 files; all 19 scanning guards remain
anchored. Python regression: 2155 passed, 28 skipped, 3 dependency warnings and
24 subtests in 86.48s. No Source runtime modules changed; separate invariant check,
Source mutation sweep and coverage were not rerun. Old coverage does not measure
this tree or migration completion. README outline and git diff --check pass.

Canonical README review baseline/state node, extension/hardening boundaries and
interactive specification are updated. Archify deliver passes 9/9 showcase checks
with zero errors/warnings. Visual inspection remains skipped after prior access
denial. Specification SHA256:
e59a5b0dc683cc57b8829e636afa0d2a1c5f5ef5b507482b03d5606c28338720;
HTML SHA256: 6ef0d98528cf0102fc06a0fece56b1ed86459a9ab83fd70bb0e8cfca896fa480.
Next bind reconstruction to configured native event/transcript records and verify
native generated request projections before moving to other full-registry services.
The complete Python-to-Go objective remains active.

## 2026-10-08 request-reconstruction Source contracts

Baseline 654b1a8. Snapshot 149 calls Source reconstruct_request against actual
SQLiteStateStore for 32 seeded recipes and a real managed-model request before/
after a live transcript rewrite. NullStateStore missing-start behavior is included.
Seeds retain ordered event inputs, epoch rows, complete output or Source exception
class. The runtime recipe removes only nondeterministic event IDs/timestamps,
retaining all reconstruction inputs and physical load order; its old request stays
identical after a later epoch appears.

Observations pin first matching model_start selection, early legacy/null epoch
return before count conversion, latest preceding matched catalogue/system/capability
projections, missing references matching null fields, physical-order early stop,
old/missing epoch reads and nullable/scalar archival output. Count conversion uses
Python truth/int rules: empty list/false/null become zero; true/float/Unicode decimal
text convert, large integers cap by slice length, negative counts slice from the
end, and invalid/nonempty containers report ValueError/TypeError. Absent sequence
and explicit null remain distinct; malformed predecessor sequence comparisons can
raise TypeError. Fingerprint equality is structural and treats integer/float values
as Python does. These behaviors constrain the closed native result/event design.

The 13 focused diagnostics/query/reconstruction tests and all 19 scanning guards
pass. No Source runtime modules or Go implementation changed. Native reconstruction
is still the next slice; no new live permission, storage durability or full migration
completion is claimed. Full feature activation, native SQLite driver approval,
remaining scalar/archival/transport profiles and full G7 remain open.

Export --check passes all 149 files, including a second independent execution of
the actual runtime recipe. Existing exporter model-deprecation warnings remain.
git diff --check passes. Full Go/Python regression, mutation sweep and coverage
were not rerun for this Source preparation slice.

## 2026-10-08 transcript-search launcher and actual runtime replay

Baseline 8c2cf7e (Source snapshot 148). launcher.Options.SessionQueryTools and
standalone --session-query-tools independently select the implemented read-only
search tool. Report.SessionQueryTools exposes that choice without startup effects.
The launcher remains process-local with a no-op transcript reader; selecting search
does not add a native state backend or widen MINILOOP_FEATURES support. CLI tests
prove dump purity and comprehensive-setting refusal before workspace creation.

Native replay compares both actual Source SQLite-backed manager/model recipes:
ordered catalogues, exact search text, final outputs and current epoch, including
the current captured tool_use query and superseded own history after a live rewrite.
Another session's history is excluded. Source Fake ToolUseBlock omits caller;
native fixture replies preserve omission rather than inserting null into searched
stored text. The idle live rewrite goes through normal persistence guards and event
capture, without changing stored counters or bypassing admission. Native tests use
an injected typed backend, not SQLite. Two actual HTTP compositions compare the
default-ten versus selected query catalogues, ordered results and final text through
a real native listener/provider adapter. Null storage gives no matches in both.

Validation: focused agent/launcher/CLI search race tests; full go test ./... -count=1
-timeout=180s, full go test -race ./... -count=1 -timeout=180s and go vet ./...
passed. Source export --check passes all 148 files; all 19 scanning guards remain
anchored. Python regression: 2155 passed, 28 skipped, 3 dependency warnings and
24 subtests in 92.64s. No Source runtime modules changed; separate invariant verifier,
Source mutation sweep and coverage were not rerun. Previous coverage is not a
current-tree or migration-completion measurement. README outline/diff checks pass.

README review baseline, canonical entry/state boundary and extension/hardening notes
are updated. Generated architecture passes 9/9 showcase checks with no errors or
warnings. Visual inspection remains skipped after prior access denial. Spec SHA256:
55096ea0cc9de403dd63dfb2f69e95548d10952018708277befbff9145eccb49;
HTML SHA256: d15835444681f1ff18f4873e172384a15bf18e7fa2b9edcadb427ccee4befef4.
Request reconstruction, arbitrary archival/scalar profiles, native SQLite driver
approval, other missing full-registry services, full activation and G7 remain open.
Next port the session_query reconstruction function and remaining full-registry
service boundaries. The complete Python-to-Go objective remains active.

## 2026-10-08 transcript-search runtime Source contracts

Baseline b9c4603. Snapshot 148 captures two actual FastAPI/manager/model compositions
(default ten tools versus explicitly enabled session_query over Null storage), plus
two actual SQLite-backed manager/model recipes with and without a live compaction
rewrite. Ordered request catalogues, final outputs, exact search results and current
epochs are retained. A durable search includes the currently captured tool_use query;
Source Fake ToolUseBlock omits caller, which must be preserved by native replay.
The rewrite recipe retains old own history while excluding another owner's rows.
HTTP Null storage reports no matches, not absent-manager state or durability.

Export --check passes all 148 files. No Source runtime modules/defaults changed.
Native replay and independent launcher/CLI selection are the next delivery slice;
request reconstruction, arbitrary archival profiles, SQLite driver approval and
full feature activation/G7 remain open.

## 2026-10-08 native session-bound transcript_search tool

Baseline 07f19da (Source snapshot 147). RuntimeConfig/ManagerServices.SessionQueryTools
explicitly install transcript_search with the captured Source schema, readonly/read
risk and parallel traits. TranscriptSearchInput is a concrete Query-only variant;
strict decoding rejects session/owner fields, and canonical hashing and recording
masking include the new variant without changing live arguments. Default catalogues
and capability role filtering remain unchanged.

Manager construction privately binds the configured TranscriptStore; the handler
uses trusted per-session identity. Nil manager storage projects a no-op reader and
returns Source's empty search. Bare runtimes and custom-selected in-process child
tools remain unbound and return the Source unavailable notice. Children do not
inherit a parent's transcript store. Existing guards/read permissions precede the
handler. The adapter converts typed stored Message content into closed archival
values in its encoded order; it does not widen the store to arbitrary Source rows.

Tests compare actual Source schema/traits and bare/null text, required Query and
scope refusals, canonical/masked input isolation, catalogue narrowing, guards before
reads, default role exclusion, selected child isolation and read-error propagation.
A restored-session model turn searches superseded own epochs and excludes another
owner's history through the injected test backend. History is seeded while the old
manager is stopped and then loaded through RestoreSessions; consistency guards are
not bypassed. This is native typed-backend evidence, not Source runtime end-to-end
serialization parity, SQLite durability or a new real restart benchmark. Request
reconstruction, independent launcher selection, arbitrary archival/scalar profiles,
native SQLite driver approval, full feature activation and full G7 remain open.

Validation: focused TranscriptSearch race tests; full go test ./... -count=1
-timeout=180s, full go test -race ./... -count=1 -timeout=180s and go vet ./...
passed. Source export --check passes all 147 files; all 19 scanning guards are
anchored. Python regression: 2155 passed, 28 skipped, 3 dependency warnings and
24 subtests in 94.35s. No Source runtime package module changed, so separate
invariant verification was not rerun. Source mutation sweep and coverage were not
refreshed; previous coverage does not describe this tree or migration completion.
README outline and git diff --check passed.

README baseline, canonical search/state path, extension/hardening notes and
interactive specification are updated together. Archify deliver passes 9/9 showcase
checks without errors/warnings. Visual inspection remains skipped after prior
access denial. Specification SHA256:
4a4d1fa7ef8e05f68b06193b5c6a4b6050e0fe441e85d43ebb9c705df050efb0;
HTML SHA256: 07aea339e6aad452a3fe08985427712734346b977228a0f0895e6bbb6b8e2e89.
Next capture actual Source manager/model search observations and add independent
launcher selection; request reconstruction remains another module function to port.
The complete Python-to-Go objective remains active.

## 2026-10-08 native historical transcript-search library

Baseline 2110eb2 (Source snapshot 147). The new sessionquery package provides named
SessionID/Epoch, Message, Match and Result structures, an explicit two-method Reader,
and Search/ToolSearch/Render APIs. Historical Role/Content use immutable closed
ArchiveValue variants and never become live protocol messages or authority. Reader
adapters must project missing roles as question-mark text and preserve stored order.

Eight real Source SQLite recipes and four absent/null-store surfaces match exact
search records and rendered output. The library retains direct empty-needle search
versus tool blank-query refusal, original query repr in no-match text, Python full
Unicode lowercasing/offsets, UTF-8 structured JSON spacing/order, gaps, match caps and
partial-scan coverage. The outer fixture's sorted inputs are not authoritative for
object order; messages_json retains actual SQLite-loaded row text. Native tests
check 20-epoch read bounds, explicit session selection, read failure and cancellation
before/during reads. Reads stop at 20 matches; per-epoch loading retains Source's
whole-epoch shape rather than claiming a byte-bounded historical scan.

This is a library slice. Runtime registration, managed owner/session admission,
request reconstruction, historical nonfinite/surrogate search parity, native SQLite,
full feature activation and full G7 remain open. Invalid/surrogate query/content text
is refused instead of silently rewritten. No dependencies or default behavior changed.

Validation: focused sessionquery race tests; full go test ./... -count=1
-timeout=180s, full go test -race ./... -count=1 -timeout=180s and go vet ./...
passed. All 147 Source exports are current, and all 19 scanning guards are anchored.
Python regression: 2155 passed, 28 skipped, 4 warnings and 24 subtests in 85.87s.
Warnings include three dependency deprecations and the previously observed asyncio
subprocess destructor Event loop is closed warning; its origin remains unidentified.
No Python runtime package module changed, so the separate invariant verifier was
not rerun. Source mutation guards and coverage were not refreshed in this library
slice; prior coverage is not a current-tree or migration-completion measurement.
README outline and git diff --check pass.

Canonical README, extension/hardening boundaries and interactive architecture are
updated. Archify deliver passes 9/9 showcase checks with no errors/warnings. Visual
inspection remains skipped after prior access denial. Specification SHA256:
e71c75133fa8a2ef26b12ec374282c1ac5a2eb5eab43bbd5c58e1496ff48b78b;
HTML SHA256: 9734cf992963a0c2f869aabb12000be4579e3dc3fdf53c571bc13b2ce7a57aed.
Next bind the search library to a typed runtime tool and managed session identity.
The complete Python-to-Go objective remains active.

## 2026-10-08 transcript-search Source contracts

Baseline 5af16e6. The comprehensive Source registry includes transcript_search,
which has no native Go tool yet. A separate Source exporter executes eight search
recipes against actual SQLiteStateStore plus four missing/null-store surfaces.
It captures the exact schema/read/parallel metadata, epoch/message inputs, bounded
search records and rendered tool text. Cases cover superseded/gapped epochs,
20-epoch coverage warnings with and without matches, the 20-match cap, Unicode
snippets and lowercase expansion, structured content, whitespace trimming, empty
queries, diagnostic quoting and foreign-session exclusion. NullStateStore returns
an empty search rather than the missing-durable-surface error; this distinction
must survive native binding. Unicode lowercase expansion can shift the Source
snippet offset because the lowercased position indexes the original string.

These are actual Source search/handler observations, not native runtime or SQLite
proof. Request reconstruction, historical nonfinite/surrogate records and native
tool binding remain separate work. No runtime defaults, dependencies or package
modules changed. Next implement a closed search result/reader and bind the native
read tool through server-owned session identity; do not enable the comprehensive
feature bundle while required tools remain absent.

Validation: export --check passes all 147 contract files. The 13 focused Source
diagnostics/query/reconstruction tests pass; all 19 scanning guards remain anchored.
git diff --check passes. The exporter reports the existing provider-model deprecation
warning. No Go implementation changed in this preparation slice; full Go/Python
regression and coverage were not rerun. Native search remains the next task.

Snapshot 147 now also retains each SQLite-loaded message as a JSON string.
The outer export sorts object keys, so replaying its structured epoch inputs alone
would reorder content and change searchable snippets. Native readers must decode
messages_json to preserve actual Source stored member order. Export --check passes
all 147 files after this correction.

## 2026-10-08 native task-child MCP binding

Baseline c9bc067 (Source snapshot 146). Explicitly selected runtime MCP connectors
bind fresh child registry/alias state from detached parent configuration. Default
capability selection still excludes MCP. Borrowed handlers keep their clients;
child registration changes only child inventory. Managed factories retain handles
in the enclosing parent lifetime through turn drain, supplying a cleanup owner for
Source's child-only factory gap. Raw servers remain operator-owned. AgentWorker is
a named runtime alias; the advertised Source task schema is unchanged.

Four differential recipes compare child catalogues/results, parent state/results
and factory/list/call identities. Fresh-state behavior is checked through connection
results and isolated parent aliases; the extra Source hook observation is not
compared directly. A blocked cancellation test verifies client close and workspace
reclamation wait for the child call to return. New nested/parallel MCP matrices,
remaining archival/scalar/transport profiles, full feature activation, native SQLite
driver approval and full G7 remain open.

Validation: focused child race tests, full go test ./... -count=1 -timeout=180s,
full go test -race ./... -count=1 -timeout=180s and go vet ./... passed. All 146
Source exports are current. Source scans, 10 selected MCP guards and 6 selected
subagent guards passed. Python regression: 2155 passed, 28 skipped, 3 dependency
warnings and 24 subtests in 101.32 seconds. No Source runtime modules changed;
separate invariant verification and the unrelated full mutation sweep were not
rerun. Coverage was not refreshed; the last Go 90.23% measurement belongs to
721b413 before MCP and is not a current-tree or migration-completion metric.

README baseline, canonical Mermaid, boundary/seam/hardening notes and generated
interactive architecture are updated. Archify deliver passes 9/9 showcase checks
with zero errors/warnings. Visual inspection remains skipped after prior access
denial. Specification SHA256:
a12233c6531a1d3c770a703bab1fcddee035948831acd714f8a220aeff05f8ca;
HTML SHA256: b6ebb2cba0b91ecefae6444ebde81cbcbc1ca505d52b85d379e127a382a086dd.

## 2026-10-08 task-child MCP Source contracts

Baseline 2a06cc3. Snapshot 146 runs four actual Source task/model/child recipes:
default capability exclusion, an explicitly selected fresh worker connector,
borrowed parent tools followed by child-only re-registration, and Explore readonly
denial. An explicit operator PermissionHook with empty rules isolates binding;
readonly remains monotonic and production defaults are not changed. It captures
both child request catalogues, ordered child/parent results, factory/list/call
identities, fresh child alias observations and unchanged parent connection state.
The recipes also prove Source accepts the worker runtime alias despite its narrower
advertised task enum. Export --check passes all 146 files. No Source runtime changes.
Native child binding and parent-retained cleanup are the next implementation slice.

## 2026-10-08 native InProcessMCP adapter

Baseline 3f19efa (Source snapshot 145). mcp.NewInProcess accepts named
InProcessTool definitions and context-aware InProcessHandler callbacks returning
immutable closed values. The public InProcessValue alias makes embedding possible
without importing internal packages; an external-package test proves the signature
and JSON decoding path. The client implements existing discovery/call/Close seams
without new dependencies or default feature activation.

Discovery preserves definition order and duplicates, while dispatch selects the
last handler per raw name, including nil -> unknown-tool text. Caller tool slices
and discovery containers detach; schema/annotation values are immutable. Scalar,
list and object results use Source Python str semantics. Ordinary callback errors
become Error text; context cancellation/deadlines propagate. Native panic isolation
returns type-only diagnostic text rather than leaking details or crashing the
consumer. Close is a reusable no-op and bare result text has no stdio cap; existing
agent recording/context limits still apply. Callbacks must honor cancellation and
synchronize shared state. Go object arguments are explicit closed values; arbitrary
Python objects/callable signatures are not fabricated.

Tests compare all twelve actual Source results and complete captured discovery
metadata, duplicate dispatch, nil/unknown tools, Close reuse and detached containers.
Native tests cover active cancellation after callback entry, pre-cancelled discovery,
panic isolation, 60,000-character bare results, invalid object arguments and public
embedding. Real model/gate tests prove same-batch publication and execution in auto
mode, retained external risk despite readOnlyHint, and no publication/callback
invocation after readonly connection denial. ManagedMCPClient conformance is checked;
existing manager/launcher lifetime tests cover composition, not a new local-client
HTTP lifecycle fixture.

Validation: focused in-process race tests; final full go test ./... -count=1
-timeout=180s, full go test -race ./... -count=1 -timeout=180s and go vet ./... passed.
All 145 Source exports are current; 19 scan anchors and 10 selected MCP mutation
guards passed. Python full regression: 2155 passed, 28 skipped, 4 warnings and
24 subtests in 93.72 seconds. Three dependency warnings and the previously observed
asyncio subprocess destructor warning (Event loop is closed) were recorded; the
originating subprocess remains unidentified. No Source runtime modules changed;
the separate invariant verifier and unrelated full mutation sweep were not rerun.
README outline and git diff --check passed. Coverage was not refreshed: the last
90.23% (21,311/23,618) belongs to 721b413 before MCP, not the current tree or migration
completion. Python coverage was not refreshed.

README baseline, canonical adapter node, boundary text, extension seam, hardening
note and interactive Language layout card are updated. Archify deliver passes 9/9
showcase checks with zero errors/warnings. Visual inspection remains skipped after
prior access denial. Spec SHA256
89331137fdde2abcf295f8ca1fdda65766bd0002be2282df8bb9b02c104915f6;
HTML SHA256 ddbd378b5241116a3c749e1b5f464cbc7ee776f4a46b1fa5cac00afad9d7aeb5.

Next: task-child MCP capability and lifecycle composition. Remaining malformed/
archival argument, model/tool scalar/transport and timing/input/output profiles,
full feature activation and full G7 remain open. Native SQLite still awaits approved
driver selection. The complete Python-to-Go objective remains active.

## 2026-10-08 InProcessMCP Source contracts

Baseline 17a5ea1. Snapshot 145 runs actual Source InProcessMCP with synchronous
and asynchronous handlers, closed scalar/list/object results, handler errors,
unknown/nil handlers, duplicate definitions with last-handler dispatch and no-op
Close followed by reuse. All twelve ordered results and discovery definitions are
captured. The complete Source exporter check passes 145 files; its subprocess
corpus emitted an Unknown child process pid warning and exited zero. Native
in-process implementation is the next slice. No Python runtime modules changed.

## 2026-10-08 native MCP launcher selection and operator configuration

Baseline 4f6b2a5 (Source snapshot 144). launcher.Options now carries MCPTools and
ordered []agent.ManagedMCPServer, forwarding trusted handles/factories to manager
composition. The independent --mcp-tools flag installs the connector with an empty
server list unless embedding code supplies servers. Python default CLI/server has
no server-list environment loader; servers enter through operator composition.
Inspection reports only mcp_tools, without invoking factories, allocating a
workspace or disclosing aliases/commands. No default feature activation, new
configuration parser or dependency was introduced. Full MINILOOP_FEATURES remains
unsupported until its complete bundle is implemented.

Native tests run the actual launcher/listener and compare three Source HTTP
recipes: disabled/configured, enabled/empty and enabled/configured. They compare
MCP catalogue projections, exact ordered results (including same-batch calls),
model output and list/call/close counts, and verify detached caller options.
The request fixture reads SDK message content through immutable closed values,
including cache_control annotations, rather than the stricter core message decoder.
A real native test-binary MCP child exercises launcher -> manager -> connector ->
stdio discovery/call and shutdown. Construction leaves the child unstarted;
authorized connection starts it, an explicitly registered credential is scrubbed,
its PID remains live until App.Stop and the child is reaped afterwards. Null
registry environment inheritance is preserved, matching Source; unregistered
credentials are not implicitly scrubbed. Separate tests preserve pure inspection
and comprehensive-feature refusal, including command-line dump selection.

Validation: focused launcher/CLI race tests; full go test ./... -count=1
-timeout=180s, full go test -race ./... -count=1 -timeout=180s and go vet ./...
passed against final native code. Python full regression: 2155 passed, 28 skipped,
3 dependency warnings and 24 subtests in 92.20 seconds. All 144 Source exports are
current; 19 scan anchors and 10 selected MCP guards passed. The unrelated full
mutation sweep and separate invariant verifier were not rerun; no Python runtime
package modules changed. README outline and git diff --check passed. Coverage was
not refreshed; 90.23% (21,311/23,618) belongs to 721b413 before MCP implementation,
not to the current tree or migration completion. Python coverage was not refreshed.

README baseline, canonical launcher selection edge, boundary text, extension seam
and hardening note are updated. The interactive Language layout card records the
new forwarding and real subprocess evidence. Archify deliver: 9/9 showcase checks,
zero errors/warnings. Visual inspection remains skipped after prior access denial.
Spec SHA256 17508d7978780a34bb0257bb0bc8116a1145946a27d0047e5b2ecefd0f452d75;
HTML SHA256 fb4b31842e3722021d5a1f7e5e630dea546bd313b47b2c7691453bf2288903e1.

Next: actual task-child MCP capability/lifetime composition and InProcessMCP.
Malformed/archival arguments, remaining model/tool scalar/transport and timing/input/
output profiles and full G7 remain open. Native SQLite still awaits approved driver
selection. The complete Python-to-Go objective remains active.

## 2026-10-08 MCP HTTP launcher Source contracts

Baseline 2cce6c8. Snapshot 144 runs real Source create_app manager_factory
composition, HTTP session creation/message admission, actual FakeAsyncAnthropic
model/tool batches and lifespan shutdown. Individual full_registry toggles select
only MCP beyond the default registry. It covers disabled/configured,
enabled/empty and enabled/configured selections, complete ordered results,
MCP request catalogue projections and list/call/close counts. No default source
server-list environment loader exists: servers enter through operator composition.
`export_go_contracts.py --check` verified all 144 files. Its existing subprocess
corpus emitted an Unknown child process pid warning; the comparison exited zero.
Native launcher selection/forwarding is the next slice. No Source runtime changes.

## 2026-10-08 native managed MCP connection lifetime

Baseline 447c9a3 (Source snapshot 143). Explicit ManagerServices.MCPTools binds
ordered ManagedMCPServer aliases to MCPConnection handles or typed cancellable
MCPConnectionFactory callbacks. ManagedMCPClient extends the named discovery/call
seam with Close() error; the existing native stdio client implements it without
new dependencies. One stable handle represents one underlying client, separating
object identity from raw server namespace and avoiding interface-value comparison.
Configuration is detached and duplicate aliases, ambiguous alternatives and
uninitialized handles are rejected before workspace allocation.

The common manager composition map binds fresh state for create/fork/restore and
teammate construction. Forks copy history, not discovered tools or connected alias
state. Each session retains acquired handles once, even across repeated aliases.
Delete revokes admission, asynchronously drains the admitted turn and owned work,
then releases references before scratch reclamation. Stop drains registered
consumers and pending deletion cleanups. Only the last acquired holder closes a
shared handle; unacquired configured clients remain operator-owned. New acquisition
waits for an in-progress close with context cancellation, permitting reusable
clients to restart afterwards. Close errors/panics enter bounded manager cleanup
diagnostics and do not stop remaining retained-client cleanup.

Retention happens before discovery, including non-nil factory results accompanying
errors and earlier raw-name replacement clients. This deliberately strengthens
Source's registered-client-map cleanup, which can lose these objects; exact Source
leak parity is not claimed. Factories must return created resources in their handle;
resources hidden behind nil returns remain factory-owned. Close must drain its
client and not recursively acquire its own handle. Raw RuntimeConfig MCPServers
and externally retained catalogues do not acquire managed lifecycle authority.

Native tests compare all six actual Source manager recipes: concrete/shared versus
factory-created clients, first/last delete, stop deduplication, repeated stop and
fresh fork MCP inventory/state. The custom Source base catalogue contains only
connect_mcp; fork comparisons explicitly select the native MCP portion rather than
claim equality of unrelated default workspace tools. Native-only tests hold
cancelled discovery until release to prove client/workspace retention, preserve
failed-factory cleanup, retain old same-name clients, deduplicate repeated aliases,
isolate close panic/error diagnostics, reject invalid configuration and serialize
close/reuse. The new stdio lifecycle seam is compile-checked; existing stdio tests
provide subprocess-close evidence, not a new managed real-subprocess fixture.

Validation: focused managed-MCP race tests; full `go test ./... -count=1
-timeout=180s`, full `go test -race ./... -count=1 -timeout=180s` and
`go vet ./...` passed against the final native source. All 143 Source contract
exports are current, 19 scan anchors passed, and `verify_guards.py -k mcp` caught
10 selected MCP mutations; the unrelated full mutation sweep was not rerun.
Python full regression: 2155 passed, 28 skipped, 4 warnings, 24 subtests in
101.11 seconds. Three dependency warnings plus one asyncio subprocess destructor
warning (Event loop is closed), observed during the owner-map test; attribution to
its originating subprocess remains unresolved. A focused owner-map rerun with
PytestUnraisableExceptionWarning promoted to errors passed all 4 tests in 0.43s.
No Python runtime package modules changed; the separate invariant verifier was not
rerun. README outline and `git diff --check` passed. Coverage was not refreshed;
the last Go statement result 90.23% (21,311/23,618) belongs to 721b413 before MCP,
and is not migration completion. Python coverage was not refreshed.

README review baseline, canonical Go manager-to-MCP ownership edge, boundary text,
extension seam and hardening note are updated. The interactive Language layout
card records lifecycle ownership. Archify deliver: 9/9 showcase checks, zero
errors/warnings. Visual inspection remains skipped after prior access denial.
Spec SHA256 235932e8cd142ac4638b158ce0938c1924b0156a5ffa59d4835c4d54b55a2e86;
HTML SHA256 318f26baac3b169929dbce933bd6faab75be22cbe78573142b11212e7bf1e1df.

Next: launcher/server configuration, task-child MCP scope and InProcessMCP.
Manager restoration shares the composition map but native SQLite still awaits
approved driver selection; no durable live-connection recovery is claimed.
Remaining malformed/archival argument, model/tool scalar/transport and timing/input/
output profiles and full G7 remain open. The full Python-to-Go objective stays open.

## 2026-10-08 MCP manager lifecycle Source contracts

Baseline 4bd7041. Snapshot 143 runs six actual Source SessionManager recipes,
using real connect_mcp Agent turns, shared concrete clients or per-session
factories, completed-boundary forks, deletion cleanup drains and repeated stop.
It records close identities after each operation, created-client counts and the
second session's initial MCP catalogue/connected state. Shared clients survive
first deletion and close with the last holder; stop deduplicates client identity.
Forks start with unconnected manager tools rather than inherited live clients.
`export_go_contracts.py --check` verified all 143 files. Native managed connection
ownership is the next implementation slice; no Source runtime modules changed.

## 2026-10-08 native raw-session MCP connection and inventory publication

Baseline 1c77855 (Source snapshot 142). RuntimeConfig.MCPTools explicitly adds the
closed connect_mcp input/schema and an external/exclusive runtime handler. Ordered
MCPServers contain exactly one named client or typed cancellable factory per alias;
configuration is detached at construction, duplicates rejected and empty aliases
retained. An empty server list still installs the Source (none) connector. No
default feature activation, dependency or ambiguous domain/service structure added.

The session owns an independent registry and connected alias map. Connection
resolves a trusted configured alias, creates a client lazily and registers its
tools through external-risk definitions. Repeat aliases return the original raw
client name without factory/discovery work. Unknown aliases return Source text.
Successful same-raw-name re-registration through another alias replaces the current
handler; cross-server normalized-name collision is refused/reported but the second
alias is still connected with Added tools: (none), matching Source. Withheld
credential diagnostics carry count and the first three returned names.

Inventory publication replaces the immutable catalogue, preserving retained old
snapshots. connect_mcp is an exclusive barrier: preceding parallel workers join
before replacement. Source _exec_tool_batch uses fresh registry lookups after each
exclusive effect, so later calls in the SAME batch can invoke discovered tools.
The native scheduler and gate retain that behavior; the next request fits fresh
schemas, while already built request/schema objects do not mutate. This is not
a claim that initial request schema fitting freezes every later handler lookup.
Initial ToolSelection applies to construction, and the authorized connector adds
newly discovered tools even when absent from that initial subset, as Source does.

Session.MCPProblems exposes the bounded Source-style ledger without taking the
turn lock. Callback implementations must honor cancellation and not reenter the
active session. Factory cancellation before registration does not publish an alias
or tools; this does not claim rollback of partially registered metadata or remote
effects. Factories retain lifecycle ownership, including unsuccessful connections.
Automatic close, shared reference counts and manager composition are still open.

Native tests run all five real Source Agent recipes and compare both fitted request
name lists, complete ordered results, alias state, connect schema, factory/list/call
counts, ownership and problem strings. They verify unchanged retained snapshots,
detached server configuration, external permission before factory construction and
parent cancellation without publication. Protocol tests cover required string name,
empty alias, strict invalid input rejection, canonical archive and recording masks.

Validation: focused native MCP race tests; full `go test ./... -count=1
-timeout=180s`, full `go test -race ./... -count=1 -timeout=180s` and
`go vet ./...` passed. Source contract export `--check` verified 142 files;
`verify_scans.py` verified 19 scan anchors and `verify_guards.py -k mcp`
verified 10 selected load-bearing guards. The unrelated full mutation sweep was
not rerun. Python full regression: 2155 passed, 28 skipped, 3 warnings and
24 subtests passed in 76.74 seconds. README outline and `git diff --check` passed.
No Python package-module changes; the separate invariant verifier was not rerun.
Coverage was not refreshed: the last Go statement result, 90.23%
(21,311/23,618), belongs to 721b413 before the MCP implementations. Python
coverage was not refreshed; statement coverage is not feature migration completion.

README baseline, canonical Mermaid, boundary text, extension seam and hardening
barrier note are updated. The existing interactive map records the connection flow
in its Language layout card. Archify deliver passes 9/9 showcase checks with zero
errors/warnings; visual review remains skipped after prior access denial. Spec SHA256
b1e6bf611faf85f91653b200f4496d7a6b9f58d868491086d5b32bb1cef38287; HTML SHA256 56b6f0fa2c9d68d01acae553ca07d422f26c55c19c3e2a44901aef759a37f45f.

Next: compose manager/client reference lifetime, failed-factory cleanup, fork/child
MCP scope and launcher configuration. InProcessMCP and wider malformed/archival
argument profiles remain open. Native session SQLite still awaits driver approval.
Remaining timing/input/output and model/tool scalar/transport profiles and full G7
remain open; the full Python-to-Go objective stays active.

## 2026-10-08 same-batch MCP connection Source contracts

Baseline e87772e. Source snapshot 142 runs five actual Python Agent model/tool
batches through install_mcp, lazy factories and register_mcp. It captures two fitted
request tool-name lists per scenario, complete ordered result strings, connect
schemas, alias/raw-name maps, factory/list/call counts and withheld/collision
diagnostics. Scenarios cover missing servers, aliases versus raw client names,
idempotent connection, same-batch discovered calls, same raw server replacement
through another alias, cross-server refusal, empty discovery/catalogues and empty
aliases. This proves the Source batch observes newly registered tools through
fresh lookups; it does not pin handler identity to the initial fitted schema list.
All 142 exports, all 19 source scan anchors and all 10 selected MCP guards pass.
No Python runtime package module or dependency changed. Native composition and
full regression gates are recorded in the following implementation checkpoint.

## 2026-10-08 native MCP registration and fixed runtime snapshots

Baseline a008dc8 (Source snapshot 141). A concrete MCP protocol variant lowers
server-defined arguments to immutable closed JSON objects before the agent/gate
layer; nested keys participate in canonical hashes and recording masks. Exact
MCP schema variants retain refs/numeric enums/extensions, including Source null
and bool metadata, without treating schema text as an argument validator or
authority. Request/schema archives round-trip through the namespaced decoder.
Arguments currently enforce finite/scalar-Unicode JSON; wider historical Source
argument profiles remain open. No any/interface{}, map[string]any or RawMessage
was added to a domain/service struct; no dependency or Python runtime change.

MCPRegistry.Register discovers outside its publication lock and preserves raw
server ownership for normalized names. Cross-server takeover is refused with a
bounded problem record; same-server replacement retains order and publishes a
fresh immutable catalogue. Existing returned catalogues/gates keep old handlers.
Description str projections and 4,000 Source-character caps match the fixtures.
The wrapper calls the original raw tool name with a bounded context. A wrapper
timeout returns Source text; cancellation of its parent propagates as cancellation.
Custom clients must honor context deadlines and be safe for their caller's sharing.

Risk is pinned external; readonly hints remain advisory. Definition construction
and catalogue admission allow that mismatch only for normalized MCP names with
external risk. Execution stays exclusive and capabilities empty. Readonly sessions
deny the external effect, interactive sessions require approval, and auto mode
retains the existing explicit permission behavior. RuntimeConfig.MCPCatalog installs
a fixed operator snapshot before session admission, with namespaced-external
validation and the existing ToolSelection filter. Default tool inventory stays ten.
Client lifecycle remains operator-owned, including clients in older snapshots.

Native tests compare seven actual Source registration scenarios/ten publications,
seven namespace normalizations and two original-name/timeout handler results. They
also verify old/new handler binding, readonly/interactive refusal despite the hint,
before rewrite -> guard -> permission -> execute -> after -> observer order, real
model-request session execution and terminal journal replay under current permission.
Protocol tests cover nested key masking without changing live arguments, canonical
Unicode input/archive round trips, exact schema variants and unsupported strict
argument/namespace rejection. Malformed Source registration profiles are not all
implemented; this is not a claim of complete MCP parity.

Validation: focused MCP protocol/registration/runtime race tests, full go test
./..., full go test -race ./... (-count=1 -timeout=180s) and go vet ./... pass.
Full Python regression passes separately: 2,155 passed, 28 skipped, 24 subtests,
three warnings in 75.87s. All 141 Source exports and all 19 source scan anchors
pass. verify_guards.py selects all 10 MCP guards plus readonly/risk drift and
external-tool approval (12 guards, all load-bearing); the unrelated full mutation
sweep was not rerun. No Python package-module changes, so verify_invariants.py
was not rerun. Logs/exit codes 0 are retained under /tmp/mini-loop-mcp-registration-
{go,python,contracts,guards,risk-guards}.{log,exit}. README outline and git diff
--check pass. Coverage was not refreshed; the previous 90.23% belongs to 721b413.

README review baseline, canonical Mermaid, boundary explanation, extension map and
hardening invariant note are updated. Existing interactive geometry is retained;
its Language layout card records the fixed snapshot/gate seam. Archify deliver
passes 9/9 showcase checks with zero errors/warnings. Visual inspection stays skipped
after prior access denial. Specification SHA256 df41473d60e3c756f6a892aa8077b49102bff2db2212810cae73bc8b2c1a41e6;
HTML SHA256 c15e8a8c2754634fb33dffad2139544c5c10e196a2509c457a53268de610f75e.

Live connect_mcp/session catalogue publication, manager/client reference lifetime,
child/fork composition, launcher activation and InProcessMCP remain open. Native
session SQLite still needs the unanswered driver approval. Remaining model/tool
scalar/transport and timing/input/output profiles and full G7 remain open. The
complete Python-to-Go goal remains active; this checkpoint does not narrow it.

## 2026-10-08 MCP registration contract preparation

Baseline edd6c46. Source snapshot 141 captures seven real register_mcp scenarios
and ten publications: separator normalization, lossy cross-server collision refusal,
same-server replacement, degenerate names, exact/over Unicode description caps,
non-string description str projection, advisory truthy readonly hints, exact refs/
numeric enum schemas and Source null/bool schema values, and same-server normalized
duplicate tools. Complete catalogue order/metadata, raw owners and problem strings
are retained. Seven namespace normalizations and two actual wrapped original-name/
timeout handler outputs are captured. All 141 Source exports and all 19 scanner
anchors pass. No Python runtime module or dependency changed. Native gate/runtime
composition is validated and recorded in the next implementation checkpoint.

## 2026-10-08 native MCP stdio client library

Baseline bb069d3 (Source snapshot 140). The dependency-free mcp package owns a
native argv child, detached passthrough settings, bounded stdout framing and
serialized JSON-RPC request ownership. A named ToolDescription carries inert
closed discovery variants; arguments must be closed JSON objects. Unsupported
non-text tool names fail at the discovery boundary. No domain any/interface{},
map[string]any or RawMessage, Python runtime invocation or new dependency added.

The Source initialization version/clientInfo and initialized notification are
explicit. Notifications and mismatched numeric IDs are skipped. Request IDs
remain monotonic across restart. Live children are reused; dead children are
reaped and restarted on the next operation, without retrying a failed effect.
Handshake/list waits use the configured timeout; tools/call uses its context.
Read timeout retains a live child and later RPCs skip its late response. A blocked
pipe-write cancellation kills/reaps the child instead of abandoning partial bytes;
this native cleanup detail is explicit, not claimed as exact asyncio cancellation.
Close sends SIGTERM, escalates after two seconds and allows later restart.

Registered names are withheld from the child unless explicitly passed through,
including names absent from the parent environment in the detached withheld view.
Null registry retains the Source deployment default. Eight-MiB framing discards an
oversized line and can resume; text caps count Source characters, while fallback
JSON is capped before the truncation-notice check. isError does not rewrite text.

Sixteen actual Source discovery/result cases compare complete output hashes and
character counts; small outputs also compare exact text. Native child-process
tests verify live reuse, failed effect once, restart/close handshakes and IDs,
registered-secret isolation, explicit passthrough, detached snapshots, handshake
and list timeout, late responses, RPC faults, oversized-line recovery and twelve
concurrent calls without crossed responses. Source failure classes are recorded;
native errors preserve failure, not exact Python exception bodies.

Validation: focused native MCP race tests and final full go test ./... / full
go test -race ./... (-count=1 -timeout=180s) / go vet ./... pass. Final Go log/exit
0: /tmp/mini-loop-mcp-go-final.{log,exit}. All 140 Source exports and all 19 scanner
anchors pass. Full Python regression passes separately: 2,155 passed, 28 skipped,
24 subtests, three warnings in 70.30s (/tmp/mini-loop-mcp-python-serial.{log,exit}).
The earlier concurrent run failed the existing 40-turn 0.5s timing gate at 0.69s;
no runtime/test threshold change was made. README outline and git diff --check pass.
No Python runtime modules/guards changed, so invariant/guard sweeps are not rerun.
Coverage was not refreshed; 90.23% belongs to 721b413, not this implementation.

README baseline/canonical Mermaid/boundary and extension seam are updated. The
interactive architecture records the operator library in its Language layout card;
existing diagram geometry is retained after new-node routing conflicts. Archify
delivery passes 9/9 showcase checks with zero errors/warnings; visual review remains
skipped after prior access denial. Specification SHA256
eb47d25a0f107ef8868464dbc9788a1f1954f88bbf8f541af49d7ff37949a47d;
HTML SHA256 2e716ab92fa65dcfb7aa7e3259fff4607b05c123890fe84d0fa148534068fef6.

This library is not yet registered through ToolGate, pinned catalogue publication,
connect_mcp, manager cleanup or launcher configuration. Those paths, InProcessMCP,
remaining timing/input/output and model/tool scalar/transport profiles, native
session SQLite and full G7 remain open. SQLite driver approval is still unanswered;
no dependency added. The complete Python-to-Go objective remains open.

## 2026-10-08 MCP stdio contract preparation

Baseline 721b413. Snapshot 140 captures sixteen actual Python StdioMCP
discovery/result cases against a scripted local subprocess: default schemas,
inert annotation/description variants, text/non-text blocks, isError text, empty
and Unicode fallback JSON, exact/over character caps, repeated Unicode, oversized
fallback, and malformed text/content/result failures. The peer also emits
notifications and mismatched IDs before every response. Large payloads use explicit
repeat recipes; complete result SHA256 and Source character counts avoid giant
fixture copies. All 140 contract exports and all 19 scanner anchors pass.
Full Python regression passes (2,155 passed, 28 skipped, 24 subtests, three warnings
in 70.30s). This records Source behavior, not MCP registration or native
session persistence. No Python runtime code or dependency changed.

## 2026-10-08 native unbounded ledger metrics and conversion phases

Baseline f292c5b (Source snapshot 139 preparation). IntegerCount retains canonical
private decimal text and performs arithmetic through local big.Int values; published
counts are immutable. MetricValue retains a closed historical counter until render.
Usage tokens cross PythonInteger conversion at ledger construction, while header
model/tool/error counters cross it at render. Token folds and cross-ledger totals no
longer overflow int64. Source decimal text rules, pinned Unicode decimal digits,
float truncation and the 4300-digit conversion/serialization/format limits are explicit.

Metric containers lower dict(value or {}) at the decoder boundary, including pair
sequences, string/object pairs, scalar key equality and unhashable-key refusal.
Unknown metric members remain inert archive values. Original header token fields
are overwritten by the actual usage fold, matching Source. Event truthiness uses
closed-value semantics. Render now returns (string, error); callers and the existing
full-HTML contract test handle this result. RenderUTF8 validates capped final text
after metric conversion. No mutable big.Int or ambiguous any/interface/map/RawMessage
was added to a domain struct. No dependencies or production defaults changed.

Native tests compare 71 actual Source decoded-wire recipes: complete folded metrics,
separate build/serialization/render failures, single and two-ledger HTML hashes and
284 real Source HTTP outcomes with complete successful owned page hashes. Sixteen
recipes fail build and thirteen fail single-page render. Historical files are explicit
seeds, not proof of native session SQLite. Source exception classes are recorded;
native typed errors preserve the phase but do not claim exact Python error bodies.

Validation: focused metric/ledger/archive/summary race tests; full go test ./...
with shared coverage; full race suite (-count=1 -timeout=180s); go vet ./... passed.
Logs and exit code 0 are retained in /tmp/mini-loop-metrics-{go,python}-validation.{log,exit}.
All 139 Source exports are current; all 19 source scan guards anchored. Full Python
regression: 2,155 passed, 28 skipped, 24 subtests, three dependency warnings in 78.52s.
README outline and git diff --check passed. No Python runtime modules changed;
Python invariant/guard sweeps were not rerun.

Fresh Go statement coverage: 21,311/23,618 (90.23%), merging duplicate source blocks
by maximum hit count across -coverpkg=./... test binaries. Profile:
/tmp/mini-loop-trace-metrics-coverage.out. Python coverage was not refreshed;
statement coverage does not measure feature migration completion.

README baseline, canonical Mermaid, boundary text, extension map and architecture
source/generated HTML are updated. Archify passed 9/9 showcase checks with zero
errors/warnings. Specification SHA256:
fb66e7ff264a0c5c2da1ca89be0ce793f4dcfe353bef189fed4c97e3fab585f2.
HTML SHA256: 78200504fefb0b038551bd232032788c38691af091527136701bd6a0a57b7d85.
Visual review remains skipped after prior access denial. Permissive timing/input/output,
remaining model/tool scalar and transport profiles, MCP, native session SQLite and
full G7 remain open. SQLite driver approval remains unanswered; no dependency was
added. Next capture actual Source timing/input/output behavior and advance remaining
native capabilities that can be implemented without the pending driver decision.
The complete Python-to-Go objective stays open.

## 2026-10-08 ledger metric conversion contract preparation

Baseline b0c1111. Source snapshot 139 executes seventy-one decoded-wire recipes
through real build_ledger/render_html and 284 FastAPI outcomes. It records build,
metric serialization and render failures separately, complete folded metric values,
full single/two-ledger page hashes and owned HTTP page hashes. Cases cover booleans,
binary-float truncation, Unicode decimal text, underscores/whitespace, nonfinite and
surrogate refusal, arbitrary integers, the 4300-digit conversion/format limit,
unbounded token sums, pair-container normalization and Python scalar key equality.
These fixtures prepare native metric conversion without narrowing the full port or
claiming timing/input/output, native SQLite, MCP or full G7 completion.

## 2026-10-08 native historical summary field variants

Baseline 801584b (Source snapshot 138). TrajectorySummaryArchive adds named closed
model/workspace/build, metrics and timestamp values to read-side summaries. Writer
metadata remains concretely typed. Native text projections and bounded integer
counters remain available; full historical values use ArchiveValue. Standard summary
MarshalJSON is strict, and HTTP owned lists filter before recursive recording masks
and final strict UTF-8 encoding. Observations never configure model/workspace/build
execution authority. No domain any/interface/map[string]any or RawMessage was added.

Streaming scans retain only summary-relevant fields and terminal metrics, preserving
source scalar identity without materializing event bodies. Invalid truthy non-object
metadata is a distinct summary error: summary/list and owned admission fail as in
Source, while direct document reads still succeed. Empty legacy metrics render with
zero ledger totals; truthy non-object metrics retain their archive/list values but
fail ledger construction. Existing JSON/HTML download policies and ownership/size/
append-lock boundaries remain in place. Source errors propagate with native error
messages; exhaustive error-body/exception-class parity is not claimed.

Native tests compare forty complete Source documents, summaries/lists or their shape
failures, direct HTML hashes and 280 actual Source HTTP outcomes. Successful list
bodies and complete owned HTML response hashes match. Historical files are explicit
seeds; this is not runtime-generated metadata or native session SQLite evidence.

Validation: focused summary/metadata/archive/ledger race tests; full go test ./...
with shared coverage; full race suite (-count=1 -timeout=180s); go vet ./... passed.
The direct-page test passed its focused race gate after full gates. Original full
validation handles became unavailable with no live processes; rerun gates have logs
and exit code 0 in /tmp/mini-loop-summary-{go,python}-validation.{log,exit}.
All 138 Source exports are current; all 19 source scan guards anchored. Full Python
regression: 2,155 passed, 28 skipped, 24 subtests, three dependency warnings in 79.24s.
README outline and git diff --check passed. No dependencies or Python runtime modules
changed; Python invariant/guard sweeps were not rerun.

Fresh Go statement coverage: 21,176/23,456 (90.28%), merging duplicate source blocks
by maximum hit count across -coverpkg=./... test binaries. Profile:
/tmp/mini-loop-trajectory-summary-coverage.out. Python coverage was not refreshed;
statement coverage does not measure feature migration completion.

README baseline, canonical Mermaid, boundary text, extension map and interactive
architecture source/generated HTML are updated. Archify passed 9/9 showcase checks
with zero errors/warnings. Specification SHA256:
1c439f7adf6ae81b6a53be413ccf16293cc0cafe0b532718b35966b3eb5e5443.
HTML SHA256: 6eb144818c64cf01de4455657d963eb63b73036b0c0e829f2c264f4e39490cb1.
Visual review remains skipped after prior access denial. Ledger metric integer
conversion/overflow profiles, permissive timing/input/output, model/tool scalar and
transport profiles, MCP, native session SQLite and full G7 remain open. Next capture
actual Source ledger metric conversions and timing/output behavior before extending
those read-side fields. The complete Python-to-Go objective stays open.

## 2026-10-08 historical summary field contract preparation

Baseline 4c48db4. Source snapshot 138 executes forty historical file recipes:
nine closed JSON variants for each model/workspace/build field, four truthy
non-object metadata shapes and nine metrics shapes. Real Source get, summary,
list, direct ledger rendering and 280 FastAPI outcomes are retained, including
complete successful list values and HTML response hashes. Non-object metadata
fails summary/list with AttributeError while get still succeeds. Nonfinite or
surrogate metadata fields can render a page while their JSON response fails.
These fixtures prepare the native summary implementation; runtime-generated
metadata, permissive timing/input/output, native SQLite and full G7 remain open.

## 2026-10-08 native historical trajectory metadata projections

Baseline 1e8b2cc (Source snapshot 137 preparation). Streaming summary decoders now
select used header/end fields before strict typed decoding. Only model/workspace/build
metadata enters the typed summary; unused inert members never invalidate that read.
The file and ledger boundaries likewise select their used fields and metrics. Bare
file assembly constructs a closed archival document directly, removing redundant
strict intermediate encoders and the obsolete event-replacement helper.

Store.JSON retains complete original metadata and terminal metrics, including empty
legacy metadata, unknown nonfinite/surrogate members and integer/float identity.
The first header and last terminal supply archival values; known native fields and
summary counters remain named types. Public strict/detail, legacy/download and final
HTML encoding policies are unchanged. Ownership, size caps, streaming scan and append
locks remain in place. No domain any/interface/map[string]any or RawMessage was added.

Tests compare all eleven Source documents in full, three known summary metadata
fields, complete ledger/file/trajectory-id/session HTML hashes and fifty-five actual
Source HTTP outcomes with complete successful owned-view hashes. These are explicit
historical file seeds, not runtime-generated metadata or native session SQL proof.

Validation: focused archive/metadata/ledger race tests; full go test ./... with
shared coverage, full race suite (-count=1 -timeout=180s) and go vet ./... passed.
After simplifying the new fixture decoder, its focused traceview race gate passed.
All 137 Source exports are current; all 19 source scan guards anchored. Full Python
regression: 2,155 passed, 28 skipped, 24 subtests, three dependency warnings in 93.83s.
README outline and git diff --check passed. No dependencies or Python runtime modules
changed; Python invariant/guard sweeps were not rerun.

Fresh Go statement coverage: 21,089/23,369 (90.24%), merging duplicate source blocks
by maximum hit count across -coverpkg=./... test binaries. Profile:
/tmp/mini-loop-trajectory-metadata-coverage.out. Python coverage was not refreshed;
statement coverage does not measure feature migration completion.

README baseline, canonical Mermaid and boundary documentation, extension map and
interactive architecture specification/generated HTML are updated. Archify passed
9/9 showcase checks with zero errors/warnings. Specification SHA256:
4138ec331008203613f5c66d2cf709c342036dc08cf80962134c45e9f182f97b.
HTML SHA256: fd4a7c9b7cf1942701c60a15183714218fe94fe5a3ff67f5b53630dd0aac5308.
Visual review remains skipped after prior access denial. Arbitrary known metadata
field types, complete historical summary metrics, permissive timing/input/output and
model/tool scalar profiles, MCP, native session SQLite and full G7 remain pending.
Next capture actual Source known metadata and summary-metric type profiles before
extending retained projections. The complete Python-to-Go objective stays open.

## 2026-10-08 historical trajectory metadata contract preparation

Baseline cf5fbec. Source snapshot 137 records eleven actual historical metadata
recipes: null/false/zero/empty text/empty list/object, arbitrary object members,
ignored header/end fields and unknown metrics. Real TrajectoryStore get/summary,
trace-view file assembly/rendering and fifty-five FastAPI HTTP outcomes are captured.
A direct ledger input also contains ignored nonfinite/surrogate root fields. Full
HTML response hashes and complete archival document values are retained for native
comparison. These fixtures do not establish arbitrary model/workspace/build types,
permissive timing/input/output fields, native session SQLite or full port completion.

## 2026-10-08 native workflow ledger and final UTF-8 boundaries

Baseline ebf0e68 (Source snapshot 136). Native ledger construction, JSONL assembly
and path/id/session loading now retain historical workflow scalar values through
immutable closed JSON values. Inspector strings preserve Source insertion order,
integer/float identity, nonfinite numbers and surrogatepass characters. Rendering
applies Source character caps before final UTF-8 validation, permitting a fully
omitted surrogate tail while rejecting visible surrogate text or keys.

Store.JSON provides an ASCII archival projection. Owned details use strict UTF-8,
downloads use legacy UTF-8, and views retain archival values until page rendering.
The CLI applies the same final boundary and leaves private 0600 output empty on
render failure. Ownership, admission, masking and size bounds remain in place.

Native tests compare fourteen actual Source ledgers, complete valid HTML hashes,
file assembly and CLI path/id/session output. Seventy Source HTTP outcomes and
successful complete HTTP HTML body hashes match. These tests do not establish
native session SQLite or permissive metadata parity.

Validation: focused race tests; full go test ./... with shared coverage; full
race tests (-count=1 -timeout=180s); go vet ./... passed. All 136 Source exports
are current; all 19 scan guards anchored. Full Python regression: 2,155 passed,
28 skipped, 24 subtests and three dependency warnings in 69.74s. README outline
and git diff --check passed. No dependencies or Python runtime modules changed;
Python invariant/guard sweeps were not rerun.

Fresh Go statement coverage: 21,039/23,326 (90.20%), merging duplicate source blocks
by maximum hit count across -coverpkg=./... test binaries. Profile:
/tmp/mini-loop-workflow-trace-view-coverage.out. Python coverage was not refreshed;
statement coverage does not measure feature migration completion.

README baseline, canonical Mermaid, boundary text and generated architecture map
are updated. Archify passed 9/9 showcase checks with zero errors/warnings.
Specification SHA256: a4533638fad5c9d48ffa1be106adb039152ba8b7cd629336f94a1b93ef2ff49b.
HTML SHA256: b26d108b653fdd69b6663dbb31ff7e86b77d745fcd70fb7ef6016ffefaa6fbf5.
Visual review remains skipped after prior access denial. Permissive workflow and
trajectory metadata, remaining model/tool/timing scalar and transport profiles,
MCP, native session SQLite and full G7 remain pending. The next slice should
capture actual Source metadata profiles. The complete Python-to-Go port stays open.

## 2026-10-08 workflow trace-view contract preparation

Baseline b474699. Source snapshot 136 compares fourteen actual ledgers and complete
page hashes, JSONL file assembly, direct CLI output and seventy FastAPI HTTP outcomes.
It reuses the ten real trajectory/privacy recipes and adds escaped surrogate text/keys,
a fully capped surrogate tail and nonfinite Unicode/HTML payloads. Source ledger
contents retain surrogatepass characters; final UTF-8 output fails only if they remain
in the rendered page. CLI files stay private and empty on failed writes. These source
fixtures prepare the following native ledger/view implementation without narrowing
the full Python-to-Go objective or claiming permissive metadata/native SQL parity.

## 2026-10-08 native workflow trajectory scalar files

Baseline 50715c8 (Source snapshot 135 preparation). TrajectoryRecord.MarshalArchiveJSON
now hands immutable historical workflow payloads to the real file writer. The writer
applies content privacy before legacy UTF-8 encoding: nonfinite numbers survive;
redacted surrogate text can disappear; surviving surrogate text/keys fail without
appending a record. Ordinary strict JSON encoders remain unchanged.

Native scanners classify records from closed values while retaining original event
bytes. Summary metrics, document assembly, streaming export and filtered iteration
now retain nonfinite payloads instead of silently skipping valid Source records.
The privacy transform uses the closed Value variants, preserving integer/float
identity and Source redaction labels, including floats and surrogate character counts.
Owned JSON downloads use archival UTF-8 semantics; ordinary JSON details remain
strict. Size, ownership, sequence/provenance and append-lock boundaries remain in place.

Actual native files are compared against all ten Source privacy/write recipes,
including append failure, completed JSONL records, full documents, summaries and
filtered iteration. Forty actual Source HTTP outcomes match native routes: ordinary
nonfinite details fail with 500; downloads remain available; foreign owners get 404.
Successful native JSON HTTP bodies are also compared with the Source document.
These are real native trajectory file tests, not proof of native session SQLite.

Validation: focused trajectory/scalar/UTF-8 race tests; full Go tests with shared
coverage; full Go race tests (-count=1 -timeout=180s); go vet ./... passed. Strengthened
HTTP document equality assertions passed their focused race gate after full gates.
All 135 Source exports current; all 19 scan guards anchored; full Python regression:
2,155 passed, 28 skipped, 24 subtests, three dependency warnings in 72.34s.
README outline and git diff --check passed. No dependencies or Python runtime modules
changed; Python invariant/guard sweeps were not rerun.

Fresh Go statement coverage: 20,997/23,272 (90.22%), merging duplicate source blocks
by maximum hit count across -coverpkg=./... test binaries. Profile:
/tmp/mini-loop-workflow-trajectory-coverage.out. Python coverage was not refreshed;
these statement percentages do not measure feature migration completion.
Architecture baseline, canonical Mermaid, boundary text and generated map are updated.
Archify passed 9/9 showcase checks with zero errors/warnings. Specification SHA256
48e047ea58cbadbfcd55e023b899daa7b55bfca5b7bd633a9ecad98afc68633e;
HTML SHA256 794747982f5277915bf63e06824ea457f3d98bb09ec2a6cc29f70e416c3c7d65.
Visual review remains skipped after prior access denial. Legacy ledger/HTML consumers,
permissive trajectory/workflow metadata and historical escaped-surrogate seed profiles,
remaining malformed/transport profiles, MCP, native session SQLite and full G7 remain
pending. Next archive slice must compare Source ledger/view behavior for these payloads.
The complete Python-to-Go port stays open.

## 2026-10-08 workflow trajectory contract preparation

Baseline 164d5c8. Source snapshot 135 records ten actual trajectory file/privacy
recipes and forty FastAPI HTTP outcomes. Nonfinite payloads, surrogate text/keys,
privacy-eliminated surrogates and nonfinite redaction labels are checked against
real append results, summaries, documents, filtered iteration and completed files.
Header timestamps and IDs are normalized; integer/float payload identity remains
intact. The Source corpus proves JSONL/JSON downloads can retain archival numbers
while ordinary detail responses reject them. It prepares the following native file
implementation without claiming native session SQLite or complete G7 parity.

## 2026-10-08 historical workflow scalars and owned SSE

Baseline 3b3a0be. Source snapshot 134 runs five actual WorkflowEvent recipes through
managed capture, SQLite and the FastAPI observe-route iterator: nonfinite numbers,
surrogate text/keys, nested combinations and masked-key collisions. It records live
and stored projections, actual SSE frames, foreign-owner refusal and UTF-8 failures.
Source SQLite accepts nonfinite payloads; surrogate writes degrade with
UnicodeEncodeError while escaped SSE still publishes the event. Framing normalization
removes only volatile session/time values and retains payload scalar identity.

Go archival decoding now parses closed inert payload values before decoding typed
metadata. DecodeStoredEvent admits legacy JSON only for known workflow observations;
standard metadata/alias/version/session guards remain. The explicit record encoder
MarshalWorkflowArchiveJSON preserves Source archival/SSE scalars and recursively
masks decoded keys/values. Standard MarshalJSON remains strict. CatchUpEvents still
validates scope/order, bounds the window and re-decodes every record before delivery.
Owned HTTP SSE uses the legacy vocabulary for observations, without live authority.

Native differential tests cover capture, masking, detached archival round trips,
non-observational scalar refusal and Source frames over real TCP configured-store
catch-up. Foreign ownership remains 404. Native tests use an injected backing; they
do not prove SQLite writes or the Source surrogate-write failure in Go SQL.

Validation: focused workflow/archive/catch-up race tests; full go test ./... with
shared-package coverage; full go test -race ./... (-count=1 -timeout=180s); go vet.
All 134 Source exports current; all 19 scan guards anchored; full Python regression:
2,155 passed, 28 skipped, 24 subtests, three dependency warnings in 79.52s. README outline and git diff --check passed. No dependencies or
Python runtime modules changed; invariant/guard sweeps were not rerun.
Fresh Go statement coverage: 20,945/23,235 (90.14%), merging duplicate blocks by
maximum hit count across -coverpkg=./... test binaries. Profile:
/tmp/mini-loop-workflow-legacy-coverage.out. Python coverage was not refreshed;
statement coverage is not feature migration completion.

README baseline, canonical Mermaid, boundary text and generated map are updated.
Archify passed 9/9 showcase checks with zero errors/warnings. Specification SHA256
8d04d47bbe7b3f6ce3f023df8232ac0a4741f23440c40551d948eaa2388c55f9;
HTML SHA256 4f65cf6f8175d972b7277dfa00229ea3b79fe3b827cf0809bde3d6f53ae3379e.
Visual review remains skipped after prior access denial. Native trajectory scalar
fidelity, permissive constructor metadata profiles, MCP, native session SQLite,
remaining malformed/transport profiles and full G7 remain pending. Next archive
slice must preserve these payload values through the real native trajectory writer,
including Source's UTF-8 surrogate-write degradation. The complete port stays open.

## 2026-10-08 complete finite workflow observations

Baseline 474ee49 (Source snapshot 133 preparation). The Go observation decoder
now accepts all eighteen Source workflow kinds and optional PhaseID. Complete finite
JSON object payloads use the existing immutable closed Value representation, retaining
unknown nested fields, sparse progress, historical decisions and large integer/float
identity. Native live service events keep their twelve named payload variants;
ObservationPayload exposes historical data and Progress reports only native progress.
Explicitly empty archived event IDs match Source constructors; native service event
identity remains required. Alias/version/session guards remain in place.

Native differential tests compare all 22 Source observations through raw round trips,
recursive secret masking, managed backlog/subscriber/sink, injected event storage and
stored-event restoration. All ten constructor refusals match Source diagnostics.
Payload claims of explicit human authority or workflow.launch approval remain inert;
restored RunContext is untrusted and grants no capability. Reserved observation kinds
do not activate controllers or introduce runtime emissions or new defaults.

Validation: focused workflow observation/session/archive race tests; full Go tests
with shared-package coverage and full race tests (-count=1 -timeout=180s); go vet;
all 133 Source exports current; all 19 scan guards anchored. Full Python regression: 2,155 passed, 28 skipped, 24 subtests,
three dependency warnings in 72.93s.
README outline and git diff --check passed. Python runtime modules and guarded Source
behavior did not change, so invariant/guard sweeps were not rerun. No new dependencies.

Fresh Go statement coverage: 20,887/23,168 (90.15%), merging identical source blocks
by maximum hit count across -coverpkg=./... binaries. Agent 7,570/8,431 (89.79%),
httpapi 1,646/1,834 (89.75%), launcher 152/189 (80.42%) and workflows 1,524/1,655
(92.08%). Profile: /tmp/mini-loop-workflow-archive-coverage.out. Python coverage was
not refreshed; these statement percentages do not measure Python feature parity.

README baseline, canonical Mermaid, boundary text and generated interactive map are
updated. Archify passed 9/9 showcase checks with zero errors/warnings. Specification
SHA256 4856e0b532ea1d7ec790b72774f944d6849925835de96b6a3dd981d2224326b4;
HTML SHA256 62b06e0342f52e83cf947d441ff6ab3c6dbf4ddf2ce61e04d9d8f5305ece9b56.
Visual review remains skipped after prior access denial. Legacy non-finite/surrogate
and permissive constructor header profiles, MCP, native session SQLite, remaining
malformed/transport profiles and full G7 remain pending. The complete port stays open.

## 2026-10-08 workflow archive contract preparation

Baseline 474f8e8. Source snapshot 133 records all eighteen WorkflowEvent kinds
through actual constructors and managed-session masking/capture backed by Python
SQLite. The isolated exporter module records 22 valid finite JSON observations and
ten constructor refusals, including sparse and unknown nested payloads, optional
phase/empty identities, an explicitly empty event ID and historical decisions.
Timestamp/session normalization removes only nondeterministic framing; integer/float
payload identity and large integers remain intact. These fixtures are evidence for
the following Go archive implementation, not proof of native SQLite or execution
authority. Legacy scalar and permissive header profiles remain outside this corpus.

## 2026-10-08 workflow launcher activation

Baseline 3789ae9. The standalone launcher now supports Source's
MINILOOP_EXPERIMENTAL_WORKFLOWS setting and an independent --workflow-tools
selector. Logical OR selects ManagerServices.WorkflowTools; all four typed workflow
caps are passed to the existing manager constructor and captured before service
creation. New() honors the Source environment setting without operator injection.
Unsupported() no longer falsely reports workflows unavailable. Default-off behavior
and comprehensive MINILOOP_FEATURES refusal remain explicit. --dump-config reports
workflow_tools selection without manager, provider, listener or workspace effects.

Snapshot 132 runs the real default Python create_app lifespan with no injected
manager or worker. Six default/enabled/disabled/authenticated/custom-cap profiles
capture health, owned empty lists, installed tools, caps, pool and journal binding.
Native launcher comparisons verify visible selection/catalogues across those same
profiles; custom-cap admission probes exercise all four captured process limits.
Existing manager snapshot 128 retains private pool/journal identity and lifecycle
proof. A Python configuration comment says the server ignores the flag, but the
actual lifespan forwards it; this snapshot follows executable Source behavior.

Native TCP integration uses the real HTTP model adapter and FreshWorkflowRunner,
submits an artifact, observes owned result data and rejects foreign-owner access.
A forced Workflow model call from an ordinary HTTP message creates no new run even
in auto permission mode. Shutdown cancels an active upstream worker request and
joins manager task/publication ownership. The explicit CLI flag selects the same
service as the Source environment path. No authority/default/persistence widening
is introduced; workflow graphs and the default journal remain process-local.

Validation passed: focused Workflow/Unsupported launcher/config/CLI race suite;
full go test ./... and go test -race ./... (-count=1 -timeout=180s); go vet ./...;
all 132 Source exports current; all 19 scan guards anchored; full Python regression
with 2,155 passed, 28 skipped, 24 subtests and three dependency warnings in 88.84s.
README outline and git diff --check passed. Python invariant/guard sweeps were not
rerun because no Python runtime package or guarded Source behavior changed.
Architecture baseline, canonical Mermaid annotations,
boundary text and generated interactive map are updated. Archify passed 9/9 showcase
checks with zero errors/warnings. Specification SHA256:
050ab039c672fdb90140ad81a396d66feed0ffa43e068c9a9d2b1b4c95b47c27;
HTML SHA256 627c643753aeace97b304b3b5b036464c289393ba2aecc4ca0158201ff995c0f.
Visual review remains skipped after prior access denial. No dependencies or Python
runtime package changes were introduced. Coverage was not refreshed; 90.23% belongs
to 136cd53. Six reserved event projections, open-payload archive fidelity, MCP,
native session SQLite, remaining malformed/transport profiles and full G7 remain
pending. The complete Python-to-Go port stays open.


## 2026-10-08 authenticated workflow HTTP launch

Baseline 06a9584. POST /sessions/{session_id}/workflows now admits a required
closed request object and delegates definition/args validation and execution to
the optional manager-owned service. The four Source workflow HTTP operations are
installed as native routes. Request-model validation follows definition, args and
action_id order before owner lookup, after ingress authentication. Omitted args
become an empty object; nullable/empty action_id selects a fresh wfhttp_ identity;
unknown request fields are ignored, including nonserializable legacy extra values.
Retained JSON fields lower into the existing immutable closed Value sum, preserving
integer/float and legacy scalar identity. Diagnostic values never confer authority.

An authenticated deployment and admitted session owner are required before stamping
explicit_human authority. The narrowly named trusted constructor stamps only
workflow.launch, caller actor, http channel and mini_loop.server. Message identity
is msg_ plus action_id, so retries bind the same immutable journal identity. General
HTTP messages retain their existing untrusted authority. The service owns launch,
replay conflicts, worker execution and publication; HTTP launch_turn remains Source
zero. Permission/journal/lookup/validation failures map to 403/409/404/400, with
Source's 500-character truncation for known 400 diagnostics; unexpected errors stay
private. Default manager/launcher workflows remain disabled.

The differential race test compares all 44 actual FastAPI fixture cases, including
exact request-model errors, disabled/open-deployment/foreign-owner refusals, launch
provenance, same action replay, changed payload or parent conflicts, ignored legacy
extra fields, operator cancellation and terminal replay/list. Additional native tests
execute real FreshWorkflowRunner artifacts and verify generated identities for
omitted/null/empty action_id. The existing typed runtime/core/journal tests retain
worker readonly/capability, ownership, cancellation and pruning boundaries.
This does not establish exhaustive malformed nested definition, legacy retained
scalar, transport or archival parity. Those profiles remain part of G7 work.

Final gates passed: focused HTTP/definition/admission race suite; full go test ./...
and go test -race ./... (-count=1 -timeout=180s); go vet ./...; all 131 Source
exports current; all 19 scan guards anchored; full Python regression with
2,155 passed, 28 skipped, 24 subtests and three dependency warnings in 82.26s.
README outline and git diff --check passed. Python invariant/guard sweeps were
not rerun because no Python runtime package or guarded Source behavior changed.
No dependencies or ambiguous Go business types were introduced. Coverage was
not refreshed; 90.23% (20,742/22,989) belongs to 136cd53.

README baseline, canonical Mermaid/boundary explanation and generated interactive
map now describe authenticated HTTP launch. Archify passed all 9 showcase checks
with zero errors/warnings. Specification SHA256:
0adf7cbfb0b8257939714d41eaa9b348e4c2a0b9dd0d7f74e01accccdb3b3e2b;
HTML SHA256 c5a786a03bae0440ac20c5e005e87301768447d4147ac31bc4d21ca8c70765aa.
Visual review remains skipped after prior access denial. Launcher activation, six
reserved event projections, open-payload archival fidelity, MCP, native session
SQLite, remaining profiles and full G7 remain pending. The complete port stays open.


## 2026-10-08 workflow HTTP Source contracts and constructor diagnostics

Baseline 136cd53. Source snapshot 131 records 44 actual FastAPI workflow HTTP
cases across disabled, anonymous and authenticated deployments. The real manager,
service, journal, engine and store run; only worker execution is replaced with a
blocking callable. It captures request-model error order, auth/owner refusal,
launch/cancel/list, unchanged and conflicting actions, terminal replay, ignored
legacy-valued extra fields and the stored human provenance. HTTP launch is prepared
by this snapshot; the native route is delivered in the following implementation slice.

Actual HTTP exercise exposed generic native missing-definition-field errors. The
closed definition decoder now retains unknown top-level keys for a constructor
refusal after typed lowering, and exposes exact Source missing name/return_from
TypeError details. Three new actual admission profiles bring that corpus to 23.
The prior unknown-field exception in the native differential test is removed;
that profile now compares error kind and detail. Supported definition/hash identity
is unchanged. This does not establish exhaustive malformed-constructor parity.

Validation passed: focused definition/admission and actual workflow HTTP race
suite; full go test ./... and go test -race ./... (-count=1 -timeout=180s);
go vet ./...; all 131 Source exports current; all 19 scan guards anchored; full
Python regression with 2,155 passed, 28 skipped, 24 subtests and three dependency
warnings in 82.26s. README outline and git diff --check passed. Python invariant/
guard sweeps were not rerun because no runtime package or guarded Source behavior
changed. No dependencies changed. Coverage was not refreshed; 90.23% belongs to
136cd53. HTTP implementation was verified in the working tree alongside this
preparation, and is committed separately next.
The full port remains open; native HTTP launch, launcher activation, reserved event
variants, archival fidelity, MCP, native session SQLite and full G7 remain pending.


## 2026-10-08 owner-scoped workflow HTTP reads and cancellation

Baseline d32c3e5. A focused slice adds GET session workflow list, GET run detail
and POST run cancel. Typed responses delegate to the optional manager-owned
service. Disabled lists expose enabled=false with an empty array; disabled detail
and cancel return 404. Session ownership is checked before service access, and
missing or foreign runs remain indistinguishable. Cancel does not mint launch
capabilities: existing session ownership suffices to reduce capability.

Required cancel-body validation precedes session lookup, after shared ingress
authentication. Missing/null bodies, non-object bodies and non-string reasons retain
Source validation categories; omitted reason defaults to requested by operator,
and extra fields are ignored. Cancellation awaits owned worker settlement through
the service before returning its typed terminal status.

Native HTTP race tests cover disabled/enabled projections, owner and run-session
isolation, validation/auth/lookup ordering, Unicode reasons, ignored extra fields,
worker cancellation/join and repeated terminal cancellation. Python server routes
were inspected directly; no new differential HTTP snapshot was added in this slice.
The existing 130 Source snapshots remain the previous evidence corpus. These tests
do not establish exhaustive malformed-body or archival parity.

Validation passed: focused WorkflowHTTP race tests; full go test ./... and
full go test -race ./... (-count=1 -timeout=180s); go vet ./...; all 130 Source
exports current; all 19 scan guards anchored; full Python regression with
2,155 passed, 28 skipped, 24 subtests and three dependency warnings in 76.21s.
Fresh shared-package Go statement coverage is 90.23% (20,742/22,989), using
-coverpkg=./... and merging identical source blocks by maximum hit count. This
measures executed statements, not Python feature parity. README outline and
git diff --check passed. Invariant/guard sweeps were not rerun because no Python
runtime package or guarded Source behavior changed.

README baseline, canonical Mermaid annotations, boundary explanation and generated
interactive map are updated together. Archify showcase validation passed 9/9 with
zero errors/warnings; visual review remains skipped after prior access denial.
Specification SHA256: ef916c4915dff4ea57b2f8fa58c5bd91757556d9f8111e4e19ac3226ba6f264f.
HTML SHA256: fae2485547e09e51c3d152ffe52fda9aa04f727bc2cf7214bd76381d42e3434e.
No dependencies or Python runtime modules changed.

HTTP launch, launcher activation, six reserved event projections, open-payload
archive fidelity, MCP, native session SQLite, remaining profiles and full G7 remain
pending. The complete Python-to-Go port remains open.


## 2026-10-08 owned later-turn workflow result injection

Baseline 182c53b. W6C installs notification delivery for manager-bound workflow
parents after custom injectors and before steering/posture. The real managed turn
counter controls eligibility: launch-turn notifications remain pending; later real
turns append the Source untrusted-artifact-data user message and then acknowledge.
No new event or trusted authority is created. Bare runtimes, autonomous teammates,
FreshWorkflowRunner and delegated children have no manager-bound delivery path.

The concrete parent appender checks session/owner/turn identity, current live manager
binding and cancellation before changing history. Construction/append failure releases
claims through ServiceViews; acknowledgment failure retains the append effect, as in
Source. The core turn lock owns append and the normal managed admission serializes
turns. This is process-local live-context append-before-ack, not a durable state/outbox
transaction or exactly-once guarantee across restart. Existing truncation, bounded
notification batches, claim fencing and pruning semantics remain unchanged.

Snapshot 130 runs the actual Python manager-installed injector with real service,
engine, store and journal; only worker execution is replaced. It compares same-turn
suppression, custom-injector order, exact wrapper/result text, pending/ack counts and
nonduplication on a further real turn. Native race tests compare the same managed
turns and verify deleted-parent refusal, unchanged history, pending messages and
claim release for retry. Existing callable ServiceViews error-path tests retain
construction/append/ack failure coverage.

Final gates passed: focused WorkflowInbox race test; full go test ./... and
full go test -race ./... (-count=1 -timeout=180s); go vet ./...; all 130 Source
exports current; all 19 scan guards anchored. Full Python regression passed:
2,155 passed, 28 skipped, 24 subtests, three dependency warnings in 86.95s.
git diff --check and README outline passed. Python invariant/guard sweeps were
not rerun because no Python runtime package or guarded Source behavior changed.
Archify passed 9/9 showcase checks with zero errors/warnings. Specification SHA256:
4e0acf764ffc207173b1f43e4254c4755c0f9e1b06d7d10d9d4cdc28c7d01226;
HTML SHA256 2c7cc8de45778d01cc0b13576130899c539a5995a7a61f6a00a66abb57b16a13. Coverage was not refreshed; 90.23% belongs to
99b87f3. The README architecture baseline, Mermaid/boundary explanation and generated
interactive map describe the installed path. Visual review remains skipped after
prior access denial. No dependencies, ambiguous Go domain types, Python runtime
modules or guarded Source behavior changed.

Dedicated workflow HTTP routes, launcher activation, six reserved event projections
and open-payload archival fidelity remain pending, along with MCP, native session
SQLite, remaining profiles and full G7. The full Python-to-Go port remains open.


## 2026-10-08 typed workflow session events and summaries

Baseline 99b87f3. W6C now routes the twelve event kinds actually emitted by
WorkflowService through the owned parent's common session event bus. A closed
WorkflowEvent variant is retained in SessionEvent with detached pointers and a
concrete payload decoder; no generic domain map or RawMessage was added. Session
JSON adds the source sequence alias without duplicate type members. Known archival
rows require consistent kind/run aliases, version 1 and required event identities;
archival provenance remains untrusted and cannot recreate human capabilities.
Node-claimed rows require node identity but may omit an attempt, matching Source.

Masking covers workflow names, identities and every emitted string payload before
state, trajectory, backlog, subscribers and operator sinks. The usual non-ephemeral
capture/publication path applies, including bounded queues, durable-first ordering
for injected stores, active trajectory capture and contained sink faults. The native
SQLite implementation is still pending; the differential evidence uses actual Python
SQLite rows versus the existing injected Go state-store contract.

Launch admission and terminal observation have separate owned manager lookups:
stopping rejects launches while service shutdown can still publish terminal events
to surviving sessions. Deleted parents remain absent. Injected services preserve
operator-owned event resolution. Session info exposes ordered typed RunSummary
slices, empty when disabled or freshly forked. Existing owner-scoped HTTP session
projections retain those summaries, health reports actual activation, and SSE resumes
workflow records through the existing cursor/envelope path. Dedicated workflow HTTP
routes and launcher selection are still separate work.

Snapshot 129 compares twelve actual Python WorkflowEvent/session captures across
live subscription, backlog, sink and SQLite storage, including recursive secret
masking and metadata aliases. Additional actual fresh Python worker execution proves
completed/fork/disabled summary projections. A blocked Source worker checks shutdown
terminal publication. Older manager/fork fixtures now retain Source's existing empty
workflows field instead of excluding it. Native race tests compare the four surfaces,
archive round trips and untrusted provenance, detached accessors, real fresh manager
worker telemetry and summaries, stopping/deleted resolution, owner-scoped HTTP and
actual resumable SSE. Unsupported/inconsistent archive identity/version is refused.

Final gates passed: focused agent/HTTP race profiles; full go test ./... and
full go test -race ./... (both -count=1 -timeout=180s); go vet ./...; all 129
source exports current; all 19 scan guards anchored; full Python regression:
2,155 passed, 28 skipped, 24 subtests, three dependency warnings in 74.76s.
The first Go run exposed omitted empty workflows fields in two old Source fixture
projections; those fields were restored from actual info() before rerunning all gates.
One concurrent export check flagged the unrelated child-background snapshot;
the sequential rerun passed without changing that fixture. Package invariant/guard
sweeps were not rerun because no Python runtime package or guarded source changed.
git diff --check and README outline passed. Architecture specification SHA256:
3d3d6065dca12fded4726b391d2f32448c700dd5ecadbfebd449d5dd3cb9f929;
HTML SHA256 4614865699e91fc693797d14d0416fdc87069fbb92dbc4ab62b09d22ff4550fd. No Python runtime module or
load-bearing guard changed; no dependencies were added. README canonical Mermaid
and its boundary explanation were updated; the JSON specification regenerates the
HTML with all nine Archify showcase checks passing and zero errors/warnings. Visual
inspection remains skipped after prior access denial. Coverage was not refreshed;
90.23% (20,568/22,796) belongs to 99b87f3, not this implementation.

Next W6C slice installs later-turn result injection with append-before-ack ownership.
The six reserved Source event kinds (approval-required/rejected/phase-started/
checkpointed/paused/resumed) are not emitted by the current Source service and remain
outside this decoder. They still need a defined native archival projection before
full event-model parity. MCP, native session SQLite, remaining profiles and full G7
remain open. The full Python-to-Go port is unfinished; workflow activation stays off
by default and workflow state remains process-local.


## 2026-10-08 manager-owned workflow composition

Baseline 09bde07. W6B adds optional ManagerServices workflow construction and owned
lifetime. WorkflowTools captures typed caps and creates one service; injecting a
WorkflowService enables the tools and selects its action journal and attempt pool.
An explicitly conflicting pool is refused before workspace allocation. Default fresh
readonly workers inherit provider/model budgets, skills/secrets/role policy,
recovery/cache/stuck seams and shared model/tool permits. New, forked and restored
sessions bind the same service; autonomous teammates omit all three workflow tools.
Workflows() is an operator API and does not establish owner admission.

The default parent resolver accepts only current active manager sessions. Native
identity guards reject deleted or stopping manager handles before journal replay.
Managed launches carry a private parent handle, checked under service admission;
CancelSession snapshots runs through the same barrier and joins terminal publication
as well as active engine tasks. An already-admitted launch cannot escape deletion
cleanup, including when an injected resolver still recognizes the old parent.
Direct operator requests and custom cooperative callbacks retain operator policy.

Delete owns asynchronous cancellation and existing session cleanup before scratch
reclamation. Enabled idle deletion now also uses the owned background cleanup path;
this native timing extension is separate from the source final-state comparison.
Preserved and bound workspaces remain. Cancellation failure or remaining work pins
paths against other shared-session cleanup; shutdown retries eligible drained pins.
Stop closes the service and joins cleanup even after its initiating caller cancels.
No durable worker recovery, cross-process fencing or exactly-once effects are implied.

Snapshot 128 compares 12 actual Python SessionManager/service/engine/store/journal
profiles: disabled, complete, custom caps, injected/same/conflicting pool, running and
idle deletion, preserved deletion, shutdown, fork and teammate catalogue. Only worker
execution is replaced. Native tests additionally exercise real fresh workers, captured
caps, shared permits, deleted replay refusal, blocked admission versus deletion,
terminal observer publication, caller timeout and injected cleanup-failure retention.
The failure test is a native fault injection, not a source cancellation-failure profile.

Fresh full shared-package Go statement coverage is 90.23% (20,568/22,796),
using go test ./... -count=1 -timeout=180s -coverpkg=./... and merging identical
source blocks across package test binaries by maximum hit count. This measures
executed statements, not full Python feature parity. The previous 90.20% profile
belongs to def5cec. Coverage tests passed; the final gates follow below. The initial full Go run
found that capacity-zero rejection broke initialized wait-only pools in existing
cancellation tests. Validation now rejects only an uninitialized nil-channel pool;
the focused engine/manager race suite passes, retaining source permit-wait behavior.
The README architecture baseline and canonical Mermaid now describe manager-owned
workflows; the interactive specification and generated HTML pass all 9 showcase
checks with zero errors/warnings. Visual inspection remains skipped after prior
access denial. No dependencies or ambiguous Go domain types were added.

Final gates: full go test ./..., go test -race ./... and go vet ./... passed;
all 128 source export files passed --check and all 19 scan guards remain anchored.
Full Python regression passed on the second run: 2,155 passed, 28 skipped,
24 subtests, three dependency deprecation warnings in 85.34s. The first run had
2,154 passes and one existing 40-turn timing assertion at 0.535s versus 0.5s;
the entire double-cost test file then passed (12 tests) before the full rerun.
No Python runtime package or guarded behavior changed, so invariant/guard sweeps
were not rerun. git diff --check and README outline passed. Architecture specification
SHA256 82ba01ebf1d97e26e16b24b46f6e7164282e81792a46cbeb0bb647a3ea40ce8c;
HTML SHA256 b523a9908999de9276d2274fa50aba55b266784665afd03a97610d8458f6bb85.

W6C remains: live parent event/SSE/archive integration, summaries and later-turn
notification injection. Workflow HTTP/launcher activation, MCP, native session
SQLite, remaining differential profiles and full G7 are still open. The full
Python-to-Go port is unfinished; defaults remain off and storage is process-local.


## 2026-10-08 bound workflow model tools

Baseline def5cec. W6A installs concrete Workflow/WorkflowStatus/WorkflowCancel
handlers through explicit RuntimeConfig selection. Supplying WorkflowService enables
these tools and shares its action journal; an unrelated runtime journal is superseded.
WorkflowTools without a service preserves the source unavailable-service boundary.
Defaults remain off; catalogue filtering can reduce but cannot enable the surface.
NewManagedSession binds the actual parent handle and captures runCount for launches;
bare Session launch is refused. Original typed definition/args remain the action input.

Launch/manage require private trusted human per-message capabilities. Immutable
owner/session/workspace and capability guards precede journal replay; handler checks
repeat admission before effects. This native replay fence is an explicit extension
protecting private cached results, not an inference from direct source handler tests.
All three tools are ordering barriers: launch exec, cancel write, status readonly read.
Live permission mode still applies. Status/cancel use the bound session filter;
cancel joins workers with source reason "cancelled by trusted parent". Replies retain
source sorted, spaced UTF-8 JSON, including Unicode values and integer/float identity.

Snapshot 127 compares 21 actual Python tool handler profiles and installed traits.
The source uses real WorkflowService/engine/store/journal and replaces only worker
execution. Profiles cover normal launch/status/cancel, isolated launch/manage grants,
untrusted or absent context, missing action/call/managed parent/service and foreign
status/cancel. Launch input hashes retain forged metadata separately from normalized
definitions. Native tests cover a real managed model turn launching a blocked worker,
tool cancellation/join, original journal effects, injected journal precedence,
same-turn notification suppression, replay equality and foreign/capability fencing,
readonly cancellation refusal, default absence, bare launch and catalogue reduction.
The new Python fixture helper is separate from the general exporter for maintenance.

Validation: focused native workflow race tests, full Go tests/race/vet passed;
all 127 source exports are current and all 19 scan guards remain anchored.
Full Python regression passed: 2,155 passed, 28 skipped, 24 subtests in 73.68s;
three dependency deprecation warnings. It ran after the Go gates to avoid timing
assertion interference.
No Python package module or guarded runtime changed, so package invariant/guard
sweeps were not rerun. No dependencies or ambiguous Go domain types were added.
git diff --check and README outline passed. The canonical Resources node now includes
optional workflow bindings; its interactive specification was regenerated with Archify
9/9 showcase checks, zero errors/warnings. Specification SHA256:
32e91adc254b291c29077f954740787c178caef40bb441df15f717d52b4cb052;
HTML SHA256 50d514b2b38e03b9a928c5970e382ca166d4083223fdaf7a5cb8c6d876c3770b.
Visual inspection remains skipped after prior access denial. Coverage was not refreshed
this slice; 90.20% (20,359/22,572) belongs to def5cec and is not the current profile.

Next W6B binds optional manager construction and owned lifecycle to the service,
including parent resolution, deletion/shutdown joins and readonly teammate isolation.
W6C adds automatic later-turn notification injection and typed events/summary/HTTP
projections. MCP, native session SQLite, remaining profiles and full G7 remain open;
the full Python-to-Go port is unfinished. Runtime operators still own the service lifetime.


## 2026-10-08 owned workflow service

Baseline 9454a06. W5B3 adds agent.WorkflowService as an explicit operator library.
Trusted human origin, per-message workflow.launch, parent owner binding, dynamic
admission, authority policy and args validation precede journal/store publication.
Original ActionInput or source normalized fallback binds one run. Background tasks
own fresh readonly workers, shared permits, min(definition, process) budgets,
shielded wait, cancel, deleted-parent cleanup and draining Close. Saved provenance
remains inert; queued replay requires matching original trusted live context.
Task completion reclaims live bindings. Service pruning pins active tasks through
terminal event/outbox publication and removes per-run event/launch bookkeeping.

Snapshot 126 uses the actual Python service/engine/store/journal in 23 profiles,
replacing only worker execution. Full normalized launch/run/node/attempt/artifact/
outbox/event/context/journal effects are compared. Profiles cover raw action input,
serial/verify, worker/factory failures, missing/invalid results, timeout, cancellation,
shutdown, wait shielding, foreign cancel, replay/conflict, observer failures and
ordered admission refusals. Closed events retain source header/payload fields;
progress excludes inputs/outputs. Observer errors are bounded at 100 entries of
500 code points and do not alter results. Native tests cover concurrent replay and
cancel joins, deleted parents, cleanup after caller timeout, trusted replay isolation,
actual fresh workers and terminal publication retention. Native live-context replay,
positive representable duration validation and goroutine graph pins are explicit
extensions. Callback cooperation remains required; no durable task recovery is added.

Validation: full Go tests, full race tests and vet passed; focused retention/race
checks also passed. All 126 source exports are current and all 19 scan guards
anchored. Full Python regression passed on an isolated rerun: 2,155 passed,
28 skipped, 24 subtests and three dependency deprecation warnings in 81.31s.
An initial concurrent run failed only the existing 0.5s session timing assertion
at 0.527s while full Go race tests were active; no Python test/runtime was changed.
The final isolated full suite passed. git diff --check and README outline passed.
Python package invariants/guard sweeps were not rerun because no package module or
guarded Python runtime changed. No dependency was added. README map reviewed;
runtime Mermaid topology remains unchanged because this library is uninstalled.
Archify regeneration passed 9/9 showcase checks with zero errors/warnings.
Specification SHA256 c7e53a0d949a3ba70d6a69844c4f49df650422c59a15449f6bcf85b61c640901;
HTML SHA256 750aff832e71d45bbb0efee080fb224a8e9aad208c78b8ec85dd42de2042d65c.
Visual inspection remains skipped after prior access denial.

Fresh Go shared-package statement coverage is 90.20% (20,359/22,572), merging
identical source blocks across -coverpkg=./... test binaries by maximum hit count.
Profile: /tmp/mini-loop-workflow-service-coverage.out. This measures tested
statements, not migration completion. Python coverage was not refreshed.

Next W6 installs optional manager/tools/HTTP, owned manage admission, parent
notification append, manager deletion/shutdown joins and SSE/archive projection.
MCP, native session SQLite, remaining profiles and full G7 remain open. The full
Python-to-Go port remains unfinished.



## 2026-10-08 typed workflow tool inputs and journal contracts

Baseline 640aace. W5B2 extends the closed ToolInput union with concrete WorkflowInput
and WorkflowReferenceInput for Workflow/WorkflowStatus/WorkflowCancel. Definition
and args are immutable closed JSON sums constrained to objects. Original definition
metadata participates in action identity; dynamic admission must normalize a separate
copy. WorkflowRunID is shared with workflows.RunID and the agent action record alias.
Canonical compact and spaced Python JSON recursively sorts object keys while retaining
integer/float identity and array order. Recording masking covers nested keys/values;
original payloads remain unchanged. Archived input decoding and schemas round-trip.
WorkflowToolSchemas supplies detached exact source schemas without handlers or default
catalogue activation. Definition/DAG/argument-schema validation stays service-owned.

Snapshot 125 compares eight actual canonical/action identities, three installed
source tool schemas, 20 refusals under the source advertised schema validator and
18 actual InMemoryActionJournal/DurableActionJournal(SQLite) profiles. Advertised
schema validation is fixture evidence, not a claim about Python generic dispatch.
Profiles cover original/normalized metadata and argument changes, tool-use/session/
message conflicts, caller-seeded tool-use fallback, completed-action replay, repeated
same-run binding, conflicting binding and missing actions. Full normalized record
effects match. Existing native Begin/AttachWorkflow now support these typed variants;
attachment does not change action status or establish an execution/dispatch claim.
Native StoredActionJournal is exercised against its injected test store and compared
to actual source SQLite effects; no native SQLite driver is inferred. Missing-action
errors are compared as refusals, not Python KeyError text; typed native conflicts are
required. Immutable inputs and detached journal bindings do not alias stored state.

Validation: focused workflow input/action tests, full
`go test ./... -count=1 -timeout=180s`, full
`go test -race ./... -count=1 -timeout=180s` and `go vet ./...` passed.
All 125 Python exports are current; all 19 source scan guards are anchored.
Full Python regression: 2,155 passed, 28 skipped, 24 subtests in 90.85 seconds.
Four warnings: three dependency deprecations and an asyncio subprocess transport
finalizer after loop closure. No tests failed. `git diff --check` and README outline
passed. Python package invariant/guard checks were not rerun: no package module
or guarded Python runtime changed.
No dependencies or Python runtime modules changed. Runtime Mermaid topology remains
unchanged because no workflow service or tools are installed in the manager.
Architecture delivery passed 9/9 showcase checks, zero errors/warnings. Specification
SHA256: e176bf76c6a146d28b6586c6f66347b8d539d54414dd85955b94d81ea1278095;
HTML SHA256: a8d000ba2e5e6f0ee923fffdc037a3845fe9f9d19d7a45d32429c39c763890eb.
Visual inspection remains skipped after prior access denial. Overall coverage was
not refreshed this slice; 90.43% (19,855/21,956) belongs to 8809e24 before W5B1/W5B2.
These are statement coverage figures, not migration completion. Python coverage
was not refreshed.

Next W5B3 adds the owned service: trusted live launch contexts, argument admission,
original/fallback action binding, background execute/wait/cancel, wall-time policy
and typed lifecycle/progress events. W6 installs optional manager/tools/HTTP, parent
notification append and shutdown/delete joins. MCP, native session SQLite, remaining
profiles and full G7 remain open. The full Python-to-Go port is unfinished.

## 2026-10-08 dynamic workflow definition admission

Baseline 8809e24. W5B1 adds concrete DefinitionCaps, captured DefinitionAdmission
and immutable AdmittedDefinition. Dynamic admission drops definition_hash,
definition_id, revision, parent_revision, source and source_version before decoding,
then forces dynamic source. Definition/schema/DAG validation precedes ordered
process concurrency/total-agent/round/wall-time cap checks. Successful admission
returns normalized Definition and source-compatible wfpolicy digest using the
operator caps and definition policy, including Python float identity for seconds.
The caller's immutable input remains intact for future action identity. Admission
neither grants authority nor writes a run, journal or execution task.

Snapshot 124 compares 20 actual Python WorkflowService._definition recipes and
launch's canonical policy hash expression: mapping/typed definitions, arbitrary
forged identity/provenance values, reversed tool order, retained authority policy,
invalid definition/graph before cap refusal, duplicate/subset tools, each lowered
cap and exact boundary, refusal priority and fractional seconds. Valid definitions
and policy digests match in full. Unknown top-level fields are refused by both;
Go retains its strict decoder diagnostic rather than Python constructor TypeError.
Native tests also verify captured policy cannot be widened by caller mutation,
metadata-insensitive identity, nil/non-object admission and invalid operator caps
(including nonfinite seconds). Existing ValidateDefinition already enforces the
exact read-only tool set; no redundant service-level set check is needed.

Validation: focused admission tests, full `go test ./... -count=1 -timeout=180s`,
full `go test -race ./... -count=1 -timeout=180s` and `go vet ./...` passed.
All 124 Python exports are current; all 19 source scan guards are anchored.
Full Python regression: 2,155 passed, 28 skipped, 24 subtests in 84.03 seconds.
Four warnings: three dependency deprecations and an asyncio subprocess transport
finalizer after loop closure. No tests failed. `git diff --check` and README outline
passed. Python package invariant/guard checks were not rerun because no package
module or guarded Python runtime changed.
No dependencies or Python runtime modules changed. Canonical runtime Mermaid
stays unchanged: this callable admission library is not an installed service.
Architecture regeneration passed 9/9 showcase checks, zero errors/warnings;
visual review remains skipped after the prior access denial. Specification SHA256:
594b9d0cdeb154e773f0f44c8b51c5d9b03889a8beeec02dcbc69316fcc381f3;
HTML SHA256: 29d809d101ccde756600367b3614538fae5765f8923fa5d83fe700e23d868960.
Overall coverage was not refreshed after this implementation; the latest 90.43%
(19,855/21,956) was measured at 8809e24 before this slice. It is test statement
coverage, not migration completion. Python coverage was not refreshed.

Next W5B2 adds concrete Workflow tool/action inputs and journal binding; W5B3 owns
trusted live launch contexts, background execution/wait/cancel, wall-time control
and lifecycle/progress events. W6 installs optional manager/tools/HTTP and admitted
parent notification delivery. MCP, native session SQLite, remaining profiles and
full G7 remain open. The full Python-to-Go port is unfinished.

## 2026-10-08 workflow state views and notification delivery

Baseline 48276bc. W5A ports source status/summaries, launch-turn bookkeeping,
prepare/ack/release and workflow_injector's append-before-ack flow into typed
ServiceViews, RunStatusView, NodeStatusView, RunSummary, WorkflowNotification,
NotificationBatch and NotificationAppender. It is the service's projection/delivery
layer, not the complete owned launch/task service. Status hides foreign-session
runs as NotFound before definition/artifact reads and returns detached fields; one
native store lock keeps run/node/artifact projections coherent during settlement.
Summaries preserve store run order. Session filtering does not authenticate an owner.

RecordLaunchTurn retains the first turn and requires an existing run. PruneTerminalRuns
removes the returned run IDs from that bookkeeping under its own mutex. Notifications
are eligible only after the launch turn (unset means zero). Every claim is capped at
50; overflow remains pending. Results are canonical compact Unicode JSON, with strict
UTF-8 byte length >8,000 selecting null plus the first 2,000 code points and the source
WorkflowStatus retrieval instruction. Run diagnostic strings win when nonempty; else
arbitrary outbox payload diagnostics survive in immutable closed JSON variants. A
leased NotificationBatch owns private recipient/turn/message IDs/token and detaches
its public views. The source untrusted-artifact-data wrapper is reproduced exactly.

DeliverNotifications invokes an explicit typed append adapter before acknowledgment.
Prepare/construction/append failure releases the lease; release refusal replaces the
original fault as in source. Ack failure leaves the already appended context intact
and returns count plus error; it cannot undo the external effect. No claim is called
exactly-once or durable. Caller owner admission and the live parent append adapter
remain future owned-service/manager responsibilities. Idle empty claims still mint
source tokens; no append occurs for empty notifications. Context cancellation and
missing appender admission are explicit native API checks.

Snapshot 123 compares 33 actual Python WorkflowService status/summary/prepare/ack/
release recipes. Actual workflow_injector runs append success/failure against a
recording list; records/artifacts/statuses/launch turns are explicit trusted fixture
seeds, not authorized launches. It covers all 13 run statuses, final/no/missing
artifact, diagnostic fallback, foreign sessions/status, same/future/past/negative
turns, ASCII and multibyte exact/over 8,000-byte bounds, release/ack and a 51-notice
batch. UUIDs, lease tokens and time are normalized. Full status/summary/notification/
message/outbox effects and error kinds/details match. Native race tests verify two
unique batches across four concurrent deliveries, append-before-delivered observation,
first-turn replay, bookkeeping cleanup and detached/coherent status during execution.
No Python runtime modules or dependencies changed.

Validation: focused workflow race, full `go test ./... -count=1 -timeout=180s`,
full `go test -race ./... -count=1 -timeout=180s` and `go vet ./...` passed.
All 123 Python exports are current (dependency deprecations only) and all 19 source
scan guards are anchored. Full Python regression passed: 2,155 tests, 28 skipped,
24 subtests, 3 dependency deprecation warnings in 85.09 seconds. `git diff --check`
and README outline passed. Python package invariant/guard checks were not rerun:
no package module or guarded Python runtime changed. Architecture regeneration
passed 9/9 checks with zero errors/warnings. Specification SHA256:
`406d63369067f7d27997f8f35a3a5e6edd2a712ce0e53bf981262c9d39c6cf2b`;
HTML SHA256: `2f9e71d30b72cb69fe800b5ac87a181eb463942a938579d07cbb32d7fb5cbd3e`.
Canonical runtime topology stays unchanged because no owned workflow service or
parent-session injector is installed. Visual review remains skipped after the prior
access denial. Overall coverage was not refreshed; 90.56% belongs to c1016d7 and is
not a current completion measure.

Next W5B adds dynamic-definition policy caps, concrete Workflow action input/journal
binding, trusted live launch contexts, background execution/wait/cancel, wall-time
control and typed lifecycle/progress events. W6 installs optional manager/tools/HTTP
and binds notification append to admitted parent turns with shutdown/delete joins.
MCP, native session SQLite, remaining runtime profiles and full G7 remain open.
The full Python-to-Go port is unfinished; workflow persistence/restart-resume is
not established by this callable projection/delivery layer.


## 2026-10-08 isolated native workflow workers

Baseline c1016d7. W4B adds agent.NewFreshWorkflowRunner with explicit provider,
owner, workspace, model budgets, optional catalogue/Explore role policy, shared
model/tool limiters, recovery/cache/stuck/skill/masking seams and a typed live
WorkflowContextResolver. Every call constructs a fresh readonly Session, default
read_file/glob catalogue, node-specific return_artifact capture and InMemoryCompactor.
No parent transcript, injectors, permission hooks, stop hooks, approvals, persistence
or manager are accepted. Operator catalogues keep their own handler bindings; the
readonly permission gate still denies selected write/exec/external/unclassified tools.
Repository reads use the existing confined native file handlers. The resolver must
return a valid private RunContext; inert saved snapshots cannot enter this seam.
Derived peer provenance names the attempt agent, retains parent message/delegation
and drops explicit-human capabilities. Cancellation remains cooperative.

The protocol now has a concrete ReturnArtifactInput carrying immutable closed Value,
strict exactly-value JSON admission (null is a real value), canonical/spaced identities
and recursive recording masking. Synthetic return_artifact ToolSchema alone retains
an exact immutable schema projection, including numeric enum/const. Ordinary tool
schema parsing remains unchanged; typed request/catalogue clone and archival reads
preserve the synthetic projection. Workflow schema-language validation belongs to
the controller/runner, not the wire decoder. No open service/domain payload is added.
Capture validates before publishing the first result; repair remains possible and
later submissions retain the first value. The selected synthetic handler is replaced
by owned capture if an operator catalogue supplies that same name. Round count is
min(runner default 8 or explicit positive limit, nonzero node limit). Normal exit
without capture has the source RuntimeError; provider/cancellation faults propagate.
LastWorker is a detached synchronized diagnostic, not execution authority/history.

Snapshot 122 runs real Python FreshAgentRunner/Agent with scripted fake model calls,
using an observational Agent subclass to retain the actual constructed worker. Eight
profiles compare submitted values, calls, tools, fixed worker system, full synthetic
schema, round cap and named peer context: object, repair, duplicate, numeric enum/const,
null, missing result, exhaustion and task-name fallback. Source UUID message identity
is normalized by comparing parent linkage. Source resolver wrong-type inputs are
excluded by the Go callback signature; invalid zero contexts fail before model access.
Native tests additionally cover readonly denial under an enlarged role catalogue,
masked repository reads reaching artifacts, no context-management file writes,
concurrent fresh histories, detached diagnostics and a real two-node native engine
executing fresh workers through completion. No external model endpoint was called.
The native runner validates the schema before session/model creation and uses bounded
existing native file/JSON contracts; arbitrary Python live-object inputs are excluded.

Validation: full `go test ./... -count=1 -timeout=180s`, full
`go test -race ./... -count=1 -timeout=180s` and `go vet ./...` passed.
Final focused worker/protocol race also passed, including operator same-name handler
replacement. All 122 actual Python exports are current; exporter emitted dependency
deprecations and a subprocess-transport finalizer after event-loop closure, with no
contract mismatch. All 19 source scan guards are anchored. Full Python regression:
2,155 passed, 28 skipped, 24 subtests passed, 3 dependency deprecation warnings in
94.17 seconds. `git diff --check` and README outline passed. Python package invariant/
guard checks were not rerun: no package module or guarded Python runtime changed.
Architecture regeneration passed 9/9 checks, zero errors/warnings. Specification
SHA256: `7f67379ebadb2846ac25cd3bfe545ec78fee76597d7737c696caf49b3eb92ad7`;
HTML SHA256: `04482bbbf5c53ad771ecc91cb50d114d47dec5bc58408a1366e6c4a780ae3372`.
Runtime topology remains unchanged: this operator runner is callable, with no manager
workflow activation. Visual review remains skipped after the earlier access denial.
No dependencies or Python runtime modules changed. Overall statement coverage was
not refreshed; the previous 90.56% belongs to c1016d7 and is not a current completion
claim.

Next W5 adds owner-bound launch/status/wait/cancel, trusted live origin admission,
service wall-time control, launch-turn bookkeeping and parent notification append/
receipt/retention flow. W6 installs optional workflow tools/HTTP and manager lifecycle.
MCP, native session SQLite, remaining runtime profiles and full G7 remain open.
Full Python-to-Go migration is unfinished; workflow persistence/restart-resume is
not delivered by this in-process runner.


## 2026-10-08 typed workflow batch engine

Baseline 826f7d1. W4A adds WorkflowRunner, detached AttemptExecution, finite
RunnerError, WorkflowEngine and shared AttemptPool/AttemptPermit. Execution validates
before terminal return, serializes each run, selects dependency-ready nodes in
source definition order and joins each batch before selecting the next. Engine and
definition concurrency plus remaining attempt budget bound claims. Verifier parent
identity comes from the latest first-dependency attempt. Inputs retain scalar versus
array artifact folding and the dependency-named-args overwrite. Permits cover runner
calls and release before structured validation/settlement. Default capacity is four;
zero blocks cooperatively, negative capacity is refused. Release is idempotent.

Snapshot 121 executes actual Python engine/store methods across 42 profiles and
five constructor limits. Full run/node/attempt/artifact/outbox projections, ordered
captured runner inputs and repeat outcomes are compared. Coverage includes every
run status, ordinary/verifier submission and runner faults, all verification folds,
diamond/serial scheduling, parallel failure, budget exhaustion, deadlock, absent
return artifacts, args collision and running/permit-wait cancellation. UUID/time
are normalized; concurrent runner callback order is not asserted. Native race tests
cover shared pools, same-run Execute serialization, cooperative cancellation,
parent deadline settlement and cancelled lock waits with empty idle bookkeeping.

Native deviations are explicit: errors outside finite RunnerError use
NativeRunnerError; idle execution locks are reclaimed instead of accumulating;
claim publication and cancellation tracking share a mutex. Parent context expiration
settles attempts and returns its error but does not request run cancellation.
Python wall-time budgets are service-owned, so no engine wall-time timer is added.
Runner callbacks must honor context; arbitrary panics/hard interruption are outside
this contract. Source verifier nil submissions preserve their AttributeError before
structured validation; verifier faults produce schema-invalid unverified artifacts.
Decoded provenance remains inert. No dependencies or Python runtime modules changed.

Validation: full Go tests with per-package and shared-package coverage, full
`go test -race ./... -count=1 -timeout=180s`, `go vet ./...`, final focused
workflow race and vet passed. All 121 Python exports are current and all 19 scan
guards are anchored. Full Python regression passed: 2,155 tests, 28 skips and 24 subtests in
128.22 seconds. Four warnings: three dependency deprecations plus an asyncio
subprocess-transport finalizer after loop closure; no test failure. `git diff --check`
and README outline passed. No Python package module or guarded runtime changed,
so Python package invariant/guard checks were not rerun. Architecture delivery
passed 9/9 checks with zero errors/warnings. Specification SHA256:
`5c8bf150d1188f451dba12496bc063f5909d18020153d39774ef5b03a648c8af`;
HTML SHA256: `c24a1cd1d59cb168ac18b57951b89d379eac4076a5ecf3dfab73f4b990e3cb7b`.
Topology remains unchanged because this is an uninstalled library. Visual inspection
remains skipped after the earlier access denial; no visual approval is claimed.

Fresh shared-package Go statement coverage uses `-coverpkg=./...` with duplicate
source blocks merged by maximum execution count: overall 19,581/21,622 (90.56%),
workflows 1,326/1,433 (92.53%) and engine 224/269 (83.27%). Profile:
`/tmp/mini-loop-engine-shared-coverage.out`. Input object member order is compared
byte-for-byte with source in addition to canonical full-state comparisons. These
figures measure exercised statements, not full migration completion. Python
coverage was not refreshed.

Next W4B adapts the isolated readonly FreshAgentRunner and trusted live context
resolver. W5 adds owned launch/status/wait/cancel, service wall-time control, parent
notification append/receipts and retention bookkeeping. W6 installs optional
workflow tools/HTTP and manager composition. MCP, native session SQLite, remaining
runtime profiles and full G7 remain open. Full Python-to-Go migration is unfinished;
no durable workflow persistence or restart-resume is inferred.


## 2026-10-08 terminal-and-drained workflow graph retention

Snapshot 120 executes prune_terminal_runs over 65 actual full graph profiles,
default-500 eviction and stable-ID timestamp ties. Graphs are built through public
registration/create/transition/claim/start/commit/finalize/outbox methods; status,
notice removal, node state, timestamps and tie IDs are explicit fixture seeds.
All finite run statuses, absent/pending/claimed/delivered/expired unread notices,
negative/zero/positive bounds, repeated pruning and retained/evicted launch replay
are compared. Full run/node/attempt/artifact/outbox/key/definition projections match.
Native also removes its attempt/outbox insertion indexes, preserving survivor order.
Definitions/hashes and other runs' parent metadata are retained, matching owner-based
source cascading. Native minimum-int handling avoids overflow while preserving
source negative-limit slicing. Race tests cover acknowledgment versus cleanup and
assert every owned map/index remains complete. Returned IDs expose service cleanup;
automatic service invocation, trusted live origins, engine/service/tools/HTTP,
parent append and manager activation remain open. This is process-local retention,
not durable storage or full migration completion.


## 2026-10-08 workflow outbox enqueue and lease settlement

Snapshot 119 executes source enqueue_outbox, claim_outbox, acknowledge_outbox and
release_outbox across 106 profiles with repeated outcomes and full outbox state.
Explicit fixture state seeds cover active/expired/future/incomplete/empty-token
leases, delivered records, session/run filters, omitted/empty run sets, bounded and
nonpositive limits, nonpositive/NaN/infinite caller lease windows, deduplication,
duplicate IDs and partial effects on missing/foreign/mismatched later IDs. Generated
IDs and time values are normalized; age checks run against the real source clock
with five-/sixty-second margins. Selection is insertion-ordered before response sort.
Acknowledgment requires nonempty token but delivered no-ops bypass lease comparison;
release has no nonempty-token admission. Both hide foreign-session messages as 404.
Native tests cover 16 claim and 16 enqueue contenders, expiry re-lease/stale-token
fencing, release/retry/ack, detached timestamps and finalization insertion indexes.
New native enqueue payloads must be immutable closed objects; source broad dict
conversion is outside this typed boundary. No external parent append is installed.
Whole terminal-and-drained graph retention, trusted origins, workflow engine/service,
tools/HTTP and manager activation remain open; no durable delivery is claimed.


## 2026-10-08 workflow cancellation and completion outbox creation

Snapshot 118 runs actual cancel_claimed_attempts, request_cancel, finish_cancellation,
fail_run and finalize_run: 131 initial-state/refusal profiles, repeated outcomes and
full run/node/attempt/outbox projections. Initial status seeds are explicitly trusted
fixtures, not restored live authority. Cancellation includes claim insertion order,
mixed started/unstarted tasks, sticky reasons including empty text, version-first
refusal and terminal no-ops. Later bad-node cancellation keeps earlier settled tasks
while leaving the run projection unchanged, matching source. Finalization checks
artifact existence before successful node states; foreign artifacts/schema-invalid
artifacts are accepted by this store layer and caller validation remains necessary.
Native overflow/entropy admission precedes publication. Race tests elect one request
and one finalization CAS winner; the finalization creates one completion message.
Outbox reads preserve source filters, creation-time/ID ordering and detached dates.
Delivery leasing/ack/release, terminal-and-drained retention, live workflow service,
engine, origin authority, tools/HTTP and manager activation remain open.


## 2026-10-08 workflow attempt startup/settlement and artifacts

Snapshot 117 executes source start_attempt and commit_attempt: 14 startup
status/version profiles, 32 terminal-pair/artifact/refusal/verification/terminal-run
profiles and their repeated commits. Native attempt CAS and exact source refusal
kinds/details match; every run/node/attempt/artifact projection is compared.
Terminal enums are independent; no artifact schema_valid/verification correlation
or run-status gate is invented. Provenance mismatch precedes effects. Late invalid
verification conversion preserves source partial artifact/reference and terminal
attempt/version/timestamp changes while node/run settlement remains pending.
Nil native verification selects the source omitted default; explicit empty text
remains invalid. Matrix initial states are an explicit trusted fixture, not restore
support. Native race tests add 16 startup and 16 commit contenders with one winner,
single artifact publication and overflow refusal before shared mutations. Ordinary
artifacts are immutable domain models. Cancellation/finalization/outbox/pruning and
live workflow execution/authority/installation remain open.


## 2026-10-08 process-local workflow store admission and claims

Snapshot 116 executes actual InMemoryWorkflowStore methods: 53 sequential
registration/admission/read/CAS/claim operations, all 169 run-status pairs and
three canonical hash refusals. Only generated IDs/timestamps are normalized;
transition matrix initial statuses/versions are an explicit trusted fixture.
Native definition validation/deduplication, launch identity hashes, refusal kinds
and details, final effects, replay of latest state, clone isolation, source sorting,
same-state CAS ordering, empty-claim version increments and atomic batch validation
match these profiles. Definitions preserve first registered semantic revision;
returned semantic aliases are not registered under the supplied revision.
Native tests add 24 concurrent launch/claim contenders, pointer isolation, resume
start retention, UUIDv4 prefix shape, sort ties and int64 overflow refusal. Payload
hashing uses closed values directly so nonfinite/surrogate data cannot be silently
repaired by encoding/json. Object args are a native typed boundary; source arbitrary
live objects are not admitted. The store does not check argument schemas or grant
launch authority. Dependencies/concurrency remain engine-owned. This is an
uninstalled process-local core; settlement/artifacts/outbox/pruning, trusted origin,
engine, service/tools/HTTP and native workflow durability are not implemented.


## 2026-10-08 workflow runtime record projections

Snapshot 115 executes actual Python WorkflowRun, NodeState, AttemptClaim,
NodeAttempt and OutboxMessage constructors: 46 projections and eight invalid
state cases. Native decoders preserve defaults, all finite state spellings,
optional fields, array order, supplied zero timestamps, empty identifiers,
negative counters and extensible outbox kind text. Required args/payload are
closed immutable objects; snapshot provenance is inert, including historical
unknown authority. Native tests verify every mutable field is detached by Clone.
The source frozen claim/outbox values are exposed as detached mutable DTOs, not
shared live objects. Native boundaries additionally reject unknown fields,
malformed declared JSON types, null scalar/default fields and invalid ID-array
members. Counters use machine integers/int64 and timestamps use float64; Python
arbitrary live objects/integers are not asserted. No store/engine/launch binding
or external-delivery evidence is added. Trusted origin and W3-W6 remain open.


## 2026-10-08 shared inert provenance preparation

Native runmeta now owns the existing snapshot wire record and named provenance
types. agent exports compatible aliases; its private live RunContext, trusted
factories, delegation and capability checks are unchanged. Clone detaches pointer
and slice fields and preserves archival null/empty distinctions. Source RunContext
as_dict and the existing context/delegation evidence remain the contract; native
tests cover decoded wire retention, isolation and unchanged live authority. No
workflow launch policy or storage is installed by this extraction.


## 2026-10-08 workflow DAG/schema and structured artifact validation

Snapshot 114 executes actual source definition/schema/value validators and artifact
submission functions: 36/18/21/4/9 recipes for definitions, schemas, values,
submissions and verification input. Go matches exact error classes/details, first
refusal ordering, immutable validation and successful bound artifact projections.
Definitions preserve property insertion order via raw JSON recipe strings. The
validator admits only AGENT/VERIFY/REDUCE and exactly read_file/glob under the
workflow-readonly profile; launch authority is separately service-owned. Python
bool/numeric equality and decoded-JSON NaN membership/container behavior are
retained. Only visited float values are checked for finiteness during validation;
artifact hashing independently rejects nonfinite data anywhere. Arbitrary Python
live-object identity is not modeled. Runtime records/storage/engine remain open.

## 2026-10-08 workflow model foundation

Snapshot 113 compares the actual Python workflow model constructors: five definitions,
eight scalar/nested hash profiles, four hash failures, two artifacts and every finite
status predicate. Native workflows ports named states, immutable definition identity
and artifact records. UTF-8 canonical digests preserve 900 versus 900.0 and reject
nonfinite/lone-surrogate data. Saved hashes are recomputed; revision/parent metadata
is outside semantic identity. Native typed admission rejects unknown fields, invalid
variants and non-object schemas. This is an uninstalled library; run/attempt/outbox
records, DAG/schema validation, storage and scheduling remain open.

## 2026-10-08 teammate restart and managed root labels

Snapshot 112 now includes an actual Python SQLite close/reopen after real teammate
spawn and initial model execution. The restored handle is an ordinary lead in its
own team: owner, workspace and transcript survive, while role, roster and runner
do not. Old teammate mail remains in the old group and the new inbox is empty.
Persisted running status is retained without active work. Native injected-store
reconstruction matches these effects; it does not prove native SQLite durability.
The differential test found and corrected managed root labels from session ID to
source main, including create/fork/restore composition. Teammate names still override
the label. Complete team identity recovery would extend source behavior.

## 2026-10-08 actual shared-team diagnostic boundary

The actual Python manager binds its shared mailbox at state[bus]; self-audit
looks for state[teams]. Two additional real manager recipes record a bad-key
mailbox problem and retain a pending message, then capture fleet and owner reports.
Go matches both exact reports and leaves mail unread. Shared-bus aggregation is
a potential source enhancement, not an unimplemented parity requirement. Existing
explicit per-session Teams observations remain available to typed library callers.


Baseline: Python `main` at `ad71e05`, checked on 2026-10-02. This is an
implementation inventory, not evidence that any Go behavior exists. Update
each row with a fixture or test result as the port proceeds.
Python module names in the table are relative to `python/mini_loop/`; test
names are relative to `python/tests/`.

The versioned contract snapshot is under [`go/testdata/`](go/testdata/). Run
`.venv/bin/python python/tools/export_go_contracts.py --check` to detect drift;
the generator captures `default_registry()`, `create_app().openapi()`, the
SQLite v7 schema, default-tool risk/capability metadata, Python's stop-reason
constants, serialized fake replies, and actual file-tool outputs and file
digests, glob searches and filename matching, todo transitions, textual question
outputs, deployment skill catalogue contracts, request/context contracts, role
selection, run provenance, child loops, action identities, transitions and
actual replay paths, grant candidates and parked/reviewer approval outcomes from
the current implementation (112 generated snapshots), plus registry masking,
environment selection and typed recording projections, plus real foreground commands
and command-result rendering recipes. The loop snapshot adds cache wire/token
projections, stuck signals/hashes and actual nudge/halt paths. The lifecycle
snapshot runs nine actual managed turns, event-bus bounds, activity titles and
tool labels. The scheduling snapshot exercises six execution modes, prompt nil/empty
rewrites, adverse batch completion with a barrier, ordered stuck steps and four
Todo-nag turns. The manager snapshot compares actual creation/defaults, ten
workspace binding outcomes, shared services, deletion and stopping. The HTTP
snapshot adds actual authenticated/anonymous responses,
finite SSE framing and the admitted authority stamp. The controls snapshot adds
six actual managed scenarios and six HTTP mode/steer responses, including
Unicode/count bounds and a real readonly-refused file effect. The fork snapshot
adds completed/empty history copies, fresh child state, lineage/source events
and five HTTP outcomes. The provider snapshot captures 33 actual pinned SDK
HTTP scenarios, requests, consumed replies, error classes and retry waits. The streaming
snapshot adds nine actual SDK/StreamingTransport SSE calls, replies, raw deltas,
masked coalesced progress, retry headers, partial state and body closure. The recovery snapshot captures 24 actual Python
policy trajectories: every request, event, wait, final response and mirrored history.
The progress snapshot adds nine actual configurable Python transport cases and ten
stateful direct/stream fake calls, including thinking signatures, usage and identities.
The configuration snapshot adds 64 actual Settings outcomes covering all 49 fields
and the exact default skill asset/descriptions/load contract. The private spill snapshot
adds fourteen store cases, eight actual string-Bash commands, three projection
recipes, collision/symlink refusal, a managed structured default tool round and
three best-effort manager construction cases. The thirtieth snapshot executes per-run JSONL store/privacy/restart cases, managed
recording faults, owned HTTP reads/exports and binary-float duration rounding.

Unicode casing tables are generated separately by
`python/tools/export_go_unicode.py --check`.

| Slice | Python source of truth | Required Go contract | Go evidence |
|---|---|---|---|
| Session lifecycle, ownership, workspace binding and concurrent isolation | `mini_loop/manager.py`, `session.py`, `auth.py`; `tests/test_concurrent_turns.py`, `test_auth.py`, `test_workspace_binding.py` | Typed owner/session IDs, serialized turns per session, concurrent separate sessions, identical refusal status | Library sessions serialize turns and bind resolved workspaces. Caller-stamped typed RunContext and peer derivation are implemented, with five Python snapshots and detached data. Shared subagent providers retain session isolation under race testing. NewManagedSession adds process-local admission/status/run counts, active-turn cancellation, closure rechecks and nonblocking Info activity. Tests cover queue cancellation, approval waits, handler panic repair and reentry after faults. NewSessionManager owns ordered handles, shared eight-slot model/tool pools, default in-memory approvals/actions, explicit owner lookup and bounded remembered owners. The twentieth Python snapshot matches creation/defaults, ten binding status categories, deletion and stop. Go tests cover simultaneous creation/stop, closed queued admission, retiring shared directories, factory faults and symlink-only cleanup. Bound directories are retained; scratch reclaim waits for active holders. Go library requires explicit owners and returns a typed missing/foreign error; Python raw manager defaults omitted owners to anonymous and delete returns false. HTTP authentication and owner-scoped routes are now implemented. Bounded library steering, live modes and owner-scoped HTTP wakeup are now implemented. Typed environment settings, standalone HTTP ownership and per-run trajectory recording are implemented; session restore remains pending. |
| Model/tool loop and transcript protocol | `agent.py`, `blocks.py`, `fake_llm.py`; `tests/test_transcript_contract.py`, `test_tool_batch_invariants.py`, `test_provider_fidelity.py`, `test_stop_reasons.py` | Text, thinking, tool use and result variants; ordered batches and strict transcript validation | `go/protocol` decodes Python fake replies into typed content, caller, usage and stop reasons; transcript pairing is strict. `go/agent` tests pause, refusal, unknown-stop behavior and cancelled-batch repair in memory. Todo, stop, stuck, error, approval and subagent events share a typed, sequenced 200-event backlog with label/depth/run provenance. Round exhaustion and stuck halts report a stop marker before partial output. Sixteen Python detector cases, six input/output hash cases and five actual loops match Go, including nudge reset, denied calls, monologue continuation, zero nudge budget and null opt-out. Tests cover the 20-step bound, per-user reset, final rewritten inputs, paired detector errors, child inheritance and masked/detached stuck events. Nine actual Python managed turns match core/status/done/cancelled event fields/order and history, including provider failure and model/tool cancellation. The bus matches 200-event replay and 2,000-event drop-oldest subscriptions; ephemeral deltas do not replay. Event framing tracks transcript rewrites by epoch. Error labels/random IDs/timing/numeric spelling are normalized; an empty refusal [] becomes a valid Go empty string. Failure/denial is recorded in telemetry while paired model results omit is_error, as in source. Absent caller and explicit null remain distinct. Typed prompt hooks, named per-round injectors, Todo nagging, shared model/tool limiters and consecutive parallel batches are implemented. The nineteenth Python snapshot matches six classifier cases, nil/empty prompt rewrites, adverse completion/barrier order, result/step order and four cross-turn nag counters. Go tests cover cancelled admission, shared caps, exclusive pool bypass, completed-sibling repair, worker joins/panics, invalid injector batches and fresh-child seam/pool inheritance. Bounded steering injects after custom injectors before compaction at the next model round; named posture updates distinguish operator facts from user text. Modes refresh at permission evaluation, with child control isolation. Explicit typed SSE, provisional progress and shown-text cancellation repair are now implemented; DefaultRecovery is implemented; durable steering and mid-batch injection remain pending. |
| Registry and execution boundary | `registry.py`, `builtins.py`, `permissions.py`, `approvals.py`, `actions.py`, `tools.py`, `durable.py`; `tests/test_tool_pipeline.py`, `test_permission_modes.py`, `test_durable_approvals.py`, `test_external_cancellation.py` | Pinned catalogue; before, monotonic guard, permission, execute, after, observer order; denied and unknown-effect outcomes | All ten default inputs are typed. Go has an immutable executable catalog, one ordered gate, readonly/interactive/auto policy, in-process approval and cancellation repair. `NewWorkspaceSession` registers Bash plus read/write/edit/glob; `go/workspace` matches 34 Python output/file-effect cases and tests atomic failure and symlink-parent resolution. Go agent tests workspace binding, readonly calls and a path changed after approval. Glob matches 38 Python searches and 182 filename patterns; tests cover enumeration budgets, cancellation and deep directories under a low descriptor limit. `NewRuntimeSession` adds TodoWrite/load_skill/ask_user/compress/task (all ten executable tools), with 10 Python todo transitions, four question variants and 19 skill catalogue/load scenarios. Runtime binding, readonly behavior and question cancellation are tested. `compress` defers summary generation until the full batch is paired. Task crosses the execution-risk gate and delegates through a bound provider with depth quotas. Optional journals hash final rewritten inputs, replay terminal results and reconcile unknown actions through typed verifier verdicts; only proven non-landing permits retry. Settlement precedes observers. Twelve input/identity cases and ten actual Python replay paths match Go. Explicit optional JSON null remains distinct from an absent field through cloning, masking and identity hashing. An optional bound approval broker matches 38 Python grant candidates and 21 actual approval/question/reviewer outcomes. Tests cover parked actions, same-session grants, reviewer ordering, foreign binding, context cancellation, concurrent cancellation and fresh child isolation. Preview and stored-answer redaction are optional typed seams. Injected-store restore-time expiry is implemented; SQLite approval backend and future sink coverage remain pending. Per-call execution classifiers fail to exclusive barriers; every parallel call retains the same gate and holds one permit through hooks, execution and observers. Tool use/result spans, activity labels and reconciliation telemetry are implemented; unknown-action reconciliation emits before effects/settlement. The real host shell is implemented with typed result/failure metadata, aggregate capture, full-stream masks and process-group cancellation. Eight Python command cases and seven render recipes match; real process tests cover exited-parent pipe holders, detached-pipe cleanup, cancellation, overflow, environment selection, runtime binding and replay. No Go Seatbelt backend ships; process evidence is macOS, Linux untested and other hosts rejected. |
| Default REST and SSE | `server.py`; `tests/test_server.py`, `test_streaming.py`, `test_webui_routes.py` | Health, session CRUD/message/cancel, approval, event stream, transcript, trajectory, UI response shapes | Partial: go/httpapi is an embeddable handler with thirty-four method/path operations. The twenty-first snapshot compares authenticated/anonymous CRUD, completed replay, cancellation, approvals, Null-store transcript, basic health and finite SSE frames; eight header cases, principals, six bind cases and HTTP authority stamps match. Targeted tests cover ten-MiB ingress before auth, one admitted principal, foreign endpoints, busy admission, bounded cache/rate/list work, owner rate windows, remembered approvals, SSE cursor/envelope/query auth, disconnect cleanup/cancellation and optional JSON masking without history mutation. Cache lookup/rate/claim is atomic through result publication. Streams queue; disconnect cancels their own active turn or wait, matching a separate real Python HTTP probe. Full health posture/source build fingerprint, optional UI data routes, native SQL restart and complete FastAPI validation/coercion remain pending; injected-store bounded event catch-up and transcript epoch reads are implemented. The twenty-second snapshot compares six owner-scoped mode/steer HTTP responses and model-visible wrappers; Go concurrency tests prove one idle holder, own-manager shutdown joins and rate spending. The twenty-third snapshot compares paired fork history, model/system inheritance, fresh child state, lineage/source events and five actual HTTP responses; Go tests cover atomic source admission, cancellation repair, construction failure and shutdown. Fork persistence remains G5. Snapshot 32 compares sixteen real Python shell/auth/method/static-path outcomes and four source asset identities. The embedded JS passes all 37 source DOM interaction cases; real built-binary browser tests cover create/send/tool/final/trajectory flows. Optional pane APIs remain open. The standalone cmd/miniloop launcher owns settings composition, requested/actual bind refusal, listening and signal shutdown; real TCP and built-binary tests cover auth, tool effects and active/queued cancellation. |
| Provider and recovery | `providers.py`, `transport.py`, `recovery.py`; `tests/test_provider_surface.py`, `test_streaming_failures.py`, `test_recovery_backoff.py` | Fake and Anthropic-compatible transports; served-model identity, bounded retries, safe interrupted streams | Go fake consumes typed requests, validates transcript/schema/budgets, reports the requested model and counts the complete payload. Explicit `go/provider.Client` implements direct Anthropic-compatible HTTP with typed ingress, cache wire payloads, signed thinking, served-model/usage identity and SDK-level retries. The twenty-fourth snapshot pins SDK 0.107.1/version/source hashes and 33 real mock-HTTP cases: status and connection/body/timeout retries, headers, waits, budget preflight and descriptions. Tests cover real httptest HTTP tool execution, pairing, served aliases, context-owned timeout/cancellation/backoff, limits, credential-free errors/debug identity, rejected redirects, malformed/unsupported content and shared-client concurrency. Go adds bounded wire sizes, no automatic redirects, explicit credentials and a whole-attempt deadline; SDK has per-operation timeouts. Typed SSE assembly, coalesced provisional progress, fresh stream IDs and shown-text cancellation repair are implemented; the twenty-fifth snapshot compares nine actual SDK/StreamingTransport outcomes. Local real HTTP tests cover complete tool rounds, signed history/usage, context-owned read timeout/cancellation and 32 shared-client calls. Go requires stopped validated blocks and final delta/stop, stricter than SDK unchecked partial EOF. Captured stream progress thresholds reach manager/fork/child construction; zero/negative values and elapsed-time arrival flush match actual Python transport cases. Stateful fake clients now own atomic message IDs, signed thinking and typed responder/ceiling/delay settings, with explicit direct/stream views sharing the same sequence. Ten actual Python calls pin final content/signatures/usage and deltas; concurrent Go calls retain unique IDs, delay cancellation consumes only an admitted ordinal, and stream views lift the direct ceiling. Non-null citation/server-tool/search variants and optional request settings/auth modes remain pending. The launcher captures fake delay from the environment; direct library construction remains explicit. DefaultRecovery now matches 24 actual Python trajectories, including bounded transient waits/fallback, escalation refusal retaining the partial, whole-answer continuation, truncated tools, and one reactive shrink mirrored into paired live history. Tests verify fresh stream regeneration without splicing, permits released during outer waits, persistent session fallback and fresh child override state. Internal/protected request projections and durable recovery state remain pending. Nullable text citations are omitted explicitly by the fixture projection. No live production endpoint/cache benefit is claimed. |
| Context and model budget | `prompts.py`, `skills.py`, `compaction.py`, `caching.py`, `metering.py`, `token_efficiency.py`; relevant `tests/test_*` files | Bounded request construction, skills, cache and compaction, usage accounting | The standalone binary embeds the exact Python default skill, independent of checkout/cwd; explicit filesystem catalogues retain source rechecks. `go/skills` preserves bounded deployment catalogue/body, sorted first-wins discovery, strict UTF-8/newlines and source-digest verification against 19 Python scenarios. Diagnostic occurrence/eviction counts and concurrent shared reads are tested. Invalid-name prefixes in diagnostics are bounded/marked at 2,048 characters in Go. `protocol.ModelRequest` carries fitted typed schemas, a stable system prompt, skill descriptions, changed runtime facts and output budgets to the provider. Six catalogues, three Unicode/wire budget cases, four pair-safe snip/micro histories, six meter steps, result spill and transcript/summary artifacts match Python. Usage includes cached input, calibrates same-envelope growth and observes signed shrink; summaries do not anchor the live meter. Workspace-backed sessions use all four default compaction layers, with typed receipts and guarded deferred `compress`. Tests cover empty/failed/cancelled summaries, whole-batch ordering, cheap shrink avoiding a summary and workspace archive containment. The Bash-only constructor uses in-memory compaction for an unbound injected executor; a real bound executor selects workspace compaction. Optional RuntimeConfig.Secrets masks default spill/archives/summary without changing live arguments or the summary request. Default typed cache annotations project up to four ephemeral breakpoints into a detached wire request, including summaries; live history stays raw. Twelve Python wire/position/token cases match Go. The fake rejects more than four points. Policies and stop hooks inherit into fresh children. Existing system block-list input, real provider cache reuse/savings, user-skill layering, masking for future provider/optional-feature sinks, optional prompt sections and token-efficiency policies remain pending. |
| Private Bash preservation | `spill.py`, `tools.py`, `builtins.py`, `manager.py`; `tests/test_spill.py` | Private bounded artifacts, masked content, opaque retrieval and failure-preserving preview | Typed `go/spill.Store`, Request, Ref and LocalStore match fourteen source cases: exact UTF-8, 8,000,000-byte bounds, safe names/hash groups and private/exclusive files. Eight actual string-Bash cases and three projection recipes compare preview/file digests; collisions, planted leaf symlinks, concurrent unique saves, masks and save faults are tested. Typed RuntimeConfig/ManagerServices seams retain credentials/process tracking without mutating caller executors; forks get fresh namespace names. Launcher matches three best-effort source manager cases and retains artifacts after stop. **Actual source gap:** default Python Bash uses the structured `run_bash_result`, which bypasses string preservation; Go structured default does the same (actual managed source tool round, zero artifacts). Only the string compatibility interface appends preservation notes. No owner ACL, parent-race sandbox, unbounded capture or automatic purge is claimed. Go additionally checks UTF-8/context/short writes and attempts failed-leaf cleanup after an inode check; check/unlink is not atomic against host tampering. |
| Storage and evidence | `storage.py`, `trajectory.py`, `actions.py`, `session.py`; `tests/test_storage.py`, `test_crash_windows.py`, `test_trajectory_ownership.py` | Optional SQLite state, lease and restart behavior, append-only evidence, ownership checks | Named action records/statuses, detached snapshots, workflow binding, memory result retention and a typed ActionStore/StoredActionJournal transition adapter are implemented. Six transitions per Python memory/SQLite journal, conflict/invalid-settlement behavior and Unicode result bounds match source snapshots. Tests cover cancellation, store errors and scoped unknown marking through a typed test backing. ApprovalRecord/statuses and a typed ApprovalStore write seam preserve pending, terminal, cancellation and reviewer rows; faults are bounded diagnostics and do not rewrite human decisions. Remembered grants remain process-local. Process-local detached event subscriptions, sink fault containment and transcript-epoch framing are implemented. No shipped Go SQLite store, native-database restart evidence or cross-process dispatch fencing yet; configured-store bounded event catch-up is implemented. Explicit injected-store manager restoration is implemented. File-backed per-run trajectory persistence is implemented: typed writer/reader interfaces, masked full model/tool/child fields outside the live backlog, ordered finish receipts, source privacy redaction, retained owner reads and explicit drained purge. Snapshot 30 compares eight source store cases, seven managed cases, sixteen HTTP outcomes and ten rounding boundaries. Go adds 64 MiB per-record limits; JSON reads cap eight MiB and raw JSONL streams without append locks across client waits. Typed HTML ledger and filtered record visitor are implemented. Snapshot 31 compares eleven complete Python HTML pages, file assembly, nine iterator cases, four HTTP outcomes and source asset hashes. The independent traceview CLI renders exports, IDs or a session chronology to 0600 HTML. Snapshot 44 captures actual Python SQLite epochs, rollback, exact-expiry leases, two-connection appends, identity-preserving upserts, explicit unknown recovery, audit-preserving deletion and v1/future/corrupt schema cases. Go now has concrete SessionRecord and separate session/transcript/event/lease/approval-read consumer contracts, plus a bounded current-variant archival event decoder. Tests compare all session/message/audit projection shapes and current writer variants, including a live tool loop and untrusted archival provenance. These contract/decoder tests do not run SQL in Go. Driver approval, backend execution and native restart validation remain pending. Configured RuntimeConfig/ManagerServices.StateStore now composes live session projection, request guard, prefix rewrite epochs, masked event-first capture and process leases. Default journal/approval storage uses the injected backend; explicit overrides remain caller-owned. Snapshot 45 compares actual Python AgentSession/SQLite guard, masking, event epoch/ordinal and confirmed/unconfirmed renewal recipes with typed Go test backing. Ordinary write faults degrade; guard-query/count errors stop; confirmed lease loss cancels the turn. Go claims after admission, persists queued steering before acknowledgment and disables writes before deletion to prevent late resurrection; stop releases after drain and cleanup faults are reported. Renewal/metadata require transcript growth, so stored status may remain running after completion. The global default unknown sweep follows source policy without proving foreign process death. Native Go SQLite, real database restart validation and state-backend launcher activation remain pending. Snapshot 46 runs actual Python manager/SQLite restores for seven histories. Go restores owner/binding/system/run/status, highest epoch references, Todo/steering and sequence, expires parked approvals and repairs crash tails before the first resumed model request. Go explicitly preserves repair-time metadata, avoids payload sequence reuse and claims before repair. Foreign-held handles remain pending; admission reloads under claim. Read/repair faults refuse publication and release leases without deleting historical data; stop joins restore. This is injected-store behavior through a test backing; native SQL restart and state-backend launcher activation remain pending; typed goal snapshots/disarmed restoration are implemented; configured-store bounded SSE, transcript epochs and plan-mode events/folding are implemented. Snapshot 47 compares seven actual scheduled restore/request cases. Lazy cron restores bound or freshly provisioned scratch, preserves saved owner/state and starts a fresh untrusted turn only after arm. Missing SQL rows cannot claim; no-store anonymous fallback is ephemeral. Stop cancels cooperative restore reads. |
| Optional orchestration | `background.py`, `cron.py`, `tasks.py`, `teams.py`, `subagents.py`, `worktrees.py`, `workflows/`; matching test families | Each default and authority rule preserved; typed states and bounded queues | Default in-process task delegation is implemented: capability-selected tools, fresh child state, peer provenance, Explore readonly/worker interactive modes, depth 2 and round budget 30. Four role selections and three actual child loops match Python, including exhaustion. Custom providers cannot bypass the depth quota; tests cover nested task rebinding, cancellation repair and concurrent isolation. Children inherit the exact optional model limiter and parallel-tool limiter, plus prompt hooks/injectors and stop hooks. Explicit library TaskTools now adds the five persistent graph tools through the same gate, with typed task records and masked atomic files. Snapshot 33 covers 53 source store steps, six actual tool outputs, five tool schemas/traits and five HTTP results. Dependencies, bounded rendering, corrupt-file reporting, retained crash markers and one successful cross-process claimer are verified. Manager boards isolate workspaces and forks start fresh. Task API is active independently of tools. Full MINILOOP_FEATURES activation remains unsupported. The explicit worktrees operator library ports creation/task binding, changes/keep/remove/list, audited effects and source-compatible workspace-factory fallback. Snapshot 34 compares nine real Python/Git scenarios, and real Go tests cover Git second guards, retained partial effects and owned bound-session writes/retention. Explicit WorktreeTools now installs five closed input variants through the same risk/permission gate. Snapshot 35 compares three actual source tool flows (25 steps), five schemas/traits and eight canonical input identities, including unconfigured service, linked/pinned task board and lazy first board admission after entry. enter_worktree is an exclusive barrier even with a custom parallel classifier: files, shell/sandbox, catalogue and broker surfaces prepare before execution scope publication; model context/hooks and fresh children follow the new root. Original lifecycle workspace remains authoritative for Info/task HTTP/trajectory attribution/scratch deletion. Go tests cover failed/nil/unbound/wrong-root preparation, ordered parallel groups, retained journal/observer settlement, approval/question scopes, fresh child-state isolation (selected worktree tools remain unconfigured), fresh forks and entered work surviving scratch deletion. Built-in rebind retains secrets/spill/deadline/capture/interrupt ownership; custom executors need an explicit workspace Bash factory returning WorkspaceBashExecutor for the requested root. Empty source capabilities keep these optional tools out of default child roles. Snapshot 36 compares seven actual managed factory/Git scenarios: clean/dirty delete removes only directories, preserve/stop retains work, and nonrepo/unborn/branch-conflict allocation falls back to plain directories. A concrete WorktreeWorkspaceFactory adapts the explicit service to ManagerConfig without tool activation. Shared-holder and failed-admission tests pin Go cleanup: the last holder loses scratch while Git registration/branch remains; unpublished failure cleanup is an existing Go addition, whereas Python leaves the allocation. Actual source has no Git-aware manager reclaimer. Explicit background operator service now ports typed tasks, independent contexts/groups, merged byte capture, status/rendering, retention/listing/drain, original-root ledger and orphan reporting. Snapshot 37 captures twelve actual native commands, unrecorded execution, five retained completions, started cancellation, five orphan seeds/Unicode counter reuse, 53 orphan notifications through an actual Agent injector and ten heuristics. Python Unicode 14 decimal counter inputs are pinned across every digit block. Go additionally cleans pre-start cancellation where source leaves Running/ledger; it validates bounded typed ledger reads, retains admitted executor bindings and isolates foreground interruption. Explicit RuntimeConfig.BackgroundTools now installs two closed variants through the common gate, adapts conditional Bash dispatch/classification while retaining foreground metadata, shares denial/prefix approval logic, lazily boots per-session ledger state, injects newest-50 task notifications with typed background_result events and names live survivors in interruption markers. Workspace entry publishes the prepared native shell to both paths. Snapshot 38 compares two schemas/traits, eleven canonical/null/grant variants, six activation-dependent classifier inputs, fifteen actual source-gate calls, disabled Bash behavior and the source live-task interruption marker. Native tests cover actual model request injection, 53-orphan bounded boot without tool use, mask/readonly/destructive/foreign authority, rebind, operator cancellation and default child foreground scope. ManagerServices.BackgroundTools now composes fresh session/fork state with delete/stop joins after turn drain and before reclamation. Snapshot 39 compares five actual source manager delete/preserve/bound/stop/fork cases; Go additionally checks a draining turn, late lazy creation and real launcher HTTP shutdown with a reaped native PID. launcher.Options.BackgroundTools and --background-tools select the individual service; MINILOOP_FEATURES remains unsupported. Explicit selected child background scopes now retain independent queues/counters, derive qualified IDs from peer message identity and remain reachable by parent recursive lifetime cleanup after return. Default children omit background state. Snapshot 40 captures six actual source cases and proves inherited harness injectors can orphan/unlink a still-live parent ledger. Go initializes root adoption before child admission, never child-adopts parent records and reports qualified child orphans through root restart adoption. Native tests cover check-only and Explore policies, completion requests, preserved parent/child ledgers, returned/nested child lifetime and manager cleanup. Child metadata/queues remain unbounded; no cross-session/process ledger authority is claimed. Explicit go/cron operator service now matches typed five-field expressions, per-session controls, bounded problems, masked atomic stores, disarmed restoration, mark-before-dispatch and exclusive minute claims. Snapshot 41 captures 27 expressions, 14 operator states, one-shot/pair/loss/masking/bound cases and actual source untrusted dispatch. Go adds typed scalar/read bounds and explicit Start, error/panic reporting, cancellable joins and stopped-generation suppression; two independent native processes exercise claim arbitration. SessionManager now owns cron by default, binds fresh untrusted scheduled turns and owner-scoped library operations, removes future jobs on delete and drains/joins runs on stop. Snapshot 42 compares real source authority, five lifecycle/ownership states and a live scheduled cancellation. Go adds queued-turn suppression, continued cleanup with reported cron-save failure and native foreground-shell reaping; standalone Serve starts restored jobs without arming them. Explicit CronTools installs three closed schedule/list/cancel variants through the common gate and a typed owner-bound CronControl; model Arm is absent. Snapshot 43 compares three actual source tool contexts (24 calls), six canonical/grant inputs, schemas/traits and 29 HTTP requests. Four owner-scoped HTTP list/schedule/cancel/arm operations work without model-tool activation; HTTP preserves source boolean coercions and validation-before-owner ordering. Go adds strict schema booleans for model inputs, replay/masking/foreign-authority tests, fresh activated forks and unconfigured selected children, restored operator authorization without persisted activation and actual local TCP launcher composition. --cron-tools selects the individual service. Lazy configured-store stable-ID cron restoration is implemented; native SQLite restart evidence, exact 422 validation details, workflows and remaining runtime profiles remain pending. The manager-owned team coordinator and ten explicit TeamTools variants now cross the common gate; snapshots 110/111 cover operator and installed-handler semantics. Native teammate creation/resource inheritance, automatic round inbox delivery, owned idle/task/worktree turns and cancel/join are implemented; actual source fleet/owner reports omit shared bus diagnostics because the manager binds bus while the scanner looks for teams; native differential probes match this omission and preserve unread messages. Source read_inbox traits and partial consume effects are preserved. |
| Optional goals | `goals.py`, `permissions.py`, `session.py`, `server.py`; `tests/test_goals.py` | Revision CAS, human arming, bounded default stop continuation and disarmed restore | Five closed goal tool inputs and typed records/events are selected by GoalTools/--goal-tools; default ten tools stay unchanged. Nil StopHooks installs GoalContinuation; explicit lists replace it, including empty lists. Default consumer is inert without an armed active goal. Create/resume require explicit human provenance, whereas ordinary HTTP/cron/children remain untrusted/peer. Complete/block do not arm. Only requested stop continuations consume budget, before stuck/global-round decisions. Refusals preserve source failed=false. Whole snapshots/clear tombstones fold under restore and pending claim reload, always disarmed; forks/children start fresh. Owned GET goal projects masked detached facts. Snapshot 51 compares 46 real source gate calls, six stop calls, default/custom loops, actual Python SQLite restore/rearm/requests and four owned HTTP views. Native tests cover parallel CAS, overflow, capture-before-request, authenticated HTTP refusal, masking, pending reload, forks/children and TCP launcher. Go revisions are bounded int64 and strict JSON rejects Python coercions; corrupt/unknown archival variants fail closed. No edit/clear/pause tools ship; source cap text still mentions nonexistent goal_edit. No native Go SQL durability is claimed. |
| Optional plan mode | `plan_mode.py`, `prompts.py`, `session.py`; `tests/test_plan_mode.py` | Stable catalog, soft guidance, review refusal with feedback and last logged whole boolean | Explicit typed RuntimeConfig/ManagerServices/launcher PlanModeTools plus --plan-mode-tools install two closed variants through the common gate. Nil uses source headless approval; typed PlanApprover receives bound authority. Fault/panic/cancel keeps planning. Default/custom builders receive boolean state; fixed prompts remain fixed. Readonly/Auto permissions stay independent. Boolean events flow through existing capture/SSE/trajectory paths and restore fold, including pending reload. Forks start off; selected children bind fresh state; default role profiles omit source capability-free tools. Snapshot 50 matches 35 actual source gate calls, source schemas/traits/canonical grants and two Python SQLite restores plus next requests. Go tests three actual model rounds, persistence-before-request, faults/cancel/masking/foreign identity, forks/children and real TCP launcher. Go archival active is strict boolean, whereas Python's generic fold uses truthiness; source emits booleans. Restore folds all logged scopes, as source. Native Go SQLite and approval UI wiring remain pending. Plan refusals now preserve source text with failed=false/completed journal status; callback/hook faults remain failed=true. Snapshot 50 additionally pins 35 failed/denied outcomes. Snapshot 52 compares five real managed model loops, settled observers, memory journal rows, stuck-step flags, live/stored events, real recording error metrics and same-action replay. Model results omit is_error. This specific telemetry gap is closed; full release audit remains open. |
| Optional user resources | `user_resources.py`, `memory.py`, `skill_capture.py`; `tests/test_user_resource_lifecycle.py` | Owner snapshots and bounded publication | Canonical fields, exact private directories, layered catalogues, scoped Markdown memory, immutable owner snapshots and operator create-only publication are implemented as typed libraries. Snapshot 63 compares 16 actual Python publication scenarios and 27 calls; explicit trusted manager/runtime snapshot binding is implemented; explicit owner memory tools, automatic selection/extraction/consolidation and contained source-endpoint capture are implemented; snapshots 68/69 compare origins, owner isolation and actual endpoint calls. Shared manager Memory fallback now binds exact admitted owners across create/fork/ordinary and scheduled restoration; native tests reopen Markdown files and preserve resource-resolver precedence. Launcher/configuration roots and individual memory tool/auto selection are now implemented; snapshot 70 compares seven actual source constructor cases. Trusted successful-turn skill capture is now implemented; typed candidate parsing and preview business flow are implemented; standalone native-session model binding is implemented; manager binding and HTTP publication remain pending. |
| Optional typed decisions | `decisions.py`, `decision_tools.py`, `decision_llm.py`; `tests/test_decisions.py`, `test_decision_tools.py`, `test_decision_llm.py`, `test_decision_replay.py` | Explicit choice/score/noul judgments, actual model/usage, default-off and no execution authority | The go/decisions operator library has closed JSON Value variants and named Request/Question/Answer/Result/TokenUsage/Provider contracts. Snapshots detach every mutable surface. Strict request/result validation preserves IDs, full distributions, maximum choice, weighted score/legend, noul probability and optional whole usage. Fixed-endpoint Jev transport disables redirects, ignores proxies for owned clients, closes response bodies, bounds total deadlines/read bytes/retries and sanitizes transport feedback. Borrowed transports stay caller-owned; concurrent calls are tested. Snapshot 53 compares 28 actual source requests, 22 results, four UTF-8 bounds and 21 offline HTTP cases, including retry-after clamp/fallback, non-retried status, malformed/duplicate payload and transport faults. Native tests additionally cover local TLS, cancellation/read caps and exact large-integer legend equality. Go counts are signed64; nonstandard JSON/duplicate local keys and excessive wire nesting fail closed. Canonical map order is not exact source insertion order. Explicit RuntimeConfig/ManagerServices DecisionTools now installs the closed external-risk tool; backend injection alone leaves it off. State and member names are masked/revalidated before custom/Jev or fresh isolated LLM queries. Shared model limits, complete-response checks before recovery, typed decision/custom-model metadata and private trajectory inputs are implemented. Snapshot 54 compares 31 actual Python LLM recipes and eight gate outcomes. Native tests cover child/manager composition, fallback/parent isolation, timeout/cancellation/panic, recording projections and exact >4 KiB result replay under current guards. Default-off remains. Launcher/environment activation now selects llm/jev, with explicit backend precedence and credential-free offline inspection. Snapshot 55 compares actual source maximum-byte memory/SQLite-reopen replay, aggregate shedding and Unicode bounds/reconciliation; native common-gate replay and recreated stored adapters match result hashes/retention. Snapshot 56 compares cooperative cancellation, journal cancellation and permit release, and pins a source escaped-model negative case. Go masks result structures before escaping; full managed tests inspect decoded live/SSE, sink callbacks, private trajectories, stored events/messages and action results. This deliberately closes the source escaped-output masking gap. Native SQL/restart and live-provider audit remain pending. Native replies require complete model/usage, surrogate refusal occurs earlier, unknown backend failures normalize to RuntimeError and derived floats use 1e-12 tolerance. No paid service or native SQL decision evidence is claimed. |
| Configuration and standalone startup | `config.py`, `__main__.py`, `server.py`, `skills.py`; configuration/startup tests | Named settings, loud validation, explicit service wiring, bind guard, shutdown and redacted diagnostics | `go/config` has all 49 source fields; 64 real Python outcomes compare accepted values and rejected configurations. Go has explicit int/duration bounds, no `.env` discovery or reload, and pure loading without Python Settings workspace mkdir. `launcher.New` wires supported services, `App.Serve` owns TCP listener/shutdown, and cmd/miniloop handles SIGINT/SIGTERM. Default trajectory recording is active; its configured root failure refuses startup. Enabled unavailable services still refuse activation. The private spill root is now best-effort constructed by default; an empty path disables it, and root failure retains startup. Builtin skill bytes/descriptions/load match Python. `--dump-config` reports settings-and-availability without runtime/probe, redacts credential fields and URL secrets, and includes Go VCS build info; it is not full effective posture. Local HTTP provider and real shell timeout tests prove option wiring. Positive subnanosecond durations and nonfinite values refuse; fake-delay validation also runs for a real provider. Public console/UI shells are implemented; missing optional services and full posture remain G3/G5/G6. |
| Audit and extension seams | `config.py`, `harness.py`, `identity.py`, `audit.py`, `EXTENDING.md`; `tests/test_extension_contracts.py`, `test_effective_posture.py` | Explicit constructor dependencies, truthful default posture and credential-free reporting | Typed UserPromptHook and named MessageInjector seams receive detached history/todo/authority views; full injector batches validate before append, before runtime facts and compaction. Optional typed ApprovalReviewer and ApprovalRedactor seams, default CachePolicy/StuckDetector, typed StopHook continuation and optional ordered EventSink are implemented. Sink faults/panics are contained; callbacks may inspect Info/events but cannot recursively emit or block on live Messages. Go constructors reject invalid cache strides and detector thresholds; valid configured cases match Python. Reviewer faults/invalid verdicts abstain; remembered human grants precede review, and readonly/final denial stays authoritative. Guardian implementation, full effective-posture reporting and future provider/SQLite/optional-feature sink masking remain pending. The typed go/secrets Registry preserves named injection APIs, lazy cached values, retry/reporting, ANSI matching, Unicode casing and the default Null posture; 15 mask recipes, four environments and ten typed previews match actual Python. Runtime integration tests cover journal-before-observer order, raw execution arguments, masked model results, scoped shared-broker registries, compaction artifacts and child event isolation. The real shell consumes the selected environment and masks full/split streams before projection. Runtime registries bind independent credential configurations without mutating a shared executor. |

Snapshot 48 invokes the actual Python events endpoint iterator over SQLite/Null
for 17 window/header cases, including subscription-before-read and a concurrent
boundary emission. Go separates physical EventOrdinal from SSE EventSequence,
reads the newest 2,000 stored rows and filters by sequence. The source drops 50
stored status events when ephemeral sequences move its ordinal query past the
physical head, and raises SQL OverflowError on a huge positive header; Go closes
these measured gaps. Fresh/invalid/negative IDs use the 200-event backlog; the
pinned Python 3.11 default 4,300-digit conversion bound is retained. Owned TCP
tests prove de-duplication, live writes during catch-up, fresh replay, disconnect
cancellation and opaque pre-SSE 503 read failures. No native SQL/reopen proof is
claimed; the Go runtime tests use synchronized injected memory backings.

## HTTP inventory

Personal-skill request boundary (2026-10-07): snapshot 80 compares 43 actual
Pydantic preview/commit validation outcomes and normalized values with the native
typed decoders. Unknown fields, exact keys, null/type rejection, source patterns,
Unicode code-point limits and duplicate-key normalization are covered. This is
request decoding evidence. Snapshot 81 adds 11 actual HTTP outcomes for both
routes, including authenticated capture, preview/receipt fields, policy/draft
errors, and failure retention followed by success. Snapshot 82 compares 41 actual
FastAPI validation lists with ordered errors, locations, context and input echo.
Snapshot 83 adds 63 actual syntax/media/UTF-8 responses, including code-point
offsets, strict missing Content-Type, application +json, UTF-8 BOM and illegal
encoding. Snapshot 84 adds 88 scalar UTF-8/16/32 HTTP outcomes with BOM/NUL byte
detection, source Unicode offsets and invalid unit handling. Its six historical
surrogate counterexamples are now included in snapshot 85's 90 passing comparisons.
Raw/escaped surrogate handling, code-point syntax offsets, last-key retention and
safe Unicode refusals are covered. Snapshot 86 adds 288 actual numeric HTTP
outcomes: source double rounding, arbitrary precision integers, the pinned
4300-digit parse limit, nonfinite refusal and last-key retention. Snapshot 87 adds
144 live Uvicorn HTTP depth outcomes, compared both without and with registered
masking (288 native comparisons). Its pinned recursion-1000 profile permits 985
containers at parsing and 978 in echoed values; TestClient's stack has different
thresholds. Alternate runtime-stack profiles remain unverified. Snapshot 88 adds
48 actual owned catalogue HTTP outcomes across empty, legacy, layered and anonymous
scenarios. Retained versus future/fork publication visibility and owner admission
match; native tests additionally verify registered-secret masking and safe custom
catalogue failures.

`python/mini_loop/server.py` currently declares 44 routes. The default Go slice should
start with `/healthz`, `/sessions`, session detail/deletion,
`/sessions/{session_id}/messages`, `/sessions/{session_id}/cancel`,
`/sessions/{session_id}/events`, approvals, transcript, trajectories, `/`, and
`/ui`. Optional route groups cover skills/memory, workflows, cron, tasks/team,
improvement, audit, and benchmark. Every route requires a response/error/event
fixture before its row can be marked covered; route presence alone is weak
evidence. The current Go handler registers all 44 Source method/path operations across
40 patterns (Go /{$} implements the exact Source / root). Registration does not
establish complete response, error, persistence or G7 parity. Snapshot 131 adds
44 real workflow HTTP comparisons; launcher activation and additional malformed
profiles remain pending. The implemented boundary inventory is:

| Method | Path | Current boundary |
|---|---|---|
| GET | / | Public embedded development console; protected data APIs |
| GET | /ui | Public embedded full source shell; optional data routes remain open |
| GET | /healthz | Basic fields; full posture deferred |
| POST | /benchmark | Authenticated/rate-limited fake-only visible and heldout comparison; fresh supported config/deployment catalogue; joined temporary cleanup |
| GET | /self-audit | Admitted-owner live report under auth; fleet operator view when open; projected plain text and source character cap |
| GET | /self-audit/suggestions | Ledger-only inert typed objectives; source default limit; no recording IO or rate spending |
| GET | /self-audit/bench-task-drafts | Ledger-only typed draft tasks with fixed null expectation; no admission or work launch |
| POST, GET | /sessions | Create and bounded recent listing |
| GET, DELETE | /sessions/{session_id} | Owner-scoped lookup and draining deletion |
| POST | /sessions/{session_id}/messages | Atomic busy admission, replay and rate bounds |
| POST | /sessions/{session_id}/messages/stream | Queued turn with own disconnect cancellation |
| POST | /sessions/{session_id}/cancel | Active-turn cancellation |
| POST | /sessions/{session_id}/mode | Current permission mode and next-round posture note |
| POST | /sessions/{session_id}/steer | Owned idle wakeup or bounded busy steering |
| POST | /sessions/{session_id}/fork | Completed transcript copy, fresh scratch, typed lineage and source event |
| POST | /sessions/{session_id}/personal-skills/preview | Owned evidence preview; typed policy/validation/syntax errors and strict media; pinned byte, Unicode, numeric and live HTTP depth semantics; alternate stack profiles unverified |
| POST | /sessions/{session_id}/personal-skills/{draft_id}/commit | Owned reviewed publication; exact cleanup; typed validation/syntax errors and strict media; pinned byte, Unicode, numeric and live HTTP depth semantics; alternate stack profiles unverified |
| GET | /sessions/{session_id}/skills | Owned retained model-facing catalogue; typed masked description response; publication affects future sessions/forks |
| GET | /sessions/{session_id}/memory | Owned latest metadata from the fixed scoped store; typed masked response |
| GET | /sessions/{session_id}/memory/{name} | Owned exact-name body; source missing-name quoting and encoded-slash refusal |
| GET | /sessions/{session_id}/approvals | Scoped pending approvals |
| POST | /sessions/{session_id}/approvals/{approval_id} | Bound allow/deny/answer/remember |
| GET | /sessions/{session_id}/events | Bounded replay/live SSE; configured event-store catch-up (2,000 stored rows), owner admission before read and sequence deduplication; native SQL pending |
| GET | /sessions/{session_id}/transcript | Owned configured-store epoch reads; concrete persisted messages, historical/gap/crash-tail views and pinned query validation; Null-store 404 remains |
| GET, POST | /sessions/{session_id}/workflows | Owned list plus authenticated explicit-human launch; stable action/message identity and journal replay; defaults remain off |
| GET | /sessions/{session_id}/workflows/{run_id} | Scoped typed run detail; missing/foreign run indistinguishable |
| POST | /sessions/{session_id}/workflows/{run_id}/cancel | Ownership reduces capability; required reason model before lookup; owned worker settlement |
| GET, POST | /sessions/{session_id}/cron | Owned structured jobs and schedule; fresh process authorization |
| DELETE | /sessions/{session_id}/cron/{job_id} | Owned cancel; foreign job reads like missing |
| POST | /sessions/{session_id}/cron/{job_id}/arm | Operator-only process authorization for restored jobs |
| GET | /sessions/{session_id}/goal | Owned detached goal/arming/plan facts; no HTTP arming or mutation |
| GET | /improvements | Manager-rooted newest-first lineage; auth-bound owner, open operator view, source legacy data and private serialization failures |
| POST | /sessions/{session_id}/propose-improvement | Owner-bound verified proposal in existing checkout; source typed validation and busy/logical/private failures |
| GET | /sessions/{session_id}/tasks | Fresh workspace board after owner admission; non-consuming structured rows |
| GET | /sessions/{session_id}/team | Bound lead identity, manager-rooted non-consuming newest 50; historical data preserved and private encoding failures |
| GET | /sessions/{session_id}/trajectories | Live owned session recording list |
| GET | /trajectories | Recorded-owner filtered file summaries |
| GET | /trajectories/{trajectory_id} | Owned JSON document; eight-MiB source cap |
| GET | /trajectories/{trajectory_id}/export | Owned JSON or unlocked JSONL stream |
| GET | /trajectories/{trajectory_id}/view | Owned HTML ledger; eight-MiB source cap |


## Defaults to preserve

The README reports core loop, workspace tools, REST/SSE, caching, stuck
detection, and local trajectory recording as on by default. Comprehensive
features, authentication on loopback, owner resources, token efficiency,
guardian, typed decisions, and workflows are off unless configured. SQLite,
sandbox, and secrets are null boundaries by default. The verified loop is
library-only. Go documentation and tests must state the actual Go posture,
even while it differs from Python.

## Owner resource contract progress

Snapshot 57 invokes Python user_resources canonicalization and owner hashing.
Go userresources now has closed SkillFields, immutable CanonicalSkill, named safe
ValidationError/Code and exact DirectoryKey derivation. Character/line bounds,
Unicode wrapper matching, newline/whitespace normalization and content hashes
match 42 source cases; ten owner keys preserve exact identifiers. Native UTF-8
refusal is stronger. No directory/owner binding, layered loader, memory store,
publication/activation or HTTP route ships in this library slice.

Owner directory progress: snapshot 58 compares ten actual Python root/child path
recipes. Native DirectoryResolver/DirectoryBinding now pin resolved trusted roots,
tighten 0700 permissions, reject planted child symlinks/files and cache immutable
exact owner paths. Cancellation/concurrent reuse are tested. The source's stores
and layered catalogue are not constructed natively yet; session/HTTP/configuration
activation remains pending. Source path checks are not external-process fencing.

Layered skill progress: snapshot 59 compares 16 actual Python source pairs,
qualified/scope selection, collision/invalid/unknown-name refusals, Unicode
combined budgets and per-source changed/missing/identical/newline-equivalent
file verification. Native LayeredCatalog is concrete, uses the same verifier as
legacy Catalog and preserves separate problem ownership. Builtin/cancellation
and detached diagnostics are native tests. Owner/session composition is pending.

Memory storage progress: snapshot 60 compares 13 actual Python MemoryStore/
ScopedMemory scenarios, 118 operations and exact file hashes. Typed native file
storage implements cache/deferred index/search/owner replacement/legacy migration
and masking. Context cancellation, private temporary modes and closed scoped API
are native additions; imported bodies retain source full reads and mtime/size
cache behavior. LLM selection/extraction/consolidation, tools and trusted runtime
resource composition remain pending.

Owner resource composition progress: snapshot 61 compares actual Python frozen
bundles, exact owner caches, fixed skill snapshots, independent user logs,
operator diagnostics, masked memory and owner isolation. Native Resources uses
private binding fields and returns ScopedStore directly; completed cache values
are shared, failed builds are uncached and retries recheck links. Four source
scenarios and native concurrency/cancellation/retry checks pass. Publication,
manager/restore/child composition, configuration and routes remain pending.

Publication file-boundary progress: snapshot 62 compares 12 actual Python
atomic_create_bytes/read_bytes_no_follow scenarios. Native durable uses anchored
component-wise O_NOFOLLOW descriptors, bounded regular reads, private fsynced
scratch and no-replace hard-link commits; typed device/inode identity is returned.
Darwin native concurrency/rename/cancellation/FIFO tests pass. Linux/Intel-Darwin
compile checks are separate from runtime evidence. Full skill publication remains
pending; no path-only replacement is used as an equivalent.

Pre-commit catalogue progress: native Catalog.WithSourceDocument prepares a
detached snapshot with the existing parser before filesystem commit, refusing
name/path conflicts and malformed/truncated sources. Existing snapshot 57's valid
actual Python canonical cases compare full-source/body hashes, prepared entries,
restart descriptions and loaded output. Native tests cover absent/changed-file
refusal, path ordering, independent concurrent diagnostics, bounds and cancelled
preparation. This is a publisher prerequisite; full publication and managed
activation remain pending. No new source snapshot or coverage measurement.

Create-only publisher progress: snapshot 63 exercises 16 actual Python scenarios,
27 calls, safe receipt/error projections, collision warnings, normalization, raw
secret screening, short/unresolved registry refusal, owner isolation and exact
committed bytes/modes. Native PublishSkill retains live snapshots and memory,
prepares before hard-link commit, verifies bounded exact canonical retries and
handles independent resolver races. Native secret-surface/panic/cancellation tests
fail closed; source digest retention is stronger than synthetic Python entries.
Capture/preview, manager/configuration/routes and cross-process cache refresh
remain pending. No vague domain payloads or new dependencies were introduced.

Managed resource progress: snapshot 64 runs actual Python manager create/fork,
teammate binding, SQLite restoration, missing scheduled restoration and disabled
legacy layering: nine frames and 36 actual tool calls include domain-refusal
flags/output hashes. Native explicit ManagerServices.UserResources resolves owners
for new/forked/restored handles; RuntimeConfig.UserResources validates and copies
a complete matching bundle. Live snapshots remain fixed and selected children
inherit. Native tests compare the source teammate's inherited binding through the
runtime seam; they do not implement a teammate scheduler. Native restored-state
evidence uses the injected test backing, not Go SQLite. Complete native child
bundle retention also pins scoped memory, while Python in-process subagents
explicitly inherit skills. Memory lifecycle/tools, launcher root selection,
capture/routes, native SQL, teams and remaining groups are still pending.

Memory tool progress: snapshot 65 captures 14 actual Python common-gate calls
over a shared memory store, including owner isolation, overwrite, defaults/nulls,
unknown string type normalization, recall provenance/escaping and readonly
denial. Native closed input variants compare schema/traits/replay identity;
explicit runtime/managed tools retain bound owner stores and selected-child
sharing. Native malformed non-string payloads are refused at the typed boundary.
Default tools/launcher activation, automatic memory selection/extraction/
consolidation, context index and capture/routes remain pending.

Memory context progress: snapshot 66 executes 12 actual source selection side
queries and change-only runtime-facts cases. Native optional memory tools now
default automatic selection on, with detached MemoryAuto=false override.
Prepared input, request tail/budget/absence of tools/system, model fault fallback,
ordered duplicate/bool indices, owner isolation, unchanged live meter and typed
load events match. Native JSON rejects nonfinite values into lexical fallback;
transcript/lease authority failures propagate. Runtime index requires recall,
independently of the auto flag. Full native turn and cancellation tests exercise
request integration. Extraction/consolidation, healthy-endpoint capture and
launcher/default activation remain pending.

Memory lifecycle progress (2026-10-07): Store-owned process-local lifecycle
serialization is shared by every scoped binding and separate from the ordinary
operation lock. Explicit remember holds it through Write, matching
python/mini_loop/memory.py install_memory; automatic extraction/consolidation
remain pending. Native tests exercise shared bindings, cancellable waiting,
error release, independent stores, and scoped operations inside a callback.
A native bypass mutation is caught by the gated remember cancellation test.

Memory extraction stage progress (2026-10-07): snapshot 67 executes actual
extract_memories through Agent._create in 13 cases, comparing the prompt,
1,500-token request, source agent_turn purpose, incremental count/files/origins,
owner isolation, ASCII Unicode tail and greedy context stripping. Go retains
supported typed thinking/tool-use variants while removing tool results and
runtime/recalled context. Side requests now carry explicit history ownership;
normal recovery/cache/provider/event behavior remains, without live meter/history.
The native decoder rejects non-string/null entry fields and nonfinite JSON;
source may coerce malformed header scalars. Cancellation and native transcript/
lease failures propagate. The stage awaits consolidation and healthy-endpoint
capture wiring; default runtime end behavior has not been activated yet.

Memory consolidation/capture progress (2026-10-07): snapshot 68 compares ten actual
Python consolidation cases, and snapshot 69 compares eleven real Agent endpoint
paths. Automatic capture holds lifecycle across extraction and owner-scoped
replacement, requires the tool pair/auto/writable posture, preserves unchanged
origins and contains ordinary lifecycle faults. Normal final, stop-hook stuck halt
and exhaustion capture; source tool-batch stuck halt omission is measured/preserved.
Readonly, disabled, missing pair, provider error and cancellation produce no
capture writes. Native authority loss propagates; capture error class labels differ
from Python. Typed extract/error events detach, mask and archive without authority.
Root/default launcher selection is delivered below; user-skill capture/preview/routes remain pending.


Launcher memory progress (2026-10-07): snapshot 70 records seven actual source
manager construction cases: default/configured shared storage, owner-local with
shared storage retained, and shared/owner roots blocked by regular files. Native
startup compares outcomes, side effects and owner-root permissions. The launcher
always constructs the shared Store, then optional UserResources Resolver; exact
owner binding stays in the common manager composition. Native HTTP provider tests
exercise all selected storage modes, Alice/Bob/anonymous isolation, owner-local
precedence and copied MemoryAuto=false. Default startup installs no memory tools;
explicit Options.MemoryTools/--memory-tools installs the pair. Automatic behavior
defaults on only when that pair is present, with --memory-auto=false opt-out.
Dump/Inspect reports selected memory backend/tool/auto choices without effects.
Comprehensive feature activation stays unavailable. Root errors refuse startup;
no memory tool can change owner or roots. Native SQLite and user-skill capture/
preview/routes remain open.


Personal-skill draft storage progress (2026-10-07): snapshot 71 compares 40 actual
Python operations and source defaults. The native explicit DraftStore uses typed
coverage, identifiers, immutable private handles and detached public previews.
FIFO session/owner/global quotas preserve other owners; wrong authority is 404
before expiry/digest checks, expired access is 410, digest mismatch is 409 and
capacity refusal is 429. Get/Peek do not consume; atomic Consume is one-shot;
DiscardCommitted requires exact identity and ignores TTL after publication.
Evidence remains signed/duplicate integers at this storage layer, matching source.
Native tests add 32 concurrent consumers, detached evidence and hidden authority.
Configuration uses typed durations and safe errors (Python uses ValueError);
initial entropy failure occurs before quota mutation. Model preview/projection,
manager injection and authenticated skill routes are still pending.


Skill evidence projection progress (2026-10-07): snapshot 72 compares 38 actual
source legacy/admitted-text cases, full compact Unicode JSON hashes and metadata.
The native pure helpers preserve greedy recalled-memory stripping, malformed
wrapper refusal, whole interjection handling, injected marker/compaction flags,
assistant text-only blocks and complete user-array exclusion. Already-admitted
text is preserved verbatim. Source whitespace/IGNORECASE and Unicode 14 word
boundaries are pinned, including a Unicode 15 classification difference. Fixed
field strings/keys mask before whole-message suffix budgeting, including key
collisions and role-label redaction; labels establish no provider role. Exact
brackets/commas/escapes/Unicode cost, 40k cap, omissions and coverage match. Native
limits/UTF-8/typed protocol validation are stricter at malformed boundaries.
Model preview and authenticated skill routes remain
pending; legacy filtering does not grant admission.

Admitted-turn skill capture progress (2026-10-07): snapshot 73 compares 20 actual
Python recorder recipes and their state/projection hashes. ManagedSession owns
a fresh process-local typed ledger, populated after successful terminal flush
only with trusted capture capability. HTTP normal/streaming calls record once;
idempotent replay, foreign requests, ordinary/peer turns and cancellation do not.
Source masking, Unicode trim/JSON costs, 64-message/40k bounds, single-message
eviction, omission counts and plain-history compaction flags match. Registered
short/unresolved or unavailable screening latches failure; healthy recovery does
not clear it. Projection preserves masked field/key/collision semantics and
refuses capture errors. Native snapshots detach and serialize concurrent readers;
typed storage removes malformed dynamic-state cases and faults use safe latches.
No ledger is reconstructed from restored/forked history. Candidate parsing is
implemented below; model preview, draft-manager injection and authenticated routes
remain pending.

Skill candidate parser progress (2026-10-07): snapshot 74 compares 63 actual
source outcomes and accepted-result hashes. Typed immutable create/skip results
preserve original fields and detached evidence. Exact schema, recursive masking
and subsequent validation order, empty skip, strict integer/unique/in-range
evidence and canonical skill validation match. Transient JSON boundary records
preserve last-wins duplicate keys and nonfinite-type rejection outcomes, without
retained dynamic/nonfinite payload. Source integer digit limits are pinned. Native
UTF-8/lone-surrogate refusals prevent lossy decoding; safe masking_unavailable
contains native masker panics. Preview health checks/model retries, manager draft
injection and authenticated routes remain pending; no runtime activation changed.

Skill preview business flow progress (2026-10-07): snapshot 75 compares 29
actual Python flows, exact request hashes and retained draft fields/digest. The
complete preview function is ported through typed model/store seams: strict
ledger presence, projection/refusal ordering, masked bounded focus, fixed system/
output tokens, two sanitized attempts, safe repair reason, provider/cancellation
outcomes, post-parse health, skip and bound retention. Closed JSON maps are
transient boundaries; no arbitrary payload enters service state. Failed previews
retain no draft. Native panic/deadline handling is explicit. The native-session
model adapter, manager admission/lease binding and authenticated routes remain
pending; this library grants no publication authority or runtime activation.

Standalone native skill preview progress (2026-10-07): snapshot 76 compares six
actual Agent model calls, repair/refusal/provider outcomes and model events.
Session.PreviewPersonalSkill serializes with core turns and binds configured
owner/session to process-local drafts. Normal cache/recovery/shared limiter and
telemetry are retained; tools are empty and only text response blocks are joined.
Recovery receives no live history and the live token meter is unchanged. Manager
admission/lease/cancellation, draft injection and authenticated routes remain open.

Manager skill draft storage (2026-10-07): snapshot 77 compares seven actual
Python create/fork/ordinary and scheduled restore frames with the shared pool,
including resources-disabled and anonymous construction. Managers now inject
one process-local typed DraftStore through the common runtime factory. New
managers start empty; SQL source evidence does not prove a native SQL backend.
Native checks verify adapter retention/owner absence and global 64-item capacity
across sixteen sessions without foreign eviction. Manager preview admission/lease
and authenticated routes remain pending.

Owned manager preview (2026-10-07): snapshot 78 compares eight actual source
policy/preview outcomes. Native owner/configuration, admission identity recheck,
lease requirement and strict ledger supply are implemented. Readonly can preview.
Native before/during request lease loss maps to safe 409; turns retain idle status
and count, with separate private cancellation/join on deletion/stop. This join
extends source turn-only cleanup. Commit/publication HTTP routes remain open.

Manager reviewed commit (2026-10-07): snapshot 79 compares six actual source
owner/readonly/digest/publication/retention/idempotency cases. Commit now shares
preview admission/lease/lifetime, publishes through the typed create-only resolver
and discards exact identity only after success. Failure retains drafts; receipts
activate in future sessions and preserve source status/message classification.
HTTP preview/commit routes and final lifecycle audit remain open.

### HTTP resource boundary — current checkpoint

Owned preview/commit routes are implemented; their body admission now covers
source media, byte encodings, syntax, retained Unicode and numeric values, ordered
schema diagnostics and pinned live HTTP depth. Earlier resource paragraphs above
record chronological implementation checkpoints and their then-open work. They
are historical; current resource status is summarized in the matrix and HTTP
inventory. Native SQLite, complete teams
and other remaining groups still require their planned implementation and audit.


## Benchmark statistics progress — 2026-10-07

The native benchmark result/measurement types, median aggregation, strict pass
votes, paired conservative verdict and transcript motion metrics are implemented
as an operator library. Snapshot 90 compares 10 actual source aggregations,
15 paired comparisons (including task-set errors), six typed-transcript metric
cases and 20 decimal-rounding cases. Exact large integers, bool-as-number,
integer/float median identity, duplicate task pairing/all-row totals, ordered
warnings, zero-base deltas and identical read windows are explicit. Nonfinite
native arithmetic refuses with a typed error; malformed arbitrary source rows are
not representable by the concrete native result struct. The JSON integer limit
is pinned to Python's current 4300 digits. An immutable runtime/manager-create
tool selection now matches source subset order, duplicate/unknown/empty semantics
and reduces both model schemas and actual gate handlers; forced excluded write/Bash
calls cannot reach their handlers, and child roles inherit the reduced catalogue.
This transient construction profile is not persisted across fork/restore.
Five visible and three heldout task specifications, trusted typed judge/setup
callbacks and exact seeded log bytes are now implemented. Snapshot 91 compares
eight source specs, 50 actual filesystem/text judgments and the 324000-byte log
digest. Unicode splitlines, strict UTF-8 faults, permissive substring/existence
behavior and symlink semantics are pinned. Nil native judges/zero tasks refuse
explicitly; malformed arbitrary callback objects are unrepresentable.
Native RunArm now owns/joins a manager, executes fresh anonymous interactive
sessions, captures task storage, applies whitelists and measures real run time and
final transcript cost/motion. Setup/create/cancellation abort; ordinary run/judge
faults score failed rows. Snapshot 92 captures eight actual source arm recipes,
including visible/heldout default-fake effects, empty sets and fault/recovery
boundaries. The eight default task effects/final texts/deterministic rows match.
Native failure diagnostic spelling remains different: the recovered fault fixture
costs 38 versus source 32. FakeObjectProvider models source non-SDK object transcript
projection without changing raw client/real-provider absent/null semantics; the
launcher selects it. Empty native history now costs zero. POST /benchmark now
composes four fresh fake clients and owned arms, with transient roots removed after
all joins. Snapshot 93 compares five actual HTTP profiles: default, ignored body,
malformed JSON body, real-main configuration and one-round fresh environment.
Only duration and its derived warning are normalized; other results/metrics match.
Native tests cover authentication/methods, shared per-owner rate budget, cancellation
and construction cleanup, fresh deployment catalogues and private configuration
failures. The handler now has 34 operations/31 patterns. Source create is not
rate-limited; benchmark shares the message/fork/steer expensive-route budget.
Unsupported activated runtime profiles remain explicit failures, not parity.
No G7 acceptance or migration completion is claimed.


## Typed self-audit observation core

`go/selfaudit` renders explicit observation snapshots for the five source report
sections, with typed optional fields and safe failure classes. Activity uses the
100 most recent sessions; owner totals preserve the source's bounded count.
Problems retain ledger ordering/counts/churn; trends use 50 global summaries or
10 per recent owned session (at most 20 sessions/50 summaries). Skill usage scans
200 tool-use events per summary and labels correlation. Cron exposes sorted
disarmed IDs only when global inclusion is selected. Reports cap at 8000 Unicode
characters with the source truncation marker.

Suggestions inspect the last three distinct entries per source, strip Python
whitespace, cap at 300 characters and deduplicate after truncation. Limits clamp
to at least one; the default is eight. Draft names use the source SHA-256 prefix
and ledger- name. Expect is a concrete null-only type, never a callable; native
benchmark.NewTask still rejects a draft without a human-authored judge.
Snapshot 94 compares 26 actual source processing profiles, including scope,
empty/missing seams, Unicode/deduplication, scan/report caps and independent
collection/inspection/event failures. No report fields or timings are normalized.
Native tests also refuse non-null draft expectations and check input immutability.
Snapshot 94 proves the processing library; manager collection is now implemented
as described below. Runtime tool installation remains pending; the HTTP routes are implemented below. It does not synthesize empty
reports for unimplemented services. Source parity evidence covers scalar UTF-8 and finite numeric observation
profiles; arbitrary malformed Python namespace values are not claimed covered. No HTTP inventory/G7 acceptance is added by these core checks.


## Exact problem ledger and holder diagnostics

`go/problems.Log` ports source ProblemLog append/extend/clear, bounded distinct
FIFO retention, repeat counts, lifetime totals, eviction counts and churn. Snapshot
95 compares nine actual source sequences, including 400 appends across a churning
four-message/three-slot log, returning evictions, exact Unicode/whitespace and
nonpositive capacities. Source invalid-capacity append increments total before
IndexError; native ErrEmptyEviction preserves that state and unwrap-compatible
error identity. Native ingress accepts rendered strings; arbitrary Python object
str() callbacks/list mutation are outside this typed API. Zero Log uses default
50; explicit New(0) preserves zero and its append failure.

Counters use immutable exact nonnegative integers and detached big.Int views.
Ledger.Total now accepts that Counter instead of a machine-sized int, retaining
all prior snapshot-94 report outputs and JSON integer shape. Source header counts
are rendered without float conversion. Optional ProblemSource exposes detached
SelfAuditProblems. ApprovalBroker and InMemoryActionJournal record every diagnostic
occurrence before their legacy deduplication; existing Problems() remains unchanged.
Native tests trigger repeated real reviewer faults and result shedding, checking
counted snapshots, private panic omission, old API behavior and replay authority.
Manager collection and native holder adapters are now implemented below. Stored
journals without a diagnostic seam and model tool remain pending. HTTP binding
is implemented below; inventory is now 37 operations/34 patterns.


## Live self-audit observation

SessionManager.ObserveSelfAudit captures owner-admitted handles before any injected
service IO, sorts actual immutable creation times and inspects at most 100 handles.
TotalSessions retains fleet size without inspecting older handles and is scoped
to the bounded owned set in owner observations. No manager/session lock spans IO.
Native cron, trajectory, skill, task, approval, action and gate holders supply
detached typed ProblemSource ledgers; task diagnostics use the already-created
runtime store through an atomic pointer, never initializing a second board. Cron
jobs and arming are read atomically without retaining prompts or targets.

Fleet recordings use list(50); owner reads query only admitted recent 20 sessions
with per-session list(10), cap 50 before event IO, verify returned session/owner
metadata and request tool_use events with a 200-event budget. All other tool uses
consume that budget. A total 64 MiB event-byte budget bounds wire decoding and
returns ObservationLimitError; raw JSON is transient ingress only. List/event
faults and panics retain class-only failures; event faults preserve trend results.

Memory raw stores retain exact full diagnostics. Fixed-owner bindings retain only
known-owner write/replacement diagnostics in separate bounded logs. Unreadable
shared files cannot establish an owner and are fleet-only. This intentionally
corrects source ScopedMemory's shared problems delegation; bindings do not share
observations merely because they have the same owner. Unattributed errors from
per-owner resource stores are not yet available in fleet collection. Existing
cron/trajectory/skill/task counters retain their native machine-sized storage;
the adapter does not claim to remove those overflow limits.

Snapshot 96 constructs actual Python SessionManager/AgentSession instances for six
empty, mixed activity/owner, unknown-owner and 100-session cap profiles. Reports
match exactly with no normalization. Native tests cover actual JSONL turns, owner
admission before event IO, 20/10/50/200 budgets, cancellation, panic/error isolation,
concurrent snapshots, lazy task faults and unattributed-memory filename omission.
This observer slice alone adds no HTTP acceptance; the route binding below has
its own corpus. Model-tool and other migration rows, including stored persistence,
teams and workflows, remain open.


## Self-audit HTTP composition

The three GETs now bind actual manager observation and the existing pure report,
objective and draft processors. Configured authentication selects the already
admitted principal and excludes fleet ledgers/cron; open deployments use the
unscoped operator view. Query/body fields do not override owner, global inclusion
or the fixed default curation limit. GETs consume no rate budget and do not launch
turns, schedule jobs or admit benchmark tasks. Existing ingress caps and global
authentication precede handlers; query tokens remain event-route-only.

ObserveSelfAuditProblems uses a private finite collection mode to skip activity/
Info, cron overview and trajectory list/visitor IO. It reads only existing ledgers
and treats a failed collection as an error, not an empty suggestion set. Report
mode retains independent visible class-only section failures. Curation failures
return a private plain 500; typed JSON responses use the shared fail-closed
projection. Native plain report masking is applied before writing, with the source
Unicode cap/marker reapplied after replacements that can expand text. This extends
source self-audit masking while preserving the native recording boundary.

Snapshot 97 compares 72 actual source HTTP responses (open, authenticated, private
owner-resource memory): scope, headers, report strings, objective/draft structures,
ignored malformed bodies/query overrides, bearer/query authentication and wrong
methods. Only generated session IDs are replaced with labels. Native tests cover
real no-rate semantics before/after exhausted turn budget, body cap before auth,
no recording IO/launches, shared-memory filename omission, projection panic/private
errors, Unicode report cap and mask expansion. HTTP inventory is 37 operations/
34 patterns versus source 44; seven improvement/team/workflow operations remain
missing. Source shared-memory diagnostic delegation remains intentionally corrected
as documented above, and model-tool installation remains open.

## Self-audit model-input contract

Snapshot 98 captures the actual installed Python tool's schema, read risk,
readonly/exclusive traits, absence from the default registry and five keyword
admission outcomes. The native closed input union now recognizes self_audit,
with an empty concrete payload and detached schema. Unknown fields, including
owner/global/limit overrides, cannot survive decoding. Canonical replay input,
masked recording copies and provider tool-use blocks retain its discriminator.
The schema matches source exactly; it does not install the tool. Runtime handler,
manager visibility policy, launcher selection and child propagation remain open.
This protocol step does not change the HTTP inventory or enable comprehensive
features. The Python model handler's unscoped manager report must be reconciled
with native owner authority at trusted composition, never via model arguments.

## Bound self-audit model tool

The optional SelfAuditTools runtime/manager flag now installs the source schema
and read-risk/readonly/exclusive traits through the common gate. A typed observer
and finite trusted view bind collection to the session owner by default; explicit
operator construction preserves the source unscoped manager report. Standalone
--self-audit-tools selects owner view under configured auth and operator view when
open. Authenticated model-tool scope intentionally corrects Python's unscoped
handler. The model cannot override scope. The shared manager construction map
covers new/restored/forked runtimes; selected children inherit the bound view,
while default role capabilities remain unchanged. Bare activated sessions retain
the source no-manager notice. Cancellation and private panic containment surround
collection, which does not acquire the active turn lock. Existing result hooks,
masking and journal semantics remain in order.

Snapshot 98 now includes the actual source no-manager result. Native model loops
compare it and the source empty-manager report from snapshot 96, then verify
owned/operator diagnostics during an active turn, default-off/selection removal,
readonly traits, child binding, guard-before-observation, result masking and local
HTTP-provider launcher execution under both auth modes. No HTTP inventory change;
G0–G7, comprehensive features, persistence and other gaps remain open.

## Improvement acceptance-instrument checks

Snapshot 99 captures three actual source classifier profiles and ten actual
fingerprints: empty/missing/NUL roots, changed bytes, renaming, all four globs,
Unicode/binary data, hidden files, directory omission, file/broken/looping symlinks
and source read-failure marker. Go improvement.VerifierTouches preserves case,
order, duplicates and substring rules. VerifierFingerprint returns a concrete
comparable 16-byte value with the source 32-hex JSON/text projection. It hashes
relative paths and full bytes without separators, preserving pattern order and
sorting within each glob. Literal workspace metacharacters remain literal.
Permission errors in glob enumeration are ignored, stat permission errors abort,
absent/loop paths are skipped and failed reads contribute <unreadable>. Other
scan IO failures abort. Native tests exercise mutation/restoration, detached touch
results and explicit filesystem faults; malformed filename encoding is tested via
a detached entry because APFS refuses creating such filenames.

These root-relative globs cover only tools/verify_*, .github/workflows/*,
conftest.py and tests/conftest.py; they do not scan every path flagged by substring
classification or arbitrary nested verifier directories. Fingerprint sampling
at acceptance judgment remains required in the future verified loop. This
library installs no runtime/model/HTTP feature. HTTP inventory remains 37/44;
verified loop, proposal flow, both improvement operations and
G0–G7 remain open.

## Improvement archive append

Snapshot 100 runs the actual Python ImprovementArchive.record with fixed UUID
and clock: missing/null fields, populated Unicode fields, false/zero/empty values,
explicit empty and default anonymous owners, parent lineage, secret masking of
values and keys with last-key collisions, and best-effort root/open failures.
Go Record preserves the twelve-field JSON schema and allocated raw ID even if
its stored value is masked. ProposalFields admits the known producer schema;
summary/next/unknown inputs are omitted. JSON formatting is semantically compatible,
not byte-identical to Python's ASCII/spaced output. Native tests exercise concurrent
complete appends, prior-row preservation, detached projection, failure before IO,
private masking panic refusal and ID shape. Constructor performs no filesystem IO.
The lock serializes one instance only; this index is not a transactional artifact.
Legacy reading and owner filtering are now implemented as the next slice below;
runtime/HTTP binding remains pending. HTTP inventory stays 37/44 and G0–G7 remains open.

## Improvement archive compatibility reads

Snapshot 101 compares 27 actual Python ImprovementArchive.list profiles: unknown
fields, non-object rows, source newest-first order, malformed/blank lines, exact
owner and empty owner, accepted-row limit accounting, zero/negative and default
200 limits, duplicate-key last value with first-position order, Python Unicode
splitlines, BOM skipping, arbitrary-width integers up to 4300 digits, rounded
floats and nonfinite values, lone-surrogate escapes, >64 KiB rows, missing/IO
failures, strict UTF-8 failure and 500/1005-depth samples. Canonical per-row digests
compare complete source values, including large rows and unknown nested fields.
Go ArchiveQuery and private-field ArchiveValue variants contain no any or raw JSON
service state. Accessors detach slices and expose explicit text/integer/float/
boolean variants. New proposal admission remains ProposalFields. Historical rows
are not remasked; the source reads the whole file, with no imposed byte cap.
Scoped non-object rows abort the whole query, like source AttributeError, unless
the accepted-row limit was already satisfied. IO failures yield [] while UTF-8
and conversion failures propagate. Nonfinite values survive library reads but
MarshalJSON refuses them, matching the future standard HTTP serialization boundary.
Go accepts 1000 nested containers; Python's precise call-stack-dependent recursion
cutoff is not established by the 500/1005 samples. Runtime/HTTP binding remains
pending; HTTP inventory stays 37/44 and G0–G7 remains open.

## Improvement lineage HTTP reads

Snapshot 102 compares 46 actual source responses across open and authenticated
deployments: alice/bob/fleet lineage, query/body overrides ignored, missing/bad/
query-only credentials, wrong methods, malformed line skipping, missing/directory
IO results, invalid UTF-8, arbitrary legacy scalar rows, owner type mismatches,
duplicate owner keys, nonfinite/lone-surrogate response failures and filtering
foreign malformed-response values before serialization. Finite float/bigint values
and source default 200 rows also match. The cap fixture retains a complete response
digest rather than expanding 400 repetitive rows. Historical strings are returned
without remasking, including a value registered in the current secret registry.

Manager construction owns a fixed .improvements archive without startup archive
IO. ListImprovements takes a trusted optional OwnerID; HTTP derives it solely from
Authenticator.Configured and the admitted principal. Serialization finishes before
headers; read and response faults use the source private plain-text 500. Typed
ArchiveValue still retains lone surrogates through read accessors, but MarshalJSON
now refuses them as well as nonfinite numbers at the source HTTP boundary. Native
tests also verify request byte admission, GET rate neutrality, cancellation, no
provider/session work and constructor absence. Inventory is now 38 operations/
35 patterns versus source 44. Six POST-improvement/team/workflow operations, native
persistence and G0–G7 remain open.

## Pure verified-loop contracts and folds

Snapshot 103 compares 32 actual verified_loop.py folds and refusals: empty patch
revision increments, pending/blocked/untrusted changes, nine verdict/integrity
pairs with matching/nonmatching coverage, one usable receipt among others, stale
CAS, contract revision mismatch, unused foreign receipts, unknown status and
missing IDs, typed artifacts/facts, duplicate blocker first removal, atomic refusal,
and duplicate/extra checkpoint requirements. Hash and canonical identities match
source byte-for-byte, including sorted keys, spaced JSON, Unicode/HTML characters
and tuple projections. Hash deliberately excludes allowed surfaces, persistence
and contamination rules; it is not a signature. Source permits extra checkpoint
IDs and duplicate state IDs; first StatusOf and last-value fold behavior survive.

Go TaskContract/Checkpoint/Receipt/RoundPlan retain private snapshots with detached
constructors/accessors. Five Operation constructors form a closed union, replacing
open patch tuples. Native tests also prove replay, input/result detachment, source
defaults, Unicode whitespace refusal, invalid constructors and no revision wrap.
Native revisions are signed 64-bit, and canonical identity refuses non-scalar
UTF-8 text; arbitrary-width counters/non-scalar Python canonical text are outside
this native profile. ApplyPatch is pure; executor, acceptance command, integrity
probe and session service remain pending. HTTP stays 38/44 and G0–G7 remains open.

## Verified-loop execute / accept / fold coordination

Snapshot 104 runs the actual Python VerifiedLoopService over explicit worker,
CommandResult, probe and event seams in 22 scenarios: first success, repair after
feedback, prose-only failure, None exit, timeout, source overflow/error-plus-zero
exit behavior, tampered/restored instruments, nil baseline/current samples, zero/
negative/default round counts, Unicode prefix/tail limits and effect/event errors.
Outcomes, canonical checkpoints, full receipts, objective feedback and exact effect/
event ordering match. The source success predicate is exit zero, not timed out and
not tampered; overflow/error flags do not independently change that predicate.
Successful later restoration yields clean final integrity while retaining earlier
suspect receipts. A nil initial sample disables later probes, matching source None.

Go Service uses typed Worker/AcceptanceRunner/IntegrityProbe/VerifiedEventSink seams
and a closed round/receipt/checkpoint telemetry union. ShellAcceptance and
WorkspaceIntegrity reuse actual workspace shell and instrument effects. Native
tests prove actual command execution, tamper-before-acceptance refusal, restored
second-round success, restoration during acceptance still refused, cancellation
without late acceptance, private panic refusal and detached exit-code telemetry.
Command/sink/probe errors abort without a successful outcome. Rune limits match
source characters. Source-bound 64-bit/scalar text profile remains explicit.
Runtime session/worker/approval/event binding, proposal Git/POST, native persistence
and G0–G7 remain open; HTTP stays 38/44 (35 patterns). This is an explicit operator
coordinator, not a registered runtime feature or a claim of live-model convergence.

## Managed verified task binding

RunVerifiedWithContext composes the coordinator with actual managed admission,
cancellation, worker role/subagent derivation, structured command execution and
typed session events. The operator acceptance command uses the bound executor
directly, matching Python; worker tools retain role/permission/approval hooks.
The existing snapshot 104's event payloads round-trip through the archival adapter
without wire changes. Native integration tests run a real child write and shell
acceptance, inspect peer authority and owner/workspace binding, deny the child
write, cancel a parked child before acceptance, refuse concurrent managed turns,
reuse the session after cancellation, and stop on foreign leases at round, worker
end and checkpoint boundaries. Stored events preserve masking and typed values.
The lease check at effect/event/return boundaries is an explicit native embedding
guard; it is not a heartbeat or proof of process fencing. Parent transcript does
not acquire child messages. Source live-model convergence is still untested.
HTTP remains 38/44 (35 patterns); Git proposal composition/POST, team/workflows,
native persistence and G0–G7 remain open.

## Git proposal composition and managed lineage

Snapshot 105 executes actual Python propose_improvement and its actual verified
coordinator across 17 scenarios: successful/unchanged/unverified proposals,
failed add with a subsequent commit attempt, failed/nil-exit commit fallback,
ordered/repeated/renamed verifier paths, Unicode splitlines/truncation, archive
owner/parent/null-parent, parent without archive, zero rounds, blank acceptance,
non-Git refusal and command/archive/event exceptions. Full proposals, command/
event order, archive producer fields and improvement_proposed payloads match.
Git diff/branch stdout remains source-consumed without an added exit policy.
Fixed Git command text is retained; model objectives are not interpolated.

Native selfimprove.Service supplies typed proposal/event/lineage values, private
callback panic refusal, context cancellation and real Git/structured-shell adapters.
Real temporary checkout tests verify branch commits, clean status, commit message,
diff stats and owner-scoped lineage, including unverified-but-committed attempts.
Managed ProposeImprovementWithContext holds admission across verification, Git and
archive, uses the session owner, and preserves worker gates and event projections.
Cancellation at Git status refuses competing turns and stops before stage/commit.
Archive IDs retain the source best-effort index contract; no merge occurs. Trusted
callers must provide isolated workspaces because source stages all changed paths.
Proposal POST, teams/workflows, native persistence and G0–G7 remain open; HTTP
stays 38/44 (35 patterns). Source live-model convergence remains unverified.

## Owned improvement proposal POST

Snapshot 106 runs actual FastAPI admission against an observed proposal effect
seam in 41 scenarios. It compares complete responses for defaults, ignored extras
(including nonfinite ignored data), absent/text/JSON content types, malformed/
empty/non-object bodies, missing/type/length failures, optional parent values,
integer/string/boolean/float max-round coercion and range errors, foreign/missing
sessions, auth, busy state, blank command/non-Git admission and private failures.
Source request validation precedes owner lookup; busy precedes logical service
admission. The native manager binds its archive/session owner and atomically
refuses busy admission. Success responses retain the source direct projection.
Only retained diagnostic values trigger nonfinite/depth serialization failure;
ignored fields cannot poison a valid model. Existing JSON transport profiles are
shared; canonical service text retains the previously declared scalar profile.

The route uses the existing session execution workspace, creates no worktree,
and spends no rate budget as in source. Native tests verify real TCP -> actual
worker write -> acceptance shell -> Git commit -> owner-scoped archive reads;
schema/refused callers cannot reach the provider, and the message rate budget
remains available after proposals. HTTP now registers 39 operations/36 patterns
against source 44. Team/workflow route groups, native persistence and G0–G7 remain
open; source live-model convergence is unverified.


## Native team protocol coordinator

Snapshot 110 compares 25 actual manager recipes/117 operations over real member
sessions and mailbox IO. A trusted construction fixture installs their roster;
this is not native/source spawn or lifecycle evidence. UUIDs and wall timestamps
are fixed; actual methods perform handshake publication, delivery, consumption,
response matching, review and pruning. Compact result hashes and exact indentation
state/render hashes retain full untruncated plans/feedback, original row order,
Unicode, nonfinite/surrogate values and source exception/ledger outcomes.

Go implements both handshake types, explicit status/member variants, a global
200-entry table, detached snapshots and source resolved-before-pending eviction.
RequestPlan refuses long instructions; result delivery truncates by Python code
points and records refusals. Publication/resolution happens before IO; failed
writes retain state. Consuming clears first, can partially acknowledge/resolve
before malformed-row failure, and returns the shutdown assignment outcome even on
error. Response matching retains source request ID/type/status correlation, including
the cross-sender source profile. Source type/truthiness/str projections are tested
for sixteen closed values. Unicode printable tables now pin Python 14.0.0, correcting
a proven Go-newer Unicode mismatch in nested response repr/diagnostics.

The coordinator is now manager-owned; ten explicit team tools cross the existing
gate. Default HTTP GET remains non-consuming. Native initial member construction,
resource inheritance and prompt identity are implemented as described below. Automatic inbox injection and owned shutdown/idle/task lifetime are implemented below.
Fleet diagnostics remain open with the other G0–G7 packages.

## Actual source team lifecycle corpus

Snapshot 112 captures actual Python spawning, then 8 injector and 11 idle recipes.
Model turns, mailbox/task effects and clocks/sleeps use the real source path. Prepared
worktree directories exercise selection rather than Git branch creation. Initial
resource sharing, exact prompts/results, fresh peer provenance and partial effects are
retained with normalized IDs/paths. This corpus alone does not prove Go idle/injection
binding; native differential consumption is the following implementation checkpoint.

## Managed team tool effects

Snapshot 111 compares eleven actual installed Python handler recipes over real
manager/member sessions and mailbox IO. The explicit trusted roster seam is not
spawning/lifecycle evidence. Displayed request IDs and created_at timestamps are
normalized; full returned text, metadata ordering, refusals, lead restrictions,
shutdown assignments and protocol counts are compared. The corpus includes empty
teams, unavailable surfaces, Unicode/quote recipient diagnostics, routed messages,
broadcast recipient exclusion and first-three refusal summaries, plan review,
shutdown acknowledgment, partial consume fault and nil/empty metadata.

Ten exact source schemas and risk/readonly/parallel traits are installed only with
TeamTools; source read_inbox is read-only despite effects. Native gates additionally
prove rewrite/masking/replay, foreign/stale replay admission, live readonly modes,
selection, fresh-team forks and delegated-child unavailable state with rebound guards.
Standalone --team-tools selects this slice independently; MINILOOP_FEATURES remains
unsupported. A real HTTP/direct-provider round publishes ten tools, sends and reads
through the gate and proves repeated GET leaves messages available for delivery.
Native SpawnTeammate now creates an owner-bound member sharing the original lifecycle
workspace, fixed parent skills/memory/resource bundle and shared task board. Fresh manager
model/system-builder and interactive mode, teammate prompt identity and named peer
capability dropping follow the inspected Python manager.py spawn/initial-run contract.
Name reservation, construction rollback, concurrent initial result delivery, delete/stop
cancellation and joins are tested natively. Children omit recursive spawn; parent deletion
leaves registered members and bound roots survive last-member deletion. Snapshot 112 adds actual Python spawn evidence with 8 injector and 11 idle-loop recipes,
including cleaned versus raw JSON prompts, exact model input/run provenance, result
metadata, task effects, shutdown priority, partial faults, overflow, invalid sender,
missing/invalid/existing worktree selection and polling across the deadline. The initial
source idle task is cancelled before each explicit probe; scheduling/model/mailbox/task
behavior then runs through the real source implementation, without clock/run stubs.
Native round injection checks live core identity before IO, emits a typed archival count
and retains shutdown assignment on partial failure. Delegated subagents do not consume
parent mail. The owned worker now runs the source idle loop after initial delivery,
clearing shutdown before messages/tasks, resetting deadline on activity, claiming tasks
exclusively and rebinding only the execution workspace. Task board/lifecycle root remain
fixed. Fresh autonomous contexts drop all human grants. Delete/stop join idle-started
model runs; shutdown/timeout leaves the handle registered. Launcher settings bind the
1s poll / 60s timeout defaults. Task/worktree model tools remain separately selected.
Fleet diagnostics, process restart teammate restoration and full feature activation
remain unimplemented.

## Typed team/protocol inputs

Snapshot 109 executes actual installed Python team tools with no manager to
capture ten schemas, readonly/risk/exclusive traits and 74 keyword binding cases.
Accepted source inputs retain exact sorted compact/spaced Unicode identities and
registered-secret structural masks. Fixture input_json preserves original member
order through the otherwise sorted snapshot envelope; this is required for masked
key collisions. It proves the keyword/encoding contract, not team execution.

Go has concrete variants/constructors/accessors for all ten tool names, object-only
immutable metadata, detached optionals, absent/null metadata recording, false/empty
values, recursive sorted hashing and key/value masking. Tool-use blocks round-trip
through the existing provider/storage decoder. Native malformed-type tests add
strict scalar/container validation, nonfinite/surrogate metadata refusal, duplicate
last-value semantics and foreign identity/root rejection. Python's handlers do
not enforce all schema types; Go's stricter typed boundary is explicit. The source
metadata schema {type:object} is retained exactly and null schema controls still
fail. Default tool count remains ten. Team tool registration/common-gate effects,
teammate lifecycle, automatic inbox/protocol processing and scheduling are pending.

## Bounded team mailbox and owned view

Snapshots 107/108 cover the actual MessageBus and default team route. The bus
compares 32 memory/file recipes with exact result strings, ordered historical
rows, persisted-byte hashes, file existence and problem ledgers. These include
send size boundaries, queue shedding, 99-message overflow delivery plus notice,
injected legacy memory overflow, repeated peek, malformed/non-object records,
Unicode line separators, duplicate keys, integer parse limits, strict versus
seek-boundary UTF-8, nonfinite/surrogate values, masking before escaping, extension
fields and safe-name refusal. Both runtimes now reject dot path components;
Python's former regex accepted path navigation. A mutation guard pins this fix.
The shared immutable Python JSON value implementation retains the archive API.

The default manager gives every created/restored session its own lead identity;
a fork creates another one-member team. GET team admits owner before file reads,
peeks at most 100 and returns newest 50, spends no rate budget and adds no second
mask to historical rows. Eleven actual HTTP source outcomes compare in native
handlers; the custom-agent teamless outcome compares the nil-identity response
projection and has a native manager unit test. Real TCP viewing preserves all
75 delivered messages, and unreadable foreign data still returns 404 before IO.
Locks serialize one bus instance, not multiple processes; the trusted root is not
a filesystem sandbox. Source recursion boundaries depend on call stack; this
shared parser retains the declared 1000-container compatibility profile.

HTTP now has 40 operations/37 patterns against source 44; four workflow routes
remain. Teammate construction/lifecycle, lead/member model tools, inbox/protocol
injection, autonomous task/idle loops, prompt identity, source diagnostics binding,
MCP, native SQLite and the full G0–G7 release audit remain open.

## Source full-feature composition contracts — 2026-10-09

`python/tools/feature_bundle_contracts.py` captures actual Python registries and
manager construction into `go/testdata/python-feature-bundle.json`: 14 registry
cases, eight manager cases and 57 shared concrete definitions. The full bundle
contains 48 tools in Source order. Custom registries retain their ten tools; an
explicit empty injector list removes background/team default injectors. Workflow,
AST and decision selections independently extend the bundle to 51, 52 and 49
tools (56 together). Bash classification changes only with background enabled.

The AST path and digest select inventory only; no AST command or model request is
executed. Two focused tests verify deterministic exports across separate roots,
ordered composition, override behavior and per-call bash classification. This
fixture establishes Source evidence, not Go full-bundle parity. Go still rejects
`MINILOOP_FEATURES`: default Python-syntax parsing, catalogue ordering and
injector override semantics need implementation before activation. Runtime
topology, authority, persistence and defaults are unchanged; the README map was
reviewed and its baseline updated.

Validation: focused contracts 2 passed; full Python suite 2,157 passed, 28
skipped and 24 subtests passed; exporter `--check` current for all 187 files;
`verify_scans.py` anchors all 19 scanning guards. Full Go tests with
`-p=1 -count=1 -timeout=300s -coverpkg=./...` and `go vet ./...` pass.
`git diff --check` passes. Go runtime and Python package modules were unchanged;
race, invariants and guarded-mutation checks were not rerun for this fixture slice.

## Go built-in feature publication order — 2026-10-09

`orderBuiltinDefinitions` orders built-in catalogues by Source full-registry
groups, followed by independently selected decisions, workflows and AST tools.
Sorting precedes capability filtering; extra tools keep their relative order
after the built-ins. Explicit custom catalogues skip this ordering step. No
feature is automatically selected and no handler, authority or permission is
changed.

An actual native Manager with twelve explicitly selected groups matches all 48
Source tools in order, concrete schemas, risks, readonly/parallel traits,
capabilities and bash classification. Diagnostics is selected with an
inventory-only provider that errors if called: no native parser is claimed.
All 14 Source registry and eight Manager catalogue recipes also test ordering
from reversed publication input. A separate actual custom Manager preserves its
caller-supplied two-tool order without widening. The overall feature setting
remains refused; default Python-syntax parsing and injector override semantics
are still required.

Validation: focused contract and its race run pass; full Go tests and full race
tests with `-p=1 -count=1 -timeout=300s` pass, as does `go vet ./...`. Race
linking reports existing nonfatal macOS CGO LC_DYSYMTAB warnings. Architecture
delivery passes 9/9 with zero errors/warnings and exact specification/HTML
bytes and hashes verified; browser visual inspection remains skipped after the
earlier denial. README Mermaid/outline and `git diff --check` reviewed. Python
code/exports were unchanged in this slice; their full suite/export/scanning gates
passed at acb2646 and were not rerun. Coverage was not remeasured after the new
ordering helper.

## Explicit lifecycle injector overrides — 2026-10-09

Source Manager uses a supplied injector list even when empty; otherwise it selects
background/team defaults for the full feature profile. Workflow and core controls
are appended independently. Go previously injected background/team before every
custom list and collapsed empty lists into nil during fleet/child copying.

`cloneInjectors` now preserves nil versus an explicit empty list through Manager,
Runtime and delegated child construction. `injectMessages` selects background/team
defaults only for nil; explicit lists execute their supplied entries in order.
Workflow inbox remains afterward, followed by the existing steering/posture and
runtime-fact pipeline. No tools are removed by selecting custom injectors.

Native tests queue an orphan ledger entry and actual team mail, run a real managed
model turn with nil/empty/custom lists, compare request contents and delivery
events, and verify overridden queues remain untouched. Restoring nil for a later
turn delivers both pending observations in background/team order. Child snapshots
preserve empty/custom selection. Existing actual-Source workflow-inbox and child
loop-extension tests cover independently appended workflow/custom injection.

This closes the explicit-list override gap for future full-feature composition.
Go full startup selection still requires a default Python-syntax parser. No new
dependency or complete feature profile is introduced in this slice.

Validation: focused native injector/extension tests, full Go tests and full race
tests (`-p=1 -count=1 -timeout=300s`) and `go vet ./...` pass. Existing nonfatal
macOS CGO linker warnings occur in race linking. `verify_guards.py -k
injector-return-unchecked` catches its mutation (1/1). An extra full 378-guard
Source sweep was intentionally interrupted while an unrelated foreground-cancel
mutation waited; it is not complete or claimed passed. Its remaining probe sleep
was terminated. Python runtime/exports are unchanged; full Python/export/scans
were not refreshed. Architecture passes 9/9 with zero errors/warnings and exact
receipt hashes/bytes verified; browser visual inspection remains skipped after
the earlier denial. README outline and `git diff --check` pass. Coverage was not
remeasured.

## Actual Python syntax-parser profile — 2026-10-09

`python/tools/python_syntax_contracts.py` exports 62 actual `ast.parse(source)`
observations to `go/testdata/python-syntax-parser.json`. The profile records
CPython 3.11.3 explicitly, base64 UTF-8 with surrogatepass input, readable escaped
source, clean/syntax/non-syntax disposition, error class, line and message.
Coverage includes current grammar, unsupported 3.12 grammar, indentation/tabs,
f-strings, delimiters, assignment targets, numeric literals, Unicode and NUL.

The captured Source accepts module-level return/break/continue/await/nonlocal,
duplicate function arguments and call keywords, late/unknown future imports.
Compilation would reject some of these: a compiler cannot replace ast.parse.
NUL raises ValueError in this actual 3.11.3 profile, and a directly supplied lone
surrogate raises UnicodeEncodeError; both are parser faults, not clean results
or SyntaxError diagnostics. File scanning replaces malformed UTF-8 before
parsing, so the direct surrogate case documents the parser seam, not a claim
that file reads produce lone surrogates. Later CPython profiles require explicit
contract refresh rather than assuming error-text compatibility.

This is actual Source evidence and conformance input, not a Go grammar or parser
implementation. The default parser and full startup setting remain incomplete.
A user decision is pending on allowing the existing CPython 3.11.3 read-only
parser backend versus requiring removal of the Python runtime. The latter
requires a native parser rather than approximate syntax checks. No new runtime
or dependency has been introduced; README architecture was reviewed and its
baseline updated, with unchanged topology/authority/defaults.

Validation: 62 observations comprise 24 clean, 36 syntax failures and two
non-syntax faults. Focused tests: 2 passed; final full Python suite: 2,159 passed,
28 skipped, 24 subtests passed. Exporter `--check`: 188 current files. Source
scan verification anchors all 19 guards. Full Go tests
`-p=1 -count=1 -timeout=300s ./...`, `go vet ./...`, README outline and
`git diff --check`: pass. An earlier Python run read the old fixture filename
while it was renamed; the final full run above validates the final paths.
Go runtime and Python package modules were unchanged; race, invariants and
mutation guards were not rerun for this corpus slice. Coverage was not refreshed.
