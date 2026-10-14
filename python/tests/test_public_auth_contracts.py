import json
from pathlib import Path

from tools.public_auth_contracts import public_auth_contracts


def test_actual_public_auth_contracts():
    actual = public_auth_contracts()
    root = Path(__file__).resolve().parents[2]
    assert actual == json.loads((root / "go/testdata/python-public-auth.json").read_text())
    assert (len(actual["constructors"]), len(actual["environments"])) == (12, 24)
    constructors = {row["recipe"]["name"]: row for row in actual["constructors"]}
    environments = {row["recipe"]["name"]: row for row in actual["environments"]}
    assert constructors["empty"]["construction_error"]["exception_class"] == "ValueError"
    assert constructors["detached-input"]["before"] == constructors["detached-input"]["after"]
    assert constructors["ascii-principal-set"]["before"]["principals"] == ["alice", "bob"]
    assert constructors["literal-principals"]["before"]["principals"] == ["\x1c", " é ", "e\u0301", "用户"]
    for name in ["unicode-token-first", "unicode-token-last", "unicode-only"]:
        observations = constructors[name]["before"]["observations"]
        assert any(row["error"] and row["error"]["exception_class"] == "TypeError" for row in observations)
        assert all(row["principal"] is None for row in observations)
    last = constructors["unicode-token-last"]["before"]["observations"][2]
    assert last["header"] == "Bearer x"
    assert last["error"] == {"exception_class": "TypeError", "message": "comparing strings with non-ASCII characters is not supported"}
    assert environments["explicit-empty-isolates-ambient"]["before"]["auth_class"] == "NullAuth"
    assert environments["single-python-whitespace-only"]["before"]["configured"] is False
    for name in ["single-python-strip", "multi-python-empty-falls-back", "multi-python-entry-strip"]:
        assert environments[name]["construction_error"] is None
        assert environments[name]["before"]["configured"] is True
        assert any(row["principal"] is not None and row["header"] == "Bearer x" for row in environments[name]["before"]["observations"])
    for name in ["duplicate-different-owner", "duplicate-before-empty-owner-validation", "later-duplicate-before-earlier-empty-owner"]:
        error = environments[name]["construction_error"]
        assert error["exception_class"] == "ValueError"
        assert error["message"].startswith("MINILOOP_API_TOKENS assigns one token to both ")
    for name in ["explicit-input-snapshot", "ambient-input-snapshot"]:
        assert environments[name]["before"] == environments[name]["after"]
