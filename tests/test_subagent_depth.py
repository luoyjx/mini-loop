"""Delegation depth is a quota, not an accident (roadmap G8).

Depth was tracked (`depth=parent.depth + 1`, lineage, event fields) but never
enforced: nothing in the runtime refused a delegation at any depth. The only
thing preventing model-driven infinite recursion was that the `task` tool
declares no capabilities, so `with_capabilities` drops it from every child
catalogue -- a side effect of the role policy, never stated as the rule, and
void for programmatic callers (`_run_subagent`) and custom providers.

Pinned here:

* the quota refuses at the seam, before `subagent_start`, without ever
  constructing a child -- every provider is governed, not just the default;
* the refusal is a tool-visible string that falls toward doing less
  (the model is told to do the work directly), never an exception;
* the accidental first layer is now a declared contract: `task` must not
  appear in any role-policy child catalogue.
"""

import asyncio
import pathlib

from mini_loop.agent import Agent
from mini_loop.builtins import default_registry
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, tool
from mini_loop.harness import Harness
from mini_loop.skills import SkillLoader
from mini_loop.tool_policy import DEFAULT_ROLE_TOOL_POLICY

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


class SpyProvider:
    def __init__(self):
        self.calls = []

    async def run(self, parent, *, prompt, agent_type, run_context):
        self.calls.append((agent_type, prompt))
        return "spy summary"


def _agent(tmp_path, client, **over):
    ws = tmp_path / "ws" / "sess"
    ws.mkdir(parents=True, exist_ok=True)
    events = []

    async def emit(event):
        events.append(event)

    agent = Agent(
        client=client,
        settings=Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                          skills_dir=SKILLS, spill_dir=None),
        workspace=ws,
        skills=SkillLoader(SKILLS),
        emit=emit,
        **over,
    )
    return agent, events


def test_delegation_at_the_quota_is_refused_without_construction(tmp_path):
    """At depth == quota, no provider runs and no subagent_start fires."""

    provider = SpyProvider()
    client = FakeAsyncAnthropic(responder=scripted([([], "end_turn")]))
    agent, events = _agent(
        tmp_path, client, harness=Harness(subagents=provider), depth=2
    )
    summary = asyncio.run(agent._run_subagent("dig deeper", "worker"))

    assert provider.calls == []
    assert "delegation refused" in summary and "do the work directly" in summary
    kinds = [e["type"] for e in events]
    assert "subagent_refused" in kinds
    assert "subagent_start" not in kinds
    refused = next(e for e in events if e["type"] == "subagent_refused")
    assert refused["reason"] == "depth"
    assert refused["child_depth"] == 3 and refused["limit"] == 2


def test_delegation_below_the_quota_proceeds(tmp_path):
    provider = SpyProvider()
    client = FakeAsyncAnthropic(responder=scripted([([], "end_turn")]))
    agent, events = _agent(
        tmp_path, client, harness=Harness(subagents=provider), depth=1
    )
    summary = asyncio.run(agent._run_subagent("one level down", "worker"))

    assert provider.calls == [("worker", "one level down")]
    assert summary == "spy summary"
    kinds = [e["type"] for e in events]
    assert "subagent_start" in kinds and "subagent_end" in kinds
    assert "subagent_refused" not in kinds


def test_the_model_driven_path_sees_the_refusal_as_a_tool_result(tmp_path):
    """A `task` call on an at-quota agent returns the refusal to the model."""

    provider = SpyProvider()
    client = FakeAsyncAnthropic(
        responder=scripted([
            ([tool("task", _id="t1", prompt="recurse", agent_type="Explore")],
             "tool_use"),
            ([], "end_turn"),
        ])
    )
    agent, events = _agent(
        tmp_path, client, harness=Harness(subagents=provider), depth=2
    )
    asyncio.run(agent.run("go"))

    assert provider.calls == []
    results = [e for e in events if e["type"] == "tool_result"]
    assert any("delegation refused" in str(e.get("output", "")) for e in results)


def test_the_task_tool_never_reaches_a_child_catalogue():
    """The accidental barrier, declared: no role catalogue carries `task`.

    `task` registers with no capabilities, so `with_capabilities` drops it
    from every child. Before this test that was a side effect anyone could
    undo by tidily annotating the tool (round 104 shows fields do get
    aligned in cleanup passes); now it is a contract with a name.
    """

    parent = default_registry()
    assert "task" in parent.names()
    for role in ("explore", "worker", "general-purpose"):
        child = DEFAULT_ROLE_TOOL_POLICY.select(role, parent)
        assert "task" not in child.names(), role


def test_the_quota_rejects_a_nonpositive_setting(monkeypatch):
    monkeypatch.setenv("MINILOOP_SUBAGENT_MAX_DEPTH", "0")
    try:
        Settings(fake_llm=True)
    except ValueError as error:
        assert "subagent_max_depth" in str(error)
    else:
        raise AssertionError("subagent_max_depth=0 was accepted")
