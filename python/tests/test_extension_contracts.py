"""An extension point that corrupts shared state on a wrong return.

Injectors are a documented seam: a callable that runs before each turn and may
add messages. The loop did `self.messages.extend(await inject(self))`, and
`extend` on a *string* appends its characters:

    transcript is now 14 entries:
      [{'role': 'user', 'content': 'real message'}, 'o', 'o', 'p', 's', ' ', ...]

The conversation is destroyed in place. Nothing notices at the seam; the first
symptom is `AttributeError: 'str' object has no attribute 'get'` raised from
`tool_result_budget` -- inside the compactor, a module with nothing to do with
injectors, several frames and one subsystem away from the mistake.

Returning a bare string is the obvious mistake to make here, and it is made by
someone outside this file. So the seam checks its own contract and names who
broke it. Loud rather than degrading: an extension returning the wrong type is a
bug, not a runtime condition, and the alternative is corrupt shared state.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.agent import _injected_messages
from mini_loop.fake_llm import FakeAsyncAnthropic

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _agent(tmp_path, **kwargs):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
        **kwargs,
    ).create().agent


def _run(agent):
    return asyncio.run(agent.run("hi"))


# --- the contract, checked at the seam ------------------------------------

def test_a_string_return_is_refused_by_name(tmp_path):
    async def note_injector(agent):
        return "just a note"

    agent = _agent(tmp_path, injectors=[note_injector])
    with pytest.raises(TypeError) as caught:
        _run(agent)
    message = str(caught.value)
    assert "note_injector" in message, "the failing extension must be named"
    assert "str" in message
    assert "character" in message, "say what would have happened"


def test_the_transcript_is_not_corrupted_by_a_bad_injector(tmp_path):
    """The point of checking at the seam rather than downstream."""

    async def note_injector(agent):
        return "just a note"

    agent = _agent(tmp_path, injectors=[note_injector])
    with pytest.raises(TypeError):
        _run(agent)
    assert all(isinstance(message, dict) for message in agent.messages), (
        f"characters leaked into the transcript: {agent.messages}"
    )


def test_a_list_of_non_messages_is_refused_with_its_index(tmp_path):
    async def sloppy(agent):
        return [{"role": "user", "content": "fine"}, "oops"]

    agent = _agent(tmp_path, injectors=[sloppy])
    with pytest.raises(TypeError, match="index 1"):
        _run(agent)


def test_a_dict_without_a_role_is_refused(tmp_path):
    async def roleless(agent):
        return [{"content": "no role"}]

    agent = _agent(tmp_path, injectors=[roleless])
    with pytest.raises(TypeError, match="role"):
        _run(agent)


# --- and the shapes that must keep working --------------------------------

def test_a_correct_injector_is_untouched(tmp_path):
    async def good(agent):
        return [{"role": "user", "content": "RUNTIME-FACT"}]

    agent = _agent(tmp_path, injectors=[good])
    _run(agent)
    assert any(
        message.get("content") == "RUNTIME-FACT" for message in agent.messages
    )


@pytest.mark.parametrize("empty", [None, [], ()])
def test_injecting_nothing_is_allowed(tmp_path, empty):
    async def quiet(agent):
        return empty

    _run(_agent(tmp_path, injectors=[quiet]))


def test_the_shipped_injector_satisfies_its_own_contract(tmp_path):
    """`runtime_facts_injector` is the reference implementation of this seam."""
    from mini_loop.caching import runtime_facts_injector

    agent = _agent(tmp_path)
    agent.todo.update(
        [{"content": "a task", "status": "pending", "activeForm": "doing a task"}]
    )
    produced = asyncio.run(runtime_facts_injector(agent))
    assert _injected_messages(produced, runtime_facts_injector) == list(produced)


def test_the_validator_names_a_callable_object_too():
    class Injector:
        async def __call__(self, agent):
            return "bad"

    with pytest.raises(TypeError, match="Injector"):
        _injected_messages("bad", Injector())


# --- the seam that turned out to be fine ----------------------------------

def test_a_subagent_inherits_a_custom_system_builder(tmp_path):
    """Checked because the subagent re-lists nine seams and omits this one.

    It is carried anyway: `derive()` starts from the parent's harness, and a
    builder configured on the manager is in it. Pinned so the omission stays
    harmless rather than becoming true later.
    """
    from mini_loop.prompts import default_system_builder

    POLICY = "POLICY: never write outside /tmp."

    def restricted(agent):
        return default_system_builder(agent) + "\n\n" + POLICY

    parent = _agent(tmp_path, system_builder=restricted)
    captured = {}
    import mini_loop.agent as module

    real = module.Agent

    class Spy(real):
        def __init__(self, **kwargs):
            super().__init__(**kwargs)
            captured["child"] = self

    module.Agent = Spy
    try:
        asyncio.run(parent._run_subagent("look around", "Explore"))
    finally:
        module.Agent = real

    child = captured["child"]
    assert POLICY in child.system_builder(child)
    for seam in ("secrets", "sandbox", "stuck_detector", "cache_policy", "compactor"):
        assert getattr(child, seam) is getattr(parent, seam), f"{seam} not inherited"


def _spy_subagent(tmp_path, agent_type):
    parent = _agent(tmp_path)
    captured = {}
    import mini_loop.agent as module

    real = module.Agent

    class Spy(real):
        def __init__(self, **kwargs):
            super().__init__(**kwargs)
            captured["child"] = self

    module.Agent = Spy
    try:
        asyncio.run(parent._run_subagent("go", agent_type))
    finally:
        module.Agent = real
    return captured["child"]


def test_an_explore_subagent_is_read_only(tmp_path):
    """`task` promises the model "Explore is read-only". That was a tool-list
    convention only: bash in the default interactive mode runs a plain
    `echo x > file` with no approval (only *destructive* shell asks), so an
    Explore subagent could mutate a workspace delegated as read-only. It runs in
    read-only permission mode now -- which denies bash -- and its registry offers
    no bash to begin with."""
    child = _spy_subagent(tmp_path, "Explore")
    assert child.state.get("permission_mode") == "readonly"
    assert "bash" not in child.tools.names()
    assert set(child.tools.names()) <= {"read_file", "glob"}


def test_a_general_purpose_subagent_keeps_write_tools(tmp_path):
    """The other half: 'general-purpose may also edit files', so it keeps write
    tools and the interactive default -- read-only mode is Explore's alone."""
    child = _spy_subagent(tmp_path, "general-purpose")
    assert child.state.get("permission_mode") != "readonly"
    assert "write_file" in child.tools.names()


def test_an_explore_subagent_cannot_write_the_workspace(tmp_path):
    """End to end: the Explore subagent's bash write is denied, so a workspace
    delegated as read-only stays untouched."""
    from mini_loop.agent import Agent
    from mini_loop.builtins import full_registry
    from mini_loop.fake_llm import scripted, text, tool
    from mini_loop.permissions import default_hooks
    from mini_loop.secrets import NullSecretRegistry

    ws = tmp_path / "ws"
    ws.mkdir()
    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("task", prompt="explore", agent_type="Explore", _id="t1")], "tool_use"),
        ([tool("bash", command="echo PWNED > stolen.txt", _id="b1")], "tool_use"),
        ([text("sub done")], "end_turn"),
        ([text("parent done")], "end_turn"),
    ]))
    agent = Agent(
        client=client,
        settings=Settings(fake_llm=True, workspace_root=ws, skills_dir=SKILLS),
        workspace=ws,
        tools=full_registry(),
        hooks=default_hooks(),
        secrets=NullSecretRegistry(),
    )
    asyncio.run(agent.run("delegate an explore"))
    assert not (ws / "stolen.txt").exists(), "the read-only Explore subagent wrote a file"
