"""Auto-review substitutes the approver; it never escalates (round 215).

Codex's Guardian shape: an optional reviewer decides the same allow/deny a
human would, before the request is parked. Its bounds, taken from the
research doc: it does not widen writable roots, relax the sandbox, or
change the permission mode -- it answers THIS action only. A reviewer that
abstains or raises falls through to the human, never to silent approval.
"""

import asyncio

from dataclasses import dataclass

from mini_loop.approvals import ApprovalBroker


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
    def __init__(self, session_id="s1"):
        self.events = []

        class _Agent:
            secrets = None
            state = {"session": type("S", (), {"id": session_id})()}

        self.agent = _Agent()

    async def emit_event(self, *args, **fields):
        kind = args[0] if args else fields.get('type')
        self.events.append((kind, fields))


_RULE = _Rule(name="destructive", message="confirm deletion")
_CALL = _Call("bash", {"command": "rm -rf build"}, "c1")


def _ask(reviewer, timeout=0.2):
    broker = ApprovalBroker(timeout=timeout)
    broker.reviewer = reviewer
    return broker, asyncio.run(broker.ask(_Ctx(), _CALL, _RULE))


def test_a_reviewer_allow_substitutes_for_the_human():
    async def approve(ctx, call, rule):
        return True

    broker, decision = _ask(approve)
    assert decision is True


def test_a_reviewer_deny_substitutes_for_the_human():
    async def refuse(ctx, call, rule):
        return False

    broker, decision = _ask(refuse)
    assert decision is False


def test_an_abstaining_reviewer_falls_through_to_the_human_and_times_out():
    async def abstain(ctx, call, rule):
        return None

    # No human answers within the timeout -> the safe default (deny), which
    # is the human path, not an auto-decision.
    broker, decision = _ask(abstain)
    assert decision is False
    # No auto-review event: the reviewer did not decide.
    ctx = _Ctx()
    broker2 = ApprovalBroker(timeout=0.1)
    broker2.reviewer = abstain
    asyncio.run(broker2.ask(ctx, _CALL, _RULE))
    assert not any(k == "approval_auto_reviewed" for k, _ in ctx.events)
    assert any(k == "approval_required" for k, _ in ctx.events), (
        "an abstention must still reach the human path"
    )


def test_a_raising_reviewer_is_contained_and_recorded():
    async def boom(ctx, call, rule):
        raise RuntimeError("reviewer model unreachable")

    broker, decision = _ask(boom)
    assert decision is False  # fell through to the human, timed out
    assert any("auto-reviewer raised" in p for p in broker.problems)


def test_no_reviewer_is_the_unchanged_human_path():
    broker = ApprovalBroker(timeout=0.1)
    ctx = _Ctx()
    decision = asyncio.run(broker.ask(ctx, _CALL, _RULE))
    assert decision is False
    assert any(k == "approval_required" for k, _ in ctx.events)
    assert not any(k == "approval_auto_reviewed" for k, _ in ctx.events)


def test_the_auto_decision_is_persisted_distinctly():
    rows = []

    class _Store:
        def write_approval(self, row):
            rows.append(row)

    async def approve(ctx, call, rule):
        return True

    broker = ApprovalBroker(timeout=0.1, store=_Store())
    broker.reviewer = approve
    asyncio.run(broker.ask(_Ctx(), _CALL, _RULE))
    assert any(r.get("status") == "auto_allowed" for r in rows), (
        "an auto-decision must be distinguishable in the audit trail"
    )
