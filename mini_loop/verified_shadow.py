"""Shadow contracts from recorded trajectories (LongHorizon Phase 1).

LONGHORIZON_HARNESS_RESEARCH.md Phase 1: "从既有 trajectory 生成候选
TaskContractV1 / RoundPlanV1 / AuditReceiptV1... 不执行任何 Manager 建议,
不改变 completion 状态." This module is that generator: read-only over an
assembled trajectory (`TrajectoryStore.get()` shape), producing typed
candidates whose only consumers are tests and inspection.

The receipts here are DETERMINISTIC shadows -- verdict and integrity are
derived from recorded facts (the run's terminal status; whether error
events occurred), never from any text the model produced. That is the
phase gate rehearsed on real data: prose cannot cross into authority, and
the same trajectory always folds to the byte-identical checkpoint.
"""

from __future__ import annotations

import hashlib

from .verified_loop import (
    AuditReceiptV1,
    RequirementV1,
    RoundPlanV1,
    StatePatchV1,
    TaskContractV1,
    VerifiedCheckpointV1,
    apply_patch,
)

__all__ = ["shadow_from_trajectory", "fold_shadow", "evidence_problems"]

#: The one deterministic requirement a bare trajectory supports. Deriving
#: finer-grained requirements is LLM work, which Phase 1 compares against
#: gates like this one but never trusts alone.
USER_REQUEST_ID = "user-request"


def shadow_from_trajectory(trajectory: dict) -> dict:
    """Candidate contract, round plans, and receipts for one recorded run."""

    run_id = str(trajectory.get("trajectory_id") or "traj_unknown")
    input_text = str(trajectory.get("input") or "")
    contract = TaskContractV1(
        run_id=run_id,
        revision=1,
        original_request_hash=hashlib.sha256(
            input_text.encode("utf-8")
        ).hexdigest()[:16],
        requirements=(
            RequirementV1(
                id=USER_REQUEST_ID,
                # Projection only: apply_patch reads no semantics out of
                # this text, however imperative the recorded request was.
                text=input_text[:500] or "(no recorded input)",
                blocking=True,
                acceptance="the recorded run reached completed status",
                authority="deterministic",
            ),
        ),
    )

    events = trajectory.get("events") or []
    starts = [
        event for event in events
        if event.get("type") == "model_start"
        and event.get("purpose") == "agent_turn"
        and not event.get("depth")
    ]
    # Wider than the trajectory's own terminal status, deliberately: a
    # stuck-HALTED run returns normally, so its trajectory reads
    # "completed" -- but the typed `stuck{halted}` event is on the record,
    # and an audit gate that trusts terminal status alone would verify a
    # run the harness itself gave up on. Typed events only; never prose.
    saw_error = any(
        event.get("type") == "error"
        or (event.get("type") == "stuck" and event.get("halted"))
        for event in events
    )
    completed = str(trajectory.get("status")) == "completed"

    rounds: list[RoundPlanV1] = []
    receipts: list[AuditReceiptV1] = []
    for index, start in enumerate(starts):
        span = str(start.get("span_id") or f"round-{index}")
        tool_spans = tuple(
            str(event.get("span_id"))
            for event in events
            if event.get("type") == "tool_use"
            and event.get("parent_span_id") == start.get("span_id")
        )
        rounds.append(RoundPlanV1(
            round_id=span,
            base_state_revision=index,
            objective=f"shadow of recorded round {index + 1}",
            evidence_refs=(span,) + tool_spans,
        ))
        final = index == len(starts) - 1
        receipts.append(AuditReceiptV1(
            contract_hash=contract.contract_hash,
            round_id=span,
            # Deterministic shadow verdict: only the FINAL round of a run
            # that terminally completed reads complete; error events
            # anywhere taint integrity to suspect -- which the fold turns
            # into "can block, can never verify".
            verdict="complete" if (final and completed) else "incomplete",
            integrity="suspect" if saw_error else "clean",
            coverage=(USER_REQUEST_ID,) if final else (),
            evidence_refs=(span,) + tool_spans,
            verifier_ids=("shadow:terminal-status",),
        ))
    return {"contract": contract, "rounds": rounds, "receipts": receipts}


def fold_shadow(shadow: dict) -> VerifiedCheckpointV1:
    """Fold the shadow receipts through the real typed gate.

    Uses `apply_patch` itself -- the same CAS, the same rule 6 -- so the
    shadow rehearses the exact authority path the live coordinator will
    use, and a rule change breaks the rehearsal too.
    """

    contract: TaskContractV1 = shadow["contract"]
    checkpoint = VerifiedCheckpointV1(
        contract_revision=contract.revision,
        state_revision=0,
        requirements=tuple((r.id, "pending") for r in contract.requirements),
    )
    for receipt in shadow["receipts"]:
        operations: list[tuple] = []
        for requirement_id in receipt.coverage:
            if receipt.can_verify(requirement_id):
                operations.append(
                    ("set_requirement_status", requirement_id, "verified")
                )
            elif receipt.integrity != "clean":
                operations.append(
                    ("set_requirement_status", requirement_id, "untrusted")
                )
        if not operations:
            continue
        checkpoint = apply_patch(contract, checkpoint, StatePatchV1(
            base_revision=checkpoint.state_revision,
            operations=tuple(operations),
            supporting_receipts=(receipt,),
        ))
    return checkpoint


def evidence_problems(shadow: dict, trajectory: dict) -> list[str]:
    """Phase 1's evidence-coverage gate: every citation must resolve.

    A receipt whose `evidence_refs` name spans the trajectory never
    recorded is an audit citing nothing -- indistinguishable, to a reader,
    from one citing everything. And a receipt that would move a
    requirement to verified while carrying zero evidence is the emptiest
    possible authority. Both are reported by name; an empty list is the
    gate passing.
    """

    recorded = {
        str(event.get("span_id"))
        for event in (trajectory.get("events") or [])
        if event.get("span_id")
    }
    problems: list[str] = []
    for receipt in shadow.get("receipts", ()):
        dangling = [ref for ref in receipt.evidence_refs if ref not in recorded]
        if dangling:
            problems.append(
                f"receipt {receipt.round_id}: evidence refs {dangling} name "
                "no recorded span"
            )
        if receipt.coverage and receipt.verdict == "complete" and not receipt.evidence_refs:
            problems.append(
                f"receipt {receipt.round_id}: would verify "
                f"{list(receipt.coverage)} while citing no evidence at all"
            )
    return problems


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: a read-only shadow generator whose outputs feed "
    "tests and inspection; the rules it rehearses are enforced by "
    "verified_loop.apply_patch, which it calls rather than reimplements."
)
