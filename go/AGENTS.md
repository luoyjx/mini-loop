# Go port instructions

## Scope

These instructions apply to `go/`.

## Type boundaries

- Read `../GO_PORT_PLAN.md` and `../GO_PARITY_MATRIX.md` before extending a
  runtime slice. Record the Python contract and Go evidence for each slice.
- Model messages, tool inputs/results, events, authority, states, and durable
  records with named types and explicit variants. Decode JSON into a supported
  variant before it enters the agent or service layer.
- Do not store `any`, `interface{}`, `map[string]any`, or `json.RawMessage` in
  domain and service structs. A generic type parameter in a boundary decoder
  is acceptable when its caller supplies a concrete type.
- Keep a single execution gate for tool effects. Preserve the Python
  before/guard/permission/execute/after/result order as the gate is ported.

## Validation

- Run `go test ./...`, `go vet ./...`, and `go test -race ./...` before committing
  Go runtime changes.
- Run `.venv/bin/python python/tools/export_go_contracts.py --check` from the
  repository root after changing Python contract exports or Go default-tool types.
- Update the README runtime baseline and architecture source when an
  implementation slice changes topology, authority, persistence, or defaults.
