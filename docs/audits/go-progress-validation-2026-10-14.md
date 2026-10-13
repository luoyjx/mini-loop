# Validation with the explicitly relaxed resource profile

The user requested: “放宽限制，优先推进工作进度”. This supersedes the earlier
normal-pressure-only profile for this run. Original stopped attempts remain
retained; the new run uses separate stage directories under
`var/go-progress-validation-2026-10-13/` (the run began before local midnight).

The working profile chosen under that instruction is `GOMAXPROCS=2`, Go build `-p=1`, test
`-parallel=2`, `GOMEMLIMIT=512MiB` (soft), `GOGC=75`, nice 10. The observed
pressure values 1 and 2 are admitted; every other/unknown value stops the
workload. Sampled owned-tree RSS stops at 1.5 GiB, sustained CPU above 300% for
45 seconds stops, and each stage retains a 1,800-second timeout. Stages run
serially. No unrelated process has been stopped. The monitor now records signal
API timing without treating it as kernel delivery or target-reaction timing.

## Final observed results

| Gate | Result | Scope |
| --- | --- | --- |
| Ordinary Go | passed | `./...`, 51 packages, 1,433 top-level passes, 6 skips |
| Go vet | passed | `./...` |
| Python | passed | 2,256 passed, 28 skipped, 96 warnings, 24 subtests passed |
| Invariants | passed | 77 modules |
| Source scans | passed | all 19 scan guards anchored |
| Consumer inventory | failed, known gaps | 254/256 candidate passing-test associations |
| Complete race | passed | `./...`, 1,433 top-level passes, 6 skips; six linker warnings retained |
| Full-package atomic coverage | passed | `./...`, `-coverpkg=./...`, `-covermode=atomic` |

Ordinary Go's skips are two subprocess entry helpers and four opt-in diagnostic
output tests. The consumer gaps are `python-provider-idna.json` and
`python-syntax-parser.json`, both unreferenced in the current native inventory.
Both fixtures and the corresponding provider/protocol code already existed
unchanged at local main `69cba43`; the new legacy recovery fixture has a passing
consumer association. Consumer association is a static relationship plus a
passing test result, not semantic parity for every field.

The earlier Source exporter and four type-audit passes are reused as historical
evidence after streaming hash verification: Python, production Go, and fixture
bytes match their original inputs. The new CLI test changes and documentation
are identified separately; no claim of a rerun is made.

The previous full-stage interruption remains in
[the earlier receipt](go-low-memory-validation-2026-10-13.md). This run verifies
the recovery and test-budget delivery; complete Go-port acceptance still has
known open contracts. The consumer gate remains failed for those two baseline
gaps; it has not been reclassified as full-port acceptance.

The Go race log contains six paired compiler package headers and
`LC_DYSYMTAB` linker warnings outside its JSON test events. All 51 package
results and the process exit confirm completion. The first summary conservatively
rejected those lines; `receipt-v2.json` retains every warning and raw-log hash,
with a narrow reviewed classification. No stage was rerun or its log altered.

| Go statement coverage | Covered / total | Percent |
| --- | --- | --- |
| Application (exclude `/tools/`, include CLI) | 33,644 / 37,622 | 89.43% |
| All Go packages | 34,010 / 38,137 | 89.18% |
| Provider only | 1,524 / 1,756 | 86.79% |

The complete 91,164,335-byte atomic profile contains 1,305,178 rows, reduced to
26,635 unique spans. Repeated spans count once, covered if any occurrence has
hits. The official `go tool cover -func` total is 89.2%, consistent with rounding.
These numbers cover Go statements; they do not measure Python, native C,
subprocess interiors, branches, other platforms, or complete migration parity.

Monitored peaks were 440.06 MiB (ordinary Go), 548.34 MiB (vet), 435.92 MiB
(Python), 525.58 MiB (race), and 578.25 MiB (atomic coverage). Every completed
stage has unchanged frozen inputs and normal-exit checks with no remaining
observed task identities. These are sampled RSS observations, not hard RAM
caps or a before/after memory reduction.

## Local main integration

After the required implementation checks completed, local main was fast-forwarded
from `69cba43` to `f913d20`, preserving the five commits for Source contracts,
recovery behavior, budget propagation and evidence. The prior user instruction
“代码合到主分支” authorized this integration. The tested runtime and fixture
inputs are identical after the merge. No remote push or remote refresh occurred.
The delivery receipt retains integration identity and all source/test provenance;
the documentation commit follows this runtime integration on main.

The [Go GC guide](https://go.dev/doc/gc-guide#Memory_limit) explains the soft
runtime target. The pressure gate is defined by the exact observed integers,
without assuming that a current XNU internal enum is identical to this macOS
15.5 sysctl's exported values.

## Architecture iteration

The [iteration image](go-progress-validation-infographic-2026-10-14.png) marks
the recovery/CLI changes now integrated into local main and the current
contract-audit evidence. The canonical layered topology, README Mermaid,
interactive specification and base diagram have no added or removed components.
The [exact generation prompt](go-progress-validation-infographic-2026-10-14.prompt.txt)
retains the coverage method, warnings and incomplete migration boundary.
