"""The web UI inherits every safety property the console pinned (R1).

Same threat model as test_console_safety.py: the page holds an API token in
localStorage and renders model text, tool output, and skill/approval prose --
all attacker-influenceable. Same contract: textContent only, no external
resources (the CSP's script-src is 'unsafe-inline' with no 'self', so an
external script would not even load -- a reference to one is a bug by
construction), one self-contained response.
"""

import re

import pytest

from mini_loop.webui import render_page

UNSAFE_SINKS = (
    "innerHTML",
    "outerHTML",
    "insertAdjacentHTML",
    "document.write",
    "eval(",
    "new Function",
    ".setHTML(",
)


def test_the_ui_uses_no_unsafe_dom_sink():
    page = render_page()
    present = [sink for sink in UNSAFE_SINKS if sink in page]
    assert not present, (
        f"the web UI gained an unsafe DOM sink: {present}. Render through "
        "textContent, or a crafted tool result becomes script in the "
        "operator's browser."
    )


def test_the_ui_renders_through_textcontent():
    page = render_page()
    assert page.count("textContent") >= 10, (
        "the rendering helpers should reach the DOM through textContent"
    )


def test_the_page_is_self_contained():
    """No external script/style/img references: the CSP would block them,
    so any reference is a silently-broken feature waiting to be 'fixed' by
    weakening the CSP."""

    page = render_page()
    assert "/*CSS*/" not in page and "/*JS*/" not in page, "assembly markers left"
    for pattern in (r'src\s*=\s*["\']https?://', r'href\s*=\s*["\']https?://',
                    r"@import", r"url\(https?://"):
        assert not re.search(pattern, page), f"external reference: {pattern}"


def test_the_route_serves_the_page_with_security_headers(tmp_path):
    from fastapi.testclient import TestClient

    from mini_loop import SessionManager, Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.server import create_app

    skills = pathlib_skills()
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=skills, spill_dir=None)
    app = create_app(manager=SessionManager(settings, FakeAsyncAnthropic()),
                     settings=settings)
    with TestClient(app) as client:
        response = client.get("/ui")
        assert response.status_code == 200
        assert "mini-loop" in response.text
        assert "Content-Security-Policy" in response.headers
        assert "frame-ancestors 'none'" in response.headers["Content-Security-Policy"]


def pathlib_skills():
    import pathlib

    return pathlib.Path(__file__).resolve().parent.parent / "skills"


def test_the_ui_wires_only_existing_api_paths():
    """Every fetch/EventSource path in app.js must exist on the server.

    The UI consumes the API, never invents it: a path typo (or a UI built
    against a route that was renamed) should fail here, not in a browser
    three weeks later.
    """

    import pathlib

    js = (pathlib.Path(__file__).resolve().parent.parent
          / "mini_loop" / "webui" / "app.js").read_text()
    called = set(re.findall(r'"(/(?:sessions|healthz|trajectories)[^"]*)"', js))
    known_prefixes = (
        "/sessions", "/healthz", "/trajectories",
    )
    for path in called:
        assert path.startswith(known_prefixes), path
    # The composed paths in the code concatenate ids; check the literal
    # route stems the client uses against the server's route table.
    from mini_loop.server import app as default_app

    server_paths = {route.path for route in default_app.routes}
    for stem, template in (
        ("/healthz", "/healthz"),
        ("/sessions", "/sessions"),
        ("/messages", "/sessions/{session_id}/messages"),
        ("/steer", "/sessions/{session_id}/steer"),
        ("/cancel", "/sessions/{session_id}/cancel"),
        ("/fork", "/sessions/{session_id}/fork"),
        ("/mode", "/sessions/{session_id}/mode"),
        ("/approvals", "/sessions/{session_id}/approvals"),
        ("/events", "/sessions/{session_id}/events"),
        ("/tasks", "/sessions/{session_id}/tasks"),
        ("/team", "/sessions/{session_id}/team"),
        ("/goal", "/sessions/{session_id}/goal"),
        ("/memory", "/sessions/{session_id}/memory"),
        ("/skills", "/sessions/{session_id}/skills"),
        ("/cron", "/sessions/{session_id}/cron"),
        ("/benchmark", "/benchmark"),
        ("/propose-improvement", "/sessions/{session_id}/propose-improvement"),
    ):
        assert stem in js, f"the UI lost its {stem} wiring"
        assert template in server_paths, f"server lost {template}"
