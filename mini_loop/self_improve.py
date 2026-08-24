"""Proposing harness changes through the verified loop (self-evolution, L4).

The composition, not a new mechanism: VerifiedLoopService already runs
manager -> executor -> auditor with receipt-gated completion ("未验证不完成"),
and worktrees already give a session an isolated checkout on its own
branch. This module points that machinery at an improvement objective and
stops exactly where a machine must stop:

* **propose, never merge.** The product is a verified diff on an isolated
  branch plus the audit receipt; landing it is a human act, on the same
  authorization footing as every other gate here (approvals fall to the
  human, cron re-arms by hand, skills recatalogue by restart).
* **only in a git checkout.** A proposal must be diffable and revertible;
  a session whose workspace is not a git repository is refused, because
  an unreviewable "improvement" is just a mutation.
* **the auditor is a command, not an opinion.** acceptance_command is
  typically the target repo's own suite and guards -- the regression
  half; the paired benchmark (benchmark.py) is the capability half an
  operator runs on the proposal before merging.
"""

from __future__ import annotations

from typing import Any

from .verified_loop_service import VerifiedLoopService
from .worktrees import is_git_repo

__all__ = ["propose_improvement"]


async def propose_improvement(
    session: Any,
    objective: str,
    *,
    acceptance_command: str,
    max_rounds: int = 3,
) -> dict:
    """Run the verified loop at an improvement objective; return the proposal."""

    agent = session.agent
    workspace = agent.workspace
    if not acceptance_command.strip():
        raise ValueError(
            "an improvement needs an acceptance command; without one the "
            "auditor has nothing to verify and 'verified' would be a vibe"
        )
    if not is_git_repo(workspace):
        raise ValueError(
            "self-improvement runs only in a git checkout (worktree): a "
            "proposal must be diffable and revertible, or it is just a "
            "mutation"
        )

    outcome = await VerifiedLoopService(session).run_task(
        objective, acceptance_command=acceptance_command, max_rounds=max_rounds,
    )

    # The proposal artifact is a COMMIT on the isolated branch: durable,
    # reviewable, revertible -- and never merged from here. The message is
    # fixed text; the objective is model/operator prose and does not get
    # interpolated into a shell command.
    status = agent.toolset.run_bash_result("git status --porcelain")
    if (status.stdout or "").strip():
        staged = agent.toolset.run_bash_result("git add -A")
        committed = agent.toolset.run_bash_result(
            "git commit -m 'self-improvement proposal' --no-verify"
        )
        if staged.exit_code == 0 and committed.exit_code == 0:
            diff_stat = agent.toolset.run_bash_result(
                "git diff --stat HEAD~1 HEAD"
            )
            diff_text = (diff_stat.stdout or "").strip()
        else:
            # The commit failed; naming HEAD~1 now would describe someone
            # else's change. The working tree still holds the proposal --
            # say so and show what it touches.
            diff_text = (
                "(uncommitted: git add/commit failed in the worktree; the "
                "proposal is the working-tree change below)\n"
                + (status.stdout or "").strip()
            )
    else:
        diff_text = "(no changes)"
    branch = agent.toolset.run_bash_result("git rev-parse --abbrev-ref HEAD")
    proposal = {
        "objective": objective,
        "verified": outcome.get("status") == "complete",
        "rounds": outcome.get("rounds"),
        "summary": outcome.get("summary"),
        "workspace": str(workspace),
        "branch": (branch.stdout or "").strip(),
        "diff_stat": diff_text,
        # The human's next move, stated by the machine that must not make it.
        "next": "review the diff on the branch; merge only after the paired "
                "benchmark and your own read agree it is an improvement",
    }
    await agent._send(
        "improvement_proposed",
        objective=objective[:200],
        verified=proposal["verified"],
        branch=proposal["branch"],
        diff_stat=proposal["diff_stat"][:500],
    )
    return proposal


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: a composition of the verified loop and a git "
    "worktree; the no-merge rule holds because no merge code exists here "
    "to misfire."
)
