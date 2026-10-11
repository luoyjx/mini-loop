"""Actual AST invocation/public-operation decimal conversion boundaries."""
import json
import sys
import tempfile
from pathlib import Path


def ast_integer_contracts() -> dict:
    from mini_loop import ast_context as source

    def envelope(fields: str) -> str:
        return '{"tool":"ast-outline","schema_version":1,"command":"outline","files":[{}],' + fields + '}'

    maximum = "9" * 4300
    cases = [
        ("parse-limit", "1" * 4301),
        ("parse-negative", "-" + "1" * 4302),
        ("parse-nested-unicode", '{"α":[' + "1" * 4303 + ']}'),
        ("parse-before-trailing-fault", "1" * 4301 + "!"),
        ("syntax-before-integer", '{x:' + "1" * 4301 + '}'),
        ("maximum-metric", envelope('"error_count":' + maximum)),
        ("parse-error-sum", envelope('"error_count":' + maximum + ',"parse_errors":1')),
        ("truncation-sum", envelope('"truncated_count":' + maximum + ',"nested":{"truncated_count":true}')),
        ("sum-before-ambiguity", envelope('"error_count":' + maximum + ',"parse_errors":1,"results":null')),
        ("error-envelope-before-sum", envelope('"error":{},"error_count":' + maximum + ',"parse_errors":1')),
        ("ignored-negative", envelope('"error_count":-' + maximum)),
        ("float-not-integer", "1" * 4301 + ".0"),
    ]
    rows = []
    with tempfile.TemporaryDirectory(prefix="mini-loop-ast-integer-") as temp:
        root = Path(temp).resolve()
        workspace = root / "workspace"
        workspace.mkdir()
        (workspace / "code.py").write_text("class One: pass\n")
        body, calls, binary = root / "stdout", root / "calls", root / "ast-cli"
        binary.write_text(
            f"#!{sys.executable}\nimport json,sys,os\nfrom pathlib import Path\n"
            f"with Path({str(calls)!r}).open('a') as stream: stream.write(json.dumps(dict(args=sys.argv[1:],cwd=os.getcwd()))+'\\n')\n"
            "if sys.argv[1:]==['--version']:\n"
            f" sys.stdout.write('ast-outline 1.9.0\\nhomepage: {source.AST_OUTLINE_HOMEPAGE}\\n')\n"
            "else:\n"
            f" sys.stdout.write(Path({str(body)!r}).read_text())\n"
        )
        binary.chmod(0o700)
        adapter = source.AstOutlineAdapter(source.AstContextConfig(binary=binary))
        for name, stdout in cases:
            body.write_text(stdout)
            row = dict(name=name, stdout=stdout)
            for profile in ("invoke", "public"):
                calls.unlink(missing_ok=True)
                try:
                    if profile == "invoke":
                        result = adapter._invoke("file_outline", "outline", ["outline", "--json"], workspace)
                    else:
                        result = adapter.file_outline(workspace, ["code.py"])
                    row[profile] = dict(result_json=json.dumps(result.as_dict(), ensure_ascii=True, separators=(",", ":")))
                except Exception as error:
                    row[profile] = dict(error_class=type(error).__name__, error=str(error))
                observed = [json.loads(line) for line in calls.read_text().splitlines()]
                for call in observed:
                    assert Path(call["cwd"]) == workspace
                    call["cwd"] = "<workspace>"
                    for i, arg in enumerate(call["args"]):
                        if "/mini-loop-ast-" in arg:
                            assert not Path(arg).exists(), "snapshot survived conversion failure"
                            call["args"][i] = "<snapshot>" + arg.split("/workspace", 1)[1]
                row[profile]["calls"] = observed
            rows.append(row)
    return dict(invocations=rows)
