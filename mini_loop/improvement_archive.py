"""The proposal archive: lineage for self-improvement (Darwin Gödel Machine).

A one-shot `propose_improvement` hill-climbs and forgets: every proposal
starts from zero knowledge of what was tried, what verified, and what the
human declined to merge. The DGM result is that the ARCHIVE is the
mechanism -- keeping every attempt with its parentage turns isolated
mutations into a population later proposals can build on or branch away
from.

This module is only the memory, deliberately: append-only JSONL under the
manager's root (beside `.teams`), one row per proposal with its parent
link, verifier-touch flag, and outcome. It launches nothing, merges
nothing, and scores nothing -- selection stays with the human, exactly
like the no-merge rule in self_improve.py. Rows are masked before they
land (an objective is operator/model prose and may quote anything).
"""

from __future__ import annotations

import json
import time
import uuid
from pathlib import Path

__all__ = ["ImprovementArchive"]

#: Rows returned by list(); the file itself is unbounded history.
MAX_LISTED = 200


class ImprovementArchive:
    def __init__(self, root: Path, *, secrets=None) -> None:
        self.root = Path(root)
        self.path = self.root / "archive.jsonl"
        self.secrets = secrets

    def record(self, proposal: dict, *, owner: str = "anonymous",
               parent_id: str | None = None) -> str:
        """Append one proposal row; returns its archive id.

        Best-effort by contract: a failed append returns the id with the
        row unwritten rather than failing the proposal -- the proposal's
        authoritative artifact is the branch commit, not this index.
        """

        proposal_id = f"imp_{uuid.uuid4().hex[:12]}"
        row = {
            "proposal_id": proposal_id,
            "parent_id": parent_id,
            "owner": owner,
            "created_at": time.time(),
            **{key: proposal.get(key) for key in (
                "objective", "verified", "rounds", "branch", "workspace",
                "diff_stat", "touches_verifiers",
            )},
        }
        if self.secrets is not None:
            row = self.secrets.mask_payload(row)
        try:
            self.root.mkdir(parents=True, exist_ok=True)
            with open(self.path, "a", encoding="utf-8") as handle:
                handle.write(json.dumps(row, default=str) + "\n")
        except OSError:
            pass
        return proposal_id

    def list(self, *, owner: str | None = None,
             limit: int = MAX_LISTED) -> list[dict]:
        """Newest-first rows, optionally narrowed to one owner.

        Owner narrowing is the same rule as /self-audit: on an
        authenticated deployment a caller reads their own lineage, never
        the fleet's.
        """

        try:
            lines = self.path.read_text(encoding="utf-8").splitlines()
        except OSError:
            return []
        rows: list[dict] = []
        for line in reversed(lines):
            if len(rows) >= max(1, limit):
                break
            try:
                row = json.loads(line)
            except json.JSONDecodeError:
                continue
            if owner is not None and row.get("owner") != owner:
                continue
            rows.append(row)
        return rows


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: an append-only index whose authoritative artifact is the branch commit it points at; a lost row loses recall, never a proposal."
)
