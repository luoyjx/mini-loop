# Go personal-assistant demo D10 — Telegram, Discord and email channels (2026-11-11)

Baseline: `16ee882` (D9 `65ce3e6` and its research reindex, followed by ten
`deepagents` commits that do not touch `go/`).

Request this round implements (2026-11-11): "添加 Telegram、discord、邮件方式的信道"
(add Telegram, Discord and email channels). Earlier constraints still hold:

- secrets are read from the environment ("密钥都通过env 读取就没问题了");
- no new dependency without approval;
- no LaunchAgent install and no push without confirmation.

Scope:

- `go/assistant`:
  - new `remote.go` (the `RemoteChannel` interface, `Bridge` and its store
    methods), `telegram.go`, `discord.go`, `email.go` and `imap.go`, with tests in
    `remote_test.go`, `telegram_test.go`, `discord_test.go` and `email_test.go`;
  - changes to the store schema, `AnswerQuestion` (now shared with remote
    replies), the router's run context, health labels, the app wiring, the state
    view and the page.
- `go/cmd/miniloop-assistant`: the channel flags, the environment secrets and the
  plist refusal, with tests.
- The roadmap, the README baseline, Mermaid, boundary text and repository map.
- The Go README, the demo README, and the parity matrix and plan entries.
- The architecture JSON and HTML, and the imagegen base diagram.

`go/memory`, `go/agent` and `go/mcp` are unchanged. No dependency was added:
Telegram and Discord use `net/http`, and email uses a small IMAP client and
`net/smtp`.

## What changed

- **One conversation, several surfaces.** A `RemoteChannel` talks to exactly one
  person, the owner. Its `Bridge` runs two loops:
  - the receive loop records the owner's messages as chat lines on the page's chat
    thread, tagged with `channel.source`;
  - the delivery loop copies the assistant's and the system's lines (never the
    user's) to the service in channel order.

  A reply to a delivered question line answers that question through the same
  `answerQuestion` the page uses.
- **Durability.**
  - `ReceiveRemote` commits the accepted lines, their dedupe rows in
    `remote_links` (keyed by the service's message ID) and the receive cursor in
    one transaction. A message received twice is skipped.
  - `SentRemote` records the service's message IDs and moves the delivery cursor
    once the service accepted a line, so a crash in between sends that line again.
  - A failed send stops the round so order holds. A line refused for its content
    (HTTP 400, or SMTP 5xx after `DATA`) is skipped and recorded.
  - A first start begins at the service's newest message and the channel's newest
    line.
- **Authority.** Remote chat turns run as explicit human, like typing on the page.
  The run context now names the source (`web-sim`, `telegram`, `discord` or
  `email`). Approval cards are copied with a note that approval happens on the
  page, and no remote action can decide a proposal.
- **Pause.** While paused, messages are still recorded and delivery waits.
- **Telegram** (`-telegram-chat`, `MINILOOP_TELEGRAM_TOKEN`):
  - receives with a `getUpdates` long poll (25 s) and posts with `sendMessage`;
  - accepts only the configured private chat, where the chat ID and the sender ID
    equal the owner's user ID, and skips `/start`;
  - the token sits in the request path, so every error replaces it with `<token>`;
  - a 409 explains that another reader or a webhook holds the bot.
- **Discord** (`-discord-user`, `MINILOOP_DISCORD_TOKEN`):
  - opens the DM once and reads it over REST every 3 s, with no gateway
    WebSocket;
  - skips the bot's own messages and system notices and refuses other authors;
  - 401, 403 and 429 carry hints.
- **Email** (`-email-owner`, `-email-user`, `-email-imap`, `-email-smtp`,
  `-email-authserv`, `MINILOOP_EMAIL_PASSWORD`):
  - reads the assistant's own inbox every 30 s with `EXAMINE`, `UID SEARCH` and
    `BODY.PEEK[]`, so nothing is marked read or moved;
  - accepts a mail only when From is the owner's address and the topmost
    `Authentication-Results` header, from the `-email-authserv` server, shows
    `dmarc=pass` for the owner's domain or `dkim=pass` signed by it;
  - takes the first plain-text part (or the HTML part's text), drops the quoted
    original and the signature, refuses non-UTF-8 bodies and anything over 256 KB;
  - sends one mail per line with a unique `Message-ID`, which the owner's
    `In-Reply-To` quotes;
  - IMAP uses TLS, and SMTP uses TLS on 465 or requires STARTTLS. Plain text is
    allowed only to a loopback host.
- **Page and health.**
  - The header shows a badge per channel, red with the error in the tooltip while
    receiving or sending fails.
  - Remote lines show "来自 Telegram" and the like.
  - Three failures in a row reach the attention list, under labels such as
    "Telegram 收消息".
  - The ledger's new `remote` rows record receipts, refusals and skipped lines.
- **CLI.**
  - The secrets come only from the environment. Their names match the credential
    patterns the MCP child's environment is already scrubbed of.
  - `launchd-plist` refuses the channel flags.
  - The startup line lists `channels=page,…`.

## Evidence

Unit and integration tests (`go test ./assistant ./cmd/miniloop-assistant`):

- the bridge with an in-memory channel:
  - a first start pins the head;
  - refusals are counted in the ledger;
  - a message repeated by the service is skipped;
  - the cursor commits with the batch;
  - user lines are not echoed back;
  - order holds;
  - pause holds delivery, and a channel first started while paused still sends
    the lines written during the pause once the user resumes;
  - a failed send is retried in order;
  - a refused line is skipped;
  - a reply answers an open question, and later replies are plain chat;
- an end-to-end run with the scripted model and two fake channels: a Telegram line
  reaches the chat worker, the memory is written, and the reply reaches both
  channels;
- a fake Bot API: offsets, the private-chat filter, `/start`, replies, splitting,
  400 as a refused line, 409, and token scrubbing on a transport error;
- a fake Discord REST API: the DM opened once, newest-first pages ordered, the
  bot's own and system messages skipped, a stranger refused, 400/403/429 and a bad
  token;
- fake IMAP and SMTP servers:
  - head and `UIDVALIDITY` reset, a size refusal, and no mailbox-changing command;
  - a login failure that quotes the server, not the password;
  - a sent mail's headers and body, and a 554 after `DATA` as a refused line;
- `Authentication-Results`:
  - accepted: DMARC, DKIM by domain and DKIM by identity;
  - refused: SPF only, another domain, a parent domain, failures, another server,
    a forged pass below a failing top header, and a pass hidden in a comment;
- MIME: alternative, base64, quoted-printable, HTML only, attachments, Chinese and
  wrapped English quote introductions, signatures, subject-only replies, GBK
  refused and empty mail;
- the CLI: each channel refused without its secret, the plist refusing channel
  flags, and all three channels built from flags and environment.

A live run against local fakes:

- **Setup**:
  - a stdlib Python script played the Telegram Bot API, Discord REST, an IMAP
    server and an SMTP server on loopback;
  - the built daemon ran with all three channels, the scripted model and a fresh
    data directory.
- **First start**: the 07:00 brief, posted after the channels pinned their heads,
  reached all three; the seed line did not.
- **Telegram chat**: "记住 周五下午不安排会" was answered "好的，记住了：…" on all three
  channels.
- **A question answered remotely**: "同事来问" put the question on all three with
  "（直接回复这条消息即可回答）".
  - A Discord reply "周三上午" answered question #1. The worker drafted the reply,
    and the card reached the channels with "这里不能批准". The proposal stayed
    pending on the page.
  - In a second run, a Telegram reply did the same.
- **Email**: in one poll, a mail whose top header failed DMARC with a forged pass
  below it, a stranger's mail and the owner's "1 分钟后提醒我 喝水" (with a wrapped
  "On … wrote:" quote) arrived.
  - The first two were refused in one ledger row.
  - The third created the reminder task.
- **Pause**: pausing, then sending "今天日程" on Telegram, recorded the line and
  queued its trigger, and sent nothing for 45 s while the reminder came due.
  - On resume, the pause line, the resume line, the calendar answer and the
    reminder went out in order.
- **Restart**: a Discord message sent while the daemon was stopped was answered
  once after restart. Each channel received exactly one new line; nothing earlier
  was resent.
- **Outage**: stopping the fakes turned every badge red. After three failures,
  Telegram and Discord reached the attention list. The Telegram error read
  `bot<token>/getUpdates`, and the token appeared 0 times in the daemon log.
- **Page**: checked at 1360 px and 390 px. At 390 px there is no horizontal
  overflow.

## Gates

Run with `GOMAXPROCS=2`, `GOFLAGS=-p=1`, `-parallel=2`, `nice -n 10`,
`GOMEMLIMIT` set, one heavy run at a time:

- `gofmt -l` (0 files) and `go vet ./...` from `go/`;
- `go test ./...`: 51 packages ok, 2 without tests, 2 failing. Both failures
  are the macOS FSEvents native-observer tests, which time out at
  `native fixture readiness` / `native callback did not enter diagnostic IO` in
  this sandbox: `reload` (`TestNativeEventsActualSourceRecipes` and three more)
  and `cmd/miniloop` (`TestActualCLIRebuildsSourceRetainsPortAndReloadsDotenv`,
  which waits on the same observer). Neither package imports `assistant` or is
  touched by this change. A single rerun of `TestNativeEventsCloseJoinsActiveCallback`
  fails the same way, and `go-reload-required-queue-control-2026-10-11` recorded
  the same timeout;
- `go test -count=3 ./assistant ./cmd/miniloop-assistant`: ok; `-race -count=2`
  on the same packages: ok (only the known Darwin `ld` LC_DYSYMTAB warning);
- `.venv/bin/python -m pytest -q -p no:xdist`: 2256 passed, 28 skipped;
- `verify_scans`: all 19 scanning guards anchored;
- Archify validate and deliver (showcase): 9/9, 0 errors and 0 warnings. The spec
  is `e28996b1…` (215,395 B) and the HTML is `6a036d66…` (957,567 B), regenerated
  after the rebase onto `99c5721`. The page was inspected in a browser;
- `npm run content` then `npm run content:check` in `research-site`: current
  (23 documents), committed separately;
- `git diff --check`: clean.

Not run:

- `go test -race ./...` over the whole module: only `assistant` and
  `cmd/miniloop-assistant` changed, and `reload` already fails here without
  the race detector;
- `export_go_contracts --check`: environment-blocked. It stops in
  `watchfiles_contracts` with `native Source observer did not arm`, the same
  FSEvents limitation; the assistant has no Python contract;
- `research-site` `npm test`: no `node_modules`, and Node 22.12 is below the
  required 22.13;
- `verify_invariants` and `verify_guards`: no Python module or guarded anchor
  changed;
- a Mermaid renderer: the map was checked by script for defined node IDs and
  edges only;
- any real Telegram, Discord or mail account.

## Architecture map

The README Mermaid gains two nodes. `PARemote` is the bridges and `PARemoteSvc`
the external services. Four edges connect them: owner lines into `PAStore`,
assistant lines out of it, the service calls, and failures to `PAHealth`.
`PAChannel` and `PAStore` labels mention the badges, the source tag, the links and
the cursors.

The architecture JSON gains `paRemote` inside the assistant region and
`paRemoteSvc` below it, outside the region, because those services are not on the
Mac. The canvas grows to 6300 px, and three connections are added.

- **Pre-existing failure**: Archify had not passed the committed spec since the
  `deepagents` commits `478ee6a`, `d6761a4` and `72f2306`. Their delivery notes
  say Archify was unavailable there, and the HTML was synced by a one-off script.
  The render failed on one crossing and one label overlap in the Task lane.
- **Fix**: this iteration moved `tsActivityTrace`'s label below its corridor
  (`labelAt [2070, 4590]`) and routed `tsApiTrace` below the lane into the trace
  node's bottom. Upstream `99c5721` (the focused-view split) made the same repair
  in parallel, so the rebase keeps its values (`via [[730,4620],[2275,4620]]`,
  `labelAt [1500, 4635]`). Nodes, labels and meaning are unchanged; only routing
  changed.
- **New edge**: the assistant's new chat edge runs outside the region's left
  border at x=12. The left margin carries the health board's "problems" label, and
  every inner corridor would cross the scheduler's `calendar_list` edge.

The layered base image was edited once with imagegen and serves as the new base
and this iteration's highlight. In the bottom band:

- box 1 became "本机网页 + 远程信道" with "Telegram · Discord · 邮件 · 默认关闭";
- box 2's line became "触发器 · 人物事实 · 远程游标与去重";
- the strip became "只认你本人的账号 · 你的消息进同一个对话，助理的话按顺序同步出去 ·
  审批只在本机网页";
- the badge became "2026-11-11：Telegram / Discord / 邮件信道";
- boxes 1 and 2 carry the orange glow.

Everything above the band, including the Task lane's LangSmith box, was kept. The
exact prompt is `go-assistant-channels-2026-11-11.prompt.txt`, and the image is
`go-assistant-channels-2026-11-11.png`, identical to the new base.

Deviations in the image:

- in box 3's unchanged line, "夜间" reads like "夜问";
- the other small glyphs carried over from the previous base keep their earlier
  softness.

The README Mermaid stays canonical.

## Deviations from the plan

- The roadmap's P4 planned one channel with approval buttons. This round adds three
  channels, as asked, and keeps approval on the loopback page. Someone who controls
  one of those accounts could otherwise send mail as the owner. Remote approval
  waits for P2's channel authentication and a decision on its scope (roadmap §8,
  item 4).
- Discord polls REST instead of holding a gateway connection, trading up to 3 s of
  latency for no WebSocket dependency.
- The base image was not given a sixth box. Box 1 was retitled so the five-box flow
  stays legible and truthful: both channels feed `assistant.db`.

## Boundaries

- Remote messages carry the owner's authority. Someone who controls the owner's
  Telegram or Discord account, or can send mail that the `-email-authserv` server
  marks aligned with the owner's domain, can chat, answer questions and cancel
  tasks, but cannot approve. The email check trusts the topmost header, so
  `-email-authserv` must name a server that adds one to every incoming mail.
- Copied lines leave the machine through Telegram, Discord or the mail provider and
  can quote mail subjects, names and the schedule.
- Delivery is at least once, and a line split into several messages may be resent
  in full.
- No group chats, attachments, voice, webhooks or Discord gateway. The daemon still
  listens only on loopback.
- The managed mail and calendar stay simulated. The email channel is a
  conversation with the owner, not the managed mailbox.
