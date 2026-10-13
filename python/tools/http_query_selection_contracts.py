"""Actual authenticated ASGI scalar-query selection and validation observations."""

import asyncio
import hashlib
import inspect
import json
import os
import platform
import tempfile
from importlib.metadata import version
from pathlib import Path
from unittest.mock import patch
from urllib.parse import urlencode

from starlette.datastructures import ImmutableMultiDict, QueryParams

from mini_loop.auth import TokenAuth
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.manager import SessionManager
from mini_loop import server
from mini_loop.session import AgentSession
from mini_loop.storage import SQLiteStateStore
from mini_loop.trajectory import TrajectoryStore


def _recipes():
    rows = []

    def add(group, name, path, query=(), *, authorization="Bearer token-a", method="GET"):
        rows.append({"name": group + "/" + name, "group": group, "method": method,
                     "path": path, "query": [{"name": k, "value": v} for k, v in query],
                     "authorization": authorization,
                     "request_headers": ([{"name": "Last-Event-ID", "value": "1"}]
                                         if method == "GET" and path.endswith("/events") else [])})

    events = "/sessions/{alice_old}/events"
    for name, tokens in (("valid-last", ["invalid", "token-a"]),
                         ("invalid-last", ["token-a", "invalid"]),
                         ("empty-last", ["token-a", ""]),
                         ("empty-first", ["", "token-a"])):
        add("auth", name, events, [("access_token", token) for token in tokens],
            authorization=None, method="TRACE")
    for name, header, token in (("header-valid", "Bearer token-a", "invalid"),
                                 ("header-invalid", "Bearer invalid", "token-a"),
                                 ("header-empty", "", "token-a")):
        add("auth", name, events, [("access_token", token)], authorization=header, method="TRACE")
    for name, path, token in (("unknown-events-valid", "/missing/events", "token-a"),
                              ("unknown-events-empty", "/missing/events", ""),
                              ("non-events-query", "/sessions", "token-a"),
                              ("trailing-events-query", events + "/", "token-a")):
        add("auth", name, path, [("access_token", token)], authorization=None, method="TRACE")
    add("auth", "missing-query", events, authorization=None, method="TRACE")
    add("auth", "owner-query-last", events, [("access_token", "token-b"), ("access_token", "token-a")], authorization=None)
    add("auth", "foreign-query-last", events, [("access_token", "token-a"), ("access_token", "token-b")], authorization=None)
    add("auth", "header-foreign", events, [("access_token", "token-a")], authorization="Bearer token-b")

    limits = [("default", []), ("one-then-two", ["1", "2"]), ("two-then-one", ["2", "1"]),
              ("empty-last", ["2", ""]), ("empty-first", ["", "2"]),
              ("invalid-last", ["2", "bad"]), ("invalid-first", ["bad", "2"]),
              ("zero", ["0"]), ("negative", ["-2"]), ("large", ["501"])]
    for group, path in (("sessions", "/sessions"),
                        ("session-trajectories", "/sessions/{alice_old}/trajectories"),
                        ("trajectories", "/trajectories")):
        for name, values in limits:
            add(group, name, path, [("limit", value) for value in values])
    for name, value in (("integer-plus", "+2"), ("integer-space", " 2 "),
                        ("integer-float", "2.0"), ("integer-exponent", "2e0"),
                        ("integer-underscore", "1_0"), ("integer-unicode", "٢"),
                        ("integer-large", "999999999999999999999999999999")):
        add("sessions", name, "/sessions", [("limit", value)])
    add("session-trajectories", "foreign-invalid", "/sessions/{bob}/trajectories", [("limit", "bad")])
    add("session-trajectories", "missing-invalid", "/sessions/missing/trajectories", [("limit", "bad")])
    add("trajectories", "foreign-invalid", "/trajectories", [("session_id", "{bob}"), ("limit", "bad")])
    add("sessions", "unauthenticated-invalid", "/sessions", [("limit", "bad")], authorization=None)

    for name, values in (("default", []), ("true-last", ["false", "true"]),
                         ("false-last", ["true", "false"]), ("empty-last", ["true", ""]),
                         ("empty-first", ["", "true"]), ("invalid-last", ["true", "bad"]),
                         ("invalid-first", ["bad", "false"]), ("uppercase", ["TRUE"]),
                         ("zero", ["0"]), ("one", ["1"]), ("yes", ["yes"]),
                         ("on", ["on"]), ("off", ["off"]), ("whitespace", [" true "])):
        add("envelope", name, events, [("envelope", value) for value in values])
    add("envelope", "foreign-invalid", "/sessions/{bob}/events", [("envelope", "bad")])
    add("envelope", "missing-invalid", "/sessions/missing/events", [("envelope", "bad")])
    add("envelope", "foreign-valid", "/sessions/{bob}/events", [("envelope", "true")])

    for name, values in (("owned", ["{alice_old}"]), ("foreign", ["{bob}"]), ("empty", [""]),
                         ("foreign-last", ["{alice_old}", "{bob}"]),
                         ("owned-last", ["{bob}", "{alice_old}"]),
                         ("empty-last", ["{alice_old}", ""]),
                         ("empty-first", ["", "{alice_old}"]), ("missing", ["missing"])):
        add("trajectory-session", name, "/trajectories", [("session_id", value) for value in values])

    export = "/trajectories/{trace_alice_new}/export"
    for name, values in (("default", []), ("jsonl-last", ["json", "jsonl"]),
                         ("json-last", ["jsonl", "json"]), ("empty-last", ["json", ""]),
                         ("empty-first", ["", "json"]), ("invalid-last", ["json", "yaml"]),
                         ("invalid-first", ["yaml", "json"])):
        add("format", name, export, [("format", value) for value in values])
    add("format", "foreign-invalid", "/trajectories/{trace_bob}/export", [("format", "yaml")])
    add("format", "foreign-valid", "/trajectories/{trace_bob}/export", [("format", "json")])

    transcript = "/sessions/{alice_old}/transcript"
    for name, values in (("default", []), ("two-last", ["1", "2"]), ("one-last", ["2", "1"]),
                         ("empty-last", ["1", ""]), ("invalid-last", ["1", "bad"]),
                         ("invalid-first", ["bad", "1"]), ("zero", ["0"]), ("unavailable", ["3"])):
        add("epoch", name, transcript, [("epoch", value) for value in values])
    add("epoch", "foreign-invalid", "/sessions/{bob}/transcript", [("epoch", "bad")])
    return rows


async def _request(app, recipe, replacements):
    def resolve(value):
        for key, actual in replacements.items():
            value = value.replace("{" + key + "}", actual)
        return value

    path = resolve(recipe["path"])
    query = urlencode([(item["name"], resolve(item["value"])) for item in recipe["query"]])
    headers = [(b"host", b"fixture")]
    if recipe["authorization"] is not None:
        headers.append((b"authorization", recipe["authorization"].encode("ascii")))
    headers.extend((item["name"].lower().encode("ascii"), item["value"].encode("ascii"))
                   for item in recipe["request_headers"])
    scope = {"type": "http", "asgi": {"version": "3.0", "spec_version": "2.3"},
             "http_version": "1.1", "method": recipe["method"], "scheme": "http",
             "path": path, "raw_path": path.encode("utf-8"), "query_string": query.encode("ascii"),
             "root_path": "", "headers": headers, "client": ("127.0.0.1", 12345),
             "server": ("fixture", 80)}
    disconnected = asyncio.Event()
    initial_request = True
    observed = {"status": None, "headers": [], "chunks": []}

    async def receive():
        nonlocal initial_request
        if initial_request:
            initial_request = False
            return {"type": "http.request", "body": b"", "more_body": False}
        await disconnected.wait()
        return {"type": "http.disconnect"}

    async def send(message):
        if message["type"] == "http.response.start":
            observed["status"] = message["status"]
            observed["headers"] = message["headers"]
        elif message["type"] == "http.response.body":
            observed["chunks"].append(message.get("body", b""))
            media = dict(observed["headers"]).get(b"content-type", b"")
            if media.startswith(b"text/event-stream"):
                body = b"".join(observed["chunks"]).replace(b"\r\n", b"\n")
                if b"data:" in body and b"\n\n" in body:
                    disconnected.set()
            elif not message.get("more_body", False):
                disconnected.set()

    # The stream is genuinely infinite; only the test client disconnects after
    # its one durable catchup frame after cursor 1. Timeout/cancellation is a
    # capture failure; this fixture makes no missing-cursor/live-backlog claim.
    await asyncio.wait_for(app(scope, receive, send), timeout=5)
    if observed["status"] is None:
        raise AssertionError("ASGI request produced no response")
    return observed


def _normalise(value, identities, root):
    if isinstance(value, dict):
        return {key: _normalise(item, identities, root) for key, item in value.items()}
    if isinstance(value, list):
        return [_normalise(item, identities, root) for item in value]
    if isinstance(value, str):
        for actual, stable in identities.items():
            value = value.replace(actual, stable)
        return value.replace(str(root), "<workspace>")
    return value


def _response(observed, identities, root):
    header_names = {"content-type", "allow", "www-authenticate", "location", "content-disposition",
                    "cache-control", "x-accel-buffering", "content-security-policy",
                    "x-content-type-options", "x-frame-options", "referrer-policy"}
    headers = {key.decode("latin-1"): value.decode("latin-1") for key, value in observed["headers"]
               if key.decode("latin-1") in header_names}
    text = b"".join(observed["chunks"]).decode("utf-8")
    media = headers.get("content-type", "")
    events, body_json, body_text = [], None, None
    if media.startswith("text/event-stream"):
        kind = "sse"
        for frame in text.replace("\r\n", "\n").split("\n\n"):
            fields = {}
            for line in frame.splitlines():
                key, separator, value = line.partition(":")
                if separator and key in {"id", "event", "data"}:
                    fields[key] = value.removeprefix(" ")
            if "data" in fields:
                fields["data"] = json.loads(fields["data"])
                events.append(fields)
        if len(events) != 1:
            raise AssertionError(f"expected one replay frame before disconnect, got {len(events)}")
    elif media.startswith("application/x-ndjson"):
        kind, body_json = "jsonl", [json.loads(line) for line in text.splitlines() if line]
    elif media.startswith("application/json"):
        kind, body_json = "json", json.loads(text)
    else:
        kind, body_text = "text", text
    return _normalise({"status": observed["status"], "response_headers": headers,
                       "body_kind": kind, "body_json": body_json, "body_text": body_text,
                       "events": events}, identities, root)


async def _capture(root):
    state = SQLiteStateStore(root / "state.db")
    trajectories = TrajectoryStore(root / "trajectories")
    settings = Settings(fake_llm=True, model="fixture-model", workspace_root=root / "sessions",
                        skills_dir=root / "skills", spill_dir=None, trajectory_enabled=False,
                        ast_outline_enabled=False, enable_features=False)
    manager = SessionManager(settings, FakeAsyncAnthropic(), state_store=state, trajectory_store=trajectories)
    app = server.create_app(settings=settings, manager=manager)
    replacements, identities = {}, {}
    sessions, session_seeds, trajectory_seeds = [], [], []
    try:
        async with app.router.lifespan_context(app):
            app.state.auth = TokenAuth({"token-a": "alice", "token-b": "bob"})
            for index, (key, owner) in enumerate((("alice_old", "alice"), ("bob", "bob"), ("alice_new", "alice"))):
                created = 1000.0 + index
                with patch("mini_loop.session.time.time", return_value=created):
                    session = manager.create(owner=owner, permission_mode="readonly")
                replacements[key], identities[session.id] = session.id, key
                sessions.append(session)
                session_seeds.append({"key": key, "id": key, "owner": owner, "created_at": created})
            for index, session in enumerate(sessions):
                for sequence in (1, 2):
                    with patch("mini_loop.session.time.time", return_value=1100.0 + index + (sequence - 1) * 0.25):
                        await session.emit({"type": "status", "status": "idle", "agent": "main", "depth": 0})
            event_seeds = [{"session": identities[session.id],
                            "event": _normalise(event, identities, root)}
                           for session in sessions for event in state.load_events(session.id)]
            # Public durable APIs seed two distinct historical epochs without a
            # model turn, shell tool, or fabricated route response.
            for epoch in (1, 2):
                state.append_messages(sessions[0].id,
                                      [{"role": "user", "content": f"epoch {epoch}"}], epoch=epoch)
            for index, (key, session_key, owner) in enumerate((
                    ("trace_alice_old", "alice_old", "alice"),
                    ("trace_bob", "bob", "bob"),
                    ("trace_alice_new", "alice_old", "alice"))):
                with patch("mini_loop.trajectory.time.time", return_value=1200.0 + index):
                    trace = trajectories.start(session_id=replacements[session_key], owner=owner,
                                               run_index=index + 1, input_text=key,
                                               metadata={"model": "fixture-model"})
                trajectories.append(trace, {"type": "status", "status": "idle", "seq": 1,
                                             "ts": 1200.25 + index, "session": replacements[session_key]})
                with patch("mini_loop.trajectory.time.time", return_value=1200.5 + index):
                    trajectories.finish(trace, status="completed", output="done", duration_ms=500.0)
                replacements[key], identities[trace] = trace, key
                trajectory_seeds.append({"key": key, "id": key, "session": session_key,
                                         "owner": owner, "run_index": index + 1,
                                         "raw_jsonl": _normalise("".join(trajectories.stream_raw(trace)), identities, root)})

            def effects():
                return {"sessions": len(manager.list()),
                        "events": sum(state.event_cursor(session.id) for session in sessions),
                        "subscribers": sum(len(session._subscribers) for session in sessions),
                        "trajectory_files": len(list(trajectories.root.glob("traj_*.jsonl")))}

            cases = []
            for recipe in _recipes():
                before = effects()
                observed = await _request(app, recipe, replacements)
                after = effects()
                if after["subscribers"] != 0:
                    raise AssertionError("SSE disconnect left a subscribed queue")
                cases.append({**recipe, **_response(observed, identities, root),
                              "effects": {"before": before, "after": after}})
            seeds = {"sessions": session_seeds, "events": event_seeds, "trajectories": trajectory_seeds,
                     "transcript_epochs": [{"epoch": epoch, "messages": [{"role": "user", "content": f"epoch {epoch}"}]}
                                           for epoch in (1, 2)]}
            return seeds, cases
    finally:
        state.close()


def http_query_selection_contracts() -> dict:
    # The real lifespan still constructs its authenticator. Keep host token
    # configuration from changing that setup before the explicit two-owner
    # TokenAuth is injected; neither authentication nor routes are replaced.
    with patch.dict(os.environ, {"MINILOOP_API_TOKENS": "alice:token-a,bob:token-b",
                                 "MINILOOP_API_TOKEN": ""}, clear=True), \
            tempfile.TemporaryDirectory(prefix="mini-loop-query-selection-") as scratch:
        seeds, cases = asyncio.run(_capture(Path(scratch)))
    sources = {"create_app": server.create_app, "credential": server._credential,
               "principal": server._principal, "require": server._require,
               "routes": server._register_routes, "TokenAuth.authenticate": TokenAuth.authenticate,
               "QueryParams.__init__": QueryParams.__init__,
               "ImmutableMultiDict.__init__": ImmutableMultiDict.__init__,
               "SessionManager.create": SessionManager.create, "AgentSession.emit": AgentSession.emit,
               "AgentSession.subscribe": AgentSession.subscribe,
               "TrajectoryStore.list": TrajectoryStore.list,
               "TrajectoryStore.stream_raw": TrajectoryStore.stream_raw,
               "SQLiteStateStore.append_messages": SQLiteStateStore.append_messages,
               "SQLiteStateStore.load_events": SQLiteStateStore.load_events}
    return {"versions": {"python": platform.python_version(), **{name: version(name) for name in
                         ("fastapi", "starlette", "pydantic", "httpx", "sse-starlette")}},
            "source_sha256": {name: hashlib.sha256(inspect.getsource(function).encode()).hexdigest()
                              for name, function in sources.items()},
            "scope": "Actual create_app middleware and FastAPI scalar validation, TokenAuth, real SessionManager/FakeAsyncAnthropic, SQLite epochs and TrajectoryStore, with an isolated environment. Two status events are genuinely emitted per session; GET event requests use Last-Event-ID: 1 and the direct ASGI client disconnects after durable catchup frame 2. This fixture makes no claim about missing-cursor/live-backlog behavior. Seeds contain actual SQLite.load_events records and TrajectoryStore.stream_raw text. Only generated IDs/workspace paths and seed timestamps are normalized; no route or query parser is replaced, no model turn or external network/shell runs.",
            "seeds": seeds, "cases": cases}
