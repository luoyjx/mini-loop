"""The verified loop coordinator, minimal path (LongHorizon priority 4).

Default OFF. Nothing in the default assembly constructs this service; a
caller opts in explicitly. The shape is the research doc's Phase 2 --
"最小可用的单 Manager、单 Executor、单 Auditor 路径... 先支持代码/文件任务
和确定性 verifier" -- taken at its word:

- the MANAGER of this first cut is a deterministic policy (the round
  objective is the task plus the previous round's evidence-backed
  feedback), not a model call;
- the EXECUTOR is an ordinary worker subagent through the existing
  provider seam -- real capabilities, real telemetry, every side effect
  through the ordinary tool pipeline (authority rule 4);
- the AUDITOR of this first cut is the deterministic verifier: the
  caller's acceptance command, run through the sandboxed toolset, its
  exit code becoming an `AuditReceiptV1`. Rule 5 says deterministic
  first and LLM only for semantics; the model-role constructors
  (round 211) are ready for the semantic pass when it earns its cost.

State moves only through `apply_patch` (round 203): CAS, receipt-gated
verification, unverified-never-completes. A run that exhausts its rounds
returns `unverified` with the stop stated first (round 187's rule) --
never a completed-looking answer.
"""

from __future__ import annotations

import asyncio
import hashlib

from .verified_loop import (
    AuditReceiptV1,
    RequirementV1,
    StatePatchV1,
    TaskContractV1,
    VerifiedCheckpointV1,
    apply_patch,
)

__all__ = ["VerifiedLoopService"]

REQUIREMENT_ID = "acceptance"


class VerifiedLoopService:
    """Drive one task through execute -> verify -> fold rounds."""

    def __init__(self, session) -> None:
        self.session = session

    async def run_task(
        self,
        request: str,
        *,
        acceptance_command: str,
        max_rounds: int = 3,
        integrity_probe=None,
    ) -> dict:
        """Drive the rounds; `integrity_probe` guards the auditor itself.

        The probe is a zero-argument callable returning a fingerprint of
        the acceptance instruments. Sampled once before the first round
        and again immediately before EVERY acceptance run: an executor
        that weakens the instruments mid-round -- even one that restores
        them afterwards -- is judged by a different auditor than the one
        the task started with, and the receipt for that round says
        `integrity: "suspect"` instead of letting a green exit code speak
        alone. (The DGM incident: a self-improvement scored 2.0/2.0 by
        deleting the detection markers it was asked to satisfy.)
        """

        agent = self.session.agent
        instruments_baseline = integrity_probe() if integrity_probe else None
        contract = TaskContractV1(
            run_id=self.session.id,
            revision=1,
            original_request_hash=hashlib.sha256(
                request.encode("utf-8")
            ).hexdigest()[:16],
            requirements=(
                RequirementV1(
                    id=REQUIREMENT_ID,
                    text=request[:500],
                    blocking=True,
                    acceptance=acceptance_command,
                    authority="deterministic",
                ),
            ),
        )
        checkpoint = VerifiedCheckpointV1(
            contract_revision=1, state_revision=0,
            requirements=((REQUIREMENT_ID, "pending"),),
        )
        receipts: list[AuditReceiptV1] = []
        feedback = ""

        for round_no in range(1, max_rounds + 1):
            objective = request if not feedback else (
                f"{request}\n\nPrevious round's verification failed. "
                f"Evidence:\n{feedback}"
            )
            await self.session.emit({
                "type": "verified_round", "round": round_no,
                "objective": objective[:2000],
            })
            # The executor: an ordinary worker subagent -- real
            # capabilities, telemetry in the loop, effects through the
            # ordinary pipeline.
            summary = await agent._run_subagent(objective, "worker")

            # The deterministic auditor: the acceptance command, sandboxed.
            # Probe BEFORE the run: the question is whether the instruments
            # that are ABOUT to judge this round are still the ones the
            # task started with.
            tampered = (
                instruments_baseline is not None
                and integrity_probe() != instruments_baseline
            )
            result = await asyncio.to_thread(
                agent.toolset.run_bash_result, acceptance_command
            )
            # A changed auditor cannot verify: verified_loop.py only lets a
            # `clean` receipt support a requirement, so a tampered round is
            # judged not-passed here rather than tripping the gate below.
            passed = (result.exit_code == 0 and not result.timed_out
                      and not tampered)
            evidence = [f"exit:{result.exit_code}",
                        f"command:{acceptance_command[:120]}"]
            if tampered:
                evidence.append("instruments:changed-since-baseline")
            receipt = AuditReceiptV1(
                contract_hash=contract.contract_hash,
                round_id=f"round-{round_no}",
                verdict="complete" if passed else "incomplete",
                integrity="suspect" if (result.timed_out or tampered) else "clean",
                coverage=(REQUIREMENT_ID,),
                evidence_refs=tuple(evidence),
                verifier_ids=("command",),
            )
            receipts.append(receipt)
            await self.session.emit({
                "type": "verified_receipt", "round": round_no,
                "verdict": receipt.verdict, "integrity": receipt.integrity,
                "exit_code": result.exit_code,
            })
            if passed:
                checkpoint = apply_patch(contract, checkpoint, StatePatchV1(
                    base_revision=checkpoint.state_revision,
                    operations=(
                        ("set_requirement_status", REQUIREMENT_ID, "verified"),
                    ),
                    supporting_receipts=(receipt,),
                ))
                await self.session.emit({
                    "type": "verified_checkpoint",
                    "state_revision": checkpoint.state_revision,
                    "status": "complete",
                })
                return {
                    "status": "complete",
                    "rounds": round_no,
                    "checkpoint": checkpoint,
                    "receipts": receipts,
                    "summary": summary,
                    "integrity": "clean",
                }
            feedback = result.render()[-2000:]
            if tampered:
                feedback = (
                    "[integrity] the acceptance instruments changed after "
                    "the task began; a passing exit code from a changed "
                    "auditor does not verify. Restore the instruments, or "
                    "make changing them the explicit objective.\n" + feedback
                )

        # Rounds exhausted, requirement unverified: the stop leads (round
        # 187's rule) and the state says pending, never a quiet success.
        await self.session.emit({
            "type": "verified_checkpoint",
            "state_revision": checkpoint.state_revision,
            "status": "unverified",
        })
        return {
            "status": "unverified",
            "rounds": max_rounds,
            "checkpoint": checkpoint,
            "receipts": receipts,
            "summary": (
                f"[stopped after {max_rounds} rounds without verification]\n"
                f"Last evidence:\n{feedback}"
            ),
            "integrity": (
                "suspect"
                if any(r.integrity == "suspect" for r in receipts)
                else "clean"
            ),
        }


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: the coordinator holds no authority of its own -- "
    "every state transition goes through verified_loop.apply_patch, whose "
    "CAS and receipt gates are the enforced invariant."
)
