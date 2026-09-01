"""Pluggable context-compaction strategy (s08).

The agent calls `compactor.maybe_compact(agent)` at the top of every loop pass
and `compactor.compact(agent)` for an explicit `compress`. Swap in your own
`Compactor` to change *what* gets dropped or *how* history is summarized
(e.g. keep a rolling summary, store transcripts in S3, never auto-compact).
"""

from __future__ import annotations

import json
import re
import time
from pathlib import Path
from typing import Protocol, runtime_checkable

from .durable import atomic_write_text
from .blocks import block_field, block_text


def estimate_tokens(messages: list) -> int:
    return len(json.dumps(messages, default=str)) // 4


def _block_type(block) -> str:
    return block_field(block, "type", "")


def _message_has_tool_use(message: dict) -> bool:
    content = message.get("content")
    return isinstance(content, list) and any(_block_type(block) == "tool_use" for block in content)


def _is_tool_result_message(message: dict) -> bool:
    content = message.get("content")
    return isinstance(content, list) and any(_block_type(block) == "tool_result" for block in content)


def snip_compact(messages: list, max_messages: int = 50) -> int:
    """Remove the conversation middle without splitting tool-use/result pairs."""
    if len(messages) <= max_messages or max_messages < 4:
        return 0

    head_end = min(3, len(messages))
    if head_end < len(messages) and _message_has_tool_use(messages[head_end - 1]):
        while head_end < len(messages) and _is_tool_result_message(messages[head_end]):
            head_end += 1

    tail_budget = max(1, max_messages - head_end - 1)
    tail_start = max(head_end, len(messages) - tail_budget)
    if (tail_start > 0 and tail_start < len(messages)
            and _is_tool_result_message(messages[tail_start])
            and _message_has_tool_use(messages[tail_start - 1])):
        tail_start -= 1

    removed = tail_start - head_end
    if removed <= 0:
        return 0
    messages[:] = [
        *messages[:head_end],
        {"role": "user", "content": f"[snipped {removed} messages from conversation middle]"},
        *messages[tail_start:],
    ]
    return removed


def microcompact(messages: list) -> int:
    """Blank old, consumed tool results except for the 3 most recent.

    Returns how many were cleared. A tool-result batch is consumable context
    until a later assistant response proves the model has seen it. Preserve
    every result after the last assistant message as one protected tail -- an
    injector may append more user messages before the next provider request,
    but that does not make the preceding results consumed.

    Among results that *have* a later assistant response, old tool output is
    the cheapest context to shed -- the model already acted on it.

    Cleared entries are **replaced, not mutated**. That is not a style choice:
    the session mirrors the transcript to durable storage and detects a rewrite
    by pointer comparison, so an edit that leaves the message object in place is
    invisible to it. This function used to assign into the block dict, and the
    consequence was that compaction did not survive a restart -- the store kept
    the uncompacted transcript, and a session that compacted *because* it was
    near the context limit came back exactly as large as when it overflowed.
    """
    last_assistant = max(
        (
            index
            for index, message in enumerate(messages)
            if message.get("role") == "assistant"
        ),
        default=-1,
    )
    consumed = [
        (index, part_index)
        for index, message in enumerate(messages)
        if index < last_assistant
        if message.get("role") == "user" and isinstance(message.get("content"), list)
        for part_index, part in enumerate(message["content"])
        if isinstance(part, dict) and part.get("type") == "tool_result"
    ]
    targets: dict[int, list[int]] = {}
    for index, part_index in consumed[:-3]:
        targets.setdefault(index, []).append(part_index)

    # Micro-experiment C (docs/RSI_RESEARCH_AND_PLAN.md §5): the marker
    # names what was cleared and how big it was, so the model can weigh a
    # re-fetch instead of guessing what "[cleared]" used to be. The name
    # comes from the paired tool_use block, read shape-agnostically.
    tool_names = {
        block_field(block, "id"): block_field(block, "name")
        for message in messages
        if message.get("role") == "assistant"
        and isinstance(message.get("content"), list)
        for block in message["content"]
        if _block_type(block) == "tool_use"
    }

    cleared = 0
    for index, part_indexes in targets.items():
        message = messages[index]
        content = list(message["content"])
        touched = False
        for part_index in part_indexes:
            part = content[part_index]
            weight = _result_weight(part.get("content"))
            if weight > 100:
                name = tool_names.get(part.get("tool_use_id"))
                size = f"{weight:,} chars"
                marker = (f"[cleared: {name}, {size}]" if name
                          else f"[cleared: {size}]")
                content[part_index] = {**part, "content": marker}
                cleared += 1
                touched = True
        if touched:
            messages[index] = {**message, "content": content}
    return cleared


def _result_weight(content) -> int:
    """Context weight of a tool_result's content, whichever shape.

    Micro-experiment D (docs/RSI_RESEARCH_AND_PLAN.md §5): the clear gate
    used to be `isinstance(content, str)`, so a block-shaped result hid
    behind its shape however old and huge. Weight is what the context
    actually pays -- serialized size, the same measure estimate_tokens
    uses -- so both shapes clear by the same rule.
    """

    if isinstance(content, str):
        return len(content)
    return len(json.dumps(content, default=str))


def _safe_result_id(value: str) -> str:
    return re.sub(r"[^A-Za-z0-9._-]", "_", value or "tool-result")[:100]


def tool_result_budget(
    messages: list,
    workspace: Path,
    *,
    max_bytes: int = 200_000,
    preview_chars: int = 2_000,
    secrets=None,
) -> int:
    """Persist largest results until the newest result batch fits its budget.

    What lands on disk is masked. The in-memory transcript is allowed to hold a
    raw credential -- it goes back to the provider that already has it and dies
    with the process -- but this writes into the *workspace*, where the file
    outlives the session and the agent can read it back. The replacement block
    even hands the model the path. Spilling context is not a reason to widen
    where a secret is stored.
    """
    if not messages:
        return 0
    content = None
    target_index = None
    for index in range(len(messages) - 1, -1, -1):
        candidate = messages[index].get("content")
        if (messages[index].get("role") == "user" and isinstance(candidate, list)
                and any(isinstance(part, dict) and part.get("type") == "tool_result"
                        for part in candidate)):
            content = candidate
            target_index = index
            break
    if content is None:
        return 0
    targets = [(position, part) for position, part in enumerate(content)
               if isinstance(part, dict) and part.get("type") == "tool_result"
               and isinstance(part.get("content"), str)]
    total = sum(len(part["content"].encode("utf-8")) for _, part in targets)
    if total <= max_bytes:
        return 0

    output_dir = Path(workspace) / ".task_outputs" / "tool-results"
    output_dir.mkdir(parents=True, exist_ok=True)
    replacements: dict[int, str] = {}
    for position, part in sorted(
        targets, key=lambda item: len(item[1]["content"]), reverse=True
    ):
        if total <= max_bytes:
            break
        original = part["content"]
        result_id = _safe_result_id(str(part.get("tool_use_id", "tool-result")))
        path = output_dir / f"{result_id}-{int(time.time() * 1000)}.txt"
        atomic_write_text(
            path, secrets.mask(original) if secrets is not None else original
        )
        preview = original[:preview_chars]
        replacement = (
            f'<persisted-output path="{path}" bytes="{len(original.encode("utf-8"))}">\n'
            f"{preview}\n</persisted-output>"
        )
        total -= len(original.encode("utf-8"))
        total += len(replacement.encode("utf-8"))
        replacements[position] = replacement
    if not replacements:
        return 0
    # Replace the block dicts *and* the message object -- do not edit in place.
    # The session mirrors the transcript to disk and detects a rewrite by
    # comparing message-object references (`_transcript_was_rewritten`); a block
    # edited inside the same message object is invisible to it, so an in-place
    # spill was never mirrored and a restart handed back the un-budgeted (large)
    # transcript -- the exact stale-store bug round 27 fixed for `microcompact`,
    # here in the one rewriter the mirroring test's roster had left out.
    new_content = [
        {**part, "content": replacements[position]}
        if position in replacements else part
        for position, part in enumerate(content)
    ]
    messages[target_index] = {**messages[target_index], "content": new_content}
    return len(replacements)


def context_used(agent) -> int:
    """How full the context is, preferring the provider's count to a guess.

    `estimate_tokens` is off by 0.36x-2.64x depending on content, and never saw
    the system prompt or tool schemas at all. Every response carries the exact
    number; `TokenMeter` keeps it. Falls back to the estimate for an agent that
    has no meter, or before the first response has been seen.

    Note this changes what `token_threshold` *means*: it used to be compared
    against an estimate that excluded the system prompt and tools, so the real
    prompt was always larger than the number being thresholded.
    """

    meter = getattr(agent, "token_meter", None)
    if meter is None:
        return estimate_tokens(agent.messages)
    envelope_of = getattr(agent, "_request_envelope", None)
    if callable(envelope_of):
        # An anchor read under one system-prompt/tool-catalog envelope
        # misprices another (an MCP connect alone can add tens of thousands
        # of schema tokens the anchor never saw). `used_for` sets the anchor
        # aside on mismatch and answers with the estimate until the next
        # response re-anchors.
        return meter.used_for(agent.messages, envelope=envelope_of())
    return meter.used(agent.messages)


@runtime_checkable
class Compactor(Protocol):
    async def maybe_compact(self, agent) -> None: ...
    async def compact(self, agent) -> None: ...


class InMemoryCompactor:
    """Bound context without persisting tool results or transcripts.

    Workflow workers use this compactor so their nominally read-only tool
    policy cannot be bypassed by an internal context-management write.
    """

    def __init__(
        self,
        token_threshold: int | None = None,
        *,
        max_messages: int = 50,
    ) -> None:
        self.token_threshold = token_threshold
        self.max_messages = max_messages

    async def maybe_compact(self, agent) -> None:
        threshold = self.token_threshold or agent.settings.token_threshold
        if context_used(agent) < threshold:
            return
        snip_compact(agent.messages, self.max_messages)
        microcompact(agent.messages)

    async def compact(self, agent) -> None:
        snip_compact(agent.messages, self.max_messages)
        microcompact(agent.messages)


#: The summarization request, framed as a handoff (Codex's "CONTEXT
#: CHECKPOINT COMPACTION" pattern, prompts/templates/compact/prompt.md in
#: openai/codex): name the four things a resuming agent actually needs,
#: instead of asking for an unstructured summary and hoping the right
#: content lands.
COMPACTION_PROMPT = (
    "You are performing a context checkpoint compaction. Write a handoff "
    "summary for another instance of this agent that will resume the task. "
    "Include: current progress and key decisions made; important context, "
    "constraints, or user preferences; what remains to be done, as clear "
    "next steps; any critical data, examples, or references needed to "
    "continue. Be concise, structured, and focused on letting the next "
    "instance continue seamlessly."
)

#: Prefixed to the summary when it replaces the transcript. Framing the
#: summary as ANOTHER instance's handoff (Codex's summary_prefix.md) keeps
#: the model from mistaking it for user input, and carries the one behavior
#: instruction that matters: build on the work, do not redo it.
SUMMARY_PREFIX = (
    "A previous instance of this agent worked on this task and left the "
    "handoff summary below. Build on what is already done; do not repeat "
    "completed work."
)


class DefaultCompactor:
    """Four ordered layers: result budget, snip, micro, LLM summary."""

    def __init__(self, token_threshold: int | None = None, *, max_messages: int = 50,
                 result_budget: int = 200_000) -> None:
        self.token_threshold = token_threshold
        self.max_messages = max_messages
        self.result_budget = result_budget

    async def maybe_compact(self, agent) -> None:
        persisted = tool_result_budget(
            agent.messages,
            agent.workspace,
            max_bytes=self.result_budget,
            secrets=getattr(agent, "secrets", None),
        )
        if persisted:
            await agent._send("compact", kind="budget", persisted=persisted)
        snipped = snip_compact(agent.messages, self.max_messages)
        if snipped:
            await agent._send("compact", kind="snip", removed=snipped)
        cleared = microcompact(agent.messages)
        if cleared:
            await agent._send("compact", kind="micro", cleared=cleared)
        threshold = self.token_threshold or agent.settings.token_threshold
        if context_used(agent) > threshold:
            # The summary stage is itself a model call and can fail like one
            # (rate limits, overload, a provider error). It used to propagate:
            # the turn died on its *context-management* step, before the
            # request the user was waiting on was even attempted. DeepSeek
            # Harness's compaction taxonomy is explicit that a failed summary
            # closes the attempt with the surface unchanged -- the next
            # request either fits anyway or fails as itself, and *that* error
            # stays authoritative. Cancellation still wins.
            import asyncio

            try:
                await self.compact(agent)
            except asyncio.CancelledError:
                raise
            except Exception as error:
                await agent._send(
                    "compact", kind="failed",
                    error=f"{type(error).__name__}: {error}"[:500],
                )

    async def compact(self, agent) -> None:
        from .storage import _json_safe

        secrets = getattr(agent, "secrets", None)

        transcript_dir = agent.workspace / ".transcripts"
        transcript_dir.mkdir(parents=True, exist_ok=True)
        path = transcript_dir / f"transcript_{int(time.time() * 1000)}.jsonl"
        with open(path, "w") as f:
            for msg in agent.messages:
                # Detach before masking. An assistant turn can hold provider
                # block *objects*, and a masker that walks dicts and lists steps
                # straight past them -- the same trap that kept the durable
                # tables leaking after the event stream was fixed.
                if secrets is not None:
                    msg = secrets.mask_payload(_json_safe(msg))
                f.write(json.dumps(msg, default=str) + "\n")

        conv = json.dumps(agent.messages, default=str)[-80_000:]
        resp = await agent._create(
            [{"role": "user", "content": f"{COMPACTION_PROMPT}\n{conv}"}],
            max_tokens=2000,
            purpose="compaction",
        )
        # Read shape-agnostically: this request can itself be truncated and
        # come back as a `ContinuedResponse`, whose content is dicts. Reading it
        # by attribute yielded an empty summary -- and this line is followed by
        # replacing the *entire* transcript, so the agent lost everything and
        # got a file path in return.
        summary = block_text(resp.content)
        if not summary.strip():
            # The next line replaces the ENTIRE transcript. An empty summary
            # would trade the whole working context for a file path -- refuse,
            # so the pressure path reports a failed attempt with the surface
            # unchanged and an explicit `compress` returns an error the model
            # can read.
            raise RuntimeError(
                "compaction summary came back empty; refusing to replace the "
                "transcript with nothing"
            )
        # The summary is model-written prose about a transcript that may hold a
        # credential, and it becomes the *permanent* history -- every later turn
        # carries it. Masking it is the one case of "prose the model wrote about
        # a secret" this harness can actually reach, because it asked for it.
        if secrets is not None:
            summary = secrets.mask(summary)
        # Provenance measured BEFORE the replacement, while the compacted
        # transcript still exists: how many messages this summary stands in
        # for, and the token estimate it replaced. Pi P1-4 -- summary,
        # retained tail, original history, generation usage and provenance
        # persisted SEPARATELY -- so an audit can answer "what did this
        # compaction cost and what did it replace" from the log alone,
        # never from prose inside the summary.
        replaced_count = len(agent.messages)
        replaced_tokens = estimate_tokens(agent.messages)
        usage = getattr(resp, "usage", None)
        agent.messages[:] = [
            {"role": "user", "content":
                f"[Context compressed. Full transcript: {path}]\n"
                f"{SUMMARY_PREFIX}\n{summary}"}
        ]
        await agent._send(
            "compact", kind="auto", transcript=str(path),
            replaced_messages=replaced_count,
            replaced_tokens_estimate=replaced_tokens,
            summary_input_tokens=getattr(usage, "input_tokens", None),
            summary_output_tokens=getattr(usage, "output_tokens", None),
            summary_model=getattr(resp, "model", None),
        )

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: every compaction layer replaces objects rather than mutating them, which the transcript-mirror tests pin; runtime re-checking would re-read the whole log per pass."
)
