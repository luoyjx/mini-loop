"""Actual AST tool text versus UTF-8/Anthropic request encoding boundaries."""
import asyncio
import json
import sys
import tempfile
from pathlib import Path


def ast_text_contracts() -> dict:
    from mini_loop.ast_context import AstContextConfig, AstOutlineAdapter, install_ast_context_tools
    from mini_loop.registry import ToolContext, ToolRegistry
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
                rows.append(row)
        return dict(invocations=rows)
    return asyncio.run(run())
