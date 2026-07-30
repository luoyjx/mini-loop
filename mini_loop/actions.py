"""Process-local action journal used by the experimental workflow boundary.

The journal records the exact tool payload before its handler runs and binds it
to the stable ``ToolContext.action_id``.  It is intentionally an in-memory MVP
contract, not a claim of restart-safe exactly-once side effects.
"""

from __future__ import annotations

import copy
import hashlib
import json
import threading
import time
from dataclasses import dataclass, replace
from typing import Any, Mapping


class ActionJournalConflict(RuntimeError):
    """The same action ID was reused with different immutable input."""


def _payload_hash(value: Mapping[str, Any]) -> str:
    encoded = json.dumps(
        value,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
        allow_nan=False,
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


@dataclass(frozen=True, slots=True)
class ActionRecord:
    action_id: str
    session_id: str
    message_id: str
    tool_use_id: str
    tool_name: str
    input_hash: str
    status: str = "started"
    result: str | None = None
    workflow_run_id: str | None = None
    created_at: float = 0.0
    completed_at: float | None = None


class InMemoryActionJournal:
    """Thread-safe process-local action records with immutable input binding."""

    def __init__(self) -> None:
        self._lock = threading.RLock()
        self._records: dict[str, ActionRecord] = {}

    def begin(
        self,
        *,
        action_id: str,
        session_id: str,
        message_id: str,
        tool_use_id: str,
        tool_name: str,
        input_value: Mapping[str, Any],
    ) -> ActionRecord:
        if not action_id or not session_id or not message_id or not tool_name:
            raise ValueError(
                "action_id, session_id, message_id, and tool_name are required"
            )
        candidate = ActionRecord(
            action_id=action_id,
            session_id=session_id,
            message_id=message_id,
            tool_use_id=tool_use_id,
            tool_name=tool_name,
            input_hash=_payload_hash(input_value),
            created_at=time.time(),
        )
        with self._lock:
            existing = self._records.get(action_id)
            if existing is None:
                self._records[action_id] = candidate
                return copy.deepcopy(candidate)
            immutable = (
                "session_id",
                "message_id",
                "tool_use_id",
                "tool_name",
                "input_hash",
            )
            if any(getattr(existing, name) != getattr(candidate, name) for name in immutable):
                raise ActionJournalConflict(
                    f"action {action_id} was replayed with a different payload"
                )
            return copy.deepcopy(existing)

    def finish(
        self,
        action_id: str,
        *,
        status: str,
        result: str | None = None,
    ) -> ActionRecord:
        if status not in {"completed", "failed", "denied", "cancelled"}:
            raise ValueError(f"unsupported action status: {status}")
        with self._lock:
            try:
                existing = self._records[action_id]
            except KeyError as error:
                raise KeyError(f"action {action_id} was not started") from error
            if existing.status != "started":
                return copy.deepcopy(existing)
            updated = replace(
                existing,
                status=status,
                result=result[:4_000] if result is not None else None,
                completed_at=time.time(),
            )
            self._records[action_id] = updated
            return copy.deepcopy(updated)

    def attach_workflow(self, action_id: str, run_id: str) -> ActionRecord:
        with self._lock:
            try:
                existing = self._records[action_id]
            except KeyError as error:
                raise KeyError(f"action {action_id} was not started") from error
            if existing.workflow_run_id not in {None, run_id}:
                raise ActionJournalConflict(
                    f"action {action_id} is already bound to "
                    f"{existing.workflow_run_id}"
                )
            updated = replace(existing, workflow_run_id=run_id)
            self._records[action_id] = updated
            return copy.deepcopy(updated)

    def get(self, action_id: str) -> ActionRecord | None:
        with self._lock:
            record = self._records.get(action_id)
            return copy.deepcopy(record) if record is not None else None
