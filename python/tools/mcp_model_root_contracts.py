"""Actual Source MCP model inputs through whole-batch preparation and gate."""

import asyncio
import copy
import json
import tempfile
from pathlib import Path


def mcp_model_root_contracts() -> dict:
    recipes=[
        ("object",{"x":1}), ("empty-object",{}), ("null",None), ("false",False), ("true",True),
        ("zero",0), ("integer",7), ("negative",-2), ("float-zero",0.0), ("negative-zero",-0.0),
        ("float",1.25), ("empty-text",""), ("text","x"), ("long-text","abc"),
        ("empty-array",[]), ("pair-array",[["name","World"]]),
        ("repeated-pairs",[["x",1],["x",2]]), ("text-pairs",["ab","cd"]),
        ("unicode-pair",["用户"]), ("integer-row",[1]), ("empty-row",[[]]),
        ("short-row",[["x"]]), ("long-row",[["x",1,2]]),
    ]

    return dict(cases=capture_mcp_model_root_cases(recipes, batch_names=("true", "pair-array", "integer-row")),limits=[
            "Actual finite MCP roots in Source model loop; no non-string pair keys/nonfinite root profile claimed.",
            "All batch arguments are copied before effects. MCP readonly hint never lowers external risk.",
            "Original assistant model input remains unchanged while execution receives the prepared object.",
        ])


def capture_mcp_model_root_cases(recipes, *, batch_names=(), batch_all=False, journal=False) -> list[dict]:
    from mini_loop.agent import Agent
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, ToolUseBlock, text
    from mini_loop.mcp import MCPClient, register_mcp
    from mini_loop.permissions import PermissionHook
    from mini_loop.registry import Hook, Hooks


    async def capture(name,value,mode,batch,root):
        trace,results,calls,requests=[],[],[],[]
        class Peer(MCPClient):
            name="peer"
            async def list_tools(self):return [dict(name="echo",description="echo",input_schema={"type":"object"},annotations={"readOnlyHint":True})]
            async def call_tool(self,name,args):
                trace.append("handler");calls.append(copy.deepcopy(args))
                return json.dumps(args,ensure_ascii=False,sort_keys=True,separators=(",",":"))
            async def close(self):pass
        class Probe(Hook):
            async def before_tool(self,ctx,call):trace.append("before")
            async def after_tool(self,ctx,call,output):trace.append("after");return output
            async def on_result(self,ctx,call,output,*,denied=False,failed=False):
                trace.append("result");results.append(dict(output=output,denied=denied,failed=failed))
        def responder(kwargs):
            requests.append(copy.deepcopy(kwargs))
            if len(requests)>1:return [text("done")],"end_turn"
            blocks=[]
            if batch:blocks.append(ToolUseBlock("mcp__peer__echo",{"first":1},"first"))
            blocks.append(ToolUseBlock("mcp__peer__echo",copy.deepcopy(value),"model"))
            return blocks,"tool_use"
        state={"permission_mode":mode}
        if journal:
            from mini_loop.actions import InMemoryActionJournal
            state["action_journal"]=InMemoryActionJournal()
        agent=Agent(client=FakeAsyncAnthropic(responder=responder,thinking=False),workspace=root,
                    settings=Settings(fake_llm=True,skills_dir=root/"skills",spill_dir=None),
                    hooks=Hooks([Probe(),PermissionHook()]),state=state)
        await register_mcp(agent,Peer())
        try:output=await agent.run("call MCP");error=None
        except Exception as failure:output=None;error=dict(kind=type(failure).__name__,message=str(failure))
        row=dict(name=mode+"/"+name+("/batch" if batch else ""),mode=mode,input_json=json.dumps(value,ensure_ascii=False),
                    batch=batch,output=output,error=error,trace=trace,calls=calls,results=results,
                    model_input_unchanged=json.dumps(agent.messages[1]["content"][-1]["input"],ensure_ascii=True)==json.dumps(value,ensure_ascii=True),
                    requests=len(requests))
        if journal:
            records=state["action_journal"]._records.values()
            row["actions"]=[dict(tool_use_id=r.tool_use_id,status=r.status,input_hash=r.input_hash,result=r.result)
                            for r in sorted(records,key=lambda r:r.tool_use_id)]
        return row
    with tempfile.TemporaryDirectory(prefix="mini-loop-mcp-model-root-") as scratch:
        async def collect():
            rows=[]
            for mode in ["auto","readonly"]:
                for name,value in recipes:
                    for batch in [False,True] if batch_all or name in batch_names else [False]:
                        root=Path(scratch)/(mode+"-"+name+("-batch" if batch else ""));root.mkdir()
                        rows.append(await capture(name,value,mode,batch,root))
            return rows
        return asyncio.run(collect())
