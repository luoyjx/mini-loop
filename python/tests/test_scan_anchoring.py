"""Every source scan must notice if it stops looking.

Round 82's actual discovery was not a bug in the harness but a new *shape*: a
refactor removed the syntactic writes from two modules, so the AST scan that
classifies disk sinks silently stopped covering them, and nothing failed. A scan
matching zero files passes forever, gets greener the more it misses, and reads
in review as "checked".

`tools/verify_guards.py` cannot find this. It breaks the code under a test and
asks whether the test notices; a scan pointed at nothing notices nothing to
begin with. So `tools/verify_scans.py` asks the complementary question by
emptying what each scan walks. This file is the thin wrapper that makes it run
without anyone remembering to, plus the two properties the tool needs in order
to be believed at all.
"""

import ast
import pathlib
import subprocess
import sys

import pytest

REPO = pathlib.Path(__file__).resolve().parent.parent
TOOL = REPO / "tools" / "verify_scans.py"


def _tool():
    spec = __import__("importlib.util", fromlist=["util"]).spec_from_file_location(
        "verify_scans", TOOL
    )
    module = __import__("importlib.util", fromlist=["util"]).module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


# -- the tool has to be believable before its verdict means anything -------

def test_the_generated_plugin_is_valid_python():
    """It was not, and every module reported "could not collect" as a result.

    `PLUGIN` is `.format()`-ed into a file. A non-raw template turned the `\\n`
    in it into a real newline, which ended a string literal mid-line and made
    the injected plugin a syntax error -- so nothing ran and every module came
    back empty.
    """

    tool = _tool()
    rendered = tool.PLUGIN.format(names=["PACKAGE"], report="/tmp/x.json")
    ast.parse(rendered)


def test_measuring_nothing_is_a_failure_not_a_shrug():
    """The first version printed a clean bill of health over 33 empty modules.

    That is the exact false green this tool exists to find, in the tool. A
    module that produces no outcome must be reported as unmeasured, not skipped.
    """

    source = TOOL.read_text()
    assert "unmeasured" in source
    assert "return 2" in source, "an unmeasured module must not exit clean"


def test_a_fixture_path_is_not_a_scan():
    """Naming a constant is not walking it.

    The first filter asked only whether a test could *reach* the root constant,
    and reported ninety-odd false positives: a module passing `SKILLS` to
    `Settings(skills_dir=...)` reaches the name without scanning anything.
    """

    tool = _tool()
    probe = REPO / "tests" / "test_context_pressure.py"
    if not probe.exists():                                  # pragma: no cover
        pytest.skip("probe module absent")
    constants = tool._root_constants(probe)
    if not constants:                                       # pragma: no cover
        pytest.skip("probe module has no root constant")
    scanning = [t for t in tool._tests_in(probe)
                if tool._scans_constant(probe, t, constants)]
    assert not scanning, f"fixture paths counted as scans: {scanning}"


# -- the verdict ----------------------------------------------------------

def test_every_scanning_module_is_anchored():
    """Each module with a scan must have one test that fails on an empty scan."""

    result = subprocess.run(
        [sys.executable, str(TOOL)], cwd=REPO, capture_output=True, text=True,
        timeout=900,
    )
    assert result.returncode == 0, (
        "a module's entire scanning surface passes with nothing to scan:\n"
        + result.stdout[-2000:]
    )
