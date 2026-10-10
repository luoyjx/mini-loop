from tools.provider_tool_search_contracts import provider_tool_search_contracts


def test_sdk_tool_search_preserves_references_errors_and_stream_defaults():
    fixture = provider_tool_search_contracts()
    rows = {(row["name"], row["mode"]): row for row in fixture["cases"]}
    assert len(rows) == 12
    for mode in ("direct", "stream"):
        assert rows["empty", mode]["expected_content"][0]["content"]["tool_references"] == []
        refs = rows["references", mode]["expected_content"][0]["content"]["tool_references"]
        assert [ref["tool_name"] for ref in refs] == ["bash", "not_registered", "名称 tool"]
        error = rows["error", mode]["expected_content"][0]["content"]
        assert error["error_message"] is None
        assert rows["error-message", mode]["expected_content"][0]["content"]["error_message"] == "detail"
        future = rows["future-error", mode]["expected_content"][0]["content"]
        assert future["error_code"] == "future_code" and future["future"] == 17
        extension = rows["extensions", mode]["expected_content"][0]
        assert extension["future"] == {"root": "metadata"}
        assert extension["content"]["tool_references"][0]["future"] == {"reference": "metadata"}
        if mode == "stream":
            assert extension["text"] is None and extension["citations"] is None and extension["parsed_output"] is None
