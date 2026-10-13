"""Actual raw ASGI query-byte decoding and authenticated route observations."""

import asyncio
import hashlib
import inspect
import os
import platform
import tempfile
import urllib.parse
from importlib.metadata import version
from pathlib import Path
from unittest.mock import patch

from starlette.datastructures import ImmutableMultiDict, QueryParams, URL
from starlette.requests import HTTPConnection

from mini_loop import server
from mini_loop.auth import TokenAuth
from mini_loop.manager import SessionManager
from mini_loop.session import AgentSession
from mini_loop.storage import SQLiteStateStore
from mini_loop.trajectory import TrajectoryStore
from tools.http_query_selection_contracts import _capture, _request


def _recipes():
    rows = []

    def add(group, name, raw, *, path=None, authorization=None):
        path = path or ("/sessions/{alice_old}/events" if group == "auth" else "/sessions")
        rows.append({
            "name": group + "/" + name, "group": group, "method": "GET",
            "path": path, "query": [], "raw_query_hex": raw.hex(),
            "authorization": authorization,
            "request_headers": ([{"name": "Last-Event-ID", "value": "1"}]
                                if path.endswith("/events") else []),
        })

    # Every byte below enters scope.query_string unchanged. In particular, this
    # producer never asks a URL encoder to repair malformed percent sequences.
    for name, tail in (
        ("percent-last", b"%"), ("bad-escape-last", b"%GG"),
        ("semicolon-last", b"bad;tail"), ("encoded-semicolon-last", b"bad%3Btail"),
        ("escaped-percent-last", b"%25"), ("utf8-valid-last", b"%E4%B8%AD"),
        ("utf8-truncated-last", b"%E2%82"),
    ):
        add("auth", name, b"access_token=token-a&access_token=" + tail)
    add("auth", "percent-first", b"access_token=%&access_token=token-a")
    add("auth", "header-valid", b"access_token=token-a&access_token=%", authorization="Bearer token-a")
    add("auth", "header-invalid", b"access_token=token-a", authorization="Bearer invalid")
    add("auth", "encoded-key", b"access%5Ftoken=token-a")
    add("auth", "foreign", b"access_token=token-b&ignored=%")

    for name, tail in (
        ("percent-last", b"%"), ("bad-escape-last", b"%GG"),
        ("semicolon-last", b"bad;tail"), ("encoded-semicolon-last", b"bad%3Btail"),
        ("escaped-percent-last", b"%25"), ("utf8-valid-last", b"%E4%B8%AD"),
        ("utf8-truncated-last", b"%E2%82"),
        ("utf8-invalid-percent-last", b"%FF"),
        ("valid-literal-utf8-last", "中".encode("utf-8")),
    ):
        add("limit", name, b"limit=2&limit=" + tail, authorization="Bearer token-a")
    add("limit", "percent-first", b"limit=%&limit=2", authorization="Bearer token-a")
    add("limit", "encoded-key", b"limit=1&l%69mit=2", authorization="Bearer token-a")
    add("limit", "foreign-invalid", b"limit=2&limit=%",
        path="/sessions/{bob}/trajectories", authorization="Bearer token-a")
    return rows


def http_query_wire_contracts() -> dict:
    """Capture the installed Source stack; no route or parser is replaced."""
    with patch.dict(os.environ, {"MINILOOP_API_TOKENS": "alice:token-a,bob:token-b",
                                 "MINILOOP_API_TOKEN": ""}, clear=True), \
            tempfile.TemporaryDirectory(prefix="mini-loop-query-wire-") as scratch:
        seeds, cases = asyncio.run(_capture(Path(scratch), _recipes()))
    sources = {
        "create_app": server.create_app, "credential": server._credential,
        "principal": server._principal, "require": server._require,
        "routes": server._register_routes, "TokenAuth.authenticate": TokenAuth.authenticate,
        "HTTPConnection.query_params": HTTPConnection.query_params.fget,
        "URL.__init__": URL.__init__,
        "QueryParams.__init__": QueryParams.__init__,
        "ImmutableMultiDict.__init__": ImmutableMultiDict.__init__,
        "urllib.parse.parse_qsl": urllib.parse.parse_qsl,
        "urllib.parse.unquote": urllib.parse.unquote,
        "urllib.parse.unquote_to_bytes": urllib.parse.unquote_to_bytes,
        "SessionManager.create": SessionManager.create,
        "AgentSession.emit": AgentSession.emit,
        "AgentSession.subscribe": AgentSession.subscribe,
        "SQLiteStateStore.load_events": SQLiteStateStore.load_events,
        "SQLiteStateStore.append_messages": SQLiteStateStore.append_messages,
        "TrajectoryStore.list": TrajectoryStore.list,
        "TrajectoryStore.stream_raw": TrajectoryStore.stream_raw,
        "capture_helper": _capture, "request_helper": _request,
    }
    return {
        "versions": {"python": platform.python_version(), **{name: version(name) for name in
                     ("fastapi", "starlette", "pydantic", "httpx", "sse-starlette")}},
        "source_sha256": {name: hashlib.sha256(inspect.getsource(function).encode()).hexdigest()
                          for name, function in sources.items()},
        "scope": "Actual create_app middleware, TokenAuth, Starlette QueryParams/urllib.parse and FastAPI scalar validation with raw_query_hex decoded directly into ASGI scope query_string. Real SessionManager/FakeAsyncAnthropic, SQLite epochs/events and TrajectoryStore use an isolated environment. Successful SSE requests explicitly use Last-Event-ID: 1 and disconnect after durable catchup sequence 2; no new missing-cursor/live-backlog claim. Seeds are actual stored events and trajectory JSONL. Only generated identities/workspace paths and seed timestamps are normalized. No route/parser is replaced and no model turn, shell or external network runs. Invalid UTF-8 is exercised after percent decoding; raw invalid UTF-8 rejected earlier by URL construction is outside this corpus. ASGI byte observations do not prove Uvicorn/proxy admission or complete HTTP wire parity.",
        "seeds": seeds, "cases": cases,
    }
