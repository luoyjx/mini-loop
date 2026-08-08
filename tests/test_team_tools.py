"""The agent-facing team tools had no coverage at all.

Round 54 ended on a measurable fact: each of the last four rounds found its
defect in the previous round's fix. Mutation testing answers "is this guard
load-bearing"; it cannot answer "is this code executed by anything", so this
round measured coverage instead. The package sits at 90%; `teams.py` was 65%,
and the missing block was every tool the model actually calls -- spawn, send,
broadcast, shutdown, plan, review. Round 50 had tested the `MessageBus`
underneath them and nothing above it.

The gap contained exactly the shape those four rounds keep producing.
`broadcast` loops over teammates calling `bus.send` and **discarded the return
value**. That was harmless until round 50 gave `send` a size limit and made it
report refusals by returning a string -- after which an oversized broadcast
answered:

    [broadcast] returned : 'Broadcast to 3 teammate(s)'
    [broadcast] delivered: 0 messages

The lead is told it reached three teammates, none received anything, and it
carries on believing it has coordinated with its team. A fix in one layer turned
a discarded return value in another into a lie.
"""

import asyncio
import pathlib
import tempfile

import pytest

from mini_loop.registry import ToolRegistry
from mini_loop.secrets import SecretRegistry
from mini_loop.teams import MessageBus, install_teams

SECRET = "sk-TEAM-TOOL-0123456789abcdef"


class FakeManager:
    def __init__(self, teammates=("alice", "bob", "carol")):
        self.teammates = list(teammates)
        self.shutdowns: list[tuple] = []

    def teammates_of(self, team_id):
        return self.teammates

    def request_shutdown(self, team_id, target, reason):
        self.shutdowns.append((team_id, target, reason))
        return f"Shutdown requested for {target}"


class Context:
    def __init__(self, state):
        self.state = state


@pytest.fixture
def team(tmp_path):
    bus = MessageBus(tmp_path / "teams", secrets=SecretRegistry.from_environ(
        environ={"P_API_KEY": SECRET}))
    manager = FakeManager()
    registry = install_teams(ToolRegistry())
    context = Context({"bus": bus, "manager": manager, "team_id": "t",
                       "agent_name": "lead"})
    return registry, context, bus, manager


def _call(registry, name, context, **kwargs):
    return asyncio.run(registry.get(name).handler(context, **kwargs))


def _delivered(bus, manager):
    return sum(len(bus.read(f"t/{name}")) for name in manager.teammates)


# --- the defect -----------------------------------------------------------

def test_a_refused_broadcast_is_not_reported_as_delivered(team):
    registry, context, bus, manager = team
    result = _call(registry, "broadcast", context, content="X" * 2_000_000)

    assert "refused" in result, result
    assert "Broadcast to 0" in result
    assert _delivered(bus, manager) == 0


def test_a_normal_broadcast_still_reports_plainly(team):
    registry, context, bus, manager = team
    result = _call(registry, "broadcast", context, content="team, status please")

    assert result == "Broadcast to 3 teammate(s)"
    assert "refused" not in result
    assert _delivered(bus, manager) == 3


def test_a_partial_broadcast_reports_both_halves(team):
    """One mailbox refusing must not hide the ones that worked, or the other
    way round."""
    registry, context, bus, manager = team
    manager.teammates = ["alice", "../../escape", "carol"]
    result = _call(registry, "broadcast", context, content="status")

    assert "Broadcast to 2" in result and "1 refused" in result


def test_a_broadcast_does_not_send_to_the_sender(team):
    registry, context, bus, manager = team
    manager.teammates = ["lead", "alice"]
    _call(registry, "broadcast", context, content="hello")
    assert bus.read("t/lead") == []
    assert len(bus.read("t/alice")) == 1


# --- the rest of the surface the coverage gap pointed at ------------------

def test_sending_goes_through_the_bus_limits(team):
    registry, context, bus, manager = team
    assert _call(
        registry, "send_message", context, to="alice", content="X" * 2_000_000
    ).startswith("Error:")
    assert bus.read("t/alice") == []


def test_a_sent_message_is_masked_on_disk(team, tmp_path):
    registry, context, bus, manager = team
    _call(registry, "send_message", context, to="alice",
          content=f"the key is {SECRET}")
    written = [p for p in (tmp_path / "teams").rglob("*") if p.is_file()]
    assert written
    assert not [p for p in written if SECRET.encode() in p.read_bytes()]


def test_reading_an_empty_inbox_says_so(team):
    registry, context, bus, manager = team
    context.state.pop("manager")
    assert _call(registry, "read_inbox", context) == "(empty inbox)"


def test_reading_returns_what_was_sent(team):
    registry, context, bus, manager = team
    context.state.pop("manager")
    bus.send("alice", "t/lead", "here is my status")
    assert "here is my status" in _call(registry, "read_inbox", context)


def test_only_the_lead_may_request_a_shutdown(team):
    """An access-control rule with no coverage until now."""
    registry, context, bus, manager = team

    context.state["agent_name"] = "alice"
    refused = _call(registry, "request_shutdown", context, target="bob")
    assert refused.startswith("Error:") and "lead" in refused
    assert manager.shutdowns == []

    context.state["agent_name"] = "lead"
    allowed = _call(registry, "request_shutdown", context, target="bob")
    assert not allowed.startswith("Error:")
    assert manager.shutdowns == [("t", "bob", "")]


def test_listing_teammates(team):
    registry, context, bus, manager = team
    listed = _call(registry, "list_teammates", context)
    assert all(name in listed for name in manager.teammates)

    manager.teammates = []
    assert _call(registry, "list_teammates", context) == "No teammates."


@pytest.mark.parametrize(
    "tool,kwargs",
    [
        ("send_message", {"to": "alice", "content": "hi"}),
        ("broadcast", {"content": "hi"}),
        ("read_inbox", {}),
    ],
)
def test_every_tool_degrades_when_the_bus_is_absent(team, tool, kwargs):
    """These run in agents that were built without teams enabled."""
    registry, context, bus, manager = team
    context.state["bus"] = None
    assert _call(registry, tool, context, **kwargs).startswith("Error:")


@pytest.mark.parametrize(
    "tool,kwargs",
    [
        ("list_teammates", {}),
        ("request_shutdown", {"target": "bob"}),
        ("broadcast", {"content": "hi"}),
    ],
)
def test_every_tool_degrades_when_the_manager_is_absent(team, tool, kwargs):
    registry, context, bus, manager = team
    context.state["manager"] = None
    assert _call(registry, tool, context, **kwargs).startswith("Error:")


# --- protocol retention: the manager-level handshake store must not grow ----

def _real_manager(tmp_path):
    from mini_loop import SessionManager, Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic

    settings = Settings(
        fake_llm=True, workspace_root=tmp_path / "ws",
        skills_dir=pathlib.Path(__file__).resolve().parent.parent / "skills",
    )
    return SessionManager(settings, FakeAsyncAnthropic())


def test_resolved_protocol_handshakes_do_not_accumulate(tmp_path, monkeypatch):
    """`self.protocols` was only ever added to -- every `submit_plan` /
    `request_shutdown` left a `ProtocolState` (with its plan payload), and
    nothing, not even `delete()`, removed a resolved one, though the model still
    re-reads them all through `list_protocols`. It is bounded now: resolved
    handshakes are evicted oldest-first while every live pending one is kept.
    """
    import mini_loop.manager as manager_module

    monkeypatch.setattr(manager_module, "MAX_PROTOCOLS", 50)
    manager = _real_manager(tmp_path)

    live = [
        manager._new_protocol("plan_approval", "t", f"p{i}", "lead", "x").request_id
        for i in range(5)
    ]
    for i in range(500):
        state = manager._new_protocol("plan_approval", "t", f"r{i}", "lead", "step " * 200)
        state.status = "approved"

    assert len(manager.protocols) == 50, "resolved handshakes accumulated past the cap"
    assert all(request_id in manager.protocols for request_id in live), (
        "a live pending handshake was evicted while resolved history remained"
    )


def test_all_pending_protocols_are_still_bounded(tmp_path, monkeypatch):
    """The safety valve: if the cap is reached by pending handshakes alone -- a
    requester that vanished mid-handshake, e.g. a deleted session -- the oldest
    (most likely stale) give way rather than growing without bound."""
    import mini_loop.manager as manager_module

    monkeypatch.setattr(manager_module, "MAX_PROTOCOLS", 10)
    manager = _real_manager(tmp_path)

    ids = [
        manager._new_protocol("plan_approval", "t", f"p{i}", "lead", "x").request_id
        for i in range(30)
    ]
    assert len(manager.protocols) == 10
    assert list(manager.protocols) == ids[-10:], "did not keep the newest pending"
