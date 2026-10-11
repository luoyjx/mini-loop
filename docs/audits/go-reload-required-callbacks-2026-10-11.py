"""Trace original required recipes at C entry before Go admission or filtering.

The first retained cohort attempted CGO Go-file overlay markers that did not
appear. Only observed C/test markers support its conclusions. The reusable
collector instruments those two observed surfaces; it makes no registry claim.
"""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]


def replace_once(text, anchor, replacement):
    assert text.count(anchor) == 1, anchor
    return text.replace(anchor, replacement)


def classify(log):
    """Use callback key and wall clock; exclude readiness and cleanup windows."""
    roots, operations, ends, batches, items, outcomes = {}, {}, {}, [], [], {}
    for line in log.splitlines():
        parts = line.split("\t")
        if parts[0] == "MINILOOP_RAW_START":
            roots[parts[2]] = int(parts[1])
        elif parts[0] == "MINILOOP_OP":
            operations[parts[2]] = (int(parts[1]), parts[3])
        elif parts[0] == "MINILOOP_END":
            ends[parts[2]] = int(parts[1])
        elif parts[0] == "MINILOOP_C_BATCH":
            batches.append((int(parts[1]), int(parts[2]), int(parts[3])))
        elif parts[0] == "MINILOOP_RAWC":
            items.append((int(parts[1]), int(parts[2]), parts[5]))
        match = re.search(r"--- (PASS|FAIL): TestNativeEventsActualSourceRecipes/([^ ]+) ", line)
        if match:
            outcomes[match[2]] = match[1]
    cases = []
    for name, (start, path) in operations.items():
        key, end = roots[str(Path(path).parent)], ends[name]
        before = [event for event in items if event[1] == key and event[0] < start]
        window_batches = [event for event in batches if event[1] == key and start <= event[0] <= end]
        window_items = [event for event in items if event[1] == key and start <= event[0] <= end]
        assert before, "C instrumentation must be witnessed before the operation"
        assert sum(event[2] for event in window_batches) == len(window_items)
        cases.append({"name": name, "result": outcomes[name], "key": key, "path": path,
                      "operation_start_ns": start, "cleanup_entry_ns": end,
                      "operation_window_seconds": round((end-start)/1e9, 6),
                      "before_operation_c_items": len(before),
                      "operation_c_batches": len(window_batches), "operation_c_items": len(window_items),
                      "operation_paths": [event[2] for event in window_items],
                      "after_cleanup_c_items": sum(event[1] == key and event[0] > end for event in items)})
    return {"cases": cases, "readiness_failures": sorted(set(outcomes)-set(operations)),
            "observed_raw_go_markers": sum(line.startswith("MINILOOP_RAWGO\t") for line in log.splitlines()),
            "observed_go_fence_markers": sum(line.startswith("MINILOOP_FENCE\t") for line in log.splitlines()),
            "scope": "Callback entry in the original operation window only; not kernel delivery, queue liveness, Go registry/fences or statistical acceptance."}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output-prefix", default="/tmp/mini-loop-reload-required-callbacks-2026-10-11")
    args = parser.parse_args()
    prefix = Path(args.output_prefix)
    receipt = Path(str(prefix) + ".json")
    assert not receipt.exists(), "choose fresh prefix; do not retry a completed cohort"
    helper = ROOT / "docs/audits/go-reload-boundary-overlay-2026-10-11.py"
    spec = importlib.util.spec_from_file_location("boundary_overlay", helper)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    paths = [ROOT / "go/reload/native_events_darwin.c", ROOT / "go/reload/native_events_recipes_test.go"]
    originals = {str(p): p.read_text() for p in paths}
    with tempfile.TemporaryDirectory(prefix="mini-loop-required-callback-overlay-") as scratch:
        destination = Path(scratch) / "c-overlay"
        module.overlay(destination)
        c_path = destination / paths[0].name
        c_text = replace_once(c_path.read_text(), " char **names = paths;", ''' char **names = paths;
 struct timespec entered;
 clock_gettime(CLOCK_REALTIME,&entered);
 fprintf(stderr,"MINILOOP_C_BATCH\\t%llu\\t%lu\\t%lu\\n",
  (unsigned long long)entered.tv_sec*1000000000ULL+(unsigned long long)entered.tv_nsec,
  (unsigned long)(uintptr_t)info,(unsigned long)count);''')
        c_path.write_text(c_text)
        test_text = replace_once(originals[str(paths[1])], '\t\t\tt.Cleanup(func() {', '''\t\t\tt.Cleanup(func() {
\t\t\t\tfmt.Fprintf(os.Stderr,"MINILOOP_END\\t%d\\t%s\\t%s\\n",time.Now().UnixNano(),recipe.Name,sourcePath)''')
        test_text = replace_once(test_text, '\t\t\ttrace.mark(fmt.Sprintf("operation=%s path=%s", recipe.Operation, sourcePath))', '''\t\t\tfmt.Fprintf(os.Stderr,"MINILOOP_OP\\t%d\\t%s\\t%s\\n",time.Now().UnixNano(),recipe.Name,sourcePath)
\t\t\ttrace.mark(fmt.Sprintf("operation=%s path=%s", recipe.Operation, sourcePath))''')
        generated = {str(paths[0]): c_path}
        for original_path, text in [(paths[1], test_text)]:
            target = Path(scratch) / original_path.name
            target.write_text(text)
            subprocess.run(["gofmt", "-w", str(target)], check=True)
            generated[str(original_path)] = target
        overlay = Path(scratch) / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {name: str(p) for name, p in generated.items()}}))
        command = ["go", "test", "-overlay=" + str(overlay), "-v", "-race", "-covermode=atomic", "-coverpkg=./...",
                   "-count=1", "-timeout=180s", "-run=^TestNativeEventsActualSourceRecipes/(ignored-hidden|ignored-backup|ignored-text|transient-source)$", "./reload"]
        log = Path(str(prefix) + ".log")
        began = time.monotonic()
        with log.open("w") as output:
            result = subprocess.run(command, cwd=ROOT / "go", stdout=output, stderr=subprocess.STDOUT)
        report = {"baseline": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
                  "command": command, "exit_code": result.returncode, "seconds": round(time.monotonic()-began,3),
                  "log": str(log), "raw_log": log.read_text(),
                  "classification": classify(log.read_text()),
                  "production_sha256": {str(p.relative_to(ROOT)): hashlib.sha256(originals[str(p)].encode()).hexdigest() for p in paths},
                  "overlay_sources": {str(Path(n).relative_to(ROOT)): p.read_text() for n,p in generated.items()},
                  "production_unchanged": all(p.read_text()==originals[str(p)] for p in paths)}
    assert report["production_unchanged"]
    receipt.write_text(json.dumps(report,indent=2)+'\n')
    print("go", result.returncode, flush=True)


if __name__ == "__main__":
    main()
