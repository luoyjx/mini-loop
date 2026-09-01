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

__all__ = ["BenchTask", "DEFAULT_TASKS", "HELDOUT_TASKS", "aggregate_runs",
           "compare", "run_arm"]


@dataclass(frozen=True, slots=True)
class BenchTask:
    name: str
    prompt: str
    #: Judged on observable effects: (workspace, final_text) -> passed.
    expect: Callable[[Path, str], bool]
    #: Optional workspace pre-seeding, run host-side before the session
    #: starts (judge-side mechanism, human-admitted 2026-09-01): some
    #: admitted tasks need fixtures -- a long log to page through -- and
    #: the fixture must come from the instrument, never from the arm's
    #: own conversation.
    setup: Callable[[Path], None] | None = None


def _wrote_greeting(workspace: Path, final: str) -> bool:
    target = workspace / "greeting.txt"
    return target.exists() and "hello" in target.read_text().lower()


def _answered_sum(workspace: Path, final: str) -> bool:
    return "12" in final


def _edited_config(workspace: Path, final: str) -> bool:
    target = workspace / "config.ini"
    return target.exists() and "retries = 3" in target.read_text()


def _seed_long_log(workspace: Path) -> None:
    lines = [
        f"line-{index:05d} value token-{index:05d} {'x' * 24}"
        for index in range(1, 6_001)
    ]
    (workspace / "data.log").write_text("\n".join(lines) + "\n")


def _found_deep_line(workspace: Path, final: str) -> bool:
    return "token-04321" in final


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
    # Admitted 2026-09-01 (human-approved judge-side change): the first
    # task that exercises paging through a file too large to read whole,
    # so tool-ergonomics experiments (offset notices, truncation
    # guidance) have a behavioral read on the benchmark instead of being
    # structurally unmeasurable.
    BenchTask(
        "page-long-log",
        "data.log has 6000 numbered lines and is too large to read in "
        "one go. Report the exact token-NNNNN value that appears on "
        "line 4321.",
        _found_deep_line,
        setup=_seed_long_log,
    ),
)


def _appended_log(workspace: Path, final: str) -> bool:
    target = workspace / "notes.log"
    return target.exists() and len(target.read_text().splitlines()) >= 3


def _counted_words(workspace: Path, final: str) -> bool:
    return "5" in final


def _made_nested(workspace: Path, final: str) -> bool:
    return (workspace / "src" / "app" / "main.txt").exists()


#: The promotion gate's second opinion (practice #10): tasks that are NOT
#: part of the visible optimization loop, run before a merge to catch a
#: candidate that overfit the tasks it was tuned against. Honesty about
#: blindness: these live in the repo, so a proposal editing this checkout
#: CAN read them -- they are held out of the loop, not hidden from the
#: model. A truly blind set is operator-supplied at the terminal
#: (tools/paired_benchmark.py accepts its own task module).
HELDOUT_TASKS: tuple[BenchTask, ...] = (
    BenchTask(
        "append-log",
        "Create notes.log containing three lines: alpha, beta, gamma.",
        _appended_log,
    ),
    BenchTask(
        "word-count",
        "How many words are in 'the quick brown fox jumps'? Answer with "
        "the number.",
        _counted_words,
    ),
    BenchTask(
        "nested-file",
        "Create the file src/app/main.txt containing ok.",
        _made_nested,
    ),
)


def _behavioral_metrics(messages: list) -> dict:
    """Wasted-motion metrics, derived from the arm's own transcript.

    The verdict judges effects; these judge nothing. They measure the shape
    of the path the arm took to its effects -- provider round-trips, tool
    calls, reads of a path already read, tool calls that came back as
    errors -- so a tool-ergonomics experiment (a truncation notice reworded,
    a default changed) has an instrument sensitive enough to show its
    effect. Deterministic on both transports: the fake conversation burns
    rounds and repeats reads exactly like a real one.
    """

    from .blocks import block_field, block_text

    rounds = tool_calls = tool_errors = repeated_reads = 0
    read_paths: set[str] = set()
    for message in messages or []:
        if not isinstance(message, dict):
            continue
        role = message.get("role")
        if role == "assistant":
            rounds += 1
        content = message.get("content")
        if not isinstance(content, list):
            continue
        for block in content:
            kind = block_field(block, "type", "")
            if role == "assistant" and kind == "tool_use":
                tool_calls += 1
                if block_field(block, "name") == "read_file":
                    inputs = block_field(block, "input") or {}
                    path = (
                        str(inputs.get("path", ""))
                        if isinstance(inputs, dict) else ""
                    )
                    if path and path in read_paths:
                        repeated_reads += 1
                    if path:
                        read_paths.add(path)
            elif kind == "tool_result":
                result = block_field(block, "content")
                text = result if isinstance(result, str) else block_text(result)
                if text.lstrip().startswith("Error"):
                    tool_errors += 1
    return {"rounds": rounds, "tool_calls": tool_calls,
            "tool_errors": tool_errors, "repeated_reads": repeated_reads}


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
        if task.setup is not None:
            task.setup(session.workspace)
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
        # Cost proxy, deterministic on both transports: how much context the
        # arm burned to get here. Fake-transport token counts are fabricated;
        # the transcript estimate is real either way.
        from .agent import estimate_tokens

        agent = session.agent
        results.append({
            "arm": label,
            "task": task.name,
            "passed": passed,
            "duration_ms": duration_ms,
            "context_tokens_estimate": (
                estimate_tokens(agent.messages) if agent is not None else 0
            ),
            **_behavioral_metrics(agent.messages if agent is not None else []),
            "error": error,
        })
    return results


#: Dimensions reported beyond correctness (SICA's multi-dimensional
#: utility, practice #8). They inform, they never judge: folding cost into
#: the verdict would make the utility formula itself a gaming surface, so
#: the verdict stays anchored on observable effects and the human weighs
#: the trade the warnings name. The behavioral four are transcript-derived
#: (`_behavioral_metrics`): they exist so small tool-ergonomics changes
#: have an instrument sensitive enough to register.
DIMENSIONS = (
    "duration_ms",
    "context_tokens_estimate",
    "rounds",
    "tool_calls",
    "tool_errors",
    "repeated_reads",
)

#: A candidate worse than baseline by more than this on any dimension gets
#: a named warning in the comparison.
DIMENSION_WARN_PCT = 25.0


def _dimension_fold(baseline: list[dict], candidate: list[dict]):
    dims: dict[str, dict] = {}
    warnings: list[str] = []
    for dim in DIMENSIONS:
        base_total = sum(r.get(dim) or 0 for r in baseline)
        cand_total = sum(r.get(dim) or 0 for r in candidate)
        if base_total <= 0 and cand_total <= 0:
            continue  # rows without the dimension: nothing to report
        delta_pct = (
            round((cand_total - base_total) * 100.0 / base_total, 1)
            if base_total > 0 else None
        )
        dims[dim] = {"baseline": base_total, "candidate": cand_total,
                     "delta_pct": delta_pct}
        if delta_pct is not None and delta_pct > DIMENSION_WARN_PCT:
            warnings.append(f"{dim} worsened {delta_pct}%")
    return dims, warnings


def aggregate_runs(runs: list[list[dict]]) -> list[dict]:
    """Median-of-N aggregation for repeated arm runs.

    The real-transport calibration (§5, 2026-08-31) showed identical arms
    drifting up to 33% on small-integer dimensions -- single runs cannot
    carry a verdict on a micro-experiment. Dimensions take the median
    across repeats; `passed` takes the strict majority (a tie fails),
    with the raw pass_rate reported alongside so a flaky 2-of-3 stays
    visible, never laundered into a clean pass.
    """

    from statistics import median

    by_task: dict[str, list[dict]] = {}
    for run in runs:
        for row in run:
            by_task.setdefault(row["task"], []).append(row)
    aggregated = []
    for task, rows in by_task.items():
        passes = sum(1 for row in rows if row["passed"])
        agg = {
            "arm": rows[0]["arm"],
            "task": task,
            "passed": 2 * passes > len(rows),
            "pass_rate": round(passes / len(rows), 3),
            "repeats": len(rows),
            "error": next(
                (row.get("error") for row in rows if row.get("error")), None),
        }
        for dim in DIMENSIONS:
            values = [row[dim] for row in rows
                      if isinstance(row.get(dim), (int, float))]
            if values:
                agg[dim] = median(values)
        aggregated.append(agg)
    return aggregated


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
    dims, dimension_warnings = _dimension_fold(baseline, candidate)
    return {
        "tasks": len(names),
        "baseline_passed": sum(r["passed"] for r in baseline),
        "candidate_passed": sum(r["passed"] for r in candidate),
        "wins": wins,
        "regressions": regressions,
        "dimensions": dims,
        "dimension_warnings": dimension_warnings,
        # Conservative: any regression sinks the candidate, wins do not buy
        # it back. A human weighs trades; the instrument does not -- and the
        # cost/latency dimensions above inform that weighing without ever
        # entering this verdict.
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
