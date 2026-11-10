"""Low-cardinality, best-effort Task process metrics.

This module deliberately has no Temporal import so the HTTP adapter remains
importable without the optional Task runtime. Metric failures never rewrite an
API, Workflow, or model-provider outcome.
"""

from __future__ import annotations

from threading import Lock
from typing import Any


class TaskMetrics:
    """Small process-local facade over a Temporal SDK metric meter."""

    def __init__(self, meter: Any = None) -> None:
        self._meter = meter
        self._lock = Lock()
        self._activity_inflight = 0
        self._instruments: dict[str, Any] = {}
        if meter is not None:
            self._attach(meter)

    def _attach(self, meter: Any) -> None:
        definitions = {
            "process_up": ("gauge", "mini_loop_task_process_up", None),
            "api_temporal_ready": ("gauge", "mini_loop_task_api_temporal_ready", None),
            "worker_polling": ("gauge", "mini_loop_task_worker_polling", None),
            "worker_draining": ("gauge", "mini_loop_task_worker_draining", None),
            "activity_inflight": ("gauge", "mini_loop_task_model_activity_inflight", None),
            "api_requests": ("counter", "mini_loop_task_api_requests", None),
            "submissions": ("counter", "mini_loop_task_submissions", None),
            "status_observations": ("counter", "mini_loop_task_status_observations", None),
            "cancel_requests": ("counter", "mini_loop_task_cancel_requests", None),
            "activity_attempts": ("counter", "mini_loop_task_model_activity_attempts", None),
            "activity_queue_latency": ("histogram", "mini_loop_task_model_activity_queue_latency", "ms"),
        }
        try:
            for key, (kind, name, unit) in definitions.items():
                create = getattr(meter, f"create_{kind}")
                self._instruments[key] = create(name, unit=unit)
        except Exception:
            self._instruments.clear()

    def _set(self, key: str, value: int, attributes: dict[str, str] | None = None) -> None:
        try:
            self._instruments[key].set(value, attributes)
        except Exception:
            return

    def _add(self, key: str, attributes: dict[str, str]) -> None:
        try:
            self._instruments[key].add(1, attributes)
        except Exception:
            return

    def _record(self, key: str, value: int, attributes: dict[str, str]) -> None:
        try:
            self._instruments[key].record(value, attributes)
        except Exception:
            return

    def process_up(self, available: bool) -> None:
        self._set("process_up", int(available))

    def api_ready(self, available: bool) -> None:
        self._set("api_temporal_ready", int(available))

    def worker_state(self, *, polling: bool, draining: bool) -> None:
        self._set("worker_polling", int(polling))
        self._set("worker_draining", int(draining))

    def api_request(self, operation: str, result: str) -> None:
        self._add("api_requests", {"operation": operation, "result": result})

    def submission(self, outcome: str) -> None:
        self._add("submissions", {"outcome": outcome})

    def status(self, status: str) -> None:
        self._add("status_observations", {"status": status})

    def cancel(self, outcome: str) -> None:
        self._add("cancel_requests", {"outcome": outcome})

    def activity_started(self, *, attempt: int, queue_latency_ms: int | None) -> None:
        self._add("activity_attempts", {"outcome": "started", "retry": str(attempt > 1).lower()})
        if queue_latency_ms is not None:
            self._record("activity_queue_latency", max(0, queue_latency_ms), {})
        with self._lock:
            self._activity_inflight += 1
            current = self._activity_inflight
        self._set("activity_inflight", current)

    def activity_finished(self, outcome: str, *, attempt: int) -> None:
        self._add("activity_attempts", {"outcome": outcome, "retry": str(attempt > 1).lower()})
        with self._lock:
            self._activity_inflight = max(0, self._activity_inflight - 1)
            current = self._activity_inflight
        self._set("activity_inflight", current)


NOOP_TASK_METRICS = TaskMetrics()
