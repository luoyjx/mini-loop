# Go personal-assistant demo — delivery evidence (2026-11-01)

Baseline: `c65073a`. Scope: the opt-in `go/assistant` package and
`go/cmd/miniloop-assistant`; the Go-first [roadmap](../PROACTIVE_AGENT_ROADMAP.md);
README, Go README and parity/plan entries; the architecture JSON/HTML; and the
imagegen base diagram.

## Iteration image

- Image: [go-assistant-demo-2026-11-01.png](go-assistant-demo-2026-11-01.png). It also
  replaces `docs/mini-loop-layered-architecture.png` as the canonical raster base.
- Exact prompt: [go-assistant-demo-2026-11-01.prompt.txt](go-assistant-demo-2026-11-01.prompt.txt),
  also stored in `docs/mini-loop-layered-architecture.prompt.txt`.
- Explanation: the edit adds one bottom band, "独立可选 Demo · Go 个人助理 ·
  go/assistant · 默认关闭". The band contains:
  - the flow 模拟信道网页 → assistant.db 触发器队列 → Router → Worker 会话 → 连接器工具;
  - a strip naming human approval with an atomic effect and ledger, the daily brief
    with same-day catch-up, and restart requeue/restore;
  - an orange badge for 2026-11-01.

  The canvas keeps its original size, so the upper bands are slightly compressed.

  **Known image deviation:** the requested subtitle "cmd/miniloop-assistant ·
  只监听回环地址 · 模拟信道与模拟邮件/日历" was omitted by the generator. The
  loopback-only and simulated-data boundary is stated in the README, the
  architecture JSON card and the demo README. Small raster text in older bands
  was already imperfect and is not authoritative. The README Mermaid is canonical.

## Manual runtime evidence

These runs used the offline scripted model and a scratch data directory.

1. Deliver the conflicting invite, manager mail, newsletter, phishing mail and
   friend chatter, then run the brief. Results:
   - The invite is declined with a suggested later time (pending card).
   - The manager reply is drafted (pending card).
   - The newsletter is ignored by rule.
   - The phishing mail is flagged with no proposal.
   - The friend chatter stays quiet.
   - The brief lists the agenda and the manager's mail.
2. Approve the manager reply. Exactly one mail appears in Sent, and a second
   approval returns 409. Approving the decline from the browser card sets the
   invite to `declined`.
3. Two follow-ups on one thread reach the same worker session.
4. After `kill -9` and a restart on the same data directory:
   - proposals, the channel and the ledger persist;
   - the next follow-up is routed with "订阅直通：从持久状态恢复 worker" to the
     same session ID.
5. SIGTERM shuts the daemon down with exit 0.

## Validation gates

Resource controls for the Go runs: `GOMAXPROCS=2`, `GOFLAGS=-p=1`, `-parallel=2`,
`GOMEMLIMIT=1536MiB` and `nice -n 10`. The host had 16 GiB of RAM with 36–49% free
during the runs.

| Gate | Result |
|---|---|
| `go test -race ./assistant/ ./cmd/miniloop-assistant/` | pass |
| `gofmt -l assistant cmd/miniloop-assistant` | clean |
| `go vet ./...` | pass |
| `go test -count=1 -timeout=300s ./...` | 53 packages ok, exit 0 |
| `go test -race -count=1 -timeout=600s ./...` | 53 packages ok, exit 0 |
| `.venv/bin/python -m pytest -q` | 2256 passed, 28 skipped, 104 warnings (104s) |
| `python/tools/export_go_contracts.py --check` | current (268 files) |
| `python/tools/verify_scans.py` | 19 scanning guards anchored |
| Archify `validate --quality showcase` | 9/9 checks, 0 errors, 0 warnings |
| Archify `deliver` | ok; spec `fa64358f…` (198,474 B), HTML `766a1cd9…` (913,419 B) |
| `research-site`: `npm run content`, `npm run content:check` | pass (22 documents) |
| `git diff --check` | clean |

**Not run, and why:**

- `research-site` `npm test` (build plus rendered HTML): `node_modules` is absent,
  and the local Node 22.12.0 is below the declared `>=22.13.0`.
- `verify_invariants.py` and `verify_guards.py`: no Python package module and no
  guarded behaviour changed.
- The README Mermaid was not machine-rendered, because no Mermaid tooling is
  installed. Its node IDs were checked by hand against existing definitions.
- No push was performed.
