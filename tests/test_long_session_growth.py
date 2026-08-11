"""What a session accumulates over ninety turns, not over one.

Round 85's lesson was about test *shape*: a per-turn budget bug is invisible to
any test that runs one turn. The same blind spot hides anything that grows. So
this runs a session long enough for growth to be visible and asserts what is
allowed to keep climbing.

Run that way, one line kept climbing while everything else plateaued:

    agent.messages              8  40  51  51    <- compaction holds it
    session._persisted_refs     8  40  51  51
    actions._records            2  10  40  90    <- nothing holds it

The action journal caps each result at 4,000 characters, so the size *per
record* was bounded and the *count* never was:

    20,000 completed actions -> 81.0 MB of result text, never released

The fix sheds payloads rather than evicting records, and that distinction is
the important one: a replayed action whose record is gone reads as "never
started" and runs its side effect a second time -- the exact failure an action
journal exists to prevent. `test_a_shed_action_is_not_re_run` is the guard on
that, and it matters more than the memory.
"""

import pathlib
import sys

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.actions import (
    MAX_RESULTS_RETAINED,
    SHED_RESULT,
    InMemoryActionJournal,
)
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _journal(count, *, result="x" * 4_000, status="completed"):
    journal = InMemoryActionJournal()
    for i in range(count):
        journal.begin(action_id=f"a{i}", session_id="s", message_id="m",
                      tool_use_id=f"t{i}", tool_name="run_bash",
                      input_value={"i": i})
        journal.finish(f"a{i}", status=status, result=result)
    return journal


def _result_bytes(journal):
    return sum(sys.getsizeof(r.result or "") for r in journal._records.values())


# -- the property that outranks the memory --------------------------------

def test_a_shed_action_is_not_re_run():
    """Shedding must never look like "never started"."""

    journal = _journal(MAX_RESULTS_RETAINED + 200)
    oldest = journal.get("a0")
    assert oldest.result == SHED_RESULT
    assert oldest.status == "completed"

    replayed = journal.begin(action_id="a0", session_id="s", message_id="m",
                             tool_use_id="t0", tool_name="run_bash",
                             input_value={"i": 0})
    assert replayed.status == "completed", "a shed action would be run twice"


def test_an_unfinished_action_is_never_shed():
    """A `started` action has an outcome nobody has recorded yet."""

    journal = _journal(MAX_RESULTS_RETAINED + 50)
    journal.begin(action_id="live", session_id="s", message_id="m",
                  tool_use_id="tl", tool_name="run_bash", input_value={"i": -1})
    for i in range(MAX_RESULTS_RETAINED + 50):   # push it far past the bound
        journal.begin(action_id=f"b{i}", session_id="s", message_id="m",
                      tool_use_id=f"u{i}", tool_name="run_bash", input_value={"i": i})
        journal.finish(f"b{i}", status="completed", result="y" * 4_000)
    assert journal.get("live").status == "started"


# -- the memory -----------------------------------------------------------

def test_results_are_released_beyond_the_bound():
    journal = _journal(20_000)
    assert len(journal._records) == 20_000, "records stay answerable"
    assert _result_bytes(journal) < 10_000_000, (
        f"{_result_bytes(journal)/1e6:.1f} MB of result text retained"
    )


def test_the_newest_results_are_intact():
    """Shedding the wrong end would make the journal useless for reconciliation."""

    journal = _journal(MAX_RESULTS_RETAINED + 100)
    newest = journal.get(f"a{MAX_RESULTS_RETAINED + 99}")
    assert newest.result and newest.result != SHED_RESULT
    assert len(newest.result) == 4_000


def test_truncated_action_result_is_explicit_on_replay():
    journal = InMemoryActionJournal()
    journal.begin(
        action_id="long",
        session_id="session",
        message_id="message",
        tool_use_id="tool",
        tool_name="bash",
        input_value={},
    )
    journal.finish("long", status="completed", result="x" * 5_000)

    result = journal.get("long").result
    assert len(result) == 4_000
    assert result.endswith("[action result truncated; original_chars=5000]")


def test_a_short_session_sheds_nothing():
    """Not vacuous: the bound must not fire on ordinary use."""

    journal = _journal(MAX_RESULTS_RETAINED - 1)
    assert not any(r.result == SHED_RESULT for r in journal._records.values())
    assert not journal.problems


def test_shedding_is_reported():
    journal = _journal(MAX_RESULTS_RETAINED + 10)
    assert any("released" in p for p in journal.problems.summary())


# -- the shape that found it ----------------------------------------------

@pytest.mark.asyncio
async def test_nothing_unbounded_survives_a_long_session(tmp_path):
    """Ninety turns, then ask what is still climbing."""

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
        tool_registry=full_registry(),
    )
    session = manager.create()

    def sizes():
        journal = manager.actions
        return {
            "messages": len(session.agent.messages),
            "result_bytes": _result_bytes(journal) if hasattr(journal, "_records") else 0,
        }

    for i in range(20):
        await session.agent.run(f"q{i}")
    early = sizes()
    for i in range(20, 90):
        await session.agent.run(f"q{i}")
    late = sizes()

    assert late["messages"] == early["messages"], "compaction stopped holding messages"
    # Not "equal": the retention window itself is allowed to fill.
    assert late["result_bytes"] <= MAX_RESULTS_RETAINED * 4_100, (
        f"result text grew to {late['result_bytes']:,} bytes over 90 turns"
    )


@pytest.mark.asyncio
async def test_a_stalled_subscriber_queue_is_bounded(tmp_path):
    """A subscriber that stops reading -- a backgrounded tab, a stalled SSE
    client the ASGI server is backpressuring -- otherwise accumulates every
    event for the life of the session. The replay backlog was bounded and the
    live subscriber queues were not, so one stalled client grew memory without
    limit while the backlog it could resume from stayed capped. The queue is
    bounded now; on overflow the oldest event is dropped, so the client keeps
    the newest progress and can resume the middle from the backlog.
    """
    from mini_loop.session import SUBSCRIBER_QUEUE_MAX

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(), tool_registry=full_registry(),
    )
    session = manager.create()
    stalled = session.subscribe(replay=False)  # created, then never drained

    for i in range(SUBSCRIBER_QUEUE_MAX + 500):
        await session.emit({"type": "noise", "i": i})

    assert stalled.qsize() == SUBSCRIBER_QUEUE_MAX, "the stalled queue grew past its bound"
    kept = [stalled.get_nowait()["i"] for _ in range(stalled.qsize())]
    # Drop-oldest keeps the newest, so a live stream still shows latest progress.
    assert kept[-1] == SUBSCRIBER_QUEUE_MAX + 499, "the newest event was not retained"

    # Non-vacuity: a subscriber that keeps up loses nothing.
    live = session.subscribe(replay=False)
    await session.emit({"type": "x", "i": 7})
    assert live.get_nowait()["i"] == 7


@pytest.mark.asyncio
async def test_the_background_notification_batch_is_bounded(tmp_path):
    """Each background result is capped at OUTPUT_CAP, but the count was not: a
    long round during which many background tasks finish drains them all into one
    injected `<task_notification>` message. The batch is capped, keeping the
    newest; the overflow is marked and stays retrievable via check_background --
    the team inbox's per-drain cap (round 50), applied here.
    """
    from mini_loop.background import (
        BackgroundManager,
        MAX_NOTIFICATIONS,
        background_injector,
    )

    mgr = BackgroundManager(tmp_path / "bg")
    for i in range(MAX_NOTIFICATIONS * 3):
        mgr._completed.append({"bg_id": f"bg_{i}", "status": "completed", "result": f"r{i}"})

    class _Agent:
        state = {"background": mgr}

        async def _send(self, *a, **k):
            pass

    [msg] = await background_injector(_Agent())
    text = msg["content"]
    assert text.count("<task_notification") == MAX_NOTIFICATIONS, "the batch grew past its bound"
    assert f'"bg_{MAX_NOTIFICATIONS * 3 - 1}"' in text, "the newest result was dropped"
    assert "omitted from this batch" in text, "the overflow was dropped without a trace"
