"""Prompt-cache tests -- offline, deterministic, no API key.

Two properties matter and neither shows up as a crash when it breaks:

* the cached prefix (tools + system) must stay byte-identical across turns, and
* every request must leave a breakpoint within the provider's lookback window,
  even when a round emits a large parallel tool batch.

Both are asserted directly rather than inferred from a passing request.
"""

import asyncio
import json
from pathlib import Path

from mini_loop.agent import Agent
from mini_loop.caching import (
    BREAKPOINT_STRIDE,
    LOOKBACK_BLOCKS,
    DefaultCachePolicy,
    NullCachePolicy,
    runtime_facts_injector,
)
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, system_text, text, tool
from mini_loop.prompts import default_system_builder, runtime_facts
from mini_loop.registry import Hooks, Tool, ToolRegistry
from mini_loop.skills import SkillLoader

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path, **over) -> Settings:
    base = dict(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR)
    base.update(over)
    return Settings(**base)


def _agent(tmp_path, client, *, tools=None, hooks=None, **over):
    settings = _settings(tmp_path, **over)
    ws = settings.workspace_root / "sess"
    ws.mkdir(parents=True, exist_ok=True)
    return Agent(
        client=client,
        settings=settings,
        workspace=ws,
        skills=SkillLoader(SKILLS_DIR),
        tools=tools,
        hooks=hooks,
    )


def _user(*blocks):
    return {"role": "user", "content": [dict(b) for b in blocks]}


def _tool_result(n: int) -> dict:
    return {"type": "tool_result", "tool_use_id": f"toolu_{n}", "content": f"out {n}"}


def _marked(messages) -> list[int]:
    """Indices of messages carrying at least one breakpoint."""
    out = []
    for i, m in enumerate(messages):
        content = m.get("content")
        if isinstance(content, list) and any(
            isinstance(b, dict) and "cache_control" in b for b in content
        ):
            out.append(i)
    return out


def _flat_blocks(messages) -> list[bool]:
    """Flatten every message into blocks; True where a breakpoint sits."""
    flat = []
    for m in messages:
        content = m.get("content")
        if isinstance(content, list):
            for b in content:
                flat.append(isinstance(b, dict) and "cache_control" in b)
        else:
            flat.append(False)
    return flat


def _blocks_back_to_nearest_breakpoint(messages) -> int:
    flat = _flat_blocks(messages)
    for distance, marked in enumerate(reversed(flat)):
        if marked:
            return distance
    return len(flat)


# --- policy placement ------------------------------------------------------

def test_system_string_becomes_a_cached_block():
    policy = DefaultCachePolicy()
    system, tools, _ = policy.annotate(system="core rules", tools=[{"name": "x"}], messages=[])
    assert system == [
        {"type": "text", "text": "core rules", "cache_control": {"type": "ephemeral"}}
    ]
    # Tools render before system, so they ride the same prefix untouched.
    assert tools == [{"name": "x"}]


def test_empty_system_is_left_alone():
    policy = DefaultCachePolicy()
    system, _, _ = policy.annotate(system="", tools=None, messages=[])
    assert system == ""


def test_ttl_is_forwarded():
    policy = DefaultCachePolicy(ttl="1h")
    system, _, _ = policy.annotate(system="core", tools=None, messages=[])
    assert system[0]["cache_control"] == {"type": "ephemeral", "ttl": "1h"}


def test_newest_turn_is_always_marked():
    policy = DefaultCachePolicy()
    messages = [_user({"type": "text", "text": "hi"})]
    _, _, out = policy.annotate(system="core", tools=None, messages=messages)
    assert _marked(out) == [0]


def test_annotation_does_not_mutate_the_caller_history():
    """`agent.messages` is durable history; provider keys must not leak into it."""
    policy = DefaultCachePolicy()
    messages = [_user({"type": "text", "text": "hi"})]
    original = json.dumps(messages, sort_keys=True)
    policy.annotate(system="core", tools=None, messages=messages)
    assert json.dumps(messages, sort_keys=True) == original


def test_assistant_turns_are_never_annotated():
    """Assistant content is provider objects round-tripped verbatim."""
    policy = DefaultCachePolicy()
    messages = [
        _user({"type": "text", "text": "hi"}),
        {"role": "assistant", "content": [object()]},
    ]
    _, _, out = policy.annotate(system="core", tools=None, messages=messages)
    assert _marked(out) == [0]


def test_breakpoints_stay_inside_the_lookback_window():
    """The regression that matters: a parallel batch must not orphan the cache.

    One round here adds a 12-block assistant turn plus a 12-block tool-result
    turn. With only the newest turn marked, the next request's breakpoint would
    look back past the 20-block window and silently miss.
    """
    policy = DefaultCachePolicy()
    messages = []
    for round_index in range(4):
        messages.append({"role": "assistant", "content": [object()] * 12})
        messages.append(_user(*[_tool_result(i) for i in range(12)]))

    _, _, out = policy.annotate(system="core", tools=None, messages=messages)
    assert _marked(out), "no breakpoint placed at all"

    # The guarantee: the newest breakpoint is inside the window, so the request
    # currently being built always writes an entry the next one can read.
    distance = _blocks_back_to_nearest_breakpoint(out)
    assert distance <= LOOKBACK_BLOCKS, f"nearest breakpoint {distance} blocks back"

    # Per-block placement must beat per-message placement: marking only the
    # last block of each turn is what silently orphaned the cache.
    flat = _flat_blocks(out)
    assert sum(flat) > 1, "expected multiple breakpoints in a batch-heavy history"


def test_wide_batches_exceed_what_the_budget_can_chain():
    """Pin the known limit so a future change has to acknowledge it.

    An assistant turn's `tool_use` blocks are provider objects and cannot carry
    a breakpoint, so a round of N parallel tools contributes N unmarkable
    blocks. Past a point no budget of 4 can keep every gap inside the window --
    the newest entry is still written, which is what keeps this a degradation
    rather than a break.
    """
    policy = DefaultCachePolicy()
    messages = []
    for _ in range(4):
        messages.append({"role": "assistant", "content": [object()] * 30})
        messages.append(_user(*[_tool_result(i) for i in range(30)]))

    _, _, out = policy.annotate(system="core", tools=None, messages=messages)
    assert _blocks_back_to_nearest_breakpoint(out) <= LOOKBACK_BLOCKS

    flat = _flat_blocks(out)
    gaps = [i for i, marked in enumerate(flat) if marked]
    widest = max(
        (later - earlier - 1 for earlier, later in zip(gaps, gaps[1:])),
        default=0,
    )
    assert widest > LOOKBACK_BLOCKS, (
        "a 30-wide batch should out-run the budget; if this now passes, the "
        "placement improved and the documented limit needs updating"
    )


def test_breakpoint_budget_is_respected():
    policy = DefaultCachePolicy()
    messages = []
    for _ in range(40):
        messages.append(_user(*[_tool_result(i) for i in range(BREAKPOINT_STRIDE + 1)]))
    _, _, out = policy.annotate(system="core", tools=None, messages=messages)
    # One breakpoint is spent on tools+system; the rest go to the conversation.
    assert len(_marked(out)) <= 3


def test_null_policy_changes_nothing():
    messages = [_user({"type": "text", "text": "hi"})]
    system, tools, out = NullCachePolicy().annotate(
        system="core", tools=[{"name": "x"}], messages=messages
    )
    assert system == "core" and tools == [{"name": "x"}] and out is messages


def test_stride_must_fit_the_lookback_window():
    for bad in (0, LOOKBACK_BLOCKS + 1):
        try:
            DefaultCachePolicy(stride=bad)
        except ValueError:
            continue
        raise AssertionError(f"stride={bad} should have been rejected")


# --- the prefix-stability property -----------------------------------------

def test_todo_state_is_not_in_the_system_prompt(tmp_path):
    """The invalidator this whole module exists to remove."""
    agent = _agent(tmp_path, FakeAsyncAnthropic())
    before = default_system_builder(agent)
    agent.todo.update([
        {"content": "ship it", "status": "in_progress", "activeForm": "shipping"}
    ])
    assert default_system_builder(agent) == before
    assert "ship it" in runtime_facts(agent)


def test_system_prompt_is_byte_stable_across_a_real_turn(tmp_path):
    """Capture every system payload the client sees and assert they match."""
    seen = []

    def responder(kwargs: dict):
        if not kwargs.get("tools"):
            return [text("[summary]")], "end_turn"
        seen.append(system_text(kwargs))
        if len(seen) == 1:
            return (
                [tool("TodoWrite", _id="toolu_1", todos=[
                    {"content": "step one", "status": "in_progress", "activeForm": "doing"}
                ])],
                "tool_use",
            )
        return [text("done")], "end_turn"

    agent = _agent(tmp_path, FakeAsyncAnthropic(responder=responder))
    asyncio.run(agent.run("go"))

    assert len(seen) >= 2
    assert len(set(seen)) == 1, "system prompt changed mid-conversation"
    # The todo state still reached the model -- just through the message stream.
    stream = json.dumps(agent.messages, default=str)
    assert "step one" in stream


def test_runtime_facts_are_sent_once_until_they_change(tmp_path):
    agent = _agent(tmp_path, FakeAsyncAnthropic())

    assert asyncio.run(runtime_facts_injector(agent)) is None  # nothing yet

    agent.todo.update([
        {"content": "a", "status": "pending", "activeForm": "doing a"}
    ])
    first = asyncio.run(runtime_facts_injector(agent))
    assert first and "a" in first[0]["content"]
    # Existing injectors all use plain string content; the fake client and the
    # loop both branch on that shape.
    assert isinstance(first[0]["content"], str)

    assert asyncio.run(runtime_facts_injector(agent)) is None  # unchanged

    agent.todo.update([
        {"content": "a", "status": "completed", "activeForm": "doing a"},
        {"content": "b", "status": "in_progress", "activeForm": "doing b"},
    ])
    assert asyncio.run(runtime_facts_injector(agent)) is not None


def test_empty_memory_store_does_not_emit_runtime_facts(tmp_path):
    """An empty store still renders a placeholder line -- it must not count."""
    from mini_loop.memory import MemoryStore

    agent = _agent(tmp_path, FakeAsyncAnthropic())
    agent.state["memory"] = MemoryStore(tmp_path / "mem")
    assert runtime_facts(agent) == ""

    agent.state["memory"].write("pref", "user", "indent", "Use tabs.")
    # The index rides in runtime facts only when the agent can act on it --
    # `memory_root` alone builds a store without registering `remember`/`recall`.
    from mini_loop.builtins import full_registry

    agent.tools = full_registry()
    assert "pref" in runtime_facts(agent)


def test_policy_reaches_the_wire(tmp_path):
    """End to end: the request the client receives carries breakpoints."""
    payloads = []

    async def echo(_ctx, value="x"):
        return "ok"

    registry = ToolRegistry()
    registry.register(
        Tool("echo", "echo", {"type": "object", "properties": {"value": {"type": "string"}}}, echo)
    )

    def responder(kwargs: dict):
        if not kwargs.get("tools"):
            return [text("[summary]")], "end_turn"
        payloads.append(kwargs)
        if len(payloads) == 1:
            return [tool("echo", _id="toolu_1", value="x")], "tool_use"
        return [text("done")], "end_turn"

    agent = _agent(
        tmp_path, FakeAsyncAnthropic(responder=responder), tools=registry, hooks=Hooks()
    )
    asyncio.run(agent.run("go"))

    assert len(payloads) >= 2
    for kwargs in payloads:
        assert isinstance(kwargs["system"], list)
        assert kwargs["system"][-1]["cache_control"] == {"type": "ephemeral"}
    # The tool-result turn carries the rolling conversation breakpoint.
    assert _marked(payloads[-1]["messages"])
