"""Keep host credentials out of the transcript, the event stream, and the disk.

`run_bash` inherits the whole process environment, so `printenv` puts the host's
API keys into the tool result -- and from there into the model's context, the
SSE stream the console renders, the trajectory JSONL, and (since the state store
landed) a SQLite file on disk. Four sinks, one leak.

Five and six turned up later, and from an unexpected direction: **compaction
spills context to files in the workspace** -- large tool results to
`.task_outputs/`, whole transcripts to `.transcripts/` -- and it predates this
module, so it was never on anyone's list of sinks. Those files are worse than
the transcript on two counts: they outlive the session, and they sit in the
directory the agent itself can read, with the path handed to the model in the
replacement text. Enumerating sinks by hand is how both were missed; the guard
now sweeps the entire workspace instead.

Seven is memory, and it is the worst of them. Compaction spills are debris;
memories are durable *by design*, and their index is read back into the context
of every later request. A credential captured into one is not merely written
down, it is re-injected indefinitely -- by a subsystem whose whole purpose is to
outlive the session.

Eight is the cron file, which persists a *prompt* that will fire unattended
after a restart. Enumerating sinks by hand is how five, six, seven and eight
were each missed in turn; the guards now sweep whole directories rather than
naming paths.

Nine was the event table -- and it was found by a *sweep* rather than by
reading, which is the point. `_capture_event` persists the transcript and the
event stream in the same function; the first had been masked for rounds and the
second never was, so `messages` was clean and `events` held the same text raw.
Events are now masked once, before they reach the durable table, the trajectory
or any SSE subscriber.

Ten and eleven were the team mailbox and the task store, and they were found by
*asking every content store the same question at once* rather than by reading
another module. That is the only way this list has ever stopped growing one
entry at a time -- see `tests/test_content_stores.py`.

The design is taken from the OpenHands SDK `SecretRegistry`
(``openhands-sdk/openhands/sdk/conversation/secret_registry.py``), which splits
the problem in two:

* **Injection is narrow.** A command only receives the secrets whose *names* it
  mentions, so an unrelated command cannot read them out of its own environment.
* **Masking is wide.** Output is scrubbed of the value of *every* registered
  secret, not only the ones a command referenced -- upstream's comment is worth
  repeating: a value can reach the output without the command ever naming it,
  "e.g. a token in a git remote URL".

Two more properties are load-bearing and easy to get wrong:

* **Cached values are never re-resolved.** After a secret rotates, the registry
  keeps masking the value it previously handed out; re-resolving would let the
  old value start appearing in output again.
* **Short values are not masked.** Upstream has no length floor; masking a
  two-character value would shred unrelated output, so a floor is applied here
  and short secrets are reported rather than silently ignored.

Masking runs in **both directions**, because tool *arguments* are model-generated
too: a model that read a credential can write it straight into a command.
Masking only results covered one side, and the argument reached the event
stream, the console, the trajectory and the durable tables -- four sinks out of
five.

What is masked is what is **recorded and emitted**, never what is executed: the
live `ToolCall` handed to a tool keeps the real value, or the command would not
work. One place keeps the raw value on purpose -- the in-memory transcript,
which goes back to the same provider that already holds the credential and dies
with the process. Everything that reaches a console, a disk or another party is
masked.

It still does **not** cover prose the model writes about a credential it read.
Nothing here substitutes for not giving an agent credentials it does not need.
"""

from __future__ import annotations

import fnmatch
import os
import re
import threading
import time
from collections.abc import Callable, Iterable, Mapping
from typing import Any

__all__ = [
    "MASK",
    "MIN_MASKABLE_LENGTH",
    "DEFAULT_SECRET_PATTERNS",
    "SecretRegistry",
    "NullSecretRegistry",
]

MASK = "<secret-hidden>"
# Below this, a value is more likely to be a common substring than a credential,
# and masking it would corrupt unrelated output.
MIN_MASKABLE_LENGTH = 8
FAILED_LOOKUP_RETRY_SECONDS = 60.0

# Terminal control sequences are presentation bytes, not visible credential
# characters.  A tool can split ``TOPSECRET`` as ``TOP\x1b[31mSECRET``; a
# reducer that strips colour then reconstructs the credential unless masking
# treats those bytes as transparent while matching.  CSI covers colour/cursor
# controls, OSC covers terminal-title/hyperlink controls, and the final branch
# covers the remaining two-byte ESC sequences.
_ANSI_ESCAPE = (
    r"(?:\x1b(?:\[[0-?]*[ -/]*[@-~]"
    r"|\][^\x07\x1b]*(?:\x07|\x1b\\)"
    r"|[@-_]))"
)


def _mask_ansi_interleaved(text: str, value: str, replacement: str) -> str:
    """Mask ``value`` even when ANSI controls occur between its characters."""

    if not value:
        return text
    separator = rf"(?:{_ANSI_ESCAPE})*"
    pattern = separator.join(re.escape(character) for character in value)
    return re.sub(pattern, lambda _match: replacement, text)

DEFAULT_SECRET_PATTERNS = (
    "*_API_KEY",
    "*_APIKEY",
    "*_TOKEN",
    "*_SECRET",
    "*_SECRET_KEY",
    "*_PASSWORD",
    "*_PASSWD",
    "*_CREDENTIALS",
    "*_PRIVATE_KEY",
    "AWS_SECRET_ACCESS_KEY",
    "AWS_SESSION_TOKEN",
)


class NullSecretRegistry:
    """Register nothing, mask nothing. The behaviour before this module."""

    def names(self) -> tuple[str, ...]:
        return ()

    def find_in_text(self, text: str) -> set[str]:
        return set()

    def env_for_command(self, command: str) -> dict[str, str]:
        return {}

    def scrub_env(self, environ: Mapping[str, str] | None = None) -> dict[str, str]:
        return dict(os.environ if environ is None else environ)

    def mask(self, text: Any) -> Any:
        return text

    def mask_payload(self, value: Any) -> Any:
        return value


class SecretRegistry:
    """Named secrets: injected by name, masked by value."""

    def __init__(
        self,
        *,
        mask_with: str = MASK,
        min_length: int = MIN_MASKABLE_LENGTH,
    ) -> None:
        self._sources: dict[str, Callable[[], str | None]] = {}
        self._resolved: dict[str, str] = {}
        self._failed_at: dict[str, float] = {}
        self._too_short: set[str] = set()
        self._lock = threading.RLock()
        self.mask_with = mask_with
        self.min_length = min_length

    # -- registration ------------------------------------------------------
    def register(self, name: str, value: str | Callable[[], str | None]) -> None:
        """Register a secret by name. `value` may be a callable resolved lazily."""

        with self._lock:
            self._sources[name] = value if callable(value) else (lambda v=value: v)
            self._resolved.pop(name, None)
            self._failed_at.pop(name, None)
            self._too_short.discard(name)

    @classmethod
    def from_environ(
        cls,
        *,
        patterns: Iterable[str] = DEFAULT_SECRET_PATTERNS,
        environ: Mapping[str, str] | None = None,
        extra_names: Iterable[str] = (),
        **kwargs: Any,
    ) -> "SecretRegistry":
        """Seed from environment variables whose names look like credentials."""

        source = os.environ if environ is None else environ
        registry = cls(**kwargs)
        globs = tuple(patterns)
        for name, value in source.items():
            if not value:
                continue
            if name in extra_names or any(
                fnmatch.fnmatchcase(name.upper(), pattern) for pattern in globs
            ):
                registry.register(name, value)
        return registry

    def names(self) -> tuple[str, ...]:
        with self._lock:
            return tuple(sorted(self._sources))

    def unresolved(self) -> tuple[str, ...]:
        """Registered names whose value could not be read.

        A lookup that fails is not a neutral event: the name stays registered,
        so the deployment believes it is masked, while `mask()` has no value to
        look for and the credential passes through every sink untouched. The
        failure was swallowed and cached for a retry window, which is right --
        a broken vault must not stall an agent -- but it has to be reportable.
        Same rule as a failing state store: degrade, then say so.
        """

        with self._lock:
            return tuple(sorted(self._failed_at))

    def short_values(self) -> tuple[str, ...]:
        """Names whose value is too short to mask safely -- reported, not hidden."""

        with self._lock:
            return tuple(sorted(self._too_short))

    # -- resolution --------------------------------------------------------
    def _value(self, name: str) -> str | None:
        """Resolve once and cache. A rotated secret keeps masking its old value."""

        with self._lock:
            cached = self._resolved.get(name)
            if cached is not None:
                return cached
            failed_at = self._failed_at.get(name)
            if failed_at is not None and time.monotonic() - failed_at < FAILED_LOOKUP_RETRY_SECONDS:
                return None
            source = self._sources.get(name)
        if source is None:
            return None
        try:
            value = source()
        except Exception:
            with self._lock:
                self._failed_at[name] = time.monotonic()
            return None
        if not value:
            with self._lock:
                self._failed_at[name] = time.monotonic()
            return None
        with self._lock:
            self._resolved[name] = value
            self._failed_at.pop(name, None)
            if len(value) < self.min_length:
                self._too_short.add(name)
        return value

    # -- injection is narrow ----------------------------------------------
    def find_in_text(self, text: str) -> set[str]:
        """Registered names mentioned in `text`, case-insensitively."""

        if not text:
            return set()
        lowered = text.lower()
        with self._lock:
            return {name for name in self._sources if name.lower() in lowered}

    def env_for_command(self, command: str) -> dict[str, str]:
        """Only the secrets this command names get handed to it."""

        out: dict[str, str] = {}
        for name in self.find_in_text(command):
            value = self._value(name)
            if value:
                out[name] = value
        return out

    def scrub_env(self, environ: Mapping[str, str] | None = None) -> dict[str, str]:
        """A copy of the environment with every registered name removed."""

        source = os.environ if environ is None else environ
        with self._lock:
            known = set(self._sources)
        return {k: v for k, v in source.items() if k not in known}

    # -- masking is wide ---------------------------------------------------
    def mask_payload(self, value: Any) -> Any:
        """Mask every string inside a nested structure.

        Tool *arguments* are model-generated, so a model that read a credential
        can write it straight into a command. Masking only outputs left that
        copy in the event stream, the console, the trajectory and the durable
        tables -- four sinks out of five.

        This masks what is **recorded and emitted**, never what is executed: it
        runs on a copy, and the live `ToolCall` handed to the tool keeps the
        real value or the command would not work.

        Keys are masked as well as values. A credential-listing tool that keys a
        map *by* the credential (`{"<token>": {"scopes": ...}}` -- a real API
        shape) otherwise wrote the secret straight into the trajectory and the
        durable tables as a key, which the value-only mask walked past. `mask`
        leaves a non-string key untouched, so this only ever rewrites a key that
        actually carries a registered secret; if two keys collapse to the same
        masked form the last wins, which loses a recorded value but never leaks
        the key -- the right trade on a copy kept for audit.
        """

        if isinstance(value, str):
            return self.mask(value)
        if isinstance(value, Mapping):
            return {
                self.mask(key): self.mask_payload(item)
                for key, item in value.items()
            }
        if isinstance(value, (list, tuple)):
            return [self.mask_payload(item) for item in value]
        return value

    def mask(self, text: Any) -> Any:
        """Replace every registered secret's value wherever it appears.

        Values are masked whether or not the producing command referenced the
        name: a token can arrive embedded in a URL, a config dump, or a stack
        trace. Longest first, so a secret that contains another is not left
        half-masked.
        """

        if not isinstance(text, str) or not text:
            return text
        values = []
        for name in self.names():
            value = self._value(name)
            if value and len(value) >= self.min_length:
                values.append(value)
        for value in sorted(values, key=len, reverse=True):
            # The regex also matches the ordinary no-ANSI form.  Use a callable
            # replacement so custom mask strings containing ``\\`` stay literal.
            text = _mask_ansi_interleaved(text, value, self.mask_with)
        return text

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: masking correctness is characterized by round-trip tests; a runtime self-check would need the plaintext it exists to hide."
)
