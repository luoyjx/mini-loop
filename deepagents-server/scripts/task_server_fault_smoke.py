"""Real HTTP Task API fault acceptance on a private local Temporal service.

Two API replicas share only Temporal Workflow History as durable Task authority.
The private test Worker drives real Deep Agents with a deterministic model; no
provider keys, business SQLite ledger, or automatic server download are used.
"""

from __future__ import annotations

import argparse
import asyncio
from datetime import timedelta
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import secrets
import signal
import socket
import sys
import time
import traceback
from uuid import uuid4

import httpx


MODULE = Path(__file__).resolve().parents[1]


def load_support():
    """Reuse the audited process/resource implementation rather than fork it."""
    name = "_mini_loop_temporal_fault_process_support"
    if name in sys.modules:
        return sys.modules[name]
    path = Path(__file__).with_name("temporal_fault_smoke.py")
    specification = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(specification)
    sys.modules[name] = module
    specification.loader.exec_module(module)
    return module


SUPPORT = load_support()
HarnessFailure = SUPPORT.HarnessFailure
ResourceBlocked = SUPPORT.ResourceBlocked


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


async def exercise(processes, binary, directory, run_id, receipt, persist, *, restart_temporal):
    temporal_port, first_api_port, second_api_port = free_ports(3)
    address, queue = f"127.0.0.1:{temporal_port}", "http-fault-" + run_id
    temporal_database = directory / "temporal.sqlite"
    barriers = directory / "barriers"
    barriers.mkdir(mode=0o700)
    urls = [f"http://127.0.0.1:{port}" for port in (first_api_port, second_api_port)]
    server_arguments = [binary, "--disable-config-env", "--disable-config-file", "--log-level", "warn",
                        "server", "start-dev", "--ip", "127.0.0.1", "--port", str(temporal_port),
                        "--headless", "--db-filename", str(temporal_database)]

    def server(name):
        return processes.spawn(name, server_arguments,
                               extra_environment={"GOMAXPROCS": "2", "GOMEMLIMIT": "512MiB"})

    def worker(name):
        return processes.spawn(name, [sys.executable, str(MODULE / "tests/task_worker_fixture.py"),
                                      "--address", address, "--task-queue", queue, "--identity", name,
                                      "--barrier-directory", str(barriers)])

    alice, bob = secrets.token_urlsafe(32), secrets.token_urlsafe(32)
    environment = {"MINILOOP_DEEPAGENTS_TOKENS": json.dumps({alice: "alice", bob: "bob"}),
                   "MINILOOP_TASK_SERVER_CURSOR_SECRET": secrets.token_urlsafe(32)}

    def api(name, port):
        return processes.spawn(name, [sys.executable, "-m", "mini_loop_deepagents.task_server.api",
                                    "--address", f"127.0.0.1:{port}", "--temporal-address", address,
                                    "--namespace", "default", "--task-queue", queue], extra_environment=environment)

    temporal = server("temporal-1")
    temporal_client = await SUPPORT.connect(processes, address)
    workers = [worker("worker-a"), worker("worker-b")]
    apis = [api("api-a", first_api_port), api("api-b", second_api_port)]
    receipt.update(temporal_database=str(temporal_database), authority="Temporal Workflow History only",
                   idempotency_scope="retained Temporal Workflow history; no permanent or cross-retention guarantee",
                   task_queue=queue, api_urls=urls)
    persist()

    async with httpx.AsyncClient(timeout=5, follow_redirects=False, trust_env=False) as observer:
        for url in urls:
            await ready(processes, observer, url)
        key_a, key_b = "key-a-" + run_id, "key-b-" + run_id
        message_a, message_b = "HTTP fault task A " + run_id, "HTTP fault task B " + run_id
        responses = await asyncio.gather(*(detached_submit(processes, url, alice, key_a, message_a) for url in urls))
        admitted = [decode(response, 202) for response in responses]
        task_a = admitted[0].get("task_id")
        if not isinstance(task_a, str) or not task_a.startswith("task-") or admitted[1].get("task_id") != task_a:
            raise HarnessFailure("Two API replicas did not converge on the same accepted Task ID")
        if sorted(value.get("reused") for value in admitted) != [False, True]:
            raise HarnessFailure("The competing submissions did not distinguish one create and one reuse")
        if any(value.get("version") != "task-v2" for value in admitted):
            raise HarnessFailure("The production API did not select the current V2 Workflow for new Tasks")

        async def count():
            return (await temporal_client.count_workflows(query=f'WorkflowId = "{task_a}"')).count
        count_value = await SUPPORT.eventually(processes, count, lambda value: value == 1, timeout=10)
        receipt.update(task_a=task_a, cross_replica_race={"response_task_ids": [item["task_id"] for item in admitted],
                                                       "reused": [item["reused"] for item in admitted],
                                                       "native_temporal_workflow_count": count_value})
        receipt["passed"].append("two independent HTTP replicas race one owner/idempotency key: HTTP 202 responses identify exactly one native Temporal Workflow")
        persist()

        conflict = await detached_submit(processes, urls[1], alice, key_a, message_a + " changed")
        decode(conflict, 409)
        for suffix, method in (("", "GET"), ("/result", "GET"), ("/events", "GET"), ("/cancel", "POST")):
            response = await observer.request(method, urls[1] + "/v1/tasks/" + task_a + suffix,
                                              headers={"Authorization": "Bearer " + bob})
            decode(response, 404)
        receipt["passed"].append("changed payload under the same retained key returns 409; foreign owner read/result/events/cancel return 404")

        task_b = decode(await detached_submit(processes, urls[1], alice, key_b, message_b), 202)["task_id"]
        receipt["task_b"] = task_b
        if task_b == task_a:
            raise HarnessFailure("Independent messages/keys were collapsed into one Task")

        async def markers():
            return [read_marker(barriers, task_id) for task_id in (task_a, task_b)]
        blocked = await SUPPORT.eventually(processes, markers, lambda value: all(value), timeout=30)
        if {item["pid"] for item in blocked} != {record.process.pid for record in workers}:
            raise HarnessFailure("Two accepted Tasks did not reach the real Deep Agents barrier on two independent Workers")
        run_ids = {task_id: (await temporal_client.get_workflow_handle(task_id).describe()).run_id
                   for task_id in (task_a, task_b)}
        initial_snapshots = {task_id: await temporal_client.get_workflow_handle(task_id).query("snapshot")
                             for task_id in (task_a, task_b)}
        for task_id, snapshot in initial_snapshots.items():
            check_snapshot_contract(snapshot, task_id, run_ids[task_id], "task-v2", "running")
            if (not snapshot.get("messages")
                    or not snapshot.get("files") or not snapshot.get("todos")):
                raise HarnessFailure("An accepted blocked Task lacks its native running state")
            SUPPORT.private_json(directory / (task_id + "-initial-snapshot.json"), snapshot, run_id)
        receipt["initial_snapshot_files"] = {task_id: str(directory / (task_id + "-initial-snapshot.json"))
                                             for task_id in (task_a, task_b)}
        receipt["blocked_worker_markers"] = blocked
        receipt["accepted_run_ids"] = run_ids
        receipt["passed"].append("detached producers exit after HTTP 202; two different Tasks execute real Deep Agents planning/file/model steps concurrently on independent Worker PIDs")

        page_response = await observer.get(urls[0] + "/v1/tasks/" + task_a + "/events", params={"limit": 2},
                                           headers={"Authorization": "Bearer " + alice})
        first_page = decode(page_response, 200)
        event_ids = check_page(first_page, task_a, run_ids[task_a])
        cursor = first_page["next_cursor"]
        if len(event_ids) != 2 or not cursor:
            raise HarnessFailure("The pre-restart history did not produce a bounded resumable event page")
        receipt["event_pagination"] = {"first_page_ids": event_ids[:], "cursor_sha256": hashlib.sha256(cursor.encode()).hexdigest()}
        persist()

        old_apis = [record.process.pid for record in apis]
        for record in apis:
            await asyncio.to_thread(processes.kill, record)
        apis = [api("api-c", first_api_port), api("api-d", second_api_port)]
        for url in urls:
            await ready(processes, observer, url)
        for task_id in (task_a, task_b):
            response = await read_until(processes, observer, urls[0] + "/v1/tasks/" + task_id, alice,
                                        lambda value: value.status_code == 200)
            state = decode(response, 200)
            if state.get("task_id") != task_id or state.get("run_id") != run_ids[task_id] or state.get("status") != "running":
                raise HarnessFailure("Full API replacement changed or lost an accepted running Task")
        pending = decode(await observer.get(urls[1] + "/v1/tasks/" + task_b + "/result",
                                            headers={"Authorization": "Bearer " + alice}), 202)
        if pending.get("ready") is not False:
            raise HarnessFailure("A blocked Task result did not remain asynchronous after API replacement")
        receipt["api_restart"] = {"old_pids": old_apis, "new_pids": [record.process.pid for record in apis]}
        receipt["passed"].append("all HTTP API processes SIGKILL/restart: accepted Task IDs, active Run IDs, and asynchronous result handles survive without application-local durable state")

        pages = 1
        while cursor is not None:
            processes.check()
            response = await observer.get(urls[1] + "/v1/tasks/" + task_a + "/events",
                                           params={"limit": 2, "cursor": cursor}, headers={"Authorization": "Bearer " + alice})
            page = decode(response, 200)
            ids = check_page(page, task_a, run_ids[task_a])
            if ids[0] >= event_ids[-1]:
                raise HarnessFailure("A resumed event page overlaps or moves ahead of its previous cursor")
            event_ids.extend(ids)
            cursor = page["next_cursor"]
            pages += 1
            if pages > 100:
                raise HarnessFailure("Durable event pagination exceeded its bounded page budget")
        if event_ids != list(range(event_ids[0], 0, -1)):
            raise HarnessFailure("Cross-replica event cursor replay dropped or duplicated native Temporal history events")
        if page["events"][-1]["type"] != "workflow_execution_started":
            raise HarnessFailure("The durable history cursor did not reach WorkflowExecutionStarted")
        receipt["event_pagination"].update(page_count=pages, all_event_ids=event_ids,
                                            oldest_type=page["events"][-1]["type"])
        receipt["passed"].append("signed durable lifecycle-event cursor continues across API replacement and another replica, with contiguous descending Temporal event IDs through event 1")
        persist()

        if restart_temporal:
            old_server_pid = temporal.process.pid
            for record in [temporal, *workers]:
                await asyncio.to_thread(processes.kill, record)
            if not temporal_database.is_file() or temporal_database.stat().st_size == 0:
                raise HarnessFailure("The owned Temporal service did not retain its SQLite history")
            temporal = server("temporal-2")
            await SUPPORT.connect(processes, address)
            workers = [worker("worker-c"), worker("worker-d")]
            for task_id in (task_a, task_b):
                response = await read_until(processes, observer, urls[0] + "/v1/tasks/" + task_id, alice,
                                            lambda value: value.status_code == 200, timeout=30)
                value = decode(response, 200)
                if value.get("run_id") != run_ids[task_id] or value.get("status") != "running":
                    raise HarnessFailure("Temporal/Worker replacement changed an already accepted Task identity")

            def retries_visible(value):
                return all(value) and {item["pid"] for item in value} == {record.process.pid for record in workers} and all(item["attempt"] > 1 for item in value)
            retried = await SUPPORT.eventually(processes, markers, retries_visible, timeout=30)
            for task_id in (task_a, task_b):
                recovered = await temporal_client.get_workflow_handle(task_id).query("snapshot")
                difference = SUPPORT.first_difference(initial_snapshots[task_id], recovered, "snapshot")
                if difference:
                    raise HarnessFailure("Cold replay changed the full native V2 state at " + difference)
            receipt["temporal_restart"] = {"old_server_pid": old_server_pid, "new_server_pid": temporal.process.pid,
                                            "cold_worker_markers": retried, "preserved_run_ids": run_ids}
            receipt["passed"].append("Temporal and both Workers SIGKILL/restart on the same history database: live API replicas reconnect and new Worker PIDs retry both Tasks under their original Run identities")
            persist()
        else:
            receipt["skipped"] = ["optional same-database Temporal/Worker process replacement; separate prototype gate covers this boundary"]

        decode(await observer.post(urls[0] + "/v1/tasks/" + task_a + "/cancel",
                                    headers={"Authorization": "Bearer " + alice}), 202)
        cancelled_response = await read_until(processes, observer, urls[1] + "/v1/tasks/" + task_a, alice,
                                               lambda response: response.status_code == 200 and response.json().get("status") == "cancelled")
        cancelled = decode(cancelled_response, 200)
        if cancelled.get("run_id") != run_ids[task_a]:
            raise HarnessFailure("Cancellation changed the accepted Run identity")
        still_running = decode(await observer.get(urls[1] + "/v1/tasks/" + task_b,
                                                  headers={"Authorization": "Bearer " + alice}), 200)
        if still_running.get("status") != "running":
            raise HarnessFailure("Cancelling one Task stopped a different running Task")
        release(barriers, task_b)
        final_response = await read_until(processes, observer, urls[1] + "/v1/tasks/" + task_b + "/result", alice,
                                           lambda response: response.status_code == 200 and response.json().get("ready") is True, timeout=30)
        final = decode(final_response, 200)
        snapshot = final.get("result")
        if final.get("status") != "succeeded" or final.get("run_id") != run_ids[task_b] or not isinstance(snapshot, dict):
            raise HarnessFailure("The detached Task did not return its completed result under the accepted Run identity")
        check_snapshot_contract(snapshot, task_b, run_ids[task_b], "task-v2", "completed")
        if snapshot.get("files", {}).get("/plan.md", {}).get("content") != "plan:" + message_b:
            raise HarnessFailure("The completed HTTP result lost the real Deep Agents virtual file")
        check_preserved_state(initial_snapshots[task_b], snapshot)
        history = await temporal_client.get_workflow_handle(task_b).fetch_history()
        activity_types = sorted({event.activity_task_scheduled_event_attributes.activity_type.name for event in history.events
                                 if event.HasField("activity_task_scheduled_event_attributes")})
        if "deepagents.invoke_model" not in activity_types:
            raise HarnessFailure("Temporal history lacks independently persisted model Activities")
        duplicate = decode(await detached_submit(processes, urls[0], alice, key_b, message_b), 202)
        if duplicate.get("task_id") != task_b or duplicate.get("reused") is not True:
            raise HarnessFailure("A repeated completed submission was not reconciled with its retained Workflow history")
        receipt.update(cancelled_task=cancelled, completed_result=final, observed_activity_types=activity_types)
        receipt["passed"].append("after API replacement, owner cancellation terminates only its selected Task; another detached Task preserves its full original message prefix/file metadata/plan content, completes its todos, and reuses retained-history duplicate submission")
        persist()

        from temporalio.common import WorkflowIDConflictPolicy, WorkflowIDReusePolicy
        legacy_key, legacy_message = "key-loop-" + run_id, "[fixture:loop-limit] " + run_id
        legacy = legacy_submission("alice", legacy_key, legacy_message, queue)
        task_limit = legacy["task_id"]
        legacy_handle = await temporal_client.start_workflow(
            "TaskWorkflow", legacy["payload"], id=task_limit, task_queue=queue, memo=legacy["memo"],
            id_reuse_policy=WorkflowIDReusePolicy.REJECT_DUPLICATE,
            id_conflict_policy=WorkflowIDConflictPolicy.FAIL,
            execution_timeout=timedelta(hours=24), rpc_timeout=timedelta(seconds=5))
        reused_legacy = decode(await detached_submit(processes, urls[0], alice, legacy_key, legacy_message), 202)
        if (reused_legacy.get("task_id") != task_limit or reused_legacy.get("reused") is not True
                or reused_legacy.get("version") != "task-v1"
                or reused_legacy.get("run_id") != legacy_handle.first_execution_run_id):
            raise HarnessFailure("The new production API failed to reuse an existing native v1 Task")
        decode(await detached_submit(processes, urls[1], alice, legacy_key, legacy_message + " changed"), 409)
        for suffix, method in (("", "GET"), ("/result", "GET"), ("/events", "GET"), ("/cancel", "POST")):
            decode(await observer.request(method, urls[1] + "/v1/tasks/" + task_limit + suffix,
                headers={"Authorization": "Bearer " + bob}), 404)
        receipt["step_limit_task_id"] = task_limit
        receipt["legacy_v1_compatibility"] = {"created_via": "trusted Temporal Start using original v1 contract",
            "workflow_type": "TaskWorkflow", "version": reused_legacy["version"], "http_reused": True,
            "payload_conflict_status": 409, "foreign_owner_status": 404,
            "first_execution_run_id": legacy_handle.first_execution_run_id}
        persist()
        failed_response = await read_until(processes, observer, urls[1] + "/v1/tasks/" + task_limit, alice,
                                            lambda response: response.status_code == 200 and response.json().get("status") == "failed",
                                            timeout=60)
        failed_summary = decode(failed_response, 200)
        failed_result = decode(await observer.get(urls[0] + "/v1/tasks/" + task_limit + "/result",
                                                  headers={"Authorization": "Bearer " + alice}), 200)
        if (failed_result.get("status") != "failed" or failed_result.get("ready") is not True
                or failed_result.get("result") is not None or failed_result.get("error", {}).get("code") != "workflow_failed"
                or failed_result.get("run_id") != failed_summary.get("run_id")):
            raise HarnessFailure("The graph step limit did not expose a completed HTTP failure result")
        if read_marker(barriers, task_limit) is not None:
            raise HarnessFailure("The endless planning fixture unexpectedly entered a model barrier")
        failure_evidence = check_step_limit_history(await temporal_client.get_workflow_handle(task_limit).fetch_history())
        receipt["step_limit_failure"] = {"task_id": task_limit, "run_id": failed_result["run_id"],
                                         "configured_graph_step_limit": 128, "http_status": failed_result["status"],
                                         "http_ready": failed_result["ready"], **failure_evidence}
        receipt["passed"].append("existing native v1 Task reuses through the upgraded API with conflict/owner isolation; its real loop exhausts the original 128-step bound with HTTP failed/ready and native task_step_limit, without WorkflowTaskFailed")
        persist()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--temporal-binary", type=Path, required=True)
    parser.add_argument("--binary-sha256", help="Expected SHA-256 of the already selected executable")
    parser.add_argument("--output-dir", type=Path, default=MODULE.parent / "var/task-server-prototype")
    parser.add_argument("--timeout-seconds", type=float, default=180)
    parser.add_argument("--restart-temporal", action="store_true", help="Also replace the Temporal service and both Workers while Tasks are active")
    args = parser.parse_args(argv)
    started, run_id = time.monotonic(), uuid4().hex
    directory = args.output_dir.resolve() / run_id
    directory.mkdir(parents=True, mode=0o700)
    receipt_path = directory / "receipt.json"
    receipt = {"run_id": run_id, "driver_pid": os.getpid(), "status": "running", "started_at": SUPPORT.timestamp(),
               "passed": [], "cleanup_confirmed": False, "production_ha_acceptance": False,
               "rss_budget_mib": 2048, "minimum_memory_free_percent": 15,
               "scope": "private local Temporal plus two real HTTP APIs and deterministic-model Deep Agents Workers",
               "excluded_claims": ["production cluster HA", "real provider inference", "permanent idempotency", "external-tool exactly-once", "token streaming replay"]}
    persist = lambda: SUPPORT.private_json(receipt_path, receipt, run_id)
    persist()
    processes = watcher = None
    exit_code = 1
    previous_sigterm = signal.getsignal(signal.SIGTERM)

    def interrupted(_signum, _frame):
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        raise HarnessFailure("The HTTP acceptance driver received SIGTERM")

    signal.signal(signal.SIGTERM, interrupted)
    try:
        if not 10 <= args.timeout_seconds <= 180:
            raise HarnessFailure("The overall run budget must be between 10 and 180 seconds")
        binary = args.temporal_binary.resolve()
        if not binary.is_file() or not os.access(binary, os.X_OK):
            raise HarnessFailure("An existing Temporal CLI executable is required; no download was attempted")
        with binary.open("rb") as stream:
            checksum = hashlib.file_digest(stream, "sha256").hexdigest()
        if args.binary_sha256 is not None and args.binary_sha256 != checksum:
            raise HarnessFailure("The Temporal executable checksum did not match")
        receipt.update(temporal_binary=str(binary), temporal_binary_sha256=checksum,
                       supplied_checksum_verified=args.binary_sha256 is not None)
        processes = SUPPORT.Processes(directory, started + args.timeout_seconds)
        watcher = SUPPORT.Watchdog(processes)
        watcher.start()

        async def bounded():
            async with asyncio.timeout(max(0.1, processes.deadline - 5 - time.monotonic())):
                await exercise(processes, binary, directory, run_id, receipt, persist,
                               restart_temporal=args.restart_temporal)

        asyncio.run(bounded())
        processes.check()
        receipt["status"] = "passed"
        exit_code = 0
    except ResourceBlocked as error:
        receipt.update(status="resource_blocked", reason=str(error))
        exit_code = 3
    except BaseException as error:
        receipt.update(status="failed", reason=str(error) if isinstance(error, HarnessFailure) else "Acceptance stopped: " + type(error).__name__)
        SUPPORT.private_text(directory / "driver-error.log", traceback.format_exc())
        receipt["failure_detail_file"] = str(directory / "driver-error.log")
    finally:
        if processes is not None:
            receipt["cleanup_confirmed"] = processes.stop_all(force=processes.failure is not None)
            if processes.failure is not None:
                receipt.update(status="resource_blocked" if isinstance(processes.failure, ResourceBlocked) else "failed", reason=str(processes.failure))
                exit_code = 3 if isinstance(processes.failure, ResourceBlocked) else 1
            receipt["processes"] = [{"name": item.name, "pid": item.process.pid, "returncode": item.process.poll(),
                                     "log": str(item.log_path)} for item in processes.records]
            if not receipt["cleanup_confirmed"]:
                receipt.update(status="failed", reason="Cleanup of the owned acceptance processes could not be confirmed")
                exit_code = 1
        else:
            receipt["cleanup_confirmed"] = True
        if watcher is not None:
            watcher.stop()
            receipt.update(peak_owned_tree_rss_mib=round(watcher.peak_kib / 1024, 1),
                           minimum_observed_memory_free_percent=watcher.minimum_free, resource_samples=watcher.samples)
        receipt.update(finished_at=SUPPORT.timestamp(), seconds=round(time.monotonic() - started, 2))
        persist()
        signal.signal(signal.SIGTERM, previous_sigterm)
        print(json.dumps({"run_id": run_id, "status": receipt["status"], "receipt": str(receipt_path),
                          "seconds": receipt["seconds"], "passed": receipt["passed"]}, indent=2))
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
