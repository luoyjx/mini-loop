"""Fault-injection census for recovery: pin the whole-turn properties.

The recovery suite pins each mechanism well -- classification, per-wait
bounds, dropped streams, escalation -- but nobody pinned the *composed*
properties a failing turn actually experiences: what the exhaustion of
retries looks like, how long the worst case hangs, and whether the 529
breaker can fire at all in production. All deterministic: injected fake
failures, computed constants, no sleeps (docs/RSI_RESEARCH_AND_PLAN.md §5).

Census findings, candidates for deliberate experiments:

* RESOLVED (micro-experiment H, 2026-09-01): the fallback-model breaker
  used to be fully built and fully unreachable -- tested, evented, and
  never constructed with a model to switch to. MINILOOP_FALLBACK_MODEL
  now plumbs through Settings into the default construction; unset keeps
  the breaker unarmed, so the default behavior is unchanged.
* RESOLVED (micro-experiment G, 2026-09-01): Retry-After used to have a
  per-wait ceiling but no total budget -- 50 minutes of honored waiting
  was reachable. Accumulated sleep is now capped by
  MAX_TOTAL_RETRY_WAIT_MS; the computed-backoff worst case (~199s) fits
  underneath, so only the hostage path feels the cut.
"""

import pytest

import mini_loop.recovery as recovery
from mini_loop.recovery import (
    BASE_DELAY_MS, DefaultRecovery, MAX_CONSECUTIVE_529, MAX_DELAY_MS,
    MAX_RETRIES, MAX_RETRY_AFTER_MS, MAX_TOTAL_RETRY_WAIT_MS, backoff_delay,
)


class Overloaded(Exception):
    status_code = 529


class _Agent:
    def __init__(self):
        self.state = {}
        self.events = []

    async def _send(self, kind, **fields):
        self.events.append((kind, fields))


def _always_fail(_kwargs):
    raise Overloaded("engine overloaded")


async def _call(kwargs):
    _always_fail(kwargs)


def test_exhausted_retries_fail_loudly_with_the_last_error(monkeypatch):
    """The composed exhaustion path: every retry is announced, the final
    failure is a named event, and the original exception surfaces to the
    caller -- never swallowed, never infinite."""

    import asyncio

    monkeypatch.setattr(recovery, "backoff_delay", lambda *a, **k: 0.0)
    agent = _Agent()
    runner = DefaultRecovery(max_retries=2)

    with pytest.raises(Overloaded):
        asyncio.run(runner.run(agent, {"messages": []}, _call))

    actions = [fields.get("action") for kind, fields in agent.events
               if kind == "recovery"]
    assert actions == ["retry", "retry", "failed"]
    attempts = [fields["attempt"] for _, fields in agent.events
                if fields.get("action") == "retry"]
    assert attempts == [1, 2]


def test_the_529_breaker_switches_models_only_when_configured(monkeypatch):
    """The breaker works when armed -- and the census names that nothing
    arms it (FINDING above): the production construction is bare."""

    import asyncio

    monkeypatch.setattr(recovery, "backoff_delay", lambda *a, **k: 0.0)
    agent = _Agent()
    runner = DefaultRecovery(fallback_model="backup-model", max_retries=6)
    kwargs = {"messages": [], "model": "primary"}

    with pytest.raises(Overloaded):
        asyncio.run(runner.run(agent, kwargs, _call))

    assert kwargs["model"] == "backup-model"
    assert agent.state["recovery_model"] == "backup-model"
    switches = [f for _, f in agent.events
                if f.get("action") == "fallback_model"]
    assert len(switches) >= 1
    # A bare construction stays unarmed; reaching the breaker is the
    # operator's explicit MINILOOP_FALLBACK_MODEL (micro-experiment H).
    assert DefaultRecovery().fallback_model is None


def test_the_breaker_is_reachable_from_settings(tmp_path, monkeypatch):
    """Micro-experiment H: MINILOOP_FALLBACK_MODEL plumbs through Settings
    into the default recovery construction -- the census-found dead branch
    is an operator opt-in now, unarmed by default."""

    import pathlib

    from mini_loop import SessionManager, Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic

    skills = pathlib.Path(__file__).resolve().parent.parent / "skills"
    monkeypatch.setenv("MINILOOP_FALLBACK_MODEL", "backup-model")
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=skills, spill_dir=None)
    assert settings.fallback_model == "backup-model"
    session = SessionManager(settings, FakeAsyncAnthropic()).create()
    assert session.agent.recovery.fallback_model == "backup-model"

    monkeypatch.delenv("MINILOOP_FALLBACK_MODEL")
    unarmed = Settings(fake_llm=True, workspace_root=tmp_path / "ws2",
                       skills_dir=skills, spill_dir=None)
    assert unarmed.fallback_model is None


def test_the_worst_case_hang_is_a_computed_number_not_a_surprise():
    """Two worst cases, from the constants alone. Computed backoff:
    ~199s before the turn gives up -- defensible. Honored Retry-After:
    50 minutes, because the ceiling is per-wait and no total budget
    exists (FINDING above)."""

    computed = sum(
        min(BASE_DELAY_MS * (2 ** attempt), MAX_DELAY_MS)
        for attempt in range(MAX_RETRIES)
    ) / 1000.0 * 1.25  # maximum jitter
    assert computed == pytest.approx(199.375)
    assert computed < 240, "the computed-backoff worst case stays under 4min"

    # Micro-experiment G: per-wait ceilings alone would allow 3000s; the
    # total budget cuts the reachable worst case to its own value, and the
    # computed path fits underneath so only the hostage scenario is cut.
    assert MAX_RETRIES * MAX_RETRY_AFTER_MS / 1000.0 == 3000.0
    assert MAX_TOTAL_RETRY_WAIT_MS / 1000.0 == 300.0
    assert computed < MAX_TOTAL_RETRY_WAIT_MS / 1000.0, (
        "the normal retry path must never feel the total budget"
    )


def test_a_retry_after_hostage_is_cut_at_the_total_budget(monkeypatch):
    """Micro-experiment G: a server answering every attempt with a big
    honored Retry-After used to hold the turn for 50 minutes. Accumulated
    sleep is now budgeted: the wait that would cross the cap is refused,
    the failure is named, and the original error surfaces."""

    import asyncio

    slept = []

    async def _fake_sleep(seconds):
        slept.append(seconds)

    monkeypatch.setattr(recovery, "backoff_delay", lambda *a, **k: 150.0)
    monkeypatch.setattr(recovery.asyncio, "sleep", _fake_sleep)
    agent = _Agent()
    runner = DefaultRecovery(max_retries=10)

    with pytest.raises(Overloaded):
        asyncio.run(runner.run(agent, {"messages": []}, _call))

    assert slept == [150.0, 150.0], (
        "exactly two 150s waits fit a 300s budget; the third is refused"
    )
    (failure,) = [f for _, f in agent.events if f.get("action") == "failed"]
    assert "total retry wait" in failure["reason"]


def test_backoff_stays_inside_its_documented_envelope():
    """The delay for attempt N is base*2^N capped at MAX_DELAY_MS, plus
    at most 25% jitter -- pinned as an envelope so the RNG stays free."""

    for attempt in (0, 3, 6, 20):
        base = min(BASE_DELAY_MS * (2 ** attempt), MAX_DELAY_MS) / 1000.0
        for _ in range(8):
            delay = backoff_delay(attempt)
            assert base <= delay <= base * 1.25
