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
from collections import deque
import time
from .problems import ProblemLog
from dataclasses import asdict, dataclass, replace
from typing import Any, Mapping


MAX_ACTION_RESULT_CHARS = 4_000


def _bounded_result(result: str | None) -> str | None:
    """Retain a replay-safe prefix that explicitly reports truncation."""

    if result is None or len(result) <= MAX_ACTION_RESULT_CHARS:
        return result
    marker = f"\n[action result truncated; original_chars={len(result)}]"
    keep = MAX_ACTION_RESULT_CHARS - len(marker)
    return f"{result[:max(0, keep)]}{marker}"[:MAX_ACTION_RESULT_CHARS]


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


#: Terminal records that keep their `result` text. Past this, the oldest give up
#: the payload and keep everything else.
#:
#: Each result is already capped at 4,000 characters, so the *per record* size
#: was bounded and the *count* was not. Measured on a long-lived session doing
#: ordinary tool work:
#:
#:     20,000 completed actions -> 81.0 MB of result text, never released
#:
#: Found by running a session for ninety turns and watching what grew, rather
#: than by reading: `messages` plateaus at 51 under compaction and every other
#: structure with it, so this was the one line still climbing.
MAX_RESULTS_RETAINED = 512

#: Records held before the journal starts saying so. Deliberately not a cap.
#: Dropping a record is not like trimming a log: a replay of an evicted action
#: reads as "never started" and runs the side effect a second time, which is the
#: exact failure this journal exists to prevent. Shedding results reclaims
#: essentially all of the memory while leaving every action answerable, so the
#: residual growth is reported rather than silently truncated.
REPORT_RECORDS_ABOVE = 50_000


class InMemoryActionJournal:
    """Thread-safe process-local action records with immutable input binding."""

    def __init__(
        self,
        *,
        max_results_retained: int = MAX_RESULTS_RETAINED,
    ) -> None:
        self._lock = threading.RLock()
        self._records: dict[str, ActionRecord] = {}
        self.max_results_retained = max_results_retained
        #: Terminal action ids in completion order, oldest first. Only these are
        #: candidates for shedding: an action still `started` has an outcome
        #: nobody has recorded yet.
        self._completed: deque[str] = deque()
        #: Results dropped to stay within the bound. Reported, not hidden.
        self.problems = ProblemLog()

    def _shed_old_results(self) -> None:
        """Release payloads beyond the retention bound, keeping the records."""

        dropped = 0
        while len(self._completed) > self.max_results_retained:
            action_id = self._completed.popleft()
            record = self._records.get(action_id)
            if record is None or record.result is None:
                continue
            self._records[action_id] = replace(record, result=SHED_RESULT)
            dropped += 1
        if dropped:
            self.problems.append(
                f"released {dropped} action result(s) beyond the newest "
                f"{self.max_results_retained}; status and identity are kept"
            )
        if len(self._records) > REPORT_RECORDS_ABOVE:
            self.problems.append(
                f"action journal holds {len(self._records):,} records; it is not "
                "evicted because a replayed action that reads as absent runs twice"
            )

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
        if status not in {"completed", "failed", "denied", "cancelled", "unknown"}:
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
                result=_bounded_result(result),
                completed_at=time.time(),
            )
            self._records[action_id] = updated
            if status in TERMINAL_STATUSES or status == UNKNOWN:
                self._completed.append(action_id)
                self._shed_old_results()
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


# --- durability -------------------------------------------------------------

TERMINAL_STATUSES = frozenset({"completed", "failed", "denied", "cancelled"})
#: Dispatched, never accounted for. Replaying it must not re-run the side effect.
UNKNOWN = "unknown"

#: Stands in for a result released to stay within the retention bound. The
#: action is still recorded as having completed -- only the text is gone -- so
#: a replay still refuses to re-run it.
SHED_RESULT = "[result released; the action completed and was not re-run]"
RECONCILED_RESULT = (
    "[reconciled] This tool was dispatched before the process terminated, and a "
    "check confirms it already took effect. It has not been run again."
)
UNKNOWN_RESULT = (
    "[unknown] This tool was dispatched but the process terminated before its "
    "result was recorded. Whether it completed is not known. Do not retry it; "
    "check whether it already took effect first."
)
#: The other kind of missing result, and the opposite advice. A call that was
#: parked on an approval when the process died never reached its handler --
#: no side effect happened, so retrying is safe. Round 96 learned that two
#: different absences encoded as one value invite every consumer to pick the
#: wrong one; UNKNOWN's "do not retry" is exactly wrong for this case.
NOT_RUN_RESULT = (
    "[not run] This tool was awaiting approval when the process terminated. "
    "It was never executed and had no side effect. Ask again if it is still "
    "needed."
)


class DurableActionJournal:
    """An action journal backed by the state store.

    Two properties matter, and both come from durable-execution engines
    (Temporal, Restate, Azure Durable Task) rather than from any agent harness
    -- most harnesses journal actions for audit and re-run them anyway:

    * **A journalled step is not re-executed.** ``begin`` returns the existing
      record, and a caller that sees a terminal status must return the recorded
      result instead of calling the tool again. The stable ``action_id`` derived
      from (session, message, tool_use id, tool name) is the idempotency key.
    * **In-flight is not failed.** An action still ``started`` when a new
      process opens the database belongs to a process that is gone; it becomes
      ``unknown``. Marking it ``failed`` would invite a retry of a side effect
      that may already have landed.
    """

    def __init__(self, store) -> None:
        self.store = store
        self._lock = threading.RLock()

    @staticmethod
    def _record(row: Mapping[str, Any]) -> ActionRecord:
        return ActionRecord(**{k: row[k] for k in ActionRecord.__dataclass_fields__})

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
        input_hash = _payload_hash(input_value)
        with self._lock:
            existing = self.store.read_action(action_id)
            if existing is not None:
                for name, value in (
                    ("session_id", session_id),
                    ("message_id", message_id),
                    ("tool_use_id", tool_use_id),
                    ("tool_name", tool_name),
                    ("input_hash", input_hash),
                ):
                    if existing[name] != value:
                        raise ActionJournalConflict(
                            f"action {action_id} was replayed with a different payload"
                        )
                return self._record(existing)
            record = ActionRecord(
                action_id=action_id,
                session_id=session_id,
                message_id=message_id,
                tool_use_id=tool_use_id,
                tool_name=tool_name,
                input_hash=input_hash,
                created_at=time.time(),
            )
            self.store.write_action(asdict(record))
            return record

    def finish(
        self,
        action_id: str,
        *,
        status: str,
        result: str | None = None,
    ) -> ActionRecord:
        if status not in TERMINAL_STATUSES | {UNKNOWN}:
            raise ValueError(f"unsupported action status: {status}")
        with self._lock:
            existing = self.store.read_action(action_id)
            if existing is None:
                raise KeyError(f"action {action_id} was not started")
            if existing["status"] != "started":
                return self._record(existing)
            updated = {
                **existing,
                "status": status,
                "result": _bounded_result(result),
                "completed_at": time.time(),
            }
            self.store.write_action(updated)
            return self._record(updated)

    def reconcile(self, action_id: str, *, status: str, result: str) -> ActionRecord:
        """Resolve an `unknown` action from external evidence.

        Deliberately not `finish()`: that only moves a record out of `started`,
        which is the guard against a second settlement, and loosening it would
        make every terminal record rewritable. Reconciliation is the one
        transition permitted out of `unknown`, and only out of `unknown`.
        """

        if status not in TERMINAL_STATUSES:
            raise ValueError(f"cannot reconcile to {status!r}")
        with self._lock:
            existing = self.store.read_action(action_id)
            if existing is None:
                raise KeyError(f"action {action_id} was not started")
            if existing["status"] != UNKNOWN:
                return self._record(existing)
            updated = {
                **existing,
                "status": status,
                "result": _bounded_result(result),
                "completed_at": time.time(),
            }
            self.store.write_action(updated)
            return self._record(updated)

    def attach_workflow(self, action_id: str, run_id: str) -> ActionRecord:
        with self._lock:
            existing = self.store.read_action(action_id)
            if existing is None:
                raise KeyError(f"action {action_id} was not started")
            if existing["workflow_run_id"] not in (None, run_id):
                raise ActionJournalConflict(
                    f"action {action_id} is already bound to a different workflow run"
                )
            updated = {**existing, "workflow_run_id": run_id}
            self.store.write_action(updated)
            return self._record(updated)

    def get(self, action_id: str) -> ActionRecord | None:
        row = self.store.read_action(action_id)
        return self._record(row) if row is not None else None

    def mark_inflight_unknown(self, session_id: str | None = None) -> list[str]:
        return self.store.mark_inflight_unknown(session_id)
