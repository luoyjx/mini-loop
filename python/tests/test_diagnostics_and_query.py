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

    found = search_transcript(store, "s1", "xyzzy")["matches"]
    assert found and found[0]["epoch"] == 1
    assert "xyzzy-plugh" in found[0]["snippet"]
    store.close()


def test_search_is_bounded(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    store.append_messages("s1", [
        {"role": "user", "content": f"needle number {i}"} for i in range(200)
    ], epoch=1)
    found = search_transcript(store, "s1", "needle")["matches"]
    from mini_loop.session_query import MAX_MATCHES

    assert len(found) == MAX_MATCHES
    store.close()


def test_search_names_the_epochs_it_skipped(tmp_path):
    """Past the epoch cap, "No matches" must not read as "nothing anywhere".

    The tool used to claim "search every durable epoch" while silently
    scanning only the newest MAX_EPOCHS_SCANNED -- so the one query whose
    answer lived in epoch 1 of an old session got a clean, wrong report.
    """
    from mini_loop.session_query import MAX_EPOCHS_SCANNED

    store = SQLiteStateStore(tmp_path / "state.db")
    total = MAX_EPOCHS_SCANNED + 5
    store.append_messages("s1", [
        {"role": "user", "content": "the answer lives in epoch one"},
    ], epoch=1)
    for epoch in range(2, total + 1):
        store.append_messages("s1", [
            {"role": "user", "content": f"noise for epoch {epoch}"},
        ], epoch=epoch)

    result = search_transcript(store, "s1", "answer lives")
    assert result["matches"] == []  # epoch 1 really is out of reach
    assert result["epochs_skipped"] == 5
    assert result["first_epoch"] == 6 and result["current_epoch"] == total
    store.close()


def test_the_tool_renders_the_coverage_caveat(tmp_path):
    """The model-facing answer itself carries the partial-scan warning."""
    from mini_loop.session_query import MAX_EPOCHS_SCANNED, install_session_query

    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, state_store=store)
    session = manager.create()
    agent = session.agent
    for epoch in range(1, MAX_EPOCHS_SCANNED + 4):
        store.append_messages(session.id, [
            {"role": "user", "content": f"noise {epoch}"},
        ], epoch=epoch)
    registry = agent.tools
    if registry.get("transcript_search") is None:
        install_session_query(registry)
    tool = registry.get("transcript_search")
    ctx = ToolContext(
        agent=agent, workspace=agent.workspace, state=agent.state,
        call=ToolCall(name="transcript_search",
                      input={"query": "missing-needle"}, id="q2"),
    )
    out = asyncio.run(tool.run(ctx, query="missing-needle"))
    assert "No matches" in out
    assert "were not scanned" in out, (
        "a partial scan must not present a clean report"
    )


def test_a_full_scan_reports_no_caveat(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    store.append_messages("s1", [
        {"role": "user", "content": "plain history"},
    ], epoch=1)
    result = search_transcript(store, "s1", "anything")
    assert result["epochs_skipped"] == 0
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
