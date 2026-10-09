"""Freeze real dotenv discovery and file reading without touching host .env files."""

import __main__
import importlib.metadata
import io
import logging
import os
from pathlib import Path
import sys
from unittest.mock import patch

from dotenv import dotenv_values, find_dotenv, load_dotenv
from dotenv.main import _walk_to_root


def _fault(exc: Exception) -> dict:
    return dict(type=type(exc).__name__, message=str(exc))


def dotenv_file_contracts(scratch: Path) -> dict:
    scratch = scratch.resolve()
    scratch.mkdir(parents=True, exist_ok=True)
    discovery = []
    recipes = [
        ("caller-parent", False, False, False, False, "parent"),
        ("cwd-explicit", True, False, False, False, "parent"),
        ("interactive", False, True, False, False, "parent"),
        ("debugger", False, False, True, False, "parent"),
        ("frozen", False, False, False, True, "parent"),
        ("nearest", False, False, False, False, "file"),
        ("directory-skipped", False, False, False, False, "directory"),
        ("symlink-file", False, False, False, False, "symlink"),
        ("dangling-skipped", False, False, False, False, "dangling"),
        ("fifo", False, False, False, False, "fifo"),
    ]
    for name, usecwd, interactive, debugger, frozen, kind in recipes:
        root = scratch / name
        caller = root / "source/pkg/config.py"
        caller.parent.mkdir(parents=True, exist_ok=True)
        caller.touch()
        cwd = root / "working/deep"
        cwd.mkdir(parents=True, exist_ok=True)
        filename = ".env"
        (root / filename).write_text("ROOT=yes\n")
        (root / "working" / filename).write_text("WORK=yes\n")
        (root / "source" / filename).write_text("SOURCE=yes\n")
        local = caller.parent / filename
        if kind == "file":
            local.write_text("LOCAL=yes\n")
        elif kind == "directory":
            local.mkdir(exist_ok=True)
        elif kind == "symlink":
            local.symlink_to("../.env")
        elif kind == "dangling":
            local.symlink_to("absent")
        elif kind == "fifo":
            os.mkfifo(local)
        # A real existing synthetic Source filename selects the caller directory.
        # A missing wrapper frame is skipped, as dotenv does for notebook frames.
        namespace = {"find_dotenv": find_dotenv, "load_dotenv": load_dotenv}
        exec(compile("def wrapper():\n return find_dotenv(usecwd=" + str(usecwd) + ")\n",
                     "<missing-contract-frame>", "exec"), namespace)
        exec(compile("def invoke():\n return wrapper()\ndef load():\n return load_dotenv(override=True)\n",
                     str(caller), "exec"), namespace)
        loaded, applied = None, None
        previous = Path.cwd()
        try:
            os.chdir(cwd)
            with patch.object(sys, "gettrace", return_value=object() if debugger else None), \
                    patch.object(sys, "frozen", frozen, create=True), \
                    patch.object(__main__, "__file__", str(caller), create=True):
                with patch.object(sys, "ps1", ">>>", create=True) if interactive else _without_repl():
                    selected = namespace["invoke"]()
                    if kind != "fifo":
                        with patch.dict(os.environ, {}, clear=True):
                            loaded = namespace["load"]()
                            applied = dict(os.environ)
        finally:
            os.chdir(previous)
        discovery.append(dict(name=name, cwd="working/deep", caller="source/pkg/config.py",
                              filename=filename, usecwd=usecwd, interactive=interactive,
                              debugger=debugger, frozen=frozen, kind=kind,
                              selected=Path(selected).relative_to(root).as_posix(),
                              loaded=loaded, applied=applied))

    # A unique filename prevents a host ancestor from satisfying the missing case.
    root = scratch / "missing"
    root.mkdir(exist_ok=True)
    missing_name = ".absent-" + scratch.name
    missing = []
    previous = Path.cwd()
    try:
        os.chdir(root)
        for required in (False, True):
            selected, fault = None, None
            try:
                selected = find_dotenv(filename=missing_name, usecwd=True,
                                       raise_error_if_not_found=required)
            except OSError as exc:
                fault = _fault(exc)
            missing.append(dict(required=required, selected=selected, fault=fault))
    finally:
        os.chdir(previous)
    walk = []
    for name, start in [("directory", scratch / "caller-parent/source/pkg"),
                        ("file", scratch / "caller-parent/source/pkg/config.py"),
                        ("missing", scratch / "no-such-path")]:
        paths, fault = [], None
        try:
            for path in _walk_to_root(str(start)):
                try:
                    paths.append(Path(path).relative_to(scratch).as_posix())
                except ValueError:
                    break  # Actual walk continues to host ancestors; only fixture-local prefix matters.
        except OSError as exc:
            fault = _fault(exc)
        walk.append(dict(name=name, paths=paths, fault=fault))

    reads = []
    recipes = [
        ("utf8", "file", b"A=hello\nB=\xe4\xbd\xa0\xe5\xa5\xbd\n", None, "utf-8", {}),
        ("newlines", "file", b"A='one\r\ntwo\rthree'\r\nB=end\r", None, "utf-8", {}),
        ("bom", "file", b"\xef\xbb\xbfA=one\n", None, "utf-8", {}),
        ("invalid-utf8", "file", b"A=prior\nB=\xff\n", None, "utf-8", {}),
        ("truncated-utf8", "file", b"A=\xe2\x82", None, "utf-8", {}),
        ("empty", "file", b"", None, "utf-8", {}),
        ("missing", "missing", b"", None, "utf-8", {}),
        ("directory", "directory", b"", None, "utf-8", {}),
        ("symlink", "symlink", b"A=linked\n", None, "utf-8", {}),
        ("dangling", "dangling", b"", None, "utf-8", {}),
        ("file-before-stream", "file", b"A=file\n", "A=stream\n", "utf-8", {}),
        ("missing-stream", "missing", b"", "A=stream\r\n", "utf-8", {}),
        ("directory-stream", "directory", b"", "A=stream\n", "utf-8", {}),
        ("disabled-invalid", "file", b"A=\xff\n", None, "utf-8", {"PYTHON_DOTENV_DISABLED": "yes"}),
        ("latin1", "file", b"A=caf\xe9\n", None, "latin-1", {}),
        ("utf8-sig", "file", b"\xef\xbb\xbfA=one\n", None, "utf-8-sig", {}),
        ("interpolation", "file", b"A=file\nB=${A}\n", None, "utf-8", {"A": "external"}),
        ("partial-fault", "file", b"A=prior\n'bad=key'=value\n", None, "utf-8", {}),
    ]
    for name, kind, content, stream, encoding, environment in recipes:
        root = scratch / ("read-" + name)
        root.mkdir(exist_ok=True)
        path = root / ".env"
        if kind == "file":
            path.write_bytes(content)
        elif kind == "directory":
            path.mkdir(exist_ok=True)
        elif kind == "symlink":
            (root / "target").write_bytes(content)
            path.symlink_to("target")
        elif kind == "dangling":
            path.symlink_to("absent")
        for override in (True, False):
            values, value_fault, loaded, fault = None, None, None, None
            with patch.dict(os.environ, environment, clear=True), \
                    patch.object(logging.getLogger("dotenv.main"), "warning"):
                try:
                    result = dotenv_values(path, stream=io.StringIO(stream) if stream is not None else None,
                                           encoding=encoding)
                    values = [dict(key=k, value=v) for k, v in result.items()]
                except (OSError, ValueError) as exc:
                    value_fault = _fault(exc)
                try:
                    loaded = load_dotenv(path, stream=io.StringIO(stream) if stream is not None else None,
                                         override=override, encoding=encoding)
                except (OSError, ValueError) as exc:
                    fault = _fault(exc)
                applied = dict(os.environ)
            reads.append(dict(name=name, kind=kind, content=content.hex(), stream=stream,
                              encoding=encoding, environment=environment, override=override,
                              values=values, valueFault=value_fault, loaded=loaded,
                              fault=fault, applied=applied))
    return dict(version=importlib.metadata.version("python-dotenv"),
                discovery=discovery, missing=missing, walk=walk, reads=reads)


class _without_repl:
    """Restore interactive markers exactly, even inside an interactive exporter."""

    def __enter__(self):
        self.saved = {name: getattr(sys, name) for name in ("ps1", "ps2") if hasattr(sys, name)}
        for name in self.saved:
            delattr(sys, name)

    def __exit__(self, *exc):
        for name, value in self.saved.items():
            setattr(sys, name, value)
