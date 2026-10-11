import json
from pathlib import Path

from tools.dotenv_boot_contracts import dotenv_boot_contracts


def test_actual_config_import_and_settings_boot_phases(tmp_path):
    expected = json.loads((Path(__file__).resolve().parents[2] /
                           "go/testdata/python-dotenv-boot.json").read_text())
    actual = dotenv_boot_contracts(tmp_path)
    assert actual == expected
    assert actual["version"] == "1.2.2"
    assert len(actual["cases"]) == 18
    rows = {row["name"]: row for row in actual["cases"]}
    assert rows["caller-nearest"]["settings"]["model"] == "nearest"
    assert rows["cwd-ignored"]["settings"]["model"] == "incoming"
    assert rows["frozen"]["settings"]["model"] == "cwd"
    partial = rows["partial-import-fault"]
    assert partial["stage"] == "import" and partial["settings"] is None
    assert partial["applied"]["MODEL_ID"] == "prior"
    assert partial["applied"]["ANTHROPIC_AUTH_TOKEN"] == "incoming-token"
    assert not partial["workspaceCreated"]
    failed = rows["settings-integer-fault"]
    assert failed["stage"] == "settings" and failed["imported"]
    assert "ANTHROPIC_AUTH_TOKEN" not in failed["applied"]
    assert not failed["workspaceCreated"]
    assert rows["disabled-invalid-file"]["settings"]["model"] == "incoming"
    assert "ANTHROPIC_AUTH_TOKEN" not in rows["disabled-base-url-cleanup"]["applied"]
    late = rows["factories-after-import"]
    assert late["settings"]["model"] == "later"
    assert late["applied"]["ANTHROPIC_AUTH_TOKEN"] == "later-token"
    assert late["workspaceCreated"]
