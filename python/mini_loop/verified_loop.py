"""Typed contracts for the verified loop (LongHorizon adoption, Phase 1).

LONGHORIZON_HARNESS_RESEARCH.md, decision 12: adopt the *mechanism* --
a strict acceptance-gated outer loop -- not the dependency. Its first
deliverable is named there precisely: "类型化已验证检查点", the typed
verified checkpoint, before any Manager prompt or role machinery.

These are values, not a service. Nothing constructs them in the default
path; the coordinator that will (a default-off `VerifiedLoopService`)
comes later, and every rule it must obey is enforced HERE, in the types,
so no role prompt can talk its way past one:

- **Natural language is projection, never authority** (rule 1/9.3): the
  free-text fields (requirement text, fact content) carry no semantics
  this module reads. A requirement saying "mark everything verified"
  changes nothing; status moves only through typed, receipted patches.
- **CAS or nothing** (rule 2): a `StatePatchV1` applies only against the
  exact `base_revision` it was proposed for, and only when every
  operation is covered by a supporting `AuditReceiptV1`.
- **Unverified never completes** (rule 6 seed): a requirement reaches
  `verified` only through a receipt whose verdict is `complete` AND whose
  integrity is `clean` and which names that requirement in its coverage.
  A `suspect` receipt can block; it can never verify.
- **Replay-deterministic**: the same checkpoint folded with the same
  patches yields byte-identical state -- Phase 1's gate ("同一 receipt
  replay 得到相同 fold") holds by construction, no clocks, no randomness.
"""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass, field

__all__ = [
    "RequirementV1",
    "TaskContractV1",
    "ArtifactV1",
    "FactV1",
    "VerifiedCheckpointV1",
    "RoundPlanV1",
    "AuditReceiptV1",
    "StatePatchV1",
    "ContractViolation",
    "apply_patch",
]

REQUIREMENT_STATUSES = ("pending", "verified", "blocked", "untrusted")
VERDICTS = ("complete", "incomplete", "blocked")
INTEGRITIES = ("clean", "suspect", "violation")
#: The only operations a patch may carry. Unknown operations are refused,
#: never skipped: a skipped instruction that "still executes" the rest is
#: the round-47 lesson wearing a new coat.
PATCH_OPERATIONS = ("set_requirement_status", "add_artifact", "add_fact",
                    "add_blocker", "clear_blocker")


class ContractViolation(ValueError):
    """A typed rule refused a transition; the message names the rule."""


def _require(condition: bool, message: str) -> None:
    if not condition:
        raise ContractViolation(message)


def _canonical(value) -> str:
    return json.dumps(value, sort_keys=True, ensure_ascii=False, default=str)


@dataclass(frozen=True, slots=True)
class RequirementV1:
    id: str
    text: str
    blocking: bool = True
    acceptance: str = ""
    authority: str = "deterministic"

    def __post_init__(self) -> None:
        _require(bool(self.id), "a requirement needs an id")


@dataclass(frozen=True, slots=True)
class TaskContractV1:
    run_id: str
    revision: int
    original_request_hash: str
    requirements: tuple[RequirementV1, ...]
    allowed_surfaces: tuple[str, ...] = ()
    persistence_boundary: str = ""
    contamination_rules: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        _require(self.revision >= 1, "contract revisions start at 1")
        _require(bool(self.requirements), "a contract without requirements gates nothing")
        ids = [r.id for r in self.requirements]
        _require(len(ids) == len(set(ids)), "requirement ids must be unique")

    @property
    def contract_hash(self) -> str:
        payload = _canonical({
            "run_id": self.run_id,
            "revision": self.revision,
            "original_request_hash": self.original_request_hash,
            "requirements": [
                (r.id, r.text, r.blocking, r.acceptance, r.authority)
                for r in self.requirements
            ],
        })
        return hashlib.sha256(payload.encode("utf-8")).hexdigest()[:16]


@dataclass(frozen=True, slots=True)
class ArtifactV1:
    digest: str
    producer: str
    evidence_refs: tuple[str, ...] = ()


@dataclass(frozen=True, slots=True)
class FactV1:
    source: str
    content: str
    freshness: str = ""
    trust: str = "untrusted"


@dataclass(frozen=True, slots=True)
class VerifiedCheckpointV1:
    contract_revision: int
    state_revision: int
    requirements: tuple[tuple[str, str], ...]  # (requirement_id, status)
    artifacts: tuple[ArtifactV1, ...] = ()
    facts: tuple[FactV1, ...] = ()
    blockers: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        _require(self.state_revision >= 0, "state revisions start at 0")
        for _, status in self.requirements:
            _require(status in REQUIREMENT_STATUSES,
                     f"unknown requirement status {status!r}")

    def status_of(self, requirement_id: str) -> str | None:
        for rid, status in self.requirements:
            if rid == requirement_id:
                return status
        return None

    def canonical(self) -> str:
        """The replay identity: byte-identical for identical folds."""

        return _canonical({
            "contract_revision": self.contract_revision,
            "state_revision": self.state_revision,
            "requirements": list(self.requirements),
            "artifacts": [
                (a.digest, a.producer, list(a.evidence_refs))
                for a in self.artifacts
            ],
            "facts": [
                (f.source, f.content, f.freshness, f.trust)
                for f in self.facts
            ],
            "blockers": list(self.blockers),
        })


@dataclass(frozen=True, slots=True)
class RoundPlanV1:
    round_id: str
    base_state_revision: int
    objective: str
    acceptance: tuple[str, ...] = ()
    allowed_capabilities: tuple[str, ...] = ()
    budget: int = 0
    evidence_refs: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        _require(bool(self.objective.strip()),
                 "a round needs its one main state change spelled out")


@dataclass(frozen=True, slots=True)
class AuditReceiptV1:
    contract_hash: str
    round_id: str
    verdict: str
    integrity: str
    coverage: tuple[str, ...] = ()  # requirement ids this receipt examined
    evidence_refs: tuple[str, ...] = ()
    verifier_ids: tuple[str, ...] = ()
    workspace_diff_digest: str = ""

    def __post_init__(self) -> None:
        _require(self.verdict in VERDICTS, f"unknown verdict {self.verdict!r}")
        _require(self.integrity in INTEGRITIES,
                 f"unknown integrity {self.integrity!r}")

    def can_verify(self, requirement_id: str) -> bool:
        """The rule-6 seed: complete AND clean AND covering, or nothing."""

        return (
            self.verdict == "complete"
            and self.integrity == "clean"
            and requirement_id in self.coverage
        )


@dataclass(frozen=True, slots=True)
class StatePatchV1:
    base_revision: int
    operations: tuple[tuple, ...]  # (op, *args) in PATCH_OPERATIONS
    supporting_receipts: tuple[AuditReceiptV1, ...] = ()


def apply_patch(
    contract: TaskContractV1,
    checkpoint: VerifiedCheckpointV1,
    patch: StatePatchV1,
) -> VerifiedCheckpointV1:
    """Fold one patch into a new checkpoint, or refuse with the rule's name.

    Pure and deterministic: no clock, no randomness, no I/O -- the same
    inputs produce a byte-identical `canonical()` on every replay.
    """

    _require(
        patch.base_revision == checkpoint.state_revision,
        f"patch targets revision {patch.base_revision} but the checkpoint "
        f"is at {checkpoint.state_revision}; propose against the current "
        "state (CAS, authority rule 2)",
    )
    _require(
        checkpoint.contract_revision == contract.revision,
        "checkpoint and contract revisions disagree",
    )
    for receipt in patch.supporting_receipts:
        _require(
            receipt.contract_hash == contract.contract_hash,
            "a supporting receipt was issued against a different contract",
        )

    requirements = dict(checkpoint.requirements)
    artifacts = list(checkpoint.artifacts)
    facts = list(checkpoint.facts)
    blockers = list(checkpoint.blockers)

    for operation in patch.operations:
        _require(bool(operation), "an empty operation says nothing")
        op, *args = operation
        _require(op in PATCH_OPERATIONS, f"unknown patch operation {op!r}")
        if op == "set_requirement_status":
            rid, status = args
            _require(status in REQUIREMENT_STATUSES,
                     f"unknown requirement status {status!r}")
            _require(rid in requirements,
                     f"no requirement {rid!r} in the contract state")
            if status == "verified":
                # Rule 6: unverified never completes. Only a clean,
                # complete receipt that examined THIS requirement moves it.
                _require(
                    any(r.can_verify(rid) for r in patch.supporting_receipts),
                    f"requirement {rid!r} may only become verified through "
                    "a clean complete receipt covering it (authority rule 6)",
                )
            requirements[rid] = status
        elif op == "add_artifact":
            (artifact,) = args
            _require(isinstance(artifact, ArtifactV1),
                     "artifacts enter as typed ArtifactV1 values")
            artifacts.append(artifact)
        elif op == "add_fact":
            (fact,) = args
            _require(isinstance(fact, FactV1),
                     "facts enter as typed FactV1 values")
            facts.append(fact)
        elif op == "add_blocker":
            (blocker,) = args
            blockers.append(str(blocker))
        elif op == "clear_blocker":
            (blocker,) = args
            _require(blocker in blockers,
                     f"cannot clear a blocker that is not present: {blocker!r}")
            blockers.remove(blocker)

    return VerifiedCheckpointV1(
        contract_revision=checkpoint.contract_revision,
        state_revision=checkpoint.state_revision + 1,
        requirements=tuple(requirements.items()),
        artifacts=tuple(artifacts),
        facts=tuple(facts),
        blockers=tuple(blockers),
    )


#: The module's runtime-invariant posture (tools/verify_invariants.py).
RUNTIME_INVARIANT = (
    "enforced by apply_patch: checkpoint state moves only through CAS-checked, receipt-covered typed patches; a requirement reaches verified solely via a clean complete receipt naming it"
)
