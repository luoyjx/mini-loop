# Go public surface inventory

## Current evidence — 2026-10-13

Review baseline: local main `e4c5aff`. This inventory preserves original P0/P1/G0–G7.
It enumerates157 root Python exports,12 README posture rows,18 curriculum labels
covering s01–s20,49 Settings fields,44 OpenAPI operations and3 SDK producers.
Name and composition candidates are lookup pointers; they prove no semantic parity.

Source `__all__` has52 name candidates,99 explicit renamed/composition candidates,
and6 public names without a Go namespace binding identified. Four are standalone
helpers; two are constants. Runtime/internal behavior may already exist.

The64 Source configuration cases include all49 fields; current selected Go gates
and full-feature catalogue publication pass. Default full-feature CLI still refuses
without an injected diagnostics provider; the proposed CPython backend decision
is unapproved. SQLite driver approval does not approve it.

Provider host/request/real HTTP/proxy-TLS checks pass at this baseline. The old
dotted-I discrepancy was repaired by2c71009; literal targets by978b4fd. Earlier
failed snapshots remain historical receipts. Broader IDNA/custom transport,
129 body observations, historical timing, other platforms and release scope remain open.

This audit changes no runtime/default/dependency. Source tests initially hit a
sandbox loopback-bind refusal; the same22 assertions pass with local listening
permitted, without code or fixture changes. No fresh full-suite/coverage claim.

[Machine inventory](docs/audits/go-public-default-inventory-2026-10-13.json).

## Namespace bindings that still need a decision

| Public Python name | Kind | Current gap |
|---|---|---|
| `TRAJECTORY_SCHEMA_VERSION` | constant_or_imported_value | No public constant namespace binding identified; internal semantic values may exist. Do not label this a missing runtime implementation solely from declaration matching. |
| `UNKNOWN_RESULT` | constant_or_imported_value | No public constant namespace binding identified; internal semantic values may exist. Do not label this a missing runtime implementation solely from declaration matching. |
| `tool_result_budget` | function | Runtime adoption exists, but no public standalone Go callable was identified; verify whether an operator counterpart is required and provide typed composition/effect evidence. |
| `select_relevant_memories` | function | Runtime adoption exists, but no public standalone Go callable was identified; verify whether an operator counterpart is required and provide typed composition/effect evidence. |
| `prepare_memory_context` | function | Runtime adoption exists, but no public standalone Go callable was identified; verify whether an operator counterpart is required and provide typed composition/effect evidence. |
| `consolidate_memories` | function | Runtime adoption exists, but no public standalone Go callable was identified; verify whether an operator counterpart is required and provide typed composition/effect evidence. |

Next: inspect the exact Source call signature, owner, effect and callback contracts
for each helper and define a typed Go public seam with required Source comparisons.
Constant value equivalents need exact-value evidence; a state enum/type name is not
a replacement for a public exported constant. Existing runtime integration evidence
does not prove the absent public invocation shape.

## README capability and test index

| Capability | Source default | Python test files | Go test candidates |
|---|---|---|---|
| Async agent loop, workspace tools, session isolation, REST/SSE console | **On** | `test_transcript_contract.py`, `test_two_tenant_isolation.py`, `test_server.py` | `go/agent/runtime_test.go`, `go/httpapi/server_test.go` |
| Prompt caching and stuck detection | **On** | `test_caching.py`, `test_stuck.py` | `go/agent/loop_contract_test.go` |
| Local trajectory recording | **On** | `test_trajectory.py`, `test_trajectory_ownership.py` | `go/launcher/trajectory_test.go` |
| Comprehensive s09–s20 feature bundle | **Off** | `test_features.py` | `go/agent/manager_features_test.go`, `go/launcher/features_test.go` |
| HTTP authentication | **Off on loopback** | `test_auth.py` | `go/httpapi/server_test.go` |
| Sandbox, secret registry, SQLite state | **Null boundaries** | `test_sandbox.py`, `test_storage.py`, `test_tool_pipeline.py` | `go/sandbox/contracts_test.go`, `go/sqlitestate/store_test.go`, `go/agent/gate_test.go` |
| Owner-scoped skills, memory, and personal-skill publication | **Off** | `test_user_resource_lifecycle.py`, `test_memory_tenant_isolation.py` | `go/userresources/resolver_test.go`, `go/launcher/memory_test.go` |
| Token-efficiency pipeline and semantic code tools | **Off** | `test_token_efficiency.py`, `test_ast_context.py` | `go/efficiency/request_test.go`, `go/launcher/ast_test.go` |
| Guardian approval reviewer | **Off** | `test_guardian.py`, `test_guardian_wiring.py` | `go/launcher/guardian_test.go` |
| Typed decisions: choice, score, and noul | **Off, including the full feature bundle** | `test_decisions.py`, `test_decision_replay.py` | `go/decisions/contracts_test.go`, `go/launcher/decisions_test.go` |
| Declarative workflow MVP | **Off, process-local** | `test_workflows.py`, `test_workflow_routes.py` | `go/workflows/engine_test.go`, `go/launcher/workflows_test.go` |
| Verified execute → verify → fold loop | **Library-only** | `test_verified_loop_service.py`, `test_verified_shadow.py` | `go/verifiedloop/service_test.go` |

These are verified paths and candidate tests, not proof of every feature field,
default, lifecycle, public caller or inter-feature combination. Full feature
composition keeps workflows, AST and decisions independent; the diagnostics
backend is the known default CLI gap.

## Root export inventory

| Python export | Source module | Go namespace candidates | Evidence status |
|---|---|---|---|
| `DecisionError` | `decisions` | `decisions.Error` | Rename/composition candidate; semantic review required |
| `DecisionValidationError` | `decisions` | `decisions.ValidationError` | Rename/composition candidate; semantic review required |
| `DecisionProvider` | `decisions` | `decisions.Provider` | Rename/composition candidate; semantic review required |
| `DecisionRequest` | `decisions` | `decisions.Request` | Rename/composition candidate; semantic review required |
| `DecisionResult` | `decisions` | `decisions.Result` | Rename/composition candidate; semantic review required |
| `JevDecisionProvider` | `decisions` | `decisions.JevProvider` | Rename/composition candidate; semantic review required |
| `LLMDecisionProvider` | `decision_llm` | `agent.LLMDecisionProvider` | Name candidate; semantic review required |
| `validate_result` | `decisions` | `decisions.ValidateResult` | Name candidate; semantic review required |
| `install_decisions` | `decision_tools` | `agent.ManagerServices`, `agent.RuntimeConfig`, `launcher.Options` | Rename/composition candidate; semantic review required |
| `Agent` | `agent` | `agent.NewRuntimeSession`, `agent.Session` | Rename/composition candidate; semantic review required |
| `AgentSession` | `session` | `agent.ManagedSession` | Rename/composition candidate; semantic review required |
| `SessionManager` | `manager` | `agent.SessionManager` | Name candidate; semantic review required |
| `Settings` | `config` | `config.Settings` | Name candidate; semantic review required |
| `load_settings` | `config` | `config.Load` | Rename/composition candidate; semantic review required |
| `build_client` | `config` | `launcher.New`, `provider.New` | Rename/composition candidate; semantic review required |
| `TrajectoryStore` | `trajectory` | `trajectory.New`, `trajectory.Store` | Rename/composition candidate; semantic review required |
| `TRAJECTORY_SCHEMA_VERSION` | `trajectory` | — | No namespace binding identified |
| `RunContext` | `run_context` | `agent.RunContext` | Name candidate; semantic review required |
| `EXPLICIT_HUMAN` | `run_context` | `agent.AuthorityExplicitHuman` | Rename/composition candidate; semantic review required |
| `PEER_AGENT` | `run_context` | `agent.AuthorityPeerAgent` | Rename/composition candidate; semantic review required |
| `UNTRUSTED` | `run_context` | `agent.AuthorityUntrusted` | Rename/composition candidate; semantic review required |
| `WORKFLOW_LAUNCH` | `run_context` | `agent.CapabilityWorkflowLaunch` | Rename/composition candidate; semantic review required |
| `WORKFLOW_MANAGE` | `run_context` | `agent.CapabilityWorkflowManage` | Rename/composition candidate; semantic review required |
| `ActionRecord` | `actions` | `agent.ActionRecord` | Name candidate; semantic review required |
| `ActionJournalConflict` | `actions` | `agent.ActionJournalConflict` | Name candidate; semantic review required |
| `InMemoryActionJournal` | `actions` | `agent.InMemoryActionJournal` | Name candidate; semantic review required |
| `DurableActionJournal` | `actions` | `agent.StoredActionJournal` | Rename/composition candidate; semantic review required |
| `UNKNOWN_RESULT` | `actions` | — | No namespace binding identified |
| `Tool` | `registry` | `agent.ToolDefinition` | Rename/composition candidate; semantic review required |
| `ToolRegistry` | `registry` | `agent.ToolCatalog` | Rename/composition candidate; semantic review required |
| `ToolCatalogSnapshot` | `registry` | `agent.ToolCatalogSnapshot` | Name candidate; semantic review required |
| `ToolContext` | `registry` | `agent.ToolAuthority`, `agent.ToolCall` | Rename/composition candidate; semantic review required |
| `ToolCall` | `registry` | `agent.ToolCall` | Name candidate; semantic review required |
| `Hook` | `registry` | `agent.AfterHook`, `agent.BeforeHook`, `agent.GuardHook`, `agent.ResultObserver` | Rename/composition candidate; semantic review required |
| `Hooks` | `registry` | `agent.GateHooks` | Rename/composition candidate; semantic review required |
| `PermissionHook` | `permissions` | `agent.PermissionPolicy`, `agent.ToolGate` | Rename/composition candidate; semantic review required |
| `PermissionRule` | `permissions` | `agent.PermissionRule` | Name candidate; semantic review required |
| `default_hooks` | `permissions` | `agent.DefaultPermissionPolicy`, `agent.NewToolGate` | Rename/composition candidate; semantic review required |
| `TokenAuth` | `auth` | `httpapi.TokenAuth` | Name candidate; semantic review required |
| `NullAuth` | `auth` | `httpapi.NullAuth` | Name candidate; semantic review required |
| `Principal` | `auth` | `httpapi.Principal` | Name candidate; semantic review required |
| `load_auth` | `auth` | `httpapi.AuthFromEnvironment` | Rename/composition candidate; semantic review required |
| `build_id` | `identity` | `launcher.CurrentBuild` | Rename/composition candidate; semantic review required |
| `runtime_identity` | `identity` | `launcher.Inspect` | Rename/composition candidate; semantic review required |
| `audit` | `audit` | `audit.Check` | Rename/composition candidate; semantic review required |
| `render_audit` | `audit` | `audit.Render` | Rename/composition candidate; semantic review required |
| `Finding` | `audit` | `audit.Finding` | Name candidate; semantic review required |
| `Harness` | `harness` | `agent.ManagerServices`, `agent.RuntimeConfig`, `launcher.Options` | Rename/composition candidate; semantic review required |
| `Compactor` | `compaction` | `agent.Compactor` | Name candidate; semantic review required |
| `DefaultCompactor` | `compaction` | `agent.DefaultCompactor` | Name candidate; semantic review required |
| `CachePolicy` | `caching` | `agent.CachePolicy` | Name candidate; semantic review required |
| `DefaultCachePolicy` | `caching` | `agent.DefaultCachePolicy` | Name candidate; semantic review required |
| `NullCachePolicy` | `caching` | `agent.NullCachePolicy` | Name candidate; semantic review required |
| `runtime_facts` | `prompts` | `agent.SystemContext` | Rename/composition candidate; semantic review required |
| `runtime_facts_injector` | `caching` | `agent.MessageInjector` | Rename/composition candidate; semantic review required |
| `StuckDetector` | `stuck` | `agent.StuckDetector` | Name candidate; semantic review required |
| `DefaultStuckDetector` | `stuck` | `agent.DefaultStuckDetector` | Name candidate; semantic review required |
| `NullStuckDetector` | `stuck` | `agent.NullStuckDetector` | Name candidate; semantic review required |
| `StuckSignal` | `stuck` | `agent.StuckSignal` | Name candidate; semantic review required |
| `StuckThresholds` | `stuck` | `agent.StuckThresholds` | Name candidate; semantic review required |
| `ToolStep` | `stuck` | `agent.ToolStep` | Name candidate; semantic review required |
| `Sandbox` | `sandbox` | `shell.Sandbox` | Name candidate; semantic review required |
| `SeatbeltSandbox` | `sandbox` | `sandbox.SeatbeltSandbox` | Rename/composition candidate; semantic review required |
| `NullSandbox` | `sandbox` | `sandbox.NullSandbox` | Rename/composition candidate; semantic review required |
| `default_sandbox` | `sandbox` | `sandbox.NewDefault` | Rename/composition candidate; semantic review required |
| `SecretRegistry` | `secrets` | `secrets.Registry` | Rename/composition candidate; semantic review required |
| `NullSecretRegistry` | `secrets` | `secrets.Null` | Rename/composition candidate; semantic review required |
| `DEFAULT_SECRET_PATTERNS` | `secrets` | `secrets.DefaultPatterns` | Rename/composition candidate; semantic review required |
| `MASK` | `secrets` | `secrets.Mask` | Name candidate; semantic review required |
| `StateStore` | `storage` | `agent.StateStore` | Name candidate; semantic review required |
| `SQLiteStateStore` | `storage` | `sqlitestate.Store` | Rename/composition candidate; semantic review required |
| `NullStateStore` | `storage` | `agent.StateStore` | Rename/composition candidate; semantic review required |
| `SessionRecord` | `storage` | `agent.SessionRecord` | Name candidate; semantic review required |
| `StorageSchemaError` | `storage` | `sqlitestate.SchemaError` | Rename/composition candidate; semantic review required |
| `STORAGE_SCHEMA_VERSION` | `storage` | `sqlitestate.SchemaVersion` | Rename/composition candidate; semantic review required |
| `UserResourceResolver` | `user_resources` | `userresources.Resolver` | Rename/composition candidate; semantic review required |
| `UserResources` | `user_resources` | `userresources.Resources` | Rename/composition candidate; semantic review required |
| `LayeredSkillLoader` | `skills` | `skills.LayeredCatalog` | Rename/composition candidate; semantic review required |
| `SkillLoader` | `skills` | `skills.Catalog` | Rename/composition candidate; semantic review required |
| `Toolset` | `tools` | `shell.Executor`, `workspace.Files` | Rename/composition candidate; semantic review required |
| `CommandResult` | `tools` | `shell.Result` | Rename/composition candidate; semantic review required |
| `TodoManager` | `agent` | `agent.TodoManager` | Name candidate; semantic review required |
| `default_registry` | `builtins` | `agent.NewWorkspaceToolCatalog` | Rename/composition candidate; semantic review required |
| `explore_registry` | `builtins` | `agent.CapabilityRoleToolPolicy` | Rename/composition candidate; semantic review required |
| `worker_registry` | `builtins` | `agent.CapabilityRoleToolPolicy` | Rename/composition candidate; semantic review required |
| `full_registry` | `builtins` | `agent.ManagerConfig`, `agent.ManagerServices` | Rename/composition candidate; semantic review required |
| `default_injectors` | `builtins` | `agent.ManagerServices`, `agent.RuntimeConfig` | Rename/composition candidate; semantic review required |
| `default_system_builder` | `prompts` | `agent.DefaultSystemBuilder` | Name candidate; semantic review required |
| `sections_builder` | `prompts` | `agent.SystemBuilder` | Rename/composition candidate; semantic review required |
| `estimate_tokens` | `compaction` | `agent.EstimateTokens` | Name candidate; semantic review required |
| `microcompact` | `compaction` | `agent.MicroCompact` | Name candidate; semantic review required |
| `snip_compact` | `compaction` | `agent.SnipCompact` | Name candidate; semantic review required |
| `tool_result_budget` | `compaction` | — | No namespace binding identified |
| `AstContextConfig` | `ast_context` | `astcontext.Config` | Rename/composition candidate; semantic review required |
| `AstContextProbe` | `ast_context` | `astcontext.Probe` | Rename/composition candidate; semantic review required |
| `AstContextResult` | `ast_context` | `astcontext.Result` | Rename/composition candidate; semantic review required |
| `AstOutlineAdapter` | `ast_context` | `astcontext.Adapter` | Rename/composition candidate; semantic review required |
| `install_ast_context_tools` | `ast_context` | `agent.ManagerServices`, `agent.RuntimeConfig` | Rename/composition candidate; semantic review required |
| `ComponentDescriptor` | `token_efficiency` | `efficiency.Descriptor` | Rename/composition candidate; semantic review required |
| `ComponentStage` | `token_efficiency` | `efficiency.Stage` | Rename/composition candidate; semantic review required |
| `Lossiness` | `token_efficiency` | `efficiency.Lossiness` | Name candidate; semantic review required |
| `OptimizationMode` | `token_efficiency` | `efficiency.Mode` | Rename/composition candidate; semantic review required |
| `OptimizationStatus` | `token_efficiency` | `efficiency.Status` | Rename/composition candidate; semantic review required |
| `OptimizationReceipt` | `token_efficiency` | `efficiency.Receipt` | Rename/composition candidate; semantic review required |
| `MaskedObservation` | `token_efficiency` | `efficiency.MaskedObservation` | Name candidate; semantic review required |
| `MaskedRawArtifactStore` | `token_efficiency` | `efficiency.RawStore` | Rename/composition candidate; semantic review required |
| `ObservationReducer` | `token_efficiency` | `efficiency.ObservationReducer` | Name candidate; semantic review required |
| `RequestContext` | `token_efficiency` | `efficiency.RequestContext` | Name candidate; semantic review required |
| `RequestContextOptimizer` | `token_efficiency` | `efficiency.RequestOptimizer` | Rename/composition candidate; semantic review required |
| `ResponsePolicy` | `token_efficiency` | `efficiency.ResponsePolicy` | Name candidate; semantic review required |
| `TokenEfficiencyRegistry` | `token_efficiency` | `efficiency.Registry` | Rename/composition candidate; semantic review required |
| `TokenEfficiencyRuntime` | `token_efficiency` | `efficiency.Runtime` | Rename/composition candidate; semantic review required |
| `DeterministicLosslessReducer` | `token_efficiency` | `efficiency.DeterministicLosslessReducer` | Name candidate; semantic review required |
| `ConciseResponsePolicy` | `token_efficiency` | `efficiency.ConciseResponsePlanner` | Rename/composition candidate; semantic review required |
| `ConciseResponsePolicySettings` | `token_efficiency` | `efficiency.StableResponseSettings` | Rename/composition candidate; semantic review required |
| `install_token_efficiency_tools` | `token_tools` | `agent.InstallTokenArtifactTool` | Rename/composition candidate; semantic review required |
| `RoleToolPolicy` | `tool_policy` | `agent.RoleToolPolicy` | Name candidate; semantic review required |
| `CapabilityRoleToolPolicy` | `tool_policy` | `agent.CapabilityRoleToolPolicy` | Name candidate; semantic review required |
| `DefaultRecovery` | `recovery` | `agent.DefaultRecovery` | Name candidate; semantic review required |
| `DirectRecovery` | `recovery` | `agent.DirectRecovery` | Name candidate; semantic review required |
| `TaskStore` | `tasks` | `tasks.Store` | Rename/composition candidate; semantic review required |
| `install_tasks` | `tasks` | `agent.ManagerServices`, `launcher.Options` | Rename/composition candidate; semantic review required |
| `BackgroundManager` | `background` | `background.Manager` | Rename/composition candidate; semantic review required |
| `install_background` | `background` | `agent.ManagerServices`, `launcher.Options` | Rename/composition candidate; semantic review required |
| `background_injector` | `background` | `agent.MessageInjector` | Rename/composition candidate; semantic review required |
| `is_slow_operation` | `background` | `background.IsSlowOperation` | Name candidate; semantic review required |
| `should_run_background` | `background` | `background.ShouldRunBackground` | Name candidate; semantic review required |
| `MemoryStore` | `memory` | `memory.Store` | Rename/composition candidate; semantic review required |
| `install_memory` | `memory` | `agent.ManagerServices`, `launcher.Options` | Rename/composition candidate; semantic review required |
| `memory_system_builder` | `memory` | `agent.SystemBuilder` | Rename/composition candidate; semantic review required |
| `select_relevant_memories` | `memory` | — | No namespace binding identified |
| `prepare_memory_context` | `memory` | — | No namespace binding identified |
| `consolidate_memories` | `memory` | — | No namespace binding identified |
| `CronScheduler` | `cron` | `cron.Scheduler` | Rename/composition candidate; semantic review required |
| `install_cron` | `cron` | `agent.ManagerServices`, `launcher.Options` | Rename/composition candidate; semantic review required |
| `MessageBus` | `teams` | `teams.Bus` | Rename/composition candidate; semantic review required |
| `ProtocolState` | `teams` | `teams.ProtocolState` | Name candidate; semantic review required |
| `install_teams` | `teams` | `agent.ManagerServices`, `launcher.Options` | Rename/composition candidate; semantic review required |
| `team_injector` | `teams` | `agent.MessageInjector` | Rename/composition candidate; semantic review required |
| `worktree_workspace_factory` | `worktrees` | `agent.WorktreeWorkspaceFactory` | Name candidate; semantic review required |
| `remove_worktree` | `worktrees` | `worktrees.Manager.Remove` | Rename/composition candidate; semantic review required |
| `WorktreeManager` | `worktrees` | `worktrees.Manager` | Rename/composition candidate; semantic review required |
| `install_worktrees` | `worktrees` | `agent.ManagerServices`, `launcher.Options` | Rename/composition candidate; semantic review required |
| `MCPClient` | `mcp` | `agent.MCPClient` | Name candidate; semantic review required |
| `InProcessMCP` | `mcp` | `mcp.InProcessClient` | Rename/composition candidate; semantic review required |
| `StdioMCP` | `mcp` | `mcp.StdioClient` | Rename/composition candidate; semantic review required |
| `install_mcp` | `mcp` | `agent.ManagerServices`, `launcher.Options` | Rename/composition candidate; semantic review required |
| `register_mcp` | `mcp` | `agent.MCPRegistry.Register` | Rename/composition candidate; semantic review required |
| `WorkflowDefinition` | `workflows` | `workflows.Definition` | Rename/composition candidate; semantic review required |
| `WorkflowNode` | `workflows` | `workflows.Node` | Rename/composition candidate; semantic review required |
| `WorkflowRun` | `workflows` | `workflows.WorkflowRun` | Name candidate; semantic review required |
| `WorkflowEngine` | `workflows` | `workflows.WorkflowEngine` | Name candidate; semantic review required |
| `WorkflowService` | `workflows.service` | `agent.WorkflowService` | Rename/composition candidate; semantic review required |
| `WorkflowLaunchResult` | `workflows.service` | `agent.WorkflowLaunchResult` | Rename/composition candidate; semantic review required |
| `FreshAgentRunner` | `workflows` | `agent.FreshWorkflowRunner` | Rename/composition candidate; semantic review required |
| `InMemoryWorkflowStore` | `workflows` | `workflows.InMemoryStore` | Rename/composition candidate; semantic review required |
| `install_workflows` | `workflows.tools` | `agent.ManagerServices`, `launcher.Options` | Rename/composition candidate; semantic review required |
