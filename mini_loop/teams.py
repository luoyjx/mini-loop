"""Agent teams, request protocols, and autonomous inbox/task polling (s15-s17)."""

from __future__ import annotations

import json
import re
import threading
import time
from dataclasses import asdict, dataclass, field
from pathlib import Path

from .problems import ProblemLog
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

    #: A message is injected whole into a peer's message stream. One measured
    #: 2,000,000 characters -- half a million tokens delivered into another
    #: agent's context by a sender it does not control.
    MAX_CONTENT = 16_000
    #: `team_injector` delivers a whole mailbox at once; 2,000 messages arrived
    #: as one injection.
    MAX_INBOX = 100
    #: The most a single `read` pulls into memory. A read delivers at most
    #: MAX_INBOX messages, each <= MAX_CONTENT, so the last MAX_INBOX lines fit
    #: here with room for metadata; reading only this much from the tail bounds
    #: a read to the batch it returns, however large an undrained mailbox grew.
    MAX_READ_BYTES = MAX_INBOX * (MAX_CONTENT + 4_096)

    def __init__(self, root: Path | None = None, *, secrets=None) -> None:
        self.inboxes: dict[str, list[dict]] = {}
        self.root = Path(root) if root is not None else None
        self._lock = threading.RLock()
        self.secrets = secrets
        #: Reads that found a malformed mailbox, which otherwise looks empty.
        self.problems = ProblemLog()

    def _path(self, key: str) -> Path:
        team_id, separator, name = key.partition("/")
        if (not separator or not re.fullmatch(r"[A-Za-z0-9._-]{1,64}", team_id)
                or not re.fullmatch(r"[A-Za-z0-9._-]{1,64}", name)):
            raise ValueError("mailbox keys must be '<safe-team>/<safe-name>'")
        assert self.root is not None
        return self.root / team_id / "inboxes" / f"{name}.jsonl"

    def send(self, frm: str, to: str, content: str, msg_type: str = "message",
             metadata: dict | None = None, **extra) -> str:
        if len(content) > self.MAX_CONTENT:
            return (
                f"Error: message is {len(content):,} characters; the limit is "
                f"{self.MAX_CONTENT:,}"
            )
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
                inbox = self.inboxes.setdefault(to, [])
                inbox.append(msg)
                # Bound the resource, not just the read. `read` returns only the
                # last MAX_INBOX, but the queue held every message ever sent to a
                # recipient that never drains -- a shut-down teammate, or a live
                # one busy in a long turn -- growing without limit in RAM. Shed
                # the oldest here so peak memory tracks the bound; the reader
                # still sees the same last MAX_INBOX it always did. The persisted
                # backend already bounds delivery by reading only the tail.
                if len(inbox) > self.MAX_INBOX:
                    del inbox[: -self.MAX_INBOX]
            else:
                try:
                    path = self._path(to)
                except ValueError as error:
                    return f"Error: {error}"
                path.parent.mkdir(parents=True, exist_ok=True)
                # Mask the *structure*, not the serialized line. `mask()` matches
                # a secret's raw bytes, but `json.dumps` escapes non-ASCII to
                # `\uXXXX` and quotes/backslashes to `\"`/`\\`, so a credential
                # carrying any of those survived a post-serialization mask into
                # this durable mailbox -- and it is then read straight into a
                # peer agent's context. `mask_payload` scrubs each value before
                # it is escaped, the order every other durable sink already uses.
                payload = (
                    self.secrets.mask_payload(msg) if self.secrets is not None else msg
                )
                with path.open("a") as stream:
                    stream.write(json.dumps(payload) + "\n")
        return f"Sent {msg_type} to {to.split('/')[-1]}"

    def _read_tail(self, path: Path) -> tuple[str, bool]:
        """The last MAX_READ_BYTES of the mailbox, and whether it was truncated.

        A `read` returns at most MAX_INBOX messages but had loaded the whole
        file to do it, so an undrained mailbox -- a peer that keeps sending to a
        recipient which is busy, idle, or shut down -- grew without bound and
        would OOM the shared process the moment anything finally read it (the
        delivered batch is bounded; the read that produced it was not). Reading
        only the tail bounds a read to the batch it delivers. The first line of
        a truncated read is the tail of a message the seek cut through, so it is
        dropped without being mistaken for corruption.
        """
        size = path.stat().st_size
        if size <= self.MAX_READ_BYTES:
            return path.read_text(), False
        with path.open("rb") as handle:
            handle.seek(size - self.MAX_READ_BYTES)
            chunk = handle.read(self.MAX_READ_BYTES)
        text = chunk.decode("utf-8", errors="ignore")
        newline = text.find("\n")
        return (text[newline + 1:] if newline != -1 else ""), True

    def read(self, name: str) -> list[dict]:
        with self._lock:
            if self.root is None:
                messages = self.inboxes.get(name, [])
                self.inboxes[name] = []
                # The bound is the mailbox's, not the durable backend's: the
                # in-memory path returned every queued message uncapped while
                # the persisted path capped at MAX_INBOX. A new backend inherits
                # the resource's bound, it does not get to skip it.
                return messages[-self.MAX_INBOX:]
            try:
                path = self._path(name)
            except ValueError as error:
                # `send` reports a bad key and this returned `[]`, so a typo in
                # a mailbox name looked exactly like an empty inbox -- to the
                # agent waiting on it, forever.
                self.problems.append(f"read({name!r}) refused: {error}")
                return []
            if not path.exists():
                return []
            text, truncated = self._read_tail(path)
            path.unlink(missing_ok=True)
            messages = []
            for line in text.splitlines():
                try:
                    value = json.loads(line)
                except json.JSONDecodeError:
                    self.problems.append(f"{path}: a malformed message was dropped")
                    continue
                if isinstance(value, dict):
                    messages.append(value)
            if truncated or len(messages) > self.MAX_INBOX:
                self.problems.append(
                    f"{path}: mailbox exceeded {self.MAX_READ_BYTES:,} bytes; "
                    "older messages dropped unread"
                    if truncated else
                    f"{path}: {len(messages)} messages delivered at once; "
                    f"{len(messages) - self.MAX_INBOX} dropped"
                )
                messages = messages[-self.MAX_INBOX:]
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
        # A message to a name nobody consumes lands in a limbo inbox and is
        # silently lost, while the sender is told "Sent" -- OpenWorker's
        # unrouted-message hazard (research doc 6.4), and the same
        # confirmed-a-delivery-that-never-happened bug round 50 fixed for an
        # oversized broadcast. The manager knows the roster, so refuse a
        # recipient that is not on it. The lead is always addressed as "lead"
        # (manager pins its agent_name), so it is a valid recipient in any team.
        manager = ctx.state.get("manager")
        if manager is not None:
            team_id = ctx.state.get("team_id", "")
            known = set(manager.teammates_of(team_id)) | {"lead"}
            if to not in known:
                roster = ", ".join(sorted(known))
                return (
                    f"Error: no teammate named {to!r} in this team; message not "
                    f"sent. Known recipients: {roster}"
                )
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
        sent, refused = 0, []
        for teammate in manager.teammates_of(ctx.state.get("team_id", "")):
            if teammate == me:
                continue
            # `bus.send` reports refusals by returning a string starting with
            # "Error:", and this discarded it -- so once round 50 gave `send` a
            # size limit, an oversized broadcast answered "Broadcast to 3
            # teammate(s)" while delivering none, and the lead carried on
            # believing it had coordinated with its team.
            result = bus.send(_self_key(ctx), _key(ctx, teammate), content, "broadcast")
            if str(result).startswith("Error:"):
                refused.append(f"{teammate}: {result}")
            else:
                sent += 1
        if refused:
            detail = "; ".join(refused[:3])
            return (
                f"Broadcast to {sent} teammate(s); {len(refused)} refused ({detail})"
            )
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

    registry.register(Tool("spawn_teammate", "Spawn an autonomous concurrent teammate.", _SPAWN, spawn_teammate, risk="exec"))
    registry.register(Tool("send_message", "Send a typed message to a teammate.", _SEND, send_message, risk="write"))
    registry.register(Tool("read_inbox", "Read, route, and drain your inbox.", _EMPTY, read_inbox, readonly=True, risk="read"))
    registry.register(Tool("broadcast", "Send a message to all teammates.", _BROADCAST, broadcast, risk="write"))
    registry.register(Tool("list_teammates", "List teammates in this team.", _EMPTY, list_teammates, readonly=True, risk="read"))
    registry.register(Tool("request_shutdown", "Request a teammate shutdown with an auditable handshake.",
                           _SHUTDOWN, request_shutdown, risk="write"))
    registry.register(Tool("request_plan", "Ask a teammate to submit a plan for a task.",
                           _REQUEST_PLAN, request_plan, risk="write"))
    registry.register(Tool("submit_plan", "Submit a plan to the lead for approval.", _PLAN, submit_plan, risk="write"))
    registry.register(Tool("review_plan", "Approve or reject a submitted teammate plan.", _REVIEW, review_plan, risk="write"))
    registry.register(Tool("list_protocols", "List protocol request states.", _EMPTY, list_protocols, readonly=True, risk="read"))
    return registry
