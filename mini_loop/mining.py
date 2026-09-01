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

__all__ = ["bash_profile", "mine", "mine_trajectory", "model_profile",
           "render", "render_bash", "render_model"]

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


def _started_at(summary: dict) -> float:
    try:
        return float(summary.get("started_at") or 0.0)
    except (TypeError, ValueError):
        return 0.0


def _window(summaries, since: float | None, until: float | None):
    for summary in summaries:
        started = _started_at(summary)
        if since is not None and started < since:
            continue
        if until is not None and started >= until:
            continue
        yield summary


def mine(store: Any, *, session_id: str | None = None,
         limit: int = MAX_TRAJECTORIES, since: float | None = None,
         until: float | None = None) -> dict:
    """Fold the newest recorded trajectories into one friction report.

    `since`/`until` (unix timestamps, half-open window) slice the corpus
    by era, so a landed experiment gets a real before/after reading from
    the same instrument instead of a synthetic one.
    """

    rows = []
    for summary in _window(store.list(session_id=session_id,
                                      limit=max(1, limit)), since, until):
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


def _command_head(command: str) -> str:
    for token in command.split():
        return token
    return "?"


def bash_profile(store: Any, *, session_id: str | None = None,
                 limit: int = MAX_TRAJECTORIES, since: float | None = None,
                 until: float | None = None) -> dict:
    """The shape of recorded bash usage: heads, cwd distrust, repeats.

    The corpus's first profile (2026-09-02) showed 97% of commands
    prefixed with `cd /abs/path &&` -- the model re-establishing its
    working directory on every call because the work it was asked to do
    lived outside the session workspace. That prefix rate is named here
    as cwd_distrust: a high value is a workload/workspace mismatch
    signal, the same root the absolute-path read errors grew from.
    """

    heads: dict[str, int] = {}
    error_heads: dict[str, int] = {}
    repeats: dict[str, int] = {}
    pending: dict[str, str] = {}
    total = cd_prefixed = 0
    for summary in _window(store.list(session_id=session_id,
                                      limit=max(1, limit)), since, until):
        trajectory_id = summary.get("trajectory_id") or summary.get("id")
        if not trajectory_id:
            continue
        seen: dict[str, int] = {}
        for event in store.iter_events(
            trajectory_id, types={"tool_use", "tool_result"}, limit=2_000,
        ):
            name = event.get("name")
            if name != "bash":
                continue
            if event.get("type") == "tool_use":
                inputs = event.get("input")
                command = (str(inputs.get("command", ""))
                           if isinstance(inputs, dict) else "")
                total += 1
                head = _command_head(command)
                heads[head] = heads.get(head, 0) + 1
                if head == "cd":
                    cd_prefixed += 1
                seen[command] = seen.get(command, 0) + 1
                pending[str(event.get("id"))] = head
            else:
                output = str(event.get("output", ""))
                if (output.lstrip().startswith("Error")
                        or "(exit " in output[-24:]):
                    head = pending.get(str(event.get("id")), "?")
                    error_heads[head] = error_heads.get(head, 0) + 1
        for command, count in seen.items():
            if count > 1:
                key = command[:80]
                repeats[key] = repeats.get(key, 0) + count - 1
    return {
        "commands": total,
        "cwd_distrust": round(cd_prefixed / total, 3) if total else 0.0,
        "heads": dict(sorted(heads.items(), key=lambda kv: -kv[1])),
        "error_heads": dict(sorted(error_heads.items(),
                                   key=lambda kv: -kv[1])),
        "repeated_commands": dict(sorted(
            repeats.items(), key=lambda kv: -kv[1])[:MAX_HOTSPOTS]),
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


def model_profile(store: Any, *, session_id: str | None = None,
                  limit: int = MAX_TRAJECTORIES, since: float | None = None,
                  until: float | None = None) -> dict:
    """What the recorded model calls actually cost, from provider counts.

    model_end events carry the provider's own usage numbers -- input,
    output, cache reads, cache creation -- and the stop reason. Folded
    here into the questions that pick experiments: how much prompt is
    served from cache (cache_read_share), how often answers truncate
    (stop max_tokens), and what a call costs end to end.
    """

    from statistics import median

    calls = 0
    stop_reasons: dict[str, int] = {}
    tokens = {"input": 0, "output": 0, "cache_read": 0, "cache_creation": 0}
    durations: list[float] = []
    for summary in _window(store.list(session_id=session_id,
                                      limit=max(1, limit)), since, until):
        trajectory_id = summary.get("trajectory_id") or summary.get("id")
        if not trajectory_id:
            continue
        for event in store.iter_events(
            trajectory_id, types={"model_end"}, limit=2_000,
        ):
            calls += 1
            reason = str(event.get("stop_reason"))
            stop_reasons[reason] = stop_reasons.get(reason, 0) + 1
            usage = event.get("usage")
            if isinstance(usage, dict):
                tokens["input"] += int(usage.get("input_tokens") or 0)
                tokens["output"] += int(usage.get("output_tokens") or 0)
                tokens["cache_read"] += int(
                    usage.get("cache_read_input_tokens") or 0)
                tokens["cache_creation"] += int(
                    usage.get("cache_creation_input_tokens") or 0)
            try:
                durations.append(float(event.get("duration_ms")))
            except (TypeError, ValueError):
                pass
    prompt_total = (tokens["input"] + tokens["cache_read"]
                    + tokens["cache_creation"])
    return {
        "calls": calls,
        "stop_reasons": dict(sorted(stop_reasons.items(),
                                    key=lambda kv: -kv[1])),
        "tokens": tokens,
        "cache_read_share": (round(tokens["cache_read"] / prompt_total, 3)
                             if prompt_total else 0.0),
        "median_call_ms": round(median(durations), 1) if durations else None,
        "truncations": stop_reasons.get("max_tokens", 0),
    }


def render_model(profile: dict) -> str:
    """Bounded text projection of a model-call profile."""

    tokens = profile["tokens"]
    lines = [f"# model profile ({profile['calls']} calls)"]
    lines.append(
        f"prompt tokens: {tokens['input']:,} uncached + "
        f"{tokens['cache_read']:,} cache-read + "
        f"{tokens['cache_creation']:,} cache-write "
        f"(cache_read_share {profile['cache_read_share']:.0%}) | "
        f"output {tokens['output']:,}"
    )
    reasons = ", ".join(f"{k}: {v}" for k, v in profile["stop_reasons"].items())
    lines.append(f"stop reasons: {reasons or 'none'}")
    if profile["truncations"]:
        lines.append(f"TRUNCATIONS: {profile['truncations']} calls stopped "
                     "at max_tokens")
    if profile["median_call_ms"] is not None:
        lines.append(f"median call: {profile['median_call_ms']:,} ms")
    return "\n".join(lines)


def render_bash(profile: dict) -> str:
    """Bounded text projection of a bash usage profile."""

    lines = [f"# bash profile ({profile['commands']} commands)"]
    lines.append(f"cwd_distrust: {profile['cwd_distrust']:.0%} of commands "
                 "re-establish the working directory with a cd prefix")
    lines.append("\n## heads")
    for head, count in list(profile["heads"].items())[:MAX_HOTSPOTS]:
        errors = profile["error_heads"].get(head)
        suffix = f" ({errors} errored)" if errors else ""
        lines.append(f"- {head}: {count}{suffix}")
    if profile["repeated_commands"]:
        lines.append("\n## repeated identical commands (wasted motion)")
        for command, extra in profile["repeated_commands"].items():
            lines.append(f"- {extra}x extra: {command}")
    return "\n".join(lines)


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: a pure read-only fold over TrajectoryStore's "
    "bounded readers; the worst outcome is a report over fewer rows."
)
