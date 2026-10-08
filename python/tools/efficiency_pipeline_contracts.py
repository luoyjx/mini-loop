"""Real observation runtime modes/guards/storage/faults with controlled reducer SPI."""
import asyncio
import base64
from dataclasses import asdict
from pathlib import Path
import tempfile
from unittest.mock import patch
from mini_loop.token_efficiency import (ComponentDescriptor, MaskedObservation,
    MaskedRawArtifactStore, ObservationReduction, TokenEfficiencyRegistry)


def efficiency_pipeline_contracts() -> dict:
    base_text = "building\n" * 20
    def component(id="probe", action="shorten", mode=None, **fields):
        return dict(id=id, action=action, mode=mode, fields=fields)
    specs = [
        ("empty", []), ("default-off", [component()]),
        ("default-enforce", [component()], dict(default_mode="enforce")),
        ("override-off", [component(mode="off")], dict(default_mode="enforce")),
        ("shadow", [component(mode="shadow")]),
        ("enforce", [component(mode="enforce")]),
        ("passthrough", [component(action="same",mode="enforce")]),
        ("inflate-bytes", [component(action="inflate",mode="enforce")]),
        ("inflate-tokens", [component(mode="enforce")], dict(counter="inverse")),
        ("inflation-disabled", [component(action="inflate",mode="enforce")], dict(inflation_guard=False)),
        ("double", [component(mode="enforce"),component(id="second",mode="enforce")]),
        ("double-disabled", [component(mode="enforce"),component(id="second",action="empty",mode="enforce")], dict(prevent_double_reduction=False)),
        ("upstream-owned", [component(mode="enforce")], dict(reduced_by=["upstream"])),
        ("unsupported", [component(mode="enforce")], dict(content_type="image/png")),
        ("byte-cap", [component(mode="enforce",max_input_bytes=1)]),
        ("recoverable-no-persist", [component(mode="enforce",lossiness="recoverable",recoverable=True)]),
        ("recoverable-no-store", [component(mode="enforce",lossiness="recoverable",recoverable=True)], dict(persist=True)),
        ("recoverable-store", [component(mode="enforce",lossiness="recoverable",recoverable=True)], dict(persist=True,store="normal")),
        ("recoverable-closed", [component(mode="enforce",lossiness="recoverable",recoverable=True)], dict(persist=True,store="closed")),
        ("recoverable-small", [component(mode="enforce",lossiness="recoverable",recoverable=True)], dict(persist=True,store="small")),
        ("existing-ref", [component(mode="enforce",lossiness="recoverable",recoverable=True)], dict(raw_ref="raw_"+"x"*43,raw_digest="sha256:"+"a"*64)),
        ("storage-fallback-second", [component(mode="enforce",lossiness="recoverable",recoverable=True),component(id="second",mode="enforce")]),
        ("duplicate-storage-warnings", [component(mode="enforce",lossiness="recoverable",recoverable=True),component(id="second",mode="enforce",lossiness="recoverable",recoverable=True)]),
        ("runtime-error", [component(action="RuntimeError",mode="enforce")]),
        ("value-error", [component(action="ValueError",mode="enforce")]),
        ("unsafe-error-name", [component(action="unsafe-error",mode="enforce")]),
        ("wrong-result", [component(action="wrong-result",mode="enforce")]),
        ("invalid-warning", [component(action="invalid-warning",mode="enforce")]),
        ("timeout", [component(action="timeout",mode="enforce",timeout_ms=1)]),
        ("cancelled", [component(action="cancelled",mode="enforce")]),
        ("fault-fallback-second", [component(action="RuntimeError",mode="enforce"),component(id="second",mode="enforce")]),
        ("broken-counter", [component(mode="enforce")], dict(counter="broken")),
        ("negative-counter", [component(mode="enforce")], dict(counter="negative")),
        ("large-counter", [component(action="same",mode="enforce")], dict(counter="large")),
        ("shadow-before-enforce", [component(mode="shadow"),component(id="second",mode="enforce")]),
        ("off-after-enforce", [component(mode="enforce"),component(id="second",mode="off")]),
        ("shadow-inflation", [component(action="inflate",mode="shadow")]),
        ("metadata-preserved", [component(mode="enforce")], dict(metadata=[["exit_code",7],["timed_out",False]])),
    ]
    async def capture(root):
        rows=[]
        for spec in specs:
            name,components,*options=spec
            options=options[0] if options else {}
            calls=[]
            class Probe:
                def __init__(self,c):
                    self.recipe=c
                    self.descriptor=ComponentDescriptor(id=c["id"],version="1",stage="observation",content_types=("text/*",),**c["fields"])
                async def reduce(self,observation,*,query=None,budget_tokens=None):
                    action=self.recipe["action"]
                    calls.append(dict(component=self.descriptor.id,content=observation.content,query=query,budget_tokens=budget_tokens))
                    if action in ("RuntimeError","ValueError"): raise {"RuntimeError":RuntimeError,"ValueError":ValueError}[action]("private payload must not escape")
                    if action=="unsafe-error": raise type("SECRET=value",(RuntimeError,),{})("private")
                    if action=="cancelled": raise asyncio.CancelledError()
                    if action=="timeout": await asyncio.sleep(1)
                    if action=="wrong-result": return "bad result"
                    if action=="invalid-warning": return ObservationReduction("short",("unsafe warning",))
                    content=observation.content if action=="same" else observation.content+"extra" if action=="inflate" else "" if action=="empty" else "short"
                    return ObservationReduction(content,("candidate:1","candidate:1"))
            registry=TokenEfficiencyRegistry()
            for c in components: registry.register_observation(Probe(c),mode=c["mode"])
            store=None
            if options.get("store"):
                store=MaskedRawArtifactStore(root,clock=lambda:0,max_artifact_bytes=1 if options["store"]=="small" else 2_000_000)
                if options["store"]=="closed":store.close()
            counter_name=options.get("counter","normal")
            def counter(text):
                if counter_name=="broken":raise RuntimeError("counter unavailable")
                if counter_name=="negative":return -1
                if counter_name=="large":return 10**100
                if counter_name=="inverse":return 1000 if text=="short" else 1
                return (len(text.encode())+3)//4
            runtime=registry.runtime(default_mode=options.get("default_mode","off"),raw_store=store,
                inflation_guard=options.get("inflation_guard",True),prevent_double_reduction=options.get("prevent_double_reduction",True),token_counter=counter)
            obs=MaskedObservation(base_text,content_type=options.get("content_type","text/plain"),reduced_by=tuple(options.get("reduced_by",[])),raw_ref=options.get("raw_ref"),raw_digest=options.get("raw_digest"),metadata=tuple(options.get("metadata",[])))
            output,error,recovered=None,None,None
            with patch("mini_loop.token_efficiency.secrets.token_urlsafe",return_value=base64.urlsafe_b64encode(bytes([1])*32).decode().rstrip("=")):
                try:
                    result=await runtime.reduce_observation(obs,query="masked query",budget_tokens=100,persist_masked_raw=options.get("persist",False))
                    receipts=[r.as_dict() for r in result.receipts]
                    for r in receipts:r["elapsed_ms"]=0
                    output=dict(observation=asdict(result.observation),receipts=receipts,warnings=list(result.warnings))
                    if store and result.observation.raw_ref:recovered=store.get_masked(result.observation.raw_ref)
                except BaseException as fault:
                    error=dict(kind=type(fault).__name__,message=str(fault))
            rows.append(dict(name=name,components=components,options=options,input=asdict(obs),output=output,error=error,calls=calls,recovered=recovered,enforced=runtime.observation_enforced))
            if store:store.close()
        return rows
    with tempfile.TemporaryDirectory() as directory:
        return dict(rows=asyncio.run(capture(Path(directory))))
