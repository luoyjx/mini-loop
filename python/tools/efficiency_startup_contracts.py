"""Actual server lifespan + HTTP turns over settings-selected efficiency builtins."""
from pathlib import Path

COMMAND = "for i in $(seq 1 300); do printf '\\033[31mNOTICE duplicate\\033[0m\\n'; done"


def efficiency_startup_contracts(scratch: Path | str) -> dict:
    from fastapi.testclient import TestClient
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool, system_text
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app

    cases = [(f"{mode}-{style}", mode, style, "auto", True, 1)
             for mode in ("off", "shadow", "enforce") for style in ("normal", "concise")]
    cases += [("readonly", "enforce", "concise", "readonly", True, 1),
              ("no-persistence", "enforce", "concise", "auto", False, 1),
              ("below-threshold", "enforce", "normal", "auto", True, 20000)]
    rows = []
    for name, mode, style, permission, persist, minimum in cases:
        requests, receipts = [], []
        manager = None
        session = None
        raw_ref = None
        rounds = 0

        def responder(request):
            nonlocal rounds
            rounds += 1
            results = []
            for message in request["messages"]:
                if isinstance(message.get("content"), list):
                    results.extend(block for block in message["content"]
                                   if block.get("type") == "tool_result")
            if results:
                result = results[-1]
                content = result["content"]
                requests.append(dict(max_tokens=request["max_tokens"],
                    recovery_tool=any(t["name"] == "read_token_artifact" for t in request["tools"]),
                    concise_instruction="Answer concisely." in system_text(request),
                    result=dict(id=result["tool_use_id"], bytes=len(content.encode("utf-8")),
                        ansi="\x1b[" in content, marker="read_token_artifact" in content,
                        full_raw=content.count("NOTICE duplicate") == 300)))
            else:
                requests.append(dict(max_tokens=request["max_tokens"],
                    recovery_tool=any(t["name"] == "read_token_artifact" for t in request["tools"]),
                    concise_instruction="Answer concisely." in system_text(request), result=None))
            if rounds == 1:
                return [tool("bash", _id="command", command=COMMAND)], "tool_use"
            if rounds == 2 and raw_ref:
                return [tool("read_token_artifact", _id="recover", raw_ref=raw_ref)], "tool_use"
            return [text("done")], "end_turn"

        def factory(cfg):
            nonlocal manager
            manager = SessionManager(cfg, FakeAsyncAnthropic(responder=responder, thinking=False))
            return manager

        settings = Settings(fake_llm=True, workspace_root=Path(scratch)/name,
            skills_dir=Path(scratch)/"empty-skills", trajectory_enabled=False,
            max_tokens=4000, max_turns=4, token_efficiency_mode=mode,
            token_efficiency_response_style=style, token_efficiency_persist_raw=persist,
            token_efficiency_raw_min_bytes=minimum, token_efficiency_artifact_ttl_seconds=7,
            token_efficiency_max_artifact_bytes=20000, token_efficiency_max_total_bytes=40000)
        with TestClient(create_app(settings=settings, manager_factory=factory)) as http:
            response = http.post("/sessions", json={"mode":permission})
            assert response.status_code == 200, response.text
            session = manager.get(response.json()["id"])
            original_emit = session.agent.emit
            async def emit(event):
                nonlocal raw_ref
                if event["type"] == "optimization_receipt":
                    receipt = {key:value for key,value in event.items() if key not in
                        {"type","agent","depth","message_id","parent_message_id"}}
                    receipt["elapsed_ms"] = 0
                    if receipt.get("parent_span_id"):
                        receipt["parent_span_id"] = "<tool>"
                    if receipt.get("raw_ref"):
                        raw_ref = receipt["raw_ref"]
                    receipts.append(receipt)
                if original_emit:
                    await original_emit(event)
            session.agent.emit = emit
            response = http.post(f"/sessions/{session.id}/messages", json={"message":"inspect command output"})
            assert response.status_code == 200, response.text
            store = session.agent.token_efficiency.raw_store
            limits = [store.ttl_seconds,store.max_artifact_bytes,store.max_total_bytes] if store else None
            for receipt in receipts:
                if receipt.get("raw_ref"):
                    receipt["raw_ref"] = "<raw>"
            row = dict(name=name,mode=mode,style=style,permission=permission,persist=persist,
                minimum=minimum,command=COMMAND,output=response.json()["final"],
                store_present=store is not None,limits=limits,requests=requests,receipts=receipts)
        row["store_closed"] = store is None or store.closed
        rows.append(row)
    return dict(rows=rows)
