"""Observe public Source AST operations with actual snapshots and CLI children."""
import json
from pathlib import Path
import re
import sys
import tempfile

from mini_loop.ast_context import AstContextConfig, AstOutlineAdapter, AST_OUTLINE_HOMEPAGE


def ast_operation_contracts() -> dict:
    cases = []
    for density in ("names", "compact", "default", "wide"):
        cases.append(dict(name="map-" + density, operation="repo_map", paths=["code.py", "code.py", "other.py"], density=density, include_imports=density == "wide"))
    for view in ("full", "public", "minimal"):
        for imports in (False, True):
            cases.append(dict(name=f"outline-{view}-{imports}", operation="file_outline", paths=["code.py"], view=view, include_imports=imports))
    cases += [
        dict(name="show-explicit", operation="show_symbol", path_or_glob="code.py", symbols=["-Option", "One"]),
        dict(name="show-signature", operation="show_symbol", path_or_glob="cod*.py", symbols=["One"], signature_only=True),
        dict(name="show-empty-glob", operation="show_symbol", path_or_glob="none*.py", symbols=["One"]),
        dict(name="show-ambiguous-glob", operation="show_symbol", path_or_glob="*.py", symbols=["One"]),
        dict(name="show-directory", operation="show_symbol", path_or_glob=".", symbols=["One"]),
        dict(name="references", operation="symbol_references", patterns=["-Option", "One"], paths=["code.py"]),
        dict(name="references-options", operation="symbol_references", patterns=["One"], paths=["code.py"], kinds=["def", "call", "def"], max_per_file=7),
        dict(name="references-all-kinds", operation="symbol_references", patterns=["One"], paths=["code.py"], kinds=["def", "call", "ref", "import", "comment", "string"], max_per_file=10000),
        dict(name="map-bad-density", operation="repo_map", paths=["code.py"], density="bad"),
        dict(name="outline-bad-view", operation="file_outline", paths=["code.py"], view="bad"),
        dict(name="outline-empty-paths", operation="file_outline", paths=[]),
        dict(name="show-empty-symbols", operation="show_symbol", path_or_glob="code.py", symbols=[]),
        dict(name="references-empty-patterns", operation="symbol_references", patterns=[], paths=["code.py"]),
        dict(name="references-bad-kind", operation="symbol_references", patterns=["One"], paths=["code.py"], kinds=["bad"]),
        dict(name="references-too-many-kinds", operation="symbol_references", patterns=["One"], paths=["code.py"], kinds=["def"] * 7),
        dict(name="references-bad-cap", operation="symbol_references", patterns=["One"], paths=["code.py"], max_per_file=0),
    ]
    rows = []
    with tempfile.TemporaryDirectory(prefix="mini-loop-ast-operation-contract-") as temp:
        root = Path(temp).resolve()
        workspace = root / "workspace"
        workspace.mkdir()
        (workspace / "code.py").write_text("class One: pass\n")
        (workspace / "other.py").write_text("class Other: pass\n")
        calls = root / "calls"
        binary = root / "ast-cli"
        binary.write_text(
            f"#!{sys.executable}\nimport json,sys,os\nfrom pathlib import Path\n"
            f"calls=Path({str(calls)!r})\n"
            "with calls.open('a') as stream: stream.write(json.dumps({'args':sys.argv[1:],'cwd':os.getcwd()})+'\\n')\n"
            "if sys.argv[1:]==['--version']:\n"
            f" sys.stdout.write('ast-outline 1.9.0\\nhomepage: {AST_OUTLINE_HOMEPAGE}\\n')\n"
            "else:\n"
            " sys.stdout.write(json.dumps({'tool':'ast-outline','schema_version':1,'command':sys.argv[1],'files':[],'results':[]},separators=(',',':')))\n"
        )
        binary.chmod(0o700)
        adapter = AstOutlineAdapter(AstContextConfig(binary=binary))
        def normalize(text):
            text = text.replace(str(workspace), "<workspace>")
            return re.sub(r"/[^\s\"']*/mini-loop-ast-[^/\s\"']+/workspace", "<snapshot>", text)
        for case in cases:
            calls.unlink(missing_ok=True)
            arguments = {key: value for key, value in case.items() if key not in ("name", "operation")}
            result = getattr(adapter, case["operation"])(workspace, **arguments)
            invoked = [json.loads(line) for line in calls.read_text().splitlines()] if calls.exists() else []
            snapshots = [arg for call in invoked for arg in call["args"] if "/mini-loop-ast-" in arg]
            rows.append(dict(case=case, result_json=normalize(result.to_json()), calls=json.loads(normalize(json.dumps(invoked))), cleaned=all(not Path(path).exists() for path in snapshots)))
    return {"operations": rows}
