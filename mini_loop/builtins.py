"""The built-in tools, expressed as `Tool`s over a `ToolContext`.

These are just the first entries in the registry -- nothing about them is
special, and any can be removed (`registry.unregister`) or replaced
(`registry.register(..., replace=True)`).

  default_registry()  -> bash, read_file, write_file, edit_file, glob,
                         TodoWrite, task, load_skill, compress
  explore_registry()  -> read_file, glob               (read-only exploration subagents)
  worker_registry()   -> bash, read_file, write_file, edit_file, glob
"""

from __future__ import annotations

import asyncio

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

ASK_USER = {
    "name": "ask_user",
    "description": (
        "Ask the human operator one clarifying question and wait for their "
        "reply. Use sparingly -- prefer acting on the information you have. "
        "The reply may take a while; if nobody answers, you will be told so."
    ),
    "input_schema": {
        "type": "object",
        "properties": {"question": {"type": "string"}},
        "required": ["question"],
    },
}


# --- handlers (all receive ctx first) --------------------------------------

async def _bash(ctx: ToolContext, command: str, run_in_background: bool = False) -> str:
    if "background_run" in ctx.agent.tools:
        from .background import background_manager_for, should_run_background

        if should_run_background(command, run_in_background):
            return background_manager_for(ctx).run(command)
    return await asyncio.to_thread(ctx.agent.toolset.run_bash_result, command)


async def _read_file(
    ctx: ToolContext, path: str, limit: int | None = None, offset: int = 0
) -> str:
    return await ctx.agent.toolset.dispatch(
        "read_file", {"path": path, "limit": limit, "offset": offset}
    )


async def _write_file(ctx: ToolContext, path: str, content: str) -> str:
    return await ctx.agent.toolset.dispatch("write_file", {"path": path, "content": content})


async def _write_file_took_effect(ctx: ToolContext, call) -> bool | None:
    """Did this exact write already land?

    Answerable precisely because `write_file` is a typed call rather than an
    opaque shell string: the intended path and content are both in the request,
    so the file on disk settles it. `bash` has no equivalent -- which is the
    concrete argument for promoting a side effect out of it.
    """

    path = call.input.get("path")
    content = call.input.get("content")
    if not isinstance(path, str) or not isinstance(content, str):
        return None
    try:
        target = ctx.agent.toolset.safe_path(path)
    except ValueError:
        return None
    if not target.is_file():
        return False  # provably not applied: retrying is safe
    try:
        return target.read_text() == content
    except OSError:
        return None  # cannot tell -- stay unknown rather than guess


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


async def _ask_user(ctx: ToolContext, question: str) -> str:
    # The broker lives on the manager; a bare Agent (no manager in state) has
    # nobody to route the question to and says so instead of hanging.
    broker = getattr(ctx.state.get("manager"), "approvals", None)
    if broker is None:
        return "[ask_user unavailable on this surface: no approval broker]"
    answer = await broker.ask_question(ctx, question)
    if answer is None:
        return ("[no answer] The user declined or did not respond in time. "
                "Proceed on your best judgment and say what you assumed.")
    return f"The user answered: {answer}"


def _file_tools() -> list[Tool]:
    return [
        Tool(
            "bash",
            BASH["description"],
            BASH["input_schema"],
            _bash,
            readonly=False,
            risk="exec",
            capabilities=frozenset({"process.exec"}),
        ),
        Tool(
            "read_file",
            READ_FILE["description"],
            READ_FILE["input_schema"],
            _read_file,
            readonly=True,
            parallel_safe=True,
            risk="read",
            capabilities=frozenset({"repo.read"}),
        ),
        Tool(
            "write_file",
            WRITE_FILE["description"],
            WRITE_FILE["input_schema"],
            _write_file,
            verify=_write_file_took_effect,
            risk="write",
            capabilities=frozenset({"workspace.write"}),
        ),
        Tool(
            "edit_file",
            EDIT_FILE["description"],
            EDIT_FILE["input_schema"],
            _edit_file,
            risk="write",
            capabilities=frozenset({"workspace.write"}),
        ),
        Tool(
            "glob",
            GLOB["description"],
            GLOB["input_schema"],
            _glob,
            readonly=True,
            parallel_safe=True,
            risk="read",
            capabilities=frozenset({"repo.search"}),
        ),
    ]


def default_registry() -> ToolRegistry:
    reg = ToolRegistry(_file_tools())
    reg.register(Tool("TodoWrite", TODO_WRITE["description"], TODO_WRITE["input_schema"], _todo_write, risk="write"))
    reg.register(Tool("task", TASK["description"], TASK["input_schema"], _task, risk="exec"))
    # readonly=True keeps the advisory field in step with risk="read" -- round
    # 104 found these two the only built-ins where the two had drifted.
    reg.register(Tool("load_skill", LOAD_SKILL["description"], LOAD_SKILL["input_schema"], _load_skill, readonly=True, risk="read"))
    reg.register(Tool("compress", COMPRESS["description"], COMPRESS["input_schema"], _compress, risk="write"))
    # A question mutates nothing; a readonly session may still ask.
    reg.register(Tool("ask_user", ASK_USER["description"], ASK_USER["input_schema"], _ask_user, readonly=True, risk="read"))
    return reg


def explore_registry() -> ToolRegistry:
    # No bash: the Explore subagent runs in read-only permission mode (see
    # `Agent._run_subagent`), which denies exec-risk tools, so an offered bash
    # would be a tool the model is told it has and cannot ever call. read_file
    # and glob are read-risk and are all a read-only explorer needs.
    by_name = {t.name: t for t in _file_tools()}
    return ToolRegistry([by_name["read_file"], by_name["glob"]])


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
