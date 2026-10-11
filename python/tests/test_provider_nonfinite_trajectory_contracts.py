import json
import math

from tools.provider_nonfinite_trajectory_contracts import provider_nonfinite_trajectory_contracts


def test_managed_nonfinite_trajectory_snapshots_and_privacy(tmp_path):
    fixture = provider_nonfinite_trajectory_contracts(tmp_path)
    assert len(fixture["cases"]) == 4
    for row in fixture["cases"]:
        assert row["transport_requests"] == 1
        assert row["recording_error"] is None
        assert row["status"] == "error"
        assert not row["raw_contains_secret"]
        assert row["live_input_json"] == row["input_json"]
        snapshots = json.loads(row["snapshots_json"])
        assert [event["type"] for event in snapshots] == ["model_start", "model_end", "tool_use", "model_start"]
        if row["capture_content"]:
            observed = snapshots[1]["output"][0]["input"]
            assert "fixture-secret" not in observed
            assert observed["<secret-hidden>"][0] == "<secret-hidden>"
            number = observed["keep"]
            assert math.isnan(number) if row["name"] == "nan" else number == float("inf")
            result = snapshots[3]["messages"][-1]["content"][0]
            assert result["cache_control"] == {"type": "ephemeral"}
        else:
            assert snapshots[0]["messages"] == "[redacted: dict, 4 item(s)]"
            assert snapshots[1]["output"] == "[redacted: list, 1 item(s)]"
            assert snapshots[2]["input"] == "[redacted: dict, 3 item(s)]"
