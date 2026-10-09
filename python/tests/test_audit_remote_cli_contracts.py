"""Pin HTTP/exit behavior of the actual Source remote audit command."""

import json
from pathlib import Path

from tools.audit_remote_cli_contracts import audit_remote_cli_contracts


def test_remote_audit_cli_matches_source(tmp_path):
    expected = json.loads((Path(__file__).resolve().parents[2] /
                           "go/testdata/python-audit-remote-cli.json").read_text())
    result = audit_remote_cli_contracts(tmp_path)
    assert result == expected
    assert len(result["cases"]) == 22
    assert all(row["request"] == dict(path="/healthz", authorization=None)
               for row in result["cases"])
    cases = {row["name"]: row for row in result["cases"]}
    assert cases["missing"]["exit_code"] == 0
    assert cases["all-selected"]["exit_code"] == 0
    assert cases["default-empty"]["exit_code"] == 1
    assert result["missing_url"]["exit_code"] == 2
