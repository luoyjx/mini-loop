"""Typed decisions survive action replay without billing the provider twice."""

import asyncio
import json

import pytest

from mini_loop.actions import (
    DurableActionJournal,
    InMemoryActionJournal,
    MAX_ACTION_RESULT_CHARS,
    MAX_DECISION_ACTION_RESULT_CHARS,
    MAX_RETAINED_RESULT_CHARS,
    SHED_RESULT,
)
from mini_loop.agent import Agent
from mini_loop.config import Settings
from mini_loop.decision_tools import install_decisions
from mini_loop.decisions import (
    DecisionRequest,
    DecisionResult,
    MAX_DECISION_RESPONSE_BYTES,
    validate_result,
)
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.harness import Harness
from mini_loop.registry import Hooks, ToolCall, ToolRegistry
from mini_loop.run_context import RunContext
from mini_loop.storage import SQLiteStateStore


def _request():
    return DecisionRequest(state="Evaluate this case", questions={
        f"route-{question}": {
            "type": "choice", "instructions": "Choose the most suitable category",
            "criteria": {f"category-{index:03}": None for index in range(32)},
        }
        for question in range(16)
    })


class CountingProvider:
    def __init__(self, *, at_limit=False):
        self.calls = 0
        self.at_limit = at_limit

    async def evaluate(self, request):
        self.calls += 1
        answers = {}
        for name, question in request.questions.items():
            choice = next(iter(question["criteria"]))
            answers[name] = {
                "type": "choice", "choice": choice, "confidence": 1,
                "probabilities": {key: int(key == choice) for key in question["criteria"]},
            }
        result = DecisionResult(
            provider="test", model="test-model", answers=answers,
            usage={"input_tokens": 5, "output_tokens": 9},
            probability_source="llm_estimate",
        )
        if self.at_limit:
            # A valid result exactly at the contract boundary catches a mismatch
            # between compact validation and whitespace-heavy tool serialization.
            payload = result.to_dict()
            size = len(json.dumps(payload, separators=(",", ":")).encode("utf-8"))
            payload["model"] += "m" * (MAX_DECISION_RESPONSE_BYTES - size)
            result = DecisionResult(**payload)
        return validate_result(request, result)


def _agent(tmp_path, journal, provider):
    registry = ToolRegistry()
    install_decisions(registry, provider)
    return Agent(
        client=FakeAsyncAnthropic(),
        settings=Settings(fake_llm=True, skills_dir=tmp_path / "skills"),
        workspace=tmp_path,
        harness=Harness(tools=registry, hooks=Hooks()),
        state={"session_id": "decision-session", "action_journal": journal},
    )


@pytest.mark.parametrize("durable", [False, True], ids=["memory", "sqlite-reopen"])
@pytest.mark.parametrize("at_limit", [False, True], ids=["batch-over-4k", "maximum-result"])
def test_large_decision_replays_exactly_without_another_provider_call(tmp_path, durable, at_limit):
    journal = (
        DurableActionJournal(SQLiteStateStore(tmp_path / "actions.db"))
        if durable else InMemoryActionJournal()
    )
    provider = CountingProvider(at_limit=at_limit)
    context = RunContext.default()
    call = ToolCall(name="decision", input=_request().to_dict(), id="same-decision")
    try:
        agent = _agent(tmp_path, journal, provider)
        first = asyncio.run(agent._exec_tool(call, run_context=context))
        assert len(first) > MAX_ACTION_RESULT_CHARS
        first_payload = json.loads(first)
        assert set(first_payload["answers"]) == set(_request().questions)
        assert provider.calls == 1
        if at_limit:
            assert len(first.encode("utf-8")) == MAX_DECISION_RESPONSE_BYTES
        if durable:
            journal.store.close()
            journal = DurableActionJournal(SQLiteStateStore(tmp_path / "actions.db"))
        replay_agent = _agent(tmp_path, journal, provider)
        replay = asyncio.run(replay_agent._exec_tool(call, run_context=context))
        assert json.loads(replay) == first_payload
        assert replay == first
        assert provider.calls == 1
    finally:
        if durable:
            journal.store.close()


def _begin(journal, *, action_id="a1", tool_name="decision"):
    return journal.begin(
        action_id=action_id, session_id="s1", message_id="m1", tool_use_id=action_id,
        tool_name=tool_name, input_value={},
    )


@pytest.mark.parametrize("durable", [False, True])
@pytest.mark.parametrize("tool_name,limit", [
    ("decision", MAX_DECISION_ACTION_RESULT_CHARS),
    ("ordinary", MAX_ACTION_RESULT_CHARS),
])
def test_result_limits_remain_bounded_and_are_specific_to_the_tool(tmp_path, durable, tool_name, limit):
    journal = (
        DurableActionJournal(SQLiteStateStore(tmp_path / "limits.db"))
        if durable else InMemoryActionJournal()
    )
    try:
        for suffix, result in [("exact", "x" * limit), ("over", "x" * (limit + 1)), ("none", None)]:
            _begin(journal, action_id=suffix, tool_name=tool_name)
            record = journal.finish(suffix, status="completed", result=result)
            if suffix == "over":
                assert len(record.result) == limit
                assert record.result.endswith(f"[action result truncated; original_chars={limit + 1}]")
            else:
                assert record.result == result
    finally:
        if durable:
            journal.store.close()


@pytest.mark.parametrize("tool_name,limit", [
    ("decision", MAX_DECISION_ACTION_RESULT_CHARS),
    ("ordinary", MAX_ACTION_RESULT_CHARS),
])
def test_reconciliation_uses_the_recorded_tools_result_budget(tmp_path, tool_name, limit):
    journal = DurableActionJournal(SQLiteStateStore(tmp_path / "reconcile.db"))
    try:
        for suffix, result in [("exact", "x" * limit), ("over", "x" * (limit + 1))]:
            _begin(journal, action_id=suffix, tool_name=tool_name)
            journal.finish(suffix, status="unknown")
            record = journal.reconcile(suffix, status="completed", result=result)
            assert len(record.result) == limit
            if suffix == "exact":
                assert record.result == result
            else:
                assert "action result truncated" in record.result
    finally:
        journal.store.close()


def test_decision_payloads_still_obey_in_memory_retention():
    journal = InMemoryActionJournal(max_results_retained=1)
    for action_id in ["first", "second"]:
        _begin(journal, action_id=action_id)
        journal.finish(action_id, status="completed", result="x" * 10_000)
    first = journal.get("first")
    assert first.status == "completed"
    assert first.result == SHED_RESULT
    assert len(journal.get("second").result) == 10_000


def test_large_decisions_shed_at_the_aggregate_limit_without_reexecution(tmp_path):
    journal = InMemoryActionJournal()
    provider = CountingProvider(at_limit=True)
    agent = _agent(tmp_path, journal, provider)
    context = RunContext.default()
    calls = [
        ToolCall(name="decision", input=_request().to_dict(), id=f"decision-{index}")
        for index in range(5)
    ]
    for call in calls:
        result = asyncio.run(agent._exec_tool(call, run_context=context))
        assert len(result) == MAX_DECISION_RESPONSE_BYTES
    assert provider.calls == 5
    assert len(journal._records) == 5
    retained = [record for record in journal._records.values() if record.result != SHED_RESULT]
    assert sum(len(record.result) for record in retained) <= MAX_RETAINED_RESULT_CHARS
    assert len(retained) < len(calls)
    assert journal._retained_result_chars == sum(len(record.result) for record in retained)
    replay = asyncio.run(agent._exec_tool(calls[0], run_context=context))
    assert replay == SHED_RESULT
    assert provider.calls == 5


def test_ordinary_results_keep_the_configured_count_when_under_total_budget():
    journal = InMemoryActionJournal(max_results_retained=3)
    for index in range(4):
        _begin(journal, action_id=str(index), tool_name="ordinary")
        journal.finish(str(index), status="completed", result="x" * MAX_ACTION_RESULT_CHARS)
    assert journal.get("0").result == SHED_RESULT
    assert all(len(journal.get(str(index)).result) == MAX_ACTION_RESULT_CHARS for index in range(1, 4))
    assert journal._retained_result_chars == 3 * MAX_ACTION_RESULT_CHARS


def test_finishing_again_does_not_double_count_the_retained_payload():
    journal = InMemoryActionJournal()
    _begin(journal)
    journal.finish("a1", status="completed", result="x" * 10_000)
    journal.finish("a1", status="completed", result="y" * 20_000)
    assert journal._retained_result_chars == 10_000


def test_journal_budget_preserves_every_accepted_decision_byte():
    assert MAX_DECISION_ACTION_RESULT_CHARS >= MAX_DECISION_RESPONSE_BYTES
