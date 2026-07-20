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

from .registry import Tool, ToolContext, ToolRegistry
from .tools import DANGEROUS, OUTPUT_CAP


SLOW_KEYWORDS = (
    " install", " build", " test", " deploy", " compile", "docker build",
    "pip install", "npm install", "pnpm install", "cargo build", "pytest", " make",
)


def is_slow_operation(command: str) -> bool:
    normalized = f" {command.lower()}"
    return any(keyword in normalized for keyword in SLOW_KEYWORDS)


def should_run_background(command: str, explicit: bool = False) -> bool:
    return explicit or is_slow_operation(command)


class BackgroundManager:
    def __init__(self, workspace, *, default_timeout: int = 300) -> None:
        self.workspace = workspace
        self.default_timeout = default_timeout
        self._tasks: dict[str, dict] = {}
        self._completed: list[dict] = []
        self._counter = 0

    def run(self, command: str, timeout: int | None = None) -> str:
        if any(d in command for d in DANGEROUS):
            return "Error: Dangerous command blocked"
        self._counter += 1
        bg_id = f"bg_{self._counter:04d}"
        self._tasks[bg_id] = {"status": "running", "command": command, "result": None}
        self._tasks[bg_id]["handle"] = asyncio.create_task(
            self._exec(bg_id, command, timeout or self.default_timeout)
        )
        return f"Started background task {bg_id}: {command[:80]}"

    async def _exec(self, bg_id: str, command: str, timeout: int) -> None:
        proc = None
        try:
            proc = await asyncio.create_subprocess_shell(
                command, cwd=str(self.workspace),
                stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.STDOUT,
            )
            try:
                out, _ = await asyncio.wait_for(proc.communicate(), timeout=timeout)
                result = (out or b"").decode("utf-8", "replace").strip()[:OUTPUT_CAP] or "(no output)"
                status = "completed"
            except asyncio.TimeoutError:
                proc.kill()
                await proc.wait()
                result, status = f"Error: Timeout ({timeout}s)", "error"
        except asyncio.CancelledError:
            if proc is not None and proc.returncode is None:
                proc.kill()
                await proc.wait()
            result, status = "Cancelled", "cancelled"
            self._tasks[bg_id].update(status=status, result=result)
            raise
        except Exception as e:
            result, status = f"Error: {e}", "error"
        self._tasks[bg_id].update(status=status, result=result)
        self._completed.append({"bg_id": bg_id, "status": status, "result": result})

    def check(self, bg_id: str | None = None) -> str:
        if bg_id:
            t = self._tasks.get(bg_id)
            return f"[{t['status']}] {t.get('result') or '(running)'}" if t else f"Unknown: {bg_id}"
        if not self._tasks:
            return "No background tasks."
        return "\n".join(f"{k}: [{v['status']}] {v['command'][:60]}" for k, v in self._tasks.items())

    def drain(self) -> list[dict]:
        done, self._completed = self._completed, []
        return done

    async def close(self) -> None:
        handles = [task.get("handle") for task in self._tasks.values()
                   if task.get("handle") is not None and not task["handle"].done()]
        for handle in handles:
            handle.cancel()
        if handles:
            await asyncio.gather(*handles, return_exceptions=True)


def _mgr(ctx: ToolContext) -> BackgroundManager:
    mgr = ctx.state.get("background")
    if mgr is None:
        mgr = ctx.state["background"] = BackgroundManager(ctx.workspace)
    return mgr


def background_manager_for(ctx: ToolContext) -> BackgroundManager:
    return _mgr(ctx)


async def background_injector(agent) -> list:
    """Drain completed background results into the next turn (an Agent injector)."""
    mgr = agent.state.get("background")
    if mgr is None:
        return []
    done = mgr.drain()
    if not done:
        return []
    text = "\n".join(
        f"<task_notification id=\"{d['bg_id']}\" status=\"{d['status']}\">\n{d['result']}\n</task_notification>"
        for d in done
    )
    await agent._send("background_result", count=len(done))
    return [{"role": "user", "content": text}]


_RUN = {
    "type": "object",
    "properties": {"command": {"type": "string"}, "timeout": {"type": "integer"}},
    "required": ["command"],
}
_CHECK = {"type": "object", "properties": {"bg_id": {"type": "string"}}}


def install_background(registry: ToolRegistry) -> ToolRegistry:
    async def background_run(ctx, command, timeout=None):
        return _mgr(ctx).run(command, timeout)

    async def check_background(ctx, bg_id=None):
        return _mgr(ctx).check(bg_id)

    registry.register(Tool("background_run", "Run a slow shell command in the background; returns a bg_id immediately. "
                                             "Results arrive later as a <task_notification>.", _RUN, background_run))
    registry.register(Tool("check_background", "Check background task status (all, or one bg_id).", _CHECK, check_background, readonly=True))
    return registry
