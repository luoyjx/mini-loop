"""A session = one persistent Agent + an event bus + a run lock.

The Agent holds the conversation; the session wraps it with the machinery a
server needs:

  * `emit` fans every agent event out to all live SSE subscribers and appends
    it to a bounded backlog (so a late subscriber can catch up);
  * `lock` serializes runs *within* one session -- a session is a single
    conversation, so two overlapping messages would corrupt its history.
    Different sessions hold different locks and run fully concurrently.
"""

from __future__ import annotations

import asyncio
import contextlib
import hashlib
import inspect
import json
import time
from collections import deque
from collections.abc import Callable
from pathlib import Path

from .agent import Agent, unanswered_tool_uses
from .run_context import RunContext
from .actions import UNKNOWN_RESULT
from .blocks import block_field
from .storage import NullStateStore, SessionRecord, StateStore
from .trajectory import TrajectoryStore

BACKLOG = 200  # recent events retained for replay to new subscribers
#: Per-subscriber queue bound. A subscriber that stops reading -- a backgrounded
#: browser tab, a slow or stalled SSE client whose `yield` the ASGI server is
#: backpressuring -- otherwise accumulates every event for the life of the
#: session. The backlog was bounded and the live queues were not, so one stalled
#: client grew memory without limit while the backlog it could resume from
#: stayed capped. Above the backlog, so a subscriber still within replay range is
#: never dropped; on overflow the oldest queued event is discarded and the
#: client, seeing the seq gap, resumes from the backlog via `last-event-id`.
SUBSCRIBER_QUEUE_MAX = 2000
#: Per-steer size and queue bounds. Every queued steer is joined into one
#: `<user_interjection>` message and injected into the transcript, so an
#: unbounded steer -- or an unbounded number of them on a busy session -- floods
#: the context exactly as an unbounded team message did (round 50's
#: `MessageBus.MAX_CONTENT`/`MAX_INBOX`), the bound applied there and not here.
MAX_STEER_CHARS = 16_000
MAX_STEER_QUEUE = 100


class LeaseLost(RuntimeError):
    """Another process holds this session."""


class AgentSession:
    def __init__(
        self,
        session_id: str,
        workspace: Path,
        *,
        system: str | None = None,
        event_sink: Callable[[dict], object] | None = None,
        trajectory_store: TrajectoryStore | None = None,
        state_store: StateStore | None = None,
    ) -> None:
        self.id = session_id
        self.workspace = workspace
        self.system = system
        self.created_at = time.time()
        self.status = "idle"  # idle | running | error
        # Set by the HTTP surface; process-local callers own everything.
        self.owner = "anonymous"
        self.run_count = 0
        # Risk -> decision posture for this session's permission hook. Runtime
        # state, deliberately not persisted: a restored session asks again.
        self.permission_mode = "interactive"
        # Messages sent while a turn is running. OpenWorker's gateway turns
        # them into steering; ours used to answer 409 and drop the caller's
        # words on the floor. Drained by `steering_injector` at the next loop
        # round, so mid-turn input reaches the model mid-turn.
        self._steering: list[str] = []

        # Optional global observer (metrics, logging, persistence). Sync or async.
        self._event_sink = event_sink
        # Durable state: the transcript and event cursor a restart resumes from.
        self.state_store = state_store or NullStateStore()
        self._persisted_messages = 0
        self._transcript_epoch = 1
        self._persisted_refs: list = []
        #: Why durable state is not being written, or None. Public and reported:
        #: this used to be a private field that was assigned and read by nothing,
        #: so a store that opened fine and then failed every write left the agent
        #: running, the console clean, and the session unrecoverable.
        self.persist_error: str | None = None
        #: Why the injected event sink stopped receiving events, or None --
        #: the sink's failures are contained (an observer must not kill the
        #: turn it observes) but never silent.
        self.sink_error: str | None = None
        # Tool calls whose outcome the crash left unknown.
        self._unknown_tool_uses: tuple[str, ...] = ()
        # Set by the manager when a durable store makes concurrent processes
        # possible at all.
        self.lease_owner: str | None = None
        self.lease_ttl: float = 120.0
        # True once this process has actually *held* the lease (a successful
        # `_require_lease` acquire), not merely been assigned an owner id. A
        # renewal that fails matters only after that: a session that held the
        # lease and lost it mid-turn must stop, but one that never acquired it
        # (a restored session whose claim lost to a still-held lease) has no
        # lease to lose and must not raise on a renewal it was never going to win.
        self.lease_confirmed: bool = False
        # The in-flight turn, so it can be observed and stopped. Without this a
        # runaway agent can only be ended by killing the process.
        self._running: asyncio.Task | None = None
        self._cancel_reason: str | None = None
        self._accepting_runs = True
        self._closed_reason = ""
        self._trajectory_store = trajectory_store
        self._active_trajectory_id: str | None = None
        self._trajectory_started = 0.0
        self._trajectory_had_error = False
        self._trajectory_recording_error: str | None = None
        self._trajectory_count = 0
        if trajectory_store is not None:
            try:
                self._trajectory_count = trajectory_store.count(session_id)
            except Exception as error:
                self._trajectory_recording_error = f"{type(error).__name__}: {error}"

        self.lock = asyncio.Lock()
        self._emit_lock = asyncio.Lock()
        self._subscribers: set[asyncio.Queue] = set()
        self._backlog: deque[dict] = deque(maxlen=BACKLOG)
        self._seq = 0

        self._agent: Agent | None = None  # attached by the manager after construction

    @property
    def agent(self) -> "Agent | None":
        return self._agent

    @agent.setter
    def agent(self, value: "Agent | None") -> None:
        """Attach the agent; the transcript guard rides along.

        A property rather than a bare attribute because the manager attaches
        agents at four different sites (build, restore, teammate spawn,
        workflow worker) and each future one would otherwise have to remember
        the guard -- the round-144 "a new path inherits the need" defect
        class, avoided structurally.
        """

        self._agent = value
        if value is not None:
            value.transcript_guard = self._transcript_guard

    def _transcript_guard(self, messages: list) -> None:
        """Model-visible means logged -- enforced, not assumed.

        DeepSeek Harness states this as an architecture rule with a runtime
        invariant behind it: anything that reaches a model request must be
        reconstructable from the durable log. Our injectors and steering
        extend `agent.messages` between event beats, so the log could lag the
        request by exactly the injected input; a crash then left a transcript
        whose next assistant message answers content the record never held.

        Flush first -- that is the fix (the injected tail becomes durable
        before the model sees it). Then assert coverage: the current epoch
        must hold as many messages as the request carries. Values on disk are
        masked, so the assertion compares counts, not bytes; the *shape* of
        the request must be reconstructable, while secrets stay maskable.
        """

        if isinstance(self.state_store, NullStateStore):
            return  # no log to cover the request; nothing is claimed durable
        if self.persist_error is not None:
            return  # persistence already degraded and reported; keep running
        if not self._accepting_runs:
            # Teardown: `delete()` discards the durable rows *on purpose* while
            # the cancelled turn may still reach one more request. A log that
            # is being deleted cannot and need not cover it.
            return
        try:
            self._flush_messages()
        except LeaseLost:
            raise
        except Exception as error:
            # Same degrade contract as the event path: a persistence fault is
            # reported, never a stalled agent.
            self.persist_error = f"{type(error).__name__}: {error}"
            return
        count = self.state_store.message_count(self.id)
        if count != len(messages):
            from .invariants import InvariantError

            raise InvariantError(
                "mini_loop.session",
                f"model request carries {len(messages)} messages but the "
                f"durable epoch holds {count}; a model-visible input bypassed "
                "the log",
            )

    # -- event bus --
    async def _capture_event(self, event: dict) -> dict:
        event = dict(event)
        trajectory_fields = event.pop("_trajectory_fields", None)
        # A streamed turn emits one delta per token. Persisting them would put
        # thousands of fragment rows in the durable log per turn and bury the
        # events that describe what actually happened. Progress is worth showing
        # live and is not worth keeping.
        ephemeral = bool(event.pop("_ephemeral", False))
        self._seq += 1
        event = {**event, "seq": self._seq, "ts": time.time(), "session": self.id}
        # Masked here, once, before the event reaches any of its three
        # destinations: the durable table, the trajectory, and every SSE
        # subscriber. `_flush_messages` had been masking the transcript for
        # rounds while this path -- in the same function -- persisted the same
        # text raw, so `messages` was clean and `events` was not.
        agent = self.agent
        secrets = getattr(agent, "secrets", None) if agent is not None else None
        if secrets is not None:
            from .storage import _json_safe

            event = secrets.mask_payload(_json_safe(event))
            if isinstance(trajectory_fields, dict):
                trajectory_fields = secrets.mask_payload(
                    _json_safe(trajectory_fields)
                )

        if ephemeral:
            # Kept on the event rather than only in this frame: the backlog and
            # the SSE client both need to tell live progress from the record,
            # and the flag was being consumed before either could see it.
            event["ephemeral"] = True
        if str(event.get("type", "")).startswith("workflow_"):
            event.setdefault("sequence", self._seq)
            event.setdefault("occurred_at", event["ts"])
        # Durable first: a crash after this point still replays the event, and
        # a duplicate is cheaper than a hole in the cursor an SSE client resumes
        # from. Persistence failures degrade to a reported error, never a
        # stalled agent -- same contract the trajectory sink already follows.
        if not ephemeral:
            try:
                self.state_store.append_event(self.id, event)
                self._flush_messages()
            except LeaseLost:
                # Not a persistence fault to degrade past: another process owns
                # this session now, so stopping is the point. Propagate it the
                # way `_require_lease` does, rather than swallowing it into
                # `persist_error` and continuing to drive a session we lost.
                raise
            except Exception as error:
                self.persist_error = f"{type(error).__name__}: {error}"
        if self._active_trajectory_id is not None and not ephemeral:
            event["trajectory_id"] = self._active_trajectory_id
            event["trace_id"] = self._active_trajectory_id
            event["group_id"] = self.id
            if event.get("type") == "error":
                self._trajectory_had_error = True
            if self._trajectory_store is not None:
                try:
                    recorded_event = (
                        {**event, **trajectory_fields}
                        if isinstance(trajectory_fields, dict) else event
                    )
                    for key in (
                        "type", "seq", "ts", "session", "trajectory_id", "trace_id",
                        "group_id", "agent", "depth",
                    ):
                        if key in event:
                            recorded_event[key] = event[key]
                    await asyncio.to_thread(
                        self._trajectory_store.append,
                        self._active_trajectory_id,
                        recorded_event,
                    )
                except Exception as error:  # observability must not stop the agent
                    self._trajectory_recording_error = f"{type(error).__name__}: {error}"
        return event

    @staticmethod
    def _digest(messages) -> str:
        """Content fingerprint of a transcript prefix.

        Kept for tests and diagnostics. It is *not* on the flush path: hashing
        the whole prefix on every event is O(bytes) per event and therefore
        O(n x events) per session -- measured at 5.5 ms per pass over a 1.6 MB
        transcript, so ~550 ms of pure overhead across 100 events. Identity
        comparison replaces it; see `_transcript_was_rewritten`.
        """

        from .storage import _json_safe

        return hashlib.sha256(
            json.dumps(_json_safe(list(messages)), sort_keys=True, ensure_ascii=False).encode()
        ).hexdigest()[:16]

    def _transcript_was_rewritten(self, messages: list) -> bool:
        """True when the live transcript no longer extends what was persisted.

        Compaction rewrites `agent.messages` in place -- sometimes shortening it
        (auto-compaction replaces history with a summary), sometimes editing
        entries without changing the length (tool-result snipping). An
        append-only table cannot mirror either by index, so both are detected
        and answered with a new epoch rather than a corrupt splice.
        """

        if self._persisted_messages == 0:
            return False
        if len(messages) < self._persisted_messages:
            return True
        # Pointer comparison, not a content hash. Every rewrite in this codebase
        # goes through `messages[:] = [...]`, which builds new dicts, so a
        # replaced entry is a different object. Holding the references also
        # keeps ids from being recycled underneath the check.
        #
        # Known limit: mutating a message dict *in place*, without replacing it,
        # is invisible here. This comment used to assert that nothing did that,
        # and `microcompact` had been doing it since before this check existed
        # -- so compaction was never mirrored, and a restart handed back the
        # uncompacted transcript. A stale claim of safety is worse than a stated
        # gap: it is what everything built afterwards was validated against.
        # The limit is now enforced rather than asserted --
        # `tests/test_compaction_composition.py` runs every shipped rewriter and
        # requires the store to match memory, so a new in-place mutator fails
        # there instead of inheriting this guarantee silently.
        return any(
            messages[index] is not reference
            for index, reference in enumerate(self._persisted_refs)
        )

    @property
    def busy(self) -> bool:
        """True while a turn is in flight."""

        return self._running is not None and not self._running.done()

    def stop_accepting(self, reason: str = "session closed") -> None:
        """Reject new and lock-queued turns before dependencies are revoked."""

        self._accepting_runs = False
        self._closed_reason = reason

    def _record_interruption(self, reason: str, repaired) -> bool:
        """Leave the transcript describing a turn that was cut short.

        Two things were missing. A turn interrupted mid-generation left *no*
        trace at all -- the next run appended a second user message and the
        model saw two questions in a row with nothing between them. And when the
        transport was streaming, the console had already rendered text the
        transcript had no record of, so a follow-up like "finish that thought"
        referred to something the agent could not see.

        The partial is only recorded when no dangling `tool_use` was repaired:
        in that case the assistant turn is already in the transcript and its
        results must follow immediately, so inserting text between them would
        break the pairing the repair just fixed.
        """

        agent = self.agent
        if agent is None:
            return False

        partial = (getattr(agent, "streamed_text", "") or "").strip()
        agent.streamed_text = ""
        if repaired:
            # The repair already ends the transcript with an explicit `[unknown]`
            # result saying the turn was cut short, and it has to stay last so
            # the tool_use it answers is the one immediately before it. A second
            # marker would add nothing and a second consecutive user message.
            return False
        # Recorded as the assistant's own turn, not as a user message. The note
        # describes what the assistant was doing, and a standalone user message
        # would sit directly before the next prompt -- swapping "two user turns
        # with nothing between them" for "two user turns with a marker between
        # them", which is the same shape wearing a label.
        note = f"[Turn interrupted: {reason}]"
        agent.messages.append({
            "role": "assistant",
            "content": [{"type": "text", "text": f"{partial}\n{note}" if partial else note}],
        })
        return True

    async def cancel(self, reason: str = "cancelled by operator") -> bool:
        """Stop the turn in flight. Returns False when there was nothing to stop.

        The transcript is repaired afterwards the same way a crash is: a turn
        interrupted between dispatching a tool and recording its result would
        otherwise leave an unanswered `tool_use`, which the provider rejects
        outright. Cancellation must not leave a session unusable.
        """

        task = self._running
        if task is None or task.done():
            return False
        self._cancel_reason = reason
        task.cancel()
        with contextlib.suppress(asyncio.CancelledError, Exception):
            await task
        self.status = "idle"
        repaired = self._close_unanswered_tools()
        shown = self._record_interruption(reason, repaired)
        if repaired or shown:
            self._flush_messages()
        await self.emit({
            "type": "cancelled",
            "reason": reason,
            "repaired_tool_uses": list(repaired),
        })
        return True

    def _require_lease(self) -> None:
        """Refuse to advance a session this process does not hold.

        Without this, two processes on one database interleave their turns into
        a single transcript -- consecutive user messages, orphaned tool results,
        a shape the provider rejects. Failing the run is the lesser outcome.
        """

        if self.lease_owner is None:
            return
        acquire = getattr(self.state_store, "acquire_lease", None)
        if acquire is None:
            return
        if not acquire(self.id, self.lease_owner, ttl=self.lease_ttl):
            holder = self.state_store.lease_holder(self.id)
            raise LeaseLost(
                f"session {self.id} is held by {holder!r}; this process "
                f"({self.lease_owner!r}) will not advance it"
            )
        # We hold it now; a later renewal failure is a real loss, not a claim
        # that never won.
        self.lease_confirmed = True

    def _renew_lease(self) -> None:
        if self.lease_owner is None:
            return
        renew = getattr(self.state_store, "renew_lease", None)
        if renew is not None and not renew(self.id, self.lease_owner, ttl=self.lease_ttl):
            # Renewal happens per persistence beat, not per second, so a single
            # operation quieter than the TTL -- a slow non-streaming model call --
            # can let a held lease lapse, and another process can take it. The
            # old code discarded this result and kept appending to a transcript
            # it no longer owned: the exact double-drive the lease exists to
            # stop, undetected mid-turn. Raise only once we actually held it, so
            # a restored session that never won its claim does not fail on a
            # renewal it was never going to win.
            if self.lease_confirmed:
                holder = self.state_store.lease_holder(self.id)
                raise LeaseLost(
                    f"session {self.id} lease lost to {holder!r} mid-turn; this "
                    f"process ({self.lease_owner!r}) will stop advancing it"
                )

    def _flush_messages(self) -> None:
        """Persist transcript growth since the last flush.

        Called from the event path because that is where the transcript
        actually moves -- a model turn and a tool batch each emit before the
        loop continues, so a kill between events loses at most the tail the
        next event would have carried.
        """

        agent = self.agent
        if agent is None or not agent.messages:
            return
        if self._transcript_was_rewritten(agent.messages):
            # Start a new epoch and write the whole current transcript. The
            # superseded epoch stays on disk as the record of what the agent
            # actually saw before compaction rewrote its history.
            self._transcript_epoch += 1
            self._persisted_messages = 0
        pending = agent.messages[self._persisted_messages:]
        if not pending:
            return
        # The live transcript keeps what the model actually said -- it goes
        # back to the same provider that already holds the credential. What
        # lands on disk does not need to.
        secrets = getattr(agent, "secrets", None)
        if secrets is not None:
            # Detach first: an assistant turn holds provider block *objects*, and
            # a masker that walks dicts and lists steps straight past them --
            # which is how the durable table kept leaking after the event stream
            # stopped. `_json_safe` is idempotent, so the store re-running it is
            # harmless.
            from .storage import _json_safe

            persisted = [secrets.mask_payload(_json_safe(m)) for m in pending]
        else:
            persisted = pending
        self.state_store.append_messages(
            self.id, persisted, epoch=self._transcript_epoch
        )
        self._persisted_messages = len(agent.messages)
        self._persisted_refs = list(agent.messages)
        self._persist_session_record()
        # Renewed on the same beat as persistence, so a long turn cannot let a
        # lease lapse under an actively working process.
        self._renew_lease()

    def _persist_session_record(self) -> None:
        """Refresh the mutable session row.

        `create()` writes this once, which leaves `run_count`, `status` and the
        todo board frozen at their initial values for the life of the session --
        a restart would then restore a session that claims zero runs and an
        empty plan while its transcript plainly shows otherwise.
        """

        from .storage import SessionRecord

        agent = self.agent
        self.state_store.upsert_session(
            SessionRecord(
                session_id=self.id,
                workspace=str(self.workspace),
                system=self.system,
                created_at=self.created_at,
                run_count=self.run_count,
                status=self.status,
                event_cursor=0,  # derived on read
                todos=tuple(agent.todo.snapshot()) if agent is not None else (),
                owner=self.owner,
            )
        )

    @staticmethod
    def _unanswered_tool_uses(messages: list) -> list[str]:
        """Delegates: the transcript invariant belongs with the transcript."""

        return unanswered_tool_uses(messages)

    # `steering_injector` (module function below) delivers what these queue.

    def steer(self, text: str) -> int:
        """Queue mid-turn input; returns the queue length.

        Callable from any context (sync, another task): it only appends. The
        running turn picks the text up at its next loop round via
        `steering_injector`; an idle session sees it at the start of its next
        turn. Nothing 409s.

        Bounded, because every queued steer is joined into one injected message:
        an oversized steer is truncated (with a marker the model can see) so a
        single interjection cannot flood the context, and a busy session's queue
        is capped -- dropping the oldest, since the caller's latest redirection
        is the one that matters -- so repeated steering cannot grow it without
        limit. A steer that still fires, partially, beats one silently dropped;
        the user is present and can correct it.
        """

        text = str(text)
        if len(text) > MAX_STEER_CHARS:
            text = text[:MAX_STEER_CHARS] + "\n[steer truncated]"
        self._steering.append(text)
        if len(self._steering) > MAX_STEER_QUEUE:
            del self._steering[0]
        return len(self._steering)

    def drain_steering(self) -> list[str]:
        drained, self._steering = self._steering, []
        return drained

    def _mask(self, value):
        """Mask a value for a durable trajectory record.

        Events reach the trajectory through `_capture_event`, which masks them.
        The `start`/`finish` records do not go through that path -- they write
        `input_text`, `metadata`, and `output` directly -- so a secret a user
        pastes into a message landed in the trajectory file raw while the
        transcript masked it (round 111's inconsistency, one sink over). The
        trajectory store has no secrets of its own, so the mask is applied here.
        """

        secrets = getattr(self.agent, "secrets", None)
        if secrets is None or value is None:
            return value
        return secrets.mask(value) if isinstance(value, str) else secrets.mask_payload(value)

    def _close_unanswered_tools(self, overrides=None) -> list[str]:
        """Answer dangling tool calls with an explicit *unknown* outcome.

        Delegates for the same reason as above. This ran on `Session.cancel()`
        and on restore, and a cancellation arriving from outside the harness --
        an HTTP client disconnecting, a `wait_for` timeout -- reached neither,
        so the agent owns the repair now and this is the call-through.
        """

        agent = self.agent
        if agent is None:
            return []
        agent.close_unanswered_tools(overrides)
        # What the agent repaired, whether just now or inside a cancelled
        # `run()`. Reported either way.
        return agent.take_repaired_tool_uses()

    def restore(self) -> int:
        """Rebuild this session's in-memory state from the store."""

        agent = self.agent
        if agent is None:
            return 0
        messages = self.state_store.load_messages(self.id)
        agent.messages[:] = messages
        self._persisted_messages = len(messages)
        self._transcript_epoch = max(1, self.state_store.transcript_epoch(self.id))
        self._persisted_refs = list(agent.messages)
        self._seq = self.state_store.event_cursor(self.id)
        # Plan mode and the goal are log-only, whole-value state: the value in
        # force is the last logged snapshot, folded here -- no mirror to
        # drift. The goal's ACTIVATION is deliberately not folded: a restored
        # goal is a fact; firing unattended again needs a new human edge
        # (goal_resume), same rule as restored cron jobs.
        from .goals import fold_goal
        from .plan_mode import fold_plan_mode

        logged_events = self.state_store.load_events(self.id)
        agent.state["plan_mode"] = fold_plan_mode(logged_events)
        restored_goal = fold_goal(logged_events)
        if restored_goal is not None:
            agent.state["goal"] = restored_goal
        agent.state["goal_armed"] = False
        # Repair before the transcript is ever sent: the provider rejects an
        # unanswered tool_use, so an unrepaired session fails on every turn.
        # Calls that were parked on an approval when the process died are
        # answered as *not run* -- they never reached a handler, so unlike a
        # dispatched call the model may safely ask again. Two different
        # absences, two different answers (round 96's _MISSING lesson, one
        # layer up).
        repaired = self._close_unanswered_tools(self._expire_parked_approvals())
        if repaired:
            self._flush_messages()
            self._unknown_tool_uses = tuple(repaired)
        return len(agent.messages)

    def _expire_parked_approvals(self) -> dict:
        """Expire this session's pending durable approvals; the overrides map.

        The process that parked them is gone, so no future can resolve them.
        Their tool_use ids identify the dangling calls that never ran.
        """

        from .actions import NOT_RUN_RESULT

        read = getattr(self.state_store, "read_approvals", None)
        write = getattr(self.state_store, "write_approval", None)
        if read is None or write is None:
            return {}
        overrides = {}
        for row in read(self.id, status="pending"):
            write({**row, "status": "expired", "resolved_at": time.time()})
            if row.get("tool_use_id"):
                overrides[row["tool_use_id"]] = NOT_RUN_RESULT
        return overrides

    async def _publish_event(self, event: dict) -> None:
        # Not replayed either. A late subscriber catching up on a finished
        # turn would be handed stale fragments of text the final
        # `assistant_text` already superseded -- progress that is no longer
        # progress. Excluded from the store and the trajectory but left in the
        # replay buffer, which is two thirds of a decision.
        if not event.get("ephemeral"):
            self._backlog.append(event)
        for q in list(self._subscribers):
            try:
                q.put_nowait(event)
            except asyncio.QueueFull:
                # A subscriber that cannot keep up must not grow without bound.
                # Drop its oldest queued event to make room and keep the newest:
                # a live stream wants the latest progress, and the client sees
                # the seq gap and can resume from the backlog via last-event-id.
                with contextlib.suppress(asyncio.QueueEmpty):
                    q.get_nowait()
                with contextlib.suppress(asyncio.QueueFull):
                    q.put_nowait(event)
        if self._event_sink is not None:
            # Contained like every other observer: the sink is user-supplied
            # (metrics, logging), and one that throws must not kill the turn
            # that emitted -- the dispatcher-containment rule the hook chain
            # already follows (registry.Hooks.result). Reported, not
            # swallowed: a sink that stopped working is visible in info().
            try:
                res = self._event_sink(event)
                if inspect.isawaitable(res):
                    await res
            except Exception as error:
                self.sink_error = f"{type(error).__name__}: {error}"

    async def emit(self, event: dict) -> None:
        # Parallel tool calls may emit concurrently. Keep sequence assignment,
        # trajectory append, backlog publication, and sinks in one order.
        async with self._emit_lock:
            await self._publish_event(await self._capture_event(event))

    def subscribe(self, replay: bool = True) -> asyncio.Queue:
        q: asyncio.Queue = asyncio.Queue(maxsize=SUBSCRIBER_QUEUE_MAX)
        if replay:
            for event in self._backlog:
                q.put_nowait(event)
        self._subscribers.add(q)
        return q

    def unsubscribe(self, q: asyncio.Queue) -> None:
        self._subscribers.discard(q)

    # -- run one user message to completion (serialized per session) --
    async def run(
        self,
        message: str,
        run_context: RunContext | None = None,
    ) -> str:
        """Drive one turn. Concurrent callers queue on this session's lock."""

        if not self._accepting_runs:
            raise RuntimeError(self._closed_reason or "session is not accepting turns")
        self._require_lease()
        async with self.lock:
            # A caller may have passed the first check, then queued behind a
            # turn while SessionManager.stop/delete closed admission.
            if not self._accepting_runs:
                raise RuntimeError(
                    self._closed_reason or "session is not accepting turns"
                )
            # `_running` is the task that HOLDS the lock -- the turn actually
            # in flight -- not whoever most recently entered run(). Set before
            # the lock, a caller still queued on it (a cron fire into a busy
            # session) overwrote the running task's reference, and cancel()
            # then stopped the queued turn while the running one continued.
            self._running = asyncio.current_task()
            try:
                return await self._run(message, run_context)
            finally:
                self._running = None

    async def _run(
        self,
        message: str,
        run_context: RunContext | None = None,
    ) -> str:
        assert self.agent is not None
        resolved_context = run_context or RunContext.default()
        self.status = "running"
        self.run_count += 1
        self._trajectory_had_error = False
        self._trajectory_recording_error = None
        self._trajectory_started = time.monotonic()
        if self._trajectory_store is not None:
            try:
                self._active_trajectory_id = await asyncio.to_thread(
                    self._trajectory_store.start,
                    session_id=self.id,
                    run_index=self.run_count,
                    input_text=self._mask(message),
                    owner=self.owner,
                    metadata=self._mask({
                        "model": self.agent.settings.model,
                        "workspace": str(self.workspace),
                        "agent": self.agent.label,
                        "system": self.agent.refresh_system(),
                        "tools": self.agent.tools.names(),
                    }),
                )
                self._trajectory_count += 1
            except Exception as error:  # keep the requested run available
                self._active_trajectory_id = None
                self._trajectory_recording_error = f"{type(error).__name__}: {error}"
        if self._active_trajectory_id is not None:
            await self.emit({
                "type": "trajectory_start",
                "run_index": self.run_count,
            })
        await self.emit({"type": "status", "status": "running"})
        try:
            agent_run = self.agent.run
            signature = inspect.signature(agent_run)
            parameters = signature.parameters.values()
            accepts_context = (
                "run_context" in signature.parameters
                or any(
                    parameter.kind is inspect.Parameter.VAR_KEYWORD
                    for parameter in parameters
                )
            )
            if accepts_context:
                final = await agent_run(
                    message,
                    run_context=resolved_context,
                )
            else:
                # Preserve tests/extensions that replace ``Agent.run`` with
                # the pre-RunContext one-argument callable.
                final = await agent_run(message)
        except asyncio.CancelledError:
            self.status = "idle"
            await self._finish_trajectory(
                "cancelled",
                terminal_event={
                    "type": "status", "status": "idle", "cancelled": True,
                },
            )
            raise
        except Exception as e:
            self.status = "error"
            detail = f"{type(e).__name__}: {e}"
            self._trajectory_had_error = True
            await self._finish_trajectory(
                "error",
                terminal_event={"type": "error", "error": detail},
                error=detail,
            )
            raise
        self.status = "idle"
        outcome = "error" if self._trajectory_had_error else "completed"
        await self._finish_trajectory(
            outcome,
            terminal_event={
                "type": "done",
                "text": final,
                "phase": "final_answer",
            },
            output=final,
        )
        if resolved_context.allows("personal_skill.capture_source"):
            try:
                from .skill_capture import record_personal_skill_turn

                record_personal_skill_turn(self.agent, message, final)
            except Exception as error:
                # Capturing provenance is optional read-side state; it must
                # never turn a durably completed user turn into an error.
                self.agent.state["personal_skill_capture_error"] = (
                    type(error).__name__
                )
        return final

    async def _finish_trajectory(
        self,
        status: str,
        *,
        terminal_event: dict,
        output: str | None = None,
        error: str | None = None,
    ) -> None:
        trajectory_id = self._active_trajectory_id
        if trajectory_id is None:
            await self.emit({
                **terminal_event,
                "trajectory_id": None,
                "trajectory_status": "disabled",
                "trajectory_recording_error": self._trajectory_recording_error,
                # The same treatment the trajectory sink already got. Both
                # degrade rather than stopping the agent; both have to say so.
                "state_persisted": self.persist_error is None,
                "persist_error": self.persist_error,
            })
            return
        duration_ms = (time.monotonic() - self._trajectory_started) * 1000
        await self.emit({
            "type": "trajectory_end",
            "status": status,
            "duration_ms": round(duration_ms, 3),
        })
        # Keep capture, trajectory finalization, and publication under the same
        # ordering lock. Background workflow events can otherwise publish seq
        # N+1 while this terminal seq N is waiting on the store.
        async with self._emit_lock:
            terminal = await self._capture_event({
                **terminal_event,
                "trajectory_id": trajectory_id,
                "trace_id": trajectory_id,
                "group_id": self.id,
                "trajectory_status": status,
                "duration_ms": round(duration_ms, 3),
            })
            try:
                assert self._trajectory_store is not None
                await asyncio.to_thread(
                    self._trajectory_store.finish,
                    trajectory_id,
                    status=status,
                    output=self._mask(output),
                    error=self._mask(error),
                    duration_ms=duration_ms,
                )
            except Exception as recording_error:
                self._trajectory_recording_error = (
                    f"{type(recording_error).__name__}: {recording_error}"
                )
            finally:
                self._active_trajectory_id = None
            terminal["trajectory_persisted"] = self._trajectory_recording_error is None
            terminal["trajectory_recording_error"] = self._trajectory_recording_error
            await self._publish_event(terminal)

    # -- introspection for the API --
    def info(self) -> dict:
        agent = self.agent
        info = {
            "id": self.id,
            "status": self.status,
            "busy": self.busy,
            "cancel_reason": self._cancel_reason,
            "created_at": self.created_at,
            "run_count": self.run_count,
            "permission_mode": self.permission_mode,
            "pending_steering": len(self._steering),
            "workspace": str(self.workspace),
            "model": agent.settings.model if agent else None,
            "message_count": len(agent.messages) if agent else 0,
            "todos": agent.todo.snapshot() if agent else [],
            "subscribers": len(self._subscribers),
            "active_trajectory_id": self._active_trajectory_id,
            "trajectory_count": self._trajectory_count,
            "trajectory_recording_error": self._trajectory_recording_error,
            "sink_error": self.sink_error,
        }
        workflow_service = agent.state.get("workflow_service") if agent else None
        info["workflows"] = (
            workflow_service.summaries(self.id)
            if workflow_service is not None else []
        )
        return info


async def steering_injector(agent) -> list[dict]:
    """Deliver queued mid-turn input at the next loop round (an Agent injector).

    One message carries every queued steer, in arrival order, wrapped so the
    model can tell redirection from the original request. The queue drains
    whether the session was busy (mid-turn) or idle (start of the next turn):
    the caller's words always arrive, never a 409.
    """

    session = agent.state.get("session")
    if session is None:
        return []
    drained = session.drain_steering()
    if not drained:
        return []
    await agent._send("steering_delivered", count=len(drained))
    body = "\n\n".join(drained)
    return [{
        "role": "user",
        "content": f"<user_interjection>\n{body}\n</user_interjection>",
    }]

#: The module's runtime-invariant posture (tools/verify_invariants.py).
RUNTIME_INVARIANT = (
    "enforced by _transcript_guard: the message list sent to the model is covered by the durable transcript epoch (model-visible means logged); a gap raises InvariantError instead of proceeding"
)
