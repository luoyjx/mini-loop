"""Compaction never orphans a tool block, at any boundary.

Every `tool_use` needs a `tool_result` and every `tool_result` needs its
`tool_use`; a transcript missing either is rejected by the provider with a 400
on *every subsequent turn* -- the session is bricked, not just the turn. Two
compaction paths cut the transcript and must preserve the pairing across their
cut:

* `snip_compact` removes the conversation middle. It extends the head past a
  trailing tool_use's results, and pulls the tail back to include a leading
  tool_result's tool_use.
* `reactive_compact` (recovery) drops the oldest turns to fit a "prompt too
  long" error, pulling its boundary back the same way.

The existing coverage was one `snip_compact` case at one boundary. This sweeps
the boundary across many transcript lengths, both parities (odd/even leading
messages), parallel tool calls (several tool_use in one message), and the
double-leading-user shape that lands a tool_use exactly on the head cut --
each of the three boundary adjustments is load-bearing on some case in this
sweep (removing it orphans 18-102 outcomes), and the guard is `validate_
transcript`, the provider's own shape check, not a hand-rolled scan.
"""

import pytest

from mini_loop.compaction import microcompact, snip_compact
from mini_loop.fake_llm import InvalidTranscript, validate_transcript
from mini_loop.recovery import reactive_compact


def _use(i, width):
    return {"role": "assistant", "content": [
        {"type": "tool_use", "id": f"t{i}_{j}", "name": "bash", "input": {}}
        for j in range(width)]}


def _result(i, width):
    return {"role": "user", "content": [
        {"type": "tool_result", "tool_use_id": f"t{i}_{j}", "content": "out"}
        for j in range(width)]}


def _text_user(s):
    return {"role": "user", "content": s}


def _text_asst(s):
    return {"role": "assistant", "content": [{"type": "text", "text": s}]}


def _transcripts():
    """Every shape the boundary math has to survive."""

    for pairs in range(1, 22):
        for width in (1, 2, 3):
            body = [m for i in range(pairs)
                    for m in (_use(i, width), _result(i, width))]
            # 0..2 leading (assistant,user) text turns shift the cut's parity.
            for lead in range(0, 3):
                head = [_text_user("start")]
                head += [m for _ in range(lead)
                         for m in (_text_asst("thinking"), _text_user("more"))]
                yield head + body + [_text_asst("final")]
            # Two leading user messages (first turn + an injected note) put a
            # tool_use exactly on the head cut -- the head-extension case.
            yield [_text_user("first"), _text_user("note")] + body + [_text_asst("f")]


def _copy(messages):
    return [dict(m) for m in messages]


def _compactions():
    yield "snip", lambda m: snip_compact(m, max_messages=6)
    yield "snip8", lambda m: snip_compact(m, max_messages=8)
    yield "micro", microcompact


def test_no_compaction_path_ever_orphans_a_tool_block():
    changed = 0
    total = 0
    for transcript in _transcripts():
        for name, fn in _compactions():
            work = _copy(transcript)
            fn(work)
            total += 1
            if len(work) != len(transcript):
                changed += 1
            try:
                validate_transcript(work)
            except InvalidTranscript as error:
                pytest.fail(
                    f"{name} orphaned a tool block on a "
                    f"{len(transcript)}-message transcript: {error}"
                )
        # reactive_compact returns a new list rather than mutating.
        for keep in (4, 6, 8):
            compacted = reactive_compact(_copy(transcript), keep=keep)
            total += 1
            if len(compacted) != len(transcript):
                changed += 1
            try:
                validate_transcript(compacted)
            except InvalidTranscript as error:
                pytest.fail(
                    f"reactive_compact(keep={keep}) orphaned a tool block on a "
                    f"{len(transcript)}-message transcript: {error}"
                )
    # Non-vacuity: the sweep must actually exercise compaction, not skip it
    # because every transcript was already short enough.
    assert changed > total // 2, (
        f"only {changed}/{total} cases actually compacted; the sweep is mostly "
        "testing the no-op path"
    )


def test_the_validator_would_catch_an_orphan():
    """The guard above means nothing if validate_transcript accepts orphans."""

    orphan_result = [
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "gone", "content": "x"}]},
        {"role": "assistant", "content": [{"type": "text", "text": "hi"}]},
    ]
    with pytest.raises(InvalidTranscript):
        validate_transcript(orphan_result)

    orphan_use = [
        {"role": "user", "content": "hi"},
        {"role": "assistant", "content": [
            {"type": "tool_use", "id": "t0", "name": "bash", "input": {}}]},
        {"role": "user", "content": "no result followed"},
    ]
    with pytest.raises(InvalidTranscript):
        validate_transcript(orphan_use)


def test_the_head_extension_case_is_actually_reached():
    """The double-leading-user transcript must really put a tool_use on the
    head cut, or the head-extension guard is never exercised by the sweep."""

    from mini_loop.compaction import _message_has_tool_use

    reached = [t for t in _transcripts()
               if t[0]["content"] == "first" and _message_has_tool_use(t[2])]
    assert reached, "no transcript lands a tool_use at the head boundary"
