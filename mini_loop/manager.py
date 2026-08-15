"""SessionManager -- the multi-agent registry and the place to inject every
extension seam for the whole fleet, now including the cross-session services
(message bus, cron scheduler, teammate spawning).

Inject once at construction; every session inherits it:

    tool_registry / hooks / system_builder / compactor / recovery / injectors
    workspace_factory / event_sink / trajectory_store

Flip `enable_features=True` (or env MINILOOP_FEATURES) to turn on the complete
tool set and its background/team lifecycle injectors.
"""

from __future__ import annotations

import asyncio
import dataclasses
import inspect
import json
import os
import re
import shutil
import uuid
from collections.abc import Callable
from pathlib import Path

from .actions import DurableActionJournal, InMemoryActionJournal
from .agent import Agent
from .ast_context import AstContextConfig, install_ast_context_tools
from .builtins import default_injectors, default_registry, full_registry
from .config import Settings
from .cron import CronScheduler
from .harness import Harness
from .memory import MemoryStore
from .registry import Hooks, ToolRegistry
from .run_context import RunContext
from .session import AgentSession
from .secrets import NullSecretRegistry
from .skills import SkillLoader
from .storage import NullStateStore, SessionRecord, StateStore
from .tasks import TaskStore
from .teams import MessageBus, ProtocolState, team_key
from .trajectory import TrajectoryStore
from .token_efficiency import (
    ConciseResponsePolicy,
    ConciseResponsePolicySettings,
    DeterministicLosslessReducer,
    OptimizationMode,
    TokenEfficiencyRegistry,
)
from .tool_policy import DEFAULT_ROLE_TOOL_POLICY
from .token_tools import install_token_efficiency_tools
from .user_resources import UserResourceResolver
from .worktrees import WorktreeManager
from .workflows.service import WorkflowService, workflow_injector
from .workflows.tools import install_workflows

#: How many deleted-session owners to remember for the legacy trajectory
#: access-check fallback. Modern trajectories carry their own durable owner and
#: never consult this map, so the cap only bounds how far back a *pre-round-74*
#: trajectory of a deleted session stays attributable within one process.
MAX_REMEMBERED_OWNERS = 10_000

#: Team protocol handshakes (plan approvals, shutdown requests) retained. Each
#: `submit_plan`/`request_shutdown` added a `ProtocolState` -- holding the plan
#: payload -- to `self.protocols`, and nothing, not even `delete()`, ever removed
#: one: a resolved handshake is pure history the model still re-reads through
#: `list_protocols`. This is round 146's leak again (the background result store),
#: a manager-level dict that only grows. Past this, the oldest *resolved*
#: handshakes are evicted; a pending one is a live request and is spared until
#: the cap is reached by pending alone.
MAX_PROTOCOLS = 200


def _remove_workspace(path) -> None:
    """Remove a workspace: unlink a link-shaped path, rmtree a real directory.

    `shutil.rmtree` refuses a path that is itself a symlink, and every removal
    site passed `ignore_errors=True` -- so a workspace replaced by a symlink
    (possible without an OS sandbox, where nothing stops a rename against the
    parent) was silently never reclaimed, and worse, whether the link's TARGET
    survived depended on Python internals rather than on a stated rule. The
    rule, from DeepSeek Harness's defensive patterns: unlink deletes only the
    link and never follows it; recursive deletion is reserved for known real
    directories.
    """

    from pathlib import Path

    path = Path(path)
    try:
        if path.is_symlink():
            path.unlink()
            return
        shutil.rmtree(path, ignore_errors=True)
    except OSError:
        pass


class SessionManager:
    @property
    def session_owners(self) -> dict:
        """`session_id -> owner`, retained after a session is deleted.

        Trajectories outlive their session, so their access check needs an
        attribution that outlives it too. Within this process only; after a
        restart the mapping is gone and the check fails closed, which is the
        right direction for an access check and is recorded as still open.

        Bounded to `MAX_REMEMBERED_OWNERS` (round 112): a legacy fallback that
        modern trajectories -- which carry their own durable `owner` -- never
        read, it had grown one entry per deleted session forever. Eviction of
        the oldest fails the same closed way a restart does.
        """

        return self._session_owners

    def persistence_error(self) -> str | None:
        """Why durable state is not being written, or None if it is.

        A store that constructs fine and then fails every write reports itself
        present everywhere -- `posture()` names the class, the audit sees a
        store configured -- while nothing reaches disk. "Installed" and
        "working" are different questions and only one of them was being asked.
        """

        for session in getattr(self, "_sessions", {}).values():
            error = getattr(session, "persist_error", None)
            if error:
                return error
        return None

    def __init__(
        self,
        settings: Settings,
        client,
        *,
        llm_semaphore=None,
        tool_semaphore=None,
        skills: SkillLoader | None = None,
        user_resources: UserResourceResolver | None = None,
        tool_registry: ToolRegistry | None = None,
        hooks: Hooks | None = None,
        system_builder: Callable[[Agent], str] | None = None,
        compactor=None,
        recovery=None,
        stuck_detector=None,
        cache_policy=None,
        transport=None,
        state_store: StateStore | None = None,
        secrets=None,
        sandbox=None,
        spill=None,
        injectors: list | None = None,
        workspace_factory: Callable[[str], Path] | None = None,
        event_sink: Callable[[dict], object] | None = None,
        trajectory_store: TrajectoryStore | None = None,
        enable_features: bool = False,
        enable_workflows: bool = False,
        workflow_service: WorkflowService | None = None,
        workflow_attempt_semaphore: asyncio.Semaphore | None = None,
        mcp_servers: dict | None = None,
        token_efficiency=None,
        role_tool_policy=None,
    ) -> None:
        self.settings = settings
        self.client = client
        self.skills = skills or SkillLoader(settings.skills_dir)
        if llm_semaphore is None:
            llm_semaphore = asyncio.Semaphore(settings.max_concurrent_llm)
        self.llm_semaphore = llm_semaphore
        if tool_semaphore is None:
            tool_semaphore = asyncio.Semaphore(settings.max_concurrent_tools)
        self.tool_semaphore = tool_semaphore
        if workflow_service is not None:
            if (
                workflow_attempt_semaphore is not None
                and (
                    workflow_attempt_semaphore
                    is not workflow_service.attempt_semaphore
                )
            ):
                raise ValueError(
                    "workflow_attempt_semaphore conflicts with the injected "
                    "WorkflowService"
                )
            self.workflow_attempt_semaphore = workflow_service.attempt_semaphore
        else:
            self.workflow_attempt_semaphore = (
                workflow_attempt_semaphore
                if workflow_attempt_semaphore is not None
                else asyncio.Semaphore(
                    settings.workflow_max_concurrent_agents
                )
            )

        # Pending tool approvals, resolvable over the API. When the embedding
        # application supplies its own hooks it owns approval routing too; the
        # broker still exists so the REST surface answers with empty lists
        # rather than errors.
        from .approvals import ApprovalBroker

        self.approvals = ApprovalBroker(timeout=settings.approval_timeout)
        if hooks is None:
            from .permissions import default_hooks

            hooks = default_hooks(approval=self.approvals.ask)
        self.hooks = hooks
        self.system_builder = system_builder
        self.compactor = compactor
        self.recovery = recovery
        # Stateless policy: history lives on each Agent, so one detector
        # instance is safely shared by every session in this manager.
        self.stuck_detector = stuck_detector
        self.cache_policy = cache_policy
        # Durable conversation state. Default is off: persistence is a
        # deployment choice, and the store holds unredacted transcripts.
        self.state_store: StateStore = state_store or NullStateStore()
        # Late-bound: the broker exists before the store does. With a durable
        # store every ask leaves a row, so a restart can tell "parked, never
        # ran" from "dispatched, outcome unknown" (session.restore).
        self.approvals.store = self.state_store
        # Identifies this process to the lease table. Leases only exist when a
        # durable store does -- a NullStateStore has no second process to race.
        self.instance_id = f"{uuid.uuid4().hex[:8]}@{os.getpid()}"
        self._session_owners: dict[str, str] = {}
        # Host credentials: narrow injection into bash, wide masking of
        # every tool result. Default off -- callers opt in explicitly.
        self.secrets = secrets if secrets is not None else NullSecretRegistry()
        # The broker persists a human's answer to the durable approvals table;
        # a registered secret in it must be masked there like every other sink.
        self.approvals.secrets = self.secrets
        self.sandbox = sandbox
        # Preserve-on-truncate: oversized tool output is spilled to a private
        # per-session store instead of being destroyed. `spill_dir=None`
        # (MINILOOP_SPILL_DIR="") disables it; an explicit `spill` object wins.
        if spill is None and settings.spill_dir is not None:
            from .spill import LocalSpillStore

            try:
                spill = LocalSpillStore(settings.spill_dir)
            except OSError:
                # A store that cannot be created must not stop the manager:
                # preservation is best-effort all the way down.
                spill = None
        self.spill = spill
        if token_efficiency is None:
            efficiency_registry = TokenEfficiencyRegistry()
            efficiency_mode = OptimizationMode(settings.token_efficiency_mode)
            if efficiency_mode is not OptimizationMode.OFF:
                efficiency_registry.register_observation(
                    DeterministicLosslessReducer()
                )
            if settings.token_efficiency_response_style == "concise":
                efficiency_registry.register_response_policy(
                    ConciseResponsePolicy(
                        ConciseResponsePolicySettings(require_opt_in=False)
                    )
                )
            token_efficiency = efficiency_registry.runtime(
                default_mode=efficiency_mode,
                raw_store=None,
            )
        self.token_efficiency = token_efficiency
        self.role_tool_policy = (
            role_tool_policy
            if role_tool_policy is not None
            else DEFAULT_ROLE_TOOL_POLICY
        )
        self._token_efficiency_initialize_report = None
        self._token_efficiency_initialize_error: str | None = None
        self._token_efficiency_close_report = None
        self._token_efficiency_close_error: str | None = None
        self._token_efficiency_store_close_errors: list[str] = []
        self._token_efficiency_started = False
        self._manager_stopped = False
        # One value, assembled once. Every agent this manager builds -- session
        # agents, their subagents, workflow workers -- starts from it, so a new
        # seam does not have to be threaded to each construction site.
        self.harness = Harness(
            hooks=hooks,
            system_builder=system_builder,
            compactor=compactor,
            recovery=recovery,
            stuck_detector=stuck_detector,
            cache_policy=cache_policy,
            transport=transport,
            secrets=self.secrets,
            sandbox=sandbox,
            spill=self.spill,
            token_efficiency=self.token_efficiency,
            role_tool_policy=self.role_tool_policy,
        )
        self.workspace_factory = workspace_factory or (lambda sid: self.settings.workspace_root / sid)
        self.event_sink = event_sink
        self.enable_features = enable_features
        self.enable_workflows = bool(enable_workflows or workflow_service is not None)
        self.mcp_servers = mcp_servers or {}

        if trajectory_store is not None:
            self.trajectories = trajectory_store
        elif settings.trajectory_enabled:
            trajectory_root = settings.trajectory_root or (settings.workspace_root / ".trajectories")
            self.trajectories = TrajectoryStore(
                trajectory_root,
                capture_content=settings.trajectory_capture_content,
            )
        else:
            self.trajectories = None

        # Tool registry template (cloned per session).
        tool_registry_was_supplied = tool_registry is not None
        if tool_registry is not None:
            self.tool_registry = (
                tool_registry.clone()
                if self.enable_workflows or settings.ast_outline_enabled
                else tool_registry
            )
        elif enable_features:
            self.tool_registry = full_registry(mcp_servers=self.mcp_servers or None)
        elif self.enable_workflows:
            self.tool_registry = default_registry()
        else:
            self.tool_registry = None  # -> Agent default (default_registry)
        if self.enable_workflows:
            assert self.tool_registry is not None
            install_workflows(self.tool_registry)
        if settings.ast_outline_enabled:
            if self.tool_registry is None:
                self.tool_registry = default_registry()
            install_ast_context_tools(
                self.tool_registry,
                AstContextConfig(
                    binary=settings.ast_outline_binary,
                    expected_sha256=settings.ast_outline_sha256,
                    timeout_seconds=settings.ast_outline_timeout,
                    max_output_bytes=settings.ast_outline_max_output_bytes,
                ),
            )
        if (
            self.tool_registry is not None
            and not tool_registry_was_supplied
            and getattr(self.token_efficiency, "observation_enforced", False)
            and settings.token_efficiency_persist_raw
        ):
            # Install on the parent catalogue before any workflow/subagent
            # capability policy narrows it. Constructors must never widen an
            # already-filtered child registry.
            install_token_efficiency_tools(self.tool_registry)
        if self.tool_registry is not None:
            # Workflow workers derive a capability subset from this template;
            # session agents still receive a private clone in `_build_agent`.
            self.harness = self.harness.derive(tools=self.tool_registry)

        if injectors is not None:
            self.injectors = list(injectors)
        elif enable_features:
            self.injectors = default_injectors()
        else:
            self.injectors = []
        if self.enable_workflows and workflow_injector not in self.injectors:
            self.injectors.append(workflow_injector)
        # Core, not a feature toggle: every manager-built session can be
        # steered, or a busy session drops its caller's words on the floor.
        from .session import steering_injector

        if steering_injector not in self.injectors:
            self.injectors.append(steering_injector)

        # Cross-session services.
        self.bus = MessageBus(settings.workspace_root / ".teams", secrets=self.secrets)
        # The journal is durable when the state store is: an in-memory journal
        # cannot tell a resumed process which side effects already happened.
        if workflow_service is not None:
            self.actions = workflow_service.action_journal
        elif hasattr(self.state_store, "read_action"):
            self.actions = DurableActionJournal(self.state_store)
            # Anything still `started` belongs to a process that is gone.
            self.unknown_actions = tuple(self.actions.mark_inflight_unknown())
        else:
            self.actions = InMemoryActionJournal()
        self.unknown_actions = getattr(self, "unknown_actions", ())
        self.memory = MemoryStore(
            settings.memory_root or (settings.workspace_root / ".memory"),
            secrets=self.secrets,
        )
        self.user_resources = user_resources
        if (
            self.user_resources is None
            and settings.user_resources_root is not None
        ):
            self.user_resources = UserResourceResolver(
                settings.user_resources_root,
                self.skills,
                secrets=self.secrets,
            )
        self.worktrees = WorktreeManager(settings.repo_root) if settings.repo_root else None
        self.cron = CronScheduler(
            self,
            durable_path=settings.workspace_root / ".cron.json",
            secrets=self.secrets,
        )
        self._teammates: dict[str, dict[str, str]] = {}
        self.protocols: dict[str, ProtocolState] = {}
        self._sessions: dict[str, AgentSession] = {}
        self._cleanup_tasks: set[asyncio.Task] = set()
        self._deferred_workspace_cleanup: dict[str, Path] = {}
        self._cleanup_errors: list[dict[str, str]] = []
        self.workflows = workflow_service
        if self.enable_workflows and self.workflows is None:
            self.workflows = WorkflowService(
                settings=settings,
                client=client,
                action_journal=self.actions,
                session_resolver=lambda session_id: self._sessions.get(session_id),
                llm_semaphore=self.llm_semaphore,
                tool_semaphore=self.tool_semaphore,
                attempt_semaphore=self.workflow_attempt_semaphore,
                secrets=self.secrets,
                sandbox=self.sandbox,
                harness=self.harness,
            )

    # -- lifecycle (called by the server lifespan) --
    async def start(self) -> None:
        if self._token_efficiency_started or self._manager_stopped:
            return
        self._token_efficiency_started = True
        initialize = getattr(self.token_efficiency, "initialize", None)
        if callable(initialize):
            try:
                # Components do not receive the SessionManager: that would hand
                # an optimization plugin credentials, stores, process services,
                # and authority well beyond its stage contract.
                report = initialize(None)
                if inspect.isawaitable(report):
                    report = await report
                self._token_efficiency_initialize_report = report
            except Exception as error:
                # Components are advisory projections.  A broken lifecycle hook
                # must not stop the authoritative agent runtime from starting.
                self._token_efficiency_initialize_error = (
                    type(error).__name__
                )
        if self.enable_features or self.cron.jobs:
            self.cron.start()

    async def stop(self) -> None:
        if self._manager_stopped:
            return
        self._manager_stopped = True
        for session in tuple(self._sessions.values()):
            session.stop_accepting("session manager stopped")
        # Resolve parked approvals first so those turns can reach their normal
        # denied terminal record. Give every current holder one short grace
        # window, then cancel anything genuinely stuck before dependencies
        # close. Queued turns already fail the admission check inside the lock.
        self.approvals.cancel_all()
        active_tasks = tuple(
            task
            for session in self._sessions.values()
            if (task := getattr(session, "_running", None)) is not None
            and not task.done()
        )
        if active_tasks:
            await asyncio.wait(active_tasks, timeout=0.25)
        # Ordinary session turns are not background/lifecycle tasks. Drain
        # them explicitly before revoking stores or closing shared optimizer
        # components, otherwise a tool result can race a closed dependency.
        await asyncio.gather(
            *(
                session.cancel("session manager stopped")
                for session in tuple(self._sessions.values())
                if session.busy
            ),
            return_exceptions=True,
        )
        # A clean shutdown hands the sessions back at once. A crash does not,
        # which is what the TTL is for -- the next process waits it out rather
        # than assuming the holder is gone.
        release = getattr(self.state_store, "release_lease", None)
        if release is not None:
            for session in list(self._sessions.values()):
                if session.lease_owner:
                    release(session.id, session.lease_owner)
                    session.lease_owner = None
        await self.cron.stop()
        if self.workflows is not None:
            await self.workflows.close()
        if self._cleanup_tasks:
            await asyncio.gather(
                *tuple(self._cleanup_tasks),
                return_exceptions=True,
            )
        for session_id in tuple(self._deferred_workspace_cleanup):
            if not self._finalize_workspace_cleanup(session_id):
                self._record_cleanup_error(
                    session_id,
                    "workspace retained after shutdown because a workflow "
                    "or sharing session is still active",
                )
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
        # Raw recovery stores are session capabilities, not component-global
        # caches. Revoke every distinct store after consumers have drained and
        # before shared components close.
        raw_stores = {}
        template_store = getattr(self.token_efficiency, "raw_store", None)
        if template_store is not None:
            raw_stores[id(template_store)] = template_store
        for session in self._sessions.values():
            agent = session.agent
            if agent is None:
                continue
            store = getattr(getattr(agent, "token_efficiency", None), "raw_store", None)
            if store is not None:
                raw_stores[id(store)] = store
        for store in raw_stores.values():
            close_store = getattr(store, "close", None)
            if not callable(close_store):
                continue
            try:
                result = close_store()
                if inspect.isawaitable(result):
                    await result
            except Exception as error:
                self._token_efficiency_store_close_errors.append(
                    type(error).__name__
                )
        # Consumers are stopped before their shared components. Closing this
        # at the top races active session/background work that may still be
        # producing an observation or finishing a request optimization.
        close = getattr(self.token_efficiency, "close", None)
        if callable(close):
            try:
                report = close()
                if inspect.isawaitable(report):
                    report = await report
                self._token_efficiency_close_report = report
            except Exception as error:
                # Optional component failure must not undo the authoritative
                # cleanup that has already completed.
                self._token_efficiency_close_error = (
                    type(error).__name__
                )

    # -- internal: build an Agent with services seeded into its state --
    def _build_agent(
        self,
        session: AgentSession,
        *,
        settings: Settings,
        extra_state: dict,
        label: str = "main",
        resolve_user_resources: bool = True,
    ) -> Agent:
        registry = self.tool_registry.clone() if self.tool_registry is not None else None
        resource_owner = getattr(session, "owner", "anonymous")
        resources = (
            self.user_resources.for_owner(resource_owner)
            if self.user_resources is not None and resolve_user_resources
            else None
        )
        skills = resources.skills if resources is not None else self.skills
        memory = resources.memory if resources is not None else self.memory
        state = {
            "manager": self,
            "session": session,
            "session_id": session.id,
            "bus": self.bus,
            "cron": self.cron,
            "team_id": session.id,
            "agent_name": "lead",
            "action_journal": self.actions,
            "memory": memory,
            "memory_root": memory.dir,
            "resource_owner": resource_owner,
            "worktrees": self.worktrees,
            "workflow_service": self.workflows,
            "permission_mode": session.permission_mode,
        }
        state.update(extra_state)
        return Agent(
            client=self.client,
            settings=settings,
            workspace=session.workspace,
            system=session.system,
            harness=self.harness.derive(
                tools=registry,
                skills=skills,
                injectors=tuple(self.injectors),
            ),
            emit=session.emit,
            llm_semaphore=self.llm_semaphore,
            tool_semaphore=self.tool_semaphore,
            label=label,
            state=state,
        )

    def create(
        self,
        *,
        system: str | None = None,
        model: str | None = None,
        permission_mode: str = "interactive",
        owner: str = "anonymous",
    ) -> AgentSession:
        if self._manager_stopped:
            raise RuntimeError("session manager is stopped")
        from .permissions import PERMISSION_MODES

        if not isinstance(owner, str) or not owner:
            raise ValueError("owner must be a non-empty string")
        if permission_mode not in PERMISSION_MODES:
            raise ValueError(
                f"unknown permission mode {permission_mode!r}; "
                f"expected one of {PERMISSION_MODES}"
            )
        session_id = uuid.uuid4().hex[:12]
        workspace = Path(self.workspace_factory(session_id))
        workspace.mkdir(parents=True, exist_ok=True)

        session = AgentSession(
            session_id,
            workspace,
            system=system,
            event_sink=self.event_sink,
            trajectory_store=self.trajectories,
            state_store=self.state_store,
        )
        session.permission_mode = permission_mode
        session.owner = owner
        settings = self.settings if model is None else dataclasses.replace(self.settings, model=model)
        session.agent = self._build_agent(session, settings=settings, extra_state={})
        self._sessions[session_id] = session
        self._record(session)
        self._claim(session)
        return session

    def _claim(self, session: AgentSession) -> None:
        """Take the session's lease, if the store supports one."""
        if not hasattr(self.state_store, "acquire_lease"):
            return
        session.lease_owner = self.instance_id
        # Check the acquire, don't discard it -- the sibling smell to the
        # renewal `_renew_lease` used to have (round 157). A claim whose result
        # goes unread is a lease this process cannot prove it holds. On success,
        # record that it holds it, so a mid-turn loss is a real loss even for a
        # session driven straight through `agent.run`, which never reaches
        # `_require_lease`. `lease_owner` stays set on failure so `_require_lease`
        # at the turn's start still re-checks and fails closed on a live foreign
        # lease -- the restored session that lost its claim stays unconfirmed and
        # does not raise on a renewal it was never going to win.
        if self.state_store.acquire_lease(
            session.id, self.instance_id, ttl=session.lease_ttl
        ):
            session.lease_confirmed = True

    def _record(self, session: AgentSession) -> None:
        """Write the session's identity so a later process can rebuild it."""
        self.state_store.upsert_session(
            SessionRecord(
                session_id=session.id,
                workspace=str(session.workspace),
                system=session.system,
                created_at=session.created_at,
                run_count=session.run_count,
                status=session.status,
                event_cursor=0,  # derived on read; not authoritative here
                owner=getattr(session, "owner", "anonymous"),
            )
        )

    def restore_sessions(self) -> list[AgentSession]:
        """Rebuild live handles for every persisted session.

        The store is the source of truth for the transcript and the event
        cursor. Workspaces are *not* recreated -- a session whose workspace is
        gone is restored with an empty one rather than being silently skipped,
        so the caller can see it and decide.

        Not in this slice: run status is restored as recorded, so a process
        killed mid-run comes back as `running` with no run attached. There is no
        run state machine to resume yet, and inventing one here would be worse
        than surfacing the truth.
        """
        if self._manager_stopped:
            raise RuntimeError("session manager is stopped")
        restored = []
        for record in self.state_store.load_sessions():
            if record.session_id in self._sessions:
                continue
            workspace = Path(record.workspace)
            workspace.mkdir(parents=True, exist_ok=True)
            session = AgentSession(
                record.session_id,
                workspace,
                system=record.system,
                event_sink=self.event_sink,
                trajectory_store=self.trajectories,
                state_store=self.state_store,
            )
            session.owner = record.owner
            session.agent = self._build_agent(
                session, settings=self.settings, extra_state={}
            )
            self._rehydrate(session, record)
            self._claim(session)
            self._sessions[record.session_id] = session
            restored.append(session)
        return restored

    def restore_scheduled_session(self, session_id: str) -> AgentSession:
        """Restore the stable session identity referenced by a durable cron job."""
        if self._manager_stopped:
            raise RuntimeError("session manager is stopped")
        existing = self.get(session_id)
        if existing is not None:
            return existing
        record = next(
            (
                item
                for item in self.state_store.load_sessions()
                if item.session_id == session_id
            ),
            None,
        )
        workspace = Path(self.workspace_factory(session_id))
        workspace.mkdir(parents=True, exist_ok=True)
        session = AgentSession(
            session_id,
            workspace,
            event_sink=self.event_sink,
            trajectory_store=self.trajectories,
            state_store=self.state_store,
        )
        if record is not None:
            session.owner = record.owner
        session.agent = self._build_agent(session, settings=self.settings, extra_state={})
        # Same rehydration as `restore_sessions`: without it the handle starts
        # with an empty transcript while the store already holds one, and the
        # next flush appends into that same epoch -- splicing two histories.
        self._rehydrate(session, record)
        self._claim(session)
        self._sessions[session_id] = session
        return session

    def _rehydrate(
        self,
        session: AgentSession,
        record: SessionRecord | None = None,
    ) -> None:
        """Rebuild a session's durable state onto a freshly built handle."""
        if record is None:
            record = next(
                (
                    item
                    for item in self.state_store.load_sessions()
                    if item.session_id == session.id
                ),
                None,
            )
        if record is None:
            return
        session.created_at = record.created_at
        session.run_count = record.run_count
        session.status = record.status
        session.restore()
        if record.todos and session.agent is not None:
            session.agent.todo.items = [dict(t) for t in record.todos]

    # -- s15-17: spawn a teammate = a concurrent session sharing the workspace --
    async def spawn_teammate(
        self,
        parent_id: str,
        name: str,
        role: str,
        prompt: str,
        run_context: RunContext | None = None,
    ) -> str:
        if self._manager_stopped:
            return "Error: session manager is stopped"
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
        # Shares the parent's workspace -> shared task board and mailbox group.
        # User memory is inherited through the owner-bound resource resolver;
        # the legacy fallback may still live under the shared workspace root.
        session = AgentSession(
            session_id,
            parent.workspace,
            event_sink=self.event_sink,
            trajectory_store=self.trajectories,
            state_store=self.state_store,
        )
        session.owner = parent.owner
        session.agent = self._build_agent(
            session, settings=self.settings,
            extra_state={
                "team_id": team_id,
                "agent_name": name,
                "role": role,
                "session_id": session_id,
                "team_workspace": parent.workspace,
                "tasks": TaskStore(parent.workspace, secrets=self.secrets),
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
            for tool_name in ("Workflow", "WorkflowStatus", "WorkflowCancel"):
                session.agent.tools.unregister(tool_name)  # no nested workflows
        self._sessions[session_id] = session
        self._teammates.setdefault(team_id, {})[name] = session_id
        parent_context = (
            run_context
            or parent.agent.current_run_context
            or RunContext.default()
        )
        teammate_context = parent_context.derive_peer_agent(
            delegated_by=parent.agent.label,
            actor_id=name,
        )
        session.spawn_task = asyncio.create_task(  # type: ignore[attr-defined]
            self._initial_teammate_run(session, prompt, teammate_context)
        )
        return f"Spawned teammate '{name}' (session {session_id}); running concurrently."

    def _deliver(self, frm: str, to: str, content: str,
                 msg_type: str = "message", metadata: dict | None = None) -> str:
        """Deliver a team message, keeping the content rather than dropping it.

        Eight `bus.send` calls in this file discarded their return value. That
        was harmless until round 50 gave `send` a size limit and made it report
        refusals by returning a string -- after which a teammate's finished work
        was silently lost:

            a teammate's finished result: 26,032 chars
            bus.send returned           : 'Error: message is 26,032 characters...'
            lead's inbox                : 0 messages

        A *result* is truncated rather than refused, which is the opposite of
        the call made for a cron prompt in round 47 and for the same reason: a
        truncated report still carries most of the work, while a truncated
        instruction that still executes is worse than one that never ran.
        `request_plan` sends an instruction and so keeps using `bus.send`
        directly, where a refusal is returned to its caller.

        Anything the bus still refuses is recorded rather than swallowed.
        """

        limit = MessageBus.MAX_CONTENT
        if len(content) > limit:
            kept = content[: limit - 200]
            content = (
                f"{kept}\n\n[truncated: {len(content):,} characters delivered "
                f"as {limit:,}]"
            )
        result = self.bus.send(frm, to, content, msg_type, metadata)
        if str(result).startswith("Error:"):
            self.bus.problems.append(f"delivery to {to!r} failed: {result}")
        return result

    def teammates_of(self, team_id: str) -> list[str]:
        return list(self._teammates.get(team_id, {}))

    def teammate_session(self, team_id: str, name: str) -> AgentSession | None:
        session_id = self._teammates.get(team_id, {}).get(name)
        return self.get(session_id) if session_id else None

    async def _initial_teammate_run(
        self,
        session: AgentSession,
        prompt: str,
        run_context: RunContext | None = None,
    ) -> str:
        assert session.agent is not None
        result = await session.run(
            prompt,
            run_context=run_context or self._teammate_run_context(session),
        )
        state = session.agent.state
        self._deliver(team_key(state["team_id"], state["agent_name"]),
                      team_key(state["team_id"], "lead"), result, "result")
        session.lifecycle_task = asyncio.create_task(  # type: ignore[attr-defined]
            self._teammate_idle_loop(session)
        )
        return result

    @staticmethod
    def _teammate_run_context(session: AgentSession) -> RunContext:
        assert session.agent is not None
        state = session.agent.state
        return RunContext.peer_agent(
            delegated_by="lead",
            actor_id=state.get("agent_name"),
            stamped_by="session_manager",
        )

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
                result = await session.run(
                    prompt,
                    run_context=self._teammate_run_context(session),
                )
                self._deliver(team_key(team_id, name), team_key(team_id, "lead"), result, "result")
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
                f"{claimed.description}\nComplete the work, then call complete_task for {claimed.id}.",
                run_context=self._teammate_run_context(session),
            )
            self._deliver(team_key(team_id, name), team_key(team_id, "lead"), result,
                          "result", {"task_id": claimed.id})
            deadline = loop.time() + self.settings.team_idle_timeout

        self._deliver(team_key(team_id, name), team_key(team_id, "lead"),
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
        self._prune_protocols()
        return state

    def _prune_protocols(self) -> None:
        """Bound `self.protocols`, evicting resolved handshakes before pending.

        A resolved (approved/rejected) handshake is history the model still
        re-reads through `list_protocols`; a pending one is a live request
        awaiting a response, and dropping it reads as "never asked" -- the
        action journal's rule. So resolved entries give way first, oldest by
        insertion order; only a cap reached by pending alone (a requester that
        vanished mid-handshake, e.g. a deleted session that never resolved) lets
        the oldest pending give way, as a bounded safety valve.
        """

        overflow = len(self.protocols) - MAX_PROTOCOLS
        if overflow <= 0:
            return
        resolved = [rid for rid, s in self.protocols.items() if s.status != "pending"]
        victims = resolved[:overflow]
        if len(victims) < overflow:
            pending = [rid for rid, s in self.protocols.items() if s.status == "pending"]
            victims += pending[: overflow - len(victims)]
        for request_id in victims:
            del self.protocols[request_id]

    def request_shutdown(self, team_id: str, target: str, reason: str = "") -> str:
        if self.teammate_session(team_id, target) is None:
            return f"Error: no teammate {target}"
        state = self._new_protocol("shutdown", team_id, "lead", target, reason)
        self._deliver(state.sender, state.target, reason or "Please shut down.",
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
        self._deliver(state.sender, state.target, plan, "plan_approval_request",
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
        self._deliver(team_key(team_id, "lead"), state.sender,
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
                self._deliver(team_key(team_id, name), team_key(team_id, "lead"),
                              "Shutdown approved.", "shutdown_response",
                              {"request_id": request_id, "approve": True})
                session.agent.state["shutdown_requested"] = True
        return messages

    # -- registry ops --
    def get(self, session_id: str) -> AgentSession | None:
        return self._sessions.get(session_id)

    def list(self) -> list[AgentSession]:
        return list(self._sessions.values())

    @property
    def cleanup_errors(self) -> tuple[dict[str, str], ...]:
        return tuple(dict(item) for item in self._cleanup_errors)

    def _record_cleanup_error(self, session_id: str, error: str) -> None:
        workspace = self._deferred_workspace_cleanup.get(session_id)
        self._cleanup_errors.append({
            "session_id": session_id,
            "workspace": str(workspace) if workspace is not None else "",
            "error": error[:500],
        })
        del self._cleanup_errors[:-100]

    def _finalize_workspace_cleanup(self, session_id: str) -> bool:
        workspace = self._deferred_workspace_cleanup.get(session_id)
        if workspace is None:
            return True
        if (
            self.workflows is not None
            and self.workflows.has_active(session_id)
        ):
            return False
        if any(
            session.workspace == workspace
            for session in self._sessions.values()
        ):
            return False
        _remove_workspace(workspace)
        self._deferred_workspace_cleanup.pop(session_id, None)
        return True

    async def _drain_workspace_cleanup(
        self,
        session_id: str,
        cancellation_tasks: tuple[asyncio.Task, ...],
    ) -> None:
        outcomes = await asyncio.gather(
            *cancellation_tasks,
            return_exceptions=True,
        )
        failures = [
            outcome for outcome in outcomes
            if isinstance(outcome, BaseException)
        ]
        if failures:
            detail = "; ".join(
                f"{type(error).__name__}: {error}"
                for error in failures
            )
            self._record_cleanup_error(session_id, detail)
            return
        if not self._finalize_workspace_cleanup(session_id):
            self._record_cleanup_error(
                session_id,
                "cancellation completed but workflow remains active",
            )

    def delete(self, session_id: str, *, remove_workspace: bool = True) -> bool:
        session = self._sessions.get(session_id)
        if session is not None:
            # A legacy fallback, and now a bounded one. Trajectories recorded
            # since round 74 carry their own durable `owner` field, which is
            # the primary path in `_owns_trajectory`; this map is consulted
            # only for older trajectories that lack it. Left unbounded it grew
            # one entry per deleted session forever -- memory, and O(deleted)
            # latency on every trajectory *listing*, which iterates it. Capped
            # here: eviction only affects legacy trajectories of long-ago
            # deleted sessions, which then fail the access check closed -- the
            # same safe direction as after a restart (see `session_owners`).
            owners = self._session_owners
            owners.pop(session_id, None)  # move-to-newest on re-delete
            owners[session_id] = getattr(session, "owner", "anonymous")
            while len(owners) > MAX_REMEMBERED_OWNERS:
                del owners[next(iter(owners))]
        if session is None:
            return False
        session.stop_accepting("session deleted")
        running_turn = getattr(session, "_running", None)
        workflow_active = (
            self.workflows is not None
            and self.workflows.has_active(session_id)
        )
        cancellation_tasks = (
            self.workflows.request_cancel_session(session_id)
            if self.workflows is not None
            else ()
        )
        self._sessions.pop(session_id, None)
        # Disown the lease before waking a parked turn below: `delete_session`
        # removes the row this session's lease lived in, so a turn that emits
        # after that finds its lease gone and -- if it held one -- would read the
        # self-delete as another process stealing it. Clearing `lease_owner` (as
        # the clean-shutdown path already does) makes the renewal a no-op, so the
        # cancelled turn finishes recording its own cancellation instead.
        session.lease_owner = None
        session.lease_confirmed = False
        # A turn may be parked on an approval nobody will ever answer now;
        # denying it lets that turn finish instead of waiting out the timeout.
        self.approvals.cancel_session(session_id)
        # Cancel the session's scheduled work too. A leftover cron job outlives
        # its session and resurrects it via restore_scheduled_session on its next
        # fire -- re-creating the workspace removed just below and rehydrating the
        # transcript -- so a delete that skipped this did not actually stop the
        # one kind of work that runs unattended.
        self.cron.cancel_for_session(session_id)
        # Remove the durable record too, or `restore_sessions()` on the next
        # startup rebuilds the deleted session from the `sessions` row that
        # survived -- the same resurrection as the cron path above, on every
        # restart. This also frees the session's lease, which lives in that row.
        self.state_store.delete_session(session_id)
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
        # The services stop() reclaims, reclaimed for this one session.
        # delete() used to skip this, and the gap compounded: the background
        # shell kept running in a workspace this method was about to remove,
        # `check_background` had left with the session, and stop() could no
        # longer see the manager because the session was already popped from
        # `_sessions` -- so a process started with `start_new_session=True`
        # survived the server itself. OpenWorker documents the same hazard
        # (background shell outliving session/server) as an open risk; here
        # the close path existed and nothing on this route called it.
        agent = session.agent
        background = agent.state.get("background") if agent is not None else None
        # An MCP client object may be registered on several sessions; close
        # it only when no surviving session still holds it -- the same rule
        # as the shared workspace below.
        still_held = {
            id(client)
            for other in self._sessions.values() if other.agent is not None
            for client in other.agent.state.get("mcp_clients", {}).values()
        }
        mcp_clients = [
            client
            for client in (agent.state.get("mcp_clients", {}).values() if agent else ())
            if id(client) not in still_held
        ]
        raw_store = (
            getattr(getattr(agent, "token_efficiency", None), "raw_store", None)
            if agent is not None
            else None
        )
        if raw_store is not None and any(
            getattr(
                getattr(other.agent, "token_efficiency", None),
                "raw_store",
                None,
            )
            is raw_store
            for other in self._sessions.values()
            if other.agent is not None
        ):
            raw_store = None
        # Don't delete a workspace shared by teammates.
        shared = any(s.workspace == session.workspace for s in self._sessions.values())
        remove_now = remove_workspace and not shared and not workflow_active
        if (
            background is not None
            or mcp_clients
            or raw_store is not None
            or (running_turn is not None and not running_turn.done())
        ):
            try:
                asyncio.get_running_loop()
            except RuntimeError:
                # No loop to run an async close on. Cancellation is still
                # requested -- the process-group kill rides on it if the loop
                # ever runs again -- and the workspace is removed as before.
                if background is not None:
                    background.cancel_all()
                if running_turn is not None and not running_turn.done():
                    running_turn.get_loop().call_soon_threadsafe(running_turn.cancel)

                    def _revoke_after_turn(_task) -> None:
                        close_store = getattr(raw_store, "close", None)
                        if callable(close_store):
                            close_store()
                        if remove_now:
                            _remove_workspace(session.workspace)

                    running_turn.add_done_callback(_revoke_after_turn)
                else:
                    close_store = getattr(raw_store, "close", None)
                    if callable(close_store):
                        close_store()
                    if remove_now:
                        _remove_workspace(session.workspace)
            else:
                async def _close_services() -> None:
                    if running_turn is not None and not running_turn.done():
                        try:
                            await asyncio.wait_for(
                                asyncio.shield(running_turn), timeout=5
                            )
                        except asyncio.TimeoutError:
                            await session.cancel("session deleted")
                        except (asyncio.CancelledError, Exception):
                            pass
                    closes = []
                    if background is not None:
                        closes.append(background.close())
                    closes.extend(client.close() for client in mcp_clients)
                    await asyncio.gather(*closes, return_exceptions=True)
                    close_store = getattr(raw_store, "close", None)
                    if callable(close_store):
                        result = close_store()
                        if inspect.isawaitable(result):
                            await result
                    # Only after the shell is dead: removing the directory a
                    # live process has as its cwd is a race, not a cleanup.
                    if remove_now:
                        _remove_workspace(session.workspace)
                cleanup = asyncio.create_task(_close_services())
                self._cleanup_tasks.add(cleanup)
                cleanup.add_done_callback(self._cleanup_tasks.discard)
        elif remove_now:
            _remove_workspace(session.workspace)
        # A process-local workflow may still have a read in flight while its
        # cooperative cancellation task drains.
        if remove_workspace and not shared and workflow_active:
            self._deferred_workspace_cleanup[session_id] = session.workspace
            if cancellation_tasks:
                cleanup = asyncio.create_task(
                    self._drain_workspace_cleanup(
                        session_id,
                        cancellation_tasks,
                    )
                )
                self._cleanup_tasks.add(cleanup)
                cleanup.add_done_callback(self._cleanup_tasks.discard)
        return True

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: fleet bookkeeping is bounded by explicit caps (owners, protocols) whose overflow behavior tests pin; a runtime census would rescan every session per request."
)
