"""Repeated A/B trials against the real endpoint.

Rounds 62 and 64 both changed what the agent is told and both measured the
effect with a single run per condition. Round 64's own numbers showed how far
that is from evidence -- 3, 3 against 6, 4 on the same comparison -- and
re-running round 62's claim confirmed it:

    round 62 published : unaware 7 attempts, told 2
    n=5 re-measurement : unaware [3, 2, 2, 1, 2]  median 2.0
                         told    [2, 1, 1, 2, 2]  median 2.0

The 7 was an outlier. A single run of a stochastic system is an anecdote, and
the fix is not to stop measuring but to stop measuring once.

    python tools/ab_trial.py <trial-module> [-n 5]

A trial module defines `CONDITIONS` (label -> setup callable) and an async
`run(agent) -> float` returning the metric. This reports the distribution per
condition and, deliberately, refuses to declare a winner: with samples this
small and this noisy, the honest output is the numbers and their overlap.
"""

from __future__ import annotations

import argparse
import asyncio
import importlib.util
import statistics
import sys
from pathlib import Path


def _load(path: Path):
    # The repo root, so a trial module can import `mini_loop` however it is
    # invoked -- these run from a tools/ subdirectory.
    root = str(Path(__file__).resolve().parent.parent)
    if root not in sys.path:
        sys.path.insert(0, root)
    spec = importlib.util.spec_from_file_location(path.stem, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[path.stem] = module
    spec.loader.exec_module(module)
    return module


def _summary(values: list[float]) -> str:
    if not values:
        return "no successful runs"
    body = (
        f"median {statistics.median(values):.1f}  "
        f"mean {statistics.mean(values):.1f}  "
        f"range {min(values):g}-{max(values):g}"
    )
    if len(values) > 1:
        body += f"  stdev {statistics.stdev(values):.2f}"
    return body


def _overlaps(a: list[float], b: list[float]) -> bool:
    """Do the two observed ranges overlap at all?

    Not a significance test -- with five samples there is no honest one. It is
    the weakest question worth asking, and the answer was yes for both claims
    this tool was written after.
    """

    if not a or not b:
        return True
    return min(a) <= max(b) and min(b) <= max(a)


async def _run(module, repeats: int) -> int:
    results: dict[str, list[float]] = {}
    for label, setup in module.CONDITIONS.items():
        values: list[float] = []
        for index in range(repeats):
            try:
                values.append(float(await module.run(setup)))
            except Exception as error:  # a failed run is data, not a crash
                print(f"  {label} run {index}: {type(error).__name__}: {error}")
        results[label] = values
        print(f"{label:<28} {values}")

    print()
    for label, values in results.items():
        print(f"{label:<28} {_summary(values)}")

    labels = list(results)
    if len(labels) == 2:
        a, b = results[labels[0]], results[labels[1]]
        if _overlaps(a, b):
            print(
                f"\nranges overlap: no difference is demonstrated at n={repeats}. "
                "Report the numbers, not a winner."
            )
        else:
            print(f"\nranges do not overlap at n={repeats}; still not a significance test.")
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("trial", type=Path)
    parser.add_argument("-n", dest="repeats", type=int, default=5)
    args = parser.parse_args(argv)
    module = _load(args.trial)
    return asyncio.run(_run(module, args.repeats))


if __name__ == "__main__":  # pragma: no cover - CLI entry
    raise SystemExit(main())
