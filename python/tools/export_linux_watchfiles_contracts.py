"""Capture the pinned Linux native WatchFiles profile separately from macOS exports."""
from __future__ import annotations

import argparse
import json
import platform
import tempfile
from pathlib import Path
from threading import Event

import watchfiles.main as watch_main

from watchfiles_contracts import _native_case, watchfiles_contracts


def capture() -> dict:
    if platform.system() != "Linux":
        raise RuntimeError("Linux WatchFiles capture requires an actual Linux kernel")
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        captured = watchfiles_contracts(root / "cases")
        if (captured["uvicorn_version"], captured["watchfiles_version"]) != ("0.49.0", "1.2.0"):
            raise RuntimeError("Linux capture requires Uvicorn 0.49.0 and WatchFiles 1.2.0")
        result = {key: captured[key] for key in (
            "platform", "uvicorn_version", "watchfiles_version", "default_backend",
            "source_sha256", "watch_source_sha256", "native_events",
        )}
        result["native_events"].append(_native_case(root / "alias", dict(
            name="directory-symlink-source", operation="add", path="alias/a.py",
            directory_alias="alias", directory_target="target", initial=False, mtime=100,
        )))
        if any(row["native_arguments"]["backend"] != "INotifyWatcher" for row in result["native_events"]):
            raise RuntimeError("Linux capture did not select native inotify")
        result["native_events"].append(_native_case(root / "reverse-alias", dict(
            name="directory-symlink-reverse", operation="add", path="zalias/a.py",
            directory_alias="zalias", directory_target="atarget", initial=False, mtime=100,
        )))
        cycle = root / "cycle-root"
        cycle.mkdir()
        (cycle / "cycle").symlink_to(".", target_is_directory=True)
        (cycle / "file-loop.py").symlink_to("file-loop.py")
        boundary = dict(entered=False, closed=False, leaf_loop=True)
        try:
            with watch_main.RustNotify([str(cycle)], False, False, 300, True, True) as observer:
                boundary["entered"] = True
                changes = observer.watch(1600, 50, 300, Event())
                boundary["outcome"] = "set" if isinstance(changes, set) else changes
        except Exception as error:
            boundary.update(error_type=type(error).__name__, error=str(error).replace(str(cycle), "<root>"))
        boundary["closed"] = True
        result["native_cycle"] = boundary
        source = root / "noop.py"
        source.write_text("before\n")
        with watch_main.RustNotify([str(root)], False, False, 300, True, True) as observer:
            with source.open("a"):
                pass
            changes = observer.watch(1600, 50, 300, Event())
        result["native_noop_open"] = dict(outcome="set" if isinstance(changes, set) else changes, closed=True)
        watched = root / "move-root"
        watched.mkdir()
        (watched / "a.py").write_text("before")
        with watch_main.RustNotify([str(watched)], False, False, 300, True, True) as observer:
            moved = root / "moved-root"
            watched.rename(moved)
            first = observer.watch(1600, 50, 500, Event())
            (moved / "a.py").write_text("after")
            second = observer.watch(1600, 50, 500, Event())
        def names(changes):
            return sorted("<root>" if path == str(watched) else str(Path(path).relative_to(watched))
                          for _, path in changes) if isinstance(changes, set) else changes
        result["native_root_move"] = dict(root_move=names(first), subsequent_write=names(second), closed=True)
        return result


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path)
    options = parser.parse_args()
    data = json.dumps(capture(), indent=2) + "\n"
    if options.output:
        options.output.write_text(data)
    else:
        print(data, end="")


if __name__ == "__main__":
    main()
