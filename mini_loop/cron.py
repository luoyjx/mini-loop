"""Cron scheduler (s14), async-native and session-scoped.

A single asyncio ticker (started by the manager) matches every job's 5-field
cron expression against the wall clock once a minute. When a job fires it
"wakes" its owning session by kicking off `session.run(prompt)` as a background
task -- the always-on / heartbeat pattern. Durable jobs are persisted to JSON so
their definitions survive a restart (nothing fires while the process is down).

Jobs are scoped to the session that created them. Tools: schedule_cron /
list_crons / cancel_cron, reading the scheduler + session id from agent state.
"""

from __future__ import annotations

import asyncio
import contextlib
import json
import uuid
from dataclasses import asdict, dataclass
from datetime import datetime
from pathlib import Path

from .registry import Tool, ToolContext, ToolRegistry


def _part_values(part: str, lower: int, upper: int) -> range | tuple[int, ...]:
    base, separator, step_text = part.partition("/")
    step = int(step_text) if separator else 1
    if step <= 0:
        raise ValueError("step must be greater than zero")
    if base == "*":
        start, end = lower, upper
    elif "-" in base:
        start_text, end_text = base.split("-", 1)
        start, end = int(start_text), int(end_text)
    else:
        if separator:
            raise ValueError("step requires '*' or a range")
        value = int(base)
        start = end = value
    if not (lower <= start <= upper and lower <= end <= upper):
        raise ValueError(f"value must be in {lower}-{upper}")
    if start > end:
        raise ValueError("range start must not exceed range end")
    return range(start, end + 1, step) if start != end or separator else (start,)


def _field_matches(field: str, value: int, lower: int = 0, upper: int = 59) -> bool:
    return any(value in _part_values(part.strip(), lower, upper)
               for part in field.split(",") if part.strip())


def cron_matches(expr: str, dt: datetime) -> bool:
    fields = expr.split()
    if len(fields) != 5:
        return False
    minute, hour, dom, month, dow = fields
    try:
        if not (_field_matches(minute, dt.minute, 0, 59)
                and _field_matches(hour, dt.hour, 0, 23)
                and _field_matches(month, dt.month, 1, 12)):
            return False
        dom_ok = _field_matches(dom, dt.day, 1, 31)
        dow_ok = _field_matches(dow, (dt.weekday() + 1) % 7, 0, 6)
    except (TypeError, ValueError):
        return False
    dom_free, dow_free = dom == "*", dow == "*"
    if dom_free and dow_free:
        return True
    if dom_free:
        return dow_ok
    if dow_free:
        return dom_ok
    return dom_ok or dow_ok


def validate_cron(expr: str) -> str | None:
    fields = expr.split()
    if len(fields) != 5:
        return "cron must have 5 fields: minute hour day-of-month month day-of-week"
    bounds = ((0, 59), (0, 23), (1, 31), (1, 12), (0, 6))
    names = ("minute", "hour", "day-of-month", "month", "day-of-week")
    for field_value, (lower, upper), name in zip(fields, bounds, names):
        try:
            parts = field_value.split(",")
            if not parts or any(not part for part in parts):
                raise ValueError("empty list item")
            for part in parts:
                _part_values(part, lower, upper)
        except (TypeError, ValueError) as error:
            return f"{name}: {error}"
    return None


@dataclass
class CronJob:
    id: str
    cron: str
    prompt: str
    session_id: str
    recurring: bool = True
    durable: bool = False
    last_fired: str = ""


class CronScheduler:
    """Manager-level. Holds all jobs; one ticker fires them into sessions."""

    def __init__(self, manager, *, durable_path: Path | None = None) -> None:
        self.manager = manager
        self.jobs: dict[str, CronJob] = {}
        self.durable_path = durable_path
        self._task: asyncio.Task | None = None
        self._running: set[asyncio.Task] = set()
        self._load()

    # -- lifecycle --
    def start(self) -> None:
        if self._task is None:
            self._task = asyncio.create_task(self._tick_loop())

    async def stop(self) -> None:
        if self._task:
            self._task.cancel()
            with contextlib.suppress(asyncio.CancelledError):
                await self._task
            self._task = None
        pending = [task for task in self._running if not task.done()]
        for task in pending:
            task.cancel()
        if pending:
            await asyncio.gather(*pending, return_exceptions=True)
        self._running.clear()

    async def _tick_loop(self) -> None:
        while True:
            try:
                self._tick_once(datetime.now())
            except Exception:
                pass
            await asyncio.sleep(20)

    def _tick_once(self, now: datetime) -> None:
        marker = now.strftime("%Y-%m-%d %H:%M")
        for job in list(self.jobs.values()):
            try:
                if not cron_matches(job.cron, now) or job.last_fired == marker:
                    continue
                job.last_fired = marker
                self._fire(job)
                if not job.recurring:
                    self.jobs.pop(job.id, None)
                if job.durable:
                    self._save()
            except Exception:
                # One malformed or unavailable job cannot starve the rest.
                continue

    def _fire(self, job: CronJob) -> None:
        session = self.manager.get(job.session_id)
        if session is None and hasattr(self.manager, "restore_scheduled_session"):
            session = self.manager.restore_scheduled_session(job.session_id)
        if session is not None:
            task = asyncio.create_task(session.run(f"[Scheduled cron {job.id}] {job.prompt}"))
            self._running.add(task)
            task.add_done_callback(self._running.discard)

    # -- ops (used by tools) --
    def schedule(self, session_id: str, cron: str, prompt: str, *, recurring: bool = True,
                 durable: bool = True) -> str:
        err = validate_cron(cron)
        if err:
            return f"Error: {err}"
        job = CronJob(id=uuid.uuid4().hex[:8], cron=cron, prompt=prompt,
                      session_id=session_id, recurring=recurring, durable=durable)
        self.jobs[job.id] = job
        # `install_cron()` also works à la carte, without comprehensive mode.
        # Scheduling from a running agent lazily starts the ticker.
        try:
            asyncio.get_running_loop()
        except RuntimeError:
            pass
        else:
            self.start()
        if durable:
            self._save()
        return f"Scheduled cron {job.id}: '{cron}' -> {prompt[:60]}"

    def cancel(self, job_id: str) -> str:
        if self.jobs.pop(job_id, None):
            self._save()
            return f"Cancelled cron {job_id}"
        return f"No cron {job_id}"

    def list_for(self, session_id: str) -> str:
        jobs = [j for j in self.jobs.values() if j.session_id == session_id]
        if not jobs:
            return "No scheduled jobs."
        return "\n".join(
            f"{j.id}: '{j.cron}' [{'recurring' if j.recurring else 'one-shot'}"
            f"{', durable' if j.durable else ''}] -> {j.prompt[:50]}" for j in jobs)

    # -- durability --
    def _save(self) -> None:
        if not self.durable_path:
            return
        self.durable_path.parent.mkdir(parents=True, exist_ok=True)
        durable = [asdict(j) for j in self.jobs.values() if j.durable]
        temporary = self.durable_path.with_suffix(".tmp")
        temporary.write_text(json.dumps(durable, indent=2))
        temporary.replace(self.durable_path)

    def _load(self) -> None:
        if self.durable_path and self.durable_path.exists():
            try:
                records = json.loads(self.durable_path.read_text())
            except (OSError, json.JSONDecodeError):
                return
            for record in records if isinstance(records, list) else []:
                try:
                    job = CronJob(**record)
                except (TypeError, ValueError):
                    continue
                if validate_cron(job.cron) is None:
                    self.jobs[job.id] = job


_SCHEDULE = {
    "type": "object",
    "properties": {
        "cron": {"type": "string", "description": "5-field cron: minute hour day-of-month month day-of-week"},
        "prompt": {"type": "string"},
        "recurring": {"type": "boolean"},
        "durable": {"type": "boolean"},
    },
    "required": ["cron", "prompt"],
}
_CANCEL = {"type": "object", "properties": {"job_id": {"type": "string"}}, "required": ["job_id"]}
_EMPTY = {"type": "object", "properties": {}}


def install_cron(registry: ToolRegistry) -> ToolRegistry:
    def _sched(ctx: ToolContext) -> CronScheduler | None:
        return ctx.state.get("cron")

    async def schedule_cron(ctx, cron, prompt, recurring=True, durable=True):
        sched = _sched(ctx)
        if sched is None:
            return "Error: cron scheduler not available"
        return sched.schedule(ctx.state.get("session_id", ""), cron, prompt,
                              recurring=recurring, durable=durable)

    async def list_crons(ctx):
        sched = _sched(ctx)
        return sched.list_for(ctx.state.get("session_id", "")) if sched else "Error: cron not available"

    async def cancel_cron(ctx, job_id):
        sched = _sched(ctx)
        return sched.cancel(job_id) if sched else "Error: cron not available"

    registry.register(Tool("schedule_cron", "Schedule a prompt to run on a cron schedule (wakes this session).", _SCHEDULE, schedule_cron))
    registry.register(Tool("list_crons", "List this session's scheduled cron jobs.", _EMPTY, list_crons, readonly=True))
    registry.register(Tool("cancel_cron", "Cancel a scheduled cron job by id.", _CANCEL, cancel_cron))
    return registry
