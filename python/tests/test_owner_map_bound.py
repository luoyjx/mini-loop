"""The deleted-session owner map is bounded, and modern access ignores it.

`_session_owners` remembers a deleted session's owner so its trajectories stay
attributable. It was populated on every delete and never bounded -- one entry
per deleted session forever. That is round 86's unbounded-growth shape, and
worse than pure memory: `_owned_session_ids` iterates the whole map on every
trajectory *listing*, so the growth is per-request latency too.

The map is a *legacy* fallback. Trajectories recorded since round 74 carry
their own durable `owner` field, which `_owns_trajectory` reads first; the map
is consulted only for older trajectories that lack it. So bounding the map
cannot break a modern trajectory's access check -- that is the property pinned
hardest here. Eviction only reaches legacy trajectories of long-ago deleted
sessions, which then fail the check closed, the same safe direction as a
restart.
"""

import pathlib

import pytest

import mini_loop.manager as manager_module
from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(),
    )


class _Req:
    def __init__(self, manager):
        self.app = type("A", (), {
            "state": type("S", (), {"manager": manager})()})()


class _Caller:
    def __init__(self, ident):
        self.id = ident


def test_the_owner_map_stays_bounded_across_many_deletes(tmp_path, monkeypatch):
    monkeypatch.setattr(manager_module, "MAX_REMEMBERED_OWNERS", 5)
    manager = _manager(tmp_path)

    for i in range(50):
        session = manager.create()
        session.owner = f"user{i}"
        manager.delete(session.id)

    assert len(manager._session_owners) == 5, "the owner map grew past its cap"
    # The survivors are the most recent, not an arbitrary five.
    assert list(manager._session_owners.values()) == [f"user{i}" for i in range(45, 50)]


def test_a_modern_trajectory_is_accessible_after_its_owner_is_evicted(tmp_path,
                                                                      monkeypatch):
    """The property that makes bounding safe: a trajectory carrying its own
    `owner` never consults the map, so eviction cannot lock its owner out."""

    from mini_loop.server import _owns_trajectory

    monkeypatch.setattr(manager_module, "MAX_REMEMBERED_OWNERS", 3)
    manager = _manager(tmp_path)

    first = manager.create()
    first.owner = "alice"
    first_id = first.id
    manager.delete(first_id)
    # Push alice's entry out of the map.
    for i in range(5):
        s = manager.create()
        s.owner = f"filler{i}"
        manager.delete(s.id)
    assert first_id not in manager._session_owners

    modern = {"owner": "alice", "session_id": first_id}
    assert _owns_trajectory(_Req(manager), modern, _Caller("alice")) is True
    assert _owns_trajectory(_Req(manager), modern, _Caller("mallory")) is False


def test_a_legacy_trajectory_fails_closed_after_eviction(tmp_path, monkeypatch):
    """A pre-round-74 trajectory (no owner field) of an evicted session can no
    longer be attributed -- the same fail-closed as a restart, not a leak."""

    from mini_loop.server import _owns_trajectory

    monkeypatch.setattr(manager_module, "MAX_REMEMBERED_OWNERS", 3)
    manager = _manager(tmp_path)

    old = manager.create()
    old.owner = "alice"
    old_id = old.id
    manager.delete(old_id)
    for i in range(5):
        s = manager.create()
        s.owner = f"filler{i}"
        manager.delete(s.id)

    legacy = {"session_id": old_id}  # no "owner" key
    assert _owns_trajectory(_Req(manager), legacy, _Caller("alice")) is False


def test_a_legacy_trajectory_is_attributable_before_eviction(tmp_path, monkeypatch):
    """Not vacuous: within the bound, the fallback still works for legacy
    trajectories -- the map is not simply broken."""

    from mini_loop.server import _owns_trajectory

    monkeypatch.setattr(manager_module, "MAX_REMEMBERED_OWNERS", 100)
    manager = _manager(tmp_path)

    session = manager.create()
    session.owner = "alice"
    sid = session.id
    manager.delete(sid)

    legacy = {"session_id": sid}
    assert _owns_trajectory(_Req(manager), legacy, _Caller("alice")) is True
