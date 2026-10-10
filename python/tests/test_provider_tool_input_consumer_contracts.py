from tools.provider_tool_input_consumer_contracts import provider_tool_input_consumer_contracts


def test_actual_agent_bash_binding_and_hook_order(tmp_path):
    fixture = provider_tool_input_consumer_contracts(tmp_path)
    assert len(fixture["cases"]) == 51
    rows = {row["name"]: row for row in fixture["cases"]}
    for name in ("complete", "open-object", "ordered-extra", "empty-extra", "extra-and-missing"):
        assert rows[name]["trace"] == ["before", "guard", "after", "result"]
        assert rows[name]["outcomes"][0]["failed"] is True
    assert "'z_extra'" in rows["ordered-extra"]["output"]
    assert "unexpected keyword" in rows["extra-and-missing"]["output"]
    for name in ("extra-rewritten", "missing-rewritten"):
        assert rows[name]["trace"] == ["before", "guard", "execute", "after", "result"]
        assert rows[name]["output"] == "executed:rewritten"
    for name in ("extra-denied-before", "missing-denied-before"):
        assert rows[name]["trace"] == ["before", "result"]
        assert rows[name]["outcomes"][0]["denied"] is True

    rows = {row["name"]: row for row in fixture["cases"]}
    assert rows["root-float"]["output"].endswith("not float")
    assert rows["root-rewritten"]["trace"] == ["before", "guard", "execute", "after", "result"]
    assert rows["root-denied-before"]["trace"] == ["before", "result"]
