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

- [ ] P0 contract inventory (`GO_PARITY_MATRIX.md` has the capability baseline;
      route schemas, storage columns, and per-tool fixtures remain)
- [ ] P1 Python directory split (files moved and server smoke passes; final
      full-suite timing gate remains unstable under host load)
- [ ] G0 typed Go contracts (message and bash input slice implemented)
- [ ] G1 session loop (in-memory fake-provider slice implemented)
- [ ] G2 execution gate
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

Next: finish P0's concrete route, storage, and tool fixtures; finish G0 with
the remaining default tool inputs, model response and event variants; then
complete the G1 loop and build the G2 execution gate before exposing a Go server.
