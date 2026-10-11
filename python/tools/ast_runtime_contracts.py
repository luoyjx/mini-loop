"""Run real Source HTTP/session AST tools against a pinned isolated CLI child."""
import hashlib
import json
from pathlib import Path
import sys
import tempfile


def ast_runtime_contracts() -> dict:
    from fastapi.testclient import TestClient
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app

    cases = [
        dict(name="disabled", enabled=False, operation="file_outline", input=dict(paths=["code.py"])),
        dict(name="outline", enabled=True, operation="file_outline", input=dict(paths=["code.py"], view="minimal", include_imports=True)),
        dict(name="map", enabled=True, operation="repo_map", input=dict(paths=["code.py"], density="wide")),
        dict(name="show", enabled=True, operation="show_symbol", input=dict(path_or_glob="cod*.py", symbols=["-Option"], signature_only=True)),
        dict(name="references", enabled=True, operation="symbol_references", input=dict(patterns=["-Option"], paths=["code.py"], kinds=["def", "call"], max_per_file=7)),
        dict(name="escape", enabled=True, operation="file_outline", input=dict(paths=["../outside.py"])),
        dict(name="wrong-digest", enabled=True, operation="file_outline", input=dict(paths=["code.py"]), mode="wrong-digest"),
        dict(name="missing-binary", enabled=True, operation="file_outline", input=dict(paths=["code.py"]), mode="missing"),
        dict(name="incompatible-version", enabled=True, operation="file_outline", input=dict(paths=["code.py"]), mode="version"),
        dict(name="invalid-json", enabled=True, operation="file_outline", input=dict(paths=["code.py"]), mode="invalid"),
        dict(name="non-object", enabled=True, operation="file_outline", input=dict(paths=["code.py"]), mode="array"),
        dict(name="nonzero", enabled=True, operation="file_outline", input=dict(paths=["code.py"]), mode="nonzero"),
        dict(name="output-cap", enabled=True, operation="file_outline", input=dict(paths=["code.py"]), mode="overflow", output_cap=120),
        dict(name="timeout", enabled=True, operation="file_outline", input=dict(paths=["code.py"]), mode="timeout", timeout=1.0),
    ]
    rows = []
    for case in cases:
        with tempfile.TemporaryDirectory(prefix="mini-loop-ast-runtime-") as temp:
            root = Path(temp).resolve()
            calls, binary = root / "calls", root / "ast-cli"
            mode = case.get("mode", "normal")
            binary.write_text(
                f"#!{sys.executable}\nimport json,sys,os,time\nfrom pathlib import Path\n"
                f"calls=Path({str(calls)!r})\nmode={mode!r}\n"
                "with calls.open('a') as stream: stream.write(json.dumps({'args':sys.argv[1:],'cwd':os.getcwd()})+'\\n')\n"
                "if sys.argv[1:]==['--version']:\n"
                " version='1.10.0' if mode=='version' else '1.9.0'\n"
                " sys.stdout.write('ast-outline '+version+'\\nhomepage: https://github.com/ast-outline/ast-outline\\n')\n"
                "else:\n"
                " if mode=='timeout': time.sleep(3)\n"
                " if mode=='invalid': sys.stdout.write('{x:1}');sys.exit(0)\n"
                " if mode=='array': sys.stdout.write('[]');sys.exit(0)\n"
                " if mode=='overflow': sys.stdout.write('x'*1000);sys.exit(0)\n"
                " if mode=='nonzero': sys.stdout.write('output');sys.stderr.write('  cli failure  ');sys.exit(7)\n"
                " sys.stdout.write(json.dumps({'tool':'ast-outline','schema_version':1,'command':sys.argv[1],'files':[],'results':[]},separators=(',',':')))\n"
            )
            binary.chmod(0o700)
            digest = hashlib.sha256(binary.read_bytes()).hexdigest()
            settings = Settings(fake_llm=True, workspace_root=root / "sessions",
                skills_dir=root / "skills", trajectory_enabled=False, model="configured",
                ast_outline_enabled=case["enabled"], ast_outline_binary=str(binary),
                ast_outline_sha256="0" * 64 if mode == "wrong-digest" else digest,
                ast_outline_timeout=case.get("timeout", 10.0), ast_outline_max_output_bytes=case.get("output_cap", 1000000))
            if mode == "missing":
                binary.unlink()
            observed = dict(names=[], results=[])
            def respond(request):
                observed["names"].append([entry["name"] for entry in request["tools"]])
                results = [block["content"] for message in request["messages"]
                    if isinstance(message.get("content"), list) for block in message["content"]
                    if isinstance(block, dict) and block.get("type") == "tool_result" and block.get("tool_use_id") == "ast"]
                if results:
                    observed["results"].extend(results)
                    return [text("done")], "end_turn"
                if case["enabled"]:
                    return [tool(case["operation"], _id="ast", **case["input"])], "tool_use"
                return [text("done")], "end_turn"
            def factory(cfg):
                return SessionManager(cfg, FakeAsyncAnthropic(responder=respond, thinking=False), enable_features=False)
            with TestClient(create_app(settings=settings, manager_factory=factory)) as http:
                created = http.post("/sessions", json=dict(mode="readonly"))
                assert created.status_code == 200, created.text
                workspace = Path(created.json()["workspace"])
                (workspace / "code.py").write_text("class One: pass\n")
                response = http.post(f"/sessions/{created.json()['id']}/messages", json=dict(message="inspect source"))
                assert response.status_code == 200, response.text
                final = response.json()["final"]
            invoked = [json.loads(line) for line in calls.read_text().splitlines()] if calls.exists() else []
            for call in invoked:
                assert Path(call["cwd"]) == workspace
                call["cwd"] = "<workspace>"
                for i, arg in enumerate(call["args"]):
                    if "/mini-loop-ast-" in arg:
                        assert not Path(arg).exists()
                        suffix = arg.split("/workspace", 1)[1]
                        call["args"][i] = "<snapshot>" + suffix
            observed["results"] = [value.replace(str(workspace), "<workspace>").replace(str(binary), "<binary>") for value in observed["results"]]
            rows.append(dict(case=case, output=final, calls=invoked, **observed))
    return dict(http=rows)
