"""Actual custom schema metadata in Source manager/model request paths."""

import asyncio
import copy
import json
import tempfile
from pathlib import Path


def extension_schema_contracts() -> dict:
    from mini_loop import SessionManager, Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.registry import Tool, ToolRegistry

    base = {"type": "object", "properties": {"name": {"type": "string"}}}
    recipes = [
        ("basic", base),
        ("references", dict(base, **{"$defs": {"choice": {"enum": [1, True, None]}},
                                    "properties": {"name": {"type": "string"}, "choice": {"$ref": "#/$defs/choice"}}})),
        ("numeric-enum", dict(base, properties={"name": {"type": "string"}, "choice": {"enum": [1, 1.5, True, None]}})),
        ("structured-const", dict(base, properties={"name": {"type": "string"}, "constant": {"const": {"a": [1, False]}}})),
        ("fractional-bounds", dict(base, properties={"name": {"type": "string"}, "count": {"type": "number", "minimum": 0.5, "maximum": 2.5, "multipleOf": 0.25}})),
        ("pattern-format", dict(base, properties={"name": {"type": "string", "pattern": "^[A-Z]", "format": "custom", "examples": ["World", "用户"]}})),
        ("conditional", dict(base, **{"if": {"required": ["name"]}, "then": {"minProperties": 1}, "else": False})),
        ("boolean-property", dict(base, properties={"name": {"type": "string"}, "ignored": False})),
        ("array-without-items", dict(base, properties={"name": {"type": "string"}, "ignored": {"type": "array"}})),
        ("empty-schema", {}),
        ("root-true", True),
        ("root-false", False),
        ("root-null", None),
        ("root-array", ["inert", {"key": 1}]),
        ("root-text", "metadata only"),
        ("root-number", 1.25),
    ]

    async def capture(row_name, schema, root):
        requests, received = [], []

        def responder(kwargs):
            requests.append(copy.deepcopy(kwargs["tools"]))
            if len(requests) % 2:
                return [tool("greet", name="World")], "tool_use"
            return [text("done")], "end_turn"

        async def greet(ctx, name="World", **extra):
            received.append(ctx.state["resource_owner"])
            return f'{ctx.state["resource_owner"]}:Hello, {name}!'

        registry = ToolRegistry([Tool("greet", "Unicode schema 描述", copy.deepcopy(schema), greet, risk="read")])
        manager = SessionManager(Settings(fake_llm=True, workspace_root=root / "ws", skills_dir=root / "skills", spill_dir=None),
                                 FakeAsyncAnthropic(responder=responder, thinking=False), tool_registry=registry)
        rows = []
        sessions = [manager.create(owner=owner, permission_mode="auto") for owner in ["alice", "bob"]]
        for owner, session in zip(["alice", "bob"], sessions):
            snapshot = session.agent.tools.snapshot()
            await session.run("greet")
            result = session.agent.messages[2]["content"][0]
            rows.append(dict(owner=owner, output=result["content"], snapshot_json=snapshot.schema_json,
                             fingerprint=snapshot.fingerprint,
                             request_tools_json=json.dumps(requests[-2], ensure_ascii=False, sort_keys=True, separators=(",", ":")),
                             private_registry=session.agent.tools is not registry,
                             paired=result["tool_use_id"] == "toolu_x"))
        separate = sessions[0].agent.tools is not sessions[1].agent.tools
        await manager.stop()
        return dict(name=row_name, schema_json=json.dumps(schema, ensure_ascii=False), sessions=rows,
                    received=received, separate_registries=separate, template_names=registry.names())

    with tempfile.TemporaryDirectory(prefix="mini-loop-extension-schema-") as scratch:
        async def collect():
            return [await capture(name, schema, Path(scratch) / name) for name, schema in recipes]
        return dict(cases=asyncio.run(collect()), limits=[
            "Actual offline Source model requests; schema metadata is not validated or resolved by ToolRegistry.",
            "Finite schema values only. Server acceptance of non-object metadata is not claimed.",
            "Two manager owners execute a registered named handler; exact schema preservation grants no new authority.",
        ])
