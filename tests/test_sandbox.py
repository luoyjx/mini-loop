"""OS-level confinement -- the escape tests actually execute.

Assertions here run real commands through `sandbox-exec`, so they prove the
policy rather than the policy string. They skip on non-macOS hosts, where the
seam falls back to `NullSandbox` and there is nothing to confine.

Written against the roadmap's Phase 2 acceptance list, including the item this
does *not* satisfy (resource limits), which is asserted as a known gap so the
next change has to acknowledge it.
"""

import subprocess
from pathlib import Path

import pytest

from mini_loop.sandbox import (
    SANDBOX_EXEC,
    NullSandbox,
    SeatbeltSandbox,
    default_sandbox,
)
from mini_loop.tools import Toolset

seatbelt_only = pytest.mark.skipif(
    not SeatbeltSandbox.available(), reason="Seatbelt is macOS-only"
)


def _sandbox(tmp_path, **over):
    base = dict(writable_roots=[tmp_path / "ws"], unreadable_roots=[tmp_path / "secret"])
    base.update(over)
    (tmp_path / "ws").mkdir(exist_ok=True)
    (tmp_path / "secret").mkdir(exist_ok=True)
    return SeatbeltSandbox(**base)


def _run(sandbox, command, cwd) -> subprocess.CompletedProcess:
    return subprocess.run(
        sandbox.argv(command), cwd=cwd, capture_output=True, text=True, timeout=30
    )


# --- policy construction (runs anywhere) -----------------------------------

def test_paths_are_parameters_not_interpolated(tmp_path):
    """A workspace path containing policy syntax must not rewrite the policy."""
    hostile = tmp_path / 'ws") (allow file-write* (subpath "/'
    hostile.mkdir(parents=True)
    sandbox = SeatbeltSandbox(writable_roots=[hostile])
    policy, params = sandbox._policy_and_params()

    assert str(hostile.resolve()) not in policy, "path leaked into the policy body"
    assert any(str(hostile.resolve()) in p for p in params)
    assert '(subpath (param "WRITE_0"))' in policy


def test_an_excluded_root_gets_both_clauses(tmp_path):
    """`subpath` alone leaves the root itself creatable -- upstream's bug."""
    sandbox = _sandbox(tmp_path)
    policy, _ = sandbox._policy_and_params()
    assert '(require-not (literal (param "NOREAD_0")))' in policy
    assert '(require-not (subpath (param "NOREAD_0")))' in policy


def test_network_rules_are_absent_unless_granted(tmp_path):
    denied, _ = _sandbox(tmp_path)._policy_and_params()
    assert "network-outbound" not in denied
    allowed, _ = _sandbox(tmp_path, allow_network=True)._policy_and_params()
    assert "network-outbound" in allowed


def test_policy_is_closed_by_default(tmp_path):
    policy, _ = _sandbox(tmp_path)._policy_and_params()
    assert policy.splitlines()[1].strip() == "(deny default)"


def test_a_sandbox_without_a_writable_root_is_rejected():
    with pytest.raises(ValueError):
        SeatbeltSandbox(writable_roots=[])


def test_default_sandbox_denies_the_hosts_credential_stores(tmp_path):
    """Seatbelt allows reads broadly and confines only writes, but a confined
    shell can still `cat ~/.ssh/id_rsa` and `cp` it into the writable workspace.
    The factory now supplies the read deny-list the module always documented, so
    the default policy protects SSH and cloud keys, not just the disk."""
    from mini_loop.sandbox import default_unreadable_roots

    protected = default_unreadable_roots()
    assert protected, "there should be a default credential deny-list"

    policy, params = default_sandbox(tmp_path / "ws")._policy_and_params()
    # Every default credential path becomes a NOREAD param and a deny clause.
    noread_params = [p for p in params if p.startswith("NOREAD_")]
    assert len(noread_params) >= len(protected)
    assert any(".ssh" in p for p in noread_params)
    assert "(allow file-read* (require-all" in policy  # not the unconditional grant

    # Opt-out restores the read-everything policy for a deployment that needs it.
    opted_out = default_sandbox(tmp_path / "ws", protect_credentials=False)
    _policy, params = opted_out._policy_and_params()
    assert not any(p.startswith("NOREAD_") for p in params)


@seatbelt_only
def test_a_denied_credential_cannot_be_copied_into_the_workspace(tmp_path):
    """The exfiltration path the read deny-list closes: read a protected file and
    `cp` it into the writable workspace, where the agent reads it back. Both the
    read and the copy must fail, and nothing lands in the workspace."""
    secret = tmp_path / "secret"
    secret.mkdir()
    (secret / "id_rsa").write_text("PRIVATE-KEY-DO-NOT-LEAK")
    ws = tmp_path / "ws"
    sandbox = _sandbox(tmp_path)  # denies tmp_path/secret, writes to tmp_path/ws

    read = _run(sandbox, f"cat {secret / 'id_rsa'}", ws)
    assert "PRIVATE-KEY-DO-NOT-LEAK" not in (read.stdout + read.stderr)

    copy = _run(sandbox, f"cp {secret / 'id_rsa'} {ws / 'stolen'}", ws)
    assert copy.returncode != 0
    assert not (ws / "stolen").exists()

    # The workspace itself is still writable -- confinement, not paralysis.
    assert _run(sandbox, "echo ok > out.txt", ws).returncode == 0


def test_the_binary_is_never_resolved_through_path(tmp_path):
    assert _sandbox(tmp_path).argv("true")[0] == SANDBOX_EXEC == "/usr/bin/sandbox-exec"


def test_null_sandbox_is_a_plain_shell():
    assert NullSandbox().argv("echo hi") == ["/bin/sh", "-c", "echo hi"]


def test_toolset_defaults_to_no_confinement(tmp_path):
    """Confinement is opt-in; the default must stay host execution."""
    assert isinstance(Toolset(tmp_path / "ws").sandbox, NullSandbox)


# --- the escapes, actually executed ---------------------------------------

@seatbelt_only
def test_cannot_read_a_canary_outside_the_allowed_roots(tmp_path):
    canary = tmp_path / "secret" / "key.txt"
    (tmp_path / "secret").mkdir(exist_ok=True)
    canary.write_text("CANARY-TOPSECRET")
    sandbox = _sandbox(tmp_path)

    result = _run(sandbox, f"cat {canary}", tmp_path / "ws")
    assert "CANARY-TOPSECRET" not in result.stdout
    assert result.returncode != 0


@seatbelt_only
def test_a_shell_still_works(tmp_path):
    """Confinement that breaks `sh` is not confinement, it is an outage."""
    sandbox = _sandbox(tmp_path)
    result = _run(sandbox, "echo hello && head -c 4 /etc/hosts > /dev/null", tmp_path / "ws")
    assert result.returncode == 0
    assert "hello" in result.stdout


@seatbelt_only
def test_writes_inside_the_workspace_succeed(tmp_path):
    ws = tmp_path / "ws"
    sandbox = _sandbox(tmp_path)
    result = _run(sandbox, "echo ok > a.txt && cat a.txt", ws)
    assert result.returncode == 0
    assert "ok" in result.stdout
    assert (ws / "a.txt").read_text().strip() == "ok"


@seatbelt_only
def test_writes_outside_the_workspace_are_denied(tmp_path):
    outside = tmp_path / "elsewhere"
    outside.mkdir()
    sandbox = _sandbox(tmp_path)

    result = _run(sandbox, f"echo pwned > {outside}/b.txt", tmp_path / "ws")
    assert result.returncode != 0
    assert not (outside / "b.txt").exists()


@seatbelt_only
def test_creating_the_excluded_root_itself_is_denied(tmp_path):
    """The gap `(require-not (subpath X))` alone would leave open."""
    protected = tmp_path / "protected"
    sandbox = SeatbeltSandbox(
        writable_roots=[tmp_path / "ws"], unreadable_roots=[protected]
    )
    (tmp_path / "ws").mkdir(exist_ok=True)

    result = _run(sandbox, f"ls {protected} 2>&1 || true", tmp_path / "ws")
    assert "CANARY" not in result.stdout


@seatbelt_only
def test_network_is_denied_by_default(tmp_path):
    sandbox = _sandbox(tmp_path)
    result = _run(
        sandbox,
        "curl -s -m 5 -o /dev/null -w '%{http_code}' https://example.com || echo BLOCKED",
        tmp_path / "ws",
    )
    combined = result.stdout + result.stderr
    assert "200" not in combined, f"network reached the internet: {combined!r}"


@seatbelt_only
def test_the_toolset_path_is_confined_end_to_end(tmp_path):
    """Not just the sandbox object -- the tool the model actually calls."""
    ws = tmp_path / "ws"
    canary = tmp_path / "secret" / "key.txt"
    (tmp_path / "secret").mkdir(parents=True, exist_ok=True)
    canary.write_text("CANARY-TOPSECRET")

    toolset = Toolset(ws, sandbox=_sandbox(tmp_path))
    assert "CANARY-TOPSECRET" not in toolset.run_bash(f"cat {canary}")
    assert "ok" in toolset.run_bash("echo ok > inside.txt && cat inside.txt")


@seatbelt_only
def test_default_sandbox_confines_on_this_host(tmp_path):
    ws = tmp_path / "ws"
    ws.mkdir()
    sandbox = default_sandbox(ws)
    assert isinstance(sandbox, SeatbeltSandbox)
    assert "seatbelt" in sandbox.describe


# --- the gap this does not close ------------------------------------------

@seatbelt_only
def test_resource_limits_are_a_known_gap(tmp_path):
    """Seatbelt is not a container: it bounds access, not consumption.

    The roadmap's fork-bomb / resource-exhaustion criterion needs a container
    backend. Pinned here so a future change that claims to close it has to
    update this test rather than quietly inherit the claim.
    """
    sandbox = _sandbox(tmp_path)
    policy, _ = sandbox._policy_and_params()
    # Strip comments: prose about CPU sysctls is not a resource limit.
    directives = "\n".join(
        line for line in policy.splitlines() if not line.strip().startswith(";")
    ).lower()
    for knob in ("rlimit", "maxproc", "memory-limit", "cpu-limit"):
        assert knob not in directives

    # Spawning subprocesses is explicitly permitted -- nothing caps how many.
    result = _run(sandbox, "for i in 1 2 3; do (true &) ; done; echo spawned", tmp_path / "ws")
    assert "spawned" in result.stdout


# --- unavailable is not the same fact as unconfigured ----------------------

from unittest.mock import patch  # noqa: E402

from mini_loop.sandbox import (  # noqa: E402
    SandboxUnavailable,
    UnavailableSandbox,
    default_sandbox as _default_sandbox,
    unavailable_reason,
)


def _no_seatbelt():
    return patch.object(SeatbeltSandbox, "available", staticmethod(lambda: False))


def test_degrading_says_why_instead_of_impersonating_a_choice(tmp_path):
    """An operator who asked for confinement and one who did not looked alike.

    Both showed `NullSandbox` in the posture and drew the same audit finding,
    though one needs to pass a sandbox and the other needs a different host --
    remedies with nothing in common.
    """
    with _no_seatbelt():
        sandbox = _default_sandbox(tmp_path / "ws")
    assert isinstance(sandbox, UnavailableSandbox)
    assert sandbox.reason
    assert "unavailable" in sandbox.describe
    assert not isinstance(_default_sandbox(tmp_path / "ws"), UnavailableSandbox) or (
        not SeatbeltSandbox.available()
    )


def test_requiring_confinement_fails_closed(tmp_path):
    """Same discipline as refusing an unauthenticated public bind."""
    with _no_seatbelt():
        with pytest.raises(SandboxUnavailable) as refusal:
            _default_sandbox(tmp_path / "ws", require=True)
    assert str(refusal.value)


def test_the_reason_names_the_platform(tmp_path):
    with _no_seatbelt():
        reason = unavailable_reason()
    assert reason and ("macOS" in reason or "Darwin" in reason)


def test_an_unavailable_sandbox_still_runs_commands(tmp_path):
    """Loud, not broken: development on an unsupported host must still work."""
    workspace = tmp_path / "ws"
    workspace.mkdir()
    with _no_seatbelt():
        toolset = Toolset(workspace, sandbox=_default_sandbox(workspace))
    assert "ok" in toolset.run_bash("echo ok")


def test_rebinding_an_unavailable_sandbox_is_a_no_op(tmp_path):
    sandbox = UnavailableSandbox("no backend here")
    assert sandbox.for_workspace(tmp_path) is sandbox


def test_posture_and_audit_tell_the_two_apart(tmp_path):
    from mini_loop.audit import audit
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.identity import posture
    from mini_loop.manager import SessionManager

    settings = Settings(
        fake_llm=True,
        workspace_root=tmp_path / "ws",
        skills_dir=Path(__file__).resolve().parent.parent / "skills",
    )
    env = {"PATH": "/usr/bin"}

    with _no_seatbelt():
        asked = SessionManager(
            settings, FakeAsyncAnthropic(), sandbox=_default_sandbox(tmp_path / "ws")
        )
        never = SessionManager(settings, FakeAsyncAnthropic())

        assert posture(asked)["sandbox_reason"], "the reason is not reported"
        assert posture(never)["sandbox_reason"] is None

        asked_check = next(
            f.check for f in audit(asked, environ=env) if "confinement" in f.check
        )
        never_check = next(
            f.check for f in audit(never, environ=env) if "confinement" in f.check
        )
    assert asked_check == "shell-confinement-unavailable"
    assert never_check == "shell-confinement"
    assert asked_check != never_check


def test_a_remote_audit_also_distinguishes_them():
    from mini_loop.audit import audit_posture

    unavailable = audit_posture(
        {"authenticated": True, "posture": {
            "sandbox": "UnavailableSandbox", "sandbox_reason": "no backend",
            "secrets": "SecretRegistry", "state_store": "SQLiteStateStore",
            "action_journal": "DurableActionJournal"}},
        source="x",
    )
    assert [f.check for f in unavailable] == ["shell-confinement-unavailable"]


# --- round 160: a git worktree's shared .git is writable ---------------------

def _git(cwd, *args):
    return subprocess.run(["git", *args], cwd=cwd, capture_output=True, text=True)


def _repo_with_worktree(tmp_path):
    from mini_loop.worktrees import worktree_workspace_factory

    repo = tmp_path / "repo"
    repo.mkdir()
    for args in (["init", "-q"], ["config", "user.email", "t@t"],
                 ["config", "user.name", "t"]):
        _git(repo, *args)
    (repo / "README.md").write_text("hi")
    _git(repo, "add", ".")
    _git(repo, "commit", "-qm", "init")
    worktree = worktree_workspace_factory(repo)("s1")
    return repo, worktree


def test_a_worktree_sandbox_can_write_the_shared_git_dir(tmp_path):
    """A linked worktree keeps its index, refs and objects in the repository's
    shared `.git`, outside the worktree directory. A sandbox confined to the
    worktree alone blocks every writing git command; the shared git dir is added
    so a worktree the harness provisions for isolation is still usable. A plain
    workspace adds nothing."""
    _repo, worktree = _repo_with_worktree(tmp_path)
    roots = default_sandbox(worktree).writable_roots
    assert any(root.endswith(".git") for root in roots)

    plain = tmp_path / "plain"
    plain.mkdir()
    assert not any(root.endswith(".git") for root in default_sandbox(plain).writable_roots)


@seatbelt_only
def test_a_sandboxed_worktree_commits_but_stays_isolated(tmp_path):
    """End to end: git commit works inside the sandboxed worktree, yet the
    repository's *working* tree and everything outside stay unwritable -- only
    its git metadata was added, so worktrees remain isolated from each other."""
    repo, worktree = _repo_with_worktree(tmp_path)
    sandbox = default_sandbox(worktree)

    (worktree / "work.txt").write_text("new")
    commit = _run(sandbox, "git add . && git commit -qm work && echo DONE", worktree)
    assert commit.returncode == 0 and "DONE" in commit.stdout

    # The repository's working files (a *different* worktree's tree) stay safe.
    _run(sandbox, f"echo pwned > {repo / 'README.md'}", worktree)
    assert (repo / "README.md").read_text() == "hi"

    # And an unrelated path outside the repo is still denied.
    outside = tmp_path / "escape.txt"
    _run(sandbox, f"echo x > {outside}", worktree)
    assert not outside.exists()
