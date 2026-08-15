"""The diagnostics seam: language intelligence behind one provider interface.

DeepSeek Harness reaches LSP servers through a seam (`dsh-lsp` behind
`lsp-stdio`) so the model-facing tool neither knows nor cares whether a real
language server, a linter, or a compiler answered. This module is that seam
for mini-loop, with deliberately narrow scope:

* `DiagnosticsProvider` is the interface a stdio LSP client would implement;
* `PythonSyntaxDiagnostics` is the zero-dependency built-in -- `ast.parse`
  over workspace Python files, bounded in file count and bytes, so the tool
  is honest about what it checks (syntax, not types);
* the `diagnostics` tool is workspace-confined through the same `safe_path`
  every file tool uses.

The seam is the deliverable: swapping in a real LSP moves this capability
without touching the tool, the registry, or the loop.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Protocol, runtime_checkable

__all__ = [
    "Diagnostic",
    "DiagnosticsProvider",
    "PythonSyntaxDiagnostics",
    "install_diagnostics",
]

#: Bounded work: files examined and bytes parsed per call.
MAX_FILES = 200
MAX_FILE_BYTES = 1_000_000
MAX_REPORTED = 50


@dataclass(frozen=True, slots=True)
class Diagnostic:
    path: str
    line: int
    message: str
    severity: str = "error"

    def render(self) -> str:
        return f"{self.path}:{self.line} [{self.severity}] {self.message}"


@runtime_checkable
class DiagnosticsProvider(Protocol):
    #: Human-readable statement of what this provider can actually see,
    #: rendered into the tool result so the model never over-trusts a clean
    #: report ("no syntax errors" is not "no bugs").
    scope: str

    def diagnose(self, root: Path, target: Path | None) -> list[Diagnostic]:
        """Diagnostics for `target` (a file), or for the tree under `root`."""
        ...


class PythonSyntaxDiagnostics:
    """`ast.parse` over Python files: syntax errors only, zero dependencies."""

    scope = "Python syntax only (ast.parse); clean output does not mean type- or logic-clean"

    def diagnose(self, root: Path, target: Path | None) -> list[Diagnostic]:
        if target is not None:
            files = [target] if target.suffix == ".py" else []
        else:
            files = sorted(root.rglob("*.py"))[:MAX_FILES]
        found: list[Diagnostic] = []
        import ast

        for path in files:
            try:
                if path.stat().st_size > MAX_FILE_BYTES:
                    found.append(Diagnostic(
                        str(path.relative_to(root)), 0,
                        f"skipped: over {MAX_FILE_BYTES:,} bytes", "warning",
                    ))
                    continue
                ast.parse(path.read_text(encoding="utf-8", errors="replace"))
            except SyntaxError as error:
                found.append(Diagnostic(
                    str(path.relative_to(root)),
                    int(error.lineno or 0),
                    error.msg or "syntax error",
                ))
            except OSError as error:
                found.append(Diagnostic(
                    str(path.relative_to(root)), 0, str(error), "warning",
                ))
            if len(found) >= MAX_REPORTED:
                break
        return found


_SCHEMA = {
    "type": "object",
    "properties": {
        "path": {
            "type": "string",
            "description": "Optional workspace-relative file; omit to check the tree.",
        },
    },
}


def install_diagnostics(registry, provider: DiagnosticsProvider | None = None) -> None:
    from .registry import Tool

    active = provider if provider is not None else PythonSyntaxDiagnostics()

    async def diagnostics(ctx, path: str | None = None) -> str:
        root = ctx.agent.toolset.workspace
        target = None
        if path:
            try:
                # The same confinement every file tool has: a diagnostics
                # request must not become a read primitive outside the
                # workspace.
                target = ctx.agent.toolset.safe_path(path)
            except ValueError as error:
                return f"Error: {error}"
        found = active.diagnose(root, target)
        header = f"[scope: {active.scope}]"
        if not found:
            return f"{header}\nNo diagnostics."
        lines = [d.render() for d in found[:MAX_REPORTED]]
        if len(found) >= MAX_REPORTED:
            lines.append(f"... (reporting capped at {MAX_REPORTED})")
        return "\n".join([header, *lines])

    registry.register(Tool(
        "diagnostics",
        "Run language diagnostics over the workspace (or one file). "
        "The result names the provider's scope; a clean report covers "
        "only that scope.",
        _SCHEMA,
        diagnostics,
        readonly=True,
        parallel_safe=True,
        risk="read",
    ))


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: providers are pure functions from files to "
    "findings; the workspace confinement is enforced by safe_path at entry."
)
