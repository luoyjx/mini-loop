# Go personal-assistant demo D5 — admission grading, triage, background preparation, watchdog (2026-11-01)

Baseline: `a3bcbe5`.

Request this round implements (2026-11-01): do both D4 candidates, then roadmap P5
and P6. This record covers P5, delivered as demo iteration D5:

- an LLM router as the semantic slow path;
- admission grading;
- speculative preparation;
- asynchronous start/stop;
- a watchdog;
- a 300-event acceptance benchmark.

Scope:

- `go/assistant`:
  - new `contacts.go`, `triage.go`, `jobs.go`, `watchdog.go`, `admission_test.go`
    and `bench_test.go`;
  - changes to the model, rules, store, router, tools, scripted model, control,
    routines, seed, world, server, app and page;
  - updated rule, flow, MCP-count and launcher test helpers;
- the roadmap, the README baseline, Mermaid and boundary text;
- the Go README, the demo README, and the parity matrix and plan entries;
- the architecture JSON and HTML, the imagegen base diagram and the research index.

## What changed

- **Admission grading.** `Rules.Classify` takes whether the sender is a contact.
  - Bulk mail is ignored as before.
  - A known contact writing to the user is explicit and goes to the worker.
  - A known contact copying the user, or a stranger writing to the user, is an
    inferred candidate.
  - A stranger copying the user is ignored.
  - Every other trigger kind is explicit.

  The `contacts` table is seeded once with four demo contacts under its own meta
  key, so older data directories gain them. `CompleteProposal` counts an interaction
  with each recipient it sends to and adds unknown recipients. `world.db` gains a
  `cc` column through an additive migration, and mail carries `Cc`.
- **Triage.** `Triage.Grade` makes one `PurposeDecision` call without tools on
  the observing provider. The prompt holds the sender, recipients, subject and a
  preview inside `<external>`. The answer is JSON `{verdict, reason}`.
  - The candidate mode is stored in meta `candidate_mode`, and observe is the
    default.
  - In observe mode, `handle` settles the trigger as `observed`. In auto mode it
    runs the worker with the triage reason in the rule line.
  - `observe` settles as `observed` and `ignore` as `ignored`. A failed or
    unreadable answer settles as `observed`.
  - `Store.Candidates` lists observed triggers. `Store.DecideCandidate` re-queues
    one as admitted (`triggers.admitted`), so the router skips the triage, or
    ignores it.
  - `POST /api/candidates/{id}/handle|ignore` and `POST /api/candidates/mode` drive
    this from the page.
- **Background preparation.** `prepare_start` and `prepare_stop` are local write
  tools.
  - `Store.CreateJob` deduplicates per trigger and goal and allows at most five
    queued or running jobs.
  - The `Preparer` claims one job at a time. Each job runs in a fresh session whose
    `agent.SelectTools` holds `mail_list`, `mail_read`, `calendar_list` and
    `task_list`. The session runs untrusted with a two-minute limit, and its tool
    calls are bound to the job's trigger for the ledger.
  - `Store.FinishJob` settles the job and, in the same transaction, enqueues
    `job.done` on the starting thread. The router passes the result to the thread's
    worker inside `<prepared>`. A job cancelled by the page
    (`POST /api/jobs/{id}/stop`) or by `prepare_stop` from the same thread wakes
    nobody.
  - `RequeueJobs` runs at start, and the Preparer claims nothing while paused.
- **Watchdog.** After each successful `task.wakeup` turn the router snapshots the
  task's check and note into `task_checks`. `Store.Watchdog` runs after each
  scheduler tick and turns the last snapshots into `agent.ToolStep` values for
  `agent.NewStuckDetector`. The thresholds are three repeats or six alternations,
  with no monologue or nudge handling. A flag is raised once per streak in the
  `watchdog` table, with a ledger row, a channel line and the `no_progress`
  attention kind (`progress-<task>`). A check that makes progress clears it, and a
  new streak lists it again even after a dismissal.
- **Router counters.** `RouterStats` counts turns, batched turns, merged triggers,
  rule ignores, explicit requests, triage verdicts and observed triggers, in memory.
- **Scripted model.** It answers triage requests with keyword rules: suspicious
  instructions `handle`, promotion `ignore`, and anything else `observe`. A mail
  that only copies the user stays quiet. A bill starts `prepare_start` and stays
  quiet. A preparation session compares the newest two bills in the inbox, and a
  `job.done` turn posts the result. The seeded world gains last month's bill.
- **Presets.** **经理群发，你在抄送里**, **陌生人：合作推广** and
  **供应商账单（后台核对）**. The custom form has a **我在抄送里** box.
- **Page.**
  - A **候选** tab holds the observe/auto switch, the router counters and the
    candidates with **交给 worker** and **忽略**.
  - The task tab lists background jobs with **停止**.
  - The world tab lists contacts.
  - The attention list shows **可能卡住** with **立即检查**, **取消任务** and
    **知道了**.
  - Chips no longer wrap at phone width.

## Deviations from the roadmap text

- The asynchronous start/stop uses durable `jobs` rows and one daemon goroutine,
  not the `go/background` shell service. Workers have no shell, and preparation
  should only read.
- The "LLM router" is a triage call for inferred candidates only. Explicit
  requests never wait for it, and the main router stays deterministic.
- Speculative preparation demonstrates bill checking only, and preparation sessions
  cannot draft.
- Router counters are process-local and reset on restart.

## Acceptance stream

`TestSyntheticEventStreamAcceptance` mixes 15 event kinds by weight into 300
events. It delivers them through the simulator and the poller, or as chat, to a
daemon with four workers, a real clock and the offline model. It then waits for
every trigger to settle. One run:

| Measure | Value |
|---|---|
| Events (excluding routine, task, `job.done` and reminder triggers) | 300 |
| Worker turns | 112 |
| Batched turns / merged triggers | 25 / 74 |
| Rule ignores / explicit requests | 60 / 186 |
| Triaged: handle / observe / ignore / failed | 63: 15 / 24 / 24 / 0 |
| Final statuses: done / quiet / ignored / observed | 139 / 38 / 84 / 39 |
| Quiet rate (quiet, ignored or observed) | 0.54 |
| Trigger latency p50 / p95 / max | 0.51 s / 1.01 s / 1.09 s |
| Wall time | 1.25 s |

The test fails if any trigger fails, if a router path is never exercised, if the
quiet rate drops below 0.4, or if there are as many turns as events. Latency comes
from millisecond-precision timestamps and the scripted model, so it measures the
daemon's own queueing, not a real model.

## Iteration image

- Image: [go-assistant-admission-2026-11-01.png](go-assistant-admission-2026-11-01.png).
  It also replaces `docs/mini-loop-layered-architecture.png` as the canonical raster
  base (`b749d778…`).
- Exact prompt: [go-assistant-admission-2026-11-01.prompt.txt](go-assistant-admission-2026-11-01.prompt.txt),
  also stored in `docs/mini-loop-layered-architecture.prompt.txt`. It is one edit pass
  on the previous base.
- Explanation: the bottom band "独立可选 Demo · Go 个人助理" keeps its five-box flow.
  The new part, the Preparer, joins the router box.
  - The page box lists 候选 and 后台任务.
  - `assistant.db` lists 联系人, 准备任务 and 检查快照.
  - The router box is titled "Router + Worker + Preparer" and reads
    "准入分级 · 分诊 · 后台只读准备".
  - Boxes 1–3 carry this iteration's orange outline.
  - The strip names the triage default, where preparation results go and the
    watchdog.
  - The badge reads "2026-11-01：准入分级 · 分诊 · 后台准备 · 看门狗".

**Known image deviations:**

- Small glyphs are soft. In box 1, 候选 is hard to read. In box 2, 检查快照 reads
  close to 捡度快照.
- The mock-mcp box wraps `world.db` onto a second line, as before.
- The two thin strips at the foot of the Task container band are still missing, as
  disclosed for earlier iterations.

No repair pass was tried. The previous iteration's repair made the image worse. The
README Mermaid is canonical; small raster text is not authoritative.

## Manual runtime evidence

These runs used the built binary, the offline scripted model, the real `mock-mcp`
stdio child and a scratch data directory.

1. **Grading and triage.** Five presets were delivered:
   - the cc'd team mail (#2) became `observed` with "分诊：只记录（用户只在抄送里，先记录）";
   - the promotion (#3) became `ignored` with "分诊：忽略（推广或合作邀约…）";
   - the suspicious mail (#4) became `observed` with "分诊建议处理（含可疑指令…）；观察模式只记录…";
   - the bill (#5) stayed quiet with "已在后台核对（准备任务 #1），核对完再汇报";
   - the manager mail (#6) produced a draft and a task.

   The router counters read 3 triaged (1 handle, 1 observe, 1 ignore), 2 observed
   and 4 explicit.
2. **Preparation.** Job #1 finished with "本期「11 月云服务账单 ¥1,280」（m-8）；
   上一期「10 月云服务账单 ¥1,080」（m-seed-4）；比上一期多 ¥200（+18.5%）。".
   `job.done` #7 posted "后台核对完成：…" on the bill's thread.
3. **Hand-over and contacts.** Clicking **交给 worker** on #4 in the browser ran the
   worker with no further triage; it posted "⚠️ 可疑邮件：…". Approving the manager
   draft sent it through MCP and raised Alice's interaction count to 1.
4. **Watchdog.** A second manager mail created task #2. Three **立即检查** calls each
   recorded "回复还没发出，草稿等用户批准". The next scheduler tick posted
   "看门狗：任务 #2「…」最近 3 次检查要核对的内容和进展都没变，可能卡住了。" and
   listed **可能卡住** under **需要你处理**. **知道了** removed it.
5. **Restart.** After a restart on the same data directory, the following were kept:
   - the candidate mode;
   - the remaining candidate;
   - the finished job and the contacts;
   - the dismissal.

   The router counters started from zero.
6. **Custom cc.** A cc'd custom mail from a stranger was ignored by the rules
   ("陌生发件人、你只在抄送里：只归档"). The same from Dave was triaged and
   observed.
7. **Page.** The page was inspected at 1360 px and 390 px wide.
   - At 390 px the document is 390 px wide, so there is no horizontal overflow.
   - A trigger chip wrapped mid-word at 390 px; `white-space: nowrap` on chips
     fixed it.
   - The only console error was the `favicon.ico` 404.

## Validation gates

Resource controls for the Go runs: `GOMAXPROCS=2`, `GOFLAGS=-p=1`, `-parallel=2`,
`GOMEMLIMIT` (1.5 GiB for package runs, 2 GiB for module runs) and `nice -n 10`.
The host had 16 GiB of RAM, and `memory_pressure` reported 62% free during the
module runs. Pytest ran serially without xdist.

| Gate | Result |
|---|---|
| `gofmt -l assistant cmd/miniloop-assistant` | clean, after formatting `server.go` |
| `go vet ./...` | pass |
| `go test -count=1 -parallel=2 ./...` | 53 packages ok, exit 0 |
| `go test -race -count=1 -parallel=2 ./...` | 53 packages ok, exit 0 |
| `go test -count=3 ./assistant/ ./cmd/miniloop-assistant/` and `-race -count=2` on both, after `gofmt` | pass |
| `TestSyntheticEventStreamAcceptance -v` | pass; report above |
| `.venv/bin/python -m pytest -q -p no:xdist` | 2256 passed, 28 skipped, 104 warnings (94s) |
| `python/tools/export_go_contracts.py --check` | current (268 files) |
| `python/tools/verify_scans.py` | 19 scanning guards anchored |
| Archify `validate --quality showcase` | ok after placing two labels |
| Archify `deliver` | ok, 9/9 checks; spec `d826b57b…` (205,819 B), HTML `c43b3196…` (933,738 B); assistant region inspected in a browser |
| `research-site`: `npm run content`, `npm run content:check` | run in the follow-up reindex commit, because the index records the roadmap's last commit |
| `git diff --check` | clean |

**Not run, and why:**

- `research-site` `npm test` (build plus rendered HTML): `node_modules` is absent,
  and the local Node 22.12.0 is below the declared `>=22.13.0`.
- `verify_invariants.py` and `verify_guards.py`: no Python package module and no
  guarded behaviour changed.
- The README Mermaid was not machine-rendered, because no Mermaid tooling is
  installed. Its node IDs and edges were checked by hand.
