"""Actual public string-only TokenAuth/load_auth observations, without HTTP."""

from __future__ import annotations

import hashlib
import hmac
import json
import os
from pathlib import Path
import platform
from unittest.mock import patch


def public_auth_contracts() -> dict:
    from mini_loop import TokenAuth, load_auth

    def binding(token, principal):
        return dict(token=token, principal=principal)

    def constructor(name, bindings, headers, after_bindings=None):
        return dict(name=name, bindings=bindings, headers=headers, after_bindings=after_bindings)

    def environment(name, environ, headers, *, ambient=None, after_environ=None, after_ambient=None):
        return dict(name=name, environ=environ, ambient=ambient or {}, headers=headers,
                    after_environ=after_environ, after_ambient=after_ambient)

    basic = [None, "", "Bearer x", "Bearer missing"]
    constructor_recipes = [
        constructor("empty", [], basic),
        constructor("empty-token", [binding("", "owner")], basic),
        constructor("empty-principal", [binding("x", "")], basic),
        constructor("ascii-principal-set", [binding("z", "bob"), binding("x", "alice"), binding("y", "alice")],
                    [None, "Bearer x", "Bearer y", "Bearer z", "Bearer missing"]),
        constructor("literal-principals", [binding("x", "用户"), binding("y", " é "), binding("z", "e\u0301"), binding("w", "\x1c")],
                    ["Bearer x", "Bearer y", "Bearer z", "Bearer w"]),
        constructor("space-token", [binding(" ", " space owner ")], [None, "Bearer", "Bearer ", "Bearer  ", "Bearer   "]),
        constructor("control-token", [binding("a\x00\tb", "literal")], ["Bearer a\x00\tb", "Bearer a\tb", "Bearer a"]),
        constructor("unicode-token-first", [binding("中", "unicode"), binding("x", "ascii")], [None, "Basic x", "Bearer x", "Bearer 中"]),
        constructor("unicode-token-last", [binding("x", "ascii"), binding("中", "unicode")], [None, "Bearer ", "Bearer x", "Bearer other"]),
        constructor("unicode-only", [binding("é", "owner")], [None, "", "Bearer", "Bearer ", "Bearer é", "Bearer x"]),
        constructor("bearer-shape", [binding("x", "owner")],
                    [None, "", "bearer x", "BEARER x", "bEaReR x", " Bearer x", "Bearer\tx", "Bearer  x", "Bearer x ", "Basic x", "Ｂearer x", "Bearer 中"]),
        constructor("detached-input", [binding("x", "before")], ["Bearer x", "Bearer y"],
                    [binding("x", "after"), binding("y", "new")]),
    ]
    environment_recipes = [
        environment("explicit-empty-isolates-ambient", {}, basic, ambient={"MINILOOP_API_TOKEN": "ambient"}),
        environment("nil-reads-ambient-single", None, ["Bearer ambient", "Bearer x"], ambient={"MINILOOP_API_TOKEN": "ambient"}),
        environment("nil-reads-ambient-multi", None, basic, ambient={"MINILOOP_API_TOKENS": "alice:x,bob:y"}),
        environment("single-ascii-strip", {"MINILOOP_API_TOKEN": " \tx\r\n"}, basic),
        environment("single-python-strip", {"MINILOOP_API_TOKEN": "\x1c\x1d\x1ex\x1f"}, basic),
        environment("single-python-whitespace-only", {"MINILOOP_API_TOKEN": "\x1c\x1d\x1e\x1f"}, basic),
        environment("multi-priority", {"MINILOOP_API_TOKENS": "alice:x", "MINILOOP_API_TOKEN": "single"}, ["Bearer x", "Bearer single"]),
        environment("multi-python-empty-falls-back", {"MINILOOP_API_TOKENS": "\x1c\x1d\x1e\x1f", "MINILOOP_API_TOKEN": "x"}, basic),
        environment("multi-python-entry-strip", {"MINILOOP_API_TOKENS": "\x1calice:x\x1d,\x1ebob:y\x1f"}, ["Bearer x", "Bearer y"]),
        environment("multi-no-inner-strip", {"MINILOOP_API_TOKENS": " alice : x "}, ["Bearer x", "Bearer  x"]),
        environment("token-colon-preserved", {"MINILOOP_API_TOKENS": "alice:x:y"}, ["Bearer x:y", "Bearer x"]),
        environment("empty-entries", {"MINILOOP_API_TOKENS": ", ,alice:x,,"}, basic),
        environment("commas-only", {"MINILOOP_API_TOKENS": ", ,", "MINILOOP_API_TOKEN": "fallback"}, basic),
        environment("missing-colon", {"MINILOOP_API_TOKENS": "alice"}, basic),
        environment("empty-token", {"MINILOOP_API_TOKENS": "alice:"}, basic),
        environment("empty-owner", {"MINILOOP_API_TOKENS": ":x"}, basic),
        environment("duplicate-same-owner", {"MINILOOP_API_TOKENS": "alice:x,alice:x,alice:y"}, ["Bearer x", "Bearer y"]),
        environment("duplicate-different-owner", {"MINILOOP_API_TOKENS": "alice:x,bob:x"}, basic),
        environment("duplicate-principal-repr", {"MINILOOP_API_TOKENS": "O'Brien:x,用户\t\"Q\":x"}, basic),
        environment("duplicate-before-empty-owner-validation", {"MINILOOP_API_TOKENS": "alice:x,:x"}, basic),
        environment("later-duplicate-before-earlier-empty-owner", {"MINILOOP_API_TOKENS": ":empty,alice:x,bob:x"}, basic),
        environment("unicode-token-comparison", {"MINILOOP_API_TOKENS": "alice:x,用户:中"}, [None, "Basic x", "Bearer x", "Bearer 中"]),
        environment("explicit-input-snapshot", {"MINILOOP_API_TOKEN": "x"}, ["Bearer x", "Bearer y"],
                    after_environ={"MINILOOP_API_TOKEN": "y"}),
        environment("ambient-input-snapshot", None, ["Bearer x", "Bearer y"], ambient={"MINILOOP_API_TOKEN": "x"},
                    after_ambient={"MINILOOP_API_TOKEN": "y"}),
    ]

    def failure(error):
        return dict(exception_class=type(error).__name__, message=str(error))

    def observe(auth, headers):
        observations = []
        for header in headers:
            try:
                principal = auth.authenticate(header)
                observations.append(dict(header=header,
                    principal=None if principal is None else dict(id=principal.id, anonymous=principal.anonymous),
                    error=None))
            except Exception as error:
                observations.append(dict(header=header, principal=None, error=failure(error)))
        return dict(auth_class=type(auth).__name__, configured=auth.configured,
                    principals=list(auth.principals()) if isinstance(auth, TokenAuth) else None,
                    observations=observations)

    constructors, environments = [], []
    for recipe in constructor_recipes:
        tokens = {row["token"]: row["principal"] for row in recipe["bindings"]}
        # A public Mapping has unique keys. Native sequence duplicate admission
        # is a separate declared adapter rule, not inferred from dict coercion.
        assert len(tokens) == len(recipe["bindings"])
        row = dict(recipe=recipe, construction_error=None, before=None, after=None)
        try:
            auth = TokenAuth(tokens)
            row["before"] = observe(auth, recipe["headers"])
            if recipe["after_bindings"] is not None:
                tokens.clear()
                tokens.update((entry["token"], entry["principal"]) for entry in recipe["after_bindings"])
                row["after"] = observe(auth, recipe["headers"])
        except Exception as error:
            row["construction_error"] = failure(error)
        constructors.append(row)
    for recipe in environment_recipes:
        environ = None if recipe["environ"] is None else dict(recipe["environ"])
        row = dict(recipe=recipe, construction_error=None, before=None, after=None)
        with patch.dict(os.environ, recipe["ambient"], clear=True):
            try:
                auth = load_auth(environ)
                row["before"] = observe(auth, recipe["headers"])
                if recipe["after_environ"] is not None:
                    environ.clear()
                    environ.update(recipe["after_environ"])
                if recipe["after_ambient"] is not None:
                    os.environ.clear()
                    os.environ.update(recipe["after_ambient"])
                if recipe["after_environ"] is not None or recipe["after_ambient"] is not None:
                    row["after"] = observe(auth, recipe["headers"])
            except Exception as error:
                row["construction_error"] = failure(error)
        environments.append(row)

    package = Path(__file__).resolve().parents[1] / "mini_loop"
    result = dict(constructors=constructors, environments=environments,
        python_version=platform.python_version(),
        scope=[
            "Actual root-public TokenAuth and load_auth; finite scalar Unicode strings, unique Mapping keys and nullable string authorization only.",
            "Each authentication observation calls the Source method; TypeError is retained as a failed operation, never converted to rejection or a principal.",
            "Explicit environment maps isolate ambient values; None reads the scoped real process environment. Construction snapshots are observed after caller input mutation.",
            "Source string compare_digest rejects non-ASCII at comparison, even after an earlier matching binding. Principal IDs remain literal and receive no normalization.",
            "No HTTP/server/model/tool call, arbitrary Python object coercion, lone-surrogate string or all-environment acceptance claim.",
            "The Go compatibility bool API projects acceptance; its named AuthenticationResult is required to preserve the Source fault distinction. Native duplicate sequence conflict refusal remains a declared typed adapter rule.",
        ],
        source_sha256={"mini_loop/auth.py": hashlib.sha256((package / "auth.py").read_bytes()).hexdigest(),
                       "mini_loop/__init__.py": hashlib.sha256((package / "__init__.py").read_bytes()).hexdigest(),
                       "stdlib/hmac.py": hashlib.sha256(Path(hmac.__file__).read_bytes()).hexdigest()},
        producer_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest())
    json.dumps(result, allow_nan=False)
    return result
