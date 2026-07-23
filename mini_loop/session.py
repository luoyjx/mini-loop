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
import inspect
import time
from collections import deque
from collections.abc import Callable
from pathlib import Path

from .agent import Agent
from .trajectory import TrajectoryStore

BACKLOG = 200  # recent events retained for replay to new subscribers


class AgentSession:
    def __init__(
        self,
        session_id: str,
        workspace: Path,
        *,
        system: str | None = None,
        event_sink: Callable[[dict], object] | None = None,
        trajectory_store: TrajectoryStore | None = None,
    ) -> None:
        self.id = session_id
        self.workspace = workspace
        self.system = system
        self.created_at = time.time()
        self.status = "idle"  # idle | running | error
        self.run_count = 0

        # Optional global observer (metrics, logging, persistence). Sync or async.
        self._event_sink = event_sink
        self._trajectory_store = trajectory_store
        self._active_trajectory_id: str | None = None
        self._trajectory_started = 0.0
        self._trajectory_had_error = False
        self._trajectory_recording_error: str | None = None
        self._trajectory_count = 0
        if trajectory_store is not None:
            try:
                self._trajectory_count = trajectory_store.count(session_id)
            except Exception as error:
                self._trajectory_recording_error = f"{type(error).__name__}: {error}"

        self.lock = asyncio.Lock()
        self._emit_lock = asyncio.Lock()
        self._subscribers: set[asyncio.Queue] = set()
        self._backlog: deque[dict] = deque(maxlen=BACKLOG)
        self._seq = 0

        self.agent: Agent | None = None  # attached by the manager after construction

    # -- event bus --
    async def _capture_event(self, event: dict) -> dict:
        event = dict(event)
        trajectory_fields = event.pop("_trajectory_fields", None)
        self._seq += 1
        event = {**event, "seq": self._seq, "ts": time.time(), "session": self.id}
        if self._active_trajectory_id is not None:
            event["trajectory_id"] = self._active_trajectory_id
            event["trace_id"] = self._active_trajectory_id
            event["group_id"] = self.id
            if event.get("type") == "error":
                self._trajectory_had_error = True
            if self._trajectory_store is not None:
                try:
                    recorded_event = (
                        {**event, **trajectory_fields}
                        if isinstance(trajectory_fields, dict) else event
                    )
                    for key in (
                        "type", "seq", "ts", "session", "trajectory_id", "trace_id",
                        "group_id", "agent", "depth",
                    ):
                        if key in event:
                            recorded_event[key] = event[key]
                    await asyncio.to_thread(
                        self._trajectory_store.append,
                        self._active_trajectory_id,
                        recorded_event,
                    )
                except Exception as error:  # observability must not stop the agent
                    self._trajectory_recording_error = f"{type(error).__name__}: {error}"
        return event

    async def _publish_event(self, event: dict) -> None:
        self._backlog.append(event)
        for q in list(self._subscribers):
            q.put_nowait(event)
        if self._event_sink is not None:
            res = self._event_sink(event)
            if inspect.isawaitable(res):
                await res

    async def emit(self, event: dict) -> None:
        # Parallel tool calls may emit concurrently. Keep sequence assignment,
        # trajectory append, backlog publication, and sinks in one order.
        async with self._emit_lock:
            await self._publish_event(await self._capture_event(event))

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
            self._trajectory_had_error = False
            self._trajectory_recording_error = None
            self._trajectory_started = time.monotonic()
            if self._trajectory_store is not None:
                try:
                    self._active_trajectory_id = await asyncio.to_thread(
                        self._trajectory_store.start,
                        session_id=self.id,
                        run_index=self.run_count,
                        input_text=message,
                        metadata={
                            "model": self.agent.settings.model,
                            "workspace": str(self.workspace),
                            "agent": self.agent.label,
                            "system": self.agent.refresh_system(),
                            "tools": self.agent.tools.names(),
                        },
                    )
                    self._trajectory_count += 1
                except Exception as error:  # keep the requested run available
                    self._active_trajectory_id = None
                    self._trajectory_recording_error = f"{type(error).__name__}: {error}"
            if self._active_trajectory_id is not None:
                await self.emit({
                    "type": "trajectory_start",
                    "run_index": self.run_count,
                })
            await self.emit({"type": "status", "status": "running"})
            try:
                final = await self.agent.run(message)
            except asyncio.CancelledError:
                self.status = "idle"
                await self._finish_trajectory(
                    "cancelled",
                    terminal_event={
                        "type": "status", "status": "idle", "cancelled": True,
                    },
                )
                raise
            except Exception as e:
                self.status = "error"
                detail = f"{type(e).__name__}: {e}"
                self._trajectory_had_error = True
                await self._finish_trajectory(
                    "error",
                    terminal_event={"type": "error", "error": detail},
                    error=detail,
                )
                raise
            self.status = "idle"
            outcome = "error" if self._trajectory_had_error else "completed"
            await self._finish_trajectory(
                outcome,
                terminal_event={"type": "done", "text": final},
                output=final,
            )
            return final

    async def _finish_trajectory(
        self,
        status: str,
        *,
        terminal_event: dict,
        output: str | None = None,
        error: str | None = None,
    ) -> None:
        trajectory_id = self._active_trajectory_id
        if trajectory_id is None:
            await self.emit({
                **terminal_event,
                "trajectory_id": None,
                "trajectory_status": "disabled",
                "trajectory_recording_error": self._trajectory_recording_error,
            })
            return
        duration_ms = (time.monotonic() - self._trajectory_started) * 1000
        await self.emit({
            "type": "trajectory_end",
            "status": status,
            "duration_ms": round(duration_ms, 3),
        })
        terminal = await self._capture_event({
            **terminal_event,
            "trajectory_id": trajectory_id,
            "trace_id": trajectory_id,
            "group_id": self.id,
            "trajectory_status": status,
            "duration_ms": round(duration_ms, 3),
        })
        try:
            assert self._trajectory_store is not None
            await asyncio.to_thread(
                self._trajectory_store.finish,
                trajectory_id,
                status=status,
                output=output,
                error=error,
                duration_ms=duration_ms,
            )
        except Exception as recording_error:
            self._trajectory_recording_error = (
                f"{type(recording_error).__name__}: {recording_error}"
            )
        finally:
            self._active_trajectory_id = None
        terminal["trajectory_persisted"] = self._trajectory_recording_error is None
        terminal["trajectory_recording_error"] = self._trajectory_recording_error
        await self._publish_event(terminal)

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
            "active_trajectory_id": self._active_trajectory_id,
            "trajectory_count": self._trajectory_count,
            "trajectory_recording_error": self._trajectory_recording_error,
        }
