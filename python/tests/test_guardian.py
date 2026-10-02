"""The guardian: an agent answers approvals, bridged to the broker hook.

Completing an operator draft (round 216) whose docstring cites rounds
211/215: it reviews with a zero-write role agent and resolves through the
same broker path a human uses. The properties that matter are the safety
ones -- a guardian can only answer, never escalate, and every non-verdict
falls to the human.
"""

import asyncio
import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text
from mini_loop.guardian import AgentGuardian, Guardian, broker_reviewer

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _parent(tmp_path, responder):
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(responder=responder),
        tool_registry=full_registry(),
    )
    return manager.create().agent


def test_an_allow_verdict_is_parsed(tmp_path):
    parent = _parent(tmp_path, scripted([([text("ALLOW it is safe")], "end_turn")]))
    guardian = AgentGuardian(parent)
    assert isinstance(guardian, Guardian)
    verdict = asyncio.run(guardian.review(
        tool="bash", rule="destructive", message="confirm",
        input_preview="{}", session_id="s1"))
    assert verdict == (True, "ALLOW it is safe")


def test_a_deny_verdict_is_parsed(tmp_path):
    parent = _parent(tmp_path, scripted([([text("DENY that removes data")], "end_turn")]))
    verdict = asyncio.run(AgentGuardian(parent).review(
        tool="bash", rule="destructive", message="confirm",
        input_preview="{}", session_id="s1"))
    assert verdict == (False, "DENY that removes data")


def test_an_undecidable_reply_falls_through_to_the_human(tmp_path):
    parent = _parent(tmp_path, scripted([([text("DEFER, I'm not sure")], "end_turn")]))
    verdict = asyncio.run(AgentGuardian(parent).review(
        tool="bash", rule="destructive", message="confirm",
        input_preview="{}", session_id="s1"))
    assert verdict is None


def test_the_review_agent_cannot_write(tmp_path):
    """The reviewer is a zero-write role: it cannot approve its own tracks."""
    hostile = scripted([
        ([text("reviewing"),
          __import__("mini_loop.fake_llm", fromlist=["tool"]).tool(
              "write_file", _id="w", path="planted.txt", content="x")], "tool_use"),
        ([text("ALLOW")], "end_turn"),
    ])
    parent = _parent(tmp_path, hostile)
    asyncio.run(AgentGuardian(parent).review(
        tool="bash", rule="x", message="y", input_preview="{}", session_id="s1"))
    assert not (parent.workspace / "planted.txt").exists()


def test_the_adapter_bridges_to_the_broker_hook(tmp_path):
    from mini_loop.approvals import ApprovalBroker
    from dataclasses import dataclass

    @dataclass
    class _Rule:
        name: str
        message: str

    @dataclass
    class _Call:
        name: str
        input: dict
        id: str

    class _Ctx:
        def __init__(self, agent):
            self.agent = agent
            self.events = []

        async def emit_event(self, *args, **fields):
            self.events.append(args[0] if args else fields.get("type"))

    parent = _parent(tmp_path, scripted([([text("DENY dangerous")], "end_turn")]))
    broker = ApprovalBroker(timeout=0.1)
    broker.reviewer = broker_reviewer(AgentGuardian(parent))
    ctx = _Ctx(parent)
    decision = asyncio.run(broker.ask(
        ctx, _Call("bash", {"command": "rm -rf x"}, "c1"),
        _Rule("destructive", "confirm")))
    assert decision is False
    assert "approval_auto_reviewed" in ctx.events
