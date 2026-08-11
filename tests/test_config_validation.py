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


@pytest.mark.parametrize(
    "field",
    [
        "token_efficiency_raw_min_bytes",
        "token_efficiency_artifact_ttl_seconds",
        "token_efficiency_max_artifact_bytes",
        "token_efficiency_max_total_bytes",
        "ast_outline_timeout",
        "ast_outline_max_output_bytes",
    ],
)
@pytest.mark.parametrize("bad", [0, -1])
def test_token_efficiency_and_ast_positive_bounds_are_validated(
    tmp_path, field, bad
):
    with pytest.raises(ValueError, match=field):
        _settings(tmp_path, **{field: bad})


@pytest.mark.parametrize(
    ("field", "bad"),
    [
        ("token_efficiency_mode", "auto"),
        ("token_efficiency_response_style", "terse"),
        ("ast_outline_binary", ""),
    ],
)
def test_token_efficiency_and_ast_enums_or_binary_are_validated(
    tmp_path, field, bad
):
    with pytest.raises(ValueError, match=field):
        _settings(tmp_path, **{field: bad})


def test_artifact_limit_cannot_exceed_total_capacity(tmp_path):
    with pytest.raises(ValueError, match="must not exceed"):
        _settings(
            tmp_path,
            token_efficiency_max_artifact_bytes=101,
            token_efficiency_max_total_bytes=100,
        )


def test_raw_persistence_threshold_cannot_exceed_artifact_limit(tmp_path):
    with pytest.raises(ValueError, match="raw_min_bytes must not exceed"):
        _settings(
            tmp_path,
            token_efficiency_raw_min_bytes=101,
            token_efficiency_max_artifact_bytes=100,
        )


def test_token_efficiency_and_ast_defaults_are_safe(tmp_path):
    settings = _settings(tmp_path)

    assert settings.token_efficiency_mode == "off"
    assert settings.token_efficiency_response_style == "normal"
    assert settings.token_efficiency_persist_raw is True
    assert settings.ast_outline_enabled is False
    assert settings.ast_outline_binary == "ast-outline"
    assert settings.ast_outline_sha256 is None


def test_token_efficiency_and_ast_settings_are_env_backed(tmp_path, monkeypatch):
    monkeypatch.setenv("MINILOOP_TOKEN_EFFICIENCY_MODE", "SHADOW")
    monkeypatch.setenv("MINILOOP_TOKEN_EFFICIENCY_RESPONSE_STYLE", "CONCISE")
    monkeypatch.setenv("MINILOOP_TOKEN_EFFICIENCY_PERSIST_RAW", "false")
    monkeypatch.setenv("MINILOOP_TOKEN_EFFICIENCY_RAW_MIN_BYTES", "123")
    monkeypatch.setenv(
        "MINILOOP_TOKEN_EFFICIENCY_ARTIFACT_TTL_SECONDS", "45.5"
    )
    monkeypatch.setenv("MINILOOP_TOKEN_EFFICIENCY_MAX_ARTIFACT_BYTES", "456")
    monkeypatch.setenv("MINILOOP_TOKEN_EFFICIENCY_MAX_TOTAL_BYTES", "789")
    monkeypatch.setenv("MINILOOP_AST_OUTLINE_ENABLED", "true")
    monkeypatch.setenv("MINILOOP_AST_OUTLINE_BINARY", "/opt/ast-outline")
    monkeypatch.setenv("MINILOOP_AST_OUTLINE_SHA256", "a" * 64)
    monkeypatch.setenv("MINILOOP_AST_OUTLINE_TIMEOUT", "3.5")
    monkeypatch.setenv("MINILOOP_AST_OUTLINE_MAX_OUTPUT_BYTES", "987")

    settings = _settings(tmp_path)

    assert settings.token_efficiency_mode == "shadow"
    assert settings.token_efficiency_response_style == "concise"
    assert settings.token_efficiency_persist_raw is False
    assert settings.token_efficiency_raw_min_bytes == 123
    assert settings.token_efficiency_artifact_ttl_seconds == 45.5
    assert settings.token_efficiency_max_artifact_bytes == 456
    assert settings.token_efficiency_max_total_bytes == 789
    assert settings.ast_outline_enabled is True
    assert settings.ast_outline_binary == "/opt/ast-outline"
    assert settings.ast_outline_sha256 == "a" * 64
    assert settings.ast_outline_timeout == 3.5
    assert settings.ast_outline_max_output_bytes == 987


def test_enabled_ast_outline_requires_absolute_digest_pinned_binary(tmp_path):
    with pytest.raises(ValueError, match="absolute binary path"):
        _settings(tmp_path, ast_outline_enabled=True)
    with pytest.raises(ValueError, match="requires ast_outline_sha256"):
        _settings(
            tmp_path,
            ast_outline_enabled=True,
            ast_outline_binary="/opt/ast-outline",
        )
    with pytest.raises(ValueError, match="64 lowercase hex"):
        _settings(tmp_path, ast_outline_sha256="not-a-digest")
