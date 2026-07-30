"""Core contracts for the standalone Dynamic Workflow MVP."""

from __future__ import annotations

import asyncio
from collections.abc import Mapping

import pytest

from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.run_context import RunContext
from mini_loop.workflows import (
    ArtifactValidationError,
    AttemptClaim,
    AttemptStatus,
    BudgetPolicy,
    FreshAgentRunner,
    IdempotencyConflict,
    InMemoryWorkflowStore,
    NodeKind,
    NodeStatus,
    RunStatus,
    ToolPolicy,
    VerificationStatus,
    VersionConflict,
    WorkflowDefinition,
    WorkflowEngine,
    WorkflowNode,
    WorkflowValidationError,
    artifact_from_submission,
    return_artifact,
    validate_definition,
)


VALUE_SCHEMA = {
    "type": "object",
    "required": ["value"],
    "properties": {"value": {"type": "string"}},
    "additionalProperties": False,
}

VERDICT_SCHEMA = {
    "type": "object",
    "required": ["status", "claim_id", "evidence", "reason"],
    "properties": {
        "status": {
            "type": "string",
            "enum": ["verified", "refuted", "unverified"],
        },
        "claim_id": {"type": "string"},
        "evidence": {"type": "array", "items": {"type": "string"}},
        "reason": {"type": "string"},
    },
    "additionalProperties": False,
}


def _definition(
    nodes: tuple[WorkflowNode, ...] | None = None,
    *,
    budget: BudgetPolicy | None = None,
    policy: ToolPolicy | None = None,
    return_from: str | None = None,
) -> WorkflowDefinition:
    selected = nodes or (
        WorkflowNode("discover", NodeKind.AGENT, output_schema=VALUE_SCHEMA),
    )
    return WorkflowDefinition(
        name="repo-audit",
        description="Audit a repository with structured results.",
        nodes=selected,
        return_from=return_from or selected[-1].id,
        input_schema={
            "type": "object",
            "required": ["question"],
            "properties": {"question": {"type": "string"}},
            "additionalProperties": False,
        },
        output_schema=selected[-1].output_schema,
        budget=budget or BudgetPolicy(),
        policy=policy or ToolPolicy(),
    )


def _context() -> RunContext:
    return RunContext.explicit_human(
        actor_id="test-user",
        channel="test",
        stamped_by="tests",
    )


def _registered_store(definition: WorkflowDefinition | None = None):
    definition = definition or _definition()
    store = InMemoryWorkflowStore()
    store.register_definition(definition)
    return store, definition


def _create_run(
    store: InMemoryWorkflowStore,
    definition: WorkflowDefinition,
    *,
    key: str = "launch-1",
    args: Mapping | None = None,
    run_context: RunContext | None = None,
    launch_action_id: str | None = "action-1",
):
    return store.create_run(
        definition_revision=definition.revision,
        session_id="session-1",
        run_context=run_context or _context(),
        idempotency_key=key,
        args=dict(args or {"question": "audit routes"}),
        launch_action_id=launch_action_id,
        policy_snapshot_hash="policy:v1",
    )


def test_definition_hash_is_canonical_and_versioned():
    first = _definition(
        (
            WorkflowNode(
                "discover",
                NodeKind.AGENT,
                output_schema={
                    "properties": {"value": {"type": "string"}},
                    "required": ["value"],
                    "type": "object",
                },
            ),
        )
    )
    second = _definition(
        (
            WorkflowNode(
                "discover",
                NodeKind.AGENT,
                output_schema={
                    "type": "object",
                    "required": ["value"],
                    "properties": {"value": {"type": "string"}},
                },
            ),
        )
    )
    changed = _definition(
        (WorkflowNode("discover", NodeKind.AGENT, prompt_template="different"),)
    )

    assert first.schema_version == 1
    assert first.definition_hash == second.definition_hash
    assert first.revision == second.revision
    assert first.definition_hash != changed.definition_hash
    assert WorkflowDefinition.from_dict(first.to_dict()).definition_hash == first.definition_hash


@pytest.mark.parametrize(
    ("definition", "message"),
    [
        (
            _definition(
                (
                    WorkflowNode("a", NodeKind.AGENT, needs=("b",)),
                    WorkflowNode("b", NodeKind.AGENT, needs=("a",)),
                )
            ),
            "acyclic",
        ),
        (
            _definition(
                (WorkflowNode("a", NodeKind.AGENT, needs=("missing",)),)
            ),
            "unknown node",
        ),
        (
            _definition(budget=BudgetPolicy(max_concurrent_agents=5)),
            "max_concurrent_agents",
        ),
        (
            _definition(policy=ToolPolicy(allowed_tools=("read_file", "bash"))),
            "not read-only",
        ),
        (
            _definition(policy=ToolPolicy(allowed_tools=("read_file",))),
            "exactly read_file and glob",
        ),
        (
            _definition(budget=BudgetPolicy(wall_time_seconds=0)),
            "wall_time_seconds",
        ),
        (
            _definition(budget=BudgetPolicy(token_budget=1_000)),
            "token_budget is not implemented",
        ),
        (
            _definition((WorkflowNode("map", NodeKind.MAP),)),
            "not implemented",
        ),
        (
            _definition((WorkflowNode("branch", NodeKind.BRANCH),)),
            "not implemented",
        ),
        (
            _definition((WorkflowNode("repeat", NodeKind.REPEAT_UNTIL),)),
            "not implemented",
        ),
        (
            _definition(
                (WorkflowNode("too-many-rounds", NodeKind.AGENT, max_rounds=3),),
                budget=BudgetPolicy(max_rounds=2),
            ),
            "exceeds workflow budget",
        ),
    ],
)
def test_definition_validation_rejects_unsafe_or_unbounded_ir(definition, message):
    with pytest.raises(WorkflowValidationError, match=message):
        validate_definition(definition)


def test_store_launch_idempotency_cas_and_payload_conflict():
    store, definition = _registered_store()
    context = _context()
    first = _create_run(store, definition, run_context=context)
    repeated = _create_run(store, definition, run_context=context)
    assert repeated.run_id == first.run_id
    assert isinstance(repeated.run_context, RunContext)

    with pytest.raises(IdempotencyConflict, match="different payload"):
        _create_run(
            store,
            definition,
            args={"question": "a different audit"},
            run_context=context,
        )

    with pytest.raises(IdempotencyConflict, match="different payload"):
        _create_run(store, definition, run_context=_context())

    with pytest.raises(IdempotencyConflict, match="different payload"):
        _create_run(
            store,
            definition,
            run_context=context,
            launch_action_id="different-action",
        )

    running = store.transition_run(
        first.run_id,
        expected_version=first.version,
        to_status=RunStatus.RUNNING,
    )
    assert running.version == first.version + 1
    with pytest.raises(VersionConflict):
        store.transition_run(
            first.run_id,
            expected_version=first.version,
            to_status=RunStatus.FAILED,
        )


def test_store_tracks_node_attempt_artifact_and_outbox_atomically():
    store, definition = _registered_store()
    run = _create_run(store, definition)
    run = store.transition_run(
        run.run_id,
        expected_version=run.version,
        to_status=RunStatus.RUNNING,
    )
    [claimed] = store.claim_nodes(
        run.run_id,
        [AttemptClaim("discover", "agent-discover", 0)],
        expected_version=run.version,
    )
    started = store.start_attempt(claimed.attempt_id, expected_version=claimed.version)
    artifact = artifact_from_submission(
        return_artifact({"value": "routes"}),
        attempt=started,
        node=definition.nodes[0],
    )
    committed = store.commit_attempt(
        started.attempt_id,
        expected_version=started.version,
        attempt_status=AttemptStatus.SUCCEEDED,
        node_status=NodeStatus.SUCCEEDED,
        artifact=artifact,
    )
    assert committed.result_artifact_id == artifact.artifact_id
    assert store.get_node(run.run_id, "discover").result_artifact_ids == (
        artifact.artifact_id,
    )
    assert store.get_artifact(artifact.artifact_id).value == {"value": "routes"}

    current = store.get_run(run.run_id)
    finished = store.finalize_run(
        run.run_id,
        expected_version=current.version,
        final_artifact_id=artifact.artifact_id,
    )
    assert finished.status == RunStatus.COMPLETED
    [message] = store.list_outbox(run_id=run.run_id)
    assert message.payload["artifact_id"] == artifact.artifact_id
    assert message.delivered_at is None
    claim_token, [claimed] = store.claim_outbox(
        session_id=run.session_id,
        run_ids={run.run_id},
    )
    assert claimed.claim_token == claim_token
    _other_token, duplicate_claim = store.claim_outbox(
        session_id=run.session_id,
        run_ids={run.run_id},
    )
    assert duplicate_claim == []
    [acknowledged] = store.acknowledge_outbox(
        session_id=run.session_id,
        message_ids=(message.message_id,),
        claim_token=claim_token,
    )
    assert acknowledged.delivered_at is not None
    assert store.list_outbox(run_id=run.run_id)[0].delivered_at is not None


def test_schema_subset_rejects_constraints_it_cannot_enforce():
    constrained = {
        "type": "string",
        "pattern": "^only-runtime-enforced-patterns$",
    }
    definition = WorkflowDefinition(
        name="unsupported_schema_constraint",
        nodes=(
            WorkflowNode(
                "result",
                NodeKind.AGENT,
                output_schema=constrained,
            ),
        ),
        return_from="result",
        output_schema=constrained,
    )

    with pytest.raises(
        WorkflowValidationError,
        match="unsupported schema keywords.*pattern",
    ):
        validate_definition(definition)


def test_structured_artifact_rejects_raw_or_schema_invalid_results():
    store, definition = _registered_store()
    run = _create_run(store, definition)
    run = store.transition_run(
        run.run_id,
        expected_version=run.version,
        to_status=RunStatus.RUNNING,
    )
    [attempt] = store.claim_nodes(
        run.run_id,
        [AttemptClaim("discover", "agent-discover", 0)],
        expected_version=run.version,
    )
    attempt = store.start_attempt(attempt.attempt_id, expected_version=attempt.version)

    with pytest.raises(ArtifactValidationError, match="return_artifact"):
        artifact_from_submission(
            {"value": "bypass"},  # type: ignore[arg-type]
            attempt=attempt,
            node=definition.nodes[0],
        )
    with pytest.raises(ArtifactValidationError, match="unknown keys"):
        artifact_from_submission(
            return_artifact({"value": "ok", "extra": True}),
            attempt=attempt,
            node=definition.nodes[0],
        )


def test_fresh_agent_runner_repairs_invalid_artifact_with_strict_peer_context(tmp_path):
    store, definition = _registered_store(
        _definition(
            (
                WorkflowNode(
                    "discover",
                    NodeKind.AGENT,
                    output_schema=VALUE_SCHEMA,
                    max_rounds=3,
                ),
            )
        )
    )
    run = _create_run(store, definition)
    running = store.transition_run(
        run.run_id,
        expected_version=run.version,
        to_status=RunStatus.RUNNING,
    )
    [attempt] = store.claim_nodes(
        run.run_id,
        [AttemptClaim("discover", "fresh-agent", 0)],
        expected_version=running.version,
    )
    client = FakeAsyncAnthropic(
        responder=scripted(
            [
                (
                    [
                        tool(
                            "return_artifact",
                            _id="invalid-artifact",
                            value={"value": 123},
                        )
                    ],
                    "tool_use",
                ),
                (
                    [
                        tool(
                            "return_artifact",
                            _id="artifact-1",
                            value={"value": "bounded result"},
                        )
                    ],
                    "tool_use",
                ),
                ([text("done")], "end_turn"),
            ]
        )
    )
    runner = FreshAgentRunner(
        client=client,
        settings=Settings(
            fake_llm=True,
            workspace_root=tmp_path / "workspaces",
            skills_dir=tmp_path / "skills",
        ),
        workspace=tmp_path,
        context_resolver=lambda claimed: store.get_run(claimed.run_id).run_context,
        max_rounds=4,
    )

    submission = asyncio.run(runner(attempt, definition.nodes[0], {}))

    assert submission.value == {"value": "bounded result"}
    assert runner.last_tool_names == ("read_file", "glob", "return_artifact")
    assert client.calls == 3
    assert runner.last_run_context is not None
    assert runner.last_run_context.authority == "peer_agent"
    assert runner.last_run_context.actor_id == attempt.agent_id
    assert runner.last_run_context.parent_message_id == run.run_context.message_id


def test_fresh_agent_runner_fails_when_agent_never_returns_artifact(tmp_path):
    store, definition = _registered_store()
    run = _create_run(store, definition)
    running = store.transition_run(
        run.run_id,
        expected_version=run.version,
        to_status=RunStatus.RUNNING,
    )
    [attempt] = store.claim_nodes(
        run.run_id,
        [AttemptClaim("discover", "fresh-agent", 0)],
        expected_version=running.version,
    )
    runner = FreshAgentRunner(
        client=FakeAsyncAnthropic(
            responder=scripted([([text("no structured result")], "end_turn")])
        ),
        settings=Settings(
            fake_llm=True,
            workspace_root=tmp_path / "workspaces",
            skills_dir=tmp_path / "skills",
        ),
        workspace=tmp_path,
        context_resolver=lambda claimed: store.get_run(claimed.run_id).run_context,
        max_rounds=1,
    )

    with pytest.raises(RuntimeError, match="did not call return_artifact"):
        asyncio.run(runner(attempt, definition.nodes[0], {}))


def test_fresh_agent_runner_never_writes_workspace_for_context_management(tmp_path):
    large_file = tmp_path / "large.txt"
    large_file.write_text("x" * 60_000)
    schema = {
        "type": "object",
        "properties": {"value": {"type": "string"}},
        "required": ["value"],
        "additionalProperties": False,
    }
    definition = WorkflowDefinition(
        name="readonly_compaction",
        nodes=(
            WorkflowNode(
                "inspect",
                NodeKind.AGENT,
                output_schema=schema,
                max_rounds=8,
            ),
        ),
        return_from="inspect",
        output_schema=schema,
        budget=BudgetPolicy(max_rounds=8),
    )
    store, definition = _registered_store(definition)
    run = _create_run(store, definition)
    running = store.transition_run(
        run.run_id,
        expected_version=run.version,
        to_status=RunStatus.RUNNING,
    )
    [attempt] = store.claim_nodes(
        run.run_id,
        [AttemptClaim("inspect", "readonly-agent", 0)],
        expected_version=running.version,
    )
    turns = [
        ([tool("read_file", _id=f"read-{index}", path="large.txt")], "tool_use")
        for index in range(5)
    ]
    turns.extend([
        (
            [
                tool(
                    "return_artifact",
                    _id="readonly-result",
                    value={"value": "unchanged"},
                )
            ],
            "tool_use",
        ),
        ([text("done")], "end_turn"),
    ])
    runner = FreshAgentRunner(
        client=FakeAsyncAnthropic(responder=scripted(turns)),
        settings=Settings(
            fake_llm=True,
            workspace_root=tmp_path / "workspaces",
            skills_dir=tmp_path / "skills",
        ),
        workspace=tmp_path,
        context_resolver=lambda item: store.get_run(item.run_id).run_context,
        max_rounds=8,
    )
    before = {
        path.relative_to(tmp_path): path.read_bytes()
        for path in tmp_path.rglob("*")
        if path.is_file()
    }

    submission = asyncio.run(runner(attempt, definition.nodes[0], {}))

    after = {
        path.relative_to(tmp_path): path.read_bytes()
        for path in tmp_path.rglob("*")
        if path.is_file()
    }
    assert submission.value == {"value": "unchanged"}
    assert after == before
    assert not (tmp_path / ".task_outputs").exists()
    assert not (tmp_path / ".transcripts").exists()


def test_repo_audit_engine_is_bounded_stable_and_marks_verifier_failure_unverified():
    nodes = (
        WorkflowNode(
            "discover",
            NodeKind.AGENT,
            output_schema={
                "type": "object",
                "required": ["targets"],
                "properties": {
                    "targets": {"type": "array", "items": {"type": "string"}}
                },
                "additionalProperties": False,
            },
        ),
        WorkflowNode("audit_a", NodeKind.AGENT, needs=("discover",), output_schema=VALUE_SCHEMA),
        WorkflowNode("audit_b", NodeKind.AGENT, needs=("discover",), output_schema=VALUE_SCHEMA),
        WorkflowNode("verify_a", NodeKind.VERIFY, needs=("audit_a",), output_schema=VERDICT_SCHEMA),
        WorkflowNode("verify_b", NodeKind.VERIFY, needs=("audit_b",), output_schema=VERDICT_SCHEMA),
        WorkflowNode(
            "reduce",
            NodeKind.REDUCE,
            needs=("verify_a", "verify_b"),
            output_schema={
                "type": "object",
                "required": ["ordered"],
                "properties": {
                    "ordered": {"type": "array", "items": {"type": "string"}}
                },
                "additionalProperties": False,
            },
        ),
    )
    definition = _definition(nodes, return_from="reduce")
    store, definition = _registered_store(definition)
    run = _create_run(store, definition)
    active = 0
    max_active = 0
    reduce_input_order = []
    discover_inputs = {}

    async def runner(attempt, node, inputs):
        nonlocal active, max_active, discover_inputs
        active += 1
        max_active = max(max_active, active)
        try:
            if node.id == "discover":
                discover_inputs = dict(inputs)
                return return_artifact({"targets": ["a.py", "b.py"]})
            if node.id.startswith("audit_"):
                await asyncio.sleep(0.02 if node.id == "audit_a" else 0.005)
                return return_artifact({"value": node.id})
            if node.id == "verify_a":
                return return_artifact({
                    "status": "verified",
                    "claim_id": "audit_a",
                    "evidence": ["line 1"],
                    "reason": "confirmed",
                })
            if node.id == "verify_b":
                raise RuntimeError("verifier unavailable")
            reduce_input_order.extend(key for key in inputs if key != "args")
            return return_artifact(
                {"ordered": [key for key in inputs if key != "args"]}
            )
        finally:
            active -= 1

    result = asyncio.run(WorkflowEngine(store, runner, max_concurrent_agents=2).execute(run.run_id))

    assert result.status == RunStatus.COMPLETED
    assert max_active == 2
    assert discover_inputs == {"args": {"question": "audit routes"}}
    assert reduce_input_order == ["verify_a", "verify_b"]
    final = store.get_artifact(result.final_artifact_id)
    assert final.value == {"ordered": ["verify_a", "verify_b"]}

    attempts = {attempt.node_id: attempt for attempt in store.list_attempts(run.run_id)}
    assert attempts["verify_a"].agent_id != attempts["audit_a"].agent_id
    assert attempts["verify_a"].parent_agent_id == attempts["audit_a"].agent_id
    assert attempts["verify_b"].verification_status == VerificationStatus.UNVERIFIED
    assert store.get_node(run.run_id, "verify_b").status == NodeStatus.UNVERIFIED
    [fallback] = store.artifacts_for_node(run.run_id, "verify_b")
    assert fallback.value["status"] == "unverified"
    assert fallback.schema_valid is False


def test_engine_cancel_stops_future_claims():
    definition = _definition(
        (
            WorkflowNode("first", NodeKind.AGENT, output_schema=VALUE_SCHEMA),
            WorkflowNode(
                "never_claimed",
                NodeKind.AGENT,
                needs=("first",),
                output_schema=VALUE_SCHEMA,
            ),
        ),
        return_from="never_claimed",
    )
    store, definition = _registered_store(definition)
    run = _create_run(store, definition)
    started = asyncio.Event()

    async def runner(_attempt, node, _inputs):
        assert node.id == "first"
        started.set()
        await asyncio.Event().wait()
        raise AssertionError("cancel should interrupt the task")

    async def scenario():
        engine = WorkflowEngine(store, runner)
        execution = asyncio.create_task(engine.execute(run.run_id))
        await started.wait()
        await engine.cancel(run.run_id)
        return await execution

    result = asyncio.run(scenario())
    assert result.status == RunStatus.CANCELLED
    assert [attempt.node_id for attempt in store.list_attempts(run.run_id)] == ["first"]
    assert store.get_node(run.run_id, "never_claimed").status == NodeStatus.CANCELLED


def test_engine_cancel_settles_attempt_task_before_its_first_step():
    definition = _definition(
        (WorkflowNode("only", NodeKind.AGENT, output_schema=VALUE_SCHEMA),),
        return_from="only",
    )
    store, definition = _registered_store(definition)
    run = _create_run(store, definition)
    runner_called = False

    async def runner(_attempt, _node, _inputs):
        nonlocal runner_called
        runner_called = True
        return return_artifact({"value": "unexpected"})

    async def scenario():
        engine = WorkflowEngine(store, runner)
        loop = asyncio.get_running_loop()
        cancel_done = loop.create_future()

        def schedule_cancel():
            task = asyncio.create_task(engine.cancel(run.run_id))

            def finish_cancel(completed):
                if completed.exception() is not None:
                    cancel_done.set_exception(completed.exception())
                else:
                    cancel_done.set_result(completed.result())

            task.add_done_callback(finish_cancel)

        # Put the cancel callback ahead of the attempt task that execute()
        # creates, while still allowing execute() to claim the node first.
        loop.call_soon(schedule_cancel)
        execution = asyncio.create_task(engine.execute(run.run_id))
        result = await asyncio.wait_for(execution, timeout=0.5)
        await cancel_done
        return result

    result = asyncio.run(scenario())
    [attempt] = store.list_attempts(run.run_id)

    assert result.status == RunStatus.CANCELLED
    assert attempt.status == AttemptStatus.CANCELLED
    assert attempt.started_at is None
    assert store.get_node(run.run_id, "only").status == NodeStatus.CANCELLED
    assert runner_called is False


def test_shared_attempt_cap_across_concurrent_engines_and_runs():
    schema = {
        "type": "object",
        "properties": {"value": {"type": "string"}},
        "required": ["value"],
        "additionalProperties": False,
    }
    definition = WorkflowDefinition(
        name="process_wide_cap",
        nodes=(
            WorkflowNode("left", NodeKind.AGENT, output_schema=schema),
            WorkflowNode("right", NodeKind.AGENT, output_schema=schema),
        ),
        return_from="right",
        output_schema=schema,
        budget=BudgetPolicy(max_concurrent_agents=2, max_agents=2),
    )
    store, definition = _registered_store(definition)
    first = _create_run(store, definition, key="first")
    second = _create_run(store, definition, key="second")
    active = 0
    peak = 0

    async def runner(_attempt, node, _inputs):
        nonlocal active, peak
        active += 1
        peak = max(peak, active)
        try:
            await asyncio.sleep(0.01)
            return return_artifact({"value": node.id})
        finally:
            active -= 1

    shared_attempts = asyncio.Semaphore(2)
    first_engine = WorkflowEngine(
        store,
        runner,
        max_concurrent_agents=2,
        attempt_semaphore=shared_attempts,
    )
    second_engine = WorkflowEngine(
        store,
        runner,
        max_concurrent_agents=2,
        attempt_semaphore=shared_attempts,
    )

    async def execute_both():
        return await asyncio.gather(
            first_engine.execute(first.run_id),
            second_engine.execute(second.run_id),
        )

    results = asyncio.run(execute_both())

    assert [result.status for result in results] == [
        RunStatus.COMPLETED,
        RunStatus.COMPLETED,
    ]
    assert peak == 2
