"""What the command blocklist does not do, pinned so nobody forgets.

`safe_path` was probed adversarially first and held everything -- `..`,
absolute paths, null bytes, and symlinks both to a file and to a parent
directory, planted inside the workspace the way `run_bash` could plant them.
That is a negative result and it is recorded as one.

`DANGEROUS` is the opposite. It is substring matching on a command string, and
a shell has unlimited ways to spell the same instruction:

    'rm -rf /'          blocked=True
    'rm  -rf  /'        blocked=False    <- a typo defeats it
    'rm -rf $HOME'      blocked=False
    '$(echo rm) -rf /'  blocked=False
    "r''m -rf /"        blocked=False
    'find / -delete'    blocked=False

The risk is not that the list is weak -- a blocklist over a shell cannot be
strong -- it is that its presence reads as a mitigating control. An operator who
sees it may count it as one, and the thing that actually confines a shell
(`SeatbeltSandbox`, or a container) is opt-in and macOS-only.

So the bypasses are asserted *as bypasses*. A future change that "hardens" the
list will fail here, which is the point: the failure should prompt reading why
it cannot work, not another pattern.
"""

import pathlib
import tempfile

import pytest

from mini_loop.audit import audit
from mini_loop.tools import DANGEROUS, Toolset, looks_dangerous


@pytest.fixture
def toolset(tmp_path):
    workspace = tmp_path / "ws"
    workspace.mkdir()
    return Toolset(workspace)


# --- the containment that does hold ---------------------------------------

ESCAPES = [
    "../secret.txt",
    "../../etc/passwd",
    "/etc/passwd",
    "a/../../secret.txt",
    "./../secret.txt",
    "link",                 # symlink to a file outside
    "dirlink/secret.txt",   # symlink to a parent directory
]


@pytest.fixture
def planted(tmp_path):
    """A workspace with symlinks out of it, as `run_bash` could leave behind."""
    root = tmp_path.resolve()
    workspace = root / "ws"
    workspace.mkdir()
    (root / "secret.txt").write_text("TOP SECRET")
    (workspace / "link").symlink_to(root / "secret.txt")
    (workspace / "dirlink").symlink_to(root)
    return Toolset(workspace)


@pytest.mark.parametrize("attempt", ESCAPES)
def test_no_path_escapes_the_workspace(planted, attempt):
    with pytest.raises(ValueError, match="escapes workspace"):
        planted.safe_path(attempt)


@pytest.mark.parametrize("attempt", ESCAPES)
def test_no_file_tool_reads_through_an_escape(planted, attempt):
    assert "TOP SECRET" not in planted.run_read(attempt)


def test_a_null_byte_does_not_reach_the_filesystem(planted):
    with pytest.raises(ValueError):
        planted.safe_path("a\x00/../../secret.txt")


@pytest.mark.parametrize("literal", ["~/.ssh/id_rsa", "....//....//x", "ok.txt"])
def test_names_that_only_look_like_escapes_stay_inside(planted, literal):
    """`~` and `....` are ordinary directory names on POSIX, not traversal."""
    assert planted.safe_path(literal).is_relative_to(planted.workspace)


# --- the filtering that does not, asserted as such ------------------------

def test_the_literal_spellings_are_refused(toolset):
    for pattern in DANGEROUS:
        assert looks_dangerous(f"{pattern} something")


def test_extra_whitespace_no_longer_defeats_it():
    """A doubled space is a typo, and a typo guard that a typo defeats is none."""
    assert looks_dangerous("rm  -rf  /")
    assert looks_dangerous("rm\t-rf\t/")
    assert looks_dangerous("  sudo   apt install  ")


KNOWN_BYPASSES = [
    "rm -rf $HOME",
    "$(echo rm) -rf /",
    "r''m -rf /",
    "find / -delete",
    "python -c \"import shutil,os;shutil.rmtree(os.path.expanduser('~'))\"",
    "cat script.sh | sh",
]


@pytest.mark.parametrize("command", KNOWN_BYPASSES)
def test_the_blocklist_is_not_a_security_control(command):
    """Asserted as a bypass on purpose.

    If this test starts failing, someone added a pattern. The right response is
    not to add more -- it is to read why a blocklist over a shell cannot work,
    and to reach for the sandbox instead.
    """
    assert not looks_dangerous(command), (
        f"{command!r} is now blocked; the list grew. A blocklist cannot enumerate "
        "the ways a shell spells an instruction -- confine it instead."
    )


def test_the_audit_says_the_blocklist_is_not_confinement(tmp_path):
    from mini_loop import SessionManager, Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=pathlib.Path(__file__).resolve().parent.parent / "skills"),
        FakeAsyncAnthropic(),
    )
    findings = {f.check: f for f in audit(manager, environ={"PATH": "/usr/bin"})}
    detail = findings["shell-confinement"].detail
    assert "typo guard" in detail and "not confinement" in detail, (
        "an operator reading the audit must not count the blocklist as a control"
    )
