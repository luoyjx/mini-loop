"""Behavioral coverage for the learn-claude-code s03-s20 curriculum.

These tests intentionally assert end-to-end invariants, not just that a helper
or tool name exists.
"""

from __future__ import annotations

import asyncio
import json
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime
from pathlib import Path

import pytest

from mini_loop import (
    Agent,
    DefaultCompactor,
    DefaultRecovery,
    Hook,
    Hooks,
    PermissionHook,
    PermissionRule,
    SessionManager,
    Settings,
    StdioMCP,
    TaskStore,
    Tool,
    WorktreeManager,
    default_registry,
    full_registry,
    snip_compact,
    tool_result_budget,
)
from mini_loop.background import background_injector
from mini_loop.cron import cron_matches, validate_cron
from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
from mini_loop.registry import ToolCall, ToolContext


SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path, **over):
    base = dict(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR)
    base.update(over)
    return Settings(**base)


def _agent(tmp_path, client=None, **kw):
    settings = kw.pop("settings", _settings(tmp_path))
    workspace = settings.workspace_root / "session"
    workspace.mkdir(parents=True, exist_ok=True)
    return Agent(client=client or FakeAsyncAnthropic(), settings=settings,
                 workspace=workspace, **kw)


def test_permission_pipeline_can_ask_and_deny(tmp_path):
    decisions = []

    async def approve(ctx, call, rule):
        decisions.append((call.name, rule.name))
        return False

    hook = PermissionHook(
        rules=[PermissionRule("writes", ("write_file",), lambda _ctx, _call: True,
                              "writes need approval")],
        approval=approve,
    )
    agent = _agent(tmp_path, hooks=Hooks([hook]))
    call = ToolCall("write_file", {"path": "x.txt", "content": "x"}, "p1")
    ctx = ToolContext(agent, agent.workspace, agent.state, call)
    denied = asyncio.run(hook.before_tool(ctx, call))
    assert "denied" in denied.lower()
    assert decisions == [("write_file", "writes")]
    assert not (agent.workspace / "x.txt").exists()


def test_user_prompt_and_stop_hooks_are_in_the_loop(tmp_path):
    class Lifecycle(Hook):
        def __init__(self):
            self.stops = 0

        async def on_user_prompt(self, agent, text_value):
            return text_value + " [hooked]"

        async def on_stop(self, agent, messages, last_text):
            self.stops += 1
            return "continue once" if self.stops == 1 else None

    seen = []

    def responder(kwargs):
        seen.append(kwargs["messages"][-1]["content"])
        return [text("done")], "end_turn"

    lifecycle = Lifecycle()
    agent = _agent(tmp_path, FakeAsyncAnthropic(responder=responder),
                   hooks=Hooks([lifecycle]))
    asyncio.run(agent.run("hello"))
    assert seen == ["hello [hooked]", "continue once"]


def test_loop_uses_actual_tool_blocks_instead_of_stop_reason(tmp_path):
    turns = 0

    def responder(_kwargs):
        nonlocal turns
        turns += 1
        if turns == 1:
            # Deliberately inconsistent provider metadata: the tool block is
            # still authoritative and must execute.
            return [tool("write_file", path="signal.txt", content="worked")], "end_turn"
        # The inverse mismatch must stop instead of spinning an empty tool turn.
        return [text("finished")], "tool_use"

    agent = _agent(tmp_path, FakeAsyncAnthropic(responder=responder))
    final = asyncio.run(agent.run("go"))
    assert final == "finished"
    assert (agent.workspace / "signal.txt").read_text() == "worked"
    assert turns == 2


def test_notifications_are_compacted_before_llm_and_manual_compact_continues(tmp_path):
    snapshots = []

    class ProbeCompactor:
        def __init__(self):
            self.explicit = 0

        async def maybe_compact(self, agent):
            snapshots.append(agent.messages[-1]["content"])

        async def compact(self, agent):
            self.explicit += 1
            agent.messages[:] = [{"role": "user", "content": "[compacted]"}]

    injected = False

    async def injector(_agent):
        nonlocal injected
        if injected:
            return []
        injected = True
        return [{"role": "user", "content": "<task_notification>ready</task_notification>"}]

    turns = 0

    def responder(_kwargs):
        nonlocal turns
        turns += 1
        if turns == 1:
            return [tool("compress")], "tool_use"
        return [text("continued after compact")], "end_turn"

    compactor = ProbeCompactor()
    agent = _agent(tmp_path, FakeAsyncAnthropic(responder=responder),
                   compactor=compactor, injectors=[injector])
    final = asyncio.run(agent.run("go"))
    assert snapshots[0] == "<task_notification>ready</task_notification>"
    assert compactor.explicit == 1
    assert turns == 2
    assert final == "continued after compact"


def test_default_compaction_pipeline_runs_budget_before_snip_and_micro(tmp_path):
    events = []

    async def emit(event):
        events.append(event)

    compactor = DefaultCompactor(
        token_threshold=10_000_000, max_messages=20, result_budget=100_000
    )
    agent = _agent(tmp_path, compactor=compactor, emit=emit)
    messages = [{"role": "user", "content": "start"}]
    for index in range(30):
        messages.extend([
            {"role": "assistant", "content": [
                {"type": "tool_use", "id": f"old-{index}", "name": "read_file", "input": {}}
            ]},
            {"role": "user", "content": [
                {"type": "tool_result", "tool_use_id": f"old-{index}", "content": "x" * 300}
            ]},
        ])
    messages.extend([
        {"role": "assistant", "content": [
            {"type": "tool_use", "id": f"large-{index}", "name": "read_file", "input": {}}
            for index in range(4)
        ]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": f"large-{index}", "content": "z" * 60_000}
            for index in range(4)
        ]},
    ])
    agent.messages[:] = messages
    asyncio.run(compactor.maybe_compact(agent))
    kinds = [event["kind"] for event in events if event["type"] == "compact"]
    assert kinds[:3] == ["budget", "snip", "micro"]


def test_core_glob_and_read_offset_are_workspace_bound(tmp_path):
    agent = _agent(tmp_path)
    (agent.workspace / "nested").mkdir()
    (agent.workspace / "nested" / "one.txt").write_text("a\nb\nc\nd\n")
    (tmp_path / "outside.txt").write_text("secret")
    assert agent.toolset.run_read("nested/one.txt", limit=2, offset=1) == "b\nc\n... (1 more lines)"
    assert agent.toolset.run_glob("**/*.txt") == "nested/one.txt"
    assert agent.toolset.run_glob("../../*.txt") == "(no matches)"


def test_default_permission_rules_cover_workspace_and_mcp_boundaries(tmp_path):
    hook = PermissionHook()
    agent = _agent(tmp_path)

    async def check(call):
        ctx = ToolContext(agent, agent.workspace, agent.state, call)
        return await hook.before_tool(ctx, call)

    escaped = asyncio.run(check(ToolCall(
        "write_file", {"path": "../outside.txt", "content": "x"}, "escape"
    )))
    deploy = asyncio.run(check(ToolCall("mcp__prod__deploy", {}, "deploy")))
    background_delete = asyncio.run(check(ToolCall(
        "background_run", {"command": "rm obsolete.txt"}, "background-delete"
    )))
    allowed = asyncio.run(check(ToolCall(
        "write_file", {"path": "inside.txt", "content": "x"}, "inside"
    )))
    assert "escapes" in escaped.lower()
    assert "approval required" in deploy.lower()
    assert "approval required" in background_delete.lower()
    assert allowed is None


def test_four_layer_compaction_helpers_preserve_pairs_and_persist(tmp_path):
    messages = [{"role": "user", "content": "start"}]
    for i in range(8):
        messages.extend([
            {"role": "assistant", "content": [
                {"type": "tool_use", "id": f"u{i}", "name": "read_file", "input": {}}
            ]},
            {"role": "user", "content": [
                {"type": "tool_result", "tool_use_id": f"u{i}", "content": "x" * 300}
            ]},
        ])
    removed = snip_compact(messages, max_messages=8)
    assert removed > 0
    # No retained tool_result may be orphaned from the preceding tool_use.
    for i, message in enumerate(messages):
        if isinstance(message.get("content"), list) and any(
                p.get("type") == "tool_result" for p in message["content"]):
            assert i and any(p.get("type") == "tool_use"
                             for p in messages[i - 1].get("content", []))

    latest = [{"role": "user", "content": [
        {"type": "tool_result", "tool_use_id": "huge", "content": "z" * 2000}
    ]}]
    persisted = tool_result_budget(latest, tmp_path, max_bytes=200, preview_chars=40)
    assert persisted == 1
    assert "persisted-output" in latest[0]["content"][0]["content"]
    assert list((tmp_path / ".task_outputs" / "tool-results").glob("*.txt"))


def test_oversized_tool_output_is_persisted_before_the_next_llm_call(tmp_path):
    observed = []
    injection_round = 0

    async def late_notification(_agent):
        nonlocal injection_round
        injection_round += 1
        if injection_round == 2:
            return [{"role": "user", "content": "<task_notification>also ready</task_notification>"}]
        return []

    def responder(kwargs):
        if not observed:
            observed.append("first")
            return [tool("huge_output")], "tool_use"
        batch = next(
            message["content"] for message in reversed(kwargs["messages"])
            if isinstance(message.get("content"), list)
            and any(part.get("type") == "tool_result" for part in message["content"])
        )
        observed.append(batch[0]["content"])
        return [text("done")], "end_turn"

    registry = default_registry()
    registry.register(Tool(
        "huge_output", "return a large payload", {"type": "object", "properties": {}},
        lambda _ctx: "z" * 10_000,
    ))
    agent = _agent(
        tmp_path,
        FakeAsyncAnthropic(responder=responder),
        tools=registry,
        compactor=DefaultCompactor(token_threshold=10_000_000, result_budget=4_000),
        injectors=[late_notification],
    )
    assert asyncio.run(agent.run("go")) == "done"
    assert "persisted-output" in observed[1]
    persisted = list((agent.workspace / ".task_outputs" / "tool-results").glob("*.txt"))
    assert len(persisted) == 1 and len(persisted[0].read_text()) == 10_000


def test_system_prompt_rebuilds_when_runtime_tools_change(tmp_path):
    systems = []

    def responder(kwargs):
        systems.append(kwargs.get("system", ""))
        if len(systems) == 1:
            return [tool("add_runtime_tool")], "tool_use"
        return [text("done")], "end_turn"

    registry = default_registry()

    async def add_runtime_tool(ctx):
        ctx.agent.tools.register(Tool(
            "late_capability", "runtime", {"type": "object", "properties": {}},
            lambda _ctx: "ok",
        ))
        return "added"

    registry.register(Tool("add_runtime_tool", "add", {"type": "object", "properties": {}},
                           add_runtime_tool))
    agent = _agent(tmp_path, FakeAsyncAnthropic(responder=responder), tools=registry)
    asyncio.run(agent.run("go"))
    assert "late_capability" not in systems[0]
    assert "late_capability" in systems[1]


def test_recovery_continues_after_second_truncation():
    class Stub:
        def __init__(self):
            self.state = {}

        async def _send(self, *args, **kwargs):
            return None

    calls = []

    async def call(kwargs):
        calls.append((kwargs["model"], kwargs["max_tokens"], len(kwargs["messages"])))
        if len(calls) < 3:
            return type("R", (), {"stop_reason": "max_tokens", "content": [text("partial")]})()
        return type("R", (), {"stop_reason": "end_turn", "content": [text("done")]})()

    stub = Stub()
    kwargs = {"model": "primary", "messages": [{"role": "user", "content": "x"}],
              "max_tokens": 8000}
    asyncio.run(DefaultRecovery(max_continuations=1).run(stub, kwargs, call))
    assert calls[0][1] == 8000 and calls[1][1] > 8000
    assert calls[2][2] == 3  # original + truncated assistant + continuation prompt


def test_unrecoverable_llm_error_becomes_an_agent_result(tmp_path):
    events = []

    async def emit(event):
        events.append(event)

    def fail(_kwargs):
        raise ValueError("provider rejected request")

    agent = _agent(tmp_path, FakeAsyncAnthropic(responder=fail), emit=emit)
    final = asyncio.run(agent.run("go"))
    assert final == "[Error] ValueError: provider rejected request"
    assert any(event["type"] == "error" for event in events)


def test_task_state_machine_rejects_completing_pending(tmp_path):
    store = TaskStore(tmp_path)
    task = store.create("must be claimed")
    assert "in_progress" in store.complete(task.id)
    assert store.load(task.id).status == "pending"
    with pytest.raises(ValueError):
        store.create("unsafe", blocked_by=["../../outside"])
    with pytest.raises(ValueError):
        store.create("unsafe", worktree="..")


def test_task_claim_is_atomic_across_store_instances(tmp_path):
    first, second = TaskStore(tmp_path), TaskStore(tmp_path)
    task = first.create("one owner")
    with ThreadPoolExecutor(max_workers=2) as pool:
        results = list(pool.map(
            lambda pair: pair[0].claim(task.id, pair[1]),
            [(first, "alice"), (second, "bob")],
        ))
    assert sum(result.startswith("Claimed") for result in results) == 1
    assert first.load(task.id).owner in {"alice", "bob"}


def test_memory_is_selected_and_extracted_across_sessions(tmp_path):
    main_prompts = []

    def responder(kwargs):
        if kwargs.get("tools"):
            main_prompts.append(kwargs["messages"][-1]["content"])
            return [text("done")], "end_turn"
        prompt = kwargs["messages"][-1]["content"]
        if "Select relevant memory indices" in prompt:
            return [text("[0]")], "end_turn"
        if "extract durable facts" in prompt:
            return [text(json.dumps([{
                "name": "prefers-tabs",
                "type": "user",
                "description": "indentation preference",
                "body": "Use tabs for indentation.",
            }]))], "end_turn"
        return [text("[]")], "end_turn"

    async def main():
        manager = SessionManager(_settings(tmp_path), FakeAsyncAnthropic(responder=responder),
                                 enable_features=True)
        first = manager.create()
        await first.run("I always prefer tabs")
        second = manager.create()
        await second.run("How should indentation work?")
        await manager.stop()

    asyncio.run(main())
    assert "prefers-tabs" in (tmp_path / "ws" / ".memory" / "MEMORY.md").read_text()
    assert any("<memory_context>" in str(prompt) and "Use tabs" in str(prompt)
               for prompt in main_prompts)


def test_bash_background_flag_routes_through_background_manager(tmp_path):
    async def main():
        agent = _agent(tmp_path, tools=full_registry(), injectors=[background_injector])
        call = ToolCall("bash", {"command": "echo automatic-bg", "run_in_background": True}, "bg")
        output = await agent._exec_tool(call)
        assert "Started background task" in output
        await asyncio.sleep(0.05)
        injected = await background_injector(agent)
        manager = agent.state["background"]
        manager.run("sleep 10")
        cancelled_id = list(manager._tasks)[-1]
        await asyncio.sleep(0.02)
        await manager.close()
        return injected, manager._tasks[cancelled_id]["status"]

    messages, status = asyncio.run(main())
    assert messages and "automatic-bg" in messages[0]["content"]
    assert status == "cancelled"


def test_cron_semantics_and_validation_are_strict():
    monday = datetime(2026, 7, 20, 9, 0)
    assert cron_matches("0 9 * * 1", monday)
    assert not cron_matches("0 9 * * 2", monday)
    assert not cron_matches("0 9 21 * *", monday)
    assert validate_cron("garbage garbage garbage garbage garbage") is not None
    assert validate_cron("*/0 * * * *") is not None


def test_cron_lazily_starts_without_comprehensive_mode(tmp_path):
    async def main():
        manager = SessionManager(_settings(tmp_path), FakeAsyncAnthropic())
        assert manager.cron._task is None
        manager.cron.schedule("standalone", "* * * * *", "wake", durable=False)
        assert manager.cron._task is not None
        await manager.stop()

    asyncio.run(main())


def test_durable_cron_restores_its_session_after_restart(tmp_path):
    async def main():
        settings = _settings(tmp_path)
        first = SessionManager(settings, FakeAsyncAnthropic(), enable_features=True)
        session = first.create()
        first.cron.schedule(session.id, "* * * * *", "restored", durable=True)

        second = SessionManager(settings, FakeAsyncAnthropic(), enable_features=True)
        assert second.get(session.id) is None
        job = next(iter(second.cron.jobs.values()))
        second.cron._fire(job)
        await asyncio.sleep(0.05)
        restored = second.get(session.id)
        await second.stop()
        return restored

    restored = asyncio.run(main())
    assert restored is not None and restored.run_count == 1


def test_full_registry_contains_every_optional_curriculum_entrypoint():
    names = set(full_registry().names())
    assert {
        "connect_mcp", "create_worktree", "keep_worktree", "remove_worktree",
        "glob", "request_shutdown", "request_plan", "submit_plan", "review_plan",
    } <= names


def test_worktree_manager_binds_task_and_audits(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir()
    for args in (["init", "-q"], ["config", "user.email", "t@t"],
                 ["config", "user.name", "t"]):
        subprocess.run(["git", *args], cwd=repo, check=True)
    (repo / "seed.txt").write_text("seed")
    subprocess.run(["git", "add", "."], cwd=repo, check=True)
    subprocess.run(["git", "commit", "-qm", "seed"], cwd=repo, check=True)

    board = tmp_path / "board"
    store = TaskStore(board)
    task = store.create("isolated work")
    manager = WorktreeManager(repo)
    result = manager.create("alice", task_id=task.id, task_store=store)
    assert "created" in result.lower()
    assert store.load(task.id).worktree == "alice"
    assert manager.path_for("alice").exists()
    assert '"type": "create"' in manager.events_path.read_text()
    original_changes = manager._changes
    manager._changes = lambda _name: (-1, -1)
    assert "could not verify" in manager.remove("alice").lower()
    manager._changes = original_changes
    assert "removed" in manager.remove("alice", discard_changes=True).lower()


def test_autonomous_teammate_enters_task_worktree(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir()
    for args in (["init", "-q"], ["config", "user.email", "t@t"],
                 ["config", "user.name", "t"]):
        subprocess.run(["git", *args], cwd=repo, check=True)
    (repo / "seed.txt").write_text("seed")
    subprocess.run(["git", "add", "."], cwd=repo, check=True)
    subprocess.run(["git", "commit", "-qm", "seed"], cwd=repo, check=True)

    async def main():
        settings = _settings(tmp_path, repo_root=repo, team_idle_poll=0.01, team_idle_timeout=0.2)
        manager = SessionManager(settings, FakeAsyncAnthropic(), enable_features=True)
        lead = manager.create()
        board = TaskStore(lead.workspace)
        task = board.create("isolated")
        assert "created" in manager.worktrees.create(
            "task-wt", task_id=task.id, task_store=board).lower()
        await manager.spawn_teammate(lead.id, "alice", "worker", "stand by")
        teammate = manager.teammate_session(lead.id, "alice")
        await teammate.spawn_task
        expected = manager.worktrees.path_for("task-wt")
        for _ in range(30):
            if teammate.agent.workspace == expected:
                break
            await asyncio.sleep(0.01)
        actual = teammate.agent.workspace
        await manager.stop()
        return actual, expected

    actual, expected = asyncio.run(main())
    assert actual == expected


def test_stdio_mcp_completes_initialize_discovery_and_call(tmp_path):
    marker = tmp_path / "initialized"
    server = tmp_path / "mcp_server.py"
    server.write_text(
        "import json, sys\n"
        f"marker = {str(marker)!r}\n"
        "for line in sys.stdin:\n"
        "    msg = json.loads(line)\n"
        "    if msg.get('method') == 'notifications/initialized':\n"
        "        open(marker, 'w').write('yes')\n"
        "        continue\n"
        "    method = msg.get('method')\n"
        "    if method == 'initialize': result = {'protocolVersion': '2024-11-05', 'capabilities': {}}\n"
        "    elif method == 'tools/list': result = {'tools': [{'name': 'echo', 'description': 'echo', 'inputSchema': {'type': 'object', 'properties': {'value': {'type': 'string'}}}, 'annotations': {'readOnlyHint': True}}]}\n"
        "    elif method == 'tools/call': result = {'content': [{'type': 'text', 'text': msg['params']['arguments']['value']}]}\n"
        "    else: result = {}\n"
        "    print(json.dumps({'jsonrpc': '2.0', 'id': msg['id'], 'result': result}), flush=True)\n"
    )

    async def main():
        client = StdioMCP("local", [sys.executable, str(server)])
        tools = await client.list_tools()
        result = await client.call_tool("echo", {"value": "hello-mcp"})
        await client.close()
        return tools, result

    tools, result = asyncio.run(main())
    assert tools[0]["name"] == "echo"
    assert tools[0]["annotations"]["readOnlyHint"] is True
    assert result == "hello-mcp"
    assert marker.read_text() == "yes"


def test_team_protocol_and_autonomous_claim(tmp_path):
    async def main():
        settings = _settings(tmp_path, team_idle_poll=0.01, team_idle_timeout=0.2)
        manager = SessionManager(settings, FakeAsyncAnthropic(), enable_features=True)
        lead = manager.create()
        task_store = TaskStore(lead.workspace)
        task = task_store.create("pick me")
        await manager.spawn_teammate(lead.id, "alice", "worker", "stand by")
        teammate = next(s for s in manager.list() if s.id != lead.id)
        await teammate.spawn_task
        for _ in range(30):
            if task_store.load(task.id).owner == "alice":
                break
            await asyncio.sleep(0.01)
        assert task_store.load(task.id).owner == "alice"

        request_id = manager.request_shutdown(lead.id, "alice", "done")
        assert request_id.startswith("req_")
        for _ in range(30):
            manager.consume_team_inbox(lead.id, "lead")
            if manager.protocols[request_id].status == "approved":
                break
            await asyncio.sleep(0.01)
        assert manager.protocols[request_id].status == "approved"
        await manager.stop()

    asyncio.run(main())


def test_plan_approval_protocol_is_correlated_by_request_id(tmp_path):
    async def main():
        settings = _settings(tmp_path, team_idle_poll=0.01, team_idle_timeout=0.2)
        manager = SessionManager(settings, FakeAsyncAnthropic(), enable_features=True)
        lead = manager.create()
        await manager.spawn_teammate(lead.id, "alice", "planner", "draft a plan")
        teammate = manager.teammate_session(lead.id, "alice")
        await teammate.spawn_task
        assert "runtime_team_tool" not in teammate.agent.refresh_system()
        teammate.agent.tools.register(Tool(
            "runtime_team_tool", "late team capability",
            {"type": "object", "properties": {}}, lambda _ctx: "ok",
        ))
        assert "runtime_team_tool" in teammate.agent.refresh_system()
        request_id = manager.submit_plan(lead.id, "alice", "1. inspect\n2. change\n3. test")
        inbox = manager.consume_team_inbox(lead.id, "lead")
        plan_message = next(message for message in inbox if message["type"] == "plan_approval_request")
        assert plan_message["metadata"]["request_id"] == request_id
        assert "approved" in manager.review_plan(lead.id, request_id, True, "go").lower()
        assert manager.protocols[request_id].status == "approved"
        await manager.stop()

    asyncio.run(main())
