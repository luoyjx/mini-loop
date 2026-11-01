# Go personal-assistant demo D4 — per-thread batches, structured reports, questions for the user (2026-11-01)

Baseline: `14edecb`.

Request this round implements (2026-11-01): do both D4 candidates from the
`dsh-superagent` comparison, then continue with P5 and P6. This record covers D4:

- merge a thread's queued triggers into one turn;
- replace the `[QUIET]` text marker with a structured report.

Scope:

- `go/assistant`:
  - new `report.go` and `report_test.go`;
  - changes to the model, store, router, tools, scripted model, control, rules,
    seed, world, server and page;
  - updated claim call sites in two tests;
- the roadmap, the README baseline, Mermaid, boundary text and repository map;
- the Go README, the demo README, and the parity matrix and plan entries;
- the architecture JSON and HTML, the imagegen base diagram and the research index.

## What changed

- **One turn per batch.** `Store.ClaimBatch` replaces `ClaimTrigger`. With the
  oldest due trigger of a thread that has nothing running, it claims up to seven
  more due pending triggers of that thread.
  - Chat runs with explicit human authority and every other kind untrusted, so chat
    batches only with chat. A routine slot runs alone. Chat is still claimed first,
    and the reserved slot still claims only chat.
  - `Router.dispatch` reads and filters every trigger of the batch before it runs
    anything. Filtered triggers, such as a closed task's check, settle on their own.
  - The rest go to the subscribed worker in one prompt, each under an
    `[event id=… kind=… ref=…]` header.
  - Drafts, tasks and questions are stamped with the oldest trigger, so a re-run
    after a crash converges.
  - A failed turn retries or fails the whole batch, using the highest attempt count
    in the batch.
- **Structured report.** The new `report` tool (local write) records `notify`,
  `ask` or `quiet` for the session's current turn. The router acts on it after the
  turn:
  - `notify` posts the message with any new cards;
  - `ask` posts the question and records it with those messages in one transaction;
  - `quiet` posts nothing and writes the reason to the ledger, unless the turn
    drafted proposals whose cards still need the user.

  A turn without a report is delivered as `notify` with its final text, and the
  ledger notes the missing report. The worker system prompt and every per-kind
  prompt now ask for `report`. `QuietMarker` is removed.
- **Questions.** `questions` holds one question per lead trigger.
  - Open questions are an attention kind (**等你回答**) that can be dismissed and
    still answered later.
  - `POST /api/questions/{id}/answer` moves an open question to answered, posts the
    user's line and enqueues `user.answer` on the question's thread, so the same
    worker continues there.
  - That turn runs untrusted, because the thread's transcript carries external mail.
- **Scripted model.**
  - Background turns end with a `report` call.
  - An email batch reads every mail and answers the newest; two manager mails get
    one reply covering both.
  - A colleague's either-or question is reported as `ask`, and an answer turn
    drafts the reply with the user's choice.
  - A chat batch is answered by its newest message.
- **Presets.** **经理连发两封** delivers two mails on one thread in one poll.
  **同事来问** sends Dave's either-or question.
- **Page.** A question row has an answer box. The row keeps a half-typed answer and
  its focus across the 1.5-second refresh.

## Iteration image

- Image: [go-assistant-batch-report-2026-11-01.png](go-assistant-batch-report-2026-11-01.png).
  It also replaces `docs/mini-loop-layered-architecture.png` as the canonical raster
  base.
- Exact prompt: [go-assistant-batch-report-2026-11-01.prompt.txt](go-assistant-batch-report-2026-11-01.prompt.txt),
  also stored in `docs/mini-loop-layered-architecture.prompt.txt`. It is one edit pass
  on the previous base.
- Explanation: the bottom band "独立可选 Demo · Go 个人助理" keeps its five-box flow.
  - The page box lists 待你处理 and 回答提问.
  - `assistant.db` lists 待答提问.
  - The router box reads "同一 thread 合并一回合 · report 汇报".
  - Boxes 1–3 carry this iteration's orange outline.
  - The strip names the batch rule, the three report outcomes and where an answer
    goes.
  - The badge reads "2026-11-01：合并回合 · 结构化汇报 · 会提问".

**Known image deviations:**

- In box 1 the title's 信 reads close to 伯, and the small line's glyphs are soft
  enough to misread.
- In box 2 the small line is soft: 触发器 reads close to 触发团, and 待答提问 is hard
  to read.
- In the badge, 结构化 is slightly malformed.
- The two thin strips at the foot of the Task container band are still missing, as
  disclosed for earlier iterations.

One repair pass on the text of boxes 1 and 2 was tried. It made box 2 and the strip
worse and changed glyphs in other bands, so it was discarded. The README Mermaid is
canonical; small raster text is not authoritative.

## Manual runtime evidence

These runs used the built binary, the offline scripted model, the real `mock-mcp`
stdio child and a scratch data directory.

1. **Batch.** **经理连发两封** produced triggers #2 and #3 on one thread.
   - #2 reads "已发到信道 2 条（含 1 个待批准提案）（这一回合合并了 2 个触发器）".
   - #3 reads "并入触发器 #2 的回合：…".
   - The channel got one report ("Alice（你的经理）连发 2 封：…我合并起草了一封回复")
     and one card.
2. **Question.** **同事来问** posted "❓ Dave 问：…你怎么定？" and listed the question
   under **需要你处理**. Answering "周三上午" over the API enqueued `user.answer`
   #5 on thread `t-6`. The same worker drafted proposal #2 to Dave with "周三上午"
   in the body.
3. **Page.** The page was inspected at 1360 px and 390 px wide.
   - Text typed into the answer box survived four seconds of refreshes with its
     focus kept.
   - Clicking **回答** posted the answer, and the worker drafted the reply.
   - At 390 px the document is 390 px wide, so there is no horizontal overflow.
   - The only console error was the `favicon.ico` 404.

## Validation gates

Resource controls for the Go runs: `GOMAXPROCS=2`, `GOFLAGS=-p=1`, `-parallel=2`,
`GOMEMLIMIT=1536MiB` and `nice -n 10`. The host had 16 GiB of RAM; `memory_pressure`
reported 45–52% free during the runs. Pytest ran serially without xdist. A
credential-pattern scan of the staged diff found nothing.

| Gate | Result |
|---|---|
| `gofmt -l assistant cmd/miniloop-assistant` | clean |
| `go vet ./...` | pass |
| `go test -count=1 -timeout=600s ./...` | 53 packages ok, exit 0 |
| `go test -race -count=1 -timeout=600s ./...` | 53 packages ok, exit 0 |
| `go test -count=3 ./assistant/ ./cmd/miniloop-assistant/` and `-race -count=2` on both | pass |
| `.venv/bin/python -m pytest -q` | 2256 passed, 28 skipped, 104 warnings (99s) |
| `python/tools/export_go_contracts.py --check` | current (268 files) |
| `python/tools/verify_scans.py` | 19 scanning guards anchored |
| Archify `validate --quality showcase` | 9/9 checks, 0 errors, 0 warnings |
| Archify `deliver` | ok; spec `a31fa571…` (203,902 B), HTML `51773a08…` (930,074 B); assistant region inspected in a browser |
| `research-site`: `npm run content`, `npm run content:check` | regenerated; current (22 documents) |
| `git diff --check` | clean |

**Not run, and why:**

- `research-site` `npm test` (build plus rendered HTML): `node_modules` is absent,
  and the local Node 22.12.0 is below the declared `>=22.13.0`.
- `verify_invariants.py` and `verify_guards.py`: no Python package module and no
  guarded behaviour changed.
- The README Mermaid was not machine-rendered, because no Mermaid tooling is
  installed. Its node IDs and edges were checked by hand.
