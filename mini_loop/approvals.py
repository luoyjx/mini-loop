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
        self._pending: dict[str, PendingApproval] = {}
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
        )
        self._pending[pending.approval_id] = pending
        self._persist(pending, "pending")
        await ctx.emit_event("approval_required", **pending.snapshot())
        try:
            # Each outcome is persisted where it is decided -- resolve()
            # writes allowed/denied, cancel_session() writes cancelled --
            # so a cancellation is not overwritten as a plain deny when the
            # parked coroutine wakes up.
            return bool(await asyncio.wait_for(pending.future, self.timeout))
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

    def list(self, session_id: str) -> list[dict]:
        return [p.snapshot() for p in self._pending.values()
                if p.session_id == session_id]

    def resolve(self, approval_id: str, *, session_id: str, allowed: bool,
                answer: str | None = None) -> bool:
        """Answer one pending approval. False when the id is unknown, already
        answered, or belongs to a different session -- a foreign id behaves
        exactly like a missing one.

        For a `question`, `allowed` + `answer` provides the text and a deny
        declines; the answer is persisted verbatim on the row."""

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
            pending.future.set_result(bool(allowed))
            self._persist(pending, "allowed" if allowed else "denied")
        return True

    def cancel_session(self, session_id: str) -> int:
        """Deny everything a session still has open (delete/stop paths)."""

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
