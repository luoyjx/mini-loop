import asyncio
from dataclasses import FrozenInstanceError
from pathlib import Path

import pytest

from mini_loop.agent import Agent
from mini_loop.config import Settings
from mini_loop.fake_llm import system_text, FakeAsyncAnthropic, _last_result_text, scripted, text, tool
from mini_loop.manager import SessionManager
from mini_loop.registry import Hook, Hooks, ToolCall, ToolContext
from mini_loop.run_context import EXPLICIT_HUMAN, PEER_AGENT, UNTRUSTED, RunContext
from mini_loop.builtins import default_registry


SKILLS_DIR = Path(__file__).resolve().parents[1] / "skills"


def _settings(tmp_path, **over):
    values = {
        "fake_llm": True,
        "workspace_root": tmp_path / "ws",
        "skills_dir": SKILLS_DIR,
    }
    values.update(over)
    return Settings(**values)


def test_run_context_is_immutable_and_delegation_drops_human_authority():
    human = RunContext.explicit_human(
        actor_id="user-1",
        channel="cli",
        stamped_by="test-auth",
        approved_capabilities=("workflow.manage", "workflow.launch"),
    )

    with pytest.raises(FrozenInstanceError):
        human.authority = UNTRUSTED

    child = human.derive_peer_agent(delegated_by="main", actor_id="worker")
    assert human.authority == EXPLICIT_HUMAN
    assert child.authority == PEER_AGENT
    assert child.origin == PEER_AGENT
    assert child.delegated_by == "main"
    assert child.parent_message_id == human.message_id
    assert child.message_id != human.message_id
    assert human.allows("workflow.launch")
    assert child.approved_capabilities == ()
    assert human.with_new_message().approved_capabilities == ()


def test_tool_context_new_fields_keep_legacy_constructor_compatible(tmp_path):
    call = ToolCall("probe", {}, "toolu_probe")
    context = ToolContext(object(), tmp_path, {}, call)

    assert context.call is call
    assert context.run_context is None
    assert context.action_id is None


def test_session_propagates_exact_context_to_tool_and_emits_action_id(tmp_path):
    captured = []
    events = []
    registry = default_registry()

    @registry.add("probe_context", "Capture context.", {"type": "object", "properties": {}}, risk="read")
    async def probe_context(ctx):
        captured.append(ctx)
        return ctx.run_context.authority

    async def main():
        manager = SessionManager(
            _settings(tmp_path),
            FakeAsyncAnthropic(responder=scripted([
                ([tool("probe_context", _id="toolu_context")], "tool_use"),
            ])),
            tool_registry=registry,
            event_sink=events.append,
        )
        session = manager.create()
        human = RunContext.explicit_human(actor_id="user-1")
        await session.run("inspect context", run_context=human)
        return human

    human = asyncio.run(main())
    assert captured[0].run_context is human
    assert captured[0].action_id.startswith("act_")
    tool_use = next(event for event in events if event["type"] == "tool_use")
    tool_result = next(event for event in events if event["type"] == "tool_result")
    assert tool_use["action_id"] == captured[0].action_id
    assert tool_result["action_id"] == captured[0].action_id


def test_action_id_is_stable_per_session_message_and_tool_use(tmp_path):
    async def main():
        settings = _settings(tmp_path)
        workspace = settings.workspace_root / "action-ids"
        workspace.mkdir(parents=True)
        first_events = []
        second_events = []

        async def emit_first(event):
            first_events.append(event)

        async def emit_second(event):
            second_events.append(event)

        first_agent = Agent(
            client=FakeAsyncAnthropic(),
            settings=settings,
            workspace=workspace,
            emit=emit_first,
            state={"session_id": "session-a"},
        )
        second_agent = Agent(
            client=FakeAsyncAnthropic(),
            settings=settings,
            workspace=workspace,
            emit=emit_second,
            state={"session_id": "session-b"},
        )
        call = ToolCall("missing_tool", {}, "toolu_same")
        message_one = RunContext(message_id="msg_one")
        message_two = RunContext(message_id="msg_two")

        await first_agent._exec_tool(call, run_context=message_one)
        await first_agent._exec_tool(call, run_context=message_one)
        await first_agent._exec_tool(call, run_context=message_two)
        await second_agent._exec_tool(call, run_context=message_one)
        return first_events, second_events

    first_events, second_events = asyncio.run(main())
    first_ids = [
        event["action_id"]
        for event in first_events
        if event["type"] == "tool_use"
    ]
    second_id = next(
        event["action_id"]
        for event in second_events
        if event["type"] == "tool_use"
    )
    assert first_ids[0] == first_ids[1]
    assert first_ids[2] != first_ids[0]
    assert second_id != first_ids[0]


def test_legacy_session_run_defaults_to_untrusted(tmp_path):
    authorities = []
    registry = default_registry()

    @registry.add("probe_context", "Capture context.", {"type": "object", "properties": {}}, risk="read")
    async def probe_context(ctx):
        authorities.append(ctx.run_context.authority)
        return "ok"

    async def main():
        manager = SessionManager(
            _settings(tmp_path),
            FakeAsyncAnthropic(responder=scripted([
                ([tool("probe_context")], "tool_use"),
            ])),
            tool_registry=registry,
        )
        await manager.create().run("legacy call")

    asyncio.run(main())
    assert authorities == [UNTRUSTED]


def test_inline_subagent_tool_context_is_peer_agent(tmp_path):
    seen = []

    class CaptureContext(Hook):
        async def before_tool(self, ctx, call):
            seen.append((ctx.agent.depth, call.name, ctx.run_context))
            return None

    def responder(kwargs):
        tools = kwargs.get("tools")
        messages = kwargs["messages"]
        last = messages[-1]
        is_child = "subagent" in system_text(kwargs)
        if not tools:
            return [text("[summary]")], "end_turn"
        if isinstance(last.get("content"), str):
            if is_child:
                return [tool("bash", _id="child_tool", command="echo child")], "tool_use"
            return [
                tool(
                    "task",
                    _id="parent_tool",
                    prompt="inspect",
                    agent_type="Explore",
                )
            ], "tool_use"
        if is_child:
            return [text("child done")], "end_turn"
        return [text("parent done: " + _last_result_text(last["content"]))], "end_turn"

    async def main():
        settings = _settings(tmp_path)
        workspace = settings.workspace_root / "agent"
        workspace.mkdir(parents=True)
        agent = Agent(
            client=FakeAsyncAnthropic(responder=responder),
            settings=settings,
            workspace=workspace,
            hooks=Hooks([CaptureContext()]),
            system="Main orchestrator.",
        )
        human = RunContext.explicit_human(actor_id="user-1")
        await agent.run("delegate", run_context=human)
        return human

    human = asyncio.run(main())
    parent = next(context for depth, name, context in seen if depth == 0 and name == "task")
    child = next(context for depth, name, context in seen if depth == 1 and name == "bash")
    assert parent is human
    assert child.authority == PEER_AGENT
    assert child.parent_message_id == human.message_id


def test_manager_teammate_initial_run_is_peer_agent(tmp_path):
    seen = []
    registry = default_registry()

    @registry.add("probe_context", "Capture context.", {"type": "object", "properties": {}}, risk="read")
    async def probe_context(ctx):
        seen.append(ctx.run_context)
        return "ok"

    async def main():
        manager = SessionManager(
            _settings(tmp_path),
            FakeAsyncAnthropic(responder=scripted([
                ([tool("probe_context")], "tool_use"),
            ])),
            tool_registry=registry,
        )
        parent = manager.create()
        human = RunContext.explicit_human(actor_id="user-1")
        await manager.spawn_teammate(
            parent.id,
            "worker",
            "researcher",
            "inspect",
            run_context=human,
        )
        teammate = manager.teammate_session(parent.id, "worker")
        await teammate.spawn_task
        teammate.lifecycle_task.cancel()
        return human

    human = asyncio.run(main())
    assert seen[0].authority == PEER_AGENT
    assert seen[0].parent_message_id == human.message_id
