# Go personal-assistant demo D3 — attention list, pause switch, user-first slots, failure classification (2026-11-01)

Baseline: `f8bdd34`.

Request this round implements (2026-11-01): compare the demo with DeepSeek's
`dsh-superagent-prototype` (<https://github.com/deepseek-ai/dsh-superagent-prototype>,
commit `6337fc8`) and apply the improvements it suggests. The comparison table and
the items left for later are in the
[roadmap](../PROACTIVE_AGENT_ROADMAP.md#dsh-superagent-原型给的启发2026-11-01-对照).

Scope:

- `go/assistant`: new `control.go`, `health.go`, `lock_unix.go`, `lock_other.go` and
  `control_test.go`; changes to the app, store, router, scheduler, outbox, poller,
  tasks, server, model and page; one test call site;
- `go/cmd/miniloop-assistant`: the `-workers` help text;
- the roadmap, the README baseline, Mermaid, boundary text and repository map, the
  Go README, the demo README, and the parity matrix and plan entries;
- the architecture JSON and HTML, the imagegen base diagram and the research index.

## What changed

- **Attention list.** `Store.Attention` and `App.Attention` derive one list of what
  waits for the user, with disjoint kinds:
  - drafts to approve;
  - approved proposals that finally failed;
  - tasks handed back after six checks;
  - triggers that failed in the last 24 hours;
  - components that failed three times in a row.

  Components come from an in-memory health board fed by the mail poll, calendar
  watch, outbox transport and worker model calls. The page shows the list first,
  with one action per item, and hides it when it is empty. A `dismissals` table
  backs **知道了**. Retry, resume and reprocess clear the dismissal and reset the
  attempt or check budget.
- **Pause.** A durable switch in `meta` stops the router, the outbox and the
  scheduler. The poller still records mail, decisions are still recorded, and
  running turns finish. Resuming wakes every loop. Pause and resume each write a
  ledger row and a channel line.
- **User first.** `ClaimTrigger` claims chat first. The router keeps one slot beyond
  `-workers` that may only claim chat.
- **Failures.** The agent session returns a failed model call, and round exhaustion,
  as final text with a nil error. Before this round the router posted that text to
  the channel as the worker's answer and marked the trigger done; a model outage was
  never retried. The router now reads the turn's `RunError` events and treats both
  as failed turns. A wrapping provider keeps the typed provider error and preserves
  streaming. MCP refusals, 4xx rejections and round exhaustion fail at once; other
  failures retry with backoff.
- **One daemon.** `NewApp` takes an advisory `flock` on `<data>/daemon.lock` and
  refuses a second daemon with `ErrDataDirBusy`, naming the holder's pid.
- **Page.** Besides the list and the pause button, the worker badge shows the
  reserved slot. A stalled task card offers **再跟进一轮**. At phone width the grid no
  longer overflows; this overflow predates the round.

## Iteration image

- Image: [go-assistant-attention-2026-11-01.png](go-assistant-attention-2026-11-01.png).
  It also replaces `docs/mini-loop-layered-architecture.png` as the canonical raster
  base.
- Exact prompt: [go-assistant-attention-2026-11-01.prompt.txt](go-assistant-attention-2026-11-01.prompt.txt),
  also stored in `docs/mini-loop-layered-architecture.prompt.txt`. It is one edit pass
  on the previous base.
- Explanation: the bottom band "独立可选 Demo · Go 个人助理" keeps its five-box flow.
  - The page box lists 待你处理 and 暂停开关.
  - `assistant.db` lists 暂停开关.
  - The router box reads "你的消息优先 · 预留 1 个槽位".
  - Boxes 1–3 carry this iteration's orange outline.
  - The strip names the attention kinds, the pause rule and the one-daemon lock.
  - The badge reads "2026-11-01：少打扰 · 可暂停 · 你优先".

**Known image deviations:**

- In box 1 the small glyphs are soft: the title's 信 reads close to 仙, and the small
  line's 待 and 审 read close to 得 and 暂.
- In the strip, 照 in 照常收信 is slightly malformed.
- The two thin strips at the foot of the Task container band are still missing, as
  disclosed for earlier iterations.

Earlier repair passes rewrote other bands, so this pass was accepted as is. The
README Mermaid is canonical; small raster text is not authoritative.

The interactive map draws the health board with the scheduler's feed and its edge to
the page. The poller feed would cross the scheduler's `calendar_list` route, and
the outbox and router feeds would cross the lane, so the health board's tag names
those sources instead. The README Mermaid draws all four feeds.

## Manual runtime evidence

These runs used the built binary, the offline scripted model, the real `mock-mcp`
stdio child and scratch data directories.

1. **One daemon.** A second daemon on the same `-data` printed "data directory is in
   use by another assistant daemon: … (pid 73823)" and exited 1. The first kept
   serving.
2. **Pause.** After **暂停处理**, a manager mail and a chat message were recorded as
   pending triggers and nothing ran. After SIGTERM and a restart the state still read
   paused and both triggers stayed pending. **恢复处理** processed both within
   seconds, and the manager draft appeared under **需要你处理**.
3. **Approve from the list.** **批准发送** in the list sent the draft through MCP
   (receipt `sent-5`). The follow-up task closed, and the list emptied and hid
   itself.
4. **Component problem.** The binary was renamed so the MCP child could not restart,
   then the child was killed. The MCP badge turned red at once. The mail poll entered
   the list only after its third failure in a row ("连续失败 4 次" at the next look).
   It left the list on the first good poll after the binary was restored.
5. **Upgrade.** A data directory written by the committed D2 binary (`f8bdd34`)
   started under the new binary. It gained `proposals.settled_at` and `dismissals`,
   and its pending draft appeared in the list.
6. **Page.** The page was inspected at 1360 px and 390 px wide, paused and running.
   The phone-width check found two bugs, both fixed and checked again:
   - a literal `null` when the page was not paused;
   - the grid overflowing the screen.

   The only console errors were `favicon.ico` 404s and refused polls after the
   daemon stopped.

## Validation gates

Resource controls for the Go runs: `GOMAXPROCS=2`, `GOFLAGS=-p=1`, `-parallel=2`,
`GOMEMLIMIT=1536MiB` and `nice -n 10`. The host had 16 GiB of RAM, with 52% free
before the runs and 49–51% free during the race run. Pytest ran serially without
xdist. A credential-pattern scan of the diff and the new files found nothing.

| Gate | Result |
|---|---|
| `gofmt -l assistant cmd/miniloop-assistant` | clean |
| `go test -count=3 ./assistant/ ./cmd/miniloop-assistant/` and `-race -count=2` on both | pass |
| `go vet ./...` | pass |
| `go test -count=1 -timeout=600s ./...` | 53 packages ok, exit 0 |
| `go test -race -count=1 -timeout=600s ./...` | 53 packages ok, exit 0 |
| `.venv/bin/python -m pytest -q` | 2256 passed, 28 skipped, 104 warnings (100s) |
| `python/tools/export_go_contracts.py --check` | current (268 files) |
| `python/tools/verify_scans.py` | 19 scanning guards anchored |
| Archify `validate --quality showcase` | 9/9 checks, 0 errors, 0 warnings, 0 crossings |
| Archify `deliver` | ok; spec `1d2937c3…` (203,444 B), HTML `f5bbdc57…` (929,608 B); inspected in a browser |
| `research-site`: `npm run content`, `npm run content:check` | regenerated; current (22 documents) |
| `git diff --check` | clean |

**Not run, and why:**

- `research-site` `npm test` (build plus rendered HTML): `node_modules` is absent,
  and the local Node 22.12.0 is below the declared `>=22.13.0`.
- `verify_invariants.py` and `verify_guards.py`: no Python package module and no
  guarded behaviour changed.
- The README Mermaid was not machine-rendered, because no Mermaid tooling is
  installed. Its node IDs and edges were checked by script and by hand.
- The DeepSeek prototype was read, not run.
