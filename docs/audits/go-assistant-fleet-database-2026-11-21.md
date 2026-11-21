# Go personal-assistant demo D14 — the fleet's state in PostgreSQL (2026-11-21)

Baseline: `36d3ec6` (D13 `5721b3c`, its architecture view `26ad03e` and the
research reindex `36d3ec6`).

Request this round implements (2026-11-21): "我们在生产环境，我们的 agent server
服务其实是部署在服务器上的，那么数据应该不能存在本地，肯定是得有一个数据库的。然后你可以考虑一下，通过什么数据库去存储更合适，然后以及现在的任务它是不是需要一些任务管理的机制，比如说我们
agent 的 server 它在下发任务了之后，然后这时候，具体执行任务的节点发生了崩溃，或者是服务的部署重启，那么这个任务是不是还能够正常恢复回来，继续执行？以及任务的进度如何进行存储？如果按照不同的存储机制或者持久化的策略，它的存储的数据量是不是合理的？这一些都要需要考虑。然后还有其他的我没有考虑到的，你再自己考虑一下，然后再开始写方案自己实现。"

In English: in production the service runs on servers, so data cannot live only
on a node's disk and needs a database. Which database fits? Does a task that
was handed out survive a crash or a redeploy of the node that runs it, and
continue? How is its progress stored, and is the stored volume reasonable under
different persistence policies? Think of what was not asked, then write the
plan and implement it.

Decision asked of the user this round: which PostgreSQL driver to add. The
answer was `github.com/jackc/pgx/v5`.

Earlier constraints still hold:

- secrets are read from the environment;
- no further dependency without approval;
- no LaunchAgent install and no push without confirmation.

Scope:

- `go/assistant`:
  - new `fleet_db.go` (the fleet's database on SQLite or PostgreSQL) and
    `fleet_vault.go` (tenants' files in the database, checkpoints, the fence,
    held answers, the cache sweep), with `fleet_vault_test.go` and the opt-in
    `fleet_vault_report_test.go`;
  - portable registry statements in `fleet_registry.go` and `fleet_cluster.go`;
  - attach, checkpoint loop, seal and missed sleep records in `fleet.go`;
  - the outbox's `durable` hook; the interruption limit in `store.go`;
    `ClearLeases` on open in `app.go`.
- `go/sqlitestate`: `Store.ClearLeases`.
- `go/cmd/miniloop-assistant`: `MINILOOP_FLEET_DATABASE_URL` and `-checkpoint`.
- `go/go.mod`, `go/go.sum`, the Linux CI harness's module inventory, and one
  reload test fixture's Go version.
- The roadmap, the README baseline, Mermaid and boundary text, the system
  architecture spec and its HTML, the layered base image, the Go README, the
  demo README, and the parity matrix and plan entries.

## The questions and the answers

### Which database

PostgreSQL.

| Need | Why PostgreSQL fits |
|---|---|
| A tenant is claimed by one node; a checkpoint is written only by the node that holds it | One transaction with a row lock and a conditional update. A key-value or document store needs the same guarantee rebuilt from compare-and-set calls |
| Deleting a tenant removes its registry row and all its data together | One transaction |
| Backups, point-in-time recovery and failover without writing a replication layer | Managed PostgreSQL has all three |
| Growth beyond one database | Every transaction touches one tenant, so tenants can be split across databases, or moved to a PostgreSQL-compatible distributed database, without changing a statement |

What was weighed against it:

- **MySQL** would do the same job. PostgreSQL was chosen for partial indexes,
  which the wake list uses, and `BYTEA` values that are compressed without
  configuration.
- **Redis** is not a place for the only copy of the data. It could later carry
  the wake signal between nodes; a one-second poll does that now.
- **Object storage** suits large files but has no transaction across objects
  and no way to make a write conditional on the registry. It is the right place
  for tenant pages if the page table outgrows PostgreSQL; the vault's interface
  is small enough to move.
- **Temporal**, which the repository's Task server uses, records every step of
  a long task in its own history. The assistant's work is many short turns per
  owner over shared state (mail, tasks, memory), not one long task, and its
  state already lives in per-owner SQLite files.

### What is stored, and why pages

| Data | Form |
|---|---|
| Registry: tenants, model usage, nodes, placements | Rows. The statements are the same on SQLite and PostgreSQL |
| A tenant's data: `assistant.db`, `agent.db`, the simulated mailbox, memory files | The files themselves, as 4 KiB pages in `vault_pages`, one row per page; `vault_heads` holds the tenant's checkpoint number |

A tenant stays an unchanged single-owner assistant on a directory of SQLite
files, open on one node at a time. The alternative, tenant tables as PostgreSQL
rows with a tenant column, was not taken:

- it means porting about 40 tables and every statement of `go/assistant`, and
  the agent's session store, which is kept identical to the Python
  implementation's SQLite schema and is checked against it;
- a turn makes dozens of small reads; on local SQLite they cost microseconds;
- one tenant's data can still be moved, exported or deleted as a unit.

The price: a tenant's mail, tasks and transcripts cannot be queried across
tenants in SQL, and a checkpoint writes whole pages.

### Does a task survive its node, and how is progress stored

A task is a trigger row in the tenant's `assistant.db`: its status, how often
it was claimed and when it may run again. Its progress is the worker's session
in `agent.db`, written as the turn goes. Both reach the database with the
tenant's checkpoints.

| Event | What happens |
|---|---|
| The node is redeployed | It stops its turns, stores a last checkpoint for every open tenant, and gives the placements up. The tenants are due at once and another node runs them |
| The node is killed | After `-node-ttl` (10 s) another node claims the tenant and reads its last checkpoint. Triggers that were running are queued again and run in the same worker session, which holds the transcript up to the checkpoint. Drafts and tasks the first run made are found by their dedupe keys, not made twice |
| The node stalls and is taken for dead | Another node takes the tenant. The stalled node's checkpoints are refused; it cannot answer a write or send an effect, and it closes the tenant |
| A turn takes its node down every time | The trigger is failed when it is interrupted on its fourth run, and the owner is told |

A turn is run again from its start; it does not continue at the step it
reached. The agent has no call that resumes a turn in the middle. What a
re-run keeps is the queue entry, the session with the earlier steps in it, and
the effects.

Two things were wrong before this round and are fixed for the single daemon too:

- A stopped process left its session leases in `agent.db`. The next process
  could not take them until they expired (15 s), so the first re-run of every
  interrupted turn failed with `session_lease_lost`, used one of four attempts
  and waited out a 20 s backoff. `NewApp` now clears the leases once it holds
  the directory's lock: no other process can be using them.
- A trigger whose turn stopped the process was queued again without limit.

### When is a checkpoint written, and what does each policy cost

| When | Why |
|---|---|
| Every `-checkpoint` (2 s) while the tenant is open, if anything changed | Bounds what a lost node takes with it |
| Before an owner's write (`POST`) is answered | An answered write is never undone |
| Before the outbox sends an approved effect | The approval is stored before anything leaves. The proposal's ID is the idempotency key, so a send repeated after a lost node is absorbed by the connector |
| When the tenant sleeps, before the placement is given up | The next node starts from the latest state |

Measured for one tenant doing 12 turns (8 chat, 4 mail) with the scripted
model, PostgreSQL 16 in a container on the same laptop
(`MINILOOP_VAULT_REPORT=1 MINILOOP_TEST_POSTGRES=... go test -run TestVaultReport -v ./assistant`):

| | |
|---|---|
| A tenant opened once | 303,623 bytes in 5 files, 76 pages |
| After 12 turns | 541,816 bytes: 19.8 KB of new data per turn |
| Checkpoints at owners' writes and at sleep only | 9 checkpoints, 274 pages, 92.6 KB per turn |
| Also after every turn | 20 checkpoints, 358 pages, 121 KB per turn |
| Also during the turn, every few milliseconds | 44 checkpoints, 553 pages, 188 KB per turn |
| One checkpoint | p50 4.9–6.9 ms, p99 6.9–10.4 ms |
| Opening a sleeping tenant | 11.5 ms from the node's copy, 17.6 ms from the vault |
| PostgreSQL tables for the three tenants | 1,056,768 bytes for about 1.6 MB of files |
| PostgreSQL write-ahead log for the whole run | 1,530,736 bytes for 4.8 MB of pages written |

Reading it:

- Page-sized units cost 5 to 9 times the new data before compression. After
  PostgreSQL compresses the pages it is 1.5 to 3 times, about 30–60 KB of log
  per turn.
- Checkpointing more often costs less than proportionally: 5 times as many
  checkpoints wrote twice the pages, because the same pages change again.
- With a real model a turn takes seconds, so the 2 s default should add a few
  checkpoints per turn and land between the second and third rows. That was
  not measured: the scripted model answers in microseconds.
- Storage per tenant is its file size, compressed to about two thirds; each
  page is stored once and replaced in place, so there is no history to collect.
  A tenant grows by about 20 KB per turn and nothing trims it yet.

### What else had to be decided

| Question | Answer |
|---|---|
| Can a node write a tenant it no longer holds? | No. A checkpoint first takes the tenant's placement row and compares the checkpoint number with the one the node last wrote. Either mismatch refuses it. With separate disks there is no shared file lock, so this is the only fence, and it is in the same transaction as the write |
| Can the owner see something that is later undone? | Not a write that was answered or an effect that was sent: both wait for a checkpoint. A page read may show up to `-checkpoint` of state a lost node takes with it |
| Are the tenant's databases consistent with each other in a checkpoint? | Yes. Each is read from a read snapshot, and the snapshots are of one instant: every database's `data_version` is read before the first snapshot and after the last, and any change makes the checkpoint read again. Without this a checkpoint could hold a session that mentions a draft the queue's database does not have |
| What if the database cannot be reached? | Owners' writes get 503. Checkpoints are retried. After `-node-ttl` without a heartbeat a node closes its tenants; a sleep it could not record is recorded when the registry answers |
| What does a node keep on disk? | A copy of each tenant it opened and a second copy to compare with. A copy is reused only if it was closed cleanly at the vault's current checkpoint by the same tenant; otherwise it is deleted and read again. Copies unused for `-archive-after` are dropped |
| Does a deleted tenant's data linger? | Its rows go in the transaction that removes it. A cached copy elsewhere carries the old tenant's creation time and is never given to a new tenant of the same name |
| Where does the password go? | `MINILOOP_FLEET_DATABASE_URL`, environment only. A URL that cannot be used is refused without being printed |

## Evidence

Tests in `fleet_vault_test.go` use nodes with separate data directories. They
run on a SQLite file by default and on PostgreSQL when `MINILOOP_TEST_POSTGRES`
holds a database URL; each test then gets a schema of its own.

| Test | What it shows |
|---|---|
| `TestVaultLetsNodesWithTheirOwnDisksShareTenants` | Written on one node, slept, read on another; a node's own copy is reused only while current |
| `TestVaultStoresOnlyChangedPages` | One line costs at most 8 pages; an unchanged tenant writes nothing; each page of each file is stored once |
| `TestVaultFollowsFilesThatShrinkAndGo` | Pages past a file's new end and a removed file's pages are deleted |
| `TestVaultKeepsCheckpointsOfAKilledNode` | The checkpointed state survives; a later write does not |
| `TestVaultAnswersAWriteOnceItIsStored` | A chat that was answered 202 is there after the node is killed at once |
| `TestVaultResumesATurnOfAKilledNode` | A turn stopped between two model calls, with its draft made and 5 or more session messages stored, ends `done` on the other node on attempt 2, in the same session, with one draft |
| `TestVaultRefusesANodeThatLostTheTenant` | Refused by the placement alone and by the checkpoint number alone; the write gets 503; the node closes the tenant; the holder's state is untouched |
| `TestVaultSnapshotsDatabasesAtOneInstant` | Commits placed between two snapshots make the checkpoint read again; a tenant that never rests is not stored half-read |
| `TestVaultCheckpointsAnOpenTenantOnItsOwn` | A line nobody checkpointed by hand survives the node |
| `TestVaultWakesEachDueTenantOnce` | Eight due tenants, two wakers, each opened once |
| `TestVaultForgetsADeletedTenant`, `TestVaultNodeDropsCopiesOfAbsentTenants` | No rows left; a stale copy not reused even at the same checkpoint number; old copies dropped and read back |
| `TestRegistryOnEitherEngine` | Every registry statement, with its result, on both engines |
| `TestFleetRecordsASleepTheRegistryMissed` | A sleep that could not be written is written later |
| `TestOutboxWaitsUntilTheApprovalIsStored`, `TestATriggerInterruptedTooOftenIsFailed` | The effect gate and the interruption limit |

Each safety rule was also removed in turn to see a test fail: the one-instant
check, the placement check, the checkpoint-number check, the held answer, the
creation-time check and the checkpoint-number check on reuse. The first run of
this had four rules whose removal no test noticed; those tests were rewritten.

Two real `miniloop-assistant fleet` processes with separate directories against
PostgreSQL 16, scripted model:

```text
1. chat through node A: 202
   mail arrives:        202
   approve proposal 1 through node B (passed on to A): 200
   channel lines: 7 | has the remembered line: True | proposals: [(1, 'sent')] | triggers: [(3, 'done', 1), (2, 'done', 1), (1, 'done', 1)]
   stats: {'database': 'postgres', 'active': 1, 'forwards': 0, 'takeovers': 0, 'checkpoints': 5, 'checkpoint_bytes': 693622, 'vault_pulls': 0, 'cache_reuses': 0, 'abandons': 0} nodes: [('18791', 1, True), ('18792', 0, True)]
2. a second mail starts a turn, and node A is killed with -9 right after it is accepted
   mail arrives: 202
   state through node B right after the kill: 503
3. after the node TTL, through node B: 200
   channel lines: 7 | has the remembered line: True | proposals: [(1, 'sent')] | triggers: [(4, 'quiet', 1), (3, 'done', 1), (2, 'done', 1), (1, 'done', 1)]
   stats: {'database': 'postgres', 'active': 1, 'forwards': 2, 'takeovers': 1, 'checkpoints': 1, 'checkpoint_bytes': 118784, 'vault_pulls': 1, 'cache_reuses': 0, 'abandons': 0} nodes: [('18792', 1, True)]
4. node A comes back with its old directory; node B is stopped cleanly
   one more line through node B: 202
   node B exit: 0
   state through node A: 200
   channel lines: 9 | has the remembered line: True | proposals: [(1, 'sent')] | triggers: [(5, 'done', 1), (4, 'quiet', 1), (3, 'done', 1), (2, 'done', 1), (1, 'done', 1)]
   stats: {'database': 'postgres', 'active': 1, 'forwards': 0, 'takeovers': 0, 'checkpoints': 0, 'checkpoint_bytes': 0, 'vault_pulls': 1, 'cache_reuses': 0, 'abandons': 0} nodes: [('18791', 0, True)]
vault: seq=10 bytes=435320 files=7 pages=110 table_bytes=245760
```

The mail accepted in step 2 was stored before its `202`, so node B found it and
ran its turn. In this run the kill came before node A had claimed the trigger,
so node B's run was the first attempt; the test above covers a turn stopped in
the middle.

## Gates

Heavy checks ran one at a time with `GOMAXPROCS=2 GOFLAGS=-p=1
GOMEMLIMIT=1536MiB nice -n 10`, `-parallel=2`, and pytest without xdist.

| Gate | Result |
|---|---|
| `gofmt -l .` in `go/` | no files |
| `go vet ./...` | ok |
| `go test ./... -count=1` | first run: 52 packages ok, `reload` failed (below). After the fix `reload`, `assistant`, `cmd/miniloop-assistant` and `sqlitestate` were run again: ok |
| `go test -race ./... -count=1` | 53 packages ok |
| `go test -race ./assistant -run 'Vault\|Registry\|Cluster\|Fleet\|Outbox' -count=2` with `MINILOOP_TEST_POSTGRES` (PostgreSQL 16) | ok |
| Two `miniloop-assistant fleet` processes on PostgreSQL, holder killed with `SIGKILL` | output above |
| `.venv/bin/python -m pytest -q -p no:xdist` | 2256 passed, 28 skipped; `ci/go-port` unit tests 31 passed |
| `python/tools/verify_scans.py` | 19 guards anchored |
| `python/tools/export_go_contracts.py --check` | 268 files current |
| `ci/go-port/bootstrap.py` `module_inputs` against the real `go.mod` and `go.sum` | accepted |
| Archify `deliver` on the system spec | 9 of 9 checks, no errors or warnings |
| README Mermaid node IDs (script) | 124 defined, none missing |
| `git diff --check` | clean |

The `reload` failure: three supervisor tests build a child from a fixture
module that requires this module. The fixture said `go 1.23`; this module now
says `go 1.23.0`, and the build refused. The fixture was raised to `go 1.23.0`
and the package passes.

Not run:

- the Linux acceptance workflow (`ci/go-port`): only its inventory check and
  its unit tests;
- PostgreSQL on another machine, a replicated PostgreSQL, a failover, more
  than two nodes, or any load;
- the measurement with a real model;
- migration from a data directory: there is no tool;
- `research-site` `npm test`: `node_modules` is not installed and Node 22.12 is
  below the required 22.13. `npm run content` and `content:check` run in a
  separate commit;
- `verify_invariants` and `verify_guards`: no Python package module or guarded
  anchor changed;
- a Mermaid renderer: a script checked only that every node ID in an edge is
  defined.

Known and not fixed: `TestPauseHoldsProcessingAcrossARestartAndResumeCatchesUp`
can fail rarely (see the D13 record); it did not fail in this round's runs.

## Architecture map

The README Mermaid gains:

- a `PAVault` node, "PostgreSQL · only with MINILOOP_FLEET_DATABASE_URL · run
  by the operator", with the registry and vault tables;
- a dashed edge from `PAFleet` to it: registry statements, changed pages every
  2 s, at sleep, before an owner's write is answered and before an approved
  effect is sent, and the whole tenant on open when the node's copy is not
  current;
- a line on `PAFleet` naming the opt-in database, and `PAFleetPeer` now says
  the nodes share a data directory or a database.

The system architecture JSON gains a `paVault` component beside `paFleet` with
one dashed connection, a tag on `paFleet`, and the D14 text in the subtitle and
the personal-assistant card.

The base layered image was edited once with imagegen. That edit is both the new
`docs/mini-loop-layered-architecture.png` and this iteration's image,
`go-assistant-fleet-database-2026-11-21.png`. In the bottom band, box 2
(`assistant.db × 每个租户`) now reads "可选：检查点存入 PostgreSQL" and carries the
highlight, the strip reads "节点磁盘只是缓存 · 先存检查点再应答和发送 ·
节点丢了从检查点接着跑", and the badge reads "2026-11-21：fleet 状态进 PostgreSQL ·
节点只是缓存". Box 1 lost last iteration's highlight. The first run was kept: the
band's texts are as asked, and the layers above it look as they did in the
base, including the small text that earlier edits had already blurred.

The exact prompt is in `go-assistant-fleet-database-2026-11-21.prompt.txt` and
`docs/mini-loop-layered-architecture.prompt.txt`.

The personal-assistant view `docs/architecture/personal-assistant.architecture.json`
pins a revision that must contain `fleet_vault.go`, so it moves in a follow-up
commit.

## Deviations from the plan

- The roadmap listed PostgreSQL under "not for the demo" until a dependency was
  approved. The user approved pgx this round.
- The roadmap's next phase was P1, hardening the single-Mac daemon. It moves
  back again; two of this round's fixes (stale leases, the interruption limit)
  are P1 material and apply to the daemon.
- Tenant data went into the database as pages, not rows. The request said
  "a database"; rows are the more usual reading. The reasons are above, and the
  cost is stated in the boundaries.
- The Linux acceptance harness pins `go.mod` and `go.sum` exactly and asks for
  a review when they change. Its inventory was updated to the new module set.
  The harness itself was not run.
- `go.mod` now says `go 1.23.0`, because pgx does. A module that requires this
  one must say at least that; the reload test's fixture module was raised.

## Boundaries

- **Pages, not rows.** A tenant's mail, tasks and transcripts cannot be queried
  across tenants. The registry and usage can.
- **A turn is re-run, not resumed at a step.** A lost node costs up to
  `-node-ttl` of availability for its tenants and up to `-checkpoint` of work,
  and the re-run repeats the turn's model calls.
- **PostgreSQL's availability is the fleet's.** Not run against a replicated
  database or through a failover.
- **Reads can run ahead** of the vault by up to `-checkpoint`.
- **Cost grows with the tenant.** A checkpoint copies each changed database
  locally before comparing pages; an open from the vault reads the whole
  tenant; nothing trims history; there is no size limit per tenant below the
  4 GB a restore accepts.
- **A request at the wrong node** costs a few registry statements before it is
  passed on.
- **No migration** of an existing data directory into the database.
- **Clocks.** Liveness still compares a heartbeat with the reader's clock.
- **Nodes trust each other and the database.** Tenant data in PostgreSQL is not
  encrypted by the fleet; use the database's own encryption and TLS
  (`sslmode=verify-full`).
- **Scale was not measured.** Two nodes on one laptop, PostgreSQL in a
  container, the scripted model.
