import json
from pathlib import Path

from tools.dotenv_file_contracts import dotenv_file_contracts


def test_real_dotenv_discovery_decoding_and_partial_effects(tmp_path):
    actual = dotenv_file_contracts(tmp_path)
    expected = json.loads((Path(__file__).resolve().parents[2] /
                           "go/testdata/python-dotenv-files.json").read_text())
    assert actual == expected
    assert actual["version"] == "1.2.2"
    assert len(actual["discovery"]) == 10
    assert len(actual["reads"]) == 36
    selected = {row["name"]: row["selected"] for row in actual["discovery"]}
    assert selected["caller-parent"] == "source/.env"
    assert selected["cwd-explicit"] == "working/.env"
    assert selected["fifo"] == "source/pkg/.env"
    assert selected["directory-skipped"] == selected["dangling-skipped"] == "source/.env"
    reads = {(row["name"], row["override"]): row for row in actual["reads"]}
    assert reads["newlines", True]["applied"]["A"] == "one\ntwo\nthree"
    assert reads["disabled-invalid", True]["loaded"] is False
    assert reads["disabled-invalid", True]["valueFault"]["type"] == "UnicodeDecodeError"
    assert reads["invalid-utf8", True]["applied"] == {}
    assert reads["partial-fault", True]["applied"] == {"A": "prior"}
    assert reads["interpolation", False]["applied"] == {"A": "external", "B": "external"}
    assert reads["interpolation", False]["values"][-1]["value"] == "file"
