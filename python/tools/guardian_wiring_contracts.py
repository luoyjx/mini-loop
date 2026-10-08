"""Actual manager-selected Guardian reviews through the Source approval broker."""
import asyncio
import tempfile
from pathlib import Path
from types import SimpleNamespace


def guardian_wiring_contracts() -> dict:
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.manager import SessionManager
    from mini_loop.registry import ToolCall
    from mini_loop.secrets import SecretRegistry
    from mini_loop.storage import NullStateStore

    async def capture(name, *, enabled=True, answer="ALLOW reviewed", human=False,
                      fault=False, constructor_fault=False, cancel=False, missing="", hostile=False,
                      wide=False, repeat=False, remember=False, masked=False):
        writes, events, requests, inspections = [], [], [], []

        class Store(NullStateStore):
            def write_approval(self, row):
                writes.append(dict(row))

        def respond(request):
            requests.append(request)
            if cancel:
                raise asyncio.CancelledError()
            if fault:
                raise RuntimeError(answer)
            results = [block["content"] for message in request["messages"]
                       if isinstance(message.get("content"), list)
                       for block in message["content"]
                       if block.get("type") == "tool_result"]
            if (hostile or repeat) and not results:
                if hostile:
                    return [tool("write_file", _id="write", path="planted.txt", content="x")], "tool_use"
                return [tool("read_file", _id="read", path="marker.txt")], "tool_use"
            inspections.extend(results)
            return [text(answer)], "end_turn"

        with tempfile.TemporaryDirectory(prefix="mini-loop-guardian-wiring-") as temp:
            root = Path(temp).resolve()
            secrets = SecretRegistry(mask_with="<secret-hidden>")
            if masked:
                secrets.register("FIXTURE", "fixture-credential")
            manager = SessionManager(Settings(fake_llm=True, model="configured",
                workspace_root=root / "sessions", skills_dir=root / "skills",
                spill_dir=None, trajectory_enabled=False, guardian_enabled=enabled),
                FakeAsyncAnthropic(responder=respond, thinking=False),
                enable_features=False, secrets=secrets, state_store=Store())
            try:
                session = manager.create()
                parent = session.agent
                parent.messages.append(dict(role="user", content="private parent history"))
                if wide or constructor_fault:
                    class WidePolicy:
                        def select(self, role, registry):
                            if constructor_fault:
                                raise ValueError("fixture constructor fault")
                            return registry
                    parent.role_tool_policy = WidePolicy()
                if missing == "session":
                    parent.state.pop("session")

                class Context:
                    agent = None if missing == "agent" else parent

                    async def emit_event(self, event_type, **fields):
                        events.append(dict(type=event_type, **fields))

                call = ToolCall("bash", dict(command="git status fixture-credential"
                                if masked else "git status --short"), "action")
                rule = SimpleNamespace(name="fixture-rule", message="approval required")
                outcomes, parked = [], []
                for index in range(2 if repeat or remember else 1):
                    (parent.workspace / "marker.txt").write_text(f"current-{index}\n")
                    task = asyncio.create_task(manager.approvals.ask(Context(), call, rule))
                    # Wait for an actual terminal task or published human question.
                    async with asyncio.timeout(5):
                        while not task.done() and not manager.approvals.list(session.id):
                            await asyncio.sleep(0)
                    pending = manager.approvals.list(session.id)
                    parked.append(bool(pending))
                    if pending:
                        assert manager.approvals.resolve(pending[0]["approval_id"],
                            session_id=session.id, allowed=human, remember=remember)
                    try:
                        outcomes.append(await task)
                    except asyncio.CancelledError:
                        outcomes.append("cancelled")
                assert not (parent.workspace / "planted.txt").exists()
                assert parent.messages == [dict(role="user", content="private parent history")]
                # IDs and timestamps are nondeterministic; preserve all semantic fields.
                ids = {}
                for row in writes + events:
                    if "approval_id" in row:
                        ids.setdefault(row["approval_id"], f"apr_{len(ids)}")
                        row["approval_id"] = ids[row["approval_id"]]
                    if "session_id" in row:
                        assert row["session_id"] == session.id
                        row["session_id"] = "parent"
                    if "created_at" in row:
                        row["created_at"] = 0
                    if row.get("resolved_at") is not None:
                        row["resolved_at"] = 0
                return dict(name=name, enabled=enabled, answer=answer, human=human,
                    fault=fault, constructor_fault=constructor_fault,
                    cancel=cancel, missing=missing, hostile=hostile,
                    wide=wide, repeat=repeat, remember=remember, masked=masked,
                    reviewer_bound=manager.approvals.reviewer is not None,
                    outcomes=outcomes, parked=parked, writes=writes, events=events,
                    problems=list(manager.approvals.problems),
                    prompts=[request["messages"][0]["content"] for request in requests],
                    catalogues=[[entry["name"] for entry in request["tools"]] for request in requests],
                    inspections=inspections, parent_messages=1, file_exists=False,
                    remaining=manager.approvals.list(session.id))
            finally:
                await manager.stop()

    async def run():
        specs = [
            ("default-off-human-deny", dict(enabled=False)),
            ("default-off-human-allow", dict(enabled=False, human=True)),
            ("automatic-allow", {}),
            ("automatic-deny", dict(answer="DENY reviewed")),
            ("defer-human-allow", dict(answer="DEFER uncertain", human=True)),
            ("defer-human-deny", dict(answer="DEFER uncertain")),
            ("provider-fault-human-allow", dict(fault=True, answer="fixture provider fault", human=True)),
            ("provider-fault-allow-word", dict(fault=True, answer="ALLOW provider fault")),
            ("constructor-fault-human-allow", dict(constructor_fault=True, human=True)),
            ("provider-cancellation", dict(cancel=True)),
            ("missing-agent", dict(missing="agent")),
            ("missing-session", dict(missing="session")),
            ("hostile-narrow", dict(hostile=True)),
            ("hostile-wide", dict(hostile=True, wide=True)),
            ("fresh-review-current-file", dict(repeat=True)),
            ("human-grant-before-guardian", dict(answer="DEFER uncertain", human=True, remember=True)),
            ("masked-automatic-preview", dict(masked=True)),
        ]
        rows = [await capture(name, **options) for name, options in specs]
        by_name = {row["name"]: row for row in rows}
        assert by_name["automatic-allow"]["outcomes"] == [True]
        assert by_name["automatic-deny"]["outcomes"] == [False]
        assert by_name["provider-fault-allow-word"]["outcomes"] == [True]
        assert by_name["provider-cancellation"]["outcomes"] == ["cancelled"]
        assert by_name["constructor-fault-human-allow"]["problems"] == [
            "auto-reviewer raised on bash: ValueError"]
        assert by_name["fresh-review-current-file"]["inspections"] == ["current-0", "current-1"]
        assert by_name["human-grant-before-guardian"]["parked"] == [True, False]
        assert len(by_name["human-grant-before-guardian"]["prompts"]) == 1
        assert all(not row["remaining"] for row in rows)
        return dict(cases=rows)

    return asyncio.run(run())
