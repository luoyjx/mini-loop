"""A run that exhausts its rounds must say so before showing partial text.

dsh's subagent-output note (2026-08-10): three consumers each selected a
child's output independently, and a max-tokens child whose terminal message
was empty reported nothing -- the fix was one canonical selection rule owned
at the source, and for a non-completed run, "the message appends the child's
partial text after the stop-reason headline so the parent model receives
both the failure and available output."

mini-loop's mirror image, measured before the fix: `_loop` round exhaustion
kept `last_text` verbatim, so a child that spent all its rounds on tools
returned its last mid-run commentary line ("I'll check the workspace
first.") as the delegation's *completed summary*. The `error` event fired,
but the tool result the parent model actually reads carried no signal --
and the same line serves the main agent, so an HTTP caller behind
`session.run` got the same silent truncation at max_turns.

Fixed at the source rather than per consumer (the dsh lesson): the stop
headline comes first, the partial text after it, for every reader of
`Agent.run()` at once. Writing the subagent probe surfaced a second site of
the same class by accident: the stuck-detector *halt* used the identical
`last_text or marker` fallback, so a halted run with any commentary also
read as a completed one. Both paths now share `_mark_stopped` -- one rule,
one owner.
"""

import asyncio
import pathlib

from mini_loop.agent import Agent
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.skills import SkillLoader

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _agent(tmp_path, client, **over):
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS, spill_dir=None)
    workspace = tmp_path / "ws" / "a"
    workspace.mkdir(parents=True)
    return Agent(client=client, settings=settings, workspace=workspace,
                 skills=SkillLoader(SKILLS), **over)


def test_an_exhausted_run_reports_the_stop_before_the_partial_text(tmp_path):
    def talk_then_tool(kwargs):
        return [text("I'll check the workspace first."),
                tool("bash", _id="t", command="echo loop")], "tool_use"

    agent = _agent(tmp_path, FakeAsyncAnthropic(responder=talk_then_tool),
                   max_rounds=2)
    final = asyncio.run(agent.run("go"))

    assert final.startswith("[stopped after 2 rounds"), (
        "the stop must be the headline, not an afterthought"
    )
    # The partial answer survives, after the headline -- not instead of it.
    assert "I'll check the workspace first." in final


def test_a_silent_exhausted_run_still_reports_the_stop(tmp_path):
    def tools_only(kwargs):
        return [tool("bash", _id="t", command="echo loop")], "tool_use"

    agent = _agent(tmp_path, FakeAsyncAnthropic(responder=tools_only),
                   max_rounds=2)
    final = asyncio.run(agent.run("go"))
    assert final.startswith("[stopped after 2 rounds")
    assert "Partial output" not in final  # nothing to show, nothing invented


def test_a_finished_run_is_untouched(tmp_path):
    responder = scripted([
        ([text("working"), tool("bash", _id="t", command="echo hi")], "tool_use"),
        ([text("all done")], "end_turn"),
    ])
    agent = _agent(tmp_path, FakeAsyncAnthropic(responder=responder),
                   max_rounds=5)
    final = asyncio.run(agent.run("go"))
    assert final == "all done"


def test_an_exhausted_subagent_is_not_a_clean_summary(tmp_path):
    """The parent's tool result carries the stop, not just the commentary."""

    def child_never_stops(kwargs):
        prompt = str(kwargs["messages"][0].get("content", ""))
        if "delegate" in prompt:
            return [text("Progress note from the child."),
                    tool("bash", _id="c", command="echo child")], "tool_use"
        return [text("parent done")], "end_turn"

    agent = _agent(tmp_path, FakeAsyncAnthropic(responder=child_never_stops))
    summary = asyncio.run(agent._run_subagent("delegate this", "worker"))
    # Whichever early stop fires first (here the stuck detector halts the
    # repeating child before its round budget), the headline leads.
    assert summary.startswith("[stopped"), (
        "a cut-off child must not read as a completed delegation"
    )
    assert "Progress note from the child." in summary
