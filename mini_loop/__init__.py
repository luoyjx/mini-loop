"""mini-loop -- a minimal, complete-capability coding agent served concurrently.

The agent is the s01 loop from `learn-claude-code` with the essential harness
mechanisms layered on (tools, planning, subagents, skills, context compaction).
Everything is *instance-based and async* so a single FastAPI process can drive
many independent agents at once.

    Agent  = one async loop + tools + todo + subagent + skills + compaction
    Server = FastAPI + SessionManager (one isolated Agent per session)
"""

from .agent import Agent, TodoManager
from .builtins import default_registry, explore_registry, worker_registry
from .compaction import Compactor, DefaultCompactor, estimate_tokens, microcompact
from .config import Settings, build_client, load_settings
from .manager import SessionManager
from .prompts import default_system_builder, sections_builder
from .registry import Hook, Hooks, Tool, ToolCall, ToolContext, ToolRegistry
from .session import AgentSession
from .skills import SkillLoader
from .tools import Toolset

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
    "Compactor",
    "DefaultCompactor",
    "SkillLoader",
    "Toolset",
    "TodoManager",
    "default_registry",
    "explore_registry",
    "worker_registry",
    "default_system_builder",
    "sections_builder",
    "estimate_tokens",
    "microcompact",
]

__version__ = "0.1.0"
