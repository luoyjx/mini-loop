"""Run the actual AST invocation classifier over a real isolated CLI child."""

import json
from pathlib import Path
import sys
import tempfile

import mini_loop.ast_context as source


def ast_payload_contracts() -> dict:
    def envelope(command="outline", **fields):
        return {"tool": "ast-outline", "schema_version": 1, "command": command, **fields}

    cases = []

    def payload(name, value, command="outline", imports=None):
        cases.append({"name": name, "command": command, "stdout": json.dumps(value, ensure_ascii=True, separators=(",", ":")),
                      "include_imports": imports})

    payload("outline-applied", envelope(files=[{"path": "/private/frozen/workspace/code.py", "nodes": [{"name": "One"}]}]))
    payload("outline-empty", envelope(files=[]))
    payload("outline-missing-files", envelope())
    payload("digest-empty", envelope("digest", files=[]), command="digest")
    payload("outline-object-files", envelope(files={"foreign": 1}))
    payload("outline-null-files", envelope(files=None))
    payload("outline-null-files-import-filter", envelope(files=None), imports=False)
    payload("schema-boolean-one", {**envelope(files=[]), "schema_version": True})
    payload("schema-float-one", {**envelope(files=[]), "schema_version": 1.0})
    payload("schema-text-one", {**envelope(files=[]), "schema_version": "1"})
    payload("wrong-tool", {**envelope(files=[]), "tool": "other"})
    payload("wrong-command", envelope("digest", files=[]))
    payload("error-null", envelope(error=None, files=[{"imports": ["kept"]}]), imports=False)
    payload("error-scalar", envelope(error="detail"))
    payload("error-notes-hint", envelope(error={"notes": ["first", 42, False, {"item": [1, None]}], "hint": ["choose", 2]}))
    payload("error-notes-string", envelope(error={"notes": "αβ", "hint": True}))
    payload("error-notes-object", envelope(error={"notes": {"first": 1, "second": 2}, "hint": ""}))
    payload("error-empty-notes-hint", envelope(error={"notes": [], "hint": "use explicit path"}))
    payload("error-null-notes", envelope(error={"notes": None}))
    payload("error-quiet-hint", envelope(error={"notes": [], "hint": 0}))
    files = [{"imports": ["one"], "conditional_imports_count": 3, "import_regions": ["one"], "other": 1}, 42, None]
    payload("imports-removed", envelope(files=files), imports=False)
    payload("imports-retained", envelope(files=files), imports=True)
    payload("imports-unspecified", envelope(files=files))
    payload("imports-under-error-retained", envelope(error={}, files=files), imports=False)
    payload("rewrite-values-only", envelope(files=[{"path": "/private/frozen/workspace/code.py",
            "nested": ["/private/frozen/workspace/a /private/frozen/workspace/b", {"/private/frozen/workspace/key": "/private/frozen/workspace/value"}]}]))
    payload("warning-array", envelope(files=[{}], notes=["first", 2, None, "α"]))
    payload("warning-string", envelope(files=[{}], notes="αβ"))
    payload("warning-object", envelope(files=[{}], notes={"first": 1, "second": 2}))
    payload("warning-null", envelope(files=[{}], notes=None))
    payload("nested-metrics", envelope(files=[{"error_count": True, "parse_errors": [None, {}, False],
            "truncated_count": {"one": 1, "two": 2}, "child": {"error_count": 7}}], error_count=-2, parse_errors=2.0,
            other=[{"parse_errors": 3}, {"truncated_count": True}], notes=["first"]))
    payload("metric-container-does-not-recurse", envelope(files=[{}], error_count={"parse_errors": [1, 2, 3]}))
    payload("unbounded-metric", envelope(files=[{}], error_count=10**100))
    payload("ignored-float-metrics", envelope(files=[{}], error_count=float("inf"), parse_errors=float("nan")))
    for name, results in (("empty", []), ("one", [{"matches": [1]}]), ("two", [{"matches": [1, 2]}]),
                          ("true-ambiguous", [{"ambiguous": True, "matches": []}]),
                          ("integer-ambiguous", [{"ambiguous": 1, "matches": []}]),
                          ("mixed", [42, {"matches": []}]), ("object", {"name": 1}), ("null", None)):
        payload("show-" + name, envelope("show", results=results), command="show")
    payload("show-partial-precedes-invalid-results", envelope("show", results=None, error_count=1), command="show")
    for name, fields in (("missing", {}), ("empty", {"files": []}), ("all-empty", {"files": [{"matches": []}]}),
                         ("one", {"files": [{"matches": [1]}]}), ("non-object-files", {"files": [42]}),
                         ("summary-zero", {"summary": {"total_matches": 0}, "files": [{"matches": [1]}]}),
                         ("summary-false", {"summary": {"total_matches": False}}),
                         ("summary-float-zero", {"summary": {"total_matches": 0.0}}),
                         ("summary-text-zero", {"summary": {"total_matches": "0"}})):
        payload("grep-" + name, envelope("grep", **fields), command="grep")
    for name, text in (("array", "[]"), ("null", "null"), ("number", "1"), ("string", '"text"'),
                       ("empty-json", ""), ("invalid-json", "not JSON"), ("invalid-key", "{x:1}"),
                       ("trailing-comma", '{"a":1,}'), ("trailing-data", "{} {}"),
                       ("invalid-escape", '"bad\\q"'), ("unterminated", '"bad'), ("unicode-invalid", '{"α":!}')):
        cases.append({"name": name, "command": "outline", "stdout": text, "include_imports": None})
    cases.append({"name": "nonzero-stderr-precedence", "command": "outline", "stdout": "output", "stderr": "  failure /private/frozen/workspace/code.py  ", "exit_code": 7})
    cases.append({"name": "nonzero-stdout-fallback", "command": "outline", "stdout": "  failure α /private/frozen/workspace/code.py  ", "stderr": "  ", "exit_code": 8})

    rows = []
    with tempfile.TemporaryDirectory(prefix="mini-loop-ast-payload-contract-") as temporary:
        scratch = Path(temporary).resolve()
        workspace = scratch / "workspace"
        workspace.mkdir()
        case_path, binary, calls = scratch / "case.json", scratch / "ast-cli", scratch / "calls"
        binary.write_text(
            f"#!{sys.executable}\nimport json,sys\nfrom pathlib import Path\n"
            f"calls=Path({str(calls)!r})\n"
            "with calls.open('a') as stream: stream.write(json.dumps(sys.argv[1:])+'\\n')\n"
            "if sys.argv[1:] == ['--version']:\n"
            f" sys.stdout.write('ast-outline 1.9.0\\nhomepage: {source.AST_OUTLINE_HOMEPAGE}\\n')\n"
            "else:\n"
            f" case=json.loads(Path({str(case_path)!r}).read_text())\n"
            " sys.stdout.write(case['stdout'])\n sys.stderr.write(case.get('stderr',''))\n sys.exit(case.get('exit_code',0))\n"
        )
        binary.chmod(0o700)
        adapter = source.AstOutlineAdapter(source.AstContextConfig(binary=binary))
        operations = {"outline": "file_outline", "digest": "repo_map", "show": "show_symbol", "grep": "symbol_references"}
        for case in cases:
            calls.unlink(missing_ok=True)
            case_path.write_text(json.dumps(case, ensure_ascii=True))
            row = {"case": case, "operation": operations[case["command"]]}
            try:
                result = adapter._invoke(row["operation"], case["command"], [case["command"], "--json"], workspace,
                    include_imports=case.get("include_imports"), path_rewrites=(("/private/frozen/workspace", str(workspace)),))
                result_data = result.as_dict()
                row["result_json"] = json.dumps(result_data, ensure_ascii=True, separators=(",", ":")).replace(str(workspace), "<workspace>")
                try:
                    row["utf8_json"] = result.to_json().replace(str(workspace), "<workspace>")
                except Exception as error:
                    row["serialization_error"] = str(error)
            except Exception as error:
                row.update(error_class=type(error).__name__, error=str(error).replace(str(workspace), "<workspace>"))
            row["calls"] = [json.loads(line) for line in calls.read_text().splitlines()]
            rows.append(row)
    return {"snapshot_prefix": "/private/frozen/workspace", "invocations": rows}
