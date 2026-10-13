"""Actual Agent mixed-batch effects and batch-local step ledger admission."""
import asyncio
import json
import tempfile
from pathlib import Path


def batch_step_boundary_contracts() -> dict:
    from mini_loop.agent import Agent
    from mini_loop.caching import NullCachePolicy
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.registry import Tool, ToolRegistry

    patterns = [('exclusive',['exclusive']*3),('parallel',['parallel']*3),
                ('parallel-barrier',['parallel','exclusive','parallel']),
                ('barrier-parallel',['exclusive','parallel','parallel'])]
    async def capture(name, modes, bad, capacity, root):
        root.mkdir(parents=True)
        effects, events, requests = [], [], []
        async def handler(ctx, slot):
            effects.append(slot)
            return '\ud800' if slot == bad else f'output-{slot}'
        registry = ToolRegistry([Tool(mode,'Batch boundary',{'type':'object','properties':{'slot':{'type':'integer'}}},
                                      handler,risk='read',parallel_safe=mode=='parallel') for mode in ['exclusive','parallel']])
        def respond(kwargs):
            requests.append(kwargs)
            if len(requests)==1:
                return [tool(mode,_id=f'id-{i}',slot=i) for i,mode in enumerate(modes)],'tool_use'
            return [text('done')],'end_turn'
        async def emit(event):events.append(event)
        agent=Agent(client=FakeAsyncAnthropic(responder=respond,thinking=False),workspace=root,tools=registry,
                    emit=emit,tool_semaphore=asyncio.Semaphore(capacity),cache_policy=NullCachePolicy(),
                    settings=Settings(fake_llm=True,spill_dir=None,skills_dir=root/'skills'))
        error,output=None,''
        try:output=await agent.run('batch')
        except Exception as exc:error=type(exc).__name__
        # Every handler is immediate. Let already scheduled gather callbacks
        # finish before inspecting effects; this introduces no timeout verdict.
        await asyncio.sleep(0)
        results=[part for message in agent.messages if isinstance(message.get('content'),list)
                 for part in message['content'] if part.get('type')=='tool_result']
        return dict(name=f'{name}/bad-{bad}/capacity-{capacity}',modes=modes,bad=bad,capacity=capacity,
                    error=error,output=output,effects=effects,result_event_ids=[e['id'] for e in events if e['type']=='tool_result'],
                    tool_event_ids=[e['id'] for e in events if e['type']=='tool_use'],model_starts=sum(e['type']=='model_start' for e in events),
                    requests=len(requests),messages=len(agent.messages),result_ids=[r['tool_use_id'] for r in results],
                    result_text_json=[json.dumps(r['content'],ensure_ascii=True) for r in results],
                    steps=[dict(name=s.name,input_hash=s.input_hash,output_hash=s.output_hash,failed=s.failed,denied=s.denied) for s in agent.recent_steps])
    with tempfile.TemporaryDirectory(prefix='mini-loop-batch-step-') as scratch:
        async def run():
            return [await capture(name,modes,bad,capacity,Path(scratch)/f'{name}-{bad}-{capacity}')
                    for name,modes in patterns for bad in [-1,0,1,2] for capacity in [1,2]]
        return dict(cases=asyncio.run(run()),limits=['Actual Source Agent.run with registered read tools, exclusive/parallel barriers and capacities1/2.',
                                                   'Callbacks are immediate observations; delayed/canceled/resource-exhausted/wider host/platform cases remain separate.',
                                                   'Successful batches drain steps in model order. Ordinary UTF-8 hash failure preserves events/effects but admits no batch-local step/history.'])
