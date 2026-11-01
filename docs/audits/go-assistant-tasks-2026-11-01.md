# Go personal-assistant demo D2 — durable tasks, self-scheduled checks, upcoming-event reminders (2026-11-01)

Baseline: `2103c07`.

Request this round implements (2026-11-01): the assistant should keep watching
events, carry each piece of work through to completion, and handle several things
at once.

Scope:

- `go/assistant`: new `tasks.go` and `tasks_test.go`; changes to the model, store,
  tools, router, rules, scheduler, outbox, app, server, scripted model, mock world,
  mock MCP server, connector and page;
- `go/cmd/miniloop-assistant`: the expected tool count in the daemon test;
- the [roadmap](../PROACTIVE_AGENT_ROADMAP.md), the README baseline, Mermaid and
  repository map, the Go README, the demo README, and the parity matrix and plan
  entries;
- the architecture JSON and HTML, the imagegen base diagram and the research index.

## What changed

- **Tasks.** `assistant.db` gains `tasks` and `reminders` tables. Workers get
  `task_create`, `task_schedule`, `task_complete` (write, local only) and
  `task_list` (read). A task is bound to the thread and worker session that created
  it.
- **Wakeups.** Each scheduler tick claims due checks. In one transaction the claim
  counts the check, enqueues a `task.wakeup` trigger on the task's thread and sets a
  30-minute fallback check. The thread's subscribed worker resumes.
  - A task whose previous wakeup is still queued or running waits for it.
  - After six checks the task is handed to the user and no longer wakes.
- **Event links.** A send, a final send failure or a denial on the same thread
  brings the task's check forward. The reason reaches the wakeup prompt.
- **Access.** A turn driven by external mail may change only the tasks on its own
  thread. A turn the user starts in the channel may change any task.
- **Time triggers.** The scheduler reads the next 15 minutes through MCP
  `calendar_list`. It enqueues one `calendar.upcoming` trigger per confirmed event
  and start time. The mock server adds `sim_add_event`, for ten tools.
- **Concurrency.** Different threads already ran on parallel workers and one
  thread ran in order. This is now tested, and the page shows running workers and a
  task board with **立即检查** and **取消**.
- **Prompts.** Every worker prompt carries the current time and the thread's other
  active tasks.

## Iteration image

- Image: [go-assistant-tasks-2026-11-01.png](go-assistant-tasks-2026-11-01.png). It also
  replaces `docs/mini-loop-layered-architecture.png` as the canonical raster base.
- Exact prompt: [go-assistant-tasks-2026-11-01.prompt.txt](go-assistant-tasks-2026-11-01.prompt.txt),
  also stored in `docs/mini-loop-layered-architecture.prompt.txt`. It is one edit pass
  on the previous base.
- Explanation: the bottom band "独立可选 Demo · Go 个人助理" keeps its five-box flow.
  - `assistant.db` now lists 持久任务.
  - The router box is now "Router + 并行 Worker · 同一 thread 串行".
  - Both boxes carry this iteration's orange outline.
  - The strip names task wakeups to the same worker with at most six checks, the
    15-minute event reminder, and the outbox with `proposal-id`.
  - The badge reads "2026-11-01：持久任务 + 自约检查 + 日程临近".

**Known image deviations:**

- In box 1 the generator wrote "模拟邮信" instead of "模拟来信".
- The strip dropped the word "幂等键" before `proposal-id`.
- The two thin strips at the foot of the Task container band are still missing, as
  disclosed for the previous iteration.

Earlier repair passes rewrote other bands, so this pass was accepted as is. The
README Mermaid is canonical; small raster text is not authoritative.

## Manual runtime evidence

These runs used the built binary, the offline scripted model, the real `mock-mcp`
stdio child and scratch data directories.

1. **Startup.** The daemon logs `mcp=mock-mailcal over stdio, 10 tools`.
2. **One pass of events.** The deliveries below ran in parallel threads and settled
   within seconds. The same-day 07:00 brief caught up once.
   - The manager mail produced a reply draft and task #1, with a check an hour out.
   - The conflicting invite produced a decline draft.
   - The phishing mail was flagged with no proposal.
   - "1 分钟后提醒我 喝水" created task #2.
   - **日历：12 分钟后有会** produced one `calendar.upcoming` reminder. It named
     Alice as the manager, her two recent mails and task #1.
3. **Reminder.** One real minute later the scheduler woke task #2 and posted
   "⏰ 提醒：喝水"; the task closed as done.
4. **Follow-through.** Approving the reply sent it through MCP (receipt `sent-7`).
   The outcome brought task #1 forward. The same worker session woke, found the reply
   in the sent folder and closed the task within a second.
5. **Restart.** A second manager follow-up created task #3. After SIGTERM and a
   restart on the same directory:
   - the task and its next check were still on the board;
   - **立即检查** woke the worker restored from `agent.db`, which found no reply and
     scheduled the next check in 30 minutes;
   - denying the draft brought the check forward again, and the worker closed the
     task as declined.
6. **Upgrade.** A data directory written by the committed D1 binary (`2103c07`)
   started under the new binary. It gained the `tasks` and `reminders` tables, kept
   its pending proposal, and the next manager follow-up created a task.
7. **Page.** The task board, the running-worker badge, the task buttons and the new
   simulator button were inspected in a browser. The only console errors were
   `favicon.ico` 404s and refused polls after the daemon stopped.

## Validation gates

Resource controls for the Go runs: `GOMAXPROCS=2`, `GOFLAGS=-p=1`, `-parallel=2`,
`GOMEMLIMIT=1536MiB` and `nice -n 10`. The host had 16 GiB of RAM, with 46–49% free
during the runs. Pytest ran serially without xdist.

| Gate | Result |
|---|---|
| `gofmt -l assistant cmd/miniloop-assistant` | clean |
| `go test -count=3 ./assistant/ ./cmd/miniloop-assistant/` and `-race -count=2` on both | pass |
| `go vet ./...` | pass |
| `go test -count=1 -timeout=600s ./...` | 53 packages ok, exit 0 |
| `go test -race -count=1 -timeout=600s ./...` | 53 packages ok, exit 0 |
| `.venv/bin/python -m pytest -q` | 2256 passed, 28 skipped, 104 warnings (92s) |
| `python/tools/export_go_contracts.py --check` | current (268 files) |
| `python/tools/verify_scans.py` | 19 scanning guards anchored |
| Archify `validate --quality showcase` | 9/9 checks, 0 errors, 0 warnings, 0 crossings |
| Archify `deliver` | ok; spec `acfa76a3…` (202,139 B), HTML `941f6bce…` (925,992 B); inspected in a browser |
| `research-site`: `npm run content`, `npm run content:check` | regenerated; current (22 documents) |
| `git diff --check` | clean |

**Not run, and why:**

- `research-site` `npm test` (build plus rendered HTML): `node_modules` is absent,
  and the local Node 22.12.0 is below the declared `>=22.13.0`.
- `verify_invariants.py` and `verify_guards.py`: no Python package module and no
  guarded behaviour changed.
- The README Mermaid was not machine-rendered, because no Mermaid tooling is
  installed. Its node IDs and edges were checked by hand.
- The interactive map omits the Mermaid's dashed "outcome brings thread tasks
  forward" edge, because every route for it crossed another relationship. The
  outbox component's tag and the demo card state it instead.
- Nothing was committed or pushed, as the owner asked.
