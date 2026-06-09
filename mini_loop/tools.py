"""Per-workspace tools and the tool schemas the model sees.

This is the s02 dispatch pattern, with two changes for multi-tenancy:

  * every filesystem/shell tool is bound to a *single session's* workspace via
    `safe_path`, so concurrent agents can't read or clobber each other;
  * the blocking calls (subprocess, file I/O) are wrapped in `asyncio.to_thread`
    so one agent's `bash` never stalls the event loop the others share.

The schemas live here as plain dicts so both the main agent and its subagents
can compose tool subsets from one source of truth.
"""

from __future__ import annotations

import asyncio
import subprocess
from pathlib import Path

# --- Tool schemas (the contract the model reasons over) --------------------

BASH = {
    "name": "bash",
    "description": "Run a shell command in the workspace. Returns combined stdout+stderr.",
    "input_schema": {
        "type": "object",
        "properties": {"command": {"type": "string"}},
        "required": ["command"],
    },
}
READ_FILE = {
    "name": "read_file",
    "description": "Read a file's contents (workspace-relative path).",
    "input_schema": {
        "type": "object",
        "properties": {"path": {"type": "string"}, "limit": {"type": "integer"}},
        "required": ["path"],
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
FILE_TOOLS = [BASH, READ_FILE, WRITE_FILE, EDIT_FILE]
READONLY_TOOLS = [BASH, READ_FILE]

OUTPUT_CAP = 50_000
DANGEROUS = ("rm -rf /", "sudo", "shutdown", "reboot", "> /dev/", ":(){", "mkfs")


class Toolset:
    """The four base tools, sandboxed to one workspace directory."""

    def __init__(self, workspace: Path, *, bash_timeout: int = 120) -> None:
        self.workspace = workspace.resolve()
        self.workspace.mkdir(parents=True, exist_ok=True)
        self.bash_timeout = bash_timeout

    # -- path safety: nothing may escape the session's workspace --
    def safe_path(self, p: str) -> Path:
        path = (self.workspace / p).resolve()
        if path != self.workspace and not path.is_relative_to(self.workspace):
            raise ValueError(f"Path escapes workspace: {p}")
        return path

    # -- blocking primitives (run via to_thread in `dispatch`) --
    def run_bash(self, command: str) -> str:
        if any(d in command for d in DANGEROUS):
            return "Error: Dangerous command blocked"
        try:
            r = subprocess.run(
                command, shell=True, cwd=self.workspace,
                capture_output=True, text=True,
                encoding="utf-8", errors="replace", timeout=self.bash_timeout,
            )
            out = (r.stdout + r.stderr).strip()
            return out[:OUTPUT_CAP] if out else "(no output)"
        except subprocess.TimeoutExpired:
            return f"Error: Timeout ({self.bash_timeout}s)"
        except (FileNotFoundError, OSError) as e:
            return f"Error: {e}"

    def run_read(self, path: str, limit: int | None = None) -> str:
        try:
            lines = self.safe_path(path).read_text().splitlines()
            if limit and limit < len(lines):
                lines = lines[:limit] + [f"... ({len(lines) - limit} more lines)"]
            return "\n".join(lines)[:OUTPUT_CAP]
        except Exception as e:
            return f"Error: {e}"

    def run_write(self, path: str, content: str) -> str:
        try:
            fp = self.safe_path(path)
            fp.parent.mkdir(parents=True, exist_ok=True)
            fp.write_text(content)
            return f"Wrote {len(content)} bytes to {path}"
        except Exception as e:
            return f"Error: {e}"

    def run_edit(self, path: str, old_text: str, new_text: str) -> str:
        try:
            fp = self.safe_path(path)
            content = fp.read_text()
            if old_text not in content:
                return f"Error: Text not found in {path}"
            fp.write_text(content.replace(old_text, new_text, 1))
            return f"Edited {path}"
        except Exception as e:
            return f"Error: {e}"

    # -- async dispatch: route + offload blocking work off the event loop --
    async def dispatch(self, name: str, args: dict) -> str:
        if name == "bash":
            return await asyncio.to_thread(self.run_bash, args["command"])
        if name == "read_file":
            return await asyncio.to_thread(self.run_read, args["path"], args.get("limit"))
        if name == "write_file":
            return await asyncio.to_thread(self.run_write, args["path"], args["content"])
        if name == "edit_file":
            return await asyncio.to_thread(self.run_edit, args["path"], args["old_text"], args["new_text"])
        return f"Unknown tool: {name}"

    def handles(self, name: str) -> bool:
        return name in ("bash", "read_file", "write_file", "edit_file")
