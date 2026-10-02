"""Self-improvement grows memory and a self-weakening tell -- not autonomy.

Three mechanisms from the RSI literature (docs/RSI_RESEARCH_AND_PLAN.md),
each stopping where the house rules stop:

* the ARCHIVE (Darwin Gödel Machine): every proposal -- verified or not --
  lands in a durable lineage with its parent, so attempts form a
  population instead of independent hill-climbs. Loose admission, strict
  promotion: admission is a JSONL row, promotion is still a human merge.
* the VERIFIER-TOUCH flag (the DGM "removed the detection markers and
  scored 2.0/2.0" case): a proposal that changes the acceptance
  instruments is NAMED, never silently passed -- and never blocked,
  because sometimes the instruments are the objective. The human judges.
* SUGGESTED objectives (self-diagnosis half of the loop): problem ledgers
  become reviewable objective strings; suggestion is not authorization.
"""

import asyncio
import pathlib

from mini_loop import SessionManager, Settings
from mini_loop.builtins import full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, text, tool
from mini_loop.improvement_archive import ImprovementArchive
from mini_loop.self_improve import propose_improvement, verifier_touches

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


# -- verifier_touches --------------------------------------------------------


def test_the_acceptance_instruments_are_recognized():
    touched = verifier_touches([
        "mini_loop/agent.py",
        "tools/verify_guards.py",
        ".github/workflows/ci.yml",
        "tests/conftest.py",
        "docs/README.md",
    ])
    assert touched == ["tools/verify_guards.py", ".github/workflows/ci.yml",
                       "tests/conftest.py"]
    assert verifier_touches([]) == []


# -- the archive -------------------------------------------------------------


def test_the_archive_keeps_lineage_and_scopes_by_owner(tmp_path):
    archive = ImprovementArchive(tmp_path / ".improvements")
    first = archive.record({"objective": "a", "verified": False,
                            "branch": "p/1"}, owner="alice")
    second = archive.record({"objective": "b", "verified": True,
                             "branch": "p/2"}, owner="alice", parent_id=first)
    archive.record({"objective": "c", "verified": True, "branch": "p/3"},
                   owner="bob")

    rows = archive.list(owner="alice")
    assert [r["proposal_id"] for r in rows] == [second, first], "newest first"
    assert rows[0]["parent_id"] == first, "lineage lost"
    assert rows[1]["verified"] is False, (
        "unverified attempts belong in the archive too: loose admission, "
        "strict promotion"
    )
    assert all(r["owner"] == "alice" for r in rows)
    assert len(archive.list()) == 3


def test_a_corrupt_row_is_skipped_not_fatal(tmp_path):
    archive = ImprovementArchive(tmp_path / ".improvements")
    archive.record({"objective": "a"}, owner="x")
    with open(archive.path, "a") as handle:
        handle.write("not json\n")
    archive.record({"objective": "b"}, owner="x")
    assert [r["objective"] for r in archive.list()] == ["b", "a"]


# -- propose_improvement wiring ----------------------------------------------


def _git(cwd, *args):
    import subprocess

    return subprocess.run(["git", *args], cwd=cwd, capture_output=True,
                          text=True, check=True)


def _repo_workspace_session(tmp_path, responder):
    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(responder=responder),
        tool_registry=full_registry(),
    )
    session = manager.create()
    ws = session.workspace
    _git(ws, "init", "-b", "main")
    _git(ws, "config", "user.email", "t@t")
    _git(ws, "config", "user.name", "t")
    (ws / "README.md").write_text("baseline\n")
    _git(ws, "add", "-A")
    _git(ws, "commit", "-m", "baseline")
    _git(ws, "checkout", "-b", "proposal/x")
    return manager, session


def test_mid_loop_tampering_cannot_verify(tmp_path):
    """The DGM incident, closed at the judgment window: the executor edits
    an acceptance instrument during the round, the acceptance command still
    exits 0 -- and the round does NOT verify, because the auditor that
    judged it is not the auditor the task started with. Weaken-then-restore
    is caught the same way: the probe samples before each acceptance run,
    not just at the end."""

    executor = scripted([
        ([text("weakening the guard runner"),
          tool("write_file", _id="w1", path="tools/verify_guards.py",
               content="# gutted\n")],
         "tool_use"),
        ([text("done")], "end_turn"),
    ])
    manager, session = _repo_workspace_session(tmp_path, executor)
    # The instrument exists at baseline; the executor rewrites it mid-round.
    (session.workspace / "tools").mkdir()
    (session.workspace / "tools" / "verify_guards.py").write_text("# real\n")
    _git(session.workspace, "add", "-A")
    _git(session.workspace, "commit", "-m", "instruments")

    proposal = asyncio.run(propose_improvement(
        session, "improve things", acceptance_command="true", max_rounds=1,
        archive=manager.improvements, owner="alice",
    ))

    assert proposal["verified"] is False, (
        "a green exit code from a changed auditor verified the proposal"
    )
    assert proposal["integrity"] == "suspect"
    assert "instruments changed" in proposal["summary"]
    [row] = manager.improvements.list(owner="alice")
    assert row["integrity"] == "suspect"


def test_a_verifier_touching_proposal_is_named_and_archived(tmp_path):
    executor = scripted([
        ([text("adjusting the guard runner"),
          tool("write_file", _id="w1", path="tools/verify_guards.py",
               content="# weakened\n")],
         "tool_use"),
        ([text("done")], "end_turn"),
    ])
    manager, session = _repo_workspace_session(tmp_path, executor)

    proposal = asyncio.run(propose_improvement(
        session, "improve the guard runner",
        acceptance_command="true",
        archive=manager.improvements, owner="alice",
    ))

    assert proposal["touches_verifiers"] == ["tools/verify_guards.py"]
    assert "CHANGES THE ACCEPTANCE INSTRUMENTS" in proposal["next"]
    assert proposal["proposal_id"].startswith("imp_")

    [row] = manager.improvements.list(owner="alice")
    assert row["proposal_id"] == proposal["proposal_id"]
    assert row["touches_verifiers"] == ["tools/verify_guards.py"]


def test_a_plain_proposal_carries_no_flag_and_links_its_parent(tmp_path):
    executor = scripted([
        ([text("improving"),
          tool("write_file", _id="w1", path="improved.txt", content="x")],
         "tool_use"),
        ([text("done")], "end_turn"),
    ])
    manager, session = _repo_workspace_session(tmp_path, executor)

    proposal = asyncio.run(propose_improvement(
        session, "add improved.txt", acceptance_command="test -f improved.txt",
        archive=manager.improvements, owner="alice", parent_id="imp_parent01",
    ))

    assert proposal["touches_verifiers"] == []
    assert "CHANGES THE ACCEPTANCE" not in proposal["next"]
    [row] = manager.improvements.list(owner="alice")
    assert row["parent_id"] == "imp_parent01"


# -- suggested objectives ----------------------------------------------------


def test_problem_ledgers_become_reviewable_objectives(tmp_path):
    from mini_loop.self_audit import suggest_objectives

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(),
    )
    assert suggest_objectives(manager) == []

    manager.approvals.problems.append("approval persistence failed (OSError)")
    suggestions = suggest_objectives(manager)
    assert suggestions, "a recorded problem produced no suggestion"
    assert any("approval persistence failed" in s["problem"]
               for s in suggestions)
    assert all("Find and eliminate" in s["objective"] for s in suggestions)


def test_problem_ledgers_hatch_bench_task_drafts_not_admissions(tmp_path):
    """The curation half of growing the benchmark set (§5): friction
    becomes a reviewable DRAFT. The judge-side boundary is structural --
    a draft ships no expect predicate, its name lives in a reserved
    namespace no admitted task uses, and calling the hatchery touches
    neither task set."""

    from mini_loop.benchmark import DEFAULT_TASKS, HELDOUT_TASKS
    from mini_loop.self_audit import DRAFT_TASK_PREFIX, suggest_bench_tasks

    manager = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=SKILLS, spill_dir=None),
        FakeAsyncAnthropic(),
    )
    assert suggest_bench_tasks(manager) == []

    admitted_before = [t.name for t in DEFAULT_TASKS + HELDOUT_TASKS]
    manager.approvals.problems.append("approval persistence failed (OSError)")
    drafts = suggest_bench_tasks(manager)
    assert drafts, "a recorded problem produced no draft"
    (draft,) = [d for d in drafts
                if "approval persistence failed" in d["problem"]]
    assert draft["name"].startswith(DRAFT_TASK_PREFIX)
    assert draft["expect"] is None, (
        "a draft with an expectation would be an admission the human "
        "never authored"
    )
    assert draft["problem"] in draft["prompt_draft"]
    assert "judge-side" in draft["note"]

    # Admission is a human edit to benchmark.py, never a side effect here.
    assert [t.name for t in DEFAULT_TASKS + HELDOUT_TASKS] == admitted_before
    assert not any(name.startswith(DRAFT_TASK_PREFIX)
                   for name in admitted_before), (
        "the draft namespace is reserved; an admitted task inside it "
        "blurs the draft/admitted line"
    )


def test_the_http_surface_scopes_like_self_audit(tmp_path):
    from fastapi.testclient import TestClient

    from mini_loop.auth import TokenAuth
    from mini_loop.server import create_app

    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS, spill_dir=None)
    manager = SessionManager(settings, FakeAsyncAnthropic())
    app = create_app(manager=manager, settings=settings)
    with TestClient(app) as client:
        app.state.auth = TokenAuth({"tok-alice": "alice", "tok-bob": "bob"})
        alice = {"Authorization": "Bearer tok-alice"}
        bob = {"Authorization": "Bearer tok-bob"}

        manager.improvements.record({"objective": "a", "verified": True},
                                    owner="alice")

        mine = client.get("/improvements", headers=alice).json()["proposals"]
        assert len(mine) == 1 and mine[0]["owner"] == "alice"
        assert client.get("/improvements", headers=bob).json()["proposals"] == []

        suggested = client.get("/self-audit/suggestions", headers=alice)
        assert suggested.status_code == 200
        assert "suggestions" in suggested.json()

        drafts = client.get("/self-audit/bench-task-drafts", headers=alice)
        assert drafts.status_code == 200
        assert "drafts" in drafts.json()
