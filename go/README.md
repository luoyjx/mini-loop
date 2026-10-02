# Go implementation

This directory is the independent Go port of the Python runtime in `../python/`.
It is under construction and is not yet a runnable agent or HTTP server. The
protocol package fixes the default fake-model transcript shapes as explicit Go
types, completed model replies, usage and stop reasons, and has concrete inputs
for all ten Python default tools. It rejects
unsupported tool names and extra fields at the JSON boundary instead of
admitting an untyped payload into the runtime. The `agent` package adds an
injected, in-memory fake-model turn loop. Every tool call now passes through a
typed gate with an immutable executable catalogue, ordered hooks and permission
modes. Only `bash` is registered by default; other typed tools return an
explicit unknown-tool result until their handlers are ported.
It records typed pause, refusal and unknown-stop events. The fake provider's
input usage is still a placeholder until Go builds the full model request.
Approvals are an in-process callback, with no durable broker, action journal,
secret masking, HTTP server or persistence yet. Python
contract snapshots and fake reply fixtures are generated into `testdata/` by
`../python/tools/export_go_contracts.py`.

```sh
cd go
go test ./...
go vet ./...
```

See [the plan](../GO_PORT_PLAN.md) and [parity matrix](../GO_PARITY_MATRIX.md)
for remaining work. Do not treat a compiling package as runtime parity.
