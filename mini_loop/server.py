"""FastAPI server exposing many concurrent agents.

Concurrency model:
  * one shared event loop, one shared LLM client, one shared LLM semaphore;
  * each session is an independent Agent with its own workspace + history;
  * a session's runs are serialized by its own lock, but *different* sessions
    run truly concurrently -- while agent A awaits the model, agent B's loop
    keeps going. Blocking tool calls (bash, file I/O) are offloaded to threads
    inside the agent, so they never freeze the loop the others share.

Endpoints
  GET    /                          embedded console + endpoint list
  GET    /healthz                   liveness + config
  POST   /sessions                  {system?, model?} -> session info
  GET    /sessions                  list sessions
  GET    /sessions/{id}             session info (status, todos, msg count)
  DELETE /sessions/{id}             drop session + workspace
  POST   /sessions/{id}/messages    {message} -> run to completion, return final text
  POST   /sessions/{id}/messages/stream   {message} -> SSE of live events
  GET    /sessions/{id}/events      SSE: observe a session's event stream
"""

from __future__ import annotations

import asyncio
import contextlib
import json
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException
from fastapi.responses import HTMLResponse
from pydantic import BaseModel
from sse_starlette.sse import EventSourceResponse

from .config import build_client, load_settings
from .manager import SessionManager


class CreateSessionReq(BaseModel):
    system: str | None = None
    model: str | None = None


class MessageReq(BaseModel):
    message: str


@asynccontextmanager
async def lifespan(app: FastAPI):
    settings = load_settings()
    client = build_client(settings)
    app.state.settings = settings
    app.state.manager = SessionManager(settings, client)
    yield
    with contextlib.suppress(Exception):
        await client.close()  # AsyncAnthropic exposes .close(); fake doesn't, hence suppress


app = FastAPI(title="mini-loop", version="0.1.0", lifespan=lifespan)


def _manager(app: FastAPI) -> SessionManager:
    return app.state.manager


def _require(session_id: str) -> "AgentSession":  # noqa: F821
    session = _manager(app).get(session_id)
    if session is None:
        raise HTTPException(status_code=404, detail=f"No session '{session_id}'")
    return session


@app.get("/healthz")
async def healthz():
    s = app.state.settings
    return {"status": "ok", "model": s.model, "fake_llm": s.fake_llm,
            "max_concurrent_llm": s.max_concurrent_llm, "sessions": len(_manager(app).list())}


@app.post("/sessions")
async def create_session(req: CreateSessionReq):
    session = _manager(app).create(system=req.system, model=req.model)
    return session.info()


@app.get("/sessions")
async def list_sessions():
    return [s.info() for s in _manager(app).list()]


@app.get("/sessions/{session_id}")
async def get_session(session_id: str):
    return _require(session_id).info()


@app.delete("/sessions/{session_id}")
async def delete_session(session_id: str):
    ok = _manager(app).delete(session_id)
    if not ok:
        raise HTTPException(status_code=404, detail=f"No session '{session_id}'")
    return {"deleted": session_id}


@app.post("/sessions/{session_id}/messages")
async def post_message(session_id: str, req: MessageReq):
    session = _require(session_id)
    final = await session.run(req.message)
    return {"session": session_id, "final": final, "info": session.info()}


@app.post("/sessions/{session_id}/messages/stream")
async def post_message_stream(session_id: str, req: MessageReq):
    session = _require(session_id)

    async def gen():
        q = session.subscribe(replay=False)
        run_task = asyncio.create_task(session.run(req.message))
        try:
            while True:
                getter = asyncio.ensure_future(q.get())
                done, _ = await asyncio.wait({getter, run_task}, return_when=asyncio.FIRST_COMPLETED)
                if getter in done:
                    event = getter.result()
                    yield {"event": event["type"], "data": json.dumps(event)}
                else:
                    getter.cancel()
                    # run finished: flush any trailing events, then stop
                    while not q.empty():
                        event = q.get_nowait()
                        yield {"event": event["type"], "data": json.dumps(event)}
                    break
        finally:
            session.unsubscribe(q)
            with contextlib.suppress(Exception):
                await run_task

    return EventSourceResponse(gen())


@app.get("/sessions/{session_id}/events")
async def observe(session_id: str):
    session = _require(session_id)

    async def gen():
        q = session.subscribe(replay=True)
        try:
            while True:
                event = await q.get()
                yield {"event": event["type"], "data": json.dumps(event)}
        finally:
            session.unsubscribe(q)

    return EventSourceResponse(gen())


@app.get("/", response_class=HTMLResponse)
async def console():
    return CONSOLE_HTML


CONSOLE_HTML = """<!doctype html>
<meta charset="utf-8"><title>mini-loop console</title>
<style>
 body{font:14px ui-monospace,Menlo,monospace;margin:0;background:#0d1117;color:#c9d1d9}
 header{padding:10px 14px;background:#161b22;border-bottom:1px solid #30363d}
 main{display:flex;gap:10px;padding:10px;flex-wrap:wrap}
 .col{flex:1;min-width:320px;background:#161b22;border:1px solid #30363d;border-radius:8px;padding:10px}
 input,button,textarea{font:inherit;background:#0d1117;color:#c9d1d9;border:1px solid #30363d;border-radius:6px;padding:6px}
 button{cursor:pointer;background:#238636;border-color:#2ea043;color:#fff}
 button.sec{background:#21262d;border-color:#30363d;color:#c9d1d9}
 .log{height:60vh;overflow:auto;white-space:pre-wrap;font-size:12px}
 .ev{margin:2px 0;padding:2px 4px;border-radius:4px}
 .text{color:#79c0ff}.tool_use{color:#d2a8ff}.tool_result{color:#8b949e}
 .done{color:#3fb950}.error{color:#f85149}.status,.compact,.subagent_start,.subagent_end,.todo{color:#e3b341}
 .id{color:#58a6ff}
</style>
<header><b>mini-loop</b> &mdash; minimal complete agent, served concurrently.
 Open this page in two tabs and run both to watch agents work in parallel.</header>
<main>
 <div class="col">
  <div>system prompt (optional):<br><textarea id="sys" rows="2" style="width:100%"></textarea></div>
  <div style="margin-top:6px"><button onclick="mk()">+ new session</button>
   <span id="sid" class="id"></span></div>
  <div style="margin-top:8px"><textarea id="msg" rows="3" style="width:100%"
       placeholder="message to the agent">build a hello world script and run it</textarea></div>
  <div style="margin-top:6px"><button onclick="send()">run (stream)</button>
   <button class="sec" onclick="document.getElementById('log').innerHTML=''">clear</button></div>
 </div>
 <div class="col"><div class="log" id="log"></div></div>
</main>
<script>
let sid=null;
const log=document.getElementById('log');
function line(t,c){const d=document.createElement('div');d.className='ev '+(c||'');d.textContent=t;log.appendChild(d);log.scrollTop=log.scrollHeight;}
async function mk(){
 const system=document.getElementById('sys').value||null;
 const r=await fetch('/sessions',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({system})});
 const j=await r.json(); sid=j.id; document.getElementById('sid').textContent=' session '+sid; line('created session '+sid,'status');
}
async function send(){
 if(!sid){await mk();}
 const message=document.getElementById('msg').value;
 line('>>> '+message,'id');
 const r=await fetch('/sessions/'+sid+'/messages/stream',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({message})});
 const reader=r.body.getReader();const dec=new TextDecoder();let buf='';
 while(true){const {value,done}=await reader.read();if(done)break;buf+=dec.decode(value,{stream:true});
  let parts=buf.split('\\n\\n');buf=parts.pop();
  for(const p of parts){let ev='message',data='';for(const ln of p.split('\\n')){if(ln.startsWith('event:'))ev=ln.slice(6).trim();if(ln.startsWith('data:'))data+=ln.slice(5).trim();}
   if(!data)continue;let o={};try{o=JSON.parse(data)}catch(e){continue;}
   let txt=ev;
   if(ev==='assistant_text')txt='💬 '+o.text;
   else if(ev==='tool_use')txt='🔧 '+o.name+' '+JSON.stringify(o.input);
   else if(ev==='tool_result')txt='   ↳ '+o.output;
   else if(ev==='done')txt='✅ '+o.text;
   else if(ev==='error')txt='❌ '+o.error;
   else if(ev==='subagent_start')txt='┌ subagent('+o.agent_type+')';
   else if(ev==='subagent_end')txt='└ subagent → '+o.summary;
   else if(ev==='todo')txt='📋 '+o.items.map(i=>i.status[0]+' '+i.content).join(' | ');
   else if(ev==='compact')txt='🗜 compact('+o.kind+')';
   else if(ev==='status')txt='• '+o.status;
   line(txt,ev);
  }
 }
}
</script>
"""
