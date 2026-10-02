"""GET /sessions is bounded and ordered (round 233, roadmap G4).

The unbounded form returned info() -- real per-session work -- for every
session the caller ever created. A `limit` (capped, most-recent-first)
bounds both the response and the work, matching the trajectory routes.
"""

import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
ALICE = {"Authorization": "Bearer tok-alice"}


@pytest.fixture
def client(tmp_path):
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.server import create_app

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
    )
    app = create_app(manager=manager)
    with TestClient(app) as c:
        app.state.auth = TokenAuth({"tok-alice": "alice"})
        yield c


def test_the_listing_is_capped(client):
    for _ in range(7):
        client.post("/sessions", json={}, headers=ALICE)
    got = client.get("/sessions?limit=3", headers=ALICE).json()
    assert len(got) == 3


def test_the_cap_is_bounded_above(client):
    ids = [client.post("/sessions", json={}, headers=ALICE).json()["id"]
           for _ in range(4)]
    # An absurd limit is clamped, not honored; all 4 fit under the ceiling.
    got = client.get("/sessions?limit=100000", headers=ALICE).json()
    assert len(got) == 4


def test_most_recent_first(client):
    first = client.post("/sessions", json={}, headers=ALICE).json()["id"]
    last = client.post("/sessions", json={}, headers=ALICE).json()["id"]
    got = client.get("/sessions?limit=1", headers=ALICE).json()
    assert got[0]["id"] == last, "the newest session must lead a capped page"


def test_default_still_returns_a_list(client):
    client.post("/sessions", json={}, headers=ALICE)
    got = client.get("/sessions", headers=ALICE).json()
    assert isinstance(got, list) and len(got) == 1
