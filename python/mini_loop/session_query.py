"""Session query: the durable transcript is searchable, epochs included.

DeepSeek Harness exposes session reads/traces/search as a service
(`ctx.sessionQuery`) with a model-facing tool beside it. The mini-loop
version scopes deliberately narrower -- ONE session, its own -- because
that is where the capability pays for itself: compaction replaces the
live transcript with a summary, and the superseded epochs on disk are the
canonical record of what the agent actually saw. Before this tool the
model was told "[Context compressed. Full transcript: <path>]" -- a path
that `read_file` cannot always reach and that says nothing about earlier
epochs. Now it can search the durable transcript itself.

Bounds, because a search is work: epochs scanned, matches returned, and
snippet length are all capped, and rows come back as the store holds them
-- already masked (`_flush_messages` masks before appending), so a spilled
secret cannot be recovered through the search path.

The epoch bound is *stated*, never silent: past MAX_EPOCHS_SCANNED
compactions, the oldest epochs fall outside the scan, and a bare
"No matches" would read as "nothing anywhere in history" -- a clean report
that cannot be over-trusted is the same defect class as dsh's snapshot
suite accepting UNKNOWN_TOOL fixtures (postmortem 0002), and the round-185
viewer rule applies verbatim: a cap nobody mentions reads as full coverage.
"""

from __future__ import annotations

import json
from typing import Any

__all__ = ["search_transcript", "reconstruct_request", "install_session_query"]

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


def search_transcript(store: Any, session_id: str, query: str) -> dict:
    """Case-insensitive substring search across this session's epochs.

    Returns ``{"matches": [...], "first_epoch": N, "current_epoch": M,
    "epochs_skipped": K}`` -- coverage is part of the result, so no caller
    can render a partial scan as an exhaustive one.
    """

    needle = query.lower()
    current = max(1, store.transcript_epoch(session_id))
    first = max(1, current - MAX_EPOCHS_SCANNED + 1)
    result = {
        "matches": [],
        "first_epoch": first,
        "current_epoch": current,
        "epochs_skipped": first - 1,
    }
    matches: list[dict] = result["matches"]
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
                return result
    return result


def reconstruct_request(store: Any, session_id: str, seq: int) -> dict:
    """Rebuild the request a `model_start` describes, from the log alone.

    The join dsh's reconstructable-requests rule promises: messages from the
    epoch table (the event's `transcript_epoch` stamp names which transcript
    the model was seeing), tool schemas from the `tool_catalog` event the
    fingerprint references, and the system prompt from the `system_prompt`
    event the hash references. Rows come back as the store holds them --
    masked -- so the reconstruction is the durable projection of the
    request, byte-identical to what was sent whenever no secret was present.
    """

    events = store.load_events(session_id)
    start = next(
        (e for e in events
         if e.get("seq") == seq and e.get("type") == "model_start"),
        None,
    )
    if start is None:
        return {"error": f"no model_start at seq {seq}"}
    epoch = start.get("transcript_epoch")
    if epoch is None:
        return {"error": "this model_start predates the epoch stamp"}
    count = int(start.get("message_count") or 0)
    tools = system = capability = None
    for event in events:
        if event.get("seq", 0) >= seq:
            break
        if (event.get("type") == "tool_catalog"
                and event.get("fingerprint") == start.get("tool_catalog_fingerprint")):
            tools = event.get("schemas")
        elif (event.get("type") == "system_prompt"
                and event.get("hash") == start.get("system_hash")):
            system = event.get("text")
        elif (event.get("type") == "capability_plan"
                and event.get("fingerprint") == start.get("capability_fingerprint")):
            # What could execute, not only what existed (round 208): the
            # permission mode and sandbox posture in force for this request.
            capability = {
                k: event.get(k)
                for k in ("permission_mode", "sandbox", "sandbox_confined",
                          "catalog_fingerprint")
            }
    return {
        "messages": store.load_messages(session_id, epoch=epoch)[:count],
        "system": system,
        "tools": tools,
        "capability": capability,
        "model": start.get("model"),
        "max_tokens": start.get("max_tokens"),
    }


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
        result = search_transcript(store, session_id, query.strip())
        found = result["matches"]
        # The coverage caveat rides every partial-scan answer, match or not:
        # a bare "No matches" over a scan that skipped epochs reads as
        # "nothing anywhere in history".
        coverage = ""
        if result["epochs_skipped"]:
            coverage = (
                f"\n(searched epochs {result['first_epoch']}"
                f"-{result['current_epoch']} only; "
                f"{result['epochs_skipped']} earlier epoch(s) were not "
                "scanned -- older history exists beyond this search)"
            )
        if not found:
            return (
                f"No matches for {query!r} in the searched transcript."
                + coverage
            )
        lines = [
            f"epoch {m['epoch']} #{m['index']} ({m['role']}): ...{m['snippet']}..."
            for m in found
        ]
        if len(found) >= MAX_MATCHES:
            lines.append(f"... (capped at {MAX_MATCHES} matches; refine the query)")
        return "\n".join(lines) + coverage

    registry.register(Tool(
        "transcript_search",
        f"Search the durable transcript of THIS session (the most recent "
        f"{MAX_EPOCHS_SCANNED} epochs), including history that compaction "
        "summarized away. States when older epochs fall outside the scan.",
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
