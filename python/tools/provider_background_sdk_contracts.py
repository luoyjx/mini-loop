"""Pinned SDK envelopes and actual managed background optional-field stages."""
from __future__ import annotations

import asyncio
import copy
import hashlib
import inspect
import json
import warnings
from pathlib import Path
from unittest.mock import patch

import anthropic
import httpx

from mini_loop.agent import Agent, _content_payload
from mini_loop.background import BackgroundManager, install_background
from mini_loop.builtins import default_registry
from mini_loop.config import Settings
from mini_loop.manager import SessionManager
from mini_loop.registry import Hook, Hooks
from tools.provider_background_options_contracts import background_option_inputs


def _envelope(arguments):
    body = {'id': 'message', 'type': 'message', 'role': 'assistant', 'model': 'model',
        'content': [{'type': 'tool_use', 'id': 'call', 'name': 'background_run', 'input': arguments}],
        'stop_reason': 'tool_use', 'usage': {'input_tokens': 1, 'output_tokens': 0}}
    events = [('message_start', {'type': 'message_start', 'message': {**body, 'content': [], 'stop_reason': None}}),
        ('content_block_start', {'type': 'content_block_start', 'index': 0,
            'content_block': {**body['content'][0], 'input': {}}}),
        ('content_block_delta', {'type': 'content_block_delta', 'index': 0,
            'delta': {'type': 'input_json_delta', 'partial_json': json.dumps(arguments)}}),
        ('content_block_stop', {'type': 'content_block_stop', 'index': 0}),
        ('message_delta', {'type': 'message_delta', 'delta': {'stop_reason': 'tool_use', 'stop_sequence': None},
            'usage': {'output_tokens': 0}}), ('message_stop', {'type': 'message_stop'})]
    return body, ''.join(f'event: {event}\ndata: {json.dumps(value)}\n\n' for event, value in events)


def provider_background_sdk_contracts(scratch: Path | str) -> dict:
    if anthropic.__version__ != '0.107.1':
        raise RuntimeError('Background SDK contracts require anthropic 0.107.1')
    final_body = {'id': 'final', 'type': 'message', 'role': 'assistant', 'model': 'model',
        'content': [{'type': 'text', 'text': 'finished'}], 'stop_reason': 'end_turn',
        'usage': {'input_tokens': 1, 'output_tokens': 0}}

    async def capture():
        sdk_rows, managed_rows = [], []
        for name, arguments in background_option_inputs():
            body, sse = _envelope(arguments)
            requests = []
            async def transport(request):
                payload = json.loads(request.content)
                requests.append(payload)
                if payload.get('stream'):
                    return httpx.Response(200, content=sse.encode(), headers={'content-type': 'text/event-stream'})
                reply = final_body if len(payload['messages']) > 1 else body
                return httpx.Response(200, content=json.dumps(reply).encode(), headers={'content-type': 'application/json'})
            sdk = anthropic.AsyncAnthropic(api_key='fixture-key', base_url='https://fixture.invalid', max_retries=0,
                http_client=httpx.AsyncClient(transport=httpx.MockTransport(transport)))
            try:
                direct = await sdk.messages.create(model='model', max_tokens=8, messages=[{'role': 'user', 'content': 'hello'}])
                async with sdk.messages.stream(model='model', max_tokens=8, messages=[{'role': 'user', 'content': 'hello'}]) as stream:
                    streamed = await stream.get_final_message()
                before = len(requests)
                try:
                    await sdk.messages.create(model='model', max_tokens=8, messages=[{'role': 'user', 'content': 'hello'},
                        {'role': 'assistant', 'content': _content_payload(direct.content)},
                        {'role': 'user', 'content': [{'type': 'tool_result', 'tool_use_id': 'call', 'content': 'accepted'}]}])
                    forward_error = None
                except Exception as error:
                    forward_error = {'class': type(error).__name__, 'message': str(error)}
                sdk_rows.append({'name': name, 'kind': 'tool_use', 'input_json': json.dumps(arguments),
                    'response_json': json.dumps(body), 'content_json': json.dumps(_content_payload(direct.content), sort_keys=True),
                    'sse': sse, 'stream_content_json': json.dumps(_content_payload(streamed.content), sort_keys=True),
                    'forward_requests': len(requests)-before, 'forward_error': forward_error})
            finally:
                await sdk.close()

            requests.clear()
            trace, events, processes = [], [], []
            class Probe(Hook):
                async def before_tool(self, ctx, call): trace.append('before')
                async def guard_tool(self, ctx, call): trace.append('guard')
                async def after_tool(self, ctx, call, output): trace.append('after')
                async def on_result(self, ctx, call, output, *, denied=False, failed=False): trace.append('result')
            async def emit(event):
                if event['type'] in {'tool_use', 'tool_result'}:
                    events.append(copy.deepcopy(event))
            original_spawn = asyncio.create_subprocess_exec
            async def spawn(*args, **kwargs):
                process = await original_spawn(*args, **kwargs)
                processes.append(process)
                return process
            sdk = anthropic.AsyncAnthropic(api_key='fixture-key', base_url='https://fixture.invalid', max_retries=0,
                http_client=httpx.AsyncClient(transport=httpx.MockTransport(transport)))
            manager = SessionManager(Settings(fake_llm=True, workspace_root=Path(scratch)/('managed-'+name),
                skills_dir=Path(scratch)/'skills'), sdk, tool_registry=install_background(default_registry()),
                hooks=Hooks([Probe()]), event_sink=emit)
            with patch.object(asyncio, 'create_subprocess_exec', spawn), warnings.catch_warnings(record=True) as recorded:
                warnings.simplefilter('always', RuntimeWarning)
                try:
                    session = manager.create(owner='alice', permission_mode='auto')
                    try:
                        final = await session.run('hello')
                        error = None
                    except Exception as failure:
                        final = None
                        error = {'class': type(failure).__name__, 'message': str(failure)}
                    owned = session.agent.state.get('background')
                    if owned:
                        task = owned._tasks['bg_0001']
                        await task['handle']
                        task_result = {'status': task['status'], 'result': task['result']}
                    else:
                        task_result = None
                    uses = [event for event in events if event['type']=='tool_use']
                    results = [event for event in events if event['type']=='tool_result']
                    managed_rows.append({'name': name, 'input_json': json.dumps(arguments), 'response_json': json.dumps(body),
                        'final_response_json': json.dumps(final_body), 'trace': trace, 'transport_requests': len(requests),
                        'tool_output': results[0]['output'] if results else None,
                        'action_recorded': manager.actions.get(uses[0]['action_id']) is not None if uses else False,
                        'tool_uses': len(uses), 'messages': len(session.agent.messages), 'final': final, 'error': error,
                        'background_initialized': owned is not None, 'background_counter': owned._counter if owned else 0,
                        'starts_at_settlement': len(processes), 'task': task_result,
                        'warnings': [{'class': w.category.__name__, 'message': str(w.message)} for w in recorded]})
                finally:
                    for process in processes:
                        await process.communicate()
                    await manager.stop()
                    await sdk.close()
        return sdk_rows, managed_rows
    rows, managed = asyncio.run(capture())
    sources = {'capture.inputs': background_option_inputs, 'Agent._exec_tool': Agent._exec_tool,
        'Agent._exec_tool_batch': Agent._exec_tool_batch, 'BackgroundManager.run': BackgroundManager.run,
        'BackgroundManager._exec': BackgroundManager._exec}
    return {'sdk_version': anthropic.__version__, 'cases': rows, 'managed': managed,
        'source_sha256': {name: hashlib.sha256(inspect.getsource(fn).encode()).hexdigest() for name, fn in sources.items()},
        'limits': ['Task waiting uses direct await; no observer watchdog', 'Managed transport returns one tool reply then a final reply',
            'No physical startup timing, PID, lease timing or full-state recovery claim']}
