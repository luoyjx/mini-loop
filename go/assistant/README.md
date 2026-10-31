# Proactive personal-assistant demo (Go addition)

`go/assistant` and `go/cmd/miniloop-assistant` form an opt-in demo. They show how the
Go runtime can grow into a proactive personal agent. The channel is a simulated local
web page, and mail and calendar are mock data. The demo has no Python counterpart and
does not change `cmd/miniloop` defaults. The roadmap is in
[docs/PROACTIVE_AGENT_ROADMAP.md](../../docs/PROACTIVE_AGENT_ROADMAP.md).

## Run it

```sh
cd go
go run ./cmd/miniloop-assistant              # http://127.0.0.1:8790, offline scripted model
go run ./cmd/miniloop-assistant -data /tmp/pa -addr 127.0.0.1:8791
ANTHROPIC_API_KEY=... go run ./cmd/miniloop-assistant -model anthropic -model-id <model>
```

The listener must be a loopback address because the page has no authentication.
`-model scripted` is the default. It is a deterministic stand-in that picks tool calls
from the trigger header and earlier tool results. Every call still crosses the real
`ToolGate`. `-model anthropic` uses the Go provider client with
`ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` and `ANTHROPIC_BASE_URL`.

Try this sequence:

1. Click **会议邀约：与 1:1 冲突**. The worker reads the invite and the calendar, finds
   the conflict, and drafts a decline that suggests a later time. The draft appears
   as an approval card.
2. Click **经理来信**. The worker recalls who the manager is and drafts a reply.
3. Click **Newsletter**. The rule fast path ignores it before any model call.
4. Click **可疑邮件**. The mail asks for forwarding and a password. The worker flags it
   and creates no proposal.
5. Click **经理追问** twice. Both follow-ups reach the same subscribed worker session.
6. Approve or deny the cards. Only an approval moves mail into **已发送** or changes
   the calendar.
7. Stop the process with Ctrl-C, or kill it, then start it again with the same
   `-data`. The following still hold:
   - Pending cards, the channel and the ledger remain.
   - Interrupted triggers are requeued.
   - The next follow-up restores the subscribed worker from `agent.db`.
   - A 07:00 brief missed earlier the same day runs once.

## How the six parts map to code

| Part | In this demo | Code |
|---|---|---|
| Own computer | One long-running daemon. `assistant.db` holds the world, triggers, outbox, channel and ledger. `agent.db` (`sqlitestate`) holds transcripts, the action journal and leases. `memory/` holds owner facts. | `app.go`, `store.go` |
| Integrations | Typed extension tools: `mail_list`, `mail_read`, `calendar_list` (read), `propose_email`, `propose_invite_response` (write: draft only). The custom catalogue replaces the Bash and file tools. | `tools.go` |
| Memory | The existing `remember` and `recall` tools on an owner-scoped `memory.Store`. Facts are seeded on first start. | `seed.go` |
| Routines and triggers | Inbound events and chat become trigger rows. Rules form the fast path. Thread subscriptions route follow-ups to the same worker. The daily brief claims slots exactly once. | `rules.go`, `router.go`, `routines.go` |
| Approval gates | Workers can only append pending proposals. The human decision moves `pending` to `sent` or `denied` and applies the mock effect in one transaction. | `store.go: DecideProposal`, `server.go` |
| Audit log | Append-only ledger rows: trigger → rule → route → gated tool call (`action_id`) → proposal → decision (actor) → effect. | `store.go`, `router.go: OnResult` |

## Boundaries

- The mail and calendar connectors are simulated. There are no OAuth, webhook
  signatures, push notifications or real sending.
- Trigger runs use untrusted authority. Chat typed on the page uses explicit-human
  authority, because the page is loopback-only.
- The router is deterministic code. The semantic slow path is the worker's own
  judgement, and a worker can answer `[QUIET]` to stay off the channel. There is no
  LLM router, speculative background work beyond invite drafting, or Watchdog.
- A re-run after a crash may repeat model calls. Proposal dedupe keys and card checks
  keep it from duplicating drafts or channel cards.
- The process-local `ApprovalBroker` is not used for these effects. Its 300s timeout
  and its restart behavior are unchanged.
