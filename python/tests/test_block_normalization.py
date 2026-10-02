"""Provider blocks never reach a traversal written for data.

This root cause produced four separate defects, each fixed at its own site:

* the store's serializer stringified them into `"ToolUseBlock(...)"`;
* the secret masker walked past them, so credentials kept reaching disk after
  the event stream was already clean;
* the trajectory writer had the same latent flaw, unexercised only because its
  caller happened to convert first;
* the dangling-tool-call scan found nothing on a live transcript, so cancelling
  a turn repaired nothing.

Fixing the fifth site was never going to be the answer. `agent.messages` now
holds plain data because the assistant turn is converted on the way *in*, and
these tests hold that line — including for the traversals that were each wrong
once.
"""

import asyncio
from pathlib import Path

import pytest

from mini_loop.agent import Agent, _content_payload
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, TextBlock, ToolUseBlock, text, tool
from mini_loop.registry import Hooks, Tool, ToolRegistry
from mini_loop.session import AgentSession
from mini_loop.skills import SkillLoader

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _agent(tmp_path, responder=None):
    async def echo(_ctx, **_):
        return "ok"

    registry = ToolRegistry()
    registry.register(
        Tool("echo", "echo", {"type": "object", "properties": {}}, echo)
    )
    return Agent(
        client=FakeAsyncAnthropic(responder=responder) if responder else FakeAsyncAnthropic(),
        settings=Settings(
            fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR
        ),
        workspace=tmp_path / "ws",
        skills=SkillLoader(SKILLS_DIR),
        tools=registry,
        hooks=Hooks(),
    )


def _tool_then_stop(kwargs: dict):
    if not kwargs.get("tools"):
        return [text("[summary]")], "end_turn"
    last = kwargs["messages"][-1]
    if isinstance(last.get("content"), str):
        return [text("working"), tool("echo", _id="t1")], "tool_use"
    return [text("done")], "end_turn"


def _raw_blocks(messages) -> list:
    return [
        block
        for message in messages
        for block in (
            message["content"] if isinstance(message.get("content"), list) else []
        )
        if not isinstance(block, dict)
    ]


# --- the invariant ----------------------------------------------------------

def test_a_live_transcript_holds_only_plain_data(tmp_path):
    agent = _agent(tmp_path, _tool_then_stop)
    asyncio.run(agent.run("go"))

    assert _raw_blocks(agent.messages) == [], (
        "a provider object reached the transcript; every traversal written for "
        "dicts will walk past it"
    )
    kinds = {
        block["type"]
        for message in agent.messages
        if isinstance(message.get("content"), list)
        for block in message["content"]
    }
    assert {"text", "tool_use", "tool_result"} <= kinds, kinds


def test_conversion_keeps_what_a_provider_requires_back():
    """A reasoner returns `thinking` blocks and rejects a turn that drops them.

    Verified against a live endpoint: the dict form is accepted with its
    signature intact.
    """

    class Thinking:
        type = "thinking"

        def model_dump(self):
            return {"type": "thinking", "thinking": "...", "signature": "sig-abc"}

    converted = _content_payload([Thinking(), ToolUseBlock("bash", {"c": 1}, "t1")])
    assert converted[0] == {"type": "thinking", "thinking": "...", "signature": "sig-abc"}
    assert converted[1]["type"] == "tool_use" and converted[1]["id"] == "t1"


def test_conversion_does_not_lose_an_unrecognised_block():
    """An unknown block keeps its type rather than vanishing."""

    class Novel:
        type = "some_future_block"

    assert _content_payload([Novel()]) == [{"type": "some_future_block"}]


# --- the four traversals that were each wrong once -------------------------

@pytest.mark.parametrize(
    "name,traverse",
    [
        ("storage._json_safe", lambda m: __import__(
            "mini_loop.storage", fromlist=["_json_safe"]
        )._json_safe(m)),
        ("secrets.mask_payload", lambda m: __import__(
            "mini_loop.secrets", fromlist=["SecretRegistry"]
        ).SecretRegistry().mask_payload(m)),
        ("trajectory._json_safe", lambda m: __import__(
            "mini_loop.trajectory", fromlist=["_json_safe"]
        )._json_safe(m)),
    ],
)
def test_each_traversal_sees_structured_blocks(name, traverse):
    """Fed the shape the transcript now actually holds, none of them lose it."""
    message = {
        "role": "assistant",
        "content": _content_payload([TextBlock("hi"), ToolUseBlock("bash", {"c": "x"}, "t1")]),
    }
    out = str(traverse(message))
    assert "tool_use" in out, f"{name} lost the tool call"
    assert "ToolUseBlock(" not in out, f"{name} stringified the block"


def test_the_dangling_scan_reads_both_shapes():
    """Kept bilingual: restored transcripts are dicts, and legacy rows may not be."""
    live = [{"role": "assistant", "content": _content_payload([ToolUseBlock("bash", {}, "tu_1")])}]
    legacy = [{"role": "assistant", "content": [ToolUseBlock("bash", {}, "tu_1")]}]
    assert AgentSession._unanswered_tool_uses(live) == ["tu_1"]
    assert AgentSession._unanswered_tool_uses(legacy) == ["tu_1"]


def test_the_guard_would_notice_a_raw_block(tmp_path):
    """A guard that cannot fail is not a guard."""
    agent = _agent(tmp_path)
    agent.messages.append({"role": "assistant", "content": [TextBlock("raw")]})
    assert _raw_blocks(agent.messages), "the detector cannot see a raw block"
