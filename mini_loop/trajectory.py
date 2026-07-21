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
    ) -> str:
        trajectory_id = f"traj_{uuid.uuid4().hex[:24]}"
        self._write(trajectory_id, {
            "record_type": "trajectory_start",
            "schema_version": SCHEMA_VERSION,
            "trajectory_id": trajectory_id,
            "trace_id": trajectory_id,
            "group_id": session_id,
            "session": session_id,
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
        trajectory = self.get(trajectory_id)
        input_text = trajectory.get("input")
        if isinstance(input_text, str) and len(input_text) > 160:
            input_text = input_text[:159] + "…"
        return {
            key: trajectory[key]
            for key in (
                "id", "trajectory_id", "trace_id", "group_id", "session", "run_index",
                "status", "started_at", "ended_at", "duration_ms", "metrics", "partial",
            )
        } | {
            "input_preview": input_text,
            "model": trajectory.get("metadata", {}).get("model"),
        }

    def list(self, *, session_id: str | None = None, limit: int = 100) -> list[dict]:
        summaries = []
        for path in self.root.glob("traj_*.jsonl"):
            try:
                summary = self.summary(path.stem)
            except (KeyError, ValueError, OSError):
                continue
            if session_id is None or summary["session"] == session_id:
                summaries.append(summary)
        summaries.sort(key=lambda item: item.get("started_at") or 0, reverse=True)
        return summaries[:max(0, limit)]

    def count(self, session_id: str) -> int:
        return len(self.list(session_id=session_id, limit=1_000_000))

    def raw(self, trajectory_id: str) -> str:
        path = self._path(trajectory_id)
        if not path.is_file():
            raise KeyError(trajectory_id)
        with self._file_lock(trajectory_id):
            return path.read_text(encoding="utf-8")
