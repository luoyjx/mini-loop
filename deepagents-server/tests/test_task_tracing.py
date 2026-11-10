"""Fail-closed Task tracing settings, redaction and Temporal plugin wiring."""

from __future__ import annotations

import traceback
from unittest.mock import patch

import langsmith
from langsmith import tracing_context
from langchain_core.language_models.fake_chat_models import FakeMessagesListChatModel
from langchain_core.messages import AIMessage
import pytest
import requests

pytest.importorskip("temporalio.contrib.langsmith")

from mini_loop_deepagents.task_server.observability import DeploymentIdentity
from mini_loop_deepagents.task_server import tracing


KEY = "synthetic-langsmith-key"
PROJECT = "mini-loop-task-production"


def enabled_env(**overrides):
    values = {
        tracing.TRACING_ENV: "true",
        tracing.PROJECT_ENV: PROJECT,
        tracing.API_KEY_ENV: KEY,
        tracing.RETENTION_ENV: "30",
    }
    values.update(overrides)
    return values


def test_tracing_is_explicitly_disabled_by_default_and_secret_is_not_represented():
    assert tracing.TaskTracingSettings.from_env({}) == tracing.TaskTracingSettings()
    settings = tracing.TaskTracingSettings.from_env(enabled_env())
    assert settings.enabled and settings.project_name == PROJECT and settings.retention_days == 30
    assert KEY not in repr(settings)


@pytest.mark.parametrize("env,code", [
    ({tracing.TRACING_ENV: "1"}, "task_trace_enabled_invalid"),
    (enabled_env(**{tracing.PROJECT_ENV: "private project"}), "task_trace_project_invalid"),
    (enabled_env(**{tracing.API_KEY_ENV: ""}), "task_trace_api_key_invalid"),
    (enabled_env(**{tracing.ENDPOINT_ENV: "http://trace.invalid"}), "task_trace_endpoint_invalid"),
    (enabled_env(**{tracing.ENDPOINT_ENV: "https://user:secret@trace.invalid"}), "task_trace_endpoint_invalid"),
    (enabled_env(**{tracing.RETENTION_ENV: "0"}), "task_trace_retention_invalid"),
])
def test_invalid_trace_settings_fail_with_fixed_non_secret_codes(env, code):
    with pytest.raises(tracing.TaskTracingConfigurationError) as raised:
        tracing.TaskTracingSettings.from_env(env)
    assert str(raised.value) == code
    diagnostic = "".join(traceback.format_exception(raised.value))
    assert KEY not in diagnostic and "user:secret" not in diagnostic


def test_trace_anonymizer_removes_content_credentials_and_bad_metadata_but_keeps_usage():
    private = "private prompt Authorization: Bearer test-secret"
    metadata = {
        "task_id": "task-" + "a" * 64,
        "temporalRunID": "11111111-1111-4111-8111-111111111111",
        "langgraph_node": "summarization",
        "task_owner": "private-owner@example.com",
        "private": private,
        "usage_metadata": {
            "input_tokens": 12, "output_tokens": 3, "total_tokens": 15,
            "input_token_details": {"cache_read": 2, "bad": -1},
            "raw_provider_response": private,
        },
    }
    safe = tracing.trace_anonymizer(metadata)
    assert safe == {
        "task_id": "task-" + "a" * 64,
        "temporalRunID": "11111111-1111-4111-8111-111111111111",
        "langgraph_node": "summarization",
        "usage_metadata": {
            "input_tokens": 12, "output_tokens": 3, "total_tokens": 15,
            "input_token_details": {"cache_read": 2},
        },
    }
    assert private not in repr(safe)
    assert tracing.trace_anonymizer({"error": private}) == {"error": "redacted"}


def test_plugin_hides_inputs_outputs_errors_runtime_and_carries_safe_temporal_metadata():
    settings = tracing.TaskTracingSettings.from_env(enabled_env())
    deployment = DeploymentIdentity("sha256:" + "d" * 64)
    plugins = tracing.create_trace_plugins("worker", deployment, settings)
    assert len(plugins) == 1
    plugin = plugins[0]
    interceptor = plugin.interceptors[0]
    client = interceptor._client
    assert client._hide_run_inputs({"messages": ["private prompt"]}) == {}
    assert client._hide_run_outputs({"content": "private result"}) == {}
    assert client._hide_run_error("credential=test-secret") == "redacted"
    assert client._omit_traced_runtime_info is True
    assert interceptor._add_temporal_runs is True
    assert interceptor._project_name == PROJECT
    assert interceptor._default_metadata == {
        "mini_loop_service_role": "worker",
        "mini_loop_protocol_version": "task-v2",
        "mini_loop_snapshot_schema": 2,
        "mini_loop_image_digest": "sha256:" + "d" * 64,
        "mini_loop_trace_retention_days": 30,
    }
    assert tracing.create_trace_plugins("api", deployment, tracing.TaskTracingSettings()) == ()


def test_content_free_client_scrubs_the_whole_sdk_operation_before_enqueueing():
    settings = tracing.TaskTracingSettings.from_env(enabled_env())
    client = tracing.create_trace_plugins(
        "worker", DeploymentIdentity("sha256:" + "d" * 64), settings,
    )[0].interceptors[0]._client
    private = "PRIVATE_PROMPT_OR_RESULT_SENTINEL"
    raw = {
        "id": "11111111-1111-4111-8111-111111111111",
        "serialized": {"repr": private},
        "outputs": {"content": private},
        "events": [{"name": private}],
        "attachments": {"secret.txt": ("text/plain", private.encode())},
        "extra": {
            "runtime": {"platform": private},
            "invocation_params": {"stop": private},
            "metadata": {
                "task_owner": private,
                "usage_metadata": {"input_tokens": 12, "output_tokens": 3, "total_tokens": 15},
            },
        },
        "tags": ["mini-loop-task", private],
    }
    with patch.object(langsmith.Client, "create_run") as create, \
            patch.object(langsmith.Client, "update_run") as update:
        client.create_run(private, {"messages": [private]}, "llm", **raw)
        client.update_run(raw["id"], name=private, error=private, inputs={"prompt": private}, **raw)

    create_payload = create.call_args.kwargs
    update_payload = update.call_args.kwargs
    rendered = repr((create_payload, update_payload))
    assert private not in rendered
    assert create_payload["name"] == update_payload["name"] == "redacted-run"
    assert create_payload["inputs"] == update_payload["inputs"] == {}
    assert create_payload["outputs"] == update_payload["outputs"] == {}
    assert update_payload["error"] == "redacted"
    assert create_payload["tags"] == update_payload["tags"] == ["mini-loop-task"]
    assert create_payload["extra"] == update_payload["extra"] == {
        "metadata": {
            "usage_metadata": {"input_tokens": 12, "output_tokens": 3, "total_tokens": 15},
        },
    }
    for payload in (create_payload, update_payload):
        assert "serialized" not in payload and "events" not in payload and "attachments" not in payload
        assert payload["dangerously_allow_filesystem"] is False


def test_actual_langchain_http_payload_is_content_free_but_retains_usage():
    class CaptureSession(requests.Session):
        def __init__(self):
            super().__init__()
            self.bodies = []

        def request(self, method, url, **kwargs):
            self.bodies.append(kwargs.get("data") or kwargs.get("json"))
            response = requests.Response()
            response.status_code = 200
            response._content = b"{}"
            response.headers = {}
            response.url = url
            return response

    session = CaptureSession()
    client = tracing._ContentFreeLangSmithClient(
        api_key=KEY, session=session, auto_batch_tracing=False,
        hide_inputs=True, hide_outputs=True, hide_metadata=tracing.sanitize_trace_metadata,
        anonymizer=tracing.trace_anonymizer, omit_traced_runtime_info=True,
    )
    model = FakeMessagesListChatModel(responses=[AIMessage(
        content="PRIVATE_RESULT_SENTINEL",
        usage_metadata={"input_tokens": 12, "output_tokens": 3, "total_tokens": 15},
    )])
    with tracing_context(client=client, project_name=PROJECT, enabled=True):
        model.invoke("PRIVATE_PROMPT_SENTINEL")

    rendered = "\n".join(
        body.decode() if isinstance(body, bytes) else str(body) for body in session.bodies
    )
    assert len(session.bodies) == 2
    assert "PRIVATE_PROMPT_SENTINEL" not in rendered
    assert "PRIVATE_RESULT_SENTINEL" not in rendered
    assert all(field not in rendered for field in ("serialized", "runtime", "attachments"))
    assert "input_tokens" in rendered and "total_tokens" in rendered
