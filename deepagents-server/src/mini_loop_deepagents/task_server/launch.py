"""Dedicated container process entry point: ``launch api|worker [CLI args]``.

Only fixed role-specific secret files are expanded. The real CLI runs in this
same process and retains signal ownership; no child process or shell is used.
"""

from __future__ import annotations

from collections.abc import MutableMapping, Sequence
import os
import socket
import sys

from .connection import TemporalConfigurationError, _read_file

_ROLE_FILES = {
    "api": (
        ("MINILOOP_DEEPAGENTS_TOKENS", 64 * 1024, False),
        ("MINILOOP_TASK_SERVER_CURSOR_SECRET", 4 * 1024, True),
    ),
    "worker": (("OPENAI_API_KEY", 16 * 1024, True),),
}


class LaunchConfigurationError(ValueError):
    """A fixed error code that contains no secret, filename, or raw exception."""


def load_role_secrets(role: str, environ: MutableMapping[str, str] | None = None) -> None:
    """Expand only this role's allowlisted files, atomically after validation.

    Secret-mount symlinks are supported by the common bounded regular-file
    reader. Scalar credentials accept one final LF/CRLF, never multiline values.
    With no file setting the existing inline environment behavior is retained.
    """
    if role not in _ROLE_FILES:
        raise LaunchConfigurationError("task_launch_role_invalid") from None
    env = os.environ if environ is None else environ
    loaded = {}
    for variable, limit, scalar in _ROLE_FILES[role]:
        file_variable = variable + "_FILE"
        if file_variable not in env:
            continue
        if variable in env:
            raise LaunchConfigurationError("task_secret_ambiguous") from None
        try:
            raw = _read_file(env[file_variable], limit=limit, code="task_secret_file_invalid")
            value = raw.decode("utf-8")
        except (TemporalConfigurationError, UnicodeError):
            raise LaunchConfigurationError("task_secret_file_invalid") from None
        if scalar:
            value = value[:-2] if value.endswith("\r\n") else value[:-1] if value.endswith("\n") else value
            if not value or any(not 33 <= ord(c) <= 126 for c in value):
                raise LaunchConfigurationError("task_secret_value_invalid") from None
        elif "\0" in value:
            raise LaunchConfigurationError("task_secret_value_invalid") from None
        loaded[variable] = value
    env.update(loaded)


def main(argv: Sequence[str] | None = None) -> None:
    arguments = list(sys.argv[1:] if argv is None else argv)
    if not arguments or arguments[0] not in _ROLE_FILES:
        raise SystemExit("task_launch_role_invalid") from None
    role, forwarded = arguments[0], arguments[1:]
    try:
        load_role_secrets(role)
    except LaunchConfigurationError as error:
        raise SystemExit(str(error)) from None
    if role == "api":
        from .api import main as role_main
    else:
        from .worker import main as role_main
        # The real argparse parser preserves explicit --identity (including its
        # accepted abbreviations): later forwarded options override this default.
        forwarded = ["--identity", f"{socket.gethostname()}:{os.getpid()}", *forwarded]
    original_argv = sys.argv
    try:
        sys.argv = ["task-" + role, *forwarded]
        role_main()
    finally:
        sys.argv = original_argv


if __name__ == "__main__":
    main()
