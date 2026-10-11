"""Real recovery tool/schema, including character offsets and rendered byte caps."""
import asyncio
import hashlib
from pathlib import Path
from types import SimpleNamespace
import tempfile
from mini_loop.registry import ToolContext, ToolRegistry, Tool
from mini_loop.token_efficiency import MaskedRawArtifactStore
from mini_loop.token_tools import RAW_ARTIFACT_TOOL, install_token_efficiency_tools


def efficiency_recovery_contracts() -> dict:
    async def capture():
        tool = install_token_efficiency_tools(ToolRegistry()).get(RAW_ARTIFACT_TOOL)
        rows = []
        specs = [
            ("default", "hello文🔒", 1, {}, "normal"),
            ("slice", "hello文🔒", 1, dict(offset=5, limit=1), "normal"),
            ("beyond", "hello", 1, dict(offset=999), "normal"),
            ("huge-offset", "hello", 1, dict(offset=10**100), "normal"),
            ("huge-limit", "a", 60000, dict(limit=10**100), "normal"),
            ("ascii-page", "a", 60000, {}, "normal"),
            ("emoji-page", "🔒", 50000, {}, "normal"),
            ("second-page", "🔒", 50000, dict(offset=12470), "normal"),
            ("mixed-page", "文a🔒", 20000, {}, "normal"),
            ("empty", "", 1, {}, "normal"),
            ("negative-offset", "x", 1, dict(offset=-1), "normal"),
            ("zero-limit", "x", 1, dict(limit=0), "normal"),
            ("negative-limit", "x", 1, dict(limit=-1), "normal"),
            ("null-offset", "x", 1, dict(offset=None), "normal"),
            ("null-limit", "x", 1, dict(limit=None), "normal"),
            ("no-store", "x", 1, {}, "unavailable"),
            ("foreign-store", "x", 1, {}, "foreign"),
            ("closed-store", "x", 1, {}, "closed"),
            ("expired", "x", 1, {}, "expired"),
            ("invalid-ref", "x", 1, {}, "invalid"),
        ]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name, content, repeat, params, state in specs:
                now = 0
                store = MaskedRawArtifactStore(root, clock=lambda: now, ttl_seconds=10)
                pointer = store.put_masked(content * repeat)
                active_store = store
                if state == "unavailable": active_store = None
                if state == "foreign": active_store = MaskedRawArtifactStore(root)
                if state == "closed": store.close()
                if state == "expired": now = 10
                ref = "../invalid" if state == "invalid" else pointer.ref
                ctx = ToolContext(SimpleNamespace(token_efficiency=SimpleNamespace(raw_store=active_store)), root, {})
                output = await tool.run(ctx, raw_ref=ref, **params)
                encoded = output.encode("utf-8")
                rows.append(dict(name=name, content=content, repeat=repeat, params=params, state=state,
                    output_sha256=hashlib.sha256(encoded).hexdigest(), output_bytes=len(encoded),
                    header=output.split("\n",1)[0]))
                store.close()
                if active_store is not None: active_store.close()
            custom = Tool(RAW_ARTIFACT_TOOL, "custom", {}, lambda ctx: "custom")
            registry = ToolRegistry([custom])
            same_registry = install_token_efficiency_tools(registry) is registry
            custom_retained = registry.get(RAW_ARTIFACT_TOOL) is custom
        return dict(schema=tool.schema, readonly=tool.readonly, parallel_safe=tool.parallel_safe,
                    risk=tool.risk, capabilities=sorted(tool.capabilities), rows=rows,
                    same_registry=same_registry, custom_retained=custom_retained)
    return asyncio.run(capture())
