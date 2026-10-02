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
      usage and stop events implemented; other event and state variants remain)
- [ ] G1 session loop (in-memory fake-provider slice and cancellation repair implemented;
      request construction, bounded events, and parallel batches remain)
- [ ] G2 execution gate (typed catalogue, ordered gate, basic modes and
      workspace read/write/edit/glob implemented; durable approvals,
      action journal, masking and remaining handlers remain)
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
