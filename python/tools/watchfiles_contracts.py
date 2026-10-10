"""Capture installed default Uvicorn filtering and actual native file events."""
from __future__ import annotations

import hashlib
import inspect
import os
import platform
import re
from pathlib import Path
from queue import Empty, Queue
from threading import Event, Thread
from unittest.mock import patch

import uvicorn
import watchfiles
import watchfiles.main as watch_main
import uvicorn.supervisors.watchfilesreload as reload_module
from uvicorn import Config
from uvicorn.supervisors import ChangeReload
from uvicorn.supervisors.watchfilesreload import FileFilter, WatchFilesReload


def _config(root: Path, **options) -> Config:
    return Config("mini_loop.server:app", reload=True, reload_dirs=[str(root)],
                  reload_delay=0, log_config=None, **options)


def _filters(root: Path) -> list[dict]:
    excluded = root / "excluded"
    excluded.mkdir()
    paths = ["a.py", "nested/a.py", ".hidden.py", "nested/.hidden.py",
             "~backup.py", ".sw.part.py", ".git/a.py", ".venv/a.py",
             "__pycache__/a.py", "notes.txt", "UPPER.PY", "package.py",
             "excluded/a.py", "excluded/literal.py"]
    profiles = [
        ("default", [], []),
        ("exclude-python", [], ["*.py"]),
        ("include-text", ["*.txt"], []),
        ("allow-hidden-and-backup", [".*", "~*"], []),
        ("exclude-directory", [], [str(excluded)]),
        ("exact-include-shortcut", ["literal.py"], ["*.py", str(excluded)]),
    ]
    rows = []
    for name, includes, excludes in profiles:
        predicate = FileFilter(_config(root, reload_includes=includes,
                                       reload_excludes=excludes))
        rows.append(dict(name=name, includes=includes,
                         excludes=["<root>/excluded" if e == str(excluded) else e
                                   for e in excludes],
                         paths=[dict(path=path, allowed=predicate(root / path))
                                for path in paths]))
    return rows


def _native_case(root: Path, recipe: dict) -> dict:
    root.mkdir()
    source = root / recipe["path"]
    source.parent.mkdir(parents=True, exist_ok=True)
    if recipe.get("symlink_target"):
        (root / recipe["symlink_target"]).write_text("before\n")
        source.symlink_to(recipe["symlink_target"])
    if recipe["initial"]:
        source.write_text("before\n")
        os.utime(source, (100, 100))
    output: Queue = Queue()
    armed, release = Event(), Event()
    errors = []
    arguments = []
    closed = Event()
    original_notify, original_watch = watch_main.RustNotify, reload_module.watch

    class ReadyNotify:
        def __init__(self, *args):
            arguments.append(dict(force_polling=args[2], poll_delay_ms=args[3],
                                  recursive=args[4], ignore_permission_denied=args[5]))
            self.value = original_notify(*args)
            representation = repr(self.value)
            if not representation.startswith("RustNotify(Recommended("):
                raise RuntimeError("Source selected a non-native observer")
            arguments[-1]["backend"] = next((name for name in
                ["FsEventWatcher", "INotifyWatcher", "ReadDirectoryChangesWatcher"]
                if name in representation), "Recommended")
            if arguments[-1]["backend"] == "FsEventWatcher":
                flags = re.search(r"flags: (\d+),", representation)
                latency = re.search(r"latency: ([0-9.]+),", representation)
                since = re.search(r"since_when: (\d+),", representation)
                if not (flags and latency and since):
                    raise RuntimeError("unknown native FSEvents configuration")
                arguments[-1]["fsevents"] = dict(flags=int(flags[1]),
                    latency_seconds=float(latency[1]), since_now=int(since[1]) == 2**64 - 1)

        def __enter__(self):
            return self.value.__enter__()

        def __exit__(self, *args):
            result = self.value.__exit__(*args)
            closed.set()
            return result

    def bounded_watch(*args, **kwargs):
        # Only timeout waiting is shortened. Native backend, default debounce,
        # step, recursion, filtering and Uvicorn dispatch remain unchanged.
        return original_watch(*args, **kwargs, rust_timeout=100)

    with patch.dict(os.environ, {"WATCHFILES_FORCE_POLLING": "false",
                                 "WATCHFILES_DEBUG": "", "WATCHFILES_POLL_DELAY_MS": ""}), \
            patch.object(watch_main, "RustNotify", ReadyNotify), \
            patch.object(reload_module, "watch", bounded_watch):
        observer = WatchFilesReload(_config(root), lambda sockets: None, [])

        def observe():
            try:
                # Drain pre-subscription filesystem writes until a real native
                # timeout. Keep the live subscription while the operation runs.
                while observer.should_restart() is not None:
                    pass
                armed.set()
                release.wait(5)
                while not observer.should_exit.is_set():
                    changed = observer.should_restart()
                    if changed is not None:
                        output.put(sorted(str(path.relative_to(root)) for path in changed))
                        return
            except StopIteration:
                pass
            except BaseException as error:
                errors.append(error)
                armed.set()

        thread = Thread(target=observe, name="source-watchfiles-contract")
        thread.start()
        try:
            if not armed.wait(5) or errors:
                raise RuntimeError("native Source observer did not arm") from (errors[0] if errors else None)
            operation = recipe["operation"]
            if operation in {"write", "add", "unchanged-write"}:
                source.write_text("before\n" if operation == "unchanged-write" else "after\n")
                os.utime(source, (recipe["mtime"], recipe["mtime"]))
            elif operation == "remove":
                source.unlink()
            elif operation == "rename":
                source.rename(root / recipe["destination"])
            elif operation == "touch":
                os.utime(source, (recipe["mtime"], recipe["mtime"]))
            elif operation == "mkdir":
                source.mkdir()
            elif operation == "transient":
                source.write_text("temporary\n")
                source.unlink()
            else:
                raise ValueError(operation)
            release.set()
            try:
                changed = output.get(timeout=3)
            except Empty:
                if errors:
                    raise RuntimeError("native Source observation failed") from errors[0]
                changed = None
        finally:
            observer.should_exit.set()
            release.set()
            thread.join(5)
            if thread.is_alive():
                raise RuntimeError("native Source observer did not join")
            observer.watcher.close()
        if errors:
            raise RuntimeError("native Source observation failed") from errors[0]
    if not closed.is_set():
        raise RuntimeError("native Source context was not closed")
    if len(arguments) != 1 or arguments[0]["force_polling"]:
        raise RuntimeError("contract did not use one native observer")
    return dict(**recipe, changed=changed, native_arguments=arguments[0], closed=True)


def _environment() -> dict:
    force = []
    for value in [None, "", "false", "FALSE", "disable", "disabled", "0", "1", " "]:
        with patch.dict(os.environ):
            os.environ.pop("WATCHFILES_FORCE_POLLING", None)
            if value is not None:
                os.environ["WATCHFILES_FORCE_POLLING"] = value
            force.append(dict(value=value, result=watch_main._default_force_polling(None),
                              explicit_true=watch_main._default_force_polling(True),
                              explicit_false=watch_main._default_force_polling(False)))
    delay = []
    for value in [None, "", "50", "0", "-1", "nonnumeric", "١٠", "²"]:
        with patch.dict(os.environ):
            os.environ.pop("WATCHFILES_POLL_DELAY_MS", None)
            if value is not None:
                os.environ["WATCHFILES_POLL_DELAY_MS"] = value
            row = dict(value=value)
            try:
                row["result"] = watch_main._default_poll_delay_ms(300)
            except ValueError as error:
                row["error"] = str(error)
            delay.append(row)
    return dict(force_polling=force, poll_delay=delay)


def _cancel(root: Path) -> dict:
    root.mkdir()
    entered, closed = Event(), Event()
    errors, outcomes = [], []
    original = watch_main.RustNotify

    class CancellationNotify:
        def __init__(self, *args):
            self.value = original(*args)

        def __enter__(self):
            value = self.value.__enter__()
            entered.set()
            return value

        def __exit__(self, *args):
            result = self.value.__exit__(*args)
            closed.set()
            return result

    with patch.dict(os.environ, {"WATCHFILES_FORCE_POLLING": "false", "WATCHFILES_DEBUG": ""}), \
            patch.object(watch_main, "RustNotify", CancellationNotify):
        observer = WatchFilesReload(_config(root), lambda sockets: None, [])

        def observe():
            try:
                while True:
                    observer.should_restart()
            except StopIteration:
                outcomes.append("StopIteration")
            except BaseException as error:
                errors.append(error)

        thread = Thread(target=observe, name="source-watchfiles-cancel")
        thread.start()
        try:
            if not entered.wait(5):
                raise RuntimeError("native cancellation observer did not enter")
        finally:
            observer.should_exit.set()
            thread.join(5)
            if thread.is_alive():
                raise RuntimeError("native cancellation did not join")
            observer.watcher.close()
        if errors or outcomes != ["StopIteration"] or not closed.is_set():
            raise RuntimeError("native cancellation did not close") from (errors[0] if errors else None)
    return dict(exit="StopIteration", native_context_closed=True, thread_joined=True)


def watchfiles_contracts(scratch: Path) -> dict:
    if ChangeReload is not WatchFilesReload:
        raise RuntimeError("installed Source default is not WatchFilesReload")
    scratch.mkdir(parents=True, exist_ok=True)
    scratch = scratch.resolve()
    recipes = [
        dict(name="added-source", operation="add", path="new.py", initial=False, mtime=100),
        dict(name="hidden-parent-source", operation="add", path=".git/a.py", initial=False, mtime=100),
        dict(name="symlink-target-write", operation="write", path="linked.py", symlink_target="notes.txt", initial=True, mtime=100),
        dict(name="removed-source", operation="remove", path="source.py", initial=True),
        dict(name="same-mtime-write", operation="write", path="source.py", initial=True, mtime=100),
        dict(name="lower-mtime-write", operation="write", path="source.py", initial=True, mtime=99),
        dict(name="identical-write", operation="unchanged-write", path="source.py", initial=True, mtime=100),
        dict(name="same-mtime-touch", operation="touch", path="source.py", initial=True, mtime=100),
        dict(name="rename-out", operation="rename", path="source.py", destination="notes.txt", initial=True),
        dict(name="rename-in", operation="rename", path="notes.txt", destination="target.py", initial=True),
        dict(name="rename-source", operation="rename", path="source.py", destination="target.py", initial=True),
        dict(name="ignored-hidden", operation="add", path=".hidden.py", initial=False, mtime=100),
        dict(name="ignored-backup", operation="add", path="~backup.py", initial=False, mtime=100),
        dict(name="ignored-text", operation="add", path="notes.txt", initial=False, mtime=100),
        dict(name="source-directory", operation="mkdir", path="package.py", initial=False),
        dict(name="transient-source", operation="transient", path="flash.py", initial=False),
    ]
    source = Path(inspect.getfile(WatchFilesReload))
    return dict(uvicorn_version=uvicorn.__version__, watchfiles_version=watchfiles.__version__,
                platform=platform.system(), default_backend=ChangeReload.__name__,
                source_sha256=hashlib.sha256(source.read_bytes()).hexdigest(),
                watch_source_sha256=hashlib.sha256(Path(watch_main.__file__).read_bytes()).hexdigest(),
                environment=_environment(), cancellation=_cancel(scratch / "cancel"),
                filters=_filters(scratch),
                native_events=[_native_case(scratch / recipe["name"], recipe)
                               for recipe in recipes])
