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
import json
import uuid
from dataclasses import asdict, dataclass, field
from datetime import datetime
from pathlib import Path

from .registry import Tool, ToolContext, ToolRegistry


def _field_matches(field: str, value: int) -> bool:
    for part in field.split(","):
        if part == "*":
            return True
        if part.startswith("*/"):
            step = int(part[2:])
            if step and value % step == 0:
                return True
        elif "-" in part:
            lo, hi = (int(x) for x in part.split("-", 1))
            if lo <= value <= hi:
                return True
        elif part.isdigit() and int(part) == value:
            return True
    return False


def cron_matches(expr: str, dt: datetime) -> bool:
    fields = expr.split()
    if len(fields) != 5:
        return False
    minute, hour, dom, month, dow = fields
    return (
        _field_matches(minute, dt.minute)
        and _field_matches(hour, dt.hour)
        and _field_matches(month, dt.month)
        # DOM/DOW OR semantics (cron quirk)
        and (_field_matches(dom, dt.day) or _field_matches(dow, dt.weekday() % 7))
    )


def validate_cron(expr: str) -> str | None:
    if len(expr.split()) != 5:
        return "cron must have 5 fields: minute hour day-of-month month day-of-week"
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
        self._load()

    # -- lifecycle --
    def start(self) -> None:
        if self._task is None:
            self._task = asyncio.create_task(self._tick_loop())

    async def stop(self) -> None:
        if self._task:
            self._task.cancel()
            self._task = None

    async def _tick_loop(self) -> None:
        while True:
            try:
                now = datetime.now()
                marker = now.strftime("%Y-%m-%d %H:%M")
                for job in list(self.jobs.values()):
                    if cron_matches(job.cron, now) and job.last_fired != marker:
                        job.last_fired = marker
                        self._fire(job)
                        if not job.recurring:
                            self.jobs.pop(job.id, None)
                            self._save()
            except Exception:
                pass
            await asyncio.sleep(20)

    def _fire(self, job: CronJob) -> None:
        session = self.manager.get(job.session_id)
        if session is not None:
            asyncio.create_task(session.run(f"[Scheduled cron {job.id}] {job.prompt}"))

    # -- ops (used by tools) --
    def schedule(self, session_id: str, cron: str, prompt: str, *, recurring: bool = True,
                 durable: bool = False) -> str:
        err = validate_cron(cron)
        if err:
            return f"Error: {err}"
        job = CronJob(id=uuid.uuid4().hex[:8], cron=cron, prompt=prompt,
                      session_id=session_id, recurring=recurring, durable=durable)
        self.jobs[job.id] = job
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
        durable = [asdict(j) for j in self.jobs.values() if j.durable]
        self.durable_path.write_text(json.dumps(durable, indent=2))

    def _load(self) -> None:
        if self.durable_path and self.durable_path.exists():
            for d in json.loads(self.durable_path.read_text()):
                job = CronJob(**d)
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

    async def schedule_cron(ctx, cron, prompt, recurring=True, durable=False):
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
