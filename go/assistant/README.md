# Proactive personal-assistant demo (Go addition)

`go/assistant` and `go/cmd/miniloop-assistant` form an opt-in demo. They show how the
Go runtime can grow into a proactive personal agent that runs on your own Mac. The
channel is a simulated local web page. Mail and calendar come from a simulated MCP
server that the daemon starts as a child process. Workers can follow a piece of
work to completion: they create durable tasks, schedule their own checks, and the
daemon wakes them until each task is done. The page tries to take as little of
your attention as possible. One list at the top shows only what waits for you, one
switch pauses everything, and your own messages never wait behind background work.
The demo has no Python counterpart
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
2. Click **经理来信**. The worker recalls who the manager is, drafts a reply and creates
   a task to check within an hour that the reply went out.
3. Click **Newsletter**. The rule fast path ignores it before any model call.
4. Click **可疑邮件**. The mail asks for forwarding and a password. The worker flags it
   and creates no proposal.
5. Click **经理追问** twice. Both follow-ups reach the same subscribed worker session.
   The first one creates a follow-up task. The second sees that task in its prompt and
   reschedules it instead of creating another.
6. Approve or deny the cards:
   - An approval moves the card to **已批准 · 发件箱执行中**. The outbox then calls
     `mail_send` or `calendar_respond` with the key `proposal-<id>`, and the card
     shows the receipt.
   - Only these calls move mail into **已发送** or change the calendar.
   - A second approval of the same card returns 409.
   - Sending the manager reply brings its follow-up task's check forward. The same
     worker wakes, finds the reply in the sent folder and closes the task (**任务**
     tab). A denial also brings the check forward, and the worker closes the task as
     declined.
7. Type **1 分钟后提醒我 喝水** in the channel. The chat worker creates a reminder task,
   and a minute later the scheduler wakes it to post the reminder.
8. Click **日历：12 分钟后有会**. The simulator tool `sim_add_event` books a confirmed
   meeting. The scheduler sees it through `calendar_list` and sends one reminder,
   because the meeting starts within 15 minutes. The reminder names the organizer's
   recent mail and related tasks.
9. Leave a task unanswered. Its worker checks again on its own schedule. After six
   checks the task moves to **转交给你** and appears under **需要你处理**, where
   **再跟进一轮** gives it six more checks. **立即检查** and **取消** on a task card act
   right away.
10. Click **暂停处理** in the header. Deliver a mail and send a chat message: both are
    recorded but not processed, nothing is sent, and no check or reminder fires.
    Running turns finish. Click **恢复处理** and the backlog catches up. The pause also
    holds across a restart.
11. Watch **需要你处理** at the top. It lists drafts to approve, approved proposals
    that finally failed (**重试发送** or **知道了**), tasks handed back to you,
    triggers that failed in the last 24 hours (**重新处理** or **知道了**), and
    components such as the mail poll that failed three times in a row. The list
    disappears when nothing waits for you.
12. Stop the process with Ctrl-C, or kill it, then start it again with the same
    `-data`. The following still hold:
    - Pending cards, the channel, the task board and the ledger remain.
    - A task check missed while the daemon was down, or the Mac asleep, runs on the
      first scheduler tick.
    - Interrupted triggers are requeued.
    - Approved proposals that were not sent yet are sent once.
    - The next follow-up restores the subscribed worker from `agent.db`.
    - A 07:00 brief missed earlier the same day runs once. This also applies after the
      Mac wakes from sleep.
    - A second daemon on the same `-data` refuses to start while the first runs.

## Processes and data

| Process | Owns | Talks to |
|---|---|---|
| `miniloop-assistant` daemon | `assistant.db`: triggers, mail cursor and ingested IDs, subscriptions, tasks, event reminders, proposals, channel, ledger, pause switch, dismissed attention items. `agent.db`: transcripts, action journal, leases. `memory/`. `daemon.lock`, held while the daemon runs | The page over loopback HTTP; the child over stdio JSON-RPC |
| `miniloop-assistant mock-mcp` child | `mock-mcp/world.db`: mailbox, calendar, effect receipts by idempotency key | Only the daemon, through stdin and stdout |

If the child dies, the next MCP call restarts it. If the daemon dies, the child's
stdin closes and the child exits.

The mock server offers ten tools:

| Tools | Caller | Purpose |
|---|---|---|
| `mail_list`, `mail_read`, `calendar_list`, `calendar_get` | Worker tools, the page view and the scheduler (upcoming events) | Reads |
| `mail_poll` | Poller | New inbox mail after a cursor. `limit 0` returns only the head |
| `mail_send`, `calendar_respond` | Outbox | Effects, idempotent per `idempotency_key` |
| `sim_deliver`, `sim_deliver_custom`, `sim_add_event` | Page simulator | The outside world. A real server has no such tools |

## Tasks

A task is follow-up work that outlives one trigger. It is a row in `assistant.db`
bound to the thread and worker session that created it.

| Step | What happens |
|---|---|
| Create | A worker calls `task_create` with a title, what to check and a first check 1 minute to 7 days away. A re-run of the same trigger reuses the task. At most 20 tasks are active. |
| Wake | Each scheduler tick claims due tasks. In one transaction the claim counts the check, enqueues a `task.wakeup` trigger on the task's thread and sets a fallback check 30 minutes out. The trigger reaches the thread's subscribed worker. A task whose previous wakeup is still queued or running waits. |
| Bring forward | An outbox send, a final send failure or a denial on the same thread moves the check to now. The reason appears in the wakeup prompt. So does **立即检查** on the page. |
| Check | The woken worker checks progress with its read tools. It then calls `task_complete`, or `task_schedule` with the next check. If it does neither, the fallback check stands. |
| Close | Done by a worker, cancelled by the user, or stalled after six checks, when the user is told and the scheduler stops waking it. |

Every prompt lists the thread's other active tasks, so a new mail on a thread sees
the work already in flight. A turn driven by external mail may change only the
tasks on its own thread; a turn the user starts in the channel may change any task.

The scheduler also reminds once per confirmed event that starts within 15 minutes,
keyed by event ID and start time. An event that already started is not reminded.

## Attention and control

The page answers "does anything need me?" first. The list under **需要你处理** is
derived from `assistant.db` and an in-memory health board, so its count always
equals its rows.

| Item | Listed while | Actions |
|---|---|---|
| Draft to approve | The proposal is pending | 批准发送 / 拒绝 |
| Failed send | An approved proposal failed for good and was not dismissed | 重试发送: back to the outbox with a fresh attempt budget under the same `proposal-<id>` key / 知道了 |
| Task handed to you | The task used its six checks and was not dismissed | 再跟进一轮: six more checks, starting now, on the same worker / 知道了 |
| Failed trigger | It failed in the last 24 hours and was not dismissed | 重新处理: queued again with a fresh attempt budget / 知道了 |
| Component problem | The mail poll, calendar watch, outbox transport or worker model call failed three times in a row | None; it clears on the next success |

The header shows how many worker turns run. Your chat is claimed before any
background trigger, and one slot beyond `-workers` runs only your chat, so a burst
of mail never makes you wait.

**暂停处理** writes a durable switch. While it is on:

- the router claims nothing, the outbox sends nothing, and the scheduler claims no
  routine, task check or reminder;
- the poller still records new mail, and the page still records your decisions;
- turns already running finish.

**恢复处理** wakes every loop. Missed checks and today's missed routines catch up
as after downtime. An event that started during the pause is not reminded.

Failures are classified before a retry:

- A failed model call and a turn that ran out of rounds come back from the agent
  session as final text. The router treats both as failed turns and never posts that
  text to the channel.
- An MCP refusal, a 4xx model rejection such as bad credentials, and a turn out of
  rounds fail at once and appear in the list.
- Outages, overload, rate limits and timeouts retry up to four attempts with
  backoff.

## How the six parts map to code

| Part | In this demo | Code |
|---|---|---|
| Own computer | One long-running daemon on your Mac, optionally under launchd. Durable state is described in [Processes and data](#processes-and-data). | `app.go`, `store.go`, `cmd/miniloop-assistant` |
| Integrations | Mail and calendar exist only behind the simulated MCP server. Worker tools are typed wrappers with declared risk: `mail_list`, `mail_read`, `calendar_list`, `task_list` (read); `propose_email`, `propose_invite_response` (write: draft only); `task_create`, `task_schedule`, `task_complete` (write: local task board). The custom catalogue replaces the Bash and file tools. | `world.go`, `mockmcp.go`, `connector.go`, `tools.go` |
| Memory | The existing `remember` and `recall` tools on an owner-scoped `memory.Store`. Facts are seeded on first start. | `seed.go` |
| Routines and triggers | The poller turns new mail into trigger rows, and channel chat becomes trigger rows too. Rules form the fast path. Thread subscriptions route follow-ups to the same worker. The scheduler claims the daily brief's slots, due task checks and upcoming-event reminders, each exactly once. Different threads run on parallel workers (`-workers`, default 4, plus one slot for your chat); one thread runs in order. The pause switch holds all of it except mail intake. | `poller.go`, `rules.go`, `router.go`, `routines.go`, `tasks.go`, `control.go` |
| Approval gates | Workers can only append pending proposals. A human decision moves a proposal from `pending` to `approved` or `denied`. The outbox alone executes approved proposals through MCP and moves them to `sent` or `failed`. | `store.go: DecideProposal`, `outbox.go`, `server.go` |
| Audit log | Append-only ledger rows. The chain is: trigger (poller, channel or scheduler) → rule → route → gated tool call (`action_id`) → proposal → decision (actor) → effect (MCP tool, receipt, key, replay) or failed attempt. Task rows record creation, rescheduling, checks brought forward (with the reason), completion, cancellation, hand-over and resumption. Control rows record pause, resume, retries and dismissals. | `store.go`, `tasks.go`, `control.go`, `router.go: OnResult` |

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
  The fallback check and the six-check budget keep a task from being forgotten or
  waking forever; they do not judge whether a check made progress.
- Task checks are ordinary untrusted triggers. A task cannot grant a worker any
  ability it does not already have, and a task check can only draft.
- A re-run after a crash may repeat model calls. Proposal dedupe keys and card checks
  keep it from duplicating drafts or channel cards. Outbox idempotency keys keep it
  from duplicating sends.
- One daemon per data directory: `daemon.lock` is an advisory `flock`, released by
  the kernel when the process exits. It is taken on macOS and Linux only. The mock
  MCP child starts before the lock is checked, so a refused second daemon briefly
  opens the same `world.db`.
- The health board lives in memory; after a restart a component must fail three
  times again before it is listed. A stuck-detector halt still reaches the channel as
  its stop text, because the agent exposes no public accessor for that event.
- Proposals left pending from a first-demo data directory may name mail or events that
  the new mock world does not have. Approving one fails with the server's refusal.
- The process-local `ApprovalBroker` is not used for these effects. Its 300s timeout
  and its restart behavior are unchanged.
