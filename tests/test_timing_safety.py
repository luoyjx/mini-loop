"""Bearer tokens are compared in constant time -- now actually pinned.

`auth.py` calls this out as a deliberate difference from the upstream it was
modelled on: upstream tests membership with `key not in keys`, which leaks token
bytes through timing, and this uses `hmac.compare_digest`. It also compares
against *every* token without short-circuiting, so response time does not depend
on which token matched or on the order they were registered.

Both properties were documented, neither was tested. Replacing
`hmac.compare_digest(token, presented)` with `token == presented` passed the
entire suite -- found by `tools/verify_guards.py`, which breaks each hardening
on purpose and requires a named test to notice.

Timing is asserted structurally rather than by measurement: a wall-clock test
for a few hundred nanoseconds is noise on shared CI, and a flaky security test
gets deleted. The structure is the thing that can regress in a diff.
"""

import ast
import hmac
import pathlib

import pytest

from mini_loop.auth import Principal, TokenAuth, load_auth

AUTH_SOURCE = pathlib.Path(__file__).resolve().parent.parent / "mini_loop" / "auth.py"


def _authenticate_body():
    tree = ast.parse(AUTH_SOURCE.read_text())
    for node in ast.walk(tree):
        if isinstance(node, ast.FunctionDef) and node.name == "authenticate":
            for parent in ast.walk(tree):
                if (isinstance(parent, ast.ClassDef) and parent.name == "TokenAuth"
                        and node in parent.body):
                    return node
    raise AssertionError("TokenAuth.authenticate not found")


def test_the_credential_is_never_compared_with_equality():
    """`==` on a secret leaks a prefix through timing, one byte at a time."""
    body = _authenticate_body()
    for node in ast.walk(body):
        if isinstance(node, ast.Compare):
            operators = {type(op).__name__ for op in node.ops}
            if operators & {"Eq", "NotEq", "In", "NotIn"}:
                rendered = ast.unparse(node)
                assert "presented" not in rendered or "scheme" in rendered, (
                    f"the presented credential is compared with {operators}: {rendered}"
                )


def test_comparison_goes_through_compare_digest():
    body = _authenticate_body()
    calls = {
        ast.unparse(node.func)
        for node in ast.walk(body)
        if isinstance(node, ast.Call)
    }
    assert any("compare_digest" in call for call in calls), (
        f"authenticate does not call hmac.compare_digest; it calls {sorted(calls)}"
    )


def test_every_token_is_compared_regardless_of_which_one_matches(monkeypatch):
    """No short-circuit: returning early makes latency reveal token order."""
    comparisons: list[int] = []
    real = hmac.compare_digest

    def counting(a, b):
        comparisons.append(1)
        return real(a, b)

    monkeypatch.setattr("mini_loop.auth.hmac.compare_digest", counting)
    auth = TokenAuth({"tok-a": "alice", "tok-b": "bob", "tok-c": "carol"})

    counts = []
    for token in ("tok-a", "tok-c", "no-such-token"):
        comparisons.clear()
        auth.authenticate(f"Bearer {token}")
        counts.append(len(comparisons))

    assert len(set(counts)) == 1, (
        f"comparison count varies with the token: {counts} -- position is observable"
    )
    assert counts[0] == 3


def test_a_matching_token_still_authenticates():
    auth = TokenAuth({"tok-a": "alice", "tok-b": "bob"})
    assert auth.authenticate("Bearer tok-b") == Principal(id="bob")
    assert auth.authenticate("Bearer nope") is None


@pytest.mark.parametrize(
    "header", [None, "", "tok-a", "Basic tok-a", "Bearer", "Bearer "]
)
def test_malformed_credentials_are_refused_without_comparing(header):
    auth = TokenAuth({"tok-a": "alice"})
    assert auth.authenticate(header) is None


def test_the_shipped_loader_produces_a_constant_time_authenticator():
    auth = load_auth({"MINILOOP_API_TOKEN": "secret-value"})
    assert isinstance(auth, TokenAuth)
    assert auth.authenticate("Bearer secret-value") == Principal(id="default")


# --- the mutation list itself must not go stale ---------------------------

def test_every_guard_mutation_still_applies():
    """`tools/verify_guards.py` anchors on exact source text.

    When the code it targets is rewritten, the anchor stops matching and the
    mutation silently stops running -- a check that quietly retires is the
    failure mode this whole tool exists to catch, so it must not have it.
    """
    import importlib.util

    path = pathlib.Path(__file__).resolve().parent.parent / "tools" / "verify_guards.py"
    import sys

    spec = importlib.util.spec_from_file_location("verify_guards", path)
    module = importlib.util.module_from_spec(spec)
    # Registered before exec: the dataclass decorator resolves its own module
    # out of sys.modules, and fails on a module that is not there yet.
    sys.modules["verify_guards"] = module
    spec.loader.exec_module(module)

    root = path.parent.parent
    stale = [
        f"{m.name}: anchor gone from {m.file}"
        for m in module.MUTATIONS
        if m.old not in (root / m.file).read_text()
    ]
    assert not stale, "\n  ".join(["stale guard mutations:"] + stale)
