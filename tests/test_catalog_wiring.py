import asyncio
from pathlib import Path

from mini_loop.agent import Agent
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, system_text, text
from mini_loop.registry import Tool


SKILLS = Path(__file__).resolve().parent.parent / "skills"


async def _ok(_ctx, **_kwargs):
    return "ok"


def _agent(tmp_path, responder) -> Agent:
    settings = Settings(
        fake_llm=True,
        workspace_root=tmp_path / "ws",
        skills_dir=SKILLS,
    )
    return Agent(
        client=FakeAsyncAnthropic(responder=responder),
        settings=settings,
        workspace=settings.workspace_root / "session",
    )


def test_system_prompt_and_provider_tools_share_one_catalogue_snapshot(tmp_path):
    requests = []

    def responder(kwargs):
        requests.append(kwargs)
        return [text("done")], "end_turn"

    agent = _agent(tmp_path, responder)
    late = Tool(
        "late_tool",
        "registered while the prompt is built",
        {"type": "object", "properties": {}},
        _ok,
        readonly=True,
        risk="read",
        capabilities=frozenset({"repo.read"}),
    )

    def builder(current):
        catalog = current._request_tool_catalog
        if catalog is None:
            return "catalog=initial"
        names = ",".join(catalog.sent_names)
        if "late_tool" not in current.tools:
            current.tools.register(late)
        return f"catalog={names}"

    agent.use_system_builder(builder)
    assert asyncio.run(agent.run("hello")) == "done"

    request = requests[0]
    sent = [schema["name"] for schema in request["tools"]]
    described = system_text(request).removeprefix("catalog=").split(",")
    assert sent == described
    assert "late_tool" not in sent


def test_explore_subagent_inherits_parent_semantic_tools_by_capability(tmp_path):
    child_catalogues = []

    def responder(kwargs):
        if "Explore subagent" in system_text(kwargs):
            child_catalogues.append([tool["name"] for tool in kwargs.get("tools", [])])
        return [text("reported")], "end_turn"

    agent = _agent(tmp_path, responder)
    agent.tools.register(
        Tool(
            "semantic_probe",
            "semantic read",
            {"type": "object", "properties": {}},
            _ok,
            readonly=True,
            risk="read",
            capabilities=frozenset({"repo.semantic_outline"}),
        )
    )

    assert asyncio.run(agent._run_subagent("inspect", "Explore")) == "reported"
    assert child_catalogues
    names = child_catalogues[0]
    assert names == ["read_file", "glob", "semantic_probe"]
    assert "bash" not in names
    assert "write_file" not in names
