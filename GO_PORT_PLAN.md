# Go port plan

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

## 2026-10-09 approved SQLite driver prerequisite

User explicitly approved mattn/go-sqlite3 v1.14.52 on 2026-10-09. The Go
module now pins that exact version and records its module checksums. Go remains
at 1.23; the local validation toolchain is Go 1.23.3 on darwin/arm64 with CGO
and clang. This supersedes the pending approval recorded in the historical
2026-10-06 driver decision below.

An isolated temporary Go probe imported the actual driver and opened an on-disk
database. It reported SQLite 3.53.4 and passed WAL selection, transaction rollback,
transaction commit, independent-connection visibility and close/reopen checks.
This is driver prerequisite evidence; the native StateStore schema/migrations,
records, leases and Manager wiring remain unimplemented. The dependency is
currently indirect because production code does not import it yet.

Validation: go mod verify, go vet ./... and
go test ./... -p=1 -count=1 -timeout=300s passed. git diff --check passed.
No runtime source, topology, authority or default behavior changes in this slice.
No push. Python tests, race suites and coverage were not refreshed for this
module-only change.


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

## 2026-10-09 coverage refresh after response policy delivery

Measured implementation baseline: `6feeba2`. Full cross-package coverage run passed:
`cd go && go test ./... -p=1 -count=1 -timeout=180s -coverpkg=./... -coverprofile=/tmp/mini-loop-current-coverage.out`.
Repeated profile blocks were deduplicated by source location, with execution counts
combined by maximum; statements count once across test packages.

| Scope | Covered / total statements | Coverage |
| --- | --- | --- |
| All Go packages | 25,648 / 28,448 | 90.16% |
| agent | 8,183 / 9,129 | 89.64% |
| efficiency | 1,349 / 1,481 | 91.09% |
| httpapi | 1,613 / 1,802 | 89.51% |

This is statement coverage, not Python-to-Go port completion. Python coverage and
mutation coverage were not refreshed. Evidence: Darwin arm64; local execution.

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

## Token-efficiency migration slices — 2026-10-09

Review baseline `bcaeca9` plus native benchmark post-construction restriction:
typed observation/request/response services, shared lifecycle, Manager/child
bindings and Launcher/HTTP/fake benchmark mode/style settings are installed.
E6.4 supported composition is delivered; full Source token_efficiency.py fidelity,
including broader wire/SPI/custom profiles, still requires a coverage audit.

- E0 (native foundation delivered; broader wire/SPI profiles open): named immutable component descriptors and optimization receipts, closed stage/
  mode/status/lossiness/cost types, bounded warnings, unbounded integer metrics and
  Source-safe event projections. Capture constructor/receipt behavior before wiring.
- E1 (native store/recovery tool and managed/default lifecycle delivered; broader profiles open): scoped masked raw-artifact memory store, opaque refs, trusted monotonic TTL,
  per-item/total capacity, integrity checks, discard/sweep/clear/close and tools.
- E2 (observation library and typed Agent masking/adoption delivered; broader profiles pending): observation reducers and runtime off/shadow/enforce behavior; mask-before-input,
  single ownership, no inflation, recoverable storage and fail-open fault receipts.
- E3 (request library and typed Agent protocol/ledger adoption delivered; broader profiles pending): detached typed request projection, frozen-prefix/identity/envelope guards,
  optimizer admission, budgets and cache-order/transcript preservation.
- E4 (response library, Agent adoption and selected default composition delivered; broader profiles open): stable typed response settings and concise policy; bounded instructions,
  existing output-cap/context-budget preservation, shadow and fault behavior.
- E5 (three-stage registry/lifecycle and session/Manager/child adoption delivered; broader profiles open): shared component identities, serial admission, initialize/health/close once,
  draining/deadlines/retry, per-session clone/store binding and contained failures.
- E6 (E6.1 response, E6.2 request, E6.3 observations and E6.4 supported defaults/lifecycle delivered; broader profile audit open): agent/manager/launcher/benchmark/HTTP composition, events/recording, independent
  environment selection and actual Source end-to-end feature-combination tests.

Each slice retains the overall port objective and gets a separate validated commit.
E0 alone will not activate the runtime or remove unsupported settings; completion
requires the admission, protection, lifecycle and runtime consumers above.

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


## 2026-10-08 workflow publication retention pins

Baseline b4aac9e. Store and ServiceViews now expose PruneTerminalRunsExcept
with named RunID exclusions. Native service tasks can retain their whole graph
until terminal event/outbox publication has joined. Unpinned records retain the
source terminal-and-drained predicates, ordering and default limits. Duplicate or
unknown pins are harmless; releasing a pin permits normal cascade cleanup and
removes parallel launch-turn bookkeeping. This is process-local retention only.

Validation: focused retention tests and race tests passed, including whole-graph
pin/release and bookkeeping cleanup. Runtime topology and feature defaults are
unchanged; no new source fixture or dependency. Owned service composition remains
the next unit. README architecture baseline reviewed in this commit.


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

Baseline d593d14. W3C2 adds named TerminalRunLimit, MaxTerminalRuns (500) and
PruneTerminalRuns. Nil keeps the default; explicit negative limits preserve source
all-eligible eviction while avoiding native minimum-int subtraction overflow.
Eligibility is terminal status plus no undelivered outbox, independent of lease age
or node status. Timestamp/ID ordering determines oldest eligible runs. One mutex
covers selection and complete removal of run/node/attempt/artifact/outbox records,
outbox/launch keys and native attempt/outbox insertion indexes. Cascading scans
each owned map once rather than once per evicted run; survivor order is preserved.
Definition/hash indexes remain reusable. Parent references in other runs remain
historical metadata; ownership is exact run_id, not recursive parent traversal.
Evicted launch keys start a fresh run on replay; retained keys still reject changed
payloads. Returned IDs let a future service clear its parallel launch-turn bookkeeping.
No automatic service pruning hook or live workflow runtime is installed yet.

Snapshot 120 builds graphs using actual source public methods, then applies explicit
status/node/notice/time seeds. Sixty-five profiles compare full run/node/attempt/
artifact/outbox/launch/outbox-key/definition/hash projections, pruned ID order,
repeated cleanup and retained/evicted replay/conflict outcomes. A source 501-run
profile verifies the default 500 bound and launch-key removal; stable-ID thin records
make equal timestamp ordering observable without UUID noise. Native tests additionally
cover both insertion indexes, getter behavior after eviction, minimum native limit,
and concurrent acknowledgment/pruning without removing unread or active runs.
Focused workflow race passed; no dependencies or Python runtime modules changed.

Validation: `go test ./... -count=1 -timeout=180s`, `go vet ./...` and
`go test -race ./... -count=1 -timeout=180s` all passed, alongside focused workflow
race. All 120 source exports are current (dependency deprecation warnings only);
all 19 scan guards are anchored. Full Python regression passed: 2,155 tests,
28 skips, 24 subtests and 3 dependency deprecation warnings in 97.41 seconds.
`git diff --check` and README outline passed. Python package invariants/guard checks
were not rerun: no package module or guarded Python runtime behavior changed.
Architecture regeneration passed all 9 checks, zero errors/warnings. Visual review
remains skipped after the previous access denial; no visual approval is claimed.
Specification SHA256: `1adfda0ba39597381ea5a3a570f7eb1215b863348ba0bdad1edad54cc97f66e2`;
HTML SHA256: `5d49f8b0bb29415c8647c0bae3ef1d92a7f0005141e40fefd88a241a24c0ce96`.
README canonical
runtime topology reviewed: this remains an uninstalled process-local library.
Overall coverage was not refreshed; the prior 90.47% statement figure belongs to
d696498 and does not establish current coverage or migration completion.

Next W4-W6 ports trusted live origins, scheduler/engine/controller workers, workflow
service/tools/HTTP/parent notification append and manager activation. MCP, native
session SQLite, runtime profiles and full G7 remain open. The complete Python-to-Go
port is unfinished; no durable workflow backend or restart-resume is inferred.

The current source structure narrows the next implementation sequence:

- W4A: lower WorkflowRunner and engine `_inputs_for`, `_execute_locked` and
  `_execute_attempt` into closed input/submission records, then differential
  scheduling, attempt-semaphore, cancellation and settlement contracts.
- W4B: adapt FreshAgentRunner's isolated read_file/glob/return_artifact catalog
  and live context_resolver; decoded runmeta snapshots must not mint authority.
- W5: port owned launch/status/wait/cancel and parent notification append/receipt
  flow, including pruning returned IDs from parallel launch-turn bookkeeping.
- W6: install optional workflow service/tools/HTTP in manager composition with
  truthful defaults and cross-language end-to-end evidence.


## 2026-10-08 workflow outbox enqueue, leasing and settlement

Baseline bb726d1. W3C1 adds named EnqueueOutboxInput, ClaimOutboxInput, OutboxLease
and SettleOutboxInput with EnqueueOutbox/ClaimOutbox/AcknowledgeOutbox/ReleaseOutbox.
The store tracks outbox insertion order for claim selection; both explicit enqueue
and final completion add that index. Shared response sorting preserves created_at/ID
order without changing limited insertion-order selection. Enqueue checks kind before
run lookup, then run/kind deduplication before new immutable object payload admission.
Existing completion messages retain their original payload and delivery projections.

Nil run filters mean any run; explicit empty slices mean none. Nil lease duration
selects 30 seconds; nil limit is unbounded, nonpositive limits return no messages.
Every successful claim returns a fresh token, including empty selections. Source
lease age uses the current caller's duration rather than a saved expiry; native keeps
nonpositive refusal and operator NaN/positive-infinity comparison behavior. Clock
reads and UUID allocation precede the lock, as in source. Claiming does not deliver.
Acknowledgment admits nonempty tokens before lookup; release has no such admission.
Missing/foreign-session messages return NotFound before delivery/lease checks.
Delivered records no-op after session admission. Unacknowledged records require a
matching nonnull token; cleared nil token differs from explicitly stored empty text.
Batch settlement follows caller ID order and can retain earlier effects on a later
refusal. Duplicate acknowledgment IDs return duplicate records; duplicate release
can conflict after its first clears the lease. Returned dates/tokens are detached.

Snapshot 119 executes four actual source methods over 106 profiles, repeating each
operation and comparing full record fields, returned projections/tokens and refusal
kinds/details. Initial lease timestamps and insertion-vs-sort differences are
explicit fixtures, using the real source clock with generous age margins. Native
tests cover 16 claim contenders without duplicate active leases, expired re-lease
and old-token fencing, 16 run/kind enqueue contenders with one message, release/
retry/ack, immutable object admission, detached timestamps and finalization indexes.
Focused workflow race passed. No dependencies or Python runtime modules changed.

Validation: `go test ./... -count=1 -timeout=180s`, `go vet ./...` and
`go test -race ./... -count=1 -timeout=180s` passed, alongside focused workflow race.
All 119 source exports are current; export execution emitted dependency deprecations
and an unknown-child return-code-255 warning but exited successfully. All 19 scan
guards are anchored. Full Python regression passed: 2,155 tests, 28 skips,
24 subtests and 3 dependency deprecation warnings in 94.96 seconds.
`git diff --check` and README outline passed. Python package invariants/guard checks
were not rerun because no package module or guarded Python runtime behavior changed.
Architecture regeneration passed all 9 checks with zero errors/warnings; visual
review remains skipped after the previous access denial, and no visual approval
is claimed. Specification SHA256:
`934fdf1bbd8e7371db2c13bc0fb67abfaf69eb708e46a0f8f2a56c57ad1edbe3`;
HTML SHA256: `40fdb7c9610e6c9ec9207d602fb697ebfdceff44ee87945a232c8494ce443bd9`.
README runtime
topology reviewed; the library is not installed in manager/model/HTTP/parent append
flows. Overall coverage is not refreshed in this slice; the previous 90.47% figure
belongs to d696498 and does not measure current statement coverage.

Next W3C2 adds terminal-and-drained whole graph retention, cascading run/node/attempt/
artifact/outbox/key/launch maps and both insertion indexes. W4-W6 trusted live origin,
engine/service/tools/HTTP/manager activation, MCP, native session SQLite, runtime
profiles and full G7 remain open. The complete Python-to-Go port is unfinished.
No durable workflow storage, restart-resume or exactly-once delivery is inferred.


## 2026-10-08 workflow cancellation and final completion

Baseline d696498. W3B2 adds CancelClaimedAttempts, RequestCancel,
FinishCancellation, FailRun, FinalizeRun and ListOutbox with named OutboxFilter.
The process-local store owns typed outbox maps/key indexes and attempt insertion
order. Claim order differs from spawn/read sorting and is preserved for cancellation.
Only CLAIMED attempts settle before startup; running tasks settle through CommitAttempt.
Source node-status refusal can retain earlier cancellations with stale run active IDs;
that operator-state failure is preserved explicitly. Native bounded-counter admission
precedes shared mutations. Nil cancellation detail/reason selects source defaults;
empty text stays explicit. Sticky first nonempty reason is stored even when a
transition fails. RUNNING without active nodes still goes through CANCELLING then
CANCELLED, with two version increments; repeated terminal requests retain state.
FinishCancellation gates live node states. FailRun keeps the source unlocked read
followed by versioned TransitionRun, so a concurrent change may return a CAS conflict.

FinalizeRun requires run CAS/RUNNING, existing artifact, then every node satisfying
dependencies. Store checks neither final artifact ownership/return-node provenance
nor schema_valid/verification truth. Completion, final artifact reference and one
WorkflowCompleted notification publish under one mutex; immutable closed payloads
carry named run/revision/artifact fields. Random ID allocation and int64 version
overflow refuse before publication. This is process-local atomic publication, not
durable delivery. ListOutbox sorts detached records by created_at/message_id with
explicit run/session/undelivered filters. No message leasing or append is installed.

Snapshot 118 executes five actual source methods over 131 profiles and repeated
submissions, comparing full run/node/attempt/outbox fields, error classes/details
and cancellation return order. State seeds are explicit fixtures. Native tests cover
started/unstarted cancellation through public APIs, 16 cancellation CAS contenders,
16 finalization CAS contenders with one outbox message, run double-increment/node/
attempt overflow refusal, sorted filters and detached delivery timestamps.
Initial corpus generation caught an omitted node_status in the missing-run profile;
the seed default was corrected. Focused compilation caught test-only ArtifactInput field names and a Value
comparison mismatch; corrected to existing typed constructors/canonical comparisons.
Focused workflow race passed before full gates.

Validation: `go test ./... -count=1 -timeout=180s`, `go vet ./...` and
`go test -race ./... -count=1 -timeout=180s` all passed; focused workflow race
passed too. All 118 source exports are current; export execution emitted dependency
deprecations and an unknown-child return-code-255 warning but exited successfully.
All 19 scan guards are anchored. Full Python regression passed: 2,155 tests,
28 skips, 24 subtests and 3 dependency deprecation warnings in 122.35 seconds.
`git diff --check` and README outline passed. Python package invariants/guard checks
were not rerun because no package module or guarded Python runtime behavior changed.
Archify regenerated the architecture HTML with all 9 checks passing, zero errors
and zero warnings. Visual review remains skipped after the previous access denial;
no visual approval is claimed. Specification SHA256:
`cc6b44f50c6ba65b0a7469fd08b7286f50e91dc0497552adb0201b75d8379658`;
HTML SHA256: `01415f0b6cf32349cfa359b6b0719ecb612282501f790b9613e643e0c09c905c`.
README canonical
runtime topology reviewed: no manager/model/HTTP installation changes. Boundary,
architecture specification, extension, parity and package documents are updated.
No dependencies or Python runtime/package modules changed. Overall coverage is not
refreshed in this slice; the prior 90.47% measurement belongs to d696498.

Next W3C adds explicit enqueue, delivery claims/ack/release and terminal-and-drained
whole graph pruning, including attempt insertion order and outbox-key indexes.
W4-W6 trusted live origin/engine/service/tools/HTTP/manager activation, MCP, native
session SQLite, profiles and full G7 remain open. No durable workflow backend or
restart-resume guarantee is inferred. Full Python-to-Go migration remains unfinished.


## 2026-10-08 workflow attempt CAS, settlement and artifact binding

Baseline cf12cef. W3B1 adds StartAttempt, CommitAttemptInput/CommitAttempt,
GetArtifact and ArtifactsForNode to the process-local store. Artifacts are private
immutable domain values indexed by named IDs. Startup checks missing/version/status
in source order, then CLAIMED -> RUNNING and stamps one start/heartbeat time.
It does not advance run/node versions. Commit validates terminal attempt/node enums
before lookup, checks attempt CAS/RUNNING and node RUNNING, then binds artifact
provenance. Artifact schema_valid, verification truth and correlated terminal pairs
are not store-owned checks. Commit can settle a still-running attempt after its run
was independently marked terminal, matching source.

Optional verification distinguishes omitted (nil -> not_applicable) from explicit
empty/unknown strings. The actual source conversion is late: invalid verification
fails after artifact/index references and terminal attempt status/version/timestamps
are stored, before node/run status/version/error settlement. Native retains that
observable partial failure, and repeated submission sees the advanced attempt
version. This is documented operator-input behavior, not an atomic failure claim;
future service/engine callers must supply validated finite enum variants.
For valid input, node/run counters advance and active node IDs retain definition
order. Native int64 version/allocation admission checks occur before publication.

Snapshot 117 runs actual source methods: 14 startup status/version profiles and
32 terminal pair, artifact, provenance, schema flag, refusal, verification and
already-terminal run profiles, including repeated commits. Generated IDs and times
are normalized only in output; fixture state seeding is explicit. All record fields,
error classes/details and repeated outcomes are compared. The first focused test
run found a fixture-only mismatch (schema object versus empty args); the seed was
corrected and focused workflows race passed. Native tests exercise 16 startup
contenders and 16 commit contenders with one CAS winner, single artifact publication,
missing getters and overflow refusal without publishing an artifact.

Fresh Go statement coverage uses `-coverpkg=./...`, merging duplicate source blocks
by maximum execution count: overall 19,032/21,036 (90.47%), workflows 787/847
(92.92%), agent 6,717/7,402 (90.75%), httpapi 1,478/1,638 (90.23%), teams 384/401
(95.76%) and runmeta 12/12 (100%). Profile:
`/tmp/mini-loop-go-shared-coverage-20261008.out`. These measure executed statements,
not migration completion; Python coverage was not refreshed.

Validation: focused workflow race tests, full Go tests, full Go race, full shared
coverage and `go vet ./...` passed. All 117 source exports are current and all 19
scan guards anchored. The first full export check reported a stale existing
child-background fixture with an unknown-child return code 255; an isolated retry
passed without changing that fixture. Full Python regression passed: 2,155 tests,
28 skips, 24 subtests and 3 dependency deprecation warnings in 76.07 seconds.
Package invariants and guarded-behavior checks were not rerun: no Python package
module or guarded Python runtime behavior changed.
`git diff --check` and README outline passed. Architecture regeneration passed 9/9
checks with zero errors/warnings. Visual inspection remains skipped because access
was denied; no visual approval is claimed. Specification SHA256:
`c34712b9d798a15af97754f4cbf99e3ea7fae4aaae7941c4fdd9573028d2c7a0`;
HTML SHA256: `7c6e941eb8dab9b5b5701ed362a57ae89d4b0fc150b99aab970a05c2d77c8a05`.
README canonical Mermaid topology reviewed: no live manager/model/HTTP installation;
boundary/specification, parity, extension and package documentation are updated.
No dependencies or Python runtime/package modules changed.

Next W3B2 adds cancel_claimed_attempts, request/finish cancellation and fail/finalize
run with completion outbox creation; W3C adds delivery claims/ack/release and whole
graph terminal-and-drained pruning. W4-W6 engine/live origin/service/tools/HTTP and
manager activation, MCP, native session SQLite, profiles and full G7 remain open.
No durable workflow backend or restart-resume guarantee is inferred.


## 2026-10-08 workflow store registration/admission/CAS/claim core

Baseline 23e208e. W3A adds NewInMemoryStore and finite StoreError kinds/details.
Named revision/hash, run/node/attempt and session-key maps sit behind one mutex.
Definition validation precedes deduplication: conflicting revisions fail; a semantic
alias returns the first registered definition without installing a new alias key.
Immutable definition projections need no shared mutable clones.

CreateRunInput retains named definition/session/key/parent/action/policy and inert
provenance fields with immutable object args. Required session/key errors precede
missing definitions. Launch identity hashes exact source canonical payload fields;
closed-value hashing retains integer/float identity and strictly rejects nonfinite
or surrogate data without encoding/json repair. Same session/key+payload returns
the latest stored run; different payload fails. Newly supplied contexts materialize
omitted capabilities as the source empty tuple/list; archival Snapshot.Clone retains
nulls. The added constructor profile verifies this new-launch boundary.
Run/node/attempt reads and write inputs detach mutable pointers/slices; run lists sort by creation time/ID, node
lists by definition order, attempts by spawn index/ID.

TransitionRun validates target before lookup, checks expected version before a
same-state no-op, preserves first start time and stamps terminal time. ClaimNodes
checks run/version/status, attempt budget, duplicates and every node before any
mutation. It does not check DAG readiness or max_concurrent_agents; engine owns
those constraints. Empty claims still increment run version and recompute active
IDs. Returned claims follow request order; active IDs follow definition order.
Native allocation/version checks are staged before commits; int64 overflow returns
NativeRecordOverflow without state changes rather than wrapping source counters.

Snapshot 116 executes actual source methods: 53 sequential operations, the complete
13x13 run transition matrix and three canonical refusal profiles. Random IDs and
generated wall times are normalized only in output. Matrix initial state/version
seeding is an explicit trusted fixture, not evidence of a public restore API. Tests
compare exact source kinds/details, canonical launch digests, all record effects
and refusal ordering. The first focused run found a missing quote in the source
invalid-status ValueError; Python repr rendering fixed it and the focused race
suite passed afterward. Native tests also cover 24 concurrent launch calls, 24
claim contenders with exactly one CAS winner, deep isolation, resume start retention,
UUIDv4 prefix shape, sort ties and atomic overflow failures.

Validation: focused workflows race and full Go test/race/vet passed. Final full
Go gates and source --check repeated after adding the omitted-capability constructor
case; all 116 snapshots are current. All 19 source scan guards anchored. The first
exporter check reported an ignored subprocess destructor/Event loop is closed
exception while still exiting zero; the final fresh check passed with only its
dependency deprecations. Full Python regression ran after final Go gates finished:
2,155 passed, 28 skipped, 24 subtests, three dependency warnings in 70.10s.
No implementation changed after those final gates. Overall coverage was not
refreshed. Package-module invariants and mutation guards were not rerun because
Python package modules and guarded behavior/anchors did not change. Diff check
and README outline passed.

Interactive architecture regenerated from JSON with showcase 9/9 and zero
errors/warnings. Spec SHA256
7ed5e7b2141744828d4e6b090fafed85166741be0b4f8b11fb45c7448189fef0;
HTML SHA256 fd704c632bf96eb3c487dc15bb00fa5a98596a0c1bfb201795e989f92802ec8c.
Visual review remains skipped after prior denial, without retry/bypass.
No dependencies or Python runtime/package modules were changed. README canonical
Mermaid reviewed: active topology remains unchanged because the store is not
installed. Boundary, extension, parity and package docs are updated. Source shared
workflow retention invariants in HARDENING_NOTES 8dw were reviewed; retention is
still W3C, not silently omitted from the remaining plan.

Next W3B ports attempt start/settlement, artifacts, cancellation and finalization;
W3C ports outbox/pruning with terminal-and-drained whole-graph eviction. W4-W6
engine, live origin policy and service/tools/HTTP/manager activation remain open.
MCP, native session SQLite, remaining profiles and full G7 are still open. Workflow
store state remains process-local and no restart-resume claim is made.


## 2026-10-08 workflow runtime records

Baseline 3fb1e91. W1B data projections now port WorkflowRun, NodeState,
AttemptClaim, NodeAttempt and OutboxMessage (native OutboxSnapshot). IDs, keys,
versions, cursors, attempt numbers/counts, spawn indexes, kinds and claim tokens
have named types. State enums reuse the existing finite variants. Args/payload
are closed immutable object values. Runtime provenance uses inert runmeta.Snapshot,
not the live agent context; historical unknown authority remains observable.

Five concrete Decode functions set source defaults only for omitted fields,
validate required declared field types and finite state variants, retain empty
IDs/negative counters/array order and preserve explicitly supplied zero times.
Explicit null status/scalar/list fields are refused before encoding/json can
silently keep their defaults. Native boundaries reject unknown fields and null
ID-array members. Python arbitrary live-object behavior is outside the native
JSON contract. Counters are machine integers/int64, timestamps float64.

All five projections have detached Clone methods: optional pointers, lists and
provenance pointers/capability lists cannot mutate the original. Closed args/payload
containers are already immutable. Source frozen claim/outbox objects are exposed
as detached mutable DTOs; storage owns eventual internal records and must clone
both admission and reads. Record decoding does not validate argument schemas,
authorize launch, advance run state or establish external delivery.

Snapshot 115 runs actual source constructors: 46 default/full/state/provenance
projections and eight invalid status/verification cases. Only generated wall times
are normalized, and only when omitted by the recipe. Native tests compare every
projected field and terminal predicate, require generated times to be positive,
mutate every clone pointer/list and cover stricter native malformed input.

Validation: focused workflows race, full Go test/race/vet passed; all 115 source
snapshots current and all 19 scan guards anchored. Full Python regression ran after
Go gates completed: 2,155 passed, 28 skipped, 24 subtests in 100.46s. Four warnings:
three dependency deprecations and one subprocess destructor/Event loop is closed
warning during test_owner_map_bound.py::test_the_owner_map_stays_bounded_across_many_deletes.
That warning did not fail the suite; no corresponding Python runtime module changed.
Diff check and README outline passed. Overall coverage was not refreshed this slice.
Python package invariants and mutation guards were not rerun because package
modules and guarded source behavior/anchors were unchanged.

Interactive architecture regenerated from JSON: showcase 9/9, zero errors/warnings.
Spec SHA256 aba7b19cb46e921163c656081e53fbaa56ea841f5d94556f8743a66a0794091f;
HTML SHA256 75f9918eabf8dc8c61cd9887c4288dda3989efd843930c5434364d24c2200361.
Visual review remains skipped after prior denial, without retry/bypass.
Nested provenance is an already materialized inert snapshot; this decoder does not
stamp missing caller fields or reconstruct a trusted live context.
No new dependencies or Python runtime/package modules changed. Active README
Mermaid topology is reviewed and unchanged because this is an uninstalled library;
boundary/specification and extension/parity/package docs are updated.

Next W3A ports actual source in-memory definition registration, idempotent run
admission, read/list, versioned transition and atomic claim. W3B settlement/cancel,
W3C outbox/pruning, trusted live origin adaptation, W4-W6 execution/installation,
MCP, native session SQLite, profiles and full G7 remain open. Source workflow
storage is process-local; no restart durability is inferred from session SQLite.


## 2026-10-08 shared inert provenance preparation

Baseline c2efdcb. Preparing W1B without a workflows -> agent -> workflows
import cycle: runmeta now owns MessageID, ActorID, Authority, Capability and
Snapshot. agent retains its existing public aliases and constants, private live
RunContext fields, trusted factories, validation, delegation and capability checks.
No decoded snapshot can construct a trusted context. Source RunContext.as_dict
remains the wire contract; shared fields do not add authority or approval.

Snapshot.Clone detaches optional actor/delegation/parent pointers and capability
storage. It retains nil versus empty capability arrays for historical projections;
live agent snapshots keep their existing nonnil empty capability list. Native tests
verify exact decoded wire bytes, clone isolation, unchanged untrusted authority,
trusted caller isolation and human-capability removal on delegation. Existing
source context, archival, event and teammate evidence is reused without a new
source snapshot. No dependencies, Python package modules, scanners or guarded
source behavior were changed.

README architecture reviewed: active Mermaid flows remain unchanged; the boundary
and interactive card now name shared inert provenance ownership. Interactive HTML
regenerated from JSON with showcase 9/9, zero errors/warnings. Spec SHA256
60bc8242ef7243bfce086e0d0d8c358615821e48a82172bfa9eb2353d5c966f5;
HTML SHA256 4c75fa4407c4b84d70a9e2e593ea43db6daa10e9c02d36a8dbac8eb54971053b.
Visual inspection remains skipped after prior denial, without retry/bypass.

Current full Go coverage uses -coverpkg=./... with duplicate source blocks merged
by maximum execution count: overall 18,623/20,608 (90.37%), agent 6,717/7,402
(90.75%), httpapi 1,478/1,638 (90.23%), workflows 380/419 (90.69%), runmeta 12/12
(100%). Profile: /tmp/mini-loop-runmeta-coverage.out. These are statement coverage,
not migration completion percentages; Python coverage was not refreshed.

Validation: focused provenance tests under race, full Go test with shared coverage,
full Go race and go vet passed. The initial vet found an unkeyed cross-package
snapshot literal; explicit field names fixed it, followed by full final-source
coverage and race gates. All 114 source exports current; all 19 scan guards anchored.
After the Go gates finished, full Python regression passed: 2,155 passed, 28 skipped,
24 subtests, three dependency warnings in 86.85s. Diff check and README outline
passed. Python package invariants and mutation guards were not rerun because their
package modules/behavior/anchors did not change.

This is a focused preparation commit; workflow run/node/attempt/outbox records, trusted live launch adaptation,
process-local store CAS/claim/outbox, engine, integration, MCP, native session
SQLite and full G7 remain open. Source list_nodes follows definition order; list_runs
sorts by creation time and run ID. Preserve those contracts in the store slice.

Next small commits:

1. W1B records: WorkflowRun, NodeState, AttemptClaim, NodeAttempt and OutboxMessage
   with named identities/counters, closed immutable args/payload and detached
   provenance; compare actual Python constructor defaults and projections.
2. W3A store admission: register/get definitions, idempotent create/read/list runs,
   versioned transition_run and atomic claim_nodes. Preserve definition-order node
   lists, refusal order, empty-claim version increments and all-or-nothing claims.
3. W3B attempt settlement/cancellation: start, commit, failure, terminal artifact
   binding, cancellation and finalization with source state-transition evidence.
4. W3C outbox/pruning: claims, acknowledgment/release and complete cascade pruning.
   This remains process-local, matching actual source; it is not workflow durability.
5. W4-W6 execute/install: trusted live origin admission, fresh readonly worker
   engine, service/tools/HTTP/manager selection and actual integration evidence.


## 2026-10-08 workflow DAG/schema and artifact submission validation

Baseline d0c4656. The workflow model library now ports the source definition
validator, its JSON Schema subset and structured artifact completion functions.
Named ValidationKind/ValidationError distinguish WorkflowValidationError,
ArtifactValidationError and the source TypeError branch. Definition validation
retains source refusal order across version/name/node admission, hard budgets,
exact read_file/glob policy and workflow-readonly profile, input/output schemas,
node IDs/kinds/rounds/items, return schema, needs and DAG acyclicity. Only the
source MVP AGENT/VERIFY/REDUCE nodes are admitted for execution definitions.
Validation does not authorize launch or mutate the immutable definition.

Schema validation supports only type/properties/required/items/enum/const/
additionalProperties/title/description, with recursive property order and exact
paths/messages. Value validation preserves missing/extra-key ordering, array
indices, numeric-versus-boolean admission, finite-float checks and Python
bool/int/float equality for enum/const. Exact big-integer/float comparison uses
rationals rather than rounding integers to float. Python JSON decoder NaN
constant identity is retained for membership and container comparison; scalar
const equality remains false. Arbitrary live Python object identity is outside
the closed JSON projection. Only visited floats are validated; ignored extra
fields are not recursively scanned, matching source. Artifact hashing remains
the independent recursive nonfinite refusal boundary.

ArtifactSubmission is immutable. ArtifactFromSubmission rejects missing or
wrong-tool completion, validates the controller schema, then creates an artifact
using explicit controller-owned run/node/attempt IDs. Worker payload cannot choose
binding identities. VerificationFromValue preserves known statuses and otherwise
returns unverified; it does not establish verification truth. No manager/model/HTTP
workflow installation, execution, authority grant or storage behavior was added.

Snapshot 114 executes actual source validators/functions: 36 definition/DAG,
18 schema, 21 value, four submission and nine verification recipes. Definition
inputs are raw JSON strings so generated fixture sorting cannot reorder schema
properties and change the first refusal. Native tests compare exact classes and
details, unchanged definition identities and full successful artifact projections.

Validation: focused workflow race and full Go test/race/vet passed. The full
Go gates were repeated after making submission fields immutable; no implementation
changed afterward. All 114 source snapshots current; all 19 scanning guards
anchored. Python full regression passed after the heavier Go gates finished:
2,155 passed, 28 skipped, 24 subtests, three dependency warnings in 70.74s.
No Python package modules or source mutation anchors changed; package invariants
and mutation catalogue were not rerun. No dependencies introduced. Diff check
and README outline passed.

README architecture baseline/canonical runtime topology reviewed: this remains
an uninstalled library, so active diagram flows are unchanged. Boundary, extension,
parity and Go package documentation are updated; the interactive card was regenerated
from JSON with showcase 9/9, zero errors/warnings. Spec SHA256
6cd3fb2c2f7d08ea34317cb9296aaa70908e31c0e51524f0c9418d7ff2011ecc;
HTML SHA256 eb519ebf78e862893668b40019e146525d5ae8c32219373fafdae8854f38dafb.
Visual inspection remains skipped after prior denial, without retry/bypass.

Next W1B ports run/node/attempt/outbox records and trusted-origin adaptation,
then W3 ports the actual source InMemoryWorkflowStore CAS/claim/outbox lifecycle.
The source workflow store is process-local; no durable workflow backend or
restart-resume claim is inferred from session SQLite. W4–W6 runtime/integration,
MCP, native session SQLite, remaining profiles and full G7 remain open.

## 2026-10-08 workflow state, definition and artifact foundation

Baseline 121125f (strict UTF-8 canonical profile). The new uninstalled workflows
package ports all finite node-kind/run/node/attempt/verification/source states,
terminal and dependency predicates, named identities, budgets/policies, immutable
WorkflowDefinition projections and concrete Artifact snapshots. Dynamic schema
and artifact data use the existing immutable closed JSON variants, never any,
interface{} or RawMessage in domain records. The generic strict boundary decoder
is used only with a concrete wire record. Unknown fields and malformed declared
variants are rejected before model construction.

Canonical identity follows actual source: recursive sorted keys, compact separators,
ensure_ascii=False and nonfinite refusal. Unicode, float/integer/negative-zero
identity and supplied integer versus default float wall budgets survive. Derived
wfdef/wf IDs use the first sixteen digest hex digits; explicit IDs/revisions survive;
parent/revision/identity metadata is outside semantic content and saved hashes are
ignored/recomputed. Schema objects are admitted without asserting semantic schema
or DAG validity. Canonical projections are private and immutable.

Artifacts retain explicit run/node/attempt IDs, content/schema hashes, verification,
schema_valid, media type and creation time. IDs preserve source UUIDv4 prefix shape;
random IDs and wall timestamps are normalized only in differential output. Creation
does not establish schema validity or verification truth. No storage, dispatcher,
manager, model tool or HTTP activation is supplied by this model slice.

Snapshot 113 executes actual Python constructors for five definitions, eight hash
profiles, four hash failures and two artifact states, plus every enum/status
predicate. Native tests compare canonical bytes, hashes and full model projections;
additional tests pin typed admission, saved-hash recomputation and detached views.
This is W1A. W1B run/node-attempt/outbox records and trusted-origin adaptation,
W2 DAG/schema validation and W3–W6 execution/storage/integration remain open.

Validation: focused workflow/jsonvalue race tests and full Go test/race/vet passed;
all 113 source snapshots current; all 19 source scanning guards anchored. The final
UUIDv4 metadata alignment was followed by another focused workflow/jsonvalue race
run; the final direct UTF-8 golden test was separately verified under race. The
initial exporter attempt failed because a local dataclasses import was absent;
after adding it, the new fixture regenerated and the full source check passed.
Python full regression passed after the heavy Go gates completed: 2,155 passed,
28 skipped, 24 subtests and three dependency warnings in 82.58s. The final direct
UTF-8 golden race test overlapped that run. No Python
package modules changed; package invariants and full mutation checks were not rerun.
No dependencies were introduced. Full migration is still open.

README architecture baseline/boundary, parity, extension and Go package docs are
updated. Canonical runtime topology is reviewed and unchanged because this library
is not installed. Interactive card regenerated from JSON: showcase 9/9, zero errors
or warnings. Spec SHA256
b6c61ea2e0c98c2fb41c9b6f13e3e4f0c3e0e5ee9e4b56283f11db639e0ea176;
HTML SHA256 38c51cdf3545cff3be99ebd4655a6341eafabdd49b06752e9690b4104e549bf9.
Visual review stays skipped after the prior denial, without retry/bypass.


## 2026-10-08 UTF-8 workflow canonical preparation

Baseline bc18796. The closed JSON value now has a separate strict MarshalUTF8
profile for Python ensure_ascii=False compact canonical bytes. Existing ASCII
archive renderers remain unchanged. It preserves integer/float identity and
rejects nonfinite/lone-surrogate values recursively, including object keys.
A direct Python golden probe and a native race test cover CJK, emoji, U+2028,
HTML punctuation, negative zero and invalid nested values. Full Go test/race/vet
passed during the coupled workflow model iteration. No dependencies, authority,
activation defaults or live topology changed. README baseline was reviewed.
The workflow model library is a subsequent commit; full G0–G7 remains open.


## 2026-10-08 actual teammate restart and managed root labels

Baseline 3de3fab. Snapshot 112 now includes a real source SQLite close/reopen
after manager spawning and a completed initial model turn. Owner, workspace,
run count and transcript survive, but team ID becomes the restored session's
own ID, member/label become lead/main, role/task binding and roster disappear,
and no teammate lifetime is restarted. The old team mailbox remains unread;
the restored handle's own lead inbox is empty. Recorded running status survives
without an attached worker. Full teammate identity recovery would extend Python
source behavior, rather than close a Go parity gap.

The native differential test uses the existing typed injected test backend and
compares that actual source projection after manager stop/reconstruction. It
proves adapter behavior, not SQL durability or a native process restart. Native
SQLite is still open. Both fixtures use default-off team tools; operator spawning
is exercised directly. Existing selected-tool/idle behavior has separate evidence.

The test exposed managed Go root labels using session IDs instead of source main.
The shared create/fork/restore composition now supplies main; named teammates
continue overriding their label. Event labels and delegated-by metadata follow
that source label, without changing session IDs, ownership, grants or admission.
The runtime correction and its source/native evidence belong in one small commit.

Validation: full Go test and vet passed; the additional focused restart race test
passed after simplifying its concrete projection; full Go race passed. All
112 source snapshots current, all 19 scanning guards anchored; Python full suite
passed 2,155 tests, 28 skipped, 24 subtests, three dependency warnings in 73.48s.
Source checking/scanning, focused Go and full Go tests overlapped Python regression;
full Go race started after Python completed. No Python package module changed, so
package invariants were not rerun. No guarded behavior changed; mutations were not
rerun. Final diff check and README outline passed.

README canonical topology is reviewed and unchanged; the boundary explanation,
extension seam, parity matrix and generated architecture card record the corrected
provenance and restart effects. Archify delivery passed showcase 9/9, zero errors
or warnings. Specification SHA256
cc99cae5c9c3223b2ccd8e45b45ebd42b0cafe81ef0c065093ecb4766fb6729a;
HTML SHA256 e5d0bf083d13ed7e121ba970f16b9a3177c94f19f45a29b2e87e34b98d16fe50.
Visual inspection remains skipped after the prior denial, without retry/bypass.

Next workflow migration slices, following the actual source modules:

- W1: closed identifiers/states/records and source canonical digests (models.py).
- W2: DAG/schema/artifact validation and exact refusals (validation.py/artifacts.py).
- W3: typed storage, claims and outbox transitions with recovery evidence (store.py).
- W4: attempt/runner/engine dispatch, cancellation and outcome transitions.
- W5: explicit service/tools/HTTP/manager bindings with retained authority gates.
- W6: real-source differential and restart/concurrency/effect acceptance gates.

Do not install a partial workflow service by default. Workflows, MCP, native SQLite,
remaining runtime profiles and full G7 remain open. The full migration goal is active.

## 2026-10-08 actual shared-team diagnostic boundary

Baseline 80cae9d. Rechecking the next planned fleet-diagnostics slice against real
source found an inaccurate parity assumption: SessionManager binds its shared
mailbox as agent.state[bus], while self_audit._problem_sources scans state[teams].
The actual source manager therefore omits shared bus problems from both fleet and
owner reports. Adding shared aggregation would enhance source behavior, rather
than close a Go parity gap; do not expose a shared cross-owner ledger through a
per-session observation merely to fill that slot.

The existing actual-manager snapshot now has eight recipes, including fleet and
owner cases that record a real invalid-key bus problem and retain unread mail.
Native differential tests compare the exact reports, require a real bus ledger
entry and verify observation leaves the message pending. No runtime behavior,
type schema, activation default or dependency changed. The existing typed Teams
observation slot remains available to explicit library callers.

The README baseline and boundary, parity matrix and generated architecture card
are corrected. The canonical diagram topology is unchanged. Architecture delivery
passed showcase 9/9 with zero errors/warnings; specification SHA256
172ab109362746a5180113c3a67e5329402baa9465fe2592a2c7d5bb24bc7e61,
HTML SHA256 4e573e863366b28a9c7c44d33bd0ed5c857805d2730f7546c2c84433dc720fb4.
Visual review remains skipped after the prior denial, without retry or bypass.

Validation: focused actual-manager Go race test passed after fixture regeneration;
112 source snapshots current; all 19 source scanning guards anchored. Python full
regression passed: 2,155 passed, 28 skipped, 24 subtests and three dependency
warnings (84.59s); focused Go race, source check and scanning guards overlapped
the beginning of that run. Final diff check and README outline passed.
Full Go/race/vet and mutation checks are not repeated for
this test/evidence/documentation-only change. No package modules changed.

Next investigate actual-source teammate restart effects before designing native
restoration: persisted records do not include teammate identity, and source builds
fresh lead state without restarting a teammate runner. Workflows, MCP, native
SQLite, remaining runtime profiles and full G7 remain open; the full goal is active.

## 2026-10-08 automatic team inbox and owned idle/task turns

Baseline cf4debb (actual source lifecycle corpus; raw JSON preparation c014fea);
full G0–G7 remains open. The previous
checkpoint implemented initial
teammate execution. This iteration extends that same owned lifetime through source
inbox polling, shutdown, task claims, worktree selection and timeout notification.

- Explicit TeamTools supplies round injection after background results and before
  custom injectors. It checks the live registered core before consuming its own inbox,
  retains shutdown assignment on partial faults, emits a typed archival team_inbox
  count before cleaned rendering and does not grant authority to message content.
  Delegated children cannot consume parent mail. GET remains a non-consuming view.
- After initial delivery, the owned worker polls then consumes, pops sticky shutdown,
  runs raw inbox prompts, or claims the first runnable task. Activity resets its
  monotonic deadline after delivery. A sleep crossing the deadline still processes
  that poll, matching source. Timeout sends idle_notification; shutdown/cancel does not.
- Autonomous turns use fresh named peer contexts stamped session_manager and delegated
  by lead, without parent messages or human capabilities. Task result metadata carries
  task_id. Claims and task tools retain the original shared board/lifecycle workspace;
  existing worktrees select atomic execution dependency rebind, while missing/invalid
  names fall back to the original workspace. Managed admission protects rebind plus run.
- Delete/stop cancel and join idle-started model runs before reclamation. Timeout and
  shutdown retain the registered session. Initial completion has its own observation
  signal, separate from lifetime completion; existing tests now use the correct phase.
- ManagerConfig exposes typed idle durations with 1s / 60s defaults and negative-value
  refusal before IO. Launcher binds the existing validated environment settings. Tool
  selection and feature defaults remain explicit; task/worktree tools remain separately
  selected. No dependencies or vague domain/service payload types were introduced.

Snapshot 112 adds 8 actual injector and 11 actual idle recipes after real Python spawn.
Source model execution, clocks/sleeps, task effects and mailbox effects are real; the
initial idle task is cancelled before the explicit probe. Prepared worktree directories
exercise path selection rather than Git branch creation. IDs/paths are normalized;
returned prompts/results, peer provenance, task snapshots and partial effects are
compared. New shared AppendLegacyDefault preserves Python default JSON spacing, insertion
order, ASCII escaping and historical nonfinite/surrogate values for raw idle prompts.
Native tests additionally prove idle-run deletion joins, shutdown handle retention,
stale pre-IO admission, delegated-child inbox isolation and idle-duration validation.

Validation completed: focused team/launcher/jsonvalue race tests; full Go test with
-coverpkg=./... statement profile, full Go race and vet; all 112 exported source
snapshots current; 19 anchored scanning guards; three selected mailbox mutations
caught (tail read, in-memory cap and dot segments). Full Python suite passed: 2,155
passed, 28 skipped, 24 subtests and three dependency warnings (82.95s). The extra
jsonvalue/teams race run overlapped the beginning of Python regression; the heavier
full Go/export/mutation handles were terminal first. No Python package module changed;
package invariants and the full mutation catalogue were not rerun. Final diff check
and README outline passed. No implementation changed after these gates.

Full-package merged statement coverage (deduplicated source blocks) is 18,197/20,135,
90.37%; agent 6,716/7,401 (90.74%), httpapi 1,478/1,638 (90.23%), teams 384/401
(95.76%), protocol 1,407/1,545 (91.07%), launcher 152/189 (80.42%). Profile path:
/tmp/mini-loop-team-lifecycle-coverage.out. This differs from the preceding 87.1%
package-own-test profile; it measures cross-package execution and is not a migration
completion percentage. Python coverage was not refreshed.

Architecture HTML regenerated from JSON, showcase 9/9, zero errors/warnings,
correction_rounds 0. Spec SHA256
0e4be6f189f38c6b272a39e075c81e585b2bc2b1120729a9d8bee925e231ee73;
artifact SHA256 6ac1a4f69c93a9539d739f520df72ff635329a8a842a644bff5734dedddf7d77.
Visual review remains skipped after the prior denial, without retry/bypass.

Delivery is split into three commits: raw JSON profile/renderer preparation,
actual-source lifecycle evidence, and owned lifecycle/injection runtime with its
behavior tests. Team fleet self-audit
aggregation, teammate restart restoration, workflows, MCP, native SQLite, remaining
profiles and full G7 remain open. Full migration is not complete.

## 2026-10-08 actual source team lifecycle corpus

Baseline c014fea. Snapshot 112 captures real Python spawning before 8 injector and
11 idle recipes, with real model/mailbox/task effects and short real sleeps. Prepared
worktree directories test path selection; no Git branch creation is claimed. Initial
resource sharing, exact prompts/results, autonomous peer contexts, task effects,
partial faults and shutdown priority are retained; IDs/paths are normalized.
This source evidence adds no Go runtime activation or topology change. Native binding
and differential consumption are delivered in the next checkpoint. Export --check
reports all 112 snapshots current; scans and the full Python regression passed.

## 2026-10-08 raw Python JSON prompt preparation

Baseline e67ae85. Add the closed-value AppendLegacyDefault profile for Python's
spaced default separators, preserving insertion order, ASCII escapes and historical
nonfinite/surrogate scalars. The team library exposes a raw historical message renderer
for idle prompts, separate from cleaned round injection. This preparation changes
no installed tools, defaults or topology. The README architecture baseline is reviewed.
Full Go test/race/vet passed with these functions; the additional direct JSON profile
examples passed the focused jsonvalue/teams race run. The next checkpoint records the
actual source lifecycle corpus and runtime binding. No dependencies were added.

## 2026-10-08 standalone team-tool help boundary

Baseline 968e8ee. Update --team-tools help to advertise team tools and concurrent
initial teammate turns. Runtime selection/defaults are unchanged from that commit;
automatic inbox injection and idle/task lifetime remain open. README architecture
baseline was reviewed; no topology change is required. The full Go test/race/vet,
contracts/scans, selected mutation and isolated Python gates recorded in the next
checkpoint included this CLI code. The separate CLI test run also passed before
this small documentation/help commit. Coverage is the measured checkpoint below.

## 2026-10-08 owned initial teammate execution

Baseline 01cd8dc (common composition / named peer preparation); full G0–G7 remains
open. Native teammate creation and its initial
concurrent turn are implemented. This checkpoint does not claim the source idle loop,
automatic inbox injection, shutdown-driven lifetime or autonomous task/worktree claims.

The inspected Python manager.py spawn_teammate / _initial_teammate_run contract
inherits the parent's owner, original workspace, workspace retention, skills and memory
without resolving current user resources. The child rebuilds the manager model/system
builder and interactive permission mode, prefixes teammate coordination guidance,
derives named peer provenance without human capabilities, shares the original task
board and removes recursive spawn. Normal initial completion delivers its result to
lead. Parent deletion leaves the child registered; bound roots are retained.

Go exposes typed SpawnTeammateRequest / SpawnedTeammate / TeamSpawnRefusal, reserves
member names before construction and tracks creation through manager shutdown. Failed
construction releases reservations and unpublished persistence. The initial worker
has a manager-owned cancellation context and join handle, independent of the spawning
request. Delete/stop close admission, cancel and join before scratch reclamation.
The existing common runtime composition is reused while bypassing resource resolution.
Ten exact source schemas/traits now install with explicit TeamTools / --team-tools;
teammates omit spawn. A private binding guard and existing exec permission gate still
apply. No dependencies were added; full feature activation remains unsupported.

Native tests prove fixed parent resource binding even with a different current resolver,
fresh model/system/mode, named peer actor/delegation/capability dropping, result delivery,
concurrent duplicate refusal, failed-factory retry, stop-during-construction rejection,
parent-delete retention, stop/delete joins, bound-root retention, readonly rejection and
no reacquisition of the parent's active core turn mutex. Existing source snapshots
remain 111; no new differential spawn evidence is claimed. Snapshots 110/111 retain
their trusted roster scope. Team identity/runner is process-local and is not restored
as a teammate.

Validation: focused team/launcher/CLI tests; full Go test with statement coverage,
full Go race and vet; all 111 exported source snapshots; 19 anchored scanning guards;
the selected delivery-drops-oversized-work mutation caught; isolated Python 2,155 passed,
28 skipped, 24 subtests passed and three dependency warnings (73.17s). No Python package
modules changed; invariants were not rerun. The complete mutation catalogue was not run.
The named-peer test added after the full Go run passed with the focused team race run.
Go statement coverage is 87.1% overall, agent 89.6%, httpapi 90.1% and teams 95.5%;
this is test statement coverage, not a migration-completion percentage. The profile
was generated at /tmp/mini-loop-team-spawn-coverage.out. Python coverage was not refreshed.

Architecture delivery: docs/mini-loop-system.architecture.html regenerated from the
JSON specification; showcase 9/9, zero errors/warnings, correction_rounds 0.
Specification SHA256 78857e41f2f39e138f0920f1d00dfd298939f5c45f63a5c479e4aa5fe921c5b5;
artifact SHA256 6c1d867190b89aa2f35a0a5c50e9a210681525cf2c208a27a20bca1afe2677b2.
Visual review remains skipped after the prior denial; no retry or bypass.

Delivery is split into common composition/provenance preparation, owned teammate
runtime with tests and architecture, and the standalone CLI help boundary. Stage only
task-owned paths. Automatic inbox injection, idle/shutdown/task lifetime, workflows,
MCP, native SQLite and full G7 remain open.

## 2026-10-08 teammate composition preparation

Baseline 533f824. Extract the common managed runtime composition before applying
owner-resource resolution, so teammate creation can inherit a fixed parent bundle.
Add named peer derivation that retains parent/delegation provenance and drops human
capabilities. This preparation changes no installed tools, defaults or topology.
The focused peer-context test checks authority stripping and invalid parent refusal.
The README architecture baseline is reviewed with no topology change required.
An isolated export of this staged Go tree passed full test, race and vet before commit.

## 2026-10-08 independent team launcher selection

Baseline 7570408; full G0–G7 remains open. launcher.Options.TeamTools and standalone
--team-tools independently select the nine implemented mailbox/protocol tools.
The redacted availability report includes team_tools. Selection is off by default;
MINILOOP_FEATURES still refuses comprehensive activation, and spawn_teammate remains
absent. This launcher adapter does not supply a teammate construction/lifecycle path.
README review baseline, extension/parity boundary and architecture specification /
generated artifact are updated in this separate delivery.

CLI tests prove explicit/off reports without starting providers/listeners. A real
local HTTP server uses the direct provider boundary for send_message/read_inbox tool
rounds, observes all nine schemas (no spawn), checks tool results and transcript
completion, proves repeated GET team retains the message, and sees an empty inbox
only after the model read. Server stop joins the manager. Initial test adapter faults
(decoded provider messages as internal protocol objects and read-only teams.Message
as an HTTP decoder) were fixed by using only the provider fields being observed and
a concrete inbound HTTP projection; runtime serialization was unchanged.

The combined final gates recorded below include this launcher code: focused team /
launcher / CLI tests, full Go test/race/vet, all 111 source snapshots, 19 scanner
guards, three selected relevant mutations, and isolated Python 2,155 passed / 28
skipped / 24 subtests / three dependency warnings (71.73s). No implementation changed
after these gates. Final doc diff/README outline passed before staging. Coverage
not refreshed; native spawning/inheritance, injection/shutdown/idle lifetime, workflow
routes/runtime, MCP, native SQLite, remaining profiles and full G7 remain open.

Architecture delivery passed 9/9 automated checks with zero errors/warnings:
spec SHA256 b9460c8debc34927c8b7b5b4b8ac4f11614c668e869d5e067e1d22e13bb4aef2;
artifact SHA256 4893298faadc059adf04ad72c8a94d3ce70b4bfd3748cd5750166897cfa3f198.
Visual inspection remains skipped after the previous denial; no retry/bypass.

## 2026-10-08 manager-bound team mailbox/protocol tools

Baseline 6eaab05; full G0–G7 remains open. SessionManager now owns the native
protocol coordinator alongside its fixed bus. ManagerServices.TeamTools explicitly
installs nine closed mailbox/protocol variants through the existing execution gate;
spawn_teammate is absent until native construction/lifecycle is implemented. Source
schemas and read/write/readonly/parallel traits are retained. In particular read_inbox
is source read-risk/read-only despite draining and automatically sending shutdown acks.

Runtime-owned identity supplies team/sender/root. Send validates current roster plus
lead, broadcast skips self and preserves source count/first-three refusal summaries,
and shutdown/request-plan/review are lead-only. Directory views derive from immutable
registered session identities in manager order, outside coordinator locks. This is
not native roster construction or spawning evidence. Consume stores the sticky atomic
shutdown assignment even on a later partial error; consuming it in the owned teammate
lifecycle is still pending. Exact empty-inbox/protocol notices and legacy rendering use
the library, while masks, results, permissions and action settlements use the common gate.

A private owner/session/workspace and current-registration guard runs before journal
replay as well as new effects, closing the replay bypass that handler-only checks would
leave. Foreign/deleted/stopped handles cannot retrieve settled private inbox projections.
Selected delegated children rebind the built-in guard to their fresh handler and do not
inherit manager/bus state, matching actual source child construction. Selection reduces
installed tools only; forks keep activation with a fresh lead team. Default activation
is unchanged. No loosely typed domain state or dependency is added.

Snapshot 111 adds eleven actual installed Python handler recipes / 65 steps over real
sessions and mailbox IO. An explicit trusted roster fixture precedes execution; only
displayed request IDs and created_at are normalized. Routed/empty/unconfigured messages,
Unicode recipient diagnostics, broadcast/refusal, lead checks, review/ack, partial drain
and nil/empty metadata are compared. Native tests also cover schema/trait identity,
rewrite/masking/replay, foreign/stale authority, readonly acks, selection/forks and a
real selected-child model turn. Initial failures were test adapter mistakes (mailbox
inboxes path, message decoding API and permission-mode method), corrected before gates.

Next deliveries: standalone flag selection; native teammate construction with immutable
resource/workspace/owner inheritance and owned initial-run cancellation/join; automatic
inbox injection/shutdown consumption, idle/task claims, prompt identity and diagnostic
aggregation. The four workflow routes/runtime, MCP, native SQLite driver/crash cases,
remaining provider/context profiles and full G7 differential/performance/release audit
remain open. The overall migration is not complete.

Final combined worktree validation (all process handles terminal):

- Focused agent/launcher/CLI team tests passed. `go test ./... -count=1 -timeout=180s`,
  `go test -race ./... -count=1 -timeout=180s` and `go vet ./...` passed.
- `export_go_contracts.py --check`: all 111 files current, two dependency deprecations.
  `verify_scans.py`: all 19 scanning guards anchored.
- Three selected source mutations were caught: broadcast-ignores-refusals,
  shutdown-authorization-dropped, message-to-a-ghost-teammate-silently-lost.
  They validate source guards; native replay/owner admission is proved by Go tests.
  The full mutation catalogue was not rerun.
- Final isolated `.venv/bin/python -m pytest -q`: 2,155 passed, 28 skipped,
  24 subtests and three dependency deprecation warnings in 71.73s.
- Architecture delivery: 9/9 automated checks, zero errors/warnings. Spec SHA256
  d31524056e27a736e2fe5e9b1f2cac2bfc6a78dad67dc4aa726a95b38f505f46;
  artifact SHA256 fb4a2bb3ee2eadc6c5858aec7b845bf086843e89ee5f810da07f76edb51d6c45.
  A redundant direct gate edge initially crossed existing routes; the final map uses
  its existing bound-resources path and manager binding with explicit team labels/cards.
  Visual inspection remains skipped after the earlier denial, with no retry/bypass.
- `git diff --check` and README outline checked before staging. Coverage not refreshed.
  Python package modules and Unicode tables are unchanged; invariants/Unicode check
  were not rerun. No code change followed the successful final gates.

## 2026-10-08 native operator team protocol coordinator

Baseline c55439b; full G0–G7 remains open. The native teams library now coordinates
shutdown requests/acks, task-plan requests, plan submissions/reviews and correlated
responses through a concrete trusted MemberDirectory and a detached insertion-ordered
protocol view. Its process-local table holds 200 requests, evicting resolved history
before pending requests. Reports truncate at the source preview boundary; oversized
instructions are refused. Publication/resolution precedes IO. Consume drains first;
a later malformed row can retain earlier effects and a shutdown outcome even on error.
Callers must apply that sticky outcome. Correlation preserves the source request-ID,
type and status checks; it adds no sender authentication or transactional outbox.

Snapshot 110 adds 25 actual Python manager recipes and 117 operations. Fixed clocks
and IDs make exact result/render hashes comparable. Fixture roster construction uses
real source sessions and bus IO but does not prove teammate spawning or scheduling.
Cases cover empty/no-agent/missing members, review ordering and foreign teams,
truncation/refusal, historical nonfinite/surrogate JSON, spoofed sender correlation,
partial drain failures, filesystem faults before/after state publication, eviction,
and response truthiness/string rendering. Native tests also cover cancellation,
invalid directory states, entropy failure, ID collisions, detached snapshots and
concurrent reviews. No new dependencies or loosely typed domain state are added.

README canonical Mermaid and boundary, EXTENDING, parity matrix and architecture
specification/generated HTML now identify the coordinator as an operator library.
The default manager still binds only its bus and owned nonconsuming GET. Next slices:
managed teammate roster/construction with resource/workspace inheritance and owner
lifecycle; team tools through the common execution gate; automatic inbox injection,
idle/task claiming, prompt identity and diagnostic aggregation/cancellation joins.
All four workflow routes/runtime, MCP, native SQLite driver/crash cases, remaining
provider/context profiles and the full G7 release audit remain open.

Final validation (all process handles terminal):

- Focused teams/jsonvalue/pytext tests passed. Full `go test ./... -count=1
  -timeout=180s`, `go test -race ./... -count=1 -timeout=180s` and `go vet ./...` passed.
- `export_go_contracts.py --check`: all 110 files current (two dependency warnings).
  `export_go_unicode.py --check`: pinned Unicode 14.0.0 tables current.
- `verify_scans.py`: all 19 scanning guards anchored. `verify_guards.py -k protocols-`:
  both protocols-never-pruned and protocols-evict-live-pending mutations caught.
  The full mutation catalogue was not rerun.
- Final isolated `.venv/bin/python -m pytest -q`: 2,155 passed, 28 skipped,
  24 subtests, four warnings, 129.16s. Three dependency deprecations and one ignored
  subprocess transport destructor/event-loop-closed warning in
  test_owner_map_bound.py::test_the_owner_map_stays_bounded_across_many_deletes.
- Architecture delivery: 9/9 automated checks, zero errors/warnings. Spec SHA256
  10e2997e5c991756aec8f8ab4a420912673735b60f4d5c154d05f1fb845a6e40;
  generated artifact SHA256
  00cac2493c4783cbcc327a2692408f597cd18b62e562b5435f58123bc3e9f9a3.
  Visual inspection remains skipped after the earlier denial; no bypass was tried.
- `git diff --check` and README outline passed before staging. Coverage was not
  refreshed. Python runtime package modules were unchanged; invariants were not rerun.

## 2026-10-08 closed Python value projections and printable tables

Baseline 2a0b5c7; full G0–G7 remains open. This prerequisite provides immutable
closed JSON truthiness, Python str/repr, legacy ASCII indent rendering and
surrogatepass code-point prefixes. No any/interface/raw domain state or dependency
is added. The existing Unicode generator now emits Python 14.0.0 printable ranges;
Go's newer assigned characters must remain escaped under the source profile.
A differential failure on Unicode 15 U+1FAE8 exposed and verified this distinction.
README architecture review baseline and projection boundary are updated; these
helpers change no ownership, routing, tool installation or feature defaults.

Focused jsonvalue/pytext tests passed. Final combined worktree gates passed:
`go test ./... -count=1 -timeout=180s`,
`go test -race ./... -count=1 -timeout=180s`, `go vet ./...`, and
`export_go_unicode.py --check` (Unicode 14.0.0). The independently isolated final
Python suite passed: 2,155 passed, 28 skipped, 24 subtests, four warnings in 129.16s.
Three warnings are dependency deprecations; the fourth is an ignored subprocess
transport destructor reporting an already closed event loop in the owner-map test.
No Python runtime module changed. Coverage was not refreshed. Final diff whitespace
and README outline checks are required before each commit. Team coordinator work
is delivered separately, with its own source snapshots and architecture boundary.

## 2026-10-08 typed team/protocol input contracts

Baseline 14a0665; full G0–G7 remains open. This is the concrete input part of
team slice 3. Next implement the typed protocol coordinator and manager-bound
team tools through the existing execution gate, then teammate construction,
resource/workspace inheritance, inbox delivery, lifecycle and idle/task claiming.
All four workflow routes/runtime, MCP, native SQLite and the G7 audit remain open.

All ten source tool names now have closed concrete input variants and exact source
schemas. The empty variants retain their discriminators. Inputs have no owner,
session/team identity or mailbox-root fields. Standard object-only metadata uses
the shared immutable JSON value tree; no any/interface/raw domain state is added.
Optional pointers are detached on construction/copy/access; nil metadata preserves
absence versus explicit null for replay hashing, and empty/false remain distinct.
Nested key sorting creates a separate projection without changing live order;
structural masking retains source last-value behavior when masked keys collide.
Integer precision, floating .0/negative zero, Unicode and nested array order are
retained. Nonfinite/surrogate metadata is refused at the model-input boundary,
while the historical mailbox keeps its declared legacy behavior.

Snapshot 109 captures the actual ten installed Python tools, source traits and
74 keyword/input cases. It invokes real handlers with no manager, so the corpus
proves keyword binding and projections, not execution or teammate scheduling.
input_json preserves source member order through the sorted outer fixture. Source
registered-secret masking is compared before encoding, including key collisions.
Native tests cover wrong types, required fields, foreign authority/root inputs,
duplicate members, constructors/accessors and provider/tool-use storage round trips.
Python handlers do not enforce all advertised types; Go's strict typed validation
is an explicit migration boundary. Optional text null is rejected, metadata null
is retained. No dependencies, tool installation, feature defaults or architecture
flow change. README review baseline/boundary and extension/parity docs are updated;
the unchanged canonical map is not regenerated.

Validation:

- Focused team/decision-schema/memory input tests passed. Initial focused failures
  exposed missing spawn/request-plan sorted field order, an ineffective source
  secret minimum length and input-member reordering in the fixture envelope;
  all were corrected before final gates. A duplicate type-null tracking tag also
  broke memory identity in the first run; one shared Type flag now preserves both.
- `go test ./... -count=1 -timeout=180s`,
  `go test -race ./... -count=1 -timeout=180s` and `go vet ./...` passed.
- `export_go_contracts.py --check`: 109 files current, two dependency deprecations.
- `verify_scans.py`: all 19 scanning guards remain anchored.
- Final isolated `.venv/bin/python -m pytest -q`: 2,155 passed, 28 skipped,
  24 subtests and three dependency deprecation warnings in 78.54s. The interrupted
  earlier run and independently committed offline fixture fix are recorded below.
- `git diff --check` and README outline passed. Coverage was not refreshed.
- Python runtime modules/guard behavior were unchanged; invariant and mutation
  catalogues were not rerun. No interactive-map regeneration/visual run was needed.

## 2026-10-08 offline documentation example verification

Baseline 084ee79; full G0–G7 remains open. The runnable documentation test gave
its preamble fake settings/client, but the README composition example reloads
settings and builds a new client. Host environment could therefore select the
real provider during a supposedly offline suite. The test now pins
MINILOOP_FAKE_LLM=1 and MINILOOP_FAKE_DELAY=0 with monkeypatch before executing
runnable blocks. It still executes the documented production construction seams;
illustrative blocks keep their existing skips. Runtime behavior is unchanged.

A first full suite was interrupted after checking the live process and finding
the documented-example working directory and an unpinned provider selection.
Its eventual interrupted output had 2,148 passed, 28 skipped and exit 2 in 334.91s;
it is not a successful gate. After fixing the fixture, the focused documentation
suite passed (15/12 in 0.35s) and a new isolated full suite passed: 2,155 tests,
28 skipped, 24 subtests, three dependency deprecations in 78.54s. The original
process was confirmed terminal before the new run. This test correction is
committed independently. README baseline/validation prose and diff check passed.

## 2026-10-08 unconstrained source object schema

Baseline 1abfa99; full G0–G7 remains open. Python send_message advertises
metadata with exactly {"type":"object"}, without properties or additionalProperties.
The Go schema validator now accepts that valid unconstrained object leaf without
rewriting the source schema. Explicit null properties/additionalProperties remain
invalid, including when additionalProperties=true. The existing decision schema
null/union/bounds guards remain in force. New tests retain the exact round-trip
schema and clone projection. This prerequisite adds no tool activation or runtime
control/data flow, so the canonical architecture remains unchanged.

Validation: focused schema/team/memory tests, full `go test ./... -count=1
-timeout=180s`, `go test -race ./... -count=1 -timeout=180s` and `go vet ./...`
passed on the final worktree. Isolated `.venv/bin/python -m pytest -q` passed:
2,155 tests, 28 skipped, 24 subtests, three dependency deprecations in 78.54s.
Source exporter check has 109 current files; 19 scan guards remain anchored.
README outline and `git diff --check` passed. The schema prerequisite is committed
separately from the follow-up team input and documentation test changes.

## 2026-10-08 bounded team mailbox and owned peek

Baseline bb7a366; full G0–G7 remains open. The next team slices are:

1. Shared immutable Python JSONL values and native bounded mailbox (this slice).
2. Bound lead identity and owner-scoped non-consuming GET team (this slice).
3. Concrete team/protocol tool variants through the common execution gate.
4. Teammate resource/workspace inheritance, lifecycle and protocol inbox delivery.
5. Idle/task claiming, prompt identity, diagnostic binding and cancellation joins.

Then continue all four workflow routes and their native runtime, MCP, native SQLite,
remaining provider/context profiles and the full differential/performance audit.

MessageBus contracts now have 32 actual source recipes: both backends, 16,000
code-point send bound, 100-message memory shedding and delivery bounds, byte-tail
loading, peek/drain distinction, source loss notice/ledger, malformed rows and
Unicode separators, nonfinite/surrogate historical data, integer parse limits,
structural masking and persisted bytes. A 1-GiB sparse native mailbox test verifies
tail loading rather than whole-file buffering. Native concurrency tests serialize
send/peek/read within one instance; no cross-process queue transaction is claimed.
The immutable Python JSON parser/renderer moves to go/internal/jsonvalue and keeps
the improvement archive API through aliases; no arbitrary Go values enter state.
Source-owned metadata and legacy extensions remain explicit closed JSON variants.

Source path regex incorrectly accepted '.'/'..' components; both runtimes now
refuse them before IO, with four Python regressions and a named mutation guard.
Other allowed dotted identifiers retain source behavior; this is lexical validation,
not root/symlink confinement. No new dependency is introduced.

Managed sessions bind team=id/name=lead before publication, including state-store
restoration; a fork binds a fresh identity. PeekTeam checks ownership before IO,
uses the fixed manager bus and returns newest 50. Source HTTP corpus has eleven
handler outcomes plus one custom-agent teamless response projection. Native TCP
checks leave 75 messages available to delivery after repeated viewing; a foreign
caller sees 404 for an unreadable owned mailbox. Viewing starts no model work,
consumes no rate budget, performs no second historical-data mask and never drains.
Bus diagnostics are available on the library but not yet aggregated in self-audit.
Teammate/protocol/autonomy/model-tool/prompt paths remain explicitly pending.

Snapshot count 108. HTTP now has 40 operations/37 patterns versus source 44.
README Mermaid/boundaries and interactive architecture source are updated.

Validation:

- Focused native team/mailbox tests and 66 Python team/content/UI tests passed.
- Final `go test ./... -count=1 -timeout=180s -coverpkg=./... -coverprofile=…`,
  `go test -race ./... -count=1 -timeout=180s` and `go vet ./...` passed.
  Vet initially rejected unkeyed literals of an aliased external Field; defining
  the native named Field and explicitly converting it resolved that finding.
- Shared statement coverage deduplicates locations and retains maximum execution
  counts: overall 17,316/19,207 (90.15%); teams 180/193 (93.26%); shared jsonvalue
  295/307 (96.09%); agent 6,346/6,986 (90.84%); HTTP 1,477/1,638 (90.17%).
  Package boundaries changed for archive parsing; the archive package alone is
  159/174 (91.38%). Coverage is not migration completion.
- Source exporter `--check`: 108 files current, two dependency deprecations.
- `verify_invariants.py`: 77 modules; `verify_scans.py`: 19 guards anchored.
- `verify_guards.py -k mailbox-dot-segment-escape` and
  `verify_guards.py -k team-peek-consumes-the-inbox`: both mutations caught.
  The full mutation catalogue was not rerun; these are selected guard checks.
- Isolated `.venv/bin/python -m pytest -q`: 2,155 passed, 28 skipped,
  24 subtests passed, three dependency deprecation warnings in 146.79s.
- Architecture regeneration passed 9/9 with zero errors/warnings after routing
  corrections. Spec SHA `350acd432f80180e9133914df2d08ef9076ec45342c40d951bbe3ce575e683a1`;
  artifact SHA `d37e0c94f2937940352fadda5c3f2a74434709b360166bea5168f31a098c0539`.
  Visual inspection remains skipped following the earlier tool denial.
- README outline and `git diff --check` passed.



## 2026-10-08 shared Python JSONL value parser

Baseline 97bf9b1. Historical improvement rows now use go/internal/jsonvalue's
immutable closed variants: null/text/integer/float/boolean/array/object. The
archive keeps its exported names and errors through aliases. Parsing, duplicate
key ordering, legacy escaping and strict HTTP serialization retain their prior
behavior; this is code sharing for the forthcoming team mailbox, not activation.
No dependencies or open Go domain objects are added.

Validation: independent improvement/httpapi tests passed immediately after the
extraction; final combined Go full test/coverage, full race and vet gates passed.
Existing source archive fixtures retain coverage. Full Python regression suite
also passed (2,155 tests, 28 skipped, 24 subtests). Runtime topology is unchanged.

## 2026-10-08 Python mailbox path components

Source review for the Go team port found that the allowed-name regex admitted
standalone dot path components. MessageBus now refuses those before any filesystem
access; ordinary dotted names retain their behavior. Four regression cases cover
send, peek/read refusal and absence of filesystem effects. The named mutation
removes the predicate and is caught. Trusted-root symlinks remain outside this
lexical boundary. Native mailbox and owned team view continue in separate commits.

Validation: 66 focused tests passed; full isolated Python suite 2,155 passed,
28 skipped, 24 subtests passed, three dependency deprecation warnings in 146.79s.
verify_invariants: 77 modules; verify_scans: 19 anchored guards. Both selected
verify_guards runs (mailbox-dot-segment-escape and team-peek-consumes-the-inbox)
passed; the complete mutation catalogue was not rerun.

## 2026-10-08 owned improvement proposal POST

Default POST /sessions/{session_id}/propose-improvement now binds admitted owner,
manager archive and existing session execution workspace under atomic try
admission for the complete managed proposal. Request values are concrete typed
strings/int64/optional ProposalID; unknown owner/workspace/archive overrides are
ignored as in source. Shared transport decoding retains closed diagnostic values;
the request model performs source length, integer/string/boolean/float coercion,
default and [1,10] bounds. Validation precedes owner lookup; busy precedes logical
blank-command/non-Git admission. Operator-correctable admission errors are typed
400s; missing/foreign targets are 404, busy is 409, other faults private plain 500.
Retained nonfinite/deep diagnostic values fail privately; ignored data stays ignored.

The earlier next-step assumption of automatic isolated-worktree/rate admission
was checked against source and corrected: this route neither creates a worktree
nor consumes the common rate budget. The operator supplies the isolated checkout
through existing session creation/binding. Source stages all changed paths.
Success returns the direct source projection, while events/index retain masks.
Snapshot 106 captures 41 actual FastAPI outcomes with an observed proposal effect
seam and real blank/non-Git refusals. Native TCP tests exercise actual worker file
write, acceptance shell, Git commit, manager-owned lineage and owner-scoped GET.
Additional cases ensure rejected callers never reach the provider and proposal
calls preserve the message rate budget. The shared personal-skill transport
refactor retains its existing validation behavior. Canonical service scalar text
and native counter profile remain explicit; live-model convergence is unverified.

README boundary/Mermaid and architecture specification describe live GET/POST.
HTTP now has 39 operations across 36 patterns versus source 44. Remaining routes
are team and workflows; native persistence and the full G0–G7 audit remain open.

Validation:

- `go test ./... -count=1 -timeout=180s -coverpkg=./... -coverprofile=…`
  passed; shared statement blocks were deduplicated by source location, retaining
  the maximum execution count. Overall: 17,047/18,911 (90.14%); agent 90.80%,
  httpapi 90.22%, selfimprove 88.79%, verifiedloop 95.88%, improvement 96.41%.
  These are Go statement coverage figures, not migration completion estimates.
- `go test -race ./... -count=1 -timeout=180s` and `go vet ./...` passed.
- `export_go_contracts.py --check`: all 106 snapshots current; two dependency
  deprecation warnings (Starlette/httpx and the historical Anthropic model).
- `verify_scans.py`: all 19 scanning guards anchored.
- Isolated `.venv/bin/python -m pytest -q`: 2,151 passed, 28 skipped,
  24 subtests passed in 153.17s; three dependency deprecation warnings and one
  asyncio subprocess cleanup warning (`Event loop is closed`), exit 0.
- Python package invariant/mutation sweeps were not rerun: only the contract
  exporter changed, with no Python package or guarded runtime behavior changes.
- Architecture regeneration: 9/9 checks, zero errors/warnings. Specification SHA
  `37156bc2a6bad26253aebc51a06ecbc766f2bc4e35b59682a524f20072ff1130`;
  HTML SHA `b2c9e5d091cfa01a05e41a2829600be1c8b021623c93e25f57701a408bdbdb64`.
  Visual inspection remains skipped after the earlier tool denial.
- Commit split: shared request transport is independent of the proposal route;
  its staged source was checked in a temporary HEAD archive with existing Python
  HTTP fixtures: `go test ./httpapi -count=1 -timeout=180s` passed (12.760s).
  The initial overlay attempt could not resolve an import during vet, and the
  first archive attempt lacked the cross-language web UI fixture; both setup
  limitations were resolved before the successful isolated check.
- README structure and `git diff --check` passed.

## 2026-10-08 native Git proposal and managed lineage

selfimprove.Service now composes real receipted verification with fixed Git
status/add/commit/diff/branch commands and optional archive lineage. Git-only and
nonblank acceptance admission match source. Unverified attempts still create
reviewable commits. Failed add/commit names the working-tree change rather than
someone else's HEAD~1; a failed add still attempts the fixed commit as in source.
Verifier touch flags retain source order/repeats/rename parsing and Unicode
splitlines. Proposal metadata distinguishes absent archive from attached lineage
with a null parent; archive/event values are detached before callbacks.

Snapshot 105 executes actual Python proposal composition and its actual verified
coordinator in 17 scenarios, comparing full proposals, command/event ordering,
archive producer fields and proposed-event payloads. Native temporary checkouts
prove actual branch commits, clean status, fixed commit message, diff stats,
owner-scoped lineage and unverified-but-committed artifacts. Callback exceptions/
panics and cancellation abort without manufacturing a successful result.

ManagedSession.ProposeImprovementWithContext holds one admission across verified
worker effects, Git, archive and typed improvement_proposed publication. It binds
the session owner/root/executor, keeps ordinary worker gates and uses verified
lease checks at command/archive/event/return boundaries. Tests run a real child
write and commit; event projections mask credentials and detach lineage pointers.
A parked Git-status test refuses a competing turn and cancels before stage/commit.
The shared verified-effects turn scope preserves the previous managed verified
API. Source stages all changed paths, so the operator must supply an isolated
checkout. This API does not create/merge branches and installs no HTTP/model tool.
HTTP remains 38/44 operations (35 patterns). Next bind the manager's isolated
worktree/owner/rate admission and POST, then teams/workflows/native persistence
and the full G0–G7 audit. Live-model convergence remains unverified.

Validation passed: focused proposal/managed verified/stored-event tests; full Go
tests and race checks (-count=1 -timeout=180s), go vet ./..., all 105 contract
exports and 19 scan anchors. After every preceding handle finished, isolated
Python pytest passed 2151 tests, 28 skipped and 24 subtests in 79.51s, with three
dependency deprecations. Export reported two dependency deprecations. Python
package invariant/mutation sweeps were not rerun because only the exporter
changed, not package modules or guarded Python behavior. Coverage was not refreshed.
README outline and git diff --check pass. Regenerated architecture passes all nine
automated checks with zero errors/warnings; visual inspection remains skipped
after the earlier permission denial.
Spec SHA256: b280ca043e8cc1436c1d46213517947849c92f4320d7acec8853bdec01af7cde.
Artifact SHA256: d78cd1a3eb0757a4d06f02fdb41ab7c9f8555283f0e82eff1a9915a0bc29b49c.

## 2026-10-08 managed verified task binding

ManagedSession.RunVerifiedWithContext now owns one admitted active turn across
the entire execute/accept/fold task. Real worker subagents retain the role/tool
gate, owner/workspace binding and peer provenance; parent human capabilities are
not inherited. Acceptance uses the configured structured command executor directly,
matching the source trusted-operator path. String-only command executors are
refused; workspace-aware executors must match the execution root. Optional
instrument probes sample that root. Typed round/receipt/checkpoint events use
ordinary masking, stored decoding, trajectory and subscription publication.
The common managed-operation seam preserves ordinary run admission, cancellation,
lease binding and terminal commitment. Parent transcripts remain parent-owned.

Because verified tasks do not grow parent transcripts, configured leases are
renewed before worker, acceptance and verified events and at the return boundary.
Missing leases are not reacquired inside the task; uncertain renewal errors abort.
These are explicit native embedding checks, not a heartbeat, continuous process
fencing or proof of OS confinement. The caller still owns external owner admission.
Existing source snapshot 104's three event variants round-trip without wire changes.
Native tests run actual child filesystem writes and shell acceptance, inspect
authority, deny worker writes, cancel before acceptance, refuse occupied managed
admission, reuse cancelled sessions, and refuse foreign leases after round,
worker-end and checkpoint events without publishing a successful terminal.
Stored projections retain masking and detached receipt metadata.

README Mermaid/boundaries and architecture specification now show explicit managed
verified task admission. The managed-to-session connection carries the new flow;
the generated diagram avoids a redundant crossing over the separate archive view.
No model tool, default task policy or HTTP route is installed; HTTP remains 38/44
operations (35 patterns). Next compose proposal Git artifacts and archive lineage,
then proposal POST, teams/workflows, native persistence and the full G0–G7 audit.

Validation passed: focused managed verified/stored-event tests; full Go tests and
race checks (-count=1 -timeout=180s), go vet ./.... Once every Go handle finished,
isolated Python pytest passed 2151 tests, 28 skipped and 24 subtests in 85.58s,
with three dependency deprecations. Python runtime/exporter/scanner/guard targets
are unchanged, so export freshness and separate Python invariant/scan/mutation
sweeps were not rerun. Source event comparisons use the existing 104th snapshot.
Coverage was not refreshed this iteration. README outline and git diff --check
pass. Regenerated architecture passes all nine automated checks with zero errors/
warnings; visual inspection remains skipped after the earlier permission denial.
Spec SHA256: 49603aefd50082251396a5c88de10fff5b45998300d9b1e36955ba53d5fa0371.
Artifact SHA256: 4bb0d78f743654563dbc648ff838cf704fe1b7f8e414d7a29a23a3c91835a883.

## 2026-10-08 verified-loop execute / accept / fold coordinator

Service now drives execute -> pre-acceptance integrity -> actual acceptance ->
receipt -> pure fold, using concrete typed effect seams and a closed telemetry
union. Source snapshot 104 compares 22 real Python coordinator scenarios, complete
receipts/checkpoints/outcomes, feedback objectives and exact effect/event order.
Source success remains exit zero, !timeout, !tamper; overflow/error flags do not
independently alter it. Initial nil sample disables probing. Later restoration
can verify with clean final integrity while preserving earlier suspect receipts.
Native ShellAcceptance and WorkspaceIntegrity use real configured shell/filesystem
effects; integration tests run workspace commands, tamper then restore, and prove
restoration during acceptance cannot undo the pre-run tamper decision. Additional
native tests prove cancellation, private panic refusal and telemetry detachment.
Callback errors abort, never manufacture receipt success. Source rune truncation,
default three/explicit zero/negative rounds and stop-leading unverified summary
remain intact. Coordinator state is per-call; callbacks require appropriate
embedding synchronization. Existing scalar text/signed-counter profile remains.
Next bind actual managed-session worker role, acceptance workspace/approval policy
and typed events through the existing admission/cancellation/recording pipeline.
Then compose proposal Git artifacts and POST, continue teams/workflows/native
persistence and the full G0–G7 audit. This operator coordinator installs no model
tool or default runtime service; HTTP remains 38/44 operations (35 patterns).

Validation passed: focused verifiedloop tests; full Go tests and race checks
(-count=1 -timeout=180s), go vet ./..., all 104 contract exports and 19 scan
anchors. After those handles finished, isolated Python pytest passed 2151 tests,
28 skipped and 24 subtests in 84.06s, with three dependency deprecations. Export
reported two dependency deprecations and an existing child-process cleanup warning;
the check passed. Python package invariant/guard sweeps were not rerun because
only the exporter changed, not package modules or guarded behavior.
Fresh Go shared-package coverage (-coverpkg=./..., duplicate blocks merged by
maximum execution count) is 90.14% (16682/18507), verifiedloop 95.88% (326/340),
improvement 96.41% (403/418), agent 90.84% (6178/6801), HTTP 89.70% (1341/1495).
These are statement coverage, not migration completion; Python coverage was not
refreshed. README outline and git diff --check pass. Regenerated architecture
passes all nine automated checks with zero errors/warnings; visual inspection
remains skipped after the earlier permission denial.
Spec SHA256: 11a43a22737637ef67633a26163105ab22a774ce991a69d0a595023c104dc657.
Artifact SHA256: ea825763a95617efa22b63849cf9335c9a58a31b20902a7c9c6144bb33313c3f.


## 2026-10-08 pure receipted verification folds

This slice ports verified_loop.py as the standalone verifiedloop package. Named
IDs, signed revisions and status/verdict/integrity variants replace string tuples;
TaskContract/Checkpoint/Receipt/RoundPlan hold private detached snapshots. Five
closed patch operation constructors carry only their named typed payloads. The
pure ApplyPatch refuses stale CAS, revision mismatch, every foreign supporting
receipt and receipt-free verified status, applying operations atomically.
Source snapshot 103 captures 32 real folds/refusals and complete byte-identical
contract hashes and canonical checkpoints, including Unicode, duplicate state
IDs, extra state IDs, typed artifacts/facts, blocker multiplicity and all receipt
verdict/integrity combinations. Source first lookup versus last duplicate fold
and metadata-excluded hash behavior remain explicit. Native tests prove detachment,
replay, constructor/default rules, Unicode whitespace and overflow refusal.
Native counters are signed 64-bit; canonical identity admits scalar UTF-8. The
source arbitrary-width/non-scalar profile is not claimed. Receipt data is not a
signature or evidence of an executed acceptance command by itself.
Next port VerifiedLoopService execute/accept/fold composition, integrity sampling
before each acceptance run, timeout/feedback/round exhaustion and typed telemetry.
Then bind real session worker/acceptance effects, compose proposal Git artifacts
and POST, continue teams/workflows/native persistence and the full G0–G7 audit.
No runtime/model feature is activated; HTTP inventory stays 38/44 (35 patterns).

Validation passed: focused verifiedloop source/native checks; full Go tests and
race checks (-count=1 -timeout=180s), go vet ./..., all 103 contract exports and
all 19 scan anchors. After those handles finished, isolated Python pytest passed
2151 tests, 28 skipped and 24 subtests in 107.97s, with three dependency deprecations.
Export reported two dependency deprecations and the existing child-process warning,
but passed. Python package invariant/guard sweeps were not rerun because only the
exporter changed, not package modules or guarded Python behavior. Overall coverage
was not refreshed this iteration. README outline and git diff --check pass.
Archify passes 9/9 showcase checks with zero errors/warnings; HTML is regenerated.
Visual inspection remains skipped after the earlier denial, without retry.
Specification SHA256: c66040e13d0f708d454515736296f6898bf09edfc953b9aaa4d4358c66f73fb8.
Artifact SHA256: 8ceb5a62c1a204fd5c3d93ff4b25570c9c6fd0a39dcf4ace9c82b2e76ee9a64c.


## 2026-10-07 owned improvement lineage HTTP

Default GET /improvements now reads the manager-owned .improvements archive.
Configured authentication binds the admitted owner; an open deployment reads
the fleet. Query/body overrides are ignored, the default 200 accepted-row limit
is fixed, and GET spends no rate budget. Source snapshot 102 compares 46 actual
HTTP responses, including historical secret retention, unknown/scalar legacy
rows, owner/query/method admission, empty/IO results, and private serialization
failures. The 200-row profile uses a full response digest for compact fixtures.
The constructor performs no archive IO; the private archive is rooted at the
manager workspace, independent of bound/factory session workspaces.
ArchiveValue preserves lone-surrogate text in read accessors, but standard JSON
serialization now refuses it, like the source UTF-8 HTTP response boundary.
Nonfinite/read/scoped shape errors also become plain private 500s before headers.
Native HTTP tests exercise byte admission, rate neutrality and zero model/session
work. README Mermaid, boundary text and architecture JSON describe the live GET
through the manager; generated HTML is refreshed. Inventory is now 38 operations/
35 patterns versus source 44. Next implement the receipt-gated verified loop,
then proposal Git composition and POST; continue teams/workflows, native
persistence and the complete G0–G7 release audit.

Validation passed: focused archive/manager/HTTP tests; full Go tests and race
checks (-count=1 -timeout=180s), go vet ./..., all 102 source contract exports and
all 19 scan anchors. After those handles finished, isolated Python pytest passed
2151 tests, 28 skipped and 24 subtests in 78.55s, with three dependency deprecations.
Export reported two dependency deprecations. Python package invariant/guard sweeps
were not rerun because only the exporter changed, not package modules or guarded
Python behavior. Overall statement coverage was not refreshed this iteration.
A current route/OpenAPI comparison confirms 38 operations/35 patterns versus 44,
with exactly POST propose-improvement, GET team and four workflow operations absent.
README outline and git diff --check pass. Archify passes 9/9 showcase checks with
zero errors/warnings; HTML is regenerated. Visual inspection remains skipped
after the earlier denial, without retry. Exact source recursion profiles at both
archive parse and HTTP response traversal remain release-audit work.
Specification SHA256: 98a5a2626cb1d29fafd26d210eabcd3b3ece3a5879f08bd6308af4f03967a6ce.
Artifact SHA256: 59483e044641eb4a4e4a263e89a5a72e6c988d35165549524c8b65c370c74c8d.


## 2026-10-07 improvement archive compatibility reads

Archive.List now implements newest-first legacy JSONL reads, trusted owner
filtering and accepted-row limits. ArchiveValue is a closed private-field union
for null/text/integer/float/boolean/array/object data, preserving unknown fields
and non-object legacy rows without broad Go payloads. Source snapshot 101 compares
27 real reads using full canonical row digests; native accessors detach containers.
The reader does not take an append lock or remask historical rows. Source IO
failures mean []; malformed JSON lines are skipped, while encoding, integer-limit
and owner-shape errors abort the whole query. Nonfinite numbers remain typed and
observable but standard MarshalJSON refuses them. The whole file is read, matching
source. Native profile accepts 1000 nested containers; exact Python stack-dependent
recursion cutoff remains unverified beyond 500/1005 contract samples.
Next: bind the owned GET /improvements view, port verified-loop receipts and
acceptance execution, compose proposal Git artifacts and POST route, then continue
teams/workflows, native persistence and the G0–G7 release audit. HTTP stays 37/44;
no model/runtime/default feature is activated by this operator library.

Validation passed: focused source/native archive checks; full Go tests and race
checks (-count=1 -timeout=180s), go vet ./..., all 101 source contract exports and
all 19 scan anchors. After those handles finished, isolated Python pytest passed
2151 tests, 28 skipped and 24 subtests in 80.81s, with three dependency deprecations.
The export check reported two deprecations and the existing child-process warning,
but passed. Python package invariant/guard sweeps were not rerun because only the
exporter changed, not package modules or guarded behavior. Overall statement
coverage was not refreshed this iteration. README outline and git diff --check pass.
Archify passes 9/9 showcase checks with zero errors/warnings; HTML is regenerated.
Visual inspection remains skipped after the earlier denial, without retry.
Specification SHA256: 5aa059eb4e0d44a9850b874b0263c814785bd6e8d1c2b1210ad910fab0a2d565.
Artifact SHA256: d6c8a71e421d66fbfdbcb908c59027a44fe57791034d2a5d663abc6e50e36fa2.
The HTTP slice must compare legacy nonfinite/lone-surrogate serialization failures
as well as owner admission and successful lineage responses.


## 2026-10-07 improvement archive append

This slice adds typed ProposalFields, ArchiveRecordOptions and best-effort
archive.jsonl append with owner/parent metadata and a concrete proposal ID.
Snapshot 100 captures seven actual Python append recipes. Masking includes keys,
values and verifier lists, preserving last-key collisions; projection failures
abort without raw fallback. Filesystem failures return the allocated raw ID,
matching the source index contract. The branch commit remains authoritative.
Native tests cover concurrent complete rows, append preservation, detached input
projection, masking before the IO lock, private panic refusal and entropy failure.
The constructor performs no IO; the mutex has only per-instance authority.
Legacy arbitrary JSONL reading and owner narrowing remain the next small task;
verified-loop receipts, proposal Git composition, both HTTP operations and G0–G7
remain open. No runtime/default/HTTP feature is activated. README Mermaid and
architecture JSON reflect the added operator append seam; HTML is regenerated.

Validation passed: focused improvement tests; full Go tests and race tests
(-count=1 -timeout=180s), go vet ./..., all 100 source contract exports and all
19 scan anchors. After those handles finished, isolated Python pytest passed
2151 tests, 28 skipped and 24 subtests in 101.76s. Its four warnings include
three dependency deprecations and the existing subprocess destructor/closed-loop
warning; this slice does not fix that warning. Export also reported dependency
deprecations and a child-process returncode warning, but its check passed.
Merged Go statement coverage was refreshed with -coverpkg=./...: 16047/17856
(89.87%); improvement 122/131 (93.13%); archive.go 73/81 (90.12%).
README outline and git diff --check pass. Python package invariant/guard sweeps
were skipped because only the exporter changed, not package modules or guards.
Archify passes 9/9 showcase checks, zero errors/warnings; HTML is regenerated.
Visual inspection remains skipped after the earlier denial, without retry.
Specification SHA256: 713639d05d114e34c0350d6a9f4b45f6ed2f1fbbe78e0a520d43689845d33d77.
Artifact SHA256: 2fb63445096ea10a8ec98d56b94f408563256cd83e9220d3f902e59d825f3df4.


## 2026-10-07 improvement acceptance-instrument checks

The proposal route requires native archive and verified-loop composition. This
first prerequisite ports the source verifier path classifier and filesystem
fingerprint, using a fixed 16-byte InstrumentFingerprint instead of broad payloads.
Snapshot 99 captures three classifier profiles and ten actual source digests.
Native tests cover actual filesystem recipes, changed/restored contents, symlinks,
Unicode/binary bytes, literal workspace metacharacters, read-failure markers,
stat/scan fault distinctions and malformed-name refusal via a detached APFS entry.
The four source globs retain their ordering and coverage; substring touch flags
remain broader. Callers must sample before each acceptance judgment, since a
restored final digest cannot reveal earlier tampering by itself.

Next cohesive tasks:
1. Port verified-loop receipts and acceptance execution (archive append/read and GET are now implemented).
2. Port verified-loop task/receipt/integrity transitions and acceptance execution.
3. Bind proposal worktree/git artifact composition, verifier touch flags and archive.
4. Bind and compare both improvement HTTP operations, then continue teams/workflows,
   native persistence and the G0–G7 release audit.

This explicit library changes no runtime feature default or HTTP inventory
(37 operations versus source 44). README canonical Mermaid and architecture JSON
now show the operator-only instrument library; HTML is regenerated.
Validation passed: focused source/native filesystem checks; full Go tests and
race tests (`-count=1 -timeout=180s`), `go vet ./...`, all 99 contract exports
(`--check`) and all 19 scan anchors. After all preceding handles were terminal,
isolated Python pytest passed 2151 tests, 28 skipped and 24 subtests in 86.53s,
with three dependency deprecations. Export reported two dependency deprecations.
`git diff --check` and README outline pass. Python package invariant/guard mutation
sweeps were not rerun because no package module or guarded Python behavior changed.
Archify passes 9/9 showcase checks with zero errors/warnings; HTML is regenerated.
Visual inspection remains skipped after the earlier denial, without retry.
Specification SHA256: 12d49686f19841a0d4df7789b8474b14d51806fc2341ceb7ad5a918313fd022a.
Artifact SHA256: d5ecf56415c28b86d75c14dad051e7d7adbe4aa8d4dba738058a79b4f2c15929. Overall coverage not refreshed this iteration.


## 2026-10-07 bound self-audit model tool

SelfAuditTools installs the read-risk/readonly/exclusive tool through the existing
execution gate. A typed observer and finite trusted visibility policy bind owner
scope by default; explicit operator configuration retains the source fleet report.
The shared manager construction map binds the actual manager for new/restored/
forked runtimes. Selected children inherit the view without acquiring capabilities
in default role policies. Bare activated sessions retain the source no-manager
notice. Observer panic messages remain private; cancellation surrounds collection,
which never reenters the active turn lock. Existing guards, journal, permissions,
result hooks and masking remain in order. The source report cap precedes those
normal result hooks/projection.

--self-audit-tools selects the individual service; configured auth chooses owner
view and open startup chooses operator view. This explicitly corrects the Python
model handler's unscoped report under auth. No model argument can widen scope.
Full feature-bundle activation, seven HTTP operations and G0–G7 remain open.
Snapshot 98 now captures the source no-manager handler result. Native tests cover
actual model loops, active managed reports, owner/operator diagnostics, read-only
traits, selected/default children, guard refusal before collection, masked results
and local HTTP provider launcher execution in open/authenticated deployments.
The README canonical Mermaid and architecture JSON describe the bound model-tool
flow via the gate; HTML is regenerated from that JSON.

Validation passed: focused runtime/launcher/CLI tests, full Go tests and race
checks (`-count=1 -timeout=180s`), `go vet ./...`, the separate shared coverage
run, all 98 source contract exports (`--check`) and all 19 source scan anchors.
After all preceding process handles were terminal, isolated Python pytest passed
2151 tests, 28 skipped and 24 subtests in 90.47s, with three dependency deprecation
warnings. Export reported two dependency deprecations. No earlier subprocess
cleanup warning fix is claimed. `git diff --check` and README outline pass.
Python package invariant/guard mutation sweeps were not rerun because no package
module or guarded Python behavior changed. Archify passes 9/9 showcase checks,
zero composition errors/warnings, after routing the new semantic model report
edge from the session with an explicit via-gate label. HTML was regenerated from
JSON; visual inspection remains skipped after the earlier denial, without retry.
Specification SHA256: b7500581de1d3c45421f38a4577256267e9e55fac48af86838d017cd0d8281de.
Artifact SHA256: 49694cdf167f9b49dfbdebe129cae27884b1c187732dfde515611dec4a78f35b.
Fresh Go statement coverage uses `go test ./... -coverpkg=./... -coverprofile=<path>
-count=1 -timeout=180s`, followed by the merged profile's statement-weighted totals.
The profile covers this iteration's code (9e102f4 plus bound model composition):
15926/17725 statements, 89.85% overall; agent 6170/6793 (90.83%), httpapi 1325/1479
(89.59%), protocol 1241/1372 (90.45%), launcher 152/189 (80.42%), benchmark 406/443
(91.65%) and selfaudit 228/231 (98.70%). New self_audit_tool.go is 16/17 (94.12%).
The raw profile is local temporary output; these counts and the command retain
reproducible evidence. Feature parity remains tracked independently.


## 2026-10-07 self-audit model-input contract

This focused protocol step adds a concrete empty SelfAuditInput variant, source
schema and constructors without activating the tool. Snapshot 98 captures actual
Python installation metadata and five handler keyword admission cases. Native
checks cover unknown scope fields, non-object/trailing input, canonical replay
JSON, masked copies, provider tool-use block round trips and detached schemas.
The default catalogue stays unchanged. Runtime handler, trusted visibility
binding, launcher selection and child propagation are the next composition task;
G0–G7 remains open. Python's model handler reads an unscoped manager report, so
that visibility must be addressed explicitly at trusted runtime composition.
README baseline was reviewed. This changes protocol contracts only, with no
runtime topology, authority, feature-default or HTTP inventory changes; existing
Mermaid and generated interactive map remain accurate.

Validation passed: focused protocol tests, full Go and race tests
(`-count=1 -timeout=180s`), `go vet ./...`, all 98 contract exports (`--check`)
and all 19 source scan anchors. After all preceding process handles were terminal,
isolated Python pytest passed: 2151 tests, 28 skipped, 24 subtests in 160.49s.
It reported three dependency deprecations plus the previously observed ignored
BaseSubprocessTransport destructor / closed-event-loop warning. No cleanup fix
is claimed. Export passed with two dependency deprecations. `git diff --check`
and README outline pass. Python package invariant/guard mutation sweeps were not
rerun because no package module or guarded Python behavior changed. The generated
map did not require regeneration for this protocol-only step. Overall coverage
was not refreshed.


## 2026-10-07 self-audit HTTP composition

Three default GET routes now serve the actual report, typed objective suggestions
and inadmissible benchmark drafts. Authentication scopes observation to the
admitted principal and disables fleet inclusion; open deployments use the
operator view. Query/body scope/limit overrides, including malformed JSON bodies,
are ignored. The source GETs do not spend rate budget or launch/admit any work.
Global auth/body admission still precedes handlers; query tokens stay event-only.

A finite private collection mode shares owner handle admission and ledger capture
without forcing Info, cron overview or trajectory IO for curation. Failed snapshot
collection is not converted to an empty suggestion/draft array; those handlers
return private plain 500s, while report sections retain class-only failures.
Typed JSON uses the existing masking projection. Plain reports are masked before
writes and recapped at the source Unicode limit after expansion; projection
panics cannot fall back to raw text. This extends source self-audit masking and
retains the native output boundary.

Snapshot 97 captures 72 actual source HTTP responses across open/authenticated/
owner-resource deployments. Only generated session IDs are replaced by labels.
Native tests exercise no recording IO/turns/jobs, no-rate behavior before and
after exhausted turn budget, body admission before auth, shared-memory filename
omission, class/private fault boundaries, report caps and mask expansion.
HTTP inventory is 37 operations/34 patterns versus source 44. The seven remaining
improvement/team/workflow operations and self_audit model tool remain separate
composition steps; native persistence, other gaps and G0–G7 remain open.

Validation passed: focused actual-source/native route tests; full Go tests and
race tests (`-count=1 -timeout=180s`), `go vet ./...`, all 97 contract exports
(`--check`) and all 19 scan anchors. After all preceding handles were terminal,
isolated Python pytest passed: 2151 tests, 28 skipped, 24 subtests, 3 dependency
deprecation warnings in 76.16s. Export also emitted the previously observed
BaseSubprocessTransport destructor / closed-event-loop diagnostic and two
deprecations; exit code was zero and all snapshot bytes were current. No cleanup
fix is claimed. `git diff --check` and README outline pass; Archify passes 9/9
automatic checks after rerouting the benchmark arm corridor for the new HTTP
edge. HTML is regenerated; visual inspection remains skipped after the earlier
denial and was not retried. Python invariant/guard mutation sweeps were not rerun
because no Python package module or guarded Python behavior changed. Overall
coverage was not refreshed in this iteration.

## 2026-10-07 live self-audit observation

SessionManager.ObserveSelfAudit now reads actual sessions, native problem holders,
cron jobs/arming and streamed recording tool-use events into the existing concrete
processing core. Owner handle admission precedes injected service IO; only the
recent 100 handles are inspected and fleet totals are preserved without broad Info
projection. Owner observations never contain the other owners' fleet total. Native
cron, trajectory, skill, task and gate adapters join approval/action snapshots.
Task observation uses the already-created runtime Store through an atomic pointer;
it does not initialize a task board. No manager/session lock spans IO/callbacks.

Source recording budgets apply before body IO (50 global, recent 20 owned sessions
with 10 each, cap 50, 200 tool-use events). Returned owner/session metadata is
checked. Transient known wire decoding adds a total 64 MiB budget and visible
ObservationLimitError. Callback panics and errors become private class-only
failures; event failures preserve successful trajectory trends.

Memory raw stores now count exact diagnostic occurrences. Bound views keep only
known-owner write/replacement occurrences, bounded separately per binding. Raw
unreadable filenames cannot establish ownership and remain privileged fleet data.
This intentionally corrects source ScopedMemory delegation; separate bindings do
not aggregate one another. Unattributed per-owner-resource store errors in fleet
views and native machine-counter overflow limits remain explicit gaps.

Snapshot 96 compares six actual Python managers/sessions: empty, mixed activity,
owner filtering, unknown owner, fleet and owned 100-session caps. No normalization.
Native tests read real runtime JSONL and exercise read admission/budgets, private
faults/panics, cancellation, concurrent snapshots, lazy task diagnostics and memory
isolation. Model-tool installation, three HTTP routes, stored-journal diagnostic
adapters and the broader G0–G7 migration remain open. HTTP stays 34 ops/31 patterns.

Validation passed: focused actual-source/native observer tests, full Go tests and
race tests (`-count=1 -timeout=180s`), `go vet ./...`, all 96 contract exports
(`--check`) and all 19 scan anchors. After every preceding process finished,
isolated Python pytest passed: 2151 tests, 28 skipped, 24 subtests, 3 dependency
deprecation warnings in 88.77s. `git diff --check` and README outline pass. Archify
passes 9/9 automatic checks after repairing the added edge's corridor; HTML is
regenerated. Visual inspection remains skipped after the earlier denial and was
not retried. Python invariant/guard mutation sweeps were not rerun because no
Python package module or guarded Python behavior changed. Overall coverage was
not refreshed in this iteration.

## 2026-10-07 exact problem ledger and diagnostic snapshots

Live self-audit collection exposed a prerequisite: approval and in-memory action
diagnostics discarded duplicate occurrence counts. The new problems.Log ports
source FIFO distinct retention, repeat/total/dropped counters, churn and clear,
with atomic detached snapshots. Source nonpositive capacities keep their exact
failed-append total increment; the native sentinel is ErrEmptyEviction. Counter
is an immutable exact nonnegative integer with copied big.Int storage, preserving
JSON integer and decimal report identity. selfaudit.Ledger.Total now uses it.

ApprovalBroker and InMemoryActionJournal mirror every diagnostic occurrence into
the source-sized 50-entry ledger before preserving their old 100-entry Problems()
API/deduplication. Optional ProblemSource supplies typed snapshots without
changing action/approval interface requirements. Real repeated reviewer faults
and action result shedding exercise these paths; denied permissions, private
panic omission, released results and retained replay identity remain verified.

Snapshot 95 captures nine actual source state sequences. Previous 26 self-audit
profiles stay exact, and native tests exercise concurrent reads/appends, detached
views and integers beyond machine bounds. Input strings are already rendered;
arbitrary Python object __str__ and inherited list mutation are not native APIs.

Live manager collection, other holder/stored adapters, runtime tool and HTTP
routes remain pending. Source ScopedMemory delegates its problems accessor to
the raw store; a shared store can hold diagnostic filenames for multiple owners.
The native collector must preserve owner attribution before exposing that ledger
in authenticated reports. No global fallback or fake empty route is added.
Inventory remains 34 operations/31 patterns; G0–G7 and full migration remain open.

Validation completed: full Go tests and race tests (`-count=1 -timeout=180s`),
`go vet ./...`, all 95 contract exports (`--check`) and all 19 scan anchors pass.
After every preceding process finished, isolated Python pytest passed: 2151 tests,
28 skipped, 24 subtests, 3 dependency deprecation warnings in 131.80s. The formatted
exporter also reproduces the new fixture byte-for-byte. `git diff --check` and
README outline pass; Archify regeneration passes 9/9 automatic checks. Visual
inspection remains skipped after the earlier denial; it was not retried. Python
package invariants and guard mutation checks were not rerun because this change
does not modify Python package modules or guarded behavior. Overall statement
coverage was not refreshed in this iteration.

## 2026-10-07 typed self-audit observation core

Concrete observations now support the full report processing core, typed
objectives and structurally inadmissible benchmark drafts. Source ordering,
owner filtering, bounded owner totals, global inclusion, problem counts/churn,
trajectory/skill scan limits, cron IDs and 8000-character report cap are retained.
Python whitespace and 300-character post-cap deduplication feed source SHA-256
draft names. Draft expectations encode/decode only null; no callable is stored.
No file/model/benchmark work is launched from these suggestions.

Snapshot 94 compares 26 actual Python processing profiles over controlled
observable manager/store seams, including scopes, missing fields, Unicode, scan
and report limits, safe failure classes and correlation. No output normalization
is applied. Native tests also prove snapshots remain unchanged and benchmark
constructors reject drafts lacking a reviewed judge. This is snapshot processing
evidence; live manager observation, self_audit model tool and three HTTP routes
remain pending. Source parity evidence covers scalar/finite observation profiles; arbitrary
malformed Python namespace objects are not modeled. The existing HTTP inventory stays
34 operations/31 patterns. G0–G7 and the complete migration remain open.

Validation:

- All 26 source profiles passed exact report/objective/draft comparisons, input
  immutability and native task-admission refusal. Null-only draft decoding passed.
  Focused selfaudit statement coverage is **98.7%**; overall Go/Python coverage
  was not refreshed.
- Full `go test ./... -count=1 -timeout=180s`, full
  `go test -race ./... -count=1 -timeout=180s` and `go vet ./...` passed.
- Exporter `--check`: all 94 files current; `verify_scans.py`: all 19 guards
  anchored. Python package modules and guarded mutation anchors are unchanged;
  separate invariant/guard mutation sweeps were not rerun.
- Initial full fixture generation emitted legacy child-watcher/closed-loop
  destructor diagnostics but exited zero. Subsequent complete exporter checks
  passed with only the two existing dependency/model deprecation warnings.
  No Python subprocess cleanup repair is claimed.
- README baseline/outline and `git diff --check` passed. Architecture source was
  regenerated with Archify and passed all 9 showcase checks on first delivery.
  Visual inspection remains skipped after the earlier local-file denial; those
  automated checks are not visual evidence. A doc-append variable mistake was
  repaired from the verified clean base, retaining only new owned sections.
- After every Go/export/scanner handle was terminal, isolated
  `.venv/bin/python -m pytest -q` passed: **2151 passed, 28 skipped,
  24 subtests passed, 4 warnings**, in 95.65s. Three are dependency deprecations;
  one is PytestUnraisableExceptionWarning from BaseSubprocessTransport destruction
  with an already closed event loop. That existing cleanup warning remains unfixed.

## 2026-10-07 fake-only benchmark HTTP composition

POST /benchmark now owns four fresh fake object clients, visible baseline/candidate
and heldout baseline/candidate arms. Typed FakeReport exposes visible rows and two
comparisons, with the exact source note and real=false. Every manager joins before
transient a/b roots are removed on success, cancellation and construction failure.
No main paid provider or owner snapshot enters the instrument. Explicit external
memory/trajectory roots retain configured semantics.

HTTP reads a fresh supported Settings environment profile per request, forcing
fake/workspace/no-spill and preserving the deployment skills path captured by the
app. Skills are loaded freshly per arm; native defaults use compiled builtin data.
Body/query knobs, including malformed JSON, do not select paid work or workloads.
Authentication, body cap and shared per-owner expensive-route rate budget precede
work. Source session creation does not spend this rate budget; message/fork/steer
do. Unsupported activated profiles fail privately and remain migration gaps.

Snapshot 93 compares five actual Python HTTP profiles: default, ignored body,
malformed JSON, real-main setting and fresh one-round environment. Only duration
and derived duration warnings are normalized; all deterministic rows/metrics and
comparisons remain exact. Source probes confirm four clients, 5/5/3/3 sessions and
removed workspace roots, then explicitly join captured source managers. Native
tests add live cancellation/construction cleanup, real main-provider isolation,
shared rate accounting, methods, fresh skill snapshots and private config errors.
HTTP inventory is now 34 operations/31 patterns, against 44 source OpenAPI
operations. The ten absent operations are improvements list/proposal, self-audit
and its suggestions/task drafts, owned team view and four workflow operations.
G0–G7 and the full migration remain open.

Validation:

- Focused benchmark HTTP/source profiles, rate, projection, fresh catalogue and
  live cancellation/construction cleanup tests passed.
- Full `go test ./... -count=1 -timeout=180s -coverpkg=./...` with a shared
  profile, `go test -race ./... -count=1 -timeout=180s` and `go vet ./...` passed.
  Deduplicated statement coverage: overall **89.60%** (15334/17114), agent
  **90.66%** (5969/6584), HTTP API **89.50%** (1287/1438), benchmark **91.65%**
  (406/443). This is statement coverage, not migration completion.
- Exporter `--check`: all 93 files current; `verify_scans.py`: all 19 guards
  anchored. Python package modules and guarded mutation anchors are unchanged;
  separate invariant/guard mutation sweeps were not rerun.
- Architecture source regenerated with Archify and passed all 9 showcase checks
  on the first delivery. README outline and `git diff --check` passed. Visual
  inspection remains skipped after the earlier local-file denial; automated
  composition checks are not visual evidence.
- After all Go/export/scanner handles were terminal, isolated
  `.venv/bin/python -m pytest -q` passed: **2151 passed, 28 skipped,
  24 subtests passed, 3 dependency deprecation warnings**, in 105.21s.
  Python coverage was not refreshed.

## 2026-10-07 owned benchmark arm execution

RunArm now owns one manager, captures admitted task storage before callbacks,
creates fresh anonymous interactive sessions and applies the task's immutable
catalogue subset. A custom caller-owned workspace factory must isolate paths;
default scratch paths are independent. Workspaces are retained for inspection.
Explicit empty/nil workloads mean no tasks; callers select visible/heldout lists.
Setup/create failures abort with a typed ArmError and original unwrapped cause.
Ordinary run/judge failures score failed rows; cancellation aborts. Panic handling
for trusted setup/judge callbacks reports the type without exposing panic values.
The manager is stopped/joined on every exit with an independent cleanup context,
a stronger native lifecycle guarantee than source run_arm's implicit lifetime.
Injected shared services retain caller ownership.

Duration measures only session.Run and uses monotonic time/one decimal millisecond;
setup/judge time is excluded. Cost/motion derive from actual final typed history,
not fabricated provider usage. Empty typed history now estimates zero tokens,
matching the source's empty list rather than native nil's JSON null spelling.
Provider failures recovered into ordinary final error text still reach the effect
judge. A separate ordinary session-entry failure skips the judge and scores a row.
Provider-returned cancellation now propagates even when the parent context remains
live, matching Python's BaseException cancellation boundary. A focused mutation
removed that cancellation check; the new source-recipe test failed as expected,
then the check was restored before the full gates.

Actual default-fake arms revealed a representation difference: raw FakeProvider
client replies correctly retain caller=null, while Python's non-SDK fake objects
omit caller when Agent._content_payload writes history. FakeProvider.ObjectView
now explicitly models that object profile; the launcher selects it. Raw client
wire tests and real-provider absent/null distinctions remain unchanged. This
fixes four excess history-estimate tokens per default fake task without inventing
a cost adjustment. Dictionary/SDK simulations keep the raw client profile.

Snapshot 92 captures eight actual source arm recipes: five visible tasks, three
heldout tasks, empty workloads, setup/entry-run/judge faults, provider-failure
recovery and provider cancellation. Wall time alone is omitted as nondeterministic;
source histories, tool catalogues, owner/mode, workspace file effects and all other
rows remain captured. The source entry-run fault is injected at AgentSession.run,
while the native equivalent uses a failing user-prompt hook before history append.
All eight default task effects/final texts/deterministic row measurements match.
Native stage/error diagnostic spelling is explicit: the recovered fault fixture
has 38 native versus 32 source tokens because the native bounded diagnostic adds
its Go error type before the scripted source error text. That actual native cost
is retained; arbitrary Go errors are not renamed to Python exception classes.

Native tests also exercise setup/judge panic/error boundaries, cancellation and
call joins, captured workload mutation, independent histories/workspaces, actual
advertised/executable whitelists, forced anonymous/interactive mode and measured
setup/judge exclusion. POST /benchmark's fake-only two-arm/default-plus-heldout
composition remains pending. Paid evaluation stays operator initiated; this
library is explicit operator launch authority. Native HTTP inventory remains
33 operations/30 patterns; the broader migration and G0–G7 remain open.

Validation:

- Focused source arm/fault/recovery/cancellation, fake client wire and native
  callback/capture/join tests passed. The targeted native cancellation mutation
  failed as expected; restoration preceded all full gates. This is one focused
  native mutation, not a full mutation sweep.
- Full `go test ./... -count=1 -timeout=180s -coverpkg=./...` with shared profile,
  `go test -race ./... -count=1 -timeout=180s` and `go vet ./...` passed.
  Deduplicated statement coverage: overall **89.60%** (15244/17013), agent
  **90.63%** (5967/6584), HTTP API **89.29%** (1259/1410), benchmark **94.32%**
  (349/370). Coverage is not migration completion.
- Contract exporter `--check`: all 92 files current. `verify_scans.py`: all 19
  scanner guards anchored. Python package modules and mutation anchors are
  unchanged; separate Python invariant/mutation verifier sweeps were not rerun.
- After every Go/export/scanner handle was terminal, isolated
  `.venv/bin/python -m pytest -q` passed: **2151 passed, 28 skipped,
  24 subtests passed, 3 dependency deprecation warnings**, in 108.03s.
  Python coverage was not refreshed.
- `git diff --check` passed; README outline checked. The regenerated architecture
  passed all 9 showcase checks after routing the new operator-arm edge around
  diagnosed crossings. Visual inspection remains skipped after the earlier
  local-file denial; automated checks are not visual evidence.

## 2026-10-07 admitted benchmark tasks and effect judges

`go/benchmark` now has immutable Task specifications, explicit trusted Judge/Setup
function types and optional detached protocol.ToolName lists. DefaultTasks returns
five visible tasks; HeldoutTasks returns the source's three heldout tasks. NewTask
requires a judge but preserves empty names/prompts; invalid zero tasks refuse
explicitly. Callbacks own their closed-over state and must honor cancellation.
Prepare/Judge propagate faults and check context before and after callbacks.
These definitions do not admit model-generated or improvement-draft callbacks.

Prompts and intentionally permissive source judges are preserved: arithmetic and
word count are substrings, config need not contain an actual section, append-log
only needs three splitlines, and nested-file only needs existence (a directory
counts). Greeting applies Python Unicode lower. File reads use strict UTF-8 and
universal newlines in the pinned environment; Python's Unicode splitlines includes
vertical/form feeds, file/group/record separators, NEL and Unicode line/paragraph
separators, but not the unit separator. Missing/broken/looping paths fail effects;
read/decode faults remain errors. Native OS error text is not Python exception
spelling. Judge callbacks are trusted host instrument code, not workspace tools
or sandbox enforcement.

Both paging setups write the exact 6000 lines/324000 bytes, overwriting prior
content as source does. SHA256: ad1732b4a905a4fa23e59188c6dc0c02efd5f4d31870b76648fb1b1e8f8cc6e1.
Snapshot 91 captures eight actual source specifications, 50 actual judge outcomes
and seeded-byte size/hash/first/deep/last lines. Native tests reproduce filesystem
recipes, including valid/broken/looping symlinks, directories, malformed UTF-8,
Unicode separators and permissive wrong-answer/content cases. They also verify
setup replacement, callbacks/errors/cancellation, immutable list/whitelist
storage and absent versus explicitly empty selection.

Fresh-session arm execution, actual fake-provider transcript/effect comparison,
timing/token/motion row measurement and POST /benchmark remain pending. The next
slice must use these tasks and real catalogue reduction, then preserve source
setup-aborts-arm versus run/judge-fault-scored-row boundaries. HTTP remains fake
only; paid-model evaluation stays operator initiated. Native route counts remain
33 operations/30 patterns; broader migration and G0–G7 remain open.

Validation:

- Focused admitted task specification/judge/setup/callback tests passed.
- Full `go test ./... -count=1 -timeout=180s`,
  `go test -race ./... -count=1 -timeout=180s` and `go vet ./...` passed.
- Contract exporter `--check`: all 91 files current. `verify_scans.py`: all 19
  scanner guards anchored. Python package modules and mutation anchors are
  unchanged; separate invariant and mutation sweeps were not rerun.
- After all Go/export/scanner handles were terminal, isolated
  `.venv/bin/python -m pytest -q` passed: **2151 passed, 28 skipped,
  24 subtests passed, 3 dependency deprecation warnings**, in 89.23s. The prior
  subprocess-destructor warning did not recur; no Python runtime fix is claimed.
- `git diff --check` passed; README outline checked. Regenerated architecture
  passed 9/9 showcase checks with zero errors/warnings. Visual inspection remains
  skipped after the earlier local-file denial. Coverage was not refreshed; the
  previous slice's percentages predate this added task code.

## 2026-10-07 immutable session tool selection

A prerequisite for the source benchmark's read-only paging task is now available
through `agent.SelectTools`, `RuntimeConfig.ToolSelection` and
`CreateSessionRequest.ToolSelection`. The named value has private immutable state:
constructor input is detached, the zero value retains all installed definitions,
and an explicit empty selection removes all tools. Unknown/duplicate names are
ignored, retaining source registry order. Filtering precedes the single gate's
construction, so model schemas and actual execution share the selected catalogue.
Names do not enable optional features or change permission modes.

The source evidence is `ToolRegistry.subset` and `benchmark.run_arm`'s admitted
`BenchTask.tool_names` reduction. Four live source probes confirmed reversed
selection order with duplicates/unknown names, empty selection, unknown-only
selection and write-only selection; parent registry order remained unchanged.
Native tests verify the default ten-tool profile, unavailable optional-tool names,
unknown/duplicate/empty/order semantics, unchanged modes, input detachment before
manager factory callbacks, independent sessions and actual advertised schemas.
Hostile write/Bash requests are refused through the gate in auto mode, while read
still succeeds. Explicit empty profiles refuse all forced calls and leave the
workspace untouched. An Explore child inherits the selected parent catalogue.

This is a transient construction profile, matching source runtime subset behavior;
independent creates, forks and restored handles retain their current configuration.
It is not a persisted authority policy or a host sandbox. Benchmark task definitions,
judges/setup, fresh-session arm execution, heldout work and POST /benchmark remain
pending; native routes stay at 33 operations/30 patterns and G0–G7 remain open.

Validation:

- Focused tool-selection/runtime/manager/child regressions passed.
- Full `go test ./... -count=1 -timeout=180s -coverpkg=./...` with a shared
  profile, `go test -race ./... -count=1 -timeout=180s` and `go vet ./...` passed.
  Deduplicated statement coverage: overall **89.57%** (15108/16868), agent
  **90.62%** (5954/6570), HTTP API **89.29%** (1259/1410), benchmark **94.14%**
  (225/239). These percentages measure tested statements, not migration completion.
- Contract exporter `--check`: all 90 files current. Python modules, exporters,
  scanner inventories and mutation anchors are unchanged; separate invariant,
  scan and mutation verifier sweeps were not rerun.
- After every Go/export job was terminal, isolated `.venv/bin/python -m pytest -q`
  passed: **2151 passed, 28 skipped, 24 subtests passed**, in 112.92s. Four warnings:
  three dependency deprecations and a subprocess transport destructor observing
  an already-closed event loop, reported during
  `test_the_owner_map_stays_bounded_across_many_deletes`.
  The latter source-suite warning is unresolved by this Go-only slice.
  Python coverage was not refreshed.
- `git diff --check` passed; README outline checked. Regenerated architecture
  passed all 9 showcase checks with zero errors/warnings. Visual inspection
  remains skipped after the earlier local-file denial; automated acceptance is
  not visual evidence.

## 2026-10-07 typed benchmark statistics

`go/benchmark` now implements the source aggregation, paired comparison and
transcript-motion component with concrete TaskResult/AggregateResult/Comparison
types and six explicit optional measurements. Number is an immutable closed
exact-integer/finite-double/boolean union. Integer constructors/accessors detach
big.Int values; the source interpreter's current 4300-digit JSON limit is pinned.
There is no any, open interface or retained raw JSON in this component's state.

Source behavior preserved: first-seen task order/arm, every supplied row as a
vote, strict majority (a tie fails), rounded pass rate, first nonempty error,
integer/float/bool median identity, exact mixed-number ordering and integer
midpoints beyond signed64. Comparison pairs the last row by task while totals
include all rows; task sets must match. Any regression overrides wins. Ordered
dimension warnings require a rounded delta strictly above 25%, and never change
the effect verdict. Missing/zero-base dimensions and signed-zero truthiness match.
Behavior counts assistant messages/tool calls, identical read windows and source
rendered failures, including Unicode decimal exit suffixes. is_error alone is not
the judge. Python decimal rounding uses the exact IEEE-754 value and ties to even.

Snapshot 90 compares 51 actual source outcomes: ten aggregations, fifteen paired
comparisons (including mismatches), six typed-transcript motion cases and twenty
rounding cases. Native tests add immutable/detached numeric and result state,
non-mutating failed decode, exact totals beyond signed64, finite arithmetic and
integer serialization limits. Nonfinite/overflowing float results refuse with a
typed native error; malformed arbitrary Python rows cannot enter concrete native
results. These pure probes do not execute models or judge task effects.

Next benchmark slices:

1. Port admitted visible/heldout tasks and their actual filesystem/final-text
   judges, setup functions and per-task tool whitelists.
2. Implement fresh-session arm execution with typed setup/expect seams, isolated
   configuration/workspaces, transcript token/motion measurement and explicit
   setup/provider/judge fault behavior. Compare actual source arm transcripts and
   effects using the stateful native fake provider.
3. Implement POST /benchmark's rate-limited, fake-only two-arm/default-plus-heldout
   composition and typed response. Source real-budget evaluation stays an
   operator terminal act; do not implicitly expose it through HTTP.
4. Continue teams, workflows, audit/improvement, native SQLite and the broader
   G0–G7 migration/release audit. This statistical component closes none of those
   remaining runtime/HTTP requirements.

Validation:

- Focused benchmark regressions, `go test ./...`, `go test -race ./...` and
  `go vet ./...` passed.
- Contract exporter `--check`: all 90 files current; `verify_scans.py`: all 19
  scanning guards anchored. Separate Python package invariant/mutation sweeps
  were not rerun because their modules and anchors are unchanged.
- After every Go/export/scanner job was terminal, isolated
  `.venv/bin/python -m pytest -q` passed: **2151 passed, 28 skipped,
  24 subtests passed, 3 dependency warnings**, in 99.62s.
- `git diff --check` passed. README outline checked; regenerated architecture
  passed 9/9 automated checks after correcting the new operator edge's endpoint
  sides. Visual inspection remains skipped after the earlier local-file denial.
  Coverage was not refreshed in this slice.

## 2026-10-07 owned memory HTTP views

Two cohesive delivery slices cover default embedded-manager storage and the
owned list/body view. `ManagedSession.MemoryRecords` retains the runtime's fixed
scoped binding, rejects missing/foreign bindings and returns detached current
records. The HTTP adapter requires the admitted owner before reading and exposes
concrete metadata/body types through the existing masked projection. Reading
does not install tools or resolve another resource bundle. Mutable memory writes
remain visible to existing same-owner sessions.

Snapshot 89 compares 45 actual Python HTTP responses across shared, owner-local
and anonymous deployments: empty lists, metadata/body, owner isolation, query
credential rejection, missing sessions/names, Unicode/quote/backslash names,
encoded slashes and later writes. Native tests additionally exercise cancellation,
corrupt bindings, detached records, secret masking and safe backing-store errors.
A removed native root yields safe plain 500; Python's glob produces an empty list.
The native inventory now has 33 method/path operations across 30 patterns; other
route groups, native SQLite and the overall G0–G7 migration remain open.

Validation for both delivery slices:

- Focused manager memory/fork/resource-cleanup and HTTP memory regressions passed.
- `go test -coverprofile=/tmp/mini-loop-commit-coverage.out ./...`,
  `go test -race ./...` and `go vet ./...` passed.
- `go test -coverpkg=./... -coverprofile=/tmp/mini-loop-commit-shared-coverage.out ./...`
  passed. Deduplicated statement blocks: overall 14866/16617 (**89.46%**),
  agent 5941/6558 (**90.59%**), HTTP API 1256/1410 (**89.08%**).
- Contract exporter `--check`: all 89 files current. `verify_scans.py`: 19
  scanning guards anchored. README outline checked; generated architecture passed
  9/9 automated checks. Visual inspection remains skipped after the earlier denial.
- The first isolated Python run was interrupted and is not counted as a pass.
  A fresh complete `.venv/bin/python -m pytest -q -o faulthandler_timeout=60`
  passed: **2151 passed, 28 skipped, 24 subtests passed, 3 dependency warnings**
  in 79.75s. The option adds stack diagnostics, without changing test assertions.
- Python package-module invariants and mutation anchors were unchanged; their
  separate verifier sweeps were not rerun. Python coverage was not refreshed.

## 2026-10-07 embedded manager memory binding

Nil `ManagerServices.Memory` now constructs `WorkspaceRoot/.memory` with the
configured masker, matching Python manager construction. The shared root must
construct even with owner-local resources selected. Runtime binding retains owner
isolation and resolver precedence; storage alone enables neither tools nor capture.
Focused regression tests cover default storage, isolated owners, unchanged tool
selection and a blocked shared root with and without an owner resolver.


## Outcome

Keep the existing Python implementation runnable in `python/` and build a
separate Go implementation in `go/`. The Go version must eventually cover the
same documented default runtime, public REST/SSE behavior, extension seams, and
opt-in capabilities, with any remaining differences listed explicitly. A Go
directory or a compiling stub alone is not parity.

## Baseline and boundaries

- Source baseline: `ad71e05` on `main` (2026-09-29). The Python package has a
  session manager, per-session agent loop, tool catalogue and execution gates,
  model providers, REST/SSE server, optional persistence, and feature bundles.
- Keep root `README.md`, `EXTENDING.md`, `docs/`, and `research-site/` as shared
  documentation. Move the Python package, tests, tools, examples, skills,
  requirements, test configuration, and Python environment example together.
- Treat Python tests and their documented behavior as the reference. Pin
  fixtures and observable API examples before changing a Go behavior.
- No new dependency is part of the directory split or initial Go core. A later
  third-party Go dependency needs separate explicit approval.

## Type rules for Go

1. Model protocol messages, content blocks, tool inputs and results, event
   variants, approval decisions, session state, and persistence records as
   named types. Use tagged unions with explicit discriminators and validation
   at JSON and provider boundaries.
2. Avoid `any`, `interface{}`, `map[string]any`, unchecked type assertions, and
   untyped JSON payloads in the domain and service layers. Raw bytes may exist
   only at ingress/egress adapters, must be size bounded, and must be decoded
   into a known variant before entering the runtime.
3. Represent optional values with pointers or named option types, distinct
   from zero values where the protocol distinguishes them. Use enums for
   finite states and exhaustive switches for transitions.
4. Keep request-scoped identity/authority immutable. Interfaces should be
   small and owned by their consumers; explicit constructors validate required
   dependencies. Preserve the Python guard ordering and failure behavior.

## Work packages

Each package below is small enough to review and validate independently.
Record its parity evidence and remaining gaps before checking it off.

| ID | Work | Acceptance evidence |
|---|---|---|
| P0 | Inventory Python entry points, contracts, defaults, storage schema, and enabled versus optional features | Parity matrix names every README capability and its Python tests |
| P1 | Move Python-owned files to `python/`; update imports, paths, scripts, and docs | Full Python suite and repository verifiers pass from documented commands; fake server starts |
| G0 | Create `go/` module and typed protocol/domain contracts | `go test ./...`, `go vet ./...`, JSON boundary tests, no broad dynamic domain payloads |
| G1 | Implement bounded session state, ordered transcript, fake provider, and serial per-session turn loop | Go tests replay Python fake-model fixtures, concurrent sessions isolate state |
| G2 | Implement immutable tool catalogue and guarded execution, permissions, approvals, cancellation | Ordering and denial tests match the Python contract; no bypass around the gate |
| G3 | Implement REST/SSE and browser assets against typed service APIs | Python/Go HTTP and SSE contract corpus matches for default routes and errors |
| G4 | Implement real provider transport, streaming, recovery, usage and cache behavior | Recorded protocol fixtures and interrupted-stream cases pass |
| G5 | Implement optional SQLite persistence, leases, owner isolation, trajectories and journals | Restart/crash-window and owner-isolation fixtures pass against both runtimes |
| G6 | Port opt-in skills, memory, MCP, background/cron/tasks/teams/worktrees, workflows, verified loop, and decisions in bounded slices | Each feature has an explicit flag/default, parity fixtures, and documented limitations |
| G7 | Run cross-language differential suite, performance checks, documentation/audit pass | Parity matrix has no unverified required rows; README default and durability labels are truthful |

## Sequence and gates

1. Complete P0 and write the contract matrix before large code translation.
2. Complete P1 as a path-only migration where possible. Keep its behavior
   changes separate from Go behavior changes. Run `python -m pytest -q`,
   `tools/verify_invariants.py`, `tools/verify_scans.py`,
   `tools/verify_guards.py`, and a fake-server smoke check.
3. Complete G0-G3 as a default-runtime vertical slice. Check every interface
   against the Python construction seam and every exposed route against a
   captured Python response or event fixture.
4. Complete G4-G6 in feature slices, retaining default-off labels until the
   feature works in Go. Add a Go check to CI only after a working test target
   exists.
5. Complete G7 before claiming the Go version replaces Python. Keep the
   Python tree available until parity is demonstrated, then decide whether
   either implementation becomes the default entry point.

## Current status

Reviewed 2026-10-10 at `552541f`. This section is the current delivery index;
implementation checkpoints below retain historical status at their own baselines.
An implemented feature is not a claim that its whole work package has passed the
final G7 scope audit. Original work-package acceptance remains in [Work packages](#work-packages).

| Package | Current implementation and evidence | Remaining acceptance |
| --- | --- | --- |
| P0 | Source contract inventory, HTTP inventory and generated Source recipes in `go/testdata/` | Reconcile the final inventory against current Python public entry points and defaults in G7 |
| P1 | Python under `python/`, Go under `go/`; original regression, verifiers and server smoke recorded | Refresh final release gates before merge/push |
| G0 | Named protocol/domain types, closed tool inputs/results/events, explicit services and archival decoders | Final audit of domain/service type boundaries; transient fixture/wire parsing is a distinct boundary |
| G1 | Managed serial turns, fake/HTTP providers, context/compaction/cache/recovery, hooks, steering, limiter/child integration and cancellation repair | Final cross-feature and default-path scope audit |
| G2 | Ordered risk/permission gate, actual tools/shell, approvals, secret projection and action replay; native SQLite actions/approvals implemented | Final authority/guard scope audit; stored replay never restores prior grants |
| G3 | Default/optional owned HTTP routes, SSE, console shells, typed validation, health posture, configuration and both CLI startup paths | Default full-feature CLI diagnostics backend and `MINILOOP_RELOAD` remain unavailable; audit public-entry parity |
| G4 | Typed HTTP transport/replies/SSE, SDK retries, Agent recovery, isolated decision calls and coalesced progress | Final supported-provider variant/options audit; no paid-provider deployment check has been claimed |
| G5 | Native schema-v7 SQLite, leases, transcripts/events/actions/approvals, explicit manager/Launcher restore, owner isolation, cron/scheduled restore; killed-generation recovery, compaction restart and 512 KiB decision replay verified | Reconcile G5 required Source recipes in G7; leases/cron claims do not fence external effects or prove power-loss durability |
| G6 | Tasks/worktrees/background/cron, goals/plan mode, skills/owner resources/memory, decisions, MCP, teams, workflows/verified improvement, diagnostics injection, self-audit and native benchmark arms implemented in explicit slices | Default Python syntax diagnostics dependency decision; final defaults/lifecycle/feature inventory audit |
| G7 | 199 exported Source recipe files freshly checked at `552541f`, native tests and documented adaptations | Public-entry/default/type audit, performance evidence and final regression/verifier gates; goal remains incomplete |

### Confirmed open implementation items

1. Default Python syntax diagnostics: native filesystem diagnostics supports an
   explicit typed `PythonSyntaxParser`, and Manager/Launcher full-feature composition
   works with an injected diagnostics provider. Default CLI full activation still
   refuses `MINILOOP_FEATURES`; choosing its proposed CPython backend requires the
   outstanding explicit dependency decision. SQLite driver approval does not approve it.
2. Development reload: `go/config/server.go` explicitly refuses `MINILOOP_RELOAD`.
   The Source launcher accepts this setting. A native implementation or a reviewed,
   explicit deployment adaptation is still needed; default-off does not remove the gap.

### Resolved items that must not remain on the implementation queue

- SQLite is implemented using approved `mattn/go-sqlite3 v1.14.52`; native opening,
  transactional stores and real reopen/restore tests exist in `go/sqlitestate/`.
- Launcher has explicit caller-owned state injection and restoration; construction
  and serving perform no automatic restore. See `go/launcher/state_test.go`.
- Native benchmark arm execution, heldout tasks and HTTP fake comparison exist in
  `go/benchmark/` and the benchmark HTTP adapter. The old “arm execution pending”
  note is superseded; complete performance acceptance still requires G7 review.
- Python manager scratch cleanup is **not Git-aware**: `_remove_workspace` removes
  the directory/link, and `_finalize_workspace_cleanup` checks live references.
  Go preserves this behavior. The worktree service's separate guarded Remove API
  is implemented. Git registration cleanup must not be invented as missing Source
  behavior; the actual Source cleanup limitation stays documented in README.

### Next decision slices

1. Closed JSON values, choice/score/noul contracts and an explicit operator Jev
   provider are implemented and compared with actual Python contracts and HTTP
   mocks. They add no runtime tool, route or activation flag.
2. Implemented: closed decision tool through the common external-risk gate;
   masked/revalidated explicit state, typed model/decision events, shared model
   limiter and isolated current-LLM queries with complete-response checks,
   recovery isolation and estimated-probability provenance. Native large-result
   replay is tested; snapshot 54 compares 31 actual LLM and eight gate cases.
3. Individual configuration/launcher selection is implemented, retaining
   default-off and explicit-provider precedence. Compare exact large result replay/retention,
   cancellation and sink masking before claiming complete decision parity.
   Snapshot 55 now compares maximum-size replay, aggregate shedding and Unicode
   bounds with actual source memory/SQLite-reopen operations. Native SQL reopen
   now also passes baseline and 512 KiB managed decision replay, fresh restored
   permission admission and readonly denial; see the 2026-10-10 restart proof.
   Snapshot 56 now compares cooperative cancellation and sink
   masking, including a deliberately stronger native escaped-result guard.

### Next persistence slices

The implementation queue from the earlier injected-store checkpoint is closed:
`go/sqlitestate` implements native schema-v7/session/lease/transcript/event/action/
approval storage, including concurrent connections and rollback recipes. Real
manager and Launcher tests close/reopen SQLite before restoring. Additional
native tests cover scheduled/cron reconstruction, a killed generation writer,
compaction success/failure/cancellation and baseline/512 KiB decision replay.

Remaining work is the G7 acceptance audit against the original G5 recipe scope,
not implementing another backend. Keep proof limits explicit: no power-loss or
external-effect fencing claim follows from these tests. Restore never resumes
human authority or arms durable cron/goal state.

### Next cron slices

Native scheduler/tools/operator HTTP selection and lifecycle are implemented.
The real SQLite scheduled-restoration recipes and managed one-shot reopen test
now verify lazy stable-ID restoration, current workspace selection, fresh untrusted
turn provenance and explicitly re-armed jobs. See
`go/sqlitestate/scheduled_restore_test.go` and `go/sqlitestate/cron_restart_test.go`.

Remaining work is G7 scope reconciliation. `.cron.json` and occurrence claims
remain independent of SQLite sessions/leases; they do not provide an exactly-once
transaction with external effects or repair stale whole-file state across live
writers. Default comprehensive CLI activation is blocked by diagnostics backend
selection, not by an unimplemented cron group.


## 2026-10-02 checkpoint

- The Python package, tests, tools, examples, skills, requirements, test
  bootstrap, and environment example now live in `python/`. The shipped skill
  default resolves relative to that implementation, independent of cwd.
- The fake Python server starts from the repository root with
  `PYTHONPATH=python` and returns HTTP 200 from `/healthz`.
- Python tests: 2150 passed, 28 skipped, and one timing test deselected in the
  broad run. The timing test passed alone in 0.36 seconds but failed in two
  complete runs at 1.30 and 2.45 seconds against its 0.5-second limit. One
  earlier complete run passed before the final config edit; P1 therefore
  awaits a clean final full run or a measured explanation of the variance.
- `verify_invariants.py`, `verify_scans.py`, and all 377 mutation guards passed;
  the guards were run before the package-relative skills default changed.
- The Go module now has strict JSON message/block variants for the fake bash
  path and an injected in-memory session loop. `go test -race ./...` and
  `go vet ./...` pass. HTTP, permission gates, storage, and most tool variants
  remain unimplemented.
- The Research Atlas build and five site tests pass. The architecture JSON
  regenerated HTML with all nine Archify checks passing and no composition
  warnings. Browser visual inspection of a local `file:` URL was unavailable
  because the browser policy blocked that protocol.

## 2026-10-02 continuation

- `python/tools/export_go_contracts.py` now exports the real default registry,
  44-operation FastAPI OpenAPI schema, SQLite v7 schema, and a small manifest.
  `--check` verifies byte-for-byte freshness without writing.
- Go has concrete, closed input variants for all ten Python default tools.
  The protocol tests use each variant, check required fields and enums against
  Python's exported schema, and reject unknown tools and extra input fields.
- The in-memory Go agent still executes only `bash`; it rejects another typed
  tool before appending an unanswered `tool_use` or invoking the bash executor.
- The final full Python suite passed: 2151 passed, 28 skipped, and 24 subtests
  passed, including the previously load-sensitive timing case. The earlier
  failures are retained above as part of the audit history.

## 2026-10-02 model reply slice

- Go now decodes completed model replies into named fields, including optional
  cache usage, and preserves unknown stop reasons for the session to report.
  The Python contract manifest pins its six known reasons, eight-resumption
  bound, and refusal notice. Python fake reply fixtures cover tool use, final
  text, empty refusal, and the optional typed caller field.
- The in-memory loop resumes `pause_turn` without inventing a user message,
  bounds repeated pauses, reports refusal and unknown stops through typed
  local events, and still executes only `bash`.
- Provider streaming, `max_tokens` recovery, request metering, the full event
  stream, and the guarded tool dispatcher remain open. The current fake usage
  reports zero input tokens because system/tool request construction is not yet
  present in Go.

Next: finish the typed event/state contracts and default execution gate, then
build the HTTP vertical slice against the pinned Python responses.

## 2026-10-02 execution gate slice

- The Python export now includes ordered default-tool risk, readonly,
  parallel-safe and capability metadata, plus the unknown-effect result text.
- Go's immutable executable catalogue currently registers Bash only. The
  session reaches it solely through a typed gate: before rewrite, monotonic
  guard, permission, handler, after replacement, and final observer. Denials
  bypass replacement hooks; observer errors and panics do not alter outcomes.
- Default policy recognizes readonly/interactive/auto, the immutable shell
  deny list, destructive-command approval, and workspace path escapes. An ask
  without an approver is denied; auto skips asks but keeps explicit denials.
  Approval remains an injected callback, with no durable broker yet.
- Cancellation closes every unanswered tool use with the Python unknown-effect
  notice, preserving completed results and transcript ordering. Other Python
  tools are still unregistered in Go and return `Unknown tool`.
- Validation: `go test ./...`, `go vet ./...`, and `go test -race ./...`
  passed, including partial-batch cancellation and auto-mode custom denials.
  Python contract check reports six current files; all 77 module invariant
  declarations, 19 scan guards and 377 mutation guards passed. The final
  isolated `.venv/bin/python -m pytest -q`
  run passed with 2151 tests, 28 skips and 24 subtests. Two earlier full runs
  each failed one timing-sensitive test; both targeted reruns passed.
- README Mermaid and the interactive specification now show the independent
  Go execution path. Generated HTML passed all nine Archify showcase checks,
  with zero errors or warnings. Browser visual review remains unavailable
  because the local-file navigation was blocked by the browser policy.

Next: port file handlers with a second path check at execution, add the
action/approval journal and masking boundary, then expose a default HTTP slice.

The file-tool slice must preserve these Python contracts before registration:

1. Resolve workspace paths at execution and reject traversal or symlink escapes.
2. Read at most the Python character cap after skipping the requested lines;
   preserve Unicode counting, newline behavior, pagination and truncation notices.
3. Write through a sibling temporary file, fsync and rename so failures retain
   the previous file; report Python's character count in the compatibility text.
4. Edit only files within the size cap and only one exact occurrence; retain
   Python's missing-text and ambiguous-text messages and avoid partial writes.
5. Compare actual Python/Go results for these cases using generated fixtures,
   including non-ASCII content, absent parents, pagination and failed edits.

## 2026-10-02 workspace file slice

- `go/workspace.Files` binds a resolved root. Its typed read/write/edit methods
  resolve each path at execution and preserve symlink-before-parent semantics;
  write permissions use the same resolver. This does not provide OS shell confinement.
- Reads count Unicode characters, normalize universal newlines, replace invalid
  UTF-8, skip lines without collecting them, and preserve the Python pagination
  and truncation notices. Edits decode strictly and require one exact match
  within the byte cap. Writes use sibling temporary files, fsync and rename,
  with cleanup on failure and a cancellation check before publication.
- `NewWorkspaceSession` registers injected Bash plus read/write/edit behind
  the existing gate. The Bash-only `NewSession` remains available. The backend
  checks authority against its bound root and rechecks paths after approval.
- The seventh contract snapshot contains 34 cases produced by actual Python
  file operations. Go compares outputs, failure signals and final file hashes.
  Other Go tests cover atomic write failure, cancellation, readonly mode,
  workspace mismatch and a symlink changed after permission approval.
- Validation passed: `go test ./...`, `go vet ./...`, `go test -race ./...`,
  the seven-file Python contract check, 77 Python invariant declarations,
  19 scanning guards, 377 mutation guards and `git diff --check`. The full
  `.venv/bin/python -m pytest -q` run passed with 2151 tests, 28 skips and
  24 subtests. These results were obtained on macOS; other platforms remain
  part of the G7 audit.
- The README and interactive architecture include the workspace backend.
  Archify passed all nine showcase checks, with no errors or warnings. Its
  receipt is specification SHA-256
  `c0b7364dd372d78cda06fb6152c9af7e1848239d3589e88128b3a7d8ec99c41b`
  (10,237 bytes) and HTML SHA-256
  `d9409a5bc35ff8e440ec5860fe38bb1b22344dd286264747b6a35cee2c21b8d1`
  (637,737 bytes). Visual review remains unavailable because browser policy
  blocked local-file navigation.

Next: port glob and the remaining default handlers, then action/approval
journals, secret masking, and the default HTTP/SSE service.

## 2026-10-02 glob slice

- `workspace.Files.Glob` is a typed backend in `NewWorkspaceSession` and uses
  the same readonly execution gate. The workspace catalogue now registers five
  tools; the Bash-only convenience constructor retains its existing scope.
- The Python 3.11 `glob` and `fnmatch` implementations supply the reference for
  recursive `**`, hidden entries, malformed bracket patterns, literal backslashes,
  lexical path spelling and symlink result filtering. A named-token matcher
  avoids regex backtracking and untyped domain payloads. Native enumeration is
  budgeted in Unicode characters before deduplication and final sorting,
  including duplicate matches and the truncation notice.
- The eighth generated snapshot captures 38 actual Python glob results and
  182 filename component patterns across 25 names. Session tests cover readonly
  execution and ordered file-write/edit/read/glob results; workspace tests cover
  cancellation, unchanged file contents and a 120-level search with the process
  descriptor limit reduced to 64. That test exposed and now prevents ancestor
  handles exhausting the descriptor budget. Go caps open directory handles at
  16 and resumes deep native cursors with bounded reads. Like the Python source,
  this is not a snapshot of a concurrently changing filesystem.
- Validation passed: `go test ./...`, `go vet ./...`, `go test -race ./...`,
  the eight-file Python contract check, 19 scanning guards, 77 Python invariant
  declarations, 377 Python mutation guards and `git diff --check`. The isolated
  `.venv/bin/python -m pytest -q` run passed with 2151 tests, 28 skips,
  24 subtests and three existing dependency deprecation warnings. The Python
  mutation checks establish the reference's guards; they do not mutate Go.
  These results are from macOS; the cross-platform audit remains open.
- README, parity matrix and interactive architecture are synchronized. Archify
  passed all nine showcase checks, with no errors or warnings. The specification
  SHA-256 is `36624bf1632b241409e77f83db8ef5b944cd0f8a6d95cb34ea5c878780a1aff1`
  (10,236 bytes); generated HTML SHA-256 is
  `f960bc895717e5bb2b3f8364c6620b9b0b4fa90a62ecee6838d034cbd0f3b59f`
  (637,718 bytes). Browser visual inspection remains unavailable because
  local-file navigation was blocked by browser policy.

Next: port the remaining default handlers, then action/approval journals,
secret masking, and the default HTTP/SSE service. G2 remains incomplete.

## 2026-10-02 session resource slice

- `NewRuntimeSession(RuntimeConfig)` composes eight implemented tools through
  the existing gate. It adds typed todo state, deployment skill sources and
  textual questions, binding their handler to one session, owner and workspace.
  The existing Bash-only and five-tool workspace constructors preserve their
  scopes. This is still an injected, in-memory runtime, not a standalone service.
- Todo updates validate the entire input before replacing state. At most 20
  items are retained, each field is capped at 2,000 Unicode characters, one item
  may be in progress, and failed updates leave the accepted board intact.
  Accepted updates emit a typed event. Todo and provider stop events now share
  a 200-record backlog with monotonic sequence numbers and detached snapshots;
  subscription delivery, other lifecycle events and durability remain pending.
- `skills.Catalog` indexes sorted `SKILL.md` sources without following directory
  symlinks, checks source-root containment, retains first-wins names, isolates
  unreadable files and bounds model-visible descriptions and bodies. The small
  frontmatter parser matches Python's line rules, including Unicode metadata
  separators, rather than parsing general YAML. Strict UTF-8, universal newlines
  and complete source hashing are streamed; retained body prefixes stay bounded.
  Source edits or removal refuse loading; identical normalized rewrites still
  serve. Problems deduplicate under a 50-distinct-message limit with occurrence
  and eviction counts. Entries and diagnostic snapshots detach their slices.
- Question answers are an explicit text/unanswered union. A declined or absent
  answer preserves the Python proceed-on-assumption notice; an empty string is
  still an answer. Nil surfaces report unavailability, invalid answer variants
  fail as tool errors, and cancellation closes the tool batch before another
  model request. The callback receives bound authority, not an approval boolean.
- Two additional Python snapshots capture 10 todo transitions, four question
  outputs and 19 skill scenarios. Go compares accepted state, outputs, source
  and body hashes, load output hashes and diagnostic counts. Tests additionally
  cover shared-source concurrency, event retention, cross-session refusal,
  readonly behavior, cancellation and large streamed input.
- Remaining differences: skill descriptions are exposed but not yet consumed
  by the model request pipeline; user-source layering and broker persistence
  remain pending. Go marks and bounds invalid-name prefixes in diagnostics at 2,048
  characters, while Python may print an unbounded invalid name. `task` and
  `compress` remain unregistered until their execution paths are ported.
- Validation passed: `go test ./...`, `go vet ./...`, `go test -race ./...`,
  the ten-file Python contract check, 19 anchored scanning guards, and
  `git diff --check`. The Python reference mutation selectors `skill`, `todo`,
  `ask-user` and `question-answer` caught all 11 selected mutations; this is a
  scoped reference check, not a Go mutation suite or a new full 377-guard run.
  The isolated `.venv/bin/python -m pytest -q` run passed with 2151 tests,
  28 skips, 24 subtests and three dependency deprecation warnings. Python package
  modules were unchanged, so the Python invariant declaration check was not
  required for this slice. Results are from macOS; cross-platform audit is open.
- The README, parity matrix and interactive map describe the new constructor
  and state boundary. Archify passed all nine showcase checks, with no errors
  or warnings after diagnosed label-route clearance fixes. Specification SHA-256:
  `864f8b7d5c1b51b303977ec5855dfaf5159dad21e1d7d39ea7a21047cd0aed34`
  (10,762 bytes); generated HTML SHA-256:
  `e4c9b07a1cd2e86562928da0857f1e69f36643d257aadb221edce014125889a0`
  (640,078 bytes). Browser visual review remains unavailable because local-file
  navigation was blocked by browser policy.

Next: implement the typed context/request pipeline with real compaction and
subagent execution, then approval/action journals, masking and HTTP/SSE. The
full objective and G0-G7 remain open.

## 2026-10-02 request and context slice

- `Provider.Complete` now accepts `protocol.ModelRequest` instead of a message
  slice. The request carries model, output budget, detached messages, optional
  system text, recursively typed tool schemas and a local purpose. Absent tools
  and an explicitly empty catalogue remain distinct when serialized. Production
  default descriptors are embedded in `go/protocol/`, independently compared
  with the Python export; runtime code never reads `go/testdata/`.
- One immutable fitted catalogue feeds both the system builder and provider.
  Python's 60,000-character tool budget, description trim steps, ordered omission
  and canonical SHA-256 match six exported catalogues, including oversized
  properties. Skill descriptions now enter actual requests. Changed todo and
  bucketed pressure reminders enter the message stream, preserving the stable
  system prefix. Explicit system-builder and compactor interfaces use named
  contexts/results rather than dynamic state maps.
- The fake provider counts the complete messages/system/tools payload, preserves
  requested model identity and checks the Python non-streaming budget ceiling.
  The live meter includes cached input, learns same-envelope growth, projects
  signed shrinkage and discards a stale envelope anchor. Summary requests do not
  anchor it. Three Unicode/wire cases and six meter steps match Python.
- Workspace-backed sessions run the four default layers: oversized-result
  spill, pair-safe middle snip, consumed-result micro compaction and a full
  transcript archive plus model summary. Result markers preserve IDs/error flags;
  retained tool calls preserve thinking signatures and caller metadata. Archives
  and spill files use the bound workspace resolver and atomic replacement.
  Summary requests use 2,000 output tokens, no system or tools, and the last
  80,000 serialized characters. Typed receipts record pre-replacement provenance
  and actual summary usage/model. Empty/failed/cancelled summaries preserve
  paired history; automatic ordinary failures record a bounded failure event.
- `compress` is the ninth runtime tool, crosses the existing write-risk gate
  and defers until the whole batch has results. Readonly denial creates no
  summary archive. Automatic compaction retains Python's ordinary-agent behavior
  and can write artifacts even under readonly tool mode; a read-only worker must
  inject `InMemoryCompactor`. The workspace-less Bash convenience constructor
  uses that strategy by default. Custom compactor errors with a zero result
  preserve the original error/history; invalid transcript rewrites are rejected.
- The eleventh Python snapshot also captures four snip/micro histories, result
  spill content, summary request, archive and receipt. Go integration tests cover
  whole-batch ordering, summary/live-meter separation, empty and failed summaries,
  cancellation, workspace escape refusal, detached schema snapshots, changed
  runtime facts, extension failure and cheap shrink avoiding an unnecessary summary.
- Validation: `go test ./...`, `go vet ./...`, `go test -race ./...`, eleven-file
  Python export check, all 19 scanning guards and `git diff --check` passed.
  `.venv/bin/python -m pytest -q`: 2151 passed, 28 skipped, 24 subtests passed,
  three existing dependency deprecation warnings. Python reference mutation
  selectors `compact`, `meter`, `envelope`, `snip` and `summary` caught all
  17 executions (16 distinct mutations); this is a scoped reference check,
  not a Go mutation suite or a full 377-guard run. No Python package modules
  changed, so invariant declarations did not require revalidation.
- README Mermaid, boundary explanation and interactive source were reviewed
  together. Archify passed 9/9 showcase checks, zero errors/warnings.
  Specification SHA-256:
  `129ce970813466b07051914dcd59b600c327b6f37c16b2658c6187670742a794`
  (11,762 bytes); generated HTML SHA-256:
  `3b40ba84e04cef992e6d025c78f5ad2ed89561f205ebf98d02c41ab67d7b641a`
  (644,727 bytes). Browser visual inspection remains unavailable because
  local-file navigation was blocked by browser policy. All runtime gates ran
  on macOS; cross-platform and live-provider audit remain open.

Next: implement bound subagent execution (`task`), then durable approvals/action
journals, masking, provider cache annotation and HTTP/SSE. Streaming transport,
provider recovery, optional prompt sections/user resources, token efficiency and
session restoration remain open. Compaction files are durable artifacts, not a
Go session-restoration store. The full objective and G0-G7 remain open.

## 2026-10-02 subagent slice

- `NewRuntimeSession` now registers all ten Python default tools in source
  order. `task` retains Python's execution risk and empty capability set, passes
  the existing gate and invokes an explicit `SubagentProvider`. Custom providers
  receive a pinned environment with named settings, identity and catalogue
  accessors, rather than a mutable parent session or untyped state map.
- The default provider creates fresh child history, todos, runtime facts and
  token meter. It inherits the available model/context/tool services, uses the
  source's fixed role system prompt and selects tools by nonempty capability
  sets wholly contained in an immutable role profile. Ordered profile input
  preserves last-wins normalized aliases. Explore uses readonly mode;
  general-purpose and worker use interactive mode independently of the parent.
  Default roles omit unclassified tools, including task. Custom selected
  built-in handlers bind the fresh child, so nested task execution still reaches
  the same depth guard. Default depth is 2; child rounds default to 30.
- Typed `RunContext` supports default untrusted and explicit-human provenance,
  detached snapshots, new-message approval replacement and peer derivation.
  Default children link their parent message and drop human actor/approvals.
  `RunWithContext` and `DelegateWithContext` keep caller stamps out of model
  JSON. Custom providers receive the parent context; the default provider owns
  peer derivation, matching the Python injection seam. This does not implement
  authenticated HTTP ownership or the full Python RunContext construction API.
- Typed subagent start/end/refusal and round-exhaustion variants join the bounded
  200-event backlog. Child forwarding retains label, depth and detached run
  provenance; parent history remains separate. Depth refusal happens before
  provider construction/start telemetry. Prompt and summary displays retain at
  most 2,000 Unicode characters while the provider/result receives full text.
  Cancelled/error children emit no successful end. Round exhaustion returns the
  source stop marker before partial output, with an error event and no Go error.
- The twelfth generated Python snapshot covers four role selections, five
  provenance cases and three actual child loops: Explore reads, general-purpose
  writes and an exhausted child reports its stop. Tests compare fixed system,
  fresh messages, model/output budgets, selected tools, provenance, lineage,
  summary and filesystem effects. Additional Go tests cover quota enforcement
  for custom providers, nested task rebinding, a wider custom Explore catalogue
  under an auto parent, cancellation pairing and concurrent shared services.
- Validation: `go test ./...`, `go vet ./...`, `go test -race ./...`, twelve-file
  Python export check, all 19 scanning guards and `git diff --check` passed.
  `.venv/bin/python -m pytest -q`: 2151 passed, 28 skipped, 24 subtests passed,
  three existing dependency deprecation warnings. Scoped Python reference
  mutation selectors `subagent`, `explore-registry`, `task-tool-bypasses` and
  `early-stop-swallowed` caught all nine distinct mutations. This is not a Go
  mutation suite or the full 377-guard run. No Python package modules changed;
  invariant declarations did not require revalidation.
- The canonical README Mermaid, boundary prose and interactive specification
  were updated together. Archify passed 9/9 showcase checks with zero warnings
  or errors after fixing the reported endpoint direction and label clearance.
  Specification SHA-256:
  `c3d89d7dc8baee21e0d784dadb7bb777bbe06e3ef0e63f14a44a77369c48f67d`
  (12,972 bytes); generated HTML SHA-256:
  `15e1184ee5f8f77a7c388067bd8e06c068dcb36b1e5e94a5000e215aca26e044`
  (649,492 bytes). Browser visual inspection remains unavailable following the
  earlier local-file navigation policy block; static layout validation passed.
  Runtime gates ran on macOS; cross-platform/live-provider audit remains open.

Next: durable approvals and action journals, then masking, provider cache
annotation and HTTP/SSE. Shared concurrency limiters, stuck detection, complete
lifecycle events, custom child broker/state inheritance, owner resources,
streaming transport, recovery and session restoration remain open. The full
objective and G0-G7 remain open.

## 2026-10-02 action journal slice

- `ActionRecord`, `ActionStatus`, `ActionRequest`, `ActionSettlement`, action IDs,
  workflow IDs and input hashes are named types. `InMemoryActionJournal` and
  `StoredActionJournal` implement the Python begin/finish/get/workflow-binding
  contracts; the stored adapter additionally supports explicit reconciliation
  and scoped in-flight-to-unknown marking through a typed `ActionStore`. Record
  snapshots detach optional fields. Payload or workflow reuse conflicts are
  typed errors; finish cannot rewrite a terminal or unknown record, and only
  reconcile can settle unknown. A stored adapter is not itself a durable backend.
- Optional `RuntimeConfig.ActionJournal` / `NewJournaledToolGate` binds replay
  into the existing gate. Action IDs hash session/message/tool-use/name; sorted,
  compact UTF-8 JSON binds final rewritten arguments. Current guards and
  permissions run before journal begin, including for replay. Denied calls do
  not become dispatched records. Terminal replay reuses the stored result while
  retaining post hooks; source replay does not reproduce old failure flags.
  Cancellation settles cancelled without reusing its cancelled context. Store
  settlement faults propagate before observers. Nil journal retains the bare
  agent default. Default children do not inherit the parent's state journal.
- `ToolVerifier` uses three named verdicts. Unknown actions retry only after
  proven non-landing; undetermined, invalid/error or panic cannot become a no.
  Proven landing returns the source reconciliation marker and uses the optional
  journal reconciliation seam. `write_file` has a bound verifier; Bash has none.
  Full strict UTF-8 reads preserve Python universal-newline semantics, validate
  even a mismatching suffix and use bounded working memory. Source behavior is
  preserved: a proven non-landing retry cannot rewrite unknown via finish, so
  that record stays unknown until explicit reconciliation. Result metadata is
  typed on `ToolOutcome`; complete lifecycle/SSE event projection remains open.
- Ordinary results retain at most 4,000 Unicode characters with an explicit
  truncation marker; the future decision result budget remains 512 KiB characters.
  Memory retention bounds both count (default 512) and aggregate characters
  (2,048,000), shedding result text while keeping every identity/status to avoid
  reexecution after eviction. Growth/shedding diagnostics are bounded. Journals
  do not claim cross-process dispatch ownership or exactly-once external effects.
- The thirteenth Python export adds twelve canonical input/hash/identity cases,
  six transitions per actual memory/SQLite journal, conflicts/invalid settlement,
  two Unicode result bounds and ten actual tool replay/reconciliation paths.
  The Go stored-adapter tests use a typed test backing and do not prove SQLite
  persistence. Go integration covers final rewrites, post-hook replay, current
  permission refusal, cancellation/fault propagation, shared-journal session
  isolation, workflow binding, retention without identity eviction and bound
  write verification including malformed UTF-8/newlines.
- Validation: `go test ./...`, `go vet ./...`, `go test -race ./...`, thirteen-file
  Python export check, all 19 scanning guards and `git diff --check` passed.
  `.venv/bin/python -m pytest -q`: 2151 passed, 28 skipped, 24 subtests passed,
  three existing dependency deprecation warnings. Reference mutation selectors
  `reconcile`, `action-results` and `decision-results` caught all three mutations;
  this is not a Go mutation suite or a full 377-guard run. No Python package
  modules changed, so invariant declarations did not require revalidation.
- README baseline, canonical Mermaid, boundary prose and interactive source
  were updated together. Archify passed 9/9 showcase checks, zero warnings/errors,
  after routing the new journal connection away from the reported crossings.
  Specification SHA-256:
  `f16190b14c9daa87781d484f68b319d7f12ac225b8b3015b20bd4e20be530cde`
  (13,516 bytes); generated HTML SHA-256:
  `4d7014f7f04980b5f11a96fde1cd325ebbd22a2fa8be7e454e7e7ec3212cb8f7`
  (651,938 bytes). Browser visual review remains unavailable after the earlier
  local-file policy block. Runtime tests ran on macOS; live provider and
  cross-platform/restart evidence remain open.

Next: typed approval broker, session grants/questions and persistence-fault
reporting, then the SQLite backend, masking, cache annotation and HTTP/SSE.
The SQLite driver dependency authorization is pending under AGENTS.md; this
slice adds no dependencies. Session restoration, leases, shared limiters,
stuck detection, complete lifecycle events and remaining optional features are
still required. The full objective and G0-G7 remain open.

## 2026-10-02 approval broker slice

- `ApprovalBroker` and its session/owner/workspace-bound `ApprovalSurface` use
  named IDs, kinds, statuses, records, reviewer verdicts and a closed approval
  event union. The optional `RuntimeConfig.Approvals` binds the approver and
  textual-question surfaces together; conflicting explicit surfaces are rejected.
  Nil retains the existing unconfigured default. The broker is process-local,
  and `ApprovalStore` is only a typed write seam; no Go SQLite backend is shipped.
- Pending calls remain before execution and action-journal begin. Human decisions,
  denied/time-out/cancelled states and question text remain distinct; an empty
  answer is still answered. Duplicate/foreign resolutions cannot settle a row.
  Explicit session/all cancellation writes `cancelled` and wakes waiters; context
  cancellation removes the pending entry but leaves its stored row pending,
  matching the source cancellation window. Store errors/panics produce bounded,
  credential-free diagnostics without changing the human decision.
- Grants match the command's own token prefix or the concrete tool name. Bash
  defaults to two Python-whitespace tokens; honest proposals use two to six.
  Invalid proposals fall back to the default, and the source's 28 banned heads
  cannot become remembered grants. Remembered grants are session-only, precede
  the optional reviewer, and die with explicit session cancellation. Reviewer
  errors/panics/invalid verdicts abstain; readonly and final-deny policy remain
  ahead of the broker. Fresh children do not inherit a parent broker surface,
  even when a custom role catalogue admits mutation or question tools.
- Optional `ApprovalRedactor` masks typed preview inputs before JSON escaping,
  question text before publication and answers before storage. It is not the
  application-wide secrets implementation. The 400-character preview and
  2,000-character question bounds preserve the current source contracts. Six
  approval event variants share the sequenced 200-event backlog and caller run
  provenance; HTTP authentication, event streaming and subscriptions remain open.
- The fourteenth Python contract export captures 38 grant candidate cases and
  21 actual broker outcomes, including Unicode/escaped-secret previews, empty
  questions, grant/proposal reuse, cancellation, reviewer abstention and store
  faults. Differential normalization changes random IDs and wall-clock timestamps,
  not decisions or stored rows. Timeout tests verify outcomes/event shape rather
  than scheduler timing. Broader optional-tool/background inputs remain G6 work.
- Validation: `go test ./...`, `go vet ./...`, `go test -race ./...`, fourteen-file
  Python export check, all 19 scanning guards and `git diff --check` passed.
  `.venv/bin/python -m pytest -q`: 2151 passed, 28 skipped, 24 subtests passed;
  four warnings (three dependency deprecations and a Python subprocess cleanup
  warning about a closed event loop). Source reference guard selectors `approval`,
  `auto-reviewer`, `grants-`, `a-lying-prefix`, `banned-heads`, `question-answer`,
  `answer-secret` and `ask-leaves` caught all 16 unique mutations (18 executions,
  since two reviewer cases also match `approval`). This is neither a Go mutation
  suite nor the full 377-guard sweep. No Python package modules changed.
- README baseline, canonical Mermaid, boundary prose and interactive source were
  updated together. Archify passed 9/9 showcase checks, zero warnings/errors.
  Specification SHA-256:
  `edbe0a91a356b36c47571fb346fdeb7874fc3ebf74ba04a7965b0015340f93b7`
  (14,650 bytes); generated HTML SHA-256:
  `f9087b5b92c608b5e3d7adb0876d382647169703cccc7885ac6ceec20bff22b0`
  (656,082 bytes). Browser visual review remains unavailable after the earlier
  local-file policy block. Runtime evidence is macOS, not live provider or
  cross-platform/restart verification.

Next: application-wide secret masking and cache annotation, session/event
services and HTTP/SSE; SQLite persistence follows the pending driver dependency
authorization under AGENTS.md. This slice adds no dependencies. Session restore,
leases, shared concurrency limits, stuck detection, full lifecycle, provider
recovery and remaining optional features are still required. G0-G7 and the full
port objective remain open.

## 2026-10-02 secrets slice

- `go/secrets.Registry` uses named credential names, a concrete environment map,
  explicit lookup functions and optional configuration values. Literal/lazy
  registration, detached process/environment snapshots, credential-shaped name
  selection, narrow command-name injection APIs and wide value masking preserve
  the source rules. Successful values remain cached until re-registration;
  errors, empty/nil lookups and panics are contained and reported by name, with
  a 60-second retry window. Short values are reported, not masked. Concurrent
  reads share one lookup flight, and re-registration cannot publish an old
  in-flight value into the new cache. `Null` retains the unconfigured default.
- ANSI-interleaved matching covers CSI, OSC and two-byte ESC controls. Values
  match longest first by Unicode character count; replacements stay literal,
  including backslashes and an explicitly empty replacement. Name lookup uses
  full Python lowercase mappings and final sigma; environment patterns use
  full uppercase mappings, including expanding characters. Generated Unicode
  14.0.0 tables are checked by `python/tools/export_go_unicode.py --check`.
  Python filename matching moved to `internal/fnmatch` for shared use, preserving
  the existing glob wrapper and all 182 source pattern cases.
- Optional `RuntimeConfig.Secrets` binds the gate, event backlog, compactor and
  bound approval surface. Tool results are masked after post hooks and before
  journal settlement, observers and model results, including denied/error paths.
  Trusted hooks/handlers still receive raw calls; live assistant tool arguments
  and model requests remain raw, as in Python. Default compaction masks durable
  spill content, transcript archives and summaries. Ordinary model-authored
  prose is not rewritten. Children inherit the registry while keeping fresh
  broker state. Recording copies mask caller metadata without changing its
  authoritative actor/capabilities; parent event forwarding does not re-mask
  the same child event. Shared brokers retain each surface's row redactor.
- `protocol.MapToolInputStrings` maps all ten concrete payloads without mutating
  the originals. `MaskedPythonJSON` accepts a concrete boundary type and uses a
  private closed JSON projection for decoded strings/keys before final escaping;
  numbers/member order are preserved and masked-key collisions use the last
  value. Its projection decoder rejects over 16 MiB or 256 levels of nesting.
  No untyped payload or raw JSON is retained in domain/service structs.
- The fifteenth Python export adds 15 mask recipes, four environment scenarios,
  ten concrete previews, eleven Unicode casing cases, nested/key-collision
  projections and actual source rotation/retry outcomes. Runtime tests cover
  execution versus recording, journal-before-observer order, default-off behavior,
  per-session shared-broker answers, raw summary requests versus masked artifacts,
  inherited child registry and detached caller/event metadata. Boundary limits
  and missing/panicking/concurrent/re-registered lookups are covered.
- Validation: `go test ./...`, `go vet ./...`, `go test -race ./...`, fifteen-file
  Python export check, Unicode table check, all 19 scanning guards and
  `git diff --check` passed. `.venv/bin/python -m pytest -q`: 2151 passed,
  28 skipped, 24 subtests passed; four warnings (three dependency deprecations
  and a subprocess cleanup warning about a closed Python event loop). Scoped
  source reference selectors `tool-result-unmasked-before-the-journal`,
  `spill-is-unmasked`, `mask-payload-skips-dict-keys`,
  `approval-preview-masked-after-serialize`, `answer-secret-persisted-raw` and
  `tool-input-unmasked` caught all six mutations; this is not a Go mutation suite
  or a full 377-guard run. No Python package modules changed.
- README baseline, Mermaid, boundary prose and interactive specification were
  updated together. Archify passed 9/9 showcase checks, zero warnings/errors.
  Specification SHA-256:
  `ab335ce669fea17193d13a696e80e1509b5c040075ac606ba5385879dc6026cc`
  (15,246 bytes); generated HTML SHA-256:
  `acc305b60a604cba07a4fedecfa9a13a25ed424e4ae6a1b93df9c253ddceadd2`
  (658,501 bytes). Browser visual review remains unavailable after the earlier
  local-file policy block. Runtime evidence remains macOS and offline providers.

Next: the real workspace Bash executor, process-group cancellation, bounded
stdout/stderr capture, typed command-result metadata and consumption of the
selected environment with direct-call masking; then cache annotation, session
services and HTTP/SSE. The current injected Bash interface does not automatically
consume environment selections. Future provider/HTTP/SQLite/trajectory and
optional-feature sinks still require explicit masking integration. SQLite driver
dependency authorization is pending under AGENTS.md; this slice adds no
dependencies. Restoration, leases, concurrency limits, stuck detection, complete
lifecycle, real provider/recovery and optional features remain open. The full
port objective and G0-G7 remain active.

## 2026-10-03 foreground shell slice

- `go/shell.Executor` is a real `/bin/sh -c` backend with a resolved immutable
  workspace, default timeout of 120 seconds, optional typed Sandbox argv
  rebinding and credential source. The original `BashExecutor` string seam
  remains supported. The real executor additionally returns a concrete `Result`:
  separate stdout/stderr, optional exit/error/projection, timeout/overflow flags,
  monotonic duration and capture limit. Rendering preserves quiet/nonzero exits,
  independent timeout diagnostics, Python whitespace and head/tail truncation.
- Processes start in a new session on macOS/Linux. One deadline covers both
  process completion and pipe EOF; a shell exiting first does not lose its group
  identifier. Cancellation, overflow and explicit `Interrupt` kill the whole
  group with SIGKILL. The wait goroutine owns reaping; read ends close after a
  bounded five-second cleanup if a detached descendant retains them. Such a
  descendant has left the group and is not killed by this executor. Other
  platforms reject construction; no Windows parity claim is made.
- Both pipes consume one aggregate capture budget. Python's source calls it a
  byte bound but counts decoded characters; Go preserves the 5,000,000-character
  bound and notice, with fixed-size buffered UTF-8/newline decoding. The existing
  workspace decoder now shares its maximal-subpart UTF-8 decoder with the shell.
  Output allocation remains bounded by this budget, not by the producer's total
  output. Exact-fit capture does not report overflow.
- Registered ambient names are removed before selected command names are added
  back. Full streams are masked before rendering and truncation; credentials
  split across stdout/stderr use the safe combined projection. Runtime Secrets
  config binds a credential-scoped executor copy without changing other sessions;
  copies retain one explicit process interrupt tracker. Mask-only redactors can
  override full-stream projection without introducing environment credentials.
  The direct-caller typo guard is preserved, case-sensitive and explicitly not
  a confinement boundary. Default execution is on the host; Go Seatbelt remains
  unimplemented.
- The common gate detects structured Bash results, records failed status from
  exit/error/timeout, and supplies detached typed metadata to observers after
  journal settlement. Replay has stored text and no fresh process metadata,
  preserving the existing source failure/replay semantics. Binding to a foreign
  workspace is refused before execution. Full tool lifecycle events are still
  pending. Bash-only sessions derive a real executor's explicit workspace;
  arbitrary injected string executors keep their existing unbound behavior.
- The sixteenth Python export adds eight actual foreground command cases, seven
  explicit rendering recipes and six blocklist decisions. Real Go tests cover
  aggregate overflow and exact-fit Unicode, invalid UTF-8/universal newlines,
  named environment selection, whole-stream/split-stream masks, process and pipe
  deadlines, exited parents, SIGTERM-ignoring groups, cancellation, interruption,
  concurrent capture isolation, detached pipe cleanup, Sandbox rebinding/start
  errors, readonly/foreign binding, replay settlement and runtime cancellation
  transcript repair. Evidence is local macOS with fake/offline providers.

- Validation: final `go test ./...`, `go vet ./...` and `go test -race ./...`
  passed. Sixteen-file Python export check, Unicode 14.0.0 table check, all
  19 scanning guards and `git diff --check` passed. Final standalone full
  Python run: 2151 passed, 28 skipped, 24 subtests passed, three dependency
  deprecation warnings. The earlier concurrent run failed only the offline
  40-turn timing threshold (0.62 seconds versus 0.5); an isolated targeted run
  and the complete isolated rerun passed, without runtime/test changes.
  Scoped source reference selectors `bash-reads-all-output-into-memory`,
  `timeout-hides-the-diagnostic-output`, `cancelled-turn-abandons-the-shell`,
  `interrupt-spares-the-commands-children` and
  `finished-shells-linger-in-the-live-set` caught all five mutations. This is
  not a Go mutation suite or the full 377-guard sweep. No Python package
  modules changed, so the package invariant verifier was not required.
- README baseline, canonical Mermaid, boundary prose and interactive source
  were updated together. Archify passed 9/9 showcase checks, no warnings/errors.
  Specification SHA-256:
  `888204f0cba31f2e67fd701705362315dab0d3c59d5f4871b63c8d4684eddce2`
  (15,316 bytes); generated HTML SHA-256:
  `755eaf6f720169697b1ad2d262c3edbc24a8e1f071c8695a1fdb4a6d28d86d08`
  (658,598 bytes). Browser visual review remains unavailable after the earlier
  local-file policy block. Runtime process evidence remains local macOS.


Next: cache annotation, stuck detection, complete session lifecycle/services
and HTTP/SSE; real provider/recovery and durable session storage follow. SQLite
requires an external Go driver; the dependency authorization question remains
pending under AGENTS.md. No dependencies were added in this slice. Remaining
provider/HTTP/SQLite/trajectory/optional-feature sinks require explicit masking.
Restoration, leases, shared concurrency limits and optional features remain
open; the full G0-G7 goal remains active.

## 2026-10-03 cache and stuck slice

- Cache annotations are enabled by default at the model boundary, including
  summary requests. `CachePolicy`, `CacheConfig`, `CacheControl` and concrete
  wire variants preserve system text, absent/empty tools and typed message
  content without putting cache metadata into live transcript blocks. The
  stable system prefix takes one ephemeral breakpoint; remaining eligible user
  blocks follow Python's reverse traversal and stride. The default budget is
  four, stride 15 and lookback ceiling 20. TTL is forwarded when configured.
  `NullCachePolicy` is an explicit opt-out. The fake counts the annotated wire
  payload and rejects more than four breakpoints; real provider cache reuse and
  savings are unverified. Preexisting system block-list input remains open.
- `StuckDetector`, `StuckThresholds`, `ToolStep`, `StepHash` and typed stuck
  events implement all five source rules and their precedence: monologue,
  repeated error, unproductive tool, repeated result and alternating actions.
  At most 20 steps bind final rewritten inputs and final masked outputs; hash
  bytes preserve Python's sorted/spaced UTF-8 JSON and 16-digit SHA-256 prefix.
  One default reminder resets the window; a further signal halts with all tool
  results paired and an explicit stop marker before partial output. Every new
  user intent resets both counters and ledger. Detector faults also retain a
  paired transcript. Round exhaustion shares the same stop-text formatter.
- Explicit `StopHook` continuation enables the monologue path (nil finishes,
  empty string continues). Hooks receive detached context snapshots; they must
  not recursively enter the locked session. Children inherit policies/hooks,
  while their ledger and nudge budget remain fresh. Null policies and custom
  valid thresholds are tested. Go constructors reject invalid thresholds and
  cache strides more strictly than Python's invalid-configuration behavior.
- Differential hashes exposed an actual decoder mismatch: absent optional
  fields and explicit JSON null had been merged. Closed flags now retain the
  distinction for Bash, read, task and skill inputs through cloning, masking,
  wire serialization and both identity hashes. Execution accessors preserve
  their existing nil defaults; no arbitrary JSON enters the domain.
- The seventeenth Python export adds twelve cache wire/position/token cases,
  sixteen detector decisions, six hash recipes and five actual source loops.
  Real loop cases cover one nudge then halt, denied calls, zero nudge budget,
  continued monologues and null opt-out. Go additionally checks the bounded
  window, new-user reset, before-hook rewrites, detached/masked stuck events,
  paired detector errors, summary annotation and child policy inheritance.
- Final Go validation uncovered a preexisting foreground cancellation race.
  Repeated tests and goroutine/process diagnostics showed a child surviving
  inside the original group after a shell fork overlapped the first SIGKILL;
  the reaped shell left the child holding both pipes. Cleanup now repeats the
  group signal every 20 milliseconds while pipes remain open, within the
  existing five-second bound. Explicit Interrupt notifies the running command
  to enter that same cleanup path. A new test exercises 20 immediate stops in
  each cancellation/interrupt mode; the existing runtime test passed 30
  consecutive runs after the fix. Detached children remain outside the group.

- Final validation: `go test ./...`, `go vet ./...`, `go test -race ./...`
  passed after the cancellation fix. The isolated full Python run passed:
  2151 passed, 28 skipped, 24 subtests passed and three dependency deprecation
  warnings in 98.81 seconds. The 17-file Python export check, all 19 scanning
  guards and `git diff --check` passed. Five scoped Python reference selectors
  caught their mutations: `cache-budget-drifts-from-the-limit`,
  `double-allows-extra-breakpoints`, `stuck-halt-bypasses-the-stop-rule`,
  `cancelled-turn-abandons-the-shell` and
  `interrupt-spares-the-commands-children`. These are source reference checks,
  not a Go mutation suite or the full 377-guard sweep. No Python package
  modules changed, so the package invariant verifier was not required.
- README baseline, canonical Mermaid, boundary prose and interactive source
  were updated together. Archify delivery passed 9/9 showcase checks with
  zero errors/warnings, correction_rounds: 0. Specification SHA-256:
  `ca9295892b59fb35b40c1f9bf19d7f2524bdc0828d4dc40093f11659a98b8fac`
  (15,477 bytes); artifact SHA-256:
  `335a39f4117f931d26b4c9886eca28ddd5cce679da2c1cf726c0d56465702849`
  (658,744 bytes), output: `docs/mini-loop-system.architecture.html`.
  `visual_review: skipped (image reader unavailable)`; the earlier browser
  local-file policy block remains, so no rendered visual acceptance is claimed.
  Runtime process evidence remains local macOS; no Linux-host claim is added.

Next: complete session lifecycle/services and HTTP/SSE, followed by real
provider/recovery and durable session storage. SQLite still requires a Go driver
and the existing dependency authorization question is unanswered. No
dependencies were added. Owner resources, shared concurrency limits, trajectory,
Go Seatbelt and optional features remain open; the full G0-G7 goal stays active.


## 2026-10-03 lifecycle and managed session slice

- Core model start/end events record request estimates, purposes, requested and
  served models, usage, prompt tokens, catalogue/system/capability fingerprints
  and the conversation meter. Summary calls use the same cache/telemetry seam
  without replacing the live meter anchor. Bounded fingerprint sets retain the
  source clear-at-512 behavior. Ordinary provider errors emit failed recovery,
  model error and a bounded error reply; retry/backoff is still pending.
- Commentary/final-answer text, tool spans and activity associations are typed.
  Tool-use events precede the unchanged execution gate; result events follow
  masked settlement and observers, carry failure/denial/replay flags, cap display
  text at 2,000 Unicode characters and retain detached command metadata.
  Reconciliation is emitted immediately after verification, before retry/effect
  settlement. Raw executed arguments/history stay raw; recording copies mask
  values, schema descriptions, display labels, errors and caller framing.
- The process-local event bus has a 200-event durable backlog and a 2,000-record
  live queue per subscription. Slow subscribers drop oldest; ephemeral deltas
  advance sequence numbers but never enter replay. Publication and optional
  EventSink callbacks are ordered and detached. Sink errors/panics are contained
  and masked; slow callbacks can delay the emitter. Callback reentry into emission
  or the blocking Messages accessor is unsupported. No streaming provider is
  implemented merely because a typed delta variant exists.
- NewManagedSession privately owns the core and adds admission, idle/running/error
  status, status/done/cancelled events, run counts and operator cancellation.
  The active marker is set after admission; cancellation cannot hit a queued
  caller. Context cancellation abandons admission promptly, and closure is
  checked both before and after waiting. Info uses immutable live snapshots and
  can report awaiting_approval/stuck without waiting for the loop lock. Once a
  terminal decision is committed, Cancel returns false even while final callbacks
  finish. Children remain raw sessions and emit no outer status/done events.
- Event framing includes root session ID, timestamp, sequence and transcript
  epoch. Appends preserve the epoch; row replacement or shortening opens a new
  epoch, even with no store. Storage identity is compared on immutable content;
  no JSON re-encoding of complete history is needed for every event.
- A batch interrupted by a caller-provided handler panic retains completed
  outputs and pairs unresolved calls with unknown-effect results. ManagedSession
  reports a type-only runtime fault, releases admission and can run again.
- The eighteenth Python export runs nine actual AgentSession paths: completion,
  pre-hook denial, handler failure, pause/resume, refusal, unknown stop, provider
  failure, model cancellation and tool cancellation. Go compares every exported
  event field/order and message history, normalizing random IDs, durations,
  timestamps, language-specific exception labels and numeric JSON spelling only.
  The empty Python refusal [] is normalized to the valid Go empty-string content
  at the fixture boundary. Source display-only probes receive required unused
  typed fields at the test boundary. Source bus bounds, eight titles and ten tool
  labels are tested. All previous seventeen snapshots remain byte-identical.
- Differential runs corrected two older protocol differences: omitted provider
  caller metadata now remains omitted (explicit null stays null), and paired
  model results omit is_error even when telemetry reports failure/denial, as the
  actual Python batch does. Existing failure/denial tests now assert the typed
  event flags alongside the paired textual response.

- Validation: `go test ./...`, `go vet ./...` and `go test -race ./...`
  passed. The 18-file Python export check, all 19 scanning guards and
  `git diff --check` passed. Three scoped Python reference guards caught their
  mutations: `subscriber-queue-unbounded`, `running-marker-set-before-the-lock`
  and `external-cancel-leaves-a-dangling-tool`. These are source reference
  checks, not Go mutations or the full 377-guard sweep. No Python package
  modules changed, so the package invariant verifier was not required.
- Full Python validation was run independently after Go/guard processes ended:
  2149 passed, 28 skipped, 24 subtests passed and two timing failures in 385.89
  seconds. `test_sessions_run_concurrently` measured 0.802 seconds against its
  0.5-second ceiling; `test_a_forty_turn_session_stays_fast` measured 1.245
  seconds against the same ceiling. The targeted rerun passed concurrency but
  still failed forty turns at 0.848 seconds. A clean `git archive` of HEAD
  `7090fdf7d309d07b6a2cd74e777d55e8b39af9dd`, with its Python source/test files
  and the same interpreter, also failed forty turns at 1.235 seconds. This
  establishes a preexisting timing-gate failure in the current environment;
  its root cause is unconfirmed. The full Python gate is **not green**. Source
  runtime/tests and thresholds are unchanged. The full run reported three
  dependency deprecations and one subprocess teardown warning (event loop closed).
- README review baseline, canonical Mermaid, boundary prose and interactive
  source were updated together. Archify delivery passed 9/9 showcase checks
  with zero errors/warnings. Specification SHA-256:
  `6cdc6a26840a6db4399ce8dcc95bccb95cc4c75a0f7fddb17ccfcaeda6614e2c`
  (16,095 bytes); HTML SHA-256:
  `5c1c5fe84fdf1b587b4ce1f2d773195ae02eb546c1823447ea7fdb81ca787066`
  (661,082 bytes), output: `docs/mini-loop-system.architecture.html`.
  Rendered visual review remains skipped following the earlier browser local-file
  policy block; the automated checks do not establish rendered visual acceptance.

Next: session manager/services and HTTP/SSE, followed by real provider/recovery
and durable storage. Remaining G1 work includes user-prompt hooks/injectors, todo
nagging, shared concurrency limits and parallel batches. SQLite driver approval
is still unanswered; no dependencies were added. Owner resources, trajectories,
Go Seatbelt and optional features remain open. The G0-G7 goal stays active.

## 2026-10-03 loop extensions and scheduling slice

Reviewed Python `agent.py` prompt/injector/Todo-nag and `_exec_tool_batch`,
`registry.py` per-call execution modes, `config.py` limits, and child semaphore
inheritance against Go baseline `0e4d8e0`. This completes another G1/G2 library
slice; the complete port remains open.

Implemented:

- Typed sequential `UserPromptHook`: nil preserves the submitted prompt, an
  empty replacement is valid, and failure occurs before the user message.
- Named `MessageInjector`: detached transcript/todo/authority views, whole-batch
  typed validation before append, one pass before runtime facts and compaction
  per model round. Go validates concrete protocol messages more strictly than
  Python's injector role-presence check; error strings are not claimed identical.
- Todo reminders after three tool batches without an attempted TodoWrite.
  Counters survive user turns, attempts reset them even when denied, and fresh
  children retain independent todos/counters.
- Typed `ExecutionMode` / `ExecutionClassifier`: a per-call override before gate
  rewriting; errors, panics and invalid values create an exclusive barrier.
  Static readonly does not imply parallel. Default read_file and glob opt in.
- Consecutive parallel groups with barriers; each worker retains the same gate.
  Tool-use telemetry starts in model order, result telemetry follows completion,
  and transcript results / bounded stuck steps drain in model order.
- Explicit typed shared model/tool pools. Bare model calls remain unbounded;
  parallel tools default to eight per session. RuntimeConfig can share pools
  across sessions; children inherit the exact pools. Summaries use the model
  pool, and exclusive tools (including default task delegation) bypass tool
  permits. A custom nested task classifier must not hold a saturated pool while
  waiting for that same pool.
- Interrupted groups join all started workers, preserve completed results and
  close unfinished uses as unknown before returning. Parallel worker panics
  expose only their type and release capacity; group cancellation retains its
  original failure cause instead of reporting a sibling's cancellation. Existing
  exclusive panic repair/rethrow remains. Custom handlers must honor context
  cancellation. These Go fault tests do not claim a complete differential corpus
  for Python's parallel cancellation/crash windows.
- Child inheritance of prompt/injector seams and limiter pointers, with fresh
  history, todo/meter and stuck state. No new authority source or effect bypass.

Evidence:

- Nineteenth real-Python snapshot: six classifier decisions, nil/empty prompt
  chain, forced reverse completions in two groups around a barrier, ordered
  result rows / step hashes, and four cross-turn Todo-nag counters.
- Deterministic Go synchronization checks: group overlap/barrier/tail ordering,
  classification after an earlier barrier, shared pool caps, cancelled model/tool
  admission, exclusive bypass, completed-sibling preservation, worker joins,
  capacity release and original panic cause, next-turn recovery, atomic injector
  rejection, per-round injection before compaction, and child seam/pool identity.
- Original eighteen contract exports remain unchanged.
- README canonical Mermaid, boundary explanation, Go docs and interactive source
  now describe the actual loop scheduling and inherited pools.

Validation:

- Final `go test ./...`, `go vet ./...` and `go test -race ./...`: pass.
  Earlier focused synchronization/race runs also passed.
- `.venv/bin/python python/tools/export_go_contracts.py --check`: all 19 current.
  `.venv/bin/python python/tools/verify_scans.py`: all 19 anchored scans pass.
- Full `.venv/bin/python -m pytest -q`: **2148 passed, 28 skipped, 3 failed,
  24 subtests passed, 4 warnings**, 351.78 seconds. Failures:
  `test_agent.py::test_sessions_run_concurrently` (0.755 > 0.5 seconds),
  `test_double_cost.py::test_a_forty_turn_session_stays_fast` (1.112 > 0.5),
  and `test_curriculum.py::test_bash_background_flag_routes_through_background_manager`
  (empty completion injection after its fixed 50ms wait).
- Targeted rerun: background case passed; concurrent-session and forty-turn
  thresholds still failed (0.639 and 0.914 seconds). The previous lifecycle
  checkpoint already recorded those timing failures and a clean baseline
  archive's forty-turn failure. Root cause remains unconfirmed; this full gate
  is **not green**. Python package source, tests, waits and thresholds are unchanged.
  Warnings: three dependency deprecations and an asyncio subprocess-destructor
  event-loop-closed warning.
- Four targeted Python mutation guards passed: `injector-return-unchecked`,
  `broken-classifier-goes-parallel`, `batch-results-follow-completion-order`,
  `barrier-runs-before-the-group-settles` (one `verify_guards.py -k NAME` invocation
  per guard). They verify the source guard anchors; Go behavior is verified by
  its synchronization and differential tests above.
- New Go domain/service files contain no stored broad dynamic payload types.
  Python package invariants were not rerun because package modules are unchanged.
- Interactive map regenerated from JSON: **9/9 showcase checks**, zero errors or
  warnings. Spec SHA `a059e8a198bdcdc7e5672fb1e7b0e59e3115eb7cad97c50fcb84a94a7ee9bf6f`,
  HTML SHA `e409f2ce48bf1cc05762def9654ff25949f32fb50e4f662a16fffbcb1169d143`.
  Visual acceptance remains skipped after the earlier browser policy block;
  automatic layout validation does not claim rendered acceptance.
- README outline reviewed and `git diff --check` passed; final staged diff is
  checked before delivery.

Remaining: fleet composition/environment settings, mid-batch steering,
streaming/provider recovery, authenticated HTTP/SSE, SQLite and lease/restart
behavior, trajectory evidence, owner resources and optional feature services.
No dependencies added. G0/G1/G2 remain open until their complete required parity
contracts pass; G3–G7 remain open.

## 2026-10-03 process-local manager slice

**Scope and source evidence**

- Continued on `feat/go-port` from `a05b499`. Reviewed README architecture,
  manager construction/create/binding/get/list/delete/stop, reclamation and bounded
  owner history in Python; reviewed `EXTENDING.md` fleet/workspace seams and
  `HARDENING_NOTES.md` reclamation, cancellation and owner-map constraints.
- This slice implements a process-local service layer for subsequent HTTP work.
  G0–G7 remain open; no routes, authentication, transport, environment loader,
  trajectory/restore or optional fleet services are claimed. SQLite remains
  pending the existing dependency question; no dependency was added.

**Implementation**

- Added typed `ManagerConfig`, `ManagerServices`, `SessionDefaults`,
  `CreateSessionRequest`, `SessionBinding`, workspace/Bash factories, manager
  state, binding errors and deletion/cleanup receipts. Provider is required;
  identity/workspace are stamped at construction. Default interactive mode,
  50 rounds, shared model/tool pools of eight, approval broker, in-memory action
  journal and foreground host shell match the source manager path. Nil skills
  remain an empty catalogue; deployment loading must be injected until packaging
  and environment settings are ported.
- Creates reserve random 12-character IDs, isolate scratch/history and preserve
  creation order. Construction callbacks execute outside the metadata lock and
  recheck shutdown/caller cancellation before publication. Configuration slices
  and system pointers are copied. Factories cannot recursively create/delete/stop
  the manager; shared services must synchronize mutable state.
- Get/list/cancel/delete require explicit caller-established owners. Unknown and
  foreign IDs share `ErrSessionNotFound`. This typed library deliberately differs
  from Python raw manager's omitted-owner anonymous default and false-only
  missing deletion; HTTP authentication and anonymous identity establishment
  remain separate work.
- Binding resolves home prefixes and symlinks, checks disabled/outside/manager-root
  policy before existence (403), then checks allowed existing directories (400).
  Home expansion preserves the lexical symlink/.. suffix until resolution;
  an additional Go case covers policy after that ordered resolution.
  Explicit bound directories are retained. Scratch status reports
  `workspace_bound=false` even though every executor has a resolved cwd.
- Delete closes admission, unpublishes and revokes approvals, gives an active turn
  five seconds, then cancels and joins it before scratch removal. Shared live and
  retiring references prevent early removal. Retiring-reference release and
  reclamation use the same lock, so simultaneous shared-directory deletion cannot
  leave both cleaners skipping the final removal. A replaced root symlink is
  unlinked without following its target. Factory error/empty-path results never
  become owned cleanup paths. `PreserveWorkspace` and `WaitCleanup` are explicit.
- Stop closes all admission first, joins pending construction and deleted-session
  cleanup, and gives current holders 250ms before cancellation. It retains
  surviving scratch. Caller cancellation ends only the wait; later Stop joins
  the same shutdown. Non-cooperative provider/sink callbacks can delay draining.
- Remembered owners are capped at 10,000 and detached from callers; masked cleanup
  diagnostics are capped at 100 with bounded error text. Go reports removal faults
  while Python silently ignores them. These are process-local records.

**Evidence and gates**

- Added `python-manager.json`, the twentieth generated contract snapshot, using
  the actual Python manager: initial info/defaults, shared services, creation
  order, ten workspace outcomes, bound retention, deletion and stopped creation.
  Absolute temporary paths, random IDs and timestamps are normalized. The
  previous nineteen snapshots did not change.
- Go comparisons consume concrete fixture structs. Synchronization tests cover
  owner isolation, real default shell, queued admission closure, live workspace
  retention, deleted-turn shutdown joins, concurrent shared scratch deletion,
  symlink-only reclamation, construction during stop, factory failures and
  bounded detached owner/diagnostic views.
- `go test ./...`, `go vet ./...`, and `go test -race ./...` passed after the
  final home-path correction. `.venv/bin/python
  python/tools/export_go_contracts.py --check` reported all twenty current;
  `.venv/bin/python python/tools/verify_scans.py` passed all nineteen.
- `.venv/bin/python python/tools/verify_guards.py -k NAME` caught each of
  `workspace-removal-forgets-its-turn`, `owner-map-grows-without-bound` and
  `workspace-removal-follows-a-link`. They ran sequentially after full pytest;
  production source was restored by the mutation runner.
- `.venv/bin/python -m pytest -q` completed alone in 120.70s: **2,150 passed,
  28 skipped, 24 subtests passed, 1 failed, 3 warnings**. The remaining failure
  is `test_double_cost.py::test_a_forty_turn_session_stays_fast`: 0.6906s against
  the 0.5s wall-clock ceiling. Earlier scheduling/lifecycle checkpoints and a
  clean historical archive also recorded this timing failure. This gate is
  **not green**; no thresholds were relaxed and its cause remains unproven.
  The other two failures from the prior checkpoint passed in this run.
  Warnings are dependency deprecations. Log: `/tmp/mini-loop-manager-pytest.log`.
- Python production modules were not changed, so the package invariant verifier
  was not rerun. `git diff --check` passed; README outline was reviewed. No
  dependencies added. Full Go/contract evidence does not imply HTTP or durable
  parity.
- README baseline, canonical Mermaid, explanations and interactive specification
  were updated together. The interactive map folds ManagedSession into the Go
  SessionManager component. Archify delivery passed all 9/9 showcase checks, zero
  errors/warnings. Spec SHA
  `253cc93af41edaf73425c9548fb60f3202d4afd199ad7b9342cbc347cbfac272`,
  HTML SHA `2c803002b2fe744b379bb8c21c5af8093e597efbd5ac2377078754c2d3483187`.
  A prior browser file-policy block still prevents rendered visual acceptance;
  this receipt is automated composition evidence.

Next: authenticated HTTP/session routes and SSE over the manager, transcript and
trajectory/fork/steering boundaries, real provider transport/recovery, environment
composition, SQLite/restart recovery, optional features and release differential
checks. Overall goal remains active.


## 2026-10-03 typed HTTP and SSE slice

Baseline: `e6540af` plus this slice. The independent Go port remains incomplete;
this checkpoint advances G0/G3 and does not close G3 or the overall outcome.

### Implemented

- `go/httpapi.New(Config)` composes a standard `http.Handler` over the existing
  owner-scoped manager. Requests and responses use named concrete types. Generic
  JSON helpers instantiate concrete payloads; no untyped payload enters runtime
  state. Boundary-only fixture normalization uses RawMessage/maps.
- `Authenticator` admits one principal per request. Null auth is anonymous;
  configured bearer bindings use constant-time comparisons, reject conflicting
  duplicates, preserve environment precedence and accept query tokens only on
  event streams. The open-bind helper matches Python's loopback policy; the
  embedding application must call it before listening. No Go startup CLI exists.
- A ten-MiB declared/streamed body cap wraps all routes before authentication.
  Security headers cover authentication failures. Foreign and missing sessions
  are both 404. HTTP RunContext retains untrusted authority while stamping the
  actor and personal-skill capture-source capability, never human authority.
- Twelve method/path operations cover basic health; session create/list/detail/
  delete; message, message stream and cancel; approvals and resolution; events;
  and transcript's Null-store response. Listing clamps to 1..500 before Info work.
  Optional trajectories/workflows/steering/fork metadata truthfully stays disabled.
- Completed message replay keys include owner, session and idempotency key, capped
  at 1,024; fixed-minute owner rate windows are capped at 4,096 and default off.
  Cache lookup, rate spending and the in-flight claim form one locked transition.
  The claim survives cache publication, then releases before network output.
  ManagedSession.TryRunWithContext atomically refuses a busy turn; normal streams
  still queue and cannot replace another holder's cancellation target.
- SSE uses flat typed SessionEventRecord JSON, sequence IDs, event kinds, CRLF,
  no-store headers, bounded subscriptions, pings, cursor deduplication and optional
  agent_event envelopes. Closed clients release subscriptions and cancel their own
  submitted turn/wait. A real uvicorn/httpx source probe verified cancellation
  while the Python provider was blocked (`cancelled_after_disconnect=True`, busy
  false before server shutdown); this is separate from the finite-stream fixture.
- Optional manager Secrets project HTTP JSON and SSE data through the existing
  bounded recording boundary before escaping. Cached snapshots are projected on
  output; live model history remains raw. Projection errors/panics fail closed.
  This Go integration is verified explicitly and is not a claim that every Python
  optional HTTP sink already has identical masking coverage.
- The lifecycle fixture now compares the actual Go event wire serializer instead
  of a hand-written test-only projection. The new twenty-first Python snapshot
  captures authenticated/anonymous HTTP responses, auth/bind cases, the HTTP stamp
  and finite SSE frames (40 HTTP responses and 16 frames in total). Previous twenty
  snapshots remain unchanged. Source's
  NullStateStore transcript endpoint reports `no epoch 0 (current: 0)` with 404;
  no synthetic durable transcript is invented.

### Remaining boundaries

Full health posture and source-derived build fingerprint, standalone listening/
configuration/CLI, UI, mode/steering/fork, trajectory and optional route groups are
pending. JSON validation returns 422 but does not yet match FastAPI detail arrays,
scalar coercion or every redirect/method nuance. Empty model overrides differ at
the library validation boundary. SSE here streams session events, not a real model
transport; provider streaming/recovery remains G4. Durable cursor gap recovery,
SQLite, restart and lease recovery remain G5; no dependency was added. Optional
registered-secret recording does not imply confinement or a default-on registry.

### Validation

- `go test ./...`, `go vet ./...`, and `go test -race ./...` pass from `go/`,
  including actual-source fixture comparison, concurrent replay, authenticated
  approvals, disconnect ownership and raw-history/recording separation.
- `.venv/bin/python -m pytest -q`: **2,151 passed, 28 skipped, 24 subtests passed,
  three deprecation warnings, 154.06s**. The prior forty-turn timing failure did
  not reproduce in this run; no timing thresholds or Python tests were changed.
  This passing run does not establish the cause of the prior intermittent failure.
- `.venv/bin/python python/tools/export_go_contracts.py --check`: 21 snapshots
  current. `.venv/bin/python python/tools/verify_scans.py`: all 19 scans anchored.
  Python package modules are unchanged, so no package-invariant change is claimed.
- Related source guards all caught their mutations, invoked separately with
  `.venv/bin/python python/tools/verify_guards.py -k NAME`:
  `request-content-length-unchecked`, `request-streamed-bytes-uncounted`,
  `shared-token-silently-collapses`, `idempotency-key-ignored`,
  `rate-limit-never-fires`, `sessions-listing-unbounded`. The full mutation
  catalogue was not rerun for this slice.
- `git diff --check` passes. README architecture outline is verified. Archify
  `deliver architecture ... --quality showcase --json` passes **9/9**, zero errors
  and warnings. Specification: 24,692 bytes, SHA-256
  `2eb065ea861bcbc4c558ff5e38190c10f2a896a04778f6e086d6b3a2826ae562`;
  generated HTML: 663,692 bytes, SHA-256
  `c22d02335e3c32cb439de04ef0a2170b96bb00d0a1663ee356a996b079c60281`.
  HTML was regenerated from the frozen specification. Rendered visual inspection
  was not repeated because of the existing browser file-access policy block;
  acceptance here is the automated geometry/composition check.
- No dependencies were added. Go process/race evidence is macOS; it does not
  establish Linux execution or cross-process persistence.


## 2026-10-03 live controls and wakeup slice

Baseline: `6a11877` plus this slice. G1/G3 advance; the original full-port outcome
remains active. No dependency or Python runtime change is part of this slice.

### Python contract and typed implementation

- `ManagedSession.Steer` is a synchronous, process-local parking API callable
  from a provider/hook while the model is running. A separate `sessionControl`
  mutex protects named permission mode, steering and posture state without
  waiting for the transcript lock. The queue preserves arrival order, caps at
  100, and drops the oldest. Each input retains at most 16,000 Unicode characters
  plus `\n[steer truncated]`; scanning only the prefix avoids a whole-input rune
  allocation. Empty text remains accepted.
- After custom message injectors and before context facts/compaction, one user
  message wraps the drained batch in `<user_interjection>`. A typed
  SteeringDeliveredEvent records count and the first 2,000 characters. The live
  queue/history stays raw; registered-secret masking applies to event/HTTP copies.
  Delivery is once within the process; history retains the previous injection.
- `ChangePermissionMode` validates the three named modes before changing state.
  The gate loads the current mode after before/guard hooks, just before permission
  evaluation, preserving the single execution gate. Mode changes do not cancel
  pending approvals or revoke a decision already made. Capability fingerprints,
  extension authority snapshots, stop hooks and parent bindings see current mode;
  children keep their own selected mode and do not inherit control queues.
- A real change after the first run has started queues the exact Python meaning
  gloss in a separate `<posture_update>` message/event at the next model round.
  Initial changes and no-ops queue nothing. Multiple changes join in order with
  newlines. The posture-note queue retains source's uncapped behavior; the bounded
  steering claim does not apply to this separate queue.
- Two authenticated operations bring the handler total to fourteen:
  `POST /sessions/{id}/mode` and `POST /sessions/{id}/steer`. Foreign/missing IDs
  share 404. Steering spends owner rate budget; mode updates do not.
- HTTP steering of an idle session wakes a background turn and returns
  `{queued: 0, busy: false, delivered: new_turn}`; busy steering queues and returns
  `delivered: steering`. Admission selection and holder publication occur under
  the managed lock before the response, so simultaneous wakeups cannot publish
  extra idle turns. The existing managed active holder makes deletion/shutdown
  cancel and join the background turn. The request context does not cancel it.
  The wakeup uses default untrusted provenance, like Python's `session.run(text)`;
  it does not grant human/workflow authority or an HTTP capture stamp.
- `TryRunWithSnapshot` fixes the completion boundary for completed HTTP messages:
  it captures named SessionInfo before releasing admission, so a queued turn or
  idle wakeup cannot rewrite the returned turn's run count/status. A deterministic
  waiting second holder proves the result stays idle/count-one while current state
  has already reached running/count-two. The idempotency cache stores that snapshot.
- A steer arriving after the last model round may remain queued for a future turn,
  as in source. No retroactive model obedience or extra final round is invented.

### Evidence and remaining work

The twenty-second source snapshot runs six real managed scenarios: ordered
steers, Unicode truncation, drop-oldest bounds, silent pre-first mode change,
batched notes/no-op, and provider-time steer plus readonly refusal before a file
write. It records wrappers, delivery JSON, pending counts, mode, actual file effect
per-request injection counts and changing capability modes. Its HTTP portion uses a blocked async provider
and the actual FastAPI app for six mode/steer responses, busy/idle wakeup and owner
refusals. The previous twenty-one snapshots remain byte-identical.

Go tests compare the actual event serializer and model inputs, verify a mode
changed inside a before hook prevents the same call's write, exercise 250 concurrent
control mutations while a model is blocked, preserve raw history while masking
recordings, and prove a child cannot consume parent steering. HTTP tests cover
concurrent wakeups, background ownership across request cancellation, shutdown
joins, closed controls, validation status and the owner rate budget.

SQLite pending-steering persistence, masked durable queue projection, restore-time
redelivery and persistence diagnostics remain G5. Full FastAPI validation shapes,
CLI/UI, health posture/build fingerprint, fork/trajectory/optional routes and real
model transport/recovery remain open. The fake proves delivery and effects, not
live-model obedience; operator-gated Python provider tests remain skipped.

### Validation

- `go test ./...`, `go vet ./...`, `go test -race ./...` pass from `go/`,
  including the final completion-snapshot and capability-mode cases.
- `.venv/bin/python python/tools/export_go_contracts.py --check`: 22 snapshots
  current, previous 21 byte-identical. `verify_scans.py`: all 19 scans anchored.
  Guard mutations and final fixture generation/checks ran separately; Python
  package source is unchanged. No package-invariant change is claimed.
- Ten related source mutations are caught, invoked individually using
  `.venv/bin/python python/tools/verify_guards.py -k NAME`:
  `steering-is-never-delivered`, `steer-drops-the-callers-words`,
  `steer-queue-unbounded`, `steer-size-unbounded`,
  `a-stranger-steers-the-session`, `idle-steer-parks-forever`,
  `posture-changes-happen-behind-the-models-back`,
  `readonly-mode-asks-instead-of-refusing`, `auto-mode-widens-what-is-refused`,
  `capability-plan-ignores-permission-mode`. The full mutation catalogue was
  not rerun for this slice. These source checks complement the actual Go tests;
  they do not imply a Go mutation catalogue exists.
- Initial full Python run: **2 failed, 2,149 passed, 28 skipped, 24 subtests passed,
  three warnings, 140.48s**. Failures were `test_sessions_run_concurrently`
  (1.1058s against <0.5s) and `test_a_forty_turn_session_stays_fast`
  (1.2064s against <0.5s). Both then passed together in an isolated targeted run
  (2 passed, 0.71s). No test or timing threshold changed; the cause remains
  unconfirmed. Final full rerun after other checks: **1 failed, 2,150 passed,
  28 skipped, 24 subtests passed, four warnings, 160.01s**. Only the forty-turn
  threshold failed (0.5503s against <0.5s); the concurrency case passed. The fourth
  warning was an unraisable BaseSubprocessTransport destructor exception after an
  event loop closed. No causal link to the timing failure is established. The full
  Python gate is **not green**; targeted passes do not override this result.
- `git diff --check` and README outline are verified. Archify deliver passes
  **9/9 showcase checks**, zero errors and warnings. Specification: 24,976 bytes,
  SHA-256 `8a76e0de6c51edad91303ceb093c97e79cae088170059bf8bac964ce8ce3af2d`;
  HTML: 663,993 bytes,
  SHA-256 `131614742d80b3373826ac246d706b5b249b93361fd61c470ab4b562a6006366`.
  The exact frozen JSON produced the HTML. No rendered visual inspection was
  repeated because of the existing browser file-access policy block; only
  automated geometry/composition acceptance is claimed.
- No dependencies added. Evidence is on macOS; Linux execution, live-provider
  obedience and cross-process queue durability are not established.


## 2026-10-03 completed-boundary fork slice

Baseline: `07aaf13` plus this slice. G0/G3 advance; the full-port goal remains
active. Python runtime behavior is unchanged and no dependencies were added.

### Python contract and typed implementation

- Python `SessionManager.fork_session` copies an idle transcript, retaining owner,
  explicit system and current permission mode. The child uses manager default
  model/settings, fresh tool state and a newly provisioned scratch workspace.
  A bound source does not bind its child or copy workspace files. Pending steering,
  posture notes, run counters, cancellation state, Todo board and telemetry stay
  with the source. Historical runtime-state notes remain ordinary copied text.
- Named `ForkLineage` and `SessionForkedEvent` replace the static HTTP lineage
  placeholder. Lineage is detached in `Info`; history and lineage are initialized
  before manager publication. The source records the child's ID and snapshot count
  through the normal masked event serializer, backlog/subscription/sink boundary.
- Go `Fork(ctx, owner, id)` admits the source atomically and refuses an occupied
  admission slot with Python's exact open-turn error. This also covers Go's short
  terminal-event/release interval and simultaneous fork construction. An admission
  token pins the history only until its detached snapshot is captured; mode/control
  updates retain their independent lock. Construction does not expose an empty
  child. Snapshotting validates every paired row, allowing an empty history and
  repaired cancellation history. `protocol.Content.Clone` detaches string/block
  storage, thinking, caller metadata and typed tool-input arrays without JSON
  roundtrip size limits. Source callbacks do not hold the transcript mutex while
  factories or event sinks run.
- Fork creation uses the existing reservation/workspace/construction lifecycle.
  Manager shutdown joins it; cancelled/failed/unpublished creation cleans unused
  scratch and emits no successful fork event. A source admitted before concurrent
  deletion may still finish a fork from its captured boundary, matching an operation
  already admitted against the live source. The token is released before child
  provisioning so later source turns and idle HTTP steering can progress while
  factories run; their changes cannot alter the captured fork boundary.
  Child workspace policy is the existing
  configured factory; custom factories may deliberately share paths.
- `POST /sessions/{id}/fork` makes fifteen method/path operations. Ownership lookup
  precedes owner rate budget, busy sources return 409, foreign/missing share 404,
  and successful child `Info` is returned without a request body requirement.

### Differential evidence and remaining work

The twenty-third generated snapshot, `python-forks.json`, executes real Python
manager and FastAPI paths. It records paired Todo history, inherited fixed system
/current readonly mode, manager default model, fresh child board/counters/queues,
workspace marker absence, source divergence/isolation, empty forks, source events
and five HTTP outcomes (foreign, missing, empty, busy and completed). Go compares
actual request wire/cache projection and event JSON as well as typed history/Info.
Additional tests cover cancelled dispatched Bash results (completed plus unknown),
invalid open histories, construction failure, source wakeup while a
factory is blocked, idle steering during the copy itself, snapshot independence
from later source turns, shutdown join/cleanup and owner-scoped child routes/listings.

Python with a real StateStore flushes the copied history before its first turn;
Go has no shipped session store, so fork durability/restart and durable source
events remain G5. Provider transport/recovery, CLI/UI, health posture, trajectory,
optional services, user resources and complete FastAPI validation are still open.
No claim of complete G0-G7 parity or cross-process durability is made.

### Validation

- `go test ./...`, `go vet ./...`, `go test -race ./...` run from `go/`,
  **all pass**, including the final source-wakeup/copy concurrency cases.
- `.venv/bin/python python/tools/export_go_contracts.py --check`: **23 current
  snapshots**, previous 22 byte-identical. `.venv/bin/python
  python/tools/verify_scans.py`: all **19** scans anchored.
- `.venv/bin/python python/tools/verify_guards.py -k fork`: **4 caught** source
  mutations (`fork-cuts-an-open-turn`, `fork-shares-mutable-rows`,
  `a-stranger-forks-the-session`, `fork-leaves-no-trace-in-the-source`).
  Mutations completed before exporter/full-suite runs. The whole source mutation
  catalogue was not rerun, and no Go mutation catalogue is claimed.
- Full Python `.venv/bin/python -m pytest -q`: **1 failed, 2,150 passed,
  28 skipped, 24 subtests passed, three warnings, 221.24s**. The sole failure is
  `python/tests/test_double_cost.py::test_a_forty_turn_session_stays_fast`: 0.7194s
  against <0.5s. Targeted follow-up with that test and `test_session_fork.py`:
  **1 failed, 7 passed, one warning, 2.74s**; the performance case still measured
  0.7276s, while all seven fork tests passed. Python runtime and test thresholds
  are unchanged. The cause is unconfirmed; the full Python gate is **not green**.
  Python package invariants were not rerun because no package module changed.
- `git diff --check` and README outline checked. Archify deliver: **9/9 showcase**,
  zero errors/warnings, correction rounds **0**. Exact frozen specification:
  25,142 bytes, SHA-256
  `72d7ef586b6eff686b53e5d49596dc44624efa85a5cca13cd0c16197ac72bb6d`; generated
  HTML: 664,177 bytes, SHA-256
  `2b7f1b2398315dbbe0106a72522456eed94af50cc7aba52a4fd57afad582b4c7`.
  Diagram type: architecture. Output:
  `docs/mini-loop-system.architecture.html`. Rendered visual review is skipped
  because of the existing browser file-access policy restriction; automated
  acceptance does not prove rendered visual review.
- No dependencies added. Validation host is macOS; live providers, Linux behavior
  and cross-process fork durability remain unverified.


## 2026-10-03 direct provider and SDK retry slice

Baseline: `22d3ca2` plus this slice. G4 advances with a real HTTP adapter using
only Go's standard library. The full-port goal stays active; no dependency or
Python runtime change is included.

### Source contract and implementation

- Python `AnthropicCompatibleProvider.create_client` constructs AsyncAnthropic
  with its SDK defaults. This is a separate retry layer from `DefaultRecovery`:
  SDK 0.107.1 retries two times by default for connection/read/timeout failures,
  408/409/429/5xx, or `x-should-retry: true`; false overrides status classification.
  The fixture records the exact installed source hashes, not only version text.
- `go/provider.Client` implements the existing concrete `agent.Provider` seam
  without a special execution path. Config validates an explicit API key/base
  URL, optional standard HTTP client, byte limits, deadline, retry count and
  concurrency-safe waiter/jitter/clock dependencies. No environment, profile
  or bearer-token discovery occurs. Describe/String/GoString carry no credential.
- SDK backoff is 0.5s × 2^attempt capped at 8s with 0..25% negative jitter,
  unlike Agent recovery's 32s cap and positive jitter. SDK honors finite positive
  Retry-After up to 60s, including ms precedence and date parsing; larger, zero,
  negative, non-finite or malformed values fall back to SDK backoff. Original
  finite nonnegative Retry-After seconds remain a separate typed field for future
  Agent recovery's different 300s policy. Waits and calls honor caller cancellation.
- Default non-streaming budget preflight matches the pinned SDK: eight listed
  Opus aliases cap at 8,192; other models cap at floor(128,000/6) under its
  600-second estimate. An explicitly configured custom timeout bypasses this
  preflight, as in the SDK's custom-timeout path. These are SDK transport limits,
  not assertions about model capacity. Future SDK drift requires fixture review.
- Requests use `ModelRequest.MarshalJSON` so fitted schemas/system/message cache
  annotations are preserved and local Purpose stays off the wire. The endpoint
  preserves its base path and appends `/v1/messages`; API version is 2023-06-01.
  Retry-count headers are updated while body bytes remain identical.
- Bounded local ingress DTOs discard unconsumed response-level/usage SDK metadata
  and construct validated concrete replies. Consumed text, signed thinking,
  default-tool inputs, caller metadata, usage/cache fields and served-model alias
  retain their meanings. SDK-normalized missing tool caller becomes explicit null.
  Nullable text citation metadata is omitted; nonempty citations and unsupported
  server/search/redacted blocks fail explicitly until their typed variants ship.
- Named FailureKind/ErrorClass, status, request ID and distinct retry metadata
  preserve HTTP failure categories. Diagnostics are bounded and scrub the API key;
  transport failures do not print arbitrary client error strings. Body handles
  close on success/failure. The adapter defaults to 8 MiB request/reply caps
  (configurable up to 64 MiB) and bounds configured retries to 0..10.
- Go adds a whole-attempt 10-minute deadline instead of SDK per-operation
  connect/read/write/pool timeouts, refuses redirects to prevent forwarding
  x-api-key to a different endpoint, and rejects credential/query/fragment base
  URLs. Custom HTTP clients are shallow-copied so their redirect policy stays
  untouched; injected transports/waiters must honor contexts and be thread-safe.

### Evidence and remaining work

`python-provider.json` is the twenty-fourth source snapshot: 33 actual
AsyncAnthropic scenarios over httpx.MockTransport, with SDK sleep/random/time
patched only to record deterministic waits. It captures request body/path/headers,
consumed reply projection, error class/status, preflight requests and descriptions.
Prior 23 snapshots remain unchanged. Go compares every request/attempt, final
reply/error, delay and listed model ceiling. This is actual SDK protocol evidence,
not a claim that an external endpoint was called.

Go httptest tests exercise real HTTP timeout/cancellation and a complete two-call
agent tool round, confirming workspace write effects, immediate tool pairing,
served-model alias and usage telemetry. Additional tests cover response cap+1
reads, body close, request rejection before sending, malformed/unsupported replies,
credential-scrubbed errors/debug identity, redirect refusal without mutating a
supplied client, owned backoff waits and 32 concurrent calls with isolated bodies.

Streaming event assembly/coalescing/provisional generations, interrupted text
repair, Agent DefaultRecovery continuation/escalation/fallback/reactive compaction,
advanced request options, credential profiles/bearer environment discovery and
full content variants remain G4. Shared model permits currently cover each
Complete call, including its SDK waits. Live external conformance/cache savings
are unverified and operator-gated Python live tests remain skipped. No G4/G7
completion or default provider switch is claimed.

### Validation

- `go test ./...`, `go vet ./...`, `go test -race ./...`: **all pass**, including
  the final UTF-8/nullable metadata and HTTP runtime cases. No new dependency.
- `.venv/bin/python python/tools/export_go_contracts.py --check`: **24 current
  files**, prior 23 byte-identical; `verify_scans.py`: **19 anchored** scans.
  SDK version: 0.107.1. SDK `_base_client.py` SHA-256:
  `a6a53bd97f9cfe4ec55231791dd77961dc3a65da8b37799bb36e54c7e7e19d7e`;
  `_constants.py`:
  `c000de52a63796cb1e1742fa8c87a7f8f7b17d0d3a14eb5fd9d5444970ccf36f`;
  `resources/messages/messages.py`:
  `fb911dc0fd234fd1989de303928e322bd7796c074f54332f1a6b97e4183c51e5`.
- Three source mutations caught using sequential `verify_guards.py -k NAME`:
  `served-model-never-recorded`, `provider-seam-ignores-the-fake-flag`,
  `describe-leaks-the-credential`. Mutations finished before final export/full
  Python gates. The full mutation catalogue was not rerun, and no Go mutation
  catalogue is claimed. Python package invariants were not rerun because no
  Python package module changed.
- `.venv/bin/python -m pytest -q`: **2,151 passed, 28 skipped, 24 subtests
  passed**, three dependency deprecation warnings, 132.01 seconds. The previous
  checkpoint's timing failure remains historical; this run passed the full gate.
- `git diff --check` and README outline checked. Archify deliver: **9/9 showcase**,
  zero errors/warnings, correction rounds **0**. Specification: 25,495 bytes,
  SHA-256 `d4d4f5f95f2456f70d1331affa9b130f5783812def6710af452475d810cb70fd`;
  HTML: 664,569 bytes, SHA-256
  `6f2e458c48247a9a6f28c62357e0c49f9cc091df13e99c2132d375afcbc0be63`.
  Diagram type: architecture; output: `docs/mini-loop-system.architecture.html`.
  Visual review is skipped under the existing browser file-access policy block;
  only automated geometry/composition acceptance is claimed.
- Host: macOS. External live-provider conformance, cache benefit and Linux runtime
  behavior remain unverified. SDK mock HTTP and local real HTTP tests consume no
  external credentials or paid provider calls.

## 2026-10-03 typed streaming and interrupted-text slice

Baseline: `99d23a0` plus this slice. G4 adds explicit SSE model transport and
session-owned progress. Python remains unchanged outside its contract exporter;
Go's default Fake/direct provider selection remains explicit. G4 and the overall
port remain incomplete.

### Source contract and implementation

- Actual Anthropic SDK 0.107.1 `AsyncMessageStream.accumulate_event` and SSE
  decoding provide the source contract: text/thinking appends, signature replaces,
  input JSON fragments replace the initial tool input, final usage updates only
  present fields, response model remains authoritative, and HTTP SDK retries stop
  after successful response headers. Streaming bypasses the direct token ceiling.
- `provider.NewStreaming(Config)` constructs a shared `StreamingClient` implementing
  the existing Provider and optional consumer-owned StreamingProvider. Both use
  concrete requests/replies. Callback deltas have closed text/thinking kinds;
  callbacks are synchronous and stop before return. A standalone Complete can
  discard progress without discarding its final reply.
- SSE reads support CR/LF/CRLF, comments, multiline data and missing JSON type
  (using the event name), with network reads splitting UTF-8 and JSON arbitrarily.
  Unknown SSE event names and ping are ignored as in the SDK. Total wire bytes
  including ignored frames obey configured reply limits; individual frames,
  lines and assembled blocks obey the protocol 512 KiB bound.
- Partial input bytes exist only in the bounded ingress accumulator. A stopped
  block is converted to a concrete ToolInput and validated; incomplete tool inputs
  and unsigned thinking never enter transcripts or execute tools. Text/thinking
  progress is separate from stored signed thinking blocks. Text builders avoid
  repeated whole-answer copies while accumulating fragments.
- SDK retries cover failed opening/status responses. Once headers succeed, body
  drops/timeouts/errors return without replaying inside the adapter. Request IDs
  and finite Retry-After seconds stay typed; configured API-key diagnostics are
  scrubbed. Response bodies close on success, failure, consumer stop and cancel.
- A session selects StreamingProvider when its configured Provider implements the
  optional interface, retaining shared model permits, cache annotation, telemetry,
  served-model identity and usage. Direct providers preserve one-shot behavior.
  Existing manager/fork/child provider inheritance also carries the stream seam;
  all mutable accumulation belongs to the receiving session/call.
- Each send starts a fresh ephemeral `stream_start`, then coalesces progress at
  200 Unicode characters or 200 ms checked on fragment arrival (no timer flush),
  matching Python defaults. Delta events are masked provisional commentary with
  the same stream ID that final assistant_text uses for authoritative phase.
  Both ephemeral variants are offered live but excluded from replay/history epoch
  accounting. Optional recording sinks receive detached masked progress records.
- Only flushed answer text, never thinking or pending fragments, is recoverable
  on managed cancellation. Repair appends it above the interruption marker only
  when no dangling tool results need immediate pairing. Partial text clears on
  successful completion, including internal compaction calls, and after repair.

### Evidence, differences and remaining work

`python-streams.json` is snapshot 25. Nine actual SDK/StreamingTransport calls
capture final replies, raw SDK text/thinking deltas, coalesced masked progress,
partial state, HTTP body/attempt headers, retries and closed bodies. Cases include
CR, CRLF, multiline data, missing type, HTTP 429 before opening, empty refusal,
midstream ReadError and an SSE error frame. The fixture records exact SDK and
Python transport source hashes. SDK-only nullable citations/parsed_output metadata
are omitted explicitly from the consumed projection.

Go compares all nine final outcomes/progress streams/HTTP attempts. SDK streaming
leaves a body ReadError as its raw httpx class; Go uses a named, bounded connection
Failure instead. The drop case compares the connection category rather than
claiming identical error-class strings. Read timeouts likewise use named timeout
Failure. Session tests
compare eight coalesced transport outcomes and partial state (network-read drop
is covered by the provider and cancellation tests). Real httptest model calls run
a complete signed-thinking/write_file/final-answer round through a fleet session,
checking next-request thinking signature/tool pairing, filesystem effect, cache
wire, served-model usage and final phase/stream correlation. Other cases test
owned cancel/read timeout, consumer callback failure, body close, invalid UTF-8,
unsupported delta, ordering/index/signature/JSON errors, line/total bounds, fresh
generations, shown-text repair and successful internal-call clearing. Thirty-two
shared-client calls check isolation under the race detector.

Go deliberately requires message_delta/message_stop and stopped validated blocks
before accepting a final reply. The SDK may expose an unchecked partial snapshot
at clean EOF; Go reports an incomplete connection failure. Go rejects invalid
indices/discriminator mismatches/unknown content delta variants explicitly; SDK
accumulation is more permissive. Nonempty citations, advanced content/request/auth
variants and custom coalescing settings remain pending. Dropped streams do not yet
regenerate automatically: DefaultRecovery retry/continuation/escalation/fallback/
reactive compaction is the next G4 work. No durable SQLite state, CLI/UI streaming,
external-provider conformance or production cache savings is claimed.

### Validation

- Streaming-targeted Go tests passed before the final gates, including the
  actual SDK corpus, real tool round/cancellation/timeout and child inheritance.
- Final `go test ./...`, `go vet ./...`, `go test -race ./...`: **all passed**.
  A prior attempt encountered missing files in the host's shared Go build cache;
  final commands use isolated `GOCACHE=/tmp/mini-loop-go-stream-cache`. No
  dependencies were added. Intermediate fixture/helper compile mismatches were
  corrected before these final gates; no unsuccessful attempt is counted as green.
- `.venv/bin/python python/tools/export_go_contracts.py --check`: **25 current
  files**. Prior 24 snapshots are byte-identical. `verify_scans.py`: **19 anchored**.
  SDK 0.107.1 `_streaming.py` SHA-256:
  `65b4253475703abbdbcccdab8b2ae1a9451bc787c6f617b8cd0f8a5e7340347d`;
  `lib/streaming/_messages.py`:
  `cf8088c4e60919a7d4d67c3dcfba6c16ff17eef28fd69348174e584c0630047b`;
  Python `transport.py`:
  `ce7fc744213272848cc5ba01e06b851d49ca106da0560746d5ae7b6929503873`.
- Four source guards caught, run sequentially before the final exporter/Python
  suite: `stream-deltas-unmasked`, `completed-stream-leaves-stale-partial`,
  `deltas-are-replayed`, `retry-does-not-announce-itself`. Full catalogue and Go
  mutation catalogue were not run. Package invariants were not rerun because
  no Python package module changed; only the export tool changed.
- Full `.venv/bin/python -m pytest -q`: **1 failed, 2,150 passed, 28 skipped,
  24 subtests passed**, three dependency warnings, 278.24 seconds. Sole failure:
  `test_double_cost.py::test_a_forty_turn_session_stays_fast`, measured 1.136820s
  against the existing 0.5s gate. Go final commands overlapped this run, but no
  causal attribution to scheduler/load is proven. Targeted follow-up
  `.venv/bin/python -m pytest -q python/tests/test_double_cost.py`: **1 failed,
  11 passed**, 1.44 seconds; the same gate measured 0.639308s. The timing gate
  remains failing in this iteration and is not described as passing.
  Python runtime and the performance test are unchanged in this slice.
- `git diff --check` and README outline checked. Archify validation/delivery:
  **9/9 showcase, zero errors/warnings**, correction rounds **0**. Frozen
  specification: 25,562 bytes, SHA-256
  `23100ec84701bdda73eaad19aed9c2045470c0dcb87372babbf8a25986a6a558`;
  artifact: 664,609 bytes, SHA-256
  `ca18410228be28c91c2709ad128932d6e7b4be06240d6a63c4d12d766404c8f4`.
  Diagram type: architecture; output: `docs/mini-loop-system.architecture.html`.
  Visual review remains skipped under the existing browser file-access policy
  block; automated acceptance only is claimed.
- Host: macOS. External endpoint/cache conformance, Linux runtime and optional
  durable storage/trajectory remain unverified. All HTTP provider tests use local
  httptest or SDK MockTransport with synthetic keys; no paid provider calls.

## 2026-10-03 default Agent recovery slice

Baseline: `8645782` plus this slice. G4 adds the Python DefaultRecovery algorithm
behind a typed consumer-owned seam; the full goal remains active.

### Implementation and source evidence

- RecoveryInput/RecoveryServices/ModelCall/Recovery are concrete named contracts.
  Nil runtime/fleet Recovery selects DefaultRecovery; DirectRecovery explicitly
  opts out. Config is immutable after construction, with no mutable shared retry
  state. Children/forks inherit policy configuration and start fresh fallback state.
- SSE overloaded/rate-limit error types retain typed recovery categories even
  when the human message is neutral, matching the SDK error-body classification.
  Local HTTP tests verify both regenerate without losing that metadata.
- Provider Failure exposes detached protocol.ModelFailure, preserving kind, class,
  status, bounded message and finite Retry-After seconds. SDK's listed model map
  is now shared in protocol; recovery still distinguishes listed ceilings from
  the generic direct transport preflight. Message heuristics retain Python's
  overload/rate/context/streaming recognition; arbitrary Go connection errors
  need typed connection/timeout evidence instead of Python class-name guessing.
- Default outer retries: ten, 0.5s exponential base capped at 32s, positive 0..25%
  jitter. Finite nonnegative Retry-After seconds cap at 300s, with 300s total
  outer sleep. This budget does not include HTTP/SDK elapsed time. Context owns
  every wait/call; model permits release between attempts and during outer waits.
- Three overloads may select a configured fallback, which persists on subsequent
  session calls. Default fallback is unset. Each model_start/model_end spans the
  logical recovery call; usage/served identity comes from the final response only.
- Escalation regenerates to 64K, capped only by a listed direct SDK ceiling, and
  requires at least 1.5x headroom. Unknown-model SDK refusal restores budget and
  retains the paid-for front as a continuation chunk. Up to three continuations
  concatenate all content with final response metadata. Truncated tool replies
  return for immediate dispatch instead of appending an unanswered continuation.
- Reactive shrink runs once, retains a tool use/result pair across the cut, and
  rebases only retained request cache markers. It retries only after actual
  outgoing token-estimate reduction and mirrors raw live agent history explicitly.
  Summary/internal calls do not mirror into the conversation. Policy events occur
  at mutation time, so event epoch tracking observes the rewrite correctly.
- Recovery event variants carry optional typed attempt/budget/capped/model/reason
  fields. Projection masks/detaches them before sinks/replay; DefaultRecovery emits
  failure events, DirectRecovery does not invent them. Existing lifecycle, refusal
  and direct-provider fixtures remain green after fixing empty-content clone
  preservation in the continued-reply helper.

Snapshot 26, python-recovery.json, records 24 actual DefaultRecovery/DirectRecovery
trajectories with complete request, event, wait, final response and mirrored-history
projections. Error class labels are supplied as typed fixture evidence; terminal
Go diagnostic type spelling differs and is normalized while action/reason/status
meaning remains explicit. Tests also cover real dropped SSE regeneration, unique
stream IDs/no spliced transcript, released shared permits, persistent fallback and
fresh child state, cache-marker rebasing, bounded jitter/header/config, cancellation
and detached event pointers.

Remaining G4 includes advanced content/auth/request options, custom coalescing,
protected token-efficiency projections, unique fake IDs and live endpoint/cache
conformance. SQLite/durable recovery state, CLI/UI and optional features remain
G3/G5/G6. No dependencies or Python runtime modules were changed. Constructor
retry/continuation values are bounded to 0..100 (Python accepts unbounded custom
integers); empty truncated content fails explicit validation instead of creating
an invalid empty assistant transcript. G4/G7 are not marked complete.

### Validation

- `go test ./...`, `go vet ./...`, `go test -race ./...`: **all passed**,
  including the final rerun after neutral SSE classification. Commands use
  isolated `GOCACHE=/tmp/mini-loop-go-stream-cache`, including final recovery
  corpus, stream/fallback/cache cases and existing runtime suites.
- `.venv/bin/python python/tools/export_go_contracts.py --check`: **26 current
  snapshots**; prior 25 byte-identical. `verify_scans.py`: **19 anchored** scans.
  Python recovery.py SHA-256:
  `bf388d43dcebb900aee263d060f9be9db756a7f30783fffd423f5ae94de99aca`.
- Four source mutations caught sequentially before export/full Python tests:
  `continuation-returns-the-tail`, `refused-escalation-loses-the-partial`,
  `continuation-orphans-a-tool-call`, `a-patient-server-can-hang-a-turn-forever`.
  Full mutation catalogue and Go mutations were not run. Python package
  invariants were not rerun because no Python package module changed.
- Full `.venv/bin/python -m pytest -q`: **2 failed, 2,149 passed, 28 skipped,
  24 subtests passed**, four warnings, 271.83 seconds. Failures are the existing
  timing gates: `test_agent.py::test_sessions_run_concurrently` (0.735815s versus
  0.5s) and `test_double_cost.py::test_a_forty_turn_session_stays_fast` (>0.5s).
  No Go test/compile overlapped this full run. Python package/test source is
  unchanged; no causal attribution is proven. Targeted follow-up of both failed
  tests on 2026-10-04: **2 passed in 0.97s**. The full-suite result remains failed;
  this follow-up does not establish the cause or replace that result.
- `git diff --check` and README outline checked. Archify validate/deliver:
  **9/9 showcase, zero errors/warnings**, correction rounds **0**. Specification:
  25,675 bytes, SHA-256
  `d4e9e4048dfe64f87e040c82a434a5d469cb03e52cb8918cf774cae6d0aed54a`;
  HTML: 664,722 bytes, SHA-256
  `d0eb681e20bc581259f685bbe09523fa3bf3cc32b171501d064749fa69b5a459`.
  Diagram type: architecture; output: `docs/mini-loop-system.architecture.html`.
  Visual review skipped under the existing browser file-access policy block;
  automated acceptance only is claimed.
- Host: macOS. External endpoint/cache savings, Linux runtime, durable restore
  and protected token-efficiency projections remain unverified. No paid calls.

## 2026-10-04 stream progress and stateful fake slice

Baseline: `3f4c110` plus this slice. G4 closes configurable progress and fake-client
identity/thinking/stream behavior; the complete Go port remains active.

### Python contract and typed implementation

- StreamProgressConfig captures optional character and duration thresholds plus a
  named StreamClock. Nil selects 200 Unicode characters/200 ms. Explicit zero or
  negative thresholds flush each nonempty fragment, matching Python's constructor
  behavior. Empty fragments do not trigger a flush. Duration checks occur only on
  arrivals; no timer flush is added. Go time.Duration expresses finite durations;
  Python's nonfinite float settings are not a representable Go configuration.
- Runtime constructors copy threshold values; manager captures them before later
  session creation/fork; children inherit the same policy with fresh generation
  buffers and shown-answer state. Shared custom clocks must be concurrency-safe.
- FakeProvider is now a pointer-owned client with an atomic call sequence. Its
  zero value and typed NewFakeProvider defaults match Python's default responder,
  signed thinking, zero direct delay and 8192 nonstreaming ceiling. Existing call
  sites now pass pointers; copying a used client is unsupported.
- FakeProviderConfig captures Thinking, Delay, NonStreamingCeiling,
  DisableNonStreamingCeiling and a named FakeResponder returning FakeGeneration.
  The client stamps usage/model/message identity and optional thinking. Direct
  calls advance before delay/responder; streaming advances after the responder,
  with the Python pre-increment thinking-signature ordinal. Delay honors contexts;
  rejected requests consume no ordinal, while cancellation in an admitted delay
  consumes that ordinal. Responder callbacks receive detached requests and must
  synchronize shared state and honor contexts.
- fake.Streaming() is an explicit view over the same client, never an implicit
  switch for direct sessions. It lifts the direct ceiling, ignores direct delay,
  fragments text/thinking into thirds by Unicode characters and stops callbacks
  before return. Default tool-use ID remains toolu_1, as in Python; only client
  message IDs are a call sequence. Thinking signatures survive actual tool rounds.
- protocol.Block.Thinking exposes a detached named value; no dynamic payload,
  dependency or Python runtime module changes were introduced.

### Differential evidence and remaining work

Snapshot 27, python-progress.json, records nine actual Python StreamingTransport
scenarios using a timed event adapter: defaults, character/elapsed thresholds,
zero/negative thresholds, mixed thinking/text masking/interruption and an unshown
failed tail. It compares every event and raw recoverable partial. The existing
SDK wire corpus remains separate. Ten actual FakeAsyncAnthropic direct/stream
calls compare complete replies, signatures, input/output/cache usage, model IDs,
client call counts and Unicode fragments for thinking enabled/disabled.

Go tests inspect publication while still inside a provider callback, proving
manager snapshot/fork/child thresholds apply before final flush. They also prove
32 concurrent shared-client calls retain unique identities/signatures, independent
clients restart their own sequence, direct ceiling rejection consumes no ordinal,
streaming lifts that ceiling, and delay cancellation retains the admitted ordinal.
A complete fake streaming tool round validates paired signed history and clears
completed progress. Prior 26 snapshots are byte-identical.

Fake delay environment discovery, advanced content/request/auth modes, protected
token-efficiency projections and live endpoint/cache conformance remain G4/G7.
CLI/UI, durable restoration/persistence and optional services remain G3/G5/G6.
No production endpoint or paid model calls were made.

### Validation

- `go test ./...`, `go vet ./...`, `go test -race ./...`: **all passed**.
  Commands use `GOCACHE=/tmp/mini-loop-go-stream-cache` and include complete
  agent/provider/protocol/HTTP/workspace/shell suites, not only new cases.
- `.venv/bin/python python/tools/export_go_contracts.py --check`: **27 current
  snapshots**, prior 26 byte-identical. `verify_scans.py`: **19 anchored scans**.
  No Python package-module changes; package invariants were not rerun.
- Three source mutations caught sequentially before the final export/Python run:
  `completed-stream-leaves-stale-partial`, `thinking-replayed-as-answer`,
  `stream-deltas-unmasked`. Full mutation catalogue and Go mutation checks were
  not run; no broader mutation claim is made.
- Full `.venv/bin/python -m pytest -q`: **2,151 passed, 28 skipped,
  24 subtests passed**, three existing deprecation warnings, 94.09 seconds.
  No Go compile/test or source mutation overlapped this full run. This successful
  run does not establish the cause of prior timing-gate failures.
- Archify validate/deliver: **9/9 showcase, zero errors/warnings**, correction
  rounds **0**. Diagram type: architecture. Output:
  `docs/mini-loop-system.architecture.html`. Specification 25,860 bytes:
  `4314931ecd6fd8c1b9196672f63cb70a0d5295b065c2960317d37efb23c6e681`;
  HTML 664,931 bytes:
  `264044e41d892e29c9d3777e55c1d9307959413076381b6020cf921d6133d8b5`.
  Visual review skipped under the existing browser file-access policy block;
  automated acceptance only is claimed.
- `git diff --check` and README outline: **passed**. Host: macOS; Linux
  runtime and live endpoint/cache behavior remain unverified. No dependencies
  or paid model calls.

## 2026-10-04 typed configuration and standalone launcher slice

Base: `05a5b53` (stream progress/stateful fake). G3 advances; the overall port
remains incomplete and G0–G7 remain open.

### Implementation and source evidence

- `go/config.Settings` models all 49 Python dataclass fields using named modes,
  optional pointers, integer bounds and explicit seconds. Environment parsing
  preserves source defaults, Unicode decimal digits, between-digit underscores,
  strict boolean normalization, exact legacy fake/feature truthiness, path
  resolution and positive/cross-field validation. Credentials use an explicit
  reveal seam and detached credential-free snapshots; diagnostics remove URL
  userinfo/query/fragment data.
- Source exporter snapshot 28, `python-configuration.json`, executes actual
  `Settings()` under 64 isolated environment cases: default/core/optional/path
  overrides, malformed numbers/booleans/enums, AST pinning, artifact/workflow
  relationships and forty zero/negative positive-bound failures. It also pins
  the actual default skill bytes, descriptions and loaded wrapper. Existing
  27 snapshots remain byte-identical. Tests compare every accepted setting and
  the reject/accept outcome, without claiming literal Python exception messages.
- `go/skills` embeds the exact `python/skills/code_review/SKILL.md` deployment
  asset. The standalone binary needs no Python or source checkout. Explicit
  filesystem catalogues keep their bounded reader, UTF-8/newline behavior and
  digest recheck; builtin data is immutable compiled content.
- `launcher.New` resolves an explicitly owned fake/direct model adapter, skill
  catalogue, configured fallback recovery, bound shell, manager defaults/shared
  pools/approval timeout/workspace binding, auth, rate settings and HTTP handler.
  Failed construction cleans owned services. Loading/inspection creates no files.
  `App.Serve` owns its TCP listener, rechecks the actual bind address, and joins
  HTTP plus manager shutdown. The command supplies bounded cleanup on every exit.
- `cmd/miniloop` adds HTTP startup and SIGINT/SIGTERM handling, HOST/PORT, captured
  fake delay, and redacted settings-and-availability inspection. Empty hosts are
  refused because Go's empty TCP host binds all interfaces. Requested and actual
  unauthenticated binds are both checked. Header/idle deadlines exist; global
  read/write deadlines do not terminate long model/SSE calls. Signal cancellation
  cancels request contexts as well as manager-owned turns, then uses an independent
  ten-second shutdown deadline and closes the owned provider transport.

### Remaining boundaries

Python enables trajectory recording and a separate full-output spill store by
default. Serving currently requires explicit `MINILOOP_TRAJECTORIES=0` and empty
`MINILOOP_SPILL_DIR`; enabling those unavailable services or feature/workflow/
guardian/decision/token-efficiency/AST/owner-resource/memory integrations fails
activation. Inactive optional settings still validate. Existing compaction spill
artifacts do not implement the separate complete tool-output store. Implementing
these defaults is remaining work, not a changed default or a claim of full parity.

Go performs no `.env` discovery/override, reload, manager/provider posture probe or
Python source-build fingerprint. The inspection report is intentionally labeled
settings-and-availability, not full effective posture. Go adds finite number/int/
duration bounds (minimum one nanosecond for positive durations), and validates
fake delay even when the real provider is selected. Python's constructor creates
its workspace; pure Go loading leaves creation to manager construction. No new
dependency or production model invocation occurred. UI, exact FastAPI validation,
trajectory/durable restoration/SQLite and optional capabilities remain pending.

### Validation

- Narrow configuration/launcher/command/skills tests: **passed** after the final
  64-case snapshot was generated. The real upstream HTTP test checks model/output
  settings and the embedded skill in the request, then observes a configured
  one-second timeout from the actual manager-bound Bash tool in the next request.
- `GOCACHE=/tmp/mini-loop-go-stream-cache go test ./...`, `go vet ./...`, and
  `go test -race ./...` from `go/`: **passed** on the final runtime/test code.
- Built `/tmp/mini-loop-go-launcher` with standard Go tooling. Outside-repository
  startup, no-file inspection, authenticated HTTP create/mode/tool round, and
  SIGTERM both at idle and during a one-hour fake delay: **passed**, exit 0.
  Go TCP tests also join an active message and queued SSE stream on shutdown.
- `export_go_contracts.py --check`: **28 files current**; the previous 27 snapshots
  are byte-identical. New source case 64 rejects hexadecimal float syntax that Go
  strconv accepts but Python float refuses. `verify_scans.py`: **19 anchored**.
- Source mutation checks: **5 caught** (`config-guesses-a-number`,
  `config-guesses-a-duration`, `config-reads-a-typo-as-true`,
  `bash-timeout-zero-times-out-everything`, `approval-timeout-zero-denies-everything`).
  These pin Python reference guards; Go fixture/bounds tests independently check
  the port. Python package invariants were not rerun: no package module changed.
- Python full suite, first run: **1 failed, 2150 passed, 28 skipped, 3 warnings,
  24 subtests passed**, 200.72s. Existing `test_a_forty_turn_session_stays_fast`
  measured 0.898939s against its unchanged <0.5s gate. Individual reruns measured
  0.620813s and 0.700156s and still failed.
- Python full suite, final run: **2 failed, 2149 passed, 28 skipped, 3 warnings,
  24 subtests passed**, 196.32s. Existing `test_sessions_run_concurrently` measured
  0.624537s and the forty-turn case 0.648273s, both against <0.5s. Final isolated
  pair: **1 passed, 1 failed**, 1.43s; concurrency passed, forty turns measured
  0.727225s. No Python runtime/test source or gate was changed. A live process
  snapshot showed substantial other CPU work; attribution was not proven. Python's
  full gate is **not green**; this remains a source performance validation limit.
- Archify architecture `validate` and `deliver --quality showcase`: **9/9**, zero
  composition errors/warnings. One schema repair shortened the Go view note to
  its 140-character bound; no geometry repair was required. Frozen JSON SHA-256
  `c88dc5e7228491624dd6c6134be2f6fbac97c504bfa5cd04f5360ff885721166`,
  26,598 bytes; HTML SHA-256
  `7ea7725a686adc4154b3c06fad8ed65ccc2e44fbfc5b4caa489c1dd2e7af6ad3`,
  665,812 bytes. Interactive HTML was generated, not hand-edited. Visual inspection
  remains unperformed because the earlier local-file browser access was denied;
  no alternate access bypass was attempted.
- README architecture baseline/outline and `git diff --check`: **passed**. Host:
  macOS; no Linux execution or production endpoint/cache evidence, no dependencies.

Next G3 work is to port the Python default full-output spill and trajectory services
so standalone startup no longer needs those explicit opt-outs. UI/full posture,
G5 durability and G6 optional services remain on the plan; overall goal stays active.

## 2026-10-04 private spill and string-Bash preservation slice

Base: `5928c71` (typed configuration and standalone launcher). G3 advances;
the overall port remains incomplete. No dependencies or Python runtime behavior
were changed.

### Implementation and actual source boundary

- Added named `spill.Namespace`, `Request`, `Ref` and `Store`, with a stdlib
  `LocalStore`. It preserves already-masked UTF-8 bytes, an 8,000,000-byte ceiling,
  SHA-256 namespace grouping, sanitized filenames, 16-hex random prefixes,
  private root/new namespace permissions (0700), exclusive leaves (0600), and
  file sync before successful return. Namespaces group by workspace basename,
  as Python Toolset does; they are not session ownership ACLs.
- `shell.ExecuteBash` now matches Python string `Toolset.run_bash`: outputs over
  50,000 Unicode characters save stripped full captured text and append a locator,
  byte count and retrieval hint. Split streams are masked before projection and
  preservation. Store errors or plugin panics keep the same preview. Captured
  text remains bounded by the shell cap; Unicode text can exceed the independent
  store byte cap and then retain just its preview.
- **Actual source gap:** Python's default `_bash` calls `run_bash_result`, which
  has no preservation call. An actual managed Python tool round with 60,004 output
  characters returns a 49,989-character preview and creates zero spill artifacts.
  Go's default typed Bash handler calls `ExecuteBashResult` and preserves that
  behavior. The new string policy is a compatibility surface, not a claim that
  default model-visible Bash results now preserve complete output. Any future
  source behavior correction must be a separate explicit change.
- Typed `RuntimeConfig.Spill` and `ManagerServices.Spill` bind the service to an
  independent real shell executor, preserving credential scope and shared process
  tracking. The caller's executor remains unchanged. Completed forks retain the
  service with a fresh workspace namespace; selected child handlers inherit the
  bound executor. Custom non-shell executors own their preservation policy.
- Launcher startup best-effort constructs a store at the configured root
  (`./var/spill` by default). Empty disables it; root construction failure disables
  preservation without failing startup, matching Python manager behavior. Spill
  is no longer listed as an unsupported activation. Trajectory still requires
  explicit opt-out; other unavailable integrations retain their refusal checks.
  Config inspection creates no directories and reports settings/availability only.
- Go additionally checks invalid UTF-8, context cancellation and short writes;
  failed-write cleanup checks that the leaf still has the created inode before
  attempting removal. The identity check and unlink are not atomic against host
  tampering. Python does not
  verify short writes or clean up a failed partial leaf. Neither implementation
  confines parent-component filesystem races or establishes a host-shell sandbox.
  Existing namespaces are retained; no TTL/purge is implemented, and session
  deletion/application stop retain spill evidence. Compaction's workspace artifacts
  remain separate.

### Differential evidence and validation

The 29th snapshot executes fourteen real Python store cases, ordinary collisions,
a planted leaf symlink, eight real Bash commands (including Unicode, nonzero exit,
no store, broken store and a split secret), three CommandResult projection recipes,
the actual managed default adapter, and three manager construction cases. Content
and normalized previews compare byte SHA-256 digests, with explicit character/byte
counts and modes. Previous 28 snapshots remain unchanged. Go tests also cover 32
concurrent unique writes, cancellation/invalid text, plugin panic containment,
independent credential rebinding, fresh fork namespaces and retained stop evidence.

- Narrow `go test ./spill ./shell ./agent ./launcher ./config ./cmd/miniloop` passed.
- Full `go test ./...`, `go vet ./...`, and `go test -race ./...` passed.
- `export_go_contracts.py --check`: **29 files current**.
- `verify_scans.py`: **19 scanning guards anchored**.
- Three source mutation checks caught their mutants:
  `spill-save-failure-breaks-the-tool-call`, `spill-artifact-is-world-readable`,
  and `spill-follows-a-planted-symlink`. Source mutations were restored before
  starting the full Python suite. No Python package-module changes require an
  invariants rerun in this iteration.
- A freshly built binary ran outside the checkout with fake provider,
  trajectory opt-out and **default spill settings**. `--dump-config` created no
  files; startup created a 0700 spill root; an authenticated auto-mode real-shell
  tool round completed; the structured adapter created zero preservation artifacts;
  SIGTERM exited 0 and retained the root. No paid provider was contacted.
- README architecture baseline/Mermaid/boundary explanation, extension seams,
  Go README and parity matrix were updated. The architecture JSON was regenerated
  into HTML, with Archify showcase **9/9 checks, zero errors/warnings**.
  JSON: 26,957 bytes, SHA-256
  `f21d3232d2f9cb8670ce98f5c42605dcec57367b88a5d29d7b8ee60e961be8a6`.
  HTML: 666,368 bytes, SHA-256
  `5aa13a1a3d31167eafaa956ecd2628a22235c91bffa6caf5b3b2c3446169a1bb`.
  Visual inspection remains unperformed: prior local HTML browser access was
  denied, and no alternate access was used to bypass that restriction.
- Full `.venv/bin/python -m pytest -q`: **2,151 passed, 28 skipped,
  24 subtests passed, three dependency deprecation warnings, 246.36s**. Unlike
  the preceding launcher checkpoint, this full run has no timing-test failures.
- Final `git diff --check` passed.

Next G3 work is the default trajectory service and its REST/UI evidence boundaries.
SQLite persistence/restore/leases, UI, optional services and the full release audit
remain open; G0–G7 are not marked complete by this slice.


## 2026-10-04 per-run trajectory recording slice

Reviewed base: `097ada3`. This advances G0/G1/G3/G5 without closing G0–G7.

### Implementation and actual source boundary

- Added `go/trajectory`: private append-only JSONL, source schema/ID format,
  start/end headers, summary/count/list/full document/stream, corruption handling,
  recursive content privacy, 32 process-local locks and bounded distinct diagnostics.
- Added named agent writer/reader/store interfaces and typed start/finish/metadata,
  metrics, summary, stamp and terminal receipts. Full model requests/replies and
  tool/child text are synchronous private recording details; the 200-record live
  backlog retains capped public fields. JSON trees remain local wire projections.
- Managed runs start before status publication, record ordered non-ephemeral events,
  and capture terminal status/duration before final append. File finish precedes the
  live persistence receipt. Count/start/append/finish failures degrade to masked
  recording diagnostics without rewriting turn outcomes. Disabled receipts retain
  the current Null-state fields. Children contribute scoped details to their parent.
- Manager composition binds the shared store; default deletion retains files.
  Explicit library RemoveTrajectories drains active writers before purge and reports
  backend cleanup faults. HTTP adds four owner-scoped list/inspect/export operations
  (nineteen total). Recorded owners survive deletion/restart; legacy null owners use
  bounded remembered owner state. Owner checks precede full JSON or streaming reads.
- Launcher enables trajectories by default at the configured root or workspace-root
  `.trajectories`; root failure refuses startup, while private spill remains best
  effort. Dump-config is still side-effect free. No dependency was added.
- Python source files persist full capture by default and do not fsync trajectory
  writes. Go also does not claim crash durability, cross-process locking, host path
  confinement or session restoration. New root/file modes are 0700/0600; existing
  modes remain unchanged. Go adds a 64 MiB single-record cap and a byte-bounded JSON read that also
  counts malformed/racing appended bytes, and streams 64 KiB
  bytes instead of Python's character chunks. JSON inspection/export cap eight MiB.
- Source snapshot 30 executes eight actual store cases (including redaction,
  malformed tails and last-end wins), seven managed success/cancellation/failure
  cases, sixteen actual HTTP results, and ten binary-float rounding boundaries.
  The previous 29 snapshots remain unchanged. Named typed fixtures compare these
  results; additional Go tests cover concurrent append, full-versus-live output,
  registered masking, terminal finish ordering, active purge and stream failures.

### Remaining work

Trajectory HTML view and filtered event iteration remain pending, along with full
posture/FastAPI validation, UI, SQLite session/approval/lease recovery, durable SSE
catch-up and optional features. JSONL export observes the source file as it is read,
not a frozen cross-process snapshot. The interactive map aggregates the trajectory
file service inside Go SessionManager; the canonical Mermaid shows its separate
file boundary. Implementation gates and delivery receipts are recorded below.

### Validation

- `go test ./...`, `go vet ./...`, `go test -race ./...`: all pass.
- `.venv/bin/python python/tools/export_go_contracts.py --check`: 30 current files;
  the previous 29 tracked fixtures have no changes.
- `.venv/bin/python python/tools/verify_scans.py`: all 19 scanner guards anchored.
- Seven targeted source mutations were caught: summary-reads-the-whole-body,
  trajectory-export-readable-by-anyone, trajectory-json-export-unbounded,
  trajectory-json-inspect-unbounded, trajectory-input-recorded-with-the-secret,
  owner-not-written and deleted-session-recordings-immortal. These prove the current
  Python guard witnesses; Go differential/concurrency tests cover the port separately.
- A built binary ran outside the checkout with fake clients and default trajectories:
  dump-config created no files; owned full model inputs and exact JSONL export were
  retained with file mode 0600; ordinary delete retained recordings; a new process
  read the same owned document and returned foreign 404 with no restored sessions.
  SIGTERM during an hour-delayed model call wrote a cancelled terminal; all three
  processes exited 0. No real model provider was contacted.
- README outline reviewed; interactive specification regenerated with Archify
  showcase acceptance: 9/9 checks, zero errors/warnings. Frozen specification
  SHA-256 `83ff56cac24d0a73ecf5d960d2e98c1a838a5bb68731b261643f554b480bec89`
  (27,523 bytes); HTML SHA-256
  `5507c3cb699b4c3991c3d3c8718ad53742d5ccc6c1a12a0423cfd6e96b0af3c9`
  (667,031 bytes). Visual browser inspection was not performed: prior local-HTML
  access was rejected, and no alternate access was attempted.
- Initial Python full suite: one existing timing guard failed (`test_double_cost`
  forty-turn offline model took 1.57 s against a 0.5 s threshold); 2,150 passed,
  28 skipped, 24 subtests and three warnings in 106.64 s. No Python package/test
  modules changed. Isolated failing test rerun: one pass in 0.34 s. Complete
  suite rerun passed: **2,151 passed, 28 skipped, 24 subtests passed, three warnings
  in 99.45 s**. The failed first run remains recorded above.
- `git diff --check`: pass. Python package modules were not changed, so
  verify_invariants is not applicable to the exporter-only change.


## 2026-10-04 typed trajectory ledger and reader slice

Reviewed base: `d79ac86`. This advances G0/G3/G5; G0–G7 remain open.

### Implementation and actual source boundary

- `go/traceview` folds recorded wire JSON into named Ledger/Row/Field/Metrics
  structures. Dynamic JSON remains transient in decoding/formatting; domain rows
  contain plain inspector strings and concrete scalar fields. Model/tool spans fold
  by span ID, child agent/depth remain visible, every request shares one numbering
  space, and only parent agent-turn calls advance parent steps.
- Unknown events remain visible; reference catalog/system/capability payloads collapse
  into compact rows with detailed inspectors. Bookkeeping/done mirrors collapse
  without hiding a final answer. Unclosed spans show in-flight/start markers, never
  fabricated duration. Usage totals cover every row before the 2000-row tail cap.
  Source preview/inspector character limits and omission notices are preserved.
- One escaping boundary covers all supplied HTML text. Embedded CSS/filter JavaScript
  preserve the actual Python source language, font stack, spacing and light/dark
  colors. HTML is self-contained. Generation time is explicit for repeatable tests.
- A twentieth HTTP operation, GET `/trajectories/{id}/view`, checks recorded owner
  before size/full-document reading, shares the eight-MiB source cap and existing
  security headers, and applies the configured recording projection before rendering.
- Independent `cmd/traceview` reads an exported file, trajectory ID or up to 500
  recent stored session turns in chronological order and writes new HTML at 0600.
  Flags may follow the target as in argparse. Missing end means interrupted; blank
  lines are ignored, malformed JSON marks partial, and the last end wins. Existing
  output modes remain unchanged; writes are not fsynced, as in Python.
- The offline file reader has no whole-file HTTP size cap. It retains the previously
  documented Go 64 MiB record limit. Operator-selected reads have no HTTP ownership
  admission or filesystem sandbox claim. No dependency was added.
- `TrajectoryReader.VisitRecords` accepts a typed type/limit query and detached
  encoded bytes at the visitor boundary. Nil types includes headers/ends; empty types
  yields none. Limits count yielded records, so filters can scan the whole file.
  Missing/open-unreadable files yield none like Python; cancellation/visitor errors
  propagate, and no append lock spans callbacks.
- Source snapshot 31 executes eleven complete HTML scenarios, exported-file assembly,
  nine iterator outcomes, four real HTTP owner/foreign/invalid/oversized results,
  and source CSS/JS hashes. Existing thirty snapshots remain unchanged. Tests compare
  entire rendered HTML byte for byte, not merely the presence of route names.

### Remaining work

Main console UI/browser assets, full health/effective posture, full FastAPI validation,
SQLite state/lease/approval recovery, durable SSE catch-up and optional integrations
remain. File evidence and offline ledgers do not restore sessions. Source's open JSON
coercions for malformed records are not a broad Go domain contract; strict typed
runtime decoding remains intentional and further wire-variant audit belongs to G7.

### Validation

- `go test ./...`, `go vet ./...` and `go test -race ./...` passed; both
  `cmd/miniloop` and `cmd/traceview` built successfully.
- The source exporter regenerated snapshot 31 and its `--check` passed with all
  31 files current. The previous thirty snapshots and Python package modules have
  no changes. `verify_scans.py` confirmed all 19 scanning guards are anchored.
- `verify_guards.py` caught all eight selected trace-view/ownership/reference
  mutations. Source files were restored before the final export/check and tests.
- Built binaries ran outside the checkout over real TCP: owned HTML returned 200,
  a foreign owner returned 404, injection rendered as escaped text, file/session
  CLI inputs produced new 0600 HTML, and graceful server shutdown exited zero.
- The actual Go page was visually inspected in the browser. Child nesting and
  open-span markers remained visible; expanding a tool showed its inspector and
  searching `compaction_summary` filtered the rows. The browser test server was
  stopped after verification.
- Archify regenerated the interactive map from JSON: 9/9 checks, zero errors and
  zero warnings. The delivered JSON/HTML SHA-256 digests match the saved receipt.
  Architecture-map visual inspection remains unperformed because local HTML
  access was denied; the independently allowed product-page inspection above is
  separate evidence.
- `.venv/bin/python -m pytest -q`: **2151 passed, 28 skipped, 24 subtests
  passed in 81.97 s**, with three existing dependency deprecation warnings.
- `git diff --check` passed and the README outline was verified.
  `verify_invariants.py` is not applicable: no Python package module changed.


## 2026-10-04 embedded browser entry slice

Reviewed base: `2651690`. Previous turn delivered authoritative code and receipts;
this turn advances G3. All G0–G7 remain open.

### Implementation and actual source boundary

- Go embeds the original `/` development console and `/ui` full interface. The
  HTML/CSS/JS preserve source language, fonts, layout, accessibility, command
  palette, shortcut handling and client interactions. No dependency was added.
- `python/tools/export_go_webui.py` extracts the console literal through the Python
  AST and copies the three webui sources into `go/httpapi/uiassets`. `--check`
  rejects stale copies. The independent binary assembles the source's first CSS/JS
  markers from immutable embeds; it requires neither Python nor checkout assets.
- Both GET shells remain public even with wrong credentials, matching source:
  browser navigation cannot carry authorization. Session data still requires the
  admitted principal; query-token fallback remains limited to event streams.
  Ingress bounds and all existing security headers are preserved. No static mount
  or asset/traversal handler exists. The total is twenty-two method/path operations.
- Snapshot 32 captures sixteen actual Python HTTP outcomes: public/valid/invalid
  shell access, wrong methods/HEAD, gated session reads, rejected query token,
  nonexistent static child and favicon. Whole successful response hashes and the
  four embed input hashes/byte counts are compared. Previous 31 snapshots remain
  unchanged. The real source DOM harness runs against Go's embedded inputs and
  passes all 37 interaction cases, not against the Python asset directory.
- A built binary outside the checkout served the actual browser: first-message
  session creation, fake model/tool/final rows and retained trajectory listing
  worked; `/` exposed the cross-linked console. Actual layout was inspected.
- The full source UI also calls optional Tasks/Team/Goal/Cron/Workflows/Skills/Memory/
  Improvement/Benchmark/Self-audit APIs not yet implemented in Go. The browser
  demonstrated the Tasks error state. Those gaps are retained explicitly rather
  than claiming the shell proves feature coverage. No optional state is fabricated.

### Remaining work

Implement optional panel services with their typed domain contracts, full health
posture, exact validation semantics, SQLite recovery and durable SSE catch-up.
The default Null-store Transcript response remains source-equivalent, not durable.

### Validation

- `go test ./...`, `go vet ./...`, and `go test -race ./...` all passed.
  `cmd/miniloop` built, ran outside the checkout and exited zero on SIGTERM.
- `.venv/bin/python -m pytest -q`: **2151 passed, 28 skipped, 24 subtests
  passed in 78.97 s**, with three existing dependency deprecation warnings.
- Contract exporter `--check`: 32 snapshots current; browser exporter `--check`:
  all four inputs current. The previous 31 snapshots remain unchanged.
- `verify_scans.py`: all 19 guards anchored. The selected
  `webui-ships-without-its-script` mutation was caught, then source was restored
  before export/check and full regression. No Python package module changed;
  `verify_invariants.py` is not applicable.
- Sixteen actual-source HTTP cases pass over a real test listener, including
  whole page hashes, HEAD/method handling and unchanged security headers. The
  embedded script passes all 37 Node DOM regressions with zero failures/skips.
- Built-binary browser inspection verified core create/send/tool/final/trajectory
  flows and the development console. The test tab was closed and server stopped.
- README outline and `git diff --check` pass. Archify delivered 9/9 showcase,
  zero errors/warnings, correction_rounds: 0. Specification SHA-256:
  `3da2b61f16ecc3411ae0e442826384827e3e1a83f69d616952097199393f5d6a`
  (28,374 bytes); artifact SHA-256:
  `e63543d514696941af899e0261fb4aedab07db5182a8b74a93ee5608440dca1d`
  (667,925 bytes). Architecture visual review remains skipped due the previously
  denied local HTML access; product-page browser inspection is separate evidence.


## 2026-10-04 persistent task graph slice

Reviewed base: `b5eb2f1`. Previous turn made authoritative implementation progress.
This advances G0/G2/G3/G6; G0–G7 remain open.

### Implementation and actual source boundary

- `go/tasks` has named Task/ID/Owner/Status/Diagnostics values, dependency readiness,
  create/save/load/list/render/runnable/claim/complete/worktree binding operations.
  Unknown status/malformed open dataclass coercions are rejected at wire admission.
- Subject/description caps, Unicode previews, first-to-last field JSON ordering,
  structural credential masking, same-directory atomic replacement, file fsync and
  best-effort directory sync match source. Modes follow umask. Go adds a 64 MiB
  read cap; save also uses the existing bounded recording projection. No dependency.
- Process-local coordination uses 32 striped root locks rather than an unbounded
  root-lock map. Exclusive `.owner` creation is the cross-process claim authority.
  Crash-window markers remain, report the holder/operator path and never authorize
  takeover. Completed state lands before marker removal; no leases or general
  cross-process serialization are claimed.
- Render scans all rows for missing dependencies, keeps the 50-row tail and previews
  subjects at 200 characters. Complete task data remains addressable. Diagnostics
  retain 50 distinct messages with occurrence/eviction counts; files are not pruned.
- Named closed protocol variants, cloning, optional-null identity, canonical hashes
  and input masking cover the five graph tools. Description null is rejected as a
  non-string wire variant; dependencies/worktree null keep source default behavior.
- Explicit `RuntimeConfig.TaskTools` / `ManagerServices.TaskTools` opt-in installs
  five tools through the existing handler/gate. Readonly refusal precedes directory
  creation; task ownership uses the agent label, not an invented HTTP actor. Manager
  sessions get separate boards; forks retain activation and start fresh. Comprehensive
  `MINILOOP_FEATURES` remains unsupported and the ten defaults stay unchanged.
- A twenty-third operation, GET `/sessions/{id}/tasks`, admits the owner before
  opening the fresh workspace store. It lists typed rows without descriptions and
  never consumes claim state. Like source, an empty read can create the directory;
  the file store is an operator capability, not a host symlink sandbox/ACL.
- Snapshot 33 executes 53 Python store steps, six actual installed-tool outputs,
  five schemas/traits and five owner/foreign/missing/empty/unauthenticated HTTP
  outcomes. Previous 32 snapshots remain unchanged. Real Go subprocess contention
  admits one claimant and spent-marker tests prove completed work cannot reopen.

### Remaining work

Other optional services and panel APIs, comprehensive feature activation, full health
posture, SQLite recovery, durable SSE catch-up and full validation semantics remain.
Persistent task files do not restore a Go session, task runner or process lease.

### Validation

- `go test ./...`, `go vet ./...`, and `go test -race ./...`: pass.
  `cmd/miniloop` built and ran outside the checkout with an isolated fake provider.
- `.venv/bin/python -m pytest -q`: **2151 passed, 28 skipped, 24 subtests
  passed in 101.70 s**, with three existing dependency deprecation warnings.
- Source contract exporter `--check`: 33 files current; source browser assets:
  four files current. The previous 32 snapshots and Python package modules have
  no changes. `verify_scans.py` confirms all 19 scanning guards are anchored.
- Seven selected task guard mutations were caught: credential-field masking,
  mask-before-serialize, missing dependencies, row cap, subject cap, corrupt-file
  reporting and exclusive cross-process claims. Full regression ran afterward.
  `verify_invariants.py` is not applicable: no Python package module changed.
- Source fixture comparisons cover all 53 store steps, six full installed-tool
  outputs, five schemas/traits and five HTTP results. Tests also cover typed
  input cloning/null/canonical/masking boundaries, readonly no-directory refusal,
  manager activation, owner workspaces, fresh forks and real subprocess claims.
- The real built-binary browser showed two structured task rows and their owner/
  dependency states. An independent Go library invocation completed the first
  task, confirmed the second was runnable and claimed it. Refresh displayed
  completed/in_progress states. Source UI still labels stored dependency IDs
  `blocked by` even when the blocker has completed; this source wording is
  preserved, and the store's readiness/claim evidence is separate.
  The browser tab was closed and the isolated server exited cleanly after SIGTERM.
- README outline and `git diff --check`: pass. Archify delivered 9/9 showcase,
  zero errors/warnings, correction_rounds: 0. Specification SHA-256:
  `6d6818447cb145b5ea330dbc404bcfaf751d0f23d043f46b52233c6e171f2b35`
  (28,768 bytes); artifact SHA-256:
  `5b1ee063dd948c6cf7041f91d12469ca26b297af126ad1f1590348334660c358`
  (668,366 bytes). Architecture visual review remains skipped due previously
  denied local HTML access; the actual product-page layout was inspected.

## 2026-10-05 operator worktree lifecycle slice

Reviewed base: `6e1a136`. The previous goal turn measured current statement
coverage and verified the functional inventory; this turn advances G0/G6 with
an executable worktree service. G0–G7 remain open.

### Implementation and actual source boundary

- `go/worktrees` ports the complete Python WorktreeManager service and workspace
  factory: named Name/TaskBoard/Changes/Event values, explicit repository,
  default versus explicit-empty path/branch overrides and source name validation.
- Create verifies an existing task before Git, creates the branch/worktree, binds
  the task and appends an audit event. Binding/audit failures after Git preserve
  created work for inspection; these effects are not a transaction or rollback.
- Remove rejects unknown status, dirty files or commits ahead of current repo HEAD
  unless discard was explicit. Git receives ordinary remove and branch `-d` by
  default; `--force`/`-D` require discard. Git's separate checks preserve work
  arriving after preflight and unmerged branches after a stale ahead count.
- Successful directory removal ignores branch deletion failure like Python; the
  branch can remain for review. Keep/remove/create events append local JSONL with
  named types. This is neither fsynced durable dispatch nor an ownership/lease ACL.
- WorkspaceFor preserves source sanitization, existing paths and ordinary-directory
  fallback after Git failure/non-repo/unborn HEAD. That fallback is not Git branch
  isolation. Its embedding caller owns retention and Git-aware removal.
- Real sessions can use explicitly provisioned paths through existing bindable-root
  and owner admission. Bound paths survive session deletion/manager stop. The
  service does not install a scratch factory/reclaimer, model tools or live rebind.
- Git uses concrete argv, the source inherited environment, a context-owned
  30-second deadline, five-MiB capture per output channel and bounded pipe cleanup.
  Go requires an explicit repository and replaces malformed UTF-8 diagnostics.
  Context cancellation cannot undo already landed Git/binding/audit effects.
  No dependency was added and no host symlink sandbox is claimed.
- Snapshot 34 runs nine real Python/Git scenarios and 58 steps: output, file/branch
  effects, binding and audit fields, including custom/empty configuration, invalid
  preconditions, dirty/ahead state, missing tasks, duplicate names and fallbacks.
  Only nondeterministic audit times, commit hashes and Git list column padding are
  normalized. Previous 33 source snapshots and Python package modules are unchanged.

### Remaining work

Model-facing create/remove/keep/list/enter tools, atomic live workspace/file/shell/
sandbox/context/approval rebind, Git-aware manager reclamation and comprehensive
feature activation remain. Background, cron, teams, workflows, user resources,
decisions, SQLite restoration/leases, durable SSE and release audit remain open.
A worktree service and retained task/audit files do not restore a session or runner.

Next implementation order (source-grounded design, not shipped behavior):

1. Add five closed worktree inputs/schema/null/identity/masking variants and an
   explicit constructor opt-in; preserve source risks and the common gate.
2. Separate manager-owned lifecycle workspace from mutable execution workspace.
   Python AgentSession retains its original workspace, while Agent.enter_workspace
   replaces the toolset/agent workspace. Go currently uses core.workspace for both
   authority and reclamation; merely changing that field could delete entered work.
3. Rebind files, foreground argv/sandbox, context facts, approval authority and fresh
   child handlers together at the serialized tool barrier, preserving task-board
   roots and original cleanup ownership. Port background re-confinement when that
   service exists. Prove entered work survives deleting original scratch.

### Validation

- `go test ./...`, `go vet ./...`, `go test -race ./...`: pass, including all
  real Git source comparisons and the owned bound-session integration.
- Independent Git tests verify post-preflight dirt survives, unmerged branches
  survive an obsolete preflight, partial binding/audit faults preserve created work,
  duplicate creation serializes and cancelled calls create no workspace.
- Real owned session: a common-gate Bash write lands in the selected worktree,
  foreign owner lookup refuses, main checkout remains untouched, deletion/stop
  preserve the work and Git registration, and ordinary removal refuses it.
- Source guard mutations `worktree-removal-always-forces` and
  `worktree-keeps-nothing-unverified`: both caught. Full source regression afterward.
- Source exporter `--check`: 34 files current. `verify_scans.py`: all 19 scanning
  guards anchored. Native Git: `2.39.5 (Apple Git-154)` on macOS; Linux behavior
  has not been validated.
- `.venv/bin/python -m pytest -q` ran twice after the source guard mutations.
  First: **2150 passed, 1 failed, 28 skipped, 24 subtests passed, 3 warnings**
  in 145.78 s. The 40-turn test took 0.605 s against its 0.5 s bound.
  Its first isolated recheck passed. Full retry: **2149 passed, 2 failed,
  28 skipped, 24 subtests passed, 4 warnings** in 195.53 s: concurrent sessions
  took 0.575 s and 40 turns 0.669 s, both against 0.5 s bounds. The fourth
  warning was an unraisable asyncio subprocess finalizer after loop closure.
- A later joint timing recheck also failed (0.563 s concurrent sessions and
  2.668 s forty turns). At that point host load averages were 30.76/22.16/15.12.
  Python runtime/test files are byte-unchanged in this iteration. Host contention
  is a plausible explanation, not a proved cause. Neither assertion was relaxed
  or skipped; **the full Python regression gate is not green**. This remains a
  release-validation limitation despite passing Go/contract/source-guard gates.
- Archify architecture delivery: 9/9 showcase, zero errors/warnings,
  correction_rounds: 0. Specification SHA-256:
  `deba23eb5016a9814abd0dbefd4c523da496f5c9c19ad1dc05a63e614c2dd70b`
  (29,272 bytes); artifact SHA-256:
  `83b3a1005d21b382f2e2294693e710217ae70b1368d4d956c6aafc4b4973dd4a`
  (668,900 bytes). Visual review remains skipped due previously denied local HTML
  access; no rendered visual success is claimed. Canonical Mermaid carries the
  distinct operator library path; interactive cards describe the same separation.
- README outline and `git diff --check`: pass. `verify_invariants.py` is not
  applicable: no Python package module changed. No source asset or scanner target
  changed; verification is scoped to this worktree service/exporter iteration.

## 2026-10-05 gated worktree tools and execution workspace slice

Reviewed base: `108fb01`. The previous goal turn measured committed Go statement
coverage at 87.6% and identified the ungenerated in-progress worktree-tool fixture.
This implementation advances G0/G6; it does not close the complete Go port.

### Implementation and actual source boundary

- Five concrete worktree input variants/schemas install only through explicit
  RuntimeConfig/ManagerServices.WorktreeTools. Worktrees selects the repository
  service; a nil service retains the source unconfigured error. The default ten
  tools and standalone unavailable-feature refusal stay unchanged.
- Closed typed input decoding, clone/masking and Python canonical identities
  preserve optional null/omission and explicit discard. The exact source schema,
  risk, readonly and empty capability metadata is retained. Default child roles
  therefore omit these optional tools; a trusted role policy may select them.
- enter_worktree always creates an exclusive scheduling barrier, including when
  an injected classifier proposes parallel execution. The scheduler joins prior
  workers before preparing files, executor/sandbox, immutable catalogue copies,
  write verifier and broker approval/question surfaces. A cancelled, failed, nil
  unbound or wrong-root preparation refuses publication; later tools use the new scope.
- Session now keeps separate lifecycle and execution workspace values. Model
  context, tool/turn/stop authority and subsequent child environments follow the
  execution root. Managed Info, task HTTP reads, trajectory attribution and
  scratch cleanup retain the original lifecycle root, matching Python's
  AgentSession/Agent split. Deletion of original scratch preserves entered work.
- Existing task stores stay pinned. create_worktree lazily admits a task store
  even without a task ID; a first lazy admission after entry uses that execution
  root. No team workspace, runner recovery or durable activation is implied.
- Built-in shell.Executor.WithWorkspace binds cwd/sandbox together while retaining
  secrets, spill, deadline/capture policy and foreground interrupt ownership.
  Existing executor/catalogue copies and children retain their previous scope.
  Custom executors require an explicit WorkspaceBashFactory; managed sessions
  reuse Services.BashFactory and require a WorkspaceBashExecutor reporting the
  requested root. The embedding factory owns policy preservation.
- The gate retains hooks, journal, observers, live mode, secrets and diagnostics.
  Entry settles/observes with the authority under which it was admitted; subsequent
  calls see the new root. Broker session/owner identity, grant scope, redaction
  and event sink stay bound. Fresh child handlers inherit current files/shell,
  but start without the parent worktree service or task board, matching actual
  Python child state. Even an explicitly selected worktree tool therefore reports
  unconfigured. Forks retain manager activation and fresh scratch.
- Snapshot 35 executes actual Python tools on real managed Agents: three flows,
  25 steps, five schemas/trait sets and eight canonical inputs. It covers linked
  tasks, missing entry, dirty refusal, two switches, unconfigured service and late
  task-board initialization. Structured Bash output uses CommandResult.__str__;
  only temp roots, Git hashes/list padding are normalized. The previous 34 source
  fixtures and all Python package/test files remain unchanged.
  A real child loop with explicitly selected tools records three additional tool
  results, proving the service is absent while inherited files/shell use the
  entered directory. This check corrected an initial Go service inheritance
  assumption before delivery. Source child role prompts stay fixed, matching Go;
  the default runtime system builder consumes the current SystemContext root.
- Canonical Mermaid shows the explicit service plus common-gate model path and
  scope preparation. The interactive map aggregates this service under Go
  Resources and documents the execution/lifecycle split in its boundary cards.
  No dependency was added.

### Remaining work

Git-aware SessionManager worktree provisioning/reclamation and full feature
activation remain. Background, cron, teams, workflows, owner resources/memory,
decisions, SQLite session/approval/lease restoration, durable SSE, remaining
provider variants and the full G7 differential/release audit remain open.
No OS sandbox backend, host ACL, cross-process lease or exactly-once activation
claim follows from this process-local workspace switch. Custom handlers, hooks,
approvers and system builders receive current authority/context and retain their
explicit contracts; supplied fixed prompts stay fixed. Background sandbox rebinding must be ported
with that service, as pinned by the existing source mutation.

### Validation

- Final `go test ./... -coverpkg=./... -coverprofile=...`, `go vet ./...` and
  `go test -race ./...`: pass across all packages, including the added custom
  factory success/cancellation cases. Aggregate statement coverage is **87.7%**;
  that measures implemented Go statements, not feature parity.
- Scoped real tests prove failed/nil/unbound/wrong-root/unavailable/cancelled preparation
  keeps old bindings; custom factory success retains the old executor; parallel
  writes finish before entry despite an attempted parallel classifier; immutable
  catalogue copies/write verifiers remain correctly scoped; journal settlement
  precedes observers; broker permission/question joins retain owner identity;
  fresh children retain the entered files/shell without borrowing the manager
  worktree service and forks start in fresh scratch; deleting
  original scratch preserves entered files. Shell behavior verifies sandbox cwd,
  credential injection/masking, capture/deadline policy, private spill and shared
  foreground interruption after rebinding.
- `.venv/bin/python python/tools/export_go_contracts.py --check`: **35 files
  current**, with the previous 34 byte-identical. `verify_scans.py`: all **19**
  scanner guards anchored. Selected source mutation
  `worktree-switch-leaves-background-misconfined`: caught. The full source mutation
  sweep was not rerun; no Python runtime, source scanner target or guard anchor
  changed. `verify_invariants.py` is not applicable to this exporter-only change.
- `.venv/bin/python -m pytest -q`: **2151 passed, 28 skipped, 24 subtests passed,
  4 warnings**, 80.65 s. This iteration's full Python gate passes, including the
  previously failing 0.5 s timing assertions; those assertions were not changed.
  A subprocess-transport finalizer warning after event-loop closure still occurs,
  alongside existing deprecation warnings. The skipped operator/live-provider
  cases remain unvalidated; no production endpoint or paid model call was made.
- Archify validate/deliver: **9/9 showcase**, zero composition errors/warnings,
  correction_rounds: 1 (child state fidelity). Specification SHA-256:
  `8680d34feaf6010f0cd19986e0fe1d1ed4296a5a727ac92eff1c4edcb132d3c9`
  (29,865 bytes); artifact SHA-256:
  `b0e28baabad82a385b574095a1609533ba88c7ce10b456c59d2d7929e3670ba0`
  (669,496 bytes). Visual review remains skipped because local HTML access was
  previously denied; no rendered visual acceptance is claimed.
- README/plan outlines and `git diff --check`: pass. Validation ran on macOS with
  Go 1.23.3 and native Git 2.39.5 (Apple Git-154); Linux remains unvalidated.

## 2026-10-05 managed worktree factory slice

Reviewed base: `a3ae8b9`. The previous status turn refreshed Go statement coverage
at 87.7% and confirmed the four in-progress factory files. This iteration
completes the explicit library factory composition in G6; G0–G7 remain open.

### Implementation and actual source boundary

- `agent.WorktreeWorkspaceFactory` has a concrete private `*worktrees.Manager`
  field and implements the typed WorkspaceFactory seam. Its constructor refuses
  a nil service; an uninitialized receiver refuses allocation. Context
  cancellation reaches the service before any directory or Git work. No
  dependency, default activation, model-tool installation or manager lifecycle
  mutation was added.
- Reviewed actual Python SessionManager.create/delete/stop and
  worktree_workspace_factory, plus the workspace extension seam and the
  worktree removal/shared-Git sandbox hardening notes. Source create treats all
  factory paths as scratch, including nonrepo/unborn/branch-conflict fallbacks.
  Stop retains the directory. Ordinary delete removes even dirty directories
  after draining/references, leaving Git registration and branches. Explicit
  bound admission and preserve retain the work. The guarded WorktreeManager
  Remove API is a separate explicit operator/model boundary.
- This source finding corrects the earlier checkpoints' unsupported expectation
  of a pending Git-aware manager reclaimer. Python has no such path. The Go
  adapter keeps source scratch semantics and records that cleanup gap. The
  existing Go manager additionally reclaims unpublished scratch on construction
  failure; Python create leaves the allocation. This Go cleanup also leaves Git
  registration/branch and is documented as a difference, not source parity.
- Snapshot 36 executes seven actual Python/Git managed cases, with directory,
  linked `.git`, Git registry, branch and real common-gate Bash marker probes
  before/after cleanup. It covers clean/dirty delete, dirty preserve, dirty stop,
  nonrepo/unborn/conflicting-branch fallback, owner, scratch flag and output. Only
  temporary root/session identities are normalized; Git objects are queried
  directly. The previous 35 snapshots stay unchanged.
- Go integration tests compare all seven source states and verify explicit
  service admission does not activate optional tools or bypass owner lookup.
  Additional native Git tests prove a shared surviving holder keeps dirty work
  until the final delete and executor construction failure reclaims only the
  unpublished directory. Existing shared/retiring/cancel/stop/symlink manager
  guards stay in force.
- README Mermaid adds the explicit manager-to-worktree factory edge. The
  interactive map aggregates factory provisioning under SessionManager and
  records the source cleanup gap, retention and failed-admission difference.

### Remaining work

Standalone feature activation, background, cron, teams, workflows, owner
resources/memory/decisions, MCP, SQLite session/approval/lease restore, durable
SSE, remaining provider variants and the full G7 differential/release audit
remain open. Worktree provisioning and Git registration do not supply a host
ACL, OS sandbox backend, cross-process lease or session recovery.

Next G6 sequence, grounded in the actual BackgroundManager source:

1. Port concrete background IDs/status/task/result records and the operator
   service: independent process groups, command/environment guards, deadlines,
   bounded capture, retained results/listing, durable in-flight ledger and
   orphan reporting. Cancellation requests join handles before service close.
2. Add exact source schemas/inputs/results and the common-gate model handlers,
   source-derived fixtures and bounded next-turn completion injection. A turn
   cancellation leaves background work alive and reports its live count.
3. Compose explicit manager activation, drain it before delete/stop, and update
   future background cwd/sandbox when entering a worktree. Preserve source ledger
   and fresh-child/fork ownership rules before standalone feature activation.

### Validation

- Narrow factory/source-state tests: pass. Final `go test ./...` with
  `-coverpkg=./... -coverprofile=...`, `go vet ./...` and `go test -race ./...`:
  pass across all packages. Aggregate statement coverage is **87.7%**
  (**8,642 / 9,856** statements); this is not feature-parity coverage.
- `.venv/bin/python python/tools/export_go_contracts.py --check`: **36 files
  current**. All **35** previously tracked source fixtures were byte-compared
  against HEAD and are unchanged. `verify_scans.py`: all **19** scanner guards
  anchored. The full source mutation sweep was not rerun; no Python runtime,
  source scanner target or guard anchor changed. Package invariants are not
  applicable to this exporter-only Python change.
- First `.venv/bin/python -m pytest -q`: **1 failed, 2150 passed, 28 skipped,
  24 subtests passed, 3 warnings**, 214.81 s. The sole failure was the existing
  `test_a_forty_turn_session_stays_fast` 0.5 s threshold: measured 0.782 s.
  Isolated `python/tests/test_double_cost.py`: **11 passed, 1 failed**, with the
  same 40-turn test at 0.824 s. Python runtime and test files are identical to
  HEAD. Host load averages were 16.65/26.04/18.11 during inspection; this
  observation does not prove the performance failure's cause. Threshold and
  unrelated source were not changed. Full-suite rerun: **1 failed, 2150 passed,
  28 skipped, 24 subtests passed, 3 warnings**, 163.68 s, again only this test,
  at 0.584 s. Runtime import paths resolve to the current `python/mini_loop`.
  The full Python performance gate remains **failed**, despite the Go gates
  and source contracts passing. This feature-branch delivery does not claim
  a green repository-wide Python gate or a resolved performance cause.
- Archify architecture validate/deliver: **9/9 showcase**, zero composition
  errors/warnings, correction_rounds: 0. Specification SHA-256:
  `1503f7558859ca062f92646b2f4581821231990cf9a4e4884f5814fd7abbecdf`
  (30,466 bytes); artifact SHA-256:
  `a2712406e4e5471c3ed021dac3acc26b7c4368c4a0ddb115e4f4813f76a0bcb8`
  (670,148 bytes). Exact current specification/artifact bytes match the final
  receipt. Visual review remains skipped because local HTML access was
  previously denied; rendered visual acceptance is not claimed.
- README/plan outline review and `git diff --check`: pass. Host validation uses
  macOS, Go 1.23.3 and native Git 2.39.5 (Apple Git-154); Linux remains
  unvalidated. No production endpoint or paid model was called.

## 2026-10-05 operator background command slice

Reviewed base: `d007ae8`. The coverage status turn verified the uncommitted
service and its earlier 87.9% report. This iteration finishes the first of the
three planned background steps: concrete operator service and shared native
shell primitive. Background runtime composition remains open, as do G0–G7.

### Implementation and actual source boundary

- `go/background` provides named ID, Status, Request, Started, Record,
  Notification, Handle, Batch and Config types over a concrete shell executor.
  No dependency or ambiguous service payload was added. Run checks admission
  context and the existing dangerous-command blocklist; each admitted command
  receives an independent cancellation context and native process group.
  Caller-turn cancellation and foreground Interrupt leave it running.
- Foreground and background execution now share argv/sandbox, scrubbed and
  selected environment, masking, deadline, group cancellation, pipe draining and
  shell reaping. Public BackgroundCommand/BackgroundResult have distinct types:
  background uses one native merged pipe and a raw-byte budget; foreground keeps
  decoded-character/universal-newline capture. Invalid/truncated UTF-8 uses the
  pinned Python maximal-subpart replacement decoder. Overflow ends the group.
  Rendering matches the 50,000-character tail, nonzero exit and timeout recipes;
  the default background deadline is 300s and capture budget is 5,000,000 bytes.
- Status/Check/LiveCount/Wait/Drain, last-100 full-result retention, last-50
  listing and last-50 notification projection match source. Nonzero process exit
  still yields Completed with exit text. Undrained notifications retain their
  independent full text after task-result shedding. Metadata and undrained
  queues are not globally bounded; default-off labels remain explicit.
- CancelAll requests cancellation and returns joinable handles. Close cancels
  and joins current handles, requires caller-owned admission quiescence, and
  permits later Run as source does. Native tests verify group descendants cannot
  leave delayed effects and an interrupted wait can resume joining.
- Best-effort atomic private `.background` files record a masked 200-character
  command, PID and timestamp before/during execution; failure adds the exact
  unrecorded warning. Completion/cancellation removes the file. Construction
  reports each orphan's unknown outcome and PID liveness, reserves its numeric
  identity, delivers a terminal result and removes its record. Adopted PIDs are
  never rerun, controlled or signalled. This evidence supplies no fsync, host
  ACL, ownership lease, PID-reuse proof, session restoration or live-process
  recovery.
- Orphan enumeration treats the workspace as a literal directory, including
  brackets in real root names; joining the root into a glob would misinterpret
  it. Both source and Go adoption probes run under a bracketed root.
- Numeric orphan identities use all 66 Python Unicode 14 decimal digit blocks
  and arbitrary-precision counters. A source fixture pins 74 input recipes,
  including mixed scripts, invalid digits, future Unicode and an 80-digit
  counter. Unicode ledger names stay intact; newly allocated names are ASCII.
  Superscripts/circled digits are still reportable in Go, while source's
  isdigit/int combination may raise ValueError during construction.
- Rebind prepares cwd/sandbox before publishing future executions; admitted
  commands retain their executor and the ledger retains its original root.
  This pins at admission more strongly than Python's coroutine-time workspace
  lookup. Go additionally cleans cancelled-before-start metadata and ledger;
  actual source can leave Running/ledger in that window. Go refuses negative
  retention, bounds orphan reads to one MiB and treats wrong field types as
  unreadable rather than source coercion. Start-observer panic containment kills
  and joins the group; worker/start faults expose only the panic type. Trusted
  Started callbacks must return promptly.
- Snapshot 37 executes twelve actual Python/native shell commands, unrecorded
  execution, five retained completions, started cancellation and the pre-start
  cancellation source gap, five orphan seeds/Unicode counter reservation, 53
  orphans through an actual Agent completion injector, ten slow-operation
  heuristics and the decimal input recipes. Source module hashes are recorded;
  no paid model endpoint was called. Native Go tests compare source outputs and
  add concurrent admission, selective credentials/split-stream masking,
  foreground-interrupt isolation, rebind and close ownership checks.
- README canonical Mermaid adds the operator service and in-flight ledger with
  their direct library entry. The interactive map aggregates these under the
  shared shell and explicitly leaves runtime injection/manager joins pending.

### Remaining work

Next background step is exact model-tool schemas/inputs/identity/null behavior,
common-gate handlers, bounded completion injection and live-count interruption
markers/events. Then compose manager lifecycle close before delete/stop,
execution-workspace rebind, fresh child/fork scopes and explicit activation.
Cron, teams, workflows, owner resources/memory/decisions, MCP, SQLite
session/approval/lease restore, durable SSE, remaining provider variants and the
full G7 differential/release audit remain open. The operator service alone does
not make these runtime paths available.

### Validation

- Narrow background/shell/source-derived tests: pass. Full
  `go test ./... -coverpkg=./... -coverprofile=...`, `go vet ./...` and
  `go test -race ./...`: pass. Final aggregate statement coverage is **87.9%**
  (**8,947 / 10,184** statements); background is **90.2%** (230 / 255), shell
  **93.8%** (363 / 387). These are execution measurements, not feature parity.
- `python/tools/export_go_contracts.py --check`: **37 files current**.
  All **36** previous source-export files were byte-compared against HEAD and
  are unchanged. `verify_scans.py`: **19** scanner guards anchored.
  `verify_guards.py -k background`: all **12** selected source mutations caught,
  including sandbox/environment/masking, retention/listing, worktree rebind,
  descendant timeout, notification cap, source manager delete, orphan adoption
  and interruption markers. This is Python guard verification, not Go mutation
  coverage; the complete unrelated guard sweep was not rerun. Python runtime
  package modules and guard anchors remain unchanged, so package invariants do
  not apply to this exporter-only source change.
- `.venv/bin/python -m pytest -q`: **2151 passed, 28 skipped, 24 subtests
  passed, 3 warnings**, 164.21 s. The previously failing unchanged forty-turn
  0.5s performance test passed in this run. Python runtime/test files and the
  threshold were not changed; this pass does not establish the cause of the
  prior failures.
- Archify final validate/deliver: **9/9 showcase**, zero errors/warnings,
  correction_rounds: 0. Specification SHA-256:
  `27fdc21acdd021b595dbdd727b7c4d60055c40ef2a876af741c4e79e58056bde` (31492 bytes); artifact SHA-256:
  `fa84ec187c2aa6eb780f93535d173237600f5308e3245481dfdfc50ed31f675c` (671240 bytes). Exact current bytes match the final receipt.
  Visual review remains skipped because local HTML access was previously denied;
  rendered visual acceptance is not claimed.
- README/plan outline review and `git diff --check`: pass. Validation uses native
  macOS, Go 1.23.3 and Git 2.39.5 (Apple Git-154); Linux remains unvalidated.

## 2026-10-05 background model-tool and notification slice

Reviewed base: `16d88fc`. This completes the second background step: typed
native-session tools, common-gate Bash dispatch, completion delivery and
interruption evidence. Execution-workspace rebinding was included here because
an enabled runtime must preserve foreground/background confinement together.
G0–G7 remain open; manager, standalone and selected-child composition follow.

### Implementation and actual source boundary

- `RuntimeConfig.BackgroundTools` explicitly adds background_run/check_background
  to a native session. The default catalogue remains ten tools. Activation
  requires a nonnil native shell executor bound to the resolved session root.
  Each session owns lazy concrete state; no shared manager injection can borrow
  another session's credentials, IDs or completion queue. NewWithExecutor uses
  the exact shell already prepared for that session.
- Named inputs preserve absent/null/zero values, detached timeout/prefix/ID
  accessors, canonical action identity and masked recording copies. Timeout is
  optional integer seconds. Go refuses values outside native duration range;
  source's unbounded integer input is not silently narrowed or overflowed.
- Both shell names use the immutable deny list, destructive-command approval
  and validated remembered-prefix candidates through the common execution gate.
  Source tool traits and schemas are preserved. Enabled Bash dispatches explicit
  true and slow-operation heuristics into the same service; only explicit true
  changes scheduling to parallel. Foreground results retain structured status
  and exit metadata; an enqueued task does not fabricate foreground metadata.
- Before the next model request, the queue delivers one user task_notification
  batch and a typed background_result count/dropped event. Projection retains the
  newest 50 completions, leaves omitted IDs queryable and consumes the queue once.
  Existing nonempty ledger evidence triggers lazy adoption without a tool call.
- Operator turn cancellation leaves independent background tasks running. When
  no tool-result repair must remain last, the source interruption text names
  live survivors and points to check_background. CloseBackground is a separate
  cancel/join operation requiring caller-owned admission quiescence.
- Workspace entry publishes a single prepared native shell to foreground and
  background state. Already admitted work and the original ledger stay pinned.
  Default child roles retain native foreground Bash and do not borrow parent
  background state; explicitly selected child background tools remain
  unconfigured until the ownership composition step.
- Snapshot 38 exports actual source schemas/traits, eleven optional input and
  prefix recipes, six enabled/disabled Bash classifications, fifteen real
  common-gate calls, disabled foreground behavior and native live-task
  interruption evidence. Go tests compare these and exercise notifications in
  actual model requests, orphan batch limits, permission/credential boundaries,
  cancellation, prepared workspace rebind and default child isolation. No paid
  model endpoint was called.
- README canonical Mermaid includes gate-to-background and completion-to-session
  flow. The interactive map aggregates background under its shared native shell;
  its semantic card describes notification, cancellation and rebind relationships.

### Remaining work

Compose CloseBackground with manager delete/stop after admission is quiesced,
enable the feature explicitly through manager/standalone settings and give
selected children/forks fresh ownership. Complete the remaining optional groups,
SQLite session/approval/lease restore, durable SSE, provider options and full G7
differential/release audit. The current library flag does not activate these
unimplemented paths.

### Validation

- Narrow source-derived protocol/runtime tests: pass. Full
  `go test ./... -coverpkg=./... -coverprofile=...`, `go vet ./...` and
  `go test -race ./...`: pass. Aggregate statement coverage is **87.9%**
  (**9,135 / 10,392**), deduplicating shared profile blocks across packages.
  Agent is **89.0%** (3,893 / 4,373), background **90.3%** (242 / 268),
  protocol **88.5%** (910 / 1,028), shell **93.8%** (364 / 388).
  These are execution measurements, not feature parity.
- Source export `--check`: **38 files current**. All **37** previous tracked
  exports were byte-compared against HEAD and remain unchanged.
  `verify_scans.py`: **19** scanner guards anchored.
  `verify_guards.py -k background`: all **12** selected source mutations caught.
  This verifies Python source guards, not Go mutation coverage; unrelated guard
  groups were not rerun. Python runtime package modules and mutation anchors
  are unchanged; package invariants do not apply to this exporter-only change.
- `.venv/bin/python -m pytest -q`: **2151 passed, 28 skipped, 24 subtests
  passed, 3 warnings**, 135.12 s. Runtime/test files and the forty-turn
  performance threshold were unchanged; this run passed that existing gate.
- Archify final validate/deliver: **9/9 showcase**, zero errors/warnings;
  correction_rounds: 2. Specification SHA-256:
  `f0f7e4ba345fc78a32e84636b66d9cf61a06f027a532bca4c9f4461dfaa528da`
  (31,705 bytes); generated artifact SHA-256:
  `a3e2b4fc63fd2141b36a896c4a0103a51ab6684816f3bc7a55b7c02c693d90ec`
  (671,453 bytes). Exact bytes match the delivery receipt. Visual review remains
  skipped because local HTML access was previously denied; rendered visual
  acceptance is not claimed.
- README/plan outline review and `git diff --check`: pass. Native macOS,
  Go 1.23.3 and Git 2.39.5 (Apple Git-154); Linux remains unvalidated.


## 2026-10-05 managed background lifecycle and startup slice

Reviewed base: `0812a21`. Manager ownership and standalone selection now reach
real native background tasks. This continues the third background step; selected
subagent ownership still requires source investigation and implementation. G0–G7
remain open.

### Implementation and actual source boundary

- `ManagerServices.BackgroundTools` passes explicit activation into every fresh
  managed runtime. Existing default services and ten-tool catalogues are unchanged.
  Host/native binding is still required; a custom string-only shell cannot opt
  into the native background service. No dependency or ambiguous payload is added.
- Delete/Stop revoke admission before draining the active turn. The manager then
  closes and joins initialized background ownership before workspace or recording
  reclamation. Ownership is inspected after the drain, so a turn may create its
  lazy service during the grace window without escaping cleanup. Close errors go
  through existing bounded, masked cleanup diagnostics.
  Source delete captures its background object before awaiting the turn; Go's
  post-drain ownership lookup is an additional guard, not a claim that source's
  late-creation behavior was exercised by the five exported scenarios.
- Idle initialized background owners use the tracked asynchronous cleanup path,
  as actual source does. Shutdown also awaits retiring sessions; an expired Stop
  observer can resume waiting on the same authoritative shutdown. Unused enabled
  sessions are stopped without constructing a service or adopting ledger records.
  Bound/preserved roots stay intact, and Stop retains scratch as source does.
- Completed-boundary forks use ordinary managed construction and fresh roots,
  executors and lazy background state. They copy paired history without borrowing
  parent IDs/tasks/queues. Tests initialize the fork service and verify it cannot
  see the parent's still-running native task.
- `launcher.Options.BackgroundTools`, NewWithOptions and InspectWithOptions expose
  individual Go service selection while existing New/Inspect default to false.
  The executable accepts `--background-tools`; `--dump-config` reports selection
  without starting a listener or workspace. This is a Go-specific individual
  option, not Python's comprehensive MINILOOP_FEATURES contract: that complete
  bundle remains refused until all its services exist. Settings snapshots and
  Python environment codec exports are unchanged.
- Snapshot 39 uses actual source SessionManager, optional tool registry/injectors,
  fake provider and native processes. Five scenarios cover scratch delete,
  scratch preserve, bound delete, stop and completed-boundary fork, including
  cancelled status, ledger removal, catalogue and workspace retention. Go tests
  compare these outputs and add a still-draining turn/retiring shutdown, lazy
  service creation after Delete has closed admission and native-only activation.
- A real loopback HTTP launcher with a local scripted model endpoint publishes
  twelve schemas, executes background_run via the model/tool gate and records a
  live native PID. Serve cancellation joins manager shutdown, reaps that PID,
  removes its in-flight ledger and retains scratch. No paid model endpoint is
  called. CLI inspection preserves comprehensive-feature refusal and zero effects.
- Canonical README Mermaid adds manager drain/close and explicit startup selection
  relationships. The interactive map aggregates them under its existing shell,
  startup and managed components and explains the ownership sequence in its card.

### Remaining work

Selected subagents still have native foreground Bash and no configured background
service. Actual Python builds fresh child state in the same workspace, inherits
injectors and returns child.run without an explicit child-service close. Investigate
shared-root ledger/ID collisions and reachable ownership before completing that
path; do not describe a borrowed parent queue or a detached unreachable service as
fresh ownership. Then continue remaining optional groups, SQLite
session/approval/lease restore, durable SSE, provider variants and the G7 audit.

### Validation

- Narrow managed/background/launcher/CLI tests: pass. Full
  `go test ./... -coverpkg=./... -coverprofile=...`, `go vet ./...` and
  `go test -race ./...`: pass. Aggregate statement coverage is **87.9%**
  (**9,142 / 10,403**), deduplicating shared profile blocks. Agent is **89.1%**
  (3,902 / 4,381), background **90.3%** (242 / 268), launcher **77.0%**
  (114 / 148), cmd/miniloop **54.4%** (31 / 57). This is execution coverage,
  not feature parity or release completion.
- Export `--check`: **39 files current**; all **38** previous tracked exports
  were byte-compared against HEAD and remain unchanged. `verify_scans.py`:
  **19** anchored scanner guards. Python package runtime and guard anchors are
  unchanged; package invariants do not apply to this exporter-only change.
- `verify_guards.py -k background`: all **12** selected source mutations caught.
  Unrelated guard groups were not rerun; source mutation checks do not constitute
  Go mutation coverage.
- `.venv/bin/python -m pytest -q`: **2151 passed, 28 skipped, 24 subtests
  passed, 3 warnings**, 81.42 s. Python runtime/test files and existing
  performance thresholds are unchanged.
- Archify validate/deliver: **9/9 showcase**, zero errors/warnings,
  correction_rounds: 0. Specification SHA-256:
  `2b11bd8cdf973f95430188ea2fad0b769a5cd840d85fc759a59a7df82a927863`
  (31,913 bytes); generated artifact SHA-256:
  `a3fb0fbc0f1a0e047e042944b63e9f7940a900e44df68980c410d879a77cdf9c`
  (671,657 bytes). Exact bytes match the receipt. Visual review remains skipped
  because local HTML access was previously denied; rendered acceptance is not
  claimed. Generated HTML was not hand-edited.
- README/plan outline and `git diff --check`: pass. Native macOS, Go 1.23.3,
  Git 2.39.5 (Apple Git-154); Linux remains unvalidated.


## 2026-10-05 selected child background ownership slice

Reviewed base: `cf6995b`. Selected child background tools now execute in fresh
native state and remain reachable by parent lifetime cleanup after child return.
This completes the planned child ownership part of the background composition;
G0–G7 remain open and the comprehensive feature bundle remains unavailable.

### Implementation and actual source boundary

- A selected built-in background_run/check_background definition binds a fresh
  concrete child backgroundState. Check-only catalogues work without exposing
  foreground Bash; custom handlers retain their own contract. Default capability
  roles continue to omit these tools and retain native foreground Bash. Explore
  readonly permission denies execution before constructing its child service.
- Each child obtains a qualified `bg_<scope>_<counter>` identity. Scope is the
  full SHA-256 of its freshly derived peer message identity; a named background
  Scope accepts only 64 lowercase hexadecimal characters. Root IDs/counters stay
  source-compatible. This is an intentional child-format difference that keeps
  simultaneous parent, child and sibling records from overwriting one another.
- The parent initializes its root evidence before admitting selected children.
  Scoped constructors never adopt the shared root's records, so a child cannot
  report its live parent's task as an orphan. Queues, checks, counters and model
  notifications remain local to each scope. Child native execution retains the
  exact admitted executor's credentials, sandbox/capture and workspace binding.
- Admitted child tasks keep running after summary return until completion,
  deadline or owner cleanup. Parents retain the private child scope tree rather
  than borrowing its manager/queue. CloseBackground requests cancellation for
  every initialized scope before awaiting any handle; an expired observer can
  resume joining. Manager delete/stop use this recursive close after turn drain
  and before scratch reclamation, even when the parent has no tasks of its own.
- Qualified records use the admitted workspace's existing .background ledger.
  A fresh root constructor reports them through ordinary orphan adoption, removes
  adopted evidence and leaves the root numeric counter unchanged. It never
  controls/replays the PID. Native tests seed exact captured child ledger bytes
  after a graceful close to model retained crash evidence; this is a disk-record
  adoption test, not an actual killed-host/restart or fsync proof.
- Snapshot 40 runs six actual Python children using harness-bound injectors:
  selected direct/Bash backgrounding, Explore denial, check-only selection,
  live-parent selected child and live-parent default child. Child IDs alone are
  normalized to <child-id> for comparisons. Real native markers confirm source
  parent/child task execution; source module hashes pin the evidence.
- **Measured source gap:** both live-parent cases adopt bg_0001 as an orphan and
  unlink its ledger while the parent remains running. Even the default child,
  whose catalogue has no background tools, inherits that injector via Harness.
  Selected child work remains live after return with no source child close.
  Go intentionally keeps parent evidence intact and a reachable lifetime owner;
  it does not reproduce false orphan adoption or share parent completion queues.
- Go tests compare schema visibility/output/status/readonly behavior and add
  scoped ID/ledger/queue isolation, completion injection into child requests,
  returned child cleanup with an idle parent, nested scope ownership and resumed
  close. Source background output text remains the same apart from qualified
  child IDs. No dependency or paid model endpoint was added.
- README canonical Mermaid adds selected-child-to-background flow. The interactive
  map aggregates the service under existing components and describes scope/lifetime
  relationships in its semantic card; generated HTML is never hand-edited.

### Remaining boundaries

Checks, notifications and interruption live counts remain local to the executing
scope; the parent model does not automatically consume a returned child's queue.
Retained child state and undrained metadata/queues are not globally bounded. This
is process-local lifetime ownership, not host ACLs, a lease, replay or arbitration
between separately bound root sessions/processes sharing one ledger. The Go
scope-format/ownership additions and measured source gaps remain explicit for G7.
Cron, teams, workflows, remaining optional context/provider/UI groups, SQLite
session/approval/lease restore, durable SSE and the full differential/release audit
still require work. Do not mark the overall Go migration complete.

### Validation

- Source-derived child/native scope tests: pass. Full
  `go test ./... -coverpkg=./... -coverprofile=...`, `go vet ./...` and
  `go test -race ./...`: pass. Aggregate statement coverage is **87.95%**
  (**9,182 / 10,440**), deduplicating shared blocks. Agent **89.1%**
  (3,928 / 4,410); background **90.9%** (251 / 276). Coverage is execution
  evidence, not feature parity. Initial test-only mistakes used an unsupported
  task role and a completion-wait helper for deliberately live commands;
  corrected tests and the final full profile are the acceptance evidence.
- Export `--check`: **40 files current**. All **39** prior tracked exports
  were byte-compared against HEAD and remain unchanged. Initial source probe
  passed injectors outside Harness and failed to capture children; the corrected
  probe binds them through Harness and all six cases finish with explicit native
  cleanup. Python runtime/package modules and mutation anchors are unchanged;
  package invariants do not apply to this exporter-only change.
- `verify_scans.py`: **19** scanner guards anchored.
  `verify_guards.py -k background`: all **12** selected mutations caught;
  `verify_guards.py -k subagent`: all **6** selected mutations caught.
  No Go mutation coverage or full unrelated sweep is claimed.
- Python full suite: **2,151 passed / 28 skipped / 24 subtests passed**, three
  warnings, **125.13 seconds**. The first full run had 2,150 passes and one
  failure: the unchanged forty-turn timing guard measured 0.72 seconds against
  its 0.5-second limit. A focused repeat measured 0.75 seconds. Profiling showed
  request projection/protocol checks and event processing as the major costs,
  not the old character-by-character fake-token count. Other high-load host
  processes were observed, but that observation alone does not prove causation.
  Python runtime/tests were byte-compared with the committed baseline; no timing
  threshold, runtime or test behavior was changed. The final full rerun passed
  the original gate; the earlier failures remain recorded as timing variability.
  A separately extracted `cf6995b` Python tree reproduced the same guard failure
  at 0.59 seconds (11 other cost tests passed), confirming it exists without this
  slice. This control is diagnostic evidence, not a second passing gate.
- Archify final validate/deliver: **9/9 showcase**, zero errors/warnings,
  correction_rounds: 0. Specification SHA-256:
  `3aae85573dd95f157317fb82a8ee4e6770139dd1019c87a48caad1d0f1d154f2`
  (32,260 bytes); generated artifact SHA-256:
  `998a87d8098f039c7641b7632a674e170914731a0d444c46c60338b5e0fbcf5e`
  (672,004 bytes). Exact bytes match the receipt. Visual review remains skipped
  because local HTML access was previously denied; rendered acceptance is not
  claimed.
- README/plan outline and `git diff --check`: pass. Native macOS, Go 1.23.3,
  Git 2.39.5 (Apple Git-154); Linux remains unvalidated.

## 2026-10-05 operator cron scheduler slice

Reviewed base: `aaa499b`. The explicit Go operator scheduler implements the first
cron work package above. Python and Go remain independently runnable; cron
manager/tool/HTTP composition and the remaining G0–G7 requirements stay open.

### Implementation and source contract

- `go/cron` uses concrete Job/ID/SessionID/Request/Expression/Invocation records
  and named Resolver/Runner/Masker seams. Raw JSON is confined to transient store
  decoding; no generic payload enters service state. Invocation's read-only
  authority accessor always returns untrusted. The future managed adapter must
  create a fresh untrusted RunContext; this library is not yet an actual Go
  Agent firing path. Source `_fire` is exercised with its real default context.
- Five-field matching preserves ranges/lists/steps, Sunday 0 and restricted
  day-of-month/day-of-week OR. Arbitrary precision steps cannot overflow into
  small values. Parsing includes signs, digit separators and Python's default
  4,300-digit conversion bound. The pinned Unicode 14 decimal table moved from
  background into internal/pytext; both callers use it, and the existing 74
  background source cases remain unchanged. Python split whitespace includes
  ASCII separators absent from Go's ordinary Fields.
- Schedule refuses more than 8,000 Unicode characters or 200 live jobs. Both
  optional booleans default true for new schedules; stored dataclass defaults are
  recurring true/durable false. Random IDs regenerate on a collision, a Go
  addition. Arm/cancel can scope by session and hide foreign IDs like unknown
  ones. CancelForSession removes future jobs/last claims, without stopping an
  already admitted run. No model arm tool is installed.
- Atomic save stores only durable jobs and masks prompts in the disk copy,
  retaining live raw input. Diagnostics describe changed post-restart prompts.
  Restoration preserves valid records/ordering, replaces duplicate IDs in place
  and disarms all jobs; activation is never written. Source non-array JSON yields
  an empty store. Go additionally checks concrete scalar types and caps reads and
  writes at eight MiB; Python's permissive dataclass may retain malformed scalar
  or null fields. Unknown fields/bad cron rows are individually diagnosed.
- Each occurrence first wins an O_EXCL minute claim, then advances its mark and
  removes one-shot jobs, saves via same-directory private temporary file/fsync/
  rename/directory sync, and only then resolves/dispatches. Claim/save failure
  reports a lost occurrence with no dispatch. Memory and the current claim stay
  consumed on save failure as in source; successful saves prune only the previous
  claim. A loser quietly advances its local mark but does not remove its one-shot.
- Explicit Start owns an immediate tick plus a 20-second ticker. Go has no
  implicit running asyncio loop, so Schedule does not start it automatically.
  Tick accepts the local civil minute; it adds no catch-up/timezone conversion.
  Stop cancels/joins ticker and admitted runs and permits resumed joins after an
  expired observer. A stopped generation cannot dispatch a delayed resolution.
  Later Start remains legal. Caller owns final admission quiescence.
- Resolver callbacks run outside the state lock and may inspect the scheduler;
  Mask runs during persistence and must not reenter. Go reports resolver/save
  callback panics and asynchronous runner errors/panics, beyond source's pending
  task-error handling. Problems retain 50 distinct entries and detached counts
  for all occurrences/evictions; runs and armed metadata are not globally bounded.
- Source snapshot 41 executes 27 expression cases over six civil times, 14 actual
  operator states, one-shot removal-before-dispatch, two stale live claimers,
  three claim/save-loss boundaries, masked restoration, 8,001-character/201st-job
  refusal and actual `_fire` untrusted invocation/missing-session diagnostics.
  Only generated schedule IDs are normalized in Go comparisons. Source probes
  patch UUID generation for deterministic IDs, not scheduler behavior, and pin
  cron/durable/problems/run-context module SHA-256 values.
- Go comparisons inspect actual disk state at resolution and live/state/queue
  projections, plus two independent native test processes loading stale copies
  before a common gate. Exactly one dispatches that minute. Additional tests
  cover ticker idempotency, cancelled runs, resumed joins, delayed resolver
  suppression, partial typed loads and fault reporting. These are bounded local
  process/filesystem experiments, not a host-crash or external-effect proof.
- README canonical Mermaid adds a separate operator cron/file/embedding path.
  Interactive Go overview adds an independent cron component, deliberately without
  a manager edge until composition exists. Existing background/workspace binding
  documentation was corrected to describe the implemented native rebind.

### Remaining boundaries

Whole-file JSON state is stale between live writers: the claim prevents duplicate
admission for a shared minute, but a later writer can overwrite other jobs/state.
Claims do not transact with arbitrary external effects, prove exactly-once work,
provide a host lease, reclaim stale claims or replace SQLite/session restoration.
The Go operator resolver/runner is still an embedding seam. Managed untrusted
turns, delete/stop composition, closed model tool variants, owned operator HTTP
activation and individual launcher selection are next. Comprehensive feature
activation, teams/workflows, optional context/provider/UI groups, approval/session/
lease restore, durable SSE and the full differential/release audit remain open.

### Validation

- Narrow source/cron/background tests: pass. Full `go test ./... -coverpkg=./...`
  with a coverage profile, `go test -race ./...` and `go vet ./...`: pass.
  Aggregate statement coverage **88.12%** (**9,615 / 10,911**), deduplicating
  shared blocks. Cron **91.91%** (432 / 470), background **90.49%** (238 / 263),
  internal/pytext **88.46%** (92 / 104). Moving shared decimal code changes package
  denominators; these are statement metrics, not feature parity.
- Export `--check`: **41 files current**; all **40** prior tracked exports are
  byte-identical to HEAD. The check emitted an asyncio child-watcher warning
  (`Unknown child process pid ...`); its terminal status was zero and the complete
  byte comparison matched. This is retained diagnostic context, not a claim that
  the warning was repaired. Python runtime/test modules and mutation anchors are
  unchanged; package invariants do not apply to this exporter-only Python change.
- `verify_scans.py`: **19** guards anchored. `verify_guards.py -k cron`: all
  **14** selected source mutations caught. No Go mutation coverage or unrelated
  full mutation sweep is claimed.
- Python full suite: **2,151 passed / 28 skipped / 24 subtests passed**,
  three warnings, **82.06 seconds**, with the original performance gate intact.
- Final Archify validate/deliver: **9/9 showcase**, zero errors/warnings,
  correction_rounds: 0. Final specification SHA-256:
  `f9a03479bb1cc8501a8aeab059a5ac7768f5b84860397bdfdf818552b21996d3`
  (32,893 bytes); artifact SHA-256:
  `e73db9e1fc5278d94aaf5cdffe76b2466036f3c86fad3606315ea93c288819bf`
  (673,766 bytes). Exact bytes match the final receipt. An earlier serialization
  changed JSON formatting unnecessarily; the final candidate preserves the prior
  indent style and was revalidated/redelivered. Visual review stays skipped due
  to the earlier local HTML access denial; rendered acceptance is not claimed.
- README/plan outlines and `git diff --check`: pass. Native macOS / Go 1.23.3;
  Linux remains unvalidated. No dependency or paid model endpoint was added.

## 2026-10-06 managed cron lifecycle slice

Reviewed base: `d62aa98`. This advances the managed portion of cron work package
2. G0–G7 remain open; Python and Go remain independently runnable. The previous
coverage status turn refreshed the actual worktree profile rather than changing
implementation. This iteration delivers the already implemented managed path,
its source contract and runtime/architecture evidence.

### Implementation and source contract

- SessionManager creates one typed cron scheduler after workspace/config
  validation, using `<WorkspaceRoot>/.cron.json` and the configured secret masker.
  This default ownership matches actual Python with features disabled; construction
  loads without starting a ticker, and the ten default model tools remain unchanged.
- Concrete ScheduleCronRequest and CronJobView records bind owner-scoped schedule,
  list, cancel and arm operations. Caller input cannot supply an authoritative
  session. Foreign sessions use ErrSessionNotFound, foreign jobs read like missing
  jobs, and list results are detached. CronScheduler remains privileged explicit
  operator access without owner checks; raw scheduler Start stays explicit.
- Managed resolution selects a live session only while the manager is active.
  RunScheduled verifies the typed invocation binding and invokes ManagedSession.Run
  with its fresh default untrusted RunContext. A previous human actor, grant or
  message identity is not retained. The normal serialized queue, tool gate, shared
  model/tool pools and per-run trajectory capture remain in force. No session/lease
  restoration adapter is supplied yet.
- Manager.Start starts existing jobs without arming restored records; standalone
  Serve calls it after actual listener admission. An admitted owner schedule starts
  its ticker even if persistence fails after in-memory admission, matching source's
  retained job/start behavior. Invalid requests do not start it. Arm is an operator
  act; no model arm tool is introduced.
- Owner cron mutations, deletion and Stop share an admission lock. Delete revokes
  the session and removes future jobs before draining/reclaiming scratch. A failed
  cron save is retained in CleanupErrors while cleanup continues; Python currently
  propagates that save exception after removing its session. This is a documented
  Go addition. Mask callbacks must not reenter scheduler or manager cron methods.
- Manager.Stop revokes all turn admission, joins managed drains, then cancels/joins
  the cron ticker and admitted runs before terminal state. A cron request queued
  behind another turn cannot reenter after Stop. Expired Stop observers can resume
  the existing join. PreserveWorkspace, bound workspaces and normal Stop retain
  files; forks share the service without copying parent jobs.
- Snapshot 42 exercises actual Python manager/session/cron/run-context code: a
  human then scheduled turn with fresh untrusted authority, five delete/preserve/
  bound/fork/stop ownership states, and a live scheduled turn cancelled by Stop.
  Only random message/job identities are normalized. The fixed civil minute and
  source module SHA-256 values are pinned; scheduling the authority case outside
  an event loop avoids ambient wall-clock ticking. No paid provider is contacted.
- Go tests compare those source projections and additionally exercise foreign
  owner refusal before mutation, detached lists, closed admission after delete/
  Stop, a queued cron turn, save-failure cleanup, and actual foreground shell PID
  reaping on manager Stop. Real TCP launcher startup loads durable disarmed jobs,
  starts their ticker, consumes no occurrence and joins shutdown while retaining
  the store. Native process evidence is macOS only.
- README canonical Mermaid now connects manager ownership, launcher startup and
  fresh untrusted managed dispatch. Its explanation, interactive specification,
  Go README, extension seam and parity matrix are updated together. The interactive
  overview adds the manager ownership edge and aggregates file/dispatch details
  in its cron component and semantic card. Generated HTML is produced by Archify.

### Remaining boundaries

Closed schedule/list/cancel model tool variants, owner-scoped cron HTTP operations
and operator activation are next in work package 2. Comprehensive feature-bundle
activation remains unsupported. SQLite sessions/approvals/leases, restoration
interaction, teams/workflows, remaining context/provider/UI groups, durable SSE
and G7 remain open. Occurrence claims still do not arbitrate stale whole-file
writers, transact with external effects or prove exactly-once work. The raw
privileged operator surface does not confer HTTP ownership checks.

### Validation

- Narrow agent cron tests and launcher restored-startup test: pass. Full
  `go test ./... -coverpkg=./... -coverprofile=...`: pass. Shared coverage blocks
  are deduplicated across test binaries: **88.13%** (**9,686 / 10,990**) statements;
  agent **89.10%** (3,998 / 4,487), cron **92.77%** (436 / 470), launcher **76.67%**
  (115 / 150). These are execution metrics, not functional parity percentages.
- `go test -race ./...` and `go vet ./...`: pass. No dependency was added.
- Source exporter `--check`: **42 files current**; all **41** previously tracked
  exports, including the SQLite SQL file, are byte-identical to HEAD. This check
  emits existing model-deprecation warnings and exits zero. Python runtime/test
  modules and mutation anchors are unchanged; package invariants do not apply
  to the exporter-only change.
- `verify_scans.py`: all **19** scanning guards anchored.
  `verify_guards.py -k cron`: all **14** selected source mutations caught. No
  Go mutation coverage or unrelated full mutation sweep is claimed.
- Python full `.venv/bin/python -m pytest -q`: **2,151 passed / 28 skipped /
  24 subtests passed**, three warnings, **80.21 seconds**. The original performance
  gate is unchanged.
- Final Archify validate/deliver: **9/9 showcase**, zero errors/warnings. Three
  focused layout repairs moved the cron component near its manager and removed
  diagnosed route crossings/node intersections. The final candidate is frozen:
  specification SHA-256 `4a76fc47208003ae517e67ad2eab7eb75c9375eb4a05c401f098f2d88d4572c8`
  (**33,307 bytes**), artifact SHA-256
  `ec27f3e0625f4816f4011034a98055c6cfe4802b8a1d8615122d7f92932f44ab`
  (**674,351 bytes**). Visual review remains skipped due to the earlier local HTML
  access denial; no rendered visual acceptance is claimed.
- README/plan/source outlines and `git diff --check`: pass. Native macOS /
  Go 1.23.3; Linux remains unvalidated.

## 2026-10-06 cron tool and owned HTTP slice

Reviewed base: `20a4b79`. The previous goal iteration delivered managed cron and
verified its pushed state. This iteration completes the model-tool, operator HTTP
and individual launcher portions of work package 2. G0–G7 remain open; restoration/
lease interaction and final differential/release audit remain work package 3.

### Implementation and source contract

- Three closed protocol variants represent schedule_cron, list_crons and
  cancel_cron. Required fields are typed strings; optional booleans retain absent
  versus false and detach in constructors/accessors/cloning. Canonical replay/hash
  inputs and recording masks cover all new strings. Session/owner input is refused;
  no arm_cron model variant exists. DefaultToolNames remains the ten source tools.
- A consumer-owned CronControl names schedule/list/cancel methods with established
  OwnerID/SessionID bindings. RuntimeConfig.CronTools installs source schemas and
  write/read-readonly/write traits, all exclusive and with no default child
  capabilities. RuntimeConfig.Cron supplies the service; bare/unconfigured sessions
  return source notices. ManagerServices.CronTools binds the manager itself.
- Effects retain the existing before-rewrite, monotonic-guard, permission,
  journal/settlement, masking and observer path. Semantic schedule refusals retain
  source text. Go tests verify before-hook rewrites, live raw prompt/masked result
  and disk copies, same-action replay without duplicate schedules and refusal of
  a foreign runtime authority. These are process-local action guarantees.
- A real model turn schedules through the same gate; forks retain tool activation
  with fresh job scope. Explicitly selected child tools bind fresh handlers without
  CronControl and report source unavailability, even when a role policy selects
  every tool. Default roles omit the empty-capability optional tools.
- Four operator method/path operations are registered independently of model tools:
  GET/POST /sessions/{session_id}/cron, DELETE /sessions/{session_id}/cron/{job_id},
  POST /sessions/{session_id}/cron/{job_id}/arm. They admit the established principal
  and session owner, then call owner-scoped manager methods. Foreign jobs read like
  missing ones; Arm is an explicit operator authorization edge, never a model tool.
- HTTP schedule validates required nonempty strings and the 100-character cron
  limit before owner lookup, matching actual FastAPI ordering. Concrete boundary
  decoding preserves source boolean strings and numeric 0/1, rejects null/other
  values and ignores extra HTTP fields like Pydantic. Raw field bytes exist only
  during decoding; no generic payload enters request/service state. Structured
  list/result/arm replies pass through the common masked JSON projection.
- Invalid expressions/size/count refusals are 400; hidden session/job lookup is
  404; stopped admission is 503 and persistence errors return a scrubbed 500.
  Full Pydantic 422 detail remains an existing open HTTP boundary: tests compare
  refusal status for validation cases and complete typed bodies for other cases.
- The model decoder enforces the advertised boolean schema and rejects explicit
  null/nonboolean values. Python's kwargs handler is more permissive for these
  schema-invalid model values. This difference is documented, not claimed as full
  malformed-input conformance; G7 still requires invalid-input differential audit.
- --cron-tools / launcher.Options.CronTools selects just the implemented cron
  tools; dump-config reports the captured flag without constructing files/listeners.
  MINILOOP_FEATURES remains unavailable until every group is implemented. Cron
  service ownership/startup/stop remain independent of model-tool activation.
- Snapshot 43 executes actual Python code for three tool contexts (unconfigured,
  enabled, readonly; **24 gate calls**), **six** canonical/grant variants, three
  schemas/traits and **29** authenticated HTTP requests. Generated session/job IDs
  are normalized, not outcomes. Future schedules cannot tick during export. Source
  SHA-256 values pin cron, registry, permission and HTTP code.
- Go HTTP tests additionally load an actual durable disarmed job, create an owned
  local session with six controlled random identity bytes, refuse a foreign arm,
  authorize its owner and prove no activation is stored—even after another save
  and reload. This is a test identity seam, not durable session restoration.
- A local TCP upstream returns actual typed model tool use to the standalone HTTP
  launcher. With CronTools enabled it sends thirteen schemas, schedules through
  the normal session gate, exposes the job through owned HTTP, cancels it and joins
  shutdown. No paid model endpoint or dependency is added.
- README canonical Mermaid, explanation, interactive specification, extension seam,
  Go README and parity inventory are updated together. The HTTP inventory now has
  **27** method/path operations. The interactive common resource node aggregates
  the cron handlers; its manager-owned cron component explains operator activation.
  Source browser assets remain unchanged and the Cron pane now has its data APIs;
  a new rendered browser interaction pass is not claimed.

### Remaining boundaries

SQLite session/approval/lease restoration, cron restoration/lease interaction,
teams/workflows and remaining context/provider/UI groups remain open. Full 422
validation detail and malformed model-input conformance need G7 evidence. Source
whole-file cron writes remain stale across live writers; minute claims do not
transact with arbitrary external effects or prove exactly-once execution. Default
feature-bundle activation is still unavailable. A valid restored job can only be
armed through an owned live session or explicit privileged library operator until
session restoration is implemented.

### Validation

- Narrow protocol/agent/HTTP/CLI comparisons and local TCP launcher test: pass.
  Final full `go test ./... -coverpkg=./... -coverprofile=...`: pass. Aggregate,
  deduplicated statement coverage **88.17%** (**9,829 / 11,148**). The earlier
  profile reported 9,833 covered statements (88.20%); the final profile is the
  recorded gate. Coverage is execution evidence, not feature-parity percentage.
- Final `go test -race ./...` and `go vet ./...`: pass. Vet initially found an
  unkeyed external ScheduleCronRequest literal; it was corrected to named fields,
  and full coverage/race gates were rerun against that final source.
- Exporter --check: **43 files current**; all **42** previous tracked exports are
  byte-identical to HEAD. Existing model-deprecation warnings remain; terminal
  status is zero. Python runtime/test modules and mutation anchors are unchanged;
  package invariants do not apply to the exporter-only change.
- verify_scans.py: **19** scanning guards anchored; verify_guards.py -k cron:
  **14** selected source mutations caught. No Go mutation or unrelated full source
  mutation sweep is claimed.
- Python full `.venv/bin/python -m pytest -q`: **2,151 passed / 28 skipped /
  24 subtests passed**, three warnings, **86.14 seconds**. The original performance
  guard is unchanged.
- Final Archify validate/deliver: **9/9 showcase**, zero errors/warnings, no layout
  repairs. Specification SHA-256
  `dea94e6f234b47c72d3efe7b265a539d8274217ad2102302545b9ed2c412c301`
  (**33,527 bytes**); artifact SHA-256
  `2748c6112c2431f5bffedb53d5cc73d523fa177c686ee9ba4347ef4a938e5097`
  (**674,569 bytes**). Exact bytes match the frozen delivery receipt. Visual review
  stays skipped because of the earlier local HTML access denial; no rendered
  visual acceptance is claimed.
- README/plan/source outlines and git diff --check: pass. Native macOS / Go 1.23.3;
  Linux remains unvalidated.


## Implementation checkpoint — 2026-10-06 state contracts and archival decoding

Reviewed base: `81614ba`. The preceding coverage/status turn did not change
implementation state. This iteration advances G0/G5 with concrete persistence
consumer contracts, a complete decoder for current Go event writer variants,
and an actual Python SQLite probe. It does not mark G5 complete.

### Source evidence and authority boundary

- Python `SessionRecord` carries schema-v7 owner, workspace binding, pending
  steering, todos and mutable run/status fields. The cursor is derived from
  event rows; upsert preserves original creation time and independently held
  lease columns. Model/mode/lineage/human authority are not session-row fields.
- SQLite append allocates ordinals in `BEGIN IMMEDIATE`; message ordinals are
  global per session across epochs, while an empty append returns the selected
  epoch's count. A trigger-induced second-row failure rolls back the first row.
- Lease acquire is one conditional UPDATE, including the same-owner case;
  foreign takeover uses strict expiry, and renewal accepts exact equality.
  Renewal never reclaims an expired/stolen lease. These are session admission
  controls, not a fencing token for an external tool effect or a heartbeat.
- Opening `SQLiteStateStore` or constructing `DurableActionJournal` does not
  mark actions. Python manager composition explicitly calls the journal's
  `mark_inflight_unknown`; the backend operation supports scoped/all selection
  and changes started to unknown. A later process opening the same database is
  not itself proof that the earlier holder is dead. Delete removes operational
  session/message/event rows and the lease, retaining action/approval audit rows.
- These observations correct stale statements in EXTENDING: store-open unknown
  transitions, missing reconciliation and missing leases were not current facts.

### Delivered contracts and decoder

- `agent.SessionRecord` uses existing concrete IDs/statuses/todos, with detached
  copies of nullable system and slice data. Separate `SessionStore`,
  `TranscriptStore`, `EventStore`, `LeaseStore` and `ApprovalReader` interfaces
  remain consumer-owned; tenant `OwnerID` and process `LeaseOwner` are distinct.
- `DecodeStoredEvent` and `SessionEventRecord.UnmarshalJSON` decode flat current
  writer projections into closed variants. Ordinary JSON cannot silently leave
  the private event discriminator empty, and a failed decode cannot replace an
  existing record. Known tool inputs enter through the closed protocol decoder.
- Each archival row is bounded to 16 MiB; positive sequence/epoch, session and
  nonnegative depth are required. Unknown event/compaction variants fail. Extra
  unused fields are ignored. Tool input decoding retains existing smaller caps;
  system text supports strings or the current single cached text-block shape.
  The rendered error survives, but its original cause category is not in the
  flat projection. Current writer canonicalization determines ephemeral flags
  and approval-refusal text; arbitrary historical extension fields are not kept.
- Historical message lineage is informational. The decoded scope has untrusted
  authority, no actor and no approved capabilities. Approval grant rows do not
  mutate a broker. No manager, launcher or SSE path binds a StateStore yet.
- Snapshot 44 executes real Python SQLite for sessions/upsert, provider-object
  serialization, epochs/empty appends, rollback, bounded event reads, 13 lease
  steps, scoped/global unknown transitions, immutable audit identity, deletion,
  two-connection concurrent append, v1 migration and v8/corrupt-file refusal.
  Go tests establish projection compatibility, all current writer variants,
  real tool-loop round trips, malformed rows and authority-bearing extensions.
  They do not establish Go SQL execution or session restoration.

### Driver decision requiring user input

Repository AGENTS requires explicit approval before adding dependencies. No
module dependency or toolchain change has been made. A pending user question
offers a version-pinned choice; independent contracts/probes are delivered first.

- Recommendation: [mattn/go-sqlite3 v1.14.52](https://github.com/mattn/go-sqlite3/tree/v1.14.52),
  source revision `b0be46fa28d17ee0b65c79774ac0dad84b6db068`. Its go.mod declares
  Go 1.21 and the bundled header declares SQLite 3.53.4. The driver uses CGO and
  needs a C compiler for the build. Validate with this checkout's Go 1.23.3 after
  approval; no driver build or runtime claim is made from module metadata.
- Alternative: [modernc/sqlite v1.60.1 go.mod](https://gitlab.com/cznic/sqlite/-/blob/v1.60.1/go.mod),
  revision `b122d0417c01508beb55158faedeb64a0c5bfd8a`. This avoids CGO but declares
  Go 1.26.0 and brings a wider module graph; selecting it also requires the
  explicitly offered toolchain upgrade. Module/version metadata were refreshed
  from the Go module proxy on 2026-10-06, with official source docs cross-checked.
- Next work remains the actual SQLite backend, native SQL differential checks,
  configured-only manager persistence, restore/lease/approval expiry, durable
  catch-up and cron restoration. Optional feature and release work remain open.

### Validation and delivery evidence

- Final narrow archival/state projection tests: pass. Final `go test ./...`
  with a shared-package profile, `go test -race ./...`, and `go vet ./...`: pass.
  Deduplicated statement coverage: **88.58%** (**10,009 / 11,300**); agent
  **90.06%** (**4,206 / 4,670**). This measures implemented statements, not parity.
  Final source includes the ordinary-JSON decoder guard and atomic failure test.
- Exporter `--check`: **44 files current**; all 43 previous tracked exports
  remain unchanged. Updated exporter description acknowledges temporary SQL and
  offline model probes; no paid endpoint or credential was used.
- `verify_scans.py`: **19** anchored scans. Selected `verify_guards.py` runs:
  **8** mutations caught (six matching `lease`, including action-result release,
  plus schema downgrade and missing migration column). This is Python guard
  evidence, not Go mutation coverage or the complete unrelated sweep.
- Python full suite: **2,151 passed / 28 skipped / 24 subtests passed**,
  three warnings in **86.02 s**. Python package/runtime/test modules are unchanged;
  package-module invariants are not applicable to this exporter-only change.
- `git diff --check` and README outline: pass. README baseline, canonical Mermaid,
  boundary explanation and interactive semantic specification are updated;
  map geometry and live runtime topology are retained. Archify final acceptance
  is **9/9**, zero composition errors/warnings. Specification SHA-256
  `4aab6a77aef455425b37e0dfcc69eef74eee641489b9906f3207d02cea56b726`
  (34,059 bytes); HTML SHA-256
  `4925b0d1e587338ae5cfcc793f36da45b9013f2600b92a23f02b604c8b535b40`
  (675,135 bytes). Receipts match the exact frozen files. Visual review remains
  skipped after the earlier local-file access denial; no new rendered/browser
  inspection is claimed. Go evidence is darwin/arm64; Linux remains unvalidated.

## Implementation checkpoint — 2026-10-06 live state injection

The full Go-port goal remains active. This slice connects the existing typed
state contracts to live managed sessions without adding a driver or claiming a
production SQLite backend. G0–G7 remain open for the outstanding inventory.

### Source contract and configured runtime

- `StateStore` composes the small session/transcript/event/lease and action/approval
  contracts. `RuntimeConfig.StateStore` enables managed-session injection;
  `ManagerServices.StateStore` selects it for new/forked fleet sessions. Bare
  `NewRuntimeSession` rejects managed state configuration. No dependency, default
  activation, launcher persistence option or production storage substitute is added.
- Manager creation supplies a fresh process lease identity, separate from tenant
  ownership, and initializes state only after binding metadata/system/fork history.
  Default journals and approval rows use the injected store; explicit services keep
  their own stores. Backend close remains caller-owned. Default stored-journal
  construction explicitly marks started actions unknown, matching Python manager
  policy; store/journal construction alone does not mark them. This global policy
  does not prove that another process died or fence external effects.
- Python `session.py::_transcript_guard`, `_capture_event`, `_flush_messages`,
  `_persist_session_record`, `_require_lease` and `_renew_lease` define the live
  timing. Go flushes before provider admission and checks the highest epoch count.
  Immutable protocol content supplies prefix identity; replacement opens a new
  epoch and retains old rows. A rewrite event is stamped before the ensuing flush
  advances the epoch. Ephemeral events consume live sequence IDs without adding
  persisted ordinals. Durable SSE must still address that separate cursor contract.
- Transcript/event/steering projections mask detached copies. Session-row explicit
  system and Todo projection follow the source record contract; this slice does not
  claim blanket masking of every metadata field. Stored event decoding drops human
  execution authority, actors/capabilities and live broker grants.
- Ordinary write errors/panics degrade and remain reported through the concrete
  `PersistenceStatus` snapshot. Count/query invariant failures stop the request.
  A failed renewal stops a turn only after the process successfully acquired the
  lease. Cancellation retains `ErrSessionLeaseLost` across provider-error and
  managed terminal boundaries, suppressing later publication/model/tool admission.
  Confirmation records a past successful claim, not a fresh holder query.
- Renewal and session-row refresh require transcript growth; they are not a timed
  heartbeat. A final assistant-text beat can flush while status is `running`, then
  the no-growth terminal beat leaves that stored status unchanged. The real source
  probe pins this behavior rather than forcing an idle row into the expectation.
  Queued steering has its own metadata write before acknowledgment.

### Go admission and teardown additions

- Go claims after owning serialized turn admission and rechecks acceptance before
  publishing an active turn, including idle HTTP steering. A queued caller cannot
  reuse a claim taken while a previous turn still owned admission.
- Delete disables operational writes/disowns the lease before row deletion and
  cancellation. Late callbacks cannot upsert the deleted row; action/approval audit
  storage remains available for terminal settlement. Failed deletion attempts
  conditional release and reports both errors while remaining cleanup continues.
- Stop releases leases after each owned turn drains. A blocked provider retains its
  lease through the drain window; release failure is recorded and other cleanup
  continues. No backend is closed by manager stop. Unpublished manager state is
  cleaned on failed admission; bare constructor claim/release errors are returned.
- These tests use a concrete, test-only backing with synchronized named records.
  They establish runtime ordering/fault handling and never establish SQL transaction
  atomicity, cross-process leasing, real restart safety or physical durability.

### Actual source probe and remaining work

- Snapshot 45 constructs the actual Python `AgentSession` and `Agent`, using a real
  SQLite database and an offline fake client. It executes capture/guard directly:
  pre-request coverage, Unicode/escaped-secret projections, SDK thinking/text blocks,
  queue writes, old/current epochs, live sequence versus physical cursor, metadata
  timing, disabled terminal fields and confirmed/unconfirmed renewal loss.
- Store-method fault injection distinguishes ordinary append/event degradation from
  guard-count query failure. The count probe first completes its append because the
  real backend also calls `message_count` while appending; otherwise it would test
  a write failure instead of the guard query. Outcomes are derived, not normalized.
- Go executes the same source recipe through its typed injected backing and compares
  counts, queues, raw/masked boundaries, status, terminal fields, epochs/sequences,
  loss and fault outcomes. Additional native runtime tests exercise a real gated
  tool loop, no-effect lease-loss boundaries, queued admission, fork-before-publish,
  owner isolation, deletion with a draining provider and cleanup failure reporting.
  Active terminal rows retain trace/group join IDs before trajectory finalization;
  ordinary state events receive no later trajectory stamp, and stored terminal
  rows do not claim a successful future trajectory finish.
- SQLite driver approval remains unanswered. Native schema/migrations, transactional
  append and conditional lease implementation, session restore/parked-approval expiry,
  crash-tail repair, durable SSE catch-up and launcher selection remain required.
  No goal-completion or G5-completion claim is made.

### Validation and delivery evidence

- Final focused `go test ./agent -run '^TestState' -count=1 -timeout=45s`: pass.
  Final full `go test ./...` with shared-package coverage, `go test -race ./...`
  and `go vet ./...`: pass. Deduplicated statement coverage is **88.69%**
  (**10,311 / 11,626**); agent **90.15%** (**4,504 / 4,996**). These are code
  coverage figures, not migration-completion percentages or SQL durability evidence.
- Exporter `--check` after the selected mutations: **45 files current**; all 44
  previous tracked exports remain unchanged. No paid endpoint is called.
- `verify_scans.py`: **19 anchored scans**. Selected `verify_guards.py` runs catch
  **7 mutations**: six matching `lease` (including the action-result-release
  substring match), plus `injected-input-rides-unlogged`. This is source guard
  evidence, not a full guard sweep or Go mutation coverage.
- Python full suite: **2,151 passed / 28 skipped / 24 subtests passed**, three
  warnings in **99.18 s**. Python package/runtime/test modules are unchanged;
  package-module invariant verification is not applicable to this exporter change.
- `git diff --check` and README outline: pass. README baseline, canonical Mermaid,
  boundary explanation, parity inventory and interactive specification are updated.
  Archify acceptance: **9/9**, zero composition errors/warnings, after **one**
  focused route correction. Frozen specification SHA-256
  `c1d585df0d7e251067d0b02da0e5890ca36aaab84b70e2b951ccb4430115c1ae`
  (34,927 bytes); generated HTML SHA-256
  `1464c33ad82f5ee3d89a1e9b48fb2f7fd97c826f6189f3b98e1f9b3ba682fc98`
  (676,343 bytes). Both receipts match the exact files. Archify visual review
  remains skipped after the earlier local-file access denial; no new browser or
  rendered inspection is claimed. Go tests are darwin/arm64; Linux is unvalidated.

## Implementation checkpoint — 2026-10-06 injected-store session restoration

Baseline: `03bd832` on `feat/go-port`. This iteration implements an explicit
operator restore operation on the typed backend seam. It does not add a driver,
a native SQL backend, launcher activation or physical restart evidence.
The full Python-to-Go objective and G0–G7 remain open.

### Actual source evidence and deliberate Go differences

Snapshot 46 (`python-state-restore.json`) executes the real Python SessionManager,
AgentSession and SQLiteStateStore for seven histories: clean, bare user string,
bare user blocks, two unanswered tools with one parked approval, paired tools,
empty transcript and a foreign active lease. It captures repeat restoration,
actual database projections and a second real manager restore. Source hashes pin
session.py, manager.py, agent.py and storage.py; no paid endpoint is used.

- Python recreates missing saved workspace directories, preserves recorded
  owner/bound/system/run/status and restores with interactive mode/no active turn.
  Recorded `running` is a status fact, not resumed work.
- Source repair flushes before Todo/steering are installed. The live handle keeps
  them, but its stored row and second restore lose both in the repair cases.
  Go installs those facts first, preserving the write and second restoration.
- Source resets live sequence to the physical event ordinal even when a stored
  payload sequence is higher. Go starts above both, avoiding identifier reuse;
  this does not install a durable SSE backlog or interpret an SSE cursor.
- Source repairs before claiming, including a foreign-held session. Go claims
  first, reloads after acquisition and delays repair when the claim is refused.
  A pending handle displays recorded facts but does not expire approvals, append
  a repair, acknowledge parked steering or admit a turn without a later claim.
- Pending approvals expire before repair; their unanswered calls get the shared
  not-run marker. Other unanswered calls get the shared unknown marker, with
  non-error tool results. Bare user tails get the source interruption text.
  No historical grants, actor/capabilities or goal/cron activation are installed.

### Delivered lifecycle and remaining boundaries

`SessionManager.RestoreSessions(ctx)` serializes fleet restores, skips live IDs,
refuses reserved/retiring/deleted identities and reserves each saved identity
before construction. It uses the saved workspace without invoking the new-session
factory. A shared concrete composition helper keeps new and restored services
aligned. The highest epoch and immutable prefix references are seeded before any
append; crash repairs extend the same epoch. Known event rows are validated for
session scope and supported variants, then used to seed sequence numbering.

Pending turn admission claims only after owning admission, re-reads current
metadata/history/events, refuses changed tenant/workspace/binding/system or a
missing row, and repairs before beginning the turn. Failed reloads conditionally
release the acquired lease and remain retryable. Status diagnostics expose
Restored/RestorePending and detached repaired IDs, without changing HTTP Info.

Restore read/repair faults prevent publication. A failed unpublished handle
releases its process lease; its historical rows/workspace and partial database
writes are retained. Earlier handles can remain published when a later fleet row
fails: no fleet transaction or repair rollback is claimed. Stop waits for the
whole inventory/restore operation, prevents late publication and releases an
unpublished acquired lease. Failed deletion cannot revive a remembered identity
within the current manager.

The Go synchronized backing is test-only. Native Go SQLite migrations/WAL,
transaction/rollback, real reopen and concurrent-connection outcomes still need
an explicitly approved driver. Leases are not external-effect transactions,
fencing or a periodic heartbeat. Scheduled-session restore resolution, plan/goal
event variants and folding, durable SSE/catch-up and launcher selection remain.
The existing manager-wide unknown-action policy does not prove foreign death.

### Validation and delivery evidence

- Focused `go test ./agent -run '^(TestRestore|TestState|TestManager)' -count=1
  -timeout=60s`: pass. Seven actual-source recipes compare repaired projections;
  tests make the first resumed model request and validate pairing and prior
  transcript coverage. Pending reload, changed identities, scoped event refusal,
  backend faults, repair failure, reservations/deletion and stop races are tested.
- Full `go test ./...` with shared-package coverage, `go test -race ./...`
  and `go vet ./...`: pass. Deduplicated statement coverage **88.74%**
  (**10,563 / 11,903**); agent **90.27%** (**4,760 / 5,273**);
  restore.go **92.42%** (**244 / 264**). These measure implemented code,
  not migration completion, native SQL or Linux behavior.
- Exporter final `--check`: **46 files current**; all 45 previous tracked
  exports remain unchanged. `verify_scans.py`: **19 anchored scans**.
  Python full suite: **2,151 passed / 28 skipped / 24 subtests passed**, three
  warnings in **86.59 s**. No paid endpoint was called.
- `git diff --check` and README outline: pass. Exact task-owned paths are staged;
  all implementation gates are terminal before commit/push.
- Five selected source mutations caught: owner-before-build, restored digest
  seeding, queued-steer restoration, crash interruption and unknown/not-run
  distinction. This is a focused source guard run, not a full sweep or Go mutation
  coverage. Python runtime/package/test modules are unchanged; package-module
  invariant verification is not applicable to this exporter-only Python change.
- Architecture README baseline/Mermaid/boundary explanation and specification
  updated. Archify accepts **9/9**, zero composition errors/warnings, with
  **zero geometry correction rounds**. Specification SHA-256
  `427eacc01d81d6b82fddf1a344a602ff98138821719637036980fcff49ad2946`
  (35,433 bytes); generated HTML SHA-256
  `343671253320fdecaa5678c66822a8aa8fbc13cae6b8a0b89aabdd1f50999c5e`
  (676,849 bytes). Receipt hashes match the exact files. Visual review remains
  skipped after the earlier local-file access denial; no rendered inspection is
  claimed. Go tests run on darwin/arm64; Linux remains unvalidated.

## Implementation checkpoint — 2026-10-06 lazy scheduled session restoration

Baseline: `067f16e` on `feat/go-port`. This iteration connects the source cron
stable-ID resolver to typed Go state restoration. No dependencies are added;
native Go SQLite and real database reopen evidence still await driver approval.
P0/P1 remain complete; the full G0–G7 port/release objective remains active.

### Actual Python scheduled path and next-request evidence

Snapshot 47 (`python-scheduled-restore.json`) invokes real SessionManager
restore_scheduled_session with real SQLiteStateStore or NullStateStore, then makes
the next AgentSession.run request through FakeAsyncAnthropic. Seven cases cover
bound/scratch clean history, bound/scratch crash tails, missing SQLite and Null
rows, and a foreign-held bound row. It records actual factory calls, selected
workspace, saved/live system, duplicate handle identity, owner/mode/run/status,
repair IDs, lease confirmation, next-request result/count and persistence. Source
hashes pin manager.py/session.py/cron.py/storage.py. No paid endpoint is called.

- Existing live handles return unchanged, including their mode/system/owner.
- Saved bound sessions retain the recorded workspace; scratch restoration invokes
  today's factory using the stable ID. The default factory uses WorkspaceRoot/id.
- The source scheduled constructor omits recorded explicit system, while fleet
  restoration passes it. Go reproduces this scheduled path using the current
  system builder and nil explicit system, independently of new-session defaults.
  A clean stored row retains its old path/system until growth; repair/growth
  publishes the scheduled projection. Owner and recorded run/status/history persist.
- A missing row creates an anonymous, idle, interactive handle. With Null storage,
  its next request runs ephemerally. With real source SQLite, conditional UPDATE
  cannot claim a nonexistent session row: lease remains unconfirmed and the next
  run raises LeaseLost before any model call. Go reproduces that refusal and
  reports the lost occurrence; it performs no unconditional upsert that could
  overwrite a concurrent creator. This is a measured source gap, not proof that
  a fresh durable identity can run without a stored record.
- Known crash-tail repair preserves pairing and the shared unknown marker. Go
  retains its prior metadata-preservation and claim-before-repair additions.
  Foreign-held state remains pending; later turn admission re-reads under claim.

### Delivered composition, authority and shutdown

`SessionManager.RestoreScheduledSession(ctx, id)` is a privileged embedding
operation, not a new HTTP owner-bypass route. Live identity lookup precedes store
inventory. Missing handles share fleet restoration serialization, creating/drain
ownership and exact-ID reservations. Retiring/remembered deleted identities are
refused. Bound and scratch provisioning are explicit variants of one constructor;
saved facts are installed before publication. Errors release owned leases and
preserve historical state/workspaces.

The stored expected identity is distinct from the current scheduled execution
projection. Tenant, recorded workspace/binding/system changes still refuse pending
reload. A successful upsert advances the expected projection so a retry after
partial repair accepts its own newly selected path/system. This is not lease
fencing or a transaction with external effects.

The manager cron resolver reuses a live runner or returns a typed lazy restore
runner. Restoration runs inside the scheduler-owned cancellation context; Run
then creates a fresh default untrusted context, with no scheduling actor,
capabilities or old message identity. Mark/save/minute claim still precede
resolution, and restored jobs stay disarmed until a new operator Arm. No model
arm operation or activation restoration is introduced.

Manager Stop now cancels a restore lifetime context before waiting for creating
operations. This cancels cooperative inventory/factory/backend reads, closes late
publication and joins cleanup; callbacks ignoring cancellation can still hold
shutdown. Restore checks cancellation before repair. The previously blocked
approval-read stop test now proves original history is retained without repair.
Info snapshots creation time under its existing metadata lock, covering concurrent
pending reload. Active turn drain, saved ownership, lease release, one-shot
removal and bounded scheduler diagnostics retain their existing rules.

Remaining: native Go SQLite/WAL/migrations/transactions/reopen/concurrent connections,
plan/goal event variants/folding, durable SSE/catch-up, state-backend launcher
selection and the rest of G0–G7. Independent claim files do not transact with
external effects, and global unknown-action marking does not prove foreign death.
The test backing is synchronized memory, not a shipped backend or restart proof.

### Validation and delivery evidence

- Focused agent tests for Scheduled/ManagedCron/Restore/State/ManagerCron: pass.
  Seven actual source recipes compare projections and next model requests.
  Go integration tests restore an armed disk-loaded job lazily with fresh
  untrusted provenance, validate crash-tail pairing/persistence-before-request,
  compare factory/system/owner selection, handle foreign/missing row refusal,
  retry after partial selected-projection repair, bypass storage for live handles,
  refuse stopped resolution and cancel pending inventory during manager Stop.
- Full `go test ./... -count=1` with shared-package coverage,
  `go test -race ./... -count=1` and `go vet ./...`: pass.
  Deduplicated statement coverage **88.70%** (**10,663 / 12,021**);
  agent **90.15%** (**4,860 / 5,391**). New branches expand the denominator;
  these are code coverage, not migration completion or SQL durability.
- Final exporter `--check`: **47 files current**. `verify_scans.py`:
  **19 anchored scans**. All 46 previous tracked exports remain unchanged.
  Python full regression: **2,151 passed / 28 skipped / 24 subtests passed**,
  three warnings in **84.93 s**. No paid endpoint was called.
- `git diff --check` and README outline: pass. All implementation gates are
  terminal before exact-path staging, commit and push.
- Three selected source mutations caught: cron owner-before-build, restored job
  requiring a new arm, and delete cancelling jobs before resurrection. This is
  targeted source evidence, not a full mutation sweep or Go mutation coverage.
  Python runtime/package/test modules are unchanged; package-module invariant
  verification does not apply to this exporter-only Python edit.
- README baseline/canonical Mermaid/authority and persistence explanation plus
  interactive specification are updated. A new lazy cron-to-manager restore
  connection uses automatic routing. Archify acceptance **9/9**, zero errors and
  warnings, **zero correction rounds**. Specification SHA-256
  `416eccffb064764597a0b20b039ce74f3cf046d79b50627a5b8ec157b4de32bd`
  (36,028 bytes); generated HTML SHA-256
  `5286c4a5f5abee52fc4ad66ab7cdd2730990f24ef64b6ae9f8617f9b435b0f7a`
  (677,651 bytes). Both receipts match the exact files. Visual review remains
  skipped after the earlier local-file access denial; no rendered inspection is
  claimed. Go test platform is darwin/arm64; Linux remains unvalidated.

## Implementation checkpoint — 2026-10-06 bounded stored-event SSE catch-up

Baseline: `647ebd7` on `feat/go-port`. This iteration composes the injected
EventStore with owned SSE resume. P0/P1 remain complete; G0–G7 remain open.
No dependencies are added; native Go SQLite still awaits explicit driver approval.

### Actual source endpoint evidence and measured differences

Snapshot 48 (`python-event-catchup.json`) invokes the actual registered events
endpoint and consumes its actual EventSourceResponse body iterator against real
SQLiteStateStore/NullStateStore. Seventeen cases pin normal/bounded windows,
ephemeral sequence gaps, a real event emitted during the threaded read, Null
fallback and twelve header inputs. Source SHA-256 pins server.py/session.py/storage.py.
The direct iterator probe is not a live HTTP transport test; Python's existing
live-uvicorn tests and Go's new TCP tests supply separate wire evidence.

- Resume 5 on 250 stored status events returns 6–250; a 2,210-event session
  returns the newest 2,000. Fresh/zero/negative/invalid IDs return the last 200
  backlog entries. Null storage also falls back to backlog.
- Subscribe precedes the read. A source event emitted in the load callback is
  present in both storage and the queue but sent once. Iterator cancellation
  leaves zero subscribers in all captured cases.
- With ten actual `_ephemeral` deltas before each status, live sequence reaches
  2,750 while storage has 250 rows. Python queries physical `after=750`, returning
  no rows, then only 200 backlog statuses: fifty stored statuses are lost.
  Go reads by physical ordinal and filters by event sequence, returning all 250
  statuses within its storage window. This is a measured Go correction.
- The source accepts decimal sign/whitespace/underscore syntax. Its default
  Python 3.11 conversion refuses over 4,300 digits and ASCII U+001C/U+001F
  whitespace, falling back to backlog. Go retains that pinned default, including
  Python Unicode decimal digits and integer whitespace distinctions.
- A huge positive valid decimal ID exceeds SQLite's signed integer adapter and
  raises OverflowError in the actual source iterator. Go saturates the sequence
  filter without using it as a SQL ordinal, so no rows pass and no integer
  overflow is required. Other backend read faults return an opaque HTTP 503
  before SSE headers, differing from the source generator's post-header failure.

### Delivered typed composition and boundaries

`EventOrdinal` now names physical row positions in SessionRecord/EventStore;
`EventSequence` continues to name live/SSE IDs. JSON numeric representation is
unchanged. Embedding implementations must update the exported method signatures.
Restoration explicitly takes the physical cursor as a minimum and still uses the
maximum stored payload sequence, preserving its no-reuse rule.

`ManagedSession.CatchUpEvents(ctx, sequence)` skips storage for cursor zero,
missing persistence or deleted state. Otherwise it reads the physical head and
at most 2,000 newest rows with a concrete limit. Stored records must have the
session's identity, positive/increasing sequence, known closed variants and
non-ephemeral payloads. It re-decodes detached archival projections, preserving
informational lineage but stripping live actor/capability authority. No historical
approval grants or activation are installed. Bad scope/order/variant, overlarge
windows and callback errors/panics fail closed without changing writer fault or
lease status. Historical reads require no lease acquisition.

The reader briefly snapshots configuration under the persistence lock and releases
it before backend I/O; the backend owns concurrency safety. The HTTP owner and
envelope checks precede subscription/read. Subscribe-first, caught-window-first
and max-sequence queue de-duplication preserve the handoff. Disconnect cancellation
reaches cooperative backend reads and deferred subscription cleanup; independent
live capture/provider work can proceed while the read is blocked. Fresh replay,
no-store behavior and submitted-turn stream ownership retain their existing rules.

Remaining: native Go SQLite/WAL/migrations/transactions/reopen/concurrent-process
proof, backend launcher selection, plan/goal variants/folding, other optional
feature groups and G7 release audit. The window and 2,000-entry live queue can
shed older data; complete-history resume and cross-process live tailing are not
claimed. The Go backing used here is synchronized test memory, not a shipped
backend or physical durability evidence. No paid endpoint was called.

### Validation and delivery evidence

- Focused agent/HTTP catch-up tests: pass. Cases cover actual source recipes,
  restored empty-backlog reads, immutable masked/untrusted projections, callback
  errors/panics, foreign rows, duplicate order, unsupported/ephemeral payloads,
  overlarge windows, fresh/deleted paths and cancellation. Real TCP cases cover
  owner-before-read, more than 200 old events, emission during catch-up,
  de-duplication/live continuation, fresh 200-event replay, blocked-read concurrent
  writer/provider work, opaque 503 and disconnect reclamation.
- Full `go test ./... -count=1 -timeout=180s -coverpkg=./...` with a coverage
  profile, `go test -race ./... -count=1 -timeout=240s`, and `go vet ./...`: pass.
  Deduplicated statement coverage **88.83%** (**10,756 / 12,108**); agent
  **90.29%** (**4,909 / 5,437**); HTTP **85.36%** (**647 / 758**).
  These measure implemented statements, not migration completion/durability.
- Exporter final `--check`: **48 files current**. Previous tracked exports remain
  unchanged. `verify_scans.py`: **19 anchored scans**. Selected source guards
  `sse-resume-gaps-beyond-the-backlog`, `subscriber-queue-unbounded` and
  `ownership-leaks-existence`: all three caught. This is targeted source mutation
  evidence, not a full mutation sweep or Go mutation coverage.
- Python full regression: **2,151 passed / 28 skipped / 24 subtests passed**,
  three warnings in **90.95 s**. Python runtime/package/test modules are unchanged;
  package-module invariant checks do not apply to this exporter-only Python edit.
- `git diff --check` and README outline: pass. README review baseline, canonical
  Mermaid, boundary prose, extension seam and parity matrix are updated.
- Interactive map regenerated from its JSON via Archify deliver:
  **9/9 showcase, zero errors/warnings**. The map groups the core and managed
  handle; the existing session-state relationship now includes bounded catch-up.
  Two focused candidate repairs corrected a note length and removed a redundant
  direct HTTP-state edge that crossed core nodes/corridors. No HTML is hand-edited.
  `correction_rounds: 2`; `diagram_type: architecture`.
  Specification **36,054 bytes**, SHA-256
  `2c5885d5b2172ff8a3ac3f935369dd1bb9f94965db20274af69dae77f4b079da`;
  artifact **677,747 bytes**, SHA-256
  `2e11ed913425cd6db7f6cdd6e2bb5b16ad6d5ce1fe7766afd2d1862548658399`.
  Exact saved bytes match both receipt hashes. Output:
  `docs/mini-loop-system.architecture.html`.
  `visual_review: skipped` — previous local-file access denial leaves no accessible
  rendered image for inspection; no bypass or visual-pass claim is made.
- Environment: darwin/arm64, Go 1.23.3. Linux execution is unvalidated.
  All implementation gates are terminal before exact-path staging/commit/push.

## Implementation checkpoint — 2026-10-06 owned transcript epoch reads

Baseline: `7d0cf39` on `feat/go-port`. This iteration completes the configured
TranscriptStore read path behind the already registered owned HTTP endpoint.
No dependency is added. P0/P1 stay complete; G0–G7 remain open, including native
Go SQLite and real database reopen/concurrent-process verification.

### Actual Python endpoint and canonical-history evidence

Snapshot 49 (`python-transcript.json`) executes real FastAPI requests with real
SQLiteStateStore/NullStateStore, real SessionManager and real microcompact. It
captures 56 responses and both stored compaction epochs. Source SHA-256 pins
server.py/session.py/storage.py/compaction.py; the installed validation stack is
FastAPI 0.136.3, Pydantic 2.13.4 and pydantic_core 2.46.4. No paid model is called.

- Empty storage reports current epoch zero, so default/explicit reads outside
  `1..current` return the source's exact 404 detail. Live messages are not a
  substitute for the durable endpoint, including after a Null-store turn.
- The source flushes original history, performs actual microcompact and flushes
  the rewritten projection. Both source and Go still expose original tool-result
  bodies through epoch 1 and cleared bodies through epoch 2. Stored secrets are
  masked; history returned by the API is the persisted projection.
- Default selects highest stored epoch; explicit epochs within that bound can
  be empty when there is a gap. A higher stored epoch containing an unanswered
  tool use is readable archival data; the reader does not synthesize a result.
- Authentication middleware precedes validation. Invalid epoch requests return
  422 before owner/missing-ID lookup; valid foreign/missing requests return 404.
  Missing credentials remain 401 with Bearer challenge. Query-token auth is not
  accepted for this route. Repeated epoch parameters select the last value.
- Pydantic accepts ASCII integers, sign/whitespace, underscores and an integral
  `.0` suffix; Unicode decimal digits, fractional values and invalid text fail.
  Large valid integers retain their numeric identity in out-of-range 404 details.
  Excess significant digits return typed `int_parsing_size` validation details;
  long leading-zero strings can still select epoch 1. This differs from the
  Python int syntax used by the SSE Last-Event-ID route and is tested separately.

### Delivered concrete reader and boundaries

`agent.TranscriptSelection` has a zero/current variant and an owned exact
`big.Int` value. `SelectTranscriptEpoch` supports concrete backend epoch values;
`SelectTranscriptEpochNumber` copies arbitrary exact query integers. No generic
payload or raw JSON is retained in service/domain state. Concrete
`TranscriptSnapshot` names session/epoch/highest-epoch/messages, and
`TranscriptEpochNotFound` names the requested decimal identity and current epoch.

`ManagedSession.ReadTranscript(ctx, selection)` reads the configured backend,
validates supported message roles/closed content and returns detached copies.
It briefly snapshots persistence configuration, releases that mutex before
backend I/O, passes cancellation into metadata/message reads and checks it before
publication and during copying. Reads do not flush live history, acquire a lease,
start a model turn, repair tool calls, mutate writer error state or install human
permissions. A foreign-held pending-restoration handle can read its unrepaired
stored history without changing the holder or claim count. Library callers own
authorization; the HTTP adapter performs owner admission before backend access.

The HTTP adapter uses concrete query-issue variants and a two-element location,
matching the captured 422 shapes. Opaque 503 handles backend read errors/panics
before writing any transcript body; unlike source uncaught backend faults, this
is an explicit Go addition. There is no live/raw fallback. Metadata selection
and message loading are separate backend reads, matching the source; no database
snapshot transaction is claimed. An epoch returns its full stored message array,
not a newly introduced pagination/truncation policy.

The synchronized test backing now reports epoch zero for no stored messages,
including empty append bookkeeping. The former empty-restore test expected a
phantom stored epoch 1; it now compares the actual source's zero while runtime
restoration still starts its next write at at least one. Python runtime/package
code is unchanged. Native Go SQL, backend launcher selection, unsupported
state/provider variants, optional feature groups and release audit remain open.

### Validation and delivery evidence

- Focused agent/HTTP tests: pass. Native tests drive actual Go flush/microcompact
  and compare both epochs with source; cover no-live fallback, detached masked
  rows, selected/gap/crash-tail reads, exact large-integer ownership, deleted state,
  metadata/load faults and panics, malformed rows, cancellation, concurrent event
  capture and foreign-held pending restoration without repair/claim mutation.
  HTTP tests compare all 56 actual source responses, exercise a real TCP epoch
  response without a model turn, and cancel a blocked TCP read while another
  provider turn persists successfully. The backing is test memory, not SQL proof.
- Full `go test ./... -count=1 -timeout=180s -coverpkg=./...` with coverage profile,
  `go test -race ./... -count=1 -timeout=240s`, and `go vet ./...`: pass.
  Deduplicated statement coverage **88.87%** (**10,849 / 12,208**); agent
  **90.30%** (**4,953 / 5,485**); HTTP **86.42%** (**700 / 810**).
- Exporter final `--check`: **49 files current**; the 48 earlier tracked exports
  are unchanged. `verify_scans.py`: **19 anchored scans**. Selected source
  `foreign-caller-reads-the-transcript` and
  `compaction-splices-into-the-canonical-epoch` mutations: both caught. This is
  targeted source evidence, not full mutation or Go mutation coverage.
- Python full regression: **2,151 passed / 28 skipped / 24 subtests passed**,
  three warnings in **89.28 s**. Package-module invariant checks do not apply
  because only the Python exporter changes; no package/runtime/test module does.
- `git diff --check` and README outline: pass. README review baseline/canonical
  Mermaid/boundary prose, EXTENDING and parity/current-plan status are updated.
- Interactive map regenerated from JSON with Archify deliver:
  **9/9 showcase, zero errors/warnings**, `diagram_type: architecture`,
  `correction_rounds: 0`. Existing state relationship now names epoch reads;
  no geometry or generated HTML is manually edited.
  Specification **36,040 bytes**, SHA-256
  `fff91e86ffa4bff93c07fcfa0748f202fe996771de0de381654daf740b15eb2d`;
  artifact **677,732 bytes**, SHA-256
  `92e80b35a58cf06ebcd5d22605a4d291aef067db229d5dfb50befa2f81a9d359`.
  Saved bytes match both receipts. Output: `docs/mini-loop-system.architecture.html`.
  `visual_review: skipped` — prior local-file access denial leaves no accessible
  rendered image; no workaround or visual-pass claim is made.
- Environment: Go 1.23.3, darwin/arm64; Linux execution remains unvalidated.
  Required implementation gates are terminal before exact-path staging/commit/push.


## Implementation checkpoint — 2026-10-06 typed plan mode

Baseline `31cbc44`; P0/P1 remain complete and G0–G7 remain open. This slice
ports the actual optional Python plan tools and log-folded prompt guidance.
Native SQLite/backend/restart, goals, other optional modules and the final
release audit still require implementation and evidence.

### Actual source contract and differential evidence

- Snapshot 50 (`go/testdata/python-plan-mode.json`) invokes 35 actual
  `Agent._exec_tool` calls in five installations: headless, readonly, accepted
  review, rejected review and fixed system. It compares schemas, empty source
  capabilities, read/readonly/exclusive traits, canonical inputs, grant proposals,
  tool outputs, active state, system section and stable catalog fingerprint.
  Repeated entry emits another whole boolean event; inactive exit, invalid
  Markdown and rejected review produce readable failure without leaving planning.
- Two actual Python SQLite managers restore opposite final logged values and
  make the next offline model request; the request's planning section matches the
  restored value. The fixture records SHA-256 for plan_mode/prompts/session/registry.
  No paid model endpoint is called. All 49 previous exports remain unchanged.
- Nil reviewer uses source headless approval. The source installation does not
  implicitly adapt the session approval broker into a plan reviewer; Go preserves
  that choice. Strict Go provider input rejects missing/null/nonstring plan and
  model-supplied identity/approval fields. Actual tools emit boolean `active`;
  Go archival decoding requires that boolean, while Python's general fold accepts
  truthy values. This boundary difference is explicit.

### Delivered types and runtime integration

- Closed EnterPlanMode / ExitPlanMode ToolInput variants with named
  ExitPlanModeInput, typed schemas, canonical identity and string masking.
  Explicit RuntimeConfig/ManagerServices/launcher PlanModeTools and
  --plan-mode-tools activate the individual pair; default tools stay ten.
- Concrete PlanReviewRequest / PlanReview and PlanApprover interface. Reviews
  receive the final trimmed plan and bound ToolAuthority after common gate checks.
  Rejection is failed telemetry with feedback; callback faults/panics and cancelled
  approval retain active planning. Callback implementations own cancellation,
  fleet concurrency and non-reentry. Nil/CLI approval is headless, not a human UI.
- Atomic Session.PlanModeActive; concrete PlanModeEvent and archival encoder/
  decoder; last-value restore fold, including pending reload after a claim.
  The default SystemBuilder appends the exact source PlanSection; custom builders
  receive SystemContext.PlanMode and fixed systems stay fixed. Envelope changes
  reflect prompt changes, while fitted tool schemas stay unchanged.
- Plan mode does not change sandbox or effect permissions. Native tests show
  Auto still writes and Readonly denies. Fresh forks start inactive. Explicitly
  selected children bind independent state/reviewer authority; default role
  profiles omit the source capability-free pair. Like source, restoration folds
  all logged plan events, including child scopes; this fact grants no authority.
- Existing event-first capture, trajectories and stored-event SSE carry the
  boolean event. A native three-round model turn proves persisted entry before
  the next provider request and prompt states off/on/off with stable schemas.
  Ordinary store faults retain existing degraded-persistence semantics; no
  durable backend or atomic state-plus-external-effect transaction is claimed.

### Validation and delivery evidence

- Focused plan-mode agent/protocol/launcher/CLI tests: pass. Cases cover 35 source
  calls and both restore recipes, exact source whitespace/feedback, stable schemas,
  no-op event logging, invalid archival rows, callback errors/panics/cancellation,
  masked review feedback, raw review input, foreign authority, fork/child state,
  pending claim reload and bounded event catch-up. Local TCP launcher tests drive
  real HTTP provider calls and three planning/model rounds; no paid endpoint.
- Full `go test ./... -count=1 -timeout=180s -coverpkg=./...` with coverage profile,
  `go test -race ./... -count=1 -timeout=240s`, and `go vet ./...`: pass.
  Deduplicated statement coverage **88.93%** (**10,928 / 12,289**); agent
  **90.37%** (**5,013 / 5,547**); protocol **88.98%** (**969 / 1,089**);
  HTTP **86.42%** (**700 / 810**). Coverage measures implemented statements,
  not overall migration completion or native SQL durability.
- Exporter final `--check`: **50 files current**. `verify_scans.py`: **19 anchored
  scans**. Three selected source guards caught: rejected-plan-still-exits,
  exit-outside-plan-mode-flips-state and restore-forgets-plan-mode. These are
  focused Python mutation checks, not Go mutation coverage or a full guard sweep.
- Python full regression: **2,151 passed / 28 skipped / 24 subtests passed**,
  three warnings in **78.07 s**. Python runtime/package/test modules are unchanged;
  package-module invariant checks do not apply to the exporter-only Python edit.
- `git diff --check` and README outline: pass. README review baseline/canonical
  Mermaid/boundary prose, extension seam, current plan and parity matrix updated.
  Historical checkpoints retain their contemporaneous pending labels; current
  storage/HTTP summaries now name implemented catch-up/epoch/plan behavior.
- Interactive architecture HTML regenerated from frozen JSON via Archify:
  **9/9 showcase**, **zero composition errors/warnings**,
  `diagram_type: architecture`, `correction_rounds: 0`.
  Specification **36,197 bytes**, SHA-256
  `c179125b466e3721e70daca861977ddde174c5ac9a0f5b6f89a85af824f7474e`;
  artifact **677,886 bytes**, SHA-256
  `e941ad8b2f4247766d4907b4574b7825d858127c8de42dcb6fb35d483b263a16`.
  Exact saved files match receipts. Output docs/mini-loop-system.architecture.html.
  `visual_review: skipped` — prior local-file access denial remains; no rendered
  inspection or bypass is claimed. JSON geometry and existing edges are retained.
- Environment: Go 1.23.3, darwin/arm64; Linux remains unvalidated. All required
  implementation gates are terminal before exact-path staging/commit/push.

## Implementation checkpoint — 2026-10-06 typed goal domain

Baseline `f30cbfd`; P0/P1 remain complete, G0–G7 remain open. The five shipped
Python goal tools, their default continuation consumer and owned read endpoint
are now implemented in Go. Native SQLite/restart, the remaining optional groups,
context/provider variants and release audit still require evidence.

### Actual source contract and differential evidence

- Snapshot 51 (`go/testdata/python-goals.json`) executes 46 actual source gate
  calls plus six direct stop calls across Auto/Readonly. It captures schemas,
  empty capabilities, risk/readonly/exclusive traits, canonical/grant identity,
  every result's failed/denied flags, detached whole snapshots and cumulative
  events with normalized random IDs. Existing 50 snapshots remain unchanged.
- Actual Agent default/custom-hook loops make three/one model requests. Actual
  Python SQLite restore retains an active revision-five goal but always disarms;
  an untrusted next turn makes one request. One further actual trusted resume
  gate call re-arms; two model requests consume the remaining budget and block.
  Four real owner/auth HTTP views pin the source goal response. No paid model
  endpoint is called. Fixture source SHA-256 pins goals/permissions/agent/session/server;
  fixture 70,069 bytes, SHA-256
  `a37572caf70c5190c9412d18e140365c9c4303af051e8e9609deaa20c1ef456a`.
- Create/resume check explicit human provenance before domain CAS; authenticated
  HTTP alone does not grant it. Other mutations do not arm. Block may change an
  already complete goal, matching source. Omitted/null/zero caps use ten. The
  actual regex accepts repeated/trailing hyphens and one final newline; Python
  whitespace trimming is preserved. No edit/clear/pause tool ships, although
  source cap-exhaustion prose mentions nonexistent `goal_edit`.
- Domain refusals are source textual tool outputs with `failed=false`; permission
  denials stay denied. Go rejects JSON coercions and signed-64 overflow where
  Python accepts coerced/unbounded integers. Go archival decoding validates
  supported operations, phase/reason consistency and bounded positive records;
  Python generic fold tolerates partial dicts/unknown operations. Both fold all
  logged scopes and clear tombstones; neither restores activation.

### Delivered composition and boundaries

- Closed Create/Status/Complete/Block/Resume inputs, named GoalID/GoalRevision/
  GoalPhase/GoalBlockReason/GoalRecord and GoalChangeEvent. Cap pointers, snapshot
  blocked pointers and event payloads are detached. CAS runs under per-session
  synchronization through the existing exclusive execution gate. Revisions do
  not wrap. Live goal text remains raw; configured sinks and HTTP project masks.
- Explicit RuntimeConfig/ManagerServices/launcher GoalTools and --goal-tools
  install five additional tools. Default ten remain unchanged. `GoalContinuation`
  is a stateless default stop hook, inert without an armed active goal. Nil
  StopHooks uses it; every explicit list replaces it, including empty lists.
  Manager copying preserves the nil/empty distinction; children inherit the
  selected hook chain with independent session goals and peer provenance.
- Only requested stop continuations consume goal budget; model tool rounds do
  not. Counter mutation/emission precedes stuck/global-round checks, so it counts
  requested continuations even if the next provider request cannot start. Source
  RunContext persists through that turn's continuation. There is no goal-specific
  prompt section, permission expansion or new authority stamp.
- Owned GET /sessions/{session_id}/goal exposes concrete detached goal,
  goal_armed and plan_mode fields; total Go HTTP method/path operations are now
  28. Owner admission precedes reads. It never arms or starts a turn. Existing
  capture/trajectory/stored-event SSE encode typed snapshots. Injected-store
  restore and pending claim reload install the folded goal with armed=false;
  fresh forks/children do not inherit it. Test backings do not prove Go SQL.
- A separate release-audit gap was exposed while tracing source telemetry:
  current Go plan validation/review refusals return errors and set failed=true,
  while Python returns textual Error strings with failed=false. Snapshot 50
  pins output/state but not these flags. The current parity row records this
  difference; G7 remains open rather than claiming complete telemetry parity.

### Validation and delivery evidence

- Focused agent/protocol/HTTP/launcher/CLI goal tests pass. Meaningful cases
  include 12 concurrent CAS writers (one mutation), detached/blocked-pointer
  reads, signed-revision overflow, cancellation, malformed archival records,
  clear/pending reload, disarmed SQL recipe reconstruction, trusted re-arm,
  fresh fork/child state, child arming refusal and detached reader snapshots. A native
  four-request model turn proves create/round capture before the next request
  and retained source provenance; a one-request global budget pins attempt-count
  ordering. Real TCP launcher calls prove explicit 15-tool selection and normal
  HTTP's inability to arm. HTTP secrets stay masked without changing live state.
- Full go test ./... -count=1 -timeout=180s -coverpkg=./... with profile: pass.
  Deduplicated statement coverage **89.09%** (**11,185 / 12,555**); agent
  **90.56%** (**5,211 / 5,754**), protocol **89.40%** (**1,020 / 1,141**),
  HTTP **86.64%** (**707 / 816**), launcher **76.67%** (**115 / 150**).
  This measures implemented statements, not migration completion.
- Full go test -race ./... -count=1 -timeout=240s and go vet ./...: pass.
  Additional focused goal race tests pass after adding archive/blocked-mask cases.
  Full ordinary coverage tests were rerun after those test-only additions.
- Exporter --check: 51 files current. verify_scans.py: 19 anchored scans.
  Four selected goal source mutations caught: stale CAS, budget overflow,
  untrusted arming and armed restoration. This is a focused Python guard
  selection, not Go mutation coverage or the complete guard sweep.
- Python full regression: 2,151 passed / 28 skipped / 24 subtests passed,
  three warnings in 93.16 s. Python package/runtime/test modules are unchanged;
  invariant checker does not apply to the exporter-only Python edit.
- git diff --check and README outline pass. README baseline/canonical Mermaid,
  ownership/default prose, extension seam, Go guide, current plan and parity row
  are updated. Existing historical checkpoints retain their original status.
- Archify regenerated HTML from frozen JSON: architecture, 9/9 showcase, zero
  composition errors/warnings. correction_rounds: 2 (bounded metadata-length
  repair only; existing geometry/edges retained). Specification 36,448 bytes,
  SHA-256 `4a20f85a77bac73f78b65f49360a95c036554a88af5510dc51fd6c267060738f`;
  artifact 678,149 bytes, SHA-256
  `fb6d7926aa96e65db8fae72dd5778d07a3c98d27cc2a1e81ac2aa4b865d02d63`.
  Saved-byte identity matches both receipts. Output
  docs/mini-loop-system.architecture.html. visual_review: skipped — prior
  local-file access denial remains; no rendered inspection or bypass is claimed.
- Go 1.23.3 darwin/arm64; Linux-host validation remains open. No dependencies
  added. No top-level docs/*.md report was added/renamed/removed, so Research
  Atlas regeneration does not apply. SQLite driver approval is still pending;
  other port work can continue. Required gates are terminal before exact-path
  staging, commit and push.

## Implementation checkpoint — 2026-10-06 plan outcome fidelity

Baseline `f21541a`; P0/P1 remain complete, G0–G7 remain open. This closes the
specific plan-refusal telemetry gap identified in the previous checkpoint.
Native SQL/restart, remaining optional groups, provider/context variants and
the full release audit still require implementation and evidence.

### Actual source outcomes and bounded comparison

- Python's inactive, invalid-Markdown and reviewer-rejected plan calls return
  textual Error feedback rather than raising. Their settled journal status is
  completed, and result observers, live/stored events and stuck steps retain
  failed=false. Reviewer/hook exceptions retain failed=true. The source guard
  prose calls a rejected plan a failed call; that wording does not describe
  the actual telemetry bit. No error-prefix classification is introduced.
- Go now returns those three business refusals as exact source text with nil
  execution error. Permission/guard checks and genuine callback faults retain
  the common gate ordering. Cancellation/panic handling and the retained
  planning state remain unchanged. No new reviewer or caller authority exists.
- Snapshot 50 now additionally captures all 35 actual source failed/denied
  outcomes. Its updated 16,955-byte fixture has SHA-256
  `5535d57a4250969a7a22c09fa4ce721008723cf3e0d2b41aadfd501437fd9cec`.
  Historical checkpoint hashes describe their original fixture versions.
- New snapshot 52 (`go/testdata/python-plan-outcomes.json`) executes five actual
  Python SessionManager/Agent loops: rejection, approval, reviewer fault,
  before-hook denial and after-hook fault. Each makes four model requests and
  three gate calls with a memory action journal and real JSONL recording.
  Each then directly replays the same action once. Fixture 20,537 bytes,
  SHA-256 `c37eb20a7da3b6c01d8fd66982c5d7aa932d1a12efdc54e2a599f72b480cb64b`.
  Source file hashes pin the executable plan/gate/journal/hook/trajectory code.
- Native tests compare settled rows before result observation, failed/denied
  flags in live and recorded events, stuck steps, model result content and
  absence of is_error, review calls and aggregate tool-error counts
  (0, 0, 1, 2, 2). Cache annotations have separate contracts; this projection
  does not claim complete provider-wire parity. The existing model-result
  omission already matched Python and required no production change.
- The source replay uses a direct gate call; Go reuses the same RunContext
  and action ID in a second two-request managed turn. The comparison covers
  the replay outcome, observer row, review calls and planning state, not
  whole-turn event equivalence. Stored failed text replays with fresh
  failed=false unless a current after hook faults; current before denials
  still win before replay. These memory backings do not prove native SQL.

### Validation and delivery evidence

- Focused go test ./agent ./trajectory -run '^TestPlan' -count=1: pass.
  Full go test ./... -count=1 -timeout=180s -coverpkg=./... with profile: pass.
  Deduplicated statement coverage **89.10%** (**11,187 / 12,555**); agent
  **90.60%** (**5,213 / 5,754**), protocol **89.40%** (**1,020 / 1,141**),
  HTTP **86.64%** (**707 / 816**), trajectory **78.84%** (**339 / 430**).
  Coverage measures implemented statements, not migration completion.
- Full go test -race ./... -count=1 -timeout=240s and go vet ./...: pass.
  Exporter --check: 52 files current. verify_scans.py: 19 anchored scans.
  Six selected source guards caught by verify_guards.py -k plan: rejected
  planning, exit while inactive, restore fold, capability mode identity,
  capability relogging and spill planted-symlink refusal. The last case
  matches the selector substring; this is a focused Python mutation selection,
  not the complete guard sweep or Go mutation coverage.
- Full Python regression: 2,151 passed / 28 skipped / 24 subtests passed,
  three warnings in 93.50 s. Python runtime/package/test modules are unchanged;
  the package invariant checker does not apply to the exporter-only edit.
- git diff --check and README outline pass. Canonical Mermaid, boundary prose,
  Go guide, extension seam and parity row now describe source outcome semantics.
  The README's stale route count and unimplemented Goal API description are
  corrected to the existing 28 operations and owned goal read endpoint.
- Archify regenerated HTML from frozen JSON: 9/9 showcase checks, zero errors
  and warnings, correction_rounds=0; existing geometry/edges retained.
  Specification 36,658 bytes, SHA-256
  `a7c8d5b5fcc8c314d0fa554aa7bc991cd88cf1a7811e80601b79589f1eeb6c50`;
  artifact 678,359 bytes, SHA-256
  `a7c94adecec9bc0411aa37cad180b3c56f94ae5ba2624a74356decf735dfcd79`.
  Saved bytes match both receipts. Output docs/mini-loop-system.architecture.html.
  Visual review remains skipped following the prior local-file access denial;
  no rendered inspection or bypass is claimed.
- Go 1.23.3 darwin/arm64; Linux-host validation remains open. No dependencies
  added. No top-level docs/*.md report was added/renamed/removed; Research Atlas
  regeneration does not apply. SQLite driver approval remains pending, while
  independent port work can continue. Required gates are terminal before
  exact-path staging, commit and push.

## Implementation checkpoint — 2026-10-06 typed decisions and Jev operator library

Baseline `6f4a4b0`; P0/P1 remain complete, G0–G7 remain open. The previously
unported decision contracts and Jev backend now have a native typed operator
library. This is the first prerequisite of the explicit decision migration
sequence above; it does not substitute for the required gate/LLM/configuration,
masking/event/limiter and large-result replay integration.

### Actual source contract and native implementation

- `go/decisions` adds closed ValueKind (null/string/number/boolean/array/object),
  Question/Answer variants, Request/Result/TokenUsage and Provider. State and
  descriptions carry only the closed Value union; domain/service structs retain
  no any, interface{}, map[string]any or json.RawMessage. Open decoder tokens
  are lowered at the boundary. Test fixtures alone use raw JSON to express
  malformed inputs. Constructor/accessor copies detach nested state, rubric,
  probability, answer and usage containers.
- Source request limits are retained: 1..32 questions, choice 1..255 criteria,
  score 2..10 levels, nonempty instructions, 128 KiB compact UTF-8 JSON and
  nesting depth 32. Empty state remains valid. Result IDs/types must match;
  distributions cover the complete requested set and sum to one within 1e-5;
  choice names a maximum, and score/legend match the weighted ordered rubric.
  Noul is a finite yes probability without invented confidence. Whole usage
  can be unavailable for custom adapters; Jev requires both token counts.
- Integer versus float spelling survives wire decoding, including probabilities,
  score and confidence, without retaining raw JSON. Source float exponent/decimal
  projection and signed zero are retained in closed Values. Exact integer/binary
  float legend equality avoids rounding different large integers together and
  preserves Python's nested numeric/boolean description equality.
- Jev defaults come from the current Python source: fixed systemone endpoint,
  model alias jev-latest, total 20 s timeout and two retries. Explicit bounds are
  (0,120] s and 0..3 retries. Construction performs no network request. Only
  429/529 retry; Retry-After clamps at two seconds, with bounded invalid-header
  fallback. One context deadline covers attempts, reads and sleeps. Bodies close
  before retry and on terminal paths; reads stop after 512 KiB plus one sentinel
  byte. Redirects are disabled on private clients, actual served provenance is
  retained and transport/raw-response feedback is sanitized.
- Owned clients use a fresh transport without environment proxies and close idle
  connections after each evaluation. Injected clients are shallow-copied only to
  install redirect policy; transports remain caller-owned. Provider evaluation
  revalidates/snapshots its request and supports concurrent independent calls.
  The library does not implicitly mask, collect context or grant execution
  authority: the explicit operator caller owns those choices. Runtime gate and
  LLM composition remain pending; default tools, HTTP operations and launcher
  decision refusal are unchanged.

### Differential evidence and deliberate boundaries

- New snapshot 53 (`go/testdata/python-decisions.json`) executes 28 actual Python
  request cases, 22 result cases (including integer probability encoding), four
  UTF-8 size boundary recipes and 21 httpx isolated-transport cases. It captures
  full outbound method/URL/auth/content-type/body, served result, attempt counts,
  retry delays, safe errors and borrowed-client lifetime. Existing 52 snapshots
  remain unchanged. Fixture 148,542 bytes, SHA-256
  `152db2aae352936e762146819dd3a81fafc59e6eed9c960da55a9cdb6b1ae233`;
  source decisions.py hash is captured in the fixture.
- Native tests compare canonical full JSON shapes and scalar numeric spelling,
  not merely accepted/rejected status. They additionally exercise mutable-copy
  isolation, duplicate/invalid UTF-8/surrogate/nonfinite boundaries, unbounded
  source integer descriptions, exact numeric legend equality, deadline/cancel,
  early response read bounds, real local TLS redirect refusal and 12 concurrent
  calls through one borrowed provider. Local TLS uses the test certificate name
  only in the injected transport; production URL remains fixed. No paid endpoint
  or model is contacted.
- Go token counts use nonnegative signed-64 integers; Python accepts arbitrary
  integers. Go rejects duplicate local request keys, nonstandard NaN/Infinity JSON
  and excessive raw/ignored response nesting before domain decoding. Raw JSON
  boundary size caps at 512 KiB independently of compact request 128 KiB. Keys
  are emitted canonically; source insertion order is not promised. These stronger
  boundary checks remain explicit rather than being labeled complete wire parity.
- No native SQL or decision journal proof is claimed. Gate failures/permissions,
  masking and model/decision telemetry, shared limits, the complete current-LLM
  response contract, explicit configuration and replay retention are subsequent
  required slices. Complete feature activation remains unavailable.

### Validation and delivery evidence

- Focused go test ./decisions -count=1 -timeout=60s passes; the final full suite
  reruns after the integer/float projection correction and its source fixture.
  Full go test ./... -count=1 -timeout=180s -coverpkg=./... with profile: pass.
  Deduplicated statement coverage **88.95%** (**11,829 / 13,299**), decisions
  **86.02%** (**640 / 744**), agent **90.62%** (**5,214 / 5,754**), protocol
  **89.40%** (**1,020 / 1,141**) and HTTP **86.64%** (**707 / 816**).
  The new module expands the denominator; coverage is not port completion.
- Final full go test -race ./... -count=1 -timeout=240s and go vet ./...: pass.
  Exporter --check after the final fixture change: 53 files current.
  verify_scans.py: 19 anchored scanning guards. Python full regression:
  2,151 passed / 28 skipped / 24 subtests passed, three warnings in 79.45 s.
  Python package/runtime/test modules remain unchanged; the package invariant
  checker and source mutation rerun do not apply to this exporter/new Go-library
  slice. No Python guarded behavior or mutation anchor changed.
- README runtime baseline/canonical Mermaid and boundary prose, extension seam,
  Go guide, current plan and parity rows are updated. Source user resources
  remain pending; their matrix row is now separate from partial decision delivery.
  git diff --check and README outline pass. No dependencies were added.
- Archify regenerated the exact frozen JSON: architecture, 9/9 showcase checks,
  zero errors/warnings, correction_rounds=0, existing geometry/edges retained.
  Specification 36,675 bytes, SHA-256
  `5598c62f6d76ec76491a9411de6806e5621fd28cce8daec35c44b5d552a00b7c`;
  artifact 678,367 bytes, SHA-256
  `942ecb1d355c9c5f05834d91c653d5a58fb5ee5b273c30b631cdb177e0fd1607`.
  Output docs/mini-loop-system.architecture.html; saved bytes match the receipt.
  Visual review remains skipped following the previous local-file access denial;
  no rendered review or bypass is claimed. No top-level docs/*.md report was
  added/renamed/removed, so Research Atlas regeneration does not apply.
- Go 1.23.3 darwin/arm64; Linux-host and paid-provider audits remain open. SQLite
  driver approval remains pending; independent port work continues. Required
  gates are terminal before exact-path staging, commit and push.

## Implementation checkpoint — 2026-10-06 closed decision estimation

Pure `decisions.Estimate` validates bounded current-LLM response JSON against the
explicit request, preserves response member order for ties and probability folds,
and computes choice confidence and rubric scores. Integer score/probability
spelling is retained. Closed `Value.MapStrings` masks member names and string
values on detached copies before JSON escaping. Malformed, duplicate, nonfinite
and oversized responses fail without retaining provider text.

This library slice installs no tool. Native focused estimate tests and the final
Go full/race/vet gates cover it; runtime integration is delivered separately.

## Implementation checkpoint — 2026-10-06 opaque thinking replies

Protocol messages and direct HTTP/SSE decoding now preserve the named
`redacted_thinking` block with required opaque string data. It does not become
text or a provisional text delta. Tests cover detached access, strict malformed
wire refusal and complete direct/SSE reply round trips. This decoder slice
changes no activation or authority. Full Go/race/vet gates passed.

## Implementation checkpoint — 2026-10-06 closed decision tool protocols

The closed ToolInput variant and source decision schema now support explicit
state and choice/score/noul questions. Schema composition has named union,
constant, numeric, array and additional-property variants. Diagnostic masking
projects detached member names and values before escaping; the projection
cannot execute or acquire an action identity. Live inputs remain validated.

This protocol slice installs no tool. Its staged snapshot passes protocol tests;
the complete integration passes Go full/race/vet. Runtime activation follows in
a separate commit.

## Implementation checkpoint — 2026-10-06 gated decisions and isolated LLM

### Composition and source evidence

Explicit RuntimeConfig/ManagerServices DecisionTools installs the common
external-risk tool. A custom/Jev provider overrides the current-LLM default;
provider injection alone installs nothing. Selected custom child roles inherit
the service with a rebound session; default roles omit it. No new route,
environment activation or SQL dependency is introduced.

Supplied state/member names are masked and revalidated before execution. Custom
providers receive detached requests and share the model limiter through result
validation. The LLM query shares provider/recovery/limits and explicit peer run
provenance, but creates fresh history, tools, cache, meter and fallback state,
using Complete only. Nonfinal/tool-bearing replies are refused before recovery.
Total deadlines include permit wait/retry; contexts must be honored by providers.
Panics and unknown errors produce sanitized failures.

Named decision/custom-model event variants preserve metadata and optional whole
usage. Full masked input stays private trajectory evidence; recording projections
refuse execution/identity. Complete >4 KiB native results retain exact replay
without another provider call, with current guards and hook ordering retained.

Snapshot 54 invokes actual Python Agent, LLMDecisionProvider and common gate for
31 LLM recipes and eight gate outcomes. It pins the source schema/system prompt,
response-order tie/folds, integer probability/score spelling, model provenance,
lineage, parent isolation, structural masks and model/decision event shapes.
Native tests add manager/selected-child integration, fallback, pool contention,
cancellation/panics, real tool turns, private recording and journal replay.

### Limits and remaining work

Native ModelReply still requires complete model/usage; Python can return empty
usage. Lone-surrogate JSON is refused earlier. Unknown custom errors normalize
to RuntimeError. Derived floats compare at 1e-12; raw numeric probabilities are
retained. Zero native LLM config selects defaults, while negatives fail. Source
request map insertion order is not promised. No paid provider audit was run.

Environment/launcher decision activation remains refused. Full actual-source
large-result aggregate retention, SQL/restart and cancellation audit remain open.
Native SQLite requires the pending dependency choice. G0–G7 remain open.

### Validation

- Focused decision/estimate/opaque tests: agent, provider, protocol, decisions pass.
- Full Go: go test ./... -count=1 -timeout=180s with shared coverage; pass.
- Race: go test -race ./... -count=1 -timeout=240s; pass.
- go vet ./...; pass.
- Coverage: 12,392 / 13,895 statements = 89.18% (deduplicated blocks).
  Agent 90.54%, decisions 89.11%, protocol 90.08%, provider 85.00%.
- Python contracts --check: all 54 current; verify_scans: 19 anchored checks.
- verify_guards -k decision: one aggregate-budget mutation caught.
- Full Python pytest: 2,151 passed, 28 skipped, 24 subtests, three existing
  deprecation warnings; 103.36 seconds.
- Python invariants: not rerun; no Python package module changed (exporter only).
- Archify validate/deliver: showcase 9/9, zero errors/warnings. Specification
  8ba0d1e641fd9b20d53ea27b79f5cb7c2f94c603826b982abc442126029fbbff,
  37,078 bytes; HTML ba0f790de193b5dcf4278e7c82627b6d1466fd9c7ee9b61dc524e84226bbac24,
  678,785 bytes. Visual review skipped after prior local-file access denial;
  no bypass or visual-pass claim. Mermaid remains canonical; HTML regenerated.
- Separate staged-tree tests passed for estimation, opaque protocol/provider,
  and decision protocol plus pre-integration agent, proving commit dependencies.

## Implementation checkpoint — 2026-10-06 decision launcher selection

Go launcher now accepts the existing typed off/llm/jev settings. LLM remains
complete-only and isolated; Jev uses the fixed endpoint, configured model and
explicit Typesafe credential. Constructor/inspection makes no provider request.
FakeLLM does not replace Jev. A selected explicit Options provider wins; injecting
a backend alone does not enable an off tool. Options also exposes explicit
DecisionTools and DecisionLLM. Comprehensive features remain unsupported.

Source evidence: Python manager.py construction installs decisions only for
non-off settings and preserves an installed explicit decision backend; Settings
validation still requires the Jev key. Native launcher tests exercise seven
selection/precedence cases, redacted offline reports and complete HTTP tool
turns for LLM and Jev-mode with an explicit mock backend. No paid call is made.
Jev construction is verified separately; fixed HTTP transport already has
snapshot 53. This is not full SQL/restart or large-result source replay proof.

Validation: focused launcher and CLI decision tests pass; full Go and go vet pass.
All 54 Python contract snapshots are current. The decision aggregate-budget
mutation is caught. Archify validate/deliver passes showcase 9/9 with zero
errors/warnings; specification fb71683005ba68fe1e89cfb318f50d7862d8ea210ed7ec799908ecbebe2a8394
(37,154 bytes), HTML 7a79bfdfc3cc1489fe37ae14dacc65c94f75e48dff5110fcbd6c74602bbe8975
(678,861 bytes). Visual review remains skipped following prior access denial.
No Python package modules or scanner targets changed, so invariants/scans were
not rerun. README outline and diff checks pass. Full Go race suite passes. Full Python: 2151 passed, 28 skipped, 3 warnings, 24 subtests passed in 100.96s (0:01:40).
G0–G7 remain open.

## Implementation checkpoint — 2026-10-06 large decision replay audit

Snapshot 55 invokes actual test_decision_replay.py helpers, Agent tool execution,
InMemoryActionJournal and SQLiteStateStore. Four recipes compare exact replay
for 10,203-byte and 524,288-byte results, before/after source SQLite reopen.
Additional recipes cover five maximum payloads and decision/ordinary Unicode
character bounds, including actual SQL unknown-to-completed reconciliation.
Fixture records use lengths and canonical hashes rather than duplicating 512 KiB
payloads; source test/runtime/storage hashes are pinned.

Native common-gate tests preserve the exact first-returned bytes on fresh-session
replay, compare source canonical result hashes and refuse readonly replay.
The stored adapter is recreated over a typed test backing; this proves adapter
behavior, not Go SQLite durability. After five maximum results, both journals
retain three payloads/1,572,864 characters, keep all five identities, emit the
same bounded problem and return the shed marker without another backend call.
Terminal resettlement cannot double-count or replace the retained result. Unicode
bounds and recorded-tool reconciliation match source hashes.

No runtime implementation change was needed: current bounds and aggregate
retention match this evidence. Source SQL reopen is verified; native SQL/restart,
remaining source sink/cancellation audit and G0–G7 stay open.

Validation: focused DecisionSource tests pass; full Go tests, race suite and
go vet pass. All 55 Python contract snapshots are current; exporter returned
zero but emitted an ignored subprocess-transport cleanup warning
(Event loop is closed). The new isolated replay export has no subprocess calls
and completed without that warning. Scans: 19 anchored checks. Decision
aggregate-budget mutation: caught. Full Python suite: 2151 passed, 28 skipped, 3 warnings, 24 subtests passed in 112.94s (0:01:52). Diff check and README outline pass. No Python package
module changed, so invariants were not rerun. Runtime topology is unchanged;
interactive architecture regeneration was not required for this test-only slice.

## Implementation checkpoint — 2026-10-06 decision sinks and cancellation

Snapshot 56 invokes actual Python Agent/common gate with registered quote/Unicode
canary values and a cooperative cancelled backend. Explicit state/member names
are masked before execution. Cancellation propagates, emits a cancelled custom
model end, settles the journal as cancelled, emits neither decision_completed nor
decision_failed, and releases a one-slot model semaphore. Native tests compare
these outcomes and reacquire the shared permit under a bounded deadline.

The source negative recipe returns a raw canary as the backend model. The bare
Agent result retains the encoded secret: text masking after serialization cannot
match the escaped quote. Its raw emit callback also receives that model; this is
not a claim about source ManagedSession's separate sink masking. Go now masks
the closed validated Result before JSON escaping with the existing typed
projection encoder. The provider still returns its actual typed result; only
output/recording projection is changed. No generic domain field is introduced.
This deliberately stronger native guard closes the source output masking gap.

A complete native managed tool turn inspects decoded JSON, including nested
result strings, across next model request, live/SSE event projection, EventSink,
private trajectories, stored event/message test backing and action journal.
Remote decision requests match the source masked structure. Native SQL/restart
and live-provider audits remain open; typed test storage is not SQLite proof.

Validation: focused managed-sink and cancellation tests pass. Full Go tests and
go vet pass. All 56 source contracts are current; no transport cleanup warning
in this export. Scans: 19 anchored checks. verify_guards -k decision catches the
source aggregate-budget mutation. Additional isolated Go mutation removes the
pre-escape mask and is caught by the escaped-model sink test. Full Go race
suite passes. Initial full Python run: 1 failed/2,150 passed; the unchanged
40-turn performance test measured 0.513s against its 0.5s threshold while other
verification tasks were active. Isolated recheck: 1 passed (0.49s pytest total).
No threshold/runtime change. Standalone full-suite recheck: 2151 passed, 28 skipped, 4 warnings, 24 subtests passed in 116.88s (0:01:56). Diff check and README outline pass. No Python package module
changed, so invariants were not rerun.

Archify validate/deliver: showcase 9/9, zero errors/warnings. Specification
58fce370566218be0c553020d7172b3dd8346073db55a013007632154a7fbc26
(37,394 bytes); HTML 89598e18fde73d93c4534cceca7ebe269ef8df7fd3bf92dc5293ec9256a8eb28
(679,101 bytes). Visual review remains skipped after prior local-file denial.
Mermaid and boundary prose describe masking before escaping; default activation
and ownership stay unchanged. G0–G7 remain open.

## Implementation checkpoint — 2026-10-06 user skill contract library

The next owner-resource slices are: (1) canonical user fields and exact owner
keys, (2) owner directory isolation and layered skill/memory snapshots,
(3) create-only publication/idempotency and future-session activation,
(4) trusted manager/restore/child binding and owner-scoped serving. The original
G0–G7 scope stays open; native SQLite still awaits dependency approval.

Slice 1 is implemented in go/userresources with named SkillFields, CanonicalSkill,
ValidationCode/Error and DirectoryKey. Canonical content is immutable UTF-8;
normalized fields return value copies. Digest means canonical content SHA-256.
Name/description/body limits and validation order match source. Python whitespace
and splitlines semantics include their control/Unicode separators; wrapper
matching includes Python IGNORECASE dotted/dotless I aliases without changing
published content. Native invalid UTF-8 refusal is deliberate.

Snapshot 57 invokes actual _canonical_user_skill_parts and _owner_key, pins
user_resources.py/skills.py, and compares 42 validation/canonical SHA recipes and
ten exact owner keys. Case/whitespace/composed Unicode/path-like owners remain
distinct safe digest keys. It does not call for_owner or create directories.
No catalogue, memory, publication, session, route or activation is claimed.
Runtime topology/defaults are unchanged; README baseline/boundary prose reviewed.

Validation: focused userresources source/UTF-8 tests pass; full Go tests, race
suite and go vet pass. All 57 Python contracts are current. Scans: 19 anchored
checks. verify_guards -k skill-body-uncapped catches the source body-budget
mutation; an isolated native mutation removes the wrapper refusal and is
caught by the source-contract test. Full Python suite ran alone: 2151 passed, 28 skipped, 3 warnings, 24 subtests passed in 108.64s (0:01:48). No Python package module changed,
so invariants were not rerun. README outline and diff checks pass. Architecture
review found no new runtime topology or activation; interactive map unchanged.

## Implementation checkpoint — 2026-10-06 private owner directories

The owner-resource path layer now has OwnerID, immutable DirectoryBinding and
explicit DirectoryResolver. Configured root links resolve before creation,
including dangling targets and physical link/.. ordering. Root and digest/skills/
memory directories are tightened to 0700; planted child links and files refuse
without leaking host paths. Exact owner cache values are detached, serialized
through a context-aware permit and cached only after all paths complete.

Snapshot 58 calls actual Python UserResourceResolver/for_owner for ten directory
recipes; it records path suffixes, private modes, cache reuse, planted-link
refusals and outside permissions. Native tests compare these results and add
concurrent reuse, owner isolation, cancelled construction and cancelled cache
wait. Native root resolution bounds link traversal at 128. Cached bindings keep
source snapshot semantics. Filesystem checks are not atomic external-process
fencing; cancellation can leave uncached partial directory creation.

This completes directory binding only. Full resource snapshots, layered agent/
user skill catalogues, memory stores, publication/next-session activation, trusted
manager/restore/child binding and serving remain open. No runtime activation or
SQL dependency is added. G0–G7 remain open.

Validation: focused userresources tests, full `go test ./...`, full race suite
and `go vet ./...` passed. All 58 Python contract snapshots are current;
the exporter exited successfully with an asyncio subprocess cleanup warning
(`Event loop is closed`). All 19 source scanning guards remain anchored. Both
source private-directory permission guards and an isolated native planted-link
guard mutation were caught. Full Python: 2,151 passed, 28 skipped, 24 subtests,
three dependency deprecation warnings in 91.33 seconds. A fresh full Go coverage
run passed: 86.5% of statements overall, 86.9% in userresources (default package
coverage scope). Python coverage was not refreshed; the root `.coverage` file
predates the Python directory move and cannot report against current paths.
Python package-module
invariants were not applicable: only the contract exporter changed.

README outline and `git diff --check` passed. Archify validation and regeneration
passed all nine showcase cases without warnings. Generated HTML SHA256:
`62f3f5b19819e2ce475d2efced848fd4250b4fb7bfe04990fa80651c0fe0e1bd`
(679,600 bytes). Visual inspection remains skipped after local-file access was
denied; regeneration does not establish visual review.

## Implementation checkpoint — 2026-10-06 layered skill snapshots

Concrete LayeredCatalog binds independent agent/user Catalog construction
snapshots. Unique names resolve, collisions refuse unless source-qualified or
scoped; rendered instructions carry source/body digest. Single-layer and layered
serving share source verification. Combined descriptions preserve both headings,
reserve the largest omission receipt, consider agent entries first and cap the
whole output at 8,000 Unicode characters. Unknown-name lists are separately
bounded. Construction diagnostics copy once; later file failures stay in their
source catalogue. Native bounded logs deduplicate/count reports.

Snapshot 59 executes actual Python LayeredSkillLoader in 16 cases, comparing
exact descriptions, load result hashes and separate problem logs. Cases include
normalization/validation order, ambiguity, both-layer mutation/removal/identical
rewrite/newline-equivalence, single/both-source floods and available-name caps.
Native tests cover compiled sources, cancellation and detached diagnostic values.
No dependencies added. Owner bundles, layered runtime session/child/restore
binding, memory, publication and native SQL remain open; G0–G7 remain open.

Next owner-resource slices: typed memory-store contracts and native owner memory;
immutable resource bundles; create-only canonical publication with future-session
snapshot replacement; then trusted manager/restore/child inheritance and scoped
serving. Layered library completion does not close those activation boundaries.

Validation: focused skills tests, full `go test ./... -count=1`, full race suite
and `go vet ./...` passed. The 16 source cases contain 47 load comparisons;
all 59 generated contracts are current. All 19 scanning guards remain anchored.
The source `skill-served-after-tampering` mutation was caught; an isolated Go
mutation removing shared digest verification failed both agent/user changed-file
cases. Concurrent layered rendering/loading retains stable output and omission
counts. Full Python: 2,151 passed, 28 skipped, 24 subtests, three dependency
deprecation warnings in 80.93 seconds. The exporter emitted existing dependency/
model deprecation warnings. Package invariants were not applicable: no Python
package module changed. README outline and `git diff --check` passed.

Archify delivery passed nine showcase checks with no errors/warnings; generated
HTML SHA256 `39576aa9c479935e1d2c34ff4e49d457fa03ce777c20d881bc3054c2950b393b`
(679,744 bytes). Visual inspection remains skipped after the earlier local-file
access denial. Coverage was not refreshed in this slice.

## Implementation checkpoint — 2026-10-06 typed owner memory storage

Native memory package separates types, text/identity rules, disk Store and bound
ScopedStore. Exact owner/name pair digests prevent both tenant and same-slug
collisions. Anonymous filenames remain compatible; exact same-name/owner legacy
records migrate. Typed origins, metadata, source-style parsing, body/slug limits,
masked atomic writes with file fsync/best-effort directory fsync, deferred index,
mtime/size parse cache, bounded problems, stable lexical search and scoped/all-
owner replacement are implemented. The scoped API has no owner override or
catch-all delegation. Cancellation covers permit waits and pre-rename admission.

Snapshot 60 executes actual Python MemoryStore/ScopedMemory: 13 scenarios and
118 operations compare receipts, metadata, body hashes, index existence, exact
final file bytes and problem counts. Cases include owner whitespace/newline
identity, same-slug and CJK/index-name fallbacks, masking, scoped/all-owner
replacement, lazy legacy migration, unreadable files, external cache changes,
Unicode search, keyed imports and index/body limits. Native concurrency verifies
Alice replacement preserves Bob's 20 concurrent memories; cancellation and value
snapshot tests pass. Private native temporary/root modes and UTF-8 refusal are
stronger than standalone Python. The physical root is pinned. Imported files
retain full reads; mtime/size caches and nontransactional multi-file replacement
match source limits.

Remaining: immutable owner resource bundles, create-only skill publication and
future-session catalogue replacement, trusted manager/restore/child inheritance;
LLM memory selection/extraction/consolidation, tools/lifecycle and scoped serving.
No new dependencies or runtime activation. Native SQL and G0–G7 remain open.

Validation: focused memory differential/native tests passed; after correcting
missing search imports introduced by the file split, full `go test ./...`, full
race suite and `go vet ./...` passed. All 60 generated contracts are current and
all 19 scanning guards remain anchored. Source masking and tenant replacement
mutations were caught; isolated native removal of either guard failed the masked
or scoped-replacement source case. Full Python: 2,151 passed, 28 skipped,
24 subtests, three dependency deprecation warnings in 85.30 seconds. Exporter
warnings concern existing dependency/model deprecations. Python package-module
invariants were not applicable: only the exporter changed. README outline and
`git diff --check` passed. Coverage was not refreshed in this slice.

Archify delivery passed all nine showcase checks without warnings/errors.
Generated HTML SHA256
`f6d9867ae3583c5acea15e30aaeddb2dd4a0770edf46ba36b5433c8eee792c7f`
(679,913 bytes). Visual review remains skipped after the earlier local-file
access denial; generator acceptance is recorded separately.

## Implementation checkpoint — 2026-10-06 immutable owner resource snapshots

Concrete userresources Resolver/Resources now composes exact owner directories,
separate agent/user skill snapshots and typed mutable memory. Private fields pin
identity/path/catalogue/service bindings. Native memory is exposed through
ScopedStore directly, strengthening the source's frozen raw-store bundle seam.
Successful values are cached once; serialized failed/cancelled builds remain
uncached. Each retry rechecks owner/skills/memory paths until complete publication.
A fresh bounded operator diagnostic projection preserves resolution order,
counts and owner-local logs, and skips shared deployment problems.

Snapshot 61 calls actual Python UserResourceResolver in four source scenarios:
exact owners/cache reuse and memory isolation; later file edits remain absent
from a cached snapshot but appear with a fresh resolver; Alice/Bob local problems
remain separate while operator audit combines them; registered credentials stay
masked in memory. Native tests exercise 20 concurrent identical owner bindings,
failed construction/retry with a changed planted-link target, cancellation,
empty owner and nil catalogue refusal without side effects.

Remaining: create-only skill publication and next-session snapshot replacement;
trusted manager/restore/child binding; configuration/scoped serving; memory LLM
selection/extraction/consolidation and tools/lifecycle. No dependencies or runtime
activation added. Native SQL and G0–G7 remain open.

Validation: focused owner-resource tests, full `go test ./...`, full race suite
and `go vet ./...` passed. All 61 generated contracts are current; all 19 source
scanning guards remain anchored. The source private-owner-directory mutation was
caught; isolated native removal of cache reuse or exact owner cache selection
failed the source scenarios. Four scenarios cover 28 operations. README outline
and `git diff --check` passed. Python package invariants were not applicable:
only the exporter changed. Coverage was not refreshed.

Initial full Python run: 1 failed, 2,150 passed, 28 skipped, 24 subtests and four
warnings in 127.03 seconds. The fixed-delay interrupted-thinking test cancelled
before any thinking delta, so its precondition assertion failed. Its isolated
recheck passed in 0.28 seconds. With code/thresholds unchanged, a second standalone
full run passed: 2,151 passed, 28 skipped, 24 subtests, three dependency warnings
in 105.31 seconds. The first run additionally reported an asyncio subprocess
cleanup warning. This is observed timing variability, not proof of its cause;
no unrelated Python runtime/test change was made. Contract export emitted
existing dependency/model deprecation warnings.

Archify delivery passed nine showcase checks without errors/warnings. HTML
SHA256 `f506a9c346ee88830e2c826e40b504599993f8ed1292b7420bd4ea810f297b20`
(680,105 bytes). Visual review remains skipped after the earlier local-file
access denial.

## Implementation checkpoint — 2026-10-06 anchored publication file boundary

The source publisher requires descriptor-anchored component traversal and a
no-replace hard link. Go 1.23 Darwin's public syscall package lacks the required
at helpers, so native durable confines typed syscall wrappers/pointers to one
boundary and platform constants to separate files. Apple XNU revision
f6217f891ac0bb64f3d375211650a4c1ff8ca1ea, bsd/kern/syscalls.master, confirms
463/471/472; the installed Go 1.23.3 Darwin syscall assembly confirms the kernel
entry convention. Linux uses architecture-specific Go constants. No dependency,
cgo, path-based replacement or process-wide chdir is introduced.

CreateText uses component-wise O_DIRECTORY/O_NOFOLLOW/CLOEXEC, exact private
scratch mode, file fsync and same-descriptor linkat. Typed device/inode identity
is prepared before linking. Link success commits; post-link cleanup, directory
fsync and close cannot turn success into error. ReadBytesNoFollow verifies regular
files and reads one extra byte to detect overflow. Native NONBLOCK avoids hostile
FIFO hangs. Cancellation is checked before the commit point. Anchoring survives
parent relocation, which is not a proof of global filesystem tenancy/fencing.

Snapshot 62 executes 12 actual Python create/read scenarios: fresh/existing
file/directory/links, parent link and link/.. refusal, exact/oversized/empty reads,
file bytes, private mode, identity and scratch cleanup. Native tests add 20
concurrent attempts with one winner, cancelled creation, a parent renamed and
replaced by a symlink after opening, and FIFO refusal.

This is the necessary file boundary for the full create-only publisher. Next:
pre-link canonical catalogue construction, strict secret health screening, typed
safe publication/conflict receipts, idempotent bounded no-follow verification and
future-resolution-only resource replacement. Managed session activation and the
remaining port remain open; G0–G7 are not complete.

Validation: focused native/source differential tests, full `go test ./...`, full
race suite and `go vet ./...` passed on Darwin arm64. Linux amd64 and Darwin amd64
durable test binaries compiled; they were not executed. Three actual source file
boundary tests passed. All 62 generated contracts are current, all 19 scanning
guards remain anchored and the source owner-directory permission mutation was
caught. An isolated native removal of component O_NOFOLLOW failed parent-link
cases. README outline and `git diff --check` passed.

Full Python: 2,151 passed, 28 skipped, 24 subtests, four warnings in 153.24 seconds.
Warnings include three dependency deprecations and an asyncio subprocess cleanup
warning (Event loop is closed). Contract export also exited successfully with
that cleanup warning and existing dependency/model deprecations. Python package
invariants were not applicable: only the exporter changed. Coverage not refreshed.

Archify delivery passed nine showcase checks without warnings/errors. HTML
SHA256 `98d120fbd92127f22df69f98db4310df7ac8ee112d1698e4991e87c2b17e8b75`
(680,298 bytes). Visual review remains skipped after the earlier local-file
access denial. Darwin constants reference the official
[pinned XNU syscall table](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/bsd/kern/syscalls.master).

## Implementation checkpoint — 2026-10-06 pre-commit catalogue preparation

Actual Python UserResourceResolver.publish_skill builds and validates the entire
future snapshot, sorts its entries by source path and allocates the receipt before
atomic_create_text. This iteration supplies the native catalogue prerequisite:
Catalog.WithSourceDocument takes a trusted absolute clean SKILL.md path and a
bounded document, uses the same source reader as disk construction, rejects
malformed/truncated metadata/body and refuses name or path replacement. It returns
a separate ordered entry map and bounded diagnostic history. No file operation,
owner authority, secret screening or resolver cache replacement occurs here.

Full source digests are retained in prepared entries, so absent/changed files
cannot serve even through an operator-created snapshot. This is stronger than
the Python publisher's synthetic entry without source_digest; it intentionally
retains the existing native supply-chain invariant rather than weakening it.
Ordinary source construction and runtime defaults remain unchanged.

Fourteen valid canonical cases from actual Python snapshot 57 compare both
digests, detached prepared entries and restart descriptions/load output. Native
cases cover missing/changed sources, sorted insertion, copied diagnostic counters,
concurrent old-source refusals, malformed/oversized input, collisions, nil and
cancelled preparation. The first isolated shared-map mutation escaped because
Entries only observes the ordered slice. The strengthened test exercises the
original catalogue's lookup after file creation; the repeated isolated mutation
now fails with the intended lookup-isolation assertion. No implementation change
was needed to make the guard load-bearing.

Full create-only publication, strict secret health screening, safe typed receipts,
idempotent bounded verification and future-resolution-only cache replacement
remain next. Managed session activation and G0–G7 remain open.

Validation: focused skill tests, full `go test ./...`, full `go test -race ./...`
and `go vet ./...` passed on Darwin arm64, including reruns after the stronger
lookup assertion. All 62 Python-exported contracts are current; export completed
with dependency/model deprecations and an unknown-child-process cleanup message.
The source skill-served-after-tampering mutation was caught, as was the revised
native shared-map mutation. All 19 scanning guards remain anchored. README
outline and `git diff --check` passed. No Python package modules changed, so
verify_invariants was not applicable. Coverage was not refreshed.

Full Python ran alone after the preceding jobs terminated: 2,151 passed,
28 skipped, 24 subtests passed and three dependency deprecation warnings in
92.64 seconds. Archify delivery passed all nine showcase checks with no
errors/warnings; HTML SHA256
`6c7a6d12882414392aad55007e4fd86add998f6415bbe2da49f5bb43db376f46`
(680,519 bytes). Visual review remains skipped after earlier local-file access
denial; no bypass was attempted.

## Implementation checkpoint — 2026-10-06 create-only owner skill publisher

Actual Python UserResourceResolver.publish_skill validates canonical fields,
screens all original raw fields, refuses short/unresolved registered secrets,
rechecks the user directory and freshly catalogs disk. Native Resolver.PublishSkill
now composes that contract with immutable Resources, detached prepared catalogues
and descriptor-anchored durable file operations. Closed PublicationReceipt and
PublicationFailure expose only the source JSON keys; canonical/body digests remain
distinct. Internal owner bundles are private fields with explicit accessors.

Canonical validation precedes screening, but screening receives the unnormalized
fields. Named typed registration/health interfaces use the existing Registry; nil
and secrets.Null represent the no-secret configuration. Custom maskers lacking
Names fail closed even though Python treats missing Names as empty. Registered
values require both health readers. Mask/health panics map to safe reviewed errors;
no values/names/host errors appear in receipts. Static SkillFields makes unknown
mapping keys an ingress concern; this operator API has no invalid_fields mapping.

Publication serializes through the resolver permit and a lock-internal owner
builder. Current disk names and exact target/content hashes govern retries. Same
canonical content is idempotent, description/body changes conflict and a matching
name at another active path is not adopted. Layered collision warnings preserve
agent/user provenance. Candidate catalogues, receipts and resource bindings are
prepared before the hard link. Only unsuccessful EEXIST attempts rescan a racing
winner. Link success is followed only by cache overwrite and return: no validation
or late cancellation may revoke it. Live Resources stay pinned and memory is
reused; only future resolutions see the replacement. No cross-process cache
refresh is claimed. Prepared native entries retain source digests for later file
verification, stronger than Python synthetic entries without source_digest.

Snapshot 63 compares 16 actual Python scenarios and 27 calls: receipt/error
projections, canonical normalization, exact file hashes/modes, description/body
conflicts, ordering, owners, agent collision, each secret field, raw whitespace
secret screening, short/unresolved values, alternate active paths and file/dir
links. Fixture decoding first exposed the source's `error` key rather than the
assumed `message`; native failure projections now match. External seed permissions
are explicitly 0644 and preserved, while new publication files are 0600.
Native tests add two independent resolvers with 20 identical/conflicting attempts,
secret-surface/health panics, screening before owner writes, safe host-fault/context
projection, cancellation before/after link, restart identity and tamper refusal.

This is an operator library, not a model tool or authenticated publication route.
Next: trusted manager/fork/child/restore binding and configuration, memory lifecycle
and capture/preview/commit authority. G0–G7 remain open. No new dependency or vague
domain/service payload was introduced.

Validation: focused native publication/skill tests passed, and all 29 actual
Python publication tests passed. Full `go test ./...`, full `go test -race ./...`
and `go vet ./...` passed on Darwin arm64. Export --check reports all 63
contracts current; dependency/model deprecations remain. The first regeneration
also reported an unknown-child-process cleanup message but exited successfully.
All 19 source scanning guards remain anchored, and the source private-owner-
directory permission mutation was caught. Isolated native mutations removing
secret-health refusal and adding post-link cancellation both failed the intended
tests. README outline and `git diff --check` passed. Python package invariants
were not applicable because only its exporter changed. Coverage not refreshed.

Full Python ran alone after all preceding process handles terminated: 2,151
passed, 28 skipped, 24 subtests passed and three dependency deprecation warnings
in 86.00 seconds. Archify delivery passed nine showcase checks with no errors
or warnings. HTML SHA256
`38c7ef6c2a777c0145f7ad24fad22a8687c14e3f01792cab49228c4e1c761c25`
(680,960 bytes). Visual review remains skipped after earlier local-file access
denial; it was not retried or bypassed.

## Implementation checkpoint — 2026-10-06 managed owner resource snapshots

Actual Python manager binds owner before _build_agent, forks through create using
the current resolver generation, and rebinds recorded/scheduled restoration from
stored owner. Source teammates explicitly inherit parent skill/memory references;
in-process subagents explicitly inherit the skill harness. This iteration wires
the concrete Resolver into explicit ManagerServices.UserResources and copies one
complete owner-matching Resources through RuntimeConfig.UserResources. Validation
happens before runtime filesystem effects. Nil preserves legacy skill selection.

The shared managed construction map resolves before new/forked/restored runtime
construction. Getter values remain fixed; source cache refresh does not change
live handlers. Native selected children retain the parent's typed bundle, also
pinning scoped memory for future integrations. This is a stronger explicit native
binding than Python in-process skill-only inheritance; no memory tools/hooks are
activated by it. Default children still obey role/depth/gate policy. Go teams are
not implemented: the source teammate frame compares inherited resource binding
through a native runtime, not teammate scheduling. Session deletion preserves owner
files, and resource admission failure cannot publish a handle or retain scratch.

Runtime integration exposed a source outcome distinction: builtin missing/name/
collision/tampered skill refusals are completed `Error: ...` text, with failed=false.
Catalog/LayeredCatalog now return a named RefusalError while retaining standalone
error signatures; runtime adapts only that domain variant. Cancellation and custom
backend errors stay faults. The source probe initially listened for managed
`tool_end`; direct Agent emit instead publishes `tool_result`. After correcting
the probe, actual source events confirm the outcome flags.

Snapshot 64 executes actual Python manager/SQLite lifecycle and nine frames:
alice/bob/anonymous, old live generation, refreshed fork, inherited teammate,
recorded owner restore, missing scheduled anonymous restore and disabled legacy
source. All 36 tool outputs and failure flags, catalogue descriptions, source
memory reference reuse and durable resource retention are compared. Native Go
restore uses the existing injected test backing: native SQLite/restart remains
unproven. Additional tests validate owner/zero-bundle refusal before writes, caller
pointer detachment, failed manager admission cleanup, real qualified child tool
execution, pinned child bundles and custom backend/cancellation distinction.

Next: launcher/configuration root selection, memory lifecycle/tools, source
capture/preview/commit authority and remaining feature groups. G0–G7 remain open;
no new dependency or broad dynamic domain structure is introduced.

Validation: focused managed/resource/refusal tests, `go test ./...`,
`go test -race ./...` and `go vet ./...` passed. Contract exporter `--check`
confirmed all 64 snapshots current. Source scans confirmed all 19 guard anchors;
the skill-served-after-tampering guard passed. Three isolated native mutations
(owner match, caller detachment and refusal adaptation) were caught by their
intended assertions. Python resource lifecycle tests passed (9 tests).

After all Go/export/mutation jobs finished, the full Python suite ran alone:
2,151 passed, 28 skipped and 24 subtests passed in 92.27s, with three dependency
deprecation warnings. No Python package modules changed, so package invariants
were not rerun. README outline and `git diff --check` passed. Coverage was not
refreshed in this iteration.

Archify regeneration passed 9/9 showcase checks with zero errors or warnings.
Specification SHA-256: 96266f5467a9dc51f5154df33e735f0f03333d866922e22fe76194392f20034e.
HTML SHA-256: 0d42b938f54e077a996a579b6ab8f7c0cb9749e918130728e8fb70647249810d
(681,653 bytes). Visual inspection remained skipped after local-file access was
denied; renderer acceptance is not visual inspection.

## Implementation checkpoint — 2026-10-06 explicit owner memory tools

Base: e54d5c9. G6 advances; G0–G7 remain open. Source configuration activates
more than directory/catalogue binding, so launcher resource settings remain
refused until the memory lifecycle and related context behavior are complete.
This iteration implements the two actual memory tools as a separately selectable
runtime/managed library seam, a prerequisite for full activation.

Closed RememberInput/RecallInput variants preserve absent/null optional fields,
detach pointers, serialize sorted source replay identity and mask recording
copies. The schema and metadata match actual Python install_memory. Model inputs
have no owner/root. Source string/null unknown memory types normalize to project;
empty/null descriptions fall back to name. Non-string malformed scalar fields
are refused at the native typed boundary rather than carried as dynamic values.

Runtime MemoryTools requires a complete owner-matching ScopedStore before
workspace effects. An owner resource bundle has precedence over an explicit
legacy shared-store binding. ManagerServices.MemoryTools selects the pair with
a resolver; a missing store fails admission and reclaims unpublished scratch.
Remember uses the bound store's masked atomic Write; recall uses lexical Search
with limit five and source HTML-escaped attribute/provenance wrappers. Risk is
write/read respectively; both are exclusive and use the existing common gate.
Default role capabilities omit the pair; explicitly selected native children
retain the parent's store and their own permission mode. No memory lifecycle
side-query, extraction, consolidation, runtime-facts index or config activation
is claimed. The standalone source's lazy implicit store is not yet an activation
path; explicit library construction requires a matching store instead.

Snapshot 65 executes 14 actual Python Agent tool gates: shared-store owner
isolation, same-name overwrite, defaults/nulls, unknown string type fallback,
HTML attribute escaping/raw body, lexical hits/misses and read-only denial.
Native tests compare schemas, traits, canonical inputs, completed outputs and
failed/denied flags. Additional tests cover binding validation before writes,
foreign authority refusal, disk/result/argument masking and real selected-child
recall with the managed owner store.

Validation: focused native memory input/gate/child tests passed, followed by
`go test ./...` (agent 14.698s), `go test -race ./...` (agent 31.588s) and
`go vet ./...`. Exporter `--check` confirmed all 65 files current; previous
generated contracts remained byte-identical. `verify_scans.py` confirmed all
19 scanning guards anchored. Source mutations memory-written-unmasked,
recall-reads-every-owners-memory and remember-writes-as-anonymous were caught.
Three isolated native mutations were also caught by their intended assertions:
missing owner validation, missing child-store binding and wrong explicit origin.

After all other validation processes finished, the full Python suite ran alone:
2,151 passed, 28 skipped and 24 subtests passed in 90.28s, with three dependency
deprecation warnings. No Python package modules changed, so package invariants
were not rerun. README outline and `git diff --check` passed. No dependencies
were introduced and coverage was not refreshed.

Archify regenerated the canonical architecture artifact: 9/9 showcase checks,
zero errors/warnings. Specification SHA-256:
1c802ed52c44090cc27cba32e78b3619159fe165fd0382b1d964a26d2d8e2a6f (40,312 bytes).
HTML SHA-256: c235242311cda03a89b7a6a818b5dc2007614dd6b7feec2282d3e689088850e3
(682,112 bytes). Visual inspection remains skipped after the earlier local-file
access denial; renderer acceptance is not visual inspection.

## Implementation checkpoint — 2026-10-06 automatic memory selection/context

Base: 2c4effd. G1/G6 advance; G0–G7 remain open. Both selected memory tools
now enable automatic selection by default, matching source memory_enabled.
Explicit RuntimeConfig/ManagerServices.MemoryAuto=false disables selection;
construction copies the option and selected children inherit its value.
Carrying a resource bundle without the tool pair still enables no selection.

After prompt rewriting and before appending the user message, a scoped List
produces the source index/name/description catalogue. A 200-token memory_selection
request carries only one user message, with the last 4,000 Unicode codepoints
of rewritten input and no system/tools. Normal cache/provider/stream/recovery/
limiter/event flow remains active, including configured recovery model changes;
this is the source's ordinary side request, not the isolated decision query.
Its usage is recorded but does not observe the live conversation token meter.
Selected integer indices preserve order/duplicates and Python bool-as-int
behavior; floats and other values are ignored. Empty/invalid selections or
provider failures use scoped lexical search. Cancellation propagates; native
transcript/lease failures are preserved instead of converted to fallback.
Native JSON parsing uses standard finite JSON: source's permissive nonfinite
JSON values lead to lexical fallback at the native boundary.

Selected blocks use source provenance wrappers inside memory_context and emit
a closed MemoryEvent load/count variant. Flat serialization and archival decoding
retain informational provenance without restoring authority. The runtime-facts
message now includes a scoped nonempty memory index only when recall exists.
It is appended only on change, even when automatic selection is disabled.
Store errors propagate; no stale index is silently substituted.

Snapshot 66 executes actual prepare_memory_context / Agent._create and the source
runtime_facts injector in 12 cases: selected order, duplicate/bool/float indices,
empty/malformed/provider-failure lexical fallback, misses, empty owner store,
disabled auto, absent recall/remember, Unicode tail and extra bracket rejection.
Foreign records never enter the selection catalogue or index. Native tests
compare prepared input, actual request fields, load events, unchanged token
meter and change-only facts. Additional full-turn tests pin prompt-hook order
and main request integration; cancellation cannot append a turn or load fallback.
The existing selected-child test provider now routes by request purpose and
actual tool-result presence, because memory index injection adds a user message.

Remaining: extraction/consolidation and healthy-endpoint capture, launcher root/
default activation, capture/preview/routes, teams, native SQL and remaining groups.
Coverage is not refreshed; no new dependency or dynamic domain payload is added.

Validation: focused selection/context/full-turn/child/cancellation tests passed.
Final `go test ./...` passed (agent 11.436s); final `go test -race ./...` passed
(agent 28.078s); `go vet ./...` passed. Exporter `--check` confirmed 66 files
current; previous generated snapshots remained byte-identical. `verify_scans.py`
confirmed all 19 scanning guards anchored. Source index-shown-without-the-tool
mutation was caught. Native automatic-selection, runtime-index-change and
live-meter mutations were caught by their intended assertions.

Mutation audit: the first runtime-index mutation failed compilation because it
left changed unused; it was not counted. The repaired mutation retains a used
tautological expression and fails on repeated facts. The first live-meter
mutation escaped: the native test provider reported zero input tokens, which
Observe intentionally ignores. Its selection reply now reports the same 777
input / 3 output tokens as the actual source probe. The mutation then fails on
changed meter state. Full Go/race gates were rerun after this test repair.

After all other verification jobs terminated, the full Python suite ran alone:
2,151 passed, 28 skipped and 24 subtests passed in 84.96s, with three dependency
deprecation warnings. No Python package module changed, so package invariants
were not rerun. README outline and `git diff --check` passed. Initial exporter
generation printed an unknown-child cleanup warning and exited successfully;
the final `--check` completed successfully without that cleanup message.

Archify regenerated the canonical artifact with 9/9 showcase checks and zero
errors/warnings. Specification SHA-256:
286489e709a666d0cd010de53f3e985a74d41480918e87739123ca346c82cbcf (40,939 bytes).
HTML SHA-256: c4faade9289d055a20c455737b9984180ba5bcada3ed30bf7dcd98423e4f4562
(682,745 bytes). Visual inspection remains skipped after the earlier local-file
access denial; renderer acceptance is not visual inspection.

## Implementation checkpoint — 2026-10-07 scoped memory lifecycle

Base: 29c5fad. G2/G6 advance; G0–G7 remain open. Python install_memory
holds store.lifecycle_lock through explicit remember, and memory_on_stop holds
that same lock across extraction and consolidation. Go now supplies a distinct
Store-owned lifecycle channel, exposed through ScopedStore.WithLifecycle;
remember acquires it after the common tool gate and before scoped Write.
All bindings of a shared Store serialize, including distinct owners, matching
the source shared lock. Ordinary scoped operations retain their separate permit.
Callbacks must not recursively acquire the lifecycle. Waiting checks cancellation
before entry, and deferred release handles callback errors/panics. No rollback,
external-process fencing or independent-Store coordination is implied.

Native tests hold the lifecycle across Write/List/ReplaceAll, cancel a second
binding's waiter, verify release after callback error and independent stores,
and dispatch remember through the real gate while the lifecycle is held.
Cancelled remember writes no record; a later dispatch writes under the bound
owner. A compiling native mutation removes remember's lifecycle acquisition;
the test fails on both missing cancellation and unexpected persisted memory.
The mutant was restored before full verification.

Remaining: extraction/consolidation, healthy-endpoint capture, launcher root/
default activation, capture/preview/routes and remaining G0–G7 groups. This
small commit supplies their shared serialization seam without activating them.
Coverage is unrefreshed; no dependencies or Python package modules changed.

Validation: focused lifecycle/gated remember tests and the bypass mutation
passed. Final go test ./..., go test -race ./... and go vet ./... passed after
the panic-release assertion was added (unchanged packages reused Go test cache).
Exporter --check confirmed 66 current files; it exited successfully with an
unknown-child cleanup warning and dependency deprecation warnings. After all Go,
exporter and mutation jobs terminated, the Python full suite ran alone: 2,151
passed, 28 skipped and 24 subtests passed in 81.55s, with three dependency
warnings. No Python package module or scanner target changed, so package
invariants and scanner/anchor verification were not rerun. README outline and
git diff --check passed.

Archify generated the map with 9/9 showcase checks, zero errors/warnings.
Specification SHA-256: 680c9926ce64b62c5e371f371085126684da2d93f20e631d0c92ef33136a2102
(41,298 bytes); HTML SHA-256:
dca13ac0281e5849c4138159673328ac8b58a8a63c47f5e552214593bc5f31c4 (683,101 bytes).
Visual inspection remains skipped after the earlier local-file access denial;
renderer acceptance is not visual inspection.

## Implementation checkpoint — 2026-10-07 memory extraction stage

Base: e693bf0. G1/G6 advance; G0–G7 remain open. Actual Python extract_memories
cleans recalled memory prefixes, full runtime-state messages and every tool-result
body. It keeps thinking/tool-use blocks; the cleaned list need not be a valid
paired provider transcript because it is JSON text inside one user prompt.
Native projection retains the supported closed protocol variants and source
role/content key order. Greedy anchored prefix removal matches source; ASCII
PythonJSON gives the source last 40,000-character tail. Scoped existing names/
descriptions are the only catalogue used in the extraction prompt.

The source uses max_tokens=1500 and purpose=agent_turn with temporary messages.
Agent._create checks message identity before annotation: a side call does not
provide live recovery history or observe the live meter. Go now separates
completeSideModel from completeModel, sharing normal cache/recovery/limiter/
transport/telemetry while explicitly passing non-live history ownership. Ordinary
main turns keep their prior live-history behavior. Side usage is still recorded.

Text reply blocks concatenate without delimiters, first/last brackets delimit
the JSON array, and only the first five items are decoded and written. Decoding
is per entry: later malformed entries stop extraction with count zero while
previous writes remain, matching source. Typed Input fixes auto_extracted origin;
scoped Write fixes the owner and physical path. Unknown string types normalize
in the existing store. Extra model owner/root/origin fields are discarded.
Native null/non-string fields are refused; source can stringify some malformed
header scalars. Native finite JSON refusal is also stricter than source.
Ordinary store/model faults return zero; cancellation and native transcript/
lease errors remain errors. The lifecycle adapter must own WithLifecycle.

Snapshot 67 executes real Python Agent._create/extract_memories in 13 cases:
valid, ignored authority fields, missing defaults, unknown type, first-five cap,
partial missing-name/nonobject writes, empty/malformed/multiple-array replies,
provider fault, Unicode tail and greedy prefix. Native tests compare exact
requests, counts, files/provenance, foreign records and live meter/history.
Additional tests pin recovery's absent live history, cancelled requests and
native authority faults. A test provider initially omitted required native reply
ID/type/role; it was repaired to use the existing valid fakeReply constructor,
without weakening reply validation or expected source results.

The source tool-result-reingestion mutation fails the existing hygiene assertion.
Compiling native mutations granting side calls live history/meter and retaining
tool-result bodies fail their intended native differential/recovery assertions.
All mutations were restored before final gates. Existing 66 exports remain
byte-identical. No dependencies or dynamic domain/service fields are introduced.
Remaining: consolidation, contained healthy-endpoint capture, launcher root/
default activation, capture/preview/routes and all remaining plan groups.
The stage is implemented but not yet invoked at automatic turn endpoints.
Coverage is not refreshed.

Validation: focused extraction differential, recovery history, cancellation and
authority tests passed. Full go test ./... passed (agent 9.837s), go test -race
./... passed (agent 36.010s), and go vet ./... passed. Exporter --check confirmed
67 files current; verify_scans.py confirmed all 19 scanner guards anchored.
Exporter generation initially printed an unknown-child cleanup warning and
exited successfully; the final --check completed without that message.
All Go/exporter/mutation/scanner jobs terminated before the Python suite ran
alone: 2,151 passed, 28 skipped, 24 subtests passed in 76.36s, with three dependency
deprecation warnings. No Python package module remains changed, so package
invariants were not rerun. README outline and git diff --check passed.

Archify regenerated the map with 9/9 showcase checks and zero errors/warnings.
Specification SHA-256: a640f85bce52aafee5e9c790ae60c5a3a86a4932084ae5d414ef212bf5cfe1c3
(41,625 bytes); HTML SHA-256:
b3a08bef87919960d164e8c08ea0220d74742c910335c6201953efb251095c94 (683,428 bytes).
Visual inspection remains skipped after the earlier local-file access denial;
renderer acceptance is not visual inspection.

## Implementation checkpoint — 2026-10-07 consolidation and end-of-turn capture

Base: 5f72738. G1/G6 advance; G0–G7 remain open. Scoped consolidation now starts
at ten records. It sends source lowercase ordered record fields in non-ASCII
PythonJSON with max_tokens=2500 and purpose=memory_consolidation through the normal
non-live side path. Nonobjects/missing names are skipped; empty/unusable outputs
leave storage unchanged. Exact name/type/description/body identity retains the
original origin; changed/new entries are consolidated. Typed Input ignores model
owner/root/origin overrides and ScopedStore.ReplaceAll confines replacement to
the owner. The source multi-file replacement remains nontransactional.

Automatic capture now holds the Store-owned lifecycle across extraction and
consolidation. It requires both tools, MemoryAuto and non-readonly posture. Native
three endpoints mirror actual Python calls: normal final after final text, stuck
halt after a stop-hook continuation, and round exhaustion after its error marker.
Provider error and cancellation exits initiate no capture. Actual source inspection
found the tool-batch stuck return has no capture call, despite the hardening note's
broad wording. Snapshot 69 measures that omission and Go preserves it explicitly.
There is no claim that every possible stuck endpoint captures in either runtime.

Ordinary lifecycle faults are contained with one bounded memory_capture_error;
the completed result is retained. Source extraction/consolidation model and write
faults return zero, while consolidation List (outside its try block) reaches this
containment. Cancellation and native transcript/lease errors propagate. The
memory/extract closed variant requires count and consolidated count; load omits
the latter. Event clones/accessors detach pointers, masking protects error detail,
and flat serialization/archival decoding restores informational data only.
Native error class formatting differs from Python; both bound detail to 200
characters before projection masking. Default memory tools/launcher remain off;
explicit tools enable the source automatic lifecycle by default.

Snapshot 68 compares ten actual Python consolidation cases: threshold, unchanged/
changed/new origins, nonobject/missing-name skips, empty/unusable/malformed replies,
provider fault, unknown type normalization and omitted defaults. Snapshot 69 runs
real source Agent turns for eleven paths: normal, exhaustion, continuation halt,
tool halt omission, provider error, readonly, disabled, missing pair, cancellation,
contained List failure and extraction crossing the consolidation threshold.
Native tests compare request budgets/purposes, record/provenance effects, foreign
owners, events and archival projections. Additional tests pin held lifecycle during
model await, cancelled explicit remember, cancellation release/no-success event,
detached event pointers, known variants and registry masking.

Initial native cancellation double returned context.Canceled with a still-live
context; the ordinary provider-error branch contained it, as its contract allows.
The double now cancels the caller-owned context before returning, matching the
actual Python CancelledError signal. Expected source cancellation is unchanged.
The older selection/full-turn test now expects the source third extraction request
and verifies its 1,500-token/no-system/no-tools shape after selection and main turn.
Existing 67 exports remain byte-identical. No dependencies or dynamic domain/
service fields are added; native malformed scalar/null and finite JSON restrictions
remain as documented in the extraction stage.

Source guards for exhaustion-skips-memory, capture-failure-kills-the-turn and
consolidation-wipes-every-tenant were all caught. Compiling native mutations for
exhaustion omission, fatal capture error, unscoped replacement and lost unchanged
origin were caught by their intended assertions and restored before final gates.
Remaining: launcher resource/root selection, skill capture/preview/HTTP routes,
teams/native SQL/other groups and the full G0–G7 audit. Coverage is unrefreshed.

Architecture scope: README canonical Mermaid explicitly connects writable automatic
capture from Session to filesystem memory. The interactive artifact retains a
component-level overview, with capture's direct writes and posture/lifecycle
boundaries in the Session/filesystem explanation. Adding that detailed edge to
the overview first produced five direction/crossing diagnostics; a routed candidate
reduced them to four crossing/label diagnostics. The overview omits that redundant
detailed connector and retains its source-grounded component explanation, rather
than accepting overlapping routes. No renderer internals or generated HTML were
edited. Final generation evidence follows.

Validation: focused memory/consolidation/capture/lifecycle tests passed. Full
go test ./... passed (agent 9.960s); go test -race ./... passed (agent 34.231s);
go vet ./... passed. Three selected source guards and four compiling native
mutations were caught and restored. Exporter --check confirmed 69 current files;
verify_scans.py confirmed all 19 scanner guards anchored. The exporter exited
successfully but printed an ignored BaseSubprocessTransport destructor warning
with Event loop is closed; the contract comparison itself completed. All Go,
exporter, scanner and mutation jobs terminated before the full Python suite ran
alone: 2,151 passed, 28 skipped, 24 subtests passed in 84.12s, with three dependency
deprecation warnings. No Python package module changed, so package invariants
were not rerun. README outline and git diff --check passed.

Archify generated the overview with 9/9 showcase checks, zero errors/warnings.
Specification SHA-256: 360f677df012be2f63acfa1e866a2720c4cecfe24f347c129453c31c8f350076
(42,250 bytes); HTML SHA-256:
63c8259693ef4d2a10dc4bb6acadde0bf881a81ea68797a513bd5af677f26058 (684,056 bytes).
Visual inspection remains skipped after the earlier local-file access denial;
renderer acceptance is not visual inspection.


## Implementation checkpoint — 2026-10-07 shared manager memory fallback

### Source contract and native composition

Python manager construction retains one shared MemoryStore (manager.py:445-448).
_build_agent selects an owner resource bundle when configured and otherwise uses
that shared store (manager.py:657-674); runtime access binds resource_owner.
Go ManagerServices.Memory now exposes the concrete shared Store. The existing
common managedRuntimeConfig composition selects UserResources first and otherwise
binds the admitted owner through memory.Bind. Resolver failures remain admission
failures; they cannot expose the fallback. Create, completed-boundary fork,
ordinary restoration and scheduled restoration all use the same composition.
No model owner/root override, dependency, environment setting or activation
change was added. Nil keeps the previous no-memory embedding behavior; providing
storage alone preserves the default-off memory tool catalogue. Launcher root
construction/selection is the next separate slice.

### Evidence and remaining scope

Focused tests exercise remember/recall through the real execution gate for Alice,
Bob and anonymous; forks share Alice's records and cannot recall other owners.
The Markdown store is reopened before ordinary and lazy scheduled restoration.
Missing scheduled metadata retains source anonymous fallback. State restoration
uses the existing injected test backend: this is not native SQLite restart proof.
A separately populated owner resolver wins over legacy shared storage. Source
memory disk/tool contracts remain the existing snapshots; no contract changed.

Final Go gates passed: go test ./... (agent 19.952s), go test -race ./...
(agent 39.400s) and go vet ./.... Three compiling isolated native mutations were
caught: shared owner changed to anonymous, shared store overriding resolver,
and resolver failure falling back to shared storage. The selected source recall
and remember owner-isolation guards were both caught. Exporter --check confirmed
all 69 contracts current; it exited successfully while printing the existing
ignored asyncio subprocess teardown warning (Event loop is closed) and an
Unknown child process diagnostic. No exporter, scanner target, Python package
module or source guard anchor changed; scanner/package-invariant gates were not
rerun. README outline and git diff --check passed.

After all Go/exporter/mutation jobs terminated, the isolated full Python suite
first returned 1 failed, 2,150 passed, 28 skipped and 24 subtests in 145.03s.
The failure was the existing forty-turn wall-clock assertion: 0.5567s against
its unchanged 0.5s limit. The focused test then passed. A second isolated complete
run passed: 2,151 passed, 28 skipped, 24 subtests, three dependency deprecation
warnings in 132.59s. No implementation or threshold changed between these runs.
The timing variability remains recorded; coverage percentages were not refreshed.

Archify delivered the unchanged-layout overview with 9/9 showcase checks and zero
errors/warnings. Specification SHA-256:
71a18d0467fdb3d513bf8e679bfbedbc39b34bfe311ae5400419d11083abf4fd
(42,632 bytes); HTML SHA-256:
c47758dc980bc54f0155fac6545f90966e0cdd2762b06093cac954db62a5d5bc
(684,438 bytes). Visual inspection remains skipped after the earlier local-file
access denial; renderer acceptance does not establish visual review.


## Implementation checkpoint — 2026-10-07 launcher memory roots and selection

### Source contract and implementation

Snapshot 70 measures actual Python manager construction and default registries
across seven root configurations, including regular-file failures. Startup
constructs one shared Store at MemoryRoot or WorkspaceRoot/.memory even with
UserResourcesRoot configured, then optionally creates a private root Resolver
with the selected agent catalogue. The manager binds owners and retains resolver
precedence. Root failure refuses startup and closes owned provider connections;
open-bind/model/configuration refusal still precedes root effects. Construction
may retain earlier filesystem effects if a later root fails, matching source.
Owner children remain lazy. No dependency was added.

Options.MemoryTools and --memory-tools explicitly install remember/recall;
default startup preserves the source default catalogue without memory tools.
Options.MemoryAuto is copied by the manager and defaults true with the tool pair;
--memory-auto=false opts out. Context index exposure still follows recall.
Named MemoryBackend values and detached boolean choices appear in pure Inspect/
dump-config without storage creation or provider calls. Roots no longer appear
as unsupported settings. Comprehensive MINILOOP_FEATURES remains refused.

### Evidence and remaining scope

Native fixture tests compare all seven constructor outcomes, paths, root effects
and permissions. Real mocked HTTP provider requests verify default/configured
shared and owner-local roots, scoped Alice/Bob/anonymous recall, resolver
precedence, retained auto=false and default tools off. CLI dump compares flag/
root selections without startup, while retaining comprehensive-feature refusal.
User-skill capture/preview/HTTP routes, SQL backend/restart and remaining G0-G7
requirements remain open. Coverage percentages were not refreshed.

Validation passed: focused launcher/CLI/configuration tests, go test ./...
(launcher 2.466s), go test -race ./... (launcher 3.882s) and go vet ./....
Exporter --check confirmed all 70 files current; the original 69 fixtures remain
unchanged. It exited successfully with dependency/model deprecation warnings and
an Unknown child process diagnostic. verify_scans.py confirmed all 19 scanner
guards anchored. The selected source private-root guard was caught. Six compiling
isolated native mutations were caught: wrong default root, skipping shared root
with owner resources, ignoring owner resolver, memory tools default-on, dropping
the auto override and bind refusal after root effects. No native mutation timed
out or failed to compile. All these jobs were terminal before the full Python
suite ran alone: 2,151 passed, 28 skipped, 24 subtests passed, three dependency
deprecation warnings in 128.47s. No Python package module changed, so package
invariants were not rerun. README outline and git diff --check passed.

Archify delivered the overview with 9/9 showcase checks, zero errors/warnings.
Specification SHA-256:
ec1f815d5c30dec35635ca9888d75a6d79125dd15ec50072cd8ea0835107a5af
(43,113 bytes); HTML SHA-256:
04a37d07daa56e76e429c2b6e2daad18a249354fb00816941aef60723bcf6628
(684,919 bytes). Visual inspection remains skipped after the earlier local-file
access denial; renderer acceptance is not visual inspection.


## Implementation checkpoint — 2026-10-07 bounded personal-skill draft store

### Source contract and native implementation

The actual skill_capture.py PersonalSkillDraftStore defines private owner/session
bindings, four coverage variants, source canonical field normalization/digest,
FIFO quota replacement, non-consuming lookup and post-publication identity cleanup.
Snapshot 71 measures 40 operations plus defaults. Native userresources.DraftStore
ports this complete storage boundary as an explicit operator library, with no
runtime activation. Default configuration retains 15 minutes / 64 total / 16 per
owner / four per session. Go config uses positive time.Duration and typed counts;
configuration errors are named safe errors rather than source ValueError. Initial
cryptographic entropy failure is masked as a stable 500 before quota mutation.

Draft has a private immutable record pointer; Preview returns only public fields
and detached evidence. Get/Peek require exact owner/session before checking TTL
or optional digest; failures retain the source 404/410/409 ordering. Quotas evict
FIFO within session/owner and never cross owner to meet global capacity. Consume
is atomic and one-shot. DiscardCommitted checks exact object identity without TTL
so successful publication need not become an ambiguous expiry failure. The future
commit flow must Peek, publish reviewed fields, then discard; it must not consume
before a possibly failing publication. Evidence integers may be signed/duplicated
at this storage layer; candidate message-bound validation remains a separate seam.
UUID randomness normalizes only in comparison fixtures; native IDs are UUID4 hex.
No dependency or filesystem/model effect was added.

### Evidence and remaining scope

Native tests replay all 40 source operations and compare canonical fields/digests,
timestamps, code/status/message, FIFO ordering, authority/expiry and cleanup.
Additional tests mutate returned/input evidence, inspect public JSON, clone an
identity handle, check copied limits and race 32 consumers for one winner.
Next slices: provenance-gated admitted-turn ledger and bounded projection;
strict candidate parsing/screening; locked non-live preview requests; trusted
manager binding and authenticated preview/commit routes. G0-G7 remain open.
Coverage percentages were not refreshed.

Validation passed: focused draft tests; go test ./... (userresources 1.663s,
agent 16.245s); go test -race ./... (userresources 2.534s, agent 32.741s);
go vet ./.... Exporter --check confirmed 71 current files, with all earlier 70
fixtures unchanged; dependency/model deprecation warnings were nonfatal.
verify_scans.py confirmed all 19 scanner guards anchored. Seven compiling native
mutations were caught without timeout/build failure: cross-session lookup,
skipped digest, foreign global eviction, cleanup by ID alone, cleanup rejecting
elapsed TTL, mutable preview evidence and repeat consumption. No source guard
anchor or Python package module changed; source mutation/package-invariant gates
were not rerun. All Go/exporter/scanner/mutation jobs terminated before the full
Python suite ran alone: 2,151 passed, 28 skipped, 24 subtests, three dependency
warnings in 83.95s. README outline and git diff --check passed.

Archify delivered 9/9 showcase checks with zero errors/warnings. The canonical
Mermaid shows the unbound native draft library separately; the component overview
records its contract without inventing a runtime/publication edge.
Specification SHA-256:
6b48fae6eaa0cb54817441b0d88ce7c7c42b43f357b753bd73b2d3c25aef8097
(43,677 bytes); HTML SHA-256:
472bab7b707e303ebf28c63d94bc92577f482afbd8b2dfd88f6be837bc419c0e
(685,483 bytes). Visual inspection remains skipped after the earlier local-file
access denial; renderer acceptance does not establish visual review.


## Implementation checkpoint — 2026-10-07 pure skill evidence projections

### Source contract and implementation

The four skill_capture.py projection helpers are ported through typed native
input/output records. Legacy ProjectSessionText removes recalled memory through
the final legal closing wrapper, refuses malformed leading wrappers, unwraps
whole interjections and excludes internal markers. It drops complete user arrays
and includes cleaned assistant text blocks alone. Compaction/snipping detection
sets the exclusion flag even when the message is omitted. Pure authenticated
projection keeps nonempty already-admitted text verbatim, carries prior omitted
counts and an explicit excluded-history flag. It does not mint provenance.

Source Unicode whitespace, dotted/dotless i IGNORECASE and Unicode 14 word
boundaries are explicit. The existing Unicode exporter now also generates the
word classification table. Both projections mask fixed strings/keys before
fitting a whole-message suffix under sorted-key compact Unicode JSON character
cost, including brackets/commas and source escaping. The cap remains 40k; no
partial content is returned. Source key collisions/role redaction are preserved
through fixed private key labels and a transient JSON-boundary string map.
ProjectionLabel is display data rather than protocol authority. Native invalid
limits/UTF-8 use safe typed errors; malformed dynamic protocol forms reject at
existing native boundaries. No dependency, model/file effect or runtime default
changed. The source ledger recorder is intentionally a subsequent binding slice,
not a different implementation of the projection helpers.

### Evidence and remaining scope

Snapshot 72 compares 38 actual source cases and whole-result JSON hashes, counts,
coverage, omissions and flags. Budget recipes use deterministic repetition rather
than large duplicated fixtures. They include greedy/malformed wrappers, internal
prefixes, interjections, user arrays, assistant blocks, Unicode i/whitespace/word
boundaries, recursive field/key masking, exact/short/tiny/capped budgets and
Unicode/control-escape cost. Native tests add input/output detachment, positive
limits and invalid UTF-8. Next: admitted-turn ledger capture and screening; strict
candidate parser; locked non-live preview; manager injection and authenticated
preview/commit routes. G0-G7 remain open; coverage was not refreshed.

Validation: targeted projection tests, full `go test ./...`, full
`go test -race ./...` and `go vet ./...` passed. Seven compiling mutations
were caught: early memory close, user arrays, byte budgets, omitted metadata,
ASCII word boundaries, missing key masking and missing list brackets. The
contract exporter confirms all 72 files current; the Unicode exporter check
passes against Python Unicode 14.0.0. All 19 scanning guards are anchored.
The isolated full Python suite passed: 2,151 tests, 28 skipped, 24 subtests,
three existing deprecation warnings (85.79s). `git diff --check` and the README
outline pass. Archify regeneration passes 9/9 checks with zero errors/warnings;
visual inspection remains skipped after the earlier local-file access denial.
No Python package-module or guarded Python mutation-anchor changes occurred.
Coverage was not refreshed; G0-G7 remain open.

## Implementation checkpoint — 2026-10-07 admitted-turn skill evidence capture

Base: 4fa42e3. G1/G6 advance; G0-G7 remain open. The real source recorder
record_personal_skill_turn and AgentSession._run terminal ordering are implemented
through a typed process-local CaptureLedger owned by ManagedSession. Success
after terminal flush and the trusted capture capability are required; HTTP
admission already stamps it. Ordinary/peer/cron turns and cancellation do not
gain evidence. Restored/forked histories have fresh ledgers.

Source trimming, recursive display-field/key masking, 64 messages and 40k compact
Unicode JSON characters are preserved. Single-message FIFO eviction increments
omissions and can split pairs. Plain history gaps set sticky exclusion; block
arrays do not. Short/unresolved, missing or panicking health reports refuse new
evidence and latch screening failure. Healthy later capture does not clear the
latch. Snapshots detach, preserve fixed key collisions and distinguish an absent
ledger from an established empty ledger; projection interprets canonical source
fields and refuses capture errors with capture_source_unavailable/503. Typed
storage excludes dynamic corrupt-list/omitted cases; native masking faults and
invalid UTF-8 use safe capture_failed without leaking exception text. Optional
recording cannot change a completed turn into failure. No dependency or durable
state/default feature activation changed.

Snapshot 73 compares 20 actual Python recorder recipes with state and projection
hashes, including message/Unicode/escaped budgets, split-pair eviction, source
whitespace, recalled/internal-looking human input, plain/block/Unicode history
gaps, field/key/role masks, collisions, unresolved/short registries and sticky
recovery. Native tests add concurrent snapshots, masking panic and invalid UTF-8;
managed tests cover capability loss in peer derivation, ordinary runs, masked
admitted pairs, cancellation, screening recovery and terminal ordering. Actual
HTTP normal and streaming calls capture once; idempotent replay and foreign
requests cannot append. Candidate parsing, locked non-live model preview, manager
draft injection and authenticated preview/commit routes remain next. Coverage
was not refreshed.

Validation: targeted recorder/projection, managed admission/terminal and real
HTTP masking/idempotency tests passed. Full `go test ./...`, full
`go test -race ./...` and `go vet ./...` passed. Nine compiling mutations were
caught: capability bypass, screening bypass, clearing the latch, byte budgets,
message count, omitted count, masked-key interpretation, history gaps and capture
before terminal commitment. The source exporter confirms all 73 files current;
all 19 scanning guards are anchored. After every Go/exporter/scanner/mutation
job was terminal, the isolated full Python suite passed: 2,151 tests, 28 skipped,
24 subtests, three existing deprecation warnings (81.02s). `git diff --check`
and README outline passed. Archify regeneration passes 9/9 checks, zero errors
and warnings; visual inspection remains skipped after the earlier local-file
access denial. Python package modules and source mutation anchors were unchanged,
so package-invariant and source-guard mutation verifiers were not rerun. Coverage
was not refreshed; G0-G7 remain open.

## Implementation checkpoint — 2026-10-07 typed skill candidate parsing

Base: 0f2c268. G0/G6 advance; G0-G7 remain open. The complete source
_parse_candidate contract now has a native typed parser. Exact five-field schema
checks precede recursive masking-change refusal, then schema version, decision,
string fields, integer evidence, skip/create rules and canonical skill validation.
Boolean, float and exponent evidence are rejected as source non-integers; skip
requires empty fields/evidence, even for arbitrary-size integer indices, and does
not validate the requested name. Create requires nonempty unique in-range indexes.
Original description/body and evidence order are retained; normalization belongs
to publication. Immutable result accessors detach evidence and named errors reveal
no raw output. No any/RawMessage payload is retained in domain/service records.

Transient JSON-boundary maps/arrays preserve source duplicate-key last-wins,
including overridden sensitive values. A lexeme pass preserves NaN/Infinity/
-Infinity invalid-type outcomes without nonfinite domain data and pins the source
default 4,300-digit integer ceiling. Quoted literals/backslash escapes and paired
surrogates remain intact. Native UTF-8/lone-surrogate input refuses malformed_json
before lossy decoding (Python strings can retain lone surrogates); native masker
panics return safe masking_unavailable. Health checks remain the preview caller's
responsibility, not an additional parser gate. No dependency, model call, runtime
binding, route, publication authority or default changed.

Snapshot 74 compares 63 actual Python parser outcomes and accepted-result hashes.
It covers structural failures/ordering, nested/fixed-key secrets, skip semantics,
arbitrary integer/float/bool indices, duplicates/bounds, skill limits/controls,
source original Unicode/newline fields, source JSON duplicate semantics, nonfinite
values, numeric digit limits, valid surrogate pairs and literal backslash-u.
Large bodies/numbers use repeat recipes rather than duplicated fixtures. Native
tests verify detached evidence, invalid encoding and contained masker faults.
Next: locked non-live model preview and retries, manager draft injection,
authenticated preview/commit routes and final resource lifecycle audit. Full
Go port and coverage refresh remain open.

Validation: all 63 actual source parser cases and native boundary tests passed.
Thirteen compiling mutations were caught: exact fields, secret strings/keys and
nested arrays, empty skip, index bounds/duplicates, canonical skill validation,
evidence detachment, nonfinite types, integer digit limits, masking-before-schema
ordering and skip evidence types. Full `go test -race ./...` and `go vet ./...`
passed. The first concurrent full `go test ./...` failed the existing background
tail recipe's eight-second Wait deadline; the focused tail rerun passed (1.039s)
and full `go test ./...` rerun passed (background 2.770s, other successful packages
cached). No background implementation or test threshold changed. The exporter
confirms all 74 files current; all 19 scanning guards are anchored. After every
Go/exporter/scanner/mutation job finished, the isolated full Python suite passed:
2,151 tests, 28 skipped, 24 subtests, three existing deprecation warnings (107.51s).
`git diff --check` and README outline pass; Archify regeneration passes 9/9 checks,
zero errors/warnings. Visual inspection remains skipped after the earlier local
file access denial. Python package modules and source mutation anchors were not
changed, so package-invariant/source-guard mutation verifiers were not rerun.
Coverage was not refreshed; G0-G7 remain open.

## Implementation checkpoint — 2026-10-07 typed skill preview business flow

Base: 68364c8. G0/G6 advance; G0-G7 remain open. Source preview_personal_skill
now has a typed SkillPreviewer port through an explicit model interface and
DraftStore. Name/owner/sensitive-name ordering, capture-error refusal, presence
selection of authenticated ledger (including an empty ledger), standalone legacy
fallback and empty-transcript refusal precede requests. Focus is masked before
its 2,000-Unicode-character cap; secret health precedes generation. Source system
text and 2,500 output tokens are fixed; request prompt is immutable data.

Two attempts preserve recursive masking and sorted compact Unicode JSON. Repair
adds a safe candidate reason to a fresh payload, never previous model output.
Provider exceptions fail provider_failure/502; cancellation passes through.
After valid parsing, secret health is rechecked before preview_skipped/422 or
bound draft retention. Failed/cancelled operations retain no draft. Closed Value
JSON maps exist only at the transient payload boundary. Native masker/provider
panics use safe 503/502 errors and explicit context deadlines propagate. No vague
payload is retained in service/domain records; no dependencies/defaults changed.

Snapshot 75 compares 29 actual source flows, every model boundary request hash
and accepted normalized fields/digest/coverage/omissions. It covers legacy and
authenticated evidence, old-message eviction, strict empty-ledger behavior, gaps,
name/owner/health ordering, focus Unicode cap, fixed/nested key masks, malformed/
sensitive/evidence repair, two-invalid exhaustion, skip, post-call health changes,
provider failures/cancellation and session validation after successful generation.
Native checks add default store binding, panic containment, post-model context
cancellation and no failed retention. The model interface requires adapters to
extract response text blocks and preserve source normal recovery/cache/limiter/
telemetry with non-live history/meter ownership, tools empty and preview purpose.
Native-session adapter, manager admission/lease and draft injection, authenticated
preview/commit routes and final lifecycle audit remain next. The service grants
no publication authority. Coverage and the complete Go port remain open.

Validation: all 29 actual source preview flows, request hashes, retained draft
fields and native panic/cancellation/no-retention checks passed. Thirteen
compiling mutations were caught: ledger presence, sensitive name, focus/output
bounds, attempt count, repair payload, root masking, provider failure, skip,
cancellation, omission retention and pre/post-model secret health. Full
`go test ./...`, full `go test -race ./...` and `go vet ./...` passed. The exporter
confirms all 75 files current; all 19 scanning guards are anchored. Regeneration
emitted a transient asyncio child-reaper warning; the fresh exporter check matched
every file and no earlier fixture changed. After every Go/exporter/scanner/
mutation job finished, the isolated full Python suite passed: 2,151 tests,
28 skipped, 24 subtests, three existing deprecation warnings (80.86s).
`git diff --check` and README outline pass; Archify regeneration passes 9/9 checks
with zero errors/warnings. Visual inspection remains skipped after the earlier
local file access denial. Python package modules and source mutation anchors were
unchanged, so package-invariant/source-guard mutation verifiers were not rerun.
Coverage was not refreshed; G0-G7 remain open.

## Implementation checkpoint — 2026-10-07 standalone native skill preview adapter

Base: 6c596e8. Snapshot 76 captures six actual Python Agent model-boundary
flows: valid, repair, skip, ignored tool block, empty refusal and provider error.
Explicit Session.PreviewPersonalSkill now serializes with core turns, derives
owner/session from runtime configuration and retains a per-session previewer.
The native adapter uses completeSideModel, fixed system/output bounds, empty tools
and personal_skill_preview purpose. It joins only text blocks; normal configured
cache/recovery/shared limiter/telemetry remain. Live history and token meter are
unchanged; recovery receives no live history. Empty supplied provenance ledger
never falls back. Cancellation while admitted, waiting for the model pool or
after a valid model reply leaves the session reusable and retains no draft.
Manager admission/lease/cancellation and authenticated publication routes remain
next; G0-G7 remain open. No dependencies/defaults changed.

Validation: actual source requests, outputs and model events plus native recovery,
cache, admission/shared-limiter cancellation, empty-ledger and post-model
cancellation/reuse checks pass. Six compiling mutations are caught: purpose,
output tokens, system text, text-block joining, owner binding and live-history
ownership. The initial text-join mutation survived because the split fell between
JSON fields; splitting inside a string closed that test gap, with actual Python
source results unchanged. Full Go tests/race/vet pass; the changed tests also pass
focused race checking. Exporter check confirms 76 files current. All 19 scanning
guards are anchored. Exporter shutdown emitted an asyncio transport cleanup
warning after source execution; every contract still matched and the process
exited zero. Python package modules and source mutation anchors are unchanged;
package-invariant/source-guard mutation verifiers were not rerun.

Cross-package coverage refreshed with go test -coverpkg=./... -coverprofile:
deduplicated statement coverage 89.32% (14,177 / 15,873), agent 90.67%
(5,812 / 6,410), HTTP 86.52% (706 / 816), protocol 90.09% (1,227 / 1,362),
userresources 91.28% (796 / 872). These measure tested statements, not completion
of the remaining port groups. README outline and git diff --check pass. Archify
regeneration passes 9/9 checks with zero errors/warnings; visual inspection remains
skipped after the earlier local-file access denial.

After all Go/exporter/scanner/mutation jobs reached terminal state, the isolated
full Python suite passed: 2,151 tests, 28 skipped, 24 subtests (126.34s), with
three deprecation warnings and one asyncio subprocess transport cleanup warning
in test_owner_map_bound. No source test/runtime behavior was changed to suppress
these warnings. Final git diff --check passes.

## Implementation checkpoint — 2026-10-07 manager-owned skill draft storage

Base: 5b6c0ce. Manager construction now owns one typed process-local DraftStore
with source defaults and injects it through the common create/fork/ordinary and
scheduled restoration runtime factory. Native preview adapters retain into that
pool; standalone sessions with no manager injection still use independent stores.
The binding is internal, with no arbitrary state payload or new model authority.
Manager restart starts an empty pool, independently of restored conversation state.
Snapshot 77 compares seven actual Python manager frames and old-draft absence
after source SQL restoration. Native restoration uses the injected test backend;
no native SQLite backend/restart claim follows. Native checks cover global
64-item capacity across sixteen sessions, no foreign eviction, adapter retention
and owner absence. Manager preview admission/lease/cancellation, authenticated
preview/commit routes and final lifecycle audit remain next; G0-G7 stay open.

Validation: seven actual source frames and restart draft loss, native adapter
retention/owner absence and sixteen-session global capacity checks pass. Four
compiling mutations are caught: manager factory injection, runtime binding,
adapter store selection and global capacity. Full Go tests, full race and vet
pass; exporter check confirms 77 files current and all 19 scanning guards remain
anchored. Regeneration/check emitted asyncio subprocess cleanup warnings; all
contracts matched and exit status was zero. Existing fixtures are unchanged.
README outline and git diff --check pass. Archify regeneration passes 9/9 with
zero errors/warnings. Visual inspection remains skipped after earlier local-file
access denial. Python package modules and source mutation anchors are unchanged;
package-invariant/source-guard mutation verifiers were not rerun. Coverage was
not refreshed this slice; last refreshed coverage is recorded in the previous
checkpoint. After all Go/exporter/scanner/mutation jobs reached terminal state,
the isolated full Python suite passed: 2,151 tests, 28 skipped, 24 subtests,
three existing deprecation warnings (78.34s). Final git diff --check passes.

## Implementation checkpoint — 2026-10-07 owned manager skill preview

Base: ec7c2a1. Manager PreviewPersonalSkill now checks exact owner, anonymous
refusal and publication configuration before admission. Identity/accepting state
is rechecked under manager/session locks after acquisition. Lease ownership is
required before projection and a bound cancellation cause maps lease loss to
safe session_lease_lost/409, including model-time loss. Managed callers always
supply the capture ledger, including empty state; legacy/restored history cannot
mint evidence. Source readonly previews are permitted. Snapshot 78 compares
eight actual manager outcomes and model call purposes. Typed core/ledger/storage
cannot represent corrupted Python state dictionaries or invalid list payloads.

Previews use the shared process-local pool and normal side-model behavior, without
conversation status/count/done/interruption changes. Separate private lifetime
registration precedes lease calls. StopAccepting cancels it, and manager delete/
stop joins before closing services, releasing leases or reclaiming workspace.
This native join extends Python currently turn-only cleanup; operator turn Cancel
continues to ignore previews. Canceled/failed operations discard their exact draft.
Native probes cover empty ledger, foreign/anonymous/disabled ordering, normal
retention, queued deletion, fork exclusion and active delete/stop join. Commit,
authenticated HTTP preview/commit routes and lifecycle audit remain; G0-G7 open.

Validation: all eight source manager cases and native authority/ledger/retention,
caller cancellation/reuse, deterministic queued-delete identity recheck, fork
exclusion, active delete/stop join and blocked lease-acquisition stop checks pass.
Seven compiling mutations are caught: owner, anonymous, configuration, identity,
ledger supply, lease requirement and lease-loss mapping. Full Go tests/race/vet
pass. Contract check confirms 78 files current; previous fixtures are unchanged.
All 19 scanning guards remain anchored. Source guard verification ran scoped
skill and lease selectors: eight and six mutations caught, respectively; this
is not a full source guard sweep. Python package modules are unchanged, so the
package invariant verifier was not rerun. README outline and git diff --check
pass; Archify regeneration passes 9/9 with zero errors/warnings. Visual inspection
remains skipped after the earlier local-file access denial. Coverage was not
refreshed this slice. After all Go/exporter/scanner/source-guard/native-mutation
jobs reached terminal state, the isolated full Python suite passed: 2,151 tests,
28 skipped, 24 subtests, three existing deprecation warnings (79.97s). Final
git diff --check passes.

## Implementation checkpoint — 2026-10-07 reviewed manager skill commit

Base: 6b3770d. Preview and commit share a typed operation lifetime with owned
admission, identity/accepting recheck and lease claim. Commit checks readonly
before Peek, preserves exact owner/session/digest binding, publishes through
Resolver.PublishSkill and discards only the exact Peek identity after success.
Typed receipts retain canonical/body digests, source, optional warning, idempotency
and next_session activation. Existing snapshots remain pinned; new sessions see
the refreshed catalogue. Reviewed publication errors retain source code/message
and 409/422/500 classification; unknown faults/panics refuse safely. Failed
publication retains the draft. Once the file commits, no cancellation/TTL check
may rewrite success. Snapshot 79 compares six actual manager commit/retention
cases. HTTP routes and lifecycle audit remain next; G0-G7 remain open.

Validation: six actual source commit/retention outcomes pass, plus refreshed
secret screening without consumption, future-session activation/live snapshot
pinning, one successful commit and expiry during publication. Six compiling
mutations are caught: readonly, digest, consume-before-publication, post-success
discard, activation and conflict status. An initial readonly mutation failed to
compile due to an unused variable; it was corrected and rerun, and only compiling
mutations count. Existing preview cancellation/lease/join tests pass through the
shared lifetime refactor. Full Go tests/race/vet pass. Exporter check confirms
79 files current; previous fixtures are unchanged. Source guard selectors catch
eight skill and six lease mutations (not a full source guard sweep); all 19 scan
guards remain anchored. Python package modules are unchanged; package invariant
verification was not rerun. Regeneration emitted transient asyncio subprocess
cleanup warnings, while the fresh contract check matched every file and exited
zero. README outline/git diff --check pass; Archify regeneration passes 9/9 with
zero errors/warnings. Visual inspection remains skipped after prior file access
denial. Coverage was not refreshed. After every other validation job finished,
the first isolated full Python suite had one performance-ratio failure in
test_composition.test_detection_does_not_scale_with_transcript_bytes: small
0.129ms, large 0.607ms, above its 4x bound; 2,150 tests otherwise passed (103.91s).
Python runtime/test files are unchanged, and source inspection confirms pointer
comparison rather than payload hashing. The focused repeat passed (0.16s), then
the full isolated repeat passed: 2,151 tests, 28 skipped, 24 subtests, three existing
deprecation warnings (84.69s). No timing threshold or source behavior changed.
Final git diff --check passes.

## Implementation checkpoint — 2026-10-07 typed personal-skill HTTP requests

Base: 3260abc. The next HTTP slice starts with closed concrete preview and commit
request values. Decoder-local raw fields never enter service state. Unknown or
case-mismatched fields, missing required fields, explicit null and non-string
values refuse. Lowercase name/digest patterns and Unicode code-point bounds
match the actual Pydantic models; omitted focus defaults to empty and duplicate
JSON fields take their last value. Failed decoding preserves the typed receiver.
Snapshot 80 exports 43 actual source acceptance/normalization cases. This is a
separate small prerequisite commit; route registration, complete validation-error
envelopes, malformed Unicode boundary handling and HTTP lifecycle tests remain
next. G0-G7 remain open. No dependencies, defaults or runtime topology change.

Validation: focused request contracts, full Go tests/race/vet pass; the exporter
confirms 80 files current and all 19 source scanning guards remain anchored.
Source package modules and guarded runtime behavior are unchanged, so package
invariant and source mutation verifiers are not rerun. Architecture review finds
no flow/topology change; the visible README baseline is updated without changing
the canonical map. Coverage is not refreshed. After all Go/exporter/scanner jobs
finished, isolated full Python validation passed: 2,151 tests, 28 skipped,
24 subtests and three existing deprecation warnings (97.61s). README outline
and final git diff --check pass.

## Implementation checkpoint — 2026-10-07 owned personal-skill HTTP routes

Base: 0b20cf3. Both POST routes inherit bounded ingress and one authenticated
principal, decode concrete request values before ownership, require the owned
session and call Manager with the request context. Source routes do not spend
rate budget. Typed policy/draft failures preserve safe source status/code/message
through masked JSON; host faults receive a fixed safe native failure. Actual HTTP
comparison exposed prior policy message differences; owner/configuration/not-ready,
lease, readonly and publication-failure messages now match source strings.
Snapshot 81 compares eleven actual source HTTP responses after a real authenticated
message turn: authentication/query-token refusal, foreign ownership, empty ledger,
public preview, readonly, wrong digest, cross-session, reviewed receipt and consumed
draft. IDs/timestamps are checked separately; deterministic digests and all other
response fields compare exactly. Rejected commits precede successful reuse of the
same draft. Method admission and invalid-request-before-ownership/disabled behavior
have native tests. Complete Pydantic request-validation lists and malformed Unicode
boundary handling remain next; generic 422 decoding is not full FastAPI parity.
G0-G7 remain open. Dependencies and opt-in publication defaults are unchanged.

Validation: focused HTTP source comparison and full Go tests/race/vet pass;
additional final native request/method tests are verified normally and with race.
Exporter confirms 81 snapshots current; all 19 scan guards and eight selected
source skill mutation guards pass (not a full mutation sweep). Source package
modules are unchanged; package invariant verifier is not rerun. Canonical Mermaid,
boundary explanation and architecture specification are updated; generated HTML
passes Archify 9/9 checks with zero errors/warnings. Visual review remains skipped
after prior local-file access denial; automated checks do not establish visual
acceptance. Coverage is not refreshed. After all other validation jobs finished,
isolated full Python passed: 2,151 tests, 28 skipped, 24 subtests and three existing
deprecation warnings (92.48s). Final README outline and git diff --check pass.

## Implementation checkpoint — 2026-10-07 typed HTTP validation diagnostics

Base: e414508. Personal-skill routes now produce concrete ordered FastAPI-style
validation detail lists for valid JSON instead of the generic body refusal.
Diagnostic input uses a closed six-variant tree lowered immediately from decoder
tokens, with no retained any/RawMessage payload. Fields validate in source schema
order, then extra keys in decoded order. Duplicate keys preserve final values and
original positions. Required/null/non-object handling, string types, Unicode
lengths and source pattern contexts match actual HTTP errors. The existing masked
writer screens diagnostic strings and keys before JSON escaping. Snapshot 82
compares 41 actual FastAPI error responses, including nested input echo, multiple
errors, duplicate-key refusal, empty bodies and root types. Native escaped-secret
and extra-key tests verify the independent recording boundary. Successful request
and publication behavior remains covered by earlier source snapshots. Malformed
JSON/Unicode, extreme numeric input, content type and nesting-boundary fidelity
remain next. Native diagnostic nesting is capped at 256. G0-G7 remain open.

Validation: 41 actual HTTP validation comparisons and escaped-secret/key checks
pass. Final full Go tests/race/vet pass, including the linear duplicate-key index.
Exporter confirms 82 snapshots current; all 19 scan guards remain anchored. Source
Python package modules and source guard anchors are unchanged; package invariant
and source mutation verifiers are not rerun. The canonical Mermaid, boundary
explanation and specification reflect the diagnostic flow; regenerated HTML passes
Archify 9/9 with zero errors/warnings. Visual review remains skipped after prior
local-file access denial; automatic checks do not establish visual acceptance.
Coverage is not refreshed. After every other validation job finished, isolated
full Python passed: 2,151 tests, 28 skipped, 24 subtests and three existing warnings
(94.95s). README outline and final git diff --check pass.

## Implementation checkpoint — 2026-10-07 HTTP syntax and media admission

Base: 5fb24dc. The personal-skill request boundary now uses a bounded CPython-style
syntax scanner before lowering successful JSON to the existing closed diagnostic
tree and concrete request. Typed locations support named fields or integer code-
point positions. Syntax errors preserve json_invalid, safe source messages and
empty-object input. Only application/json or application +json media types parse
JSON; missing/other media preserve byte-string input. Empty bodies retain required-
body errors. UTF-8 BOM is stripped for JSON; illegal UTF-8 JSON gets source 400,
while illegal non-JSON input keeps the safe source plain 500 response. Snapshot
83 compares 63 actual HTTP results across both routes, including Unicode offsets
and media variants. Initial missing-header comparison failed, exposing FastAPI
0.136.3's strict_content_type=True default; installed routing source confirmed it,
and Go was corrected. The shared JSON test helper now sets its actual media type;
dedicated absent-header cases bypass it. Python baseline is 3.11.3. UTF-16/32,
lone-surrogate, nonfinite/extreme-number and depth boundary fidelity remain next.
The explicit native nesting bound remains 256. G0-G7 remain open.

Validation: all 63 actual parsing comparisons and existing preview/commit/request
contracts pass. Full Go tests/race/vet pass. Exporter confirms 83 snapshots current
and exits zero despite a transient subprocess cleanup warning; old snapshots are
unchanged. All 19 scan guards pass. The auth selector catches two source authority
guards (cron/shutdown), not a HTTP authentication mutation audit; no full source
mutation sweep was run. Source runtime modules are unchanged and package invariant
verification is not rerun. Canonical Mermaid, explanation and specification reflect
syntax/media admission; regenerated HTML passes Archify 9/9 with zero errors/warnings.
Visual inspection remains skipped after prior local-file access denial. Coverage is
not refreshed. After all other jobs finished, isolated full Python passed: 2,151
tests, 28 skipped, 24 subtests and three existing warnings (85.67s). Final README
outline and git diff --check pass.

## Implementation checkpoint — 2026-10-07 scalar JSON byte decoding

Base: 55d02a0. A named byte-encoding boundary now follows installed Python 3.11.3
json.detect_encoding: UTF-32/16/8 BOM precedence, first-four-byte NUL heuristics
and the two-byte special case. Scalar UTF-16/32 converts through explicit endian
units before the existing typed syntax/schema pipeline. UTF-16 pairs preserve
astral code points; truncated units and invalid UTF-32 ranges fail before parsing.
Charset parameters do not override byte detection, matching source request.json.
No dependency or publication default changes. Snapshot 84 compares 88 actual
HTTP outcomes across ten byte forms and both routes, including Unicode validation
and syntax positions. Six separate source surrogatepass cases return 500 while
native decoding currently returns 400; they are recorded as pending counterexamples
and excluded from passing counts. Isolated surrogates are explicitly refused,
without replacement. Surrogatepass representation, nonfinite/extreme-number and
depth-boundary fidelity remain next; G0-G7 remain open.

Validation: 88 actual scalar-encoding comparisons and previous personal-skill
HTTP suites pass. Full Go tests/race/vet pass; exporter confirms 84 snapshots
current, with earlier exports unchanged. All 19 scan guards pass. Source package
modules and source guard anchors are unchanged; package invariant/source mutation
verifiers are not rerun. Canonical Mermaid, explanation and specification reflect
byte conversion; regenerated HTML passes Archify 9/9 with zero errors/warnings.
Visual inspection remains skipped after prior local-file access denial. Coverage
is not refreshed. After all other jobs finished, isolated full Python passed:
2,151 tests, 28 skipped, 24 subtests and three existing warnings (82.73s).
README outline and final git diff --check pass. The six pending surrogate cases
are source evidence, not passing native parity checks.

## Implementation checkpoint — 2026-10-07 surrogatepass request boundary

Base: 76c6906. Request byte decoding now preserves non-scalar source code points
as transient surrogatepass bytes rather than refusing or replacing them. Syntax
positions count each as one code point. A closed typed value reader preserves raw
strings/keys, merges escaped pairs and resolves duplicate keys before screening.
Raw UTF-32/UTF-8 pairs remain separate, matching source codecs. Surviving non-scalars
produce source plain 500 before ownership/model operations; malformed JSON retains
source syntax errors. Overwritten malformed values do not poison the final tree.
Only scalar normalized data is marshaled into concrete requests, avoiding silent
replacement by encoding/json. Diagnostic serialization also refuses non-scalar
strings and keys. Snapshot 85 compares 90 actual source outcomes, promoting the six
previous counterexamples and adding raw/escaped pairs, syntax, distinct keys and
nested duplicate retention across both routes and three encodings. Native decoded-
value tests cover scalar pair/control escapes and both discarded-value forms.
No dependency, ownership or publication defaults change. Nonfinite/extreme-number
and depth-boundary fidelity remain next; G0-G7 remain open.

Validation: full `go test ./...`, `go test -race ./...` and `go vet ./...`
pass. Exporter `--check` confirms all 85 source snapshots; scanning verification
anchors all 19 guards. Isolated full Python suite passes: 2,151 tests, 28 skipped,
24 subtests, three dependency deprecation warnings, 82.97 seconds. Source runtime
modules are unchanged; package invariants and source mutation checks were not
rerun. Canonical Mermaid, explanation and specification are reviewed for Unicode
admission; generated HTML passes Archify's nine automated checks with no warnings
or errors. Visual inspection remains skipped after the earlier permission denial.
Fresh full `go test ./... -count=1 -timeout=180s -coverpkg=./...` passes.
Deduplicating shared profile blocks gives Go statement coverage **89.41%**
(**14,752 / 16,499**). This measures executed statements, not migration
completion. Python coverage is not refreshed.

## Implementation checkpoint — 2026-10-07 numeric request boundary

Base: f45aeae. The transient HTTP diagnostic tree now includes an explicit
nonfinite variant alongside its six JSON variants. Finite floating numbers are
normalized through double parsing and Python-style decimal formatting; integer
values remain exact, with -0 normalized to 0. A pinned 4300-digit integer check
runs inside syntax parsing, preserving source failure ordering even before later
malformed tokens or duplicate overwrites. Final retained nonfinite values return
source plain 500; discarded ones permit normal schema/ownership admission. No
nonfinite value enters service requests or runtime state. Snapshot 86 compares
288 actual source outcomes (24 forms, six placements, both routes), including
underflow, overflow, double rounding, exact large integers and digit-limit edges.
The test compares exact numeric lexemes and pins the source interpreter limit.
No dependencies or feature defaults change. Depth-boundary fidelity remains next;
G0-G7 remain open.

Validation: `go test ./...`, `go test -race ./...` and `go vet ./...` pass.
Exporter `--check` confirms all 86 snapshots, and `verify_scans.py` anchors all
19 source scanning guards. Isolated full `.venv/bin/python -m pytest -q` passes:
2,151 tests, 28 skipped, 24 subtests, 98.21 seconds. Four warnings include three
dependency deprecations and one asyncio subprocess-transport cleanup warning
(`Event loop is closed`) in the unchanged background-parity test; no test fails.
Python package modules and mutation anchors are unchanged; package invariants and
source mutation verification were not rerun. README canonical Mermaid, boundary
explanation and interactive specification are reviewed in this slice. Regenerated
HTML passes all nine Archify automated checks with zero errors/warnings; visual
inspection remains skipped after the earlier permission denial. `git diff --check`
passes. Coverage percentages are not refreshed in this slice.

## Implementation checkpoint — 2026-10-07 live HTTP request depth

Base: f284b4e. Source investigation distinguished TestClient's incidental recursion
stack (parse 980 / echo 973) from real default Uvicorn HTTP (parse 985 / echo 978),
with CPython recursion limit 1000. Snapshot 87 records 144 live network outcomes,
including array/object/empty nesting, discarded duplicate values and malformed
syntax at 12 depths for both routes. Go now uses explicit container counting and
separate retained diagnostic-input depth screening. Parsing failure remains 400;
source echo exhaustion becomes safe plain 500; shallow syntax diagnostics remain
422 even when the valid prefix exceeds echo depth. Discarded values do not consume
response depth. A bounded request-only masking projection supports deep diagnostics
without changing ordinary recording's 256-depth bound or the byte bound. The native
fixture compares complete outcomes in unmasked and secret-registered modes (288
comparisons), including a credential at the deepest echoed leaf. No dependencies,
authority or feature defaults change. Other interpreter/server-stack profiles are
unverified; the full Go migration and G0-G7 remain open.

Validation: full `go test ./...`, `go test -race ./...` and `go vet ./...` pass.
Exporter `--check` confirms all 87 snapshots; it emitted one ignored asyncio
subprocess-transport cleanup exception (`Event loop is closed`) while still
returning success with matching artifacts. `verify_scans.py` anchors all 19 source
scanning guards. The ordinary projection nesting guard and new scoped byte/depth
guards pass in the Go protocol suite. Isolated full Python suite passes: 2,151
tests, 28 skipped, 24 subtests, three dependency deprecation warnings, 81.11 seconds.
Python package modules and source mutation anchors are unchanged; source invariants
and mutation verifiers were not rerun. Canonical Mermaid, explanation and JSON
specification are reviewed; regenerated HTML passes Archify nine automated checks
with zero errors/warnings. Visual inspection remains skipped after the earlier
permission denial. `git diff --check` passes. Coverage percentages are not refreshed.

## Implementation checkpoint — 2026-10-07 owned skill catalogue HTTP

Base: a1c9c1b. Added the missing owned GET /sessions/{session_id}/skills endpoint
through a narrow ManagedSession.SkillCatalogue read and concrete session/catalogue
response. Authentication and owner admission precede catalogue access; neither
skill loading nor resolver refresh runs during GET. Existing sessions retain their
model-facing description source after publication; newly created independent
sessions and forks bind the current owner bundle. Open deployments keep the same
anonymous principal. Custom source exceptions fail with safe plain 500, while the
existing native registered-secret projection masks successful descriptions.
Snapshot 88 compares 48 actual source outcomes across four configurations. Native
counter-based tests verify denied callers cannot touch the catalogue, bodies are
not loaded, description credentials are masked, and custom panics reveal no host
content. No dependency, feature default or publication authority changes. Remaining
memory/team/workflow routes and other planned groups are still open; G0-G7 remain
unchecked.

Validation: full `go test ./...`, `go test -race ./...` and `go vet ./...` pass.
Exporter `--check` confirms all 88 snapshots; `verify_scans.py` anchors all 19 source
scanning guards. Isolated full `.venv/bin/python -m pytest -q` passes: 2,151 tests,
28 skipped, 24 subtests, 110.95 seconds. Four warnings include three dependency
deprecations and one asyncio subprocess-transport cleanup warning (`Event loop is
closed`) surfaced in the unchanged owner-map-bound test; no test fails. Python
package modules and mutation anchors are unchanged, so source invariants and
mutation verification were not rerun. The existing browser loadSkills function
calls the added URL and consumes catalogue; this is static source evidence.
Canonical Mermaid, explanation and specification are reviewed; regenerated HTML
passes all nine Archify automated checks with zero errors/warnings. Visual
inspection remains skipped after the earlier permission denial. Actual native
route registration counts 31 method/path operations across 28 path patterns.
`git diff --check` passes. Coverage percentages were not refreshed.

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

## Coverage refresh — 2026-10-09, runtime at ec20692

Fresh full Go coverage run after the native SQLite reconstruction, cron and
killed-process recovery tests:

| Scope | Covered statements | Total statements | Coverage |
| --- | ---: | ---: | ---: |
| All Go packages | 26,935 | 29,906 | 90.07% |
| agent | 8,776 | 9,783 | 89.71% |
| sqlitestate | 521 | 625 | 83.36% |
| httpapi | 1,610 | 1,802 | 89.35% |
| efficiency | 1,489 | 1,629 | 91.41% |

Command: `go test -p=1 -count=1 -timeout=300s -coverpkg=./...
-coverprofile=/tmp/mini-loop-current-coverage.out ./...` from `go/`; exit 0.
Aggregate calculation groups identical source blocks across test packages and
uses their maximum execution count before summing statements. Package log lines
with `-coverpkg=./...` are individual test contributions, not package totals.
The temporary profile is local validation output and is not checked in.

This is statement coverage, not a full-port parity or release-readiness measure.
Python full tests passed in the composition slice, but Python coverage was not
refreshed. Source contracts committed as acb2646 add no Go runtime statements.

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

## Explicit Manager full-feature composition — 2026-10-09

`ManagerConfig.EnableFeatures` now normalizes the twelve Source registry groups
when no custom tool catalogue is supplied. A full built-in catalogue requires an
explicit diagnostics Provider; rejection precedes efficiency normalization,
journal recovery and workspace allocation. No parser fallback or new dependency
is installed. Workflow, AST and decision additions keep independent selection.

A caller-supplied catalogue is authoritative and is not expanded by the full
profile. Explicit individual Go feature options still retain their own meaning.
Full-profile lifecycle binding is separate from registration: background orphan
notifications and team inbox remain available with a custom catalogue and nil
injector selection. Background binding does not overwrite the custom Bash
classifier or handler. An overridden injector list retains its earlier replacement
semantics, and custom catalogues without selected background tools or default
injection do not require the native background executor.

All eight actual Source Manager catalogue recipes now match native construction: 10
default, 48 full, ten custom tools, 48 with injector override, 51 with workflows,
52 with AST, 49 with decisions and 56 together. Full/default versus custom Bash
classification also matches. Native model turns additionally prove full-profile
and custom-catalogue notification delivery, suppressed queues under explicit empty
injection and later delivery when defaults are restored. Full profile enables
existing automatic memory extraction; the notification fixture handles its
tool-free auxiliary request instead of assuming every model request has tools.

This is an embedding API with caller-supplied diagnostics, not completed default
startup. Launcher/CLI `MINILOOP_FEATURES` still refuses activation pending a
default diagnostics backend and startup wiring. Approval for using the existing
CPython backend remains pending. Runtime paths still use the common typed gate;
custom definitions/authority and optional durable-store ownership remain explicit.

Validation: all eight catalogue recipes and focused notification/custom executor
cases pass. Final full `go test -p=1 -count=1 -timeout=300s ./...` and
`go vet ./...` pass. First full race run had only the notification fixture failure
(the old recording provider indexed an empty auxiliary-request tool list); all
other packages passed. After fixing that test fixture, full `go test -race
-count=1 -timeout=300s ./agent` passes, as does a final focused race run covering
the added custom-executor case. The aggregate race command was not repeated;
production code was unchanged after that full race run. Existing macOS CGO
linker warnings are nonfatal. Architecture delivery passes 9/9 with zero
errors/warnings and exact receipt hashes/bytes verified; browser visual inspection
remains skipped after the earlier denial. README outline and `git diff --check`
pass. Python runtime/exports and Go tool schemas are unchanged: their checks
were not refreshed, and coverage was not remeasured.

## Provider-backed Launcher full-feature startup — 2026-10-09

`Launcher.NewWithOptions` now accepts `MINILOOP_FEATURES` when the embedding caller
supplies `DiagnosticsProvider`. A detached settings copy removes only that
resolved unsupported condition during validation; original settings retain the
full selection passed to `ManagerConfig.EnableFeatures`. No provider is called
by inspection. Without one, settings-only/CLI startup still fails before resource
construction with the explicit missing default Python-syntax backend reason.

Inspection reports available full/task/worktree selection and its registry
groups only when the diagnostics backend is supplied. Missing-backend requests
remain in the settings snapshot/Unsupported list, without advertising available
full groups or changing caller options or enabling independent workflow/AST/
decision additions. The reported Unsupported list uses the same availability
rule as startup. Full mode forwards configured `RepoRoot` into a native worktree
manager; a missing root retains Source's unconfigured tool result. No new
dependency, Python runtime or default parser is introduced.

Native HTTP tests run two real model turns and compare both ordered 48-tool
request catalogues to Source's full registry. `list_worktrees` retains the exact
unconfigured-root result and, with a configured non-Git directory, Source's
`(not a git repo)` result. Inspection with a provider reports support without
provider calls/workspace creation; settings alone and caller settings still
report the missing parser. Existing explicit-only feature paths remain tested.

This closes startup wiring for an explicitly supplied diagnostic backend.
Default CLI feature activation and the default parser remain incomplete; the
previous CPython-backend approval request remains pending. This is not a release
completion or full runtime-parity claim.

Validation: focused Launcher HTTP/inspection and CLI selection tests passed.
The initial full runs exposed inspection advertising full groups without a
backend; the availability rule was corrected, and final complete runs of
`go test -p=1 -count=1 -timeout=300s ./...` and
`go test -race -p=1 -count=1 -timeout=300s ./...` both exited zero from `go/`.
`go vet ./...` passed. Existing macOS CGO linker warnings were nonfatal.
README outline and `git diff --check` passed. Architecture delivery scored 9/9
with zero errors/warnings, and receipt sizes/SHA hashes matched the specification
and generated HTML. Visual inspection remains skipped following the earlier
browser denial. Python packages, exported contracts and tool schemas did not
change in this slice, so Source tests/export/scanner/invariant/guard checks were
not rerun. Statement coverage was not refreshed for this slice.

## Deployment full-feature health selection — 2026-10-09

Base: e1e4fde. Source server health reads `settings.enable_features`, separately
from workflow availability and the actual registry. The Go health struct had a
Features field but never set it, so provider-backed full Launcher startup falsely
reported features=false. HTTP Config now accepts a concrete Features boolean;
Launcher forwards the original deployment setting. This is informational selection,
not authorization, durability or a guarantee that every optional group is enabled.

Existing real HTTP full Launcher tests now assert features=true in both repository
profiles. Source-derived default/workflow Launcher recipes assert features=false,
including independently enabled workflows. An isolated Source TestClient lifespan
probe confirmed both false/true selections against current Python code with the
fake client and temporary workspace. The first probe lacked PYTHONPATH and failed
before construction; the corrected `PYTHONPATH=python` probe passed both cases.
Full health posture and timing/build equivalence remain separate open work.

Validation: focused full-feature/workflow Launcher tests passed; complete
`go test -p=1 -count=1 -timeout=300s ./...`,
`go test -race -p=1 -count=1 -timeout=300s ./...`, and `go vet ./...` from `go/`
all exited zero. Existing macOS CGO linker warnings were nonfatal. Source live
health probe passed both profiles. Architecture regeneration passed 9/9 showcase
checks with zero errors/warnings; receipt byte sizes and SHA256 hashes match
specification/HTML. Visual inspection remains skipped after the earlier browser
denial. README outline and diff checks passed. Python package code, exported
contracts, schemas and guarded anchors did not change; full Python tests and
export/scanner/invariant/guard sweeps were not rerun. Coverage was not refreshed.
The migration remains incomplete, including the default syntax parser and full
health posture.

## Runtime-posture Source contracts — 2026-10-09

Base: 016c03e. The health port still lacks Source's `posture` field. Snapshot
`python-posture.json` records 14 real Manager/Agent and FastAPI lifespan profiles,
with 13 concrete posture fields per before/direct/HTTP/after report. Only volatile
build/PID/start/uptime fields are removed from the health document. Default-empty,
default-live, null/custom registries, unresolved/short registered values, exact-case
registration, unavailable sandbox, SQLite/authentication, actual event-write failure,
live Agent overrides, workflow selection, independently selected full deployment
settings, owner resources and trajectories are represented.

The broken-store scenario runs a real fake-model turn against an injected store
whose event append fails; it captures the surfaced persistence error. The initial
construction-only probe produced no error, so it was replaced with that real turn.
Live overrides demonstrate Source's first-live-Agent preference for secrets, cache,
detector and sandbox. Registry coverage uses nonempty environment values, uppercase
credential-pattern matching and exact registered-name matching; unresolved/short
registered values still count as covered. No-registry/default Null reports zero
unregistered credentials. Only the count is published, never credential identities.
Inspection retains session counts, and owner-resource inspection creates no owner
children. Manager startup/lifetime uses the actual TestClient lifespan, not a mock.

Next cohesive implementation slices:
1. Define a named Go posture record with explicit nullable persistence error and
   sandbox reason, concrete implementation labels and booleans/counts.
2. Read effective runtime defaults/live bindings without constructing a session or
   resolving owner resources; expose observed persistence failure and credential
   coverage through narrow typed seams. Record native sandbox/extension differences.
3. Bind the public health projection and compare supported profiles against this
   Source snapshot; preserve false feature flags for independent groups.
4. Continue runtime identity/timing, remaining dependency-approved default parser,
   native sandbox/other unresolved extension seams and the complete G0-G7 audit.

This is captured Source evidence, not an implemented Go posture or a parity claim.
No runtime code, dependency, default selection, public route or architecture
ownership changes in this slice. Go remains independently incomplete.

Validation: both new Source contract tests passed, including reconstruction under
two temporary roots. Full `.venv/bin/python -m pytest -q` passed 2161 tests,
28 skipped and 24 subtests, with three dependency deprecation warnings in 97.10s.
Exporter `--check` passed all 189 files (dependency deprecation/regex warnings);
`verify_scans.py` passed all 19 scanning anchors. README outline and
`git diff --check` passed. Python package modules and guarded runtime anchors are
unchanged, so separate invariant/mutation sweeps were not rerun. Go source and
architecture topology/specification are unchanged; Go gates and architecture
regeneration/visual inspection were not repeated. Coverage was not refreshed.

## Typed native runtime posture and health — 2026-10-09

Base: 95f7e2b. `agent.RuntimePosture` is a concrete 13-field record with nullable
persistence error/sandbox reason and named booleans/counts/component labels.
`ObserveRuntimePosture` snapshots service/first live Agent bindings in Manager
insertion order, then releases the Manager lock before registry callbacks or
persistence-status reads. Without a live Agent it uses the same native default
cache/detector constructors as sessions. It creates no session, resolves no owner
bundle, performs no model/tool call and resolves no secret value. These bindings
are immutable after publication; registry contents and persistence errors use
their existing concurrent snapshot methods. The report is a detached observation,
not a cross-component atomic snapshot or an authority/durability grant.

`secrets.CountUnregistered` matches Source's nonempty-value, uppercase credential
pattern and exact registered-name rules. Registered names count regardless of
short/unresolved values. Default/no-registry paths retain count zero; unknown
custom maskers without a Names method count all matching environment entries.
Reflection reads only implementation type metadata from typed seams; no broad
payload, unchecked data assertion or dependency was introduced. Registry callback
panics produce an error, never a fabricated healthy report.

Public health now includes the typed posture through its existing masked JSON
projection. Existing output masking may resolve registered masker values; the
observation itself reads names only. This native masking extension is retained
and is not a Source lazy-value HTTP equivalence claim. Actual event-write failures are reported; HTTP tests use an actual
SQLite store with an injected event-append failure, then an actual native turn.
Authenticated deployment posture is visible on public health, while credential
identities/values stay private. A failed registry observation returns a private
500. Default HTTP Source capture no longer strips posture or lists it as deferred;
all 13 fields now enter the existing response comparisons.

Native labels intentionally name the actual Go implementation. Native absent
state/secrets/sandbox use None, unlike Source's NullStateStore/NullSecretRegistry/
NullSandbox wrappers; explicit secrets.Null has the compatibility NullSecretRegistry
label. Registry/Store/StoredActionJournal differ from Source's SecretRegistry/
SQLiteStateStore/DurableActionJournal. Comparisons normalize only the documented
absent-component labels, not booleans/errors/counts. Nine Manager profiles replay
Source observations; the live-override profile explicitly retains native sandbox
absence instead of Source's unavailable-sandbox reason. Native sandbox selection,
Source unavailable-sandbox parity and broader custom extensions remain open.
Go persistence errors retain their existing native bounded type prefix.

Tests also prove detached faults, first-live selection after deletion, no extra
session creation, registry reentry into Manager observation without a held lock,
private registry failure, unresolved-value nonresolution and exact-case coverage.
The first fault test changed permission mode, which correctly emitted no event;
it was replaced by an actual run to exercise the backing-store failure. Focused
compilation caught a test-local marshal error variable and a root-directory Go
command; both were corrected. The HTTP export regeneration now uses a new child
scratch directory, as required by its actual constructor. No runtime default,
permission policy, dependency or persistent schema changed.

Validation: focused Source/native posture and actual SQLite HTTP fault/privacy
checks passed. Complete `go test -p=1 -count=1 -timeout=300s ./...` and
`go test -race -p=1 -count=1 -timeout=300s ./...` from `go/` exited zero;
`go vet ./...` passed. After narrowing test normalization to known null labels,
focused final agent/HTTP/secrets race comparisons passed; production code was
unchanged after the full gates started. Existing macOS CGO linker warnings were
nonfatal. Exporter `--check` passed all 189 files; it emitted dependency/regex
warnings and an ignored closed-event-loop subprocess-destructor traceback, then
exited zero. All 19 scanning anchors passed. Once those and Go jobs were terminal,
full Python pytest passed 2161 tests, 28 skips and 24 subtests with three warnings
in 80.63s. Python package modules/mutation anchors are unchanged, so separate
invariant/guard sweeps were not rerun. README outline and diff checks passed.
Architecture regeneration passed 9/9 showcase checks with zero errors/warnings;
receipt byte sizes/SHA256 match specification and HTML. Visual inspection remains
skipped following the earlier browser denial. Coverage was not refreshed.
Default Python-syntax backend, native sandbox and the full G0-G7 audit remain open.

## Existing sandbox observation and null-store audit marker — 2026-10-09

Base: b0613e4. Inspection of the actual native shell revealed a pre-existing
`Sandbox` argv/ForWorkspace seam, so the previous report's unconditional sandbox
None and documentation's “no seam” statement were incorrect for injected
executors. This slice corrects that report before adding native Seatbelt.
`Executor.SandboxPosture` reads wrapper type metadata and a detached optional
`SandboxReason`; it never calls Argv or ForWorkspace. Native host factories report
None. Custom factories may expose this typed metadata for an empty fleet;
unobservable custom factories/executors return an observation error, which public
health projects as its existing private 500. They are not guessed to be confined
or unconfined, and inspection does not construct a session or call a Bash factory.

Actual active executor bindings publish through a typed atomic pointer on initial
construction and after successful workspace preparation. This avoids reading the
mutable core Bash field concurrently with workspace entry. Tests rebind between
wrappers of different native types and inspect concurrently; reported type follows
the successful binding. Failed preparation cannot publish a new shell binding.
Metadata is configuration evidence only, not a claim that the OS enforces policy.
The Seatbelt/default availability library and its real OS enforcement tests remain
next. No sandbox is enabled by default and no dependency is added.

A second concrete gap appeared in Source `audit_posture`: it detects missing
persistence only for the literal NullStateStore marker. Native None was an honest
absence description but caused that actual auditor to miss process-local-only
transcripts. The nil native store now reports the compatible NullStateStore
marker; it creates no null store object or persistence service. Existing full
HTTP comparisons keep this marker exact. Secret/sandbox null-wrapper aliases
remain narrowly normalized, and comparison now uses closed JSON Value projection
so future unknown response fields are retained instead of dropped by typed
remarshalling. Unknown/non-null labels remain untouched. This supersedes the
previous slice's absent-state None label and its missing-sandbox-seam statement.

Focused native race tests cover wrapper metadata without command execution,
reason detachment/rebinding, empty/live observations, type changes on workspace
entry, concurrent inspection/publication, unsupported factory refusal and later
live observability. The unknown-field comparison regression guards against the
previous typed remarshal dropping future Source fields. Runtime permissions,
process execution, persistence schema and feature defaults are unchanged.

Validation: focused final agent/shell/HTTP/Launcher race tests passed. Complete
`go test -p=1 -count=1 -timeout=300s ./...`,
`go test -race -p=1 -count=1 -timeout=300s ./...` and `go vet ./...` from `go/`
all exited zero. Existing macOS CGO linker warnings were nonfatal. A freshly
built actual CLI bound an ephemeral loopback port in a temporary workspace; its
real GET /healthz document was passed to current Source audit_posture. Source
reported action-journal, authentication, secret-masking, shell-confinement and
durable-state. Replacing only the actual null-store marker with the old None
reproduced the missed durable-state finding. The owned native process received
SIGTERM, drained and exited zero; the temporary workspace was released. Evidence
is `/tmp/mini-loop-native-posture-audit.json` (local, not a repository fixture).
Architecture regeneration passed 9/9 showcase checks, zero errors/warnings;
receipt sizes/SHA256 matched specification and generated HTML. README outline
and diff checks passed. Visual review remains skipped following the earlier
browser denial. Python package, exports, schemas and mutation/scan anchors did
not change, so full Python/export/scanner/invariant/guard checks were not rerun;
the live Source auditor was executed. Coverage was not refreshed. Native
Seatbelt/default platform selection, default diagnostic parsing and full G0-G7
completion remain open.

## Coverage refresh — 2026-10-09, runtime at 82bf428

Fresh full Go statement coverage on Darwin arm64 after feature composition,
Launcher activation, HTTP posture and sandbox observation delivery.

| Scope | Covered statements | Total statements | Coverage |
| --- | ---: | ---: | ---: |
| All Go packages | 27,093 | 30,068 | 90.11% |
| agent | 8,878 | 9,885 | 89.81% |
| sqlitestate | 521 | 625 | 83.36% |
| httpapi | 1,614 | 1,806 | 89.37% |
| efficiency | 1,489 | 1,629 | 91.41% |
| shell | 381 | 405 | 94.07% |

Gate: `cd go && go test -p=1 -count=1 -timeout=300s -coverpkg=./...
-coverprofile=/tmp/mini-loop-current-82bf428-coverage.out ./...`; exit 0.
Repeated source blocks are merged by maximum execution count before statement
aggregation. Per-test-package log percentages are not package coverage totals.
The profile stays in temporary local output. Python coverage and mutation coverage
were not refreshed. This measures executed statements, not port completion.
Only documentation changes in this refresh; race/vet were not rerun.
Implementation validation is recorded in the preceding delivery entries. No push.

## Source sandbox construction contracts — 2026-10-09

Base: c2b189f; runtime baseline remains 82bf428. Export actual Source
SeatbeltSandbox, NullSandbox, UnavailableSandbox and default_sandbox construction
to python-sandbox.json. No policy is reconstructed in the exporter and no child
command is executed. Absolute scratch paths normalize to <ROOT>; snapshots are
compared across two independent roots.

Eight construction/rebind recipes cover protected reads, additive networking,
ordered duplicates, extra-only writable roots, policy-syntax paths, linked git
worktrees, symlink/nonexistent-tail resolution and home expansion. Rebind replaces
ordinary writable roots, retains explicit _extra_writable and rediscovers the new
repository's shared .git metadata. Sixteen platform/default selection recipes
pin Darwin binary availability, Linux/Windows degradation, requiring confinement
and credential opt-out. A separate omitted-option call pins the actual factory
default credential deny-list and network denial. Null/unavailable wrappers keep
their identity on rebind. Eight marker recipes cover relative/absolute paths,
exact-prefix matching, invalid parents, later valid lines, Unicode line breaks,
empty files and .git directories. Errors capture empty writable roots and invalid
UTF-8 markers. Argv, description, preview and path lists come from Source objects.

The new fixture is construction evidence, not proof of OS enforcement. Native
Seatbelt, actual shell execution under its policy, full default platform selection
and default diagnostics remain pending. No Go runtime, Python package, dependency,
architecture topology or feature default changes. README review baseline updated;
architecture regeneration is unnecessary for this contract-only slice.

Validation: focused Source sandbox/contract tests passed (30 tests), then final
contract tests passed (2 tests). The final isolated full Python run passed:
2163 passed, 28 skipped, 24 subtests, three dependency warnings in 77.10 seconds.
Final export --check passed for all 190 files; all 19 source scan guards are
anchored. README outline and git diff --check passed. The initial concurrent
full/export run failed: the existing 40-turn 0.5-second timing recipe measured
0.62 seconds, and this agent edited the fixture during verification, mixing the
old imported capture with the new snapshot. That run is not used as final
evidence; verification was repeated after fixing the source/snapshot state and
serializing the full suite and export. No thresholds or runtime were changed.
Go tests/race/vet, invariant/guard scripts and coverage were not refreshed for
this Python-tool/fixture-only slice. No push; full migration remains active.

## Native opt-in Seatbelt sandbox — 2026-10-09

Base: e0ee372. Native go/sandbox now implements Source policy/parameter building,
NullSandbox, reason-carrying UnavailableSandbox, platform availability and the
credential-protecting default factory through the existing shell.Sandbox seam.
Config and DefaultOptions are concrete types; private copied slices make bindings
immutable. Optional ProtectCredentials distinguishes omission (enabled) from an
explicit false. SandboxUnavailable and MarkerEncodingError retain typed failures.
No ambiguous JSON/domain containers or dependency were added. Configuration
metadata is not an OS attestation. No wrapper is installed by runtime startup.

Native comparisons consume the committed actual Source fixture through a closed
JSON decoder that rejects unknown fields. All eight construction/rebind recipes,
eight git markers, sixteen availability/require/credential profiles, omitted
factory defaults, null/unavailable wrappers and constructor errors match. Argv
and preview retain exact Source policy/quoting; operator path text only appears
in separate -D parameters. Workspace rebind replaces ordinary roots, retains
explicit ExtraWritable, re-resolves protected roots and rediscovers shared git
metadata, never granting sibling working-tree files. Existing workspace.ResolvePath
retains symlink/parent/nonexistent-tail semantics; marker decoding is strict UTF-8
and line splitting follows Source Unicode separators.

Actual Darwin arm64 shell.Executor executions prove protected root/file reads and
copy-to-workspace denial, outside writes denied without file creation, inside
writes allowed, successful rebind with old writes denied and original executor
still bound. Three ordinary children can spawn; resource caps are not claimed.
A real local HTTP server provides a positive network control: the same system
curl command succeeds under AllowNetwork and fails under default policy. A real
git repository with two linked worktrees commits from its sandbox while main/peer
working files remain unwritable. Availability-only checks cannot replace these
execution tests. An unavailable-backend executor actually runs host commands and
reports a detached reason; nil sandbox still reports None. Caller mutations and
concurrent rebind/argv construction cannot alter the original configuration.

README canonical Mermaid/boundary/default labels and interactive specification
updated; HTML regenerated with Archify, not edited. Showcase validation passed
9/9 with zero errors/warnings and receipt sizes/SHA256 matched both files. Visual
inspection stays skipped after the previously denied browser action. The runtime
still needs default diagnostics and wider full-port acceptance; the full goal
remains active. No push.

Validation: focused final sandbox race tests passed, including all real macOS
execution cases without skips; the final full ordinary and race suites passed
with `go test [-race] -p=1 -count=1 -timeout=300s ./...`. The initial ordinary run
failed the existing Launcher AST timeout recipe because its first probe exceeded
one second; isolated timeout regression and the repeated final full ordinary
run passed. No AST code, fixture or deadline was changed. Nonfatal macOS CGO
LC_DYSYMTAB linker warnings remain in the full race output. Python package,
exports, schemas, source scan and mutation anchors are unchanged, so full Python,
export, invariant/scanner/guard checks were not rerun. Coverage was not refreshed.

Final `go vet ./...`, README architecture outline and `git diff --check` passed.

## Direct Manager sandbox injection — 2026-10-09

Base: 8c9d90a. Source SessionManager accepts sandbox directly; native Manager
previously required callers to implement a BashFactory plus empty-fleet metadata.
ManagerServices.Sandbox now provides that typed seam through the default factory.
Fresh and restored bindings, and runtime workspace preparation, already consume
this factory; no additional execution path is introduced. Simultaneously setting
Sandbox and a custom BashFactory is refused at constructor entry, before feature
composition, services, recovery or workspace allocation. Custom factories retain
complete ownership when this option is absent. Default startup stays host-based;
Launcher sandbox configuration is not added in this slice.

A shared shell.InspectSandbox helper projects type/reason metadata without calling
ForWorkspace, Argv or constructing an executor. Both live executors and default
factory templates use it, with detached reasons. Empty and populated managed
fleets report actual UnavailableSandbox/SeatbeltSandbox configuration. Metadata
alone does not prove enforcement; optional callbacks retain the existing shared
concurrency-safety contract.

Real macOS managed model-tool turns use native Seatbelt through the normal Run
and tool gate: allowed workspace/extra-root writes succeed; protected reads and
outside writes fail with recorded model-facing results and absent forbidden files.
After workspace entry, the parent writes only its new root plus explicit extra
roots. A child constructed before entry executes a fresh model/tool turn in its
pinned old root and cannot write the parent's new root; credentials remain denied
in both. Live posture remains the actual backend. Constructor conflict refuses
without invoking the competing factory or allocating its root. No dependency,
ambiguous domain JSON container or implicit sandbox activation is added.

README Mermaid and boundary/baseline, EXTENDING and the architecture specification
are updated; HTML regenerated via Archify. Automated showcase validation passed
9/9 with zero errors/warnings, and sizes/SHA256 match the receipt. Visual review
remains skipped after the prior browser denial. Default diagnostic parsing and
remaining full-port acceptance are still open; no push.

Validation: final focused agent race tests passed (three integration tests, real
Seatbelt case executed without skip). Full ordinary and race suites from go/
passed with `go test [-race] -p=1 -count=1 -timeout=300s ./...`; nonfatal
macOS CGO linker warnings remain. Native restore consumes the same factory at
restore.go:230; real persisted sandbox restoration is not exercised in this
slice. Python package/exports/schemas/scanner and mutation targets did not change,
so Python/export/invariant/scanner/guard gates and coverage were not refreshed.

Final go vet ./..., README architecture outline and git diff --check passed.

## Explicit Launcher sandbox binding — 2026-10-09

Base: cc690a7. Launcher.Options.Sandbox now explicitly selects the sandbox used
by its existing bound Bash factory, alongside the configured BashTimeout. This
factory owns the boundary rather than setting both ManagerServices.Sandbox and
BashFactory; the prior conflict guard remains intact. The same factory serves
creation/restoration and workspace preparation. Nil remains host execution; no
CLI switch, environment setting, automatic platform selection or dependency is
added. Sandbox lifetime/concurrency safety remain caller-owned.

InspectWithOptions projects actual template metadata through shell.InspectSandbox,
retaining the historical default sandbox="none" and exposing an optional detached
sandbox_reason for unavailable backends. It allocates no workspace and performs
no Argv/ForWorkspace/command invocation. The existing public HTTP health report
reads this factory for an empty fleet and actual bound executors for live handles.
Configuration metadata alone is not an OS-enforcement guarantee.

Real loopback HTTP tests create a session, admit a model-tool turn through the
configured Anthropic-compatible HTTP provider, execute Bash and return its result
in the next actual upstream model request. Native Seatbelt (without skip on local
Darwin arm64) permits workspace writes, blocks reading a fake protected credential
and writing outside, records both denials without the canary value, leaves no
forbidden file, and enforces the configured one-second shell deadline. An explicit
UnavailableSandbox instead permits the controlled host read/write while retaining
that same deadline. Empty/live health and SDK inspect report both selections;
reason snapshots detach. The existing configured-host-shell deadline regression
also passes. These tests use concrete replies and existing closed JSON request
variants, introducing no ambiguous domain payload.

README Mermaid/boundary/review baseline, EXTENDING and architecture specification
updated; HTML regenerated via Archify with 9/9 showcase checks and zero errors/
warnings. Receipt sizes and SHA256 match the specification/artifact. Visual review
remains skipped after the previously denied browser action. Default diagnostics
and remaining full-port acceptance stay open; no push.

Validation: focused final Launcher race tests passed, including both real HTTP
profiles and the pre-existing host deadline regression (Seatbelt not skipped).
Full `go test -p=1 -count=1 -timeout=300s ./...` and the same command with
-race passed. Existing macOS CGO LC_DYSYMTAB warnings were nonfatal. Final
architecture receipt hash/size checks, README outline and git diff --check passed.
Python package/exports/default-tool schemas/source-scanner and mutation targets
were unchanged, so Python/export/invariant/scanner/guard gates and coverage were
not refreshed. Broader restart/confinement profiles remain separate acceptance
work; full port goal remains active.

Final go vet ./... passed.

## Native SQLite sandbox restoration proof — 2026-10-09

Base: 98a5a1b. A real Launcher/HTTP/native SQLite test now covers the recovery
boundary previously inferred from the shared Bash factory. Generation one creates
an authenticated owned session and runs a real model/Bash turn under Seatbelt.
After server/manager shutdown, its process lease is released and the database is
closed. A new database open and Launcher use a different explicit ExtraWritable
root, then the operator explicitly restores the persisted session. Construction
does not restore implicitly; a foreign HTTP owner still receives 404. Workspace,
stable identity and transcript remain unchanged before the next admitted turn.
Permission is explicitly reselected through the owned mode endpoint; restoration
does not recover an old human grant.

The resumed real model-tool turn permits writes in the original session workspace
and the new extra root, denies a fake protected credential read, denies outside
writes and revokes the old extra root. The actual following model request contains
the denial results without the canary value; filesystem checks prove both allowed
files and absence of forbidden effects. Live public health reports Seatbelt with
no persistence fault. Shutdown again releases the lease; a third actual database
open retains the same session identity and run_count=2. The focused race test
executes real Seatbelt on local Darwin arm64 without skip. This is close/reopen
in one process, not a power-loss or killed-sandbox-generation claim.

Runtime inspection found no implementation change necessary. No runtime, schema,
Source export, dependency, authority or feature default is modified. README map
and visible review baseline are reviewed/updated; the canonical diagram topology
and architecture specification are unchanged, so no regeneration is required.
The default diagnostics and wider full-port acceptance remain open; no push.

Validation: final focused `go test -race -count=1 -run
TestLauncherSQLiteRestoreRebindsCurrentSeatbeltPolicy -v ./launcher` passed
without a Seatbelt skip. Full serialized `go test -p=1 -count=1 -timeout=300s
./...` passed. The full race suite was not repeated for this test-only slice;
runtime code remains at 98a5a1b. Existing CGO linker warnings were nonfatal.
README outline and git diff --check passed. Python/package/export/invariant/
scanner/guard gates and coverage were not refreshed because their targets are
unchanged. No architecture topology/defaults changed.

Final go vet ./... passed.

## Remote posture audit Source contracts — 2026-10-09

Snapshot `python-audit-posture.json` fixes 22 actual Python `audit_posture`
results and `render` outputs: fourteen existing real health profiles plus eight
missing/null/empty/unknown/selected/error-precedence cases. The exporter runs
Source code in a separate scratch root; Python tests compare two independent
roots with the frozen fixture. This prepares the remote audit library; local
settings/live inspection and CLI composition remain outside this slice.

Validation: full Python pytest passed (2165 passed, 28 skipped, 24 subtests;
3 existing warnings). Source exporter --check passed for 191 files.
verify_scans passed all 19 anchored scanning guards; git diff --check passed.
No Python package modules or guarded mutation anchors changed.

## Typed remote posture audit library — 2026-10-09

`go/audit` implements concrete Report/Posture/Finding types, finite severity/check
names, Source-compatible remote findings sorted by severity/check, and exact
human rendering. Its JSON decoder preserves missing/null/empty posture and
unknown-only objects, validates audited scalar types, ignores future fields,
and publishes no partial receiver on failure. Generic closed JSON values stay
inside decoding; domain structs contain no dynamic payloads. The supported
health schema deliberately rejects ambiguous types rather than reproducing
arbitrary Python Mapping truthiness.

Twenty-two Source profiles compare findings and rendered text; negative tests
cover invalid known types, integer overflow, receiver atomicity and invalid
severity. This is a standalone report consumer: HTTP fetching, local settings
auditing, live inspection and audit CLI composition remain pending. Findings
reflect report metadata and do not independently establish OS confinement or
persistence. No runtime defaults, dependencies or permissions change.

README canonical map and boundary explanation include the consumer; architecture
JSON annotation and generated HTML are updated together. Archify delivery passed
9/9 showcase checks, zero errors/warnings; artifact/specification byte counts
and SHA256 match the receipt. Visual review was not repeated: prior browser
inspection was denied. Correction rounds: 0.

Validation: focused audit race, full serialized Go tests, go vet ./..., and full
serialized Go race with cross-package coverage all passed. Full Python pytest,
191-file exporter check and 19 scanning guards passed in the preceding Source
contract commit. README outline and git diff --check passed. Existing CGO linker
warnings were nonfatal; Python invariant/guard checks were not repeated because
package modules and guarded mutation anchors are unchanged. No push.

Fresh Go statement coverage (2026-10-09; cross-package execution, merged blocks
by maximum hit count; Darwin arm64):

| Scope | Covered | Statements | Coverage |
| --- | ---: | ---: | ---: |
| ALL | 27,359 | 30,360 | 90.12% |
| audit | 94 | 97 | 96.91% |
| agent | 8,886 | 9,887 | 89.88% |
| httpapi | 1,617 | 1,806 | 89.53% |
| launcher | 196 | 236 | 83.05% |
| sandbox | 157 | 189 | 83.07% |
| shell | 383 | 406 | 94.33% |
| sqlitestate | 521 | 625 | 83.36% |

Gate: go test -race -p=1 -count=1 -timeout=300s -coverpkg=./...
-coverprofile=/tmp/mini-loop-current-cross-coverage.out ./... (exit 0).
Python coverage and mutation coverage were not refreshed. This measures
executed statements, not migration acceptance or completion.

## Remote audit CLI Source contracts — 2026-10-09

`python-audit-remote-cli.json` captures 22 actual `mini_loop.audit.main`
executions against a loopback HTTP server. Each case fixes GET /healthz without
an Authorization header, trailing slash removal, printed build/pid metadata,
full rendered findings and exit status. Medium-only/missing posture exits 0;
high/critical findings exit 1; missing URL is usage status 2. Python Source
remains unchanged. A fixture test checks the actual command and wire request.

Validation: focused Source CLI fixture test and full Python pytest passed
(2166 passed, 28 skipped, 24 subtests; 3 existing warnings). Source exporter
--check passed for 192 files; verify_scans passed all 19 anchored guards.
git diff --check passed. No package modules or mutation anchors changed.

## Native remote audit fetching and CLI — 2026-10-09

`audit.Fetch(ctx, base, client)` reads public /healthz, trims trailing slashes,
uses a ten-second context budget and returns a concrete Report. Borrowed clients
retain their timeout/transport/redirect policy; the helper does not configure
environment credentials or mutate the client. Build is optional text, pid is an
optional integer; invalid types/overflow fail atomically at the JSON boundary.
Non-2xx, transport/read faults and invalid documents fail without publishing
findings. A 1 MiB response cap is an explicit Go bound beyond Source's unbounded
JSON load; it counts streamed bytes before decoding.

The independent `go/cmd/miniloop-audit --url` command prints Source-compatible
header/findings and returns 1 for high/critical findings, 0 otherwise. Operational
errors exit 1; invalid/missing arguments exit 2 with usage on stderr (Source's
missing-URL usage uses stdout). No-argument local auditing remains unimplemented
and is explicitly refused. Signal cancellation propagates to HTTP. Remote audit
does not construct a manager, resolve model credentials or create sessions.

Twenty-two Source CLI profiles are exercised through real native loopback HTTP,
including slash trimming, header metadata, findings, exact text and exit status.
An actual Launcher health test proves all default findings and zero sessions.
Cancellation, the ten-second deadline, borrowed-client preservation, failed HTTP,
invalid syntax/types/overflow, trailing JSON, oversized streamed bodies and
missing metadata are covered. An actual built binary successfully audited a
loopback server, printed the fixed build/pid and returned 0 with no findings.

README canonical Mermaid now includes the independent audit client and its
public health read. Architecture JSON aggregates that client under the Go entry
component and explains this grouping; generated HTML updated together. Archify
delivery passed 9/9 showcase checks, zero errors/warnings; exact specification
and artifact bytes/SHA256 match the receipt. Visual review was not repeated
because prior browser inspection was denied; correction rounds 0. No dependency,
server default, authority, schema or health behavior changes. Local/live auditing
and remaining full migration acceptance stay open.

Final validation: focused audit/command race tests, full serialized Go tests,
full serialized Go race tests and go vet ./... passed. Actual native binary
HTTP smoke passed. Full Python pytest, 192-file exporter --check and 19 scanning
guards passed in the preceding Source contract commit. README outline and
git diff --check passed. Existing CGO linker warnings were nonfatal. Python
package invariant/guard checks and coverage were not refreshed: package modules
and guarded mutation anchors are unchanged. No push; full migration remains open.

## Local configuration audit Source contracts — 2026-10-09

`python-audit-settings.json` captures 24 actual `audit_settings` calls over real
Settings and filesystem states. Inputs cover exact loopback aliases, public and
other loopback addresses, absent/blank/single/multiple/invalid authentication,
fake models, trajectory recording/content combinations, world-writable directory
and file roots, protected roots, symlink targets, dangling/cyclic links and
non-directory parents. Findings preserve Source's insertion order; only the
full live audit later sorts by severity/check. Exact text/rendering are frozen.

Source Settings constructs its workspace before auditing. Fixtures therefore
construct real Settings and mutate the root afterward, so missing/replaced/link
observations describe filesystem drift. Source Path.exists treats ELOOP and
ENOTDIR as absent; a live Source probe and the fixture pin this behavior. Two
independent scratch roots normalize only absolute temporary prefixes. Source
package code is unchanged. Authentication failures are retained as Source errors
and must not be converted into apparently clean findings.

Validation: focused Source fixture and full Python pytest passed (2167 passed,
28 skipped, 24 subtests; 3 existing warnings). Source exporter --check passed
for 193 files; verify_scans passed all 19 anchored scanning guards. Two scratch
roots reproduce the fixture. git diff --check passed. No Source package modules
or guarded mutation anchors changed.

## Native configuration audit library — 2026-10-09

`audit.Settings(config.Settings, environment)` implements all Source
configuration-only checks: unauthenticated bind (critical public / medium exact
loopback aliases), fake model info, world-write permission high, and trajectory
content medium. It retains insertion order, exact findings/remedies and rendering
for all supported profiles; it does not apply the full live audit's sorting.
The actual existing HTTP authentication loader supplies configured/refusal
outcomes. Authentication failures return no findings; shared-token denial uses
the existing native loader's shorter error text, so the differential test checks
refusal outcomes rather than claiming identical exception text.

A nil environment snapshots process variables; a nonnil empty map stays empty.
Selected config fields are read directly, with no provider, session or manager
construction. A real stat follows symlink targets and suppresses Source exists'
ENOENT/ENOTDIR/ELOOP/EBADF absence cases; unexpected stat faults return an error
without partial findings. Missing roots are not created. Existing writable
regular files are checked just like directories; chmod, filesystem writes and
credential resolution are not performed. Domain/service payloads remain
concrete config/Finding types and string-only environment maps.

Twenty-four actual Source cases verify real permissions, symlink targets,
dangling/cyclic links, non-directory parents, auth outcomes, trajectory flags,
exact findings/text and ordering. Additional native checks cover nil versus
empty environment, unexpected stat failure and no missing-root allocation.
An actual config.Load -> audit.Settings smoke verifies public-bind/fake/content
check order with a still-absent workspace.

README boundary/map and review baseline are updated. Canonical Mermaid labels
the library-only configuration/filesystem observer separately from remote
command behavior. Architecture JSON annotation and generated HTML update
together: Archify 9/9 showcase checks, zero errors/warnings, artifact/specification
bytes and SHA256 verified. Visual review was not repeated because prior browser
inspection was denied; correction rounds 0. No runtime default, dependency,
authentication policy, persistence, CLI behavior or health behavior changes.
Full live audit and local CLI integration remain open, along with default
diagnostics and wider migration acceptance.

Final validation: focused audit race, full serialized Go tests, full serialized
Go race tests and go vet ./... passed. Real config.Load -> audit.Settings smoke
passed with expected ordered findings and no workspace allocation. Full Python
pytest, 193-file exporter check and 19 scanning guards passed in the preceding
Source contract commit. README outline and git diff --check passed. Existing
CGO linker warnings were nonfatal. Python package invariant/guard checks and
coverage were not refreshed because package modules and mutation anchors are
unchanged. No push; the complete port remains active.

## Full deployment audit Source observation contracts — 2026-10-09

`python-audit-live.json` freezes actual full `audit(manager)` findings/rendering
for sixteen real SessionManagers. Cases cover default and empty catalogues,
parallel-safe read/write/exec/external/unclassified risks independent of advisory
readonly, absent/null/complete/partial/case-sensitive masking, cached unresolved
and short secrets, unavailable and empty-reason sandbox metadata, no-shell
severity lowering, actual SQLite/Seatbelt configuration, real persistence write
faults, workflows, merged multi-session MCP counts and component problem logs.
Generic channels include plain lists and churning bounded logs.

Inputs separately capture configuration findings and typed component metadata/
ledger observations from actual Source objects. Names/flags/counts represent
observations, not grants. Secret cache reporting does not resolve lazy values.
The hardened profile still reports shell resource limits; selected sandbox
confinement is not a CPU/memory/disk bound. The full function sorts by severity
and check. Only temporary root prefixes are normalized; two independent roots
reproduce the snapshot. Source package modules are unchanged.

Validation: focused actual-manager Source fixture test and full Python pytest
passed (2168 passed, 28 skipped, 24 subtests; 3 existing warnings). Source
exporter --check passed for 194 files; verify_scans passed all 19 anchored scanning
guards. Two independent roots reproduce full snapshots. git diff --check passed.
No Source package modules or guarded mutation anchors changed.

## Typed full deployment finding composition — 2026-10-09

`audit.ComposeLive` combines configuration findings with concrete
LiveObservation/CatalogObservation/ToolObservation/ProblemChannel structs.
Named protocol tools, finite agent risks, secret names and selfaudit Ledger/
exact problem counters carry observations; there are no dynamic payloads or
reflection in the native composition. It implements every Source full audit
finding branch and sorts by severity/check. Nil catalogue remains default shell,
while an explicit empty catalogue lowers confinement/journal severity and omits
resource limits. A present-but-empty sandbox reason remains an unavailable
finding, distinct from remote audit's truthiness rule.

Credential-shape detection reuses pinned Python uppercasing and fnmatch patterns;
registration is exact-case, names are sorted and previews stop at three. Values
are never rendered or resolved. Cached unresolved/short names preserve source
order. Persistence faults remain findings distinct from null-store configuration.
Specific MCP/cron/skills ledgers use Source remedies; generic channels sort by
name and exclude private, cron and skills attributes. Count summaries, distinct
problem counts, extra-problem suffixes and unbounded comma-grouped churn totals
are preserved. Copying configuration and channel containers prevents sorting
from mutating caller observations.

The sixteen actual manager profiles compare complete findings and exact rendering
under native Go. Additional cases reject unknown risk/severity, failed ledgers,
missing summaries/totals and duplicate/empty channels without partial findings.
A 30-digit churn total remains exact; private/specific-channel exclusion and
input immutability are verified. This is pure library composition. Native manager
observation, collection of every native problem source, and local audit CLI
startup/shutdown still need implementation; no HTTP/model-tool path is registered.

README map/boundary and baseline review distinguish this status. Architecture
JSON annotation and generated HTML are updated together. Archify delivery passed
9/9 showcase checks, zero errors/warnings; exact specification/artifact byte counts
and SHA256 match the receipt. Visual review was not repeated because prior
browser inspection was denied; correction rounds 0. No dependency, runtime
default, authority, persistence, health or CLI behavior changes. Wider migration
acceptance and default diagnostics remain open.

Final validation: focused native audit race, full serialized Go tests, full
serialized Go race tests and go vet ./... passed. Full Python pytest, 194-file
Source exporter check and all 19 scanning guards passed in the preceding
contract commit. README outline and git diff --check passed. Existing CGO
linker warnings were nonfatal. Python package invariant/guard checks and
coverage were not refreshed because package modules and mutation anchors are
unchanged. No push; native observation and full migration acceptance remain open.

## Native manager deployment observation and SDK audit — 2026-10-09

Base: `724899f`. `SessionManager.ObserveDeploymentAudit(ctx)` now snapshots
manager/template bindings and all published session handles. Agent owns the
concrete DeploymentAuditObservation schema; existing audit type names remain
aliases, avoiding a package cycle without dynamic payloads or reflection.
`audit.Manager` composes selected settings, one environment snapshot and real
manager observations into Source-compatible full findings. Manager ownership
and shutdown remain caller responsibilities.

The collector reads explicit/default catalogue semantics, sandbox factory
metadata, cached secret Names/Unresolved/ShortValues, persistence write faults,
cron/skills and manager-held actions/approvals/bus/raw-memory/trajectory ledgers.
It aggregates retained MCP entries from every published session, matching Source
count replay and FIFO retention in bulk, including 30-digit counters; evicted
source history is not reintroduced. Per-session task/gate/owner diagnostics
remain in existing self-audit. There is no 100-session observation truncation.

Callbacks run outside manager locks; returned containers, summaries, counters,
reasons and failures detach from injected storage. Cancellation, unobservable
custom Bash factories, callback panics and failed ledgers cannot become clean
findings. Component snapshots are not an atomic fleet transaction. This
privileged local API provides no owner admission and has no health/model-tool
binding. It creates no session/task board/owner bundle, executes no provider or
tool, visits no historical recording and resolves no secret value.

Thirteen actual Source profiles match exact native findings and rendering using
real native managers. Remaining Source fault/ledger profiles stay covered by
pure composition; native tests separately exercise actual persisted write
failure, action/approval diagnostics, multi-session MCP repeats, bounded replay,
concurrent snapshots, unlocked callbacks, injected ledger failures and detached
fields. Native persistence error class labels retain Go identity rather than
pretending to be Source RuntimeError. No new dependencies or feature defaults.

README canonical map/boundary/baseline, extension seam and architecture source
are updated. Generated HTML passes Archify 9/9 showcase checks, zero errors or
warnings; specification/artifact hashes and byte counts match the receipt.
Visual review was not repeated because prior browser inspection was denied;
correction rounds 0. Local audit CLI startup/shutdown, default diagnostic
selection and full migration acceptance remain open.

Final gates: focused TestDeployment/TestManagerAudit/TestComposeLive race tests,
full serialized Go tests, full serialized Go race tests, go vet ./... and full
cross-package coverage tests all passed. README outline, typed-payload search
and git diff --check passed. Existing Darwin CGO linker warnings were nonfatal.
Python pytest/exporter/invariant/scanner/guard gates were not repeated: no
Python package, export, scanner, mutation anchor or Go default-tool type changed.
The preceding Source contract delivery passed Python pytest and all 194 exports.
No push.

### Current coverage after manager deployment audit

Darwin arm64, implementation base `724899f` plus this slice. Cross-package
statement coverage merges identical blocks by maximum hit count.

| Scope | Covered | Statements | Coverage |
| --- | ---: | ---: | ---: |
| ALL | 27,699 | 30,729 | 90.14% |
| audit | 311 | 321 | 96.88% |
| agent | 8,992 | 10,003 | 89.89% |
| httpapi | 1,616 | 1,806 | 89.48% |
| launcher | 196 | 236 | 83.05% |
| sandbox | 157 | 189 | 83.07% |
| shell | 383 | 406 | 94.33% |
| sqlitestate | 521 | 625 | 83.36% |

Gate: `go test -p=1 -count=1 -timeout=300s -coverpkg=./...
-coverprofile=/tmp/mini-loop-manager-audit-cover.out ./...` passed.
The overall 90.14% is executed-statement coverage, not migration acceptance.
Python and mutation coverage were not refreshed. Full race ran separately
and passed without coverage instrumentation.

## Local deployment audit CLI Source contracts — 2026-10-09

Base: `964907d`. The 195th export runs actual `mini_loop.audit.main` against
14 local profiles: default/public/authenticated/exposed/content/world-write,
full features, workflow setting, ignored unknown arguments/invalid PORT, real
provider, missing provider key, invalid settings and shared-token refusal.
Settings loading, client construction, manager construction and audit/render
remain Source implementations. A constructor wrapper only records real manager
instances; harness cleanup stops managers and closes clients after observation.
All successful construction profiles have zero sessions. Root paths alone are
normalized after resolving the scratch root. Two independent roots reproduce
the frozen corpus.

Source treats non--url arguments as local mode, ignores PORT, and does not pass
the workflow environment selection to the local manager. Source settings create
the workspace before client construction. Source AsyncAnthropic accepts a
missing API key at construction; existing Go provider refuses it immediately.
That difference remains explicit implementation work, not a passing parity case.
Full Go CLI features still require the unresolved default diagnostic backend.
README architecture baseline reviewed; this contract-only slice changes no
runtime topology, dependency, authority or defaults.

Validation: focused two-root Source test, full Python pytest (2169 passed,
28 skipped, 24 subtests; 3 warnings), all 195 export checks, all 19 scanning
guards and git diff --check passed. JSON key order was corrected to the existing
exporter format before the final check. No Python package module/guarded mutation
anchor or Go implementation changed; Go/race/vet gates were not repeated.

## Native local deployment audit construction and CLI — 2026-10-09

Base: `c6e0cee`. No-URL `cmd/miniloop-audit` now captures one environment, loads
settings, builds actual native services through `launcher.AuditDeployment`,
composes full manager findings, stops its owned manager/transport and prints
exact Source rendering. High/critical findings or operational errors return 1.
Non-URL arguments select local mode and PORT is ignored, matching Source.
Source eagerly creates the workspace; the native audit constructor performs
that effect while general config.Load remains pure. Shared memory/trajectory/
skill construction uses the same private buildApp as the serving launcher.

Serving NewWithOptions preflight and actual App.Serve bind refusal remain intact.
The audit API returns findings rather than an App or listener and can report
public/no-auth configuration without serving it. No model, tool, listener,
restoration or live session is started. Owned manager/transport cleanup uses
a fresh ten-second context, independent of request cancellation; cleanup faults
return errors without partial findings. Injected stores/diagnostics retain caller
ownership. No dependency or normal serving default changes.

The Source workflow recipe now uses its correct configuration variable
MINILOOP_EXPERIMENTAL_WORKFLOWS and records the actual selected settings value.
It proves settings=true with manager=false in local main, rather than testing
an unknown environment variable. The audit constructor preserves that Source
selection; explicit SDK Options.WorkflowTools remains available.

Ten direct Source local profiles match native command findings/text/status,
including world-write permissions, open bind, authentication, credential-shaped
environment, real provider construction without HTTP calls, unknown args and
invalid PORT. A compiled native binary also matches Source output and terminates
without sessions. The full-feature SDK profile matches using the explicitly
injected diagnostics seam without calling Scope/Diagnose; caller diagnostics
and actual SQLite stores remain open. Serving bind guards and cancellation are
verified. Operational invalid-settings/authentication errors become native code 1
and stderr, rather than uncaught Source exceptions.

Two known differences remain: full default CLI features need the unresolved
default diagnostics backend; native provider construction requires credentials
while Source allows missing-key construction and defers validation. These are
explicit refusal/error cases, not passing Source parity profiles. Wider migration
acceptance remains open.

README canonical Mermaid/boundary/review baseline, extension and hardening notes
and architecture source are updated together. Generated HTML passes Archify
9/9 showcase checks, zero errors/warnings; hashes and byte counts match receipt.
Visual inspection was not repeated because previous browser review was denied;
correction rounds 0. No push.

Final gates passed: targeted local command/SDK race (including real native binary),
full serialized Go tests, full serialized Go race, go vet ./..., full Python
pytest (2169 passed, 28 skipped, 24 subtests, 3 warnings), 195-file Source exporter
and all 19 scanning guards. README outlines, typed-payload search and
git diff --check passed. Existing Darwin CGO linker warnings were nonfatal.
Python package invariant/guard checks were not repeated because package modules
and mutation anchors are unchanged. Coverage was not refreshed; the prior
90.14% measurement belongs to the preceding manager observation implementation.

## Deferred native provider missing-credential admission — 2026-10-09

Base: `798a2a6`. The 196th Source export observes fixed SDK 0.107.1 constructor
and request phases for missing/empty keys, streaming absence and non-streaming
token-ceiling priority. All four clients construct, no transport call occurs,
and TypeError/ValueError class and complete diagnostic text are frozen.
Credential discovery paths are redirected through a temporary Path.home
filesystem seam with environment cleared; no authentication guard is mocked.
The local CLI corpus now uses the same home-path isolation and remains stable.
ANTHROPIC_CONFIG_DIR is not an isolation substitute: the pinned SDK treats it
as explicit profile selection; that attempted recipe was replaced.

Native New/NewStreaming now permit empty APIKey, preserve `<absent>` identity
and defer missing-authentication admission until after request/body validation
and non-streaming token-ceiling refusal, before HTTP or retries. Finite
FailureCredential carries Source TypeError and existing ModelFailureOther
projection; HTTP AuthenticationError remains a separate status failure. Source
token-ceiling diagnostic text is now complete. Empty-key redaction does not
insert markers between endpoint or diagnostic characters. Existing constraints
on nonempty keys, endpoints, wire sizes and listener authentication stay intact.
No dependency, profile/OAuth/federation discovery or ambient key rotation added.

All four Source request profiles match native error class/text and zero HTTP/
retry/progress effects. A real uncredentialed launcher constructs an empty fleet,
then an actual managed model turn projects the refusal through existing [Error]
text without contacting HTTP. The previous early CLI missing-key refusal test
was removed because its assumed behavior changed; public bind/configuration
refusal tests remain. Eleven local audit command profiles now match Source,
including missing-key construction; full default diagnostics remains open.

README architecture review baseline, canonical provider label/boundary,
extension/hardening notes and architecture specification are updated together.
Wider G0-G7 migration acceptance remains open.

Final validation passed: focused provider/local-command/actual managed launcher
race tests; full serialized Go tests and full serialized Go race; go vet ./...;
full Python pytest (2170 passed, 28 skipped, 24 subtests, 3 warnings); all 196
Source exports; all 19 scanning guards; README outline, typed-payload search and
git diff --check. Archify passes 9/9 showcase checks with zero errors/warnings;
spec/artifact hashes and byte counts match receipt. Visual review was not
repeated because previous browser inspection was denied; correction rounds 0.
Existing Darwin CGO linker warnings were nonfatal. Python package invariant/
guard gates and coverage were not refreshed: package modules/mutation anchors
are unchanged. No push; full migration/default diagnostics remain unfinished.

## Dotenv parsing/interpolation/application Source contracts — 2026-10-09

Base: `3b529b8`. Actual Python config imports load_dotenv(override=True);
Go still reports no dotenv discovery. Snapshot 197 freezes installed
python-dotenv 1.2.2 through real parse_stream, DotEnv.dict and load_dotenv over
38 profiles (19 recipes with both override settings). This is a prerequisite
for native parsing and startup discovery, not implementation of either.

Recipes cover valueless versus empty values, export modifiers, quoted keys,
comments, literal comment markers, recognized/unknown escapes, multiline values,
CR/LF/CRLF, Unicode whitespace/keys, BOM, malformed-line recovery, trailing
garbage, duplicate insertion order, variable/default syntax, environment/prior
binding priority and disabled loading. Bindings retain nullable key/value,
exact original text, original start line and error flag. Values preserve first
key insertion order with final binding values. Environment application captures
returned load status, class/message failure and exact partially applied map.

A parser-accepted key containing '=' triggers ValueError during environment
application after earlier variables were written. This partial effect remains
frozen instead of imposing atomic application. Valueless bindings can make
load_dotenv return true without writing any variable. PYTHON_DOTENV_DISABLED
stops load_dotenv while pure DotEnv parsing/interpolation remains observable.
Only logging warnings are silenced; parser, resolver and environment application
are actual Source calls. Environment is isolated and restored per recipe.

README architecture baseline/map reviewed; no runtime topology/default/authority
change, new dependency or native source change. Native parser, filesystem
discovery, file decoding and actual CLI activation remain pending. Full default
diagnostics and wider migration acceptance remain open.

Validation passed: focused real parser/interpolation/application test, full
Python pytest (2171 passed, 28 skipped, 24 subtests, 3 warnings), all 197 Source
exports, all 19 scanning guards, README outline and git diff --check. No Go
implementation/default-tool type or Python package module/mutation anchor
changed; Go/race/vet and Python invariant/guard gates were not repeated.
Coverage was not refreshed. No push; native dotenv remains unfinished.

## Native dotenv text, variable and environment phases — 2026-10-09

Base: `5df7222`. New dependency-free dotenv package implements concrete Name/
Binding/Original/Variable/Environment/Application/Failure structures. Parse
preserves nullable keys/values, exact original spans and start lines, malformed
line recovery, Python whitespace, export prefixes, quoted/unquoted keys,
comments, recognized/unknown escapes and multiline values. Regex-style escaped
final quote backtracking and recovery's CR-before-CRLF alternation are explicit.
Invalid UTF-8 is refused at the native scalar text boundary.

Resolve expands Source ${name}/${name:-default} syntax with prior values versus
external environment priority, including valueless shadows, empty defaults,
self references and duplicate first-key order/final values. Apply returns a
detached map, honors disabled-loading case folding for its finite tokens,
skips valueless/existing-preserved values and retains writes before invalid
name/NUL faults. Loaded false and missing loaded-on-failure remain distinct.
No parser/resolver/application phase mutates process environment or input
containers; resolved value pointers detach from raw bindings.

The Source corpus expands from 38 to 48 profiles with actual escaped-quote
backtracking, bad-line CRLF recovery, NUL name/value faults and Unicode long-s
disabled loading. All raw bindings, resolved ordered values, application status,
fault class/message and exact partially applied maps compare with native Go.
Additional native checks cover scalar refusal, empty input, immutable inputs
and detached maps/value pointers.

This is the full text/environment library prerequisite. File discovery, file
decoding, config/CLI activation and default diagnostics remain unfinished.
DotEnvDiscovery stays false and config.Load remains pure. README canonical
Mermaid/boundary/review baseline and aggregate startup/SDK architecture annotation
are updated together. No dependency, authority, persistence or runtime default
change. Wider migration acceptance remains open; no push.

Validation: full Go ordinary and race suites (`-p=1 -count=1 -timeout=300s`)
and `go vet ./...` pass. Python full suite: 2171 passed, 28 skipped, 24
subtests, 3 warnings. Contract export check: 197 files current. All 19 source
scanning guards pass. Focused native dotenv race: 48 Source profiles pass.
README outline and `git diff --check` pass; regenerated Archify artifact passes
9/9 checks with zero errors/warnings and receipt hashes/byte counts verified.
Visual review was not repeated because the previous browser review was denied;
zero visual correction rounds. Python package invariants and mutation guards
were not rerun: this slice changes a contract helper/test, not package modules
or mutation anchors. No push.

Fresh cross-package Go statement coverage after this slice: 27949/30998 =
90.16% (`go test -p=1 -count=1 -timeout=300s -coverpkg=./...`), merging identical
source blocks across test binaries. Dotenv: 202/205 = 98.54%; audit: 97.20%;
agent: 89.89%; launcher: 80.61%. This measures ordinary Go test execution;
Python and mutation coverage were not refreshed.

## Dotenv discovery and file decoding Source contracts — 2026-10-09

Base: `2bc7722`. Snapshot 198 uses installed python-dotenv 1.2.2 against real
isolated directories/files/symlinks/FIFOs. Ten discovery profiles cover caller
file versus cwd selection, missing wrapper frames, interactive/debugger/frozen
selection, nearest-file precedence, directories, dangling links and FIFO
admission. Real default load_dotenv calls also capture selected environment
values; explicit find_dotenv(usecwd=True) does not change the later default
loader's caller-file origin. FIFO is tested for discovery only, without opening
it for reading. Two missing-file profiles pin empty result versus OSError and
three real walk profiles pin directory/file origins and missing-start failure.
Temporary paths are normalized only at the observation boundary; the filesystem
and discovery/parser/loader functions are actual Source calls.

Thirty-six file-read profiles pin ordinary UTF-8, universal newline conversion
(including inside quoted multiline values), retained BOM, invalid/truncated
UTF-8 with exact exception messages, empty/missing/directory inputs, linked and
dangling files, file-before-stream selection, fallback stream, disabled loading
before decoding, explicit latin-1/utf-8-sig codecs, interpolation priority and
partial environment writes on invalid names. dotenv_values always resolves with
override=true; load_dotenv uses each profile's override selection. Decode faults
leave the environment unchanged; disabled load can return false while the
independent dotenv_values observation still reports a decode fault.

No native discovery/file reading or CLI activation is claimed. DotEnvDiscovery
remains false. Native caller-origin mapping, actual FIFO reading, file codecs,
startup selection and default diagnostics remain open for subsequent slices.
README architecture map reviewed and review baseline updated; no runtime
structure/default/authority or dependency change. No push.

Validation passed: focused actual filesystem contract test; full Python suite
2172 passed, 28 skipped, 24 subtests, 3 warnings; all 198 Source exports current;
all 19 scanning guards; README outline and git diff --check. No Go runtime or
Python package module/mutation anchor changed, so Go ordinary/race/vet and
Python package invariants/mutation guards were not rerun. Architecture source
and generated map need no regeneration because topology is unchanged. Coverage
was not refreshed for this Source-only slice. Full migration remains open.

## Native dotenv discovery, decoding and file loading — 2026-10-09

Base: `1454b45`. Concrete DiscoveryContext/DiscoveryOptions/FileOptions,
finite Encoding/FailureClass and named FileValues extend the native dotenv SDK.
Find selects explicit caller files (skipping missing frames), cwd, interactive,
debugger or frozen origin and searches actual logical ancestors. Directory and
dangling entries are skipped; regular files/followed links/FIFOs are admitted.
Missing lookup and missing starting-path outcomes match Source. Missing caller
origin is an explicit native refusal, never inferred from Go build-machine paths.

Values and LoadFile implement file-first/fallback-stream selection, optional
Find, strict UTF-8 decoder faults with exact captured Source messages, BOM,
universal newline conversion, empty/missing/directory inputs, explicit finite
UTF-8-signature/Latin-1 codecs and literal interpolation selection. Values always
resolves with override=true. LoadFile checks disabled loading before any
filesystem discovery/read/decode and honors the requested override. It returns
detached maps: decode faults write nothing, while invalid-name application
faults preserve preceding writes. No API changes cwd or process environment.
These privileged local SDK reads are not a remote route or tool execution seam.
Other Python codecs are outside the finite native SDK surface; production
Source config uses default UTF-8. FIFO reads wait for producer/EOF without a cap.

All ten discovery/default-load, two missing-file, three walk and forty file-read
Source profiles compare directly. Source corpus adds actual interpolate=false
and NUL-path failures (36→40 read profiles). Additional real FIFO execution
verifies producer close, interpolation and CRLF normalization. Native checks
cover explicit empty path, missing caller origin, unknown-codec refusal,
absolute filenames, immutable inputs and unchanged process environment.

README canonical Mermaid/boundary/review baseline and interactive aggregate SDK
annotation update together. CLI/config do not invoke this library and continue
to report DotEnvDiscovery=false. Startup-origin mapping/activation, default
diagnostics and full migration acceptance remain open. No new dependency or
runtime default/authority/persistence change; no push.

Validation passed: focused native race contracts/FIFO tests; full Go ordinary
and race suites (`-p=1 -count=1 -timeout=300s`), plus go vet ./...; Python full
suite 2172 passed, 28 skipped, 24 subtests, 3 warnings; all 198 Source exports
current; all 19 scanning guards; README outline and git diff --check. An empty
caller-file guard was fixed after initial validation and all Go gates rerun.
CGO linker LC_DYSYMTAB warnings were nonfatal. No Python package module or
mutation anchor changed, so Python invariant/guard gates were not rerun.
Archify regenerated artifact: 9/9, zero errors/warnings; exact hashes and bytes
verified. Visual review was not repeated because prior browser review was denied;
zero visual correction rounds. Coverage was not refreshed. No push.

## Source config import and native startup environment preparation — 2026-10-09

Base: `0aee7e0`. Snapshot 199 loads an unmodified copy of actual
python/mini_loop/config.py under an isolated existing module filename and fresh
module identity, then calls its actual load_settings factory. All environment,
cwd, module and interactive/debugger/frozen observations restore afterward.
No dotenv finder/parser/loader, auth cleanup, Settings or mkdir is replaced.
Eighteen profiles capture nearest/caller-parent/root versus cwd origins,
interactive/debugger/frozen origin, override/interpolation, token cleanup for
nonempty/empty/whitespace base URL, disabled loading, decode and partial import
faults, Settings parsing/bounds failure, and factories after import.

Actual import runs dotenv override=true before custom-base-URL token cleanup.
Partial dotenv application faults retain earlier MODEL_ID/BASE_URL writes but
leave AUTH_TOKEN present; disabled loading still permits later cleanup. Settings
faults occur after successful import/cleanup but before workspace mkdir. A later
Settings call observes later environment changes without loading changed dotenv
contents again; authentication cleanup likewise belongs to import, not each
Settings call. Source successful Settings construction creates workspace roots;
this side effect is recorded independently from environment effects.

Native config.PrepareEnvironment composes the existing dotenv SDK with that
import-phase order over explicit environment/origin observations. Named
PreparedEnvironment privately retains detached complete/partial environment
values; Reveal returns another detached map and ordinary String/GoString exposes
no values. Import fault class/message and exact maps compare directly across all
18 profiles. Subsequent pure Load settings values match successful Source
profiles; parsing/bounds refusal is checked semantically, retaining existing
native Load diagnostic wording. Preparation/Load create no workspace; eventual
launcher construction owns that previously implemented effect. They change no
process environment/cwd and preserve input containers.

README canonical Mermaid/boundary/review baseline and interactive aggregate SDK
annotation update together. CLI default origin and actual process-environment
commit remain pending; no CLI/remote route/tool invokes preparation yet and
DotEnvDiscovery remains false. These are prerequisites for actual command
startup activation, not completion of it. No dependency or default/persistence
change. Default diagnostics and full migration acceptance remain open; no push.

Validation passed: focused Source module test and native config race contracts;
full Go ordinary/race suites (-p=1 -count=1 -timeout=300s), go vet ./...; full
Python suite 2173 passed, 28 skipped, 24 subtests, 3 warnings; all 199 Source
exports current; all 19 scanning guards; all 378 existing Python mutation guards
load-bearing. The foreground-interrupt negative case took an extended wait but
completed and caught its mutation; no verifier/test change was needed. Mutation
verification uses temporary copies and does not establish Go mutation coverage.
No Python package module changed, so Python package invariants were not rerun.
README outline and git diff --check pass. Archify artifact: 9/9, zero errors or
warnings, exact receipt hashes/bytes verified. Visual review not repeated because
prior browser review was denied; zero visual correction rounds. Nonfatal CGO
linker warnings retain their existing boundary. Coverage not refreshed; no push.

## Executable dotenv startup and child environment propagation — 2026-10-09

Base: `24822d1`. Both executable mains now invoke internal/commandenv.Initialize
before dispatch, signal hooks and runtime construction. Named InitializedEnvironment
privately owns a snapshot, reveals detached maps and reports pre-load enabled versus
disabled discovery. Preparation uses Source frozen-program origin (cwd/ancestors)
for compiled Go commands. This is an explicit deployment-origin adaptation from
ordinary Python source's config-module directory, not a claim those origins are
identical. SDK callers retain explicit caller-file/interactive/debugger/frozen
contexts. No build-machine source path is guessed and no Python file is required.

Initializer captures process variables once, performs native override=true and
ordered auth cleanup, then publishes changed/removed variables to process environment.
Prior writes survive an import-stage fault, which returns before command handling
or runtime creation. Complete publication ensures default shell/background/MCP
factories observe prepared values. Remote audit and usage dispatch also follow
initialization, like Source package import. Signal hooks start after preparation,
so a blocked startup FIFO can still be terminated. No discovery reload follows.

Eighteen independent process probes compare exact Source prepared effects and actual
inherited child environment. Ordinary Source caller-profile probes set native cwd
at the equivalent package origin; Source cwd/frozen profiles use their recorded cwd.
Existing explicit SDK tests continue to prove the original Source origin selection.
Actual main subprocess tests prove file override/credential-free dump, disabled
status, import error before help/usage, local audit using file fake mode while
ignoring PORT, no runtime roots on early failure, HTTP→auto-mode Bash inherited
MODEL_ID/custom values and removed AUTH_TOKEN, graceful shutdown and startup FIFO
termination. Pure execute snapshot helpers and SDK Load/inspection remain isolated.
CLI dump now performs executable preparation but still creates no runtime/roots
or model probe; enabled/disabled DotEnvDiscovery differs from pure SDK false.

README canonical Mermaid, boundary/review baseline, current startup docs and
interactive aggregate entry annotation update together. No dependency added.
Default diagnostics/full-feature CLI and full migration acceptance remain open;
no push. This implements dotenv command activation, not completion of the port.

Validation passed: focused race for commandenv/config/dotenv and both commands;
full Go ordinary/race suites (-p=1 -count=1 -timeout=300s), go vet ./...; standalone
compiled CLI binary dotenv dump smoke; Python full suite 2173 passed, 28 skipped,
24 subtests, 3 warnings; all 199 Source exports; all 19 scanning guards; all 378
existing Python mutation guards. Those mutation guards do not establish Go
mutation coverage. No Python package module changed, so Python package invariants
were not rerun. README outline and git diff --check pass. Archify regenerated
artifact passes 9/9 with zero errors/warnings; exact receipt hashes/bytes verified.
Visual review was not repeated because prior browser review was denied; zero
visual correction rounds. Nonfatal CGO linker warnings remain explicit.
Coverage not refreshed. No push; full migration remains open.


## 2026-10-09 coverage refresh after CLI dotenv activation

Measured code/test baseline: `57b232d` (runtime `f85a78f`). Fresh full Go tests
passed on Darwin arm64 with:
`go test -p=1 -count=1 -timeout=300s -coverpkg=./... -coverprofile=/tmp/mini-loop-current-go.cover ./...`
from `go/`. Source-location blocks were merged by maximum hit count, counting
each statement once across test packages.

| Scope | Covered / total statements | Coverage |
| --- | ---: | ---: |
| All Go packages | 28,088 / 31,202 | 90.02% |
| agent | 8,990 / 10,003 | 89.87% |
| sqlitestate | 521 / 625 | 83.36% |
| dotenv | 326 / 360 | 90.56% |
| config | 349 / 401 | 87.03% |
| launcher | 212 / 263 | 80.61% |
| audit | 312 / 321 | 97.20% |
| httpapi | 1,616 / 1,806 | 89.48% |
| internal/commandenv | 0 / 28 | 0.00% |
| cmd/miniloop | 45 / 69 | 65.22% |

Compared with the last 27,949 / 30,998 snapshot, this adds 139 covered and
204 total statements; the aggregate changes from 90.16% to 90.02%.
Subprocess execution counters are not merged into this profile. In particular,
commandenv is exercised by passing real-process tests despite its zero collected
statement count; this profile cannot measure those child-process executions.
This is statement coverage, not feature parity or mutation coverage.
Python coverage and mutation checks were not refreshed in this iteration.

The first coverage attempt failed because test helpers mixed JSON stdout with
Go coverage warnings on stderr. Commit `57b232d` separates the streams; the
successful rerun above supersedes that failed profile. Relevant race tests
(`./cmd/miniloop ./internal/commandenv`) and full `go vet ./...` passed.
Only tests and the README review baseline changed; runtime code and Python
sources were unchanged. Full race and Python suites were not rerun this iteration.
The profile remains temporary local output. No push.


## Native SQLite compaction restart proof — 2026-10-10

`go/sqlitestate/compaction_restart_test.go` composes the real manager, compress
and read_file tool batch, default archive/summary path and native SQLite backend.
Three cases cover a successful summary, provider failure and cancellation.
The archive must contain the complete paired three-message tool batch, including
the later read_file result. Successful compaction stores a summary in epoch 2
while retaining the exact paired epoch 1; the summary receipt is persisted with
its Source-compatible pre-rewrite epoch and archive path. Failure/cancellation
writes the archive but preserves the paired canonical epoch 1 without a summary
receipt. Tests close and reopen the database before explicit manager restoration.
They check owner/identity/run count, exact current and historical messages,
unchanged persisted event prefix, resumed provider request prefix, healthy lease
state, archive bytes and retained superseded epoch after the resumed turn.

This proves the composed small-history compaction/restart path. It does not
extend the proof to arbitrary large archival profiles or claim all G5/G7 work
complete. Default diagnostics selection and full migration acceptance remain open.
The README architecture review baseline was updated; runtime topology and
interactive map are unchanged. Runtime code, Python sources and contracts are
unchanged in this iteration.

Validation: narrow `go test -count=1 ./sqlitestate -run
TestCompactionNativeSQLiteRecovery`, full `go test -p=1 -count=1 -timeout=300s
./...`, full `go test -race -p=1 -count=1 -timeout=300s ./...` and `go vet ./...`
all passed from `go/`. Race linking emitted Darwin CGO LC_DYSYMTAB warnings;
all affected test binaries completed successfully. `git diff --check` passed.
Python tests, contract export, scanner/invariant/mutation checks and coverage
were not rerun because their source/targets and runtime implementation did not
change. No push.


## Native SQLite large-decision restart proof — 2026-10-10

`go/sqlitestate/decision_restart_test.go` runs both sqlite-reopen recipes from
`python-decision-replay.json` through actual managed decision tools and the native
StateStore-backed action journal. It compares the baseline 10,203-byte result
and maximum 524,288-byte result with Source byte lengths and canonical SHA-256
values. The test closes the database and creates a new manager before restoring
the session, checking exact archived transcript recovery. Restored permission
mode must be interactive: historical auto authority is not resumed. An explicit
current-operator auto selection then retries the same RunContext/tool-use identity;
the tool receives identical retained result bytes while the decision backend's
call count remains one. A subsequent readonly retry is refused and cannot return
the cached decision. Neither retry nor denial changes the settled action record.
Transcript pairing and restored persistence/lease health are checked afterward.
The integration explicitly uses an inert high-threshold in-memory compactor to
preserve the large payload; default compaction has its separate real-SQL restart
proof in the preceding entry.

This closes the previously pending native SQL comparison for snapshot 55's two
stored replay recipes. Aggregate shedding remains the Source in-memory journal
profile; this entry does not claim a new aggregate policy for durable storage.
Default diagnostics selection and complete G7 acceptance remain open. Only tests,
the README review baseline and plan/matrix evidence changed; runtime topology,
architecture artifacts, dependencies, Python sources and contract exports did not.

Validation: targeted `go test -count=1 -timeout=90s ./sqlitestate -run
TestDecisionNativeSQLiteReopenMatchesSource`, full `go test -p=1 -count=1
-timeout=300s ./...`, `go test -race -count=1 -timeout=300s ./sqlitestate` and
full `go vet ./...` passed from `go/`; `git diff --check` passed. The SQLite race
binary linked with the known Darwin CGO LC_DYSYMTAB warning and exited zero.
Full race, Python, contract-export, scanner/invariant/mutation and coverage checks
were not rerun: no runtime implementation or corresponding source/targets changed.
The initial test was terminated after identifying interactive approval waiting;
the final bounded test explicitly checks restored interactive mode before current
operator admission. No runtime authorization rule was changed. No push.
