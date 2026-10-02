"""Can the agent still do the work?

Sixty-five rounds have hardened this harness -- confinement, masking, capped
output, truncation notices, injected reminders, a bounded context signal -- and
every one of them changes what the agent sees. Nothing checked whether the agent
can still finish a task.

The instruments so far answer narrower questions. Mutation testing asks whether
a guard is load-bearing; coverage asks whether code is exercised; `ab_trial`
asks whether a change moved a behaviour. None of them can fail when a change
quietly makes the agent worse at its job, because they all measure the harness
rather than the work.

    python tools/bench.py [-n 3] [-k substring]

Each task is a workspace, a prompt, and a `verify(workspace) -> bool` that
checks the *outcome on disk* rather than what the agent said about it. Tasks run
`n` times because the thing under test is stochastic, which round 65 established
the hard way.

**Read the effort columns, not the pass rate.** That is the finding this file
was rewritten around. Three deliberate harness regressions were injected and the
pass rate did not move for any of them:

    OUTPUT_CAP cut 250x          6/6 pass
    keep_tail removed            6/6 pass
    run_bash returns nothing     6/6 pass

A capable agent routes *around* a damaged harness. Asked for the last line of a
long file it reaches for `tail`, so an output cap never binds; deprived of a
shell entirely it finishes the same tasks with the file tools. An outcome-only
benchmark measures "agent + harness" and the agent absorbs the damage.

What it absorbs it with is effort, and that signal is loud:

    task                healthy          run_bash broken
    read-long-output    2.0 cmds, 4.9s   8.0 cmds, 19.7s
    fix-failing-test    ~3 cmds, 6.8s    9.0 cmds, 22.6s

So: **pass rate detects impossibility, effort detects degradation**, and
degradation is what hardening rounds actually risk. A change that leaves every
task green while doubling the commands has made the harness worse.
"""

from __future__ import annotations

import argparse
import asyncio
import os
import pathlib
import statistics
import sys
import tempfile
import time
from dataclasses import dataclass
from typing import Callable

ROOT = pathlib.Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

for line in (ROOT / ".env").read_text().splitlines():
    if "=" in line and not line.startswith("#"):
        key, _, value = line.partition("=")
        os.environ.setdefault(key.strip(), value.strip())
os.environ["MINILOOP_FAKE_LLM"] = "0"

from mini_loop import SessionManager                       # noqa: E402
from mini_loop.config import build_client, load_settings   # noqa: E402
from mini_loop.sandbox import SeatbeltSandbox, default_sandbox  # noqa: E402
from mini_loop.secrets import SecretRegistry               # noqa: E402


@dataclass(frozen=True)
class Task:
    name: str
    prompt: str
    setup: Callable[[pathlib.Path], None]
    verify: Callable[[pathlib.Path], bool]


def _nothing(workspace: pathlib.Path) -> None:
    pass


def _seed_logs(workspace: pathlib.Path) -> None:
    (workspace / "app.log").write_text(
        "".join(
            f"{'ERROR' if index % 5 == 0 else 'INFO'} service-{index % 3} step {index}\n"
            for index in range(500)
        )
    )


def _seed_broken(workspace: pathlib.Path) -> None:
    (workspace / "calc.py").write_text(
        "def add(a, b):\n    return a - b\n\n\n"
        "def multiply(a, b):\n    return a * b\n"
    )
    (workspace / "test_calc.py").write_text(
        "from calc import add, multiply\n\n"
        "def test_add():\n    assert add(2, 3) == 5\n\n"
        "def test_multiply():\n    assert multiply(2, 3) == 6\n"
    )


def _seed_long_output(workspace: pathlib.Path) -> None:
    """Long output whose answer is at the very end.

    The first version of this benchmark could not tell a 250x cut in
    `OUTPUT_CAP` from a healthy harness -- every task used `grep -c` or read a
    short file, so nothing depended on output surviving. A benchmark that
    exercises the agent and never the harness cannot fail for the reasons it
    exists to catch.
    """

    (workspace / "build.log").write_text(
        "".join(f"compiling module_{index}.c ... ok\n" for index in range(4000))
        + "BUILD FAILED: undefined reference to `frobnicate'\n"
    )


def _seed_across_compaction(workspace: pathlib.Path) -> None:
    """A fact stated early, then enough bulk to push past the threshold.

    Compaction is the most invasive thing this harness does -- it rewrites the
    agent's own history: `microcompact` blanks older tool results, `snip_compact`
    replaces the middle, `DefaultCompactor.compact` replaces everything with a
    summary. It has been changed in rounds 27, 32, 50, 52 and 64, and no task
    here ever triggered it.

    The answer depends on a value the agent can only have seen *before* the
    rewrite, so if compaction drops it the task fails rather than merely costing
    more.
    """

    # This task verifies that compaction *fires* and the session survives it --
    # 9 compaction events in a measured run, including the `auto` pass that
    # replaces the whole transcript with a summary. It does **not** detect
    # compaction defects, and five attempts to make it establish why:
    #
    #   1. put the fact in a file  -> the agent re-reads the file (round 66's
    #      "a capable agent routes around damage", again)
    #   2. put it in the prompt    -> `snip_compact` preserves the head by
    #      design, which is exactly where a task's instructions live
    #   3. inject round 32's empty-summary defect -> still passes, for 2
    #   4. inject a pair-splitting snip -> `snip_compact` needs >50 messages and
    #      the auto pass fires first, so the broken path never runs
    #
    # The information most worth protecting is the information the design
    # protects hardest, which makes an outcome test of compaction structurally
    # hard to make discriminating. What actually guards compaction is unit-level:
    # `tests/test_compaction_composition.py`, `tests/test_transcript_contract.py`
    # and six mutation-verified guards.
    #
    # Deliberately *no* file holding the answer. The first version put the
    # region in config.txt and the task passed even with round 32's
    # empty-summary defect injected -- the agent simply read the file again.
    # To test that the harness preserves information, the information has to
    # exist only in the harness, so the region is stated in the prompt and the
    # transcript is its only copy.
    # Sized against the *default* threshold, not a lowered one. The first
    # version wrote 335 KB, which fires compaction at a 40k threshold but not at
    # the shipped 100k -- so the task would have claimed to exercise compaction
    # while never reaching it. An offline test now pins the bulk.
    for index in range(20):
        (workspace / f"chunk_{index:02d}.log").write_text(
            "".join(f"record {index}-{line} payload data here\n" for line in range(1400))
        )


def _seed_nested(workspace: pathlib.Path) -> None:
    for part in ("alpha", "beta", "gamma"):
        directory = workspace / "src" / part
        directory.mkdir(parents=True, exist_ok=True)
        (directory / "mod.py").write_text(f"VALUE = '{part}'\n")
    (workspace / "src" / "beta" / "mod.py").write_text(
        "VALUE = 'beta'\nSECRET_MARKER = 'find-me-here'\n"
    )


TASKS = [
    Task(
        "recall-across-compaction",
        "Remember this deploy region: eu-west-3. Do not write it down anywhere "
        "until the end. Now read every chunk_*.log file in turn, one `cat` per "
        "file, and count how many records each contains. When you have read all "
        "twenty, write a file answer.txt containing exactly the deploy region I "
        "gave you at the start.",
        _seed_across_compaction,
        lambda w: (w / "answer.txt").exists()
        and "eu-west-3" in (w / "answer.txt").read_text(),
    ),
    Task(
        "read-long-output",
        "Run `cat build.log` and write the final line of that output -- the one "
        "reporting the outcome -- into result.txt.",
        _seed_long_output,
        lambda w: (w / "result.txt").exists()
        and "BUILD FAILED" in (w / "result.txt").read_text(),
    ),
    Task(
        "write-file",
        "Create a file called report.md containing exactly the line: STATUS OK",
        _nothing,
        lambda w: (w / "report.md").exists()
        and "STATUS OK" in (w / "report.md").read_text(),
    ),
    Task(
        "count-in-log",
        "app.log is in your workspace. Count the ERROR lines and write just that "
        "number into a file called count.txt.",
        _seed_logs,
        lambda w: (w / "count.txt").exists()
        and (w / "count.txt").read_text().strip() == "100",
    ),
    Task(
        "find-in-tree",
        "Somewhere under src/ there is a file containing SECRET_MARKER. Write the "
        "path of that file, relative to your workspace, into found.txt.",
        _seed_nested,
        lambda w: (w / "found.txt").exists()
        and "beta/mod.py" in (w / "found.txt").read_text(),
    ),
    Task(
        "fix-failing-test",
        "Run the tests with `python -m pytest -q`. One fails. Fix the source "
        "(not the test) so both pass.",
        _seed_broken,
        lambda w: "return a + b" in (w / "calc.py").read_text()
        and "assert add(2, 3) == 5" in (w / "test_calc.py").read_text(),
    ),
    Task(
        "edit-in-place",
        "calc.py has a multiply function. Add a `divide(a, b)` function to it "
        "that returns a / b. Do not change the existing functions.",
        _seed_broken,
        lambda w: "def divide" in (w / "calc.py").read_text()
        and "def multiply" in (w / "calc.py").read_text(),
    ),
]


#: Configurations to measure. "bare" is the harness before any of the
#: protections; "hardened" is what a real deployment should run. The point is to
#: know what the protections cost, not to assume it is nothing.
CONFIGS = {
    "hardened": dict(secrets=True, sandbox=True),
    "bare": dict(secrets=False, sandbox=False),
}


async def _attempt(task: Task, config: str = "hardened") -> tuple[bool, float, int]:
    root = pathlib.Path(tempfile.mkdtemp())
    settings = load_settings()
    object.__setattr__(settings, "workspace_root", root / "ws")
    options = CONFIGS[config]
    manager = SessionManager(
        settings, build_client(settings),
        secrets=SecretRegistry.from_environ() if options["secrets"] else None,
        sandbox=(default_sandbox(root / "ws")
                 if options["sandbox"] and SeatbeltSandbox.available() else None),
    )
    session = manager.create()
    agent = session.agent
    agent.workspace.mkdir(parents=True, exist_ok=True)
    task.setup(agent.workspace)

    calls: list[str] = []
    real = agent.toolset.run_bash
    agent.toolset.run_bash = lambda command: (calls.append(command), real(command))[1]

    started = time.monotonic()
    await agent.run(task.prompt)
    elapsed = time.monotonic() - started
    try:
        passed = bool(task.verify(agent.workspace))
    except Exception:
        passed = False
    return passed, elapsed, len(calls)


async def _run(repeats: int, selector: str | None, config: str = "hardened") -> int:
    chosen = [t for t in TASKS if not selector or selector in t.name]
    if not chosen:
        print(f"no task matches {selector!r}")
        return 2

    failures = 0
    # Outcome alone is not enough. A capable agent routes *around* a damaged
    # harness -- asked for the last line of a long file it reaches for `tail`,
    # so a 250x cut in `OUTPUT_CAP` left every task passing. What a degraded
    # harness costs is effort, so shell calls are reported beside the result.
    print(f"config: {config}")
    print(f"{'task':<18} {'pass rate':>10} {'median s':>9} {'cmds':>7}  outcomes")
    print("-" * 70)
    for task in chosen:
        outcomes, times, efforts = [], [], []
        for _ in range(repeats):
            try:
                passed, elapsed, effort = await _attempt(task, config)
            except Exception as error:
                print(f"  {task.name}: {type(error).__name__}: {error}")
                passed, elapsed, effort = False, 0.0, 0
            outcomes.append(passed)
            times.append(elapsed)
            efforts.append(effort)
        rate = sum(outcomes) / len(outcomes)
        if rate < 1.0:
            failures += 1
        marks = "".join("." if o else "x" for o in outcomes)
        print(f"{task.name:<18} {rate:>9.0%} {statistics.median(times):>9.1f} "
              f"{statistics.median(efforts):>7.1f}  {marks}")

    print(f"\n{len(chosen) - failures}/{len(chosen)} tasks passed every attempt.")
    return 1 if failures else 0


async def _compare(repeats: int, selector: str | None) -> int:
    """Run every config and report the difference, without declaring a winner.

    What the protections cost is a number an operator should have, and one this
    project never produced in sixty-six rounds of adding them. Round 65's rule
    applies: with samples this small the honest output is both columns and their
    overlap, not a verdict.
    """

    chosen = [t for t in TASKS if not selector or selector in t.name]
    totals: dict[str, list[int]] = {name: [] for name in CONFIGS}
    print(f"{'task':<18} " + "".join(f"{name:>22}" for name in CONFIGS))
    print("-" * (18 + 22 * len(CONFIGS)))
    for task in chosen:
        cells = []
        for config in CONFIGS:
            efforts, times, ok = [], [], True
            for _ in range(repeats):
                try:
                    passed, elapsed, effort = await _attempt(task, config)
                except Exception:
                    passed, elapsed, effort = False, 0.0, 0
                ok = ok and passed
                efforts.append(effort)
                times.append(elapsed)
            totals[config].append(int(statistics.median(efforts)))
            flag = "" if ok else " FAIL"
            cells.append(f"{statistics.median(efforts):>10.1f} cmds{flag:<6}")
        print(f"{task.name:<18} " + "".join(f"{c:>22}" for c in cells))

    print()
    for config, values in totals.items():
        print(f"{config:<18} total {sum(values):>3} commands across {len(values)} tasks")
    names = list(totals)
    if len(names) == 2:
        left, right = totals[names[0]], totals[names[1]]
        delta = sum(left) - sum(right)
        print(
            f"\ndifference: {delta:+d} commands. Round 65 measured a single "
            "condition varying by more than this between batches, so treat a "
            "small delta as no difference."
        )
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("-n", dest="repeats", type=int, default=3)
    parser.add_argument("-k", dest="selector", default=None)
    parser.add_argument("--config", default="hardened", choices=sorted(CONFIGS))
    parser.add_argument("--compare", action="store_true",
                        help="run every config and report the difference")
    args = parser.parse_args(argv)
    if args.compare:
        return asyncio.run(_compare(args.repeats, args.selector))
    return asyncio.run(_run(args.repeats, args.selector, args.config))


if __name__ == "__main__":  # pragma: no cover - CLI entry
    raise SystemExit(main())
