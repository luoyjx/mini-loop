"""One fresh Source observation per required failed recipe; no fixture replacement."""
import argparse
import hashlib
import importlib.metadata
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import traceback

ROOT = Path(__file__).resolve().parents[2]
NAMES = ["ignored-hidden", "ignored-backup", "ignored-text", "transient-source"]


def source_worker(name):
    sys.path[:0] = [str(ROOT / "python")]
    from tools.watchfiles_contracts import _native_case
    fixture = json.loads((ROOT / "go/testdata/python-watchfiles.json").read_text())
    original = next(row for row in fixture["native_events"] if row["name"] == name)
    recipe = {key: value for key, value in original.items() if key not in {"changed", "closed", "native_arguments"}}
    began = time.monotonic()
    try:
        with tempfile.TemporaryDirectory(prefix="mini-loop-required-source-") as scratch:
            # Match watchfiles_contracts: its fixture builder canonicalizes scratch
            # before comparing the readiness probe with native callback paths.
            source_root = Path(scratch).resolve() / "root"
            captured = _native_case(source_root, recipe)
        result = {"name": name, "capture": captured, "matches_required": captured["changed"] == original["changed"]}
    except Exception:
        result = {"name": name, "error": traceback.format_exc(), "matches_required": None}
    result["seconds"] = round(time.monotonic() - began, 3)
    print(json.dumps(result), flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--source-worker", choices=NAMES)
    parser.add_argument("--source-only", action="store_true", help="capture Source without repeating an already completed Go cohort")
    parser.add_argument("--output-prefix", default="/tmp/mini-loop-reload-required-absence-2026-10-11")
    args = parser.parse_args()
    if args.source_worker:
        source_worker(args.source_worker)
        return
    prefix = Path(args.output_prefix)
    assert not Path(str(prefix) + ".json").exists(), "choose a fresh prefix; do not retry a completed cohort"
    report = {"baseline": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
              "versions": {name: importlib.metadata.version(name) for name in ["watchfiles", "uvicorn"]},
              "source": [], "required": [], "hashes": {}}
    fixture = ROOT / "go/testdata/python-watchfiles.json"
    for path in [fixture, ROOT / "python/tools/watchfiles_contracts.py", ROOT / "go/reload/native_events_recipes_test.go",
                 ROOT / "go/reload/event_reader.go", ROOT / "go/reload/native_events_darwin.go", ROOT / "go/reload/native_events_darwin.c"]:
        report["hashes"][str(path.relative_to(ROOT))] = hashlib.sha256(path.read_bytes()).hexdigest()
    report["required"] = [row for row in json.loads(fixture.read_text())["native_events"] if row["name"] in NAMES]
    receipt = Path(str(prefix) + ".json")
    for name in NAMES:
        command = [sys.executable, str(Path(__file__).resolve()), "--source-worker", name]
        began = time.monotonic()
        try:
            result = subprocess.run(command, cwd=ROOT, capture_output=True, text=True, timeout=20)
            record = {"name": name, "command": command, "exit_code": result.returncode,
                      "stdout": result.stdout, "stderr": result.stderr}
            if result.returncode == 0:
                record["result"] = json.loads(result.stdout.splitlines()[-1])
        except subprocess.TimeoutExpired as error:
            record = {"name": name, "command": command, "watchdog_timeout": True,
                      "stdout": (error.stdout or b"").decode(), "stderr": (error.stderr or b"").decode()}
        record["seconds"] = round(time.monotonic() - began, 3)
        report["source"].append(record)
        receipt.write_text(json.dumps(report, indent=2) + "\n")
        print("source", name, flush=True)
    if args.source_only:
        report["hashes_unchanged"] = all(hashlib.sha256((ROOT / name).read_bytes()).hexdigest() == digest for name, digest in report["hashes"].items())
        assert report["hashes_unchanged"]
        receipt.write_text(json.dumps(report, indent=2) + "\n")
        return
    command = ["go", "test", "-v", "-race", "-covermode=atomic", "-coverpkg=./...", "-count=1", "-timeout=180s",
               "-run=^TestNativeEventsActualSourceRecipes/(ignored-hidden|ignored-backup|ignored-text|transient-source)$", "./reload"]
    log = Path(str(prefix) + "-go.log")
    began = time.monotonic()
    with log.open("w") as output:
        result = subprocess.run(command, cwd=ROOT / "go", stdout=output, stderr=subprocess.STDOUT)
    report["go"] = {"command": command, "exit_code": result.returncode,
                    "seconds": round(time.monotonic() - began, 3), "log": str(log), "raw_log": log.read_text()}
    report["hashes_unchanged"] = all(hashlib.sha256((ROOT / name).read_bytes()).hexdigest() == digest for name, digest in report["hashes"].items())
    assert report["hashes_unchanged"]
    receipt.write_text(json.dumps(report, indent=2) + "\n")
    print("go", result.returncode, flush=True)


if __name__ == "__main__":
    main()
