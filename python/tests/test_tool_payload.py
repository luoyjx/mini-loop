"""Tool definitions are sent on every request, so their total has to be bounded.

Round 90's transferable lesson was that whichever half of a store is paid *per
request* is the half that needs a bound, and that this is a question to put to
every store rather than a fact about memory. Tool definitions are the largest
per-request payload the harness controls, and they had the same inversion:
round 40 capped each MCP description at 4,000 characters and left the count
alone.

    baseline (37 built-ins)      8,537 chars    ~2,134 tokens
     50 extra tools            222,485 chars   ~55,621 tokens
    200 extra tools            851,835 chars  ~212,958 tokens
    500 extra tools          2,110,635 chars  ~527,658 tokens

Past a point this stops being a cost problem and becomes a hard failure: the
request exceeds the context window and *every* turn fails, with a provider error
that says nothing about which tool caused it. Connecting a handful of MCP
servers reaches these numbers easily.

Descriptions are trimmed before any tool is dropped. A tool with a short
description is still callable; an absent one is a capability the model cannot
use. Dropping is the last resort and is reported.

Round 93: reported to the *model* too. The system prompt built its "Tools
available" line from `names()` -- the registry inventory -- while the request
carried `schemas()`, the budget-fitted subset. On a 3,037-tool registry the
model was affirmatively told it had 2,508 tools whose definitions it had never
seen, and the name list itself was a second per-request payload (~62KB of
system prompt) that round 91's budget never touched. The prompt now enumerates
`sent_names()` and names the omission, the way Claude Code's deferred-tools
reminder names tools absent from the request.
"""

import asyncio
import json
from pathlib import Path

import pytest

from mini_loop.agent import Agent
from mini_loop.builtins import full_registry
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, system_text, text
from mini_loop.prompts import MAX_OMITTED_NAMED
from mini_loop.registry import (
    MAX_TOOL_PAYLOAD,
    TOOL_DESCRIPTION_STEPS,
    Tool,
    ToolRegistry,
)
from mini_loop.skills import SkillLoader

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _registry(extra, *, description="D" * 4_000):
    registry = full_registry()
    for i in range(extra):
        registry.register(Tool(
            name=f"mcp__srv__tool_{i}",
            description=description,
            input_schema={"type": "object", "properties": {}},
            handler=lambda ctx, **kwargs: "ok",
        ))
    return registry


def _size(schemas):
    return len(json.dumps(schemas, default=str))


def test_an_ordinary_registry_is_untouched():
    """Not vacuous: the budget must not fire on the built-ins."""

    registry = full_registry()
    schemas = registry.schemas()

    assert _size(schemas) < MAX_TOOL_PAYLOAD
    assert not registry.problems
    # Unmodified, not merely long: several built-ins have descriptions shorter
    # than the smallest trim step, so "longer than 80 characters" would fail on
    # untouched output and pass on trimmed output for the wrong reason.
    original = {name: registry.get(name).description for name in registry.names()}
    assert {s["name"]: s["description"] for s in schemas} == original


@pytest.mark.parametrize("extra", [50, 200, 500, 3_000])
def test_the_payload_stays_within_budget(extra):
    registry = _registry(extra)
    assert _size(registry.schemas()) <= MAX_TOOL_PAYLOAD


def test_descriptions_are_trimmed_before_a_tool_is_dropped():
    """A short description is still callable; an absent tool is not."""

    registry = _registry(200)
    schemas = registry.schemas()

    assert len(schemas) == 200 + len(full_registry().names())
    assert any("trimmed" in p for p in registry.problems.summary())
    assert not any("omitted" in p for p in registry.problems.summary())


def test_names_and_schemas_are_never_truncated():
    """A trimmed description still describes a callable tool; a trimmed name
    or schema describes a tool the model cannot call correctly."""

    registry = _registry(200)
    names = {s["name"] for s in registry.schemas()}

    assert all(f"mcp__srv__tool_{i}" in names for i in range(0, 200, 37))
    for schema in registry.schemas():
        assert schema["input_schema"] == {"type": "object", "properties": {}} or schema["input_schema"]


def test_dropping_is_a_last_resort_and_is_reported():
    registry = _registry(3_000, description="")
    schemas = registry.schemas()

    assert len(schemas) < 3_000
    assert any("omitted" in p for p in registry.problems.summary())


def test_a_dropped_tool_is_counted_not_just_implied():
    registry = _registry(3_000)
    before = len(registry.names())
    kept = len(registry.schemas())

    reported = [p for p in registry.problems.summary() if "omitted" in p]
    assert reported
    assert str(before - kept) in reported[0], (
        f"the report does not say how many were dropped: {reported[0]}"
    )


# --- what the model is told (round 93) --------------------------------------


def _request_with(registry, tmp_path):
    """One agent turn against the fake; returns the request the model saw."""

    captured = []

    def responder(kwargs):
        captured.append(kwargs)
        return [text("ok")], "end_turn"

    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS_DIR)
    ws = settings.workspace_root / "sess"
    ws.mkdir(parents=True, exist_ok=True)
    agent = Agent(client=FakeAsyncAnthropic(responder=responder),
                  settings=settings, workspace=ws,
                  skills=SkillLoader(SKILLS_DIR), tools=registry)
    asyncio.run(agent.run("hello"))
    return captured[0]


def _available_line(system: str) -> set[str]:
    return set(system.split("Tools available: ")[1].split("\n")[0].split(", "))


def test_the_system_prompt_lists_only_sent_tools(tmp_path):
    """The prompt describes the request, not the registry. A name listed as
    available whose definition was never sent is an affirmative false claim."""

    registry = _registry(3_000, description="")
    request = _request_with(registry, tmp_path)

    sent = {schema["name"] for schema in request["tools"]}
    dropped = set(registry.names()) - sent
    listed = _available_line(system_text(request["system"]))

    assert dropped, "not a drop scenario; the test would be vacuous"
    assert listed == sent
    assert not listed & dropped


def test_the_model_is_told_what_was_omitted(tmp_path):
    registry = _registry(3_000, description="")
    request = _request_with(registry, tmp_path)

    system = system_text(request["system"])
    dropped = set(registry.names()) - {s["name"] for s in request["tools"]}

    assert "NOT included in this request" in system
    assert f"{len(dropped)} registered tool(s)" in system


def test_the_omission_notice_is_bounded(tmp_path):
    """Naming every dropped tool would rebuild the payload the budget removed,
    one channel over. Names are capped; the rest collapses to a count."""

    registry = _registry(3_000, description="")
    request = _request_with(registry, tmp_path)

    system = system_text(request["system"])
    notice = system[system.index("NOT included"):]
    dropped = len(set(registry.names()) - {s["name"] for s in request["tools"]})

    assert notice.count("mcp__srv__") <= MAX_OMITTED_NAMED
    assert f"and {dropped - MAX_OMITTED_NAMED} more" in notice
    # The whole prompt, names line included, must stay far below the old
    # behaviour, where the registry inventory alone was ~62KB.
    assert len(system) < MAX_TOOL_PAYLOAD // 2


def test_no_notice_when_nothing_is_omitted(tmp_path):
    """A line that is always there is a line that stops being read -- and a
    trimmed tool is still callable, so trimming alone must not raise it."""

    for registry in (full_registry(), _registry(200)):
        request = _request_with(registry, tmp_path)
        system = system_text(request["system"])

        assert "NOT included in this request" not in system
        assert _available_line(system) == set(registry.names())


def test_the_pure_queries_agree_with_the_request():
    """`sent_names()` drives the prompt and `schemas()` drives the request;
    if they diverge the prompt lies again, one refactor from now."""

    for registry in (full_registry(), _registry(200), _registry(3_000, description="")):
        sent = registry.sent_names()
        omitted = registry.omitted_names()

        assert sent == [schema["name"] for schema in registry.schemas()]
        assert set(omitted) == set(registry.names()) - set(sent)


def test_the_pure_queries_do_not_report_problems():
    """The builder calls them on every request build; if they logged, every
    turn would double-report what `schemas()` already said once."""

    registry = _registry(3_000, description="")
    registry.sent_names()
    registry.omitted_names()

    assert not registry.problems
