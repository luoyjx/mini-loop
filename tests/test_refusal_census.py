"""Census: what follows a workspace-fence refusal (§5, 2026-09-02, round 6).

The corpus's 75 read_file refusals were followed by a bash command every
time: 29 read the very file just refused, 46 went on with the shell
elsewhere, none recovered through read_file, none gave up. A refusal
costs a round; under a Null sandbox it prevents nothing. The profile
puts that in the report, per build, where the operator weighing
workspace binding can read it -- and where binding's effect (refusals
going to zero) will show.
"""

from mini_loop.mining import refusal_profile, render_refusals
from mini_loop.trajectory import TrajectoryStore

REFUSAL = ("Error: Path escapes workspace: /repo/README.md. Paths are relative "
           "to the workspace root; an absolute path only works when it points "
           "inside the workspace.")
LEGACY_REFUSAL = "Error: Path escapes workspace: /repo/setup.py"


def _use(store, tid, name, **inputs):
    store.append(tid, {"type": "tool_use", "name": name, "input": inputs, "id": "c"})


def _result(store, tid, name, output):
    store.append(tid, {"type": "tool_result", "name": name, "output": output, "id": "c"})


def test_every_outcome_after_a_refusal_is_named(tmp_path):
    store = TrajectoryStore(tmp_path / "t")
    tid = store.start(session_id="s", run_index=1, input_text="x",
                      metadata={"workspace": "/ws"})
    # 1. refused, then bash reads the same file: the fence was bypassed.
    _use(store, tid, "read_file", path="/repo/README.md")
    _result(store, tid, "read_file", REFUSAL)
    _use(store, tid, "bash", command="cd /repo && head -50 README.md")
    _result(store, tid, "bash", "text")
    # 2. refused (legacy message shape), then bash does something else.
    _use(store, tid, "read_file", path="/repo/setup.py")
    _result(store, tid, "read_file", LEGACY_REFUSAL)
    _use(store, tid, "bash", command="ls -la")
    _result(store, tid, "bash", "files")
    # 3. refused, then read_file with a relative path: recovered.
    _use(store, tid, "read_file", path="/ws/notes.txt")
    _result(store, tid, "read_file", REFUSAL)
    _use(store, tid, "read_file", path="notes.txt")
    _result(store, tid, "read_file", "notes")
    # 4. refused, then the same mistake again.
    _use(store, tid, "read_file", path="/repo/a")
    _result(store, tid, "read_file", REFUSAL)
    _use(store, tid, "read_file", path="/repo/b")
    _result(store, tid, "read_file", REFUSAL)
    # 5. ...and that last refusal ended the turn.
    store.finish(tid, status="completed", duration_ms=1.0)

    profile = refusal_profile(store)
    assert profile["refusals"] == 5
    assert profile["outcomes"] == {
        "bash reads the same path (fence bypassed)": 1,
        "bash elsewhere": 1,
        "read_file relative (recovered)": 1,
        "read_file absolute again": 1,
        "turn ended": 1,
    }
    assert profile["recovered"] == 1 and profile["bypassed"] == 1

    text = render_refusals(profile)
    assert "5 refusals" in text
    assert "recovered via read_file: 1 | same file read through bash: 1" in text
    assert "- turn ended: 1" in text


def test_a_window_without_refusals_says_so(tmp_path):
    store = TrajectoryStore(tmp_path / "t")
    tid = store.start(session_id="s", run_index=1, input_text="x")
    _use(store, tid, "bash", command="ls")
    _result(store, tid, "bash", "ok")
    store.finish(tid, status="completed", duration_ms=1.0)
    profile = refusal_profile(store)
    assert profile == {"refusals": 0, "outcomes": {}, "recovered": 0, "bypassed": 0}
    assert render_refusals(profile).endswith("no refusals in the window")
