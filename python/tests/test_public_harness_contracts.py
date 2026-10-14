"""Keep public Harness fixtures tied to actual Source operations."""
import json
from pathlib import Path

from tools.public_harness_contracts import public_harness_contracts


def test_actual_public_harness_contracts(tmp_path):
    actual = public_harness_contracts(tmp_path)
    root = Path(__file__).resolve().parents[2]
    assert actual == json.loads((root / "go/testdata/python-public-harness.json").read_text())
    assert len(actual["fields"]) == len(actual["value_rows"]) == 16
    assert len(set(actual["fields"])) == 16
    assert len(actual["default_none_fields"]) == 15
    assert actual["default_injectors_empty_tuple"]
    assert actual["empty_derive_new_value"]
    for row in actual["value_rows"]:
        assert all(value is True for name, value in row.items() if name != "field")
    assert actual["errors"]["unknown_derive"] == {
        "exception_class": "TypeError",
        "message": "Harness has no field(s): ['a_unknown', 'z_unknown']",
    }
    assert actual["errors"]["unknown_resolve_none"]["exception_class"] == "AttributeError"
    assert actual["errors"]["frozen_assignment"]["exception_class"] == "FrozenInstanceError"
    for construction in actual["construction"]:
        assert set(construction["bindings"]) == set(actual["fields"])
        assert all(construction["bindings"].values())
    assert actual["explicit_empty_injectors_replace"]
    runtime = actual["runtime"]
    assert runtime["parent_output"] == runtime["child_output"] == "harness-ok"
    assert runtime["parent_model_calls"] == 2
    assert runtime["parent_history_unchanged_by_child"]
    assert (runtime["parent_depth"], runtime["child_depth"]) == (0, 1)
    assert runtime["injector_calls"] == ["a", "a"]
    assert runtime["injector_parent_seen"] and runtime["injector_child_seen"]
    assert runtime["child_dynamic_system"] is False
    assert runtime["child_fields"]["tools"] == {
        "same_identity": False, "selected_names": ["bash"], "expected_names": ["bash"],
    }
    assert runtime["child_fields"]["injectors"] == {
        "same_identity": False, "entries_inherited": True,
    }
    assert all(row["same_identity"] for name, row in runtime["child_fields"].items()
               if name not in {"tools", "injectors"})
