"""Offline tests for the newly added learn-claude-code features:
recovery (s11), task system (s12), background (s13), memory (s09),
cron (s14), teams (s15-17), worktrees (s18), MCP (s19)."""

import asyncio
import subprocess
from datetime import datetime
from pathlib import Path

from mini_loop import (
    Agent,
    BackgroundManager,
    CronScheduler,
    DefaultRecovery,
    InProcessMCP,
    MemoryStore,
    MessageBus,
    SessionManager,
    Settings,
    TaskStore,
    background_injector,
    default_registry,
    full_registry,
    install_mcp,
    memory_system_builder,
    register_mcp,
    worktree_workspace_factory,
)
from mini_loop.cron import CronJob, cron_matches
import mini_loop.recovery as recovery
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, tool

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path, **over):
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

    return Agent(client=client, settings=settings, workspace=ws, emit=emit, **kw), events


class _Stub:
    def __init__(self, workspace=None):
        self.messages = []
        self.workspace = workspace
        self.state = {}
        self.label = "main"
        self.depth = 0

    async def _send(self, *a, **k):
        pass


def _resp(stop_reason):
    return type("M", (), {"stop_reason": stop_reason, "content": []})()


# --- s11 error recovery ----------------------------------------------------

def test_recovery_retries_then_succeeds(monkeypatch):
    monkeypatch.setattr(recovery, "backoff_delay", lambda *a, **k: 0)

    class RateLimitError(Exception):
        pass

    n = {"calls": 0}

    async def call(kw):
        n["calls"] += 1
        if n["calls"] < 3:
            raise RateLimitError("429 rate limited")
        return _resp("end_turn")

    asyncio.run(DefaultRecovery().run(_Stub(), {"model": "m", "messages": [], "max_tokens": 8000}, call))
    assert n["calls"] == 3


def test_recovery_escalates_max_tokens():
    n = {"calls": 0}

    async def call(kw):
        n["calls"] += 1
        return _resp("max_tokens" if n["calls"] == 1 else "end_turn")

    kw = {"model": "m", "messages": [], "max_tokens": 8000}
    asyncio.run(DefaultRecovery().run(_Stub(), kw, call))
    assert kw["max_tokens"] == 64000 and n["calls"] == 2


def test_recovery_reactive_compact_on_prompt_too_long(monkeypatch):
    monkeypatch.setattr(recovery, "backoff_delay", lambda *a, **k: 0)

    async def call(kw):
        if len(kw["messages"]) > 10:  # succeeds once reactive_compact trims to ~7
            raise RuntimeError("prompt is too long: too many tokens")
        return _resp("end_turn")

    kw = {"model": "m", "messages": [{"role": "user", "content": str(i)} for i in range(20)], "max_tokens": 8000}
    asyncio.run(DefaultRecovery().run(_Stub(), kw, call))
    assert len(kw["messages"]) <= 7  # reactive_compact trimmed it


def test_recovery_falls_back_to_model_after_529s(monkeypatch):
    monkeypatch.setattr(recovery, "backoff_delay", lambda *a, **k: 0)
    n = {"calls": 0}

    async def call(kw):
        n["calls"] += 1
        if n["calls"] <= 3:
            raise RuntimeError("overloaded 529")
        return _resp("end_turn")

    kw = {"model": "primary", "messages": [], "max_tokens": 8000}
    asyncio.run(DefaultRecovery(fallback_model="backup").run(_Stub(), kw, call))
    assert kw["model"] == "backup"


# --- s12 task system -------------------------------------------------------

def test_task_store_dependency_graph(tmp_path):
    s = TaskStore(tmp_path)
    a = s.create("A")
    b = s.create("B", blocked_by=[a.id])
    assert not s.can_start(b.id)
    assert "blocked" in s.claim(b.id, "me").lower()
    assert "Claimed" in s.claim(a.id, "me")
    assert "Completed" in s.complete(a.id)
    assert s.can_start(b.id)
    assert "Claimed" in s.claim(b.id, "me")


def test_task_tools_through_loop(tmp_path):
    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("create_task", subject="build feature")], "tool_use"),
        ([tool("list_tasks")], "tool_use"),
    ]))
    agent, events = _agent(tmp_path, client, tools=full_registry())
    asyncio.run(agent.run("plan it"))
    outputs = [e["output"] for e in events if e["type"] == "tool_result"]
    assert any("Created task_" in o for o in outputs)
    assert any("build feature" in o for o in outputs)


# --- s13 background tasks --------------------------------------------------

def test_background_runs_and_injects(tmp_path):
    async def main():
        mgr = BackgroundManager(tmp_path)
        mgr.run("echo hello-bg")
        await asyncio.sleep(0.3)
        agent = _Stub(tmp_path)
        agent.state["background"] = mgr
        return await background_injector(agent)

    msgs = asyncio.run(main())
    assert msgs and "hello-bg" in msgs[0]["content"]
    assert "task_notification" in msgs[0]["content"]


# --- s09 memory ------------------------------------------------------------

def test_memory_store_and_index(tmp_path):
    m = MemoryStore(tmp_path / ".memory")
    m.write("prefers tabs", "user", "indentation preference", "The user prefers tabs over spaces.")
    assert "prefers tabs" in m.index()
    hits = m.search("tabs")
    assert hits and "tabs" in hits[0]["body"]
    assert (tmp_path / ".memory" / "MEMORY.md").exists()


def test_memory_system_builder(tmp_path):
    m = MemoryStore(tmp_path / ".m")
    m.write("deadline", "project", "ship date", "Ship by Friday.")
    build = memory_system_builder(lambda a: "BASE PROMPT", m)
    out = build(_Stub())
    assert out.startswith("BASE PROMPT") and "deadline [project]" in out


# --- s14 cron --------------------------------------------------------------

def test_cron_matches():
    dt = datetime(2026, 6, 16, 9, 30)  # a Tuesday
    assert cron_matches("* * * * *", dt)
    assert cron_matches("30 9 * * *", dt)
    assert not cron_matches("0 9 * * *", dt)
    assert cron_matches("*/15 * * * *", dt)       # 30 % 15 == 0
    assert cron_matches("30 9 16 6 *", dt)
    assert cron_matches("0-45 9 * * *", dt)       # 30 in 0-45


def test_cron_schedule_list_cancel(tmp_path):
    sched = CronScheduler(manager=None, durable_path=tmp_path / ".cron.json")
    assert "Scheduled" in sched.schedule("sess", "* * * * *", "ping", durable=True)
    job_id = next(iter(sched.jobs))
    assert "ping" in sched.list_for("sess")
    assert (tmp_path / ".cron.json").exists()
    assert "Cancelled" in sched.cancel(job_id)
    assert sched.list_for("sess") == "No scheduled jobs."


def test_cron_fires_into_session(tmp_path):
    class FakeSession:
        def __init__(self):
            self.ran = []

        async def run(self, prompt):
            self.ran.append(prompt)

    class FakeMgr:
        def __init__(self, s):
            self._s = s

        def get(self, sid):
            return self._s

    async def main():
        s = FakeSession()
        sched = CronScheduler(FakeMgr(s))
        sched._fire(CronJob(id="j1", cron="* * * * *", prompt="tick", session_id="sess"))
        await asyncio.sleep(0.05)
        return s.ran

    assert "tick" in asyncio.run(main())[0]


# --- s15-17 teams ----------------------------------------------------------

def test_message_bus_send_read_drains():
    bus = MessageBus()
    bus.send("t/lead", "t/alice", "do X")
    msgs = bus.read("t/alice")
    assert msgs and msgs[0]["content"] == "do X"
    assert bus.read("t/alice") == []  # drained


def test_spawn_teammate_shares_workspace(tmp_path):
    settings = _settings(tmp_path)

    async def main():
        mgr = SessionManager(settings, FakeAsyncAnthropic(), enable_features=True)
        parent = mgr.create()
        out = await mgr.spawn_teammate(parent.id, "alice", "worker", "do the thing")
        teammate = next(s for s in mgr.list() if s.id != parent.id)
        await teammate.spawn_task
        return parent, teammate, out

    parent, teammate, out = asyncio.run(main())
    assert "alice" in out
    assert teammate.workspace == parent.workspace        # shared board
    assert teammate.run_count >= 1                        # actually ran
    assert "spawn_teammate" not in teammate.agent.tools   # fork-bomb guard


# --- s18 worktrees ---------------------------------------------------------

def test_worktree_factory_falls_back_without_git(tmp_path):
    repo = tmp_path / "not-a-repo"
    factory = worktree_workspace_factory(repo)
    ws = factory("sess1")
    assert ws.exists()
    escaped = factory("../../escape")
    assert escaped.resolve().is_relative_to((repo / ".worktrees").resolve())


def test_worktree_factory_creates_branch_in_git_repo(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir()
    for args in (["init", "-q"], ["config", "user.email", "t@t"], ["config", "user.name", "t"]):
        subprocess.run(["git", *args], cwd=repo, check=True)
    (repo / "f.txt").write_text("x")
    subprocess.run(["git", "add", "."], cwd=repo, check=True)
    subprocess.run(["git", "commit", "-qm", "init"], cwd=repo, check=True)

    ws = worktree_workspace_factory(repo)("sess1")
    assert ws.exists() and (ws / ".git").exists()
    branch = subprocess.run(["git", "rev-parse", "--abbrev-ref", "HEAD"],
                            cwd=ws, capture_output=True, text=True).stdout.strip()
    assert branch == "wt/sess1"


# --- s19 MCP ---------------------------------------------------------------

def _docs_server():
    return InProcessMCP("docs", [{
        "name": "search",
        "description": "Search the docs.",
        "input_schema": {"type": "object", "properties": {"query": {"type": "string"}}, "required": ["query"]},
        "handler": lambda query: f"hits for {query}",
    }])


def test_mcp_register_and_call(tmp_path):
    async def main():
        agent, _ = _agent(tmp_path, FakeAsyncAnthropic())
        added = await register_mcp(agent, _docs_server())
        assert added == ["mcp__docs__search"]
        assert "mcp__docs__search" in agent.tools
        return await agent.tools.get("mcp__docs__search").run(
            type("C", (), {"agent": agent, "workspace": agent.workspace, "state": {}, "call": None})(), query="loops")

    assert "hits for loops" in asyncio.run(main())


def test_connect_mcp_tool_registers_remote_tools(tmp_path):
    reg = default_registry()
    install_mcp(reg, {"docs": _docs_server()})
    client = FakeAsyncAnthropic(responder=scripted([([tool("connect_mcp", name="docs")], "tool_use")]))
    agent, events = _agent(tmp_path, client, tools=reg)
    asyncio.run(agent.run("connect docs"))
    assert "mcp__docs__search" in agent.tools
    result = next(e for e in events if e["type"] == "tool_result")
    assert "mcp__docs__search" in result["output"]
