"""Real Source HTTP turns with manager-selected readonly Guardian approvals."""
import tempfile
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path


def guardian_runtime_contracts() -> dict:
    from fastapi.testclient import TestClient
    from mini_loop.builtins import full_registry
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, system_text, text, tool
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app

    rows = []
    for name, enabled, answer, human, mode, command, hostile, fault in [
        ("off-human-allow", False, "ALLOW reviewed", True, "interactive", "rm fixture-canary.txt", False, False),
        ("off-human-deny", False, "ALLOW reviewed", False, "interactive", "rm fixture-canary.txt", False, False),
        ("automatic-allow", True, "ALLOW reviewed", False, "interactive", "rm fixture-canary.txt", False, False),
        ("automatic-deny", True, "DENY reviewed", False, "interactive", "rm fixture-canary.txt", False, False),
        ("defer-human-allow", True, "DEFER uncertain", True, "interactive", "rm fixture-canary.txt", False, False),
        ("defer-human-deny", True, "DEFER uncertain", False, "interactive", "rm fixture-canary.txt", False, False),
        ("hostile-review-deny", True, "DENY reviewed", False, "interactive", "rm fixture-canary.txt", True, False),
        ("readonly-denial", True, "ALLOW reviewed", False, "readonly", "rm fixture-canary.txt", False, False),
        ("immutable-denial", True, "ALLOW reviewed", False, "interactive", "rm -rf /", False, False),
        ("provider-error-defer", True, "fixture provider fault", True, "interactive", "rm fixture-canary.txt", False, True),
        ("provider-error-verdict", True, "ALLOW provider fault", False, "interactive", "rm fixture-canary.txt", False, True),
    ]:
        with tempfile.TemporaryDirectory(prefix="mini-loop-guardian-http-") as directory:
            root = Path(directory).resolve()
            settings = Settings(fake_llm=True, workspace_root=root,
                skills_dir=root / "skills", trajectory_enabled=False,
                spill_dir=None, guardian_enabled=enabled, model="configured")
            observations = dict(parent_names=[], parent_results=[], review_names=[],
                review_prompts=[], review_results=[])

            def respond(request):
                reviewing = system_text(request["system"]) == (
                    "You are a security reviewer. You may read to inform your "
                    "judgment but never modify anything.")
                prefix = "review" if reviewing else "parent"
                observations[prefix + "_names"].append([entry["name"] for entry in request["tools"]])
                results = [block["content"] for message in request["messages"]
                    if isinstance(message.get("content"), list) for block in message["content"]
                    if isinstance(block, dict) and block.get("type") == "tool_result"]
                observations[prefix + "_results"].extend(results)
                if reviewing:
                    observations["review_prompts"].append(request["messages"][0]["content"])
                    if fault:
                        raise RuntimeError(answer)
                    if hostile and not results:
                        return [tool("write_file", _id="review-write", path="planted.txt", content="x")], "tool_use"
                    return [text(answer)], "end_turn"
                if results:
                    return [text("done")], "end_turn"
                return [tool("bash", _id="action", command=command)], "tool_use"

            def factory(cfg):
                registry = full_registry(tasks=False, background=False, memory=False,
                    cron=False, plan=False, goals=False, diagnostics=False,
                    session_query=False, teams=False, worktrees=False, mcp=False,
                    self_audit=False)
                return SessionManager(cfg, FakeAsyncAnthropic(responder=respond,
                    thinking=False), tool_registry=registry)

            with TestClient(create_app(settings=settings, manager_factory=factory)) as http:
                created = http.post("/sessions", json={"mode": mode})
                assert created.status_code == 200, created.text
                info = created.json()
                workspace = Path(info["workspace"])
                canary = workspace / "fixture-canary.txt"
                canary.write_text("fixture")
                parked = False
                with ThreadPoolExecutor(max_workers=1) as workers:
                    pending_turn = workers.submit(http.post, f"/sessions/{info['id']}/messages",
                        json={"message": "review fixture action"})
                    deadline = time.monotonic() + 5
                    while not pending_turn.done():
                        response = http.get(f"/sessions/{info['id']}/approvals")
                        assert response.status_code == 200, response.text
                        pending = response.json()["approvals"]
                        if pending:
                            assert not parked, "more than one human question"
                            parked = True
                            resolution = http.post(f"/sessions/{info['id']}/approvals/{pending[0]['approval_id']}",
                                json={"decision": "allow" if human else "deny"})
                            assert resolution.status_code == 200, resolution.text
                        assert time.monotonic() < deadline, "Source approval turn did not finish"
                        time.sleep(0.001)
                    response = pending_turn.result()
                assert response.status_code == 200, response.text
                assert http.get(f"/sessions/{info['id']}/approvals").json()["approvals"] == []
                assert not (workspace / "planted.txt").exists()
                rows.append(dict(name=name, enabled=enabled, answer=answer, human=human,
                    mode=mode, command=command, hostile=hostile, fault=fault,
                    output=response.json()["final"], parked=parked,
                    canary_exists=canary.exists(), file_exists=False, **observations))
    assert len(rows) == 11
    return dict(http=rows)
