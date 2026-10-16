"""Keep production Workflow imports independent of the optional fault prototype."""

from __future__ import annotations

import subprocess
import sys
import textwrap

import pytest

pytest.importorskip("temporalio.contrib.deepagents")

from mini_loop_deepagents.temporal_support.determinism import (
    DeterministicStateBackend,
    StableToolMessageIDs,
)


def test_prototype_compatibility_exports_keep_the_same_adapter_classes():
    from mini_loop_deepagents.temporal_prototype import workflow as prototype

    assert prototype.DeterministicStateBackend is DeterministicStateBackend
    assert prototype.StableToolMessageIDs is StableToolMessageIDs
    assert StableToolMessageIDs("stable-workflow").name == "StableToolMessageIDs"


def test_task_workflows_import_without_loading_the_prototype_profile():
    # A fresh interpreter catches transitive dependencies hidden by pytest's
    # shared module cache. Importing these definitions must not load prototype
    # owner-memory Activities or its configured offline model fixture.
    probe = textwrap.dedent("""\
        import importlib
        import importlib.abc
        import sys

        class NoPrototype(importlib.abc.MetaPathFinder):
            def find_spec(self, fullname, path=None, target=None):
                if fullname.startswith("mini_loop_deepagents.temporal_prototype"):
                    raise AssertionError("production import reached " + fullname)

        sys.meta_path.insert(0, NoPrototype())
        for name in ("runtime", "runtime_v2", "summarization"):
            importlib.import_module("mini_loop_deepagents.task_server." + name)
        assert not any(name.startswith("mini_loop_deepagents.temporal_prototype")
                       for name in sys.modules)
    """)
    completed = subprocess.run(
        [sys.executable, "-c", probe], capture_output=True, text=True, timeout=30,
    )
    assert completed.returncode == 0, completed.stderr
