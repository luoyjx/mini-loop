"""Everything the package reads off a provider object must exist on the double.

The offline model has been too thin three times, each found by a different
accident: `usage` absent (round 29, a wiring test failed), the non-streaming
`max_tokens` ceiling unenforced (round 38, a recovery path could never have
worked), and `thinking` deltas indistinguishable from `text` deltas (round 41, a
separation that looked correct and could not have been running).

Round 30 built a check for exactly one object -- the attributes `agent.py` reads
off `response`. That check kept working and the surface grew past it. Streaming
added a stream, its events, and their deltas, and none of them were covered.

This generalizes it. Every attribute the package reads off a provider-derived
value is collected by AST, and the double is required to answer for it. What is
*not* attempted is parity with the SDK's full type surface: `citations`,
`inference_geo`, `output_tokens_details` and friends are real fields nobody here
touches, and chasing them is a treadmill that would make this check noisy enough
to disable. The fields added by hand are the ones a **live** response was
observed to carry.
"""

import ast
import asyncio
import inspect
import pathlib

import pytest

import mini_loop.fake_llm as fake
from mini_loop.fake_llm import FakeAsyncAnthropic, FakeMessage, FakeUsage

PACKAGE = pathlib.Path(__file__).resolve().parent.parent / "mini_loop"

#: Local names for values that came from the provider.
PROVIDER_NAMES = {"response", "resp", "stream", "delta", "usage"}

#: Deliberately narrow. A first version scanned the whole package by name and
#: reported `resp.headers` (an HTTP error response), `stream.write` (a file) and
#: `usage.model_dump` (an optional probe) as gaps. A check that cries wolf gets
#: switched off, so it is scoped to the files that actually hold a provider
#: object -- and widening it is a deliberate act, not an accident of naming.
PROVIDER_MODULES = ("agent.py", "transport.py", "metering.py", "compaction.py",
                    "memory.py")


def _reads() -> dict[str, set[str]]:
    """Attributes read off a provider value.

    A defaulted `getattr(x, "y", None)` **counts**. The first version of this
    treated a default as "optional" and the check went nearly empty: this
    codebase reads defensively everywhere, so `response.usage` and `delta.text`
    -- the two shapes whose absence actually caused bugs -- were both excluded
    by that rule. Tolerating absence at the call site is not permission for the
    double to omit it; it is what makes the omission *silent*, which is the
    failure mode every one of these rounds found.

    Only `hasattr`-guarded reads are excluded, because those are explicit
    "if you have it" branches over genuine alternatives -- `_usage_payload`
    probing `model_dump` then `to_dict` then `__dict__`.
    """

    found: dict[str, set[str]] = {name: set() for name in PROVIDER_NAMES}
    for name in PROVIDER_MODULES:
        path = PACKAGE / name
        if not path.exists():
            continue
        tree = ast.parse(path.read_text())
        optional: set[tuple[str, str]] = set()
        for node in ast.walk(tree):
            if (isinstance(node, ast.Call)
                    and getattr(node.func, "id", None) in {"getattr", "hasattr"}
                    and len(node.args) >= 2
                    and isinstance(node.args[0], ast.Name)
                    and isinstance(node.args[1], ast.Constant)):
                if node.func.id == "hasattr":
                    optional.add((node.args[0].id, node.args[1].value))
        for node in ast.walk(tree):
            if (isinstance(node, ast.Attribute) and isinstance(node.value, ast.Name)
                    and node.value.id in PROVIDER_NAMES
                    and (node.value.id, node.attr) not in optional):
                found[node.value.id].add(node.attr)
            if (isinstance(node, ast.Call)
                    and getattr(node.func, "id", None) == "getattr"
                    and len(node.args) >= 2
                    and isinstance(node.args[0], ast.Name)
                    and node.args[0].id in PROVIDER_NAMES
                    and isinstance(node.args[1], ast.Constant)
                    and (node.args[0].id, node.args[1].value) not in optional):
                found[node.args[0].id].add(node.args[1].value)
    return found


def _stream():
    return fake._FakeStream(FakeAsyncAnthropic(), {"messages": [], "model": "m"})


SUBJECTS = {
    "response": [lambda: FakeMessage([], "end_turn")],
    "resp": [lambda: FakeMessage([], "end_turn")],
    # `metering._field` reads a dict *or* an object on purpose, so both are
    # legitimate shapes and `usage.get` is not a gap in the double.
    "usage": [lambda: FakeUsage(1), lambda: {"input_tokens": 1}],
    "stream": [_stream],
    "delta": [lambda: fake._FakeDelta("x", "text"),
              lambda: fake._FakeDelta("x", "thinking")],
}


@pytest.mark.parametrize("name", sorted(PROVIDER_NAMES))
def test_the_double_answers_every_read(name):
    """A new read of a provider object fails here, not in production."""
    wanted = _reads()[name]
    if not wanted:
        pytest.skip(f"nothing is read off `{name}`")
    # A delta carries exactly one of `.text` / `.thinking` by design, so the
    # requirement is that *some* variant answers each read, not that one does.
    subjects = [factory() for factory in SUBJECTS[name]]
    missing = sorted(
        attr for attr in wanted
        if not any(hasattr(subject, attr) for subject in subjects)
    )
    assert not missing, (
        f"the package reads {missing} off `{name}`; the offline model has no "
        f"such attribute, so those paths are exercised by nothing"
    )


def test_the_scan_actually_finds_something():
    """A scan that silently matches nothing would pass every case above."""
    found = _reads()
    assert found["response"], "no reads of `response` found -- the scan broke"
    assert found["stream"], "no reads of `stream` found -- streaming is unscanned"
    assert "get_final_message" in found["stream"]


# --- the client's own surface --------------------------------------------

def _client_methods() -> set[str]:
    used: set[str] = set()
    for path in sorted(PACKAGE.rglob("*.py")):
        for node in ast.walk(ast.parse(path.read_text())):
            if not isinstance(node, ast.Call):
                continue
            rendered = ast.unparse(node.func)
            # `client.messages.x`, not `kwargs['messages'].append` -- the first
            # version matched the latter and reported `append` and `extend` as
            # missing client methods.
            if rendered.startswith(("self.client.messages.", "client.messages.",
                                    "agent.client.messages.")):
                used.add(rendered.rsplit(".", 1)[-1])
    return used


def test_every_client_method_the_package_calls_exists_on_the_double():
    used = _client_methods()
    assert used, "no client calls found -- the scan broke"
    messages = FakeAsyncAnthropic().messages
    missing = sorted(name for name in used if not hasattr(messages, name))
    assert not missing, f"the package calls messages.{missing} and the double has none"


# --- shapes the live endpoint was observed to return ---------------------

def test_a_message_carries_what_a_live_one_carried():
    """Observed against the real endpoint in round 30, not copied from types."""
    message = FakeMessage([], "end_turn")
    for field in ("id", "type", "role", "model", "content", "stop_reason",
                  "stop_sequence", "stop_details", "container", "usage"):
        assert hasattr(message, field), field


def test_usage_carries_what_a_live_one_carried():
    usage = FakeUsage(10)
    for field in ("input_tokens", "output_tokens", "cache_read_input_tokens",
                  "cache_creation_input_tokens", "service_tier"):
        assert hasattr(usage, field), field


def test_a_tool_use_block_carries_what_a_live_one_carried():
    block = fake.ToolUseBlock("run_bash", {"command": "x"}, "t1")
    for field in ("type", "id", "name", "input", "caller"):
        assert hasattr(block, field), field


# --- deltas discriminate, which is what round 41 turned on ---------------

@pytest.mark.parametrize("field", ["text", "thinking"])
def test_a_delta_carries_exactly_one_body_field(field):
    delta = fake._FakeDelta("body", field)
    assert getattr(delta, field) == "body"
    assert delta.type == f"{field}_delta"
    other = "thinking" if field == "text" else "text"
    assert not hasattr(delta, other), (
        "a delta carrying both fields makes thinking and answer text "
        "indistinguishable to every consumer"
    )


def test_streaming_a_thinking_block_yields_thinking_deltas():
    client = FakeAsyncAnthropic(
        responder=lambda request: ([fake.thinking("REASONING")], "end_turn"),
        thinking=False,
    )

    async def collect():
        kinds = []
        async with client.messages.stream(
            model="m", max_tokens=10, messages=[{"role": "user", "content": "hi"}]
        ) as stream:
            async for event in stream:
                kinds.append(event.delta.type)
        return kinds

    assert set(asyncio.run(collect())) == {"thinking_delta"}
