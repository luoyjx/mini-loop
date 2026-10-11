"""Pin syntax parsing separately from compilation or execution semantics."""

import base64
import json
from pathlib import Path

from tools.python_syntax_contracts import python_syntax_contracts


def test_python_syntax_contracts_match_captured_profile():
    fixture = json.loads(
        (Path(__file__).parents[2] / "go/testdata/python-syntax-parser.json").read_text()
    )
    assert python_syntax_contracts() == fixture
    assert python_syntax_contracts() == fixture
    rows = {row["name"]: row for row in fixture["cases"]}
    assert len(rows) == len(fixture["cases"]) == 62
    for row in rows.values():
        source = base64.b64decode(row["source_utf8"], validate=True).decode(
            "utf-8", "surrogatepass"
        )
        assert ascii(source) == row["source_repr"]


def test_ast_parse_profile_preserves_semantic_and_encoding_boundaries():
    rows = {row["name"]: row for row in python_syntax_contracts()["cases"]}
    for name in ["return-at-module", "break-at-module", "continue-at-module",
                 "await-at-module", "nonlocal-at-module", "duplicate-arguments",
                 "duplicate-keyword", "future-import-late", "unknown-future"]:
        assert rows[name]["kind"] == "clean", name
    for name in ["match", "exception-group", "positional-only", "walrus"]:
        assert rows[name]["kind"] == "clean", name
    assert rows["missing-indent"]["error_class"] == "IndentationError"
    assert rows["mixed-tabs-spaces"]["error_class"] == "TabError"
    assert rows["error-line-two"]["line"] == 2
    assert rows["lone-surrogate"]["kind"] == "error"
    assert rows["lone-surrogate"]["error_class"] == "UnicodeEncodeError"
    # The selected 3.11.3 profile raises ValueError, not SyntaxError, for NUL.
    assert rows["null-character"]["kind"] == "error"
    assert rows["null-character"]["error_class"] == "ValueError"
