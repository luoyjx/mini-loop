# Linux same-platform Source minima — 2026-10-14

## Result and scope

Frozen `78d95bc250c5d72183d649acb5d114bb8c157bf2`: actual Linux Source capture succeeded (`source_captured`, exit 0), then unchanged native Go passed **8 selector groups / 7 packages / 10 explicit target tests** (`passed_minima`, exit 0). Six original Source producers retained all **66 existing cases**; Go exercised only the listed minima. Source and Go used identical archive SHA-256 `98f8d50187b8b8f1efcc029c0fb273b23c3fbc0c8874c2e8d349eb8774f2ab7a`.

The six previously failing groups in `go-required-row-linux-minima-2026-10-14.json` match actual Source behavior in the same Linux environment. These witnessed mismatches need no production-code fix. Six captured files were mounted read-only over the frozen Go fixtures; tracked code, fixtures, assertions and timeouts were unchanged.

**This receipt does not establish full Linux ordinary, vet or race acceptance.** It also does not claim that all 66 Source cases were tested in Go or that the complete Source export ran.

## Six fixture differences

| Fixture | Changed cases / total | Changed leaf fields | Actual difference |
| --- | ---: | ---: | --- |
| benchmark arms | 1 / 8 | 3 | `visible/edit-config`: shorter dash EOF diagnostic; transcript estimate 243 → 208. |
| benchmark HTTP | 5 / 5 | 20 | Baseline/candidate edit-config 243 → 208; sum 1073 → 1038. `one-round`: 164 → 147; sum 764 → 747. |
| efficiency HTTP | 2 / 2 | 8 | Same edit-config and summed context estimates under both efficiency settings. |
| owner resources | 1 / 4 | 8 | Only Darwin `/private` before `$USERS`; owner/admission/problem behavior equal. |
| glob | 1 / 38 | 1 | Both admit 555 names and retain the same marker; last admitted output `_0799.txt` → `_0554.txt`. |
| worktrees | 9 / 9 | 29 | 28 `/private<REPO>` differences; one Git filesystem-boundary diagnostic. |

Total: **19 changed cases / 66, 69 changed JSON leaf fields**. Unlisted fields are structurally equal. Exact JSON pointers, representative text and complete fixture hashes are in the companion JSON.

### Why these differences appear

- **Shell and benchmark:** Linux `/bin/sh` resolves to `/usr/bin/dash`. Its `Syntax error: EOF in backquote substitution` is shorter than the captured Darwin two-line shell diagnostic. The original Source retains the real command output in messages, and `python/mini_loop/benchmark.py:239` derives the estimate from those messages. Both baseline and candidate inherit the difference; verdict and other behavioral fields do not change.
- **Paths:** the original producer normalization replaces the scratch root string. Darwin's resolved `/private` prefix remains in its stored diagnostics and worktree paths; Linux's native `/tmp` does not add it. No normalization algorithm was changed.
- **Glob:** `python/mini_loop/tools.py:649` applies the output budget during `iglob` enumeration and sorts only the admitted set afterward. The actual Linux btrfs volume yields a different admitted set than the old fixture. Both outputs are 49,973 characters / 138,773 UTF-8 bytes and end with `... (matches truncated)`. Raw Linux directory order is preserved in `workspace.context.json`.
- **Git:** Linux Git 2.39.5 on the dedicated native `/tmp` volume reports discovery stopping at a filesystem boundary for the `unverified-status` discard-remove recipe. The old Darwin fixture has the generic parent-directory `.git` error. The actual Source and unchanged Go agree on Linux.

## Actual Go targets

| Group | Explicit target | Result |
| --- | --- | --- |
| astcontext | `TestDigestRecheckedBySameAdapterBeforeEveryProbe` | pass |
| benchmark | `TestRunArmMatchesActualPythonDefaultFakeArms/visible` | pass |
| HTTP | `TestBenchmarkActualPythonHTTP/default` | pass |
| efficiency HTTP | `TestBenchmarkEfficiencyActualPythonHTTP/efficiency-shadow` | pass |
| reload | `TestNativeEventsActualSourceRecipes/directory-symlink-{source,reverse}` | both pass |
| userresources | `TestResourcesMatchActualPython/problems` | pass |
| workspace | `TestGlobMatchesPythonContracts/glob_truncated` | pass |
| worktrees | `TestActualPythonLifecycleFactoryAndTaskBinding/{not-repository,unverified-status}` | both pass |

AST and reload were already passing in the prior eight-group receipt. This same-platform fixture capture closes the remaining six groups only within these selectors.

## Environment and resources

Pinned image `sha256:874a8b40ad6058f3f160636c61946394559b067562d7478cc3f9cc500794e9aa`, native Linux arm64, Go 1.23.3, Python 3.11.2, Git 2.39.5, dash and dedicated btrfs `/tmp`. All 22 existing Python dependencies were verified at fixed versions, including Anthropic 0.107.1 and Pydantic 2.13.4. Source and Go shell hash, Git, kernel, architecture and filesystem context match.

Both stages were serial and used 2 CPU / 1,536 MiB memory / zero swap / 256 PIDs, network none, pull never, read-only container root, nice 10 and an ownership-checked cleanup. Go also used `GOMAXPROCS=2`, `GOMEMLIMIT=512MiB`, `-p=1` and `-parallel=2` with disk caches.

| Stage | Outer duration | cgroup memory peak | OOM kills | Host pressure levels |
| --- | ---: | ---: | ---: | --- |
| Source | 10.94 s | 146,280,448 bytes (~139.5 MiB) | 0 | 1 |
| Go minima | 71.54 s | 735,055,872 bytes (~701.0 MiB) | 0 | 1 |

Both task containers exited 0. Exact owned container and volume deletion commands succeeded; cleanup error lists are empty. Docker working-set samples are preserved separately from cgroup peaks; neither is labeled host process-tree RSS.

## Evidence

- `var/go-stuck-followup-linux-2026-10-14/linux-source-78d95bc/result.json`
- `var/go-stuck-followup-linux-2026-10-14/linux-source-78d95bc/logs/source-capture/receipt.json`
- `var/go-stuck-followup-linux-2026-10-14/linux-go-source-78d95bc/result.json`
- Companion `go-linux-platform-source-2026-10-14.json` contains artifact/fixture hashes, all 69 leaf differences, selectors, resource receipts and exact provenance.

This receipt was produced by reading completed evidence and rehashing logged files. The summarizer ran no Docker, Source capture, test or build.
