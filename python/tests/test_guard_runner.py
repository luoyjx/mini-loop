"""A broken test invocation must not count as evidence for a guard."""
import importlib.util
from pathlib import Path
import sys

import pytest


@pytest.fixture
def runner():
    path = Path(__file__).resolve().parents[1] / "tools" / "verify_guards.py"
    spec = importlib.util.spec_from_file_location("guard_runner", path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    try:
        spec.loader.exec_module(module)
        yield module
    finally:
        sys.modules.pop(spec.name, None)


@pytest.mark.parametrize("code,body,status", [
    (0, "<testcase/>", "passed"),
    (1, "<testcase><failure/></testcase>", "caught"),
    (1, "<testcase><error/></testcase>", "invalid"),
    (2, "<testcase><failure/></testcase>", "invalid"),
    (0, "<testcase><skipped/></testcase>", "invalid"),
    (0, "", "invalid"),
    (0, "<testcase><failure/></testcase>", "invalid"),
    (1, "<testcase/>", "invalid"),
])
def test_report_classification(runner, tmp_path, code, body, status):
    report = tmp_path / "report.xml"
    report.write_text(f"<testsuites><testsuite>{body}</testsuite></testsuites>")
    assert runner._classify(code, report).status == status


def test_missing_or_broken_report_is_invalid(runner, tmp_path):
    report = tmp_path / "report.xml"
    assert runner._classify(1, report).status == "invalid"
    report.write_text("<broken")
    assert runner._classify(1, report).status == "invalid"


@pytest.mark.parametrize("replacement,test,expected", [
    ("return True", "assert not allowed(-1)", "caught"),
    ("return value < 0", "assert allowed(1)", "caught"),
    ("return True", "assert allowed(1)", "SURVIVED"),
    ("return True", "assert False", "unmodified baseline"),
    ("this is invalid syntax", "assert not allowed(-1)", "INVALID"),
])
def test_actual_pytest_baseline_and_mutant(runner, monkeypatch, tmp_path, capsys,
                                         replacement, test, expected):
    (tmp_path / "sample.py").write_text("def allowed(value):\n    return value > 0\n")
    (tmp_path / "test_sample.py").write_text(f"from sample import allowed\ndef test_guard():\n    {test}\n")
    monkeypatch.setattr(runner, "ROOT", tmp_path)
    monkeypatch.setattr(runner, "MUTATIONS", [runner.Mutation(
        "sample", 1, "sample.py", "return value > 0", replacement,
        "test_sample.py::test_guard", "reject negative inputs")])
    result = runner._run(None)
    assert result == (0 if expected == "caught" else 1)
    assert expected in capsys.readouterr().out
    assert (tmp_path / "sample.py").read_text() == "def allowed(value):\n    return value > 0\n"


def test_all_python_mutants_have_valid_syntax(runner):
    assert runner.MUTATIONS, "mutation inventory must not be empty"
    sources = {}
    for mutation in runner.MUTATIONS:
        if not mutation.file.endswith(".py"):
            continue
        if mutation.file not in sources:
            sources[mutation.file] = (runner.ROOT / mutation.file).read_text()
        source = sources[mutation.file]
        assert source.count(mutation.old) == 1, mutation.name
        try:
            compile(source.replace(mutation.old, mutation.new, 1), mutation.file, "exec")
        except SyntaxError as error:
            pytest.fail(f"{mutation.name}: invalid mutation syntax: {error}")


def test_all_guard_selectors_can_be_collected(runner):
    assert runner.MUTATIONS, "mutation inventory must not be empty"
    selectors = sorted({mutation.test for mutation in runner.MUTATIONS})
    result = runner.subprocess.run(
        [sys.executable, "-m", "pytest", "--collect-only", "-q", *selectors],
        cwd=runner.ROOT, capture_output=True, text=True, timeout=30,
        env={**runner.os.environ, "PYTHONDONTWRITEBYTECODE": "1"},
    )
    assert result.returncode == 0, (result.stdout + result.stderr)[-4000:]


def test_missing_selector_is_invalid(runner, tmp_path):
    (tmp_path / "test_sample.py").write_text("def test_existing(): pass\n")
    result = runner._pytest(tmp_path, "test_sample.py::test_removed", tmp_path / "report.xml")
    assert result.status == "invalid"
