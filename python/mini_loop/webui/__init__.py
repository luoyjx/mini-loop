"""The web UI: separate source files, self-contained delivery.

The console taught the constraints (see server.CONSOLE_CSP and
test_console_safety.py): the CSP allows inline script and style and
nothing external, every field renders through textContent, and the page
must be one self-contained response. A full multi-view UI in one Python
string is unmaintainable, so the sources live here as three files --
index.html with `/*CSS*/` and `/*JS*/` markers, app.css, app.js -- and
are assembled once at import into the single inline page the CSP
expects. No static mount, so there is no traversal surface; no build
step, so there is nothing to forget to run.
"""

from __future__ import annotations

from pathlib import Path

__all__ = ["render_page"]

_ROOT = Path(__file__).resolve().parent


def render_page() -> str:
    html = (_ROOT / "index.html").read_text(encoding="utf-8")
    css = (_ROOT / "app.css").read_text(encoding="utf-8")
    js = (_ROOT / "app.js").read_text(encoding="utf-8")
    # Markers, not str.format: the CSS/JS are full of braces.
    return html.replace("/*CSS*/", css, 1).replace("/*JS*/", js, 1)


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: a pure assembly of three source files read once "
    "per call; safety properties (textContent-only, no external refs) are "
    "pinned by scanning tests, not runtime checks."
)
