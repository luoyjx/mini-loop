"""Decisions are usable through real turns and retain the harness boundaries."""

import asyncio
import json
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from mini_loop import (
    DecisionError, DecisionResult, SessionManager, Settings, default_registry, full_registry,
    install_decisions,
)
from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
from mini_loop.identity import dump_config
from mini_loop.registry import ToolContext
from mini_loop.secrets import SecretRegistry
from mini_loop.server import create_app
from mini_loop.storage import SQLiteStateStore

SKILLS = Path(__file__).resolve().parent.parent / "skills"
QUESTIONS = {"urgent": {"type": "noul", "instructions": "Is this urgent?"}}


class RecordingProvider:
    def __init__(self):
        self.requests = []

    async def evaluate(self, request):
        self.requests.append(request.to_dict())
        return DecisionResult(
            provider="test-provider", model="test-decision-v1",
            answers={"urgent": {"type": "noul", "noul": 0.9}},
            usage={"input_tokens": 13, "output_tokens": 3},
            probability_source="llm_estimate",
        )


def _settings(tmp_path, **kwargs):
    return Settings(
        workspace_root=tmp_path / "workspaces", skills_dir=SKILLS,
        fake_llm=True, spill_dir=None, **kwargs,
    )


def _manager(tmp_path, provider, **kwargs):
    registry = default_registry()
    install_decisions(registry, provider)

    def responder(request):
        if isinstance(request["messages"][-1]["content"], str):
            return [tool("decision", state=request["messages"][-1]["content"], questions=QUESTIONS)], "tool_use"
        return [text("Decision recorded; no action executed.")], "end_turn"

    return SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(responder),
        tool_registry=registry, **kwargs,
    )


def test_decision_is_opt_in_and_composes_with_full_registry(tmp_path):
    assert "decision" not in default_registry()
    assert "decision" not in full_registry()
    assert "decision" in full_registry(decisions=True)
    assert "decision" in full_registry(decision_provider=RecordingProvider())
    manager = SessionManager(_settings(tmp_path, decision_mode="llm"), FakeAsyncAnthropic())
    assert "decision" in manager.create().agent.tools
    assert manager.tool_registry.get("decision").risk == "external"


def test_configured_llm_decision_completes_a_real_tool_turn(tmp_path):
    requests = []

    def responder(request):
        requests.append(request)
        if request.get("tools") == []:
            body = json.loads(request["messages"][0]["content"])
            assert body == {"state": "urgent", "questions": QUESTIONS}
            return [text(json.dumps({"distributions": {"urgent": {"true": 0.8, "false": 0.2}}}))], "end_turn"
        if isinstance(request["messages"][-1]["content"], str):
            return [tool("decision", state="urgent", questions=QUESTIONS)], "tool_use"
        result = json.loads(request["messages"][-1]["content"][0]["content"])
        assert result["answers"]["urgent"]["noul"] == 0.8
        assert result["probability_source"] == "llm_estimate"
        return [text("Escalate this urgent request.")], "end_turn"

    manager = SessionManager(
        _settings(tmp_path, decision_mode="llm"), FakeAsyncAnthropic(responder),
    )
    session = manager.create(permission_mode="auto")
    assert asyncio.run(session.run("Evaluate urgency")) == "Escalate this urgent request."
    assert len(requests) == 3
    spans = [e for e in session._backlog if e["type"] == "model_end" and e.get("purpose") == "decision"]
    assert len(spans) == 1  # Child metering is not duplicated by the tool wrapper.
    assert spans[0].get("parent_message_id")
    assert session.agent.state.get("recovery_model") is None


@pytest.mark.parametrize("mode", ["readonly", "interactive"])
def test_permission_refusal_never_calls_provider(tmp_path, mode):
    provider = RecordingProvider()
    # No broker wait: a bare default hook refuses external effects headlessly.
    from mini_loop.permissions import default_hooks

    manager = _manager(tmp_path, provider, hooks=default_hooks())
    session = manager.create(permission_mode=mode)
    asyncio.run(session.run("Urgent"))
    assert not provider.requests
    results = [e for e in session._backlog if e["type"] == "tool_result"]
    assert results and "Permission denied" in str(results)
    assert not any(e["type"] == "decision_completed" for e in session._backlog)


def test_api_turns_return_decisions_and_record_usage_without_cross_session_state(tmp_path):
    provider = RecordingProvider()
    manager = _manager(tmp_path, provider)
    with TestClient(create_app(manager=manager)) as client:
        for evidence in ("first evidence", "second evidence"):
            response = client.post("/sessions", json={"mode": "auto"})
            assert response.status_code == 200
            sid = response.json()["id"]
            reply = client.post(f"/sessions/{sid}/messages", json={"message": evidence})
            assert reply.status_code == 200, reply.text
            session = manager.get(sid)
            completed = [e for e in session._backlog if e["type"] == "decision_completed"]
            assert completed[0]["provider"] == "test-provider"
            spans = [e for e in session._backlog if e["type"] == "model_end" and e.get("purpose") == "decision"]
            assert spans[0]["usage"] == {"input_tokens": 13, "output_tokens": 3}
            assert spans[0]["served_model"] == "test-decision-v1"
            assert "0.9" in json.dumps(session.agent.messages, default=str)
    assert [r["state"] for r in provider.requests] == ["first evidence", "second evidence"]


def test_masking_precedes_remote_request_and_persistence(tmp_path):
    secret = 'clé-secret-"中文"-123'
    secrets = SecretRegistry()
    secrets.register("test", secret)
    store = SQLiteStateStore(tmp_path / "state.db")
    provider = RecordingProvider()
    manager = _manager(tmp_path, provider, secrets=secrets, state_store=store)
    session = manager.create(permission_mode="auto")
    asyncio.run(session.run(secret))
    assert secret not in json.dumps(provider.requests, ensure_ascii=False)
    events = store.load_events(session.id)
    assert any(e["type"] == "decision_completed" for e in events)
    assert secret not in json.dumps(events, ensure_ascii=False)
    assert secret not in json.dumps(store.load_messages(session.id), ensure_ascii=False)
    asyncio.run(manager.stop())
    store.close()
    reopened = SQLiteStateStore(tmp_path / "state.db")
    restored = _manager(tmp_path, provider, secrets=secrets, state_store=reopened)
    restored.restore_sessions()
    assert "0.9" in json.dumps(restored.get(session.id).agent.messages)
    assert any(e["type"] == "decision_completed" for e in reopened.load_events(session.id))
    restored.delete(session.id)
    assert not reopened.load_events(session.id)
    asyncio.run(restored.stop())
    reopened.close()


def test_bad_request_and_bad_answer_do_not_become_success(tmp_path):
    provider = RecordingProvider()
    manager = _manager(tmp_path, provider)
    agent = manager.create().agent
    ctx = ToolContext(agent, agent.workspace, agent.state)
    installed = agent.tools.get("decision")
    with pytest.raises(DecisionError, match="Invalid decision request"):
        asyncio.run(installed.run(ctx, state="x", questions={}))
    assert not provider.requests
    with pytest.raises(DecisionError, match="Decision failed"):
        asyncio.run(installed.run(ctx, state="x", questions={"other": QUESTIONS["urgent"]}))
    assert not any(e["type"] == "decision_completed" for e in agent.state["session"]._backlog)


def test_invalid_provider_answer_is_a_failed_tool_action(tmp_path):
    class InvalidProvider(RecordingProvider):
        async def evaluate(self, request):
            result = await super().evaluate(request)
            result.answers["urgent"]["noul"] = 9
            return result

    manager = _manager(tmp_path, InvalidProvider())
    session = manager.create(permission_mode="auto")
    asyncio.run(session.run("urgent"))
    result = next(e for e in session._backlog if e["type"] == "tool_result")
    assert result["error"] is True
    assert "Decision failed" in result["output"]
    assert manager.actions.get(result["action_id"]).status == "failed"


def test_cancel_propagates_and_no_completion_is_reported(tmp_path):
    class WaitingProvider:
        async def evaluate(self, request):
            raise asyncio.CancelledError

    manager = _manager(tmp_path, WaitingProvider())
    agent = manager.create().agent
    with pytest.raises(asyncio.CancelledError):
        asyncio.run(agent.tools.get("decision").run(
            ToolContext(agent, agent.workspace, agent.state), state="x", questions=QUESTIONS,
        ))
    assert not any(e["type"] == "decision_completed" for e in agent.state["session"]._backlog)


def test_configured_jev_is_offline_until_called_and_credential_is_redacted(tmp_path):
    settings = _settings(tmp_path, decision_mode="jev", typesafe_api_key="test-secret-key")
    manager = SessionManager(settings, FakeAsyncAnthropic())
    assert "decision" in manager.create().agent.tools
    assert dump_config(manager, settings)["settings"]["typesafe_api_key"] == "<set>"
    assert "test-secret-key" not in repr(settings)


def test_manager_does_not_mutate_custom_registry_or_replace_explicit_provider(tmp_path):
    registry = default_registry()
    manager = SessionManager(_settings(tmp_path, decision_mode="llm"), FakeAsyncAnthropic(), tool_registry=registry)
    assert "decision" not in registry
    assert "decision" in manager.create().agent.tools
    install_decisions(registry, RecordingProvider())
    installed = registry.get("decision")
    other = SessionManager(_settings(tmp_path, decision_mode="llm"), FakeAsyncAnthropic(), tool_registry=registry)
    assert other.tool_registry.get("decision") is installed


@pytest.mark.parametrize("overrides", [
    {"decision_mode": "typo"}, {"decision_model": " "},
    {"decision_mode": "jev", "typesafe_api_key": None},
])
def test_bad_decision_settings_refuse_to_start(tmp_path, overrides):
    with pytest.raises(ValueError):
        _settings(tmp_path, **overrides)


def test_decision_settings_are_environment_backed(tmp_path, monkeypatch):
    monkeypatch.setenv("MINILOOP_DECISIONS", "jev")
    monkeypatch.setenv("MINILOOP_DECISION_MODEL", "jev-1.13.0")
    monkeypatch.setenv("TYPESAFE_API_KEY", "env-key")
    settings = _settings(tmp_path)
    assert (settings.decision_mode, settings.decision_model, settings.typesafe_api_key) == (
        "jev", "jev-1.13.0", "env-key",
    )
