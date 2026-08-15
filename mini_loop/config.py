"""Runtime configuration and LLM-client construction.

Mirrors `learn-claude-code`'s `.env` contract (ANTHROPIC_API_KEY / MODEL_ID /
optional ANTHROPIC_BASE_URL) and adds the few knobs a concurrent server needs.

The `anthropic` import is deliberately lazy (inside `build_client`) so the rest
of the package -- agent loop, server, tests -- can run against an injected fake
client without the SDK installed.
"""

from __future__ import annotations

import os
import re
from dataclasses import dataclass, field
from pathlib import Path

from dotenv import load_dotenv

load_dotenv(override=True)

# Some Anthropic-compatible providers (selected via ANTHROPIC_BASE_URL) reject
# the ANTHROPIC_AUTH_TOKEN header. Drop it whenever a custom base URL is set --
# same guard `learn-claude-code` uses.
if os.getenv("ANTHROPIC_BASE_URL"):
    os.environ.pop("ANTHROPIC_AUTH_TOKEN", None)


# A value the parser does not recognize is refused at boot, never guessed
# around. These helpers used to fall back to the default on any parse failure
# (`MINILOOP_MAX_TOKENS=8k` silently meant 8000) and to read any unknown
# boolean spelling as True (`MINILOOP_TRAJECTORY_CAPTURE_CONTENT=flase` kept
# recording full content for an operator who had turned it off). An accepted-
# but-misread setting runs with someone else's configuration; dsh's
# post-mortem 0002 is the same defect class at Loader scope, and its guardrail
# is the same: reject at the boundary. Missing stays the default, and the
# empty string keeps its historical meaning (default for numbers, False for
# booleans) because deployment tooling routinely exports empty vars.

def _reject(name: str, value: str, expected: str) -> None:
    raise ValueError(
        f"{name}={value!r} is not {expected}; refusing to guess. "
        f"Unset {name} to use the default."
    )


def _env_int(name: str, default: int) -> int:
    value = os.getenv(name)
    if value is None or not value.strip():
        return default
    try:
        return int(value)
    except ValueError:
        _reject(name, value, "an integer")


def _env_float(name: str, default: float) -> float:
    value = os.getenv(name)
    if value is None or not value.strip():
        return default
    try:
        return float(value)
    except ValueError:
        _reject(name, value, "a number")


_TRUE = ("1", "true", "yes", "on")
_FALSE = ("", "0", "false", "no", "off")


def _env_bool(name: str, default: bool) -> bool:
    value = os.getenv(name)
    if value is None:
        return default
    normalized = value.strip().lower()
    if normalized in _TRUE:
        return True
    if normalized in _FALSE:
        return False
    _reject(name, value, f"a boolean ({'/'.join(_TRUE)} or 0/false/no/off)")


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

    # Harness-level token-efficiency stages.  Components are assembled
    # explicitly by SessionManager; environment variables select only the
    # trusted built-ins and never trigger arbitrary package discovery.
    token_efficiency_mode: str = field(
        default_factory=lambda: os.getenv(
            "MINILOOP_TOKEN_EFFICIENCY_MODE", "off"
        ).strip().lower()
    )
    token_efficiency_response_style: str = field(
        default_factory=lambda: os.getenv(
            "MINILOOP_TOKEN_EFFICIENCY_RESPONSE_STYLE", "normal"
        ).strip().lower()
    )
    token_efficiency_persist_raw: bool = field(
        default_factory=lambda: _env_bool(
            "MINILOOP_TOKEN_EFFICIENCY_PERSIST_RAW", True
        )
    )
    token_efficiency_raw_min_bytes: int = field(
        default_factory=lambda: _env_int(
            "MINILOOP_TOKEN_EFFICIENCY_RAW_MIN_BYTES", 16_384
        )
    )
    token_efficiency_artifact_ttl_seconds: float = field(
        default_factory=lambda: _env_float(
            "MINILOOP_TOKEN_EFFICIENCY_ARTIFACT_TTL_SECONDS", 3_600.0
        )
    )
    token_efficiency_max_artifact_bytes: int = field(
        default_factory=lambda: _env_int(
            "MINILOOP_TOKEN_EFFICIENCY_MAX_ARTIFACT_BYTES", 2_000_000
        )
    )
    token_efficiency_max_total_bytes: int = field(
        default_factory=lambda: _env_int(
            "MINILOOP_TOKEN_EFFICIENCY_MAX_TOTAL_BYTES", 20_000_000
        )
    )

    # Optional stateless semantic-code tools.  Off by default so deployments
    # without the pinned ast-outline binary keep the existing tool surface.
    ast_outline_enabled: bool = field(
        default_factory=lambda: _env_bool("MINILOOP_AST_OUTLINE_ENABLED", False)
    )
    ast_outline_binary: str = field(
        default_factory=lambda: os.getenv(
            "MINILOOP_AST_OUTLINE_BINARY", "ast-outline"
        ).strip()
    )
    ast_outline_sha256: str | None = field(
        default_factory=lambda: (
            os.getenv("MINILOOP_AST_OUTLINE_SHA256", "").strip().lower() or None
        )
    )
    ast_outline_timeout: float = field(
        default_factory=lambda: _env_float("MINILOOP_AST_OUTLINE_TIMEOUT", 10.0)
    )
    ast_outline_max_output_bytes: int = field(
        default_factory=lambda: _env_int(
            "MINILOOP_AST_OUTLINE_MAX_OUTPUT_BYTES", 1_000_000
        )
    )

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

    # How long a turn waits on a pending tool approval before the safe
    # default -- deny -- answers instead. See mini_loop/approvals.py.
    approval_timeout: float = field(
        default_factory=lambda: float(_env_int("MINILOOP_APPROVAL_TIMEOUT", 300))
    )

    workspace_root: Path = field(
        default_factory=lambda: Path(os.getenv("MINILOOP_WORKSPACE_ROOT", "./workspaces")).resolve()
    )
    # Where oversized tool output is preserved when truncated. Empty string
    # disables preservation (truncation reverts to drop-the-middle).
    spill_dir: Path | None = field(
        default_factory=lambda: (
            Path(os.getenv("MINILOOP_SPILL_DIR", "./var/spill")).resolve()
            if os.getenv("MINILOOP_SPILL_DIR", "./var/spill").strip()
            else None
        )
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

    # Experimental Dynamic Workflow MVP. The default FastAPI server deliberately
    # does not consume this flag: callers must opt a local SessionManager into the
    # surface explicitly so the unauthenticated REST API never gains it by env
    # accident.
    enable_workflows: bool = field(
        default_factory=lambda: _env_bool("MINILOOP_EXPERIMENTAL_WORKFLOWS", False)
    )
    workflow_max_concurrent_agents: int = field(
        default_factory=lambda: _env_int("MINILOOP_WORKFLOW_MAX_CONCURRENT_AGENTS", 4)
    )
    workflow_max_agents: int = field(
        default_factory=lambda: _env_int("MINILOOP_WORKFLOW_MAX_AGENTS", 32)
    )
    workflow_max_rounds: int = field(
        default_factory=lambda: _env_int("MINILOOP_WORKFLOW_MAX_ROUNDS", 4)
    )
    workflow_wall_time_seconds: float = field(
        default_factory=lambda: _env_float("MINILOOP_WORKFLOW_WALL_TIME_SECONDS", 900.0)
    )

    def __post_init__(self) -> None:
        if self.token_efficiency_mode not in {"off", "shadow", "enforce"}:
            raise ValueError(
                "token_efficiency_mode must be one of: off, shadow, enforce"
            )
        if self.token_efficiency_response_style not in {"normal", "concise"}:
            raise ValueError(
                "token_efficiency_response_style must be one of: normal, concise"
            )
        if self.token_efficiency_raw_min_bytes < 1:
            raise ValueError("token_efficiency_raw_min_bytes must be positive")
        if self.token_efficiency_artifact_ttl_seconds <= 0:
            raise ValueError(
                "token_efficiency_artifact_ttl_seconds must be positive"
            )
        if self.token_efficiency_max_artifact_bytes < 1:
            raise ValueError(
                "token_efficiency_max_artifact_bytes must be positive"
            )
        if self.token_efficiency_max_total_bytes < 1:
            raise ValueError("token_efficiency_max_total_bytes must be positive")
        if (
            self.token_efficiency_raw_min_bytes
            > self.token_efficiency_max_artifact_bytes
        ):
            raise ValueError(
                "token_efficiency_raw_min_bytes must not exceed "
                "token_efficiency_max_artifact_bytes"
            )
        if (
            self.token_efficiency_max_artifact_bytes
            > self.token_efficiency_max_total_bytes
        ):
            raise ValueError(
                "token_efficiency_max_artifact_bytes must not exceed "
                "token_efficiency_max_total_bytes"
            )
        if not self.ast_outline_binary:
            raise ValueError("ast_outline_binary must not be empty")
        if self.ast_outline_sha256 is not None and not re.fullmatch(
            r"[0-9a-f]{64}", self.ast_outline_sha256
        ):
            raise ValueError("ast_outline_sha256 must be 64 lowercase hex characters")
        if self.ast_outline_enabled:
            if not Path(self.ast_outline_binary).expanduser().is_absolute():
                raise ValueError(
                    "enabled ast-outline requires an operator-pinned absolute binary path"
                )
            if self.ast_outline_sha256 is None:
                raise ValueError(
                    "enabled ast-outline requires ast_outline_sha256"
                )
        if self.ast_outline_timeout <= 0:
            raise ValueError("ast_outline_timeout must be positive")
        if self.ast_outline_max_output_bytes < 1:
            raise ValueError("ast_outline_max_output_bytes must be positive")
        if self.max_concurrent_tools < 1:
            raise ValueError("max_concurrent_tools must be at least 1")
        # Same failure mode as the check above, and it was missing: a
        # Semaphore(0) is never acquirable, so max_concurrent_llm < 1 does not
        # slow the agent -- it hangs it forever on the first model call, with
        # no error. Validated loudly at construction rather than deadlocked at
        # runtime.
        if self.max_concurrent_llm < 1:
            raise ValueError("max_concurrent_llm must be at least 1")
        # A zero round budget is a silent no-op: `for _ in range(0)` runs the
        # loop body never, so the agent returns having done nothing at all.
        if self.max_turns < 1:
            raise ValueError("max_turns must be at least 1")
        if self.subagent_max_rounds < 1:
            raise ValueError("subagent_max_rounds must be at least 1")
        if self.workflow_max_concurrent_agents < 1:
            raise ValueError("workflow_max_concurrent_agents must be at least 1")
        if self.workflow_max_concurrent_agents > 4:
            raise ValueError("workflow_max_concurrent_agents must not exceed 4")
        if self.workflow_max_agents < self.workflow_max_concurrent_agents:
            raise ValueError(
                "workflow_max_agents must be greater than or equal to "
                "workflow_max_concurrent_agents"
            )
        if self.workflow_max_agents > 32:
            raise ValueError("workflow_max_agents must not exceed 32")
        if self.workflow_max_rounds < 1:
            raise ValueError("workflow_max_rounds must be at least 1")
        if self.workflow_wall_time_seconds <= 0:
            raise ValueError("workflow_wall_time_seconds must be positive")
        # The budgets and timeouts have the same failure mode as the semaphores
        # above -- a non-positive one does not slow the harness, it breaks it
        # silently, so it is validated loudly at construction rather than
        # discovered as "the agent does nothing" at runtime:
        #   max_tokens < 1        -> the provider rejects every request;
        #   token_threshold < 1   -> compaction fires every turn, summarizing the
        #                            transcript away before the agent can use it;
        #   bash_timeout < 1      -> every shell command times out immediately;
        #   approval_timeout <= 0 -> every approval is denied before it is asked;
        #   team_idle_poll <= 0   -> the idle loop busy-spins with no sleep;
        #   team_idle_timeout<= 0 -> a teammate shuts down before doing any work.
        if self.max_tokens < 1:
            raise ValueError("max_tokens must be at least 1")
        if self.token_threshold < 1:
            raise ValueError("token_threshold must be at least 1")
        if self.bash_timeout < 1:
            raise ValueError("bash_timeout must be at least 1")
        if self.approval_timeout <= 0:
            raise ValueError("approval_timeout must be positive")
        if self.team_idle_poll <= 0:
            raise ValueError("team_idle_poll must be positive")
        if self.team_idle_timeout <= 0:
            raise ValueError("team_idle_timeout must be positive")
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

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: settings are a frozen dataclass resolved once from the environment; immutable state cannot drift after validation."
)
