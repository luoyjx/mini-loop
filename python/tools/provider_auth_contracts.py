"""Pinned SDK missing-authentication validation phases, without external IO."""

import asyncio
import os
from unittest.mock import patch

import anthropic
import httpx


def provider_auth_contracts(scratch) -> dict:
    async def capture():
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
        return dict(sdk_version=anthropic.__version__, cases=rows)

    return asyncio.run(capture())
