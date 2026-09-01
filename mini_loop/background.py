"""Background tasks (s13), async-native.

Slow shell commands run as `asyncio` tasks instead of blocking the loop; the
agent gets an immediate `bg_id` placeholder and keeps working. Completed
results are drained each turn by an *injector* and spliced into the next user
message as `<task_notification>` text -- decoupled from the original
tool_use_id (one tool_use still gets exactly one tool_result placeholder).

The teaching version (s13) uses OS threads; here they're asyncio tasks, which
is the natural fit for our single-event-loop server.

Enable per session with `install_background(registry)` +
`background_injector` in the agent's injectors.
"""

from __future__ import annotations

import asyncio
import contextlib
import json
import os
import time
from collections import deque
from pathlib import Path

from .durable import atomic_write_text
from .registry import Tool, ToolContext, ToolRegistry
from .tools import OUTPUT_CAP, capped, looks_dangerous


SLOW_KEYWORDS = (
    " install", " build", " test", " deploy", " compile", "docker build",
    "pip install", "npm install", "pnpm install", "cargo build", "pytest", " make",
)

#: Background results delivered in one `<task_notification>` injection. Each
#: result is already capped at `OUTPUT_CAP`, but the *count* was not: a long
#: round during which many tasks finish drains them all into one message and
#: floods the context, the team inbox's `MAX_INBOX` bound applied there and not
#: here. The overflow is not lost -- `check_background(bg_id)` still retrieves it.
MAX_NOTIFICATIONS = 50

#: Completed-task results retained in full. Each result is capped at OUTPUT_CAP,
#: but `_tasks` kept one per background command *forever* -- it is only ever
#: added to, never trimmed, so a session doing repeated background work held
#: every result indefinitely (400 tasks measured 8 MB; 20,000 would be ~1 GB).
#: This is the action journal's own leak (MAX_RESULTS_RETAINED) in a sibling
#: per-session result store that never inherited the same bound. Past this, the
#: oldest completed result text is released; the record stays so
#: `check_background` still answers, and the notification injection already
#: delivered the text when the task finished.
MAX_BACKGROUND_RESULTS = 100

#: Stands in for a result released to stay within the retention bound. The task
#: is still recorded as having completed -- only the (already delivered) text is
#: gone.
SHED_BACKGROUND_RESULT = (
    "[result released to stay within the retention bound; it was delivered as a "
    "task_notification when the task completed]"
)

#: Tasks shown by a no-argument `check_background` listing. The record store is
#: kept per-id on purpose so `check(bg_id)` still answers (above), but the
#: *aggregate* listing rendered one line per task ever run -- an unbounded tool
#: output, the one class of growth every other tool result is capped against.
#: The newest are shown and the rest summarised, so the listing tracks this cap
#: rather than the number of background commands the session has ever launched.
MAX_TASK_LISTING = 50


def is_slow_operation(command: str) -> bool:
    normalized = f" {command.lower()}"
    return any(keyword in normalized for keyword in SLOW_KEYWORDS)


def should_run_background(command: str, explicit: bool = False) -> bool:
    return explicit or is_slow_operation(command)


def _kill_group(proc) -> None:
    """End the command's whole process group. See `tools._kill_group`."""

    import contextlib
    import os
    import signal

    try:
        group = os.getpgid(proc.pid)
    except (ProcessLookupError, OSError):
        group = None
    # Never our own group -- see `tools._kill_group`.
    if group is not None and group != os.getpgid(0):
        with contextlib.suppress(ProcessLookupError, PermissionError, OSError):
            os.killpg(group, signal.SIGKILL)
    with contextlib.suppress(Exception):
        proc.kill()


class BackgroundManager:
    """Shell commands that outlive a turn.

    Every protection `Toolset.run_bash` has must apply here too, because it is
    the same shell reached through a different tool. It had none of them: the
    command ran through `create_subprocess_shell` rather than the sandbox, and
    with no `env`, so it inherited the whole process environment. Measured side
    by side on one machine:

        run_bash            printenv -> '<secret-hidden>',  escape blocked
        run_in_background   printenv -> 'sk-BACKGROUND-...', escape succeeded

    The escaping write landed outside the workspace. Confinement that one tool
    honours and its sibling ignores is not confinement.
    """

    def __init__(self, workspace, *, default_timeout: int = 300,
                 secrets=None, sandbox=None,
                 max_results_retained: int = MAX_BACKGROUND_RESULTS) -> None:
        self.workspace = workspace
        self.default_timeout = default_timeout
        self._tasks: dict[str, dict] = {}
        self._completed: list[dict] = []
        #: Finished task ids in completion order, oldest first: the shed queue.
        self._finished: deque[str] = deque()
        self.max_results_retained = max_results_retained
        self._counter = 0
        from .sandbox import NullSandbox
        from .secrets import NullSecretRegistry

        self.secrets = secrets or NullSecretRegistry()
        base = sandbox if sandbox is not None else NullSandbox()
        self.sandbox = base.for_workspace(self.workspace)
        #: Durable record of in-flight commands (roadmap G7). The transcript
        #: tells the model "Started background task bg_0001" durably; the
        #: manager holding the outcome was process memory. After a restart the
        #: fresh manager answered "Unknown: bg_0001" while the command's
        #: process -- its own session, deliberately -- may still be running
        #: unsupervised. One small file per in-flight command, removed when
        #: the task settles or is gracefully cancelled, so whatever is left
        #: at construction is exactly the orphaned work.
        self._ledger_dir = Path(self.workspace) / ".background"
        self._adopt_orphans()

    def run(self, command: str, timeout: int | None = None) -> str:
        if looks_dangerous(command):
            return "Error: Dangerous command blocked"
        self._counter += 1
        bg_id = f"bg_{self._counter:04d}"
        self._tasks[bg_id] = {"status": "running", "command": command, "result": None}
        # Recorded before the return so the crash window opens *after* the
        # durable record exists, matching the tool_result the model is about
        # to see. If the record cannot be written the task still runs, but
        # the caller is told a restart will lose track of it.
        recorded = self._write_ledger(bg_id, command, pid=None)
        self._tasks[bg_id]["handle"] = asyncio.create_task(
            self._exec(bg_id, command, timeout or self.default_timeout)
        )
        started = f"Started background task {bg_id}: {command[:80]}"
        if not recorded:
            started += " (unrecorded: a restart will lose track of it)"
        return started

    async def _exec(self, bg_id: str, command: str, timeout: int) -> None:
        proc = None
        try:
            # Same construction as `run_bash`: the sandbox owns argv, and the
            # environment is scrubbed with only the credentials this command
            # names put back.
            environment = self.secrets.scrub_env(os.environ)
            environment.update(self.secrets.env_for_command(command))
            proc = await asyncio.create_subprocess_exec(
                *self.sandbox.argv(command), cwd=str(self.workspace),
                env=environment,
                stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.STDOUT,
                # Its own process group, so a timeout or a cancel ends the whole
                # command. `proc.kill()` reaches only the shell: a background
                # task that backgrounds work outlives it, and this path exists
                # precisely for commands that run long.
                start_new_session=True,
            )
            # Best-effort pid update: with it, an orphan report can say
            # whether the process is still alive after a restart.
            self._write_ledger(bg_id, command, pid=proc.pid)
            try:
                out, _ = await asyncio.wait_for(proc.communicate(), timeout=timeout)
                # Masked here as well as at the agent boundary: this result is
                # stored, injected into the next turn, and read by `check`.
                # Rendered with the foreground rules (background census,
                # 2026-09-01): the old bare [:OUTPUT_CAP] slice truncated
                # SILENTLY and kept the head -- for command output the tail
                # is the part worth running the command for -- and a nonzero
                # exit vanished entirely, so a failed long build injected as
                # a clean "completed". Status stays lifecycle ("completed"
                # means ran-to-end); failure visibility lives in the text,
                # exactly like CommandResult.render().
                text = self.secrets.mask(
                    (out or b"").decode("utf-8", "replace").strip()
                )
                rendered = capped(text, keep_tail=True) if text else ""
                if proc.returncode not in (0, None):
                    note = f"(exit {proc.returncode})"
                    rendered = f"{rendered}\n{note}" if rendered else note
                result = rendered or "(no output)"
                status = "completed"
            except asyncio.TimeoutError:
                _kill_group(proc)
                result, status = f"Error: Timeout ({timeout}s)", "error"
        except asyncio.CancelledError:
            if proc is not None and proc.returncode is None:
                _kill_group(proc)
            result, status = "Cancelled", "cancelled"
            self._tasks[bg_id].update(status=status, result=result)
            # A graceful cancel killed the process group: nothing is left
            # running, so a record here would falsely report an orphan on
            # the next start.
            self._unledger(bg_id)
            raise
        except Exception as e:
            result, status = f"Error: {e}", "error"
        self._tasks[bg_id].update(status=status, result=result)
        self._completed.append({"bg_id": bg_id, "status": status, "result": result})
        self._settle(bg_id)

    def _write_ledger(self, bg_id: str, command: str, *, pid: int | None) -> bool:
        """Record an in-flight command durably; True when the record landed."""

        try:
            self._ledger_dir.mkdir(parents=True, exist_ok=True)
            atomic_write_text(
                self._ledger_dir / f"{bg_id}.json",
                json.dumps({
                    # Masked at the write: the ledger is a disk sink like any
                    # other, and commands carry credentials.
                    "bg_id": bg_id,
                    "command": self.secrets.mask(command)[:200],
                    "pid": pid,
                    "started_at": time.time(),
                }),
            )
            return True
        except OSError:
            return False

    def _unledger(self, bg_id: str) -> None:
        with contextlib.suppress(OSError):
            (self._ledger_dir / f"{bg_id}.json").unlink(missing_ok=True)

    def _adopt_orphans(self) -> None:
        """Surface work a dead process started and nobody will ever deliver.

        Whatever the ledger still holds at construction was in flight when
        the previous process stopped: its output will never be delivered,
        and its process -- deliberately its own session -- may still be
        running. Adopted as terminal `orphaned` records through the normal
        settle path, so the existing drain/injection machinery delivers the
        news and `check_background` answers instead of "Unknown". Never
        silently dropped, never silently re-run: the harness reports, the
        model (or the human) decides.
        """

        try:
            records = sorted(self._ledger_dir.glob("bg_*.json"))
        except OSError:
            return
        for path in records:
            bg_id = path.stem
            digits = bg_id.removeprefix("bg_")
            if digits.isdigit():
                # Adopted ids stay reserved; without this the fresh counter
                # reissues bg_0001 and the orphan record is overwritten.
                self._counter = max(self._counter, int(digits))
            command, pid = "(unreadable ledger record)", None
            try:
                record = json.loads(path.read_text())
                command = str(record.get("command") or command)
                pid = record.get("pid")
            except (OSError, json.JSONDecodeError):
                pass
            alive = False
            if isinstance(pid, int) and pid > 0:
                try:
                    os.kill(pid, 0)
                    alive = True
                except ProcessLookupError:
                    alive = False
                except OSError:
                    # Alive but not ours (EPERM): still running.
                    alive = True
            if alive:
                result = (
                    f"orphaned by a restart: {command} -- a process with pid "
                    f"{pid} is still alive and unsupervised; its output will "
                    "never be delivered. Re-run if the work matters, or kill "
                    "the pid if it should stop."
                )
            else:
                result = (
                    f"orphaned by a restart: {command} -- no live process; "
                    "whether it completed is unknown. Verify its effects "
                    "before re-running."
                )
            self._tasks[bg_id] = {
                "status": "orphaned", "command": command, "result": result,
            }
            self._completed.append(
                {"bg_id": bg_id, "status": "orphaned", "result": result}
            )
            self._settle(bg_id)

    def _settle(self, bg_id: str) -> None:
        """Record a finished task and release result text beyond the bound.

        `_completed` is drained each turn, but `_tasks` kept every result for
        `check_background` and never trimmed. Shedding the oldest keeps peak
        memory tracking the bound rather than the number of tasks ever run; the
        `_completed` copy holds its own reference, so a not-yet-drained
        notification still delivers the full text.
        """

        self._unledger(bg_id)
        self._finished.append(bg_id)
        while len(self._finished) > self.max_results_retained:
            old = self._finished.popleft()
            task = self._tasks.get(old)
            if task is None or task.get("result") in (None, SHED_BACKGROUND_RESULT):
                continue
            task["result"] = SHED_BACKGROUND_RESULT
            task.pop("handle", None)  # a completed asyncio.Task, no longer needed

    def live_count(self) -> int:
        """How many tasks are still running right now.

        Exists for the interruption marker (session._record_interruption):
        a cancelled turn does not stop background work -- outliving the turn
        is this manager's whole contract -- and the model must be told that,
        or its next turn reasons about a world where the interruption stopped
        everything.
        """

        return sum(1 for t in self._tasks.values() if t.get("status") == "running")

    def check(self, bg_id: str | None = None) -> str:
        if bg_id:
            t = self._tasks.get(bg_id)
            return f"[{t['status']}] {t.get('result') or '(running)'}" if t else f"Unknown: {bg_id}"
        if not self._tasks:
            return "No background tasks."
        # `_tasks` is insertion-ordered, so the newest are last. Render the most
        # recent and summarise the rest: an uncapped listing grew one line per
        # task *ever run*, and after a long session of background work that alone
        # floods the model context -- the very growth every other tool result is
        # bounded against. Older tasks stay queryable one at a time by id.
        items = list(self._tasks.items())
        shown = items[-MAX_TASK_LISTING:]
        lines = [f"{k}: [{v['status']}] {v['command'][:60]}" for k, v in shown]
        hidden = len(items) - len(shown)
        if hidden:
            lines.insert(
                0, f"... ({hidden} older task(s) not shown; check a bg_id directly)"
            )
        return "\n".join(lines)

    def drain(self) -> list[dict]:
        done, self._completed = self._completed, []
        return done

    def cancel_all(self) -> list:
        """Request cancellation of every live task, without awaiting it.

        Callable with no running loop: cancellation is delivered whenever the
        loop next runs the task, and the CancelledError path kills the whole
        process group. Returns the handles so an async caller can await them.
        """

        handles = [task.get("handle") for task in self._tasks.values()
                   if task.get("handle") is not None and not task["handle"].done()]
        for handle in handles:
            handle.cancel()
        return handles

    async def close(self) -> None:
        handles = self.cancel_all()
        if handles:
            await asyncio.gather(*handles, return_exceptions=True)


def _mgr(ctx: ToolContext) -> BackgroundManager:
    mgr = ctx.state.get("background")
    if mgr is None:
        agent = getattr(ctx, "agent", None)
        mgr = ctx.state["background"] = BackgroundManager(
            ctx.workspace,
            secrets=getattr(agent, "secrets", None),
            sandbox=getattr(agent, "sandbox", None),
        )
    return mgr


def background_manager_for(ctx: ToolContext) -> BackgroundManager:
    return _mgr(ctx)


async def background_injector(agent) -> list:
    """Drain completed background results into the next turn (an Agent injector)."""
    mgr = agent.state.get("background")
    if mgr is None:
        # The manager is lazily built by the first background tool call --
        # but after a restart, orphaned work must surface even if the model
        # never touches a background tool again. A non-empty ledger is the
        # signal; constructing the manager adopts it.
        ledger = Path(agent.workspace) / ".background"
        try:
            has_orphans = any(ledger.iterdir())
        except OSError:
            has_orphans = False
        if not has_orphans:
            return []
        mgr = agent.state["background"] = BackgroundManager(
            agent.workspace,
            secrets=getattr(agent, "secrets", None),
            sandbox=getattr(agent, "sandbox", None),
        )
    done = mgr.drain()
    if not done:
        return []
    dropped = max(0, len(done) - MAX_NOTIFICATIONS)
    shown = done[-MAX_NOTIFICATIONS:] if dropped else done  # keep the newest
    text = "\n".join(
        f"<task_notification id=\"{d['bg_id']}\" status=\"{d['status']}\">\n{d['result']}\n</task_notification>"
        for d in shown
    )
    if dropped:
        text += (
            f"\n[{dropped} earlier background result(s) omitted from this batch; "
            "retrieve any with check_background(bg_id)]"
        )
    await agent._send("background_result", count=len(shown), dropped=dropped)
    return [{"role": "user", "content": text}]


_RUN = {
    "type": "object",
    "properties": {
        "command": {"type": "string"},
        "timeout": {"type": "integer"},
        "approval_prefix": {
            "type": "array",
            "items": {"type": "string"},
            "description": (
                "Optional: propose the command prefix the human may remember "
                "for the session if this needs approval (see bash)."
            ),
        },
    },
    "required": ["command"],
}
_CHECK = {"type": "object", "properties": {"bg_id": {"type": "string"}}}


def install_background(registry: ToolRegistry) -> ToolRegistry:
    async def background_run(ctx, command, timeout=None, approval_prefix=None):
        # `approval_prefix` is approval-layer metadata (approvals.py);
        # execution ignores it.
        return _mgr(ctx).run(command, timeout)

    async def check_background(ctx, bg_id=None):
        return _mgr(ctx).check(bg_id)

    registry.register(Tool("background_run", "Run a slow shell command in the background; returns a bg_id immediately. "
                                             "Results arrive later as a <task_notification>.", _RUN, background_run, risk="exec"))
    registry.register(Tool("check_background", "Check background task status (all, or one bg_id).", _CHECK, check_background, readonly=True, risk="read"))
    return registry

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: task lifecycle ends in cancel_all/close which await quiescence; a leaked handle would be visible to tests, not to a runtime probe."
)
