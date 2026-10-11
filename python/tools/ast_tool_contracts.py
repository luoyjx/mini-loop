"""Capture installed Source AST tool declarations without replacing handlers."""
from mini_loop.ast_context import install_ast_context_tools
from mini_loop.registry import ToolRegistry


def ast_tool_contracts() -> dict:
    registry = install_ast_context_tools(ToolRegistry())
    rows = []
    for name in registry.names():
        tool = registry.get(name)
        rows.append(dict(schema=tool.schema, readonly=tool.readonly,
                         parallel_safe=tool.parallel_safe, risk=tool.risk,
                         capabilities=sorted(tool.capabilities)))
    return dict(tools=rows)
