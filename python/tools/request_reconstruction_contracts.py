"""Actual SQLite and model observations of Source request reconstruction."""
from __future__ import annotations

import asyncio
import copy
import tempfile
from pathlib import Path


def request_reconstruction_contracts() -> dict:
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text
    from mini_loop.manager import SessionManager
    from mini_loop.session_query import reconstruct_request
    from mini_loop.storage import NullStateStore, SQLiteStateStore

    projections = [
        dict(seq=1, type="tool_catalog", fingerprint="tools", schemas=[dict(name="read", input_schema={"type": "object"})]),
        dict(seq=2, type="system_prompt", hash="system", text="original system"),
        dict(seq=3, type="capability_plan", fingerprint="caps", permission_mode="readonly",
             sandbox="NullSandbox", sandbox_confined=False, catalog_fingerprint="tools"),
    ]
    start = dict(seq=10, type="model_start", transcript_epoch=1, message_count=2,
        tool_catalog_fingerprint="tools", system_hash="system", capability_fingerprint="caps",
        model="source-model", max_tokens=123)
    messages = [dict(role="user", content="first"), dict(role="assistant", content="second"),
                dict(role="user", content="third")]
    cases = [
        ("missing", [], 10),
        ("wrong-kind", [dict(seq=10, type="model_end")], 10),
        ("legacy", [dict(seq=10, type="model_start")], 10),
        ("null-epoch-before-bad-count", [dict(seq=10, type="model_start", transcript_epoch=None, message_count="bad")], 10),
        ("joined", projections + [start], 10),
        ("latest-preceding", projections + [
            dict(seq=4, type="tool_catalog", fingerprint="tools", schemas=[]),
            dict(seq=5, type="system_prompt", hash="system", text="latest system"),
            dict(seq=6, type="capability_plan", fingerprint="caps", permission_mode="auto"),
            start,
            dict(seq=11, type="system_prompt", hash="system", text="too late"),
        ], 10),
        ("unmatched", projections + [dict(start, tool_catalog_fingerprint="absent", system_hash="absent", capability_fingerprint="absent")], 10),
        ("missing-references-match-null", [dict(seq=1, type="tool_catalog", schemas=False),
            dict(seq=2, type="system_prompt", text=[dict(type="text", text="blocks")]),
            dict(seq=3, type="capability_plan"), dict(seq=10, type="model_start", transcript_epoch=1)], 10),
        ("physical-order-stop", [dict(seq=11, type="status"), *projections, start], 10),
        ("first-duplicate-start", [*projections, dict(start, message_count=1), dict(seq=10, type="model_start")], 10),
        ("old-epoch", [*projections, start], 10),
        ("missing-epoch-rows", [*projections, dict(start, transcript_epoch=3)], 10),
        ("historical-output-scalars", [*projections, dict(start, model=["archive"], max_tokens=False)], 10),
        ("missing-sequence-does-not-match-zero", [dict(type="model_start", transcript_epoch=1)], 0),
        ("missing-predecessor-sequence-defaults-zero", [dict(type="system_prompt", hash="system", text="undated"), start], 10),
        ("null-predecessor-sequence", [dict(seq=None, type="status"), start], 10),
        ("string-predecessor-sequence", [dict(seq="1", type="status"), start], 10),
        ("structural-fingerprint-equality", [dict(seq=1, type="tool_catalog", fingerprint={"x": [1.0]}, schemas=False),
            dict(start, tool_catalog_fingerprint={"x": [1]})], 10),
    ]
    for label, count in [
        ("zero", 0), ("negative", -1), ("very-negative", -999), ("larger", 999),
        ("large-integer", 10**100), ("decimal-string", " ٢_٠ "), ("float", 1.9),
        ("true", True), ("false", False), ("null", None), ("empty-list", []),
        ("bad-string", "not an integer"), ("nonempty-list", [1]), ("nonempty-object", {"x": 1}),
    ]:
        cases.append(("count-" + label, [*projections, dict(start, message_count=count)], 10))

    rows = []
    with tempfile.TemporaryDirectory() as directory:
        store = SQLiteStateStore(Path(directory) / "state.db")
        try:
            for name, events, seq in cases:
                store.append_messages(name, messages, epoch=1)
                store.append_messages(name, [dict(role="user", content="new epoch")], epoch=4)
                for event in events:
                    store.append_event(name, copy.deepcopy(event))
                try:
                    output = reconstruct_request(store, name, seq)
                    error = None
                except Exception as exc:
                    output, error = None, type(exc).__name__
                rows.append(dict(name=name, seq=seq, events=store.load_events(name),
                    epochs=[dict(epoch=epoch, messages=store.load_messages(name, epoch=epoch)) for epoch in (1, 4)],
                    output=output, error=error))
        finally:
            store.close()

    async def runtime():
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            store = SQLiteStateStore(root / "state.db")
            settings = Settings(fake_llm=True, workspace_root=root, skills_dir=root / "skills", trajectory_enabled=False)
            client = FakeAsyncAnthropic(responder=lambda request: ([text("done")], "end_turn"), thinking=False)
            manager = SessionManager(settings, client, state_store=store,
                system_builder=lambda agent: "fixed reconstruction system", injectors=[])
            try:
                await manager.start()
                session = manager.create(owner="owner", permission_mode="readonly")
                await session.run("original prompt")
                event = next(event for event in store.load_events(session.id) if event.get("type") == "model_start")
                before = reconstruct_request(store, session.id, event["seq"])
                session.agent.messages[:] = [dict(role="user", content="summary")]
                await session.run("later prompt")
                after = reconstruct_request(store, session.id, event["seq"])
                assert before == after
                # Project only reconstruction inputs, excluding nondeterministic
                # event IDs/timestamps while retaining their original load order.
                fields = {"seq", "type", "transcript_epoch", "message_count", "tool_catalog_fingerprint",
                    "system_hash", "capability_fingerprint", "model", "max_tokens", "fingerprint", "schemas",
                    "hash", "text", "permission_mode", "sandbox", "sandbox_confined", "catalog_fingerprint"}
                events = [{key: value for key, value in row.items() if key in fields}
                    for row in store.load_events(session.id)]
                return dict(seq=event["seq"], events=events,
                    epochs=[dict(epoch=epoch, messages=store.load_messages(session.id, epoch=epoch)) for epoch in (1, 2)],
                    before=before, after=after)
            finally:
                await manager.stop()
                store.close()

    return dict(rows=rows, null_output=reconstruct_request(NullStateStore(), "missing", 10), runtime=asyncio.run(runtime()))
