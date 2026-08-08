"""Concurrency and loop bounds are validated, because < 1 fails silently.

`max_concurrent_tools` was validated (`< 1` -> ValueError); three siblings
with the same class of failure were not:

* `max_concurrent_llm`  -- `Semaphore(0)` is never acquirable, so 0 does not
  slow the agent, it hangs it forever on the first model call, with no error.
  Measured: `session.run` never returns.
* `max_turns`           -- `for _ in range(0)` runs the loop body never, so
  the agent returns having done nothing.
* `subagent_max_rounds` -- the same no-op for a spawned subagent.

The budgets and timeouts have the same class of failure and were left unchecked:

* `max_tokens`        -- the provider rejects a request for zero tokens, so
  every model call fails.
* `token_threshold`   -- `context_used() > 0` is always true, so compaction
  fires every turn and summarizes the transcript away before the agent uses it.
* `bash_timeout`      -- `communicate(timeout=0)` times out immediately, so
  every shell command returns "Timeout". Measured: `run_bash("echo hi")` fails.
* `approval_timeout`  -- `wait_for(future, 0)` denies before anyone can answer.
* `team_idle_poll`    -- `sleep(0)` busy-spins the idle loop.
* `team_idle_timeout` -- a teammate shuts down before doing any work.

A settings object that hangs or silently no-ops the agent should fail loudly at
construction, not at runtime. These pin that it does, and that a valid config
is untouched.
"""

import pathlib

import pytest

from mini_loop.config import Settings

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _settings(tmp_path, **over):
    return Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                    skills_dir=SKILLS, **over)


@pytest.mark.parametrize("field", [
    "max_concurrent_llm",
    "max_concurrent_tools",
    "max_turns",
    "subagent_max_rounds",
    # The budgets and timeouts, same failure mode, previously unchecked.
    "max_tokens",
    "token_threshold",
    "bash_timeout",
    "approval_timeout",
    "team_idle_poll",
    "team_idle_timeout",
])
@pytest.mark.parametrize("bad", [0, -1])
def test_a_sub_one_bound_is_rejected(tmp_path, field, bad):
    with pytest.raises(ValueError, match=field):
        _settings(tmp_path, **{field: bad})


def test_a_zero_bash_timeout_would_time_out_every_command(tmp_path):
    """The concrete failure the validation prevents: with `bash_timeout=0`, if it
    were allowed, `run_bash` returns a timeout for a command that does nothing.
    Pinned by constructing the toolset the way `bash_timeout=1` would and
    confirming the boundary value works, since 0 can no longer be built."""
    from mini_loop.tools import Toolset

    settings = _settings(tmp_path, bash_timeout=1)
    toolset = Toolset(settings.workspace_root, bash_timeout=settings.bash_timeout)
    assert "hi" in toolset.run_bash("echo hi")


def test_a_valid_config_is_accepted(tmp_path):
    """Not a wall: ordinary values construct fine."""

    settings = _settings(tmp_path, max_concurrent_llm=4, max_turns=20,
                         subagent_max_rounds=10)
    assert settings.max_concurrent_llm == 4
    assert settings.max_turns == 20


def test_the_default_config_is_valid(tmp_path):
    """The defaults must satisfy their own validation."""

    settings = _settings(tmp_path)
    assert settings.max_concurrent_llm >= 1
    assert settings.max_turns >= 1
    assert settings.subagent_max_rounds >= 1


def test_min_one_is_the_boundary(tmp_path):
    """1 is allowed -- the rule is < 1, not <= 1."""

    settings = _settings(tmp_path, max_concurrent_llm=1, max_turns=1,
                         subagent_max_rounds=1)
    assert settings.max_concurrent_llm == 1
