"""FastAPI server exposing many concurrent agents.

Concurrency model:
  * one shared event loop, one shared LLM client, one shared LLM semaphore;
  * each session is an independent Agent with its own workspace + history;
  * a session's runs are serialized by its own lock, but *different* sessions
    run truly concurrently -- while agent A awaits the model, agent B's loop
    keeps going. Blocking tool calls (bash, file I/O) are offloaded to threads
    inside the agent, so they never freeze the loop the others share.

Extensibility:
  Handlers read the manager from `request.app.state.manager`, and the app is
  built by `create_app(...)`. To serve a *customized* fleet (your tools, hooks,
  prompt, workspace factory) build a SessionManager and pass it in:

      from mini_loop.server import create_app
      app = create_app(manager=my_manager)

  The module-level `app = create_app()` is the default fleet, used by
  `python -m mini_loop` and `uvicorn mini_loop.server:app`.

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
  GET    /sessions/{id}/trajectories      list durable runs for one session
  GET    /trajectories/{id}         inspect one recorded trajectory
  GET    /trajectories/{id}/export  download JSON or JSONL
"""

from __future__ import annotations

import asyncio
import contextlib
import json
from contextlib import asynccontextmanager
from collections.abc import Callable

from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import HTMLResponse, Response
from pydantic import BaseModel
from sse_starlette.sse import EventSourceResponse

from .config import Settings, build_client, load_settings
from .manager import SessionManager
from .session import AgentSession


class CreateSessionReq(BaseModel):
    system: str | None = None
    model: str | None = None


class MessageReq(BaseModel):
    message: str


def _manager(request: Request) -> SessionManager:
    return request.app.state.manager


def _require(request: Request, session_id: str) -> AgentSession:
    session = _manager(request).get(session_id)
    if session is None:
        raise HTTPException(status_code=404, detail=f"No session '{session_id}'")
    return session


def _trajectory_store(request: Request):
    store = _manager(request).trajectories
    if store is None:
        raise HTTPException(status_code=503, detail="Trajectory recording is disabled")
    return store


def create_app(
    *,
    settings: Settings | None = None,
    manager: SessionManager | None = None,
    manager_factory: Callable[[Settings], SessionManager] | None = None,
) -> FastAPI:
    """Build the FastAPI app. Pass `manager` (or `manager_factory`) to serve a
    customized fleet; omit both for the default."""

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        cfg = settings or load_settings()
        app.state.settings = cfg
        if manager is not None:
            app.state.manager = manager
            owns_client = False
        elif manager_factory is not None:
            app.state.manager = manager_factory(cfg)
            owns_client = False
        else:
            app.state.manager = SessionManager(cfg, build_client(cfg), enable_features=cfg.enable_features)
            owns_client = True
        mgr = app.state.manager
        with contextlib.suppress(Exception):
            await mgr.start()   # starts the cron ticker when features are on
        yield
        with contextlib.suppress(Exception):
            await mgr.stop()
        if owns_client:
            with contextlib.suppress(Exception):
                await mgr.client.close()

    app = FastAPI(title="mini-loop", version="0.1.0", lifespan=lifespan)
    _register_routes(app)
    return app


def _register_routes(app: FastAPI) -> None:
    @app.get("/healthz")
    async def healthz(request: Request):
        s = request.app.state.settings
        return {"status": "ok", "model": s.model, "fake_llm": s.fake_llm,
                "features": s.enable_features, "max_concurrent_llm": s.max_concurrent_llm,
                "trajectories": _manager(request).trajectories is not None,
                "sessions": len(_manager(request).list())}

    @app.post("/sessions")
    async def create_session(request: Request, req: CreateSessionReq):
        return _manager(request).create(system=req.system, model=req.model).info()

    @app.get("/sessions")
    async def list_sessions(request: Request):
        return [s.info() for s in _manager(request).list()]

    @app.get("/sessions/{session_id}")
    async def get_session(request: Request, session_id: str):
        return _require(request, session_id).info()

    @app.delete("/sessions/{session_id}")
    async def delete_session(request: Request, session_id: str):
        if not _manager(request).delete(session_id):
            raise HTTPException(status_code=404, detail=f"No session '{session_id}'")
        return {"deleted": session_id}

    @app.post("/sessions/{session_id}/messages")
    async def post_message(request: Request, session_id: str, req: MessageReq):
        session = _require(request, session_id)
        final = await session.run(req.message)
        return {"session": session_id, "final": final, "info": session.info()}

    @app.post("/sessions/{session_id}/messages/stream")
    async def post_message_stream(request: Request, session_id: str, req: MessageReq):
        session = _require(request, session_id)

        async def gen():
            q = session.subscribe(replay=False)
            run_task = asyncio.create_task(session.run(req.message))
            try:
                while True:
                    getter = asyncio.ensure_future(q.get())
                    done, _ = await asyncio.wait({getter, run_task}, return_when=asyncio.FIRST_COMPLETED)
                    if getter in done:
                        event = getter.result()
                        yield {"id": str(event["seq"]), "event": event["type"],
                               "data": json.dumps(event)}
                    else:
                        getter.cancel()
                        while not q.empty():
                            event = q.get_nowait()
                            yield {"id": str(event["seq"]), "event": event["type"],
                                   "data": json.dumps(event)}
                        break
            finally:
                session.unsubscribe(q)
                with contextlib.suppress(Exception):
                    await run_task

        return EventSourceResponse(gen())

    @app.get("/sessions/{session_id}/events")
    async def observe(request: Request, session_id: str, envelope: bool = False):
        session = _require(request, session_id)

        async def gen():
            q = session.subscribe(replay=True)
            try:
                last_seen = int(request.headers.get("last-event-id", "0"))
            except ValueError:
                last_seen = 0
            try:
                while True:
                    event = await q.get()
                    if event["seq"] <= last_seen:
                        continue
                    yield {
                        "id": str(event["seq"]),
                        "event": "agent_event" if envelope else event["type"],
                        "data": json.dumps(event),
                    }
            finally:
                session.unsubscribe(q)

        return EventSourceResponse(gen())

    @app.get("/sessions/{session_id}/trajectories")
    async def list_session_trajectories(
        request: Request, session_id: str, limit: int = 100
    ):
        return await asyncio.to_thread(
            _trajectory_store(request).list,
            session_id=session_id,
            limit=min(max(limit, 1), 500),
        )

    @app.get("/trajectories")
    async def list_trajectories(
        request: Request, session_id: str | None = None, limit: int = 100
    ):
        return await asyncio.to_thread(
            _trajectory_store(request).list,
            session_id=session_id,
            limit=min(max(limit, 1), 500),
        )

    @app.get("/trajectories/{trajectory_id}/export")
    async def export_trajectory(
        request: Request, trajectory_id: str, format: str = "json"
    ):
        store = _trajectory_store(request)
        try:
            if format == "jsonl":
                content = await asyncio.to_thread(store.raw, trajectory_id)
                media_type, suffix = "application/x-ndjson", "jsonl"
            elif format == "json":
                content = json.dumps(
                    await asyncio.to_thread(store.get, trajectory_id),
                    ensure_ascii=False,
                    indent=2,
                )
                media_type, suffix = "application/json", "json"
            else:
                raise HTTPException(
                    status_code=400, detail="format must be 'json' or 'jsonl'"
                )
        except (KeyError, ValueError):
            raise HTTPException(
                status_code=404, detail=f"No trajectory '{trajectory_id}'"
            ) from None
        return Response(
            content=content,
            media_type=media_type,
            headers={
                "Content-Disposition": (
                    f'attachment; filename="{trajectory_id}.{suffix}"'
                )
            },
        )

    @app.get("/trajectories/{trajectory_id}")
    async def get_trajectory(request: Request, trajectory_id: str):
        try:
            return await asyncio.to_thread(
                _trajectory_store(request).get, trajectory_id
            )
        except (KeyError, ValueError):
            raise HTTPException(
                status_code=404, detail=f"No trajectory '{trajectory_id}'"
            ) from None

    @app.get("/", response_class=HTMLResponse)
    async def console():
        return CONSOLE_HTML


# Default fleet (used by `python -m mini_loop` and `uvicorn mini_loop.server:app`).
app = create_app()


CONSOLE_HTML = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>mini-loop console</title>
<style>
 :root{
  color-scheme:dark;
  --bg:#0d1117;--panel:#161b22;--panel-2:#0f141b;--border:#30363d;
  --text:#e6edf3;--muted:#9da7b3;--blue:#58a6ff;--green:#3fb950;
  --yellow:#d29922;--red:#f85149;--purple:#bc8cff;
 }
 *{box-sizing:border-box}
 body{margin:0;background:var(--bg);color:var(--text);font:15px/1.55 ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace}
 header{padding:14px 18px;background:var(--panel);border-bottom:1px solid var(--border)}
 header strong{color:#fff} header span{color:var(--muted)}
 main{display:grid;grid-template-columns:minmax(300px,.8fr) minmax(0,1.7fr);gap:14px;max-width:1440px;margin:0 auto;padding:14px}
 .col{min-width:0;background:var(--panel);border:1px solid var(--border);border-radius:10px;padding:14px}
 .controls{align-self:start}
 label{display:block;margin-bottom:6px;color:var(--muted);font-size:13px;font-weight:600}
 textarea,select,button{font:inherit;border:1px solid var(--border);border-radius:7px}
 textarea,select{display:block;width:100%;padding:10px;background:var(--bg);color:var(--text);line-height:1.5}
 textarea{resize:vertical}
 textarea::placeholder{color:#6e7681}
 textarea:focus-visible,select:focus-visible,button:focus-visible,a:focus-visible,summary:focus-visible{outline:2px solid var(--blue);outline-offset:2px}
 button{min-height:44px;padding:8px 13px;cursor:pointer;background:#238636;border-color:#2ea043;color:#fff;font-weight:650;transition:background-color 180ms,border-color 180ms}
 button:hover{background:#2ea043} button:disabled{cursor:not-allowed;opacity:.58}
 button.sec{background:#21262d;border-color:#484f58;color:var(--text)}
 button.sec:hover{background:#30363d}
 .field+.field{margin-top:14px}.actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-top:12px}
 .session{margin-top:12px;padding:10px 11px;background:var(--panel-2);border:1px solid var(--border);border-radius:7px;color:var(--muted);overflow-wrap:anywhere}
 .session strong{color:var(--blue)}
 .trajectory-panel{margin-top:16px;padding:12px;background:var(--panel-2);border:1px solid var(--border);border-radius:8px}
 .trajectory-panel h3{margin:0 0 3px;font-size:14px}.trajectory-panel>p{margin:0 0 10px;color:var(--muted);font-size:12px}
 .trajectory-meta{min-height:38px;margin:9px 0 0!important;color:var(--muted);overflow-wrap:anywhere}
 .download{display:inline-flex;align-items:center;justify-content:center;min-height:44px;padding:8px 12px;border:1px solid #484f58;border-radius:7px;background:#21262d;color:var(--text);font-weight:650;text-decoration:none;transition:background-color 180ms,border-color 180ms}
 .download:hover{background:#30363d}.download[aria-disabled="true"]{pointer-events:none;opacity:.5}
 .panel-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin-bottom:12px}
 h2{font-size:16px;line-height:1.3;margin:0 0 3px} .panel-head p{margin:0;color:var(--muted);font-size:12px}
 .stream-tools{display:flex;align-items:center;justify-content:flex-end;gap:8px;flex-wrap:wrap}
 .stream-state,.event-count{display:inline-flex;align-items:center;gap:6px;min-height:28px;padding:3px 8px;background:var(--panel-2);border:1px solid var(--border);border-radius:999px;color:var(--muted);font-size:12px;white-space:nowrap}
 .state-dot{width:8px;height:8px;border-radius:50%;background:#6e7681}
 .stream-state[data-state="live"] .state-dot{background:var(--green)}
 .stream-state[data-state="connecting"] .state-dot{background:var(--yellow)}
 .stream-state[data-state="reconnecting"] .state-dot{background:var(--red)}
 .clear-events{min-height:34px;padding:4px 10px;font-size:12px}
 .log{height:calc(100vh - 145px);min-height:430px;overflow:auto;padding-right:4px;scrollbar-gutter:stable}
 .empty{display:grid;place-items:center;min-height:180px;padding:24px;text-align:center;color:var(--muted);border:1px dashed var(--border);border-radius:8px}
 .event-card{margin:0 0 8px;padding:10px 11px;background:var(--panel-2);border:1px solid var(--border);border-left:3px solid #6e7681;border-radius:7px;transition:border-color 180ms,background-color 180ms}
 .event-card:hover{background:#131a23;border-color:#484f58}
 .event-card[data-tone="info"]{border-left-color:var(--blue)}
 .event-card[data-tone="success"]{border-left-color:var(--green)}
 .event-card[data-tone="warning"]{border-left-color:var(--yellow)}
 .event-card[data-tone="error"]{border-left-color:var(--red)}
 .event-card[data-tone="tool"]{border-left-color:var(--purple)}
 .event-meta{display:flex;align-items:center;gap:7px;flex-wrap:wrap;color:var(--muted);font-size:11px}
 .event-type{padding:2px 7px;border:1px solid #3d4b5d;border-radius:999px;color:#c9d1d9;font-weight:700;letter-spacing:.02em}
 .event-seq{color:var(--blue)} .event-source{margin-left:auto;text-transform:uppercase;letter-spacing:.08em}
 .event-summary{margin-top:7px;color:var(--text);white-space:pre-wrap;overflow-wrap:anywhere}
 details{margin-top:7px;border-top:1px solid #262d36}
 summary{width:max-content;min-height:36px;padding:8px 2px 4px;color:var(--muted);cursor:pointer;font-size:12px}
 pre{max-height:280px;margin:3px 0 0;padding:10px;overflow:auto;background:#090d12;border:1px solid #262d36;border-radius:6px;color:#c9d1d9;font:12px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre-wrap;overflow-wrap:anywhere}
 @media(max-width:820px){main{grid-template-columns:1fr}.log{height:58vh;min-height:360px}.panel-head{align-items:stretch;flex-direction:column}.stream-tools{justify-content:flex-start}textarea,select,button{font-size:16px}}
 @media(prefers-reduced-motion:reduce){button,.event-card{transition:none}}
</style>
</head>
<body>
<header><strong>mini-loop</strong> <span>&mdash; concurrent agent console with live event telemetry</span></header>
<main>
 <section class="col controls" aria-labelledby="controls-title">
  <h2 id="controls-title">Run an agent</h2>
  <div class="field">
   <label for="sys">System prompt (optional)</label>
   <textarea id="sys" rows="3" placeholder="Override the default system prompt"></textarea>
  </div>
  <div class="actions">
   <button id="create-btn" type="button" onclick="mk()">New session</button>
  </div>
  <div class="session" id="session-info">No active session</div>
  <div class="field">
   <label for="msg">Message</label>
   <textarea id="msg" rows="5" placeholder="Message to the agent">build a hello world script and run it</textarea>
  </div>
  <div class="actions">
   <button id="run-btn" type="button" onclick="send()">Run agent</button>
  </div>
  <section class="trajectory-panel" aria-labelledby="trajectory-title">
   <h3 id="trajectory-title">Recorded trajectories</h3>
   <p>Each run is saved locally and can be inspected or exported.</p>
   <label for="trajectory-select">Agent run</label>
   <select id="trajectory-select" onchange="selectTrajectory()" disabled>
    <option>No recordings yet</option>
   </select>
   <div class="actions">
    <button class="sec" id="load-trajectory-btn" type="button" onclick="loadTrajectory()" disabled>View recording</button>
    <a class="download" id="export-json" href="#" aria-disabled="true">Export JSON</a>
    <a class="download" id="export-jsonl" href="#" aria-disabled="true">Export JSONL</a>
   </div>
   <p class="trajectory-meta" id="trajectory-meta" aria-live="polite">Create and run a session to record its first trajectory.</p>
  </section>
 </section>
 <section class="col" aria-labelledby="events-title">
  <div class="panel-head">
   <div><h2 id="events-title">Pushed events</h2><p>Persistent SSE feed with complete event metadata and payloads.</p></div>
   <div class="stream-tools">
    <span class="stream-state" id="stream-state" data-state="idle"><span class="state-dot" aria-hidden="true"></span><span id="stream-label">idle</span></span>
    <span class="event-count" id="event-count">0 events</span>
    <button class="sec clear-events" type="button" onclick="clearEvents()">Clear</button>
   </div>
  </div>
  <div class="log" id="log" role="log" aria-live="polite" aria-relevant="additions" aria-label="Agent event stream">
   <div class="empty" id="empty-state">Create a session to start receiving pushed events.</div>
  </div>
 </section>
</main>
<script>
let sid=null,eventSource=null,eventCount=0,lastSeq=0,trajectories=[];
const log=document.getElementById('log');
const eventCountLabel=document.getElementById('event-count');
const streamState=document.getElementById('stream-state');
const streamLabel=document.getElementById('stream-label');
const trajectorySelect=document.getElementById('trajectory-select');
const trajectoryMeta=document.getElementById('trajectory-meta');
const loadTrajectoryButton=document.getElementById('load-trajectory-btn');
const exportJson=document.getElementById('export-json');
const exportJsonl=document.getElementById('export-jsonl');

function short(value,limit=900){
 const rendered=typeof value==='string'?value:JSON.stringify(value);
 const text=rendered===undefined?String(value):rendered;
 return text.length>limit?text.slice(0,limit)+'…':text;
}
function eventTone(type,payload){
 if(type.endsWith('error')||payload.error===true||payload.decision==='deny'||payload.action==='failed'||payload.status==='error')return 'error';
 if(type==='done'||(type==='trajectory_end'&&payload.status==='completed'))return 'success';
 if(type==='tool_use'||type==='tool_result')return 'tool';
 if(['compact','recovery','permission','todo'].includes(type))return 'warning';
 if(['assistant_text','memory','background_result','team_inbox'].includes(type))return 'info';
 return 'neutral';
}
function eventSummary(type,o){
 if(type==='assistant_text'||type==='done')return o.text||'(empty text)';
 if(type==='tool_use')return (o.name||'unknown tool')+' '+short(o.input||{});
 if(type==='tool_result')return (o.name?o.name+': ':'')+short(o.output||'(empty result)');
 if(type==='trajectory_start')return 'Recording run #'+(o.run_index||'?')+' · '+(o.trajectory_id||'trajectory');
 if(type==='trajectory_end')return 'Trajectory '+(o.status||'finished')+' · '+formatDuration(o.duration_ms);
 if(type==='trajectory_recording')return 'Run #'+(o.run_index||'?')+' '+(o.status||'recorded')+' · '+formatDuration(o.duration_ms)+' · '+short(o.input||'(no input)',240);
 if(type==='model_start')return (o.purpose||'model call')+' · '+(o.model||'unknown model')+' · ~'+(o.input_tokens_estimate||0)+' input tokens';
 if(type==='model_end')return (o.purpose||'model call')+' '+(o.status||'finished')+' · '+formatDuration(o.duration_ms)+(o.stop_reason?' · '+o.stop_reason:'');
 if(type==='error'||type==='client_error')return o.error||'Unknown error';
 if(type==='status')return 'Session status: '+(o.status||'unknown')+(o.cancelled?' (cancelled)':'');
 if(type==='subagent_start')return 'Started '+(o.agent_type||'subagent')+': '+short(o.prompt||'');
 if(type==='subagent_end')return 'Subagent completed: '+short(o.summary||'');
 if(type==='todo')return (o.items||[]).map(item=>item.status+': '+item.content).join(' | ')||'Todo list updated';
 if(type==='compact')return 'Context compaction: '+(o.kind||'unknown');
 if(type==='permission')return (o.decision||'decision')+' via '+(o.rule||'rule')+(o.reason?': '+o.reason:'');
 if(type==='recovery')return 'Recovery action: '+(o.action||'unknown')+(o.error?' ('+o.error+')':'');
 if(type==='memory')return 'Memory '+(o.action||'event')+(o.count!==undefined?': '+o.count+' item(s)':'');
 if(type==='background_result')return (o.count||0)+' background result(s) ready';
 if(type==='team_inbox')return (o.count||0)+' team message(s) received';
 if(type==='user_prompt')return o.text||'(empty message)';
 const detail=Object.entries(o).filter(([key])=>!['seq','ts','session','type','agent','depth'].includes(key));
 return detail.length?short(Object.fromEntries(detail)):'Event received';
}
function formatTime(ts){
 if(!ts)return '--:--:--';
 return new Date(ts*1000).toLocaleTimeString([], {hour12:false,hour:'2-digit',minute:'2-digit',second:'2-digit'});
}
function formatDuration(ms){
 if(ms===null||ms===undefined)return 'duration unavailable';
 return ms<1000?Math.round(ms)+' ms':(ms/1000).toFixed(2)+' s';
}
function addMeta(row,text,className){
 const item=document.createElement('span');item.className=className||'';item.textContent=text;row.appendChild(item);
}
function renderEvent(eventName,payload,source='push'){
 const type=payload.type||eventName||'message';
 if(source==='SSE'&&Number.isFinite(payload.seq)){
  if(payload.seq<=lastSeq)return;
  lastSeq=payload.seq;
 }
 document.getElementById('empty-state')?.remove();
 const card=document.createElement('article');card.className='event-card';card.dataset.tone=eventTone(type,payload);
 const meta=document.createElement('div');meta.className='event-meta';
 addMeta(meta,payload.seq?'#'+payload.seq:'#local','event-seq');
 addMeta(meta,formatTime(payload.ts),'event-time');
 addMeta(meta,type,'event-type');
 if(payload.agent)addMeta(meta,payload.agent+(payload.depth!==undefined?' · depth '+payload.depth:''),'event-agent');
 addMeta(meta,source,'event-source');
 const summary=document.createElement('div');summary.className='event-summary';summary.textContent=eventSummary(type,payload);
 const details=document.createElement('details');
 const disclosure=document.createElement('summary');disclosure.textContent='View event payload';
 const pre=document.createElement('pre');pre.textContent='Open to render the complete payload.';
 details.addEventListener('toggle',()=>{
  if(details.open&&pre.dataset.loaded!=='true'){
   pre.textContent=JSON.stringify(payload,null,2);pre.dataset.loaded='true';
  }
 });
 details.append(disclosure,pre);card.append(meta,summary,details);log.appendChild(card);
 eventCount+=1;eventCountLabel.textContent=eventCount+(eventCount===1?' event':' events');
 log.scrollTop=log.scrollHeight;
}
function setStreamState(state,label){streamState.dataset.state=state;streamLabel.textContent=label;}
function clearEvents(){
 log.replaceChildren();
 const empty=document.createElement('div');empty.className='empty';empty.id='empty-state';empty.textContent='Waiting for the next pushed event.';log.appendChild(empty);
 eventCount=0;eventCountLabel.textContent='0 events';
}
function connectEvents(){
 if(eventSource)eventSource.close();
 setStreamState('connecting','connecting');
 eventSource=new EventSource('/sessions/'+encodeURIComponent(sid)+'/events?envelope=true');
 eventSource.addEventListener('agent_event',event=>{
  try{const payload=JSON.parse(event.data);renderEvent(payload.type,payload,'SSE');if(payload.type==='done'||payload.type==='error')refreshTrajectories();}
  catch(error){renderEvent('client_error',{type:'client_error',error:'Invalid event payload: '+error.message,ts:Date.now()/1000},'client');}
 });
 eventSource.onopen=()=>setStreamState('live','live');
 eventSource.onerror=()=>setStreamState('reconnecting','reconnecting');
}
async function responseJson(response){
 let body={};try{body=await response.json();}catch(error){body={detail:'Invalid JSON response'};}
 if(!response.ok)throw new Error(body.detail||('HTTP '+response.status));
 return body;
}
function selectedTrajectory(){
 return trajectories.find(item=>item.id===trajectorySelect.value)||null;
}
function selectTrajectory(){
 const trajectory=selectedTrajectory();
 loadTrajectoryButton.disabled=!trajectory;
 for(const [link,format] of [[exportJson,'json'],[exportJsonl,'jsonl']]){
  link.setAttribute('aria-disabled',trajectory?'false':'true');
  link.href=trajectory?'/trajectories/'+encodeURIComponent(trajectory.id)+'/export?format='+format:'#';
 }
 if(!trajectory)return;
 const metrics=trajectory.metrics||{};
 trajectoryMeta.textContent=(trajectory.status||'unknown')+' · '+formatDuration(trajectory.duration_ms)+' · '+(metrics.event_count||0)+' events · '+(metrics.tool_calls||0)+' tools';
}
async function refreshTrajectories(){
 if(!sid)return;
 try{
  const response=await fetch('/sessions/'+encodeURIComponent(sid)+'/trajectories');
  trajectories=await responseJson(response);
  const previous=trajectorySelect.value;
  trajectorySelect.replaceChildren();
  if(!trajectories.length){
   const option=document.createElement('option');option.textContent='No recordings yet';trajectorySelect.appendChild(option);
   trajectorySelect.disabled=true;trajectoryMeta.textContent='The next completed agent run will appear here.';
  }else{
   for(const trajectory of trajectories){
    const option=document.createElement('option');option.value=trajectory.id;
    option.textContent='#'+trajectory.run_index+' · '+trajectory.status+' · '+formatDuration(trajectory.duration_ms);
    trajectorySelect.appendChild(option);
   }
   trajectorySelect.disabled=false;
   if(trajectories.some(item=>item.id===previous))trajectorySelect.value=previous;
  }
  selectTrajectory();
 }catch(error){trajectoryMeta.textContent='Could not load trajectories: '+error.message;}
}
async function loadTrajectory(){
 const selected=selectedTrajectory();if(!selected)return;
 loadTrajectoryButton.disabled=true;loadTrajectoryButton.textContent='Loading…';
 try{
  const response=await fetch('/trajectories/'+encodeURIComponent(selected.id));
  const trajectory=await responseJson(response);clearEvents();
  const {events,...overview}=trajectory;
  renderEvent('trajectory_recording',{type:'trajectory_recording',...overview},'recording');
  for(const event of events)renderEvent(event.type,event,'recording');
  trajectoryMeta.textContent='Viewing '+trajectory.id+' · '+trajectory.status+' · '+formatDuration(trajectory.duration_ms);
 }catch(error){renderEvent('client_error',{type:'client_error',error:error.message,ts:Date.now()/1000},'client');}
 finally{loadTrajectoryButton.disabled=false;loadTrajectoryButton.textContent='View recording';}
}
async function mk(){
 const button=document.getElementById('create-btn');button.disabled=true;button.textContent='Creating…';
 try{
  const system=document.getElementById('sys').value||null;
  const response=await fetch('/sessions',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({system})});
  const session=await responseJson(response);sid=session.id;lastSeq=0;clearEvents();
  const sessionInfo=document.getElementById('session-info');
  const sessionId=document.createElement('strong');sessionId.textContent=sid;
  sessionInfo.replaceChildren(document.createTextNode('Active session: '),sessionId);
  connectEvents();
  await refreshTrajectories();
 }catch(error){renderEvent('client_error',{type:'client_error',error:error.message,ts:Date.now()/1000},'client');setStreamState('idle','idle');}
 finally{button.disabled=false;button.textContent='New session';}
}
async function send(){
 if(!sid)await mk();if(!sid)return;
 const message=document.getElementById('msg').value;
 const button=document.getElementById('run-btn');button.disabled=true;button.textContent='Running…';
 renderEvent('user_prompt',{type:'user_prompt',text:message,session:sid,ts:Date.now()/1000},'client');
 try{
  const response=await fetch('/sessions/'+encodeURIComponent(sid)+'/messages',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({message})});
  await responseJson(response);
 }catch(error){renderEvent('client_error',{type:'client_error',error:error.message,session:sid,ts:Date.now()/1000},'client');}
 finally{await refreshTrajectories();button.disabled=false;button.textContent='Run agent';}
}
window.addEventListener('beforeunload',()=>eventSource?.close());
</script>
</body>
</html>
"""
