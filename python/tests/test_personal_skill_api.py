"""Authenticated preview/commit lifecycle for personal session skills."""

from __future__ import annotations

import asyncio
from dataclasses import dataclass

import pytest
from fastapi.testclient import TestClient

from mini_loop.auth import Principal, TokenAuth
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.manager import SessionManager
from mini_loop.run_context import UNTRUSTED, RunContext
from mini_loop.server import create_app
from mini_loop.session import LeaseLost
from mini_loop.skill_capture import (
    PERSONAL_SKILL_CAPTURE_SOURCE,
    PersonalSkillError,
)
from mini_loop.skills import SkillLoader
from mini_loop.storage import SQLiteStateStore
from mini_loop.user_resources import UserResourceResolver


ALICE = {"Authorization": "Bearer tok-alice"}
BOB = {"Authorization": "Bearer tok-bob"}


def _settings(tmp_path) -> Settings:
    return Settings(
        fake_llm=True,
        workspace_root=tmp_path / "workspaces",
        skills_dir=tmp_path / "agent-skills",
        spill_dir=None,
        memory_root=None,
        repo_root=None,
        trajectory_enabled=False,
        user_resources_root=None,
    )


@dataclass
class _Api:
    client: TestClient
    manager: SessionManager
    resolver: UserResourceResolver

    def create(self, headers=ALICE, *, mode: str = "interactive") -> str:
        response = self.client.post(
            "/sessions",
            headers=headers,
            json={"mode": mode},
        )
        assert response.status_code == 200
        return response.json()["id"]

    def preview(
        self,
        session_id: str,
        *,
        headers=ALICE,
        name: str = "session-helper",
        focus: str = "the reviewed workflow",
    ):
        return self.client.post(
            f"/sessions/{session_id}/personal-skills/preview",
            headers=headers,
            json={"name": name, "focus": focus},
        )

    def commit(self, session_id: str, draft: dict, *, headers=ALICE, digest=None):
        return self.client.post(
            f"/sessions/{session_id}/personal-skills/{draft['draft_id']}/commit",
            headers=headers,
            json={"digest": digest or draft["digest"]},
        )


@pytest.fixture
def personal_api(tmp_path, monkeypatch):
    import mini_loop.manager as manager_module

    resolver = UserResourceResolver(
        tmp_path / "users",
        SkillLoader(tmp_path / "agent-skills"),
    )
    manager = SessionManager(
        _settings(tmp_path),
        FakeAsyncAnthropic(thinking=False),
        user_resources=resolver,
    )

    async def deterministic_preview(agent, owner, name, focus=""):
        body = f"# Reviewed session workflow\n\n{focus or 'Use the reviewed workflow.'}"
        return agent.state["personal_skill_drafts"].add(
            owner=owner,
            session_id=agent.state["session_id"],
            name=name,
            description="A reviewed session workflow",
            body=body,
            evidence_indexes=(0,),
            coverage="current_epoch",
            omitted=0,
        )

    monkeypatch.setattr(
        manager_module,
        "preview_personal_skill",
        deterministic_preview,
    )
    app = create_app(manager=manager)
    with TestClient(app) as client:
        app.state.auth = TokenAuth(
            {"tok-alice": "alice", "tok-bob": "bob"}
        )
        yield _Api(client, manager, resolver)


def _detail_code(response) -> str:
    return response.json()["detail"]["code"]


def test_request_authentication_is_frozen_before_owner_and_capture_checks(
    personal_api,
):
    class ScriptedAuth:
        configured = True

        def __init__(self):
            self.calls = 0

        def authenticate(self, _authorization):
            self.calls += 1
            return Principal("alice" if self.calls == 1 else "bob")

    api = personal_api
    session_id = api.manager.create(owner="alice").id
    auth = ScriptedAuth()
    api.client.app.state.auth = auth

    response = api.client.post(
        f"/sessions/{session_id}/messages",
        headers=ALICE,
        json={"message": "one authority for the entire request"},
    )

    assert response.status_code == 200
    assert auth.calls == 1


def test_owner_previews_without_writing_then_commits_exact_next_session_skill(
    personal_api,
):
    api = personal_api
    session_id = api.create()
    current = api.manager.get(session_id)
    assert current is not None and current.agent is not None
    original_skills = current.agent.skills
    skill_file = (
        api.resolver.for_owner("alice").root
        / "skills"
        / "session-helper"
        / "SKILL.md"
    )

    preview = api.preview(session_id, focus="Keep the exact reviewed steps.")

    assert preview.status_code == 200
    draft = preview.json()
    assert draft["body"].endswith("Keep the exact reviewed steps.")
    assert not skill_file.exists(), "preview must have zero publication side effects"

    committed = api.commit(session_id, draft)

    assert committed.status_code == 200
    receipt = committed.json()
    assert receipt["activation"] == "next_session"
    assert receipt["name"] == draft["name"]
    assert receipt["digest"] == draft["digest"]
    assert len(receipt["content_digest"]) == 64
    assert receipt["content_digest"] != receipt["digest"]
    saved = skill_file.read_text()
    assert draft["body"] in saved
    assert "Keep the exact reviewed steps." in saved

    # Publication replaces the resolver bundle, never a live agent's loader.
    assert current.agent.skills is original_skills
    assert current.agent.skills.load("user:session-helper").startswith("Error:")
    next_session_id = api.create()
    next_session = api.manager.get(next_session_id)
    assert next_session is not None and next_session.agent is not None
    loaded = next_session.agent.skills.load("user:session-helper")
    assert draft["body"] in loaded
    consumed = api.commit(session_id, draft)
    assert consumed.status_code == 404
    assert _detail_code(consumed) == "draft_not_found"


def test_authenticated_message_records_only_the_human_and_final_turn(personal_api):
    api = personal_api
    session_id = api.create()

    response = api.client.post(
        f"/sessions/{session_id}/messages",
        headers=ALICE,
        json={"message": "turn this reviewed procedure into a reusable skill"},
    )

    assert response.status_code == 200
    session = api.manager.get(session_id)
    assert session is not None and session.agent is not None
    assert session.agent.state["personal_skill_turns"] == [
        {
            "role": "user",
            "content": "turn this reviewed procedure into a reusable skill",
        },
        {"role": "assistant", "content": response.json()["final"]},
    ]


def test_managed_preview_fails_closed_when_the_ledger_key_is_missing(personal_api):
    api = personal_api
    session_id = api.create()
    session = api.manager.get(session_id)
    assert session is not None and session.agent is not None
    session.agent.state.pop("personal_skill_turns")
    session.agent.messages.append(
        {"role": "user", "content": "RAW_TRANSCRIPT_INJECTOR_CANARY"}
    )

    response = api.preview(session_id)

    assert response.status_code == 503
    assert _detail_code(response) == "capture_source_unavailable"


def test_routes_require_exact_authenticated_owner_and_enabled_resources(
    personal_api,
    tmp_path,
):
    api = personal_api
    alice_session = api.create()

    stranger = api.preview(alice_session, headers=BOB)
    assert stranger.status_code == 404

    disabled = SessionManager(_settings(tmp_path / "disabled"), FakeAsyncAnthropic())
    disabled_app = create_app(manager=disabled)
    with TestClient(disabled_app) as client:
        disabled_app.state.auth = TokenAuth({"tok-alice": "alice"})
        session_id = client.post("/sessions", headers=ALICE, json={}).json()["id"]
        response = client.post(
            f"/sessions/{session_id}/personal-skills/preview",
            headers=ALICE,
            json={"name": "disabled"},
        )
        assert response.status_code == 404
        assert _detail_code(response) == "personal_skills_disabled"

    anonymous_manager = SessionManager(
        _settings(tmp_path / "anonymous"),
        FakeAsyncAnthropic(),
        user_resources=UserResourceResolver(
            tmp_path / "anonymous-users",
            SkillLoader(tmp_path / "anonymous-agent-skills"),
        ),
    )
    anonymous_app = create_app(manager=anonymous_manager)
    with TestClient(anonymous_app) as client:
        session_id = client.post("/sessions", json={}).json()["id"]
        response = client.post(
            f"/sessions/{session_id}/personal-skills/preview",
            json={"name": "anonymous"},
        )
        assert response.status_code == 403
        assert _detail_code(response) == "authenticated_owner_required"


def test_readonly_wrong_digest_and_cross_session_fail_without_consuming_draft(
    personal_api,
):
    api = personal_api
    first = api.create(mode="readonly")
    second = api.create()
    draft = api.preview(first, name="guarded-skill").json()
    skill_file = (
        api.resolver.for_owner("alice").root
        / "skills"
        / "guarded-skill"
        / "SKILL.md"
    )

    readonly = api.commit(first, draft)
    assert readonly.status_code == 403
    assert _detail_code(readonly) == "readonly_session"
    assert not skill_file.exists()

    api.client.post(f"/sessions/{first}/mode", headers=ALICE,
                    json={"mode": "interactive"})
    wrong_digest = api.commit(first, draft, digest="0" * 64)
    assert wrong_digest.status_code == 409
    assert _detail_code(wrong_digest) == "draft_digest_mismatch"
    assert not skill_file.exists()

    cross_session = api.commit(second, draft)
    assert cross_session.status_code == 404
    assert _detail_code(cross_session) == "draft_not_found"
    assert not skill_file.exists()

    accepted = api.commit(first, draft)
    assert accepted.status_code == 200
    assert skill_file.exists()


def test_personal_skill_requests_reject_owner_path_and_body(personal_api):
    api = personal_api
    session_id = api.create()

    preview = api.client.post(
        f"/sessions/{session_id}/personal-skills/preview",
        headers=ALICE,
        json={"name": "safe-name", "owner": "bob", "path": "../escape"},
    )
    assert preview.status_code == 422

    draft = api.preview(session_id, name="safe-name").json()
    commit = api.client.post(
        f"/sessions/{session_id}/personal-skills/{draft['draft_id']}/commit",
        headers=ALICE,
        json={"digest": draft["digest"], "body": "caller-controlled"},
    )
    assert commit.status_code == 422

    invalid_name = api.client.post(
        f"/sessions/{session_id}/personal-skills/preview",
        headers=ALICE,
        json={"name": "../escape"},
    )
    assert invalid_name.status_code == 422

    invalid_digest = api.client.post(
        f"/sessions/{session_id}/personal-skills/{draft['draft_id']}/commit",
        headers=ALICE,
        json={"digest": "not-a-digest"},
    )
    assert invalid_digest.status_code == 422


def test_publication_failure_is_masked_and_does_not_consume_draft(
    personal_api,
    monkeypatch,
):
    api = personal_api
    session_id = api.create()
    draft = api.preview(session_id, name="retryable-skill").json()
    publish = api.resolver.publish_skill

    def fail_with_host_detail(*_args, **_kwargs):
        raise RuntimeError("/private/tenant/root must never reach the response")

    monkeypatch.setattr(api.resolver, "publish_skill", fail_with_host_detail)
    failed = api.commit(session_id, draft)

    assert failed.status_code == 500
    assert _detail_code(failed) == "publication_failed"
    assert "/private/tenant/root" not in failed.text

    monkeypatch.setattr(api.resolver, "publish_skill", publish)
    retried = api.commit(session_id, draft)
    assert retried.status_code == 200
    assert retried.json()["idempotent"] is False


def test_lease_loss_returns_stable_non_sensitive_error(personal_api, monkeypatch):
    api = personal_api
    session_id = api.create()
    session = api.manager.get(session_id)
    assert session is not None

    def lose_lease():
        raise LeaseLost("holder and process identifiers must remain private")

    monkeypatch.setattr(session, "_require_lease", lose_lease)
    response = api.preview(session_id, name="never-generated")

    assert response.status_code == 409
    assert _detail_code(response) == "session_lease_lost"
    assert "holder and process" not in response.text


def test_teammate_inherits_parent_snapshot_after_publication(tmp_path):
    async def scenario():
        resolver = UserResourceResolver(
            tmp_path / "users",
            SkillLoader(tmp_path / "agent-skills"),
        )
        manager = SessionManager(
            _settings(tmp_path),
            FakeAsyncAnthropic(thinking=False),
            user_resources=resolver,
        )
        parent = manager.create(owner="alice")
        assert parent.agent is not None
        inherited_skills = parent.agent.skills
        inherited_memory = parent.agent.state["memory"]
        draft = manager.personal_skill_drafts.add(
            owner="alice",
            session_id=parent.id,
            name="published-later",
            description="Published after the parent snapshot",
            body="# New generation only",
            evidence_indexes=(0,),
            coverage="current_epoch",
            omitted=0,
        )
        await manager.commit_personal_skill(
            parent.id,
            "alice",
            draft.draft_id,
            draft.digest,
        )

        async def no_initial_run(*_args, **_kwargs):
            return ""

        manager._initial_teammate_run = no_initial_run
        await manager.spawn_teammate(
            parent.id,
            "worker",
            "implementer",
            "stand by",
        )
        teammate = next(
            session for session in manager.list() if session.id != parent.id
        )
        try:
            assert teammate.agent is not None
            assert teammate.agent.skills is inherited_skills
            assert teammate.agent.state["memory"] is inherited_memory
            assert teammate.agent.skills.load("user:published-later").startswith(
                "Error:"
            )
            future = manager.create(owner="alice")
            assert future.agent is not None
            assert "# New generation only" in future.agent.skills.load(
                "user:published-later"
            )
        finally:
            await manager.stop()

    asyncio.run(scenario())


def test_commit_queued_behind_session_lock_refuses_after_delete(tmp_path):
    async def scenario():
        resolver = UserResourceResolver(
            tmp_path / "users",
            SkillLoader(tmp_path / "agent-skills"),
        )
        manager = SessionManager(
            _settings(tmp_path),
            FakeAsyncAnthropic(thinking=False),
            user_resources=resolver,
        )
        session = manager.create(owner="alice")
        draft = manager.personal_skill_drafts.add(
            owner="alice",
            session_id=session.id,
            name="must-not-publish",
            description="A draft queued behind a live turn",
            body="# Never publish after deletion",
            evidence_indexes=(0,),
            coverage="authenticated_turns",
            omitted=0,
        )
        skill_file = (
            resolver.for_owner("alice").root
            / "skills"
            / "must-not-publish"
            / "SKILL.md"
        )

        await session.lock.acquire()
        task = asyncio.create_task(
            manager.commit_personal_skill(
                session.id,
                "alice",
                draft.draft_id,
                draft.digest,
            )
        )
        await asyncio.sleep(0)
        assert manager.delete(session.id, remove_workspace=False) is True
        session.lock.release()
        try:
            with pytest.raises(PersonalSkillError) as caught:
                await task
            assert caught.value.code == "session_not_found"
            assert caught.value.status_code == 404
            assert not skill_file.exists()
        finally:
            if session.lock.locked():
                session.lock.release()
            await manager.stop()

    asyncio.run(scenario())


def test_terminal_failure_never_admits_the_turn_to_skill_capture(tmp_path):
    async def scenario():
        manager = SessionManager(
            _settings(tmp_path),
            FakeAsyncAnthropic(thinking=False),
            user_resources=UserResourceResolver(
                tmp_path / "users",
                SkillLoader(tmp_path / "agent-skills"),
            ),
        )
        session = manager.create(owner="alice")
        assert session.agent is not None

        async def fail_terminal(*_args, **_kwargs):
            raise RuntimeError("terminal persistence failed")

        session._finish_trajectory = fail_terminal
        context = RunContext(
            origin="authenticated_http",
            actor_id="alice",
            channel="http",
            authority=UNTRUSTED,
            approved_capabilities=(PERSONAL_SKILL_CAPTURE_SOURCE,),
        )
        try:
            with pytest.raises(RuntimeError, match="terminal persistence failed"):
                await session.run("must not become skill evidence", context)
            assert session.agent.state["personal_skill_turns"] == []
        finally:
            await manager.stop()

    asyncio.run(scenario())


def test_restored_history_is_excluded_and_counted_as_omitted_source(tmp_path):
    async def scenario():
        database = tmp_path / "state.db"
        first_store = SQLiteStateStore(database)
        first = SessionManager(
            _settings(tmp_path),
            FakeAsyncAnthropic(thinking=False),
            state_store=first_store,
            user_resources=UserResourceResolver(
                tmp_path / "users",
                SkillLoader(tmp_path / "agent-skills"),
            ),
        )
        session = first.create(owner="alice")
        await session.run("historical reviewed procedure")
        await first.stop()
        first_store.close()

        second_store = SQLiteStateStore(database)
        second = SessionManager(
            _settings(tmp_path),
            FakeAsyncAnthropic(thinking=False),
            state_store=second_store,
            user_resources=UserResourceResolver(
                tmp_path / "users",
                SkillLoader(tmp_path / "agent-skills"),
            ),
        )
        restored = second.restore_sessions()[0]
        try:
            assert restored.agent is not None
            assert restored.agent.messages
            assert restored.agent.state["personal_skill_turns"] == []
            assert restored.agent.state["personal_skill_turns_omitted"] == len(
                restored.agent.messages
            )
        finally:
            await second.stop()
            second_store.close()

    asyncio.run(scenario())
