"""Workspace binding: a session can work IN an existing checkout.

The mined corpus (docs/RSI_RESEARCH_AND_PLAN.md §5) put the fence's cost
on the table: 97% of shell commands re-establishing a cwd with a `cd`
prefix and 64 of 66 read_file errors being absolute paths refused at the
boundary -- the shape of work that lives somewhere other than the scratch
directory the manager made. Binding gives "work on this checkout" a
workspace that IS the checkout.

The boundary choice is the operator's, never a caller's: binding is off
until MINILOOP_BINDABLE_ROOTS names the directories sessions may bind to,
a bind must resolve inside one of them (symlinks judged by where they
land), the manager's own root is refused, and a bound workspace is never
reclaimed by delete -- it is the operator's repository lent to the session.
"""

import asyncio
import os
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.manager import WorkspaceBindingError
from mini_loop.storage import SQLiteStateStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path, **overrides):
    return Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                    skills_dir=SKILLS, spill_dir=None, **overrides)


def _checkout(tmp_path, name="repo"):
    src = tmp_path / "src"
    checkout = src / name
    checkout.mkdir(parents=True, exist_ok=True)
    (checkout / "README.md").write_text("bound checkout contents\n")
    return src, checkout


def _tool_results(session):
    out = []
    for message in session.agent.messages:
        content = message.get("content")
        if not isinstance(content, list):
            continue
        for block in content:
            if isinstance(block, dict) and block.get("type") == "tool_result":
                out.append(str(block.get("content")))
    return out


def test_a_bound_session_works_in_the_checkout(tmp_path):
    src, checkout = _checkout(tmp_path)
    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("read_file", _id="r1", path="README.md")], "tool_use"),
        ([text("read it")], "end_turn"),
    ]))
    manager = SessionManager(_settings(tmp_path, bindable_roots=(src,)), client)
    session = manager.create(workspace=checkout)

    assert session.workspace == checkout.resolve()
    assert session.workspace_bound is True
    assert session.info()["workspace_bound"] is True
    assert session.agent.workspace == checkout.resolve()
    asyncio.run(session.run("read the readme"))
    assert any("bound checkout contents" in r for r in _tool_results(session)), (
        "a relative read resolves inside the checkout, not inside scratch"
    )
    assert not (tmp_path / "ws" / session.id).exists(), (
        "no scratch directory is made for a bound session"
    )


def test_binding_is_off_until_the_operator_names_roots(tmp_path):
    _, checkout = _checkout(tmp_path)
    manager = SessionManager(_settings(tmp_path), FakeAsyncAnthropic())
    assert manager.settings.bindable_roots == ()
    with pytest.raises(WorkspaceBindingError) as refused:
        manager.create(workspace=checkout)
    assert refused.value.status == 403
    assert "MINILOOP_BINDABLE_ROOTS" in str(refused.value)
    # An ordinary create is untouched by the feature being off.
    assert manager.create().workspace_bound is False


def test_the_roots_are_the_boundary(tmp_path):
    src, checkout = _checkout(tmp_path)
    settings = _settings(tmp_path, bindable_roots=(src,))
    manager = SessionManager(settings, FakeAsyncAnthropic())

    elsewhere = tmp_path / "elsewhere"
    elsewhere.mkdir()
    with pytest.raises(WorkspaceBindingError, match="outside every bindable root"):
        manager.create(workspace=elsewhere)

    # A symlink inside a root is judged by where it lands.
    link = src / "escape"
    os.symlink(elsewhere, link)
    with pytest.raises(WorkspaceBindingError, match="outside every bindable root"):
        manager.create(workspace=link)

    # Policy answers before existence, so a caller cannot probe which
    # paths exist outside the roots.
    with pytest.raises(WorkspaceBindingError) as refused:
        manager.create(workspace=tmp_path / "elsewhere" / "nope")
    assert refused.value.status == 403

    # Inside a root but not a directory: 400, the caller's mistake.
    with pytest.raises(WorkspaceBindingError) as missing:
        manager.create(workspace=src / "missing")
    assert missing.value.status == 400
    with pytest.raises(WorkspaceBindingError) as not_dir:
        manager.create(workspace=checkout / "README.md")
    assert not_dir.value.status == 400


def test_the_managers_own_root_is_never_bindable(tmp_path):
    """Even when a bindable root contains it: workspace_root holds every
    session's private scratch, trajectories, memory and mailboxes."""
    settings = _settings(tmp_path, bindable_roots=(tmp_path,))
    manager = SessionManager(settings, FakeAsyncAnthropic())
    other = manager.create()
    with pytest.raises(WorkspaceBindingError, match="manager's own"):
        manager.create(workspace=other.workspace)
    with pytest.raises(WorkspaceBindingError, match="manager's own"):
        manager.create(workspace=settings.workspace_root)


def test_deleting_a_bound_session_leaves_the_checkout_alone(tmp_path):
    src, checkout = _checkout(tmp_path)
    manager = SessionManager(_settings(tmp_path, bindable_roots=(src,)),
                             FakeAsyncAnthropic())
    bound = manager.create(workspace=checkout)
    scratch = manager.create()
    scratch_dir = scratch.workspace
    assert scratch_dir.is_dir()

    assert manager.delete(bound.id) is True
    assert manager.delete(scratch.id) is True
    assert (checkout / "README.md").read_text() == "bound checkout contents\n", (
        "delete reclaimed the operator's checkout as if it were scratch"
    )
    assert not scratch_dir.exists(), "scratch is still reclaimed"


def test_a_teammate_inherits_the_binding(tmp_path):
    """The parent that bound the checkout may be deleted first; the child
    is then the last session standing on it and must still not reclaim it."""
    src, checkout = _checkout(tmp_path)

    async def main():
        settings = _settings(tmp_path, bindable_roots=(src,),
                             team_idle_poll=0.01, team_idle_timeout=0.2)
        manager = SessionManager(settings, FakeAsyncAnthropic(),
                                 enable_features=True)
        lead = manager.create(workspace=checkout)
        await manager.spawn_teammate(lead.id, "alice", "worker", "stand by")
        teammate = manager.teammate_session(lead.id, "alice")
        await teammate.spawn_task
        inherited = teammate.workspace_bound
        manager.delete(lead.id)
        manager.delete(teammate.id)
        await manager.stop()
        return inherited

    assert asyncio.run(main()) is True
    assert (checkout / "README.md").exists()


def test_the_binding_survives_a_restart(tmp_path):
    src, checkout = _checkout(tmp_path)
    db = tmp_path / "state.db"

    first = SessionManager(_settings(tmp_path, bindable_roots=(src,)),
                           FakeAsyncAnthropic(), state_store=SQLiteStateStore(db))
    session = first.create(workspace=checkout)
    # Every refresh rewrites the row; the flag must ride along.
    session._persist_session_record()
    sid = session.id
    asyncio.run(first.stop())

    record = next(r for r in SQLiteStateStore(db).load_sessions()
                  if r.session_id == sid)
    assert record.workspace_bound is True
    assert record.workspace == str(checkout.resolve())

    second = SessionManager(_settings(tmp_path, bindable_roots=(src,)),
                            FakeAsyncAnthropic(), state_store=SQLiteStateStore(db))
    (restored,) = [s for s in second.restore_sessions() if s.id == sid]
    assert restored.workspace_bound is True
    assert restored.workspace == checkout.resolve()
    second.delete(sid)
    assert (checkout / "README.md").exists(), (
        "a restart forgot the binding and delete reclaimed the checkout"
    )


def test_a_scheduled_run_resumes_in_the_bound_checkout(tmp_path):
    """"Run the tests here nightly" means here: a cron job on a bound
    session resurrects it in the checkout, not in fresh scratch."""
    src, checkout = _checkout(tmp_path)
    db = tmp_path / "state.db"
    first = SessionManager(_settings(tmp_path, bindable_roots=(src,)),
                           FakeAsyncAnthropic(), state_store=SQLiteStateStore(db))
    sid = first.create(workspace=checkout).id
    asyncio.run(first.stop())

    second = SessionManager(_settings(tmp_path, bindable_roots=(src,)),
                            FakeAsyncAnthropic(), state_store=SQLiteStateStore(db))
    resumed = second.restore_scheduled_session(sid)
    assert resumed.workspace == checkout.resolve()
    assert resumed.workspace_bound is True
    assert not (tmp_path / "ws" / sid).exists()


def test_the_http_edge_binds_and_refuses(tmp_path):
    from fastapi.testclient import TestClient
    from mini_loop.server import create_app

    src, checkout = _checkout(tmp_path)
    manager = SessionManager(_settings(tmp_path, bindable_roots=(src,)),
                             FakeAsyncAnthropic())
    with TestClient(create_app(manager=manager)) as client:
        assert client.get("/healthz").json()["workspace_binding"] is True
        created = client.post("/sessions", json={"workspace": str(checkout)})
        assert created.status_code == 200, created.text
        assert created.json()["workspace_bound"] is True
        assert created.json()["workspace"] == str(checkout.resolve())

        outside = client.post("/sessions", json={"workspace": str(tmp_path)})
        assert outside.status_code == 403
        assert "outside every bindable root" in outside.json()["detail"]

        missing = client.post("/sessions", json={"workspace": str(src / "x")})
        assert missing.status_code == 400

        plain = client.post("/sessions", json={})
        assert plain.status_code == 200 and plain.json()["workspace_bound"] is False

    off = SessionManager(_settings(tmp_path / "off"), FakeAsyncAnthropic())
    with TestClient(create_app(manager=off)) as client:
        assert client.get("/healthz").json()["workspace_binding"] is False
        refused = client.post("/sessions", json={"workspace": str(checkout)})
        assert refused.status_code == 403
