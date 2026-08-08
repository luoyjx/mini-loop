"""The agent was shown a catalogue it had no tool to open.

Rounds 43 to 60 changed the double's validation, MCP, background, memory, cron,
teams, the transport and the event path -- all verified against a stand-in that
has diverged from the real provider six times. The last end-to-end run against
the real endpoint was round 41, so this round did one with everything switched
on: SQLite store, secret registry, Seatbelt sandbox, streaming transport.

Almost all of it was a clean negative result. Two real turns with tool use:
`notes.txt` written inside the sandboxed workspace, 18 messages in memory and 18
on disk, 41 events persisted and **zero** delta rows, meter calibrated, no
persist errors, and no high or critical audit findings.

The defect was in what the model *said*:

    "I don't actually have a dedicated memory tool in my available toolset"

Setting `memory_root` builds a `MemoryStore` and puts its index into runtime
facts every turn -- without registering `remember` or `recall`, which live in
`full_registry`. So the agent carried a list of memories it could not open, paid
for it on every request, and had to tell the user it has no memory tool while
apparently knowing what it had remembered.

The class is round 26's: presenting something the agent cannot actually do. What
is new is that no assertion in 852 tests could have caught it -- it took reading
a sentence a real model wrote.
"""

import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import default_registry, full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.memory import memory_store_for
from mini_loop.prompts import runtime_facts

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _agent(tmp_path, registry=None):
    kwargs = {"tool_registry": registry} if registry is not None else {}
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS,
                 memory_root=tmp_path / "mem"),
        FakeAsyncAnthropic(),
        **kwargs,
    ).create().agent


def _with_a_memory(agent):
    memory_store_for(agent).write("alpha", "project", "a remembered fact", "body")
    return agent


def test_no_index_without_the_tool_to_read_it(tmp_path):
    agent = _with_a_memory(_agent(tmp_path, default_registry()))
    assert "recall" not in agent.tools
    assert "a remembered fact" not in (runtime_facts(agent) or "")


def test_the_index_appears_when_the_tools_do(tmp_path):
    """The fix must not disable memory wherever it is genuinely available."""
    agent = _with_a_memory(_agent(tmp_path, full_registry()))
    assert "recall" in agent.tools
    assert "a remembered fact" in (runtime_facts(agent) or "")


@pytest.mark.parametrize("registry", ["default", "full"])
def test_the_index_and_the_tool_agree(tmp_path, registry):
    """Stated as the invariant rather than as two separate cases, because the
    defect was precisely the two disagreeing."""
    agent = _with_a_memory(
        _agent(tmp_path, full_registry() if registry == "full" else default_registry())
    )
    has_tool = "recall" in agent.tools
    injected = "a remembered fact" in (runtime_facts(agent) or "")
    assert has_tool == injected


def test_a_memory_root_alone_costs_nothing_per_turn(tmp_path):
    """It was not only confusing -- the index is sent on every request."""
    agent = _agent(tmp_path, default_registry())
    store = memory_store_for(agent)
    for index in range(50):
        store.write(f"m{index}", "project", "D" * 200, "body")
    assert runtime_facts(agent) == ""


def test_the_todo_board_still_rides_along(tmp_path):
    """Runtime facts carry more than memory; the guard must be narrow."""
    agent = _agent(tmp_path, default_registry())
    agent.todo.update([{"content": "a task", "status": "pending",
                        "activeForm": "doing a task"}])
    assert "a task" in (runtime_facts(agent) or "")
