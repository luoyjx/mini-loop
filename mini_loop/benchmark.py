"""Paired benchmark: the judgment half of the self-evolution loop (Phase 0).

"Improvement" needs a verdict, not a vibe. The suite and the mutation
guards are the regression half -- they say a change broke nothing they
pin. This is the capability half: the same small task set run through a
baseline arm and a candidate arm, each task judged by an effect check on
the workspace (never by the model grading itself), and the arms compared
pairwise.

Deliberately small and deterministic:

* a task passes when its `expect` predicate holds on (workspace, final
  text) -- observable effects, the auditor rule from the verified loop;
* each arm gets a fresh manager and fresh workspaces, so no state leaks
  between arms and the pairing is by task name, not by ordering;
* the verdict is conservative: `not_worse` requires the candidate to
  pass every task the baseline passed. A tie is not an improvement, and
  a trade (wins one, loses one) is a regression until a human says
  otherwise -- the same fall-toward-the-human every gate here has.

The CLI (tools/paired_benchmark.py) runs this on the fake transport by
default; a real-endpoint run is the operator's explicit, budgeted choice.
"""

from __future__ import annotations

import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable

__all__ = ["BenchTask", "DEFAULT_TASKS", "compare", "run_arm"]


@dataclass(frozen=True, slots=True)
class BenchTask:
    name: str
    prompt: str
    #: Judged on observable effects: (workspace, final_text) -> passed.
    expect: Callable[[Path, str], bool]


def _wrote_greeting(workspace: Path, final: str) -> bool:
    target = workspace / "greeting.txt"
    return target.exists() and "hello" in target.read_text().lower()


def _answered_sum(workspace: Path, final: str) -> bool:
    return "12" in final


def _edited_config(workspace: Path, final: str) -> bool:
    target = workspace / "config.ini"
    return target.exists() and "retries = 3" in target.read_text()


DEFAULT_TASKS: tuple[BenchTask, ...] = (
    BenchTask(
        "write-file",
        "Create a file named greeting.txt containing the word hello.",
        _wrote_greeting,
    ),
    BenchTask(
        "arithmetic",
        "What is 5 + 7? Answer with the number.",
        _answered_sum,
    ),
    BenchTask(
        "edit-config",
        "Create config.ini with a [net] section containing `retries = 3`.",
        _edited_config,
    ),
)


async def run_arm(
    label: str,
    settings: Any,
    client: Any,
    tasks: tuple[BenchTask, ...] = DEFAULT_TASKS,
) -> list[dict]:
    """Run every task in a fresh session under one arm's configuration."""

    from .manager import SessionManager

    manager = SessionManager(settings, client)
    results = []
    for task in tasks:
        session = manager.create()
        started = time.monotonic()
        try:
            final = await session.run(task.prompt)
            error = None
        except Exception as exc:  # an arm that crashes scores a failure, loudly
            final, error = "", f"{type(exc).__name__}: {exc}"
        duration_ms = round((time.monotonic() - started) * 1000, 1)
        try:
            passed = error is None and bool(task.expect(session.workspace, final))
        except Exception as exc:
            passed, error = False, f"expect raised {type(exc).__name__}: {exc}"
        results.append({
            "arm": label,
            "task": task.name,
            "passed": passed,
            "duration_ms": duration_ms,
            "error": error,
        })
    return results


def compare(baseline: list[dict], candidate: list[dict]) -> dict:
    """Pairwise verdict by task name; conservative by construction."""

    base_by_task = {r["task"]: r for r in baseline}
    cand_by_task = {r["task"]: r for r in candidate}
    names = sorted(base_by_task)
    if sorted(cand_by_task) != names:
        raise ValueError("arms ran different task sets; nothing is comparable")
    regressions = [
        name for name in names
        if base_by_task[name]["passed"] and not cand_by_task[name]["passed"]
    ]
    wins = [
        name for name in names
        if not base_by_task[name]["passed"] and cand_by_task[name]["passed"]
    ]
    return {
        "tasks": len(names),
        "baseline_passed": sum(r["passed"] for r in baseline),
        "candidate_passed": sum(r["passed"] for r in candidate),
        "wins": wins,
        "regressions": regressions,
        # Conservative: any regression sinks the candidate, wins do not buy
        # it back. A human weighs trades; the instrument does not.
        "verdict": "regression" if regressions else (
            "improvement" if wins else "not_worse"
        ),
    }


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: a pure fold over per-task results; the effect "
    "checks run against throwaway workspaces and judge artifacts, not "
    "self-reports."
)
