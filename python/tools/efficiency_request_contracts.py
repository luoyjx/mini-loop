"""Actual request optimizer ordering, detached copies and frozen-prefix receipts."""
import asyncio
import copy
import json
from mini_loop.token_efficiency import (
    ComponentDescriptor, RequestContext, RequestOptimization, TokenEfficiencyRegistry,
    PassthroughRequestContextOptimizer,
)


def efficiency_request_contracts() -> dict:
    def encoded(value):
        return json.dumps(value, ensure_ascii=True, separators=(",", ":"))
    def component(id="probe",action="tail",mode="enforce",**fields):
        return dict(id=id,action=action,mode=mode,fields=fields)
    base=dict(messages=[dict(role="system",content="stable π"),dict(role="user",content="long "*40)],model="long-model-name")
    specs=[
        ("empty",[]), ("off",[component(mode="off")]),
        ("default-off",[component(mode=None)]),
        ("default-enforce",[component(mode=None)],dict(default_mode="enforce")),
        ("enforce",[component()]), ("shadow",[component(mode="shadow")]),
        ("unchanged",[component(action="same")]),
        ("prefix",[component(action="prefix")]),
        ("prefix-shadow",[component(action="prefix",mode="shadow")]),
        ("prefix-zero",[component(action="prefix")],dict(frozen=0)),
        ("missing-messages",[component(action="remove")]),
        ("short-messages",[component(action="clear")]),
        ("large-prefix",[component()],dict(frozen=10**100)),
        ("outside-prefix",[component(action="model")]),
        ("protocol-shape-library",[component(action="role")],dict(frozen=0)),
        ("byte-inflation",[component(action="inflate")]),
        ("shadow-inflation",[component(action="inflate",mode="shadow")]),
        ("inflation-disabled",[component(action="inflate")],dict(inflation_guard=False)),
        ("token-inflation",[component()],dict(counter="inverse")),
        ("byte-cap",[component(max_input_bytes=1)]),
        ("double",[component(),component(id="second",action="model")]),
        ("double-disabled",[component(),component(id="second",action="model")],dict(prevent_double_reduction=False)),
        ("upstream",[component()],dict(optimized_by=["upstream","upstream"])),
        ("off-before-double",[component(mode="off")],dict(optimized_by=["upstream"])),
        ("bad-upstream",[component()],dict(optimized_by=["private marker"])),
        ("mutating-failure",[component(action="mutate-fail")]),
        ("safe-failure",[component(action="failure")]),
        ("unsafe-failure",[component(action="unsafe")]),
        ("timeout",[component(action="timeout",timeout_ms=5)]),
        ("cancel",[component(action="cancel")]),
        ("wrong-result",[component(action="wrong")]),
        ("wrong-warning",[component(action="warning")]),
        ("fallback-then-good",[component(action="failure"),component(id="second")]),
        ("broken-counter",[component()],dict(counter="broken")),
        ("negative-counter",[component()],dict(counter="negative")),
        ("large-counter",[component()],dict(counter="large")),
        ("nonfinite-stable",[component()],dict(extra=float("nan"))),
        ("surrogate-stable",[component()],dict(extra="\ud800")),
        ("closed",[component(),component(id="second",mode="off")],dict(closed=True)),
        ("builtin-passthrough",[component(id="passthrough-request",action="builtin")]),
    ]
    async def capture():
        rows=[]
        for recipe in specs:
            name,components,*tail=recipe; options=tail[0] if tail else {}
            calls=[];counter_calls=[];registry=TokenEfficiencyRegistry()
            class Optimizer:
                def __init__(self,spec):
                    self.spec=spec
                    self.descriptor=ComponentDescriptor(id=spec["id"],version="1",stage="request_context",**spec["fields"])
                async def optimize(self,context,*,budget_tokens=None):
                    calls.append(dict(id=self.spec["id"],request_json=encoded(context.request),frozen=context.frozen_prefix_messages,optimized_by=list(context.optimized_by),budget=budget_tokens))
                    request=copy.deepcopy(dict(context.request));action=self.spec["action"]
                    if action=="timeout":await asyncio.sleep(1)
                    if action=="cancel":raise asyncio.CancelledError()
                    if action=="wrong":return "private wrong result"
                    if action=="mutate-fail":context.request["messages"][0]["content"]="mutated private"
                    if action in ("failure","mutate-fail"):raise RuntimeError("private optimizer diagnostic")
                    if action=="unsafe":raise type("Bad private name",(Exception,),{})("private")
                    if action=="tail":request["messages"][-1]["content"]="x"
                    if action=="prefix":request["messages"][0]["content"]="x"
                    if action=="model":request["model"]="x"
                    if action=="role":request["messages"][-1]["role"]="changed"
                    if action=="remove":request.pop("messages",None)
                    if action=="clear":request["messages"]=[]
                    if action=="inflate":request["messages"][-1]["content"]="long "*400
                    result=RequestOptimization(request,("tail_changed",) if action=="tail" else ())
                    if action=="warning":object.__setattr__(result,"warnings",("private invalid warning",))
                    return result
            for spec in components:registry.register_request_optimizer(PassthroughRequestContextOptimizer() if spec["action"]=="builtin" else Optimizer(spec),mode=spec["mode"])
            request=copy.deepcopy(base)
            if "extra" in options:request["extra"]=options["extra"]
            original=encoded(request)
            frozen=options.get("frozen",1);markers=options.get("optimized_by",[])
            counter=options.get("counter","cheap")
            def count(text):
                counter_calls.append(text)
                if counter=="broken":raise RuntimeError("counter private")
                if counter=="negative":return -1
                if counter=="large":return 10**100
                if counter=="inverse":return 1000 if '"content":"x"' in text else 1
                return (len(text.encode())+3)//4
            runtime=registry.runtime(default_mode=options.get("default_mode","off"),inflation_guard=options.get("inflation_guard",True),prevent_double_reduction=options.get("prevent_double_reduction",True),token_counter=count)
            if options.get("closed"):await runtime.close()
            output,error=None,None
            try:
                result=await runtime.optimize_request(RequestContext(request,frozen_prefix_messages=frozen,optimized_by=markers),budget_tokens=10**30)
                receipts=[r.as_dict() for r in result.receipts]
                for r in receipts:r["elapsed_ms"]=0
                output=dict(request_json=encoded(result.context.request),frozen=result.context.frozen_prefix_messages,optimized_by=list(result.context.optimized_by),receipts=receipts)
            except BaseException as fault:error=dict(kind=type(fault).__name__,message=str(fault))
            rows.append(dict(name=name,components=components,options={k:v for k,v in options.items() if k!="extra"},input_json=original,frozen=frozen,optimized_by=markers,output=output,error=error,calls=calls,counter_calls=counter_calls,input_unchanged=encoded(request)==original))
        return rows
    return dict(rows=asyncio.run(capture()))
