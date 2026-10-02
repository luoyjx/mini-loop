"""The policy set as one value, plus a structural guard against bypass.

Five seams were added over five iterations, and two construction sites silently
missed some of them -- workflow workers ran without secret masking or
sandboxing. Nothing failed; the capability was just absent on one path. Tests
that exercise a seam in isolation cannot catch that, so the guard below asserts
the *shape* of every `Agent(...)` call in the package instead.
"""

import ast
import dataclasses
from pathlib import Path

import pytest

from mini_loop.harness import Harness

PACKAGE = Path(__file__).resolve().parent.parent / "mini_loop"


# --- the value ------------------------------------------------------------

def test_derive_copies_everything_not_overridden():
    base = Harness(secrets="S", sandbox="B", cache_policy="C")
    child = base.derive(tools="T")

    assert child.tools == "T"
    # The point of the whole exercise: unlisted seams come along.
    assert child.secrets == "S"
    assert child.sandbox == "B"
    assert child.cache_policy == "C"


def test_derive_rejects_an_unknown_field():
    """A typo'd seam name must fail loudly, not vanish."""
    with pytest.raises(TypeError) as excinfo:
        Harness().derive(sandbxo="typo")
    assert "sandbxo" in str(excinfo.value)


def test_a_harness_is_immutable():
    harness = Harness(secrets="S")
    with pytest.raises(dataclasses.FrozenInstanceError):
        harness.secrets = "other"


def test_resolve_prefers_an_explicit_argument():
    harness = Harness(secrets="from-harness")
    assert harness.resolve("secrets", "explicit") == "explicit"
    assert harness.resolve("secrets", None) == "from-harness"


def test_unset_fields_stay_unset():
    """`None` means 'the Agent's own default', not 'today's default frozen in'."""
    harness = Harness()
    for f in dataclasses.fields(Harness):
        if f.name == "injectors":
            continue
        assert getattr(harness, f.name) is None


# --- the structural guard --------------------------------------------------

def _agent_constructions() -> list[tuple[str, int, set[str]]]:
    """Every `Agent(...)` call in the package, with the keywords it passes."""
    found = []
    for path in PACKAGE.rglob("*.py"):
        tree = ast.parse(path.read_text(), filename=str(path))
        for node in ast.walk(tree):
            if not isinstance(node, ast.Call):
                continue
            func = node.func
            name = getattr(func, "id", None) or getattr(func, "attr", None)
            if name != "Agent":
                continue
            keywords = {kw.arg for kw in node.keywords if kw.arg}
            found.append((str(path.relative_to(PACKAGE)), node.lineno, keywords))
    return found


def test_the_guard_actually_finds_the_construction_sites():
    """A guard that matches nothing passes for the wrong reason."""
    sites = _agent_constructions()
    assert len(sites) >= 2, f"expected several Agent(...) sites, found {sites}"


def test_every_agent_construction_goes_through_a_harness():
    """The rule that replaces 'remember to thread the new seam everywhere'.

    A new construction site that lists seams by hand is exactly how workflow
    workers lost masking; requiring `harness=` makes inheritance the default and
    omission the thing you have to write on purpose.
    """
    offenders = [
        f"{path}:{line} passes {sorted(keywords)}"
        for path, line, keywords in _agent_constructions()
        if "harness" not in keywords
    ]
    assert not offenders, (
        "these Agent(...) calls bypass the harness, so a seam added to Harness "
        "will not reach them:\n  " + "\n  ".join(offenders)
    )


def test_seams_are_not_passed_alongside_the_harness_by_hand():
    """Listing a seam next to `harness=` reintroduces the drift it removes."""
    seam_names = {
        f.name for f in dataclasses.fields(Harness)
    } - {"injectors"}  # injectors stay an explicit Agent argument
    offenders = []
    for path, line, keywords in _agent_constructions():
        if "harness" not in keywords:
            continue
        leaked = keywords & seam_names
        if leaked:
            offenders.append(f"{path}:{line} also passes {sorted(leaked)}")
    assert not offenders, (
        "pass these through `harness.derive(...)` instead:\n  "
        + "\n  ".join(offenders)
    )


# --- the seams actually reach a built agent -------------------------------

def test_an_agent_adopts_its_harness(tmp_path):
    from mini_loop.agent import Agent
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.secrets import SecretRegistry
    from mini_loop.skills import SkillLoader

    registry = SecretRegistry()
    skills = SkillLoader(PACKAGE.parent / "skills")
    agent = Agent(
        client=FakeAsyncAnthropic(),
        settings=Settings(
            fake_llm=True,
            workspace_root=tmp_path / "ws",
            skills_dir=PACKAGE.parent / "skills",
        ),
        workspace=tmp_path / "ws",
        harness=Harness(secrets=registry, skills=skills),
    )
    assert agent.secrets is registry
    # The toolset must see it too, or masking never reaches the shell.
    assert agent.toolset.secrets is registry


def test_a_subagent_inherits_a_seam_it_was_never_told_about(tmp_path):
    """The regression this design exists to prevent."""
    from mini_loop.agent import Agent
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.secrets import SecretRegistry

    registry = SecretRegistry()
    parent = Agent(
        client=FakeAsyncAnthropic(),
        settings=Settings(
            fake_llm=True,
            workspace_root=tmp_path / "ws",
            skills_dir=PACKAGE.parent / "skills",
        ),
        workspace=tmp_path / "ws",
        harness=Harness(secrets=registry),
    )
    child_harness = parent.harness.derive(tools=None)
    assert child_harness.secrets is registry
