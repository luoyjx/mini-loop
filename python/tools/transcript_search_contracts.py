"""Actual Source transcript search and model-facing rendering observations."""
from __future__ import annotations

import asyncio
import tempfile
from pathlib import Path
from types import SimpleNamespace


def transcript_search_contracts() -> dict:
    from mini_loop.registry import ToolContext, ToolRegistry
    from mini_loop.session_query import install_session_query, search_transcript
    from mini_loop.storage import NullStateStore, SQLiteStateStore

    registry = ToolRegistry()
    install_session_query(registry)
    tool = registry.get("transcript_search")
    assert tool is not None
    # Epoch/message inputs are retained so Go can replay them through its own
    # typed store. Source executes these against real SQLite, not a fake search.
    cases = [
        ("empty", [], "needle"),
        ("epochs", [
            (1, [{"role": "user", "content": "old NEEDLE"}]),
            (3, [{"role": "assistant", "content": [{"type": "text", "text": "new needle"}]}]),
        ], "  NeEdLe  "),
        ("omitted-history", [
            (1, [{"role": "user", "content": "needle only here"}]),
            (25, [{"role": "user", "content": "recent noise"}]),
        ], "needle"),
        ("partial-match", [
            (1, [{"role": "user", "content": "old needle"}]),
            (6, [{"role": "assistant", "content": "retained needle"}]),
            (25, [{"role": "user", "content": "noise"}]),
        ], "needle"),
        ("match-cap", [(1, [
            {"role": "user", "content": f"needle {index}"} for index in range(23)
        ])], "needle"),
        ("unicode-snippet", [(1, [
            {"role": "user", "content": "😀" * 130 + "NEEDLE" + "文" * 250},
            {"role": "assistant", "content": "İ" * 130 + "NEEDLE" + "后" * 250},
            {"content": None},
            {"role": "assistant", "content": {"needle": [True, None, "文"]}},
        ])], "needle"),
        ("quote-no-match", [], "a'\"b\\c\n"),
        ("blank", [(1, [{"role": "user", "content": "not empty"}])], " \n\t"),
    ]

    async def observe():
        rows = []
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            store = SQLiteStateStore(root / "state.db")
            try:
                # Foreign content must never be searched via a caller query.
                store.append_messages("foreign", [{"role": "user", "content": "foreign needle"}], epoch=1)
                for name, epochs, query in cases:
                    for epoch, messages in epochs:
                        store.append_messages(name, messages, epoch=epoch)
                    state = {"manager": SimpleNamespace(state_store=store), "session_id": name}
                    output = await tool.run(ToolContext(None, root, state), query=query)
                    rows.append(dict(name=name, epochs=[dict(epoch=e, messages=m) for e, m in epochs],
                        query=query, search=search_transcript(store, name, query.strip()), output=output))
                surfaces = []
                for name, state in [
                    ("bare", {}),
                    ("no-store", {"manager": SimpleNamespace(state_store=None), "session_id": "own"}),
                    ("no-id", {"manager": SimpleNamespace(state_store=store)}),
                    ("null-store", {"manager": SimpleNamespace(state_store=NullStateStore()), "session_id": "own"}),
                ]:
                    surfaces.append(dict(name=name, output=await tool.run(ToolContext(None, root, state), query="needle")))
            finally:
                store.close()
        return dict(schema=tool.schema, readonly=tool.readonly, parallel_safe=tool.parallel_safe,
            risk=tool.risk, capabilities=sorted(tool.capabilities), rows=rows, surfaces=surfaces)

    return asyncio.run(observe())
