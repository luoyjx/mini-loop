"""SessionManager -- the multi-agent registry and the place to inject every
extension seam for the whole fleet, now including the cross-session services
(message bus, cron scheduler, teammate spawning).

Inject once at construction; every session inherits it:

    tool_registry / hooks / system_builder / compactor / recovery / injectors
    workspace_factory / event_sink

Flip `enable_features=True` (or env MINILOOP_FEATURES) to turn on the full tool
set (tasks, background, memory, cron, teams) and the background injector.
"""

from __future__ import annotations

import dataclasses
import shutil
import uuid
from collections.abc import Callable
from pathlib import Path

from .agent import Agent
from .background import background_injector
from .builtins import default_injectors, full_registry
from .config import Settings
from .cron import CronScheduler
from .registry import Hooks, ToolRegistry
from .session import AgentSession
from .skills import SkillLoader
from .teams import MessageBus


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
        recovery=None,
        injectors: list | None = None,
        workspace_factory: Callable[[str], Path] | None = None,
        event_sink: Callable[[dict], object] | None = None,
        enable_features: bool = False,
        mcp_servers: dict | None = None,
    ) -> None:
        self.settings = settings
        self.client = client
        self.skills = skills or SkillLoader(settings.skills_dir)
        if llm_semaphore is None:
            import asyncio

            llm_semaphore = asyncio.Semaphore(settings.max_concurrent_llm)
        self.llm_semaphore = llm_semaphore

        self.hooks = hooks
        self.system_builder = system_builder
        self.compactor = compactor
        self.recovery = recovery
        self.workspace_factory = workspace_factory or (lambda sid: self.settings.workspace_root / sid)
        self.event_sink = event_sink
        self.enable_features = enable_features
        self.mcp_servers = mcp_servers or {}

        # Tool registry template (cloned per session).
        if tool_registry is not None:
            self.tool_registry = tool_registry
        elif enable_features:
            self.tool_registry = full_registry(mcp_servers=self.mcp_servers or None)
        else:
            self.tool_registry = None  # -> Agent default (default_registry)

        if injectors is not None:
            self.injectors = injectors
        elif enable_features:
            self.injectors = default_injectors()
        else:
            self.injectors = []

        # Cross-session services.
        self.bus = MessageBus()
        self.cron = CronScheduler(self, durable_path=settings.workspace_root / ".cron.json")
        self._teammates: dict[str, list[str]] = {}
        self._sessions: dict[str, AgentSession] = {}

    # -- lifecycle (called by the server lifespan) --
    async def start(self) -> None:
        if self.enable_features or self.cron.jobs:
            self.cron.start()

    async def stop(self) -> None:
        await self.cron.stop()

    # -- internal: build an Agent with services seeded into its state --
    def _build_agent(self, session: AgentSession, *, settings: Settings, extra_state: dict) -> Agent:
        registry = self.tool_registry.clone() if self.tool_registry is not None else None
        state = {
            "manager": self,
            "session_id": session.id,
            "bus": self.bus,
            "cron": self.cron,
            "team_id": session.id,
            "agent_name": "lead",
        }
        state.update(extra_state)
        return Agent(
            client=self.client,
            settings=settings,
            workspace=session.workspace,
            skills=self.skills,
            tools=registry,
            hooks=self.hooks,
            system=session.system,
            system_builder=self.system_builder,
            compactor=self.compactor,
            recovery=self.recovery,
            injectors=list(self.injectors),
            emit=session.emit,
            llm_semaphore=self.llm_semaphore,
            label="main",
            state=state,
        )

    def create(self, *, system: str | None = None, model: str | None = None) -> AgentSession:
        session_id = uuid.uuid4().hex[:12]
        workspace = Path(self.workspace_factory(session_id))
        workspace.mkdir(parents=True, exist_ok=True)

        session = AgentSession(session_id, workspace, system=system, event_sink=self.event_sink)
        settings = self.settings if model is None else dataclasses.replace(self.settings, model=model)
        session.agent = self._build_agent(session, settings=settings, extra_state={})
        self._sessions[session_id] = session
        return session

    # -- s15-17: spawn a teammate = a concurrent session sharing the workspace --
    async def spawn_teammate(self, parent_id: str, name: str, role: str, prompt: str) -> str:
        import asyncio

        parent = self.get(parent_id)
        if parent is None:
            return f"Error: no parent session {parent_id}"
        session_id = uuid.uuid4().hex[:12]
        # Shares the parent's workspace -> shared .tasks board + .memory + mailbox group.
        session = AgentSession(session_id, parent.workspace, event_sink=self.event_sink)
        session.agent = self._build_agent(
            session, settings=self.settings,
            extra_state={"team_id": parent_id, "agent_name": name, "role": role, "session_id": session_id},
        )
        session.agent.system = (
            f"You are teammate '{name}' (role: {role}) working in {session.workspace}.\n"
            "Coordinate with the team via send_message / read_inbox and the shared task board "
            "(list_tasks / claim_task / complete_task). Report results to 'lead'."
        )
        if session.agent.tools is not None:
            session.agent.tools.unregister("spawn_teammate")  # no fork bombs
        self._sessions[session_id] = session
        self._teammates.setdefault(parent_id, []).append(name)
        session.spawn_task = asyncio.create_task(session.run(prompt))  # type: ignore[attr-defined]
        return f"Spawned teammate '{name}' (session {session_id}); running concurrently."

    def teammates_of(self, team_id: str) -> list[str]:
        return list(self._teammates.get(team_id, []))

    # -- registry ops --
    def get(self, session_id: str) -> AgentSession | None:
        return self._sessions.get(session_id)

    def list(self) -> list[AgentSession]:
        return list(self._sessions.values())

    def delete(self, session_id: str, *, remove_workspace: bool = True) -> bool:
        session = self._sessions.pop(session_id, None)
        if session is None:
            return False
        # Don't delete a workspace shared by teammates.
        shared = any(s.workspace == session.workspace for s in self._sessions.values())
        if remove_workspace and not shared:
            shutil.rmtree(session.workspace, ignore_errors=True)
        return True
