"""The micro-experiment vehicle (§5): budgets named before they are spent.

Three properties pinned:

* a real-endpoint run without an explicit task-run budget (or with one
  smaller than the invocation) is refused before any client is built --
  the cost is authorized by naming it, never discovered on the invoice;
* an operator-supplied task module swaps the visible set (the truly blind
  set the in-repo HELDOUT_TASKS honestly cannot be), and the held-out
  second opinion still runs alongside it;
* a malformed task module refuses loudly instead of benchmarking an empty
  set into a hollow not_worse.
"""

import importlib.util
import json
import pathlib

import pytest

REPO = pathlib.Path(__file__).resolve().parent.parent


def _cli():
    spec = importlib.util.spec_from_file_location(
        "paired_benchmark_cli", REPO / "tools" / "paired_benchmark.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_a_real_run_without_an_explicit_budget_is_refused(monkeypatch, capsys):
    cli = _cli()

    refusal = cli.real_run_refusal(True, 12, None)
    assert refusal and "12" in refusal and "refusing" in refusal
    assert "MINILOOP_BENCHMARK_TASK_BUDGET=12" in refusal, (
        "the refusal must say exactly what to state, not just say no"
    )
    assert "budget 6 < 12" in cli.real_run_refusal(True, 12, "6")
    assert "not a number" in cli.real_run_refusal(True, 12, "lots")
    assert cli.real_run_refusal(True, 12, "12") is None
    assert cli.real_run_refusal(False, 12, None) is None, (
        "fake runs are free and never gated"
    )

    # End to end: the refusal happens before any client exists.
    monkeypatch.setenv("MINILOOP_BENCHMARK_REAL", "1")
    monkeypatch.delenv("MINILOOP_BENCHMARK_TASK_BUDGET", raising=False)
    assert cli.main([]) == 2
    assert "refusing" in capsys.readouterr().err


def test_an_operator_task_module_swaps_the_visible_set(
        tmp_path, monkeypatch, capsys):
    cli = _cli()
    monkeypatch.delenv("MINILOOP_BENCHMARK_REAL", raising=False)

    blind = tmp_path / "blind.py"
    blind.write_text(
        "from mini_loop.benchmark import BenchTask\n"
        "TASKS = (BenchTask('op-probe', 'say anything',\n"
        "                   lambda workspace, final: True),)\n"
    )
    assert cli.main(["--tasks", str(blind)]) == 0
    report = json.loads(capsys.readouterr().out)

    assert report["comparison"]["tasks"] == 1, "the visible set was swapped"
    assert {r["task"] for r in report["baseline"]} == {"op-probe"}
    # The second opinion rides along even under an operator set.
    assert report["heldout_comparison"]["tasks"] == 3
    assert report["task_runs"] == 8  # 2 arms x (1 visible + 3 held-out)
    # The behavioral instrument reaches the CLI report too.
    assert report["baseline"][0]["rounds"] >= 1
    assert "dimensions" in report["comparison"]


def test_repeat_multiplies_the_budget_and_aggregates(
        tmp_path, monkeypatch, capsys):
    """--repeat N spends N times the task-runs, and the budget gate counts
    every one of them; the rows come back aggregated with the repeat count
    and pass_rate named."""

    cli = _cli()

    # The gate prices repeats in: default sets (5 visible + 3 held-out)
    # at --repeat 2 cost 32.
    monkeypatch.setenv("MINILOOP_BENCHMARK_REAL", "1")
    monkeypatch.delenv("MINILOOP_BENCHMARK_TASK_BUDGET", raising=False)
    assert cli.main(["--repeat", "2"]) == 2
    assert "32" in capsys.readouterr().err

    monkeypatch.delenv("MINILOOP_BENCHMARK_REAL", raising=False)
    blind = tmp_path / "blind.py"
    blind.write_text(
        "from mini_loop.benchmark import BenchTask\n"
        "TASKS = (BenchTask('op-probe', 'say anything',\n"
        "                   lambda workspace, final: True),)\n"
    )
    assert cli.main(["--tasks", str(blind), "--repeat", "2"]) == 0
    report = json.loads(capsys.readouterr().out)
    assert report["repeat"] == 2
    assert report["task_runs"] == 16  # 2 arms x 2 repeats x (1 + 3)
    assert all(r["repeats"] == 2 for r in report["baseline"])
    assert all("pass_rate" in r for r in report["candidate"])


def test_a_broken_task_module_refuses_loudly(tmp_path):
    cli = _cli()

    empty = tmp_path / "empty.py"
    empty.write_text("TASKS = ()\n")
    with pytest.raises(SystemExit, match="non-empty TASKS"):
        cli.load_task_module(str(empty))

    dupes = tmp_path / "dupes.py"
    dupes.write_text(
        "from mini_loop.benchmark import BenchTask\n"
        "TASKS = (BenchTask('a', 'p', lambda w, f: True),\n"
        "         BenchTask('a', 'q', lambda w, f: True))\n"
    )
    with pytest.raises(SystemExit, match="unique"):
        cli.load_task_module(str(dupes))

    lame = tmp_path / "lame.py"
    lame.write_text(
        "class T:\n"
        "    name = 'x'\n"
        "    prompt = 'y'\n"
        "    expect = None\n"
        "TASKS = (T(),)\n"
    )
    with pytest.raises(SystemExit, match="callable expect"):
        cli.load_task_module(str(lame))
