"""Who is calling, and what they are allowed to see.

Until now the HTTP surface had none of this. An anonymous caller could create a
session, run arbitrary shell commands, enumerate *other* callers' sessions, read
their workspace paths, and download every recorded trajectory. The audit graded
a non-loopback bind `critical` for exactly that reason.

Shape borrowed from the OpenHands agent server
(``openhands-agent-server/openhands/agent_server/dependencies.py``):

* Authentication is a **dependency applied to the router**, not a check repeated
  in each handler -- a handler added later inherits it instead of forgetting it.
* Credentials are read from config **at request time**, so rotating a token does
  not mean restarting the process or re-registering routes.
* A credential channel opened for one surface stays scoped to it. Upstream
  accepts a cookie on its workspace static-file routes *only* because a browser
  cannot set headers on ``<iframe src>``, and its comment is explicit that no
  other endpoint honours it.

Two deliberate differences:

* **Comparison is constant-time.** Upstream tests membership with ``key not in
  keys``, which leaks token bytes through timing. ``hmac.compare_digest`` does
  not.
* **Unconfigured auth does not silently mean open.** Opt-in protection that
  defaults to silence is the aggregate failure this harness already has; a
  server without tokens refuses to bind anywhere but loopback rather than
  serving the world anonymously.

Ownership is enforced by *filtering*, and a session belonging to someone else
is reported as **404, not 403** -- 403 confirms that the id exists, which is
itself a disclosure to anyone enumerating.
"""

from __future__ import annotations

import hmac
import os
from collections.abc import Iterable, Mapping
from dataclasses import dataclass
from typing import Protocol

__all__ = [
    "ANONYMOUS",
    "LOOPBACK_HOSTS",
    "Principal",
    "Authenticator",
    "NullAuth",
    "TokenAuth",
    "load_auth",
]

LOOPBACK_HOSTS = frozenset({"127.0.0.1", "localhost", "::1", ""})


@dataclass(frozen=True, slots=True)
class Principal:
    """The identity a request acts as. `id` scopes everything it can reach."""

    id: str
    anonymous: bool = False


#: The single identity every caller shares when auth is not configured.
ANONYMOUS = Principal(id="anonymous", anonymous=True)


class Authenticator(Protocol):
    def authenticate(self, authorization: str | None) -> Principal | None:
        """Return the caller, or `None` when the credential is absent/invalid."""
        ...

    @property
    def configured(self) -> bool:
        """True when real credentials are required."""
        ...


class NullAuth:
    """No authentication. Every caller is the same anonymous principal."""

    def authenticate(self, authorization: str | None) -> Principal:
        return ANONYMOUS

    @property
    def configured(self) -> bool:
        return False


class TokenAuth:
    """Bearer tokens mapped to principals."""

    def __init__(self, tokens: Mapping[str, str]) -> None:
        if not tokens:
            raise ValueError("TokenAuth needs at least one token")
        for token, principal_id in tokens.items():
            if not token or not principal_id:
                raise ValueError("tokens and principal ids must be non-empty")
        self._tokens = dict(tokens)

    @property
    def configured(self) -> bool:
        return True

    def principals(self) -> tuple[str, ...]:
        return tuple(sorted(set(self._tokens.values())))

    def authenticate(self, authorization: str | None) -> Principal | None:
        if not authorization:
            return None
        scheme, _, presented = authorization.partition(" ")
        if scheme.lower() != "bearer" or not presented:
            return None
        # Compare against every token, without short-circuiting: returning as
        # soon as one matches would make the response time depend on token
        # order, and `==` would leak a prefix.
        matched: str | None = None
        for token, principal_id in self._tokens.items():
            if hmac.compare_digest(token, presented):
                matched = principal_id
        return Principal(id=matched) if matched is not None else None


def load_auth(environ: Mapping[str, str] | None = None) -> Authenticator:
    """Build an authenticator from the environment.

    ``MINILOOP_API_TOKENS`` is ``principal:token`` pairs separated by commas;
    ``MINILOOP_API_TOKEN`` is the single-user shorthand.
    """

    environ = os.environ if environ is None else environ
    raw = environ.get("MINILOOP_API_TOKENS", "").strip()
    if raw:
        tokens: dict[str, str] = {}
        for entry in raw.split(","):
            entry = entry.strip()
            if not entry:
                continue
            principal_id, _, token = entry.partition(":")
            if not token:
                raise ValueError(
                    "MINILOOP_API_TOKENS entries must be 'principal:token'"
                )
            # A token assigned to two principals is a shared credential -- one
            # caller silently authenticates as the other, and last-wins would
            # drop the first principal without a word. Refuse it: the same
            # fail-loud-on-insecure-config discipline as `refuse_open_bind`, at
            # the more likely place (a copy-paste in the token list).
            if token in tokens and tokens[token] != principal_id:
                raise ValueError(
                    "MINILOOP_API_TOKENS assigns one token to both "
                    f"{tokens[token]!r} and {principal_id!r}; a shared credential "
                    "lets one caller act as the other"
                )
            tokens[token] = principal_id
        return TokenAuth(tokens)

    single = environ.get("MINILOOP_API_TOKEN", "").strip()
    if single:
        return TokenAuth({single: "default"})
    return NullAuth()


def refuse_open_bind(host: str, auth: Authenticator) -> str | None:
    """Return why this bind must not be served, or `None` when it is safe.

    Serving a non-loopback address without authentication exposes session
    creation -- which runs shell commands -- to anyone who can route to the
    port. That is a refusal, not a warning: a warning is what the audit already
    prints, and it did not stop anyone.
    """

    if auth.configured or host in LOOPBACK_HOSTS:
        return None
    return (
        f"refusing to bind {host} without authentication: this would expose "
        "session creation, shell execution and every recorded transcript to "
        "any caller. Set MINILOOP_API_TOKEN, or bind 127.0.0.1."
    )
