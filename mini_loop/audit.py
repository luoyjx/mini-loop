"""Report what a deployment actually has switched on.

Every protection added to this harness is opt-in, and each one defaults to a
`Null*` implementation so that adding it changed no existing behaviour. That was
the right call per module and the wrong outcome in aggregate: a default
`SessionManager` runs with no shell confinement, no secret masking, no durable
state, and an in-memory action journal -- and nothing says so. The failure mode
is silence, which is the one thing a security posture must not be.

    python -m mini_loop.audit

Findings are graded, and the process exits non-zero when any `high` or
`critical` one is present, so it can gate a deploy. Grading is by *consequence
if the assumption is wrong*, not by how likely the misconfiguration is:
`critical` means untrusted input can reach the host, `high` means a credential
or a side effect can escape or repeat, `medium` means state or auditability is
lost, `info` is a note.

This is the roadmap's `mini-loop security audit`, minus the parts that need
machinery that does not exist yet: there is no auth or tenancy to check, and no
plugin manifest to verify.
"""

from __future__ import annotations

import os
import stat
import sys
from dataclasses import dataclass
from pathlib import Path
from collections.abc import Mapping

from .problems import ProblemLog
from typing import Any

__all__ = [
    "Finding",
    "SEVERITIES",
    "audit",
    "audit_settings",
    "audit_posture",
    "render",
    "main",
]

SEVERITIES = ("critical", "high", "medium", "info")
_BLOCKING = {"critical", "high"}

#: Channels with a hand-written check below, which gives them a specific remedy.
#: The generic sweep skips these so one fault is not reported twice.
_SPECIFICALLY_CHECKED = frozenset({"cron", "skills"})


@dataclass(frozen=True, slots=True)
class Finding:
    severity: str
    check: str
    detail: str
    remedy: str

    def __post_init__(self) -> None:
        if self.severity not in SEVERITIES:
            raise ValueError(f"unknown severity: {self.severity}")


def _render(problems) -> list[str]:
    """Distinct problems with their counts, when the log tracks them.

    A churning log is flagged, because its per-message counts are lower bounds:
    a subsystem reporting more distinct problems than the log retains evicts and
    re-adds them, restarting each count at one.
    """

    summary = getattr(problems, "summary", None)
    if not callable(summary):
        return [str(problem) for problem in problems]
    rendered = summary()
    if getattr(problems, "churning", lambda: False)() and rendered:
        rendered[0] += (
            f" [log churning: {problems.total():,} occurrences seen, counts are "
            "lower bounds]"
        )
    return rendered


def _is_null(value: Any, name: str) -> bool:
    return type(value).__name__ == name


def _credential_shaped_env(environ) -> list[str]:
    from .secrets import DEFAULT_SECRET_PATTERNS

    import fnmatch

    return sorted(
        key
        for key, value in environ.items()
        if value
        and any(fnmatch.fnmatchcase(key.upper(), p) for p in DEFAULT_SECRET_PATTERNS)
    )


def audit_settings(settings, *, environ=None) -> list[Finding]:
    """Checks that depend only on configuration, not on a live manager."""

    environ = os.environ if environ is None else environ
    findings: list[Finding] = []

    from .auth import LOOPBACK_HOSTS, load_auth

    host = environ.get("HOST", "127.0.0.1")
    auth = load_auth(environ)
    if host not in LOOPBACK_HOSTS and not auth.configured:
        findings.append(
            Finding(
                "critical",
                "host-bind",
                f"the server binds {host} with no authentication -- every "
                "session, event stream and transcript is reachable by anyone "
                "who can route to the port",
                "set MINILOOP_API_TOKEN, or bind 127.0.0.1 "
                "(`python -m mini_loop` now refuses this combination outright)",
            )
        )
    elif not auth.configured:
        findings.append(
            Finding(
                "medium",
                "authentication",
                "no API tokens are configured; every caller on the loopback "
                "interface shares one anonymous identity and can read every "
                "session and transcript",
                "set MINILOOP_API_TOKEN (or MINILOOP_API_TOKENS=alice:...,bob:...)",
            )
        )

    if settings.fake_llm:
        findings.append(
            Finding(
                "info",
                "fake-llm",
                "MINILOOP_FAKE_LLM is set; the deterministic offline model is "
                "answering, not a provider",
                "unset it for a real deployment",
            )
        )

    root = Path(settings.workspace_root)
    if root.exists():
        mode = root.stat().st_mode
        if mode & stat.S_IWOTH:
            findings.append(
                Finding(
                    "high",
                    "workspace-permissions",
                    f"{root} is world-writable, so anything on the host can plant "
                    "files an agent will read and act on",
                    f"chmod o-w {root}",
                )
            )

    if settings.trajectory_enabled and settings.trajectory_capture_content:
        findings.append(
            Finding(
                "medium",
                "trajectory-content",
                "trajectories record full tool inputs and outputs to disk",
                "set MINILOOP_TRAJECTORY_CAPTURE_CONTENT=0, or make sure secret "
                "masking is on so credentials never reach them",
            )
        )

    return findings


def audit(manager, *, environ=None) -> list[Finding]:
    """Audit a live `SessionManager`, plus its settings."""

    environ = os.environ if environ is None else environ
    findings = list(audit_settings(manager.settings, environ=environ))

    registry = manager.tool_registry
    has_shell = registry is None or "bash" in set(registry.names())

    if registry is not None:
        # `risk` is the single source of truth for "does this mutate" (round
        # 95's ladder). The audit used to read `readonly`, which the permission
        # layer never consulted and which had already drifted from `risk` on
        # two tools -- exactly the two-sources-of-truth trap this harness keeps
        # closing. A parallel_safe claim on a `read` tool is fine; on anything
        # else it is a lost update or a raced side effect the harness cannot
        # verify, and the blast radius scales with the risk: two concurrent
        # external calls (two deploys) are worse than two local writes.
        parallel = [
            tool for tool in (registry.get(name) for name in registry.names())
            if tool is not None and tool.parallel_safe
        ]
        side_effecting = sorted(
            t.name for t in parallel if t.risk in ("exec", "external") or t.risk is None
        )
        local_writers = sorted(t.name for t in parallel if t.risk == "write")
        if side_effecting:
            findings.append(
                Finding(
                    "high",
                    "concurrent-side-effects",
                    f"{', '.join(side_effecting)} declare themselves safe to run "
                    "concurrently while running code or acting outside this "
                    "machine (or declaring no risk at all); two in one "
                    "model-emitted batch can fire overlapping side effects the "
                    "harness cannot serialize or verify",
                    "drop parallel_safe on tools that exec or act externally, or "
                    "confirm the downstream has its own concurrency control",
                )
            )
        if local_writers:
            findings.append(
                Finding(
                    "medium",
                    "concurrent-writers",
                    f"{', '.join(local_writers)} declare themselves safe to run "
                    "concurrently while also mutating local state; two of them in "
                    "one model-emitted batch can lose an update, and the harness "
                    "cannot check the claim",
                    "drop parallel_safe on tools that write, or confirm their "
                    "writes go somewhere with its own concurrency control",
                )
            )

    sandbox = getattr(manager, "sandbox", None)
    reason = getattr(sandbox, "reason", None)
    if reason is not None:
        # Confinement was asked for and this host cannot give it. A different
        # fact from "none was configured", and a different remedy.
        findings.append(
            Finding(
                "high" if has_shell else "medium",
                "shell-confinement-unavailable",
                f"confinement was requested but is unavailable: {reason}. Shell "
                "commands are running on the host with the agent's full rights.",
                "run this deployment in a container, or on a host where a "
                "backend exists; passing a different sandbox= will not help",
            )
        )
    elif sandbox is None or _is_null(sandbox, "NullSandbox"):
        findings.append(
            Finding(
                "high" if has_shell else "medium",
                "shell-confinement",
                "shell commands run on the host with the agent's full user "
                "rights; `cwd` is the workspace but nothing stops `cd /`. The "
                "DANGEROUS command list is a typo guard, not confinement -- it "
                "is substring matching, and `$(echo rm) -rf /` walks past it",
                "pass sandbox=SeatbeltSandbox(...) (or default_sandbox(workspace))",
            )
        )

    if has_shell:
        # Independent of the sandbox, which answers "where may it write" and not
        # "how much may it consume". A deployment with confinement active draws
        # no `shell-confinement` finding at all, so an operator reading a clean
        # result has nothing telling them a runaway command can still take the
        # host down.
        #
        # Measured rather than assumed: `bash_timeout` bounds wall time (and
        # since round 70 reaps the process group), but on this platform
        # `ulimit -H -f` did not cap a 50 MB write, `ulimit -H -t` did not stop
        # an infinite loop, and `ulimit -v` is rejected outright. `preexec_fn`
        # is not an option either -- `run_bash` executes in a thread. Limiting
        # consumption needs a container, so that is what the remedy says.
        findings.append(
            Finding(
                "medium",
                "resource-limits",
                "nothing bounds how much a shell command consumes: memory, disk "
                "and CPU are unlimited, and a single command allocated 700 MB "
                "unopposed in testing. `bash_timeout` bounds wall time only. A "
                "sandbox does not help -- it confines where a command writes, "
                "not how much",
                "run this deployment in a container with memory, disk and CPU "
                "limits; shell `ulimit` was measured and does not enforce these "
                "on macOS",
            )
        )

    secrets = getattr(manager, "secrets", None)
    if secrets is None or _is_null(secrets, "NullSecretRegistry"):
        exposed = _credential_shaped_env(environ)
        findings.append(
            Finding(
                "high" if exposed else "medium",
                "secret-masking",
                "no secret registry: the shell inherits the whole process "
                "environment and tool output is not masked"
                + (f"; {len(exposed)} credential-shaped variables are set "
                   f"({', '.join(exposed[:3])}{'...' if len(exposed) > 3 else ''})"
                   if exposed else ""),
                "pass secrets=SecretRegistry.from_environ()",
            )
        )
    else:
        # A registry exists, but masking only covers the values it was handed.
        # A credential-shaped variable that was never registered stays in the
        # environment `run_bash` inherits (`scrub_env` removes only *registered*
        # names), and `mask()` cannot hide a value it does not know -- so it
        # reaches tool output raw. "Has a registry" is not "masks its
        # credentials"; the gap is the deployment that built the registry by
        # hand, or from a narrow pattern set, and missed one. Flagging only the
        # no-registry case let an incomplete registry read as a clean bill of
        # health, which is the failure mode this audit exists to catch.
        registered = set(getattr(secrets, "names", lambda: ())())
        unregistered = [
            key for key in _credential_shaped_env(environ) if key not in registered
        ]
        if unregistered:
            findings.append(
                Finding(
                    "high",
                    "secret-unregistered",
                    f"{len(unregistered)} credential-shaped variable(s) are set "
                    "but not registered for masking "
                    f"({', '.join(unregistered[:3])}"
                    f"{'...' if len(unregistered) > 3 else ''}); the shell "
                    "inherits them and their values reach tool output unmasked",
                    "register them (SecretRegistry.from_environ() picks up every "
                    "credential-shaped name), or unset them in this deployment",
                )
            )

    persist_error = (
        manager.persistence_error() if hasattr(manager, "persistence_error") else None
    )
    if persist_error:
        findings.append(
            Finding(
                "high",
                "durable-state-failing",
                f"a state store is configured but its writes are failing "
                f"({persist_error}); the agent keeps running and every session "
                "on this process is unrecoverable after a restart",
                "check disk space and permissions on the database path; this is "
                "a live fault, not a configuration choice",
            )
        )

    unresolved = tuple(getattr(secrets, "unresolved", lambda: ())())
    if unresolved:
        findings.append(
            Finding(
                "high",
                "secret-unresolved",
                f"{', '.join(unresolved)} are registered as secrets but their "
                "values could not be read, so nothing is being masked for them "
                "while the deployment reports masking as on",
                "check the source of those values; a registered-but-unreadable "
                "secret is worse than an unregistered one, because it looks safe",
            )
        )

    short = tuple(getattr(secrets, "short_values", lambda: ())())
    if short:
        findings.append(
            Finding(
                "medium",
                "secret-too-short",
                f"{', '.join(short)} hold values below the masking floor, so they "
                "are left in output rather than shredding unrelated text",
                "use longer values, or stop registering these names",
            )
        )

    mcp_problems = ProblemLog()
    for session in getattr(manager, "_sessions", {}).values():
        agent = getattr(session, "agent", None)
        if agent is not None:
            log = agent.state.get("mcp_problems", [])
            # Merge with counts, so one server failing across many sessions
            # reads as one problem seen many times.
            for message in log:
                for _ in range(getattr(log, "counts", {}).get(message, 1)):
                    mcp_problems.append(message)
    if mcp_problems:
        findings.append(
            Finding(
                "medium",
                "mcp-problems",
                f"{len(mcp_problems)} MCP tool(s) were refused or truncated: "
                f"{_render(mcp_problems)[0]}"
                + (f" (+{len(mcp_problems) - 1} more)" if len(mcp_problems) > 1 else ""),
                "a tool that quietly failed to appear looks the same as a server "
                "that was never connected; rename the servers or drop one",
            )
        )

    # Not `list(...)`: copying into a plain list discards the ProblemLog and
    # with it the occurrence counts, so the audit reported "10 problems" for one
    # fault that happened ten times.
    scheduled = getattr(getattr(manager, "cron", None), "problems", [])
    if scheduled:
        findings.append(
            Finding(
                "medium",
                "cron-problems",
                f"{len(scheduled)} scheduled job(s) were refused, dropped or "
                f"fired into nothing: {_render(scheduled)[0]}"
                + (f" (+{len(scheduled) - 1} more)" if len(scheduled) > 1 else ""),
                "a schedule that stops running looks exactly like one that was "
                "never created; fix or cancel the jobs",
            )
        )

    problems = getattr(getattr(manager, "skills", None), "problems", [])
    if problems:
        findings.append(
            Finding(
                "medium",
                "skills-rejected",
                f"{len(problems)} skill file(s) were refused, shadowed or "
                f"truncated: {_render(problems)[0]}"
                + (f" (+{len(problems) - 1} more)" if len(problems) > 1 else ""),
                "a skill is an instruction the model follows; fix or remove the "
                "files rather than leaving the set ambiguous",
            )
        )

    # Everything else that has somewhere to say "that did not work".
    #
    # `cron` and `skills` above are hand-written because they earn a specific
    # remedy. Every other channel was reaching nobody: the manager carries six
    # problem logs and exactly two were checked, so `actions`, `memory`,
    # `tool_registry` and the bus accumulated reports with no reader. Rounds 45
    # to 50 built the channels and rounds 86 and 91 added more; each one was
    # written as "reported, not hidden" and each was half true -- the value was
    # recorded and nothing surfaced it.
    #
    # Swept rather than enumerated, so the next channel is covered by existing
    # code instead of by somebody remembering.
    for name in sorted(dir(manager)):
        if name.startswith("_") or name in _SPECIFICALLY_CHECKED:
            continue
        log = getattr(getattr(manager, name, None), "problems", None)
        if not log or not isinstance(log, (list, tuple)):
            continue
        findings.append(
            Finding(
                "medium",
                f"{name}-problems",
                f"{len(log)} problem(s) reported by {name}: {_render(log)[0]}"
                + (f" (+{len(log) - 1} more)" if len(log) > 1 else ""),
                f"inspect manager.{name}.problems; a subsystem that degrades "
                "quietly stays degraded",
            )
        )

    if _is_null(manager.state_store, "NullStateStore"):
        findings.append(
            Finding(
                "medium",
                "durable-state",
                "no state store: transcripts and event cursors exist only in "
                "this process and are lost on restart",
                "pass state_store=SQLiteStateStore(path)",
            )
        )

    journal = getattr(manager, "actions", None)
    if journal is not None and _is_null(journal, "InMemoryActionJournal"):
        findings.append(
            Finding(
                "high" if has_shell else "medium",
                "action-journal",
                "the action journal is in-memory, so a restarted process cannot "
                "tell which side effects already happened and may run them again",
                "pass a SQLite state_store; the journal becomes durable with it",
            )
        )

    if manager.enable_workflows:
        findings.append(
            Finding(
                "info",
                "workflows",
                "the experimental workflow surface is enabled; it is "
                "process-local and not restart-safe",
                "confirm this manager is not the one serving untrusted input",
            )
        )

    order = {name: index for index, name in enumerate(SEVERITIES)}
    return sorted(findings, key=lambda f: (order[f.severity], f.check))


def render(findings: list[Finding]) -> str:
    if not findings:
        return "audit: no findings."
    lines = []
    counts: dict[str, int] = {}
    for finding in findings:
        counts[finding.severity] = counts.get(finding.severity, 0) + 1
        lines.append(f"[{finding.severity.upper():<8}] {finding.check}")
        lines.append(f"           {finding.detail}")
        lines.append(f"           -> {finding.remedy}")
    summary = ", ".join(f"{counts[s]} {s}" for s in SEVERITIES if s in counts)
    lines.append(f"\n{len(findings)} finding(s): {summary}")
    return "\n".join(lines)


def audit_posture(report: Mapping[str, Any], *, source: str) -> list[Finding]:
    """Audit a *running* server from the posture it reports.

    Fewer checks than the local audit: filesystem permissions and the host bind
    are not observable from the outside. Saying so is the point -- a remote
    audit that silently skipped them would read as a clean bill of health.
    """

    posture = dict(report.get("posture") or {})
    findings: list[Finding] = []

    if not posture:
        return [
            Finding(
                "medium",
                "posture-unavailable",
                f"{source} did not report a posture; it is older than this "
                "audit or is not a mini-loop server",
                "upgrade the server, or audit it locally",
            )
        ]

    if not report.get("authenticated"):
        findings.append(
            Finding(
                "high",
                "authentication",
                f"{source} accepts unauthenticated callers; every session and "
                "transcript on it is readable by anyone who can reach it",
                "set MINILOOP_API_TOKEN on the server",
            )
        )
    if posture.get("sandbox_reason"):
        findings.append(
            Finding(
                "high",
                "shell-confinement-unavailable",
                f"{source} asked for confinement and could not get it: "
                f"{posture['sandbox_reason']}",
                "run it in a container, or on a host with a backend",
            )
        )
    elif posture.get("sandbox") in (None, "None", "NullSandbox", "UnavailableSandbox"):
        findings.append(
            Finding(
                "high",
                "shell-confinement",
                f"{source} runs shell commands unconfined on its host",
                "start it with a sandbox",
            )
        )
    if posture.get("secrets") in (None, "None", "NullSecretRegistry"):
        findings.append(
            Finding(
                "high",
                "secret-masking",
                f"{source} does not mask credentials in tool output",
                "start it with secrets=SecretRegistry.from_environ()",
            )
        )
    elif posture.get("secrets_unregistered"):
        # A registry is present but does not cover the environment. The server
        # counts this for us -- a remote audit cannot see the host's env -- and
        # reports a count, never the names, because /healthz is public. Same leak
        # the local audit flags: the uncounted vars reach tool output unmasked.
        count = posture["secrets_unregistered"]
        findings.append(
            Finding(
                "high",
                "secret-unregistered",
                f"{source} has a secret registry but {count} credential-shaped "
                "variable(s) in its environment are not registered, so the shell "
                "inherits them and their values reach tool output unmasked",
                "register them on the server (SecretRegistry.from_environ() picks "
                "up every credential-shaped name)",
            )
        )
    if posture.get("state_store_error"):
        findings.append(
            Finding(
                "high",
                "durable-state-failing",
                f"{source} has a state store whose writes are failing: "
                f"{posture['state_store_error']}",
                "check disk space and permissions on the server's database path",
            )
        )
    elif posture.get("state_store") == "NullStateStore":
        findings.append(
            Finding(
                "medium",
                "durable-state",
                f"{source} keeps transcripts only in memory",
                "start it with a SQLite state store",
            )
        )
    if posture.get("action_journal") == "InMemoryActionJournal":
        findings.append(
            Finding(
                "high",
                "action-journal",
                f"{source} cannot tell, after a restart, which side effects "
                "already happened",
                "start it with a SQLite state store",
            )
        )

    order = {name: index for index, name in enumerate(SEVERITIES)}
    return sorted(findings, key=lambda f: (order[f.severity], f.check))


def main(argv: list[str] | None = None) -> int:
    """Audit a manager built the way `python -m mini_loop` builds one."""

    import json
    import urllib.request

    argv = sys.argv[1:] if argv is None else argv
    if argv and argv[0] == "--url":
        if len(argv) < 2:
            print("usage: python -m mini_loop.audit --url http://host:port")
            return 2
        base = argv[1].rstrip("/")
        with urllib.request.urlopen(f"{base}/healthz", timeout=10) as response:
            report = json.load(response)
        print(f"auditing {base}  build={report.get('build')} pid={report.get('pid')}")
        findings = audit_posture(report, source=base)
        print(render(findings))
        return 1 if any(f.severity in _BLOCKING for f in findings) else 0

    from .config import build_client, load_settings
    from .manager import SessionManager

    settings = load_settings()
    manager = SessionManager(
        settings, build_client(settings), enable_features=settings.enable_features
    )
    findings = audit(manager)
    print(render(findings))
    return 1 if any(f.severity in _BLOCKING for f in findings) else 0


if __name__ == "__main__":  # pragma: no cover - CLI entry
    sys.exit(main())

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: the audit IS the diagnostic surface -- it reports on other modules and holds no mutable state of its own to assert."
)
