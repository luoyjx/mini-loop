"""The diagnostics seam and the session-query tool.

Diagnostics (P2-3): a provider interface a real LSP can fill later; the
built-in checks Python syntax only and SAYS SO in every result, so a clean
report cannot be over-trusted. Workspace-confined like every file tool.

Session query (P2-5): compaction replaces the live transcript with a
summary while the superseded epochs on disk remain the canonical record;
`transcript_search` makes them reachable again -- scoped to the calling
session by construction, masked because the store rows already are.
"""

import asyncio
import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.diagnostics import PythonSyntaxDiagnostics, install_diagnostics
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.registry import ToolCall, ToolContext, ToolRegistry
from mini_loop.session_query import search_transcript
from mini_loop.storage import SQLiteStateStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


# --- diagnostics -----------------------------------------------------------


def test_the_builtin_provider_finds_syntax_errors(tmp_path):
    root = tmp_path / "ws"
    root.mkdir()
    (root / "good.py").write_text("x = 1\n")
    (root / "bad.py").write_text("def broken(:\n")
    found = PythonSyntaxDiagnostics().diagnose(root, None)
    assert [d.path for d in found] == ["bad.py"]
    assert found[0].line == 1


def _diag_call(agent, **input_):
    registry = ToolRegistry()
    install_diagnostics(registry)
    tool = registry.get("diagnostics")
    ctx = ToolContext(agent=agent, workspace=agent.workspace, state=agent.state,
                      call=ToolCall(name="diagnostics", input=dict(input_), id="d1"))
    return asyncio.run(tool.run(ctx, **input_))


def _manager(tmp_path, **kwargs):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "root",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(),
        **kwargs,
    )


def test_the_tool_names_its_scope_and_confines_paths(tmp_path):
    agent = _manager(tmp_path).create().agent
    (agent.workspace / "bad.py").write_text("def broken(:\n")
    out = _diag_call(agent)
    assert "Python syntax only" in out  # scope named on EVERY result
    assert "bad.py:1" in out
    escape = _diag_call(agent, path="../../etc/passwd")
    assert escape.startswith("Error") and "escapes" in escape


# --- session query ---------------------------------------------------------


def test_search_reaches_superseded_epochs(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    store.append_messages("s1", [
        {"role": "user", "content": "the launch code word is xyzzy-plugh"},
    ], epoch=1)
    # Compaction started a new epoch; the live transcript no longer holds it.
    store.append_messages("s1", [
        {"role": "user", "content": "[Context compressed]"},
    ], epoch=2)

    found = search_transcript(store, "s1", "xyzzy")
    assert found and found[0]["epoch"] == 1
    assert "xyzzy-plugh" in found[0]["snippet"]
    store.close()


def test_search_is_bounded(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    store.append_messages("s1", [
        {"role": "user", "content": f"needle number {i}"} for i in range(200)
    ], epoch=1)
    found = search_transcript(store, "s1", "needle")
    from mini_loop.session_query import MAX_MATCHES

    assert len(found) == MAX_MATCHES
    store.close()


def test_the_tool_is_scoped_to_its_own_session(tmp_path):
    """The session id comes from server-owned state, never tool input."""

    store = SQLiteStateStore(tmp_path / "state.db")
    store.append_messages("victim", [
        {"role": "user", "content": "victim's private content"},
    ], epoch=1)
    manager = _manager(tmp_path, state_store=store)
    session = manager.create()
    agent = session.agent
    registry = agent.tools
    from mini_loop.session_query import install_session_query

    if registry.get("transcript_search") is None:
        install_session_query(registry)
    tool = registry.get("transcript_search")
    ctx = ToolContext(
        agent=agent, workspace=agent.workspace, state=agent.state,
        call=ToolCall(name="transcript_search",
                      input={"query": "private content"}, id="q1"),
    )
    out = asyncio.run(tool.run(ctx, query="private content"))
    assert "victim's private content" not in out  # sees only its own session
    store.close()
