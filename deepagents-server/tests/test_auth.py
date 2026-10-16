"""Exercise the callbacks registered with the actual LangGraph SDK Auth API."""

from __future__ import annotations

import importlib
from types import SimpleNamespace

import pytest
from langgraph_sdk import Auth


TOKENS = {"a" * 40: "alice", "b" * 40: "bob"}


@pytest.fixture
def auth_module(monkeypatch):
    from mini_loop_deepagents import config

    # Auth's deployment export loads configuration at import time. Isolate that
    # bootstrap from the shell environment; HTTP smoke covers the real loader.
    monkeypatch.setattr(config, "load_tokens", lambda: dict(TOKENS))
    return importlib.import_module("mini_loop_deepagents.auth")


@pytest.fixture
def policy(auth_module):
    return auth_module.create_auth(TOKENS)


def context(resource, action, owner="alice"):
    return Auth.types.AuthContext(
        resource=resource,
        action=action,
        user=SimpleNamespace(identity=owner),
        permissions=[],
    )


def handler(policy, resource):
    # The SDK documents these registered handler attributes as its server
    # integration contract. Calling them proves the real decorators accepted
    # the signatures and registered each policy at the intended scope.
    return policy._handlers[(resource, "*")][0]


@pytest.mark.asyncio
@pytest.mark.parametrize("scheme", ["Bearer", "bearer", "BEARER"])
async def test_authentication_uses_token_identity(policy, scheme):
    result = await policy._authenticate_handler(f"{scheme} {'a' * 40}")
    assert result == {"identity": "alice", "is_authenticated": True}
    assert "a" * 40 not in repr(result)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "authorization",
    [None, "", "Bearer", "Basic " + "a" * 40, "Bearer wrong", "Bearer 无效", "Bearer a b"],
)
async def test_missing_or_invalid_credentials_return_401(policy, authorization):
    with pytest.raises(Auth.exceptions.HTTPException) as error:
        await policy._authenticate_handler(authorization)
    assert error.value.status_code == 401
    assert error.value.headers == {"WWW-Authenticate": "Bearer"}
    assert error.value.detail == "Invalid bearer credentials"


@pytest.mark.asyncio
async def test_policy_copies_credentials_and_checks_all_tokens(auth_module, monkeypatch):
    tokens = dict(TOKENS)
    policy = auth_module.create_auth(tokens)
    tokens.clear()
    comparisons = []
    original = auth_module.compare_digest

    def record_compare(supplied, expected):
        comparisons.append((supplied, expected))
        return original(supplied, expected)

    monkeypatch.setattr(auth_module, "compare_digest", record_compare)
    assert (await policy._authenticate_handler("Bearer " + "a" * 40))["identity"] == "alice"
    assert len(comparisons) == 2


@pytest.mark.parametrize("tokens", [{}, {"": "alice"}, {"token": ""}, {None: "alice"}])
def test_empty_or_malformed_policy_is_rejected(auth_module, tokens):
    with pytest.raises(ValueError, match="token-to-owner"):
        auth_module.create_auth(tokens)


@pytest.mark.asyncio
@pytest.mark.parametrize("action", ["create", "create_run", "read", "update", "delete", "search"])
async def test_all_thread_actions_require_original_owner(policy, action):
    metadata = {"owner": "bob", "label": "keep"}
    value = {"thread_id": "foreign-id", "metadata": metadata}
    filters = await handler(policy, "threads")(ctx=context("threads", action), value=value)
    assert filters == {"owner": "alice"}
    if action in {"create", "create_run", "update", "search"}:
        assert value["metadata"] == {"owner": "alice", "label": "keep"}
        assert value["metadata"] is metadata


@pytest.mark.asyncio
async def test_thread_run_without_metadata_is_owned(policy):
    value = {"metadata": None}
    await handler(policy, "threads")(ctx=context("threads", "create_run"), value=value)
    assert value["metadata"] == {"owner": "alice"}


@pytest.mark.asyncio
async def test_direct_run_cannot_disable_sync_checkpoints(policy):
    arguments = {"durability": "exit", "checkpoint_during": False, "input": {"messages": []}}
    value = {"kwargs": arguments}
    await handler(policy, "threads")(context("threads", "create_run"), value)
    assert value["kwargs"] is arguments
    assert arguments == {"durability": "sync", "checkpoint_during": True, "input": {"messages": []}}


@pytest.mark.asyncio
@pytest.mark.parametrize("action", ["put", "get", "search", "delete", "list_namespaces"])
async def test_store_operations_preserve_explicit_owner_namespace(policy, action):
    value = {"namespace": ("alice", "memories")}
    result = await handler(policy, "store")(ctx=context("store", action), value=value)
    assert result is None
    assert value["namespace"] == ("alice", "memories")


@pytest.mark.asyncio
@pytest.mark.parametrize("action", ["put", "get", "search", "delete", "list_namespaces"])
@pytest.mark.parametrize("namespace", [None, (), ("bob",), ("*",), "alice"])
async def test_store_denies_foreign_or_unscoped_access(policy, action, namespace):
    with pytest.raises(Auth.exceptions.HTTPException) as error:
        await handler(policy, "store")(
            ctx=context("store", action), value={"namespace": namespace}
        )
    assert error.value.status_code == 403


@pytest.mark.asyncio
@pytest.mark.parametrize("action", ["read", "search", "create", "update", "delete"])
async def test_shared_assistants_allow_only_inspection(policy, action):
    result = await handler(policy, "assistants")(
        ctx=context("assistants", action), value={}
    )
    assert result is (action in {"read", "search"})


@pytest.mark.asyncio
@pytest.mark.parametrize("action", ["create", "read", "update", "delete", "search"])
async def test_crons_are_disabled(policy, action):
    assert await handler(policy, "crons")(ctx=context("crons", action), value={}) is False


@pytest.mark.asyncio
async def test_unhandled_resources_are_denied(policy):
    assert len(policy._global_handlers) == 1
    assert await policy._global_handlers[0](ctx=context("runs", "read"), value={}) is False
