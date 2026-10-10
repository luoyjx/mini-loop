"""Pinned Source/SDK authentication contracts, without external IO."""

import asyncio
import os
import hashlib
import inspect
from unittest.mock import patch

import anthropic
import httpx
from mini_loop.providers import AnthropicCompatibleProvider


async def _source_auth_cases(scratch):
    """Keep Source construction intact; inject only the offline HTTP boundary."""
    profiles = [
        ("explicit-key", "explicit-key", {}, None),
        ("environment-key", None, {"ANTHROPIC_API_KEY": "env-key"}, None),
        ("environment-bearer", None, {"ANTHROPIC_AUTH_TOKEN": "env-token"}, None),
        ("environment-both", None, {"ANTHROPIC_API_KEY": "env-key", "ANTHROPIC_AUTH_TOKEN": "env-token"}, None),
        ("explicit-suppresses-env", "explicit-key", {"ANTHROPIC_API_KEY": "env-key", "ANTHROPIC_AUTH_TOKEN": "env-token"}, None),
        ("empty-falls-through", "", {"ANTHROPIC_AUTH_TOKEN": "env-token"}, None),
        ("custom-auth-only", None, {"ANTHROPIC_CUSTOM_HEADERS": "Authorization: Bearer custom-token\nX-Api-Key: custom-key"}, None),
        ("custom-overrides-static", "explicit-key", {"ANTHROPIC_CUSTOM_HEADERS": "X-Api-Key: custom-key\nAuthorization: Bearer custom-token\ninvalid-line\nX-Fixture: first\nX-Fixture: last"}, None),
        ("environment-endpoint", None, {"ANTHROPIC_API_KEY": "env-key", "ANTHROPIC_BASE_URL": "https://environment.invalid"}, None),
        ("explicit-endpoint", "explicit-key", {"ANTHROPIC_BASE_URL": "https://environment.invalid"}, "https://explicit.invalid"),
    ]
    real_client = anthropic.AsyncAnthropic
    rows = []
    for name, key, environment, endpoint in profiles:
        calls, kwargs_seen = [], []

        async def transport(request):
            calls.append(dict(url=str(request.url), headers={
                field: request.headers[field] for field in
                ("x-api-key", "authorization", "x-fixture") if field in request.headers}))
            return httpx.Response(200, json=dict(id="fixture", type="message", role="assistant",
                model="fixture-model", content=[dict(type="text", text="ok")],
                stop_reason="end_turn", stop_sequence=None,
                usage=dict(input_tokens=1, output_tokens=1)))

        def construct(**kwargs):
            kwargs_seen.append(kwargs.copy())
            return real_client(**kwargs, max_retries=0,
                http_client=httpx.AsyncClient(transport=httpx.MockTransport(transport)))

        with patch.dict(os.environ, environment, clear=True), \
                patch("pathlib.Path.home", return_value=scratch / "home"), \
                patch("anthropic.AsyncAnthropic", side_effect=construct):
            source = AnthropicCompatibleProvider(base_url=endpoint, api_key=key)
            client = source.create_client()
            try:
                reply = await client.messages.create(model="fixture-model", max_tokens=8,
                    messages=[dict(role="user", content="hello")])
                text = reply.content[0].text
            finally:
                await client.close()
        rows.append(dict(name=name, key=key, environment=environment, endpoint=endpoint,
            constructor=kwargs_seen, description=source.describe(), calls=calls, text=text))
    return rows


def provider_auth_contracts(scratch) -> dict:
    async def capture():
        if anthropic.__version__ != "0.107.1":
            raise RuntimeError("provider auth contracts require anthropic 0.107.1")
        rows = []
        profiles = [("missing", None, False, 8), ("empty", "", False, 8),
                    ("stream", None, True, 8), ("ceiling-first", None, False, 1000000)]
        for name, key, stream, max_tokens in profiles:
            calls = []

            async def transport(request):
                calls.append(request.method)
                return httpx.Response(500, json={"error": {"message": "unexpected transport"}})

            fault, constructed = None, False
            with patch.dict(os.environ, {}, clear=True), \
                    patch("pathlib.Path.home", return_value=scratch / "home"):
                client = anthropic.AsyncAnthropic(api_key=key, base_url="http://fixture.invalid",
                    http_client=httpx.AsyncClient(transport=httpx.MockTransport(transport)))
                constructed = True
                try:
                    await client.messages.create(model="claude-sonnet-4-6", max_tokens=max_tokens,
                                                 messages=[dict(role="user", content="hello")], stream=stream)
                except Exception as exc:
                    fault = dict(type=type(exc).__name__, message=str(exc))
                finally:
                    await client.close()
            rows.append(dict(name=name, key=key, stream=stream, max_tokens=max_tokens,
                             constructed=constructed, calls=calls, fault=fault))
        return dict(sdk_version=anthropic.__version__, cases=rows,
            constructor_sha256=hashlib.sha256(inspect.getsource(
                anthropic.AsyncAnthropic.__init__).encode()).hexdigest(),
            source_cases=await _source_auth_cases(scratch))

    return asyncio.run(capture())
