"""Actual public Source helpers: explicit arguments, effects and caller seams."""
import asyncio
import copy
import json
import tempfile
from contextlib import ExitStack
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch


def public_helper_contracts() -> dict:
    from mini_loop import UNKNOWN_RESULT, TRAJECTORY_SCHEMA_VERSION
    from mini_loop.agent import Agent
    from mini_loop.caching import NullCachePolicy
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeMessage, FakeUsage, text
    from mini_loop.memory import (MemoryStore, install_memory, select_relevant_memories,
                                  prepare_memory_context, consolidate_memories)
    from mini_loop.registry import ToolRegistry
    from mini_loop.compaction import tool_result_budget
    from mini_loop.secrets import SecretRegistry

    memory_recipes = []
    for limit in [None, 0, 1, 2, 5, -1, -3, -20]:
        for reply in ['[2, true, false, 2, 1.0, -1, 99]', '[]']:
            memory_recipes.append(dict(name=f'select/{limit}/{reply == "[]"}', operation='select',
                                       limit=limit, reply=reply, query='alpha beta gamma'))
    memory_recipes.extend([
        dict(name='select/list-fault', operation='select', reply='[0]', list_fault=True),
        dict(name='select/fallback-fault', operation='select', reply='[]', fallback_fault=True),
        dict(name='consolidate/list-fault', operation='consolidate', reply='[]', list_fault=True),
        dict(name='select/tools-off', operation='select', reply='[2,0]', tools=False),
        dict(name='select/empty', operation='select', reply='[0]', empty=True),
        dict(name='select/fault', operation='select', reply='', fault=True, query='beta'),
        dict(name='select/malformed', operation='select', reply='[2,]', query='gamma'),
        dict(name='select/unicode-tail', operation='select', reply='[1]', query='界'*4001+'tail'),
        dict(name='select/nonfinite', operation='select', reply='[NaN,Infinity,2]'),
        dict(name='prepare/default', operation='prepare', reply='[2,0]'),
        dict(name='prepare/disabled', operation='prepare', reply='[0]', auto=False),
        dict(name='prepare/tools-off', operation='prepare', reply='[0]', tools=False),
        dict(name='prepare/empty', operation='prepare', reply='[0]', empty=True),
        dict(name='prepare/no-hit', operation='prepare', reply='[]', query='unmatched'),
        dict(name='prepare/send-fault', operation='prepare', reply='[0]', send_fault=True),
    ])
    merged = json.dumps([dict(name='alpha', type='project', description='alpha facts', body='alpha body'),
                         dict(name='merged', type='reference', description='merged facts', body='merged body')])
    for threshold in [None, 0, 2, 3, 4, -1]:
        memory_recipes.append(dict(name=f'consolidate/{threshold}', operation='consolidate',
                                  threshold=threshold, reply=merged, tools=False))
    memory_recipes.extend([
        dict(name='consolidate/empty-zero', operation='consolidate', threshold=0, empty=True, reply='[]'),
        dict(name='consolidate/fault', operation='consolidate', threshold=0, reply='', fault=True),
        dict(name='consolidate/unhashable', operation='consolidate', threshold=0,
             reply='[{"name":"first","body":"ok"},{"name":[]}]'),
        dict(name='consolidate/no-names', operation='consolidate', threshold=0, reply='[null,{},3]'),
    ])

    async def capture_memory(row, root):
        root.mkdir(parents=True)
        store = MemoryStore(root/'memory')
        store.write('foreign', 'project', 'private facts', 'private body', owner='bob')
        if not row.get('empty', False):
            for name, kind, origin in [('alpha','project','explicit'), ('beta','feedback','imported'), ('gamma','reference','auto_extracted')]:
                store.write(name, kind, name+' facts', name+' body', owner='alice', origin=origin)
        calls, events = [], []
        async def create(**kwargs):
            calls.append({k:copy.deepcopy(kwargs[k]) for k in ('model','max_tokens','messages')})
            if row.get('fault', False):
                raise RuntimeError('model fault')
            return FakeMessage([text(row['reply'])], 'end_turn', FakeUsage(7,3), model='served-memory')
        async def emit(event):
            events.append(copy.deepcopy(event))
            if row.get('send_fault', False) and event['type'] == 'memory':
                raise RuntimeError('load sender fault')
        tools = install_memory(ToolRegistry()) if row.get('tools', True) else ToolRegistry()
        agent = Agent(client=SimpleNamespace(messages=SimpleNamespace(create=create)), workspace=root,
                      tools=tools, emit=emit, cache_policy=NullCachePolicy(),
                      state=dict(memory=store,resource_owner='alice',memory_auto=row.get('auto',True)),
                      settings=Settings(fake_llm=True,spill_dir=None,skills_dir=root/'skills'))
        before = store.list(owner='alice')
        meter = agent.token_meter.snapshot()
        error, selected, prepared, count = None, [], '', 0
        try:
            with ExitStack() as stack:
                if row.get('list_fault'):
                    stack.enter_context(patch.object(store,'list',side_effect=OSError('initial list fault')))
                if row.get('fallback_fault'):
                    stack.enter_context(patch.object(store,'search',side_effect=OSError('fallback fault')))
                if row['operation'] == 'select':
                    kwargs = {} if row.get('limit') is None else dict(max_items=row['limit'])
                    selected = await select_relevant_memories(agent, row.get('query','unrelated'), **kwargs)
                elif row['operation'] == 'prepare':
                    prepared = await prepare_memory_context(agent, row.get('query','unrelated'))
                else:
                    kwargs = {} if row.get('threshold') is None else dict(threshold=row['threshold'])
                    # Explicit caller store is independent of the agent's automatic-memory tools.
                    from mini_loop.memory import ScopedMemory
                    count = await consolidate_memories(ScopedMemory(store,'alice'), agent, **kwargs)
        except Exception as exc:
            error = str(exc)
        assert agent.token_meter.snapshot() == meter
        return {**row, 'query':row.get('query','unrelated'), 'limit':row.get('limit',None),
                'threshold':row.get('threshold',None), 'tools':row.get('tools',True),
                'auto':row.get('auto',True), 'empty':row.get('empty',False),
                'fault':row.get('fault',False), 'send_fault':row.get('send_fault',False),
                'before':before, 'selected':selected, 'prepared':prepared, 'count':count, 'error':error,
                'after':store.list(owner='alice'), 'foreign':store.list(owner='bob'), 'calls':calls,
                'purposes':[e['purpose'] for e in events if e['type']=='model_start'],
                'loads':[{k:e[k] for k in ('action','count')} for e in events if e['type']=='memory'],
                'messages_unchanged':agent.messages == []}

    def capture_budget(row, root):
        root.mkdir(parents=True)
        contents = row.get('contents', ['secret-value-'+('界'*400), 'b'*600])
        messages = [] if row.get('empty') else [dict(role='user',content=[dict(type='tool_result',tool_use_id=f'u/{i}',content=body,is_error=i==1) for i,body in enumerate(contents)])]
        if row.get('later'):
            messages += [dict(role='assistant',content='later'),dict(role='user',content=[dict(type='tool_result',tool_use_id='latest',content='tiny')])]
        original = copy.deepcopy(messages)
        secrets = SecretRegistry()
        secrets.register('test-secret','secret-value')
        kwargs = dict(secrets=secrets)
        for key in ('max_bytes','preview_chars'):
            if row.get(key) is not None:
                kwargs[key] = row[key]
        import mini_loop.compaction as module
        real_write = module.atomic_write_text
        writes = 0
        def write(path, body):
            nonlocal writes
            writes += 1
            if row.get('write_fault') and writes == 2:
                raise OSError('second write fault')
            return real_write(path, body)
        count, error = 0, None
        with patch.object(module.time,'time',return_value=123.456), patch.object(module,'atomic_write_text',write):
            try:
                count = tool_result_budget(messages,root,**kwargs)
            except OSError as exc:
                error = str(exc)
        normalize = lambda value: json.loads(json.dumps(value,ensure_ascii=False).replace(str(root),'<workspace>'))
        files = {p.name:p.read_text() for p in sorted((root/'.task_outputs/tool-results').glob('*.txt'))}
        return {**row, 'max_bytes':row.get('max_bytes',None), 'preview_chars':row.get('preview_chars',None),
                'before':original, 'after':normalize(messages), 'count':count, 'error':error, 'files':files}

    budget_recipes = [dict(name='budget/default'),dict(name='budget/empty',empty=True),
                      dict(name='budget/newest-only',later=True,max_bytes=20,preview_chars=2),
                      dict(name='budget/write-fault',max_bytes=0,preview_chars=1,write_fault=True)]
    for maximum in [0, 300, -1]:
        for preview in [0, 2, -1, -1000, 5000]:
            budget_recipes.append(dict(name=f'budget/{maximum}/{preview}',max_bytes=maximum,preview_chars=preview))
    budget_recipes.append(dict(name='budget/stable-character-sort',max_bytes=700,preview_chars=1,
                               contents=['界'*300,'a'*300]))
    with tempfile.TemporaryDirectory(prefix='mini-loop-public-helpers-') as scratch:
        root = Path(scratch)
        async def run():
            return [await capture_memory(row,root/str(i)) for i,row in enumerate(memory_recipes)]
        return dict(memory=asyncio.run(run()), budget=[capture_budget(row,root/f'b{i}') for i,row in enumerate(budget_recipes)],
                    constants=dict(unknown_result=UNKNOWN_RESULT,trajectory_schema=TRAJECTORY_SCHEMA_VERSION),
                    limits=['Actual public Source helpers and Agent side-request/event path.',
                            'Native contexts require an explicit owner-bound store/workspace and typed callbacks.',
                            'Callbacks are caller-owned; native cancellation/persistence authority remains an error.',
                            'No default backend/dependency change; broader public export semantics remain open.'])
