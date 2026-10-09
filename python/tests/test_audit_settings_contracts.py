"""Pin the configuration-only audit separately from live-manager sorting."""

import json
from pathlib import Path

from tools.audit_settings_contracts import audit_settings_contracts


def test_settings_audit_matches_source_in_independent_roots(tmp_path):
    expected = json.loads((Path(__file__).resolve().parents[2] /
                           "go/testdata/python-audit-settings.json").read_text())
    assert audit_settings_contracts(tmp_path / "first") == expected
    assert audit_settings_contracts(tmp_path / "second") == expected
    cases = {row["name"]: row for row in expected["cases"]}
    assert [f["check"] for f in cases["writable-order"]["findings"]] == [
        "authentication", "fake-llm", "workspace-permissions", "trajectory-content"]
    assert cases["shared-token"]["error"] is not None
    assert cases["single-token"]["findings"] == []
