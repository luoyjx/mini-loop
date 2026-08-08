"""Prose claims that other code is built on, turned into assertions.

Last round found a docstring asserting an invariant the code had stopped
holding -- and everything written afterwards had been validated against that
sentence rather than against the code. The response is not to re-read comments;
it is to make the load-bearing ones executable.

These are not new behaviours. Each test quotes a claim that already exists in
the package and pins it, so the claim fails here when it stops being true
instead of surfacing three subsystems away. A sweep of 77 absolute statements
in this package found the great majority already sound; what is pinned below is
the subset something else actually depends on.
"""

import asyncio
import copy
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.audit import audit
from mini_loop.caching import DefaultCachePolicy
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.builtins import default_registry
from mini_loop.registry import Tool
from mini_loop.secrets import SecretRegistry
from mini_loop.storage import SQLiteStateStore, SessionRecord

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path, **kwargs):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
        **kwargs,
    )


def test_caching_does_not_mutate_the_live_transcript(tmp_path):
    """caching.py: "Nothing here mutates ``agent.messages``."

    Load-bearing twice over: the transcript is what gets persisted, and the
    rewrite detector treats any change to it as a compaction that needs a new
    epoch. Annotation leaking into it would both corrupt the record and churn
    epochs on every request.
    """
    messages = [
        {"role": "user", "content": [{"type": "text", "text": "hi"}]},
        {"role": "assistant", "content": [
            {"type": "tool_use", "id": "a", "name": "x", "input": {}}]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "a", "content": "out"}]},
    ]
    before = copy.deepcopy(messages)
    system, tools, annotated = DefaultCachePolicy().annotate(
        system="sys", tools=[], messages=messages
    )
    assert messages == before, "annotate wrote into the caller's transcript"
    assert annotated is not messages


def test_secrets_never_re_resolve_a_cached_value():
    """secrets.py: "Cached values are never re-resolved."

    The point is what happens *after* a rotation: the registry must keep masking
    the value it already handed out, or the old credential starts reappearing in
    output the moment the source changes.
    """
    calls = []

    def rotating():
        calls.append(1)
        return "ORIGINAL-VALUE-XYZ" if len(calls) == 1 else "ROTATED-VALUE-ABC"

    registry = SecretRegistry()
    registry.register("K", rotating)
    assert registry.mask("see ORIGINAL-VALUE-XYZ") == "see <secret-hidden>"
    later = registry.mask("see ORIGINAL-VALUE-XYZ and ROTATED-VALUE-ABC")
    assert "ORIGINAL-VALUE-XYZ" not in later
    assert len(calls) == 1, f"resolver was called {len(calls)}x"


def test_schema_upgrade_is_idempotent(tmp_path):
    """storage.py: "Additive migrations only; each step is idempotent."

    A migration that is not idempotent turns a crash mid-upgrade, or a second
    process opening the same file, into a corrupt schema.
    """
    store = SQLiteStateStore(tmp_path / "state.db")
    columns = lambda: {row["name"] for row in
                       store._db.execute("PRAGMA table_info(sessions)").fetchall()}
    before = columns()
    store._upgrade(1)
    store._upgrade(1)
    assert columns() == before
    store.close()


def test_message_sequence_has_no_gaps(tmp_path):
    """storage.py: appends "never interleave into a gap".

    Restore reads by sequence; a gap or a repeat silently truncates or
    duplicates a transcript.
    """
    store = SQLiteStateStore(tmp_path / "state.db")
    store.upsert_session(SessionRecord(
        session_id="sess", workspace="/w", system=None, created_at=0.0,
        run_count=0, status="idle", event_cursor=0,
    ))
    for index in range(5):
        store.append_messages("sess", [{"role": "user", "content": str(index)}], epoch=1)
    rows = [r["seq"] for r in
            store._db.execute("SELECT ordinal AS seq FROM messages WHERE session_id='sess' ORDER BY seq")]
    assert rows == sorted(rows) and len(set(rows)) == len(rows)
    assert max(rows) - min(rows) == len(rows) - 1, f"gap in {rows}"
    store.close()


# --- the one claim the sweep found missing ---------------------------------

def test_a_tool_that_writes_and_runs_concurrently_is_reported(tmp_path):
    """A `parallel_safe` claim on a mutating tool is unverifiable, so say so.

    `Tool` documents why readonly does not imply parallel_safe, and says nothing
    about the reverse -- which is the direction that loses data. It is not
    rejected: a tool whose writes go somewhere with its own concurrency control
    is legitimate. It must not be *silent*, which is the aggregate failure this
    harness keeps re-learning.

    Round 104: the audit reasons about `risk`, not `readonly` -- the single
    source of truth for "mutates" -- and splits severity, because a raced
    external call is worse than a raced local write.
    """
    registry = default_registry()
    assert not [t for t in (registry.get(n) for n in registry.names())
                if t.parallel_safe and t.risk != "read"], "a shipped tool regressed"

    registry.register(
        Tool("sync_remote", "probe", {"type": "object"}, lambda ctx, **kw: "",
             risk="write", parallel_safe=True),
        replace=True,
    )
    manager = _manager(tmp_path, tool_registry=registry)
    findings = {f.check: f for f in audit(manager, environ={"PATH": "/usr/bin"})}
    assert "concurrent-writers" in findings
    assert "sync_remote" in findings["concurrent-writers"].detail


def test_a_parallel_safe_external_tool_is_a_louder_finding(tmp_path):
    """Round 95's stated open item: risk drives severity. A parallel_safe
    external tool (two concurrent deploys) is `high`, not `medium`."""

    registry = default_registry()
    registry.register(
        Tool("deploy_concurrently", "probe", {"type": "object"},
             lambda ctx, **kw: "", risk="external", parallel_safe=True),
        replace=True,
    )
    manager = _manager(tmp_path, tool_registry=registry)
    findings = {f.check: f for f in audit(manager, environ={"PATH": "/usr/bin"})}

    assert "concurrent-side-effects" in findings
    assert findings["concurrent-side-effects"].severity == "high"
    assert "deploy_concurrently" in findings["concurrent-side-effects"].detail
    # An external tool is not a mere "writer" -- the two buckets are distinct.
    assert "deploy_concurrently" not in findings.get(
        "concurrent-writers", type("F", (), {"detail": ""})).detail


def test_an_unclassified_parallel_safe_tool_is_flagged_high(tmp_path):
    """No risk declared is gated as external by permissions (round 95); the
    audit must not let it slip through the parallel-safety net either."""

    registry = default_registry()
    registry.register(
        Tool("mystery_parallel", "probe", {"type": "object"},
             lambda ctx, **kw: "", parallel_safe=True),  # risk defaults to None
        replace=True,
    )
    manager = _manager(tmp_path, tool_registry=registry)
    findings = {f.check: f for f in audit(manager, environ={"PATH": "/usr/bin"})}

    assert "mystery_parallel" in findings["concurrent-side-effects"].detail


def test_a_clean_registry_draws_no_concurrency_finding(tmp_path):
    """The check must not fire on the shipped set, or it trains people to ignore it."""
    manager = _manager(tmp_path)
    checks = {f.check for f in audit(manager, environ={"PATH": "/usr/bin"})}
    assert "concurrent-writers" not in checks
