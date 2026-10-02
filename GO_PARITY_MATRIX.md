# Python to Go parity matrix

Baseline: Python `main` at `ad71e05`, checked on 2026-10-02. This is an
implementation inventory, not evidence that any Go behavior exists. Update
each row with a fixture or test result as the port proceeds.
Python module names in the table are relative to `python/mini_loop/`; test
names are relative to `python/tests/`.

The versioned contract snapshot is under [`go/testdata/`](go/testdata/). Run
`.venv/bin/python python/tools/export_go_contracts.py --check` to detect drift;
the generator captures `default_registry()`, `create_app().openapi()`, and the
SQLite v7 schema from the current Python implementation.

| Slice | Python source of truth | Required Go contract | Go evidence |
|---|---|---|---|
| Session lifecycle, ownership, workspace binding and concurrent isolation | `mini_loop/manager.py`, `session.py`, `auth.py`; `tests/test_concurrent_turns.py`, `test_auth.py`, `test_workspace_binding.py` | Typed owner/session IDs, serialized turns per session, concurrent separate sessions, identical refusal status | Pending |
| Model/tool loop and transcript protocol | `agent.py`, `blocks.py`, `fake_llm.py`; `tests/test_transcript_contract.py`, `test_tool_batch_invariants.py`, `test_provider_fidelity.py` | Text, thinking, tool use and result variants; ordered batches and strict transcript validation | `go/protocol` covers fake bash shape and immediate pairing; `go/agent` runs an in-memory fake turn. Provider variants, gates and full parity pending. |
| Registry and execution boundary | `registry.py`, `builtins.py`, `permissions.py`, `approvals.py`, `actions.py`; `tests/test_tool_pipeline.py`, `test_permission_modes.py`, `test_durable_approvals.py` | Pinned catalogue; before, monotonic guard, permission, execute, after, observer order; denied and unknown-effect outcomes | All ten default inputs have strict Go variants checked against the Python registry snapshot. Catalogue, gates, approvals, and effects remain pending. |
| Default REST and SSE | `server.py`; `tests/test_server.py`, `test_streaming.py`, `test_webui_routes.py` | Health, session CRUD/message/cancel, approval, event stream, transcript, trajectory, UI response shapes | Pending |
| Provider and recovery | `providers.py`, `transport.py`, `recovery.py`; `tests/test_provider_surface.py`, `test_streaming_failures.py`, `test_recovery_backoff.py` | Fake and Anthropic-compatible transports; served-model identity, bounded retries, safe interrupted streams | Pending |
| Context and model budget | `prompts.py`, `skills.py`, `compaction.py`, `caching.py`, `metering.py`, `token_efficiency.py`; relevant `tests/test_*` files | Bounded request construction, skills, cache and compaction, usage accounting | Pending |
| Storage and evidence | `storage.py`, `trajectory.py`, `actions.py`, `session.py`; `tests/test_storage.py`, `test_crash_windows.py`, `test_trajectory_ownership.py` | Optional SQLite state, lease and restart behavior, append-only evidence, ownership checks | Pending |
| Optional orchestration | `background.py`, `cron.py`, `tasks.py`, `teams.py`, `subagents.py`, `worktrees.py`, `workflows/`; matching test families | Each default and authority rule preserved; typed states and bounded queues | Pending |
| Optional user resources and decisions | `user_resources.py`, `memory.py`, `skill_capture.py`, `decisions.py`, `decision_tools.py`; `tests/test_user_resource_lifecycle.py`, `test_decisions.py`, `test_decision_replay.py` | Owner snapshots and typed choice/score/noul requests/results; no implicit execution | Pending |
| Audit and extension seams | `config.py`, `harness.py`, `identity.py`, `audit.py`, `EXTENDING.md`; `tests/test_extension_contracts.py`, `test_effective_posture.py` | Explicit constructor dependencies, truthful default posture and credential-free reporting | Pending |

## HTTP inventory

`python/mini_loop/server.py` currently declares 44 routes. The default Go slice should
start with `/healthz`, `/sessions`, session detail/deletion,
`/sessions/{session_id}/messages`, `/sessions/{session_id}/cancel`,
`/sessions/{session_id}/events`, approvals, transcript, trajectories, `/`, and
`/ui`. Optional route groups cover skills/memory, workflows, cron, tasks/team,
improvement, audit, and benchmark. Every route requires a response/error/event
fixture before its row can be marked covered; route presence alone is weak
evidence.

## Defaults to preserve

The README reports core loop, workspace tools, REST/SSE, caching, stuck
detection, and local trajectory recording as on by default. Comprehensive
features, authentication on loopback, owner resources, token efficiency,
guardian, typed decisions, and workflows are off unless configured. SQLite,
sandbox, and secrets are null boundaries by default. The verified loop is
library-only. Go documentation and tests must state the actual Go posture,
even while it differs from Python.
