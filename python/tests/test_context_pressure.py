"""The agent's own output gets rewritten under it, and nothing said so.

At 89% of its token budget an agent has a `compress` tool, no idea it is at 89%,
and compaction about to happen *to* it: `microcompact` blanks older tool results
to `[cleared]`, `snip_compact` replaces the middle of the conversation with a
marker. Those artifacts appear with no explanation.

This is round 63's rule one level up. There, output cut at the cap had to say
so; here, output cleared from *history* has to say so.

**The behavioural claim did not survive measurement, and the change was narrowed
because of it.** The first version added "prefer summarising over pasting, read
files in slices rather than whole". Against the real endpoint on a task inviting
a large read, the told agent used six bash calls to an untold agent's three, and
neither dumped the file: the model already sliced sensibly, and being told to
slice made it slice more. Across four runs the counts were 3, 3 (untold) and 6,
4 (told) -- too noisy to claim an effect in either direction, and certainly not
the one intended.

What is left is the part the agent cannot know and that is being done to it. No
advice about how to work.
"""

import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.compaction import context_used
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.prompts import CONTEXT_BANDS, default_system_builder, runtime_facts

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _agent(tmp_path, threshold=100_000):
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    object.__setattr__(settings, "token_threshold", threshold)
    return SessionManager(settings, FakeAsyncAnthropic()).create().agent


def _fill(agent, pairs):
    for index in range(pairs):
        agent.messages.append({"role": "assistant", "content": [
            {"type": "tool_use", "id": f"t{index}", "name": "b", "input": {}}]})
        agent.messages.append({"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": f"t{index}",
             "content": "X" * 2000}]})


def _pressure(agent):
    return [line for line in (runtime_facts(agent) or "").splitlines()
            if line.startswith("Context is")]


# --- reported only when it is actionable ---------------------------------

def test_an_empty_context_says_nothing(tmp_path):
    """A line that is always there is a line that stops being read."""
    assert _pressure(_agent(tmp_path)) == []


def test_a_full_context_says_so(tmp_path):
    agent = _agent(tmp_path)
    _fill(agent, 200)
    assert context_used(agent) > agent.settings.token_threshold * 0.9
    assert _pressure(agent), "the agent is over 90% full and is not told"


def test_the_bands_are_crossed_in_order(tmp_path):
    agent = _agent(tmp_path)
    seen = []
    for _ in range(200):
        _fill(agent, 1)
        line = (_pressure(agent) or [""])[0]
        if line not in seen:
            seen.append(line)
    labels = [label for _, label in CONTEXT_BANDS]
    assert any(labels[1] in line for line in seen), "the lower band never fired"
    assert any(labels[0] in line for line in seen), "the upper band never fired"


# --- bucketed, because the injector re-sends on change -------------------

def test_the_value_holds_still_across_turns(tmp_path):
    """An exact percentage would inject a message every turn -- the churn the
    whole runtime-facts design exists to avoid."""
    agent = _agent(tmp_path)
    values = set()
    for _ in range(200):
        _fill(agent, 1)
        values.add((_pressure(agent) or [""])[0])
    assert len(values) <= len(CONTEXT_BANDS) + 1, (
        f"{len(values)} distinct values over 200 turns; a live percentage would "
        "give one per turn"
    )


def test_no_percentage_appears_in_the_text(tmp_path):
    agent = _agent(tmp_path)
    _fill(agent, 200)
    line = _pressure(agent)[0]
    assert "%" in line, "the band label should still be legible"
    assert not any(char.isdigit() and char not in "0759" for char in line[:40])


# --- placement, per the rule established in rounds 8, 61 and 62 ----------

def test_pressure_is_a_runtime_fact_not_a_prompt_line(tmp_path):
    """It changes, so it must not sit in the cached prefix -- the opposite of
    the confinement line in round 62, and for the same reason."""
    agent = _agent(tmp_path)
    _fill(agent, 200)
    assert "Context is" not in default_system_builder(agent)
    assert _pressure(agent)


# --- what measurement removed --------------------------------------------

def test_it_states_a_fact_and_does_not_prescribe_a_workflow(tmp_path):
    """A regression guard on a claim that was tested and refuted.

    Telling the model how to work produced more calls, not fewer. What it cannot
    know is that its own history is being rewritten; that is what it is told.
    """
    agent = _agent(tmp_path)
    _fill(agent, 200)
    line = _pressure(agent)[0].lower()

    assert "cleared automatically" in line, "the thing it cannot otherwise know"
    for advice in ("prefer", "rather than whole", "in slices", "you should"):
        assert advice not in line, f"workflow advice crept back in: {advice!r}"


def test_a_zero_threshold_disables_it(tmp_path):
    """`token_threshold=0` means no budget is being enforced, so there is no
    pressure to report."""
    agent = _agent(tmp_path, threshold=0)
    _fill(agent, 50)
    assert _pressure(agent) == []
