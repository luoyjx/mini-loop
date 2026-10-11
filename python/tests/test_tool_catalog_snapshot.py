import json

import pytest

from mini_loop.registry import Tool, ToolRegistry


async def _noop(ctx, **kwargs):
    return "ok"


def _tool(name: str, *, schema: dict | None = None, capabilities=()) -> Tool:
    return Tool(
        name,
        f"{name} description",
        schema or {"type": "object", "properties": {}},
        _noop,
        readonly=True,
        risk="read",
        capabilities=frozenset(capabilities),
    )


def test_a_snapshot_is_detached_from_every_returned_schema():
    registry = ToolRegistry([_tool("read")])
    snapshot = registry.snapshot()

    first = snapshot.schemas()
    first[0]["description"] = "mutated"
    first[0]["input_schema"]["properties"]["x"] = {"type": "string"}

    second = snapshot.schemas()
    assert second[0]["description"] == "read description"
    assert second[0]["input_schema"]["properties"] == {}
    assert json.loads(snapshot.schema_json) == second


def test_equal_catalogues_have_the_same_fingerprint_despite_mapping_key_order():
    left_schema = {
        "type": "object",
        "properties": {"a": {"type": "string"}, "b": {"type": "integer"}},
    }
    right_schema = {
        "properties": {"b": {"type": "integer"}, "a": {"type": "string"}},
        "type": "object",
    }

    left = ToolRegistry([_tool("one", schema=left_schema)]).snapshot()
    right = ToolRegistry([_tool("one", schema=right_schema)]).snapshot()

    assert left.fingerprint == right.fingerprint
    assert left.schema_json == right.schema_json


def test_order_and_registry_mutation_are_visible_in_the_snapshot_identity():
    registry = ToolRegistry([_tool("one"), _tool("two")])
    before = registry.snapshot()

    reordered = ToolRegistry([_tool("two"), _tool("one")]).snapshot()
    assert reordered.fingerprint != before.fingerprint

    registry.register(_tool("three"))
    after = registry.snapshot()
    assert after.revision > before.revision
    assert after.fingerprint != before.fingerprint
    assert after.sent_names == ("one", "two", "three")


def test_capability_subsets_are_explicit_and_keep_parent_order():
    registry = ToolRegistry(
        [
            _tool("read", capabilities={"repo.read"}),
            _tool("outline", capabilities={"repo.semantic_outline"}),
            _tool("ask", capabilities={"human.ask"}),
        ]
    )

    child = registry.with_capabilities({"repo.read", "repo.semantic_outline"})

    assert child.names() == ["read", "outline"]


def test_empty_capability_names_are_rejected():
    with pytest.raises(ValueError, match="capabilities"):
        ToolRegistry([_tool("bad", capabilities={""})])


def test_tool_normalizes_capability_iterables():
    tool = Tool(
        "read",
        "read",
        {"type": "object", "properties": {}},
        _noop,
        capabilities=["repo.read"],
    )

    assert tool.capabilities == frozenset({"repo.read"})
