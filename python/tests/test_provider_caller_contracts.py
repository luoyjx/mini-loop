from tools.provider_caller_contracts import provider_caller_contracts


def test_actual_sdk_preserves_declared_caller_extensions():
    fixture = provider_caller_contracts()
    assert fixture["sdk_version"] == "0.107.1"
    assert len(fixture["cases"]) == 48
    for row in fixture["cases"]:
        expected = row["expected_content"][0]
        original = row["message"]["content"][0]
        assert expected["caller"] == original.get("caller")
        if row["name"].endswith("direct-tool-id-extension"):
            assert expected["caller"]["tool_id"] == "opaque metadata"
