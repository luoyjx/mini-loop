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
    reg.unregister("task")
    assert "task" not in reg
    sub = reg.subset(["bash", "read_file"])
    assert sub.names() == ["bash", "read_file"]


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
