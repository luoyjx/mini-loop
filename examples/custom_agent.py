"""End-to-end example: build a domain-specific agent by composing the seams,
without editing any core file.

Run it offline (no API key):

    MINILOOP_FAKE_LLM=1 .venv/bin/python examples/custom_agent.py

It wires up:
  * a custom tool (`web_search`) added to the registry
  * a permission Hook that blocks `bash` and audits every call
  * a custom system prompt built from sections
  * a per-tenant workspace factory
  * a global event sink (here: print)

To serve YOUR configured fleet over HTTP, build the SessionManager the same way
and hand it to FastAPI -- see the `build_app()` function at the bottom and
EXTENDING.md ("Serving a customized fleet").
"""

from __future__ import annotations

import asyncio
import pathlib
import sys

# Make `mini_loop` importable when this file is run directly (python examples/...).
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent.parent))

from mini_loop import (
    Hook,
    Hooks,
    SessionManager,
    build_client,
    default_registry,
    load_settings,
    sections_builder,
)

# 1) Custom tool -------------------------------------------------------------
registry = default_registry()


@registry.add(
    "web_search",
    "Search the web for a query and return the top result (demo: echoes).",
    {"type": "object", "properties": {"query": {"type": "string"}}, "required": ["query"]},
)
async def web_search(ctx, query):
    # Real impl: call your search API here. ctx.workspace / ctx.state are yours.
    ctx.state.setdefault("searches", []).append(query)
    return f"[top result for {query!r}] ... (wire a real API in here)"


# 2) Permission + audit hook -------------------------------------------------
class Policy(Hook):
    async def before_tool(self, ctx, call):
        if call.name == "bash":
            return "DENIED: shell is disabled for this product"
        return None

    async def after_tool(self, ctx, call, output):
        await ctx.emit_event("audit", tool=call.name, ok=not output.startswith("Error"))
        return None


# 3) Custom system prompt (composed from sections) ---------------------------
system_builder = sections_builder(
    "You are AcmeBot, a research assistant. Cite sources.",
    lambda a: f"Workspace: {a.workspace}. Tools: {', '.join(a.tools.names())}.",
)


# 4) Per-tenant workspace factory -------------------------------------------
def workspace_factory(session_id: str):
    settings = load_settings()
    return settings.workspace_root / "acme" / session_id


def build_manager() -> SessionManager:
    settings = load_settings()
    client = build_client(settings)  # real or fake, per MINILOOP_FAKE_LLM
    return SessionManager(
        settings,
        client,
        tool_registry=registry,
        hooks=Hooks([Policy()]),
        system_builder=system_builder,
        workspace_factory=workspace_factory,
        event_sink=lambda e: print(f"  · {e['session'][:6]} {e['type']}: "
                                   f"{e.get('text') or e.get('name') or e.get('error') or ''}"),
    )


async def _demo():
    mgr = build_manager()
    session = mgr.create()
    print(f"session {session.id} @ {session.workspace}\n")
    final = await session.run("find recent papers on agent harnesses")
    print(f"\nFINAL: {final}")
    print(f"searches recorded in state: {session.agent.state.get('searches')}")


# Optional: serve YOUR customized fleet over HTTP -- same routes/SSE/console,
# your manager. Run with:  uvicorn examples.custom_agent:app --factory
def build_app():
    from mini_loop.server import create_app

    return create_app(manager=build_manager())


app = build_app  # `--factory` calls this to get the FastAPI instance


if __name__ == "__main__":
    asyncio.run(_demo())
