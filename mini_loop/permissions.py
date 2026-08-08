"""Permission rules and approval routing (s03), implemented as a hook (s04)."""

from __future__ import annotations

import inspect
import re
from collections.abc import Awaitable, Callable, Iterable
from dataclasses import dataclass
from typing import TYPE_CHECKING

from .registry import Hook, ToolCall, ToolContext
from .tools import DANGEROUS

if TYPE_CHECKING:
    from .registry import Hooks


RuleCheck = Callable[[ToolContext, ToolCall], bool]
Approval = Callable[[ToolContext, ToolCall, "PermissionRule"], bool | Awaitable[bool]]


@dataclass(frozen=True)
class PermissionRule:
    """A rule that can deny outright or ask an injected approval callback."""

    name: str
    tools: tuple[str, ...]
    check: RuleCheck
    message: str
    action: str = "ask"  # ask | deny

    def matches(self, ctx: ToolContext, call: ToolCall) -> bool:
        return ("*" in self.tools or call.name in self.tools) and bool(self.check(ctx, call))


_DESTRUCTIVE = re.compile(
    r"(^|[;&|\n]\s*)(rm\s|git\s+(?:reset\s+--hard|clean\s+-)|"
    r"chmod\s+(?:-R\s+|777\s)|chown\s+-R)|>\s*/etc/",
    re.IGNORECASE,
)


def _path_escapes(ctx: ToolContext, call: ToolCall) -> bool:
    try:
        root = ctx.workspace.resolve()
        path = (root / str(call.input.get("path", ""))).resolve()
        return path != root and not path.is_relative_to(root)
    except (OSError, RuntimeError, ValueError):
        return True


#: Per-session decision posture, OpenWorker's mode model reduced to the three
#: its GUI actually shows (discuss / ask / full access). The mode maps risk to
#: decision; the *rules* stay the same in every mode:
#:
#: * `readonly`    -- write/exec/external (and unclassified) deny outright,
#:                    reads pass. A session that can be handed an untrusted
#:                    prompt and provably mutate nothing.
#: * `interactive` -- the default: external and unclassified ask.
#: * `auto`        -- ask-rules auto-allow (with an audit event). Deny rules
#:                    and the immutable deny-list still apply: full access
#:                    means "stop asking", not "stop refusing".
#:
#: Runtime state, deliberately not persisted: a restored session comes back
#: `interactive` -- the fail-safe direction is toward asking again.
PERMISSION_MODES = ("readonly", "interactive", "auto")

#: Risk levels a read-only session refuses. Unclassified (None) is refused
#: too -- the round-95 rule that no claim gates upward, applied per mode.
_MUTATING_RISK = ("write", "exec", "external")


def _session_mode(ctx: ToolContext) -> str:
    state = getattr(getattr(ctx, "agent", None), "state", None) or {}
    session = state.get("session")
    mode = getattr(session, "permission_mode", None) or state.get("permission_mode")
    return mode if mode in PERMISSION_MODES else "interactive"


#: A call to a tool that is not in the registry at all. Distinct from an
#: unclassified tool on purpose: there is nothing to approve -- no handler
#: exists -- and the dispatcher's "unknown tool" answer is the feedback the
#: model needs. Round 96 found the collapse the hard way: treating missing as
#: unclassified parked a turn for the full approval timeout waiting for a
#: human to authorize a tool that does not exist.
_MISSING = object()


def _declared_risk(ctx: ToolContext, call: ToolCall):
    """The risk on the tool's contract: a level, None (no claim), or _MISSING."""

    registry = getattr(getattr(ctx, "agent", None), "tools", None)
    tool = registry.get(call.name) if registry is not None else None
    if tool is None:
        return _MISSING
    return tool.risk


def default_permission_rules() -> list[PermissionRule]:
    """Deny-list rules for the shell, plus the risk ladder, executed.

    Until round 95 the only oversight an MCP tool had was a name heuristic:
    `"deploy" in call.name`. Measured through a real turn, a tool named
    `mcp__ghsrv__delete_repository` ran against `prod/main` with zero
    permission events -- and its contract carried `readonly=True`, because the
    *server's own* readOnlyHint was taken at its word. Metadata the untrusted
    side writes and nothing executes is not a boundary. The rules below read
    `Tool.risk`, which `register_mcp` pins to `"external"` regardless of what
    the server claims.
    """

    return [
        PermissionRule(
            "workspace-boundary",
            ("write_file", "edit_file"),
            _path_escapes,
            "Path escapes the workspace",
            action="deny",
        ),
        PermissionRule(
            "destructive-shell",
            ("bash", "background_run"),
            lambda _ctx, call: bool(_DESTRUCTIVE.search(str(call.input.get("command", "")))),
            "Potentially destructive shell command",
        ),
        PermissionRule(
            "external-action",
            ("*",),
            lambda ctx, call: _declared_risk(ctx, call) == "external",
            "Tool acts outside this machine",
        ),
        PermissionRule(
            "unclassified-tool",
            ("*",),
            lambda ctx, call: _declared_risk(ctx, call) is None,
            "Tool declares no risk level; treated as external until classified",
        ),
    ]


class PermissionHook(Hook):
    """Immutable deny-list followed by ordered rules and optional approval.

    A server has no terminal prompt, so an `ask` rule is denied safely when no
    approval callback is supplied. Applications can inject a callback backed by
    their UI, policy service, or test harness.
    """

    def __init__(
        self,
        rules: Iterable[PermissionRule] | None = None,
        *,
        approval: Approval | None = None,
        deny_commands: Iterable[str] = DANGEROUS,
    ) -> None:
        self.rules = list(default_permission_rules() if rules is None else rules)
        self.approval = approval
        self.deny_commands = tuple(deny_commands)

    async def _emit(self, ctx: ToolContext, **fields) -> None:
        await ctx.emit_event("permission", **fields)

    async def before_tool(self, ctx: ToolContext, call: ToolCall) -> str | None:
        # The immutable deny-list holds in every mode. `auto` widens what is
        # not *asked about*; it never widens what is refused.
        if call.name in ("bash", "background_run"):
            command = str(call.input.get("command", ""))
            for pattern in self.deny_commands:
                if pattern in command:
                    await self._emit(ctx, decision="deny", rule="immutable-deny-list",
                                     tool=call.name, reason=pattern)
                    return f"Permission denied: '{pattern}' is blocked"

        mode = _session_mode(ctx)
        if mode == "readonly":
            risk = _declared_risk(ctx, call)
            if risk in _MUTATING_RISK or risk is None:
                # Denied, not asked: the whole point of a read-only session is
                # that no approval -- human or hook -- can mutate through it.
                # A missing tool still passes to dispatch's "unknown tool".
                await self._emit(ctx, decision="deny", rule="readonly-mode",
                                 tool=call.name, reason=f"risk={risk}")
                return (
                    "Permission denied: this session is read-only "
                    f"(tool risk: {risk or 'unclassified'})"
                )

        for rule in self.rules:
            if not rule.matches(ctx, call):
                continue
            allowed = False
            if rule.action == "ask" and mode == "auto":
                # Auto-allowed, still audited: the event stream shows what
                # would have asked, so full access is visible, not silent.
                await self._emit(ctx, decision="allow", rule=rule.name,
                                 tool=call.name, reason="auto mode")
                continue
            if rule.action == "ask" and self.approval is not None:
                allowed = self.approval(ctx, call, rule)
                if inspect.isawaitable(allowed):
                    allowed = await allowed
            decision = "allow" if allowed else "deny"
            await self._emit(ctx, decision=decision, rule=rule.name,
                             tool=call.name, reason=rule.message)
            if allowed:
                continue
            suffix = " (approval required)" if rule.action == "ask" and self.approval is None else ""
            return f"Permission denied: {rule.message}{suffix}"
        return None


def default_hooks(*, approval: Approval | None = None) -> "Hooks":
    # Local import avoids a registry -> permissions -> registry import cycle.
    from .registry import Hooks

    return Hooks([PermissionHook(approval=approval)])
