"""The test double became the dominant cost of running the tests.

Round 52 established that the suite is what executes every rule in this repo, so
its runtime is load-bearing. Profiling a 40-turn session to find the next hot
path found the answer in the *stand-in*, not the harness:

    0.851s total
    0.609s  fake_llm.count_tokens          (72%)
    3,037,893  generator steps

`sum(1 for char in payload if ord(char) < 128)` is the obvious spelling of
"count the ASCII characters", and it ran per request over the whole payload.
Encoding with `errors="ignore"` drops exactly the non-ASCII characters and
counts the rest in C -- 28x faster on a 142,600-character payload, and a 40-turn
session went from 0.851s to 0.119s.

Equivalence is pinned below rather than assumed, because this number is not
cosmetic: it becomes `FakeUsage.input_tokens`, which is what `TokenMeter`
calibrates against. An optimisation that shifted it by a token would move every
metering assertion for a reason nobody would connect to this file.

**And the honest negative.** The hunt started from `estimate_tokens`, which
serialises the whole transcript and is called about three times a turn. At a
transcript sitting on the default compaction threshold that is 0.96 ms a call,
2.87 ms a turn -- 0.144% of a model call. Real, wasteful, and not worth
touching. It is recorded here so the next person to notice it can stop after
reading rather than after measuring.
"""

import json
import time

import pytest

from mini_loop.fake_llm import count_tokens

PAYLOADS = [
    "",
    "ascii only, nothing special",
    "全中文的内容，没有任何西文字符",
    "mixed 混合 text with both",
    "emoji 🎉 and surrogate pairs 👨‍👩‍👧",
    '{"role": "user", "content": "quotes \\" and \\\\ backslashes"}',
    "x" * 10_000,
    "你" * 10_000,
]


def _naive(payload: str) -> int:
    """The definition the fast version has to keep matching, spelled out."""
    ascii_chars = sum(1 for char in payload if ord(char) < 128)
    wide_chars = len(payload) - ascii_chars
    return int(ascii_chars / 4 + wide_chars) + 8


def _request(body):
    return {"messages": [{"role": "user", "content": body}], "system": None,
            "tools": None}


@pytest.mark.parametrize("body", PAYLOADS, ids=range(len(PAYLOADS)))
def test_the_fast_count_matches_the_definition(body):
    """Not cosmetic: this feeds `FakeUsage`, which `TokenMeter` calibrates on."""
    payload = json.dumps(
        [_request(body)["messages"], None, None], default=str, ensure_ascii=False
    )
    assert count_tokens(_request(body)) == _naive(payload)


def test_counting_still_charges_non_ascii_more():
    """The property the double exists to have -- it must diverge from
    `estimate_tokens`, or every metering test passes for the wrong reason."""
    ascii_only = count_tokens(_request("hello world " * 500))
    wide = count_tokens(_request("你好世界啊啊 " * 500))
    assert wide > ascii_only * 2


def test_the_system_prompt_and_tools_are_still_counted():
    bare = count_tokens({"messages": [], "system": None, "tools": None})
    loaded = count_tokens({
        "messages": [],
        "system": "You are a careful agent. " * 200,
        "tools": [{"name": f"t{i}", "description": "does a thing " * 20}
                  for i in range(8)],
    })
    assert loaded > bare * 10


def _time(fn, payload, rounds=20):
    started = time.monotonic()
    for _ in range(rounds):
        fn(payload)
    return (time.monotonic() - started) / rounds


def test_counting_beats_the_character_by_character_version():
    """A guard on the constant, not the complexity.

    Linear is fine and unavoidable; a linear function with a Python-level
    generator inside it was 72% of a profiled session.

    Measured *against the naive version in the same run* rather than against a
    fixed millisecond budget. The first version of this asserted `< 5 ms`, which
    the slow implementation also satisfied at this payload size -- the mutation
    runner said so. An absolute threshold has to be re-tuned for every machine
    and every payload; a ratio does not.
    """
    payload = json.dumps(
        [[{"role": "user", "content": "hello world 你好世界 " * 2_000}], None, None],
        default=str, ensure_ascii=False,
    )
    request = _request("hello world 你好世界 " * 2_000)

    fast = _time(lambda _: count_tokens(request), payload)
    naive = _time(_naive, payload)

    assert fast < naive / 5, (
        f"counting takes {fast * 1000:.2f} ms against the naive "
        f"{naive * 1000:.2f} ms; the point of the change was the constant"
    )


def test_a_forty_turn_session_stays_fast(tmp_path):
    """End to end, because the constant only matters through the suite."""
    import asyncio
    import pathlib

    from mini_loop import SessionManager, Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool

    turns = {"n": 0}

    def responder(request):
        turns["n"] += 1
        if turns["n"] % 2:
            return ([tool("run_bash", _id=f"t{turns['n']}", command="echo hi")],
                    "tool_use")
        return ([text("X" * 3000)], "end_turn")

    session = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=pathlib.Path(__file__).resolve().parent.parent / "skills"),
        FakeAsyncAnthropic(responder=responder),
    ).create()

    async def run_all():
        for index in range(40):
            await session.agent.run(f"turn {index}")

    started = time.monotonic()
    asyncio.run(run_all())
    took = time.monotonic() - started

    # Measured at 0.119s after the fix and 0.851s before it, so this leaves
    # 4x headroom over the observed value and still catches a regression to the
    # character-by-character count.
    assert took < 0.5, f"40 turns took {took:.2f}s against the offline model"
