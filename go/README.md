# Go implementation

This directory is the independent Go port of the Python runtime in `../python/`.
It is under construction and is not yet a runnable agent or HTTP server. The
protocol package fixes the default fake-model transcript shapes as explicit Go
types, including the `bash` tool input. It rejects unsupported tool names at
the JSON boundary instead of admitting an untyped payload into the runtime.
The `agent` package adds an injected, in-memory fake-model turn loop. It has
no production tool gate or persistence yet.

```sh
cd go
go test ./...
go vet ./...
```

See [the plan](../GO_PORT_PLAN.md) and [parity matrix](../GO_PARITY_MATRIX.md)
for remaining work. Do not treat a compiling package as runtime parity.
