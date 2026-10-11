"""Loop-detection tests -- offline, deterministic, no API key.

The detection rules mirror the OpenHands SDK ``StuckDetector``; these tests
pin both the pure policy (unit level) and its integration with the batched
agent loop (end to end against the fake client).
"""

import asyncio
from pathlib import Path

from mini_loop.agent import Agent
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
from mini_loop.registry import Hooks, Tool, ToolRegistry
from mini_loop.skills import SkillLoader
from mini_loop.stuck import (
    DefaultStuckDetector,
    NullStuckDetector,
    StuckThresholds,
    ToolStep,
    step_hash,
)

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path, **over) -> Settings:
    base = dict(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR)
    base.update(over)
    return Settings(**base)


def _agent(tmp_path, client, *, tools=None, hooks=None, stuck_detector=None, **over):
    settings = _settings(tmp_path, **over)
    ws = settings.workspace_root / "sess"
    ws.mkdir(parents=True, exist_ok=True)
    events = []

    async def emit(event):
        events.append(event)

    agent = Agent(
        client=client,
        settings=settings,
        workspace=ws,
        skills=SkillLoader(SKILLS_DIR),
        tools=tools,
        hooks=hooks,
        stuck_detector=stuck_detector,
        emit=emit,
    )
    return agent, events


class _Probe:
    """Minimal stand-in for the agent surface the detector reads."""

    def __init__(self, steps=(), rounds_without_tools=0):
        self.recent_steps = tuple(steps)
        self.rounds_without_tools = rounds_without_tools


def _step(name="read_file", inp="a", out="x", **flags) -> ToolStep:
    return ToolStep(
        name=name,
        input_hash=step_hash(inp),
        output_hash=step_hash(out),
        **flags,
    )


# --- the policy, in isolation ---------------------------------------------

def test_identical_call_and_result_fires_at_threshold():
    detector = DefaultStuckDetector()
    steps = [_step() for _ in range(3)]
    assert detector.inspect(_Probe(steps)) is None

    steps.append(_step())
    signal = detector.inspect(_Probe(steps))
    assert signal is not None
    assert signal.pattern == "repeat_action_result"
    assert signal.tool_name == "read_file"


def test_same_input_but_changing_output_is_not_stuck():
    detector = DefaultStuckDetector()
    steps = [_step(out=f"page-{i}") for i in range(6)]
    assert detector.inspect(_Probe(steps)) is None


def test_repeated_denial_fires_earlier_than_repeated_success():
    detector = DefaultStuckDetector()
    # Three denials trip the error rule even though the success rule needs four.
    steps = [_step(name="bash", out="denied", denied=True) for _ in range(3)]
    signal = detector.inspect(_Probe(steps))
    assert signal is not None
    assert signal.pattern == "repeat_action_error"
    assert "denied" in signal.detail


def test_repeated_failure_with_differing_error_text_still_fires():
    """Same call, same failure mode -- distinct messages must not hide it."""
    detector = DefaultStuckDetector()
    steps = [_step(name="bash", out=f"Error: attempt {i}", failed=True) for i in range(3)]
    signal = detector.inspect(_Probe(steps))
    assert signal is not None
    assert signal.pattern == "repeat_action_error"
    assert "failed" in signal.detail


def test_unproductive_tool_survives_varied_inputs():
    """The repeat rules need identical inputs; this one must not."""
    detector = DefaultStuckDetector()
    steps = [_step(name="api", inp=f"query-{i}", out=f"Error: {i}", failed=True) for i in range(5)]
    signal = detector.inspect(_Probe(steps))
    assert signal is not None
    assert signal.pattern == "unproductive_tool"
    assert signal.tool_name == "api"


def test_unproductive_tool_ignores_a_tool_that_sometimes_works():
    """A flaky tool is not a stuck agent."""
    detector = DefaultStuckDetector()
    steps = [_step(name="api", inp=f"q{i}", out=f"Error: {i}", failed=True) for i in range(5)]
    steps.insert(2, _step(name="api", inp="q-good", out="fine"))
    assert detector.inspect(_Probe(steps)) is None


def test_measured_denial_ping_pong_is_caught():
    """Regression for a real traced run against a live model.

    The model emitted a denied call, then settled into a stable
    denied/succeeding ping-pong: every failure was followed by a *successful*
    workaround call. A Cline-style reset-on-success mistake counter never
    accumulates here, and the repeat rules cannot see it either.
    """
    trace = [_step(name="Workflow", inp="launch", out="ok")]
    for i in range(3):
        trace.append(_step(name="Status", inp="run-1", out="Error: denied", failed=True))
    for i in range(6):
        trace.append(_step(name="bash", inp=f"ls -{i}", out=f"listing {i}"))
        trace.append(_step(name="Status", inp=f"run-1-{i}", out=f"Error: denied {i}", failed=True))

    # The consecutive-unproductive run never exceeds 3 anywhere after the head.
    tail = trace[4:]
    longest = best = 0
    for step in tail:
        best = best + 1 if step.unproductive else 0
        longest = max(longest, best)
    assert longest == 1, "the trace must not contain a consecutive failure run"

    signal = DefaultStuckDetector().inspect(_Probe(tail))
    assert signal is not None
    assert signal.pattern == "unproductive_tool"
    assert signal.tool_name == "Status"
    assert "never once succeeded" in signal.detail


def test_unproductive_tool_is_disableable():
    detector = DefaultStuckDetector(
        StuckThresholds(unproductive_tool=0, repeat_action_error=99)
    )
    steps = [_step(name="api", inp=f"q{i}", out=f"Error: {i}", failed=True) for i in range(9)]
    assert detector.inspect(_Probe(steps)) is None


def test_alternating_pair_fires_but_a_uniform_run_does_not_report_as_alternating():
    detector = DefaultStuckDetector()
    pair = [_step(name="glob", inp="*.py", out="one"), _step(name="read_file", inp="a", out="two")]
    signal = detector.inspect(_Probe(pair * 3))
    assert signal is not None
    assert signal.pattern == "alternating"

    uniform = detector.inspect(_Probe([_step() for _ in range(6)]))
    assert uniform is not None
    assert uniform.pattern == "repeat_action_result"


def test_alternating_needs_a_stable_cycle():
    detector = DefaultStuckDetector()
    noisy = [
        _step(name="glob", out="one"),
        _step(name="read_file", out="two"),
        _step(name="glob", out="one"),
        _step(name="read_file", out="CHANGED"),
        _step(name="glob", out="one"),
        _step(name="read_file", out="two"),
    ]
    assert detector.inspect(_Probe(noisy)) is None


def test_monologue_counts_toolless_rounds():
    detector = DefaultStuckDetector()
    assert detector.inspect(_Probe((), rounds_without_tools=2)) is None
    signal = detector.inspect(_Probe((), rounds_without_tools=3))
    assert signal is not None
    assert signal.pattern == "monologue"


def test_thresholds_are_configurable():
    detector = DefaultStuckDetector(StuckThresholds(repeat_action_result=2))
    assert detector.inspect(_Probe([_step(), _step()])) is not None


def test_null_detector_never_fires():
    detector = NullStuckDetector()
    assert detector.inspect(_Probe([_step() for _ in range(10)])) is None


# --- integration with the batched loop ------------------------------------

def _looping_client(name="stuck_tool", inp=None):
    """A client that emits the same tool call forever."""
    payload = inp if inp is not None else {"value": "same"}

    def responder(kwargs: dict):
        if not kwargs.get("tools"):
            return [text("[summary]")], "end_turn"
        n = len(kwargs["messages"])
        return (
            [text("still trying"), tool(name, _id=f"toolu_{n}", **payload)],
            "tool_use",
        )

    return FakeAsyncAnthropic(responder=responder)


def _registry_with(handler, name="stuck_tool"):
    registry = ToolRegistry()
    registry.register(
        Tool(
            name,
            "A tool for tests.",
            {"type": "object", "properties": {"value": {"type": "string"}}},
            handler,
        )
    )
    return registry


def test_loop_nudges_once_then_halts(tmp_path):
    async def always_same(_ctx, value="same"):
        return "identical output"

    client = _looping_client()
    agent, events = _agent(
        tmp_path,
        client,
        tools=_registry_with(always_same),
        hooks=Hooks(),
        max_turns=40,
    )
    asyncio.run(agent.run("go"))

    stuck = [e for e in events if e["type"] == "stuck"]
    assert len(stuck) == 2, stuck
    assert stuck[0]["halted"] is False
    assert stuck[0]["pattern"] == "repeat_action_result"
    assert stuck[1]["halted"] is True
    # Halting must beat the round budget, not merely coincide with it.
    assert not any(e["type"] == "error" for e in events)
    assert client.calls < 40


def test_nudge_is_delivered_inside_the_tool_result_block(tmp_path):
    async def always_same(_ctx, value="same"):
        return "identical output"

    client = _looping_client()
    agent, _ = _agent(
        tmp_path,
        client,
        tools=_registry_with(always_same),
        hooks=Hooks(),
        max_turns=40,
    )
    asyncio.run(agent.run("go"))

    # The provider protocol requires tool_result blocks to sit in a user
    # message that answers the assistant's tool_use. The nudge must ride along
    # inside that message rather than arriving as a bare extra user turn.
    nudges = [
        block
        for message in agent.messages
        if message["role"] == "user" and isinstance(message["content"], list)
        for block in message["content"]
        if isinstance(block, dict)
        and block.get("type") == "text"
        and "<stuck" in str(block.get("text", ""))
    ]
    assert len(nudges) == 1
    carrier = next(
        message
        for message in agent.messages
        if message["role"] == "user"
        and isinstance(message["content"], list)
        and any(
            isinstance(b, dict) and "<stuck" in str(b.get("text", ""))
            for b in message["content"]
        )
    )
    assert any(b.get("type") == "tool_result" for b in carrier["content"])


def test_denied_tool_is_caught_before_the_round_budget(tmp_path):
    """The real-world case: a permission hook denies, the model keeps asking."""

    class DenyAll(Hooks):
        async def before_tool(self, ctx, call):
            return "Error: permission denied by policy"

    async def never_runs(_ctx, value="same"):  # pragma: no cover - denied first
        raise AssertionError("denied tool must not execute")

    client = _looping_client()
    agent, events = _agent(
        tmp_path,
        client,
        tools=_registry_with(never_runs),
        hooks=DenyAll(),
        max_turns=40,
    )
    asyncio.run(agent.run("go"))

    stuck = [e for e in events if e["type"] == "stuck"]
    assert stuck and stuck[0]["pattern"] == "repeat_action_error"
    assert stuck[-1]["halted"] is True
    # 3 denials -> nudge, 3 more -> halt. Far short of the 40-round budget.
    assert client.calls <= 8, client.calls


def test_detector_is_opt_out(tmp_path):
    async def always_same(_ctx, value="same"):
        return "identical output"

    client = _looping_client()
    agent, events = _agent(
        tmp_path,
        client,
        tools=_registry_with(always_same),
        hooks=Hooks(),
        max_turns=6,
        stuck_detector=NullStuckDetector(),
    )
    asyncio.run(agent.run("go"))

    assert not [e for e in events if e["type"] == "stuck"]
    # Without detection the loop still only stops at the round budget.
    assert any(e["type"] == "error" for e in events)


def test_history_resets_between_user_turns(tmp_path):
    calls = {"n": 0}

    async def alternating(_ctx, value="same"):
        calls["n"] += 1
        return f"output-{calls['n']}"

    client = _looping_client()
    agent, _ = _agent(
        tmp_path,
        client,
        tools=_registry_with(alternating),
        hooks=Hooks(),
        max_turns=3,
    )
    asyncio.run(agent.run("first"))
    assert agent.recent_steps  # populated by the turn
    agent.messages.clear()
    asyncio.run(agent.run("second"))
    # A fresh intent must not inherit the previous turn's repetition evidence.
    assert len(agent.recent_steps) <= 3


def test_monologue_correction_rides_the_stop_continuation(tmp_path):
    """A resuming stop hook is the only path a monologue can survive on."""

    class AlwaysResume(Hooks):
        async def stop(self, agent, messages, last_text):
            return "keep going"

    def responder(kwargs: dict):
        if not kwargs.get("tools"):
            return [text("[summary]")], "end_turn"
        return [text("thinking out loud")], "end_turn"

    agent, events = _agent(
        tmp_path,
        FakeAsyncAnthropic(responder=responder),
        hooks=AlwaysResume(),
        max_turns=20,
    )
    asyncio.run(agent.run("go"))

    stuck = [e for e in events if e["type"] == "stuck"]
    assert stuck and stuck[0]["pattern"] == "monologue"
    assert stuck[-1]["halted"] is True
    corrected = [
        m for m in agent.messages
        if m["role"] == "user" and isinstance(m["content"], str) and "<stuck" in m["content"]
    ]
    assert len(corrected) == 1
    assert corrected[0]["content"].endswith("keep going")


def test_parallel_batches_record_in_call_order(tmp_path):
    """Ledger order must follow the batch, not completion order."""

    async def slow(_ctx, value="same"):
        await asyncio.sleep(0.02)
        return "slow"

    async def fast(_ctx, value="same"):
        return "fast"

    registry = ToolRegistry()
    for name, handler in (("slow_tool", slow), ("fast_tool", fast)):
        registry.register(
            Tool(
                name,
                "parallel test tool",
                {"type": "object", "properties": {"value": {"type": "string"}}},
                handler,
                parallel_safe=True,
                readonly=True,
            )
        )

    sent = {"done": False}

    def responder(kwargs: dict):
        if not kwargs.get("tools"):
            return [text("[summary]")], "end_turn"
        if sent["done"]:
            return [text("finished")], "end_turn"
        sent["done"] = True
        return (
            [
                text("batch"),
                tool("slow_tool", _id="toolu_1", value="same"),
                tool("fast_tool", _id="toolu_2", value="same"),
            ],
            "tool_use",
        )

    agent, _ = _agent(
        tmp_path,
        FakeAsyncAnthropic(responder=responder),
        tools=registry,
        hooks=Hooks(),
    )
    asyncio.run(agent.run("go"))

    assert [step.name for step in agent.recent_steps] == ["slow_tool", "fast_tool"]
