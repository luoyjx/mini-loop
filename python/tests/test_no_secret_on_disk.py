"""No registered secret reaches any durable *recorded* sink, in raw bytes.

Rounds 111 and 115 each found one durable sink -- the approval `answer`
column, then the trajectory `input` field -- holding a registered secret raw
while every other sink masked it. Both slipped through because masking is
applied per field as each is written, and the write-site scan (round 82)
classifies *modules*, not fields: it passed both modules as RECORDED while a
field inside each leaked. Finding these one at a time is how the next one is
missed.

So this stops enumerating and sweeps. It drives a secret through every path
that reaches a recorded sink -- the user's message, a tool's output, an
`ask_user` answer, a saved memory -- runs a real session over a SQLite state
store, a trajectory store, and a memory store, then walks every byte of those
roots and asserts the raw secret appears nowhere. The workspace is *excluded*
on purpose: it is the EXECUTED half, where the agent's own work product may
legitimately contain a credential a command was given.

This would have caught 111 and 115, and it guards the central masking point
none of the per-field tests do: `Agent._exec_tool` masks a tool result once,
and that one call feeds the transcript, the events, the trajectory, and the
action journal. The mutation that removes it is caught here, in four sinks at
once.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.registry import Tool
from mini_loop.secrets import SecretRegistry
from mini_loop.storage import SQLiteStateStore
from mini_loop.trajectory import TrajectoryStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
SECRET = "sk-ON-DISK-SWEEP-0123456789abcdef"


def _leaky_tool():
    async def leaky(ctx):
        return f"the configured value is {SECRET}"

    return Tool("leaky", "echoes a secret", {"type": "object", "properties": {}},
                leaky, risk="read")


async def _drive(manager, session):
    """One turn that pushes the secret into every recorded sink, then answer
    the approval it parks on."""

    turn = asyncio.create_task(session.run(f"deploy using {SECRET}"))
    for _ in range(300):
        if manager.approvals.list(session.id):
            break
        await asyncio.sleep(0.01)
    if manager.approvals.list(session.id):
        manager.approvals.resolve(
            manager.approvals.list(session.id)[0]["approval_id"],
            session_id=session.id, allowed=True, answer=f"confirmed, key {SECRET}",
        )
    await turn
    await manager.stop()


def _recorded_files(tmp_path):
    """Every file a recorded sink writes -- including SQLite's `-wal` and
    `-shm` siblings, which hold rows not yet checkpointed into the main `.db`.

    Round 116 learned this the hard way: an earlier version of the sweep read
    only `state.db` and passed while the leaked secret sat in `state.db-wal`.
    A sweep that does not read every byte a store touches is a sweep with a
    blind spot exactly where the store is fastest to write."""

    files = []
    # state.db, state.db-wal, state.db-shm
    files += sorted(tmp_path.glob("state.db*"))
    for root in (tmp_path / "traj", tmp_path / "mem"):
        if root.exists():
            files += [p for p in root.rglob("*") if p.is_file()]
    return files


def _bytes_leaking(files):
    return [path.name for path in files if SECRET.encode() in path.read_bytes()]


async def _run(tmp_path, *, register_secret):
    secrets = SecretRegistry()
    if register_secret:
        secrets.register("API_KEY", SECRET)
    registry = full_registry()
    registry.register(_leaky_tool())
    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("leaky", _id="t1")], "tool_use"),
        ([tool("remember", name="deploy-key", type="reference",
               description="the deploy key", body=f"the key is {SECRET}",
               _id="t2")], "tool_use"),
        ([tool("ask_user", question="confirm the deploy?", _id="q1")], "tool_use"),
        ([text("done")], "end_turn"),
    ]))
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 memory_root=tmp_path / "mem", skills_dir=SKILLS),
        client, tool_registry=registry,
        state_store=SQLiteStateStore(tmp_path / "state.db"),
        trajectory_store=TrajectoryStore(tmp_path / "traj"),
        secrets=secrets,
    )
    await _drive(manager, manager.create())


@pytest.mark.asyncio
async def test_no_recorded_sink_leaks_a_secret(tmp_path):
    await _run(tmp_path, register_secret=True)

    leaks = _bytes_leaking(_recorded_files(tmp_path))

    assert leaks == [], (
        f"a registered secret reached recorded sink(s) in raw bytes: {leaks}"
    )


@pytest.mark.asyncio
async def test_the_sweep_detects_an_unmasked_secret(tmp_path):
    """Non-vacuity: with the value not registered as a secret, it is not
    masked, flows through, and the sweep must find it -- proving the sweep can
    see a raw secret in these files at all."""

    await _run(tmp_path, register_secret=False)

    leaks = _bytes_leaking(_recorded_files(tmp_path))

    assert leaks, (
        "the sweep found nothing even with an unmasked value on disk -- it is "
        "not actually reading the sinks"
    )
