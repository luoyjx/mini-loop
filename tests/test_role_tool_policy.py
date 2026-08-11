import pytest

from mini_loop.builtins import default_registry
from mini_loop.registry import Tool
from mini_loop.tool_policy import DEFAULT_ROLE_TOOL_POLICY
from mini_loop.token_tools import RAW_ARTIFACT_TOOL, install_token_efficiency_tools


async def _noop(_ctx, **_kwargs):
    return "ok"


def _semantic(name: str, capability: str) -> Tool:
    return Tool(
        name,
        "semantic read",
        {"type": "object", "properties": {}},
        _noop,
        readonly=True,
        risk="read",
        capabilities=frozenset({capability}),
    )


def test_explore_inherits_semantic_reads_without_write_or_exec():
    parent = default_registry()
    parent.register(_semantic("ast_outline", "repo.semantic_outline"))
    parent.register(_semantic("ast_show", "repo.symbol"))
    parent.register(_semantic("ast_grep", "repo.references"))

    child = DEFAULT_ROLE_TOOL_POLICY.select("Explore", parent)

    assert child.names() == ["read_file", "glob", "ast_outline", "ast_show", "ast_grep"]
    assert "bash" not in child
    assert "write_file" not in child
    assert "edit_file" not in child


def test_worker_inherits_semantic_reads_and_base_execution_tools():
    parent = default_registry()
    parent.register(_semantic("ast_outline", "repo.semantic_outline"))
    install_token_efficiency_tools(parent)

    child = DEFAULT_ROLE_TOOL_POLICY.select("Worker", parent)

    assert child.names() == [
        "bash",
        "read_file",
        "write_file",
        "edit_file",
        "glob",
        "ast_outline",
        RAW_ARTIFACT_TOOL,
    ]


def test_explore_cannot_recover_parent_observations():
    parent = install_token_efficiency_tools(default_registry())

    assert RAW_ARTIFACT_TOOL not in DEFAULT_ROLE_TOOL_POLICY.select(
        "Explore", parent
    )
    assert RAW_ARTIFACT_TOOL in DEFAULT_ROLE_TOOL_POLICY.select("Worker", parent)


def test_uncategorized_parent_tools_are_not_implicitly_delegated():
    parent = default_registry()
    parent.register(_semantic("private_admin", "admin.root"))

    assert "private_admin" not in DEFAULT_ROLE_TOOL_POLICY.select("Worker", parent)


def test_every_declared_capability_must_fit_the_role():
    parent = default_registry()
    parent.register(
        _semantic("read_that_also_writes", "repo.read")
    )
    parent.get("read_that_also_writes").capabilities = frozenset(
        {"repo.read", "workspace.write"}
    )

    assert "read_that_also_writes" not in DEFAULT_ROLE_TOOL_POLICY.select(
        "Explore", parent
    )


def test_unknown_roles_fail_closed_and_policy_mapping_is_immutable():
    with pytest.raises(ValueError, match="unknown agent role"):
        DEFAULT_ROLE_TOOL_POLICY.select("typo", default_registry())
    with pytest.raises(TypeError):
        DEFAULT_ROLE_TOOL_POLICY.capabilities_by_role["typo"] = frozenset()
