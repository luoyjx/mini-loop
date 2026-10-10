import json

from tools.provider_bash_options_contracts import provider_bash_options_contracts


def test_actual_bash_options_preserve_values_and_select_consumers(tmp_path):
    fixture = provider_bash_options_contracts(tmp_path)
    assert len(fixture["cases"]) == 56
    for row in fixture["cases"]:
        value = json.loads(row["input_json"])
        selected = row["background_enabled"] and bool(value.get("run_in_background"))
        effect = "background" if selected else "foreground"
        assert row["trace"] == ["before", "guard", effect, "after", "result"]
        assert row["output"] == effect + ":printf field-fixture"
        assert row["outcomes"] == [{"output": row["output"], "denied": False, "failed": False}]
        assert row["final_input_json"] == row["input_json"]
        assert row["events"][0]["input"] == value
        assert row["default_candidate"] == ("bash", "printf", "field-fixture")
        assert row["proposed_candidate"] == (("bash", "printf", "field-fixture")
                                                if row["field"] == "approval_prefix" and row["name"] == "valid" else None)
