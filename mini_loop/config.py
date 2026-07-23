"""Runtime configuration and LLM-client construction.

Mirrors `learn-claude-code`'s `.env` contract (ANTHROPIC_API_KEY / MODEL_ID /
optional ANTHROPIC_BASE_URL) and adds the few knobs a concurrent server needs.

The `anthropic` import is deliberately lazy (inside `build_client`) so the rest
of the package -- agent loop, server, tests -- can run against an injected fake
client without the SDK installed.
"""

from __future__ import annotations

import os
from dataclasses import dataclass, field
from pathlib import Path

from dotenv import load_dotenv

load_dotenv(override=True)

# Some Anthropic-compatible providers (selected via ANTHROPIC_BASE_URL) reject
# the ANTHROPIC_AUTH_TOKEN header. Drop it whenever a custom base URL is set --
# same guard `learn-claude-code` uses.
if os.getenv("ANTHROPIC_BASE_URL"):
    os.environ.pop("ANTHROPIC_AUTH_TOKEN", None)


def _env_int(name: str, default: int) -> int:
    try:
        return int(os.environ[name])
    except (KeyError, ValueError):
        return default


def _env_float(name: str, default: float) -> float:
    try:
        return float(os.getenv(name, str(default)))
    except (TypeError, ValueError):
        return default


def _env_bool(name: str, default: bool) -> bool:
    value = os.getenv(name)
    if value is None:
        return default
    return value.strip().lower() not in ("", "0", "false", "no", "off")


@dataclass(frozen=True)
class Settings:
    """Process-wide settings, resolved once from the environment."""

    model: str = field(default_factory=lambda: os.getenv("MODEL_ID", "claude-sonnet-4-6"))
    base_url: str | None = field(default_factory=lambda: os.getenv("ANTHROPIC_BASE_URL") or None)
    api_key: str | None = field(default_factory=lambda: os.getenv("ANTHROPIC_API_KEY") or None)

    # Per-call generation budget.
    max_tokens: int = field(default_factory=lambda: _env_int("MINILOOP_MAX_TOKENS", 8000))

    # Auto-compaction fires once an estimate of the history crosses this.
    token_threshold: int = field(default_factory=lambda: _env_int("MINILOOP_TOKEN_THRESHOLD", 100_000))

    # Global cap on *simultaneous* LLM calls across every session (rate-limit
    # protection). Sessions still number in the thousands; only this many are
    # mid-request at any instant.
    max_concurrent_llm: int = field(default_factory=lambda: _env_int("MINILOOP_MAX_CONCURRENT_LLM", 8))

    # Global cap on tool calls explicitly registered as parallel-safe. Unsafe
    # tools remain ordered barriers and do not consume this semaphore.
    max_concurrent_tools: int = field(
        default_factory=lambda: _env_int("MINILOOP_MAX_CONCURRENT_TOOLS", 8)
    )

    # Hard ceiling on agent-loop turns, so a misbehaving model can't spin forever.
    max_turns: int = field(default_factory=lambda: _env_int("MINILOOP_MAX_TURNS", 50))
    subagent_max_rounds: int = field(default_factory=lambda: _env_int("MINILOOP_SUBAGENT_MAX_ROUNDS", 30))

    bash_timeout: int = field(default_factory=lambda: _env_int("MINILOOP_BASH_TIMEOUT", 120))

    workspace_root: Path = field(
        default_factory=lambda: Path(os.getenv("MINILOOP_WORKSPACE_ROOT", "./workspaces")).resolve()
    )
    skills_dir: Path = field(
        default_factory=lambda: Path(os.getenv("MINILOOP_SKILLS_DIR", "./skills")).resolve()
    )
    memory_root: Path | None = field(
        default_factory=lambda: Path(os.environ["MINILOOP_MEMORY_ROOT"]).resolve()
        if os.getenv("MINILOOP_MEMORY_ROOT") else None
    )
    repo_root: Path | None = field(
        default_factory=lambda: Path(os.environ["MINILOOP_REPO_ROOT"]).resolve()
        if os.getenv("MINILOOP_REPO_ROOT") else None
    )

    # Local append-only agent trajectories. They live outside individual
    # session workspaces so deleting a workspace does not erase its audit log.
    trajectory_root: Path | None = field(
        default_factory=lambda: Path(os.environ["MINILOOP_TRAJECTORY_ROOT"]).resolve()
        if os.getenv("MINILOOP_TRAJECTORY_ROOT") else None
    )
    trajectory_enabled: bool = field(
        default_factory=lambda: _env_bool("MINILOOP_TRAJECTORIES", True)
    )
    trajectory_capture_content: bool = field(
        default_factory=lambda: _env_bool("MINILOOP_TRAJECTORY_CAPTURE_CONTENT", True)
    )

    # Autonomous teammate WORK -> IDLE -> SHUTDOWN polling.
    team_idle_poll: float = field(default_factory=lambda: _env_float("MINILOOP_TEAM_IDLE_POLL", 1.0))
    team_idle_timeout: float = field(default_factory=lambda: _env_float("MINILOOP_TEAM_IDLE_TIMEOUT", 60.0))

    # When true, build_client returns the deterministic fake -- lets the server
    # boot and be exercised end-to-end with no API key.
    fake_llm: bool = field(default_factory=lambda: os.getenv("MINILOOP_FAKE_LLM", "") not in ("", "0", "false"))

    # Turn on the comprehensive s20 tool set and lifecycle injectors.
    # Env MINILOOP_FEATURES=all (or any non-empty/true) enables it on the default server.
    enable_features: bool = field(default_factory=lambda: os.getenv("MINILOOP_FEATURES", "") not in ("", "0", "false"))

    def __post_init__(self) -> None:
        if self.max_concurrent_tools < 1:
            raise ValueError("max_concurrent_tools must be at least 1")
        self.workspace_root.mkdir(parents=True, exist_ok=True)


def load_settings() -> Settings:
    return Settings()


def build_client(settings: Settings):
    """Return an async LLM client exposing `.messages.create(...)`.

    Real path: `anthropic.AsyncAnthropic`. Fake path (MINILOOP_FAKE_LLM): a
    deterministic stand-in from `mini_loop.fake_llm`.
    """
    if settings.fake_llm:
        from .fake_llm import FakeAsyncAnthropic

        return FakeAsyncAnthropic()

    from anthropic import AsyncAnthropic

    kwargs: dict = {}
    if settings.base_url:
        kwargs["base_url"] = settings.base_url
    if settings.api_key:
        kwargs["api_key"] = settings.api_key
    return AsyncAnthropic(**kwargs)
