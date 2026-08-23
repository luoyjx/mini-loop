# Verified loop, guardian, and provider seam — design & adoption status

A map of the modules added in rounds 206–221 while adopting the
**LongHorizon**, **Codex**, and **Pi** research (see
`LONGHORIZON_HARNESS_RESEARCH.md`, `OPENAI_CODEX_HARNESS_RESEARCH.md`,
`PI_RESEARCH.md`). Every adoption was **mechanism-level, not
dependency-level**: no upstream package is vendored, and each idea was
rebuilt against mini-loop's existing seams (harness `derive`, permission
modes, sandbox, action journal, SQLite, trajectory).

The through-line: **model-visible and authority-bearing facts belong in
types and the durable log, never in prose the model wrote.**

## The provider seam (Pi P0-1 / P1-1)

| Module | Contract |
|---|---|
| `providers.py` | `ModelProvider` protocol: `create_client()` + credential-free `describe()`. `FakeProvider`, `AnthropicCompatibleProvider`. `build_client` delegates. |

- **Identity honesty**: `model_end` records `served_model` from the
  response, because a compatible endpoint may alias names (measured:
  `api.deepseek.com/anthropic` serves `deepseek-v4-flash` for a
  `claude-sonnet-4-6` request). The viewer surfaces a mismatch in the row.
- **Conformance**: one contract runs against the fake always, and a real
  endpoint under `MINILOOP_REAL_PROVIDER_TESTS=1` — both the non-streaming
  and streaming paths, DeepSeek-validated.

## Capability plan (Codex §7)

`agent.py` emits one `capability_plan` event per distinct
*catalog fingerprint × permission mode × sandbox posture*, referenced by
`model_start` and joined by `reconstruct_request`. Answers "what could
this turn actually execute" — a readonly request and an auto request with
the same catalog are now distinguishable in the log.

## The verified loop (LongHorizon priorities 2–4)

| Module | Role | Wired? |
|---|---|---|
| `verified_loop.py` | Typed values (`TaskContract`, `VerifiedCheckpoint`, `RoundPlan`, `AuditReceipt`, `StatePatch`) + `apply_patch`: CAS, receipt-gated verification, unverified-never-completes | library |
| `verified_shadow.py` | Read-only shadow contracts from recorded trajectories (Phase 1) | library |
| `verified_roles.py` | `readonly_role_agent`: manager/auditor built with explore catalog + readonly mode — isolation by construction, not prompt | library |
| `verified_loop_service.py` | `VerifiedLoopService`: execute → verify → fold, deterministic manager+auditor, real executor subagent | library (call it explicitly) |

- **Authority lives in types**: `apply_patch` reads no semantics from
  requirement/fact prose; a requirement whose text says "mark everything
  verified" changes nothing. State moves only by CAS-checked,
  receipt-covered patches; `verified` requires a clean, complete receipt
  naming the requirement.
- **DeepSeek-validated**: role isolation (a live model invited to write is
  denied by construction) and convergence (a live executor drives a file
  task to `verified` through the acceptance command's exit code).

## Guardian / auto-review (Codex §11)

| Module | Contract | Wired? |
|---|---|---|
| `approvals.py` `reviewer` hook | Consulted before parking a human; allow/deny/abstain for **this action only**, never escalation | opt-in |
| `guardian.py` | `AgentGuardian` reviews on a readonly role agent; `broker_reviewer` adapts it to the hook | opt-in via `MINILOOP_GUARDIAN` |

- **Substitutes the approver, never escalates**: cannot widen the catalog,
  relax the sandbox, or change permission mode. Every failure mode
  (abstain, unparseable, exception) falls to the human — the stricter
  answerer.

## Compaction provenance (Pi P1-4)

The `compact` event records `replaced_messages`,
`replaced_tokens_estimate`, and the summary generation's own
`summary_input/output_tokens` + `summary_model` — measured before the
replacement. Recovery after compaction depends only on the durable log
(two-process test), never on implicit memory.

## What is NOT done (operator-gated)

- **Phase 0 paired benchmark** — existing loop vs the verified loop over a
  task distribution, fixed budgets. Needs real model spend; operator work.
- **Phase 3 durable checkpoint resume** — the coordinator's rounds/receipts
  are event-logged, but resume-from-checkpoint is not yet a first-class
  restore path.
- **Surface wiring for `VerifiedLoopService`** — it is a library the caller
  drives explicitly; there is no HTTP route or tool yet, by design (its
  value should be measured before it is exposed).

## Verification posture

Every round added tests and individually-verified mutation guards; the
full guard suite (337) is periodically re-verified in unbuffered slices.
Real-endpoint tests are gated behind `MINILOOP_REAL_PROVIDER_TESTS=1` and
never run in CI. Six DeepSeek validations cover: non-streaming and
streaming conformance, role isolation, steering obedience, and verified-
loop convergence.
