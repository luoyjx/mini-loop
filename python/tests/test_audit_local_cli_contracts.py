"""Source audit's local mode constructs real services without running a session."""

import json
from pathlib import Path
from tools.audit_local_cli_contracts import audit_local_cli_contracts


def test_local_audit_entry_point_contracts(tmp_path):
    expected = json.loads((Path(__file__).resolve().parents[2] /
                           "go/testdata/python-audit-local-cli.json").read_text())
    actual = audit_local_cli_contracts(tmp_path / "first")
    for got, want in zip(actual["cases"], expected["cases"], strict=True):
        for key in want:
            assert got[key] == want[key], (got["name"], key, got[key], want[key])
    assert audit_local_cli_contracts(tmp_path / "second") == expected
    rows = {row["name"]: row for row in expected["cases"]}
    assert all(row["sessions"] == 0 for row in expected["cases"])
    assert "[CRITICAL] host-bind" in rows["public"]["stdout"]
    assert rows["workflow-setting"]["workflow_setting"] == [True]
    assert rows["workflow-setting"]["workflows"] == [False]
    assert rows["ignored-port"]["exit_code"] == 1
    assert rows["unknown-args"]["exit_code"] == 1
    assert rows["missing-key"]["workspace_exists"]
    assert rows["invalid-settings"]["fault"] is not None
    assert rows["invalid-auth"]["fault"] is not None
