"""The benchmark's own logic, and the boundary of what it can detect.

Sixty-five rounds hardened this harness and nothing checked whether the agent
could still finish a task. `tools/bench.py` does, against the real endpoint, so
what is testable offline is its task definitions and its verifiers -- plus, in
this docstring, the result that determined its design.

Three deliberate harness regressions were injected and **the pass rate did not
move for any of them**:

    OUTPUT_CAP cut 250x          6/6 pass
    keep_tail removed            6/6 pass
    run_bash returns nothing     6/6 pass

A capable agent routes around a damaged harness -- asked for the last line of a
long file it reaches for `tail`, and deprived of a shell entirely it finishes
with the file tools. The cost shows up as effort instead:

    task                healthy          run_bash broken
    read-long-output    2.0 cmds, 4.9s   8.0 cmds, 19.7s
    fix-failing-test    ~3 cmds, 6.8s    9.0 cmds, 22.6s

Pass rate detects impossibility; effort detects degradation. Degradation is what
a hardening round actually risks, so the tool reports both and the docstring
says which to read.
"""

import importlib.util
import pathlib
import sys

import pytest

TOOL = pathlib.Path(__file__).resolve().parent.parent / "tools" / "bench.py"


@pytest.fixture(scope="module")
def bench():
    if not (TOOL.parent.parent / ".env").exists():
        pytest.skip("bench.py loads credentials at import time")
    spec = importlib.util.spec_from_file_location("bench", TOOL)
    module = importlib.util.module_from_spec(spec)
    sys.modules["bench"] = module
    spec.loader.exec_module(module)
    return module


def test_there_are_tasks(bench):
    assert len(bench.TASKS) >= 5
    assert len({task.name for task in bench.TASKS}) == len(bench.TASKS)


def test_every_verifier_rejects_an_untouched_workspace(bench, tmp_path):
    """A verifier that passes on an empty workspace grades nothing."""
    for task in bench.TASKS:
        workspace = tmp_path / task.name
        workspace.mkdir()
        task.setup(workspace)
        assert not task.verify(workspace), f"{task.name} passes without any work"


def test_every_verifier_accepts_the_intended_outcome(bench, tmp_path):
    """The other direction: a verifier nothing can satisfy grades nothing either."""
    solutions = {
        "recall-across-compaction": lambda w: (w / "answer.txt").write_text("eu-west-3\n"),
        "write-file": lambda w: (w / "report.md").write_text("STATUS OK\n"),
        "count-in-log": lambda w: (w / "count.txt").write_text("100\n"),
        "find-in-tree": lambda w: (w / "found.txt").write_text("src/beta/mod.py\n"),
        "fix-failing-test": lambda w: (w / "calc.py").write_text(
            "def add(a, b):\n    return a + b\n\n\ndef multiply(a, b):\n"
            "    return a * b\n"
        ),
        "edit-in-place": lambda w: (w / "calc.py").write_text(
            "def add(a, b):\n    return a - b\n\n\ndef multiply(a, b):\n"
            "    return a * b\n\n\ndef divide(a, b):\n    return a / b\n"
        ),
        "read-long-output": lambda w: (w / "result.txt").write_text(
            "BUILD FAILED: undefined reference to `frobnicate'\n"
        ),
    }
    assert set(solutions) == {task.name for task in bench.TASKS}, (
        "a task was added or renamed without a reference solution"
    )
    for task in bench.TASKS:
        workspace = tmp_path / f"solved-{task.name}"
        workspace.mkdir()
        task.setup(workspace)
        solutions[task.name](workspace)
        assert task.verify(workspace), f"{task.name} rejects a correct solution"


def test_the_fix_task_cannot_be_passed_by_editing_the_test(bench, tmp_path):
    """The obvious wrong answer to "make the tests pass"."""
    task = next(t for t in bench.TASKS if t.name == "fix-failing-test")
    workspace = tmp_path / "cheat"
    workspace.mkdir()
    task.setup(workspace)
    (workspace / "test_calc.py").write_text(
        "from calc import add\n\ndef test_add():\n    assert add(2, 3) == -1\n"
    )
    assert not task.verify(workspace)


def test_the_long_output_task_actually_produces_long_output(bench, tmp_path):
    """It exists to exercise the harness, so it has to be big enough to."""
    from mini_loop.tools import OUTPUT_CAP

    task = next(t for t in bench.TASKS if t.name == "read-long-output")
    workspace = tmp_path / "long"
    workspace.mkdir()
    task.setup(workspace)
    assert len((workspace / "build.log").read_text()) > OUTPUT_CAP


def test_the_compaction_task_is_big_enough_to_trigger_compaction(bench, tmp_path):
    """It exists to exercise compaction, so its bulk has to exceed the threshold.

    A measured run fired 9 compaction events including the `auto` pass that
    replaces the whole transcript. What it does *not* do is detect compaction
    defects -- see the note in `tools/bench.py` for the five attempts and the
    structural reasons. It is a smoke test that a session survives compaction,
    not a guard on compaction's correctness; that lives in
    `tests/test_compaction_composition.py` and `tests/test_transcript_contract.py`.
    """
    from mini_loop.compaction import estimate_tokens

    task = next(t for t in bench.TASKS if t.name == "recall-across-compaction")
    workspace = tmp_path / "compaction"
    workspace.mkdir()
    task.setup(workspace)

    from mini_loop.config import Settings

    bulk = sum(p.stat().st_size for p in workspace.glob("chunk_*.log"))
    threshold = Settings(fake_llm=True).token_threshold
    # `estimate_tokens` is roughly chars/4, and the meter calibrates near 1.0
    # for ASCII, so the bulk has to clear the *shipped* threshold with margin.
    assert bulk / 4 > threshold, (
        f"{bulk:,} bytes is about {bulk // 4:,} tokens against a {threshold:,} "
        "threshold; compaction would never fire under default settings"
    )


def test_the_compaction_tasks_answer_is_only_in_the_transcript(bench, tmp_path):
    """The design point that took four attempts.

    To test that the harness preserves information, the information has to exist
    only in the harness -- the first version put it in `config.txt` and the agent
    simply read the file again.
    """
    task = next(t for t in bench.TASKS if t.name == "recall-across-compaction")
    workspace = tmp_path / "no-crib"
    workspace.mkdir()
    task.setup(workspace)

    assert "eu-west-3" in task.prompt
    on_disk = "".join(p.read_text() for p in workspace.iterdir() if p.is_file())
    assert "eu-west-3" not in on_disk, "the answer is readable from a file"


def test_a_verifier_that_raises_is_a_failure_not_a_crash(bench, tmp_path):
    """A missing file in a verifier must grade as a fail."""
    task = next(t for t in bench.TASKS if t.name == "count-in-log")
    workspace = tmp_path / "empty"
    workspace.mkdir()
    assert not task.verify(workspace)


# --- what the protections cost -------------------------------------------

def test_the_configs_differ_in_the_protections_not_the_task(bench):
    """`--compare` is only meaningful if the configs vary one thing.

    Two batches of six tasks measured hardened 8, 8 commands against bare 8, 9 --
    no detectable difference, with per-task deltas flipping direction between
    batches. Sixty-six rounds of confinement, masking, output caps, truncation
    notices and injected signals cost the agent nothing measurable on this set.
    """
    assert set(bench.CONFIGS) == {"hardened", "bare"}
    hardened, bare = bench.CONFIGS["hardened"], bench.CONFIGS["bare"]
    assert hardened.keys() == bare.keys()
    assert all(hardened[key] and not bare[key] for key in hardened), (
        "the two configs must differ in every protection, or the comparison "
        "attributes a difference to the wrong thing"
    )


def test_every_config_is_runnable(bench):
    """A config nobody can select is a config nobody measures."""
    import inspect

    source = inspect.getsource(bench._attempt)
    assert 'CONFIGS[config]' in source
    for key in bench.CONFIGS["hardened"]:
        assert f'options["{key}"]' in source, f"{key} is declared but never applied"
