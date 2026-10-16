"""Explicit Temporal transport settings shared by the Task API and Worker.

Only CLI entry points load environment/files. Library defaults remain plaintext
for local development. Credentials never enter Workflow inputs or metadata.
Files are read once; rotate credentials by constructing a new Client.
"""

from __future__ import annotations

import asyncio
from collections.abc import Mapping
from dataclasses import dataclass, field
import ipaddress
import os
import re
import stat
from urllib.parse import urlsplit

from temporalio.client import Client
from temporalio.service import TLSConfig

PREFIX = "MINILOOP_TASK_TEMPORAL_"
MAX_CERT_BYTES = 1024 * 1024
MAX_API_KEY_BYTES = 16 * 1024
STARTUP_TIMEOUT_SECONDS = 10
_HOST_LABEL = re.compile(r"[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\Z")
_SECURITY_FIELDS = (
    "CA_CERT_FILE", "CLIENT_CERT_FILE", "CLIENT_KEY_FILE", "SERVER_NAME", "API_KEY", "API_KEY_FILE",
)


class TemporalConfigurationError(ValueError):
    """A fixed configuration code safe for CLI and server startup logs."""


class TemporalConnectionError(RuntimeError):
    """A fixed connection code that intentionally hides SDK error contents."""


def _valid_host(value: str) -> bool:
    if not value or len(value) > 253 or not value.isascii():
        return False
    try:
        ipaddress.ip_address(value)
        return "%" not in value
    except ValueError:
        return all(_HOST_LABEL.fullmatch(label) for label in value.removesuffix(".").split("."))


def validate_address(address: str) -> None:
    """Require a host:port, including bracketed IPv6, without URL credentials."""
    try:
        if (not isinstance(address, str) or len(address) > 1024 or not address.isascii()
                or any(ord(c) <= 32 or ord(c) == 127 or c in "/?#@\\" for c in address)):
            raise ValueError()
        parsed = urlsplit("//" + address)
        if (parsed.hostname is None or not _valid_host(parsed.hostname)
                or parsed.port is None or not 1 <= parsed.port <= 65535
                or parsed.username is not None or parsed.password is not None
                or parsed.path or parsed.query or parsed.fragment):
            raise ValueError()
    except (ValueError, TypeError):
        raise TemporalConfigurationError("temporal_address_invalid") from None


def _read_file(path: str, *, limit: int, code: str) -> bytes:
    """Read bounded regular files; following Secret-mount symlinks is supported."""
    descriptor = None
    try:
        if not isinstance(path, str) or not path or len(path) > 4096 or "\0" in path:
            raise ValueError()
        # Nonblocking open prevents a misconfigured FIFO from hanging startup.
        descriptor = os.open(path, os.O_RDONLY | os.O_NONBLOCK | getattr(os, "O_CLOEXEC", 0))
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= limit:
            raise ValueError()
        with os.fdopen(descriptor, "rb") as stream:
            descriptor = None
            content = stream.read(limit + 1)
        if not 0 < len(content) <= limit:
            raise ValueError()
        return content
    except (OSError, ValueError, TypeError):
        raise TemporalConfigurationError(code) from None
    finally:
        if descriptor is not None:
            os.close(descriptor)


@dataclass(frozen=True)
class TemporalConnectionSettings:
    tls_enabled: bool = False
    server_root_ca_cert: bytes | None = field(default=None, repr=False)
    client_cert: bytes | None = field(default=None, repr=False)
    client_private_key: bytes | None = field(default=None, repr=False)
    server_name: str | None = None
    api_key: str | None = field(default=None, repr=False)

    def __post_init__(self) -> None:
        if type(self.tls_enabled) is not bool:
            raise TemporalConfigurationError("temporal_tls_invalid") from None
        fields = (self.server_root_ca_cert, self.client_cert, self.client_private_key, self.server_name, self.api_key)
        if not self.tls_enabled and any(value is not None for value in fields):
            raise TemporalConfigurationError("temporal_tls_required") from None
        if (self.client_cert is None) != (self.client_private_key is None):
            raise TemporalConfigurationError("temporal_mtls_pair_required") from None
        for value, code in (
            (self.server_root_ca_cert, "temporal_ca_cert_invalid"),
            (self.client_cert, "temporal_client_cert_invalid"),
            (self.client_private_key, "temporal_client_key_invalid"),
        ):
            if value is not None and (type(value) is not bytes or not 0 < len(value) <= MAX_CERT_BYTES):
                raise TemporalConfigurationError(code) from None
        if self.server_name is not None and (not isinstance(self.server_name, str) or not _valid_host(self.server_name)):
            raise TemporalConfigurationError("temporal_server_name_invalid") from None
        if self.api_key is not None and (
            not isinstance(self.api_key, str) or not 0 < len(self.api_key) <= MAX_API_KEY_BYTES
            or any(not 33 <= ord(c) <= 126 for c in self.api_key)
        ):
            raise TemporalConfigurationError("temporal_api_key_invalid") from None

    @classmethod
    def from_env(cls, environ: Mapping[str, str] | None = None) -> TemporalConnectionSettings:
        """Explicit CLI-only loading; never called by the connection factory."""
        env = os.environ if environ is None else environ
        tls = env.get(PREFIX + "TLS", "false")
        if tls not in ("true", "false"):
            raise TemporalConfigurationError("temporal_tls_invalid") from None
        if tls == "false" and any(PREFIX + name in env for name in _SECURITY_FIELDS):
            raise TemporalConfigurationError("temporal_tls_required") from None
        if PREFIX + "API_KEY" in env and PREFIX + "API_KEY_FILE" in env:
            raise TemporalConfigurationError("temporal_api_key_ambiguous") from None
        certificates = {}
        for name, field_name, code in (
            ("CA_CERT_FILE", "server_root_ca_cert", "temporal_ca_cert_file_invalid"),
            ("CLIENT_CERT_FILE", "client_cert", "temporal_client_cert_file_invalid"),
            ("CLIENT_KEY_FILE", "client_private_key", "temporal_client_key_file_invalid"),
        ):
            if PREFIX + name in env:
                certificates[field_name] = _read_file(env[PREFIX + name], limit=MAX_CERT_BYTES, code=code)
        api_key = env.get(PREFIX + "API_KEY")
        if PREFIX + "API_KEY_FILE" in env:
            raw = _read_file(env[PREFIX + "API_KEY_FILE"], limit=MAX_API_KEY_BYTES + 2,
                             code="temporal_api_key_file_invalid")
            # Accept exactly one conventional final line ending, not whitespace
            # trimming that could hide an invalid or multi-line secret.
            raw = raw[:-2] if raw.endswith(b"\r\n") else raw[:-1] if raw.endswith(b"\n") else raw
            try:
                api_key = raw.decode("ascii")
            except UnicodeError:
                raise TemporalConfigurationError("temporal_api_key_invalid") from None
        return cls(tls_enabled=tls == "true", **certificates,
                   server_name=env.get(PREFIX + "SERVER_NAME"), api_key=api_key)

    def tls_config(self) -> bool | TLSConfig:
        if not self.tls_enabled:
            return False
        return TLSConfig(server_root_ca_cert=self.server_root_ca_cert, domain=self.server_name,
                         client_cert=self.client_cert, client_private_key=self.client_private_key)


async def connect_temporal(
    address: str,
    *,
    namespace: str = "default",
    identity: str | None = None,
    connection: TemporalConnectionSettings | None = None,
) -> Client:
    """Eager verified connection with one startup budget and safe failures.

    API-key metadata and certificate bytes are only Client transport settings.
    A successful connection does not prove authorization for every namespace RPC.
    """
    validate_address(address)
    settings = connection if connection is not None else TemporalConnectionSettings()
    if not isinstance(settings, TemporalConnectionSettings):
        raise TemporalConfigurationError("temporal_connection_settings_invalid") from None
    try:
        async with asyncio.timeout(STARTUP_TIMEOUT_SECONDS):
            return await Client.connect(address, namespace=namespace, identity=identity, lazy=False,
                                        tls=settings.tls_config(), api_key=settings.api_key)
    except asyncio.CancelledError:
        raise
    except TimeoutError:
        raise TemporalConnectionError("temporal_connection_timeout") from None
    except Exception:
        raise TemporalConnectionError("temporal_connection_failed") from None
