import asyncio
import json
from pathlib import Path

from mini_loop.actions import InMemoryActionJournal
from mini_loop.agent import Agent, _message_protocol_shape
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, system_text, text
from mini_loop.registry import Tool, ToolCall, ToolRegistry
from mini_loop.secrets import MASK, SecretRegistry
from mini_loop.token_efficiency import (
    ComponentDescriptor,
    ComponentStage,
    ConciseResponsePolicy,
    ConciseResponsePolicySettings,
    Lossiness,
    MaskedObservation,
    MaskedRawArtifactStore,
    ObservationReduction,
    OptimizationMode,
    RequestContext,
    RequestOptimization,
    TokenEfficiencyRegistry,
)
from mini_loop.token_tools import RAW_ARTIFACT_TOOL, install_token_efficiency_tools


SKILLS = Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path, **overrides):
    values = {
        "fake_llm": True,
        "workspace_root": tmp_path / "ws",
        "skills_dir": SKILLS,
        "token_efficiency_raw_min_bytes": 1,
    }
    values.update(overrides)
    return Settings(**values)


def _agent(tmp_path, *, runtime, tools=None, secrets=None, responder=None):
    events = []

    async def emit(event):
        events.append(event)

    agent = Agent(
        client=FakeAsyncAnthropic(
            responder=responder or (lambda _kwargs: ([text("done")], "end_turn"))
        ),
        settings=_settings(tmp_path),
        workspace=tmp_path / "ws" / "session",
        tools=tools,
        secrets=secrets,
        token_efficiency=runtime,
        emit=emit,
    )
    return agent, events


class _MaskAssertingReducer:
    descriptor = ComponentDescriptor(
        id="mask-asserting",
        version="1",
        stage=ComponentStage.OBSERVATION,
        content_types=("text/*",),
        lossiness=Lossiness.RECOVERABLE,
        recoverable=True,
    )

    def __init__(self, forbidden):
        self.forbidden = forbidden
        self.seen = []

    async def reduce(self, observation, **_kwargs):
        assert self.forbidden not in observation.content
        self.seen.append(observation.content)
        return ObservationReduction("compact projection")


async def _echo_secret(_ctx, value):
    return f"prefix {value} suffix " + ("detail " * 100)


def _observation_runtime(component, mode):
    registry = TokenEfficiencyRegistry()
    registry.register_observation(component)
    return registry.runtime(default_mode=mode)


def test_observation_reducer_runs_after_mask_and_records_recoverable_projection(tmp_path):
    secret = "sk-super-secret-canary-123456"
    secrets = SecretRegistry()
    secrets.register("API_TOKEN", secret)
    reducer = _MaskAssertingReducer(secret)
    tools = ToolRegistry(
        [
            Tool(
                "echo_secret",
                "echo",
                {
                    "type": "object",
                    "properties": {"value": {"type": "string"}},
                    "required": ["value"],
                },
                _echo_secret,
                readonly=True,
                risk="read",
            )
        ]
    )
    install_token_efficiency_tools(tools)
    agent, events = _agent(
        tmp_path,
        runtime=_observation_runtime(reducer, OptimizationMode.ENFORCE),
        tools=tools,
        secrets=secrets,
    )
    journal = InMemoryActionJournal()
    agent.state["action_journal"] = journal

    output = asyncio.run(
        agent._exec_tool(ToolCall("echo_secret", {"value": secret}, "tool-1"))
    )

    authoritative = f"prefix {MASK} suffix " + ("detail " * 100)
    assert reducer.seen == [authoritative]
    assert output.endswith("compact projection")
    assert RAW_ARTIFACT_TOOL in output
    receipts = [
        event
        for event in events
        if event.get("type") == "optimization_receipt"
        and event.get("stage") == "observation"
    ]
    assert receipts and receipts[0]["status"] == "applied"
    raw_ref = receipts[0]["raw_ref"]
    assert agent.token_efficiency.raw_store.get_masked(raw_ref) == authoritative
    assert receipts[0]["projected_bytes"] == len(output.encode("utf-8"))
    assert receipts[0]["warning_count"] == 0
    assert "warnings" not in receipts[0]
    assert "input_digest" not in receipts[0]
    assert "output_digest" not in receipts[0]
    assert "raw_digest" not in receipts[0]
    action_id = next(e["action_id"] for e in events if e.get("type") == "tool_use")
    assert journal.get(action_id).result == authoritative
    assert secret not in json.dumps(events, default=str)
    assert secret not in output


class _SecretEmittingReducer:
    descriptor = ComponentDescriptor(
        id="secret-emitting",
        version="1",
        stage=ComponentStage.OBSERVATION,
        content_types=("text/*",),
        lossiness=Lossiness.LOSSLESS,
    )

    def __init__(self, secret):
        self.secret = secret

    async def reduce(self, _observation, **_kwargs):
        return ObservationReduction(self.secret, warnings=(self.secret,))


def test_reducer_output_and_warning_cannot_reintroduce_a_secret(tmp_path):
    secret = "sk-reconstructed-secret-123456789"
    secrets = SecretRegistry()
    secrets.register("API_TOKEN", secret)
    tools = ToolRegistry(
        [
            Tool(
                "long_output",
                "long",
                {"type": "object", "properties": {}},
                lambda _ctx: "safe " * 200,
                readonly=True,
                risk="read",
            )
        ]
    )
    agent, events = _agent(
        tmp_path,
        runtime=_observation_runtime(
            _SecretEmittingReducer(secret), OptimizationMode.ENFORCE
        ),
        tools=tools,
        secrets=secrets,
    )

    output = asyncio.run(agent._exec_tool(ToolCall("long_output", {}, "tool-1")))

    receipt = next(e for e in events if e.get("type") == "optimization_receipt")
    assert output == MASK
    assert receipt["warning_count"] == 1
    assert "warnings" not in receipt
    assert "input_digest" not in receipt
    assert "output_digest" not in receipt
    assert "raw_digest" not in receipt
    assert secret not in json.dumps(events, default=str)


def test_recovery_envelope_must_be_smaller_than_authoritative_output(tmp_path):
    reducer = _MaskAssertingReducer("never-present")
    tools = ToolRegistry(
        [
            Tool(
                "short_output",
                "short",
                {"type": "object", "properties": {}},
                lambda _ctx: "short authoritative output",
                readonly=True,
                risk="read",
            )
        ]
    )
    install_token_efficiency_tools(tools)
    agent, events = _agent(
        tmp_path,
        runtime=_observation_runtime(reducer, OptimizationMode.ENFORCE),
        tools=tools,
    )

    output = asyncio.run(agent._exec_tool(ToolCall("short_output", {}, "tool-1")))

    receipt = next(e for e in events if e.get("type") == "optimization_receipt")
    assert output == "short authoritative output"
    assert receipt["status"] == "degraded"
    assert receipt["reason"] == "recovery_envelope_inflation"
    assert receipt["raw_ref"] is None
    assert receipt["projected_bytes"] == len(output.encode("utf-8"))


def test_shadow_observation_is_measured_without_projection_or_raw_write(tmp_path):
    reducer = _MaskAssertingReducer("never-present")
    tools = ToolRegistry(
        [
            Tool(
                "echo_secret",
                "echo",
                {"type": "object", "properties": {}},
                lambda _ctx: "full observation",
                readonly=True,
                risk="read",
            )
        ]
    )
    agent, events = _agent(
        tmp_path,
        runtime=_observation_runtime(reducer, OptimizationMode.SHADOW),
        tools=tools,
    )

    output = asyncio.run(agent._exec_tool(ToolCall("echo_secret", {}, "tool-1")))

    assert output == "full observation"
    assert agent.token_efficiency.raw_store is None
    assert RAW_ARTIFACT_TOOL not in agent.tools
    receipt = next(e for e in events if e.get("type") == "optimization_receipt")
    assert receipt["status"] == "shadowed"
    assert receipt["raw_ref"] is None


def test_recoverable_reducer_fails_open_when_raw_storage_is_unavailable(tmp_path):
    reducer = _MaskAssertingReducer("never-present")
    runtime = _observation_runtime(reducer, OptimizationMode.ENFORCE)
    authoritative = "full observation " * 20
    outcome = asyncio.run(
        runtime.reduce_observation(
            MaskedObservation(authoritative),
            persist_masked_raw=False,
        )
    )

    assert outcome.observation.content == authoritative
    assert outcome.observation.reduced_by == ()
    assert outcome.receipts[0].status.value == "degraded"
    assert outcome.receipts[0].reason == "recovery_unavailable"


def test_readonly_agent_does_not_create_or_write_raw_sidecars(tmp_path):
    reducer = _MaskAssertingReducer("never-present")
    settings = _settings(tmp_path)
    workspace = tmp_path / "ws" / "readonly"
    agent = Agent(
        client=FakeAsyncAnthropic(),
        settings=settings,
        workspace=workspace,
        token_efficiency=_observation_runtime(
            reducer, OptimizationMode.ENFORCE
        ),
        state={"permission_mode": "readonly"},
    )

    assert agent.token_efficiency.raw_store is None
    assert not (workspace / ".token-efficiency").exists()
    assert RAW_ARTIFACT_TOOL not in agent.tools


def test_agent_rebinds_a_raw_store_from_a_different_workspace(tmp_path):
    foreign = tmp_path / "foreign"
    local = tmp_path / "ws" / "local"
    foreign.mkdir()
    local.mkdir(parents=True)
    runtime = _observation_runtime(
        _MaskAssertingReducer("never-present"), OptimizationMode.ENFORCE
    ).with_raw_store(MaskedRawArtifactStore(foreign))

    agent = Agent(
        client=FakeAsyncAnthropic(),
        settings=_settings(tmp_path),
        workspace=local,
        token_efficiency=runtime,
    )

    assert agent.token_efficiency.raw_store is not runtime.raw_store
    assert agent.token_efficiency.raw_store.workspace == local.resolve()


def test_child_constructor_does_not_widen_a_role_filtered_registry(tmp_path):
    workspace = tmp_path / "ws" / "shared"
    workspace.mkdir(parents=True)
    runtime = _observation_runtime(
        _MaskAssertingReducer("never-present"), OptimizationMode.ENFORCE
    )
    parent = Agent(
        client=FakeAsyncAnthropic(),
        settings=_settings(tmp_path),
        workspace=workspace,
        token_efficiency=runtime,
    )
    assert RAW_ARTIFACT_TOOL in parent.tools
    authoritative = "worker authority " * 100
    parent.tools.register(
        Tool(
            "read_long",
            "read",
            {"type": "object", "properties": {}},
            lambda _ctx: authoritative,
            readonly=True,
            risk="read",
            capabilities=frozenset({"repo.read"}),
        )
    )
    filtered = parent.tools.with_capabilities({"repo.read"})
    assert RAW_ARTIFACT_TOOL not in filtered

    child = Agent(
        client=FakeAsyncAnthropic(),
        settings=_settings(tmp_path),
        workspace=workspace,
        tools=filtered,
        token_efficiency=parent.token_efficiency,
        depth=1,
    )

    assert RAW_ARTIFACT_TOOL not in child.tools
    output = asyncio.run(child._exec_tool(ToolCall("read_long", {}, "tool-1")))
    assert output == authoritative


class _NewestMessageOptimizer:
    descriptor = ComponentDescriptor(
        id="newest-message",
        version="1",
        stage=ComponentStage.REQUEST_CONTEXT,
    )

    async def optimize(self, context: RequestContext, **_kwargs):
        request = dict(context.request)
        messages = [dict(message) for message in request["messages"]]
        messages[-1]["content"] = "x"
        request["messages"] = messages
        return RequestOptimization(request)


def test_request_optimizer_changes_only_provider_copy_after_frozen_prefix(tmp_path):
    requests = []

    def responder(kwargs):
        requests.append(kwargs)
        return [text("done")], "end_turn"

    registry = TokenEfficiencyRegistry()
    registry.register_request_optimizer(_NewestMessageOptimizer())
    runtime = registry.runtime(default_mode=OptimizationMode.ENFORCE)
    agent, events = _agent(tmp_path, runtime=runtime, responder=responder)
    agent.messages[:] = [
        {"role": "user", "content": "frozen first"},
        {"role": "assistant", "content": "frozen answer"},
        {"role": "user", "content": "live newest"},
    ]

    asyncio.run(agent._create(agent.messages, purpose="agent_turn"))

    assert [message["content"] for message in agent.messages] == [
        "frozen first",
        "frozen answer",
        "live newest",
    ]
    assert requests[0]["messages"][-1]["content"] == "x"
    assert any(
        event.get("type") == "optimization_receipt"
        and event.get("stage") == "request_context"
        and event.get("status") == "applied"
        for event in events
    )


def test_request_projection_remains_byte_stable_when_tail_becomes_prefix(tmp_path):
    requests = []

    def responder(kwargs):
        requests.append(kwargs)
        return [text("done")], "end_turn"

    registry = TokenEfficiencyRegistry()
    registry.register_request_optimizer(_NewestMessageOptimizer())
    agent, _events = _agent(
        tmp_path,
        runtime=registry.runtime(default_mode=OptimizationMode.ENFORCE),
        responder=responder,
    )
    agent.messages.append({"role": "user", "content": "first live delta"})

    asyncio.run(agent._create(agent.messages, purpose="agent_turn"))
    first_projection = json.dumps(
        requests[0]["messages"][0], sort_keys=True, separators=(",", ":")
    )
    agent.messages.extend(
        [
            {"role": "assistant", "content": "authoritative answer"},
            {"role": "user", "content": "second live delta"},
        ]
    )
    asyncio.run(agent._create(agent.messages, purpose="agent_turn"))
    second_prefix = json.dumps(
        requests[1]["messages"][0], sort_keys=True, separators=(",", ":")
    )

    assert first_projection == second_prefix
    assert requests[0]["messages"][0]["content"] == "x"
    assert agent.messages[0]["content"] == "first live delta"


class _DropThinkingOptimizer:
    descriptor = ComponentDescriptor(
        id="drop-thinking",
        version="1",
        stage=ComponentStage.REQUEST_CONTEXT,
    )

    async def optimize(self, context: RequestContext, **_kwargs):
        request = dict(context.request)
        messages = [dict(message) for message in request["messages"]]
        messages[-1]["content"] = [
            block
            for block in messages[-1]["content"]
            if block.get("type") != "thinking"
        ]
        request["messages"] = messages
        return RequestOptimization(request)


def test_newest_assistant_is_frozen_including_thinking_and_signature(tmp_path):
    requests = []

    def responder(kwargs):
        requests.append(kwargs)
        return [text("done")], "end_turn"

    registry = TokenEfficiencyRegistry()
    registry.register_request_optimizer(_DropThinkingOptimizer())
    agent, events = _agent(
        tmp_path,
        runtime=registry.runtime(default_mode=OptimizationMode.ENFORCE),
        responder=responder,
    )
    messages = [
        {
            "role": "assistant",
            "content": [
                {"type": "thinking", "thinking": "private", "signature": "sig"},
                {"type": "text", "text": "visible"},
            ],
        }
    ]

    asyncio.run(agent._create(messages, purpose="agent_turn"))

    assert requests[0]["messages"] == messages
    receipt = next(
        event for event in events if event.get("type") == "optimization_receipt"
    )
    assert receipt["status"] == "degraded"
    assert receipt["reason"] == "frozen_prefix_guard"


def test_protocol_shape_fingerprints_full_tool_use_payload():
    original = [
        {
            "role": "assistant",
            "content": [
                {
                    "type": "tool_use",
                    "id": "tool-id",
                    "name": "lookup",
                    "input": {"query": "long query that should stay stable"},
                }
            ],
        }
    ]
    mutated = [
        {
            "role": "assistant",
            "content": [
                {
                    "type": "tool_use",
                    "id": "tool-id",
                    "name": "lookup",
                    "input": {},
                }
            ],
        }
    ]

    assert _message_protocol_shape(original) != _message_protocol_shape(mutated)


class _PairBreakingOptimizer:
    descriptor = ComponentDescriptor(
        id="pair-breaker",
        version="1",
        stage=ComponentStage.REQUEST_CONTEXT,
    )

    async def optimize(self, context: RequestContext, **_kwargs):
        request = dict(context.request)
        messages = [dict(message) for message in request["messages"]]
        blocks = [dict(block) for block in messages[-1]["content"]]
        blocks[0]["tool_use_id"] = "diff-id"
        messages[-1]["content"] = blocks
        request["messages"] = messages
        return RequestOptimization(request)


def test_request_optimizer_cannot_break_tool_result_pairing(tmp_path):
    requests = []

    def responder(kwargs):
        requests.append(kwargs)
        return [text("done")], "end_turn"

    registry = TokenEfficiencyRegistry()
    registry.register_request_optimizer(_PairBreakingOptimizer())
    agent, events = _agent(
        tmp_path,
        runtime=registry.runtime(default_mode=OptimizationMode.ENFORCE),
        responder=responder,
    )
    messages = [
        {
            "role": "assistant",
            "content": [
                {"type": "tool_use", "id": "same-id", "name": "x", "input": {}}
            ],
        },
        {
            "role": "user",
            "content": [
                {"type": "tool_result", "tool_use_id": "same-id", "content": "ok"}
            ],
        },
    ]

    asyncio.run(agent._create(messages, purpose="agent_turn"))

    result = requests[0]["messages"][-1]["content"][0]
    assert result["tool_use_id"] == "same-id"
    assert any(
        event.get("type") == "request_optimization_rejected" for event in events
    )


def test_nonzero_command_exit_is_structured_and_marks_tool_failed(tmp_path):
    runtime = TokenEfficiencyRegistry().runtime()
    agent, events = _agent(tmp_path, runtime=runtime)

    output = asyncio.run(
        agent._exec_tool(
            ToolCall("bash", {"command": "echo broken >&2; exit 7"}, "tool-1")
        )
    )

    assert "broken" in output
    event = next(e for e in events if e.get("type") == "tool_result")
    assert event["error"] is True
    assert event["command_result"]["exit_code"] == 7
    assert event["command_result"]["timed_out"] is False


def test_concise_response_policy_changes_live_turn_not_side_queries(tmp_path):
    requests = []

    def responder(kwargs):
        requests.append(kwargs)
        return [text("done")], "end_turn"

    registry = TokenEfficiencyRegistry()
    registry.register_response_policy(
        ConciseResponsePolicy(
            ConciseResponsePolicySettings(require_opt_in=False)
        )
    )
    agent, _events = _agent(
        tmp_path,
        runtime=registry.runtime(default_mode=OptimizationMode.ENFORCE),
        responder=responder,
    )
    messages = [{"role": "user", "content": "Explain the result"}]

    asyncio.run(
        agent._create(messages, system="stable", purpose="agent_turn")
    )
    asyncio.run(
        agent._create(messages, system="stable", purpose="compaction")
    )

    assert requests[0]["max_tokens"] == 1_200
    assert "Answer concisely" in system_text(requests[0])
    assert requests[1]["max_tokens"] == agent.settings.max_tokens
    assert system_text(requests[1]) == "stable"
