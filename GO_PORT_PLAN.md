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
      run provenance, action/approval/session records and current archival event decoding implemented;
      goal/plan-mode inputs/events and closed decision request/result variants are implemented; other event and state variants remain)
- [ ] G1 session loop (typed requests, four-layer context compaction, in-memory
      fake-provider slice, cache annotation, stuck detection, scoped child execution,
      exhaustion markers and cancellation repair implemented;
      managed admission/cancellation and bounded subscriptions implemented;
      prompt hooks/injectors, Todo nagging, shared limiters and ordered parallel batches
      implemented; bounded steering and live posture updates implemented;
      direct HTTP provider/SDK retries, typed SSE and provisional progress implemented;
      default Agent recovery implemented; default goal continuation is implemented; remaining context integrations remain)
- [ ] G2 execution gate (typed catalogue, ordered gate, basic modes and
      workspace read/write/edit/glob plus todo/skill/question handlers implemented;
      compress defers a real summary after the batch and task delegates through
      a bound provider; optional action replay, journal transitions and bound approval
      broker/session grants, optional registry masking and real shell execution are implemented;
      injected-store restore-time expiry implemented; SQLite approval backend
      and remaining sink masking remain)
- [ ] G3 HTTP/SSE (process-local fleet manager, owner-scoped library lookup,
      workspace policy and draining delete/stop implemented; token/anonymous auth,
      twenty-eight HTTP method/path operations, mode/steering, completed-boundary fork and process-local SSE implemented;
      typed settings, standalone HTTP launcher, embedded default skills and private spill store implemented;
      per-run file recording and owner-scoped read/export implemented;
      typed HTML ledger, offline traceview CLI and filtered record visitor implemented;
      owner-scoped injected-store bounded SSE catch-up and transcript epoch reads implemented;
      embedded public console/UI shells implemented; optional UI data routes, full health posture, native SQL restart evidence,
      optional routes and complete validation semantics remain)
- [ ] G4 provider (direct HTTP, typed normalization, bounded SDK retries, SSE assembly
      and streamed-text cancellation repair implemented;
      default Agent recovery, explicit operator Jev HTTP library, isolated complete-only decision queries, configurable coalescing and stateful signed fake clients
      implemented; advanced variants/options and live-provider audit remain)
- [ ] G5 persistence (per-run JSONL evidence, concrete state consumer contracts and archival event decoder implemented; actual Python SQLite and AgentSession probes captured; configured live state injection, request guards, epochs, masking and confirmed lease-loss cancellation implemented; injected-store manager restoration, lease-gated approval expiry and crash-tail repair implemented; scheduled stable-ID restore, cron resolution, injected-store bounded SSE catch-up, transcript epoch reads and disarmed goal fold implemented; Go SQLite backend/restart evidence remain)
- [ ] G6 optional features (typed persistent task graph, five explicit library tools and owned Tasks HTTP view implemented; operator worktree lifecycle/task binding, explicit typed managed factory with source directory deletion, and five gated model tools with serialized workspace rebinding implemented; typed operator background service with merged byte capture/retention/orphan records implemented; explicit native-session background tools/Bash dispatch/completion injection/interruption markers and prepared execution rebind implemented; manager delete/stop joins and explicit standalone selection implemented; selected child activation with qualified IDs, independent queues and retained lifetime cleanup implemented; explicit typed cron operator parsing/controls/persistence/claims/disarmed restore and cancellable ticker/run ownership implemented; manager-owned cron with fresh untrusted turns, owner-scoped operations, delete/stop joins and standalone startup implemented; three closed cron model tools, four owned operator HTTP operations and explicit standalone selection implemented; explicit plan-mode tools/reviewer/prompt integration and log-folded restoration implemented; five explicit goal tools, CAS snapshots, bounded default stop consumer and disarmed restoration implemented; canonical user skills, exact private owner directories and explicit layered agent/user catalogues implemented as libraries; typed Markdown owner memory storage implemented as an explicit library; immutable owner resource snapshots, anchored create-only files and detached pre-commit catalogues implemented as libraries; operator create-only user publication implemented; explicit trusted manager/runtime resource snapshots and optional owner-bound remember/recall tools, automatic selection and change-only context index implemented; scoped extraction/consolidation and contained memory capture at the actual source endpoints implemented; shared manager memory fallback with exact owner binding and launcher root/tool/auto selection and explicit typed owner/session-bound draft storage and source-compatible pure skill evidence projections and trusted completed-turn evidence capture and typed source candidate parsing and typed preview business flow implemented; standalone native model binding and manager-owned draft pool injection implemented; owned manager preview and reviewed commit implemented; routes remain; other groups remain; source Git-aware cleanup is absent)
- [ ] G7 differential and release audit

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
   bounds with actual source memory/SQLite-reopen operations; native SQL reopen
   remains open. Snapshot 56 now compares cooperative cancellation and sink
   masking, including a deliberately stronger native escaped-result guard.

### Next persistence slices

1. Contracts: concrete session projection, separate tenant/process identities,
   transcript epochs, audit-preserving deletion, typed archival event decoding
   and actual Python SQLite probes are implemented. SQL outcomes remain Python
   evidence until compared against a Go backend.
2. Backend: after explicit dependency approval, implement schema v7 and additive
   migrations, one owned connection, WAL/NORMAL/foreign-key posture, transactional
   append/rollback, conditional leases and action/approval rows. Run the same
   probe recipes against real Go SQLite, including concurrent connections.
3. Session integration: configured-only live composition, persistence-before-request,
   rewrite epochs, secret projections, owner/bound-workspace retention, explicit
   default unknown-action handling, post-admission claims and drain/release are
   implemented. Explicit manager restoration, parked approval expiry and crash-tail
   repair are now implemented over the injected backend. Remaining: real backend
   integration; scheduled restoration is now implemented over the injected seam. Restore lease confirmation separately
   from recorded ownership; no human authority or goal/cron activation is restored.
4. Serving: restored cron resolution is implemented with a fresh untrusted turn;
   bounded owner-scoped SSE event catch-up and persisted transcript epoch reads
   are implemented over the injected store;
   native SQL reopen and explicit state-backend launcher selection remain. Compare real restart/crash windows and ownership paths
   before claiming G5 complete; a lease is not external-effect fencing.

### Next cron slices

1. Completed: explicit operator scheduler: five-field matching, typed jobs,
   per-session controls, bounded problems, disarmed restoration, masked atomic
   persistence, exclusive occurrence claims and cancellation/join. Compare actual
   Python operations and disk state, including loss-before-dispatch boundaries.
2. Managed untrusted turns, session deletion/stop and standalone startup are
   implemented, together with closed tool variants, owned operator HTTP activation
   and explicit launcher selection. Arm remains an
   operator act, never a model tool. Keep comprehensive activation unavailable
   until all feature groups are implemented.
3. Lazy stable-ID session restoration and its lease/owner/workspace paths are now
   implemented and compared to actual source. Audit real SQL restart and
   differential failures in G5/G7;
   an exclusive claim file does not provide an exactly-once transaction with
   external effects or repair stale whole-file state across live writers.

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
