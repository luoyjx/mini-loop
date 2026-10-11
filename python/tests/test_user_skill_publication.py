"""Create-only user skill publication is atomic, isolated, and snapshot-safe."""

from __future__ import annotations

import hashlib
import stat
import threading
from pathlib import Path

import pytest

from mini_loop.durable import atomic_create_text, read_bytes_no_follow
from mini_loop.secrets import SecretRegistry
from mini_loop.skills import SkillLoader
from mini_loop.user_resources import (
    UserResourceResolver,
    UserSkillConflict,
    UserSkillPublicationError,
    UserSkillValidationError,
    canonical_user_skill,
)


def _fields(name: str = "review", body: str = "USER BODY") -> dict[str, str]:
    return {
        "name": name,
        "description": "Review the current change",
        "body": body,
    }


def _resolver(tmp_path: Path, *, agent: SkillLoader | None = None, secrets=None):
    return UserResourceResolver(
        tmp_path / "users",
        agent or SkillLoader(tmp_path / "agent"),
        secrets=secrets,
    )


def test_atomic_create_is_0600_and_never_overwrites(tmp_path):
    target = tmp_path / "skill.md"
    atomic_create_text(target, "first")

    with pytest.raises(FileExistsError):
        atomic_create_text(target, "second")

    assert target.read_text() == "first"
    assert stat.S_IMODE(target.stat().st_mode) == 0o600
    assert not list(tmp_path.glob(".*.tmp"))


def test_atomic_create_refuses_a_symlink_target(tmp_path):
    victim = tmp_path / "victim"
    victim.write_text("untouched")
    target = tmp_path / "skill.md"
    target.symlink_to(victim)

    with pytest.raises(FileExistsError):
        atomic_create_text(target, "secret")

    assert victim.read_text() == "untouched"


def test_secure_read_refuses_a_symlink_and_bounds_the_file(tmp_path):
    victim = tmp_path / "victim"
    victim.write_bytes(b"private")
    link = tmp_path / "skill.md"
    link.symlink_to(victim)

    with pytest.raises(OSError):
        read_bytes_no_follow(link, max_bytes=100)
    with pytest.raises(OverflowError):
        read_bytes_no_follow(victim, max_bytes=3)


@pytest.mark.parametrize(
    ("field", "value", "code"),
    [
        ("name", "Not-Kebab", "invalid_name"),
        ("name", "double--dash", "invalid_name"),
        ("description", "two\nlines", "invalid_description"),
        ("description", "x" * 201, "invalid_description"),
        ("body", "", "invalid_body"),
        ("body", "x" * 50_001, "invalid_body"),
        ("body", "x\n" * 501, "invalid_body"),
        ("body", "before </skill> after", "unsafe_content"),
        ("body", "nul\x00byte", "unsafe_content"),
    ],
)
def test_canonical_skill_rejects_unsafe_fields(field, value, code):
    fields = _fields()
    fields[field] = value

    with pytest.raises(UserSkillValidationError) as raised:
        canonical_user_skill(**fields)

    assert raised.value.code == code
    if value:
        assert value not in str(raised.value)


def test_publish_rechecks_secret_masking_and_fails_closed(tmp_path):
    secrets = SecretRegistry()
    secrets.register("TOKEN", "super-secret-token")
    resolver = _resolver(tmp_path, secrets=secrets)
    fields = _fields(body="Use super-secret-token")

    with pytest.raises(UserSkillValidationError) as raised:
        resolver.publish_skill("alice", fields)

    assert raised.value.code == "secret_detected"
    assert not list((tmp_path / "users").rglob("SKILL.md"))
    assert "super-secret-token" not in str(raised.value)


def test_publish_rechecks_the_name_for_registered_secrets(tmp_path):
    secrets = SecretRegistry()
    secrets.register("SKILL_KEY", "secret-skill")
    resolver = _resolver(tmp_path, secrets=secrets)
    fields = _fields(name="secret-skill")

    with pytest.raises(UserSkillValidationError) as raised:
        resolver.publish_skill("alice", fields)

    assert raised.value.code == "secret_detected"
    assert not list((tmp_path / "users").rglob("SKILL.md"))


@pytest.mark.parametrize("secret", ["1234", lambda: None])
def test_publish_fails_closed_when_a_registered_secret_cannot_be_masked(
    tmp_path,
    secret,
):
    secrets = SecretRegistry()
    secrets.register("PIN", secret)
    resolver = _resolver(tmp_path, secrets=secrets)

    with pytest.raises(UserSkillPublicationError) as raised:
        resolver.publish_skill("alice", _fields(body="Use PIN 1234"))

    assert raised.value.code == "secret_check_failed"
    assert not list((tmp_path / "users").rglob("SKILL.md"))
    assert "PIN" not in str(raised.value)


def test_publish_refreshes_future_snapshot_and_reuses_memory(tmp_path):
    resolver = _resolver(tmp_path)
    before = resolver.for_owner("alice")

    publication = resolver.publish_skill("alice", _fields())
    after = resolver.for_owner("alice")

    assert publication.resources is after
    assert publication.activation == "next_session"
    canonical = canonical_user_skill(**_fields())
    assert publication.as_dict() == {
        "name": "review",
        "digest": hashlib.sha256(canonical.encode()).hexdigest(),
        "content_digest": hashlib.sha256(b"USER BODY").hexdigest(),
        "activation": "next_session",
        "collision_warning": None,
        "idempotent": False,
    }
    assert before is not after
    assert before.memory is after.memory
    assert "USER BODY" not in before.skills.load("user:review")
    assert "USER BODY" in after.skills.load("user:review")
    skill_file = after.root / "skills" / "review" / "SKILL.md"
    assert stat.S_IMODE(skill_file.stat().st_mode) == 0o600


def test_publish_normalizes_exactly_like_restart_and_retries_idempotently(tmp_path):
    resolver = _resolver(tmp_path)
    fields = _fields(
        "normalized",
        body="  first\r\nsecond\rthird  ",
    )
    fields["description"] = "  Use for normalized procedures.  "

    first = resolver.publish_skill("alice", fields)
    retried = resolver.publish_skill("alice", fields)
    restarted = SkillLoader(first.resources.root / "skills")
    live = first.resources.skills.user_loader.skills["normalized"]
    restored = restarted.skills["normalized"]

    assert first.idempotent is False
    assert retried.idempotent is True
    assert first.digest == retried.digest
    assert first.content_digest == restored["digest"] == live["digest"]
    assert live["body"] == restored["body"] == "first\nsecond\nthird"
    assert live["meta"] == restored["meta"] == {
        "name": "normalized",
        "description": "Use for normalized procedures.",
    }


def test_future_snapshot_order_matches_a_fresh_loader(tmp_path):
    resolver = _resolver(tmp_path)

    resolver.publish_skill("alice", _fields("zeta"))
    publication = resolver.publish_skill("alice", _fields("alpha"))
    live_names = list(publication.resources.skills.user_loader.skills)
    restarted_names = list(
        SkillLoader(publication.resources.root / "skills").skills
    )

    assert live_names == restarted_names == ["alpha", "zeta"]


def test_same_canonical_skill_is_an_idempotent_retry(tmp_path):
    resolver = _resolver(tmp_path)
    first = resolver.publish_skill("alice", _fields(body="FIRST"))

    retried = resolver.publish_skill("alice", _fields(body="FIRST"))

    assert retried.idempotent is True
    assert retried.digest == first.digest
    assert retried.resources.memory is first.resources.memory


def test_same_user_name_with_different_content_never_overwrites(tmp_path):
    resolver = _resolver(tmp_path)
    first = resolver.publish_skill("alice", _fields(body="FIRST"))
    target = first.resources.root / "skills" / "review" / "SKILL.md"
    original = target.read_bytes()

    with pytest.raises(UserSkillConflict) as raised:
        resolver.publish_skill("alice", _fields(body="SECOND"))

    assert raised.value.code == "user_skill_exists"
    assert target.read_bytes() == original
    assert "FIRST" in resolver.for_owner("alice").skills.load("user:review")
    assert "SECOND" not in resolver.for_owner("alice").skills.load("user:review")


def test_concurrent_same_name_has_exactly_one_winner(tmp_path):
    resolvers = (_resolver(tmp_path), _resolver(tmp_path))
    barrier = threading.Barrier(2)
    outcomes: list[object] = []

    def publish(resolver: UserResourceResolver, body: str) -> None:
        barrier.wait()
        try:
            outcomes.append(resolver.publish_skill("alice", _fields(body=body)))
        except UserSkillConflict as error:
            outcomes.append(error)

    threads = [
        threading.Thread(target=publish, args=(resolver, body))
        for resolver, body in zip(resolvers, ("FIRST", "SECOND"), strict=True)
    ]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    assert sum(not isinstance(item, Exception) for item in outcomes) == 1
    assert sum(isinstance(item, UserSkillConflict) for item in outcomes) == 1
    rendered = _resolver(tmp_path).for_owner("alice").skills.load("user:review")
    assert ("FIRST" in rendered) != ("SECOND" in rendered)


def test_concurrent_identical_publications_are_safe_retries(tmp_path):
    resolvers = (_resolver(tmp_path), _resolver(tmp_path))
    barrier = threading.Barrier(2)
    publications = []

    def publish(resolver: UserResourceResolver) -> None:
        barrier.wait()
        publications.append(resolver.publish_skill("alice", _fields()))

    threads = [
        threading.Thread(target=publish, args=(resolver,))
        for resolver in resolvers
    ]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    assert len(publications) == 2
    assert sorted(item.idempotent for item in publications) == [False, True]
    assert len({item.digest for item in publications}) == 1
    target = publications[0].resources.root / "skills" / "review" / "SKILL.md"
    assert target.exists()
    assert all(
        "USER BODY" in item.resources.skills.load("user:review")
        for item in publications
    )


def test_symlinked_skill_directory_is_rejected_without_touching_victim(tmp_path):
    resolver = _resolver(tmp_path)
    resources = resolver.for_owner("alice")
    victim = tmp_path / "victim"
    victim.mkdir()
    (resources.root / "skills" / "review").symlink_to(
        victim,
        target_is_directory=True,
    )

    with pytest.raises(UserSkillPublicationError) as raised:
        resolver.publish_skill("alice", _fields())

    assert raised.value.code == "unsafe_path"
    assert not (victim / "SKILL.md").exists()
    assert str(victim) not in str(raised.value)


def test_symlinked_skill_file_is_rejected_without_touching_victim(tmp_path):
    resolver = _resolver(tmp_path)
    resources = resolver.for_owner("alice")
    victim = tmp_path / "victim"
    victim.write_text("untouched")
    skill_root = resources.root / "skills" / "review"
    skill_root.mkdir()
    (skill_root / "SKILL.md").symlink_to(victim)

    with pytest.raises(UserSkillPublicationError) as raised:
        resolver.publish_skill("alice", _fields())

    assert raised.value.code == "unsafe_path"
    assert victim.read_text() == "untouched"


def test_agent_name_collision_is_allowed_but_warned(tmp_path):
    agent_root = tmp_path / "agent" / "review"
    agent_root.mkdir(parents=True)
    (agent_root / "SKILL.md").write_text(
        "---\nname: review\ndescription: Agent review\n---\nAGENT BODY"
    )
    resolver = _resolver(tmp_path, agent=SkillLoader(tmp_path / "agent"))

    publication = resolver.publish_skill("alice", _fields())

    assert publication.collision_warning is not None
    assert "AGENT BODY" in publication.resources.skills.load("agent:review")
    assert "USER BODY" in publication.resources.skills.load("user:review")
    assert "Ambiguous" in publication.resources.skills.load("review")


def test_equal_names_are_isolated_between_alice_and_bob(tmp_path):
    resolver = _resolver(tmp_path)

    alice = resolver.publish_skill("alice", _fields(body="ALICE BODY"))
    bob = resolver.publish_skill("bob", _fields(body="BOB BODY"))

    assert alice.resources.root != bob.resources.root
    assert "ALICE BODY" in alice.resources.skills.load("user:review")
    assert "BOB BODY" not in alice.resources.skills.load("user:review")
    assert "BOB BODY" in bob.resources.skills.load("user:review")
    assert "ALICE BODY" not in bob.resources.skills.load("user:review")


def test_publication_errors_never_expose_host_paths(tmp_path, monkeypatch):
    resolver = _resolver(tmp_path)
    resolver.for_owner("alice")

    def fail(*_args, **_kwargs):
        raise OSError(5, "disk failed", str(tmp_path / "private"))

    monkeypatch.setattr("mini_loop.user_resources.atomic_create_text", fail)
    with pytest.raises(UserSkillPublicationError) as raised:
        resolver.publish_skill("alice", _fields())

    assert raised.value.code == "publish_failed"
    assert str(tmp_path) not in str(raised.value)


def test_success_performs_no_fallible_loader_validation_after_the_commit_point(
    tmp_path,
    monkeypatch,
):
    resolver = _resolver(tmp_path)
    before = resolver.for_owner("alice")
    real_loader = SkillLoader
    calls = 0

    def omit_published_skill(root):
        nonlocal calls
        calls += 1
        if calls >= 2:
            raise AssertionError("loader ran after the hard-link commit point")
        return real_loader(root)

    monkeypatch.setattr("mini_loop.user_resources.SkillLoader", omit_published_skill)
    publication = resolver.publish_skill("alice", _fields())

    assert calls == 1
    assert resolver.for_owner("alice") is publication.resources
    assert publication.resources.memory is before.memory
    assert (before.root / "skills" / "review" / "SKILL.md").exists()
