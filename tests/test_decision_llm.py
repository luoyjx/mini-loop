"""The LLM adapter keeps decision queries separate from agent turns."""

import asyncio
import copy
import json
import math
from types import SimpleNamespace

import pytest

from mini_loop.agent import Agent, _CURRENT_RUN_CONTEXT
from mini_loop.config import Settings
from mini_loop.decision_llm import (
    DecisionProviderError,
    DecisionTimeoutError,
    LLMDecisionProvider,
    MAX_RESPONSE_BYTES,
)
from mini_loop.decisions import DecisionRequest, DecisionValidationError
from mini_loop.fake_llm import FakeAsyncAnthropic, FakeMessage, text, tool
from mini_loop.harness import Harness
from mini_loop.run_context import RunContext
from mini_loop.secrets import SecretRegistry
from mini_loop.transport import StreamingTransport
from mini_loop.token_efficiency import TokenEfficiencyRuntime


def _request():
    return DecisionRequest(
        state={"customer": "Please investigate this refund request"},
        questions={
            "route": {"type": "choice", "instructions": "Select the best team",
                      "criteria": {"billing": "Payments", "support": "Product help"}},
            "quality": {"type": "score", "instructions": "Assess completeness",
                        "criteria": ["missing", {"partial": True}, "complete"]},
            "ready": {"type": "noul", "instructions": "Has enough detail"},
        },
    )


def _body():
    return {"distributions": {
        "route": {"billing": 0.8, "support": 0.2},
        "quality": {"0": 0.1, "1": 0.2, "2": 0.7},
        "ready": {"true": 0.6, "false": 0.4},
    }}


def _parent(tmp_path, responder=None, **kwargs):
    return Agent(
        client=FakeAsyncAnthropic(
            responder=responder or (lambda _: ([text(json.dumps(_body()))], "end_turn"))
        ),
        settings=Settings(fake_llm=True, skills_dir=tmp_path / "skills"),
        workspace=tmp_path, harness=kwargs.pop("harness", Harness()), **kwargs,
    )


def test_computes_all_answers_and_labels_estimates(tmp_path):
    parent = _parent(tmp_path)
    result = asyncio.run(LLMDecisionProvider(parent).evaluate(_request()))
    assert result.provider == "llm"
    assert result.model == parent.settings.model
    assert result.probability_source == "llm_estimate"
    assert result.answers["route"]["choice"] == "billing"
    expected = 1 + (0.8 * math.log(0.8) + 0.2 * math.log(0.2)) / math.log(2)
    assert result.answers["route"]["confidence"] == pytest.approx(expected)
    assert result.answers["quality"]["score"] == pytest.approx(1.6)
    assert result.answers["quality"]["legend"] == {
        "0": "missing", "1": {"partial": True}, "2": "complete",
    }
    assert result.answers["ready"] == {"type": "noul", "noul": 0.6}
    assert result.usage["input_tokens"] > 0
    assert result.usage["output_tokens"] > 0


def test_side_query_has_no_tools_history_streams_or_parent_meter_effects(tmp_path):
    requests, events = [], []

    def respond(kwargs):
        requests.append(copy.deepcopy(kwargs))
        return [text(json.dumps(_body()))], "end_turn"

    async def emit(event):
        events.append(event)

    parent = _parent(
        tmp_path, respond, emit=emit,
        harness=Harness(transport=StreamingTransport()),
    )
    parent.messages = [{"role": "user", "content": "unrelated private history"}]
    parent.state["recovery_model"] = "earlier-parent-model"
    parent.last_text = "parent answer"
    parent.streamed_text = "existing stream"
    parent._last_model_span_id = "parent_span"
    before = copy.deepcopy(parent.messages), copy.deepcopy(parent.state), parent.token_meter.snapshot()
    asyncio.run(LLMDecisionProvider(parent).evaluate(_request()))
    assert len(requests) == 1
    assert requests[0]["tools"] == []
    assert requests[0]["max_tokens"] == 4096
    assert len(requests[0]["messages"]) == 1
    assert json.loads(requests[0]["messages"][0]["content"]) == _request().to_dict()
    assert "unrelated private history" not in json.dumps(requests)
    assert (parent.messages, parent.state, parent.token_meter.snapshot()) == before
    assert parent.last_text == "parent answer"
    assert parent.streamed_text == "existing stream"
    assert parent._last_model_span_id == "parent_span"
    assert not any(event["type"] in {"text_delta", "assistant_text", "stream_start"} for event in events)
    model_events = [event for event in events if event["type"] in {"model_start", "model_end"}]
    assert len(model_events) == 2
    assert all(event["purpose"] == "decision" for event in model_events)
    assert model_events[-1]["usage"]["input_tokens"] > 0


def test_masking_is_inherited_but_cache_and_request_rewriters_are_skipped(tmp_path, monkeypatch):
    requests = []
    secrets = SecretRegistry()
    secrets.register("PRIVATE_TOKEN", "a-registered-secret-value")

    class CannotRun:
        def annotate(self, **kwargs):
            raise AssertionError("Decision must not apply cache projections")

    parent = _parent(
        tmp_path,
        lambda kwargs: (requests.append(kwargs) or [text(json.dumps(_body()))], "end_turn"),
        harness=Harness(secrets=secrets, cache_policy=CannotRun()),
    )

    async def cannot_optimize(*args, **kwargs):
        raise AssertionError("Decision must not use request optimizers")

    monkeypatch.setattr(TokenEfficiencyRuntime, "optimize_request", cannot_optimize)
    monkeypatch.setattr(TokenEfficiencyRuntime, "plan_response", cannot_optimize)
    request = _request()
    request.state["token"] = "a-registered-secret-value"
    asyncio.run(LLMDecisionProvider(parent).evaluate(request))
    assert "a-registered-secret-value" not in json.dumps(requests)
    assert request.state["token"] == "a-registered-secret-value"


def test_masks_structured_request_before_json_escaping(tmp_path):
    requests = []
    secret = 'credential-with-"quotes"-and-\\slashes'
    secrets = SecretRegistry()
    secrets.register("PRIVATE_TOKEN", secret)
    request = DecisionRequest(state={"secret": secret}, questions={
        "rubric": {"type": "score", "instructions": "Assess",
                   "criteria": [secret, "high"]},
    })
    body = {"distributions": {"rubric": {"0": 0.3, "1": 0.7}}}
    parent = _parent(
        tmp_path,
        lambda kwargs: (requests.append(kwargs) or [text(json.dumps(body))], "end_turn"),
        harness=Harness(secrets=secrets),
    )
    result = asyncio.run(LLMDecisionProvider(parent).evaluate(request))
    sent = json.loads(requests[0]["messages"][0]["content"])
    assert sent["state"]["secret"] == secrets.mask(secret)
    assert result.answers["rubric"]["legend"]["0"] == secrets.mask(secret)
    assert request.state["secret"] == secret


def test_decision_events_have_child_run_lineage_and_restore_parent_context(tmp_path):
    events = []

    async def emit(event):
        events.append(event)

    async def run():
        parent = _parent(tmp_path, emit=emit)
        context = RunContext.explicit_human(actor_id="owner")
        token = _CURRENT_RUN_CONTEXT.set(context)
        try:
            await LLMDecisionProvider(parent).evaluate(_request())
            assert parent.current_run_context is context
        finally:
            _CURRENT_RUN_CONTEXT.reset(token)
        assert events
        assert all(event["parent_message_id"] == context.message_id for event in events)
        assert len({event["message_id"] for event in events}) == 1
        assert events[0]["message_id"] != context.message_id

    asyncio.run(run())


@pytest.mark.parametrize("stop", ["max_tokens", "tool_use", "pause_turn", "refusal", "stop_sequence", None])
def test_rejects_nonfinal_response_before_recovery_continuation(tmp_path, stop):
    parent = _parent(tmp_path, lambda _: ([text(json.dumps(_body()))], stop))
    with pytest.raises(DecisionValidationError, match="did not complete"):
        asyncio.run(LLMDecisionProvider(parent).evaluate(_request()))
    assert parent.client.calls == 1
    assert parent.messages == []


def test_rejects_tool_calls_even_if_provider_claims_end_turn(tmp_path):
    parent = _parent(
        tmp_path,
        lambda _: ([text(json.dumps(_body())), tool("run_bash", command="false")], "end_turn"),
    )
    with pytest.raises(DecisionValidationError, match="unsupported content"):
        asyncio.run(LLMDecisionProvider(parent).evaluate(_request()))
    assert parent.client.calls == 1


@pytest.mark.parametrize("response", [
    "not JSON with private response content",
    "```json\n{}\n```", "[]", "{}",
    '{"distributions":{},"distributions":{}}',
    '{"distributions":{"route":{"billing":NaN}}}',
    json.dumps({"distributions": {"unexpected": {"a": 1}}}),
    "x" * (MAX_RESPONSE_BYTES + 1),
    '"\ud800"',
])
def test_rejects_malformed_and_oversize_output_without_echoing_payload(tmp_path, response):
    parent = _parent(tmp_path, lambda _: ([text(response)], "end_turn"))
    with pytest.raises(DecisionValidationError) as error:
        asyncio.run(LLMDecisionProvider(parent).evaluate(_request()))
    assert "private response content" not in str(error.value)
    assert len(str(error.value)) < 120


@pytest.mark.parametrize("probabilities", [
    {"billing": 0.2, "support": 0.2},
    {"billing": 1.1, "support": -0.1},
    {"billing": True, "support": 0},
    {"billing": "0.8", "support": 0.2},
    {"billing": 1},
    {"billing": 0.8, "support": 0.2, "extra": 0},
    {"billing": 10 ** 1000, "support": 0},
])
def test_rejects_incomplete_and_invalid_distributions(tmp_path, probabilities):
    body = _body()
    body["distributions"]["route"] = probabilities
    parent = _parent(tmp_path, lambda _: ([text(json.dumps(body))], "end_turn"))
    with pytest.raises(DecisionValidationError):
        asyncio.run(LLMDecisionProvider(parent).evaluate(_request()))


def test_single_choice_and_uniform_distribution_confidence(tmp_path):
    request = DecisionRequest(state="input", questions={
        "one": {"type": "choice", "instructions": "Select", "criteria": {"only": None}},
        "tie": {"type": "choice", "instructions": "Select", "criteria": {"a": None, "b": None}},
    })
    body = {"distributions": {"one": {"only": 1}, "tie": {"a": 0.5, "b": 0.5}}}
    parent = _parent(tmp_path, lambda _: ([text(json.dumps(body))], "end_turn"))
    result = asyncio.run(LLMDecisionProvider(parent).evaluate(request))
    assert result.answers["one"]["confidence"] == 1
    assert result.answers["tie"]["confidence"] == 0
    assert result.answers["tie"]["choice"] in {"a", "b"}


def test_provider_failures_are_typed_and_do_not_echo_upstream_payload(tmp_path):
    def fail(_kwargs):
        raise ValueError("upstream body contains private-credential")

    parent = _parent(tmp_path, fail)
    with pytest.raises(DecisionProviderError) as error:
        asyncio.run(LLMDecisionProvider(parent).evaluate(_request()))
    assert "private-credential" not in str(error.value)
    assert error.value.__suppress_context__


def test_timeout_covers_waiting_for_shared_llm_semaphore(tmp_path):
    async def run():
        semaphore = asyncio.Semaphore(0)
        parent = _parent(tmp_path, llm_semaphore=semaphore)
        with pytest.raises(DecisionTimeoutError):
            await LLMDecisionProvider(parent, timeout_seconds=0.01).evaluate(_request())
        assert parent.client.calls == 0

    asyncio.run(run())


def test_cancellation_propagates_and_releases_shared_semaphore(tmp_path):
    async def run():
        entered, cancelled = asyncio.Event(), asyncio.Event()

        async def create(**kwargs):
            entered.set()
            try:
                await asyncio.Event().wait()
            finally:
                cancelled.set()

        semaphore = asyncio.Semaphore(1)
        parent = _parent(tmp_path, llm_semaphore=semaphore)
        parent.client = SimpleNamespace(messages=SimpleNamespace(create=create))
        task = asyncio.create_task(LLMDecisionProvider(parent).evaluate(_request()))
        await asyncio.wait_for(entered.wait(), timeout=1)
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task
        assert cancelled.is_set()
        assert not semaphore.locked()

    asyncio.run(run())


def test_reuses_recovery_but_isolates_its_state_and_reports_actual_model(tmp_path):
    class ModelRecovery:
        async def run(self, agent, kwargs, call, *, live_history=None):
            assert live_history is None
            assert agent.tools.names() == []
            agent.state["recovery_model"] = "fallback"
            kwargs["model"] = "fallback"
            return await call(kwargs)

    parent = _parent(tmp_path, harness=Harness(recovery=ModelRecovery()))
    result = asyncio.run(LLMDecisionProvider(parent).evaluate(_request()))
    assert result.model == "fallback"
    assert "recovery_model" not in parent.state


def test_transient_retry_uses_same_authoritative_request(tmp_path, monkeypatch):
    requests = []

    def respond(kwargs):
        requests.append(copy.deepcopy(kwargs))
        if len(requests) == 1:
            raise ConnectionError("network unavailable")
        return [text(json.dumps(_body()))], "end_turn"

    monkeypatch.setattr("mini_loop.recovery.backoff_delay", lambda *_: 0)
    parent = _parent(tmp_path, respond)
    result = asyncio.run(LLMDecisionProvider(parent).evaluate(_request()))
    assert result.answers["route"]["choice"] == "billing"
    assert len(requests) == 2
    assert requests[0] == requests[1]


def test_reports_served_model_and_dict_usage(tmp_path):
    async def create(**kwargs):
        return FakeMessage(
            [{"type": "text", "text": json.dumps(_body())}], "end_turn",
            usage={"input_tokens": 17, "output_tokens": 23}, model="served-alias",
        )

    parent = _parent(tmp_path)
    parent.client = SimpleNamespace(messages=SimpleNamespace(create=create))
    result = asyncio.run(LLMDecisionProvider(parent).evaluate(_request()))
    assert result.model == "served-alias"
    assert result.usage == {"input_tokens": 17, "output_tokens": 23}


@pytest.mark.parametrize("usage", [None, {}, {"input_tokens": 17}])
def test_missing_usage_is_unavailable_instead_of_fabricated_zero(tmp_path, usage):
    async def create(**kwargs):
        response = FakeMessage([text(json.dumps(_body()))], "end_turn")
        response.usage = usage
        return response

    parent = _parent(tmp_path)
    parent.client = SimpleNamespace(messages=SimpleNamespace(create=create))
    result = asyncio.run(LLMDecisionProvider(parent).evaluate(_request()))
    assert result.usage == {}


def test_parallel_decisions_share_the_parent_llm_limit(tmp_path):
    async def run():
        active, maximum = 0, 0

        async def create(**kwargs):
            nonlocal active, maximum
            active += 1
            maximum = max(maximum, active)
            await asyncio.sleep(0)
            active -= 1
            return FakeMessage([text(json.dumps(_body()))], "end_turn")

        parent = _parent(tmp_path, llm_semaphore=asyncio.Semaphore(1))
        parent.client = SimpleNamespace(messages=SimpleNamespace(create=create))
        provider = LLMDecisionProvider(parent)
        results = await asyncio.gather(*[provider.evaluate(_request()) for _ in range(3)])
        assert len(results) == 3
        assert maximum == 1
        assert parent.messages == []

    asyncio.run(run())


def test_request_mutation_is_revalidated_before_provider_call(tmp_path):
    request = _request()
    request.questions["route"]["criteria"] = {}
    parent = _parent(tmp_path)
    with pytest.raises(DecisionValidationError):
        asyncio.run(LLMDecisionProvider(parent).evaluate(request))
    assert parent.client.calls == 0


@pytest.mark.parametrize("kwargs", [
    {"timeout_seconds": 0}, {"timeout_seconds": float("nan")},
    {"timeout_seconds": float("inf")}, {"timeout_seconds": True},
    {"max_output_tokens": 0}, {"max_output_tokens": -1},
    {"max_output_tokens": 1.5}, {"max_output_tokens": True},
])
def test_rejects_invalid_budgets(tmp_path, kwargs):
    with pytest.raises(DecisionValidationError):
        LLMDecisionProvider(_parent(tmp_path), **kwargs)
