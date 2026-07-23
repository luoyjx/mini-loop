"""Tests for the developer extension seams -- custom tools, hooks, prompt,
workspace provisioning, and event sinks. All offline via the fake model."""

import asyncio
from pathlib import Path

from mini_loop import (
    Agent,
    Hook,
    Hooks,
    SessionManager,
    Settings,
    default_registry,
)
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, tool

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path, **over) -> Settings:
    base = dict(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR)
    base.update(over)
    return Settings(**base)


def _agent(tmp_path, client, **kw):
    settings = _settings(tmp_path)
    ws = settings.workspace_root / "sess"
    ws.mkdir(parents=True, exist_ok=True)
    events = []

    async def emit(e):
        events.append(e)

    agent = Agent(client=client, settings=settings, workspace=ws, emit=emit, **kw)
    return agent, events


# --- custom tools ----------------------------------------------------------

def test_register_custom_tool_and_call_it(tmp_path):
    reg = default_registry()

    @reg.add("greet", "Greet someone by name.",
             {"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]})
    async def greet(ctx, name):
        # custom tools have full access to the sandbox + per-session state
        ctx.state["greeted"] = name
        return f"Hello, {name}!"

    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("greet", _id="t1", name="World")], "tool_use"),
    ]))
    agent, events = _agent(tmp_path, client, tools=reg)
    asyncio.run(agent.run("say hi"))

    result = next(e for e in events if e["type"] == "tool_result")
    assert result["output"] == "Hello, World!"
    assert agent.state["greeted"] == "World"


def test_registry_subset_and_unregister():
    reg = default_registry()
    assert "task" in reg and "bash" in reg
    assert reg.get("read_file").parallel_safe is True
    assert reg.get("glob").parallel_safe is True
    assert reg.get("bash").parallel_safe is False
    reg.unregister("task")
    assert "task" not in reg
    sub = reg.subset(["bash", "read_file"])
    assert sub.names() == ["bash", "read_file"]


def test_parallel_safe_tool_calls_overlap_and_keep_result_order(tmp_path):
    async def main():
        reg = default_registry()
        gate = asyncio.Event()
        active = 0
        peak = 0
        started = 0

        @reg.add(
            "parallel_probe",
            "Return a label after a delay.",
            {
                "type": "object",
                "properties": {
                    "label": {"type": "string"},
                    "delay": {"type": "number"},
                },
                "required": ["label", "delay"],
            },
            parallel_safe=True,
        )
        async def parallel_probe(ctx, label, delay):
            nonlocal active, peak, started
            active += 1
            peak = max(peak, active)
            started += 1
            if started == 2:
                gate.set()
            try:
                await asyncio.wait_for(gate.wait(), timeout=0.5)
                await asyncio.sleep(delay)
                return label
            finally:
                active -= 1

        client = FakeAsyncAnthropic(responder=scripted([
            ([
                tool("parallel_probe", _id="t1", label="first", delay=0.03),
                tool("parallel_probe", _id="t2", label="second", delay=0),
            ], "tool_use"),
        ]))
        agent, events = _agent(tmp_path, client, tools=reg)
        final = await agent.run("run both")
        result_message = agent.messages[-2]["content"]
        completion_order = [
            event["id"] for event in events
            if event["type"] == "tool_result"
        ]
        return final, peak, result_message, completion_order

    final, peak, results, completion_order = asyncio.run(main())
    assert final == "Done."
    assert peak == 2
    assert completion_order == ["t2", "t1"]
    assert [item["tool_use_id"] for item in results] == ["t1", "t2"]
    assert [item["content"] for item in results] == ["first", "second"]


def test_parallel_tool_calls_are_bounded_and_fail_independently(tmp_path):
    async def main():
        reg = default_registry()
        active = 0
        peak = 0

        @reg.add(
            "bounded_probe",
            "Exercise the parallel tool-call limit.",
            {
                "type": "object",
                "properties": {
                    "label": {"type": "string"},
                    "fail": {"type": "boolean"},
                },
                "required": ["label"],
            },
            parallel_safe=True,
        )
        async def bounded_probe(ctx, label, fail=False):
            nonlocal active, peak
            active += 1
            peak = max(peak, active)
            try:
                await asyncio.sleep(0.03)
                if fail:
                    raise RuntimeError(f"boom:{label}")
                return f"ok:{label}"
            finally:
                active -= 1

        client = FakeAsyncAnthropic(responder=scripted([
            ([
                tool("bounded_probe", _id="t1", label="one"),
                tool("bounded_probe", _id="t2", label="two", fail=True),
                tool("bounded_probe", _id="t3", label="three"),
            ], "tool_use"),
        ]))
        events = []
        manager = SessionManager(
            _settings(tmp_path, max_concurrent_tools=2),
            client,
            tool_registry=reg,
            event_sink=events.append,
        )
        session = manager.create()
        final = await session.run("run bounded batch")
        return final, peak, session.agent.messages[-2]["content"], events

    final, peak, results, events = asyncio.run(main())
    assert final == "Done."
    assert peak == 2
    assert [item["tool_use_id"] for item in results] == ["t1", "t2", "t3"]
    assert results[0]["content"] == "ok:one"
    assert results[1]["content"] == "Error: boom:two"
    assert results[2]["content"] == "ok:three"
    sequences = [event["seq"] for event in events]
    assert sequences == sorted(sequences)
    assert len(sequences) == len(set(sequences))


def test_non_parallel_safe_tool_is_an_ordering_barrier(tmp_path):
    async def main():
        reg = default_registry()
        timeline = []

        @reg.add(
            "parallel_step",
            "Record a parallel-safe step.",
            {
                "type": "object",
                "properties": {"label": {"type": "string"}},
                "required": ["label"],
            },
            parallel_safe=True,
        )
        async def parallel_step(ctx, label):
            timeline.append(f"start:{label}")
            await asyncio.sleep(0.02)
            timeline.append(f"end:{label}")
            return label

        @reg.add(
            "ordered_step",
            "Record a step that must stay ordered.",
            {"type": "object", "properties": {}},
        )
        async def ordered_step(ctx):
            timeline.extend(["start:barrier", "end:barrier"])
            return "barrier"

        client = FakeAsyncAnthropic(responder=scripted([
            ([
                tool("parallel_step", _id="t1", label="a"),
                tool("parallel_step", _id="t2", label="b"),
                tool("ordered_step", _id="t3"),
                tool("parallel_step", _id="t4", label="c"),
                tool("parallel_step", _id="t5", label="d"),
            ], "tool_use"),
        ]))
        agent, _ = _agent(tmp_path, client, tools=reg)
        await agent.run("respect the barrier")
        return timeline

    timeline = asyncio.run(main())
    barrier_start = timeline.index("start:barrier")
    barrier_end = timeline.index("end:barrier")
    assert max(timeline.index("end:a"), timeline.index("end:b")) < barrier_start
    assert barrier_end < min(timeline.index("start:c"), timeline.index("start:d"))


# --- hooks: permission (deny) + output transform ---------------------------

def test_before_tool_hook_denies(tmp_path):
    class NoWrites(Hook):
        async def before_tool(self, ctx, call):
            if call.name == "write_file":
                return "DENIED: writes are disabled for this agent"
            return None

    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("write_file", _id="t1", path="secret.txt", content="leak")], "tool_use"),
    ]))
    agent, events = _agent(tmp_path, client, hooks=Hooks([NoWrites()]))
    asyncio.run(agent.run("write a file"))

    assert not (agent.workspace / "secret.txt").exists()  # handler never ran
    result = next(e for e in events if e["type"] == "tool_result")
    assert result.get("denied") is True
    assert "DENIED" in result["output"]


def test_after_tool_hook_transforms_output(tmp_path):
    class Shout(Hook):
        async def after_tool(self, ctx, call, output):
            return output.upper() if call.name == "bash" else None

    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("bash", _id="t1", command="echo hi")], "tool_use"),
    ]))
    agent, events = _agent(tmp_path, client, hooks=Hooks([Shout()]))
    asyncio.run(agent.run("run echo"))

    result = next(e for e in events if e["type"] == "tool_result")
    assert result["output"] == "HI"


def test_before_tool_hook_rewrites_arguments(tmp_path):
    class Redirect(Hook):
        async def before_tool(self, ctx, call):
            if call.name == "write_file":
                call.input["path"] = "safe/" + call.input["path"]  # sandbox into a subdir
            return None

    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("write_file", _id="t1", path="note.txt", content="ok")], "tool_use"),
    ]))
    agent, _ = _agent(tmp_path, client)
    agent.hooks = Hooks([Redirect()])
    asyncio.run(agent.run("write note"))

    assert (agent.workspace / "safe" / "note.txt").read_text() == "ok"
    assert not (agent.workspace / "note.txt").exists()


# --- custom system prompt --------------------------------------------------

def test_custom_system_builder(tmp_path):
    def build(agent):
        return f"CUSTOM PROMPT for {agent.label} with tools {agent.tools.names()}"

    client = FakeAsyncAnthropic()
    agent, _ = _agent(tmp_path, client, system_builder=build)
    assert agent.system.startswith("CUSTOM PROMPT for main")


# --- manager-level seams: workspace factory + event sink -------------------

def test_workspace_factory_and_event_sink(tmp_path):
    sink = []
    custom_root = tmp_path / "tenants"

    def factory(session_id):
        return custom_root / "acme" / session_id

    settings = _settings(tmp_path, max_concurrent_llm=4)

    async def main():
        client = FakeAsyncAnthropic()
        mgr = SessionManager(settings, client,
                             workspace_factory=factory,
                             event_sink=lambda e: sink.append(e["type"]))
        session = mgr.create()
        assert str(session.workspace).startswith(str(custom_root / "acme"))
        await session.run("hello")
        return session

    session = asyncio.run(main())
    assert (session.workspace).exists()
    assert "done" in sink and "tool_use" in sink


def test_manager_injects_custom_tools_per_session(tmp_path):
    template = default_registry()

    @template.add("ping", "Return pong.", {"type": "object", "properties": {}})
    async def ping(ctx):
        return "pong"

    settings = _settings(tmp_path)

    async def main():
        client = FakeAsyncAnthropic(responder=scripted([
            ([tool("ping", _id="t1")], "tool_use"),
        ]))
        mgr = SessionManager(settings, client, tool_registry=template)
        s = mgr.create()
        # each session gets an independent clone
        assert "ping" in s.agent.tools and s.agent.tools is not template
        await s.run("ping it")
        return s

    asyncio.run(main())
