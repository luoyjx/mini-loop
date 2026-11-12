# Go personal-assistant demo D11 — multi-tenant fleet host (2026-11-12)

Baseline: `4d7f982` (D10 `dbaf48a`, its architecture view `4d7f982` and the
research reindex `15cfb45`).

Request this round implements (2026-11-12): "现在的 personal assistant 是不是仅实现了
基本功能而未考虑作为一个云端服务为大量用户提供服务的场景？考虑一下方案并实施，如何承载
大量负载自己如何有效利用计算存储资源降低成本". In English: is the assistant only a
basic implementation that ignores serving many users as a cloud service? Design
and implement a plan that carries heavy load and uses compute and storage
efficiently to cut cost. Earlier constraints still hold:

- secrets are read from the environment;
- no new dependency without approval;
- no LaunchAgent install and no push without confirmation.

The short answer to the question was yes. D0–D10 build a single-owner daemon:

- one data directory per process;
- a page bound to loopback with no authentication;
- eight goroutines and several tickers per owner, running whether or not anything
  is due;
- no limit on model calls.

Running that as a service means one resident process per user.

Scope:

- `go/assistant`:
  - new `standby.go` (what an open assistant is doing and when it next needs to
    run) and `fleet.go`, `fleet_registry.go`, `fleet_budget.go`,
    `fleet_archive.go` and `fleet_http.go`, with tests in `fleet_test.go` and the
    opt-in measurement in `fleet_load_test.go`;
  - an `Admission` seam in `router.go` and `jobs.go`, the shared `routes` and
    `jsonOnly` in `server.go`, relative API paths and the fragment token in
    `web/index.html`, and `skipPassedSlot` in `seed.go` and `nightly.go`.
- `go/cmd/miniloop-assistant`: the `fleet` subcommand and `chooseModel`, shared
  with the daemon, with tests.
- The roadmap, the README baseline, Mermaid, boundary text and repository map.
- The Go README, the demo README, and the parity matrix and plan entries.
- The architecture JSON and HTML, and the imagegen base diagram.

`go/memory`, `go/agent` and `go/mcp` are unchanged. No dependency was added.

## Design

Cost comes first from model tokens, then compute, then storage. The design keeps
each tenant an unchanged single-owner `App`, so every invariant of D0–D10 still
holds per tenant. It adds a host that decides when each `App` is open.

- **Isolation by directory.** Each tenant has `tenants/<shard>/<id>/` with its
  own `assistant.db`, `agent.db`, memory and simulated world. The shard is the
  first hex digit of the ID's SHA-256. No table is shared, so a bug in one
  tenant's queries cannot read another's rows. A tenant can later move to
  another host as one directory.
- **One registry.** `fleet.db` keeps one row per tenant (token hash, state,
  next wake, last visit, activations) and a `usage` row per tenant and day
  (calls, input and output tokens). It is the only state the host itself owns.
- **Sleep by default.** `App.Standby` reports running turns, runnable work and
  the earliest timer: routine slot, task check, event reminder or approved
  effect. When it is quiet, nothing holds the tenant and its owner has been idle
  `-idle`, the reaper closes it and stores the next wake. The waker reopens
  tenants whose wake is due. A closed tenant costs one registry row and its
  files.
- **Model budget at admission.** The router and the preparer ask an `Admission`
  for a slot before they claim. The fleet's gate bounds turns across tenants
  and keeps a share for chat, so background work cannot starve owners, and
  unadmitted work keeps its attempts. The per-tenant meter wraps the provider,
  records every call in the registry and holds a tenant over its daily quota.
- **Storage by age.** Nothing is written until the owner's first request.
  Closing a tenant checkpoints its databases, so no WAL remains. Long-absent
  owners are no longer woken, and later their directory is archived.

Not built, by design or for later: several hosts, object storage, self-service
sign-up and provider push. A sharded deployment would put a router in front of
several fleets, keyed by a consistent hash of the tenant ID. Cold archives would
move to object storage. Neither needs a change to the per-tenant `App`.

## What changed

- **Fleet** (`fleet.go`):
  - opens a tenant on its owner's request or a due wake, with one placeholder
    so concurrent requests share a single open;
  - closes it when quiet and idle, waiting for in-flight requests;
  - evicts the least recently used quiet tenant when an owner's request finds
    the fleet at `-max-active`; the waker never evicts and leaves an eighth of
    the room to owners;
  - on start, makes tenants the last process left open due at once;
  - closes a tenant that, with its owner gone, is neither quiet nor running or
    waiting for a turn for 10 minutes, and does not wake it for 5 minutes;
  - stops waking owners away `-dormant-after` and archives those away
    `-archive-after`;
  - holds `daemon.lock` in its root, so a second fleet (or a daemon) on the same
    directory is refused.
- **Budget** (`fleet_budget.go`): `turnGate` with background and chat channels,
  and `tenantMeter`, which implements `Admission` and wraps the provider. A
  tenant over `-daily-calls` is held until the next day, its owner is told once
  in the channel, and chat over the quota gets 429 with the reopening time.
- **Archive** (`fleet_archive.go`): `tar.gz` at best compression, written to a
  temporary file and renamed. Restore prefers an existing directory. Extraction
  accepts only directories and regular files inside the tenant directory,
  refuses absolute paths, `..` and links, opens files with `O_EXCL` and stops at
  4 GiB.
- **HTTP** (`fleet_http.go`):
  - `/t/{id}/` serves the page without credentials;
  - `/t/{id}/api/…` needs the tenant's bearer token, compared in constant time
    against its SHA-256 hash; an unknown tenant and a wrong token both get 401;
  - a full fleet gets 503 with `Retry-After`; more than 20 requests per second
    (bursts of 40) per tenant get 429;
  - `/admin/` (create, list, show, delete, stats) needs
    `MINILOOP_FLEET_ADMIN_TOKEN`; `/healthz` answers without a credential.
- **Shared routes** (`server.go`): the daemon's `Handler` and the fleet use the
  same `routes` and `jsonOnly` (JSON content type for POST, no cross-site POST,
  capped body). Only the daemon keeps the loopback guard.
- **Page** (`web/index.html`): API paths are relative, so the same page works at
  `/` and `/t/{id}/`. A `#token=` fragment moves into the tab's
  `sessionStorage` and leaves the address bar. Polling stops while the tab is
  hidden, and a 401 is shown once.
- **Fresh installs** (`seed.go`, `nightly.go`): a routine slot that passed
  before the install is marked taken. The load test found that every tenant
  created in the evening ran the morning brief and the nightly consolidation at
  once: 9 model calls per tenant before any owner spoke. This applies to the
  daemon too.
- **CLI** (`cmd/miniloop-assistant/fleet.go`): the `fleet` subcommand with
  `-max-active`, `-idle`, `-turns`, `-chat-turns`, `-tenant-workers`,
  `-daily-calls`, `-dormant-after`, `-archive-after`, `-sweep`, `-tls-cert`,
  `-tls-key` and `-behind-tls-proxy`. It refuses a short admin token, a lone
  certificate or key, and plain HTTP on a non-loopback address. The server sets
  header, read, write and idle timeouts and a 64 KB header cap.

## Evidence

Unit and integration tests (`go test ./assistant ./cmd/miniloop-assistant`):

- a registered tenant has no directory; the first request provisions it; the
  reaper keeps it for `IdleTTL` and then closes it with its next wake at the
  07:00 brief and no WAL files;
- the waker opens a tenant for its routine, and the brief runs (not as a
  catch-up); the next wake moves to the following timer;
- a tenant left active by a stopped process is due at once and reopened, a
  second fleet on an open fleet's directory is refused, and a tenant that was
  asleep stays asleep;
- at `MaxActive` an owner's request evicts the least recently used quiet tenant;
- a tenant over its daily quota is held: its chat gets 429, queued work stays
  queued, the owner is told once, and it sleeps until its next timer;
- with every background slot taken, chat still gets a turn;
- a tenant whose work never becomes claimable is closed after `StallAfter`
  and not woken before the backoff;
- dormant tenants leave the wake index but keep their files; absent tenants are
  archived into a file far smaller than the directory and restored, with their
  history, on the next request; an existing directory wins over its archive;
- archive extraction refuses `..`, absolute paths and a symbolic link, and writes
  nothing outside;
- every `/t/{id}/api/` request needs that tenant's token, and the admin API its
  own; a wrong or missing token gets 401; a duplicate ID gets 409 and an invalid
  one 400; a deleted tenant's token stops working; without an admin token there
  is no admin API;
- the token bucket refills; the turn gate keeps chat slots;
- `Standby.Quiet` and `Standby.Next` over running, runnable, held and timer
  states;
- a fresh install at 23:47 runs nothing and next wakes at 07:00; the next day's
  brief is not a catch-up;
- the CLI refuses an unsafe start (short token, lone certificate, plain HTTP on
  a public address) and serves a created tenant end to end until cancelled.

The opt-in measurement `MINILOOP_FLEET_LOAD=1000 go test -run TestFleetLoad -v
./assistant` (scripted model, `GOMAXPROCS=2`, an Apple silicon laptop):

- **Resident baseline**: 50 single-owner assistants kept open, as 50 daemons
  would. Each took 8 goroutines, about 1.1 MB RSS, 808 KB on disk with WAL, and
  1.0 ms of CPU per second while idle.
- **Fleet**: 1000 tenants, `MaxActive 64`, `IdleTTL 500ms`, 32 clients. Each
  tenant's state was read, it chatted once, and its owner left.
  - 9740 requests at 375 per second; no 503.
  - Waking a sleeping tenant under that load: p50 351 ms, p99 950 ms. A warm read:
    p50 0.5 ms, p99 92 ms. A chat answered: p50 427 ms, p99 814 ms.
  - At most 64 open and 8 turns in use; 196 evictions; 0 open afterwards.
  - 2000 model calls, 2 per tenant, both for its chat turn.
  - With all 1000 asleep: 2.9 ms of CPU per second in total, 4 goroutines above
    the start, 312 KB per tenant on disk, 208 KB registry.
  - Archived: 15 KB per tenant. Restoring and opening one: p50 5 ms.
  - A registered tenant that was never used: 0 bytes.
  - RSS stayed 87 MB above the start after all slept: the Go heap is not
    returned to the system at once.
- An earlier 500-tenant run, before the fresh-install fix, made 4500 model calls,
  9 per tenant.
- An open tenant holds about 13 file descriptors, and a sleeping one none. A
  fleet's `-max-active` times 13 must fit under `RLIMIT_NOFILE`; Go raises the
  soft limit to the hard limit on start.

Compared with keeping 1000 daemons resident, the idle CPU falls from about one
core to 0.3% of one, and memory and file descriptors scale with the open tenants,
not the registered ones.

A live run in the browser:

- **Setup**: the built binary ran `fleet` on loopback with the scripted model
  and a fresh data directory; a tenant was created through the admin API.
- **Link**: opening `/t/<id>/#token=…` removed the token from the address bar and
  kept it in `sessionStorage`; the page loaded the tenant's state.
- **Chat**: a calendar question was answered ("今明两天：…").
- **Fresh install**: created at 23:54, the tenant ran no catch-up brief.
- **Sleep**: after the owner left, `/admin/stats` showed `active 0`,
  `hibernations 1` and `model_calls 2`. The tenant's next wake was
  2026-11-13 07:00, and its directory held no WAL files.
- **Daemon**: the single-owner daemon's page at `/` still answered "我的经理是谁".
- The only console error on either page was a missing favicon.

## Gates

Run with `GOMAXPROCS=2`, `GOFLAGS=-p=1`, `-parallel=2`, `nice -n 10`,
`GOMEMLIMIT=1536MiB`, one heavy run at a time:

- `gofmt -l` (0 files) and `go vet ./...` from `go/`;
- `go test ./...`: 53 packages ok, 2 without tests. The FSEvents observer tests
  that timed out in the previous round passed this time;
- `go test -count=3 ./assistant ./cmd/miniloop-assistant`: ok;
- `go test -race -count=2` on the same packages: ok, with only the known Darwin
  `ld` LC_DYSYMTAB warning. The process tree peaked at 301 MB RSS against a
  4 GiB budget;
- `.venv/bin/python -m pytest -q -p no:xdist`: 2256 passed, 28 skipped;
- `verify_scans`: all 19 scanning guards anchored;
- `export_go_contracts.py --check`: all 268 files current;
- Archify validate (showcase) on the system spec: 9 checks, 0 errors and 0
  warnings. A fresh render matches the committed HTML. The spec is `664b32a9…`
  (218,010 B) and the HTML is `b6ba6df8…` (963,906 B). The page was inspected in
  a browser;
- the personal-assistant view: see the follow-up commit, because Archify checks
  its source evidence at a pinned revision that must contain `fleet.go`;
- `npm run content` then `npm run content:check` in `research-site`, committed
  separately;
- `git diff --check`: clean.

Not run:

- `go test -race ./...` over the whole module: only `assistant` and
  `cmd/miniloop-assistant` changed;
- `research-site` `npm test`: Node 22.12 is below the required 22.13;
- `verify_invariants` and `verify_guards`: no Python module or guarded anchor
  changed;
- a Mermaid renderer: a script checked only that every node ID and edge in the
  map is defined;
- the 1000-tenant load test as a gate: it is opt-in and was run by hand, as
  recorded above;
- a real model, a real mailbox, or a deployment behind TLS.

## Architecture map

The README Mermaid gains a `PAFleet` node with four edges:

- the caller's explicit fleet command with the admin token;
- the fleet to `PAChannel`, for a tenant's API behind its token;
- the fleet to `PAStore`, for opening, reading `Standby` and closing;
- `PARouter` to the fleet, dashed, for the turn slot and the quota hold.

`PAChannel`'s label now says the daemon is loopback without authentication, while
the fleet puts one tenant's routes behind its token, and that the page uses
relative paths and pauses in a hidden tab. `PAScheduler`'s label says slots before
an install are not caught up.

The architecture JSON gains `paFleet` inside the assistant region, above the
router, with two connections:

- `paFleetSleep`, from the fleet into the store's right side;
- `paFleetGate`, dashed, from the router up to the fleet.

Every inner corridor from the lower rows to the store crosses the scheduler's
`calendar_list` edge, and the store's top and left are taken, so the node sits
above the top row. The region label now ends "own Mac or opt-in fleet host", and
the personal-assistant card gains a fleet item and a new baseline. Validation:
9/9, 0 errors and 0 warnings.

The layered base image was edited once with imagegen and serves as the new base
and this iteration's highlight. Only the bottom Go assistant band changed:

- its title now names the fleet as an option, next to the Mac;
- the first three boxes say how it works: a token per tenant, the registry's
  next wake and archive, and the global turn gate and daily quota;
- they carry the orange glow;
- the strip at the bottom names three rules: a directory and database per
  tenant, sleep until something is due, and a gate and quota on model calls;
- the badge says `2026-11-12：多租户 fleet，闲时睡下`.

The bands above it are unchanged. The exact prompt is in
`go-assistant-fleet-2026-11-12.prompt.txt` and
`docs/mini-loop-layered-architecture.prompt.txt`.

Two glyphs came out imperfect. `远程信道` reads close to `远程偏道`, and `归档` is
soft. A narrow follow-up edit asked to redraw only those two runs. It redrew the
whole band and blurred more characters, such as `全周路白阀门` and `层户`, so it
was rejected and the first edit kept. The text is in the README and this audit.

## Deviations from the plan

- The roadmap's next phase was P1, hardening the single-Mac daemon. This round
  answers the user's question about serving many users instead, and builds the
  single-host part of that design. Several hosts and object storage stay design
  only.
- The fleet does not add per-tenant processes or containers. Tenants share one
  process and are separated by directory and database. A process per tenant would
  cost the resident memory the fleet removes.
- The quota counts calls rather than tokens, because the scripted model reports
  no usage. The registry records tokens when a provider reports them.

## Boundaries

- One process on one machine. Every tenant shares the fleet's time zone and
  model. Fleet tenants have no remote channels or embeddings, and their mail and
  calendar are the simulated world, in process.
- A tenant token has the authority of that tenant's page, including approval.
  There is no rotation: the admin deletes the tenant. The link carries the token
  in its fragment, so whoever has the link has the token.
- The fleet does not rate-limit unauthenticated requests, and a tenant's limiter
  restarts when it reopens. Put the fleet behind a proxy that limits by address.
- A sleeping tenant wakes only for timers it recorded. A real mailbox would need
  `-sweep` or provider push. The page still polls full state every 1.5 s while
  visible.
- The load numbers come from the scripted model on one laptop. A real model's
  latency would hold turns longer, which the gate bounds but does not hide.
