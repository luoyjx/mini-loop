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


def default_permission_rules() -> list[PermissionRule]:
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
            "destructive-mcp",
            ("*",),
            lambda _ctx, call: call.name.startswith("mcp__") and "deploy" in call.name.lower(),
            "Destructive-looking MCP tool",
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
        if call.name in ("bash", "background_run"):
            command = str(call.input.get("command", ""))
            for pattern in self.deny_commands:
                if pattern in command:
                    await self._emit(ctx, decision="deny", rule="immutable-deny-list",
                                     tool=call.name, reason=pattern)
                    return f"Permission denied: '{pattern}' is blocked"

        for rule in self.rules:
            if not rule.matches(ctx, call):
                continue
            allowed = False
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
