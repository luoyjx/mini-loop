"""Agent teams (s15-s17), reframed for a multi-session server.

learn-claude-code runs each teammate as a thread with its own loop + idle-poll.
We already run every agent as an independent concurrent session, so the
thread/idle machinery is redundant. What's worth keeping is the *coordination
layer*: an async mailbox (MessageBus) so agents can talk, plus the shared task
board (tasks.py) so they can divide work.

`spawn_teammate` therefore creates a new session that **shares the spawner's
workspace** (so they share the `.tasks` board and `.memory`) and kicks it off
on a prompt -- a real concurrent teammate, not a thread. Messages are scoped to
a team (the spawner's id) so groups don't collide.

Tools: spawn_teammate / send_message / read_inbox / broadcast / list_teammates.
"""

from __future__ import annotations

import json
import time

from .registry import Tool, ToolContext, ToolRegistry


class MessageBus:
    """In-memory async mailbox. Keys are `"<team>/<name>"` strings."""

    def __init__(self) -> None:
        self.inboxes: dict[str, list[dict]] = {}

    def send(self, frm: str, to: str, content: str, msg_type: str = "message", **extra) -> str:
        msg = {"from": frm, "to": to, "content": content, "type": msg_type, "ts": time.time(), **extra}
        self.inboxes.setdefault(to, []).append(msg)
        return f"Sent {msg_type} to {to.split('/')[-1]}"

    def read(self, name: str) -> list[dict]:
        msgs = self.inboxes.get(name, [])
        self.inboxes[name] = []
        return msgs


def _key(ctx: ToolContext, name: str) -> str:
    return f"{ctx.state.get('team_id', '')}/{name}"


def _self_key(ctx: ToolContext) -> str:
    return _key(ctx, ctx.state.get("agent_name", "lead"))


_SPAWN = {
    "type": "object",
    "properties": {"name": {"type": "string"}, "role": {"type": "string"}, "prompt": {"type": "string"}},
    "required": ["name", "role", "prompt"],
}
_SEND = {"type": "object", "properties": {"to": {"type": "string"}, "content": {"type": "string"}},
         "required": ["to", "content"]}
_BROADCAST = {"type": "object", "properties": {"content": {"type": "string"}}, "required": ["content"]}
_EMPTY = {"type": "object", "properties": {}}


def install_teams(registry: ToolRegistry) -> ToolRegistry:
    async def spawn_teammate(ctx, name, role, prompt):
        mgr = ctx.state.get("manager")
        if mgr is None:
            return "Error: teams not available (no manager)"
        return await mgr.spawn_teammate(ctx.state.get("session_id", ""), name, role, prompt)

    async def send_message(ctx, to, content):
        bus = ctx.state.get("bus")
        if bus is None:
            return "Error: message bus not available"
        return bus.send(_self_key(ctx), _key(ctx, to), content)

    async def read_inbox(ctx):
        bus = ctx.state.get("bus")
        if bus is None:
            return "Error: message bus not available"
        msgs = bus.read(_self_key(ctx))
        return json.dumps([{"from": m["from"].split("/")[-1], "type": m["type"],
                            "content": m["content"]} for m in msgs], indent=2) if msgs else "(empty inbox)"

    async def broadcast(ctx, content):
        bus, mgr = ctx.state.get("bus"), ctx.state.get("manager")
        if bus is None or mgr is None:
            return "Error: teams not available"
        me = ctx.state.get("agent_name", "lead")
        sent = 0
        for tm in mgr.teammates_of(ctx.state.get("team_id", "")):
            if tm != me:
                bus.send(_self_key(ctx), _key(ctx, tm), content, "broadcast")
                sent += 1
        return f"Broadcast to {sent} teammate(s)"

    async def list_teammates(ctx):
        mgr = ctx.state.get("manager")
        if mgr is None:
            return "Error: teams not available"
        names = mgr.teammates_of(ctx.state.get("team_id", ""))
        return "\n".join(f"  - {n}" for n in names) if names else "No teammates."

    registry.register(Tool("spawn_teammate", "Spawn a concurrent teammate session sharing this workspace, on a prompt.", _SPAWN, spawn_teammate))
    registry.register(Tool("send_message", "Send a message to a teammate by name.", _SEND, send_message))
    registry.register(Tool("read_inbox", "Read and drain your inbox.", _EMPTY, read_inbox, readonly=True))
    registry.register(Tool("broadcast", "Send a message to all teammates.", _BROADCAST, broadcast))
    registry.register(Tool("list_teammates", "List teammates in this team.", _EMPTY, list_teammates, readonly=True))
    return registry
