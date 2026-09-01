"""Memory naming census: names that normalize to nothing destroyed data.

The probe wrote two Chinese-named memories and got one back: `_slug`
stripped every non-ASCII character, both names fell to the bare
"memory" fallback, and the second write silently destroyed the first --
for a CJK-writing operator, most names. Worse, on a case-insensitive
filesystem (macOS APFS) that fallback file folds onto the MEMORY.md
index, so the next flush overwrote the memory with the index and then
served the index text back as a memory named "memory".

Fixed and pinned here (docs/RSI_RESEARCH_AND_PLAN.md §5): the fallback
slug is stable per exact name (memory-<sha8>), the reserved index name
can never be a memory's filename, and index text is never served as a
memory. Benign behaviors pinned while the census was here: traversal
confinement, noticed body truncation, last-write-wins per exact name.
"""

import pathlib

from mini_loop.memory import MAX_BODY, MemoryStore


def _store(tmp_path) -> MemoryStore:
    return MemoryStore(tmp_path / "memory")


def test_distinct_cjk_names_no_longer_destroy_each_other(tmp_path):
    store = _store(tmp_path)
    store.write("部署流程", "reference", "第一条", "body-1")
    store.write("发布检查表", "reference", "第二条", "body-2")

    rows = {m["name"]: m["body"] for m in store.list()}
    assert rows == {"部署流程": "body-1", "发布检查表": "body-2"}

    # Rewriting one exact name overwrites only itself.
    store.write("部署流程", "reference", "第一条改", "body-1b")
    rows = {m["name"]: m["body"] for m in store.list()}
    assert rows == {"部署流程": "body-1b", "发布检查表": "body-2"}


def test_the_index_flush_cannot_clobber_a_memory(tmp_path):
    """On a case-insensitive filesystem the old fallback file memory.md
    IS MEMORY.md -- flushing the index used to overwrite the memory and
    then serve the index text back as a memory named 'memory'."""

    store = _store(tmp_path)
    store.write("部署流程", "reference", "发布用", "body-cn")
    store.flush()

    (row,) = store.list()
    assert row["name"] == "部署流程" and row["body"] == "body-cn"
    assert not row["file"].lower() == "memory.md", (
        "a memory filename that case-folds onto the index invites the "
        "clobber back"
    )
    names = sorted(p.name.lower() for p in (tmp_path / "memory").glob("*.md"))
    assert "memory.md" in names, "the index itself still exists"


def test_index_text_is_never_served_as_a_memory(tmp_path):
    """A legacy store may hold index text inside memory.md (the macOS
    clobber's leftovers). It parses -- and is excluded; a real legacy
    record living in memory.md keeps its frontmatter identity."""

    store = _store(tmp_path)
    store.write("anchor", "reference", "d", "b")  # ensures the dir exists
    (tmp_path / "memory" / "memory.md").write_text(
        "# Memory index\n\n- [部署流程](memory.md) — leftovers\n")
    assert {m["name"] for m in store.list()} == {"anchor"}

    (tmp_path / "memory" / "memory.md").write_text(
        "---\nname: legacy-real\ndescription: kept\ntype: reference\n---\n\nlegacy body\n")
    assert {m["name"] for m in store.list()} == {"anchor", "legacy-real"}


def test_a_traversal_name_stays_confined(tmp_path):
    store = _store(tmp_path)
    store.write("../evil", "reference", "d", "b")
    root = tmp_path / "memory"
    files = [p for p in tmp_path.rglob("*.md")]
    assert all(root in p.parents for p in files), (
        "a memory file escaped the store directory"
    )
    assert [m["name"] for m in store.list()] == ["../evil"]


def test_an_oversized_body_truncates_with_a_marker(tmp_path):
    """Round-62 rule, upheld here since round 46's checklist: the cut is
    noticed in the body AND reported to the problem ledger."""

    store = _store(tmp_path)
    store.write("big", "reference", "d", "X" * (MAX_BODY + 5_000))
    (row,) = store.list()
    assert row["body"].endswith("[memory truncated]")
    assert any("truncated" in p for p in store.problems)
