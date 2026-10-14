"""Actual Toolset/Popen filesystem-codec and sandbox-before-encoding cases."""
from __future__ import annotations

import hashlib
import json
import shlex
import sys
from pathlib import Path


def bash_surrogate_contracts(scratch: Path) -> dict:
    from mini_loop.sandbox import NullSandbox
    from mini_loop.tools import Toolset

    if (sys.getfilesystemencoding(), sys.getfilesystemencodeerrors()) != ("utf-8", "surrogateescape"):
        raise RuntimeError("this finite subprocess profile requires UTF-8/surrogateescape filesystem encoding")

    def script(value):
        return "printf effect > effect.marker; printf '%s' " + shlex.quote(value) + " > argument.bytes; printf done"

    recipes = [
        ("high-d800", "null", "\ud800"),
        ("low-dfff", "null", "\udfff"),
        ("low-dc7f-before-escape", "null", "\udc7f"),
        ("escape-dc80", "null", "\udc80"),
        ("escape-dcff", "null", "\udcff"),
        ("mixed-escape-unicode", "null", "\udc80é\udcff"),
        ("ordinary-unicode", "null", "用户-é-Ω"),
        ("escape-before-high", "null", "\udc80-\ud800"),
        ("sandbox-repairs-high", "repair", "\ud800"),
        ("sandbox-extra-high", "extra", "\ud800"),
        ("sandbox-extra-escape", "extra", "\udc80"),
        ("sandbox-program-high", "program", "\ud800"),
    ]
    rows = []
    for name, mode, value in recipes:
        workspace = Path(scratch) / "bash-surrogates" / name
        workspace.mkdir(parents=True, exist_ok=True)
        marker, argument = workspace / "effect.marker", workspace / "argument.bytes"
        marker.unlink(missing_ok=True)
        argument.unlink(missing_ok=True)
        command = script(value) if mode in {"null", "repair"} else "original command must not run"

        class ObservedSandbox:
            calls = 0
            selected = None
            received = None

            def for_workspace(self, path):
                return self

            def argv(self, original):
                self.calls += 1
                self.received = original
                if mode == "null":
                    selected = NullSandbox().argv(original)
                elif mode == "repair":
                    selected = NullSandbox().argv(script("repaired"))
                elif mode == "extra":
                    selected = ["/bin/sh", "-c", "printf effect > effect.marker; printf '%s' \"$1\" > argument.bytes; printf done", "fixture-shell", value]
                else:
                    selected = ["/bin/" + value + "sh", "-c", script("not-run")]
                self.selected = selected
                return selected

        sandbox = ObservedSandbox()
        toolset = Toolset(workspace, bash_timeout=3, sandbox=sandbox)
        actual, exception = None, None
        try:
            result = toolset.run_bash_result(command)
            actual = dict(stdout=result.stdout, stderr=result.stderr, exit_code=result.exit_code,
                          timed_out=result.timed_out, overflowed=result.overflowed,
                          error=result.error, projection=result.projection,
                          capture_limit=result.capture_limit, rendered=result.render())
        except Exception as error:
            exception = dict(exception_class=type(error).__name__, message=str(error))
            if isinstance(error, UnicodeEncodeError):
                exception.update(encoding=error.encoding, reason=error.reason,
                                 start=error.start, end=error.end,
                                 argument_index=next(index for index, arg in enumerate(sandbox.selected)
                                                     if arg == error.object),
                                 object_json_ascii=json.dumps(error.object, ensure_ascii=True))
        rows.append(dict(name=name, sandbox_mode=mode,
                         command_json_ascii=json.dumps(command, ensure_ascii=True),
                         selected_argv_json_ascii=json.dumps(sandbox.selected, ensure_ascii=True),
                         sandbox_calls=sandbox.calls, sandbox_received_original=sandbox.received == command,
                         result=actual, exception=exception,
                         marker_hex=marker.read_bytes().hex() if marker.exists() else None,
                         argument_hex=argument.read_bytes().hex() if argument.exists() else None))

    root = Path(__file__).resolve().parents[1]
    sources = ("mini_loop/tools.py", "mini_loop/sandbox.py", "mini_loop/secrets.py")
    result = dict(filesystem_encoding=sys.getfilesystemencoding(),
                  filesystem_errors=sys.getfilesystemencodeerrors(), cases=rows,
                  source_sha256={name: hashlib.sha256((root / name).read_bytes()).hexdigest() for name in sources},
                  producer_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                  scope=[
                      "Actual Toolset.run_bash_result with private cwd/relative marker and byte-output files; actual selected sandbox argv and Popen filesystem encoding.",
                      "UTF-8/surrogateescape filesystem profile only. D800/DFFF/DC7F and a bad program/extra argv entry fail before the ASCII marker command can run; DC80/DCFF preserve raw80/ff bytes.",
                      "Selected sandbox argv may repair a nonencodable input command; encoding happens after that selection and applies to each resulting argv entry.",
                      "Exception metadata and messages are retained, not converted to a successful structured result. Durations/PIDs are not comparison inputs.",
                      "Command and argv are embedded as ASCII JSON strings; effects are explicit hex. No test marker is outside its owned case workspace.",
                      "This is the shell effect boundary, not proof of full Agent ledger/next-request handling or arbitrary native byte strings, environment codecs and other filesystem encodings.",
                  ])
    json.dumps(result, ensure_ascii=True, allow_nan=False)
    return result
