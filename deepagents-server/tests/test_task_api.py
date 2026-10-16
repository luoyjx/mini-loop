"""HTTP boundary checks; actual cross-process recovery is a separate live gate."""

import asyncio
import json

import httpx
import pytest

pytest.importorskip("temporalio")

from mini_loop_deepagents.task_server.api import MAX_BODY_BYTES, create_app

TOKENS = {"a" * 40: "alice", "b" * 40: "bob"}
HEADERS = {"Authorization": "Bearer " + "a" * 40, "Idempotency-Key": "request-1"}


class RecordingService:
    def __init__(self):
        self.calls = []
        self.block = None

    async def submit(self, owner, key, message):
        self.calls.append(("submit", owner, key, message))
        if self.block is not None:
            await self.block.wait()
        return {"task_id": "task-" + "c" * 64, "status": "running", "reused": False}

    async def get(self, owner, task_id):
        self.calls.append(("get", owner, task_id))
        if self.block is not None:
            await self.block.wait()
        return {"task_id": task_id, "status": "running"}

    async def result(self, owner, task_id):
        self.calls.append(("result", owner, task_id))
        return {"task_id": task_id, "ready": False, "status": "running"}

    async def cancel(self, owner, task_id):
        self.calls.append(("cancel", owner, task_id))
        return {"task_id": task_id, "status": "running", "cancel_requested": True}

    async def events(self, owner, task_id, **kwargs):
        self.calls.append(("events", owner, task_id, kwargs))
        return {"events": [], "next_cursor": None, "order": "descending"}


@pytest.fixture
def service():
    return RecordingService()


@pytest.mark.asyncio
async def test_admission_only_passes_verified_owner_and_preserves_exact_message(service):
    app = create_app(service, tokens=TOKENS)
    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://test") as client:
        response = await client.post("/v1/tasks", headers=HEADERS, json={"message": "  original\n用户任务  "})
    assert response.status_code == 202
    assert response.headers["location"] == "/v1/tasks/task-" + "c" * 64
    assert response.headers["cache-control"] == "no-store"
    assert service.calls == [("submit", "alice", "request-1", "  original\n用户任务  ")]


@pytest.mark.asyncio
@pytest.mark.parametrize("headers", [{}, {"Authorization": "Basic xxx"}, {"Authorization": "Bearer wrong"},
                                     [("Authorization", "Bearer " + "a" * 40), ("Authorization", "Bearer " + "b" * 40)]])
async def test_unauthorized_never_reads_body_or_calls_service(service, headers):
    async def unread_body():
        raise AssertionError("unauthenticated body consumed")
        yield b""

    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=create_app(service, tokens=TOKENS)), base_url="http://test") as client:
        response = await client.post("/v1/tasks", headers=headers, content=unread_body())
        health = await client.get("/healthz")
    assert response.status_code == 401 and response.headers["www-authenticate"] == "Bearer"
    assert health.json()["check"] == "process_liveness"
    assert service.calls == []


@pytest.mark.asyncio
@pytest.mark.parametrize("body,code", [
    (b'{"message":"x","message":"y"}', "invalid_json"),
    (b'{"message":"x","owner":"bob"}', "expected_message_only"),
    (b'{"message":"x","model":"attacker"}', "expected_message_only"),
    (b'{"message":NaN}', "invalid_json"),
    (b'{"message":"\\ud800"}', "invalid_message_encoding"),
    (b'{"message":"  "}', "expected_message_only"),
    (b'[]', "expected_message_only"),
])
async def test_closed_submission_schema_and_json(service, body, code):
    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=create_app(service, tokens=TOKENS)), base_url="http://test") as client:
        response = await client.post("/v1/tasks", headers={**HEADERS, "Content-Type": "application/json"}, content=body)
    assert response.status_code == 400 and response.json()["error"]["code"] == code
    assert not service.calls


@pytest.mark.asyncio
async def test_chunked_body_is_bounded_without_trusting_content_length(service):
    async def stream():
        yield b'{"message":"'
        yield b"x" * MAX_BODY_BYTES
        raise AssertionError("body read beyond limit")

    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=create_app(service, tokens=TOKENS)), base_url="http://test") as client:
        response = await client.post("/v1/tasks", headers={**HEADERS, "Content-Type": "application/json"}, content=stream())
    assert response.status_code == 413 and not service.calls


@pytest.mark.asyncio
async def test_capacity_refuses_without_waiting_and_releases_after_request(service):
    service.block = asyncio.Event()
    app = create_app(service, tokens=TOKENS, max_inflight=1)
    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://test") as client:
        first = asyncio.create_task(client.get("/v1/tasks/a", headers=HEADERS))
        for _ in range(100):
            if service.calls:
                break
            await asyncio.sleep(0)
        assert service.calls
        refused = await client.get("/v1/tasks/b", headers=HEADERS)
        assert refused.status_code == 429 and len(service.calls) == 1
        service.block.set()
        assert (await first).status_code == 200
        assert (await client.get("/v1/tasks/b", headers=HEADERS)).status_code == 200


@pytest.mark.asyncio
async def test_timeout_reports_unknown_write_outcome_and_releases_capacity(service):
    service.block = asyncio.Event()
    app = create_app(service, tokens=TOKENS, max_inflight=1, request_timeout=1)
    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://test") as client:
        timed = await client.post("/v1/tasks", headers=HEADERS, json={"message": "x"})
        assert timed.status_code == 503
        assert timed.json()["error"]["outcome"] == "unknown"
        service.block.set()
        assert (await client.get("/v1/tasks/a", headers=HEADERS)).status_code == 200


@pytest.mark.asyncio
async def test_event_query_and_result_status(service):
    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=create_app(service, tokens=TOKENS)), base_url="http://test") as client:
        assert (await client.get("/v1/tasks/a/result", headers=HEADERS)).status_code == 202
        assert (await client.get("/v1/tasks/a/events?limit=2&cursor=signed", headers=HEADERS)).status_code == 200
        for query in ("limit=0", "limit=101", "limit=2&limit=3", "owner=bob", "limit=nan"):
            assert (await client.get("/v1/tasks/a/events?" + query, headers=HEADERS)).status_code == 400
    assert service.calls[-1] == ("events", "alice", "a", {"cursor": "signed", "limit": 2})


@pytest.mark.parametrize("kwargs", [{"tokens": {}}, {"tokens": {"short": "alice"}},
                                    {"tokens": TOKENS, "max_inflight": 0}, {"tokens": TOKENS, "request_timeout": float("nan")}])
def test_invalid_deployment_configuration_fails_before_serving(kwargs):
    with pytest.raises(ValueError):
        create_app(RecordingService(), **kwargs)
