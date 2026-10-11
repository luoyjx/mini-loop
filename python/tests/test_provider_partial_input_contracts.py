import json

from tools.provider_partial_input_contracts import provider_partial_input_contracts


def test_actual_sdk_partial_input_and_error_order():
    fixture = provider_partial_input_contracts()
    assert fixture["sdk_version"] == "0.107.1"
    assert fixture["jiter_version"] == "0.15.0"
    assert len(fixture["cases"]) == 98
    rows = {(row["kind"], row["name"]): row for row in fixture["cases"]}
    for kind in ("tool_use", "server_tool_use"):
        for name in ("no-deltas", "empty-delta", "empty-twice"):
            assert json.loads(rows[kind, name]["expected_json"])[0]["input"] == {"command": "true"}
        for name in ("partial-key", "partial-string", "partial-exponent", "duplicate-partial"):
            assert json.loads(rows[kind, name]["expected_json"])[0]["input"] == {"command": "true", "keep": 1}
        assert json.loads(rows[kind, "open-object"]["expected_json"])[0]["input"] == {}
        assert json.loads(rows[kind, "trailing-garbage"]["expected_json"])[0]["input"] == {"command": "true"}
        assert rows[kind, "invalid-repaired-later"]["error"]["class"] == "ValueError"
        assert rows[kind, "invalid-repaired-later"]["snapshots_json"] == []
        snapshots = rows[kind, "completed-two-chunks"]["snapshots_json"]
        assert json.loads(snapshots[0])[0]["input"] == {"command": "true"}
        assert json.loads(snapshots[1])[0]["input"]["keep"] == "unfinished"
