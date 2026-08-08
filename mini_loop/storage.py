"""Durable conversation state -- the transcript and event log survive a restart.

`TrajectoryStore` records what happened for audit; it is a projection, and it
redacts content when asked to. This module is the other half: the state a
process needs to *resume* a session it did not start -- the model-facing
transcript and the event cursor an SSE client reconnects against.

Roadmap `R1` picks SQLite (WAL) as the backend, because the later action journal
needs actions and events ordered inside one transaction and a file log cannot
give that. The *append contract* is taken from the OpenHands SDK `EventLog`
(``openhands-sdk/openhands/sdk/conversation/event_store.py``), which solves the
same problem over a file store, and whose hard-won properties are backend
independent:

* **Order is data, not an implementation detail.** OpenHands encodes the ordinal
  in each event's filename so the index can be rebuilt by listing a directory.
  Here every row carries an explicit ``ordinal`` with a ``UNIQUE(session_id,
  ordinal)`` constraint, so a gap or a double-write is a database error rather
  than a silently reordered history.
* **Re-read the head before appending.** OpenHands must re-scan the directory
  after taking its lock, because another process may have written while it
  waited. The equivalent here is reading ``MAX(ordinal)`` *inside* the writing
  transaction -- SQLite's transaction is what its file lock plus rescan buys.
* **Never materialize the whole history to read the tail.** OpenHands reads one
  event file at a time and bounds its stuck-detector scan; every read here takes
  ``after``/``limit`` and is served by an index.

**Scope.** This lands the ``sessions`` / ``messages`` / ``events`` tables and
restart recovery of a transcript and an event cursor. It deliberately does not
land the rest of R1: there is no action journal, no outbox, no run state
machine, no cross-process claim or lease, and no fork/snapshot. A second process
opening the same database will read consistently but nothing stops both from
advancing the same session -- that ordering work is the next phase, not this
one.

**Transcripts are epoched, because compaction rewrites them.** The store mirrors
`agent.messages`, which compaction edits *in place* -- sometimes shortening it
(auto-compaction replaces history with a summary), sometimes editing old entries
without changing the length (tool-result snipping). Mirroring a mutable list
into an append-only table by index cannot survive either: the rows become a
splice of two histories, with `tool_use` blocks whose `tool_result` no longer
follows. OpenHands avoids the problem structurally -- its log is never
rewritten, condensation is *another event*, and the conversation view is a
projection over the log. Here the equivalent is an `epoch`: when the live
transcript stops extending what was persisted, the next flush opens a new epoch
and writes the whole thing. Superseded epochs stay on disk as the record of what
the agent actually saw before compaction. Detection compares object identity across the
persisted prefix: every rewrite here goes through ``messages[:] = [...]``, which
builds new dicts, so a replaced entry is a different object. An earlier version
hashed the prefix instead -- correct, but O(bytes) on *every* event, measured at
5.5 ms per pass over a 1.6 MB transcript (~550 ms across 100 events). Identity
comparison is 260-990x cheaper and detects the same rewrites; mutating a message
dict in place would be invisible to it. `microcompact` did exactly that for
several rounds while this sentence claimed nothing did, so compaction never
reached the store; the rewriters are now pinned by
`tests/test_compaction_composition.py` rather than by assertion here. This copy
of the claim outlived the fix to the original in `session.py` -- a stale claim
duplicates like any other line, and correcting the site you found leaves the
copy lying.

**Content.** Rows hold the real transcript, unredacted -- a redacted transcript
cannot be resumed. Treat the database as sensitive as the workspace.
"""

from __future__ import annotations

import json
import sqlite3
import threading
import time
from collections.abc import Iterable, Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Protocol

__all__ = [
    "SCHEMA_VERSION",
    "SessionRecord",
    "StateStore",
    "NullStateStore",
    "SQLiteStateStore",
    "StorageSchemaError",
]

SCHEMA_VERSION = 6


class StorageSchemaError(RuntimeError):
    """The database on disk was written by an incompatible version."""


@dataclass(frozen=True, slots=True)
class SessionRecord:
    """What is needed to rebuild a live session handle after a restart."""

    session_id: str
    workspace: str
    system: str | None
    created_at: float
    run_count: int
    status: str
    event_cursor: int
    # Harness-side state that the transcript mentions but does not rebuild.
    todos: tuple[dict, ...] = ()
    # The tenant that owns the session, restored so a restart does not orphan it.
    owner: str = "anonymous"


def _json_safe(value: Any) -> Any:
    """Detach provider objects into JSON-serializable data.

    Assistant turns hold provider block objects. They must land as plain data or
    the transcript cannot be replayed by a different process (or a different SDK
    version) than the one that produced it.

    Block conversion is delegated to ``agent._content_payload`` rather than
    reimplemented. A second, simpler converter lived here and fell back to
    ``str(value)`` for anything without ``model_dump`` -- which the real
    Anthropic SDK has, so production looked fine while every non-pydantic
    client (including the offline fake, and therefore ``MINILOOP_FAKE_LLM=1``)
    silently persisted `"ToolUseBlock(...)"` strings instead of tool calls.
    Two serializers for one job is one too many.
    """

    if isinstance(value, (str, int, float, bool)) or value is None:
        return value
    if isinstance(value, Mapping):
        return {
            str(k): _content_blocks(v) if k == "content" else _json_safe(v)
            for k, v in value.items()
        }
    if isinstance(value, (list, tuple)):
        return [_json_safe(v) for v in value]
    dump = getattr(value, "model_dump", None)
    if callable(dump):
        try:
            return _json_safe(dump())
        except Exception:  # pragma: no cover - provider-specific failure
            return _block_payload(value)
    return _block_payload(value)


def _block_payload(value: Any) -> Any:
    """One provider block, through the loop's own converter."""

    from .agent import _content_payload

    return _content_payload([value])[0]


def _content_blocks(value: Any) -> Any:
    """A message's ``content``: a string, or a list of provider blocks."""

    if isinstance(value, str) or value is None:
        return value
    if isinstance(value, (list, tuple)):
        from .agent import _content_payload

        return _json_safe(_content_payload(list(value)))
    return _json_safe(value)


class StateStore(Protocol):
    """Durable conversation state. Implementations must be thread-safe."""

    def upsert_session(self, record: SessionRecord) -> None: ...

    def load_sessions(self) -> list[SessionRecord]: ...

    def append_messages(
        self, session_id: str, messages: Sequence[Any], *, epoch: int = 1
    ) -> int: ...

    def load_messages(self, session_id: str, *, epoch: int | None = None) -> list[Any]: ...

    def message_count(self, session_id: str, *, epoch: int | None = None) -> int: ...

    def append_event(self, session_id: str, event: Mapping[str, Any]) -> int: ...

    def load_events(
        self,
        session_id: str,
        *,
        after: int = 0,
        limit: int | None = None,
    ) -> list[dict]: ...

    def transcript_epoch(self, session_id: str) -> int: ...

    def event_cursor(self, session_id: str) -> int: ...

    def delete_session(self, session_id: str) -> None: ...

    def close(self) -> None: ...


class NullStateStore:
    """Persist nothing. The behaviour before this module existed."""

    def upsert_session(self, record: SessionRecord) -> None:
        return None

    def load_sessions(self) -> list[SessionRecord]:
        return []

    def append_messages(
        self, session_id: str, messages: Sequence[Any], *, epoch: int = 1
    ) -> int:
        return 0

    def load_messages(self, session_id, *, epoch=None) -> list[Any]:
        return []

    def message_count(self, session_id, *, epoch=None) -> int:
        return 0

    def append_event(self, session_id: str, event: Mapping[str, Any]) -> int:
        return 0

    def load_events(self, session_id, *, after=0, limit=None) -> list[dict]:
        return []

    def transcript_epoch(self, session_id: str) -> int:
        return 0

    def event_cursor(self, session_id: str) -> int:
        return 0

    def delete_session(self, session_id: str) -> None:
        return None

    def close(self) -> None:
        return None


_SCHEMA = """
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
    session_id   TEXT PRIMARY KEY,
    workspace    TEXT NOT NULL,
    system       TEXT,
    created_at   REAL NOT NULL,
    run_count    INTEGER NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'idle',
    todos        TEXT NOT NULL DEFAULT '[]',
    owner        TEXT NOT NULL DEFAULT 'anonymous',
    lease_owner  TEXT,
    lease_until  REAL NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
    session_id TEXT NOT NULL,
    ordinal    INTEGER NOT NULL,
    epoch      INTEGER NOT NULL DEFAULT 1,
    payload    TEXT NOT NULL,
    PRIMARY KEY (session_id, ordinal)
);
CREATE TABLE IF NOT EXISTS actions (
    action_id       TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL,
    message_id      TEXT NOT NULL,
    tool_use_id     TEXT NOT NULL,
    tool_name       TEXT NOT NULL,
    input_hash      TEXT NOT NULL,
    status          TEXT NOT NULL,
    result          TEXT,
    workflow_run_id TEXT,
    created_at      REAL NOT NULL,
    completed_at    REAL
);
CREATE TABLE IF NOT EXISTS events (
    session_id TEXT NOT NULL,
    ordinal    INTEGER NOT NULL,
    payload    TEXT NOT NULL,
    PRIMARY KEY (session_id, ordinal)
);
CREATE TABLE IF NOT EXISTS approvals (
    approval_id   TEXT PRIMARY KEY,
    session_id    TEXT NOT NULL,
    tool_use_id   TEXT,
    tool_name     TEXT NOT NULL,
    rule          TEXT NOT NULL,
    message       TEXT NOT NULL,
    input_preview TEXT NOT NULL,
    status        TEXT NOT NULL,
    created_at    REAL NOT NULL,
    resolved_at   REAL,
    kind          TEXT NOT NULL DEFAULT 'approval',
    answer        TEXT
);
"""


class SQLiteStateStore:
    """SQLite (WAL) implementation of :class:`StateStore`.

    One connection guarded by a lock: SQLite serializes writers anyway, and a
    single connection keeps ``MAX(ordinal)`` and the insert that follows it in
    the same transaction, which is the whole point of the append contract.
    """

    def __init__(self, path: Path | str) -> None:
        self.path = Path(path)
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self._lock = threading.RLock()
        self._db = sqlite3.connect(
            str(self.path),
            check_same_thread=False,
            isolation_level=None,  # explicit transactions
        )
        self._db.row_factory = sqlite3.Row
        self._db.execute("PRAGMA journal_mode=WAL")
        self._db.execute("PRAGMA synchronous=NORMAL")
        self._db.execute("PRAGMA foreign_keys=ON")
        self._migrate()

    # -- schema ------------------------------------------------------------
    def _migrate(self) -> None:
        with self._lock:
            self._db.executescript(_SCHEMA)
            row = self._db.execute("SELECT version FROM schema_version").fetchone()
            if row is None:
                self._db.execute(
                    "INSERT INTO schema_version (version) VALUES (?)", (SCHEMA_VERSION,)
                )
                self._create_indexes()
                return
            found = int(row["version"])
            if found > SCHEMA_VERSION:
                raise StorageSchemaError(
                    f"{self.path} was written by schema v{found}; this build "
                    f"understands v{SCHEMA_VERSION}. Refusing to open it rather "
                    "than silently dropping fields."
                )
            if found < SCHEMA_VERSION:
                self._upgrade(found)
            self._create_indexes()

    def _upgrade(self, found: int) -> None:
        """Additive migrations only; each step is idempotent."""

        session_columns = {
            row["name"]
            for row in self._db.execute("PRAGMA table_info(sessions)").fetchall()
        }
        if session_columns and "owner" not in session_columns:
            # The tenant owner (round 138). Without it a restart rebuilt every
            # session as the `anonymous` default, orphaning it from its real
            # owner under authentication. Pre-owner rows default to `anonymous`,
            # which is correct for the single-user deployments that predate it.
            self._db.execute(
                "ALTER TABLE sessions ADD COLUMN owner TEXT NOT NULL "
                "DEFAULT 'anonymous'"
            )
        approval_columns = {
            row["name"]
            for row in self._db.execute("PRAGMA table_info(approvals)").fetchall()
        }
        if approval_columns and "kind" not in approval_columns:
            self._db.execute(
                "ALTER TABLE approvals ADD COLUMN kind TEXT NOT NULL DEFAULT 'approval'"
            )
        if approval_columns and "answer" not in approval_columns:
            self._db.execute("ALTER TABLE approvals ADD COLUMN answer TEXT")
        if found < 5:
            columns = {
                row["name"]
                for row in self._db.execute("PRAGMA table_info(sessions)").fetchall()
            }
            if "lease_owner" not in columns:
                self._db.execute("ALTER TABLE sessions ADD COLUMN lease_owner TEXT")
            if "lease_until" not in columns:
                self._db.execute(
                    "ALTER TABLE sessions ADD COLUMN lease_until REAL NOT NULL DEFAULT 0"
                )
        if found < 4:
            self._db.executescript(
                """
                CREATE TABLE IF NOT EXISTS actions (
                    action_id       TEXT PRIMARY KEY,
                    session_id      TEXT NOT NULL,
                    message_id      TEXT NOT NULL,
                    tool_use_id     TEXT NOT NULL,
                    tool_name       TEXT NOT NULL,
                    input_hash      TEXT NOT NULL,
                    status          TEXT NOT NULL,
                    result          TEXT,
                    workflow_run_id TEXT,
                    created_at      REAL NOT NULL,
                    completed_at    REAL
                );
                """
            )
        if found < 3:
            columns = {
                row["name"]
                for row in self._db.execute("PRAGMA table_info(sessions)").fetchall()
            }
            if "todos" not in columns:
                self._db.execute(
                    "ALTER TABLE sessions ADD COLUMN todos TEXT NOT NULL DEFAULT '[]'"
                )
        if found < 2:
            columns = {
                row["name"]
                for row in self._db.execute("PRAGMA table_info(messages)").fetchall()
            }
            if "epoch" not in columns:
                self._db.execute(
                    "ALTER TABLE messages ADD COLUMN epoch INTEGER NOT NULL DEFAULT 1"
                )
        self._db.execute("UPDATE schema_version SET version = ?", (SCHEMA_VERSION,))

    # -- leases -----------------------------------------------------------
    def acquire_lease(self, session_id: str, owner: str, *, ttl: float) -> bool:
        """Claim the right to advance this session. One statement, no read first.

        Two processes on one database will otherwise both drive the same
        session: their turns interleave into a single transcript, producing
        consecutive user messages and orphaned tool results -- a shape the
        provider rejects outright.

        The claim is taken by a conditional UPDATE and confirmed by its row
        count. Reading the row and then writing it would leave a window in
        which both readers see it free.
        """

        now = time.time()
        with self._lock:
            cursor = self._db.execute(
                """
                UPDATE sessions
                   SET lease_owner = ?, lease_until = ?
                 WHERE session_id = ?
                   AND (lease_owner IS NULL OR lease_owner = ? OR lease_until < ?)
                """,
                (owner, now + ttl, session_id, owner, now),
            )
            return cursor.rowcount == 1

    def renew_lease(self, session_id: str, owner: str, *, ttl: float) -> bool:
        """Extend a lease we still hold. A lost lease is not silently retaken."""

        now = time.time()
        with self._lock:
            cursor = self._db.execute(
                "UPDATE sessions SET lease_until = ? "
                "WHERE session_id = ? AND lease_owner = ? AND lease_until >= ?",
                (now + ttl, session_id, owner, now),
            )
            return cursor.rowcount == 1

    def release_lease(self, session_id: str, owner: str) -> None:
        with self._lock:
            self._db.execute(
                "UPDATE sessions SET lease_owner = NULL, lease_until = 0 "
                "WHERE session_id = ? AND lease_owner = ?",
                (session_id, owner),
            )

    def lease_holder(self, session_id: str) -> str | None:
        """Who holds it right now, or `None` when free or expired."""

        with self._lock:
            row = self._db.execute(
                "SELECT lease_owner, lease_until FROM sessions WHERE session_id = ?",
                (session_id,),
            ).fetchone()
        if row is None or row["lease_owner"] is None:
            return None
        return row["lease_owner"] if row["lease_until"] >= time.time() else None

    # -- actions ----------------------------------------------------------
    def read_action(self, action_id: str) -> dict | None:
        with self._lock:
            row = self._db.execute(
                "SELECT * FROM actions WHERE action_id = ?", (action_id,)
            ).fetchone()
        return dict(row) if row is not None else None

    def write_action(self, record: Mapping[str, Any]) -> None:
        """Insert or update one action row. The caller owns the transition rules."""

        with self._lock:
            self._db.execute(
                """
                INSERT INTO actions (action_id, session_id, message_id, tool_use_id,
                                     tool_name, input_hash, status, result,
                                     workflow_run_id, created_at, completed_at)
                VALUES (:action_id, :session_id, :message_id, :tool_use_id,
                        :tool_name, :input_hash, :status, :result,
                        :workflow_run_id, :created_at, :completed_at)
                ON CONFLICT(action_id) DO UPDATE SET
                    status          = excluded.status,
                    result          = excluded.result,
                    workflow_run_id = excluded.workflow_run_id,
                    completed_at    = excluded.completed_at
                """,
                dict(record),
            )

    def write_approval(self, record: Mapping[str, Any]) -> None:
        """Insert or update one approval row (see approvals.py for the states)."""

        row = {"kind": "approval", "answer": None, **dict(record)}
        with self._lock:
            self._db.execute(
                """
                INSERT INTO approvals (approval_id, session_id, tool_use_id,
                                       tool_name, rule, message, input_preview,
                                       status, created_at, resolved_at, kind,
                                       answer)
                VALUES (:approval_id, :session_id, :tool_use_id, :tool_name,
                        :rule, :message, :input_preview, :status, :created_at,
                        :resolved_at, :kind, :answer)
                ON CONFLICT(approval_id) DO UPDATE SET
                    status      = excluded.status,
                    resolved_at = excluded.resolved_at,
                    answer      = excluded.answer
                """,
                row,
            )

    def read_approvals(
        self, session_id: str, *, status: str | None = None
    ) -> list[dict]:
        query = "SELECT * FROM approvals WHERE session_id = ?"
        args: list = [session_id]
        if status is not None:
            query += " AND status = ?"
            args.append(status)
        with self._lock:
            rows = self._db.execute(query + " ORDER BY created_at", args).fetchall()
        return [dict(row) for row in rows]

    def mark_inflight_unknown(self, session_id: str | None = None) -> list[str]:
        """Close out actions this process cannot account for.

        An action still `started` when the database was reopened belongs to a
        process that is gone. Whether its side effect landed is not knowable
        from here, so it becomes `unknown` -- never `failed`, which would invite
        an automatic retry of something that may already have happened.
        """

        with self._lock:
            self._db.execute("BEGIN IMMEDIATE")
            try:
                sql = "SELECT action_id FROM actions WHERE status = 'started'"
                args: list[Any] = []
                if session_id is not None:
                    sql += " AND session_id = ?"
                    args.append(session_id)
                ids = [row["action_id"] for row in self._db.execute(sql, args).fetchall()]
                if ids:
                    self._db.executemany(
                        "UPDATE actions SET status = 'unknown' WHERE action_id = ?",
                        [(i,) for i in ids],
                    )
                self._db.execute("COMMIT")
            except BaseException:
                self._db.execute("ROLLBACK")
                raise
        return ids

    def _create_indexes(self) -> None:
        self._db.execute(
            "CREATE INDEX IF NOT EXISTS messages_epoch "
            "ON messages (session_id, epoch, ordinal)"
        )
        self._db.execute(
            "CREATE INDEX IF NOT EXISTS actions_session "
            "ON actions (session_id, status)"
        )

    # -- sessions ----------------------------------------------------------
    def upsert_session(self, record: SessionRecord) -> None:
        with self._lock:
            self._db.execute(
                """
                INSERT INTO sessions
                    (session_id, workspace, system, created_at, run_count, status, todos, owner)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(session_id) DO UPDATE SET
                    workspace = excluded.workspace,
                    system    = excluded.system,
                    run_count = excluded.run_count,
                    status    = excluded.status,
                    todos     = excluded.todos,
                    owner     = excluded.owner
                """,
                (
                    record.session_id,
                    record.workspace,
                    record.system,
                    record.created_at,
                    record.run_count,
                    record.status,
                    json.dumps(_json_safe(list(record.todos)), ensure_ascii=False),
                    record.owner,
                ),
            )

    def load_sessions(self) -> list[SessionRecord]:
        with self._lock:
            rows = self._db.execute(
                "SELECT * FROM sessions ORDER BY created_at"
            ).fetchall()
            out = []
            for row in rows:
                out.append(
                    SessionRecord(
                        session_id=row["session_id"],
                        workspace=row["workspace"],
                        system=row["system"],
                        created_at=float(row["created_at"]),
                        run_count=int(row["run_count"]),
                        status=row["status"],
                        event_cursor=self._max_ordinal("events", row["session_id"]),
                        todos=tuple(json.loads(row["todos"] or "[]")),
                        owner=row["owner"] if "owner" in row.keys() else "anonymous",
                    )
                )
            return out

    # -- append contract ---------------------------------------------------
    def _max_ordinal(self, table: str, session_id: str) -> int:
        row = self._db.execute(
            f"SELECT COALESCE(MAX(ordinal), 0) AS n FROM {table} WHERE session_id = ?",
            (session_id,),
        ).fetchone()
        return int(row["n"])

    def _append(self, table: str, session_id: str, payloads: Iterable[Any]) -> int:
        """Append rows, numbering them from the head read in this transaction.

        Reading ``MAX(ordinal)`` and inserting inside one transaction is what
        makes a concurrent writer either serialize behind us or fail the unique
        constraint -- never interleave into a gap.
        """

        items = list(payloads)
        if not items:
            return self._max_ordinal(table, session_id)
        with self._lock:
            self._db.execute("BEGIN IMMEDIATE")
            try:
                start = self._max_ordinal(table, session_id)
                self._db.executemany(
                    f"INSERT INTO {table} (session_id, ordinal, payload) "
                    "VALUES (?, ?, ?)",
                    [
                        (
                            session_id,
                            start + offset + 1,
                            json.dumps(_json_safe(item), ensure_ascii=False),
                        )
                        for offset, item in enumerate(items)
                    ],
                )
                self._db.execute("COMMIT")
            except BaseException:
                self._db.execute("ROLLBACK")
                raise
            return start + len(items)

    def append_messages(
        self,
        session_id: str,
        messages: Sequence[Any],
        *,
        epoch: int = 1,
    ) -> int:
        """Append to `epoch`. Ordinals stay globally increasing per session, so
        the unique constraint keeps meaning what it meant."""

        items = list(messages)
        if not items:
            return self.message_count(session_id, epoch=epoch)
        with self._lock:
            self._db.execute("BEGIN IMMEDIATE")
            try:
                start = self._max_ordinal("messages", session_id)
                self._db.executemany(
                    "INSERT INTO messages (session_id, ordinal, epoch, payload) "
                    "VALUES (?, ?, ?, ?)",
                    [
                        (
                            session_id,
                            start + offset + 1,
                            epoch,
                            json.dumps(_json_safe(item), ensure_ascii=False),
                        )
                        for offset, item in enumerate(items)
                    ],
                )
                self._db.execute("COMMIT")
            except BaseException:
                self._db.execute("ROLLBACK")
                raise
        return self.message_count(session_id, epoch=epoch)

    def transcript_epoch(self, session_id: str) -> int:
        """Highest epoch written for this session (0 when nothing is stored)."""

        with self._lock:
            row = self._db.execute(
                "SELECT COALESCE(MAX(epoch), 0) AS n FROM messages WHERE session_id = ?",
                (session_id,),
            ).fetchone()
        return int(row["n"])

    def append_event(self, session_id: str, event: Mapping[str, Any]) -> int:
        return self._append("events", session_id, [event])

    # -- reads (always bounded) -------------------------------------------
    def load_messages(self, session_id: str, *, epoch: int | None = None) -> list[Any]:
        """Return one epoch's transcript -- the current one unless asked.

        Superseded epochs stay on disk: they are the record of what the agent
        actually saw before a compaction rewrote its history.
        """

        target = self.transcript_epoch(session_id) if epoch is None else epoch
        if target == 0:
            return []
        with self._lock:
            rows = self._db.execute(
                "SELECT payload FROM messages WHERE session_id = ? AND epoch = ? "
                "ORDER BY ordinal",
                (session_id, target),
            ).fetchall()
        return [json.loads(row["payload"]) for row in rows]

    def message_count(self, session_id: str, *, epoch: int | None = None) -> int:
        target = self.transcript_epoch(session_id) if epoch is None else epoch
        if target == 0:
            return 0
        with self._lock:
            row = self._db.execute(
                "SELECT COUNT(*) AS n FROM messages WHERE session_id = ? AND epoch = ?",
                (session_id, target),
            ).fetchone()
        return int(row["n"])

    def load_events(
        self,
        session_id: str,
        *,
        after: int = 0,
        limit: int | None = None,
    ) -> list[dict]:
        sql = (
            "SELECT payload FROM events WHERE session_id = ? AND ordinal > ? "
            "ORDER BY ordinal"
        )
        args: list[Any] = [session_id, after]
        if limit is not None:
            sql += " LIMIT ?"
            args.append(limit)
        with self._lock:
            rows = self._db.execute(sql, args).fetchall()
        return [json.loads(row["payload"]) for row in rows]

    def event_cursor(self, session_id: str) -> int:
        with self._lock:
            return self._max_ordinal("events", session_id)

    def delete_session(self, session_id: str) -> None:
        """Remove a deleted session's operational state, in one transaction.

        Without this there was no way to remove a session record, so
        `SessionManager.delete()` popped the session from memory while its
        `sessions` row survived -- and `restore_sessions()` on the next startup
        rebuilt the deleted session, re-creating its workspace and rehydrating
        its transcript. Dropping the `sessions` row stops that and frees the
        lease (which lives in that row); dropping `messages` and `events`
        reclaims the transcript a deleted session no longer needs.

        `actions` and `approvals` are deliberately kept: they are the durable
        audit and reconciliation trail (round 100 -- a session delete records
        each pending approval as `cancelled` rather than erasing it), the same
        outlive-the-session role trajectories play in their own store.
        """

        with self._lock:
            self._db.execute("BEGIN IMMEDIATE")
            try:
                for table in ("messages", "events", "sessions"):
                    self._db.execute(
                        f"DELETE FROM {table} WHERE session_id = ?", (session_id,)
                    )
                self._db.execute("COMMIT")
            except BaseException:
                self._db.execute("ROLLBACK")
                raise

    def close(self) -> None:
        with self._lock:
            self._db.close()
