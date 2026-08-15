"""FastAPI server exposing many concurrent agents.

Concurrency model:
  * one shared event loop, one shared LLM client, one shared LLM semaphore;
  * each session is an independent Agent with its own workspace + history;
  * a session's runs are serialized by its own lock, but *different* sessions
    run truly concurrently -- while agent A awaits the model, agent B's loop
    keeps going. Blocking tool calls (bash, file I/O) are offloaded to threads
    inside the agent, so they never freeze the loop the others share.

Extensibility:
  Handlers read the manager from `request.app.state.manager`, and the app is
  built by `create_app(...)`. To serve a *customized* fleet (your tools, hooks,
  prompt, workspace factory) build a SessionManager and pass it in:

      from mini_loop.server import create_app
      app = create_app(manager=my_manager)

  The module-level `app = create_app()` is the default fleet, used by
  `python -m mini_loop` and `uvicorn mini_loop.server:app`.

Endpoints
  GET    /                          embedded console + endpoint list
  GET    /healthz                   liveness + config
  POST   /sessions                  {system?, model?} -> session info
  GET    /sessions                  list sessions
  GET    /sessions/{id}             session info (status, todos, msg count)
  DELETE /sessions/{id}             drop session + workspace
  POST   /sessions/{id}/messages    {message} -> run to completion, return final text
  POST   /sessions/{id}/messages/stream   {message} -> SSE of live events
  GET    /sessions/{id}/events      SSE: observe a session's event stream
  GET    /sessions/{id}/trajectories      list durable runs for one session
  GET    /trajectories/{id}         inspect one recorded trajectory
  GET    /trajectories/{id}/view    dsh-style HTML ledger of one trajectory
  GET    /trajectories/{id}/export  download JSON or JSONL
"""

from __future__ import annotations

import asyncio
import contextlib
import json
from contextlib import asynccontextmanager
from collections.abc import Callable
from typing import Literal

from fastapi.responses import JSONResponse
from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import HTMLResponse, Response, StreamingResponse
from pydantic import BaseModel
from sse_starlette.sse import EventSourceResponse

from .config import Settings, build_client, load_settings
from .manager import SessionManager
from .auth import ANONYMOUS, NullAuth, Principal, load_auth
from .identity import runtime_identity
from .session import AgentSession


class CreateSessionReq(BaseModel):
    system: str | None = None
    model: str | None = None
    mode: Literal["readonly", "interactive", "auto"] | None = None


class ModeReq(BaseModel):
    mode: Literal["readonly", "interactive", "auto"]


class MessageReq(BaseModel):
    message: str


class ApprovalReq(BaseModel):
    decision: Literal["allow", "deny"]
    #: For kind="question" pendings: the reply text. Ignored for approvals.
    answer: str | None = None


def _manager(request: Request) -> SessionManager:
    return request.app.state.manager


#: Routes that must stay reachable without a credential. Everything else is
#: authenticated by middleware, so a handler added later inherits the check
#: instead of having to remember it -- which is how `/trajectories/{id}`, an
#: endpoint that returns a whole recorded conversation, ended up open.
PUBLIC_PATHS = frozenset({"/healthz", "/", "/favicon.ico"})


def _auth(request: Request):
    return getattr(request.app.state, "auth", None) or NullAuth()


def _credential(request: Request) -> str | None:
    """The presented credential, from the header or the SSE query fallback.

    `EventSource` cannot set request headers -- the same browser constraint that
    makes upstream accept a cookie on its static-file router. The query
    parameter is honoured *only* on the streaming routes for that reason; every
    other path ignores it, so it cannot become a general-purpose bypass.
    """

    header = request.headers.get("authorization")
    if header:
        return header
    if request.url.path.endswith("/events"):
        token = request.query_params.get("access_token")
        if token:
            return f"Bearer {token}"
    return None


def _principal(request: Request) -> Principal:
    """Authenticate at request time so a rotated token takes effect at once."""

    caller = _auth(request).authenticate(_credential(request))
    if caller is None:
        raise HTTPException(
            status_code=401,
            detail="a bearer token is required",
            headers={"WWW-Authenticate": "Bearer"},
        )
    return caller


def _require(request: Request, session_id: str) -> AgentSession:
    """Fetch a session the caller owns.

    Someone else's session is reported as 404, not 403: 403 confirms the id
    exists, which is a disclosure to anyone enumerating.
    """

    caller = _principal(request)
    session = _manager(request).get(session_id)
    if session is None or getattr(session, "owner", ANONYMOUS.id) != caller.id:
        raise HTTPException(status_code=404, detail=f"No session '{session_id}'")
    return session


def _owned_session_ids(request: Request, caller) -> set[str]:
    """Sessions this caller owns, including ones already deleted.

    Trajectories outlive their session by design, so resolving ownership from
    *live* sessions alone would make deleting a session orphan its recordings --
    unreadable by the person who made them. The manager keeps the attribution
    after the session goes.
    """

    manager = _manager(request)
    owned = {
        s.id for s in manager.list()
        if getattr(s, "owner", ANONYMOUS.id) == caller.id
    }
    remembered = getattr(manager, "session_owners", None) or {}
    owned |= {sid for sid, owner in remembered.items() if owner == caller.id}
    return owned


def _owns_trajectory(request: Request, record, caller=None) -> bool:
    """Whether this caller may see this trajectory.

    The single rule, used by the fetch *and* the listing. They had different
    ones: the fetch checked ownership, and the listing asked only "do you own
    any session at all" before returning every trajectory on the box. A caller
    who owned nothing got `[]`, which reads exactly like scoping -- and is why a
    probe missed it twice.
    """

    caller = caller or _principal(request)
    record = record or {}
    recorded = record.get("owner")
    if recorded is not None:
        return recorded == caller.id
    session_id = record.get("session_id") or record.get("session")
    return session_id is not None and session_id in _owned_session_ids(request, caller)


def _require_owned_trajectory(request: Request, record) -> None:
    """A trajectory is readable only by the owner of its session."""

    if not _owns_trajectory(request, record):
        raise HTTPException(status_code=404, detail="No such trajectory")


#: The most events a reconnecting SSE client is caught up on from the durable
#: store. The in-memory backlog holds only the last 200, so a client that missed
#: more than that gapped -- the events are durable, but the resume never read
#: them. Read the newest window after the client's last-event-id instead, bounded
#: so a resume from far back cannot force an unbounded read (round 143's lesson,
#: on the event stream). A client further behind than this reconnects fresh.
MAX_EVENT_CATCHUP = 2000


#: The largest trajectory served as a single in-memory JSON document (the
#: structured `get()` view, and the JSON export). A long run records the full
#: model input at every model call, so a trajectory grows to tens of MB;
#: building the whole thing in memory to serialize it is an OOM the same shape
#: as `edit_file`'s (round 141). Past this, the JSON views refuse and point to
#: the JSONL export, which *streams* and has no such ceiling.
MAX_TRAJECTORY_JSON_BYTES = 8 * 1024 * 1024


async def _owned_trajectory_summary(request: Request, store, trajectory_id: str) -> dict:
    """The trajectory's summary, or 404 -- ownership checked *before* any bulk read.

    Both trajectory routes used to `get()` the whole file and *then* check the
    caller. So a stranger requesting another tenant's id forced the entire
    trajectory into memory before the 404 -- a cross-tenant load, and an OOM
    lever for anyone who knows an id. `summary()` streams only the header (round
    143), which carries the owner, so ownership is decided cheaply and first.
    """

    summary = await asyncio.to_thread(store.summary, trajectory_id)
    _require_owned_trajectory(request, summary)
    return summary


async def _stream_in_thread(sync_iterable):
    """Drive a blocking generator from the event loop without stalling it."""

    iterator = iter(sync_iterable)
    sentinel = object()
    while True:
        chunk = await asyncio.to_thread(next, iterator, sentinel)
        if chunk is sentinel:
            break
        yield chunk


def _trajectory_store(request: Request):
    store = _manager(request).trajectories
    if store is None:
        raise HTTPException(status_code=503, detail="Trajectory recording is disabled")
    return store


#: The largest request body the server reads. A message and a system prompt are
#: bounded downstream (`steer` truncates, the model rejects an over-long prompt),
#: but nothing bounded the *body* on the way in: Starlette reads the whole thing
#: to parse it, so an authenticated caller POSTing a multi-gigabyte body OOMs the
#: shared process before any handler runs -- every tenant on it with it. Generous
#: enough for a full-context prompt (~1M tokens is a few MB) and a large document;
#: bounded so the ingress cannot be turned into a memory bomb.
MAX_REQUEST_BYTES = 10 * 1024 * 1024


class RequestSizeLimit:
    """Reject a request body over `max_bytes` with 413, before it is buffered.

    A pure-ASGI middleware, not `@app.middleware("http")`: the size check has to
    sit in front of the body read, and `BaseHTTPMiddleware` cannot replace the
    receive channel a handler will read from. Checks `Content-Length` first (the
    cheap common case), then counts the streamed body too, so a chunked request
    or a lying `Content-Length` cannot slip a large body past the header. Peak
    memory is bounded to `max_bytes`: the body is buffered only up to the cap,
    and a request that exceeds it is refused without ever reaching the app.
    """

    def __init__(self, app, *, max_bytes: int) -> None:
        self.app = app
        self.max_bytes = max_bytes

    async def _too_large(self, send) -> None:
        body = json.dumps({"detail": "request body too large"}).encode()
        await send({
            "type": "http.response.start",
            "status": 413,
            "headers": [
                (b"content-type", b"application/json"),
                (b"content-length", str(len(body)).encode()),
            ],
        })
        await send({"type": "http.response.body", "body": body})

    async def __call__(self, scope, receive, send) -> None:
        if scope["type"] != "http":
            return await self.app(scope, receive, send)
        for name, value in scope.get("headers", ()):
            if name == b"content-length":
                try:
                    if int(value) > self.max_bytes:
                        return await self._too_large(send)
                except ValueError:
                    pass
                break

        buffered: list[dict] = []
        total = 0
        while True:
            message = await receive()
            if message["type"] != "http.request":
                buffered.append(message)
                break
            total += len(message.get("body", b""))
            if total > self.max_bytes:
                return await self._too_large(send)
            buffered.append(message)
            if not message.get("more_body", False):
                break

        drained = False

        async def replay():
            nonlocal drained
            if buffered:
                return buffered.pop(0)
            drained = True
            return await receive()

        await self.app(scope, replay, send)


def create_app(
    *,
    settings: Settings | None = None,
    manager: SessionManager | None = None,
    manager_factory: Callable[[Settings], SessionManager] | None = None,
) -> FastAPI:
    """Build the FastAPI app. Pass `manager` (or `manager_factory`) to serve a
    customized fleet; omit both for the default."""

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        cfg = settings or load_settings()
        app.state.settings = cfg
        # Every construction path gets the same authenticator; setting this
        # inside one branch is how an injected manager ended up anonymous.
        app.state.auth = load_auth()
        if manager is not None:
            app.state.manager = manager
            owns_client = False
        elif manager_factory is not None:
            app.state.manager = manager_factory(cfg)
            owns_client = False
        else:
            app.state.manager = SessionManager(cfg, build_client(cfg), enable_features=cfg.enable_features)
            owns_client = True
        mgr = app.state.manager
        with contextlib.suppress(Exception):
            await mgr.start()   # starts the cron ticker when features are on
        yield
        with contextlib.suppress(Exception):
            await mgr.stop()
        if owns_client:
            with contextlib.suppress(Exception):
                await mgr.client.close()

    app = FastAPI(title="mini-loop", version="0.1.0", lifespan=lifespan)

    @app.middleware("http")
    async def authenticate(request: Request, call_next):
        """Authenticate every route except the explicit public list.

        Deliberately middleware rather than a per-handler call: the two
        endpoints that returned whole recorded conversations were open because
        each handler had to remember, and two of them did not.
        """

        if request.url.path not in PUBLIC_PATHS:
            try:
                _principal(request)
            except HTTPException as denial:
                return JSONResponse(
                    {"detail": denial.detail},
                    status_code=denial.status_code,
                    headers=denial.headers or {},
                )
        return await call_next(request)

    @app.middleware("http")
    async def security_headers(request: Request, call_next):
        """Bound the blast radius of a content injection, per OpenWorker 9.2.4.

        The console renders every field through `textContent`, so it has no
        XSS sink today (the guard in test_console_safety.py pins that). These
        headers are the second line: if a sink ever slips in, `connect-src
        'self'` and `default-src 'none'` block the one move that matters --
        exfiltrating the localStorage API token to another origin (via fetch,
        image, form, or script src). The console is fully self-contained
        (inline style + script, same-origin fetch/SSE, system fonts), so the
        policy costs it nothing. Added after `authenticate` so it wraps that
        middleware's early denials too.
        """

        response = await call_next(request)
        for header, value in SECURITY_HEADERS.items():
            response.headers.setdefault(header, value)
        return response

    # Outermost, so nothing -- not auth, not a handler -- ever reads an oversized
    # body: the cap sits in front of every route by construction, the same reason
    # authentication is middleware rather than a per-handler call.
    app.add_middleware(RequestSizeLimit, max_bytes=MAX_REQUEST_BYTES)

    _register_routes(app)
    return app


#: Sent on every response. The CSP allows the console's own inline script and
#: style and same-origin fetch/SSE, and nothing else -- no external script,
#: no cross-origin connection, no framing.
CONSOLE_CSP = (
    "default-src 'none'; "
    "script-src 'unsafe-inline'; "
    "style-src 'unsafe-inline'; "
    "connect-src 'self'; "
    "img-src 'self' data:; "
    "base-uri 'none'; "
    "form-action 'none'; "
    "frame-ancestors 'none'"
)
SECURITY_HEADERS = {
    "Content-Security-Policy": CONSOLE_CSP,
    "X-Content-Type-Options": "nosniff",
    "X-Frame-Options": "DENY",
    "Referrer-Policy": "no-referrer",
}


def _register_routes(app: FastAPI) -> None:
    @app.get("/healthz")
    async def healthz(request: Request):
        s = request.app.state.settings
        return {"status": "ok", "model": s.model, "fake_llm": s.fake_llm,
                "features": s.enable_features, "max_concurrent_llm": s.max_concurrent_llm,
                "max_concurrent_tools": s.max_concurrent_tools,
                "experimental_workflows": _manager(request).enable_workflows,
                "authenticated": _auth(request).configured,
                # Identity so a client can assert it is talking to the build it
                # just started, and posture so a deployment can be audited
                # remotely instead of inferred from local configuration.
                **runtime_identity(_manager(request), _auth(request)),
                "trajectories": _manager(request).trajectories is not None,
                "sessions": len(_manager(request).list())}

    @app.post("/sessions")
    async def create_session(request: Request, req: CreateSessionReq):
        caller = _principal(request)
        session = _manager(request).create(
            system=req.system, model=req.model,
            permission_mode=req.mode or "interactive",
        )
        session.owner = caller.id
        return session.info()

    @app.post("/sessions/{session_id}/mode")
    async def set_mode(request: Request, session_id: str, req: ModeReq):
        """Change the session's risk->decision posture (see permissions.py)."""
        session = _require(request, session_id)
        session.permission_mode = req.mode
        return {"session": session_id, "permission_mode": req.mode}

    @app.get("/sessions")
    async def list_sessions(request: Request):
        caller = _principal(request)
        return [
            s.info()
            for s in _manager(request).list()
            if getattr(s, "owner", ANONYMOUS.id) == caller.id
        ]

    @app.get("/sessions/{session_id}")
    async def get_session(request: Request, session_id: str):
        return _require(request, session_id).info()

    @app.delete("/sessions/{session_id}")
    async def delete_session(request: Request, session_id: str):
        # Any authenticated caller could delete anyone's session. A live probe
        # missed it by deleting as the owner first, so the stranger's 404 meant
        # "already gone" rather than "not yours"; the AST check over routes
        # taking a caller-supplied id is what found it.
        _require(request, session_id)
        if not _manager(request).delete(session_id):
            raise HTTPException(status_code=404, detail=f"No session '{session_id}'")
        return {"deleted": session_id}

    @app.post("/sessions/{session_id}/steer")
    async def steer_session(request: Request, session_id: str, req: MessageReq):
        """Queue input for a running turn (or the next one). Never 409s.

        The text reaches the model at its next loop round as a
        `<user_interjection>` -- OpenWorker's busy-session steering, on our
        injector seam.
        """
        session = _require(request, session_id)
        queued = session.steer(req.message)
        return {"session": session_id, "queued": queued, "busy": session.busy}

    @app.post("/sessions/{session_id}/cancel")
    async def cancel_run(request: Request, session_id: str):
        """Stop the turn in flight, if there is one."""
        session = _require(request, session_id)
        stopped = await session.cancel("cancelled over HTTP")
        return {"session": session_id, "cancelled": stopped, "info": session.info()}

    @app.post("/sessions/{session_id}/cron/{job_id}/arm")
    async def arm_cron(request: Request, session_id: str, job_id: str):
        """Re-authorize a cron job restored from disk.

        Activation is process-local and never persisted: a durable job
        survives a restart as a *fact*, but firing unattended again needs a
        new authorization edge, which this operator call records. Scoped to
        the caller's own session, like cancel. Deliberately not a model tool.
        """
        _require(request, session_id)
        scheduler = getattr(_manager(request), "cron", None)
        if scheduler is None:
            raise HTTPException(status_code=404, detail="cron is not enabled")
        outcome = scheduler.arm(job_id, session_id)
        if outcome.startswith("Error"):
            raise HTTPException(status_code=404, detail=outcome)
        return {"session": session_id, "job": job_id, "armed": True}

    @app.get("/sessions/{session_id}/transcript")
    async def read_transcript(request: Request, session_id: str,
                              epoch: int | None = None):
        """One epoch of the durable transcript -- the current one by default.

        Superseded epochs are the canonical record of what the agent actually
        saw before a compaction rewrote its history (storage.py); this is the
        operator's way to read them. Persisted rows are already masked.
        """
        session = _require(request, session_id)
        store = _manager(request).state_store
        if not hasattr(store, "transcript_epoch"):
            raise HTTPException(status_code=404,
                                detail="no durable transcript store")
        current = store.transcript_epoch(session.id)
        target = current if epoch is None else epoch
        if target < 1 or target > current:
            raise HTTPException(status_code=404,
                                detail=f"no epoch {target} (current: {current})")
        return {"session": session_id, "epoch": target, "epochs": current,
                "messages": store.load_messages(session.id, epoch=target)}

    @app.get("/sessions/{session_id}/approvals")
    async def list_approvals(request: Request, session_id: str):
        """Tool calls parked waiting for a human answer (see approvals.py)."""
        session = _require(request, session_id)
        return {"session": session_id,
                "approvals": _manager(request).approvals.list(session.id)}

    @app.post("/sessions/{session_id}/approvals/{approval_id}")
    async def resolve_approval(request: Request, session_id: str,
                               approval_id: str, req: ApprovalReq):
        session = _require(request, session_id)
        # Scoped resolve: an approval from another session is reported as
        # missing, the same rule _require applies to the session itself.
        resolved = _manager(request).approvals.resolve(
            approval_id, session_id=session.id, allowed=req.decision == "allow",
            answer=req.answer,
        )
        if not resolved:
            raise HTTPException(status_code=404,
                                detail=f"No pending approval '{approval_id}'")
        return {"approval_id": approval_id, "decision": req.decision}

    @app.post("/sessions/{session_id}/messages")
    async def post_message(request: Request, session_id: str, req: MessageReq):
        session = _require(request, session_id)
        if session.busy:
            # Queueing on the session lock would hold the connection open with
            # no timeout and no way to see the queue. Saying so is better than
            # hanging -- and steering is better than either when the caller
            # wants to redirect the running turn rather than start a new one.
            raise HTTPException(
                status_code=409,
                detail=(
                    f"session {session_id} is running a turn; POST "
                    f"/sessions/{session_id}/steer to redirect it, "
                    f"/sessions/{session_id}/cancel to stop it, or retry"
                ),
            )
        final = await session.run(req.message)
        return {"session": session_id, "final": final, "info": session.info()}

    @app.post("/sessions/{session_id}/messages/stream")
    async def post_message_stream(request: Request, session_id: str, req: MessageReq):
        session = _require(request, session_id)

        async def gen():
            q = session.subscribe(replay=False)
            run_task = asyncio.create_task(session.run(req.message))
            try:
                while True:
                    getter = asyncio.ensure_future(q.get())
                    done, _ = await asyncio.wait({getter, run_task}, return_when=asyncio.FIRST_COMPLETED)
                    if getter in done:
                        event = getter.result()
                        yield {"id": str(event["seq"]), "event": event["type"],
                               "data": json.dumps(event)}
                    else:
                        getter.cancel()
                        while not q.empty():
                            event = q.get_nowait()
                            yield {"id": str(event["seq"]), "event": event["type"],
                                   "data": json.dumps(event)}
                        break
            finally:
                session.unsubscribe(q)
                with contextlib.suppress(Exception):
                    await run_task

        return EventSourceResponse(gen())

    @app.get("/sessions/{session_id}/events")
    async def observe(request: Request, session_id: str, envelope: bool = False):
        session = _require(request, session_id)
        store = _manager(request).state_store

        def _sse(event: dict) -> dict:
            return {
                "id": str(event["seq"]),
                "event": "agent_event" if envelope else event["type"],
                "data": json.dumps(event),
            }

        async def gen():
            try:
                last_seen = int(request.headers.get("last-event-id", "0"))
            except ValueError:
                last_seen = 0
            # Subscribe before the durable catch-up, so an event emitted between
            # the two lands in the queue and is de-duplicated below rather than
            # slipping through the gap. Persistence happens before the queue put,
            # so anything the queue holds the store already has.
            q = session.subscribe(replay=True)
            delivered = last_seen
            try:
                loader = getattr(store, "load_events", None)
                if last_seen > 0 and loader is not None:
                    # The 200-event backlog is not enough for a client that
                    # missed more than that. Deliver the newest durable window
                    # after last-event-id, bounded; the backlog already in the
                    # queue is de-duplicated against it.
                    start = max(last_seen, session._seq - MAX_EVENT_CATCHUP)
                    caught_up = await asyncio.to_thread(
                        loader, session_id, after=start, limit=MAX_EVENT_CATCHUP
                    )
                    for event in caught_up:
                        if event["seq"] <= delivered:
                            continue
                        yield _sse(event)
                        delivered = event["seq"]
                while True:
                    event = await q.get()
                    if event["seq"] <= delivered:
                        continue
                    yield _sse(event)
                    delivered = event["seq"]
            finally:
                session.unsubscribe(q)

        return EventSourceResponse(gen())

    @app.get("/sessions/{session_id}/trajectories")
    async def list_session_trajectories(
        request: Request, session_id: str, limit: int = 100
    ):
        # Scoped like every other `/sessions/{id}` route. Without this an
        # authenticated stranger got 404 from `GET /sessions/{id}` and 200 from
        # `GET /sessions/{id}/trajectories` on the same session.
        _require(request, session_id)
        return await asyncio.to_thread(
            _trajectory_store(request).list,
            session_id=session_id,
            limit=min(max(limit, 1), 500),
        )

    @app.get("/trajectories")
    async def list_trajectories(
        request: Request, session_id: str | None = None, limit: int = 100
    ):
        caller = _principal(request)
        if session_id is not None:
            _require(request, session_id)  # 404s unless the caller owns it
        records = await asyncio.to_thread(
            _trajectory_store(request).list,
            session_id=session_id,
            limit=min(max(limit, 1), 500),
        )
        # Filtered, not gated. The previous version returned early when the
        # caller owned no session and otherwise handed back every trajectory on
        # the box -- so creating one session of your own was enough to read
        # everyone's.
        return [r for r in records if _owns_trajectory(request, r, caller)]

    @app.get("/trajectories/{trajectory_id}/export")
    async def export_trajectory(
        request: Request, trajectory_id: str, format: str = "json"
    ):
        store = _trajectory_store(request)
        if format not in ("json", "jsonl"):
            raise HTTPException(
                status_code=400, detail="format must be 'json' or 'jsonl'"
            )
        try:
            # Ownership from the header, before any bulk read: a stranger's
            # request for another tenant's id is refused without loading the
            # trajectory, and the owner's own export is bounded below.
            await _owned_trajectory_summary(request, store, trajectory_id)
            disposition = (
                f'attachment; filename="{trajectory_id}.{format}"'
            )
            if format == "jsonl":
                # Streamed, not read whole: an export must work for a trajectory
                # far larger than memory. The lock is not held across the stream.
                return StreamingResponse(
                    _stream_in_thread(store.stream_raw(trajectory_id)),
                    media_type="application/x-ndjson",
                    headers={"Content-Disposition": disposition},
                )
            size = await asyncio.to_thread(store.byte_size, trajectory_id)
            if size > MAX_TRAJECTORY_JSON_BYTES:
                raise HTTPException(
                    status_code=413,
                    detail=(
                        f"trajectory is {size:,} bytes; too large to export as one "
                        f"JSON document (limit {MAX_TRAJECTORY_JSON_BYTES:,}). Use "
                        "?format=jsonl, which streams."
                    ),
                )
            content = json.dumps(
                await asyncio.to_thread(store.get, trajectory_id),
                ensure_ascii=False,
                indent=2,
            )
        except (KeyError, ValueError):
            raise HTTPException(
                status_code=404, detail=f"No trajectory '{trajectory_id}'"
            ) from None
        return Response(
            content=content,
            media_type="application/json",
            headers={"Content-Disposition": disposition},
        )

    @app.get("/trajectories/{trajectory_id}/view", response_class=HTMLResponse)
    async def view_trajectory(request: Request, trajectory_id: str):
        """The dsh-style ledger for one recorded run, as one HTML page.

        Same access order as the JSON route below: ownership from the header
        before any bulk read, then the size bound -- rendering is a whole-file
        materialisation, so it inherits the JSON route's ceiling rather than
        inventing a second one.
        """
        from .trace_view import build_ledger, render_html

        store = _trajectory_store(request)
        try:
            await _owned_trajectory_summary(request, store, trajectory_id)
            size = await asyncio.to_thread(store.byte_size, trajectory_id)
            if size > MAX_TRAJECTORY_JSON_BYTES:
                raise HTTPException(
                    status_code=413,
                    detail=(
                        f"trajectory is {size:,} bytes; too large to render as "
                        f"one page (limit {MAX_TRAJECTORY_JSON_BYTES:,}). "
                        "Download it with /export?format=jsonl, which streams."
                    ),
                )
            trajectory = await asyncio.to_thread(store.get, trajectory_id)
        except (KeyError, ValueError):
            raise HTTPException(
                status_code=404, detail=f"No trajectory '{trajectory_id}'"
            ) from None
        return HTMLResponse(await asyncio.to_thread(
            render_html,
            [build_ledger(trajectory)],
            title=f"mini-loop trace · {trajectory_id}",
        ))

    @app.get("/trajectories/{trajectory_id}")
    async def get_trajectory(request: Request, trajectory_id: str):
        store = _trajectory_store(request)
        try:
            # Ownership first, from the header -- so a stranger's id does not
            # load the file, and the owner's huge trajectory is not built whole.
            await _owned_trajectory_summary(request, store, trajectory_id)
            size = await asyncio.to_thread(store.byte_size, trajectory_id)
            if size > MAX_TRAJECTORY_JSON_BYTES:
                raise HTTPException(
                    status_code=413,
                    detail=(
                        f"trajectory is {size:,} bytes; too large to render as one "
                        f"JSON document (limit {MAX_TRAJECTORY_JSON_BYTES:,}). "
                        "Download it with /export?format=jsonl, which streams."
                    ),
                )
            return await asyncio.to_thread(store.get, trajectory_id)
        except (KeyError, ValueError):
            raise HTTPException(
                status_code=404, detail=f"No trajectory '{trajectory_id}'"
            ) from None

    @app.get("/", response_class=HTMLResponse)
    async def console():
        return CONSOLE_HTML


# Default fleet (used by `python -m mini_loop` and `uvicorn mini_loop.server:app`).
app = create_app()


CONSOLE_HTML = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>mini-loop console</title>
<style>
 :root{
  color-scheme:dark;
  --bg:#0d1117;--panel:#161b22;--panel-2:#0f141b;--border:#30363d;
  --text:#e6edf3;--muted:#9da7b3;--blue:#58a6ff;--green:#3fb950;
  --yellow:#d29922;--red:#f85149;--purple:#bc8cff;
 }
 *{box-sizing:border-box}
 body{margin:0;background:var(--bg);color:var(--text);font:15px/1.55 ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace}
 header{padding:14px 18px;background:var(--panel);border-bottom:1px solid var(--border)}
 header strong{color:#fff} header span{color:var(--muted)}
 main{display:grid;grid-template-columns:minmax(300px,.8fr) minmax(0,1.7fr);gap:14px;max-width:1440px;margin:0 auto;padding:14px}
 .col{min-width:0;background:var(--panel);border:1px solid var(--border);border-radius:10px;padding:14px}
 .controls{align-self:start}
 label{display:block;margin-bottom:6px;color:var(--muted);font-size:13px;font-weight:600}
 textarea,select,button{font:inherit;border:1px solid var(--border);border-radius:7px}
 textarea,select{display:block;width:100%;padding:10px;background:var(--bg);color:var(--text);line-height:1.5}
 textarea{resize:vertical}
 textarea::placeholder{color:#6e7681}
 textarea:focus-visible,select:focus-visible,button:focus-visible,a:focus-visible,summary:focus-visible{outline:2px solid var(--blue);outline-offset:2px}
 button{min-height:44px;padding:8px 13px;cursor:pointer;background:#238636;border-color:#2ea043;color:#fff;font-weight:650;transition:background-color 180ms,border-color 180ms}
 button:hover{background:#2ea043} button:disabled{cursor:not-allowed;opacity:.58}
 button.sec{background:#21262d;border-color:#484f58;color:var(--text)}
 button.sec:hover{background:#30363d}
 .field+.field{margin-top:14px}.actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-top:12px}
 .session{margin-top:12px;padding:10px 11px;background:var(--panel-2);border:1px solid var(--border);border-radius:7px;color:var(--muted);overflow-wrap:anywhere}
 .session strong{color:var(--blue)}
 .trajectory-panel{margin-top:16px;padding:12px;background:var(--panel-2);border:1px solid var(--border);border-radius:8px}
 .trajectory-panel h3{margin:0 0 3px;font-size:14px}.trajectory-panel>p{margin:0 0 10px;color:var(--muted);font-size:12px}
 .trajectory-meta{min-height:38px;margin:9px 0 0!important;color:var(--muted);overflow-wrap:anywhere}
 .download{display:inline-flex;align-items:center;justify-content:center;min-height:44px;padding:8px 12px;border:1px solid #484f58;border-radius:7px;background:#21262d;color:var(--text);font-weight:650;text-decoration:none;transition:background-color 180ms,border-color 180ms}
 .download:hover{background:#30363d}.download[aria-disabled="true"]{pointer-events:none;opacity:.5}
 .panel-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin-bottom:12px}
 h2{font-size:16px;line-height:1.3;margin:0 0 3px} .panel-head p{margin:0;color:var(--muted);font-size:12px}
 .stream-tools{display:flex;align-items:center;justify-content:flex-end;gap:8px;flex-wrap:wrap}
 .stream-state,.event-count{display:inline-flex;align-items:center;gap:6px;min-height:28px;padding:3px 8px;background:var(--panel-2);border:1px solid var(--border);border-radius:999px;color:var(--muted);font-size:12px;white-space:nowrap}
 .state-dot{width:8px;height:8px;border-radius:50%;background:#6e7681}
 .stream-state[data-state="live"] .state-dot{background:var(--green)}
 .stream-state[data-state="connecting"] .state-dot{background:var(--yellow)}
 .stream-state[data-state="reconnecting"] .state-dot{background:var(--red)}
 .clear-events{min-height:34px;padding:4px 10px;font-size:12px}
 .log{height:calc(100vh - 145px);min-height:430px;overflow:auto;padding-right:4px;scrollbar-gutter:stable}
 .empty{display:grid;place-items:center;min-height:180px;padding:24px;text-align:center;color:var(--muted);border:1px dashed var(--border);border-radius:8px}
 .event-card{margin:0 0 8px;padding:10px 11px;background:var(--panel-2);border:1px solid var(--border);border-left:3px solid #6e7681;border-radius:7px;transition:border-color 180ms,background-color 180ms}
 .event-card:hover{background:#131a23;border-color:#484f58}
 .event-card[data-tone="info"]{border-left-color:var(--blue)}
 .event-card[data-tone="success"]{border-left-color:var(--green)}
 .event-card[data-tone="warning"]{border-left-color:var(--yellow)}
 .event-card[data-tone="error"]{border-left-color:var(--red)}
 .event-card[data-tone="tool"]{border-left-color:var(--purple)}
 .event-meta{display:flex;align-items:center;gap:7px;flex-wrap:wrap;color:var(--muted);font-size:11px}
 .event-type{padding:2px 7px;border:1px solid #3d4b5d;border-radius:999px;color:#c9d1d9;font-weight:700;letter-spacing:.02em}
 .event-seq{color:var(--blue)} .event-source{margin-left:auto;text-transform:uppercase;letter-spacing:.08em}
 .event-summary{margin-top:7px;color:var(--text);white-space:pre-wrap;overflow-wrap:anywhere}
 details{margin-top:7px;border-top:1px solid #262d36}
 summary{width:max-content;min-height:36px;padding:8px 2px 4px;color:var(--muted);cursor:pointer;font-size:12px}
 pre{max-height:280px;margin:3px 0 0;padding:10px;overflow:auto;background:#090d12;border:1px solid #262d36;border-radius:6px;color:#c9d1d9;font:12px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre-wrap;overflow-wrap:anywhere}
 @media(max-width:820px){main{grid-template-columns:1fr}.log{height:58vh;min-height:360px}.panel-head{align-items:stretch;flex-direction:column}.stream-tools{justify-content:flex-start}textarea,select,button{font-size:16px}}
 @media(prefers-reduced-motion:reduce){button,.event-card{transition:none}}
</style>
</head>
<body>
<header><strong>mini-loop</strong> <span>&mdash; concurrent agent console with live event telemetry</span></header>
<main>
 <section class="col controls" aria-labelledby="controls-title">
  <h2 id="controls-title">Run an agent</h2>
  <div class="field">
   <label for="sys">System prompt (optional)</label>
   <textarea id="sys" rows="3" placeholder="Override the default system prompt"></textarea>
  </div>
  <div class="actions">
   <button id="create-btn" type="button" onclick="mk()">New session</button>
  </div>
  <div class="session" id="session-info">No active session</div>
  <div class="field">
   <label for="msg">Message</label>
   <textarea id="msg" rows="5" placeholder="Message to the agent">build a hello world script and run it</textarea>
  </div>
  <div class="actions">
   <button id="run-btn" type="button" onclick="send()">Run agent</button>
  </div>
  <section class="trajectory-panel" aria-labelledby="trajectory-title">
   <h3 id="trajectory-title">Recorded trajectories</h3>
   <p>Each run is saved locally and can be inspected or exported.</p>
   <label for="trajectory-select">Agent run</label>
   <select id="trajectory-select" onchange="selectTrajectory()" disabled>
    <option>No recordings yet</option>
   </select>
   <div class="actions">
    <button class="sec" id="load-trajectory-btn" type="button" onclick="loadTrajectory()" disabled>View recording</button>
    <a class="download" id="export-json" href="#" aria-disabled="true">Export JSON</a>
    <a class="download" id="export-jsonl" href="#" aria-disabled="true">Export JSONL</a>
   </div>
   <p class="trajectory-meta" id="trajectory-meta" aria-live="polite">Create and run a session to record its first trajectory.</p>
  </section>
 </section>
 <section class="col" aria-labelledby="events-title">
  <div class="panel-head">
   <div><h2 id="events-title">Pushed events</h2><p>Persistent SSE feed with complete event metadata and payloads.</p></div>
   <div class="stream-tools">
    <span class="stream-state" id="stream-state" data-state="idle"><span class="state-dot" aria-hidden="true"></span><span id="stream-label">idle</span></span>
    <span class="event-count" id="event-count">0 events</span>
    <button class="sec clear-events" type="button" onclick="clearEvents()">Clear</button>
   </div>
  </div>
  <div class="log" id="log" role="log" aria-live="polite" aria-relevant="additions" aria-label="Agent event stream">
   <div class="empty" id="empty-state">Create a session to start receiving pushed events.</div>
  </div>
 </section>
</main>
<script>
// The console is a browser client: `fetch` can carry a header, `EventSource`
// cannot. The token therefore rides a query parameter on the stream only --
// the server honours it on no other path.
function apiToken(){ return localStorage.getItem('miniloop_token')||''; }
function authHeaders(extra){ const o=extra?JSON.parse(JSON.stringify(extra)):{};
  const t=apiToken(); if(t){ o.headers=Object.assign({},o.headers,{Authorization:'Bearer '+t}); }
  return o; }
function tokenQuery(){ const t=apiToken(); return t?('&access_token='+encodeURIComponent(t)):''; }

let sid=null,eventSource=null,eventCount=0,lastSeq=0,trajectories=[];
const log=document.getElementById('log');
const eventCountLabel=document.getElementById('event-count');
const streamState=document.getElementById('stream-state');
const streamLabel=document.getElementById('stream-label');
const trajectorySelect=document.getElementById('trajectory-select');
const trajectoryMeta=document.getElementById('trajectory-meta');
const loadTrajectoryButton=document.getElementById('load-trajectory-btn');
const exportJson=document.getElementById('export-json');
const exportJsonl=document.getElementById('export-jsonl');

function short(value,limit=900){
 const rendered=typeof value==='string'?value:JSON.stringify(value);
 const text=rendered===undefined?String(value):rendered;
 return text.length>limit?text.slice(0,limit)+'…':text;
}
function eventTone(type,payload){
 if(type.endsWith('error')||payload.error===true||payload.decision==='deny'||payload.action==='failed'||payload.status==='error')return 'error';
 if(type==='done'||(type==='trajectory_end'&&payload.status==='completed'))return 'success';
 if(type==='tool_use'||type==='tool_result')return 'tool';
 if(['compact','recovery','permission','todo'].includes(type))return 'warning';
 if(['assistant_text','memory','background_result','team_inbox'].includes(type))return 'info';
 return 'neutral';
}
function eventSummary(type,o){
 if(type==='assistant_text'||type==='done')return o.text||'(empty text)';
 if(type==='tool_use')return (o.name||'unknown tool')+' '+short(o.input||{});
 if(type==='tool_result')return (o.name?o.name+': ':'')+short(o.output||'(empty result)');
 if(type==='trajectory_start')return 'Recording run #'+(o.run_index||'?')+' · '+(o.trajectory_id||'trajectory');
 if(type==='trajectory_end')return 'Trajectory '+(o.status||'finished')+' · '+formatDuration(o.duration_ms);
 if(type==='trajectory_recording')return 'Run #'+(o.run_index||'?')+' '+(o.status||'recorded')+' · '+formatDuration(o.duration_ms)+' · '+short(o.input||'(no input)',240);
 if(type==='model_start')return (o.purpose||'model call')+' · '+(o.model||'unknown model')+' · ~'+(o.input_tokens_estimate||0)+' input tokens';
 if(type==='model_end')return (o.purpose||'model call')+' '+(o.status||'finished')+' · '+formatDuration(o.duration_ms)+(o.stop_reason?' · '+o.stop_reason:'');
 if(type==='error'||type==='client_error')return o.error||'Unknown error';
 if(type==='status')return 'Session status: '+(o.status||'unknown')+(o.cancelled?' (cancelled)':'');
 if(type==='subagent_start')return 'Started '+(o.agent_type||'subagent')+': '+short(o.prompt||'');
 if(type==='subagent_end')return 'Subagent completed: '+short(o.summary||'');
 if(type==='todo')return (o.items||[]).map(item=>item.status+': '+item.content).join(' | ')||'Todo list updated';
 if(type==='compact')return 'Context compaction: '+(o.kind||'unknown');
 if(type==='permission')return (o.decision||'decision')+' via '+(o.rule||'rule')+(o.reason?': '+o.reason:'');
 if(type==='recovery')return 'Recovery action: '+(o.action||'unknown')+(o.error?' ('+o.error+')':'');
 if(type==='memory')return 'Memory '+(o.action||'event')+(o.count!==undefined?': '+o.count+' item(s)':'');
 if(type==='background_result')return (o.count||0)+' background result(s) ready';
 if(type==='team_inbox')return (o.count||0)+' team message(s) received';
 if(type==='user_prompt')return o.text||'(empty message)';
 const detail=Object.entries(o).filter(([key])=>!['seq','ts','session','type','agent','depth'].includes(key));
 return detail.length?short(Object.fromEntries(detail)):'Event received';
}
function formatTime(ts){
 if(!ts)return '--:--:--';
 return new Date(ts*1000).toLocaleTimeString([], {hour12:false,hour:'2-digit',minute:'2-digit',second:'2-digit'});
}
function formatDuration(ms){
 if(ms===null||ms===undefined)return 'duration unavailable';
 return ms<1000?Math.round(ms)+' ms':(ms/1000).toFixed(2)+' s';
}
function addMeta(row,text,className){
 const item=document.createElement('span');item.className=className||'';item.textContent=text;row.appendChild(item);
}
function renderEvent(eventName,payload,source='push'){
 const type=payload.type||eventName||'message';
 if(source==='SSE'&&Number.isFinite(payload.seq)){
  if(payload.seq<=lastSeq)return;
  lastSeq=payload.seq;
 }
 document.getElementById('empty-state')?.remove();
 const card=document.createElement('article');card.className='event-card';card.dataset.tone=eventTone(type,payload);
 const meta=document.createElement('div');meta.className='event-meta';
 addMeta(meta,payload.seq?'#'+payload.seq:'#local','event-seq');
 addMeta(meta,formatTime(payload.ts),'event-time');
 addMeta(meta,type,'event-type');
 if(payload.agent)addMeta(meta,payload.agent+(payload.depth!==undefined?' · depth '+payload.depth:''),'event-agent');
 addMeta(meta,source,'event-source');
 const summary=document.createElement('div');summary.className='event-summary';summary.textContent=eventSummary(type,payload);
 const details=document.createElement('details');
 const disclosure=document.createElement('summary');disclosure.textContent='View event payload';
 const pre=document.createElement('pre');pre.textContent='Open to render the complete payload.';
 details.addEventListener('toggle',()=>{
  if(details.open&&pre.dataset.loaded!=='true'){
   pre.textContent=JSON.stringify(payload,null,2);pre.dataset.loaded='true';
  }
 });
 details.append(disclosure,pre);card.append(meta,summary,details);log.appendChild(card);
 eventCount+=1;eventCountLabel.textContent=eventCount+(eventCount===1?' event':' events');
 log.scrollTop=log.scrollHeight;
}
function setStreamState(state,label){streamState.dataset.state=state;streamLabel.textContent=label;}
function clearEvents(){
 log.replaceChildren();
 const empty=document.createElement('div');empty.className='empty';empty.id='empty-state';empty.textContent='Waiting for the next pushed event.';log.appendChild(empty);
 eventCount=0;eventCountLabel.textContent='0 events';
}
function connectEvents(){
 if(eventSource)eventSource.close();
 setStreamState('connecting','connecting');
 eventSource=new EventSource('/sessions/'+encodeURIComponent(sid)+'/events?envelope=true'+tokenQuery());
 eventSource.addEventListener('agent_event',event=>{
  try{const payload=JSON.parse(event.data);renderEvent(payload.type,payload,'SSE');if(payload.type==='done'||payload.type==='error')refreshTrajectories();}
  catch(error){renderEvent('client_error',{type:'client_error',error:'Invalid event payload: '+error.message,ts:Date.now()/1000},'client');}
 });
 eventSource.onopen=()=>setStreamState('live','live');
 eventSource.onerror=()=>setStreamState('reconnecting','reconnecting');
}
async function responseJson(response){
 let body={};try{body=await response.json();}catch(error){body={detail:'Invalid JSON response'};}
 if(!response.ok)throw new Error(body.detail||('HTTP '+response.status));
 return body;
}
function selectedTrajectory(){
 return trajectories.find(item=>item.id===trajectorySelect.value)||null;
}
function selectTrajectory(){
 const trajectory=selectedTrajectory();
 loadTrajectoryButton.disabled=!trajectory;
 for(const [link,format] of [[exportJson,'json'],[exportJsonl,'jsonl']]){
  link.setAttribute('aria-disabled',trajectory?'false':'true');
  link.href=trajectory?'/trajectories/'+encodeURIComponent(trajectory.id)+'/export?format='+format:'#';
 }
 if(!trajectory)return;
 const metrics=trajectory.metrics||{};
 trajectoryMeta.textContent=(trajectory.status||'unknown')+' · '+formatDuration(trajectory.duration_ms)+' · '+(metrics.event_count||0)+' events · '+(metrics.tool_calls||0)+' tools';
}
async function refreshTrajectories(){
 if(!sid)return;
 try{
  const response=await fetch('/sessions/'+encodeURIComponent(sid)+'/trajectories',authHeaders());
  trajectories=await responseJson(response);
  const previous=trajectorySelect.value;
  trajectorySelect.replaceChildren();
  if(!trajectories.length){
   const option=document.createElement('option');option.textContent='No recordings yet';trajectorySelect.appendChild(option);
   trajectorySelect.disabled=true;trajectoryMeta.textContent='The next completed agent run will appear here.';
  }else{
   for(const trajectory of trajectories){
    const option=document.createElement('option');option.value=trajectory.id;
    option.textContent='#'+trajectory.run_index+' · '+trajectory.status+' · '+formatDuration(trajectory.duration_ms);
    trajectorySelect.appendChild(option);
   }
   trajectorySelect.disabled=false;
   if(trajectories.some(item=>item.id===previous))trajectorySelect.value=previous;
  }
  selectTrajectory();
 }catch(error){trajectoryMeta.textContent='Could not load trajectories: '+error.message;}
}
async function loadTrajectory(){
 const selected=selectedTrajectory();if(!selected)return;
 loadTrajectoryButton.disabled=true;loadTrajectoryButton.textContent='Loading…';
 try{
  const response=await fetch('/trajectories/'+encodeURIComponent(selected.id),authHeaders());
  const trajectory=await responseJson(response);clearEvents();
  const {events,...overview}=trajectory;
  renderEvent('trajectory_recording',{type:'trajectory_recording',...overview},'recording');
  for(const event of events)renderEvent(event.type,event,'recording');
  trajectoryMeta.textContent='Viewing '+trajectory.id+' · '+trajectory.status+' · '+formatDuration(trajectory.duration_ms);
 }catch(error){renderEvent('client_error',{type:'client_error',error:error.message,ts:Date.now()/1000},'client');}
 finally{loadTrajectoryButton.disabled=false;loadTrajectoryButton.textContent='View recording';}
}
async function mk(){
 const button=document.getElementById('create-btn');button.disabled=true;button.textContent='Creating…';
 try{
  const system=document.getElementById('sys').value||null;
  const response=await fetch('/sessions',authHeaders({method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({system})}));
  const session=await responseJson(response);sid=session.id;lastSeq=0;clearEvents();
  const sessionInfo=document.getElementById('session-info');
  const sessionId=document.createElement('strong');sessionId.textContent=sid;
  sessionInfo.replaceChildren(document.createTextNode('Active session: '),sessionId);
  connectEvents();
  await refreshTrajectories();
 }catch(error){renderEvent('client_error',{type:'client_error',error:error.message,ts:Date.now()/1000},'client');setStreamState('idle','idle');}
 finally{button.disabled=false;button.textContent='New session';}
}
async function send(){
 if(!sid)await mk();if(!sid)return;
 const message=document.getElementById('msg').value;
 const button=document.getElementById('run-btn');button.disabled=true;button.textContent='Running…';
 renderEvent('user_prompt',{type:'user_prompt',text:message,session:sid,ts:Date.now()/1000},'client');
 try{
  const response=await fetch('/sessions/'+encodeURIComponent(sid)+'/messages',authHeaders({method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({message})}));
  await responseJson(response);
 }catch(error){renderEvent('client_error',{type:'client_error',error:error.message,session:sid,ts:Date.now()/1000},'client');}
 finally{await refreshTrajectories();button.disabled=false;button.textContent='Run agent';}
}
window.addEventListener('beforeunload',()=>eventSource?.close());
</script>
</body>
</html>
"""

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: every route re-derives ownership per request and refuses with 404; the refusal path is exercised by ownership tests per endpoint."
)
