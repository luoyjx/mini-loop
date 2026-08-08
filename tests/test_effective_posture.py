"""What is reported must be what is running.

`posture()` read the manager's configuration fields, which answer "what was
passed", while an operator is asking "what is running". They disagreed on three
seams: a manager with no `sandbox=` reported `None` while its agents ran
`NullSandbox`, and it held nothing at all for `cache_policy` or
`stuck_detector` while every agent got the `Default*` ones. Caching looked off
when it was on.

Also here: guards for two patterns that each recurred across this work --
construction sites drifting apart, and an inert default nobody reports.
"""

import ast
import pathlib
import tempfile
from collections import defaultdict
from pathlib import Path

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.audit import audit
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.identity import posture
from mini_loop.sandbox import SeatbeltSandbox
from mini_loop.secrets import SecretRegistry
from mini_loop.storage import SQLiteStateStore

PACKAGE = Path(__file__).resolve().parent.parent / "mini_loop"
SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path, **kwargs):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR),
        FakeAsyncAnthropic(),
        **kwargs,
    )


SEAMS = {
    "sandbox": lambda agent: agent.toolset.sandbox,
    "secrets": lambda agent: agent.secrets,
    "cache_policy": lambda agent: agent.cache_policy,
    "stuck_detector": lambda agent: agent.stuck_detector,
}


def _mismatches(manager) -> list[str]:
    agent = manager.create().agent
    report = posture(manager)
    return [
        f"{seam}: reported {report.get(seam)!r}, running {type(read(agent)).__name__!r}"
        for seam, read in SEAMS.items()
        if report.get(seam) != type(read(agent)).__name__
    ]


def test_defaults_are_reported_as_what_runs(tmp_path):
    assert _mismatches(_manager(tmp_path)) == []


def test_a_configured_deployment_is_reported_accurately(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(
        tmp_path,
        state_store=store,
        secrets=SecretRegistry.from_environ(environ={}),
        sandbox=(
            SeatbeltSandbox(writable_roots=[tmp_path / "ws"])
            if SeatbeltSandbox.available()
            else None
        ),
    )
    assert _mismatches(manager) == []
    store.close()


def test_a_hardened_deployment_is_not_reported_as_bare(tmp_path):
    """A report that lies in the alarming direction is still a lie.

    The first version of the probe swallowed its own failure and reported every
    seam absent, turning a hardened deployment into a false alarm -- which
    trains an operator to ignore findings.
    """
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(
        tmp_path, state_store=store, secrets=SecretRegistry.from_environ(environ={})
    )
    report = posture(manager)
    assert report["secrets"] == "SecretRegistry"
    assert report["state_store"] == "SQLiteStateStore"
    checks = {f.check for f in audit(manager, environ={"PATH": "/usr/bin"})}
    assert "secret-masking" not in checks
    store.close()


def test_posture_does_not_depend_on_a_session_existing(tmp_path):
    """The report is asked for before anyone has created a session."""
    fresh = posture(_manager(tmp_path))
    used = _manager(tmp_path)
    used.create()
    assert fresh["cache_policy"] == posture(used)["cache_policy"]


# --- guard: construction sites must not drift apart ------------------------

CONSISTENT = {"AgentSession", "Toolset"}
#: Legitimately optional per call site, rather than an omission.
OPTIONAL = {"system", "model", "extra_state", "label", "state"}


def _construction_sites(target: str):
    sites = defaultdict(list)
    for path in PACKAGE.rglob("*.py"):
        for node in ast.walk(ast.parse(path.read_text())):
            if not isinstance(node, ast.Call):
                continue
            name = getattr(node.func, "id", None) or getattr(node.func, "attr", None)
            if name == target:
                sites[target].append(
                    (
                        f"{path.relative_to(PACKAGE)}:{node.lineno}",
                        {k.arg for k in node.keywords if k.arg},
                    )
                )
    return sites[target]


@pytest.mark.parametrize("target", sorted(CONSISTENT))
def test_construction_sites_pass_the_same_configuration(target):
    """Five separate defects came from one construction path being bypassed.

    Workflow workers built without masking or confinement, a cron restore that
    skipped rehydration, an authenticator set in one of three branches. Each was
    a site that quietly passed less than its siblings.
    """
    sites = _construction_sites(target)
    assert len(sites) >= 2, f"expected several {target}(...) sites, found {sites}"

    union = set().union(*(kwargs for _, kwargs in sites)) - OPTIONAL
    drifted = [
        f"{location} omits {sorted(union - kwargs)}"
        for location, kwargs in sites
        if union - kwargs
    ]
    assert not drifted, "construction sites disagree:\n  " + "\n  ".join(drifted)


# --- guard: an inert default must be visible -------------------------------

def test_every_inert_default_is_named_in_the_report(tmp_path):
    """The aggregate failure: each seam opts in, and silence is the default."""
    report = posture(_manager(tmp_path))
    inert = {
        seam: value
        for seam, value in report.items()
        if isinstance(value, str) and value.startswith(("Null", "Unavailable"))
    }
    assert inert, "a default deployment should report inert seams, not hide them"
    for seam in ("sandbox", "secrets", "state_store"):
        assert seam in report, f"{seam} is not reported at all"
