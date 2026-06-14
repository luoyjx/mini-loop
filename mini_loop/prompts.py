"""Pluggable system-prompt assembly (s10).

The agent's system prompt is produced by a `system_builder(agent) -> str` at
construction time, so you can compose it from runtime facts (workspace, the
actual registered tools, loaded skills) instead of hardcoding a string.

Pass your own builder to Agent/SessionManager, or pass an explicit `system=`
string to bypass building entirely.
"""

from __future__ import annotations


def default_system_builder(agent) -> str:
    tools = ", ".join(agent.tools.names())
    return (
        f"You are a coding agent working in {agent.workspace}.\n"
        "Use the provided tools to act; prefer doing over explaining.\n"
        "For multi-step work, lay out a plan with TodoWrite and keep it updated.\n"
        "Delegate large side-quests to a subagent via `task` to keep your context clean.\n"
        "Pull in specialized knowledge with `load_skill` only when you need it.\n"
        f"Tools available: {tools}\n"
        f"Skills available:\n{agent.skills.descriptions()}"
    )


def sections_builder(*sections):
    """Build a system_builder from static strings and/or `f(agent) -> str`
    callables, concatenated with blank lines. Handy for layering org-wide
    policy + per-product instructions.

        build = sections_builder(BASE_RULES, lambda a: f"Workspace: {a.workspace}")
    """
    def build(agent) -> str:
        parts = []
        for s in sections:
            parts.append(s(agent) if callable(s) else str(s))
        return "\n\n".join(p for p in parts if p)
    return build
