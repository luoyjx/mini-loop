"""Deployment-owned configuration. Request context never supplies authority."""

from __future__ import annotations

import json
import math
import os
import re
from dataclasses import dataclass
from typing import Mapping

PREFIX = "MINILOOP_DEEPAGENTS_"
_OWNER = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.@+~:-]{0,127}\Z")


def _unique_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate token configuration key")
        result[key] = value
    return result


def load_tokens(environ: Mapping[str, str] | None = None) -> dict[str, str]:
    """Load token→owner bindings, rejecting missing/ambiguous configuration."""
    env = os.environ if environ is None else environ
    try:
        raw = json.loads(env.get(PREFIX + "TOKENS", ""), object_pairs_hook=_unique_object)
    except (ValueError, TypeError):
        raise ValueError("MINILOOP_DEEPAGENTS_TOKENS must be a nonempty JSON token-to-owner object") from None
    if not isinstance(raw, dict) or not raw:
        raise ValueError("MINILOOP_DEEPAGENTS_TOKENS must be a nonempty JSON token-to-owner object")
    for token, owner in raw.items():
        if not isinstance(token, str) or len(token) < 32 or not token.isascii() or token.startswith("replace-with-") or any(c.isspace() for c in token):
            raise ValueError("deployment bearer tokens must contain at least 32 non-whitespace ASCII characters")
        if not isinstance(owner, str) or not _OWNER.fullmatch(owner) or owner in {".", ".."}:
            raise ValueError("owner IDs must be safe namespace components, 1–128 characters")
    return raw


@dataclass(frozen=True)
class AgentSettings:
    model: str
    max_tokens: int = 4096
    timeout_seconds: float = 60.0

    @classmethod
    def from_env(cls, environ: Mapping[str, str] | None = None) -> AgentSettings:
        env = os.environ if environ is None else environ
        model = env.get(PREFIX + "MODEL", "openai:gpt-4.1-mini")
        # Provider choice is deployment-owned, never read from Run context/config.
        if not model.startswith("openai:") or not model.removeprefix("openai:").strip():
            raise ValueError("MINILOOP_DEEPAGENTS_MODEL must be an openai:model-name identifier")
        try:
            max_tokens = int(env.get(PREFIX + "MAX_TOKENS", "4096"))
            timeout = float(env.get(PREFIX + "TIMEOUT_SECONDS", "60"))
        except ValueError:
            raise ValueError("model limits must be numeric") from None
        if not 1 <= max_tokens <= 32768 or not math.isfinite(timeout) or not 1 <= timeout <= 300:
            raise ValueError("max_tokens must be 1–32768 and timeout_seconds must be 1–300")
        return cls(model=model, max_tokens=max_tokens, timeout_seconds=timeout)
