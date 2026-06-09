"""SessionManager -- the registry that makes this a *multi-agent* server.

It owns the shared LLM client, a shared read-only skill index, and a single
global LLM semaphore (so N concurrent agents can't blow the provider rate
limit). Each `create()` mints an isolated workspace + Agent + AgentSession.

The manager itself holds no per-request mutable state, so all of its methods
are safe to call from many concurrent requests.
"""

from __future__ import annotations

import shutil
import uuid
from pathlib import Path

from .agent import Agent
from .config import Settings
from .session import AgentSession
from .skills import SkillLoader


class SessionManager:
    def __init__(self, settings: Settings, client, *, llm_semaphore=None) -> None:
        self.settings = settings
        self.client = client
        self.skills = SkillLoader(settings.skills_dir)
        # Shared across every session: caps simultaneous in-flight LLM calls.
        if llm_semaphore is None:
            import asyncio

            llm_semaphore = asyncio.Semaphore(settings.max_concurrent_llm)
        self.llm_semaphore = llm_semaphore
        self._sessions: dict[str, AgentSession] = {}

    def create(self, *, system: str | None = None, model: str | None = None) -> AgentSession:
        session_id = uuid.uuid4().hex[:12]
        workspace = self.settings.workspace_root / session_id
        workspace.mkdir(parents=True, exist_ok=True)

        session = AgentSession(session_id, workspace, system=system)
        settings = self.settings if model is None else replace_model(self.settings, model)
        session.agent = Agent(
            client=self.client,
            settings=settings,
            workspace=workspace,
            skills=self.skills,
            system=system,
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
