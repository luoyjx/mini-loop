"""Two documented security properties that no test would have missed.

Found by `tools/verify_guards.py`: break a hardening on purpose, and if the
suite still passes, the protection is real in the code and imaginary in the test
suite. Nineteen hardenings were mutated; these two survived.

**Policy injection through a workspace path.** `sandbox.py` states it plainly:
"Paths are parameters, never string interpolation. Roots are passed to
sandbox-exec as `-D KEY=VALUE`... so a workspace path containing policy syntax
cannot rewrite the policy." Rewriting `argv()` to interpolate the paths into the
policy text passed all of `test_sandbox.py`, because every test used a benign
temp directory. The property only fails on a path that carries SBPL syntax --
exactly the case the design exists for and the one nobody wrote down.

**Reconciling to a nonsense status.** `reconcile()` is the single transition
permitted out of `unknown`, and it validates its target. Deleting that
validation passed `test_reconcile.py`: an action could be resolved to any string
at all, including back into `unknown` or into a status the rest of the system
does not handle.
"""

import pathlib

import pytest

from mini_loop.actions import (
    RECONCILED_RESULT,
    TERMINAL_STATUSES,
    UNKNOWN,
    DurableActionJournal,
)
from mini_loop.sandbox import SANDBOX_EXEC, SeatbeltSandbox
from mini_loop.storage import SQLiteStateStore

#: Directory names that are also Seatbelt policy syntax.
HOSTILE_NAMES = [
    'ws") (allow default) (deny nothing',
    "ws(allow file-write*)",
    'ws"',
    "ws)(version 1)(allow default",
    "ws\n(allow default)",
]


@pytest.mark.skipif(not SeatbeltSandbox.available(), reason="macOS Seatbelt only")
@pytest.mark.parametrize("name", HOSTILE_NAMES)
def test_a_workspace_path_cannot_reach_the_policy_text(tmp_path, name):
    """The decisive property: the path is never *in* the policy.

    Not "the policy still parses" -- that is a weaker claim that interpolation
    can satisfy by accident. If the path never appears in the `-p` argument,
    there is nothing for it to break out of.
    """
    workspace = tmp_path / name
    workspace.mkdir(parents=True)
    argv = SeatbeltSandbox(writable_roots=[workspace]).argv("echo hi")

    policy = argv[argv.index("-p") + 1]
    assert str(workspace) not in policy, "the workspace path was interpolated"
    # Only fragments the hostile *name* carries. `allow file-write*` is in the
    # base policy legitimately -- asserting its absence tests the policy, not
    # the injection.
    for fragment in ("allow default", "deny nothing", "(version 1)(allow"):
        assert fragment not in policy, f"{fragment!r} reached the policy"

    passed = [argv[i + 1] for i, arg in enumerate(argv) if arg == "-D"]
    assert any(str(workspace) in value for value in passed), (
        "the path must still be delivered, as a parameter"
    )


@pytest.mark.skipif(not SeatbeltSandbox.available(), reason="macOS Seatbelt only")
def test_the_policy_still_denies_by_default_with_a_hostile_path(tmp_path):
    workspace = tmp_path / 'ws") (allow default'
    workspace.mkdir(parents=True)
    argv = SeatbeltSandbox(writable_roots=[workspace]).argv("echo hi")
    policy = argv[argv.index("-p") + 1]
    assert "(deny default)" in policy
    assert argv[0] == SANDBOX_EXEC


@pytest.mark.skipif(not SeatbeltSandbox.available(), reason="macOS Seatbelt only")
def test_a_hostile_path_is_actually_confined(tmp_path):
    """End to end, not only in the argv: the escape must not work."""
    import subprocess

    workspace = tmp_path / 'ws") (allow default) (deny nothing'
    workspace.mkdir(parents=True)
    outside = tmp_path / "outside.txt"

    argv = SeatbeltSandbox(writable_roots=[workspace]).argv(
        f"echo pwned > {outside}"
    )
    subprocess.run(argv, capture_output=True, timeout=30)
    assert not outside.exists(), "the path escaped the policy and got a write"


# --- reconciliation validates what it is asked to write -------------------

def _journal(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    return DurableActionJournal(store), store


def _unknown_action(journal, action_id="a1"):
    journal.begin(
        action_id=action_id, session_id="s1", message_id="m1",
        tool_use_id="t1", tool_name="run_bash", input_value={"command": "x"},
    )
    journal.mark_inflight_unknown("s1")
    assert journal.get(action_id).status == UNKNOWN
    return action_id


@pytest.mark.parametrize("bogus", ["succeeded", "", "UNKNOWN", "in_progress", "started"])
def test_reconciling_to_an_unhandled_status_is_refused(tmp_path, bogus):
    journal, store = _journal(tmp_path)
    action_id = _unknown_action(journal)
    with pytest.raises(ValueError, match="reconcile"):
        journal.reconcile(action_id, status=bogus, result="evidence")
    assert journal.get(action_id).status == UNKNOWN, "the record was rewritten anyway"
    store.close()


def test_reconciling_back_into_unknown_is_refused(tmp_path):
    """`unknown` is not a resolution; allowing it makes the state absorbing."""
    journal, store = _journal(tmp_path)
    action_id = _unknown_action(journal)
    with pytest.raises(ValueError):
        journal.reconcile(action_id, status=UNKNOWN, result="still no idea")
    store.close()


@pytest.mark.parametrize("status", sorted(TERMINAL_STATUSES))
def test_every_terminal_status_is_reconcilable(tmp_path, status):
    """The other half: the check must not be so tight it blocks real evidence."""
    journal, store = _journal(tmp_path)
    action_id = _unknown_action(journal)
    assert journal.reconcile(action_id, status=status, result="checked").status == status
    store.close()


def test_a_settled_action_is_not_rewritten_by_reconciliation(tmp_path):
    """Reconciliation resolves `unknown`, and only `unknown`."""
    journal, store = _journal(tmp_path)
    action_id = "a2"
    journal.begin(
        action_id=action_id, session_id="s1", message_id="m1",
        tool_use_id="t1", tool_name="run_bash", input_value={"command": "x"},
    )
    journal.finish(action_id, status="completed", result="real output")

    journal.reconcile(action_id, status="failed", result="wrong evidence")
    settled = journal.get(action_id)
    assert settled.status == "completed"
    assert "real output" in settled.result
    store.close()
