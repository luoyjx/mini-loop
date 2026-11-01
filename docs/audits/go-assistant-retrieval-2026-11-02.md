# Go personal-assistant demo D7 — memory search with BM25, aliases and prefetch (2026-11-02)

Baseline: `28b0dc5`.

Request this round implements (2026-11-02): after comparing local embeddings, cloud
and local Jev-style judgment, graph memory and hybrid BM25 + vector retrieval, the
user approved the three-step plan ("可以，开始做吧"):

- D7: hybrid lexical retrieval with no new dependency (this record);
- D8: local embeddings through Ollama (`qwen3-embedding:0.6b`);
- D9: time-bounded facts with entities and aliases.

Cloud embeddings and Jev stay off by default.

Scope:

- `go/assistant`:
  - new `retrieval.go`, `aliases.go`, `retrieval_test.go` and
    `testdata/recall_eval.json`;
  - changes to the app, router, nightly consolidation, store schema, tools,
    scripted model, server and page;
  - `recall_test.go` loses the old lexical evaluation, which the new one replaces.
- The roadmap, the README baseline, Mermaid, boundary text and repository map.
- The Go README, the demo README, and the parity matrix and plan entries.
- The architecture JSON and HTML, the imagegen base diagram and the research index.

`go/memory` and `go/agent` are unchanged. No dependency was added.

## What changed

- **Memory search.** `MemorySearch` builds its index per query from the owner's
  memories. Three legs each rank the memories, and reciprocal rank fusion (k=60)
  merges the ranks. On a tie, more legs win, then the better single rank.
  - 关键词: BM25 (k1=1.2, b=0.75) over name, description and body. Han, kana and
    Hangul runs become overlapping two-character tokens; a lone character stays one
    token. Other letters and digits form lowercase words; single letters and a few
    English function words are dropped.
  - 别名: BM25 over the memory's aliases, counted only when their hash matches the
    memory's current name and body.
  - 子串: the existing `memory.ScopedStore.Search`.

  Each hit names its legs. Nothing is written back to `memory/`.
- **`memory_search` replaces `recall` for new workers.** It is a read-risk typed
  extension (`query` up to 200 characters, `limit` 1–10, default 5). Sessions the
  router creates use `agent.SelectTools` with the assistant's tools plus `remember`
  and `memory_search`. A session restored from an earlier version keeps its old
  catalogue, which still has `recall`.
  - Dropping `recall` also drops the runtime's memory index from the prompt. The
    profile and the prefetch below take its place.
- **Prefetch.** Before each turn, the router searches with what the events are
  about:
  - a mail's sender name, address and subject (not its body);
  - the user's words, or a question and its answer;
  - an event's title and organizer;
  - a task's title or a job's goal.

  Up to three hits, at most 600 characters, go into the prompt under 可能相关的记忆,
  marked as reference data. Routines prefetch nothing. A search failure is logged
  and the turn goes on.
- **Aliases.** After removing duplicates, `Consolidator.Run` lists memories with no
  alias row for their current text (at most 12). `Aliaser.Expand` sends them in one
  model call:
  - purpose `memory_consolidation`, no tools, 60 s timeout;
  - asks for 3–8 synonyms, broader terms and other-language wordings per memory.

  `parseAliases` keeps only the asked memories and at most 8 distinct aliases of 24
  characters or fewer, dropping the memory's own name. `memory_aliases`
  (`name`, `hash`, `aliases`, `updated_at`) stores them and deletes the rows of
  removed memories. A failed call writes no aliases. The ledger row and the nightly
  prompt report the count, and the failure note reads "别名生成失败，下次整理再试".
  The scripted model answers from a ten-entry synonym table.
- **Page.**
  - 画像与记忆 states the retrieval mode and lists each memory's aliases.
  - A search box calls `GET /api/memory/search?q=` and shows the legs of each hit.
  - The header chip reads `记忆 · memory_search/remember`.

## Evaluation

`testdata/recall_eval.json` holds 24 assistant-style memories (Chinese and English)
and 44 queries, each with the one memory it must find in the top 3. The set is
fixed so D8 and D9 are measured on the same questions.
`TestMemoryRetrievalEvaluation` pins the totals.

| Configuration | Topic (8) | Keywords (4) | Questions (10) | Paraphrases (10) | Cross-language (12) | Hits@3 | MRR@3 |
|---|---|---|---|---|---|---|---|
| Substring (old `recall`) | 8 | 4 | 0 | 0 | 0 | 12 / 44 | 0.273 |
| BM25 | 8 | 4 | 10 | 0 | 0 | 22 / 44 | 0.500 |
| BM25 + substring | 8 | 4 | 10 | 0 | 0 | 22 / 44 | 0.500 |
| All three legs | 8 | 4 | 10 | 8 | 9 | 39 / 44 | 0.852 |

- The bigram tokens find every unspaced question (哪天不能开会, 女儿几点放学,
  护照什么时候到期 …), which the substring search missed.
- The aliases find 8 of 10 paraphrases and 9 of 12 cross-language queries.
  **This figure is optimistic.** The aliases in the set were written by the same
  author as the queries, before any model wrote one. A real model's aliases will
  differ, and the scripted model's table covers only the seeded memories.
- The substring leg adds nothing on this set. It is kept for part-of-word matches
  ("Ali" finds alice), which a test covers.
- Still missed: 不能吃什么 (allergy), 网断了打给谁 (home network), teeth cleaning
  and 牙科诊所 (dentist), peanuts (allergy). None shares a word or alias with its
  memory. This is what D8's embeddings are for.

## Deviations from the plan

- BM25 runs in Go memory instead of SQLite FTS5. A few thousand memories index per
  query fast enough. FTS5 (build tag `sqlite_fts5`, not a new dependency) is left
  for indexing mail and chat later.
- A new memory has no aliases until that night's consolidation; until then only the
  keyword and substring legs find it.
- No reranker and no query rewriting.

## Iteration image

- Image: [go-assistant-retrieval-2026-11-02.png](go-assistant-retrieval-2026-11-02.png).
  It also replaces `docs/mini-loop-layered-architecture.png` as the canonical raster
  base (`ac889b33…`).
- Exact prompt: [go-assistant-retrieval-2026-11-02.prompt.txt](go-assistant-retrieval-2026-11-02.prompt.txt),
  also stored in `docs/mini-loop-layered-architecture.prompt.txt`. It is one edit pass
  on the D6 base.
- Explanation: the bottom band "独立可选 Demo · Go 个人助理" keeps its five-box flow.
  Memory search lives inside the router box, so no box was added.
  - The page box lists 记忆试搜.
  - `assistant.db` lists 检索别名.
  - The router box reads "分诊 · 后台准备 · 夜间整理 · 记忆检索".
  - Boxes 1–3 carry this iteration's orange outline.
  - The strip names the three legs and their fusion, the nightly aliases and the
    per-turn prefetch.
  - The badge reads "2026-11-02：混合检索 · 夜间别名 · 预取记忆".

**Known image deviations:**

- Small glyphs in the boxes are soft or malformed:
  - box 1: 记忆试搜 is hard to read;
  - box 2: 检索 in 检索别名 is malformed;
  - box 3: 记忆检索 is malformed.
- In the badge, 名 in 别名 reads close to 衰.
- The mock-mcp box wraps `world.db` onto a second line, as before.
- The two thin strips at the foot of the Task container band are still missing, as
  disclosed for earlier iterations.

No repair pass was tried. The README Mermaid is canonical; small raster text is not
authoritative.

## Manual runtime evidence

These runs used the built binary, the offline scripted model, the real `mock-mcp`
stdio child and a copy of the D6 data directory (three memories: `manager`,
`meeting-preference`, `preference-a94c8e6b`).

1. **Upgrade.** Start-up created `memory_aliases` and reported the retrieval mode.
   No memory had aliases.
2. **Before the nightly run**, page searches:
   - 我老板是谁 and boss found nothing;
   - 哪天不能开会 found `meeting-preference` by 关键词 (its description has 开会);
   - 周五下午有会吗 found the Friday preference by 关键词;
   - newsletter found `meeting-preference` by 关键词 and 子串;
   - Ali found `manager` by 子串.

   In chat, 我老板是谁 got "我没找到相关的记忆。".
3. **Nightly.** **立即运行** produced a quiet trigger. The ledger read "夜间整理：从已发送邮件学到联系人 0 个，去掉重复记忆 0 条，补检索别名 3 条，画像 4 项；…".
   - `manager` got 老板, 上司, 领导 and boss. The table's "manager" was dropped
     because it repeats the memory's name.
   - Both meeting memories got meeting, 日程 and schedule; `meeting-preference` also
     got 订阅邮件 and 简报.
4. **After the nightly run**, page searches:
   - 我老板是谁, boss and 上司 found `manager` by 别名;
   - schedule found both meeting memories.

   In chat, 我老板是谁 got "我记得：我的经理是 Alice（alice@acme.example），她的来信需要当天回复。".
5. **Prefetch.** A delivered manager mail started a worker turn. In `agent.db`, its
   prompt carried 可能相关的记忆 with "· manager：我的经理是 Alice（…）" between the
   profile and the external mail.
6. **Restart.** After stopping the daemon by PID and starting it again, the aliases
   and search results were unchanged.
   - The first restart attempt matched the daemon by absolute path. The daemon had
     been started by a relative path, so only the mock-mcp child got the signal. The
     new process was refused by `daemon.lock`.
7. **Page.**
   - At 1360 px the search button was stretched to half the row; the form now uses
     `1fr auto`.
   - At 390 px the document was 390 px wide and the pane 356 px, with no overflow.
     The empty result ("invoice" on this data) showed 没有匹配的记忆.
   - The console showed only the existing `favicon.ico` 404.

## Validation gates

Resource controls for the Go runs: `GOMAXPROCS=2`, `GOFLAGS=-p=1`, `-parallel=2`,
`GOMEMLIMIT` (1.5 GiB for package runs, 2 GiB for module runs) and `nice -n 10`.
The host had 16 GiB of RAM, with 41% free during the module runs. Pytest ran
serially without xdist.

**Found in review.** Self-review of the diff found that a turn with no subject (a
routine) built an empty prefetch query. `memory_search` treats an empty query as
"list memories", so the nightly prompt would have carried the first memories as
"related". The router now skips the prefetch for an empty query. The integration
test asserts the nightly prompt has no prefetch block; with the guard removed, that
assertion fails.

The module-wide runs started before that one-line guard. The assistant and command
packages were re-run after it.

| Gate | Result |
|---|---|
| `gofmt -l assistant cmd/miniloop-assistant` | clean |
| `go vet ./...` | pass |
| `go test -count=1 -parallel=2 ./...` | 53 packages ok, exit 0 |
| `go test -race -count=1 -parallel=2 ./...` | 53 packages ok, exit 0 (macOS `ld` LC_DYSYMTAB warnings only) |
| after the guard: `go test -count=3` and `-race -count=2` on `./assistant/ ./cmd/miniloop-assistant/` | pass |
| `TestMemoryRetrievalEvaluation -v` | pass; counts above |
| `.venv/bin/python -m pytest -q -p no:xdist` | 2256 passed, 28 skipped, 104 warnings (92s) |
| `python/tools/export_go_contracts.py --check` | current (268 files) |
| `python/tools/verify_scans.py` | 19 scanning guards anchored |
| Archify `validate --quality showcase` | ok, after moving the search component to avoid a crossing with the Preparer edge |
| Archify `deliver` | ok, 9/9 checks; spec `3072ad46…` (208,126 B), HTML `93328791…` (939,905 B); assistant region inspected in a browser |
| `research-site`: `npm run content`, `npm run content:check` | run in the follow-up reindex commit, because the index records the roadmap's last commit |
| `git diff --check` | clean |

**Not run, and why:**

- `research-site` `npm test` (build plus rendered HTML): `node_modules` is absent,
  and the local Node 22.12.0 is below the declared `>=22.13.0`.
- `verify_invariants.py` and `verify_guards.py`: no Python package module and no
  guarded behaviour changed.
- The README Mermaid was not machine-rendered, because no Mermaid tooling is
  installed. Its node IDs and edges were checked by hand.
- No real model was used: alias quality from a real model is unmeasured.
