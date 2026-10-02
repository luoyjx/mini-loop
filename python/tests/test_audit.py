"""Deployment audit -- what is actually switched on.

Every protection in this harness is opt-in and defaults to a `Null*`
implementation, which was right per module and wrong in aggregate: a default
deployment has no shell confinement, no secret masking, no durable state, and an
in-memory action journal, and nothing says so. These tests pin that the audit
reports the truth in both directions -- it must not cry wolf on a hardened
deployment either.
"""

import json
from pathlib import Path

import pytest

from mini_loop.audit import (
    SEVERITIES,
    Finding,
    audit,
    audit_posture,
    audit_settings,
    render,
)
from mini_loop.identity import posture
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.manager import SessionManager
from mini_loop.sandbox import SeatbeltSandbox
from mini_loop.secrets import SecretRegistry
from mini_loop.storage import SQLiteStateStore

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path, **over) -> Settings:
    base = dict(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR)
    base.update(over)
    return Settings(**base)


def _checks(findings) -> set[str]:
    return {f.check for f in findings}


def _by_check(findings, check) -> Finding:
    return next(f for f in findings if f.check == check)


CLEAN_ENV = {"PATH": "/usr/bin", "HOME": "/home/dev"}


# --- the default deployment ------------------------------------------------

def test_a_default_manager_reports_every_protection_as_off(tmp_path):
    manager = SessionManager(_settings(tmp_path), FakeAsyncAnthropic())
    findings = audit(manager, environ=CLEAN_ENV)

    assert {
        "shell-confinement",
        "secret-masking",
        "durable-state",
        "action-journal",
    } <= _checks(findings)


def test_a_hardened_manager_reports_nothing_alarming(tmp_path):
    """The audit must be usable, which means quiet when things are right."""
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = SessionManager(
        _settings(tmp_path, trajectory_enabled=False),
        FakeAsyncAnthropic(),
        state_store=store,
        secrets=SecretRegistry.from_environ(environ=CLEAN_ENV),
        sandbox=SeatbeltSandbox(writable_roots=[tmp_path / "ws"]),
    )
    findings = audit(manager, environ=CLEAN_ENV)
    blocking = [f for f in findings if f.severity in ("critical", "high")]
    assert not blocking, [f.check for f in blocking]
    store.close()


# --- severity reflects consequence, not likelihood -------------------------

def test_missing_masking_is_worse_when_real_credentials_are_present(tmp_path):
    manager = SessionManager(_settings(tmp_path), FakeAsyncAnthropic())

    without = _by_check(audit(manager, environ=CLEAN_ENV), "secret-masking")
    with_keys = _by_check(
        audit(manager, environ={**CLEAN_ENV, "STRIPE_API_KEY": "sk-live-x" * 4}),
        "secret-masking",
    )
    assert without.severity == "medium"
    assert with_keys.severity == "high"
    assert "STRIPE_API_KEY" in with_keys.detail


def test_an_incomplete_registry_flags_the_credentials_it_missed(tmp_path):
    """A registry that exists is not a registry that is complete.

    A credential-shaped variable the deployment forgot to register stays in the
    environment `run_bash` inherits (`scrub_env` drops only registered names),
    and `mask()` cannot hide a value it was never given -- so it reaches tool
    output raw. Before this the audit saw a registry and said nothing: "has
    masking" read as "masks its credentials", the clean-bill-of-health failure
    the audit exists to prevent.
    """
    registry = SecretRegistry()
    registry.register("APP_API_KEY", "sk-registered-000000000000")
    environ = {
        **CLEAN_ENV,
        "APP_API_KEY": "sk-registered-000000000000",  # registered -> scrubbed
        "STRIPE_API_KEY": "sk-live-" + "9" * 20,        # credential-shaped, missed
    }
    manager = SessionManager(_settings(tmp_path), FakeAsyncAnthropic(), secrets=registry)
    findings = audit(manager, environ=environ)

    # The no-registry finding must NOT fire -- there is a registry.
    assert "secret-masking" not in _checks(findings)
    finding = _by_check(findings, "secret-unregistered")
    assert finding.severity == "high"
    assert "STRIPE_API_KEY" in finding.detail
    # The one that was registered is not reported as a leak.
    assert "APP_API_KEY" not in finding.detail


def test_a_complete_registry_flags_no_unregistered_credential(tmp_path):
    """Not a wall: `from_environ` registers every credential-shaped name, so a
    deployment that used it trips neither the no-registry nor the incomplete
    finding, even with a live credential in the environment."""
    environ = {**CLEAN_ENV, "STRIPE_API_KEY": "sk-live-" + "9" * 20}
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(),
        secrets=SecretRegistry.from_environ(environ=environ),
    )
    checks = _checks(audit(manager, environ=environ))
    assert "secret-unregistered" not in checks
    assert "secret-masking" not in checks


def test_the_posture_counts_uncovered_credentials_without_naming_them(tmp_path, monkeypatch):
    """`/healthz` is public, so the posture reports *how many* credential-shaped
    env vars the registry missed, never *which* -- else the health endpoint would
    hand an unauthenticated caller the names of the host's secrets. The count is
    what lets a remote audit see the incomplete-registry leak it otherwise can't.
    """
    monkeypatch.setenv("PROBE_UNREG_TOKEN", "tok-" + "9" * 20)
    registry = SecretRegistry()  # present, but does not register the var above
    manager = SessionManager(_settings(tmp_path), FakeAsyncAnthropic(), secrets=registry)
    p = posture(manager)

    assert p["secrets_unregistered"] >= 1
    assert "PROBE_UNREG_TOKEN" not in json.dumps(p), "the posture leaked a credential name"


def test_a_remote_incomplete_registry_is_flagged_from_the_count():
    """`audit_posture` cannot see the server's environment, so it relies on the
    count the server plumbs through the posture. A registry that is present but
    reports uncovered credentials is the leak round 124 fixed locally, now
    reachable remotely -- and the local fix alone left this remote path blind.
    """
    report = {
        "authenticated": True,
        "posture": {
            "secrets": "SecretRegistry",
            "secrets_unregistered": 2,
            "sandbox": "SeatbeltSandbox",
            "state_store": "SQLiteStateStore",
        },
    }
    findings = {f.check: f for f in audit_posture(report, source="https://s")}
    assert "secret-masking" not in findings  # a registry IS present
    finding = findings["secret-unregistered"]
    assert finding.severity == "high"
    assert "2 credential-shaped" in finding.detail


def test_a_remote_complete_registry_is_not_flagged():
    """Not a wall: a registry reporting zero uncovered credentials trips neither
    the no-registry nor the incomplete finding."""
    report = {
        "authenticated": True,
        "posture": {
            "secrets": "SecretRegistry",
            "secrets_unregistered": 0,
            "sandbox": "SeatbeltSandbox",
            "state_store": "SQLiteStateStore",
        },
    }
    checks = {f.check for f in audit_posture(report, source="x")}
    assert "secret-unregistered" not in checks
    assert "secret-masking" not in checks


def test_a_public_bind_is_critical_because_nothing_authenticates(tmp_path):
    findings = audit_settings(
        _settings(tmp_path), environ={**CLEAN_ENV, "HOST": "0.0.0.0"}
    )
    finding = _by_check(findings, "host-bind")
    assert finding.severity == "critical"

    local = audit_settings(_settings(tmp_path), environ={**CLEAN_ENV, "HOST": "127.0.0.1"})
    assert "host-bind" not in _checks(local)


def test_a_world_writable_workspace_is_flagged(tmp_path):
    workspace = tmp_path / "ws"
    workspace.mkdir()
    workspace.chmod(0o777)
    findings = audit_settings(_settings(tmp_path), environ=CLEAN_ENV)
    assert _by_check(findings, "workspace-permissions").severity == "high"


def test_trajectory_content_is_flagged_while_it_is_recorded(tmp_path):
    on = audit_settings(
        _settings(tmp_path, trajectory_enabled=True, trajectory_capture_content=True),
        environ=CLEAN_ENV,
    )
    assert "trajectory-content" in _checks(on)

    off = audit_settings(
        _settings(tmp_path, trajectory_capture_content=False), environ=CLEAN_ENV
    )
    assert "trajectory-content" not in _checks(off)


# --- shape ------------------------------------------------------------------

def test_findings_are_ordered_worst_first(tmp_path):
    manager = SessionManager(_settings(tmp_path), FakeAsyncAnthropic())
    findings = audit(manager, environ={**CLEAN_ENV, "HOST": "0.0.0.0", "X_API_KEY": "k" * 20})
    order = [SEVERITIES.index(f.severity) for f in findings]
    assert order == sorted(order)


def test_every_finding_says_what_to_do_about_it(tmp_path):
    manager = SessionManager(_settings(tmp_path), FakeAsyncAnthropic())
    for finding in audit(manager, environ=CLEAN_ENV):
        assert finding.remedy.strip(), f"{finding.check} has no remedy"
        assert finding.detail.strip()


def test_an_unknown_severity_is_rejected():
    with pytest.raises(ValueError):
        Finding("scary", "x", "y", "z")


def test_render_is_readable_and_counts(tmp_path):
    manager = SessionManager(_settings(tmp_path), FakeAsyncAnthropic())
    text = render(audit(manager, environ=CLEAN_ENV))
    assert "shell-confinement" in text
    assert "finding(s)" in text
    assert render([]) == "audit: no findings."
