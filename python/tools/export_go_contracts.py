"""Freeze Python's current public contracts for the independent Go port.

From the repository root, run ``.venv/bin/python python/tools/export_go_contracts.py``
or add ``--check`` to reject drift without writing. The export uses the real
default registry, FastAPI OpenAPI builder, and SQLite schema constant. It does
not open a database, call a model, or include credentials.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import tempfile
from pathlib import Path


PYTHON_ROOT = Path(__file__).resolve().parent.parent
REPO_ROOT = PYTHON_ROOT.parent
TARGET = REPO_ROOT / "go" / "testdata"
sys.path.insert(0, str(PYTHON_ROOT))


def _json_bytes(value: object) -> bytes:
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + "\n").encode()


def _snapshot() -> dict[str, bytes]:
    with tempfile.TemporaryDirectory(prefix="mini-loop-go-contract-") as scratch:
        # server.py constructs its default app at import time. Isolate that
        # composition root too, before importing the module.
        os.environ["MINILOOP_FAKE_LLM"] = "1"
        os.environ["MINILOOP_WORKSPACE_ROOT"] = scratch

        from mini_loop.builtins import default_registry
        from mini_loop.agent import KNOWN_STOP_REASONS, MAX_RESUMPTIONS, REFUSAL_NOTICE
        from mini_loop.actions import UNKNOWN_RESULT
        from mini_loop.config import Settings
        from mini_loop.fake_llm import FakeMessage, FakeUsage, TextBlock, ToolUseBlock
        from mini_loop.server import create_app
        from mini_loop.storage import SCHEMA_VERSION, _SCHEMA

        registry = default_registry()
        tools = registry.schemas()
        tool_metadata = [
            {
                "name": tool.name,
                "risk": tool.risk,
                "readonly": tool.readonly,
                "parallel_safe": tool.parallel_safe,
                "capabilities": sorted(tool.capabilities),
            }
            for name in registry.names()
            if (tool := registry.get(name)) is not None
        ]
        openapi = create_app(
            settings=Settings(fake_llm=True, workspace_root=Path(scratch))
        ).openapi()

        fake_replies = [
            FakeMessage(
                [
                    TextBlock("Working on it."),
                    ToolUseBlock("bash", {"command": "echo handled: go"}, "toolu_1"),
                ],
                "tool_use", FakeUsage(13, 2), message_id="msg_fake_000001",
            ),
            FakeMessage(
                [TextBlock("Done. Tool said: handled: go")],
                "end_turn", FakeUsage(20, 1), message_id="msg_fake_000002",
            ),
            FakeMessage([], "refusal", FakeUsage(7, 0), message_id="msg_fake_000003"),
        ]

        # Keep only the fields the Python agent reads. The SDK-specific
        # stop_details/container extensions belong to the later G4 adapter.
        reply_snapshot = [
            {
                "id": reply.id,
                "type": reply.type,
                "role": reply.role,
                "model": reply.model,
                "content": [{"type": block.type, **vars(block)} for block in reply.content],
                "stop_reason": reply.stop_reason,
                "stop_sequence": reply.stop_sequence,
                "usage": vars(reply.usage),
            }
            for reply in fake_replies
        ]

    methods = {"get", "post", "put", "patch", "delete"}
    operations = sum(
        method in methods
        for path_item in openapi["paths"].values()
        for method in path_item
    )
    manifest = {
        "default_tool_names": [tool["name"] for tool in tools],
        "http_operation_count": operations,
        "sqlite_schema_version": SCHEMA_VERSION,
        "known_stop_reasons": sorted(KNOWN_STOP_REASONS),
        "max_resumptions": MAX_RESUMPTIONS,
        "refusal_notice": REFUSAL_NOTICE,
        "unknown_tool_result": UNKNOWN_RESULT,
    }
    return {
        "python-contract-manifest.json": _json_bytes(manifest),
        "python-default-tools.json": _json_bytes(tools),
        "python-default-tool-metadata.json": _json_bytes(tool_metadata),
        "python-fake-replies.json": _json_bytes(reply_snapshot),
        "python-openapi.json": _json_bytes(openapi),
        "python-sqlite-schema.sql": (_SCHEMA.strip() + "\n").encode(),
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="reject stale exports")
    args = parser.parse_args()

    snapshot = _snapshot()
    if args.check:
        stale = [name for name, body in snapshot.items()
                 if not (TARGET / name).is_file() or (TARGET / name).read_bytes() != body]
        if stale:
            print("Stale Go contract exports: " + ", ".join(stale), file=sys.stderr)
            return 1
        print(f"Python→Go contracts current: {len(snapshot)} files")
        return 0

    TARGET.mkdir(parents=True, exist_ok=True)
    for name, body in snapshot.items():
        (TARGET / name).write_bytes(body)
        print(f"wrote {TARGET / name} ({len(body)} bytes)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
