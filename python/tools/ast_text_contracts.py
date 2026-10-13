"""Actual AST tool text versus UTF-8/Anthropic request encoding boundaries."""
import asyncio
import json
import sys
import tempfile
from pathlib import Path
import traceback


def ast_text_contracts() -> dict:
    from mini_loop.ast_context import AstContextConfig, AstOutlineAdapter, install_ast_context_tools
    from mini_loop.registry import ToolContext, ToolRegistry
    from mini_loop.agent import Agent
    from mini_loop.config import Settings
    from mini_loop.caching import NullCachePolicy
    from mini_loop.fake_llm import FakeAsyncAnthropic, text as fake_text, tool as fake_tool
    from anthropic import AsyncAnthropic
    import httpx

    fields = [
        ("high-surrogate", {"text": "\ud800"}),
        ("low-surrogate", {"text": "\udfff"}),
        ("surrogate-key", {"\ud800": "value"}),
        ("warning-surrogate", {"notes": ["\ud800"]}),
        ("error-hint-surrogate", {"error": {"hint": "\udfff"}}),
        ("paired-emoji", {"text": "😀"}),
        ("unicode-controls", {"text": "界<>&\u2028\n\x00\t\"\\"}),
        ("nonfinite", {"value": float("nan"), "nested": [float("inf"), -float("inf")]}),
    ]

    async def run() -> dict:
        rows = []
        with tempfile.TemporaryDirectory(prefix="mini-loop-ast-text-") as temp:
            root = Path(temp).resolve()
            workspace = root / "workspace"
            workspace.mkdir()
            (workspace / "code.py").write_text("class One: pass\n")
            output, calls, binary = root / "stdout", root / "calls", root / "ast-cli"
            binary.write_text(
                f"#!{sys.executable}\nimport sys,json,os\nfrom pathlib import Path\n"
                f"with Path({str(calls)!r}).open('a') as stream: stream.write(json.dumps(dict(args=sys.argv[1:],cwd=os.getcwd()))+'\\n')\n"
                "if sys.argv[1:]==['--version']:\n"
                " sys.stdout.write('ast-outline 1.9.0\\nhomepage: https://github.com/ast-outline/ast-outline\\n')\n"
                "else:\n"
                f" sys.stdout.write(Path({str(output)!r}).read_text())\n"
            )
            binary.chmod(0o700)
            config = AstContextConfig(binary=binary)
            adapter = AstOutlineAdapter(config)
            registry = install_ast_context_tools(ToolRegistry(), config)
            for name, extra in fields:
                stdout = json.dumps(dict(tool="ast-outline", schema_version=1, command="outline", files=[{}], **extra), ensure_ascii=True, separators=(",", ":"))
                output.write_text(stdout)
                result = adapter.file_outline(workspace, ["code.py"])
                text = result.to_json()
                calls.unlink()
                tool_text = await registry.get("file_outline").run(ToolContext(None, workspace, {}), paths=["code.py"])
                assert text == tool_text
                row = dict(name=name, stdout=stdout, result_json=json.dumps(result.as_dict(), ensure_ascii=True, separators=(",", ":")),
                    text_json=json.dumps(text, ensure_ascii=True), text_hex=text.encode("utf-8", "surrogatepass").hex())
                try:
                    text.encode("utf-8")
                    row["utf8"] = True
                except UnicodeEncodeError:
                    row["utf8"] = False
                observed = [json.loads(line) for line in calls.read_text().splitlines()]
                for call in observed:
                    assert Path(call["cwd"]) == workspace
                    call["cwd"] = "<workspace>"
                    for i, arg in enumerate(call["args"]):
                        if "/mini-loop-ast-" in arg:
                            assert not Path(arg).exists()
                            call["args"][i] = "<snapshot>" + arg.split("/workspace", 1)[1]
                row["calls"] = observed
                requests = []
                async def respond(request):
                    requests.append(request)
                    return httpx.Response(200, json=dict(id="message", type="message", role="assistant", model="configured", content=[dict(type="text", text="done")], stop_reason="end_turn", usage=dict(input_tokens=1, output_tokens=1)))
                async with AsyncAnthropic(api_key="fixture-key", max_retries=0, http_client=httpx.AsyncClient(transport=httpx.MockTransport(respond))) as client:
                    try:
                        await client.messages.create(model="configured", max_tokens=10, messages=[dict(role="user", content="inspect"), dict(role="assistant", content=[dict(type="tool_use", id="ast", name="bash", input=dict(command="true"))]), dict(role="user", content=[dict(type="tool_result", tool_use_id="ast", content=tool_text)])])
                        row["provider_error"] = ""
                    except Exception as error:
                        row["provider_error"] = type(error).__name__
                row["provider_calls"] = len(requests)
                # Actual default compaction runs after the tool result and
                # before the next model request; direct SDK evidence is separate.
                agent_calls, agent_events = [], []
                def agent_respond(kwargs):
                    agent_calls.append(kwargs)
                    if len(agent_calls) == 1:
                        return [fake_tool("file_outline", _id="ast", paths=["code.py"])], "tool_use"
                    return [fake_text("done")], "end_turn"
                async def agent_emit(event):
                    agent_events.append(event)
                agent = Agent(client=FakeAsyncAnthropic(responder=agent_respond, thinking=False),
                              workspace=workspace, tools=registry, emit=agent_emit,
                              cache_policy=NullCachePolicy(), state=dict(permission_mode="readonly"),
                              settings=Settings(fake_llm=True, spill_dir=None, skills_dir=root/"empty"))
                agent_error, agent_output, error_frames = "", "", []
                try:
                    agent_output = await agent.run("inspect")
                except Exception as error:
                    agent_error = type(error).__name__
                    error_frames = [dict(file=Path(frame.filename).name,function=frame.name,line=frame.lineno) for frame in traceback.extract_tb(error.__traceback__) if "mini_loop" in frame.filename]
                results = [part for message in agent.messages if isinstance(message.get("content"), list)
                           for part in message["content"] if part.get("type") == "tool_result" and part.get("tool_use_id") == "ast"]
                assert all(result["content"] == tool_text for result in results)
                assert len(results) == (1 if row["utf8"] else 0), (name, agent_error, results, error_frames)
                row["agent"] = dict(error=agent_error, output=agent_output, provider_calls=len(agent_calls),
                                    model_starts=sum(e["type"] == "model_start" for e in agent_events),
                                    result_text_hex=results[0]["content"].encode("utf-8", "surrogatepass").hex() if results else None,
                                    error_frames=error_frames,
                                    events=[e["type"] for e in agent_events],
                                    budget_directory=(workspace/".task_outputs/tool-results").exists(),
                                    transcript_messages=len(agent.messages))
                rows.append(row)
        return dict(invocations=rows)
    return asyncio.run(run())
