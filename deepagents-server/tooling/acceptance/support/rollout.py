"""Shared rollout support for bounded acceptance scenarios."""
from __future__ import annotations

import asyncio
import hashlib
import json
import os
import time

from tooling.acceptance.support import process as SUPPORT
from tooling.acceptance.support.process import HarnessFailure
from tooling.acceptance.support import http as HTTP
from tooling.acceptance.fixtures import task_rollout_worker_fixture as FIXTURE


def read_marker(directory, task_id):
    path = FIXTURE.paths(directory, task_id)["blocked.json"]
    if not path.exists():
        return None
    value = json.loads(path.read_text(encoding="utf-8"))
    if value.get("workflow_id") != task_id or value.get("step") != FIXTURE.BARRIER_STEP:
        raise HarnessFailure("A rollout barrier lost its Task or step identity")
    return value


def touch_gate(directory, task_id, name):
    path = FIXTURE.paths(directory, task_id)[name]
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT, 0o600)
    os.close(descriptor)


def check_takeover(before, after):
    for key in ("workflow_id", "run_id", "activity_id", "step", "request_sha256"):
        if before.get(key) != after.get(key):
            raise HarnessFailure("Worker takeover changed the original Activity at " + key)
    if after["pid"] == before["pid"] or not before["attempt"] < after["attempt"] <= 3:
        raise HarnessFailure("Worker takeover did not retry within the native attempt budget on a new process")


def concurrency_evidence(directory, process_end_times):
    """Count actual model coroutine lifetimes; missing exits require process death."""
    intervals = []
    ignored = []
    for path in sorted(directory.glob("*.started.json")):
        started = json.loads(path.read_text(encoding="utf-8"))
        finished_path = path.with_name(path.name.replace(".started.json", ".finished.json"))
        if finished_path.exists():
            finished = json.loads(finished_path.read_text(encoding="utf-8"))
            end, outcome = finished["finished_monotonic_ns"], finished["outcome"]
        elif started["pid"] in process_end_times:
            end, outcome = process_end_times[started["pid"]], "process_exited"
        else:
            raise HarnessFailure("An observed model Activity lacks a finish or authoritative process exit")
        if end < started["started_monotonic_ns"]:
            raise HarnessFailure("An Activity interval ends before it starts")
        intervals.append({**started, "finished_monotonic_ns": end, "outcome": outcome})
    if not intervals:
        raise HarnessFailure("No model Activity execution intervals were observed")
    events = [(item["started_monotonic_ns"], 1) for item in intervals]
    events += [(item["finished_monotonic_ns"], -1) for item in intervals]
    active = maximum = 0
    for _, delta in sorted(events):
        active += delta
        maximum = max(maximum, active)
        if active < 0:
            raise HarnessFailure("Activity interval accounting became negative")
    if active != 0 or maximum > 3:
        raise HarnessFailure("Observed model concurrency exceeded three configured Activity slots")
    for path in directory.glob("*.ignored-cancellation.json"):
        ignored.append(json.loads(path.read_text(encoding="utf-8")))
    return {"maximum_active_model_activities": maximum, "activity_attempts": len(intervals),
            "outcomes": {name: sum(row["outcome"] == name for row in intervals)
                         for name in sorted({row["outcome"] for row in intervals})},
            "intervals": intervals, "ignored_cancellations": ignored}


def check_completed(result, record):
    snapshot = result.get("result", {})
    identity = {"task_id": record["task_id"], "workflow_id": record["task_id"], "owner": record["owner"],
                "first_execution_run_id": record["first_execution_run_id"]}
    if (result.get("task_id") != record["task_id"] or result.get("status") != "succeeded"
            or result.get("ready") is not True or result.get("version") != "task-v2"
            or snapshot.get("identity") != identity or snapshot.get("status") != "completed"
            or snapshot.get("schema_version") != 2 or snapshot.get("can_count", 0) < 1):
        raise HarnessFailure("An accepted rollout Task did not complete under its original owner/identity and at least one CAN")
    messages = snapshot.get("messages", [])
    if sum(row.get("type") == "human" for row in messages) != 1:
        raise HarnessFailure("A rollout Task duplicated its original Human message")
    calls = [row["data"].get("tool_call_id") for row in messages if row.get("type") == "tool"]
    if calls != [f"rollout-{index:03d}" for index in range(FIXTURE.STEPS)]:
        raise HarnessFailure("A rollout Task lost, duplicated or reordered its native tool results")
    for step in (1, 3, 5, 7):
        file = snapshot.get("files", {}).get(f"/rollout-{step}.md", {})
        if file.get("content") != f"rollout:{step}" or not file.get("created_at") or not file.get("modified_at"):
            raise HarnessFailure("A rollout Task lost its native file content or metadata")
    if snapshot.get("todos") != [{"content": "finish durable rollout task", "status": "completed"}]:
        raise HarnessFailure("A rollout Task did not preserve and finish its native plan")
    return {"task_id": record["task_id"], "owner": record["owner"], "first_execution_run_id": record["first_execution_run_id"],
            "final_run_id": result["run_id"], "status": result["status"], "can_count": snapshot["can_count"],
            "native_tool_steps": len(calls), "snapshot_sha256": hashlib.sha256(FIXTURE.canonical(snapshot)).hexdigest()}


async def wait_exit(processes, record, expected_code, *, timeout=9):
    async def exited():
        return record.process.poll()
    code = await SUPPORT.eventually(processes, exited, lambda value: value is not None, timeout=timeout)
    if code != expected_code:
        raise HarnessFailure("Worker shutdown returned an unexpected exit code")
    return {"pid": record.process.pid, "returncode": code, "exited_monotonic_ns": time.monotonic_ns()}


async def admission_backpressure(processes, observer, url, token, target, client):
    from temporalio.service import RPCError, RPCStatusCode
    host, port_text = url.removeprefix("http://").rsplit(":", 1)
    connections = []
    try:
        for index in range(2):
            reader, writer = await asyncio.open_connection(host, int(port_text))
            body = b'{"message":"holding admission"}'
            request = (f"POST /v1/tasks HTTP/1.1\r\nHost: {host}:{port_text}\r\nAuthorization: Bearer {token}\r\n"
                       f"Idempotency-Key: held-body-{index}\r\nContent-Type: application/json\r\nContent-Length: {len(body)}\r\n\r\n")
            writer.write(request.encode("ascii"))
            await writer.drain()
            connections.append((reader, writer))
        async def pressure():
            return await observer.get(url + "/v1/tasks/task-" + "0" * 64, headers={"Authorization": "Bearer " + token})
        await SUPPORT.eventually(processes, pressure, lambda response: response.status_code == 429, timeout=4)
        response = await observer.post(url + "/v1/tasks", headers={"Authorization": "Bearer " + token,
            "Idempotency-Key": target["key"]}, json={"message": target["message"]})
        value = HTTP.decode(response, 429)
        if value.get("error", {}).get("code") != "api_capacity_exceeded":
            raise HarnessFailure("The saturated API did not return its admission-capacity error")
        try:
            await client.get_workflow_handle(target["task_id"]).describe()
        except RPCError as error:
            if error.status != RPCStatusCode.NOT_FOUND:
                raise
        else:
            raise HarnessFailure("A rejected submission unexpectedly created a native Workflow")
    finally:
        for _, writer in connections:
            writer.close()
        for _, writer in connections:
            try:
                await writer.wait_closed()
            except (ConnectionError, OSError):
                pass
    async def recovered():
        return await observer.get(url + "/v1/tasks/task-" + "0" * 64, headers={"Authorization": "Bearer " + token})
    await SUPPORT.eventually(processes, recovered, lambda response: response.status_code == 404, timeout=4)
    return {"held_authenticated_requests": 2, "configured_max_inflight": 2,
            "rejected_http_status": 429, "error_code": "api_capacity_exceeded",
            "native_workflow_not_found_after_rejection": True, "retry_task_id": target["task_id"]}
