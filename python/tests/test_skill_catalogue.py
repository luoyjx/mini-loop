"""Skills answer the same questions every other content store was asked.

Round 81 swept the stores for what they do with a corrupt file, and round 46
capped what rides in every request. Skills were in neither sweep, and they are
the only store whose content is an *instruction* the model is told it has.

Two answers, and they are mirror images of defects already fixed elsewhere.

**One undecodable byte took out the loader.** `SkillLoader.__init__` reads every
`SKILL.md` eagerly and the read was unguarded, so construction raised -- and
every turn of every session with it, including sessions using a completely
different, valid skill. Round 81 found exactly this in memory. A skills
directory is somewhere an operator drops files by hand, so it is not a trusted
input either.

**The catalogue was unbounded, and the body was not.** The body is loaded on
demand and was capped at 50,000 characters rounds ago; the *description* rides
in the system prompt of every request and was capped nowhere:

    100 skills x 4,000-character descriptions
      -> 400,989 characters, ~100,000 tokens per request
      -> after: 7,923 characters, ~1,980 tokens

Round 46 found the same inversion in memory with the halves swapped: the index
capped and the body not. The lesson that transfers is not "cap the index", it is
*whichever half is paid per request is the one that has to be bounded*.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.skills import (
    MAX_SKILL_CATALOGUE,
    MAX_SKILL_DESCRIPTION,
    SkillLoader,
)


def _skill(root, name, *, description="a real skill", body="body"):
    folder = root / name
    folder.mkdir(parents=True, exist_ok=True)
    (folder / "SKILL.md").write_text(
        f"---\nname: {name}\ndescription: {description}\n---\n\n{body}"
    )
    return folder


# -- the corrupt file -----------------------------------------------------

def test_one_unreadable_skill_does_not_take_out_the_rest(tmp_path):
    root = tmp_path / "skills"
    _skill(root, "good")
    (root / "poison").mkdir(parents=True)
    (root / "poison" / "SKILL.md").write_bytes(b"\xff\xfe\x00")

    loader = SkillLoader(root)
    assert list(loader.skills) == ["good"]
    assert loader.load("good")


def test_an_unreadable_skill_is_reported(tmp_path):
    root = tmp_path / "skills"
    (root / "poison").mkdir(parents=True)
    (root / "poison" / "SKILL.md").write_bytes(b"\xff\xfe\x00")

    loader = SkillLoader(root)
    assert any("unreadable" in p for p in loader.problems.summary())


@pytest.mark.asyncio
async def test_a_poisoned_skills_directory_does_not_end_every_turn(tmp_path):
    """The blast radius, not the loader: descriptions ride in every request."""

    root = tmp_path / "skills"
    _skill(root, "good")
    (root / "poison").mkdir(parents=True)
    (root / "poison" / "SKILL.md").write_bytes(b"\xff\xfe\x00")

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=root),
        FakeAsyncAnthropic(),
        tool_registry=full_registry(),
    )
    assert await manager.create().run("say hi")


# -- the per-request cost -------------------------------------------------

def test_the_catalogue_is_bounded(tmp_path):
    root = tmp_path / "skills"
    for i in range(100):
        _skill(root, f"s{i}", description="D" * 4_000)

    catalogue = SkillLoader(root).descriptions()
    assert len(catalogue) <= MAX_SKILL_CATALOGUE + 200, (
        f"{len(catalogue):,} characters in every system prompt"
    )


def test_a_long_description_is_truncated_and_reported(tmp_path):
    root = tmp_path / "skills"
    _skill(root, "verbose", description="D" * 4_000)

    loader = SkillLoader(root)
    assert len(loader.skills["verbose"]["meta"]["description"]) <= MAX_SKILL_DESCRIPTION + 3
    assert any("description truncated" in p for p in loader.problems.summary())


def test_omitted_skills_are_named_not_dropped_silently(tmp_path):
    """A skill missing from the catalogue is a capability the model cannot know
    it has, so the omission has to be visible in the prompt itself."""

    root = tmp_path / "skills"
    for i in range(200):
        _skill(root, f"s{i}", description="D" * 200)

    loader = SkillLoader(root)
    catalogue = loader.descriptions()
    assert "omitted" in catalogue
    assert any("omitted from the catalogue" in p for p in loader.problems.summary())


def test_an_ordinary_directory_is_untouched(tmp_path):
    """Not vacuous: neither bound may fire on a normal set of skills."""

    root = tmp_path / "skills"
    for i in range(6):
        _skill(root, f"s{i}", description=f"does thing {i}")

    loader = SkillLoader(root)
    assert not loader.problems
    catalogue = loader.descriptions()
    assert "omitted" not in catalogue
    assert all(f"s{i}" in catalogue for i in range(6))


def test_the_shipped_skills_fit(tmp_path):
    """The repo's own skills must not already be against the bound."""

    shipped = pathlib.Path(__file__).resolve().parent.parent / "skills"
    loader = SkillLoader(shipped)
    assert loader.skills, "the shipped skills directory loaded nothing"
    assert "omitted" not in loader.descriptions()
