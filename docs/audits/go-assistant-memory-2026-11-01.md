# Go personal-assistant demo D6 — chain recall, nightly consolidation, user profile (2026-11-01)

Baseline: `75ba6b4`.

Request this round implements (2026-11-01): do both D4 candidates, then roadmap P5
and P6. This record covers P6, delivered as demo iteration D6:

- chain recall;
- nightly memory consolidation;
- a structured user profile;
- an evaluation of semantic retrieval, with any new dependency left for approval.

Scope:

- `go/assistant`:
  - new `lookup.go`, `nightly.go`, `profile.go` and `recall_test.go`;
  - changes to the model, store, router, tools, scripted model, seed, server, app
    and page;
- the roadmap, the README baseline, Mermaid, boundary text and repository map;
- the Go README, the demo README, and the parity matrix and plan entries;
- the architecture JSON and HTML, the imagegen base diagram and the research index.

`go/memory` is unchanged.

## What changed

- **Chain recall.** `Store.Lookup` resolves one ref and returns a summary plus the
  refs one hop away, never the ref itself.
  - `trigger-<id>` lists its thread and worker session, the drafts, tasks,
    questions and jobs it started, and the task, question or job it refers to.
  - `task-`, `proposal-`, `question-` and `job-` refs list their own trigger,
    thread and session.
  - `thread:` lists the thread's newest triggers and its subscribed worker.
  - `session:` lists the triggers a worker handled.

  An unknown or malformed ref is `ErrUnknownRef`. Output is capped at 1,600
  characters, and summaries are labelled as possibly quoting external mail. The new
  `lookup` tool (read risk) wraps it. The scripted model answers "追溯 <ref>" in
  chat with it.
- **Nightly consolidation.** `NightlyRoutine` (夜间整理, 23:30) is seeded under meta
  `nightly_seeded`, so older data directories gain it. When the router reads its
  trigger, `Consolidator.Run` runs before the worker turn:
  - recipients of the last 30 sent mails that are not contacts are added with
    source 夜间整理. Senders are never added, so a stranger cannot become known and
    skip the triage by writing twice;
  - seven days of the calendar give the window of confirmed meetings;
  - memories whose body repeats another's, ignoring case, spaces and punctuation,
    are removed through `ScopedStore.ReplaceAll` inside `WithLifecycle`. An
    explicit memory is kept first, then a `feedback` memory. Nothing is rewritten
    when nothing repeats;
  - the profile and one `memory` ledger row are written in one transaction;
  - the user's channel lines from that day are attached to the prompt.

  An MCP outage retries the trigger. The worker turn sees the summary and the chat
  and is asked to remember only stated preferences. The scripted model remembers
  lines with preference words under names derived from their text, so a re-run
  overwrites instead of duplicating.
- **Profile.** The `profile` table holds manager, frequent contacts, meeting window
  and preferences (the first three `feedback` memories).
  - `refreshProfile` rebuilds the lines that need no MCP call at start and after
    the nightly turn.
  - Every worker prompt carries the profile under 用户画像, marked as reference
    data rather than instructions.
- **Page.** The world tab has **画像与记忆**, with the profile and the owner's
  memories (type and origin). The nightly routine is listed under **Routines** with
  **立即运行**. A stated preference in chat is acknowledged. Two quick prompts were
  added.
- **Retrieval evaluation.** `TestLexicalMemoryRecallEvaluation` measures the
  existing lexical search; results are below.

## Deviations from the roadmap text

- Contacts are learned only from sent mail, not from senders.
- Consolidation removes exact duplicates after normalization. It does not merge
  near-duplicates or rewrite memory text.
- The profile's habit line is the meeting window computed from the calendar.
- No semantic retrieval: it needs an embedding model or library, which is a new
  dependency that needs approval.

## Retrieval evaluation

Ten assistant-style memories (Chinese and English) in a scratch `memory.Store`,
searched with `ScopedStore.Search(query, 3)`:

| Query set | Found in the top 3 | Missed |
|---|---|---|
| Topic words: 经理, newsletter, 周三, 爬山, 账单, 放学, English, clinic | 8 / 8 | — |
| Space-separated keywords: 周三 开会, 出差 座位, 早上 邮件, Alice 回复 | 4 / 4 | — |
| Paraphrases: 我老板是谁, boss, 哪天不能开会, 周三下午有空吗, 牙医, 坐飞机的习惯, invoice, 接孩子 | 0 / 8 | all |

Search splits a query only at non-letters, and each term must occur as a
substring. An unspaced Chinese question is therefore one long term, and a synonym
shares no substring. The test pins these counts, so an improvement shows up as a
failing expectation to update. Semantic retrieval is listed as an open decision in
the roadmap.

## Iteration image

- Image: [go-assistant-memory-2026-11-01.png](go-assistant-memory-2026-11-01.png).
  It also replaces `docs/mini-loop-layered-architecture.png` as the canonical raster
  base (`7ccbaf80…`).
- Exact prompt: [go-assistant-memory-2026-11-01.prompt.txt](go-assistant-memory-2026-11-01.prompt.txt),
  also stored in `docs/mini-loop-layered-architecture.prompt.txt`. It is one edit pass
  on the D5 base.
- Explanation: the bottom band "独立可选 Demo · Go 个人助理" keeps its five-box flow.
  The new part, the nightly consolidation, joins the router box.
  - The page box lists 画像与记忆.
  - `assistant.db` lists 用户画像.
  - The router box reads "分诊 · 后台准备 · 夜间整理 · 链式召回".
  - Boxes 1–3 carry this iteration's orange outline.
  - The strip names `lookup`, what the nightly consolidation does and where the
    profile goes.
  - The badge reads "2026-11-01：链式召回 · 夜间整理 · 用户画像".

**Known image deviations:**

- Small glyphs are soft. In box 1, 待你处理 and 画像与记忆 are hard to read; the
  latter reads close to 画像记忆. In box 2, 用户画像 reads close to 用户低务.
- In the badge, 链 is slightly malformed.
- The mock-mcp box wraps `world.db` onto a second line, as before.
- The two thin strips at the foot of the Task container band are still missing, as
  disclosed for earlier iterations.

No repair pass was tried. The README Mermaid is canonical; small raster text is not
authoritative.

## Manual runtime evidence

These runs used the built binary, the offline scripted model, the real `mock-mcp`
stdio child and a copy of the D5 data directory.

1. **Upgrade.** Starting on the D5 data seeded `nightly-consolidation` at 23:30
   next to the brief. The start-up profile had three lines: manager "Alice
   <alice@acme.example>", frequent contacts "Alice（经理） 往来 1 次", and the
   seeded meeting preference.
2. **Chat and lookup.**
   - "以后周五下午尽量不安排会" got "收到。这听起来是你的偏好，今晚 23:30 的夜间整理会把它记下来。".
   - "记住 日程冲突的邀约婉拒并建议改期，newsletter 不用提醒" stored a duplicate of
     the seeded preference as a `user` memory.
   - "追溯 trigger-6" answered with the manager-mail trigger, its decision and
     "下一跳：thread:t-9 · session:4c301c829ca1 · proposal-1 · task-1".
3. **Nightly.** **立即运行** produced routine trigger #19, quiet with
   "夜间整理完成：从今天的对话里记下 1 条偏好。".
   - The ledger read "从已发送邮件学到联系人 0 个，去掉重复记忆 1 条，画像 4 项；会议时间 通常在 10:00–16:00 之间（未来 7 天 2 个已确认会议）".
   - The memories were the new `preference-a94c8e6b` (feedback), the seeded
     `meeting-preference` and `manager`.
   - The profile gained the meeting window, and its preferences line read
     "以后周五下午尽量不安排会；日程冲突的邀约婉拒并建议改期；newsletter 不用提醒。".

   The first build kept the chat note and dropped the seeded `feedback` memory,
   which emptied that preference from the profile. The keep rule now ranks explicit
   origin, then `feedback`. The test covers this with an explicit `user` duplicate.
4. **Restart.** After a restart the profile and memories were unchanged.
5. **Page.** The page was inspected at 1360 px and 390 px wide.
   - At 390 px the profile labels wrapped one character per line, and a fourth
     column overflowed the pane. Labels no longer wrap, and the update time moved
     into the source cell.
   - Afterwards the document and pane were 390 px and 356 px wide, with no
     overflow.
   - The browser console showed no errors.

## Validation gates

Resource controls for the Go runs: `GOMAXPROCS=2`, `GOFLAGS=-p=1`, `-parallel=2`,
`GOMEMLIMIT` (1.5 GiB for package runs, 2 GiB for module runs) and `nice -n 10`.
The host had 16 GiB of RAM. Pytest ran serially without xdist.

| Gate | Result |
|---|---|
| `gofmt -l assistant cmd/miniloop-assistant` | clean |
| `go vet ./...` | pass |
| `go test -count=1 -parallel=2 ./...` | 53 packages ok, exit 0 |
| `go test -race -count=1 -parallel=2 ./...` | 53 packages ok, exit 0 |
| `go test -count=3 ./assistant/ ./cmd/miniloop-assistant/` and `-race -count=2` on both | pass |
| `TestLexicalMemoryRecallEvaluation -v` | pass; counts above |
| `.venv/bin/python -m pytest -q -p no:xdist` | 2256 passed, 28 skipped, 104 warnings (97s) |
| `python/tools/export_go_contracts.py --check` | current (268 files) |
| `python/tools/verify_scans.py` | 19 scanning guards anchored |
| Archify `validate --quality showcase` | ok |
| Archify `deliver` | ok, 9/9 checks; spec `fc4e324e…` (207,089 B), HTML `eea5fd25…` (937,105 B); assistant region inspected in a browser |
| `research-site`: `npm run content`, `npm run content:check` | run in the follow-up reindex commit, because the index records the roadmap's last commit |
| `git diff --check` | clean |

**Not run, and why:**

- `research-site` `npm test` (build plus rendered HTML): `node_modules` is absent,
  and the local Node 22.12.0 is below the declared `>=22.13.0`.
- `verify_invariants.py` and `verify_guards.py`: no Python package module and no
  guarded behaviour changed.
- The README Mermaid was not machine-rendered, because no Mermaid tooling is
  installed. Its node IDs and edges were checked by hand.
