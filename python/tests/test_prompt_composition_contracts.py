import json
from pathlib import Path

from tools.prompt_composition_contracts import prompt_composition_contracts


def test_actual_public_prompt_composition_contracts():
    actual = prompt_composition_contracts()
    root = Path(__file__).resolve().parents[2]
    assert actual == json.loads((root / "go/testdata/python-prompt-composition.json").read_text())
    assert (len(actual["sections"]), len(actual["runtime_facts"]), len(actual["memory_system"])) == (11, 13, 7)
    sections = {row["recipe"]["name"]: row for row in actual["sections"]}
    assert sections["callback-none"]["results"][0]["output"] == "kept"
    assert sections["failure-middle"]["trace"] == ["first", "failed"]
    assert sections["failure-middle"]["results"][0]["error"] == {"kind": "ValueError", "message": "section fault"}
    assert sections["repeated-build"]["trace"] == ["one", "two", "one", "two"]
    assert sections["whitespace"]["results"][0]["output"] == " \n\n\n\n\nend\n"
    facts = {row["recipe"]["name"]: row for row in actual["runtime_facts"]}
    assert facts["pressure-below"]["output"] == ""
    assert "over 75%" in facts["pressure-75"]["output"]
    assert "over 90%" in facts["pressure-90"]["output"]
    assert all(row["recipe"]["threshold"] >= 1 for row in actual["runtime_facts"])
    assert facts["memory-no-recall"]["output"] == facts["memory-foreign-only"]["output"] == ""
    assert not facts["memory-no-recall"]["index_exists"]
    assert "Owner facts" in facts["memory-owned"]["output"]
    assert "Foreign facts" not in facts["memory-owned"]["output"]
    assert all(row["records_unchanged"] and row["messages_unchanged"] and row["meter_unchanged"] and row["default_system_unchanged"] for row in actual["runtime_facts"])
    memory = {row["recipe"]["name"]: row for row in actual["memory_system"]}
    assert memory["empty-base-empty-store"]["results"][0]["output"].endswith("(no memories yet)")
    assert not memory["base-fault-before-index"]["first_index_exists"]
    assert memory["base-fault-before-index"]["trace"] == ["failed"]
    assert "Added later" not in memory["live-index"]["results"][0]["output"]
    assert "Added later" in memory["live-index"]["results"][1]["output"]
    assert all(row["first_records_unchanged"] and row["messages_unchanged"] for row in actual["memory_system"])
