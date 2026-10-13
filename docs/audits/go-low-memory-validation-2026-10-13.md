# Memory-aware validation after code commitment

## Delivery and current boundary

At the user's explicit request, the previous implementation was committed before
memory optimization: Source `6dc4956`, runtime/API/architecture `a9b1bf4`, delivery
evidence `7fad16d`. Local main remains `69cba43`; nothing was merged or pushed.
Prior passing focused/Source/type checks and interrupted full Go logs are retained.
Full migration acceptance and current coverage remain incomplete.

The current optimization changes CLI tests, not product runtime defaults. The
initial preparation was formatted and statically reviewed. A read-only preflight
at 14:48:10 UTC immediately returned memory-pressure level 2. A later one-shot
observation returned normal level 1, but the monitored regression-test attempt at
15:01:49 UTC failed its stability preflight with level 2 before launching any
compiler or test process. The 1,769 frozen inputs were unchanged. Do not treat
either interception as a test pass. The following continuations passed focused
checks, then a full-stage attempt was interrupted by host pressure. Complete
validation remains resource-blocked.

### Continuation at 15:07–15:10 UTC

The normal-pressure stability gate admitted two serial stages on the same 1,769
unchanged inputs. Both actual CLI reload scenarios retained their rebuild,
syntax-failure recovery, listener and dotenv assertions.

| Stage | Result | Monitored duration | Sampled tree peak RSS | Peak sampled CPU |
| --- | --- | --- | --- | --- |
| Two budget-construction regressions | passed | 3.459 s | 48.16 MiB | 16.9% |
| Native and polling real CLI reload | passed | 43.718 s | 403.91 MiB | 106.1% |
| Full ordinary Go suite | preflight resource-blocked | no workload launched | unavailable | unavailable |

The native and polling tests themselves took 20.89 s and 19.72 s. Observed
processes all retained nice 10, host pressure stayed normal during both running
stages, and two exit checks found no remaining observed process identities.
At 15:10:08 UTC, the next full-suite stage encountered pressure level 2 before
launch. Vet, race, full-package coverage and the remaining acceptance gates
have not run. Main is unchanged and coverage is not refreshed.

The new reload peak sample was dominated by the Go linker (195.33 MiB) and
`dsymutil` (122.42 MiB), alongside four smaller processes. This is useful
per-process attribution unavailable in the old aggregate log. The old full-Go
peak and new focused-reload peak have different workload/cache conditions;
their small difference does not prove a memory reduction. No unrelated process
was stopped to obtain these results.

### Third resumed goal turn: full-stage interruption at 15:24 UTC

The resource-budget fix and focused evidence are committed as `8a5eec4`.
The worktree was clean at the next turn's start. Initial pressure was level 2;
the 15:22:20 UTC observation returned 1, so the runner attempted a fresh full
stage only after all 30 stability samples passed. `go-full-resume-1` then ran
for 70.378 s including controlled cleanup. It was interrupted when pressure
rose to 2, with a sampled peak of 277.06 MiB and peak CPU of 99.5%. The final
pressure-triggering sample contained only 124.19 MiB of observed owned RSS.
Cleanup recorded no errors and no remaining observed process identities.

The partial stream contains 673 top-level passes and one native-reload build
failure. That failure's output arrived at 15:24:33.235 UTC, approximately 0.10 s
after the pressure-2 sample; polling then emitted `signal: terminated`. Exact
signal timestamps and the builder's stderr were not retained, so this overlap
does not independently establish a code defect or prove its precise cause.
The full run is resource-blocked, not passed. Earlier focused passes remain
valid for their own scope. No later gate or current coverage was produced.

This is the third consecutive goal turn with the same full-validation resource
blocker after the user's request to commit first and optimize memory. All task
validation handles are terminal. No necessary independent implementation change
was identified; completing acceptance requires stable external memory conditions.
The goal is eligible for `blocked`, with the original migration scope preserved.
No unrelated process was stopped. Local main remains `69cba43`.

## Current host evidence

The 14:35:21 UTC snapshot reports 16 GiB physical RAM, warning pressure level 2,
5.86 GiB occupied by the memory compressor, and 449.25 MiB swap usage. Compressor
contents represent about 11.31 GiB before compression; that number must not be
added to occupied physical compressor memory.

The largest sampled RSS was `gopls` PID 76609: 1.30 GiB, parent Claude PID 5814,
working directory `/Users/yanjixiong/crdt-workspace/CascadeProjects/crdt-redis`.
It belongs to another task. Permission to end that specific language service was
requested; no permission has been received and no unrelated process was stopped.
RSS alone does not identify the sole cause of system pressure.

## Concrete optimization

`go/cmd/miniloop/reload_process_test.go` rebuilt its CLI environment with a fixed
`GOMAXPROCS=2`, dropping the outer `GOFLAGS`, `GOMEMLIMIT` and `GOGC`. That child
launches actual Go rebuilds. The prepared fix gives startup and reload test
processes a shared test-only whitelist for these four variables. Parent values
take precedence, duplicate resource keys are removed, and an absent/empty parent
GOMAXPROCS keeps the previous test default of 2. Other absent resource keys remain
absent. Explicit application values remain isolated from ambient credentials.

Regression tests inspect the actual startup/reload command constructors and verify
budget propagation, deduplication, defaults, detached slices and credential
isolation. Existing real rebuild, syntax-failure recovery, listener and dotenv
assertions are unchanged. Focused compilation and both real reload scenarios
now pass; measured before/after savings remain unproven.

The previous full-Go tree peaked at 409.59 MiB during the native reload test window;
up to seven observed processes overlapped there. Those two scenarios took 31.30 s.
Old samples contain aggregate RSS and PID lists, so they cannot assign that peak
to an individual process. At the eventual host-pressure stop the owned tree was
only 51.89 MiB; lowering this task's memory may not resolve the whole host's pressure.

## Next execution profile

The new local runner is retained under `var/go-low-memory-validation-2026-10-13/`.
It admits one named stage after 30 consecutive normal-pressure samples, refuses
overlapping runs and output replacement, and saves input hashes before/after.

| Control | Prepared value |
| --- | --- |
| Go scheduling / compiler / test concurrency | `GOMAXPROCS=1`, `-p=1`, `-parallel=1` |
| Go heap policy | `GOMEMLIMIT=256MiB`, `GOGC=50` |
| Priority | nice 10, verified from process samples |
| Task-tree RSS stop | 768 MiB, sampled |
| Sustained CPU stop | above 150% for 30 s |
| Host pressure stop | any level other than normal 1 |
| Process observations | PID, parent, group, start time, command name, RSS, CPU, nice |

These are operating controls, not hard RAM caps. The monitor targets observed
task-owned process identities and keeps cleanup results; no global settings or
unrelated processes are changed. Short-lived children can escape samples. The
runner passed syntax parsing and independent static review. Its stability gate,
live sampling and normal-exit identity checks have executed. Forced cleanup has
not been exercised in this continuation.

The attempted narrow command was:

```sh
.venv/bin/python var/go-low-memory-validation-2026-10-13/run.py \
  --stage cli-budget-regressions --cwd go -- \
  go test -count=1 -json ./cmd/miniloop \
  -run '^(TestCLIProcessInheritsResourceBudget|TestCLIResourceEnvironmentDefaultsAndDeduplicates)$'
```

It returned exit 75 (`resource_blocked`, `launched: false`). A future attempt must
use a new stage name; preserve this attempt's output. The later
`cli-budget-regressions-resume-1` stage ran the same requested command and passed.

The narrow environment-regression tests and actual native/polling reload
scenarios have passed. After host pressure stabilizes, continue full ordinary
Go and vet using fresh stage directories.
Run the complete race suite and all-package atomic coverage as separate serial
stages, preserving full instrumentation scope across both gates. Then finish
Python, invariants/scans, consumer inventory and coverage accounting. Comparing
old/new peaks requires equivalent workloads; no percentage reduction is asserted.

Go's [GC guide](https://go.dev/doc/gc-guide#Memory_limit) explains why the memory
target is soft and lower GC targets trade CPU for memory. The
[race detector documentation](https://go.dev/doc/articles/race_detector#Runtime_Overhead)
documents its substantial instrumentation overhead. The
[Go command reference](https://pkg.go.dev/cmd/go#hdr-Compile_packages_and_dependencies)
defines build parallelism. The local toolchain is Go 1.23.3 darwin/arm64.

Raw host snapshots, old attempts and process data remain local under `var/`;
the companion JSON records hashes and distinguishes attempted from unrun checks.

## Architecture iteration

The [iteration image](go-low-memory-validation-infographic-2026-10-13.png) marks
the CLI test budget propagation and validation controls on the existing layered
base. Its status panel records the 14:48 UTC preflight; subsequent execution
results belong in this receipt. The architecture has no added or removed
components, so the base diagram and canonical topology stay unchanged. The
[exact image prompt](go-low-memory-validation-infographic-2026-10-13.prompt.txt)
is retained alongside the image.

The [focused-validation image](go-low-memory-focused-infographic-2026-10-13.png)
records the subsequent two passing stages and the 15:10 full-suite preflight
block. Its [exact prompt](go-low-memory-focused-infographic-2026-10-13.prompt.txt)
retains the workload and measurement limits.

The [interrupted full-validation image](go-low-memory-blocked-infographic-2026-10-13.png)
marks this evidence-only continuation, with its
[exact prompt](go-low-memory-blocked-infographic-2026-10-13.prompt.txt).
