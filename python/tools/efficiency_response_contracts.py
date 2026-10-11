"""Actual response setting constructors, policies, limits and sanitized receipts."""
import asyncio
from dataclasses import asdict
from mini_loop.token_efficiency import (
    ComponentDescriptor, ConciseResponsePolicy, ConciseResponsePolicySettings,
    PassthroughResponsePolicy, ResponsePolicyContext, ResponsePolicyPlan,
    StableRequestSettings, TokenEfficiencyRegistry,
)


def efficiency_response_contracts() -> dict:
    def settings(fields):
        fields=dict(fields)
        if "instructions" in fields:fields["instructions"]=tuple(fields["instructions"])
        if "metadata" in fields:fields["metadata"]=tuple(tuple(pair) for pair in fields["metadata"])
        return StableRequestSettings(**fields)
    constructors=[]
    for name,fields in [
        ("empty",{}),("one",dict(instructions=["do not remove negation"])),
        ("repr",dict(instructions=["can't",'say "yes"',"π\n\x1c"],metadata=[["one","a'b"],["two","😀"]],verbosity="concise",max_output_tokens=10**100)),
        ("instruction-bound",dict(instructions=["😀"*2048])),
        ("instruction-long",dict(instructions=["x"*2049])),
        ("instruction-count",dict(instructions=[""]*17)),
        ("instruction-total-bound",dict(instructions=["x"*2048]*4)),
        ("instruction-total-long",dict(instructions=["x"*2000]*5)),
        ("metadata-bound",dict(metadata=[["x"*128,"😀"*512]])),
        ("metadata-key-empty",dict(metadata=[["","x"]])),
        ("metadata-key-long",dict(metadata=[["x"*129,"x"]])),
        ("metadata-value-long",dict(metadata=[["x","x"*513]])),
        ("metadata-count",dict(metadata=[[str(i),"x"] for i in range(33)])),
        ("metadata-duplicate",dict(metadata=[["x","one"],["x","two"]])),
        ("zero-output",dict(max_output_tokens=0)),("negative-output",dict(max_output_tokens=-1)),
        ("empty-verbosity",dict(verbosity="")),("long-verbosity",dict(verbosity="x"*65)),
        ("verbosity-bound",dict(verbosity="😀"*64)),
    ]:
        output,error,representation=None,None,None
        try:
            value=settings(fields);output=asdict(value);representation=repr(value)
        except Exception as fault:error=dict(kind=type(fault).__name__,message=str(fault))
        constructors.append(dict(name=name,input=fields,output=output,repr=representation,error=error))
    def component(id="probe",action="change",mode="enforce",**fields):
        return dict(id=id,action=action,mode=mode,fields=fields)
    def concise(**options):return component(id="concise-response",action="concise",**options)
    specs=[
        ("empty",[]),("off",[component(mode="off")]),
        ("default-off",[component(mode=None)]),("default-enforce",[component(mode=None)],dict(default_mode="enforce")),
        ("enforce",[component()]),("shadow",[component(mode="shadow")]),
        ("unchanged",[component(action="same")]),
        ("raise-existing",[component(action="cap",cap=2000)],dict(settings=dict(max_output_tokens=1000))),
        ("remove-existing",[component(action="cap",cap=None)],dict(settings=dict(max_output_tokens=1000))),
        ("raise-budget",[component(action="cap",cap=2000)],dict(budget=1000)),
        ("remove-budget",[component(action="cap",cap=None)],dict(budget=1000)),
        ("tighter-budget",[component(action="cap",cap=1500)],dict(settings=dict(max_output_tokens=2000),budget=1000)),
        ("tighter-existing",[component(action="cap",cap=1500)],dict(settings=dict(max_output_tokens=1000),budget=2000)),
        ("shadow-before-cap",[component(action="cap",cap=2000,mode="shadow")],dict(settings=dict(max_output_tokens=1000))),
        ("unchanged-before-cap",[component(action="same")],dict(settings=dict(max_output_tokens=2000),budget=1000)),
        ("lower-cap",[component(action="cap",cap=500)],dict(settings=dict(max_output_tokens=1000))),
        ("no-ceiling",[component(action="cap",cap=10**100)]),
        ("double",[component(),component(id="second",action="metadata")]),
        ("double-disabled",[component(),component(id="second",action="metadata")],dict(prevent_double_reduction=False)),
        ("upstream",[component()],dict(applied_by=["upstream","upstream"])),
        ("off-before-owner",[component(mode="off")],dict(applied_by=["upstream"])),
        ("bad-owner",[component()],dict(applied_by=["private marker"])),
        ("input-cap-unused",[component(max_input_bytes=1)]),
        ("failure",[component(action="failure")]),("unsafe",[component(action="unsafe")]),
        ("timeout",[component(action="timeout",timeout_ms=5)]),
        ("cancel",[component(action="cancel")]),("wrong",[component(action="wrong")]),
        ("forged",[component(action="forged")]),("warning",[component(action="warning")]),
        ("fallback-then-good",[component(action="failure"),component(id="second")]),
        ("counter-broken",[component()],dict(counter="broken")),
        ("counter-negative",[component()],dict(counter="negative")),
        ("counter-large",[component()],dict(counter="large")),
        ("closed",[component(),component(id="second",mode="off")],dict(closed=True)),
        ("passthrough",[component(id="passthrough-response",action="passthrough")]),
        ("concise-not-requested",[concise()]),
        ("concise",[concise()],dict(concise_requested=True)),
        ("concise-shadow",[concise(mode="shadow")],dict(concise_requested=True)),
        ("concise-existing",[concise()],dict(concise_requested=True,settings=dict(max_output_tokens=500))),
        ("concise-budget",[concise()],dict(concise_requested=True,budget=300,settings=dict(max_output_tokens=2000))),
        ("concise-force",[concise(require_opt_in=False)]),
        ("concise-no-cap",[concise(max_output_tokens=None)],dict(concise_requested=True)),
        ("concise-metadata",[concise()],dict(concise_requested=True,settings=dict(metadata=[["response_policy","old"],["other","kept"]]))),
        ("concise-long-instruction",[concise(instruction="x"*2049)],dict(concise_requested=True)),
        ("concise-metadata-overflow",[concise()],dict(concise_requested=True,settings=dict(metadata=[[str(i),"x"] for i in range(32)]))),
        ("concise-instruction-overflow",[concise()],dict(concise_requested=True,settings=dict(instructions=[str(i) for i in range(16)]))),
        ("concise-already-included",[concise()],dict(concise_requested=True,settings=dict(instructions=[ConciseResponsePolicySettings().instruction]))),
    ]
    async def capture():
        rows=[]
        for recipe in specs:
            name,components,*tail=recipe;options=tail[0] if tail else {}
            calls=[];counter_calls=[];registry=TokenEfficiencyRegistry()
            class Policy:
                def __init__(self,spec):
                    self.spec=spec
                    self.descriptor=ComponentDescriptor(id=spec["id"],version="1",stage="response_policy",**{k:v for k,v in spec["fields"].items() if k in ("timeout_ms","max_input_bytes")})
                async def plan(self,context):
                    calls.append(dict(id=self.spec["id"],context=asdict(context)))
                    action=self.spec["action"]
                    if action=="failure":raise RuntimeError("private policy diagnostic")
                    if action=="unsafe":raise type("Bad private name",(Exception,),{})("private")
                    if action=="timeout":await asyncio.sleep(1)
                    if action=="cancel":raise asyncio.CancelledError()
                    if action=="wrong":return "private wrong result"
                    fields=asdict(context.settings)
                    if action=="change":fields["verbosity"]="brief"
                    if action=="cap":fields["verbosity"]="brief";fields["max_output_tokens"]=self.spec["fields"]["cap"]
                    if action=="metadata":fields["metadata"]=[*fields["metadata"],["second","applied"]]
                    candidate=settings(fields)
                    if action=="forged":object.__setattr__(candidate,"instructions",("x"*2049,))
                    result=ResponsePolicyPlan(candidate,("settings_changed",) if action=="change" else ())
                    if action=="warning":object.__setattr__(result,"warnings",("private invalid warning",))
                    return result
            for spec in components:
                action=spec["action"]
                policy=ConciseResponsePolicy(ConciseResponsePolicySettings(**spec["fields"])) if action=="concise" else PassthroughResponsePolicy() if action=="passthrough" else Policy(spec)
                registry.register_response_policy(policy,mode=spec["mode"])
            def count(text):
                counter_calls.append(text)
                if options.get("counter")=="broken":raise RuntimeError("private counter")
                if options.get("counter")=="negative":return -1
                if options.get("counter")=="large":return 10**100
                return (len(text.encode())+3)//4
            runtime=registry.runtime(default_mode=options.get("default_mode","off"),prevent_double_reduction=options.get("prevent_double_reduction",True),token_counter=count)
            if options.get("closed"):await runtime.close()
            original=ResponsePolicyContext(task="answer π",settings=settings(options.get("settings",{})),provider_capabilities=("verbosity","max_tokens"),budget_tokens=options.get("budget"),concise_requested=options.get("concise_requested",False),applied_by=options.get("applied_by",()))
            output,error=None,None
            try:
                result=await runtime.plan_response(original)
                receipts=[r.as_dict() for r in result.receipts]
                for r in receipts:r["elapsed_ms"]=0
                output=dict(context=asdict(result.context),receipts=receipts)
            except BaseException as fault:error=dict(kind=type(fault).__name__,message=str(fault))
            rows.append(dict(name=name,components=components,options=options,input=asdict(original),output=output,error=error,calls=calls,counter_calls=counter_calls))
        return rows
    return dict(settings=constructors,rows=asyncio.run(capture()))
