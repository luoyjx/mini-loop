from tools.provider_partial_json_contracts import provider_partial_json_contracts


def test_jiter_partial_edges_are_pinned():
    fixture = provider_partial_json_contracts()
    assert fixture["jiter_version"] == "0.15.0"
    assert len(fixture["cases"]) == 42
    rows = {row["name"]: row for row in fixture["cases"]}
    assert "expected_json" in rows["depth-200-complete"]
    assert rows["depth-201-complete"]["error"]["message"].startswith("recursion limit exceeded")
