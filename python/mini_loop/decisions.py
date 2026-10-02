"""Bounded typed decisions and the TypeSafe System One transport.

These are judgments, not permissions or executable actions. Request limits and
the requirement for explicit instructions / 2–10 score levels are mini-loop's
local contract. Jev's state and questions are sent only when evaluate is called.
"""

from __future__ import annotations

import asyncio
import copy
import json
import math
from dataclasses import dataclass
from typing import Any, Literal, Protocol

import httpx

MAX_DECISION_QUESTIONS = 32
MAX_DECISION_BYTES = 128 * 1024
MAX_DECISION_RESPONSE_BYTES = 512 * 1024
JEV_URL = "https://api.typesafe.ai/v1/systemone"
_TOLERANCE = 1e-5


class DecisionError(Exception):
    """Safe, credential-free error that may be returned to a tool caller."""


class DecisionValidationError(DecisionError):
    """A request or provider answer violates the typed decision contract."""


def _json_value(value: Any, depth: int = 0) -> None:
    if depth > 32:
        raise DecisionValidationError("Decision JSON exceeds the nesting limit.")
    if value is None or type(value) in (str, bool, int):
        return
    if type(value) is float and math.isfinite(value):
        return
    if type(value) is list:
        for item in value:
            _json_value(item, depth + 1)
        return
    if type(value) is dict and all(type(key) is str for key in value):
        for item in value.values():
            _json_value(item, depth + 1)
        return
    raise DecisionValidationError("Decision values must be finite JSON data.")


def _encoded(value: Any, limit: int) -> bytes:
    _json_value(value)
    chunks: list[bytes] = []
    size = 0
    try:
        for chunk in json.JSONEncoder(ensure_ascii=False, allow_nan=False,
                                      separators=(",", ":")).iterencode(value):
            raw = chunk.encode("utf-8")
            size += len(raw)
            if size > limit:
                raise DecisionValidationError("Decision JSON exceeds the byte limit.")
            chunks.append(raw)
    except (ValueError, UnicodeError, RecursionError):
        raise DecisionValidationError("Decision values must be valid JSON data.") from None
    return b"".join(chunks)


def _content(value: Any, *, required: bool = False) -> bool:
    if type(value) not in (str, dict, list):
        return False
    return not required or bool(value.strip() if isinstance(value, str) else value)


@dataclass(frozen=True)
class DecisionRequest:
    state: str | dict | list
    questions: dict[str, dict[str, Any]]

    def __post_init__(self) -> None:
        if not _content(self.state):
            raise DecisionValidationError("Decision state must be a string, object, or array.")
        if type(self.questions) is not dict or not 1 <= len(self.questions) <= MAX_DECISION_QUESTIONS:
            raise DecisionValidationError("Decision requests require 1 to 32 questions.")
        for name, question in self.questions.items():
            if type(name) is not str or not name.strip() or type(question) is not dict:
                raise DecisionValidationError("Decision questions require nonempty string IDs and objects.")
            if set(question) - {"type", "instructions", "criteria"}:
                raise DecisionValidationError("Decision question contains unsupported fields.")
            if not _content(question.get("instructions"), required=True):
                raise DecisionValidationError("Decision questions require nonempty instructions.")
            kind, criteria = question.get("type"), question.get("criteria")
            if kind == "choice":
                if type(criteria) is not dict or not 1 <= len(criteria) <= 255:
                    raise DecisionValidationError("Choice requires 1 to 255 named criteria.")
                if any(type(k) is not str or not k.strip() or (v is not None and not _content(v))
                       for k, v in criteria.items()):
                    raise DecisionValidationError("Choice criteria require names and JSON descriptions.")
            elif kind == "score":
                if type(criteria) is not list or not 2 <= len(criteria) <= 10 or any(not _content(v) for v in criteria):
                    raise DecisionValidationError("Score requires 2 to 10 ordered JSON descriptions.")
            elif kind == "noul":
                if criteria is not None and (type(criteria) is not dict or set(criteria) - {"true", "false"}
                        or any(v is not None and not _content(v) for v in criteria.values())):
                    raise DecisionValidationError("Noul criteria may describe true and false only.")
            else:
                raise DecisionValidationError("Decision type must be choice, score, or noul.")
        snapshot = json.loads(_encoded({"state": self.state, "questions": self.questions}, MAX_DECISION_BYTES))
        object.__setattr__(self, "state", snapshot["state"])
        object.__setattr__(self, "questions", snapshot["questions"])

    def to_dict(self) -> dict:
        return copy.deepcopy({"state": self.state, "questions": self.questions})


@dataclass(frozen=True)
class DecisionResult:
    provider: str
    model: str
    answers: dict[str, dict[str, Any]]
    usage: dict[str, int]
    probability_source: Literal["jev", "llm_estimate"]

    def to_dict(self) -> dict:
        return copy.deepcopy({"provider": self.provider, "model": self.model,
                              "answers": self.answers, "usage": self.usage,
                              "probability_source": self.probability_source})


class DecisionProvider(Protocol):
    async def evaluate(self, request: DecisionRequest) -> DecisionResult: ...


def _probability(value: Any) -> bool:
    return type(value) in (int, float) and 0 <= value <= 1 and math.isfinite(value)


def validate_result(request: DecisionRequest, result: DecisionResult) -> DecisionResult:
    """Reject mismatched or internally inconsistent answers; never repair them."""
    if not isinstance(result, DecisionResult):
        raise DecisionValidationError("Decision provider must return a DecisionResult.")
    if (not isinstance(result.provider, str) or not result.provider.strip()
            or not isinstance(result.model, str) or not result.model.strip()
            or result.probability_source not in ("jev", "llm_estimate")):
        raise DecisionValidationError("Decision result requires provider and model provenance.")
    if type(result.usage) is not dict or (result.usage and set(result.usage) != {"input_tokens", "output_tokens"}) or any(
            type(value) is not int or value < 0 for value in result.usage.values()):
        raise DecisionValidationError("Decision usage requires nonnegative integer token counts.")
    if type(result.answers) is not dict or set(result.answers) != set(request.questions):
        raise DecisionValidationError("Decision answer IDs must exactly match the questions.")
    for name, question in request.questions.items():
        answer, kind = result.answers[name], question["type"]
        if type(answer) is not dict or answer.get("type") != kind:
            raise DecisionValidationError("Decision answer type must match its question.")
        if kind == "noul":
            if set(answer) != {"type", "noul"} or not _probability(answer["noul"]):
                raise DecisionValidationError("Noul answers require only a finite yes probability.")
            continue
        expected_fields = {"type", "probabilities", "confidence", "choice"} if kind == "choice" else {
            "type", "probabilities", "confidence", "score", "legend"}
        if set(answer) != expected_fields or not _probability(answer.get("confidence")):
            raise DecisionValidationError("Decision answer fields or confidence are invalid.")
        expected = set(question["criteria"]) if kind == "choice" else {str(i) for i in range(len(question["criteria"]))}
        probabilities = answer["probabilities"]
        if (type(probabilities) is not dict or set(probabilities) != expected
                or not all(_probability(value) for value in probabilities.values())
                or not math.isclose(sum(probabilities.values()), 1.0, abs_tol=_TOLERANCE, rel_tol=0)):
            raise DecisionValidationError("Decision probabilities must cover the criteria and sum to one.")
        if kind == "choice":
            choice = answer["choice"]
            if type(choice) is not str or choice not in expected or probabilities[choice] != max(probabilities.values()):
                raise DecisionValidationError("Choice must name a highest-probability criterion.")
        else:
            score = answer["score"]
            expected_score = sum(int(key) * probability for key, probability in probabilities.items())
            legend = {str(i): level for i, level in enumerate(question["criteria"])}
            if (type(score) not in (float, int) or not 0 <= score <= len(legend) - 1
                    or not math.isfinite(score)
                    or not math.isclose(score, expected_score, abs_tol=_TOLERANCE, rel_tol=0)
                    or type(answer["legend"]) is not dict or answer["legend"] != legend):
                raise DecisionValidationError("Score and legend must match the requested rubric and probabilities.")
    _encoded(result.to_dict(), MAX_DECISION_RESPONSE_BYTES)
    return result


def _unique_object(pairs: list[tuple[str, Any]]) -> dict:
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("Duplicate JSON key")
        result[key] = value
    return result


def _retry_delay(headers: httpx.Headers, attempt: int) -> float:
    fallback = min(0.25 * 2 ** attempt, 2.0)
    try:
        value = float(headers.get("retry-after", ""))
    except ValueError:
        return fallback
    return min(value, 2.0) if math.isfinite(value) and value >= 0 else fallback


class JevDecisionProvider:
    """Explicit network evaluation; owned clients close after each call.

    An injected client belongs to its caller. The fixed URL and disabled
    redirects prevent routing the configured credential to another endpoint.
    The timeout covers all attempts, response reads, and retry delays together.
    """

    def __init__(self, api_key: str, model: str = "jev-latest", *,
                 client: httpx.AsyncClient | None = None, timeout: float = 20.0,
                 max_retries: int = 2):
        if type(api_key) is not str or not api_key.strip() or "\n" in api_key or "\r" in api_key:
            raise DecisionValidationError("A valid TypeSafe API key is required.")
        if type(model) is not str or not model.strip() or len(model) > 128:
            raise DecisionValidationError("A nonempty decision model ID is required.")
        if type(timeout) not in (float, int) or not math.isfinite(timeout) or not 0 < timeout <= 120:
            raise DecisionValidationError("Decision timeout must be finite and within (0, 120] seconds.")
        if type(max_retries) is not int or not 0 <= max_retries <= 3:
            raise DecisionValidationError("Decision retries must be between zero and three.")
        self._api_key = api_key
        self.model = model
        self._client = client
        self.timeout = timeout
        self.max_retries = max_retries

    async def evaluate(self, request: DecisionRequest) -> DecisionResult:
        # Recheck and snapshot in case a caller mutated nested request data.
        request = DecisionRequest(**request.to_dict())
        body = _encoded({"model": self.model, **request.to_dict()}, MAX_DECISION_BYTES + 256)
        try:
            async with asyncio.timeout(self.timeout):
                if self._client is not None:
                    return await self._evaluate(self._client, request, body)
                async with httpx.AsyncClient(trust_env=False) as client:
                    return await self._evaluate(client, request, body)
        except (TimeoutError, httpx.TimeoutException):
            raise DecisionError("TypeSafe decision request timed out.") from None
        except httpx.HTTPError:
            raise DecisionError("TypeSafe decision transport failed.") from None
        except (ValueError, UnicodeError, TypeError, RecursionError):
            raise DecisionError("TypeSafe returned an invalid decision response.") from None

    async def _evaluate(self, client: httpx.AsyncClient, request: DecisionRequest, body: bytes) -> DecisionResult:
        for attempt in range(self.max_retries + 1):
            async with client.stream("POST", JEV_URL, content=body,
                                     headers={"Authorization": f"Bearer {self._api_key}", "Content-Type": "application/json"},
                                     timeout=self.timeout, follow_redirects=False) as response:
                if response.status_code in (429, 529) and attempt < self.max_retries:
                    delay = _retry_delay(response.headers, attempt)
                elif response.status_code != 200:
                    raise DecisionError(f"TypeSafe decision request failed (HTTP {response.status_code}).")
                else:
                    data = bytearray()
                    async for chunk in response.aiter_bytes(chunk_size=16384):
                        if len(data) + len(chunk) > MAX_DECISION_RESPONSE_BYTES:
                            raise DecisionError("TypeSafe decision response exceeds the byte limit.")
                        data.extend(chunk)
                    payload = json.loads(data, object_pairs_hook=_unique_object)
                    if type(payload) is not dict:
                        raise DecisionValidationError("TypeSafe returned an invalid decision response.")
                    if type(payload.get("usage")) is not dict or set(payload["usage"]) != {"input_tokens", "output_tokens"}:
                        raise DecisionValidationError("TypeSafe response requires token usage.")
                    result = DecisionResult(provider="typesafe", model=payload.get("model"),
                                            answers=payload.get("answers"), usage=payload.get("usage"),
                                            probability_source="jev")
                    return validate_result(request, result)
            await asyncio.sleep(delay)
        raise DecisionError("TypeSafe decision retry limit reached.")


NO_RUNTIME_INVARIANT = (
    "No runtime invariant: decision values and provider replies are locally validated at the evaluate "
    "boundary; no ambient runtime state or executable authority is held here. "
    "Protocol, probability, timeout and byte bounds are covered by test_decisions."
)
