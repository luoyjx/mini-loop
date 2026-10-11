import json
import math

from tools.provider_nonfinite_consumer_contracts import provider_nonfinite_consumer_contracts


def test_actual_nonfinite_sdk_archive_and_forward_error_stages(tmp_path):
    fixture = provider_nonfinite_consumer_contracts(tmp_path)
    assert len(fixture["cases"]) == 4
    json.dumps(fixture, allow_nan=False)
    for row in fixture["cases"]:
        observed = json.loads(row["content_json"])[0]["input"]["keep"]
        restored = json.loads(row["reopened_messages_json"])[0]["content"][0]["input"]["keep"]
        events = json.loads(row["reopened_events_json"])
        assert len(events) == 1
        event_value = events[0]["content"][0]["input"]["keep"]
        assert all(math.isnan(value) for value in (observed, restored, event_value)) or observed == restored == event_value == float("inf")
        assert row["forward_error"]["class"] == "ValueError"
        assert row["transport_requests"] == 1  # next request fails before transport
        if row["kind"] == "tool_use":
            consumer = row["tool_consumer"]
            assert consumer["trace"] == ["before", "guard", "after", "result"]
            assert consumer["outcomes"][0]["failed"]
            assert "unexpected keyword argument 'keep'" in consumer["output"]
