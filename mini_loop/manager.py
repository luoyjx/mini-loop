"""SessionManager -- the registry that makes this a *multi-agent* server, and
the single place to inject every extension seam for the whole fleet.

Pass any of these once at construction and every session created afterwards
inherits them:

    tool_registry     ToolRegistry  -- cloned per session (your custom tools)
    hooks             Hooks         -- permissions / audit / transforms
    system_builder    f(agent)->str -- prompt assembly
    compactor         Compactor     -- context strategy
    workspace_factory f(id)->Path   -- where/how a session's sandbox is provisioned
    event_sink        f(event)      -- global observability (sync or async)

The manager holds no per-request mutable state, so its methods are safe to call
from many concurrent requests.
"""

from __future__ import annotations

import shutil
import uuid
from collections.abc import Callable
from pathlib import Path

from .agent import Agent
from .config import Settings
from .registry import Hooks, ToolRegistry
from .session import AgentSession
from .skills import SkillLoader


class SessionManager:
    def __init__(
        self,
        settings: Settings,
        client,
        *,
        llm_semaphore=None,
        skills: SkillLoader | None = None,
        tool_registry: ToolRegistry | None = None,
        hooks: Hooks | None = None,
        system_builder: Callable[[Agent], str] | None = None,
        compactor=None,
        workspace_factory: Callable[[str], Path] | None = None,
        event_sink: Callable[[dict], object] | None = None,
    ) -> None:
        self.settings = settings
        self.client = client
        self.skills = skills or SkillLoader(settings.skills_dir)
        if llm_semaphore is None:
            import asyncio

            llm_semaphore = asyncio.Semaphore(settings.max_concurrent_llm)
        self.llm_semaphore = llm_semaphore

        # Extension seams, applied to every session created.
        self.tool_registry = tool_registry
        self.hooks = hooks
        self.system_builder = system_builder
        self.compactor = compactor
        self.workspace_factory = workspace_factory or (lambda sid: self.settings.workspace_root / sid)
        self.event_sink = event_sink

        self._sessions: dict[str, AgentSession] = {}

    def create(self, *, system: str | None = None, model: str | None = None) -> AgentSession:
        session_id = uuid.uuid4().hex[:12]
        workspace = Path(self.workspace_factory(session_id))
        workspace.mkdir(parents=True, exist_ok=True)

        session = AgentSession(session_id, workspace, system=system, event_sink=self.event_sink)
        settings = self.settings if model is None else replace_model(self.settings, model)
        # Clone the template registry so a session can mutate its own tools freely.
        registry = self.tool_registry.clone() if self.tool_registry is not None else None
        session.agent = Agent(
            client=self.client,
            settings=settings,
            workspace=workspace,
            skills=self.skills,
            tools=registry,
            hooks=self.hooks,
            system=system,
            system_builder=self.system_builder,
            compactor=self.compactor,
            emit=session.emit,          # agent events flow straight to the session bus
            llm_semaphore=self.llm_semaphore,
            label="main",
        )
        self._sessions[session_id] = session
        return session

    def get(self, session_id: str) -> AgentSession | None:
        return self._sessions.get(session_id)

    def list(self) -> list[AgentSession]:
        return list(self._sessions.values())

    def delete(self, session_id: str, *, remove_workspace: bool = True) -> bool:
        session = self._sessions.pop(session_id, None)
        if session is None:
            return False
        if remove_workspace:
            shutil.rmtree(session.workspace, ignore_errors=True)
        return True


def replace_model(settings: Settings, model: str) -> Settings:
    """A copy of `settings` with a different model (Settings is frozen)."""
    import dataclasses

    return dataclasses.replace(settings, model=model)
