"""Pending tool approvals as first-class, resolvable session objects.

Round 95 put the risk ladder on the tool contract and made `external` ask
before acting. On a server that was only half a feature: there is no terminal
to ask, no callback was wired, and every ask fell through to deny -- safe, and
unusable. A gate nobody can open is a wall, and walls get torn down.

The shape follows OpenWorker's Inbox (OPENWORKER_RESEARCH.md, section 11,
principle 1): model the question to the human as an object with an id that
outlives the moment it was asked, so any surface -- REST, SSE consumer, a
test -- can answer it. This is the in-process MVP of that idea: the pending
approval lives on the manager, the turn awaits it, and `DELETE`ing the session
or stopping the manager denies whatever is still open (round 94: reclamation
is per-session work, not only shutdown work). What it deliberately is not yet:
durable. A restart loses pending questions and the turns waiting on them; a
journal-backed inbox with restart resume is its own round.

Every unanswered path ends in deny: timeout denies, cancellation denies,
resolving twice is a no-op, and an id from another session does not resolve
(the tenancy rule from round 80 -- a foreign id must behave like a missing
one).
"""

from __future__ import annotations

import asyncio
import json
import time
import uuid
from dataclasses import dataclass, field

from .problems import ProblemLog

#: Fallback when Settings carries no explicit approval timeout.
DEFAULT_APPROVAL_TIMEOUT = 300.0

#: Characters of tool input shown to the approver. Enough to decide, bounded
#: so a pathological payload cannot flood the event stream or the REST list.
INPUT_PREVIEW_CAP = 400

#: Command heads that may never anchor a remembered grant (Codex's
#: BANNED_PREFIX_SUGGESTIONS, cut to this harness's surface). Each is an
#: interpreter, an escalator, or a deleter: a prefix starting with one covers
#: effectively unbounded behavior, so "remember this" would remember far more
#: than the human just reviewed.
GRANT_BANNED_HEADS = (
    "bash", "sh", "zsh", "dash", "ksh", "python", "python3", "node", "deno",
    "perl", "ruby", "php", "sudo", "doas", "su", "rm", "eval", "exec", "env",
    "xargs", "find", "curl", "wget", "nc", "chmod", "chown", "dd", "mkfs",
)

#: Shell grants anchor on exactly this many leading tokens: `git reset
#: --hard X` may become "allow `git reset`", never "allow git".
GRANT_PREFIX_TOKENS = 2


def grant_candidate(tool: str, tool_input: dict) -> tuple[str, ...] | None:
    """The most a single yes may generalize to, or None.

    Shell: the command's first two tokens. Anything else: the tool name
    alone -- an approved MCP deploy tool may be remembered as a tool, never
    as a payload pattern the 400-char preview cannot actually capture. A
    shell command shorter than the prefix offers nothing to remember: a
    one-token grant is a head grant, which is what the ban list exists to
    prevent.
    """

    if tool in ("bash", "background_run"):
        tokens = str(tool_input.get("command", "")).split()
        if len(tokens) < GRANT_PREFIX_TOKENS:
            return None
        return (tool, *tokens[:GRANT_PREFIX_TOKENS])
    return (tool,)


def grant_banned(candidate: tuple[str, ...]) -> bool:
    return len(candidate) > 1 and candidate[1] in GRANT_BANNED_HEADS


#: Longest model-proposed prefix admitted; beyond this a "prefix" is just
#: the whole command wearing a hat.
GRANT_PROPOSAL_MAX_TOKENS = 6


def proposed_candidate(tool: str, tool_input: dict) -> tuple[str, ...] | None:
    """The model's own `approval_prefix`, admitted only when honest.

    Admissible iff it is the command's OWN leading tokens -- a proposal
    that is not a prefix of what will actually run is a lie about scope --
    at least two of them (a one-token grant is a head grant), at most
    GRANT_PROPOSAL_MAX_TOKENS, with an unbanned head. Anything else answers
    None and the caller falls back to the default candidate exactly as if
    nothing had been proposed: a bad proposal never widens or narrows what
    a yes means.
    """

    proposed = tool_input.get("approval_prefix")
    if (
        tool not in ("bash", "background_run")
        or not isinstance(proposed, list)
        or not (2 <= len(proposed) <= GRANT_PROPOSAL_MAX_TOKENS)
        or not all(isinstance(t, str) for t in proposed)
    ):
        return None
    tokens = str(tool_input.get("command", "")).split()
    if tokens[: len(proposed)] != list(proposed):
        return None
    candidate = (tool, *proposed)
    if grant_banned(candidate):
        return None
    return candidate


@dataclass
class PendingApproval:
    approval_id: str
    session_id: str
    tool: str
    rule: str
    message: str
    input_preview: str
    created_at: float
    future: asyncio.Future = field(repr=False)
    #: The provider's tool_use id, when the ask came from a model turn. This
    #: is the join key a restart uses to recognize "that dangling call never
    #: ran -- it was parked here when the process died".
    tool_use_id: str = ""
    #: "approval" -- resolve answers allow/deny. "question" -- resolve carries
    #: free text; the model asked the human something (`ask_user`). One
    #: machinery, because a question is an approval whose answer has words.
    kind: str = "approval"
    #: What resolve(remember=True) would grant for the rest of the session,
    #: shown to the approver up front so a remembered yes is informed.
    grant_candidate: tuple = ()
    #: True when the candidate is the model's own admitted approval_prefix
    #: rather than the harness default -- the approver should know whose
    #: generalization they are ratifying.
    grant_proposed: bool = False
    #: Set by resolve: "recorded" | "refused_banned" | None. Read back by the
    #: waiting ask() so the grant's fate is emitted from the turn's own loop.
    grant_outcome: str | None = None

    def snapshot(self) -> dict:
        return {
            "approval_id": self.approval_id,
            "session_id": self.session_id,
            "tool": self.tool,
            "tool_use_id": self.tool_use_id,
            "rule": self.rule,
            "message": self.message,
            "input_preview": self.input_preview,
            "created_at": self.created_at,
            "kind": self.kind,
            "grant_candidate": list(self.grant_candidate) or None,
            "grant_proposed": self.grant_proposed,
        }


class ApprovalBroker:
    """Routes `ask` rules to whoever is watching the session.

    `ask` matches the `Approval` callback signature and is wired into
    `default_hooks` by the SessionManager; `list`/`resolve` are the surface
    the server exposes. Single-event-loop by design, like the rest of the
    package -- no lock, no cross-process claim.
    """

    def __init__(self, *, timeout: float = DEFAULT_APPROVAL_TIMEOUT,
                 store=None) -> None:
        self.timeout = timeout
        #: Late-bound by the SessionManager once its state store exists. With
        #: a store, every ask leaves a durable row and every outcome updates
        #: it -- the audit trail, and what lets a restart tell "parked,
        #: never ran" from "dispatched, outcome unknown" (session.restore).
        self.store = store
        #: Late-bound by the SessionManager, like `store`. A human's answer to
        #: `ask_user` can carry a registered secret, and it is persisted to the
        #: durable `answer` column -- which must be masked like every other
        #: durable sink (round 82's write-site invariant). The transcript
        #: already masks it at the tool boundary; without this the approvals
        #: table was the one place it landed raw.
        self.secrets = None
        #: Optional auto-reviewer (Codex's Guardian shape, round 215). Default
        #: None -> every ask goes to the human, unchanged. When set, it is
        #: consulted BEFORE parking a human request and may only answer
        #: allow/deny/abstain for THIS action. It is an approver-substitution,
        #: never a privilege escalation: it cannot widen the catalog, change
        #: the permission mode, or relax the sandbox -- those are decided
        #: before `ask` is ever reached. `None`/abstain falls through to the
        #: human, so a broken or undecided reviewer fails toward the stricter
        #: path, never toward silent auto-approval.
        self.reviewer = None
        self._pending: dict[str, PendingApproval] = {}
        #: Session-scoped grants a human recorded with resolve(remember=True):
        #: session_id -> set of grant tuples. Runtime-only on purpose, the
        #: same doctrine as permission_mode -- a restarted process asks again;
        #: the fail-safe direction is toward the human.
        self._grants: dict[str, set] = {}
        #: Persistence faults, surfaced by the audit's problem-channel sweep.
        #: A silently-swallowed write undoes round 100's restart guarantee --
        #: a parked approval with no row restores as UNKNOWN (do-not-retry)
        #: instead of NOT_RUN (safe to retry) -- so the swallow is reported,
        #: not hidden. `problems` is a ProblemLog(list), which is what the
        #: `dir(manager)` sweep in audit.py looks for.
        self.problems = ProblemLog()

    def _persist(self, pending: PendingApproval, status: str,
                 answer: str | None = None) -> None:
        write = getattr(self.store, "write_approval", None)
        if write is None:
            return
        # Mask before the row is written, not the caller's job: the durable
        # answer column is the one sink a registered secret in a human reply
        # reached raw.
        if answer is not None and self.secrets is not None:
            answer = self.secrets.mask(answer)
        try:
            write({
                "approval_id": pending.approval_id,
                "session_id": pending.session_id,
                "tool_use_id": pending.tool_use_id,
                "tool_name": pending.tool,
                "rule": pending.rule,
                "message": pending.message,
                "input_preview": pending.input_preview,
                "status": status,
                "created_at": pending.created_at,
                "resolved_at": None if status == "pending" else time.time(),
                "kind": pending.kind,
                "answer": answer,
            })
        except Exception as error:
            # The broker must not turn a persistence fault into a denied or
            # hung turn; the in-memory path stays authoritative. But a swallowed
            # write is not harmless: the "pending" row is what a restart reads
            # to answer a parked call NOT_RUN rather than UNKNOWN, so its
            # absence silently degrades that guarantee. Report it. The message
            # omits the id so a broken store dedups to one line, not thousands.
            self.problems.append(
                f"approval persistence failed ({type(error).__name__}); a "
                "parked approval lost here restores as UNKNOWN, not NOT_RUN"
            )
            return

    # -- the Approval callback (runs inside the turn) -----------------------

    async def ask(self, ctx, call, rule) -> bool:
        session = getattr(ctx, "agent", None) and ctx.agent.state.get("session")
        session_id = getattr(session, "id", None)
        if session_id is None:
            # A surface with no session to attach the question to (a bare
            # Agent, a subagent's private state) keeps the pre-broker
            # behaviour: ask-with-nobody-to-ask is deny.
            return False
        # Mask the input *structure* before serializing, not the serialized
        # preview. `mask()` matches a secret's raw bytes, but `json.dumps`
        # escapes any non-ASCII or quote in a model-written argument to
        # `\uXXXX` / `\"`, so a credential the model wrote into a tool argument
        # survived a post-serialization mask straight into this durable approval
        # row and the SSE event a human reads. `mask_payload` scrubs each value
        # while it is still raw -- the order every other durable sink uses.
        secrets = getattr(ctx.agent, "secrets", None)
        shown = secrets.mask_payload(call.input) if secrets is not None else call.input
        preview = json.dumps(shown, default=str)[:INPUT_PREVIEW_CAP]
        # A grant the human recorded earlier answers first -- it IS a human
        # decision, so it outranks the auto-reviewer the same way the human
        # does.
        hit = self.granted(session_id, call.name, call.input)
        if hit is not None:
            self._persist(
                PendingApproval(
                    approval_id=f"apr_{uuid.uuid4().hex[:12]}",
                    session_id=session_id, tool=call.name, rule=rule.name,
                    message=rule.message, input_preview=preview,
                    created_at=time.time(),
                    future=asyncio.get_running_loop().create_future(),
                    tool_use_id=getattr(call, "id", "") or "",
                    grant_candidate=hit,
                ),
                "grant_allowed",
            )
            await ctx.emit_event(
                "approval_grant_used",
                tool=call.name, rule=rule.name, grant=list(hit),
            )
            return True
        # The model's admitted proposal wins over the harness default: it is
        # usually narrower and always honest (proposed_candidate refuses
        # anything that is not the command's own prefix). The approver sees
        # whose generalization they are ratifying via grant_proposed.
        proposal = proposed_candidate(call.name, call.input)
        candidate = proposal or grant_candidate(call.name, call.input)
        # Auto-review before parking a human (Codex's priority: hooks, then
        # Guardian, then the user). A decisive reviewer replaces the human
        # for THIS action only -- it decides the same allow/deny a human
        # would, over an already-masked preview, and cannot touch the
        # capability plan. Contained: a reviewer that raises is recorded and
        # treated as abstention, so a broken reviewer falls through to the
        # human rather than defaulting either way.
        if self.reviewer is not None:
            try:
                verdict = await self.reviewer(ctx, call, rule)
            except Exception as error:
                self.problems.append(
                    f"auto-reviewer raised on {call.name}: "
                    f"{type(error).__name__}"
                )
                verdict = None
            if verdict is not None:
                decided = bool(verdict)
                self._persist(
                    PendingApproval(
                        approval_id=f"apr_{uuid.uuid4().hex[:12]}",
                        session_id=session_id, tool=call.name, rule=rule.name,
                        message=rule.message, input_preview=preview,
                        created_at=time.time(),
                        future=asyncio.get_running_loop().create_future(),
                        tool_use_id=getattr(call, "id", "") or "",
                    ),
                    "auto_allowed" if decided else "auto_denied",
                )
                await ctx.emit_event(
                    "approval_auto_reviewed",
                    tool=call.name, rule=rule.name,
                    decision="allow" if decided else "deny",
                )
                return decided
        pending = PendingApproval(
            approval_id=f"apr_{uuid.uuid4().hex[:12]}",
            session_id=session_id,
            tool=call.name,
            rule=rule.name,
            message=rule.message,
            input_preview=preview,
            created_at=time.time(),
            future=asyncio.get_running_loop().create_future(),
            tool_use_id=getattr(call, "id", "") or "",
            grant_candidate=candidate or (),
            grant_proposed=proposal is not None,
        )
        self._pending[pending.approval_id] = pending
        self._persist(pending, "pending")
        await ctx.emit_event("approval_required", **pending.snapshot())
        try:
            # Each outcome is persisted where it is decided -- resolve()
            # writes allowed/denied, cancel_session() writes cancelled --
            # so a cancellation is not overwritten as a plain deny when the
            # parked coroutine wakes up.
            allowed = bool(await asyncio.wait_for(pending.future, self.timeout))
            # The grant's fate is emitted from the turn's own loop -- resolve()
            # runs on the HTTP side with no ctx to emit through.
            if allowed and pending.grant_outcome == "recorded":
                await ctx.emit_event(
                    "approval_grant_recorded",
                    tool=call.name, grant=list(pending.grant_candidate),
                )
            elif allowed and pending.grant_outcome == "refused_banned":
                await ctx.emit_event(
                    "approval_grant_refused",
                    tool=call.name, grant=list(pending.grant_candidate),
                    reason="prefix too broad to remember; this run was "
                           "allowed, the next will ask again",
                )
            return allowed
        except asyncio.TimeoutError:
            # The safe default is the old default: nobody answered, so no.
            self._persist(pending, "timeout")
            await ctx.emit_event(
                "approval_timeout",
                approval_id=pending.approval_id,
                tool=call.name,
                waited=self.timeout,
            )
            return False
        finally:
            self._pending.pop(pending.approval_id, None)

    async def ask_question(self, ctx, question: str) -> str | None:
        """Park the model's question to the human; the answer text, or None.

        None means declined or unanswered -- the caller words the difference
        for the model. Rides the approval machinery whole: same listing, same
        resolution surface, same expiry-on-restore, because a question is an
        approval whose answer has words.
        """

        session = getattr(ctx, "agent", None) and ctx.agent.state.get("session")
        session_id = getattr(session, "id", None)
        if session_id is None:
            return None
        text = str(question)
        secrets = getattr(ctx.agent, "secrets", None)
        if secrets is not None:
            text = secrets.mask(text)
        call = getattr(ctx, "call", None)
        pending = PendingApproval(
            approval_id=f"apr_{uuid.uuid4().hex[:12]}",
            session_id=session_id,
            tool="ask_user",
            rule="ask-user",
            message=text[:2_000],
            input_preview="",
            created_at=time.time(),
            future=asyncio.get_running_loop().create_future(),
            tool_use_id=getattr(call, "id", "") or "",
            kind="question",
        )
        self._pending[pending.approval_id] = pending
        self._persist(pending, "pending")
        await ctx.emit_event("approval_required", **pending.snapshot())
        try:
            answer = await asyncio.wait_for(pending.future, self.timeout)
            return answer if isinstance(answer, str) else None
        except asyncio.TimeoutError:
            self._persist(pending, "timeout")
            await ctx.emit_event(
                "approval_timeout",
                approval_id=pending.approval_id,
                tool="ask_user",
                waited=self.timeout,
            )
            return None
        finally:
            self._pending.pop(pending.approval_id, None)

    # -- the resolution surface (REST, tests, embedding apps) ---------------

    def granted(self, session_id: str, tool: str, tool_input: dict):
        """The recorded grant covering this call, or None.

        Shell grants are token prefixes of the command -- variable length,
        because a model-proposed prefix may be longer than the two-token
        default. Other grants are the tool name alone. Matching against the
        call's OWN tokens is the containment: a grant covers exactly the
        calls whose commands start with it, nothing else.
        """

        grants = self._grants.get(session_id)
        if not grants:
            return None
        if tool in ("bash", "background_run"):
            tokens = tuple(str(tool_input.get("command", "")).split())
            for g in grants:
                if g[0] == tool and len(g) > 1 and tokens[: len(g) - 1] == g[1:]:
                    return g
            return None
        return (tool,) if (tool,) in grants else None

    def list(self, session_id: str) -> list[dict]:
        return [p.snapshot() for p in self._pending.values()
                if p.session_id == session_id]

    def resolve(self, approval_id: str, *, session_id: str, allowed: bool,
                answer: str | None = None, remember: bool = False) -> bool:
        """Answer one pending approval. False when the id is unknown, already
        answered, or belongs to a different session -- a foreign id behaves
        exactly like a missing one.

        For a `question`, `allowed` + `answer` provides the text and a deny
        declines; the answer is persisted verbatim on the row.

        `remember` on an allow records the pending's grant_candidate for the
        rest of the session (approval-as-learning, Codex's prefix_rule):
        later calls with the same candidate skip the ask. A banned head is
        refused -- THIS run stays allowed, the rule is not recorded, and the
        refusal is emitted so the approver learns the generalization was too
        broad. `remember` on a deny is ignored: only a yes can generalize.
        """

        pending = self._pending.get(approval_id)
        if pending is None or pending.session_id != session_id:
            return False
        if pending.future.done():
            return False
        if pending.kind == "question":
            value = str(answer) if (allowed and answer is not None) else None
            pending.future.set_result(value)
            self._persist(pending,
                          "answered" if value is not None else "declined",
                          answer=value)
        else:
            verdict = bool(allowed)
            if verdict and remember and pending.grant_candidate:
                if grant_banned(pending.grant_candidate):
                    pending.grant_outcome = "refused_banned"
                else:
                    self._grants.setdefault(
                        pending.session_id, set()
                    ).add(pending.grant_candidate)
                    pending.grant_outcome = "recorded"
            pending.future.set_result(verdict)
            self._persist(pending, "allowed" if verdict else "denied")
        return True

    def cancel_session(self, session_id: str) -> int:
        """Deny everything a session still has open (delete/stop paths).

        Grants die with the session too: they were scoped to a conversation
        that no longer exists, and a successor session with the same id must
        start from asking."""

        self._grants.pop(session_id, None)
        cancelled = 0
        for pending in list(self._pending.values()):
            if pending.session_id != session_id or pending.future.done():
                continue
            pending.future.set_result(False)
            self._persist(pending, "cancelled")
            cancelled += 1
        return cancelled

    def cancel_all(self) -> int:
        return sum(self.cancel_session(p.session_id)
                   for p in list(self._pending.values()))

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: the broker's state machine refuses bad transitions itself; a resolved row cannot flip back by construction, leaving nothing to assert."
)
