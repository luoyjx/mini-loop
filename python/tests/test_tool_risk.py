"""Risk lives on the tool contract, and the permission layer executes it.

Before round 95 the only oversight an MCP tool had was a name heuristic --
`"deploy" in call.name` -- and the only risk metadata on the contract was an
advisory `readonly` copied from the *server's own* readOnlyHint. Measured
through a real turn with default hooks:

    mcp__ghsrv__delete_repository(repo="prod/main")
        handler executed:   yes
        permission events:  NONE
        contract said:      readonly=True   (the server's claim, taken as truth)

OpenWorker classifies every tool READ / WRITE_LOCAL / EXEC / EXTERNAL and its
own review flags the fallback for *unregistered* tools -- READ, fail-open --
as a standing hazard (docs/OPENWORKER_RESEARCH.md 9.2.8). This adopts the
ladder and inverts the fallback: `Tool.risk` is one of read/write/exec/
external, MCP tools are pinned `external` regardless of what the server
claims, and an unclassified tool is gated like `external`, never like `read`.
A claim written by the untrusted side of a boundary must not lower what the
boundary enforces.
"""

import asyncio
import pathlib

import pytest

from mini_loop.agent import Agent
from mini_loop.builtins import full_registry
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.permissions import PermissionHook, default_hooks
from mini_loop.registry import RISK_LEVELS, Hooks, Tool
from mini_loop.skills import SkillLoader

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _mcp_delete_tool(ran):
    async def delete_repository(ctx, repo):
        ran.append(repo)
        return f"deleted {repo}"

    # What register_mcp used to produce: server self-certifies read-only.
    return Tool("mcp__ghsrv__delete_repository",
                "[mcp:ghsrv] Delete a repository permanently.",
                {"type": "object", "properties": {"repo": {"type": "string"}}},
                delete_repository, readonly=True, risk="external")


def _turn(tmp_path, registry, call, *, hooks=None):
    events = []

    async def emit(e):
        events.append(e)

    client = FakeAsyncAnthropic(responder=scripted([
        ([call], "tool_use"),
        ([text("done")], "end_turn"),
    ]))
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    ws = settings.workspace_root / "s"
    ws.mkdir(parents=True, exist_ok=True)
    agent = Agent(client=client, settings=settings, workspace=ws,
                  skills=SkillLoader(SKILLS), tools=registry, emit=emit,
                  **({"hooks": hooks} if hooks is not None else {}))
    asyncio.run(agent.run("go"))
    return events


def _permission_events(events):
    return [e for e in events if e["type"] == "permission"]


# -- the ladder is executed -------------------------------------------------


def test_an_external_tool_is_denied_without_an_approval_path(tmp_path):
    """The probe that found the hole, kept as the guard that closes it."""

    ran = []
    registry = full_registry()
    registry.register(_mcp_delete_tool(ran))

    events = _turn(tmp_path, registry,
                   tool("mcp__ghsrv__delete_repository", repo="prod/main"))

    assert not ran, "the external tool ran with no approval path"
    denied = _permission_events(events)
    assert denied and denied[0]["rule"] == "external-action"


def test_an_approved_external_tool_runs(tmp_path):
    """The gate is an approval seam, not a wall: not vacuous in either
    direction."""

    ran = []
    registry = full_registry()
    registry.register(_mcp_delete_tool(ran))
    asked = []

    async def approval(ctx, call, rule):
        asked.append(rule.name)
        return True

    events = _turn(tmp_path, registry,
                   tool("mcp__ghsrv__delete_repository", repo="prod/main"),
                   hooks=default_hooks(approval=approval))

    assert ran == ["prod/main"]
    assert asked == ["external-action"]
    assert _permission_events(events)[0]["decision"] == "allow"


def test_an_unclassified_tool_gates_upward_not_downward(tmp_path):
    """OpenWorker's hazard inverted: no claim means external, not read."""

    ran = []

    async def mystery(ctx):
        ran.append(True)
        return "ok"

    registry = full_registry()
    registry.register(Tool("mystery", "No risk declared.",
                           {"type": "object", "properties": {}}, mystery))

    events = _turn(tmp_path, registry, tool("mystery"))

    assert not ran, "an unclassified tool ran unchallenged"
    assert _permission_events(events)[0]["rule"] == "unclassified-tool"


def test_ordinary_built_ins_are_not_over_gated(tmp_path):
    """read/write/exec built-ins keep their existing behaviour; a ladder that
    gates everything is a wall, and walls get torn down."""

    registry = full_registry()
    events = _turn(tmp_path, registry, tool("bash", command="echo hello"))

    assert not _permission_events(events)
    result = [e for e in events if e["type"] == "tool_result"]
    assert result and "hello" in result[0]["_trajectory_fields"]["output"]


def test_every_shipped_tool_declares_its_risk():
    """The sweep, kept executable: a new built-in cannot ship unclassified --
    it would gate as external and the author would meet this test first.

    Composed the way the manager composes it, not just `full_registry()`:
    the first version checked only full_registry() and the three workflow
    tools -- installed separately by SessionManager -- shipped unclassified
    anyway. Checking an enumeration instead of the composition path is the
    round-80 mistake with a new surface.
    """

    from mini_loop.workflows.tools import install_workflows

    registry = install_workflows(full_registry())
    unclassified = [n for n in registry.names() if registry.get(n).risk is None]

    assert not unclassified, (
        f"shipped tools with no risk declaration: {unclassified}"
    )


def test_a_typoed_risk_is_rejected_at_registration():
    """A typo must not silently become 'unclassified but gated stricter'."""

    registry = full_registry()
    with pytest.raises(ValueError, match="unknown risk"):
        registry.register(Tool("typo", "d", {"type": "object", "properties": {}},
                               lambda ctx: "x", risk="extrenal"))


def test_the_ladder_is_closed():
    """The rules key on exact strings; a fifth level nobody gates is a hole."""

    assert RISK_LEVELS == ("read", "write", "exec", "external")


def test_shipped_readonly_agrees_with_risk():
    """`readonly` and `risk` both encode "does this mutate"; two sources of
    truth drift (round 104 found load_skill and ask_user diverged). For the
    built-ins they must agree: readonly iff risk == "read". MCP tools are
    exempt -- there `readonly` is the server's untrusted hint, kept advisory
    while `risk` is pinned external (round 95), so they diverge on purpose."""

    from mini_loop.workflows.tools import install_workflows

    registry = install_workflows(full_registry())
    drift = []
    for name in registry.names():
        if name.startswith("mcp__"):
            continue
        tool = registry.get(name)
        if bool(tool.readonly) != (tool.risk == "read"):
            drift.append((name, tool.risk, tool.readonly))

    assert not drift, f"readonly disagrees with risk: {drift}"


# -- the server's word is not the boundary's word ---------------------------


def test_mcp_risk_is_pinned_external_whatever_the_server_claims(tmp_path):
    """register_mcp: readOnlyHint stays advisory; risk does not come from the
    untrusted side."""

    from mini_loop.mcp import register_mcp

    class HintClient:
        name = "ghsrv"

        async def list_tools(self):
            return [{
                "name": "delete_repository",
                "description": "Delete a repository permanently.",
                "input_schema": {"type": "object", "properties": {}},
                "annotations": {"readOnlyHint": True},
            }]

    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS)
    ws = settings.workspace_root / "s"
    ws.mkdir(parents=True, exist_ok=True)
    agent = Agent(client=FakeAsyncAnthropic(), settings=settings, workspace=ws,
                  skills=SkillLoader(SKILLS), tools=full_registry())
    added = asyncio.run(register_mcp(agent, HintClient()))

    registered = agent.tools.get(added[0])
    assert registered.risk == "external"
    assert registered.readonly is True  # advisory metadata is kept as metadata
