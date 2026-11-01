# Proactive personal-assistant demo (Go addition)

`go/assistant` and `go/cmd/miniloop-assistant` form an opt-in demo. They show how the
Go runtime can grow into a proactive personal agent that runs on your own Mac. The
channel is a simulated local web page. Mail and calendar come from a simulated MCP
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
profile that every worker sees.
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

## Processes and data

| Process | Owns | Talks to |
|---|---|---|
| `miniloop-assistant` daemon | `assistant.db`: triggers, mail cursor and ingested IDs, subscriptions, tasks, event reminders, proposals, workers' questions, channel, ledger, pause switch, dismissed attention items, contacts, candidate mode, preparation jobs, task-check snapshots, watchdog flags, profile. `agent.db`: transcripts, action journal, leases. `memory/`. `daemon.lock`, held while the daemon runs | The page over loopback HTTP; the child over stdio JSON-RPC |
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
- rebuilds the profile and writes one ledger row.

The worker turn then sees that summary and the lines you typed in the channel that
day. It remembers the preferences you stated with `remember` and reports quiet.
Every step is idempotent, so a manual run or a re-run after a crash converges.

The profile has four lines: manager, frequent contacts, meeting window and
preferences (the first three `feedback` memories). Every worker prompt shows it
under **用户画像**, marked as reference data rather than instructions. The daemon
rebuilds the lines that need no calendar at start and after the nightly turn.

### Memory search

Workers search memory with `memory_search`. The router creates every session with
the assistant's tools plus `remember` and `memory_search`; the built-in `recall` is
no longer offered, because its substring search misses a question asked in other
words. A session restored from an earlier version keeps its old catalogue.

`MemorySearch` builds its index per query from the owner's memories and ranks them
with three legs:

| Leg | What it matches |
|---|---|
| 关键词 | BM25 over name, description and body. Runs of Han, kana or Hangul are split into overlapping two-character tokens, other letters and digits into lowercase words; single letters and a few English function words are dropped. |
| 别名 | BM25 over the memory's search aliases, when they were written for its current text. |
| 子串 | The existing `memory.Store.Search`: the query split at non-letters, each term counted as a substring. |

Reciprocal rank fusion (k=60) merges the three rankings; on a tie, more legs win,
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
the prompt under **可能相关的记忆**, marked as reference data. Routines prefetch
nothing.

On the page, **画像与记忆** names the retrieval mode, lists each memory's aliases
and has a search box (`GET /api/memory/search?q=`) that shows the legs of each hit.

`TestMemoryRetrievalEvaluation` scores each configuration on a fixed set in
`testdata/recall_eval.json`: 24 memories and 44 queries in five categories, counting
hits in the top 3 and MRR@3.

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
their memory. Local embeddings are the planned next step. Set
`MINILOOP_ASSISTANT_RECALL_REPORT` to a path to write the scores as JSON.

## How the six parts map to code

| Part | In this demo | Code |
|---|---|---|
| Own computer | One long-running daemon on your Mac, optionally under launchd. Durable state is described in [Processes and data](#processes-and-data). | `app.go`, `store.go`, `cmd/miniloop-assistant` |
| Integrations | Mail and calendar exist only behind the simulated MCP server. Worker tools are typed wrappers with declared risk: `mail_list`, `mail_read`, `calendar_list`, `task_list` (read); `propose_email`, `propose_invite_response` (write: draft only); `task_create`, `task_schedule`, `task_complete` (write: local task board); `report` (write: the turn's result, acted on by the router); `prepare_start`, `prepare_stop` (write: local preparation queue); `lookup`, `memory_search` (read: local records, owner memory). The custom catalogue replaces the Bash and file tools. | `world.go`, `mockmcp.go`, `connector.go`, `tools.go` |
| Memory | The existing `remember` tool on an owner-scoped `memory.Store`; workers search it with `memory_search` (BM25 with Chinese bigrams, nightly aliases and substring, fused by rank), and each prompt carries the memories related to its events. Facts are seeded on first start. `lookup` follows a piece of work through `assistant.db`. The nightly routine consolidates contacts, the meeting window, duplicate memories and search aliases into a profile shown in every worker prompt. | `seed.go`, `retrieval.go`, `aliases.go`, `lookup.go`, `nightly.go`, `profile.go` |
| Routines and triggers | The poller turns new mail into trigger rows, and channel chat becomes trigger rows too. Rules form the fast path and grade mail by contact and address; the triage decides inferred candidates. A Preparer runs background preparation jobs, and the watchdog checks task progress on each scheduler tick. Thread subscriptions route follow-ups to the same worker. The scheduler claims the daily brief's slots, due task checks and upcoming-event reminders, each exactly once. Different threads run on parallel workers (`-workers`, default 4, plus one slot for your chat); one thread runs in order, and what queued up on it meanwhile runs as one batch. The pause switch holds all of it except mail intake. | `poller.go`, `rules.go`, `contacts.go`, `triage.go`, `router.go`, `jobs.go`, `routines.go`, `tasks.go`, `watchdog.go`, `control.go` |
| Approval gates | Workers can only append pending proposals. A human decision moves a proposal from `pending` to `approved` or `denied`. The outbox alone executes approved proposals through MCP and moves them to `sent` or `failed`. | `store.go: DecideProposal`, `outbox.go`, `server.go` |
| Audit log | Append-only ledger rows. The chain is: trigger (poller, channel or scheduler) → rule → route → gated tool call (`action_id`) → proposal → decision (actor) → effect (MCP tool, receipt, key, replay) or failed attempt. Task rows record creation, rescheduling, checks brought forward (with the reason), completion, cancellation, hand-over and resumption. Control rows record pause, resume, retries, dismissals, answers, candidate decisions and mode switches. Report rows record each worker's report and its final text. Triage rows record each verdict with its reason and grade; job rows record queueing, completion, failure and cancellation; watchdog rows record each flag. | `store.go`, `tasks.go`, `control.go`, `router.go: OnResult` |

## Boundaries

- The MCP server and its data are simulated. There is no real mailbox or calendar,
  no OAuth and no push notification. The connector speaks only the stdio transport
  that `go/mcp` already has.
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
- Memory search is lexical with model-written aliases; there are no embeddings yet.
  The prefetch query can come from an external mail's sender and subject, so mail can
  influence which memories a worker sees; it cannot make the worker send. See
  [Memory search](#memory-search). The nightly
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
