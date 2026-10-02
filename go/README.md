# Go implementation

This directory is the independent Go port of the Python runtime in `../python/`.
It is under construction and has no standalone agent CLI or HTTP server yet. The
protocol package fixes the default fake-model transcript shapes as explicit Go
types, completed model replies, usage and stop reasons, and has concrete inputs
for all ten Python default tools. It rejects
unsupported tool names and extra fields at the JSON boundary instead of
admitting an untyped payload into the runtime. The `agent` package adds an
injected, in-memory fake-model turn loop. Every tool call now passes through a
typed gate with an immutable executable catalogue, ordered hooks and permission
modes. `NewSession` registers injected Bash only. `NewWorkspaceSession` binds a
workspace and registers Bash, `read_file`, `write_file`, `edit_file` and `glob` through
the same gate. `NewRuntimeSession(RuntimeConfig)` adds `TodoWrite`, `load_skill`
and `ask_user`, plus deferred `compress`, with explicit dependencies and state
bound to one session and owner. Todo, stop and compaction events share a typed,
sequenced 200-event backlog. `task` still returns an explicit unknown-tool result.
The `workspace` package preserves bounded Unicode reads, line pagination,
unique exact edits and atomic replacements. Every file path is checked at
execution; write paths also undergo permission checks. Thirty-four Python cases
compare the output and resulting file hashes, including failure cases. Glob
preserves Python recursive and filename matching, hidden-file filtering,
workspace result filtering and Unicode budgets before deduplication and sorting.
A separate snapshot covers 38 searches and 182 filename patterns; cancellation,
readonly session execution and deep search under a low descriptor limit are
tested in Go. Enumeration uses native directory order and is not a filesystem
snapshot.
The deployment skill catalogue discovers sorted sources, preserves first-wins
names, bounds descriptions and bodies, reports refusal/omission counts and
rechecks the complete normalized source digest before serving a body. Streaming
reads retain bounded prefixes while hashing the full source. Nineteen Python
catalogue scenarios cover outputs, hashes and diagnostic counts. Ten todo
updates and four question variants match real Python outputs; tests also cover
readonly calls, cancellation, session binding and concurrent skill reads.
`RuntimeConfig.Skills` is explicit; nil supplies an empty catalogue. Descriptions
enter the actual model request with fitted, immutable tool schemas. User
skill layering is pending. A nil question surface reports unavailability;
an injected `Questioner` returns answered text or an unanswered variant, and is
not a durable broker. Invalid-name values in diagnostics retain at most 2,048 characters
and mark truncation, unlike Python's potentially unbounded diagnostic text.
`Provider.Complete` now consumes `protocol.ModelRequest`: model, output budget,
detached messages, optional system text, recursively typed tool schemas and a
local request purpose. The default system builder names only fitted tools and
includes skill descriptions; changed todo/pressure facts enter the message
stream, leaving the stable prefix alone. Schema fitting and fingerprints match
Python's JSON character budgets and canonical serialization. The fake provider
counts messages, system and schemas with Python's ASCII/wide-character model.
The token meter includes cached input, learns same-envelope growth and sees
signed shrinkage. Summary usage belongs to its receipt, not the live anchor.
Workspace-backed sessions run result spill, pair-safe snip, consumed-result
micro compaction and transcript-plus-model summary in that order. `compress`
passes the write-risk gate and runs after the entire batch has results. Failures,
empty summaries and cancellation preserve paired history. Archive/spill paths
use the workspace resolver and atomic replacement. Automatic compaction can
write artifacts under readonly tool mode, like Python's ordinary agent;
read-only workers must inject `InMemoryCompactor`. The Bash-only constructor,
which has no workspace, uses that in-memory strategy by default.
The context snapshot compares six catalogues, three wire/token cases, four
cheap-compaction histories, six meter steps, spill output and summary artifacts
against Python. Cache annotations, masking, team/plan/memory prompt sections,
and provider recovery remain pending. Compaction files persist, but cannot
restore a Go session on their own.
Approvals are an in-process callback, with no durable broker, action journal,
secret masking, HTTP server or session persistence yet. Python
contract snapshots and fake reply fixtures are generated into `testdata/` by
`../python/tools/export_go_contracts.py`.

```sh
cd go
go test ./...
go vet ./...
go test -race ./...
```

See [the plan](../GO_PORT_PLAN.md) and [parity matrix](../GO_PARITY_MATRIX.md)
for remaining work. Do not treat a compiling package as runtime parity.
