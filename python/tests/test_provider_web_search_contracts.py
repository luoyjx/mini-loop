from tools.provider_web_search_contracts import provider_web_search_contracts


def test_sdk_web_search_result_preserves_named_content_and_extensions():
    fixture = provider_web_search_contracts()
    rows = {(row["name"], row["mode"]): row for row in fixture["cases"]}
    assert len(rows) == 14
    for mode in ("direct", "stream"):
        assert rows["empty", mode]["expected_content"][0]["content"] == []
        result = rows["results", mode]["expected_content"][0]
        assert result["caller"] is None
        assert result["content"][0]["page_age"] is None
        assert result["content"][1]["page_age"] == "2 days"
        assert rows["future-error", mode]["expected_content"][0]["content"] == {"type": "web_search_tool_result_error", "error_code": "future_code", "future": 7}
        extension = rows["extensions", mode]["expected_content"][0]
        assert extension["future"] == {"root": "metadata"}
        assert extension["content"][0]["future"] == {"generation": 8}
