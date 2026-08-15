"""Pluggable system-prompt assembly (s10).

The agent's system prompt is produced by a `system_builder(agent) -> str` before
every model call, so runtime facts never go stale after tools, memory, tasks, or
team identity change.

Pass your own builder to Agent/SessionManager, or pass an explicit `system=`
string to bypass building entirely.

**Stable vs volatile.** Providers render a request as ``tools -> system ->
messages`` and cache it as a *prefix*: one changed byte in ``system``
invalidates every cached token after it, for the whole conversation. So the
builder below emits only facts that hold still for the life of an agent.
Anything that changes turn to turn -- the TodoWrite board, the memory index --
is volatile, and lives in ``runtime_facts`` instead, which
``mini_loop/caching.py`` delivers through the *message stream*. Appending to the
end of the conversation invalidates nothing before it.

A custom builder that interpolates changing state back into the system prompt
is correct but uncacheable; that is a real trade, not a bug.
"""

from __future__ import annotations

#: Omitted tools named in the system prompt before collapsing to a count. The
#: notice must stay bounded -- an unbounded list of dropped names would rebuild
#: the very payload the budget removed, one channel over.
MAX_OMITTED_NAMED = 20


def _omitted_notice(agent) -> str:
    """Name the gap between the registry and the request, when there is one.

    The model's map of its own capabilities comes from this prompt; the
    request's `tools` param is what it can actually call. Round 93 found the
    two diverging silently: the builder listed every *registered* name while
    `schemas()` sent a budget-fitted subset, so the model was affirmatively
    told it had thousands of tools whose definitions it had never seen.
    Claude Code's deferred-tools reminder is the pattern followed here: what
    is absent from the request is said to be absent, not left implied.
    """

    catalog = getattr(agent, "_request_tool_catalog", None)
    omitted = list(
        catalog.omitted_names
        if catalog is not None
        else agent.tools.snapshot().omitted_names
    )
    if not omitted:
        return ""
    shown = ", ".join(omitted[:MAX_OMITTED_NAMED])
    more = len(omitted) - min(len(omitted), MAX_OMITTED_NAMED)
    tail = f", and {more} more" if more else ""
    return (
        f"{len(omitted)} registered tool(s) are NOT included in this request "
        "because the combined tool definitions exceed the per-request budget: "
        f"{shown}{tail}. Their definitions were not sent, so they cannot be "
        "called reliably; if one is needed, it is unavailable."
    )


def default_system_builder(agent) -> str:
    """Return only the parts that hold still for this agent's lifetime."""

    # The names the request will carry, not the registry inventory. These can
    # differ, and the prompt describing tools the request does not define is
    # an affirmative false claim -- worse than silence.
    catalog = getattr(agent, "_request_tool_catalog", None)
    sent_names = (
        catalog.sent_names
        if catalog is not None
        else agent.tools.snapshot().sent_names
    )
    tools = ", ".join(sent_names)
    parts = [
        f"You are a coding agent working in {agent.workspace}.\n"
        "Use the provided tools to act; prefer doing over explaining.\n"
        "Before calling tools, send one concise user-facing progress update; "
        "it is commentary, not the final answer. After the work is complete, "
        "send a concise final answer.\n"
        "For multi-step work, lay out a plan with TodoWrite and keep it updated.\n"
        "Delegate large side-quests to a subagent via `task` to keep your context clean.\n"
        "Pull in specialized knowledge with `load_skill` only when you need it.\n"
        f"Tools available: {tools}\n"
        f"Skills available:\n{agent.skills.descriptions()}"
    ]
    notice = _omitted_notice(agent)
    if notice:
        parts.append(notice)
    # Confinement is a stable fact for this agent's lifetime, so it belongs
    # here rather than in the runtime facts -- unlike the memory index.
    #
    # Asked directly, a real agent said: "the description alone doesn't state
    # any sandboxing... I cannot confirm actual confinement without testing."
    # An agent that does not know it is confined discovers the boundary as an
    # opaque `Operation not permitted` from /bin/sh, and its reasonable next
    # move is to work around what looks like a broken tool.
    sandbox = getattr(agent, "sandbox", None)
    if getattr(sandbox, "confined", False):
        parts.append(
            f"Shell commands are confined: {sandbox.describe}.\n"
            "Work inside the workspace. A permission error outside it is that "
            "boundary, not a broken tool, and retrying or escalating will not "
            "get past it."
        )

    if agent.state.get("agent_name"):
        # Team identity is assigned once at construction, so it is stable.
        parts.append(
            f"Team identity: {agent.state['agent_name']}"
            f" (role: {agent.state.get('role', 'lead')}, team: {agent.state.get('team_id', '-')})"
        )

    if agent.state.get("plan_mode"):
        # Soft guidance only: what the model is TOLD, never what it is
        # ALLOWED -- sandbox and permission policy enforce independently
        # and neither reads plan state (plan_mode.py).
        from .plan_mode import PLAN_SECTION

        parts.append(PLAN_SECTION)
    return "\n\n".join(parts)


#: Bands at which the agent is told, and what it can do about it. Coarse on
#: purpose: the value has to hold still across turns or it re-injects.
CONTEXT_BANDS = ((0.90, "over 90%"), (0.75, "over 75%"))


def _context_pressure(agent) -> str:
    """How full the context is, when that should change what the agent does.

    An agent at 89% of its budget has a `compress` tool, no idea it is at 89%,
    and compaction about to happen *to* it -- old tool results blanked to
    `[cleared]`, the middle of the conversation replaced by a snip marker --
    with nothing having warned it. One `cat` of a large file at that point costs
    it context it was relying on.
    """

    from .compaction import context_used

    threshold = getattr(agent.settings, "token_threshold", 0) or 0
    if threshold <= 0:
        return ""
    used = context_used(agent)
    for fraction, label in CONTEXT_BANDS:
        if used >= threshold * fraction:
            # States what is happening, and does not prescribe how to work.
            # The first version added "prefer summarising over pasting, read
            # files in slices" and was measured against the real endpoint: on a
            # task inviting a large read it produced *six* bash calls where an
            # untold agent used three, and neither dumped the file. The model
            # already sliced sensibly; being told to slice made it slice more.
            #
            # What is left is the part it cannot know and that happens to it:
            # its own output is about to be rewritten.
            return (
                f"Context is {label} full; older tool output may be cleared "
                "automatically to make room. The `compress` tool frees space "
                "deliberately."
            )
    return ""


def runtime_facts(agent) -> str:
    """Return the turn-to-turn state, or ``""`` when there is none.

    This is everything that used to sit in the system prompt and changed as the
    agent worked. It is delivered as a message-stream reminder so the cached
    prefix survives; see ``mini_loop/caching.py``.
    """

    parts = []
    if agent.todo.items:
        parts.append(f"Current TodoWrite state:\n{agent.todo.render()}")
    # Bucketed, not a live percentage. `runtime_facts_injector` re-sends only
    # when this string changes, so an exact figure would inject a message every
    # turn -- the churn the whole runtime-facts design exists to avoid.
    #
    # Reported only when it is actionable. Below the first band there is nothing
    # for the agent to do differently, and a line that is always there is a line
    # that stops being read.
    pressure = _context_pressure(agent)
    if pressure:
        parts.append(pressure)

    # Through the seam, not the raw store. Reading `state["memory"]` directly
    # bypasses the owner binding, which is how the index kept leaking after
    # `recall` was already scoped -- one call site around the outside is all it
    # takes.
    from .memory import memory_store_for

    memory = memory_store_for(agent) if agent.state.get("memory") is not None else None
    # `index()` renders a placeholder line for an empty store, so ask the store
    # whether it actually holds anything rather than trusting a truthy string.
    #
    # And only when the tools exist. Setting `memory_root` builds a store
    # without registering `remember`/`recall`, and this injected the index
    # anyway -- so the agent was shown a catalogue of memories it had no tool to
    # open, every turn, at whatever the index costs. Caught by reading what a
    # real model said about it: "I don't actually have a dedicated memory tool
    # in my available toolset."
    if memory is not None and memory.list() and "recall" in agent.tools:
        parts.append(f"Known memories (use `recall` for full text):\n{memory.index()}")
    return "\n\n".join(parts)


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

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: the builder is a pure function of agent state re-run per request; nothing persists between calls to drift."
)
