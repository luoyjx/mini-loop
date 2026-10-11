"""Efficiency census for compaction: measure what the strategies save.

The compaction suite pins correctness thoroughly -- mirroring, pairing,
masking, provenance -- but nothing measured *efficiency*: how much
context each strategy actually recovers, and which shapes it silently
misses. Savings are fully deterministic (estimate_tokens is a pure
function of the transcript), so this surface is experimentable offline,
no real-endpoint budget needed (docs/RSI_RESEARCH_AND_PLAN.md §5).

Census findings, candidates for deliberate experiments:

* RESOLVED (micro-experiment D, 2026-09-01): a block-shaped tool_result
  used to be invisible to microcompact (`isinstance(content, str)` gate);
  the gate now weighs serialized size -- the same measure the context
  cost model uses -- so both shapes clear by the same rule.
* RESOLVED (micro-experiment C, 2026-09-01): the placeholder used to be
  the bare "[cleared]"; it now names the tool and the original size, so
  the model can weigh whether a re-fetch is worth a round-trip.

As with the tool edge census: pins first, experiments second, and a pin
flips in the same change as the experiment that lands.
"""

from mini_loop.compaction import estimate_tokens, microcompact


def _turn(tool_id: str, result: object) -> list[dict]:
    return [
        {"role": "assistant", "content": [
            {"type": "tool_use", "id": tool_id, "name": "read_file",
             "input": {"path": "x"}}]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": tool_id,
             "content": result}]},
    ]


def _transcript(results: list[object]) -> list[dict]:
    messages = [{"role": "user", "content": "do the thing"}]
    for index, result in enumerate(results):
        messages += _turn(f"t{index}", result)
    messages.append(
        {"role": "assistant", "content": [{"type": "text", "text": "done"}]})
    return messages


def test_microcompact_savings_are_measured():
    """Ten consumed 5k-char results: all but the 3-most-recent window are
    cleared, and the transcript sheds well over half its estimated
    tokens. This is the number the strategy exists to move -- measured,
    so a future policy experiment has a baseline to beat."""

    messages = _transcript(["R" * 5_000] * 10)
    before = estimate_tokens(messages)
    cleared = microcompact(messages)
    after = estimate_tokens(messages)

    assert cleared == 7
    assert after < before * 0.4, (
        f"microcompact recovered too little: {before} -> {after}"
    )
    # The keep-window holds exactly the 3 most recent consumed results.
    survivors = [
        part["content"]
        for message in messages
        if message.get("role") == "user" and isinstance(message["content"], list)
        for part in message["content"]
        if part.get("type") == "tool_result"
        and not str(part["content"]).startswith("[cleared")
    ]
    assert survivors == ["R" * 5_000] * 3


def test_a_list_shaped_result_is_cleared_like_any_other():
    """Micro-experiment D: the clear gate used to be
    isinstance(content, str), so a block-shaped result survived every
    pass however old and huge -- the exact shape-blindness blocks.py
    exists to prevent. The gate now weighs serialized size, so the
    block-shaped result clears, marker and all."""

    import json

    block_shaped = [{"type": "text", "text": "B" * 5_000}]
    messages = _transcript([block_shaped] + ["R" * 5_000] * 9)
    cleared = microcompact(messages)

    assert cleared == 7, "the block-shaped result now clears with the rest"
    oldest = messages[2]["content"][0]
    expected_weight = len(json.dumps(block_shaped, default=str))
    assert oldest["content"] == (
        f"[cleared: read_file, {expected_weight:,} chars]"
    )
    assert "B" * 5_000 not in str(messages), (
        "the block-shaped result's 5k chars are no longer carried"
    )

    # A small block-shaped result stays, same as a small string one.
    tiny_blocks = [{"type": "text", "text": "ok"}]
    small = _transcript([tiny_blocks] + ["R" * 5_000] * 9)
    microcompact(small)
    assert small[2]["content"][0]["content"] == tiny_blocks


def test_a_cleared_result_names_what_it_was():
    """Micro-experiment C: the placeholder used to be the bare
    "[cleared]" -- no tool name, no size -- so the model could not weigh
    a re-fetch. It now carries both, from the paired tool_use block."""

    messages = _transcript(["R" * 5_000] * 10)
    microcompact(messages)
    placeholder = messages[2]["content"][0]["content"]
    assert placeholder == "[cleared: read_file, 5,000 chars]"

    # A result with no findable tool_use pair still names the size.
    orphanish = [
        {"role": "user", "content": "go"},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "mystery",
             "content": "M" * 500}]},
    ] + _transcript(["R" * 5_000] * 4)[1:]
    microcompact(orphanish)
    assert orphanish[1]["content"][0]["content"] == "[cleared: 500 chars]"

    # The marker itself is under the 100-char clear gate: a second pass
    # must not clear or grow it (compaction runs repeatedly).
    before = [m for m in messages]
    assert microcompact(messages) == 0
    assert messages == before


def test_micro_compaction_waits_for_context_pressure(tmp_path):
    """Mined 2026-09-02: the prompt cache decayed 88% -> 33% across real
    sessions because microcompact rewrote history unconditionally every
    turn -- each clear invalidates the cached prefix from the edit point.
    Below half the summary threshold the transcript stays byte-stable and
    the cache keeps paying; past it, context space wins over cache."""

    import asyncio
    from types import SimpleNamespace

    from mini_loop.compaction import DefaultCompactor

    class _Agent:
        def __init__(self, messages, threshold):
            self.messages = messages
            self.workspace = tmp_path
            self.settings = SimpleNamespace(token_threshold=threshold)
            self.secrets = None
            self.events = []

        async def _send(self, *args, **fields):
            self.events.append((args[0], fields))

    # ~13k estimated tokens of clearable results: well under half of the
    # default 100k threshold -- the transcript must stay untouched.
    calm = _Agent(_transcript(["R" * 5_000] * 10), threshold=100_000)
    asyncio.run(DefaultCompactor().maybe_compact(calm))
    assert not any(kind == "compact" and fields.get("kind") == "micro"
                   for kind, fields in calm.events)
    assert all("[cleared" not in str(m) for m in calm.messages)

    # The same transcript over a low threshold is pressure: micro fires.
    pressed = _Agent(_transcript(["R" * 5_000] * 10), threshold=20_000)
    asyncio.run(DefaultCompactor(token_threshold=20_000).maybe_compact(pressed))
    assert any(fields.get("kind") == "micro" and fields.get("cleared") == 7
               for _, fields in pressed.events)


def test_a_small_consumed_result_is_left_alone():
    """Clearing a result at or under 100 chars saves almost nothing and
    costs information; the gate skips them, and the census pins it."""

    messages = _transcript(["tiny result"] + ["R" * 5_000] * 9)
    cleared = microcompact(messages)
    assert cleared == 6
    assert messages[2]["content"][0]["content"] == "tiny result"
