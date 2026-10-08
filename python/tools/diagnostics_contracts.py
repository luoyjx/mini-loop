"""Actual Source diagnostics scans and installed provider/tool boundaries."""
from __future__ import annotations

import asyncio
import sys
import tempfile
from dataclasses import asdict
from pathlib import Path
from types import SimpleNamespace


def diagnostics_contracts() -> dict:
    from mini_loop.diagnostics import (
        MAX_FILES, MAX_FILE_BYTES, MAX_REPORTED, Diagnostic,
        PythonSyntaxDiagnostics, install_diagnostics,
    )
    from mini_loop.registry import ToolContext, ToolRegistry
    from mini_loop.tools import Toolset

    recipes = [
        dict(name="empty", files=[], input={}),
        dict(name="valid", files=[dict(path="ok.py", text="def ok(x: int):\n    return x + 1\n")], input={}),
        dict(name="syntax", files=[dict(path="bad.py", text="def broken(:\n")], input={}),
        dict(name="sorted-tree", files=[dict(path="z.py", text="x =\n"),
            dict(path="a/first.py", text="if True\n    pass\n"),
            dict(path="ignore.PY", text="x =\n")], input={}),
        dict(name="hidden-tree", files=[dict(path=".hidden.py", text="x =\n"),
            dict(path=".nested/问题.py", text="x =\n")], input={}),
        dict(name="target-only", files=[dict(path="a.py", text="x =\n"),
            dict(path="b.py", text="x =\n")], input=dict(path="b.py")),
        dict(name="non-python", files=[dict(path="bad.txt", text="x =\n")], input=dict(path="bad.txt")),
        dict(name="missing-non-python", files=[], input=dict(path="missing.txt")),
        dict(name="missing-python", files=[], input=dict(path="missing.py")),
        dict(name="directory-python", files=[dict(path="dir.py", directory=True)], input=dict(path="dir.py")),
        dict(name="null-path", files=[dict(path="bad.py", text="x =\n")], input=dict(path=None)),
        dict(name="empty-path", files=[dict(path="bad.py", text="x =\n")], input=dict(path="")),
        dict(name="parent-refusal", files=[], input=dict(path="../outside/bad.py")),
        dict(name="absolute-inside", files=[dict(path="bad.py", text="x =\n")], input=dict(path="<workspace>/bad.py")),
        dict(name="absolute-outside", files=[], input=dict(path="<outside>/bad.py")),
        dict(name="symlink-target-refusal", files=[dict(path="link.py", symlink="<outside>/bad.py")], input=dict(path="link.py")),
        # Source confines explicit paths but does not re-resolve each tree row.
        # Keep this observation rather than silently claiming tree confinement.
        dict(name="symlink-tree", files=[dict(path="link.py", symlink="<outside>/bad.py")], input={}),
        dict(name="symlink-directory-tree", files=[dict(path="linked", symlink="<outside>")], input={}),
        dict(name="broken-symlink", files=[dict(path="link.py", symlink="<outside>/missing.py")], input={}),
        dict(name="invalid-utf8-in-string", files=[dict(path="ok.py", bytes_hex="78203d2027ff270a")], input={}),
        dict(name="invalid-utf8-in-code", files=[dict(path="bad.py", bytes_hex="78203d20ff0a")], input={}),
        dict(name="nul-source", files=[dict(path="bad.py", text="x = '\x00'\n")], input={}),
        dict(name="byte-limit-exact", files=[dict(path="limit.py", text="#", repeat=MAX_FILE_BYTES)], input={}),
        dict(name="byte-limit-over", files=[dict(path="limit.py", text="#", repeat=MAX_FILE_BYTES + 1)], input={}),
        dict(name="file-cap", files=[dict(path=f"f{index:03}.py", text="pass\n" if index < MAX_FILES else "x =\n")
            for index in range(MAX_FILES + 1)], input={}),
        dict(name="report-cap", files=[dict(path=f"f{index:03}.py", text="x =\n")
            for index in range(MAX_REPORTED + 3)], input={}),
        dict(name="nonempty-list-path", files=[], input=dict(path=["bad.py"])),
        dict(name="empty-list-path", files=[dict(path="bad.py", text="x =\n")], input=dict(path=[])),
        dict(name="unknown-key", files=[], input=dict(extra="unrecognized")),
    ]

    def observe_failure(error: Exception, normalize) -> dict:
        return dict(error_class=type(error).__name__, error=normalize(str(error)))

    async def observe():
        rows = []
        with tempfile.TemporaryDirectory(prefix="mini-loop-diagnostics-contract-") as directory:
            base = Path(directory).resolve()
            outside = base / "outside"
            outside.mkdir()
            (outside / "bad.py").write_text("x =\n", encoding="utf-8")
            for recipe in recipes:
                root = base / recipe["name"]
                toolset = Toolset(root)
                normalize = lambda text: text.replace(str(root), "<workspace>").replace(str(outside), "<outside>")
                materialize = lambda text: text.replace("<workspace>", str(root)).replace("<outside>", str(outside))
                for entry in recipe["files"]:
                    path = root / entry["path"]
                    path.parent.mkdir(parents=True, exist_ok=True)
                    if entry.get("directory"):
                        path.mkdir()
                    elif "symlink" in entry:
                        path.symlink_to(materialize(entry["symlink"]))
                    else:
                        body = bytes.fromhex(entry["bytes_hex"]) if "bytes_hex" in entry else (entry["text"] * entry.get("repeat", 1)).encode("utf-8")
                        path.write_bytes(body)
                registry = ToolRegistry()
                install_diagnostics(registry)
                tool = registry.get("diagnostics")
                assert tool is not None
                input_ = dict(recipe["input"])
                if isinstance(input_.get("path"), str):
                    input_["path"] = materialize(input_["path"])
                ctx = ToolContext(SimpleNamespace(toolset=toolset), root, {})
                try:
                    observation = dict(output=normalize(await tool.run(ctx, **input_)))
                except Exception as error:
                    observation = observe_failure(error, normalize)
                rows.append(dict(**recipe, **observation))

            class RecordingProvider:
                scope = "injected types only"

                def __init__(self, found, failure=None):
                    self.found, self.failure, self.calls = found, failure, []

                def diagnose(self, root, target):
                    self.calls.append(dict(root="<workspace>", target=None if target is None else str(target.relative_to(root))))
                    if self.failure is not None:
                        raise self.failure
                    return self.found

            provider_rows = []
            root = base / "provider"
            toolset = Toolset(root)
            for name, count, input_, failure in [
                ("clean", 0, {}, None),
                ("single", 1, dict(path="nested/../module.py"), None),
                ("exact-cap", MAX_REPORTED, {}, None),
                ("over-cap", MAX_REPORTED + 3, {}, None),
                ("provider-error", 0, {}, RuntimeError("provider fault")),
                ("refuse-before-provider", 1, dict(path="../outside/bad.py"), None),
            ]:
                found = [Diagnostic(f"module{index}.py", index + 1, f"type mismatch {index}", "warning") for index in range(count)]
                provider = RecordingProvider(found, failure)
                registry = ToolRegistry()
                install_diagnostics(registry, provider)
                tool = registry.get("diagnostics")
                ctx = ToolContext(SimpleNamespace(toolset=toolset), root, {})
                try:
                    observation = dict(output=await tool.run(ctx, **input_))
                except Exception as error:
                    observation = observe_failure(error, lambda text: text)
                provider_rows.append(dict(name=name, scope=provider.scope, diagnostics=[asdict(d) for d in found],
                    input=input_, calls=provider.calls, **observation))

            registry = ToolRegistry()
            install_diagnostics(registry)
            tool = registry.get("diagnostics")
            return dict(interpreter=dict(implementation=sys.implementation.name, version=list(sys.version_info[:3])),
                limits=dict(files=MAX_FILES, file_bytes=MAX_FILE_BYTES, reported=MAX_REPORTED),
                schema=tool.schema, readonly=tool.readonly, parallel_safe=tool.parallel_safe,
                risk=tool.risk, capabilities=sorted(tool.capabilities), rows=rows, providers=provider_rows)

    return asyncio.run(observe())
