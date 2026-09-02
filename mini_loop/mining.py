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

import re
from pathlib import Path
from typing import Any

__all__ = ["bash_profile", "era_table", "mine", "mine_trajectory",
           "model_profile", "refusal_profile", "render", "render_bash",
           "render_eras", "render_model", "render_refusals", "render_time",
           "time_profile"]

#: Trajectories examined per mine() call, newest first.
MAX_TRAJECTORIES = 50
#: Hotspot rows kept per section in the rendered report.
MAX_HOTSPOTS = 8


def _is_error(output: object) -> bool:
    # One vocabulary with the benchmark and the tool renderer: a command
    # that ended "(exit N)" failed in the model's eyes even though its
    # output began with whatever the command printed (16 of the corpus's
    # first 1,176 bash results, all invisible to the prefix-only rule).
    from .tools import is_failed_result

    return is_failed_result(output)


def mine_trajectory(store: Any, trajectory_id: str) -> dict:
    """Behavioral metrics for one recorded trajectory."""

    rounds = tool_calls = tool_errors = repeated_reads = 0
    per_tool: dict[str, dict] = {}
    read_paths: dict[str, int] = {}
    # A repeat is the same WINDOW (path, offset, limit) asked for twice;
    # a new offset on the same path is paging, not waste. Same rule as
    # benchmark._read_window, so the miner and the bench count one thing.
    seen_windows: dict[tuple, int] = {}
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
                    window = (path, inputs.get("offset"), inputs.get("limit"))
                    seen_windows[window] = seen_windows.get(window, 0) + 1
                    read_paths.setdefault(path, 1)
                    if seen_windows[window] > 1:
                        repeated_reads += 1
                        read_paths[path] += 1
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


def _window(summaries, since: float | None, until: float | None,
            build: str | None = None):
    """Slice the corpus by era: a clock window and/or a build prefix.

    The clock alone cannot tell which code a trajectory ran on -- a server
    started before an edit keeps answering with the old code, and one run
    with reload picks up working-tree edits mid-experiment (both observed).
    `build` matches the build_id recorded on the trajectory header, so an
    experiment's before/after is read against the code, not the wall clock.
    """
    for summary in summaries:
        started = _started_at(summary)
        if since is not None and started < since:
            continue
        if until is not None and started >= until:
            continue
        if build and not str(summary.get("build") or "").startswith(build):
            continue
        yield summary


def mine(store: Any, *, session_id: str | None = None,
         limit: int = MAX_TRAJECTORIES, since: float | None = None,
         until: float | None = None, build: str | None = None) -> dict:
    """Fold the newest recorded trajectories into one friction report.

    `since`/`until` (unix timestamps, half-open window) slice the corpus
    by era, so a landed experiment gets a real before/after reading from
    the same instrument instead of a synthetic one.
    """

    rows = []
    for summary in _window(store.list(session_id=session_id,
                                      limit=max(1, limit)), since, until, build):
        trajectory_id = summary.get("trajectory_id") or summary.get("id")
        if not trajectory_id:
            continue
        mined = mine_trajectory(store, trajectory_id)
        mined["status"] = summary.get("status")
        mined["duration_ms"] = summary.get("duration_ms")
        mined["build"] = summary.get("build")
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
    builds: dict[str, int] = {}
    for row in rows:
        key = str(row.get("build") or "(unrecorded)")
        builds[key] = builds.get(key, 0) + 1
    return {
        "trajectories": len(rows),
        # The era composition: which builds the rows ran on. A reading that
        # mixes builds is a reading of nothing in particular.
        "builds": dict(sorted(builds.items(), key=lambda kv: -kv[1])),
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


def _cd_target(command: str) -> str:
    tokens = command.split()
    return tokens[1].strip("\"'") if len(tokens) > 1 else ""


def _cd_class(command: str, workspace: str | None) -> str:
    """Where a `cd`-prefixed command goes, relative to the session workspace.

    "home": back into the workspace it was already in -- pure cwd distrust,
    the model not trusting that the shell starts there. "foreign": somewhere
    else -- the work lives outside the workspace (the mismatch that
    workspace binding exists to remove). "unknown": no recorded workspace or
    an unparseable target. Two levers, two gauges: a clearer cwd contract
    should move "home"; binding should move "foreign".
    """
    target = _cd_target(command)
    if not target or target == "-":
        return "unknown"
    if target.startswith("~") or target == "/":
        return "foreign"
    if not target.startswith("/"):
        return "foreign" if target.startswith("..") else "home"
    if not workspace:
        return "unknown"
    try:
        home = Path(workspace)
        there = Path(target)
    except (TypeError, ValueError):
        return "unknown"
    return "home" if there == home or there.is_relative_to(home) else "foreign"


def bash_profile(store: Any, *, session_id: str | None = None,
                 limit: int = MAX_TRAJECTORIES, since: float | None = None,
                 until: float | None = None,
                 build: str | None = None) -> dict:
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
    cd_classes: dict[str, int] = {}
    foreign: dict[str, int] = {}
    total = cd_prefixed = 0
    for summary in _window(store.list(session_id=session_id,
                                      limit=max(1, limit)), since, until, build):
        trajectory_id = summary.get("trajectory_id") or summary.get("id")
        if not trajectory_id:
            continue
        seen: dict[str, int] = {}
        workspace = summary.get("workspace")
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
                    where = _cd_class(command, workspace)
                    cd_classes[where] = cd_classes.get(where, 0) + 1
                    if where == "foreign":
                        target = _cd_target(command)
                        foreign[target] = foreign.get(target, 0) + 1
                seen[command] = seen.get(command, 0) + 1
                pending[str(event.get("id"))] = head
            else:
                if _is_error(event.get("output")):
                    head = pending.get(str(event.get("id")), "?")
                    error_heads[head] = error_heads.get(head, 0) + 1
        for command, count in seen.items():
            if count > 1:
                key = command[:80]
                repeats[key] = repeats.get(key, 0) + count - 1
    return {
        "commands": total,
        "cwd_distrust": round(cd_prefixed / total, 3) if total else 0.0,
        "cwd_home": round(cd_classes.get("home", 0) / total, 3) if total else 0.0,
        "cwd_foreign": round(cd_classes.get("foreign", 0) / total, 3) if total else 0.0,
        "foreign_targets": dict(sorted(
            foreign.items(), key=lambda kv: -kv[1])[:MAX_HOTSPOTS]),
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
    if report.get("builds"):
        lines.append("builds: " + ", ".join(
            f"{build} x{count}" for build, count in report["builds"].items()))
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
                  until: float | None = None,
                 build: str | None = None) -> dict:
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
    by_call: dict[str, dict] = {}
    for summary in _window(store.list(session_id=session_id,
                                      limit=max(1, limit)), since, until, build):
        trajectory_id = summary.get("trajectory_id") or summary.get("id")
        if not trajectory_id:
            continue
        index = 0
        for event in store.iter_events(
            trajectory_id, types={"model_end"}, limit=2_000,
        ):
            calls += 1
            index += 1
            reason = str(event.get("stop_reason"))
            stop_reasons[reason] = stop_reasons.get(reason, 0) + 1
            usage = event.get("usage")
            if isinstance(usage, dict):
                read = int(usage.get("cache_read_input_tokens") or 0)
                uncached = int(usage.get("input_tokens") or 0)
                created = int(usage.get("cache_creation_input_tokens") or 0)
                tokens["input"] += uncached
                tokens["output"] += int(usage.get("output_tokens") or 0)
                tokens["cache_read"] += read
                tokens["cache_creation"] += created
                # The decay gauge: a healthy prefix cache holds its share
                # as a session grows; a share that collapses with call
                # index means something rewrites history mid-session.
                key = str(index) if index < 5 else "5+"
                bucket = by_call.setdefault(key, {"read": 0, "prompt": 0})
                bucket["read"] += read
                bucket["prompt"] += read + uncached + created
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
        "cache_share_by_call": {
            key: round(bucket["read"] / bucket["prompt"], 3)
            for key, bucket in by_call.items() if bucket["prompt"]
        },
        "median_call_ms": round(median(durations), 1) if durations else None,
        "truncations": stop_reasons.get("max_tokens", 0),
    }


def time_profile(store: Any, *, session_id: str | None = None,
                 limit: int = MAX_TRAJECTORIES, since: float | None = None,
                 until: float | None = None,
                 build: str | None = None) -> dict:
    """Where the wall-clock went: model, tools, or harness slack.

    Every model_end and tool_result carries its own duration; the
    trajectory carries the wall total. What neither covers -- injectors,
    compaction, journaling, the loop itself -- is the slack, computed by
    subtraction. First corpus reading (2026-09-02, 85 trajectories):
    98% model, 2% tools, 0.2% slack -- a clean bill that says the
    latency lever is fewer rounds, not faster harness code.
    """

    wall = model = tool = 0.0
    tool_ms: dict[str, float] = {}
    trajectories = 0
    for summary in _window(store.list(session_id=session_id,
                                      limit=max(1, limit)), since, until, build):
        trajectory_id = summary.get("trajectory_id") or summary.get("id")
        if not trajectory_id:
            continue
        try:
            wall += float(summary.get("duration_ms") or 0)
        except (TypeError, ValueError):
            continue
        trajectories += 1
        for event in store.iter_events(
            trajectory_id, types={"model_end", "tool_result"}, limit=2_000,
        ):
            try:
                duration = float(event.get("duration_ms") or 0)
            except (TypeError, ValueError):
                continue
            if event.get("type") == "model_end":
                model += duration
            else:
                tool += duration
                name = str(event.get("name", "?"))
                tool_ms[name] = tool_ms.get(name, 0.0) + duration
    slack = max(0.0, wall - model - tool)
    def _share(part: float) -> float:
        return round(part / wall, 3) if wall else 0.0
    return {
        "trajectories": trajectories,
        "wall_ms": round(wall, 1),
        "model_ms": round(model, 1),
        "tool_ms": round(tool, 1),
        "slack_ms": round(slack, 1),
        "shares": {"model": _share(model), "tool": _share(tool),
                   "slack": _share(slack)},
        "tool_ms_by_name": dict(sorted(
            ((k, round(v, 1)) for k, v in tool_ms.items()),
            key=lambda kv: -kv[1])),
    }


def render_time(profile: dict) -> str:
    """Bounded text projection of a wall-clock ledger."""

    shares = profile["shares"]
    lines = [f"# time ledger ({profile['trajectories']} trajectories)"]
    lines.append(
        f"wall {profile['wall_ms'] / 1000:,.1f}s = "
        f"model {shares['model']:.0%} + tools {shares['tool']:.0%} + "
        f"harness slack {shares['slack']:.1%}"
    )
    for name, ms in list(profile["tool_ms_by_name"].items())[:MAX_HOTSPOTS]:
        lines.append(f"- {name}: {ms / 1000:,.1f}s")
    return "\n".join(lines)


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
    if profile.get("cache_share_by_call"):
        curve = " ".join(
            f"#{key}:{share:.0%}"
            for key, share in sorted(profile["cache_share_by_call"].items(),
                                     key=lambda kv: (len(kv[0]), kv[0])))
        lines.append(f"cache share by call: {curve}")
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
    lines.append(f"  home {profile['cwd_home']:.0%} (cd back into the session "
                 "workspace: pure cwd distrust) | "
                 f"foreign {profile['cwd_foreign']:.0%} (cd elsewhere: the "
                 "work lives outside the workspace)")
    if profile["foreign_targets"]:
        lines.append("\n## foreign cd targets (where the work really lives)")
        for target, count in profile["foreign_targets"].items():
            lines.append(f"- {target}: {count}")
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


_REFUSAL_PATH = (
    # Experiment I's message: "... escapes workspace: {p}. Paths are relative ..."
    re.compile(r"escapes workspace: (.*?)\. Paths are relative"),
    # The pre-I message ended at the path.
    re.compile(r"escapes workspace: (\S+)"),
)


def _refused_path(output: str) -> str:
    for pattern in _REFUSAL_PATH:
        match = pattern.search(output)
        if match:
            return match.group(1).strip()
    return ""


def refusal_profile(store: Any, *, session_id: str | None = None,
                    limit: int = MAX_TRAJECTORIES, since: float | None = None,
                    until: float | None = None,
                    build: str | None = None) -> dict:
    """What the model did right after the workspace fence refused a path.

    The fence's cost is a round per refusal; its worth is what the refusal
    prevented. The corpus's first reading (2026-09-02, 75 refusals): zero
    recoveries through read_file, zero abandoned turns, and every single
    one followed by a bash command -- 29 of them reading the very file
    just refused. Under a Null sandbox that is a speed bump, not a
    boundary, and the number belongs in the report where the operator
    weighing workspace binding can see it.
    """

    outcomes: dict[str, int] = {}
    refusals = 0
    for summary in _window(store.list(session_id=session_id,
                                      limit=max(1, limit)), since, until, build):
        trajectory_id = summary.get("trajectory_id") or summary.get("id")
        if not trajectory_id:
            continue
        events = list(store.iter_events(
            trajectory_id, types={"tool_use", "tool_result"}, limit=2_000,
        ))
        for index, event in enumerate(events):
            if event.get("type") != "tool_result":
                continue
            output = str(event.get("output") or "")
            if not output.lstrip().startswith("Error: Path escapes workspace"):
                continue
            refusals += 1
            refused = _refused_path(output)
            base = refused.rsplit("/", 1)[-1]
            following = next(
                (e for e in events[index + 1:] if e.get("type") == "tool_use"),
                None,
            )
            if following is None:
                outcome = "turn ended"
            else:
                name = str(following.get("name"))
                inputs = following.get("input")
                inputs = inputs if isinstance(inputs, dict) else {}
                if name == "bash":
                    command = str(inputs.get("command", ""))
                    outcome = ("bash reads the same path (fence bypassed)"
                               if base and base in command else "bash elsewhere")
                elif name == "read_file":
                    path = str(inputs.get("path", ""))
                    outcome = ("read_file absolute again"
                               if path.startswith("/")
                               else "read_file relative (recovered)")
                else:
                    outcome = f"{name}"
            outcomes[outcome] = outcomes.get(outcome, 0) + 1
    return {
        "refusals": refusals,
        "outcomes": dict(sorted(outcomes.items(), key=lambda kv: -kv[1])),
        "recovered": outcomes.get("read_file relative (recovered)", 0),
        "bypassed": outcomes.get("bash reads the same path (fence bypassed)", 0),
    }


def render_refusals(profile: dict) -> str:
    """Bounded text projection of the post-refusal profile."""

    lines = [f"# workspace fence ({profile['refusals']} refusals)"]
    if not profile["refusals"]:
        lines.append("no refusals in the window")
        return "\n".join(lines)
    lines.append(f"recovered via read_file: {profile['recovered']} | "
                 f"same file read through bash: {profile['bypassed']}")
    lines.append("\n## what followed each refusal")
    for outcome, count in list(profile["outcomes"].items())[:MAX_HOTSPOTS]:
        lines.append(f"- {outcome}: {count}")
    return "\n".join(lines)


def era_table(store: Any, *, session_id: str | None = None,
              limit: int = MAX_TRAJECTORIES, since: float | None = None,
              until: float | None = None, build: str | None = None) -> list[dict]:
    """The acceptance gauges, one row per build, newest build first.

    A landed experiment's before/after is a comparison between builds;
    running the profiles twice with two `--build` prefixes and lining up
    the numbers by hand is where transcription errors live. This fold
    keys the gauges that experiments are read on -- the two cd shares and
    the read_file error rate -- by the build each trajectory recorded, so
    one report carries the comparison and its sample sizes.
    """

    eras: dict[str, dict] = {}
    for summary in _window(store.list(session_id=session_id,
                                      limit=max(1, limit)), since, until, build):
        trajectory_id = summary.get("trajectory_id") or summary.get("id")
        if not trajectory_id:
            continue
        key = str(summary.get("build") or "(unrecorded)")
        era = eras.setdefault(key, {
            "build": key, "trajectories": 0, "commands": 0, "cd_home": 0,
            "cd_foreign": 0, "read_calls": 0, "read_errors": 0,
            "latest": 0.0,
        })
        era["trajectories"] += 1
        era["latest"] = max(era["latest"], _started_at(summary))
        workspace = summary.get("workspace")
        pending: dict[str, str] = {}
        for event in store.iter_events(
            trajectory_id, types={"tool_use", "tool_result"}, limit=2_000,
        ):
            name = event.get("name")
            if event.get("type") == "tool_use":
                inputs = event.get("input")
                if name == "bash":
                    command = (str(inputs.get("command", ""))
                               if isinstance(inputs, dict) else "")
                    era["commands"] += 1
                    if _command_head(command) == "cd":
                        where = _cd_class(command, workspace)
                        if where in ("home", "foreign"):
                            era[f"cd_{where}"] += 1
                elif name == "read_file":
                    era["read_calls"] += 1
                    pending[str(event.get("id"))] = "read_file"
            elif name == "read_file" and _is_error(event.get("output")):
                era["read_errors"] += 1
    rows = []
    for era in sorted(eras.values(), key=lambda e: -e["latest"]):
        commands = era["commands"]
        rows.append({
            **era,
            "cwd_home": round(era["cd_home"] / commands, 3) if commands else 0.0,
            "cwd_foreign": round(era["cd_foreign"] / commands, 3) if commands else 0.0,
            "read_error_rate": (round(era["read_errors"] / era["read_calls"], 3)
                                if era["read_calls"] else 0.0),
        })
    return rows


def render_eras(rows: list[dict]) -> str:
    """Bounded text table of the acceptance gauges by build."""

    lines = ["# by build (acceptance gauges; newest build first)",
             "build          n  commands  cwd_home  cwd_foreign  read_file errors"]
    for row in rows[:MAX_HOTSPOTS]:
        lines.append(
            f"{row['build'][:12]:<12} {row['trajectories']:>4} {row['commands']:>9} "
            f"{row['cwd_home']:>9.0%} {row['cwd_foreign']:>12.0%} "
            f"{row['read_errors']:>6}/{row['read_calls']}"
        )
    if not rows:
        lines.append("(no trajectories in the window)")
    return "\n".join(lines)


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: a pure read-only fold over TrajectoryStore's "
    "bounded readers; the worst outcome is a report over fewer rows."
)
