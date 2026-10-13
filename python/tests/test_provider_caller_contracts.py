from tools.provider_caller_contracts import provider_caller_contracts


def test_actual_sdk_preserves_declared_caller_extensions():
    fixture = provider_caller_contracts()
    assert fixture["sdk_version"] == "0.107.1"
    assert len(fixture["cases"]) == 160
    for row in fixture["cases"][:48]:
        expected = row["expected_content"][0]
        original = row["message"]["content"][0]
        assert expected["caller"] == original.get("caller")
        if row["name"].endswith("direct-tool-id-extension"):
            assert expected["caller"]["tool_id"] == "opaque metadata"

    observed_ids = {
        "empty": "",
        "missing": None,
        "null": None,
        "numeric-513": 513,
        "bool-true": True,
        "list": [None, "secret-value", 513],
        "object": {"secret-key": "secret-value"},
    }
    caller_types = {
        "code-2025": "code_execution_20250825",
        "code-2026": "code_execution_20260120",
    }
    block_classes = {
        "tool_use": "ToolUseBlock",
        "server_tool_use": "ServerToolUseBlock",
        "web_search_tool_result": "WebSearchToolResultBlock",
        "web_fetch_tool_result": "WebFetchToolResultBlock",
    }
    expected_callers = {
        f"{kind}/{tag}-tool-id-{label}": {"type": caller_type, "tool_id": value}
        for kind in block_classes
        for tag, caller_type in caller_types.items()
        for label, value in observed_ids.items()
    }
    observed_rows = fixture["cases"][48:]
    assert {(row["name"], row["mode"]) for row in observed_rows} == {
        (name, mode) for name in expected_callers for mode in ("direct", "stream")
    }
    for row in observed_rows:
        original = row["message"]["content"][0]
        observed = row["expected_content"][0]
        kind = original["type"]
        # The fixed streaming SDK falls back to ParsedTextBlock for this tag,
        # while the actual dumped type and caller metadata remain intact.
        expected_class = ("ParsedTextBlock" if kind == "web_fetch_tool_result"
                          and row["mode"] == "stream" else block_classes[kind])
        assert row["sdk_block_class"] == expected_class
        assert observed["type"] == kind
        assert observed["caller"] == expected_callers[row["name"]]
        if kind in ("tool_use", "server_tool_use"):
            assert observed["id"] == original["id"]
            assert observed["name"] == original["name"]
            assert observed["input"] == original["input"]
        else:
            assert observed["tool_use_id"] == original["tool_use_id"]
        if row["name"].endswith("missing"):
            assert "tool_id" not in original["caller"]
        else:
            assert original["caller"] == expected_callers[row["name"]]
        if row["name"].endswith("numeric-513"):
            assert type(observed["caller"]["tool_id"]) is int
        elif row["name"].endswith("bool-true"):
            assert type(observed["caller"]["tool_id"]) is bool
