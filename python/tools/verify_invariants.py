#!/usr/bin/env python3
"""The third verification instrument: every module states its invariant posture.

`verify_guards.py` breaks hardenings and requires a test to notice;
`verify_scans.py` empties what scanners scan and requires them to report;
this one asks a prior question: **has each module even said what its runtime
invariant is?** DeepSeek Harness requires every package to publish an
invariant companion -- and, crucially, its verifier mechanically rejects the
ways such a convention rots: generated placeholder text, empty declarations
without a package-specific explanation, and checks that ignore the failure
reporter.

The convention here (scaled to a single-package harness):

* every module under mini_loop/ (dunder modules excepted) declares exactly
  one module-level string constant:
    RUNTIME_INVARIANT   = "enforced by <symbol>: <what is asserted>"
    NO_RUNTIME_INVARIANT = "No runtime invariant: <why nothing is checkable>"
* a RUNTIME_INVARIANT must name a symbol that exists in that module -- a
  declaration pointing at nothing is the lie this tool exists to catch;
* a NO_RUNTIME_INVARIANT must start with the exact prefix, carry a
  module-specific explanation (minimum length), and no two modules may
  share one -- duplicated text is the signature of generated boilerplate.

Run: .venv/bin/python tools/verify_invariants.py
"""

from __future__ import annotations

import ast
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
PACKAGE = ROOT / "mini_loop"

PREFIX = "No runtime invariant: "
MIN_EXPLANATION = 30
_ENFORCED_BY = re.compile(r"enforced by ([A-Za-z_][\w.]*)")


def _declarations(tree: ast.Module) -> dict[str, str]:
    found: dict[str, str] = {}
    for node in tree.body:
        if not isinstance(node, ast.Assign):
            continue
        for target in node.targets:
            if isinstance(target, ast.Name) and target.id in (
                "RUNTIME_INVARIANT", "NO_RUNTIME_INVARIANT"
            ):
                if isinstance(node.value, ast.Constant) and isinstance(
                    node.value.value, str
                ):
                    found[target.id] = node.value.value
                else:
                    found[target.id] = ""  # non-literal: rejected below
    return found


def _module_names(tree: ast.Module) -> set[str]:
    names: set[str] = set()
    for node in ast.walk(tree):
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
            names.add(node.name)
        elif isinstance(node, ast.Name):
            names.add(node.id)
        elif isinstance(node, ast.Attribute):
            names.add(node.attr)
    return names


def run() -> int:
    modules = sorted(
        path
        for path in PACKAGE.rglob("*.py")
        if not path.name.startswith("__") and "__pycache__" not in path.parts
    )
    problems: list[str] = []
    explanations: dict[str, str] = {}

    for path in modules:
        rel = path.relative_to(ROOT)
        source = path.read_text()
        tree = ast.parse(source)
        declared = _declarations(tree)

        if not declared:
            problems.append(f"{rel}: declares neither RUNTIME_INVARIANT nor NO_RUNTIME_INVARIANT")
            continue
        if len(declared) > 1:
            problems.append(f"{rel}: declares both; pick one")
            continue

        [(kind, text)] = declared.items()
        if not text:
            problems.append(f"{rel}: {kind} must be a string literal")
            continue

        if kind == "NO_RUNTIME_INVARIANT":
            if not text.startswith(PREFIX):
                problems.append(f"{rel}: NO_RUNTIME_INVARIANT must start with {PREFIX!r}")
                continue
            explanation = text[len(PREFIX):].strip()
            if len(explanation) < MIN_EXPLANATION:
                problems.append(
                    f"{rel}: the explanation is {len(explanation)} chars; a "
                    f"module-specific reason needs at least {MIN_EXPLANATION}"
                )
                continue
            twin = explanations.get(explanation)
            if twin:
                problems.append(
                    f"{rel}: explanation duplicates {twin} -- boilerplate is "
                    "not a module-specific reason"
                )
                continue
            explanations[explanation] = str(rel)
        else:
            match = _ENFORCED_BY.search(text)
            if not match:
                problems.append(
                    f"{rel}: RUNTIME_INVARIANT must say 'enforced by <symbol>: ...'"
                )
                continue
            symbol = match.group(1).split(".")[-1]
            if symbol not in _module_names(tree):
                problems.append(
                    f"{rel}: RUNTIME_INVARIANT names {symbol!r}, which does "
                    "not exist in the module -- the declaration points at nothing"
                )

    print(f"{len(modules)} modules checked")
    if problems:
        for problem in problems:
            print(f"  UNDECLARED  {problem}")
        return 1
    print("every module states its invariant posture.")
    return 0


if __name__ == "__main__":
    sys.exit(run())
