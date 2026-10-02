"""Explicit typed judgment through the ordinary permission and event pipeline."""

from __future__ import annotations

import asyncio
import json
import time
import uuid

from .decisions import (
    DecisionError, DecisionProvider, DecisionRequest, DecisionValidationError,
    validate_result,
)
from .registry import Tool, ToolRegistry

_CONTENT = {"type": ["string", "object", "array"]}
_SCHEMA = {
    "type": "object",
    "properties": {
        "state": {**_CONTENT, "description": "Explicit evidence to evaluate; no session history is added."},
        "questions": {
            "type": "object", "minProperties": 1, "maxProperties": 32,
            "description": "Named independent questions evaluated against the same state.",
            "additionalProperties": {
                "oneOf": [
                    {
                        "type": "object",
                        "properties": {
                            "type": {"const": "choice"},
                            "instructions": _CONTENT,
                            "criteria": {
                                "type": "object", "minProperties": 1, "maxProperties": 255,
                                "additionalProperties": {"type": ["string", "object", "array", "null"]},
                            },
                        },
                        "required": ["type", "instructions", "criteria"],
                        "additionalProperties": False,
                    },
                    {
                        "type": "object",
                        "properties": {
                            "type": {"const": "score"}, "instructions": _CONTENT,
                            "criteria": {"type": "array", "minItems": 2, "maxItems": 10, "items": _CONTENT},
                        },
                        "required": ["type", "instructions", "criteria"],
                        "additionalProperties": False,
                    },
                    {
                        "type": "object",
                        "properties": {
                            "type": {"const": "noul"}, "instructions": _CONTENT,
                            "criteria": {
                                "type": "object",
                                "properties": {"true": _CONTENT, "false": _CONTENT},
                                "additionalProperties": False,
                            },
                        },
                        "required": ["type", "instructions"],
                        "additionalProperties": False,
                    },
                ],
            },
        },
    },
    "required": ["state", "questions"],
    "additionalProperties": False,
}


def install_decisions(
    registry: ToolRegistry, provider: DecisionProvider | None = None,
) -> None:
    """Install an explicit, billable decision tool; None uses the session LLM.

    Provider selection belongs to the embedding application, never tool input.
    Providers injected here must support concurrent independent sessions.
    """

    async def decision(ctx, state, questions) -> str:
        from .decision_llm import LLMDecisionProvider

        try:
            request = DecisionRequest(state=state, questions=questions)
            # Mask the structure BEFORE either backend sees it. No hidden
            # transcript, workspace, or owner resource is added to the input.
            masked = ctx.agent.secrets.mask_payload(request.to_dict())
            request = DecisionRequest(**masked)
        except DecisionValidationError as error:
            await ctx.emit_event("decision_failed", error_type=type(error).__name__)
            raise DecisionValidationError(f"Invalid decision request: {error}") from None

        backend = provider if provider is not None else LLMDecisionProvider(ctx.agent)
        managed_llm = isinstance(backend, LLMDecisionProvider)
        span = f"model_{uuid.uuid4().hex[:16]}"
        started = time.monotonic()
        if not managed_llm:
            await ctx.emit_event(
                "model_start", span_id=span, purpose="decision",
                model=getattr(backend, "model", "custom-decision"),
                tool_count=0, message_count=1,
                _trajectory_fields={"model_input": request.to_dict()},
            )
        try:
            async with asyncio.timeout(60):
                if managed_llm:
                    result = await backend.evaluate(request)
                else:
                    async with ctx.agent.semaphore:
                        result = await backend.evaluate(request)
                result = validate_result(request, result)
        except BaseException as error:
            cancelled = isinstance(error, asyncio.CancelledError)
            if not managed_llm:
                await ctx.emit_event(
                    "model_end", span_id=span, purpose="decision",
                    status="cancelled" if cancelled else "error",
                    duration_ms=round((time.monotonic() - started) * 1000, 3),
                )
            if not isinstance(error, Exception):
                raise
            # Custom provider errors may contain raw state or credentials.
            # Only our bounded contract errors are appropriate tool feedback.
            detail = str(error) if isinstance(error, DecisionError) else type(error).__name__
            await ctx.emit_event("decision_failed", error_type=type(error).__name__)
            raise DecisionError(f"Decision failed: {detail}") from None
        if not managed_llm:
            await ctx.emit_event(
                "model_end", span_id=span, purpose="decision", status="completed",
                duration_ms=round((time.monotonic() - started) * 1000, 3),
                served_model=result.model, usage=result.usage,
                prompt_tokens=result.usage.get("input_tokens", 0),
            )
        await ctx.emit_event(
            "decision_completed", provider=result.provider, model=result.model,
            probability_source=result.probability_source,
            question_count=len(request.questions), usage=result.usage,
        )
        return json.dumps(
            result.to_dict(), ensure_ascii=False, allow_nan=False, separators=(",", ":"),
        )

    registry.register(Tool(
        "decision",
        "Evaluate explicit state with typed questions: choice returns an option distribution, "
        "score returns a probability-weighted 0-based rubric score, noul returns P(yes). "
        "Batch independent questions. Uses the configured decision provider and may incur "
        "a separate model/API charge. Returns judgments only; confidence never authorizes "
        "actions. Inspect probability_source: llm_estimate is uncalibrated.",
        _SCHEMA, decision, risk="external",
    ))


NO_RUNTIME_INVARIANT = (
    "No runtime invariant: decision dispatch uses the existing tool "
    "permission pipeline; decisions.validate_result owns the typed-result boundary."
)
