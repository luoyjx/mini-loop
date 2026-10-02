"""The guardian is reachable, not just a library (round 221).

"Default off" must mean "opt-in", not "unreachable". A module that can
only be constructed in tests is dead code with tests, not an adopted
feature. This pins the one wiring line: MINILOOP_GUARDIAN / guardian_enabled
binds an AgentGuardian to the broker's reviewer hook per session, and off
by default leaves every approval on the human path.
"""

import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path, **over):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None, **over),
        FakeAsyncAnthropic(),
    )


def test_off_by_default_no_reviewer_is_bound(tmp_path):
    manager = _manager(tmp_path)
    assert manager.approvals.reviewer is None


def test_the_flag_binds_a_reviewer(tmp_path):
    manager = _manager(tmp_path, guardian_enabled=True)
    assert manager.approvals.reviewer is not None


def test_the_env_var_enables_it(tmp_path, monkeypatch):
    monkeypatch.setenv("MINILOOP_GUARDIAN", "1")
    assert Settings(workspace_root=tmp_path).guardian_enabled is True
    monkeypatch.setenv("MINILOOP_GUARDIAN", "0")
    assert Settings(workspace_root=tmp_path).guardian_enabled is False


def test_the_bound_reviewer_reaches_the_guardian(tmp_path):
    """End to end through the real broker: a wired guardian decides, and a
    ctx with no agent falls through to the human."""
    import asyncio
    from dataclasses import dataclass

    from mini_loop.fake_llm import scripted, text

    @dataclass
    class _Rule:
        name: str
        message: str

    @dataclass
    class _Call:
        name: str
        input: dict
        id: str

    manager = _manager(tmp_path, guardian_enabled=True)
    session = manager.create()
    # Script the review agent to DENY.
    session.agent.client.responder = scripted([([text("DENY unsafe")], "end_turn")])

    class _Ctx:
        def __init__(self, agent):
            self.agent = agent
            self.events = []

        async def emit_event(self, *a, **k):
            self.events.append(a[0] if a else k.get("type"))

    ctx = _Ctx(session.agent)
    decision = asyncio.run(manager.approvals.ask(
        ctx, _Call("bash", {"command": "rm -rf x"}, "c1"),
        _Rule("destructive", "confirm")))
    assert decision is False
    assert "approval_auto_reviewed" in ctx.events

    # No agent on the ctx -> no session to attach a question to, so the
    # broker's pre-existing "nobody to ask is deny" wins before the reviewer
    # is even consulted: deny, and no auto-review event.
    ctx2 = _Ctx(None)
    assert asyncio.run(manager.approvals.ask(
        ctx2, _Call("bash", {}, "c2"), _Rule("destructive", "confirm"))) is False
    assert "approval_auto_reviewed" not in ctx2.events


def test_a_wired_guardians_review_cannot_re_enter_the_reviewer(tmp_path):
    """The guardian does not recurse -- audited in round 223, and the
    audit found DEFENSE IN DEPTH: the review runs on a readonly role agent
    whose explore catalog contains no approval-triggering tool (first
    layer), AND readonly denies mutating tools outright without the broker
    (second layer). A guard mutation on either layer alone SURVIVED,
    because the other still protects the property -- which is the honest
    reason this is a documented test rather than a single-point guard.
    """
    import asyncio
    from dataclasses import dataclass

    from mini_loop.fake_llm import scripted, text, tool

    @dataclass
    class _Rule:
        name: str
        message: str

    @dataclass
    class _Call:
        name: str
        input: dict
        id: str

    calls = {"reviewer": 0}
    manager = _manager(tmp_path, guardian_enabled=True)
    session = manager.create()
    # The review agent tries a WRITE (a mutating action) before answering.
    # If readonly routed through the broker, this would re-enter the wired
    # reviewer; instead it is denied outright and the review still answers.
    session.agent.client.responder = scripted([
        ([text("let me modify something first"),
          tool("write_file", _id="w", path="x.txt", content="mutate")], "tool_use"),
        ([text("DENY after being unable to write")], "end_turn"),
    ])

    base_reviewer = manager.approvals.reviewer

    async def counting_reviewer(ctx, call, rule):
        calls["reviewer"] += 1
        return await base_reviewer(ctx, call, rule)

    manager.approvals.reviewer = counting_reviewer

    class _Ctx:
        def __init__(self, agent):
            self.agent = agent
            self.events = []

        async def emit_event(self, *a, **k):
            self.events.append(a[0] if a else k.get("type"))

    decision = asyncio.run(manager.approvals.ask(
        _Ctx(session.agent), _Call("bash", {"command": "rm -rf x"}, "c1"),
        _Rule("destructive", "confirm")))
    assert decision is False
    # The reviewer was entered exactly once: the review's own write attempt
    # was denied without a second approval, so no re-entry.
    assert calls["reviewer"] == 1, (
        "the guardian re-entered its own reviewer: readonly stopped denying "
        "outright and now routes mutations through the approval broker"
    )
    assert not (session.agent.workspace / "x.txt").exists()
