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
      usage, stop/error/subagent events, run provenance and action records implemented;
      other event and state variants remain)
- [ ] G1 session loop (typed requests, four-layer context compaction, in-memory
      fake-provider slice, scoped child execution, exhaustion markers and cancellation repair implemented;
      complete lifecycle events and parallel batches remain)
- [ ] G2 execution gate (typed catalogue, ordered gate, basic modes and
      workspace read/write/edit/glob plus todo/skill/question handlers implemented;
      compress defers a real summary after the batch and task delegates through
      a bound provider; optional action replay and journal transitions are implemented;
      durable approvals, SQLite backend and masking remain)
- [ ] G3 HTTP/SSE
- [ ] G4 provider
- [ ] G5 persistence
- [ ] G6 optional features
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
