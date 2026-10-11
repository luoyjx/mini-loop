"""Actual HTTP proxy, TLS and CONNECT bytes on owned loopback endpoints.

TLS verifies the pinned test certificate and hostname. CONNECT relays opaque TCP
bytes to the owned TLS origin; HTTP parsing/encoding and SDK behavior are real.
"""
from __future__ import annotations

import asyncio
import json
import os
import ssl
from pathlib import Path
from unittest.mock import patch

import anthropic
import httpx
import httpcore
from httpcore._backends.anyio import AnyIOBackend
from mini_loop.providers import AnthropicCompatibleProvider
from tools.provider_endpoint_contracts import provider_endpoint_contracts

PATHS = (('ordinary', '/tenant'), ('backslash', '/ascii-\\-tail'),
         ('caret', '/ascii-^-tail'), ('pipe', '/ascii-|-tail'))
ROUTES = ('http-proxy', 'https-direct', 'https-proxy')


def provider_transport_wire_contracts() -> dict:
    if httpcore.__version__ != '1.0.9':
        raise RuntimeError('TCP seam requires httpcore 1.0.9')
    root = Path(__file__).resolve().parents[2]
    cert = root / 'go/testdata/provider-wire-test-cert.pem'
    key = root / 'go/testdata/provider-wire-test-key.pem'
    payload = provider_endpoint_contracts()
    original = anthropic.AsyncAnthropic

    async def capture():
        rows = []
        for route in ROUTES:
            for name, path in PATHS:
                for mode in ('direct', 'stream'):
                    requests, connects, snis, dials = [], [], [], []
                    handlers = set()
                    writers = set()
                    response_sent = asyncio.Event()

                    async def close(writer):
                        writer.close()
                        try:
                            await writer.wait_closed()
                        except (ConnectionResetError, BrokenPipeError):
                            # A completed HTTP response can be followed by a peer
                            # TCP reset during TLS shutdown. Only close errors are
                            # tolerated; handshake/read/write failures still fail.
                            pass
                        finally:
                            writers.discard(writer)

                    async def head(reader):
                        lines = (await asyncio.wait_for(reader.readuntil(b'\r\n\r\n'), 10)).decode('latin1').split('\r\n')
                        method, target, version = lines[0].split(' ')
                        headers = {k.lower(): v for k, v in (line.split(': ', 1) for line in lines[1:] if line)}
                        return dict(method=method, target=target, version=version,
                                    host=headers['host'], api_key=headers.get('x-api-key')), headers

                    async def reply(reader, writer, row, headers):
                        await asyncio.wait_for(reader.readexactly(int(headers.get('content-length', 0))), 10)
                        requests.append(row)
                        body = (payload['sse'] if mode == 'stream' else json.dumps(payload['message'])).encode()
                        kind = 'text/event-stream' if mode == 'stream' else 'application/json'
                        writer.write(f'HTTP/1.1 200 OK\r\nContent-Type: {kind}\r\nContent-Length: {len(body)}\r\nConnection: close\r\n\r\n'.encode()+body)
                        await writer.drain()
                        response_sent.set()

                    async def origin(reader, writer):
                        task = asyncio.current_task(); handlers.add(task); writers.add(writer)
                        try:
                            row, headers = await head(reader)
                            await reply(reader, writer, row, headers)
                        finally:
                            try:
                                await close(writer)
                            finally:
                                handlers.discard(task)

                    tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
                    tls.load_cert_chain(cert, key)
                    tls.set_alpn_protocols(['http/1.1'])
                    tls.set_servername_callback(lambda socket, name, context: snis.append(name))
                    server = await asyncio.start_server(origin, '127.0.0.1', 0, ssl=tls)
                    origin_port = server.sockets[0].getsockname()[1]

                    async def proxy(reader, writer):
                        task = asyncio.current_task(); handlers.add(task); writers.add(writer)
                        upstream = None
                        try:
                            row, headers = await head(reader)
                            if row['method'] != 'CONNECT':
                                await reply(reader, writer, row, headers)
                                return
                            connects.append(row)
                            peer, upstream = await asyncio.open_connection('127.0.0.1', origin_port)
                            writers.add(upstream)
                            writer.write(b'HTTP/1.1 200 Connection established\r\n\r\n'); await writer.drain()

                            async def relay(source, destination):
                                try:
                                    while chunk := await source.read(65536):
                                        destination.write(chunk); await destination.drain()
                                    if destination.can_write_eof():
                                        destination.write_eof()
                                except (ConnectionResetError, BrokenPipeError):
                                    # A peer can reset a completed CONNECT tunnel
                                    # while HTTPX closes it. Never tolerate reset
                                    # before the full controlled response is sent.
                                    if not response_sent.is_set():
                                        raise
                            await asyncio.gather(relay(reader, upstream), relay(peer, writer))
                        finally:
                            for stream in (writer, upstream):
                                if stream is not None:
                                    await close(stream)
                            handlers.discard(task)

                    proxy_server = await asyncio.start_server(proxy, '127.0.0.1', 0)
                    proxy_port = proxy_server.sockets[0].getsockname()[1]
                    base_url = ('http' if route == 'http-proxy' else 'https')+'://fixture.invalid'+path
                    verify = ssl.create_default_context(cafile=str(cert))

                    class Backend(AnyIOBackend):
                        async def connect_tcp(self, host, port, timeout=None, local_address=None, socket_options=None):
                            if route == 'https-direct':
                                assert (host, port) == ('fixture.invalid', 443)
                                dials.append(dict(host=host, port=port))
                                host, port = '127.0.0.1', origin_port
                            else:
                                assert (host, port) == ('127.0.0.1', proxy_port)
                                dials.append(dict(host=host, port='owned-proxy'))
                            return await super().connect_tcp(host, port, timeout, local_address, socket_options)

                    try:
                        async with server, proxy_server:
                            transport = httpx.AsyncHTTPTransport(verify=verify, trust_env=False,
                                proxy=None if route == 'https-direct' else f'http://127.0.0.1:{proxy_port}')
                            transport._pool._network_backend = Backend()
                            async with httpx.AsyncClient(transport=transport, trust_env=False, timeout=10) as http:
                                def factory(**kwargs):
                                    return original(**kwargs, http_client=http, max_retries=0)
                                with patch.dict(os.environ, {}, clear=True), patch.object(anthropic, 'AsyncAnthropic', factory):
                                    provider = AnthropicCompatibleProvider(base_url=base_url, api_key='fixture-key')
                                    async with provider.create_client() as client:
                                        request = dict(model='fixture-model', max_tokens=8,
                                                       messages=[dict(role='user', content='hello')])
                                        if mode == 'direct':
                                            response = await client.messages.create(**request)
                                        else:
                                            async with client.messages.stream(**request) as stream:
                                                response = await stream.get_final_message()
                            # The SDK has consumed/validated the whole response
                            # and closed its client. Abort only owned remaining
                            # sockets to join TLS shutdown through a TCP tunnel.
                            # This does not alter handshake or application bytes.
                            for writer in tuple(writers):
                                writer.transport.abort()
                            if handlers:
                                await asyncio.wait_for(asyncio.gather(*tuple(handlers)), 10)
                    finally:
                        for writer in tuple(writers):
                            writer.close()
                        for task in tuple(handlers):
                            task.cancel()
                        if handlers:
                            await asyncio.gather(*tuple(handlers), return_exceptions=True)
                        server.close(); proxy_server.close()
                        await server.wait_closed(); await proxy_server.wait_closed()
                    rows.append(dict(route=route, name=name, base_url=base_url, mode=mode,
                                     requests=requests, connects=connects, snis=snis, dials=dials,
                                     identity=provider.describe(), text=response.content[0].text,
                                     handlers_joined=not handlers, writers_closed=not writers))
        return dict(sdk_version=payload['sdk_version'], httpx_version=payload['httpx_version'],
                    httpcore_version=httpcore.__version__, message=payload['message'], sse=payload['sse'], cases=rows)
    return asyncio.run(capture())
