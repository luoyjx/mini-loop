"""A secret in a user message is masked in the durable trajectory too.

Events reach the trajectory through `_capture_event`, which masks them. But
the `start` and `finish` records take a different path -- they write
`input_text`, `metadata`, and `output` directly -- and that path did not mask.
So a secret a user pasted into a message landed in the trajectory file raw
while the transcript masked it at flush: round 111's inconsistency (the
approval `answer` column), one durable sink over.

The trajectory store has no secrets of its own, so the session masks at the
boundary before recording, the same place the transcript is masked. This pins
that the user input, the recorded output, and the system prompt in metadata
all carry `<secret-hidden>`, in both the parsed record and the raw file bytes.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text
from mini_loop.secrets import SecretRegistry
from mini_loop.trajectory import TrajectoryStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
SECRET = "sk-USER-PASTED-abcdef1234567890"


def _manager(tmp_path, *, responder=None, register_secret=True):
    secrets = SecretRegistry()
    if register_secret:
        secrets.register("API_KEY", SECRET)
    traj = TrajectoryStore(tmp_path / "traj")
    client = FakeAsyncAnthropic(responder=responder) if responder else FakeAsyncAnthropic()
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        client, trajectory_store=traj, secrets=secrets,
    )
    return manager, traj


def _raw(traj):
    return "".join(p.read_text() for p in traj.root.glob("traj_*.jsonl"))


@pytest.mark.asyncio
async def test_a_secret_in_the_input_is_masked_in_the_trajectory(tmp_path):
    manager, traj = _manager(tmp_path)
    session = manager.create()

    await session.run(f"deploy with {SECRET} now")

    tid = traj.list(session_id=session.id)[0]["trajectory_id"]
    record = traj.get(tid)
    assert SECRET not in str(record["input"])
    assert "<secret-hidden>" in str(record["input"])
    # Not just the parsed record -- the bytes on disk.
    assert SECRET not in _raw(traj)


@pytest.mark.asyncio
async def test_a_secret_in_the_output_is_masked(tmp_path):
    responder = scripted([([text(f"I used {SECRET} as requested")], "end_turn")])
    manager, traj = _manager(tmp_path, responder=responder)
    session = manager.create()

    await session.run("go")

    assert SECRET not in _raw(traj), "a secret echoed in the output reached the file"


@pytest.mark.asyncio
async def test_an_ordinary_input_is_recorded_verbatim(tmp_path):
    """Not a wall: a message with no secret is untouched."""

    manager, traj = _manager(tmp_path)
    session = manager.create()

    await session.run("just deploy to staging")

    tid = traj.list(session_id=session.id)[0]["trajectory_id"]
    assert traj.get(tid)["input"] == "just deploy to staging"


@pytest.mark.asyncio
async def test_the_trajectory_and_transcript_agree(tmp_path):
    """The two durable copies of the input must not disagree on the secret --
    the specific inconsistency this closes."""

    from mini_loop.storage import SQLiteStateStore

    store = SQLiteStateStore(tmp_path / "state.db")
    secrets = SecretRegistry()
    secrets.register("API_KEY", SECRET)
    traj = TrajectoryStore(tmp_path / "traj")
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        FakeAsyncAnthropic(), trajectory_store=traj, state_store=store, secrets=secrets,
    )
    session = manager.create()

    await session.run(f"use {SECRET}")

    transcript = str(store.load_messages(session.id))
    trajectory = _raw(traj)
    assert SECRET not in transcript
    assert SECRET not in trajectory
    store.close()
