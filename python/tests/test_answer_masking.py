"""A registered secret in an ask_user answer is masked in the durable row.

The invariant across this package is that what lands *durably* is masked --
the transcript, the event log, the trajectory all strip a registered
credential before it reaches disk. Round 102 added the approvals table's
`answer` column (the human's reply to `ask_user`) and it was the one durable
sink that missed the rule: the transcript masked the answer at the tool
boundary, but the row kept it raw.

Measured before the fix: a human answering with a registered key left
`sk-...` in the approvals table while every other sink had it as
`<secret-hidden>`. The broker now masks the answer before the row is written,
using the secrets registry the manager late-binds onto it -- the same
late-binding as `store`.
"""

import asyncio
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.secrets import SecretRegistry
from mini_loop.storage import SQLiteStateStore

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"
SECRET = "sk-SUPER-SECRET-abcdef123456"


def _manager(tmp_path, *, with_secret=True):
    secrets = SecretRegistry()
    if with_secret:
        secrets.register("API_KEY", SECRET)
    store = SQLiteStateStore(tmp_path / "s.db")
    client = FakeAsyncAnthropic(responder=scripted([
        ([tool("ask_user", question="deploy key?", _id="q1")], "tool_use"),
        ([text("done")], "end_turn"),
    ]))
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS),
        client, tool_registry=full_registry(), state_store=store, secrets=secrets,
    )
    return manager, store


async def _ask_and_answer(manager, session, answer):
    turn = asyncio.create_task(session.run("ask"))
    for _ in range(200):
        if manager.approvals.list(session.id):
            break
        await asyncio.sleep(0.01)
    pending = manager.approvals.list(session.id)[0]
    manager.approvals.resolve(pending["approval_id"], session_id=session.id,
                              allowed=True, answer=answer)
    await turn


@pytest.mark.asyncio
async def test_a_secret_in_the_answer_is_masked_in_the_row(tmp_path):
    manager, store = _manager(tmp_path)
    session = manager.create()

    await _ask_and_answer(manager, session, f"the key is {SECRET}")

    row = store.read_approvals(session.id)[0]
    assert SECRET not in row["answer"], "the raw secret reached the durable row"
    assert "<secret-hidden>" in row["answer"]
    # The rest of the answer survives -- masking, not deletion.
    assert row["answer"].startswith("the key is ")
    store.close()


@pytest.mark.asyncio
async def test_the_answer_is_masked_everywhere_consistently(tmp_path):
    manager, store = _manager(tmp_path)
    session = manager.create()

    await _ask_and_answer(manager, session, f"use {SECRET} to deploy")

    # The durable row and the durable transcript agree: neither holds the raw
    # secret. Before the fix these two disagreed.
    row_answer = store.read_approvals(session.id)[0]["answer"]
    transcript = str(store.load_messages(session.id))
    assert SECRET not in row_answer
    assert SECRET not in transcript
    store.close()


@pytest.mark.asyncio
async def test_an_ordinary_answer_is_stored_verbatim(tmp_path):
    """Not a wall: an answer with no secret is untouched."""

    manager, store = _manager(tmp_path)
    session = manager.create()

    await _ask_and_answer(manager, session, "just use staging")

    assert store.read_approvals(session.id)[0]["answer"] == "just use staging"
    store.close()
