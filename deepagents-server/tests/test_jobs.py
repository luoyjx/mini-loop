"""Contract checks exercise the real pinned SDK through an HTTP MockTransport."""

import asyncio
import importlib.util
import json
import stat
from pathlib import Path
from uuid import UUID

import httpx
import pytest

from mini_loop_deepagents.jobs import (
    AsyncJobsClient, JobHandle, JobRequestError, JobResumeError, JobWaitTimeout,
    SubmissionUncertain, normalize_url,
)


URL = "http://127.0.0.1:2024"
TOKEN = "test-token-do-not-print"
THREAD = "10000000-0000-4000-8000-000000000001"
RUN = "20000000-0000-4000-8000-000000000001"
NEW_RUN = "20000000-0000-4000-8000-000000000002"
REQUEST = "30000000-0000-4000-8000-000000000001"


def job(**updates):
    fields = dict(server_url=URL, thread_id=THREAD, run_id=RUN, request_id=REQUEST)
    return JobHandle(**(fields | updates))


def run_data(status="pending", *, thread_id=THREAD, run_id=RUN):
    return {"thread_id": thread_id, "run_id": run_id, "status": status, "assistant_id": "agent"}


def response(data, status=200):
    return httpx.Response(status, json=data)


def client(handler, **options):
    return AsyncJobsClient(URL, TOKEN, transport=httpx.MockTransport(handler), **options)


@pytest.mark.parametrize("operation,expected", [
    ("create_thread", "no Run was submitted"),
    ("cancel", "submission metadata does not prove cancellation"),
    ("submit", "match metadata.request_id"),
    ("resume", "match metadata.request_id"),
])
def test_uncertain_write_reconciliation_matches_the_operation(operation, expected):
    error = SubmissionUncertain(server_url=URL, thread_id=THREAD, request_id=REQUEST,
                                operation=operation, run_id=RUN if operation == "cancel" else None)
    assert expected in error.to_dict()["reconcile"]


def test_handle_has_strict_secret_free_portable_schema():
    handle = job()
    assert JobHandle.from_json(handle.to_json()) == handle
    assert set(handle.to_dict()) == {
        "version", "server_url", "thread_id", "run_id", "request_id", "assistant_id",
    }
    assert TOKEN not in handle.to_json()
    for invalid in (
        handle.to_dict() | {"token": TOKEN},
        {key: value for key, value in handle.to_dict().items() if key != "version"},
        handle.to_dict() | {"version": True},
        handle.to_dict() | {"run_id": "../other"},
        handle.to_dict() | {"assistant_id": "foreign-graph"},
        handle.to_dict() | {"server_url": URL + "/"},
        [],
    ):
        with pytest.raises(ValueError):
            JobHandle.from_json(json.dumps(invalid))


@pytest.mark.parametrize("url", [
    "file:///tmp/server", "http://user:secret@host", "https://host/?token=secret",
    "https://host/#secret", "https://host\\other", "https://host:bad", "https://host/ bad",
])
def test_invalid_server_urls_are_rejected_without_echoing_values(url):
    with pytest.raises(ValueError) as error:
        normalize_url(url)
    assert "secret" not in str(error.value)


def test_url_normalization_and_constructor_input_validation():
    assert normalize_url("HTTP://LOCALHOST:2024///") == "http://localhost:2024"
    assert normalize_url("http://[::1]:2024/base/") == "http://[::1]:2024/base"
    for options in ({"poll_interval": 0}, {"poll_interval": float("nan")}, {"request_timeout": -1}):
        with pytest.raises(ValueError):
            client(lambda request: response({}), **options)
    with pytest.raises(ValueError):
        AsyncJobsClient(URL, "bad\nsecret")


async def test_submit_returns_detached_handles_for_independent_threads():
    requests = []

    def handler(request):
        requests.append(request)
        payload = json.loads(request.content)
        if request.url.path == "/threads":
            return response({"thread_id": payload["thread_id"]})
        thread_id = request.url.path.split("/")[2]
        return response(run_data(thread_id=thread_id))

    async with client(handler) as connection:
        first = await connection.submit("long task")
        second = await connection.submit("independent task")
    assert first.thread_id != second.thread_id
    assert len(requests) == 4
    assert {r.method for r in requests} == {"POST"}
    for request in requests:
        assert request.headers["Authorization"] == "Bearer " + TOKEN
        assert "x-api-key" not in request.headers
    payload = json.loads(requests[1].content)
    assert payload["input"]["messages"] == [{"role": "user", "content": "long task"}]
    assert payload["durability"] == "sync"
    assert payload["multitask_strategy"] == "enqueue"
    assert payload["metadata"] == {"request_id": first.request_id}
    assert "context" not in payload and "config" not in payload


async def test_explicit_thread_enqueues_without_creating_a_thread():
    requests = []

    def handler(request):
        requests.append(request)
        return response(run_data())

    async with client(handler) as connection:
        result = await connection.submit("continue", thread_id=THREAD)
    assert result.thread_id == THREAD
    assert len(requests) == 1
    assert requests[0].url.path == f"/threads/{THREAD}/runs"


@pytest.mark.parametrize("operation", ["status", "state", "wait", "cancel", "resume"])
async def test_handle_cannot_redirect_credentials_to_another_deployment(operation):
    requests = []

    def handler(request):
        requests.append(request)
        return response({})

    async with client(handler) as connection:
        with pytest.raises(ValueError, match="trusted server"):
            await getattr(connection, operation)(job(server_url="https://attacker.example"))
    assert requests == []


async def test_status_reads_run_while_state_reads_current_thread():
    paths = []

    def handler(request):
        paths.append(request.url.path)
        return response({"current_thread": True} if request.url.path.endswith("/state") else run_data("error"))

    async with client(handler) as connection:
        assert (await connection.status(job()))["status"] == "error"
        assert (await connection.state(job()))["current_thread"]
    assert paths == [f"/threads/{THREAD}/runs/{RUN}", f"/threads/{THREAD}/state"]


async def test_wait_polls_existing_run_through_transient_read_disconnect():
    requests = []

    def handler(request):
        requests.append(request)
        if len(requests) == 1:
            raise httpx.ReadError("not safe to display " + TOKEN, request=request)
        return response(run_data("running" if len(requests) == 2 else "success"))

    async with client(handler, poll_interval=0.001) as connection:
        result = await connection.wait(job(), timeout=1)
    assert result["status"] == "success"
    assert len(requests) == 3
    assert all(request.method == "GET" for request in requests)


@pytest.mark.parametrize("terminal", ["success", "error", "timeout", "interrupted"])
async def test_wait_returns_actual_terminal_status(terminal):
    async with client(lambda request: response(run_data(terminal))) as connection:
        assert (await connection.wait(job(), timeout=1))["status"] == terminal


async def test_wait_timeout_and_caller_cancellation_never_cancel_the_run():
    requests = []

    def handler(request):
        requests.append(request)
        return response(run_data("running"))

    async with client(handler, poll_interval=0.001) as connection:
        with pytest.raises(JobWaitTimeout) as error:
            await connection.wait(job(), timeout=0.01)
        assert error.value.job == job()
        pending = asyncio.create_task(connection.wait(job()))
        await asyncio.sleep(0.005)
        pending.cancel()
        with pytest.raises(asyncio.CancelledError):
            await pending
    assert requests and all(request.method == "GET" for request in requests)


async def test_wait_does_not_retry_authorization_failures():
    requests = []

    def handler(request):
        requests.append(request)
        return response({"detail": TOKEN}, 403)

    async with client(handler) as connection:
        with pytest.raises(JobRequestError) as error:
            await connection.wait(job(), timeout=1)
    assert len(requests) == 1
    assert error.value.status_code == 403 and TOKEN not in str(error.value)


async def test_cancel_is_a_single_nonblocking_interrupt_request():
    requests = []

    def handler(request):
        requests.append(request)
        return response(None)

    async with client(handler) as connection:
        await connection.cancel(job())
    assert len(requests) == 1
    request = requests[0]
    assert request.method == "POST" and request.url.path.endswith(f"/{RUN}/cancel")
    assert request.url.params["wait"] == "0" and request.url.params["action"] == "interrupt"


@pytest.mark.parametrize("terminal", ["error", "timeout", "interrupted"])
async def test_resume_is_new_run_from_checkpoint_without_repeating_input(terminal):
    requests = []

    def handler(request):
        requests.append(request)
        if request.method == "POST":
            return response(run_data(run_id=NEW_RUN))
        if request.url.path.endswith("/state"):
            return response({"next": ["model"], "interrupts": [], "tasks": []})
        if request.url.path.endswith("/runs"):
            return response([run_data(terminal)])
        return response(run_data(terminal))

    async with client(handler) as connection:
        resumed = await connection.resume(job())
    assert resumed.run_id == NEW_RUN and resumed.request_id != REQUEST
    assert resumed.thread_id == THREAD
    writes = [request for request in requests if request.method == "POST"]
    assert len(writes) == 1
    payload = json.loads(writes[0].content)
    assert payload.get("input") is None and "command" not in payload
    assert payload["durability"] == "sync" and payload["multitask_strategy"] == "reject"
    assert payload["metadata"]["request_id"] == resumed.request_id


@pytest.mark.parametrize("status,latest,state", [
    ("success", RUN, {"next": ["model"]}),
    ("pending", RUN, {"next": ["model"]}),
    ("running", RUN, {"next": ["model"]}),
    ("error", NEW_RUN, {"next": ["model"]}),
    ("error", RUN, {"next": []}),
    ("interrupted", RUN, {"next": ["model"], "interrupts": [{"value": "approve?"}]}),
    ("interrupted", RUN, {"next": ["model"], "tasks": [{"interrupts": [{"value": "approve?"}]}]}),
])
async def test_resume_rejects_unsafe_continuations_without_writing(status, latest, state):
    requests = []

    def handler(request):
        requests.append(request)
        if request.url.path.endswith("/state"):
            return response(state)
        if request.url.path.endswith("/runs"):
            return response([run_data(status, run_id=latest)])
        return response(run_data(status))

    async with client(handler) as connection:
        with pytest.raises(JobResumeError):
            await connection.resume(job())
    assert requests and all(request.method == "GET" for request in requests)


@pytest.mark.parametrize("failure", ["read_timeout", "http_500", "invalid_json", "missing_id", "cancelled"])
async def test_ambiguous_submission_is_never_retried_and_has_reconciliation_ids(failure):
    requests = []

    def handler(request):
        requests.append(request)
        if failure == "read_timeout":
            raise httpx.ReadTimeout(TOKEN, request=request)
        if failure == "http_500":
            return response({"detail": TOKEN}, 500)
        if failure == "invalid_json":
            return httpx.Response(200, content=b"invalid-json")
        if failure == "cancelled":
            raise asyncio.CancelledError
        return response({"status": "pending"})

    async with client(handler) as connection:
        with pytest.raises(SubmissionUncertain) as error:
            await connection.submit("private message", thread_id=THREAD)
    assert len(requests) == 1
    uncertain = error.value
    assert uncertain.thread_id == THREAD and uncertain.operation == "submit"
    assert str(UUID(uncertain.request_id)) == uncertain.request_id
    details = json.dumps(uncertain.to_dict())
    assert TOKEN not in details and "private message" not in details
    assert TOKEN not in str(uncertain)


async def test_uncertain_thread_creation_never_proceeds_to_run_creation():
    requests = []

    def handler(request):
        requests.append(request)
        raise httpx.ReadTimeout(TOKEN, request=request)

    async with client(handler) as connection:
        with pytest.raises(SubmissionUncertain) as error:
            await connection.submit("task")
    assert len(requests) == 1 and requests[0].url.path == "/threads"
    assert error.value.operation == "create_thread"
    assert json.loads(requests[0].content)["thread_id"] == error.value.thread_id


async def test_rejected_submission_is_explicit_and_redacted():
    requests = []

    def handler(request):
        requests.append(request)
        return response({"detail": TOKEN}, 409)

    async with client(handler) as connection:
        with pytest.raises(JobRequestError) as error:
            await connection.submit("task", thread_id=THREAD)
    assert len(requests) == 1 and error.value.status_code == 409
    assert TOKEN not in str(error.value)


async def test_uncertain_cancel_keeps_run_identifiers_without_retrying():
    requests = []

    def handler(request):
        requests.append(request)
        raise httpx.ReadTimeout(TOKEN, request=request)

    async with client(handler) as connection:
        with pytest.raises(SubmissionUncertain) as error:
            await connection.cancel(job())
    assert len(requests) == 1
    assert error.value.operation == "cancel" and error.value.run_id == RUN


@pytest.mark.parametrize("write_status", [409, 500])
async def test_resume_race_or_ambiguous_write_is_not_retried(write_status):
    requests = []

    def handler(request):
        requests.append(request)
        if request.method == "POST":
            return response({"detail": TOKEN}, write_status)
        if request.url.path.endswith("/state"):
            return response({"next": ["model"]})
        if request.url.path.endswith("/runs"):
            return response([run_data("error")])
        return response(run_data("error"))

    expected = JobRequestError if write_status == 409 else SubmissionUncertain
    async with client(handler) as connection:
        with pytest.raises(expected) as error:
            await connection.resume(job())
    assert len([request for request in requests if request.method == "POST"]) == 1
    assert TOKEN not in str(error.value)
    if isinstance(error.value, SubmissionUncertain):
        assert error.value.run_id == RUN and error.value.request_id != REQUEST


async def test_http_redirect_does_not_forward_bearer_credentials():
    requests = []

    def handler(request):
        requests.append(request)
        return httpx.Response(307, headers={"Location": "https://attacker.example/threads"})

    async with client(handler) as connection:
        with pytest.raises(JobRequestError):
            await connection.submit("task", thread_id=THREAD)
    assert len(requests) == 1 and requests[0].url.host == "127.0.0.1"


@pytest.fixture
def cli():
    path = Path(__file__).resolve().parents[1] / "scripts/jobs.py"
    specification = importlib.util.spec_from_file_location("jobs_cli_test", path)
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


def test_cli_handle_publication_is_atomic_private_and_requires_explicit_overwrite(cli, tmp_path):
    path = tmp_path / "job.json"
    cli.write_handle(path, job())
    assert cli.read_handle(path) == job()
    assert stat.S_IMODE(path.stat().st_mode) == 0o600
    replacement = job(run_id=NEW_RUN)
    with pytest.raises(FileExistsError):
        cli.write_handle(path, replacement)
    assert cli.read_handle(path) == job()
    cli.write_handle(path, replacement, overwrite=True)
    assert cli.read_handle(path) == replacement
    assert list(tmp_path.iterdir()) == [path]


def test_cli_checks_existing_output_before_submitting(cli, tmp_path, monkeypatch):
    path = tmp_path / "job.json"
    cli.write_handle(path, job())
    monkeypatch.setenv("MINILOOP_DEEPAGENTS_CLIENT_TOKEN", TOKEN)

    async def must_not_submit(*args):
        raise AssertionError("existing output should be rejected before HTTP")

    monkeypatch.setattr(cli, "run", must_not_submit)
    with pytest.raises(SystemExit) as error:
        cli.main(["submit", "task", "--output", str(path)])
    assert error.value.code == 2


def test_cli_reports_uncertainty_and_never_writes_a_success_handle(cli, tmp_path, monkeypatch, capsys):
    output = tmp_path / "job.json"
    monkeypatch.setenv("MINILOOP_DEEPAGENTS_CLIENT_TOKEN", TOKEN)

    async def uncertain(*args):
        raise SubmissionUncertain(
            server_url=URL, thread_id=THREAD, request_id=REQUEST, operation="submit",
        )

    monkeypatch.setattr(cli, "run", uncertain)
    assert cli.main(["submit", "task", "--output", str(output)]) == 3
    captured = capsys.readouterr()
    assert json.loads(captured.err)["request_id"] == REQUEST
    assert TOKEN not in captured.err and captured.out == "" and not output.exists()


def test_cli_retains_accepted_handle_if_atomic_save_fails(cli, tmp_path, monkeypatch, capsys):
    monkeypatch.setenv("MINILOOP_DEEPAGENTS_CLIENT_TOKEN", TOKEN)

    async def accepted(*args):
        return job()

    def cannot_save(*args, **kwargs):
        raise OSError(TOKEN)

    monkeypatch.setattr(cli, "run", accepted)
    monkeypatch.setattr(cli, "write_handle", cannot_save)
    assert cli.main(["submit", "task", "--output", str(tmp_path / "job.json")]) == 1
    captured = capsys.readouterr()
    assert JobHandle.from_json(captured.out) == job()
    assert "do not resubmit" in captured.err and TOKEN not in captured.err
