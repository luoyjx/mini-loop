"""Shared http support for bounded acceptance scenarios."""
from __future__ import annotations

import hashlib
import json
import os
import socket
import sys
from uuid import uuid4
import httpx

from tooling.acceptance.support import process as SUPPORT
from tooling.acceptance.support.process import HarnessFailure


def free_ports(count):
    sockets = []
    try:
        for _ in range(count):
            listener = socket.socket()
            listener.bind(("127.0.0.1", 0))
            sockets.append(listener)
        return [listener.getsockname()[1] for listener in sockets]
    finally:
        for listener in sockets:
            listener.close()


def marker_path(directory, task_id):
    return directory / (hashlib.sha256(task_id.encode()).hexdigest() + ".blocked.json")


def read_marker(directory, task_id):
    try:
        value = json.loads(marker_path(directory, task_id).read_text())
    except (OSError, ValueError):
        return None
    if value.get("workflow_id") != task_id:
        raise HarnessFailure("The controlled Worker marker belongs to a different Task")
    return value


def release(directory, task_id):
    name = hashlib.sha256(task_id.encode()).hexdigest() + ".release"
    descriptor = os.open(directory / name, os.O_WRONLY | os.O_CREAT, 0o600)
    os.close(descriptor)


def decode(response, expected):
    if response.status_code != expected:
        raise HarnessFailure(f"Expected HTTP {expected}, received {response.status_code}")
    try:
        value = response.json()
    except ValueError:
        raise HarnessFailure("The Task API returned invalid JSON") from None
    if not isinstance(value, dict):
        raise HarnessFailure("The Task API response is not a JSON object")
    return value


def check_page(page, task_id, run_id):
    if page.get("task_id") != task_id or page.get("run_id") != run_id or page.get("order") != "descending":
        raise HarnessFailure("A durable event page changed Task, Run, or ordering identity")
    events = page.get("events")
    if not isinstance(events, list) or not events:
        raise HarnessFailure("A required durable event page was empty")
    ids = [event.get("event_id") for event in events]
    if any(type(event_id) is not int or event_id < 1 for event_id in ids):
        raise HarnessFailure("A durable event page lacks valid Temporal event IDs")
    if ids != sorted(set(ids), reverse=True):
        raise HarnessFailure("A durable event page repeats or reorders Temporal event IDs")
    for event in events:
        if not isinstance(event.get("type"), str) or not isinstance(event.get("time"), str):
            raise HarnessFailure("A durable event summary lacks native event type/time")
        if any(key in event for key in ("messages", "input", "payloads", "token")):
            raise HarnessFailure("The event endpoint exposed payload/token data instead of lifecycle summaries")
    cursor = page.get("next_cursor")
    if cursor is not None and not isinstance(cursor, str):
        raise HarnessFailure("The durable event cursor has an invalid type")
    return ids


def check_preserved_state(before, after):
    """Compare native state without normalizing message IDs or file metadata."""
    for field in ("identity", "schema_version", "version", "files"):
        difference = SUPPORT.first_difference(before[field], after.get(field), field)
        if difference:
            raise HarnessFailure("The completed Task changed preserved state at " + difference)
    messages = after.get("messages", [])
    if len(messages) <= len(before["messages"]):
        raise HarnessFailure("The completed Task did not extend the original transcript")
    difference = SUPPORT.first_difference(before["messages"], messages[:len(before["messages"])], "messages")
    if difference:
        raise HarnessFailure("The completed Task changed preserved state at " + difference)
    todos = after.get("todos", [])
    if not before["todos"] or len(todos) != len(before["todos"]):
        raise HarnessFailure("The completed Task changed its original plan length")
    for index, (original, completed) in enumerate(zip(before["todos"], todos, strict=True)):
        difference = SUPPORT.first_difference({k: v for k, v in original.items() if k != "status"},
                                               {k: v for k, v in completed.items() if k != "status"},
                                               f"todos[{index}]")
        if difference or completed.get("status") != "completed":
            raise HarnessFailure("The completed Task did not preserve and complete its plan at " + (difference or f"todos[{index}].status"))
    if after.get("status") != "completed":
        raise HarnessFailure("The completed Task snapshot lacks its terminal state")
    if "state" in before:
        initial, completed = before["state"], after.get("state")
        if not isinstance(initial, dict) or not isinstance(completed, dict):
            raise HarnessFailure("The completed Task lost its full native state")
        for key, original in initial.items():
            expected = original
            if key == "messages":
                candidate = completed.get(key, [])[:len(original)]
            elif key == "todos":
                # A final native write_todos completes this exact original plan.
                expected = [{**item, "status": "completed"} for item in original]
                candidate = completed.get(key)
            else:
                candidate = completed.get(key)
            difference = SUPPORT.first_difference(expected, candidate, "state." + key)
            if difference:
                raise HarnessFailure("The completed Task changed preserved native state at " + difference)


def check_snapshot_contract(snapshot, task_id, run_id, version, status):
    schemas = {"task-v1": 1, "task-v2": 2}
    identity = {"task_id": task_id, "workflow_id": task_id, "owner": "alice"}
    if version == "task-v2":
        identity["first_execution_run_id"] = run_id
        if not isinstance(snapshot.get("state"), dict):
            raise HarnessFailure("A V2 Task lacks its complete native state")
    if (version not in schemas or snapshot.get("identity") != identity
            or snapshot.get("schema_version") != schemas[version]
            or snapshot.get("version") != version or snapshot.get("status") != status):
        raise HarnessFailure("The Task snapshot lost its trusted version, identity, schema, or status")


def legacy_submission(owner, key, message, task_queue):
    """Frozen original v1 Start contract, submitted only through trusted RPC.

    This deliberately does not call the current TaskService submit implementation:
    an already-existing v1 execution must remain readable/reusable after upgrade.
    """
    canonical = lambda value: json.dumps(value, sort_keys=True, ensure_ascii=False,
                                         separators=(",", ":"), allow_nan=False).encode()
    task_id = "task-" + hashlib.sha256(b"mini-loop:task-id:v1\0" + canonical([owner, key])).hexdigest()
    payload = {"task_id": task_id, "owner": owner, "message": message, "version": "task-v1"}
    digest = hashlib.sha256(canonical({"payload": payload, "task_queue": task_queue})).hexdigest()
    return {"task_id": task_id, "payload": payload,
            "memo": {"owner": owner, "request_sha256": digest, "version": "task-v1", "task_id": task_id}}


def check_step_limit_history(history):
    """Require a real terminal History failure after repeated model Activities."""
    failed = [event.workflow_execution_failed_event_attributes for event in history.events
              if event.HasField("workflow_execution_failed_event_attributes")]
    if len(failed) != 1:
        raise HarnessFailure("The exhausted Task lacks one native WorkflowExecutionFailed event")
    failure = failed[0].failure
    if (not failure.HasField("application_failure_info")
            or failure.application_failure_info.type != "task_step_limit"
            or not failure.application_failure_info.non_retryable):
        raise HarnessFailure("The graph limit did not become the expected nonretryable native Task failure")
    model_scheduled = sum(event.HasField("activity_task_scheduled_event_attributes")
                          and event.activity_task_scheduled_event_attributes.activity_type.name == "deepagents.invoke_model"
                          for event in history.events)
    model_completed = sum(event.HasField("activity_task_completed_event_attributes") for event in history.events)
    workflow_task_failed = sum(event.HasField("workflow_task_failed_event_attributes") for event in history.events)
    if model_scheduled < 2 or model_completed < 2 or workflow_task_failed:
        raise HarnessFailure("The exhausted Task did not finish a real repeated-model loop without WorkflowTask failure retries")
    return {"failure_type": failure.application_failure_info.type, "non_retryable": True,
            "model_activities_scheduled": model_scheduled, "activities_completed": model_completed,
            "workflow_task_failed_count": workflow_task_failed}


async def ready(processes, observer, url):
    async def health():
        try:
            return await observer.get(url + "/healthz")
        except httpx.TransportError:
            return None
    await SUPPORT.eventually(processes, health,
                             lambda response: response is not None and response.status_code == 200, timeout=25)


async def read_until(processes, observer, url, token, predicate, *, timeout=25):
    async def read():
        try:
            response = await observer.get(url, headers={"Authorization": "Bearer " + token})
        except httpx.TransportError:
            return None
        if response.status_code == 503:
            return None
        return response
    return await SUPPORT.eventually(processes, read,
                                   lambda response: response is not None and predicate(response), timeout=timeout)


PRODUCER_CODE = """
import httpx, json, os
request = json.loads(os.environ['MINILOOP_FAULT_SUBMISSION'])
with httpx.Client(timeout=10, follow_redirects=False, trust_env=False) as client:
    response = client.post(request['url'] + '/v1/tasks',
        headers={'Authorization': 'Bearer ' + request['token'], 'Idempotency-Key': request['key']},
        json={'message': request['message']})
    descriptor = os.open(os.environ['MINILOOP_FAULT_RESPONSE'], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'w') as stream:
        json.dump({'status': response.status_code, 'body': response.json()}, stream)
"""


async def detached_submit(processes, url, token, key, message):
    # A separate owned producer process must exit before acceptance is observed.
    # Secrets stay in its environment and never enter the command or response file.
    name = "producer-" + uuid4().hex[:12]
    path = processes.directory / (name + ".json")
    record = processes.spawn(name, [sys.executable, "-c", PRODUCER_CODE], extra_environment={
        "MINILOOP_FAULT_SUBMISSION": json.dumps({"url": url, "token": token, "key": key, "message": message}),
        "MINILOOP_FAULT_RESPONSE": str(path),
    })

    async def status():
        return record.process.poll()

    code = await SUPPORT.eventually(processes, status, lambda value: value is not None, timeout=12)
    if code != 0:
        raise HarnessFailure("An owned submission producer did not exit successfully; see its private log")
    value = json.loads(path.read_text())
    return httpx.Response(value["status"], json=value["body"])
