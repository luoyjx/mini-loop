"""SessionManager composition for token-efficiency and semantic-code tools."""

import asyncio
from pathlib import Path

import pytest

from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.manager import SessionManager
from mini_loop.registry import Tool, ToolRegistry
from mini_loop.token_efficiency import (
    ConciseResponsePolicy,
    DeterministicLosslessReducer,
    RawArtifactStoreError,
    OptimizationMode,
)
from mini_loop.token_tools import RAW_ARTIFACT_TOOL


SKILLS = Path(__file__).resolve().parent.parent / "skills"
AST_TOOLS = {"repo_map", "file_outline", "show_symbol", "symbol_references"}


def _settings(tmp_path, **over):
    return Settings(
        fake_llm=True,
        workspace_root=tmp_path / "ws",
        skills_dir=SKILLS,
        **over,
    )


async def _noop(ctx):
    return "ok"


def test_manager_builds_an_explicit_off_runtime_by_default(tmp_path):
    manager = SessionManager(_settings(tmp_path), FakeAsyncAnthropic())

    runtime = manager.token_efficiency
    assert runtime is manager.harness.token_efficiency
    assert runtime.default_mode is OptimizationMode.OFF
    assert runtime.raw_store is None
    assert runtime.components.observation_reducers == ()
    assert runtime.components.response_policies == ()


def test_selected_builtins_are_registered_without_package_discovery(tmp_path):
    manager = SessionManager(
        _settings(
            tmp_path,
            token_efficiency_mode="shadow",
            token_efficiency_response_style="concise",
        ),
        FakeAsyncAnthropic(),
    )

    runtime = manager.token_efficiency
    assert runtime.default_mode is OptimizationMode.SHADOW
    assert isinstance(
        runtime.components.observation_reducers[0].component,
        DeterministicLosslessReducer,
    )
    policy = runtime.components.response_policies[0].component
    assert isinstance(policy, ConciseResponsePolicy)
    assert policy.settings.require_opt_in is False


def test_enforce_mode_does_not_widen_a_caller_owned_catalogue(tmp_path):
    caller_registry = ToolRegistry()

    manager = SessionManager(
        _settings(tmp_path, token_efficiency_mode="enforce"),
        FakeAsyncAnthropic(),
        tool_registry=caller_registry,
    )

    assert manager.tool_registry is caller_registry
    assert RAW_ARTIFACT_TOOL not in caller_registry
    session = manager.create()
    assert RAW_ARTIFACT_TOOL not in session.agent.tools
    assert session.agent.token_efficiency.raw_store is None


def test_manager_scopes_and_revokes_in_memory_recovery_stores(tmp_path):
    manager = SessionManager(
        _settings(tmp_path, token_efficiency_mode="enforce"),
        FakeAsyncAnthropic(),
    )
    session = manager.create()
    store = session.agent.token_efficiency.raw_store
    assert store is not None
    pointer = store.put_masked("masked authority")

    asyncio.run(manager.stop())

    assert store.closed
    with pytest.raises(RawArtifactStoreError, match="closed"):
        store.get_masked(pointer.ref)


def test_delete_revokes_recovery_and_readonly_session_never_gets_it(tmp_path):
    manager = SessionManager(
        _settings(tmp_path, token_efficiency_mode="enforce"),
        FakeAsyncAnthropic(),
    )
    readonly = manager.create(permission_mode="readonly")
    assert readonly.agent.state["permission_mode"] == "readonly"
    assert readonly.agent.token_efficiency.raw_store is None

    writable = manager.create()
    store = writable.agent.token_efficiency.raw_store
    assert store is not None
    assert manager.delete(writable.id, remove_workspace=False)
    assert store.closed


def test_injected_runtime_and_role_policy_are_preserved(tmp_path):
    runtime = object()

    class FalseyRolePolicy:
        def __bool__(self):
            return False

    role_policy = FalseyRolePolicy()

    manager = SessionManager(
        _settings(tmp_path),
        FakeAsyncAnthropic(),
        token_efficiency=runtime,
        role_tool_policy=role_policy,
    )

    assert manager.token_efficiency is runtime
    assert manager.harness.token_efficiency is runtime
    assert manager.role_tool_policy is role_policy
    assert manager.harness.role_tool_policy is role_policy


def test_ast_tools_are_installed_on_a_private_clone(tmp_path):
    caller_registry = ToolRegistry(
        [
            Tool(
                "caller_tool",
                "caller owned",
                {"type": "object", "properties": {}},
                _noop,
            )
        ]
    )

    manager = SessionManager(
        _settings(
            tmp_path,
            ast_outline_enabled=True,
            ast_outline_binary="/definitely/missing/ast-outline",
            ast_outline_sha256="a" * 64,
        ),
        FakeAsyncAnthropic(),
        tool_registry=caller_registry,
    )

    assert caller_registry.names() == ["caller_tool"]
    assert manager.tool_registry is not caller_registry
    assert set(manager.tool_registry.names()) == {"caller_tool", *AST_TOOLS}
    for name in AST_TOOLS:
        tool = manager.tool_registry.get(name)
        assert tool is not None
        assert tool.readonly is True
        assert tool.parallel_safe is True


def test_ast_tools_follow_workflow_installation(tmp_path):
    manager = SessionManager(
        _settings(
            tmp_path,
            ast_outline_enabled=True,
            ast_outline_binary="/definitely/missing/ast-outline",
            ast_outline_sha256="a" * 64,
        ),
        FakeAsyncAnthropic(),
        enable_workflows=True,
    )

    names = manager.tool_registry.names()
    assert names.index("WorkflowCancel") < names.index("repo_map")
    assert AST_TOOLS <= set(names)


class _LifecycleProbe:
    def __init__(self, *, fail_close=False):
        self.events = []
        self.fail_close = fail_close
        self.initialize_report = object()
        self.close_report = object()

    async def initialize(self, services):
        self.events.append(("initialize", services))
        return self.initialize_report

    async def close(self):
        self.events.append(("close", None))
        if self.fail_close:
            raise RuntimeError("component close failed")
        return self.close_report


def test_manager_runs_token_efficiency_lifecycle(tmp_path):
    probe = _LifecycleProbe()
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), token_efficiency=probe
    )

    asyncio.run(manager.start())
    asyncio.run(manager.stop())

    assert probe.events == [("initialize", None), ("close", None)]
    assert manager._token_efficiency_initialize_report is probe.initialize_report
    assert manager._token_efficiency_close_report is probe.close_report


def test_manager_token_efficiency_lifecycle_is_idempotent(tmp_path):
    probe = _LifecycleProbe()
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), token_efficiency=probe
    )

    asyncio.run(manager.start())
    asyncio.run(manager.start())
    asyncio.run(manager.stop())
    asyncio.run(manager.stop())

    assert probe.events == [("initialize", None), ("close", None)]
    with pytest.raises(RuntimeError, match="manager is stopped"):
        manager.create()
    with pytest.raises(RuntimeError, match="manager is stopped"):
        manager.restore_scheduled_session("scheduled")


def test_bad_component_close_does_not_prevent_manager_cleanup(tmp_path):
    probe = _LifecycleProbe(fail_close=True)
    manager = SessionManager(
        _settings(tmp_path), FakeAsyncAnthropic(), token_efficiency=probe
    )
    cleaned = []

    async def stop_cron():
        cleaned.append("cron")

    manager.cron.stop = stop_cron
    asyncio.run(manager.stop())

    assert cleaned == ["cron"]
    assert "RuntimeError" in manager._token_efficiency_close_error
