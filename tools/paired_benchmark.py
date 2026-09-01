"""Run the paired benchmark: baseline arm vs candidate arm.

Fake transport by default (free, deterministic -- exercises the harness,
not the model). A real-endpoint run is the operator's explicit, budgeted
choice, and the budget is stated in task-runs, not vibes:

    MINILOOP_BENCHMARK_REAL=1 MINILOOP_BENCHMARK_TASK_BUDGET=12 \\
        python tools/paired_benchmark.py

The tool refuses a real run whose stated budget does not cover every
task-run the invocation would execute: the cost is named before it is
spent, never discovered on the invoice. This is the vehicle for the §5
micro-experiments (docs/RSI_RESEARCH_AND_PLAN.md) -- tool-ergonomics
changes only show their effect on real model behavior, and the behavioral
dimensions in the comparison are the instrument that shows it.

Arms differ by environment overlays: MINILOOP_BENCH_CANDIDATE_* variables
are applied (with the prefix stripped to MINILOOP_*) to the candidate arm
only, so e.g. MINILOOP_BENCH_CANDIDATE_SUBAGENT_MAX_DEPTH=3 benchmarks a
depth-quota change against the current default.

``--tasks path/to/module.py`` swaps the visible task set for an
operator-supplied module exporting ``TASKS`` (a tuple of BenchTask) --
the truly blind set the in-repo HELDOUT_TASKS honestly cannot be. The
held-out comparison runs either way as the second opinion, and a
regression in EITHER comparison fails the invocation.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import pathlib
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent.parent))

from mini_loop.benchmark import (  # noqa: E402
    DEFAULT_TASKS, HELDOUT_TASKS, aggregate_runs, compare, run_arm,
)
from mini_loop.config import Settings, build_client  # noqa: E402

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _settings(root: pathlib.Path, real: bool) -> Settings:
    return Settings(
        fake_llm=not real,
        workspace_root=root,
        skills_dir=SKILLS,
        spill_dir=None,
    )


def load_task_module(path: str):
    """An operator-supplied task module: must export a non-empty TASKS.

    Each entry needs the BenchTask shape -- a name, a prompt, a callable
    expectation. Validated here so a typo'd module refuses loudly instead
    of benchmarking an empty set and reporting a hollow not_worse.
    """

    import importlib.util

    spec = importlib.util.spec_from_file_location("operator_bench_tasks", path)
    if spec is None or spec.loader is None:
        raise SystemExit(f"cannot load task module: {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    tasks = tuple(getattr(module, "TASKS", ()) or ())
    if not tasks:
        raise SystemExit(f"{path} must export a non-empty TASKS tuple")
    for task in tasks:
        if not (getattr(task, "name", None) and getattr(task, "prompt", None)
                and callable(getattr(task, "expect", None))):
            raise SystemExit(
                f"{path}: every TASKS entry needs name, prompt, and a "
                f"callable expect (got {task!r})"
            )
    names = [task.name for task in tasks]
    if len(set(names)) != len(names):
        raise SystemExit(f"{path}: TASKS names must be unique: {names}")
    return tasks


def real_run_refusal(real: bool, task_runs: int,
                     budget: str | None) -> str | None:
    """Refusal message for an unbudgeted or under-budgeted real run.

    Fake runs are free and never gated. A real run must state, up front,
    a task-run budget that covers everything this invocation will execute
    -- naming the cost is the authorization.
    """

    if not real:
        return None
    if budget is None:
        return (
            f"refusing: a real-endpoint run would execute {task_runs} "
            f"task-runs; state the budget explicitly, e.g. "
            f"MINILOOP_BENCHMARK_TASK_BUDGET={task_runs}"
        )
    try:
        stated = int(budget)
    except ValueError:
        return f"refusing: MINILOOP_BENCHMARK_TASK_BUDGET={budget!r} is not a number"
    if stated < task_runs:
        return (
            f"refusing: budget {stated} < {task_runs} task-runs this "
            f"invocation would execute; raise the budget or trim the task set"
        )
    return None


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--tasks", metavar="MODULE.py", default=None,
        help="operator task module exporting TASKS; replaces the visible set",
    )
    parser.add_argument(
        "--repeat", type=int, default=1, metavar="N",
        help="run each arm N times and aggregate by median/majority -- the "
             "calibration noise floor makes single real runs unjudgeable",
    )
    args = parser.parse_args(argv)
    if args.repeat < 1:
        parser.error("--repeat must be >= 1")

    visible = load_task_module(args.tasks) if args.tasks else DEFAULT_TASKS
    real = os.getenv("MINILOOP_BENCHMARK_REAL") == "1"
    # Two arms, each running the visible set and the held-out second
    # opinion, --repeat times over: every repeat is a spent task-run.
    task_runs = 2 * args.repeat * (len(visible) + len(HELDOUT_TASKS))
    refusal = real_run_refusal(
        real, task_runs, os.getenv("MINILOOP_BENCHMARK_TASK_BUDGET"))
    if refusal:
        print(refusal, file=sys.stderr)
        return 2

    overlay = {
        key.replace("MINILOOP_BENCH_CANDIDATE_", "MINILOOP_", 1): value
        for key, value in os.environ.items()
        if key.startswith("MINILOOP_BENCH_CANDIDATE_")
    }

    async def arm_rows(label, settings, client, tasks):
        runs = [await run_arm(label, settings, client, tasks)
                for _ in range(args.repeat)]
        return runs[0] if args.repeat == 1 else aggregate_runs(runs)

    async def run() -> dict:
        with tempfile.TemporaryDirectory(prefix="bench-base-") as base_dir, \
             tempfile.TemporaryDirectory(prefix="bench-cand-") as cand_dir:
            base_settings = _settings(pathlib.Path(base_dir), real)
            base_client = build_client(base_settings)
            baseline = await arm_rows(
                "baseline", base_settings, base_client, visible)
            heldout_base = await arm_rows(
                "baseline", base_settings, base_client, HELDOUT_TASKS)
            saved = {k: os.environ.get(k) for k in overlay}
            os.environ.update(overlay)
            try:
                cand_settings = _settings(pathlib.Path(cand_dir), real)
            finally:
                for key, value in saved.items():
                    if value is None:
                        os.environ.pop(key, None)
                    else:
                        os.environ[key] = value
            cand_client = build_client(cand_settings)
            candidate = await arm_rows(
                "candidate", cand_settings, cand_client, visible)
            heldout_cand = await arm_rows(
                "candidate", cand_settings, cand_client, HELDOUT_TASKS)
            return {
                "real": real,
                "overlay": sorted(overlay),
                "repeat": args.repeat,
                "task_runs": task_runs,
                "baseline": baseline,
                "candidate": candidate,
                "comparison": compare(baseline, candidate),
                "heldout_comparison": compare(heldout_base, heldout_cand),
            }

    report = asyncio.run(run())
    print(json.dumps(report, indent=2))
    verdicts = (report["comparison"]["verdict"],
                report["heldout_comparison"]["verdict"])
    return 1 if "regression" in verdicts else 0


if __name__ == "__main__":
    raise SystemExit(main())
