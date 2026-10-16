"""Dependency direction and import isolation are architectural contracts."""
from __future__ import annotations

import ast
from pathlib import Path
import subprocess
import sys


MODULE = Path(__file__).resolve().parents[1]


def imports(path: Path, root: Path):
    package = list(path.relative_to(root).with_suffix("").parts[:-1])
    for node in ast.walk(ast.parse(path.read_text(), filename=str(path))):
        if isinstance(node, ast.Import):
            yield from (alias.name for alias in node.names)
        elif isinstance(node, ast.ImportFrom):
            prefix = package[:len(package) - node.level + 1] if node.level else []
            if node.module:
                prefix.extend(node.module.split("."))
            for alias in node.names:
                yield ".".join([*prefix, alias.name])


def test_runtime_never_depends_on_checkout_tooling_or_tests():
    root = MODULE / "src"
    violations = [(str(path.relative_to(root)), dependency)
                  for path in root.rglob("*.py") for dependency in imports(path, root)
                  if dependency.split(".")[0] in {"tooling", "tests"}]
    assert not violations


def test_acceptance_dependencies_point_to_support_not_other_scenarios():
    root = MODULE / "tooling/acceptance"
    violations = [(str(path.relative_to(MODULE)), dependency)
                  for directory in ("support", "scenarios")
                  for path in (root / directory).glob("*.py")
                  for dependency in imports(path, MODULE)
                  if dependency == "tooling.acceptance.scenarios"
                  or dependency.startswith("tooling.acceptance.scenarios.")]
    assert not violations


def test_http_adapter_imports_without_temporal_sdk_or_checkout_tooling():
    code = r'''
import importlib.abc
import sys

class BlockRuntimeDependencies(importlib.abc.MetaPathFinder):
    def find_spec(self, fullname, path=None, target=None):
        if fullname.split('.')[0] in {'temporalio', 'tooling', 'tests'}:
            raise AssertionError('HTTP adapter crossed its dependency boundary: ' + fullname)
        return None

sys.meta_path.insert(0, BlockRuntimeDependencies())
from mini_loop_deepagents.task_server.http import create_app
app = create_app(None, tokens={'example-token-with-enough-entropy': 'owner'})
assert app is not None
assert not any(name == 'temporalio' or name.startswith('temporalio.') for name in sys.modules)
'''
    result = subprocess.run([sys.executable, "-c", code], cwd=MODULE,
                            capture_output=True, text=True, timeout=15)
    assert result.returncode == 0, result.stderr
