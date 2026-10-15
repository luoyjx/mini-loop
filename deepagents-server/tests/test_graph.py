from types import SimpleNamespace

import pytest
from langgraph.checkpoint.memory import InMemorySaver
from langgraph.store.memory import InMemoryStore

from mini_loop_deepagents.config import AgentSettings
from mini_loop_deepagents.graph import agent, build_agent, memory_namespace
from smoke_graph import OfflineToolModel


@pytest.fixture
def local_memory(monkeypatch):
    # Local ainvoke has no server_info. HTTP tests exercise the actual verified
    # user path; these tool/checkpoint tests explicitly use a test namespace.
    monkeypatch.setattr("mini_loop_deepagents.graph.memory_namespace", lambda rt: ("local-test", "memories"))


def test_memory_namespace_only_uses_verified_runtime_user():
    user = SimpleNamespace(identity="alice", is_authenticated=True)
    assert memory_namespace(SimpleNamespace(server_info=SimpleNamespace(user=user))) == ("alice", "memories")
    for runtime in [None, SimpleNamespace(server_info=None), SimpleNamespace(server_info=SimpleNamespace(user=None))]:
        with pytest.raises(PermissionError):
            memory_namespace(runtime)


def test_serving_factory_builds_actual_provider_during_inspection(monkeypatch):
    # Constructs the actual ChatOpenAI adapter and Deep Agent schema. No model
    # request is made; real provider credentials remain a deployment gate.
    monkeypatch.setenv("OPENAI_API_KEY", "synthetic-schema-check-only")
    called = []
    runtime = SimpleNamespace(ensure_user=lambda: called.append(True), execution_runtime=None)
    graph = agent(runtime)
    assert called == [True]
    assert "messages" in graph.get_input_jsonschema()["properties"]


async def test_real_graph_tool_execution_and_checkpoint_isolation(local_memory):
    graph = build_agent(
        AgentSettings(model="openai:offline"), model=OfflineToolModel(),
        checkpointer=InMemorySaver(), store=InMemoryStore(),
    )
    first = {"configurable": {"thread_id": "one"}}
    state = await graph.ainvoke({"messages": [{"role": "user", "content": 'TOOL:{"name":"write_file","args":{"file_path":"/note.txt","content":"private"}}'}]}, first)
    assert state["files"]["/note.txt"]["content"] == "private"
    assert "TOOL_RESULT" in state["messages"][-1].content
    assert (await graph.aget_state(first)).values["files"]["/note.txt"]["content"] == "private"
    other = await graph.ainvoke({"messages": [{"role": "user", "content": "hello"}]}, {"configurable": {"thread_id": "two"}})
    assert "/note.txt" not in other.get("files", {})


async def test_virtual_backend_refuses_shell_execution(local_memory):
    graph = build_agent(AgentSettings(model="openai:offline"), model=OfflineToolModel(), checkpointer=InMemorySaver(), store=InMemoryStore())
    state = await graph.ainvoke({"messages": [{"role": "user", "content": 'TOOL:{"name":"execute","args":{"command":"touch /tmp/should-never-execute"}}'}]}, {"configurable": {"thread_id": "no-shell"}})
    assert "execution not available" in state["messages"][-1].content.lower()
