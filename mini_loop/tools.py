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
import subprocess
from pathlib import Path

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
DANGEROUS = ("rm -rf /", "sudo", "shutdown", "reboot", "> /dev/", ":(){", "mkfs", "dd if=")


class Toolset:
    """The five base tools, sandboxed to one workspace directory."""

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

    def run_read(self, path: str, limit: int | None = None, offset: int = 0) -> str:
        try:
            lines = self.safe_path(path).read_text().splitlines()
            offset = max(int(offset or 0), 0)
            lines = lines[offset:]
            limit = max(int(limit), 0) if limit is not None else None
            if limit is not None and limit < len(lines):
                lines = lines[:limit] + [f"... ({len(lines) - limit} more lines)"]
            return "\n".join(lines)[:OUTPUT_CAP]
        except Exception as e:
            return f"Error: {e}"

    def run_glob(self, pattern: str) -> str:
        try:
            matches = []
            total = 0
            for match in globlib.iglob(pattern, root_dir=self.workspace, recursive=True):
                resolved = (self.workspace / match).resolve()
                if resolved == self.workspace or resolved.is_relative_to(self.workspace):
                    matches.append(match)
                    total += len(match) + 1
                    if total >= OUTPUT_CAP:
                        matches.append("... (matches truncated)")
                        break
            return "\n".join(sorted(set(matches)))[:OUTPUT_CAP] if matches else "(no matches)"
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
