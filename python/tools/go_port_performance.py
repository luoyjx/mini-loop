"""Compare isolated Python/Go fake HTTP workloads; no external provider calls.

Build Go separately, then run with this repository's venv Python and --go-binary.
Latencies include client JSON/HTTP work. RSS is a post-workload server sample,
not peak memory or descendants. This does not benchmark streaming or persistence.
"""
from __future__ import annotations

import argparse
from contextlib import contextmanager
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
import hashlib
import http.client
import json
import math
import os
from pathlib import Path
import platform
import signal
import socket
import statistics
import subprocess
import sys
import tempfile
import time
from typing import Iterator

ROOT = Path(__file__).resolve().parents[2]


@dataclass(frozen=True)
class Sample:
    metric: str
    milliseconds: float


@dataclass(frozen=True)
class Run:
    runtime: str
    pair: int
    samples: list[Sample]
    finals: list[str]
    rss_kib_after_workload: int | None


def summary(values: list[float]) -> dict[str, float | int]:
    if not values or any(not math.isfinite(v) or v < 0 for v in values):
        raise ValueError("samples must be nonempty, finite and nonnegative")
    ordered = sorted(values)
    return {"n": len(values), "median_ms": statistics.median(values),
            "p95_ms": ordered[math.ceil(len(values) * .95) - 1],
            "min_ms": ordered[0], "max_ms": ordered[-1]}


def request(client: http.client.HTTPConnection, method: str, path: str,
            body: dict[str, str] | None = None) -> tuple[dict, float]:
    started = time.perf_counter()
    encoded = None if body is None else json.dumps(body).encode()
    client.request(method, path, encoded, {"Content-Type": "application/json"})
    response = client.getresponse()
    value = json.loads(response.read())
    elapsed = (time.perf_counter() - started) * 1000
    if response.status != 200 or not isinstance(value, dict):
        raise RuntimeError(f"{method} {path}: unexpected HTTP {response.status}")
    return value, elapsed


def isolated_environment(directory: Path, port: int) -> dict[str, str]:
    return {"PATH": os.environ.get("PATH", "/usr/bin:/bin"),
            "HOME": str(directory), "TMPDIR": str(directory),
            "PYTHONPATH": str(ROOT / "python"), "PYTHON_DOTENV_DISABLED": "1",
            "HOST": "127.0.0.1", "PORT": str(port),
            "MINILOOP_WORKSPACE_ROOT": str(directory / "workspaces"),
            "MINILOOP_FAKE_LLM": "1", "MINILOOP_FAKE_DELAY": "0"}


@contextmanager
def server(runtime: str, binary: Path, directory: Path) -> Iterator[tuple[http.client.HTTPConnection, Sample, int]]:
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        port = reservation.getsockname()[1]
    command = [sys.executable, "-m", "mini_loop"] if runtime == "python" else [str(binary)]
    with (directory / "server.log").open("wb") as log:
        started = time.perf_counter()
        process = subprocess.Popen(command, cwd=directory,
                                   env=isolated_environment(directory, port),
                                   stdout=log, stderr=log, start_new_session=True)
        client = http.client.HTTPConnection("127.0.0.1", port, timeout=5)
        try:
            deadline = time.monotonic() + 30
            while time.monotonic() < deadline:
                if process.poll() is not None:
                    raise RuntimeError(f"{runtime} exited before readiness")
                try:
                    value, _ = request(client, "GET", "/healthz")
                    if value.get("status") != "ok":
                        raise RuntimeError("unhealthy server")
                    break
                except (OSError, http.client.HTTPException):
                    client.close()
                    time.sleep(.01)
            else:
                raise RuntimeError(f"{runtime} readiness timed out")
            yield client, Sample("startup", (time.perf_counter()-started)*1000), process.pid
        finally:
            client.close()
            requested_stop = process.poll() is None
            if requested_stop:
                process.send_signal(signal.SIGTERM)
            try:
                code = process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
                raise RuntimeError(f"{runtime} failed to join on shutdown")
            log_text = (directory / "server.log").read_text(errors="replace")
            # Uvicorn re-raises the captured signal after its lifespan closes.
            source_closed = (runtime == "python" and requested_stop and
                             code == -signal.SIGTERM and
                             "Application shutdown complete." in log_text)
            if code != 0 and not source_closed:
                raise RuntimeError(f"{runtime} exited with {code}; log: {log_text}")
            with socket.socket() as probe:
                probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                probe.bind(("127.0.0.1", port))


def turn(client: http.client.HTTPConnection, prompt: str) -> tuple[list[Sample], str]:
    info, create_ms = request(client, "POST", "/sessions", {})
    session_id = info.get("id")
    if not isinstance(session_id, str) or not session_id:
        raise RuntimeError("create returned no session identity")
    path = "/sessions/" + session_id
    mode, _ = request(client, "POST", path + "/mode", {"mode": "auto"})
    if mode.get("permission_mode") != "auto":
        raise RuntimeError("auto permission mode was not selected")
    reply, turn_ms = request(client, "POST", path + "/messages", {"message": prompt + ";printf perf>proof.txt"})
    final = reply.get("final")
    if not isinstance(final, str) or "handled: " + prompt not in final:
        raise RuntimeError("fake turn did not return the actual Bash output")
    workspace = info.get("workspace")
    if not isinstance(workspace, str) or (Path(workspace) / "proof.txt").read_bytes() != b"perf":
        raise RuntimeError("Bash proof artifact is missing or incorrect")
    deleted, delete_ms = request(client, "DELETE", path)
    if deleted.get("deleted") != session_id:
        raise RuntimeError("session cleanup did not succeed")
    return [Sample("create", create_ms), Sample("turn", turn_ms), Sample("delete", delete_ms)], final


def resident_kib(pid: int) -> int | None:
    try:
        output = subprocess.check_output(["ps", "-o", "rss=", "-p", str(pid)], text=True)
        return int(output.strip())
    except (OSError, subprocess.CalledProcessError, ValueError):
        return None


def run(runtime: str, pair: int, binary: Path, directory: Path,
        health_samples: int, turns: int) -> Run:
    samples, finals = [], []
    with server(runtime, binary, directory) as (client, startup, pid):
        samples.append(startup)
        for _ in range(2):
            request(client, "GET", "/healthz")
            turn(client, "performance-warmup")
        for _ in range(health_samples):
            health, elapsed = request(client, "GET", "/healthz")
            if health.get("status") != "ok":
                raise RuntimeError("health workload failed")
            samples.append(Sample("health", elapsed))
        for index in range(turns):
            measured, final = turn(client, f"performance-probe-{index}")
            samples.extend(measured)
            finals.append(final)
        rss = resident_kib(pid)
    return Run(runtime, pair, samples, finals, rss)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go-binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--pairs", type=int, default=3)
    parser.add_argument("--health-samples", type=int, default=100)
    parser.add_argument("--turns", type=int, default=20)
    args = parser.parse_args(argv)
    if min(args.pairs, args.health_samples, args.turns) < 1:
        parser.error("pairs, health-samples and turns must all be positive")
    binary = args.go_binary.resolve(strict=True)
    runs = []
    with tempfile.TemporaryDirectory(prefix="mini-loop-port-perf-") as temporary:
        for pair in range(args.pairs):
            order = ["python", "go"] if pair % 2 == 0 else ["go", "python"]
            measured = []
            for runtime in order:
                directory = Path(temporary) / f"{pair}-{runtime}"
                directory.mkdir()
                result = run(runtime, pair, binary, directory, args.health_samples, args.turns)
                runs.append(result)
                measured.append(result)
            if measured[0].finals != measured[1].finals:
                raise RuntimeError("Python/Go fake turn output differs")
    summaries = {}
    for runtime in ("python", "go"):
        selected = [r for r in runs if r.runtime == runtime]
        summaries[runtime] = {metric: summary([s.milliseconds for r in selected for s in r.samples if s.metric == metric])
                              for metric in ("startup", "health", "create", "turn", "delete")}
    report = {"schema_version": 1, "recorded_utc": datetime.now(timezone.utc).isoformat(),
              "code_baseline": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
              "host": {"system": platform.system(), "machine": platform.machine(), "release": platform.release()},
              "python": sys.version, "go_build": subprocess.check_output(["go", "version", "-m", str(binary)], text=True),
              "go_binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
              "parameters": {"pairs": args.pairs, "health_samples": args.health_samples, "turns": args.turns, "warmup_turns": 2},
              "scope": "serial persistent HTTP; default fake; zero provider delay; real Bash with verified file effect; fresh sessions; prebuilt Go; nearest-rank p95; RSS after workload, not peak/descendants; startup includes readiness polling; no SSE/SQLite/concurrency/provider benchmark",
              "summary": summaries, "runs": [asdict(r) for r in runs]}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(summaries, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
