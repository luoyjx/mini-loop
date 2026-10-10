from tools.provider_container_contracts import provider_container_contracts


def test_sdk_container_upload_survives_repository_normalization():
    fixture = provider_container_contracts()
    assert len(fixture["cases"]) == 6
    for row in fixture["cases"]:
        expected = {"type": "container_upload", "file_id": row["file_id"]}
        if row["mode"] == "stream":
            expected.update(text=None, citations=None, parsed_output=None)
        assert row["expected_content"][0] == expected
        assert row["expected_content"][1]["text"] == "ready"
