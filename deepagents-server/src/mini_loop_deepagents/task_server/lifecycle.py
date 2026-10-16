"""Signals and a hard deadline owned only by a dedicated Worker process.

The SDK's Activity grace period is not a process exit deadline. The watchdog
deliberately bypasses asyncio cleanup if cooperative shutdown cannot finish.
Library callers use ``serve`` directly and retain ownership of their process.
"""

from __future__ import annotations

import asyncio
import math
import os
import signal
import threading
from types import FrameType
from typing import Any

HARD_EXIT_CODE = 70


def validate_shutdown_settings(grace: float, timeout: float) -> tuple[float, float]:
    """Validate independent SDK Activity grace and total process stop budgets."""
    if type(grace) not in (int, float) or not math.isfinite(grace) or not 0 <= grace <= 300:
        raise ValueError("shutdown grace must be finite and between 0 and 300 seconds")
    if (type(timeout) not in (int, float) or not math.isfinite(timeout)
            or not 0 < timeout <= 360 or timeout <= grace):
        raise ValueError("shutdown timeout must be finite, greater than grace, and at most 360 seconds")
    return float(grace), float(timeout)


def lifecycle_log(message: str) -> None:
    """Only fixed lifecycle labels enter process logs, never task data."""
    try:
        os.write(2, (message + "\n").encode("ascii"))
    except OSError:
        # Logging is observational; a closed collector must not abort shutdown.
        pass


class ProcessLifecycle:
    """Own signal handlers and a daemon watchdog around a CLI's asyncio run.

    Install before starting the event loop so a signal during connection or SDK
    validation cannot be forgotten. Neither this class nor its hard exit is
    constructed by the async library entry point.
    """

    def __init__(self, timeout: float):
        self.timeout = timeout
        self._lock = threading.RLock()
        self._requested = False
        self._closed = False
        self._timer: threading.Timer | None = None
        self._loop: asyncio.AbstractEventLoop | None = None
        self._stop_event: asyncio.Event | None = None
        self._previous: dict[signal.Signals, Any] = {}

    def __enter__(self) -> ProcessLifecycle:
        if threading.current_thread() is not threading.main_thread():
            raise RuntimeError("Worker process entry point requires the main thread")
        try:
            for signum in (signal.SIGTERM, signal.SIGINT):
                self._previous[signum] = signal.signal(signum, self._handle_signal)
        except BaseException:
            self.close()
            raise
        return self

    def bind(self, loop: asyncio.AbstractEventLoop, stop_event: asyncio.Event) -> None:
        with self._lock:
            self._loop, self._stop_event = loop, stop_event
            if self._requested:
                stop_event.set()

    def _handle_signal(self, signum: int, frame: FrameType | None) -> None:
        with self._lock:
            if self._closed:
                return
            if self._requested:
                self._force_exit("second_signal")
                return
            self._requested = True
            self._timer = threading.Timer(self.timeout, self._force_exit, args=("deadline",))
            self._timer.daemon = True
            self._timer.start()
            # Python invokes these handlers on the main thread, which owns this
            # event loop. Set immediately: scheduling set() for a later turn can
            # let readiness slip out after a signal received during startup.
            if self._stop_event is not None:
                try:
                    self._stop_event.set()
                except RuntimeError:
                    # A signal can arrive while Runner is closing the loop.
                    # The requested flag remains set and the watchdog is armed.
                    pass
                if self._loop is not None:
                    try:
                        # Event.set() queues callbacks but does not wake an idle
                        # selector. PEP 475 can restart Linux epoll after this
                        # handler, so also write the loop's thread-safe self-pipe.
                        self._loop.call_soon_threadsafe(self._stop_event.set)
                    except RuntimeError:
                        # The bound loop may already have closed during cleanup.
                        pass
        # A full stderr pipe may block this main thread. Do not hold the state
        # lock: the watchdog must still enter its nonblocking hard-exit path.
        lifecycle_log("TASK_WORKER_STOP_REQUESTED signal=" + signal.Signals(signum).name)

    def _force_exit(self, reason: str) -> None:
        with self._lock:
            if not self._closed:
                # stderr backpressure must not defeat the hard deadline. This
                # descriptor change is confined to the imminent process exit.
                try:
                    os.set_blocking(2, False)
                    lifecycle_log("TASK_WORKER_HARD_EXIT reason=" + reason)
                except OSError:
                    pass
                finally:
                    os._exit(HARD_EXIT_CODE)

    def close(self) -> None:
        with self._lock:
            self._closed = True
            timer, self._timer = self._timer, None
            if timer is not None:
                timer.cancel()
            self._loop = self._stop_event = None
            for signum, previous in self._previous.items():
                signal.signal(signum, previous)
            self._previous.clear()
        if timer is not None and timer is not threading.current_thread():
            timer.join(timeout=0.2)

    def __exit__(self, *exc: Any) -> None:
        self.close()
