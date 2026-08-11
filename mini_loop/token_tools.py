"""Typed recovery tool for token-efficiency projections."""

from __future__ import annotations

import asyncio

from .registry import Tool, ToolContext, ToolRegistry
from .token_efficiency import ObservationOutcome, RawArtifactStoreError


RAW_ARTIFACT_TOOL = "read_token_artifact"
MAX_RECOVERY_CHARS = 50_000


async def _read_token_artifact(
    ctx: ToolContext,
    raw_ref: str,
    offset: int = 0,
    limit: int = MAX_RECOVERY_CHARS,
) -> str:
    if isinstance(offset, bool) or not isinstance(offset, int) or offset < 0:
        return "Error: offset must be a non-negative integer"
    if isinstance(limit, bool) or not isinstance(limit, int) or limit <= 0:
        return "Error: limit must be a positive integer"
    limit = min(limit, MAX_RECOVERY_CHARS)
    runtime = getattr(ctx.agent, "token_efficiency", None)
    store = getattr(runtime, "raw_store", None)
    if store is None:
        return "Error: masked raw artifact recovery is not enabled for this session"
    try:
        content = await asyncio.to_thread(store.get_masked, raw_ref)
    except RawArtifactStoreError as error:
        return f"Error: {error}"
    total = len(content)
    start = min(offset, total)
    maximum_end = min(start + limit, total)

    def render(end: int) -> str:
        continuation = f"; continue with offset={end}" if end < total else ""
        header = f"[masked raw artifact; chars={start}:{end}/{total}{continuation}]"
        return f"{header}\n{content[start:end]}"

    # Binary-search by character offset, but account in UTF-8 bytes: 50k emoji
    # are very different from 50k ASCII bytes in a provider context.
    low, high, end = start, maximum_end, start
    while low <= high:
        candidate_end = (low + high) // 2
        if len(render(candidate_end).encode("utf-8")) <= MAX_RECOVERY_CHARS:
            end = candidate_end
            low = candidate_end + 1
        else:
            high = candidate_end - 1
    continuation = f"; continue with offset={end}" if end < total else ""
    header = f"[masked raw artifact; chars={start}:{end}/{total}{continuation}]"
    return f"{header}\n{content[start:end]}"


def install_token_efficiency_tools(registry: ToolRegistry) -> ToolRegistry:
    """Install the narrow recovery capability without replacing user tools."""

    if RAW_ARTIFACT_TOOL in registry:
        return registry
    registry.register(
        Tool(
            RAW_ARTIFACT_TOOL,
            "Read one bounded page of an already-masked tool result by session-scoped raw_ref.",
            {
                "type": "object",
                "properties": {
                    "raw_ref": {
                        "type": "string",
                        "description": "Opaque raw_ref emitted with a reduced tool result.",
                    },
                    "offset": {
                        "type": "integer",
                        "minimum": 0,
                        "default": 0,
                        "description": "Character offset for bounded recovery.",
                    },
                    "limit": {
                        "type": "integer",
                        "minimum": 1,
                        "maximum": MAX_RECOVERY_CHARS,
                        "default": MAX_RECOVERY_CHARS,
                        "description": (
                            "Requested characters; the complete rendered page is "
                            "hard-capped at 50000 UTF-8 bytes."
                        ),
                    },
                },
                "required": ["raw_ref"],
            },
            _read_token_artifact,
            readonly=True,
            parallel_safe=True,
            risk="read",
            capabilities=frozenset({"observation.recover"}),
        )
    )
    return registry


def render_recovery_marker(outcome: ObservationOutcome) -> str:
    """Return a bounded, content-free marker only for an applied projection."""

    observation = outcome.observation
    if not observation.reduced_by or observation.raw_ref is None:
        return ""
    components = ",".join(observation.reduced_by)
    return (
        "[token-efficiency projection; "
        f"components={components}; full masked result: "
        f'{RAW_ARTIFACT_TOOL}(raw_ref="{observation.raw_ref}"); '
        f"digest={observation.raw_digest}]"
    )


__all__ = [
    "RAW_ARTIFACT_TOOL",
    "MAX_RECOVERY_CHARS",
    "install_token_efficiency_tools",
    "render_recovery_marker",
]
