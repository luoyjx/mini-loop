"""Every model-visible input is recoverable from the durable log.

dsh's reconstructable-requests rule. Messages live in the epoch table and
the system prompt in the session record; the tool schemas -- equally
model-visible -- were represented in the log only by their fingerprint, so
a past request could not be rebuilt once the catalog changed (an MCP
connect, a plan-mode registry, a role policy). One `tool_catalog` event per
distinct fingerprint now carries the schemas, and every `model_start`
references them by fingerprint.
"""

import asyncio
import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.storage import SQLiteStateStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _manager(tmp_path, store, responder=None):
    return SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(responder=responder) if responder else FakeAsyncAnthropic(),
        state_store=store,
    )


def test_the_catalog_is_recoverable_by_fingerprint(tmp_path):
    store = SQLiteStateStore(tmp_path / "state.db")
    session = _manager(tmp_path, store).create()
    asyncio.run(session.run("hello"))

    events = store.load_events(session.id)
    catalogs = [e for e in events if e.get("type") == "tool_catalog"]
    starts = [e for e in events if e.get("type") == "model_start"]
    assert catalogs, "no durable catalog event was written"
    assert starts, "no model_start recorded"
    by_print = {c["fingerprint"]: c["schemas"] for c in catalogs}
    for start in starts:
        schemas = by_print.get(start.get("tool_catalog_fingerprint"))
        assert schemas, "a request references a catalog the log does not hold"
        assert any(s.get("name") == "bash" for s in schemas)
    store.close()


def test_one_event_per_distinct_catalog(tmp_path):
    """The schemas are written once, not once per round."""
    responder = scripted([
        ([text("a"), tool("bash", _id="t1", command="echo 1")], "tool_use"),
        ([text("b"), tool("bash", _id="t2", command="echo 2")], "tool_use"),
        ([text("done")], "end_turn"),
    ])
    store = SQLiteStateStore(tmp_path / "state.db")
    session = _manager(tmp_path, store, responder).create()
    asyncio.run(session.run("three rounds"))

    events = store.load_events(session.id)
    catalogs = [e for e in events if e.get("type") == "tool_catalog"]
    starts = [e for e in events if e.get("type") == "model_start"]
    assert len(starts) == 3
    assert len(catalogs) == 1, "an unchanged catalog was re-logged per round"
    store.close()


def test_a_catalog_change_writes_a_second_event(tmp_path):
    from mini_loop.registry import Tool

    responder = scripted([
        ([text("a"), tool("bash", _id="t1", command="echo 1")], "tool_use"),
        ([text("done")], "end_turn"),
        ([text("later")], "end_turn"),
    ])
    store = SQLiteStateStore(tmp_path / "state.db")
    session = _manager(tmp_path, store, responder).create()
    asyncio.run(session.run("first turn"))

    async def novelty(ctx) -> str:
        return "ok"

    session.agent.tools.register(Tool(
        "novelty", "a tool that did not exist in turn one",
        {"type": "object", "properties": {}}, novelty,
    ))
    asyncio.run(session.run("second turn"))

    events = store.load_events(session.id)
    catalogs = [e for e in events if e.get("type") == "tool_catalog"]
    assert len(catalogs) == 2, "a changed catalog did not write its schemas"
    prints = {c["fingerprint"] for c in catalogs}
    assert len(prints) == 2
    assert any(
        any(s.get("name") == "novelty" for s in c["schemas"])
        for c in catalogs
    )
    store.close()


# -- the round trip (round 198) ----------------------------------------------
# Round 197 made reconstruction POSSIBLE; a claim nobody executes is
# documentation, not a property (round 99's lesson). `reconstruct_request`
# performs the join, and this proves it byte-identical against what the
# model actually received -- including for a request from a superseded
# epoch, which is where the `transcript_epoch` stamp earns its keep.

def _normalized(messages):
    import json as _json

    from mini_loop.storage import _json_safe

    return _json.dumps(_json_safe(messages), sort_keys=True, default=str)


def test_the_round_trip_is_exact(tmp_path):
    import json as _json

    from mini_loop.session_query import reconstruct_request

    responder = scripted([
        ([text("step"), tool("bash", _id="t1", command="echo hi")], "tool_use"),
        ([text("done")], "end_turn"),
    ])
    store = SQLiteStateStore(tmp_path / "state.db")
    session = _manager(tmp_path, store, responder).create()

    live = []
    real_create = session.agent._create

    async def spying_create(messages, **kwargs):
        live.append({
            "messages": _json.loads(_normalized(messages)),
            "system": kwargs.get("system"),
            "tools": kwargs.get("tools"),
        })
        return await real_create(messages, **kwargs)

    session.agent._create = spying_create
    asyncio.run(session.run("round trip me"))

    starts = [e for e in store.load_events(session.id)
              if e.get("type") == "model_start"]
    assert len(starts) == len(live) == 2
    for start, seen in zip(starts, live):
        rebuilt = reconstruct_request(store, session.id, start["seq"])
        assert "error" not in rebuilt, rebuilt.get("error")
        assert _normalized(rebuilt["messages"]) == _normalized(seen["messages"])
        # The spy sits at `_create`'s entry, BEFORE the cache policy renders
        # the system prompt into blocks; the event records the post-policy
        # form the model actually received. Compare the words, which both
        # shapes carry.
        from mini_loop.fake_llm import system_text

        assert system_text(rebuilt["system"]) == system_text(seen["system"])
        assert rebuilt["tools"] == seen["tools"]
    store.close()


def test_a_superseded_epoch_request_still_reconstructs(tmp_path):
    """The stamp earns its keep: rebuild turn one AFTER compaction moved on."""
    from mini_loop.session_query import reconstruct_request

    store = SQLiteStateStore(tmp_path / "state.db")
    session = _manager(tmp_path, store).create()
    asyncio.run(session.run("first turn"))
    first_start = next(e for e in store.load_events(session.id)
                       if e.get("type") == "model_start")
    original = reconstruct_request(store, session.id, first_start["seq"])

    # A sanctioned rewrite (what every compactor does) bumps the epoch.
    session.agent.messages[:] = [
        {"role": "user", "content": "summary of everything so far"},
    ]
    asyncio.run(session.run("second turn"))

    after = reconstruct_request(store, session.id, first_start["seq"])
    assert "error" not in after, after.get("error")
    assert _normalized(after["messages"]) == _normalized(original["messages"])
    assert any("first turn" in str(m) for m in after["messages"]), (
        "the pre-compaction request reconstructed from the wrong epoch"
    )
    store.close()
