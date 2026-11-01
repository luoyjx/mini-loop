# Proactive personal-assistant demo (Go addition)

`go/assistant` and `go/cmd/miniloop-assistant` form an opt-in demo. They show how the
Go runtime can grow into a proactive personal agent that runs on your own Mac. The
channel is a simulated local web page. Mail and calendar come from a simulated MCP
server that the daemon starts as a child process. The demo has no Python counterpart
and does not change `cmd/miniloop` defaults. The roadmap is in
[docs/PROACTIVE_AGENT_ROADMAP.md](../../docs/PROACTIVE_AGENT_ROADMAP.md).

## Run it

```sh
cd go
go run ./cmd/miniloop-assistant              # http://127.0.0.1:8790, offline scripted model
go run ./cmd/miniloop-assistant -data /tmp/pa -addr 127.0.0.1:8791
ANTHROPIC_API_KEY=... go run ./cmd/miniloop-assistant -model anthropic -model-id <model>
```

The listener must be a loopback address because the page has no authentication.

State lives in `~/Library/Application Support/mini-loop/assistant` by default
(`os.UserConfigDir()`). Use `-data` to choose another directory.

The daemon starts `miniloop-assistant mock-mcp -data <data>/mock-mcp` as a child.
It checks that the child offers the seven tools it needs before it serves the page.

`-model scripted` is the default. It is a deterministic stand-in that picks tool calls
from the trigger header and earlier tool results. Every call still crosses the real
`ToolGate`. `-model anthropic` uses the Go provider client with
`ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` and `ANTHROPIC_BASE_URL`. These variables are
withheld from the MCP child.

## Keep it running on your Mac

launchd does not read your shell profile, and `go run` binaries are temporary, so
install a built binary first. The LaunchAgent restarts the daemon after a crash, but
not after a clean stop.

```sh
cd go
go build -o ~/.local/bin/miniloop-assistant ./cmd/miniloop-assistant
~/.local/bin/miniloop-assistant launchd-plist > ~/Library/LaunchAgents/dev.mini-loop.assistant.plist
plutil -lint ~/Library/LaunchAgents/dev.mini-loop.assistant.plist
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/dev.mini-loop.assistant.plist
open http://127.0.0.1:8790
```

Day-to-day commands:

```sh
launchctl print gui/$(id -u)/dev.mini-loop.assistant | head -20   # status
tail -f ~/Library/Logs/mini-loop-assistant.log                     # log
launchctl kickstart -k gui/$(id -u)/dev.mini-loop.assistant        # restart after rebuilding
launchctl bootout gui/$(id -u)/dev.mini-loop.assistant             # stop and unload
rm ~/Library/LaunchAgents/dev.mini-loop.assistant.plist            # uninstall
```

`launchd-plist` accepts the daemon's `-addr`, `-data` and `-workers` flags, plus
`-label`, `-binary` and `-log`. It refuses three things: a `go run` binary, a
non-loopback address, and `-model anthropic`. A plist is no place for an API key, and
a Keychain-backed credential source is a later roadmap step. Run the real model from a
shell for now.

## Try it

1. Click **会议邀约：与 1:1 冲突**. The button calls the simulator tool `sim_deliver`,
   which puts the invite into the external inbox. The poller finds it within two
   seconds; the page also wakes the poller right away. The worker reads the invite and
   the calendar through MCP, finds the conflict, and drafts a decline that suggests a
   later time. The draft appears as an approval card.
2. Click **经理来信**. The worker recalls who the manager is and drafts a reply.
3. Click **Newsletter**. The rule fast path ignores it before any model call.
4. Click **可疑邮件**. The mail asks for forwarding and a password. The worker flags it
   and creates no proposal.
5. Click **经理追问** twice. Both follow-ups reach the same subscribed worker session.
6. Approve or deny the cards:
   - An approval moves the card to **已批准 · 发件箱执行中**. The outbox then calls
     `mail_send` or `calendar_respond` with the key `proposal-<id>`, and the card
     shows the receipt.
   - Only these calls move mail into **已发送** or change the calendar.
   - A second approval of the same card returns 409.
7. Stop the process with Ctrl-C, or kill it, then start it again with the same
   `-data`. The following still hold:
   - Pending cards, the channel and the ledger remain.
   - Interrupted triggers are requeued.
   - Approved proposals that were not sent yet are sent once.
   - The next follow-up restores the subscribed worker from `agent.db`.
   - A 07:00 brief missed earlier the same day runs once. This also applies after the
     Mac wakes from sleep.

## Processes and data

| Process | Owns | Talks to |
|---|---|---|
| `miniloop-assistant` daemon | `assistant.db`: triggers, mail cursor and ingested IDs, subscriptions, proposals, channel, ledger. `agent.db`: transcripts, action journal, leases. `memory/` | The page over loopback HTTP; the child over stdio JSON-RPC |
| `miniloop-assistant mock-mcp` child | `mock-mcp/world.db`: mailbox, calendar, effect receipts by idempotency key | Only the daemon, through stdin and stdout |

If the child dies, the next MCP call restarts it. If the daemon dies, the child's
stdin closes and the child exits.

The mock server offers nine tools:

| Tools | Caller | Purpose |
|---|---|---|
| `mail_list`, `mail_read`, `calendar_list`, `calendar_get` | Worker tools and the page view | Reads |
| `mail_poll` | Poller | New inbox mail after a cursor. `limit 0` returns only the head |
| `mail_send`, `calendar_respond` | Outbox | Effects, idempotent per `idempotency_key` |
| `sim_deliver`, `sim_deliver_custom` | Page simulator | The outside world. A real server has no such tools |

## How the six parts map to code

| Part | In this demo | Code |
|---|---|---|
| Own computer | One long-running daemon on your Mac, optionally under launchd. Durable state is described in [Processes and data](#processes-and-data). | `app.go`, `store.go`, `cmd/miniloop-assistant` |
| Integrations | Mail and calendar exist only behind the simulated MCP server. Worker tools are typed wrappers with declared risk: `mail_list`, `mail_read`, `calendar_list` (read), `propose_email`, `propose_invite_response` (write: draft only). The custom catalogue replaces the Bash and file tools. | `world.go`, `mockmcp.go`, `connector.go`, `tools.go` |
| Memory | The existing `remember` and `recall` tools on an owner-scoped `memory.Store`. Facts are seeded on first start. | `seed.go` |
| Routines and triggers | The poller turns new mail into trigger rows, and channel chat becomes trigger rows too. Rules form the fast path. Thread subscriptions route follow-ups to the same worker. The daily brief claims slots exactly once. | `poller.go`, `rules.go`, `router.go`, `routines.go` |
| Approval gates | Workers can only append pending proposals. A human decision moves a proposal from `pending` to `approved` or `denied`. The outbox alone executes approved proposals through MCP and moves them to `sent` or `failed`. | `store.go: DecideProposal`, `outbox.go`, `server.go` |
| Audit log | Append-only ledger rows. The chain is: trigger (poller, channel or scheduler) → rule → route → gated tool call (`action_id`) → proposal → decision (actor) → effect (MCP tool, receipt, key, replay) or failed attempt. | `store.go`, `router.go: OnResult` |

## Boundaries

- The MCP server and its data are simulated. There is no real mailbox or calendar,
  no OAuth and no push notification. The connector speaks only the stdio transport
  that `go/mcp` already has.
- The daemon does not register MCP tools in the agent catalogue. It declares risk on
  its own typed wrappers and keeps `mail_poll` and the effect tools out of model
  reach. The core rule that pins registered MCP tools to `external` is unchanged.
- Trigger runs use untrusted authority. Chat typed on the page uses explicit-human
  authority, because the page is loopback-only.
- The router is deterministic code. The semantic slow path is the worker's own
  judgement, and a worker can answer `[QUIET]` to stay off the channel. There is no
  LLM router, no speculative background work beyond invite drafting, and no Watchdog.
- A re-run after a crash may repeat model calls. Proposal dedupe keys and card checks
  keep it from duplicating drafts or channel cards. Outbox idempotency keys keep it
  from duplicating sends.
- Run only one daemon per data directory; there is no cross-process lease yet.
- Proposals left pending from a first-demo data directory may name mail or events that
  the new mock world does not have. Approving one fails with the server's refusal.
- The process-local `ApprovalBroker` is not used for these effects. Its 300s timeout
  and its restart behavior are unchanged.
