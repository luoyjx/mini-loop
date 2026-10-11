"""Full deployment auditing is distinct from public posture and self-audit."""

import json
from pathlib import Path
from tools.audit_live_contracts import audit_live_contracts


def test_live_audit_contracts_match_actual_managers(tmp_path):
    expected = json.loads((Path(__file__).resolve().parents[2] /
                           "go/testdata/python-audit-live.json").read_text())
    assert audit_live_contracts(tmp_path / "first") == expected
    assert audit_live_contracts(tmp_path / "second") == expected
    cases = {row["name"]: row for row in expected["cases"]}
    assert [f["check"] for f in cases["hardened"]["findings"]] == ["resource-limits", "fake-llm"]
    assert cases["empty-reason"]["observation"]["sandbox_reason"] == ""
    assert cases["problem-ledgers"]["observation"]["mcp"]["summary"][0] == "MCP collision (x4)"
    assert cases["cached-secret-faults"]["observation"]["unresolved"] == ["FAILED_TOKEN"]
