"""Shared process support for bounded acceptance scenarios."""
from __future__ import annotations

import asyncio
from dataclasses import dataclass
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import threading
import time

from tooling.paths import MODULE


RSS_BUDGET_KIB = 2 * 1024 * 1024
FREE_FLOOR_PERCENT = 15
MAX_SECONDS = 180


class HarnessFailure(RuntimeError):
    """An intentionally safe diagnostic suitable for the receipt."""


class ResourceBlocked(HarnessFailure):
    pass


def timestamp():
    return datetime.now(timezone.utc).isoformat()


def private_json(path, value, run_id):
    """Atomically replace this run's receipt so stale passes cannot survive a run."""
    temporary = path.with_name(path.name + "." + run_id + ".tmp")
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    try:
        with os.fdopen(descriptor, "w") as stream:
            os.fchmod(stream.fileno(), 0o600)
            json.dump(value, stream, indent=2)
            stream.write("\n")
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def private_text(path, text):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(text)


def first_difference(expected, actual, path="state"):
    """Locate a snapshot mismatch without dumping message bodies or schemas."""
    if type(expected) is not type(actual):
        return path + " (type)"
    if isinstance(expected, dict):
        if set(expected) != set(actual):
            return path + " (keys)"
        for key in sorted(expected):
            difference = first_difference(expected[key], actual[key], path + "." + key)
            if difference:
                return difference
    elif isinstance(expected, list):
        if len(expected) != len(actual):
            return path + " (length)"
        for index, (left, right) in enumerate(zip(expected, actual, strict=True)):
            difference = first_difference(left, right, f"{path}[{index}]")
            if difference:
                return difference
    elif expected != actual:
        return path
    return None


def subprocess_environment():
    allowed = {"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL"}
    environment = {key: value for key, value in os.environ.items() if key in allowed}
    environment.update(PYTHONPATH=os.pathsep.join((str(MODULE / "src"), str(MODULE))), PYTHONUNBUFFERED="1",
                       LANGSMITH_TRACING="false", OMP_NUM_THREADS="1",
                       OPENBLAS_NUM_THREADS="1", VECLIB_MAXIMUM_THREADS="1")
    return environment


def process_rows():
    result = subprocess.run(["/bin/ps", "-axo", "pid=,ppid=,pgid=,rss="],
                            capture_output=True, text=True, timeout=3, check=False)
    if result.returncode:
        raise HarnessFailure("Owned process RSS could not be observed")
    rows = {}
    for line in result.stdout.splitlines():
        fields = line.split()
        if len(fields) == 4 and all(field.isdigit() for field in fields):
            pid, parent, group, rss = map(int, fields)
            rows[pid] = (parent, group, rss)
    if os.getpid() not in rows:
        raise HarnessFailure("The acceptance driver process was absent from the RSS sample")
    return rows


def owned_rss_kib(rows, driver_pid, process_groups):
    owned = {driver_pid}
    while True:
        additions = {pid for pid, (parent, _, _) in rows.items() if parent in owned}
        if additions <= owned:
            break
        owned.update(additions)
    # Process-group membership retains owned descendants after reparenting.
    owned.update(pid for pid, (_, group, _) in rows.items() if group in process_groups)
    return sum(rows[pid][2] for pid in owned if pid in rows)


def free_percent():
    if sys.platform != "darwin":
        return None
    result = subprocess.run(["/usr/bin/memory_pressure", "-Q"], capture_output=True,
                            text=True, timeout=3, check=False)
    match = re.search(r"System-wide memory free percentage:\s*(\d+)%", result.stdout)
    if result.returncode or not match:
        raise HarnessFailure("System memory pressure could not be observed")
    return int(match[1])


@dataclass


class OwnedProcess:
    name: str
    process: subprocess.Popen
    log_path: Path


class Processes:
    def __init__(self, directory, deadline):
        self.directory, self.deadline = directory, deadline
        self.records = []
        self.lock = threading.RLock()
        self.stop_lock = threading.Lock()
        self.failure = None

    def check(self):
        if self.failure is not None:
            raise self.failure
        # Reserve a bounded cleanup window inside the overall 180-second budget.
        if time.monotonic() >= self.deadline - 5:
            raise HarnessFailure("The acceptance execution deadline was reached")

    def spawn(self, name, arguments, *, extra_environment=None):
        self.check()
        environment = subprocess_environment()
        environment.update(extra_environment or {})
        log_path = self.directory / (name + ".log")
        descriptor = os.open(log_path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
        with os.fdopen(descriptor, "ab", buffering=0) as log:
            process = subprocess.Popen(["/usr/bin/nice", "-n", "10", *map(str, arguments)],
                                       cwd=MODULE, env=environment, stdin=subprocess.DEVNULL,
                                       stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        record = OwnedProcess(name, process, log_path)
        with self.lock:
            self.records.append(record)
        return record

    def signal(self, record, signum):
        with self.lock:
            if record not in self.records:
                raise HarnessFailure("Refusing to signal an unregistered process")
        process = record.process
        if process.poll() is not None:
            raise HarnessFailure("The owned process was already stopped before fault injection")
        # start_new_session makes this group unique to this recorded child.
        if os.getpgid(process.pid) != process.pid:
            raise HarnessFailure("The owned child no longer has its original process group")
        os.killpg(process.pid, signum)

    def kill(self, record):
        self.check()
        self.signal(record, signal.SIGKILL)
        record.process.wait(timeout=min(3, max(0.1, self.deadline - time.monotonic())))
        if record.process.returncode != -signal.SIGKILL:
            raise HarnessFailure("The intended SIGKILL fault was not observed")

    def groups(self):
        with self.lock:
            return {record.process.pid for record in self.records if record.process.poll() is None}

    def stop_all(self, *, force=False):
        with self.stop_lock:
            with self.lock:
                records = list(reversed(self.records))
            for record in records:
                if record.process.poll() is None:
                    try:
                        self.signal(record, signal.SIGKILL if force else signal.SIGTERM)
                    except (ProcessLookupError, HarnessFailure):
                        pass
            grace_deadline = min(self.deadline, time.monotonic() + (0.2 if force else 1.5))
            while any(record.process.poll() is None for record in records) and time.monotonic() < grace_deadline:
                time.sleep(0.02)
            for record in records:
                if record.process.poll() is None:
                    try:
                        self.signal(record, signal.SIGKILL)
                    except (ProcessLookupError, HarnessFailure):
                        pass
            for record in records:
                remaining = self.deadline - time.monotonic()
                if record.process.poll() is None and remaining > 0:
                    try:
                        record.process.wait(timeout=min(0.5, remaining))
                    except subprocess.TimeoutExpired:
                        pass
            return all(record.process.poll() is not None for record in records)


class Watchdog:
    def __init__(self, processes):
        self.processes = processes
        self.peak_kib = 0
        self.minimum_free = None
        self.samples = 0
        self.stopped = threading.Event()
        self.thread = threading.Thread(target=self.observe, name="temporal-fault-resource-watch", daemon=True)

    def observe(self):
        failures = 0
        while not self.stopped.is_set():
            try:
                if time.monotonic() >= self.processes.deadline - 5:
                    raise HarnessFailure("The acceptance execution deadline was reached")
                rss = owned_rss_kib(process_rows(), os.getpid(), self.processes.groups())
                self.peak_kib = max(self.peak_kib, rss)
                free = free_percent()
                if free is not None:
                    self.minimum_free = min(self.minimum_free if self.minimum_free is not None else free, free)
                self.samples += 1
                if rss > RSS_BUDGET_KIB:
                    raise ResourceBlocked("Owned aggregate process RSS exceeded 2 GiB")
                if free is not None and free < FREE_FLOOR_PERCENT:
                    raise ResourceBlocked("System memory free percentage fell below 15 percent")
                failures = 0
            except (ResourceBlocked, HarnessFailure) as error:
                if isinstance(error, ResourceBlocked) or time.monotonic() >= self.processes.deadline - 5:
                    self.processes.failure = error
                    self.processes.stop_all(force=True)
                    return
                failures += 1
                if failures >= 3:
                    self.processes.failure = ResourceBlocked("Resource observation repeatedly failed")
                    self.processes.stop_all(force=True)
                    return
            except Exception:
                failures += 1
                if failures >= 3:
                    self.processes.failure = ResourceBlocked("Resource observation repeatedly failed")
                    self.processes.stop_all(force=True)
                    return
            self.stopped.wait(0.5)

    def start(self):
        self.thread.start()

    def stop(self):
        self.stopped.set()
        self.thread.join(timeout=min(3, max(0, self.processes.deadline - time.monotonic())))


async def eventually(processes, operation, predicate, *, timeout=25):
    deadline = min(processes.deadline - 5, time.monotonic() + timeout)
    last_value = None
    while time.monotonic() < deadline:
        processes.check()
        last_value = await operation()
        if predicate(last_value):
            return last_value
        await asyncio.sleep(0.1)
    raise HarnessFailure("A required fault-recovery condition was not observed before its deadline")


async def connect(processes, address):
    from temporalio.client import Client

    async def attempt():
        try:
            return await asyncio.wait_for(Client.connect(address), timeout=2)
        except Exception:
            return None

    return await eventually(processes, attempt, lambda client: client is not None, timeout=30)
