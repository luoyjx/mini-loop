"""A store's own directory is not a trusted input.

Every durable store here reads files it did not necessarily write. The agent
writes memories with its own tools, an operator edits them by hand, and a
process killed mid-write leaves a half-file behind. Rounds 45 to 50 built a
reporting channel for exactly this and wired it to the *write* paths; the read
paths were never asked the same question.

Asking all of them at once found two answers, one bad and one worse:

* `TaskStore.load` swallowed four exception types into `return None`, which is
  also how it says "no such task". A board with one corrupt file reported one
  task where two existed and said nothing.
* `MemoryStore._parse_uncached` called `read_text()` unguarded, and `list()`
  parses every file, so one undecodable byte took out `list`, `index` and
  `search` together. `index()` is called while building every request, so three
  bytes in the memory directory ended every turn of every session.

The second is why this file drives a real turn rather than asserting at the
store: the store-level bug is a raised exception, and the thing worth pinning is
that it reached the request builder.
"""

import pathlib
import tempfile

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.memory import MemoryStore
from mini_loop.tasks import TaskStore

POISON = b"\xff\xfe\x00"
GOOD_MEMORY = (
    "---\nname: real\ndescription: matters\nmetadata:\n  type: project\n---\nbody"
)


def _memory(tmp_path):
    root = tmp_path / "mem"
    root.mkdir(parents=True, exist_ok=True)
    return root


# -- memory ---------------------------------------------------------------

def test_an_unreadable_memory_does_not_take_out_the_rest(tmp_path):
    root = _memory(tmp_path)
    (root / "good.md").write_text(GOOD_MEMORY)
    (root / "poison.md").write_bytes(POISON)

    store = MemoryStore(root)
    assert [m["name"] for m in store.list()] == ["real"]
    assert "real" in store.index()
    assert store.search("matters")


def test_an_unreadable_memory_is_reported(tmp_path):
    root = _memory(tmp_path)
    (root / "poison.md").write_bytes(POISON)

    store = MemoryStore(root)
    store.list()
    assert any("poison.md" in p for p in store.problems.summary())


@pytest.mark.asyncio
async def test_a_poisoned_memory_directory_does_not_end_every_turn(tmp_path):
    """The blast radius, not the store.

    `runtime_facts` calls `index()` while building the request, so this failed
    on the *first* turn of every session on the manager -- including sessions
    that never touched memory.
    """

    settings = Settings(
        fake_llm=True,
        workspace_root=tmp_path / "ws",
        memory_root=tmp_path / "mem",
        skills_dir=pathlib.Path(__file__).resolve().parent.parent / "skills",
    )
    manager = SessionManager(
        settings, FakeAsyncAnthropic(), tool_registry=full_registry()
    )
    session = manager.create()

    assert await session.agent.run("say hi")          # not vacuous: clean first
    root = _memory(tmp_path)
    (root / "poison.md").write_bytes(POISON)
    assert await session.agent.run("say hi again")


# -- tasks ----------------------------------------------------------------

def test_a_corrupt_task_is_reported_rather_than_vanishing(tmp_path):
    store = TaskStore(tmp_path / "board")
    store.create(subject="real work")
    written = next((tmp_path / "board").rglob("task_*.json"))
    # The name has to match the glob the store actually reads, or the file is
    # not corrupt input at all -- it is a file nobody looks at.
    written.parent.joinpath("task_deadbeef00.json").write_text('{"id": "x", "subj')

    reopened = TaskStore(tmp_path / "board")
    assert len(list(written.parent.glob("task_*.json"))) == 2
    assert len(reopened.list()) == 1
    assert any("task_deadbeef00" in p for p in reopened.problems.summary())


def test_a_missing_task_is_still_silent(tmp_path):
    """`None` for "no such task" must stay unremarkable, or every miss reports."""

    store = TaskStore(tmp_path / "board")
    assert store.load("task_absent0000") is None
    assert not store.problems
