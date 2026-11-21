# Go personal-assistant demo D13 — fleet nodes on one data directory (2026-11-21)

Baseline: `23947de` (D12 `2e4d061`, its architecture view `fa3a8f6` and the
research reindex `23947de`).

Request this round implements (2026-11-21), after a review of what would break
at scale: "模型这个我们先不考虑，它不是一个问题。其他的我们就按照用户规模上来的时候，然后服务本身是可以
水平扩展或者或者直接扩展，然后达到服务容量扩展的这种类似无状态服务的这种机制或者架构，但是不一定真的是
无状态的。" In English: leave model capacity out. For the rest, the service should
grow by adding instances, the way a stateless service does, without having to
be stateless.

Earlier constraints still hold:

- secrets are read from the environment;
- no new dependency without approval;
- no LaunchAgent install and no push without confirmation.

Scope:

- `go/assistant`:
  - new `fleet_cluster.go` (node membership, placements, takeover, fencing,
    passing requests on) with `fleet_cluster_test.go`;
  - `nodes` and `placements` tables, and placement-aware `remove` and
    `hibernated`, in `fleet_registry.go`;
  - claim before open, heartbeat loop and leave on close in `fleet.go`;
  - passing on of tenant requests, pushes and deletes in `fleet_http.go`;
  - a placement held while a tenant is archived in `fleet_archive.go`;
  - a shared form of the data-directory lock in `lock_unix.go`.
- `go/cmd/miniloop-assistant`: `-advertise` and `-node-ttl`, with a test.
- The roadmap, the README baseline, Mermaid and boundary text, the system
  architecture spec and its HTML, the layered base image, the Go README, the
  demo README, and the parity matrix and plan entries.

## The problem

The fleet of D11 is one process. Its capacity is `-max-active` open assistants
and `-turns` model turns, and when the process stops every owner is cut off.
The review before this round listed what breaks first as owners grow:

| Problem | What D13 does about it |
|---|---|
| One process: a capacity ceiling and a single point of failure | Several identical nodes; a stopped node's tenants are taken over |
| Wake storms: everyone's morning brief is due at once and waits for a slot | Every node's waker draws from the same due list, so the open slots add up |
| Held long polls pin tenants open (stated in the demo README) | Open slots add up across nodes; a full node passes owners on |
| Deploys interrupt every turn at once | Nodes can be restarted one at a time; a closing node's tenants move |
| The registry is one hot SQLite file | Not solved: every node writes to it |
| Tenant data grows without bound, real connectors, forged pushes | Not in this round |
| Model capacity | Left out at the user's request |

## The design

The shape is the virtual-actor one: a tenant is an actor that is activated on
some node when needed, and a directory says where. The fleet already activated
and hibernated tenants; what it lacked was a directory that more than one
process could use.

| Part | How |
|---|---|
| Identical nodes | Each process is a full fleet. It takes any request and runs the same waker, reaper and janitor. A load balancer needs no knowledge of tenants |
| Membership | `nodes` in `fleet.db`: a row per node with its address, open count and a heartbeat every fifth of `-node-ttl` (10 s). A restart is a new node |
| Placement | `placements`: the node that holds a tenant. Claiming is one `INSERT ... ON CONFLICT DO UPDATE ... WHERE` statement that succeeds when the tenant exists and no other live node holds it |
| Lifecycle | Claim, then open. The transaction that records a tenant's sleep removes its placement, and writes nothing if the node no longer holds it. A failed open gives the claim up |
| Routing | A request, push or delete for a tenant another live node holds goes through `httputil.ReverseProxy` to that node. The owner's or operator's token travels along and is checked again. One hop only |
| Overflow | A full node that cannot evict passes an owner's request to the node with the most room |
| Clean stop | `Close` puts tenants to sleep, due at once, and deletes the node's row |
| Crash | After `-node-ttl` without a heartbeat, any node's heartbeat loop makes the dead node's tenants due and removes the node. Running triggers are requeued when the tenant opens, as after a restart |
| Stalled node | Each App already locks its data directory with `flock`. A node taken for dead still holds those locks, so another node's open fails and is retried. When the stalled node beats again and sees a gap longer than the TTL, it closes its tenants |
| Mode exclusion | Cluster nodes hold `daemon.lock` shared; a single fleet or a daemon holds it exclusively |

Two things were considered and left out:

- A network database for the registry and an object store for tenant
  directories would let nodes run on separate disks. Both need a dependency,
  which needs approval.
- A lease per tenant would detect a dead holder per tenant. A heartbeat per node
  is one write for all of a node's tenants, and the directory lock already
  covers the case where the heartbeat is wrong.

## Evidence

`fleet_cluster_test.go` runs two fleets on one directory. Tests call a node's
handler directly, as a load balancer would, and the nodes reach each other over
real HTTP listeners.

| Test | What it shows |
|---|---|
| `TestClusterPassesRequestsToTheHolder` | A state read, a push and a delete sent to the node that does not hold the tenant are answered by the holder. A wrong token is refused before anything is passed on. After the tenant sleeps, the other node opens it |
| `TestClusterTakesOverAStoppedNode` | A node stopped without closing: 503 before the TTL, a fresh heartbeat is not given up, and after the TTL the tenant is due and the other node runs it. A tenant that was not open stays asleep |
| `TestClusterNodeTakenForDeadClosesItsTenants` | A node silent past the TTL but still running: the other node's open fails at the directory lock and gives its claim up. The stalled node closes its tenants on its next heartbeat without overwriting the row, and then the other node opens the tenant |
| `TestClusterOverflowsToANodeWithRoom` | A full node passes a request on, and a request that was already passed on gets 503 |
| `TestClusterWakesEachDueTenantOnce` | Eight due tenants and two wakers running at once: each tenant is open on exactly one node, with no wake failures |
| `TestClusterAndSingleFleetExcludeEachOther` | Both orders are refused, a closed node leaves the registry, and five malformed advertise addresses are refused |

Two real processes were also run on one directory on loopback, and the holder
killed with `SIGKILL`:

```text
open state via http://127.0.0.1:18791: 200
passed on state via http://127.0.0.1:18792: 200
  stats: {'active': 1, 'forwards': 0, 'takeovers': 0} nodes: [('18792', 0, True), ('18791', 1, True)]
  stats: {'active': 0, 'forwards': 1, 'takeovers': 0} nodes: [('18792', 0, True), ('18791', 1, True)]
killed node A
right after the kill state via http://127.0.0.1:18792: 503
after the node TTL state via http://127.0.0.1:18792: 200
  stats: {'active': 1, 'forwards': 2, 'takeovers': 1} nodes: [('18792', 1, True)]
node B exit: 0
```

## Gates

Heavy checks ran one at a time with `GOMAXPROCS=2 GOFLAGS=-p=1
GOMEMLIMIT=1536MiB nice -n 10`, `-parallel=2`, and pytest without xdist.

| Gate | Result |
|---|---|
| `gofmt -l .` in `go/` | no files |
| `go vet ./...` | ok |
| `go test ./... -count=1` | 53 packages ok, 2 without tests; repeated on the final code |
| `go test ./assistant ./cmd/miniloop-assistant -count=3` | `cmd` ok. `assistant` failed once: `TestPauseHoldsProcessingAcrossARestartAndResumeCatchesUp`, in one of its three runs (see below) |
| `go test -race ./assistant ./cmd/miniloop-assistant -count=2` | ok; only the Darwin linker's `LC_DYSYMTAB` warning |
| `go test ./assistant -run Cluster` | ok |
| Two `miniloop-assistant fleet` processes on one directory, holder killed with `SIGKILL` | output above |
| `.venv/bin/python -m pytest -q -p no:xdist` | 2256 passed, 28 skipped |
| `python/tools/verify_scans.py` | 19 guards anchored |
| `python/tools/export_go_contracts.py --check` | 268 files current |
| Archify `deliver` on the system spec | 9 of 9 checks, no errors or warnings; two renders byte-identical |
| README Mermaid node IDs (script) | 123 defined, none missing |
| `git diff --check` | clean |

The one failure: `TestPauseHoldsProcessingAcrossARestartAndResumeCatchesUp`
reported "paused mail trigger is done" once under the full package run. Run
alone it passed 60 of 60 times, and the next full runs passed. `Router.claim`
reads the pause flag and then claims in a separate step, so a dispatch pass that
began before the pause can still claim a trigger enqueued just after it. That
code is the single-owner router, which this round did not change. It is not
fixed here.

Not run:

- `go test -race ./...` over the whole module: only `assistant` and
  `cmd/miniloop-assistant` changed;
- nodes on separate machines, a cluster filesystem, or more than two nodes;
- a load measurement of the cluster: `TestFleetLoad` still measures one fleet;
- `research-site` `npm test`: `node_modules` is not installed and Node 22.12 is
  below the required 22.13. `npm run content` and `content:check` run in a
  separate commit;
- `verify_invariants` and `verify_guards`: no Python module or guarded anchor
  changed;
- a Mermaid renderer: a script checked only that every node ID in an edge is
  defined;
- sampling of the validation processes' RSS: only host memory was read before
  the race run (about 4 GB reclaimable);
- a real model, real connectors, or a deployment behind TLS.

## Architecture map

The README Mermaid gains:

- a `PAFleetPeer` node, "other fleet nodes · only with -advertise";
- a dashed edge from `PAFleet` to it: a request, push or delete passed on once,
  and takeover of a stopped node's tenants after the TTL;
- a line on `PAFleet` naming the `nodes` and `placements` tables.

The system architecture JSON keeps its layout. `paFleet` gets a new tag, and the
subtitle and the personal-assistant card describe the nodes and the D13
baseline. A sentence that said the fleet is a single process was corrected.

The base layered image was edited once with imagegen. That edit is both the new
base and this iteration's highlight. Only the bottom Go assistant band changed:

- box 1's line names the long poll, the push and the nodes, and carries the
  orange glow;
- the strip names three rules: a tenant is open on one node at a time, a
  request goes to the node that holds it, and a stopped node's tenants are
  taken over;
- the badge says `2026-11-21：fleet 多节点 · 加进程扩容`.

The exact prompt is in `go-assistant-fleet-nodes-2026-11-21.prompt.txt` and
`docs/mini-loop-layered-architecture.prompt.txt`.

The first run of that prompt changed a character it was told to keep: box 2's
line read `接交` instead of `提交`. A one-line repair edit did not fix it and
blurred other text, so it was discarded. The same prompt was run once more on
the unchanged base, and that second result is the one kept. Its text was read
from enlarged crops of the bottom band.

The personal-assistant view `docs/architecture/personal-assistant.architecture.json`
pins a revision that must contain `fleet_cluster.go`, so it moves in a
follow-up commit.

## Deviations from the plan

- The roadmap's next phase was P1, hardening the single-Mac daemon. This round
  answers the user's request instead, and P1 moves back again.
- The review before this round proposed admission fairness, wake jitter and
  per-tenant push limits first. The user chose horizontal growth instead, and
  set model capacity aside.
- The nodes share a filesystem instead of talking to a network registry and a
  snapshot store. That keeps the module free of new dependencies and makes the
  mechanism testable here, at the cost of needing one filesystem with locks.

## Boundaries

- **One filesystem with working `flock`.** One host, or a cluster filesystem
  that honours it. SQLite's WAL mode does not work over NFS. Nothing here was
  run across machines.
- **No copy of the data.** A lost disk loses its tenants.
- **The registry is one SQLite file.** Heartbeats, placements, wake times and
  model usage from every node are written to it. Two nodes were run; the write
  rate with many nodes was not measured.
- **Per node, not global:** `-max-active`, `-turns` and the counters in
  `/admin/stats`. `-daily-calls` is per tenant across nodes.
- **A killed node's tenants are unavailable for up to `-node-ttl`.** Requests
  for them get 503 with `Retry-After`. Its unsettled turns repeat their model
  calls; step-level replay is still design only.
- **Nodes trust each other.** They share the data directory and the admin and
  push tokens. A passed-on request carries the caller's token to the other
  node over the advertised URL; use `https` or a private network.
- **Clocks.** Liveness compares a heartbeat with the reader's clock, so nodes
  need clocks that agree to well within `-node-ttl`.
- **Overflow uses a count that is up to one heartbeat old**, so two full nodes
  can pass requests to the same third node, which may then answer 503.
- **A delete of a tenant held by a node taken for dead** removes the row and the
  files at once. The stalled node's late close writes nothing to the registry.
