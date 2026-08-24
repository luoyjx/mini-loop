"""Self-observation: one bounded report of what the runtime knows about itself.

The first connecting piece of the self-evolution loop (docs/SELF_EVOLUTION
discussion, round 245+): every subsystem already keeps a deduplicating,
bounded ProblemLog, every turn already records a trajectory summary, and
round 232/244 taught info() to name what a session is actually doing -- but
nothing ever READ any of it. The operator had to know each surface existed
and visit them one by one, which in practice means never.

`build_report` folds those surfaces into one text report; the `self_audit`
tool serves it to a session (an operator's, or a scheduled one whose prompt
says "run self_audit and act on what it says"). Deliberately read-only and
disk-free: observation must not grow a write surface, and the tool result
passes through the same masking every tool output does. Every section is
independently guarded -- a broken source becomes a line in the report, never
a missing report -- and every scan is capped, because a self-audit that
grows with runtime age is the bounded-work defect reporting on itself.
"""

from __future__ import annotations

from typing import Any

__all__ = ["MAX_REPORT_CHARS", "build_report", "install_self_audit"]

#: Hard cap on the rendered report; truncated with a marker, never silently.
MAX_REPORT_CHARS = 8_000
#: Sessions examined for the activity distribution (most recent first).
MAX_SESSIONS_SCANNED = 100
#: Trajectory summaries examined for the trend section.
MAX_TRAJECTORIES_SCANNED = 50


def _problem_sources(manager: Any) -> list[tuple[str, Any]]:
    """Every ProblemLog the runtime holds, named. Missing seams are skipped."""

    sources: list[tuple[str, Any]] = []
    for name, holder in (
        ("cron", getattr(manager, "cron", None)),
        ("trajectories", getattr(manager, "trajectories", None)),
        ("approvals", getattr(manager, "approvals", None)),
        ("skills", getattr(manager, "skills", None)),
        ("actions", getattr(manager, "actions", None)),
    ):
        log = getattr(holder, "problems", None)
        if log is not None:
            sources.append((name, log))
    for session in _recent_sessions(manager):
        agent = getattr(session, "agent", None)
        if agent is None:
            continue
        sid = getattr(session, "id", "?")
        registry_log = getattr(getattr(agent, "tools", None), "problems", None)
        if registry_log:
            sources.append((f"registry[{sid}]", registry_log))
        state = getattr(agent, "state", None) or {}
        for key in ("tasks", "teams", "memory"):
            log = getattr(state.get(key), "problems", None)
            if log:
                sources.append((f"{key}[{sid}]", log))
    return sources


def _recent_sessions(manager: Any) -> list[Any]:
    sessions = list(getattr(manager, "list", lambda: [])())
    sessions.sort(key=lambda s: getattr(s, "created_at", 0), reverse=True)
    return sessions[:MAX_SESSIONS_SCANNED]


def _section(title: str, lines: list[str]) -> list[str]:
    return [f"## {title}", *(lines or ["(nothing)"]), ""]


def build_report(manager: Any) -> str:
    """One bounded self-audit: problems, activity, trajectory trends."""

    out: list[str] = ["# self-audit"]

    # -- sessions and what they are doing right now ------------------------
    try:
        sessions = _recent_sessions(manager)
        total = len(getattr(manager, "list", lambda: [])())
        distribution: dict[str, int] = {}
        for session in sessions:
            try:
                activity = session.info().get("activity", "unknown")
            except Exception:
                activity = "uninspectable"
            distribution[activity] = distribution.get(activity, 0) + 1
        lines = [f"{count} {name}" for name, count in sorted(distribution.items())]
        if total > len(sessions):
            lines.append(
                f"(distribution over the {len(sessions)} most recent of "
                f"{total} sessions)"
            )
        out += _section(f"sessions ({total})", lines)
    except Exception as error:
        out += _section("sessions", [f"unreadable: {type(error).__name__}"])

    # -- every subsystem's own problem ledger ------------------------------
    try:
        problem_lines: list[str] = []
        for name, log in _problem_sources(manager):
            try:
                summary = list(log.summary()) if hasattr(log, "summary") else list(log)
                if not summary:
                    continue
                total_count = log.total() if hasattr(log, "total") else len(summary)
                churn = " (churning: counts are lower bounds)" if (
                    hasattr(log, "churning") and log.churning()
                ) else ""
                problem_lines.append(f"### {name}: {total_count} reported{churn}")
                problem_lines += [f"- {line}" for line in summary]
            except Exception as error:
                problem_lines.append(f"### {name}: unreadable ({type(error).__name__})")
        out += _section("problems", problem_lines)
    except Exception as error:
        out += _section("problems", [f"unreadable: {type(error).__name__}"])

    # -- trajectory trends: how recent turns actually went -----------------
    try:
        store = getattr(manager, "trajectories", None)
        if store is None:
            out += _section("trajectories", ["(recording disabled)"])
        else:
            summaries = store.list(limit=MAX_TRAJECTORIES_SCANNED)
            by_status: dict[str, int] = {}
            durations: list[tuple[float, str]] = []
            for summary in summaries:
                status = str(summary.get("status", "unknown"))
                if summary.get("partial"):
                    status += "+partial"
                by_status[status] = by_status.get(status, 0) + 1
                duration = summary.get("duration_ms")
                if isinstance(duration, (int, float)):
                    durations.append((float(duration), summary.get("id", "?")))
            lines = [f"{count} {name}" for name, count in sorted(by_status.items())]
            durations.sort(reverse=True)
            if durations:
                slowest = ", ".join(
                    f"{tid} ({ms / 1000:.1f}s)" for ms, tid in durations[:3]
                )
                lines.append(f"slowest: {slowest}")
            lines.append(f"(the {len(summaries)} most recent recordings)")
            out += _section("trajectories", lines)
    except Exception as error:
        out += _section("trajectories", [f"unreadable: {type(error).__name__}"])

    # -- skill usage: which instructions get loaded, and how those turns end
    try:
        store = getattr(manager, "trajectories", None)
        if store is not None and hasattr(store, "iter_events"):
            usage: dict[str, dict[str, int]] = {}
            for summary in store.list(limit=MAX_TRAJECTORIES_SCANNED):
                outcome = str(summary.get("status", "unknown"))
                rough = "bad" if outcome in ("error", "interrupted") else "ok"
                for event in store.iter_events(
                    summary.get("id", ""), types={"tool_use"}, limit=200,
                ):
                    if event.get("name") != "load_skill":
                        continue
                    skill = str((event.get("input") or {}).get("name", "?"))
                    counts = usage.setdefault(skill, {"loads": 0, "bad": 0})
                    counts["loads"] += 1
                    if rough == "bad":
                        counts["bad"] += 1
            lines = [
                f"{name}: {c['loads']} load(s), {c['bad']} in turns that "
                "ended error/interrupted"
                for name, c in sorted(usage.items())
            ]
            if lines:
                lines.append(
                    "(correlation, not causation: a skill loaded in a bad "
                    "turn is a lead, not a verdict)"
                )
            out += _section("skill usage", lines)
    except Exception as error:
        out += _section("skill usage", [f"unreadable: {type(error).__name__}"])

    # -- scheduled work and its authorization state ------------------------
    try:
        cron = getattr(manager, "cron", None)
        jobs = dict(getattr(cron, "jobs", {}) or {})
        armed = set(getattr(cron, "_armed", ()) or ())
        disarmed = [job_id for job_id in jobs if job_id not in armed]
        lines = [f"{len(jobs)} scheduled, {len(disarmed)} disarmed"]
        if disarmed:
            lines.append(
                "disarmed (restored, awaiting operator arm): "
                + ", ".join(sorted(disarmed)[:10])
            )
        out += _section("cron", lines)
    except Exception as error:
        out += _section("cron", [f"unreadable: {type(error).__name__}"])

    report = "\n".join(out).rstrip()
    if len(report) > MAX_REPORT_CHARS:
        report = report[:MAX_REPORT_CHARS] + "\n[report truncated at the cap]"
    return report


_SCHEMA = {"type": "object", "properties": {}}


def install_self_audit(registry) -> None:
    from .registry import Tool

    async def self_audit(ctx) -> str:
        manager = (getattr(ctx, "state", None) or {}).get("manager")
        if manager is None:
            return "Error: no manager in scope; self_audit runs inside a managed session"
        return build_report(manager)

    registry.register(Tool(
        "self_audit",
        "One bounded report of the runtime's own state: per-subsystem "
        "problem ledgers, session activity, recent trajectory outcomes, "
        "and scheduled work. Read-only.",
        _SCHEMA,
        self_audit,
        readonly=True,
        risk="read",
    ))


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: a pure fold over other subsystems' state; every "
    "section catches its own failures into report lines, so the worst "
    "outcome is a report that says a section is unreadable."
)
