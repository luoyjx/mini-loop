"""Build identity and remote posture audit.

A process on a port tells you nothing about the code inside it. A server started
from this package once survived fourteen hours of `pkill -f "python -m
mini_loop"` -- the real command line has a capital P, so the pattern never
matched -- and a full round of measurements was taken against a build that
predated the code under test. These tests make "am I talking to my build?" a
checkable fact rather than an assumption.
"""

import hashlib
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from mini_loop.audit import audit_posture
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.identity import build_id, posture, runtime_identity
from mini_loop.manager import SessionManager
from mini_loop.sandbox import SeatbeltSandbox
from mini_loop.secrets import SecretRegistry
from mini_loop.server import create_app
from mini_loop.storage import SQLiteStateStore

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"
PACKAGE = Path(__file__).resolve().parent.parent / "mini_loop"


def _manager(tmp_path, **kwargs):
    settings = Settings(
        fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR
    )
    return SessionManager(settings, FakeAsyncAnthropic(), **kwargs)


# --- the fingerprint --------------------------------------------------------

def test_the_build_id_is_stable_within_a_process():
    assert build_id() == build_id()
    assert len(build_id()) == 12


def test_the_build_id_covers_the_package_source():
    """It must move when the code moves, or it answers nothing."""
    digest = hashlib.sha256()
    for path in sorted(PACKAGE.rglob("*.py")):
        if "__pycache__" in path.parts:
            continue
        digest.update(path.relative_to(PACKAGE).as_posix().encode())
        digest.update(path.read_bytes())
    assert build_id() == digest.hexdigest()[:12]


def test_an_edit_changes_the_fingerprint(tmp_path):
    """Recomputed over a copy: an added byte must produce a different id."""

    def fingerprint(root: Path) -> str:
        digest = hashlib.sha256()
        for path in sorted(root.rglob("*.py")):
            if "__pycache__" in path.parts:
                continue
            digest.update(path.relative_to(root).as_posix().encode())
            digest.update(path.read_bytes())
        return digest.hexdigest()[:12]

    import shutil

    copy = tmp_path / "pkg"
    shutil.copytree(PACKAGE, copy, ignore=shutil.ignore_patterns("__pycache__"))
    before = fingerprint(copy)
    (copy / "agent.py").write_text((copy / "agent.py").read_text() + "\n# edit\n")
    assert fingerprint(copy) != before


def test_identity_reports_the_process_it_came_from():
    import os

    identity = runtime_identity()
    assert identity["pid"] == os.getpid()
    assert identity["uptime_s"] >= 0


# --- posture ---------------------------------------------------------------

def test_posture_reports_the_null_defaults(tmp_path):
    report = posture(_manager(tmp_path))
    assert report["sandbox"] in ("None", "NullSandbox")
    assert report["secrets"] in ("None", "NullSecretRegistry")
    assert report["state_store"] == "NullStateStore"
    assert report["action_journal"] == "InMemoryActionJournal"
    assert report["authenticated"] is False


def test_posture_reports_a_hardened_process(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(
        tmp_path,
        state_store=store,
        secrets=SecretRegistry.from_environ(environ={}),
        sandbox=SeatbeltSandbox(writable_roots=[tmp_path / "ws"]),
    )
    report = posture(manager)
    assert report["state_store"] == "SQLiteStateStore"
    assert report["action_journal"] == "DurableActionJournal"
    assert report["sandbox"] == "SeatbeltSandbox"
    store.close()


# --- auditing a running server ---------------------------------------------

def test_a_default_server_is_audited_through_its_own_report(tmp_path):
    manager = _manager(tmp_path)
    app = create_app(manager=manager)
    with TestClient(app) as client:
        report = client.get("/healthz").json()

    assert report["build"] == build_id()
    findings = audit_posture(report, source="http://x")
    checks = {f.check for f in findings}
    assert {"authentication", "shell-confinement", "secret-masking"} <= checks
    assert all("http://x" in f.detail for f in findings)


def test_a_hardened_server_reports_nothing_blocking(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(
        tmp_path,
        state_store=store,
        # A hardened server registers from the environment it actually runs in:
        # the posture counts credential-shaped variables the registry misses
        # against the real `os.environ` (round 125), so `from_environ()` -- the
        # recommended path -- is what leaves nothing uncovered. An empty-environ
        # registry looks configured but covers none of the host's real
        # credentials, which is precisely the leak this audit now reports.
        secrets=SecretRegistry.from_environ(),
        sandbox=SeatbeltSandbox(writable_roots=[tmp_path / "ws"]),
    )
    app = create_app(manager=manager)
    from mini_loop.auth import TokenAuth

    with TestClient(app) as client:
        app.state.auth = TokenAuth({"tok-abcdefghijkl": "alice"})
        report = client.get(
            "/healthz", headers={"Authorization": "Bearer tok-abcdefghijkl"}
        ).json()

    blocking = [f for f in audit_posture(report, source="x") if f.severity == "high"]
    assert not blocking, [f.check for f in blocking]
    store.close()


def test_a_server_without_a_posture_is_reported_not_passed():
    """A remote audit that silently skipped checks would read as clean."""
    findings = audit_posture({"status": "ok"}, source="http://old")
    assert [f.check for f in findings] == ["posture-unavailable"]
    assert findings[0].severity == "medium"


def test_the_identity_check_catches_a_stale_server():
    """The guard the fourteen-hour process defeated."""
    stale = {"build": "0" * 12, "posture": {}}
    assert stale["build"] != build_id()
