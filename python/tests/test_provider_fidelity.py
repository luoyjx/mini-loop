"""The offline model is the only provider 432 tests ever see.

Whatever it does not reproduce, the suite cannot check. Diffed against the real
endpoint, it was missing most of a response:

    real   fields: id, model, role, type, stop_reason, stop_sequence, usage, ...
    real   blocks: thinking(signature), tool_use, text
    fake   fields: content, stop_reason
    fake   blocks: text, tool_use

`usage` was the previous round's finding. `thinking` is this one's, and it is
the *most common assistant block in real traffic* from a reasoning model -- the
live endpoint returns `['thinking', 'tool_use']` for an ordinary tool call. It
has to round-trip byte-exact: a continued tool-use conversation sends the
assistant's thinking back up, and the API rejects a thinking block whose
`signature` did not survive.

`_content_payload` reduced any block it did not name to `{"type": ...}`, so a
thinking block from a non-pydantic provider adapter arrived at the next request
with its signature and reasoning gone.
"""

import ast
import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.agent import _content_payload
from mini_loop.caching import DefaultCachePolicy
from mini_loop.compaction import microcompact, snip_compact
from mini_loop.fake_llm import FakeAsyncAnthropic, FakeMessage, ThinkingBlock
from mini_loop.secrets import SecretRegistry
from mini_loop.storage import SQLiteStateStore, _json_safe

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
AGENT_SOURCE = pathlib.Path(__file__).resolve().parent.parent / "mini_loop" / "agent.py"
SIGNATURE = "sig-ABC123-must-round-trip-verbatim"


def _manager(tmp_path, **kwargs):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
        **kwargs,
    )


def _thinking_turn():
    return [ThinkingBlock("weighing the options", SIGNATURE)]


def _signatures(payload):
    return [
        block.get("signature")
        for message in (payload if isinstance(payload, list) else [payload])
        for block in (message.get("content") if isinstance(message, dict) else message)
        if isinstance(block, dict) and block.get("type") == "thinking"
    ]


# --- the defect ------------------------------------------------------------

def test_an_unrecognized_block_keeps_its_fields():
    """The fallback used to summarize; it now preserves.

    Reducing a block to its type is lossy in the worst way for thinking, and the
    list of types not named explicitly only grows -- redacted thinking, server
    tool use, search results.
    """
    out = _content_payload(_thinking_turn())
    assert out[0]["type"] == "thinking"
    assert out[0]["signature"] == SIGNATURE
    assert out[0]["thinking"] == "weighing the options"


def test_a_block_with_no_attributes_still_degrades_safely():
    class Slotted:
        __slots__ = ()
        type = "mystery"

    assert _content_payload([Slotted()]) == [{"type": "mystery"}]


# --- the property that matters: the signature survives every transform -----

def test_signature_survives_normalization_and_storage(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    session = _manager(tmp_path, state_store=store).create()
    session.agent.messages.append(
        {"role": "assistant", "content": _content_payload(_thinking_turn())}
    )
    session._flush_messages()
    restored = store.load_messages(session.id, epoch=store.transcript_epoch(session.id))
    assert _signatures(restored) == [SIGNATURE]
    store.close()


def test_masking_does_not_disturb_a_signature(tmp_path):
    """A signature is opaque bytes; a masker walking strings must not touch it."""
    registry = SecretRegistry.from_environ(environ={"P_API_KEY": "sk-SECRETVALUE-123"})
    message = {"role": "assistant", "content": _content_payload(_thinking_turn())}
    masked = registry.mask_payload(_json_safe(message))
    assert _signatures([masked]) == [SIGNATURE]


def test_the_cache_policy_does_not_mark_a_thinking_block(tmp_path):
    """Verified against the live endpoint, which puts the breakpoint on the
    tool_result -- a `cache_control` on a thinking block is not accepted."""
    messages = [
        {"role": "assistant", "content": _content_payload(_thinking_turn())},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "t", "content": "out"}]},
    ]
    _, _, annotated = DefaultCachePolicy().annotate(
        system="s", tools=[], messages=messages
    )
    marked = [
        block.get("type")
        for message in annotated
        if isinstance(message.get("content"), list)
        for block in message["content"]
        if isinstance(block, dict) and "cache_control" in block
    ]
    assert "thinking" not in marked


@pytest.mark.parametrize(
    "rewrite",
    [microcompact, lambda m: snip_compact(m, max_messages=6)],
    ids=["microcompact", "snip_compact"],
)
def test_compaction_does_not_corrupt_a_signature(rewrite):
    messages = []
    for index in range(8):
        messages.append({"role": "assistant", "content": [
            *_content_payload(_thinking_turn()),
            {"type": "tool_use", "id": f"t{index}", "name": "b", "input": {}}]})
        messages.append({"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": f"t{index}", "content": "X" * 500}]})
    rewrite(messages)
    assert set(_signatures(messages)) <= {SIGNATURE}, "a signature was rewritten"


def test_a_reasoning_session_runs_end_to_end(tmp_path):
    agent = _manager(tmp_path).create().agent
    asyncio.run(agent.run("hello"))
    thinking = [
        block for message in agent.messages
        if isinstance(message.get("content"), list)
        for block in message["content"]
        if isinstance(block, dict) and block.get("type") == "thinking"
    ]
    assert thinking, "the offline model no longer produces thinking blocks"
    assert all(b.get("signature") for b in thinking)


# --- the family guard: the stand-in must carry what the harness reads ------

def test_the_fake_response_carries_every_field_the_agent_reads():
    """Mechanical, so the next missing field is caught rather than discovered.

    `usage` was found last round only because a wiring test failed; nothing was
    comparing the stand-in against what the code actually reaches for.
    """
    read: set[str] = set()
    for node in ast.walk(ast.parse(AGENT_SOURCE.read_text())):
        # getattr(response, "x", ...)
        if (isinstance(node, ast.Call) and getattr(node.func, "id", None) == "getattr"
                and len(node.args) >= 2 and isinstance(node.args[0], ast.Name)
                and node.args[0].id == "response"
                and isinstance(node.args[1], ast.Constant)):
            read.add(node.args[1].value)
        # response.x
        elif (isinstance(node, ast.Attribute) and isinstance(node.value, ast.Name)
              and node.value.id == "response"):
            read.add(node.attr)

    assert read, "found no reads of `response` -- the scan broke, not the fake"
    fake = FakeMessage([], "end_turn")
    missing = sorted(name for name in read if not hasattr(fake, name))
    assert not missing, (
        f"the agent reads {missing} from a response; the offline model has no such "
        "field, so those paths are exercised by nothing"
    )
