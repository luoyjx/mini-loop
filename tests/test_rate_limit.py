"""The expensive routes carry a per-principal budget (roadmap G4).

A turn is a model call and real tool work; the HTTP request is the cheap
part. With principal scoping, idempotency and pagination in place, the
listed G4 remainder was rate limiting: one noisy caller -- a retry storm,
a runaway script -- could submit turns as fast as the socket allows.

Off by default (0): loopback single-user needs no limiter, and a surprise
429 there would be a regression. Pinned here:

* disabled means disabled -- the default settings never 429;
* over budget answers 429 with Retry-After and the limit named;
* the budget is per principal: one caller exhausting theirs leaves
  another's untouched;
* the budget is shared across the expensive routes (a fork spends from
  the same minute as a message);
* an idempotent replay is served from the cache without spending budget.
"""

import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"

ALICE = {"Authorization": "Bearer tok-alice"}
BOB = {"Authorization": "Bearer tok-bob"}


def _client(tmp_path, **settings_over):
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.server import create_app

    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS, **settings_over)
    manager = SessionManager(settings, FakeAsyncAnthropic())
    # settings passed explicitly: create_app(manager=...) alone reads
    # app-level settings from the environment, not from the manager.
    app = create_app(manager=manager, settings=settings)
    client = TestClient(app)
    client.__enter__()
    app.state.auth = TokenAuth({"tok-alice": "alice", "tok-bob": "bob"})
    return client


def _sid(client, headers):
    return client.post("/sessions", json={}, headers=headers).json()["id"]


def test_disabled_by_default_never_429s(tmp_path):
    client = _client(tmp_path)
    try:
        sid = _sid(client, ALICE)
        for i in range(5):
            r = client.post(f"/sessions/{sid}/messages",
                            json={"message": f"m{i}"}, headers=ALICE)
            assert r.status_code == 200
    finally:
        client.__exit__(None, None, None)


def test_over_budget_answers_429_with_retry_after(tmp_path):
    client = _client(tmp_path, rate_limit_per_minute=2)
    try:
        sid = _sid(client, ALICE)
        for i in range(2):
            assert client.post(f"/sessions/{sid}/messages",
                               json={"message": f"m{i}"},
                               headers=ALICE).status_code == 200
        third = client.post(f"/sessions/{sid}/messages",
                            json={"message": "m2"}, headers=ALICE)
        assert third.status_code == 429
        assert "2/minute" in third.json()["detail"]
        assert 0 < int(third.headers["retry-after"]) <= 60
    finally:
        client.__exit__(None, None, None)


def test_the_budget_is_per_principal(tmp_path):
    client = _client(tmp_path, rate_limit_per_minute=1)
    try:
        alice_sid = _sid(client, ALICE)
        bob_sid = _sid(client, BOB)
        assert client.post(f"/sessions/{alice_sid}/messages",
                           json={"message": "a"}, headers=ALICE).status_code == 200
        assert client.post(f"/sessions/{alice_sid}/messages",
                           json={"message": "a2"}, headers=ALICE).status_code == 429
        assert client.post(f"/sessions/{bob_sid}/messages",
                           json={"message": "b"}, headers=BOB).status_code == 200
    finally:
        client.__exit__(None, None, None)


def test_the_budget_is_shared_across_expensive_routes(tmp_path):
    client = _client(tmp_path, rate_limit_per_minute=2)
    try:
        sid = _sid(client, ALICE)
        for i in range(2):
            client.post(f"/sessions/{sid}/messages",
                        json={"message": f"m{i}"}, headers=ALICE)
        forked = client.post(f"/sessions/{sid}/fork", headers=ALICE)
        assert forked.status_code == 429
    finally:
        client.__exit__(None, None, None)


def test_an_idempotent_replay_spends_no_budget(tmp_path):
    client = _client(tmp_path, rate_limit_per_minute=2)
    try:
        sid = _sid(client, ALICE)
        h = {**ALICE, "Idempotency-Key": "same"}
        assert client.post(f"/sessions/{sid}/messages",
                           json={"message": "x"}, headers=h).status_code == 200
        # Replays of the cached result, well past the limit: all 200.
        for _ in range(4):
            assert client.post(f"/sessions/{sid}/messages",
                               json={"message": "x"}, headers=h).status_code == 200
    finally:
        client.__exit__(None, None, None)


def test_a_negative_limit_is_rejected(monkeypatch):
    monkeypatch.setenv("MINILOOP_RATE_LIMIT_PER_MINUTE", "-1")
    with pytest.raises(ValueError, match="rate_limit_per_minute"):
        Settings(fake_llm=True)
