# Go personal-assistant demo D8 — local embeddings as a fourth retrieval leg (2026-11-02)

Baseline: `66fe1f6` (D7 `c8efea9` plus its research reindex).

Request this round implements (2026-11-02): the second of the three approved
retrieval steps ("可以，开始做吧"):

- D7: hybrid lexical retrieval (done);
- D8: local embeddings through Ollama (`qwen3-embedding:0.6b`), this record;
- D9: time-bounded facts with entities and aliases.

The user approved installing Ollama and the model on this Mac. Cloud embeddings and
Jev stay off by default.

Scope:

- `go/assistant`:
  - new `embed.go` and `embed_test.go`;
  - changes to the app, store schema, health board, retrieval, router, tools, server
    and page;
  - eight no-answer questions added to `testdata/recall_eval.json`.
- `go/cmd/miniloop-assistant`: the `-embed`, `-embed-url` and `-embed-model` flags
  and the LaunchAgent pass-through.
- The roadmap, the README baseline, Mermaid, boundary text and repository map.
- The Go README, the demo README, and the parity matrix and plan entries.
- The architecture JSON and HTML, the imagegen base diagram and the research index.

`go/memory` and `go/agent` are unchanged. No Go dependency was added; the client
uses `net/http`. Ollama 0.40.13 was installed with Homebrew as a local tool and runs
under `ollama serve` started by hand. `brew services` was not used, so no Ollama
LaunchAgent exists, and the assistant's own LaunchAgent is still not installed.

## What changed

- **Embedder.** `OllamaEmbedder` posts `{"model", "input", "truncate": true}` to
  `/api/embed`.
  - It accepts only an http(s) endpoint and a non-empty model.
  - Responses are capped at 64 MiB.
  - A connection error reads "ollama unreachable at …"; a non-2xx status carries
    Ollama's error text; a vector count that does not match the input is an error.
  - Queries are prefixed with the Qwen3 retrieval instruction. Memories are sent as
    name, description and body, cut to 1000 characters.
- **Vector index.** `VectorIndex` keeps vectors in memory and in a new
  `memory_vectors` table: name, an FNV-64 hash of name, description and body,
  model, dimensions and little-endian float32 values.
  - Before a semantic search, `Sync` embeds memories with no current vector, 16 per
    call, and deletes rows of removed memories.
  - The first load deletes rows of another model.
  - Vectors are normalized, so cosine is a dot product.
  - Status (model, endpoint, indexed / total, available, last error, last contact)
    has its own mutex, so the page never waits behind a long sync.
- **Search.** With an embedder, `MemorySearch` adds the 语义 leg to the rank
  fusion: memories with cosine at least `denseFloor` 0.35, top 10. Each hit carries
  its similarity. A failed embedding call skips the leg; the three lexical legs
  still answer.
- **Health.** Each embedding call records success or failure as `HealthEmbed`
  ("语义检索（本地嵌入模型）"). Three failures in a row put it on the attention list,
  and a success clears it.
- **Start.** `App.Start` runs `warmVectors`. It syncs once, then `Probe` embeds one
  short text if nothing has reached the server yet, so the status is true after a
  restart whose vectors were all stored.
- **Prefetch.** A hit qualifies only when the keyword or alias leg found it, or its
  similarity is at least `prefetchDenseFloor` 0.45. A substring match alone no
  longer qualifies.
- **Tool and page.**
  - `memory_search` results carry `similarity="0.56"` on semantic hits.
  - `GET /api/memory/search` returns `similarity`.
  - The page shows 相似度 next to each semantic hit, plus the four-leg mode, the
    index count and the last contact, or "语义检索已配置但暂时不可用（模型：原因），恢复后自动并入".
- **Flags.**
  - `-embed off|ollama`, default off.
  - `-embed-url`, default `http://127.0.0.1:11434`.
  - `-embed-model`, default `qwen3-embedding:0.6b`.
  - The startup line names the embedder.
  - `launchd-plist` validates the flags and passes them through when not off;
    launchd does not start Ollama.

## Evaluation

The set is the D7 set (24 memories, 44 queries) plus eight questions with no answer
(比特币价格, 最喜欢的电影是哪部, 公司年会在哪里办, what is my blood type, 家里的猫叫什么,
favorite band, 明天天气怎么样, 股票账户密码是多少).

A first pass in Python against the same Ollama server measured the semantic leg
with no floor:

- it found all 44;
- correct answers' similarities ranged from 0.324, with a median of 0.533;
- the best no-answer similarity was 0.436.

`TestMemoryRetrievalWithOllama` then swept three floors. It is opt-in
(`MINILOOP_ASSISTANT_OLLAMA_URL=http://127.0.0.1:11434`) and was re-run on the final
code; its JSON report was identical to the first run. Hits in the top 3 of 44, with
MRR@3:

| Floor | Semantic only | Keyword + semantic | Four legs, no aliases | Four legs |
|---|---|---|---|---|
| 0.35 (default) | 43 (0.955) | 43 (0.928) | 43 (0.928) | 44 (0.977) |
| 0.40 | 40 (0.909) | 40 (0.898) | 40 (0.898) | 44 (0.966) |
| 0.45 | 37 (0.841) | 38 (0.852) | 38 (0.852) | 42 (0.932) |

Category counts for the semantic leg at 0.35: topic 8/8, keywords 4/4, questions
10/10, paraphrases 10/10, cross-language 11/12. Its one miss is "out of office";
the four legs find it through the aliases.

| Floor | No-answer questions with any hit | Prefetched |
|---|---|---|
| 0.35 | 6/8 | 1/8 |
| 0.40 | 3/8 | 1/8 |
| 0.45 | 2/8 | 1/8 |

The prefetched one at every floor is 家里的猫叫什么 → `pickup`. It comes through the
keyword leg, because the bigram 家里 is in that memory's description.

Readings:

- Keyword + semantic has a lower MRR than semantic alone. BM25 sometimes puts a
  word-sharing memory above the right one.
- The aliases still matter with embeddings: they lift every floor to 42–44.
- These scores use the author-written aliases from D7, so the four-leg figures stay
  optimistic.

**How the floors were chosen.** Both floors were picked on this same set; there is
no held-out split, and eight negatives give only a rough false-positive estimate.

- 0.35 keeps every right answer the semantic leg finds except one, at the cost of
  weak hits in search results. The page now shows their similarity so they can be
  judged.
- The prefetch floor of 0.45 sits above the best no-answer similarity (0.436).

Before this round's prefetch change, 2 of 8 no-answer questions were prefetched at
every floor. The second was "what is my blood type" → `language`: the substring
leg matched "is" inside "English". That is why a substring match alone no longer
qualifies.

The regular tests use `conceptEmbedder`, a fake that maps words to concept axes. It
covers the code paths, not retrieval quality.

## Deviations from the plan

- The floors were tuned on the evaluation set (see above).
- A new memory is embedded lazily at the next semantic search (about 60 ms for one
  memory here), not when it is written.
- The planned page text "语义检索未启用" is used only when the flag is off. When the
  flag is on but Ollama is down, the page says the leg is temporarily unavailable
  and will rejoin.
- No reranker, no cloud embeddings and no Jev judgments. Roadmap open item 7 keeps
  them for a later decision.

## Iteration image

- Image: [go-assistant-embeddings-2026-11-02.png](go-assistant-embeddings-2026-11-02.png).
  It also replaces `docs/mini-loop-layered-architecture.png` as the canonical raster
  base (`5dcbf5a2…`).
- Exact prompt: [go-assistant-embeddings-2026-11-02.prompt.txt](go-assistant-embeddings-2026-11-02.prompt.txt),
  also stored in `docs/mini-loop-layered-architecture.prompt.txt`. It is one edit
  pass on the D7 base.
- Explanation: the bottom band "独立可选 Demo · Go 个人助理" keeps its five-box flow.
  - The new architecture part, the owner-started Ollama server, is named in the
    strip rather than drawn as a sixth box, because the band has no room for one.
    The README Mermaid (`PAEmbed`) and the interactive map (`paEmbed`) show it as a
    component.
  - `assistant.db` lists 别名与向量.
  - The router box reads "分诊 · 后台准备 · 夜间整理 · 四路检索".
  - Boxes 2 and 3 carry this iteration's orange outline.
  - The strip reads "memory_search：关键词 + 别名 + 子串 + 语义向量 · 语义向量走本机
    Ollama（-embed，默认关） · 连不上时退回词面检索".
  - The badge reads "2026-11-02：本地语义检索 · Ollama 可选".

**Known image deviations:**

- Box 3: 分诊 renders as 分台.
- Box 1: 记忆试搜 is still malformed, carried over from D7.
- The mock-mcp box wraps `world.db` onto a second line, as before.
- The two thin strips at the foot of the Task container band are still missing, as
  disclosed for earlier iterations.

No repair pass was tried. The README Mermaid is canonical; small raster text is not
authoritative.

## Manual runtime evidence

These runs used the built binary with `-embed ollama`, the offline scripted model,
the real `mock-mcp` stdio child, `ollama serve` on `127.0.0.1:11434` with
`qwen3-embedding:0.6b` (`ac6da0dfba84`, 639 MB), and a copy of the D7 data
directory. That directory held three memories, each with nightly aliases.

1. **Upgrade.** Start-up created `memory_vectors` and embedded the three memories
   (indexed 3/3).
2. **New memory.** In chat, "记住 我对花生过敏，订餐时要避开" stored a memory.
   - The page search 不能吃什么 then found it by 语义. That search took 62 ms,
     including embedding the new memory; later searches took about 18 ms.
   - peanuts and 订外卖要注意什么 found it.
   - 我老板是谁 found `manager` by 别名 and 语义.
   - 比特币价格 returned nothing.
3. **Outage.** With Ollama stopped:
   - searches fell back to the lexical legs;
   - the mode read "暂时不可用（…connection refused）";
   - after three failures the attention list showed "语义检索（本地嵌入模型）
     连续失败 3 次".

   With Ollama started again, the next search used the semantic leg and the problem
   cleared without a restart.
4. **Restart, found in review.** A restart loaded four stored vectors and made no
   embedding call. Because nothing reached the server, the status read
   "还没连上过" until the first search. The startup probe was added for this. After
   rebuilding, a restart made exactly one embedding call (the probe), and the state
   reported available with 4/4 indexed.
5. **Page, found in review.** At 1360 px, 不能吃什么 returned the allergy memory and
   a second, weak semantic-only hit (以后周五下午尽量不安排会). Nothing on the page
   showed the second hit was weak, so `similarity` was added to the API and the page.
   - After the change the hits read 相似度 0.56 and 0.39.
   - At 390 px the document was 390 px wide, the search form 328 px and the button
     54 px, with no overflow.
   - The console showed only the existing `favicon.ico` 404.
6. **Prefetch.** The chat 订外卖要注意什么 shares no keyword with the allergy memory.
   Its prompt in `agent.db` carried 可能相关的记忆 with that memory, found by the
   semantic leg above 0.45. The scripted model answered with its help text, because
   it has no rule for that question.

The daemon was stopped after these runs. `ollama serve` was left running for the
evaluation gate.

## Validation gates

Resource controls for the Go runs: `GOMAXPROCS=2`, `GOFLAGS=-p=1`, `-parallel=2`,
`GOMEMLIMIT` (1 GiB for vet, 1.5 GiB for tests) and `nice -n 10`. The host had
16 GiB of RAM, with 51% free before the module runs. Each heavy run ran alone.
Pytest ran serially without xdist.

| Gate | Result |
|---|---|
| `gofmt -l .` (in `go/`) | clean |
| `go vet ./...` | pass |
| `go test -count=1 -parallel=2 ./...` | 53 packages ok, exit 0 |
| `go test -race -count=1 -parallel=2 ./...` | 53 packages ok, exit 0 (macOS `ld` LC_DYSYMTAB warnings only) |
| `go test -count=3` on `./assistant/ ./cmd/miniloop-assistant/` | pass |
| `go test -race -count=2` on `./assistant/ ./cmd/miniloop-assistant/` | pass |
| `TestMemoryRetrievalWithOllama -v` with a real Ollama | pass; counts above, identical to the first run |
| `.venv/bin/python -m pytest -q -p no:xdist` | 2256 passed, 28 skipped, 104 warnings (101s) |
| `python/tools/export_go_contracts.py --check` | current (268 files) |
| `python/tools/verify_scans.py` | 19 scanning guards anchored |
| Archify `validate --quality showcase` | ok, 9/9, 0 crossings |
| Archify `deliver` | ok, 9/9 checks; spec `7e140b08…` (209,555 B), HTML `bbd2264d…` (943,202 B); assistant region inspected in a browser |
| `research-site`: `npm run content`, `npm run content:check` | run in the follow-up reindex commit, because the index records the roadmap's last commit |
| `git diff --check` | clean |

**Not run, and why:**

- `research-site` `npm test` (build plus rendered HTML): `node_modules` is absent,
  and the local Node 22.12.0 is below the declared `>=22.13.0`.
- `verify_invariants.py` and `verify_guards.py`: no Python package module and no
  guarded behaviour changed.
- The README Mermaid was not machine-rendered, because no Mermaid tooling is
  installed. A script checked that every edge endpoint is defined and every new
  label's quotes balance.
- No real model was used for aliases or worker turns. Real-model alias quality, and
  how the four legs behave with real aliases, are unmeasured.
