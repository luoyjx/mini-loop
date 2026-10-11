"""Overlay diagnostic: retain the exact fixture, assertions and 30s context.

Logs stage boundaries without editing repository test/runtime sources. Four
distinct instrumentation modes separate race and coverage effects, without retries.
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / "go/sqlitestate/decision_restart_test.go"
PREFIX = Path("/tmp/mini-loop-sqlite-decision-budget-2026-10-11")


def capture_native_sample(process, binary, log, prefix):
    assert process.args[0] == str(binary), "sample only the directly launched owned binary"
    # Process discovery is diagnostic only; it is not a filesystem observer fallback.
    while process.poll() is None:
        if "decision-stage readonly-turn-start" in log.read_text():
            table = subprocess.check_output(["/bin/ps", "-ww", "-p", str(process.pid), "-o", "pid=,ppid=,comm="], text=True)
            fields = table.strip().split(None, 2)
            if len(fields) == 3 and fields[0] == str(process.pid) and fields[2] == str(binary) and process.poll() is None:
                pid = process.pid
                destination = Path(str(prefix) + "-native.sample")
                command = ["/usr/bin/sample", str(pid), "5", "1", "-mayDie", "-fullPaths", "-file", str(destination)]
                began = time.monotonic()
                result = subprocess.run(command, capture_output=True, text=True, timeout=20)
                return {"pid": pid, "parent_pid": int(fields[1]), "binary": str(binary),
                        "identity_table": table, "command": command, "exit_code": result.returncode,
                        "seconds": round(time.monotonic() - began, 3),
                        "stdout": result.stdout, "stderr": result.stderr, "path": str(destination),
                        "raw_sample": destination.read_text() if destination.exists() else None}
        time.sleep(0.05)
    return {"not_captured": "owned binary did not remain live after readonly stage"}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--modes", nargs="+", choices=["ordinary", "race", "coverage", "race-coverage"],
                        default=["ordinary", "race", "coverage", "race-coverage"])
    parser.add_argument("--output-prefix", default=str(PREFIX))
    parser.add_argument("--native-sample", action="store_true", help="macOS: sample the owned race-coverage test child for 5s at readonly start")
    args = parser.parse_args()
    modes = args.modes
    if args.native_sample:
        assert sys.platform == "darwin" and modes == ["race-coverage"], "native sampling requires macOS and one race-coverage mode"
    prefix = Path(args.output_prefix)
    assert len(modes) == len(set(modes)), "each mode runs once"
    baseline = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    fixture_sha256 = hashlib.sha256((ROOT / "go/testdata/python-decision-replay.json").read_bytes()).hexdigest()
    original = SOURCE.read_text()
    candidate = original
    anchor = "\t\t\tdefer cancel()\n"
    assert candidate.count(anchor) == 1
    candidate = candidate.replace(anchor, anchor + '''\t\t\tstarted := time.Now()
\t\t\tmark := func(stage string) {
\t\t\t\tdeadline, _ := ctx.Deadline()
\t\t\t\tt.Logf("decision-stage %s elapsed=%s remaining=%s context=%v", stage, time.Since(started), time.Until(deadline), ctx.Err())
\t\t\t}
\t\t\tmark("context-created")
''')
    anchors = {
        '\t\t\troot := t.TempDir()': 'payload-decoded',
        '\t\t\tif _, err := session.RunWithContext(ctx, "decide", run);': 'first-turn-start',
        '\t\t\tcall := agent.ToolCall': 'first-turn-end',
        '\t\t\thistory := session.Messages()': 'action-read-and-digest-end',
        '\t\t\tstore = openStore(t, path)': 'database-reopen-start',
        '\t\t\tsession = restoreRestartSession(t, manager)': 'restore-start',
        '\t\t\tif !sameRestartMessages': 'restore-end',
        '\t\t\tif _, err := session.RunWithContext(ctx, "retry same identity", run);': 'replay-turn-start',
        '\t\t\tif _, err := session.ChangePermissionMode(agent.ModeReadonly);': 'replay-turn-end',
        '\t\t\tif _, err := session.RunWithContext(ctx, "retry while readonly", run);': 'readonly-turn-start',
        '\t\t\tretained, found, err := store.ReadAction': 'readonly-turn-end',
        '\t\t\tif status := session.PersistenceStatus()': 'final-health',
    }
    for anchor, stage in anchors.items():
        assert candidate.count(anchor) == 1, anchor
        candidate = candidate.replace(anchor, '\t\t\tmark("' + stage + '")\n' + anchor)
    anchor = '\t\t\t\tmodelCalls++'
    assert candidate.count(anchor) == 1
    candidate = candidate.replace(anchor, anchor + '\n\t\t\t\tt.Logf("decision-model-call %d elapsed=%s", modelCalls, time.Since(started))')
    receipts = []
    previous = Path(str(prefix) + ".json")
    if previous.exists():
        prior = json.loads(previous.read_text())
        assert prior["baseline"] in {baseline, baseline[:7]}, "choose fresh output prefix for a different revision"
        assert prior.get("fixture_sha256") == fixture_sha256, "choose fresh output prefix for a different or unrecorded fixture"
        assert prior["source_sha256"] == hashlib.sha256(original.encode()).hexdigest()
        assert prior["overlay_sha256"] == hashlib.sha256(candidate.encode()).hexdigest()
        assert not set(modes).intersection(run["name"] for run in prior["runs"]), "choose fresh output prefix to repeat a mode"
        receipts = prior["runs"]
    with tempfile.TemporaryDirectory(prefix="mini-loop-decision-overlay-") as scratch:
        overlay_source = Path(scratch) / SOURCE.name
        overlay_source.write_text(candidate)
        overlay = Path(scratch) / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {str(SOURCE): str(overlay_source)}}))
        common = ["go", "test", "-overlay=" + str(overlay), "-v", "-count=1", "-timeout=90s",
                  "-run=^TestDecisionNativeSQLiteReopenMatchesSource/maximum$", "./sqlitestate"]
        variants = {"ordinary": [], "race": ["-race"], "coverage": ["-covermode=atomic", "-coverpkg=./...",
                    "-coverprofile=" + str(prefix) + "-coverage.cover"],
                    "race-coverage": ["-race", "-coverpkg=./...", "-coverprofile=" + str(prefix) + ".cover",
                                      "-cpuprofile=" + str(prefix) + ".cpu", "-o=" + str(prefix) + ".test"]}
        for name in modes:
            command = common[:2] + variants[name] + common[2:]
            compilation = None
            if args.native_sample:
                binary = Path(str(prefix) + ".test")
                compile_command = ["go", "test", "-c", "-race", "-coverpkg=./...", "-overlay=" + str(overlay), "-o=" + str(binary), "./sqlitestate"]
                compiled = subprocess.run(compile_command, cwd=ROOT / "go", capture_output=True, text=True)
                compilation = {"command": compile_command, "working_directory": str(ROOT / "go"),
                               "exit_code": compiled.returncode, "stdout": compiled.stdout, "stderr": compiled.stderr}
                assert compiled.returncode == 0, compilation
                command = [str(binary), "-test.v", "-test.count=1", "-test.timeout=90s",
                           "-test.run=^TestDecisionNativeSQLiteReopenMatchesSource/maximum$",
                           "-test.coverprofile=" + str(prefix) + ".cover", "-test.cpuprofile=" + str(prefix) + ".cpu"]
            log = Path(str(prefix) + "-" + name + ".log")
            began = time.monotonic()
            native_sample = None
            working_directory = ROOT / "go/sqlitestate" if args.native_sample else ROOT / "go"
            with log.open("w") as output:
                with subprocess.Popen(command, cwd=working_directory,
                                      stdout=output, stderr=subprocess.STDOUT) as process:
                    if args.native_sample:
                        native_sample = capture_native_sample(process, Path(str(prefix) + ".test"), log, prefix)
                    exit_code = process.wait()
            receipts.append({"name": name, "command": command, "exit_code": exit_code,
                             "working_directory": str(working_directory),
                             "seconds": round(time.monotonic() - began, 3), "log": str(log),
                             "raw_log": log.read_text(), "compilation": compilation, "native_sample": native_sample})
            print(name, exit_code, flush=True)
    report = {"baseline": baseline, "fixture_sha256": fixture_sha256,
              "source_sha256": hashlib.sha256(original.encode()).hexdigest(),
              "overlay_sha256": hashlib.sha256(candidate.encode()).hexdigest(), "overlay_source": candidate,
              "original_context": "30 seconds shared across unchanged preparation, first turn, restart, replay and readonly",
              "instrumentation": "stage/model logs, CPU profiling in race-coverage mode, optional owned-process native sample; exact original fixture, assertions and contexts retained",
              "runs": receipts}
    previous.write_text(json.dumps(report, indent=2) + "\n")


if __name__ == "__main__":
    main()
