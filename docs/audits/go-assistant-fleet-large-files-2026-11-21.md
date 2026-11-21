# Go personal-assistant demo D16 — tiering a tenant's data by size (2026-11-21)

Baseline: `1d55b3c` (D15 `6b57d05`, its architecture view `5c37342` and the
research reindex `1d55b3c`).

Request this round implements (2026-11-21): "我希望也能够按照数据大小自动分层，比如单条数据超过一定大小影响性能时"

In English: tier data by size automatically as well, for example when a
single item grows past a size where it hurts performance.

No decision was asked of the user this round. No dependency was added.

Earlier constraints still hold:

- secrets are read from the environment and never printed;
- no dependency without approval;
- no LaunchAgent install and no push without confirmation.

Scope:

- `go/assistant`:
  - new `fleet_files.go` (which files are large, writing and reading their
    objects, the rule for a new object, the store's time limits and cooldown,
    the janitor's check of an object) with `fleet_files_test.go` and the
    report `fleet_files_report_test.go`, which runs only with
    `MINILOOP_LARGE_FILE_REPORT`;
  - `fleet_vault.go`: a checkpoint stages large files before its transaction
    and settles them after it; a read puts the object under its pages;
  - `fleet_shards.go`: shard schema step 3, moves that copy the new columns,
    removals that write their objects down first;
  - `fleet_cold.go`: an archiving puts a large file whole into the tar and
    removes its objects;
  - `fleet_db.go`, `fleet.go`, `fleet_http.go`: the version gate, the janitor's
    `~file/` rows, the counters.
- `go/cmd/miniloop-assistant`: `-large-file-kb` (1024, 0 off).

A fleet without `-archive-store`, or with `-large-file-kb 0`, keeps every file
as pages. Its shards are brought up to schema step 3 in place.

## The questions and the answers

### What is large here, and why by file

A fleet tenant's data is a few SQLite files and the Markdown memory. D14 stores
every file in its shard as 4 KiB pages, one row each, and a checkpoint writes
only the pages that changed. A changed page is cheap; many stored pages are
not: each is a row and an index entry that the database logs, replicates,
backs up and vacuums.

What grows in a tenant is a file — a database that gets longer, a note or an
attachment written whole — not a row. Rows are bounded by the inputs: a chat
message is 2,000 characters at most, a mail 256 KiB. So the unit of the tier
is the file. A file of `-large-file-kb` (1 MiB) or more keeps its bulk as one
object in the archive store, the store D15 added for absent tenants.

### Base and pages over it

- A file reaching the size is written whole to the store as one object, named
  `<tenant>.<born>.<random>.file`, so no two objects share a name. The file's
  row in the shard (`vault_files`) records the object, its size and its
  SHA-256; no pages stay.
- A file that changes a few pages at a time, as a database does, goes on
  writing only those pages, over the object.
- Once the pages over the object, or the pages one checkpoint changes, come to
  more than a quarter of the file, the next checkpoint writes a new object and
  removes the file's pages; the old object goes after the checkpoint. A file
  written whole crosses that at once; a growing database crosses it each time
  it grows by a quarter. The count over the object is the one the last
  checkpoint took in the shard, so the same pages changed again are not
  counted twice.
- A file that shrinks below half the size, or every file once large files are
  turned off, goes back to pages, and its object goes.
- A reader takes the object, puts the pages over it and checks the whole file's
  SHA-256. A missing object, or one that is not what was written, keeps the
  tenant closed and is counted as damage, as a damaged page is.

### What the store being away does to a checkpoint

- A checkpoint waits for one request to the store at most 5 s and 1 s more for
  each MiB it sends, and never more than half the time it has left; the other
  half is for its shard. If the store does not answer, the file's changes go to
  the shard as pages and `large_fallbacks` counts it.
- After a failure the node does not ask the store from a checkpoint for 30 s,
  and leaves objects no longer named to the janitor.
- Store trouble is not shard trouble: it does not count toward the shard's
  breaker.
- Opening a tenant that has a large file needs the store.

### Why no object is lost or left over

- An object that may end up unnamed — a checkpoint that failed after its object
  was stored, a copy that is removed, a node taken for dead in between — is a
  row in the registry's `garbage` table before the step that could leave it so.
  A checkpoint that succeeds removes its new object's row and the old object.
- The janitor holds the tenant and makes itself the tenant's writer in its shard
  before it looks whether the shard still names the object. A node taken for
  dead that was about to checkpoint is then refused, so it cannot name an
  object that was just removed. A checkpoint that fails writes its object down
  again, in case the janitor looked before the object existed.
- Data removed from a shard — a copy cleaned up, a tenant deleted, room made for
  a new copy — writes down the objects it names first.
- A move copies the rows, and both copies name the same objects. An archiving
  puts the file whole into the tar and removes its objects; reading back writes
  new ones.

### Upgrading

Shard schema step 3 adds four columns to `vault_files`. A node with large files
on refuses to start while a node of D15 is alive; one with `-large-file-kb 0`
runs beside it. A D15 node refuses a database a D16 node has updated, also a
shard it opens only afterwards, so its requests for that shard's tenants fail
until it is stopped. The old node is not let read the new tables: it would
archive a file whose bulk is an object as if its pages were all of it, and
remove the data from the shard.

## Evidence

Tests in `fleet_files_test.go` run on SQLite files by default and on PostgreSQL
when `MINILOOP_TEST_POSTGRES` holds a database URL. The store is a directory, a
store that fails or hangs on demand, or Garage with `MINILOOP_TEST_S3_URL`.

| Test | What it shows |
|---|---|
| `TestLargeFilesKeepTheirBulkInTheArchiveStore` | A large file is one object and no pages; one changed page writes one page; growing keeps the object; a node reuses its copy; another node reads the file whole; a rewrite makes a new object and removes the old; shrinking goes back to pages; a removed file and a deleted tenant leave no object |
| `TestLargeFilesGoWhereTheirTenantGoes` | After a move, an archiving, a reading back and a removal the store holds exactly the objects the shards name |
| `TestLargeFileIsPagesWhileTheStoreIsAway` | Pages and a count when the store fails; the janitor removes the object that may exist; a node that cannot read the store does not open the tenant and does not take the shard for down; no request within the cooldown |
| `TestLargeFileObjectIsCheckedWhenRead` | A changed object, a missing one, a damaged page over it and a page outside the file keep the tenant closed |
| `TestRemovedDataWritesItsObjectsDownFirst`, `TestRemovalThatStopsLeavesItsObjectsWrittenDown` | Objects are written down before shard data goes; the janitor removes only the row of an object still named |
| `TestStageStoresOnlyImagesOfItsOwn` | A checkpoint stores only what it read itself |
| `TestLargeFileOfANodeTakenForDeadIsNotLeft` | A node held up before and after storing its object while another node takes the tenant, with and without writing it: its checkpoint is refused, its object goes, everything written is there |
| `TestLargeFilesWaitForTheNodesBeforeThem` | Large files on refuse to start beside a D15 node; off they start |
| `TestLargeFileCheckpointDoesNotWaitForAStoreThatHangs` | With a store that hangs: a checkpoint without a deadline waits 5 s, one with 2 s waits under 1.5 s and writes pages; the shard is not taken for down; no request to the store within the cooldown; the janitor, with its deadline, removes both objects once the store answers |
| `TestLargeFileThatChangesInPlaceIsNotStoredAgain` | The same three pages changed four times keep the object; a quarter changed at once makes a new one |

The report (`MINILOOP_LARGE_FILE_REPORT=1 go test -run TestLargeFileReport`) on
PostgreSQL 16 and Garage v1.0.1: one tenant, an 8 MiB SQLite database changed
three rows at a time 24 times and a 2 MiB attachment rewritten 4 times.

| Measure | Large files in the shard | From 1 MiB in the store |
|---|---|---|
| Page rows in the shard | 2,643 | 148 |
| Page bytes | 10.8 MB | 0.60 MB |
| `vault_pages` table | 20.7 MB | 0.31 MB |
| PostgreSQL WAL | 18.9 MB | 0.26 MB |
| Objects stored / held at the end | – | 6 (18.9 MB) / 2 (10.5 MB) |
| Checkpoint of three changed rows, p50 / p99 | 27.7 / 36.1 ms | 26.5 / 31.5 ms |
| Checkpoint after the rewrite, p50 | 40 ms | 47 ms |
| Open on another node | 72 ms | 71 ms |

Each new rule was removed in turn to see a test fail: 38 removals, among them
the sum of the whole file, the object's size and sum, the structure of the
pages over it, the rows written before an object is stored and before shard
data goes, the janitor's hold, seizure and check, the version gate, the store's
time limits, its cooldown in the stage and in the settle, the rebase rule's
count and the archiving's merge of objects. In the run of the final code every
removal made a test fail, none by failing to compile. Getting there took
four runs:

- the first, of 31 removals, left 6 standing, and 5 removals did not compile.
  The six: a checkpoint stores only an image it read itself (reached only in a
  race), the janitor's branch for a tenant gone or cold, the row written before
  a removal purges, the object's size and sum, store trouble kept apart from
  shard trouble, and the check of an object name's form. Each got a test or an
  assertion;
- the second, of those 11, left 3: the image check again, the whole-file sum
  and the structure of the pages over the object. The image check got a test
  that calls the stage directly, and the reading test a damaged page over the
  object and a page outside the file;
- after the process-level run below, 7 removals were added for the store's
  time limits, its cooldown and the rebase count. The removal of the time limit
  on a checkpoint's removals stood: the janitor's test passed a deadline of its
  own. The test now has a checkpoint without a deadline reach a removal that
  hangs;
- the fourth, of 38, is the run of the final code.

Two `miniloop-assistant fleet` processes with separate directories, a
PostgreSQL 16 registry, two more PostgreSQL 16 servers as shards `s1` and `s2`,
and a Garage v1.0.1 bucket, all in containers on one laptop, scripted model,
`-node-ttl 10s -checkpoint 1s -idle 3s -large-file-kb 64`. This is the run of
the final binary, one script from empty databases to the end; step 1 uses the
binary built from `1d55b3c`:

```text
1. a rolling upgrade from the D15 build
   D15 node: readyz ok
   two tenants created and chatted through it: 202 202
   D16 node with large files on: exited; running nodes are of a version that keeps every file in its shard
   D16 node with -large-file-kb 0: readyz ok; dev1 through it while the D15 node holds it: 202
   the D15 node started again: exited; written by a newer version of the fleet
2. large files of 64 KiB or more in the archive store
   dev3: objects agent.db=72KiB+0p assistant.db=196KiB+7p; pages 21 rows 74501 B
   dev3, 40 messages of 1990 letters: {'202': 40} p50 0.023s max 0.067s
   dev3: objects agent.db=2408KiB+0p assistant.db=536KiB+5p; pages 19 rows 66309 B
   A {'objects_stored': 31, 'object_bytes': 11845632, 'objects_removed': 27, 'large_fallbacks': 0}
3. dev3 opened by the other node
   through B: 200 1 200 1; B {'objects_read': 2, 'corrupt': 0}
4. the store is away
   dev3 writes through B: {'202': 10} p50 0.037s max 7.541s
   B {'large_fallbacks': 2, 'objects_stored': 2}; shards home:closed:0 s1:open:2 s2:open:1
   33 s later, when the node asks the store again: {'202': 5} p50 0.046s max 0.117s
   B {'large_fallbacks': 22, 'objects_stored': 4, 'objects_removed': 2}
5. node B is killed with -9 right after a chat
   through A: 503 503 503 503 503 503 200 (about 10 s); last request kept: 2
   A {'takeovers': 1, 'objects_read': 2, 'corrupt': 0}
6. archived and back, then removed
   after 20 s: 3 tenants cold; garbage rows: 0; bucket: Objects: 3
   owner of dev3 returns through B: 200 2 200 11 200 2; B {'thawed': 1, 'objects_stored': 2}
   dev3 deleted: 204; dev1, dev2 deleted while archived: 204 204
   garbage rows: 0; vault rows left: 0 0 ; bucket: Size: 0 B (0 B) Objects: 0
7. the same work with large files off
   40 messages of 1990 letters: {'202': 40} p50 0.022s max 0.035s
   qev3: objects ''; pages 750 rows 3060485 B
processes left: 0; panics or races in the logs: 0
```

Lines are shortened; the full output is kept with the delivery evidence.

What the run shows beyond the tests:

- **The shard keeps almost nothing of a busy tenant.** After 40 long messages
  the tenant's shard rows are 19 pages (66 KB) instead of 750 (3.06 MB), and
  replies are as fast (p50 23 ms against 22 ms).
- **The price is what is sent to the store.** The node wrote 31 objects,
  11.8 MB, for databases that ended at 2.9 MiB: at 64 KiB a growing database
  becomes a new object every quarter it grows. At the default 1 MiB it does so
  far less often, and keeps more pages in the shard.
- **A store that hangs costs one request one wait.** With the bucket's
  container paused, 10 of 10 writes were answered 202, one after 7.5 s; no
  shard was taken for down. The node asked the store again 30 s later.

## Gates

Heavy checks ran one at a time with `GOMAXPROCS=2 GOFLAGS=-p=1
GOMEMLIMIT=1536MiB nice -n 10`, `-parallel=2`, and pytest without xdist. Every
row is a run of the final code.

| Gate | Result |
|---|---|
| `gofmt -l .` in `go/` | no files |
| `go vet ./...` | ok |
| `go test ./... -count=1` | 53 packages ok |
| `go test -race ./... -count=1` | 53 packages ok; the linker warns about `LC_DYSYMTAB` on this Mac, as before |
| `go test -race ./assistant -count=1` of the fleet's tests with `MINILOOP_TEST_POSTGRES` (PostgreSQL 16, each shard a schema) and `MINILOOP_TEST_S3_URL` (Garage v1.0.1) | 69 tests ok. `TestLargeFileReport`, `TestFleetLoad` and `TestVaultReport` are skipped without their own switches |
| 38 removals of new rules, each run against its test | 38 made a test fail |
| `MINILOOP_LARGE_FILE_REPORT=1` report on PostgreSQL 16 and Garage | table above |
| Two `miniloop-assistant fleet` processes, a PostgreSQL registry, two PostgreSQL shards, a Garage bucket and the D15 binary | output above |
| `.venv/bin/python -m pytest -q -p no:xdist` | 2256 passed, 28 skipped; `ci/go-port` unit tests 31 passed |
| `python/tools/verify_scans.py` | 19 guards anchored |
| `python/tools/export_go_contracts.py --check` | 268 files current |
| Archify `deliver` on the system spec | 9 of 9 checks, no errors or warnings; it writes the HTML that is committed |
| README Mermaid, parsed and rendered by Mermaid 11.4.1 | 117 nodes, 221 edges |
| `git diff --check` | clean |
| The change searched for the test credentials and for key patterns | none of the credentials. The one URL with a password is the unchanged example in `go/assistant/README.md` |

Not run:

- the Linux acceptance workflow (`ci/go-port`): only its unit tests;
- Amazon S3 or any store but Garage; separate machines; any load; a real
  model;
- GitHub's own Mermaid renderer;
- `ci/go-port/bootstrap.py` `module_inputs`: `go.mod` and `go.sum` did not
  change;
- `research-site` `npm test`: Node 22.12 is below the required 22.13, as in
  D15. `npm run content` runs in a separate commit;
- `verify_invariants` and `verify_guards`: no Python package module or guarded
  anchor changed.

Known and not fixed: `TestPauseHoldsProcessingAcrossARestartAndResumeCatchesUp`
can fail rarely (see the D13 record); it did not fail in this round's runs.

## Architecture map

The README Mermaid changes three labels:

- `PAShards`: `vault_files` names an object and the pages over it, and a file
  of `-large-file-kb` or more keeps only the pages that differ from its object;
- `PAArchive`: one object per large file of an open tenant, under a name of its
  own, removed when nothing names it;
- the dashed edge `PAFleet` → `PAArchive`: a new object when a quarter changed,
  read before the pages over it, a checkpoint waits at most half its time.

No node or edge was added. The block was parsed and rendered by Mermaid 11.4.1
with its default limits: 117 nodes, 221 edges, 40,557 characters.

The system architecture JSON gives `paShards` and `paArchive` new tags, names
the connection "archive / large files", and adds D16 to the subtitle and the
personal-assistant card. Archify `deliver` passed 9 of 9 checks with no errors
or warnings.

The base layered image was edited with imagegen from the D15 base. That edit is
both the new `docs/mini-loop-layered-architecture.png` and this iteration's
image, `go-assistant-fleet-large-files-2026-11-21.png`. In the bottom band, box
2 (`assistant.db × 每个租户`) now reads "可选：分片 · 冷归档 · 大文件进归档存储" and
keeps the highlight, the strip reads "热：节点缓存 · 温：PostgreSQL 分片 ·
小文件和改动的页 · 冷：归档存储 · 归档租户和大文件主体", and the badge reads
"2026-11-21：按大小分层 · 大文件主体进归档存储".

Three edits were made; the third is kept:

- the first, from the D15 base, drew the badge's 分层 as 分屋;
- the second, from the first with a prompt for the badge alone, fixed the
  badge and blurred the small lines of boxes 4 and 5 further;
- the third, from the D15 base again with the badge text shortened, 分层 spelled
  out and the small lines of boxes 3 to 5 quoted, keeps those lines as they
  were. Enlarged three times, the badge's last character reads more like 档
  than 储; at the image's size the band reads as asked.

The exact prompt of the kept edit is in
`go-assistant-fleet-large-files-2026-11-21.prompt.txt` and
`docs/mini-loop-layered-architecture.prompt.txt`.

The personal-assistant view `docs/architecture/personal-assistant.architecture.json`
pins a revision that must contain `fleet_files.go`, so it moves in a follow-up
commit.

## Deviations from the plan

- The roadmap had no size tier; D15 tiered by age. The user asked for this one.
- The request speaks of a single item ("单条数据"). Rows are already bounded by
  the input limits, so the tier is by file, the unit that grows. That reading is
  the round's choice, not the user's.
- The first version passed its tests and failed the first process-level run.
  With the bucket's container paused (connections hang, they are not refused) a
  checkpoint's upload had no time limit of its own and used up the checkpoint's
  time; the write to the shard then failed on the expired context and counted
  against the shard, which was taken for down, and writes got 503. The same
  run showed the rebase rule counting the same pages twice: 40 messages made 41
  new objects. The test stores failed at once and changed different pages, so
  neither showed. Both are fixed and have tests, and the run above is of the
  final binary.
- The input limit was written as 64 KiB for a chat message in two documents;
  it is 2,000 characters (64 KiB is the request body). Both are corrected.

## Boundaries

- **The tier is by file, not by row.**
- **A tenant with a large file needs the store to open.** Every node needs the
  same store and the same `-large-file-kb`; a node with large files off writes
  such files back to pages the next time they change.
- **A growing file is written whole again** each time it grows a quarter, and a
  database changed here and there each time a quarter of it is over its object.
  There is no other policy; a larger `-large-file-kb` sends less and keeps more
  in the shard.
- **The 30 s cooldown is per node**, not per tenant or object.
- **One request per object**; S3 allows up to 5 GB.
- **Mixed versions**: a D15 node must be stopped soon after a D16 node starts,
  since it cannot open what the new node updated.
- **Not run:** Amazon S3 or a store other than Garage, separate machines, any
  load, a real model.
- Everything D15 listed still holds.
