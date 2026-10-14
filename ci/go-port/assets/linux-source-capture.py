#!/usr/bin/env python3
"""Prepare-only artifact: root runs this inside its bounded native Linux container.

Run six Source recipe groups, serially, into a fresh output directory. Production
code and tracked fixtures are read-only inputs. There is no Docker/pip/Go command.
"""
from __future__ import annotations

import argparse
import hashlib
import importlib.metadata
import importlib.util
import inspect
import json
import os
from pathlib import Path
import platform
import signal
import subprocess
import sys
import tempfile
import time
import traceback


GROUPS = {
    "benchmark": ("_benchmark_arm_contracts", ["visible"], "python-benchmark-arms.json"),
    "httpapi": ("_benchmark_http_contracts", ["default"], "python-benchmark-http.json"),
    "httpapi-efficiency": ("_benchmark_http_contracts", ["efficiency-shadow"], "python-benchmark-efficiency-http.json"),
    "userresources": ("_owner_resource_contracts", ["problems"], "python-owner-resources.json"),
    "workspace": ("_glob_contracts", ["glob_truncated"], "python-glob-tools.json"),
    "worktrees": ("_worktree_contracts", ["not-repository", "unverified-status"], "python-worktrees.json"),
}


def sha(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def write_json(path: Path, data: object) -> None:
    with path.open("x", encoding="utf-8") as stream:
        json.dump(data, stream, indent=2, sort_keys=True, ensure_ascii=False)
        stream.write("\n")


def command(argv: list[str]) -> dict:
    result = subprocess.run(argv, capture_output=True, text=True, timeout=10)
    return {"argv": argv, "exit_code": result.returncode,
            "stdout": result.stdout, "stderr": result.stderr}


def cgroup() -> dict:
    result = {}
    for name in ("memory.current", "memory.peak", "memory.events", "memory.max",
                 "memory.swap.max", "cpu.max", "pids.current", "pids.max"):
        path = Path("/sys/fs/cgroup") / name
        result[name] = path.read_text().strip() if path.exists() else None
    return result


def mount_for(path: Path) -> dict:
    candidates = []
    for line in Path("/proc/self/mountinfo").read_text().splitlines():
        left, right = line.split(" - ", 1)
        fields, tail = left.split(), right.split()
        mount = Path(fields[4].replace("\\040", " ").replace("\\134", "\\"))
        if path == mount or mount in path.parents:
            candidates.append({"mountpoint": str(mount), "filesystem": tail[0],
                               "source": tail[1], "options": fields[5], "raw": line})
    if not candidates:
        raise RuntimeError(f"No mountinfo entry for {path}")
    return max(candidates, key=lambda row: len(row["mountpoint"]))


def context() -> dict:
    shell = Path("/bin/sh").resolve()
    return {"platform": platform.platform(), "uname": list(platform.uname()),
            "python": sys.version, "executable": sys.executable,
            "shell": {"path": str(shell), "sha256": sha(shell.read_bytes())},
            "git": command(["git", "--version"]),
            "tmp": {"resolved": str(Path("/tmp").resolve()),
                    "mount": mount_for(Path("/tmp")),
                    "stat": command(["stat", "-f", "-c", "%T", "/tmp"]),
                    "device": Path("/tmp").stat().st_dev},
            "os_release": Path("/etc/os-release").read_text(),
            "locale": {key: os.environ.get(key) for key in ("LANG", "LC_ALL", "LC_CTYPE")},
            "cgroup": cgroup()}


def worker(args: argparse.Namespace) -> int:
    source_root, output = args.source_root.resolve(), args.output.resolve()
    group = args.worker
    symbol, selected, fixture = GROUPS[group]
    producer_path = source_root / "python/tools/export_go_contracts.py"
    scratch = Path(tempfile.mkdtemp(prefix=f"miniloop-source-{group}-", dir="/tmp"))
    os.chdir(scratch)
    tempfile.tempdir = str(scratch)
    # Clear inherited credentials/configuration before importing any runtime.
    # PYTHONPATH is already reflected in sys.path; external /deps remains usable.
    os.environ.clear()
    os.environ.update({"HOME": str(scratch), "PATH": "/usr/local/bin:/usr/bin:/bin",
                       "LANG": "C.UTF-8", "TMPDIR": str(scratch),
                       "PYTHONDONTWRITEBYTECODE": "1"})
    sys.dont_write_bytecode = True
    sys.path.insert(0, str(source_root / "python"))
    spec = importlib.util.spec_from_file_location("miniloop_source_exporter", producer_path)
    if spec is None or spec.loader is None:
        raise RuntimeError("Cannot import frozen Source producer")
    producer = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = producer
    spec.loader.exec_module(producer)
    function = getattr(producer, symbol)
    lines, line = inspect.getsourcelines(function)
    exact = "".join(lines)
    write_json(output / f"{group}.producer.json", {
        "producer_path": "python/tools/export_go_contracts.py",
        "producer_sha256": sha(producer_path.read_bytes()),
        "symbol": symbol, "line": line, "exact_source": exact,
        "function_sha256": sha(exact.encode()),
        "driver": "Direct original producer call. All original cases and return metadata retained.",
        "source_transforms": [],
    })
    started = time.monotonic()
    if group == "benchmark":
        result = function()
    elif group.startswith("httpapi"):
        result = function(efficiency_settings=group == "httpapi-efficiency")
    else:
        result = function(scratch / "recipes")
    plan = json.loads(args.plan.read_text())
    expected = plan["groups"][group]["expected_all_cases"]
    actual = [row["name"] for row in result["cases"]]
    if actual != expected:
        raise RuntimeError("Captured original producer inventory differs from baseline")
    write_json(output / fixture, result)
    extra = {"scratch": str(scratch), "mount": mount_for(scratch), "seconds": time.monotonic() - started,
             "top_level_keys": sorted(result), "captured_cases": actual,
             "go_minimum_cases": selected, "cgroup": cgroup()}
    if group == "workspace":
        workspace = scratch / "recipes/glob_truncated/workspace"
        # Record the actual unsorted directory entries after the real Source
        # capture. No additional files are created inside this directory.
        extra["raw_directory_order"] = os.listdir(workspace)
        extra["directory_stat"] = {"device": workspace.stat().st_dev,
                                   "inode": workspace.stat().st_ino}
    write_json(output / f"{group}.context.json", extra)
    return 0


def verify_inputs(args: argparse.Namespace, plan: dict) -> dict:
    if platform.system() != "Linux" or platform.machine() not in ("aarch64", "arm64"):
        raise RuntimeError("Capture requires native Linux arm64")
    if sys.version_info[:2] != (3, 11):
        raise RuntimeError("Capture requires Python 3.11 to match the selected baseline")
    environment = context()
    tmp_mount = environment["tmp"]["mount"]
    if tmp_mount["mountpoint"] != "/tmp" or tmp_mount["filesystem"] not in ("btrfs", "ext4", "xfs"):
        raise RuntimeError("Use a dedicated native Linux Docker volume mounted at /tmp")
    if environment["git"]["stdout"].strip() != "git version 2.39.5":
        raise RuntimeError("Git version differs from the original Linux comparison")
    if Path(environment["shell"]["path"]).name != "dash":
        raise RuntimeError("/bin/sh differs from original Linux dash context")
    verified_files = {}
    for path, expected in plan["source_hashes"].items():
        actual = sha((args.source_root / path).read_bytes())
        if actual != expected:
            raise RuntimeError(f"Frozen Source hash mismatch: {path}")
        verified_files[path] = actual
    actual_packages = {}
    for row in plan["dependencies"]["capture_required"]:
        actual = importlib.metadata.version(row["name"])
        if actual != row["version"]:
            raise RuntimeError(f"Dependency mismatch: {row['name']} {actual} != {row['version']}")
        actual_packages[row["name"]] = actual
    environment.update(source_hashes=verified_files, dependencies=actual_packages,
                       plan_sha256=sha(args.plan.read_bytes()),
                       script_sha256=sha(Path(__file__).read_bytes()))
    return environment


def capture(args: argparse.Namespace) -> int:
    args.source_root = args.source_root.resolve()
    args.output = args.output.resolve()
    if args.output == args.source_root or args.source_root in args.output.parents:
        raise RuntimeError("Output must be outside the frozen source root")
    args.output.mkdir(parents=False, exist_ok=False)
    receipt = {"status": "started", "groups": [], "input_plan": str(args.plan),
               "source_root": str(args.source_root), "executed_full_export": False}
    active = None

    def interrupted(signum, frame):
        raise InterruptedError(f"signal {signum}")

    for sig in (signal.SIGTERM, signal.SIGINT):
        signal.signal(sig, interrupted)
    try:
        plan = json.loads(args.plan.read_text())
        environment = verify_inputs(args, plan)
        write_json(args.output / "environment.json", environment)
        for group, (_, selected, fixture) in GROUPS.items():
            row = {"id": group, "go_minimum_cases": selected, "fixture": fixture,
                   "started": time.time(), "cgroup_before": cgroup()}
            receipt["groups"].append(row)
            argv = [sys.executable, "-B", str(Path(__file__).resolve()), "--source-root", str(args.source_root),
                    "--output", str(args.output), "--plan", str(args.plan.resolve()), "--worker", group]
            with (args.output / f"{group}.stdout.log").open("xb") as stdout, \
                    (args.output / f"{group}.stderr.log").open("xb") as stderr:
                active = subprocess.Popen(argv, stdout=stdout, stderr=stderr, start_new_session=True)
                try:
                    row["exit_code"] = active.wait(timeout=args.timeout)
                except subprocess.TimeoutExpired:
                    row["timed_out"] = True
                    raise
                finally:
                    row["finished"] = time.time()
                    row["cgroup_after"] = cgroup()
            if row["exit_code"] != 0:
                receipt["status"] = "capture_failed"
                break
            active = None
        else:
            receipt["status"] = "captured_six_original_producers"
    except BaseException as exc:
        receipt["status"] = "capture_failed"
        receipt["error"] = f"{type(exc).__name__}: {exc}"
        receipt["traceback"] = traceback.format_exc()
    finally:
        # This runner owns only its current recipe process group. The outer
        # root runner owns container limits, stats, host pressure and cleanup.
        for sig in (signal.SIGTERM, signal.SIGINT):
            signal.signal(sig, signal.SIG_IGN)
        if active is not None and active.poll() is None:
            os.killpg(active.pid, signal.SIGTERM)
            try:
                active.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(active.pid, signal.SIGKILL)
                active.wait(timeout=5)
        receipt["files"] = {path.name: {"sha256": sha(path.read_bytes()), "bytes": path.stat().st_size}
                            for path in sorted(args.output.iterdir()) if path.is_file()}
        receipt["cgroup_final"] = cgroup()
        write_json(args.output / "receipt.json", receipt)
    print(json.dumps({"status": receipt["status"], "output": str(args.output)}))
    return 0 if receipt["status"] == "captured_six_original_producers" else 1


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-root", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--plan", type=Path, required=True)
    parser.add_argument("--timeout", type=int, default=240)
    parser.add_argument("--worker", choices=GROUPS)
    args = parser.parse_args()
    return worker(args) if args.worker else capture(args)


if __name__ == "__main__":
    raise SystemExit(main())
