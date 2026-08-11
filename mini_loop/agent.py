"""The agent -- one async loop, complete capabilities, zero global state.

This is the s01 loop in spirit:

    while True:
        response = LLM(messages, tools)
        append assistant turn
        if there are no tool_use blocks: return
        execute tools; append results

Every capability is now a *swappable seam* rather than baked into the loop:

    tools          a ToolRegistry          (builtins.py: bash/read/write/edit/glob/
                                             TodoWrite/task/load_skill/compress)
    hooks          a Hooks chain           (permissions, audit, transforms)
    system prompt  a system_builder(agent) (prompts.py)
    compaction     a Compactor             (compaction.py)
    skills         a SkillLoader           (skills.py)
    LLM            an injected client      (config.py)

See EXTENDING.md for how to replace each one without touching this file.
"""

from __future__ import annotations

import asyncio
import copy
import hashlib
import json
import time
import uuid
from collections import deque
from collections.abc import Awaitable, Callable
from contextvars import ContextVar
from dataclasses import replace
from pathlib import Path

from .actions import TERMINAL_STATUSES as _REPLAYABLE_STATUSES
from .actions import InMemoryActionJournal
from .builtins import default_registry
from .blocks import block_field, block_text
from .caching import CachePolicy, DefaultCachePolicy, runtime_facts_injector
from .compaction import Compactor, DefaultCompactor, estimate_tokens, microcompact  # re-exported
from .metering import TokenMeter, prompt_tokens
from .transport import DirectTransport
from .config import Settings
from .actions import RECONCILED_RESULT, UNKNOWN_RESULT
from .harness import Harness
from .permissions import default_hooks
from .prompts import default_system_builder
from .recovery import DefaultRecovery
from .registry import Hooks, ToolCall, ToolContext, ToolRegistry
from .run_context import RunContext
from .sandbox import NullSandbox
from .secrets import NullSecretRegistry
from .skills import SkillLoader
from .stuck import (
    STUCK_WINDOW,
    DefaultStuckDetector,
    StuckDetector,
    StuckSignal,
    ToolStep,
    step_hash,
)
from .tool_policy import DEFAULT_ROLE_TOOL_POLICY, RoleToolPolicy
from .token_efficiency import (
    MaskedObservation,
    MaskedRawArtifactStore,
    OptimizationStatus,
    RequestContext,
    ResponsePolicyContext,
    StableRequestSettings,
    TokenEfficiencyRegistry,
    TokenEfficiencyRuntime,
)
from .token_tools import (
    RAW_ARTIFACT_TOOL,
    install_token_efficiency_tools,
    render_recovery_marker,
)
from .tools import CommandResult, Toolset

# An injector is `async (agent) -> list[message]` run at the top of each loop
# pass; it returns messages to splice into history (e.g. background results,
# fired cron prompts). See background.py / cron.py.
Injector = Callable[["Agent"], Awaitable[list]]

__all__ = ["Agent", "TodoManager", "microcompact", "estimate_tokens"]

EmitFn = Callable[[dict], Awaitable[None]]
DISPLAY_CAP = 2000   # how much of a tool result to surface in an event

#: Stop reasons this harness knows how to act on. The loop decides by *content*
#: -- run the tool_use blocks, stop when there are none -- which is right when a
#: provider disagrees with itself about `end_turn` versus `tool_use`. It is
#: wrong for reasons that say something the content cannot: a paused turn and a
#: finished one both arrive with no tool blocks. Anything not named here is
#: still returned to the caller, but is reported rather than passing as an
#: ordinary completion.
KNOWN_STOP_REASONS = frozenset({
    "end_turn", "tool_use", "max_tokens", "stop_sequence", "pause_turn",
    "refusal",
})

#: Reasons that mean "send this back to me", not "I am done".
RESUMABLE_STOP_REASONS = frozenset({"pause_turn"})

#: A provider that keeps pausing must not loop forever. Each resumption is a
#: real request, so this is a spend limit as much as a liveness one.
MAX_RESUMPTIONS = 8

#: Returned when the provider refuses and sends no content. Attributed to the
#: harness rather than phrased as the model speaking, because the model said
#: nothing -- that is what a refusal is.
REFUSAL_NOTICE = (
    "[the model declined to answer this request and returned no content]"
)
_CURRENT_RUN_CONTEXT: ContextVar[RunContext | None] = ContextVar(
    "mini_loop_current_run_context",
    default=None,
)


def _tool_action_id(
    *,
    session_id: str,
    run_context: RunContext,
    call: ToolCall,
) -> str:
    """Return a replay-stable id when the provider supplied a tool-use id."""

    if not call.id:
        return f"act_{uuid.uuid4().hex}"
    identity = "\0".join(
        (session_id, run_context.message_id, call.id, call.name)
    )
    return f"act_{hashlib.sha256(identity.encode()).hexdigest()}"


def _usage_payload(response) -> dict | None:
    usage = getattr(response, "usage", None)
    if usage is None:
        return None
    if hasattr(usage, "model_dump"):
        return usage.model_dump()
    if hasattr(usage, "to_dict"):
        return usage.to_dict()
    if hasattr(usage, "__dict__"):
        return {
            key: value for key, value in vars(usage).items()
            if not key.startswith("_")
        }
    return {"value": str(usage)}


def _injected_messages(extra, source) -> list:
    """Check an injector's return before it reaches the transcript.

    `messages.extend(...)` on a *string* appends its characters. An injector
    that returns `"note"` turns the transcript into four one-character messages,
    the conversation is destroyed in place, and the first thing anyone notices
    is an `AttributeError: 'str' object has no attribute 'get'` raised inside
    the compactor -- a module with nothing to do with injectors.

    Injectors are an extension point, so the wrong shape is a mistake someone
    outside this file will make. The seam checks its own contract and names who
    broke it. Loud, because the alternative is corrupt shared state: this is a
    bug in an extension, not a runtime condition to degrade around.
    """

    name = getattr(source, "__name__", None) or type(source).__name__
    if isinstance(extra, (str, bytes)) or not isinstance(extra, (list, tuple)):
        raise TypeError(
            f"injector {name!r} returned {type(extra).__name__}; expected a list "
            "of message dicts (or None). A string would be appended one "
            "character per message."
        )
    for index, message in enumerate(extra):
        if not (isinstance(message, dict) and message.get("role")):
            raise TypeError(
                f"injector {name!r} returned a non-message at index {index}: "
                f"{message!r:.80}. Each entry needs a 'role'."
            )
    return list(extra)


def unanswered_tool_uses(messages: list) -> list[str]:
    """Tool-use ids at the tail that no tool_result answers.

    A process killed between dispatching a tool and recording its result
    leaves exactly this shape. The provider rejects it outright --
    "`tool_use` ids were found without `tool_result` blocks immediately
    after" -- so a session restored in this state 400s on every subsequent
    turn.
    """

    if not messages:
        return []
    last = messages[-1]
    if not isinstance(last, dict) or last.get("role") != "assistant":
        return []
    content = last.get("content")
    if not isinstance(content, list):
        return []
    # Both shapes: a restored transcript holds dicts, a live one holds
    # provider block objects. Written for the first, this method silently
    # found nothing on the second -- so cancelling a live turn repaired
    # nothing, which only a real provider would have complained about.
    return [
        block_field(block, "id")
        for block in content
        if block_field(block, "type") == "tool_use" and block_field(block, "id")
    ]

def _close_unanswered_tools(self) -> list[str]:
    """Answer dangling tool calls with an explicit *unknown* outcome.

    The tool was dispatched; whether it completed is genuinely unknown, and
    reporting it as an error would invite the model to retry a side effect
    that may already have happened. Saying so plainly is what makes the
    difference: given this result a real model verifies before acting.
    """

    agent = self.agent
    if agent is None:
        return []
    pending = self._unanswered_tool_uses(agent.messages)
    if not pending:
        return []
    agent.messages.append({
        "role": "user",
        "content": [
            {
                "type": "tool_result",
                "tool_use_id": tool_use_id,
                "content": UNKNOWN_RESULT,
            }
            for tool_use_id in pending
        ],
    })
    return list(pending)



def _block(block, field: str, default=None):
    """Shape-agnostic block read. See `blocks.py` for why this exists."""

    return block_field(block, field, default)


def _content_payload(blocks: list) -> list:
    payload = []
    for block in blocks:
        if isinstance(block, dict):
            payload.append(block)
        elif hasattr(block, "model_dump"):
            payload.append(block.model_dump())
        elif getattr(block, "type", None) == "text":
            payload.append({"type": "text", "text": getattr(block, "text", "")})
        elif getattr(block, "type", None) == "tool_use":
            payload.append({
                "type": "tool_use",
                "id": getattr(block, "id", None),
                "name": getattr(block, "name", None),
                "input": getattr(block, "input", {}),
            })
        else:
            # Keep whatever the block carries. This branch used to reduce an
            # unrecognized block to its type alone, which is silently lossy in
            # the worst way for `thinking`: the signature is dropped, and the
            # next turn sends back a thinking block the API rejects. The list of
            # types this does not name explicitly only grows -- redacted
            # thinking, server tool use, search results -- so the default has to
            # be "preserve", not "summarize".
            #
            # Real SDK blocks never reach here (they have `model_dump`); a
            # provider adapter that yields plain objects does.
            fields = {
                key: value
                for key, value in (vars(block) if hasattr(block, "__dict__") else {}).items()
                if not key.startswith("_")
            }
            fields["type"] = getattr(block, "type", "unknown")
            payload.append(fields)
    return payload


def _messages_payload(messages: list) -> list:
    payload = []
    for message in messages:
        if not isinstance(message, dict):
            payload.append(message)
            continue
        item = dict(message)
        if isinstance(item.get("content"), list):
            item["content"] = _content_payload(item["content"])
        payload.append(item)
    return payload


def _latest_text_task(messages: list[dict]) -> str:
    """Best-effort task label for response policy, excluding tool results."""

    for message in reversed(messages):
        if message.get("role") != "user":
            continue
        content = message.get("content")
        if isinstance(content, str):
            return content
        if isinstance(content, list):
            text_parts = [
                str(part.get("text", ""))
                for part in content
                if isinstance(part, dict) and part.get("type") == "text"
            ]
            if text_parts:
                return "\n".join(text_parts)
    return ""


def _append_system_instructions(system, instructions: tuple[str, ...]):
    """Return a detached provider-neutral system value with stable guidance."""

    if not instructions:
        return system
    addition = "\n\n".join(instructions)
    if system is None:
        return addition
    if isinstance(system, str):
        return f"{system}\n\n{addition}"
    # Provider block form. Copy the outer blocks so policy never mutates the
    # caller's cached prefix in place.
    if isinstance(system, list):
        return [*system, {"type": "text", "text": addition}]
    return system


def _message_protocol_shape(messages) -> tuple | None:
    """Provider-sensitive message bytes an optimizer must not mutate.

    Only ordinary text and the payload of a tail ``tool_result`` are eligible
    for request-context reduction.  Tool calls, thinking/signature blocks,
    media, cache metadata, and unknown block types remain byte-stable.
    """

    def canonical(value) -> str:
        try:
            return json.dumps(
                value,
                ensure_ascii=False,
                sort_keys=True,
                separators=(",", ":"),
                allow_nan=False,
            )
        except (TypeError, ValueError):
            return repr(value)

    if not isinstance(messages, list):
        return None
    shape = []
    for message in messages:
        if not isinstance(message, dict) or message.get("role") not in {
            "user", "assistant"
        }:
            return None
        message_metadata = {
            key: value for key, value in message.items() if key != "content"
        }
        blocks = message.get("content")
        protocol = []
        if isinstance(blocks, list):
            for block in blocks:
                if not isinstance(block, dict):
                    protocol.append(("opaque", canonical(block)))
                    continue
                kind = block.get("type")
                if kind == "text":
                    stable = {key: value for key, value in block.items() if key != "text"}
                    protocol.append((kind, canonical(stable)))
                elif kind == "tool_result":
                    stable = {
                        key: value for key, value in block.items() if key != "content"
                    }
                    protocol.append((kind, canonical(stable)))
                else:
                    # Includes tool_use, thinking/signature, images and any new
                    # provider protocol block we do not yet understand.
                    protocol.append((str(kind), canonical(block)))
        elif isinstance(blocks, str):
            protocol.append(("text",))
        else:
            protocol.append(("opaque-content", canonical(blocks)))
        shape.append((canonical(message_metadata), tuple(protocol)))
    return tuple(shape)


def _canonical_message_digest(message: dict) -> str:
    """Identity for one authoritative message in the projection ledger."""

    try:
        payload = json.dumps(
            message,
            ensure_ascii=False,
            sort_keys=True,
            separators=(",", ":"),
            allow_nan=False,
        ).encode("utf-8")
    except (TypeError, ValueError):
        payload = repr(message).encode("utf-8", errors="replace")
    return hashlib.sha256(payload).hexdigest()


def _text_digest(value: str) -> str:
    return f"sha256:{hashlib.sha256(value.encode('utf-8')).hexdigest()}"


def _token_estimate(value: str) -> int:
    size = len(value.encode("utf-8"))
    return 0 if not size else max(1, (size + 3) // 4)


def _observation_content_type(value: str) -> str:
    stripped = value.lstrip()
    if stripped.startswith(("{", "[")):
        try:
            json.loads(value)
        except (TypeError, json.JSONDecodeError):
            pass
        else:
            return "application/json"
    return "text/plain"


# --- s05: TodoWrite ---------------------------------------------------------

#: Per-field cap on a todo's text. The count is capped at 20 below, but the
#: board renders into `runtime_facts` and re-injects on every change, so an
#: uncapped `content` or `activeForm` floods the context on each edit -- the
#: count bound applied and the size bound not, the shape round 50/132 kept
#: finding one sink over.
MAX_TODO_FIELD = 2_000


class TodoManager:
    def __init__(self) -> None:
        self.items: list[dict] = []

    def update(self, items: list) -> str:
        validated, in_progress = [], 0
        for i, item in enumerate(items):
            content = str(item.get("content", "")).strip()
            status = str(item.get("status", "pending")).lower()
            active = str(item.get("activeForm", "")).strip()
            if not content:
                raise ValueError(f"Item {i}: content required")
            if status not in ("pending", "in_progress", "completed"):
                raise ValueError(f"Item {i}: invalid status '{status}'")
            if not active:
                raise ValueError(f"Item {i}: activeForm required")
            if len(content) > MAX_TODO_FIELD:
                content = content[:MAX_TODO_FIELD] + " [truncated]"
            if len(active) > MAX_TODO_FIELD:
                active = active[:MAX_TODO_FIELD] + " [truncated]"
            if status == "in_progress":
                in_progress += 1
            validated.append({"content": content, "status": status, "activeForm": active})
        if len(validated) > 20:
            raise ValueError("Max 20 todos")
        if in_progress > 1:
            raise ValueError("Only one in_progress allowed")
        self.items = validated
        return self.render()

    def render(self) -> str:
        if not self.items:
            return "No todos."
        glyph = {"completed": "[x]", "in_progress": "[>]", "pending": "[ ]"}
        lines = []
        for it in self.items:
            suffix = f" <- {it['activeForm']}" if it["status"] == "in_progress" else ""
            lines.append(f"{glyph.get(it['status'], '[?]')} {it['content']}{suffix}")
        done = sum(1 for t in self.items if t["status"] == "completed")
        lines.append(f"\n({done}/{len(self.items)} completed)")
        return "\n".join(lines)

    def has_open_items(self) -> bool:
        return any(it.get("status") != "completed" for it in self.items)

    def snapshot(self) -> list[dict]:
        return list(self.items)


class _Unbounded:
    """Stand-in for an absent LLM semaphore."""

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_):
        return False


class Agent:
    """A single conversational agent. Reused recursively for subagents."""

    def __init__(
        self,
        *,
        client,
        settings: Settings,
        workspace: Path,
        skills: SkillLoader | None = None,
        tools: ToolRegistry | None = None,
        hooks: Hooks | None = None,
        system: str | None = None,
        system_builder: Callable[["Agent"], str] | None = None,
        compactor: Compactor | None = None,
        transport=None,
        recovery=None,
        stuck_detector: StuckDetector | None = None,
        cache_policy: CachePolicy | None = None,
        secrets=None,
        sandbox=None,
        token_efficiency: TokenEfficiencyRuntime | None = None,
        role_tool_policy: RoleToolPolicy | None = None,
        harness: Harness | None = None,
        injectors: list[Injector] | None = None,
        emit: EmitFn | None = None,
        llm_semaphore=None,
        tool_semaphore=None,
        label: str = "main",
        depth: int = 0,
        max_rounds: int | None = None,
        state: dict | None = None,
    ) -> None:
        self.client = client
        self.settings = settings
        self.workspace = Path(workspace)
        self.state: dict = state if state is not None else {}
        self.emit = emit
        self.semaphore = llm_semaphore or _Unbounded()
        self.tool_semaphore = (
            tool_semaphore
            if tool_semaphore is not None
            else asyncio.Semaphore(settings.max_concurrent_tools)
        )
        # One value carries the policy set; explicit kwargs still win. Children
        # derive from `self.harness`, so a seam added to Harness reaches every
        # construction site without editing any of them.
        self.harness = harness or Harness()
        tools = self.harness.resolve("tools", tools)
        catalog_was_supplied = tools is not None
        hooks = self.harness.resolve("hooks", hooks)
        skills = self.harness.resolve("skills", skills)
        system_builder = self.harness.resolve("system_builder", system_builder)
        compactor = self.harness.resolve("compactor", compactor)
        recovery = self.harness.resolve("recovery", recovery)
        stuck_detector = self.harness.resolve("stuck_detector", stuck_detector)
        cache_policy = self.harness.resolve("cache_policy", cache_policy)
        secrets = self.harness.resolve("secrets", secrets)
        sandbox = self.harness.resolve("sandbox", sandbox)
        token_efficiency = self.harness.resolve(
            "token_efficiency", token_efficiency
        )
        role_tool_policy = self.harness.resolve(
            "role_tool_policy", role_tool_policy
        )
        if injectors is None and self.harness.injectors:
            injectors = list(self.harness.injectors)

        self.label = label
        self.depth = depth
        self.max_rounds = max_rounds if max_rounds is not None else settings.max_turns
        self.skills = skills or SkillLoader(settings.skills_dir)

        # Assigned before the Toolset that consumes it.
        self.secrets = secrets if secrets is not None else NullSecretRegistry()
        self.sandbox = sandbox if sandbox is not None else NullSandbox()
        self.toolset = Toolset(
            self.workspace,
            bash_timeout=settings.bash_timeout,
            secrets=self.secrets,
            sandbox=self.sandbox,
        )
        self.todo = TodoManager()
        self.tools = tools if tools is not None else default_registry()
        self.role_tool_policy = role_tool_policy or DEFAULT_ROLE_TOOL_POLICY
        self.token_efficiency = (
            token_efficiency
            if token_efficiency is not None
            else TokenEfficiencyRegistry().runtime()
        )
        existing_raw_store = self.token_efficiency.raw_store
        if existing_raw_store is not None:
            try:
                store_workspace = Path(existing_raw_store.workspace).resolve()
            except (AttributeError, OSError, TypeError):
                store_workspace = None
            if store_workspace != self.workspace.resolve():
                # A runtime can be shared as an immutable component template,
                # but its raw authority cannot cross a workspace/session edge.
                self.token_efficiency = self.token_efficiency.with_raw_store(None)
        # Raw artifacts are authority-adjacent and therefore session scoped.
        # The manager shares an immutable component template; each Agent binds
        # a store rooted in its own workspace only when an enforced observation
        # reducer can actually return a projection.
        if (
            self.token_efficiency.observation_enforced
            and getattr(settings, "token_efficiency_persist_raw", True)
            and self.state.get("permission_mode") != "readonly"
            and (not catalog_was_supplied or RAW_ARTIFACT_TOOL in self.tools)
        ):
            if self.token_efficiency.raw_store is None:
                raw_store = MaskedRawArtifactStore(
                    self.workspace,
                    ttl_seconds=getattr(
                        settings,
                        "token_efficiency_artifact_ttl_seconds",
                        3_600,
                    ),
                    max_artifact_bytes=getattr(
                        settings,
                        "token_efficiency_max_artifact_bytes",
                        2_000_000,
                    ),
                    max_total_bytes=getattr(
                        settings,
                        "token_efficiency_max_total_bytes",
                        20_000_000,
                    ),
                )
                self.token_efficiency = self.token_efficiency.with_raw_store(
                    raw_store
                )
            # Only a genuinely bare default catalogue is widened implicitly.
            # Manager/harness and child-role catalogues are supplied policy
            # results; their constructor must not add authority back.
            if not catalog_was_supplied:
                install_token_efficiency_tools(self.tools)
        # Ensure the two new seams survive grandchildren even for a bare Agent
        # constructed with explicit overrides rather than a manager Harness.
        self.harness = self.harness.derive(
            token_efficiency=self.token_efficiency,
            role_tool_policy=self.role_tool_policy,
        )
        self.hooks = hooks if hooks is not None else default_hooks()
        self.compactor = compactor or DefaultCompactor()
        # Fed from every response's usage; read by the compactor. Not a seam:
        # it is a measurement, not a policy -- what to do about a full context
        # is the compactor's decision and that already swaps.
        self.token_meter = TokenMeter()
        self.transport = self.harness.resolve("transport", transport) or DirectTransport()
        #: Text a streaming transport has emitted for the current generation.
        #: Empty for `DirectTransport`, which shows nothing before it finishes.
        self.streamed_text = ""
        self.recovery = recovery or DefaultRecovery()
        self.stuck_detector = stuck_detector or DefaultStuckDetector()
        self.cache_policy = cache_policy or DefaultCachePolicy()
        self.injectors: list[Injector] = list(injectors or [])
        # Volatile runtime state rides the message stream instead of the system
        # prompt, so the cached prefix survives a changing todo board.
        if runtime_facts_injector not in self.injectors:
            self.injectors.append(runtime_facts_injector)
        # System prompt: explicit string wins, else build from the agent.
        self.system_builder = system_builder or default_system_builder
        self._dynamic_system = system is None
        self._system = system if system is not None else self.system_builder(self)

        self.messages: list[dict] = []
        # Provider-facing request reductions are projections, not transcript
        # authority.  Once a tail is projected it becomes the next request's
        # cache prefix, so retain that exact projection by authoritative
        # message digest and stable append index.
        self._request_projection_ledger: dict[tuple[int, str], dict] = {}
        # Bound only while one provider request is being assembled. The system
        # builder and `tools=` payload then consume the exact same fitted view.
        self._request_tool_catalog = None
        self.last_text: str = ""
        self._last_model_span_id: str | None = None
        self._rounds_without_todo = 0
        self._pending_compact = False

        # Loop-detection ledger. Bounded, ordered by execution, and reset per
        # user turn -- like the upstream detector, repetition is only
        # interesting relative to the current intent.
        self._recent_steps: deque[ToolStep] = deque(maxlen=STUCK_WINDOW)
        self._rounds_without_tools = 0
        self._resumptions = 0
        self._stuck_nudges = 0
        #: One turn at a time per agent. See `run`.
        self._turn_lock = asyncio.Lock()
        #: Tool uses closed as unknown since the last report. See `run`.
        self._repaired_tool_uses: list[str] = []

    @property
    def recent_steps(self) -> tuple[ToolStep, ...]:
        """Recently executed tool calls, oldest first, for loop detection."""

        return tuple(self._recent_steps)

    @property
    def rounds_without_tools(self) -> int:
        """Consecutive model turns that emitted no tool call."""

        return self._rounds_without_tools

    @property
    def system(self) -> str:
        return self._system

    @system.setter
    def system(self, value: str) -> None:
        # Direct assignment is an explicit override. This keeps the public API
        # backward compatible while allowing builder-based prompts to refresh.
        self._system = value
        self._dynamic_system = False

    def refresh_system(self) -> str:
        if self._dynamic_system:
            self._system = self.system_builder(self)
        return self._system

    def use_system_builder(self, builder: Callable[["Agent"], str]) -> None:
        """Switch back to a per-call prompt builder after a fixed override."""
        self.system_builder = builder
        self._dynamic_system = True
        self.refresh_system()

    def enter_workspace(self, workspace: Path) -> None:
        """Switch this agent's file tools to an already-provisioned workspace."""
        self.toolset = Toolset(
            Path(workspace),
            bash_timeout=self.settings.bash_timeout,
            secrets=self.secrets,
            sandbox=self.sandbox,
        )
        self.workspace = self.toolset.workspace
        background = self.state.get("background")
        if background is not None:
            background.workspace = self.workspace
            # Re-confine, not just re-point. The background sandbox was bound to
            # the old workspace, so after a worktree switch a background command
            # ran in the new workspace but was confined to the old one -- unable
            # to write its own worktree, yet still able to write the one it left,
            # defeating the isolation entering a worktree exists to provide.
            # `run_bash` is re-bound by the new Toolset above; its background
            # sibling has to move with it -- the same run_bash/background_run
            # parity `test_background_parity.py` exists to keep.
            background.sandbox = self.sandbox.for_workspace(self.workspace)

    async def _send(self, event_type: str, **fields) -> None:
        if self.emit is None:
            return
        await self.emit({**fields, "type": event_type, "agent": self.label, "depth": self.depth})

    async def _send_optimization_receipts(
        self,
        receipts,
        *,
        parent_span_id: str | None = None,
    ) -> None:
        """Emit provenance/metrics without placing observation content in events."""

        for receipt in receipts:
            payload = receipt.as_dict()
            warning_count = len(getattr(receipt, "warnings", ()))
            for unsafe_field in (
                "warnings",
                "input_digest",
                "output_digest",
                "raw_digest",
            ):
                payload.pop(unsafe_field, None)
            payload["warning_count"] = warning_count
            await self._send(
                "optimization_receipt",
                parent_span_id=parent_span_id,
                **payload,
            )

    async def _create(
        self,
        messages,
        *,
        tools=None,
        system=None,
        max_tokens=None,
        purpose: str = "agent_turn",
        tool_catalog_fingerprint: str | None = None,
    ):
        # Recorded before annotation: the policy hands back a copy, so this is
        # the only point where "is this request the live conversation?" is knowable.
        live_history = self.messages if messages is self.messages else None
        requested_max_tokens = max_tokens or self.settings.max_tokens

        # Response policy produces stable provider-neutral guidance. It runs
        # before cache annotation so an opt-in policy becomes part of the
        # reusable prefix rather than a provider-specific mutation afterward.
        if purpose == "agent_turn":
            response_outcome = await self.token_efficiency.plan_response(
                ResponsePolicyContext(
                    task=_latest_text_task(messages),
                    settings=StableRequestSettings(
                        max_output_tokens=requested_max_tokens
                    ),
                    budget_tokens=requested_max_tokens,
                    concise_requested=bool(
                        self.state.get("concise_response", False)
                    ),
                )
            )
            await self._send_optimization_receipts(response_outcome.receipts)
            response_settings = response_outcome.context.settings
            system = _append_system_instructions(
                system, response_settings.instructions
            )
            if response_settings.max_output_tokens is not None:
                requested_max_tokens = min(
                    requested_max_tokens, response_settings.max_output_tokens
                )

        # Re-apply earlier provider projections before optimizing the newest
        # delta.  Without this ledger, round N's compressed tail silently
        # expands back to authoritative bytes when it becomes round N+1's
        # prefix, defeating provider cache stability.
        provider_messages = messages
        if purpose == "agent_turn" and live_history is not None:
            provider_messages = copy.deepcopy(messages)
            valid_keys: set[tuple[int, str]] = set()
            for index, authoritative in enumerate(messages):
                if not isinstance(authoritative, dict):
                    continue
                key = (index, _canonical_message_digest(authoritative))
                valid_keys.add(key)
                projected = self._request_projection_ledger.get(key)
                if projected is not None:
                    provider_messages[index] = copy.deepcopy(projected)
            self._request_projection_ledger = {
                key: value
                for key, value in self._request_projection_ledger.items()
                if key in valid_keys
            }

        # Request optimizers receive a detached message copy and may only
        # transform the newest delta. The runtime protects the frozen prefix;
        # this provider-facing guard additionally preserves role count and
        # tool-use/result identities so an optimizer cannot break pairing.
        original_shape = _message_protocol_shape(provider_messages)
        latest_role = (
            messages[-1].get("role")
            if messages and isinstance(messages[-1], dict)
            else None
        )
        frozen_prefix_messages = (
            len(messages)
            if latest_role == "assistant"
            else max(0, len(messages) - 1)
        )
        request_outcome = await self.token_efficiency.optimize_request(
            RequestContext(
                request={"messages": provider_messages},
                frozen_prefix_messages=frozen_prefix_messages,
            ),
            budget_tokens=self.settings.token_threshold,
        )
        await self._send_optimization_receipts(request_outcome.receipts)
        optimized_messages = request_outcome.context.request.get("messages")
        if (
            original_shape is not None
            and _message_protocol_shape(optimized_messages) == original_shape
        ):
            messages = optimized_messages
        else:
            await self._send(
                "request_optimization_rejected",
                reason="message_protocol_guard",
            )
            messages = provider_messages
        if purpose == "agent_turn" and live_history is not None:
            for index, (authoritative, projected) in enumerate(
                zip(live_history, messages, strict=False)
            ):
                if not isinstance(authoritative, dict) or not isinstance(projected, dict):
                    continue
                key = (index, _canonical_message_digest(authoritative))
                if _canonical_message_digest(projected) == key[1]:
                    self._request_projection_ledger.pop(key, None)
                else:
                    self._request_projection_ledger[key] = copy.deepcopy(projected)
        # Breakpoints are placed on per-request copies; `self.messages` stays
        # free of provider-specific keys so history remains portable.
        system, tools, messages = self.cache_policy.annotate(
            system=system,
            tools=tools,
            messages=messages,
        )
        kwargs: dict = {
            "model": self.state.get("recovery_model", self.settings.model),
            "messages": messages,
            "max_tokens": requested_max_tokens,
        }
        if system is not None:
            kwargs["system"] = system
        if tools is not None:
            kwargs["tools"] = tools

        async def call(kw: dict):
            async with self.semaphore:   # backoff sleeps happen OUTSIDE the slot
                return await self.transport.send(self, kw)

        span_id = f"model_{uuid.uuid4().hex[:16]}"
        self._last_model_span_id = span_id
        started = time.monotonic()
        await self._send(
            "model_start",
            span_id=span_id,
            purpose=purpose,
            model=kwargs["model"],
            message_count=len(messages),
            input_tokens_estimate=estimate_tokens(messages),
            tool_count=len(tools or []),
            tool_catalog_fingerprint=tool_catalog_fingerprint,
            max_tokens=kwargs["max_tokens"],
            _trajectory_fields={
                "model_input": {
                    "messages": _messages_payload(messages),
                    "system": system,
                    "tools": tools,
                    "max_tokens": kwargs["max_tokens"],
                },
            },
        )
        try:
            response = await self.recovery.run(
                self, kwargs, call, live_history=live_history
            )
        except asyncio.CancelledError:
            await self._send(
                "model_end",
                span_id=span_id,
                purpose=purpose,
                status="cancelled",
                duration_ms=round((time.monotonic() - started) * 1000, 3),
            )
            raise
        except Exception as error:
            await self._send(
                "model_end",
                span_id=span_id,
                purpose=purpose,
                status="error",
                duration_ms=round((time.monotonic() - started) * 1000, 3),
                error=f"{type(error).__name__}: {error}"[:500],
            )
            raise
        usage = getattr(response, "usage", None)
        measured_prompt_tokens = prompt_tokens(usage)
        # The conversation meter models one stable prefix plus growth in
        # `self.messages`. Memory selection, extraction, consolidation and
        # compaction summaries use unrelated message lists (and often no tools
        # or system prompt); observing them would replace the live anchor with
        # a different request shape. Their own usage still belongs on the
        # model_end event, just not in the live conversation meter.
        if purpose == "agent_turn" and live_history is not None:
            self.token_meter.observe(usage, live_history)
        await self._send(
            "model_end",
            span_id=span_id,
            purpose=purpose,
            status="completed",
            duration_ms=round((time.monotonic() - started) * 1000, 3),
            stop_reason=getattr(response, "stop_reason", None),
            usage=_usage_payload(response),
            prompt_tokens=measured_prompt_tokens,
            tool_catalog_fingerprint=tool_catalog_fingerprint,
            token_meter=self.token_meter.snapshot(),
            _trajectory_fields={
                "model_output": _content_payload(response.content),
            },
        )
        return response

    # -- public entry: run one user turn to completion, return final text --
    @property
    def current_run_context(self) -> RunContext | None:
        """Context for the current async task, if an agent is running."""

        return _CURRENT_RUN_CONTEXT.get()

    async def run(
        self,
        user_text: str,
        run_context: RunContext | None = None,
    ) -> str:
        """Run one user turn to completion and return the final text.

        Turns on one agent are serialized. `self.messages` is a single mutable
        transcript, and two `run()` calls interleaving their appends produce a
        shape the provider refuses -- a `tool_use` block with somebody else's
        user message where its `tool_result` belongs. Measured on four
        concurrent calls to one session: five provider requests, four rejected
        with `InvalidTranscript`, all four callers handed the same error, and
        the transcript left permanently malformed.

        A server gets this from an ordinary double-submit or a reconnect, so it
        is not an exotic input. Queueing rather than refusing, because the
        second request is almost always something the user meant to ask; the
        wait is reported so that a caller blocked behind a long turn is not
        left guessing.
        """

        if self._turn_lock.locked():
            await self._send("turn_queued")
        async with self._turn_lock:
            try:
                return await self._run_one_turn(user_text, run_context)
            except asyncio.CancelledError:
                # A cancel between dispatching a tool and recording its result
                # leaves a `tool_use` the provider refuses to see unanswered,
                # and the session carries that shape forward -- every later turn
                # returns `[Error] InvalidTranscript`. `Session.cancel` already
                # repaired this, and so did restore, but a cancellation arriving
                # from *outside* -- an HTTP client disconnecting, a `wait_for`
                # timeout -- reached neither. The invariant belongs to whoever
                # owns the transcript, which is this object.
                self.close_unanswered_tools()
                # Cancelling the await abandoned the worker thread, not the
                # shell inside it: without this the command burns on until
                # bash_timeout. Both halves of stopping a turn -- the model
                # stream and the foreground shell -- belong to the same event.
                self.toolset.interrupt()
                raise

    def close_unanswered_tools(self, overrides=None) -> list[str]:
        """Answer dangling tool calls with an explicit *unknown* outcome.

        The tool was dispatched; whether it completed is genuinely unknown, and
        reporting it as an error would invite the model to retry a side effect
        that may already have happened. Saying so plainly is what makes the
        difference: given this result a real model verifies before acting.

        `overrides` maps tool_use_id -> result text for the calls where the
        caller knows better than "unknown" -- restore uses it to answer a call
        that was parked on an approval as *not run* (safe to retry), which is
        the opposite advice from unknown (do not retry).

        Idempotent, so the session-level repair that already ran on its own
        cancel path finds nothing left to do.
        """

        pending = unanswered_tool_uses(self.messages)
        if not pending:
            return []
        # Held for whoever reports the cancellation. Moving the repair earlier
        # meant the session's own call found nothing left to do and its
        # `cancelled` event started saying no tools were left unknown -- the
        # repair kept working and the report of it stopped.
        self._repaired_tool_uses.extend(pending)
        self.messages.append({
            "role": "user",
            "content": [
                {
                    "type": "tool_result",
                    "tool_use_id": tool_use_id,
                    "content": (overrides or {}).get(tool_use_id, UNKNOWN_RESULT),
                }
                for tool_use_id in pending
            ],
        })
        return list(pending)

    def take_repaired_tool_uses(self) -> list[str]:
        """Tool uses closed as unknown since the last call, and clear them."""

        taken, self._repaired_tool_uses = list(self._repaired_tool_uses), []
        return taken

    async def _run_one_turn(
        self,
        user_text: str,
        run_context: RunContext | None = None,
    ) -> str:
        resolved_context = run_context or RunContext.default()
        token = _CURRENT_RUN_CONTEXT.set(resolved_context)
        # A new user turn is a new intent: repetition before it is not evidence
        # that the model is stuck on *this* request.
        self._recent_steps.clear()
        self._rounds_without_tools = 0
        self._stuck_nudges = 0
        # Per turn, like the two above and for the same reason. Introduced in
        # round 84 as a plain instance counter, which quietly made it a
        # *session lifetime* budget: after eight paused turns spread over an
        # afternoon, every later pause was returned to the caller as a finished
        # answer -- the exact bug the counter was added to prevent, arriving
        # only in the long sessions nobody reproduces.
        self._resumptions = 0
        try:
            user_text = await self.hooks.user_prompt(self, user_text)
            # s09: index + selected bodies are loaded before the user turn.
            from .memory import prepare_memory_context

            user_text = await prepare_memory_context(self, user_text)
            self.messages.append({"role": "user", "content": user_text})
            await self._loop(resolved_context)
            return self.last_text
        finally:
            _CURRENT_RUN_CONTEXT.reset(token)

    # -- the loop --
    async def _loop(self, run_context: RunContext | None = None) -> None:
        resolved_context = (
            run_context or self.current_run_context or RunContext.default()
        )
        for _ in range(self.max_rounds):
            # Pre-turn injection: background results, fired cron prompts, etc.
            for inject in self.injectors:
                extra = await inject(self)
                if extra:
                    self.messages.extend(_injected_messages(extra, inject))

            # Every new notification goes through the same context-budget
            # pipeline before the model sees it.
            await self.compactor.maybe_compact(self)  # s08, pluggable

            catalog = self.tools.snapshot(report=True)
            self._request_tool_catalog = catalog
            try:
                response = await self._create(
                    self.messages,
                    tools=catalog.schemas(),
                    system=self.refresh_system(),
                    purpose="agent_turn",
                    tool_catalog_fingerprint=catalog.fingerprint,
                )
            except Exception as error:
                detail = f"{type(error).__name__}: {error}"[:500]
                self.last_text = f"[Error] {detail}"
                self.messages.append({
                    "role": "assistant",
                    "content": [{"type": "text", "text": self.last_text}],
                })
                await self._send("error", error=detail)
                return
            finally:
                self._request_tool_catalog = None
            # Normalized on the way in, not at each reader. Provider block
            # objects are not ordinary data structures, and four separate
            # traversals -- the store's serializer, the secret masker, the
            # trajectory writer, the dangling-tool-call scan -- were each
            # written for dicts and silently walked past them. Converting once,
            # here, removes the class rather than the instance.
            #
            # Verified against a provider that validates the round-trip: the
            # reasoner returns `thinking` blocks, requires them back, and
            # accepts the dict form with its signature intact.
            self.messages.append(
                {"role": "assistant", "content": _content_payload(response.content)}
            )

            text = block_text(response.content)
            if text:
                self.last_text = text
                await self._send("assistant_text", text=text)

            # Providers occasionally report an inconsistent stop_reason. The
            # protocol contract is the content itself: execute actual tool_use
            # blocks, and stop when none are present.
            tool_blocks = [
                block for block in response.content
                if _block(block, "type", "") == "tool_use"
            ]

            # ...but "no tool blocks" is not the same claim as "the turn is
            # over", and treating them as one made every stop reason outside an
            # implicit allowlist mean "done" silently. `pause_turn` says the
            # opposite -- the model was interrupted and is asking to be sent
            # back -- and it arrives with no tool blocks, so a paused turn was
            # returned to the caller as a finished answer.
            reason = getattr(response, "stop_reason", None)
            if not tool_blocks and reason in RESUMABLE_STOP_REASONS:
                self._resumptions += 1
                if self._resumptions <= MAX_RESUMPTIONS:
                    await self._send("turn_paused", stop_reason=reason,
                                     resumption=self._resumptions)
                    # The protocol resumption: hand the partial turn straight
                    # back. No user message -- inventing one would put words in
                    # the caller's mouth and change what the model continues.
                    continue
                await self._send(
                    "provider_stop_unhandled", stop_reason=reason,
                    detail=f"still paused after {MAX_RESUMPTIONS} resumptions",
                )

            if not tool_blocks and reason == "refusal":
                # A refusal arrives with no content by design, so the caller got
                # `""` -- indistinguishable from the model having nothing to say
                # or from the harness breaking. Naming it in the set above was
                # not enough: an entry nothing acts on is a vacuous entry.
                await self._send("provider_refusal", stop_reason=reason)
                if not self.last_text:
                    # Attributed to the harness, not spoken as the model. The
                    # alternative is returning empty and letting the caller
                    # invent an explanation.
                    self.last_text = REFUSAL_NOTICE

            if not tool_blocks and reason is not None and reason not in KNOWN_STOP_REASONS:
                # Not fatal: a new reason usually still carries a usable answer,
                # and refusing to return it would be worse than returning it.
                # But it must not look like an ordinary completion.
                await self._send("provider_stop_unhandled", stop_reason=reason,
                                 detail="unrecognized stop reason, treated as end of turn")

            if not tool_blocks:
                self._rounds_without_tools += 1
                continuation = await self.hooks.stop(self, self.messages, self.last_text)
                if continuation is not None:
                    signal = self.stuck_detector.inspect(self)
                    if signal is not None:
                        if not await self._nudge_or_halt(signal):
                            return
                        # A stop hook is resuming the model; the correction has
                        # to ride on that continuation or it never lands.
                        continuation = f"{signal.reminder()}\n\n{continuation}"
                    self.messages.append({"role": "user", "content": continuation})
                    continue
                from .memory import memory_on_stop

                await memory_on_stop(self)
                return

            self._rounds_without_tools = 0
            used_todo = any(_block(b, "name") == "TodoWrite" for b in tool_blocks)
            self._pending_compact = False
            results = await self._exec_tool_batch(
                tool_blocks,
                parent_span_id=self._last_model_span_id,
                run_context=resolved_context,
            )

            # s05 nag: a plan is open but the model drifted off TodoWrite.
            self._rounds_without_todo = 0 if used_todo else self._rounds_without_todo + 1
            if self.todo.has_open_items() and self._rounds_without_todo >= 3:
                results.append({"type": "text", "text": "<reminder>Update your todos.</reminder>"})
                self._rounds_without_todo = 0

            # Loop detection rides the same seam as the todo nag: results are
            # still mutable, so a nudge stays inside the tool_result block the
            # provider protocol requires.
            signal = self.stuck_detector.inspect(self)
            if signal is not None:
                if await self._nudge_or_halt(signal):
                    results.append({"type": "text", "text": signal.reminder()})
                else:
                    self.messages.append({"role": "user", "content": results})
                    return

            self.messages.append({"role": "user", "content": results})

            if self._pending_compact:
                await self.compactor.compact(self)
                self._pending_compact = False
                continue

        await self._send("error", error=f"Hit max_rounds ({self.max_rounds}) without finishing")
        self.last_text = self.last_text or f"[stopped after {self.max_rounds} rounds]"

    async def _nudge_or_halt(self, signal: StuckSignal) -> bool:
        """Report a stuck pattern. Return True to nudge, False to halt.

        The nudge budget is spent per user turn, so a model that ignores the
        correction cannot keep trading rounds for the same wall.
        """

        budget = int(getattr(self.stuck_detector, "max_nudges", 0))
        halted = self._stuck_nudges >= budget
        await self._send(
            "stuck",
            pattern=signal.pattern,
            detail=signal.detail,
            tool=signal.tool_name,
            halted=halted,
            nudges_used=self._stuck_nudges,
        )
        if halted:
            self.last_text = self.last_text or f"[stopped: {signal.detail}]"
            return False
        self._stuck_nudges += 1
        # The pattern has been answered. Drop the evidence so the very next
        # round does not re-fire on the same history and spend the budget
        # without the model ever getting a chance to act on the correction.
        self._recent_steps.clear()
        self._rounds_without_tools = 0
        return True

    async def _exec_tool_batch(
        self,
        blocks: list,
        *,
        parent_span_id: str | None = None,
        run_context: RunContext | None = None,
    ) -> list[dict]:
        """Execute one model-emitted tool batch without reordering results.

        Consecutive tools explicitly registered as ``parallel_safe`` run
        together under the process-wide semaphore. Every other tool is an
        ordering barrier, so reads never move across a write or unknown call.
        """
        calls = [
            ToolCall(
                _block(block, "name"),
                dict(_block(block, "input") or {}),
                _block(block, "id"),
            )
            for block in blocks
        ]
        results: list[dict] = []
        parallel_group: list[ToolCall] = []
        # Batch-local, so nothing accumulates on the agent between batches.
        ledger: dict[int, ToolStep] = {}

        async def result_for(call: ToolCall, *, limited: bool) -> dict:
            if limited:
                async with self.tool_semaphore:
                    output = await self._exec_tool(
                        call,
                        parent_span_id=parent_span_id,
                        run_context=run_context,
                        ledger=ledger,
                    )
            else:
                output = await self._exec_tool(
                    call,
                    parent_span_id=parent_span_id,
                    run_context=run_context,
                    ledger=ledger,
                )
            return {
                "type": "tool_result",
                "tool_use_id": call.id,
                "content": output,
            }

        async def flush_parallel_group() -> None:
            if not parallel_group:
                return
            # gather preserves input order even when completion order differs,
            # which is required by provider tool-result protocols.
            results.extend(
                await asyncio.gather(
                    *(result_for(call, limited=True) for call in parallel_group)
                )
            )
            parallel_group.clear()

        for call in calls:
            tool = self.tools.get(call.name)
            if tool is not None and tool.parallel_safe:
                parallel_group.append(call)
                continue

            await flush_parallel_group()
            results.append(await result_for(call, limited=False))

        await flush_parallel_group()
        # Parallel-safe calls complete out of order, so the ledger is drained in
        # `calls` order rather than completion order. Loop detection compares
        # sequences; an unstable order would make it non-deterministic.
        for call in calls:
            step = ledger.get(id(call))
            if step is not None:
                self._recent_steps.append(step)
        return results

    # -- one tool call: emit, pre-hooks, dispatch via registry, post-hooks --
    async def _exec_tool(
        self,
        call: ToolCall,
        *,
        parent_span_id: str | None = None,
        run_context: RunContext | None = None,
        ledger: dict | None = None,
    ) -> str:
        resolved_context = (
            run_context or self.current_run_context or RunContext.default()
        )
        session_id = str(self.state.get("session_id") or self.workspace)
        action_id = _tool_action_id(
            session_id=session_id,
            run_context=resolved_context,
            call=call,
        )
        ctx = ToolContext(
            agent=self,
            workspace=self.workspace,
            state=self.state,
            call=call,
            run_context=resolved_context,
            action_id=action_id,
        )
        span_id = f"tool_{uuid.uuid4().hex[:16]}"
        started = time.monotonic()
        await self._send(
            "tool_use",
            name=call.name,
            # The *recorded* arguments, not the executed ones: `call.input`
            # still carries the real value into the tool.
            input=self.secrets.mask_payload(call.input),
            id=call.id,
            span_id=span_id,
            parent_span_id=parent_span_id,
            action_id=action_id,
        )

        denied = False
        failed = False
        journal: InMemoryActionJournal | None = self.state.get("action_journal")
        journal_started = False
        replayed_action = False
        command_result: CommandResult | None = None
        try:
            decision = await self.hooks.before_tool(ctx, call)
            if decision is not None:
                out = str(decision)
                denied = True
            else:
                replayed = None
                if journal is not None:
                    prior = journal.begin(
                        action_id=action_id,
                        session_id=session_id,
                        message_id=resolved_context.message_id,
                        tool_use_id=call.id,
                        tool_name=call.name,
                        input_value=call.input,
                    )
                    # The journal is the authority on whether this step ran. A
                    # terminal record means it already did: return what was
                    # recorded instead of executing the side effect twice. An
                    # `unknown` record means a dead process dispatched it and
                    # nobody knows the outcome -- say so rather than retrying.
                    if prior.status in _REPLAYABLE_STATUSES:
                        replayed = prior.result or ""
                    elif prior.status == "unknown":
                        # A dead process dispatched this and nobody recorded the
                        # outcome. Ask the tool whether it landed, if it can say.
                        landed = None
                        candidate = self.tools.get(call.name)
                        if candidate is not None:
                            landed = await candidate.already_took_effect(ctx, call)
                        await self._send(
                            "reconcile",
                            name=call.name,
                            action_id=action_id,
                            verdict=(
                                "already_applied" if landed is True
                                else "not_applied" if landed is False
                                else "undetermined"
                            ),
                            verifiable=candidate is not None
                            and candidate.verify is not None,
                        )
                        if landed is True:
                            # It happened. Record that, and do not run it again.
                            replayed = RECONCILED_RESULT
                            reconcile = getattr(journal, "reconcile", None)
                            if reconcile is not None:
                                reconcile(
                                    action_id,
                                    status="completed",
                                    result=RECONCILED_RESULT,
                                )
                        elif landed is False:
                            # It provably did not happen, so retrying is safe --
                            # the only branch where an unknown action re-runs.
                            journal_started = True
                        else:
                            replayed = UNKNOWN_RESULT
                    else:
                        journal_started = True
                if replayed is not None:
                    out = replayed
                    replayed_action = True
                else:
                    tool = self.tools.get(call.name)
                    if tool is None:
                        out = f"Unknown tool: {call.name}"
                        failed = True
                    else:
                        try:
                            raw_result = await tool.run(ctx, **call.input)
                            if isinstance(raw_result, CommandResult):
                                command_result = raw_result
                                failed = bool(
                                    raw_result.error
                                    or raw_result.timed_out
                                    or raw_result.exit_code not in {None, 0}
                                )
                            out = str(raw_result)
                        except Exception as error:
                            # Tool errors are data the model reacts to, not crashes.
                            out = f"Error: {error}"
                            failed = True
                out = str(await self.hooks.after_tool(ctx, call, out))
        except asyncio.CancelledError:
            if journal is not None and journal_started:
                journal.finish(action_id, status="cancelled")
            raise
        except Exception as error:
            # Hook failures are isolated just like handler failures so one
            # parallel call cannot discard successful siblings.
            out = f"Error: {error}"
            failed = True

        # Wide masking, applied once as soon as the tool result is finalized:
        # everything downstream -- the action journal, the model transcript,
        # the event stream, the trajectory, the durable store -- reads the
        # masked `out`. Ordered *before* journal.finish (round 116): the finish
        # used to record the raw result, so the durable actions table kept a
        # secret the tool echoed while every other sink masked it.
        out = self.secrets.mask(out)
        # This is the journal authority.  A projection may carry an ephemeral
        # raw_ref that intentionally dies with the session; persisting that
        # projection would make replay advertise recovery it can no longer do.
        authoritative_out = out

        # Observation reduction is a formal post-mask stage. Hooks still see
        # the legacy raw result for compatibility, but no reducer, sidecar,
        # receipt, journal, event, trajectory, or transcript receives it before
        # the application-wide secret registry has run.
        if not replayed_action and call.name != RAW_ARTIFACT_TOOL:
            masked_query = json.dumps(
                self.secrets.mask_payload(call.input),
                ensure_ascii=False,
                sort_keys=True,
                default=str,
            )
            persist_masked_raw = (
                getattr(self.settings, "token_efficiency_persist_raw", True)
                and self.state.get("permission_mode") != "readonly"
                and RAW_ARTIFACT_TOOL in self.tools
                and len(out.encode("utf-8"))
                >= getattr(self.settings, "token_efficiency_raw_min_bytes", 4_096)
            )
            try:
                optimization = await self.token_efficiency.reduce_observation(
                    MaskedObservation(
                        content=out,
                        content_type=(
                            "text/x-command-output"
                            if command_result is not None
                            else _observation_content_type(out)
                        ),
                        metadata=(
                            (
                                ("exit_code", command_result.exit_code),
                                ("timed_out", command_result.timed_out),
                                ("overflowed", command_result.overflowed),
                                ("duration_ms", command_result.duration_ms),
                            )
                            if command_result is not None
                            else ()
                        ),
                    ),
                    query=masked_query,
                    budget_tokens=self.settings.token_threshold,
                    persist_masked_raw=persist_masked_raw,
                )
                # Reducers are plugins.  Mask their candidate again: even a
                # reducer that normalizes control bytes or emits malformed
                # content cannot reconstruct a registered credential into a
                # downstream sink.
                candidate_out = self.secrets.mask(
                    optimization.observation.content
                )
                recovery_marker = render_recovery_marker(optimization)
                if recovery_marker:
                    candidate_out = f"{recovery_marker}\n{candidate_out}"
                candidate_out = self.secrets.mask(candidate_out)

                applied = tuple(
                    receipt
                    for receipt in optimization.receipts
                    if receipt.status is OptimizationStatus.APPLIED
                )
                authority_bytes = len(authoritative_out.encode("utf-8"))
                candidate_bytes = len(candidate_out.encode("utf-8"))
                if applied and candidate_bytes >= authority_bytes:
                    # The recovery envelope is part of what the model sees.  A
                    # candidate that only looks smaller before that envelope is
                    # not a token-efficiency win; discard its now-unused ref.
                    raw_ref = optimization.observation.raw_ref
                    store = self.token_efficiency.raw_store
                    discard = getattr(store, "discard", None)
                    if raw_ref is not None and callable(discard):
                        try:
                            discard(raw_ref)
                        except Exception:
                            pass
                    receipts = tuple(
                        replace(
                            receipt,
                            status=OptimizationStatus.DEGRADED,
                            reason="recovery_envelope_inflation",
                            projected_bytes=authority_bytes,
                            tokens_after_estimate=_token_estimate(
                                authoritative_out
                            ),
                            output_digest=_text_digest(authoritative_out),
                            raw_ref=None,
                            raw_digest=None,
                        )
                        if receipt.status is OptimizationStatus.APPLIED
                        else receipt
                        for receipt in optimization.receipts
                    )
                    optimization = replace(optimization, receipts=receipts)
                    out = authoritative_out
                else:
                    # Receipt metrics describe the actual provider envelope,
                    # including the recovery marker and final secret mask.
                    receipts = tuple(
                        replace(
                            receipt,
                            projected_bytes=candidate_bytes,
                            tokens_after_estimate=_token_estimate(candidate_out),
                            output_digest=_text_digest(candidate_out),
                        )
                        if receipt.status is OptimizationStatus.APPLIED
                        else receipt
                        for receipt in optimization.receipts
                    )
                    optimization = replace(optimization, receipts=receipts)
                    out = candidate_out
                await self._send_optimization_receipts(
                    optimization.receipts,
                    parent_span_id=span_id,
                )
            except Exception as error:
                # A custom runtime is still a plugin boundary. Fail open with
                # the already-masked observation and expose only the exception
                # class, never plugin text that might contain the observation.
                await self._send(
                    "optimization_stage_error",
                    stage="observation",
                    parent_span_id=span_id,
                    error=type(error).__name__,
                )

        if journal is not None and journal_started:
            journal.finish(
                action_id,
                status="denied" if denied else ("failed" if failed else "completed"),
                result=authoritative_out,
            )

        result_fields = {
            "name": call.name,
            "output": out[:DISPLAY_CAP],
            "id": call.id,
            "span_id": span_id,
            "parent_span_id": parent_span_id,
            "action_id": action_id,
            "error": failed,
            "duration_ms": round((time.monotonic() - started) * 1000, 3),
            "_trajectory_fields": {"output": out},
        }
        if denied:
            result_fields["denied"] = True
        if replayed_action:
            # Surfaced so an operator can see a resumed turn reused a recorded
            # outcome rather than performing the action again.
            result_fields["replayed"] = True
        if command_result is not None:
            result_fields["command_result"] = {
                "exit_code": command_result.exit_code,
                "timed_out": command_result.timed_out,
                "overflowed": command_result.overflowed,
                "duration_ms": command_result.duration_ms,
            }
        await self._send("tool_result", **result_fields)
        step = ToolStep(
            name=call.name,
            input_hash=step_hash(call.input),
            output_hash=step_hash(out),
            failed=failed,
            denied=denied,
        )
        if ledger is None:
            # Direct call outside a batch (tests, embedders): order is the
            # caller's own, so record immediately.
            self._recent_steps.append(step)
        else:
            # Keyed by object identity, not `call.id`: a provider (or a fake
            # client) may leave the id blank, and blank keys would collide.
            ledger[id(call)] = step
        return out

    # -- s06: subagent = a fresh Agent, isolated context, restricted tools --
    async def _run_subagent(
        self,
        prompt: str,
        agent_type: str = "Explore",
        run_context: RunContext | None = None,
    ) -> str:
        parent_context = (
            run_context or self.current_run_context or RunContext.default()
        )
        child_context = parent_context.derive_peer_agent(
            delegated_by=self.label,
        )
        is_explore = agent_type.strip().lower() == "explore"
        registry = self.role_tool_policy.select(agent_type, self.tools)
        verb = "explore and report" if is_explore else "complete the task"
        child = Agent(
            client=self.client,
            settings=self.settings,
            workspace=self.workspace,
            # Derive, do not re-list: the child inherits every seam the parent
            # has, including ones added after this line was written.
            harness=self.harness.derive(
                tools=registry,
                hooks=self.hooks,
                skills=self.skills,
                compactor=self.compactor,
                recovery=self.recovery,
                stuck_detector=self.stuck_detector,
                cache_policy=self.cache_policy,
                secrets=self.secrets,
                sandbox=self.sandbox,
                transport=self.transport,
            ),
            system=f"You are a {agent_type} subagent in {self.workspace}. "
                   f"Use tools to {verb}, then give a concise final summary. No preamble.",
            emit=self.emit,
            llm_semaphore=self.semaphore,
            tool_semaphore=self.tool_semaphore,
            label=f"{self.label}>{agent_type.lower()}",
            depth=self.depth + 1,
            max_rounds=self.settings.subagent_max_rounds,
            state=(
                {"permission_mode": "readonly"}
                if is_explore
                else None
            ),
        )
        if is_explore:
            # "Explore is read-only" is a promise the `task` tool makes to the
            # model, and it was only a tool-list convention: the default
            # interactive mode runs a plain `echo x > file` via bash with no
            # approval (only *destructive* shell asks), so an Explore subagent
            # could mutate the workspace a caller delegated as read-only.
            # Read-only mode denies every mutating-risk tool -- bash included --
            # so the promise holds by construction, whatever the registry carries.
            assert child.state["permission_mode"] == "readonly"
        await self._send(
            "subagent_start",
            agent_type=agent_type,
            prompt=prompt[:DISPLAY_CAP],
            _trajectory_fields={"prompt": prompt},
        )
        summary = await child.run(prompt, run_context=child_context)
        await self._send(
            "subagent_end",
            agent_type=agent_type,
            summary=summary[:DISPLAY_CAP],
            _trajectory_fields={"summary": summary},
        )
        return summary or "(subagent produced no summary)"
