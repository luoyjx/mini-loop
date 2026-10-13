# Python to Go parity matrix

## 2026-10-13 concrete event/default review — implementation pending

[Reviewed at `3449dc1`](docs/audits/go-port-remaining-boundaries-2026-10-13.md):
Go retains permission decisions in tool outcomes but has no Session permission
event variant/publication. Existing Source fixtures contain 20 permission rows
and 12 events; the native consumer ignores event fields, including eight
readonly `risk=exec` reasons (Go retains `exec`). Recovery construction also
omits the documented legacy `FALLBACK_MODEL_ID` selection. These are static
source/fixture findings under G2/G3/G5 and G4, not newly executed failure results.
Required next work is decision-time publication plus live/masked/stored event
parity, followed by constructor/default precedence with environment isolation.

The full-Python resumption and subsequent stability window were both refused
before launching tests because host memory pressure rose. No new full suite,
coverage or main integration is claimed. Runtime, dependencies, canonical
topology and original acceptance scope are unchanged; the review records the
new evidence and a built-in imagegen iteration.

## 2026-10-13 Provider IDNA Source prerequisite — not native acceptance

On `codex/go-provider-idna-boundary` from main `66f3645`, the new
[84-row Source corpus](go/testdata/python-provider-idna.json) covers 42 hostnames
in direct/SSE modes: 48 completed, 34 constructor `InvalidURL` refusals, two
request-stage `InvalidCodepoint` refusals. Actual versions and source hashes
preserve ASCII bypass, Unicode lower/IDNA, NFC, contextual rules and labelwise
Bidi. The new fixture has no native consumer. Existing two Unicode request-object
probes still fail; existing 32 ASCII and 40 HTTP Host controls pass.

[Validation receipt](docs/audits/go-provider-idna-source-2026-10-13.json): focused
Python 3 passed, Source export 253 current. Consumer inventory exits 1 with two
unreferenced fixtures: IDNA and default parser. Its other 252 passing-candidate
observations come from the prior ordinary Go result, not a new full Go run.
Full Python was resource-blocked before launch on host memory pressure;
invariants/scans/Archify remain pending. Main stays `66f3645` until remaining
validation passes. No new dependencies or Go runtime changes were made;
dependency approval and native consumption remain open. The accepted runtime
index below retains its prior complete gates and coverage.

## 2026-10-13 current inventory and resource-budget reconciliation

Accepted baseline is main `82b14bf`; original P0/P1/G0–G7 criteria are unchanged.
P0 inventory acceptance is satisfied by the README capability/test, settings,
HTTP, SDK, storage-schema and 157-name inventories. The namespace count is
52 name matches + 105 explicit mappings + zero missing bindings; semantic
acceptance is tracked separately in G1–G7 and the current index below.

[New performance evidence](docs/benchmarks/go-port-fake-http-resource-budget-2026-10-13.json)
uses three process pairs, 60 measured turns and 300 health requests per runtime
in total, plus six warmup turns. The nonexclusive host and startup outlier remain
recorded. The
[validation receipt](docs/audits/go-performance-resource-budget-2026-10-13.json)
records 4 focused Python passes, build/invariants/scans/diff passes, and full
Python **2253 passed, 28 skipped, 96 warnings, 24 subtests passed in 156.68s**.
Go/vet/race/coverage were not rerun; accepted role-policy coverage remains app
**89.41%**, all Go **89.16%**. The 130 body observations have individual
[owner-reviewed dispositions](docs/audits/go-body-boundary-dispositions-2026-10-13.json):
no open dynamic representation was found retained in repository-owned
domain/service fields in the recorded Darwin profile. Error propagation,
request-local SSE buffers, panic rethrow and generated CGO declarations are
explicitly distinguished. This does not declare all G0 or G7 complete.

## 2026-10-13 public role profiles and child capability isolation

Branch `codex/go-role-policy-profiles`, review baseline `fabe9bd`. Source capture
is committed as `0f5c95b`; implementation is `0671295`. Segmented delivery
evidence is recorded in the validation receipt.
Preserve the
original P0/P1/G0–G7 scope. An actual Source/native probe found the same `İ` role
selects a read tool in Python but a write tool in Go when an `i` profile follows.
The retained before logs prove a capability-selection divergence. This slice
captures 34 public policy selections, seven real fresh child runs and six
invalid capability registrations from the current Python implementation.

Role lookup uses the existing Python Unicode strip/lower implementation,
including dotted-I expansion and contextual final sigma. Ordered profile entries
first preserve raw mapping keys (last value at first insertion position), then
resolve normalized key collisions. Empty role/profile capability text is legal;
capability names are literal and are not lowercased or trimmed. Actual tool
registration still refuses empty/all-Python-whitespace capabilities. Valid tool
capabilities form a detached set, preserving first occurrence order in Go.
Uncategorized tools remain excluded. Unknown roles retain Source diagnostic
quoting. Child labels use lower on the original role without stripping whitespace.

Model `task` role admission remains closed, Explore runs readonly, every child
runs with peer authority without inherited human grants, and depth admission
still precedes construction. No new default, dependency, service or topology.
Only trusted callers supply custom role profiles. Selection does not bypass the
ToolGate or widen owner/workspace scope.

Acceptance: **required gates complete for this slice; full migration remains open**.
Validation is segmented across the retained `resumed` and `completion` cohorts:
12 earlier stages plus seven missing stages, 19 total. Eighteen pass; consumer
inspection exits 1 only for the unreferenced default Python parser. All recorded
Go/Python/JSON/dependency input fingerprints match across the segments. The two
README/EXTENDING status-reference edits between them are explicitly hashed;
this is not one uninterrupted 19-stage cohort.

Source export passes 252 files; 252 of 253 consumer candidates have observed
passing roots. Four type audits have zero findings: 465 production files / 1473
named types, 1206 public roots, 278 private roots and 172 package variables.
Body inventory retains 51 packages / 22288 declarations / 130 open observations.
Actual role selections, child effects, model result blocks and observer flags
pass. The initial mistaken new-test inference and its correction are retained.

Full ordinary Go, vet, complete race, full Python, invariants, 19 scan guards,
two Explore guards, one depth guard and diff checks pass.
Python: 2253 passed, 28 skipped, 96 warnings, 24 subtests passed in 111.61s (0:01:51).
Fresh Darwin statement coverage: app **89.41%** (33575/37551),
all Go **89.16%** (33941/38066), Provider **86.67%** (1522/1756).
Only the complete successful race profile contributes; interrupted profiles are
excluded. These statements do not cover Python, native C, child-process interiors,
branches, other platforms or complete migration acceptance.

Both earlier host-pressure stops and empty remaining-process lists are retained.
The continuation observed 120 consecutive normal-pressure seconds before running
only missing gates, with unchanged selectors/timeouts/limits. Heavy stages stayed
serial: nice 10, Go concurrency 2/build parallelism 1, serial pytest, 512MiB soft
Go target and 3GiB sampled task-tree RSS budget. Across all retained cohorts,
peak RSS was 590608 KiB (576.8 MiB); accepted-gate peak was
479488 KiB (468.2 MiB). Peak sampled CPU was 295.5%,
with two isolated above-250% samples, not a sustained 30-second breach. No new resource
stop occurred during completion. Sampling and GOMEMLIMIT are not OS hard caps,
and these results do not establish the cause of the earlier host pressure.

[Validation receipt](docs/audits/go-role-policy-profiles-2026-10-13.json),
[coverage](docs/audits/go-coverage-role-policy-profiles-2026-10-13.json),
[validation continuation image](docs/audits/go-role-policy-validation-infographic-2026-10-13.png)
and [exact prompt](docs/audits/go-role-policy-validation-infographic-2026-10-13.prompt.txt).
The first [role-change image](docs/audits/go-role-policy-profiles-infographic-2026-10-13.png)
is retained. Archify passed 9/9; no new rendered-browser acceptance.
Broader dynamic objects/custom mappings/concurrent mutation, original P0/P1/G0–G7,
public/default/host/platform/performance and historical timing closure stay open.

The base image stays unchanged. README/JSON changes annotate
existing session/catalogue selection; a sibling image highlights L2 tool resources and L3 Session/Context.
The two incomplete cohorts remain under `var/go-role-policy-profiles-2026-10-13/`
(`verified` and `resumed`); `completion` contains the successful missing gates.

## 2026-10-13 public RunContext constructors and provenance

Branch `codex/go-public-run-context`, review baseline `2ca0266`. Original
P0/P1/G0–G7 scope remains. The Source producer captures 70 actual `Agent.run`
cases: 14 default/human/peer constructor recipes crossed with five identity,
new-message and peer-derivation modes. The literal
`python-public-run-context.json` fixture compares optional/explicit-empty text,
capability membership, real tool contexts/results, event correlation and fresh
message/parent relationships; only generated UUID identities are normalized.
The retained `var/go-public-run-context-2026-10-13/native-before.log` proves the
old native constructor replaced explicit-empty human channel/stamp with defaults.

`HumanRunConfig.Channel` and `StampedBy` now use `*string`: nil selects the
existing defaults while supplied text, including empty text, is retained.
This is a Go caller source migration for either explicitly initialized field:
use pointers for supplied values and omit/use nil for defaults. New
`PeerRunConfig` and `PeerAgentRunContext` create fresh fixed peer authority.
The required delegated-by string may be empty; actor/stamp/parent are optional,
and the config accepts no approvals or authority selector. Peer derivation
uses the constructor and clears inherited approvals. `WithNewMessage` keeps
its existing fresh-identity, provenance and supplied-capability behavior.
Capability membership alone does not promote authority. Validation removes only
empty channel/stamp rejection; nonempty message identity and closed authority
checks remain. Snapshots cannot reconstruct live authority.

Final cohort: 1715 frozen program and architecture inputs remained unchanged
across 19 serial stages. Eighteen passed; the consumer inventory exited 1 solely
for the unreferenced default Python parser awaiting approval. Source export has
251 files; all 251 referenced consumer candidates passed, including the new
70-case public constructor fixture. Separate native controls cover copied
inputs/snapshots, zero-value refusal, two managed archive profiles, and actual
peer goal-create/resume refusal despite workflow capability membership.

Full Go tests, vet and race with atomic coverage passed. Python: 2252 passed,
28 skipped, 96 warnings and 24 subtests in 90.81s. Invariants, 19 scan guards,
the goal-authority and cron-authority mutation guards, and diff checks passed.
All four type audits have zero findings: 465 production files / 1473 named
types, 1206 public roots, 278 private roots and 172 package variables. Body
inventory retains 51 packages / 22282 declarations / 130 open observations.

Fresh Darwin statement coverage: application 89.40%
(33564/37545), all Go 89.15% (33930/38060), Provider 86.67%
(1522/1756). These are current observed statement totals; this context
change does not claim causation for Provider coverage movement. Python, native C,
subprocess interiors, branches and other platforms are outside this metric.

No resource stop occurred. Nice 10, Go concurrency 2 / build parallelism 1,
serial pytest and a 512MiB Go soft target were used with a 3GiB sampled owned-tree
budget and host-pressure monitoring. Peak owned RSS was 578960 KiB (565.4 MiB).
Sampled CPU briefly reached 299.3%; the sustained 250% / 30-second stop threshold
was not reached. These are sampled controls, not hard process/OS memory caps.
Historical temporal/platform failures and original P0/P1/G0–G7, broader public,
default parser, host/performance/release acceptance remain open.

[Validation receipt](docs/audits/go-public-run-context-2026-10-13.json),
[coverage](docs/audits/go-coverage-public-run-context-2026-10-13.json),
[iteration image](docs/audits/go-public-run-context-infographic-2026-10-13.png)
and [exact prompt](docs/audits/go-public-run-context-infographic-2026-10-13.prompt.txt).
Archify passed 9/9 with no errors/warnings; no new rendered-browser acceptance.

The captured text/capability profiles do not cover arbitrary direct Python
dataclass overrides, non-text Python objects or every host/platform/exhaustion
case. Prior temporal/platform failures and full migration acceptance stay open.

README Mermaid and JSON annotate the existing Go Session constructor boundary.
The 37 components, 55 connections and five boundaries are preserved; there is
no new permission, trust boundary, dependency or default. The base image stays
unchanged; the iteration image highlights provenance and peer-approval clearing.
Generated HTML receives Archify validation separately; rendered-browser
acceptance is not implied.

## 2026-10-13 mixed batch output-step admission

Branch `codex/go-batch-step-boundary`, review baseline `fbe8ad4`. Original
P0/P1/G0–G7 scope remains. Thirty-two actual Source `Agent.run` profiles cover
four three-call exclusive/parallel layouts, valid outputs and each of three
invalid-output positions, with semaphore capacities one and two. The producer
and literal `python-batch-step-boundary.json` fixture retain exact event/effect,
request/history and step-hash observations. The first native comparison failed
22 profiles: exclusive calls ran beyond a hash fault and valid step prefixes
survived a later failure. Its log remains in the iteration output directory.

Native session dispatch now computes the named output `StepHash` after the
call's `tool_result` event and before returning to the scheduler. An ordinary
UTF-8 hash failure stops later exclusive calls and later groups. Already
scheduled calls in the same parallel group finish before the error returns.
The scheduler prepares steps locally and admits the complete model-ordered
batch only on success; a failed batch contributes no step prefix or tool-result
history message, and sends no second model request. Earlier events/effects
remain observable. Existing cancellation/other-error repair, pool release and
bounded ledger retention remain in place. There is no new service, dependency,
authority, tool registration or default, and no vague domain payload type.

A deterministic native observer-cancel control exposed an exclusive-call
cancellation precedence regression during review. The retained failing test
precedes the fix; exclusive and parallel cancellation controls now pass.

Final cohort: 1711 frozen inputs unchanged across 19 stages; 18 passed and
consumer inventory exited 1 solely for the unreferenced default Python parser.
Fresh Source export has 250 files; 251 consumer fixtures include the separate
Linux capture, with 250 candidate callers observed passing. All four type audits
report zero findings: 465 production files / 1472 package types, 1205 public
roots, 278 private roots and 172 package variables. Body inventory retains
51 packages / 22277 declarations / 130 open observations.
Full Go tests, vet and race with atomic coverage passed. Python: 2251 passed,
28 skipped, 96 warnings and 24 subtests in 164.24s. Invariants, 19 scan guards,
five batch guards, one barrier guard and diff checks passed.

Darwin statement coverage: application 89.39% (33557/37539), all Go 89.14%
(33923/38054), Provider 86.45% (1518/1756). Provider coverage is unchanged
from the preceding completed cohort. These are statement observations, not
Python/native C/subprocess/branch/cross-platform or whole-migration coverage.

Resource evidence: the first Source export was stopped at host memory pressure
warning; it is preserved as resource-blocked. After 60 seconds of normal host
pressure, a separate cohort completed with the same limits and no resource stop.
All heavy stages ran serially with nice 10, GOMAXPROCS=2, build parallelism 1,
test parallelism 2, and the 512MiB Go soft memory target. Maximum sampled owned
RSS was 609488 KiB (595.2 MiB); sampled CPU reached 298.3% briefly without the
sustained 250% / 30-second stop condition. The guard is sampled protection, not
an OS hard cap. The original failing native profiles and cancellation regression
remain recorded; no assertion or timeout was relaxed.

[Iteration receipt](docs/audits/go-batch-step-boundary-2026-10-13.json),
[coverage receipt](docs/audits/go-coverage-batch-step-boundary-2026-10-13.json),
[resource controls](docs/audits/validation-resource-controls-2026-10-13.json),
[iteration image](docs/audits/go-batch-step-boundary-infographic-2026-10-13.png)
and [exact image prompt](docs/audits/go-batch-step-boundary-infographic-2026-10-13.prompt.txt).

The 32 immediate-handler profiles do not establish every delayed-peer,
cancellation, host/platform or broader public/default/performance/release case.
Historical timing and platform failures remain open.

README Mermaid and JSON annotate the existing Go Session boundary with event →
hash → whole-batch admission. Connections and component inventory are unchanged;
the base image stays unchanged and the iteration image highlights this session
behavior. The generated HTML receives separate Archify validation; this does
not imply new rendered-browser acceptance.

## 2026-10-13 public spill UTF-8 and directory ordering

Branch `codex/go-public-helper-text`, review baseline115f147. Original P0/P1/G0–G7
scope remains. Independent Source/native probes demonstrate invalid original
surrogate text failing before budget, directory, clock or mask in Source; the old
native returned success at large budget and could write masked text at zero.
25 actual public Source profiles pin original UTF-8 preflight, whole latest-batch
failure, ignored older batches, directory-before-clock/mask order, partial files,
unchanged messages and code-point-based result-ID sanitizing/cap. Initial native
comparison failed22 profiles; the exact first log/fixture is retained. Two capture
issues (temporary error path and adjacent literal-surrogate JSON normalization)
were repaired separately, retaining every case and exact text assertion.

Native ToolResultBudget now rejects invalid original UTF-8 with existing typed
protocol.ErrResultUTF8 before all effects, even when the byte budget would fit.
A guarded workspace.Files.EnsureDirectory matches Source directory creation before
clock/mask, including callback/output-encoding failures. Path checks happen before
and after creation; cancellation/escape remain refusals. This primitive is neither
OS confinement nor atomic cross-process symlink fencing. Safe result IDs use the
existing immutable legacy-text code-point helpers, preserving one underscore per
surrogate and the100-character cap. No arbitrary state/JSON, dependency, service,
new model tool, authority or default. Existing61 public-helper profiles remain.

Focused Source and Go text/directory/old-helper guards pass. Freeze the final
program/README/EXTENDING/graph cohort for fresh Source/consumer, type/body audits,
full Go/vet/race+atomic/Python, invariants/scans/selected guards and diff. Actual
exits and coverage belong in the final delivery receipt; historical temporal and
other platform acceptance are not waived. README/JSON/HTML share the updated spill
boundary; no component addition/deletion, base unchanged. Imagegen highlights
Context preflight and the existing Files directory primitive.


Actual Source AST Agent follow-up: the first full ordinary Go cohort failed five
old AST success expectations and was stopped during race; its logs/frozen inputs
remain. Eight real Source Agent runs now prove five surrogate results emit their
tool_result event, then fail output step_hash UTF-8 before returning the batch:
one model call, two transcript messages, no result message and no budget directory.
Three scalar/nonfinite-text controls complete with two calls/four messages. The
old eight direct Tool/SDK fields are byte-equivalent after omitting the new agent
field. Native OutputStepHash now checks raw text before JSON replacement and leaves
this ordinary UTF-8-failed batch unanswered; cancellation and other repair remain.
Exact event text, hash error, model counts, messages, directory and ledger assertions
pass. This is a substantive new Source/runtime repair cohort, not an unchanged retry.

Final verified cohort:1706 inputs unchanged across20 stages;19 pass, consumer
inventory exits1 only for the unreferenced default parser. Fresh Source249 files;
consumer250 including a separate Linux fixture has249 observed passing candidates.
All33 new Source budget/AST Agent profiles and previous61 helpers pass; old eight
AST Tool/SDK fields remain unchanged. Go full/vet/race+atomic; Python2250 passed,
28 skipped,96 warnings,24 subtests in100.26s. Four type audits remain465 production/
1472 named,1205 public/278 private roots/172 variables withzero findings. Body51
packages/22278 declarations/130 open observations. Invariants,19 scans,18 memory,
one budget-spill andone spill-masking guard and diff pass. Darwin statements:
app89.38% (33553/37538), all89.14%
(33919/38053), Provider86.45%
(1518/1756). No C/subprocess/Python/branch/all-platform coverage claim.
First ordinary cohort failed five AST roots and was stopped during race. New
actual Source loops and substantive raw-hash/transcript repair precede this
separate verified cohort; failed logs/frozen inputs remain. No threshold relaxed
or unchanged retry-to-green. Base unchanged; built-in imagegen highlights Context
preflight/hash and Files directory primitive; Archify9/9, zero errors/warnings.
No new rendered-browser acceptance. Provider observed coverage is86.45% versus86.67% previously: unchanged retry-wait/cancellation spans lose five covered statements and gain one. The exact span delta is retained; no threshold change or percentage-raising retry. Other historical temporal failures and original
full public/default/provider/filesystem/host/platform/performance/release remain open.

[Text contract receipt](docs/audits/go-public-helper-text-2026-10-13.json);
[iteration image](docs/audits/go-public-helper-text-infographic-2026-10-13.png);
[exact prompt](docs/audits/go-public-helper-text-infographic-2026-10-13.prompt.txt).


## 2026-10-13 typed public helper composition

Branch `codex/go-public-helpers`, review baseline3269424. Preserve original P0/P1/
G0–G7 scope. The six previous namespace lookup omissions have explicit counterparts:
four typed helper functions, exported UnknownResult and the already-public
TrajectorySchema. Source41 memory/20 budget profiles and exact constants feed
external Go public-call tests. They cover defaults, zero/negative limits/previews/
thresholds, side queries/events, initial/fallback faults, owner isolation, origins,
masked partial writes and unchanged caller transcripts. Native cancellation,
persistence-authority and workspace-boundary controls remain separate evidence.

The runtime Context/capture/default-compactor delegates to the same cores; no new
service, dependency, default, role, owner or authority. Public store/workspace and
callbacks are explicit caller composition. Ordinary callback containment and
initial-list/fallback errors retain Source order. Budget replacement now waits
until all writes finish; successful earlier files remain on a later failure.

Frozen verification: 1701 inputs unchanged across20 stages;
19 pass and the consumer inventory exits1 only for the unreferenced default
parser fixture. Fresh Source248 files; consumer inventory249 including the
separate Linux fixture has248 observed passing candidate tests. Go full/vet/
race+atomic pass; Source2248 passed,28 skipped,96 warnings and24 subtests in90.14s.
All19 scans,18 memory guards,one budget-spill andone spill-masking guard pass.
Types465 production files/1472 named,1205 public/278 private roots/172 variables,
zero findings. Body51 packages/22268 declarations/130 open observations; the new
recover observation is transient and discarded, not domain state. Darwin app
statements89.40% (33534/37511), all Go89.15%
(33900/38026), Provider86.67%
(1522/1756). No branch/C/subprocess/Python/all-platform coverage claim.
Built-in imagegen iteration saved with exact prompt; base unchanged. Archify9/9,
zero errors/warnings; no new rendered-browser review. Historical temporal failures
remain unexplained, with no thresholds relaxed; this is a distinct frozen cohort.

[Public matrix](GO_PUBLIC_SURFACE_INVENTORY.md);
[namespace overlay](docs/audits/go-public-helpers-namespace-2026-10-13.json);
[delivery receipt](docs/audits/go-public-helpers-2026-10-13.json);
[iteration image](docs/audits/go-public-helpers-infographic-2026-10-13.png);
[exact prompt](docs/audits/go-public-helpers-infographic-2026-10-13.prompt.txt).


## 2026-10-13 public namespace and default scope inventory

Audit `4684918`, review baseline `e4c5aff`, branch `codex/go-public-scope-audit`. Original P0/P1/G0–G7
scope preserved. Current AST/source inventory enumerates157 root Python exports:
52 name candidates,99 explicit rename/composition candidates and6 names without
an identified public Go namespace binding. Four are standalone helpers
(tool_result_budget and three memory helpers); two are public constants. Runtime
adoption/internal values may exist, so this is not a missing-runtime verdict.
Exact public caller signatures, owner/effect/callback semantics and constant values
remain required evidence; declaration matching proves no semantic equivalence.

All12 README default-posture rows and18 curriculum labels covering s01–s20 now
link actual Source test paths.49 Settings fields all occur in the64-case fixture;
57 feature definitions/14 registry profiles/8 manager profiles,44 HTTP operations
across40 paths and3 actual SDK request producers are indexed. Current selected
Go gates pass31 public/config/feature/authority roots plus5 provider-wire roots;
Source22 feature/wire tests pass. Initial Source wire capture hit sandbox loopback
bind refusal; same assertions pass with local listening permitted, with the initial
error retained. No Source/Go program or fixture change, new dependency or threshold.

Provider scope summary no longer presents the repaired dotted-I pair as pending
or claims this already-integrated work is unmerged. Historical snapshots retain
original receipts. Full-feature CLI parser decision, broader IDNA/custom transport,
129 body observations, historical temporal failures and final public/default/
platform/performance/release scope remain open. No new full-suite/type/coverage
or rendered-browser acceptance claim. Previous e4c5aff full gates/coverage retained.

[Public matrix](GO_PUBLIC_SURFACE_INVENTORY.md);
[machine inventory](docs/audits/go-public-default-inventory-2026-10-13.json);
[iteration image](docs/audits/go-public-scope-infographic-2026-10-13.png);
[exact prompt](docs/audits/go-public-scope-infographic-2026-10-13.prompt.txt).
Canonical graph/JSON/base unchanged; regenerated HTML matches its previous bytes,
Archify9/9 structural checks pass. Built-in imagegen highlights only L1 entrypoints
and L5 operator contractaudit; all five layers/connections/cards preserved.

## 2026-10-13 native MCP ordered scalar keys and staged faults

Reviewed **2026-10-13**: implementation `833e16f`, native MCP ordered scalar-key mapping
and Source fault-stage alignment.116 direct and116 actual journal profiles pass
required assertions, including original88 differences and added nonfinite roots/
values. Immutable named entries preserve first-key kind, Python numeric equality
and last-value replacement. Unhashable keys fail batch preparation; non-text
keyword binding fails after permission, sorted query faults precede result
observers even without an active reducer. Explicit/callable MCP objects stay
finite; external risk unchanged. Direct/SSE, clone/mask/archive and seven real
SQLite close/reopen/owner-restore profiles preserve original roots, event stages,
current authority and fresh leases without external-effect replay.
Frozen1695 inputs pass Source247/manifest, focused Go/Python, four type gates,
collector/body inventory, ordinary Go/vet/race+atomic, full Python,19 scans,
6 guards and diff. Consumer inspection exits1 only for the absent syntax-parser
consumer. [Current receipt](docs/audits/go-mcp-mapping-native-2026-10-13.json).
Previous full Python memory-scaling failure remains retained without a runtime/
threshold fix or cause claim; this new implementation cohort is separate.
Broader mapping/NaN-identity/caller/transport and original P0/P1/G0-G7 stay open.


Full Python: 2247 passed, 28 skipped, 96 warnings, 24 subtests passed in 93.36s (0:01:33)
Types:463 files/1465 named types,1198 public types,278 private roots,172 variables;
four established audits report zero findings. Body inventory:51 packages/
22207 declarations/129 observations; wider data-flow/caller/escape/generic/
foreign/other-profile proof remains open. Consumer inventory:248 fixtures,
including one separate Linux capture; both mapping recipes have required native
consumer passes, the parser remains unreferenced.

The initial mapping implementation retained24 mixed-key differences because
query construction was skipped with a nil reducer. Source constructs that query
before result observers regardless of active reduction; native now does too.
Stronger SQLite status/event assertions exposed NaN-key ToolUse event loss during
generic masking. The recording path masks keys in their original kind, archives
MCP legacy observations and masks surrounding metadata without reclassifying keys.
Both faults and isolated constructor/import/epoch/build setup errors remain logged.
Model NaN negative guards were contradicted by actual Source captures; explicit
caller NaN/null refusal and malformed model syntax checks remain required.

The canonical Mermaid/control explanation and JSON→generated HTML are synchronized;
Archify9 structural/geometry checks pass. No rendered-browser acceptance claimed.
No components added/deleted, so the base remains unchanged. Built-in imagegen
highlights Session/Gate/MCP/SQLite and records current changes/limits beside it.

Passing-race statement coverage: app **89.38% (33496/37478)**,
all Go **89.13% (33862/37993)**, Provider **86.67% (1522/1756)**.
[Current coverage](docs/audits/go-coverage-mcp-mapping-native-2026-10-13.json).
Darwin statements only; native C/subprocess interiors, Python, branches and other
profiles excluded. Original migration acceptance and historical temporal
reliability remain open.


[Iteration image](docs/audits/go-mcp-mapping-native-infographic-2026-10-13.png);
[exact prompt](docs/audits/go-mcp-mapping-native-infographic-2026-10-13.prompt.txt).
No Python runtime-package, new dependency, paid-provider or remote fetch/push work.
Pending default CPython/IDNA choices,129 body observations, wider public/default/
feature/platform/performance/release and historical temporal failures remain open.

## 2026-10-13 MCP mapping-key Source prerequisite

Reviewed **2026-10-13**: Source prerequisite `d477f53`; Go programs remain unchanged.
Actual Agent.run/register_mcp captures25 key profiles × auto/readonly × single/batch:
100 required Source observations. Existing52-root fixture bytes are unchanged.
Current native required comparison fails88/100;12 text-key profiles pass. Hashable
non-text keys reach binding after auto permission, unhashable keys fail whole-batch
preparation, and mixed-key ordering faults can happen near result recording after
earlier effects. Readonly blocks invocation but does not prevent every later
comparison fault. These are open native defects, not accepted adaptations.
Frozen1689 inputs pass Source246/manifest,
11 focused Python tests, ordinary Go/vet,19 scans and diff. Full Python fails the
unchanged memory-scaling cost guard: 0.25ms/1.55ms at100/800 records exceeds4×.
2245 tests pass,28 skip; no threshold or runtime fix. Main integration is held.
Two later wall/CPU/GC histories do not reproduce that failure and prove no cause.
Consumer inspection exits1 with two absent consumers: native syntax parser and new mapping
keys. The read-only overlay comparison is retained separately from normal gates.
[Current Source receipt](docs/audits/go-mcp-mapping-keys-source-2026-10-13.json).
Latest race/coverage and type/body receipts remain at the previous unchanged Go
programs; no fresh race, type or body closure is claimed by this Source slice.


Full Python: 1 failed, 2245 passed, 28 skipped, 96 warnings, 24 subtests passed in 134.83s (0:02:14)
No Go/runtime Python behavior, authority, ownership, installed tool or dependency
changes. Source capture is shared with the existing52 cases without altering
their bytes. The new fixture envelope is finite JSON even when input_json text
contains NaN/infinity. The initial wrong-cwd overlay setup is retained separately
from the actual comparison's88 failures. Canonical map/JSON/generated HTML and
base are unchanged; built-in imagegen highlights L5 operator contractaudit and
describes the Source evidence beside the map. No new browser-rendering acceptance.

Next required native work:

1. Add a named ordered closed key/value mapping with Python hashability/equality
   and duplicate handling; do not introduce any/RawMessage/open domain maps.
2. Keep unhashable and later malformed rows at whole-batch preparation, defer
   hashable non-string binding failure until permission/execute, and preserve
   mixed-key comparison fault stage and earlier effects.
3. Verify history/masks/identity/events, managed/journal/recovery and wider
   transport profiles, then make all100 profiles required native assertions.

[Iteration image](docs/audits/go-mcp-mapping-keys-infographic-2026-10-13.png);
[exact prompt](docs/audits/go-mcp-mapping-keys-infographic-2026-10-13.prompt.txt).
Original P0/P1/G0-G7, default CPython/IDNA decisions, historical temporal failures
and wider public/default/type/feature/platform/performance/release acceptance stay
open. Source prerequisite stays on its branch pending the failed Python gate;
no remote fetch/push or paid provider call.

## 2026-10-13 finite MCP model roots and whole-batch preparation

Implementation `48f7b85`, based on main `6cb887e`, branch
`codex/go-mcp-model-roots`. Actual Python Agent.run + register_mcp captures23
finite roots in auto/readonly, plus three two-call batches per mode:52 required
profiles. Falsey roots become objects; string-key pair/text rows use Source
dict preparation; invalid truthy roots fail before any earlier batch effect.
The original assistant input remains unchanged. Native model ingress keeps a
private tag with existing closed immutable values; explicit arguments and the
callable MCP accessor remain object-only. External risk stays unchanged even
with readOnlyHint; no new grant, connection ownership or default installed tool.

Direct/SSE, canonical identity, detached clones, masking and archive codecs
retain roots. Four real SQLite close/reopen/owner-restore profiles retain history,
foreign-owner refusal, current interactive mode and fresh leases; no completed
external effect replay.48 original native failures and the initial constructor
compile typo are retained separately. The first full ordinary cohort exposed an
obsolete model MCP-null negative; actual Source evidence justified replacing it
with NaN rejection plus explicit null refusal. The partial race was stopped with
verified owned process identities before editing; it supplies no accepted coverage.

Frozen1686 inputs pass all16 required validation stages; the separate consumer
inventory exits1 only for the unreferenced native syntax-parser fixture. Source
exporter compares245 files, consumer inventory includes246 with the separate
Linux capture. Four type gates:462 files/1460 named types,1196 public types,
275 private roots,172 variables;zero findings. Body inventory:51 packages/
22104 declarations/129 observations; caller/escape/interprocedural/generic/
foreign/other-profile/runtime flow remains unproven. Full Python: 2244 passed, 28 skipped, 96 warnings, 24 subtests passed in 91.66s (0:01:31)
Darwin race statements:app89.40% (33313/37264), all Go89.15% (33679/37779), Provider86.67% (1522/1756).

README canonical map/boundary and JSON→generated HTML updated;9 structural/
geometry checks pass, no rendered browser acceptance. No components added or
removed, so the base image stays unchanged. Built-in imagegen edits that base
to highlight Session/Provider/MCP and describe this iteration beside it.

[Full receipt](docs/audits/go-mcp-model-roots-2026-10-13.json);
[coverage](docs/audits/go-coverage-mcp-model-roots-2026-10-13.json);
[iteration image](docs/audits/go-mcp-model-roots-infographic-2026-10-13.png);
[exact prompt](docs/audits/go-mcp-model-roots-infographic-2026-10-13.prompt.txt).
Nonfinite/non-string-key/wider MCP roots and callbacks, pending default syntax/
IDNA decisions, original public/default/platform/type/performance/release scope
and historical temporal failures remain open. No new dependency, paid provider,
remote fetch/push, Linux full gate or Python runtime-package change.

## 2026-10-13 fixed tool execution scope and custom child inheritance

Implementation `be59a60`, based on local main `85a4f80`, branch
`codex/go-extension-subagents`. Actual Source callbacks expose current Agent
label/depth. Go now supplies fixed `ToolAuthority.Scope: ToolExecutionScope`,
stamped by Session dispatch. It is informational, never selects principal/owner/
workspace/capability/permission or depth quota, and carries no mutable Agent or
business-state pointer. Direct gate callers may leave zero scope.

Eighteen actual Source Explore/worker/general-purpose profiles exercise read,
write, mixed, unclassified, foreign capability and read-cap/write-risk selections.
Required assertions compare complete selected schemas, fresh request history,
callback label/depth/workspace/mode, cleared-human peer provenance and outcomes.
Typed codecs/handlers remain selected; filtered names remain unknown, Explore
refuses writes and parent history stays private. A previous-dispatch Go overlay
fails all18 assertions at empty/zero scope; retained log is a controlled regression
comparison, not a complete old-tree cohort. Initial fixture reader rejected a
pattern-bearing schema before any case; corrected reader uses the complete
metadata constructor without changing Source expectations or runtime guards.

Frozen1680 inputs pass Source244/manifest, focused Go/Python, four type audits,
collector tests/body inventory, ordinary Go/vet/race+atomic, full Python,19 scans,
6 selected guards and diff. Python2244 passed,28 skipped,96 warnings,24 subtests
in88.35s. Consumer inspection exits1 only for the unreferenced native parser.
Audits:462 production files/1460 named types/1196 public/275 private roots/172
variables,zero findings. Body51 packages/22096 declarations/129 observations.
Darwin statements:app89.36% (33285/37250), all Go89.11% (33651/37765),
Provider86.67% (1522/1756). [Full receipt](docs/audits/go-extension-subagents-2026-10-13.json);
[coverage](docs/audits/go-coverage-extension-subagents-2026-10-13.json).

README canonical map/boundary/JSON/generated HTML agree;9 showcase checks,zero
errors/warnings. No component addition/deletion; base unchanged. The built-in
[iteration image](docs/audits/go-extension-subagents-infographic-2026-10-13.png)
highlights existing Session/Gate/task delegation. Required arrows/cards inspected;
exact prompt retained. Default syntax/IDNA approvals, arbitrary mutable Agent/state
API, wider callbacks/parallel/remote/lifecycle/platform/type/temporal/public scope
and original P0/P1/G0-G7 acceptance remain open. Local ff-only main integration
after two logical commits; no remote push or complete-port claim.

## 2026-10-13 complete custom schema metadata

Implementation `c92fe51`, based on local main `82cc93d`, branch
`codex/go-extension-schema-preservation`. Actual Source manager requests preserve
refs, mixed enums, structured const, fractional bounds, patterns/format/examples,
conditionals, boolean properties, arrays without items, empty schemas and finite
non-object metadata. Native decoding previously refused the captured ref schema
with `unknown field "$ref"`; original input/program/output retained. One initial
Unicode comparison setup mistake is retained separately; no production wire or
Source expectation changed to address that diagnostic.

`protocol.ExtensionToolSchema` lowers caller bytes immediately into immutable
closed finite values and sorts keys for catalogue identity. Existing projection
bound16 MiB; malformed/nonfinite/non-scalar Unicode refusal. Known built-in/MCP
schemas keep native parsing; the new archived schema fallback requires a name
without a native decoder. This is metadata, not argument validation, ref loading,
callback registration or authority. Explicit codec/Validate/gate remain unchanged.
Non-object Source emission does not prove real model-server acceptance.

Sixteen Source profiles/32 managed owner sessions match snapshot JSON/fingerprint,
actual request tools/cache selection, private catalogue publication and results.
Sixteen native SQLite close/reopen catalogue events retain schemas/fingerprints;
explicit restore rebinds operator tools, rejects foreign owner, uses current
interactive permission and confirms a new lease before fresh typed execution.
Masked recording projection does not alter the live schema; no dynamic domain
holder, dependency, feature default or process/service added.

Frozen1677 inputs pass Source243/manifest, focused Go/Python, four type audits,
collector tests/body inventory, ordinary Go/vet/race+atomic, full Python,19 scans,
6 selected guards and diff. Python2244 passed,28 skipped,96 warnings,24 subtests
in103.77s. Consumer inspection exits1 only for the unreferenced native parser.
Audits:462 production files/1459 named types/1195 public/275 private roots/172
variables,zero findings. Body:51 packages/22096 declarations/129 observations.
Race statements:app89.34% (33280/37250), all Go89.09% (33646/37765),
Provider86.67% (1522/1756). [Full receipt](docs/audits/go-extension-schema-preservation-2026-10-13.json);
[coverage](docs/audits/go-coverage-extension-schema-preservation-2026-10-13.json).

Canonical README/boundary/JSON/generated HTML agree;9 showcase checks,zero
errors/warnings. Base unchanged: no component addition/deletion. The built-in
[iteration image](docs/audits/go-extension-schema-preservation-infographic-2026-10-13.png)
highlights only L2 catalogue/L3 context/L4 custom capability; exact prompt retained.
All five layers/cards and required routes visually inspected. Default syntax/IDNA
approvals, non-object server acceptance, broader schema/callback/host profiles,
body/runtime/public/default/provider/platform/performance and original P0/P1/G0-G7
acceptance remain open. Local ff-only integration after two logical commits;
no remote push or full migration completion claim.

## 2026-10-13 typed registered extension inputs

Implementation `38b76a6`, based on local main `59767dc`, branch
`codex/go-extension-input-codecs`. Python ToolRegistry accepts named custom
handlers; Go now offers `NewTypedExtensionTool[T]` with concrete validated input,
explicit codec and typed handler. Before/guard/permission and journal admission
precede decoding. Original immutable observations stay in transcripts and action
identity. Schema-only custom definitions retain missing-decoder refusal; built-in
and MCP decoder names cannot use this constructor. No default or dependency added.

Fourteen actual Source registered-tool cases match output, fault flags and handler
effects. Thirteen also match exact traces. Readonly-write Source PermissionHook
refuses during before; native guards precede permission refusal. This existing
scheduling adaptation remains explicit. Native checks cover cancellation, decoder/
validation/handler panic containment with type-only diagnostics, nil/reserved-name
and binding faults, replay/current permission, and model-driven result pairing.
Operator callbacks must honor context and declared parallel policy; broader custom
coercion and lifecycle profiles remain unproven.

Frozen1671 inputs pass Source242/manifest, focused Go/Python, four type audits,
collector tests/body inventory, full ordinary Go/vet/race+atomic, full Python,
19 scans,6 selected guard checks and diff. Python2244 passed,28 skipped,96 warnings,
24 subtests in100.14s. Consumer inspection exits1 only for the unreferenced parser
fixture. Body inventory:51 packages/22078 declarations/129 open observations;
new recover local is transient type-only diagnostic, not retained domain data.
Established audits:461 production files/1459 named types/1195 public/275 private
roots/172 variables,zero findings. Race statements app89.32% (33244/37217), all
Go89.08% (33610/37732), Provider86.67% (1522/1756).
[Full receipt](docs/audits/go-extension-input-codecs-2026-10-13.json);
[coverage](docs/audits/go-coverage-extension-input-codecs-2026-10-13.json).
An intentional turn interruption left no race exit and its output is retained
without acceptance. The terminal replacement covers identical frozen inputs;
Source/ordinary/vet and other completed stages were not rerun. No retry-to-green.

The base image now includes the L4 custom-tool card and L3 execution-stage codec
note. The [iteration image](docs/audits/go-extension-input-codecs-infographic-2026-10-13.png)
highlights only those two cards. Exact prompts retained; all five layers/cards and
required routes inspected. No new process/service. Canonical Mermaid, boundary,
JSON and generated HTML agree;9 showcase checks,zero errors/warnings. Rendered
browser acceptance remains separate. Default syntax backend approval, wider IDNA,
body/runtime/other-platform/public-default inventory, historical temporal failures
and original P0/P1/G0-G7 acceptance remain open. Two logical commits are intended
for local ff-only main integration after gates; no remote push.

## 2026-10-12 memory-selection caller profiles

Implementation `6e1da29`, based on main505e867, branch codex/go-memory-selection-profiles. Actual Source
entry points compare300 array/object cases: direct preparation, raw Agent.run,
ManagedSession SDK, authenticated synchronous POST and SSE. Independent Source
script capture pins its ordinary entry stack; an initial -m diagnostic showed
runpy frames shift direct captures, so final producer uses a direct script. No
Python runtime or recursion limit was changed.

The old native comparison reports72 SDK and72 HTTP differences (144 total), all
retained. One HTTP test field-name compile error is separately retained as setup,
not semantic parity evidence. Source total-container limits are982/980/978/972/976.
The native closed value decoder now receives whole-reply syntax preflight from a
private named caller selector. RunContext snapshots omit it; same-caller continuations
retain it and peer derivation drops it. Managed SDK selection preserves a trusted
HTTP stamp. Principal, capability, model budget, events, bound-owner records,
readonly execution and lexical fallback remain unchanged. All300 comparisons pass
after repair; no earlier assertion or threshold was relaxed.

Source producer failure is required to propagate instead of publishing an empty
fixture; nine focused exporter tests pass. Frozen1667 inputs pass Source241/manifest,300 required profile cases, focused Go/Python, four type audits, collector tests/body inventory, full ordinary Go/vet/race+atomic, full Python,19 scans,18 memory guards and diff. Python2244 passed,28 skipped,96 warnings,24 subtests in92.74s. Consumer inspection exits1 only for the unreferenced native parser fixture. Race statements: app89.30% (33206/37186), all Go89.05% (33572/37701), Provider86.67% (1522/1756).
[Full receipt](docs/audits/go-memory-selection-profiles-2026-10-12.json); [coverage](docs/audits/go-coverage-memory-selection-profiles-2026-10-12.json). Current types:460 files/1455 named,
1192 public/274 private roots/172 variables,zero findings. Body inventory51 packages/
22046 declarations/128 open observations; interprocedural/generic/foreign scope open.
The [iteration image](docs/audits/go-memory-selection-profiles-infographic-2026-10-12.png)
edits the base with L1 entry/L2 managed/L3 context highlights. One routing-visibility
repair restores clear Session/Provider arrowheads; all five layers/cards and required
routes retained. No component addition/deletion; base unchanged. JSON/HTML synchronized;
HTML9 checks,zero errors/warnings; prior browser visual limitation remains.

The default parser decision, wider caller/host stacks and original P0/P1/G0-G7
acceptance remain open. No remote push or completion claim.

## 2026-10-12 closed memory-selection values

Implementation `8e228a3`, based on branch `5ad859c`; main before integration is `89327ab`. Selection previously used
RawMessage array entries. Sixteen new actual Source recipes extend the twelve
existing context cases. The pre-change native comparison fails five cases:
nonfinite mixed/object replies, positive/negative over-limit integers and excessive
nesting. All five failures are retained; no original test or threshold was relaxed.

`memoryIndices` now decodes the whole reply into the existing immutable closed
Value before selecting integer indices. All floats, including NaN/Infinity and
numeric overflow, are ignored. Booleans remain indices; order, duplicates and the
first-five limit stay unchanged. Full-array errors after a valid first index cause
lexical fallback. The 4300/4301 integer boundary and surrogate text/key observations
match Source; 500/1005 nesting controls match the existing 1000-container decoder
profile. Exact caller-stack recursion edges/custom host profiles remain open.
Prepared prompts, side-call budgets/purpose, owner/foreign isolation, memory events,
runtime facts and live-meter isolation compare in all28 required cases. Existing
rewrite-order and cancellation tests pass. Two local RawMessage observations are
removed; current pre-change full-body inventory is130 rather than the historical139,
without implying those remaining observations are defects or exemptions.

Runtime Python is unchanged; only its Source recipe producer and one fixture are
expanded. No dependency, default, owner binding or authority changed. README and
canonical architecture explanation/specification updated; HTML9 checks, zero
errors/warnings. Frozen1662 inputs pass Source240/manifest, focused Go/Python, four type audits, compiler-collector tests and full-body inventory, complete ordinary Go/vet/race+atomic, full Python,19 scans,18 memory guards and diff. Python2243 passed,28 skipped,96 warnings,24 subtests in125.01s. The separate consumer inventory exits1 only for the unreferenced native syntax-parser fixture. Current race statements: app89.30% (33195/37171), all Go89.05% (33561/37686), Provider86.67% (1522/1756).
[Full receipt](docs/audits/go-memory-selection-values-2026-10-12.json); [coverage](docs/audits/go-coverage-memory-selection-values-2026-10-12.json). The completed built-in
[iteration image](docs/audits/go-memory-selection-values-infographic-2026-10-12.png)
retains all five layers and required arrows; only L3 is highlighted. Base unchanged.
Original temporary gate logs/handles and one pending image handle were lost after
interruption. The replacement cohort persists frozen inputs, stage process IDs and
logs under ignored var/; the lost run supplies no acceptance. Current full-body
inventory is51 packages/22036 declarations/128 open observations. Prior ordinary
Go/Python timing failures remain retained without a runtime fix/cause claim; one
passing cohort does not close temporal reliability. Local ff-only main integration
can proceed after commits; remote push and original-scope completion are not claimed.

## 2026-10-12 timing boundary diagnostics

After `f9d4f61`, runtime and repository tests remain unchanged. Four matched
Source/Go scenarios show that the numeric capture wait begins after process
creation and PID-observer preparation. The 1.1s command times out with no injected
registration delay, but both complete after a 250ms delay; the short command also
completes. These controlled delays do not establish the cause of the earlier
ordinary Go failure, which lacks those timestamps. The native Started observer
still requires a prompt return. No original assertion, timeout or threshold changed.

One cProfile capture of the Python 40-turn workload (80 model calls) shows request
preparation, event/storage conversion and JSON projections among its hot paths.
The profiled 0.523s includes measurement overhead and is not a passing or failing
acceptance rerun. The original 0.526s/0.5s Python failure remains unresolved.
[Exact diagnostics and sources](docs/audits/go-timing-boundaries-2026-10-12.json);
[base-derived observation image](docs/audits/go-timing-boundaries-infographic-2026-10-12.png).
Five layers and required arrows inspected; no component addition/deletion, so the
base is unchanged. Runtime/test/Source hashes match the prior frozen cohort;
only its already-recorded README documentation differs. Full gates not rerun for
this documentation-only diagnostic. Main remains `89327ab`; no merge or push.

Next: obtain the missing native timing stages and measure Python stage cost
without profiler overhead before changing implementation. Original acceptance
and the diagnostics-parser decision remain open.

## 2026-10-12 legacy string text span reuse

Implementation `00059f1`, based on branch `634fe23`; main remains `89327ab`. Prior full race failure at
maximum SQLite readonly31.00s remains preserved. Its earlier owned after-stage
readonly sample includes appendLegacyString/appendLegacyStringText and textRune
beneath request projection. This is a sampled window, not total CPU attribution.

The shared legacy compact/pretty text helper now checks scalar UTF-8 and reuses
existing appendTextUTF8 spans. Surrogatepass and malformed native bytes retain the
original textRune/appendRune lane. Strict final UTF-8, member order, nonfinite
spelling, indentation and execution authority remain unchanged. Actual Source
adds14 pretty hashes and8 text/key surrogatepass recipes with16 hashes. Forty-four
compact/pretty byte assertions and native malformed-byte controls pass.

Same race+atomic benchmark uses3 iterations per case. ASCII compact508.24ms→7.39ms;
Unicode compact199.42ms→11.88ms. Escaped compact bytes/op3070752→3307536 increase,
while allocations31→19 decrease. These are bounded measurements; preserve their
tradeoff and do not infer release timing or closure of the shared-budget failure.
The original512KiB replay/readonly/backend-once test still shares its30s context.

Frozen 1661 inputs: Source240/manifest, focused Go/Python, four type audits, vet, full
race+atomic,19 scans,1 selected meter guards and diff pass. Ordinary Go fails background
timeout-true-expired (completed vs Error: Timeout (Trues)); Python: 1 failed, 2242
passed, 28 skipped, 96 warnings, 24 subtests passed in 161.92s (0:02:41),
forty-turn0.526s against0.5s threshold. Separate inventory exits1 for the missing native
parser. Race statement coverage app 89.30% (33189/37166); all Go 89.05% (33555/37681);
Provider 86.67% (1522/1756). No ordinary/Python timing or full acceptance claim; no
merge.
[Full receipt](docs/audits/go-legacy-text-spans-2026-10-12.json); [coverage](docs/audits/go-coverage-legacy-text-spans-2026-10-12.json). The complete race includes the
original maximum512KiB SQLite restart/replay/readonly/backend-once test with its
unchanged30s context. Its prior failures stay retained; this is one passing cohort.
Background and Python timing failures are not assigned a cause, retried or hidden.
No dependency/runtime Python module/context adjustment. Full invariants/mutation
sweep not rerun; exact selected guard scope reported.

The [iteration image](docs/audits/go-legacy-text-spans-infographic-2026-10-12.png) edits the existing base, highlighting only L3.
44 Source hashes, surrogatepass and final UTF-8 notes remain explicit. All five
layer names/cards and required arrows inspected after one arrow correction. Base
unchanged; exact initial/repair prompts saved. Canonical boundary/JSON synchronized;
HTML9 checks, zero errors/warnings. Prior browser visual limitation remains.

Next: diagnose Source/native background timeout completion and the offline Python
cost guard without changing thresholds or retrying for green; original acceptance
and the parser decision remain open.

## 2026-10-12 inspection JSON formatter span writes

Implementation `b066c02`, based on branch `40b88bf`; main remains `89327ab`. The prior full race failure
at maximum SQLite readonly retry is preserved. Current unchanged-before overlay
with owned native sampling fails at 35.12s. Its readonly stack window includes
TokenMeter/compaction → ObservedMessagesPythonJSON → formatPythonJSONBytes →
atomic/TSAN operations. Full pprof still has 74.17% unresolved ExternalCode; this
window cannot attribute all CPU, establish a SQL defect or explain every host run.

The formatter now finds encoded quote/backslash boundaries in bulk and reuses
existing closed text escaping after unchanged json.Unmarshal lowering. It retains
non-string numeric/legacy spelling, key order and compact/spaced separator rules.
Four actual Source text/key modes provide 56 byte hashes. Original decision value
codec benchmarks/byte-budget controls were measured as an initial candidate; its
production encoder is unchanged. The actual sampled readonly path selected the
inspection formatter for the implementation. No new dependency or default.

Same race+atomic formatter benchmarks use three iterations per case. ASCII UTF-8
changes 324.32ms→32.47ms, Unicode UTF-8 386.26ms→104.27ms. Escaped UTF-8 allocation
bytes increase 3,603,424→4,372,656 despite fewer allocations; preserve that tradeoff.
The same sampled maximum 512KiB/30s diagnostic passes after the implementation at
16.84s, with 13.16s context remaining. Backend-once, exact replay, current readonly
and final lease/persistence assertions all execute. Single runs and profiling
windows are diagnostic evidence, not release performance or broad reliability proof.

Frozen 1660 inputs record 13 passed validation gates plus a FAILED full race gate:
Source 240 files/manifest, focused Go/Python, four type audits, full ordinary Go
JSON/vet/race+atomic, Python, 19 scans, 1 selected meter mutation guards and diff.
Python: 2243 passed, 28 skipped, 96 warnings, 24 subtests passed in 135.71s (0:02:15).
Failed-run observed statements: app 89.30% (33188/37164); all Go 89.05% (33554/37679);
Provider 86.45% (1518/1756). Prior passing app89.28%/all89.16% baseline is retained.
Separate consumer inventory retains exit1 for the absent syntax-parser consumer; not
full acceptance.
[Full receipt](docs/audits/go-inspection-json-spans-2026-10-12.json); [coverage](docs/audits/go-coverage-inspection-json-spans-2026-10-12.json). The prior failed full-race cohort
and current before-diagnostic failure remain preserved. The full cohort still fails at31.00s in readonly retry. The sampled after case
passes16.84s, but does not establish full-cohort timing reliability. No retry,
30s context change or merge. The unresolved external CPU fraction is not
attributed to SQLite. Escaped-text allocation tradeoff remains explicit. No new
dependency or runtime Python-module change; full mutation sweep/invariants not rerun.

The [iteration image](docs/audits/go-inspection-json-spans-infographic-2026-10-12.png) edits the existing layered base, highlights L3
estimation and labels L5 SQLite as validation only. Five layer names and five L5
cards remain. Base unchanged. Exact built-in prompt saved and PNG inspected;
canonical map reviewed, boundary/JSON synchronized, HTML passes 9 checks with
zero errors/warnings. Browser visual acceptance remains blocked by prior URL policy.

Next: measure remaining readonly token/compaction projection costs under the full
cohort while preserving the original 30s context; do not infer completion from the
sampled16.84s pass. Original semantic/default/platform acceptance remains open.

## 2026-10-12 Source/native contract consumer inventory

Implementation `3218d44`, based on local main `89327ab`. G7 now has an explicit operator inventory across
all 241 Python-labelled Go fixture files, distinguishing the current exporter's
240 from the separately captured Linux watchfiles artifact. Literal references
and direct named-function candidate caller links are collected from active Go
metadata. Go test JSON root terminal actions can be attached without treating
root success as proof of a fixture read, active branch or complete assertions.
Enclosing if branches remain visible: the Linux fixture's candidate test root
can pass on Darwin while its Linux-only read is never selected.

The exporter optionally publishes sorted names/hashes from the same successful
capture. A failed check cannot publish the success manifest. The native consumer
rejects byte drift, duplicate/missing manifest entries and malformed/empty result
streams. Comments, inactive files, shadowed calls, skipped tests and receiver
ambiguity have explicit counterexamples. No dependency or default changes.
The parser fixture has no native consumer; its backend decision is still unapproved.
This inventory does not close original P0/P1/G0-G7 or semantic-parity acceptance.

Frozen 1658 inputs record 12 passed validation gates and one FAILED full race gate:
Source 240 files with manifest, focused Go/Python controls, four type audits, full
ordinary Go JSON/vet/race+atomic, full Python, 19 scanner anchors and diff. Python: 2243
passed, 28 skipped, 96 warnings, 24 subtests passed in 163.80s (0:02:43). Failed-run
observed statement counts: application 89.30% (33191/37168); all Go 89.05%
(33557/37683); Provider 86.67% (1522/1756). These do not replace the prior passing
coverage baseline. The separate inventory check exits 1 for the absent syntax-parser
consumer; it is incomplete, not a passing gate.
[Full receipt](docs/audits/go-contract-consumers-2026-10-12.json); [coverage](docs/audits/go-coverage-contract-consumers-2026-10-12.json). All 241 fixture artifacts remain
visible: 240 current exports and one separate Linux capture. 240 have candidate
roots with observed pass results; the parser fixture has no reference. The Linux
root pass on Darwin is retained as a counterexample to fixture-execution claims.
Semantic/per-field comparison, defaults, other profiles and original acceptance
remain open. No dependency or runtime package change. Full race fails at maximum SQLite readonly
retry with the unchanged 30s shared context; no retry, timeout change or merge.
The last passing main coverage remains app 89.28% / all Go 89.16%. Package invariants and
mutation sweep were not rerun; Source exporter controls and 19 scans pass.

The [updated base](docs/mini-loop-layered-architecture.png) adds operator contract
audit evidence. The [iteration image](docs/audits/go-contract-consumers-infographic-2026-10-12.png) edits that new base and highlights
L1 CLI and L5 contract inventory. Both exact built-in prompts saved, both PNGs
inspected. Canonical Mermaid/JSON updated; HTML passes 9 checks, zero errors/warnings.
Rendered browser acceptance remains blocked by the prior URL policy.

Next: diagnose and reduce the current maximum SQLite readonly shared-budget cost
without increasing the 30s context or retrying to green. Then review actual fixture
reads/assertions, retaining parser and separate Linux acceptance as unresolved work.

## 2026-10-12 managed HTTP memory caller profiles

Implementation `afe9207`, based on local main `0898b7d`. Actual Python FastAPI TestClient POST and SSE
handlers reveal distinct default-recursion budgets: POST accepts 968 nested
candidate containers (970 including array/entry), SSE accepts 972 (974 total).
The 64 cases span extraction/consolidation, array/object shapes and eight depths.
Required native assertions compare final wire text/SSE memory frames, owner and
foreign records, cross-owner HTTP refusal, run state, model budgets and events.
These are actual ASGI handler and native httptest adapter contracts, not a live
TCP server or universal CPython stack emulation. Source is Python 3.11.3 with
recursion 1000; other host/custom-recursion/caller profiles remain open.

A corrected pre-fix run retains 12 actual POST differences at depths 969/971/972.
The earlier 64 failures came from missing fake-provider protocol identity and
are retained as a separate fixture setup failure. The production fix adds a
private ephemeral memoryReplyScope selected by AuthenticatedHTTPPostRunContext.
Ordinary HTTP provenance/capabilities, snapshots and peer delegation keep their
existing rules. Agent.run/SSE remain at 974 and direct side helpers at 979.
Parsing still precedes writes/replacement; owner binding remains unchanged.

Frozen 1656 inputs pass all 14 gates: Source 240 files, focused Agent/HTTP tests, four
type audits, full ordinary Go/vet/race+atomic, Python, 19 scans and selected mutation
guards (memory 18, consolidation 1). Python: 2240 passed, 28 skipped, 96 warnings, 24
subtests passed in 137.73s (0:02:17). Application coverage 89.28% (33185/37168); all Go
89.16% (33356/37412); Provider 86.67% (1522/1756).
[Full receipt](docs/audits/go-memory-managed-http-2026-10-12.json); [coverage](docs/audits/go-coverage-memory-managed-http-2026-10-12.json). Twelve corrected native differences
and the separate provider-fixture setup failure are retained. No new dependency
or Python runtime-module change. Package invariants and full mutation sweep were
not rerun; prior temporal failures and original migration acceptance remain open.

The [iteration image](docs/audits/go-memory-managed-http-infographic-2026-10-12.png) edits the layered base, highlighting L1 entry,
L3 Session parsing and L5 owner-memory admission. No component added/deleted;
base unchanged. Exact built-in prompt saved and PNG inspected. Canonical Mermaid
and JSON updated; generated HTML passes 9 checks with zero errors/warnings.
Rendered browser acceptance remains blocked by the prior URL policy.

Next: reconcile other public caller/host profiles and broader memory effects,
then original migration inventory and resource/performance acceptance.

## 2026-10-12 memory reply caller and resource profiles

Implementation `9cb02fc`, based on local main `eaa5f66`. Actual Source expands to 135 extraction, 126
consolidation and 85 Agent.run endpoint cases (346 total). The final corpus adds
30/30/42 cases; the complete exploratory 474-case snapshots and their 112 native
differences are retained separately. Final cases cover adjacent array/object
nesting boundaries, consumed fields, ignored duplicate fields, deep entries after
five valid items, a 50,000-element ignored array, 2 MiB ignored text, 100,000 Unicode
characters and a malformed wide tail. Large replies are rebuilt from named
fixture recipes and checked against their actual Source SHA-256 before use.

The pinned CPython default recursion setting is 1,000. Actual direct consumers
accept 977 nested containers inside the candidate; the three actual normal,
exhausted and stuck stop/resume paths accept 972. Including the outer array and
entry object, native memoryReplyScope selects 979 or 974 container budgets.
Trusted code selects the scope; reply fields cannot. A typed syntax preflight
checks the complete selected array before closed decoding, five-item selection
or replacement deletion. Integer admission remains 4,300 digits. The shared
archive decoder keeps its existing profile. No whole-reply byte or element cap
is invented; the finite wide/text controls still match Source.

These are pinned caller profiles, with required native assertions through actual
side operations and Session.Run. Other host versions, custom recursion settings,
additional caller/HTTP stacks, memory exhaustion and release resource/performance
acceptance remain open; this cohort cannot establish universal Python-stack
emulation or full original P0/P1/G0-G7 acceptance. The initial Source export was
blocked by sandbox loopback bind permissions; its exact failure is retained and
subsequent authorized local-loopback generation succeeds.

Frozen 1654 inputs pass all 14 gates: Source 239 files, focused memory tests, four type
audits, full ordinary Go/vet/race+atomic, Python, 19 scans and selected mutation guards
(memory 18, consolidation 1). Python: 2240 passed, 28 skipped, 96 warnings, 24 subtests
passed in 96.65s (0:01:36). Application coverage 89.30% (33181/37158); all Go 89.17%
(33352/37402); Provider 86.67% (1522/1756).
[Full receipt](docs/audits/go-memory-json-resources-2026-10-12.json); [coverage](docs/audits/go-coverage-memory-json-resources-2026-10-12.json). All 32 final baseline differences
are retained. No new dependency or Python runtime-module change. Package
invariants and full Python mutation sweep were not rerun. Prior temporal failures
remain open.

The [iteration image](docs/audits/go-memory-json-resources-infographic-2026-10-12.png)
edits the layered base, highlighting L3 caller parsing and L5 pre-effect admission
phases. No component added/deleted; base unchanged. Exact prompt saved. Canonical
map and JSON synchronized; generated HTML passes 9 checks, zero errors/warnings.
PNG inspected; browser visual acceptance remains blocked by prior URL policy.

Next: reconcile other actual public/HTTP caller stacks and host profiles, then
broader memory side effects and release resource/performance acceptance. Custom
recursion settings and original migration criteria remain open.

## 2026-10-12 memory callback failure containment

Implementation `9194543`, based on local main `32ac048`. Actual Agent.run captures add 32 masker-callback
scenarios to the 11 endpoint controls, totaling 43. Normal completion, round
exhaustion and stuck stop/resume endpoints exercise first/second extraction,
first/second replacement, final index failure, repeated faults and cancellation;
below-threshold controls prove that partial extraction may remain without a
later consolidation. Records, foreign records, mask counts, model budgets,
request purposes, memory events and capture-error counts are required matches.
All 29 failing pre-fix scenarios escape as native panics. The initial command
launched outside go/ is retained separately as a command setup failure.

Go now contains ordinary callback panics at the same side-operation boundaries
as Source: extraction contains its initial List and all writes; consolidation
reads List before entering its contained preparation/replacement stage. An
extraction fault returns zero and still permits consolidation. Panics retain
already-written files and release the shared lifecycle lock. Ordinary panic payloads
are not retained or emitted. Only an ephemeral error assertion classifies native
persistence/lease authority failures, which still propagate, as does context
cancellation. Six additional native public-session controls cover wrapped ordinary,
transcript and lease errors at extraction/replacement boundaries. The raw memory
library's callback contract and owner isolation remain unchanged.

The 201 existing extraction/consolidation field scenarios still pass. Parser
depth/resource limits, broader callback side effects, whole-memory/public-entry
scope and original P0/P1/G0-G7 acceptance remain open.

Frozen 1653 inputs pass all 14 gates: Source 239 files, focused memory tests, four type
audits, full ordinary Go/vet/race+atomic, Python, 19 scans and selected mutation guards
(memory 18, consolidation 1). Python: 2240 passed, 28 skipped, 96 warnings, 24 subtests
passed in 95.93s (0:01:35). Application coverage 89.30% (33175/37149); all Go 89.18%
(33346/37393); Provider 86.67% (1522/1756).
[Full receipt](docs/audits/go-memory-callback-faults-2026-10-12.json); [coverage](docs/audits/go-coverage-memory-callback-faults-2026-10-12.json). All 29 pre-fix panic escapes
are retained. No new dependency or Python runtime-module change. Package
invariants and full Python mutation sweep were not rerun. Prior temporal failures
remain open.

The [iteration image](docs/audits/go-memory-callback-faults-infographic-2026-10-12.png)
edits the layered base, highlighting L3 exception boundaries and L5 partial-write/lifecycle
phases. No component added/deleted; base unchanged. Exact prompt saved. Canonical
map and JSON synchronized; generated HTML passes 9 checks, zero errors/warnings.
PNG inspected; browser visual acceptance remains blocked by prior URL policy.

Next: capture actual parser depth/resource limits and malformed whole-reply
boundaries, then complete the broader memory/public-entry and callback side-effect
review. Original migration criteria and previous temporal failures remain open.

## 2026-10-12 memory write encoding order

Implementation `387061f`, based on local main `e6a50b8`. Actual Python consumers add 24 extraction and
24 consolidation scenarios, totaling 105 and 96. The 12 combined header/body
recipes run under both authenticated and anonymous owners. They compare exact
records, owner diagnostics, unchanged foreign data, requests and mask-call counts.
The corrected pre-fix corpus has 34 failing scenarios. Initial anonymous fixture
owner mismatch and one fixture OwnerID compile error are retained separately;
they are test setup failures, not behavioral discrepancies.

Model entries always select the named memory.Body lane, including absent body.
That lane defers header UTF-8 validation to Source consumption phases: owner hash
first; fallback slug name hash before body consumption; body len/truncation and
diagnostics; text masking; authenticated original-name hash; final UTF-8 encoding.
The ordinary typed Input.Body path retains its upfront UTF-8 admission. Masking
can repair description/body and anonymous nonfallback names, but cannot repair
an authenticated original-name identity or a fallback slug hash. No owner, path,
origin, rollback, dependency or activation authority is added.

The known combined header/body repair cases now match Source. Masker exception
and side-effect behavior, parser depth/resource limits, other public-entry
profiles and original P0/P1/G0-G7 acceptance still require evidence. Initial
focused regex did not select memory-package tests; an unfiltered run covers that
package and the final ordinary/race gates include all packages.

Frozen 1652 inputs pass all 14 gates: Source 239 files, focused memory tests, four type
audits, full ordinary Go/vet/race+atomic, Python, 19 scans and selected mutation guards
(memory 18, consolidation 1). Python: 2240 passed, 28 skipped, 96 warnings, 24 subtests
passed in 114.47s (0:01:54). Application coverage 89.30% (33166/37142); all Go 89.17%
(33337/37386); Provider 86.67% (1522/1756).
[Full receipt](docs/audits/go-memory-write-order-2026-10-12.json); [coverage](docs/audits/go-coverage-memory-write-order-2026-10-12.json). All 34 corrected pre-fix differences
are retained. No new dependency or Python runtime-module change. Package
invariants and full Python mutation sweep were not rerun. Prior temporal failures
remain open.

The [iteration image](docs/audits/go-memory-write-order-infographic-2026-10-12.png)
edits the layered base, highlighting L3 default-body lowering and L5 write-order
phases. No component added/deleted; base unchanged. Exact prompt saved. Canonical
map and JSON synchronized; generated HTML passes 9 checks, zero errors/warnings.
PNG inspected; browser visual acceptance remains blocked by prior URL policy.

Next: inspect masker exception/panic and side-effect behavior at actual public
entry points, then parser resource/depth boundaries. Original migration criteria
remain open; the captured success/repair profiles do not prove callback failures.

## 2026-10-12 typed memory body consumption

Implementation `6e4ef14`, based on local main `48a09fa`. Actual Source adds 22 extraction and 22
consolidation body scenarios, taking the totals to 81 and 72. Twenty-two native
differences reproduce before repair. Scenarios include all JSON body kinds,
original element-count versus rendered-text length, 32,000/32,001 boundaries,
Unicode, unpaired surrogate prefixes and a masker repairing invalid body text.
Raw and bound-owner diagnostics are compared; foreign binding diagnostics stay
empty alongside unchanged foreign records.

`memory.Body` is an immutable named projection with private kind, text and
original length. `TextBody`, `ContainerBody`, `UnsizedBody` and `Input.WithBody`
carry no JSON payload, owner, path or execution grant. The existing ordinary
Input.Body string path remains available. Agent lowers closed reply values before
storage; body errors occur only when Store writes that entry. Lists/objects use
original element count, so their representation can exceed the string cap.
Oversized containers report truncation then fail. Scalar bodies fail len at the
write phase; consolidation therefore retains earlier replacement writes after
owner-scoped deletion. Its pre-existing unhashable original-identity check still
refuses container bodies before any deletion.

Observed text counts surrogatepass code points, truncates first, then masks and
checks final UTF-8 before atomic write. A surrogate outside the retained prefix
is discarded; one inside fails after the diagnostic unless masking repairs it.
No rollback, component, optional activation, owner binding or dependency change.
Combined malformed headers/body/masker faults, full resource/depth limits and the
original P0/P1/G0-G7 acceptance remain open; captured body parity is not whole-port
acceptance. The initial focused regex selected no memory-package tests; a separate
unfiltered memory-package run passed and final ordinary/race gates include it.

Frozen 1652 inputs pass all 14 gates: Source 239 files, focused memory tests, four type
audits, full ordinary Go/vet/race+atomic, Python, 19 scans and selected mutation guards
(memory 18, consolidation 1). Python: 2240 passed, 28 skipped, 96 warnings, 24 subtests
passed in 95.13s (0:01:35). Application coverage 89.28% (33152/37133); all Go 89.15%
(33323/37377); Provider 86.67% (1522/1756).
[Full receipt](docs/audits/go-memory-body-values-2026-10-12.json); [coverage](docs/audits/go-coverage-memory-body-values-2026-10-12.json). All 22 pre-fix differences
are retained. No new dependency or Python runtime-module change. Package
invariants and full Python mutation sweep were not rerun. Prior temporal failures
remain open.

The [iteration image](docs/audits/go-memory-body-values-infographic-2026-10-12.png)
edits the layered base, highlighting L3 body lowering and L5 write/diagnostic
phases. No component added/deleted; base unchanged. Exact prompt saved. Canonical
map and JSON synchronized; generated HTML passes 9 checks, zero errors/warnings.
PNG inspected; browser visual acceptance remains blocked by prior URL policy.

Next: capture combined invalid-description/oversized-body diagnostics and masking,
invalid-name slug-fallback versus authenticated filename hash order, and absent
body with repaired description. The single-field corpus does not establish those
combined stages or full original migration acceptance.

## Memory header values and original identity — 2026-10-12

Implementation `561928e`, baseline `1aaa99d`: name/description follow Python str, non-text types normalize
to project, and consolidation compares original identity before conversion or
deletion. All 109 Source cases match; 54 pre-fix differences and 24 caught origin
mutations are retained. All 14 final gates pass; [receipt](docs/audits/go-memory-header-values-2026-10-12.json).
Body conversion/failure stages and full original migration acceptance remain open.

## Memory reply JSON boundary — 2026-10-12

Implementation `27f7c4e`, baseline `e9c330a`: immutable closed JSON values replace transient RawMessage
on the extraction/consolidation path. All43 actual Source scenarios match,
including full-parse refusal, legacy values, five-item consumption, UTF-8 partial
writes and owner/origin isolation. All14 final gates pass; earlier failed and
interrupted attempts remain explicit in the [receipt](docs/audits/go-memory-reply-json-2026-10-12.json).
See [current plan](GO_PORT_PLAN.md#2026-10-12-memory-reply-json-boundary) for details
and remaining scalar/body/consolidation failure stages. Full port acceptance open.

## Native callback panic boundaries — 2026-10-12

Implementation `46697bc`, baseline `fc89e97`: compiler body inventory found139 open declarations in21,820
examined declarations. Four recover sinks formatted panic payloads with `%v`;
actual callback tests reproduce formatting and disclosure before the fix.
Plan review and cron resolver/runner/persistence now use type-only diagnostics.
Focused tests pass without changing approval or occurrence/cleanup semantics.
All14 frozen gates pass; [full receipt](docs/audits/go-panic-boundaries-2026-10-12.json).
Raw JSON escape review, other compiler profiles and
original migration acceptance remain open; inventory findings are not exemptions.

## Package variable compiler closure — 2026-10-12

Implementation `1799d20`, baseline `d273a8e`: `typeaudit -variables` now resolves inferred, exported and
private package variable roots through the active compiler/CGO source inventory.
Focused fixtures identify11 open payload variables while older named-root modes
pass; closed/platform/shadowed controls and invalid-inventory refusals pass.
Current50 packages/172 variables have zero findings. All12 frozen gates pass;
[full receipt](docs/audits/go-package-variable-closure-2026-10-12.json).
No runtime component, dependency, authority or default is changed. Function-local,
body/method, foreign/inactive-profile and runtime-flow review remains G0 work.

## Native team SQLite restart — 2026-10-11

Source `ce544f6`, native test `c19d7f2`, baseline `bf75663`: actual Source now captures first reopening, one resumed turn
and second reopening. Native SQL integration compares public restoration state,
request/history, counters, own-team lead identity and separate retained mailbox.
Existing injected-store tests separately check private role/runner/roster loss.
All11 frozen gates pass. No runtime/default change. [Full receipt](docs/audits/go-team-sqlite-restart-2026-10-11.json).

## Private compiler type roots — 2026-10-11

At baseline `2c76207`, native typeaudit adds -private for every active package-level
unexported named root, using compiler-selected/CGO-generated sources and go/types.
Focused adversarial fixtures catch aliases/generic open fields unreachable from
exported API, preserve shadowed names/closed recursion/platform handles, and honor
inactive build tags. Current455 syntax files/1441 named types, public50 packages/
1191 exported types and private50 packages/261 roots have zero findings. No new
dependency, runtime component, owner, default or authority was introduced.
Package variables, local types, bodies, opaque foreign internals, other build
profiles and runtime data flow remain explicit G0 work; full migration remains open.

## SDK integer-text zero padding — 2026-10-11

Baseline `338792a`:24 actual direct/SSE cases expose20 parity differences and
retain four oversized-significant-digit controls. Private model-array integer
text construction now counts significant digits, allowing5000 leading zeros to
normalize to small inert integers. Positive/negative/zero/decimal/separated
profiles pass required checks. Total inventory86 cases/48 snapshots/38 delta
faults,91 archival/SQLite projections and86 managed HTTP outcomes. Real count
and metering limits remain unchanged; full migration acceptance remains open.

## Inert model-array ranges — 2026-10-11

At main baseline `110b1a9`,24 new actual SDK cases expose22 native differences:
whole model-array repr incorrectly used the counted decoder. The private named
model construction now preserves arbitrary integer observations/eligible wide
integer text and finite wide floats, without numeric authority. Required checks
cover62 direct/SSE cases/36 stages and26 delta faults,67 mask/archive/SQLite
projections and62 real HTTP/ManagedSession cases. Real metering/count limits
remain unchanged; broader lexical/model/secret/public-error/default and full
original migration acceptance remain open.

## Native whole usage observation checkpoint — 2026-10-11

At local main baseline `7095efb`, the explicit SDK whole-usage adapter preserves
null/text projections and partial named models.38 actual SDK direct/SSE cases,
24 stage snapshots and14 delta-assignment errors have required parity checks;
43 projected snapshots/final values pass mask/clone/archive/SQLite reopening.
38 actual HTTP/ManagedSession cases verify model-end projection and handled
error events. Strict native counted-object decoding retains its required fields,
and whole observations gain no meter or execution authority. Source enum/model
repr is captured for the current inventory; wider count domains and final public
error/default/Provider/G0–G7 acceptance remain open. Base topology unchanged;
canonical metadata and generated interactive HTML are synchronized.

## Whole usage Source checkpoint — 2026-10-11

At local main baseline `4f75bc9`,28 actual SDK direct/SSE cases and19 snapshots
fix whole-usage missing/null/scalar/list/partial-model behavior. All14 direct cases
complete;9 SSE cases fail at delta after a start snapshot,5 object SSE cases
complete. Native static admission remains stricter; this checkpoint adds Source
contracts, not a Go parity implementation or refreshed coverage. See the current
plan's whole-usage tasks and `docs/audits/go-whole-usage-source-2026-10-11.json`.

## Current acceptance index — 2026-10-13

Reviewed **2026-10-13**, accepted main `82b14bf`. The
[accepted role-policy validation](docs/audits/go-role-policy-profiles-2026-10-13.json)
records 252 Source exports and 252 passing consumer candidates of 253; only the
default parser is unreferenced. Its full Go/vet/race gates were completed in
segments with identical recorded program fingerprints, retaining two
host-pressure stops and hashed documentation-only corrections. Those accepted
Go gates, including development reload, remain the current baseline.

The new [performance/tooling receipt](docs/audits/go-performance-resource-budget-2026-10-13.json)
records focused Python 4 passed, build, invariants, scans and diff passes, plus
full Python **2253 passed, 28 skipped, 96 warnings, 24 subtests passed in 156.68s**.
Go tests, vet, race and coverage were not rerun in this iteration. Accepted
Darwin statement coverage remains app **89.41%**, all Go **89.16%**, Provider
**86.67%**. The [role-policy coverage](docs/audits/go-coverage-role-policy-profiles-2026-10-13.json)
uses only the complete successful race profile; partial profiles remain excluded.

**P0 inventory acceptance is satisfied** under its original work-package
criterion: 12 README capability/default rows and Python-test mappings, s01–s20
labels, 49 settings, 44 HTTP operations, three SDK producers and captured SQLite
schema. The six former namespace omissions have explicit bindings: all 157
exports are mapped as 52 name candidates + 105 explicit candidates + zero
missing. Namespace mapping does not imply complete semantic parity; G1–G7 own
that evidence. RunContext has 70 constructor/live-turn cases; role policy has
34 selections, seven child executions and six invalid capability admissions.

The current body inventory's 130 observations are individually classified with
[owner-reviewed dispositions](docs/audits/go-body-boundary-dispositions-2026-10-13.json).
No open dynamic representation was found retained in repository-owned
Darwin-profile domain/service fields. Conditional original-error propagation,
panic rethrow, request-local wire buffers and compiler-generated CGO declarations
are separate dispositions. Arbitrary error-object graphs, foreign internals,
other profiles and total allocation bounds are not proved by this review;
they are not substituted for the original finite G0 contract.

Explicit required gaps include default CPython parser approval and its 62-case
native consumer, two Unicode-host direct/SSE cases, and the full fixed-environment
Linux gates. Other finite unaccepted rows below remain; these three gaps are not
an exhaustive replacement for G7. Historical checkpoints retain their original
scope, measurements, failures and limitations.

Previous verified implementation checkpoint:

Reviewed **2026-10-13**: implementation `833e16f`, native MCP ordered scalar-key mapping
and Source fault-stage alignment.116 direct and116 actual journal profiles pass
required assertions, including original88 differences and added nonfinite roots/
values. Immutable named entries preserve first-key kind, Python numeric equality
and last-value replacement. Unhashable keys fail batch preparation; non-text
keyword binding fails after permission, sorted query faults precede result
observers even without an active reducer. Explicit/callable MCP objects stay
finite; external risk unchanged. Direct/SSE, clone/mask/archive and seven real
SQLite close/reopen/owner-restore profiles preserve original roots, event stages,
current authority and fresh leases without external-effect replay.
Frozen1695 inputs pass Source247/manifest, focused Go/Python, four type gates,
collector/body inventory, ordinary Go/vet/race+atomic, full Python,19 scans,
6 guards and diff. Consumer inspection exits1 only for the absent syntax-parser
consumer. [Historical MCP receipt](docs/audits/go-mcp-mapping-native-2026-10-13.json).
Previous full Python memory-scaling failure remains retained without a runtime/
threshold fix or cause claim; this new implementation cohort is separate.
Broader mapping/NaN-identity/caller/transport and original P0/P1/G0-G7 stay open.

Previous verified Go implementation checkpoint:

Reviewed **2026-10-13**: implementation `48f7b85`, based on main `6cb887e`,
adds finite MCP model roots and whole-batch Source `dict(input or {})` preparation.
Fifty-two actual Source root/batch/auto/readonly profiles compare arguments,
failure classes/messages, hooks/results, history and external-risk refusal;
48 original native failures retained. Direct/SSE, clone/mask/archive and four
native SQLite reopen/owner-restore profiles preserve original roots and current
authority without replaying effects. Explicit MCP arguments stay object-only.
Frozen1686 inputs pass Source245/manifest, focused Go/Python, four type audits,
collector tests/body inventory, ordinary Go/vet/race+atomic, full Python,
19 scans,6 guards and diff. Parser consumer inspection remains exit1.
[Historical MCP receipt](docs/audits/go-mcp-model-roots-2026-10-13.json).
The initial full cohort's obsolete MCP-null negative and interrupted race remain
retained; actual Source evidence justified model/explicit boundary assertions.
Nonfinite/non-string-key/wider roots, original P0/P1/G0-G7 and dependency decisions
remain open. No connection, risk, authority, default or new dependency changes.

| Scope | Current authoritative evidence | Current conclusion |
| --- | --- | --- |
| Directory split / plan | `python/`, `go/`, `GO_PORT_PLAN.md` work packages; current full Python/tooling receipt | Directory split delivered; original P0/P1/G0–G7 criteria remain unchanged. Current Python/tooling checks pass; final cross-runtime acceptance remains separate |
| Source recipe freshness | Accepted role-policy exporter `--check`: 252 files | Recipe freshness passed in the accepted runtime profile; not rerun by this performance/tooling iteration. Required release gates remain open |
| Default business routing | All 168 actual Source requests on 40 explicit paths; [native evidence](docs/audits/go-http-routing-native-2026-10-11.json) | Delivered at 4b68574: first-route Allow, known slash redirects and decoded-path semantics match. Required parity assertion replaces diagnostic-only acceptance; broader URL/mount/forwarded-host scope remains |
| Managed memory HTTP | [64 actual POST/SSE contracts and frozen gates](docs/audits/go-memory-managed-http-2026-10-12.json) | Pinned Source POST/Agent.run+SSE/direct container budgets 970/974/979; trusted selector is ephemeral with unchanged authority. Wire events, owner/foreign records and lifecycle match; other caller/host profiles and full acceptance remain open |
| Source/native consumer inventory | [Current receipt](docs/audits/go-role-policy-profiles-2026-10-13.json): 252 exports plus separate Linux capture | 252 of 253 candidates observed passing in ordinary tests; only parser unreferenced; required race/Python gates completed in a later segment; Linux runtime not certified on Darwin |
| Inspection JSON / shared budget | [Source bytes, same-budget diagnostics and full gates](docs/audits/go-inspection-json-spans-2026-10-12.json) | Formatter spans retain 56 exact profiles. Its recorded full race failed at 31.00s; the later legacy-text cohort below passes the unchanged 30s context. Prior failures, ExternalCode limits and escaped-allocation tradeoff remain retained; no all-host timing claim |
| Memory-selection caller profiles | [300 required direct/Agent/Managed/POST/SSE Source cases](docs/audits/go-memory-selection-profiles-2026-10-12.json) | Total containers982/980/978/972/976 match adjacent array/object bounds; private choice adds no authority or stored/delegated state. Full gates pass; custom host/caller stacks remain open |
| Closed memory selection | [28 required Source context cases](docs/audits/go-memory-selection-values-2026-10-12.json) | Whole closed-value decode repairs five prior differences; full gates pass. Boolean/order/duplicate/first-five and owner/meter/cancellation behavior preserved. Five ordinary caller profiles now have300-case proof; custom host/caller recursion remains open |
| Legacy text spans | [Source hashes and full gate outcomes](docs/audits/go-legacy-text-spans-2026-10-12.json) | Scalar UTF-8 reuse preserves44 compact/pretty/surrogatepass projections. Full race passes30s maximum SQLite; ordinary/Python timing failures retained. Other profiles and original acceptance remain open |
| Native persistence / recovery | `go/sqlitestate/*_test.go`, `go/launcher/state_test.go`; actual schema/reopen/owner/lease/crash/cron/compaction/decision/team-resumed-turn cases | Native backend and restart integration implemented; G7 scope reconciliation remains |
| Native benchmark arms | `go/benchmark/runner.go`, `fake.go`, runner/filter/fake tests, benchmark HTTP adapter | Implemented; earlier arm-pending notes are superseded; final performance review remains |
| Glob truncation inputs | [Same-directory Source/Go pair and Source iterator controls](docs/audits/go-glob-enumeration-control-2026-10-11.json) | Darwin pair/fixture exactly match555 names and49,973 characters; selection depends on pre-sort enumeration. Linux same-filesystem Source capture remains open; relative `*.txt` does not charge absolute root length |
| Full-feature composition | `go/agent/manager_features.go`, `go/launcher/features_test.go`, Source full-feature fixtures | Explicit provider-backed Manager/Launcher composition implemented; default CLI backend unavailable |
| Usage integer text | [76 new direct/SSE cases and current full gates](docs/audits/go-usage-text-2026-10-11.json) | Exact decimal-zero and integer-separator validation repairs rounding/format differences; event kinds/stages and PromptTokens match. Native ranges and broader malformed metadata shapes remain open |
| Whole SDK usage | [Current required SDK/wire/managed and archival evidence](docs/audits/go-private-type-closure-2026-10-11.json) | Current private model-array construction:86 direct/SSE cases/48 stages and38 delta faults;91 projections survive clone/mask/archive/SQLite;86 managed HTTP outcomes verified. Broader ranges/repr/public error scope remains open |
| Known usage metadata shapes | [191 actual SDK cases/214 stages and full current receipt](docs/audits/go-private-type-closure-2026-10-11.json) | Named inert observations, model/stage fallback, actual event masking/clone/archive and191 SQLite reopen rows pass required assertions. Original100 differences repaired; 86 whole-usage cases now match projected values/stages/messages; broader ranges and final field/default/consumer/public-error scope remain open |
| Registered custom tool inputs | [Typed codec and actual Source gate proof](docs/audits/go-extension-input-codecs-2026-10-13.json) | Fourteen outcomes/13 exact traces; readonly-write scheduling adaptation retained. Explicit named codec/handler runs after common gate; schema-only observations still refused. Wider coercion/ownership/parallel profiles remain |
| Complete custom schema metadata | [Actual Source requests and native SQLite reopen](docs/audits/go-extension-schema-preservation-2026-10-13.json) | Sixteen metadata shapes/32 managed owner sessions and16 SQL profiles match snapshots/fingerprints/model tools/current authority. Inert finite values preserve all fields; no validator or ref loading. Non-object server acceptance and other schema profiles remain |
| Custom child callback scope | [Eighteen actual Source role/capability/risk profiles](docs/audits/go-extension-subagents-2026-10-13.json) | Typed adapters survive capability pruning; current Session stamps fixed label/depth; peer clears human approvals and Explore remains readonly. Scope grants no permission or quota. Arbitrary mutable Agent/state and wider lifecycle/parallel/remote profiles remain |
| MCP finite model roots | [52 initial batch/gate profiles](docs/audits/go-mcp-model-roots-2026-10-13.json) and [later scalar-key/nonfinite proof](docs/audits/go-mcp-mapping-native-2026-10-13.json) | Preparation before effects, original history, external-risk readonly refusal and four initial reopen/owner-restore profiles match. Later 116 direct + 116 journal profiles cover the captured nonfinite roots/values and scalar keys; these are no longer listed as missing. Explicit MCP arguments remain object-only |
| MCP ordered scalar keys | [116 direct/116 journal Source profiles and native SQLite](docs/audits/go-mcp-mapping-native-2026-10-13.json) | Captured nonfinite roots/values, ordered scalar keys, native stages/effects/hashes and original roots match; seven reopen/owner-restore cases check persisted events/current authority/no replay. Arbitrary callable-object equivalence is not claimed; other finite unaccepted G6/G7 rows remain |
| Public namespace/default inventory | [12 README rows / 157 exports / 49 settings / 44 HTTP operations / 3 SDK producers](GO_PUBLIC_SURFACE_INVENTORY.md); captured SQLite schema | Original P0 inventory acceptance satisfied: 52 name candidates + 105 explicit mappings + zero missing bindings, with README Python-test mappings and storage/default/optional-feature inventory. Semantic parity remains in G1–G7; default parser and required Unicode-host consumers remain open |
| Default full-feature CLI | `go/config/server.go` and `go/agent/manager_features.go` refusal; 62-case default-parser Source fixture | Open: explicit approval for the default CPython syntax-parser dependency and its native 62-case consumer remain required; SQLite-driver approval does not approve this backend |
| Development reload | [Accepted main `82b14bf` role-policy ordinary/vet/race gates](docs/audits/go-role-policy-profiles-2026-10-13.json), raw C and same-subscription queue/control receipts | Latest accepted Go gates include reload; not rerun for this tooling iteration. Prior temporal/resource failures remain retained without a watcher runtime fix or cause claim. Cross-platform and other finite unaccepted G3/G7 reliability rows remain |
| Package-level types and variables | [Accepted four type gates/body inventory](docs/audits/go-role-policy-profiles-2026-10-13.json); [owner-reviewed body dispositions](docs/audits/go-body-boundary-dispositions-2026-10-13.json) | 465 production files / 1473 named types; 1206 public roots / 278 private roots / 172 variables; zero findings in the four gates. Body: 51 packages / 22288 declarations / 130 individually classified observations. Recorded Darwin repo-owned domain/service boundaries retain no observed open payload fields; standard error propagation, request-local wire state and three generated CGO declarations are explicit. No new compiler rerun or all-profile/error-graph/memory-bound claim; full G0 not declared here |
| Linux public type closure | [Pinned CGO profile evidence](docs/audits/go-linux-public-types-2026-10-11.json) | Linux amd64/CGO1 compiler closure passes50 packages/1189 exported types/zero findings in existing fixed image through compatibility layer. Local CGO0 and arm64-CGO inventories fail; other profiles/private roots/foreign internals/runtime flow remain open. Full Linux amd64 gates terminal:ordinary/race each8 failed packages,vet passes. Native arm64/context cohort terminal:public types/vet pass,ordinary/race each7 failed packages; not accepted |
| Linux full runtime gates | [Terminal one-shot receipt](docs/audits/go-linux-full-gates-2026-10-11.json) | amd64 compatibility layer:ordinary1/vet0/race1;readonly HOME error and platform/phase/signal differences retained. No same-environment Source recapture or full Linux acceptance; native arm64 ordinary1/vet0/race1 and public types0;7 failed packages in each test gate |
| Owned provider literal targets | [Required transport inventory](docs/audits/go-provider-literal-target-2026-10-11.json) | Delivered: 224 request objects / 40 plain HTTP wire / 24 proxy-TLS assertions; 16 SNI / 8 CONNECT checks. Two required Unicode-host direct/SSE cases remain unaccepted; broader IDNA/custom transport evidence is not inferred |
| Completed reply metadata | `go/testdata/python-provider-reply-metadata.json`: 44 actual SDK 0.107.1 direct/stream captures | Implemented at 3916f71: named finite observations, defaults, normalized timestamps, SDK delta retention, cloning and public JSON match all 44 captures; arbitrary coercion/custom transport and final acceptance remain |
| Scratch cleanup | Python manager `_remove_workspace` / `_finalize_workspace_cleanup`; Go `reclaimUnusedWorkspace`; README boundary | Both remove scratch directory/link without Git registration cleanup; a Source Git-aware reclaimer is not missing implementation |
| Matched fake HTTP performance | [Current resource-budget report](docs/benchmarks/go-port-fake-http-resource-budget-2026-10-13.json) and [validation receipt](docs/audits/go-performance-resource-budget-2026-10-13.json) | Three process pairs, totaling 60 measured turns / 300 health requests and six warmup turns per runtime, with verified Bash effects and latency/RSS observations. Nonexclusive host and startup outlier retained; this bounded workload does not establish all-workload performance or platform parity |
| Provider citation variants | Source SDK 0.107.1: 35 completed/forwarded cases, two streamed/forwarded cases; native provider and protocol tests | Five named variants, SDK normalization, bounded SSE, forwarding and rewrites implemented; native direct/stream HTTP + SQLite close/reopen + masked recordings verified with an explicit registry; final provider inventory remains |
| Explicit provider authentication | Ten pinned Source recipes, each compared against native direct and SSE request headers; detached configuration, effective-header admission and credential redaction tests | Explicit key/bearer/ordered-header seam and Launcher environment selection implemented; four Settings/provider_for Source captures match managed native turns; 51 actual SDK discovery recipes are captured; Launcher compares 50 and provider tests cover same-client invalidation; defined-empty auth alignment delivered |
| Dynamic credential cache | Typed callback/token/error seam; provider race tests for SDK windows/backoff, forced exchange retry, cancellation/panic release, direct/SSE auth and concurrent token redaction | Explicit process-local cache implemented; native file-profile/user OAuth implemented; explicit native federation exchange implemented; profile federation disk cache implemented; Launcher automatic discovery delivered |
| Explicit file-profile / OAuth | Eight pinned Source cases replayed through native model/token HTTP mocks; actual loopback refresh, forced rotation, extension preservation, mode, cancellation, malformed exchange and redirect tests | Named native driver implemented with atomic private-file replacement; file-profile federation disk cache implemented; Launcher automatic discovery delivered; bounded ingress adaptations recorded |
| Explicit federation exchange | Six pinned Source exchange recipes compared through native model/token transport mocks; actual HTTP optional-field/copy/status tests, projected-file symlink rotation, SSE model-401, wire bounds, context and callback containment | Named jwt-bearer driver implemented; profile disk cache implemented; Source discovery precedence implemented; Client cache owns reuse |
| Federation profile file cache | Seventeen pinned Source profiles compared against native exchange/model headers, calls, errors, reconstructed clients, files and modes | Native profile dispatch, 30-second/fractional freshness, forced bypass, guarded reads, extension preservation and best-effort atomic writes implemented; Launcher automatic discovery delivered |
| Stream partial-input Source audit | 98 actual SDK SSE captures; latest native diagnostic at `9c01921`; [audit](docs/audits/provider-partial-input-2026-10-10.md) | Boundary per-delta parsing now matches all 98 SDK snapshots and 42 jiter edges; 16 rejection stages aligned; 78 finite client/server final/history/SQLite cases match; latest diagnostic retains four nonfinite final differences; diagnostic report is separate from parity assertions |
| Declared caller extensions | Forty-eight SDK direct/SSE cases in `go/testdata/python-provider-caller.json` | Three named kinds retain immutable extensions across client/server tool-use and web-search/web-fetch replies, masks/clones/forwarding/SQLite reopen and historical bounds; unknown caller kind/scalar audit remains |
| Text-editor execution replies | Forty-eight SDK direct/SSE captures in `go/testdata/python-provider-text-editor.json` | Named view/create/replace/error, nullable line integers/arrays/messages and SDK defaults, immutable extensions, selected SSE caller parsing, mask/clone/historical codecs, SQLite reopen and UTF-8 rollback implemented; twelve SDK kinds represented, complete field/default acceptance remains |
| Bash execution result replies | Twenty-six SDK direct/SSE captures in `go/testdata/python-provider-bash-execution.json` | Separate named result/error and output-file tags, shared integer return code, five errors/future codes, immutable extensions and SDK stream nulls; forwarding/mask/clone/historical codecs, SQLite reopen and UTF-8 rollback implemented; all twelve declared SDK kinds represented; complete field/consumer/default audit remains |
| Code-execution result replies | Twenty-eight SDK direct/SSE captures in `go/testdata/python-provider-code-execution.json` | Named ordinary/encrypted/error union, output files and unbounded integer return code, immutable extensions and SDK stream defaults; forwarding, mask/clone/historical codecs, SQLite reopen and UTF-8 rollback implemented; all twelve declared SDK kinds represented; complete field/consumer/default audit remains |
| Web-fetch document replies | Forty-two SDK direct/SSE captures in `go/testdata/python-provider-web-fetch.json` | Named document/result/error variants, text/PDF sources, title/retrieval/citations configuration, nullable SDK defaults and immutable extensions; forwarding, masking, historical codec, SQLite reopen and invalid UTF-8 rollback implemented; all twelve declared SDK kinds represented; complete field/consumer/default audit remains |
| Provider-owned tool-use observations | Thirty-two SDK direct/SSE captures in `go/testdata/python-provider-server-tool.json` | Named ID/name/input/caller, immutable finite observational input and root extensions, SDK null defaults, complete SSE accumulation and empty-delta semantics, mask/clone/historical codec and actual SQLite reopen implemented; inert client authority; all twelve declared SDK kinds represented; complete field/consumer/default audit remains |
| Server tool-search result replies | Twelve SDK direct/SSE captures in `go/testdata/python-provider-tool-search.json` | Named references/error union, nullable messages, stream defaults, immutable extensions, forwarding/mask/clone/historical codec implemented; metadata does not activate client tools; all twelve declared SDK kinds represented; complete field/consumer/default audit remains |
| Server web-search result replies | Fourteen SDK direct/SSE captures in `go/testdata/python-provider-web-search.json` | Named results/error union, optional age/caller defaults, immutable extensions, forwarding/mask/clone/historical codec implemented; all twelve declared SDK kinds represented; complete field/consumer/default audit remains |
| Provider container-upload blocks | Six actual SDK direct/SSE captures in `go/testdata/python-provider-container-upload.json`; named result-variant rows above | Named FileID and stream-null defaults in the closed Block union, forwarding and clone/mask/stored codec tests are implemented. All 12 declared SDK discriminators are now represented; complete field/default/consumer acceptance remains separate |
| Provider usage metadata | Nine actual SDK direct/SSE captures in `go/testdata/python-provider-usage.json`; Python `_usage_payload` and SDK Usage/stream accumulator hashes | Named metadata, nullable codec/default selection, immutable extensions, clone/masked event/archival JSON and HTTP/SSE adoption implemented; actual SQL events, reopen/restored writes, raw JSONL and reopened recording readers verified; broader usage coercion/field audit and final scope reconciliation remain; see [scope audit](GO_PROVIDER_SCOPE_AUDIT.md) |
| Background optional consumers | 48 standalone option profiles and 48 actual SDK/managed captures; runtime `4f56ef8` / `ea35cd3` | Timeout/prefix lowering, SDK admission and exact managed final replies implemented; cleanup/timer/scheduler adaptations and full-state scope remain explicit |
| Final acceptance | Original G7 contract/differential/performance/docs/audit gates | Open. P0 inventory is satisfied and recorded body observations are classified; default parser approval/62-case consumer, two Unicode-host direct/SSE cases, fixed-environment Linux full gates and other finite unaccepted rows still require closure |

Historical MCP full Python result: **2247 passed, 28 skipped, 96 warnings, 24 subtests passed in 93.36s (0:01:33)**.
Source247/manifest, focused controls, complete ordinary/vet/race, four type audits,
collector/body inventory,19 scans,6 guards and diff pass. Consumer inspection exits1
only for the unreferenced syntax parser. Prior memory scaling/temporal failures
remain retained without threshold/runtime fix or cause claim.

Historical MCP passing-race statement coverage: app **89.38% (33496/37478)**,
all Go **89.13% (33862/37993)**, Provider **86.67% (1522/1756)**.
[Historical MCP coverage](docs/audits/go-coverage-mcp-mapping-native-2026-10-13.json).
Darwin statements only; native C/subprocess interiors, Python, branches and other
profiles excluded. Original migration acceptance and historical temporal
reliability remain open.


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
| Configuration and standalone startup | `config.py`, `__main__.py`, `server.py`, `skills.py`; configuration/startup tests | Named settings, loud validation, explicit service wiring, bind guard, shutdown and redacted diagnostics | `go/config` has all 49 source fields; 64 real Python outcomes compare accepted values and rejected configurations. Go has explicit int/duration bounds and pure SDK loading without dotenv discovery or Python Settings workspace mkdir. Compiled commands now discover dotenv once from cwd/ancestors using Source frozen-program origin, publish prepared effects for child inheritance, and report disabled/enabled discovery; ordinary Python config-module origin remains an explicit SDK selection. No reload follows. `launcher.New` wires supported services, `App.Serve` owns TCP listener/shutdown, and cmd/miniloop handles SIGINT/SIGTERM. Default trajectory recording is active; its configured root failure refuses startup. Enabled unavailable services still refuse activation. The private spill root is now best-effort constructed by default; an empty path disables it, and root failure retains startup. Builtin skill bytes/descriptions/load match Python. `--dump-config` reports settings-and-availability without runtime/probe, redacts credential fields and URL secrets, and includes Go VCS build info; it is not full effective posture. Local HTTP provider and real shell timeout tests prove option wiring. Positive subnanosecond durations and nonfinite values refuse; fake-delay validation also runs for a real provider. Public console/UI shells are implemented; missing optional services and full posture remain G3/G5/G6. |
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


## Native development reload observation — 2026-10-10

`go/reload` adds a typed, explicitly selected source observer; no launcher default
or `MINILOOP_RELOAD` refusal is changed. WatchConfig owns normalized roots, finite
Go/Python language selection and a detached optional delay (nil selects 250ms).
Watcher retains resolved-path floating-point mtimes under its own mutex. Scan
records first-seen paths, detects only mtime increases and returns the first
changed path without replacing its baseline; Reset clears baselines after a
future supervisor replacement. Next supports context cancellation before scanning
and during the configured delay. Directory symlinks are not traversed, matching
Source; matching suffix directories and resolved file links still participate.
Filesystem discovery/stat errors are skipped as in Source polling.

Snapshot 200 (`python-reload.json`) invokes the actual installed Uvicorn 0.49.0
StatReload with zero delay over temporary real filesystem operations; it never
starts a server/process. Version and StatReload source SHA-256 are recorded.
31 steps cover initial/same/increasing/decreasing mtimes, repeat detection,
observer reconstruction, addition/deletion/recreation, unrelated suffixes, resolved
file symlinks and a matching-suffix directory. Go tests replay the same steps for
the exact Python suffix and explicit Go suffix adaptation. Additional native
checks cover cancellation, detached config and invalid construction.

The supervisor is still required to stop/join children, build native replacements,
retain listener ownership and refresh child startup. Those actions are absent
from this observer and are the next implementation slices. No runtime dependency
was added. The source exporter has a new recipe; no Python runtime module or
Go default-tool types changed. README review/boundary and the architecture language
card identify this library as explicit and unconnected to current startup.

Validation: narrow `go test -count=1 ./reload`, full `go test -p=1 -count=1
-timeout=300s ./...`, full `go test -race -p=1 -count=1 -timeout=300s ./...` and
`go vet ./...` passed from `go/`. The complete exporter check passed with 200
current files, and all 19 scanning guards passed. Installed Source default delay
was verified as 0.25s. Source/export deprecation warnings and Darwin CGO race-link
warnings remain nonfatal. `git diff --check` passed. Python runtime modules and
load-bearing guards did not change; full Python, invariants, mutation and coverage
were not rerun. Architecture delivery passed 9/9 with zero errors/warnings; exact
specification/artifact SHA-256 and byte counts were verified. Correction rounds:
0. Visual review was not repeated after the prior browser denial. No push.

Next reload slices retain the complete public-entry objective:
1. Typed build/artifact ownership using the repository Go toolchain: real syntax
   failure, corrected-source recovery, cancellation and private output cleanup.
2. Owned supervisor: stop/join before replacement, cancelled lifecycle and child
   exit behavior, retained listener and fresh child startup environment.
3. CLI selection and actual HTTP process proof: Source nonempty environment
   activation, observed source edit changing served native behavior, build failure
   then recovery and SIGTERM cleanup. The current refusal stays until these pass.


## Native reload build and artifact ownership — 2026-10-10

`reload.NewBuilder` captures explicit trusted module/package/toolchain configuration
and a detached environment. One local package is selected; option-shaped, wildcard
and parent-escaping targets are refused. Build uses the existing Go executable,
`-mod=readonly`, the current host OS/architecture and `GOTOOLCHAIN=local` to prevent
implicit toolchain downloads. This native compilation stage is the Go development
adaptation; Python StatReload restarts an interpreter and has no corresponding
native build artifact. No new dependency is added.

Each attempt allocates a private 0700 temporary output directory and streams
compiler diagnostics to its caller. Errors/cancellation clean the directory before
returning. Successful output must be a regular Go executable, checked with standard
library buildinfo, with owner-only executable mode and a retained SHA-256.
Artifact.Executable rechecks regular-file shape, execute permission and bytes;
Artifact.Close releases only its own temporary directory and is idempotent.
The consumer must stop/join its running child before closing its artifact.
Builder diagnostics are opaque to fmt so retained environment credentials are not
exposed by incidental formatting. Compiler diagnostics remain caller-owned.

The build command owns a distinct process session/group on macOS/Linux. Caller
cancellation kills it; command completion also removes lingering helpers. A
bounded WaitDelay handles inherited diagnostic pipes. Other platforms retain the
existing direct-process cancellation fallback; no descendant-group guarantee is
claimed there. Actual native tests compile syntax-invalid source, prove failed
output cleanup, repair and run two changed binaries, reject library archives,
refuse modified/closed artifacts and verify detached private configuration.
An explicit test toolchain spawns a real helper holding pipes; cancellation joins
the command, removes output and leaves no running helper on macOS/Linux.

This is an explicit SDK component. It does not yet own a server child or listener;
CLI reload remains refused until the supervisor and HTTP process proof are wired.
README review/boundary and architecture metadata identify the separate owners.
Python sources, contract exports and existing guard/scanner anchors are unchanged.

Validation: narrow `go test -count=1 -timeout=120s ./reload`, full
`go test -p=1 -count=1 -timeout=300s ./...`, full
`go test -race -p=1 -count=1 -timeout=300s ./...` and `go vet ./...` all passed
from `go/`; `git diff --check` passed. Full race linked native SQLite consumers
with existing Darwin CGO LC_DYSYMTAB warnings; affected binaries exited zero.
Architecture delivery passed 9/9 with zero errors/warnings, and both exact
specification/artifact SHA-256 and byte counts were verified. Correction rounds:
0. Visual review was not repeated after the prior browser denial. Python runtime,
exporter/fixtures, default tools and existing guard/scanner targets did not change;
Python/export/invariant/scanner/mutation/coverage checks were not rerun.
No push. The next implementation remains the owned supervisor/listener/CLI path.

## Native reload supervisor and retained listener — 2026-10-10

`reload.NewSupervisor` captures typed Builder/Watcher collaborators, child working
directory, arguments and environment. On macOS/Linux its single-use Run owns an
admitted TCP listener. The parent retains that socket through replacement, build
failure and child exit. The public child protocol duplicates inherited descriptor
3 into a TCP listener; actual-address authentication remains the caller's duty.
This is an explicit SDK; config still refuses MINILOOP_RELOAD. CLI selection and
actual miniloop HTTP proof remain required. No dependency is introduced.

An edit stops/joins the old child before compiling/replacing it. SIGTERM gets a
finite configurable grace (default 10 seconds), followed by forced process-group
termination. Wait joins children and cleans their private artifacts before the
parent closes its listener. Build/start errors and unexpected child exits emit
typed events and keep observing for a correction, without a respawn loop.
Callback panics trigger owned cleanup and an error; callbacks/writers must return
promptly. Cancellation after a successful build also closes the returned artifact.
Native builds explicitly select executable mode. Priming before each build keeps
edits during compilation visible. These bounded/native details are documented Go
adaptations; Source StatReload restarts an interpreter and joins without this
native build stage. Other platforms refuse supervisor admission before ownership.

Actual HTTP child tests import the public inherited-listener helper, replace
three generations at one address, join old children, retain the socket through
syntax failure, recover after repair, and release socket/artifacts on shutdown.
Additional real children ignore SIGTERM to prove forced cleanup; callback-panic
cleanup is verified. Detached environment and single-use rejection are checked.
README canonical topology reviewed: this unactivated library has no new runtime
edge/default; boundary/review baseline and aggregate interactive card updated.

Validation: narrow `go test -count=1 -timeout=120s ./reload`, full
`go test -p=1 -count=1 -timeout=300s ./...`, full
`go test -race -p=1 -count=1 -timeout=300s -coverprofile=/tmp/mini-loop-supervisor.cover -covermode=atomic ./...`
and `go vet ./...` passed from go/. `git diff --check` and README outline passed.
Existing Darwin CGO LC_DYSYMTAB warnings did not prevent affected tests passing.
Architecture delivery passed 9/9 with zero errors/warnings; exact spec/artifact
hashes and byte counts verified, correction rounds 0. Visual review was not
repeated after prior browser denial. Python runtime/exports/guard/scanner targets
did not change; Python/export/invariant/scanner/mutation gates were not rerun.
The race profile uses package-local coverage; comparable cross-package coverage
is a separate measurement. No push; full G0–G7 remains incomplete.

## Coverage refresh after native reload SDK — 2026-10-10

Measured implementation/test baseline: `cdd57c7`. Fresh full Go tests passed with
`go test -p=1 -count=1 -timeout=300s -coverpkg=./... -coverprofile=/tmp/mini-loop-reload-current.cover ./...`
from go/ on Darwin arm64. Source-location blocks are merged by maximum hit count,
counting each statement once across test packages, matching the previous method.

| Scope | Covered / total statements | Coverage |
| --- | ---: | ---: |
| All Go packages | 28,378 / 31,559 | 89.92% |
| reload | 284 / 357 | 79.55% |
| agent | 8,997 / 10,003 | 89.94% |
| sqlitestate | 521 / 625 | 83.36% |
| config | 349 / 401 | 87.03% |
| launcher | 212 / 263 | 80.61% |
| httpapi | 1,616 / 1,806 | 89.48% |
| internal/commandenv | 0 / 28 | 0.00% |
| cmd/miniloop | 45 / 69 | 65.22% |

Compared with the prior 28,088 / 31,202 snapshot (90.02%), this is 28,378 /
31,559 (89.92%): 290 more covered and 357 more total statements. The denominator
now includes the explicit watcher/build/supervisor SDK; the aggregate declines
0.10 percentage points. Statement coverage does not establish full feature parity.

Test subprocess counters are not merged. In particular, commandenv and the child
InheritedListener protocol have passing real-process tests whose executions are
not counted here. Python coverage and mutation checks were not refreshed. The
implementation's full ordinary/race/vet and architecture gates are recorded in
the preceding delivery entry. This follow-up changes only measurement docs and
README review baseline; git diff --check passed. Temporary profiles are untracked.
CLI reload and full G0–G7 acceptance remain incomplete. No push.

## Native CLI development reload and inherited authority — 2026-10-10

Implementation baseline: `9b59758` plus this slice. Actual Python __main__.main
(source SHA-256 `7e6730669a2307c56a1a722ebb440cc5c968c3cb027050846692b4f93b759257`)
was called with uvicorn.run captured: empty reload selects false; `0`, `false`,
`1` and one space select true. Only server dispatch was captured; Source StatReload
observation/process ownership evidence is recorded in the preceding reload slices.
Go ServerSettings now has a named Reload bool using the same nonempty selection.
No dependency or toolchain version changes.

cmd/miniloop selects the existing observer/build/supervisor on macOS/Linux. It
watches cwd .go entries and builds ./cmd/miniloop from cwd/ancestor go.mod or
operator --reload-module. This module discovery/native compilation is a documented
Go development adaptation; embedded compile-time source paths are not used.
Dump reports selection without module discovery/build/runtime allocation.
Parent config/auth admission precedes listening; it owns no manager/session.
Parent socket stays bound through native child replacement and failed compilation.

runMain captures an inherited TCP descriptor before dotenv, retains the role in a
typed local listener, and removes marker values from process/prepared environment.
Refreshed dotenv cannot recursively select another supervisor or make the child
bind changed HOST/PORT. Child config/auth validation uses the actual inherited
address before manager/workspace construction. App lifetime and process-local
sessions belong to the child; replacements reload dotenv and reset sessions.
No automatic durable restoration or authority rearming is added. Compiler and
child join/artifact cleanup retain the prior SDK guarantees and finite stop grace.

Actual miniloop process tests compile an isolated source/embed copy, change a real
Go init definition and observe new model/PID at the same port. Bash reads refreshed
dotenv values and excludes the private listener marker. Previous sessions return
404 after replacement. Invalid source stops the old child, reports build failure
and retains the port; repaired source restores real HTTP even when dotenv clears
reload and changes PORT to zero. SIGTERM joins every child and leaves the private
temporary-build directory empty, with the listener port available again.
A real inherited public socket with loopback HOST/dotenv role overwrite is refused
before workspace allocation. Pure dump and module/environment ownership tests pass.

README canonical Mermaid adds the optional reload parent and descriptor handoff
back to child startup. Boundary prose, extension/hardening notes and interactive
aggregate startup card identify distinct parent/child owners and default-off
selection. Remaining full-runtime acceptance and diagnostics decision stay open.

Validation: narrow `go test -count=1 -timeout=180s ./config ./cmd/miniloop`,
full `go test -p=1 -count=1 -timeout=300s ./...`, full
`go test -race -p=1 -count=1 -timeout=300s ./...` and `go vet ./...` passed
from go/. `.venv/bin/python -u python/tools/verify_guards.py` completed with all
378 existing Python mutations caught; it is not Go observer/authority mutation
coverage. Existing Darwin CGO LC_DYSYMTAB link warnings did not prevent passing
Go binaries. `git diff --check` and README outline passed. Architecture delivery
passed 9/9 with zero errors/warnings, exact spec/artifact hashes/bytes verified.
Correction rounds 0; visual review not repeated after prior browser denial.
Full Python suite, export/invariant/scanner checks and coverage were not rerun:
Python runtime modules, contract exports/default tool types and scanner targets
did not change. No push; default WatchFiles profile and full G0–G7 remain open.

### Default observer reconciliation discovered during CLI delivery

A live `uvicorn.supervisors.ChangeReload` inspection reports Uvicorn 0.49.0
selecting `uvicorn.supervisors.watchfilesreload.WatchFilesReload`, with watchfiles
1.2.0 installed. The preceding explicit StatReload recipes are valid for that
fallback component; they do not establish installed default observer parity.
The current CLI process/build/listener tests likewise modify existing sources
with increasing mtimes. This discovery changes the remaining implementation queue.

Next slices:

1. Capture actual WatchFiles default filtering and add/remove/modify/rename
   observations, including same/lower mtime, hidden/temporary names and cancelled
   observation; compare the default selected backend, not only fallback methods.
2. Add a named observer profile and owned native filesystem-event lifecycle using
   platform APIs, preserving the explicit StatReload SDK profile. Avoid treating
   a polling snapshot's net changes as proof of transient native-event fidelity.
3. Select the matching profile in CLI and exercise actual miniloop with source
   creation/removal and metadata/content changes. Reconcile backend environment,
   permissions/root/symlink and platform boundaries in G7; keep unresolved cases
   explicit. Existing child/socket/build ownership stays the delivered foundation.

Full G0–G7 and default reload parity remain incomplete; this is not a replacement
of the original objective with the fallback profile.

## Installed WatchFiles contracts and typed default filter SDK — 2026-10-10

Baseline: `7607ab3` plus this slice. Export 201 adds python-watchfiles.json,
pinning installed Uvicorn 0.49.0, watchfiles 1.2.0 and Darwin. The selected Source
backend is WatchFilesReload; source hashes are
`bbe32e626d0bb1930debbd2efd3b525a2301500db3e23d6c21edced80ebb129e`
(Uvicorn observer/filter) and
`520b73b467a13dc31f2c21d86d0da68112654d611511b3de82d18534b43c339b`
(watchfiles watch/environment helpers). No dependency is added: existing Source
requirements already select uvicorn[standard], and Go modules remain unchanged.

The collector calls actual FileFilter and WatchFilesReload.should_restart using
real temporary filesystem effects and the original native RustNotify subscription.
It verifies the Recommended FsEventWatcher backend (not polling), recursion and
ignore_permission_denied=true; captured FSEvents settings are flags 18, latency
0.0 and since-now. Registration drains earlier writes to a real timeout before
arming. Only rust_timeout waiting is reduced to 100ms; default debounce/step and
Uvicorn filtering/dispatch remain unchanged. Workers and native contexts close
before temporary-root teardown. No server/child/socket/model starts.

Six actual filter configurations produce 84 decisions: default, disabled Python
include, added text include, allowed hidden/backup patterns, excluded directory
and exact-include shortcut. The project default excludes hidden/backup basenames
but retains sources under .git/.venv/__pycache__ parents; matching-suffix
directories and removed paths need no stat. Exact literal inclusion can bypass
an excluded parent when the default include is removed; this is filter behavior,
not root or filesystem authority. Go implements only the project default policy.

Sixteen real native cases cover add/remove, same/lower mtime content edits,
identical writes/touches, rename in/out/both source names, hidden/backup/text
filtering, hidden-parent source, a followed symlink's non-source target, suffix
directory creation and transient create/delete. Matching add/remove/metadata
events restart despite StatReload baseline/mtime limitations. The symlink target
write reports no source restart: native event names differ from resolved StatReload
paths. A separate live subscription is cancelled/joined and reports StopIteration
with its native context closed. Nine force-polling and eight delay-environment
rows record actual helper conversion/explicit precedence; these are not Go
environment/backend implementations. Native evidence is this pinned Darwin profile.

Go reload.EventFilter has private immutable extension state and named Language
selection, an inert zero value and refusal of unknown languages. Matches is
lexical and grants no root/filesystem/execute authority. All 14 actual default
Source decisions replay for Python and Go suffixes (28 checks). Non-default
Uvicorn include/exclude configurations are captured but not exposed by this SDK
or selected by mini_loop.__main__. No native Go event source is implemented in
this slice; CLI still uses StatReload. The next work is owned native subscriptions,
backend/environment lifecycle and CLI event-profile selection. Full G0–G7 stays open.

Validation: narrow `go test -count=1 -timeout=120s ./reload`, full
`go test -p=1 -count=1 -timeout=300s ./...`, full
`go test -race -p=1 -count=1 -timeout=300s ./...` and `go vet ./...` passed
from go/. `.venv/bin/python python/tools/export_go_contracts.py --check` freshly
re-executed all recipes and passed 201 files; `.venv/bin/python
python/tools/verify_scans.py` passed all 19 scanner guards. Existing Darwin CGO
LC_DYSYMTAB warnings did not prevent passing Go tests. `git diff --check` and
README outline passed. Architecture delivery passed 9/9 with zero errors/warnings
and exact spec/artifact hashes/bytes verified. Correction rounds 0; visual review
not repeated after prior browser denial. Python package runtime, existing mutation
anchors and tool types did not change; full Python/invariant/mutation/coverage
checks were not rerun. No push; owned native Go events and full G0–G7 remain open.

## Owned macOS native-event SDK — 2026-10-10

Baseline: `32cf034` plus this slice. `reload.NewNativeEvents` owns an actual
macOS FSEvents subscription through existing CGO/system CoreServices and
CoreFoundation frameworks; Go dependencies do not change. The captured Source
configuration is flags 18 (FileEvents + NoDefer), latency 0 and since-now.
Typed EventBatch paths are lexical, unique and sorted; default filename filtering
retains removed names, suffix directories and hidden-parent sources. Next coalesces
50ms quiet/1600ms maximum. Cancellation keeps unread events available, while Close
joins the locked OS thread after stopping/invalidating/releasing the stream and
removing its typed registry entry. Only integer keys cross C ownership.

All 16 actual python-watchfiles.json native recipes replay with real Go OS events,
including add/delete/transient, equal/lower mtime, identical writes/touches, rename
in/out/both names, hidden/backup/text exclusions, hidden-parent sources, suffix
directories and a non-source symlink target. Separate checks verify cancelled-batch
retention, idempotent Close/blocked-reader release, no registry leak and explicit
kernel-loss faults. The queue retains at most 4096 unique matching paths; kernel
loss/capacity errors require explicit Close/recovery, not synthetic polling proof.
Non-Darwin/CGO construction fails explicitly. WATCHFILES environment/forced-polling
selection, Linux native events and CLI binding remain pending; CLI still uses
StatReload. Full G0–G7 acceptance remains open.

Validation: narrow `go test -run TestNative -count=1 -timeout=120s ./reload`,
full `go test -p=1 -count=1 -timeout=300s -coverpkg=./... -coverprofile=/tmp/native-events-coverage.out ./...`,
full `go test -race -p=1 -count=1 -timeout=300s ./...` and `go vet ./...`
passed from go/. Architecture delivery passed 9/9 with zero errors/warnings;
spec/artifact hashes and byte counts match. README outline and git diff checks
passed. Correction rounds 0; visual review not repeated after prior browser denial.
System SDK emits an FSEventStreamScheduleWithRunLoop deprecation warning, and
existing Darwin CGO linker LC_DYSYMTAB warnings remain nonfatal. Full Python,
recipe freshness, invariant/scanner/mutation checks were not rerun: Python runtime,
Source inventory, default-tool types and mutation targets do not change. No push.

Fresh coverage at `32cf034` plus this implementation: **89.75%**
(28501/31755 Go statements), merged from
`go test -p=1 -count=1 -timeout=300s -coverpkg=./... -coverprofile=/tmp/native-events-coverage.out ./...`.
Merge counts each source block once and takes maximum hits across package tests.
reload is 81.91% (385/470); agent 89.88%, sqlitestate 83.36%, config 87.00%,
launcher 80.61%, httpapi 89.48%, cmd/miniloop 46.41% and commandenv 0.00%.
CLI child-process counters are not merged, so this is collected statement coverage,
not CLI behavior acceptance; real subprocess tests separately verify startup/reload.
C FSEvents callback code is outside Go's statement denominator. The previous
cdd57c7 snapshot was 89.92% (28378/31559); subsequent CLI/native-event code changes
the denominator, so the old result must not be presented as current.

## Forced PollWatcher and environment Source contracts — 2026-10-10

Baseline: `4acefcc` plus this Source evidence slice. python-watchfiles.json now
adds 20 real forced-PollWatcher cases with original RustNotify and actual Uvicorn
filter/dispatch. Native rows remain unchanged. Stable native repr verifies recursive
symlink following, no content hasher and signed whole-second path mtime, including
fractional/pre-epoch inputs. Polling observes add/delete/rename and increasing
whole seconds; same/lower/fractional mtime or identical writes/touches yield no
batch. A followed file link retains its lexical name, and a directory alias adds
both alias and target paths. This is polling evidence, not transient native proof.

Actual boundary subscriptions report a WatchfilesRustInternalError for directory
cycles and three consecutive deletion batches for dangling source links and a
removed suffix-matching root. Context entry/exit is verified; raw paths are filtered
through actual FileFilter. Native/polling contexts and collecting threads close.
Environment capture adds Unicode dotted-I behavior, six actual uname combinations
(only Linux + microsoft-standard auto-forces polling), five debug truth rows,
Unicode decimals, maximum u64/overflow and CPython's 4300-digit conversion limit.
Two actual native RustNotify constructions verify the u64 millisecond boundary;
nonempty `0`/`false` debug values remain true. No dependency or CLI default changes.

Validation: fresh export followed by `.venv/bin/python
python/tools/export_go_contracts.py --check` passed all 201 files;
`.venv/bin/python python/tools/verify_scans.py` passed all 19 scanner guards.
Native rows were compared with HEAD and remain identical. Narrow native/polling
Go replay passed; complete Go runtime gates belong to the implementation slice.
Existing exporter dependency deprecation/FutureWarnings remain recorded.
README outline and diff checks passed; architecture regeneration passed 9/9,
zero errors/warnings, with exact hashes/bytes verified. Correction rounds 0;
visual review not repeated after prior browser denial. Python package runtime,
mutation anchors and default-tool types do not change; full Python/invariant/
mutation checks were not rerun. No push.
Go profile/owned polling implementation remains a separate pending delivery.

## Typed observer selection and owned polling SDK — 2026-10-10

Baseline: `c48e38a` plus this slice. ObserverBackend is closed; ObserverProfile
privately stores backend, uint64 millisecond delay and debug selection. Pure
SelectObserverProfile uses caller-captured environment/host and explicit force
precedence, pinned Unicode lowercase/decimal semantics, the 4300-digit conversion
bound, Source uint64 constructor admission and Linux microsoft-standard auto policy.
Nil environment is empty; no process/host probe or environment reference is retained.

NewObserver(ctx, config) returns the narrow Next/Close interface and binds a
captured native macOS/CGO or portable forced-polling profile. Existing NativeEvents
uses the shared typed reader; no stream/registry ownership changes. Polling owns
snapshot traversal and partitioned timer waits through Close. Construction context
cancels initial traversal; successful subscriptions retain explicit Close ownership.
The default native/polling filename filter is lexical. Directory/file links retain
aliases; signed whole-second mtime/no content hash preserve Source comparisons.
Adds/removes/renames, pre-epoch/increasing edits and hidden parents replay; lower,
equal/fractional edits/identical writes/touches remain quiet. Dangling entries and
missing roots repeatedly emit deletion, matching the three actual Source batches.
Cycle admission fails before Go worker startup; Source faults during initial watch.

All 20 Source polling operations, missing-path/cycle boundaries and expanded
force/delay/host/debug/constructor rows replay in Go. Existing 16 actual native
scenarios still pass. Source raw-event set semantics are retained: duplicates do
not extend quiet waits; polling records named add/modify/delete variants. Matching
path capacity is 4096; raw fingerprint tracking is separately bounded, after which
the hard batch cap applies. Cancellation retains unread batches, Close releases
blocked readers, and maximum uint64 waits join without nanosecond overflow.
Selected debug writes include raw excluded names, with native flags or zero for
polling; Go diagnostic text is an explicit adaptation of Rust debug internals.
Custom diagnostic writers must return promptly. No dependency is added.

This is SDK delivery: default Linux native events, CLI observer selection/ownership
and compiler-temporary self-trigger protection remain implementation work. Current
CLI still uses StatReload; Source parity/full G0–G7 acceptance is not claimed.
Validation: narrow native/polling/profile/duplicate replay passed. The real
supervisor recovery test initially timed out waiting for SourceChanged: publishing
content before a synthetic past mtime allowed an intermediate wall-clock value to
become the StatReload baseline. `c48e38a` publishes content/stamp by atomic rename;
the supervisor/duplicate checks passed three consecutive runs. Production StatReload
logic is unchanged. After that correction, fresh full
`go test -p=1 -count=1 -timeout=300s ./...`,
`go test -race -p=1 -count=1 -timeout=300s ./...` and `go vet ./...` all passed.
`CGO_ENABLED=0 go test -run 'TestObserver|TestPolling|TestRepeated' -count=1 -timeout=120s ./reload`
passed. Fresh Source export/check passed 201 files, scanner verification all 19,
and invariant verification all 77 modules. New Go domain/service structs contain
no any/interface{}/RawMessage; fixture JSON numbers stay in test boundary decoders.
README outline/diff checks passed; architecture delivery passed 9/9, zero errors/
warnings and exact hashes/bytes verified. Correction rounds 0; visual review not
repeated after prior browser denial. Existing Source deprecation/FutureWarnings,
FSEvents API deprecation and Darwin CGO linker warnings are recorded and nonfatal.
Full Python, mutation and coverage were not rerun; Python runtime/default tools/
mutation anchors are unchanged. The 4acefcc coverage snapshot is not current after
this implementation. No push.


## Go reload — owned compiler workspace (2026-10-10)

Baseline: `0c09a1e` plus this slice. BuildWorkspace allocates a private parent;
active build attempts, returned artifacts and bound observers lease its lifetime.
Close refuses active leases; failed/cancelled builds release only after compiler
join and attempt-directory cleanup. Builder overrides GOTMPDIR/TMPDIR only for
its compiler. Server/tool environment is unchanged.

NewObserver accepts the allocated workspace capability. Native callbacks exclude
its lexical subtree before filtering, batching and capacity accounting; kernel-loss
flags remain faults. Polling skips this subtree and directory aliases into it.
This is a Go compilation adaptation, not a new Python WatchFiles exclusion rule.
Ordinary hidden-parent source paths outside the allocated root remain observable.
The CLI still uses StatReload; event-profile activation and Linux native observation
remain pending. No new dependency or runtime default is selected.

Evidence: real CGO build inside a watched root retains actual generated .cgo1.go
files with go build -work, verifies both compiler environment overrides, runs the
resulting executable and observes no source batch through artifact removal.
Portable tests verify failed admission/build lease release, active artifact/observer
close refusal, outside-source observation, queue capacity and retained kernel-loss
faults. Compiler cancellation verifies active lease refusal, process-group join,
attempt cleanup and final workspace close.

Validation: focused real-CGO/workspace and compiler-cancellation tests pass;
full Go ordinary suite (`-p=1 -count=1 -timeout=300s ./...`) and go vet pass.
CGO_ENABLED=0 targeted workspace/observer/polling suite passes. Architecture
regeneration passes 9/9 with zero errors/warnings and matching hashes/bytes;
README outline and git diff --check pass. Fresh visual review remains unavailable
following the earlier browser access denial. Full Python/mutation gates were not
rerun for this Go-only slice; no merge or push is requested.


## Go coverage — owned workspace snapshot (2026-10-10)

Code baseline: `2d2b608` (owned compiler workspace implementation).
Cross-package merged coverage is **89.73% (28712/31998 statements)**;
reload **83.59% (596/713)**, agent 89.88%, sqlitestate 83.36%, config
87.00%, launcher 80.61%, httpapi 89.65%, cmd/miniloop 46.41%,
internal/commandenv 0.00%. Generated profiles are temporary, not tracked.

Method: full `go test -race -p=1 -count=1 -timeout=300s -coverpkg=./...
-coverprofile=/tmp/mini-loop-workspace.cover ./...`, then the complete SQLite
package with the same coverage scope without race in
`/tmp/mini-loop-workspace-sqlite.cover`; merge identical location/statement-count
blocks using maximum hit counts. The combined race+coverage run failed only
TestDecisionNativeSQLiteReopenMatchesSource/maximum at its internal 30-second
context deadline; this is recorded as a failed run. Its full-package coverage
rerun passed in 11.926 seconds. The ordinary full suite passed separately.
This merged profile supplements the failed contribution with the successful
complete-package rerun; it is not a claim that the combined command passed.
CLI subprocess counters and native C implementation are outside this denominator.

Independent full `go test -race -p=1 -count=1 -timeout=300s ./...` passes,
including reload (31.608s) and sqlitestate (30.170s). This closes the separate race
gate without claiming that the combined race+coverage command passed.
The existing macOS FSEvents deprecation and instrumented CGO linker warnings
remain nonfatal. No Python runtime/dependency/default change, merge or push.


## Go reload — supervisor event subscription binding (2026-10-10)

Baseline: `04bf277` plus this slice. SupervisorConfig selects exactly one Watcher
or narrow Observer. The existing StatReload contract remains a separate choice.
Run owns an event subscription only after listener admission; it starts a typed
synchronous read worker before the initial build, retaining observation through
builds without an unbounded result queue. Empty filtered batches do not rebuild.
Backend errors terminate with child/artifact/subscription cleanup. SourceChanged
retains Path and adds the detached full Paths batch. The final listener remains
owned until the child and observer reader are joined.

Real HTTP child tests now run through StatReload, forced polling and macOS native
subscriptions, including source replacement, failed compilation/correction,
retained port, captured child environment, joined PIDs and released BuildWorkspace
leases. A typed observer fault checks empty-batch inertness, exact backend error,
forced-stop/artifact/socket cleanup and Close. Ambiguous watcher/observer selection
is refused; rejected listener admission leaves the caller subscription open.
No dependency or executable default changes. CLI still uses StatReload; native
Linux events and CLI observer-profile binding remain required next work.

Validation: focused real native/polling/StatReload supervisor recovery and fault
checks pass. Full ordinary Go suite (`-p=1 -count=1 -timeout=300s ./...`)
and go vet pass. CGO_ENABLED=0 polling/admission/fault supervisor checks pass.
Architecture delivery passes 9/9 with zero errors/warnings and exact hash/byte
checks; README outline and git diff --check pass. No new visual inspection after
the previous browser denial. Python runtime/scanner/invariant/mutation targets
are unchanged; their suites were not rerun, and no merge/push is requested.
Coverage was not refreshed: the 89.73% record is the preceding 2d2b608 snapshot,
not a claim about this code. No Go domain/service any/interface{}/RawMessage added.

Full `go test -race -p=1 -count=1 -timeout=300s ./...` passes, including
reload 43.143s and sqlitestate 28.770s. Existing FSEvents deprecation and Darwin
CGO linker warnings are nonfatal. No push.


## Go reload — captured Linux Source native contracts (2026-10-10)

Runtime baseline: `48aa064`. The separate Linux fixture captures actual Uvicorn
0.49.0 / watchfiles 1.2.0 INotifyWatcher with recursive=true,
ignore_permission_denied=true and force_polling=false. Eighteen recipes include
the existing sixteen operations plus forward/reverse directory symlink names.
Actual native directory and file link loops enter/close their context and time out;
write-open/close without writing also times out. These are distinct from forced
PollWatcher loop errors. Source closure and constructor arguments are retained.

The dedicated Linux exporter refuses other platforms/versions/backends. It runs
against an actual Linux aarch64 Docker kernel, not mocked platform helpers.
Official watchfiles v1.2.0 Cargo.lock pins notify 8.0.0; its inotify implementation
uses filesystem-order WalkDir traversal and overwrites the lexical descriptor
projection on each registration. Initial descendant traversal errors are skipped.
Sources: https://raw.githubusercontent.com/samuelcolvin/watchfiles/v1.2.0/Cargo.lock
and https://raw.githubusercontent.com/notify-rs/notify/notify-8.0.0/notify/src/inotify.rs.

Reproduce from the repository tools directory in Linux with these pinned installed
packages: `python export_linux_watchfiles_contracts.py --output <fixture>`.
Container Go toolchain: golang:1.23.3-bookworm, image digest
sha256:59b8183301af6dc358c9258d7b2ab0ee1a9363618552334fb3b160d454cbda72.
Python image used: python:3.11-slim, local image ID
sha256:1eee6fcc4d866c86dd8e32a4a1a583767ddac0d640b0fe571b2c22d233b60eee.
The Source evidence precedes the Go backend delivery. CLI remains StatReload.

Root movement is also captured: after a watched root is renamed, the next child
write retains the old lexical root projection. Raw batches are [<root>] then
[a.py], and context closure is verified. The final Linux exporter recapture
matches the checked-in fixture exactly. Main Source export --check passes 201
files; invariant verification passes 77 and scanner verification passes 19.
No new project dependency or default CLI selection is introduced by this evidence.


## Go reload — owned Linux native backend (2026-10-10)

Baseline: `3b523aa` plus this implementation. Native events now support Linux
with Go/syscall APIs, no new dependency and no CGO requirement. One worker owns
its nonblocking CLOEXEC descriptor, watch registrations and bounded record reads.
Recursive filesystem-order registration follows links, skips native directory/file
cycles and retains notify 8's last lexical alias projection. Dynamic directory
creation/movement updates the subtree registrations. Access-only opens do not
produce source batches; root moves retain the old lexical projection.

Linux native event identity is distinct from Darwin flags, so IN_MODIFY is not
misclassified as dropped data. Truncated headers/names are refused; kernel queue
overflow and unmount return explicit recovery faults. Existing batch capacities,
cancellation retention and allocated-workspace exclusions are preserved. Public
NewNativeEvents remains compatible; NewObserver passes its construction context
through Linux traversal. Successful subscriptions remain caller-owned. Initial
cancellation closes the fd and partial watches; Close joins the worker.

Evidence: actual Linux aarch64 Go 1.23.3 replays all 18 Source native recipes,
link-loop/noop-open/root-move boundaries, dynamic directory replacement/move-out,
blocked Next shutdown, exact /proc/self/fd release, and cancellation after observing
an opened constructor descriptor. Real native HTTP children rebuild, repair failed
compilation, retain the port and release workspace leases. CGO=0 full reload tests
and CGO=1 race/vet reload tests pass. This is SDK delivery; CLI is still StatReload.

Initial macOS full test/race attempts exposed delayed fixture setup rename events:
FSEvents flags 67584 on main.go and 105728 on .reload-source-* were emitted before
its first child start and caused replacement. The test now drains only completed
fixture initialization before supervisor ownership transfer, matching the existing
native Source-replay harness. Actual edits still begin after first HTTP readiness;
production Darwin behavior is unchanged. Native replay/supervisor tests then pass
three consecutive runs, followed by fresh full host ordinary/race/vet success.

Gates: full host go test/go test -race with -p=1 -count=1 -timeout=300s; go vet;
Linux reload ordinary with CGO=0 and race/vet with CGO=1; main Source export --check
201; Linux Source final recapture exact match; verify_scans 19; verify_invariants 77;
git diff --check. The final root-move correction touched only Linux-only source/tests
and its dedicated Source exporter/fixture, followed by fresh Linux full reload
race/vet. Host-compiled Go files remained unchanged from the passing full gates.
Python runtime/mutation targets are unchanged; full Python/mutation gates were not
rerun. No merge/push. Coverage was not refreshed; 89.73% is the older 2d2b608 snapshot.

Architecture regeneration passes 9/9, zero errors/warnings, with exact hash/byte
checks. README outline and diff checks pass. Visual review is not repeated after
the previous browser access denial. The explicit unmount kernel-record refusal
check also passes. Source/scanner/invariant gates remain 201/19/77 respectively.


## 2026-10-10 CLI captured observer and workspace activation

Baseline: `a387bcf` plus this implementation. The reload parent now selects an
owned Observer from prepared WATCHFILES environment and a captured host. Linux
uname release drives WSL automatic polling; ordinary Linux and macOS/CGO select
native events. Explicit forced polling supports macOS without CGO. Inspection
still performs no observer construction or host probe. StatReload remains an
explicit SDK choice. No dependency or toolchain change is introduced.

Builder and Observer share a private BuildWorkspace under captured TMPDIR.
Compiler-only temporary overrides remain inside that allocation. Child/tool
environments retain prepared values with the private role marker removed.
Supervisor admission owns observer Close; CLI defers cover earlier failures and
close the workspace only after artifacts and observer leases are released.

Actual CLI tests exercise native and forced polling (`WATCHFILES_FORCE_POLLING=0`),
retained TCP, source replacement, dotenv refresh, failed-build repair, process-local
session reset, joined children and empty compiler workspace after shutdown. The
compiler temporary parent is inside the watched module. Fixture source publication
stamps a hidden temporary file before atomic rename, avoiding half-written sources
or intermediate polling timestamps. Actual Linux aarch64 Go 1.23.3 CLI tests and
macOS CGO-disabled forced-polling/dump tests pass.

Gates: fresh full host `go test -p=1 -count=1 -timeout=300s ./...`,
`go test -race -p=1 -count=1 -timeout=300s ./...` and `go vet ./...` pass.
Actual Linux aarch64 CLI native/polling/dump tests pass; macOS CGO=0 forced
polling/dump tests pass. Source export --check reports 201 current files.
README architecture outline and regenerated map pass 9/9, with exact byte/hash
verification. git diff --check passes. Existing FSEvents deprecation and macOS
linker warnings remain. Visual review was not repeated after prior browser denial.
No Python module/scanner/guard target changed; corresponding full Python,
invariant/scanner/mutation checks were not rerun. No merge or push.


## 2026-10-10 coverage refresh after CLI observer activation

Measured code baseline: `ba1c43f` on Darwin arm64. Fresh complete ordinary
`go test -p=1 -count=1 -timeout=300s -coverpkg=./...
-coverprofile=/tmp/mini-loop-cli-current.cover ./...` passes. Each source-location
block is counted once, retaining maximum hits across package tests.

Merged statement coverage: **89.72% (28758/32054)**. Package coverage:
reload **84.44% (635/752)**; agent **89.91% (8994/10003)**;
sqlitestate **83.36% (521/625)**; config **87.00% (348/400)**;
httpapi **89.48% (1616/1806)**; launcher **80.61% (212/263)**;
cmd/miniloop **41.76% (71/170)**; internal/commandenv **0% (0/28)**.

Previous 2d2b608 snapshot was 89.73% (28712/31998): covered statements
increased by 46 and total statements by 56. This reports statement coverage,
not branch coverage or full migration completion. CLI child-process counters
and native C are not merged. Actual native/polling command, HTTP and cleanup
behavior is verified separately by process tests. Linux-only inotify code is
not part of this Darwin denominator; its actual Linux tests pass separately.

Fresh full ordinary/race/vet gates passed independently of coverage. No
SQLite fallback profile was necessary in this refresh. No full Python,
mutation gate, merge or push was run. This update changes documentation only;
git diff --check and cached diff checks pass.


## 2026-10-10 package-level Go type audit

Baseline: `39dbca4` plus this developer command. `go run ./tools/typeaudit
-root .` from go/ parses every production Go file, including inactive platform
files, excluding tests/vendor/.git. It examines package-level named type bodies,
including aliases, nested maps/slices and interface/function signatures. Direct
any, empty interfaces and encoding/json.RawMessage (including renamed or dot
imports) are findings. Generic constraints and function-local ingress DTOs are
excluded. Syntax/read failures fail the command; an empty source tree is refused.

Current result: 399 production files, 1291 package-level declarations, zero
findings. Source inspection of provider.decodeReply/decodeBlock confirms local
raw JSON is normalized into validated protocol.ModelReply/Block/ToolInput
variants before returning to services. This is a sampled data-flow observation,
not a whole-program proof. External named types, concrete generic instantiations,
anonymous escaping values and authority still require review. The static check
does not establish final G0/G7 completion.

Command tests exercise map/slice any, empty interfaces, RawMessage aliases and
dot imports, inactive Windows source, allowed generic constraints/local ingress,
test exclusion and parse failure. Narrow command race tests pass. Python
verify_scans still passes all 19 scanner anchors; it does not validate this Go
scanner, which has its own tests. Full Go/vet results are recorded below.

The current plan/index now removes the stale StatReload/CLI-pending queue and
records ba1c43f native/polling/workspace delivery. The 201 Source exports and
89.72% application coverage retain their measured baselines. The new audit
command is not part of the previous coverage denominator. Default full-feature
CLI syntax-parser selection and final performance/scope/release audits remain
open. No dependency, runtime topology or default behavior changes. README
architecture baseline reviewed; canonical Mermaid/spec remain unchanged.

Validation completed: full `go test -p=1 -count=1 -timeout=300s ./...` and
`go vet ./...` pass; `go test -race ./tools/typeaudit` passes; the actual
audit reports 399/1291/0. verify_scans passes 19 anchors. README heading outline,
git diff --check and cached diff check pass. Existing macOS FSEvents compiler
warning remains. Full application race was last passed at ba1c43f; it was not
rerun because only developer audit tooling changed. Full Python/invariants,
guard mutation, Source export and coverage were not rerun; their runtime/contract
targets remain unchanged. No merge/push or migration-completion claim.


## 2026-10-10 cross-language fake HTTP performance instrument

Baseline: `723853e` plus this local developer command. The instrument starts
Python and prebuilt Go in isolated environments/workspaces with zero-delay fake
providers, alternates execution order and verifies identical final text plus
actual Bash file effects. It measures startup-to-health, persistent HTTP health,
create, turn and delete latency; output contains raw samples and sampled server
RSS. No provider credentials/features/reload settings are inherited. Both
servers join and release the port; Source Uvicorn's post-lifespan SIGTERM
re-raise is recognized only after requested termination and completed shutdown.

Four instrument tests and small/three-pair actual runs pass. Full Python:
2177 passed, 28 skipped, 24 subtests, 3 existing warnings (71.58s). Fresh
verify_invariants 77 and verify_scans 19 pass. Go CLI readonly build succeeds
with the existing FSEvents deprecation warning. No Go runtime changed; full Go
was last passed at 723853e, full application race at ba1c43f. Source exports
and coverage were not refreshed. A full 378-guard sweep was stopped after 81
caught mutations; it is incomplete, not a passing full mutation gate. No
survived/stale/ambiguous result was printed before interruption. Performance
results and reproduction evidence are delivered separately; this command does
not establish comprehensive G7 performance or migration acceptance. README
architecture baseline reviewed; no runtime map change. git diff --check passes.


## 2026-10-10 measured Python/Go default fake HTTP baseline

Instrument: `d05004c`; application code baseline: `723853e` (no Python runtime
or Go source changed between those commits). Raw host/build identity, binary
SHA-256, final texts and samples: [measured JSON](docs/benchmarks/go-port-fake-http-2026-10-10.json).
Darwin arm64 24.5.0, CPython 3.11.3 and prebuilt Go 1.23.3. Three fresh process
pairs alternate Python/Go order, each with two warmup turns, 100 health reads
and 20 fresh-session Bash turns. Each turn writes and verifies proof.txt,
returns identical Python/Go final text and deletes the session. All six servers
join and their ports rebind. RSS is sampled after workload, excluding descendants.

| Metric | Samples per runtime | Python median / p95 ms | Go median / p95 ms |
| --- | ---: | ---: | ---: |
| startup | 3 | 267.544 / 283.038 | 15.306 / 32.124 |
| health | 300 | 0.597 / 0.694 | 0.105 / 0.129 |
| create | 60 | 2.650 / 3.763 | 4.766 / 7.827 |
| turn | 60 | 9.356 / 10.292 | 7.971 / 9.382 |
| delete | 60 | 0.677 / 0.748 | 0.285 / 0.331 |

Post-workload median server RSS: Python 59136 KiB, Go 22016 KiB (three samples
each). Go creation is slower in this run; other measured medians are lower.
These are default CLI/HTTP observations on one host. Python Uvicorn access
logging remains enabled; Go has no per-request access log. Startup includes
10ms readiness polling and excludes Go compilation. p95 uses nearest rank;
individual requests are clustered within three processes, with no confidence
or significance claim. SSE, SQLite, concurrent turns, feature groups and real
providers are outside this workload. It establishes initial performance
evidence, not comprehensive G7 acceptance or model quality.

Reproduce from the repository root:

```sh
(cd go && go build -mod=readonly -o /tmp/miniloop-perf ./cmd/miniloop)
.venv/bin/python python/tools/go_port_performance.py --go-binary /tmp/miniloop-perf --output /tmp/go-port-performance.json
```

Artifact validation checks all six run/sample counts, per-pair exact final-text
identity and actual binary digest. Instrument tests/full Python/invariant/scanner
gates are recorded in the preceding delivery. Full mutation verification is
still incomplete (81/378 caught before intentional stop), and G7 default
parser/provider/public-entry/type scope audits remain open. Documentation/data
only: git diff --check and cached checks pass. No merge/push.


## 2026-10-10 non-vacuous mutation verification and inventory repair

Baseline: `73c14ad` plus this correction. The runner now requires a passing
unmodified selector, then pytest exit 1 with JUnit test-call failure and no
framework errors. Empty/skipped baselines, missing reports, collection/fixture
errors and timeouts are INVALID. Baselines cache by selector within one run;
mutants use fresh Python-tree copies. Bytecode writes are disabled to prevent
same-size/mtime replacements from reading unmodified cache. No new dependency.

Full inventory sweep exposed 376 valid captures and two invalid entries:
request-content-length-unchecked named a removed test; restore-skips-the-repair
removed an if before its surviving elif and failed during collection. Corrected
selector and assignment-only restore mutation each pass their baseline and fail
the target call. Other 376 metadata entries are unchanged. New tests compile all
Python mutants and collect all recorded selectors, plus actual pytest positive,
surviving, baseline-failure, syntax-error and missing-selector cases.

Gates: 29 targeted tests; fresh full Python 2194 passed, 28 skipped, 24 subtests,
3 existing warnings (73.60s); verify_scans 19. All 378 entries have composite
evidence (376 initial full sweep plus two corrected target reruns). The initial
full command exited 1; no fresh single full command exit-zero is claimed.
Source modules, Go runtime and contract exports are unchanged; Go gates,
Source export, coverage and invariants were not rerun. Last invariant check
is 77 at d05004c. README architecture baseline reviewed; map/spec unchanged.
Diff checks pass; no merge/push. Structured audit evidence follows separately.


## 2026-10-10 composite current Python guard evidence

Corrected instrument baseline: `dcd9f7c`. [Structured guard evidence](docs/validation/python-guards-2026-10-10.json)
records the complete initial sweep (exit 1: 376 caught, 2 INVALID) and two
passing corrected reruns. Inventory comparison with 73c14ad proves exactly
those two metadata entries changed. Their corrected captures plus the unchanged
376 names equal all 378 current names; the artifact records inventory/log hashes.
A fresh single full command returning zero was not run. This is composite
coverage, not Go mutation coverage. The new syntax/collection preflight and
2194-pass Python regression cover the corrected snapshot. Diff checks pass.

G4 inspection also confirms a remaining provider implementation gap: installed
anthropic 0.107.1 TextBlock accepts a known char_location citation, and Python
agent._content_payload preserves its nonempty citation via model_dump. An actual
offline constructor/projection probe passes. Go provider.decodeBlock currently
rejects every nonempty citations array, and the stream decoder rejects citation
deltas. Capture complete Source citation recipes and implement named citation
variants/masking/cloning/request forwarding before claiming that scope accepted.
No provider call, dependency or runtime change was made in this observation.

## 2026-10-10 pinned provider citation Source contracts

Baseline: `b16bf0a` plus this contract capture. The installed anthropic 0.107.1
SDK is exercised through offline HTTP and SSE MockTransport, with its streaming
accumulator source hash recorded in `go/testdata/python-provider-citations.json`.
Seventeen completed reply/forwarding cases cover all five citation variants,
nullable and empty metadata, negative and arbitrary-size indices, extra members,
unknown variants and malformed declared fields. Two streams capture initial
citations plus ordered citation deltas and the SDK's separate text/citation events.
SDK-normalized payloads are forwarded back through the actual request serializer.

The fixture preserves the SDK's permissive extension behavior. Native Go citation
types, provider decoding/stream accumulation, detached cloning, masking and request
forwarding remain pending; this commit establishes their Source contract only.
No dependency, runtime topology, authority, persistence or default changes.
README architecture map reviewed and visible baseline updated.

Validation: targeted capture test passes; full Python regression reports
2195 passed, 28 skipped and 24 subtests passed (3 existing warnings). Source export
check reports all 202 files current; all 19 scanner guards pass. Diff checks pass.
Go runtime tests, race, vet and coverage were not rerun for this fixture-only slice.

## 2026-10-10 immutable named citation protocol foundation

Baseline: `d04e417` plus this native protocol slice. Text blocks now retain
absent/null/empty and non-array citation metadata. Five named immutable citation
variants expose typed document/search fields and arbitrary-size signed indices.
Malformed known variants and unknown SDK data retain closed JSON extension values;
extra members survive known-variant encoding. No any/interface/RawMessage field
is retained in a domain type. Public accessors detach lists and return variant
copies; append allocates a fresh list. Recording string mapping masks extension
keys and values before escaping.

All 17 pinned SDK completed cases round-trip through protocol decoding, content
clone and actual ModelRequest request encoding. Additional tests cover absent/null
cache annotations, direct TextBlock projection, masking without source mutation,
immutable delta append and independent bounded-network/unbounded-stored-message
limits. The README canonical map was reviewed; boundary prose, visible baseline
and interactive specification describe the data model and remaining provider gap.
The interactive artifact is regenerated from the specification.

HTTP reply decoding, SSE citation accumulation and any Agent paths that reconstruct
text-only blocks still require integration and end-to-end evidence. This foundation
alone does not close G4 provider parity or prove citation truth/authority.

Validation: `go test ./protocol -count=1`, full
`go test -p=1 -count=1 -timeout=300s ./...`, full
`go test -race -p=1 -count=1 -timeout=300s ./...` and `go vet ./...` pass.
The production type audit reports 400 files, 1302 package-level types and zero
direct open-payload findings. Archify delivery passes 9/9 checks with specification
SHA256 cf50a997a8d90061e59313a65c83d16d388a1a1b6345c8855a551bba63714452
and artifact SHA256 6766a9d6217de03bb1b183386019301cbf111f1ed958034658707ff34d525b32;
both hashes match current files. No fresh browser visual inspection was performed.
Diff checks pass. Compiler output includes macOS FSEvents deprecation and race
linker warnings. Python code and default-tool types are unchanged; Python gates,
Source export check, coverage and performance were not rerun for this slice.

## 2026-10-10 native citation HTTP/SSE and metadata-preserving rewrites

Baseline: `aee0437` plus this provider integration. Completed HTTP text replies
retain all five citation variants, nullable SDK fields, arbitrary-size signed
indices and closed extension data. Native normalization mirrors whole-variant
integer coercion and loose null-filled construction: if any declared field fails
validation, original primitive values survive. Unknown discriminators use the
SDK's first char model defaults while remaining extension data in Go. The Source
capture records SDK 0.107.1, installed Pydantic version and constructor/accumulator
source hashes; 35 completed replies are compared to actual SDK forwarding.

SSE retains initial citations and appends ordered citation deltas with no text
progress for citation events. Metadata is included in the block accumulation
budget; invalid ownership, missing delta fields and non-array append fail. Two
actual SDK streams and subsequent forwarding preserve final content, including
default `parsed_output: null`. Structured output formats remain unsupported;
only the default null output profile is admitted. Older direct/stream recipes now
retain SDK citation metadata instead of projecting it away. Source exports remain
202 files. Request lowering replaces text through TextBlock.WithText, preserving
all metadata and the original authoritative history in all 35 Source cases.

README Mermaid provider label, boundary prose, visible baseline, extension seam
and interactive specification describe this integration. G4/G7 still require
citation-bearing managed persistence/restart/recording acceptance plus the final
provider content/request/auth inventory audit. No live paid-provider call, new
dependency, default streaming selection or authority change is claimed.

Validation on the complete worktree: targeted agent/provider/protocol tests pass;
full `go test -p=1 -count=1 -timeout=300s ./...`, full
`go test -race -p=1 -count=1 -timeout=300s ./...` and `go vet ./...` pass.
Full Python regression: 2195 passed, 28 skipped, 24 subtests passed, 3 existing
warnings. Source export check: 202 current files. All 19 scanner guards pass.
Production type audit: 401 files, 1302 package-level named types, zero direct
open-payload findings. Archify delivery: 9/9 checks, current specification SHA256
3a01e7ec602611b96b533cbd5676788267c55e089e538fc5b25085426292cccd and
artifact SHA256 310745bff7ae30fad7fe63bd012518c3cacafe67cfc95e18bfbb74c8deaebbc2.
Both hashes match current files. No fresh browser visual inspection, coverage,
performance or Python mutation sweep was run. Compiler output includes existing
macOS FSEvents deprecation and race linker warnings. Diff checks pass.

## 2026-10-10 citation-bearing native SQLite HTTP and recording recovery proof

Code baseline: `dff40b4`. New integration tests exercise real localhost provider
HTTP, authenticated HTTP API, Manager, native SQLite and retained trajectory files.
Both direct and optional streaming profiles compose eight citations from pinned
Source recipes: five known variants, extension members, an unknown discriminator
and a signed arbitrary-size index (10**80). This composition is native integration
evidence, not a new SDK capture or production-provider deployment check.

An explicit registry masks both citation values and extension keys. Live second-turn
provider requests retain originals; actual SQL message payloads, stored HTTP
transcripts and all three trajectory model_output captures contain only masked
metadata. Complete citation arrays, nullable stream parsed_output and the large
index survive. The database is closed, the old handle is proven unreadable, and
a newly opened connection/new Manager reconstructs the same masked history after
explicit operator restoration. Construction does not restore automatically; owner
identity and Bob's HTTP denial remain intact. Restored requests forward that durable
masked history, fresh writes retain confirmed leases, and shutdown releases the
previous lease. The registry is explicitly injected; default Null behavior is not
claimed to mask. This is same-process database/Manager reconstruction, not a new
kill-process test or crash-atomicity proof.

Only a focused integration test and review/evidence documentation changed; no
runtime implementation, topology, public flow, authority or default changes.
README architecture map reviewed and visible baseline updated. The canonical
Mermaid and generated interactive specification remain unchanged.

Validation on the final test snapshot: targeted
`go test ./sqlitestate -run '^TestCitationNativeSQLiteHTTPRecordingAndReopen$' -count=1 -timeout=90s`,
full `go test -p=1 -count=1 -timeout=300s ./...`, targeted
`go test -race ./sqlitestate -run '^TestCitationNativeSQLiteHTTPRecordingAndReopen$' -count=1 -timeout=90s`
and `go vet ./...` pass. Original JSONL files are checked directly for citation
output, absence of both registered secrets and 0600 permissions, independently
of the parsed recording export. Existing macOS compiler/linker warnings remain.
Diff checks pass. No full race, Python regression/verifiers, Source export,
coverage, performance or fresh architecture rendering was rerun for this test-only
slice; runtime and exported contracts are unchanged.

## 2026-10-10 typed credential discovery library

Baseline `80c0dc5`. Named configuration captures profile/directory, optional
federation field presence and a private identity reader. Defined empty API key
or auth token suppresses discovery. Snapshots discard selectors, literal secrets
and callbacks; optional strings are detached. Explicit map inputs capture literal
identity; an explicit reader supports rotation and disappearance. Platform profile
roots and relative config directories are resolved at settings load.

`provider.DiscoverCredentials` implements explicit profile/active pointer,
complete environment federation, then fallback profile selection. Explicit profile
errors are eager; unreadable fallback profiles defer to missing request auth.
No Launcher default is activated by this library commit. Existing guarded profile
and token boundaries remain in force. No dependency is added; the approved SQLite
driver stays pinned to v1.14.52. Runtime integration and final full-worktree gates
are recorded in the next commit. Targeted config/provider tests passed during
integration; diff checks passed. No merge or push.

Final integrated worktree gates passed: full ordinary Go tests with cross-package
coverage, full race, vet, type audit (412 production files / 1327 named types /
zero findings), Source export (202 files) and 19 scan guards. Code/tests remained
frozen during full runs. Logs: `/tmp/mini-loop-discovery-{go,race,vet,types,export,scans}.log`.
The following integration commit records coverage and architecture activation.

## 2026-10-10 Launcher automatic credential discovery

Library baseline `c910c57` plus Launcher/CLI integration. Real providers now select
explicit profile/config directory or active pointer, complete environment federation,
then fallback profile. Defined empty static key/auth suppresses lookup. Fake providers
skip lookup. Explicit profile errors remain constructor faults; unreadable fallback
profiles preserve deferred missing request auth. Profile headers precede custom headers.
Token/model HTTP share an explicitly borrowed transport or a private owned clone;
App does not close borrowed transports. Token exchanges use a 30-second timeout.
Selectors are captured once. Explicit SDK maps capture literals unless an identity
reader is supplied. Real CLI process identity reads rotate regardless of dotenv
selection. Snapshots discard live credential selectors, secrets and callbacks.

The managed Launcher harness checks constructor phases, actual model/token URLs,
headers/bodies, call counts, replies/error projections and private disk cache files
for 42 of 43 pinned discovery recipes. The forced-invalidation recipe is explicitly
skipped here because Launcher exposes no same-client invalidation handle; existing
provider tests cover that operation. Reconstruction is compared through real App
shutdown and new App creation. Settings tests cover defined-empty suppression,
privacy, detached optional strings, captured maps and explicit reader rotation.

README canonical Mermaid/boundary/review baseline, extension notes and interactive
specification were updated together. Archify delivery passed 9/9 checks, zero errors
and warnings. Specification SHA256 d6a87d7bbc9ecd62a5cd83754dea425b5478c8d9c59586c320c8ac521ccb41b5;
HTML SHA256 bda5d88b623283c4d94c1e4ce53939bd85f1d1d0f5d6d263eda8f99735ef8556.
No fresh browser visual inspection was performed. Migration/G7 remains open.
No new dependency, merge or push. Full Python regression, Python invariant/mutation,
performance, Linux-host and CGO-disabled checks were not rerun in this Go slice.

### Final integrated coverage

Darwin arm64, `c910c57` plus this integration: full
`go test -p=1 -count=1 -timeout=300s -coverpkg=./...
-coverprofile=/tmp/mini-loop-discovery-coverage.out ./...` passed.
Identical source-location blocks are counted once using maximum hit count.
Application packages excluding tools: **89.59% (29792/33254 statements)**;
all Go packages: **89.56% (29839/33318)**. Compared to application baseline
`a9585cb` (89.66%), the denominator grew by 685 statements and covered statements
by 591; percentage decreased 0.07 points. This is collected statement coverage,
not branch coverage or migration acceptance. CLI child-process/C counters are not
merged; Linux-only code is outside the Darwin denominator. Temporary profile/logs
are untracked. Python coverage remains unrefreshed.

Final code snapshot gates all passed: full Go ordinary tests with cross-package
coverage, full `go test -race -p=1 -count=1 -timeout=300s ./...`, and `go vet ./...`.
Code/test/fixture files stayed frozen throughout final full jobs. Type audit passed
with 412 production files, 1327 package-level types and zero findings; unchanged
Source contracts checked current (202 files), 19 scanning guards passed. Logs:
`/tmp/mini-loop-discovery-{go,race,vet,types,export,scans}.log`. Existing Darwin
FSEvents deprecation and LC_DYSYMTAB warnings did not prevent passing. README outline,
architecture artifact hashes, worktree and staged diff checks passed. No merge/push.

## 2026-10-10 defined-empty authentication and literal identity Source alignment

Baseline `4e92d28`. Eight real SDK 0.107.1 discovery recipes extend the inventory
from 43 to 51; the preceding 43 outputs are identical after JSON decoding.
They cover empty key/auth shadowing of a missing explicit profile, nonempty bearer
shadowing, custom-header non-shadowing, empty selector fallback, empty literal
identity, literal rotation and literal disappearance after expiry.

The new managed comparison initially failed for empty auth: native settings lost
the SDK's `Authorization: Bearer ` header. Named config mapping now retains that
presence without treating an empty Secret as a nonempty token. Nonempty API-key
priority and ordered custom-header override remain intact. No raw env payload or
new domain type is introduced. Empty literal identity already matched Source;
projected files continue rejecting empty content. Explicit readers already rotate
and report disappearance; the new harness injects that live boundary and advances
the shared clock. The fixture's original environment remains captured for selectors.

Six Python provider-auth tests and narrow Go config/provider/Launcher tests pass.
Managed tests compare 50 recipes; one same-client forced invalidation recipe remains
explicitly skipped at Launcher and covered by existing provider tests. No production
network/provider charge, dependency, Python runtime module or default tool change.
README boundary and visible baseline, extension notes and architecture specification
are updated together. Full migration/G7 acceptance remains open. Coverage has not
been refreshed for this slice; `4e92d28` retains the 89.59% application snapshot.
No merge or push.

Source export initially exited 1 for unchanged `python-watchfiles.json`; no watcher
fixture was rewritten. A fresh retry exited 0 with all 202 files current. Both logs
are retained (`/tmp/mini-loop-credential-presence-export{,-retry}.log`). All 19 scan
guards passed. Type audit: 412 production files, 1327 package-level types, zero
findings. Archify regenerated from the specification with 9/9 checks, zero errors
and warnings. Specification SHA256 411f4f4d899675ede3465afc5ac8decd07c1fecb8e11809ef1a35facf60b8bd0;
HTML SHA256 191e687bca229a841f93fe93f0a2151286837bbab7963f1fdd302a60e6cc3e8f.
No browser visual inspection was performed. Full Python regression, Python package
invariants/guard mutation, coverage, performance and non-Darwin runs were not rerun;
Python runtime modules/default tools/mutation anchors remain unchanged.

Final frozen code/test/fixture snapshot passed full
`go test -p=1 -count=1 -timeout=300s ./...`, full
`go test -race -p=1 -count=1 -timeout=300s ./...` and `go vet ./...`.
Six Python auth tests passed. Logs:
`/tmp/mini-loop-credential-presence-{go,race,vet,types,scans}.log`.
Existing Darwin FSEvents deprecation/LC_DYSYMTAB warnings remain nonfatal.
README architecture map reviewed; topology remains unchanged. Specification/artifact
hashes match generated files. Worktree/staged diff checks pass. No merge or push.

## 2026-10-10 provider runtime scope audit and complete usage Source capture

Source capture commit `ab1833a`, runtime baseline `477a209`. Current status/index
now removes delivered Launcher discovery from the open implementation queue.
[GO_PROVIDER_SCOPE_AUDIT.md](GO_PROVIDER_SCOPE_AUDIT.md) maps actual Python SDK call
sites and generated fields to named native request boundaries. It does not claim
all custom provider/transport extensions or G4 acceptance are complete.

A confirmed native gap is SDK usage event fidelity. Actual AsyncAnthropic direct
and stream responses retain nine usage fields, nullable defaults, three named
nested structures and future extension data. Native decoding currently retains
five fields, dropping cache_creation, inference_geo, output_tokens_details and
server_tool_use. Seven pinned Source recipes capture minimal/null/nested/future/
scalar-string cases. Required input/output strings survive SDK construction while
optional cache_creation_input_tokens is coerced, so a uniform coercion assumption
is disproven by actual behavior. Native usage implementation is the next task;
this commit does not claim it is fixed.

Python provider usage/auth tests: 7 passed, one expected Pydantic scalar-string
warning. Source export: 203 matching files, exit 0. Scanner verification: all 19
guards pass, exit 0. Logs: `/tmp/mini-loop-provider-usage-{export,scans}.log`.
README architecture review baseline and document navigation are updated; no
runtime topology/default/authority/persistence changed. No fresh Go/race/vet,
full Python, invariants/mutation, coverage, performance or architecture regeneration
was run; native runtime/Python package modules/default tool types are unchanged.
Diff/cached checks pass. No merge or push. Original P0/P1/G0-G7 acceptance and
the default diagnostics dependency decision remain open.

## 2026-10-10 typed usage metadata and safe archival event foundation

Baseline `14e1ca1` plus protocol/event implementation. TokenUsage now has named
cache-creation buckets, inference geography, output thinking-token details and
server-tool counters. Optional fields preserve absent/null/value; closed immutable
extensions survive at top and nested levels. Deep clones detach every mutable
pointer. Explicit WithSDKDefaults materializes seven optional SDK nulls while
native/fake constructors preserve sparse shape.

Protocol tests round-trip the six structurally valid usage Source cases; the
scalar-string case explicitly asserts existing strict native integer refusal.
Required nested counts are non-negative native integers. This is not full seven-
case SDK normalization parity. Negative/missing/mistyped/oversized ingress fails
atomically. Existing core replies and recovery fixtures retain their representation.

New event tests verify clone isolation, string/extension-key masking before JSON
escaping and exact stored-event round-trip with inert archival provenance. Metadata
masking leaves caller usage unchanged. Future extension data cannot become actor,
permissions or tool authority. Actual SQLite/trajectory restart composition remains
for the following transport adoption proof; JSON archival round-trip is not that proof.

HTTP/SSE provider decoders are unchanged and still discard metadata. This commit
establishes the named representation and event path; provider adoption, SDK defaults,
nullable/coercion reconciliation and final G4/G7 remain open. No dependency or
activation flag changes. README canonical label/boundary/review baseline, extension
notes and interactive specification describe this exact state.

Type audit passed: 413 production files, 1331 package-level types, zero findings.
The targeted `verify_guards.py -k secret` selection caught both matched mutations
with passing baselines; it is not a fresh full mutation sweep or Go mutation proof.
Archify regenerated from the reviewed specification with 9/9 checks, zero errors
and warnings. Specification SHA256 a65761be4c5abe6e918cf50235b5173772ddc36a1c26efc6417e90a4c2c0ae16;
HTML SHA256 eacf0c9c075b30a9ed7b3402d5bcc38356d9d1e90a5da9857d628dbf9f253342.
No browser visual inspection was performed. Source export, scans, full Python,
Python package invariants, coverage, performance, Linux and CGO-disabled runs were
not refreshed; Python package modules, fixtures, scanners and default tool types
are unchanged. Native metadata tests provide the direct Go guard evidence.

Final frozen code/test/fixture snapshot passed narrow protocol/agent/provider tests,
full `go test -p=1 -count=1 -timeout=300s ./...`, full
`go test -race -p=1 -count=1 -timeout=300s ./...` and `go vet ./...`.
Logs: `/tmp/mini-loop-usage-metadata-{targeted,go,race,vet,types,guards}.log`.
Existing Darwin FSEvents deprecation/LC_DYSYMTAB warnings remain nonfatal.
Code/tests stayed unchanged during full jobs. README outline and worktree/staged
diff checks pass. No merge or push. Full port acceptance remains open.

## 2026-10-10 HTTP/SSE usage metadata adoption

Baseline: `9495de6`; Source additions committed separately at `387cf80`.
Direct HTTP and message_start use named TokenUsage decoding with SDK null defaults;
message_delta updates server-tool counts with SDK accumulation semantics. Nested
counts retain absent/null/value via named optional integers, and clones detach their
pointers. Nine actual SDK cases cover eight structural matches plus explicit native
scalar-string refusal. Initial partial-nested mismatch was resolved by preserving
sparse codecs and normalizing missing nested fields only at SDK transport projection.

Verification: targeted protocol/provider/agent tests; full ordinary Go suite with
`-p=1 -count=1 -timeout=300s -coverpkg=./...`; full Go race suite
`-race -p=1 -count=1 -timeout=300s`; go vet; typeaudit (413 production files,
1331 named types, zero findings); Python usage capture test (1 passed, expected
Pydantic scalar-string warning); Source export check (203 current files); scans
(19 anchored guards); git diff --check. Architecture regenerated with Archify,
9/9 checks and zero errors/warnings. Specification SHA256
630d3fc26c3d9d18fed072c4d4ba6fbbad26bf7f2b791d0021f4086c51ace356;
HTML SHA256 534ae6b64dfe18def1ba9e6f521b29514829846a6a964a2d89b55a8504c64d51.
No fresh browser visual inspection was possible under the earlier access denial.

Coverage: application excluding tools 89.60% (29939/33413); all Go 89.57%
(29986/33477). Profile: /tmp/mini-loop-usage-adoption.cover. Darwin excludes CLI
child-process/native C and Linux-only counters. Full Python was not refreshed;
no merge or push. Real usage SQLite/recording restart composition, scalar coercion
reconciliation and complete G4/G7 scope acceptance remain open.

## 2026-10-10 usage SQL and recording reconstruction evidence

Baseline: `caa135c`. Extended the existing citation HTTP/SQLite/recording recipe
with named usage from pinned SDK nested direct/stream captures and server-tool
delta capture. Registered secrets are injected into inference geography and
root/nested extension keys/values. This is local composition evidence, not a new
SDK capture or a paid-provider deployment check.

The test checks raw SQL event payloads and typed archival reads after two turns,
closes the Manager/database, reopens a new SQLite connection and rereads events,
explicitly restores the session and executes a third turn. All fields, nulls and
extensions match the masked SDK expectation. Raw private JSONL and a newly
instantiated recording reader also preserve full usage for each of the three
turns. Existing citation forwarding, owner refusal, lease release, restored write
status and 0600 file checks remain. No runtime implementation or dependency changed.

Targeted direct/SSE reconstruction and the full ordinary Go suite
`go test -p=1 -count=1 -timeout=300s ./...` passed; the full sqlitestate race suite
`go test -race -count=1 -timeout=300s ./sqlitestate` and `go vet ./...` passed.
Architecture status was regenerated with Archify: 9/9 checks, zero errors/warnings.
Specification SHA256 d34cf46f1ffdc6da042fc5e7f246d3580472eef250112671bd7043876b45a78e;
HTML SHA256 6ed721f9ab4c9a5d66890d1bc17324282b81bdaf54520bca41ac747456bd832e.
No fresh browser visual inspection (earlier access denial), Source export, scans,
Python suite, type audit or coverage refresh is claimed; their production targets
and Source fixtures were unchanged. Previous application coverage is 89.60%.
This recipe does not prove new process-kill/power-loss durability. Scalar coercion,
provider variant/custom extension audit and complete G4/G7 acceptance remain open.
git diff --check and staged diff --check passed. No merge or push.

## 2026-10-10 named container-upload reply adoption

Baseline: `b3a4bd9`; six pinned SDK captures committed independently at `3dd7a47`.
Native protocol adds a named FileID and explicit observed null flags in its closed
Block union. Direct/SSE decoding, clone/accessor isolation, ordinary forwarding,
masked stored-message codec and SQLite FileID UTF-8 validation support the variant.
Source stream capture exposed the ParsedTextBlock union's text/citations/parsed_output
null defaults; those are retained instead of treating both transport shapes as equal.
Empty and Unicode IDs are accepted according to the captured declared string shape.
Missing/null/numeric IDs and mixed domain variants are refused. This metadata does
not become a client tool call; the Source decision content allowlist still refuses it.

Verification passed: full Go ordinary and race suites
`go test -p=1 -count=1 -timeout=300s ./...` and
`go test -race -p=1 -count=1 -timeout=300s ./...`; six Go decoder/forwarding/archival comparisons; protocol
clone/mask/atomic-ingress/variant/pairing checks; existing sqlitestate targeted suite;
Python source-capture test (1 passed); Source export check (204 current files);
scans (19 anchored guards); go vet; typeaudit (413 production files, 1332 named
types, zero findings); Python `verify_guards.py -k block` (3 caught with
passing baselines; this is a named subset, not the full mutation sweep);
git diff --check and staged diff --check. Architecture regenerated with Archify, 9/9 checks and
zero errors/warnings. Specification SHA256
0e7135faec88a57bc75b5cbdad9de5b2d6a611a7802987ceacf0b3ceae45ce0e;
HTML SHA256 0e3157ee70db8856227ccc5c49ac35b12cf1c71900decbb8dc817f0ec0fd7914.
Fresh visual inspection remains unavailable under the earlier browser access denial.
No full Python or coverage refresh; no merge/push. This is not a new upload/download
endpoint or real native container-specific SQLite restart recipe. Seven remaining
SDK server-tool/result variants, external extensions and scalar usage consumer
reconciliation remain open; the complete G4/G7 migration is not accepted.

## 2026-10-10 named web-search result replies

Baseline: `ada45b2`; fourteen SDK direct/SSE recipes committed independently at
`a83e10f`. The native closed Block union now accepts web_search_tool_result, with
ToolUseID/Caller and a closed results-or-error content union. Results name Title,
URL, EncryptedContent and optional PageAge; errors name Code and preserve observed
future string codes. Root/result/error extensions retain immutable closed JSON
values, never replacing named fields. Provider ingress selects SDK caller/page-age
null defaults; native sparse serialization and larger historical frames are retained.
Client-tool pairing/dispatch and the decision allowlist stay on their Source paths.

Metadata codec helpers are shared with usage under generic private names; the usage
codec's integer/null/default contract is unchanged. SQLite validates named strings
before encoding; masks cover metadata and extension keys/values. Tests compare all
14 SDK content projections and forwarded/stored message codecs, cloned accessors,
registered-secret extension masking, atomic rejection, closed variant validation,
empty results and >512 KiB historical metadata versus bounded network input. This
is decoder/composition evidence, not a new server-search request/execution option
or a new actual web-result SQL/restart recipe.

Verification: full ordinary Go suite `go test -p=1 -count=1 -timeout=300s ./...`;
full race suite `go test -race -p=1 -count=1 -timeout=300s ./...`;
targeted protocol/provider/sqlitestate suites; go vet; typeaudit (415 production
files, 1338 named types, zero findings); Python SDK capture test (1 passed, expected
future-code serializer warning); Source export check (205 files current); scans
(19 anchored guards); Python `verify_guards.py -k block` (3 caught with passing
baselines; a named subset, not the complete mutation sweep); git diff --check and
staged diff --check. Architecture regenerated with Archify,
9/9 checks and zero errors/warnings. Specification SHA256
97b02a4ee4436c6246415d42f63f362fad51fc61004096af88e4710aadb1ee0f;
HTML SHA256 9bc048ea55c9d540c5389ddf94ff62a4d5f6c7726ebf75b7cbe75a6de5dfacdd.
No fresh browser visual inspection under the earlier access denial, full Python
regression or coverage refresh. Six remaining SDK reply variants, caller/external
extensions, scalar usage semantics and complete G4/G7 acceptance remain open.
No merge or push.

## 2026-10-10 named tool-search result replies

Baseline: `1c68fd0` plus this implementation; Source capture commit `51ff2ce`.
Twelve actual Anthropic SDK 0.107.1 direct/SSE recipes cover empty/nonempty
references, unknown/Unicode names, error messages, future codes and extensions.
Named references/error variants retain SDK null defaults and immutable metadata;
forwarding, masking, clone isolation, atomic invalid-input refusal and historical
records above the network limit are verified. References remain inert metadata.
This slice adds archival codec evidence, not a new tool-search SQL restart recipe.

Passed: targeted protocol/provider/sqlitestate tests; full Go tests and race
(`-p=1 -count=1 -timeout=300s ./...`); go vet; typeaudit (416 production files,
1345 named types, zero findings); Python SDK capture test (1 passed, expected
future-code serializer warning); scans (19 guards); block mutation subset
(3 guards); git diff --check. Source export initial and retry checks both exit 1:
only unchanged `python-watchfiles.json` is stale; the other 205/206 files match.
The unrelated watcher fixture was not rewritten.

Architecture specification regenerated with Archify: 9/9 checks, zero errors or
warnings. Specification SHA256
217dd0ae06ec77bac1115ae667e1245995312b205d9458b4baea2934065b8e3a;
HTML SHA256 77fdc0cb429b2758b30105f37bb1ca5a648a2d98b4e07d73720887aa60ba9cf7.
No fresh browser visual inspection, full Python regression, full mutation sweep
or non-Darwin run. No merge/push. Five SDK reply kinds, usage scalar consumer
alignment, default diagnostics and broader final migration acceptance remain.

Coverage refreshed for this implementation: full `go test -p=1 -count=1
-timeout=300s -coverpkg=./... -coverprofile=/tmp/mini-loop-tool-search-integrated.cover
./...` passed. Duplicate source locations are merged with maximum hit count.
Application excluding tools: **89.47% (30239/33799)**; all Go:
**89.44% (30286/33863)**. Previous matching application snapshot was 89.60%
(29939/33413); this slice adds 386 statements, with covered statements up 300.
Darwin counters exclude CLI child-process/native C and Linux-only paths.
Package-local measurement also passed: 86.46% (29224/33799), a different scope
that excludes cross-package integration hits; use 89.47% for historical comparison.

## 2026-10-10 named provider-owned tool-use observations

Baseline: `0781cd7` plus this implementation; SDK Source capture committed at
`8c0b442`. Thirty-two Anthropic SDK 0.107.1 direct/SSE captures compare complete
server_tool_use metadata: seven known names, future name, input objects/nested
scalars, null/direct/code-execution callers, root extensions, initial-input
replacement and empty deltas. ServerToolName and ServerToolInput are separate
from client ToolName/ToolInput. The SDK declares no individual input fields:
ServerToolInput owns an immutable closed object with explicit recursive variants.
No partial JSON, raw payload, open map or interface enters the domain.
Sparse caller omission, SDK null defaults, root extensions, clone isolation,
masking and large historical input are verified. Client dispatch/pairing and
decision admission retain their existing boundaries. SQLite close/reopen
compares every SDK capture, and invalid UTF-8 named ID/name rolls back batches.
This proves database reconstruction, not a new killed-process/power-loss test.

Passed: Python SDK capture test (1 passed); targeted protocol/provider/sqlitestate
suites; full Go tests and race (`-p=1 -count=1 -timeout=300s ./...`); go vet;
typeaudit (417 production files, 1348 named types, zero findings); scans (19
guards); block mutation subset (3 guards); git diff --check. Source export
check exited 1: only unchanged python-watchfiles.json is stale, the other
206/207 files match. The unrelated watcher fixture was not rewritten.
Logs: /tmp/mini-loop-server-tool-{source,targeted,go,race,vet,types,export,scans,guards}.log.

Architecture regenerated from the specification with Archify: 9/9 checks, no
errors/warnings. Specification SHA256
9981465b5b0790e1ced31c69fcaf5a7086be92b4e7d38df5ee6a5661352ccadb;
HTML SHA256 0ec43cfe4fbfbb5b3c6f4876c17366237bb19626b4effd058d6fb77a44bca256.
No fresh browser visual inspection, full Python regression, full mutation sweep,
coverage refresh or non-Darwin run; no merge/push. The previous application
coverage remains 89.47% at the earlier tool-search snapshot, not this code.
Four SDK reply kinds, SDK partial/malformed-input and caller extension audit,
usage scalar consumer alignment, default diagnostics and final acceptance remain.

## 2026-10-10 named web-fetch document replies

Baseline: `d271228` plus this implementation; SDK Source capture committed at
`b8dc489`. Forty-two Anthropic SDK 0.107.1 direct/SSE cases compare complete
web_fetch_tool_result projections: text/PDF document sources, empty text, nullable
title/retrieval/citation configuration, enabled true/false, caller variants, nine
error codes, future code and root/result/document/source/configuration extensions.
Known fields enter named types and closed source/result/error variants. Immutable
closed JSON is retained only for unrecognized extensions. PDF data is verbatim
observational metadata; no URL/document effect or client authority is introduced.
SDK direct metadata defaults and stream ParsedTextBlock null defaults are selected
explicitly. Sparse native codecs distinguish omission. Clone/accessor isolation,
masked keys/values, atomic nested refusals and historical sources above network
limits are verified. SQLite close/reopen preserves all forty-two captures; invalid
ID/error-code/URL/data/media/title/retrieval UTF-8 rolls back message batches.
Database reconstruction is proved, not a new killed-process/power-loss scenario.

Passed: Python SDK capture test (1 passed, expected future-code serializer warning);
targeted protocol/provider/sqlitestate suites; full Go tests and race
(`-p=1 -count=1 -timeout=300s ./...`); go vet; typeaudit (419 production files,
1359 named types, zero findings); scans (19 guards); block mutation subset
(3 guards); git diff --check. Initial Source export check exited 1: unchanged
python-watchfiles.json alone was stale, other 207/208 files matched. A separate
watchfiles-only capture matched the existing fixture without rewriting it.
Full quiet-process snapshot diagnostics follow below.
Logs: /tmp/mini-loop-web-fetch-{source,targeted,go,race,vet,types,export,scans,guards}.log.

Architecture regenerated from specification with Archify: 9/9 checks, no errors
or warnings. Specification SHA256
6459bcd8509647f0820fcd98f2d922a41a27c0afe69eede305841460ffdafc60;
HTML SHA256 c17f7e3ae58b986db8b3eceaedcb3f6341efcfa02e6604a7d26d5c04a0b5ed0b.
No fresh browser visual inspection, full Python regression, full mutation sweep,
coverage refresh or non-Darwin run; no merge/push. The previous application
coverage remains 89.47% at the earlier tool-search snapshot, not this code.
Three SDK execution-result kinds, caller/partial-input audit, usage scalar
consumer alignment, default diagnostics and final migration acceptance remain.

Final Source freshness observation: after full Go jobs were terminal, the same
exporter `_snapshot()` was recomputed and every output byte compared against
its target, using the --check comparison rule. Exit 0, 208 files, stale=[].
Receipt: /tmp/mini-loop-web-fetch-export-isolated-result.json; log:
/tmp/mini-loop-web-fetch-export-isolated.log. The initial concurrent --check
exit 1 remains recorded. Watchfiles-only capture and quiet full capture both
matched; no watcher golden or helper was rewritten. This establishes current
recipe freshness while leaving the intermittent concurrent capture behavior
explicit. Avoid rewriting the fixture to hide that observation.

## 2026-10-10 named code-execution result replies

Baseline: `897cad8` plus this implementation; SDK Source captures committed at
`bc77abb`. Twenty-eight Anthropic SDK 0.107.1 direct/SSE recipes compare ordinary,
encrypted and error result projections, empty/nonempty output files, large signed
return codes, four errors/future code and root/result/output/error extensions.
ExecutionReturnCode preserves the closed integer variant above machine width;
other scalar kinds are refused. Named stdout/encrypted stdout/stderr and FileID
remain observations, with no execution, file-opening, decryption or client grants.
Output accessors/clones detach slices; extensions remain immutable closed JSON.
Sparse codecs and explicit SDK stream nulls remain distinct. Forwarding, masking
of keys/values, nested atomic refusals and historical stdout above frame limits
are verified. SQLite close/reopen retains all twenty-eight recipes, and invalid
UTF-8 in named ordinary/encrypted/error fields rolls back batches. Database
reconstruction is proved, not a new killed-process/power-loss scenario.

Passed: Python SDK capture test (1 passed, expected future-code serializer warning);
targeted protocol/provider/sqlitestate suites; full Go tests and race
(`-p=1 -count=1 -timeout=300s ./...`); go vet; typeaudit (420 production files,
1368 named types, zero findings); scans (19 guards); block mutation subset
(3 guards); git diff --check. Source export capture runs after full native Go
watcher suites are terminal, following the previous concurrent-capture drift
observation. Its terminal outcome is recorded below.
Logs: /tmp/mini-loop-code-execution-{source,targeted,go,race,vet,types,export,scans,guards}.log.

Architecture regenerated from specification with Archify: 9/9 checks, no errors
or warnings. Specification SHA256
1a07c80da4d5c9e7e80d65c03c0f3aa89243f443a3f7d4e18eaf976cfc337728;
HTML SHA256 fd273d9dc256c6af987f7545e1575a699e97b662507abf56bd23f5fea6be465f.
No fresh browser visual inspection, full Python regression, full mutation sweep,
coverage refresh or non-Darwin run; no merge/push. Previous application coverage
remains 89.47% at the older tool-search snapshot, not this implementation.
Two SDK result kinds (bash and text-editor), caller/partial-input audit, usage
scalar consumer alignment, default diagnostics and final migration acceptance
remain.

Final Source export check: `.venv/bin/python python/tools/export_go_contracts.py
--check` exited 0, Python→Go contracts current: 209 files. It ran after ordinary
and race Go suites completed, with no watcher golden/helper changes. The older
concurrent-capture drift remains in the previous checkpoint, not hidden by a
fixture rewrite. Log: /tmp/mini-loop-code-execution-export.log.

## 2026-10-10 named provider Bash execution result replies

Baseline: `37d4451` plus this implementation; SDK Source capture committed at
`b452273`. Twenty-six Anthropic SDK 0.107.1 direct/SSE cases compare complete
bash_code_execution_tool_result metadata: stdout/stderr, empty/nonempty files and
text, signed return codes above machine width, five errors including
output_file_too_large, future code and root/result/output/error extensions.
Named Bash result and file types retain distinct SDK labels; ordinary code result
or file labels are refused rather than aliased. ReturnCode reuses the existing
closed integer type. Result/error variants and immutable extensions retain
forwarding, mask, clone and sparse/history semantics. Stream null metadata is
selected explicitly. SQLite close/reopen preserves all twenty-six recipes;
invalid UTF-8 ID/code/stdout/stderr/file ID rolls back whole batches.
Database reconstruction is proved, not a new killed-process/power-loss scenario.
Provider Bash observations do not select BashInput, execute a command, open files
or grant client result pairing. Decision admission keeps its Source allowlist.

Passed: Python SDK capture test (1 passed, expected future-code serializer warning);
targeted protocol/provider/sqlitestate suites; full Go tests and race
(`-p=1 -count=1 -timeout=300s ./...`); go vet; typeaudit (421 production files,
1375 named types, zero findings); scans (19 guards); block mutation subset
(3 guards); git diff --check. Source capture begins after native ordinary/race
Go watcher suites are terminal; its result follows below.
Logs: /tmp/mini-loop-bash-execution-{source,targeted,go,race,vet,types,export,scans,guards}.log.

Architecture regenerated from specification with Archify: 9/9 checks, zero
errors/warnings. Specification SHA256
21dd88944146824d85fcfe929c033ff9eda82337303376228e3474b5c452f280;
HTML SHA256 1aa31617e3bc0e1651a5a3122fa7505b476d1964406aefb83e5917a0f819e317.
No fresh browser visual inspection, full Python regression, full mutation sweep,
coverage refresh or non-Darwin run; no merge/push. The prior application coverage
remains 89.47% at the older tool-search snapshot, not this implementation.
Text-editor SDK replies, caller/partial-input audit, usage scalar alignment,
default diagnostics and final migration acceptance remain.

Final Source export check: `.venv/bin/python python/tools/export_go_contracts.py
--check` exited 0; Python→Go contracts current: 210 files. Native Go watcher
suites were already terminal. No watcher helper/golden was rewritten.
Log: /tmp/mini-loop-bash-execution-export.log.

## 2026-10-10 named provider text-editor result replies

Baseline: `2d8ba81` plus this implementation; SDK Source capture commit `190a37e`.
Forty-eight Anthropic SDK 0.107.1 direct/SSE captures cover view text/image/pdf,
create/update, str_replace and five error codes; sparse/null/value line metadata,
empty lines, signed integers above machine width and unknown extensions remain
intact. Named view/create/replace/error variants retain SDK defaults, forwarding,
masking, detached clones and historical records above the network size limit.
SQLite close/reopen retains all 48 captures; invalid UTF-8 fields roll back batches.
This is reconstruction evidence, not a new killed-process/power-loss proof.
Editor observations remain inert and do not grant client tool-result pairing.

A captured opaque caller extension exposed eager SSE caller parsing: the actual
SDK retained it but the old native decoder refused the starting block. Caller
parsing now follows block kind; eight client-tool regression profiles preserve
existing validation. Declared caller extension parity remains a separate audit.
The installed SDK declares 12 ContentBlock kinds, all now represented; this does
not establish complete field/consumer/default-path or migration acceptance.

Passed: Source helper test (1 passed, expected future-code serializer warning);
targeted protocol/provider/sqlitestate suites; full Go ordinary and race tests
(`-p=1 -count=1 -timeout=300s ./...`); go vet; typeaudit (422 production files,
1385 named types, zero findings); scans (19 guards); block mutation subset
(3 guards). Source export and final diff validation are recorded below.
Logs: /tmp/mini-loop-text-editor-{source,targeted,go,race,vet,types,export,scans,guards}.log.

Architecture regenerated from specification using Archify: 9/9 checks, zero
errors/warnings. Specification SHA256
79e6128d82a00884d2fa9fe6ba21797aadc1c8ceb94fb3fa1680e04a3e5150ac;
HTML SHA256 79d4ed32281fa62ff17f134a3806c5f5592ae1ebbc580f3549dd98d81628f31b.
No fresh browser visual inspection, full Python regression, full mutation sweep
or non-Darwin run; no merge/push. Caller/partial-input behavior, usage scalar
consumers, default diagnostics, public-entry and external type/data-flow audits,
broader matched performance and final acceptance remain open.

Final Source export: `.venv/bin/python python/tools/export_go_contracts.py --check`
exited 0; all 211 contracts are current. Capture ran after ordinary and race
Go watcher suites were terminal; no watcher golden/helper was rewritten.
`git diff --check` passed. Coverage refresh is a separate follow-up checkpoint.

## 2026-10-10 coverage refresh after named text-editor replies

Measured runtime baseline: `b56975b`. Full cross-package coverage passed:
`go test -p=1 -count=1 -timeout=300s -coverpkg=./... -coverprofile=/tmp/mini-loop-text-editor-integrated.cover ./...`
from `go/`. Duplicate source locations are merged using their maximum hit count.
Application excluding tools: **89.28% (31132/34871)**; all Go:
**89.25% (31179/34935)**. Protocol: **88.37% (3139/3552)**;
provider: **86.10% (1344/1561)**; sqlitestate: **81.44% (575/706)**.
Compared with the matching tool-search application snapshot at `0781cd7`,
89.47% (30239/33799), covered statements increased by 893 and total statements
by 1072 across the subsequent server-tool-use, web-fetch, code, Bash and editor
slices; the ratio decreased about 0.19 percentage points. This comparison does
not attribute the difference to the editor slice alone.

Scope: Go statement coverage on Darwin, with cross-package integration hits;
CLI child-process, native C and Linux-only counters are not included. This is
not Python coverage or complete migration parity. Full ordinary/race, vet,
typeaudit, Source 211-file freshness, scans and the three block mutation guards
passed for the implementation as recorded above; no additional full Python,
full mutation, non-Darwin or browser inspection was performed. No merge/push.
Logs/profile: /tmp/mini-loop-text-editor-coverage.log,
/tmp/mini-loop-text-editor-integrated.cover;
summary: /tmp/mini-loop-text-editor-coverage-summary.json.

## 2026-10-10 declared caller extension preservation

Baseline: `b56975b` plus this implementation; actual SDK Source capture commit
`ff3fd06`. Forty-eight Anthropic SDK 0.107.1 direct/SSE recipes exercise absent,
null and unknown fields on the three declared caller kinds across client/server
tool-use and web-search/web-fetch result blocks. Before-fix provider replay
failed on unknown caller fields (log /tmp/mini-loop-caller-before.log).
ToolCaller now retains only unknown members in immutable closed extensions;
type and code caller tool_id remain named fields. Direct tool_id is an extension
and typed ToolID remains empty. No new client tool selection or grants are added.
Unknown caller kinds and malformed known fields retain their current refusal;
Source permissive construction and partial/malformed input audit remain open.

All captures match complete native direct/SSE content and forwarding/history
codecs. SQLite close/reopen preserves all 48 recipes. Protocol regression tests
verify extension key/value masks, detached caller accessor/clone behavior, failed
decode atomicity and historical nested frames above 512 KiB on all four block
kinds. Network ingress retains its bound. The old direct-tool-id rejection test
was updated to the captured SDK behavior; code callers still require a nonempty
string ID. Database reconstruction is proved, not new power-loss/process-kill
semantics. Python runtime modules and production feature defaults are unchanged.

Source helper test: 1 passed. Targeted protocol/provider/sqlitestate suites passed.
Archify regenerated the architecture from the specification: 9/9 checks, zero
errors/warnings. Specification SHA256
25dc581a684e6ac41ab093b21aa4972721bfd4b657b5ec03c00e42e06a01c62d;
HTML SHA256 faee5a252d398a6e60e73ce7b17cf31399262506eda044c787e34434a59bd004.
No fresh browser visual inspection, full Python regression, full mutation sweep,
coverage refresh or non-Darwin run; no merge/push. Coverage remains pinned to
`b56975b`: application 89.28%, not a measurement of this implementation.
Full Go and Source export gate results follow below.

Final gates: full ordinary Go and race suites (`-p=1 -count=1 -timeout=300s ./...`),
`go vet ./...`, typeaudit (422 production files, 1385 named types, zero findings),
19 anchored scanning guards, three block mutation guards and `git diff --check`
passed. `.venv/bin/python python/tools/export_go_contracts.py --check` exited 0;
all 212 exported Source contracts are current. Source capture started after both
native Go watcher suites were terminal. No watcher helper/golden was rewritten.
Logs: /tmp/mini-loop-caller-{source,targeted,ordinary,race,vet,types,scans,guards,export}.log.

## 2026-10-10 SDK partial-input contracts and native discrepancy audit

Source capture commit: `d984cbd`; native runtime baseline remains `689a1d9`.
Ninety-eight actual AsyncAnthropic SSE recipes pin SDK 0.107.1 and jiter 0.15.0,
accumulate_event and repository _content_payload source hashes. Forty-nine input
profiles cover empty deltas, incomplete containers/strings/literals/numbers,
nested values, duplicate keys, bytewise chunks, malformed input, trailing data,
root scalars and nonfinite numbers for client and server tool-use. Delivered
per-delta snapshots, final content or actual ValueError are retained. Nonfinite
observations are encoded inside JSON strings, keeping the fixture standard JSON.

Native diagnostic replay at the pinned runtime records 69 final differences
(client 37, server 32). Sixteen SDK errors are also refused by native code, but
native failure occurs at block stop rather than the corresponding input delta;
the 29 matching final outcomes do not prove intermediate/error-order parity.
Client named-input extra-field validation is a distinct gap from partial parsing.
Full evidence and the next implementation sequence are in
[the partial-input audit](docs/audits/provider-partial-input-2026-10-10.md).
The explicit opt-in diagnostic task produces a report, not a green parity gate;
its default skip must not be counted as conformance coverage.

No production parser/default behavior is changed. This is the Source and scope
prerequisite for a subsequent native implementation, not G4 or G7 completion.
Source helper test passes (1 test, four expected SDK serializer warnings).
Native diagnostic replay passes as an observation job, not a parity assertion.
No new dependencies, architecture topology, trust/persistence semantics or public
entry points are introduced. Coverage remains the b56975b snapshot (application
89.28%); no full Python regression, new race/type/mutation sweep, non-Darwin or
browser/architecture regeneration is claimed. Full Go/vet/scans/export results
follow below. No merge/push.

Final audit-slice gates passed: full ordinary Go suite
(`go test -p=1 -count=1 -timeout=300s ./...` from go/), go vet, Source helper test,
explicit native diagnostic observation, 19 anchored scanning guards and
`git diff --check`. After the full native watcher suite was terminal, Source
export check exited 0: all 213 recipe files current. No watcher helper or golden
was changed. No Go production implementation changed, so this is not a refreshed
runtime/race parity claim. Logs: /tmp/mini-loop-partial-{go,vet,source,scans,native-audit,export}.log.

## 2026-10-10 native SDK partial-input boundary and stream adoption

Baseline: `4b1aec7` plus this implementation; jiter edge capture `6ad4600`.
Separate internal/jsonvalue.DecodePartial returns immutable closed variants and
retains completed members/nested containers at EOF while omitting incomplete
scalars. Complete/archival Decode remains strict. Unicode/UTF-16 pairs, partial
hex escapes, duplicate keys, unexpected separators and the SDK 200/201 depth
edge are covered by 42 pinned jiter 0.15.0 observations. The 98 actual SDK
client/server cases assert every delivered input snapshot and rejection stage.

Selected native SSE paths parse each nonempty cumulative input delta. Empty
client/server deltas preserve initial input, raw buffers are capped before parse
and malformed input fails at its input delta. At block stop finite serialization
and existing named client ToolInput/object-valued ServerToolInput conversion
still apply. No partial bytes enter service/domain records and no new authority
or client effects are granted. Thirty-four finite server-object SDK cases match
final content, forwarding/history and actual SQLite close/reopen. Reconstruction
is proved, not new process-kill or power-loss semantics.

The audit's final differences shrink from 69 to 40 (client 33/server 7), and all
16 previously delayed syntax failures now occur at the corresponding delta.
Client extra/missing argument fields, root scalars/nonfinite consumers and
unknown caller kinds remain Source reconciliation work. Real boundary/final
assertion tests are separate from the opt-in diagnostic report; it now requires
an explicit MINILOOP_PROVIDER_AUDIT_BASELINE so new results cannot silently keep
the old snapshot label. The old native report is preserved; the new report is
[the implemented boundary observation](docs/audits/provider-partial-input-native-2026-10-10.json).

Passed so far: Source edge helper test (1 passed), targeted jsonvalue/provider/
sqlitestate suites; go vet; typeaudit (423 production files, 1386 named types,
zero findings); 19 scanning guards and the three block mutation guards.
Full Go ordinary/race and Source export results follow below.
Archify regeneration passed 9/9 checks, zero errors/warnings. Spec SHA256
b31a8fc1373730a99c829f57ea677d47eb4f969796d182d1fe599d68b005b544;
HTML SHA256 598d651444e54aef89f509300ac57da4d08d01f4d273de7cd17e7423578e6f4d.
No fresh browser inspection, full Python regression, full mutation sweep,
coverage refresh or non-Darwin run; no merge/push. Application coverage remains
89.28% at b56975b, not this implementation. Diagnostics default approval,
provider consumer/caller scope, external type/public-entry/default audits,
broader matched performance and final acceptance remain open.

Full ordinary Go suite passed. The first full race run failed only
TestActualCLIRebuildsSourceRetainsPortAndReloadsDotenv: HTTP 500 at the second
environment check, with no data-race report. The exact race test retry passed;
a second full race suite, run without concurrent validation jobs, passed.
The root cause of the first 500 is not proven. No reload code/tests were changed.
Both attempts remain recorded; this is not an uninterrupted initial race pass.
Logs: /tmp/mini-loop-partial-decoder-race.log,
/tmp/mini-loop-partial-decoder-race-reload.log,
/tmp/mini-loop-partial-decoder-race-retry.log.
Source export result follows after all native watcher jobs were terminal.

Final Source export check exited 0: all 214 recipe files are current, including
the 42-case jiter edge fixture. It ran after full ordinary and both full race
attempts were terminal; no watcher golden/helper was changed. git diff --check
passed. The narrow Source helper, targeted suites, full ordinary suite, exact CLI
race retry, full race retry, vet, typeaudit, 19 scans and three block mutation
guards are the gates run. Full Python/visual/non-Darwin/coverage/full mutation
checks remain unrefreshed. Logs: /tmp/mini-loop-partial-decoder-{source,targeted,go,race,race-reload,race-retry,vet,types,scans,guards,audit,export}.log.

## 2026-10-10 deferred default Bash argument binding

Implementation baseline: `d3c8203`; actual consumer Source contract: `cea0e39`.
The actual SDK SSE and Python `Agent._exec_tool` capture has 48 cases, including
before rewrites/denials and first-unknown-parameter order. Forty-one supported
finite-object cases match native execution outcomes and hook order; seven
non-object/nonfinite cases remain outside this adoption.

`DecodeModelToolInput` retains a named Bash binding failure with typed known
arguments, missing-command state and immutable unknown extras. Strict explicit
`DecodeToolInput` still rejects these inputs. Direct HTTP/SSE, history, clone,
masking and stored event reads preserve the input boundary. ToolGate settles the
failure after before/guard/permission/journal checks without calling an executor,
then runs ordinary after/result hooks. A valid before rewrite executes; permission
denial takes precedence. This models the default Source Bash handler's signature.

All 68 finite client/server SDK final objects match native stream output, historical
reads and SQLite close/reopen. A named stored-event test preserves failure and
unknown-parameter order without supplying authority. The explicit native diagnostic
report `docs/audits/provider-bash-binding-native-2026-10-10.json` observes 14 final
SDK differences (client 7, server 7), down from 40. It is not a complete parity gate.
Known-field scalar/null consumer adaptations, root/nonfinite values, usage scalar
alignment, caller-kind scope, default diagnostics and complete G7 remain open.

README canonical Mermaid/provider boundary, review baseline, extension docs and
interactive specification updated together. Archify regeneration passed 9/9 checks,
zero warnings; no new browser visual inspection (earlier access was denied).
Specification SHA256: ecca8ac9149099b87bd90865ecba8d1ab2bb9d15403b922388064d8145d86010.
HTML SHA256: c2ffae9ff35cda96777b43a36e1497dcd2e8c4b6ae0459dcd95fc0dbdf83e6c3.

Validation on local Darwin arm64:

- Source consumer helper: 1 passed, 4 expected SDK warnings.
- Targeted protocol/provider/agent/SQLite tests passed; the newly added archival
  event test initially omitted ordinary emitted metadata defaults, then passed
  after the test row included them. No runtime workaround was made.
- `go test -p=1 -count=1 -timeout=300s ./...`: passed.
- `go test -race -p=1 -count=1 -timeout=300s -coverpkg=./... -coverprofile=/tmp/mini-loop-bash-binding-integrated.cover ./...`: passed, no race report.
- `go vet ./...`: passed. Darwin FSEvents deprecation and existing linker
  LC_DYSYMTAB warnings were emitted; no check failure.
- `go run ./tools/typeaudit -root .`: 424 production files, 1388 named types,
  zero findings.
- Python `verify_scans.py`: 19 anchored guards; `verify_guards.py -k block`:
  3 caught mutations (selected subset, not the full mutation sweep).
- Archify: 9/9 checks; README outline and `git diff --check`: passed.

### Coverage refresh on the deferred Bash binding worktree

Cross-package profile collected with the successful full race run above. Profile
blocks are merged by source location using maximum hits, counting each statement
once. Measured baseline is `cea0e39` plus this Go implementation, before its commit.

| Scope | Covered / total statements | Coverage |
| --- | --- | --- |
| Application (excluding tools) | 31,391 / 35,115 | 89.39% |
| All Go packages | 31,438 / 35,179 | 89.37% |
| protocol | 3,211 / 3,635 | 88.34% |
| provider | 1,357 / 1,573 | 86.27% |
| sqlitestate | 583 / 706 | 82.58% |

Previous comparable application snapshot at `b56975b`: 31,132 / 34,871 (89.28%).
This refresh includes intervening caller/partial-input changes as well as this
binding slice, so its increase is not attributed solely to Bash binding.
This is executed Go statement coverage, not migration completion, mutation
strength, subprocess-internal coverage, native SQLite C coverage or non-Darwin
verification. Full Python regression was not rerun; Python runtime package modules
were unchanged. No merge or push. Goal remains incomplete.

Source export check after all native watcher jobs ended: passed, 215 current files.

## 2026-10-10 actual usage scalar consumers and native discrepancy capture

Runtime baseline: `bf4c866`. This iteration adds Source/test/diagnostic evidence,
not a native usage implementation. Actual SDK direct/SSE + repository metering
produce 34 successful profiles. Native decoding refuses 30; 4 match both event
value/kind and prompt count. Diagnostic test success is not a parity gate.

Direct required counts retain numeric strings, booleans and floats. SSE can
normalize integer-like strings, booleans and integral floats at message_start or
message_delta. Fractional floats remain observable; metering truncates them,
counts bool as int, ignores text/null/structured values, returns no observation
for zero and accepts negative anchors before clamping used(). Required text plus
coerced cache fields gives different actual direct/SSE prompt totals.

Capture includes the actual wire, stage snapshots and _field/prompt_tokens/
TokenMeter.observe hashes. Existing 9 usage metadata profiles remain unchanged.
See [scope, native report and implementation sequence](docs/audits/provider-usage-consumer-2026-10-10.md).
Known counts require named variants and separate observation/metering semantics;
SDK normalization, compaction and strict decision consumers, clone/masking and
historical/SQLite/recording recovery still require implementation and validation.
This evidence does not close G4/G7. Production coverage remains the bf4c866
worktree measurement, application 89.39%; no new measurement is claimed.

Validation for this capture: Source helpers 2 passed (12 SDK warnings); full
provider package tests passed; selected legacy usage race tests passed and the
explicit 34-case diagnostic race replay passed. `verify_scans.py` passed 19
anchored guards; Source export check passed 216 current files; README outline,
`git diff --check` and staged diff check passed. Production modules and runtime
architecture were unchanged; no new full Go/Python suite, coverage refresh,
mutation sweep or architecture regeneration was required or claimed. Existing
bf4c866 full Go/race/vet/type evidence remains the runtime baseline. No push.

## 2026-10-10 named SDK usage observations and native consumers

Baseline: `a5bde56` plus this implementation. `protocol.UsageCount` is an immutable
closed union of integer/float/bool/text/null/array/object with concrete private
payloads and members, not an any/open map/RawMessage domain field. All known root
and nested counters use the named variant; sparse/null flags stay separate.
Native/fake producers use IntegerUsageCount. Typed Integer/Kind inspection is
separate from Source Metered projection. Native bounded integer/finite float
metering range remains an adaptation, not unbounded Python arithmetic parity.

Direct required values retain their observed kind. Optional SDK counts normalize
eligible integer-like scalars. Captured SSE start/delta count validation runs per
stage before accumulation; prior state is not re-normalized by a later delta.
All 34 actual SDK final values/kinds and stage snapshots now match. Actual native
TokenMeter observations/anchors/used context match all 34 Source captures, including
ignored text/structured/null, bool-as-int, truncated fractions, zero and negative
anchors. Normalization remains scoped to these captured cases; full Pydantic
coercion, other invalid field shapes and native range adaptations remain open.

Compaction SummaryReceipt and archival event fields retain observed counters.
DecisionLLM requires integer variants with nonnegative value, refusing text,
float, bool, null, array/object and negative values even when Metered could yield
an integer. Native/fake integer producers and existing Source fixtures keep their
JSON shape. Count text and structured keys/values are masked in model_end and
compaction projections without changing the live observations. Thirty-four actual
Source usage values survive SQLite event close/reopen with untrusted authority.
Previous HTTP/SSE SQL/recording/Manager reconstruction recipes run in the suite;
no new real-provider deployment is claimed.

Canonical README provider label, usage boundary, review baseline, extension docs
and interactive specification updated together. Archify regeneration: 9/9 checks,
no warnings. No fresh browser visual inspection (prior access was denied).
Specification SHA256: 21507811f4431d03313623e046f550f88a0a7b2f40f770642b2282c8f9b7672c.
HTML SHA256: 50ae61bb40f7199d539b82627f70d7a8b1a95644d2abc860e916e858784bddfa.

Initial targeted checks exposed old tests treating Source-accepted negative/text
observations as invalid; these adaptation expectations now use malformed/nonfinite
inputs. An added masking isolation assertion compared escape spellings instead of
values; it now inspects decoded strings and compares before/after live bytes.
The actual numeric/authority guards are tested separately and remain enforced.

Final native diagnostic report: docs/audits/provider-usage-count-native-2026-10-10.json
(baseline a5bde56 plus this adoption) has 34 successes, 0 refusals, 34 event and
34 prompt matches, down from the prior 30 refusals. Independent ordinary parity
tests compare the complete JSON tree's kinds and all 36 SSE stage snapshots;
report generation alone is not the acceptance gate.

Validation on local Darwin arm64:
- Full `go test -p=1 -count=1 -timeout=300s ./...`: passed.
- Full `go test -race -p=1 -count=1 -timeout=300s ./...`: passed; no race report.
- `go vet ./...`: passed; existing Darwin FSEvents/linker warnings remain.
- `go run ./tools/typeaudit -root .`: 425 production files, 1391 named types,
  zero direct open-payload findings.
- Source scanning guards: 19 anchored. Selected `verify_guards.py -k block`:
  3 caught mutations; not a full mutation sweep.
- Explicit diagnostic replay: passed; targeted Source-kind/meter/decision/masking/
  compaction/SQLite checks passed. Archify 9/9, README outline and diff check passed.
- No Python runtime package change; full Python regression/invariant verifier were
  not rerun. No new coverage collection or non-Darwin verification. The previous
  application 89.39% snapshot is bf4c866 evidence, not this changed implementation.
- No merge/push or full-port acceptance claim. Default diagnostics approval,
  remaining provider/type/public-entry scope and complete G7 remain open.

Source export freshness after all watcher suites ended: passed, 216 current files.

## Finite non-mapping provider input adoption — 2026-10-10

Baseline: `f187508` plus finite non-mapping tool input adoption. Source commits
`a59d2d1` and `7748519` separately pin before-hook rewrite/denial order and actual
SDK direct decoding/forwarding. The direct fixture contains 12 client/server
profiles; the Bash consumer fixture has 51 profiles, including 49 finite cases.

`BashRejectedArguments` retains a closed argument kind and immutable finite
observation. Model ingress carries a named non-mapping binding failure; explicit
caller admission remains strict. The existing tool gate settles the failure after
before/guard/permission/journal and before handler invocation. Before-hook rewrite
can replace it, and before denial takes precedence. Clone, masking and history
retain the original root without inventing executable fields. Server input roots
remain inert observations and cannot select client execution. No dependency added.

All 78 finite stopped SDK client/server profiles match content, history and SQLite
reopen. The diagnostic final differences fall from 14 to 4 (two client, two server),
covering nonfinite inputs. Other known-field consumer adaptations, broader provider
scope, default diagnostics approval and complete G7 remain open. Historical reports
and coverage measurements retain their original baseline.

Validation on local Darwin arm64:
- Full ordinary Go suite and full race suite passed; no race report.
- `go vet ./...` passed; existing Darwin linker/FSEvents warnings remain.
- Type audit: 426 production files, 1393 named types, zero direct open-payload findings.
- Source tests: two focused tests passed (nine SDK dependency warnings).
- Scanning guards: 19 anchored; selected `verify_guards.py -k block`: three
  caught mutations. This was not the full mutation sweep.
- Explicit diagnostic replay passed: 98 profiles, four remaining differences.
- Archify regeneration: 9/9 checks, zero warnings; README outline checked.
  Specification SHA256 fb254bc3ac2a4166e9995b9a7f88326d3bbfe7b723be915af111552d906047d9;
  HTML SHA256 9b63910e7897cae2d8721bc93b6d4681214063234b42038dc1a84602c53d1b86.
  No fresh visual inspection because prior browser access was denied.
- Full Python regression and invariant verifier were not rerun: no Python runtime
  package modules changed. No fresh coverage collection; prior application 89.39%
  measurement belongs to bf4c866. No non-Darwin, merge/push or full-port claim.

Source export freshness after all watcher suites ended: passed, 217 current files.
Diff and staged diff checks passed.

## Named model Bash optional-field projections — 2026-10-10

Baseline: `9c01921` plus this implementation; Source contract commit `ffc3265`.
`BashBackgroundRequest` and `BashApprovalProposal` preserve finite observed option
values in immutable closed variants. Bash execution accessors return only a boolean
Python-truth request and an optional string-array proposal. Candidate length, exact
command-prefix match, banned heads and actual human approval remain the existing
agent rules. Invalid proposal shapes are ignored, not converted into grants.

Actual Source Agent/default Bash consumers cover 56 profiles: 15 background request
shapes and 13 proposal shapes, each with background tools enabled and disabled.
Text "false", nonzero numbers and nonempty containers request background execution;
empty text/containers, zero, false and null do not. Disabled background support
executes foreground regardless of the request. Twenty-eight wire samples (27 unique inputs)
retain exact original JSON values and kinds through direct/SSE decoding, clone,
masking, historical message reads and SQLite close/reopen. Native tests execute real
foreground shells/background tasks, compare Source candidate selection, and prove
readonly denial starts no background manager. Explicit DecodeToolInput is strict.

This closes the captured finite Bash optional-field adaptation. Nonfinite inputs,
non-text command values, other default tools, broader SDK/caller/coercion scope,
default diagnostics dependency approval and complete G7 acceptance remain open.

Validation on local Darwin arm64:
- Full `go test -p=1 -count=1 -timeout=300s ./...`: passed.
- Full `go test -race -p=1 -count=1 -timeout=300s ./...`: passed; no race report.
- `go vet ./...`: passed; existing Darwin FSEvents/linker warnings remain.
- Type audit: 427 production files, 1395 named types, zero direct open-payload findings.
- Focused Source consumer test: one passed. Source effect seams are explicit
  foreground/background probes; native consumer tests run real shells/tasks.
- Scans: 19 anchored; selected `verify_guards.py -k block`: three caught mutations,
  not a full mutation sweep. Targeted protocol/provider/agent/SQLite suites passed.
- The first direct-ingress test body omitted required completed-message envelope
  fields. The fixture envelope was corrected; runtime guards stayed intact.
- Archify: 9/9 checks, zero warnings; README outline reviewed.
  Specification SHA256 61fdcc1770d19d9240bcde4d08dc37cbbdd37f817f2c018d81385d6cb6f816bb;
  HTML SHA256 9a3cfc70d06fadd9676b46db3d46a96fbf1bb16a0f75d0a1f0dfc9e2e712c66e.
  No fresh browser visual inspection (earlier access denied).
- No Python runtime package modules changed; full Python regression/invariant
  verifier were not rerun. No fresh coverage: the previous 89.39% application
  snapshot belongs to bf4c866. No non-Darwin verification, merge or push.

Source export freshness after all native watcher suites ended: passed, 218 current files.
Diff and staged diff checks passed.

## Named non-text Bash command consumers — 2026-10-10

Baseline: `400b6b0` plus this implementation. Source contract commit `d12d58b`
pins actual Agent, default _bash, real Toolset/background consumption, candidate
functions and PermissionHook. Thirty-six consumption profiles and twenty permission
profiles cover finite null/bool/integer/float/array/object commands, foreground and
background activation, extra-keyword priority, before rewrites, before/guard denial,
readonly and immutable deny-list behavior. No invalid command starts a process/task.

BashCommandObservation owns a closed BashCommandKind and immutable finite value.
Permission and approval use Python str(value) as a policy-only projection. Native
handlers return named BashCommandFailure with split/lower stage: foreground split;
background auto selection lower; explicit background initializes the owned manager
then fails split. Original JSON stays in model/event/history records. Explicit
caller admission remains strict, and the existing gate still controls permission,
journal/replay, after hooks and result observation. Rejected command observations
are never shell argv. Before rewrite can replace them with executable text.

Protocol clone/masking/history and native direct/SSE/SQLite reopen cover 44 unique
inputs: 27 optional-field values and 17 non-text command objects. The prior options
inventory had 28 samples including the repeated absent-options object; uniqueness
is now enforced. Earlier statements of 28 distinct inputs are corrected to 28
samples (27 unique). Source authority decisions remain actual captured behavior.
Nonfinite inputs, other default tools, broader SDK/caller/coercion scope, default
syntax diagnostics dependency approval and full G7 acceptance remain open.

Validation on local Darwin arm64:
- Full ordinary Go suite: initial run found one legacy test rejecting numeric
  command observations in stored events. That assertion now rejects an invalid
  name type, and 36 positive event round trips preserve observed commands. Targeted
  repair passed; full `go test -p=1 -count=1 -timeout=300s ./...` retry passed.
- Full `go test -race -p=1 -count=1 -timeout=300s ./...`: passed; no race report.
- `go vet ./...`: passed; existing Darwin FSEvents/linker warnings remain.
- Type audit: 428 production files, 1400 named types, zero direct open-payload findings.
- Focused Source pytest: one passed; consumers use real Toolset/background code.
  The initial permission probe lacked agent.state, so it selected interactive mode;
  the probe now supplies actual agent state. Python runtime modules are unchanged.
- Scans: 19 anchored. Selected `verify_guards.py -k readonly`: five caught mutations,
  not a full mutation sweep. Native consumer/permission/protocol/provider/SQLite
  checks passed; 44 unique direct/SSE/reopen inputs preserve JSON kinds and values.
- Archify regeneration: 9/9 checks, zero warnings; README outline reviewed.
  Specification SHA256 ee939347fa22c61c8d58d40c25fd9cd6b5a6614c6b05f83aac697ffbc4edf8f3;
  HTML SHA256 2895a635a21b01141b8080b22ef56820103ff079abba266e678f541ad7740a66.
  No fresh visual inspection (prior browser access denied).
- No Python runtime package change; full Python regression/invariant verifier
  were not rerun. No new coverage collection, non-Darwin verification, merge/push
  or full-port acceptance claim. Last 89.39% application coverage is bf4c866 evidence.

Source export freshness after native watcher suites ended: passed, 219 current files.
Diff and staged diff checks passed.

Next observed gap: non-text command display labels. Python activity.tool_label
uses str(command), while Go agent.ToolLabel currently reads the executable
BashInput.Command shadow (empty for rejected observations). Error/permission/input
parity above does not prove complete event-display parity. Capture Source display
fields and align this observation-only consumer before accepting the full slice.

## Non-text Bash display alignment — 2026-10-10

Baseline: `9f0e9db` plus this implementation; Source display contract `fdb47d8`.
ToolLabel now reads ShellCommand's observation text for Bash, preserving actual
Python str(command) instead of the executable string shadow. Thirty-six real
Source Agent tool_use display fields and twelve independent Source SecretRegistry
mask_payload/tool_label profiles match native display. Cases cover null/bool/int/
float, nested arrays/objects, quotes, Unicode, opaque text, secret-key collisions,
mask-before-preview and the 60-character cap. Existing typed-text activity tests
still pass. Neither display nor policy text becomes argv or supplies authority.

The actual maskedEvent path recomputes display from a masked immutable input copy.
Native checks compare labels and canonical JSON values/kinds and prove that the live
input remains unchanged. Initial comparisons confused HTML/Unicode JSON escaping
with value changes; canonical comparison fixes the test without changing codecs.
No new production type or dependency. The previous recorded label gap is resolved.
Nonfinite inputs, other default tools, broader SDK/caller/default scope and complete
G7 acceptance remain open; this is not complete event/provider acceptance.

Validation on local Darwin arm64:
- Full `go test -p=1 -count=1 -timeout=300s ./...`: passed.
- Full `go test -race -p=1 -count=1 -timeout=300s -coverpkg=./... -coverprofile=/tmp/mini-loop-bash-display-integrated.cover ./...`: passed; no race report.
- `go vet ./...`: passed; existing Darwin FSEvents/linker warnings remain.
- Type audit: 428 production files, 1400 named types, zero direct open-payload findings.
- Focused Source pytest: one passed. Native display/activity/masking checks passed.
- Scans: 19 anchored. Selected `verify_guards.py -k secret`: two caught mutations,
  not a full mutation sweep.
- Archify: 9/9 checks, zero warnings; README outline reviewed.
  Specification SHA256 186e36575b751a44ff7ee4afd75b3ff7fb102db33f2eace2a4d134d7b7b9757f;
  HTML SHA256 c9d33da7eaff9465d40ad153d2a92824385bd95910b01c9aa1f353f924bee942.
  No fresh browser visual inspection (earlier access denied).
- No Python runtime package change; full Python regression/invariant verifier
  were not rerun. No non-Darwin verification, merge, push or full-port claim.

### Coverage refresh on the Bash display worktree

Successful full race run with `-coverpkg=./...` and
`-coverprofile=/tmp/mini-loop-bash-display-integrated.cover`. Identical source
locations merge with maximum hits, asserting identical statement counts. The
1,186,287 raw blocks reduce to 24,713 unique blocks; zero-statement packages have
no percentage. Profile SHA256: 3d2e0a3ca57c9299224ffba4322e064521f81e1e0b18789f080fd36eb54f2855.
[Package metrics](docs/audits/go-coverage-bash-display-2026-10-10.json) pin baseline
`9f0e9db` plus this display implementation and Source contract `fdb47d8`.

| Scope | Covered / total statements | Coverage |
| --- | --- | --- |
| Application excluding tools | 31,628 / 35,408 | 89.32% |
| All Go | 31,675 / 35,472 | 89.30% |
| agent | 9,023 / 10,023 | 90.02% |
| protocol | 3,435 / 3,910 | 87.85% |
| provider | 1,359 / 1,577 | 86.18% |
| sqlitestate | 583 / 706 | 82.58% |

Comparable previous application snapshot at bf4c866: 31,391/35,115 (89.39%).
Covered statements increased by 237, total by 293; overall rate fell by roughly
0.07 percentage points. This includes usage, command and optional-field changes
since that snapshot, not solely the current display fix. Darwin build-tag
statement coverage excludes CLI child-process internals and native C; it is not
migration completion, branch/mutation strength or non-Darwin validation.

Source export observation: the first full `export_go_contracts.py --check`
reported only `python-watchfiles.json` stale, after native Go suites had ended.
An isolated actual watchfiles capture then matched the committed fixture exactly.
The fixture was not rewritten; this records intermittent capture drift rather
than claiming that the initial check passed.
The full Source export retry passed: 219 current files, with native Go watcher
jobs terminal. Initial drift and the matching isolated observation above remain
explicit. Working-tree and staged `git diff --check` passed.

## Background command display alignment — 2026-10-10

Baseline: `fbf75c1` plus this implementation; Source contract `2c3c027`.
Actual Python tool_label selects the same shell rules for bash/background_run.
Native ToolLabel now selects both named inputs through ShellCommand. Fourteen
actual Source masked string-command profiles cover empty/simple heads, Unicode
space, pipes/substitutions/backticks/redirects/chains/newlines, quotes, Unicode
preview limits and secret masking before the cap. The existing lifecycle capture
is otherwise unchanged; the native test failed against the previous generic call
label and passes after the shared selection. The actual maskedEvent path preserves
live executable input and emits no registered secret. No new dependency/type,
execution route or tool catalogue change. Non-text background_run consumers and
complete default/provider/G7 acceptance remain open. Prior coverage is pinned to
the Bash display worktree; no coverage refresh is claimed for this slice.

Focused validation: actual Source lifecycle regeneration captured 14 new profiles
and asserted every prior fixture field unchanged. Native BackgroundDisplay plus
existing BashDisplay/ActivityContracts tests passed after reproducing the old
label mismatch. `go vet ./...` passed; type audit: 428 production files, 1400
named types, zero findings. Scans: 19 anchored; selected `verify_guards.py -k secret`:
two caught mutations, not the full mutation sweep. README outline reviewed.
Archify regenerated HTML from JSON: showcase 9/9, zero warnings; specification
SHA256 b57db989c63ac455243ab591544bff7f131a79365a3ef34573b375e1a1967673,
HTML SHA256 1a25d705d2544d211b756a0850683959675397c4b1dc6d0442ea514f9c6634dc.
No fresh browser visual inspection (earlier access denied). No Python runtime
package changes; full Python regression/invariant verifier were not rerun.
No non-Darwin execution, merge, push or complete-port acceptance claim.

Full race `go test -race -p=1 -count=1 -timeout=300s ./...` passed with no race
report. Initial full ordinary run failed only in cmd/miniloop native reload
readiness and reload native event recipes (hidden/lower-mtime/rename/directory).
It overlapped the race run. The code was kept frozen; ordinary validation was
restarted sequentially after race completion. This overlap is an observed
condition, not proof of the failure's cause. Existing Darwin FSEvents deprecation
and linker warnings remain. No fixture or watcher implementation was weakened.

Sequential full ordinary `go test -p=1 -count=1 -timeout=300s ./...` failed:
cmd/miniloop passed (25.433s), while reload native recipes failed symlink-target-write
(`got [linked.go] want []`) and removed-source (`context deadline exceeded`).
All other packages passed. This remains an unresolved native observation/reliability
gate; passing race does not supersede it. The changed runtime path is ToolLabel;
go/reload, cmd/miniloop and python-watchfiles fixture have no diff. No listener/
watcher implementation or expectation was changed to hide this result.
Source export check after both full runs terminated passed: 219 current files.
Working-tree and staged diff checks pass. This slice adds no Python runtime module;
full Python regression remains unrefreshed. Final G3/G7 acceptance stays open.

## Native Darwin callback admission — 2026-10-10

Baseline: `45d086b` plus this implementation. notify 8.0.0 callback_impl fences
recursive event paths against subscription roots; translate_flags ignores
HistoryDone and emits no change for item-type/OwnEvent/ID-wrap flags alone.
NativeEvents now owns the detached canonical root list and applies the same
lexical root/change admission before the existing filename filter. Subscribed
loss remains a sticky error (a stronger documented native adaptation). No
leaf stat resolution, wider roots, new dependency, default selection or global
FSEvents purge. Three independent observer-boundary tests failed with the former
raw forwarding and pass with admission; cancellation retains later real deletion.

Actual Python native recipes repeated three times: all 48 observations matched
the committed fixture. Native raw diagnostic runs before this change also matched
all sixteen recipes in three rounds. Earlier timeout/symlink failures are thus
not causally explained by this patch and must remain on G3/G7's reliability queue.
[Source pins and repeated observations](docs/audits/go-fsevents-boundary-2026-10-10.json)
retain exact source hashes and proof limits. Source fixtures/expectations and
runtime timers were not changed; no coverage refresh is claimed for this slice.

The pinned notify run method uses continuous CFRunLoopRun. Native Go's existing
50ms owner-thread slices now service all pending sources for the slice rather
than return after the first handled source; Stop is still checked between slices.
This restores continuous servicing within the existing bounded cleanup design.
It does not enlarge batch/context timers or claim a proven cause for the old
intermittent failures. Event class deduplication/complete temporal equivalence
remain separate acceptance questions.

Validation checkpoints before final isolated runs:
- Native reload package before the continuous-service correction passed three
  whole-package rounds (113.159s); boundary/cancellation/Close tests after the
  correction passed (1.407s). Linux amd64 compile passed, no Linux runtime claim.
- Full ordinary before the service correction failed late readiness-probe delivery
  in the real Cgo compiler test and a hidden-parent empty batch. It overlapped
  the full Python mutation copier; overlap is not a proven cause. Previous failures
  remain recorded. Final ordinary/race runs are isolated and sequential.
- Full Python mutation verifier: 378/378 caught, each with a passing baseline;
  this covers registered Python guards, not the new Go/C callback boundary.
- Scans: all 19 anchored. Type audit: 428 production files, 1400 named types,
  zero findings. No Python package/runtime change; full Python regression and
  package invariant verifier were not rerun.
- Archify regenerated HTML from JSON: showcase 9/9, zero warnings.
  Specification SHA256 a04fd8eb43a76b12e2cd83093b27032a789f4f4f172831f232a966ba7974fe02;
  HTML SHA256 465a635749243471224f221fa2ed507dd10278042e5a033400b07f3d91b9557b.
  README outline reviewed; no fresh browser visual inspection (earlier access
  denied). No merge/push, paid provider check or complete-port acceptance claim.

Final isolated ordinary run after the loop correction still failed one native
recipe: hidden-parent-source timed out (3.35s); all other packages passed, including
actual CLI reload and the Cgo workspace test. Neither overlapping verification nor
single-source loop yielding fully explains the observed reliability gap. This
patch repairs independently proven Source admission/servicing differences; it does
not close the native timing queue or claim a green ordinary full-suite gate.

Full isolated race `go test -race -p=1 -count=1 -timeout=300s ./...` passed with no
race report. Final `go vet ./...` passed. Existing Darwin FSEvents deprecation
and linker warnings remain. The ordinary failure above remains authoritative;
passing race is not a substitute. Source evidence is committed at `b998ba9`.
No final native reliability or complete migration acceptance is claimed.

Source freshness check after all native jobs terminated failed only
python-watchfiles.json. This confirms that fixed native-event expectations can
drift in actual Python capture as well; a read-only full snapshot comparison is
collecting the exact difference. The committed fixture was not changed, and this
check is not reported as a pass. Other full exported files were current.

Read-only full snapshot retry completed: `_snapshot()` produced 219 files and the
same byte-for-byte TARGET comparison used by --check found zero stale files.
No fixture was written. The first failed --check snapshot was not retained, so
its exact event difference remains unknown; current equality does not erase that
failure or prove capture determinism. Stop optional repeats after this freshness
observation. Current ordinary native hidden-parent timeout remains unresolved.
Working-tree/staged diff checks pass. No merge or push performed.

## Retain failed observer captures — 2026-10-10

Baseline: `5656802` plus diagnostics. Full export --check now reports bounded
unified differences (80 lines per stale file) and both SHA256 values from the
same captured snapshot. Explicit --diagnostics-dir retains exact expected/actual
bytes and a manifest in a fresh child directory; successful checks create none.
Invalid expected UTF-8 remains preserved exactly and uses replacement only for
the display diff. The check never rewrites TARGET and never samples a replacement
observation to explain failure. Normal export behavior remains unchanged.

Five focused Python tests cover one capture/no target rewrite, missing fixtures,
bounded text/corrupt bytes, success without output creation and flag admission.
Native recipe tests now retain a mutex-protected, 8 KiB capped failure timeline
with setup/operation phases and accepted raw paths/flags, closing the owned
observer before reading logs. Existing timers, operations and expectations remain.
Three native recipe rounds passed. No Go runtime/API change, new dependency or
open domain type. This diagnostic work does not resolve G3/G7 reliability.

The actual full --check failed with one changed Source profile:
symlink-target-write expected [] but observed [linked.py]; 218 other exports were
current. [Exact captured profile and hashes](docs/audits/go-watchfiles-drift-2026-10-10.json)
retain that failed observation. Expected SHA256 fe1e0247ada37a2497f0969cf9446ef5e4209a1624325adf4c757df673c2e4e0;
actual SHA256 00c60c7690aac36bf9e86d647a86b04bfcd5ae6ad58578e5cce36cd563042743.
This matches a previously failing native profile, but does not prove event origin
or readiness causality. No fixture widening or additional freshness retry.

Validation: five Source diagnostics tests passed; selected new export-diagnostics
mutation caught (passing baseline, failed mutated test); Go vet passed. Full ordinary
Go failed hidden-parent-source and symlink-target-write; new bounded timelines show
setup directory/symlink Created events arriving after the 200ms drain timeout.
The operation timeline is retained below. This proves the timeout did not establish
setup completion, not that a runtime first-batch expectation can be weakened.
Source --check's failed bytes were retained; no freshness retry. Full Python and
invariant verifier were not rerun (no Python runtime package change). Previous
full race/type audit remains pinned to 5656802; no fresh full race claimed here.

```text
--- FAIL: TestNativeEventsActualSourceRecipes (21.13s)
    --- FAIL: TestNativeEventsActualSourceRecipes/hidden-parent-source (2.01s)
        native_events_recipes_test.go:203: got [] want [.git/a.go]
        native_events_recipes_test.go:141: native recipe timeline:
            +87ms subscription constructed; drain setup events
            +289ms setup next: paths=[] error=context deadline exceeded
            +289ms operation=add path=/private/var/folders/_m/0dybd23x1wv_m665n47v56bc0000gn/T/TestNativeEventsActualSourceRecipeshidden-parent-source3339926772/001/.git/a.go
            +1.93s reload native event: path="/private/var/folders/_m/0dybd23x1wv_m665n47v56bc0000gn/T/TestNativeEventsActualSourceRecipeshidden-parent-source3339926772/001" flags=164096
            +1.93s reload native event: path="/private/var/folders/_m/0dybd23x1wv_m665n47v56bc0000gn/T/TestNativeEventsActualSourceRecipeshidden-parent-source3339926772/001/.git" flags=164096
            +1.98s operation next: paths=[] error=<nil>

    --- FAIL: TestNativeEventsActualSourceRecipes/symlink-target-write (0.64s)
        native_events_recipes_test.go:203: got [linked.go] want []
        native_events_recipes_test.go:141: native recipe timeline:
            +198ms subscription constructed; drain setup events
            +399ms setup next: paths=[] error=context deadline exceeded
            +399ms operation=write path=/private/var/folders/_m/0dybd23x1wv_m665n47v56bc0000gn/T/TestNativeEventsActualSourceRecipessymlink-target-write2476421173/001/linked.go
            +559ms reload native event: path="/private/var/folders/_m/0dybd23x1wv_m665n47v56bc0000gn/T/TestNativeEventsActualSourceRecipessymlink-target-write2476421173/001" flags=164096
            +559ms reload native event: path="/private/var/folders/_m/0dybd23x1wv_m665n47v56bc0000gn/T/TestNativeEventsActualSourceRecipessymlink-target-write2476421173/001/linked.go" flags=295168
            +559ms reload native event: path="/private/var/folders/_m/0dybd23x1wv_m665n47v56bc0000gn/T/TestNativeEventsActualSourceRecipessymlink-target-write2476421173/001/notes.txt" flags=103680
            +609ms operation next: paths=[/private/var/folders/_m/0dybd23x1wv_m665n47v56bc0000gn/T/TestNativeEventsActualSourceRecipessymlink-target-write2476421173/001/linked.go] error=<nil>

FAIL
```

## Calibrate observer recipe readiness — 2026-10-10

Baseline: `cf808fe` plus this calibration. Captured timelines show initial setup
Created events arriving after a quiet drain timeout. Both Source _native_case and
native Go recipe tests now observe a file created after subscription construction
before arming the actual operation. The inert valid probe remains until observer
close, and remaining setup batches are drained. Go reuses the existing native
fixture readiness helper; Python uses its actual WatchFilesReload should_restart.
A quiet timeout alone is not treated as kernel readiness.

The recipe operations, three-second operation timeout, expected batches, backend
flags and native runtime remain unchanged. Python's complete calibrated native/
polling capture matched the existing python-watchfiles fixture exactly. No fixture
was regenerated or widened. This repairs a proved capture precondition; runtime
handoff reliability and complete temporal equivalence still require their own
scope audit. No dependency, domain/API change or coverage refresh.

Fresh validation: native actual-Source recipes passed three consecutive rounds
(`/tmp/mini-loop-native-calibrated.log`). Full ordinary Go tests and full race
with cross-package coverage passed sequentially; `go vet ./...` passed. Type
audit: 428 production files, 1,400 named types, zero findings. Source scanner
verification: all 19 anchored; export-diagnostic tests: five passed. Full Python
regression and non-Darwin gates were not rerun; no merge or push. Coverage is
recorded separately to preserve a focused calibration commit.

Full Source export --check passed: 219 current files, after all native Go jobs
ended. No diagnostics directory was needed and no fixture was rewritten.

## Coverage refresh after observer readiness calibration — 2026-10-10

Measured baseline: `e3fc640`. Full ordinary Go tests passed, followed by
`go test -race -p=1 -count=1 -timeout=300s -coverpkg=./...
-coverprofile=/tmp/mini-loop-calibrated-integrated.cover ./...` (passed).
[Package metrics](docs/audits/go-coverage-observer-readiness-2026-10-10.json)
record the exact profile hash and source-location aggregation.

| Scope | Covered / total statements | Coverage |
| --- | --- | --- |
| Application excluding tools | 31,644 / 35,418 | 89.34% |
| All Go | 31,691 / 35,482 | 89.32% |

The 1,186,527 raw blocks merge to 24,718 unique source locations using maximum
hit counts with consistent statement counts. Compared with the previous Bash
display snapshot, covered statements increased by 16 and total statements by 10;
application coverage increased approximately 0.02 percentage points. This is
Darwin arm64 statement coverage; native C and CLI subprocess internals are not
instrumented. Python coverage, branch coverage and migration acceptance are not
measured. Source export: 219 current files; vet passed, type audit zero findings,
19 scanner guards anchored and five export diagnostic tests passed. Full Python
regression and non-Darwin gates were not rerun; no merge or push.

## Direct background argument Source capture — 2026-10-10

Baseline: `957b73a`; actual Source Agent._exec_tool, installed background_run,
Tool.run, BackgroundManager.run and looks_dangerous source hashes are captured.
Eighteen recipes cover finite non-text command values, missing/extra/ordered-extra
parameters, non-mapping roots, before/guard denial and argument rewriting.
The source contract is `go/testdata/python-provider-background-input.json`.

All eight non-text commands pass before/guard, lazily initialize the actual
BackgroundManager, then fail its `.split()` consumer before counter/task creation.
Missing/extra/non-mapping invocation errors retain after/result hooks while leaving
background uninitialized. Before denial skips guard/after; guard denial skips
after. A before rewrite to a real inert printf command starts bg_0001, and the
actual task is cancelled/joined by manager.close. No Source handler or executor is
mocked. The probe observes actual state instead of treating admission as execution.

[Native admission audit](docs/audits/go-background-input-drift-2026-10-10.json)
retains all 18 differences: DecodeModelToolInput rejects before these consumers,
including cases that Source hooks can rewrite or deny. This is a pinned diagnostic,
not a passing parity assertion. The next implementation slice must model observed
command and binding failures with named variants, preserve mask/display/archive
observations, and settle binding failures after the common before/guard/permission
gate; command consumer failure must preserve actual manager initialization without
creating a task. Strict explicit API admission remains a separately audited seam.
No runtime implementation, dependency, default or architecture topology changed.

Validation: full Source export --check passed, 220 current files. Existing native
Background/BashBinding tests in agent and protocol passed. All 19 scanner guards
remain anchored; working-tree diff check passed. Native diagnostic rejected all
18 cases as recorded. No Go/Python runtime module changed; full Go/Python suites,
invariants, full mutation sweep and coverage were not rerun for this capture slice.
The preceding e3fc640 full ordinary/race/vet and coverage gates retain their own
baseline. No merge or push; full migration goal remains incomplete.

## Named direct background argument consumers — 2026-10-11

Baseline: `d632a6e` plus this implementation, using the 18 actual Source recipes.
BackgroundBindingFailure and private backgroundModelArguments retain invocation
stage observations separately from executable BackgroundRunInput. Finite command
observations reuse the common named split-failure projection; closed immutable
extras remain inert unknown parameters. Strict DecodeToolInput admission stays
separate. Before hooks may rewrite or deny; guards/permission/journal admission
precede binding-failure settlement in the single ToolGate. Command split failure
occurs after lazy scoped-manager initialization and before task allocation.

All 18 actual Source cases compare exact output, masked display, final input,
before/guard/after/result trace and failed/denied outcomes. Tests additionally
start the next real inert background command and assert its ID, proving invalid
commands did not advance task allocation. Named cloning/masking, canonical sorted
step identity and stored-message restoration preserve root/command kind, unknown
parameter order and explicit known-option nulls without mutating live inputs.
Nonfinite observations refuse admission. Broader timeout/approval-prefix coercion,
other default-tool consumers and complete G3/G4/G7 acceptance remain open.

Architecture Mermaid and boundary text reviewed; Archify regenerated HTML from
the JSON specification with showcase 9/9, zero errors/warnings. Specification
SHA256 dea510a34d205f8f0b01652595e804794d5fce48f678099afaf85eafca7a8225;
HTML SHA256 2ec2fc5974f68e39453ad46a96f5e6e3a44c7d010f5dfb3becc84165b313a324.
No fresh visual inspection (earlier browser access denied). No dependencies,
Python runtime modules, fixture expectations or feature activation defaults changed.
No new coverage measurement; e3fc640's 89.34% application coverage remains pinned
to that prior baseline. No merge, push or complete-port acceptance claim.

Fresh validation: focused BackgroundModel/BackgroundDisplay/BashBinding tests
passed; sequential full Go ordinary and race suites passed; vet passed. Type audit: 429 production files / 1,402 named types / zero
findings. Selected Source guard mutation tool-input-unmasked was caught (passing
baseline, failed test call); this is one Source mutation, not a full mutation sweep
or a native Go mutation claim. Full Python regression, package invariants and
scanner verification were not rerun: no Python package or scanner targets changed.

Full Source export --check passed after all native Go jobs and the selected
mutation completed: 220 current files. No fixture or diagnostics output was
rewritten. Working-tree diff check passed.

## Missing model tool Source capture — 2026-10-11

Baseline: `5cfb518`. Actual Source Agent._exec_tool and PermissionHook capture
32 missing-tool recipes across all three real modes (interactive/auto/readonly),
eight finite argument shapes, empty/case-sensitive/whitespace/Unicode tool names,
and before rewrite/denial, guard denial and after replacement. Unknown calls
retain before/guard/after/result order, fail with `Unknown tool: NAME`, and produce
no permission requests even in readonly mode. Input rewrites preserve unknown
lookup; denials retain precedence. This is actual installed Source dispatch,
not a mocked missing-tool executor. The fixture is
`go/testdata/python-provider-unknown-tool.json`; source hashes are retained.
Native model admission at the baseline rejects unsupported names before this
boundary. The next implementation must preserve inert finite observations in a
named variant; custom handlers must still require a concrete native input codec.
Nonfinite inputs, unknown MCP root shapes and registered extension input codecs
remain separate audit scope. No Python runtime/default or dependency changed.

Validation: full Source export --check passed, 221 current files, with native Go
watcher jobs terminal. Every capture uses the three real Source permission modes;
the earlier standard-mode capture was replaced before delivery because Source
normalizes unknown mode strings to interactive. No runtime parity implementation
is claimed by this capture commit. Full Python regression and coverage were not
refreshed; no merge or push.

Scanner verification passed: all 19 anchored. Working-tree diff check passed.

## Inert missing-model-tool observations — 2026-10-11

Baseline: `47b44de` plus this implementation. UnsupportedToolNameError is a named
private admission distinction; only absent native codecs take the model-observation
branch. Known malformed input never silently downgrades. UnknownModelToolObservation
retains finite immutable roots, with native wire/name bounds and UTF-8 admission,
through model decoding, cloned/masked metadata, canonical action identity and
stored-message restoration. Explicit DecodeToolInput still rejects unknown names.
The common gate preserves missing catalogue lookup after hooks/guards/permission/
journal and produces ordinary failed after/result outcomes. A schema without a
concrete native codec cannot receive this observation as executable handler input.

All 32 actual Source cases match output, final input, display, hook order and
failed/denied flags across interactive/auto/readonly modes. No permission request
or handler call occurs for an unregistered name. A separate custom-schema test
proves the inert variant cannot execute a registered handler. Native direct and
SSE decoding preserve all 32 recipe names and root kinds, including empty names;
these wire tests are native transport checks, not a new complete SDK field audit.
The initial wire test omitted SSE event headers and was corrected to standard
named frames; runtime SSE admission was not changed. Finite/type/history tests
also prove known decoder errors and nonfinite/malformed JSON do not fall back.
Registered extension codec design, unknown MCP root shapes, malformed name-field
SDK defaults, nonfinite inputs and complete G0/G4/G7 reconciliation remain open.

README canonical Mermaid and boundary text reviewed; architecture HTML regenerated
from JSON using Archify showcase 9/9, zero errors/warnings. Specification SHA256
 d4476554b43dbb4a2e65169d26377b3a6f74ac25ce0e3f4e2cbf4c752e19072f;
HTML SHA256 9e14f5da2703a0a7387d66a5f1914908514402c214c2d9b04d03477c5552a044.
No fresh browser visual inspection (earlier access denied). No new dependencies,
feature activation, executable tool catalogue or Python runtime module changes.
Coverage was not refreshed; e3fc640's 89.34% application measurement retains
that baseline. No merge, push or complete migration acceptance claim.

First full ordinary gate failed in two packages: agent's prior archival rejection
of name=future (now deliberately supported inert metadata), and reload's
removed-source operation timeout after confirmed probe readiness. The archival
assertion was replaced by a positive inert stored-event test; malformed names and
unknown event kinds still refuse. Focused agent/protocol/provider tests passed.
[Exact native failure](docs/audits/go-native-remove-timeout-2026-10-11.json)
retains the complete admitted-event timeline and hashes of unchanged watcher
sources/fixture. Readiness confirmation alone does not prove reliable subsequent
mutation delivery; no raw callback absence or cause is claimed. No timeout,
watcher implementation or fixture expectation was changed. A second full ordinary
gate was started after the archive test correction, with code frozen. Native
G3/G7 temporal acceptance stays open even if that run passes.

Second full ordinary gate failed only in unchanged reload native recipes:
symlink-target-write and lower-mtime-write timed out after acknowledged readiness.
Agent/protocol/provider and every other package passed after the archive correction.
Both full failures are retained in the native timeout audit. A progress statement
that reload had passed was corrected when the terminal failure was inspected;
package completion alone is not success evidence. No further ordinary freshness
retry is substituted for these failures. Full race was started sequentially with
code frozen. Final native temporal acceptance remains open.

Full race gate passed sequentially, including reload (38.775s); no data race was
reported. This does not supersede the two full ordinary failures retained above.
Vet passed; type audit: 430 production files / 1,404 named types / zero findings.
Selected Source mutation missing-tool-parks-for-approval was caught with passing
baseline and failed test call (one mutation, not a full sweep). Scanner verification
at Source capture passed all 19 anchored guards. Python full regression, package
invariants, coverage and non-Darwin execution were not rerun; no Python runtime
module or scanner target changed in this implementation slice. Runtime watcher
reliability is still unaccepted. No merge or push.

Final Source export --check passed after all native jobs and the selected guard
mutation completed: 221 current files. No fixture expectations or watcher runtime
were changed. Working-tree diff check passed. The unknown-model feature is scoped
and verified; full ordinary Go validation remains failed in native reload, so no
complete migration, complete release gate or native reliability claim follows.

## Coverage refresh after unknown model tool delivery — 2026-10-11

Measured runtime baseline: `f2cdbca`. The full sequential race coverage gate
passed: `go test -race -p=1 -count=1 -timeout=300s -coverpkg=./...
-coverprofile=/tmp/mini-loop-f2cdbca-integrated.cover ./...`.
[Package metrics](docs/audits/go-coverage-unknown-model-2026-10-11.json)
retain the profile hash and exact source-location aggregation.

| Scope | Covered / total statements | Coverage |
| --- | --- | --- |
| Application excluding tools | 31,807 / 35,601 | 89.34% |
| All Go | 31,854 / 35,665 | 89.31% |

The 1,193,343 raw blocks merge into 24,860 unique source locations using
maximum hit counts, with matching statement counts asserted. Relative to
`e3fc640`, application covered statements increased by 163 and total statements
by 183; the exact percentage decreased approximately 0.0014 percentage points.
This is Darwin arm64 Go statement coverage. Native C and CLI subprocess
internals are not instrumented; Python coverage, branch coverage and migration
completion are not measured.

Reload passed this race coverage run in 39.542 seconds. This does not supersede
the two preceding full ordinary native reload failures retained in
[the timeout audit](docs/audits/go-native-remove-timeout-2026-10-11.json).
Native temporal acceptance remains open. No new full ordinary, vet, type audit,
Source export, Python regression, mutation sweep or non-Darwin gate was run in
this documentation iteration. The prior 221-file Source export and 430-file /
1,404-named-type audit retain their original validation baseline. Runtime source,
tests and fixtures were frozen throughout the coverage gate.

README canonical architecture map reviewed: no topology, ownership, authority,
flow, persistence or default changed. Only its visible review baseline was
advanced; interactive specification and generated HTML are unchanged.
Validation for this documentation slice: full race coverage gate, JSON metrics
consistency assertions and `git diff --check`. No merge or push.

## Continuous Darwin owner lifetime — 2026-10-11

Baseline: `0027a97` plus this implementation. Pinned notify 8 runs a fresh
owner thread in CFRunLoopRun; the prior Go implementation re-entered 50ms slices
and returned its owner OS thread to the Go pool. Darwin now runs continuously
on a locked thread that retires at goroutine exit. An opaque C MiniLoopEvents
owns the stream, loop and version-zero stop source. Close signals that source
and wakes the loop; its callback stops on the owner thread, retaining an early
signal before loop entry. The signalling call is joined before C objects are
released. Unexpected loop return fails the reader, with caller Close still owed.

The stop protocol follows Apple's [custom input-source signalling guidance](https://developer.apple.com/library/archive/documentation/Cocoa/Conceptual/Multithreading/RunLoopManagement/RunLoopManagement.html).
Callback admission, roots, batch/context deadlines and all Source recipe fixtures
are unchanged. No global device purge or dependencies added. This is Source
lifecycle alignment, not a claim to resolve the historical timeout cause.

[Exact audit](docs/audits/go-fsevents-continuous-owner-2026-10-11.json) pins hashes
of the implementation and unchanged recipe/fixture plus the complete ordinary
failure. Gates run sequentially with source/tests/fixtures frozen:

- Existing close/cancellation test, 20 repetitions: passed.
- Focused race lifecycle tests: passed, including 32 immediate-close subscriptions
  with four simultaneous Close callers each and final registry cleanup.
- Actual Source recipes, three repetitions (48 scenarios): passed.
- Full ordinary `go test -p=1 -count=1 -timeout=300s ./...`: **failed only reload**.
  Added-source timed out after acknowledged readiness: +1.775s probe observed,
  +1.977s add, +4.980s operation deadline, no admitted post-operation diagnostic.
  This does not prove whether raw callbacks were absent or rejected.
- Full `go test -race -p=1 -count=1 -timeout=300s ./...`: passed;
  reload 38.729s. It does not supersede the ordinary failure.
- `go vet ./...`: passed, existing FSEvents API deprecation warning retained.
- Type audit: 430 production files / 1,404 package-level types / zero findings.

README canonical Mermaid/boundary/review baseline and architecture specification
updated together; generated HTML delivered via Archify showcase 9/9, zero errors
and warnings. Specification SHA256
82ad1b788a52e6f3f6ebc88c1caf53b6e697c638c3394de87eb0a5c8593ec2dd;
HTML SHA256 a2a4c735a808e4efa3b7086ef043579edd0a223ace7a6657d8b4d03641b57c5c.
No fresh browser visual inspection. Native temporal acceptance remains open;
the next diagnosis must distinguish raw callback absence from admission rejection.
No coverage refresh: f2cdbca's 89.34% application measurement keeps its baseline.
Python runtime modules and scanner/guard targets were unchanged; full Python,
mutation sweep and non-Darwin gates were not rerun. No merge or push.

Final Source export --check passed after all native Go jobs were terminal: 221
current files, no fixture changes. Working-tree diff check passed. Lifecycle
alignment is scoped; full ordinary native temporal validation remains failed.

## Darwin raw callback boundary diagnosis — 2026-10-11

Baseline: `936c1c7`. [Captured audit](docs/audits/go-fsevents-raw-callback-2026-10-11.json)
retains the temporary C callback instrumentation, its SHA256, restored source and
fixture hashes, all failed-subscription raw records, exact failure timelines,
full log hash, Source comparator results and its reproducible script.

Temporary stderr logging was inserted before every Go callback forwarding call.
Full ordinary `go test -p=1 -count=1 -timeout=300s ./...` failed only reload:
hidden-parent-source, same-mtime-write and lower-mtime-write. All three observed
a post-subscription readiness probe, then timed out on the operation. Each failed
subscription's raw C records contain only setup/readiness events; no operation
callback was recorded before the deadline and cleanup. These failures do not
support attributing the timeout to Go lexical admission or batch deduplication.
They do not prove kernel event loss, absent stream registration or a host cause.
Stderr instrumentation may affect scheduling; event IDs are not timestamps.

After that Go process terminated, a sequential actual Python/watchfiles native
comparison first constructed/closed 32 RustNotify subscriptions, then ran all 16
WatchFilesReload recipes using the existing Source helper. All matched the pinned
fixture, including the three failed Go names. Source kept its 3-second operation
window. Its workload is not the full Go package workload or identical scheduling;
this does not prove a Go-only defect or explain causality.

Instrumentation was fully removed after capture; the restored C SHA256 is
73e544bdcd054012392a9123ac8230d716b05d41ae65c6a191296dcd47389649,
with `git diff --exit-code -- go python` confirming no runtime/test/fixture edits.
No fixture widening, deadline extension, fallback backend or default change.
Next diagnosis targets registration/delivery before the Go callback and matched
Source workload, not wider callback admission. Native temporal acceptance remains
open. Documentation validation: audit assertions and diff checks; no fresh full
race, vet, type audit, export sweep, Python regression, coverage or visual gate.
Prior gates retain their original baselines. Architecture is unchanged. No merge
or push, and no complete migration claim.

## Immediate Source comparison after native timeout — 2026-10-11

Baseline: `d3e6eee`. [Exact experiment audit](docs/audits/go-fsevents-immediate-source-2026-10-11.json)
pins the temporary C/Go instrumentation hashes, restored-source hashes, complete
failure timelines, raw Go callback records, actual Source outputs and comparator
script. This refines the preceding isolated Source comparator.

The full ordinary Go suite with experimental tracing failed only reload:
added-source, hidden-parent-source and ignored-backup. After each operation Next
timed out, the experimental test explicitly closed/joined that Go subscription,
then immediately ran the same Source recipe in a Python subprocess. The Go test
waited for that comparator and preserved its original failure. Source used the
actual RustNotify/WatchFilesReload helper, native flags 18, latency zero and
since-now, with the unchanged 3-second operation output window.

| Failed Go recipe | Immediate Source changed | Matches pinned fixture |
| --- | --- | --- |
| added-source | new.py | yes |
| hidden-parent-source | null: no result within the Source operation window | no |
| ignored-backup | empty observed filtered batch | yes |

Source helper completion/close succeeded in all three cases. Hidden-parent's null
is distinct from an observed empty batch. It demonstrates that a native wait can
expire in actual Source too; it does not prove identical causes, kernel loss or
absence of raw Source callbacks. The Source total elapsed seconds include setup,
readiness and cleanup and must not be reported as operation latency. Sequential
same-host comparisons still differ in paths, times, language and scheduling.
Raw Go records for each failed subscription contained only setup/readiness;
no operation callback was recorded before its timeout/cleanup.

Local Apple SDK FSEvents.h also documents purge as root-only and device query as
zero for host-relative streams; this experiment ran as UID 501. No global purge
was introduced or tested as a fix. Header path/hash and discussion line ranges
are pinned. No conclusion that purge success/failure caused these timeouts.

All temporary tracing/comparator test changes were removed only after the full Go
process terminated. `git diff --exit-code -- go python` confirms no production,
test or fixture changes remain. Audit assertions and diff checks passed. There
is no new passing full ordinary gate, race/vet/type-audit/export/coverage/Python
regression or visual check. Fixtures and deadlines remain unchanged. Native
reliability acceptance remains open; repeated Source/native timing experiments
alone must not be substituted for the broader remaining SDK/default/G7 closure.
Architecture and feature defaults remain unchanged. No merge or push.

## Nonfinite SDK downstream consumers — 2026-10-11

Runtime baseline: `03d71c2`. The explicitly requested provider diagnostic replay
at this baseline has 98 cases and four remaining differences (client/server
NaN and Infinity). [Current audit](docs/audits/provider-nonfinite-consumer-2026-10-11.json)
pins the full replay hash and the new downstream Source fixture hash. Its diagnostic
job is an observation report, not a passing parity gate.

`python-provider-nonfinite-consumer.json` captures four actual AsyncAnthropic
0.107.1 direct responses through httpx 0.28.1, repository _content_payload, actual
SQLite message/event append and close/reopen, and the next SDK request. Legacy
nonfinite response bytes are passed as response content; using Response(json=...)
would refuse them before the SDK and measure a different stage. All nonfinite
values in the fixture are inside JSON strings; strict outer JSON is asserted.

Source retains NaN/Infinity in input.keep. The existing actual Agent._exec_tool /
Tool.run / builtins._bash consumer captures are included for the two client cases:
before → guard → after → result, binding error for unexpected keyword keep,
no execution callback. This does not claim new permission-mode coverage or actual
shell execution. Both server and client observation values survive actual SQLite
messages/events after a separate close/reopen. This proves reconstruction, not
killed-process recovery, new leases or stronger durability.

All four next SDK requests fail during finite HTTP request encoding with
ValueError: Out of range float values are not JSON compliant. The mock transport
receives only the initial request, so no failed-forward request reaches transport.
Source SDK observation/archive acceptance and finite request encoding are distinct
boundaries. Go currently rejects all four earlier at content_block_stop. Merely
loosening request JSON or serializing the values as strings would not match Source.
Native implementation must preserve typed inert observations and archival legacy
numbers through the correct consumers, then retain finite request admission.
No Go production decoder, execution authority or default changed in this capture.

Passed: new nonfinite consumer test plus existing Bash consumer test (2 tests;
8 expected SDK serializer warnings), 19 anchored scanner guards, standard-JSON
fixture/audit consistency checks and git diff --check. Source export --check passed
sequentially: 222 current files. No fixture expectations were widened. Python
runtime modules were unchanged, so no package invariant check was required;
full Python regression, full Go/race/vet/type/mutation gates, new coverage,
performance, non-Darwin and browser checks were not run. Their prior baselines
remain explicit; native watcher ordinary failures remain unresolved. Architecture
is unchanged. This is Source prerequisite delivery, not four-gap closure, G4/G7
acceptance or complete port acceptance. No merge or push.

## Native nonfinite SDK observation consumers — 2026-10-11

Delivered as `486f8ec` (named protocol observation/archive foundation) and
`c8491d2` (provider, event and SQLite consumers).
[Native audit](docs/audits/provider-nonfinite-native-2026-10-11.json) records
98 diagnostic SDK snapshots with zero differences, all 51 captured Bash gate
consumers, four direct response and SQLite message reopen cases, and two named
client tool-event SQLite reopen cases. Valid next-request transcripts refuse
nonfinite encoding before transport; historical event reconstruction remains
untrusted. Standard request/event JSON stays finite. The SDK replay remains a
diagnostic, not complete G4/G7 acceptance. Broader known-field adaptations,
unknown-tool nonfinite inputs and nonfinite SSE event delivery remain open.

Integrated worktree verification: targeted protocol/provider/agent/SQLite tests,
full Go race with coverage, go vet and package type audit passed (434 production
files / 1,405 named types / zero findings). Full ordinary Go ran and failed only
in native reload recipes removed-source, lower-mtime-write and identical-write.
No rerun was used to replace those failures. Source export check passed
sequentially after native Go processes ended: 222 current files. Architecture
source/generated artifact passes Archify 9/9 with no errors or warnings; no fresh
browser visual inspection. Both code commits were validated in the combined
worktree; the intermediate commit was not separately full-suite replayed.

[Current race coverage](docs/audits/go-coverage-nonfinite-observation-2026-10-11.json):
application excluding tools 32,037 / 35,891 = **89.26%**; all Go
32,084 / 35,955 = **89.23%**. Source locations are deduplicated using maximum
hit count; this is the Darwin arm64 statement denominator. Versus runtime
`f2cdbca`, application covered statements increased by 230 and total by 290;
percentage decreased from 89.34%. Native C and child-process internals are not
instrumented. Passing race coverage does not supersede ordinary watcher failures.
No Python runtime/scanner/mutation anchor changed, so those gates were not rerun.
Full Python, performance, non-Darwin and browser refresh were not run.
Native watcher reliability, default diagnostics and full migration acceptance
remain open. No merge or push.

## Managed nonfinite journal/SSE Source contracts — 2026-10-11

Baseline: `412e94c`. Four actual Source ASGI message-stream turns use the real
AsyncAnthropic 0.107.1 parser, managed session, hooks, default memory journal and
explicit SQLite journal. The stream retains NaN/Infinity in tool_use.input.keep.
Journal fingerprinting uses allow_nan=False: tool_result reports the finite JSON
error, hook order is before → guard → result (no after), and no action is created.
This differs from the 51 standalone ToolGate consumers without a journal, which
settle the unexpected-keyword binding failure through after/result. Both paths
remain authoritative for their own composition. The next model request fails
before transport; the Source stream reaches done. Actual SQLite tool-use events
retain the observations. The fixture's outer JSON remains strict; legacy numbers
are represented inside JSON strings.

A targeted mutation enables nonfinite action hashes and is caught by this real
Source test after a passing baseline; 19 scanner guards remain anchored.
Native integration is in progress. Realtime persistence projection, observation
query and request estimation were discovered as separate finite-only consumers.
Full native regression and Source export refresh are pending. This prerequisite
does not establish complete G4/G7 acceptance or native watcher reliability.

Source prerequisite gates completed: both nonfinite Source tests passed; the
journal fingerprint mutation was caught, all 19 scanner guards are anchored,
and sequential Source export --check reports 223 current files.
[Managed stream fixture](go/testdata/python-provider-nonfinite-stream.json).

## Managed nonfinite observation adoption — 2026-10-11

Source prerequisite `d36323d`; native consumer adoption `cc5cb1d`.
[Managed nonfinite audit](docs/audits/provider-nonfinite-stream-2026-10-11.json)
records four Source and native managed message-stream cases: memory and real
SQLite, NaN and Infinity in unknown Bash input.keep. SSE preserves the original
observation; finite journal fingerprint admission fails before action creation.
Before/guard/result run, after is skipped, no native handler executes, and the
next request does not reach transport. Standard REST and model request JSON
remain finite. Native live event/message archival projections mask decoded
strings/keys and reconstruct stored records with untrusted authority. Request
estimation retains detached cache annotations; observation queries use separate
read-only values rather than marking executable decision inputs as projections.

Verification on the combined worktree: targeted native tests including Source
consumer comparison, mask panic/secret-key tests and finite cache JSON equality;
full Go race; go vet; package type audit (436 production files / 1,405 types / zero
findings); two Source tests; one caught journal fingerprint mutation with passing
baseline; all 19 scanner anchors; sequential Source export --check (223 files);
Archify 9/9 with no errors/warnings; git diff --check. Temporary Launcher diagnostic
changes were restored. The initial full ordinary run exposed decision recording
projection regressions in Launcher LLM/Jev and SQLite baseline/maximum paths;
those were fixed and targeted proofs passed. The fresh full ordinary run passes
those packages and fails only native reload same-mtime-write/lower-mtime-write
recipes. That failure is retained, not superseded by race passing. Intermediate
commits were not separately full-suite replayed.

Coverage is not refreshed for this slice: 89.26% application / 89.23% all Go is
the prior runtime c8491d2 snapshot, not current cc5cb1d measurement. Full Python
regression, non-Darwin, performance and fresh browser visual checks were not run.
Python package modules were unchanged; source scanner/mutation inventory gates
were run for the changed tools. Legacy trajectory/model snapshots, complete
historical restore consumers, broader known-field/unknown-tool input adaptations,
native watcher reliability and default diagnostics remain open. This is concrete
managed consumer delivery, not complete G4/G7 or migration acceptance. No merge
or push.

## Nonfinite SQLite restore and catch-up — 2026-10-11

Baseline `73d3dbc`; Source prerequisite `a5c6864`; native fix `a176242`.
Two actual Source SQLite captures physically close/reopen
and construct a new Manager before explicit restoration. NaN/Infinity in Bash
input.keep survive; owner alice is retained, current permission mode is
interactive, and transcript counts are 4 before / 4 restored / 6 after resume.
The resumed Source request fails finite encoding before any transport call.
Source restore method hashes and legacy observation strings are captured in the
[managed stream fixture](go/testdata/python-provider-nonfinite-stream.json).

Native restore/catch-up now validate known events with the named archival codec
before re-decoding closed variants. Scope/order/window and identity checks remain.
Two real native database close/reopen tests retain the observed transcript,
owner/current policy and original tool input. Every catch-up context is untrusted;
no historical tool action exists. Resume sends zero new transport requests and
retains healthy persistence with the current lease confirmed. Catch-up acquires
no lease. Full Source/native transcript bytes and SDK error-text/class equality
are not claimed.

Combined-worktree gates: targeted sqlitestate/agent Nonfinite/Catchup/Restore;
full Go race (50 packages); go vet; type audit (436 production files, 1,405 named
types, zero findings); Source managed nonfinite test; 19 scanner anchors; one
caught journal-fingerprint mutation with passing baseline; sequential Source
export --check (223 files); Archify 9/9, zero errors/warnings; git diff --check.
Full ordinary Go fails only reload lower-mtime-write and identical-write. That
failure remains open. Commits are split for review and were not independently
full-suite replayed. See the [restore audit](docs/audits/provider-nonfinite-restore-2026-10-11.json).

Coverage was not refreshed: 89.26% application / 89.23% all Go belongs to runtime
c8491d2. Full Python, non-Darwin, performance and fresh browser visual checks were
not run. Broader historical/trajectory/model consumers, SDK error fidelity,
watcher reliability, default diagnostics and complete migration acceptance remain
open. No dependency was added; no merge or push.

## Nonfinite managed trajectory snapshots — 2026-10-11

Baseline `591a3c9`. [Source fixture](go/testdata/python-provider-nonfinite-trajectory.json)
adds four real AsyncAnthropic 0.107.1 / Manager.run / JSONL / fresh-reader cases:
NaN/Infinity in unknown Bash input parameters, with capture_content on/off.
Selected model outputs, tool inputs and both model-start message snapshots match
native sorted legacy JSON exactly. The next-request snapshot includes the detached
cache annotation; registered secret keys/values are masked. Content-off projections
match Source redaction. Live inputs retain original unmasked observations. Only
one initial request reaches transport; recording remains healthy and status error
reflects the subsequent finite request failure.

TrajectoryRecord.MarshalArchiveJSON now uses concrete event and SDK observation
encoders, preserving the writer's privacy-before-UTF-8 policy. Standard MarshalJSON
and model HTTP encoding remain finite. The first implementation missed the second
model_start: request optimizer documents/digests still encoded finite messages.
Those now use detached observed messages; the existing protocol shape guard and
concrete text/result lowering retain original immutable tool variants and grants.
No dependency or execution path was added. Full transcript/system/catalogue bytes,
SDK error-class/text fidelity, broader historical scalar consumers, watcher
reliability, default diagnostics and complete migration acceptance remain open.

Completed native gates on the combined worktree: targeted trajectory/agent
consumer comparison and projection/archive selectors; full Go race; go vet;
type audit (437 production files / 1,405 named types / zero findings). One Source
test covers four actual profiles; 19 scanner anchors pass and the selected journal
fingerprint mutation is caught after a passing baseline. Full ordinary Go fails
only reload removed-source, same-mtime-write and rename-out recipes. Archify passes
9/9 with zero errors/warnings; no fresh browser visual inspection was performed.
See the [trajectory audit](docs/audits/provider-nonfinite-trajectory-2026-10-11.json).
Coverage is not refreshed: 89.26% application / 89.23% all Go belongs to c8491d2.
Full Python, non-Darwin and performance checks were not run. No merge or push.

Sequential Source export --check reports 224 current files; git diff --check
passes. Intermediate commits were not independently full-suite replayed.

Source prerequisite: `5870a1d`; native adoption: `935cf83`.

## Nonfinite known Bash inputs and batch preparation — 2026-10-11

Source prerequisite `c3f555a`; native implementation `770f065`, baseline
`4c807ae`. Actual pinned Anthropic 0.107.1 captures cover 24 unique direct/SSE
inputs, 36 standalone gate profiles and 24 managed runs. Named command/options
and rejected roots preserve NaN/Infinity through observations and archive identity.
Explicit caller and outbound HTTP admission remain finite; background_run remains
finite. Every Bash batch prepares inputs before effects: a valid first call plus
an invalid second root has no hooks, actions or handler effects. Six managed
roots fail before the gate; eighteen objects fail finite journal fingerprints
after before/guard, without after hooks or action entries.

Targeted Go consumers pass, as does the actual Source test (six expected SDK
warnings). Nineteen scanner anchors pass; selected lazy-batch-copy and nonfinite
journal mutants are caught after passing baselines. Source export check reports
225 current files. Vet passes with existing FSEvents warnings; type audit reports
439 production files / 1,407 named types / zero findings. Archify passes 9/9,
zero errors/warnings; fresh browser visual inspection was not performed.

Full ordinary Go fails only reload identical-write, same-mtime-touch, rename-source
and ignored-hidden. Full race with coverage passes reload but fails SQLite decision
restart maximum on context deadline. No data race was reported. One subsequent
maximum-only race check without coverage passes in 13.744 seconds; it does not
replace the full failing result. Latest measured application coverage is
32,196/36,106 = **89.17%**; all Go is 32,243/36,170 = **89.14%**. This is a
measurement from the failing coverage run; the timed-out profile may undercount.

See [consumer audit](docs/audits/provider-nonfinite-bash-2026-10-11.json) and
[coverage audit](docs/audits/go-coverage-nonfinite-bash-2026-10-11.json).
Non-string batch pair keys are explicitly unsupported. Falsy/string-key pair
normalization lacks a complete cross-language profile corpus. These new shapes
have named archive round-trip proof, not new SQLite restart proof. Broader inputs,
SDK error-class/text equality, watcher reliability, default diagnostics and full
migration acceptance remain open. No full Python, non-Darwin or performance check;
combined worktree gates do not claim independent replay of intermediate commits.
No dependency added in this slice; no push or merge.

## Nonfinite background command/root consumers — 2026-10-11

Baseline `7992994`; Source prerequisite `49ee982`. Shared actual SDK/standalone/
managed capture helpers retain the original Bash fixture observations. New
background profiles cover NaN/Infinity/-Infinity in roots, arrays, command
scalars/arrays/objects and unknown parameters: 18 unique direct/SSE inputs,
27 standalone consumers and 18 actual Manager runs. Before rewrite/deny/guard
priority matches. Standalone command split failures create the scoped background
manager without tasks; root/binding failures do not initialize it. Managed
batch-copy failure precedes hooks/effects; finite journal refusal precedes
background manager initialization. A valid earlier call plus a later bad root
creates no manager/task.

Existing named BashCommandObservation, BashRejectedArguments and background
argument types retain legacy numbers. Observations, canonical identity and
archives are separate from strict caller/request/event JSON. Both Python and
native SQLite message close/reopen retain all 18 unique SDK inputs. Native
readonly checks cannot obtain a grant or initialize background work. Known
timeout/approval-prefix coercion remains explicitly outside this slice. No new
production type or dependency was introduced.

Validation on the combined worktree: targeted protocol/provider/agent/SQLite
consumers pass; full Go race passes; vet passes with existing FSEvents warning;
type audit has 439 production files / 1,407 named types / zero findings. Two
Source tests pass with 12 expected SDK warnings; 19 scanner anchors pass; selected
background lazy-copy and finite-journal mutants are caught after passing baselines.
Archify passes 9/9 with zero errors/warnings; no fresh browser visual inspection.
Full ordinary Go fails only native reload removed-source, same-mtime-write,
lower-mtime-write, identical-write, same-mtime-touch, rename-out, rename-source,
ignored-hidden and ignored-backup. Race passing does not close watcher reliability.

See [background audit](docs/audits/provider-nonfinite-background-2026-10-11.json).
No coverage refresh: 89.17% application / 89.14% all Go belongs to runtime
`770f065` and its failing instrumented SQLite maximum profile. New persistence
proof covers message observations, not full event/session/trajectory recovery for
these inputs. Non-string batch keys, broader optional fields, global SDK error
class/text, default diagnostics and complete migration acceptance remain open.
Full Python, non-Darwin and performance checks were not run. Intermediate commits
were not independently full-suite replayed. No push or merge.

Full Source export --check did not complete: Python Watchfiles native observer
did not arm during snapshot construction, before all fixture comparisons and
the diagnostic writer. No diagnostic directory was created. A separate exact
_json_bytes check of the current Bash/background helper exports confirms both
task-owned fixtures byte-for-byte. This narrow check does not establish that
all 226 export targets are current. git diff --check passes.

Native implementation: `83d12e2`; Source prerequisite: `49ee982`.

## Darwin per-subscription dispatch queue — 2026-10-11

Baseline `a5aed66`. The deprecated FSEventStreamScheduleWithRunLoop path now
uses Apple's supported FSEventStreamSetDispatchQueue with one owned serial
queue per stream. Start and stop/invalidate serialize with event callbacks.
Close drains work before stream/queue release and typed registry retirement;
start failures release partial ownership. The coordinator joins cleanup without
a private locked OS thread, CFRunLoop or second stop-signalling goroutine.

Apple API requirements are pinned in the [queue audit](docs/audits/go-fsevents-dispatch-queue-2026-10-11.json)
with the installed SDK header hash/version and implementation/test/fixture hashes.
Callbacks retain the same flags 18, latency zero, since-now, root/change/loss
admission and lexical projection. Native/polling selection and all batching,
context and recipe deadlines remain unchanged. No global purge, fallback or
fixture relaxation. This is a supported native API adaptation; it does not prove
the causal origin of the previous Go or Source callback timing failures.

Lifecycle/cancellation, 32 immediate concurrent-close subscriptions, real
active-callback join and all 16 recipes pass in targeted ordinary and race runs.
A callback held inside diagnostic IO demonstrates Close cannot return before
that callback is released and registry cleanup joins. Fixed three-round recipe
validation passes all 48 scenarios in 21.418 seconds. Full ordinary Go still
fails only reload symlink-target-write, removed-source, same-mtime-write,
same-mtime-touch and rename-out. Full Go race passes (reload 39.524 seconds);
it does not supersede those ordinary failures or establish exhaustive reliability.

Vet passes; the FSEvents deprecation warning is removed, while the existing
macOS race linker warning remains. Type audit has 439 production files / 1,407
named types / zero findings. README canonical map, boundary, review baseline and
architecture specification are synchronized; generated HTML passes Archify 9/9,
zero errors/warnings. No fresh browser visual inspection. Coverage/full Python/
non-Darwin/performance were not rerun. No Python package-module/scanner/guard
anchor changes. Prior coverage keeps its `770f065` baseline and failed-run limit.
Native temporal reliability, Source capture readiness, default diagnostics and
complete migration acceptance remain open. No dependency added, merge or push.

Native implementation: `0d794bf`. Sequential Source export --check passes after
all native Go jobs terminated: **226 files current**, including the previously
interrupted background export slice. This run's Source readiness/capture succeeded;
it does not prove the cause of the earlier intermittent Source arm failure.
Exact recipe/helper/fixture bytes remain unchanged from `a5aed66`. Full ordinary
failure timelines/log hashes are retained in the audit; diff checks pass.

## Background optional-field execution Source contracts — 2026-10-11

Baseline `ce3406c`. The current Go model background decoder forwards known
`timeout`/`approval_prefix` fields to strict `BackgroundRunInput` decoding.
This rejects Source-supported observations before the execution stage. New
`python-provider-background-options.json` records 48 actual standalone Agent
gate and BackgroundManager task profiles, with Source function hashes. It
separates accepted tool results from asynchronous task results and observes
process creation/group termination before capture cleanup.

All optional profiles allocate bg_0001 and return Started with a successful
tool outcome before any process starts. Falsy timeout values select the default;
positive integers, bool and fractions are accepted. Negative numeric timeout
values kill the group after creation. The expired bool label is `Trues`.
Truthy text/list/dict timeout values fault after process creation with Python
comparison errors. A 401-digit integer faults at float conversion. Those five
fault profiles do not kill the group and emit an unawaited-capture RuntimeWarning;
the fixture joins and drains the harmless commands after settlement. NaN and
positive infinity complete the selected short command; this is a captured
profile, not an exhaustive event-loop timing guarantee. Negative infinity times
out with `-infs`. All prefix shapes permit execution; only an honest two-string
leading prefix contributes a proposal. Invalid proposals do not grant authority.

Next native slice: retain optional observations in named immutable protocol
variants; lower timeout observations into explicit service/shell wait policies;
keep task allocation and post-spawn fault ordering; preserve owner cancellation
and process-group cleanup. Compare direct/SSE, readonly approval, managed journal
and SQLite/archive consumers separately. Explicit typed caller admission stays
strict. This prerequisite changes no runtime, topology, default or dependency;
the Go optional-field mismatch remains open.

Validation: three Source contract tests pass with 12 existing SDK warnings;
19 scanner guards pass. Full Source export --check passes: 227 files current.
git diff --check passes. No full Python/Go, race, vet, coverage, non-Darwin or
performance run is claimed for this Source-only prerequisite. No merge or push.

## Background NaN observer correction — 2026-10-11

The initial `5985636` Source capture awaited the task with an outer three-second
asyncio watchdog. Fresh isolated-loop comparison proves that this alters the
NaN profile: direct await yields `Error: Timeout (nans)` after spawning/killing;
watchdog-guided await completes the 30ms command. The capture now directly awaits
its harmless bounded commands. A regression test records both observations, and
the 48-case fixture retains the unguarded outcome plus the capture function hash.
The Go policy must use a named immediate timeout for this unguarded NaN profile,
not infer an unbounded wait from the instrumented observation. Exact Python
heap/interleaving behavior remains an explicit adaptation; positive infinity is
still unbounded. All other optional inputs and initial call acceptance stay the
same. This correction changes the Source oracle, not Python production behavior.

Correction gates: two Source tests pass; full export --check passes with 227
current files; git diff --check passes. Native adoption is validated separately.

## Named background optional policy delivery — 2026-10-11

Runtime `4f56ef8`; corrected Source prerequisite `b6f49c6` (original `5985636`).
BackgroundTimeoutObservation and BackgroundTimeoutPlan keep raw observations out
of the service/shell; explicit BackgroundWait variants handle default truth,
bool/fraction/negative values, immediate unguarded NaN, unbounded positive infinity
and post-spawn comparison/integer-overflow faults. Known integer projections and
strict typed caller admission remain available. Long finite deadlines use bounded
timer steps; old time.Duration callers retain their exact timer path. Timeout
labels are masked before returning. Approval metadata reaches existing validation
only as string-array candidates and cannot initialize readonly background work.

All 48 Source initial call outcomes and settled task results match; hook order,
notifications and readonly candidate refusal are checked. Native direct/SSE,
clone/masking/identity/archive and SQLite message close/reopen retain those inputs.
These decoder envelopes are generated from Source inputs, not newly captured
Python SDK transport/coercion observations. SQL proof is messages, not full-state
recovery for all optional profiles. Source type/overflow faults leave processes
alive; native code deliberately kills/joins before publishing the same error.
Native goroutines may start before caller return; that scheduler property is not
claimed aligned. The paired Source test proves a watchdog changes NaN from timeout
to completion, so exact timer-heap behavior remains a documented adaptation.

Gates: six relevant Go packages pass; full race and vet pass; type audit reports
441 production files / 1,413 package-level types / zero findings. Two corrected
Source tests and final full export --check pass (227 files after native jobs
terminate). Archify passes 9/9 with zero errors/warnings; no fresh browser visual.
Final ordinary full Go fails only reload symlink-target-write, removed-source,
same-mtime-write, lower-mtime-write, identical-write and same-mtime-touch.
See [background option audit](docs/audits/go-background-options-2026-10-11.json)
for exact commands, source/implementation hashes, preserved logs and limits.
No full Python, non-Darwin, coverage or performance refresh. Default diagnostics,
broader managed option consumers and complete port acceptance remain open.
git diff --check passes. No dependency added, merge or push.

## Captured SDK background optional consumers — 2026-10-11

Source prerequisite `9086419`; native tests `8888e72`; production runtime remains
`4f56ef8`. The pinned anthropic 0.107.1 capture exercises 48 direct/SSE and
forwarding profiles plus 48 actual Source SessionManager runs. Native tests now
consume those captured envelopes. All 42 finite profiles match managed admission,
hook order, journal presence, initial output, final reply and settled task result.
Six nonfinite profiles match rejection before forwarding and background allocation,
but final error rendering remains open: Source returns `[Error] ValueError: Out of
range float values are not JSON compliant`; Go returns `[Error] *provider.Failure:
cannot encode provider request`. The test logs this difference; it does not assert
error-text parity. Existing process cleanup and scheduler adaptations remain.

Gates: three Source tests pass; complete provider/agent race suites pass; 19 scan
guards pass; full Source export --check passes with 228 current files after native
jobs terminate; focused nonfinite diagnostic tests and git diff --check pass.
This slice changes tests/export tooling only. No new full Go/Python suite, vet,
type audit, architecture visual, coverage or non-Darwin gate is claimed.
See [SDK consumer audit](docs/audits/go-background-sdk-2026-10-11.json).
No merge or push; full port acceptance and SDK final error rendering remain open.

## Nonfinite provider encoding error projection — 2026-10-11

Runtime `ea35cd3`; captured SDK/managed prerequisite `9086419`, native stage tests
`8888e72`. Direct and streaming request serialization now recognizes the wrapped
nonfinite JSON sentinel as `FailureNonfiniteRequest` with SDK `ValueError` class
and fixed `Out of range float values are not JSON compliant` text. Encoding still
fails before dynamic credentials, HTTP, retries or stream callbacks. Recovery
retains nonretryable protocol evidence. The named ProviderErrorDetail interface
projects only the adapter's captured nonfinite diagnostic at the final Session
provider boundary, bounded to 500 runes. Generic lifecycle diagnostics and other
provider failure formatting retain their previous behavior.

All 48 captured managed optional profiles now assert exact final reply text,
closing the six nonfinite final-message differences logged by `98f94be`. Twelve
direct/streaming boundary cases also assert class, recovery evidence and zero
credential/HTTP/delta effects. Wrapped-error projection and diagnostic bounds are
checked. This does not close broader SDK exception parity, full-state optional
recovery, native post-spawn cleanup/scheduler adaptations or full port acceptance.

Gates: focused provider/agent tests pass; full Go race and vet pass; type audit
reports 443 production files / 1,414 package-level types / zero findings. Full
ordinary Go fails only reload identical-write, same-mtime-touch, rename-source
and ignored-hidden; SQLite passes. Source full export --check passes with 228
current files after native tests terminate. Archify delivery passes 9/9 with zero
errors/warnings after one note-length repair; browser visual review is not
refreshed. README outline and git diff --check pass. No full Python suite,
coverage, non-Darwin, performance or mutation refresh. Python runtime/scanner
modules are unchanged. No merge or push. See
[encoding error audit](docs/audits/go-request-encoding-2026-10-11.json).

Next: investigate ordinary native reload event failures against actual Source
recipes; retain the passing race result separately and preserve watcher ownership.

## Native recipe probe setup fidelity — 2026-10-11

Test prerequisite `e5064d8`; production baseline `ea35cd3` unchanged. Actual Source
`_native_case` drains subscription setup before writing its readiness probe, then
drains again after observing that probe, with rust_timeout=100 and reload_delay=0.
The Go recipe test now uses that same two-phase order, with 100ms context-bounded
Next waits. This aligns setup order; it does not prove identical timer scheduling
or timeout behavior during an in-progress batch. The live subscription, expected
16 recipes, event filtering and 3-second operation deadline are unchanged.

Counterevidence matters: the unmodified targeted 16-case run already passed in
7.590s. After the test change, three targeted rounds (48 scenarios) pass in 18.239s.
Fresh actual Source captures for identical-write, same-mtime-touch, rename-source
and ignored-hidden all match the pinned fixture, including the excluded empty
batch. Nevertheless, full ordinary Go still fails only reload lower-mtime-write,
identical-write, rename-out and rename-source (reload 97.211s). Thus omitted
pre-probe draining is not established as the cause of the full-run failures.
The failure timelines contain no admitted diagnostic event after the operation;
that diagnostic runs after Darwin flag/root admission, so it cannot prove that
the OS or raw C callback produced no event. Native temporal reliability remains
open. The reload package race run passes in 59.235s with existing macOS linker
warnings and no reported data race; it does not supersede the ordinary result.

See [probe setup audit](docs/audits/go-reload-source-drain-2026-10-11.json) for
Source results, test/source hashes, commands and log hashes. git diff --check
passes. This is a tests-only change: runtime/types/defaults/dependencies and
architecture are unchanged. No new full race, vet/type audit, full Source export,
Python suite, coverage, non-Darwin or performance gate is claimed. No merge/push;
the full port goal remains active.

Next: distinguish raw FSEvents callback delivery from Go flag/root admission at
the exact failing runtime baseline before proposing a listener implementation fix.

## Raw Darwin callback boundary experiment — 2026-10-11

Baseline `7b6c3ad`; production runtime `ea35cd3` unchanged. Temporary C logging
records time, subscription key, event ID, flags and path before calling Go; a
temporary test trace records the operation wall-clock epoch. The instrumented
full ordinary Go run fails only reload lower-mtime-write, identical-write,
same-mtime-touch, rename-source, ignored-hidden and ignored-backup (110.951s).
Each failed subscription has two or three raw callbacks before its acknowledged
readiness probe and **zero raw C entries after the operation epoch**. The last
raw entry precedes the operation by about 151–157ms. Thus Go flag/root admission,
lexical filtering and batching are not the direct source of these six missing
callback entries in this run. This narrows the failure boundary before C entry;
it does not determine whether event stream delivery or dispatch queue service is
responsible, and it does not establish a kernel/fseventsd defect.

[Raw callback audit](docs/audits/go-reload-raw-c-entry-2026-10-11.json) preserves
per-subscription evidence, event IDs/flags, exact command, log and file hashes.
The run is instrumented: logging may affect scheduling, so it does not replace an
uninstrumented acceptance gate or prove a root cause. No same-pressure Source
comparison was made; prior Source captures retain their own scope. Temporary C
and Go files were restored byte-for-byte and verified against baseline hashes;
git status was clean before writing this evidence. No listener/runtime/test
change is retained, no deadline/fixture/fallback was relaxed and no dependency
was added. git diff --check passes. No new race/vet/type audit/export/Python suite,
coverage, non-Darwin, performance or architecture visual gate is claimed.
Native temporal reliability and complete migration acceptance remain open.
No merge or push.

Next: measure private serial dispatch queue progress independently of event
callbacks during the failing operation window, separating queue service from
upstream stream delivery before proposing an implementation change.

## Darwin queue progress probe — 2026-10-11

Baseline `360f844`; production runtime `ea35cd3` unchanged. A temporary 100ms
serial-queue timer and raw C callback recorder retain bounded in-memory samples,
printing only after close joins timer cancellation and callbacks. Probe lifecycle
checks pass (32 immediate concurrent closes and active callback join). The
instrumented full ordinary run still fails eight native recipes (reload 115.733s).
Every failed subscription executes 30 timer handlers in its 3-second operation
window; maximum window gap is 105.521–109.824ms, with no raw/tick/path overflow.
All eight have zero raw C event entries after operation. Thus an entire-window
private queue stall is excluded for this run; the event-stream delivery/dispatch
boundary remains unresolved. This is not proof of a kernel/daemon defect.

[Queue progress audit](docs/audits/go-reload-queue-progress-2026-10-11.json) pins
samples, subscription keys, log/file hashes and timer cleanup header evidence.
Timer activity and probe context storage are observer interventions; these results
do not replace uninstrumented acceptance. Temporary files were restored exactly;
no implementation/test/default/dependency change is retained. git diff --check
passes. No new uninstrumented full Go/race/vet/type/export/Python/coverage/visual
or non-Darwin gate. Native temporal reliability and complete port acceptance stay
open; no merge/push. Next: compare actual Source stream configuration, registration
history and delivery under matching subscription churn before changing runtime.

## Source/native immediate-close controls — 2026-10-11

Baseline `05d675c`; production runtime `ea35cd3` unchanged. Local notify 8.0.0
source matches the pinned d6c13ff6 hash: flags 18, latency zero, since-now and a
joined dedicated Source run loop. Actual watchfiles 1.2.0 / Uvicorn 0.49.0 controls
match all 16 native scenarios both without churn and after 32 immediate closes.
Fresh Go controls also match all 16 in both profiles (7.083s / 6.239s), including
its four-caller concurrent close check. Thus 32 immediate closes alone do not
reproduce the full-run delivery gap. Exact close concurrency/timing and compiler
preload are not matched; no root-cause or reliability conclusion is implied.

[Source churn audit](docs/audits/go-reload-source-churn-2026-10-11.json) retains
scenario outputs, commands, script/source/fixture/log hashes and reproduction
script. No repository runtime/test/fixture change, new dependency or full gate;
previous full ordinary failures and coverage baseline retain their limits.
git diff --check passes; no merge/push; complete port acceptance remains open.
Next: compare actual Source after preceding compiler/CLI volume workload,
separating shared filesystem workload from process-local subscription history.

## Native completed reply metadata — 2026-10-11

`3916f71` preserves container/stop_details through named protocol variants,
direct/SSE ingress, immutable clone and public JSON. All 44 actual SDK 0.107.1
captures match. Full ordinary/race Go, vet, syntax/public type audits, Python
regression (2231 passed, 28 skipped, 24 subtests) and 19 scan guards pass. Current
full Source export is interrupted by WatchFiles native readiness failure; metadata
fixtures are freshly regenerated and compared by Python regression. Last complete
full freshness remains the e1e82f0 229-file check. No coverage refresh or complete
G4/G7 claim. See [evidence](docs/audits/go-provider-reply-metadata-native-2026-10-11.json).

## Protected native API documentation — 2026-10-11

`cf1ebea` matches 36 actual Source requests for four default documentation routes;
embedded bytes and OpenAPI schema match, after shared cap/auth/security admission.
The 40 explicit business paths are unchanged. Full Source freshness is 230 files;
Python regression is 2232 passed/28 skipped/24 subtests, and full Go race, vet,
named-type audits, 19 scanner guards and two request-size guards pass. Ordinary
Go retains two known reload timing failures. Architecture generation passes nine
checks; local HTML visual review is blocked by browser security policy. External
UI library rendering, custom mounts/root paths, coverage refresh and final G3/G7
acceptance are not claimed. See [evidence](docs/audits/go-api-documentation-native-2026-10-11.json).

## Default business routing Source prerequisite — 2026-10-11

The native diagnostic observes 160 captured requests: 117 match, 43 differ.
Source 307 redirects versus native 404 account for 39 cases; Source first-partial
route Allow versus native method union accounts for four. Shared authentication,
query credential refusal, unknown-path and oversized-body cases match. Full Source
freshness is 231 files; Python regression is 2233 passed, 28 skipped and 24
subtests, with 19 scan guards and vet passing. Native routing repair and normal required parity assertions
are the next implementation slice. This prerequisite changes no production Go
behavior and does not accept generic encoded/repeated-slash or mount semantics.
See [evidence](docs/audits/go-http-routing-source-native-2026-10-11.json).

## Owned named Source routing — 2026-10-11

`4b68574` fixes all 43 prior business method/slash differences and seven additional
path-edge differences. All 168 actual Source requests pass a normal required
native TCP test. Body/auth admission and handler/service ownership remain intact.
Full Source freshness is 231 files; full race, vet, type audits, Python 2233 tests
(28 skipped/24 subtests), 19 scanners and two request-size mutation guards pass.
Ordinary Go retains five native reload timeouts. Architecture generation passes
nine checks; rendered review is blocked by browser policy. Broader URL/mount scope,
coverage refresh, default diagnostics selection and full G3/G7 acceptance remain
open. See [evidence](docs/audits/go-http-routing-native-2026-10-11.json).

## Captured SDK endpoint paths — 2026-10-11

`4da75a3` constructs direct/SSE message URLs from the validated escaped base path.
Captured HTTPX literal dot normalization preserves percent escapes and repeated
slashes; the SDK adds a slash only when absent before `v1/messages`. Configured
endpoint audit identity, credential admission and redirect refusal remain intact.
All 40 actual SDK requests (20 paths, direct/SSE) pass required native assertions;
22 pre-fix differences are eliminated. No dependency or domain type is added.

Provider ordinary/race, vet, zero-finding type audits and 19 scanner guards pass.
Full ordinary Go fails reload hidden-parent-source delivery and supervisor readiness;
full race fails reload rename-source delivery. Full Source export fails WatchFiles
native readiness, so complete freshness remains the prior 231-fixture checkpoint.
Python regression reports 2233 passed, 28 skipped, 24 subtests and one existing
concurrency timing assertion failure (0.557s vs 0.5s). A focused check of that
assertion and the endpoint fixture passes (2 tests); it does not supersede the
full-run failure. Complete gate outputs are retained in the
[endpoint audit](docs/audits/go-provider-endpoint-native-2026-10-11.json).
README canonical boundary and architecture source are updated; generation passes
9/9 checks with no errors or warnings. Rendered review remains blocked by prior
browser policy. General URL/hostname/port/custom-transport behavior, coverage
refresh, default diagnostics decision and final G0–G7 acceptance remain open.

## Provider URL construction/admission scope prerequisite — 2026-10-11

Production baseline `6a6d5ed`. The actual Python `AnthropicCompatibleProvider`
factory and SDK 0.107.1 / HTTPX 0.28.1 are observed across 34 URL profiles,
direct and SSE (68 observations). Only HTTP clients/retries and ambient environment
are injected; URL parsing, SDK construction and request preparation are real.
The Source fixture has 52 completions, 10 request-stage failures and six constructor
refusals. Go has 36 completions and 32 constructor refusals. Among 36 completions
on both sides, 22 have identical captured calls and 14 differ: six authority
normalization profiles plus the empty-fragment representation. Another 26 cases
construct in Source but are refused by existing native endpoint admission.

The explicit diagnostic observer is not a parity gate accepting these differences.
Injected transports do not prove real-network acceptance of FTP, oversized ports,
relative URLs or host syntax. HTTPX raw_path includes queries while Go EscapedPath
does not; full URLs and phase evidence must also be compared. Existing native
credential/query/fragment guards remain intact. No runtime implementation or
new dependency changes in this prerequisite. Next: normalize accepted authority
profiles with required assertions, establish real-wire IDNA/fragment evidence,
and reconcile every admission difference against existing security requirements.

See [URL scope observations](docs/audits/go-provider-url-scope-2026-10-11.json).
Complete migration, default diagnostics and native reload timing remain open.

Validation for the URL prerequisite: full Source --check completes 233 entries,
232 match and WatchFiles drifts in two changed-path observations. Full Python
passes 2235 tests (28 skipped, 54 warnings, 24 subtests), provider race, vet and
19 scanner guards pass. Full ordinary Go reports agent timeout-fraction settlement
and reload rename-source delivery failures; no full-race/runtime/type-audit refresh
was performed because runtime implementation is unchanged. A built-in imagegen
PNG summarizes this round; text/counts were visually checked and one correction
removed inaccurate quantitative bar geometry. This is evidence delivery, not G4/G7
acceptance. Coverage is not refreshed.

## Captured request authority and real HTTP wire — 2026-10-11

`3cd31be` normalizes outbound hostname/port spelling. `b5b7ef8` restricts
lowercase mapping to ASCII: Python Unicode lowercase can expand characters,
whereas Go rune mapping differs. IPv6 spelling and configured audit identity
remain intact. No dependency was added; admission and redirect guards are unchanged.
All 32 captured ASCII/IPv6 direct/SSE request assertions match (22 before).
The original 12 wire assertions matched at `3cd31be`; expanding to 14 reveals
2 required dotted-I failures. Current wire matches: 12/14; original `9c39e32`
with the same expanded inventory: 8/14. Native Host is `xn--bfa.invalid`, Source
is `xn--i-9bb.invalid`. This discrepancy existed before normalization; preserving
Unicode spelling does not reconcile standard transport IDNA. Assertions remain
required and failing. Dependency approval for pinned x/net and x/text is pending.

Actual SDK/provider and HTTP encoders/parsers are retained, with only the TCP
destination redirected to an owned loopback listener. Host, target, method/API key
and original dial authority are compared. Captured bücher/ß hosts match; this
inventory does not prove complete IDNA or TLS/SNI parity.

The separate 68-observation diagnostic has 36 both-completed observations:
32 identical calls and four Unicode-host/empty-fragment representation differences.
It does not contain dotted-I and is not a parity gate. Existing native admission
still refuses 26 Source-constructible inputs. Broader acceptance remains open.

Current gates and retained historical runs are recorded in the authority evidence.
Current full Source export fails before completing freshness: native Source observer
did not arm. The historical 234-fixture pass remains at the original 12-wire baseline.
Focused actual Source wire capture passes the expanded 14-case inventory.
Final Python regression passes 2236 tests, 28 skipped, 54 warnings, 24 subtests
(105.68 seconds).
Full ordinary Go fails the two required dotted-I cases and five reload cases:
hidden-parent-source, removed-source, lower-mtime-write, ignored-backup, ignored-text.
Full race also fails dotted-I, CLI reload and native reload timing cases; no data race
report appears in the log. Vet, type audits and 19 scanner guards pass. The branch is held from local main
`9c39e32` while stable Provider failures remain. No push. Coverage is not refreshed;
the latest measurement remains the `f2cdbca` snapshot (application 89.34%, all Go
89.31%). Prior background timing failures are not declared resolved.
README and architecture specification reflect the failing wire boundary; generated
architecture passes 9/9 checks. Rendered architecture review remains policy-blocked.
The built-in imagegen graphic was edited and visually checked for 32/32 request
matches, 12/14 wire matches and two open required failures. Full G0–G7 remains open.

See [native authority evidence](docs/audits/go-provider-authority-native-2026-10-11.json).

## Captured SDK container expiry validation paths — 2026-10-11

Runtime baseline `1a48a13`; native expiry implementation `f230942`. Actual
Anthropic 0.107.1, Pydantic 2.13.4 / core 2.46.4 and Unicode 14.0.0 captures expand
completed reply metadata from 44 to 196 direct/SSE observations. 38 finite expiry
profiles each run with a valid text ID and an invalid numeric ID, exercising
whole-container validation and SDK loose construction. Source/native differences
are established against the same inventory: 150/196 matches before, 196/196 after.
All 46 original failed assertions are preserved in the baseline audit.

Optional Container validation tries Pydantic date/timestamp parsing first. If
container fields fail validation, SDK construction falls back to Python datetime
parsing. Native ingress now selects those paths explicitly. Captures cover
positive/negative fractional epochs, watershed/millisecond units, microsecond
rounding/carry, date-only/lowercase/comma forms, Unicode digits, Python numeric
strings and overflow saturation, offset minute carry, invalid calendars and
finite list/object fallback values. Pinned upstream speedate 0.17.0 source and
installed SDK parser hashes are retained in the audit. A negative float with a
valid ID produces `1969-12-31T23:59:59.123457Z`; invalid ID selects the Source
fallback `1969-12-31T23:59:59.876544Z`. This is observed Source behavior, not a
claim that both parsers implement one mathematical timestamp interpretation.

Named immutable metadata, clone and public JSON round trips preserve these
observations; archive reads do not re-normalize stored expiry. No dependency,
broad dynamic domain payload, execution authority or feature default is added.
Existing finite metadata admission remains. This inventory establishes these
captured profiles, not arbitrary datetime/provider equivalence. Default syntax
backend, IDNA wire failures and native reload timing remain open; G0–G7 acceptance
and main merge remain unproven. Coverage is not refreshed.

README canonical map reviewed: topology/ownership/flow/persistence/defaults are
unchanged. Visible baseline and reply-metadata boundary updated; interactive map
unchanged. Exact current gates are retained in
[expiry evidence](docs/audits/go-provider-expiry-native-2026-10-11.json).
Built-in imagegen shows 44→196 captures, 150/196→196/196 matches and open goal items.

Implementation commit: `f230942`. Final full Python regression: 2236 passed,
28 skipped, 74 warnings, 24 subtests (89.24s). Vet, both type audits (449 production
files / 1430 package-level types; 50 packages / 1189 exported types; zero findings),
19 scanner guards and focused Source/native date gates pass. Full ordinary Go
fails only two dotted-I wire assertions and the native observer during CGO build.
Full race fails the same two wire assertions and nine native reload recipes;
no data race is reported. Full Source export fails before freshness comparison
at native Source observer readiness. Those failures remain required evidence;
no retry-until-green, main merge, push, coverage refresh or full goal completion.

## Paired native Source/Go observations on one root — 2026-10-11

Baseline `c36903b`; runtime remains `f230942` plus the existing authority branch.
Probe commit `5d06660`: a standalone typed Go probe and Python driver observe one owned temporary root,
the same `.py` paths and one shared filesystem operation. Go explicitly selects
the existing PythonSource SDK filter; actual Source uses Uvicorn 0.49.0 /
WatchFiles 1.2.0 with FsEventWatcher, recursion/default filter, reload_delay=0 and
rust_timeout=100. Both subscribers acknowledge the same post-subscription probe,
then retain the 3-second operation and 5-second readiness deadlines.

Before the full-Go workload: 15/16 common fixture matches, one common operation
timeout (`added-source`), zero asymmetric results. The subsequent unchanged full
ordinary Go gate fails two dotted-I wire assertions and seven native reload
recipes. Immediately afterward: 8/16 common fixture matches, six common operation
timeouts and two common readiness failures (`lower-mtime-write`, `rename-out`),
zero asymmetric results. All operation-error classifications are verified as
Source operation deadline / Go context deadline rather than arbitrary exceptions.
Thus this cohort reproduces missing delivery simultaneously in actual Source and
Go; failures cannot be assigned exclusively to Go admission/batching from these
observations. This does not establish a kernel/daemon fault or resolve reliability.

Paired subscriptions are a diagnostic intervention. Go uses a new process per
case, Source one process per cohort; process-local registration history is not
matched. Go's PythonSource selection removes filename differences, and does not
exercise CLI GoSource adaptation. Shared preload causality is unproved: there is
no randomized/reversed/isolated workload control. Source and Go callback boundaries
are not instrumented. High fseventsd CPU remains an observation without causality.
No production runtime, fixture, deadline, default or dependency is changed.

Both 16-case control jobs exit 0; all 32 Go probes exit cleanly after Close and
Source workers join through the driver. Two readiness-failure rows use finally
cleanup without a separate native Source __exit__ counter. Probe build, probe vet,
full module vet and git diff --check pass. Full Go's failures are retained. No new
full race/Source export/Python regression/type/coverage/non-Darwin acceptance is
claimed. README canonical map reviewed: topology/ownership/flow/defaults unchanged;
only visible review baseline advances. Architecture graph remains unchanged.
No merge/push; default diagnostics, IDNA and complete G0–G7 remain open.

[Paired observations and hashes](docs/audits/go-reload-same-root-controls-2026-10-11.json)
include executable reproduction sources. Next: distinguish shared volume/event-stream
delivery from process-local registration under an isolated, matched workload before
proposing a listener change or altering a required gate.

## Matched native observer process histories — 2026-10-11

Diagnostic probe commit: `24687ff`.

Baseline `2e8e18d` is now on local main after the user-requested fast-forward;
no push. This slice changes diagnostic probes only. Both actual Uvicorn 0.49.0 /
WatchFiles 1.2.0 FsEventWatcher and Go PythonSource subscribe to the same owned
root, acknowledge one shared readiness file, and consume one operation. The
16 original recipes each run with both sides in fresh processes and both sides
in reused processes. Profile order alternates by recipe, giving 32 paired cases.
Five cases fail readiness before mutation: actual shared operations total 27.

Fresh profile: 13/16 both match, three common readiness timeouts (added-source,
hidden-parent-source, symlink-target-write). Reused profile: 14/16 both match,
two common readiness timeouts (added-source, hidden-parent-source). There are
zero asymmetric Source/Go outcomes and zero operation timeouts after shared
readiness. All 34 child processes exit 0. Source RustNotify __exit__ counters and
Go Close acknowledgements balance 64 opened / 64 closed native subscriptions;
the two reused processes each reach 16/16. PID observations verify process reuse.

Thus process reuse is not necessary for the observed common readiness failures.
This single interleaved cohort does not establish workload, kernel/daemon or
registration causality. It has no isolated volume/daemon control or raw callback
trace. Source's diagnostic timer sets should_exit at 3-second operation / 5-second
probe deadlines, while Go cancels Next; failed subscriptions close before reuse.
This diagnostic is not an acceptance replacement or a claim about default CLI
GoSource. No runtime, fixture, deadline, dependency or default is changed.

Probe build/vet, Python syntax compilation, actual 32-case diagnostic and module
vet pass. Full Python: 2236 passed, 28 skipped, 74 warnings, 24 subtests (99.47s).
Full Go ordinary still fails two required dotted-I wire cases, transient-source
and the native supervisor recovery case. Its shell runs vet afterward; shell
exit 0 belongs to vet, not the failed Go test gate. No new race, full Source
export, coverage or non-Darwin acceptance is claimed. IDNA, default diagnostics,
native reload reliability and complete G0–G7 remain open. README canonical map
reviewed, visible baseline advanced; architecture graph unchanged.

[Raw observations, hashes and full Go failures](docs/audits/go-reload-process-history-2026-10-11.json)
retain cleanup and process evidence. Next: trace delivery at native callback
boundaries with the same acknowledged root/operation before proposing changes to
listener ownership, batching or required assertions. The built-in imagegen
[graphic](docs/audits/go-reload-process-history-infographic-2026-10-11.png) is corrected
to distinguish 32 paired cases from 27 executed shared operations.

## Paired native callback/binding boundaries — 2026-10-11

Diagnostic probe commit: `124222b`.

Baseline `f9f6248` on local main. The retained history controller is reused with
private stderr files, a build-only C overlay and a Source RustNotify proxy.
Production Go/Python sources remain byte-for-byte unchanged. C records epoch,
subscription key, event ID, flags and path before forwarding to Go. Source records
actual RustNotify.watch return before WatchFiles/Uvicorn filtering. These are
different boundaries: Source's internal notify/FSEvents callbacks are not traced.

32 paired cases: fresh processes 15/16 both match, one common operation timeout;
reused processes 15/16 both match, one common readiness timeout. Both failures
are source-directory. Thirty-one shared mutations execute; readiness failure has
no operation. Zero asymmetric outcomes. All 34 children exit 0; Source native
__exit__ and Go Close counters balance 64/64 subscriptions. Matching raw-start
root/key and PID bind Go C traces to each case; Source roots bind binding records.

The fresh failed directory operation is preceded by shared nonce acknowledgement.
Go has two setup/readiness C entries, then zero entries after the operation epoch.
Source has one setup/readiness native-change return, then only timeout/stop returns
and no operation changes. The reused failed readiness case has zero C entries and
no Source native changes. These observations locate this cohort's missing changes
before downstream Go admission/batching and WatchFiles/Uvicorn filtering; they do
not prove a kernel/daemon defect or general reliability. Prior private-queue progress
was measured in another cohort, not repeated here. Logging/context proxies alter
scheduling; stderr uses owned files to avoid a full-pipe stall. Source diagnostic
timers retain their stop-on-timeout distinction from Go Next cancellation.

Overlay build/vet, syntax compilation, the full fixed 32-case experiment, cleanup
assertions and diff checks pass. Full Python: 2236 passed, 28 skipped, 74 warnings, 24 subtests (200.94s).
Full ordinary Go/race, Source export, coverage and non-Darwin gates are not rerun:
production runtime, fixtures, exports and public types are unchanged. The preceding
full Go gate still fails dotted-I, transient-source and native supervisor recovery;
these remain required failures. No dependency, runtime default, fixture or deadline
is changed. README canonical map reviewed, baseline advanced; graph unchanged.
IDNA, default diagnostics, native reload and complete G0–G7 acceptance remain open.

[Paired boundary evidence](docs/audits/go-reload-paired-boundaries-2026-10-11.json)
retains all native/binding logs, the exact overlay C source and hashes. Reproduction
scripts produce the overlay without modifying production files. The built-in
imagegen [graphic](docs/audits/go-reload-paired-boundaries-infographic-2026-10-11.png)
shows the two measured boundaries and preserves the uncertainty. Next: test stream
liveness with an independent owned control root during a failed shared operation,
or isolate host event delivery before proposing listener changes.

## Native Python hostname casing before IDNA — 2026-10-11

Implementation commit: `2c71009`.

Baseline `dce2202`. Installed HTTPX 0.28.1 applies whole-host lower() before
idna.encode; Source uses Python Unicode 14.0.0 and idna 3.18. The existing Go
internal/pytext.Lower already supplies the generated full mappings and contextual
final sigma. Native requests now use it for non-IPv6 hostnames before the existing
standard HTTP IDNA encoder. No dependency, Python runtime call or new domain type.
IPv6 spelling/zones, port normalization, configured audit identity, credentials,
redirect refusal and existing endpoint admission remain intact.

Real direct/SSE HTTP/TCP captures expand from 14 to 34 assertions (17 authority
profiles). The same expanded inventory matches 12/34 before, 34/34 after: 22 failed
assertions are fixed, including the original two required dotted-I failures.
Repeated/mixed dotted-I, uppercase Unicode/sharp-s, Greek final/medial/across-dot/
combining sigma, Armenian and Cyrillic exercise the Source full/contextual mapping.
For example fixture.ΟΣ lowers to fixture.ος, but ΟΣ.invalid lowers to οσ.invalid:
Python sigma context crosses the ignorable dot into the following cased label.
The test also pins Source Unicode version to the native table version. Original
32 ASCII/IPv6 request-object assertions stay required and pass.

Focused actual Source capture, canonical export bytes, all Provider/pytext tests,
Unicode table --check, module vet, both type audits and 19 scanner guards pass.
Type audits: 449 production files / 1430 package-level types; 50 packages / 1189
exported types; zero findings. Full Python: 2236 passed, 28 skipped, 74 warnings,
24 subtests (163.85s). Full ordinary Go passes Provider and fails CLI reload,
callback entry readiness and four native recipes. Full race passes Provider but
fails CLI reload, CGO observer readiness, callback entry readiness and five recipes;
no data-race report appears. Full Source export exits before freshness because
native Source observer did not arm. Focused 34-case wire export is byte-fresh;
this does not replace the full gate. Coverage/non-Darwin acceptance not refreshed.

README canonical Mermaid/boundary and architecture specification reflect the
casing order and updated wire evidence in this slice. Generated HTML passes 9/9
showcase checks, zero errors/warnings; rendered review remains policy-blocked.
Unicode request-object/custom-transport ASCII IDNA representation, general IDNA
validation/normalization/admission and TLS/SNI remain open. Broader dependency
proposal approval is still pending; this casing fix needs none. Default diagnostics,
native reload and complete G0–G7 acceptance are not declared complete.

[Before/after evidence and exact gates](docs/audits/go-provider-host-casing-2026-10-11.json)
retain all 22 baseline failed assertions and final logs. The built-in imagegen
[graphic](docs/audits/go-provider-host-casing-infographic-2026-10-11.png) explicitly
shows a pure-Go table lookup, combining-dot expansion and remaining full gates.

## Independent control-root native liveness — 2026-10-11

Diagnostic probe commit: `2c17414`; runtime baseline `72b6bcf`.
Both native subscriptions watch the same business root and an independent sibling
control root. The retained C build overlay traces before Go forwarding; the actual
Source RustNotify binding traces before filtering. After the shared readiness nonce,
the recipe executes at time zero, a control file is written at one second, and both
workers collect cumulative paths for the full three-second window. Probe readiness
remains five seconds, Rust timeout 100ms. Production runtime, fixture, dependency,
default and acceptance deadlines are unchanged. This adds a root and changes the
diagnostic collection semantics: cumulative paths do not replace first-batch tests.

32 paired samples: 12 common readiness failures before mutation; 20 operations.
Cumulative business paths match both sides in 18 cases and mismatch both in two:
reused same-mtime-write has no source.py; reused symlink-target-write reports
linked.py where the fixture's first-batch expectation is empty. Six control mutations
are received by both sides; fourteen are missed by both. In all fourteen, no matching
control event enters the Go C callback or Source native-change binding return.
Zero asymmetric business/control outcomes. No failed business observation retains
a delivered control sentinel, so the hoped-for root-specific versus whole-stream
liveness distinction is unproven. Logging, a second root, full-window aggregation,
Source stop/Go cancellation differences and concurrent unrelated Go test workloads
limit inference; this is not proof of a host/kernel/daemon defect or a listener fix.

All 34 child exits are zero; 64 native opens and closes balance. Overlay build,
probe vet, repository go vet, syntax compilation, capture assertions and diff check
pass. Python full gate **fails**: 2235 passed, 1 failed, 28 skipped, 74 warnings,
24 subtests (171.96s), test_the_text_the_user_saw_is_kept. One focused diagnostic
run also **fails**: 8 passed, 1 failed (2.16s),
test_a_cancelled_stream_releases_its_connection. Both raw failures are retained;
a fixed-delay cancellation helper does not establish readiness, but causality is
not established here. No retry-to-green, test weakening or production change.
Full Go ordinary/race, global Source export, coverage and non-Darwin are not rerun;
previous native reload failures and incomplete global Source freshness remain open.
README canonical map reviewed; visible diagnostic baseline updated; graph unchanged.
Full IDNA admission, default diagnostics and complete G0–G7 acceptance remain open.

[Control-root audit](docs/audits/go-reload-control-root-liveness-2026-10-11.json)
retains raw C/binding logs, subscription keys, control mutation epochs, independent
terminal gate results, production hashes and the exact overlay source.
[Built-in imagegen graphic](docs/audits/go-reload-control-root-liveness-infographic-2026-10-11.png)
shows the failures and inference limit. Next: make the interrupted-stream test's
observation boundary explicit; isolate native delivery controls before listener changes.

## Event-boundary interruption verification — 2026-10-11

Test implementation commit: `a7f873e`; baseline `d060ac2`.
The Python interrupted-turn helper previously slept 120ms before cancellation,
which did not establish that a stream had entered or shown a partial response.
It now wraps the actual session event capture, waits for assistant_delta, and
holds the captured event boundary until cancellation. Partial-answer and thinking
claims wait for their specific text. A ten-second readiness failure bound prevents
an absent event hanging the test; it is not a widened production/observer deadline.
Cleanup restores the original capture callback and joins any remaining task.
Two added 200ms startup-delay cases retain the connection-exit and partial-answer
assertions. The reusable-session test now compares semaphore permits with its
pre-cancellation value, replacing a self-comparison that could prove nothing.
Production Python/Go, accepted contracts, dependencies and defaults are unchanged.

A private copy of the old helper with 200ms extra first-delta delay yields one
real assertion failure (partial answer missing) and one pass (connection exit).
It therefore demonstrates the partial-text race; it does not reproduce or prove
the cause of the preceding connection-exit failure. An initial external-file
attempt lacked PYTHONPATH and failed collection; this invalid evidence is retained
separately. After correcting only the invocation environment, JUnit confirms an
actual test-call failure. No runtime fix or general scheduling-cause claim is made.

Focused suite: 11 passed (1.82s). Full Python: 2238 passed, 28 skipped, 74 warnings,
24 subtests (158.45s), exit 0. Five selected mutation guards all establish passing
unmodified baselines and failed mutant test calls: interruption-leaves-no-trace,
crash-restore-forgets-the-interruption, interruption-marker-hides-surviving-background-work,
thinking-replayed-as-answer and completed-stream-leaves-stale-partial. The complete
19-module scan gate and diff check pass. These selected guards are not the full
mutation inventory. README architecture map reviewed and visible baseline updated;
runtime graph, specification and generated HTML unchanged. Invariants, full Go
ordinary/race, global Source export, coverage and non-Darwin are not rerun because
production modules, Go types and exports are unchanged. Earlier native reload
failures and incomplete global Source freshness remain open; the prior Python
failures are retained in their original audit, not rewritten as passing.

[Event-boundary audit](docs/audits/go-interruption-test-boundary-2026-10-11.json)
retains all raw gate logs, the controlled old test source and JUnit result.
[Built-in imagegen graphic](docs/audits/go-interruption-test-boundary-infographic-2026-10-11.png)
shows the actual test coordination change and remaining acceptance limits.
Native reload, broader IDNA, default diagnostics and complete G0–G7 remain open.

## Required ASCII path and HTTP target inventory — 2026-10-11

Contract commit: `0ea2210`; production baseline `49dda91`.
The actual Anthropic 0.107.1 / HTTPX 0.28.1 SDK path producer now includes every
printable ASCII character except percent/query/fragment delimiters: 92 added
profiles plus the original 20, direct and SSE, 224 required request assertions.
Native matches 218/224; six fail for literal backslash, caret and pipe. The
actual HTTP wire producer adds the same three paths in both modes: 40 required
wire assertions, 34 matches and six failures. Source preserves the characters;
Go net/url escapes %5C, %5E and %7C, changing real request targets. Host, method,
API-key, dial authority, configured identity and replies remain compared. Only
TCP destination is redirected; HTTP encoders/parsers and actual provider/SDK are
retained. Original 40 object and 34 hostname wire observations still pass.
These are additional required failures, not a diagnostic that excuses differences.

A reproducible standard-library probe compares normal, origin-opaque and
absolute-opaque URL representations for a pipe path. Normal representation
escapes both URL and target; origin-opaque preserves the target but changes the
full URL string and keeps origin-form in WriteProxy; absolute-opaque preserves
full URL spelling but makes the direct target absolute-form. The three measured
representations do not jointly satisfy the captured requirements. This is a
Request.Write/WriteProxy probe, not actual Source proxy/TLS evidence or a proof
about every possible transport design. No Opaque workaround, dependency, admission
relaxation, production change or default change is adopted. Next implementation
needs an owned request-target transport design with proxy/TLS and custom-boundary
contracts; the path-normalization helper alone cannot settle these assertions.

Python focused producer checks: 2 passed (1.17s); full Python: 2238 passed,
28 skipped, 74 warnings, 24 subtests (101.37s), exit 0. Initial focused checks failed
only their old inventory counts; those assertions were updated with the new cases.
Full ordinary Go fails twelve provider assertions across the two inventories plus
native CGO-build readiness, callback-entry waiting and added-source readiness.
CLI passes uncached; some other packages are cached, so no general reload recovery
claim is made. Repository vet, representation probe build/vet, 19 scanner guards
and diff check pass. Full Source --check runs after the Go native gate terminates:
all 234 snapshots are compared, 233 match; the sole drift is WatchFiles
lower-mtime-write changed [source.py] -> null. Exact expected/actual bytes and
manifest are retained; the original WatchFiles fixture is not rewritten.

README canonical map reviewed and status explanations updated. Architecture
cards reflect the retained failures; generated HTML deliver passes 9/9 with zero
errors/warnings. Rendered review remains policy-blocked. Full race, coverage,
production type/invariant audits and non-Darwin are not rerun because production
modules/types are unchanged. Previous native race failures remain open.
Broader IDNA, default diagnostics and complete G0–G7 acceptance remain open.

[ASCII path audit](docs/audits/go-provider-path-ascii-2026-10-11.json) retains both
required failure logs, all independent gate terminal results, Source wire rows,
URL representation results and the complete WatchFiles drift snapshots.
[Built-in imagegen graphic](docs/audits/go-provider-path-ascii-infographic-2026-10-11.png)
shows that this is an expanded acceptance inventory, not a runtime fix.

## Actual HTTP proxy, TLS and CONNECT target contracts — 2026-10-11

Contract commit: `d8992df`; baseline `e4133c2`. Production runtime is unchanged.
Actual SDK/provider requests cover three routes, four paths (ordinary, backslash,
caret, pipe) and direct/SSE: 24 required observations. HTTP forward proxy receives
absolute-form and serves the controlled response itself; it does not forward to a
second HTTP origin. HTTPS direct uses origin-form after TLS. HTTPS proxy first
sends CONNECT fixture.invalid:443, then relays opaque TCP to the owned TLS origin
and sends origin-form. HTTP codecs, TLS handshake, SDK parsing and reply behavior
are real. Explicit trusted test CA and fixture.invalid hostname verification remain
enabled. ALPN offers HTTP/1.1 only. Direct TLS redirects only TCP destination;
proxy routes dial the actual owned proxy. Its ephemeral port is asserted against
the listener and represented by the stable owned-proxy role in captured data.

Native six ordinary controls match; all eighteen special-path application targets
fail. All sixteen SNI observations and eight CONNECT authority/credential-absence
observations match; API-key is present on each application request. Identity, method,
HTTP version, dial authority/role and parsed replies remain asserted. This confirms
routing must select absolute-form for HTTP forward proxy and origin-form for both
HTTPS application routes, separately from CONNECT. It does not implement a fix.
Original six object and six plain-wire failures remain required too.

The capture harness initially hit TCP reset/TLS-shutdown errors. Corrected teardown
aborts only owned remaining sockets after SDK reply validation and client closure,
then joins handlers. Only close errors and post-complete-response tunnel resets are
tolerated; handshake/response-read errors still fail. An initial native attempt
lacked the not-yet-generated fixture and is invalid parity evidence. Raw initial
errors and final gates are retained separately. All 24 final Source handlers/sockets
join. The checked-in CA/key are deliberate public test fixtures, not production
credentials or new dependencies. No production observer deadline is changed.

Source focused: 1 passed (0.88s). Python full: 2239 passed, 28 skipped, 75 warnings,
24 subtests (99.86s). Full ordinary Go fails 30 provider assertions across the
three inventories, CLI invalid-source compilation observation, CGO observer
readiness, rename-in event delivery and rename-source readiness. Some other packages
are cached; no general reliability inference. Vet, 19 scanner guards and diff check
pass. Full Source --check follows terminal Go native gates: 235 snapshots compared,
234 match; sole WatchFiles drift is removed-source changed [source.py] -> null.
Exact expected/actual bytes and manifest are retained; original fixture is unchanged.
Architecture status cards and README baseline/map explanations are synchronized;
generated HTML deliver passes 9/9, zero errors/warnings; rendered review remains
policy-blocked. Full race, coverage, type/invariant audits and non-Darwin are not
rerun because production modules/types are unchanged.

Scope excludes environment proxy discovery, proxy authentication, HTTP/2, arbitrary
certificates and general TLS/IDNA host equivalence. Broader IDNA, default diagnostics,
native reliability and complete G0–G7 remain open. Next: design the owned target
transport while preserving these route, SNI, CONNECT and credential boundaries.
[Proxy/TLS audit](docs/audits/go-provider-proxy-tls-2026-10-11.json) retains all raw
terminal gate logs and Source observations. [Built-in imagegen graphic](docs/audits/go-provider-proxy-tls-infographic-2026-10-11.png)
shows Source wire examples, native failures and the scope limits.

## Literal request target native adoption — 2026-10-11

Implementation `978b4fd` preserves Source original path bytes and installs an owned
standard Transport clone only for literal backslash/caret/pipe paths. Proxy policy
runs once on original metadata; a wire URL clone selects route-specific forms.
Caller transport remains unchanged; explicit idle cleanup is available on direct
and streaming clients. Ordinary/custom transport selection retains its documented
boundary. Source raw_path maps to URL.RawPath; Go EscapedPath remains canonical.

| Required scope | Previous matches | Current matches |
| --- | --- | --- |
| Request URL / raw path, direct and SSE | 218/224 | 224/224 |
| Actual plain HTTP wire | 34/40 | 40/40 |
| HTTP proxy / verified TLS / CONNECT wire | 6/24 | 24/24 |
| TLS SNI | 16/16 | 16/16 |
| CONNECT authority / API-key absence | 8/8 | 8/8 |

All original failures/counts remain asserted; expected fixtures are unchanged.
Adapter tests retain proxy/refusal, request ownership and idle-pool lifecycle checks.
Provider ordinary/race, Python 2239 tests (28 skipped, 24 subtests), vet, both type
audits, 19 scanner guards and diff check pass. Full ordinary Go fails CLI/reload;
full race coverage fails native reload and maximum-decision SQLite restart deadline.
No race warning is reported. Source full --check aborts before fixture comparison
when the native Source observer does not arm. No retry/deadline widening or fallback.

Current failed-run Go statement coverage: application 32663/36613 = 89.21%, all
Go 32784/36771 = 89.16%, Provider 1515/1748 = 86.67%. All package commands ended;
the profile is measurable, but the gate failed. Last passing recorded profile is
f2cdbca (89.34% / 89.31%). Native C/CLI subprocess internals, Python/branch coverage
and port completion are outside this Darwin arm64 / Go1.23.3 / CGO denominator.
Architecture README/spec/generated HTML are synchronized, showcase 9/9; rendered
review remains policy-blocked. General IDNA/admission, arbitrary custom wire, ambient
proxy/authentication/HTTP2, default diagnostics, native reliability and G0–G7 remain
open. No dependency or push. [Boundary/gate evidence](docs/audits/go-provider-literal-target-2026-10-11.json)
and [coverage receipt](docs/audits/go-coverage-literal-target-2026-10-11.json) preserve
exact failures and profile aggregation/hash. Built-in imagegen produced the
[iteration graphic](docs/audits/go-provider-literal-target-infographic-2026-10-11.png).

## SQLite maximum-decision shared-budget diagnosis — 2026-10-11

Production/test baseline `14e3b02`; runtime, original test and fixture are unchanged.
A maintained overlay script adds only phase/model-call logging to the actual
maximum sqlite-reopen subtest. The original 512KiB payload, digest, backend-once,
exact retained replay, current readonly refusal, lease/transcript assertions and
30-second shared context remain intact. Four distinct modes execute once each:

| Mode | Maximum subtest wall time | Result |
| --- | --- | --- |
| Ordinary | 4.65s | Pass |
| Race only | 14.00s | Pass |
| Atomic coverage only | 8.98s | Pass |
| Race + atomic coverage + CPU profile | 30.26s | Fail |

In the combined diagnostic, payload preparation ends at 2.681s; first turn ends
at 29.218s; action read/digest at 29.500s; restore ends at 30.255s. Replay starts
with the shared context already expired and fails. This localizes this diagnostic
failure before replay execution; it does not reconstruct timing of the earlier
full-suite failure, prove a SQLite defect, or justify changing the test budget.
CPU samples are 74.61% runtime._ExternalCode and cannot attribute the dominant
cost to SQLite or any specific encoder. JSON per-rune helpers appear among the
remaining samples but are not proven as the cause. Overlay logging, CPU profiling
and host load can affect timings; one run per mode is not a statistical baseline.

Original source hashes and logs are retained. Script syntax compilation and the
preflight refusal of an already-completed mode pass without rerunning it. Each
native command terminated; no live job or host-service intervention remains from
this diagnostic. The profiling binary was moved to the named /tmp artifact; final
script emits it outside the checkout. README canonical map reviewed, visible
baseline advanced; topology/ownership/authority/persistence/defaults and generated
architecture remain unchanged. Full Python regression passes: 2239 passed, 28 skipped, 74 warnings, 24 subtests passed in 166.73s (0:02:46).
Diff check passes. No full ordinary/race Go, Source export, type/scanner/mutation,
coverage refresh or non-Darwin gate is rerun:
production/test/inventory targets are unchanged. Earlier global failures remain.

[Reproducer](docs/audits/go-sqlite-decision-budget-2026-10-11.py),
[exact logs / overlay / CPU audit](docs/audits/go-sqlite-decision-budget-2026-10-11.json),
and [built-in imagegen graphic](docs/audits/go-sqlite-decision-budget-infographic-2026-10-11.png)
retain the evidence. Next measure concrete serialization/validation stages and
reduce verified costs while retaining full byte/guard contracts; do not infer a
SQL or host cause from runtime._ExternalCode. Full G0–G7 remains open; no push.

## Native scalar JSON text span adoption — 2026-10-11

Implementation `b85b5c5`. Scalar UTF-8 guard uses standard validation; writer copies
safe spans between JSON escapes. Malformed native bytes retain RuneError projection;
surrogatepass remains refused at strict/legacy UTF-8 byte boundaries. ASCII archive
text except DEL reuses this byte-equivalent path. Unicode/DEL/surrogate archival
escaping, numeric variants, insertion order and budget/authority schemas are unchanged.

Fourteen real CPython text/object-key recipes yield 56 required native encoding
comparisons; all pass, including large/quoted/Unicode/control/DEL/scalar-edge data.
Old fixtures and the decision512KiB/30s context/replay/readonly checks remain intact.
Three-iteration race+atomic samples: UTF-8 ASCII343.98→2.24ms, escaped340.39→3.44ms,
Unicode149.84→4.13ms, archiveASCII166.55→2.50ms. Not a statistical or service
throughput baseline. Escaped allocations30→18 but B/op2538240→2775056; retained
tradeoff. Archive setup is amortized over the same three iterations in both versions.

Focused codec packages, Python2239 (28 skipped/24 subtests), vet, both zero-finding
type audits,19 scanner guards,77 invariant declarations,4 selected nonfinite mutation
guards and diff check pass. Full ordinary fails CLI/native reload; full race fails
native reload readiness/callback/recipes. SQLite passes both full gates. Separate
race+atomic decision checks still fail at readonly turn (33.91s initially,31.30s
finally); original30s budget is not widened. No end-to-end closure is claimed.

Full Source export, after terminal native jobs, aborts at native observer readiness
before comparison. Nominal inventory236; new fixture canonical bytes independently
match. Frozen source/test/fixture hashes and all logs are retained in
[the audit](docs/audits/go-json-text-spans-2026-10-11.json). No aggregate coverage
refresh, dependency or non-Darwin gate. Canonical architecture reviewed; baseline/
boundary updated, graph unchanged. General IDNA/default diagnostics/native reliability
and complete G0–G7 remain open. [Built-in infographic](docs/audits/go-json-text-spans-infographic-2026-10-11.png)
shows modified writers, measured byte contracts and unresolved acceptance.

## Post-span maximum SQLite shared-budget profile — 2026-10-11

Runtime `18920b3` remains unchanged. One race+atomic+CPU-profile execution of the
original maximum512KiB/30s shared-context fixture fails in readonly RunWithContext
at33.69s. First turn6.624s, restore7.559s and replay18.930s finish; fifth model
callback starts26.068s. Exact replay/readonly/backend-once checks and Source
fixture are unchanged. This is not an ordinary/full gate or timeout closure.

CPU samples31.54s over33.85s include27.17s/86.14% unresolved
runtime._ExternalCode. No attribution to SQLite/CGO/race/JSON or statistical
comparison with prior runs. Next evidence is a native stack from the exact owned
test process; small visible Go samples do not identify the dominant cause.

The overlay diagnostic records actual Git baseline and decision fixture SHA256.
Three preflight checks reject historical revision, fixture mismatch and repeated
mode without dispatch or receipt mutation. All1158 Go source/fixture/module hashes
remain unchanged. Python full2239/28skipped/74warnings/24subtests (101.49s) and
diff check pass. Full Go/Source/coverage/non-Darwin gates are not refreshed;
previous native watcher failures and complete G0–G7 acceptance remain open.
README map/boundaries reviewed, visible baseline updated; graph unchanged.
[Raw receipts](docs/audits/go-decision-post-spans-2026-10-11.json) and
[built-in infographic](docs/audits/go-decision-post-spans-infographic-2026-10-11.png).

## Owned native decision readonly-stage stacks — 2026-10-11

Go baseline `21a91e6`; runtime/test/fixture unchanged. Optional native-sample mode
compiles then directly launches the exact race+atomic binary in the package cwd,
verifies owned PID/executable and samples5s at readonly-start. Default modes remain
serial; invalid native mode, unrelated binary and repeated completed mode are
rejected before dispatch/attach/receipt mutation. Two setup failures are preserved:
wrapper saved-output/executed-path mismatch and direct-launch wrong fixture cwd.

Successful PID72601 sample resolves JSON string-loop → sync/atomic → TSAN
chains; reader disassembly verifies emitted atomic coverage operations. Native
collapsed counts atomic-fetch-add1446/read387/atomic-store263 are not CPU shares.
Sample command8.237s includes symbol processing. Full-run pprof84.54% external
CPU remains unresolved; it cannot be wholly attributed from a5s window.
Original512KiB/shared30s/readonly/replay/backend-once test still fails at34.04s.
No ordinary performance or timeout-closure claim. Next measure a safe string-span
reader with byte/surrogate/control/invalid-byte/ownership proofs.

All1158 Go source/fixture/module hashes unchanged. Python2239/28skipped/74warnings/
24subtests (99.02s), refusal checks and diff check pass. Full Go/Source/coverage/
non-Darwin gates not refreshed; canonical architecture reviewed, graph unchanged.
Native reload/default diagnostics/broader IDNA/full G0–G7 remain incomplete.
[Receipts](docs/audits/go-decision-native-stacks-2026-10-11.json),
[full native sample](docs/audits/go-decision-native-stacks-2026-10-11.sample.txt),
[disassembly](docs/audits/go-decision-native-stacks-2026-10-11.reader.disasm.txt)
and [built-in infographic](docs/audits/go-decision-native-stacks-infographic-2026-10-11.png).

## Native immutable JSON string read spans — 2026-10-11

Implementation `014e913`, prior baseline `05d74cf`. Reader copies safe byte spans; unescaped
short strings.Clone preserves detached input ownership. Existing Unicode escapes/
surrogatepass/raw native bytes/duplicate-key replacement/limits/wire refusals stay
unchanged.14 Source recipes retain56 byte hash checks and56 decode projections;
directed grammar/control/raw-byte tests and1MiB backing-allocation checks pass.
No erased domain type/dependency/default/schema/authority change.

Instrumented3-iteration samples: ASCII105.65→1.69ms,escaped108.89→3.37ms,
Unicode99.28→1.54ms; not normal-runtime/statistical/service throughput. Original
512KiB/shared30s decision focused race+atomic passes baseline3.76/maximum22.18s
with exact replay/readonly/backend-once checks. Full SQLite passes ordinary and
race+atomic46.670s. Full ordinary fails CLI/native reload; full race+atomic fails
ignored-hidden/ignored-backup/ignored-text/transient-source native recipes. No
DATA RACE report; variability is not attributed to the reader optimization.

Vet/type audits (450 files1434 types;50 packages1189 public types,zero findings),
Python2239/28skipped/74warnings/24subtests (93.93s),19 scans,4 selected nonfinite
mutations and diff check pass.1159 Go file hashes frozen. Source export not
triggered/refreshing; previous native observer failure remains open. No Python
module change/non-Darwin/rendered acceptance. Canonical architecture reviewed,
visible baseline/boundary updated, graph unchanged. Failed full-run coverage
app89.2073%/all89.1531%/Provider86.6705%,not passing migration acceptance.
[Coverage](docs/audits/go-coverage-json-read-spans-2026-10-11.json),
[full evidence](docs/audits/go-json-read-spans-2026-10-11.json),
[built-in infographic](docs/audits/go-json-read-spans-infographic-2026-10-11.png).
Native reliability/default diagnostics/broader IDNA/full G0–G7 remain incomplete.

## Required native empty-batch versus absence Source capture — 2026-10-11

Runtime `b22a005` unchanged. Fresh four required Source captures, after matching
the original builder's canonical scratch root, all match their old fixture: three
ignored names produce[],transient-source yields flash.py; four Source contexts
closed (watchfiles1.2.0/Uvicorn0.49.0). Initial unresolved-alias arm errors retained
as diagnostic setup failures. No fixture/producer/test/deadline/fallback changes.

Original unchanged four-case Go race+atomic cohort passes hidden/backup/transient,
fails ignored-text with no post-operation admitted diagnostic and original3s wait.
Source-only correction does not repeat Go.[] and absence stay distinct; separate
roots/times are not causal/temporal acceptance. Filename filtering follows the
admitted diagnostic; earlier change/root fences and raw C callback remain unproven.
Next capture raw C before Go admission rather than weaken golden assertions.

Six relevant source hashes unchanged, repeated-prefix refusal preserves receipt.
Python2239/28skipped/74warnings/24subtests (90.51s) and diff check pass. Full Go/
coverage/Source/non-Darwin acceptance not refreshed. Canonical map/boundaries
reviewed; baseline updated, graph unchanged. Native reliability/default diagnostics/
broader IDNA/full G0–G7 remain incomplete.
[Collector](docs/audits/go-reload-required-absence-2026-10-11.py),
[exact receipts](docs/audits/go-reload-required-absence-2026-10-11.json),
[built-in infographic](docs/audits/go-reload-required-absence-infographic-2026-10-11.png).

## Required native raw C callback-window evidence — 2026-10-11

Runtime baseline `d375a68` unchanged. One original four-case race+atomic diagnostic
cohort passes ignored-hidden and fails backup/text/transient at original3s wait.
All four subscriptions have readiness C events. OP-to-cleanup-entry classification
observes1 C batch/item for hidden and zero for each failed case. This excludes
subsequent filename filtering as an explanation for this cohort's absences; kernel
cause, queue liveness, registry/fence state and temporal acceptance stay unproven.
Attempted CGO Go overlay markers were not observed; receipt preserves exact executed
source and full overlays. Reusable collector removes those unproven markers; no
cohort replay after correction. No producer/fixture/runtime/timeout/default/dependency
change. Next probe needs same-subscription queue and operation/control evidence.

Original command exits1 (26.277s; package23.044s). Window-parser/refusal preflights,
production hashes, Python2239/28skipped/74warnings/24subtests (98.47s) and diff check
pass. Audit-only changes do not refresh full Go/vet/race/coverage/Source gates or
non-Darwin acceptance. Canonical map reviewed, visible baseline updated, graph
unchanged, no rendered acceptance/push. Native reliability/default diagnostics/
broader IDNA/full G0–G7 remain incomplete.
[Collector](docs/audits/go-reload-required-callbacks-2026-10-11.py),
[full evidence and classification](docs/audits/go-reload-required-callbacks-2026-10-11.json).

## Same-subscription queue and post-result native control — 2026-10-11

Runtime `4454186` unchanged. Diagnostic100ms timer uses the same C serial stream
queue; distinct control is written only after original result, with its own3s
cleanup window. Original readiness/operation assertions and deadlines remain;
timer/extended cleanup alter scheduling, so no temporal acceptance claim.

One targeted race+atomic cohort exits1: hidden fails readiness before operation,
backup/text/transient operations pass. Hidden has300 first-tick-to-result marks,
max gap110.977ms, then its control C event arrives0.569941s later. All four controls
have one C item; no late business-path item in their control windows. This excludes
a sustained whole-queue block in this cohort, not readiness loss/daemon cause/
concurrent control reliability. Earlier operation absences remain unresolved.

Full command/log/overlays/executed collector/hashes retained; parser distinguishes
late business from control and prefix refusal prevents completed-cohort dispatch.
Python2239/28skipped/74warnings/24subtests (138.69s) and diff check pass. Audit-only
changes do not refresh full Go/vet/race/coverage/Source/non-Darwin acceptance.
Canonical map reviewed, baseline updated, graph unchanged; no rendered acceptance
or push. Native reliability/default diagnostics/broader IDNA/full G0–G7 remain open.
[Collector](docs/audits/go-reload-required-queue-control-2026-10-11.py),
[exact evidence](docs/audits/go-reload-required-queue-control-2026-10-11.json).
