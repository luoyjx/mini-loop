"""Permission decisions observed through real managed Source session events."""

import asyncio
import copy
import hashlib
import os
import tempfile
from dataclasses import replace
from pathlib import Path
from unittest.mock import patch


PERMISSION_FIELDS = (
    "type", "decision", "rule", "tool", "reason", "agent", "depth",
    "message_id", "session",
)
MASK_VALUE = "secret_token"
MASK_REPLACEMENT = "<hidden>"


def permission_event_contracts() -> dict:
    from mini_loop import RunContext, SessionManager, Settings, Tool, ToolRegistry
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.permissions import PermissionHook
    from mini_loop.registry import Hooks
    from mini_loop.secrets import SecretRegistry

    def recipe(name, mode, *, tool_name="probe", risk="external", command="echo safe",
               answers=(), approval_fault="", masked=False):
        return dict(name=name, mode=mode, tool_name=tool_name, risk=risk,
                    command=command, answers=list(answers),
                    approval_fault=approval_fault, masked=masked)

    cases = [
        recipe("readonly-read", "readonly", risk="read"),
        recipe("readonly-exec", "readonly", risk="exec"),
        recipe("readonly-external", "readonly"),
        recipe("readonly-unclassified", "readonly", risk=None),
        recipe("immutable-auto", "auto", tool_name="bash", risk="exec", command="sudo safe"),
        recipe("auto-external", "auto"),
        recipe("interactive-allow", "interactive", answers=(True,)),
        recipe("interactive-deny", "interactive", answers=(False,)),
        recipe("auto-two-rules", "auto", tool_name="bash", command="rm safe"),
        recipe("interactive-two-allow", "interactive", tool_name="bash", command="rm safe",
               answers=(True, True)),
        recipe("interactive-later-deny", "interactive", tool_name="bash", command="rm safe",
               answers=(True, False)),
        recipe("approval-later-error", "interactive", tool_name="bash", command="rm safe",
               answers=(True,), approval_fault="error"),
        recipe("approval-later-cancel", "interactive", tool_name="bash", command="rm safe",
               answers=(True,), approval_fault="cancel"),
        recipe("masked-interactive", "interactive", tool_name="probe_secret_token",
               command="echo secret_token", answers=(True,), masked=True),
    ]

    async def capture(spec):
        observed, ordered, handlers, permissions, results = [], [], [], [], []
        answers_used = callback_count = request_count = 0
        manager = None

        async def approve(ctx, call, rule):
            nonlocal callback_count
            callback_count += 1
            ordered.append(dict(type="approval_callback", tool=call.name, rule=rule.name))
            if callback_count == 2:
                if spec["approval_fault"] == "error":
                    raise RuntimeError("fixture approval failed")
                if spec["approval_fault"] == "cancel":
                    raise asyncio.CancelledError()
            return await manager.approvals.ask(ctx, call, rule)

        async def handler(ctx, command):
            ordered.append(dict(type="handler", tool=ctx.call.name))
            handlers.append(dict(tool=ctx.call.name, command=command))
            return "handled:" + command

        def respond(request):
            nonlocal request_count
            request_count += 1
            if request_count == 1:
                return [tool(spec["tool_name"], _id="permission-tool", command=spec["command"])], "tool_use"
            return [text("done")], "end_turn"

        async def sink(event):
            nonlocal answers_used
            observed.append(copy.deepcopy(event))
            kind = event["type"]
            if kind in ("tool_use", "permission", "tool_result"):
                row = dict(type=kind, tool=event.get("tool", event.get("name")))
                if kind == "permission":
                    row.update(decision=event["decision"], rule=event["rule"])
                    permissions.append({key: copy.deepcopy(event[key]) for key in PERMISSION_FIELDS})
                elif kind == "tool_result":
                    results.append({key: copy.deepcopy(event[key]) for key in
                                    ("id", "name", "output", "error", "denied") if key in event})
                ordered.append(row)
            if kind == "approval_required":
                answer = spec["answers"][answers_used]
                answers_used += 1
                assert manager.approvals.resolve(event["approval_id"], session_id=event["session"],
                                                 allowed=answer)

        with tempfile.TemporaryDirectory(prefix="mini-loop-permission-events-") as temp:
            root = Path(temp).resolve()
            secrets = SecretRegistry(mask_with=MASK_REPLACEMENT)
            if spec["masked"]:
                secrets.register("FIXTURE", MASK_VALUE)
            registry = ToolRegistry([Tool(
                spec["tool_name"], "Safe permission fixture; never executes a command",
                {"type": "object", "properties": {"command": {"type": "string"}},
                 "required": ["command"]}, handler,
                risk=spec["risk"], readonly=spec["risk"] == "read", parallel_safe=False,
            )])
            manager = SessionManager(
                Settings(fake_llm=True, model="fixture-model", workspace_root=root / "sessions",
                         skills_dir=root / "skills", spill_dir=None, trajectory_enabled=False,
                         guardian_enabled=False, max_turns=4),
                FakeAsyncAnthropic(responder=respond, thinking=False, delay=0),
                tool_registry=registry, hooks=Hooks([PermissionHook(approval=approve)]),
                secrets=secrets, event_sink=sink, enable_features=False,
            )
            try:
                session = manager.create(owner="fixture-owner", permission_mode=spec["mode"],
                                         system="Permission event contract.")
                context = replace(RunContext.explicit_human(actor_id="fixture-human"),
                                  message_id="permission-turn")
                output, error = None, None
                try:
                    async with asyncio.timeout(5):
                        output = await session.run("observe permission", run_context=context)
                except asyncio.CancelledError as exc:
                    error = dict(type=type(exc).__name__, text=str(exc))
                except Exception as exc:
                    error = dict(type=type(exc).__name__, text=str(exc))
                # Sink failures are contained by the runtime, but cannot quietly
                # turn a broken capture into apparently successful evidence.
                assert session.sink_error is None, session.sink_error
                assert answers_used == len(spec["answers"])
                assert [event["seq"] for event in observed] == list(range(1, len(observed) + 1))
                assert all(event["session"] == session.id for event in observed)
                for event in permissions:
                    assert event["message_id"] == context.message_id
                    event["session"] = "session"
                    event["message_id"] = "turn"
                return dict(**spec, ordered_trace=ordered, permission_events=permissions,
                            output=output, error=error, handler_calls=handlers,
                            tool_results=results, status=session.status,
                            pending_approvals=len(manager.approvals.list(session.id)))
            finally:
                await manager.stop()

    async def run():
        return [await capture(spec) for spec in cases]

    # Ambient optional services/settings must not change this finite Source run.
    # The real session, agent, PermissionHook and broker execute unchanged.
    with patch.dict(os.environ, {}, clear=True):
        rows = asyncio.run(run())
    package = Path(__file__).resolve().parents[1] / "mini_loop"
    sources = {"mini_loop/" + name: package / name for name in
               ("agent.py", "session.py", "manager.py", "permissions.py", "registry.py",
                "approvals.py", "secrets.py", "fake_llm.py")}
    sources["tools/permission_event_contracts.py"] = Path(__file__)
    return dict(
        source_sha256={name: hashlib.sha256(path.read_bytes()).hexdigest()
                       for name, path in sources.items()},
        scope="Actual SessionManager/AgentSession.run with FakeAsyncAnthropic, PermissionHook and ApprovalBroker; safe handlers only, no shell or live network. Permission fields are emitted and masked by the real session; only random session identity and supplied turn identity are normalized. Private callback/handler observations retain actual arguments.",
        mask=dict(value=MASK_VALUE, replacement=MASK_REPLACEMENT), cases=rows,
    )
