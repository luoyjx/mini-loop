"""Observe real AST path selection, basename globs and string-array admission."""

import os
from pathlib import Path
import tempfile
from unittest.mock import patch

import mini_loop.ast_context as source


def ast_path_contracts() -> dict:
    rows = []
    with tempfile.TemporaryDirectory(prefix="mini-loop-ast-path-contract-") as temporary:
        scratch = Path(temporary).resolve()
        root, outside = scratch / "workspace", scratch / "outside"
        root.mkdir()
        outside.mkdir()
        files = [
            {"path": "code.py", "text": "source\n"},
            {"path": "space name.py", "text": "source\n"},
            {"path": "src/a.py", "text": "source\n"},
            {"path": "src/b.PY", "text": "source\n"},
            {"path": "src/α.py", "text": "source\n"},
            {"path": "src/.hidden.py", "text": "source\n"},
            {"path": "src/bracket[.py", "text": "source\n"},
            {"path": "src/back\\slash.py", "text": "source\n"},
            {"path": "src/readme.md", "text": "source\n"},
            {"path": "src/directory.py", "directory": True},
            {"path": "empty", "directory": True},
            {"path": "home/home.py", "text": "source\n"},
            {"path": "link.py", "symlink": "code.py"},
            {"path": "linked", "symlink": "src"},
            {"path": "escape", "symlink": "<outside>"},
            {"path": "outside.py", "symlink": "<outside>/secret.py"},
            {"path": "broken.py", "symlink": "absent.py"},
            {"path": "loop.py", "symlink": "loop.py"},
        ]
        if hasattr(os, "mkfifo"):
            files.append({"path": "src/pipe.py", "fifo": True})
        (outside / "secret.py").write_text("outside secret\n")
        for item in files:
            path = root / item["path"]
            path.parent.mkdir(parents=True, exist_ok=True)
            if item.get("directory"):
                path.mkdir(exist_ok=True)
            elif "symlink" in item:
                path.symlink_to(item["symlink"].replace("<outside>", str(outside)))
            elif item.get("fifo"):
                os.mkfifo(path)
            else:
                path.write_text(item["text"])

        def normalize(value):
            return str(value).replace(str(root), "<workspace>").replace(str(outside), "<outside>").replace(str(scratch), "<scratch>")

        recipes = [
            ("path", "relative-file", "code.py"),
            ("path", "root", "."),
            ("path", "absolute-file", "<workspace>/code.py"),
            ("path", "missing-suffix", "new/nested/code.py"),
            ("path", "file-as-parent", "code.py/missing.py"),
            ("path", "internal-symlink", "link.py"),
            ("path", "internal-directory-link", "linked/a.py"),
            ("path", "link-dotdot-order", "linked/../code.py"),
            ("path", "broken-symlink", "broken.py"),
            ("path", "outside-link", "outside.py"),
            ("path", "outside-static", "escape/secret.py"),
            ("path", "outside-dotdot", "escape/../workspace/code.py"),
            ("path", "loop", "loop.py"),
            ("path", "absolute-outside", "<outside>/secret.py"),
            ("path", "parent-outside", "../outside/secret.py"),
            ("path", "spaces-preserved", "space name.py"),
            ("path", "home-file", "~/home.py"),
            ("path", "home-root", "~"),
            ("path", "wildcard-refusal", "*.py"),
            ("path", "bracket-refusal", "src/bracket[.py"),
            ("glob", "root-files", "*.py"),
            ("glob", "nested-files", "src/*.py"),
            ("glob", "absolute-glob", "<workspace>/src/*.py"),
            ("glob", "unicode-question", "src/?.py"),
            ("glob", "bracket-class", "src/[aα].py"),
            ("glob", "negated-class", "src/[!a].py"),
            ("glob", "descending-class", "src/[z-a].py"),
            ("glob", "malformed-bracket-literal", "src/bracket[.py"),
            ("glob", "literal-backslash", "src/back\\*.py"),
            ("glob", "explicit-hidden", "src/.*.py"),
            ("glob", "class-does-not-expose-hidden", "src/[.]*.py"),
            ("glob", "case-sensitive", "src/*.PY"),
            ("glob", "no-match", "src/*.rs"),
            ("glob", "empty-directory", "empty/*.py"),
            ("glob", "internal-directory-link-refusal", "linked/*.py"),
            ("glob", "outside-directory-link-refusal", "escape/*.py"),
            ("glob", "missing-static-directory", "missing/*.py"),
            ("glob", "file-static-directory", "code.py/*.py"),
            ("glob", "clean-static-dotdot", "missing/../src/*.py"),
            ("glob", "clean-symlink-dotdot", "escape/../src/*.py"),
            ("glob", "parent-outside", "../outside/*.py"),
            ("glob", "absolute-outside", "<outside>/*.py"),
            ("glob", "recursive-refusal", "src/**/*.py"),
            ("glob", "intermediate-refusal", "s*/a.py"),
            ("glob", "dotdot-after-glob", "*/../a.py"),
            ("glob", "trailing-slash", "src/*.py/"),
            ("glob", "trailing-dot", "src/*.py/."),
            ("glob", "home-files", "~/*.py"),
        ]
        for operation in ("path", "glob"):
            recipes.extend((operation, name, raw) for name, raw in (
                ("empty", ""), ("nul", "code\x00.py"),
                ("numeric", 42), ("null", None),
                ("too-long-ascii", "x" * 2049),
                ("unicode-character-bound", "α" * 2048 if operation == "path" else "α" * 2047 + "*"),
            ))
        for operation, name, raw in recipes:
            row = {"operation": operation, "name": name, "input": raw}
            if isinstance(raw, str):
                raw = raw.replace("<workspace>", str(root)).replace("<outside>", str(outside))
            with patch.dict(os.environ, {"HOME": str(root / "home")}):
                try:
                    if operation == "path":
                        row["output"] = normalize(source.AstOutlineAdapter()._safe_path(root, raw))
                    else:
                        row["output"] = [normalize(path) for path in source.AstOutlineAdapter()._safe_glob_paths(root, raw)]
                except Exception as error:
                    row.update(error_class=type(error).__name__, error=normalize(error))
            rows.append(row)

        # Only capacity/deadline configuration changes; enumeration remains real.
        for name, raw, limit, timeout in (
            ("all-entries-before-filter", "src/*.rs", 2, 15),
            ("real-deadline", "src/*.py", 10000, 1e-9),
            ("empty-directory-deadline", "empty/*.py", 10000, 1e-9),
        ):
            row = {"operation": "glob", "name": name, "input": raw,
                   "max_entries": limit, "timeout_seconds": timeout}
            with patch.object(source, "_MAX_GLOB_MATCHES", limit):
                try:
                    adapter = source.AstOutlineAdapter(source.AstContextConfig(timeout_seconds=timeout))
                    row["output"] = [normalize(path) for path in adapter._safe_glob_paths(root, raw)]
                except Exception as error:
                    row.update(error_class=type(error).__name__, error=normalize(error))
            rows.append(row)

        workspaces = []
        for name, raw in (("root", "<workspace>"), ("directory-link", "<workspace>/linked"),
                          ("file", "<workspace>/code.py"), ("missing", "<workspace>/missing"),
                          ("home", "~")):
            row = {"name": name, "input": raw}
            expanded = raw.replace("<workspace>", str(root))
            with patch.dict(os.environ, {"HOME": str(root / "home")}):
                try:
                    row["output"] = normalize(source.AstOutlineAdapter._workspace(Path(expanded)))
                except Exception as error:
                    row.update(error_class=type(error).__name__, error=normalize(error))
            workspaces.append(row)

    arrays = []
    for name, value in (("single", ["one"]), ("duplicates", ["one", "one"]),
                        ("maximum-count", ["one"] * 64), ("too-many", ["one"] * 65),
                        ("empty", []), ("scalar", "one"), ("numeric", [42]),
                        ("null", [None]), ("nul", ["\x00"]), ("empty-item", [""]),
                        ("max-unicode", ["α" * 2048]), ("too-long", ["α" * 2049])):
        row = {"name": name, "input": value, "label": "symbols"}
        try:
            row["output"] = source.AstOutlineAdapter._validate_strings(value, "symbols")
        except Exception as error:
            row.update(error_class=type(error).__name__, error=str(error))
        arrays.append(row)
    return {"files": files, "selections": rows, "workspaces": workspaces, "arrays": arrays,
            "limits": {"items": source._MAX_INPUT_ITEMS, "characters": source._MAX_INPUT_CHARS,
                       "glob_entries": source._MAX_GLOB_MATCHES}}
