"""Session query: the durable transcript is searchable, epochs included.

DeepSeek Harness exposes session reads/traces/search as a service
(`ctx.sessionQuery`) with a model-facing tool beside it. The mini-loop
version scopes deliberately narrower -- ONE session, its own -- because
that is where the capability pays for itself: compaction replaces the
live transcript with a summary, and the superseded epochs on disk are the
canonical record of what the agent actually saw. Before this tool the
model was told "[Context compressed. Full transcript: <path>]" -- a path
that `read_file` cannot always reach and that says nothing about earlier
epochs. Now it can search every epoch of its own durable transcript.

Bounds, because a search is work: epochs scanned, matches returned, and
snippet length are all capped, and rows come back as the store holds them
-- already masked (`_flush_messages` masks before appending), so a spilled
secret cannot be recovered through the search path.
"""

from __future__ import annotations

import json
from typing import Any

__all__ = ["search_transcript", "install_session_query"]

MAX_EPOCHS_SCANNED = 20
MAX_MATCHES = 20
SNIPPET_CHARS = 240


def _texts(message: dict) -> str:
    content = message.get("content")
    if isinstance(content, str):
        return content
    try:
        return json.dumps(content, ensure_ascii=False, default=str)
    except (TypeError, ValueError):
        return str(content)


def search_transcript(store: Any, session_id: str, query: str) -> list[dict]:
    """Case-insensitive substring search across this session's epochs."""

    needle = query.lower()
    current = max(1, store.transcript_epoch(session_id))
    first = max(1, current - MAX_EPOCHS_SCANNED + 1)
    matches: list[dict] = []
    for epoch in range(first, current + 1):
        for index, message in enumerate(store.load_messages(session_id, epoch=epoch)):
            text = _texts(message)
            position = text.lower().find(needle)
            if position < 0:
                continue
            start = max(0, position - SNIPPET_CHARS // 2)
            matches.append({
                "epoch": epoch,
                "index": index,
                "role": message.get("role", "?"),
                "snippet": text[start:start + SNIPPET_CHARS],
            })
            if len(matches) >= MAX_MATCHES:
                return matches
    return matches


_SCHEMA = {
    "type": "object",
    "properties": {
        "query": {"type": "string", "description": "Substring to find (case-insensitive)."},
    },
    "required": ["query"],
}


def install_session_query(registry) -> None:
    from .registry import Tool

    async def transcript_search(ctx, query: str) -> str:
        manager = ctx.state.get("manager")
        store = getattr(manager, "state_store", None)
        session_id = ctx.state.get("session_id", "")
        if store is None or not session_id:
            return "Error: no durable transcript on this surface"
        if not (query or "").strip():
            return "Error: an empty query matches everything and answers nothing"
        found = search_transcript(store, session_id, query.strip())
        if not found:
            return f"No matches for {query!r} in the durable transcript."
        lines = [
            f"epoch {m['epoch']} #{m['index']} ({m['role']}): ...{m['snippet']}..."
            for m in found
        ]
        if len(found) >= MAX_MATCHES:
            lines.append(f"... (capped at {MAX_MATCHES} matches; refine the query)")
        return "\n".join(lines)

    registry.register(Tool(
        "transcript_search",
        "Search every durable epoch of THIS session's transcript, including "
        "history that compaction summarized away.",
        _SCHEMA,
        transcript_search,
        readonly=True,
        parallel_safe=True,
        risk="read",
    ))


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: reads are scoped to the calling session by "
    "construction (the id comes from server-owned state, not tool input)."
)
