"""Transport configuration and CLI wiring; no Temporal or model service."""

import asyncio
from dataclasses import FrozenInstanceError
import os
import sys
import traceback

import pytest
from temporalio.service import TLSConfig

from mini_loop_deepagents.task_server import api, connection, worker

P = connection.PREFIX


@pytest.fixture(autouse=True)
def isolated_temporal_environment(monkeypatch):
    for name in tuple(os.environ):
        if name.startswith(P):
            monkeypatch.delenv(name)


@pytest.mark.parametrize("value", ["", "TRUE", "False", "1", "yes", " true", True])
def test_tls_environment_is_explicit_and_strict(value):
    with pytest.raises(connection.TemporalConfigurationError, match="^temporal_tls_invalid$"):
        connection.TemporalConnectionSettings.from_env({P + "TLS": value})


@pytest.mark.parametrize("name", ["CA_CERT_FILE", "CLIENT_CERT_FILE", "CLIENT_KEY_FILE",
                                  "SERVER_NAME", "API_KEY", "API_KEY_FILE"])
def test_plaintext_rejects_every_security_field_even_when_empty(name):
    with pytest.raises(connection.TemporalConfigurationError, match="^temporal_tls_required$"):
        connection.TemporalConnectionSettings.from_env({P + "TLS": "false", P + name: ""})


@pytest.mark.parametrize("kwargs", [
    {"tls_enabled": "true"}, {"api_key": "secret"},
    {"tls_enabled": True, "client_cert": b"cert"},
    {"tls_enabled": True, "client_private_key": b"key"},
    {"tls_enabled": True, "server_root_ca_cert": b""},
    {"tls_enabled": True, "server_root_ca_cert": "not-bytes"},
    {"tls_enabled": True, "client_cert": b"cert", "client_private_key": bytearray(b"key")},
    {"tls_enabled": True, "api_key": ""},
    {"tls_enabled": True, "api_key": "Bearer secret"},
    {"tls_enabled": True, "api_key": "secret\n"},
    {"tls_enabled": True, "api_key": "secret\x7f"},
    {"tls_enabled": True, "api_key": "密钥"},
    {"tls_enabled": True, "api_key": "x" * (connection.MAX_API_KEY_BYTES + 1)},
    {"tls_enabled": True, "server_name": "https://temporal.example"},
    {"tls_enabled": True, "server_name": "temporal.example:7233"},
    {"tls_enabled": True, "server_name": "bad name"},
])
def test_explicit_library_settings_have_the_same_fail_closed_rules(kwargs):
    with pytest.raises(connection.TemporalConfigurationError):
        connection.TemporalConnectionSettings(**kwargs)


@pytest.mark.parametrize("address", [
    "localhost", "localhost:", "localhost:0", "localhost:65536", "https://localhost:7233",
    "http://localhost:7233", "user:secret@localhost:7233", "localhost:7233/path",
    "localhost:7233?", "localhost:7233#", "localhost:7233 ", "localhost:7233\n",
    "::1:7233", "[::1]", "[::1]:0", "[::1]:7233/path", "bad..name:7233", None,
])
def test_target_rejects_url_scheme_credentials_and_invalid_host_port(address):
    with pytest.raises(connection.TemporalConfigurationError, match="^temporal_address_invalid$"):
        connection.validate_address(address)


@pytest.mark.parametrize("address", ["localhost:7233", "temporal.example:7233", "127.0.0.1:7233", "[::1]:7233"])
def test_target_accepts_explicit_dns_ipv4_and_bracketed_ipv6(address):
    connection.validate_address(address)


def test_settings_are_frozen_and_all_credential_material_is_hidden_from_repr():
    settings = connection.TemporalConnectionSettings(tls_enabled=True, server_root_ca_cert=b"private-ca-data",
        client_cert=b"client-cert-data", client_private_key=b"private-key-data", api_key="sensitive-api-key")
    display = repr(settings)
    assert all(secret not in display for secret in ("private-ca-data", "client-cert-data", "private-key-data", "sensitive-api-key"))
    with pytest.raises(FrozenInstanceError):
        settings.api_key = "another"


@pytest.mark.parametrize("ending", [b"", b"\n", b"\r\n"])
def test_files_are_loaded_once_and_secret_mount_symlinks_work(tmp_path, ending):
    paths = {}
    for name, content in (("CA_CERT_FILE", b"ca"), ("CLIENT_CERT_FILE", b"cert"),
                          ("CLIENT_KEY_FILE", b"private-key"), ("API_KEY_FILE", b"key-one" + ending)):
        target = tmp_path / name
        target.write_bytes(content)
        link = tmp_path / (name + "-mount")
        link.symlink_to(target)
        paths[P + name] = str(link)
    env = {P + "TLS": "true", P + "SERVER_NAME": "temporal.internal", **paths}
    settings = connection.TemporalConnectionSettings.from_env(env)
    tls = settings.tls_config()
    assert isinstance(tls, TLSConfig)
    assert (tls.server_root_ca_cert, tls.client_cert, tls.client_private_key, tls.domain) == (
        b"ca", b"cert", b"private-key", "temporal.internal")
    assert tls.verification_server_name is None
    assert settings.api_key == "key-one"
    (tmp_path / "API_KEY_FILE").write_bytes(b"key-two")
    assert settings.api_key == "key-one"
    assert connection.TemporalConnectionSettings.from_env(env).api_key == "key-two"


@pytest.mark.parametrize("content", [b"", b"key\n\n", b"key\r", b"two lines\n", b"\xff"])
def test_key_file_does_not_strip_invalid_or_multiline_credentials(tmp_path, content):
    path = tmp_path / "key"
    path.write_bytes(content)
    with pytest.raises(connection.TemporalConfigurationError):
        connection.TemporalConnectionSettings.from_env({P + "TLS": "true", P + "API_KEY_FILE": str(path)})


def test_key_environment_and_file_are_ambiguous_even_if_one_is_empty():
    with pytest.raises(connection.TemporalConfigurationError, match="^temporal_api_key_ambiguous$"):
        connection.TemporalConnectionSettings.from_env({P + "TLS": "true", P + "API_KEY": "",
                                                       P + "API_KEY_FILE": "unread-secret-path"})


@pytest.mark.parametrize("kind", ["missing", "empty", "large", "directory", "fifo"])
def test_bad_certificate_files_fail_without_content_or_path_in_diagnostics(tmp_path, kind):
    path = tmp_path / "sensitive-deployment-path"
    if kind == "empty":
        path.write_bytes(b"")
    elif kind == "large":
        path.write_bytes(b"secret-certificate" + b"x" * connection.MAX_CERT_BYTES)
    elif kind == "directory":
        path.mkdir()
    elif kind == "fifo":
        if not hasattr(os, "mkfifo"):
            pytest.skip("FIFO validation is POSIX-specific")
        os.mkfifo(path)
    with pytest.raises(connection.TemporalConfigurationError) as raised:
        connection.TemporalConnectionSettings.from_env({P + "TLS": "true", P + "CA_CERT_FILE": str(path)})
    diagnostic = "".join(traceback.format_exception(raised.value))
    assert str(raised.value) == "temporal_ca_cert_file_invalid"
    assert str(path) not in diagnostic and "secret-certificate" not in diagnostic


@pytest.mark.asyncio
async def test_default_factory_ignores_ambient_security_environment_and_is_eager(monkeypatch):
    monkeypatch.setenv(P + "TLS", "invalid-ambient-config")
    monkeypatch.setenv(P + "API_KEY", "ambient-secret")
    captured, result = [], object()

    async def connect(*args, **kwargs):
        captured.append((args, kwargs))
        return result

    monkeypatch.setattr(connection.Client, "connect", connect)
    assert await connection.connect_temporal("localhost:7233", namespace="selected-ns", identity="worker-a") is result
    assert captured == [(("localhost:7233",), {"namespace": "selected-ns", "identity": "worker-a",
                        "lazy": False, "tls": False, "api_key": None})]
    assert connection.STARTUP_TIMEOUT_SECONDS == 10


@pytest.mark.asyncio
async def test_tls_and_api_key_use_sdk_transport_fields_without_rpc_metadata_override(monkeypatch):
    captured = []

    async def connect(*args, **kwargs):
        captured.append(kwargs)
        return object()

    monkeypatch.setattr(connection.Client, "connect", connect)
    settings = connection.TemporalConnectionSettings(tls_enabled=True, api_key="key", server_name="temporal.internal")
    await connection.connect_temporal("127.0.0.1:7233", connection=settings)
    value = captured[0]
    assert value["api_key"] == "key" and value["lazy"] is False
    assert isinstance(value["tls"], TLSConfig) and value["tls"].domain == "temporal.internal"
    assert value["tls"].verification_server_name is None
    assert "rpc_metadata" not in value


@pytest.mark.asyncio
async def test_factory_hides_sdk_exception_contents_and_chaining(monkeypatch):
    async def connect(*args, **kwargs):
        raise RuntimeError("provider echoed sensitive-api-key and private-key-content")

    monkeypatch.setattr(connection.Client, "connect", connect)
    with pytest.raises(connection.TemporalConnectionError) as raised:
        await connection.connect_temporal("localhost:7233")
    diagnostic = "".join(traceback.format_exception(raised.value))
    assert str(raised.value) == "temporal_connection_failed"
    assert "sensitive-api-key" not in diagnostic and "private-key-content" not in diagnostic


@pytest.mark.asyncio
async def test_factory_startup_budget_cancels_connection_and_reports_safe_timeout(monkeypatch):
    cancelled = asyncio.Event()

    async def connect(*args, **kwargs):
        try:
            await asyncio.Event().wait()
        finally:
            cancelled.set()

    monkeypatch.setattr(connection.Client, "connect", connect)
    monkeypatch.setattr(connection, "STARTUP_TIMEOUT_SECONDS", 0.01)
    with pytest.raises(connection.TemporalConnectionError, match="^temporal_connection_timeout$"):
        await connection.connect_temporal("localhost:7233")
    assert cancelled.is_set()


@pytest.mark.asyncio
async def test_factory_preserves_caller_cancellation(monkeypatch):
    entered = asyncio.Event()

    async def connect(*args, **kwargs):
        entered.set()
        await asyncio.Event().wait()

    monkeypatch.setattr(connection.Client, "connect", connect)
    task = asyncio.create_task(connection.connect_temporal("localhost:7233"))
    await entered.wait()
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task


def test_worker_cli_loads_settings_and_preserves_namespace_identity(monkeypatch):
    captured = []
    monkeypatch.setenv(P + "TLS", "true")
    monkeypatch.setenv(P + "API_KEY", "deployment-key")
    monkeypatch.setattr(worker, "run_process", lambda *a, **k: captured.append((a, k)))
    monkeypatch.setattr(sys, "argv", ["worker", "--address", "temporal.example:7233", "--namespace", "owned-ns",
                                     "--task-queue", "queue", "--identity", "worker-a"])
    worker.main()
    args, kwargs = captured[0]
    assert args == ("temporal.example:7233", "queue", "worker-a")
    assert kwargs["namespace"] == "owned-ns"
    assert kwargs["connection"].tls_enabled and kwargs["connection"].api_key == "deployment-key"


def test_library_process_entry_forwards_explicit_settings_without_loading_env(monkeypatch):
    settings = connection.TemporalConnectionSettings(tls_enabled=True, api_key="explicit-key")
    captured = []

    class NoSignals:
        def __init__(self, *args):
            pass
        def __enter__(self):
            return self
        def __exit__(self, *args):
            pass
        def bind(self, *args):
            pass

    async def serve(*args, **kwargs):
        captured.append(kwargs["connection"])

    monkeypatch.setattr(worker, "ProcessLifecycle", NoSignals)
    monkeypatch.setattr(worker, "serve", serve)
    monkeypatch.setattr(connection.TemporalConnectionSettings, "from_env",
                        lambda *a: pytest.fail("library entry must not load deployment environment"))
    worker.run_process("localhost:7233", "queue", "worker-a", connection=settings)
    worker.run_process("localhost:7233", "queue", "worker-a")
    assert captured == [settings, None]


@pytest.mark.parametrize("queue_arguments,expected_queue", [
    ([], "mini-loop-task"),
    (["--task-queue", "deployment-queue"], "deployment-queue"),
])
def test_api_cli_loads_shared_settings_into_lifespan_without_a_server(monkeypatch, queue_arguments, expected_queue):
    import uvicorn
    captured = []
    monkeypatch.setenv(P + "TLS", "true")
    monkeypatch.setenv(P + "API_KEY", "deployment-key")
    monkeypatch.setenv("MINILOOP_TASK_SERVER_CURSOR_SECRET", "c" * 40)
    monkeypatch.setattr(api, "load_tokens", lambda *a: {"a" * 40: "owner"})
    monkeypatch.setattr(sys, "argv", ["api", "--temporal-address", "temporal.example:7233", "--namespace", "owned-ns", *queue_arguments])

    async def connect(*args, **kwargs):
        captured.append((args, kwargs))
        return object()

    def run(app, **kwargs):
        async def startup():
            async with app.router.lifespan_context(app):
                assert app.state.task_service is not None
                assert app.state.task_service.task_queue == expected_queue
        asyncio.run(startup())

    monkeypatch.setattr(api, "connect_temporal", connect)
    monkeypatch.setattr(uvicorn, "run", run)
    api.main()
    args, kwargs = captured[0]
    assert args == ("temporal.example:7233",) and kwargs["namespace"] == "owned-ns"
    assert kwargs["connection"].tls_enabled and kwargs["connection"].api_key == "deployment-key"


@pytest.mark.parametrize("entrypoint", [api, worker])
def test_cli_configuration_error_does_not_echo_secret_values(monkeypatch, capsys, entrypoint):
    monkeypatch.setenv(P + "TLS", "false")
    monkeypatch.setenv(P + "API_KEY", "never-print-this-key")
    monkeypatch.setattr(api, "load_tokens", lambda *a: {"a" * 40: "owner"})
    arguments = ["api"] if entrypoint is api else ["worker", "--address", "localhost:7233",
                                                   "--task-queue", "q", "--identity", "i"]
    monkeypatch.setattr(sys, "argv", arguments)
    with pytest.raises(SystemExit) as raised:
        entrypoint.main()
    assert raised.value.code == 2
    diagnostic = capsys.readouterr().err
    assert "temporal_tls_required" in diagnostic and "never-print-this-key" not in diagnostic
