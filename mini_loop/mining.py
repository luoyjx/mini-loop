"""Trajectory mining: behavioral metrics over recorded real sessions.

The alignment direction (docs/RSI_RESEARCH_AND_PLAN.md §5) is "align to
recorded friction": the benchmark measures synthetic tasks, but the
trajectories record what actually happened -- every tool call, every
error, every re-read, on real work. This module folds those recordings
into the same behavioral vocabulary the benchmark uses (rounds,
tool_calls, tool_errors, repeated_reads), plus per-tool error rates and
re-read hotspots, so experiment selection can follow observed friction
instead of taste.

Deliberately read-only and bounded: mining must not grow a write
surface, every scan rides TrajectoryStore's own bounded readers, and
the events were masked at capture -- nothing here re-opens that
decision.
"""

from __future__ import annotations

from typing import Any

__all__ = ["mine", "mine_trajectory", "render"]

#: Trajectories examined per mine() call, newest first.
MAX_TRAJECTORIES = 50
#: Hotspot rows kept per section in the rendered report.
MAX_HOTSPOTS = 8


def _is_error(output: object) -> bool:
    return str(output or "").lstrip().startswith(("Error", "Unknown tool"))


def mine_trajectory(store: Any, trajectory_id: str) -> dict:
    """Behavioral metrics for one recorded trajectory."""

    rounds = tool_calls = tool_errors = repeated_reads = 0
    per_tool: dict[str, dict] = {}
    read_paths: dict[str, int] = {}
    for event in store.iter_events(
        trajectory_id,
        types={"model_start", "tool_use", "tool_result"},
    ):
        kind = event.get("type")
        if kind == "model_start":
            rounds += 1
        elif kind == "tool_use":
            tool_calls += 1
            name = str(event.get("name", "?"))
            per_tool.setdefault(name, {"calls": 0, "errors": 0})
            per_tool[name]["calls"] += 1
            if name == "read_file":
                inputs = event.get("input")
                path = (str(inputs.get("path", ""))
                        if isinstance(inputs, dict) else "")
                if path:
                    read_paths[path] = read_paths.get(path, 0) + 1
                    if read_paths[path] > 1:
                        repeated_reads += 1
        elif kind == "tool_result":
            if _is_error(event.get("output")):
                tool_errors += 1
                name = str(event.get("name", "?"))
                per_tool.setdefault(name, {"calls": 0, "errors": 0})
                per_tool[name]["errors"] += 1
    return {
        "trajectory_id": trajectory_id,
        "rounds": rounds,
        "tool_calls": tool_calls,
        "tool_errors": tool_errors,
        "repeated_reads": repeated_reads,
        "per_tool": per_tool,
        "reread_paths": {p: n for p, n in read_paths.items() if n > 1},
    }


def mine(store: Any, *, session_id: str | None = None,
         limit: int = MAX_TRAJECTORIES) -> dict:
    """Fold the newest recorded trajectories into one friction report."""

    rows = []
    for summary in store.list(session_id=session_id, limit=max(1, limit)):
        trajectory_id = summary.get("trajectory_id") or summary.get("id")
        if not trajectory_id:
            continue
        mined = mine_trajectory(store, trajectory_id)
        mined["status"] = summary.get("status")
        mined["duration_ms"] = summary.get("duration_ms")
        rows.append(mined)

    per_tool: dict[str, dict] = {}
    reread: dict[str, int] = {}
    for row in rows:
        for name, counts in row["per_tool"].items():
            bucket = per_tool.setdefault(name, {"calls": 0, "errors": 0})
            bucket["calls"] += counts["calls"]
            bucket["errors"] += counts["errors"]
        for path, count in row["reread_paths"].items():
            reread[path] = reread.get(path, 0) + count - 1
    return {
        "trajectories": len(rows),
        "rows": rows,
        "totals": {
            "rounds": sum(r["rounds"] for r in rows),
            "tool_calls": sum(r["tool_calls"] for r in rows),
            "tool_errors": sum(r["tool_errors"] for r in rows),
            "repeated_reads": sum(r["repeated_reads"] for r in rows),
        },
        "per_tool": per_tool,
        "reread_hotspots": dict(sorted(
            reread.items(), key=lambda kv: -kv[1])[:MAX_HOTSPOTS]),
        "error_hotspots": {
            name: counts for name, counts in sorted(
                per_tool.items(), key=lambda kv: -kv[1]["errors"])
            if counts["errors"]
        },
    }


def render(report: dict) -> str:
    """Bounded text projection of a mining report."""

    lines = [f"# trajectory mining ({report['trajectories']} trajectories)"]
    totals = report["totals"]
    lines.append(
        f"rounds {totals['rounds']} | tool_calls {totals['tool_calls']} | "
        f"tool_errors {totals['tool_errors']} | "
        f"repeated_reads {totals['repeated_reads']}"
    )
    lines.append("\n## per-tool")
    for name, counts in sorted(report["per_tool"].items(),
                               key=lambda kv: -kv[1]["calls"]):
        rate = (f" ({counts['errors']}/{counts['calls']} errors)"
                if counts["errors"] else "")
        lines.append(f"- {name}: {counts['calls']} calls{rate}")
    if report["error_hotspots"]:
        lines.append("\n## error hotspots")
        for name, counts in list(report["error_hotspots"].items())[:MAX_HOTSPOTS]:
            lines.append(f"- {name}: {counts['errors']} errors "
                         f"in {counts['calls']} calls")
    if report["reread_hotspots"]:
        lines.append("\n## re-read hotspots (wasted motion)")
        for path, extra in report["reread_hotspots"].items():
            lines.append(f"- {path}: {extra} redundant read(s)")
    return "\n".join(lines)


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: a pure read-only fold over TrajectoryStore's "
    "bounded readers; the worst outcome is a report over fewer rows."
)
