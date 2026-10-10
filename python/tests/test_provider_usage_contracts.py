from tools.provider_usage_contracts import provider_usage_contracts


def test_real_sdk_usage_keeps_all_declared_fields_and_future_extensions():
    fixture = provider_usage_contracts()
    rows = {(row["name"], row["mode"]): row for row in fixture["cases"]}
    assert len(rows) == 7
    for mode in ("direct", "stream"):
        minimal = rows["minimal", mode]["event_usage"]
        assert set(minimal) == set(fixture["usage_fields"])
        assert minimal["input_tokens"] == 13 and minimal["output_tokens"] == 5
        assert minimal["cache_creation"] is None
        nested = rows["nested", mode]["event_usage"]
        assert nested["cache_creation"] == {"ephemeral_1h_input_tokens": 7, "ephemeral_5m_input_tokens": 10}
        assert nested["inference_geo"] == "us"
        assert nested["output_tokens_details"] == {"thinking_tokens": 3}
        assert nested["server_tool_use"] == {"web_fetch_requests": 2, "web_search_requests": 4}
        assert nested["service_tier"] == "priority"
    assert rows["explicit-null", "direct"]["event_usage"] == rows["minimal", "direct"]["event_usage"]
    assert rows["future-extension", "direct"]["event_usage"]["future_counter"] == {"generation": 8}
    assert rows["scalar-strings", "direct"]["event_usage"]["input_tokens"] == "13"
    assert rows["scalar-strings", "direct"]["event_usage"]["cache_creation_input_tokens"] == 17
