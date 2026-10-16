"""HTTP authentication, bounded admission and Task routes.

This adapter depends on the TaskOperations contract, not Temporal or a model.
Embed create_app with an explicitly trusted service; the CLI lives in api.py.
"""

from __future__ import annotations

import asyncio
from hmac import compare_digest
import json
import logging
import math
from typing import Any, Mapping

from starlette.applications import Starlette
from starlette.exceptions import HTTPException
from starlette.middleware import Middleware
from starlette.requests import Request
from starlette.responses import JSONResponse
from starlette.routing import Route

from mini_loop_deepagents.config import load_tokens
from .protocol import TaskOperations, TaskServiceError

LOG = logging.getLogger(__name__)
MAX_BODY_BYTES = 16 * 1024


def error_response(code: str, status: int, **details: Any) -> JSONResponse:
    headers = {"Cache-Control": "no-store"}
    if status == 401:
        headers["WWW-Authenticate"] = "Bearer"
    if status in (429, 503):
        headers["Retry-After"] = "1"
    return JSONResponse({"error": {"code": code, **details}}, status_code=status, headers=headers)


class AccessBoundary:
    """Authenticate before body consumption and bound each API process's work."""

    def __init__(self, app: Any, *, tokens: Mapping[str, str], max_inflight: int, timeout: float):
        self.app = app
        self.tokens = load_tokens({"MINILOOP_DEEPAGENTS_TOKENS": json.dumps(dict(tokens))})
        if type(max_inflight) is not int or not 1 <= max_inflight <= 1024:
            raise ValueError("max_inflight must be 1–1024")
        if not math.isfinite(timeout) or not 1 <= timeout <= 120:
            raise ValueError("request timeout must be 1–120 seconds")
        self.max_inflight, self.timeout, self.active = max_inflight, timeout, 0

    async def __call__(self, scope: dict, receive: Any, send: Any) -> None:
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return
        if scope["path"] in {"/healthz", "/readyz"} and scope["method"] == "GET":
            await self.app(scope, receive, send)
            return
        authorization = [v for k, v in scope["headers"] if k.lower() == b"authorization"]
        owner = None
        if len(authorization) == 1:
            try:
                scheme, candidate = authorization[0].decode("ascii").split(" ", 1)
                if scheme.lower() == "bearer" and candidate and len(candidate) <= 4096 and not any(c.isspace() for c in candidate):
                    for token, principal in self.tokens.items():
                        if compare_digest(candidate, token):
                            owner = principal
            except (UnicodeDecodeError, ValueError):
                pass
        if owner is None:
            await error_response("unauthorized", 401)(scope, receive, send)
            return
        if self.active >= self.max_inflight:
            await error_response("api_capacity_exceeded", 429)(scope, receive, send)
            return
        # No await between the capacity check and increment on this ASGI loop.
        self.active += 1
        scope.setdefault("state", {})["task_owner"] = owner
        started = False

        async def guarded_send(message: dict) -> None:
            nonlocal started
            if message["type"] == "http.response.start":
                started = True
                headers = [(k, v) for k, v in message.get("headers", []) if k.lower() != b"cache-control"]
                message = {**message, "headers": [*headers, (b"cache-control", b"no-store")]}
            await send(message)

        try:
            async with asyncio.timeout(self.timeout):
                await self.app(scope, receive, guarded_send)
        except TimeoutError:
            if started:
                raise
            # A remote write may already have committed; never claim no task exists.
            await error_response("request_timeout", 503, outcome="unknown")(scope, receive, send)
        except Exception as error:
            if started:
                raise
            LOG.error("Task API operation failed (%s)", type(error).__name__)
            await error_response("service_unavailable", 503)(scope, receive, send)
        finally:
            self.active -= 1


def _unique_json(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    value: dict[str, Any] = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("duplicate field")
        value[key] = item
    return value


def _reject_constant(value: str) -> None:
    raise ValueError("non-finite JSON number")


async def _submission(request: Request) -> tuple[str, str]:
    if request.headers.get("content-type", "").split(";", 1)[0].strip().lower() != "application/json":
        raise HTTPException(415, "json_required")
    keys = request.headers.getlist("idempotency-key")
    if len(keys) != 1:
        raise HTTPException(400, "idempotency_key_required")
    lengths = request.headers.getlist("content-length")
    if lengths:
        if len(lengths) != 1 or not lengths[0].isascii() or not lengths[0].isdigit():
            raise HTTPException(400, "invalid_content_length")
        if int(lengths[0]) > MAX_BODY_BYTES:
            raise HTTPException(413, "request_too_large")
    body = bytearray()
    async for chunk in request.stream():
        if len(body) + len(chunk) > MAX_BODY_BYTES:
            raise HTTPException(413, "request_too_large")
        body.extend(chunk)
    try:
        value = json.loads(body, object_pairs_hook=_unique_json, parse_constant=_reject_constant)
    except (ValueError, UnicodeDecodeError, RecursionError):
        raise HTTPException(400, "invalid_json") from None
    if not isinstance(value, dict) or set(value) != {"message"} or not isinstance(value["message"], str) or not value["message"].strip():
        raise HTTPException(400, "expected_message_only")
    try:
        value["message"].encode("utf-8")
    except UnicodeEncodeError:
        raise HTTPException(400, "invalid_message_encoding") from None
    return keys[0], value["message"]


def create_app(service: TaskOperations | None, *, tokens: Mapping[str, str], max_inflight: int = 32,
               request_timeout: float = 20.0, lifespan: Any = None) -> Starlette:
    """Compose the public API; injecting a service is a trusted embedding seam."""
    # Validate deployment configuration eagerly, before binding a listening socket.
    validated = AccessBoundary(None, tokens=tokens, max_inflight=max_inflight, timeout=request_timeout)

    def operations(request: Request) -> TaskOperations:
        return request.app.state.task_service

    async def health(request: Request) -> JSONResponse:
        return JSONResponse({"status": "ok", "service": "task-server", "check": "process_liveness"})

    async def ready(request: Request) -> JSONResponse:
        try:
            available = await request.app.state.task_service.readiness() is True
        except Exception:
            available = False
        return JSONResponse({"status": "ready" if available else "not_ready"},
                            status_code=200 if available else 503, headers={"Cache-Control": "no-store"})

    async def submit(request: Request) -> JSONResponse:
        key, message = await _submission(request)
        result = await operations(request).submit(request.state.task_owner, key, message)
        return JSONResponse(result, status_code=202, headers={"Location": "/v1/tasks/" + result["task_id"]})

    async def get(request: Request) -> JSONResponse:
        return JSONResponse(await operations(request).get(request.state.task_owner, request.path_params["task_id"]))

    async def result(request: Request) -> JSONResponse:
        value = await operations(request).result(request.state.task_owner, request.path_params["task_id"])
        return JSONResponse(value, status_code=200 if value.get("ready") else 202)

    async def cancel(request: Request) -> JSONResponse:
        # Cancellation is a durable Temporal request, not proof the task stopped.
        return JSONResponse(await operations(request).cancel(request.state.task_owner, request.path_params["task_id"]), status_code=202)

    async def events(request: Request) -> JSONResponse:
        query = request.query_params
        if set(query) - {"cursor", "limit", "run_id"} or any(len(query.getlist(k)) != 1 for k in query):
            raise HTTPException(400, "invalid_event_query")
        try:
            limit_text = query.get("limit", "50")
            if not limit_text.isascii() or not limit_text.isdigit():
                raise ValueError()
            limit = int(limit_text)
            if not 1 <= limit <= 100:
                raise ValueError()
        except ValueError:
            raise HTTPException(400, "invalid_event_limit") from None
        return JSONResponse(await operations(request).events(
            request.state.task_owner, request.path_params["task_id"], cursor=query.get("cursor"),
            limit=limit, run_id=query.get("run_id"),
        ))

    async def http_error(request: Request, error: HTTPException) -> JSONResponse:
        return error_response(error.detail if isinstance(error.detail, str) else "invalid_request", error.status_code)

    async def service_error(request: Request, error: TaskServiceError) -> JSONResponse:
        return error_response(error.code, error.status_code, **error.details)

    app = Starlette(routes=[
        Route("/healthz", health, methods=["GET"]),
        Route("/readyz", ready, methods=["GET"]),
        Route("/v1/tasks", submit, methods=["POST"]),
        Route("/v1/tasks/{task_id}", get, methods=["GET"]),
        Route("/v1/tasks/{task_id}/result", result, methods=["GET"]),
        Route("/v1/tasks/{task_id}/cancel", cancel, methods=["POST"]),
        Route("/v1/tasks/{task_id}/events", events, methods=["GET"]),
    ], middleware=[Middleware(AccessBoundary, tokens=validated.tokens, max_inflight=max_inflight, timeout=request_timeout)],
        exception_handlers={HTTPException: http_error, TaskServiceError: service_error}, lifespan=lifespan)
    app.state.task_service = service
    return app
