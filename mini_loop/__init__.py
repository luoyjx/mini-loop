"""mini-loop -- a minimal, complete-capability coding agent served concurrently.

The agent is the s01 loop from `learn-claude-code` with the essential harness
mechanisms layered on (tools, planning, subagents, skills, context compaction).
Everything is *instance-based and async* so a single FastAPI process can drive
many independent agents at once.

    Agent  = one async loop + tools + todo + subagent + skills + compaction
    Server = FastAPI + SessionManager (one isolated Agent per session)
"""

from .agent import Agent
from .config import Settings, load_settings, build_client
from .session import AgentSession
from .manager import SessionManager

__all__ = [
    "Agent",
    "AgentSession",
    "SessionManager",
    "Settings",
    "load_settings",
    "build_client",
]

__version__ = "0.1.0"
