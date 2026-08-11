"""Typed, read-only ast-outline tools for structural code context.

The adapter deliberately stays outside the default registry.  An operator opts
in by constructing :class:`AstContextConfig` and calling
``install_ast_context_tools`` on the registry they want to extend.

Security properties live at this boundary rather than in prompt guidance:

* the configured executable is invoked with a fixed argv and ``shell=False``;
* every path is resolved beneath the current ``ToolContext.workspace``;
* subprocess time and captured output are bounded;
* the binary identity, supported 1.9.x version, homepage, and JSON schema are
  checked before model-visible output is accepted;
* ast-outline's exit-zero user errors are detected from the JSON envelope.
"""

from __future__ import annotations

import asyncio
import contextlib
import glob as globlib
import hashlib
import hmac
import json
import os
import re
import shutil
import signal
import subprocess
import threading
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Iterable

from .registry import Tool, ToolContext, ToolRegistry


AST_OUTLINE_HOMEPAGE = "https://github.com/ast-outline/ast-outline"
AST_OUTLINE_SCHEMA_VERSION = 1

_VERSION_RE = re.compile(
    r"^ast-outline\s+(?P<version>\d+\.\d+\.\d+(?:[-+][^\s]+)?)\s*$",
    re.MULTILINE,
)
_HOMEPAGE_RE = re.compile(r"^homepage:\s*(?P<homepage>\S+)\s*$", re.MULTILINE)
_GLOB_CHARS = frozenset("*?[")
_RESULT_STATUSES = frozenset(
    {"applied", "no_match", "partial", "ambiguous", "error", "missing", "incompatible"}
)
_REFERENCE_KINDS = frozenset({"def", "call", "ref", "import", "comment", "string"})
_OUTLINE_VIEWS = frozenset({"full", "public", "minimal"})
_DIGEST_DENSITIES = frozenset({"names", "compact", "default", "wide"})
_SAFE_ENV_NAMES = ("PATH", "LANG", "LC_ALL", "TMPDIR", "SYSTEMROOT")
_MAX_INPUT_ITEMS = 64
_MAX_INPUT_CHARS = 2_048
_MAX_GLOB_MATCHES = 10_000


def _version_tuple(version: str) -> tuple[int, int, int]:
    match = re.match(r"^(\d+)\.(\d+)\.(\d+)", version)
    if match is None:
        raise ValueError(f"invalid semantic version: {version!r}")
    return tuple(int(part) for part in match.groups())


@dataclass(frozen=True)
class AstContextConfig:
    """Operator-controlled ast-outline process contract.

    ``1.9.x`` is intentionally narrow.  A future release can change its JSON
    or matching semantics without changing this package, so widening the range
    should be an explicit code/config review rather than an automatic upgrade.
    """

    binary: str | Path = "ast-outline"
    timeout_seconds: float = 15.0
    max_output_bytes: int = 2_000_000
    min_version: str = "1.9.0"
    max_version_exclusive: str = "1.10.0"
    homepage: str = AST_OUTLINE_HOMEPAGE
    schema_version: int = AST_OUTLINE_SCHEMA_VERSION
    env_passthrough: tuple[str, ...] = ()
    expected_sha256: str | None = None

    def __post_init__(self) -> None:
        if not str(self.binary):
            raise ValueError("ast-outline binary must not be empty")
        if self.timeout_seconds <= 0:
            raise ValueError("ast-outline timeout_seconds must be positive")
        if self.max_output_bytes <= 0:
            raise ValueError("ast-outline max_output_bytes must be positive")
        passthrough = tuple(dict.fromkeys(self.env_passthrough))
        if any(
            not isinstance(name, str)
            or not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", name)
            for name in passthrough
        ):
            raise ValueError("env_passthrough must contain environment names")
        object.__setattr__(self, "env_passthrough", passthrough)
        if self.expected_sha256 is not None:
            expected = self.expected_sha256.strip().lower()
            if not re.fullmatch(r"[0-9a-f]{64}", expected):
                raise ValueError("expected_sha256 must be 64 lowercase hex characters")
            object.__setattr__(self, "expected_sha256", expected)
        if self.schema_version != AST_OUTLINE_SCHEMA_VERSION:
            raise ValueError("this adapter supports ast-outline JSON schema 1 only")
        if _version_tuple(self.min_version) >= _version_tuple(self.max_version_exclusive):
            raise ValueError("ast-outline version range is empty")


@dataclass(frozen=True)
class AstContextProbe:
    status: str
    version: str = ""
    homepage: str = ""
    schema_version: int = AST_OUTLINE_SCHEMA_VERSION
    message: str = ""

    def __post_init__(self) -> None:
        if self.status not in _RESULT_STATUSES:
            raise ValueError(f"unknown ast context status: {self.status}")

    def as_dict(self) -> dict[str, Any]:
        result: dict[str, Any] = {
            "status": self.status,
            "version": self.version,
            "homepage": self.homepage,
            "schema_version": self.schema_version,
        }
        if self.message:
            result["message"] = self.message
        return result


@dataclass(frozen=True)
class AstContextResult:
    status: str
    operation: str
    data: dict[str, Any] | None = None
    probe: AstContextProbe | None = None
    warnings: tuple[str, ...] = field(default_factory=tuple)
    message: str = ""
    output_bytes: int = 0

    def __post_init__(self) -> None:
        if self.status not in _RESULT_STATUSES:
            raise ValueError(f"unknown ast context status: {self.status}")

    def as_dict(self) -> dict[str, Any]:
        result: dict[str, Any] = {
            "status": self.status,
            "operation": self.operation,
            "warnings": list(self.warnings),
        }
        if self.probe is not None:
            result["probe"] = self.probe.as_dict()
        if self.message:
            result["message"] = self.message
        if self.data is not None:
            result["data"] = self.data
        if self.output_bytes:
            result["output_bytes"] = self.output_bytes
        return result

    def to_json(self) -> str:
        return json.dumps(self.as_dict(), ensure_ascii=False, separators=(",", ":"))


@dataclass(frozen=True)
class _ProcessResult:
    status: str
    stdout: str = ""
    stderr: str = ""
    returncode: int | None = None
    message: str = ""


class _BoundedBytes:
    """Drain one subprocess stream without buffering past ``limit``."""

    def __init__(self, limit: int, on_overflow) -> None:
        self._limit = limit
        self._on_overflow = on_overflow
        self.data = bytearray()
        self.overflowed = False

    def drain(self, stream) -> None:
        while True:
            chunk = stream.read(65_536)
            if not chunk:
                return
            remaining = self._limit - len(self.data)
            if len(chunk) > remaining:
                if remaining > 0:
                    self.data.extend(chunk[:remaining])
                self.overflowed = True
                self._on_overflow()
                return
            self.data.extend(chunk)


def _kill_process_group(process: subprocess.Popen) -> None:
    """Best-effort termination of the process and descendants we started."""

    if os.name != "nt":
        try:
            group = os.getpgid(process.pid)
        except (ProcessLookupError, OSError):
            group = None
        if group is not None and group != os.getpgid(0):
            with contextlib.suppress(ProcessLookupError, PermissionError, OSError):
                os.killpg(group, signal.SIGKILL)
    with contextlib.suppress(Exception):
        process.kill()
    with contextlib.suppress(Exception):
        process.wait(timeout=2)


class AstOutlineAdapter:
    """Process-isolated adapter for ast-outline 1.9 JSON schema 1."""

    def __init__(self, config: AstContextConfig | None = None) -> None:
        self.config = config or AstContextConfig()

    def _binary(self) -> str | None:
        configured = str(self.config.binary)
        if os.path.isabs(configured) or os.sep in configured or (os.altsep and os.altsep in configured):
            candidate = Path(configured).expanduser().resolve()
            return str(candidate) if candidate.is_file() else None
        found = shutil.which(configured)
        return str(Path(found).resolve()) if found else None

    def _binary_integrity_error(self, binary: str, cwd: Path | None) -> str | None:
        """Validate the operator-pinned executable immediately before every exec."""

        candidate = Path(binary).resolve()
        if cwd is not None:
            root = cwd.resolve()
            if candidate == root or candidate.is_relative_to(root):
                return "ast-outline executable must live outside the model-visible workspace"
        expected = self.config.expected_sha256
        if expected is None:
            return None
        digest = hashlib.sha256()
        try:
            with candidate.open("rb") as stream:
                for chunk in iter(lambda: stream.read(128 * 1024), b""):
                    digest.update(chunk)
        except OSError as error:
            return f"could not verify ast-outline executable: {type(error).__name__}"
        if not hmac.compare_digest(digest.hexdigest(), expected):
            return "ast-outline executable SHA-256 does not match the pinned digest"
        return None

    def _run(self, args: list[str], *, cwd: Path | None = None) -> _ProcessResult:
        binary = self._binary()
        if binary is None:
            return _ProcessResult(
                "missing", message=f"ast-outline executable not found: {self.config.binary}"
            )
        integrity_error = self._binary_integrity_error(binary, cwd)
        if integrity_error is not None:
            return _ProcessResult("incompatible", message=integrity_error)
        argv = [binary, *args]
        env_names = (*_SAFE_ENV_NAMES, *self.config.env_passthrough)
        child_env = {
            name: os.environ[name]
            for name in env_names
            if name in os.environ
        }
        child_env["NO_COLOR"] = "1"
        try:
            process = subprocess.Popen(
                argv,
                cwd=str(cwd) if cwd is not None else None,
                env=child_env,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                shell=False,
                start_new_session=os.name != "nt",
            )
        except FileNotFoundError:
            return _ProcessResult("missing", message=f"ast-outline executable not found: {binary}")
        except OSError as exc:
            return _ProcessResult("error", message=f"could not start ast-outline: {exc}")

        assert process.stdout is not None
        assert process.stderr is not None
        stdout = _BoundedBytes(
            self.config.max_output_bytes, lambda: _kill_process_group(process)
        )
        stderr = _BoundedBytes(
            self.config.max_output_bytes, lambda: _kill_process_group(process)
        )
        readers = [
            threading.Thread(target=stdout.drain, args=(process.stdout,), daemon=True),
            threading.Thread(target=stderr.drain, args=(process.stderr,), daemon=True),
        ]
        for reader in readers:
            reader.start()
        # Reading threads, rather than communicate(), enforce the capture cap.
        # The process wait and both drains share one deadline: a child can close
        # stdout and then hang, or exit while a grandchild keeps the pipe open.
        # Both shapes must still hit the same wall-clock bound.
        deadline = time.monotonic() + self.config.timeout_seconds
        timed_out = False
        try:
            process.wait(timeout=max(0.0, deadline - time.monotonic()))
        except subprocess.TimeoutExpired:
            timed_out = True
        if not timed_out:
            for reader in readers:
                reader.join(timeout=max(0.0, deadline - time.monotonic()))
            timed_out = any(reader.is_alive() for reader in readers)
        if timed_out:
            _kill_process_group(process)
        with contextlib.suppress(subprocess.TimeoutExpired):
            process.wait(timeout=2)
        for reader in readers:
            reader.join(timeout=2)

        decoded_stdout = bytes(stdout.data).decode("utf-8", errors="replace")
        decoded_stderr = bytes(stderr.data).decode("utf-8", errors="replace")
        if timed_out:
            return _ProcessResult(
                "error",
                decoded_stdout,
                decoded_stderr,
                process.returncode,
                f"ast-outline timed out after {self.config.timeout_seconds:g}s",
            )
        if stdout.overflowed or stderr.overflowed:
            return _ProcessResult(
                "error",
                decoded_stdout,
                decoded_stderr,
                process.returncode,
                f"ast-outline output exceeded {self.config.max_output_bytes:,} bytes",
            )
        return _ProcessResult(
            "applied", decoded_stdout, decoded_stderr, process.returncode
        )

    def probe(self) -> AstContextProbe:
        process = self._run(["--version"])
        if process.status != "applied":
            return AstContextProbe(
                process.status,
                schema_version=self.config.schema_version,
                message=process.message,
            )
        if process.returncode != 0:
            return AstContextProbe(
                "error",
                schema_version=self.config.schema_version,
                message=f"ast-outline --version exited {process.returncode}: {process.stderr.strip()}",
            )
        version_match = _VERSION_RE.search(process.stdout)
        homepage_match = _HOMEPAGE_RE.search(process.stdout)
        if version_match is None or homepage_match is None:
            return AstContextProbe(
                "incompatible",
                schema_version=self.config.schema_version,
                message="ast-outline --version did not report the expected version and homepage",
            )
        version = version_match.group("version")
        homepage = homepage_match.group("homepage").rstrip("/")
        expected_homepage = self.config.homepage.rstrip("/")
        compatible = (
            _version_tuple(self.config.min_version)
            <= _version_tuple(version)
            < _version_tuple(self.config.max_version_exclusive)
        )
        if homepage != expected_homepage or not compatible:
            return AstContextProbe(
                "incompatible",
                version=version,
                homepage=homepage,
                schema_version=self.config.schema_version,
                message=(
                    f"expected {expected_homepage} version >= {self.config.min_version} "
                    f"and < {self.config.max_version_exclusive}"
                ),
            )
        return AstContextProbe(
            "applied",
            version=version,
            homepage=homepage,
            schema_version=self.config.schema_version,
        )

    @staticmethod
    def _workspace(workspace: Path) -> Path:
        root = Path(workspace).expanduser().resolve()
        if not root.is_dir():
            raise ValueError(f"workspace is not a directory: {root}")
        return root

    @staticmethod
    def _contained(root: Path, candidate: Path) -> bool:
        return candidate == root or root in candidate.parents

    def _safe_path(self, root: Path, raw: str, *, allow_glob: bool = False) -> str:
        if not isinstance(raw, str) or not raw or "\x00" in raw:
            raise ValueError("paths must be non-empty strings without NUL bytes")
        if len(raw) > _MAX_INPUT_CHARS:
            raise ValueError(f"paths must be at most {_MAX_INPUT_CHARS} characters")
        path = Path(raw).expanduser()
        has_glob = allow_glob and any(char in raw for char in _GLOB_CHARS)
        if not has_glob:
            candidate = (path if path.is_absolute() else root / path).resolve()
            if not self._contained(root, candidate):
                raise ValueError(f"path resolves outside workspace: {raw}")
            return str(candidate)

        parts = path.parts
        glob_index = next(
            index
            for index, part in enumerate(parts)
            if any(char in part for char in _GLOB_CHARS)
        )
        if any("**" in part for part in parts):
            raise ValueError("recursive glob '**' is not allowed")
        if glob_index != len(parts) - 1:
            raise ValueError(
                "glob wildcards are allowed only in the final path component"
            )
        suffix = parts[glob_index:]
        if any(part == ".." for part in suffix):
            raise ValueError(f"glob escapes workspace: {raw}")
        static = Path(*parts[:glob_index]) if glob_index else Path(".")
        prefix = (static if static.is_absolute() else root / static).resolve()
        if not self._contained(root, prefix):
            raise ValueError(f"glob resolves outside workspace: {raw}")
        pattern = prefix.joinpath(*suffix)
        # Resolve existing matches too: a wildcard can otherwise traverse a
        # symlink whose static prefix itself still sits inside the workspace.
        # Only a basename is variable and recursive expansion is forbidden, so
        # validation cannot walk an unbounded tree before the process timeout.
        for index, matched in enumerate(
            globlib.iglob(str(pattern), recursive=False), start=1
        ):
            if index > _MAX_GLOB_MATCHES:
                raise ValueError(
                    f"glob matched more than {_MAX_GLOB_MATCHES} paths"
                )
            resolved = Path(matched).resolve()
            if not self._contained(root, resolved):
                raise ValueError(f"glob resolves outside workspace: {raw}")
        return str(pattern)

    @staticmethod
    def _validate_strings(values: Iterable[str], label: str) -> list[str]:
        if isinstance(values, (str, bytes)):
            raise ValueError(f"{label} must be a non-empty array of strings")
        result: list[str] = []
        for value in values:
            if len(result) >= _MAX_INPUT_ITEMS:
                raise ValueError(
                    f"{label} must contain at most {_MAX_INPUT_ITEMS} items"
                )
            if not isinstance(value, str) or not value or "\x00" in value:
                raise ValueError(f"{label} must be a non-empty array of strings")
            if len(value) > _MAX_INPUT_CHARS:
                raise ValueError(
                    f"{label} entries must be at most {_MAX_INPUT_CHARS} characters"
                )
            result.append(value)
        if not result:
            raise ValueError(f"{label} must be a non-empty array of strings")
        return result

    def _path_error(self, operation: str, exc: Exception) -> AstContextResult:
        return AstContextResult("error", operation, message=str(exc))

    def _invoke(
        self,
        operation: str,
        command: str,
        argv: list[str],
        root: Path,
        *,
        include_imports: bool | None = None,
    ) -> AstContextResult:
        probe = self.probe()
        if probe.status != "applied":
            return AstContextResult(
                probe.status, operation, probe=probe, message=probe.message
            )
        process = self._run(argv, cwd=root)
        if process.status != "applied":
            return AstContextResult(
                process.status,
                operation,
                probe=probe,
                message=process.message,
                output_bytes=len(process.stdout.encode("utf-8")),
            )
        if process.returncode != 0:
            detail = process.stderr.strip() or process.stdout.strip()
            return AstContextResult(
                "error",
                operation,
                probe=probe,
                message=f"ast-outline exited {process.returncode}: {detail}",
                output_bytes=len(process.stdout.encode("utf-8")),
            )
        try:
            payload = json.loads(process.stdout)
        except (TypeError, json.JSONDecodeError) as exc:
            return AstContextResult(
                "error",
                operation,
                probe=probe,
                message=f"ast-outline returned invalid JSON: {exc}",
                output_bytes=len(process.stdout.encode("utf-8")),
            )
        if not isinstance(payload, dict):
            return AstContextResult(
                "error", operation, probe=probe, message="ast-outline JSON must be an object"
            )
        if (
            payload.get("tool") != "ast-outline"
            or payload.get("schema_version") != self.config.schema_version
            or payload.get("command") != command
        ):
            return AstContextResult(
                "incompatible",
                operation,
                probe=probe,
                message=(
                    "unexpected ast-outline JSON envelope: expected tool=ast-outline, "
                    f"schema_version={self.config.schema_version}, command={command}"
                ),
                output_bytes=len(process.stdout.encode("utf-8")),
            )
        if "error" in payload:
            error = payload.get("error")
            notes = error.get("notes", []) if isinstance(error, dict) else []
            hint = error.get("hint", "") if isinstance(error, dict) else ""
            message = "; ".join(str(note) for note in notes)
            if hint:
                message = f"{message} (hint: {hint})" if message else str(hint)
            return AstContextResult(
                "error",
                operation,
                data=payload,
                probe=probe,
                message=message or "ast-outline reported an error",
                output_bytes=len(process.stdout.encode("utf-8")),
            )

        if include_imports is False:
            for file_result in payload.get("files", []):
                if not isinstance(file_result, dict):
                    continue
                file_result.pop("imports", None)
                file_result.pop("conditional_imports_count", None)
                file_result.pop("import_regions", None)

        warnings = [str(note) for note in payload.get("notes", []) if isinstance(note, str)]
        parse_errors = _metric_total(payload, {"error_count", "parse_errors"})
        truncated = _metric_total(payload, {"truncated_count"})
        if parse_errors:
            warnings.append(f"ast_parse_errors:{parse_errors}")
        if truncated:
            warnings.append(f"ast_matches_truncated:{truncated}")
        if parse_errors or truncated:
            status = "partial"
        elif _is_ambiguous(command, payload):
            status = "ambiguous"
        elif _is_no_match(command, payload):
            status = "no_match"
        else:
            status = "applied"
        return AstContextResult(
            status,
            operation,
            data=payload,
            probe=probe,
            warnings=tuple(warnings),
            output_bytes=len(process.stdout.encode("utf-8")),
        )

    def repo_map(
        self,
        workspace: Path,
        paths: Iterable[str],
        *,
        density: str = "default",
        include_imports: bool = False,
    ) -> AstContextResult:
        operation = "repo_map"
        try:
            if density not in _DIGEST_DENSITIES:
                raise ValueError(f"density must be one of {sorted(_DIGEST_DENSITIES)}")
            root = self._workspace(workspace)
            safe_paths = [
                self._safe_path(root, path)
                for path in self._validate_strings(paths, "paths")
            ]
        except (OSError, ValueError) as exc:
            return self._path_error(operation, exc)
        argv = ["digest", "--format", density]
        if include_imports:
            argv.append("--imports")
        argv.extend(["--json", "--", *safe_paths])
        return self._invoke(
            operation, "digest", argv, root, include_imports=include_imports
        )

    def file_outline(
        self,
        workspace: Path,
        paths: Iterable[str],
        *,
        view: str = "full",
        include_imports: bool = False,
    ) -> AstContextResult:
        operation = "file_outline"
        try:
            if view not in _OUTLINE_VIEWS:
                raise ValueError(f"view must be one of {sorted(_OUTLINE_VIEWS)}")
            root = self._workspace(workspace)
            safe_paths = [
                self._safe_path(root, path)
                for path in self._validate_strings(paths, "paths")
            ]
        except (OSError, ValueError) as exc:
            return self._path_error(operation, exc)
        argv = ["outline"]
        if view in {"public", "minimal"}:
            argv.append("--no-private")
        if view == "minimal":
            argv.extend(["--no-fields", "--no-docs", "--no-attrs"])
        if include_imports:
            argv.append("--imports")
        argv.extend(["--json", "--", *safe_paths])
        return self._invoke(
            operation, "outline", argv, root, include_imports=include_imports
        )

    def show_symbol(
        self,
        workspace: Path,
        path_or_glob: str,
        symbols: Iterable[str],
        *,
        signature_only: bool = False,
    ) -> AstContextResult:
        operation = "show_symbol"
        try:
            root = self._workspace(workspace)
            safe_target = self._safe_path(root, path_or_glob, allow_glob=True)
            safe_symbols = self._validate_strings(symbols, "symbols")
        except (OSError, ValueError) as exc:
            return self._path_error(operation, exc)
        argv = ["show"]
        if signature_only:
            argv.append("--signature")
        argv.extend(["--json", "--", safe_target, *safe_symbols])
        return self._invoke(operation, "show", argv, root)

    def symbol_references(
        self,
        workspace: Path,
        patterns: Iterable[str],
        paths: Iterable[str],
        *,
        kinds: Iterable[str] = (),
        max_per_file: int | None = None,
    ) -> AstContextResult:
        operation = "symbol_references"
        try:
            root = self._workspace(workspace)
            safe_patterns = self._validate_strings(patterns, "patterns")
            safe_paths = [
                self._safe_path(root, path)
                for path in self._validate_strings(paths, "paths")
            ]
            if isinstance(kinds, (str, bytes)):
                raise ValueError("kinds must be an array")
            safe_kinds = []
            for kind in kinds:
                if len(safe_kinds) >= len(_REFERENCE_KINDS):
                    raise ValueError("too many kinds")
                safe_kinds.append(kind)
            if any(kind not in _REFERENCE_KINDS for kind in safe_kinds):
                raise ValueError(f"kinds must be drawn from {sorted(_REFERENCE_KINDS)}")
            if max_per_file is not None and (
                isinstance(max_per_file, bool)
                or not isinstance(max_per_file, int)
                or max_per_file <= 0
                or max_per_file > _MAX_GLOB_MATCHES
            ):
                raise ValueError(
                    f"max_per_file must be an integer from 1 to {_MAX_GLOB_MATCHES}"
                )
        except (OSError, TypeError, ValueError) as exc:
            return self._path_error(operation, exc)

        argv = ["grep", "--json"]
        if safe_kinds:
            argv.append(f"--kind={','.join(safe_kinds)}")
        if max_per_file is not None:
            argv.append(f"--max-count={max_per_file}")
        # Attached -e values remain one argv even if a pattern begins with '-'.
        # The first -e is promoted by ast-outline's documented normalizer.
        argv.extend(f"--expression={pattern}" for pattern in safe_patterns)
        argv.extend(["--", *safe_paths])
        return self._invoke(operation, "grep", argv, root)


def _metric_total(value: Any, keys: set[str]) -> int:
    total = 0
    if isinstance(value, dict):
        for key, child in value.items():
            if key in keys:
                if isinstance(child, bool):
                    total += int(child)
                elif isinstance(child, int) and child > 0:
                    total += child
                elif isinstance(child, (list, tuple, set, dict)):
                    total += len(child)
            else:
                total += _metric_total(child, keys)
    elif isinstance(value, list):
        total += sum(_metric_total(child, keys) for child in value)
    return total


def _is_ambiguous(command: str, payload: dict[str, Any]) -> bool:
    if command != "show":
        return False
    for result in payload.get("results", []):
        if not isinstance(result, dict):
            continue
        matches = result.get("matches", [])
        if result.get("ambiguous") is True or (isinstance(matches, list) and len(matches) > 1):
            return True
    return False


def _is_no_match(command: str, payload: dict[str, Any]) -> bool:
    if command == "grep":
        summary = payload.get("summary", {})
        if isinstance(summary, dict) and summary.get("total_matches") == 0:
            return True
        files = payload.get("files", [])
        return bool(files) and all(not item.get("matches") for item in files if isinstance(item, dict))
    if command == "show":
        results = payload.get("results", [])
        return not results or all(
            isinstance(result, dict) and not result.get("matches") for result in results
        )
    files = payload.get("files", [])
    return isinstance(files, list) and not files


_REPO_MAP_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "properties": {
        "paths": {"type": "array", "items": {"type": "string", "maxLength": _MAX_INPUT_CHARS}, "minItems": 1, "maxItems": _MAX_INPUT_ITEMS},
        "density": {
            "type": "string",
            "enum": ["names", "compact", "default", "wide"],
            "default": "default",
        },
        "include_imports": {"type": "boolean", "default": False},
    },
    "required": ["paths"],
}

_FILE_OUTLINE_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "properties": {
        "paths": {"type": "array", "items": {"type": "string", "maxLength": _MAX_INPUT_CHARS}, "minItems": 1, "maxItems": _MAX_INPUT_ITEMS},
        "view": {
            "type": "string",
            "enum": ["full", "public", "minimal"],
            "default": "full",
        },
        "include_imports": {"type": "boolean", "default": False},
    },
    "required": ["paths"],
}

_SHOW_SYMBOL_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "properties": {
        "path_or_glob": {
            "type": "string",
            "maxLength": _MAX_INPUT_CHARS,
            "description": (
                "Workspace path, or a non-recursive basename glob in a static "
                "workspace directory; '**' and intermediate wildcards are rejected."
            ),
        },
        "symbols": {"type": "array", "items": {"type": "string", "maxLength": _MAX_INPUT_CHARS}, "minItems": 1, "maxItems": _MAX_INPUT_ITEMS},
        "signature_only": {"type": "boolean", "default": False},
    },
    "required": ["path_or_glob", "symbols"],
}

_SYMBOL_REFERENCES_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "properties": {
        "patterns": {"type": "array", "items": {"type": "string", "maxLength": _MAX_INPUT_CHARS}, "minItems": 1, "maxItems": _MAX_INPUT_ITEMS},
        "paths": {"type": "array", "items": {"type": "string", "maxLength": _MAX_INPUT_CHARS}, "minItems": 1, "maxItems": _MAX_INPUT_ITEMS},
        "kinds": {
            "type": "array",
            "items": {
                "type": "string",
                "enum": ["def", "call", "ref", "import", "comment", "string"],
            },
            "maxItems": len(_REFERENCE_KINDS),
            "default": [],
        },
        "max_per_file": {"type": "integer", "minimum": 1, "maximum": _MAX_GLOB_MATCHES},
    },
    "required": ["patterns", "paths"],
}


def install_ast_context_tools(
    registry: ToolRegistry, config: AstContextConfig | None = None
) -> ToolRegistry:
    """Register four stateless semantic-read tools on ``registry``.

    The adapter has no cache or mutable request state, and every invocation has
    its own subprocess, so these read-only handlers explicitly opt into the
    harness's parallel-safe batch execution.
    """

    adapter = AstOutlineAdapter(config)

    async def repo_map(ctx: ToolContext, paths, density="default", include_imports=False):
        result = await asyncio.to_thread(
            adapter.repo_map,
            ctx.workspace,
            paths,
            density=density,
            include_imports=include_imports,
        )
        return result.to_json()

    async def file_outline(ctx: ToolContext, paths, view="full", include_imports=False):
        result = await asyncio.to_thread(
            adapter.file_outline,
            ctx.workspace,
            paths,
            view=view,
            include_imports=include_imports,
        )
        return result.to_json()

    async def show_symbol(ctx: ToolContext, path_or_glob, symbols, signature_only=False):
        result = await asyncio.to_thread(
            adapter.show_symbol,
            ctx.workspace,
            path_or_glob,
            symbols,
            signature_only=signature_only,
        )
        return result.to_json()

    async def symbol_references(
        ctx: ToolContext, patterns, paths, kinds=(), max_per_file=None
    ):
        result = await asyncio.to_thread(
            adapter.symbol_references,
            ctx.workspace,
            patterns,
            paths,
            kinds=kinds,
            max_per_file=max_per_file,
        )
        return result.to_json()

    specs = [
        (
            "repo_map",
            "Map supported source files with ast-outline digest; returns JSON status applied, no_match, partial, error, missing, or incompatible.",
            _REPO_MAP_SCHEMA,
            repo_map,
            "repo.semantic_outline",
        ),
        (
            "file_outline",
            "Read structural signatures and line ranges for files or directories without full method bodies.",
            _FILE_OUTLINE_SCHEMA,
            file_outline,
            "repo.semantic_outline",
        ),
        (
            "show_symbol",
            "Read exact source or signature for named symbols inside one workspace path, directory, or non-recursive basename glob.",
            _SHOW_SYMBOL_SCHEMA,
            show_symbol,
            "repo.symbol",
        ),
        (
            "symbol_references",
            "Find definitions, calls, references, or imports grouped by enclosing scope with ast-outline grep.",
            _SYMBOL_REFERENCES_SCHEMA,
            symbol_references,
            "repo.references",
        ),
    ]
    for name, description, schema, handler, capability in specs:
        registry.register(
            Tool(
                name,
                description,
                schema,
                handler,
                readonly=True,
                parallel_safe=True,
                risk="read",
                capabilities=frozenset({capability}),
            )
        )
    return registry


__all__ = [
    "AST_OUTLINE_HOMEPAGE",
    "AST_OUTLINE_SCHEMA_VERSION",
    "AstContextConfig",
    "AstContextProbe",
    "AstContextResult",
    "AstOutlineAdapter",
    "install_ast_context_tools",
]
