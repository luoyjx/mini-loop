"""Actual SDK HTTP bytes with only the TCP destination redirected to loopback."""
from __future__ import annotations

import asyncio
import json
import os
import unicodedata
from unittest.mock import patch

import anthropic
import httpx
import httpcore
import idna
from httpcore._backends.anyio import AnyIOBackend
from mini_loop.providers import AnthropicCompatibleProvider
from tools.provider_endpoint_contracts import provider_endpoint_contracts

WIRE_URLS = (
    ('uppercase-host', 'http://FIXTURE.INVALID/tenant'),
    ('default-port', 'http://fixture.invalid:00080/tenant'),
    ('other-port', 'http://fixture.invalid:8080/tenant'),
    ('unicode-host', 'http://bücher.invalid/tenant'),
    ('sharp-s-host', 'http://faß.invalid/tenant'),
    ('dotted-i-host', 'http://İ.invalid/tenant'),
    ('double-dotted-i-host', 'http://İİ.invalid/tenant'),
    ('mixed-dotted-i-host', 'http://FİXTURE.INVALID/tenant'),
    ('uppercase-unicode-host', 'http://BÜCHER.INVALID/tenant'),
    ('uppercase-sharp-s-host', 'http://FAẞ.INVALID/tenant'),
    ('greek-final-sigma-host', 'http://fixture.ΟΣ/tenant'),
    ('greek-medial-sigma-host', 'http://fixture.ΟΣΑ/tenant'),
    ('greek-across-dot-host', 'http://ΟΣ.invalid/tenant'),
    ('greek-combining-sigma-host', 'http://fixture.AΣ́/tenant'),
    ('uppercase-armenian-host', 'http://fixture.ԱԲ/tenant'),
    ('uppercase-cyrillic-host', 'http://fixture.Я/tenant'),
    ('empty-fragment', 'http://fixture.invalid/tenant#'),
    ('literal-backslash-path', 'http://fixture.invalid/ascii-\\-tail'),
    ('literal-caret-path', 'http://fixture.invalid/ascii-^-tail'),
    ('literal-pipe-path', 'http://fixture.invalid/ascii-|-tail'),
)


def provider_authority_wire_contracts() -> dict:
    if httpcore.__version__ != "1.0.9":
        raise RuntimeError("wire TCP seam requires httpcore 1.0.9")
    payload = provider_endpoint_contracts()
    original = anthropic.AsyncAnthropic

    async def capture():
        rows = []
        for name, base_url in WIRE_URLS:
            for mode in ('direct', 'stream'):
                requests = []
                dials = []
                handlers = set()

                async def handle(reader, writer):
                    task = asyncio.current_task()
                    handlers.add(task)
                    try:
                        head = (await asyncio.wait_for(reader.readuntil(b'\r\n\r\n'), 10)).decode('latin1')
                        lines = head.split('\r\n')
                        method, target, version = lines[0].split(' ')
                        headers = dict(line.split(': ', 1) for line in lines[1:] if line)
                        normalized = {key.lower(): value for key, value in headers.items()}
                        await asyncio.wait_for(reader.readexactly(int(normalized.get('content-length', 0))), 10)
                        requests.append(dict(method=method, target=target, version=version,
                                             host=normalized['host'], api_key=normalized['x-api-key']))
                        body = (payload['sse'] if mode == 'stream' else json.dumps(payload['message'])).encode()
                        content_type = 'text/event-stream' if mode == 'stream' else 'application/json'
                        writer.write(f'HTTP/1.1 200 OK\r\nContent-Type: {content_type}\r\nContent-Length: {len(body)}\r\nConnection: close\r\n\r\n'.encode() + body)
                        await writer.drain()
                    finally:
                        writer.close()
                        await writer.wait_closed()
                        handlers.discard(task)

                server = await asyncio.start_server(handle, '127.0.0.1', 0)
                destination = server.sockets[0].getsockname()[1]

                class RedirectedBackend(AnyIOBackend):
                    async def connect_tcp(self, host, port, timeout=None, local_address=None, socket_options=None):
                        dials.append(dict(host=host, port=port))
                        return await super().connect_tcp('127.0.0.1', destination, timeout, local_address, socket_options)

                async with server:
                    transport = httpx.AsyncHTTPTransport(trust_env=False)
                    transport._pool._network_backend = RedirectedBackend()
                    async with httpx.AsyncClient(transport=transport, trust_env=False, timeout=10) as http:
                        def factory(**kwargs):
                            return original(**kwargs, http_client=http, max_retries=0)
                        with patch.dict(os.environ, {}, clear=True), patch.object(anthropic, 'AsyncAnthropic', factory):
                            provider = AnthropicCompatibleProvider(base_url=base_url, api_key='fixture-key')
                            async with provider.create_client() as client:
                                request = dict(model='fixture-model', max_tokens=8,
                                               messages=[dict(role='user', content='hello')])
                                if mode == 'direct':
                                    reply = await client.messages.create(**request)
                                else:
                                    async with client.messages.stream(**request) as stream:
                                        reply = await stream.get_final_message()
                    if handlers:
                        await asyncio.gather(*tuple(handlers))
                rows.append(dict(name=name, base_url=base_url, mode=mode, identity=provider.describe(),
                                 dials=dials, requests=requests, text=reply.content[0].text))
        return dict(sdk_version=payload['sdk_version'], httpx_version=payload['httpx_version'],
                    idna_version=idna.__version__, unicode_version=unicodedata.unidata_version,
                    httpcore_version=httpcore.__version__, message=payload['message'], sse=payload['sse'], cases=rows)

    return asyncio.run(capture())
