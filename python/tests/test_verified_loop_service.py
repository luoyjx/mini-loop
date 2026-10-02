"""The minimal coordinator: unverified never completes, end to end.

Default off -- these tests construct the service explicitly; nothing in
the default assembly does. The executor does real (fake-driven) work
through the ordinary subagent path; the deterministic auditor runs the
acceptance command through the sandboxed toolset and its exit code is the
only thing that can verify the requirement.
"""

import asyncio
import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.verified_loop_service import VerifiedLoopService

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _session(tmp_path, responder):
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(responder=responder),
        tool_registry=full_registry(),
    )
    return manager.create()


def test_a_task_completes_only_through_the_passing_command(tmp_path):
    executor = scripted([
        ([text("creating the artifact"),
          tool("write_file", _id="w1", path="artifact.txt", content="done")],
         "tool_use"),
        ([text("artifact written")], "end_turn"),
    ])
    session = _session(tmp_path, executor)
    outcome = asyncio.run(VerifiedLoopService(session).run_task(
        "create artifact.txt",
        acceptance_command="test -f artifact.txt",
    ))
    assert outcome["status"] == "complete"
    assert outcome["rounds"] == 1
    assert outcome["checkpoint"].status_of("acceptance") == "verified"
    assert outcome["receipts"][0].verdict == "complete"


def test_a_failing_command_feeds_the_next_round(tmp_path):
    # Round 1's executor writes the wrong file; round 2 fixes it.
    executor = scripted([
        ([text("writing"), tool("write_file", _id="w1", path="wrong.txt",
                                content="oops")], "tool_use"),
        ([text("done, I think")], "end_turn"),
        ([text("fixing"), tool("write_file", _id="w2", path="right.txt",
                               content="fixed")], "tool_use"),
        ([text("fixed it")], "end_turn"),
    ])
    session = _session(tmp_path, executor)
    queue = session.subscribe()
    outcome = asyncio.run(VerifiedLoopService(session).run_task(
        "create right.txt",
        acceptance_command="test -f right.txt",
        max_rounds=3,
    ))
    assert outcome["status"] == "complete"
    assert outcome["rounds"] == 2
    # The second round's objective carries the evidence, not just a retry.
    rounds = []
    while not queue.empty():
        event = queue.get_nowait()
        if event["type"] == "verified_round":
            rounds.append(event["objective"])
    assert "verification failed" in rounds[1], (
        "round 2 got no evidence-backed feedback"
    )
    assert outcome["receipts"][0].verdict == "incomplete"
    assert outcome["receipts"][1].verdict == "complete"


def test_unverified_never_reads_as_complete(tmp_path):
    def never_succeeds(kwargs):
        return [text("claiming success loudly: the task is COMPLETE")], "end_turn"

    session = _session(tmp_path, never_succeeds)
    outcome = asyncio.run(VerifiedLoopService(session).run_task(
        "produce nothing-that-passes",
        acceptance_command="test -f never-created.txt",
        max_rounds=2,
    ))
    assert outcome["status"] == "unverified"
    assert outcome["checkpoint"].status_of("acceptance") == "pending"
    assert outcome["summary"].startswith("[stopped after 2 rounds"), (
        "the stop must lead; a confident executor claim must not"
    )
    assert all(r.verdict == "incomplete" for r in outcome["receipts"])


def test_the_rounds_are_observable_in_the_session_stream(tmp_path):
    executor = scripted([
        ([text("work"), tool("bash", _id="b1", command="touch made.txt")],
         "tool_use"),
        ([text("made")], "end_turn"),
    ])
    session = _session(tmp_path, executor)
    queue = session.subscribe()
    asyncio.run(VerifiedLoopService(session).run_task(
        "make made.txt", acceptance_command="test -f made.txt",
    ))
    kinds = []
    while not queue.empty():
        kinds.append(queue.get_nowait()["type"])
    assert "verified_round" in kinds
    assert "verified_receipt" in kinds
    assert "verified_checkpoint" in kinds


import os as _os

import pytest as _pytest


@_pytest.mark.skipif(
    not _os.getenv("MINILOOP_REAL_PROVIDER_TESTS"),
    reason="real-model verified-loop validation is operator-gated",
)
def test_a_real_model_drives_the_loop_to_verified(tmp_path):
    """The offline tests prove the coordinator's SAFETY (unverified never
    completes); only a real executor proves it CONVERGES. A live model
    gets a file task and a deterministic acceptance command; the loop must
    reach verified through the command's exit code, not the model's word.
    """
    from mini_loop.config import build_client

    settings = Settings(workspace_root=tmp_path / "ws", skills_dir=SKILLS,
                        spill_dir=None, subagent_max_rounds=6)
    manager = SessionManager(settings, build_client(settings),
                             tool_registry=full_registry())
    session = manager.create()
    outcome = asyncio.run(VerifiedLoopService(session).run_task(
        "Create a file named report.txt in the workspace whose contents are "
        "exactly the word DONE.",
        acceptance_command="test \"$(cat report.txt)\" = DONE",
        max_rounds=4,
    ))
    assert outcome["status"] == "complete", (
        f"the live loop did not converge: {outcome['status']} after "
        f"{outcome['rounds']} rounds"
    )
    assert outcome["checkpoint"].status_of("acceptance") == "verified"
    # The verification is the command's, not the model's claim: the file
    # is really there with the right contents.
    assert (session.agent.workspace / "report.txt").read_text().strip() == "DONE"
