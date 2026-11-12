# Proactive personal-assistant demo (Go addition)

`go/assistant` and `go/cmd/miniloop-assistant` form an opt-in demo. They show how the
Go runtime can grow into a proactive personal agent that runs on your own Mac. The
main channel is a local web page. Opt-in [remote channels](#remote-channels) let
you talk to the same assistant from a private chat with your Telegram bot, a DM
with your Discord bot, or email. Mail and calendar come from a simulated MCP
server that the daemon starts as a child process. Workers can follow a piece of
work to completion: they create durable tasks, schedule their own checks, and the
daemon wakes them until each task is done. The page tries to take as little of
your attention as possible. One list at the top shows only what waits for you, one
switch pauses everything, and your own messages never wait behind background work.
Events that queue up on one thread while its worker is busy are handled together
in one turn. A worker ends each turn with a structured report: tell you something,
stay quiet, or ask you one question whose answer goes back to the same worker.
Mail is graded by sender and address before any model call. An unclear mail passes
a short triage and by default waits for you under **候选** instead of starting
work. Workers can prepare material in a background read-only session, and a
watchdog flags tasks whose checks stop making progress. Workers can trace a piece of
work through its records, and a nightly routine consolidates memory into a short
profile that every worker sees. An opt-in [fleet host](#many-owners-the-fleet-host)
runs many owners' assistants in one process and lets each sleep while it is idle.
The demo has no Python counterpart
and does not change `cmd/miniloop` defaults. The roadmap is in
[docs/PROACTIVE_AGENT_ROADMAP.md](../../docs/PROACTIVE_AGENT_ROADMAP.md).

## Run it

```sh
cd go
go run ./cmd/miniloop-assistant              # http://127.0.0.1:8790, offline scripted model
go run ./cmd/miniloop-assistant -data /tmp/pa -addr 127.0.0.1:8791
ANTHROPIC_API_KEY=... go run ./cmd/miniloop-assistant -model anthropic -model-id <model>
go run ./cmd/miniloop-assistant -embed ollama     # adds local semantic memory search
MINILOOP_TELEGRAM_TOKEN=... go run ./cmd/miniloop-assistant -telegram-chat <your user ID>
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

`-embed off` is the default. `-embed ollama` adds a semantic leg to memory search
through a local [Ollama](https://ollama.com) server, which you start yourself:

```sh
brew install ollama
ollama serve &                        # or the Ollama app
ollama pull qwen3-embedding:0.6b      # about 640 MB, Apache-2.0
```

`-embed-url` (default `http://127.0.0.1:11434`) and `-embed-model` (default
`qwen3-embedding:0.6b`) choose the server and model. See
[Memory search](#memory-search).

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

`launchd-plist` accepts the daemon's `-addr`, `-data`, `-workers` and `-embed*`
flags, plus `-label`, `-binary` and `-log`. launchd does not start Ollama; with
`-embed ollama`, keep the Ollama app or server running, or search stays lexical. It
refuses four things: a `go run` binary, a non-loopback address, `-model anthropic`
and the remote channel flags. A plist is no place for an API key or a bot token, and
a Keychain-backed credential source is a later roadmap step. Run the real model and
remote channels from a shell for now.

## Try it

1. Click **会议邀约：与 1:1 冲突**. The button calls the simulator tool `sim_deliver`,
   which puts the invite into the external inbox. The poller finds it within two
   seconds; the page also wakes the poller right away. The worker reads the invite and
   the calendar through MCP, finds the conflict, and drafts a decline that suggests a
   later time. The draft appears as an approval card.
2. Click **经理来信**. The worker searches its memory for who the manager is, drafts a reply and creates
   a task to check within an hour that the reply went out.
3. Click **Newsletter**. The rule fast path ignores it before any model call.
4. Click **可疑邮件**. The sender is a stranger writing to you, so the mail is an
   inferred candidate and passes the triage first. The triage would hand it to a
   worker, but the default observe mode only records it under **候选**. Click
   **交给 worker** there: the worker flags the request for forwarding and a password
   and creates no proposal.
5. Click **经理追问** twice. Both follow-ups reach the same subscribed worker session.
   The first one creates a follow-up task. The second sees that task in its prompt and
   reschedules it instead of creating another. Two quick clicks run as one batch.
6. Click **经理连发两封**. Two mails land on one thread in the same poll. The router
   claims both as one batch, and the worker drafts one reply that covers both and
   posts one message. In **触发器**, the second trigger reads **并入触发器 #…**.
7. Click **同事来问**. Dave asks you to pick a day, which only you can decide, so the
   worker reports `ask`. The question appears in the channel and under
   **需要你处理**. Type an answer there and click **回答**: the answer goes back to
   the same thread's worker, which drafts the reply with your choice.
8. Approve or deny the cards:
   - An approval moves the card to **已批准 · 发件箱执行中**. The outbox then calls
     `mail_send` or `calendar_respond` with the key `proposal-<id>`, and the card
     shows the receipt.
   - Only these calls move mail into **已发送** or change the calendar.
   - A second approval of the same card returns 409.
   - Sending the manager reply brings its follow-up task's check forward. The same
     worker wakes, finds the reply in the sent folder and closes the task (**任务**
     tab). A denial also brings the check forward, and the worker closes the task as
     declined.
9. Type **1 分钟后提醒我 喝水** in the channel. The chat worker creates a reminder task,
   and a minute later the scheduler wakes it to post the reminder.
10. Click **日历：12 分钟后有会**. The simulator tool `sim_add_event` books a confirmed
   meeting. The scheduler sees it through `calendar_list` and sends one reminder,
   because the meeting starts within 15 minutes. The reminder names the organizer's
   recent mail and related tasks.
11. Leave a task unanswered. Its worker checks again on its own schedule. When three
   checks in a row record the same check and note, the watchdog lists the task as
   **可能卡住** under **需要你处理** (**立即检查**, **取消任务** or **知道了**). After
   six checks the task moves to **转交给你**, where **再跟进一轮** gives it six more
   checks. **立即检查** and **取消** on a task card act right away.
12. Click **暂停处理** in the header. Deliver a mail and send a chat message: both are
    recorded but not processed, nothing is sent, and no check or reminder fires.
    Running turns finish. Click **恢复处理** and the backlog catches up. The pause also
    holds across a restart.
13. Click **经理群发，你在抄送里**. Alice is a known contact but writes to the team
    and copies you, so the triage records the mail under **候选**. Click
    **陌生人：合作推广**: the triage ignores the promotion. Click
    **供应商账单（后台核对）**: the vendor is a known contact, so the worker runs,
    starts a background job with `prepare_start` and stays quiet. The Preparer
    compares the bill with last month's in a read-only session, and the result
    returns to the bill's thread, where the worker posts the comparison. The job is
    listed under **任务 → 后台准备任务**, with **停止** while it runs. Switch
    **推断候选** to **自动** to let the triage hand candidates to workers directly.
14. Type **以后周五下午尽量不安排会** in the channel: the assistant notes it as a
    preference for tonight. Type **追溯 trigger-1**: the worker answers with the
    `lookup` tool, listing the trigger and the refs one hop away. Under
    **Routines**, click **立即运行** next to **夜间整理**. The daemon consolidates
    contacts, the meeting window and duplicate memories, and the worker remembers
    your preference. The result shows under **画像与记忆**, and later worker prompts
    carry the profile.
15. Watch **需要你处理** at the top. It lists drafts to approve, approved proposals
    that finally failed (**重试发送** or **知道了**), tasks handed back to you or flagged by the watchdog,
    triggers that failed in the last 24 hours (**重新处理** or **知道了**), and
    components such as the mail poll that failed three times in a row. The list
    disappears when nothing waits for you.
16. Stop the process with Ctrl-C, or kill it, then start it again with the same
    `-data`. The following still hold:
    - Pending cards, the channel, the task board and the ledger remain.
    - A task check missed while the daemon was down, or the Mac asleep, runs on the
      first scheduler tick.
    - Interrupted triggers and preparation jobs are requeued.
    - Approved proposals that were not sent yet are sent once.
    - The next follow-up restores the subscribed worker from `agent.db`.
    - A 07:00 brief missed earlier the same day runs once. This also applies after the
      Mac wakes from sleep.
    - A second daemon on the same `-data` refuses to start while the first runs.
17. With a [remote channel](#remote-channels) on, send your bot **记住 周五下午不安排会**.
    The line appears on the page as **来自 Telegram** and the reply comes back to the
    bot, and to every other remote channel. Click **同事来问**: the question reaches the
    bot with **（直接回复这条消息即可回答）**. Reply to that message with **周三上午**:
    the answer goes back to Dave's worker, which drafts the reply. The approval card
    reaches the bot too, but you approve it on the page.

## Remote channels

Remote channels are off by default. Each one connects the assistant to one person,
you, on a service you already use. They share the page's conversation:

- **Inbound**: your messages join the page's chat thread, tagged with where they
  came from, and run with the same explicit human authority as typing on the page.
  A reply to a question's message answers that question, exactly as **回答** does.
- **Outbound**: the assistant's lines and the system's lines (not your own) are
  copied to every enabled channel, in channel order. An approval card arrives with
  **（到本机助理网页批准或拒绝；这里不能批准）**: approvals stay on the loopback page.
- **Pause**: while you pause, your messages are still recorded and copying waits.

| Channel | Turn it on | Secret (environment only) | Reads | Accepts only |
|---|---|---|---|---|
| Telegram | `-telegram-chat <your user ID>` | `MINILOOP_TELEGRAM_TOKEN` | `getUpdates` long poll, 25 s | Your private chat with the bot: chat ID and sender ID equal your user ID |
| Discord | `-discord-user <your user ID>` | `MINILOOP_DISCORD_TOKEN` | The DM over REST, every 3 s | Messages in the bot's DM with you, by your user ID |
| Email | `-email-owner`, `-email-user`, `-email-imap`, `-email-smtp`, `-email-authserv` | `MINILOOP_EMAIL_PASSWORD` | The inbox over IMAP, read-only, every 30 s | Mail from your address that the receiving server authenticated for your domain |

Setting each service up:

- **Telegram**: create a bot with @BotFather and copy its token. Open a chat with
  the bot and send it anything. Find your user ID, for example with @userinfobot.
  Use a bot no other program polls: Telegram serves one `getUpdates` reader per bot,
  and a second one, or a webhook, makes both fail with 409. `-telegram-api` points
  at a self-hosted Bot API server.
- **Discord**: in the Developer Portal, create an application and a bot and copy the
  token. Invite the bot to a server you are in; it needs no permissions there. Allow
  DMs from members of that server, then turn on Developer Mode to copy your user ID.
  The daemon opens the DM itself. It reads the DM over REST every three seconds
  instead of holding a gateway WebSocket, so no dependency is added. Bots read DM
  text without the privileged message-content intent.
- **Email**: give the assistant a mailbox of its own, for example a new Gmail account
  with an app password, and write to it from your usual address:

  ```sh
  MINILOOP_EMAIL_PASSWORD=... go run ./cmd/miniloop-assistant \
    -email-owner you@example.com -email-user assistant@gmail.com \
    -email-imap imap.gmail.com:993 -email-smtp smtp.gmail.com:587 \
    -email-authserv mx.google.com
  ```

  From is easy to forge, so a mail counts as yours only when two things hold. Its
  From is `-email-owner`. Its topmost `Authentication-Results` header names the
  `-email-authserv` server and shows `dmarc=pass` for your domain, or `dkim=pass`
  signed by it. That header is the one the receiving server prepends; headers below
  it may come from the sender. Other mail is refused and counted in the ledger.
  - The channel uses `EXAMINE` and `BODY.PEEK[]`, so nothing in the mailbox is
    marked read, moved or deleted.
  - It reads up to 20 new messages per poll, refuses any over 256 KB, and takes the
    first plain-text part, or the HTML part's text.
  - It drops the quoted original and your signature. A body that is not UTF-8 is
    refused with a note asking for UTF-8.
  - Each assistant line is one mail to you, and replying to one answers its
    question.
  - IMAP uses TLS. SMTP uses TLS on port 465 and requires STARTTLS on any other
    port. Both run in plain text only to a loopback host, such as a local mail
    bridge.

Durability and status:

- **Receiving**: each receive commits your messages, their dedupe links and the
  service cursor in one transaction. A message seen again is skipped by its
  service ID, so a crash neither loses nor repeats one. A channel's first start
  begins at the service's newest message, so older messages are not replayed.
- **Sending**: a line is marked delivered right after the service accepted it. A
  crash in between sends it once more. A failed send stops the round so order holds.
  A line the service refuses for its content (HTTP 400, or SMTP 5xx after `DATA`)
  is skipped and recorded instead of blocking the rest.
- **Status**: the header shows a badge per channel, red while receiving or sending
  fails. Three failures in a row put the channel on **需要你处理**. The ledger's
  `remote` rows record what each channel received and refused, and lines skipped.

## Many owners: the fleet host

The daemon above serves one owner on one Mac. `miniloop-assistant fleet` hosts
many owners' assistants in one process, the way a cloud service would. It is off
by default, and the single-owner daemon does not change.

```sh
cd go
export MINILOOP_FLEET_ADMIN_TOKEN=$(openssl rand -hex 24)   # at least 24 characters
go run ./cmd/miniloop-assistant fleet                        # http://127.0.0.1:8791
curl -s -X POST http://127.0.0.1:8791/admin/tenants \
  -H "Authorization: Bearer $MINILOOP_FLEET_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' -d '{"id":"alice"}'
# {"id":"alice","token":"…","link":"/t/alice/#token=…"}
```

Open the returned link on the fleet's address. The token is shown only once.
The fleet stores its SHA-256 hash, not the token. The token rides in the
fragment, which browsers do not send. The page moves it into the tab's
`sessionStorage`, removes it from the address bar and sends it as a bearer token.
It is the same page as the daemon's, with relative API paths.

Each tenant is an unchanged single-owner assistant on its own directory,
`<data>/tenants/<shard>/<id>/`, with its own `assistant.db`, `agent.db`, memory and
simulated world. Tenants share no tables. `<data>/fleet.db` keeps one row per
tenant: token hash, state, next wake, last visit and model calls and tokens per day.

| Path | Credential | Does |
|---|---|---|
| `GET /t/{id}/` | none | The page. It carries no tenant data |
| `/t/{id}/api/…` | that tenant's token | The daemon's API for that tenant. Opens the assistant if it sleeps |
| `POST /admin/tenants` | admin token | Creates a tenant, `{"id": …}` or `{}` for a random ID, and returns its token once |
| `GET /admin/tenants?after=&limit=` | admin token | Lists tenants by ID, without tokens |
| `GET`, `DELETE /admin/tenants/{id}` | admin token | Shows a tenant with today's model use, or deletes its row, directory and archive |
| `GET /admin/stats` | admin token | Open assistants, turns in use and waiting, wakes, sleeps, evictions, refusals, stalls, archives and model calls |
| `GET /healthz` | none | `ok` |

An unknown tenant and a wrong token both get 401, so IDs cannot be probed. A full
fleet answers 503 with `Retry-After`, and a tenant over 20 requests per second
(bursts of 40) gets 429.

### What it costs

Model tokens cost the most, then compute, then storage. The fleet saves on each:

| Resource | Single-owner daemon | Fleet |
|---|---|---|
| Model calls | Every turn runs as soon as a worker is free | One gate bounds turns across all tenants (`-turns`) and keeps a quarter for owners' chat. Routers and the preparer ask for a slot before they claim work, so waiting work keeps its attempts. Every call is metered per tenant and day. A tenant over `-daily-calls` is held until the next day, its chat gets 429 with the time it reopens, and its owner is told once. Owners away for `-dormant-after` are no longer woken, so routines stop spending on them |
| Compute and memory | Always open: 8 goroutines, the scheduler, poller, router and outbox ticking | Open only while its owner uses it or something is due. It sleeps once it is quiet (no turn running, no work runnable, nothing due before its next timer) and its owner has been gone `-idle`. Before closing, the fleet writes the earliest of its next routine slot, task check, event reminder and approved effect into the registry. A waker opens due tenants. At `-max-active`, an owner's request evicts the least recently used quiet assistant; the waker never evicts and leaves an eighth of the room to owners |
| Storage | One open directory with WAL files | Nothing on disk until the owner's first request. A sleeping tenant's databases are checkpointed, so no WAL files remain. Owners away for `-archive-after` have their directory compressed into one `.tar.gz`, restored on their next request |

Two guards keep a tenant from being lost or holding a slot forever. If its owner has
gone and for 10 minutes it is neither quiet nor running or waiting for a turn, for
example work it can see but never claims, it is closed and not woken for 5
minutes. A tenant left open when the process crashed is due at once on the next
start, so its work continues.

Fresh installs no longer catch up routine slots that passed before the install.
This applies to the daemon too. Without it, every tenant created in the evening
ran a morning brief and the nightly consolidation at once.

### Flags

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `127.0.0.1:8791` | A non-loopback address needs `-tls-cert` and `-tls-key`, or `-behind-tls-proxy` |
| `-data` | `<config dir>/mini-loop/fleet` | `fleet.db` and `tenants/` |
| `-model`, `-model-id` | `scripted` | Shared by every tenant, as for the daemon |
| `-max-active` | 256 | Assistants open at once. Each takes about 13 file descriptors |
| `-idle` | 2m | How long an assistant stays open after its owner's last request |
| `-turns`, `-chat-turns` | 8, a quarter | Model turns at once across all tenants, and how many of them are kept for chat |
| `-tenant-workers` | 2 | Concurrent turns of one tenant |
| `-daily-calls` | 200 | Model calls per tenant per day; 0 is unlimited |
| `-dormant-after` | 168h | Stop waking a tenant whose owner has been away this long; 0 keeps waking |
| `-archive-after` | 720h | Compress the data of a tenant whose owner has been away this long; 0 never |
| `-sweep` | 0 | Wake each sleeping tenant at least this often, for mail sources without push |

### Measured

`MINILOOP_FLEET_LOAD=1000 go test -run TestFleetLoad -v ./assistant` creates 1000
tenants. 32 clients then open each tenant's state, chat once and leave, with
`-max-active 64`, `-idle 500ms` and the scripted model. The resident row keeps
50 single-owner assistants open, as 50 daemons would. Numbers are from an Apple
silicon laptop with `GOMAXPROCS=2`.

| | Resident (per assistant) | Fleet, 1000 tenants |
|---|---|---|
| Idle CPU | 1.0 ms/s each, so about one core per 1000 | 2.9 ms/s for all 1000 asleep |
| Goroutines | 8 each | 4 above the start once all sleep |
| Open assistants | all | at most 64, 0 after the run |
| Disk | 808 KB open, with WAL | 312 KB asleep, 15 KB archived, 0 before the first request |
| Model calls | — | 2 per tenant, both for its one chat turn. Before the fresh-install fix: 9, with a catch-up brief and the nightly routine |
| Latency | — | Waking a sleeping tenant p50 351 ms, p99 950 ms under load. A warm read p50 0.5 ms. Restoring an archived tenant p50 5 ms |

No request got 503. The Go heap is not returned to the system at once: RSS stayed
87 MB above the start after every tenant slept.

## Processes and data

| Process | Owns | Talks to |
|---|---|---|
| `miniloop-assistant` daemon | `assistant.db`: triggers, mail cursor and ingested IDs, subscriptions, tasks, event reminders, proposals, workers' questions, channel (each user line tagged with its source), ledger, pause switch, dismissed attention items, contacts, candidate mode, preparation jobs, task-check snapshots, watchdog flags, profile, memory aliases and vectors, entities and facts, remote channel links and cursors. `agent.db`: transcripts, action journal, leases. `memory/`. `daemon.lock`, held while the daemon runs | The page over loopback HTTP; the child over stdio JSON-RPC; with `-embed ollama`, the Ollama server over HTTP; with remote channels, the Telegram Bot API and Discord's REST API over HTTPS, and the assistant's mail servers over IMAP and SMTP |
| `miniloop-assistant mock-mcp` child | `mock-mcp/world.db`: mailbox, calendar, effect receipts by idempotency key | Only the daemon, through stdin and stdout |
| `miniloop-assistant fleet` (opt-in) | `fleet.db`: tenants (token hash, state, next wake, last visit) and model use per tenant and day. `tenants/<shard>/<id>/`: each tenant's own `assistant.db`, `agent.db`, `memory/` and `mock-mcp/world.db`, or `<id>.tar.gz` while archived. `daemon.lock` in the fleet's root while it runs, so a second fleet or a daemon there is refused, and in each open tenant's directory | Owners over HTTP with their tokens, the operator over the admin API, each open tenant's simulated world in process instead of a child |

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

## Batches and reports

The router claims one thread at a time. With the oldest due trigger it also claims
the thread's other due pending triggers of the same trust class, up to eight, and
the worker handles them in one turn and reports once.

- The user's chat runs with explicit human authority and every other kind
  untrusted, so a batch never mixes chat with other kinds. A routine slot always
  runs alone.
- Triggers the fast path filters out, such as a closed task's check, settle on
  their own.
- Drafts, tasks and questions are stamped with the oldest trigger in the batch, so
  a re-run after a crash converges on the same rows.
- If the turn fails, the whole batch is retried or failed together.

A worker says what its turn means for you with the `report` tool, instead of a
marker in its text:

| Outcome | The router |
|---|---|
| `notify` | Posts the message, at most 400 characters, with any new approval cards |
| `ask` | Posts one question and keeps it under **需要你处理**. Your answer becomes a `user.answer` trigger on the same thread |
| `quiet` | Posts nothing and writes the reason to the ledger. Cards for new drafts are still posted, because they need your decision |

A turn that ends without a report is delivered as `notify` with its final text,
and the ledger notes the missing report. The answer turn runs untrusted like any
other event on that thread: your words are quoted as yours, but the thread's
transcript carries external mail.

## Admission and preparation

The rules grade every mail before a model sees it:

| Mail | Grade | What happens |
|---|---|---|
| Bulk: newsletter category or a bulk sender such as `noreply` | Ignored | Archived, counted in the brief |
| A known contact writes to you | Explicit request | Straight to the thread's worker |
| A known contact copies you, or a stranger writes to you | Inferred candidate | The triage decides |
| A stranger copies you | Ignored | Archived |

Every other trigger kind (chat, routine, invite, task check, reminder, answer,
`job.done`) is explicit. The contact table starts with four demo contacts (the
manager, a colleague, a friend and the cloud vendor). Each approved send counts an
interaction with its recipient, and an unknown recipient becomes a contact.

The triage is one model call without tools. It reads the sender, recipients,
subject and a preview inside `<external>` and answers `handle`, `observe` or
`ignore` with a reason. What happens to `handle` depends on the candidate mode:

- **观察** (observe, the default) records the trigger as `observed`. It is listed
  under **候选**, where **交给 worker** queues it again as admitted, so the router
  skips the triage, and **忽略** settles it.
- **自动** (auto) runs the worker right away.

`observe` and `ignore` settle the trigger without a worker turn in both modes. A
failed or unreadable triage records the trigger, so a model outage turns an unclear
mail into neither work nor silence. The mode is stored in `assistant.db`. The
counters under **候选** (worker turns, merged triggers, rule and triage decisions)
are kept in memory and start from zero after a restart.

A worker can prepare material in the background with `prepare_start`:

- The job is a row in `assistant.db`, deduplicated per trigger and goal, with at
  most five queued or running.
- The Preparer runs one job at a time in a fresh session whose catalogue holds only
  `mail_list`, `mail_read`, `calendar_list` and `task_list`. The session runs
  untrusted for at most two minutes.
- The result returns to the starting thread as a `job.done` trigger, so that
  thread's worker decides what you hear. **停止** on the page, or `prepare_stop`
  from a worker on the same thread, cancels the job. A cancelled job wakes nobody.
- Jobs left running by a crash are queued again on start, and the Preparer claims
  nothing while processing is paused.

The watchdog judges whether a task's checks make progress. After every check the
router snapshots the task's check and note. On each scheduler tick the agent's stuck
detector reads the last snapshots. Three in a row with the same check and note, or
six alternating between two states, flag the task once: a ledger row, a channel
line and **可能卡住** under **需要你处理**. A later check that changes the note
clears the flag; a new streak raises it again, even after **知道了**.

`TestSyntheticEventStreamAcceptance` sends 300 events through the daemon with the
offline model, using personal mail, bursts, invites, bulk, suspicious, cc'd and
stranger mail, bills and chat. It reports latency, router decisions and the quiet
rate: the share of events that reached you neither as a message nor as an attention
item. Set `MINILOOP_ASSISTANT_STREAM_REPORT=<path>` to keep the JSON. One run on the
development Mac took 112 worker turns for 300 events, with a quiet rate of 0.54,
p50 0.51 s and p95 1.01 s. These numbers come from the scripted model, not a real
one.

## Memory and profile

`lookup` is chain recall over `assistant.db`. Given one ref, it returns a short
summary and the refs one hop away:

| Ref | Summary | Next hops |
|---|---|---|
| `trigger-<id>` | Kind, status, thread, summary, decision | Its thread and worker session; the drafts, tasks, questions and jobs it started; the task, question or job it refers to |
| `task-<id>`, `proposal-<id>`, `question-<id>`, `job-<id>` | The record's own fields | Its trigger, thread and worker session |
| `thread:<thread>` | The thread's newest triggers and its subscribed worker | Those triggers, the worker and what the thread started |
| `session:<worker>` | The triggers the worker handled | Those triggers, their threads and what the worker started |

It only reads, and its summaries may quote external mail. In the channel,
**追溯 trigger-3** (or any ref) asks the worker to look one up.

**夜间整理** is a routine seeded at 23:30, under its own key so older data
directories gain it. Like the brief, a slot missed earlier the same day runs once
as a catch-up. Before the worker turn the daemon:

- adds the recipients of the last 30 sent mails as contacts. Senders are never
  added, so a stranger cannot become a known contact by writing twice;
- reads the confirmed meetings of the next seven days and records the window they
  fall in;
- removes memories whose body repeats another's, ignoring case, spaces and
  punctuation. It keeps an explicit memory first, then a `feedback` memory, and
  rewrites `memory/` only when something repeats;
- records facts (see [Facts](#facts)): from the contact table, the inbox and the
  calendar, and those the model extracts from what you said in the channel;
- rebuilds the profile and writes one ledger row.

The worker turn then sees that summary and the lines you typed in the channel that
day. It remembers the preferences you stated with `remember` and reports quiet.
Every step is idempotent, so a manual run or a re-run after a crash converges.

The profile has four lines:

- manager: a view of the current manager fact and the one before it;
- frequent contacts: a contact marked 经理 who is no longer the manager shows as
  前经理;
- meeting window;
- preferences: the first three `feedback` memories.

Every worker prompt shows it under **用户画像**, marked as reference data rather
than instructions. The daemon rebuilds the lines that need no calendar at start
and after the nightly turn.

### Facts

`facts.go` keeps time-bounded statements about people in three `assistant.db`
tables:

- `entities`: people and organizations;
- `entity_aliases`: lowercase names, addresses and domains. The first entity to
  claim an alias keeps it, so two people with one name share an entity;
- `facts`: subject, predicate, object entity or value, `valid_from`, `valid_to`,
  `superseded_by`, source and evidence.

| Predicate | Values | Comes from |
|---|---|---|
| `manager` (经理) | one at a time, the user's own | the contact marked 经理; what you said |
| `works_at` (公司) | one at a time | a contact's work domain (acme.example is Acme, also known as acme; friends, family and free-mail domains excluded); what you said |
| `role` (职位) | one at a time | what you said |
| `wrote` (来信), `organizes` (会议) | the latest five per person | inbox mail and calendar events of known contacts |

Strangers never become entities, so a mail cannot create or change a fact.

**Extraction.** The nightly run sends the lines you typed since the last run (at
most 40, numbered, with their time) to the model in one call without tools. The
answer must use the fixed format `序号 | 主语 | 谓词 | 宾语` for `manager`,
`works_at` and `role`. Lines in another format, with an unknown predicate, an
out-of-range number or a long name are dropped. A fact starts when you said it,
and its evidence is the channel message. On success a cursor moves past those
lines. On failure the ledger notes "事实提取失败，下次整理再试" and the next night
asks again. The offline scripted model answers from a few fixed phrasings.

**Timeline.** Facts of one subject and predicate form a timeline by start time.
A new fact ends where the next one starts, and the one before it ends where it
starts, so a late report of an old value slots in between. Sources rank: what
you said > the contact table > a domain, the inbox or the calendar. A newer
value from a weaker source does not replace a stronger current fact. A fact
already recorded as current, or with the same start, is not written again, so
re-runs converge.

**Use.**

- `memory_search` lists the facts a question is about before the memories:
  - facts of the people it names, and facts pointing at them;
  - your own facts when it asks for one ("我的经理");
  - only that predicate when it asks for one;
  - current single-valued facts first, then replaced ones, then interactions, at
    most six.
- The 人物 leg ranks memories by how many of the named people (or your current
  manager, for "我老板") they mention. A mail address names only its own entity.
- A memory that states a replaced value, a word for its predicate and its
  subject (unless that is you) carries
  `outdated="我的经理自 2026-11-02 起是 Carol，不再是 Alice"`.
- Each turn's prompt carries up to four facts about its events under
  **相关的人物事实**. The worker rules say a current fact wins over a memory that
  disagrees.
- The page lists every fact under **人物与事实**, striking through replaced ones
  and naming what replaced them. The search box shows the facts above the hits,
  and a 可能过期 chip on outdated memories.

### Memory search

Workers search memory with `memory_search`. The router creates every session with
the assistant's tools plus `remember` and `memory_search`; the built-in `recall` is
no longer offered, because its substring search misses a question asked in other
words. A session restored from an earlier version keeps its old catalogue.

`MemorySearch` builds its lexical index per query from the owner's memories and
ranks them with four legs, plus a fifth with `-embed ollama`:

| Leg | What it matches |
|---|---|
| 关键词 | BM25 over name, description and body. Runs of Han, kana or Hangul are split into overlapping two-character tokens, other letters and digits into lowercase words; single letters and a few English function words are dropped. |
| 别名 | BM25 over the memory's search aliases, when they were written for its current text. |
| 子串 | The existing `memory.Store.Search`: the query split at non-letters, each term counted as a substring. |
| 人物 | Memories naming the people the question names, or your current manager when it asks for one; see [Facts](#facts). |
| 语义 | Only with `-embed ollama`: cosine similarity between the query's embedding and each memory's, at least 0.35, top 10. |

Reciprocal rank fusion (k=60) merges the rankings; on a tie, more legs win,
then the better single rank. Each hit names the legs that found it. The Markdown
memory files stay the authority; nothing is written back to them.

**Aliases** come from the nightly consolidation. After removing duplicates, it asks
the model once, without tools, for three to eight synonyms, broader terms and
other-language wordings of each memory that has none for its current text (at most
12 a night). `memory_aliases` in `assistant.db` keys them by memory name and a hash
of the text, so an edited memory stops using its old aliases and a removed memory
loses its row. A failed request is noted in the ledger and retried the next night;
search works without aliases. The offline scripted model answers from a small
synonym table.

**Prefetch**: before each turn the router searches with what the turn's events are
about (a mail's sender and subject, the user's words, an event's title and
organizer, a task's title) and puts up to three hits, at most 600 characters, into
the prompt under **可能相关的记忆**, marked as reference data. Only hits that the
keyword, alias or 人物 leg found, or whose similarity is at least 0.45, qualify; a
substring match alone does not, because "is" in an unrelated question matched
"English". Routines prefetch nothing.

**Embeddings** (`embed.go`): `OllamaEmbedder` posts to Ollama's `/api/embed` with the
standard library. Memories are sent 16 per call as name, description and body, cut
to 1000 characters; queries carry the retrieval instruction Qwen3 embedding models
expect. `VectorIndex` keeps the vectors in memory and in `memory_vectors`
(`assistant.db`), keyed by memory name, a hash of its text, and the model:

- before each semantic search, new or changed memories are embedded and removed
  ones dropped;
- the first load deletes vectors of another model;
- at start the daemon syncs once and probes the server, so the status is true after
  a restart even when every vector was stored.

When a call fails, the semantic leg is skipped and the other three answer. The
health board counts the failure, and three in a row list **语义检索（本地嵌入模型）**
under 需要你处理 until a call succeeds.

On the page, **画像与记忆** names the retrieval mode and, with embeddings, the index
count and the last contact with Ollama. It lists each memory's aliases and has a
search box (`GET /api/memory/search?q=`) that shows the legs of each hit and the
similarity of semantic hits.

`TestMemoryRetrievalEvaluation` scores each lexical configuration on a fixed set in
`testdata/recall_eval.json`: 24 memories, 44 queries in five categories and eight
questions with no answer, counting hits in the top 3 and MRR@3.

| Configuration | Topic (8) | Keywords (4) | Questions (10) | Paraphrases (10) | Cross-language (12) | Total | MRR@3 |
|---|---|---|---|---|---|---|---|
| Substring (old `recall`) | 8 | 4 | 0 | 0 | 0 | 12 | 0.273 |
| BM25 | 8 | 4 | 10 | 0 | 0 | 22 | 0.500 |
| BM25 + substring | 8 | 4 | 10 | 0 | 0 | 22 | 0.500 |
| All three legs | 8 | 4 | 10 | 8 | 9 | 39 | 0.852 |

The bigrams find every unspaced question; the aliases find most paraphrases and
cross-language queries. The aliases in the set were written by the author of the
queries, so that figure is optimistic; a real model's aliases will differ. The five
misses (不能吃什么, 网断了打给谁, teeth cleaning, peanuts, 牙科诊所) share no word with
their memory. Set `MINILOOP_ASSISTANT_RECALL_REPORT` to a path to write the scores as
JSON.

`TestMemoryRetrievalWithOllama` measures the semantic leg with a real model. It runs
only when `MINILOOP_ASSISTANT_OLLAMA_URL` is set (`MINILOOP_ASSISTANT_OLLAMA_MODEL`
overrides the model, `MINILOOP_ASSISTANT_OLLAMA_REPORT` writes JSON). It sweeps
three floors. With `qwen3-embedding:0.6b`:

| Floor | Semantic only | Keyword + semantic | Four legs, no aliases | Four legs | Negatives with hits | Negatives prefetched |
|---|---|---|---|---|---|---|
| 0.35 (default) | 43 (MRR 0.955) | 43 | 43 | 44 (MRR 0.977) | 6/8 | 1/8 |
| 0.40 | 40 | 40 | 40 | 44 | 3/8 | 1/8 |
| 0.45 | 37 | 38 | 38 | 42 | 2/8 | 1/8 |

At 0.35 the semantic leg alone misses only "out of office", which the aliases find.
The prefetched negative is 家里的猫叫什么, whose bigram 家里 matches the school-pickup
memory. Both floors were chosen on this set, which has no separate validation split
and only eight negatives, so the figures are optimistic. The regular tests use a
fake embedder that maps words to concepts.

`TestFactsEvaluation` measures time-sensitive questions on
`testdata/facts_eval.json`: four contacts, eight things said over two months (each
also remembered) and eleven questions whose current value is known. With the
offline model's extraction:

| Answer taken from | Current value | Replaced value |
|---|---|---|
| The top memory (keyword, alias and substring legs) | 5 | 5 |
| The first fact `memory_search` lists | 11 | 0 |

All eight replaced-value memories in the top 3 carry an `outdated` note. One
question has no answer, and no fact is invented for it. The chat lines use the
phrasings the extraction rules cover, so the fact figure is optimistic; a real
model's extraction is unmeasured. Set `MINILOOP_ASSISTANT_FACTS_REPORT` to write
the scores as JSON. The lexical evaluation above is unchanged: its store holds no
facts.

## How the six parts map to code

| Part | In this demo | Code |
|---|---|---|
| Own computer | One long-running daemon on your Mac, optionally under launchd. Durable state is described in [Processes and data](#processes-and-data). The opt-in fleet host runs many owners' assistants in one process and keeps each asleep while it has nothing to do. | `app.go`, `store.go`, `standby.go`, `fleet*.go`, `cmd/miniloop-assistant` |
| Integrations | Mail and calendar exist only behind the simulated MCP server. Worker tools are typed wrappers with declared risk: `mail_list`, `mail_read`, `calendar_list`, `task_list` (read); `propose_email`, `propose_invite_response` (write: draft only); `task_create`, `task_schedule`, `task_complete` (write: local task board); `report` (write: the turn's result, acted on by the router); `prepare_start`, `prepare_stop` (write: local preparation queue); `lookup`, `memory_search` (read: local records, owner memory). The custom catalogue replaces the Bash and file tools. | `world.go`, `mockmcp.go`, `connector.go`, `tools.go` |
| Memory | The existing `remember` tool on an owner-scoped `memory.Store`; workers search it with `memory_search` (BM25 with Chinese bigrams, nightly aliases, substring, people named in the question and, with `-embed ollama`, local embeddings, fused by rank), and each prompt carries the facts and memories related to its events. Time-bounded facts about people come from the contact table, the inbox, the calendar and a nightly extraction from your chat. Facts are seeded on first start. `lookup` follows a piece of work through `assistant.db`. The nightly routine consolidates contacts, the meeting window, duplicate memories and search aliases into a profile shown in every worker prompt. | `seed.go`, `retrieval.go`, `aliases.go`, `embed.go`, `facts.go`, `lookup.go`, `nightly.go`, `profile.go` |
| Routines and triggers | The poller turns new mail into trigger rows, and channel chat becomes trigger rows too, whether typed on the page or sent from a remote channel. Rules form the fast path and grade mail by contact and address; the triage decides inferred candidates. A Preparer runs background preparation jobs, and the watchdog checks task progress on each scheduler tick. Thread subscriptions route follow-ups to the same worker. The scheduler claims the daily brief's slots, due task checks and upcoming-event reminders, each exactly once. Different threads run on parallel workers (`-workers`, default 4, plus one slot for your chat); one thread runs in order, and what queued up on it meanwhile runs as one batch. The pause switch holds all of it except mail intake and remote channel intake. | `poller.go`, `rules.go`, `contacts.go`, `triage.go`, `router.go`, `jobs.go`, `routines.go`, `tasks.go`, `watchdog.go`, `control.go`, `remote.go`, `telegram.go`, `discord.go`, `email.go`, `imap.go` |
| Approval gates | Workers can only append pending proposals. A human decision moves a proposal from `pending` to `approved` or `denied`. The outbox alone executes approved proposals through MCP and moves them to `sent` or `failed`. | `store.go: DecideProposal`, `outbox.go`, `server.go` |
| Audit log | Append-only ledger rows. The chain is: trigger (poller, channel or scheduler) → rule → route → gated tool call (`action_id`) → proposal → decision (actor) → effect (MCP tool, receipt, key, replay) or failed attempt. Task rows record creation, rescheduling, checks brought forward (with the reason), completion, cancellation, hand-over and resumption. Control rows record pause, resume, retries, dismissals, answers, candidate decisions and mode switches. Report rows record each worker's report and its final text. Triage rows record each verdict with its reason and grade; job rows record queueing, completion, failure and cancellation; watchdog rows record each flag. Remote rows record what remote channels received and refused, and lines a service refused. | `store.go`, `tasks.go`, `control.go`, `router.go: OnResult`, `remote.go` |

## Boundaries

- The MCP server and its data are simulated. There is no real mailbox or calendar
  for the assistant to manage, no OAuth and no push notification. The connector
  speaks only the stdio transport that `go/mcp` already has. The email remote
  channel is a conversation with you, not the managed mailbox.
- A remote channel message from your account has the same authority as typing on
  the page. Someone who controls your Telegram or Discord account, or can send mail
  that your `-email-authserv` server marks DKIM- or DMARC-aligned with your domain,
  can chat as you, answer questions and cancel tasks. They cannot approve: decisions
  are only on the loopback page. The email check trusts the topmost
  `Authentication-Results` header, so `-email-authserv` must name a server that adds
  one to every incoming mail.
- Remote channels send text off the machine. Your messages and the assistant's
  lines, which can quote mail subjects, names and your schedule, pass through
  Telegram, Discord or your mail provider. Tokens and the mailbox password come
  only from the environment, are withheld from the MCP child, and are never put in
  a plist. The Telegram token sits in the request path and is replaced by
  `<token>` in every error.
- Remote channels poll. There are no webhooks, because the daemon listens only on
  loopback, and no Discord gateway. A line reaches a channel at least once: a crash
  after the service accepted it, or partway through a line split into several
  messages, sends it again. Attachments, images, voice and group chats are refused
  or ignored.
- The daemon does not register MCP tools in the agent catalogue. It declares risk on
  its own typed wrappers and keeps `mail_poll` and the effect tools out of model
  reach. The core rule that pins registered MCP tools to `external` is unchanged.
- Trigger runs use untrusted authority. Chat typed on the page uses explicit-human
  authority, because the page is loopback-only.
- The rule fast path is deterministic code. The triage is a model call only for
  inferred candidates; explicit requests never wait for it. The offline scripted
  model answers the triage with keyword rules, so its verdicts only demonstrate the
  path. A worker still reports `quiet` to stay off the channel.
- Background preparation uses durable rows and one daemon goroutine, not the
  `go/background` shell jobs, because workers have no shell. Jobs use the worker
  model and run one at a time. Their sessions see only read tools and cannot draft,
  so preparation never reaches an approval card on its own.
- The watchdog compares only what checks recorded (the check and the note); it does
  not inspect the mailbox. It flags and suggests; it never cancels a task. The
  fallback check and the six-check budget still keep a task from being forgotten or
  waking forever.
- Contacts are local demo data. A known contact still runs untrusted, because the
  mail content is external.
- Memory search is lexical with model-written aliases unless `-embed ollama` adds
  local embeddings. With it, memory text and queries go to `-embed-url`; the default
  is loopback, and another host receives the owner's memory over the network. The
  prefetch query can come from an external mail's sender and subject, so mail can
  influence which memories a worker sees, and with embeddings that text reaches the
  embedding server; it cannot make the worker send. See
  [Memory search](#memory-search).
- Facts come only from the contact table, known contacts' mail and meetings, and
  what you typed in the channel. A stranger cannot create an entity or change a
  fact, and the extraction never reads memories a worker wrote. The fact request
  carries your recent chat to the configured model only. Same-name people share an
  entity; there is no disambiguation. The nightly
  turn runs untrusted like any routine and is asked to remember only what you said
  in the channel. A memory a worker wrote from mail can still reach the profile's
  preference line, which is why the profile is marked as reference data.
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
- The fleet host is one process on one machine. Multi-node sharding, replication
  of tenant directories to object storage, self-service sign-up and provider
  webhooks are design only. Every tenant uses the fleet's time zone and model, and
  none has remote channels or embeddings. Mail and calendar stay the simulated
  world, one per tenant.
- A fleet tenant's token has the authority of typing on that tenant's page,
  including approval. Anyone holding the link holds the token until the admin
  deletes the tenant; there is no rotation yet. Admin and tenant tokens come only
  from the environment and the admin API. The fleet does not rate-limit
  unauthenticated requests or keep a request's limit across sleep, so put it
  behind a proxy that limits by address.
- A sleeping tenant wakes only for timers it recorded. New mail in a real mailbox
  would need `-sweep` or a push from the provider. The page still polls the whole
  state every 1.5 s while visible; an ETag or pushed changes would cut that.
- The quota counts model calls, not tokens. A held tenant still accepts reads and
  approvals, and its queued work runs the next day.
