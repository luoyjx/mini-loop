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
and `ask_user`, plus deferred `compress` and bound `task`: all ten default tools
now execute through the same gate, with explicit dependencies and state bound to
one session and owner. Todo, stop, compaction and scoped child events share a
typed, sequenced 200-event backlog.
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
`task` passes the execution-risk gate and uses an explicit `SubagentProvider`.
The default `InProcessSubagents` creates fresh child history, todos and meter;
it inherits model/budgets and injected context/tool services, then uses a fixed
role-specific system prompt. `CapabilityRoleToolPolicy` selects tools only when
their nonempty capability set is entirely allowed. Explore is readonly;
general-purpose and worker are interactive independently of the parent mode.
The default roles omit unclassified tools, including task. Custom selected
built-in handlers are rebound to the child, with the same depth guard.
`RunWithContext` and `DelegateWithContext` accept caller-stamped `RunContext`;
`Run` and `Delegate` default to untrusted provenance. Default child derivation
creates a peer message linked to its parent and drops human actor/approvals.
Custom providers receive a pinned `SubagentParent` environment and parent run
context. Every delegation checks the depth quota before invoking the provider
or emitting start telemetry (defaults: depth 2, child rounds 30). Child events
forward their original label/depth/provenance into the parent backlog. Exhausted
runs prepend a stop marker to partial output instead of reporting a clean summary.
The twelfth Python snapshot covers four role selections, five context snapshots
and three real child loops (read, write and exhaustion). Go tests also cover
custom-provider refusal/cancellation, nested task rebinding, readonly Explore
with an auto parent and shared-provider session isolation under the race detector.
This library slice does not supply authenticated HTTP ownership, shared tool/LLM
limiters, stuck detection or complete lifecycle events. Custom broker/state
inheritance, owner resources and remote provider transport remain open.
`RuntimeConfig.ActionJournal` and `NewJournaledToolGate` optionally bind a typed
journal. Stable action IDs hash session/message/tool-use/name; canonical input
hashes bind final rewritten arguments. Terminal replay does not execute again,
but still crosses current permissions and post hooks. An unknown action asks an
optional `ToolVerifier`: already-applied returns a reconciliation marker,
not-applied permits retry, and undetermined/error/panic preserves unknown.
`write_file` compares the full strict UTF-8 text with universal newlines inside
the bound workspace; Bash cannot verify an opaque shell effect.
`InMemoryActionJournal` keeps all identities and sheds old result text by count
and aggregate Unicode character budgets. `StoredActionJournal` ports Python
transitions over a concrete `ActionStore` interface; its durability depends on
that injected store. The Go SQLite adapter remains pending. Finish changes only
started records, reconcile changes only unknown records, and terminal settlement
is immutable. Like Python, a proven non-landing retry does not rewrite unknown
through finish. Journal settlement runs before observers; cancelled calls settle
without the cancelled context, and settlement faults propagate. Default children
have fresh state and do not inherit the parent journal. No cross-process dispatch
claim or exactly-once side-effect guarantee is supplied by these journals.
The thirteenth Python snapshot compares twelve input/hash/identity cases, memory
and SQLite transitions, Unicode bounds and ten real replay/reconciliation paths.
The Go stored-adapter tests use a typed test backing; they do not prove SQLite
persistence. Approvals remain an in-process callback. Durable broker, secret
masking, HTTP server and session persistence remain pending. Python
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
