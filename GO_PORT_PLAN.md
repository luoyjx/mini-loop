# Go port plan

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

- [x] P0 contract inventory (`GO_PARITY_MATRIX.md`, the generated Python
      contracts in `go/testdata/`, and Go cases for each default tool input)
- [x] P1 Python directory split (server smoke, complete Python suite, and
      repository verifiers pass from the documented layout)
- [ ] G0 typed Go contracts (messages, all default inputs, completed replies,
      usage, core lifecycle/status/cancel/stop/error/stuck/subagent/approval events,
      run provenance, action and approval records implemented;
      other event and state variants remain)
- [ ] G1 session loop (typed requests, four-layer context compaction, in-memory
      fake-provider slice, cache annotation, stuck detection, scoped child execution,
      exhaustion markers and cancellation repair implemented;
      managed admission/cancellation and bounded subscriptions implemented;
      prompt hooks/injectors, Todo nagging, shared limiters and ordered parallel batches
      implemented; bounded steering and live posture updates implemented;
      direct HTTP provider/SDK retries, typed SSE and provisional progress implemented;
      default Agent recovery implemented; remaining context integrations remain)
- [ ] G2 execution gate (typed catalogue, ordered gate, basic modes and
      workspace read/write/edit/glob plus todo/skill/question handlers implemented;
      compress defers a real summary after the batch and task delegates through
      a bound provider; optional action replay, journal transitions and bound approval
      broker/session grants, optional registry masking and real shell execution are implemented;
      SQLite approvals, restore-time expiry
      and remaining sink masking remain)
- [ ] G3 HTTP/SSE (process-local fleet manager, owner-scoped library lookup,
      workspace policy and draining delete/stop implemented; token/anonymous auth,
      twenty-three HTTP method/path operations, mode/steering, completed-boundary fork and process-local SSE implemented;
      typed settings, standalone HTTP launcher, embedded default skills and private spill store implemented;
      per-run file recording and owner-scoped read/export implemented;
      typed HTML ledger, offline traceview CLI and filtered record visitor implemented;
      embedded public console/UI shells implemented; optional UI data routes, full health posture, durable catch-up,
      optional routes and complete validation semantics remain)
- [ ] G4 provider (direct HTTP, typed normalization, bounded SDK retries, SSE assembly
      and streamed-text cancellation repair implemented;
      default Agent recovery, configurable coalescing and stateful signed fake clients
      implemented; advanced variants/options and live-provider audit remain)
- [ ] G5 persistence (per-run JSONL evidence implemented; session/lease/SQLite restore remains)
- [ ] G6 optional features (typed persistent task graph, five explicit library tools and owned Tasks HTTP view implemented; operator worktree lifecycle/task binding, explicit typed managed factory with source directory deletion, and five gated model tools with serialized workspace rebinding implemented; typed operator background service with merged byte capture/retention/orphan records implemented; explicit native-session background tools/Bash dispatch/completion injection/interruption markers and prepared execution rebind implemented; manager delete/stop joins and explicit standalone selection implemented; selected child activation with qualified IDs, independent queues and retained lifetime cleanup implemented; other groups remain; source Git-aware cleanup is absent)
- [ ] G7 differential and release audit

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
