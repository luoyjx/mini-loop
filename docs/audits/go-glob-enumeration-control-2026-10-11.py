"""Compare the unchanged Source glob method and Go on one directory.

This isolates the Source method, not Toolset construction or the full exporter.
Controlled iterator orders diagnose truncation inputs; they are not native runs.
"""
import ast
import glob
import hashlib
import json
import subprocess
import tempfile
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

REPO = Path(__file__).resolve().parents[2]


def digest(text):
    return hashlib.sha256(text.encode()).hexdigest()


def run():
    source_path = REPO / "python/mini_loop/tools.py"
    source = source_path.read_text()
    tree = ast.parse(source)
    capped = next(n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name == "capped")
    toolset = next(n for n in tree.body if isinstance(n, ast.ClassDef) and n.name == "Toolset")
    method = next(n for n in toolset.body if isinstance(n, ast.FunctionDef) and n.name == "run_glob")
    cap = next(n for n in tree.body if isinstance(n, ast.Assign) and any(
        isinstance(t, ast.Name) and t.id == "OUTPUT_CAP" for t in n.targets))
    namespace = {"globlib": glob}
    exec(compile(ast.Module(body=[cap, capped, method], type_ignores=[]), str(source_path), "exec"), namespace)
    fixture_path = REPO / "go/testdata/python-glob-tools.json"
    fixture = next(c for c in json.loads(fixture_path.read_text())["cases"] if c["name"] == "glob_truncated")
    runner = '''package main
import (
 "context"
 "fmt"
 "os"
 "github.com/luoyjx/mini-loop/go/protocol"
 "github.com/luoyjx/mini-loop/go/workspace"
)
func main() {
 files, err := workspace.NewFiles(os.Args[1]); if err != nil { panic(err) }
 out, err := files.Glob(context.Background(), protocol.GlobInput{Pattern: "*.txt"}); if err != nil { panic(err) }
 fmt.Print(out)
}
'''
    series = fixture["series"]
    names = [series["prefix"] + f"_{i:04d}.txt" for i in range(series["count"])]
    with tempfile.TemporaryDirectory(prefix="mini-loop-glob-pair-") as directory:
        parent = Path(directory).resolve()
        root = parent / "workspace"
        root.mkdir()
        for name in names:
            (root / name).touch()
        instance = SimpleNamespace(workspace=root)
        native_order = list(glob.iglob("*.txt", root_dir=root, recursive=True))
        actual = namespace["run_glob"](instance, "*.txt")
        go_path = parent / "main.go"
        go_path.write_text(runner)
        command = ["go", "run", str(go_path), str(root)]
        completed = subprocess.run(command, cwd=REPO / "go", text=True, capture_output=True)
        if completed.returncode:
            raise RuntimeError(completed.stderr)
        projected = completed.stdout
        controls = {}
        for label, order in [("ascending", names), ("descending", list(reversed(names)))]:
            with patch.object(glob, "iglob", return_value=iter(order)):
                output = namespace["run_glob"](instance, "*.txt")
            controls[label] = summarize(output)
        assert actual == projected, "same-directory Source/Go output mismatch"
        assert controls["ascending"]["sha256"] != controls["descending"]["sha256"]
        receipt = {
            "baseline": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=REPO, text=True).strip(),
            "source_sha256": digest(source),
            "source_symbols": ["OUTPUT_CAP", "capped", "Toolset.run_glob"],
            "fixture_sha256": hashlib.sha256(fixture_path.read_bytes()).hexdigest(),
            "pattern": "*.txt", "series": series,
            "native_enumeration_indices": [int(n[-8:-4]) for n in native_order],
            "source_native": summarize(actual), "go_native": summarize(projected),
            "source_fixture": summarize(fixture["expected_output"]),
            "source_order_controls": controls,
            "exact_native_match": actual == projected,
            "command": command, "go_exit_code": completed.returncode,
            "go_stderr": completed.stderr,
            "scope": "Darwin same-directory method-isolated Source/Go pair. Controlled Source iterator orders are diagnostic, not native filesystem captures. Toolset construction/full exporter/Linux are unverified.",
            "linux_environment": "Docker daemon unavailable; no Linux result or causal attribution claimed.",
        }
    destination = Path(__file__).with_suffix(".json")
    if destination.exists():
        raise FileExistsError(destination)
    destination.write_text(json.dumps(receipt, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps({"exact_native_match": True, "native": receipt["source_native"],
                      "fixture": receipt["source_fixture"], "controls": controls}, ensure_ascii=False))


def summarize(output):
    lines = output.splitlines()
    return {"sha256": digest(output), "characters": len(output),
            "truncated": lines[-1] == "... (matches truncated)",
            "admitted_indices": [int(n[-8:-4]) for n in lines if n.endswith(".txt")]}


if __name__ == "__main__":
    run()
