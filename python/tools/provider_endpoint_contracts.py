"""Actual SDK outbound paths for compatible endpoints, direct and streaming."""
from __future__ import annotations

import asyncio
import json

import anthropic
import httpx

PATHS = (
    '', '/', '//', '/tenant', '/tenant/', '/tenant//', '/tenant///',
    '/a/./tenant', '/a/../tenant', '/a/../../tenant', '/tenant/.', '/tenant/..',
    '/a//../tenant', '/a/..//tenant', '/tenant%2F/', '/tenant/%2e%2E/child',
    '/tenant%2f//', '/tenant path/', '/café/中文', '/v1/',
) + tuple("/ascii-" + chr(code) + "-tail" for code in range(0x20, 0x7f)
          if chr(code) not in "%?#")


def provider_endpoint_contracts() -> dict:
    if anthropic.__version__ != '0.107.1':
        raise RuntimeError('endpoint contracts require anthropic 0.107.1')

    async def capture():
        rows = []
        message = dict(id='fixture', type='message', role='assistant', model='fixture-model',
                       content=[dict(type='text', text='ok')], stop_reason='end_turn',
                       stop_sequence=None, usage=dict(input_tokens=1, output_tokens=1))
        events = (
            ('message_start', dict(type='message_start', message={**message, 'content': [], 'stop_reason': None})),
            ('content_block_start', dict(type='content_block_start', index=0, content_block=message['content'][0])),
            ('content_block_stop', dict(type='content_block_stop', index=0)),
            ('message_delta', dict(type='message_delta', delta=dict(stop_reason='end_turn', stop_sequence=None), usage=dict(output_tokens=1))),
            ('message_stop', dict(type='message_stop')),
        )
        sse = ''.join(f'event: {kind}\ndata: {json.dumps(value)}\n\n' for kind, value in events)
        for path in PATHS:
            for mode in ('direct', 'stream'):
                calls = []

                def transport(request):
                    calls.append(dict(url=str(request.url), raw_path=request.url.raw_path.decode(), method=request.method,
                                      api_key=request.headers['x-api-key']))
                    if mode == 'direct':
                        return httpx.Response(200, json=message)
                    return httpx.Response(200, text=sse, headers={'content-type': 'text/event-stream'})

                async with httpx.AsyncClient(transport=httpx.MockTransport(transport)) as http:
                    async with anthropic.AsyncAnthropic(base_url='https://fixture.invalid' + path,
                            api_key='fixture-key', max_retries=0, http_client=http) as client:
                        request = dict(model='fixture-model', max_tokens=8, messages=[dict(role='user', content='hello')])
                        if mode == 'direct':
                            reply = await client.messages.create(**request)
                        else:
                            async with client.messages.stream(**request) as stream:
                                reply = await stream.get_final_message()
                rows.append(dict(base_url='https://fixture.invalid' + path, mode=mode,
                                 calls=calls, text=reply.content[0].text))
        return dict(sdk_version=anthropic.__version__, httpx_version=httpx.__version__, message=message, sse=sse, cases=rows)

    return asyncio.run(capture())
