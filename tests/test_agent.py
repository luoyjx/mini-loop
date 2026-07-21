"""Offline tests for the agent core -- no API key, no network.

Everything runs against the deterministic FakeAsyncAnthropic. Async pieces are
driven with asyncio.run() so we need no pytest-asyncio plumbing.
"""

import asyncio
import time
from pathlib import Path

from mini_loop.agent import Agent, TodoManager, microcompact
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool, _last_result_text
from mini_loop.manager import SessionManager
from mini_loop.skills import SkillLoader
from mini_loop.tools import Toolset

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path, **over) -> Settings:
    base = dict(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR)
    base.update(over)
    return Settings(**base)


def _agent(tmp_path, client, **over):
    settings = _settings(tmp_path, **over)
    ws = settings.workspace_root / "sess"
    ws.mkdir(parents=True, exist_ok=True)
    events = []

    async def emit(e):
        events.append(e)

    agent = Agent(client=client, settings=settings, workspace=ws,
                  skills=SkillLoader(SKILLS_DIR), emit=emit)
    return agent, events


# --- the loop --------------------------------------------------------------

def test_basic_loop_runs_one_tool_then_finishes(tmp_path):
    client = FakeAsyncAnthropic()
    agent, events = _agent(tmp_path, client)
    final = asyncio.run(agent.run("hello"))

    assert final.startswith("Done.")
    assert client.calls == 2  # fresh-prompt turn + tool-result turn
    types = [e["type"] for e in events]
    assert [kind for kind in types if not kind.startswith("model_")] == [
        "assistant_text", "tool_use", "tool_result", "assistant_text",
    ]
    assert types.count("model_start") == types.count("model_end") == 2
    tool_use = next(event for event in events if event["type"] == "tool_use")
    tool_result = next(event for event in events if event["type"] == "tool_result")
    assert tool_use["name"] == "bash"
    assert "handled: hello" in tool_result["output"]


def test_max_turns_guard(tmp_path):
    # A responder that never stops calling tools would loop forever without the cap.
    def never_stop(kwargs):
        return [tool("bash", _id="t", command="echo loop")], "tool_use"

    client = FakeAsyncAnthropic(responder=never_stop)
    agent, events = _agent(tmp_path, client, max_turns=4)
    final = asyncio.run(agent.run("go"))

    assert client.calls == 4
    assert "stopped after 4 rounds" in final
    assert any(e["type"] == "error" for e in events)


# --- tools + sandbox -------------------------------------------------------

def test_workspace_sandbox(tmp_path):
    ws = tmp_path / "ws"
    ws.mkdir()
    ts = Toolset(ws)

    assert ts.run_write("../escape.txt", "x").startswith("Error")
    assert not (tmp_path / "escape.txt").exists()
    assert ts.run_read("../../etc/passwd").startswith("Error")

    assert ts.run_write("sub/a.txt", "hi").startswith("Wrote")
    assert ts.run_read("sub/a.txt") == "hi"
    assert ts.run_edit("sub/a.txt", "hi", "bye") == "Edited sub/a.txt"
    assert ts.run_read("sub/a.txt") == "bye"


def test_dispatch_offloads_blocking_tools(tmp_path):
    ws = tmp_path / "ws"
    ws.mkdir()
    ts = Toolset(ws)
    out = asyncio.run(ts.dispatch("write_file", {"path": "x.txt", "content": "yo"}))
    assert out.startswith("Wrote")
    assert asyncio.run(ts.dispatch("read_file", {"path": "x.txt"})) == "yo"
    assert asyncio.run(ts.dispatch("nope", {})).startswith("Unknown tool")


# --- TodoWrite -------------------------------------------------------------

def test_todo_manager_validation_and_render():
    todo = TodoManager()
    out = todo.update([
        {"content": "step one", "status": "completed", "activeForm": "doing one"},
        {"content": "step two", "status": "in_progress", "activeForm": "doing two"},
    ])
    assert "[x] step one" in out and "[>] step two <- doing two" in out
    assert "(1/2 completed)" in out
    assert todo.has_open_items()

    for bad in (
        [{"content": "", "status": "pending", "activeForm": "a"}],
        [{"content": "x", "status": "bogus", "activeForm": "a"}],
        [{"content": "a", "status": "in_progress", "activeForm": "a"},
         {"content": "b", "status": "in_progress", "activeForm": "b"}],
    ):
        try:
            todo.update(bad)
            assert False, "expected ValueError"
        except ValueError:
            pass


# --- compaction ------------------------------------------------------------

def test_microcompact_keeps_last_three():
    messages = []
    for i in range(5):
        messages.append({"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": f"id{i}", "content": "x" * 200}
        ]})
    cleared = microcompact(messages)
    assert cleared == 2
    results = [p for m in messages for p in m["content"] if p["type"] == "tool_result"]
    assert results[0]["content"] == "[cleared]"
    assert results[1]["content"] == "[cleared]"
    assert results[-1]["content"] == "x" * 200


def test_auto_compact_replaces_history(tmp_path):
    client = FakeAsyncAnthropic()
    # token_threshold tiny -> auto-compact fires on the very first loop pass.
    # (With a real threshold the post-compaction summary sits well below it, so
    # this only thrashes under the artificial threshold=1; bound it with max_turns.)
    agent, events = _agent(tmp_path, client, token_threshold=1, max_turns=3)
    asyncio.run(agent.run("compress me"))
    assert any(e["type"] == "compact" and e["kind"] == "auto" for e in events)
    # a transcript file was persisted under the workspace
    transcripts = list((agent.workspace / ".transcripts").glob("*.jsonl"))
    assert transcripts


# --- subagent (context isolation) -----------------------------------------

def test_subagent_delegation(tmp_path):
    def responder(kwargs):
        tools, msgs = kwargs.get("tools"), kwargs["messages"]
        last = msgs[-1]
        is_sub = "subagent" in (kwargs.get("system") or "")
        if not tools:
            return [text("[summary]")], "end_turn"
        if isinstance(last.get("content"), str):
            if is_sub:
                return [tool("bash", _id="t_sub", command="echo from-subagent")], "tool_use"
            return [tool("task", _id="t_task", prompt="go look around", agent_type="Explore")], "tool_use"
        if is_sub:
            return [text("subagent summary: found the bug")], "end_turn"
        return [text("main done: " + _last_result_text(last["content"]))], "end_turn"

    client = FakeAsyncAnthropic(responder=responder)
    agent, events = _agent(tmp_path, client)
    # The default system prompt mentions "subagent"; give the parent a plain one
    # so the responder's is_sub check only matches the child.
    agent.system = "Main orchestrator. Use the task tool to delegate exploration."
    final = asyncio.run(agent.run("investigate"))

    assert "subagent summary: found the bug" in final
    kinds = [e["type"] for e in events]
    assert "subagent_start" in kinds and "subagent_end" in kinds
    # the subagent emitted with a nested label / deeper depth
    assert any(e.get("depth", 0) >= 1 for e in events)


# --- skills ----------------------------------------------------------------

def test_skill_loader():
    loader = SkillLoader(SKILLS_DIR)
    assert "code_review" in loader.skills
    assert "code_review" in loader.descriptions()
    body = loader.load("code_review")
    assert body.startswith('<skill name="code_review">')
    assert "Correctness" in body
    assert loader.load("nope").startswith("Error: Unknown skill")


# --- the headline: many agents, concurrently -------------------------------

def test_sessions_run_concurrently(tmp_path):
    """10 sessions, each 2 LLM calls @50ms. Run concurrently they finish in
    ~one call's worth of wall time, not the ~1s a sequential run would take."""
    settings = _settings(tmp_path, max_concurrent_llm=50)

    async def main():
        client = FakeAsyncAnthropic(delay=0.05)
        mgr = SessionManager(settings, client, llm_semaphore=asyncio.Semaphore(50))
        sessions = [mgr.create() for _ in range(10)]
        t0 = time.perf_counter()
        finals = await asyncio.gather(*(s.run(f"task {i}") for i, s in enumerate(sessions)))
        return finals, time.perf_counter() - t0, client.calls

    finals, elapsed, calls = asyncio.run(main())
    assert len(finals) == 10 and all(f.startswith("Done.") for f in finals)
    assert calls == 20  # 2 per session
    assert elapsed < 0.5, f"expected concurrency (<0.5s), got {elapsed:.2f}s"


def test_sessions_are_isolated(tmp_path):
    """Each session gets its own workspace; a file written in one is invisible
    to another."""
    settings = _settings(tmp_path)

    async def main():
        client = FakeAsyncAnthropic()
        mgr = SessionManager(settings, client)
        a, b = mgr.create(), mgr.create()
        # write into A's workspace directly via its agent's toolset
        a.agent.toolset.run_write("secret.txt", "A-only")
        assert a.agent.toolset.run_read("secret.txt") == "A-only"
        assert b.agent.toolset.run_read("secret.txt").startswith("Error")  # not in B
        assert a.workspace != b.workspace

    asyncio.run(main())
