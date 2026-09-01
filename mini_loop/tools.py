"""Per-workspace tools and the tool schemas the model sees.

This is the s02 dispatch pattern, with two changes for multi-tenancy:

  * file/glob tools enforce a *single session's* workspace via `safe_path`;
    shell commands use that workspace as their cwd (not an OS security sandbox);
  * the blocking calls (subprocess, file I/O) are wrapped in `asyncio.to_thread`
    so one agent's `bash` never stalls the event loop the others share.

The schemas live here as plain dicts so both the main agent and its subagents
can compose tool subsets from one source of truth.
"""

from __future__ import annotations

import asyncio
import glob as globlib
import contextlib
from dataclasses import dataclass
import os
import signal
import subprocess
import threading
import time
from pathlib import Path

from .durable import atomic_write_text

# --- Tool schemas (the contract the model reasons over) --------------------

BASH = {
    "name": "bash",
    "description": "Run a shell command in the workspace. Returns combined stdout+stderr.",
    "input_schema": {
        "type": "object",
        "properties": {
            "command": {"type": "string"},
            "run_in_background": {
                "type": "boolean",
                "description": "Run asynchronously and return a task id immediately.",
            },
            "approval_prefix": {
                "type": "array",
                "items": {"type": "string"},
                "description": (
                    "Optional: when this command will need approval and is part "
                    "of a routine you expect to repeat, propose the command "
                    "prefix the human may remember for the rest of the session, "
                    "e.g. [\"git\", \"pull\"]. It must be the command's own "
                    "leading words, at least two; interpreter, deleter, and "
                    "escalator heads are never remembered."
                ),
            },
        },
        "required": ["command"],
    },
}
READ_FILE = {
    "name": "read_file",
    "description": "Read a file's contents (workspace-relative path).",
    "input_schema": {
        "type": "object",
        "properties": {
            "path": {"type": "string"},
            "limit": {"type": "integer"},
            "offset": {"type": "integer"},
        },
        "required": ["path"],
    },
}
GLOB = {
    "name": "glob",
    "description": "Find workspace files matching a glob pattern.",
    "input_schema": {
        "type": "object",
        "properties": {"pattern": {"type": "string"}},
        "required": ["pattern"],
    },
}
WRITE_FILE = {
    "name": "write_file",
    "description": "Write content to a file (creates parent dirs).",
    "input_schema": {
        "type": "object",
        "properties": {"path": {"type": "string"}, "content": {"type": "string"}},
        "required": ["path", "content"],
    },
}
EDIT_FILE = {
    "name": "edit_file",
    "description": "Replace the first exact occurrence of old_text with new_text.",
    "input_schema": {
        "type": "object",
        "properties": {
            "path": {"type": "string"},
            "old_text": {"type": "string"},
            "new_text": {"type": "string"},
        },
        "required": ["path", "old_text", "new_text"],
    },
}

# Convenience bundles used when composing main-agent vs. subagent tool sets.
FILE_TOOLS = [BASH, READ_FILE, WRITE_FILE, EDIT_FILE, GLOB]
READONLY_TOOLS = [BASH, READ_FILE, GLOB]

OUTPUT_CAP = 50_000
#: Characters `read_file` pulls into memory at once. The *output* is capped at
#: OUTPUT_CAP, but `read_text()` loaded the whole file first, so a model that
#: created a huge file (shell output is capped, but a file it writes is not) and
#: then read it would OOM the process -- every tenant on it with it. Larger than
#: OUTPUT_CAP so the output cap still governs a normal read; a file past this is
#: truncated rather than loaded whole.
READ_CHAR_CAP = 2_000_000

#: Characters of command output `run_bash` reads into memory. `communicate()`
#: read *all* of stdout before `capped` ever ran, so a high-output command
#: (`yes`, `cat /dev/zero | tr`, `base64 /dev/urandom`) produced gigabytes within
#: the timeout window and OOMed the host: 40 MB of output measured 120 MB
#: resident. The timeout bounds time; this bounds memory (the round-140 "bounded
#: output is not bounded work" hazard, for bash). Well above OUTPUT_CAP so a
#: normal command's true end still survives `keep_tail`; past it the capture
#: stops and the command is ended rather than buffered.
MAX_BASH_CAPTURE = 5_000_000


@dataclass(frozen=True, slots=True)
class CommandResult:
    """Structured, masked result from one foreground shell command.

    ``stdout`` and ``stderr`` retain their separate meanings for reducers and
    audit code. ``render`` is the compatibility string projection the model
    receives through :meth:`Toolset.run_bash`: stdout followed by stderr and
    bounded with the same tail-preserving policy. Cross-stream interleaving is
    not reconstructed; consumers that care about channel semantics use the
    structured fields.

    ``error`` represents a failure outside the command's own exit status (for
    example a blocked command, failure to start, or the harness deadline).  It
    is intentionally separate from stderr so callers never mistake a harness
    decision for bytes produced by the child process.
    """

    stdout: str
    stderr: str
    exit_code: int | None
    timed_out: bool
    overflowed: bool
    duration_ms: int
    error: str | None = None
    projection: str | None = None
    capture_limit: int = MAX_BASH_CAPTURE

    def render(self) -> str:
        # Orthogonal outcomes, independently reported: a command can time out
        # AND have printed the diagnostic that explains why (the last line of
        # a spinning loop, a stack trace before a hang). The old projection
        # nested the whole report inside the error branch -- `Error: Timeout`
        # with the captured output silently discarded, though the structured
        # fields carried it. Each fact now reports on its own.
        out = (
            self.projection
            if self.projection is not None
            else self.stdout + self.stderr
        ).strip()
        rendered = capped(out, keep_tail=True) if out else ""
        if self.overflowed and rendered:
            rendered += (
                f"\n[output exceeded {self.capture_limit:,} bytes; capture "
                "stopped and the command was ended]"
            )
        if self.error is not None:
            return f"{rendered}\n{self.error}" if rendered else self.error
        return rendered or "(no output)"

    def __str__(self) -> str:
        return self.render()


class _BoundedCapture:
    """Read stdout and stderr under one aggregate bound.

    Each pipe is drained in its own thread, so neither can fill while the other
    is active.  Both readers debit the same locked budget: separating the
    streams must not silently turn the old five-megabyte ceiling into ten.
    Once a byte beyond the budget is observed, the producer is ended.
    """

    def __init__(self, limit: int, on_overflow) -> None:
        self._limit = limit
        self._on_overflow = on_overflow
        self._lock = threading.Lock()
        self._size = 0
        self._parts: dict[str, list[str]] = {"stdout": [], "stderr": []}
        self.overflowed = False

    def drain(self, stream, channel: str) -> None:
        while True:
            # Reading one character when the budget is exactly exhausted lets
            # us distinguish "exactly at the cap" from real overflow without
            # retaining anything beyond the cap.
            with self._lock:
                if self.overflowed:
                    return
                room = self._limit - self._size
            chunk = stream.read(min(65536, room) if room > 0 else 1)
            if not chunk:
                return
            notify = False
            with self._lock:
                if self.overflowed:
                    return
                room = self._limit - self._size
                accepted = chunk[:room]
                if accepted:
                    self._parts[channel].append(accepted)
                    self._size += len(accepted)
                if len(chunk) > len(accepted):
                    self.overflowed = True
                    notify = True
            if notify:
                self._on_overflow()  # both pipes reach EOF after the group ends
                return

    def finish(self) -> tuple[str, str]:
        """Return captured streams and release the chunk lists."""

        with self._lock:
            stdout = "".join(self._parts["stdout"])
            stderr = "".join(self._parts["stderr"])
            self._parts = {"stdout": [], "stderr": []}
        return stdout, stderr


def _kill_group(process) -> None:
    """End a command's whole process group, then reap it.

    `SIGKILL` rather than `SIGTERM`: the group is being killed because it
    ignored a deadline, and a second grace period for something already past one
    is how orphans survive.
    """

    try:
        group = os.getpgid(process.pid)
    except (ProcessLookupError, OSError):
        group = None
    # Never signal our own group. If the child was not started in a new session
    # it shares ours, and `killpg` would take down the harness -- discovered
    # when the mutation that removes `start_new_session=True` hung the guard
    # verifier instead of failing its test. A safety net that kills the process
    # holding it is worse than none.
    if group is not None and group != os.getpgid(0):
        with contextlib.suppress(ProcessLookupError, PermissionError, OSError):
            os.killpg(group, signal.SIGKILL)
    with contextlib.suppress(Exception):
        process.kill()
    with contextlib.suppress(Exception):
        process.wait(timeout=5)


def capped(text: str, *, keep_tail: bool = False) -> str:
    """Truncate to `OUTPUT_CAP`, saying so.

    `run_bash` and `read_file` cut at exactly 50,000 characters and said
    nothing, so the agent received output ending mid-stream with no way to know
    more existed -- it reasons about "the end" of a file it never saw, or
    concludes a search found no further matches. Two of the three sites had
    already thought about truncation and then applied a blanket `[:OUTPUT_CAP]`
    that silently truncated again.

    `keep_tail` is for command output, where the important part is usually at
    the end: a test summary, a stack trace, an exit status. Keeping only the
    head throws away exactly the part that was worth running the command for.
    """

    if len(text) <= OUTPUT_CAP:
        return text
    note = f"\n[truncated: {len(text):,} characters capped at {OUTPUT_CAP:,}]"
    if not keep_tail:
        return text[: OUTPUT_CAP - len(note)] + note
    head = OUTPUT_CAP // 2
    tail = OUTPUT_CAP - head - len(note) - 64
    omitted = len(text) - head - tail
    return (
        text[:head]
        + f"\n[... {omitted:,} characters omitted from the middle ...]\n"
        + text[-tail:]
        + note
    )
#: Commands refused before the shell sees them.
#:
#: **This is a typo guard, not a security control, and it cannot become one.**
#: It is substring matching on a command string, and a shell has unlimited ways
#: to spell the same instruction -- `$(echo rm) -rf /`, `r\'\'m -rf /`,
#: `find / -delete`, or any of them read from a file. Every one of those is
#: below, pinned as *not blocked*, so nobody extends this list believing they
#: are closing a hole.
#:
#: What it is worth: an agent that has decided to run `sudo` or `rm -rf /`
#: usually got there by mistake, and stopping the literal spelling is cheap.
#: What confines a shell is `SeatbeltSandbox` (or a container). The audit says
#: so when `bash` is registered without one.
DANGEROUS = ("rm -rf /", "sudo", "shutdown", "reboot", "> /dev/", ":(){", "mkfs", "dd if=")


def looks_dangerous(command: str) -> bool:
    """Match the blocklist against a whitespace-normalized command.

    `rm  -rf  /` with a doubled space slipped through raw substring matching --
    and a doubled space is a typo, which is the one thing this check is actually
    for. Normalizing does not make it a security control; it makes it do the job
    it does have.
    """

    normalized = " ".join(command.split())
    return any(pattern in normalized for pattern in DANGEROUS)


class Toolset:
    """The five base tools, sandboxed to one workspace directory."""

    def __init__(
        self,
        workspace: Path,
        *,
        bash_timeout: int = 120,
        secrets=None,
        sandbox=None,
        spill=None,
    ) -> None:
        self.workspace = workspace.resolve()
        self.workspace.mkdir(parents=True, exist_ok=True)
        self.bash_timeout = bash_timeout
        from .sandbox import NullSandbox
        from .secrets import NullSecretRegistry

        self.secrets = secrets if secrets is not None else NullSecretRegistry()
        # OS-level confinement for the shell. Default is host execution.
        # Bind the sandbox to *this* workspace: a shared sandbox object whose
        # writable root still points at a previous workspace denies every write.
        base = sandbox if sandbox is not None else NullSandbox()
        self.sandbox = base.for_workspace(self.workspace)
        # Optional spill store: when present, an output too large to inline is
        # preserved verbatim and the preview carries a locator; when absent,
        # truncation keeps its old drop-the-middle behavior.
        self.spill = spill
        # Foreground shells currently executing, so a cancelled turn can end
        # them. `run_bash` runs in worker threads; the lock keeps the set
        # coherent if a misdeclared parallel-safe tool ever runs two at once.
        import threading

        self._live_lock = threading.Lock()
        self._live: set = set()

    def _spill_note(self, text: str, *, tool: str, label: str = "result") -> str:
        """Preserve an over-cap `text`; return the locator note, or "".

        Truncation used to *destroy* the overflow: `capped` kept a preview and
        the rest was gone, unrecoverable, even though the model was told it
        existed. With a spill store configured the full (already masked) text
        is saved first and the preview gains a locator plus the backend's own
        retrieval hint. Preservation is best-effort by contract: a failed save
        returns "" and the caller keeps the plain preview -- it never turns a
        successful tool call into an error, and never costs the model the
        preview it already had.

        `read_file` deliberately does not spill: its source of truth is a
        model-reachable file and the truncation notice already says how to
        read further (`offset`); a spill copy would add disk cost and a second
        divergent copy without adding any capability. `glob` does not spill
        either -- round 167 bounded its *enumeration*, so the overflow is
        never collected in the first place.
        """

        if len(text) <= OUTPUT_CAP or self.spill is None:
            return ""
        try:
            ref = self.spill.save_text(
                session_id=self.workspace.name,
                tool_name=tool,
                label=label,
                suggested_name=f"{tool}.txt",
                content=text,
            )
        except Exception:
            return ""
        return (
            f"\n[full output preserved: {ref.locator} ({ref.bytes:,} bytes); "
            f"{ref.retrieval_hint}]"
        )

    # -- path safety: nothing may escape the session's workspace --
    def safe_path(self, p: str) -> Path:
        path = (self.workspace / p).resolve()
        if path != self.workspace and not path.is_relative_to(self.workspace):
            raise ValueError(f"Path escapes workspace: {p}")
        return path

    # -- blocking primitives (run via to_thread in `dispatch`) --
    def run_bash_result(self, command: str) -> CommandResult:
        """Run a foreground shell and return its structured, masked result."""

        started_ns = time.monotonic_ns()

        def result(
            *,
            stdout: str = "",
            stderr: str = "",
            exit_code: int | None = None,
            timed_out: bool = False,
            overflowed: bool = False,
            error: str | None = None,
            projection: str | None = None,
        ) -> CommandResult:
            return CommandResult(
                stdout=stdout,
                stderr=stderr,
                exit_code=exit_code,
                timed_out=timed_out,
                overflowed=overflowed,
                duration_ms=max(0, (time.monotonic_ns() - started_ns) // 1_000_000),
                error=error,
                projection=projection,
                capture_limit=MAX_BASH_CAPTURE,
            )

        if looks_dangerous(command):
            return result(error="Error: Dangerous command blocked")
        # Narrow injection: the shell sees registered credentials only when the
        # command names them, so an unrelated `printenv` has nothing to read.
        env = self.secrets.scrub_env(os.environ)
        env.update(self.secrets.env_for_command(command))
        try:
            # The sandbox owns argv construction, so the shell is invoked the
            # same way whether or not confinement is active.
            # In its own process group, so a timeout can end the *whole*
            # command rather than only the shell that started it.
            #
            # `subprocess.run(timeout=...)` kills the direct child. A command
            # that backgrounds work survives it: three spin loops started by one
            # `run_bash` call were still burning CPU after the harness had
            # reported `Error: Timeout (3s)` and the agent had moved on. Repeat
            # that a few times and the host is unusable, with nothing in the
            # transcript to explain why.
            process = subprocess.Popen(
                self.sandbox.argv(command), cwd=self.workspace,
                stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env,
                text=True, encoding="utf-8", errors="replace",
                start_new_session=True,
            )
            with self._live_lock:
                self._live.add(process)
            # Drain both pipes concurrently under one aggregate byte bound, so
            # either high-output stream cannot fill memory or block the child.
            capture = _BoundedCapture(MAX_BASH_CAPTURE, lambda: _kill_group(process))
            assert process.stdout is not None and process.stderr is not None
            readers = [
                threading.Thread(
                    target=capture.drain,
                    args=(process.stdout, "stdout"),
                    daemon=True,
                ),
                threading.Thread(
                    target=capture.drain,
                    args=(process.stderr, "stderr"),
                    daemon=True,
                ),
            ]
            for reader in readers:
                reader.start()
            # A reader ends at its pipe's EOF, which needs the command *and* any
            # child holding that pipe to exit. Bound the whole drain by one
            # shared deadline (not one timeout per stream),
            # then end the group. The *second* join is bounded too: a child that
            # keeps a pipe open past the kill (or a kill that does not reach the
            # group) must fail this command, not hang the harness -- an unbounded
            # join here is the wait `communicate(timeout=...)` used to own.
            deadline = time.monotonic() + self.bash_timeout
            for reader in readers:
                reader.join(timeout=max(0.0, deadline - time.monotonic()))
            timed_out = any(reader.is_alive() for reader in readers)
            if not timed_out and process.poll() is None:
                try:
                    process.wait(timeout=max(0.0, deadline - time.monotonic()))
                except subprocess.TimeoutExpired:
                    timed_out = True
            if timed_out:
                _kill_group(process)
            with contextlib.suppress(subprocess.TimeoutExpired):
                process.wait(timeout=5)
            for reader in readers:
                reader.join(timeout=5)
            with self._live_lock:
                self._live.discard(process)
            stdout, stderr = capture.finish()
            # Mask here too, not only at the agent boundary: narrow injection
            # still hands a secret to a command that names it, and a direct
            # Toolset caller never passes through `Agent._exec_tool`. Each whole
            # captured stream is masked before any projection can truncate it,
            # so a secret cannot be split into a leaking fragment within a stream.
            projection = self.secrets.mask(stdout + stderr)
            stdout = self.secrets.mask(stdout)
            stderr = self.secrets.mask(stderr)
            # A credential can be deliberately split between the two pipes.
            # Masking each stream alone would then let ``render`` reconstruct
            # the complete value.  In that exceptional case the safe combined
            # projection becomes the only returned stream content.
            if projection != stdout + stderr:
                stdout, stderr = projection, ""
            if timed_out:
                return result(
                    stdout=stdout,
                    stderr=stderr,
                    exit_code=process.returncode,
                    timed_out=True,
                    overflowed=capture.overflowed,
                    error=f"Error: Timeout ({self.bash_timeout}s)",
                    projection=projection,
                )
            return result(
                stdout=stdout,
                stderr=stderr,
                exit_code=process.returncode,
                overflowed=capture.overflowed,
                projection=projection,
            )
        except (FileNotFoundError, OSError) as e:
            return result(error=self.secrets.mask(f"Error: {e}"))

    def run_bash(self, command: str) -> str:
        """Compatibility projection used by the existing ``bash`` tool."""

        result = self.run_bash_result(command)
        rendered = result.render()
        if result.error is not None and result.projection is None:
            return rendered
        # The projection is already masked (run_bash_result masks each whole
        # stream before any truncation), so the spilled copy is safe to keep.
        full = (
            result.projection
            if result.projection is not None
            else result.stdout + result.stderr
        ).strip()
        return rendered + self._spill_note(full, tool="bash", label="output")

    def run_read(self, path: str, limit: int | None = None, offset: int = 0) -> str:
        try:
            offset = max(int(offset or 0), 0)
            # Bounded read: never load more than READ_CHAR_CAP into memory, so a
            # pathologically large file cannot OOM the process. `offset` skips
            # that many lines *from the file*, then the cap bounds what is read
            # from there. The old read capped first and applied the line offset
            # *within* that window, so any line past READ_CHAR_CAP was
            # unreachable -- while the truncation notice told the model to seek
            # it with `offset`. Now a larger offset genuinely reaches later lines.
            with self.safe_path(path).open("r", encoding="utf-8", errors="replace") as handle:
                hit_eof = False
                skipped = 0
                for _ in range(offset):
                    # Skip one line, reading in READ_CHAR_CAP-sized pieces so an
                    # overlong line cannot pull more than the cap into memory --
                    # readline(size) stops at a newline *or* `size` chars.
                    saw_content = False
                    while True:
                        piece = handle.readline(READ_CHAR_CAP)
                        if not piece:
                            hit_eof = True
                            break
                        saw_content = True
                        if piece.endswith("\n"):
                            break
                    if saw_content:
                        skipped += 1
                    if hit_eof:
                        break
                data = "" if hit_eof else handle.read(READ_CHAR_CAP + 1)
            truncated_read = len(data) > READ_CHAR_CAP
            if not data and offset > 0:
                # Micro-experiment A (docs/RSI_RESEARCH_AND_PLAN.md §5): an
                # offset past the end used to answer the same empty string as
                # an empty file, so the model could not tell "paged too far"
                # from "nothing there". Name the end instead.
                return (f"... (nothing at offset {offset}: the file ends "
                        f"after {skipped} lines)")
            lines = data[:READ_CHAR_CAP].splitlines()
            limit = max(int(limit), 0) if limit is not None else None
            if limit is not None and limit < len(lines):
                tail = ", read truncated" if truncated_read else ""
                lines = lines[:limit] + [f"... ({len(lines) - limit} more lines{tail})"]
            elif truncated_read:
                # Micro-experiment B (docs/RSI_RESEARCH_AND_PLAN.md §5): as
                # the LAST line this guidance sat exactly where the head-only
                # output cap cuts, so the pathological input that most needed
                # it never saw it. First line survives any head truncation.
                lines.insert(
                    0,
                    f"... (file exceeds {READ_CHAR_CAP:,} characters from this "
                    "offset; read further with a larger `offset`)"
                )
            return capped("\n".join(lines))
        except Exception as e:
            return f"Error: {e}"

    def run_glob(self, pattern: str) -> str:
        try:
            marker = "... (matches truncated)"
            # Reserve room for the trailing notice so `capped` cannot trim it
            # off the end -- the old body filled OUTPUT_CAP exactly, so a notice
            # appended after it would have been the first thing cut.
            budget = OUTPUT_CAP - len(marker) - 1
            matches = []
            total = 0
            truncated = False
            for match in globlib.iglob(pattern, root_dir=self.workspace, recursive=True):
                resolved = (self.workspace / match).resolve()
                if resolved == self.workspace or resolved.is_relative_to(self.workspace):
                    if total + len(match) + 1 > budget:
                        truncated = True
                        break
                    matches.append(match)
                    total += len(match) + 1
            if not matches:
                return "(no matches)"
            lines = sorted(set(matches))
            if truncated:
                # Appended *after* the sort. Mixed into `matches` it sorted by
                # its leading "." to the top of the list, so the notice read as
                # if truncation happened before any match rather than after the
                # last one shown -- it belongs at the end, as a trailing signal.
                lines.append(marker)
            return capped("\n".join(lines))
        except Exception as e:
            return f"Error: {e}"

    def run_write(self, path: str, content: str) -> str:
        try:
            fp = self.safe_path(path)
            # Atomic: write beside the target and rename over it, the same
            # temp+fsync+rename the durable store uses everywhere else. A bare
            # write_text truncates the target in place, so a crash mid-write
            # leaves a half-written file, and a teammate sharing the workspace
            # can read one -- the file is either fully old or fully new, never a
            # torn mix, and a failed write leaves the original untouched.
            atomic_write_text(fp, content)
            return f"Wrote {len(content)} bytes to {path}"
        except Exception as e:
            return f"Error: {e}"

    def run_edit(self, path: str, old_text: str, new_text: str) -> str:
        try:
            fp = self.safe_path(path)
            # Edit needs the whole file to replace within it, so `read_file`'s
            # bounded read (round 140) cannot apply here -- reading a huge file
            # whole would OOM the process, every tenant with it. Refuse instead:
            # the same danger, the same agent reach (a file the agent grew), a
            # different remedy because the operation cannot be truncated.
            size = fp.stat().st_size
            if size > READ_CHAR_CAP:
                return (
                    f"Error: {path} is {size:,} bytes; too large to edit in one "
                    f"pass (limit {READ_CHAR_CAP:,}). Rewrite it with write_file."
                )
            content = fp.read_text()
            occurrences = content.count(old_text)
            if occurrences == 0:
                return f"Error: Text not found in {path}"
            if occurrences > 1:
                # Replacing the first of several silently edits a location the
                # model may not have meant, and reports success so it never
                # learns. Claude Code's Edit and Aider both require the anchor to
                # be unique for exactly this reason; refuse and say how to fix it.
                return (
                    f"Error: old_text matches {occurrences} places in {path}; the "
                    "edit is ambiguous. Include enough surrounding context to "
                    "identify exactly one occurrence."
                )
            atomic_write_text(fp, content.replace(old_text, new_text, 1))
            return f"Edited {path}"
        except Exception as e:
            return f"Error: {e}"

    # -- async dispatch: route + offload blocking work off the event loop --
    def interrupt(self) -> int:
        """End every foreground shell still running; the count killed.

        Cancelling the turn only abandons `to_thread(run_bash, ...)` -- the
        worker thread and its subprocess keep going until `bash_timeout`.
        Measured: after `session.cancel()` a foreground `sleep` was still
        alive a second later, with 119 seconds of billed CPU left in it.
        OpenWorker's stop contract names both halves -- "interrupt the model
        stream *and* the foreground shell" -- and round 94 already held the
        background sibling to it; this is the same rule for the tool that
        started the turn. Kills the whole process group: ending only the
        shell would orphan whatever it had spawned.
        """

        with self._live_lock:
            live = [p for p in self._live if p.poll() is None]
        for process in live:
            _kill_group(process)
        return len(live)

    async def dispatch(self, name: str, args: dict) -> str:
        if name == "bash":
            return await asyncio.to_thread(self.run_bash, args["command"])
        if name == "read_file":
            return await asyncio.to_thread(
                self.run_read, args["path"], args.get("limit"), args.get("offset", 0)
            )
        if name == "write_file":
            return await asyncio.to_thread(self.run_write, args["path"], args["content"])
        if name == "edit_file":
            return await asyncio.to_thread(self.run_edit, args["path"], args["old_text"], args["new_text"])
        if name == "glob":
            return await asyncio.to_thread(self.run_glob, args["pattern"])
        return f"Unknown tool: {name}"

    def handles(self, name: str) -> bool:
        return name in ("bash", "read_file", "write_file", "edit_file", "glob")

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: output bounds are enforced inside each operation (capped, _BoundedCapture) where the data flows; there is no post-hoc state to measure."
)
