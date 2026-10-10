from tools.provider_web_fetch_contracts import provider_web_fetch_contracts


def test_actual_sdk_web_fetch_contracts():
    fixture = provider_web_fetch_contracts()
    assert fixture["sdk_version"] == "0.107.1"
    assert len(fixture["cases"]) == 42
    for row in fixture["cases"]:
        block = row["expected_content"][0]
        assert block["type"] == "web_fetch_tool_result"
        assert block["tool_use_id"] == "server_fetch"
        assert "caller" in block
        content = block["content"]
        if content["type"] == "web_fetch_result":
            assert "retrieved_at" in content
            document = content["content"]
            assert "title" in document and "citations" in document
            assert document["source"] == row["message"]["content"][0]["content"]["content"]["source"]
        if row["name"] == "extensions":
            assert block["future"] == {"root": "metadata"}
            assert content["content"]["citations"]["future"] == 10
