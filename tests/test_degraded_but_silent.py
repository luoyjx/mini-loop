"""Degrade when the failing thing records; refuse when it writes.

That rule came out of the previous round. Applied as a sweep over every broad
`except` in the package, it separates cleanly: handlers around trajectories,
verifiers and tool bodies all report what they swallowed. Two did not, and both
are on the write side.

**A state store whose writes fail.** `_capture_event` caught the error into
`self._persist_error` under a comment reading "degrade to a reported error,
never a stalled agent -- same contract the trajectory sink already follows."
The trajectory sink genuinely reports; this field was assigned and read by
nothing. A store that opens and then fails every write therefore produced:

    the run: SUCCEEDED
    session.status      : idle
    messages in memory  : 4
    messages persisted  : 0
    posture says state_store = 'BrokenStore'
    audit 'durable-state' flagged: False

Everything healthy, nothing on disk, every session unrecoverable. Round 26 moved
`posture()` from *configured* to *installed*; this is the level under that --
installed is not working.

**A secret whose lookup fails.** The name stays registered, so the deployment
believes it is masked, while `mask()` has no value to search for and the
credential passes through every sink. Registered-but-unreadable is worse than
unregistered, because it looks safe.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.audit import audit, audit_posture
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.identity import posture
from mini_loop.secrets import SecretRegistry
from mini_loop.storage import SQLiteStateStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
CLEAN_ENV = {"PATH": "/usr/bin"}


class BrokenStore(SQLiteStateStore):
    """Opens fine, then every write fails: disk full, permissions, locked."""

    def append_event(self, *args, **kwargs):
        raise OSError("[Errno 28] No space left on device")

    def append_messages(self, *args, **kwargs):
        raise OSError("[Errno 28] No space left on device")


def _manager(tmp_path, **kwargs):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
        **kwargs,
    )


# --- a store that is installed but not working ----------------------------

def test_a_failing_store_still_lets_the_agent_finish(tmp_path):
    """The degrade half of the rule, which was already right."""
    store = BrokenStore(tmp_path / "state.db")
    session = _manager(tmp_path, state_store=store).create()
    assert asyncio.run(session.agent.run("do some work"))
    assert session.status != "error"
    store.close()


def test_a_failing_store_is_reported_on_the_session(tmp_path):
    store = BrokenStore(tmp_path / "state.db")
    session = _manager(tmp_path, state_store=store).create()
    asyncio.run(session.agent.run("do some work"))
    assert "No space left" in (session.persist_error or "")
    store.close()


def test_a_failing_store_reaches_the_posture(tmp_path):
    store = BrokenStore(tmp_path / "state.db")
    manager = _manager(tmp_path, state_store=store)
    session = manager.create()
    asyncio.run(session.agent.run("do some work"))
    report = posture(manager)
    assert report["state_store_error"], "posture reports the class, not the health"
    store.close()


def test_a_failing_store_blocks_the_audit(tmp_path):
    """`high`, so `python -m mini_loop.audit` exits non-zero and gates a deploy."""
    store = BrokenStore(tmp_path / "state.db")
    manager = _manager(tmp_path, state_store=store)
    asyncio.run(manager.create().agent.run("do some work"))
    findings = {f.check: f for f in audit(manager, environ=CLEAN_ENV)}
    assert "durable-state-failing" in findings
    assert findings["durable-state-failing"].severity == "high"
    assert "No space left" in findings["durable-state-failing"].detail
    store.close()


def test_a_remote_server_with_a_failing_store_is_audited_too(tmp_path):
    findings = {
        f.check for f in audit_posture(
            {"authenticated": True,
             "posture": {"state_store": "SQLiteStateStore",
                         "state_store_error": "OSError: disk gone",
                         "sandbox": "SeatbeltSandbox", "secrets": "SecretRegistry"}},
            source="http://host:1",
        )
    }
    assert "durable-state-failing" in findings


def test_a_working_store_draws_no_finding(tmp_path):
    """A check that always fires is a check nobody reads."""
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, state_store=store)
    asyncio.run(manager.create().agent.run("do some work"))
    checks = {f.check for f in audit(manager, environ=CLEAN_ENV)}
    assert "durable-state-failing" not in checks
    assert "durable-state" not in checks
    store.close()


# --- a secret that is registered but unreadable ---------------------------

def test_a_secret_that_cannot_be_read_is_reported(tmp_path):
    def broken_vault():
        raise RuntimeError("vault unreachable")

    registry = SecretRegistry()
    registry.register("VAULT_API_KEY", broken_vault)
    registry.mask("some output")  # forces resolution

    assert registry.unresolved() == ("VAULT_API_KEY",)
    findings = {f.check: f for f in audit(
        _manager(tmp_path, secrets=registry), environ=CLEAN_ENV
    )}
    assert "secret-unresolved" in findings
    assert findings["secret-unresolved"].severity == "high"
    assert "VAULT_API_KEY" in findings["secret-unresolved"].detail


def test_an_unreadable_secret_does_not_stall_the_agent(tmp_path):
    """Still the degrade half: a broken vault must not stop a run."""
    def broken_vault():
        raise RuntimeError("vault unreachable")

    registry = SecretRegistry()
    registry.register("VAULT_API_KEY", broken_vault)
    session = _manager(tmp_path, secrets=registry).create()
    assert asyncio.run(session.agent.run("do some work"))


def test_a_short_secret_is_reported_rather_than_silently_unmasked(tmp_path):
    registry = SecretRegistry()
    registry.register("TINY_TOKEN", "abc")
    registry.mask("some output")
    assert registry.short_values() == ("TINY_TOKEN",)
    checks = {f.check for f in audit(
        _manager(tmp_path, secrets=registry), environ=CLEAN_ENV
    )}
    assert "secret-too-short" in checks


def test_healthy_secrets_draw_no_finding(tmp_path):
    registry = SecretRegistry.from_environ(environ={"REAL_API_KEY": "sk-long-enough-value"})
    registry.mask("some output")
    checks = {f.check for f in audit(
        _manager(tmp_path, secrets=registry), environ=CLEAN_ENV
    )}
    assert "secret-unresolved" not in checks
    assert "secret-too-short" not in checks
