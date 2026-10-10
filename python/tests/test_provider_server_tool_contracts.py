from tools.provider_server_tool_contracts import provider_server_tool_contracts


def test_actual_sdk_server_tool_use_contracts():
    fixture = provider_server_tool_contracts()
    assert fixture["sdk_version"] == "0.107.1"
    assert len(fixture["cases"]) == 32
    for row in fixture["cases"]:
        block = row["expected_content"][0]
        assert block["type"] == "server_tool_use"
        assert block["id"] == "server_search"
        assert block["input"] == row["message"]["content"][0]["input"]
        assert "caller" in block
        if row["name"] == "future-name":
            assert block["name"] == "future_server_tool"
        if row["name"] == "extensions":
            assert block["future"] == {"root": "metadata"}
        if row["name"] == "initial-replaced":
            assert block["input"] == {"query": "final query"}
