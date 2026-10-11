"""Actual provider construction and SDK request observations for URL scope audit."""
from __future__ import annotations

import asyncio
import os
from unittest.mock import patch

import anthropic
import httpx

from mini_loop.providers import AnthropicCompatibleProvider
from tools.provider_endpoint_contracts import provider_endpoint_contracts

URLS = (
    ('default', None), ('empty', ''), ('uppercase-host', 'https://FIXTURE.INVALID/tenant'),
    ('uppercase-scheme', 'HTTPS://fixture.invalid/tenant'),
    ('https-default-port', 'https://fixture.invalid:443/tenant'),
    ('http-default-port', 'http://fixture.invalid:80/tenant'),
    ('https-other-port', 'https://fixture.invalid:8443/tenant'),
    ('zero-padded-port', 'https://fixture.invalid:00443/tenant'),
    ('empty-port', 'https://fixture.invalid:/tenant'),
    ('ipv6', 'http://[::1]:8080/tenant'),
    ('ipv6-expanded', 'http://[0:0:0:0:0:0:0:1]/tenant'),
    ('ipv6-default-port', 'http://[::1]:80/tenant'),
    ('unicode-host', 'https://bücher.invalid/tenant'),
    ('punycode-host', 'https://xn--bcher-kva.invalid/tenant'),
    ('trailing-host-dot', 'https://fixture.invalid./tenant'),
    ('query', 'https://fixture.invalid/tenant?q=fixture'),
    ('empty-query', 'https://fixture.invalid/tenant?'),
    ('fragment', 'https://fixture.invalid/tenant#fixture'),
    ('empty-fragment', 'https://fixture.invalid/tenant#'),
    ('userinfo', 'https://fixture-user:fixture-pass@fixture.invalid/tenant'),
    ('username', 'https://fixture-user@fixture.invalid/tenant'),
    ('relative-path', '/tenant'), ('bare-host', 'fixture.invalid/tenant'),
    ('scheme-relative', '//fixture.invalid/tenant'),
    ('ftp', 'ftp://fixture.invalid/tenant'),
    ('missing-host', 'https:///tenant'),
    ('invalid-port', 'https://fixture.invalid:wrong/tenant'),
    ('oversized-port', 'https://fixture.invalid:99999/tenant'),
    ('invalid-escape', 'https://fixture.invalid/%zz'),
    ('unmatched-ipv6', 'https://[::1/tenant'),
    ('host-space', 'https://fixture host.invalid/tenant'),
    ('leading-space', ' https://fixture.invalid/tenant'),
    ('trailing-space', 'https://fixture.invalid/tenant '),
    ('newline', 'https://fixture.invalid/tenant\n'),
)


def provider_url_contracts() -> dict:
    payload = provider_endpoint_contracts()
    original = anthropic.AsyncAnthropic

    async def capture():
        rows = []
        for name, base_url in URLS:
            for mode in ('direct', 'stream'):
                calls = []
                provider = AnthropicCompatibleProvider(base_url=base_url, api_key='fixture-key')
                observation = dict(name=name, base_url=base_url, mode=mode,
                                   identity=provider.describe(), stage='constructor', error=None, calls=calls)

                def transport(request):
                    calls.append(dict(url=str(request.url), raw_path=request.url.raw_path.decode(),
                                      method=request.method, api_key=request.headers.get('x-api-key'),
                                      authorization=request.headers.get('authorization')))
                    if mode == 'direct':
                        return httpx.Response(200, json=payload['message'])
                    return httpx.Response(200, text=payload['sse'], headers={'content-type': 'text/event-stream'})

                async with httpx.AsyncClient(transport=httpx.MockTransport(transport), trust_env=False) as http:
                    def factory(**kwargs):
                        return original(**kwargs, http_client=http, max_retries=0)
                    client = None
                    try:
                        # The producer and SDK are real. Only inject the HTTP boundary;
                        # ambient endpoint/auth environment is cleared for reproducibility.
                        with patch.dict(os.environ, {}, clear=True), patch.object(anthropic, 'AsyncAnthropic', factory):
                            client = provider.create_client()
                            observation['stage'] = 'request'
                            request = dict(model='fixture-model', max_tokens=8,
                                           messages=[dict(role='user', content='hello')])
                            if mode == 'direct':
                                reply = await client.messages.create(**request)
                            else:
                                async with client.messages.stream(**request) as stream:
                                    reply = await stream.get_final_message()
                            observation.update(stage='complete', text=reply.content[0].text)
                    except Exception as exc:
                        observation['error'] = dict(type=type(exc).__name__, text=str(exc))
                    finally:
                        if client is not None:
                            await client.close()
                rows.append(observation)
        return dict(sdk_version=payload['sdk_version'], httpx_version=payload['httpx_version'],
                    message=payload['message'], sse=payload['sse'], cases=rows)

    return asyncio.run(capture())
