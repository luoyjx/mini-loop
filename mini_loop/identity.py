"""Which build is answering, and what it has switched on.

A process listening on a port tells you nothing about the code inside it. That
is not a theoretical concern: a server started from this package once stayed up
for fourteen hours through repeated `pkill -f "python -m mini_loop"` -- the real
command line is `.../Python -m mini_loop`, capital P, so the pattern never
matched and every kill silently failed. An entire round of measurements was then
taken against a build that predated the code being measured, and read as
"the feature does not work".

The fix is to make the question answerable. `/healthz` reports a **build
fingerprint** over the package source, so a client can assert that the server it
is talking to is the one it just built, and the effective **posture**, so an
operator can audit a running deployment instead of inferring it from local
config.

Upstream (the OpenHands agent server) reports `importlib.metadata.version(...)`,
which is right for a released package and useless between edits of an unreleased
one -- the version does not move when the source does. Hence a content hash.
It also sweeps stale artifacts from previous runs at startup, which is the same
lesson approached from the other side.
"""

from __future__ import annotations

import hashlib
import os
import time
from functools import lru_cache
from pathlib import Path
from typing import Any

__all__ = ["build_id", "runtime_identity", "posture", "STARTED_AT"]

STARTED_AT = time.time()
_PACKAGE = Path(__file__).resolve().parent


@lru_cache(maxsize=1)
def build_id() -> str:
    """A short hash over this package's source.

    Content, not mtime: a checkout, a copy or a rebuild that produces identical
    code should produce an identical id, and an edit must change it. Computed
    once per process -- it cannot change under a running interpreter that has
    already imported the modules.
    """

    digest = hashlib.sha256()
    for path in sorted(_PACKAGE.rglob("*.py")):
        if "__pycache__" in path.parts:
            continue
        digest.update(path.relative_to(_PACKAGE).as_posix().encode())
        digest.update(path.read_bytes())
    return digest.hexdigest()[:12]


def _name(value: Any) -> str:
    return type(value).__name__ if value is not None else "None"


def _uncovered_credentials(agent) -> int:
    """How many credential-shaped env vars the registry did *not* register.

    A registry masks only the values it was handed. `run_bash` builds its child
    environment as `scrub_env(os.environ)`, which drops only registered names,
    so a credential-shaped variable nobody registered stays in it and reaches
    tool output unmasked -- the local audit flags this (round 124), but a remote
    audit reading `/healthz` cannot see the server's environment, so the server
    has to count it here. A **count**, never the names: `/healthz` is public, so
    the identities of a host's credentials must not leak, but the count is
    enough for `audit_posture` to see the coverage gap it otherwise can't.
    """

    import fnmatch

    from .secrets import DEFAULT_SECRET_PATTERNS, NullSecretRegistry

    secrets = getattr(agent, "secrets", None)
    if secrets is None or isinstance(secrets, NullSecretRegistry):
        # The no-registry case is already reported as "no masking"; do not
        # double-count it here.
        return 0
    registered = set(getattr(secrets, "names", lambda: ())())
    return sum(
        1
        for key, value in os.environ.items()
        if value
        and key not in registered
        and any(fnmatch.fnmatchcase(key.upper(), p) for p in DEFAULT_SECRET_PATTERNS)
    )


def posture(manager, auth=None) -> dict[str, Any]:
    """What protections this process actually has active.

    Read off a **probe agent** built the way real ones are, not off the
    manager's configuration fields. The two disagree: a manager with no
    `sandbox=` reports `None` while its agents run `NullSandbox`, and it holds
    nothing at all for `cache_policy` or `stuck_detector` while every agent gets
    the `Default*` ones. Reporting the configuration answers "what was passed";
    an operator is asking "what is running".

    Reported rather than inferred, because every seam is opt-in and defaults to
    an implementation that changes nothing visible.
    """

    from .auth import NullAuth

    auth = auth or NullAuth()
    # Deliberately not wrapped: a probe that fails silently reports every seam
    # as absent, which turned a hardened deployment into a false alarm -- a
    # report that lies in the "looks worse than it is" direction is still a lie,
    # and this one would train an operator to ignore findings.
    agent = _probe_agent(manager)
    sandbox = getattr(agent, "sandbox", None) or getattr(manager, "sandbox", None)
    return {
        "authenticated": bool(auth.configured),
        "state_store": _name(manager.state_store),
        # Installed is not the same question as working. A store that opens and
        # then fails every write reported itself present here while nothing
        # reached disk.
        "state_store_error": (
            manager.persistence_error()
            if hasattr(manager, "persistence_error") else None
        ),
        "action_journal": _name(getattr(manager, "actions", None)),
        "secrets": _name(getattr(agent, "secrets", None)),
        # A registry can be present and still not cover the environment. Counted
        # here (where the env is visible), not named (see `_uncovered_credentials`),
        # so a remote audit can catch the incomplete-registry leak round 124 fixed
        # for the local one.
        "secrets_unregistered": _uncovered_credentials(agent),
        "sandbox": _name(sandbox),
        # Why it is inert, when it is -- "nobody configured one" and "this
        # platform cannot provide one" have different remedies.
        "sandbox_reason": getattr(sandbox, "reason", None),
        "cache_policy": _name(getattr(agent, "cache_policy", None)),
        "stuck_detector": _name(getattr(agent, "stuck_detector", None)),
        "workflows": bool(manager.enable_workflows),
        "trajectories": manager.trajectories is not None,
    }


def _probe_agent(manager):
    """An agent built exactly as a session's would be, for reading defaults off.

    Cheap and side-effect free: `_build_agent` only wires objects together. If a
    manager ever makes that expensive, this should read a live session instead
    -- but it must not go back to reading configuration fields, which is what
    made the report disagree with reality.
    """

    live = next(iter(getattr(manager, "_sessions", {}).values()), None)
    if live is not None and getattr(live, "agent", None) is not None:
        return live.agent

    class _Probe:
        id = "__posture_probe__"
        workspace = manager.settings.workspace_root
        system = None
        emit = None
        # Match AgentSession's fail-safe default without constructing or
        # persisting a real session just to report runtime posture.
        permission_mode = "interactive"

    return manager._build_agent(_Probe(), settings=manager.settings, extra_state={})


def runtime_identity(manager=None, auth=None) -> dict[str, Any]:
    """Everything a client needs to know it is talking to the right process."""

    identity: dict[str, Any] = {
        "build": build_id(),
        "pid": os.getpid(),
        "started_at": STARTED_AT,
        "uptime_s": round(time.time() - STARTED_AT, 3),
    }
    if manager is not None:
        identity["posture"] = posture(manager, auth)
    return identity


def dump_config(manager, settings, auth=None) -> dict[str, Any]:
    """The composition this process actually boots, as one printable value.

    DeepSeek Harness's `--dump-config` prints the plugin tree the machine
    really runs, so an operator debugs the actual composition instead of the
    one they believe they configured. Same rule as `posture`: read off a
    probe agent built the way real agents are, never off configuration
    fields, and redact anything credential-shaped rather than trusting the
    caller to.
    """

    import dataclasses

    redacted = {}
    for field in dataclasses.fields(settings):
        value = getattr(settings, field.name)
        if (
            field.name.endswith(("_key", "_token", "_secret", "_password"))
            or field.name in ("api_key",)
            or "secret" in field.name
        ):
            # Credential-shaped names show presence, never the value.
            # Suffix-matched, not substring: `token_threshold` is a budget
            # and `token_efficiency_mode` is a mode, not a credential.
            if isinstance(value, str) or value is None:
                value = "<set>" if value else None
        redacted[field.name] = value

    agent = _probe_agent(manager)
    harness = {
        field.name: _name(getattr(manager.harness, field.name))
        for field in dataclasses.fields(manager.harness)
        if field.name != "injectors"
    }
    return {
        "settings": redacted,
        "harness": harness,
        "tools": sorted(agent.tools._tools) if agent.tools else [],
        "injectors": len(manager.harness.injectors or ()),
        "features": {
            "enable_features": bool(getattr(manager, "enable_features", False)),
            "enable_workflows": bool(getattr(manager, "enable_workflows", False)),
        },
        "posture": posture(manager, auth),
    }

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: the build id is derived from package source at import; staleness is a test-time question (test_identity), not a runtime one."
)
