import json

from tools.provider_bash_command_contracts import provider_bash_command_contracts


def test_actual_nontext_command_failures_keep_consumer_and_initialization_order(tmp_path):
    fixture = provider_bash_command_contracts(tmp_path)
    assert len(fixture["cases"]) == 36
    assert len(fixture["permissions"]) == 20
    for row in fixture["cases"]:
        assert row["background_live"] == 0
        if row["mode"] == "keep":
            value = json.loads(row["input_json"])["command"]
            kind = type(value).__name__
            operation = "lower" if row["background_enabled"] and not row["explicit"] else "split"
            assert row["output"] == f"Error: '{kind}' object has no attribute '{operation}'"
            assert row["background_initialized"] == (row["background_enabled"] and row["explicit"])
            assert row["trace"] == ["before", "guard", "after", "result"]
            assert row["outcomes"][0]["failed"]
        else:
            assert not row["background_initialized"]
    rows = {row["name"]: row for row in fixture["cases"] if row["mode"] != "keep"}
    assert rows["null-extra"]["output"] == "Error: _bash() got an unexpected keyword argument 'extra'"
    assert rows["null-rewrite"]["output"] == "command-rewritten"
    assert not rows["null-rewrite"]["outcomes"][0]["failed"]
    assert rows["null-deny"]["trace"] == ["before", "result"]
    assert rows["null-guard-deny"]["trace"] == ["before", "guard", "result"]
    for row in fixture["permissions"]:
        if row["name"].startswith("blocked"):
            assert row["output"] == "Permission denied: 'rm -rf /' is blocked"
        elif row["mode"] == "readonly":
            assert row["output"] == "Permission denied: this session is read-only (tool risk: exec)"
        else:
            assert row["output"] is None
