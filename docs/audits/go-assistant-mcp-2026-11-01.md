# Go personal-assistant demo D1 — simulated MCP, polling, outbox, local Mac deployment (2026-11-01)

Baseline: `b1a0a72`.

Decisions this round implements (2026-11-01): run the demo on the owner's own Mac,
keep the simulated channel page instead of a real channel, and integrate mail and
calendar through MCP, simulated first.

Scope:

- `go/assistant`: new `world.go`, `mockmcp.go`, `connector.go`, `poller.go`,
  `outbox.go` and `mcp_test.go`; changes to the store, tools, router, app, server,
  routines, seed and page;
- `go/cmd/miniloop-assistant`: the `mock-mcp` and `launchd-plist` subcommands and the
  default data directory;
- the [roadmap](../PROACTIVE_AGENT_ROADMAP.md), the README baseline, Mermaid and
  boundary text, the Go README, and the parity matrix and plan entries;
- the architecture JSON and HTML, and the imagegen base diagram.

## Iteration image

- Image: [go-assistant-mcp-2026-11-01.png](go-assistant-mcp-2026-11-01.png). It also
  replaces `docs/mini-loop-layered-architecture.png` as the canonical raster base.
- Exact prompts: [go-assistant-mcp-2026-11-01.prompt.txt](go-assistant-mcp-2026-11-01.prompt.txt),
  also stored in `docs/mini-loop-layered-architecture.prompt.txt`. The image came from
  three chained edit passes, and the file lists each pass's prompt and inputs in order.
- Explanation: the bottom band "独立可选 Demo · Go 个人助理 · go/assistant · 默认关闭 ·
  部署在本机 Mac" now shows:
  - the flow 模拟信道网页 → assistant.db → Router + Worker 会话 → MCP 连接器 →
    mock-mcp 子进程, with the last two boxes outlined in orange as this iteration's
    change;
  - a strip naming `mail_poll` polling, outbox sends through MCP with the
    `proposal-id` idempotency key, and launchd with crash restart and sleep catch-up;
  - an orange badge "2026-11-01：模拟 MCP + 轮询 + 发件箱 + launchd".

**Known image deviation:** to fit the taller bottom band, the generator dropped the
two thin text strips at the foot of the Task container band. These are its
checkpoint/save detail line and its memory/API key line. The repair pass (pass 2)
restored them but garbled the new band's wording. The final pass (pass 3) fixed the
wording and dropped the strips again. The result was accepted after three passes.
Those Task details remain in the README Mermaid, the architecture JSON and
`deepagents-server/TASK_SERVER.md`. Small raster text is not authoritative; the
README Mermaid is canonical.

## Manual runtime evidence

These runs used the built binary, the offline scripted model and scratch data
directories.

1. **Startup.** The daemon starts `mock-mcp` as a child, verifies its tools and logs
   `mcp=mock-mailcal over stdio, 9 tools`. The data directory holds `assistant.db`,
   `agent.db`, `memory/` and `mock-mcp/world.db`.
2. **Delivery.** Five simulator deliveries return mail IDs, and the poller turns
   each one into a trigger:
   - the invite is declined with a suggested time (pending card);
   - the manager mail gets a drafted reply (pending card);
   - the newsletter is ignored by rule;
   - the phishing mail is flagged with no proposal;
   - the friend chatter stays quiet.

   The same-day 07:00 brief ran once as a catch-up.
3. **Approval.**
   - Approving the reply returns `approved`. A second approval returns 409.
   - The outbox then sends both proposals through MCP with receipts `sent-9` and
     `sent-10`. The invite becomes `declined`.
   - The ledger records the `mail_send` and `calendar_respond` effects with receipt
     and key.
   - Approving another card from the browser moves it to 已发出 with its receipt.
4. **Child crash.** After `kill -9` of the MCP child, the next delivery succeeds on a
   restarted child and the page shows no integration error.
5. **Daemon crash.** After `kill -9` of the daemon, the orphaned child exits when its
   stdin closes. After a restart on the same directory:
   - the follow-up is routed with "订阅直通：从持久状态恢复 worker" to the same
     session;
   - the brief does not fire again.
6. **Shutdown.** SIGTERM stops the daemon and the child, with exit 0.
7. **Upgrade.** A data directory from the first demo is upgraded in place: the
   proposals table gains the four outbox columns and the old statuses are kept, and
   the mock world seeds itself.

## Validation gates

Resource controls for the Go runs: `GOMAXPROCS=2`, `GOFLAGS=-p=1`, `-parallel=2`,
`GOMEMLIMIT=1536MiB` and `nice -n 10`. The host had 16 GiB of RAM with 46–49% free
during the runs.

| Gate | Result |
|---|---|
| `gofmt -l assistant cmd/miniloop-assistant` | clean |
| `go test -count=3 ./assistant/` and `-race -count=2` on both demo packages | pass |
| `go vet ./...` | pass |
| `go test -count=1 -timeout=600s ./...` | 53 packages ok, exit 0 |
| `go test -race -count=1 -timeout=600s ./...` | 53 packages ok, exit 0 |
| `.venv/bin/python -m pytest -q` | 2256 passed, 28 skipped, 104 warnings (101s) |
| `python/tools/export_go_contracts.py --check` | current (268 files) |
| `python/tools/verify_scans.py` | 19 scanning guards anchored |
| Archify `validate --quality showcase` | 9/9 checks, 0 errors, 0 warnings, 0 crossings |
| Archify `deliver` | ok; spec `eb13be5c…` (201,428 B), HTML `6d533bae…` (924,494 B); visually inspected in a browser |
| `research-site`: `npm run content`, `npm run content:check` | see the commit that regenerates the index |
| `git diff --check` | clean |

**Not run, and why:**

- `research-site` `npm test` (build plus rendered HTML): `node_modules` is absent,
  and the local Node 22.12.0 is below the declared `>=22.13.0`.
- `verify_invariants.py` and `verify_guards.py`: no Python package module and no
  guarded behaviour changed.
- The README Mermaid was not machine-rendered, because no Mermaid tooling is
  installed. Its node IDs and edges were checked by hand.
- The LaunchAgent was not installed or loaded on this machine, because that is
  persistent user configuration. Its plist is generated and checked with
  `plutil -lint` by the tests.
- No push was performed.
