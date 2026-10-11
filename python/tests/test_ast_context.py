"""The ast-outline boundary is typed, bounded, and distrusts exit code zero.

The fake executable records the argv it receives.  That makes the shell-safety
test stronger than a mocked subprocess assertion: attacker-shaped values make
it through as one argument, while no marker command is executed.
"""

import asyncio
import contextlib
import hashlib
import json
import os
import stat as statlib
import textwrap
from pathlib import Path
from types import SimpleNamespace

import pytest

from mini_loop.ast_context import (
    AST_OUTLINE_HOMEPAGE,
    AstContextConfig,
    AstOutlineAdapter,
    _AST_SOURCE_BASENAMES,
    _AST_SOURCE_SUFFIXES,
    _validate_ignore_patterns,
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
            if mode == "nonzero_path":
                print("failed to read " + args[-1], file=sys.stderr)
                raise SystemExit(7)

            command = args[0]
            envelope = {
                "tool": "ast-outline",
                "schema_version": 2 if mode == "bad_schema" else 1,
                "command": command,
                "notes": (
                    ["credential-leaked"]
                    if mode == "env"
                    and (
                        "PROBE_AST_SECRET" in os.environ
                        or "PATH" in os.environ
                    )
                    else (
                        ["credential-scrubbed"]
                        if mode == "env"
                        else (
                            [
                                "tmpdir-contained"
                                if pathlib.Path(os.environ["TMPDIR"]).resolve()
                                == pathlib.Path.cwd().resolve()
                                or pathlib.Path.cwd().resolve()
                                in pathlib.Path(os.environ["TMPDIR"]).resolve().parents
                                else "tmpdir-isolated"
                            ]
                            if mode == "tmpdir"
                            else []
                        )
                    )
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
    monkeypatch.setenv("PATH", str(tmp_path / "model-controlled-bin"))
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
    source, log = fake_ast_outline
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
    assert not log.exists(), "containment must run before the --version probe"


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

    monkeypatch.setattr("mini_loop.ast_context.os.scandir", unexpected_glob)
    recursive = adapter.show_symbol(
        workspace, "linked/**/definitely-missing.py", ["Secret"]
    )
    intermediate = adapter.show_symbol(workspace, "*/secret.py", ["Secret"])

    assert recursive.status == "error"
    assert "recursive glob" in recursive.message
    assert intermediate.status == "error"
    assert "final path component" in intermediate.message
    assert not log.exists(), "path rejection must happen before the executable runs"


def test_glob_is_frozen_to_one_private_snapshot_before_execution(
    tmp_path, fake_ast_outline
):
    executable, log = fake_ast_outline
    workspace = _workspace(tmp_path)
    result = _adapter(executable).show_symbol(workspace, "*.py", ["Thing"])

    assert result.status == "applied"
    calls = [json.loads(line) for line in log.read_text(encoding="utf-8").splitlines()]
    assert calls[0] == ["--version"]
    target = calls[1][calls[1].index("--") + 1]
    assert not any(char in target for char in "*?[")
    assert "mini-loop-ast-" in target
    assert not Path(target).exists(), "the private snapshot must be ephemeral"


def test_glob_symlink_swap_is_rejected_before_probe(
    tmp_path, fake_ast_outline, monkeypatch
):
    executable, log = fake_ast_outline
    workspace = _workspace(tmp_path)
    outside = tmp_path / "outside.py"
    outside.write_text("SECRET\n", encoding="utf-8")
    adapter = _adapter(executable)
    expand = adapter._safe_glob_paths

    def expand_then_swap(root, raw):
        targets = expand(root, raw)
        target = workspace / "x.py"
        target.unlink()
        target.symlink_to(outside)
        return targets

    monkeypatch.setattr(adapter, "_safe_glob_paths", expand_then_swap)
    result = adapter.show_symbol(workspace, "*.py", ["Thing"])

    assert result.status == "error"
    assert not log.exists(), "a swapped target must not reach probe or execution"


def test_glob_static_directory_swap_is_rejected_before_enumeration(
    tmp_path, fake_ast_outline, monkeypatch
):
    executable, log = fake_ast_outline
    workspace = _workspace(tmp_path)
    source = workspace / "src"
    source.mkdir()
    outside = tmp_path / "outside"
    outside.mkdir()
    (outside / "secret.py").write_text("SECRET\n", encoding="utf-8")
    real_open = os.open
    swapped = False

    def swap_then_open(path, flags, *args, **kwargs):
        nonlocal swapped
        if path == "src" and kwargs.get("dir_fd") is not None and not swapped:
            source.rmdir()
            source.symlink_to(outside, target_is_directory=True)
            swapped = True
        return real_open(path, flags, *args, **kwargs)

    monkeypatch.setattr("mini_loop.ast_context.os.open", swap_then_open)
    result = _adapter(executable).show_symbol(workspace, "src/*.py", ["Thing"])

    assert result.status == "error"
    assert swapped
    assert not log.exists(), "a swapped directory must not be enumerated or executed"


def test_multi_match_glob_returns_bounded_ambiguity_without_execution(
    tmp_path, fake_ast_outline
):
    executable, log = fake_ast_outline
    workspace = _workspace(tmp_path)
    (workspace / "y.py").write_text("class Other: pass\n", encoding="utf-8")

    result = _adapter(executable).show_symbol(workspace, "*.py", ["Thing"])

    assert result.status == "ambiguous"
    assert result.data == {
        "matches": ["x.py", "y.py"],
        "total_match_count": 2,
        "truncated": False,
    }
    assert "choose one explicit path" in result.message
    assert not log.exists(), "ambiguity is resolved by the harness, not the child"


def test_ambiguous_glob_projection_has_count_and_byte_bounds(
    tmp_path, fake_ast_outline, monkeypatch
):
    executable, log = fake_ast_outline
    workspace = _workspace(tmp_path)
    adapter = _adapter(executable)
    targets = tuple(
        str(workspace / f"{index:04d}-{'x' * 500}.py")
        for index in range(100)
    )
    monkeypatch.setattr(adapter, "_safe_glob_paths", lambda *_args: targets)

    result = adapter.show_symbol(workspace, "*.py", ["Thing"])

    assert result.status == "ambiguous"
    assert result.data["total_match_count"] == 100
    assert result.data["truncated"] is True
    assert len(result.data["matches"]) <= 32
    assert len(json.dumps(result.data).encode("utf-8")) < 20_000
    assert not log.exists()


@pytest.mark.parametrize("operation", ["repo_map", "file_outline", "symbol_references"])
def test_every_ast_path_is_snapshotted_before_child_execution(
    tmp_path, fake_ast_outline, monkeypatch, operation
):
    executable, log = fake_ast_outline
    workspace = _workspace(tmp_path)
    outside = tmp_path / "outside.py"
    outside.write_text("SECRET\n", encoding="utf-8")
    adapter = _adapter(executable)
    safe_path = adapter._safe_path
    swapped = False

    def resolve_then_swap(root, raw):
        nonlocal swapped
        resolved = safe_path(root, raw)
        if not swapped:
            target = workspace / "x.py"
            target.unlink()
            target.symlink_to(outside)
            swapped = True
        return resolved

    monkeypatch.setattr(adapter, "_safe_path", resolve_then_swap)
    if operation == "repo_map":
        result = adapter.repo_map(workspace, ["x.py"])
    elif operation == "file_outline":
        result = adapter.file_outline(workspace, ["x.py"])
    else:
        result = adapter.symbol_references(workspace, ["Thing"], ["x.py"])

    assert result.status == "error"
    assert not log.exists(), "a swapped target must not reach probe or execution"


def test_recursive_snapshot_skips_directory_symlinks(tmp_path, fake_ast_outline):
    executable, _ = fake_ast_outline
    workspace = _workspace(tmp_path)
    source = workspace / "src"
    source.mkdir()
    (source / "ok.py").write_text("class OK: pass\n", encoding="utf-8")
    outside = tmp_path / "outside.py"
    outside.write_text("SECRET\n", encoding="utf-8")
    (source / "linked.py").symlink_to(outside)
    adapter = _adapter(executable)

    with adapter._snapshot_workspace_paths(
        workspace.resolve(), (source.resolve(),)
    ) as frozen:
        paths, _ = frozen
        snapshot = paths[0]
        assert (snapshot / "ok.py").is_file()
        assert not (snapshot / "linked.py").exists()


def test_directory_snapshot_covers_the_pinned_1_9_language_contract(
    tmp_path, fake_ast_outline
):
    executable, _ = fake_ast_outline
    workspace = _workspace(tmp_path)
    source = workspace / "languages"
    source.mkdir()
    expected_suffixes = frozenset(
        {
            ".c++", ".cc", ".cjs", ".cpp", ".cppm", ".cs", ".css",
            ".cxx", ".ex", ".exs", ".gd", ".gemspec", ".go", ".h",
            ".h++", ".hh", ".hpp", ".htm", ".html", ".hxx", ".inl",
            ".ipp", ".ixx", ".java", ".js", ".jsx", ".kt", ".kts",
            ".lua", ".markdown", ".md", ".mdown", ".mdx", ".mjs",
            ".php", ".php8", ".phps", ".phtml", ".py", ".pyi",
            ".rake", ".rb", ".rs", ".ru", ".sc", ".scala", ".scss",
            ".sql", ".swift", ".tpp", ".ts", ".tsx", ".vue", ".wlua",
            ".yaml", ".yml",
        }
    )
    assert _AST_SOURCE_SUFFIXES == expected_suffixes
    assert _AST_SOURCE_BASENAMES == frozenset({"Gemfile", "Rakefile"})
    expected_names = set(_AST_SOURCE_BASENAMES)
    for index, suffix in enumerate(sorted(_AST_SOURCE_SUFFIXES)):
        name = f"sample_{index}{suffix}"
        (source / name).write_text("source\n", encoding="utf-8")
        expected_names.add(name)
    for name in _AST_SOURCE_BASENAMES:
        (source / name).write_text("source\n", encoding="utf-8")
    (source / "unsupported.txt").write_text("skip\n", encoding="utf-8")

    with _adapter(executable)._snapshot_workspace_paths(
        workspace.resolve(), (source.resolve(),)
    ) as frozen:
        snapshot, _ = frozen
        copied_names = {path.name for path in snapshot[0].iterdir() if path.is_file()}

    assert copied_names == expected_names


def test_directory_snapshot_applies_root_and_nested_ignore_frames_before_caps(
    tmp_path, fake_ast_outline, monkeypatch
):
    executable, _ = fake_ast_outline
    workspace = _workspace(tmp_path)
    (workspace / ".git").mkdir()
    (workspace / ".gitignore").write_text(
        "generated/\n*.swift\n", encoding="utf-8"
    )
    root_ignore = (
        "!visible.swift\n"
        "!node_modules/\n"
        "node_modules/*/\n"
        "!node_modules/our-fork/\n"
    )
    (workspace / ".ignore").write_text(root_ignore, encoding="utf-8")
    (workspace / "visible.swift").write_text("struct Visible {}\n", encoding="utf-8")
    generated = workspace / "generated"
    generated.mkdir()
    (generated / "hidden.py").write_text("X" * 10_000, encoding="utf-8")
    source = workspace / "src"
    source.mkdir()
    (source / ".gitignore").write_text("ignored.py\n*.vue\n", encoding="utf-8")
    (source / ".ignore").write_text("!App.vue\n", encoding="utf-8")
    (source / "ignored.py").write_text("SECRET\n", encoding="utf-8")
    (source / "App.vue").write_text("<template/>\n", encoding="utf-8")
    kept_vendor = workspace / "node_modules" / "our-fork"
    kept_vendor.mkdir(parents=True)
    (kept_vendor / "kept.ts").write_text("export {}\n", encoding="utf-8")
    ignored_vendor = workspace / "node_modules" / "other"
    ignored_vendor.mkdir()
    (ignored_vendor / "hidden.ts").write_text("SECRET\n", encoding="utf-8")
    monkeypatch.setattr("mini_loop.ast_context._MAX_SNAPSHOT_BYTES", 512)
    adapter = _adapter(executable)

    with adapter._snapshot_workspace_paths(
        workspace.resolve(), (workspace.resolve(),)
    ) as frozen:
        snapshot, _ = frozen
        root = snapshot[0]
        assert (root / ".git").is_dir()
        assert (root / ".gitignore").read_text(encoding="utf-8") == (
            "generated/\n*.swift\n"
        )
        assert not (root / "generated").exists()
        assert (root / "visible.swift").is_file()
        assert not (root / "src" / "ignored.py").exists()
        assert (root / "src" / "App.vue").is_file()
        assert (root / "node_modules" / "our-fork" / "kept.ts").is_file()
        assert not (root / "node_modules" / "other").exists()

    explicit = adapter.file_outline(workspace, ["generated/hidden.py"])
    assert explicit.status == "error"
    assert "exceeds 512 bytes" in explicit.message


def test_explicit_nested_candidates_override_a_shallow_directory_filter(
    tmp_path, fake_ast_outline
):
    executable, _ = fake_ast_outline
    workspace = _workspace(tmp_path)
    (workspace / ".git").mkdir()
    source = workspace / "src"
    source.mkdir()
    (source / ".gitignore").write_text(
        "generated.py\nignored-dir/\n", encoding="utf-8"
    )
    generated = source / "generated.py"
    generated.write_text("GENERATED\n", encoding="utf-8")
    ignored_directory = source / "ignored-dir"
    ignored_directory.mkdir()
    (ignored_directory / "kept.ts").write_text("export {}\n", encoding="utf-8")
    adapter = _adapter(executable)

    with adapter._snapshot_workspace_paths(
        workspace.resolve(),
        (source.resolve(), generated.resolve(), ignored_directory.resolve()),
    ) as frozen:
        snapshots, _ = frozen
        source_snapshot, generated_snapshot, directory_snapshot = snapshots
        assert generated_snapshot.is_file()
        assert directory_snapshot.joinpath("kept.ts").is_file()
        assert source_snapshot.joinpath("generated.py").is_file()
        assert source_snapshot.joinpath("ignored-dir", "kept.ts").is_file()


@pytest.mark.parametrize(
    "pattern, message",
    [
        ("*a*a*a*ab", "variable-star groups"),
        ("**/one/**/two/**/three", "variable-star groups"),
        ("**/*a*a/*a*a/**/b", "variable-star groups"),
    ],
)
def test_ignore_patterns_reject_backtracking_shapes_before_probe(
    tmp_path, fake_ast_outline, pattern, message
):
    executable, log = fake_ast_outline
    workspace = _workspace(tmp_path)
    (workspace / ".git").mkdir()
    (workspace / ".gitignore").write_text(pattern + "\n", encoding="utf-8")
    (workspace / ("a" * 200 + "x.py")).write_text("source\n", encoding="utf-8")

    result = _adapter(executable).repo_map(workspace, ["."])

    assert result.status == "error"
    assert message in result.message
    assert not log.exists(), "unsafe ignore regex must be rejected before execution"


def test_ignore_pattern_guard_keeps_common_two_group_gitwildmatch_shapes():
    _validate_ignore_patterns(["**/*.py", "foo/**/bar/**", "*generated*.ts"])


def test_workspace_tmpdir_cannot_capture_or_recurse_into_snapshots(
    tmp_path, fake_ast_outline, monkeypatch
):
    executable, _ = fake_ast_outline
    workspace = _workspace(tmp_path)
    monkeypatch.setenv("TMPDIR", str(workspace))
    monkeypatch.setenv("FAKE_AST_MODE", "tmpdir")
    monkeypatch.setattr("mini_loop.ast_context.tempfile.tempdir", None)

    result = _adapter(executable).repo_map(workspace, ["."])

    assert result.status == "applied"
    assert "tmpdir-isolated" in result.warnings
    assert "tmpdir-contained" not in result.warnings
    assert not list(workspace.glob("mini-loop-ast-*"))


def test_snapshot_paths_are_rewritten_in_process_errors(
    tmp_path, fake_ast_outline, monkeypatch
):
    executable, _ = fake_ast_outline
    workspace = _workspace(tmp_path)
    monkeypatch.setenv("FAKE_AST_MODE", "nonzero_path")

    result = _adapter(executable).file_outline(workspace, ["x.py"])

    assert result.status == "error"
    assert str(workspace / "x.py") in result.message
    assert "mini-loop-ast-" not in result.message


@pytest.mark.skipif(
    not hasattr(os, "mkfifo") or not hasattr(os, "O_NONBLOCK"),
    reason="POSIX FIFO test",
)
def test_snapshot_open_requests_nonblocking_before_regular_file_check(
    tmp_path, fake_ast_outline, monkeypatch
):
    executable, log = fake_ast_outline
    workspace = _workspace(tmp_path)
    fifo = workspace / "fifo.py"
    os.mkfifo(fifo)
    adapter = _adapter(executable)
    real_stat = os.stat
    real_open = os.open

    def stat_as_regular(path, *args, **kwargs):
        if path == "fifo.py" and kwargs.get("dir_fd") is not None:
            return SimpleNamespace(st_mode=statlib.S_IFREG | 0o600)
        return real_stat(path, *args, **kwargs)

    def require_nonblocking(path, flags, *args, **kwargs):
        if path == "fifo.py":
            assert flags & os.O_NONBLOCK
            raise OSError("test stopped before opening the FIFO")
        return real_open(path, flags, *args, **kwargs)

    monkeypatch.setattr("mini_loop.ast_context.os.stat", stat_as_regular)
    monkeypatch.setattr("mini_loop.ast_context.os.open", require_nonblocking)
    result = adapter.show_symbol(workspace, "fifo.py", ["Thing"])

    assert result.status == "error"
    assert not log.exists()


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

    entries = (
        SimpleNamespace(
            name=f"{index}.py",
            is_file=lambda **_kwargs: True,
        )
        for index in range(10_001)
    )
    monkeypatch.setattr(
        "mini_loop.ast_context.os.scandir",
        lambda *_args, **_kwargs: contextlib.nullcontext(entries),
    )
    expanded = adapter.show_symbol(workspace, "*.py", ["Thing"])
    assert expanded.status == "error"
    assert "more than 10000 entries" in expanded.message


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
