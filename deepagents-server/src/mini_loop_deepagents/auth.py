"""Bearer authentication and resource isolation for the Agent Server.

The deployment's graphs are shared definitions. Conversation resources and
long-term store namespaces belong to the authenticated user; clients cannot
mutate assistant definitions or schedule background cron jobs.
"""

from __future__ import annotations

from collections.abc import Mapping
from hmac import compare_digest
from typing import Any

from langgraph_sdk import Auth

from mini_loop_deepagents.config import load_tokens


def create_auth(tokens: Mapping[str, str]) -> Auth:
    """Create a fail-closed auth policy from token-to-owner configuration.

    ``load_tokens`` validates deployment token strength. Taking a private copy
    here prevents changes to the caller's mapping from changing a live policy.
    Tokens are never included in user metadata, graph state, or error messages.
    """
    if not tokens or any(
        not isinstance(token, str)
        or not token
        or not isinstance(owner, str)
        or not owner
        for token, owner in tokens.items()
    ):
        raise ValueError("Authentication requires nonempty token-to-owner entries")
    credentials = tuple((token.encode("utf-8"), owner) for token, owner in tokens.items())
    policy = Auth()

    @policy.authenticate
    async def authenticate(authorization: str | None) -> Auth.types.MinimalUserDict:
        parts = authorization.split() if authorization else []
        if len(parts) != 2 or parts[0].lower() != "bearer":
            raise Auth.exceptions.HTTPException(
                status_code=401,
                detail="Invalid bearer credentials",
                headers={"WWW-Authenticate": "Bearer"},
            )
        supplied = parts[1].encode("utf-8")
        owner = None
        # Compare every configured token without early return on a match.
        for expected, candidate_owner in credentials:
            if compare_digest(supplied, expected):
                owner = candidate_owner
        if owner is None:
            raise Auth.exceptions.HTTPException(
                status_code=401,
                detail="Invalid bearer credentials",
                headers={"WWW-Authenticate": "Bearer"},
            )
        return {"identity": owner, "is_authenticated": True}

    @policy.on
    async def deny_unhandled(ctx: Auth.types.AuthContext, value: dict[str, Any]) -> bool:
        return False

    @policy.on.threads
    async def authorize_threads(
        ctx: Auth.types.AuthContext, value: dict[str, Any]
    ) -> Auth.types.FilterType:
        owner_filter = {"owner": ctx.user.identity}
        if ctx.action in {"create", "create_run", "update", "search"}:
            metadata = value.get("metadata")
            if not isinstance(metadata, dict):
                metadata = {}
                value["metadata"] = metadata
            # The server keeps a reference to this dict when handling creates
            # and updates. Mutate it in place so the persisted owner is stamped.
            metadata.update(owner_filter)
        # The server applies this to existing resources before reads, updates,
        # deletes and run operations, so caller-supplied IDs confer no access.
        return owner_filter

    @policy.on.assistants
    async def authorize_assistants(
        ctx: Auth.types.AuthContext, value: dict[str, Any]
    ) -> bool:
        # Server-installed graph assistants are shared immutable definitions.
        # Do not put user-specific secrets in their config or metadata.
        return ctx.action in {"read", "search"}

    @policy.on.store
    async def authorize_store(ctx: Auth.types.AuthContext, value: dict[str, Any]) -> None:
        namespace = value.get("namespace")
        if (
            not isinstance(namespace, (tuple, list))
            or not namespace
            or namespace[0] != ctx.user.identity
        ):
            raise Auth.exceptions.HTTPException(
                status_code=403, detail="Not authorized to access this namespace"
            )
        # Includes list_namespaces: an empty or foreign prefix is rejected.
        # Graph code already supplies the identity; never prepend it again.

    @policy.on.crons
    async def authorize_crons(ctx: Auth.types.AuthContext, value: dict[str, Any]) -> bool:
        return False

    return policy


auth = create_auth(load_tokens())
