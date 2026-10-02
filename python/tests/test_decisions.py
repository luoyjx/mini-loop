"""Typed judgments, malformed replies, and the bounded Jev HTTP boundary."""

import asyncio
import json

import httpx
import pytest

from mini_loop.decisions import (
    DecisionError, DecisionRequest, DecisionResult, DecisionValidationError,
    JevDecisionProvider, MAX_DECISION_BYTES, MAX_DECISION_RESPONSE_BYTES,
    validate_result,
)


def mixed_request():
    return DecisionRequest(
        state={"ticket": "I was billed twice", "charges": [12, 12]},
        questions={
            "department": {"type": "choice", "instructions": "Which department?",
                           "criteria": {"billing": {"about": "Charges"}, "technical": None}},
            "urgency": {"type": "score", "instructions": {"question": "How urgent?"},
                        "criteria": ["Can wait", {"description": "Needs attention"}, ["Urgent"]]},
            "refund": {"type": "noul", "instructions": "Is a refund requested?"},
        },
    )


def mixed_result():
    return DecisionResult(
        provider="typesafe", model="jev-1.13.0", probability_source="jev",
        usage={"input_tokens": 125, "output_tokens": 17},
        answers={
            "department": {"type": "choice", "choice": "billing", "confidence": .7,
                           "probabilities": {"billing": .85, "technical": .15}},
            "urgency": {"type": "score", "score": 1.1, "confidence": .4,
                        "probabilities": {"0": .1, "1": .7, "2": .2},
                        "legend": {"0": "Can wait", "1": {"description": "Needs attention"}, "2": ["Urgent"]}},
            "refund": {"type": "noul", "noul": .65},
        },
    )


def wire_payload():
    result = mixed_result().to_dict()
    return {key: result[key] for key in ("model", "answers", "usage")}


def test_mixed_contract_preserves_typed_data_and_snapshots():
    request = mixed_request()
    result = mixed_result()
    assert validate_result(request, result) is result
    assert result.answers["urgency"]["score"] == 1.1
    assert "confidence" not in result.answers["refund"]
    original = request.to_dict()
    constructed = DecisionRequest(**original)
    original["state"]["charges"].append(999)
    assert constructed.state["charges"] == [12, 12]
    result.to_dict()["answers"]["refund"]["noul"] = 0
    assert result.answers["refund"]["noul"] == .65


@pytest.mark.parametrize("question", [
    {"type": "text", "instructions": "What next?"},
    {"type": "noul", "instructions": "  "},
    {"type": "noul", "instructions": "Yes?", "criteria": {"yes": "true"}},
    {"type": "noul", "instructions": "Yes?", "execute": "anything"},
    {"type": "choice", "instructions": "Which?", "criteria": {}},
    {"type": "choice", "instructions": "Which?", "criteria": {"": None}},
    {"type": "choice", "instructions": "Which?", "criteria": {str(i): None for i in range(256)}},
    {"type": "score", "instructions": "How much?", "criteria": ["one"]},
    {"type": "score", "instructions": "How much?", "criteria": ["level"] * 11},
    {"type": "score", "instructions": "How much?", "criteria": [None, "one"]},
])
def test_invalid_question_rejected(question):
    with pytest.raises(DecisionValidationError):
        DecisionRequest(state="test", questions={"q": question})


@pytest.mark.parametrize("state", [None, 0, True, {"number": float("nan")}, {"number": float("inf")}, {1: "key"}])
def test_non_json_or_unsupported_state_rejected(state):
    with pytest.raises(DecisionValidationError):
        DecisionRequest(state=state, questions={"q": {"type": "noul", "instructions": "True?"}})


def test_local_bounds_are_on_utf8_bytes_question_count_and_depth():
    question = {"type": "noul", "instructions": "True?"}
    with pytest.raises(DecisionValidationError):
        DecisionRequest(state="界" * (MAX_DECISION_BYTES // 2), questions={"q": question})
    with pytest.raises(DecisionValidationError):
        DecisionRequest(state="", questions={str(i): question for i in range(33)})
    nested = "value"
    for _ in range(40):
        nested = [nested]
    with pytest.raises(DecisionValidationError):
        DecisionRequest(state=nested, questions={"q": question})
    assert len(DecisionRequest(state="", questions={str(i): question for i in range(32)}).questions) == 32


@pytest.mark.parametrize("mutation", [
    lambda a: a.pop("refund"),
    lambda a: a.update(extra={"type": "noul", "noul": .5}),
    lambda a: a["refund"].update(type="choice"),
    lambda a: a["refund"].update(confidence=.3),
    lambda a: a["refund"].update(noul=True),
    lambda a: a["refund"].update(noul=float("nan")),
    lambda a: a["department"].update(choice="technical"),
    lambda a: a["department"].update(choice="unknown"),
    lambda a: a["department"].update(confidence=float("inf")),
    lambda a: a["department"]["probabilities"].update(billing=.8),
    lambda a: a["department"]["probabilities"].update(billing=10**400),
    lambda a: a["department"]["probabilities"].pop("technical"),
    lambda a: a["urgency"].update(score=1),
    lambda a: a["urgency"].update(score=10**400),
    lambda a: a["urgency"]["legend"].update({"1": "changed"}),
])
def test_inconsistent_provider_answers_rejected(mutation):
    result = mixed_result()
    mutation(result.answers)
    with pytest.raises(DecisionValidationError):
        validate_result(mixed_request(), result)


def test_no_usage_is_honest_for_adapters_but_partial_and_bool_counts_are_invalid():
    base = mixed_result().to_dict()
    assert validate_result(mixed_request(), DecisionResult(**{**base, "usage": {}})).usage == {}
    for usage in ({"input_tokens": 2}, {"input_tokens": True, "output_tokens": 2},
                  {"input_tokens": -1, "output_tokens": 2}):
        with pytest.raises(DecisionValidationError):
            validate_result(mixed_request(), DecisionResult(**{**base, "usage": usage}))


@pytest.mark.asyncio
async def test_http_wire_and_versioned_result_with_borrowed_client():
    calls = []

    async def respond(request):
        calls.append(request)
        assert str(request.url) == "https://api.typesafe.ai/v1/systemone"
        assert request.headers["Authorization"] == "Bearer test-private-key"
        assert request.method == "POST"
        assert json.loads(request.content) == {"model": "jev-latest", **mixed_request().to_dict()}
        return httpx.Response(200, json=wire_payload())

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await JevDecisionProvider("test-private-key", client=client).evaluate(mixed_request())
        assert not client.is_closed
        assert result == mixed_result()
    assert len(calls) == 1


@pytest.mark.asyncio
async def test_owned_client_closes_after_call(monkeypatch):
    real_client = httpx.AsyncClient
    clients = []

    def make_client(**kwargs):
        assert kwargs == {"trust_env": False}
        client = real_client(transport=httpx.MockTransport(lambda request: httpx.Response(200, json=wire_payload())))
        clients.append(client)
        return client

    monkeypatch.setattr("mini_loop.decisions.httpx.AsyncClient", make_client)
    await JevDecisionProvider("key").evaluate(mixed_request())
    assert len(clients) == 1 and clients[0].is_closed


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [401, 422, 302, 500])
async def test_http_errors_are_sanitized_and_not_retried(status):
    calls = []

    def respond(request):
        calls.append(request)
        return httpx.Response(status, text="secret-request-key-or-body", headers={"location": "https://other.test/"})

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond), follow_redirects=True) as client:
        with pytest.raises(DecisionError, match=f"HTTP {status}") as error:
            await JevDecisionProvider("secret-key", client=client).evaluate(mixed_request())
    assert "secret" not in str(error.value) and len(calls) == 1


@pytest.mark.asyncio
async def test_retries_are_bounded_and_retry_after_is_clamped(monkeypatch):
    calls, delays = [], []

    def respond(request):
        calls.append(request)
        return httpx.Response(429 if len(calls) == 1 else 529, headers={"retry-after": "999999"})

    async def sleep(delay):
        delays.append(delay)

    monkeypatch.setattr("mini_loop.decisions.asyncio.sleep", sleep)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        with pytest.raises(DecisionError, match="HTTP 529"):
            await JevDecisionProvider("key", client=client).evaluate(mixed_request())
    assert len(calls) == 3 and delays == [2.0, 2.0]


@pytest.mark.asyncio
@pytest.mark.parametrize("header", ["nan", "inf", "-1", "invalid"])
async def test_invalid_retry_after_uses_bounded_fallback(monkeypatch, header):
    calls, delays = [], []

    def respond(request):
        calls.append(request)
        return httpx.Response(429, headers={"retry-after": header}) if len(calls) == 1 else httpx.Response(200, json=wire_payload())

    async def sleep(delay):
        delays.append(delay)

    monkeypatch.setattr("mini_loop.decisions.asyncio.sleep", sleep)
    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        result = await JevDecisionProvider("key", client=client).evaluate(mixed_request())
    assert result.model == "jev-1.13.0" and delays == [.25]


@pytest.mark.asyncio
@pytest.mark.parametrize("body", [b"not-json-secret", b'{"model":"first","model":"second"}', b"[]",
                                  json.dumps({**wire_payload(), "usage": {}}).encode()])
async def test_invalid_wire_response_has_safe_error(body):
    async with httpx.AsyncClient(transport=httpx.MockTransport(lambda request: httpx.Response(200, content=body))) as client:
        with pytest.raises(DecisionError) as error:
            await JevDecisionProvider("secret", client=client).evaluate(mixed_request())
    assert "secret" not in str(error.value)


class StreamingBody(httpx.AsyncByteStream):
    def __init__(self):
        self.closed = False
        self.chunks = 0

    async def __aiter__(self):
        for _ in range(MAX_DECISION_RESPONSE_BYTES // 16384 + 10):
            self.chunks += 1
            yield b"x" * 16384

    async def aclose(self):
        self.closed = True


@pytest.mark.asyncio
async def test_response_is_bounded_during_read_and_closed():
    stream = StreamingBody()
    async with httpx.AsyncClient(transport=httpx.MockTransport(lambda request: httpx.Response(200, stream=stream))) as client:
        with pytest.raises(DecisionError, match="byte limit"):
            await JevDecisionProvider("key", client=client).evaluate(mixed_request())
    assert stream.closed and stream.chunks == MAX_DECISION_RESPONSE_BYTES // 16384 + 1


@pytest.mark.asyncio
async def test_total_timeout_and_cancellation():
    entered = asyncio.Event()

    async def respond(request):
        entered.set()
        await asyncio.Event().wait()

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        with pytest.raises(DecisionError, match="timed out"):
            await JevDecisionProvider("key", client=client, timeout=.02).evaluate(mixed_request())
        entered.clear()
        task = asyncio.create_task(JevDecisionProvider("key", client=client).evaluate(mixed_request()))
        await entered.wait()
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task
        assert not client.is_closed


@pytest.mark.asyncio
@pytest.mark.parametrize("exception", [httpx.ReadTimeout("secret"), httpx.ConnectError("secret")])
async def test_transport_exception_is_sanitized(exception):
    def respond(request):
        raise exception

    async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
        with pytest.raises(DecisionError) as error:
            await JevDecisionProvider("secret", client=client).evaluate(mixed_request())
    assert "secret" not in str(error.value)


@pytest.mark.parametrize("kwargs", [{"timeout": 0}, {"timeout": float("inf")}, {"timeout": 121},
                                   {"timeout": True}, {"max_retries": -1}, {"max_retries": 4},
                                   {"model": ""}])
def test_constructor_rejects_unbounded_or_invalid_settings(kwargs):
    with pytest.raises(DecisionValidationError):
        JevDecisionProvider("key", **kwargs)
