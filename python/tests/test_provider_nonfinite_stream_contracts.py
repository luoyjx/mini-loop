import json
import math

from tools.provider_nonfinite_stream_contracts import provider_nonfinite_stream_contracts


def test_managed_nonfinite_input_sse_and_journal_failure(tmp_path):
    fixture = provider_nonfinite_stream_contracts(tmp_path)
    assert len(fixture["cases"]) == 4
    json.dumps(fixture, allow_nan=False)
    for row in fixture["cases"]:
        assert row["status"] == 200
        assert row["content_type"].startswith("text/event-stream")
        assert row["trace"] == ["before", "guard", "result"]
        events = json.loads(row["tool_events_json"])
        value = events[0]["input"]["keep"]
        assert math.isnan(value) if row["name"] == "nan" else value == float("inf")
        assert events[1]["error"] is True
        assert events[1]["output"] == "Error: Out of range float values are not JSON compliant"
        assert row["transport_requests"] == 1
        assert row["action_recorded"] is False
        assert row["event_types"][-1] == "done"
        assert "ValueError: Out of range float values are not JSON compliant" in row["done_text"]
        if row["store"] == "sqlite":
            stored = json.loads(row["stored_tool_events_json"])
            assert len(stored) == 1
            restored = stored[0]["input"]["keep"]
            assert math.isnan(restored) if row["name"] == "nan" else restored == float("inf")
