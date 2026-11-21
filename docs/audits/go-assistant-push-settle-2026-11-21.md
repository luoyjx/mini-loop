# Go personal-assistant demo D12 — push, long poll and turn settlement (2026-11-21)

Baseline: `0bdd2a8` (D11 `bb03f22`, its architecture view `ff3b4bd` and the
research reindex `0bdd2a8`).

Request this round implements (2026-11-21): "对比一下现在的架构跟我发的这张图有多大的区别，
或者是有多大的差距，看还有什么缺口补齐一下", about the personal agent ("我说的是personal
agent 架构"). In English: compare the personal-assistant architecture with the
attached Grok Bot server diagram, measure the gap and fill what is missing.

Midway the user added a second diagram, DeepSeek's SuperAgent prototype, with a
suggestion to look first at Grok's split of orchestration from compute and at
DeepSeek's transactions and recovery.

Earlier constraints still hold:

- secrets are read from the environment;
- no new dependency without approval;
- no LaunchAgent install and no push without confirmation.

Both diagrams are concept drawings of server roles, not code. The comparison is
by role.

Scope:

- `go/assistant`:
  - new `changes.go` (store version, change feed, state tag, long poll) and
    `settle.go` (one-transaction turn settlement), with `changes_test.go` and
    `settle_test.go`;
  - `Fleet.Push`, `registry.nudge` and a waker kick in `fleet.go` and
    `fleet_registry.go`;
  - `POST /push/{tenant}` and a shared token check in `fleet_http.go`;
  - change signals in `store.go`, the halt channel in `app.go`, ETag handling
    in `server.go`, the router's settlement in `router.go`, `askQuestion` in
    `report.go`, and the long-poll loop in `web/index.html`.
- `go/cmd/miniloop-assistant`: `MINILOOP_FLEET_PUSH_TOKEN`, with tests.
- The roadmap, the README baseline, Mermaid and boundary text, the system
  architecture spec and its HTML, the layered base image, the Go README, the
  demo README, and the parity matrix and plan entries.

## Comparison

### Grok Bot: event driven, durable execution, orchestration apart from compute

| Grok part | Here before D12 | Gap |
|---|---|---|
| Channels with ingress dedupe | Page, remote bridges and mail poller; ingested mail IDs and remote links | None |
| Webhook / event ingress | The daemon polls mail. A sleeping fleet tenant woke only for its own timers | **Filled**: `POST /push/{id}` |
| Session queue per conversation | Trigger table: one thread in order, threads in parallel, chat first with a reserved slot; the fleet's turn gate keeps chat slots | None |
| Durable execution of each step | Durable rows and idempotent effects. A crash re-runs the whole turn | Step replay stays design only |
| Helpers / sub-agents | The Preparer: durable read-only jobs, one at a time, results back to the thread | Parallel helpers stay design only |
| Tool compute in a per-user sandbox, apart from orchestration | Effects only through the MCP connector, a child process for the daemon. Workers have no shell. The fleet runs each tenant's simulated world in process | Sandbox per user stays design only |
| Shared model service with metering | One provider; the fleet meters calls per tenant and day | None |
| Client subscriptions to changes | The page fetched the whole state every 1.5 s | **Filled**: long poll with `ETag` |
| Connectors (OAuth) | Simulated MCP mail and calendar | Design only |
| Many nodes, sharding | One process | Design only |

### DeepSeek SuperAgent: state machine, transactional enqueue, recovery

| DeepSeek part | Here before D12 | Gap |
|---|---|---|
| Source adapters | Poller, page chat, remote bridges | None |
| Engine: a state change and its enqueued work in one transaction | Mail and cursor, slot and trigger, decision and outbox, question and its lines were each one transaction. A turn's end was not: lines, each trigger's status, task-check snapshots and ledger rows were separate writes | **Filled**: `Store.Settle` |
| Trigger / Thread, Ticket / Assignment, Effects / Outbox tables | Triggers and subscriptions, tasks, proposals | None |
| Effects executor: dedupe key, concurrency limit, backoff | The outbox: key `proposal-<id>`, one at a time, growing backoff, refusals fail at once | None |
| Recovery: retry pending effects | Approved proposals resume after restart | None |
| Recovery: resume interrupted assignments | Running triggers are requeued; the turn re-runs on the thread's restored session | Re-run, not resume; see above |
| Recovery: reuse session and workdir | The thread's worker session is restored from `agent.db`. Workers have no workdir by design | None |
| Router as a model session that judges and coordinates | Rules plus a tool-less triage call; the router is code | Different by design |
| Harness session log | `agent.db` transcripts and the ledger | None |
| SourceRegistry: reply to the source channel | Lines go to the page and every enabled remote channel; owner lines carry their source | One owner, one conversation, by design |

The one real transactional gap was the turn's end. In `Router.deliver` the reply
was committed by `PostAll`, then each trigger by its own `FinishTrigger`, whose
error was only logged. A task's check snapshot was written before both. A crash,
or a failed update, between these writes left the reply posted and the trigger
`running`. The restart's `RequeueInterrupted` then ran the turn again: model
calls repeated (expected), the reply was posted a second time and reached every
remote channel again, and the task got a second snapshot for the same trigger,
which the watchdog can read as a repeated check.

## Design

### Push

- A push names one tenant and carries nothing else. The handler does not read
  the body, so the hint cannot inject mail. The poller's cursor and ingested IDs
  decide what is new.
- Outcomes:
  - open tenant: `PollOnce` while holding a ref, then wake the scheduler →
    `polled`;
  - sleeping tenant: `registry.nudge` sets `next_wake` to now unless it is
    already due, then kicks the waker → `scheduled`. Repeated pushes coalesce;
  - closing tenant: `pushed` is set and hibernation nudges after the row is
    written → `scheduled`;
  - opening tenant: wait for ready, then retry;
  - never opened or dormant: `deferred`; unknown: 404.
- The token is optional. Without it `/push/` is not served. It must be 24
  characters or more and differ from the admin token. The fleet refuses to start
  otherwise.

### Long poll

- The state tag is `"<epoch>-<store version>-<world counter>-<fnv64 of in-memory
  status>"`:
  - `Store.Version` reads `total_changes()` on the pinned connection. A reset
    counter is folded in so the version never goes back. Read-only
    transactions, such as an empty `ClaimBatch`, do not change it;
  - the world counter covers simulator writes to the external mailbox and
    calendar, which the store does not see;
  - the hash covers the router counters, remote status, health problems, the
    poll error and the vector status without its check time.
- Every committed write signals a `changeFeed` by closing a channel. A held poll
  subscribes before it recomputes the tag, so no signal is missed. It also
  rechecks every 2 seconds for changes kept in memory.
- The wait is capped at 25 seconds and ends on the client's context or
  `App.halt`, which closes when the loops stop. The daemon cancels its loops
  before `Shutdown`, and a fleet tenant's hibernation waits for its loops, so
  held polls are answered before either waits for requests.
- The page runs one loop while visible, aborts on hide, and backs off on errors
  up to 30 seconds.

### Settlement

`Store.Settle(Settlement)` runs one transaction that:

1. appends the turn's ledger rows;
2. asks the question with its lines, idempotent per lead trigger, or posts the
   lines;
3. snapshots each checked task once per trigger;
4. ends every trigger of the batch only if it is still `running`. Otherwise it
   returns `errSettled`, and nothing is written.

The router uses it for notify, ask and quiet ends, for triggers that the fast
path or a state check skips, and for observed candidates. `FailBatch` does the
same for a batch that failed for good. A failed commit goes through the normal
retry with backoff.

## What changed

- `changes.go`, `store.go`, `app.go`, `server.go`, `web/index.html`: the long
  poll.
- `fleet.go`, `fleet_registry.go`, `fleet_http.go`,
  `cmd/miniloop-assistant/fleet.go`: the push.
- `settle.go`, `router.go`, `report.go`: the settlement. `finishBatch` and
  `Router.finish` are gone. `Store.FinishTrigger` and `RecordTaskCheck` remain
  for tests.
- Tests: `changes_test.go`, `settle_test.go`, the fleet and CLI tests.

## Evidence

- `TestStateLongPollAnswersWhenSomethingChanges`:
  - the current tag gets 304 with an empty body, and a stale tag gets 200;
  - `wait=1` answers 304 after at least 900 ms;
  - a held poll is answered within the recheck after a chat, with the new state;
  - a simulator event changes the tag.
- `TestFleetHeldPollReturnsWhenTheTenantCloses`: a held poll keeps the tenant
  through a reap, and `Close` answers it at once.
- `TestFleetPushWakesASleepingTenantForNewMail`:
  - empty, wrong and admin tokens get 401, and an unknown tenant 404;
  - a never-opened tenant is `deferred`;
  - a sleeping tenant pushed three times has its wake moved to now. The waker
    opens it, and the pushed mail becomes exactly one trigger;
  - a push to the open tenant is `polled`, and one during closing is
    `scheduled` and lands after hibernation;
  - after 8 days away the tenant is `deferred`, and the stats count 8 pushes.
- `TestFleetRefusesUnsafeStartup` adds a short push token and one equal to the
  admin token. `TestFleetServesTenantsUntilCancelled` checks that an owner's
  token cannot push and that the push token gets `polled`.
- `TestTurnOutcomeCommitsAsOne` adds a SQLite trigger that aborts the update of
  a trigger to `done`:
  - the chat turn's settlement fails as a whole: no reply, no report or channel
    ledger row, and the trigger is pending with the fault in its decision;
  - after the fault is dropped, the retry ends `done` on attempt 2 with exactly
    one reply;
  - settling the finished batch again returns `errSettled` and posts nothing.

  With the previous `router.go` and the same test the reply was posted, the
  finish error was only logged, and the trigger stayed `running`. The test
  failed after its 20 s wait. A restart would have run the turn and posted again.
- `MINILOOP_STATE_COST=1 go test -run TestStatePollCost -v ./assistant`, with six
  mails and four chats and 300 rounds each:

  | | Server time | Bytes |
  |---|---|---|
  | Full state build, the old 1.5 s poll | 664 µs | 30,727 |
  | Unchanged check, the new long poll | 6 µs | 0 (304) |

  An idle visible page for one minute: 40 full builds (26.6 ms) before; 2–3
  requests and about 32 checks (0.19 ms) now.

## Gates

Run with `GOMAXPROCS=2`, `GOFLAGS=-p=1`, `-parallel=2`, `nice -n 10`,
`GOMEMLIMIT=1536MiB`, one heavy run at a time:

- `gofmt -l` (0 files) and `go vet ./...` from `go/`;
- `go test ./...`: 53 packages ok, 2 without tests;
- `go test -count=3 ./assistant ./cmd/miniloop-assistant`: ok;
- `go test -race -count=2` on the same packages: ok, twice. The first run
  was before the settlement change and the second after it. Only the known
  Darwin `ld` LC_DYSYMTAB warning appeared. Process-tree RSS was not sampled
  this round. Host memory was 47% free when checked;
- `.venv/bin/python -m pytest -q -p no:xdist`: 2256 passed, 28 skipped;
- `verify_scans`: all 19 scanning guards anchored;
- `export_go_contracts.py --check`: all 268 files current;
- Archify validate (showcase) on the system spec: 9 of 9 checks, no errors or
  warnings. Two renders of the spec produced the same bytes.
  - The first render left the `paChannel` tag 3 px outside its box, so the tag
    was shortened. The final spec is `01ac9749…` (219,067 B) and the HTML is
    `5c7fcfc0…` (964,986 B).
  - In a browser, no personal-assistant node's text leaves its box;
- the personal-assistant view: see the follow-up commit;
- `git diff --check`: clean.

Not run:

- `go test -race ./...` over the whole module: only `assistant` and
  `cmd/miniloop-assistant` changed;
- `research-site` `npm test`: Node 22.12 is below the required 22.13.
  `npm run content` and `content:check` run in a separate commit;
- `verify_invariants` and `verify_guards`: no Python module or guarded anchor
  changed;
- a Mermaid renderer: a script checked only that every node ID and edge in the
  map is defined (122 nodes, none missing);
- a real model, a real mail provider's notifications, or a deployment behind
  TLS.

## Architecture map

The README Mermaid gains:

- a `PAPush` node, the provider notification relay, labelled "not connected yet";
- three edges:
  - `PAPush` to the fleet, dashed, a wake hint only;
  - the fleet to `PAPoller`: poll an open tenant now or wake a sleeping one;
  - `PARouter` to `PAStore`: one settlement per turn.

Labels change in three places:

- `PAChannel` says the page uses a long poll with `ETag`;
- `PAFleet` lists `POST /push/{id}`;
- `PARouter` names the settlement.

The store-to-page edge says a held poll is answered on change.

The system architecture JSON keeps its layout. Changes:

- `paChannel`, `paFleet` and `paRouter` get new tags;
- the subtitle and the personal-assistant card get the push, the long poll,
  the settlement and the D12 baseline.

The base layered image was edited once with imagegen. That edit is both the new
base and this iteration's highlight. Only the bottom Go assistant band changed:

- box 1's line names the long poll and push wake;
- box 2's line names the one-transaction turn result;
- both boxes carry the orange glow;
- the strip names three rules: a push is only a hint, no full state without a
  change, one transaction per turn result;
- the badge says `2026-11-21：推送唤醒 · 长轮询 · 一个事务`.

The exact prompt is in `go-assistant-push-settle-2026-11-21.prompt.txt` and
`docs/mini-loop-layered-architecture.prompt.txt`.

The personal-assistant view `docs/architecture/personal-assistant.architecture.json`
pins a revision that must contain `changes.go` and `settle.go`, so it moves in a
follow-up commit.

## Deviations from the plan

- The roadmap's next phase was P1, hardening the single-Mac daemon. This round
  answers the user's comparison instead, and P1 moves back.
- The page subscribes with a long poll, not server-sent events or WebSockets. A
  long poll needs no new dependency, passes through any proxy, and reuses the
  ETag on reconnect.
- The push is a wake hint, not a delivery of mail. No provider is wired to it,
  and the daemon, which listens only on loopback, has no push at all.
- Step-level replay was not built. With effects already idempotent and the
  settlement atomic, a crash costs repeated model calls and nothing visible to
  the owner.

## Boundaries

- The push token lets its holder spend wakes and `mail_poll` calls. Give it to the
  relay only. There is no rotation.
- A visible fleet page holds its tenant open. One held poll uses one connection
  and one goroutine. In-memory changes reach the page within 2 seconds.
- A crash still repeats a turn's model calls. Proposal keys and card checks keep
  drafts and cards single, and the outbox keys keep sends single.
- The settlement covers the router's writes at a turn's end. Tools still commit
  their writes as they happen, as before. Drafts and tasks are stamped with the
  lead trigger, so a re-run converges on them. A memory written twice is left
  for the nightly dedupe.
- Sandboxes per user, parallel preparation, OAuth connectors and several nodes
  remain design only.
