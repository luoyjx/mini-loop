"""Owner-scoped skill catalogues keep provenance without cross-layer shadowing."""

from __future__ import annotations

import hashlib
from dataclasses import FrozenInstanceError
from pathlib import Path

import pytest

from mini_loop.skills import (
    MAX_SKILL_CATALOGUE,
    LayeredSkillLoader,
    SkillLoader,
)
from mini_loop.user_resources import UserResourceResolver


def _skill(
    root: Path,
    directory: str,
    *,
    name: str,
    description: str = "description",
    body: str = "body",
) -> None:
    target = root / directory
    target.mkdir(parents=True, exist_ok=True)
    (target / "SKILL.md").write_text(
        f"---\nname: {name}\ndescription: {description}\n---\n{body}"
    )


def _owner_key(owner: str) -> str:
    return "u-" + hashlib.sha256(owner.encode("utf-8")).hexdigest()


def test_layered_catalogue_names_sources_and_content_digests(tmp_path):
    agent_root = tmp_path / "agent"
    user_root = tmp_path / "user"
    _skill(agent_root, "review", name="review", body="AGENT BODY")
    _skill(user_root, "notes", name="notes", body="USER BODY")

    skills = LayeredSkillLoader(
        SkillLoader(agent_root),
        SkillLoader(user_root),
    )
    catalogue = skills.descriptions()

    assert "Agent-provided skills:" in catalogue
    assert "User-scoped skills:" in catalogue
    assert "agent:review [digest=" in catalogue
    assert "user:notes [digest=" in catalogue

    agent_digest = hashlib.sha256(b"AGENT BODY").hexdigest()
    user_digest = hashlib.sha256(b"USER BODY").hexdigest()
    assert f"agent:review [digest={agent_digest[:16]}]" in catalogue
    assert f"user:notes [digest={user_digest[:16]}]" in catalogue
    assert agent_digest not in catalogue
    assert user_digest not in catalogue
    assert (
        f'<skill name="review" source="agent" digest="{agent_digest}">'
        in skills.load("review")
    )
    assert (
        f'<skill name="notes" source="user" digest="{user_digest}">'
        in skills.load("user:notes")
    )
    assert "USER BODY" in skills.load("notes", scope="user")


def test_legacy_loader_accepts_the_scoped_tool_call_shape(tmp_path):
    root = tmp_path / "agent"
    _skill(root, "review", name="review", body="AGENT BODY")
    loader = SkillLoader(root)

    assert "AGENT BODY" in loader.load("review", scope="agent")
    assert "AGENT BODY" in loader.load("agent:review")
    assert "unavailable" in loader.load("review", scope="user")
    assert len(loader.load("x" * 100_000)) < 200


def test_layering_rejects_a_two_method_loader_without_a_snapshot(tmp_path):
    class LegacyLoader:
        def descriptions(self):
            return "legacy"

        def load(self, name):
            return name

    with pytest.raises(TypeError, match="construction snapshot"):
        LayeredSkillLoader(LegacyLoader(), SkillLoader(tmp_path / "user"))


def test_cross_source_collision_is_ambiguous_until_the_source_is_explicit(tmp_path):
    agent_root = tmp_path / "agent"
    user_root = tmp_path / "user"
    _skill(agent_root, "deploy", name="deploy", body="AGENT DEPLOY")
    _skill(user_root, "deploy", name="deploy", body="USER DEPLOY")
    skills = LayeredSkillLoader(SkillLoader(agent_root), SkillLoader(user_root))

    ambiguous = skills.load("deploy")
    assert "Ambiguous skill 'deploy'" in ambiguous
    assert "agent:deploy" in ambiguous and "user:deploy" in ambiguous
    assert "AGENT DEPLOY" not in ambiguous and "USER DEPLOY" not in ambiguous

    assert "AGENT DEPLOY" in skills.load("agent:deploy")
    assert "USER DEPLOY" in skills.load("user:deploy")
    assert "AGENT DEPLOY" in skills.load("deploy", scope="agent")
    assert "USER DEPLOY" in skills.load("deploy", scope="user")
    assert "conflicts with scope 'user'" in skills.load(
        "agent:deploy", scope="user"
    )


def test_unknown_skill_reports_only_the_visible_qualified_candidates(tmp_path):
    agent_root = tmp_path / "agent"
    user_root = tmp_path / "user"
    _skill(agent_root, "agent-one", name="agent-one")
    _skill(user_root, "user-one", name="user-one")
    skills = LayeredSkillLoader(SkillLoader(agent_root), SkillLoader(user_root))

    error = skills.load("missing")
    assert "agent:agent-one" in error
    assert "user:user-one" in error
    assert str(agent_root) not in error
    assert str(user_root) not in error


def test_combined_catalogue_has_one_budget_and_keeps_agent_provenance(tmp_path):
    agent_root = tmp_path / "agent"
    user_root = tmp_path / "user"
    _skill(agent_root, "core", name="core", description="deployment policy")
    for index in range(200):
        _skill(
            user_root,
            f"s{index:03d}",
            name=f"s{index:03d}",
            description="D" * 200,
        )
    skills = LayeredSkillLoader(SkillLoader(agent_root), SkillLoader(user_root))

    catalogue = skills.descriptions()

    assert len(catalogue) <= MAX_SKILL_CATALOGUE
    assert catalogue.startswith("Agent-provided skills:")
    assert "agent:core" in catalogue
    assert "User-scoped skills:" in catalogue
    assert "omitted; catalogue is full" in catalogue
    assert any("combined" in problem for problem in skills.problems)

    for index in range(2_000):
        skills.user_loader.skills[f"extra-{index:04d}"] = {
            "meta": {}, "body": "", "digest": "0" * 64,
        }
    unknown = skills.load("missing")
    assert len(unknown) <= MAX_SKILL_CATALOGUE
    assert "more omitted" in unknown


def test_resolver_uses_only_a_digest_path_reuses_and_freezes_the_bundle(tmp_path):
    root = tmp_path / "users"
    agent = SkillLoader(tmp_path / "agent")
    secret_registry = object()
    resolver = UserResourceResolver(root, agent, secrets=secret_registry)
    owner = "../Alice/\nowner: bob/用户"

    resources = resolver.for_owner(owner)
    again = resolver.for_owner(owner)
    other = resolver.for_owner("bob")

    assert resources is again
    assert resources is not other
    assert resources.scope == "user"
    assert resources.root == root.resolve() / _owner_key(owner)
    assert resources.root.name == _owner_key(owner)
    assert owner not in str(resources.root.relative_to(root.resolve()))
    assert resources.memory.dir == resources.root / "memory"
    assert resources.memory.secrets is secret_registry
    assert (resources.root / "skills").is_dir()
    with pytest.raises(FrozenInstanceError):
        resources.root = tmp_path / "elsewhere"  # type: ignore[misc]


def test_resolver_gives_each_owner_only_their_skill_snapshot(tmp_path):
    root = tmp_path / "users"
    agent_root = tmp_path / "agent"
    _skill(agent_root, "shared", name="shared", body="AGENT SHARED")
    for owner, body in (("alice", "ALICE PROFILE"), ("bob", "BOB PROFILE")):
        _skill(
            root / _owner_key(owner) / "skills",
            "profile",
            name="profile",
            body=body,
        )

    resolver = UserResourceResolver(root, SkillLoader(agent_root))
    alice = resolver.for_owner("alice")
    bob = resolver.for_owner("bob")

    assert "ALICE PROFILE" in alice.skills.load("user:profile")
    assert "BOB PROFILE" not in alice.skills.load("user:profile")
    assert "BOB PROFILE" in bob.skills.load("user:profile")
    assert "ALICE PROFILE" not in bob.skills.load("user:profile")
    assert "AGENT SHARED" in alice.skills.load("agent:shared")
    assert "AGENT SHARED" in bob.skills.load("agent:shared")


def test_resolver_does_not_merge_owner_problem_views(tmp_path):
    root = tmp_path / "users"
    for owner in ("alice", "bob"):
        _skill(
            root / _owner_key(owner) / "skills",
            "invalid",
            name="not a valid name",
        )

    resolver = UserResourceResolver(root, SkillLoader(tmp_path / "agent"))
    alice = resolver.for_owner("alice")
    alice_problems = tuple(alice.skills.problems)
    bob = resolver.for_owner("bob")

    assert alice.skills.problems is not bob.skills.problems
    assert tuple(alice.skills.problems) == alice_problems
    assert any(_owner_key("alice") in problem for problem in alice_problems)
    assert all(_owner_key("bob") not in problem for problem in alice.skills.problems)
    assert any(_owner_key("bob") in problem for problem in bob.skills.problems)
    assert any(_owner_key("alice") in problem for problem in resolver.problems)
    assert any(_owner_key("bob") in problem for problem in resolver.problems)

    alice.skills.problems.append("late alice catalogue problem")
    assert any("late alice" in problem for problem in resolver.problems)
    assert all("late alice" not in problem for problem in bob.skills.problems)


def test_a_user_skill_file_cannot_follow_a_link_outside_its_source(tmp_path):
    source = tmp_path / "skills"
    outside = tmp_path / "outside" / "SKILL.md"
    outside.parent.mkdir()
    outside.write_text(
        "---\nname: escaped\ndescription: outside\n---\nOUTSIDE BODY"
    )
    linked = source / "escaped"
    linked.mkdir(parents=True)
    (linked / "SKILL.md").symlink_to(outside)

    loader = SkillLoader(source)

    assert "escaped" not in loader.skills
    assert any("outside its source root" in problem for problem in loader.problems)


def test_resolver_refuses_a_preplanted_owner_directory_symlink(tmp_path):
    root = tmp_path / "users"
    outside = tmp_path / "outside"
    root.mkdir()
    outside.mkdir()
    (root / _owner_key("alice")).symlink_to(outside, target_is_directory=True)
    resolver = UserResourceResolver(root, SkillLoader(tmp_path / "agent"))

    with pytest.raises(RuntimeError, match="must not be a symlink"):
        resolver.for_owner("alice")
