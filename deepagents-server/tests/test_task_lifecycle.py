"""Process and async stop contracts without starting a Temporal service."""

import asyncio
import os
import select
import signal
import subprocess
import sys
import textwrap
from datetime import timedelta

import pytest

from mini_loop_deepagents.task_server import lifecycle, worker


# Standalone stdlib-only program also executed against the rebuilt Linux image.
# The selector handshake fixes timeout=None before announcing IDLE, so a signal
# arriving just before the syscall still requires a self-pipe wakeup. No sleep or
# periodic asyncio timer can accidentally hide the idle-selector regression.
IDLE_SIGNAL_PROBE = """
import asyncio
import os
from mini_loop_deepagents.task_server.lifecycle import ProcessLifecycle

async def main(control):
    loop = asyncio.get_running_loop()
    stop = asyncio.Event()
    control.bind(loop, stop)
    command = loop.create_future()
    def on_input():
        value = os.read(0, 1)
        loop.remove_reader(0)
        if value != b'I':
            command.set_exception(RuntimeError('probe_command_invalid'))
        else:
            command.set_result(None)
    loop.add_reader(0, on_input)
    print('READY', flush=True)
    await command
    original_select = loop._selector.select
    def observed_select(timeout=None):
        if timeout is None:
            print('IDLE', flush=True)
        return original_select(timeout)
    loop._selector.select = observed_select
    try:
        await stop.wait()
    finally:
        loop._selector.select = original_select
    print('DRAINED', flush=True)

with ProcessLifecycle(1.0) as control:
    asyncio.run(main(control))
print('CLEAN', flush=True)
"""


@pytest.mark.parametrize("grace,timeout", [
    (-1, 15), (301, 350), (True, 15), ("2", 15), (float("nan"), 15),
    (float("inf"), 15), (2, 0), (2, 2), (2, 1), (2, 361),
    (2, float("nan")), (2, float("inf")), (2, True),
])
def test_shutdown_budgets_reject_unbounded_or_ambiguous_values(grace, timeout):
    with pytest.raises(ValueError):
        lifecycle.validate_shutdown_settings(grace, timeout)


def test_shutdown_budgets_keep_grace_separate_from_total_deadline():
    assert lifecycle.validate_shutdown_settings(0, 0.1) == (0.0, 0.1)
    assert lifecycle.validate_shutdown_settings(300, 360) == (300.0, 360.0)


class FakeTimer:
    def __init__(self, interval, function, args=()):
        self.interval, self.function, self.args = interval, function, args
        self.daemon = False
        self.started = self.cancelled = self.joined = False

    def start(self):
        self.started = True

    def cancel(self):
        self.cancelled = True

    def join(self, timeout):
        self.joined = True

    def fire(self):
        self.function(*self.args)


@pytest.fixture
def process_environment(monkeypatch):
    originals = {signal.SIGTERM: object(), signal.SIGINT: object()}
    handlers = dict(originals)
    exits, logs, timers = [], [], []

    def install(signum, handler):
        previous, handlers[signum] = handlers[signum], handler
        return previous

    def timer(*args, **kwargs):
        result = FakeTimer(*args, **kwargs)
        timers.append(result)
        return result

    monkeypatch.setattr(lifecycle.signal, "signal", install)
    monkeypatch.setattr(lifecycle.threading, "Timer", timer)
    monkeypatch.setattr(lifecycle.os, "_exit", exits.append)
    monkeypatch.setattr(lifecycle.os, "set_blocking", lambda *args: None)
    monkeypatch.setattr(lifecycle, "lifecycle_log", logs.append)
    return originals, handlers, exits, logs, timers


@pytest.mark.parametrize("second_signal", [False, True])
def test_process_signal_before_loop_is_retained_and_forces_only_at_boundary(process_environment, second_signal):
    originals, handlers, exits, logs, timers = process_environment
    with lifecycle.ProcessLifecycle(15) as control:
        handlers[signal.SIGTERM](signal.SIGTERM, None)
        stop = asyncio.Event()
        control.bind(None, stop)
        assert stop.is_set()
        assert not exits
        assert timers[0].interval == 15 and timers[0].started and timers[0].daemon
        if second_signal:
            handlers[signal.SIGINT](signal.SIGINT, None)
        else:
            timers[0].fire()
        assert exits == [lifecycle.HARD_EXIT_CODE]
        assert logs[-1] == "TASK_WORKER_HARD_EXIT reason=" + ("second_signal" if second_signal else "deadline")
    assert handlers == originals
    assert timers[0].cancelled and timers[0].joined
    timers[0].fire()
    assert exits == [lifecycle.HARD_EXIT_CODE]


def test_process_fatal_exception_restores_handlers_and_cancels_watchdog(process_environment, monkeypatch):
    originals, handlers, exits, logs, timers = process_environment
    failure = RuntimeError("fatal startup")

    async def fail(*args, **kwargs):
        handlers[signal.SIGTERM](signal.SIGTERM, None)
        raise failure

    monkeypatch.setattr(worker, "serve", fail)
    with pytest.raises(RuntimeError) as raised:
        worker.run_process("unused", "queue", "identity", model_provider=lambda _: None)
    assert raised.value is failure
    assert handlers == originals and not exits
    assert timers[0].cancelled and timers[0].joined


def test_signal_keeps_immediate_stop_state_when_bound_loop_has_closed(process_environment):
    _, handlers, exits, _, timers = process_environment
    calls = []

    class ClosedLoop:
        def call_soon_threadsafe(self, callback):
            calls.append(callback)
            raise RuntimeError("Event loop is closed")

    stop = asyncio.Event()
    with lifecycle.ProcessLifecycle(15) as control:
        control.bind(ClosedLoop(), stop)
        handlers[signal.SIGTERM](signal.SIGTERM, None)
        assert stop.is_set() and len(calls) == 1 and not exits
    assert timers[0].cancelled and timers[0].joined


@pytest.mark.skipif(os.name != "posix", reason="POSIX selector, pipe and signal contract")
def test_signal_wakes_idle_selector_without_a_periodic_timer():
    process = subprocess.Popen([sys.executable, "-c", IDLE_SIGNAL_PROBE], stdin=subprocess.PIPE,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, bufsize=0)

    def line():
        assert select.select([process.stdout], [], [], 3)[0], "selector probe handshake timed out"
        return process.stdout.readline()

    try:
        assert line() == b"READY\n"
        process.stdin.write(b"I")
        process.stdin.flush()
        assert line() == b"IDLE\n"
        process.send_signal(signal.SIGTERM)
        assert line() == b"DRAINED\n"
        assert line() == b"CLEAN\n"
        assert process.wait(timeout=3) == 0
        assert b"TASK_WORKER_HARD_EXIT" not in process.stderr.read()
    finally:
        if process.poll() is None:
            process.kill()
            process.wait(timeout=3)
        process.stdin.close()
        process.stdout.close()
        process.stderr.close()


@pytest.mark.skipif(os.name != "posix", reason="POSIX process signal and pipe contract")
def test_watchdog_exits_even_when_first_signal_log_blocks_on_full_stderr():
    # This owns one tiny stdlib-only child, no Worker, SDK, Temporal or network.
    code = textwrap.dedent("""
        import os, time
        from mini_loop_deepagents.task_server.lifecycle import ProcessLifecycle
        with ProcessLifecycle(0.2):
            os.set_blocking(2, False)
            try:
                while True:
                    os.write(2, b'x' * 4096)
            except BlockingIOError:
                pass
            os.set_blocking(2, True)
            print('SATURATED', flush=True)
            while True:
                time.sleep(0.05)
    """)
    process = subprocess.Popen([sys.executable, "-c", code], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        assert select.select([process.stdout], [], [], 3)[0], "child did not reach the full-pipe barrier"
        assert process.stdout.readline() == b"SATURATED\n"
        process.send_signal(signal.SIGTERM)
        assert process.wait(timeout=3) == lifecycle.HARD_EXIT_CODE
    finally:
        if process.poll() is None:
            process.kill()
            process.wait(timeout=3)
        process.stdout.close()
        process.stderr.close()


class ControlledWorker:
    def __init__(self, *, validation=None, failure=None):
        self.validation = validation
        self.failure = failure
        self.is_running = False
        self.started = asyncio.Event()
        self.shutdown_called = asyncio.Event()
        self.finish = asyncio.Event()

    async def run(self):
        self.started.set()
        if self.validation is not None:
            await self.validation.wait()
        if self.failure is not None:
            raise self.failure
        self.is_running = True
        await self.finish.wait()

    async def shutdown(self):
        self.shutdown_called.set()
        if self.validation is not None:
            self.validation.set()
        self.finish.set()


@pytest.fixture
def compose_worker(monkeypatch):
    captures = []

    async def connect(*args, **kwargs):
        return object()

    monkeypatch.setattr(worker, "connect_temporal", connect)
    monkeypatch.setattr(worker, "ProcessLifecycle", lambda *args: pytest.fail("library must not own process"))

    def compose(instance):
        def build(*args, **kwargs):
            captures.append(kwargs)
            return instance
        monkeypatch.setattr(worker, "Worker", build)
        return captures
    return compose


@pytest.mark.asyncio
async def test_library_stop_drains_registered_v1_v2_without_cancelling_worker(compose_worker, capsys):
    instance = ControlledWorker()
    captures = compose_worker(instance)
    stop = asyncio.Event()
    task = asyncio.create_task(worker.serve("address", "queue", "identity",
        model_provider=lambda _: None, stop_event=stop, shutdown_grace_seconds=3))
    await instance.started.wait()
    # Let serve observe successful validation and publish readiness.
    await asyncio.sleep(0.02)
    stop.set()
    await asyncio.wait_for(task, 1)
    assert instance.shutdown_called.is_set()
    assert captures[0]["graceful_shutdown_timeout"] == timedelta(seconds=3)
    assert captures[0]["workflows"] == [worker.TaskWorkflow, worker.TaskWorkflowV2]
    assert "TASK_WORKER_READY identity=identity" in capsys.readouterr().out


@pytest.mark.asyncio
async def test_stop_during_sdk_validation_never_claims_ready(compose_worker, capsys):
    validation = asyncio.Event()
    instance = ControlledWorker(validation=validation)
    compose_worker(instance)
    stop = asyncio.Event()
    task = asyncio.create_task(worker.serve("address", "queue", "identity",
        model_provider=lambda _: None, stop_event=stop))
    await instance.started.wait()
    stop.set()
    await asyncio.wait_for(task, 1)
    assert instance.shutdown_called.is_set()
    assert "TASK_WORKER_READY" not in capsys.readouterr().out


@pytest.mark.asyncio
async def test_stop_during_connect_cancels_startup_without_model_or_worker(monkeypatch, capsys):
    connecting, cancelled, stop = asyncio.Event(), asyncio.Event(), asyncio.Event()

    async def connect(*args, **kwargs):
        connecting.set()
        try:
            await asyncio.Event().wait()
        finally:
            cancelled.set()

    monkeypatch.setattr(worker, "connect_temporal", connect)
    monkeypatch.setattr(worker, "Worker", lambda *a, **k: pytest.fail("stopped before Worker"))
    task = asyncio.create_task(worker.serve("address", "queue", "identity", stop_event=stop))
    await connecting.wait()
    stop.set()
    await asyncio.wait_for(task, 1)
    assert cancelled.is_set()
    assert "TASK_WORKER_READY" not in capsys.readouterr().out


@pytest.mark.asyncio
async def test_stopped_before_start_does_not_connect(monkeypatch, capsys):
    stop = asyncio.Event()
    stop.set()
    monkeypatch.setattr(worker, "connect_temporal", lambda *a, **k: pytest.fail("already stopped"))
    await worker.serve("address", "queue", "identity", stop_event=stop)
    assert "TASK_WORKER_READY" not in capsys.readouterr().out


@pytest.mark.asyncio
async def test_validation_fatal_is_not_hidden_by_shutdown_wait(compose_worker, capsys):
    failure = RuntimeError("SDK namespace validation")
    instance = ControlledWorker(failure=failure)
    compose_worker(instance)
    with pytest.raises(RuntimeError) as raised:
        await asyncio.wait_for(worker.serve("address", "queue", "identity",
            model_provider=lambda _: None), 1)
    assert raised.value is failure
    assert "TASK_WORKER_READY" not in capsys.readouterr().out


@pytest.mark.asyncio
async def test_library_caller_cancellation_requests_shutdown_and_propagates(compose_worker):
    instance = ControlledWorker()
    compose_worker(instance)
    task = asyncio.create_task(worker.serve("address", "queue", "identity", model_provider=lambda _: None))
    await instance.started.wait()
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await asyncio.wait_for(task, 1)
    assert instance.shutdown_called.is_set()


def test_cli_rejects_bad_shutdown_budget_before_process_start(monkeypatch):
    monkeypatch.setattr(worker, "run_process", lambda *a, **k: pytest.fail("invalid CLI must not start"))
    monkeypatch.setattr("sys.argv", ["worker", "--address", "x", "--task-queue", "q", "--identity", "i",
                                    "--shutdown-grace-seconds", "nan"])
    with pytest.raises(SystemExit) as raised:
        worker.main()
    assert raised.value.code == 2
