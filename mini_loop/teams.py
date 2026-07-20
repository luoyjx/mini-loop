"""Agent teams, request protocols, and autonomous inbox/task polling (s15-s17)."""

from __future__ import annotations

import json
import re
import threading
import time
from dataclasses import asdict, dataclass, field
from pathlib import Path

from .registry import Tool, ToolContext, ToolRegistry


@dataclass
class ProtocolState:
    request_id: str
    type: str                    # shutdown | plan_approval
    sender: str
    target: str
    status: str = "pending"     # pending | approved | rejected
    payload: str = ""
    created_at: float = field(default_factory=time.time)
    feedback: str = ""


class MessageBus:
    """Consume-on-read mailbox, optionally persisted as team JSONL files."""

    def __init__(self, root: Path | None = None) -> None:
        self.inboxes: dict[str, list[dict]] = {}
        self.root = Path(root) if root is not None else None
        self._lock = threading.RLock()

    def _path(self, key: str) -> Path:
        team_id, separator, name = key.partition("/")
        if (not separator or not re.fullmatch(r"[A-Za-z0-9._-]{1,64}", team_id)
                or not re.fullmatch(r"[A-Za-z0-9._-]{1,64}", name)):
            raise ValueError("mailbox keys must be '<safe-team>/<safe-name>'")
        assert self.root is not None
        return self.root / team_id / "inboxes" / f"{name}.jsonl"

    def send(self, frm: str, to: str, content: str, msg_type: str = "message",
             metadata: dict | None = None, **extra) -> str:
        msg = {
            "from": frm,
            "to": to,
            "content": content,
            "type": msg_type,
            "metadata": dict(metadata or {}),
            "ts": time.time(),
            **extra,
        }
        with self._lock:
            if self.root is None:
                self.inboxes.setdefault(to, []).append(msg)
            else:
                try:
                    path = self._path(to)
                except ValueError as error:
                    return f"Error: {error}"
                path.parent.mkdir(parents=True, exist_ok=True)
                with path.open("a") as stream:
                    stream.write(json.dumps(msg) + "\n")
        return f"Sent {msg_type} to {to.split('/')[-1]}"

    def read(self, name: str) -> list[dict]:
        with self._lock:
            if self.root is None:
                messages = self.inboxes.get(name, [])
                self.inboxes[name] = []
                return messages
            try:
                path = self._path(name)
            except ValueError:
                return []
            if not path.exists():
                return []
            messages = []
            for line in path.read_text().splitlines():
                try:
                    value = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if isinstance(value, dict):
                    messages.append(value)
            path.unlink(missing_ok=True)
            return messages


def team_key(team_id: str, name: str) -> str:
    return f"{team_id}/{name}"


def _key(ctx: ToolContext, name: str) -> str:
    return team_key(ctx.state.get("team_id", ""), name)


def _self_key(ctx: ToolContext) -> str:
    return _key(ctx, ctx.state.get("agent_name", "lead"))


def _render_messages(messages: list[dict]) -> str:
    cleaned = [{
        "from": message.get("from", "").split("/")[-1],
        "type": message.get("type", "message"),
        "content": message.get("content", ""),
        "metadata": message.get("metadata", {}),
    } for message in messages]
    return json.dumps(cleaned, indent=2)


async def team_injector(agent) -> list[dict]:
    manager = agent.state.get("manager")
    if manager is None or not agent.state.get("team_id"):
        return []
    messages = manager.consume_team_inbox(
        agent.state["team_id"], agent.state.get("agent_name", "lead")
    )
    if not messages:
        return []
    await agent._send("team_inbox", count=len(messages))
    return [{"role": "user", "content": f"<team_inbox>\n{_render_messages(messages)}\n</team_inbox>"}]


_SPAWN = {
    "type": "object",
    "properties": {"name": {"type": "string"}, "role": {"type": "string"},
                   "prompt": {"type": "string"}},
    "required": ["name", "role", "prompt"],
}
_SEND = {
    "type": "object",
    "properties": {"to": {"type": "string"}, "content": {"type": "string"},
                   "type": {"type": "string"}, "metadata": {"type": "object"}},
    "required": ["to", "content"],
}
_BROADCAST = {"type": "object", "properties": {"content": {"type": "string"}},
              "required": ["content"]}
_SHUTDOWN = {
    "type": "object",
    "properties": {"target": {"type": "string"}, "reason": {"type": "string"}},
    "required": ["target"],
}
_REQUEST_PLAN = {
    "type": "object",
    "properties": {"teammate": {"type": "string"}, "task": {"type": "string"}},
    "required": ["teammate", "task"],
}
_PLAN = {"type": "object", "properties": {"plan": {"type": "string"}}, "required": ["plan"]}
_REVIEW = {
    "type": "object",
    "properties": {"request_id": {"type": "string"}, "approve": {"type": "boolean"},
                   "feedback": {"type": "string"}},
    "required": ["request_id", "approve"],
}
_EMPTY = {"type": "object", "properties": {}}


def install_teams(registry: ToolRegistry) -> ToolRegistry:
    async def spawn_teammate(ctx, name, role, prompt):
        manager = ctx.state.get("manager")
        if manager is None:
            return "Error: teams not available (no manager)"
        return await manager.spawn_teammate(ctx.state.get("session_id", ""), name, role, prompt)

    async def send_message(ctx, to, content, type="message", metadata=None):
        bus = ctx.state.get("bus")
        if bus is None:
            return "Error: message bus not available"
        return bus.send(_self_key(ctx), _key(ctx, to), content, type, metadata)

    async def read_inbox(ctx):
        manager, bus = ctx.state.get("manager"), ctx.state.get("bus")
        if bus is None:
            return "Error: message bus not available"
        messages = (manager.consume_team_inbox(ctx.state.get("team_id", ""),
                                                ctx.state.get("agent_name", "lead"))
                    if manager else bus.read(_self_key(ctx)))
        return _render_messages(messages) if messages else "(empty inbox)"

    async def broadcast(ctx, content):
        bus, manager = ctx.state.get("bus"), ctx.state.get("manager")
        if bus is None or manager is None:
            return "Error: teams not available"
        me = ctx.state.get("agent_name", "lead")
        sent = 0
        for teammate in manager.teammates_of(ctx.state.get("team_id", "")):
            if teammate != me:
                bus.send(_self_key(ctx), _key(ctx, teammate), content, "broadcast")
                sent += 1
        return f"Broadcast to {sent} teammate(s)"

    async def list_teammates(ctx):
        manager = ctx.state.get("manager")
        if manager is None:
            return "Error: teams not available"
        names = manager.teammates_of(ctx.state.get("team_id", ""))
        return "\n".join(f"  - {name}" for name in names) if names else "No teammates."

    async def request_shutdown(ctx, target, reason=""):
        manager = ctx.state.get("manager")
        if manager is None:
            return "Error: teams not available"
        if ctx.state.get("agent_name", "lead") != "lead":
            return "Error: only the lead can request teammate shutdown"
        return manager.request_shutdown(ctx.state.get("team_id", ""), target, reason)

    async def submit_plan(ctx, plan):
        manager = ctx.state.get("manager")
        if manager is None:
            return "Error: teams not available"
        return manager.submit_plan(ctx.state.get("team_id", ""),
                                   ctx.state.get("agent_name", "lead"), plan)

    async def request_plan(ctx, teammate, task):
        manager = ctx.state.get("manager")
        if manager is None:
            return "Error: teams not available"
        if ctx.state.get("agent_name", "lead") != "lead":
            return "Error: only the lead can request plans"
        return manager.request_plan(ctx.state.get("team_id", ""), teammate, task)

    async def review_plan(ctx, request_id, approve, feedback=""):
        manager = ctx.state.get("manager")
        if manager is None:
            return "Error: teams not available"
        if ctx.state.get("agent_name", "lead") != "lead":
            return "Error: only the lead can review plans"
        return manager.review_plan(ctx.state.get("team_id", ""), request_id, approve, feedback)

    async def list_protocols(ctx):
        manager = ctx.state.get("manager")
        if manager is None:
            return "Error: teams not available"
        states = [asdict(state) for state in manager.protocols.values()
                  if state.sender.startswith(ctx.state.get("team_id", "") + "/")]
        return json.dumps(states, indent=2) if states else "No protocol requests."

    registry.register(Tool("spawn_teammate", "Spawn an autonomous concurrent teammate.", _SPAWN, spawn_teammate))
    registry.register(Tool("send_message", "Send a typed message to a teammate.", _SEND, send_message))
    registry.register(Tool("read_inbox", "Read, route, and drain your inbox.", _EMPTY, read_inbox, readonly=True))
    registry.register(Tool("broadcast", "Send a message to all teammates.", _BROADCAST, broadcast))
    registry.register(Tool("list_teammates", "List teammates in this team.", _EMPTY, list_teammates, readonly=True))
    registry.register(Tool("request_shutdown", "Request a teammate shutdown with an auditable handshake.",
                           _SHUTDOWN, request_shutdown))
    registry.register(Tool("request_plan", "Ask a teammate to submit a plan for a task.",
                           _REQUEST_PLAN, request_plan))
    registry.register(Tool("submit_plan", "Submit a plan to the lead for approval.", _PLAN, submit_plan))
    registry.register(Tool("review_plan", "Approve or reject a submitted teammate plan.", _REVIEW, review_plan))
    registry.register(Tool("list_protocols", "List protocol request states.", _EMPTY, list_protocols, readonly=True))
    return registry
