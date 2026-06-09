"""A session = one persistent Agent + an event bus + a run lock.

The Agent holds the conversation; the session wraps it with the machinery a
server needs:

  * `emit` fans every agent event out to all live SSE subscribers and appends
    it to a bounded backlog (so a late subscriber can catch up);
  * `lock` serializes runs *within* one session -- a session is a single
    conversation, so two overlapping messages would corrupt its history.
    Different sessions hold different locks and run fully concurrently.
"""

from __future__ import annotations

import asyncio
import time
from collections import deque
from pathlib import Path

from .agent import Agent

BACKLOG = 200  # recent events retained for replay to new subscribers


class AgentSession:
    def __init__(self, session_id: str, workspace: Path, *, system: str | None = None) -> None:
        self.id = session_id
        self.workspace = workspace
        self.system = system
        self.created_at = time.time()
        self.status = "idle"  # idle | running | error
        self.run_count = 0

        self.lock = asyncio.Lock()
        self._subscribers: set[asyncio.Queue] = set()
        self._backlog: deque[dict] = deque(maxlen=BACKLOG)
        self._seq = 0

        self.agent: Agent | None = None  # attached by the manager after construction

    # -- event bus --
    async def emit(self, event: dict) -> None:
        self._seq += 1
        event = {"seq": self._seq, "ts": time.time(), **event}
        self._backlog.append(event)
        for q in list(self._subscribers):
            q.put_nowait(event)

    def subscribe(self, replay: bool = True) -> asyncio.Queue:
        q: asyncio.Queue = asyncio.Queue()
        if replay:
            for event in self._backlog:
                q.put_nowait(event)
        self._subscribers.add(q)
        return q

    def unsubscribe(self, q: asyncio.Queue) -> None:
        self._subscribers.discard(q)

    # -- run one user message to completion (serialized per session) --
    async def run(self, message: str) -> str:
        assert self.agent is not None
        async with self.lock:
            self.status = "running"
            self.run_count += 1
            await self.emit({"type": "status", "status": "running"})
            try:
                final = await self.agent.run(message)
            except Exception as e:
                self.status = "error"
                await self.emit({"type": "error", "error": f"{type(e).__name__}: {e}"})
                raise
            self.status = "idle"
            await self.emit({"type": "done", "text": final})
            return final

    # -- introspection for the API --
    def info(self) -> dict:
        agent = self.agent
        return {
            "id": self.id,
            "status": self.status,
            "created_at": self.created_at,
            "run_count": self.run_count,
            "workspace": str(self.workspace),
            "model": agent.settings.model if agent else None,
            "message_count": len(agent.messages) if agent else 0,
            "todos": agent.todo.snapshot() if agent else [],
            "subscribers": len(self._subscribers),
        }
