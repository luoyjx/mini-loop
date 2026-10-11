from tools.provider_text_editor_contracts import provider_text_editor_contracts


def test_actual_sdk_text_editor_contracts():
    fixture = provider_text_editor_contracts()
    assert fixture["sdk_version"] == "0.107.1"
    assert len(fixture["cases"]) == 48
    for row in fixture["cases"]:
        block = row["expected_content"][0]
        assert block["type"] == "text_editor_code_execution_tool_result"
        assert block["tool_use_id"] == "server_editor"
        content = block["content"]
        if content["type"] == "text_editor_code_execution_view_result":
            assert all(key in content for key in ("num_lines", "start_line", "total_lines"))
        if content["type"] == "text_editor_code_execution_str_replace_result":
            assert all(key in content for key in ("lines", "new_lines", "new_start", "old_lines", "old_start"))
        if content["type"] == "text_editor_code_execution_tool_result_error":
            assert "error_message" in content
        if row["mode"] == "stream":
            assert block["text"] is None and block["citations"] is None and block["parsed_output"] is None
