"""Named Activities resolve owner from persistent workflow bindings, not models.

The caller chooses the database at worker startup. No per-Workflow backend object
or process-local backend reference crosses an Activity boundary.
"""

from __future__ import annotations

import asyncio
from collections.abc import Iterator
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import sqlite3
from typing import Any

from temporalio import activity
from temporalio.exceptions import ApplicationError

_database: Path | None = None
_identity = "unconfigured"


@contextmanager
def _connect(database: str | Path) -> Iterator[sqlite3.Connection]:
    connection = sqlite3.connect(str(database), timeout=5)
    try:
        connection.row_factory = sqlite3.Row
        connection.execute("PRAGMA busy_timeout=5000")
        with connection:
            yield connection
    finally:
        connection.close()


def initialize_database(database: str | Path) -> None:
    """Create the local prototype ledger; never use this as an HA claim."""
    path = Path(database).resolve()
    path.parent.mkdir(parents=True, exist_ok=True)
    with _connect(path) as connection:
        connection.execute("PRAGMA journal_mode=WAL")
        connection.executescript("""
            CREATE TABLE IF NOT EXISTS bindings (
                workflow_id TEXT PRIMARY KEY, owner TEXT NOT NULL,
                request_sha256 TEXT
            );
            CREATE TABLE IF NOT EXISTS memories (
                owner TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL,
                PRIMARY KEY (owner, key)
            );
            CREATE TABLE IF NOT EXISTS effects (
                workflow_id TEXT NOT NULL, operation_id TEXT NOT NULL,
                owner TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL,
                payload_sha256 TEXT NOT NULL, committed_by TEXT NOT NULL,
                committed_pid INTEGER NOT NULL,
                PRIMARY KEY (workflow_id, operation_id)
            );
            CREATE TABLE IF NOT EXISTS gates (
                workflow_id TEXT NOT NULL, gate_key TEXT NOT NULL,
                released INTEGER NOT NULL DEFAULT 0,
                PRIMARY KEY (workflow_id, gate_key)
            );
            CREATE TABLE IF NOT EXISTS activity_calls (
                call_id INTEGER PRIMARY KEY AUTOINCREMENT,
                workflow_id TEXT NOT NULL, run_id TEXT NOT NULL,
                activity_id TEXT NOT NULL, attempt INTEGER NOT NULL,
                worker TEXT NOT NULL, pid INTEGER NOT NULL,
                kind TEXT NOT NULL, operation_id TEXT NOT NULL,
                status TEXT NOT NULL
            );
        """)


def configure_database(database: str | Path, identity: str) -> None:
    """Process startup configuration, not a Workflow or Activity input."""
    global _database, _identity
    _database = Path(database).resolve()
    _identity = identity
    initialize_database(_database)


def _text(value: Any, label: str) -> str:
    if not isinstance(value, str) or not value or len(value) > 4096:
        raise ValueError(f"Invalid {label}")
    return value


def bind_workflow(
    database: str | Path, workflow_id: str, owner: str,
    request_sha256: str | None = None,
) -> None:
    """Trusted admission fixture: persist an immutable workflow-to-owner binding."""
    _text(workflow_id, "workflow ID")
    _text(owner, "owner")
    with _connect(database) as connection:
        connection.execute("BEGIN IMMEDIATE")
        existing = connection.execute(
            "SELECT owner, request_sha256 FROM bindings WHERE workflow_id=?", (workflow_id,),
        ).fetchone()
        if existing is not None:
            if existing["owner"] != owner:
                raise PermissionError("Workflow belongs to another owner")
            if request_sha256 is not None and existing["request_sha256"] not in (None, request_sha256):
                raise ValueError("Workflow ID already has a different request")
            if request_sha256 is not None:
                connection.execute(
                    "UPDATE bindings SET request_sha256=? WHERE workflow_id=?", (request_sha256, workflow_id),
                )
        else:
            connection.execute("INSERT INTO bindings VALUES (?, ?, ?)", (workflow_id, owner, request_sha256))


def lookup_owner(database: str | Path, workflow_id: str) -> str | None:
    with _connect(database) as connection:
        row = connection.execute("SELECT owner FROM bindings WHERE workflow_id=?", (workflow_id,)).fetchone()
        return row["owner"] if row is not None else None


def release_gate(database: str | Path, workflow_id: str, gate_key: str = "after-effect") -> None:
    with _connect(database) as connection:
        connection.execute(
            "INSERT INTO gates VALUES (?, ?, 1) ON CONFLICT(workflow_id, gate_key) "
            "DO UPDATE SET released=1", (workflow_id, gate_key),
        )


def inspect_database(database: str | Path) -> dict[str, list[dict[str, Any]]]:
    with _connect(database) as connection:
        result = {
            table: [dict(row) for row in connection.execute(f"SELECT * FROM {table} ORDER BY rowid")]
            for table in ("bindings", "effects", "activity_calls", "gates", "memories")
        }
    for row in result["activity_calls"]:
        row["activity"] = row["kind"]
    for row in result["effects"]:
        row["effect_id"] = row["operation_id"]
    return result


def _context() -> tuple[Path, Any]:
    info = activity.info()
    if _database is None or info.workflow_id is None or info.workflow_run_id is None:
        raise ApplicationError("A bound Workflow Activity is required", non_retryable=True)
    return _database, info


def _owner(connection: sqlite3.Connection, workflow_id: str) -> str:
    row = connection.execute("SELECT owner FROM bindings WHERE workflow_id=?", (workflow_id,)).fetchone()
    if row is None:
        raise ApplicationError("Workflow has no trusted owner binding", non_retryable=True)
    return row["owner"]


def _record(connection: sqlite3.Connection, info: Any, kind: str, operation: str, status: str) -> int:
    cursor = connection.execute(
        "INSERT INTO activity_calls (workflow_id, run_id, activity_id, attempt, worker, pid, "
        "kind, operation_id, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
        (info.workflow_id, info.workflow_run_id, info.activity_id, info.attempt,
         _identity, os.getpid(), kind, operation, status),
    )
    return int(cursor.lastrowid)


def _put(database: Path, info: Any, request: dict[str, Any]) -> tuple[dict[str, str], int]:
    if set(request) - {"key", "value", "operation_id", "block_after_commit"}:
        raise ApplicationError("Unexpected memory request fields", non_retryable=True)
    try:
        key, value, operation = (_text(request.get(k), k) for k in ("key", "value", "operation_id"))
    except ValueError as error:
        raise ApplicationError(str(error), non_retryable=True) from None
    if type(request.get("block_after_commit", False)) is not bool:
        raise ApplicationError("Invalid gate request", non_retryable=True)
    with _connect(database) as connection:
        connection.execute("BEGIN IMMEDIATE")
        owner = _owner(connection, info.workflow_id)
        digest = hashlib.sha256(json.dumps([owner, key, value], ensure_ascii=False).encode()).hexdigest()
        existing = connection.execute(
            "SELECT payload_sha256 FROM effects WHERE workflow_id=? AND operation_id=?",
            (info.workflow_id, operation),
        ).fetchone()
        if existing is not None and existing["payload_sha256"] != digest:
            raise ApplicationError("Logical operation payload conflict", non_retryable=True)
        if existing is None:
            connection.execute(
                "INSERT INTO memories VALUES (?, ?, ?) ON CONFLICT(owner, key) DO UPDATE SET value=excluded.value",
                (owner, key, value),
            )
            connection.execute("INSERT INTO effects VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
                               (info.workflow_id, operation, owner, key, value, digest, _identity, os.getpid()))
        call_id = _record(connection, info, "memory_put", operation,
                          "effect_committed" if existing is None else "effect_reused")
        connection.execute("INSERT OR IGNORE INTO gates VALUES (?, 'after-effect', 0)", (info.workflow_id,))
    return {"key": key, "value": value, "operation_id": operation}, call_id


def _gate_released(database: Path, workflow_id: str) -> bool:
    with _connect(database) as connection:
        row = connection.execute(
            "SELECT released FROM gates WHERE workflow_id=? AND gate_key='after-effect'", (workflow_id,),
        ).fetchone()
        return row is not None and bool(row["released"])


def _returned(database: Path, call_id: int) -> None:
    with _connect(database) as connection:
        connection.execute("UPDATE activity_calls SET status='returned' WHERE call_id=?", (call_id,))


@activity.defn(name="prototype_memory_put")
async def memory_put(request: dict[str, Any]) -> dict[str, str]:
    """Idempotent local memory write; fault gate is after commit, before ack."""
    database, info = _context()
    receipt, call_id = await asyncio.to_thread(_put, database, info, request)
    if request.get("block_after_commit", False):
        while not await asyncio.to_thread(_gate_released, database, info.workflow_id):
            activity.heartbeat({"operation_id": receipt["operation_id"], "effect_committed": True})
            await asyncio.sleep(0.2)
    await asyncio.to_thread(_returned, database, call_id)
    return receipt


@activity.defn(name="prototype_memory_get")
async def memory_get(request: dict[str, Any]) -> dict[str, str | None]:
    """Read only the namespace selected by the trusted workflow binding."""
    database, info = _context()
    if set(request) != {"key"}:
        raise ApplicationError("A memory key is required", non_retryable=True)
    try:
        key = _text(request["key"], "key")
    except ValueError as error:
        raise ApplicationError(str(error), non_retryable=True) from None

    def read() -> dict[str, str | None]:
        with _connect(database) as connection:
            owner = _owner(connection, info.workflow_id)
            row = connection.execute("SELECT value FROM memories WHERE owner=? AND key=?", (owner, key)).fetchone()
            _record(connection, info, "memory_get", key, "returned")
            return {"key": key, "value": row["value"] if row is not None else None}

    return await asyncio.to_thread(read)
