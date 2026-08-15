"""mini-loop -- a minimal, complete-capability coding agent served concurrently.

The agent is the s01 loop from `learn-claude-code` with the essential harness
mechanisms layered on (tools, planning, subagents, skills, context compaction).
Everything is *instance-based and async* so a single FastAPI process can drive
many independent agents at once.

    Agent  = one async loop + tools + todo + subagent + skills + compaction
    Server = FastAPI + SessionManager (one isolated Agent per session)
"""

from .actions import (
    UNKNOWN_RESULT,
    ActionJournalConflict,
    ActionRecord,
    DurableActionJournal,
    InMemoryActionJournal,
)
from .agent import Agent, TodoManager
from .ast_context import (
    AstContextConfig,
    AstContextProbe,
    AstContextResult,
    AstOutlineAdapter,
    install_ast_context_tools,
)
from .background import (
    BackgroundManager,
    background_injector,
    install_background,
    is_slow_operation,
    should_run_background,
)
from .auth import ANONYMOUS, Authenticator, NullAuth, Principal, TokenAuth, load_auth
from .identity import build_id, posture, runtime_identity
from .audit import Finding, audit, render as render_audit
from .builtins import default_injectors, default_registry, explore_registry, full_registry, worker_registry
from .compaction import (
    Compactor,
    DefaultCompactor,
    estimate_tokens,
    microcompact,
    snip_compact,
    tool_result_budget,
)
from .caching import (
    CachePolicy,
    DefaultCachePolicy,
    NullCachePolicy,
    runtime_facts_injector,
)
from .config import Settings, build_client, load_settings
from .cron import CronScheduler, install_cron
from .harness import Harness
from .manager import SessionManager
from .mcp import InProcessMCP, MCPClient, StdioMCP, install_mcp, register_mcp
from .memory import (
    MemoryStore,
    consolidate_memories,
    install_memory,
    memory_system_builder,
    prepare_memory_context,
    select_relevant_memories,
)
from .permissions import PermissionHook, PermissionRule, default_hooks
from .prompts import default_system_builder, runtime_facts, sections_builder
from .recovery import DefaultRecovery, DirectRecovery
from .registry import (
    Hook,
    Hooks,
    Tool,
    ToolCall,
    ToolCatalogSnapshot,
    ToolContext,
    ToolRegistry,
)
from .run_context import (
    EXPLICIT_HUMAN,
    PEER_AGENT,
    UNTRUSTED,
    WORKFLOW_LAUNCH,
    WORKFLOW_MANAGE,
    RunContext,
)
from .session import AgentSession
from .sandbox import NullSandbox, Sandbox, SeatbeltSandbox, default_sandbox
from .secrets import (
    DEFAULT_SECRET_PATTERNS,
    MASK,
    NullSecretRegistry,
    SecretRegistry,
)
from .skills import LayeredSkillLoader, SkillLoader
from .stuck import (
    DefaultStuckDetector,
    NullStuckDetector,
    StuckDetector,
    StuckSignal,
    StuckThresholds,
    ToolStep,
)
from .storage import (
    SCHEMA_VERSION as STORAGE_SCHEMA_VERSION,
)
from .storage import (
    NullStateStore,
    SessionRecord,
    SQLiteStateStore,
    StateStore,
    StorageSchemaError,
)
from .tasks import TaskStore, install_tasks
from .teams import MessageBus, ProtocolState, install_teams, team_injector
from .token_efficiency import (
    ComponentDescriptor,
    ComponentStage,
    ConciseResponsePolicy,
    ConciseResponsePolicySettings,
    DeterministicLosslessReducer,
    Lossiness,
    MaskedObservation,
    MaskedRawArtifactStore,
    ObservationReducer,
    OptimizationMode,
    OptimizationReceipt,
    OptimizationStatus,
    RequestContext,
    RequestContextOptimizer,
    ResponsePolicy,
    TokenEfficiencyRegistry,
    TokenEfficiencyRuntime,
)
from .token_tools import install_token_efficiency_tools
from .tool_policy import (
    CapabilityRoleToolPolicy,
    RoleToolPolicy,
)
from .tools import CommandResult, Toolset
from .trajectory import SCHEMA_VERSION as TRAJECTORY_SCHEMA_VERSION
from .trajectory import TrajectoryStore
from .user_resources import UserResourceResolver, UserResources
from .worktrees import WorktreeManager, install_worktrees, remove_worktree, worktree_workspace_factory
from .workflows import (
    FreshAgentRunner,
    InMemoryWorkflowStore,
    WorkflowDefinition,
    WorkflowEngine,
    WorkflowNode,
    WorkflowRun,
)
from .workflows.service import WorkflowLaunchResult, WorkflowService
from .workflows.tools import install_workflows

__all__ = [
    # core
    "Agent",
    "AgentSession",
    "SessionManager",
    "Settings",
    "load_settings",
    "build_client",
    "TrajectoryStore",
    "TRAJECTORY_SCHEMA_VERSION",
    "RunContext",
    "EXPLICIT_HUMAN",
    "PEER_AGENT",
    "UNTRUSTED",
    "WORKFLOW_LAUNCH",
    "WORKFLOW_MANAGE",
    "ActionRecord",
    "ActionJournalConflict",
    "InMemoryActionJournal",
    "DurableActionJournal",
    "UNKNOWN_RESULT",
    # extension seams
    "Tool",
    "ToolRegistry",
    "ToolCatalogSnapshot",
    "ToolContext",
    "ToolCall",
    "Hook",
    "Hooks",
    "PermissionHook",
    "PermissionRule",
    "default_hooks",
    "TokenAuth",
    "NullAuth",
    "Principal",
    "load_auth",
    "build_id",
    "runtime_identity",
    "audit",
    "render_audit",
    "Finding",
    "Harness",
    "Compactor",
    "DefaultCompactor",
    "CachePolicy",
    "DefaultCachePolicy",
    "NullCachePolicy",
    "runtime_facts",
    "runtime_facts_injector",
    "StuckDetector",
    "DefaultStuckDetector",
    "NullStuckDetector",
    "StuckSignal",
    "StuckThresholds",
    "ToolStep",
    "Sandbox",
    "SeatbeltSandbox",
    "NullSandbox",
    "default_sandbox",
    "SecretRegistry",
    "NullSecretRegistry",
    "DEFAULT_SECRET_PATTERNS",
    "MASK",
    "StateStore",
    "SQLiteStateStore",
    "NullStateStore",
    "SessionRecord",
    "StorageSchemaError",
    "STORAGE_SCHEMA_VERSION",
    "UserResourceResolver",
    "UserResources",
    "LayeredSkillLoader",
    "SkillLoader",
    "Toolset",
    "CommandResult",
    "TodoManager",
    "default_registry",
    "explore_registry",
    "worker_registry",
    "full_registry",
    "default_injectors",
    "default_system_builder",
    "sections_builder",
    "estimate_tokens",
    "microcompact",
    "snip_compact",
    "tool_result_budget",
    # token-efficient code context and projection stages
    "AstContextConfig",
    "AstContextProbe",
    "AstContextResult",
    "AstOutlineAdapter",
    "install_ast_context_tools",
    "ComponentDescriptor",
    "ComponentStage",
    "Lossiness",
    "OptimizationMode",
    "OptimizationStatus",
    "OptimizationReceipt",
    "MaskedObservation",
    "MaskedRawArtifactStore",
    "ObservationReducer",
    "RequestContext",
    "RequestContextOptimizer",
    "ResponsePolicy",
    "TokenEfficiencyRegistry",
    "TokenEfficiencyRuntime",
    "DeterministicLosslessReducer",
    "ConciseResponsePolicy",
    "ConciseResponsePolicySettings",
    "install_token_efficiency_tools",
    "RoleToolPolicy",
    "CapabilityRoleToolPolicy",
    # error recovery (s11)
    "DefaultRecovery",
    "DirectRecovery",
    # task system (s12)
    "TaskStore",
    "install_tasks",
    # background tasks (s13)
    "BackgroundManager",
    "install_background",
    "background_injector",
    "is_slow_operation",
    "should_run_background",
    # memory (s09)
    "MemoryStore",
    "install_memory",
    "memory_system_builder",
    "select_relevant_memories",
    "prepare_memory_context",
    "consolidate_memories",
    # cron (s14)
    "CronScheduler",
    "install_cron",
    # teams (s15-17)
    "MessageBus",
    "ProtocolState",
    "install_teams",
    "team_injector",
    # worktrees (s18)
    "worktree_workspace_factory",
    "remove_worktree",
    "WorktreeManager",
    "install_worktrees",
    # mcp (s19)
    "MCPClient",
    "InProcessMCP",
    "StdioMCP",
    "install_mcp",
    "register_mcp",
    # experimental Dynamic Workflow MVP
    "WorkflowDefinition",
    "WorkflowNode",
    "WorkflowRun",
    "WorkflowEngine",
    "WorkflowService",
    "WorkflowLaunchResult",
    "FreshAgentRunner",
    "InMemoryWorkflowStore",
    "install_workflows",
]

__version__ = "0.1.0"
