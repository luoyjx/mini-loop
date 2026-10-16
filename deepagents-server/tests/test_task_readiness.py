"""Bounded namespace readiness remains separate from liveness and task writes."""

import asyncio
from datetime import timedelta
from types import SimpleNamespace

import httpx
import pytest
from temporalio.api.enums.v1 import NamespaceState
from temporalio.api.workflowservice.v1 import DescribeNamespaceResponse
from temporalio.service import RPCError, RPCStatusCode

from mini_loop_deepagents.task_server.api import create_app
from mini_loop_deepagents.task_server import service as module

TOKENS = {"a" * 40: "owner"}


def response(name="private-ns", state=NamespaceState.NAMESPACE_STATE_REGISTERED):
    return DescribeNamespaceResponse(namespace_info={"name": name, "state": state})


def service(callback):
    client = SimpleNamespace(namespace="private-ns", workflow_service=SimpleNamespace(describe_namespace=callback))
    return module.TaskService(client, task_queue="queue", cursor_secret="c" * 40)


@pytest.mark.asyncio
async def test_concurrent_probes_share_one_namespace_read_and_caller_cancellation_is_local():
    entered, release = asyncio.Event(), asyncio.Event()
    calls = []

    async def read(request, **kwargs):
        calls.append((request, kwargs))
        entered.set()
        await release.wait()
        return response()

    tasks_service = service(read)
    tasks = [asyncio.create_task(tasks_service.readiness()) for _ in range(40)]
    await entered.wait()
    tasks[0].cancel()
    with pytest.raises(asyncio.CancelledError):
        await tasks[0]
    release.set()
    assert all(await asyncio.gather(*tasks[1:]))
    assert await tasks_service.readiness() is True
    assert len(calls) == 1
    request, kwargs = calls[0]
    assert request.namespace == "private-ns"
    assert kwargs == {"retry": False, "timeout": timedelta(seconds=1)}


@pytest.mark.asyncio
@pytest.mark.parametrize("value", [
    response(state=NamespaceState.NAMESPACE_STATE_DEPRECATED),
    response(name="another-ns"), DescribeNamespaceResponse(),
    RPCError("never expose private namespace or credentials", RPCStatusCode.PERMISSION_DENIED, b""),
    RuntimeError("never expose private endpoint"),
])
async def test_bad_namespace_or_rpc_errors_fail_closed_and_are_cached(value):
    calls = []

    async def read(*args, **kwargs):
        calls.append(1)
        if isinstance(value, Exception):
            raise value
        return value

    tasks_service = service(read)
    assert await tasks_service.readiness() is False
    assert await tasks_service.readiness() is False
    assert len(calls) == 1


@pytest.mark.asyncio
async def test_failed_readiness_recovers_after_short_ttl(monkeypatch):
    monkeypatch.setattr(module, "READINESS_TTL_SECONDS", 0.01)
    healthy, calls = False, []

    async def read(*args, **kwargs):
        calls.append(1)
        if not healthy:
            raise RuntimeError("offline")
        return response()

    tasks_service = service(read)
    assert await tasks_service.readiness() is False
    healthy = True
    assert await tasks_service.readiness() is False
    await asyncio.sleep(0.02)
    assert await tasks_service.readiness() is True
    assert len(calls) == 2


@pytest.mark.asyncio
async def test_namespace_probe_has_an_independent_outer_deadline(monkeypatch):
    monkeypatch.setattr(module, "READINESS_TIMEOUT_SECONDS", 0.01)
    cancelled = asyncio.Event()

    async def read(*args, **kwargs):
        try:
            await asyncio.Event().wait()
        finally:
            cancelled.set()

    assert await asyncio.wait_for(service(read).readiness(), 1) is False
    assert cancelled.is_set()


@pytest.mark.asyncio
async def test_public_readiness_returns_only_shallow_status_while_liveness_stays_healthy():
    async def read(*args, **kwargs):
        raise RuntimeError("sensitive endpoint namespace and secret")

    app = create_app(service(read), tokens=TOKENS)
    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://task") as client:
        ready = await client.get("/readyz")
        health = await client.get("/healthz")
        assert ready.status_code == 503 and ready.json() == {"status": "not_ready"}
        assert ready.headers["cache-control"] == "no-store"
        assert health.status_code == 200
        assert health.json() == {"status": "ok", "service": "task-server", "check": "process_liveness"}
        assert (await client.post("/v1/tasks", json={"message": "protected"})).status_code == 401


@pytest.mark.asyncio
async def test_readiness_remains_public_when_business_admission_is_full():
    entered, release = asyncio.Event(), asyncio.Event()

    async def read(*args, **kwargs):
        return response()

    tasks_service = service(read)

    class BusyService:
        readiness = tasks_service.readiness

        async def submit(self, *args):
            entered.set()
            await release.wait()
            return {"task_id": "accepted"}

    app = create_app(BusyService(), tokens=TOKENS, max_inflight=1)
    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://task") as client:
        headers = {"Authorization": "Bearer " + "a" * 40, "Idempotency-Key": "key"}
        pending = asyncio.create_task(client.post("/v1/tasks", json={"message": "task"}, headers=headers))
        await entered.wait()
        try:
            ready = await client.get("/readyz")
            assert ready.status_code == 200 and ready.json() == {"status": "ready"}
            assert (await client.post("/v1/tasks", json={"message": "task"}, headers=headers)).status_code == 429
        finally:
            release.set()
            assert (await pending).status_code == 202


@pytest.mark.asyncio
async def test_missing_service_is_not_ready_without_disclosing_configuration():
    app = create_app(None, tokens=TOKENS)
    async with httpx.AsyncClient(transport=httpx.ASGITransport(app=app), base_url="http://task") as client:
        response = await client.get("/readyz")
    assert response.status_code == 503 and response.json() == {"status": "not_ready"}
