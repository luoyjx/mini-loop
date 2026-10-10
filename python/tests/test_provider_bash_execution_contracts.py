from tools.provider_bash_execution_contracts import provider_bash_execution_contracts


def test_actual_sdk_bash_execution_contracts():
    fixture = provider_bash_execution_contracts()
    assert fixture["sdk_version"] == "0.107.1"
    assert len(fixture["cases"]) == 26
    for row in fixture["cases"]:
        block = row["expected_content"][0]
        assert block["type"] == "bash_code_execution_tool_result"
        assert block["tool_use_id"] == "server_bash"
        content = block["content"]
        if content["type"] != "bash_code_execution_tool_result_error":
            assert content["return_code"] == row["message"]["content"][0]["content"]["return_code"]
            assert content["content"] == row["message"]["content"][0]["content"]["content"]
        if row["mode"] == "stream":
            assert block["text"] is None and block["citations"] is None and block["parsed_output"] is None
