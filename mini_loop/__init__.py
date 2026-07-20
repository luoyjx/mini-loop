"""mini-loop -- a minimal, complete-capability coding agent served concurrently.

The agent is the s01 loop from `learn-claude-code` with the essential harness
mechanisms layered on (tools, planning, subagents, skills, context compaction).
Everything is *instance-based and async* so a single FastAPI process can drive
many independent agents at once.

    Agent  = one async loop + tools + todo + subagent + skills + compaction
    Server = FastAPI + SessionManager (one isolated Agent per session)
"""

from .agent import Agent, TodoManager
from .background import (
    BackgroundManager,
    background_injector,
    install_background,
    is_slow_operation,
    should_run_background,
)
from .builtins import default_injectors, default_registry, explore_registry, full_registry, worker_registry
from .compaction import (
    Compactor,
    DefaultCompactor,
    estimate_tokens,
    microcompact,
    snip_compact,
    tool_result_budget,
)
from .config import Settings, build_client, load_settings
from .cron import CronScheduler, install_cron
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
from .prompts import default_system_builder, sections_builder
from .recovery import DefaultRecovery, DirectRecovery
from .registry import Hook, Hooks, Tool, ToolCall, ToolContext, ToolRegistry
from .session import AgentSession
from .skills import SkillLoader
from .tasks import TaskStore, install_tasks
from .teams import MessageBus, ProtocolState, install_teams, team_injector
from .tools import Toolset
from .worktrees import WorktreeManager, install_worktrees, remove_worktree, worktree_workspace_factory

__all__ = [
    # core
    "Agent",
    "AgentSession",
    "SessionManager",
    "Settings",
    "load_settings",
    "build_client",
    # extension seams
    "Tool",
    "ToolRegistry",
    "ToolContext",
    "ToolCall",
    "Hook",
    "Hooks",
    "PermissionHook",
    "PermissionRule",
    "default_hooks",
    "Compactor",
    "DefaultCompactor",
    "SkillLoader",
    "Toolset",
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
]

__version__ = "0.1.0"
