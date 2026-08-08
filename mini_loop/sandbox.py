"""OS-level confinement for shell commands.

`run_bash` sets `cwd` to the session workspace, but `shell=True` means the model
can `cd /` and read or write anything the host user can. The README has always
said as much -- "still require normal OS-level isolation if untrusted users can
submit prompts" -- and this module is that isolation for the platform we can
actually verify on.

The policy is modelled on the OpenAI Codex CLI sandbox
(``codex-rs/sandboxing/src/seatbelt_base_policy.sbpl`` and ``seatbelt.rs``),
whose shape encodes several non-obvious decisions:

* **Deny by default, then allow.** The base policy opens with ``(deny default)``
  and re-grants only what a shell needs to function.
* **Reads are broad, writes are narrow.** Codex allows ``file-read*`` globally
  unless told otherwise, because a process that cannot read ``/bin/sh`` or the
  dynamic linker is not a shell. Confinement lives on the *write* side, plus an
  explicit read deny-list for the paths that actually matter.
* **Paths are parameters, never string interpolation.** Roots are passed to
  ``sandbox-exec`` as ``-D KEY=VALUE`` and referenced as ``(param "KEY")``, so a
  workspace path containing policy syntax cannot rewrite the policy.
* **An excluded path needs two clauses.** ``(require-not (subpath X))`` alone
  leaves a gap: it does not cover creating ``X`` itself. Codex pairs it with
  ``(require-not (literal X))``; the comment upstream cites ``mkdir .codex``.
* **Network is additive.** Denied unless network rules are appended, which is
  why the upstream policy lives in a second file.
* **The binary path is hardcoded.** ``/usr/bin/sandbox-exec``, never resolved
  through ``PATH``.

**What this does not do.** Seatbelt is not a container: there are no CPU,
memory, PID or wall-clock limits, so it does not stop a fork bomb -- the
roadmap's resource-exhaustion criterion needs `DockerWorkspace`, not this. It
confines one process tree on one machine, and only on macOS. `NullSandbox`
remains the default and is trusted-callers-only.
"""

from __future__ import annotations

import os
import platform
import shlex
from collections.abc import Iterable, Sequence
from pathlib import Path
from typing import Protocol

__all__ = [
    "SANDBOX_EXEC",
    "Sandbox",
    "NullSandbox",
    "UnavailableSandbox",
    "SandboxUnavailable",
    "SeatbeltSandbox",
    "default_sandbox",
    "default_unreadable_roots",
]

# Hardcoded, never resolved through PATH.
SANDBOX_EXEC = "/usr/bin/sandbox-exec"
_SHELL = "/bin/sh"

# Re-granted after `(deny default)` so a shell can still start, read system
# libraries, and report a sane machine. Trimmed from the upstream base policy to
# what a POSIX shell and common build tooling touch.
_BASE_POLICY = """(version 1)
(deny default)

; child processes inherit this policy
(allow process-exec)
(allow process-fork)
(allow signal (target same-sandbox))
(allow process-info* (target same-sandbox))

; a process that cannot write /dev/null cannot redirect output
(allow file-write-data
  (require-all
    (path "/dev/null")
    (vnode-type CHARACTER-DEVICE)))

; machine facts: without these, tooling that probes the CPU or hostname fails
(allow sysctl-read)
(allow mach-lookup)
(allow ipc-posix-sem)
(allow user-preference-read)

; interactive tools expect a tty
(allow pseudo-tty)
(allow file-read* file-write* file-ioctl (literal "/dev/ptmx"))
"""

# Appended only when network access is granted. Its absence is the denial.
_NETWORK_POLICY = """
(allow network-outbound)
(allow network-inbound)
(allow system-socket)
"""


class Sandbox(Protocol):
    """Turn a shell command into the argv that should actually be executed."""

    #: True when commands are genuinely confined by this backend. Declared
    #: rather than inferred from the class name, so a new backend has to answer
    #: the question rather than inherit an assumption.
    confined: bool

    def argv(self, command: str) -> list[str]: ...

    def for_workspace(self, workspace: Path | str) -> "Sandbox":
        """Return a sandbox bound to `workspace`.

        A sandbox that bakes in a workspace path goes silently wrong the moment
        the agent moves -- `Agent.enter_workspace` switches into a per-task
        worktree, and a stale writable root denies every write in the new one.
        Rebinding is therefore part of the protocol, not the caller's job.
        """
        ...

    @property
    def describe(self) -> str: ...


class NullSandbox:
    """Run on the host with no confinement. Trusted callers only."""

    confined = False

    def argv(self, command: str) -> list[str]:
        return [_SHELL, "-c", command]

    def for_workspace(self, workspace: Path | str) -> "NullSandbox":
        return self  # nothing is bound, so nothing needs rebinding

    @property
    def describe(self) -> str:
        return "none (host execution)"


class SandboxUnavailable(RuntimeError):
    """Confinement was required and this platform cannot provide it."""


class UnavailableSandbox(NullSandbox):
    """Inert, but says *why* -- which is a different fact from "not configured".

    An operator who wrote `sandbox=default_sandbox(ws)` and one who configured
    nothing were previously indistinguishable: both showed `NullSandbox` in the
    posture and drew the same audit finding, though their remedies have nothing
    in common. One needs to pass a sandbox; the other needs a different host or
    a container, and no amount of configuration will help.
    """

    def __init__(self, reason: str) -> None:
        self.reason = reason

    def for_workspace(self, workspace: Path | str) -> "UnavailableSandbox":
        return self

    @property
    def describe(self) -> str:
        return f"unavailable: {self.reason}"


def _resolve(path: Path | str) -> str:
    return str(Path(path).expanduser().resolve())


def _worktree_git_common_dir(workspace: str) -> str | None:
    """The shared ``.git`` a linked git worktree writes, or ``None``.

    A linked worktree's ``.git`` is a file ``gitdir: <repo>/.git/worktrees/<name>``,
    and git writes the index, refs and objects under ``<repo>/.git`` -- outside
    the worktree directory. The main worktree's ``.git`` is a directory inside the
    workspace and needs nothing added.
    """

    marker = Path(workspace) / ".git"
    try:
        if not marker.is_file():
            return None
        text = marker.read_text()
    except OSError:
        return None
    for line in text.splitlines():
        if line.startswith("gitdir:"):
            gitdir = Path(line.split(":", 1)[1].strip())
            if not gitdir.is_absolute():
                gitdir = Path(workspace) / gitdir
            common = gitdir.resolve().parent.parent
            if common.name == ".git":
                return str(common)
    return None


class SeatbeltSandbox:
    """macOS Seatbelt confinement via ``sandbox-exec``.

    Writes are confined to ``writable_roots``; reads are allowed everywhere
    except ``unreadable_roots``; network egress is denied unless
    ``allow_network`` is set.
    """


    confined = True
    def __init__(
        self,
        *,
        writable_roots: Iterable[Path | str],
        unreadable_roots: Iterable[Path | str] = (),
        allow_network: bool = False,
        _extra_writable: Iterable[Path | str] = (),
    ) -> None:
        self.writable_roots = [_resolve(p) for p in writable_roots]
        self.unreadable_roots = [_resolve(p) for p in unreadable_roots]
        self.allow_network = allow_network
        # Roots that are not the workspace survive a rebind; the workspace root
        # is replaced by it.
        self._extra_writable = [_resolve(p) for p in _extra_writable]
        for extra in self._extra_writable:
            if extra not in self.writable_roots:
                self.writable_roots.append(extra)
        # A git worktree's index, refs and objects live in the repository's
        # shared `.git`, outside the worktree directory, so confining writes to
        # the worktree alone breaks every `git` command that writes -- commit,
        # add, checkout fail with `Operation not permitted` on
        # `.git/worktrees/<name>/index.lock`. The harness itself provisions a
        # worktree per session for isolation (`worktree_workspace_factory`), so
        # a sandbox that makes git unusable there confines the very layout it set
        # up. Allow the shared git dir; only the repository's git *metadata* is
        # added, never its working files, so worktrees stay isolated from each
        # other. Re-detected here rather than carried, so `for_workspace` -- which
        # rebuilds the sandbox on a worktree switch -- picks up the new one.
        for root in list(self.writable_roots):
            common = _worktree_git_common_dir(root)
            if common is not None and common not in self.writable_roots:
                self.writable_roots.append(common)
        if not self.writable_roots:
            raise ValueError(
                "a sandbox with no writable root cannot run a shell; pass the "
                "session workspace"
            )

    def for_workspace(self, workspace: Path | str) -> "SeatbeltSandbox":
        """Rebind the workspace root, keeping policy and any extra roots."""

        target = _resolve(workspace)
        if self.writable_roots == [target] or (
            target in self.writable_roots and len(self.writable_roots) == 1
        ):
            return self
        return SeatbeltSandbox(
            writable_roots=[target],
            unreadable_roots=self.unreadable_roots,
            allow_network=self.allow_network,
            _extra_writable=self._extra_writable,
        )

    # -- availability ------------------------------------------------------
    @staticmethod
    def available() -> bool:
        return platform.system() == "Darwin" and os.path.exists(SANDBOX_EXEC)

    # -- policy ------------------------------------------------------------
    def _policy_and_params(self) -> tuple[str, list[str]]:
        params: list[str] = []
        lines = [_BASE_POLICY]

        # Reads: broad, minus the explicitly protected roots.
        if self.unreadable_roots:
            clauses = []
            for index, root in enumerate(self.unreadable_roots):
                key = f"NOREAD_{index}"
                params.append(f"{key}={root}")
                # Both clauses: `subpath` alone does not cover the root itself.
                clauses.append(
                    f'(require-not (literal (param "{key}"))) '
                    f'(require-not (subpath (param "{key}")))'
                )
            lines.append(
                "; reads allowed except the protected roots\n"
                f"(allow file-read* (require-all {' '.join(clauses)} ))"
            )
        else:
            lines.append("; reads allowed\n(allow file-read*)")

        # Writes: only under the declared roots.
        write_clauses = []
        for index, root in enumerate(self.writable_roots):
            key = f"WRITE_{index}"
            params.append(f"{key}={root}")
            write_clauses.append(f'(subpath (param "{key}"))')
        lines.append(
            "; writes confined to the declared roots\n"
            f"(allow file-write* {' '.join(write_clauses)})"
        )

        if self.allow_network:
            lines.append(_NETWORK_POLICY)

        return "\n".join(lines), params

    def argv(self, command: str) -> list[str]:
        policy, params = self._policy_and_params()
        argv = [SANDBOX_EXEC, "-p", policy]
        for param in params:
            argv += ["-D", param]
        argv += [_SHELL, "-c", command]
        return argv

    @property
    def describe(self) -> str:
        net = "network allowed" if self.allow_network else "network denied"
        return (
            f"seatbelt (write: {', '.join(self.writable_roots)}; "
            f"no-read: {', '.join(self.unreadable_roots) or 'none'}; {net})"
        )

    def preview(self, command: str = "true") -> str:
        """Render the exact command line, for auditing a configuration."""

        return " ".join(shlex.quote(part) for part in self.argv(command))


def unavailable_reason() -> str | None:
    """Why confinement cannot be provided here, or `None` when it can."""

    if SeatbeltSandbox.available():
        return None
    if platform.system() != "Darwin":
        return (
            f"Seatbelt is macOS-only and this host is {platform.system()}; "
            "there is no Linux or Windows backend yet, so confinement needs a "
            "container"
        )
    return f"{SANDBOX_EXEC} is missing on this macOS host"


#: Credential stores a workspace shell has no business reading. Seatbelt allows
#: reads broadly -- a shell that cannot read `/bin/sh` or the dynamic linker is
#: not a shell -- and confines only writes. But a confined shell can still
#: `cat ~/.ssh/id_rsa` and `cp` it into the writable workspace, where the agent
#: reads it back and hands it to the model: write-confinement without a read
#: deny-list protects the disk and leaks the keys. The module always documented
#: "a read deny-list for the paths that actually matter" -- and then the factory
#: supplied none, so out-of-box confinement had exactly this gap. These are
#: denied by default; an operator who genuinely needs one in the sandbox (SSH
#: git, say) passes `protect_credentials=False` or builds `SeatbeltSandbox`
#: directly. Non-existent paths are harmless -- the deny clause simply never
#: matches.
_HOME_CREDENTIAL_PATHS = (
    ".ssh", ".aws", ".gnupg", ".kube", ".config/gcloud", ".netrc",
    ".docker/config.json", ".config/gh",
)


def default_unreadable_roots() -> list[Path]:
    """Well-known credential paths under the current user's home."""

    home = Path.home()
    return [home / relative for relative in _HOME_CREDENTIAL_PATHS]


def default_sandbox(
    workspace: Path | str,
    *,
    unreadable_roots: Sequence[Path | str] = (),
    allow_network: bool = False,
    require: bool = False,
    protect_credentials: bool = True,
) -> Sandbox:
    """Best confinement available here.

    With `require=True` this raises rather than degrading -- the same discipline
    the server applies to an unauthenticated public bind: a deployment that
    depends on confinement should fail to start, not discover the gap in an
    audit. Without it the result is an `UnavailableSandbox`, which runs commands
    on the host but reports *why* it is inert instead of impersonating a
    deliberate choice to run unconfined.

    `protect_credentials` adds a default read deny-list for the host's credential
    stores (`default_unreadable_roots`), so confinement protects the SSH and
    cloud keys a read-broad sandbox would otherwise expose, not just the disk.
    """

    reason = unavailable_reason()
    if reason is None:
        roots = list(unreadable_roots)
        if protect_credentials:
            roots = [*default_unreadable_roots(), *roots]
        return SeatbeltSandbox(
            writable_roots=[workspace],
            unreadable_roots=roots,
            allow_network=allow_network,
        )
    if require:
        raise SandboxUnavailable(reason)
    return UnavailableSandbox(reason)
