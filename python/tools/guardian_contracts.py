"""Real readonly Guardian model execution and verdict parsing observations."""
import asyncio
import tempfile
from pathlib import Path


def guardian_contracts() -> dict:
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.guardian import AgentGuardian
    from mini_loop.guardian import broker_reviewer
    from mini_loop.manager import SessionManager
    from mini_loop.registry import ToolCall, ToolContext
    from mini_loop.secrets import SecretRegistry
    from types import SimpleNamespace

    cases = [
        ("allow", "ALLOW safe", False), ("deny", "DENY unsafe", False),
        ("defer", "DEFER uncertain", False), ("lowercase", "allow safe", False),
        ("embedded", "I would ALLOW this", False), ("first-deny", "DENY or ALLOW", False),
        ("word-suffix", "ALLOWING", False), ("underscore", "_ALLOW_", False),
        ("unicode-prefix", "界ALLOW", False), ("unicode-suffix", "DENYé", False),
        ("combining-prefix", "\u0301ALLOW", False), ("punctuation", "(ALLOW)", False),
        ("multiline", "Heading\nDENY unsafe", False), ("unicode-line", "Heading\u2028ALLOW safe", False),
        ("long-reason", "ALLOW " + "😀" * 210, False), ("empty", "", False),
        ("hostile-write", "ALLOW after inspection", True),
        ("hostile-wide-catalog", "ALLOW after inspection", True),
    ]

    async def run():
        rows = []
        for name, answer, hostile in cases:
            with tempfile.TemporaryDirectory(prefix="mini-loop-guardian-") as temp:
                root = Path(temp).resolve()
                seen = []
                def respond(request):
                    seen.append(request)
                    if hostile and len(seen) == 1:
                        return [tool("write_file", _id="write", path="planted.txt", content="x")], "tool_use"
                    return [text(answer)], "end_turn"
                manager = SessionManager(Settings(fake_llm=True, model="configured", workspace_root=root / "sessions", skills_dir=root / "skills", spill_dir=None, trajectory_enabled=False),
                    FakeAsyncAnthropic(responder=respond, thinking=False), enable_features=False)
                parent = manager.create().agent
                wide = name == "hostile-wide-catalog"
                if wide:
                    class WidePolicy:
                        def select(self, role, registry):
                            return registry
                    parent.role_tool_policy = WidePolicy()
                verdict = await AgentGuardian(parent).review(tool="bash", rule="danger", message="approval required", input_preview='{"command": "rm fixture"}', session_id="observed")
                assert not (parent.workspace / "planted.txt").exists()
                outputs = [block["content"] for request in seen for message in request["messages"]
                    if isinstance(message.get("content"), list) for block in message["content"]
                    if isinstance(block, dict) and block.get("type") == "tool_result"]
                rows.append(dict(name=name, answer=answer, hostile=hostile, wide=wide,
                    verdict=None if verdict is None else dict(allowed=verdict[0], reason=verdict[1]),
                    names=[[entry["name"] for entry in request["tools"]] for request in seen],
                    prompt=seen[0]["messages"][0]["content"], system=seen[0]["system"], results=outputs,
                    parent_messages=len(parent.messages), file_exists=False))
                await manager.stop()
        bridges = []
        for name, command, secret, answer in [
            ("masked-allow", "curl fixture-credential", "fixture-credential", "ALLOW reviewed"),
            ("escaped-deny", 'echo "fixture\\credential"', "fixture\\credential", "DENY reviewed"),
            ("bounded-preview", "😀" * 800, "", "DENY reviewed"),
            ("defer", "inspect", "", "DEFER uncertain"),
        ]:
            with tempfile.TemporaryDirectory(prefix="mini-loop-guardian-bridge-") as temp:
                root = Path(temp).resolve()
                seen = []
                def respond(request):
                    seen.append(request)
                    return [text(answer)], "end_turn"
                secrets = SecretRegistry(mask_with="<secret-hidden>")
                if secret:
                    secrets.register("FIXTURE", secret)
                manager = SessionManager(Settings(fake_llm=True, model="configured", workspace_root=root / "sessions", skills_dir=root / "skills", spill_dir=None, trajectory_enabled=False),
                    FakeAsyncAnthropic(responder=respond, thinking=False), enable_features=False, secrets=secrets)
                parent = manager.create().agent
                verdict = await broker_reviewer(AgentGuardian(parent))(ToolContext(parent, parent.workspace, {}),
                    ToolCall("bash", dict(command=command), "tool"), SimpleNamespace(name="danger", message="approval required"))
                bridges.append(dict(name=name, command=command, secret=secret, answer=answer, verdict=verdict,
                    prompt=seen[0]["messages"][0]["content"]))
                await manager.stop()
        return dict(reviews=rows, bridges=bridges)
    return asyncio.run(run())
