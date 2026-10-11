"""Native audit migration consumes actual Source profiles, not re-created rules."""

import json
from pathlib import Path

from tools.audit_posture_contracts import audit_posture_contracts


def test_audit_posture_export_matches_source_across_roots(tmp_path):
    expected = json.loads((Path(__file__).resolve().parents[2] /
                           "go/testdata/python-audit-posture.json").read_text())
    assert audit_posture_contracts(tmp_path / "first") == expected
    assert audit_posture_contracts(tmp_path / "second") == expected


def test_remote_audit_distinguishes_missing_posture_and_error_precedence(tmp_path):
    cases = {row["name"]: row for row in audit_posture_contracts(tmp_path)["cases"]}
    for name in ("missing", "null", "empty"):
        assert [f["check"] for f in cases[name]["findings"]] == ["posture-unavailable"]
    assert cases["all-selected"]["findings"] == []
    assert cases["all-selected"]["rendered"] == "audit: no findings."
    assert [f["check"] for f in cases["error-precedence"]["findings"]] == [
        "action-journal", "durable-state-failing", "secret-masking",
        "shell-confinement-unavailable"]
    assert any(f["check"] == "durable-state" for f in cases["default-empty"]["findings"])
