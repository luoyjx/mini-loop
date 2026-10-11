"""Every event names its turn (roadmap G10).

The action journal records which turn a tool ran in (session_id + message_id
+ action_id), but the event stream did not: model_end usage, assistant_text,
compact and steering events had no turn identifier at all, so "why was this
turn slow, why was it expensive" could only be answered by ordering
heuristics over the stream -- which interleave under subagents and break
across restores. Pinned here:

* every event emitted inside a run carries that run's `message_id`;
* a subagent's events carry the child's own id plus `parent_message_id`,
  so the delegation tree is explicit in the data, not inferred from
  event ordering;
* events emitted outside any run carry no stamp -- a lie about provenance
  would be worse than silence.
"""

import asyncio
import pathlib

from mini_loop.agent import Agent
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, tool
from mini_loop.run_context import RunContext
from mini_loop.skills import SkillLoader

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


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


def test_every_event_in_a_turn_carries_the_turn_message_id(tmp_path):
    client = FakeAsyncAnthropic(
        responder=scripted([
            ([tool("bash", _id="t1", command="echo hi")], "tool_use"),
            ([], "end_turn"),
        ])
    )
    agent, events = _agent(tmp_path, client)
    context = RunContext.default()
    asyncio.run(agent.run("go", run_context=context))

    in_turn = [e for e in events if e["type"] != "turn_queued"]
    assert in_turn, "the run emitted nothing"
    for event in in_turn:
        assert event.get("message_id") == context.message_id, event["type"]
    # The correlation reaches the expensive events specifically: the ones
    # cost and latency questions group by.
    kinds = {e["type"] for e in in_turn}
    assert "model_end" in kinds and "tool_result" in kinds


def test_subagent_events_link_back_to_the_delegating_turn(tmp_path):
    client = FakeAsyncAnthropic(
        responder=scripted([
            ([tool("task", _id="t1", prompt="look", agent_type="Explore")],
             "tool_use"),
            ([], "end_turn"),  # the child's turn
            ([], "end_turn"),  # the parent continues
        ])
    )
    agent, events = _agent(tmp_path, client)
    context = RunContext.default()
    asyncio.run(agent.run("go", run_context=context))

    child_events = [e for e in events if e["depth"] == 1]
    assert child_events, "the subagent emitted nothing"
    child_ids = {e.get("message_id") for e in child_events}
    assert len(child_ids) == 1
    (child_id,) = child_ids
    assert child_id != context.message_id
    for event in child_events:
        assert event.get("parent_message_id") == context.message_id
    # The parent's telemetry around the delegation stays the parent's.
    start = next(e for e in events if e["type"] == "subagent_start")
    assert start["message_id"] == context.message_id


def test_events_outside_a_run_carry_no_stamp(tmp_path):
    client = FakeAsyncAnthropic(responder=scripted([([], "end_turn")]))
    agent, events = _agent(tmp_path, client)
    asyncio.run(agent._send("diagnostic", detail="not in a turn"))

    (event,) = events
    assert "message_id" not in event
    assert "parent_message_id" not in event
