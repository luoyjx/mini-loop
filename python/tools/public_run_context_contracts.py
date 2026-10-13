"""Actual public RunContext constructors, derivations and tool-call consumers."""
from __future__ import annotations

import asyncio
from pathlib import Path
import tempfile


def public_run_context_contracts() -> dict:
    from mini_loop import RunContext
    from mini_loop.agent import Agent
    from mini_loop.caching import NullCachePolicy
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.registry import Tool, ToolRegistry

    bases = [
        ('default', 'default', {}),
        ('human-default', 'human', {}),
        ('human-empty-channel', 'human', {'channel': ''}),
        ('human-empty-stamp', 'human', {'stamped_by': ''}),
        ('human-empty-provenance', 'human', {'channel': '', 'stamped_by': ''}),
        ('human-empty-actor', 'human', {'actor_id': ''}),
        ('human-unicode-grants', 'human', {'actor_id': '用户', 'channel': ' CLI ', 'stamped_by': '控制器',
                                         'approved_capabilities': ['workflow.launch', '', 'a', 'a', '权限']}),
        ('peer-default', 'peer', {'delegated_by': 'lead'}),
        ('peer-empty-delegate', 'peer', {'delegated_by': ''}),
        ('peer-empty-stamp', 'peer', {'delegated_by': 'lead', 'stamped_by': ''}),
        ('peer-empty-actor', 'peer', {'delegated_by': 'lead', 'actor_id': ''}),
        ('peer-empty-parent', 'peer', {'delegated_by': 'lead', 'parent_message_id': ''}),
        ('peer-parent', 'peer', {'delegated_by': 'lead', 'parent_message_id': 'prior-message'}),
        ('peer-unicode', 'peer', {'delegated_by': '父代理', 'actor_id': '子代理',
                                'stamped_by': '控制器', 'parent_message_id': '先前消息'}),
    ]
    modes = ['identity', 'new-clear', 'new-grants', 'derive-peer', 'derive-empty-actor']

    async def capture(name, kind, options, mode, root):
        root.mkdir(parents=True)
        if kind == 'human':
            base = RunContext.explicit_human(**options)
        elif kind == 'peer':
            base = RunContext.peer_agent(**options)
        else:
            base = RunContext.default()
        if mode == 'new-clear':
            run = base.with_new_message()
        elif mode == 'new-grants':
            run = base.with_new_message(approved_capabilities=('workflow.launch', '', 'a', 'a'))
        elif mode == 'derive-peer':
            run = base.derive_peer_agent(delegated_by='controller')
        elif mode == 'derive-empty-actor':
            run = base.derive_peer_agent(delegated_by='', actor_id='')
        else:
            run = base

        def identity(value):
            if value == run.message_id:
                return 'self'
            if value == base.message_id:
                return 'base'
            return value

        def snapshot(value):
            result = value.as_dict()
            result['message_id'] = identity(result['message_id'])
            result['parent_message_id'] = identity(result['parent_message_id'])
            return result

        captured, events, requests = [], [], []

        async def probe(ctx):
            captured.append(snapshot(ctx.run_context))
            return ctx.run_context.authority

        def respond(kwargs):
            requests.append(kwargs)
            if len(requests) == 1:
                return [tool('context_probe', _id='probe')], 'tool_use'
            return [text('done')], 'end_turn'

        async def emit(event):
            if event['type'] in ('tool_use', 'tool_result'):
                events.append({'type': event['type'], 'message_id': identity(event.get('message_id')),
                               'parent_present': 'parent_message_id' in event,
                               'parent_message_id': identity(event.get('parent_message_id'))})

        registry = ToolRegistry([Tool('context_probe', 'Read caller context', {'type': 'object', 'properties': {}},
                                      probe, risk='read', readonly=True)])
        agent = Agent(client=FakeAsyncAnthropic(responder=respond, thinking=False), workspace=root,
                      tools=registry, emit=emit, cache_policy=NullCachePolicy(),
                      settings=Settings(fake_llm=True, spill_dir=None, skills_dir=root/'skills'))
        output = await agent.run('context', run_context=run)
        result = next(part['content'] for message in agent.messages if isinstance(message.get('content'), list)
                      for part in message['content'] if part.get('type') == 'tool_result')
        return {'name': name+'/'+mode, 'kind': kind, 'options': options, 'mode': mode,
                'snapshot': snapshot(run), 'tool_contexts': captured, 'events': events,
                'allows': {cap: run.allows(cap) for cap in ['', 'a', 'workflow.launch', 'workflow.manage']},
                'fresh_message': run.message_id != base.message_id, 'output': output,
                'result': result, 'requests': len(requests), 'messages': len(agent.messages)}

    with tempfile.TemporaryDirectory(prefix='mini-loop-public-context-') as scratch:
        async def collect():
            return [await capture(name, kind, options, mode, Path(scratch)/f'{name}-{mode}')
                    for name, kind, options in bases for mode in modes]
        return {'cases': asyncio.run(collect()), 'scope': [
            'Actual public default/explicit_human/peer_agent constructors and both derivation methods with declared text/capability inputs.',
            'Real Agent.run, registered readonly tool context, result and emitted correlation identity; generated UUIDs are normalized only.',
            'Arbitrary direct dataclass overrides, non-text Python objects and all host/platform/exhaustion profiles remain separate.',
            'Capability membership is not human authority; peer derivation drops all inherited approvals.',
        ]}
