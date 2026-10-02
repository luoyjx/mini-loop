"""`remember` got slower the more the agent remembered.

Round 51 ended on "a rule only holds when something executes it", which makes
the suite the thing that executes every rule in this repo -- so its runtime is
load-bearing. It had just doubled, from about 10s to 22s, and two tests
accounted for half of that. The tests were not the problem.

`write` rebuilt `MEMORY.md`, the rebuild called `list()`, and `list()` parsed
*every* memory file. Storing N memories therefore read N^2/2 files, on a path
the model touches every time it remembers something:

    memories   total s  per write ms  file reads
          50      0.03          0.65       1,275
         100      0.11          1.14       5,050
         200      0.43          2.13      20,100
         400      1.68          4.20      80,200

Two fixes, and the first alone was not enough. Caching parses by (mtime, size)
made the *reads* linear -- 800 instead of 320,400 -- and left the time
quadratic, because every write still materialised the whole index. Deferring
the rebuild to the next read fixed the rest:

    memories   total s  per write ms
         400     0.040         0.101
        1600     0.164         0.102

Flat per write, 42x faster at 400 memories, and the suite is back to 10s.
"""

import pathlib
import time

import pytest

from mini_loop.memory import MemoryStore


def _store(tmp_path):
    return MemoryStore(tmp_path / "mem")


def _write_many(store, count, *, body="a small body"):
    for index in range(count):
        store.write(f"m{index}", "project", "a description", body)


# --- scaling --------------------------------------------------------------

def _count_reads(action):
    reads = {"count": 0}
    real = pathlib.Path.read_text

    def counting(self, *args, **kwargs):
        reads["count"] += 1
        return real(self, *args, **kwargs)

    pathlib.Path.read_text = counting
    try:
        action()
    finally:
        pathlib.Path.read_text = real
    return reads["count"]


def test_writing_a_memory_does_not_read_every_other_one(tmp_path):
    """The direct cause: `list()` parsed the whole directory on every write."""
    store = _store(tmp_path)
    _write_many(store, 60)
    assert _count_reads(lambda: store.write("one-more", "project", "d", "b")) <= 2


def test_re_reading_the_index_does_not_re_parse_unchanged_memories(tmp_path):
    """Where the parse cache earns its place now.

    Deferring the rebuild took reads out of `write` entirely, which changed what
    the cache is *for*: `index()` runs on every turn through the runtime facts,
    so without it each turn re-parses every memory the agent has ever kept. The
    first version of this test measured reads during a write and, once writes
    stopped reading at all, could not tell the cache from its absence -- the
    mutation runner said so.
    """
    store = _store(tmp_path)
    _write_many(store, 60)
    store.index()                       # first pass populates the cache

    assert _count_reads(store.index) == 0, "every turn re-parses the whole store"


def test_a_new_memory_costs_one_read_not_sixty(tmp_path):
    store = _store(tmp_path)
    _write_many(store, 60)
    store.index()
    store.write("brand-new", "project", "d", "b")
    assert _count_reads(store.index) == 1


def test_write_cost_does_not_grow_with_the_store(tmp_path):
    """Quadratic is the thing being guarded, so this compares two sizes rather
    than asserting an absolute time -- absolute times are a CI flake."""
    def cost(count):
        store = MemoryStore(tmp_path / f"mem{count}")
        started = time.monotonic()
        _write_many(store, count)
        store.flush()
        return (time.monotonic() - started) / count

    small, large = cost(100), cost(800)
    assert large < small * 4, (
        f"per-write cost went from {small * 1000:.2f}ms at 100 memories to "
        f"{large * 1000:.2f}ms at 800; that is superlinear growth"
    )


# --- the cache must not serve stale content ------------------------------

def test_an_updated_memory_is_re_read(tmp_path):
    store = _store(tmp_path)
    store.write("alpha", "project", "first description", "first body")
    assert "first description" in store.index()

    store.write("alpha", "project", "second description", "second body")
    assert "second description" in store.index()
    assert "first description" not in store.index()


def test_a_file_changed_underneath_the_process_is_re_read(tmp_path):
    """An operator with an editor, and `replace_all`, both do this."""
    store = _store(tmp_path)
    store.write("alpha", "project", "original", "body")
    assert "original" in store.index()

    path = store.dir / "alpha.md"
    path.write_text(path.read_text().replace("original", "edited-on-disk"))
    # mtime granularity: make sure the change is observable.
    import os
    stat = path.stat()
    os.utime(path, ns=(stat.st_atime_ns, stat.st_mtime_ns + 1_000_000))

    assert "edited-on-disk" in store.index()


def test_a_deleted_memory_disappears(tmp_path):
    store = _store(tmp_path)
    store.write("alpha", "project", "d", "b")
    store.write("beta", "project", "d", "b")
    (store.dir / "alpha.md").unlink()
    assert "alpha" not in store.index()
    assert "beta" in store.index()


def test_replace_all_is_reflected(tmp_path):
    store = _store(tmp_path)
    store.write("old", "project", "d", "b")
    store.replace_all([{"name": "new", "type": "project",
                        "description": "d", "body": "b"}])
    assert "new" in store.index()
    assert "old" not in store.index()


# --- the deferred index is still an index --------------------------------

def test_reading_the_index_flushes_it(tmp_path):
    store = _store(tmp_path)
    _write_many(store, 5)
    store.index()
    written = (store.dir / "MEMORY.md").read_text()
    assert written.count("- [") == 5


def test_searching_sees_the_newest_memory(tmp_path):
    store = _store(tmp_path)
    store.write("alpha", "project", "about deployments", "b")
    found = store.search("deployments")
    assert any(item["name"] == "alpha" for item in found)


def test_the_file_on_disk_catches_up(tmp_path):
    """`MEMORY.md` is eventually consistent for anything outside the process;
    `flush()` is what makes that bounded rather than indefinite."""
    store = _store(tmp_path)
    _write_many(store, 3)
    store.flush()
    assert (store.dir / "MEMORY.md").read_text().count("- [") == 3
