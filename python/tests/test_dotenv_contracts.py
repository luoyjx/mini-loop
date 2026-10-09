import json
from pathlib import Path
from tools.dotenv_contracts import dotenv_contracts


def test_dotenv_contracts_use_real_parser_and_override_phases():
    expected = json.loads((Path(__file__).resolve().parents[2] /
                           "go/testdata/python-dotenv.json").read_text())
    assert dotenv_contracts() == expected
    assert len(expected["cases"]) == 48
    rows = {(row["name"], row["override"]): row for row in expected["cases"]}
    assert rows["priority", True]["applied"]["A"] == "file-again"
    assert rows["priority", False]["applied"]["A"] == "external"
    assert rows["valueless", True]["loaded"] is True
    assert rows["valueless", True]["applied"] == {}
    assert rows["disabled", True]["loaded"] is False
    assert any(binding["error"] for binding in rows["recovery", True]["bindings"])
