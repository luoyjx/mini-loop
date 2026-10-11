"""Every user's memories were injected into every other user's context.

Round 77's lesson was that isolation has to be tested with *both* parties
holding data. That lens applies past HTTP, and the manager builds **one**
`MemoryStore` for every session it creates. Round 61 established that its index
rides in the runtime facts of every turn. Together:

    alice  context own=True  OTHER'S=True
    bob    context own=True  OTHER'S=True

Bob's confidential memory arrived in Alice's model context automatically, each
turn, with no API call involved -- worse in that respect than the trajectory
disclosure of rounds 74 to 76, which at least required asking.

The shared store is right for the case it was built for: one user carrying
knowledge between their own sessions. It became wrong when the same process
started serving two callers, which is what the auth work of round 24 and the
scoping of rounds 74 to 77 are for. Scoping by owner keeps both: the same person
still sees their memories across their sessions.

Records written before the field are `anonymous`, so an unauthenticated
single-user deployment keeps seeing its own -- asserted below, because a
security fix that silently empties someone's memory is its own defect.
"""

import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic
from mini_loop.memory import MemoryStore, ScopedMemory, memory_store_for
from mini_loop.prompts import runtime_facts

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
ALICE_NOTE = "ALICE-CONFIDENTIAL launch date"
BOB_NOTE = "BOB-CONFIDENTIAL budget 40k"


@pytest.fixture
def tenants(tmp_path):
    """Both callers hold memories. With one holding none, an empty index and a
    scoped one are the same observation -- round 77."""
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS,
                 memory_root=tmp_path / "mem"),
        FakeAsyncAnthropic(),
        tool_registry=full_registry(),
    )
    alice, bob = manager.create(), manager.create()
    alice.owner, bob.owner = "alice", "bob"
    memory_store_for(alice.agent).write("alice-note", "project", ALICE_NOTE, "body")
    memory_store_for(bob.agent).write("bob-note", "project", BOB_NOTE, "body")
    return manager, alice, bob


def test_the_store_really_is_shared(tenants):
    """Otherwise this file tests two isolated stores and proves nothing."""
    _, alice, bob = tenants
    a = memory_store_for(alice.agent)
    b = memory_store_for(bob.agent)
    assert a._store is b._store


@pytest.mark.parametrize("viewer,mine,theirs", [
    ("alice", ALICE_NOTE, BOB_NOTE),
    ("bob", BOB_NOTE, ALICE_NOTE),
])
def test_context_carries_only_the_callers_memories(tenants, viewer, mine, theirs):
    _, alice, bob = tenants
    session = alice if viewer == "alice" else bob
    facts = runtime_facts(session.agent) or ""

    assert mine in facts, "the caller lost their own memories"
    assert theirs not in facts, "another caller's memory reached this context"


@pytest.mark.parametrize("viewer,mine,theirs", [
    ("alice", ALICE_NOTE, BOB_NOTE),
    ("bob", BOB_NOTE, ALICE_NOTE),
])
def test_recall_returns_only_the_callers_memories(tenants, viewer, mine, theirs):
    _, alice, bob = tenants
    session = alice if viewer == "alice" else bob
    found = str(memory_store_for(session.agent).search(""))

    assert mine in found
    assert theirs not in found


def test_the_index_written_to_disk_is_not_the_scoping(tenants):
    """`MEMORY.md` holds everything; the scoping is on the read path. If it were
    the file, two owners would overwrite each other's index."""
    _, alice, bob = tenants
    store = memory_store_for(alice.agent)._store
    store.flush()
    contents = (store.dir / "MEMORY.md").read_text()
    assert "alice-note" in contents and "bob-note" in contents


# --- the seam, and the way round it -------------------------------------

def test_the_seam_returns_a_scoped_view(tenants):
    _, alice, _ = tenants
    scoped = memory_store_for(alice.agent)
    assert isinstance(scoped, ScopedMemory)
    assert scoped.owner == "alice"


def test_reading_the_raw_store_is_what_leaked(tenants):
    """Kept as a test because it is the failure mode, not a hypothetical.

    `recall` was scoped first and the context still leaked: `runtime_facts` read
    `agent.state["memory"]` directly, around the seam. One call site outside is
    all it takes, which is round 26's lesson.
    """
    _, alice, _ = tenants
    raw = alice.agent.state["memory"]
    assert BOB_NOTE in raw.index(), (
        "the raw store is expected to hold everything; the scoping is the view"
    )
    assert BOB_NOTE not in (runtime_facts(alice.agent) or "")


# --- the case the shared store was built for ----------------------------

def test_a_single_user_deployment_still_sees_its_memories(tmp_path):
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS,
                 memory_root=tmp_path / "mem"),
        FakeAsyncAnthropic(),
        tool_registry=full_registry(),
    )
    session = manager.create()
    memory_store_for(session.agent).write("solo", "project", "SOLO-NOTE", "body")
    assert "SOLO-NOTE" in (runtime_facts(session.agent) or "")


def test_one_person_keeps_their_memories_across_sessions(tmp_path):
    """The point of a shared store, which scoping must not break."""
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS,
                 memory_root=tmp_path / "mem"),
        FakeAsyncAnthropic(),
        tool_registry=full_registry(),
    )
    first, second = manager.create(), manager.create()
    first.owner = second.owner = "alice"
    memory_store_for(first.agent).write("carried", "project", "CARRIED-NOTE", "body")
    assert "CARRIED-NOTE" in (runtime_facts(second.agent) or "")


def test_a_memory_written_before_the_field_is_anonymous(tmp_path):
    """A security fix that silently empties an existing deployment's memory is
    its own defect."""
    store = MemoryStore(tmp_path / "mem")
    (store.dir / "legacy.md").write_text(
        "---\nname: legacy\ndescription: LEGACY-NOTE\ntype: project\n---\n\nbody\n"
    )
    assert any(m["owner"] == "anonymous" for m in store.list())
    assert "LEGACY-NOTE" in store.index("anonymous")
    assert "LEGACY-NOTE" not in store.index("alice")
