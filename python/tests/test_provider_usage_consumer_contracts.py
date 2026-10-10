from tools.provider_usage_consumer_contracts import provider_usage_consumer_contracts


def test_actual_sdk_scalar_event_values_and_metering_are_distinct():
    fixture = provider_usage_consumer_contracts()
    rows = {(row["name"], row["mode"]): row for row in fixture["cases"]}
    assert len(rows) == 34
    for mode in ("direct", "stream"):
        assert rows["input-numeric-string", mode]["event_usage"]["input_tokens"] == ("13" if mode == "direct" else 13)
        assert rows["input-numeric-string", mode]["consumer"]["anchor"] == (None if mode == "direct" else 13)
        assert rows["input-float-fraction", mode]["event_usage"]["input_tokens"] == 13.75
        assert rows["input-float-fraction", mode]["consumer"]["anchor"] == 13
        assert rows["input-float-negative", mode]["consumer"]["anchor"] == -3
        assert rows["input-true", mode]["consumer"]["anchor"] == 1
        for name in ("zero", "false", "null", "array", "object", "invalid-string"):
            assert rows["input-" + name, mode]["consumer"]["observations"] == 0
        cached = rows["required-strings-cache-coerced", mode]
        assert cached["event_usage"]["cache_creation_input_tokens"] == 17
        assert cached["consumer"]["anchor"] == (17 if mode == "direct" else 30)
    assert rows["input-strings-delta-numeric", "stream"]["event_usage"]["input_tokens"] == 21
    assert rows["input-numeric-delta-strings", "stream"]["event_usage"]["input_tokens"] == 21
    assert rows["input-numeric-delta-strings", "stream"]["consumer"]["anchor"] == 21
    assert "prompt_tokens" in fixture["source_hashes"]

    stages = rows["input-numeric-string", "stream"]["usage_stages"]
    assert [stage["event"] for stage in stages] == ["message_start", "message_delta"]
    assert stages[0]["usage"]["input_tokens"] == 13
    assert type(rows["input-true", "direct"]["event_usage"]["input_tokens"]) is bool
    assert type(rows["input-true", "stream"]["event_usage"]["input_tokens"]) is int
    assert type(rows["input-float-integral", "direct"]["event_usage"]["input_tokens"]) is float
    assert type(rows["input-float-integral", "stream"]["event_usage"]["input_tokens"]) is int
