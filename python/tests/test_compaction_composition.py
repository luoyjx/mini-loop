"""Compaction rewrites the transcript, and everything built later watched it.

Compaction is the oldest subsystem here. Durable state, epochs, secret masking
and the rewrite detector were all added around it, and each was validated
against what compaction was *documented* to do rather than what it does. Two
things fell through:

* `microcompact` edited tool-result blocks **in place**. The session detects a
  rewrite by pointer comparison and its docstring said "nothing does that
  today", so compaction was never mirrored -- a session that compacted because
  it was near the context limit came back from a restart exactly as large as
  when it overflowed.
* Compaction spills context to **files in the workspace**, which the masking
  module did not count among its sinks. A credential kept out of the transcript
  was written to a path the agent can read, and which outlives the session.

Both guards below are written against the *family* rather than those two sites,
so a third strategy or a third sink is covered on arrival.
"""

import ast
import asyncio
import inspect
import pathlib
import tempfile
import textwrap

import pytest

from mini_loop import SessionManager, Settings
from mini_loop import compaction as compaction_module
from mini_loop.compaction import (
    Compactor,
    DefaultCompactor,
    InMemoryCompactor,
    microcompact,
    snip_compact,
    tool_result_budget,
)
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.secrets import SecretRegistry
from mini_loop.storage import SQLiteStateStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
SECRET = "sk-ant-NOTAREALKEY-0123456789abcdef"

#: Every compaction strategy the package ships. New ones join this list.
STRATEGIES = [DefaultCompactor, InMemoryCompactor]
#: Every transcript rewriter the package ships, each driven so it actually
#: fires -- a rewriter that no-ops proves nothing about mirroring.
REWRITERS = {
    "microcompact": lambda agent: microcompact(agent.messages),
    "snip_compact": lambda agent: snip_compact(agent.messages, max_messages=6),
    # `tool_result_budget` spills the newest oversized result to disk and
    # replaces it with a marker. It needs a workspace (for the spill file) and a
    # small budget to fire on the fixture's 500-char results -- and it was the
    # one shipped rewriter this roster used to omit, so its in-place block edit
    # went unmirrored until round 122.
    "tool_result_budget": lambda agent: tool_result_budget(
        agent.messages, agent.workspace, max_bytes=200, preview_chars=50
    ),
}


def _manager(tmp_path, **kwargs):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
        **kwargs,
    )


def _transcript(count=8, *, body="X" * 500):
    messages = []
    for index in range(count):
        messages.append({"role": "assistant", "content": [
            {"type": "tool_use", "id": f"t{index}", "name": "run_bash",
             "input": {"command": "echo hi"}}]})
        messages.append({"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": f"t{index}", "content": body}]})
    return messages


def _result_bodies(messages):
    return [
        part.get("content")
        for message in messages
        if isinstance(message.get("content"), list)
        for part in message["content"]
        if isinstance(part, dict) and part.get("type") == "tool_result"
    ]


# --- guard 1: a rewrite must reach the durable store -----------------------

@pytest.mark.parametrize("name", sorted(REWRITERS))
def test_every_rewriter_is_mirrored_to_the_store(tmp_path, name):
    """Whatever a rewriter does, the next flush must make disk match memory.

    Otherwise a restart silently un-does compaction.
    """
    store = SQLiteStateStore(tmp_path / "state.db")
    session = _manager(tmp_path, state_store=store).create()
    agent = session.agent
    agent.messages.extend(_transcript())
    session._flush_messages()

    assert REWRITERS[name](agent) > 0, f"{name} changed nothing to mirror"
    session._flush_messages()

    epoch = store.transcript_epoch(session.id)
    on_disk = store.load_messages(session.id, epoch=epoch)
    assert _result_bodies(on_disk) == _result_bodies(agent.messages)
    assert len(on_disk) == len(agent.messages)
    store.close()


def _rewriters_maybe_compact_runs():
    """Every function `maybe_compact` calls by name whose first parameter is
    `messages` -- i.e. every in-place transcript rewriter it drives.

    Derived from the source, not hand-listed: that is the whole point. The
    roster used to be a hand-kept dict, and a shipped rewriter
    (`tool_result_budget`) was left out of it, so its store-mirroring went
    untested and a restart un-did its compaction. This makes the omission
    impossible -- add a fourth cheap layer to `maybe_compact` and forget the
    roster, and this fails instead of shipping an unmirrored rewriter.
    """
    src = textwrap.dedent(inspect.getsource(DefaultCompactor.maybe_compact))
    called = {
        node.func.id
        for node in ast.walk(ast.parse(src))
        if isinstance(node, ast.Call) and isinstance(node.func, ast.Name)
    }
    rewriters = set()
    for name in called:
        fn = getattr(compaction_module, name, None)
        if not callable(fn):
            continue
        try:
            params = list(inspect.signature(fn).parameters)
        except (TypeError, ValueError):
            continue
        if params and params[0] == "messages":
            rewriters.add(name)
    return rewriters


def test_the_mirroring_roster_covers_every_rewriter_maybe_compact_runs():
    """The roster is complete against the code, not against memory."""
    invoked = _rewriters_maybe_compact_runs()
    assert invoked, "the source scan found no rewriters -- it broke, not the code"
    missing = invoked - set(REWRITERS)
    assert not missing, (
        f"maybe_compact runs {sorted(missing)} but the mirroring roster omits "
        "it, so its store-mirroring is untested -- exactly how tool_result_budget "
        "slipped through until round 122"
    )


def test_microcompact_replaces_messages_rather_than_mutating_them(tmp_path):
    """The specific contract the rewrite detector depends on.

    Pointer comparison is the detector; an edit that leaves the message object
    in place is invisible to it. This is the property, stated directly, so a
    future refactor back to in-place assignment fails here and not three
    subsystems away.
    """
    messages = _transcript()
    before = list(messages)
    assert microcompact(messages) > 0
    changed = [i for i, m in enumerate(messages) if m is not before[i]]
    assert changed, "microcompact left every message object identical"
    for index in changed:
        assert messages[index] != before[index]


def test_a_restart_gets_the_compacted_transcript(tmp_path):
    """The consequence, end to end -- the reason guard 1 exists."""
    store = SQLiteStateStore(tmp_path / "state.db")
    manager = _manager(tmp_path, state_store=store)
    session = manager.create()
    session.agent.messages.extend(_transcript())
    session._flush_messages()
    microcompact(session.agent.messages)
    session._flush_messages()

    live = len([b for b in _result_bodies(session.agent.messages)
                if isinstance(b, str) and len(b) > 100])
    restored = store.load_messages(session.id, epoch=store.transcript_epoch(session.id))
    survived = len([b for b in _result_bodies(restored)
                    if isinstance(b, str) and len(b) > 100])
    assert survived == live, (
        f"restart restores {survived} uncleared results but the live agent has {live}"
    )
    store.close()


# --- guard 2: nothing compaction writes to disk carries a secret -----------

@pytest.mark.parametrize("strategy", STRATEGIES, ids=lambda c: c.__name__)
def test_no_compaction_strategy_writes_a_secret_into_the_workspace(tmp_path, strategy):
    """Walks the whole workspace, so a *new* sink is covered on arrival.

    Asserting on the two known paths would have passed before this round: the
    transcript dump was the larger leak and nobody had listed it as a sink.
    """
    manager = _manager(
        tmp_path, secrets=SecretRegistry.from_environ(environ={"PROBE_API_KEY": SECRET})
    )
    agent = manager.create().agent
    agent.messages.extend(_transcript(count=2))
    agent.messages.append({"role": "assistant", "content": [
        {"type": "tool_use", "id": "big", "name": "run_bash",
         "input": {"command": f"echo {SECRET}"}}]})
    agent.messages.append({"role": "user", "content": [
        {"type": "tool_result", "tool_use_id": "big",
         "content": f"PROBE_API_KEY={SECRET}\n" + "Y" * 300_000}]})

    compactor = strategy()
    asyncio.run(compactor.maybe_compact(agent))
    asyncio.run(compactor.compact(agent))

    written = [p for p in pathlib.Path(agent.workspace).rglob("*") if p.is_file()]
    leaked = [p for p in written if SECRET in p.read_text(errors="ignore")]
    assert not leaked, (
        f"{strategy.__name__} wrote the raw secret to: "
        + ", ".join(str(p) for p in leaked)
    )


def test_the_strategy_roster_covers_every_compactor_the_module_ships():
    """The secret-leak sweep above is parametrized over `STRATEGIES`, a hand-kept
    list carrying the same "every strategy the package ships" promise the sibling
    rewriter roster made -- and round 122 found *that* one silently omitting a
    shipped rewriter, so an in-place spill went unmirrored. This roster gates a
    stronger check (a credential reaching disk), so an omitted strategy would
    ship exempt from a secret sweep. Derive the set from the module instead:
    every concrete class satisfying the `Compactor` protocol has to be on it.
    """
    listed = {c.__name__ for c in STRATEGIES}
    shipped = {
        name
        for name, obj in vars(compaction_module).items()
        if inspect.isclass(obj)
        and obj.__module__ == compaction_module.__name__
        and obj is not Compactor
        and issubclass(obj, Compactor)
    }
    assert shipped, "no Compactor implementations found -- the scan broke, not the code"
    missing = shipped - listed
    assert not missing, (
        f"the module ships {sorted(missing)} but STRATEGIES omits it, so the "
        "secret-leak sweep never runs against it -- the rot round 122 found in "
        "the sibling rewriter roster, in the roster that guards disk secrets"
    )


def test_the_summary_that_becomes_history_is_masked(tmp_path):
    """`compact` replaces the whole transcript with model-written prose.

    That prose is derived from a transcript that may hold a credential, and it
    is carried by every later turn. It is the one case of "prose the model wrote
    about a secret" this harness can reach, because it asked for the prose.
    """
    manager = _manager(
        tmp_path, secrets=SecretRegistry.from_environ(environ={"PROBE_API_KEY": SECRET})
    )
    agent = manager.create().agent
    agent.messages.extend(_transcript(count=1, body=f"key={SECRET}"))
    asyncio.run(DefaultCompactor().compact(agent))
    assert SECRET not in str(agent.messages)


def test_a_spill_helper_cannot_forget_masking_silently(tmp_path):
    """`tool_result_budget` defaults to unmasked, so the seam must be visible.

    A default of `secrets=None` is right for a pure function, but it means a
    caller that forgets the argument leaks. The shipped callers are covered by
    the workspace sweep above; this pins that the parameter exists at all, so
    removing it breaks here rather than quietly reverting the fix.
    """
    assert "secrets" in inspect.signature(tool_result_budget).parameters

    workspace = tmp_path / "spill"
    workspace.mkdir()
    registry = SecretRegistry.from_environ(environ={"PROBE_API_KEY": SECRET})
    messages = [
        {"role": "assistant", "content": [
            {"type": "tool_use", "id": "b", "name": "run_bash", "input": {}}]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "b",
             "content": f"{SECRET}\n" + "Z" * 300_000}]},
    ]
    assert tool_result_budget(messages, workspace, secrets=registry) == 1
    spilled = list((workspace / ".task_outputs").rglob("*.txt"))
    assert spilled and all(SECRET not in p.read_text() for p in spilled)
