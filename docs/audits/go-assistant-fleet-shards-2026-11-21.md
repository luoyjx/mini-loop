# Go personal-assistant demo D15 — shards, tiers and what a lost node can change (2026-11-21)

Baseline: `7ffcac2` (D14 `97391eb`, its architecture view `5f7dcf3` and the
research reindex `7ffcac2`).

Request this round implements (2026-11-21): "数据规模上去之后为了保证数据库的性能和陈本，分片和数据分层也实现一下，还有什么分布式高可用系统需要考虑的点我漏掉的你也补充上一起做了"

In English: once the data grows, keep the database fast and affordable:
implement sharding and data tiering too, and add and implement whatever else a
distributed, highly available system needs that the request left out.

No decision was asked of the user this round. No dependency was added.

Earlier constraints still hold:

- secrets are read from the environment and never printed;
- no dependency without approval;
- no LaunchAgent install and no push without confirmation.

Scope:

- `go/assistant`:
  - new `fleet_shards.go` (shards, moves between them, leftovers, the
    per-shard breaker) with `fleet_shards_test.go`;
  - new `fleet_cold.go` (the archive store on a directory or an S3-compatible
    bucket, archiving and reading back) with `fleet_cold_test.go`;
  - `fleet_db.go`: numbered schema steps, statement deadlines, connection
    settings;
  - `fleet_vault.go`: the fence in the shard, checksums, cache budget, copies
    that are ahead of the vault;
  - `fleet_cluster.go`, `fleet.go`, `fleet_http.go`, `fleet_registry.go`,
    `fleet_budget.go`: the registry's clock, the outage grace, the route cache,
    readiness, metrics, the shard API, usage retention, usage kept through an
    outage, and nodes that start at the same moment.
- `go/cmd/miniloop-assistant`: `MINILOOP_FLEET_SHARD_URLS`, the archive store's
  flags and key variables, and the new tuning flags.

A fleet without `MINILOOP_FLEET_DATABASE_URL` is unchanged. One with it and
neither shards nor an archive store behaves as in D14; its tables are brought
up to date in place.

## The questions and the answers

### Why shard, and how

In D14 one PostgreSQL held everything. The registry needs a few hundred bytes
per tenant; a tenant's pages take hundreds of kilobytes. The size of that
database, its write volume and its connections therefore follow the tenants'
data. The pages are what has to be spread.

- **A directory, not a hash.** `tenants.shard` names the database that holds a
  tenant's pages. Adding a shard moves nothing, and a tenant can be moved alone.
  A hash of the tenant ID would need every node to agree on the shard list at
  every moment and would move a share of all tenants whenever the list changes.
- **One tenant, one shard.** Every statement about a tenant's pages goes to one
  database. No transaction spans two.
- **Placement.** A new tenant gets the open shard with the fewest tenants when
  it is first opened. Nothing balances by size afterwards.
- **States.** `open` takes new tenants, `closed` keeps what it has, `draining`
  gives its tenants to the open shards, `retired` is empty for good. A shard
  new to an existing fleet joins `closed`, so that a node that was not yet
  restarted with its URL is never asked to open a tenant there. A node that
  lacks the URL of a shard the registry knows does not start.
- **Moving a tenant.** The target's place for the tenant is emptied, the source
  is read in one snapshot and written to the target in one transaction, the
  registry is pointed at the target, and the source is removed. Each node moves
  up to 8 sleeping tenants every 5 seconds. An open tenant waits until it
  sleeps.
- **One shard must not take the others down.** Every statement has a deadline.
  After three failures in a row a node leaves the shard alone for 5 seconds at
  a time, so a request for one of its tenants fails at once instead of holding
  a connection and a goroutine for the length of a timeout.

The registry itself is not sharded. It holds a row per tenant, a row per
tenant and day of use, and the nodes' rows.

### Why tier, and how

A tenant nobody uses still took the most expensive storage the fleet has.

| Tier | Where | A tenant is there while |
|---|---|---|
| Hot | A node's disk | The node opened it less than `-cache-after` ago and the node's copies fit in `-cache-max-mb`. It is a cache |
| Warm | A shard | It is in use, or no archive store is configured |
| Cold | The archive store | Its owner has been away `-archive-after` and nothing is due to wake it within 24 hours |

- **One object per tenant.** The tenant's files are written from the shard into
  a gzip tar. Measured: 1.42 MB of files in four tenants became 78.7 KB.
- **Which copy is the truth.** While the registry says `cold`, the object is.
  Once it says `warm`, the shard is. A reading back empties the tenant's place
  in the shard, unpacks the object, stores it as the tenant's first checkpoint
  there, tells the registry and then deletes the object. If it stops before
  the registry is told, the next open does it again from the object.
- **The store.** A directory, or an S3-compatible bucket. The bucket needs PUT,
  GET and DELETE of one object, signed with Signature Version 4, which the
  standard library's HMAC and SHA-256 are enough for. An SDK would have been a
  dependency of several megabytes for three requests.
- **Not archived only to be read back.** A tenant with something due within a
  day stays in its shard.

### What a step that stops half way leaves

A move, an archiving and a reading back each make a second copy of a tenant
before the first is removed, in two systems no transaction spans. A copy that
must go is therefore written down in the registry's `garbage` table before it
exists, and the row goes when the copy is gone:

- the target of a move, until the registry names it as the tenant's shard;
- the source of a move, from that same statement on;
- the object of an archiving, until the registry calls the tenant cold;
- the shard's copy of an archived tenant and the object of one read back;
- everything of a deleted tenant.

The janitor removes what is written down. For a tenant that still exists it
first takes the tenant's placement, so it never removes the copy of a step
that is still running.

### What a node that was taken for dead can still change

This was the part the first implementation got wrong, and the guard-removal
check found it. A node holds a tenant through its placement row. A node that
is held up for longer than `-node-ttl` (a stopped virtual machine, a long
pause) loses that row to another node without knowing. D14 already refused its
checkpoints. Its moves, archivings and removals were refused only by what it
had read before it was held up. Three sequences lost data:

- The node archives a tenant and is held up before it tells the registry.
  Another node opens the tenant, writes it and puts it to sleep. The first
  node goes on, the registry accepts "cold" because the tenant is asleep and
  warm, and the shard's newer copy is removed.
- The node moves a tenant and is held up before it removes the old copy.
  Another node moves the tenant back. The first node goes on and removes the
  old copy, which is the tenant's data again.
- The node reads a tenant back and is held up before it deletes the object.
  Another node archives the tenant again under the same object name. The first
  node goes on and deletes the object.

The rules that close them:

- **The registry believes only the holder.** Every statement that changes
  where a tenant's data is (`moved`, `frozen`, `thawed`) also requires the
  node's placement row, in the same statement.
- **A shard lets only the writer remove.** A node that opens a tenant in a
  shard, or moves it there, writes itself into the tenant's head row. Removing
  a copy requires being that writer. The janitor makes itself the writer of a
  leftover and then reads the registry again: a node that took the tenant
  there earlier is visible in the registry, and one that takes it there later
  replaces the janitor as the writer.
- **An object's name is used once.** Each archiving writes a new object, named
  in `tenants.archive`. A late delete removes an object nothing refers to.

### What else was missing

| Gap | What happens now |
|---|---|
| The registry cannot be reached | D14 closed every open tenant after `-node-ttl`. Now a node with a vault keeps them open for `-registry-grace` (12 × `-node-ttl`, 2 minutes): no other node can take them without the registry either, and their checkpoints go to the shards. When the registry answers, the node keeps the tenants no other node took and gives up the rest. A request whose token cannot be checked gets 503, not 401, so a client does not discard its token |
| Model use during that outage | D14 logged and dropped it. Now it is counted in memory and recorded when the registry answers, and a model call no longer waits on a registry that is away |
| Clocks | D14 compared a heartbeat with the reader's clock. With PostgreSQL the nodes now write and judge heartbeats by the registry database's clock |
| A database that never answers | Every statement gets a deadline of 20 seconds, transfers one that grows with the tenant, a connection 5 seconds. A URL may name several hosts |
| Silent damage | Each stored file has a SHA-256. A tenant whose pages do not match does not open, and the node counts it. Nothing repairs it |
| Schema changes | The tables change in numbered steps, recorded in the database and run under a lock. A node refuses a database a later version changed, and shards or an archive store while a node from before them is alive |
| Stopping a node | `/readyz` answers 503 once the node is told to stop, and `-leave-wait` gives the load balancer time before the node closes |
| Seeing what happens | `/admin/metrics` in the Prometheus text format; `/admin/shards` |
| A request at the wrong node | The node remembers for 3 seconds where the tenant is and the hash of the token it checked, and passes that owner's requests on without the registry |
| A close that could not be stored | The node marks its copy as ahead of the vault. If it opens the tenant next it goes on from there; if another node wrote first, the copy is replaced |
| Growth of the usage table | `-usage-retention` drops old days, one day to a statement |
| Connections | A node keeps at most `-database-conns` (8) to each database |
| Nodes that start together | Each clears the rows of nodes that stopped before, in a snapshot transaction, and PostgreSQL ends all but one such transaction. D14 exited on that. A node now starts and the heartbeat clears what is left |
| A store's refusal in the log | S3 names the access key ID in the answer to a request it refuses. The keys are taken out before the answer is printed |

## Evidence

Tests in `fleet_shards_test.go` and `fleet_cold_test.go` use nodes with
separate data directories. They run on SQLite files by default, one file per
shard, and on PostgreSQL when `MINILOOP_TEST_POSTGRES` holds a database URL;
each shard is then a schema of its own.

| Test | What it shows |
|---|---|
| `TestShardsSpreadTenantsAndKeepTheirDataApart` | Four tenants, two in each shard, no pages in the registry database; any node reads each from its shard |
| `TestShardThatIsAwayHoldsUpOnlyItsTenants` | A shard that fails is left alone after three failures; the other shard's tenants are served; a node starts without it |
| `TestShardNewToTheFleetWaitsToBeOpened` | A new shard joins closed; a node without a known shard's URL does not start; the last open shard stays open; a shard is retired only when empty |
| `TestDrainingAShardMovesItsTenants` | Sleeping tenants move, an open one waits, the shard is retired and a node without its URL serves every tenant |
| `TestMoveLeftHalfDoneIsCompletedOrUndone` | A move stopped after the copy is undone; one stopped after the registry was told is completed; neither while the mover holds the tenant |
| `TestDeletedTenantLeavesNothingInItsShard` | Also when the shard was away at the delete, and when a tenant of the same name exists again |
| `TestVaultRefusesPagesThatDoNotMatchTheirChecksum` | A damaged page and a missing page |
| `TestVaultNodeDoesNotTrustACopyByItsNumberAlone` | A copy with the shard's checkpoint number and another checkpoint's name is read again |
| `TestAbsentTenantsMoveToTheArchiveStoreAndBack` | Dormant after 3 days, archived after 7, read back by another node, archived again under a new name, deleted with its object |
| `TestTenantDueSoonIsNotArchived` | Due in an hour stays; due in three days goes |
| `TestArchiveIsTheTruthWhileATenantIsCold` | A stale copy in the shard is not read; a reading back that stopped is done again; a missing object is an error, not an empty tenant |
| `TestNodeTakenForDeadChangesNothingWhenItGoesOn` | A node held up at eight points of a move, an archiving, a reading back and a removal while the other node takes the tenant. Each time everything written is still there, read from the shard, and no copy is left over |
| `TestS3SignatureMatchesTheDocumentedExample`, `TestArchiveStoreSpeaksS3` | The worked example from the Signature Version 4 documentation; a test server that verifies every signature; a wrong key leaves the tenant in its shard |
| `TestArchiveStoreAgainstARealBucket` (needs `MINILOOP_TEST_S3_URL`) | PUT, GET and DELETE against Garage v1.0.1 |
| `TestClusterKeepsTenantsThroughARegistryOutage` | Tenants kept, a write stored during the outage, 503 for a token that cannot be checked, tenants given up after the grace and when another node took them |
| `TestFleetRecordsModelUseTheRegistryMissed` | Calls made while the registry is away are recorded once it answers, once |
| `TestNodesThatStartTogetherAllStart` | Six nodes join at the same moment, five times, while the rows of four stopped nodes are there. On PostgreSQL it fails without the change; SQLite has no such conflict |
| `TestClusterJudgesHeartbeatsByTheRegistryClock` | Nodes whose clocks are 7 minutes ahead and 4 minutes behind see each other alive, and a node that stops is still taken over |
| `TestFleetDatabaseIsUpgradedInPlace` | Tables from before shards are upgraded with their tenant; shards wait for old nodes to stop; a newer schema is refused |
| `TestVaultNodeKeepsItsCopiesWithinABudget`, `TestVaultKeepsChangesItCouldNotStoreAtClose`, `TestFleetDropsOldUsage`, `TestFleetReadinessAndMetrics`, `TestDatabaseTroubleIsToldFromAnAnswer` | The cache budget, copies ahead of the vault, usage retention, readiness and metrics, and deadlines |

Each new rule was removed in turn to see a test fail: 53 removals, among them
the writer and the checkpoint number in a checkpoint, the placement in each of
the three registry statements, the writer in a removal, the second look at
the registry, the per-archiving object name, the row written before an object
is stored, the breaker, the checksum, the registry's clock, each branch of the
outage handling, the schema refusal and the cache budget. Every removal made a
test fail. Three runs were needed to get there:

- the first, of 36 removals, left 4 standing, and one removal did not
  compile. One of the four was a duplicate guard, which was deleted. Two were
  missing assertions. The fourth, the conditions on the archiving statement,
  led to the three sequences described above;
- the second, of 48, left none standing, but one removal made its test hang
  instead of fail: the test asserted while it held a tenant. It now releases
  first;
- the third, of 51, left none standing. Two rules came after it, each with
  its removal: the store's keys are taken out of a refusal before it is
  printed, and a node starts when another clears the same rows;
- the fourth, of 53, is the run of the final code.

Two `miniloop-assistant fleet` processes with separate directories, a
PostgreSQL 16 registry, two more PostgreSQL 16 servers as shards `s1` and `s2`,
and a Garage v1.0.1 bucket, all in containers on one laptop, scripted model,
`-node-ttl 10s -checkpoint 1s -idle 3s`. This is the run of the final binary,
one script from an empty registry database to the end:

```text
1. four tenants, chat through both nodes
   shards: home:closed:0 s1:open:2 s2:open:2
   page rows: registry=0 s1=162 s2=160
   tenants: [('fox1','warm','s1'), ('fox2','warm','s2'), ('fox3','warm','s1'), ('fox4','warm','s2')]
   fox1 read through the node that does not hold it, twice: 200, 200 (forwards 2, route_hits 1)
2. the server of s1 is paused (packets dropped)
   fox2 on s2: 202 in 0.023s
   fox1 on s1, 1st write: 503 in 5.0s
   fox3 on s1, write:     503 in 8.0s
   fox1 2nd write:        503 in 5.0s
   fox1 3rd write:        503 in 0.014s
   fox4 on s2: 202 in 0.043s
   shards: home:closed:0 s1:open:2 DOWN s2:open:2
   resumed; next write to fox1: 202 in 0.114s
   fox1 has 1 of its 3 refused writes
3. the registry's server is paused
   12 writes each to fox1 (open on A) and fox2 (open on B): 24 of 24 answered 202
   sleeping tenant fox3, asked as the pause began: 503 in 20.0s
   /readyz: 200
   resumed
   fleet: the registry was away for 22s; kept 1 tenants open and gave up 0 that another node took
   fleet: the registry was away for 17s; kept 1 tenants open and gave up 0 that another node took
   both nodes: outages 1, fences 0, usage_owed 0
   fox1 has 12 of 12 of those lines, read through B; fox2 12 of 12, read through A
   model calls recorded for fox1: 6 before, 18 after; for fox2: 3 before, 16 after
4. node A is killed with -9 right after it accepted a chat for fox1, which it held
   through B: 503 503 503 503 503 503 200 (about 9 s)
   history kept: True | last request kept: True
   both nodes killed with -9 and started at the same moment, five times: ready, ready each time
5. s1 is set to draining
   page rows before: s1=191 s2=185
   after one 3 s wait: home:closed:0 s1:draining:0 s2:open:4
   page rows after: s1=0 s2=376, leftovers 0
   s1 retired; fox1 and fox3 read from s2: 200, 200
6. both nodes restarted without the URL of s1, with -archive-store on the
   bucket, -dormant-after 6s -archive-after 12s
   after 12 s: 4 tenants cold
   s2: 0 page rows; bucket: +4 objects, +86.4 KiB
   frozen_bytes 406277 + 1084387, archive_bytes 24581 + 63880, leftovers 0
   owner of fox1 returns through B: 200 in 0.062s, history kept; bucket: -1 object
   fox4 deleted while archived: 204; bucket: -1 object; leftovers 0
7. node A is stopped with -leave-wait 2s
   /readyz at once: 503; a request for fox1 to it meanwhile: 200; closed after the wait
processes left: 0; panics or races in the logs: 0; nodes that exited with an error: 0
```

What the runs showed that the tests had not:

- **Two nodes started at the same moment, and one exited.** The run before
  this one started both nodes together in step 6, and the second stopped with
  `could not serialize access due to concurrent update (SQLSTATE 40001)`.
  Every node clears the rows of the nodes that stopped before when it starts,
  in a snapshot transaction, and PostgreSQL lets one of two such transactions
  remove a row. The heartbeat already treated that as "look again next time";
  the start treated it as fatal. A fleet that came back after a power cut
  would have lost all but one of its nodes until something restarted them. A
  node now starts and leaves what is left to the heartbeat.
  `TestNodesThatStartTogetherAllStart` starts six at once; it fails without
  the change on PostgreSQL and cannot fail on SQLite, which has no such
  conflict. Step 4 now does it five times with two processes.
- **A count of model use was written twice.** The 24 calls of step 3 were
  recorded as 25, and as 26 in the run before. A node whose statement gets no
  answer gives up after 3 s and records the count again later. The likely
  cause, not checked on the server's side, is that the paused server had the
  first statement in its socket and ran it when it resumed. Nothing bounds
  this to one call: any count sent to a server that takes it and does not
  answer can be written again. It errs towards a tenant's budget being used
  up sooner, and an exact count would need a key per write.
- **How long a 503 takes depends on what the node was waiting for.** In step 2
  the writes waited 5 to 8 s here and 15 s in an earlier run: the node waits
  for a checkpoint before it answers. In step 3 the sleeping tenant's request
  waited the whole statement deadline, 20 s, because it was sent on a
  connection that was open when the registry stopped; in the run before it
  came after the pool had dropped its connections and took 5 s, the connect
  timeout. Once a shard is left alone the answer takes 14 ms. The registry has
  no such breaker.
- **A write answered 503 may have been kept.** One of the three writes to
  `fox1` that were answered 503 in step 2 was in the tenant afterwards (two of
  three in the earlier run). The node had applied it and stored it with the
  first checkpoint after the shard came back. A client that sent it again
  would have written it twice.
- In step 3 `/readyz` stayed 200. A node that cannot reach the registry still
  serves its open tenants, and taking it out of rotation would cut them off.

## Gates

Heavy checks ran one at a time with `GOMAXPROCS=2 GOFLAGS=-p=1
GOMEMLIMIT=1536MiB nice -n 10`, `-parallel=2`, and pytest without xdist. Every
row is a run of the final code.

| Gate | Result |
|---|---|
| `gofmt -l .` in `go/` | no files |
| `go vet ./...` | ok |
| `go test ./... -count=1` | 53 packages ok |
| `go test -race ./... -count=1` | 53 packages ok. An earlier run failed in `assistant` (below) |
| `go test -race ./assistant -count=2` of the fleet's tests with `MINILOOP_TEST_POSTGRES` (PostgreSQL 16, each shard a schema) and `MINILOOP_TEST_S3_URL` (Garage v1.0.1) | 63 tests ok, both times. `TestFleetLoad` and `TestVaultReport` are skipped without their own switches |
| 53 removals of new rules, each run against its test, three of them on PostgreSQL | 53 made a test fail |
| Two `miniloop-assistant fleet` processes, a PostgreSQL registry, two PostgreSQL shards and a Garage bucket | output above |
| `.venv/bin/python -m pytest -q -p no:xdist` | 2256 passed, 28 skipped; `ci/go-port` unit tests 31 passed |
| `python/tools/verify_scans.py` | 19 guards anchored |
| `python/tools/export_go_contracts.py --check` | 268 files current |
| Archify `deliver` on the system spec | no errors or warnings; it writes the HTML that is committed |
| README Mermaid, parsed and rendered by Mermaid 11.4.1 | 117 nodes, 221 edges |
| `git diff --check` | clean |
| The change searched for the test credentials and for key patterns | none of the credentials. The one key of Amazon's shape is the example pair from Amazon's documentation, in the signature test |

The race failure: `TestShardThatIsAwayHoldsUpOnlyItsTenants` put its tenants to
sleep while a turn that a chat had started was still running, and one tenant
stayed open. The test now waits for the turns. It was a fault of the test.

Not run:

- the Linux acceptance workflow (`ci/go-port`): only its unit tests;
- Amazon S3 or any store but Garage; a replicated PostgreSQL or a failover; a
  connection pooler; separate machines; any load; a real model;
- GitHub's own Mermaid renderer;
- `ci/go-port/bootstrap.py` `module_inputs`: `go.mod` and `go.sum` did not
  change;
- `research-site` `npm test`: `node_modules` is not installed and Node 22.12 is
  below the required 22.13. `npm run content` and `content:check` run in a
  separate commit;
- `verify_invariants` and `verify_guards`: no Python package module or guarded
  anchor changed.

Known and not fixed: `TestPauseHoldsProcessingAcrossARestartAndResumeCatchesUp`
can fail rarely (see the D13 record); it did not fail in this round's runs.

## Architecture map

The README Mermaid gains:

- `PAShards`, "PostgreSQL shards · only with MINILOOP_FLEET_SHARD_URLS", with
  the vault tables, the four states and the writer rule;
- `PAArchive`, "Archive store · only with -archive-store";
- `PAVault` is now the registry: its tables, the nodes' clock, and the shard
  named `home`;
- three dashed edges from `PAFleet` in place of one: registry statements, pages
  and moves, and archiving;
- two lines on `PAFleet` for the outage grace, the new endpoints and what the
  janitor now does.

The README Mermaid was parsed and rendered by Mermaid 11.4.1 with its default
limits, for the first time in this series. It did not parse: the `PASearch`
label, added with D9 (`65ce3e6`), held a quotation inside its quoted text, so
this Mermaid cannot have parsed the canonical diagram since then. How GitHub
showed it was not checked. The quotation marks are now
`#quot;`, and the block renders with 117 nodes and 221 edges. It is 40,225
characters long (40,797 bytes); Mermaid's default limit is 50,000 characters.

The system architecture JSON gains `paShards` and `paArchive` beside `paFleet`
with one dashed connection each, new tags on `paFleet` and `paVault`, and the
D15 text in the subtitle and the personal-assistant card. Archify `deliver`
passed 9 of 9 checks with no errors or warnings.

The base layered image was edited once with imagegen. That edit is both the new
`docs/mini-loop-layered-architecture.png` and this iteration's image,
`go-assistant-fleet-shards-2026-11-21.png`. In the bottom band, box 2
(`assistant.db × 每个租户`) now reads "可选：PostgreSQL 分片 + 冷归档" and keeps the
highlight, the strip reads "热：节点缓存 · 温：PostgreSQL 分片 ·
冷：归档存储里的对象", and the badge reads "2026-11-21：fleet 分片 · 冷热分层 ·
注册表不可达时照常服务". The first run was kept: the band's texts are as asked,
and the layers above it look as they did in the base, including the small text
that earlier edits had already blurred.

The exact prompt is in `go-assistant-fleet-shards-2026-11-21.prompt.txt` and
`docs/mini-loop-layered-architecture.prompt.txt`.

The personal-assistant view `docs/architecture/personal-assistant.architecture.json`
pins a revision that must contain `fleet_shards.go` and `fleet_cold.go`, so it
moves in a follow-up commit.

## Deviations from the plan

- The roadmap listed sharding and object storage under "not yet, until the
  fleet is deployed to a cloud". The user asked for both this round.
- The roadmap's next phase was P1, hardening the single-Mac daemon. It moves
  back again.
- The request named sharding and tiering. About half of the change is the
  third part, "what was left out". The table above lists what was added under
  it, so that each item can be judged on its own.
- The first version of this round passed its tests and a first run of real
  processes while it could lose a tenant's data in the three sequences above.
  Removing guards one at a time showed a condition no test needed, and
  following that led to them. The placement condition, the writer condition,
  the per-archiving object name and the test that holds a node up were added
  after that. The process-level run above is of the final code.
- The run of the final binary found a defect that D14 already had: of two
  nodes started at the same moment, one could exit. It is not about shards or
  tiers. Its fix and its test are in this round because a fleet that cannot
  come back after a power cut is not highly available.
- No Amazon S3 account was used. `minio/minio` could no longer be pulled from
  Docker Hub or quay.io, so the real S3 server is Garage.

## Boundaries

- **The registry is one database** and is not sharded. While it is away only
  open tenants are served, for up to `-registry-grace`.
- **Nothing balances shards by size.** Tenants move only out of a draining
  shard, and an open tenant only once it sleeps.
- **Damage is found, not repaired.**
- **Databases restored to different moments are not reconciled.** A registry
  restored to before a move or an archiving points where the data no longer
  is.
- **Mixed versions.** Nodes from before this round must be stopped before
  shards or an archive store are turned on.
- **Model use during a registry outage** is kept in memory and lost if the node
  stops first. A count whose statement timed out may be recorded twice; the
  process-level runs recorded 25 and 26 calls for 24.
- **The registry has no breaker.** A request that needs it while it does not
  answer waits for the statement deadline, 20 seconds, or the connect timeout,
  5 seconds, before its 503.
- **A write answered 503 may have been kept**, and chat has no idempotency key.
- **The archive store's keys are static.** One PUT per tenant, which S3 allows
  up to 5 GB.
- **Not run:** Amazon S3 or another provider than Garage, a PostgreSQL
  failover (a paused container stands in), a connection pooler, more than two
  processes (six nodes in one test process), separate machines, any load, a
  real model.
- Everything D14 listed still holds: pages and not rows, a turn re-run and not
  resumed at a step, no migration from a data directory, no trimming of a
  tenant's history.
