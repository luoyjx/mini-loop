"""Durable, local-first trajectory recording for agent runs.

Each user run is one trajectory (trace), while the session id is its group id.
Records are appended as JSON Lines so a process crash still leaves a readable
partial trajectory.  The public ``get`` representation is convenient for UI
and JSON export; ``raw`` preserves the append-only event stream for tooling.
"""

from __future__ import annotations

import json
import os
import re
import threading
import time
import uuid
from pathlib import Path

from .problems import ProblemLog

SCHEMA_VERSION = "mini-loop.trajectory.v1"
_ID_RE = re.compile(r"traj_[0-9a-f]{24}")
_CONTENT_FIELDS = {
    "content", "error", "input", "message", "model_input", "model_output", "output",
    "prompt", "summary", "system", "text",
}


def _json_safe(value):
    """Detach arbitrary provider objects into JSON-safe values."""
    return json.loads(json.dumps(value, default=str))


def _redacted(value):
    if isinstance(value, str):
        return f"[redacted: {len(value)} chars]"
    if value is None:
        return None
    try:
        size = len(value)
    except TypeError:
        size = 1
    return f"[redacted: {type(value).__name__}, {size} item(s)]"


def _protect_content(value, *, capture_content: bool, key: str | None = None):
    if not capture_content and key in _CONTENT_FIELDS:
        return _redacted(value)
    if isinstance(value, dict):
        return {
            str(child_key): _protect_content(
                child_value, capture_content=capture_content, key=str(child_key)
            )
            for child_key, child_value in value.items()
        }
    if isinstance(value, list):
        return [
            _protect_content(item, capture_content=capture_content)
            for item in value
        ]
    return value


class TrajectoryStore:
    """Append-only JSONL trajectory store safe for concurrent sessions."""

    def __init__(self, root: Path, *, capture_content: bool = True) -> None:
        self.root = Path(root).resolve()
        self.root.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.capture_content = capture_content
        self._state_lock = threading.RLock()
        self._file_locks = tuple(threading.RLock() for _ in range(32))
        self._active: set[str] = set()
        #: Trajectory files dropped from `list()` because they could not be
        #: read. Surfaced by the audit's problem-channel sweep -- a corrupt
        #: recording silently vanishing from the listing looks like one that
        #: was never made (round 81's pattern for stores).
        self.problems = ProblemLog()

    def _path(self, trajectory_id: str) -> Path:
        if not _ID_RE.fullmatch(trajectory_id):
            raise ValueError("invalid trajectory id")
        return self.root / f"{trajectory_id}.jsonl"

    def _file_lock(self, trajectory_id: str):
        return self._file_locks[hash(trajectory_id) % len(self._file_locks)]

    def _write(self, trajectory_id: str, record: dict) -> None:
        payload = _protect_content(
            _json_safe(record), capture_content=self.capture_content
        )
        with self._file_lock(trajectory_id):
            descriptor = os.open(
                self._path(trajectory_id),
                os.O_APPEND | os.O_CREAT | os.O_WRONLY,
                0o600,
            )
            with os.fdopen(descriptor, "a", encoding="utf-8") as handle:
                handle.write(
                    json.dumps(payload, ensure_ascii=False, separators=(",", ":")) + "\n"
                )
                handle.flush()

    def start(
        self,
        *,
        session_id: str,
        run_index: int,
        input_text: str,
        metadata: dict | None = None,
        owner: str = "anonymous",
    ) -> str:
        trajectory_id = f"traj_{uuid.uuid4().hex[:24]}"
        self._write(trajectory_id, {
            "record_type": "trajectory_start",
            "schema_version": SCHEMA_VERSION,
            "trajectory_id": trajectory_id,
            "trace_id": trajectory_id,
            "group_id": session_id,
            "session": session_id,
            # First-class rather than tucked into `metadata`: this is what the
            # API's access check reads. Round 74 resolved ownership from live
            # sessions, so a restart left a trajectory unreadable by the person
            # who made it -- fail-closed, which is right for a check and wrong
            # for the owner. On disk it survives the process.
            "owner": owner,
            "run_index": run_index,
            "started_at": time.time(),
            "input": input_text,
            "metadata": metadata or {},
        })
        with self._state_lock:
            self._active.add(trajectory_id)
        return trajectory_id

    def append(self, trajectory_id: str, event: dict) -> None:
        self._write(trajectory_id, {**event, "record_type": "event"})

    def finish(
        self,
        trajectory_id: str,
        *,
        status: str,
        output: str | None = None,
        error: str | None = None,
        duration_ms: float | None = None,
    ) -> None:
        trajectory = self.get(trajectory_id)
        metrics = self._metrics(trajectory["events"])
        try:
            self._write(trajectory_id, {
                "record_type": "trajectory_end",
                "trajectory_id": trajectory_id,
                "trace_id": trajectory_id,
                "group_id": trajectory["group_id"],
                "session": trajectory["session"],
                "owner": trajectory.get("owner", "anonymous"),
                "status": status,
                "ended_at": time.time(),
                "duration_ms": round(duration_ms, 3) if duration_ms is not None else None,
                "output": output,
                "error": error,
                "metrics": metrics,
            })
        finally:
            with self._state_lock:
                self._active.discard(trajectory_id)

    def _records(self, trajectory_id: str) -> tuple[list[dict], bool]:
        path = self._path(trajectory_id)
        if not path.is_file():
            raise KeyError(trajectory_id)
        records, partial = [], False
        with self._file_lock(trajectory_id), path.open(encoding="utf-8") as handle:
            for line in handle:
                try:
                    records.append(json.loads(line))
                except json.JSONDecodeError:
                    partial = True
        if not records or records[0].get("record_type") != "trajectory_start":
            raise ValueError(f"trajectory {trajectory_id} has no valid header")
        return records, partial

    @staticmethod
    def _metrics(events: list[dict]) -> dict:
        tool_results = [event for event in events if event.get("type") == "tool_result"]
        return {
            "event_count": len(events),
            "model_calls": sum(event.get("type") == "model_start" for event in events),
            "tool_calls": sum(event.get("type") == "tool_use" for event in events),
            "tool_errors": sum(
                bool(event.get("error") or event.get("denied")) for event in tool_results
            ),
            "errors": sum(event.get("type") == "error" for event in events),
        }

    def get(self, trajectory_id: str) -> dict:
        records, partial = self._records(trajectory_id)
        start = records[0]
        events = [record for record in records if record.get("record_type") == "event"]
        end = next(
            (record for record in reversed(records) if record.get("record_type") == "trajectory_end"),
            None,
        )
        with self._state_lock:
            active = trajectory_id in self._active
        if end:
            status = end.get("status", "completed")
        else:
            status = "running" if active else "interrupted"
        return {
            "schema_version": start.get("schema_version", SCHEMA_VERSION),
            "id": trajectory_id,
            "trajectory_id": trajectory_id,
            "trace_id": start.get("trace_id", trajectory_id),
            "group_id": start.get("group_id", start.get("session")),
            "session": start.get("session"),
            "owner": start.get("owner", "anonymous"),
            "run_index": start.get("run_index"),
            "status": status,
            "started_at": start.get("started_at"),
            "ended_at": end.get("ended_at") if end else None,
            "duration_ms": end.get("duration_ms") if end else None,
            "input": start.get("input"),
            "output": end.get("output") if end else None,
            "error": end.get("error") if end else None,
            "metadata": start.get("metadata", {}),
            "metrics": end.get("metrics", self._metrics(events)) if end else self._metrics(events),
            "events": events,
            "partial": partial or end is None,
        }

    def summary(self, trajectory_id: str) -> dict:
        """A listing row: header fields, terminal metrics, and a preview.

        Streams the file rather than calling `get()`, which materialises every
        event body into a list. A summary needs none of those bodies -- only the
        header (first record) and the terminal metrics (stored in the end record
        at `finish`, or counted while streaming when the run is still open). The
        old path read the whole trajectory to discard all of it, so `list()`
        (and `count()`, called on every session construction) cost scaled with
        recorded *content*, not trajectory count -- and one oversized trajectory
        owned by anyone loaded its whole body into memory before the server
        could filter the listing by caller, amplifying one tenant's recording
        into everyone's listing cost. Streaming keeps one line resident at a
        time; the bound is the size of a single record, not the file.
        """
        start, end, partial, counts = self._scan_summary(trajectory_id)
        with self._state_lock:
            active = trajectory_id in self._active
        if end is not None:
            status = end.get("status", "completed")
            metrics = end.get("metrics", counts)
            ended_at = end.get("ended_at")
            duration_ms = end.get("duration_ms")
        else:
            status = "running" if active else "interrupted"
            metrics = counts
            ended_at = None
            duration_ms = None
        input_text = start.get("input")
        if isinstance(input_text, str) and len(input_text) > 160:
            input_text = input_text[:159] + "…"
        return {
            "id": trajectory_id,
            "trajectory_id": trajectory_id,
            "trace_id": start.get("trace_id", trajectory_id),
            "group_id": start.get("group_id", start.get("session")),
            "session": start.get("session"),
            "owner": start.get("owner", "anonymous"),
            "run_index": start.get("run_index"),
            "status": status,
            "started_at": start.get("started_at"),
            "ended_at": ended_at,
            "duration_ms": duration_ms,
            "metrics": metrics,
            "partial": partial or end is None,
            "input_preview": input_text,
            "model": (start.get("metadata") or {}).get("model"),
            # The workspace the run was recorded in, so a reader profiling
            # shell usage can tell "cd back into my own workspace" (cwd
            # distrust) from "cd somewhere else" (the work lives elsewhere).
            "workspace": (start.get("metadata") or {}).get("workspace"),
        }

    def _scan_summary(
        self, trajectory_id: str
    ) -> tuple[dict, dict | None, bool, dict]:
        """One streaming pass: header, last terminal record, counts, partial.

        Never holds more than the current line plus the header and the end
        record -- the same result `get()` produces from a full in-memory
        `records` list, without paying for the event bodies. The counts match
        `_metrics` exactly so a still-open trajectory summarises identically.
        """
        path = self._path(trajectory_id)
        if not path.is_file():
            raise KeyError(trajectory_id)
        start: dict | None = None
        end: dict | None = None
        partial = False
        counts = {
            "event_count": 0, "model_calls": 0, "tool_calls": 0,
            "tool_errors": 0, "errors": 0,
        }
        with self._file_lock(trajectory_id), path.open(encoding="utf-8") as handle:
            for line in handle:
                try:
                    record = json.loads(line)
                except json.JSONDecodeError:
                    partial = True
                    continue
                if start is None:
                    if record.get("record_type") != "trajectory_start":
                        raise ValueError(
                            f"trajectory {trajectory_id} has no valid header"
                        )
                    start = record
                    continue
                record_type = record.get("record_type")
                if record_type == "trajectory_end":
                    end = record
                elif record_type == "event":
                    counts["event_count"] += 1
                    event_type = record.get("type")
                    if event_type == "model_start":
                        counts["model_calls"] += 1
                    elif event_type == "tool_use":
                        counts["tool_calls"] += 1
                    elif event_type == "error":
                        counts["errors"] += 1
                    elif event_type == "tool_result" and (
                        record.get("error") or record.get("denied")
                    ):
                        counts["tool_errors"] += 1
        if start is None:
            raise ValueError(f"trajectory {trajectory_id} has no valid header")
        return start, end, partial, counts

    def list(self, *, session_id: str | None = None, limit: int = 100) -> list[dict]:
        summaries = []
        for path in self.root.glob("traj_*.jsonl"):
            try:
                summary = self.summary(path.stem)
            except KeyError:
                # Deleted between the glob and the read -- a benign race, not a
                # corrupt file, so nothing to report.
                continue
            except (ValueError, OSError) as error:
                # A recording that cannot be read is dropped from the listing;
                # saying nothing makes it look like one that was never made.
                # Reported, not hidden -- the same call round 81 made for the
                # memory and task stores and cron's durable load.
                self.problems.append(
                    f"{path.name}: unreadable ({type(error).__name__}); dropped "
                    "from the trajectory listing"
                )
                continue
            if session_id is None or summary["session"] == session_id:
                summaries.append(summary)
        summaries.sort(key=lambda item: item.get("started_at") or 0, reverse=True)
        return summaries[:max(0, limit)]

    def count(self, session_id: str) -> int:
        return len(self.list(session_id=session_id, limit=1_000_000))

    def delete_for_session(self, session_id: str) -> int:
        """Remove every recording whose header names this session; the count.

        Session deletion reclaims the workspace, the cron jobs, the durable
        row -- and left these files forever: unreadable through the API once
        ownership eviction hit, held on disk regardless (roadmap G10's "can
        it be safely deleted"). The caller runs this after the session's turn
        is dead, or a still-winding-down capture recreates the file it was
        appending to. A file whose header cannot be read is left in place and
        reported: deletion is destructive, and "cannot prove it is this
        session's" falls toward keeping bytes, never toward removing what
        might be someone else's record.
        """

        removed = 0
        for path in self.root.glob("traj_*.jsonl"):
            trajectory_id = path.stem
            try:
                with path.open("r", encoding="utf-8") as handle:
                    header = json.loads(handle.readline() or "null")
            except (OSError, json.JSONDecodeError):
                self.problems.append(
                    f"{path.name}: header unreadable; left in place rather "
                    "than deleted on a guess"
                )
                continue
            if not isinstance(header, dict) or header.get("session") != session_id:
                continue
            with self._file_lock(trajectory_id):
                try:
                    path.unlink()
                except FileNotFoundError:
                    continue
                except OSError as error:
                    self.problems.append(
                        f"{path.name}: deletion failed ({type(error).__name__}); "
                        "the recording outlives its deleted session"
                    )
                    continue
            with self._state_lock:
                self._active.discard(trajectory_id)
            removed += 1
        return removed

    def iter_events(
        self,
        trajectory_id: str,
        *,
        types: set[str] | None = None,
        limit: int = 1_000,
    ):
        """Stream parsed event records, optionally filtered, always bounded.

        `get()` materialises every event body; readers that only need a few
        fields from a few event types (the skill-usage feedback in
        self_audit, round 246) get a generator instead. `limit` counts
        *yielded* records, so a filtered scan still terminates on a long
        file: bounded output must be bounded work.
        """

        path = self._path(trajectory_id)
        yielded = 0
        try:
            with path.open("r", encoding="utf-8") as handle:
                for line in handle:
                    if yielded >= limit:
                        return
                    try:
                        record = json.loads(line)
                    except json.JSONDecodeError:
                        continue
                    if types is not None and record.get("type") not in types:
                        continue
                    yielded += 1
                    yield record
        except OSError:
            return

    def raw(self, trajectory_id: str) -> str:
        path = self._path(trajectory_id)
        if not path.is_file():
            raise KeyError(trajectory_id)
        with self._file_lock(trajectory_id):
            return path.read_text(encoding="utf-8")

    def byte_size(self, trajectory_id: str) -> int:
        """The trajectory file's size on disk, without reading it."""

        path = self._path(trajectory_id)
        if not path.is_file():
            raise KeyError(trajectory_id)
        return path.stat().st_size

    def stream_raw(self, trajectory_id: str, *, chunk_size: int = 65_536):
        """Yield the trajectory file in chunks rather than loading it whole.

        `raw()` read the entire file with `read_text()`, but a long run stores
        the full model input at *every* model call, so a trajectory grows to
        tens of MB, and an export loaded all of it into memory -- the process,
        every tenant on it with it. Streaming bounds an export to one chunk,
        however large the file. No file lock is held across the (client-paced)
        stream: appends are atomic `O_APPEND` writes, so a reader sees a prefix
        of committed bytes -- a snapshot -- and holding the lock would block the
        trajectory's own writes for as long as a slow client takes to download.
        """

        path = self._path(trajectory_id)
        if not path.is_file():
            raise KeyError(trajectory_id)
        with path.open("r", encoding="utf-8") as handle:
            while True:
                chunk = handle.read(chunk_size)
                if not chunk:
                    break
                yield chunk

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: recording failures degrade to a reported error field by contract; the field is the diagnostic."
)
