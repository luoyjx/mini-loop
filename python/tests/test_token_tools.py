import asyncio

from mini_loop.registry import ToolCall, ToolContext, ToolRegistry
from mini_loop.token_efficiency import (
    MaskedObservation,
    MaskedRawArtifactStore,
    ObservationOutcome,
)
from mini_loop.token_tools import (
    MAX_RECOVERY_CHARS,
    RAW_ARTIFACT_TOOL,
    install_token_efficiency_tools,
    render_recovery_marker,
)


def test_recovery_tool_is_read_only_and_inheritable():
    registry = install_token_efficiency_tools(ToolRegistry())

    tool = registry.get(RAW_ARTIFACT_TOOL)
    assert tool is not None
    assert tool.readonly and tool.parallel_safe and tool.risk == "read"
    assert tool.capabilities == frozenset({"observation.recover"})


def test_recovery_tool_resolves_only_through_the_agents_scoped_store(tmp_path):
    first = tmp_path / "first"
    second = tmp_path / "second"
    first.mkdir()
    second.mkdir()
    first_store = MaskedRawArtifactStore(first)
    second_store = MaskedRawArtifactStore(second)
    pointer = first_store.put_masked("masked full output")

    class Runtime:
        raw_store = second_store

    class Agent:
        token_efficiency = Runtime()

    call = ToolCall(RAW_ARTIFACT_TOOL, {"raw_ref": pointer.ref}, "id")
    ctx = ToolContext(Agent(), second, {}, call)
    tool = install_token_efficiency_tools(ToolRegistry()).get(RAW_ARTIFACT_TOOL)

    denied = asyncio.run(tool.run(ctx, raw_ref=pointer.ref))
    assert denied.startswith("Error:")
    Runtime.raw_store = first_store
    recovered = asyncio.run(tool.run(ctx, raw_ref=pointer.ref))
    assert recovered.endswith("\nmasked full output")
    assert "chars=0:18/18" in recovered


def test_recovery_tool_pages_with_a_hard_fifty_thousand_character_cap(tmp_path):
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    store = MaskedRawArtifactStore(
        workspace,
        max_artifact_bytes=MAX_RECOVERY_CHARS * 3,
        max_total_bytes=MAX_RECOVERY_CHARS * 3,
    )
    content = "x" * (MAX_RECOVERY_CHARS + 17)
    pointer = store.put_masked(content)

    class Runtime:
        raw_store = store

    class Agent:
        token_efficiency = Runtime()

    call = ToolCall(RAW_ARTIFACT_TOOL, {"raw_ref": pointer.ref}, "id")
    ctx = ToolContext(Agent(), workspace, {}, call)
    tool = install_token_efficiency_tools(ToolRegistry()).get(RAW_ARTIFACT_TOOL)

    first = asyncio.run(
        tool.run(ctx, raw_ref=pointer.ref, limit=MAX_RECOVERY_CHARS * 2)
    )
    first_payload = first.split("\n", 1)[1]
    first_end = len(first_payload)
    assert len(first.encode("utf-8")) <= MAX_RECOVERY_CHARS
    assert first_payload == "x" * first_end
    assert f"continue with offset={first_end}" in first
    second = asyncio.run(
        tool.run(ctx, raw_ref=pointer.ref, offset=first_end)
    )
    assert second.endswith("\n" + "x" * (len(content) - first_end))
    assert "continue with" not in second

    unicode_pointer = store.put_masked("🙂" * 20_000)
    unicode_page = asyncio.run(tool.run(ctx, raw_ref=unicode_pointer.ref))
    assert len(unicode_page.encode("utf-8")) <= MAX_RECOVERY_CHARS
    assert "continue with offset=" in unicode_page


def test_recovery_marker_is_only_added_for_applied_recoverable_projection():
    unchanged = ObservationOutcome(MaskedObservation("same"))
    assert render_recovery_marker(unchanged) == ""

    reduced = ObservationOutcome(
        MaskedObservation(
            "short",
            reduced_by=("fold",),
            raw_ref="raw_" + "a" * 43,
            raw_digest="sha256:" + "b" * 64,
        )
    )
    marker = render_recovery_marker(reduced)
    assert RAW_ARTIFACT_TOOL in marker
    assert "raw_" + "a" * 43 in marker
    assert "masked full output" not in marker
