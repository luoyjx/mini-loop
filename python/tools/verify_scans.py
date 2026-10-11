"""Find guards that would not notice if they stopped looking.

`tools/verify_guards.py` asks whether a *test* is load-bearing by breaking the
code under it. This asks a different question about a different failure: whether
a **source scan** is still pointed at anything.

Round 82 is why. `tests/test_write_sites.py` classifies every module that writes
to disk by AST-scanning for write calls. Routing four durable writers through a
shared helper removed their syntactic writes, and `cron.py` and `tasks.py`
silently stopped being classified at all -- an inventory built to stop disk sinks
being missed quietly stopped covering two of them. Nothing failed. The refactor
was mine, it was correct on its own terms, and the guard's coverage shrank as a
side effect.

That is a distinct failure mode from a wrong assertion. A scan matching zero
files passes forever, gets greener the more it misses, and reads in review as
"checked". Several rounds added an ad-hoc `test_the_scan_finds_something` after
being bitten; this asks every scan the same question at once instead, which is
the only way this project has ever stopped finding one thing at a time.

Method, behavioural rather than by reading: point each test module's root
constant at an empty directory and re-run the module. A test that inspects the
scan must fail. One that passes either does not use the scan -- checked
separately, by whether it can reach the constant -- or is vacuous.

    python tools/verify_scans.py            # all scanning test modules
    python tools/verify_scans.py write      # only modules matching a substring
"""

from __future__ import annotations

import argparse
import ast
import json
import pathlib
import subprocess
import sys
import tempfile

REPO = pathlib.Path(__file__).resolve().parent.parent
TESTS = REPO / "tests"

#: Module-level constants that name something on disk a scan walks.
ROOT_NAMES = ("PACKAGE", "SKILLS", "DOCS", "ROOT", "REPO", "SOURCE", "TESTS")

#: Walking a directory. Merely *naming* the constant is not scanning it.
WALK_CALLS = {"glob", "rglob", "iterdir", "walk"}


def _root_constants(path: pathlib.Path) -> list[str]:
    """Module-level `NAME = <path expression>` assignments a scan could walk."""

    found = []
    for node in ast.parse(path.read_text()).body:
        if not isinstance(node, ast.Assign) or len(node.targets) != 1:
            continue
        target = node.targets[0]
        if not isinstance(target, ast.Name) or target.id not in ROOT_NAMES:
            continue
        rendered = ast.unparse(node.value)
        if "Path" in rendered or "parent" in rendered:
            found.append(target.id)
    return found


def _scans_constant(path: pathlib.Path, test: str, constants: list[str]) -> bool:
    """Whether `test` actually *walks* the constant, directly or via a helper.

    The first cut of this asked only whether the test could see the constant,
    and reported 90-odd false positives: a module that passes `SKILLS` to
    `Settings(skills_dir=...)` reaches the name without scanning anything, so
    emptying the directory changes nothing and the test reads as vacuous. A
    fixture path is not a scan. The constant has to be the receiver of a
    directory walk for emptying it to mean anything.
    """

    tree = ast.parse(path.read_text())
    bodies = {
        node.name: node
        for node in tree.body
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef))
    }
    seen: set[str] = set()

    def walk(name: str) -> bool:
        if name in seen or name not in bodies:
            return False
        seen.add(name)
        for sub in ast.walk(bodies[name]):
            if not isinstance(sub, ast.Call):
                continue
            func = sub.func
            if (
                isinstance(func, ast.Attribute)
                and func.attr in WALK_CALLS
                and any(c in ast.unparse(func.value) for c in constants)
            ):
                return True
            called = getattr(func, "id", None)
            if called and walk(called):
                return True
        return False

    return walk(test)


def _tests_in(path: pathlib.Path) -> list[str]:
    return [
        node.name
        for node in ast.parse(path.read_text()).body
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef))
        and node.name.startswith("test_")
    ]


#: Injected before the module under test runs, repointing its roots at an empty
#: directory. A conftest plugin rather than an edit, so nothing on disk changes.
# Raw: the template must reach the generated file with a literal
# backslash-n, not a real newline that ends the string mid-literal.
PLUGIN = r'''
import json, pathlib, tempfile
_EMPTY = pathlib.Path(tempfile.mkdtemp(prefix="empty-scan-"))
_NAMES = {names!r}
_REPORT = pathlib.Path({report!r})

def pytest_collection_modifyitems(session, config, items):
    for item in items:
        module = getattr(item, "module", None)
        if module is None:
            continue
        for name in _NAMES:
            if hasattr(module, name):
                setattr(module, name, _EMPTY)

def pytest_runtest_logreport(report):
    # Recorded here rather than via pytest-reportlog, which is not a dependency
    # of this project and whose absence made every module read as "could not
    # collect" -- while the summary still printed a clean bill of health.
    if report.when != "call":
        return
    with _REPORT.open("a") as fh:
        fh.write(json.dumps({{"test": report.nodeid, "outcome": report.outcome}}) + "\n")
'''


def _run(path: pathlib.Path, constants: list[str]) -> dict[str, bool]:
    """Run `path` with its roots emptied; return {test: passed}."""

    with tempfile.TemporaryDirectory() as work:
        plugin = pathlib.Path(work) / "empty_scan_plugin.py"
        report = pathlib.Path(work) / "report.json"
        plugin.write_text(PLUGIN.format(names=constants, report=str(report)))
        subprocess.run(
            [
                sys.executable, "-m", "pytest", str(path), "-q", "--no-header",
                "-p", "no:cacheprovider", "-p", "empty_scan_plugin",
            ],
            cwd=REPO,
            env={**__import__("os").environ, "PYTHONPATH": work},
            capture_output=True,
            text=True,
        )
        if not report.exists():
            return {}
        outcomes: dict[str, bool] = {}
        for line in report.read_text().splitlines():
            try:
                entry = json.loads(line)
            except json.JSONDecodeError:
                continue
            name = entry["test"].split("::")[-1].split("[")[0]
            outcomes[name] = entry["outcome"] == "passed"
        return outcomes


def main(selector: str | None) -> int:
    modules = sorted(TESTS.glob("test_*.py"))
    vacuous: list[str] = []
    unmeasured: list[str] = []
    checked = 0

    for path in modules:
        if selector and selector not in path.name:
            continue
        constants = _root_constants(path)
        if not constants:
            continue
        scan_tests = [t for t in _tests_in(path) if _scans_constant(path, t, constants)]
        if not scan_tests:
            continue
        checked += len(scan_tests)
        outcomes = _run(path, constants)
        if not outcomes:
            # Reporting this as a shrug and then printing "all guards pass" is
            # the same false green this tool exists to find, so it is a failure.
            unmeasured.append(path.name)
            print(f"  UNMEASURED {path.name}: produced no outcomes")
            continue
        # The honest unit is the module, not the test. A negative assertion
        # ("no module does X") is *always* vacuous on an empty scan -- that is
        # what a negative means -- and is perfectly sound when a companion in
        # the same module asserts the scan found something. What cannot be
        # defended is a module where the whole scanning surface goes green with
        # nothing to scan: that module stops checking and never says so.
        blind = [t for t in scan_tests if outcomes.get(t) is True]
        pinning = [t for t in scan_tests if outcomes.get(t) is False]
        for test in pinning:
            print(f"  pins       {path.name}::{test}")
        for test in blind:
            print(f"  green-blind {path.name}::{test}")
        if blind and not pinning:
            vacuous.append(path.name)
            print(f"  UNGUARDED  {path.name}: every scanning test passes with "
                  f"nothing to scan")

    print()
    if unmeasured:
        print(f"{len(unmeasured)} module(s) produced no result at all -- this tool "
              f"measured nothing for them:")
        for name in unmeasured:
            print(f"  {name}")
        return 2
    if vacuous:
        print(f"{len(vacuous)} module(s) whose entire scanning surface passes "
              f"with nothing to scan:")
        for name in vacuous:
            print(f"  {name}")
        return 1
    print(f"all {checked} scanning guard(s) across every module are anchored: "
          f"each module has at least one test that fails when its scan is emptied.")
    return 0


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("selector", nargs="?", help="substring of a test module name")
    raise SystemExit(main(parser.parse_args().selector))
