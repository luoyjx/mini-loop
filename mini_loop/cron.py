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

from .durable import atomic_write_text
from .problems import ProblemLog
from .run_context import RunContext
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

    #: A scheduled prompt is an instruction that fires unattended, so an
    #: unbounded one is a half-million-token user message nobody is watching.
    MAX_PROMPT = 8_000
    #: Jobs accumulate; 500 `schedule` calls produced 503 of them.
    MAX_JOBS = 200

    def __init__(self, manager, *, durable_path: Path | None = None, secrets=None) -> None:
        self.manager = manager
        self.jobs: dict[str, CronJob] = {}
        #: The durable file is a disk sink -- prompts persist across restarts.
        self.secrets = secrets
        #: Jobs refused, dropped or fired into nothing. Reported rather than
        #: swallowed: a schedule that silently stops running is indistinguishable
        #: from one that never existed.
        self.problems = ProblemLog()
        self.durable_path = durable_path
        self._task: asyncio.Task | None = None
        self._running: set[asyncio.Task] = set()
        #: Process-local activation, deliberately NOT persisted (DeepSeek
        #: Harness's goal-domain rule: durable state answers "what was
        #: scheduled", activation answers "may THIS process fire it
        #: unattended"). A job scheduled in this process is armed by the act
        #: of scheduling -- the authorization edge just happened. A job
        #: restored from disk is disarmed until `arm()` records a new edge:
        #: without this, one model turn that scheduled a durable job became
        #: unattended authority surviving every restart, with no human in the
        #: loop ever again -- "held" quietly replaced by "once held".
        self._armed: set[str] = set()
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
            except Exception as error:
                # `_tick_once` catches per job, so reaching here means the sweep
                # itself broke and *every* job missed this minute.
                self.problems.append(
                    f"a scheduler tick failed ({type(error).__name__}: {error}); "
                    "no job ran this minute"
                )
            await asyncio.sleep(20)

    def _tick_once(self, now: datetime) -> None:
        marker = now.strftime("%Y-%m-%d %H:%M")
        for job in list(self.jobs.values()):
            try:
                if not cron_matches(job.cron, now) or job.last_fired == marker:
                    continue
                if job.id not in self._armed:
                    # Restored but not re-authorized. The occurrence is NOT
                    # consumed (`last_fired` untouched): once armed, the next
                    # matching minute fires normally. Reported so a disarmed
                    # schedule is visible, not indistinguishable from a dead one.
                    self.problems.append(
                        f"{job.id}: restored from disk and not re-armed; "
                        "skipping until arm() records a new authorization"
                    )
                    continue
                job.last_fired = marker
                if not job.recurring:
                    self.jobs.pop(job.id, None)
                # Persist the mark (and any one-shot removal) BEFORE dispatching
                # the run. The old order fired first and saved after, so a crash
                # in that window left the run dispatched but the mark only in
                # memory -- a restart within the same minute re-fired it. This
                # is at-most-once across a crash, matching the in-process
                # semantics: if the durable state cannot be written, the
                # occurrence is not fired (and is reported below as lost), which
                # is the safe direction. A crash before the save means nothing
                # was dispatched, so re-firing on restart is correct.
                if job.durable:
                    self._save()
                self._fire(job)
            except Exception as error:
                # One malformed or unavailable job cannot starve the rest --
                # still true, and still not a reason to say nothing. The
                # occurrence is already marked fired by this point, so a
                # swallowed failure here is a scheduled run that silently did
                # not happen.
                self.problems.append(
                    f"{job.id}: firing failed ({type(error).__name__}: {error}); "
                    "the occurrence was lost"
                )
                continue

    def _fire(self, job: CronJob) -> None:
        session = self.manager.get(job.session_id)
        if session is None and hasattr(self.manager, "restore_scheduled_session"):
            session = self.manager.restore_scheduled_session(job.session_id)
        if session is None:
            # Silence here is the worst outcome: the occurrence is already
            # marked fired, so the schedule is consumed and nothing runs -- for
            # every occurrence, forever, with no signal that the job is dead.
            self.problems.append(
                f"{job.id}: fired at its scheduled time but session "
                f"{job.session_id!r} does not exist; the occurrence was lost"
            )
            return
        # A scheduled prompt fires unattended and durably -- it survives a
        # restart and runs with no human present. It must therefore carry
        # UNTRUSTED authority, never the human's: workflow launch and manage
        # require EXPLICIT_HUMAN (workflows/service.py, workflows/tools.py), so a
        # cron job running with human authority would let a model schedule one
        # turn that escalates to launching workflows on every later firing.
        # `session.run` already defaults to `RunContext.default()` (untrusted);
        # passing it explicitly makes the security choice visible at the firing
        # site and resistant to a well-meaning edit that hands cron "the
        # scheduler's authority".
        prompt = f"[Scheduled cron {job.id}] {job.prompt}"
        task = asyncio.create_task(
            session.run(prompt, run_context=RunContext.default())
        )
        self._running.add(task)
        task.add_done_callback(self._running.discard)

    # -- activation (process-local, never persisted) --
    def armed(self, job_id: str) -> bool:
        return job_id in self._armed

    def arm(self, job_id: str, session_id: str | None = None) -> str:
        """Record a new authorization edge for a restored job.

        Exposed at the operator surface (the HTTP API), scoped like `cancel`:
        with `session_id` given, a caller can only arm jobs that belong to
        that session. Deliberately NOT a model-facing tool -- a model that
        could re-arm its own restored schedule would erase the distinction
        this bit exists to draw.
        """

        job = self.jobs.get(job_id)
        if job is None:
            return f"Error: no such job {job_id}"
        if session_id is not None and job.session_id != session_id:
            return f"Error: no such job {job_id}"
        self._armed.add(job_id)
        return f"Armed {job_id}"

    def arm_all(self) -> int:
        """Arm every restored job in one deliberate operator act; the count."""

        disarmed = [job_id for job_id in self.jobs if job_id not in self._armed]
        self._armed.update(disarmed)
        return len(disarmed)

    # -- ops (used by tools) --
    def schedule(self, session_id: str, cron: str, prompt: str, *, recurring: bool = True,
                 durable: bool = True) -> str:
        err = validate_cron(cron)
        if err:
            return f"Error: {err}"
        if len(prompt) > self.MAX_PROMPT:
            # Refused, not truncated. A truncated instruction that still fires
            # is worse than one that never got scheduled.
            return (
                f"Error: prompt is {len(prompt):,} characters; the limit is "
                f"{self.MAX_PROMPT:,}"
            )
        if len(self.jobs) >= self.MAX_JOBS:
            return f"Error: {self.MAX_JOBS} scheduled jobs already exist"
        job = CronJob(id=uuid.uuid4().hex[:8], cron=cron, prompt=prompt,
                      session_id=session_id, recurring=recurring, durable=durable)
        self.jobs[job.id] = job
        # Scheduling IS the authorization edge for this process; only a
        # restart erases it (activation is never persisted).
        self._armed.add(job.id)
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

    def cancel(self, job_id: str, session_id: str | None = None) -> str:
        """Cancel a job, optionally only if it belongs to this session.

        `list_for` filters by session and this did not, so one session could
        cancel another's scheduled work by id -- the same "filtered index,
        unprotected direct reference" shape as the trajectory fetch in round 74
        and the trajectory listing in round 76, here for the third time.

        `session_id=None` keeps the unscoped behaviour for callers that own the
        scheduler outright (an operator, the tests). The tool passes its own
        session, so an agent can only cancel its own.

        A job that exists but belongs to someone else answers exactly like one
        that does not exist, per round 24: a distinguishable refusal confirms
        the id.
        """

        job = self.jobs.get(job_id)
        if job is None or (session_id is not None and job.session_id != session_id):
            return f"No cron {job_id}"
        del self.jobs[job_id]
        self._armed.discard(job_id)
        self._save()
        return f"Cancelled cron {job_id}"

    def cancel_for_session(self, session_id: str) -> int:
        """Remove every job scheduled for a session; returns how many.

        Called when the session is deleted. A leftover job outlives its session
        and, on its next fire, `restore_scheduled_session` rebuilds the session
        from durable state -- re-creating the workspace `delete` removed and
        rehydrating the transcript -- so the deleted session comes back to life
        and keeps running its scheduled prompt. `delete` reclaimed every other
        per-session resource (background shells, MCP clients, approvals) and left
        this one, so the one form of work that fires unattended was the one a
        delete did not stop.
        """

        removed = [jid for jid, job in self.jobs.items()
                   if job.session_id == session_id]
        if not removed:
            return 0
        persist = any(self.jobs[jid].durable for jid in removed)
        for jid in removed:
            self.jobs.pop(jid, None)
            self._armed.discard(jid)
        if persist:
            self._save()
        return len(removed)

    def list_for(self, session_id: str) -> str:
        jobs = [j for j in self.jobs.values() if j.session_id == session_id]
        if not jobs:
            return "No scheduled jobs."
        return "\n".join(
            f"{j.id}: '{j.cron}' [{'recurring' if j.recurring else 'one-shot'}"
            f"{', durable' if j.durable else ''}"
            f"{', DISARMED (restored; needs operator arm)' if j.id not in self._armed else ''}"
            f"] -> {j.prompt[:50]}" for j in jobs)

    # -- durability --
    def _save(self) -> None:
        if not self.durable_path:
            return
        self.durable_path.parent.mkdir(parents=True, exist_ok=True)
        durable = [asdict(j) for j in self.jobs.values() if j.durable]
        if self.secrets is not None:
            for record in durable:
                masked = self.secrets.mask(record["prompt"])
                if masked != record["prompt"]:
                    self.problems.append(
                        f"{record['id']}: prompt held a registered secret; the "
                        "stored copy is masked and will fire masked after a "
                        "restart"
                    )
                    record["prompt"] = masked
        atomic_write_text(self.durable_path, json.dumps(durable, indent=2))

    def _load(self) -> None:
        if self.durable_path and self.durable_path.exists():
            try:
                records = json.loads(self.durable_path.read_text())
            except (OSError, json.JSONDecodeError) as error:
                # Every durable job just vanished. Saying nothing makes a lost
                # schedule look like an empty one.
                self.problems.append(
                    f"{self.durable_path}: unreadable ({type(error).__name__}); "
                    "all durable jobs were dropped"
                )
                return
            for record in records if isinstance(records, list) else []:
                try:
                    job = CronJob(**record)
                except (TypeError, ValueError) as error:
                    self.problems.append(f"a stored job was unreadable: {error}")
                    continue
                if validate_cron(job.cron) is not None:
                    self.problems.append(
                        f"{job.id}: dropped, {job.cron!r} is not a valid cron "
                        "expression"
                    )
                    continue
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
        if not sched:
            return "Error: cron not available"
        return sched.cancel(job_id, session_id=ctx.state.get("session_id"))

    registry.register(Tool("schedule_cron", "Schedule a prompt to run on a cron schedule (wakes this session).", _SCHEDULE, schedule_cron, risk="write"))
    registry.register(Tool("list_crons", "List this session's scheduled cron jobs.", _EMPTY, list_crons, readonly=True, risk="read"))
    registry.register(Tool("cancel_cron", "Cancel a scheduled cron job by id.", _CANCEL, cancel_cron, risk="write"))
    return registry

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: activation is process-local by construction (_armed never rides _save/_load), pinned by mutation guards rather than a runtime probe."
)
