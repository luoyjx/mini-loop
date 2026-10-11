from tools.provider_whole_usage_contracts import provider_whole_usage_contracts


def test_actual_sdk_whole_usage_projection_and_stream_failure_phase():
    fixture = provider_whole_usage_contracts()
    rows = {(row["name"], row["mode"]): row for row in fixture["cases"]}
    assert len(rows) == 62
    for name in ("absent", "null", "integer", "fraction", "boolean", "text",
                 "empty-array", "array-model", "nested-array", "array-text-counts",
                 "array-nested-models", "array-metadata-list", "array-extensions", "mixed-array"):
        direct, stream = rows[name, "direct"], rows[name, "stream"]
        assert direct["outcome"] == "completed"
        assert direct["consumer"]["observations"] == 0
        assert stream["outcome"] == "error"
        assert stream["phase"] == "message_delta"
        assert stream["error"]["class"] == "AttributeError"
        assert len(stream["usage_stages"]) == 1
        assert stream["usage_stages"][0]["event"] == "message_start"
        assert stream["usage_stages"][0]["event_usage"] == direct["event_usage"]
    assert rows["absent", "direct"]["wire_usage_present"] is False
    assert rows["absent", "direct"]["usage_present"] is True
    assert rows["absent", "direct"]["event_usage"] is None
    assert rows["null", "direct"]["usage_present"] is True
    assert rows["boolean", "direct"]["event_usage"] == {"value": "True"}
    assert "Usage(" in rows["array-model", "direct"]["event_usage"]["value"]
    for name in ("empty-object", "input-only", "output-only", "extension-only", "complete"):
        direct, stream = rows[name, "direct"], rows[name, "stream"]
        assert direct["outcome"] == stream["outcome"] == "completed"
        assert [stage["event"] for stage in stream["usage_stages"]] == ["message_start", "message_delta"]
        assert stream["event_usage"]["output_tokens"] == 5
        assert stream["event_usage"]["input_tokens"] == (13 if name in {"input-only", "complete"} else None)
        assert stream["consumer"]["anchor"] == (13 if name in {"input-only", "complete"} else None)
    assert rows["extension-only", "direct"]["event_usage"]["secret-marker"] == [13, "secret-marker"]

    wide_names = {f"array-{field}-{kind}" for field in ("required", "optional", "nested")
                  for kind in ("int64-over", "negative-wide", "wide-text", "wide-float")}
    wide = [row for row in fixture["cases"] if row["name"] in wide_names]
    assert len(wide) == 24
    for row in wide:
        if row["mode"] == "direct":
            assert row["outcome"] == "completed"
            assert row["consumer"]["observations"] == 0
        else:
            assert row["outcome"] == "error"
            assert row["phase"] == "message_delta"
            assert len(row["usage_stages"]) == 1
    text = rows["array-optional-wide-text", "direct"]["event_usage"]["value"]
    assert "cache_read_input_tokens=" + str(10**80) in text
    assert "cache_read_input_tokens='" not in text
    assert "cache_read_input_tokens=1e+100" in rows["array-optional-wide-float", "direct"]["event_usage"]["value"]
