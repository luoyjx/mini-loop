"""Integration boundaries for the experimental Dynamic Workflow surface."""

import asyncio
import json
from pathlib import Path
from types import SimpleNamespace

import pytest

from mini_loop.actions import ActionJournalConflict, InMemoryActionJournal
from mini_loop.config import Settings
from mini_loop.events import WorkflowEvent
from mini_loop.fake_llm import system_text, FakeAsyncAnthropic, scripted, text, tool
from mini_loop.manager import SessionManager
from mini_loop.registry import ToolCall
from mini_loop.run_context import RunContext
from mini_loop.session import AgentSession
from mini_loop.workflows import (
    BudgetPolicy,
    NodeKind,
    ToolPolicy,
    WorkflowDefinition,
    WorkflowService,
    WorkflowNode,
    workflow_injector,
)


SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path, **overrides) -> Settings:
    return Settings(
        workspace_root=tmp_path / "workspaces",
        skills_dir=SKILLS_DIR,
        **overrides,
    )


def test_workflows_are_default_off_even_when_comprehensive_features_are_enabled(
    tmp_path, monkeypatch
):
    monkeypatch.delenv("MINILOOP_EXPERIMENTAL_WORKFLOWS", raising=False)
    monkeypatch.setenv("MINILOOP_FEATURES", "all")

    settings = _settings(tmp_path)

    assert settings.enable_features is True
    assert settings.enable_workflows is False
    assert settings.workflow_max_concurrent_agents == 4
    assert settings.workflow_max_agents == 32
    assert settings.workflow_max_rounds == 4
    assert settings.workflow_wall_time_seconds == 900.0


@pytest.mark.parametrize(
    ("overrides", "message"),
    [
        (
            {"workflow_max_concurrent_agents": 0},
            "workflow_max_concurrent_agents must be at least 1",
        ),
        (
            {"workflow_max_concurrent_agents": 5},
            "workflow_max_concurrent_agents must not exceed 4",
        ),
        (
            {
                "workflow_max_concurrent_agents": 4,
                "workflow_max_agents": 3,
            },
            "workflow_max_agents must be greater than or equal to "
            "workflow_max_concurrent_agents",
        ),
        (
            {"workflow_max_rounds": 0},
            "workflow_max_rounds must be at least 1",
        ),
        (
            {"workflow_max_agents": 33},
            "workflow_max_agents must not exceed 32",
        ),
        (
            {"workflow_wall_time_seconds": 0},
            "workflow_wall_time_seconds must be positive",
        ),
    ],
)
def test_workflow_limits_are_validated(tmp_path, overrides, message):
    with pytest.raises(ValueError, match=message):
        _settings(tmp_path, **overrides)


def _event(**overrides) -> WorkflowEvent:
    values = {
        "kind": "workflow_started",
        "session_id": "session-1",
        "run_id": "wfrun-1",
        "workflow_name": "repo-audit",
        "definition_revision": "wfdef-1",
    }
    values.update(overrides)
    return WorkflowEvent(**values)


@pytest.mark.parametrize(
    ("overrides", "message"),
    [
        (
            {"kind": "not_a_workflow_event"},
            "unsupported workflow event kind",
        ),
        (
            {"session_id": ""},
            "session_id is required",
        ),
        (
            {"run_id": ""},
            "run_id is required",
        ),
        (
            {"workflow_name": ""},
            "workflow_name is required",
        ),
        (
            {"definition_revision": ""},
            "definition_revision is required",
        ),
        (
            {"kind": "workflow_phase_started"},
            "workflow_phase_started requires phase_id",
        ),
        (
            {"kind": "workflow_node_claimed"},
            "workflow_node_claimed requires node_id",
        ),
        (
            {
                "kind": "workflow_agent_started",
                "node_id": "audit",
            },
            "workflow_agent_started requires attempt_id",
        ),
        (
            {
                "kind": "workflow_agent_started",
                "node_id": "audit",
                "attempt_id": "attempt-1",
            },
            "workflow_agent_started requires agent_id",
        ),
        (
            {"payload_version": 2},
            "unsupported workflow event payload_version",
        ),
    ],
)
def test_workflow_event_rejects_invalid_correlation(overrides, message):
    with pytest.raises(ValueError, match=message):
        _event(**overrides)


def test_agent_session_emit_owns_sequence_and_session_envelope(tmp_path):
    async def main():
        session = AgentSession("session-1", tmp_path / "workspace")
        subscriber = session.subscribe(replay=False)
        event = _event(
            kind="workflow_agent_progress",
            phase_id="audit-phase",
            node_id="audit-node",
            attempt_id="attempt-1",
            agent_id="agent-1",
            parent_agent_id="lead",
            payload={"completed": 2, "total": 5},
        )

        await session.emit(event.as_session_event())
        first = await subscriber.get()
        await session.emit(
            _event(
                kind="workflow_completed",
                payload={"artifact_id": "artifact-1"},
            ).as_session_event()
        )
        second = await subscriber.get()
        session.unsubscribe(subscriber)
        return first, second

    first, second = asyncio.run(main())

    assert first["type"] == "workflow_agent_progress"
    assert first["seq"] == 1
    assert first["sequence"] == 1
    assert second["seq"] == 2
    assert first["session"] == second["session"] == "session-1"
    assert first["session_id"] == "session-1"
    assert isinstance(first["ts"], float)
    assert isinstance(first["occurred_at"], float)
    assert first["event_id"].startswith("wfevt_")
    assert first["kind"] == first["type"]
    assert first["run_id"] == first["workflow_run_id"] == "wfrun-1"
    assert first["node_id"] == "audit-node"
    assert first["attempt_id"] == "attempt-1"
    assert first["agent_id"] == "agent-1"
    assert first["parent_agent_id"] == "lead"
    assert first["payload"] == {"completed": 2, "total": 5}
    assert second["type"] == "workflow_completed"


def test_action_journal_binds_immutable_payload_and_workflow():
    journal = InMemoryActionJournal()
    fields = {
        "action_id": "act-1",
        "session_id": "session-1",
        "message_id": "message-1",
        "tool_use_id": "tool-1",
        "tool_name": "Workflow",
        "input_value": {"definition": {"name": "audit"}, "args": {"question": "q"}},
    }

    first = journal.begin(**fields)
    assert journal.begin(**fields) == first
    bound = journal.attach_workflow("act-1", "wfrun-1")
    assert bound.workflow_run_id == "wfrun-1"
    completed = journal.finish("act-1", status="completed", result="launched")
    assert completed.status == "completed"
    assert completed.workflow_run_id == "wfrun-1"

    with pytest.raises(ActionJournalConflict, match="different payload"):
        journal.begin(**{**fields, "input_value": {"args": {"question": "other"}}})
    with pytest.raises(ActionJournalConflict, match="already bound"):
        journal.attach_workflow("act-1", "wfrun-2")


def test_injected_workflow_service_shares_manager_action_journal(tmp_path):
    settings = _settings(tmp_path, fake_llm=True)
    journal = InMemoryActionJournal()
    service = WorkflowService(
        settings=settings,
        client=FakeAsyncAnthropic(),
        action_journal=journal,
        session_resolver=lambda _session_id: None,
    )
    manager = SessionManager(
        settings,
        FakeAsyncAnthropic(),
        workflow_service=service,
    )

    assert manager.actions is journal
    assert manager.actions is service.action_journal
    assert manager.workflow_attempt_semaphore is service.attempt_semaphore
    asyncio.run(manager.stop())


def _repo_audit_definition() -> WorkflowDefinition:
    audit_schema = {
        "type": "object",
        "properties": {
            "claim_id": {"type": "string"},
            "target": {"type": "string"},
            "summary": {"type": "string"},
        },
        "required": ["claim_id", "target", "summary"],
        "additionalProperties": False,
    }
    verdict_schema = {
        "type": "object",
        "properties": {
            "status": {
                "type": "string",
                "enum": ["verified", "refuted", "unverified"],
            },
            "claim_id": {"type": "string"},
            "evidence": {"type": "array", "items": {"type": "string"}},
            "reason": {"type": "string"},
        },
        "required": ["status", "claim_id", "evidence", "reason"],
        "additionalProperties": False,
    }
    final_schema = {
        "type": "object",
        "properties": {
            "verified": {"type": "array", "items": {"type": "string"}},
            "unverified": {"type": "array", "items": {"type": "string"}},
        },
        "required": ["verified", "unverified"],
        "additionalProperties": False,
    }
    return WorkflowDefinition(
        name="repo-audit-mvp",
        description="Explicit bounded fan-out, independent verify, and reduce.",
        input_schema={
            "type": "object",
            "properties": {"question": {"type": "string"}},
            "required": ["question"],
            "additionalProperties": False,
        },
        output_schema=final_schema,
        budget=BudgetPolicy(
            max_concurrent_agents=2,
            max_agents=8,
            max_rounds=3,
            wall_time_seconds=10,
        ),
        policy=ToolPolicy(allowed_tools=("read_file", "glob")),
        nodes=(
            WorkflowNode(
                "discover",
                NodeKind.AGENT,
                prompt_template="Discover the targets for args.question.",
                output_schema={
                    "type": "object",
                    "properties": {
                        "targets": {
                            "type": "array",
                            "items": {"type": "string"},
                        }
                    },
                    "required": ["targets"],
                    "additionalProperties": False,
                },
            ),
            WorkflowNode(
                "audit_a",
                NodeKind.AGENT,
                needs=("discover",),
                prompt_template="Audit target a.py.",
                output_schema=audit_schema,
            ),
            WorkflowNode(
                "audit_b",
                NodeKind.AGENT,
                needs=("discover",),
                prompt_template="Audit target b.py.",
                output_schema=audit_schema,
            ),
            WorkflowNode(
                "verify_a",
                NodeKind.VERIFY,
                needs=("audit_a",),
                prompt_template="Independently verify audit_a.",
                output_schema=verdict_schema,
            ),
            WorkflowNode(
                "verify_b",
                NodeKind.VERIFY,
                needs=("audit_b",),
                prompt_template="Independently verify audit_b.",
                output_schema=verdict_schema,
            ),
            WorkflowNode(
                "reduce",
                NodeKind.REDUCE,
                needs=("verify_a", "verify_b"),
                prompt_template="Reduce verdicts without dropping unverified claims.",
                output_schema=final_schema,
            ),
        ),
        return_from="reduce",
    )


def _workflow_responder(definition: WorkflowDefinition, observed_results: list[str]):
    artifacts = {
        "discover": {"targets": ["a.py", "b.py"]},
        "audit_a": {
            "claim_id": "claim-a",
            "target": "a.py",
            "summary": "a is covered",
        },
        "audit_b": {
            "claim_id": "claim-b",
            "target": "b.py",
            "summary": "b is covered",
        },
        "verify_a": {
            "status": "verified",
            "claim_id": "claim-a",
            "evidence": ["a.py:1"],
            "reason": "confirmed",
        },
        "verify_b": {
            "status": "verified",
            "claim_id": "claim-b",
            "evidence": ["b.py:1"],
            "reason": "confirmed",
        },
        "reduce": {"verified": ["claim-a", "claim-b"], "unverified": []},
    }

    def responder(kwargs):
        messages = kwargs["messages"]
        last = messages[-1]["content"]
        system = system_text(kwargs)
        if system.startswith("You are an isolated read-only workflow worker."):
            first_prompt = next(
                item["content"]
                for item in messages
                if item["role"] == "user" and isinstance(item["content"], str)
            )
            node_id = first_prompt.split("Workflow node: ", 1)[1].split(" ", 1)[0]
            if isinstance(last, str):
                return [
                    tool(
                        "return_artifact",
                        _id=f"artifact-{node_id}",
                        value=artifacts[node_id],
                    )
                ], "tool_use"
            return [text(f"{node_id} complete")], "end_turn"

        if isinstance(last, str) and last == "run the repo audit workflow":
            return [
                tool(
                    "Workflow",
                    _id="workflow-launch-1",
                    definition=definition.to_dict(),
                    args={"question": "audit the repository"},
                )
            ], "tool_use"
        if isinstance(last, str) and last.startswith("<workflow-results"):
            observed_results.append(last)
            return [text("received workflow result")], "end_turn"
        if isinstance(last, list):
            return [text("workflow launched")], "end_turn"
        return [text("ordinary turn")], "end_turn"

    return responder


def test_manager_keeps_workflow_surface_explicitly_default_off(tmp_path):
    settings = _settings(tmp_path, fake_llm=True, enable_workflows=True)
    default_manager = SessionManager(
        settings,
        FakeAsyncAnthropic(),
        enable_features=True,
    )
    default_session = default_manager.create()
    assert default_manager.enable_workflows is False
    assert default_manager.workflows is None
    assert "Workflow" not in default_session.agent.tools

    enabled_manager = SessionManager(
        settings,
        FakeAsyncAnthropic(),
        enable_workflows=True,
    )
    enabled_session = enabled_manager.create()
    assert enabled_manager.enable_workflows is True
    assert enabled_manager.workflows is not None
    assert enabled_session.agent.tools.names()[-3:] == [
        "Workflow",
        "WorkflowStatus",
        "WorkflowCancel",
    ]


def test_untrusted_parent_cannot_launch_workflow(tmp_path):
    definition = _repo_audit_definition()
    observed = []

    async def main():
        manager = SessionManager(
            _settings(tmp_path, fake_llm=True),
            FakeAsyncAnthropic(
                responder=_workflow_responder(definition, observed)
            ),
            enable_workflows=True,
        )
        session = manager.create()
        untrusted_final = await session.run("run the repo audit workflow")
        no_approval_final = await session.run(
            "run the repo audit workflow",
            run_context=RunContext.explicit_human(actor_id="local-user"),
        )
        runs = manager.workflows.store.list_runs(session_id=session.id)
        await manager.stop()
        return untrusted_final, no_approval_final, runs

    untrusted_final, no_approval_final, runs = asyncio.run(main())
    assert untrusted_final == "workflow launched"
    assert no_approval_final == "workflow launched"
    assert runs == []
    assert observed == []


def test_workflow_status_requires_fresh_capability_and_session_ownership(tmp_path):
    definition = _single_node_definition(wall_time_seconds=5)

    async def main():
        manager = SessionManager(
            _settings(tmp_path, fake_llm=True),
            FakeAsyncAnthropic(
                responder=scripted([
                    (
                        [
                            tool(
                                "return_artifact",
                                value={"value": "owned"},
                            )
                        ],
                        "tool_use",
                    )
                ])
            ),
            enable_workflows=True,
        )
        owner = manager.create()
        stranger = manager.create()
        launch_context = RunContext.explicit_human(
            approved_capabilities=("workflow.launch",),
        )
        launched = await manager.workflows.launch(
            session_id=owner.id,
            definition=definition,
            args={},
            run_context=launch_context,
            action_id="managed-status-action",
            action_input={"definition": definition.to_dict(), "args": {}},
        )
        await manager.workflows.wait(launched.run_id)
        no_approval = await owner.agent._exec_tool(
            ToolCall(
                "WorkflowStatus",
                {"run_id": launched.run_id},
                "status-without-approval",
            ),
            run_context=launch_context.with_new_message(),
        )
        manage_context = launch_context.with_new_message(
            approved_capabilities=("workflow.manage",),
        )
        allowed = await owner.agent._exec_tool(
            ToolCall(
                "WorkflowStatus",
                {"run_id": launched.run_id},
                "status-owner",
            ),
            run_context=manage_context,
        )
        foreign = await stranger.agent._exec_tool(
            ToolCall(
                "WorkflowStatus",
                {"run_id": launched.run_id},
                "status-stranger",
            ),
            run_context=manage_context.with_new_message(
                approved_capabilities=("workflow.manage",),
            ),
        )
        await manager.stop()
        return no_approval, allowed, foreign

    no_approval, allowed, foreign = asyncio.run(main())
    assert "requires per-message workflow.manage approval" in no_approval
    assert json.loads(allowed)["status"] == "COMPLETED"
    assert "not found" in foreign


def test_trusted_parent_runs_workflow_and_injects_result_once(tmp_path):
    definition = _repo_audit_definition()
    observed_results = []
    events = []

    async def main():
        settings = _settings(
            tmp_path,
            fake_llm=True,
            workflow_max_concurrent_agents=2,
            workflow_max_agents=8,
            workflow_max_rounds=3,
            workflow_wall_time_seconds=10,
        )
        manager = SessionManager(
            settings,
            FakeAsyncAnthropic(
                responder=_workflow_responder(definition, observed_results),
                delay=0.005,
            ),
            enable_workflows=True,
            event_sink=events.append,
        )
        session = manager.create(system="fixed parent system")
        context = RunContext.explicit_human(
            actor_id="local-user",
            approved_capabilities=("workflow.launch",),
        )

        first = await session.run(
            "run the repo audit workflow",
            run_context=context,
        )
        [run] = manager.workflows.store.list_runs(session_id=session.id)
        finished = await manager.workflows.wait(run.run_id)
        status = manager.workflows.status(run.run_id, session_id=session.id)
        info = session.info()
        replayed = await manager.workflows.launch(
            session_id=session.id,
            definition=definition.to_dict(),
            args={"question": "audit the repository"},
            run_context=context,
            action_id=run.launch_action_id,
            launch_turn=1,
            action_input={
                "definition": definition.to_dict(),
                "args": {"question": "audit the repository"},
            },
            tool_use_id="workflow-launch-1",
        )
        with pytest.raises(ActionJournalConflict, match="different payload"):
            await manager.workflows.launch(
                session_id=session.id,
                definition=definition.to_dict(),
                args={"question": "different payload"},
                run_context=context,
                action_id=run.launch_action_id,
                launch_turn=1,
                action_input={
                    "definition": definition.to_dict(),
                    "args": {"question": "different payload"},
                },
                tool_use_id="workflow-launch-1",
            )

        second = await session.run(
            "show the completed result",
            run_context=context.with_new_message(),
        )
        third = await session.run(
            "ordinary follow-up",
            run_context=context.with_new_message(),
        )
        outbox = manager.workflows.store.list_outbox(
            run_id=run.run_id,
        )
        action = manager.actions.get(run.launch_action_id)
        runs = manager.workflows.store.list_runs(session_id=session.id)
        await manager.stop()
        return (
            first,
            second,
            third,
            finished,
            status,
            info,
            outbox,
            action,
            replayed,
            runs,
        )

    (
        first,
        second,
        third,
        finished,
        status,
        info,
        outbox,
        action,
        replayed,
        runs,
    ) = asyncio.run(main())

    assert first == "workflow launched"
    assert second == "received workflow result"
    assert third == "ordinary turn"
    assert finished.status.value == "COMPLETED"
    assert status["result"] == {
        "verified": ["claim-a", "claim-b"],
        "unverified": [],
    }
    assert info["workflows"][0]["status"] == "COMPLETED"
    assert len(observed_results) == 1
    parsed = json.loads(observed_results[0].splitlines()[2])
    assert parsed[0]["result"] == status["result"]
    assert len(outbox) == 1
    assert outbox[0].delivered_at is not None
    assert action.workflow_run_id == finished.run_id
    assert action.status == "completed"
    assert replayed.reused is True
    assert replayed.run_id == finished.run_id
    assert len(runs) == 1

    event_types = [event["type"] for event in events]
    assert "workflow_planned" in event_types
    assert "workflow_started" in event_types
    assert "workflow_agent_progress" in event_types
    assert "workflow_verdict_recorded" in event_types
    assert "workflow_completed" in event_types
    assert "workflow_result_enqueued" in event_types
    sequences = [event["seq"] for event in events]
    assert sequences == sorted(sequences)
    assert len(sequences) == len(set(sequences))


def _single_node_definition(*, wall_time_seconds: float) -> WorkflowDefinition:
    schema = {
        "type": "object",
        "properties": {"value": {"type": "string"}},
        "required": ["value"],
        "additionalProperties": False,
    }
    return WorkflowDefinition(
        name="slow-readonly",
        input_schema={"type": "object"},
        output_schema=schema,
        budget=BudgetPolicy(
            max_concurrent_agents=1,
            max_agents=1,
            max_rounds=1,
            wall_time_seconds=wall_time_seconds,
        ),
        nodes=(
            WorkflowNode(
                "slow",
                NodeKind.AGENT,
                prompt_template="Return a structured value.",
                output_schema=schema,
            ),
        ),
        return_from="slow",
    )


def test_dynamic_launch_canonicalizes_model_controlled_provenance(tmp_path):
    definition = _single_node_definition(wall_time_seconds=5)
    payload = definition.to_dict()
    payload.update({
        "source": "plugin",
        "source_version": "forged-source",
        "definition_id": "forged-id",
        "revision": "forged-revision",
        "definition_hash": "forged-hash",
        "parent_revision": "forged-parent",
    })

    async def main():
        manager = SessionManager(
            _settings(tmp_path, fake_llm=True),
            FakeAsyncAnthropic(
                responder=scripted([
                    (
                        [
                            tool(
                                "return_artifact",
                                value={"value": "canonical"},
                            )
                        ],
                        "tool_use",
                    )
                ])
            ),
            enable_workflows=True,
        )
        session = manager.create()
        context = RunContext.explicit_human(
            approved_capabilities=("workflow.launch",),
        )
        launched = await manager.workflows.launch(
            session_id=session.id,
            definition=payload,
            args={},
            run_context=context,
            action_id="canonical-action",
            action_input={"definition": payload, "args": {}},
        )
        run = manager.workflows.store.get_run(launched.run_id)
        stored = manager.workflows.store.get_definition(
            run.definition_revision
        )
        await manager.stop()
        return stored

    stored = asyncio.run(main())
    assert stored.source.value == "dynamic"
    assert stored.source_version is None
    assert stored.parent_revision is None
    assert stored.definition_id.startswith("wf_")
    assert stored.definition_id != "forged-id"
    assert stored.revision.startswith("wfdef_")
    assert stored.revision != "forged-revision"
    assert stored.definition_hash != "forged-hash"


def test_observability_failure_does_not_change_workflow_state(tmp_path):
    definition = _single_node_definition(wall_time_seconds=5)
    started_state = []
    manager_holder = {}

    def sink(event):
        if event["type"] == "workflow_started":
            run = manager_holder["manager"].workflows.store.get_run(
                event["run_id"]
            )
            started_state.append(run.started_at)
            raise RuntimeError("started sink failed")
        if event["type"] == "workflow_agent_progress":
            raise RuntimeError("progress sink failed")

    async def main():
        manager = SessionManager(
            _settings(tmp_path, fake_llm=True),
            FakeAsyncAnthropic(
                responder=scripted([
                    (
                        [
                            tool(
                                "return_artifact",
                                value={"value": "observable"},
                            )
                        ],
                        "tool_use",
                    )
                ])
            ),
            enable_workflows=True,
            event_sink=sink,
        )
        manager_holder["manager"] = manager
        session = manager.create()
        context = RunContext.explicit_human(
            approved_capabilities=("workflow.launch",),
        )
        launched = await manager.workflows.launch(
            session_id=session.id,
            definition=definition,
            args={},
            run_context=context,
            action_id="observability-action",
            action_input={"definition": definition.to_dict(), "args": {}},
        )
        result = await manager.workflows.wait(launched.run_id)
        sink_errors = [
            s.sink_error
            for s in manager._sessions.values()
            if s.sink_error is not None
        ]
        await manager.stop()
        return result, sink_errors

    result, sink_errors = asyncio.run(main())
    assert result.status.value == "COMPLETED"
    assert started_state and started_state[0] is not None
    # The sink's failure is contained one layer earlier now -- inside the
    # session's dispatcher (the round-181 observer-containment rule), so the
    # workflow service never sees an emit failure. The claims survive: the
    # run completed untouched, and the broken sink is diagnosable, on
    # `session.sink_error` instead of the workflow observability list (which
    # still covers storage/capture failures inside emit itself).
    assert sink_errors and any("sink failed" in e for e in sink_errors)


def test_outbox_ack_happens_only_after_parent_append(tmp_path):
    definition = _single_node_definition(wall_time_seconds=5)

    class FailingMessages(list):
        def append(self, _message):
            raise RuntimeError("parent append failed")

    async def main():
        manager = SessionManager(
            _settings(tmp_path, fake_llm=True),
            FakeAsyncAnthropic(
                responder=scripted([
                    (
                        [
                            tool(
                                "return_artifact",
                                value={"value": "deliverable"},
                            )
                        ],
                        "tool_use",
                    )
                ])
            ),
            enable_workflows=True,
        )
        session = manager.create()
        context = RunContext.explicit_human(
            approved_capabilities=("workflow.launch",),
        )
        launched = await manager.workflows.launch(
            session_id=session.id,
            definition=definition,
            args={},
            run_context=context,
            action_id="outbox-action",
            action_input={"definition": definition.to_dict(), "args": {}},
            launch_turn=0,
        )
        await manager.workflows.wait(launched.run_id)
        session.run_count = 1
        agent = SimpleNamespace(
            state={
                "workflow_service": manager.workflows,
                "session": session,
            },
            messages=FailingMessages(),
        )
        with pytest.raises(RuntimeError, match="parent append failed"):
            await workflow_injector(agent)
        [pending] = manager.workflows.store.list_outbox(
            run_id=launched.run_id,
        )
        assert pending.delivered_at is None
        assert pending.claim_token is None

        agent.messages = []
        await workflow_injector(agent)
        [delivered] = manager.workflows.store.list_outbox(
            run_id=launched.run_id,
        )
        await manager.stop()
        return agent.messages, delivered

    messages, delivered = asyncio.run(main())
    assert len(messages) == 1
    assert "<workflow-results" in messages[0]["content"]
    assert delivered.delivered_at is not None


def test_failed_workflow_notification_keeps_error_diagnostic(tmp_path):
    definition = _single_node_definition(wall_time_seconds=5)

    async def main():
        manager = SessionManager(
            _settings(tmp_path, fake_llm=True),
            FakeAsyncAnthropic(
                responder=scripted([
                    ([text("finished without an artifact")], "end_turn"),
                ])
            ),
            enable_workflows=True,
        )
        session = manager.create()
        context = RunContext.explicit_human(
            approved_capabilities=("workflow.launch",),
        )
        launched = await manager.workflows.launch(
            session_id=session.id,
            definition=definition,
            args={},
            run_context=context,
            action_id="failed-notification-action",
            action_input={"definition": definition.to_dict(), "args": {}},
        )
        result = await manager.workflows.wait(launched.run_id)
        notifications, message_ids, claim_token = (
            manager.workflows.prepare_notifications(
                session_id=session.id,
                parent_turn=1,
            )
        )
        manager.workflows.release_notifications(
            session_id=session.id,
            message_ids=message_ids,
            claim_token=claim_token,
        )
        await manager.stop()
        return result, notifications

    result, notifications = asyncio.run(main())
    assert result.status.value == "FAILED"
    assert "did not call return_artifact" in result.error
    assert notifications[0]["status"] == "FAILED"
    assert notifications[0]["error"] == result.error
    assert notifications[0]["cancel_reason"] is None


def test_wall_time_timeout_cancels_cleanly_and_shutdown_is_idempotent(tmp_path):
    definition = _single_node_definition(wall_time_seconds=0.02)
    events = []

    async def main():
        manager = SessionManager(
            _settings(
                tmp_path,
                fake_llm=True,
                workflow_max_concurrent_agents=1,
                workflow_max_agents=1,
                workflow_max_rounds=1,
                workflow_wall_time_seconds=1,
            ),
            FakeAsyncAnthropic(delay=0.1),
            enable_workflows=True,
            event_sink=events.append,
        )
        session = manager.create()
        context = RunContext.explicit_human(
            approved_capabilities=("workflow.launch",),
        )
        launched = await manager.workflows.launch(
            session_id=session.id,
            definition=definition,
            args={},
            run_context=context,
            action_id="timeout-action",
            action_input={"definition": definition.to_dict(), "args": {}},
            tool_use_id="timeout-tool",
        )
        result = await manager.workflows.wait(launched.run_id)
        await manager.stop()
        return result

    result = asyncio.run(main())
    assert result.status.value == "CANCELLED"
    assert result.cancel_reason == "wall_time budget exceeded"
    cancelled = [
        event for event in events if event["type"] == "workflow_cancelled"
    ]
    assert len(cancelled) == 1
    assert cancelled[0]["payload"]["reason"] == "wall_time budget exceeded"


def test_manual_cancel_emits_one_terminal_event(tmp_path):
    definition = _single_node_definition(wall_time_seconds=5)
    events = []

    async def main():
        started = asyncio.Event()

        def sink(event):
            events.append(event)
            if event["type"] == "workflow_agent_started":
                started.set()

        manager = SessionManager(
            _settings(
                tmp_path,
                fake_llm=True,
                workflow_max_concurrent_agents=1,
                workflow_max_agents=1,
                workflow_max_rounds=1,
                workflow_wall_time_seconds=5,
            ),
            FakeAsyncAnthropic(delay=0.1),
            enable_workflows=True,
            event_sink=sink,
        )
        session = manager.create()
        context = RunContext.explicit_human(
            approved_capabilities=("workflow.launch",),
        )
        launched = await manager.workflows.launch(
            session_id=session.id,
            definition=definition,
            args={},
            run_context=context,
            action_id="cancel-action",
            action_input={"definition": definition.to_dict(), "args": {}},
            tool_use_id="cancel-tool",
        )
        await asyncio.wait_for(started.wait(), timeout=1)
        result = await manager.workflows.cancel(
            launched.run_id,
            session_id=session.id,
            reason="test cancellation",
        )
        notifications, message_ids, claim_token = (
            manager.workflows.prepare_notifications(
                session_id=session.id,
                parent_turn=1,
            )
        )
        manager.workflows.release_notifications(
            session_id=session.id,
            message_ids=message_ids,
            claim_token=claim_token,
        )
        await manager.stop()
        return result, notifications

    result, notifications = asyncio.run(main())
    assert result.status.value == "CANCELLED"
    assert result.cancel_reason == "test cancellation"
    cancelled = [
        event for event in events
        if event["type"] == "workflow_cancelled"
    ]
    assert len(cancelled) == 1
    assert cancelled[0]["payload"]["reason"] == "test cancellation"
    assert notifications[0]["status"] == "CANCELLED"
    assert notifications[0]["cancel_reason"] == "test cancellation"
    assert notifications[0]["error"] is None


def test_shutdown_records_service_cancel_reason(tmp_path):
    definition = _single_node_definition(wall_time_seconds=5)
    events = []

    async def main():
        started = asyncio.Event()

        def sink(event):
            events.append(event)
            if event["type"] == "workflow_agent_started":
                started.set()

        manager = SessionManager(
            _settings(
                tmp_path,
                fake_llm=True,
                workflow_max_concurrent_agents=1,
                workflow_max_agents=1,
                workflow_max_rounds=1,
                workflow_wall_time_seconds=5,
            ),
            FakeAsyncAnthropic(delay=0.1),
            enable_workflows=True,
            event_sink=sink,
        )
        session = manager.create()
        context = RunContext.explicit_human(
            approved_capabilities=("workflow.launch",),
        )
        launched = await manager.workflows.launch(
            session_id=session.id,
            definition=definition,
            args={},
            run_context=context,
            action_id="shutdown-action",
            action_input={"definition": definition.to_dict(), "args": {}},
        )
        await asyncio.wait_for(started.wait(), timeout=1)
        await manager.stop()
        return manager.workflows.store.get_run(launched.run_id)

    result = asyncio.run(main())
    assert result.status.value == "CANCELLED"
    assert result.cancel_reason == "service shutdown"
    cancelled = [
        event for event in events
        if event["type"] == "workflow_cancelled"
    ]
    assert len(cancelled) == 1
    assert cancelled[0]["payload"]["reason"] == "service shutdown"


def test_parent_delete_cancels_before_workspace_removal(tmp_path):
    definition = _single_node_definition(wall_time_seconds=5)

    async def main():
        started = asyncio.Event()

        def sink(event):
            if event["type"] == "workflow_agent_started":
                started.set()

        manager = SessionManager(
            _settings(
                tmp_path,
                fake_llm=True,
                workflow_max_concurrent_agents=1,
                workflow_max_agents=1,
                workflow_max_rounds=1,
                workflow_wall_time_seconds=5,
            ),
            FakeAsyncAnthropic(delay=0.1),
            enable_workflows=True,
            event_sink=sink,
        )
        session = manager.create()
        context = RunContext.explicit_human(
            approved_capabilities=("workflow.launch",),
        )
        launched = await manager.workflows.launch(
            session_id=session.id,
            definition=definition,
            args={},
            run_context=context,
            action_id="delete-action",
            action_input={"definition": definition.to_dict(), "args": {}},
        )
        await asyncio.wait_for(started.wait(), timeout=1)
        workspace = session.workspace
        assert manager.delete(session.id) is True
        retained_while_draining = workspace.exists()
        result = await manager.workflows.wait(launched.run_id)
        await manager.stop()
        return result, retained_while_draining, workspace.exists()

    result, retained_while_draining, exists_after_stop = asyncio.run(main())
    assert retained_while_draining is True
    assert exists_after_stop is False
    assert result.status.value == "CANCELLED"
    assert result.cancel_reason == "parent session deleted"


def test_parent_delete_retains_workspace_when_cancellation_task_fails(tmp_path):
    definition = _single_node_definition(wall_time_seconds=5)

    async def main():
        started = asyncio.Event()

        def sink(event):
            if event["type"] == "workflow_agent_started":
                started.set()

        manager = SessionManager(
            _settings(
                tmp_path,
                fake_llm=True,
                workflow_max_concurrent_agents=1,
                workflow_max_agents=1,
                workflow_max_rounds=1,
                workflow_wall_time_seconds=5,
            ),
            FakeAsyncAnthropic(delay=0.1),
            enable_workflows=True,
            event_sink=sink,
        )
        session = manager.create()
        context = RunContext.explicit_human(
            approved_capabilities=("workflow.launch",),
        )
        launched = await manager.workflows.launch(
            session_id=session.id,
            definition=definition,
            args={},
            run_context=context,
            action_id="delete-failure-action",
            action_input={"definition": definition.to_dict(), "args": {}},
        )
        await asyncio.wait_for(started.wait(), timeout=1)
        workspace = session.workspace
        original_cancel = manager.workflows.cancel

        async def fail_cancel(*_args, **_kwargs):
            raise RuntimeError("injected cancellation failure")

        manager.workflows.cancel = fail_cancel
        assert manager.delete(session.id) is True
        cleanup_tasks = tuple(manager._cleanup_tasks)
        await asyncio.gather(*cleanup_tasks, return_exceptions=True)
        retained_after_failure = workspace.exists()
        active_after_failure = manager.workflows.has_active(session.id)
        cleanup_errors = manager.cleanup_errors

        manager.workflows.cancel = original_cancel
        await manager.stop()
        result = manager.workflows.store.get_run(launched.run_id)
        return (
            retained_after_failure,
            active_after_failure,
            cleanup_errors,
            workspace.exists(),
            result,
        )

    (
        retained_after_failure,
        active_after_failure,
        cleanup_errors,
        exists_after_retry,
        result,
    ) = asyncio.run(main())
    assert retained_after_failure is True
    assert active_after_failure is True
    assert "injected cancellation failure" in cleanup_errors[-1]["error"]
    assert exists_after_retry is False
    assert result.status.value == "CANCELLED"


def test_sync_parent_delete_defers_workspace_cleanup_until_stop(tmp_path):
    manager = SessionManager(
        _settings(tmp_path, fake_llm=True),
        FakeAsyncAnthropic(),
        enable_workflows=True,
    )
    session = manager.create()
    definition = manager.workflows.store.register_definition(
        _single_node_definition(wall_time_seconds=5)
    )
    run = manager.workflows.store.create_run(
        definition_revision=definition.revision,
        session_id=session.id,
        run_context=RunContext.explicit_human(
            approved_capabilities=("workflow.launch",),
        ),
        idempotency_key="sync-delete-run",
        args={},
    )
    workspace = session.workspace

    assert manager.delete(session.id) is True
    assert workspace.exists() is True
    assert session.id in manager._deferred_workspace_cleanup

    asyncio.run(manager.stop())
    result = manager.workflows.store.get_run(run.run_id)
    assert result.status.value == "CANCELLED"
    assert workspace.exists() is False
