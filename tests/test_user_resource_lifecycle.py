"""Owner-scoped resources are bound before every Agent construction path."""

from __future__ import annotations

import asyncio
from types import SimpleNamespace

from fastapi.testclient import TestClient
import pytest

from mini_loop.auth import TokenAuth
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.identity import posture
from mini_loop.manager import SessionManager
from mini_loop.memory import MemoryStore
from mini_loop.server import create_app
from mini_loop.storage import SQLiteStateStore


TOKEN = "tok-alice-000000000000"


class _OwnerSkills:
    def __init__(self, owner: str) -> None:
        self.owner = owner

    def descriptions(self) -> str:
        return f"  - {self.owner}-only: resource for {self.owner}"

    def load(self, name: str) -> str:
        return f"{self.owner}:{name}"


class _RecordingResolver:
    def __init__(self, root) -> None:
        self.root = root
        self.calls: list[str] = []
        self.resolved: dict[str, SimpleNamespace] = {}

    def for_owner(self, owner: str):
        self.calls.append(owner)
        if owner not in self.resolved:
            root = self.root / owner
            self.resolved[owner] = SimpleNamespace(
                scope="user",
                skills=_OwnerSkills(owner),
                memory=MemoryStore(root / "memory"),
                root=root,
            )
        return self.resolved[owner]


def _settings(tmp_path, **overrides) -> Settings:
    values = {
        "fake_llm": True,
        "workspace_root": tmp_path / "workspaces",
        "skills_dir": tmp_path / "agent-skills",
        "spill_dir": None,
        "memory_root": None,
        "repo_root": None,
        "trajectory_enabled": False,
        "user_resources_root": None,
    }
    values.update(overrides)
    return Settings(**values)


def _manager(tmp_path, *, resolver=None, store=None, settings=None):
    return SessionManager(
        settings or _settings(tmp_path),
        FakeAsyncAnthropic(),
        state_store=store,
        user_resources=resolver,
    )


def test_user_resource_root_is_opt_in_and_builds_the_default_resolver(
    tmp_path, monkeypatch
):
    from mini_loop import LayeredSkillLoader, UserResourceResolver, UserResources

    assert LayeredSkillLoader is not None
    assert UserResourceResolver is not None
    assert UserResources is not None

    monkeypatch.delenv("MINILOOP_USER_RESOURCES_ROOT", raising=False)
    assert _settings(tmp_path).user_resources_root is None

    configured_root = tmp_path / "configured-users"
    monkeypatch.setenv("MINILOOP_USER_RESOURCES_ROOT", str(configured_root))
    settings = Settings(
        fake_llm=True,
        workspace_root=tmp_path / "configured-workspaces",
        skills_dir=tmp_path / "configured-agent-skills",
        spill_dir=None,
        memory_root=None,
        repo_root=None,
        trajectory_enabled=False,
    )
    manager = _manager(tmp_path, settings=settings)

    assert settings.user_resources_root == configured_root.resolve()
    assert isinstance(manager.user_resources, UserResourceResolver)
    assert list(configured_root.iterdir()) == []
    assert posture(manager)["user_resources"] is True
    assert list(configured_root.iterdir()) == []

    resources = manager.user_resources.for_owner("alice")
    assert isinstance(resources, UserResources)
    assert isinstance(resources.skills, LayeredSkillLoader)


def test_disabled_user_resources_keep_the_legacy_skills_and_memory(tmp_path):
    manager = _manager(tmp_path)
    session = manager.create(owner="alice")

    assert session.agent.skills is manager.skills
    assert session.agent.state["memory"] is manager.memory
    assert session.agent.state["resource_owner"] == "alice"
    assert posture(manager)["user_resources"] is False


def test_create_rejects_an_empty_owner_before_allocating_a_session(tmp_path):
    manager = _manager(tmp_path)

    with pytest.raises(ValueError, match="non-empty"):
        manager.create(owner="")

    assert manager.list() == []


def test_two_owners_are_resolved_before_their_agents_are_built(tmp_path):
    resolver = _RecordingResolver(tmp_path / "users")
    manager = _manager(tmp_path, resolver=resolver)

    alice = manager.create(owner="alice")
    bob = manager.create(owner="bob")

    assert resolver.calls == ["alice", "bob"]
    assert alice.agent.skills is resolver.resolved["alice"].skills
    assert bob.agent.skills is resolver.resolved["bob"].skills
    assert alice.agent.state["memory"] is resolver.resolved["alice"].memory
    assert bob.agent.state["memory"] is resolver.resolved["bob"].memory
    assert alice.agent.state["resource_owner"] == "alice"
    assert bob.agent.state["resource_owner"] == "bob"
    assert "alice-only" in alice.agent.system
    assert "bob-only" not in alice.agent.system
    assert "bob-only" in bob.agent.system
    assert "alice-only" not in bob.agent.system


def test_owner_is_recorded_before_first_run_and_restored_before_build(tmp_path):
    database = tmp_path / "state.db"
    store = SQLiteStateStore(database)
    first_resolver = _RecordingResolver(tmp_path / "first-users")
    first = _manager(tmp_path, resolver=first_resolver, store=store)
    session = first.create(owner="alice")
    session_id = session.id

    record = next(item for item in store.load_sessions() if item.session_id == session_id)
    assert record.owner == "alice"
    assert session.run_count == 0
    asyncio.run(first.stop())
    store.close()

    store = SQLiteStateStore(database)
    restored_resolver = _RecordingResolver(tmp_path / "restored-users")
    restored_manager = _manager(
        tmp_path, resolver=restored_resolver, store=store
    )
    restored = restored_manager.restore_sessions()

    assert restored_resolver.calls == ["alice"]
    assert restored[0].owner == "alice"
    assert restored[0].agent.state["resource_owner"] == "alice"
    assert restored[0].agent.skills is restored_resolver.resolved["alice"].skills
    asyncio.run(restored_manager.stop())
    store.close()

    store = SQLiteStateStore(database)
    cron_resolver = _RecordingResolver(tmp_path / "cron-users")
    cron_manager = _manager(tmp_path, resolver=cron_resolver, store=store)
    revived = cron_manager.restore_scheduled_session(session_id)

    assert cron_resolver.calls == ["alice"]
    assert revived.owner == "alice"
    assert revived.agent.state["resource_owner"] == "alice"
    assert revived.agent.skills is cron_resolver.resolved["alice"].skills
    asyncio.run(cron_manager.stop())
    store.close()


def test_teammate_inherits_parent_resources_before_build(tmp_path):
    async def scenario():
        resolver = _RecordingResolver(tmp_path / "users")
        manager = _manager(tmp_path, resolver=resolver)
        parent = manager.create(owner="alice")
        await manager.spawn_teammate(
            parent.id, "worker", "implementer", "stand by"
        )
        teammate = next(item for item in manager.list() if item.id != parent.id)
        try:
            # The child inherits the parent's immutable resource generation;
            # a publication between parent/child construction must not give
            # the teammate a catalogue the parent never saw.
            assert resolver.calls == ["alice"]
            assert teammate.owner == "alice"
            assert teammate.agent.state["resource_owner"] == "alice"
            assert teammate.agent.skills is resolver.resolved["alice"].skills
            assert teammate.agent.state["memory"] is resolver.resolved["alice"].memory
        finally:
            await manager.stop()

    asyncio.run(scenario())


def test_server_passes_the_authenticated_owner_into_create(tmp_path):
    resolver = _RecordingResolver(tmp_path / "users")
    manager = _manager(tmp_path, resolver=resolver)
    app = create_app(manager=manager)

    with TestClient(app) as client:
        app.state.auth = TokenAuth({TOKEN: "alice"})
        response = client.post(
            "/sessions",
            headers={"Authorization": f"Bearer {TOKEN}"},
            json={},
        )

        assert response.status_code == 200
        session = manager.get(response.json()["id"])
        assert resolver.calls == ["alice"]
        assert session is not None
        assert session.owner == "alice"
        assert session.agent.state["resource_owner"] == "alice"
