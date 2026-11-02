# Go personal-assistant demo D9 — time-bounded facts about people (2026-11-02)

Baseline: `4a6b87e` (D8 `970c48c` plus its research reindex).

Request this round implements (2026-11-02): the third of the three approved
retrieval steps ("可以，开始做吧"):

- D7: hybrid lexical retrieval (done);
- D8: local embeddings through Ollama (done);
- D9: time-bounded facts with entities and aliases, this record.

Cloud embeddings and Jev stay off by default.

Scope:

- `go/assistant`:
  - new `facts.go`, `facts_test.go` and `testdata/facts_eval.json`;
  - changes to the store schema, nightly consolidation, profile, retrieval, router
    prompt, tools, scripted model, server and page;
  - four existing tests updated for the new answers (listed below).
- The roadmap, the README baseline, Mermaid, boundary text and repository map.
- The Go README, the demo README, and the parity matrix and plan entries.
- The architecture JSON and HTML, the imagegen base diagram and the research index.

`go/memory` and `go/agent` are unchanged. No dependency was added.

## What changed

- **Tables.**
  - `entities`: id, kind (person or org), name.
  - `entity_aliases`: a lowercase name, address or domain, owned by the first
    entity to claim it.
  - `facts`: subject, predicate, object entity or value, `valid_from`,
    `valid_to`, `superseded_by`, source, evidence.

  `manager`, `works_at` and `role` hold one value at a time. `wrote` and
  `organizes` keep the latest five per person.
- **Facts without a model.** `syncContactFacts` runs at start and in the nightly
  run:
  - each contact becomes an entity known by address and name;
  - a work domain becomes the employer (acme.example → Acme, alias acme);
    friends, family and free-mail domains are skipped, and the 供应商 contact is an
    organization;
  - the contact marked 经理 becomes the user's manager;
  - facts start when the contact was added.

  `syncMailFacts` (nightly) records which inbox threads and calendar meetings known
  contacts started. Strangers never become entities.
- **Extraction.** `FactExtractor` sends the user's channel lines since a cursor (at
  most 40, numbered, with their time) in one tool-less `memory_consolidation`
  request.
  - The answer must use the format `序号 | 主语 | 谓词 | 宾语`.
  - `parseFacts` drops lines with another shape, an unknown predicate, an
    out-of-range number or names over 40 characters.
  - A fact starts at its message's time; the evidence is the channel message
    number.
  - Success moves the cursor; a failure notes "事实提取失败，下次整理再试" and
    keeps it.
  - The scripted model answers from a few fixed Chinese and English phrasings and
    skips questions ("我的经理是谁呀").
- **Timeline and precedence.** `assertFact` keeps facts in start order per
  subject and predicate:
  - a new fact ends where the next one starts, and the previous one ends where it
    starts;
  - sources rank user > contact table > domain, inbox, calendar, and a newer value
    from a weaker source does not replace a stronger current fact;
  - a fact already recorded as current, or with the same start, is not written
    again;
  - an interaction that the five-per-person cap would evict at once is not
    inserted, so re-runs converge.
- **Profile.**
  - The manager line is a view of the current fact and the one before it, for
    example "Carol（自 11/02 起）；之前是 Alice".
  - A contact marked 经理 who is no longer the manager shows as 前经理 under
    常联系.
- **Search.**
  - `MemorySearch.Facts` returns the facts a question is about:
    - facts of the people it names, and facts pointing at them;
    - the user's own facts when it asks for one with a first-person word or names
      nobody;
    - only the asked predicate when there is one;
    - ordered current single-valued, replaced, then interactions, at most six.
  - A mail address names only its own entity, so alice@acme.example is Alice, not
    Acme.
  - The 人物 leg ranks memories by how many named people, or the user's current
    manager, they mention.
  - Hits stating a replaced value, a predicate word and the subject (unless the
    subject is the user) carry `outdated`.
- **Tool, prompt and page.**
  - `memory_search` prints a `<facts>` block before the memories.
  - Each turn's prompt carries up to four facts under 相关的人物事实. The worker
    rules say a current fact wins over a disagreeing memory, and outdated notes
    are not to be trusted.
  - `GET /api/memory/search` returns `{facts, hits}`, and the state view gains
    `facts`.
  - The page shows a 人物与事实 table: replaced rows are struck through with
    "→ new value", and interactions show their date. The table scrolls inside its
    own container on narrow screens.
  - Search shows facts above the hits, with a 可能过期 chip on outdated memories.
  - The ledger row adds "新事实 N 条".

## Evaluation

`testdata/facts_eval.json` has four contacts and eight channel lines over two
months, each also remembered as a memory, plus 11 questions:

- the current manager, asked three ways;
- Dave's employer, asked two ways;
- Carol's role, asked two ways;
- Grace's employer, after two moves;
- Erin's employer and Frank's role, which never changed;
- Bob's employer, which is unknown.

`TestFactsEvaluation` runs the real pipeline: contact facts, then the scripted
extraction, then `updateFacts`.

| Answer taken from | Current value | Replaced value | Neither |
|---|---|---|---|
| Top memory (keyword, alias, substring legs) | 5 | 5 | 1 |
| First fact listed | 11 | 0 | 0 |

- All 8 replaced-value memories in the top 3 (all legs) carry an `outdated` note.
- For Bob no fact is listed, which counts as correct.
- The keyword legs favor the older memory because it shares more words with the
  question ("我的经理是 Alice" versus "我的经理换成 Carol 了").

Caveats:

- The chat lines use the phrasings the scripted rules cover, and both were written
  by the same author, so the fact figure is optimistic. Extraction by a real model
  is unmeasured: no chat model was configured, and pulling a local one was not
  approved.
- The outdated note needs a predicate word in the memory. The memory "Dave 在 Acme
  工作，负责数据平台" carries 工作 for that reason; "Dave 在 Acme 做数据平台" would
  not be marked.
- The D7 lexical evaluation (39/44) and the D8 Ollama evaluation (44/44 at 0.35)
  were re-run and are unchanged. Their stores hold no facts, so the 人物 leg adds
  nothing there.

## Deviations from the plan

- Only the profile's manager line became a fact view; contacts, meeting window and
  preferences are unchanged.
- The model extracts from the user's channel lines, not from memory files. Memory
  files carry no time, and a worker may have written them from mail.
- Same-name people share an entity; there is no disambiguation.
- The 人物 leg adds only the user's own current fact objects (the manager) as
  targets. During the manual run, using every fact object pulled an unrelated
  memory in through an address domain.

## Iteration image

- Image: [go-assistant-facts-2026-11-02.png](go-assistant-facts-2026-11-02.png).
  It also replaces `docs/mini-loop-layered-architecture.png` as the canonical raster
  base (`6a5fd84b…`).
- Exact prompt: [go-assistant-facts-2026-11-02.prompt.txt](go-assistant-facts-2026-11-02.prompt.txt),
  also stored in `docs/mini-loop-layered-architecture.prompt.txt`. It is one edit
  pass on the D8 base.
- Explanation: the bottom band keeps its five-box flow.
  - The fact tables live in `assistant.db`, so no box was added.
  - Box 1 lists 人物事实, box 2 lists 别名向量 and 人物事实, and box 3 reads
    "分诊 · 后台准备 · 夜间整理 · 五路检索". Boxes 1–3 carry the orange outline.
  - The strip reads "人物事实带有效期，新值让旧值过期 · 夜间从你说的话里提取事实 ·
    memory_search：事实在前，过期记忆有标注".
  - The badge reads "2026-11-02：带有效期的人物事实".

**Known image deviations:**

- Box 1's title 模拟信道网页 now renders closer to 模拟值道网页. That text was
  meant to stay unchanged.
- Box 1's 人物事实 is malformed, and 触发器 in box 2 is soft.
- In the strip, 夜间 renders closer to 夜同.
- `world.db` still wraps in the mock-mcp box, and the Task band's two thin strips
  are still missing.

One repair pass was tried with a prompt listing only the broken glyphs
(`imagegen-repair.txt`, kept in the scratchpad). It fixed 夜间 but kept 值道 and
corrupted the small lines of boxes 4 and 5 (客户端, 启动时校验, 模拟邮件/日历). The
first edit was kept. The README Mermaid is canonical; small raster text is not
authoritative.

## Manual runtime evidence

These runs used the built binary with `-embed ollama`, the offline scripted model,
the real `mock-mcp` stdio child and a copy of the D8 data directory.

1. **Upgrade.** Start-up created the three tables and derived three facts from the
   contact table: 我的经理 Alice, Alice → Acme and Dave → Acme. The profile's
   manager line became "Alice <alice@acme.example>（自 11/01 起）", from
   事实表（联系人表）.
2. **Before the nightly run.** The chat lines were "记住 我的经理换成 Carol 了",
   "记住 Dave 跳槽去了 Globex" and "Carol 现在是产品总监". Searching 我的经理是谁
   still listed Alice as current, and the new memory ranked second. Facts change
   only at night.
3. **Nightly run.** The first trigger attempt by curl was refused ("same-origin
   JSON required") because it had no JSON content type; it ran with one.
   - The ledger read "…补检索别名 1 条，新事实 11 条，画像 4 项…".
   - Facts: 我的经理 Carol (from chat, 信道 #37) is current, and Alice expired
     11/02 with "→ Carol". Dave → Globex is current, and Acme expired. Carol's role
     is 产品总监.
   - Contacts' inbox mail and the 1:1 meeting became interactions.
   - The profile read "Carol（自 11/02 起）；之前是 Alice", with 常联系 "Alice（前经理）".
4. **Search and chat.**
   - 我的经理是谁 listed Carol, then Alice (expired). The memory "我的经理换成 Carol
     了" ranked first by 关键词, 人物 and 语义. The old manager memory carried
     "我的经理自 2026-11-02 起是 Carol，不再是 Alice".
   - Dave 在哪工作 → Globex, then Acme (expired).
   - Carol 是什么职位 → 产品总监.
   - Alice 最近来信 → her three threads.
   - 比特币价格 → nothing.
   - In chat, 我老板是谁 got "我记得：我的经理：Carol（自 2026-11-02 起；你在信道里说的）",
     and Dave 在哪工作 got "我记得：Dave 的公司：Globex（自 2026-11-02 起；你在信道里说的）".
5. **Prefetch, found in review.** A mail from Alice put 相关的人物事实 in the
   prompt, including "我的经理：Alice <alice@acme.example>（已过期：… 之后是 Carol）",
   with the old manager memory marked 可能过期. It also listed "Dave 的公司：Acme"
   because "acme" inside her address counted as naming Acme. Addresses now name
   only their own entity; after a rebuild the next mail's prompt had no Dave line.
6. **Restart.** After a restart, 14 facts and the current manager Carol were
   unchanged.
7. **Page.**
   - At 1360 px the facts table showed current rows first and replaced rows struck
     through. Search showed facts above the hits and the 可能过期 chip.
   - Interaction rows first read "自 2026-11-02"; they now show only the date.
   - At 390 px the table first overflowed its 356 px pane (557 px wide) and clipped
     two columns. It now scrolls inside its own container: 328 px wide with 616 px
     of content, no pane overflow and no page scroll.
   - The console showed the existing `favicon.ico` 404 and connection-refused polls
     during restarts only.

**Found in review** (all fixed with tests):

- A question with a question word ("我的经理是谁呀") matched the manager rule.
  The scripted rules now skip names containing 谁, 哪 or 啥.
- `assertFact` first compared only with the current fact, so a late, older value
  got a wrong chain. It now inserts into the timeline, which the test checks.
- Entity lookup tried the name before the addresses; addresses now come first.
- Re-runs could re-insert interactions that the cap evicted at once. Such inserts
  are skipped.
- 常联系 would still label a former manager 经理. It now says 前经理.
- With a capped prompt, interactions could push out "no longer your manager".
  Single-valued facts now come first.

**Existing tests updated:**

- `TestAliasesLetAQuestionInOtherWordsFindTheMemory`: before the nightly run the
  contact table's fact answers and only the 人物 leg finds the manager memory, so
  the test checks the legs through the page API.
- `TestEmbeddingsFindAMemoryBeforeAnyAlias`: the dense leg is checked through the
  page API.
- `TestNightlyConsolidationBuildsTheProfile`: the new manager line format.
- `TestFailedAliasRequestIsRetriedTheNextNight`: the new ledger wording.
- `embed_test.go`: the mode now says 五路.

The d9 daemon was stopped after these runs.

## Validation gates

Resource controls for the Go runs: `GOMAXPROCS=2`, `GOFLAGS=-p=1`, `-parallel=2`,
`GOMEMLIMIT` (1 GiB for vet, 1.5 GiB for tests) and `nice -n 10`. The host had
16 GiB of RAM, with 65% free before the module runs. Each heavy run ran alone.
Pytest ran serially without xdist.

| Gate | Result |
|---|---|
| `gofmt -l .` (in `go/`) | clean |
| `go vet ./...` | pass |
| `go test -count=1 -parallel=2 ./...` | 53 packages ok, exit 0 |
| `go test -race -count=1 -parallel=2 ./...` | 53 packages ok, exit 0 (macOS `ld` LC_DYSYMTAB warnings only) |
| `go test -count=3` on `./assistant/ ./cmd/miniloop-assistant/` | pass |
| `go test -race -count=2` on `./assistant/ ./cmd/miniloop-assistant/` | pass |
| `TestFactsEvaluation`, `TestMemoryRetrievalEvaluation`, `TestMemoryRetrievalWithOllama` (real Ollama) | pass; 11/11 facts, 39/44 lexical, 44/44 four legs |
| `.venv/bin/python -m pytest -q -p no:xdist` | 2256 passed, 28 skipped, 104 warnings (115s) |
| `python/tools/export_go_contracts.py --check` | current (268 files) |
| `python/tools/verify_scans.py` | 19 scanning guards anchored |
| Archify `validate --quality showcase` | ok, 9/9, 0 crossings |
| Archify `deliver` | ok, 9/9 checks; spec `3d4a1772…` (211,109 B), HTML `3f3435b4…` (947,321 B); assistant region inspected in a browser |
| `research-site`: `npm run content`, `npm run content:check` | run in the follow-up reindex commit |
| `git diff --check` | clean |

**Not run, and why:**

- `research-site` `npm test`: `node_modules` is absent, and the local Node 22.12.0
  is below the declared `>=22.13.0`.
- `verify_invariants.py` and `verify_guards.py`: no Python package module and no
  guarded behaviour changed.
- The README Mermaid was not machine-rendered, because no Mermaid tooling is
  installed. A script checked edge endpoints and quote balance.
- No real chat model: fact extraction quality is unmeasured.
