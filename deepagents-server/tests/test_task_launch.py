"""Role-scoped secret files and same-process container CLI dispatch."""

import os
import signal
import sys
import traceback

import pytest

from mini_loop_deepagents.task_server import api, launch, worker


@pytest.fixture(autouse=True)
def isolate_secrets(monkeypatch):
    for name in ("MINILOOP_DEEPAGENTS_TOKENS", "MINILOOP_TASK_SERVER_CURSOR_SECRET", "OPENAI_API_KEY"):
        monkeypatch.delenv(name, raising=False)
        monkeypatch.delenv(name + "_FILE", raising=False)


def test_api_secret_files_support_secret_mount_symlinks_and_do_not_load_worker_credentials(tmp_path):
    token = tmp_path / "tokens"
    token.write_bytes(b'{"' + b"a" * 40 + b'":"owner"}\n')
    token_mount = tmp_path / "token-mount"
    token_mount.symlink_to(token)
    cursor = tmp_path / "cursor"
    cursor.write_bytes(b"c" * 40 + b"\r\n")
    env = {"MINILOOP_DEEPAGENTS_TOKENS_FILE": str(token_mount),
           "MINILOOP_TASK_SERVER_CURSOR_SECRET_FILE": str(cursor), "OPENAI_API_KEY_FILE": "/must-not-be-read"}
    launch.load_role_secrets("api", env)
    assert env["MINILOOP_DEEPAGENTS_TOKENS"] == token.read_text()
    assert env["MINILOOP_TASK_SERVER_CURSOR_SECRET"] == "c" * 40
    assert "OPENAI_API_KEY" not in env


@pytest.mark.parametrize("ending", [b"", b"\n", b"\r\n"])
def test_worker_reads_actual_openai_environment_name_without_expanding_unrelated_files(tmp_path, ending):
    key = tmp_path / "key"
    key.write_bytes(b"synthetic-openai-key" + ending)
    env = {"OPENAI_API_KEY_FILE": str(key), "UNTRUSTED_FILE": "/not-read",
           "MINILOOP_DEEPAGENTS_TOKENS_FILE": "/not-read"}
    launch.load_role_secrets("worker", env)
    assert env["OPENAI_API_KEY"] == "synthetic-openai-key"
    assert "UNTRUSTED" not in env and "MINILOOP_DEEPAGENTS_TOKENS" not in env


@pytest.mark.parametrize("role,variable", [
    ("api", "MINILOOP_DEEPAGENTS_TOKENS"), ("api", "MINILOOP_TASK_SERVER_CURSOR_SECRET"),
    ("worker", "OPENAI_API_KEY"),
])
@pytest.mark.parametrize("inline", ["", "inline-secret"])
def test_inline_and_file_conflicts_fail_without_reading_or_revealing_values(role, variable, inline):
    env = {variable: inline, variable + "_FILE": "/sensitive-path-that-does-not-exist"}
    with pytest.raises(launch.LaunchConfigurationError) as raised:
        launch.load_role_secrets(role, env)
    assert str(raised.value) == "task_secret_ambiguous"


@pytest.mark.parametrize("role,variable,limit", [
    ("api", "MINILOOP_DEEPAGENTS_TOKENS", 64 * 1024),
    ("api", "MINILOOP_TASK_SERVER_CURSOR_SECRET", 4 * 1024),
    ("worker", "OPENAI_API_KEY", 16 * 1024),
])
def test_role_file_size_limits_are_enforced_before_setting_environment(tmp_path, role, variable, limit):
    path = tmp_path / "private-path"
    path.write_bytes(b"x" * (limit + 1))
    env = {variable + "_FILE": str(path)}
    with pytest.raises(launch.LaunchConfigurationError) as raised:
        launch.load_role_secrets(role, env)
    assert str(raised.value) == "task_secret_file_invalid" and variable not in env
    assert str(path) not in "".join(traceback.format_exception(raised.value))


@pytest.mark.parametrize("content", [b"\n", b"key\n\n", b"key\r", b"key\x00", b"key with spaces", b"\xff"])
def test_scalar_file_does_not_trim_or_accept_invalid_secret_material(tmp_path, content):
    path = tmp_path / "key"
    path.write_bytes(content)
    env = {"OPENAI_API_KEY_FILE": str(path)}
    with pytest.raises(launch.LaunchConfigurationError):
        launch.load_role_secrets("worker", env)
    assert "OPENAI_API_KEY" not in env


def test_api_file_loading_does_not_leave_partial_environment_on_failure(tmp_path):
    tokens = tmp_path / "tokens"
    tokens.write_bytes(b"valid-file-content")
    cursor = tmp_path / "cursor"
    cursor.write_bytes(b"bad\n\n")
    env = {"MINILOOP_DEEPAGENTS_TOKENS_FILE": str(tokens), "MINILOOP_TASK_SERVER_CURSOR_SECRET_FILE": str(cursor)}
    original = dict(env)
    with pytest.raises(launch.LaunchConfigurationError):
        launch.load_role_secrets("api", env)
    assert env == original


def test_worker_loader_preserves_inline_behavior_without_a_file():
    env = {"OPENAI_API_KEY": "existing-inline-value"}
    launch.load_role_secrets("worker", env)
    assert env == {"OPENAI_API_KEY": "existing-inline-value"}


def test_entrypoint_calls_api_main_in_same_process_with_exact_arguments_and_restores_argv(monkeypatch, tmp_path):
    tokens = tmp_path / "tokens"
    tokens.write_bytes(b'{"' + b"a" * 40 + b'":"owner"}')
    monkeypatch.setenv("MINILOOP_DEEPAGENTS_TOKENS_FILE", str(tokens))
    original_argv, process_id = sys.argv, os.getpid()
    signals = (signal.getsignal(signal.SIGTERM), signal.getsignal(signal.SIGINT))
    captured = []

    def main():
        captured.append((list(sys.argv), os.getpid(), os.environ["MINILOOP_DEEPAGENTS_TOKENS"]))

    monkeypatch.setattr(api, "main", main)
    launch.main(["api", "--namespace", "private-ns", "--address=127.0.0.1:8123"])
    assert captured == [(["task-api", "--namespace", "private-ns", "--address=127.0.0.1:8123"],
                         process_id, tokens.read_text())]
    assert sys.argv is original_argv
    assert signals == (signal.getsignal(signal.SIGTERM), signal.getsignal(signal.SIGINT))


@pytest.mark.parametrize("explicit", [[], ["--identity", "chosen"], ["--identity=chosen"], ["--ident=chosen"]])
def test_worker_default_container_identity_and_explicit_identity_precedence(monkeypatch, explicit):
    captured = []
    monkeypatch.setattr(launch.socket, "gethostname", lambda: "container-host")
    monkeypatch.setattr(launch.os, "getpid", lambda: 1)
    monkeypatch.setattr(worker, "run_process", lambda *args, **kwargs: captured.append((args, kwargs)))
    monkeypatch.setattr(worker.TemporalConnectionSettings, "from_env", lambda: None)
    launch.main(["worker", "--address", "localhost:7233", "--task-queue", "queue", *explicit])
    assert captured[0][0] == ("localhost:7233", "queue", "chosen" if explicit else "container-host:1")


def test_entrypoint_preserves_role_fatal_exception_and_restores_argv(monkeypatch):
    original_argv = sys.argv
    failure = RuntimeError("fatal role failure")

    def fail():
        raise failure

    monkeypatch.setattr(api, "main", fail)
    with pytest.raises(RuntimeError) as raised:
        launch.main(["api"])
    assert raised.value is failure and sys.argv is original_argv


@pytest.mark.parametrize("argv", [[], ["unknown-secret-role"], ["sh", "-c", "payload"]])
def test_entrypoint_rejects_unknown_roles_with_a_fixed_error(argv):
    with pytest.raises(SystemExit) as raised:
        launch.main(argv)
    assert str(raised.value) == "task_launch_role_invalid"


def test_secret_failure_is_a_fixed_cli_error_and_does_not_call_role(monkeypatch):
    monkeypatch.setenv("OPENAI_API_KEY", "private-inline-secret")
    monkeypatch.setenv("OPENAI_API_KEY_FILE", "/private-file-path")
    monkeypatch.setattr(worker, "main", lambda: pytest.fail("invalid secret must not start Worker"))
    with pytest.raises(SystemExit) as raised:
        launch.main(["worker"])
    assert str(raised.value) == "task_secret_ambiguous"
    diagnostic = "".join(traceback.format_exception(raised.value))
    assert "private-inline-secret" not in diagnostic and "/private-file-path" not in diagnostic
