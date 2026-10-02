"""The console renders untrusted content safely, and headers bound the rest.

The SSE console holds an API token in localStorage and renders event fields
that carry model text and tool output -- content an attacker can influence.
It renders every one of them through `textContent`, which is XSS-safe. But
that safety was an unguarded property: a single future refactor to `innerHTML`
would reintroduce the hole silently, and there was no second line of defense.

OpenWorker's review flags exactly this shape (research doc 9.2.4): a null CSP
means that if content injection ever happens, the injected script can read the
token and POST it anywhere. Round 106 adds both halves:

* a scan pinning that the console uses no unsafe DOM sink, so the
  textContent-only property is load-bearing;
* security headers on every response -- a CSP whose `default-src 'none'` +
  `connect-src 'self'` blocks token exfiltration to any other origin even if a
  sink slips in, plus nosniff / frame / referrer hardening.
"""

import re

import pytest

from mini_loop.server import CONSOLE_HTML, SECURITY_HEADERS

#: DOM sinks that turn a string into live markup. `textContent` is not here.
UNSAFE_SINKS = (
    "innerHTML",
    "outerHTML",
    "insertAdjacentHTML",
    "document.write",
    "eval(",
    "new Function",
    ".setHTML(",
)


def test_the_console_uses_no_unsafe_dom_sink():
    """Every event field reaches the DOM through textContent. If this fails,
    a rendering path can turn tool output or model text into live script."""

    present = [sink for sink in UNSAFE_SINKS if sink in CONSOLE_HTML]
    assert not present, (
        f"the console gained an unsafe DOM sink: {present}. Render through "
        "textContent, or a crafted tool result becomes script in the "
        "operator's browser."
    )


def test_the_console_actually_renders_through_textcontent():
    """Not vacuous: the absence of sinks means something only if the console
    does render dynamic content at all."""

    assert CONSOLE_HTML.count("textContent") >= 5


def _headers(tmp_path, *, path="/", token="tok-alice-000000000000"):
    from fastapi.testclient import TestClient

    from mini_loop import SessionManager, Settings
    from mini_loop.auth import TokenAuth
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.server import create_app
    import pathlib

    skills = pathlib.Path(__file__).resolve().parent.parent / "skills"
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=skills),
        FakeAsyncAnthropic(),
    )
    app = create_app(manager=manager)
    with TestClient(app) as client:
        app.state.auth = TokenAuth({token: "alice"})
        return client.get(path, headers={"Authorization": f"Bearer {token}"}).headers


def test_the_csp_blocks_token_exfiltration(tmp_path):
    """The one move that matters after an XSS is sending the token elsewhere.
    default-src 'none' + connect-src 'self' block fetch/XHR/WebSocket/img/form
    to any other origin."""

    csp = _headers(tmp_path)["content-security-policy"]
    assert "default-src 'none'" in csp
    assert "connect-src 'self'" in csp
    assert "form-action 'none'" in csp
    # No wildcard or external host that would reopen the channel.
    assert "*" not in csp and "http" not in csp


def test_the_csp_still_lets_the_console_work(tmp_path):
    """The console is inline script + inline style + same-origin fetch/SSE.
    A policy that blocked those would ship a blank page."""

    csp = _headers(tmp_path)["content-security-policy"]
    assert "script-src 'unsafe-inline'" in csp
    assert "style-src 'unsafe-inline'" in csp


def test_hardening_headers_are_present_on_every_response(tmp_path):
    for path in ("/", "/healthz"):
        headers = _headers(tmp_path, path=path)
        assert headers["x-content-type-options"] == "nosniff"
        assert headers["x-frame-options"] == "DENY"
        assert headers["referrer-policy"] == "no-referrer"


def test_headers_reach_an_auth_denied_response(tmp_path):
    """The security middleware wraps `authenticate`, so even a 401 carries the
    headers -- an error page is still a page a browser renders."""

    from fastapi.testclient import TestClient

    from mini_loop import SessionManager, Settings
    from mini_loop.auth import TokenAuth
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.server import create_app
    import pathlib

    skills = pathlib.Path(__file__).resolve().parent.parent / "skills"
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=skills),
        FakeAsyncAnthropic(),
    )
    app = create_app(manager=manager)
    with TestClient(app) as client:
        app.state.auth = TokenAuth({"tok-alice-000000000000": "alice"})
        denied = client.get("/sessions")  # no Authorization header
        assert denied.status_code in (401, 403)
        assert "content-security-policy" in denied.headers
        assert denied.headers["x-content-type-options"] == "nosniff"


def test_the_sink_list_is_not_empty():
    """A scan that checks nothing passes forever."""

    assert len(UNSAFE_SINKS) >= 5
    assert set(SECURITY_HEADERS) >= {
        "Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options",
    }
