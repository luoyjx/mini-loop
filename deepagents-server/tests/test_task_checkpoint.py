"""Native saver transport checks; no Temporal service or model provider starts."""

from __future__ import annotations

from typing import Annotated, Any, TypedDict

import pytest
from langchain_core.messages import AIMessage, HumanMessage
from langgraph.channels import DeltaChannel
from langgraph.graph import END, START, StateGraph

from mini_loop_deepagents.task_server import checkpoint


def merge_messages(previous, writes):
    return [*previous, *(message for batch in writes for message in batch)]


class NativeState(TypedDict):
    messages: Annotated[list, DeltaChannel(merge_messages, list, snapshot_frequency=50)]
    files: dict[str, Any]
    _summarization_event: dict[str, Any]
    _summarization_session_id: str
    turns: int


def make_graph(saver):
    graph = StateGraph(NativeState)
    graph.add_node("left", lambda _: {"messages": [HumanMessage(content="left", id="left")]})
    graph.add_node("right", lambda _: {"messages": [HumanMessage(content="right", id="right")]})
    graph.add_node("join", lambda _: {})
    graph.add_node("model", lambda state: {"turns": state["turns"] + 1,
                                           "messages": [AIMessage(content="model", id=f"model-{state['turns']}")]})
    graph.add_edge(START, "left")
    graph.add_edge(START, "right")
    graph.add_edge(["left", "right"], "join")
    graph.add_edge("join", "model")
    graph.add_conditional_edges("model", lambda state: END if state["turns"] >= 2 else "model")
    return graph.compile(checkpointer=saver)


@pytest.mark.asyncio
async def test_public_journal_restores_delta_ancestors_routes_and_private_summary():
    config = {"configurable": {"thread_id": "task"}}
    saver = checkpoint.PortableSaver("task")
    original = make_graph(saver)
    await original.ainvoke({"messages": [HumanMessage(content="input", id="input")], "turns": 0,
                           "files": {"/a": {"content": "keep", "created_at": "fixed"}},
                           "_summarization_event": {"cutoff_index": 1, "summary_message": HumanMessage(content="summary", id="summary"), "file_path": "/history"},
                           "_summarization_session_id": "session-stable"}, config, interrupt_before=["model"])
    before = await original.aget_state(config)
    assert before.next == ("model",)
    restored_saver = checkpoint.PortableSaver.restore(saver.export(), "task")
    restored = make_graph(restored_saver)
    after = await restored.aget_state(config)
    assert after.values == before.values
    assert after.next == before.next
    assert [task.id for task in after.tasks] == [task.id for task in before.tasks]
    assert restored_saver.get_delta_channel_history(config=after.config, channels=["messages"]) == saver.get_delta_channel_history(config=before.config, channels=["messages"])
    for graph in (original, restored):
        await graph.ainvoke(None, config, interrupt_before=["model"])
        await graph.ainvoke(None, config, interrupt_before=["model"])
    assert (await restored.aget_state(config)).values == (await original.aget_state(config)).values
    assert (await restored.aget_state(config)).next == ()


def test_journal_preserves_task_path_order_and_freezes_mutable_writes():
    saver = checkpoint.PortableSaver("task")
    config = {"configurable": {"thread_id": "task", "checkpoint_ns": "", "checkpoint_id": "checkpoint"}}
    writes = [("messages", ["original"])]
    saver.put_writes(config, writes, "task-z", "a-path")
    saver.put_writes(config, [("messages", ["second"])], "task-a", "z-path")
    writes[0][1].append("mutated later")
    restored = checkpoint.PortableSaver.restore(saver.export(), "task")
    # Public saver history orders deltas by task_path before task_id. The
    # aggregate journal retains those paths even though CheckpointTuple does not.
    decoded = [restored.serde.loads_typed(entry) for entry in restored._journal]
    assert [record["task_path"] for record in decoded] == ["a-path", "z-path"]
    assert decoded[0]["writes"][0][1] == ["original"]


@pytest.mark.parametrize("mutation", [
    lambda value: value.update(thread_id="foreign"),
    lambda value: value.update(codec="pickle"),
    lambda value: value.update(raw_bytes=1),
    lambda value: value.update(sha256="0" * 64),
    lambda value: value.update(data="!not-base64"),
    lambda value: value.update(records=999),
])
def test_internal_transport_rejects_identity_encoding_or_integrity_changes(mutation):
    saver = checkpoint.PortableSaver("task")
    saver.put_writes({"configurable": {"thread_id": "task", "checkpoint_id": "id"}}, [("x", 1)], "task-id")
    snapshot = saver.export()
    mutation(snapshot)
    with pytest.raises(checkpoint.InvalidCheckpointError):
        checkpoint.PortableSaver.restore(snapshot, "task")


def test_transport_rejects_raw_compressed_and_envelope_budget(monkeypatch):
    saver = checkpoint.PortableSaver("task")
    config = {"configurable": {"thread_id": "task", "checkpoint_id": "id"}}
    saver.put_writes(config, [("x", "content")], "task-id")
    snapshot = saver.export()
    monkeypatch.setattr(checkpoint, "MAX_COMPRESSED_BYTES", 1)
    with pytest.raises(checkpoint.CheckpointLimitError):
        saver.export()
    with pytest.raises(checkpoint.CheckpointLimitError):
        checkpoint.PortableSaver.restore(snapshot, "task")
    monkeypatch.setattr(checkpoint, "MAX_COMPRESSED_BYTES", 384 * 1024)
    monkeypatch.setattr(checkpoint, "MAX_RAW_BYTES", 1)
    with pytest.raises(checkpoint.CheckpointLimitError):
        saver.put_writes(config, [("x", 2)], "another")
    monkeypatch.setattr(checkpoint, "MAX_ENVELOPE_BYTES", 1)
    with pytest.raises(checkpoint.CheckpointLimitError):
        checkpoint.require_envelope_budget({"continuation": snapshot})


def test_no_pickle_fallback_and_saver_clones_share_budget(monkeypatch):
    saver = checkpoint.PortableSaver("task")
    clone = saver.with_allowlist([])
    assert clone._journal is saver._journal
    assert clone._journal_bytes is saver._journal_bytes
    assert saver.serde.pickle_fallback is False
    monkeypatch.setattr(checkpoint, "MAX_RAW_BYTES", 1)
    with pytest.raises(checkpoint.CheckpointLimitError):
        clone.put_writes({"configurable": {"thread_id": "task", "checkpoint_id": "id"}}, [("x", "data")], "task-id")
