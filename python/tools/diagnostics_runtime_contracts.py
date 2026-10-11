"""Real Source HTTP/model turns with an explicitly installed diagnostics provider."""
from __future__ import annotations

import tempfile
from pathlib import Path


def diagnostics_runtime_contracts() -> dict:
    from fastapi.testclient import TestClient
    from mini_loop.builtins import full_registry
    from mini_loop.config import Settings
    from mini_loop.diagnostics import Diagnostic, install_diagnostics
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app

    rows = []
    for name, enabled, input_, failure in [
        ("disabled", False, {}, False),
        ("tree", True, {}, False),
        ("target", True, dict(path="nested/../module.py"), False),
        ("null-target", True, dict(path=None), False),
        ("escape", True, dict(path="../outside.py"), False),
        ("provider-error", True, {}, True),
    ]:
        with tempfile.TemporaryDirectory(prefix="mini-loop-diagnostic-runtime-") as directory:
            root = Path(directory).resolve()
            settings = Settings(fake_llm=True, workspace_root=root,
                skills_dir=root / "skills", trajectory_enabled=False, model="configured")
            observations = dict(names=[], results=[], calls=[])

            class ObservedProvider:
                scope = "fixture type checker"

                def diagnose(self, workspace, target):
                    observations["calls"].append(dict(root="<workspace>",
                        target=None if target is None else str(target.relative_to(workspace))))
                    if failure:
                        raise RuntimeError("fixture diagnostic failure")
                    return [Diagnostic("module.py", 7, "fixture type issue", "warning")]

            provider = ObservedProvider()

            def respond(request):
                observations["names"].append([entry["name"] for entry in request["tools"]])
                results = [block["content"] for message in request["messages"]
                    if isinstance(message.get("content"), list) for block in message["content"]
                    if isinstance(block, dict) and block.get("type") == "tool_result"
                    and block.get("tool_use_id") == "diagnostic"]
                if results:
                    observations["results"].extend(results)
                    return [text("done")], "end_turn"
                if enabled:
                    return [tool("diagnostics", _id="diagnostic", **input_)], "tool_use"
                return [text("done")], "end_turn"

            def factory(cfg):
                registry = full_registry(tasks=False, background=False, memory=False,
                    cron=False, plan=False, goals=False, diagnostics=False,
                    session_query=False, teams=False, worktrees=False, mcp=False,
                    self_audit=False)
                if enabled:
                    install_diagnostics(registry, provider)
                return SessionManager(cfg, FakeAsyncAnthropic(responder=respond,
                    thinking=False), tool_registry=registry)

            with TestClient(create_app(settings=settings, manager_factory=factory)) as http:
                created = http.post("/sessions", json={"mode": "readonly"})
                assert created.status_code == 200, created.text
                response = http.post(f"/sessions/{created.json()['id']}/messages",
                    json={"message": "diagnose workspace"})
                assert response.status_code == 200, response.text
                final = response.json()["final"]
            rows.append(dict(name=name, enabled=enabled, input=input_, failure=failure,
                output=final, scope=provider.scope, **observations))
    return dict(http=rows)
