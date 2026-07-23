"""The built-in tools, expressed as `Tool`s over a `ToolContext`.

These are just the first entries in the registry -- nothing about them is
special, and any can be removed (`registry.unregister`) or replaced
(`registry.register(..., replace=True)`).

  default_registry()  -> bash, read_file, write_file, edit_file, glob,
                         TodoWrite, task, load_skill, compress
  explore_registry()  -> bash, read_file, glob         (exploration subagents)
  worker_registry()   -> bash, read_file, write_file, edit_file, glob
"""

from __future__ import annotations

from .registry import Tool, ToolContext, ToolRegistry
from .tools import BASH, EDIT_FILE, GLOB, READ_FILE, WRITE_FILE

# --- agent-level tool schemas (file-tool schemas live in tools.py) ---------

TODO_WRITE = {
    "name": "TodoWrite",
    "description": "Create/replace the task checklist. Use for multi-step work.",
    "input_schema": {
        "type": "object",
        "properties": {
            "items": {
                "type": "array",
                "items": {
                    "type": "object",
                    "properties": {
                        "content": {"type": "string"},
                        "status": {"type": "string", "enum": ["pending", "in_progress", "completed"]},
                        "activeForm": {"type": "string"},
                    },
                    "required": ["content", "status", "activeForm"],
                },
            }
        },
        "required": ["items"],
    },
}
TASK = {
    "name": "task",
    "description": "Delegate isolated work to a subagent with a fresh context. "
                   "'Explore' is read-only; 'general-purpose' may also edit files. "
                   "Returns only the subagent's final summary.",
    "input_schema": {
        "type": "object",
        "properties": {
            "prompt": {"type": "string"},
            "agent_type": {"type": "string", "enum": ["Explore", "general-purpose"]},
        },
        "required": ["prompt"],
    },
}
LOAD_SKILL = {
    "name": "load_skill",
    "description": "Load a named skill's full instructions into context.",
    "input_schema": {"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]},
}
COMPRESS = {
    "name": "compress",
    "description": "Manually compress the conversation to free context.",
    "input_schema": {"type": "object", "properties": {}},
}


# --- handlers (all receive ctx first) --------------------------------------

async def _bash(ctx: ToolContext, command: str, run_in_background: bool = False) -> str:
    if "background_run" in ctx.agent.tools:
        from .background import background_manager_for, should_run_background

        if should_run_background(command, run_in_background):
            return background_manager_for(ctx).run(command)
    return await ctx.agent.toolset.dispatch("bash", {"command": command})


async def _read_file(
    ctx: ToolContext, path: str, limit: int | None = None, offset: int = 0
) -> str:
    return await ctx.agent.toolset.dispatch(
        "read_file", {"path": path, "limit": limit, "offset": offset}
    )


async def _write_file(ctx: ToolContext, path: str, content: str) -> str:
    return await ctx.agent.toolset.dispatch("write_file", {"path": path, "content": content})


async def _edit_file(ctx: ToolContext, path: str, old_text: str, new_text: str) -> str:
    return await ctx.agent.toolset.dispatch(
        "edit_file", {"path": path, "old_text": old_text, "new_text": new_text}
    )


async def _glob(ctx: ToolContext, pattern: str) -> str:
    return await ctx.agent.toolset.dispatch("glob", {"pattern": pattern})


async def _todo_write(ctx: ToolContext, items: list) -> str:
    render = ctx.agent.todo.update(items)
    await ctx.emit_event("todo", items=ctx.agent.todo.snapshot())
    return render


async def _task(ctx: ToolContext, prompt: str, agent_type: str = "Explore") -> str:
    return await ctx.agent._run_subagent(prompt, agent_type)


def _load_skill(ctx: ToolContext, name: str) -> str:
    return ctx.agent.skills.load(name)


def _compress(ctx: ToolContext) -> str:
    ctx.agent._pending_compact = True
    return "Compressing conversation..."


def _file_tools() -> list[Tool]:
    return [
        Tool("bash", BASH["description"], BASH["input_schema"], _bash, readonly=False),
        Tool(
            "read_file",
            READ_FILE["description"],
            READ_FILE["input_schema"],
            _read_file,
            readonly=True,
            parallel_safe=True,
        ),
        Tool("write_file", WRITE_FILE["description"], WRITE_FILE["input_schema"], _write_file),
        Tool("edit_file", EDIT_FILE["description"], EDIT_FILE["input_schema"], _edit_file),
        Tool(
            "glob",
            GLOB["description"],
            GLOB["input_schema"],
            _glob,
            readonly=True,
            parallel_safe=True,
        ),
    ]


def default_registry() -> ToolRegistry:
    reg = ToolRegistry(_file_tools())
    reg.register(Tool("TodoWrite", TODO_WRITE["description"], TODO_WRITE["input_schema"], _todo_write))
    reg.register(Tool("task", TASK["description"], TASK["input_schema"], _task))
    reg.register(Tool("load_skill", LOAD_SKILL["description"], LOAD_SKILL["input_schema"], _load_skill))
    reg.register(Tool("compress", COMPRESS["description"], COMPRESS["input_schema"], _compress))
    return reg


def explore_registry() -> ToolRegistry:
    by_name = {t.name: t for t in _file_tools()}
    return ToolRegistry([by_name["bash"], by_name["read_file"], by_name["glob"]])


def worker_registry() -> ToolRegistry:
    return ToolRegistry(_file_tools())


def full_registry(
    *,
    tasks: bool = True,
    background: bool = True,
    memory: bool = True,
    cron: bool = True,
    teams: bool = True,
    worktrees: bool = True,
    mcp: bool = True,
    mcp_servers: dict | None = None,
) -> ToolRegistry:
    """Comprehensive s20 registry; toggle individual feature groups as needed."""
    from .background import install_background
    from .cron import install_cron
    from .mcp import install_mcp
    from .memory import install_memory
    from .tasks import install_tasks
    from .teams import install_teams
    from .worktrees import install_worktrees

    reg = default_registry()
    if tasks:
        install_tasks(reg)
    if background:
        install_background(reg)
    if memory:
        install_memory(reg)
    if cron:
        install_cron(reg)
    if teams:
        install_teams(reg)
    if worktrees:
        install_worktrees(reg)
    if mcp:
        install_mcp(reg, mcp_servers or {})
    return reg


def default_injectors(*, background: bool = True, teams: bool = True) -> list:
    """Loop injectors paired with the comprehensive registry."""
    from .background import background_injector
    from .teams import team_injector

    injectors = []
    if background:
        injectors.append(background_injector)
    if teams:
        injectors.append(team_injector)
    return injectors
