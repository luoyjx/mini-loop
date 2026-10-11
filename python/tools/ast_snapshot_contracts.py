"""Capture the real anchored AST snapshotter, including filesystem effects."""

from contextlib import ExitStack
import hashlib
import os
from pathlib import Path
import stat
import tempfile
from unittest.mock import patch

import mini_loop.ast_context as source


def ast_snapshot_contracts() -> dict:
    def file(path, text="source\n"):
        return {"path": path, "text": text}

    def directory(path):
        return {"path": path, "directory": True}

    def link(path, target):
        return {"path": path, "symlink": target}

    recipes = [
        {"name": "explicit-file", "files": [file("code.py")], "targets": ["code.py"]},
        {"name": "deduplicated-order", "files": [file("a.py"), file("b.py")], "targets": ["b.py", "a.py", "b.py"]},
        {"name": "directory-language-inventory", "files": [
            *[file("code" + suffix) for suffix in sorted(source._AST_SOURCE_SUFFIXES)],
            *[file(name) for name in sorted(source._AST_SOURCE_BASENAMES)],
            file("upper.PY"), file(".hidden.py"), file(".py"), file("asset.bin"),
            file("Gemfile.lock"), directory("empty")], "targets": ["."]},
        {"name": "explicit-unsupported", "files": [file("asset.bin")], "targets": ["asset.bin"]},
        {"name": "explicit-hidden-basename", "files": [file(".py")], "targets": [".py"]},
        {"name": "default-ignores", "files": [file("keep.py"), file("code.min.js"),
            file("node_modules/vendor.py"), file(".git/secret.py"), file(".svn/secret.py"),
            file("__pycache__/secret.py"), file(".venv/secret.py")], "targets": ["."]},
        {"name": "ignore-frames", "files": [directory(".git"),
            file(".gitignore", "generated/\n*.swift\n"),
            file(".ignore", "!visible.swift\n!node_modules/\nnode_modules/*/\n!node_modules/our-fork/\n"),
            file("visible.swift"), file("hidden.swift"), file("generated/hidden.py"),
            file("src/.gitignore", "ignored.py\n*.vue\n"), file("src/.ignore", "!App.vue\n"),
            file("src/ignored.py"), file("src/App.vue"), file("src/Other.vue"),
            file("node_modules/our-fork/kept.ts"), file("node_modules/other/hidden.ts")], "targets": ["."]},
        {"name": "explicit-file-ignore-bypass", "files": [file(".gitignore", "code.py\n"), file("code.py")], "targets": [".", "code.py"]},
        {"name": "explicit-nested-overrides", "files": [directory(".git"),
            file("src/.gitignore", "generated.py\nignored-dir/\n"), file("src/generated.py"),
            file("src/ignored-dir/kept.ts")], "targets": ["src/ignored-dir", "src/generated.py", "src"]},
        {"name": "subtree-without-git-marker", "files": [file(".gitignore", "code.py\n"), file("src/code.py")], "targets": ["src"]},
        {"name": "subtree-with-git-marker", "files": [directory(".git"), file(".gitignore", "code.py\n"), file("src/code.py")], "targets": ["src"]},
        {"name": "nearest-git-marker", "files": [directory(".git"), file(".gitignore", "code.py\n"), directory("src/.git"), file("src/code.py")], "targets": ["src"]},
        {"name": "git-marker-file", "files": [file(".git", "gitdir: elsewhere\n"), file(".gitignore", "code.py\n"), file("src/code.py")], "targets": ["src"]},
        {"name": "tree-symlinks", "files": [file("keep.py"), file("nested/code.py"),
            link("local.py", "keep.py"), link("outside.py", "<outside>/secret.py"),
            link("linked", "nested"), link("broken.py", "absent")], "targets": ["."]},
        {"name": "explicit-symlink", "files": [file("keep.py"), link("linked.py", "keep.py")], "targets": ["linked.py"]},
        {"name": "symlink-parent", "files": [file("nested/code.py"), link("linked", "nested")], "targets": ["linked/code.py"]},
        {"name": "ignore-control-symlink", "files": [file("code.py"), file("controls", "code.py\n"), link(".gitignore", "controls")], "targets": ["."]},
        {"name": "ignore-invalid-utf8", "files": [{"path": ".gitignore", "hex": "6967ff6e6f7265642e70790a"}, file("ignored.py"), file("keep.py")], "targets": ["."]},
        {"name": "ignore-unicode-splitlines", "files": [file(".gitignore", "ignored.py\u0085other.py\n"), file("ignored.py"), file("other.py"), file("keep.py")], "targets": ["."]},
        {"name": "ignored-bytes-before-cap", "files": [file(".gitignore", "generated/\n"), file("keep.py"), {"path": "generated/large.py", "size": source._MAX_SNAPSHOT_BYTES + 1}], "targets": ["."]},
        {"name": "missing-target", "files": [], "targets": ["missing.py"]},
        {"name": "no-targets", "files": [], "targets": []},
        {"name": "outside-target", "files": [], "targets": ["<outside>/secret.py"]},
        {"name": "freeze-and-cleanup", "files": [file("code.py", "before\n")], "targets": ["code.py"], "mutate": {"path": "code.py", "text": "after\n"}},
        {"name": "default-byte-cap", "files": [{"path": "large.py", "size": source._MAX_SNAPSHOT_BYTES + 1}], "targets": ["large.py"]},
        {"name": "file-cap", "files": [file("a.py"), file("b.py")], "targets": ["."], "limits": {"_MAX_SNAPSHOT_FILES": 1}},
        {"name": "control-files-count", "files": [file(".gitignore", "# comment\n"), file("code.py")], "targets": ["."], "limits": {"_MAX_SNAPSHOT_FILES": 1}},
        {"name": "entry-cap-before-filter", "files": [file("a.bin"), file("b.bin")], "targets": ["."], "limits": {"_MAX_SNAPSHOT_ENTRIES": 1}},
        {"name": "depth-cap", "files": [file("a/b/code.py")], "targets": ["."], "limits": {"_MAX_SNAPSHOT_DEPTH": 1}},
        {"name": "ignore-byte-cap", "files": [file(".gitignore", "code.py\n")], "targets": ["."], "limits": {"_MAX_IGNORE_BYTES": 3}},
        {"name": "ignore-pattern-cap", "files": [file(".gitignore", "a\nb\n")], "targets": ["."], "limits": {"_MAX_IGNORE_PATTERNS": 1}},
        {"name": "ignore-match-cap", "files": [file("code.py")], "targets": ["."], "limits": {"_MAX_IGNORE_MATCH_OPS": 1}},
        {"name": "ignore-star-guard", "files": [file(".gitignore", "*a*a*a*ab\n"), file("code.py")], "targets": ["."]},
        {"name": "ignore-pattern-length", "files": [file(".gitignore", "a" * 4097)], "targets": ["."]},
    ]
    if hasattr(os, "mkfifo"):
        recipes.extend([
            {"name": "tree-fifo", "files": [{"path": "pipe.py", "fifo": True}, file("keep.py")], "targets": ["."]},
            {"name": "explicit-fifo", "files": [{"path": "pipe.py", "fifo": True}], "targets": ["pipe.py"]},
        ])

    rows = []
    for recipe in recipes:
        row = {"recipe": recipe}
        with tempfile.TemporaryDirectory(prefix="mini-loop-ast-snapshot-contract-") as temporary:
            scratch = Path(temporary).resolve()
            workspace = scratch / "workspace"
            workspace.mkdir()
            outside = scratch / "outside"
            outside.mkdir()
            (outside / "secret.py").write_text("outside secret\n")
            for item in recipe["files"]:
                path = workspace / item["path"]
                path.parent.mkdir(parents=True, exist_ok=True)
                if item.get("directory"):
                    path.mkdir(exist_ok=True)
                elif "symlink" in item:
                    path.symlink_to(item["symlink"].replace("<outside>", str(outside)))
                elif item.get("fifo"):
                    os.mkfifo(path)
                elif "size" in item:
                    with path.open("wb") as stream:
                        stream.truncate(item["size"])
                elif "hex" in item:
                    path.write_bytes(bytes.fromhex(item["hex"]))
                else:
                    path.write_text(item["text"], encoding="utf-8")
            targets = [Path(value.replace("<outside>", str(outside))) if value.startswith("<outside>")
                       else workspace / value for value in recipe["targets"]]
            adapter = source.AstOutlineAdapter()
            snapshot_root = None
            try:
                with ExitStack() as stack:
                    # Only configured capacity constants are changed in these
                    # tiny guard recipes. Traversal/read/matching remain real.
                    for name, value in recipe.get("limits", {}).items():
                        stack.enter_context(patch.object(source, name, value))
                    paths, rewrites = stack.enter_context(adapter._snapshot_workspace_paths(workspace, targets))
                    snapshot_root = Path(rewrites[0][0])
                    row["paths"] = [str(path.relative_to(snapshot_root)) for path in paths]
                    row["rewrite"] = ["<snapshot>", "<workspace>"]
                    row["outside_workspace"] = not snapshot_root.is_relative_to(workspace)
                    manifest = []
                    for path in sorted(snapshot_root.rglob("*")):
                        entry = {"path": str(path.relative_to(snapshot_root))}
                        if path.is_dir():
                            entry["kind"] = "directory"
                        else:
                            data = path.read_bytes()
                            entry.update(kind="file", bytes=len(data), sha256=hashlib.sha256(data).hexdigest(), mode=oct(stat.S_IMODE(path.stat().st_mode)))
                        manifest.append(entry)
                    row["manifest"] = manifest
                    if "mutate" in recipe:
                        item = recipe["mutate"]
                        (workspace / item["path"]).write_text(item["text"])
                        row["frozen_sha256"] = hashlib.sha256(paths[0].read_bytes()).hexdigest()
                row["cleaned"] = not snapshot_root.exists()
            except Exception as error:
                row["error_class"] = type(error).__name__
                row["error"] = str(error).replace(str(workspace), "<workspace>").replace(str(scratch), "<scratch>")
        rows.append(row)
    names = ("_MAX_SNAPSHOT_BYTES", "_MAX_SNAPSHOT_ENTRIES", "_MAX_SNAPSHOT_FILES", "_MAX_SNAPSHOT_DEPTH",
             "_MAX_IGNORE_BYTES", "_MAX_IGNORE_PATTERNS", "_MAX_IGNORE_PATTERN_CHARS", "_MAX_IGNORE_MATCH_OPS")
    return {"limits": {name: getattr(source, name) for name in names},
            "suffixes": sorted(source._AST_SOURCE_SUFFIXES), "basenames": sorted(source._AST_SOURCE_BASENAMES),
            "default_ignores": list(source._DEFAULT_IGNORE_PATTERNS), "snapshots": rows}
