"""Capture actual installed Uvicorn StatReload without launching a server."""

import hashlib
import inspect
import os
from pathlib import Path

import uvicorn
from uvicorn import Config
from uvicorn.supervisors.statreload import StatReload


def reload_contracts(scratch: Path) -> dict:
    scratch.mkdir(parents=True, exist_ok=True)
    scratch = scratch.resolve()
    config = Config("mini_loop.server:app", reload=True,
                    reload_dirs=[str(scratch)], reload_delay=0, log_config=None)
    observer = StatReload(config, target=lambda sockets: None, sockets=[])
    operations = [
        ("write", "a.py", 100),
        ("scan", "", 0),
        ("write", "a.py", 100),
        ("scan", "", 0),
        ("write", "a.py", 101),
        ("scan", "", 0),
        ("scan", "", 0),
        ("reconstruct", "", 0),
        ("scan", "", 0),
        ("write", "a.py", 99),
        ("scan", "", 0),
        ("write", "new.py", 200),
        ("scan", "", 0),
        ("write", "notes.txt", 300),
        ("scan", "", 0),
        ("remove", "a.py", 0),
        ("scan", "", 0),
        ("write", "a.py", 100),
        ("scan", "", 0),
        ("write", "a.py", 102),
        ("scan", "", 0),
        ("reconstruct", "", 0),
        ("symlink", "link.py", 0),
        ("scan", "", 0),
        ("write", "notes.txt", 301),
        ("scan", "", 0),
        ("reconstruct", "", 0),
        ("mkdir", "package.py", 500),
        ("scan", "", 0),
        ("touch", "package.py", 501),
        ("scan", "", 0),
    ]
    rows = []
    for operation, name, mtime in operations:
        row = dict(operation=operation, path=name, mtime=mtime)
        if operation == "write":
            path = scratch / name
            path.write_text(f"# modified at {mtime}\n")
            os.utime(path, (mtime, mtime))
        elif operation == "remove":
            (scratch / name).unlink()
        elif operation == "symlink":
            (scratch / name).symlink_to(scratch / "notes.txt")
        elif operation in {"mkdir", "touch"}:
            path = scratch / name
            if operation == "mkdir":
                path.mkdir()
            os.utime(path, (mtime, mtime))
        elif operation == "reconstruct":
            observer = StatReload(config, target=lambda sockets: None, sockets=[])
        else:
            changes = observer.should_restart()
            row["changed"] = [str(path.relative_to(scratch)) for path in changes or []]
        rows.append(row)
    source = Path(inspect.getfile(StatReload))
    return dict(uvicorn_version=uvicorn.__version__,
                source_sha256=hashlib.sha256(source.read_bytes()).hexdigest(),
                steps=rows)
