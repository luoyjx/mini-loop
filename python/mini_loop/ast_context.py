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
import fnmatch
import hashlib
import hmac
import json
import os
import re
import shutil
import signal
import stat
import subprocess
import tempfile
import threading
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Iterable, Iterator

from pathspec import GitIgnoreSpec

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
_SAFE_ENV_NAMES = ("LANG", "LC_ALL", "SYSTEMROOT")
_MAX_INPUT_ITEMS = 64
_MAX_INPUT_CHARS = 2_048
_MAX_GLOB_MATCHES = 10_000
_MAX_SNAPSHOT_BYTES = 64 * 1024 * 1024
_MAX_SNAPSHOT_ENTRIES = 50_000
_MAX_SNAPSHOT_FILES = 20_000
_MAX_SNAPSHOT_DEPTH = 64
_MAX_AMBIGUOUS_MATCHES = 32
_MAX_AMBIGUOUS_BYTES = 16_384
_MAX_IGNORE_BYTES = 512 * 1024
_MAX_IGNORE_PATTERNS = 4_096
_MAX_IGNORE_PATTERN_CHARS = 4_096
_MAX_IGNORE_MATCH_OPS = 10_000_000
_SNAPSHOT_TEMP_PARENTS = (Path("/tmp"), Path("/var/tmp"))
_HAS_SECURE_DIR_FD = os.open in os.supports_dir_fd and hasattr(os, "O_NOFOLLOW")
_SNAPSHOT_ALWAYS_SKIP_DIRS = frozenset(
    {
        ".git",
        ".hg",
        ".svn",
    }
)
_AST_SOURCE_SUFFIXES = frozenset(
    {
        ".c++",
        ".cc",
        ".cjs",
        ".cpp",
        ".cppm",
        ".cs",
        ".css",
        ".cxx",
        ".ex",
        ".exs",
        ".gd",
        ".go",
        ".gemspec",
        ".h",
        ".h++",
        ".hh",
        ".hpp",
        ".htm",
        ".html",
        ".hxx",
        ".inl",
        ".ipp",
        ".ixx",
        ".java",
        ".js",
        ".jsx",
        ".kt",
        ".kts",
        ".lua",
        ".markdown",
        ".md",
        ".mdown",
        ".mdx",
        ".mjs",
        ".php",
        ".php8",
        ".phps",
        ".phtml",
        ".py",
        ".pyi",
        ".rake",
        ".rb",
        ".rs",
        ".ru",
        ".sc",
        ".scala",
        ".scss",
        ".sql",
        ".swift",
        ".tpp",
        ".ts",
        ".tsx",
        ".vue",
        ".wlua",
        ".yaml",
        ".yml",
    }
)
_AST_SOURCE_BASENAMES = frozenset({"Gemfile", "Rakefile"})
_IGNORE_FILE_NAMES = (".gitignore", ".ignore")
_DEFAULT_IGNORE_PATTERNS = (
    ".git/",
    ".svn/",
    ".hg/",
    "node_modules/",
    "__pycache__/",
    ".venv/",
    "venv/",
    ".tox/",
    ".mypy_cache/",
    ".pytest_cache/",
    ".ruff_cache/",
    ".eggs/",
    "*.egg-info/",
    ".gradle/",
    ".idea/",
    ".vs/",
    ".vscode/",
    ".cursor/",
    ".zed/",
    ".fleet/",
    "__snapshots__/",
    ".husky/",
    ".next/",
    ".nuxt/",
    ".svelte-kit/",
    ".turbo/",
    ".parcel-cache/",
    ".vite/",
    ".terraform/",
    "*.min.js",
    "*.min.mjs",
    "*.min.cjs",
    "*.min.css",
    "*.min.html",
    "*.map",
)


def _version_tuple(version: str) -> tuple[int, int, int]:
    match = re.match(r"^(\d+)\.(\d+)\.(\d+)", version)
    if match is None:
        raise ValueError(f"invalid semantic version: {version!r}")
    return tuple(int(part) for part in match.groups())


def _validate_ignore_patterns(lines: Iterable[str]) -> None:
    """Reject GitWildMatch shapes that compile to catastrophic backtracking regex."""

    for raw_line in lines:
        if len(raw_line) > _MAX_IGNORE_PATTERN_CHARS:
            raise ValueError(
                "ast-outline ignore patterns must be at most "
                f"{_MAX_IGNORE_PATTERN_CHARS} characters"
            )
        if not raw_line or (raw_line.startswith("#") and not raw_line.startswith("\\#")):
            continue
        line = raw_line[1:] if raw_line.startswith("!") else raw_line
        variable_star_groups = 0
        for segment in line.split("/"):
            if segment == "**":
                variable_star_groups += 1
            else:
                escaped = False
                for character in segment:
                    if escaped:
                        escaped = False
                        continue
                    if character == "\\":
                        escaped = True
                        continue
                    if character == "*":
                        variable_star_groups += 1
            if variable_star_groups > 2:
                raise ValueError(
                    "ast-outline ignore patterns allow at most two "
                    "variable-star groups"
                )


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
        if cwd is not None:
            try:
                child_env["TMPDIR"] = str(self._snapshot_temp_parent(cwd))
            except OSError as exc:
                return _ProcessResult("error", message=str(exc))
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

    def probe(self, *, cwd: Path | None = None) -> AstContextProbe:
        """Probe the executable, applying workspace containment when supplied."""

        process = self._run(["--version"], cwd=cwd)
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

    def _snapshot_temp_parent(self, root: Path) -> Path:
        """Choose a fixed system temp root outside model-visible workspace state."""

        for configured in _SNAPSHOT_TEMP_PARENTS:
            try:
                candidate = configured.resolve(strict=True)
            except OSError:
                continue
            if candidate.is_dir() and not self._contained(root, candidate):
                return candidate
        raise OSError("no trusted temporary directory is available outside workspace")

    def _safe_path(self, root: Path, raw: str) -> str:
        if not isinstance(raw, str) or not raw or "\x00" in raw:
            raise ValueError("paths must be non-empty strings without NUL bytes")
        if len(raw) > _MAX_INPUT_CHARS:
            raise ValueError(f"paths must be at most {_MAX_INPUT_CHARS} characters")
        if any(char in raw for char in _GLOB_CHARS):
            raise ValueError(
                "glob syntax is accepted only by show_symbol's bounded basename matcher"
            )
        path = Path(raw).expanduser()
        candidate = (path if path.is_absolute() else root / path).resolve()
        if not self._contained(root, candidate):
            raise ValueError(f"path resolves outside workspace: {raw}")
        return str(candidate)

    def _safe_glob_paths(self, root: Path, raw: str) -> tuple[str, ...]:
        """Expand one basename glob through root-anchored, no-follow descriptors."""

        if not isinstance(raw, str) or not raw or "\x00" in raw:
            raise ValueError("paths must be non-empty strings without NUL bytes")
        if len(raw) > _MAX_INPUT_CHARS:
            raise ValueError(f"paths must be at most {_MAX_INPUT_CHARS} characters")
        path = Path(raw).expanduser()
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
        if not _HAS_SECURE_DIR_FD:
            raise OSError(
                "secure ast-outline globs require dir_fd and O_NOFOLLOW support"
            )
        static = Path(*parts[:glob_index]) if glob_index else Path(".")
        unchecked_prefix = static if static.is_absolute() else root / static
        prefix = Path(os.path.abspath(unchecked_prefix))
        try:
            relative_prefix = prefix.relative_to(root)
        except ValueError as exc:
            raise ValueError(f"glob resolves outside workspace: {raw}") from exc
        if any(part == ".." for part in relative_prefix.parts):
            raise ValueError(f"glob resolves outside workspace: {raw}")
        directory_flags = (
            os.O_RDONLY
            | getattr(os, "O_DIRECTORY", 0)
            | os.O_NOFOLLOW
            | getattr(os, "O_NONBLOCK", 0)
        )
        deadline = time.monotonic() + self.config.timeout_seconds
        current_fd = os.open(root, directory_flags)
        try:
            for component in relative_prefix.parts:
                try:
                    next_fd = os.open(
                        component, directory_flags, dir_fd=current_fd
                    )
                except OSError as exc:
                    raise ValueError(
                        "glob static directory is unavailable or outside workspace: "
                        f"{raw}"
                    ) from exc
                os.close(current_fd)
                current_fd = next_fd

            pattern = suffix[0]
            names: list[str] = []
            scan_fd = os.dup(current_fd)
            try:
                with os.scandir(scan_fd) as entries:
                    for index, entry in enumerate(entries, start=1):
                        if time.monotonic() > deadline:
                            raise ValueError(
                                "ast-outline glob expansion exceeded "
                                f"{self.config.timeout_seconds:g}s"
                            )
                        if index > _MAX_GLOB_MATCHES:
                            raise ValueError(
                                "glob directory contains more than "
                                f"{_MAX_GLOB_MATCHES} entries"
                            )
                        if entry.name.startswith(".") and not pattern.startswith("."):
                            continue
                        if not fnmatch.fnmatchcase(entry.name, pattern):
                            continue
                        if not entry.is_file(follow_symlinks=False):
                            continue
                        names.append(entry.name)
            finally:
                with contextlib.suppress(OSError):
                    os.close(scan_fd)
        finally:
            os.close(current_fd)

        # The frozen source snapshot reopens every match relative to ``root``
        # with O_NOFOLLOW, so a writer racing this enumeration cannot redirect
        # the child outside the workspace.
        matches: list[str] = []
        for name in sorted(names):
            matches.append(str(prefix / name))
        return tuple(sorted(matches))

    @contextlib.contextmanager
    def _snapshot_workspace_paths(
        self, root: Path, candidates: Iterable[Path]
    ) -> Iterator[tuple[tuple[Path, ...], tuple[tuple[str, str], ...]]]:
        """Build one bounded, no-follow source snapshot for an AST invocation.

        Every directory component is opened relative to an already-open parent
        descriptor. Workspace writers may race the copy, but they cannot turn a
        validated entry into a symlink that makes the child read outside root.
        Recursive snapshots ignore symlinks, special files, VCS metadata and
        unsupported source names. They apply ast-outline 1.9's default plus
        root/nested .gitignore/.ignore frames before copying; explicit regular
        files intentionally bypass those directory-walk filters.
        """

        if not _HAS_SECURE_DIR_FD:
            raise OSError(
                "secure ast-outline snapshots require dir_fd and O_NOFOLLOW support"
            )
        candidate_list: list[Path] = []
        seen: set[Path] = set()
        for candidate in candidates:
            candidate = Path(candidate)
            try:
                candidate.relative_to(root)
            except ValueError as exc:
                raise ValueError("snapshot target is outside workspace") from exc
            if candidate not in seen:
                candidate_list.append(candidate)
                seen.add(candidate)
        if not candidate_list:
            raise ValueError("at least one snapshot target is required")

        directory_flags = (
            os.O_RDONLY
            | getattr(os, "O_DIRECTORY", 0)
            | os.O_NOFOLLOW
            | getattr(os, "O_NONBLOCK", 0)
        )
        file_flags = (
            os.O_RDONLY
            | os.O_NOFOLLOW
            | getattr(os, "O_NONBLOCK", 0)
        )
        budget = {
            "entries": 0,
            "files": 0,
            "bytes": 0,
            "ignore_bytes": 0,
            "ignore_patterns": 0,
            "ignore_match_ops": 0,
        }
        deadline = time.monotonic() + self.config.timeout_seconds
        ignore_cache: dict[tuple[str, ...], tuple[tuple[str, bytes], ...]] = {}

        def check_deadline() -> None:
            if time.monotonic() > deadline:
                raise ValueError(
                    "ast-outline source snapshot exceeded "
                    f"{self.config.timeout_seconds:g}s"
                )

        def write_bytes(data: bytes, destination: Path) -> None:
            if budget["files"] >= _MAX_SNAPSHOT_FILES:
                raise ValueError(
                    f"ast-outline snapshot exceeds {_MAX_SNAPSHOT_FILES} files"
                )
            if budget["bytes"] + len(data) > _MAX_SNAPSHOT_BYTES:
                raise ValueError(
                    f"ast-outline snapshot exceeds {_MAX_SNAPSHOT_BYTES} bytes"
                )
            destination.parent.mkdir(parents=True, exist_ok=True)
            with destination.open("xb") as output:
                output.write(data)
            destination.chmod(0o400)
            budget["files"] += 1
            budget["bytes"] += len(data)

        def copy_regular(file_fd: int, destination: Path) -> None:
            metadata = os.fstat(file_fd)
            if not stat.S_ISREG(metadata.st_mode):
                raise ValueError("ast-outline snapshot accepts regular files only")
            if budget["files"] >= _MAX_SNAPSHOT_FILES:
                raise ValueError(
                    f"ast-outline snapshot exceeds {_MAX_SNAPSHOT_FILES} files"
                )
            if budget["bytes"] + metadata.st_size > _MAX_SNAPSHOT_BYTES:
                raise ValueError(
                    f"ast-outline snapshot exceeds {_MAX_SNAPSHOT_BYTES} bytes"
                )
            destination.parent.mkdir(parents=True, exist_ok=True)
            budget["files"] += 1
            with os.fdopen(os.dup(file_fd), "rb", closefd=True) as source:
                with destination.open("xb") as output:
                    while True:
                        check_deadline()
                        chunk = source.read(128 * 1024)
                        if not chunk:
                            break
                        budget["bytes"] += len(chunk)
                        if budget["bytes"] > _MAX_SNAPSHOT_BYTES:
                            raise ValueError(
                                f"ast-outline snapshot exceeds {_MAX_SNAPSHOT_BYTES} bytes"
                            )
                        output.write(chunk)
            destination.chmod(0o400)

        def open_parent(root_fd: int, parts: tuple[str, ...]) -> int:
            current = os.dup(root_fd)
            try:
                for component in parts:
                    next_fd = os.open(
                        component,
                        directory_flags,
                        dir_fd=current,
                    )
                    os.close(current)
                    current = next_fd
                return current
            except BaseException:
                os.close(current)
                raise

        def read_ignore_controls(
            directory_fd: int, anchor: tuple[str, ...]
        ) -> tuple[tuple[str, bytes], ...]:
            cached = ignore_cache.get(anchor)
            if cached is not None:
                return cached
            controls: list[tuple[str, bytes]] = []
            for name in _IGNORE_FILE_NAMES:
                check_deadline()
                try:
                    metadata = os.stat(
                        name,
                        dir_fd=directory_fd,
                        follow_symlinks=False,
                    )
                except OSError:
                    continue
                if not stat.S_ISREG(metadata.st_mode):
                    continue
                remaining = _MAX_IGNORE_BYTES - budget["ignore_bytes"]
                if metadata.st_size > remaining:
                    raise ValueError(
                        f"ast-outline ignore files exceed {_MAX_IGNORE_BYTES} bytes"
                    )
                try:
                    control_fd = os.open(name, file_flags, dir_fd=directory_fd)
                except OSError:
                    continue
                try:
                    if not stat.S_ISREG(os.fstat(control_fd).st_mode):
                        continue
                    with os.fdopen(os.dup(control_fd), "rb", closefd=True) as source:
                        data = source.read(remaining + 1)
                finally:
                    os.close(control_fd)
                if len(data) > remaining:
                    raise ValueError(
                        f"ast-outline ignore files exceed {_MAX_IGNORE_BYTES} bytes"
                    )
                decoded_lines = data.decode("utf-8", errors="ignore").splitlines()
                _validate_ignore_patterns(decoded_lines)
                pattern_count = len(decoded_lines)
                if budget["ignore_patterns"] + pattern_count > _MAX_IGNORE_PATTERNS:
                    raise ValueError(
                        "ast-outline ignore files exceed "
                        f"{_MAX_IGNORE_PATTERNS} patterns"
                    )
                budget["ignore_bytes"] += len(data)
                budget["ignore_patterns"] += pattern_count
                controls.append((name, data))
            frozen = tuple(controls)
            ignore_cache[anchor] = frozen
            return frozen

        def ignore_lines(controls: tuple[tuple[str, bytes], ...]) -> list[str]:
            lines: list[str] = []
            for _, data in controls:
                lines.extend(data.decode("utf-8", errors="ignore").splitlines())
            return lines

        def copy_controls(
            controls: tuple[tuple[str, bytes], ...], destination: Path
        ) -> None:
            for name, data in controls:
                target = destination / name
                if not target.exists():
                    write_bytes(data, target)

        def is_ignored(
            path_parts: tuple[str, ...],
            *,
            is_dir: bool,
            frames: list[tuple[tuple[str, ...], GitIgnoreSpec]],
        ) -> bool:
            for anchor, spec in reversed(frames):
                check_deadline()
                if path_parts[: len(anchor)] != anchor:
                    continue
                budget["ignore_match_ops"] += len(spec.patterns)
                if budget["ignore_match_ops"] > _MAX_IGNORE_MATCH_OPS:
                    raise ValueError(
                        "ast-outline ignore matching exceeds "
                        f"{_MAX_IGNORE_MATCH_OPS} pattern checks"
                    )
                rendered = "/".join(path_parts[len(anchor) :])
                if is_dir:
                    rendered += "/"
                result = spec.check_file(rendered)
                if result.include is True:
                    return True
                if result.include is False:
                    return False
            return False

        def has_git_marker(directory_fd: int) -> bool:
            try:
                metadata = os.stat(
                    ".git", dir_fd=directory_fd, follow_symlinks=False
                )
            except OSError:
                return False
            return not stat.S_ISLNK(metadata.st_mode)

        def find_project_root(
            root_fd: int, directory_parts: tuple[str, ...]
        ) -> tuple[str, ...]:
            current_fd = os.dup(root_fd)
            nearest: tuple[str, ...] | None = None
            try:
                check_deadline()
                if has_git_marker(current_fd):
                    nearest = ()
                for index, component in enumerate(directory_parts, start=1):
                    check_deadline()
                    next_fd = os.open(
                        component, directory_flags, dir_fd=current_fd
                    )
                    os.close(current_fd)
                    current_fd = next_fd
                    if has_git_marker(current_fd):
                        nearest = directory_parts[:index]
                return directory_parts if nearest is None else nearest
            finally:
                os.close(current_fd)

        def copy_directory(
            directory_fd: int,
            destination: Path,
            *,
            source_parts: tuple[str, ...],
            project_root_parts: tuple[str, ...],
            frames: list[tuple[tuple[str, ...], GitIgnoreSpec]],
            depth: int,
        ) -> None:
            if depth > _MAX_SNAPSHOT_DEPTH:
                raise ValueError(
                    f"ast-outline snapshot exceeds {_MAX_SNAPSHOT_DEPTH} directory levels"
                )
            destination.mkdir(parents=True, exist_ok=True)
            local_frames = frames
            controls = read_ignore_controls(directory_fd, source_parts)
            if source_parts != project_root_parts and controls:
                local_frames = [
                    *frames,
                    (source_parts, GitIgnoreSpec.from_lines(ignore_lines(controls))),
                ]
            copy_controls(controls, destination)

            names: list[str] = []
            scan_fd = os.dup(directory_fd)
            try:
                with os.scandir(scan_fd) as entries:
                    for entry in entries:
                        check_deadline()
                        budget["entries"] += 1
                        if budget["entries"] > _MAX_SNAPSHOT_ENTRIES:
                            raise ValueError(
                                f"ast-outline snapshot exceeds {_MAX_SNAPSHOT_ENTRIES} entries"
                            )
                        names.append(entry.name)
            finally:
                with contextlib.suppress(OSError):
                    os.close(scan_fd)
            for name in sorted(names):
                if name in _IGNORE_FILE_NAMES:
                    continue
                copy_entry(
                    directory_fd,
                    name,
                    destination / name,
                    source_parts=(*source_parts, name),
                    project_root_parts=project_root_parts,
                    frames=local_frames,
                    depth=depth + 1,
                    explicit=False,
                )

        def copy_entry(
            parent_fd: int,
            name: str,
            destination: Path,
            *,
            source_parts: tuple[str, ...],
            project_root_parts: tuple[str, ...],
            frames: list[tuple[tuple[str, ...], GitIgnoreSpec]],
            depth: int,
            explicit: bool,
        ) -> bool:
            metadata = os.stat(
                name,
                dir_fd=parent_fd,
                follow_symlinks=False,
            )
            if stat.S_ISLNK(metadata.st_mode):
                if explicit:
                    raise ValueError("ast-outline snapshot target may not be a symlink")
                return False
            if stat.S_ISDIR(metadata.st_mode):
                if not explicit and (
                    name in _SNAPSHOT_ALWAYS_SKIP_DIRS
                    or is_ignored(source_parts, is_dir=True, frames=frames)
                ):
                    return False
                child_fd = os.open(name, directory_flags, dir_fd=parent_fd)
                try:
                    if not stat.S_ISDIR(os.fstat(child_fd).st_mode):
                        raise ValueError("ast-outline snapshot directory changed type")
                    if destination.exists() and not destination.is_dir():
                        raise ValueError("ast-outline snapshot path changed type")
                    copy_directory(
                        child_fd,
                        destination,
                        source_parts=source_parts,
                        project_root_parts=project_root_parts,
                        frames=frames,
                        depth=depth,
                    )
                finally:
                    os.close(child_fd)
                return True
            if stat.S_ISREG(metadata.st_mode):
                if not explicit:
                    supported = (
                        Path(name).suffix.lower() in _AST_SOURCE_SUFFIXES
                        or name in _AST_SOURCE_BASENAMES
                    )
                    if not supported or is_ignored(
                        source_parts, is_dir=False, frames=frames
                    ):
                        return False
                if destination.exists():
                    if not destination.is_file():
                        raise ValueError("ast-outline snapshot path changed type")
                    return False
                file_fd = os.open(name, file_flags, dir_fd=parent_fd)
                try:
                    copy_regular(file_fd, destination)
                finally:
                    os.close(file_fd)
                return False
            if explicit:
                raise ValueError("ast-outline snapshot target is not a regular file or directory")
            return False

        with tempfile.TemporaryDirectory(
            prefix="mini-loop-ast-",
            dir=self._snapshot_temp_parent(root),
        ) as directory:
            snapshot_root = Path(directory) / "workspace"
            snapshot_root.mkdir()
            root_fd = os.open(root, directory_flags)
            try:
                for candidate in sorted(
                    candidate_list,
                    key=lambda path: (len(path.relative_to(root).parts), str(path)),
                ):
                    check_deadline()
                    relative = candidate.relative_to(root)
                    relative_parts = relative.parts
                    destination = snapshot_root / relative
                    if relative_parts:
                        parent_fd = open_parent(root_fd, relative_parts[:-1])
                        name = relative_parts[-1]
                        metadata = os.stat(
                            name, dir_fd=parent_fd, follow_symlinks=False
                        )
                    else:
                        parent_fd = os.dup(root_fd)
                        name = ""
                        metadata = os.fstat(parent_fd)
                    try:
                        if stat.S_ISLNK(metadata.st_mode):
                            raise ValueError(
                                "ast-outline snapshot target may not be a symlink"
                            )
                        if stat.S_ISREG(metadata.st_mode):
                            if destination.exists():
                                continue
                            file_fd = os.open(name, file_flags, dir_fd=parent_fd)
                            try:
                                copy_regular(file_fd, destination)
                            finally:
                                os.close(file_fd)
                            continue
                        if not stat.S_ISDIR(metadata.st_mode):
                            raise ValueError(
                                "ast-outline snapshot target is not a regular file or directory"
                            )

                        project_root_parts = find_project_root(
                            root_fd, relative_parts
                        )
                        project_fd = open_parent(root_fd, project_root_parts)
                        try:
                            root_controls = read_ignore_controls(
                                project_fd, project_root_parts
                            )
                        finally:
                            os.close(project_fd)
                        root_spec = GitIgnoreSpec.from_lines(
                            [*_DEFAULT_IGNORE_PATTERNS, *ignore_lines(root_controls)]
                        )
                        project_destination = snapshot_root.joinpath(
                            *project_root_parts
                        )
                        project_destination.mkdir(parents=True, exist_ok=True)
                        (project_destination / ".git").mkdir(exist_ok=True)
                        copy_controls(root_controls, project_destination)

                        directory_fd = (
                            os.dup(parent_fd)
                            if not name
                            else os.open(name, directory_flags, dir_fd=parent_fd)
                        )
                        try:
                            copy_directory(
                                directory_fd,
                                destination,
                                source_parts=relative_parts,
                                project_root_parts=project_root_parts,
                                frames=[(project_root_parts, root_spec)],
                                depth=len(relative_parts),
                            )
                        finally:
                            os.close(directory_fd)
                    finally:
                        os.close(parent_fd)
            finally:
                os.close(root_fd)

            snapshot_paths = tuple(
                snapshot_root / candidate.relative_to(root)
                for candidate in candidate_list
            )
            replacements = ((str(snapshot_root), str(root)),)
            yield snapshot_paths, replacements

    @contextlib.contextmanager
    def _snapshot_workspace_file(
        self, root: Path, candidate: Path
    ) -> Iterator[tuple[Path, tuple[tuple[str, str], ...]]]:
        with self._snapshot_workspace_paths(root, (candidate,)) as snapshot:
            paths, replacements = snapshot
            target = paths[0]
            if not target.is_file():
                raise ValueError("show_symbol requires a regular source file")
            yield target, replacements

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
        path_rewrites: tuple[tuple[str, str], ...] = (),
    ) -> AstContextResult:
        # The workspace boundary must be checked before even the version probe:
        # otherwise a model-writable executable gets one host-code execution
        # before the real command is rejected.
        probe = self.probe(cwd=root)
        if probe.status != "applied":
            return AstContextResult(
                probe.status, operation, probe=probe, message=probe.message
            )
        process = self._run(argv, cwd=root)
        if process.status != "applied":
            message = _rewrite_path_strings(process.message, path_rewrites)
            return AstContextResult(
                process.status,
                operation,
                probe=probe,
                message=message,
                output_bytes=len(process.stdout.encode("utf-8")),
            )
        if process.returncode != 0:
            detail = process.stderr.strip() or process.stdout.strip()
            detail = _rewrite_path_strings(detail, path_rewrites)
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
        if path_rewrites:
            payload = _rewrite_path_strings(payload, path_rewrites)
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
        try:
            with self._snapshot_workspace_paths(
                root, (Path(path) for path in safe_paths)
            ) as frozen:
                snapshot_paths, replacements = frozen
                argv = ["digest", "--format", density]
                if include_imports:
                    argv.append("--imports")
                argv.extend(["--json", "--", *(str(path) for path in snapshot_paths)])
                return self._invoke(
                    operation,
                    "digest",
                    argv,
                    root,
                    include_imports=include_imports,
                    path_rewrites=replacements,
                )
        except (OSError, ValueError) as exc:
            return self._path_error(operation, exc)

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
        try:
            with self._snapshot_workspace_paths(
                root, (Path(path) for path in safe_paths)
            ) as frozen:
                snapshot_paths, replacements = frozen
                argv = ["outline"]
                if view in {"public", "minimal"}:
                    argv.append("--no-private")
                if view == "minimal":
                    argv.extend(["--no-fields", "--no-docs", "--no-attrs"])
                if include_imports:
                    argv.append("--imports")
                argv.extend(["--json", "--", *(str(path) for path in snapshot_paths)])
                return self._invoke(
                    operation,
                    "outline",
                    argv,
                    root,
                    include_imports=include_imports,
                    path_rewrites=replacements,
                )
        except (OSError, ValueError) as exc:
            return self._path_error(operation, exc)

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
            safe_symbols = self._validate_strings(symbols, "symbols")
            if not isinstance(path_or_glob, str):
                raise ValueError("path_or_glob must be a string")
            if any(char in path_or_glob for char in _GLOB_CHARS):
                targets = self._safe_glob_paths(root, path_or_glob)
                if not targets:
                    return AstContextResult(
                        "no_match",
                        operation,
                        data={"matches": []},
                        message="basename glob matched no workspace files",
                    )
                if len(targets) > 1:
                    visible: list[str] = []
                    visible_bytes = 0
                    for target in targets:
                        relative = str(Path(target).relative_to(root))
                        encoded = len(relative.encode("utf-8"))
                        if (
                            len(visible) >= _MAX_AMBIGUOUS_MATCHES
                            or visible_bytes + encoded > _MAX_AMBIGUOUS_BYTES
                        ):
                            break
                        visible.append(relative)
                        visible_bytes += encoded
                    truncated = len(visible) < len(targets)
                    return AstContextResult(
                        "ambiguous",
                        operation,
                        data={
                            "matches": visible,
                            "total_match_count": len(targets),
                            "truncated": truncated,
                        },
                        message=(
                            f"basename glob matched {len(targets)} files; "
                            "choose one explicit path"
                        ),
                    )
                safe_target = targets[0]
            else:
                safe_target = self._safe_path(root, path_or_glob)
        except (OSError, ValueError) as exc:
            return self._path_error(operation, exc)
        try:
            with self._snapshot_workspace_file(
                root, Path(safe_target)
            ) as frozen:
                snapshot, replacements = frozen
                argv = ["show"]
                if signature_only:
                    argv.append("--signature")
                argv.extend(["--json", "--", str(snapshot), *safe_symbols])
                return self._invoke(
                    operation,
                    "show",
                    argv,
                    root,
                    path_rewrites=replacements,
                )
        except (OSError, ValueError) as exc:
            return self._path_error(operation, exc)

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

        try:
            with self._snapshot_workspace_paths(
                root, (Path(path) for path in safe_paths)
            ) as frozen:
                snapshot_paths, replacements = frozen
                argv = ["grep", "--json"]
                if safe_kinds:
                    argv.append(f"--kind={','.join(safe_kinds)}")
                if max_per_file is not None:
                    argv.append(f"--max-count={max_per_file}")
                # Attached -e values remain one argv even if a pattern begins with '-'.
                # The first -e is promoted by ast-outline's documented normalizer.
                argv.extend(f"--expression={pattern}" for pattern in safe_patterns)
                argv.extend(["--", *(str(path) for path in snapshot_paths)])
                return self._invoke(
                    operation,
                    "grep",
                    argv,
                    root,
                    path_rewrites=replacements,
                )
        except (OSError, ValueError) as exc:
            return self._path_error(operation, exc)


def _rewrite_path_strings(value: Any, replacements: tuple[tuple[str, str], ...]) -> Any:
    """Remove harness-private snapshot paths from an ast-outline JSON payload."""

    if isinstance(value, str):
        for source, target in replacements:
            value = value.replace(source, target)
        return value
    if isinstance(value, list):
        return [_rewrite_path_strings(item, replacements) for item in value]
    if isinstance(value, dict):
        return {
            key: _rewrite_path_strings(item, replacements)
            for key, item in value.items()
        }
    return value


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
                "Workspace source file, or a non-recursive basename glob in a static "
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
            "Read exact source or signature for named symbols inside one workspace source file or non-recursive basename glob.",
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

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: each invocation re-verifies the pinned binary hash before spawning; the check is the operation, not an observer of it."
)
