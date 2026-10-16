"""Owned local LangGraph dev-server lifecycle and resource watchdog."""
from __future__ import annotations

import os
from pathlib import Path
import signal
import subprocess
import sys
import threading
import time
import httpx

from tooling.paths import MODULE


class Server:
    def __init__(self, directory: Path, config: Path, env: dict[str, str], port: int, log: Path):
        self.directory, self.config, self.env, self.port, self.log = directory, config, env, port, log
        self.process = None
        self.peak_rss_kib = 0
        self.blocked = None
        self.stop_watch = threading.Event()

    def _watch(self):
        ticks = 0
        while not self.stop_watch.wait(0.5) and self.process.poll() is None:
            rows = subprocess.check_output(["ps", "-axo", "pid=,ppid=,rss="], text=True)
            table = [tuple(map(int, row.split())) for row in rows.splitlines()]
            owned = {self.process.pid}
            while True:
                expanded = owned | {pid for pid, ppid, rss in table if ppid in owned}
                if expanded == owned:
                    break
                owned = expanded
            total = sum(rss for pid, ppid, rss in table if pid in owned)
            self.peak_rss_kib = max(self.peak_rss_kib, total)
            if total > 1024 * 1024:
                self.blocked = "server process tree exceeded 1 GiB RSS budget"
            if sys.platform == "darwin" and ticks % 10 == 0:
                pressure = subprocess.check_output(["memory_pressure", "-Q"], text=True)
                percent = int(pressure.rsplit(":", 1)[1].strip().removesuffix("%"))
                if percent < 15:
                    self.blocked = "system memory free percentage fell below 15%"
            ticks += 1
            if self.blocked:
                os.killpg(self.process.pid, signal.SIGTERM)
                return

    def start(self):
        self.stop_watch.clear()
        with self.log.open("ab") as output:
            self.process = subprocess.Popen([
                "nice", "-n", "10", str(MODULE / ".venv/bin/langgraph"), "dev",
                "--config", str(self.config), "--host", "127.0.0.1", "--port", str(self.port),
                "--no-browser", "--no-reload", "--n-jobs-per-worker", "2",
            ], cwd=self.directory, env=self.env, stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
        self.watcher = threading.Thread(target=self._watch, daemon=True)
        self.watcher.start()
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            if self.process.poll() is not None:
                raise RuntimeError(self.blocked or f"Agent Server exited at startup; see {self.log}")
            try:
                response = httpx.get(f"http://127.0.0.1:{self.port}/ok", timeout=1)
                if response.status_code == 200:
                    return
            except httpx.TransportError:
                pass
            time.sleep(0.2)
        raise TimeoutError(f"Agent Server startup timed out; see {self.log}")

    def stop(self):
        if self.process is not None and self.process.poll() is None:
            os.killpg(self.process.pid, signal.SIGINT)
            try:
                self.process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                os.killpg(self.process.pid, signal.SIGKILL)
                self.process.wait(timeout=5)
        self.stop_watch.set()
        if hasattr(self, "watcher"):
            self.watcher.join(timeout=3)
        if self.blocked:
            raise RuntimeError("resource-blocked: " + self.blocked)
