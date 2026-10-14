"""Actual owned HTTP observations for Source's customizable no-team branch."""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
from unittest.mock import patch


def teamless_http_contracts(scratch: Path) -> dict:
    from fastapi.testclient import TestClient
    from mini_loop import TokenAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app

    scratch = Path(scratch)
    environment = {key: value for key, value in os.environ.items()
                   if key not in {"MINILOOP_API_TOKEN", "MINILOOP_API_TOKENS"}}
    settings = Settings(fake_llm=True, trajectory_enabled=False,
                        ast_outline_enabled=False, workspace_root=scratch / "workspaces",
                        skills_dir=scratch / "empty-skills", spill_dir=None,
                        rate_limit_per_minute=1)
    model = FakeAsyncAnthropic(delay=0, thinking=False)
    manager = SessionManager(settings, model)
    alice, bob = manager.create(owner="alice"), manager.create(owner="bob")
    original_agent = alice.agent
    original_team = original_agent.state["team_id"]
    mailbox = manager.bus._path(original_team + "/lead")
    mailbox.parent.mkdir(parents=True, exist_ok=True)
    valid_mailbox = b'{"content":"unread one","metadata":{}}\n{"content":"unread two","metadata":{}}\n'
    mailbox.write_bytes(valid_mailbox)
    aliases = {alice.id: "session-alice", bob.id: "session-bob"}
    rows = []

    def normalized(value):
        if isinstance(value, str):
            for actual, symbolic in aliases.items():
                value = value.replace(actual, symbolic)
            return value
        if isinstance(value, list):
            return [normalized(item) for item in value]
        if isinstance(value, dict):
            return {key: normalized(item) for key, item in value.items()}
        return value

    app = create_app(settings=settings, manager=manager)
    with patch.dict(os.environ, environment, clear=True), TestClient(app, raise_server_exceptions=False) as http:
        app.state.auth = TokenAuth({"token-a": "alice", "token-b": "bob"})
        # Wrap the real method: successful reads still execute the actual bus
        # peek. A zero count in no-team responses shows that branch bypasses it.
        with patch.object(manager, "peek_team_inbox", wraps=manager.peek_team_inbox) as peek:
            def call(name, state, *, token="token-a", target=None, method="GET"):
                before = mailbox.read_bytes()
                previous_calls = peek.call_count
                response = http.request(method, f"/sessions/{target or alice.id}/team",
                                        headers={"Authorization": "Bearer " + token} if token else {})
                body = response.json() if response.headers.get("content-type", "").startswith("application/json") else response.text
                after = mailbox.read_bytes()
                rows.append(dict(name=name, state=state, method=method,
                                 path=normalized(f"/sessions/{target or alice.id}/team"), token=token,
                                 status=response.status_code, response=normalized(body),
                                 headers={key: response.headers[key] for key in
                                          ("content-type", "content-security-policy", "x-content-type-options", "x-frame-options", "referrer-policy")
                                          if key in response.headers},
                                 actual_peek_calls=peek.call_count - previous_calls,
                                 mailbox_before_hex=before.hex(),
                                 mailbox_before_sha256=hashlib.sha256(before).hexdigest(),
                                 mailbox_after_sha256=hashlib.sha256(after).hexdigest(),
                                 mailbox_unchanged=before == after, model_calls=model.calls))

            call("default-lead", "default-managed")
            original_agent.state.pop("team_id")
            call("missing-team-key", "missing-key")
            original_agent.state["team_id"] = None
            call("null-team-key", "explicit-none")
            alice.agent = None
            call("detached-agent", "detached-agent")
            alice.agent = original_agent
            call("foreign-teamless", "explicit-none", token="token-b")
            call("no-auth-teamless", "explicit-none", token="")
            call("bad-auth-teamless", "explicit-none", token="wrong")
            call("unknown-teamless", "explicit-none", target="missing")
            call("wrong-method-teamless", "explicit-none", method="POST")
            mailbox.write_bytes(b"\xff")
            call("teamless-corrupt-mailbox", "explicit-none")
            original_agent.state["team_id"] = original_team
            call("restored-corrupt-mailbox", "restored-managed")
            mailbox.write_bytes(valid_mailbox)
            call("restored-lead", "restored-managed")

    package = Path(__file__).resolve().parents[1] / "mini_loop"
    result = dict(cases=rows, sessions_created=2, model_calls=model.calls,
                  source_sha256={"mini_loop/" + name: hashlib.sha256((package / name).read_bytes()).hexdigest()
                                 for name in ("server.py", "manager.py", "session.py", "auth.py")},
                  producer_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                  scope=[
                      "Actual Source FastAPI handler requests, real SessionManager and MessageBus.peek; no projected stand-in for a route response.",
                      "Default management always binds team=session id/name=lead. No-team cases explicitly customize the public Agent.state dictionary or AgentSession.agent property after creation.",
                      "Only random session identifiers are normalized; complete response, selected security headers, mailbox bytes and real peek/model call counts are retained.",
                      "Missing/None team identity and a detached Agent return no-team without reading even a corrupt mailbox. Auth and ownership denial remain before the view.",
                      "This Source corpus does not prove that a native public manager can construct the same state. Native default managed sessions currently always bind a private team identity.",
                      "No arbitrary false-valued Python object, native custom constructor, OS confinement, full team lifecycle, SDK surrogate or whole-port acceptance claim.",
                  ])
    json.dumps(result, allow_nan=False)
    return result
