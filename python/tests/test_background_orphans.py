"""Background work survives restarts as truth, if not as work (roadmap G7).

The transcript tells the model "Started background task bg_0001" durably;
the manager holding the outcome was process memory. After a restart the
fresh manager answered "Unknown: bg_0001", the drain never delivered, and
the command's process -- deliberately its own session -- may still be
running unsupervised. G7's third subsystem: cron and tasks got claim
protocols (rounds 236/237); background's gap is not duplicate claims (its
work is process-local) but a restart that erases the record.

One ledger file per in-flight command, removed on settle and on graceful
cancel. Whatever is left at construction is exactly the orphaned work,
adopted as terminal `orphaned` records through the normal settle path:
the existing drain/injection machinery delivers the news, check_background
answers, and nothing is silently dropped or silently re-run.
"""

import asyncio
import json
import pathlib

from mini_loop.background import BackgroundManager, background_injector


def _ws(tmp_path) -> pathlib.Path:
    ws = tmp_path / "ws"
    ws.mkdir(parents=True, exist_ok=True)
    return ws


def test_a_live_orphan_is_surfaced_with_its_pid(tmp_path):
    ws = _ws(tmp_path)

    async def scenario():
        first = BackgroundManager(ws)
        first.run("sleep 30")
        await asyncio.sleep(0.25)  # let the subprocess spawn and the pid land
        try:
            # A second manager on the same workspace IS the restarted process.
            second = BackgroundManager(ws)
            done = second.drain()
            assert len(done) == 1
            note = done[0]
            assert note["bg_id"] == "bg_0001"
            assert note["status"] == "orphaned"
            assert "still alive" in note["result"]
            assert "sleep 30" in note["result"]
            assert "orphaned" in second.check("bg_0001")
        finally:
            await first.close()

    asyncio.run(scenario())


def test_a_recordless_pid_reports_unknown_outcome(tmp_path):
    ws = _ws(tmp_path)
    ledger = ws / ".background"
    ledger.mkdir()
    # The crash-before-spawn window: a record with no pid.
    (ledger / "bg_0003.json").write_text(
        json.dumps({"bg_id": "bg_0003", "command": "make things", "pid": None})
    )

    manager = BackgroundManager(ws)
    done = manager.drain()

    assert len(done) == 1
    assert "whether it completed is unknown" in done[0]["result"]
    assert "make things" in done[0]["result"]


def test_adopted_ids_are_reserved_not_reissued(tmp_path):
    ws = _ws(tmp_path)
    ledger = ws / ".background"
    ledger.mkdir()
    (ledger / "bg_0002.json").write_text(
        json.dumps({"bg_id": "bg_0002", "command": "old work", "pid": None})
    )

    async def scenario():
        manager = BackgroundManager(ws)
        started = manager.run("echo fresh")
        assert "bg_0003" in started, (
            "a fresh counter reissued an adopted id and overwrote the orphan"
        )
        await manager.close()

    asyncio.run(scenario())


def test_settled_and_cancelled_work_leaves_no_ledger(tmp_path):
    ws = _ws(tmp_path)

    async def scenario():
        manager = BackgroundManager(ws)
        manager.run("echo done")
        for _ in range(40):
            await asyncio.sleep(0.05)
            if manager.drain():
                break
        else:
            raise AssertionError("the background echo never completed")
        manager.run("sleep 30")
        await asyncio.sleep(0.25)
        await manager.close()  # graceful cancel kills the group

    asyncio.run(scenario())
    leftover = list((ws / ".background").glob("*.json"))
    assert leftover == [], (
        f"settled/cancelled work left ledger records: {[p.name for p in leftover]}"
    )


def test_the_injector_surfaces_orphans_without_a_tool_call(tmp_path):
    ws = _ws(tmp_path)
    ledger = ws / ".background"
    ledger.mkdir()
    (ledger / "bg_0001.json").write_text(
        json.dumps({"bg_id": "bg_0001", "command": "deploy it", "pid": None})
    )

    class _Agent:
        workspace = ws
        state: dict = {}
        secrets = None
        sandbox = None
        events: list = []

        async def _send(self, event_type, **fields):
            self.events.append(event_type)

    agent = _Agent()
    injected = asyncio.run(background_injector(agent))

    assert injected, "orphaned work waited for a background tool call to surface"
    assert "task_notification" in injected[0]["content"]
    assert "deploy it" in injected[0]["content"]
    assert isinstance(agent.state.get("background"), BackgroundManager)


def test_the_injector_stays_lazy_with_no_ledger(tmp_path):
    ws = _ws(tmp_path)

    class _Agent:
        workspace = ws
        state: dict = {}
        secrets = None
        sandbox = None

        async def _send(self, event_type, **fields):
            raise AssertionError("nothing should be sent")

    agent = _Agent()
    assert asyncio.run(background_injector(agent)) == []
    assert "background" not in agent.state
