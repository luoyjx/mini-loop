"""SessionManager -- the multi-agent registry and the place to inject every
extension seam for the whole fleet, now including the cross-session services
(message bus, cron scheduler, teammate spawning).

Inject once at construction; every session inherits it:

    tool_registry / hooks / system_builder / compactor / recovery / injectors
    workspace_factory / event_sink

Flip `enable_features=True` (or env MINILOOP_FEATURES) to turn on the complete
tool set and its background/team lifecycle injectors.
"""

from __future__ import annotations

import asyncio
import dataclasses
import json
import re
import shutil
import uuid
from collections.abc import Callable
from pathlib import Path

from .agent import Agent
from .builtins import default_injectors, full_registry
from .config import Settings
from .cron import CronScheduler
from .memory import MemoryStore
from .registry import Hooks, ToolRegistry
from .session import AgentSession
from .skills import SkillLoader
from .tasks import TaskStore
from .teams import MessageBus, ProtocolState, team_key
from .worktrees import WorktreeManager


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
        self.bus = MessageBus(settings.workspace_root / ".teams")
        self.memory = MemoryStore(settings.memory_root or (settings.workspace_root / ".memory"))
        self.worktrees = WorktreeManager(settings.repo_root) if settings.repo_root else None
        self.cron = CronScheduler(self, durable_path=settings.workspace_root / ".cron.json")
        self._teammates: dict[str, dict[str, str]] = {}
        self.protocols: dict[str, ProtocolState] = {}
        self._sessions: dict[str, AgentSession] = {}

    # -- lifecycle (called by the server lifespan) --
    async def start(self) -> None:
        if self.enable_features or self.cron.jobs:
            self.cron.start()

    async def stop(self) -> None:
        await self.cron.stop()
        tasks = []
        for session in self._sessions.values():
            for attribute in ("spawn_task", "lifecycle_task"):
                task = getattr(session, attribute, None)
                if task is not None and not task.done():
                    task.cancel()
                    tasks.append(task)
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)
        clients = {}
        for session in self._sessions.values():
            if session.agent is None:
                continue
            for client in session.agent.state.get("mcp_clients", {}).values():
                clients[id(client)] = client
        if clients:
            await asyncio.gather(*(client.close() for client in clients.values()), return_exceptions=True)
        backgrounds = {}
        for session in self._sessions.values():
            if session.agent is None:
                continue
            manager = session.agent.state.get("background")
            if manager is not None:
                backgrounds[id(manager)] = manager
        if backgrounds:
            await asyncio.gather(*(manager.close() for manager in backgrounds.values()),
                                 return_exceptions=True)

    # -- internal: build an Agent with services seeded into its state --
    def _build_agent(self, session: AgentSession, *, settings: Settings, extra_state: dict,
                     label: str = "main") -> Agent:
        registry = self.tool_registry.clone() if self.tool_registry is not None else None
        state = {
            "manager": self,
            "session_id": session.id,
            "bus": self.bus,
            "cron": self.cron,
            "team_id": session.id,
            "agent_name": "lead",
            "memory": self.memory,
            "memory_root": self.memory.dir,
            "worktrees": self.worktrees,
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
            label=label,
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

    def restore_scheduled_session(self, session_id: str) -> AgentSession:
        """Restore the stable session identity referenced by a durable cron job."""
        existing = self.get(session_id)
        if existing is not None:
            return existing
        workspace = Path(self.workspace_factory(session_id))
        workspace.mkdir(parents=True, exist_ok=True)
        session = AgentSession(session_id, workspace, event_sink=self.event_sink)
        session.agent = self._build_agent(session, settings=self.settings, extra_state={})
        self._sessions[session_id] = session
        return session

    # -- s15-17: spawn a teammate = a concurrent session sharing the workspace --
    async def spawn_teammate(self, parent_id: str, name: str, role: str, prompt: str) -> str:
        parent = self.get(parent_id)
        if parent is None:
            return f"Error: no parent session {parent_id}"
        assert parent.agent is not None
        team_id = parent.agent.state.get("team_id", parent_id)
        if name == "lead" or name in self._teammates.get(team_id, {}):
            return f"Error: teammate name '{name}' is already in use"
        if not re.fullmatch(r"[A-Za-z0-9._-]{1,64}", name):
            return "Error: teammate name must match [A-Za-z0-9._-]{1,64}"
        session_id = uuid.uuid4().hex[:12]
        # Shares the parent's workspace -> shared .tasks board + .memory + mailbox group.
        session = AgentSession(session_id, parent.workspace, event_sink=self.event_sink)
        session.agent = self._build_agent(
            session, settings=self.settings,
            extra_state={
                "team_id": team_id,
                "agent_name": name,
                "role": role,
                "session_id": session_id,
                "team_workspace": parent.workspace,
                "tasks": TaskStore(parent.workspace),
            },
            label=name,
        )
        teammate_identity = f"You are teammate '{name}' (role: {role})"
        teammate_guidance = (
            "Coordinate with the team via send_message / read_inbox and the shared task board "
            "(list_tasks / claim_task / complete_task). Use submit_plan when the lead requests "
            "a plan, and wait for its correlated approval response before implementation. "
            "Report results to 'lead'."
        )
        base_builder = session.agent.system_builder
        session.agent.use_system_builder(
            lambda agent, identity=teammate_identity, guidance=teammate_guidance,
            build=base_builder: (
                f"{identity} working in {agent.workspace}.\n{guidance}\n\n{build(agent)}"
            )
        )
        if session.agent.tools is not None:
            session.agent.tools.unregister("spawn_teammate")  # no fork bombs
        self._sessions[session_id] = session
        self._teammates.setdefault(team_id, {})[name] = session_id
        session.spawn_task = asyncio.create_task(  # type: ignore[attr-defined]
            self._initial_teammate_run(session, prompt)
        )
        return f"Spawned teammate '{name}' (session {session_id}); running concurrently."

    def teammates_of(self, team_id: str) -> list[str]:
        return list(self._teammates.get(team_id, {}))

    def teammate_session(self, team_id: str, name: str) -> AgentSession | None:
        session_id = self._teammates.get(team_id, {}).get(name)
        return self.get(session_id) if session_id else None

    async def _initial_teammate_run(self, session: AgentSession, prompt: str) -> str:
        assert session.agent is not None
        result = await session.run(prompt)
        state = session.agent.state
        self.bus.send(team_key(state["team_id"], state["agent_name"]),
                      team_key(state["team_id"], "lead"), result, "result")
        session.lifecycle_task = asyncio.create_task(  # type: ignore[attr-defined]
            self._teammate_idle_loop(session)
        )
        return result

    async def _teammate_idle_loop(self, session: AgentSession) -> None:
        assert session.agent is not None
        agent = session.agent
        state = agent.state
        team_id, name = state["team_id"], state["agent_name"]
        board: TaskStore = state["tasks"]
        loop = asyncio.get_running_loop()
        deadline = loop.time() + self.settings.team_idle_timeout

        while loop.time() < deadline:
            await asyncio.sleep(self.settings.team_idle_poll)
            messages = self.consume_team_inbox(team_id, name)
            if state.pop("shutdown_requested", False):
                return
            if messages:
                prompt = f"<team_inbox>\n{json.dumps(messages, default=str)}\n</team_inbox>"
                result = await session.run(prompt)
                self.bus.send(team_key(team_id, name), team_key(team_id, "lead"), result, "result")
                deadline = loop.time() + self.settings.team_idle_timeout
                continue

            runnable = await asyncio.to_thread(board.runnable)
            claimed = None
            for task in runnable:
                result = await asyncio.to_thread(board.claim, task.id, name)
                if result.startswith("Claimed"):
                    claimed = board.load(task.id)
                    break
            if claimed is None:
                continue

            target_workspace = state["team_workspace"]
            if claimed.worktree and self.worktrees is not None:
                try:
                    path = self.worktrees.path_for(claimed.worktree)
                except ValueError:
                    path = None
                if path is not None and path.exists():
                    target_workspace = path
            agent.enter_workspace(target_workspace)
            result = await session.run(
                f"You autonomously claimed {claimed.id}: {claimed.subject}\n"
                f"{claimed.description}\nComplete the work, then call complete_task for {claimed.id}."
            )
            self.bus.send(team_key(team_id, name), team_key(team_id, "lead"), result,
                          "result", {"task_id": claimed.id})
            deadline = loop.time() + self.settings.team_idle_timeout

        self.bus.send(team_key(team_id, name), team_key(team_id, "lead"),
                      "Idle timeout reached; teammate shut down.", "idle_notification")

    def _new_protocol(self, protocol_type: str, team_id: str, sender: str,
                      target: str, payload: str) -> ProtocolState:
        request_id = f"req_{uuid.uuid4().hex[:10]}"
        state = ProtocolState(
            request_id=request_id,
            type=protocol_type,
            sender=team_key(team_id, sender),
            target=team_key(team_id, target),
            payload=payload,
        )
        self.protocols[request_id] = state
        return state

    def request_shutdown(self, team_id: str, target: str, reason: str = "") -> str:
        if self.teammate_session(team_id, target) is None:
            return f"Error: no teammate {target}"
        state = self._new_protocol("shutdown", team_id, "lead", target, reason)
        self.bus.send(state.sender, state.target, reason or "Please shut down.",
                      "shutdown_request", {"request_id": state.request_id})
        return state.request_id

    def request_plan(self, team_id: str, target: str, task: str) -> str:
        if self.teammate_session(team_id, target) is None:
            return f"Error: no teammate {target}"
        return self.bus.send(
            team_key(team_id, "lead"), team_key(team_id, target),
            f"Please submit a plan for: {task}", "plan_request",
        )

    def submit_plan(self, team_id: str, sender: str, plan: str) -> str:
        if sender == "lead":
            return "Error: only teammates submit plans to the lead"
        state = self._new_protocol("plan_approval", team_id, sender, "lead", plan)
        self.bus.send(state.sender, state.target, plan, "plan_approval_request",
                      {"request_id": state.request_id})
        return state.request_id

    def review_plan(self, team_id: str, request_id: str, approve: bool,
                    feedback: str = "") -> str:
        state = self.protocols.get(request_id)
        if state is None or state.type != "plan_approval":
            return f"Error: no plan request {request_id}"
        if state.status != "pending":
            return f"Error: request {request_id} is already {state.status}"
        if not state.sender.startswith(team_id + "/"):
            return f"Error: request {request_id} belongs to another team"
        state.status = "approved" if approve else "rejected"
        state.feedback = feedback
        self.bus.send(team_key(team_id, "lead"), state.sender,
                      feedback or state.status, "plan_approval_response",
                      {"request_id": request_id, "approve": approve})
        return f"Plan {request_id} {state.status}"

    def _match_protocol(self, message: dict) -> None:
        request_id = message.get("metadata", {}).get("request_id", "")
        state = self.protocols.get(request_id)
        if state is None or state.status != "pending":
            return
        message_type = message.get("type", "")
        expected = {"shutdown": "shutdown_response", "plan_approval": "plan_approval_response"}
        if expected.get(state.type) != message_type:
            return
        approved = bool(message.get("metadata", {}).get("approve", False))
        state.status = "approved" if approved else "rejected"
        state.feedback = str(message.get("content", ""))

    def consume_team_inbox(self, team_id: str, name: str) -> list[dict]:
        messages = self.bus.read(team_key(team_id, name))
        session = self.teammate_session(team_id, name) if name != "lead" else None
        for message in messages:
            self._match_protocol(message)
            if message.get("type") == "shutdown_request" and session and session.agent:
                request_id = message.get("metadata", {}).get("request_id", "")
                self.bus.send(team_key(team_id, name), team_key(team_id, "lead"),
                              "Shutdown approved.", "shutdown_response",
                              {"request_id": request_id, "approve": True})
                session.agent.state["shutdown_requested"] = True
        return messages

    # -- registry ops --
    def get(self, session_id: str) -> AgentSession | None:
        return self._sessions.get(session_id)

    def list(self) -> list[AgentSession]:
        return list(self._sessions.values())

    def delete(self, session_id: str, *, remove_workspace: bool = True) -> bool:
        session = self._sessions.pop(session_id, None)
        if session is None:
            return False
        for attribute in ("spawn_task", "lifecycle_task"):
            task = getattr(session, attribute, None)
            if task is not None and not task.done():
                task.cancel()
        for team_id, teammates in list(self._teammates.items()):
            for name, teammate_id in list(teammates.items()):
                if teammate_id == session_id:
                    teammates.pop(name, None)
            if not teammates:
                self._teammates.pop(team_id, None)
        # Don't delete a workspace shared by teammates.
        shared = any(s.workspace == session.workspace for s in self._sessions.values())
        if remove_workspace and not shared:
            shutil.rmtree(session.workspace, ignore_errors=True)
        return True
