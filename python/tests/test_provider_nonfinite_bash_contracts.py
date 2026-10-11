import json
from tools.provider_nonfinite_bash_contracts import provider_nonfinite_bash_contracts


def test_nonfinite_bash_sdk_gate_and_managed_batch_boundaries(tmp_path):
    fixture = provider_nonfinite_bash_contracts(tmp_path)
    assert len(fixture["cases"]) == 36
    assert len(fixture["managed"]) == 24
    for row in fixture["cases"]:
        assert json.loads(row["content_json"])[0]["type"] == "tool_use"
        assert row["content_json"] == row["stream_content_json"]
        if row["mode"] == "deny":
            assert row["trace"] == ["before", "result"]
            assert row["outcomes"][0]["denied"]
        elif row["mode"] == "guard-deny":
            assert row["trace"] == ["before", "guard", "result"]
            assert row["outcomes"][0]["denied"]
        else:
            assert row["trace"] == ["before", "guard", "after", "result"]
            if row["mode"] == "rewrite":
                assert row["output"] == "command-rewritten"
    for row in fixture["managed"]:
        assert row["transport_requests"] == 1
        assert not row["action_recorded"]
        if row["name"].startswith("root"):
            assert row["trace"] == []
            assert row["tool_uses"] == 0
            assert row["messages"] == 2
            assert row["error"]["class"] == "TypeError"
        else:
            assert row["error"] is None
            assert row["trace"] == ["before", "guard", "result"]
            assert row["tool_uses"] == 1
            assert row["messages"] == 4
            assert row["tool_output"] == "Error: Out of range float values are not JSON compliant"
