"""Detached long-job clients over the standard Agent Server API.

The server owns scheduling and checkpoints. Handles contain identifiers, never
credentials. Writes are sent once: a lost response is an uncertain submission,
not permission to retry. A request ID in metadata assists reconciliation but
does not provide atomic deduplication.
"""

from __future__ import annotations

import asyncio
import json
import math
from dataclasses import asdict, dataclass
from typing import Any, Awaitable, TypeVar
from urllib.parse import urlsplit, urlunsplit
from uuid import UUID, uuid4

import httpx
from langgraph_sdk.client import LangGraphClient
from langgraph_sdk.schema import MultitaskStrategy, Run, ThreadState


TERMINAL_STATUSES = frozenset({"success", "error", "timeout", "interrupted"})
RESUMABLE_STATUSES = frozenset({"error", "timeout", "interrupted"})
_T = TypeVar("_T")


async def _reject_redirects(response: httpx.Response) -> None:
    # The pinned SDK's status mapper handles 4xx/5xx, but silently accepts 3xx.
    # Do not turn deployment redirects into follow-up credentialed requests.
    if response.is_redirect:
        response.raise_for_status()


def normalize_url(value: str) -> str:
    """Normalize the trusted deployment URL without accepting embedded secrets."""
    if not isinstance(value, str) or not value or any(c.isspace() for c in value):
        raise ValueError("A valid http(s) Agent Server URL is required")
    try:
        parsed = urlsplit(value)
        port = parsed.port
    except ValueError:
        raise ValueError("A valid http(s) Agent Server URL is required") from None
    if (
        parsed.scheme.lower() not in {"http", "https"}
        or not parsed.hostname
        or parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
        or "\\" in value
    ):
        raise ValueError("URL must use http(s), without credentials, query or fragment")
    host = parsed.hostname.lower()
    if ":" in host:
        host = "[" + host + "]"
    authority = host + (f":{port}" if port is not None else "")
    return urlunsplit((parsed.scheme.lower(), authority, parsed.path.rstrip("/"), "", ""))


def _uuid(value: Any, field: str) -> None:
    try:
        if not isinstance(value, str) or str(UUID(value)) != value:
            raise ValueError
    except (ValueError, AttributeError):
        raise ValueError(f"{field} must be a canonical UUID") from None


@dataclass(frozen=True)
class JobHandle:
    """Portable identifiers for one Run, bound to a configured trusted server."""

    server_url: str
    thread_id: str
    run_id: str
    request_id: str
    assistant_id: str = "agent"
    version: int = 1

    def __post_init__(self) -> None:
        if type(self.version) is not int or self.version != 1:
            raise ValueError("Unsupported job handle version")
        if normalize_url(self.server_url) != self.server_url:
            raise ValueError("Job handle server_url must be normalized")
        if self.assistant_id != "agent":
            raise ValueError("Job handle assistant_id must be agent")
        for field in ("thread_id", "run_id", "request_id"):
            _uuid(getattr(self, field), field)

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)

    def to_json(self) -> str:
        return json.dumps(self.to_dict(), ensure_ascii=False, indent=2) + "\n"

    @classmethod
    def from_json(cls, value: str) -> JobHandle:
        try:
            data = json.loads(value)
        except (ValueError, TypeError):
            raise ValueError("Invalid job handle JSON") from None
        fields = {"version", "server_url", "thread_id", "run_id", "request_id", "assistant_id"}
        if not isinstance(data, dict) or set(data) != fields:
            raise ValueError("Job handle must contain exactly the version 1 fields")
        return cls(**data)


class JobRequestError(RuntimeError):
    """A redacted API failure; response bodies and credentials are not displayed."""

    def __init__(self, status_code: int | None = None):
        self.status_code = status_code
        super().__init__(
            f"Agent Server rejected the request (HTTP {status_code})"
            if status_code is not None else "Could not read Agent Server state"
        )


class JobResumeError(ValueError):
    """The handle cannot safely be used for checkpoint continuation."""


class JobWaitTimeout(TimeoutError):
    """Only the local wait ended; the background Run was not cancelled."""

    def __init__(self, job: JobHandle):
        self.job = job
        super().__init__("Local wait timed out; the background Run was not cancelled")


class SubmissionUncertain(RuntimeError):
    """The write may have reached the server; reconcile before submitting again."""

    def __init__(
        self, *, server_url: str, thread_id: str, request_id: str,
        operation: str, run_id: str | None = None,
    ):
        self.server_url = server_url
        self.thread_id = thread_id
        self.request_id = request_id
        self.operation = operation
        self.run_id = run_id
        super().__init__(
            "Write outcome is uncertain; reconcile this operation before retrying"
        )

    def to_dict(self) -> dict[str, Any]:
        guidance = {
            "create_thread": "Get the known Thread by thread_id; no Run was submitted by this call after the uncertain Thread creation",
            "cancel": "Get the known Run by run_id and inspect its status; submission metadata does not prove cancellation",
        }.get(self.operation, "List this Thread's runs and match metadata.request_id; do not automatically resubmit")
        return {
            "error": "submission_uncertain", "operation": self.operation,
            "server_url": self.server_url, "thread_id": self.thread_id,
            "request_id": self.request_id, "run_id": self.run_id,
            "reconcile": guidance,
        }


class AsyncJobsClient:
    """Own a no-retry SDK connection; use as an async context manager.

    Each submit creates a separate Thread unless a Thread is explicitly supplied.
    That allows independent jobs to execute concurrently while a shared Thread
    uses native enqueue serialization. Resume performs conservative prechecks;
    those checks and creation are not an atomic application-level dedupe lock.
    """

    def __init__(
        self, url: str, token: str, *, poll_interval: float = 1.0,
        request_timeout: float = 30.0, transport: httpx.AsyncBaseTransport | None = None,
    ):
        self.url = normalize_url(url)
        if (
            not isinstance(token, str) or not token or not token.isascii()
            or any(c.isspace() or ord(c) < 32 or ord(c) == 127 for c in token)
        ):
            raise ValueError("A nonempty bearer token is required")
        if not math.isfinite(poll_interval) or poll_interval <= 0:
            raise ValueError("poll_interval must be positive and finite")
        if not math.isfinite(request_timeout) or request_timeout <= 0:
            raise ValueError("request_timeout must be positive and finite")
        self.poll_interval = poll_interval
        # get_client() in SDK 0.4.7 enables transport retries=5. Construct its
        # public client directly so lost write responses never trigger retries.
        self._sdk = LangGraphClient(httpx.AsyncClient(
            base_url=self.url, headers={"Authorization": "Bearer " + token},
            timeout=request_timeout, follow_redirects=False,
            event_hooks={"response": [_reject_redirects]},
            transport=transport if transport is not None else httpx.AsyncHTTPTransport(retries=0),
        ))

    async def __aenter__(self) -> AsyncJobsClient:
        return self

    async def __aexit__(self, *_: Any) -> None:
        await self.aclose()

    async def aclose(self) -> None:
        await self._sdk.aclose()

    def _check(self, job: JobHandle) -> None:
        if not isinstance(job, JobHandle) or job.server_url != self.url:
            raise ValueError("Job handle does not match the configured trusted server URL")

    async def _read(self, call: Awaitable[_T]) -> _T:
        try:
            return await call
        except httpx.HTTPStatusError as exc:
            raise JobRequestError(exc.response.status_code) from None
        except (httpx.TransportError, ValueError):
            raise JobRequestError() from None

    async def _write(
        self, call: Awaitable[_T], *, thread_id: str, request_id: str,
        operation: str, run_id: str | None = None,
    ) -> _T:
        try:
            return await call
        except httpx.HTTPStatusError as exc:
            if exc.response.status_code < 500 and exc.response.status_code != 408:
                raise JobRequestError(exc.response.status_code) from None
        except (httpx.TransportError, ValueError, asyncio.CancelledError):
            pass
        raise SubmissionUncertain(
            server_url=self.url, thread_id=thread_id, request_id=request_id,
            operation=operation, run_id=run_id,
        ) from None

    async def _create_run(
        self, thread_id: str, request_id: str, *, input: dict[str, Any] | None,
        operation: str, strategy: MultitaskStrategy, previous_run_id: str | None = None,
    ) -> JobHandle:
        run = await self._write(
            self._sdk.runs.create(
                thread_id, "agent", input=input, durability="sync",
                multitask_strategy=strategy, metadata={"request_id": request_id},
            ),
            thread_id=thread_id, request_id=request_id, operation=operation,
            run_id=previous_run_id,
        )
        try:
            if run["thread_id"] != thread_id:
                raise ValueError
            return JobHandle(self.url, thread_id, run["run_id"], request_id)
        except (KeyError, TypeError, ValueError):
            raise SubmissionUncertain(
                server_url=self.url, thread_id=thread_id, request_id=request_id,
                operation=operation, run_id=previous_run_id,
            ) from None

    async def submit(self, message: str, *, thread_id: str | None = None) -> JobHandle:
        """Persist input through a background Run and return without joining it."""
        if not isinstance(message, str) or not message.strip():
            raise ValueError("A nonempty job message is required")
        request_id = str(uuid4())
        if thread_id is None:
            thread_id = str(uuid4())
            thread = await self._write(
                self._sdk.threads.create(thread_id=thread_id),
                thread_id=thread_id, request_id=request_id, operation="create_thread",
            )
            if not isinstance(thread, dict) or thread.get("thread_id") != thread_id:
                raise SubmissionUncertain(
                    server_url=self.url, thread_id=thread_id,
                    request_id=request_id, operation="create_thread",
                )
        else:
            _uuid(thread_id, "thread_id")
        return await self._create_run(
            thread_id, request_id, input={"messages": [{"role": "user", "content": message}]},
            operation="submit", strategy="enqueue",
        )

    async def status(self, job: JobHandle) -> Run:
        """Read this Run's status, rather than inferring it from Thread status."""
        self._check(job)
        return await self._read(self._sdk.runs.get(job.thread_id, job.run_id))

    async def state(self, job: JobHandle) -> ThreadState:
        """Read current Thread state; it may include later Runs on that Thread."""
        self._check(job)
        return await self._read(self._sdk.threads.get_state(job.thread_id))

    async def wait(self, job: JobHandle, *, timeout: float | None = None) -> Run:
        """Poll an existing Run; timeout/disconnection never cancels that Run."""
        self._check(job)
        if timeout is not None and (not math.isfinite(timeout) or timeout < 0):
            raise ValueError("timeout must be nonnegative and finite")
        loop = asyncio.get_running_loop()
        deadline = loop.time() + timeout if timeout is not None else None
        try:
            async with asyncio.timeout_at(deadline):
                while True:
                    try:
                        run = await self.status(job)
                        if run["status"] in TERMINAL_STATUSES:
                            return run
                    except JobRequestError as exc:
                        if exc.status_code is not None and exc.status_code < 500:
                            raise
                        # Only read requests are safe to retry after a disconnect.
                    await asyncio.sleep(self.poll_interval)
        except TimeoutError:
            raise JobWaitTimeout(job) from None

    async def cancel(self, job: JobHandle) -> None:
        """Request checkpoint-preserving interruption without blocking HTTP join."""
        self._check(job)
        await self._write(
            self._sdk.runs.cancel(job.thread_id, job.run_id, wait=False, action="interrupt"),
            thread_id=job.thread_id, request_id=job.request_id, operation="cancel", run_id=job.run_id,
        )

    async def resume(self, job: JobHandle) -> JobHandle:
        """Continue unfinished checkpoint tasks as a new Run, without new input.

        Dynamic graph interrupts need explicit human answers and are rejected.
        Native reject guards against inflight work; the prechecks are not an
        atomic lock against another continuation which already completed.
        """
        self._check(job)
        run = await self.status(job)
        if run["status"] not in RESUMABLE_STATUSES:
            raise JobResumeError("Resume requires an error, timeout or interrupted Run")
        latest = await self._read(self._sdk.runs.list(job.thread_id, limit=1))
        if not latest or latest[0]["run_id"] != job.run_id:
            raise JobResumeError("A newer Run exists; inspect its handle before resuming")
        state = await self.state(job)
        if state.get("interrupts") or any(t.get("interrupts") for t in state.get("tasks", [])):
            raise JobResumeError("Graph interrupt requires an explicit resume answer")
        if not state.get("next"):
            raise JobResumeError("Thread has no unfinished checkpoint tasks to resume")
        return await self._create_run(
            job.thread_id, str(uuid4()), input=None,
            operation="resume", strategy="reject", previous_run_id=job.run_id,
        )
