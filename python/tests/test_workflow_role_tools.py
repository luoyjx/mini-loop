from pathlib import Path

from mini_loop.builtins import default_registry
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.harness import Harness
from mini_loop.registry import Tool
from mini_loop.tool_policy import DEFAULT_ROLE_TOOL_POLICY
from mini_loop.workflows.runner import FreshAgentRunner


SKILLS = Path(__file__).resolve().parent.parent / "skills"


async def _semantic(_ctx):
    return "outline"


def test_workflow_worker_inherits_semantic_reads_without_write_or_exec(tmp_path):
    parent = default_registry()
    parent.register(
        Tool(
            "semantic_probe",
            "semantic read",
            {"type": "object", "properties": {}},
            _semantic,
            readonly=True,
            risk="read",
            capabilities=frozenset({"repo.semantic_outline"}),
        )
    )
    settings = Settings(
        fake_llm=True,
        workspace_root=tmp_path / "ws",
        skills_dir=SKILLS,
    )
    runner = FreshAgentRunner(
        client=FakeAsyncAnthropic(),
        settings=settings,
        workspace=settings.workspace_root,
        context_resolver=lambda _attempt: None,
        harness=Harness(
            tools=parent,
            role_tool_policy=DEFAULT_ROLE_TOOL_POLICY,
        ),
    )

    registry = runner._worker_registry()

    assert registry.names() == ["read_file", "glob", "semantic_probe"]
    assert "bash" not in registry
    assert "write_file" not in registry
