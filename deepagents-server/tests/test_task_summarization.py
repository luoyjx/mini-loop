"""Pinned native summary behavior must survive cold replay and subagents."""

from datetime import datetime, timezone
from types import SimpleNamespace
from uuid import UUID

import pytest

pytest.importorskip("temporalio")
from langchain_core.messages import AIMessage, HumanMessage, ToolMessage
from temporalio.contrib.deepagents import TemporalModel

from mini_loop_deepagents.task_server import summarization as module


def middleware(monkeypatch):
    monkeypatch.setattr(module.workflow, "uuid4", lambda: UUID("22222222-2222-4222-8222-222222222222"))
    monkeypatch.setattr(module.workflow, "now", lambda: datetime(2026, 10, 16, 1, 2, 3, tzinfo=timezone.utc))
    return module.make_summarization_middleware("task-x", TemporalModel(model="openai:task-server"))


def test_summary_clock_session_and_message_ids_rebuild_without_normalization(monkeypatch):
    first, second = middleware(monkeypatch), middleware(monkeypatch)
    assert first.name == "SummarizationMiddleware"
    session = first._get_session_id({})
    assert session == second._get_session_id({})
    assert first._get_session_id({"_summarization_session_id": "retained"}) == "retained"
    messages = [HumanMessage(content="hello", id="input"), AIMessage(content="answer", id="reply")]
    assert first._section(messages) == second._section(messages)
    assert "2026-10-16T01:02:03+00:00" in first._section(messages)
    a = first._build_new_messages_with_path("summary", "/history/a.md")[0]
    b = second._build_new_messages_with_path("summary", "/history/a.md")[0]
    assert a == b and a.id.startswith("task-x:summary:")
    assert a.additional_kwargs == {"lc_source": "summarization"}
    assert a.id != second._build_new_messages_with_path("changed", "/history/a.md")[0].id


@pytest.mark.asyncio
async def test_native_summary_append_preserves_old_bytes_and_excludes_previous_summary(monkeypatch):
    summary = middleware(monkeypatch)
    calls = []

    class Backend:
        async def adownload_files(self, paths):
            return [SimpleNamespace(content=b"previous section\n", error=None)]

        async def aedit(self, path, old, new):
            calls.append((path, old, new))
            return SimpleNamespace(error=None)

    messages = [HumanMessage(content="old summary", additional_kwargs={"lc_source": "summarization"}),
                HumanMessage(content="new content", id="new")]
    path = await summary._aoffload_to_backend(Backend(), messages, "session-a")
    assert path == summary._get_history_path("session-a")
    _, old, new = calls[0]
    assert old == "previous section\n" and new.startswith(old)
    assert "new content" in new and "old summary" not in new
    assert "2026-10-16T01:02:03+00:00" in new


def test_native_patch_slot_keeps_dangling_call_repair_and_ids():
    middleware = module.StablePatchToolCalls("task-x")
    assert middleware.name == "PatchToolCallsMiddleware"
    request = AIMessage(content="", id="request", tool_calls=[{"name": "read_file", "id": "call-a", "args": {}}])
    result = middleware.before_agent({"messages": [request, HumanMessage(content="continue", id="next")]}, None)
    patched = [message for message in result["messages"] if isinstance(message, ToolMessage)]
    assert len(patched) == 1 and patched[0].id and patched[0].tool_call_id == "call-a"


@pytest.mark.asyncio
async def test_stable_patch_wraps_real_tool_result_without_rewriting_content():
    middleware = module.StablePatchToolCalls("task-x")
    result = ToolMessage(content="file contents", tool_call_id="call-b")
    async def handler(request):
        return result
    wrapped = await middleware.awrap_tool_call(SimpleNamespace(tool_call={"id": "call-b"}), handler)
    assert wrapped.id == "task-x:tool:call-b:0"
    assert wrapped.content == result.content
    assert result.id is None
