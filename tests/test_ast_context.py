"""The ast-outline boundary is typed, bounded, and distrusts exit code zero.

The fake executable records the argv it receives.  That makes the shell-safety
test stronger than a mocked subprocess assertion: attacker-shaped values make
it through as one argument, while no marker command is executed.
"""

import asyncio
import hashlib
import json
import textwrap
from pathlib import Path

import pytest

from mini_loop.ast_context import (
    AST_OUTLINE_HOMEPAGE,
    AstContextConfig,
    AstOutlineAdapter,
    install_ast_context_tools,
)
from mini_loop.registry import ToolContext, ToolRegistry
from mini_loop.tool_policy import DEFAULT_ROLE_TOOL_POLICY


@pytest.fixture
def fake_ast_outline(tmp_path, monkeypatch):
    executable = tmp_path / "ast-outline"
    log = tmp_path / "argv.jsonl"
    executable.write_text(
        textwrap.dedent(
            """\
            #!/usr/bin/env python3
            import json
            import os
            import pathlib
            import sys
            import time

            args = sys.argv[1:]
            log = os.environ.get("FAKE_AST_LOG")
            if log:
                with pathlib.Path(log).open("a", encoding="utf-8") as stream:
                    stream.write(json.dumps(args) + "\\n")

            if args == ["--version"]:
                print("ast-outline " + os.environ.get("FAKE_AST_VERSION", "1.9.0"))
                homepage = os.environ.get(
                    "FAKE_AST_HOMEPAGE",
                    "https://github.com/ast-outline/ast-outline",
                )
                if homepage:
                    print("homepage: " + homepage)
                raise SystemExit(0)

            mode = os.environ.get("FAKE_AST_MODE", "applied")
            if mode == "sleep":
                time.sleep(30)
            if mode == "closed_sleep":
                os.close(1)
                os.close(2)
                time.sleep(30)
            if mode == "huge":
                print("X" * 100_000)
                raise SystemExit(0)
            if mode == "invalid_json":
                print("not json")
                raise SystemExit(0)
            if mode == "nonzero":
                print("internal crash", file=sys.stderr)
                raise SystemExit(7)

            command = args[0]
            envelope = {
                "tool": "ast-outline",
                "schema_version": 2 if mode == "bad_schema" else 1,
                "command": command,
                "notes": (
                    ["credential-leaked"]
                    if mode == "env" and "PROBE_AST_SECRET" in os.environ
                    else (["credential-scrubbed"] if mode == "env" else [])
                ),
            }
            if mode == "error":
                envelope["error"] = {
                    "notes": ["path not found"],
                    "hint": "check the workspace path",
                }
            elif command == "show":
                if mode == "no_match":
                    envelope["results"] = [{"query": "Missing", "matches": []}]
                elif mode == "ambiguous":
                    envelope["results"] = [{
                        "query": "Thing",
                        "ambiguous": True,
                        "matches": [
                            {"file": "a.py", "qualified_name": "a.Thing"},
                            {"file": "b.py", "qualified_name": "b.Thing"},
                        ],
                    }]
                else:
                    envelope["results"] = [{
                        "query": "Thing",
                        "matches": [{"qualified_name": "Thing", "source": "class Thing: pass"}],
                    }]
            elif command == "grep":
                total = 0 if mode == "no_match" else 1
                envelope["summary"] = {
                    "files_scanned": 1,
                    "total_matches": total,
                    "truncated_count": 0,
                }
                envelope["files"] = [] if total == 0 else [{"path": "x.py", "matches": [{"line": 1}]}]
            else:
                file_result = {
                    "path": "x.py",
                    "error_count": 0,
                    "imports": ["import json"],
                    "conditional_imports_count": 0,
                    "import_regions": [{"start": 0, "end": 11}],
                    "declarations": [{"kind": "class", "name": "Thing"}],
                }
                if mode == "partial":
                    file_result["error_count"] = 2
                    file_result["parse_errors"] = [{"line": 4}]
                envelope["files"] = [file_result]
            print(json.dumps(envelope))
            """
        ),
        encoding="utf-8",
    )
    executable.chmod(0o755)
    monkeypatch.setenv("FAKE_AST_LOG", str(log))
    return executable, log


def _adapter(executable, **kwargs):
    return AstOutlineAdapter(
        AstContextConfig(
            binary=executable,
            env_passthrough=(
                "FAKE_AST_LOG",
                "FAKE_AST_VERSION",
                "FAKE_AST_HOMEPAGE",
                "FAKE_AST_MODE",
            ),
            **kwargs,
        )
    )


def _workspace(tmp_path):
    workspace = tmp_path / "workspace"
    workspace.mkdir(exist_ok=True)
    (workspace / "x.py").write_text("class Thing: pass\n", encoding="utf-8")
    return workspace


def _ctx(workspace):
    return ToolContext(agent=None, workspace=workspace, state={}, call=None)


def test_probe_pins_version_homepage_and_schema_one(tmp_path, fake_ast_outline):
    executable, _ = fake_ast_outline
    probe = _adapter(executable).probe()
    assert probe.status == "applied"
    assert probe.version == "1.9.0"
    assert probe.homepage == AST_OUTLINE_HOMEPAGE
    assert probe.schema_version == 1


def test_sidecar_environment_is_allowlisted_not_inherited(
    tmp_path, fake_ast_outline, monkeypatch
):
    executable, _ = fake_ast_outline
    monkeypatch.setenv("PROBE_AST_SECRET", "sk-do-not-inherit-123456")
    monkeypatch.setenv("FAKE_AST_MODE", "env")

    result = _adapter(executable).repo_map(_workspace(tmp_path), ["x.py"])

    assert result.status == "applied"
    assert "credential-scrubbed" in result.warnings
    assert "credential-leaked" not in result.warnings


@pytest.mark.parametrize(
    ("env_name", "value"),
    [
        ("FAKE_AST_VERSION", "1.8.9"),
        ("FAKE_AST_VERSION", "1.10.0"),
        ("FAKE_AST_HOMEPAGE", "https://github.com/aeroxy/ast-bro"),
        ("FAKE_AST_HOMEPAGE", ""),
    ],
)
def test_wrong_version_or_homepage_is_incompatible(
    tmp_path, fake_ast_outline, monkeypatch, env_name, value
):
    executable, _ = fake_ast_outline
    monkeypatch.setenv(env_name, value)
    assert _adapter(executable).probe().status == "incompatible"


def test_missing_binary_is_a_typed_status(tmp_path):
    workspace = _workspace(tmp_path)
    result = _adapter(tmp_path / "does-not-exist").repo_map(workspace, ["."])
    assert result.status == "missing"
    assert "not found" in result.message


def test_binary_digest_is_rechecked_before_every_execution(
    tmp_path, fake_ast_outline
):
    executable, _ = fake_ast_outline
    expected = hashlib.sha256(executable.read_bytes()).hexdigest()
    adapter = _adapter(executable, expected_sha256=expected)

    assert adapter.probe().status == "applied"
    executable.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
    executable.chmod(0o755)

    assert adapter.probe().status == "incompatible"


def test_workspace_writable_binary_is_never_executed(tmp_path, fake_ast_outline):
    source, _ = fake_ast_outline
    workspace = _workspace(tmp_path)
    executable = workspace / "ast-outline"
    executable.write_bytes(source.read_bytes())
    executable.chmod(0o755)
    expected = hashlib.sha256(executable.read_bytes()).hexdigest()

    result = _adapter(
        executable,
        expected_sha256=expected,
    ).repo_map(workspace, ["x.py"])

    assert result.status == "incompatible"
    assert "outside the model-visible workspace" in result.message


def test_exit_zero_json_error_is_not_treated_as_success(
    tmp_path, fake_ast_outline, monkeypatch
):
    executable, _ = fake_ast_outline
    monkeypatch.setenv("FAKE_AST_MODE", "error")
    result = _adapter(executable).repo_map(_workspace(tmp_path), ["."])
    assert result.status == "error"
    assert "path not found" in result.message
    assert result.data["error"]["hint"] == "check the workspace path"


def test_no_match_is_distinct_from_error(tmp_path, fake_ast_outline, monkeypatch):
    executable, _ = fake_ast_outline
    monkeypatch.setenv("FAKE_AST_MODE", "no_match")
    result = _adapter(executable).symbol_references(
        _workspace(tmp_path), ["DefinitelyMissing"], ["."]
    )
    assert result.status == "no_match"
    assert result.data["summary"]["total_matches"] == 0


def test_error_count_and_parse_errors_make_the_result_partial(
    tmp_path, fake_ast_outline, monkeypatch
):
    executable, _ = fake_ast_outline
    monkeypatch.setenv("FAKE_AST_MODE", "partial")
    result = _adapter(executable).file_outline(_workspace(tmp_path), ["x.py"])
    assert result.status == "partial"
    assert "ast_parse_errors:3" in result.warnings


def test_include_imports_controls_the_model_visible_json(
    tmp_path, fake_ast_outline
):
    executable, _ = fake_ast_outline
    workspace = _workspace(tmp_path)
    adapter = _adapter(executable)

    hidden = adapter.file_outline(workspace, ["x.py"], include_imports=False)
    shown = adapter.file_outline(workspace, ["x.py"], include_imports=True)

    assert "imports" not in hidden.data["files"][0]
    assert shown.data["files"][0]["imports"] == ["import json"]


def test_ambiguous_symbol_has_its_own_status(
    tmp_path, fake_ast_outline, monkeypatch
):
    executable, _ = fake_ast_outline
    monkeypatch.setenv("FAKE_AST_MODE", "ambiguous")
    result = _adapter(executable).show_symbol(
        _workspace(tmp_path), "x.py", ["Thing"]
    )
    assert result.status == "ambiguous"
    assert len(result.data["results"][0]["matches"]) == 2


def test_schema_mismatch_is_incompatible(tmp_path, fake_ast_outline, monkeypatch):
    executable, _ = fake_ast_outline
    monkeypatch.setenv("FAKE_AST_MODE", "bad_schema")
    result = _adapter(executable).repo_map(_workspace(tmp_path), ["."])
    assert result.status == "incompatible"
    assert "schema_version=1" in result.message


def test_user_values_are_one_argv_and_never_reparsed_by_a_shell(
    tmp_path, fake_ast_outline
):
    executable, log = fake_ast_outline
    workspace = _workspace(tmp_path)
    marker = tmp_path / "shell-injection-ran"
    malicious = f"Thing; touch {marker}"

    result = _adapter(executable).show_symbol(workspace, "x.py", [malicious])

    assert result.status == "applied"
    assert not marker.exists()
    calls = [json.loads(line) for line in log.read_text(encoding="utf-8").splitlines()]
    assert calls[0] == ["--version"]
    assert malicious in calls[1]
    assert calls[1].count(malicious) == 1


def test_realpath_and_glob_matches_cannot_escape_workspace(
    tmp_path, fake_ast_outline
):
    executable, log = fake_ast_outline
    workspace = _workspace(tmp_path)
    outside = tmp_path / "outside"
    outside.mkdir()
    (outside / "secret.py").write_text("SECRET\n", encoding="utf-8")
    (workspace / "linked").symlink_to(outside, target_is_directory=True)
    adapter = _adapter(executable)

    exact = adapter.show_symbol(workspace, "linked/secret.py", ["Secret"])
    wildcard = adapter.show_symbol(workspace, "linked/*.py", ["Secret"])

    assert exact.status == "error"
    assert wildcard.status == "error"
    assert "outside workspace" in exact.message
    assert "outside workspace" in wildcard.message
    assert not log.exists(), "path rejection must happen before the executable runs"


def test_recursive_and_intermediate_globs_are_rejected_without_traversal(
    tmp_path, fake_ast_outline, monkeypatch
):
    executable, log = fake_ast_outline
    workspace = _workspace(tmp_path)
    outside = tmp_path / "outside"
    outside.mkdir()
    (workspace / "linked").symlink_to(outside, target_is_directory=True)
    adapter = _adapter(executable)

    def unexpected_glob(*_args, **_kwargs):
        raise AssertionError("rejected patterns must not start filesystem expansion")

    monkeypatch.setattr("mini_loop.ast_context.globlib.iglob", unexpected_glob)
    recursive = adapter.show_symbol(
        workspace, "linked/**/definitely-missing.py", ["Secret"]
    )
    intermediate = adapter.show_symbol(workspace, "*/secret.py", ["Secret"])

    assert recursive.status == "error"
    assert "recursive glob" in recursive.message
    assert intermediate.status == "error"
    assert "final path component" in intermediate.message
    assert not log.exists(), "path rejection must happen before the executable runs"


def test_model_controlled_arrays_strings_and_glob_expansion_are_bounded(
    tmp_path, fake_ast_outline, monkeypatch
):
    executable, _ = fake_ast_outline
    adapter = _adapter(executable)
    workspace = _workspace(tmp_path)

    too_many = adapter.repo_map(workspace, ["x.py"] * 65)
    too_long = adapter.show_symbol(workspace, "x.py", ["x" * 2_049])
    assert too_many.status == "error" and "at most 64" in too_many.message
    assert too_long.status == "error" and "at most 2048" in too_long.message

    monkeypatch.setattr(
        "mini_loop.ast_context.globlib.iglob",
        lambda *_args, **_kwargs: (
            str(workspace / "x.py") for _ in range(10_001)
        ),
    )
    expanded = adapter.show_symbol(workspace, "*.py", ["Thing"])
    assert expanded.status == "error"
    assert "more than 10000" in expanded.message


def test_timeout_and_output_caps_are_errors(
    tmp_path, fake_ast_outline, monkeypatch
):
    executable, _ = fake_ast_outline
    workspace = _workspace(tmp_path)

    monkeypatch.setenv("FAKE_AST_MODE", "huge")
    too_large = _adapter(executable, max_output_bytes=256).repo_map(workspace, ["."])
    assert too_large.status == "error"
    assert "exceeded 256 bytes" in too_large.message

    monkeypatch.setenv("FAKE_AST_MODE", "sleep")
    too_slow = _adapter(executable, timeout_seconds=0.1).repo_map(workspace, ["."])
    assert too_slow.status == "error"
    assert "timed out" in too_slow.message

    monkeypatch.setenv("FAKE_AST_MODE", "closed_sleep")
    closed_but_alive = _adapter(executable, timeout_seconds=0.1).repo_map(
        workspace, ["."]
    )
    assert closed_but_alive.status == "error"
    assert "timed out" in closed_but_alive.message


def test_install_registers_four_typed_parallel_read_tools(
    tmp_path, fake_ast_outline
):
    executable, _ = fake_ast_outline
    registry = install_ast_context_tools(
        ToolRegistry(), AstContextConfig(binary=executable)
    )
    expected = {"repo_map", "file_outline", "show_symbol", "symbol_references"}
    assert set(registry.names()) == expected
    capabilities = {
        "repo_map": frozenset({"repo.semantic_outline"}),
        "file_outline": frozenset({"repo.semantic_outline"}),
        "show_symbol": frozenset({"repo.symbol"}),
        "symbol_references": frozenset({"repo.references"}),
    }
    for name in expected:
        tool = registry.get(name)
        assert tool.readonly is True
        assert tool.parallel_safe is True
        assert tool.risk == "read"
        assert tool.capabilities == capabilities[name]
        assert tool.input_schema["additionalProperties"] is False

    raw = asyncio.run(
        registry.get("repo_map").run(_ctx(_workspace(tmp_path)), paths=["."])
    )
    returned = json.loads(raw)
    assert returned["status"] == "applied"
    assert returned["probe"]["schema_version"] == 1


def test_explore_role_inherits_all_ast_context_capabilities(fake_ast_outline):
    executable, _ = fake_ast_outline
    parent = install_ast_context_tools(
        ToolRegistry(), AstContextConfig(binary=executable)
    )

    child = DEFAULT_ROLE_TOOL_POLICY.select("Explore", parent)

    assert set(child.names()) == {
        "repo_map",
        "file_outline",
        "show_symbol",
        "symbol_references",
    }
