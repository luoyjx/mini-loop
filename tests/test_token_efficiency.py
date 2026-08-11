"""Contracts for the standalone token-efficiency SPI.

These tests intentionally exercise the module without wiring it into Agent or
SessionManager.  Integration must remain an explicit harness decision.
"""

from __future__ import annotations

import asyncio
import hashlib
from dataclasses import FrozenInstanceError, replace
from pathlib import Path

import pytest

from mini_loop.token_efficiency import (
    ComponentBinding,
    ComponentDescriptor,
    ComponentLifecycle,
    ComponentStage,
    ConciseResponsePolicy,
    DeterministicLosslessReducer,
    LifecycleHealth,
    LifecycleStatus,
    Lossiness,
    MaskedObservation,
    MaskedRawArtifactStore,
    ObservationReducer,
    ObservationReduction,
    OptimizationMode,
    OptimizationStatus,
    PassthroughObservationReducer,
    PassthroughRequestContextOptimizer,
    PassthroughResponsePolicy,
    RawArtifactCapacityExceeded,
    RawArtifactExpired,
    RawArtifactNotFound,
    RawArtifactStoreError,
    RawArtifactTooLarge,
    RequestContext,
    RequestContextOptimizer,
    RequestOptimization,
    ResponsePolicy,
    ResponsePolicyContext,
    ResponsePolicyPlan,
    StableRequestSettings,
    StageComponents,
    TokenEfficiencyRegistry,
    TokenEfficiencyRuntime,
)


def test_descriptor_and_receipts_are_immutable_and_typed():
    descriptor = ComponentDescriptor(
        id="example.reducer",
        version="1.2.3",
        stage="observation",
        content_types=("text/plain",),
        lossiness="lossless",
        capabilities=("logs", "logs"),
    )
    assert descriptor.stage is ComponentStage.OBSERVATION
    assert descriptor.lossiness is Lossiness.LOSSLESS
    assert descriptor.capabilities == ("logs",)
    with pytest.raises(FrozenInstanceError):
        descriptor.id = "changed"  # type: ignore[misc]
    with pytest.raises(ValueError):
        ComponentDescriptor(id="../escape", version="1", stage="observation")
    with pytest.raises(ValueError, match="version"):
        ComponentDescriptor(id="safe", version="secret\nvalue", stage="observation")
    with pytest.raises(ValueError):
        ComponentDescriptor(
            id="recoverable",
            version="1",
            stage="observation",
            lossiness=Lossiness.RECOVERABLE,
        )
    reducer = DeterministicLosslessReducer()
    assert reducer.descriptor.lossiness is Lossiness.RECOVERABLE
    assert reducer.descriptor.recoverable


def test_spi_results_reject_unbounded_or_untyped_plugin_values():
    with pytest.raises(TypeError, match="warnings"):
        ObservationReduction("ok", warnings=["not-a-tuple"])  # type: ignore[arg-type]
    with pytest.raises(TypeError, match="only strings"):
        RequestOptimization({}, warnings=(object(),))  # type: ignore[arg-type]
    with pytest.raises(ValueError, match="at most 32"):
        ObservationReduction("ok", warnings=("warning",) * 33)
    with pytest.raises(ValueError, match="at most 256"):
        ResponsePolicyPlan(StableRequestSettings(), warnings=("x" * 257,))
    with pytest.raises(ValueError, match="machine-readable"):
        ObservationReduction("ok", warnings=("SECRET_FROM_INPUT=value",))
    with pytest.raises(TypeError, match="StableRequestSettings"):
        ResponsePolicyPlan(object())  # type: ignore[arg-type]


def test_stable_request_settings_are_strict_and_bounded():
    with pytest.raises(TypeError, match="tuple"):
        StableRequestSettings(instructions=["x"])  # type: ignore[arg-type]
    with pytest.raises(TypeError, match="only strings"):
        StableRequestSettings(instructions=(object(),))  # type: ignore[arg-type]
    with pytest.raises(ValueError, match="at most 2048"):
        StableRequestSettings(instructions=("x" * 2_049,))
    with pytest.raises(ValueError, match="total at most 8192"):
        StableRequestSettings(instructions=("x" * 2_000,) * 5)
    with pytest.raises(TypeError, match="metadata"):
        StableRequestSettings(metadata={"key": "value"})  # type: ignore[arg-type]
    with pytest.raises(TypeError, match="strings"):
        StableRequestSettings(metadata=(("key", object()),))  # type: ignore[arg-type]
    with pytest.raises(ValueError, match="unique"):
        StableRequestSettings(metadata=(("key", "one"), ("key", "two")))
    with pytest.raises(TypeError, match="integer"):
        StableRequestSettings(max_output_tokens=True)
    with pytest.raises(TypeError, match="verbosity"):
        StableRequestSettings(verbosity=1)  # type: ignore[arg-type]


def test_protocols_and_registry_are_stage_specific():
    observation = PassthroughObservationReducer()
    request = PassthroughRequestContextOptimizer()
    response = PassthroughResponsePolicy()
    assert isinstance(observation, ObservationReducer)
    assert isinstance(request, RequestContextOptimizer)
    assert isinstance(response, ResponsePolicy)

    registry = TokenEfficiencyRegistry()
    assert registry.register_observation(observation) is observation
    assert registry.register_request_optimizer(request) is request
    assert registry.register_response_policy(response) is response
    snapshot = registry.snapshot()
    assert tuple(item.descriptor.id for item in snapshot.observation_reducers) == (
        "passthrough-observation",
    )
    assert tuple(item.descriptor.id for item in snapshot.request_optimizers) == (
        "passthrough-request",
    )
    assert tuple(item.descriptor.id for item in snapshot.response_policies) == (
        "passthrough-response",
    )
    with pytest.raises(ValueError, match="belongs to observation"):
        TokenEfficiencyRegistry().register_response_policy(observation)  # type: ignore[arg-type]
    with pytest.raises(ValueError, match="duplicate"):
        registry.register_observation(PassthroughObservationReducer())


def _workspace(tmp_path: Path, name: str) -> Path:
    path = tmp_path / name
    path.mkdir()
    return path


def _run(awaitable):
    return asyncio.run(awaitable)


def test_observation_metadata_survives_runtime_projection():
    registry = TokenEfficiencyRegistry()
    registry.register_observation(PassthroughObservationReducer())
    original = MaskedObservation(
        "command output",
        content_type="text/x-command-output",
        metadata=(("exit_code", 7), ("timed_out", False)),
    )

    outcome = _run(
        registry.runtime(default_mode=OptimizationMode.ENFORCE).reduce_observation(
            original
        )
    )

    assert outcome.observation.metadata == (
        ("exit_code", 7),
        ("timed_out", False),
    )


def test_masked_raw_store_returns_unpredictable_scoped_ref_and_digest(tmp_path):
    alice = _workspace(tmp_path, "alice")
    bob = _workspace(tmp_path, "bob")
    store = MaskedRawArtifactStore(alice)
    pointer = store.put_masked("credential=[MASKED]")

    assert pointer.ref.startswith("raw_")
    assert len(pointer.ref) == 47
    assert "/" not in pointer.ref and "alice" not in pointer.ref
    assert pointer.digest == "sha256:" + hashlib.sha256(
        b"credential=[MASKED]"
    ).hexdigest()
    assert pointer.size_bytes == len(b"credential=[MASKED]")
    assert store.get_masked(pointer.ref) == "credential=[MASKED]"
    assert store.workspace == alice.resolve()
    assert not (alice / ".token-efficiency").exists()
    assert not hasattr(store, "root")

    with pytest.raises(RawArtifactNotFound):
        MaskedRawArtifactStore(alice).get_masked(pointer.ref)
    with pytest.raises(RawArtifactNotFound):
        MaskedRawArtifactStore(bob).get_masked(pointer.ref)
    with pytest.raises(RawArtifactNotFound):
        store.get_masked("../../etc/passwd")
    assert not hasattr(store, "put")
    assert "already secret-masked" in MaskedRawArtifactStore.put_masked.__doc__


def test_masked_raw_store_enforces_ttl_and_removes_expired_records(tmp_path):
    now = [1_000.0]
    store = MaskedRawArtifactStore(
        _workspace(tmp_path, "workspace"),
        ttl_seconds=10,
        clock=lambda: now[0],
    )
    pointer = store.put_masked("masked")
    now[0] += 9.9
    assert store.get_masked(pointer.ref) == "masked"
    now[0] += 0.1
    with pytest.raises(RawArtifactExpired):
        store.get_masked(pointer.ref)
    with pytest.raises(RawArtifactNotFound):
        store.get_masked(pointer.ref)


def test_masked_raw_store_enforces_per_item_and_total_size(tmp_path):
    store = MaskedRawArtifactStore(
        _workspace(tmp_path, "workspace"),
        max_artifact_bytes=5,
        max_total_bytes=8,
    )
    with pytest.raises(RawArtifactTooLarge):
        store.put_masked("123456")
    store.put_masked("12345")
    with pytest.raises(RawArtifactCapacityExceeded):
        store.put_masked("1234")


def test_masked_raw_store_validates_legacy_directory_without_using_it(tmp_path):
    workspace = _workspace(tmp_path, "workspace")
    with pytest.raises(ValueError, match="contained relative"):
        MaskedRawArtifactStore(workspace, directory="../outside")

    outside = _workspace(tmp_path, "outside")
    (workspace / "linked").symlink_to(outside, target_is_directory=True)
    store = MaskedRawArtifactStore(workspace, directory="linked/raw")
    store.put_masked("masked")
    assert not tuple(outside.iterdir())
    assert not (workspace / "linked" / "raw").exists()


def test_masked_raw_store_validates_digest_and_size_in_memory(tmp_path):
    workspace = _workspace(tmp_path, "workspace")
    store = MaskedRawArtifactStore(workspace)
    pointer = store.put_masked("safe masked content")
    record = store._records[pointer.ref]
    store._records[pointer.ref] = replace(
        record, payload=b"x" * record.size_bytes
    )

    with pytest.raises(RawArtifactNotFound, match="integrity"):
        store.get_masked(pointer.ref)


def test_masked_raw_store_discard_clear_and_close_are_fail_closed(tmp_path):
    workspace = _workspace(tmp_path, "workspace")
    store = MaskedRawArtifactStore(workspace)
    first = store.put_masked("first")
    store.put_masked("second")
    assert store.discard(first.ref)
    assert not store.discard(first.ref)
    assert store.clear() == 1
    pointer = store.put_masked("third")
    assert store.close() == 1
    assert store.closed
    assert store.clear() == 0

    with pytest.raises(RawArtifactStoreError, match="closed"):
        store.get_masked(pointer.ref)
    with pytest.raises(RawArtifactStoreError, match="closed"):
        store.put_masked("fourth")


def test_masked_raw_store_sweep_reclaims_expired_capacity(tmp_path):
    now = [1_000.0]
    store = MaskedRawArtifactStore(
        _workspace(tmp_path, "workspace"),
        ttl_seconds=10,
        max_artifact_bytes=6,
        max_total_bytes=6,
        clock=lambda: now[0],
    )
    pointer = store.put_masked("123456")
    now[0] += 10
    assert store.sweep() == 1
    with pytest.raises(RawArtifactNotFound):
        store.get_masked(pointer.ref)
    assert store.put_masked("654321").size_bytes == 6


def test_passthrough_is_a_structured_noop():
    registry = TokenEfficiencyRegistry()
    registry.register_observation(
        PassthroughObservationReducer(), mode=OptimizationMode.ENFORCE
    )
    original = MaskedObservation("ERROR /tmp/a.py:42")
    outcome = _run(registry.runtime().reduce_observation(original))

    assert outcome.observation == original
    [receipt] = outcome.receipts
    assert receipt.status is OptimizationStatus.PASSTHROUGH
    assert receipt.reason == "component_passthrough"
    assert receipt.raw_bytes == receipt.projected_bytes
    assert receipt.input_digest == receipt.output_digest
    serialized = receipt.as_dict()
    assert serialized["stage"] == "observation"
    assert serialized["mode"] == "enforce"
    assert serialized["status"] == "passthrough"
    assert original.content not in repr(serialized)
    assert "content" not in serialized


def test_broken_token_estimator_cannot_break_fail_open_receipts():
    registry = TokenEfficiencyRegistry()
    registry.register_observation(
        PassthroughObservationReducer(), mode=OptimizationMode.ENFORCE
    )

    def broken(_value):
        raise RuntimeError("meter unavailable")

    outcome = _run(
        registry.runtime(token_counter=broken).reduce_observation(
            MaskedObservation("ordinary output")
        )
    )
    assert outcome.observation.content == "ordinary output"
    assert outcome.receipts[0].status is OptimizationStatus.PASSTHROUGH
    assert outcome.receipts[0].tokens_before_estimate > 0


def test_deterministic_reducer_folds_noise_but_never_key_diagnostics():
    critical = "ERROR /tmp/project/app.py:42 failed"
    source = (
        "\x1b[32mbuilding\x1b[0m\n" * 10
        + "\n\n\n"
        + critical
        + "\n"
        + critical
        + "\n"
    )
    reducer = DeterministicLosslessReducer()
    first = _run(reducer.reduce(MaskedObservation(source)))
    second = _run(reducer.reduce(MaskedObservation(source)))

    assert first == second
    assert "\x1b" not in first.content
    assert "[repeated 9 additional times]" in first.content
    assert first.content.count(critical) == 2
    assert "/tmp/project/app.py:42" in first.content
    assert "ERROR" in first.content and "failed" in first.content
    assert len(first.content.encode()) < len(source.encode())
    assert "removed_ansi:20" in first.warnings


def test_enforce_reduces_once_and_persists_only_the_masked_raw_copy(tmp_path):
    workspace = _workspace(tmp_path, "workspace")
    store = MaskedRawArtifactStore(workspace)
    registry = TokenEfficiencyRegistry()
    registry.register_observation(
        DeterministicLosslessReducer(), mode=OptimizationMode.ENFORCE
    )
    source = "progress\n" * 20 + "credential=[MASKED]\n"

    outcome = _run(
        registry.runtime(raw_store=store).reduce_observation(
            MaskedObservation(source), persist_masked_raw=True
        )
    )

    assert outcome.observation.content != source
    assert outcome.observation.reduced_by == ("deterministic-lossless",)
    assert outcome.observation.raw_ref is not None
    assert store.get_masked(outcome.observation.raw_ref) == source
    [receipt] = outcome.receipts
    assert receipt.status is OptimizationStatus.APPLIED
    assert receipt.raw_ref == outcome.observation.raw_ref
    assert receipt.raw_digest == outcome.observation.raw_digest
    assert receipt.projected_bytes < receipt.raw_bytes


def test_recoverable_passthrough_and_unsupported_content_do_not_allocate(tmp_path):
    store = MaskedRawArtifactStore(_workspace(tmp_path, "workspace"))
    registry = TokenEfficiencyRegistry()
    registry.register_observation(
        DeterministicLosslessReducer(), mode=OptimizationMode.ENFORCE
    )
    runtime = registry.runtime(raw_store=store)

    passthrough = _run(
        runtime.reduce_observation(
            MaskedObservation("single line"), persist_masked_raw=True
        )
    )
    unsupported = _run(
        runtime.reduce_observation(
            MaskedObservation("payload", content_type="image/png"),
            persist_masked_raw=True,
        )
    )

    assert passthrough.receipts[0].reason == "component_passthrough"
    assert unsupported.receipts[0].reason == "unsupported_content_type"
    assert store.clear() == 0


def test_shadow_measures_candidate_but_returns_original():
    registry = TokenEfficiencyRegistry()
    registry.register_observation(
        DeterministicLosslessReducer(), mode=OptimizationMode.SHADOW
    )
    original = MaskedObservation("same line\n" * 20)
    outcome = _run(registry.runtime().reduce_observation(original))

    assert outcome.observation == original
    [receipt] = outcome.receipts
    assert receipt.status is OptimizationStatus.SHADOWED
    assert receipt.reason == "shadow_candidate"
    assert receipt.projected_bytes < receipt.raw_bytes
    assert receipt.changed


def test_shadow_never_persists_raw_and_store_binding_is_per_runtime(tmp_path):
    workspace = _workspace(tmp_path, "workspace")
    store = MaskedRawArtifactStore(workspace)
    registry = TokenEfficiencyRegistry()
    registry.register_observation(
        DeterministicLosslessReducer(), mode=OptimizationMode.SHADOW
    )
    template = registry.runtime()
    runtime = template.with_raw_store(store)

    assert template.raw_store is None
    assert runtime.raw_store is store
    assert not runtime.observation_enforced
    outcome = _run(
        runtime.reduce_observation(
            MaskedObservation("repeat\n" * 20), persist_masked_raw=True
        )
    )
    assert outcome.observation.raw_ref is None
    assert store.clear() == 0
    assert not (workspace / ".token-efficiency").exists()

    enforced = TokenEfficiencyRegistry()
    enforced.register_observation(
        DeterministicLosslessReducer(), mode=OptimizationMode.ENFORCE
    )
    assert enforced.runtime().observation_enforced


def test_recoverable_storage_failure_is_fail_open(tmp_path):
    store = MaskedRawArtifactStore(_workspace(tmp_path, "workspace"))
    store.close()
    registry = TokenEfficiencyRegistry()
    registry.register_observation(
        DeterministicLosslessReducer(), mode=OptimizationMode.ENFORCE
    )
    original = MaskedObservation("repeat\n" * 20)

    outcome = _run(
        registry.runtime(raw_store=store).reduce_observation(
            original, persist_masked_raw=True
        )
    )

    assert outcome.observation == original
    assert outcome.receipts[0].status is OptimizationStatus.DEGRADED
    assert outcome.receipts[0].reason == "recovery_unavailable"
    assert outcome.warnings == ("raw_store_error",)


class _Expander:
    descriptor = ComponentDescriptor(
        id="expander",
        version="1",
        stage=ComponentStage.OBSERVATION,
        lossiness=Lossiness.RECOVERABLE,
        recoverable=True,
    )

    async def reduce(self, observation, *, query=None, budget_tokens=None):
        return ObservationReduction(observation.content + " this is strictly larger")


def test_inflation_guard_rejects_before_recovery_allocation(tmp_path):
    store = MaskedRawArtifactStore(_workspace(tmp_path, "workspace"))
    registry = TokenEfficiencyRegistry()
    registry.register_observation(_Expander(), mode=OptimizationMode.ENFORCE)
    original = MaskedObservation("small")
    outcome = _run(
        registry.runtime(raw_store=store).reduce_observation(
            original, persist_masked_raw=True
        )
    )

    assert outcome.observation == original
    [receipt] = outcome.receipts
    assert receipt.status is OptimizationStatus.DEGRADED
    assert receipt.reason == "inflation_guard"
    assert receipt.projected_bytes > receipt.raw_bytes
    assert store.clear() == 0


class _CountingReducer:
    def __init__(self, component_id: str, replacement: str) -> None:
        self.descriptor = ComponentDescriptor(
            id=component_id,
            version="1",
            stage=ComponentStage.OBSERVATION,
        )
        self.replacement = replacement
        self.calls = 0

    async def reduce(self, observation, *, query=None, budget_tokens=None):
        self.calls += 1
        return ObservationReduction(self.replacement)


def test_double_reduction_guard_gives_one_component_ownership():
    first = _CountingReducer("first", "x")
    second = _CountingReducer("second", "y")
    registry = TokenEfficiencyRegistry()
    registry.register_observation(first, mode=OptimizationMode.ENFORCE)
    registry.register_observation(second, mode=OptimizationMode.ENFORCE)
    outcome = _run(
        registry.runtime().reduce_observation(
            MaskedObservation("a much longer observation")
        )
    )

    assert outcome.observation.content == "x"
    assert outcome.observation.reduced_by == ("first",)
    assert first.calls == 1 and second.calls == 0
    assert [receipt.status for receipt in outcome.receipts] == [
        OptimizationStatus.APPLIED,
        OptimizationStatus.PASSTHROUGH,
    ]
    assert outcome.receipts[1].reason == "double_reduction_guard:first"


def test_existing_reduction_marker_skips_every_reducer():
    reducer = _CountingReducer("new", "short")
    registry = TokenEfficiencyRegistry()
    registry.register_observation(reducer, mode=OptimizationMode.ENFORCE)
    outcome = _run(
        registry.runtime().reduce_observation(
            MaskedObservation("already projected", reduced_by=("upstream",))
        )
    )
    assert reducer.calls == 0
    assert outcome.observation.content == "already projected"
    assert outcome.receipts[0].reason == "double_reduction_guard:upstream"


class _SecretFailure:
    descriptor = ComponentDescriptor(
        id="failure",
        version="1",
        stage=ComponentStage.OBSERVATION,
    )

    async def reduce(self, observation, *, query=None, budget_tokens=None):
        raise RuntimeError("do not copy credential-super-secret into a receipt")


def test_component_error_is_fail_open_sanitized_and_does_not_allocate(
    tmp_path,
):
    store = MaskedRawArtifactStore(_workspace(tmp_path, "workspace"))
    fallback = _CountingReducer("fallback", "ok")
    registry = TokenEfficiencyRegistry()
    registry.register_observation(_SecretFailure(), mode=OptimizationMode.ENFORCE)
    registry.register_observation(fallback, mode=OptimizationMode.ENFORCE)
    outcome = _run(
        registry.runtime(raw_store=store).reduce_observation(
            MaskedObservation("a long enough original result"),
            persist_masked_raw=True,
        )
    )

    assert outcome.observation.content == "ok"
    assert outcome.receipts[0].status is OptimizationStatus.ERROR
    assert outcome.receipts[0].reason == "component_error:RuntimeError"
    assert "super-secret" not in repr(outcome.receipts[0])
    assert outcome.receipts[1].status is OptimizationStatus.APPLIED
    assert store.clear() == 0


class _SlowReducer:
    descriptor = ComponentDescriptor(
        id="slow",
        version="1",
        stage=ComponentStage.OBSERVATION,
        timeout_ms=1,
    )

    async def reduce(self, observation, *, query=None, budget_tokens=None):
        await asyncio.sleep(0.05)
        return ObservationReduction("never returned")


def test_component_timeout_is_also_fail_open():
    registry = TokenEfficiencyRegistry()
    registry.register_observation(_SlowReducer(), mode=OptimizationMode.ENFORCE)
    original = MaskedObservation("original")
    outcome = _run(registry.runtime().reduce_observation(original))
    assert outcome.observation == original
    assert outcome.receipts[0].status is OptimizationStatus.ERROR
    assert outcome.receipts[0].reason == "component_error:TimeoutError"


class _ForgedWarningReducer:
    descriptor = ComponentDescriptor(
        id="forged-warning",
        version="1",
        stage=ComponentStage.OBSERVATION,
    )

    async def reduce(self, observation, *, query=None, budget_tokens=None):
        result = ObservationReduction("short")
        object.__setattr__(result, "warnings", ("SECRET_FROM_INPUT=value",))
        return result


def test_runtime_revalidates_forged_plugin_result_and_fails_open():
    registry = TokenEfficiencyRegistry()
    registry.register_observation(
        _ForgedWarningReducer(), mode=OptimizationMode.ENFORCE
    )
    original = MaskedObservation("a longer original observation")

    outcome = _run(registry.runtime().reduce_observation(original))

    assert outcome.observation == original
    assert outcome.receipts[0].status is OptimizationStatus.ERROR
    assert outcome.receipts[0].reason == "component_error:ValueError"
    assert "SECRET_FROM_INPUT" not in repr(outcome.receipts[0])


class _UnsafeErrorNameReducer:
    descriptor = ComponentDescriptor(
        id="unsafe-error-name",
        version="1",
        stage=ComponentStage.OBSERVATION,
    )

    async def reduce(self, observation, *, query=None, budget_tokens=None):
        unsafe_error = type("SECRET_FROM_INPUT=value", (RuntimeError,), {})
        raise unsafe_error("private diagnostic")


def test_component_error_type_is_omitted_when_not_a_safe_code():
    registry = TokenEfficiencyRegistry()
    registry.register_observation(
        _UnsafeErrorNameReducer(), mode=OptimizationMode.ENFORCE
    )

    outcome = _run(
        registry.runtime().reduce_observation(MaskedObservation("original"))
    )

    assert outcome.receipts[0].reason == "component_error"
    assert "SECRET_FROM_INPUT" not in repr(outcome.receipts[0])


class _MutatingFailure:
    descriptor = ComponentDescriptor(
        id="mutating-failure",
        version="1",
        stage=ComponentStage.REQUEST_CONTEXT,
    )

    async def optimize(self, context, *, budget_tokens=None):
        context.request["messages"][0]["content"] = "corrupted"
        raise RuntimeError("failed after mutation")


def test_request_optimizer_gets_a_copy_and_failure_cannot_mutate_input():
    request = {"messages": [{"role": "user", "content": "original"}]}
    registry = TokenEfficiencyRegistry()
    registry.register_request_optimizer(
        _MutatingFailure(), mode=OptimizationMode.ENFORCE
    )
    outcome = _run(registry.runtime().optimize_request(RequestContext(request)))

    assert request["messages"][0]["content"] == "original"
    assert outcome.context.request["messages"][0]["content"] == "original"
    assert outcome.receipts[0].status is OptimizationStatus.ERROR


class _PrefixMutator:
    descriptor = ComponentDescriptor(
        id="prefix-mutator",
        version="1",
        stage=ComponentStage.REQUEST_CONTEXT,
    )

    async def optimize(self, context, *, budget_tokens=None):
        request = dict(context.request)
        request["messages"] = [
            {"role": "system", "content": "changed"},
            {"role": "user", "content": "x"},
        ]
        return RequestOptimization(request)


class _TailReducer:
    descriptor = ComponentDescriptor(
        id="tail-reducer",
        version="1",
        stage=ComponentStage.REQUEST_CONTEXT,
    )

    async def optimize(self, context, *, budget_tokens=None):
        request = dict(context.request)
        request["messages"] = [
            *context.request["messages"][: context.frozen_prefix_messages],
            {"role": "user", "content": "x"},
        ]
        return RequestOptimization(request)


def test_request_frozen_prefix_is_byte_stable_and_tail_can_shrink():
    request = {
        "messages": [
            {"role": "system", "content": "stable"},
            {"role": "user", "content": "long " * 100},
        ]
    }

    blocked = TokenEfficiencyRegistry()
    blocked.register_request_optimizer(_PrefixMutator(), mode=OptimizationMode.ENFORCE)
    blocked_outcome = _run(
        blocked.runtime().optimize_request(
            RequestContext(request, frozen_prefix_messages=1)
        )
    )
    assert blocked_outcome.context.request == request
    assert blocked_outcome.receipts[0].reason == "frozen_prefix_guard"

    allowed = TokenEfficiencyRegistry()
    allowed.register_request_optimizer(_TailReducer(), mode=OptimizationMode.ENFORCE)
    allowed_outcome = _run(
        allowed.runtime().optimize_request(
            RequestContext(request, frozen_prefix_messages=1)
        )
    )
    assert allowed_outcome.context.request["messages"][0] == request["messages"][0]
    assert allowed_outcome.context.request["messages"][1]["content"] == "x"
    assert allowed_outcome.context.optimized_by == ("tail-reducer",)
    assert allowed_outcome.receipts[0].status is OptimizationStatus.APPLIED
    assert request["messages"][1]["content"] == "long " * 100


def test_request_shadow_mode_never_replaces_the_real_request():
    request = {
        "messages": [
            {"role": "system", "content": "stable"},
            {"role": "user", "content": "long " * 100},
        ]
    }
    registry = TokenEfficiencyRegistry()
    registry.register_request_optimizer(_TailReducer(), mode=OptimizationMode.SHADOW)
    outcome = _run(
        registry.runtime().optimize_request(
            RequestContext(request, frozen_prefix_messages=1)
        )
    )
    assert outcome.context.request == request
    assert outcome.context.optimized_by == ()
    assert outcome.receipts[0].status is OptimizationStatus.SHADOWED


def test_concise_response_policy_is_opt_in_and_preserves_safety_contract():
    registry = TokenEfficiencyRegistry()
    policy = ConciseResponsePolicy()
    registry.register_response_policy(policy, mode=OptimizationMode.ENFORCE)
    runtime = registry.runtime()

    disabled = _run(runtime.plan_response(ResponsePolicyContext(task="answer")))
    assert disabled.context.settings == StableRequestSettings()
    assert disabled.receipts[0].status is OptimizationStatus.PASSTHROUGH

    enabled = _run(
        runtime.plan_response(
            ResponsePolicyContext(
                task="answer",
                settings=StableRequestSettings(max_output_tokens=2_000),
                budget_tokens=900,
                concise_requested=True,
            )
        )
    )
    settings = enabled.context.settings
    assert settings.max_output_tokens == 900
    assert settings.verbosity == "concise"
    [instruction] = settings.instructions
    for required in (
        "negation",
        "code",
        "commands",
        "errors",
        "numbers",
        "units",
        "paths",
        "security",
    ):
        assert required in instruction
    assert enabled.context.applied_by == ("concise-response",)
    assert enabled.receipts[0].status is OptimizationStatus.APPLIED


def test_concise_response_shadow_mode_returns_stable_base_settings():
    registry = TokenEfficiencyRegistry()
    registry.register_response_policy(
        ConciseResponsePolicy(), mode=OptimizationMode.SHADOW
    )
    original = ResponsePolicyContext(task="answer", concise_requested=True)
    outcome = _run(registry.runtime().plan_response(original))
    assert outcome.context == original
    assert outcome.receipts[0].status is OptimizationStatus.SHADOWED


class _BrokenResponsePolicy:
    descriptor = ComponentDescriptor(
        id="broken-response",
        version="1",
        stage=ComponentStage.RESPONSE_POLICY,
    )

    async def plan(self, context):
        raise RuntimeError("unavailable")


class _InflatingResponsePolicy:
    descriptor = ComponentDescriptor(
        id="inflating-response",
        version="1",
        stage=ComponentStage.RESPONSE_POLICY,
    )

    def __init__(self, max_output_tokens):
        self.max_output_tokens = max_output_tokens

    async def plan(self, context):
        return ResponsePolicyPlan(
            StableRequestSettings(max_output_tokens=self.max_output_tokens)
        )


class _ForgedResponsePolicy:
    descriptor = ComponentDescriptor(
        id="forged-response",
        version="1",
        stage=ComponentStage.RESPONSE_POLICY,
    )

    async def plan(self, context):
        settings = StableRequestSettings()
        object.__setattr__(settings, "instructions", ("x" * 9_000,))
        return ResponsePolicyPlan(settings)


def test_response_policy_failure_is_fail_open():
    registry = TokenEfficiencyRegistry()
    registry.register_response_policy(
        _BrokenResponsePolicy(), mode=OptimizationMode.ENFORCE
    )
    original = ResponsePolicyContext(
        task="answer",
        settings=StableRequestSettings(instructions=("existing",)),
    )
    outcome = _run(registry.runtime().plan_response(original))
    assert outcome.context == original
    assert outcome.receipts[0].status is OptimizationStatus.ERROR
    assert outcome.receipts[0].reason == "component_error:RuntimeError"


@pytest.mark.parametrize("candidate", [2_000, None])
def test_response_policy_cannot_raise_or_remove_existing_output_cap(candidate):
    registry = TokenEfficiencyRegistry()
    registry.register_response_policy(
        _InflatingResponsePolicy(candidate), mode=OptimizationMode.ENFORCE
    )
    original = ResponsePolicyContext(
        task="answer",
        settings=StableRequestSettings(max_output_tokens=1_000),
    )

    outcome = _run(registry.runtime().plan_response(original))

    assert outcome.context == original
    assert outcome.receipts[0].status is OptimizationStatus.DEGRADED
    assert outcome.receipts[0].reason == "max_output_tokens_inflation"


def test_response_policy_cannot_exceed_context_budget_without_an_existing_cap():
    registry = TokenEfficiencyRegistry()
    registry.register_response_policy(
        _InflatingResponsePolicy(2_000), mode=OptimizationMode.ENFORCE
    )
    original = ResponsePolicyContext(task="answer", budget_tokens=1_000)

    outcome = _run(registry.runtime().plan_response(original))

    assert outcome.context == original
    assert outcome.receipts[0].reason == "max_output_tokens_inflation"


def test_response_policy_uses_the_tighter_existing_cap_or_budget():
    registry = TokenEfficiencyRegistry()
    registry.register_response_policy(
        _InflatingResponsePolicy(1_500), mode=OptimizationMode.ENFORCE
    )
    original = ResponsePolicyContext(
        task="answer",
        settings=StableRequestSettings(max_output_tokens=2_000),
        budget_tokens=1_000,
    )

    outcome = _run(registry.runtime().plan_response(original))

    assert outcome.context == original
    assert outcome.receipts[0].reason == "max_output_tokens_inflation"


def test_response_runtime_revalidates_forged_settings():
    registry = TokenEfficiencyRegistry()
    registry.register_response_policy(
        _ForgedResponsePolicy(), mode=OptimizationMode.ENFORCE
    )
    original = ResponsePolicyContext(task="answer")

    outcome = _run(registry.runtime().plan_response(original))

    assert outcome.context == original
    assert outcome.receipts[0].status is OptimizationStatus.ERROR
    assert outcome.receipts[0].reason == "component_error:ValueError"


class _LifecycleProbe:
    def __init__(self, component_id, events, *, fails=False):
        self.descriptor = ComponentDescriptor(
            id=component_id,
            version="1",
            stage=ComponentStage.OBSERVATION,
        )
        self.events = events
        self.fails = fails

    async def reduce(self, observation, *, query=None, budget_tokens=None):
        return ObservationReduction(observation.content)

    async def initialize(self, services=None):
        self.events.append((self.descriptor.id, "initialize", services))
        if self.fails:
            raise RuntimeError("private initialization diagnostic")

    def health(self):
        self.events.append((self.descriptor.id, "health", None))
        if self.fails:
            raise RuntimeError("private health diagnostic")
        return LifecycleHealth(LifecycleStatus.OK, "ready")

    async def close(self, deadline_seconds=None):
        self.events.append((self.descriptor.id, "close", deadline_seconds))
        if self.fails:
            raise RuntimeError("private close diagnostic")


def test_lifecycle_is_ordered_deduplicated_and_fail_open():
    events = []
    first = _LifecycleProbe("lifecycle-first", events)
    broken = _LifecycleProbe("lifecycle-broken", events, fails=True)
    last = _LifecycleProbe("lifecycle-last", events)
    assert isinstance(first, ComponentLifecycle)
    bindings = (
        ComponentBinding(first, OptimizationMode.ENFORCE),
        ComponentBinding(broken, OptimizationMode.ENFORCE),
        ComponentBinding(first, OptimizationMode.ENFORCE),
        ComponentBinding(last, OptimizationMode.ENFORCE),
    )
    runtime = TokenEfficiencyRuntime(
        components=StageComponents(observation_reducers=bindings)
    )

    services = {"service": "masked-artifact-store"}
    initialized = _run(runtime.initialize(services))
    assert [(component, phase) for component, phase, _ in events] == [
        ("lifecycle-first", "initialize"),
        ("lifecycle-broken", "initialize"),
        ("lifecycle-last", "initialize"),
    ]
    assert [entry.status for entry in initialized.components] == [
        LifecycleStatus.OK,
        LifecycleStatus.ERROR,
        LifecycleStatus.OK,
    ]
    assert initialized.status is LifecycleStatus.ERROR
    assert _run(runtime.initialize({"different": "services"})) is initialized
    assert len(events) == 3

    events.clear()
    health = runtime.health()
    assert [(component, phase) for component, phase, _ in events] == [
        ("lifecycle-first", "health"),
        ("lifecycle-broken", "health"),
        ("lifecycle-last", "health"),
    ]
    assert health.status is LifecycleStatus.ERROR
    assert health.components[1].detail == "component_error:RuntimeError"

    events.clear()
    closed = _run(runtime.close(deadline_seconds=1))
    assert [(component, phase) for component, phase, _ in events] == [
        ("lifecycle-last", "close"),
        ("lifecycle-broken", "close"),
        ("lifecycle-first", "close"),
    ]
    assert closed.status is LifecycleStatus.ERROR
    serialized = closed.as_dict()
    assert serialized["phase"] == "close"
    assert serialized["status"] == "error"
    assert serialized["healthy"] is False
    assert "private close diagnostic" not in repr(serialized)
    events.clear()
    assert _run(runtime.close(deadline_seconds=1)) is closed
    assert events == []
    assert runtime.health().status is LifecycleStatus.SKIPPED


def test_closed_runtime_rejects_new_component_calls():
    reducer = _CountingReducer("close-gated", "short")
    runtime = TokenEfficiencyRuntime(
        components=StageComponents(
            observation_reducers=(
                ComponentBinding(reducer, OptimizationMode.ENFORCE),
            )
        )
    )

    _run(runtime.close())
    original = MaskedObservation("a much longer original observation")
    outcome = _run(runtime.reduce_observation(original))

    assert reducer.calls == 0
    assert outcome.observation == original
    assert outcome.receipts[0].reason == "runtime_closed"


def test_close_gate_blocks_new_calls_and_runs_hook_once_concurrently():
    class BlockingClose(_CountingReducer):
        def __init__(self):
            super().__init__("blocking-close", "short")
            self.started = asyncio.Event()
            self.release = asyncio.Event()
            self.close_calls = 0

        async def close(self, deadline_seconds=None):
            self.close_calls += 1
            self.started.set()
            await self.release.wait()

    async def scenario():
        reducer = BlockingClose()
        runtime = TokenEfficiencyRuntime(
            components=StageComponents(
                observation_reducers=(
                    ComponentBinding(reducer, OptimizationMode.ENFORCE),
                )
            )
        )
        first = asyncio.create_task(runtime.close())
        await reducer.started.wait()
        second = asyncio.create_task(runtime.close())
        outcome = await runtime.reduce_observation(
            MaskedObservation("a much longer original observation")
        )
        reducer.release.set()
        reports = await asyncio.gather(first, second)
        return reducer, outcome, reports

    reducer, outcome, reports = _run(scenario())
    assert reducer.calls == 0
    assert reducer.close_calls == 1
    assert outcome.receipts[0].reason == "runtime_closed"
    assert reports[0] is reports[1]


def test_close_waits_for_an_admitted_component_call_to_drain():
    class BlockingReducer:
        descriptor = ComponentDescriptor(
            id="blocking-reducer",
            version="1",
            stage=ComponentStage.OBSERVATION,
        )

        def __init__(self):
            self.started = asyncio.Event()
            self.release = asyncio.Event()
            self.active = False
            self.closed_while_active = False
            self.close_calls = 0

        async def reduce(self, observation, *, query=None, budget_tokens=None):
            self.active = True
            self.started.set()
            await self.release.wait()
            self.active = False
            return ObservationReduction("short")

        async def close(self, deadline_seconds=None):
            self.close_calls += 1
            self.closed_while_active = self.active

    async def scenario():
        reducer = BlockingReducer()
        runtime = TokenEfficiencyRuntime(
            components=StageComponents(
                observation_reducers=(
                    ComponentBinding(reducer, OptimizationMode.ENFORCE),
                )
            )
        )
        admitted = asyncio.create_task(
            runtime.reduce_observation(
                MaskedObservation("a much longer original observation")
            )
        )
        await reducer.started.wait()
        closing = asyncio.create_task(runtime.close(deadline_seconds=1))
        await asyncio.sleep(0)
        assert runtime.health().status is LifecycleStatus.SKIPPED
        rejected = await runtime.reduce_observation(MaskedObservation("new call"))
        assert reducer.close_calls == 0
        reducer.release.set()
        await admitted
        await closing
        return reducer, rejected

    reducer, rejected = _run(scenario())
    assert reducer.close_calls == 1
    assert not reducer.closed_while_active
    assert rejected.receipts[0].reason == "runtime_closed"


def test_close_deadline_covers_drain_and_can_be_retried_safely():
    class BlockingReducer:
        descriptor = ComponentDescriptor(
            id="deadline-reducer",
            version="1",
            stage=ComponentStage.OBSERVATION,
        )

        def __init__(self):
            self.started = asyncio.Event()
            self.release = asyncio.Event()
            self.close_calls = 0

        async def reduce(self, observation, *, query=None, budget_tokens=None):
            self.started.set()
            await self.release.wait()
            return ObservationReduction("short")

        async def close(self, deadline_seconds=None):
            self.close_calls += 1

    async def scenario():
        reducer = BlockingReducer()
        runtime = TokenEfficiencyRuntime(
            components=StageComponents(
                observation_reducers=(
                    ComponentBinding(reducer, OptimizationMode.ENFORCE),
                )
            )
        )
        admitted = asyncio.create_task(
            runtime.reduce_observation(MaskedObservation("a long observation"))
        )
        await reducer.started.wait()
        timed_out = await runtime.close(deadline_seconds=0.001)
        assert reducer.close_calls == 0
        reducer.release.set()
        await admitted
        retried = await runtime.close(deadline_seconds=1)
        return reducer, timed_out, retried

    reducer, timed_out, retried = _run(scenario())
    assert timed_out.status is LifecycleStatus.ERROR
    assert timed_out.components[0].detail == "deadline_exceeded"
    assert retried.status is LifecycleStatus.OK
    assert reducer.close_calls == 1


def test_parallel_calls_are_serialized_per_component_identity():
    class StatefulReducer:
        descriptor = ComponentDescriptor(
            id="stateful-reducer",
            version="1",
            stage=ComponentStage.OBSERVATION,
        )

        def __init__(self):
            self.active = 0
            self.max_active = 0

        async def reduce(self, observation, *, query=None, budget_tokens=None):
            self.active += 1
            self.max_active = max(self.max_active, self.active)
            await asyncio.sleep(0.01)
            self.active -= 1
            return ObservationReduction("short")

    async def scenario():
        reducer = StatefulReducer()
        runtime = TokenEfficiencyRuntime(
            components=StageComponents(
                observation_reducers=(
                    ComponentBinding(reducer, OptimizationMode.ENFORCE),
                )
            )
        )
        await asyncio.gather(
            runtime.reduce_observation(MaskedObservation("first long observation")),
            runtime.reduce_observation(MaskedObservation("second long observation")),
        )
        return reducer

    assert _run(scenario()).max_active == 1


def test_runtime_clone_shares_component_lifecycle_gate(tmp_path):
    registry = TokenEfficiencyRegistry()
    registry.register_observation(
        PassthroughObservationReducer(), mode=OptimizationMode.ENFORCE
    )
    template = registry.runtime()
    clone = template.with_raw_store(
        MaskedRawArtifactStore(_workspace(tmp_path, "workspace"))
    )

    _run(template.close())

    outcome = _run(clone.reduce_observation(MaskedObservation("still open")))
    assert outcome.receipts[0].reason == "runtime_closed"


def test_lifecycle_ignores_effectively_off_components():
    events = []
    off = _LifecycleProbe("lifecycle-off", events)
    runtime = TokenEfficiencyRuntime(
        components=StageComponents(
            observation_reducers=(ComponentBinding(off, OptimizationMode.OFF),)
        ),
        default_mode=OptimizationMode.ENFORCE,
    )

    initialized = _run(runtime.initialize())
    health = runtime.health()
    closed = _run(runtime.close())

    assert events == []
    for report in (initialized, health, closed):
        assert report.status is LifecycleStatus.OK
        assert report.components == ()


def test_initialize_is_once_only_and_blocks_component_admission():
    class BlockingInitialize(_CountingReducer):
        def __init__(self):
            super().__init__("blocking-initialize", "short")
            self.started = asyncio.Event()
            self.release = asyncio.Event()
            self.initialize_calls = 0

        async def initialize(self, services=None):
            self.initialize_calls += 1
            self.started.set()
            await self.release.wait()

        def health(self):
            return LifecycleHealth(LifecycleStatus.OK, "ready")

    async def scenario():
        reducer = BlockingInitialize()
        runtime = TokenEfficiencyRuntime(
            components=StageComponents(
                observation_reducers=(
                    ComponentBinding(reducer, OptimizationMode.ENFORCE),
                )
            )
        )
        first = asyncio.create_task(runtime.initialize())
        await reducer.started.wait()
        second = asyncio.create_task(runtime.initialize())
        rejected = await runtime.reduce_observation(
            MaskedObservation("a longer original observation")
        )
        health = runtime.health()
        reducer.release.set()
        reports = await asyncio.gather(first, second)
        return reducer, rejected, health, reports

    reducer, rejected, health, reports = _run(scenario())
    assert reducer.initialize_calls == 1
    assert reducer.calls == 0
    assert rejected.receipts[0].reason == "runtime_busy"
    assert health.status is LifecycleStatus.SKIPPED
    assert reports[0] is reports[1]


def test_missing_lifecycle_hooks_are_structured_skips():
    registry = TokenEfficiencyRegistry()
    registry.register_observation(
        PassthroughObservationReducer(), mode=OptimizationMode.ENFORCE
    )
    runtime = registry.runtime()

    initialized = _run(runtime.initialize())
    health = runtime.health()
    closed = _run(runtime.close())
    for report in (initialized, health, closed):
        assert report.status is LifecycleStatus.SKIPPED
        assert report.healthy
        assert report.components[0].status is LifecycleStatus.SKIPPED
        assert report.components[0].detail == "hook_not_implemented"
