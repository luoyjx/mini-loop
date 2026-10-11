from tools.provider_nonmapping_input_contracts import provider_nonmapping_input_contracts


def test_actual_sdk_direct_nonmapping_inputs_survive_forwarding():
    fixture = provider_nonmapping_input_contracts()
    assert len(fixture["cases"]) == 12
    for row in fixture["cases"]:
        assert row["expected"] == row["forwarded"]["content"]
        assert row["expected"][0]["type"] == row["kind"]
        if row["name"] == "null":
            assert row["expected"][0]["input"] is None
        if row["name"] == "float":
            assert type(row["expected"][0]["input"]) is float
